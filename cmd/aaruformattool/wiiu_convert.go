package main

// convert-wiiu: converts WUD, WUX or AaruFormat Wii U disc images to
// AaruFormat with decrypted sector storage and the Wii U media tags/metadata.

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/wiiu"
	"github.com/eberle1080/go-aaru/media"
)

// Wii U disc layout constants.
const (
	wiiuEncryptedOffset = 0x18000    // WIIU_ENCRYPTED_OFFSET: the TOC (physical sector 3)
	wiiuTOCSignature    = 0xCCA6E67B // WIIU_TOC_SIGNATURE
	wiiuTOCEntriesOff   = 0x800      // WIIU_TOC_ENTRIES_OFF
	wiiuTOCEntrySize    = 0x80       // WIIU_TOC_ENTRY_SIZE
)

// wiiuCommonKey is the (publicly known) Wii U common key that decrypts title
// keys in tickets.
var wiiuCommonKey = [16]byte{0xD7, 0xB0, 0x04, 0x02, 0x65, 0x9B, 0xA2, 0xAB,
	0xD2, 0xCB, 0x0D, 0xB2, 0x7F, 0xA2, 0xB6, 0x56}

// wiiuPartition is a partition parsed from the TOC (WiiuPartition).
type wiiuPartition struct {
	identifier  [26]byte // NUL-terminated, at most 25 characters
	startSector uint32   // physical (0x8000-byte) sector
	key         [16]byte
	hasTitleKey bool
}

func (p *wiiuPartition) id() string { return conNulString(p.identifier[:]) }

// wiiuDiscRead reads len(buf) bytes of the disc at offset into buf and returns
// the number of bytes read, or -1 on error (DiscReadFn).
type wiiuDiscRead func(buf []byte, offset uint64) int64

// cmdConvertWiiU implements the convert-wiiu verb.
func cmdConvertWiiU(args []string) int {
	va := newVerbArgs("convert-wiiu", usageConvertWiiU)
	va.fs.String("disc-key", "", "32-char hex disc key")
	pos, ok := va.parse(args, 2, 2)
	if !ok {
		return -1
	}
	o := newConOut(os.Stdout, os.Stderr)
	defer o.flush()
	return convertWiiU(o, pos[0], pos[1], conOptString(va, "disc-key"))
}

// usageConvertWiiU prints the help of convert-wiiu.
func usageConvertWiiU() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool convert-wiiu <input> <output> [options]\n\n")
	fmt.Printf("Converts a Wii U disc WUD, WUX, or AaruFormat image to an AaruFormat image\n")
	fmt.Printf("with decrypted sector storage and Wii U encryption metadata.\n\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <input>              Path to input WUD, WUX, or AaruFormat image.\n")
	fmt.Printf("  <output>             Path to output AaruFormat image.\n\n")
	fmt.Printf("Options:\n")
	fmt.Printf("  --disc-key=<hex>     32-char hex disc key (16 bytes).\n\n")
	fmt.Printf("Key resolution order:\n")
	fmt.Printf("  1. --disc-key argument\n")
	fmt.Printf("  2. Sidecar files: <input>.disckey, <input>.key\n")
	fmt.Printf("  3. Source AaruFormat image media tags\n")
}

// wiiuReaderDiscRead adapts a WUD/WUX reader.
func wiiuReaderDiscRead(r *wiiu.Reader) wiiuDiscRead {
	return func(buf []byte, offset uint64) int64 {
		n, _ := r.ReadAt(buf, int64(offset))
		if n <= 0 {
			return -1
		}
		return int64(n)
	}
}

// wiiuImageDiscRead reads disc bytes from the logical sectors of an
// AaruFormat image.
func wiiuImageDiscRead(img *aaru.Image) wiiuDiscRead {
	sector := make([]byte, wiiu.LogicalSectorSize)
	return func(buf []byte, offset uint64) int64 {
		done := 0
		for done < len(buf) {
			cur := offset + uint64(done)
			within := int(cur % wiiu.LogicalSectorSize)
			if conReadSector(img, cur/wiiu.LogicalSectorSize, sector) != 0 {
				break
			}
			done += copy(buf[done:], sector[within:])
		}
		if done == 0 {
			return -1
		}
		return int64(done)
	}
}

// wiiuReadVolumeDecrypted reads size bytes at fileOffset of the partition
// volume starting at partitionDiscOffset, decrypting every physical sector
// with key. Unreadable data reads as zeros before decryption.
func wiiuReadVolumeDecrypted(read wiiuDiscRead, key [16]byte, partitionDiscOffset, fileOffset uint64, buf []byte) {
	enc := make([]byte, wiiu.PhysicalSectorSize)
	done := 0
	for done < len(buf) {
		cur := fileOffset + uint64(done)
		secIdx := cur / wiiu.PhysicalSectorSize
		secOff := cur % wiiu.PhysicalSectorSize

		n := read(enc, partitionDiscOffset+secIdx*wiiu.PhysicalSectorSize)
		if n < wiiu.PhysicalSectorSize {
			clear(enc[max(n, 0):])
		}
		_ = wiiu.DecryptPhysicalSector(key, enc)

		done += copy(buf[done:], enc[secOff:])
	}
}

// wiiuParseTOC reads the TOC (physical sector 3), decrypts it with the disc
// key and returns its partitions.
func wiiuParseTOC(o *conOut, read wiiuDiscRead, discKey [16]byte) ([]wiiuPartition, bool) {
	toc := make([]byte, wiiu.PhysicalSectorSize)
	if read(toc, wiiuEncryptedOffset) < wiiu.PhysicalSectorSize {
		o.errorf("Cannot read TOC sector")
		return nil, false
	}

	_ = wiiu.DecryptPhysicalSector(discKey, toc)

	if sig := binary.BigEndian.Uint32(toc); sig != wiiuTOCSignature {
		o.errorf("TOC decryption failed (signature 0x%08X, expected 0x%08X)", sig, uint32(wiiuTOCSignature))
		o.errorf("Check that the disc key is correct")
		return nil, false
	}

	count := binary.BigEndian.Uint32(toc[0x1C:])
	if count > wiiu.MaxPartitions {
		count = wiiu.MaxPartitions
	}

	parts := make([]wiiuPartition, count)
	for i := range parts {
		entry := toc[wiiuTOCEntriesOff+i*wiiuTOCEntrySize:]
		copy(parts[i].identifier[:25], entry)
		parts[i].startSector = binary.BigEndian.Uint32(entry[0x20:])
	}
	return parts, true
}

// wiiuHasPrefixFold reports whether the NUL-terminated string at b starts
// with prefix, compared case-insensitively over len(prefix) characters.
// Bytes past the end of b read as NUL.
func wiiuHasPrefixFold(b []byte, prefix string) bool {
	for i := 0; i < len(prefix); i++ {
		var c byte
		if i < len(b) {
			c = b[i]
		}
		if c == 0 || wiiuToLower(c) != wiiuToLower(prefix[i]) {
			return false
		}
	}
	return true
}

func wiiuToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// wiiuDecryptTitleKey decrypts a ticket's title key with the Wii U common
// key; the IV is the title ID followed by eight zero bytes.
func wiiuDecryptTitleKey(encKey [16]byte, titleID [8]byte) [16]byte {
	block, _ := aes.NewCipher(wiiuCommonKey[:])
	var iv [16]byte
	copy(iv[:], titleID[:])
	var out [16]byte
	cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(out[:], encKey[:])
	return out
}

// wiiuExtractTitleKeys finds the TITLE.TIK files of the SI and GI partitions,
// decrypts their title keys and assigns them to the matching GM partitions;
// every other partition gets the disc key. It returns the number of title keys
// found.
func wiiuExtractTitleKeys(o *conOut, read wiiuDiscRead, discKey [16]byte, parts []wiiuPartition) int {
	keysFound := 0

	for p := range parts {
		id := parts[p].id()
		if !strings.HasPrefix(id, "SI") && !strings.HasPrefix(id, "GI") {
			continue
		}

		partDiscOff := wiiuEncryptedOffset + uint64(parts[p].startSector)*wiiu.PhysicalSectorSize - 0x10000

		fstHdr := make([]byte, wiiu.PhysicalSectorSize)
		wiiuReadVolumeDecrypted(read, discKey, partDiscOff, 0, fstHdr)
		if string(fstHdr[:4]) != "FST\x00" {
			continue
		}

		offsetFactor := binary.BigEndian.Uint32(fstHdr[4:])
		clusterCount := binary.BigEndian.Uint32(fstHdr[8:])

		// The cluster table must fit in the decrypted FST header sector
		if uint64(clusterCount)*0x20+0x20 > wiiu.PhysicalSectorSize {
			continue
		}

		clusterOffsets := make([]uint64, clusterCount)
		for c := range clusterOffsets {
			start := uint64(binary.BigEndian.Uint32(fstHdr[0x20+c*0x20:])) * wiiu.PhysicalSectorSize
			if start > wiiu.PhysicalSectorSize {
				clusterOffsets[c] = start - wiiu.PhysicalSectorSize
			}
		}

		entriesOffset := uint64(offsetFactor)*uint64(clusterCount) + 0x20

		var root [0x10]byte
		wiiuReadVolumeDecrypted(read, discKey, partDiscOff, entriesOffset, root[:])

		totalEntries := binary.BigEndian.Uint32(root[8:])
		if totalEntries > 100000 {
			continue
		}

		nameTable := uint64(totalEntries) * 0x10
		fstDataSize := nameTable + 0x10000
		if fstDataSize > 16*1024*1024 {
			continue
		}

		fstData := make([]byte, fstDataSize)
		wiiuReadVolumeDecrypted(read, discKey, partDiscOff, entriesOffset, fstData)

		for e := uint64(0); e < uint64(totalEntries); e++ {
			ent := fstData[e*0x10 : e*0x10+0x10]
			typ := ent[0]
			nameOff := uint64(binary.BigEndian.Uint32(ent) & 0x00FFFFFF)
			fileOff := uint64(binary.BigEndian.Uint32(ent[4:])) << 5
			fileSize := binary.BigEndian.Uint32(ent[8:])
			clusterID := binary.BigEndian.Uint16(ent[0x0E:])

			if typ == 1 { // directory
				continue
			}
			if fileSize < 0x200 {
				continue
			}
			if nameOff >= fstDataSize-nameTable {
				continue
			}
			if !wiiuHasPrefixFold(fstData[nameTable+nameOff:], "title.tik") {
				continue
			}
			if uint32(clusterID) >= clusterCount {
				continue
			}

			// Encrypted title key at +0x1BF, title ID at +0x1DC.
			tik := make([]byte, 0x10+0x1D+8)
			wiiuReadVolumeDecrypted(read, discKey, partDiscOff, clusterOffsets[clusterID]+fileOff+0x1BF, tik)

			var encTitleKey [16]byte
			var titleID [8]byte
			copy(encTitleKey[:], tik)
			copy(titleID[:], tik[0x1D:])

			titleKey := wiiuDecryptTitleKey(encTitleKey, titleID)

			gmName := fmt.Sprintf("GM%02X%02X%02X%02X%02X%02X%02X%02X", titleID[0], titleID[1], titleID[2],
				titleID[3], titleID[4], titleID[5], titleID[6], titleID[7])

			for g := range parts {
				gid := parts[g].id()
				if !strings.HasPrefix(gid, "GM") {
					continue
				}
				if len(gid) >= len(gmName) && gid[:len(gmName)] == gmName {
					parts[g].key = titleKey
					parts[g].hasTitleKey = true
					keysFound++
					o.successf("Title key for %s extracted", gmName)
					break
				}
			}
			clear(titleKey[:])
		}
	}

	for i := range parts {
		if !parts[i].hasTitleKey {
			parts[i].key = discKey
		}
	}
	return keysFound
}

// wiiuDiscHeaderMetadata extracts the product code and disc number from the
// disc header (physical sector 0) into the destination image.
func wiiuDiscHeaderMetadata(o *conOut, header []byte, output *aaru.Image) {
	// Product code: first 10 bytes, trailing spaces/NULs trimmed.
	code := header[:10]
	for len(code) > 0 && (code[len(code)-1] == 0 || code[len(code)-1] == ' ') {
		code = code[:len(code)-1]
	}
	if productCode := conNulString(code); productCode != "" {
		o.info("Product Code:", productCode)
		_ = output.SetMediaPartNumber(productCode)
	}

	// Disc number: ASCII digit at offset 0x15.
	if c := header[0x15]; c >= '0' && c <= '9' {
		discNum := int32(c-'0') + 1
		o.info("Disc Number:", fmt.Sprintf("%d", discNum))
		_ = output.SetMediaSequence(discNum, discNum)
	}

	o.successf("Metadata extracted from disc header")
}

// convertWiiU converts the Wii U disc image inputPath (WUD, WUX or
// AaruFormat) to the AaruFormat image outputPath. discKeyHex
// is nil when --disc-key was not given. It returns 0 on success and -1 on
// failure.
func convertWiiU(o *conOut, inputPath, outputPath string, discKeyHex *string) int {
	var (
		discKey     [16]byte
		haveDiscKey bool
		parts       []wiiuPartition
		regions     wiiu.PartitionKeyMap
	)
	defer func() {
		clear(discKey[:])
		for i := range parts {
			clear(parts[i].key[:])
		}
		for i := range regions {
			clear(regions[i].Key[:])
		}
	}()

	o.banner("                       WII U DISC IMAGE CONVERTER")

	// ── Step 1: Resolve the disc key ──
	o.section("Key Resolution")

	if discKeyHex != nil {
		if conParseHexKey(*discKeyHex, &discKey) {
			haveDiscKey = true
			o.successf("Disc key provided via --disc-key")
		} else {
			o.errorf("Invalid --disc-key format (expected 32 hex chars)")
		}
	}

	if !haveDiscKey {
		if k, ok := conTrySidecar(inputPath, ".disckey", 16); ok {
			copy(discKey[:], k)
			haveDiscKey = true
			o.successf("Disc key loaded from sidecar .disckey file")
		}
	}

	if !haveDiscKey {
		if k, ok := conTrySidecar(inputPath, ".key", 16); ok {
			copy(discKey[:], k)
			haveDiscKey = true
			o.successf("Disc key loaded from sidecar .key file")
		}
	}

	// ── Step 2: Open the source image ──
	o.section("Opening Source Image")

	var (
		input         *aaru.Image
		wud           *wiiu.Reader
		wudFile       *os.File
		totalLogical  uint64
		totalPhysical uint64
		read          wiiuDiscRead
	)
	closeInputs := func() {
		if input != nil {
			_ = input.Close()
		}
		if wudFile != nil {
			_ = wudFile.Close()
		}
	}

	if img, err := aaru.Open(inputPath); err == nil {
		input = img
		if mt := img.Info().MediaType; mt != media.WUOD {
			o.errorf("Source image media type is %s, expected WUOD", conMediaTypeString(mt))
			closeInputs()
			return -1
		}

		totalLogical = img.Info().Sectors
		totalPhysical = totalLogical / wiiu.LogicalPerPhysical
		o.successf("AaruFormat WUOD image: %d logical sectors", totalLogical)

		if !haveDiscKey {
			if tag, err := img.ReadMediaTag(aaru.MediaTagWiiUDiscKey); err == nil && len(tag) <= 16 {
				copy(discKey[:], tag)
				haveDiscKey = true
				o.successf("Disc key loaded from source image media tags")
			}
		}
		read = wiiuImageDiscRead(img)
	} else {
		r, f, err := wiiuOpenReader(o, inputPath)
		if err != nil {
			o.errorf("Cannot open input file: %s", conStrerror(err))
			return -1
		}
		wud, wudFile = r, f

		totalPhysical = uint64(r.Size()) / wiiu.PhysicalSectorSize
		totalLogical = totalPhysical * wiiu.LogicalPerPhysical

		kind := "WUD"
		if r.IsWUX() {
			kind = "WUX"
		}
		o.successf("%s image: %d physical sectors (%.2f GiB)", kind, totalPhysical,
			float64(r.Size())/(1024.0*1024.0*1024.0))
		read = wiiuReaderDiscRead(r)
	}

	if !haveDiscKey {
		o.errorf("No disc key found. Provide --disc-key or place a sidecar .disckey / .key file.")
		closeInputs()
		return -1
	}

	// ── Step 3: Parse the TOC and derive the partition keys ──
	o.section("Parsing Partition Table")

	parts, ok := wiiuParseTOC(o, read, discKey)
	if !ok {
		o.errorf("Cannot parse Wii U TOC")
		closeInputs()
		return -1
	}

	o.successf("Found %d partition(s)", len(parts))
	for i := range parts {
		o.printf("  %s\n", fmt.Sprintf("  Partition %d: %-18s start_sector=%d", i, parts[i].id(), parts[i].startSector))
	}

	keys := wiiuExtractTitleKeys(o, read, discKey, parts)
	o.successf("Extracted %d title key(s)", keys)

	// ── Step 4: Build the partition key map ──
	o.section("Building Partition Key Map")

	sort.SliceStable(parts, func(i, j int) bool { return parts[i].startSector < parts[j].startSector })

	regions = make(wiiu.PartitionKeyMap, len(parts))
	for i := range parts {
		regions[i].StartSector = parts[i].startSector
		regions[i].Key = parts[i].key
		if i+1 < len(parts) {
			regions[i].EndSector = parts[i+1].startSector
		} else {
			regions[i].EndSector = uint32(totalPhysical)
		}
		o.printf("  %s\n", fmt.Sprintf("  Region %d: sectors %d–%d (%s)", i, regions[i].StartSector,
			regions[i].EndSector, parts[i].id()))
	}

	keyMapData, _ := regions.MarshalBinary()

	// ── Step 5: Create the destination image ──
	o.section("Creating Destination Image")

	output, err := conCreateOutput(outputPath, media.WUOD, totalLogical)
	if err != nil {
		o.errorf("Cannot create output image: %s", conStrerror(err))
		closeInputs()
		return -1
	}
	o.successf("Destination image created")

	_ = output.WriteMediaTag(aaru.MediaTagWiiUDiscKey, discKey[:])
	o.successf("Written media tag: Wii U Disc Key")

	if keyMapData != nil {
		_ = output.WriteMediaTag(aaru.MediaTagWiiUPartitionKeyMap, keyMapData)
		o.successf("Written media tag: Wii U Partition Key Map")
	}

	// ── Step 6: Copy sectors ──
	o.section("Converting Sectors")
	o.printf("\n")

	result := 0
	phys := make([]byte, wiiu.PhysicalSectorSize)
	start := time.Now()
	var bytesProcessed uint64

sectors:
	for p := uint64(0); p < totalPhysical; p++ {
		if p%100 == 0 || p == totalPhysical-1 {
			o.progress(float64(p+1)/float64(totalPhysical)*100.0, bytesProcessed, start)
		}

		if wud != nil {
			if n, _ := wud.ReadAt(phys, int64(p)*wiiu.PhysicalSectorSize); n < len(phys) {
				clear(phys[n:])
			}
		} else {
			for s := uint64(0); s < wiiu.LogicalPerPhysical; s++ {
				buf := phys[s*wiiu.LogicalSectorSize : (s+1)*wiiu.LogicalSectorSize]
				if conReadSector(input, p*wiiu.LogicalPerPhysical+s, buf) != 0 {
					clear(buf)
				}
			}
		}

		// The disc header and each partition's first sector are plaintext;
		// the rest of a partition is decrypted and stored as such.
		plaintext := p < wiiu.HeaderPhysicalSectors
		for i := range parts {
			if p == uint64(parts[i].startSector) {
				plaintext = true
				break
			}
		}

		status := aaru.SectorStatusDumped
		if !plaintext {
			for i := range regions {
				if p > uint64(regions[i].StartSector) && p < uint64(regions[i].EndSector) {
					_ = wiiu.DecryptPhysicalSector(regions[i].Key, phys)
					status = aaru.SectorStatusUnencrypted
					break
				}
			}
		}

		for s := uint64(0); s < wiiu.LogicalPerPhysical; s++ {
			logical := p*wiiu.LogicalPerPhysical + s
			buf := phys[s*wiiu.LogicalSectorSize : (s+1)*wiiu.LogicalSectorSize]
			if err := output.WriteSector(logical, false, buf, status); err != nil {
				o.printf("\n")
				o.errorf("Error writing logical sector %d (error %d)", logical, aaru.ErrorCode(err))
				result = -1
				break sectors
			}
		}

		bytesProcessed += wiiu.PhysicalSectorSize
	}

	o.printf("\n\n")

	if result == 0 {
		o.successf("Converted %d physical sectors (%s) in %.1f seconds", totalPhysical,
			conFormatBytes(bytesProcessed), time.Since(start).Seconds())
	}

	// ── Step 7: Extract metadata from the disc header ──
	if result == 0 {
		o.section("Extracting Metadata")

		header := make([]byte, wiiu.LogicalSectorSize)
		if wud != nil {
			if n, _ := wud.ReadAt(header, 0); n >= len(header) {
				wiiuDiscHeaderMetadata(o, header, output)
			}
		} else if conReadSector(input, 0, header) == 0 {
			wiiuDiscHeaderMetadata(o, header, output)
		}
	}

	// ── Step 8: Copy metadata from the source AaruFormat image ──
	if result == 0 && input != nil {
		o.section("Copying Source Metadata")
		conCopySourceMetadata(o, input, output, func(t aaru.MediaTagType) bool {
			return t == aaru.MediaTagWiiUDiscKey || t == aaru.MediaTagWiiUPartitionKeyMap
		})
	}

	result = conCloseOutput(o, output, result)
	closeInputs()
	return o.finish(result)
}

// wiiuOpenReader opens a WUD or WUX image, printing the reader's own
// diagnostics. The caller closes the returned file.
func wiiuOpenReader(o *conOut, path string) (*wiiu.Reader, *os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		o.flush()
		fmt.Fprintf(o.ew, "Error: cannot open %s\n", path)
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if st.Size() <= 0 {
		o.flush()
		fmt.Fprintf(o.ew, "Error: cannot determine file size for %s\n", path)
	}
	r, err := wiiu.NewReader(f, st.Size())
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return r, f, nil
}
