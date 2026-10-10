package main

// convert-ngcw: converts Nintendo GameCube / Wii disc ISO or AaruFormat
// images to AaruFormat, storing Wii partition data decrypted and replacing
// the pseudorandom (LFG) junk padding by a junk map.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/ngcw"
	"github.com/eberle1080/go-aaru/console/wii"
	"github.com/eberle1080/go-aaru/media"
)

// GameCube / Wii disc constants.
const (
	ngcwGCMagic          = 0xC2339F3D // NGC_GC_MAGIC, at 0x1C
	ngcwWiiMagic         = 0x5D1C9EA3 // NGC_WII_MAGIC, at 0x18
	ngcwBlockSize        = 0x8000     // GC_BLOCK_SIZE
	ngcwSectorsPerBlock  = 16         // SECTORS_PER_BLOCK
	ngcwHeaderSize       = 0x440      // disc header (boot.bin) size
	ngcwPartTableInfo    = 0x40000    // Wii partition table info
	ngcwTicketSize       = 0x2A4
	ngcwOutsideSysEnd    = 0x50000 // system area assumed outside Wii partitions
	ngcwMaxFSTSize       = 64 * 1024 * 1024
	ngcwProgressInterval = 0x7FFFF
)

// Wii common keys (index 0: common, index 1: Korean).
var (
	ngcwWiiCommonKey = [16]byte{0xEB, 0xE4, 0x2A, 0x22, 0x5E, 0x85, 0x93, 0xE4,
		0x48, 0xD9, 0xC5, 0x45, 0x73, 0x81, 0xAA, 0xF7}
	ngcwWiiKoreanKey = [16]byte{0x63, 0xB8, 0x2B, 0xB4, 0xF4, 0x61, 0x4E, 0x2E,
		0x13, 0xF2, 0xFE, 0xFB, 0xBA, 0x4C, 0x9B, 0x7E}
)

// ngcwPartition is a Wii partition (NgcwPartition).
type ngcwPartition struct {
	offset     uint64
	dataOffset uint64
	dataSize   uint64
	typ        uint32
	titleKey   [16]byte
}

// ngcwDataRegion is a file extent from the FST (NgcwDataRegion).
type ngcwDataRegion struct{ offset, length uint64 }

// ngcwDataMap lists the file extents of a disc or partition, sorted by
// offset (NgcwDataMap).
type ngcwDataMap []ngcwDataRegion

// isData reports whether [offset, offset+length) overlaps a file, using a
// binary search.
func (m ngcwDataMap) isData(offset, length uint64) bool {
	lo, hi := 0, len(m)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		end := m[mid].offset + m[mid].length
		switch {
		case end <= offset:
			lo = mid + 1
		case m[mid].offset >= offset+length:
			hi = mid - 1
		default:
			return true
		}
	}
	return false
}

// ngcwBuildDataMap collects the file extents of an FST.
// File offsets are shifted left by shift (2 on Wii) and rebased on dataStart.
func ngcwBuildDataMap(fst []byte, dataStart uint64, shift uint) ngcwDataMap {
	if len(fst) < 12 {
		return nil
	}
	total := uint64(binary.BigEndian.Uint32(fst[8:]))
	if total*12 > uint64(len(fst)) {
		return nil
	}
	var m ngcwDataMap
	for i := uint64(1); i < total; i++ {
		e := fst[i*12 : i*12+12]
		if e[0] != 0 { // directory
			continue
		}
		off := uint64(binary.BigEndian.Uint32(e[4:])) << shift
		length := uint64(binary.BigEndian.Uint32(e[8:]))
		if length == 0 {
			continue
		}
		m = append(m, ngcwDataRegion{offset: dataStart + off, length: length})
	}
	sort.SliceStable(m, func(i, j int) bool { return m[i].offset < m[j].offset })
	return m
}

// ngcwJunkCollector accumulates junk regions, merging contiguous ones with
// the same partition and seed (JunkCollector). It also holds the scratch
// generator used to verify junk.
type ngcwJunkCollector struct {
	entries  ngcw.JunkMap
	lfg      ngcw.LFG
	expected [conSectorSize]byte
}

func (jc *ngcwJunkCollector) add(offset, length uint64, partition uint16, seed ngcw.Seed) {
	if n := len(jc.entries); n > 0 {
		last := &jc.entries[n-1]
		if last.PartitionIndex == partition && last.Offset+last.Length == offset && last.Seed == seed {
			last.Length += length
			return
		}
	}
	jc.entries = append(jc.entries, ngcw.JunkEntry{Offset: offset, Length: length, PartitionIndex: partition, Seed: seed})
}

// lfgMatches reports whether data (at most one sector) equals the LFG
// stream of seed starting at stream position pos.
func (jc *ngcwJunkCollector) lfgMatches(seed ngcw.Seed, pos uint64, data []byte) bool {
	jc.lfg.Reset(seed)
	jc.lfg.Discard(pos)
	expected := jc.expected[:len(data)]
	_, _ = jc.lfg.Read(expected)
	return bytes.Equal(expected, data)
}

// ngcwJunkCounters counts the sectors classified as data and as junk.
type ngcwJunkCounters struct{ data, junk uint64 }

// ngcwDetectJunkInBlock classifies the sectors of a 0x8000-byte (or shorter,
// at the end of the disc) block at disc offset blockOff as data
// (SectorStatusDumped) or LFG junk (SectorStatusGenerable, recorded in jc),
// writing one status per sector to statuses.
//
// Sectors below sysEnd are data. Others are data if they overlap a file of
// dataMap; without a map (nil) every sector is data. Junk sectors must be
// reproduced exactly by the LFG seed recovered from the whole block or from
// their run of non-data sectors.
func ngcwDetectJunkInBlock(block []byte, blockOff uint64, dataMap *ngcwDataMap, sysEnd uint64, partition uint16,
	jc *ngcwJunkCollector, cnt *ngcwJunkCounters, statuses []aaru.SectorStatus) {
	numSectors := (len(block) + conSectorSize - 1) / conSectorSize
	sectorLen := func(si int) int { return min(conSectorSize, len(block)-si*conSectorSize) }

	isData := make([]bool, numSectors)
	for si := range isData {
		off := blockOff + uint64(si)*conSectorSize
		switch {
		case off < sysEnd:
			isData[si] = true
		case dataMap != nil:
			isData[si] = dataMap.isData(off, uint64(sectorLen(si)))
		default:
			isData[si] = true
		}
	}

	// Whole block generated by one seed.
	if len(block) >= ngcw.MinSeedData {
		if seed, matched := ngcw.FindSeed(block, 0); matched >= len(block) {
			for si := 0; si < numSectors; si++ {
				if isData[si] {
					statuses[si] = aaru.SectorStatusDumped
					cnt.data++
				} else {
					statuses[si] = aaru.SectorStatusGenerable
					jc.add(blockOff+uint64(si)*conSectorSize, uint64(sectorLen(si)), partition, seed)
					cnt.junk++
				}
			}
			return
		}
	}

	// Mixed block: one seed per run of non-data sectors.
	for si := 0; si < numSectors; {
		if isData[si] {
			statuses[si] = aaru.SectorStatusDumped
			cnt.data++
			si++
			continue
		}

		runStart := si
		for si < numSectors && !isData[si] {
			si++
		}
		runEnd := si
		runByteStart := runStart * conSectorSize
		runByteEnd := min(runEnd*conSectorSize, len(block))

		var runSeed ngcw.Seed
		runHasSeed := false
		if runByteEnd-runByteStart >= ngcw.MinSeedData {
			seed, matched := ngcw.FindSeed(block[runByteStart:runByteEnd], uint64(runByteStart))
			if matched >= runByteEnd-runByteStart {
				runSeed, runHasSeed = seed, true
			}
		}

		for ri := runStart; ri < runEnd; ri++ {
			s := ri * conSectorSize
			sector := block[s : s+sectorLen(ri)]
			if runHasSeed && jc.lfgMatches(runSeed, uint64(s), sector) {
				statuses[ri] = aaru.SectorStatusGenerable
				jc.add(blockOff+uint64(s), uint64(len(sector)), partition, runSeed)
				cnt.junk++
			} else {
				// No seed, or not junk: kept as data (zero fill deduplicates).
				statuses[ri] = aaru.SectorStatusDumped
				cnt.data++
			}
		}
	}
}

// cmdConvertNGCW implements the convert-ngcw verb.
func cmdConvertNGCW(args []string) int {
	va := newVerbArgs("convert-ngcw", usageConvertNGCW)
	pos, ok := va.parse(args, 2, 2)
	if !ok {
		return -1
	}
	o := newConOut(os.Stdout, os.Stderr)
	defer o.flush()
	return convertNGCW(o, pos[0], pos[1])
}

// usageConvertNGCW prints the help of convert-ngcw.
func usageConvertNGCW() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool convert-ngcw <input> <output>\n\n")
	fmt.Printf("Converts a Nintendo GameCube or Wii disc ISO or AaruFormat image to an\n")
	fmt.Printf("AaruFormat image with decrypted sectors (Wii) and junk regions removed.\n\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <input>              Path to input ISO or AaruFormat image.\n")
	fmt.Printf("  <output>             Path to output AaruFormat image.\n\n")
	fmt.Printf("The disc type (GameCube or Wii) is auto-detected from the disc header.\n")
	fmt.Printf("Wii common keys are hardcoded; no key arguments are needed.\n")
	fmt.Printf("A .bca sidecar file will be imported if found alongside the input.\n")
}

// ngcwDetectDiscType returns 0 for GameCube, 1 for Wii and -1 for neither.
func ngcwDetectDiscType(header []byte) int {
	if binary.BigEndian.Uint32(header[0x18:]) == ngcwWiiMagic {
		return 1
	}
	if binary.BigEndian.Uint32(header[0x1C:]) == ngcwGCMagic {
		return 0
	}
	return -1
}

func ngcwPlatform(discType int) string {
	if discType == 0 {
		return "Nintendo GameCube"
	}
	return "Nintendo Wii"
}

// ngcwReadFull reads len(buf) bytes at off, failing on a short read.
func ngcwReadFull(f *os.File, buf []byte, off uint64) bool {
	n, _ := f.ReadAt(buf, int64(off))
	return n == len(buf)
}

// ngcwReadPadded reads len(buf) bytes at off, zero-filling what cannot be
// read.
func ngcwReadPadded(f *os.File, buf []byte, off uint64) {
	n, _ := f.ReadAt(buf, int64(off))
	clear(buf[n:])
}

// ngcwParseWiiPartitions reads the Wii partition tables and the tickets and
// headers of all partitions, decrypting their title keys.
func ngcwParseWiiPartitions(o *conOut, iso *os.File, fileSize uint64) ([]ngcwPartition, bool) {
	var info [32]byte
	if !ngcwReadFull(iso, info[:], ngcwPartTableInfo) {
		o.errorf("Cannot read partition table info")
		return nil, false
	}

	var counts, offsets [4]uint32
	var total uint16
	for t := range counts {
		counts[t] = binary.BigEndian.Uint32(info[t*8:])
		offsets[t] = binary.BigEndian.Uint32(info[t*8+4:])
		total += uint16(counts[t])
	}
	if total == 0 {
		return nil, true
	}

	parts := make([]ngcwPartition, 0, total)
	for t := range counts {
		if counts[t] == 0 {
			continue
		}

		tableOffset := uint64(offsets[t]) << 2
		tableSize := uint64(counts[t] * 8) // 32-bit product
		if tableOffset+tableSize > fileSize {
			return nil, false
		}
		table := make([]byte, tableSize)
		if !ngcwReadFull(iso, table, tableOffset) {
			return nil, false
		}

		for p := uint64(0); p < uint64(counts[t]) && len(parts) < int(total); p++ {
			if p*8+8 > tableSize {
				return nil, false
			}
			partOffset := uint64(binary.BigEndian.Uint32(table[p*8:])) << 2
			part := ngcwPartition{offset: partOffset, typ: binary.BigEndian.Uint32(table[p*8+4:])}

			ticket := make([]byte, ngcwTicketSize)
			if !ngcwReadFull(iso, ticket, partOffset) {
				return nil, false
			}

			// Title key: AES-128-CBC with the common key selected by the
			// ticket, IV = title ID followed by zeros.
			commonKey := ngcwWiiCommonKey
			if ticket[0x1F1] == 1 {
				commonKey = ngcwWiiKoreanKey
			}
			var iv [16]byte
			copy(iv[:8], ticket[0x1BF+0x1D:])
			block, _ := aes.NewCipher(commonKey[:])
			cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(part.titleKey[:], ticket[0x1BF:0x1BF+16])

			var phdr [8]byte
			if !ngcwReadFull(iso, phdr[:], partOffset+0x2B8) {
				return nil, false
			}
			part.dataOffset = partOffset + uint64(binary.BigEndian.Uint32(phdr[:]))<<2
			part.dataSize = uint64(binary.BigEndian.Uint32(phdr[4:])) << 2

			parts = append(parts, part)
		}
	}
	return parts, true
}

// ngcwPartitionDataMap reads the boot block and FST of a Wii partition and
// returns its system area end (FST end, in partition user data bytes) and
// file map.
func ngcwPartitionDataMap(iso *os.File, p *ngcwPartition) (sysEnd uint64, m ngcwDataMap) {
	group := make([]byte, wii.GroupSize)
	if !ngcwReadFull(iso, group, p.dataOffset) {
		return 0, nil
	}
	_ = wii.DecryptGroup(p.titleKey, group)
	data := group[wii.HashSize:]

	fstOffset := binary.BigEndian.Uint32(data[0x424:]) << 2 // 32-bit shifts
	fstSize := binary.BigEndian.Uint32(data[0x428:]) << 2
	sysEnd = uint64(fstOffset) + uint64(fstSize)

	if fstSize == 0 || fstSize >= ngcwMaxFSTSize {
		return sysEnd, nil
	}

	fst := make([]byte, fstSize)
	for read := uint64(0); read < uint64(fstSize); {
		logical := uint64(fstOffset) + read
		grpOff := logical % wii.DataSize
		if !ngcwReadFull(iso, group, p.dataOffset+(logical/wii.DataSize)*wii.GroupSize) {
			return sysEnd, nil
		}
		_ = wii.DecryptGroup(p.titleKey, group)
		read += uint64(copy(fst[read:], group[wii.HashSize+grpOff:]))
	}
	return sysEnd, ngcwBuildDataMap(fst, 0, 2)
}

// ngcwImportBCA imports a 64-byte BCA sidecar ("<input>.bca", or the input
// name with its extension replaced by .bca) as the DVD BCA media tag.
func ngcwImportBCA(inputPath string, output *aaru.Image) bool {
	open64 := func(p string) *os.File {
		if st, err := os.Stat(p); err != nil || st.Size() != 64 {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		return f
	}

	f := open64(inputPath + ".bca")
	if f == nil {
		alt := inputPath + ".bca"
		if dot := strings.LastIndexByte(inputPath, '.'); dot >= 0 {
			alt = inputPath[:dot] + ".bca"
		}
		if f = open64(alt); f == nil {
			return false
		}
	}
	defer f.Close()

	var bca [64]byte
	if _, err := io.ReadFull(f, bca[:]); err != nil {
		return false
	}
	_ = output.WriteMediaTag(aaru.MediaTagDvdBca, bca[:])
	return true
}

// ngcwTitle returns the game title of a disc header: 64 bytes at 0x20,
// trailing spaces and NULs removed, up to the first NUL.
func ngcwTitle(header []byte) string {
	t := header[0x20:0x60]
	for len(t) > 0 && (t[len(t)-1] == ' ' || t[len(t)-1] == 0) {
		t = t[:len(t)-1]
	}
	return conNulString(t)
}

// ngcwRegion names the region of a disc from its game ID's region letter.
func ngcwRegion(c byte) string {
	switch c {
	case 'J':
		return "Japan"
	case 'E':
		return "USA"
	case 'P', 'D', 'F', 'I', 'S', 'H', 'U':
		return "Europe"
	case 'K':
		return "Korea"
	case 'W':
		return "Taiwan"
	}
	return "Unknown"
}

// ngcwExtractMetadata stores the title, game ID, disc number and a comment
// with platform, maker and version from the disc header.
func ngcwExtractMetadata(o *conOut, output *aaru.Image, header []byte, discType int) {
	if title := ngcwTitle(header); title != "" {
		o.info("Title:", title)
		_ = output.SetMediaTitle(title)
	}

	gameID := conNulString(header[:6])
	o.info("Game ID:", gameID)
	_ = output.SetMediaPartNumber(gameID)

	if n := header[6]; n > 0 {
		o.info("Disc Number:", fmt.Sprintf("%d", int(n)+1))
		_ = output.SetMediaSequence(int32(n)+1, int32(n)+1)
	}

	_ = output.SetComments(fmt.Sprintf("Platform: %s, Maker: %s, Version: %d", ngcwPlatform(discType),
		string(header[4:6]), header[7]))
}

// convertNGCW converts the GameCube or Wii disc image inputPath (ISO or
// AaruFormat) to the AaruFormat image outputPath. It returns 0 on
// success and -1 on failure.
func convertNGCW(o *conOut, inputPath, outputPath string) int {
	header := make([]byte, ngcwHeaderSize)

	o.banner("                  GAMECUBE / WII DISC IMAGE CONVERTER")

	// ── Step 1: Open and identify the source ──
	o.section("Opening Source Image")

	var (
		input    *aaru.Image
		iso      *os.File
		discSize uint64
		parts    []ngcwPartition
	)
	defer func() {
		for i := range parts {
			clear(parts[i].titleKey[:])
		}
	}()
	closeInputs := func() {
		if input != nil {
			_ = input.Close()
		}
		if iso != nil {
			_ = iso.Close()
		}
	}

	if img, err := aaru.Open(inputPath); err == nil {
		input = img
		if mt := img.Info().MediaType; mt != media.GOD && mt != media.WOD {
			o.errorf("Source image media type is %s, expected GOD or WOD", conMediaTypeString(mt))
			closeInputs()
			return -1
		}

		discSize = img.Info().Sectors * uint64(img.Info().SectorSize)

		// The header is the start of sector 0.
		sector0 := make([]byte, conSectorSize)
		if conReadSector(img, 0, sector0) != 0 {
			o.errorf("Cannot read disc header from AaruFormat source")
			closeInputs()
			return -1
		}
		copy(header, sector0)

		o.successf("AaruFormat image: %d sectors", img.Info().Sectors)
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			o.errorf("Cannot open input file: %s", conStrerror(err))
			return -1
		}
		iso = f

		st, err := f.Stat()
		if err != nil {
			o.errorf("Cannot stat input file")
			closeInputs()
			return -1
		}
		discSize = uint64(st.Size())

		if n, _ := f.ReadAt(header, 0); n < ngcwHeaderSize {
			o.errorf("Cannot read disc header")
			closeInputs()
			return -1
		}

		o.successf("ISO image: %s", conFormatBytes(discSize))
	}

	discType := ngcwDetectDiscType(header)
	if discType < 0 {
		o.errorf("Not a valid GameCube or Wii disc image")
		closeInputs()
		return -1
	}

	o.info("Disc Type:", ngcwPlatform(discType))

	// ── Step 2: Parse the partitions (Wii only) ──
	if discType == 1 && iso != nil {
		o.section("Parsing Wii Partitions")

		var ok bool
		if parts, ok = ngcwParseWiiPartitions(o, iso, discSize); !ok {
			o.errorf("Cannot parse Wii partitions")
			closeInputs()
			return -1
		}

		o.successf("Found %d partition(s)", len(parts))
		for i, p := range parts {
			ptype := "unknown"
			switch p.typ {
			case 0:
				ptype = "game"
			case 1:
				ptype = "update"
			case 2:
				ptype = "channel"
			}
			o.printf("  %s\n", fmt.Sprintf("  Partition %d: %s  offset=0x%X  data_size=%.1f MiB", i, ptype, p.offset,
				float64(p.dataSize)/(1024.0*1024.0)))
		}
	} else if discType == 1 {
		o.section("Wii Partition Parsing")
		o.successf("Wii partition parsing from AaruFormat source (partition key map required in source)")
	}

	// ── Disc information ──
	o.section("Disc Information")
	o.info("Platform:", ngcwPlatform(discType))
	o.info("Game ID:", conNulString(header[:6]))
	if title := ngcwTitle(header); title != "" {
		o.info("Title:", title)
	}
	o.info("Maker Code:", conNulString(header[4:6]))
	o.info("Disc Number:", fmt.Sprintf("%d", int(header[6])+1))
	o.info("Disc Version:", fmt.Sprintf("%d", header[7]))
	o.info("Region:", ngcwRegion(header[3]))
	o.info("Size:", conFormatBytes(discSize))

	// ── Step 3: Create the destination image ──
	o.section("Creating Destination Image")

	mediaType := media.GOD
	if discType == 1 {
		mediaType = media.WOD
	}

	output, err := conCreateOutput(outputPath, mediaType, discSize/conSectorSize)
	if err != nil {
		o.errorf("Cannot create output image: %s", conStrerror(err))
		closeInputs()
		return o.finish(-1)
	}
	o.successf("Destination image created")

	// ── Step 4: Partition key map (Wii) ──
	if discType == 1 && len(parts) > 0 {
		o.section("Building Partition Key Map")

		sort.SliceStable(parts, func(i, j int) bool { return parts[i].dataOffset < parts[j].dataOffset })

		regions := make(wii.PartitionKeyMap, len(parts))
		for i, p := range parts {
			regions[i].StartSector = uint32(p.dataOffset / wii.GroupSize)
			regions[i].EndSector = uint32((p.dataOffset + p.dataSize) / wii.GroupSize)
			regions[i].Key = p.titleKey
		}
		if keyMap, err := regions.MarshalBinary(); err == nil {
			_ = output.WriteMediaTag(aaru.MediaTagWiiPartitionKeyMap, keyMap)
			o.successf("Written media tag: Wii Partition Key Map")
		}
		for i := range regions {
			clear(regions[i].Key[:])
		}
	}

	// ── Step 5: Convert sectors ──
	o.section("Converting Sectors")
	o.printf("\n")

	var (
		jc             ngcwJunkCollector
		cnt            ngcwJunkCounters
		bytesProcessed uint64
		result         int
	)
	start := time.Now()

	// readBlock reads len(buf) bytes at disc offset off, from the ISO or from
	// the sectors of the AaruFormat source (unreadable data reads as zeros).
	sectorBuf := make([]byte, conSectorSize)
	readBlock := func(buf []byte, off uint64) {
		if iso != nil {
			ngcwReadPadded(iso, buf, off)
			return
		}
		base := off / conSectorSize
		for s := 0; s < ngcwSectorsPerBlock && s*conSectorSize < len(buf); s++ {
			sector := buf[s*conSectorSize : min((s+1)*conSectorSize, len(buf))]
			clear(sectorBuf)
			if conReadSector(input, base+uint64(s), sectorBuf) != 0 {
				clear(sectorBuf)
			}
			copy(sector, sectorBuf)
		}
	}

	// writeSectors writes the whole sectors of a block with their statuses.
	writeSectors := func(block []byte, off uint64, statuses []aaru.SectorStatus) bool {
		for si := 0; si < len(block)/conSectorSize; si++ {
			sector := off/conSectorSize + uint64(si)
			if err := output.WriteSector(sector, false, block[si*conSectorSize:(si+1)*conSectorSize], statuses[si]); err != nil {
				o.printf("\n")
				o.errorf("Error writing sector %d (error %d)", sector, aaru.ErrorCode(err))
				return false
			}
		}
		return true
	}

	progress := func(off uint64) {
		if off&ngcwProgressInterval == 0 {
			o.progress(float64(off)/float64(discSize)*100.0, bytesProcessed, start)
		}
	}

	block := make([]byte, ngcwBlockSize)
	statuses := make([]aaru.SectorStatus, ngcwSectorsPerBlock)

	if discType == 0 {
		// ---- GameCube ----
		fstOffset := binary.BigEndian.Uint32(header[0x424:])
		fstSize := binary.BigEndian.Uint32(header[0x428:])
		sysEnd := uint64(fstOffset + fstSize) // 32-bit sum

		var dataMap ngcwDataMap
		if fstSize > 0 && fstSize < ngcwMaxFSTSize {
			fst := make([]byte, fstSize)
			fstOK := false
			if iso != nil {
				fstOK = ngcwReadFull(iso, fst, uint64(fstOffset))
			} else {
				fstOK = true
				sbuf := make([]byte, conSectorSize)
				for read := uint64(0); read < uint64(fstSize); {
					pos := uint64(fstOffset) + read
					if conReadSector(input, pos/conSectorSize, sbuf) != 0 {
						fstOK = false
						break
					}
					read += uint64(copy(fst[read:], sbuf[pos%conSectorSize:]))
				}
			}
			if fstOK {
				dataMap = ngcwBuildDataMap(fst, 0, 0)
			}
		}

		for off := uint64(0); off < discSize; off += ngcwBlockSize {
			progress(off)

			b := block[:min(uint64(ngcwBlockSize), discSize-off)]
			readBlock(b, off)

			ngcwDetectJunkInBlock(b, off, &dataMap, sysEnd, ngcw.NoPartition, &jc, &cnt, statuses)

			if !writeSectors(b, off, statuses) {
				result = -1
				break
			}
			bytesProcessed += uint64(len(b))
		}
	} else {
		// ---- Wii ----
		partSysEnd := make([]uint64, len(parts))
		partMaps := make([]ngcwDataMap, len(parts))
		if iso != nil {
			for p := range parts {
				partSysEnd[p], partMaps[p] = ngcwPartitionDataMap(iso, &parts[p])
			}
		}

		group := make([]byte, wii.GroupSize)
		out := make([]byte, wii.GroupSize)

		for off := uint64(0); off < discSize; {
			progress(off)

			inPart := -1
			for p := range parts {
				if off >= parts[p].dataOffset && off < parts[p].dataOffset+parts[p].dataSize {
					inPart = p
					break
				}
			}

			if inPart >= 0 && iso != nil {
				// Inside a partition: decrypt the group, zero its junk
				// (recorded in the junk map) and store it decrypted.
				p := &parts[inPart]
				groupNum := (off - p.dataOffset) / wii.GroupSize
				groupDiscOff := p.dataOffset + groupNum*wii.GroupSize

				ngcwReadPadded(iso, group, groupDiscOff)
				_ = wii.DecryptGroup(p.titleKey, group)
				data := group[wii.HashSize:]
				logicalOffset := groupNum * wii.DataSize

				const chunks = (wii.DataSize + conSectorSize - 1) / conSectorSize
				var isData [chunks]bool
				for s := range isData {
					coff := uint64(s) * conSectorSize
					clen := min(uint64(wii.DataSize)-coff, conSectorSize)
					switch {
					case logicalOffset+coff < partSysEnd[inPart]:
						isData[s] = true
					case len(partMaps[inPart]) > 0:
						isData[s] = partMaps[inPart].isData(logicalOffset+coff, clen)
					default:
						isData[s] = true
					}
				}

				// The user data of a group spans up to two 0x8000-byte LFG
				// blocks of the partition's data stream; find a seed for each.
				blockPhase := logicalOffset % wii.GroupSize
				block2Start := uint64(wii.DataSize)
				if blockPhase > 0 {
					block2Start = min(wii.GroupSize-blockPhase, wii.DataSize)
				}

				var seeds [2]ngcw.Seed
				var haveSeed [2]bool
				for s := range isData {
					if isData[s] {
						continue
					}
					soff := uint64(s) * conSectorSize
					b := 0
					if soff >= block2Start {
						b = 1
					}
					if haveSeed[b] {
						continue
					}
					if uint64(wii.DataSize)-soff < ngcw.MinSeedData {
						continue
					}
					if seed, m := ngcw.FindSeed(data[soff:], (logicalOffset+soff)%wii.GroupSize); m > 0 {
						seeds[b], haveSeed[b] = seed, true
					}
					if haveSeed[0] && haveSeed[1] {
						break
					}
				}

				copy(out, group[:wii.HashSize])
				for s := range isData {
					coff := uint64(s) * conSectorSize
					chunk := data[coff : coff+min(uint64(wii.DataSize)-coff, conSectorSize)]
					dst := out[wii.HashSize+coff:]

					b := 0
					if coff >= block2Start {
						b = 1
					}
					if !isData[s] && haveSeed[b] &&
						jc.lfgMatches(seeds[b], (logicalOffset+coff)%wii.GroupSize, chunk) {
						clear(dst[:len(chunk)])
						jc.add(groupDiscOff+wii.HashSize+coff, uint64(len(chunk)), uint16(inPart), seeds[b])
						cnt.junk++
						continue
					}
					copy(dst, chunk)
					cnt.data++
				}

				// Junk sectors do not align with the 2048-byte AaruFormat
				// sectors (the hash block shifts them by 1 KiB), so the whole
				// group is stored decrypted and the junk is rebuilt from the
				// junk map when reading.
				for s := range statuses {
					statuses[s] = aaru.SectorStatusUnencrypted
				}
				if !writeSectors(out, groupDiscOff, statuses) {
					result = -1
					break
				}

				off = groupDiscOff + wii.GroupSize
				bytesProcessed += wii.GroupSize
				continue
			}

			// Outside the partitions: stored as is.
			alignedOff := off &^ (ngcwBlockSize - 1)
			b := block[:min(uint64(ngcwBlockSize), discSize-alignedOff)]
			readBlock(b, alignedOff)

			ngcwDetectJunkInBlock(b, alignedOff, nil, ngcwOutsideSysEnd, ngcw.NoPartition, &jc, &cnt, statuses)

			if !writeSectors(b, alignedOff, statuses) {
				result = -1
				break
			}

			off = alignedOff + uint64(len(b))
			bytesProcessed += uint64(len(b))
		}
	}

	o.printf("\n\n")

	if result == 0 {
		o.successf("Converted %d sectors (%s) in %.1f seconds — %d data, %d junk", cnt.data+cnt.junk,
			conFormatBytes(bytesProcessed), time.Since(start).Seconds(), cnt.data, cnt.junk)
	}

	// ── Step 6: Store the junk map ──
	if result == 0 && len(jc.entries) > 0 {
		o.section("Storing Junk Map")

		if junkMap, err := jc.entries.MarshalBinary(); err == nil {
			_ = output.WriteMediaTag(aaru.MediaTagNgcwJunkMap, junkMap)
			o.successf("Stored junk map with %d entries (%d bytes)", len(jc.entries), len(junkMap))
		}
	}

	// ── Step 7: Import a BCA sidecar ──
	if result == 0 && ngcwImportBCA(inputPath, output) {
		o.section("BCA Sidecar")
		o.successf("Imported 64-byte BCA from sidecar file")
	}

	// ── Step 8: Extract metadata ──
	if result == 0 {
		o.section("Extracting Metadata")
		ngcwExtractMetadata(o, output, header, discType)
		o.successf("Metadata extracted")
	}

	// ── Step 9: Copy metadata from the source AaruFormat image ──
	if result == 0 && input != nil {
		o.section("Copying Source Metadata")
		conCopySourceMetadata(o, input, output, func(t aaru.MediaTagType) bool {
			return t == aaru.MediaTagWiiPartitionKeyMap || t == aaru.MediaTagNgcwJunkMap
		})
	}

	result = conCloseOutput(o, output, result)
	closeInputs()
	return o.finish(result)
}
