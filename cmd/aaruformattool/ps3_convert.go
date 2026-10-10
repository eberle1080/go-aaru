package main

// convert-ps3: converts PS3 disc ISO or AaruFormat images to AaruFormat with
// decrypted sector storage and the PS3 media tags/metadata.

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/iso9660"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/console/ps3/ird"
	"github.com/eberle1080/go-aaru/console/ps3/sfo"
	"github.com/eberle1080/go-aaru/media"
)

// cmdConvertPS3 implements the convert-ps3 verb.
func cmdConvertPS3(args []string) int {
	va := newVerbArgs("convert-ps3", usageConvertPS3)
	va.fs.String("disc-key", "", "32-char hex disc key")
	va.fs.String("data1-key", "", "32-char hex data1 key")
	va.fs.String("ird", "", "Path to IRD file")
	pos, ok := va.parse(args, 2, 2)
	if !ok {
		return -1
	}
	o := newConOut(os.Stdout, os.Stderr)
	defer o.flush()
	return convertPS3(o, pos[0], pos[1], conOptString(va, "disc-key"), conOptString(va, "data1-key"),
		conOptString(va, "ird"))
}

// usageConvertPS3 prints the help of convert-ps3.
func usageConvertPS3() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool convert-ps3 <input> <output> [options]\n\n")
	fmt.Printf("Converts a PS3 disc ISO or AaruFormat image to an AaruFormat image with\n")
	fmt.Printf("decrypted sector storage and PS3 encryption metadata.\n\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <input>              Path to input ISO or AaruFormat image.\n")
	fmt.Printf("  <output>             Path to output AaruFormat image.\n\n")
	fmt.Printf("Options:\n")
	fmt.Printf("  --disc-key=<hex>     32-char hex disc key (16 bytes).\n")
	fmt.Printf("  --data1-key=<hex>    32-char hex data1 key (disc key will be derived).\n")
	fmt.Printf("  --ird=<path>         Path to IRD file (extracts keys, PIC, metadata).\n\n")
	fmt.Printf("Key resolution order:\n")
	fmt.Printf("  1. --disc-key argument\n")
	fmt.Printf("  2. --data1-key argument (derives disc key)\n")
	fmt.Printf("  3. --ird file (extracts data1 key, derives disc key)\n")
	fmt.Printf("  4. Sidecar files: <input>.disc_key, <input>.data1, <input>.ird\n")
	fmt.Printf("  5. Source AaruFormat image media tags\n")
}

// ps3EncMapErrorCode maps an encryption map parse error to a numeric status
// code.
func ps3EncMapErrorCode(err error) int {
	switch {
	case errors.Is(err, ps3.ErrTruncated):
		return -2
	case errors.Is(err, ps3.ErrTooManyRegions):
		return -3
	case errors.Is(err, ps3.ErrInvalidRegion):
		return -5
	}
	return -1
}

// convertPS3 converts the PS3 disc image inputPath (raw ISO or AaruFormat) to
// the AaruFormat image outputPath. The optional key arguments
// are nil when not given. It returns 0 on success and -1 on failure.
func convertPS3(o *conOut, inputPath, outputPath string, discKeyHex, data1KeyHex, irdPath *string) int {
	var (
		discKey, data1Key      [16]byte
		haveDiscKey, haveData1 bool
		irdFile                *ird.File
	)
	defer func() {
		clear(discKey[:])
		clear(data1Key[:])
	}()

	o.banner("                        PS3 DISC IMAGE CONVERTER")

	// ── Step 1: Resolve key material ──
	o.section("Key Resolution")

	if discKeyHex != nil {
		if conParseHexKey(*discKeyHex, &discKey) {
			haveDiscKey = true
			o.successf("Disc key provided via --disc-key")
		} else {
			o.errorf("Invalid --disc-key format (expected 32 hex chars)")
		}
	}

	if !haveDiscKey && data1KeyHex != nil {
		if conParseHexKey(*data1KeyHex, &data1Key) {
			haveData1 = true
			discKey = ps3.DeriveDiscKey(data1Key)
			haveDiscKey = true
			o.successf("Disc key derived from --data1-key")
		} else {
			o.errorf("Invalid --data1-key format (expected 32 hex chars)")
		}
	}

	// useIRD adopts a parsed IRD, deriving the disc key from its data1 key if
	// none is known yet.
	useIRD := func(f *ird.File, loaded, derived string) {
		irdFile = f
		o.successf("%s", loaded)
		if !haveDiscKey {
			data1Key = f.Data1
			haveData1 = true
			discKey = ps3.DeriveDiscKey(data1Key)
			haveDiscKey = true
			o.successf("%s", derived)
		}
	}

	if irdPath != nil {
		if f, err := ird.ParseFile(*irdPath); err == nil {
			useIRD(f, fmt.Sprintf("IRD loaded: %s — %s (v%d)", f.GameID, f.GameName, f.Version),
				"Disc key derived from IRD data1")
		} else {
			o.errorf("Cannot parse IRD file")
		}
	}

	if !haveDiscKey {
		if k, ok := conTrySidecar(inputPath, ".disc_key", 16); ok {
			copy(discKey[:], k)
			haveDiscKey = true
			o.successf("Disc key loaded from sidecar .disc_key file")
		}
	}

	if !haveDiscKey {
		if k, ok := conTrySidecar(inputPath, ".data1", 16); ok {
			copy(data1Key[:], k)
			haveData1 = true
			discKey = ps3.DeriveDiscKey(data1Key)
			haveDiscKey = true
			o.successf("Disc key derived from sidecar .data1 file")
		}
	}

	if !haveDiscKey || irdFile == nil {
		// Sidecar IRD: "input.iso.ird", then "input.ird".
		candidates := []string{inputPath + ".ird"}
		if alt, ok := conReplaceExt(inputPath, ".ird"); ok {
			candidates = append(candidates, alt)
		}
		for _, p := range candidates {
			if irdFile != nil {
				break
			}
			if f, err := ird.ParseFile(p); err == nil {
				useIRD(f, fmt.Sprintf("Sidecar IRD loaded: %s — %s", f.GameID, f.GameName),
					"Disc key derived from sidecar IRD data1")
			}
		}
	}

	// ── Step 2: Open source image ──
	o.section("Opening Source Image")

	var (
		input        *aaru.Image
		iso          *os.File
		totalSectors uint64
		mediaType    = media.PS3BD
	)
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
		mediaType = img.Info().MediaType

		if mediaType != media.PS3DVD && mediaType != media.PS3BD {
			o.errorf("Source image media type is %s, expected PS3DVD or PS3BD", conMediaTypeString(mediaType))
			closeInputs()
			return -1
		}

		totalSectors = img.Info().Sectors
		o.successf("AaruFormat image: %s, %d sectors", conMediaTypeString(mediaType), totalSectors)

		// Keys from the source media tags.
		if !haveDiscKey {
			if tag, err := img.ReadMediaTag(aaru.MediaTagPs3DiscKey); err == nil && len(tag) <= 16 {
				copy(discKey[:], tag)
				haveDiscKey = true
				o.successf("Disc key loaded from source image media tags")
			}
		}
		if !haveDiscKey {
			if tag, err := img.ReadMediaTag(aaru.MediaTagPs3Data1); err == nil && len(tag) <= 16 {
				copy(data1Key[:], tag)
				haveData1 = true
				discKey = ps3.DeriveDiscKey(data1Key)
				haveDiscKey = true
				o.successf("Disc key derived from source image data1 media tag")
			}
		}
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			o.errorf("Cannot open input file: %s", conStrerror(err))
			return -1
		}
		iso = f

		var fileSize int64
		if st, err := f.Stat(); err == nil {
			fileSize = st.Size()
		}
		if fileSize <= 0 || fileSize%conSectorSize != 0 {
			o.errorf("Invalid ISO file size (%d bytes, not a multiple of %d)", fileSize, conSectorSize)
			closeInputs()
			return -1
		}

		totalSectors = uint64(fileSize) / conSectorSize
		mediaType = media.PS3BD

		if totalSectors >= 2 {
			sector1 := make([]byte, conSectorSize)
			if n, _ := iso.ReadAt(sector1, conSectorSize); n == conSectorSize {
				if string(sector1[:12]) != "PlayStation3" {
					o.errorf("Sector 1 does not contain PlayStation 3 identification.")
					closeInputs()
					return -1
				}
				o.successf("PlayStation 3 identification found in sector 1")
			}
		}

		o.successf("Raw ISO image: %d sectors (%.2f GiB)", totalSectors, float64(fileSize)/(1024.0*1024.0*1024.0))
		o.info("Media type:", "PS3BD (default for ISO)")
	}

	if !haveDiscKey {
		o.errorf("No disc key found. Provide --disc-key, --data1-key, --ird, or place sidecar files.")
		closeInputs()
		return -1
	}

	// readInput reads one source sector into buf (2048 bytes), returning a
	// message describing the failure.
	readInput := func(sector uint64, buf []byte) (string, bool) {
		if iso != nil {
			if sector >= totalSectors {
				return fmt.Sprintf("Error reading ISO sector %d", sector), false
			}
			if n, _ := iso.ReadAt(buf, int64(sector)*conSectorSize); n != conSectorSize {
				return fmt.Sprintf("Error reading ISO sector %d", sector), false
			}
			return "", true
		}
		if code := conReadSector(input, sector, buf); code != 0 {
			return fmt.Sprintf("Error reading sector %d (error %d)", sector, code), false
		}
		return "", true
	}

	// ── Step 3: Read sector 0 and parse the encryption map ──
	o.section("Parsing Encryption Map")

	sector0 := make([]byte, conSectorSize)
	if iso != nil {
		if n, _ := iso.ReadAt(sector0, 0); n != conSectorSize {
			o.errorf("Cannot read sector 0 from ISO")
			closeInputs()
			return -1
		}
	} else if code := conReadSector(input, 0, sector0); code != 0 {
		o.errorf("Cannot read sector 0 from AaruFormat image (error %d)", code)
		closeInputs()
		return -1
	}

	encMap, err := ps3.ParseEncryptionMap(sector0)
	if err != nil {
		o.errorf("Cannot parse encryption map from sector 0 (error %d)", ps3EncMapErrorCode(err))
		closeInputs()
		return -1
	}

	o.successf("Encryption map: %d plaintext region(s)", len(encMap))
	for i, r := range encMap {
		o.printf("  %s\n", fmt.Sprintf("  Region %d: sectors %d–%d", i, r.StartSector, r.EndSector))
	}

	encMapData, _ := encMap.MarshalBinary()

	// ── Step 4: Create the destination image ──
	o.section("Creating Destination Image")

	output, err := conCreateOutput(outputPath, mediaType, totalSectors)
	if err != nil {
		o.errorf("Cannot create output image: %s", conStrerror(err))
		closeInputs()
		return -1
	}
	o.successf("Destination image created")

	// Media tags go first so that the write path can find the key and map.
	_ = output.WriteMediaTag(aaru.MediaTagPs3DiscKey, discKey[:])
	o.successf("Written media tag: PS3 Disc Key")

	if haveData1 {
		_ = output.WriteMediaTag(aaru.MediaTagPs3Data1, data1Key[:])
		o.successf("Written media tag: PS3 Data1")
	}

	if irdFile != nil {
		_ = output.WriteMediaTag(aaru.MediaTagPs3Data2, irdFile.Data2[:])
		o.successf("Written media tag: PS3 Data2")

		if irdFile.HasPIC {
			_ = output.WriteMediaTag(aaru.MediaTagPs3Pic, irdFile.PIC[:])
			o.successf("Written media tag: PS3 PIC")
		}
	}

	if encMapData != nil {
		_ = output.WriteMediaTag(aaru.MediaTagPs3EncryptionMap, encMapData)
		o.successf("Written media tag: PS3 Encryption Map")
	}

	// ── Step 5: Copy sectors ──
	o.section("Converting Sectors")
	o.printf("\n")

	result := 0
	sectorData := make([]byte, conSectorSize)
	start := time.Now()
	var bytesProcessed uint64

	for sector := uint64(0); sector < totalSectors; sector++ {
		if sector%1000 == 0 || sector == totalSectors-1 {
			o.progress(float64(sector+1)/float64(totalSectors)*100.0, bytesProcessed, start)
		}

		if msg, ok := readInput(sector, sectorData); !ok {
			o.printf("\n")
			o.errorf("%s", msg)
			result = -1
			break
		}

		// Encrypted on disc: stored decrypted; plaintext regions: as is.
		status := aaru.SectorStatusDumped
		if encMap.IsSectorEncrypted(sector) {
			status = aaru.SectorStatusUnencrypted
		}

		if err := output.WriteSector(sector, false, sectorData, status); err != nil {
			o.printf("\n")
			o.errorf("Error writing sector %d (error %d)", sector, aaru.ErrorCode(err))
			result = -1
			break
		}

		bytesProcessed += conSectorSize
	}

	o.printf("\n\n")

	if result == 0 {
		o.successf("Converted %d sectors (%s) in %.1f seconds", totalSectors, conFormatBytes(bytesProcessed),
			time.Since(start).Seconds())
	}

	// ── Step 6: Extract and write metadata ──
	if result == 0 {
		o.section("Extracting Metadata")

		readSector := func(lba uint64, buf []byte) error {
			if _, ok := readInput(lba, buf); !ok {
				return errors.New("cannot read sector")
			}
			return nil
		}

		sfoData, err := iso9660.ReadFile(readSector, "/PS3_GAME/PARAM.SFO")
		switch {
		case err != nil || len(sfoData) == 0:
			o.warn("PARAM.SFO not found in filesystem")
		default:
			if s, err := sfo.Parse(sfoData); err == nil {
				title, haveTitle := s.Get("TITLE")
				titleID, haveTitleID := s.Get("TITLE_ID")

				if haveTitle {
					o.info("Title:", title)
					_ = output.SetMediaTitle(title)
				}
				if haveTitleID {
					o.info("Title ID:", titleID)
					_ = output.SetMediaPartNumber(titleID)
				}

				if haveTitle || haveTitleID {
					o.successf("Metadata extracted from PARAM.SFO")
				} else {
					o.warn("PARAM.SFO found but no mappable fields")
				}
			} else {
				o.warn("Cannot parse PARAM.SFO")
			}
		}

		if irdFile != nil && irdFile.GameName != "" {
			o.info("IRD Game Name:", irdFile.GameName)
			o.info("IRD Game ID:", irdFile.GameID)
		}
	}

	// ── Step 7: Copy metadata from the source AaruFormat image ──
	//
	// Only the PS3 media tags written above are skipped, so tags that were
	// not written (data1 when the disc key came from elsewhere, data2 and PIC
	// without an IRD) are still copied from the source.
	if result == 0 && input != nil {
		o.section("Copying Source Metadata")
		conCopySourceMetadata(o, input, output, func(t aaru.MediaTagType) bool {
			switch t {
			case aaru.MediaTagPs3DiscKey, aaru.MediaTagPs3EncryptionMap:
				return true
			case aaru.MediaTagPs3Data1:
				return haveData1
			case aaru.MediaTagPs3Data2:
				return irdFile != nil
			case aaru.MediaTagPs3Pic:
				return irdFile != nil && irdFile.HasPIC
			}
			return false
		})
	}

	result = conCloseOutput(o, output, result)
	closeInputs()
	return o.finish(result)
}
