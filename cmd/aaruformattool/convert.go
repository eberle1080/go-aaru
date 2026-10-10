package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// convertProgressBarWidth is the width of the progress bar.
const convertProgressBarWidth = 40

// cmdConvert implements "convert [-l] [--erasure-coding=K,M] <input>
// <output>".
func cmdConvert(args []string) int {
	va := newVerbArgs("convert", usageConvert)
	useLong := va.fs.Bool("l", false, "Use long sector read/write (includes tags and metadata)")
	ecArg := va.fs.String("erasure-coding", "", "Enable erasure coding with K data and M parity blocks per stripe")
	pos, ok := va.parse(args, 2, 2)
	if !ok {
		return -1
	}

	ecSet := false
	va.fs.Visit(func(f *flag.Flag) { ecSet = ecSet || f.Name == "erasure-coding" })
	var ecK, ecM uint16
	if ecSet {
		k, m, ok := parseErasureCodingArg(*ecArg)
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: --erasure-coding must be K,M (e.g. 4,2)\n")
			return -1
		}
		ecK, ecM = k, m
	}
	return withCoreOutput(func(o *coreOutput) int { return convertImage(o, pos[0], pos[1], *useLong, ecK, ecM) })
}

// parseErasureCodingArg parses "K,M" with both values in 1..65535.
func parseErasureCodingArg(s string) (k, m uint16, ok bool) {
	var a, b uint16
	if n, err := fmt.Sscanf(s, "%d,%d", &a, &b); n != 2 || err != nil || a == 0 || b == 0 {
		return 0, 0, false
	}
	return a, b, true
}

// convertFormatBytes formats a byte count with binary units.
func convertFormatBytes(bytes uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	size := float64(bytes)
	unit := 0
	for size >= 1024.0 && unit < 5 {
		size /= 1024.0
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.2f %s", size, units[unit])
}

// convertFormatDuration formats seconds as "x.y sec", "m min s sec" or
// "h hr m min s sec".
func convertFormatDuration(seconds float64) string {
	secs := int(seconds)
	switch {
	case seconds < 60:
		return fmt.Sprintf("%.1f sec", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%d min %d sec", secs/60, secs%60)
	default:
		return fmt.Sprintf("%d hr %d min %d sec", secs/3600, (secs%3600)/60, secs%60)
	}
}

// convertUI prints the decorated output of convert.
type convertUI struct{ o *coreOutput }

func (u convertUI) separator() {
	u.o.print(coreAnsiBlue + strings.Repeat("─", 80) + coreAnsiReset + "\n")
}

func (u convertUI) header(title string) {
	u.o.printf("\n"+coreAnsiBold+coreAnsiCyan+"  %s"+coreAnsiReset+"\n", title)
	u.separator()
}

func (u convertUI) info(label, value string) {
	u.o.printf("  "+coreAnsiYellow+"%s"+coreAnsiReset+" %s\n", corePadBytes(label, 20), value)
}

func (u convertUI) success(msg string) { u.o.printf(coreAnsiGreen+"  ✓ %s"+coreAnsiReset+"\n", msg) }
func (u convertUI) failure(msg string) { u.o.printf(coreAnsiRed+"  ✗ %s"+coreAnsiReset+"\n", msg) }
func (u convertUI) warning(msg string) { u.o.printf(coreAnsiYellow+"  ⚠ %s"+coreAnsiReset+"\n", msg) }

// progress draws the progress bar.
func (u convertUI) progress(percentage, speed, elapsed, eta float64) {
	etaStr := "--:--"
	if eta > 0 && eta < 86400*365 {
		etaStr = convertFormatDuration(eta)
	}
	filled := int(percentage / 100.0 * convertProgressBarWidth)
	if filled > convertProgressBarWidth {
		filled = convertProgressBarWidth
	}
	var b strings.Builder
	b.WriteString("\r" + coreAnsiCyan + "  [")
	for i := 0; i < convertProgressBarWidth; i++ {
		switch {
		case i < filled:
			b.WriteString(coreAnsiGreen + "█")
		case i == filled:
			b.WriteString(coreAnsiYellow + "▓")
		default:
			b.WriteString(coreAnsiWhite + "░")
		}
	}
	fmt.Fprintf(&b, coreAnsiCyan+"] "+coreAnsiWhite+"%5.1f%%"+coreAnsiReset, percentage)
	fmt.Fprintf(&b, " │ %s/s │ %s │ ETA: %s   ", convertFormatBytes(uint64(speed)), convertFormatDuration(elapsed), etaStr)
	u.o.print(b.String())
	u.o.flush()
}

// convertMetadataFields are the metadata strings convert copies, with the
// labels it displays them with.
var convertMetadataFields = []struct {
	field aaru.MetadataField
	label string
}{
	{aaru.MetadataCreator, "Creator:"},
	{aaru.MetadataComments, "Comments:"},
	{aaru.MetadataMediaTitle, "Media Title:"},
	{aaru.MetadataMediaManufacturer, "Media Manufacturer:"},
	{aaru.MetadataMediaModel, "Media Model:"},
	{aaru.MetadataMediaSerialNumber, "Media Serial:"},
	{aaru.MetadataMediaBarcode, "Media Barcode:"},
	{aaru.MetadataMediaPartNumber, "Media Part Number:"},
	{aaru.MetadataDriveManufacturer, "Drive Manufacturer:"},
	{aaru.MetadataDriveModel, "Drive Model:"},
	{aaru.MetadataDriveSerialNumber, "Drive Serial:"},
	{aaru.MetadataDriveFirmwareRevision, "Drive Firmware:"},
}

// convertImage copies the user sectors (plain or long), tracks, geometry,
// dump hardware, Aaru JSON metadata, media tags, metadata strings and flux
// captures of input into a new image. With ecK and ecM non-zero the output
// gets Reed-Solomon erasure coding.
//
// Sectors that were never dumped are skipped (they stay "not dumped" in the
// output), and in long mode sectors that have no long form (media without
// long sectors, images without the needed tags) are copied as plain sectors.
func convertImage(o *coreOutput, inputPath, outputPath string, useLong bool, ecK, ecM uint16) int {
	u := convertUI{o}

	o.print("\n")
	o.print(coreAnsiBold + coreAnsiCyan + strings.Repeat("═", 80) + "\n")
	o.print("                           AARUFORMAT IMAGE CONVERTER\n")
	o.print(strings.Repeat("═", 80) + coreAnsiReset + "\n")

	u.header("Conversion Settings")
	u.info("Input file:", inputPath)
	u.info("Output file:", outputPath)
	if useLong {
		u.info("Mode:", "Long sectors (with metadata)")
	} else {
		u.info("Mode:", "Standard sectors")
	}

	u.header("Opening Source Image")
	in, err := aaru.Open(inputPath)
	if err != nil {
		code := coreOpenErrno(err)
		u.failure(fmt.Sprintf("Cannot open input image (error %d)", code))
		return code
	}
	inClosed := false
	defer func() {
		if !inClosed {
			in.Close()
		}
	}()
	u.success("Source image opened successfully")

	ii := in.Info()
	totalSectors := ii.Sectors
	sectorSize := ii.SectorSize

	u.header("Source Image Information")
	u.info("Media type:", mediaTypeString(in.MediaType()))
	u.info("Total sectors:", fmt.Sprintf("%d", totalSectors))
	u.info("Sector size:", fmt.Sprintf("%d bytes", sectorSize))
	totalSize := totalSectors * uint64(sectorSize)
	u.info("Total size:", convertFormatBytes(totalSize))
	if ii.Application != "" {
		u.info("Created by:", fmt.Sprintf("%s %s", ii.Application, ii.ApplicationVersion))
	}

	// Dump hardware.
	dumpHW, dumpHWErr := in.DumpHardwareRaw()
	hasDumpHW := dumpHWErr == nil && len(dumpHW) > 0
	var dumpHWEntries int
	if hasDumpHW {
		hw, _ := in.DumpHardware()
		dumpHWEntries = len(hw)
		u.header("Dump Hardware Information")
		for i, e := range hw {
			if len(hw) > 1 {
				o.printf("  "+coreAnsiBold+coreAnsiWhite+"%s"+coreAnsiReset+"\n", fmt.Sprintf("Hardware Set #%d", i+1))
			}
			if e.Manufacturer != "" {
				u.info("  Manufacturer:", e.Manufacturer)
			}
			if e.Model != "" {
				u.info("  Model:", e.Model)
			}
			if e.Revision != "" {
				u.info("  Revision:", e.Revision)
			}
			if e.Firmware != "" {
				u.info("  Firmware:", e.Firmware)
			}
			if e.Serial != "" {
				u.info("  Serial:", e.Serial)
			}
			if e.SoftwareName != "" {
				sw := e.SoftwareName
				if e.SoftwareVersion != "" {
					sw += " " + e.SoftwareVersion
				}
				u.info("  Software:", sw)
			}
			if e.SoftwareOperatingSystem != "" {
				u.info("  OS:", e.SoftwareOperatingSystem)
			}
			if len(e.Extents) > 0 {
				u.info("  Extents:", fmt.Sprintf("%d extent(s)", len(e.Extents)))
			}
		}
	}

	// Aaru JSON metadata.
	jsonData, jsonErr := in.AaruJSONMetadata()
	hasJSON := jsonErr == nil && len(jsonData) > 0
	if hasJSON {
		u.header("Aaru JSON Metadata")
		u.info("Size:", convertFormatBytes(uint64(len(jsonData))))
		u.success("Aaru JSON metadata present")
	}

	// Media sequence and metadata strings (copied raw, as UTF-16LE).
	seq, lastSeq, seqErr := in.MediaSequence()
	hasSequence := seqErr == nil && seq > 0
	hasBasicMetadata := hasSequence
	metaRaw := make([][]byte, len(convertMetadataFields))
	for i, f := range convertMetadataFields {
		if raw, err := in.MetadataRaw(f.field); err == nil && len(raw) > 0 {
			metaRaw[i] = raw
			hasBasicMetadata = true
		}
	}
	if hasBasicMetadata {
		u.header("Image Metadata")
		if hasSequence {
			u.info("Media Sequence:", fmt.Sprintf("%d of %d", seq, lastSeq))
		}
		for i, f := range convertMetadataFields {
			if metaRaw[i] != nil {
				u.info(f.label, aaru.DecodeUTF16(metaRaw[i]))
			}
		}
	}

	// Media tags.
	mediaTags := in.ReadableMediaTags()
	if len(mediaTags) > 0 {
		u.header("Media Tags")
		u.info("Count:", fmt.Sprintf("%d media tag(s) present", len(mediaTags)))
		o.print("\n")
		for _, t := range mediaTags {
			o.printf("    "+coreAnsiGreen+"•"+coreAnsiReset+" %s\n", mediaTagTypeString(t))
		}
	}

	// Geometry (block media only).
	var cylinders, heads, sectorsPerTrack uint32
	hasGeometry := false
	if ii.MetadataMediaType == media.BlockMedia {
		c, h, s, err := in.Geometry()
		if err == nil && (c != 0 || h != 0 || s != 0) {
			cylinders, heads, sectorsPerTrack, hasGeometry = c, h, s, true
			u.header("Geometry")
			u.info("Geometry:", fmt.Sprintf("%d cylinders, %d heads, %d sectors/track", c, h, s))
		}
	}

	// Destination image.
	u.header("Creating Destination Image")
	out, err := aaru.Create(outputPath, aaru.CreateParams{
		MediaType:               ii.MediaType,
		SectorSize:              sectorSize,
		UserSectors:             totalSectors,
		Application:             "aaruformattool",
		ApplicationMajorVersion: 1,
		ApplicationMinorVersion: 0,
	}, nil)
	if err != nil {
		code := coreOpenErrno(err)
		u.failure(fmt.Sprintf("Cannot create output image (error %d)", code))
		return code
	}
	outClosed := false
	defer func() {
		if !outClosed {
			out.Close()
		}
	}()
	u.success("Destination image created successfully")

	if ecK > 0 && ecM > 0 {
		if err := out.SetErasureCoding(aaru.ErasureCodingRsVandermonde, ecK, ecM); err != nil {
			code := aaru.ErrorCode(err)
			u.failure(fmt.Sprintf("Cannot enable erasure coding (error %d)", code))
			return code
		}
		u.info("Erasure coding:", fmt.Sprintf("RS(%d,%d) — %d data + %d parity blocks per stripe", ecK, ecM, ecK,
			ecM))
	}

	// Tracks.
	tracks, err := in.Tracks()
	switch {
	case err == nil:
		if err := out.SetTracks(tracks); err != nil {
			u.failure(fmt.Sprintf("Cannot set tracks on output image (error %d)", aaru.ErrorCode(err)))
			return aaru.ErrorCode(err)
		}
		u.success(fmt.Sprintf("Copied %d track(s)", len(tracks)))
	case !errors.Is(err, aaru.ErrTrackNotFound):
		u.failure(fmt.Sprintf("Cannot get tracks from input image (error %d)", aaru.ErrorCode(err)))
		return aaru.ErrorCode(err)
	}

	// Sectors.
	u.header("Converting Sectors")
	o.print("\n")
	start := time.Now()
	var bytesProcessed uint64
	lastError := 0
	for sector := uint64(0); sector < totalSectors; sector++ {
		if sector%100 == 0 || sector == totalSectors-1 {
			elapsed := time.Since(start).Seconds()
			percentage := float64(sector+1) / float64(totalSectors) * 100.0
			speed := 0.0
			if elapsed > 0 {
				speed = float64(bytesProcessed) / elapsed
			}
			remaining := 0.0
			if speed > 0 {
				remaining = float64(totalSize-bytesProcessed) / speed
			}
			u.progress(percentage, speed, elapsed, remaining)
		}

		data, status, long, err := convertReadSector(in, sector, useLong)
		if errors.Is(err, aaru.ErrSectorNotDumped) {
			continue
		}
		if err != nil {
			o.print("\n")
			u.failure(fmt.Sprintf("Error reading sector %d (error %d)", sector, aaru.ErrorCode(err)))
			lastError = aaru.ErrorCode(err)
			break
		}

		err = convertWriteSector(out, sector, data, status, long, sectorSize)
		if err != nil {
			o.print("\n")
			u.failure(fmt.Sprintf("Error writing sector %d (error %d)", sector, aaru.ErrorCode(err)))
			lastError = aaru.ErrorCode(err)
			break
		}
		bytesProcessed += uint64(len(data))
	}
	o.print("\n\n")

	// Tracks again, in case they changed while copying sectors.
	if tracks, err := in.Tracks(); err == nil {
		if err := out.SetTracks(tracks); err != nil {
			u.warning(fmt.Sprintf("Warning: Could not finalize tracks (error %d)", aaru.ErrorCode(err)))
		}
	}

	if hasGeometry {
		if err := out.SetGeometry(cylinders, heads, sectorsPerTrack); err == nil {
			u.success("Geometry copied to destination image")
		} else {
			u.warning(fmt.Sprintf("Warning: Could not copy geometry (error %d)", aaru.ErrorCode(err)))
		}
	}

	if hasDumpHW {
		if err := out.SetDumpHardwareRaw(dumpHW); err == nil {
			u.success(fmt.Sprintf("Dump hardware information copied (%d set(s))", dumpHWEntries))
		} else {
			u.warning(fmt.Sprintf("Warning: Could not copy dump hardware (error %d)", aaru.ErrorCode(err)))
		}
	}

	if hasJSON {
		if err := out.SetAaruJSONMetadata(jsonData); err == nil {
			u.success(fmt.Sprintf("Aaru JSON metadata copied (%s)", convertFormatBytes(uint64(len(jsonData)))))
		} else {
			u.warning(fmt.Sprintf("Warning: Could not copy Aaru JSON metadata (error %d)", aaru.ErrorCode(err)))
		}
	}

	if len(mediaTags) > 0 {
		copied, failed := 0, 0
		for _, t := range mediaTags {
			data, err := in.ReadMediaTag(t)
			if err != nil || len(data) == 0 {
				failed++
				continue
			}
			if err := out.WriteMediaTag(t, data); err != nil {
				failed++
				continue
			}
			copied++
		}
		if copied > 0 {
			u.success(fmt.Sprintf("Media tags copied (%d of %d)", copied, len(mediaTags)))
		}
		if failed > 0 {
			u.warning(fmt.Sprintf("Warning: %d media tag(s) could not be copied", failed))
		}
	}

	if hasBasicMetadata {
		copied, failed := 0, 0
		count := func(err error) {
			if err == nil {
				copied++
			} else {
				failed++
			}
		}
		if hasSequence {
			count(out.SetMediaSequence(seq, lastSeq))
		}
		for i, f := range convertMetadataFields {
			if metaRaw[i] != nil {
				count(out.SetMetadataRaw(f.field, metaRaw[i]))
			}
		}
		if copied > 0 {
			u.success(fmt.Sprintf("Basic metadata copied (%d field(s))", copied))
		}
		if failed > 0 {
			u.warning(fmt.Sprintf("Warning: %d metadata field(s) could not be copied", failed))
		}
	}

	totalElapsed := time.Since(start).Seconds()
	avgSpeed := 0.0
	if totalElapsed > 0 {
		avgSpeed = float64(bytesProcessed) / totalElapsed
	}
	u.header("Conversion Summary")
	u.info("Data processed:", convertFormatBytes(bytesProcessed))
	u.info("Time elapsed:", convertFormatDuration(totalElapsed))
	u.info("Average speed:", convertFormatBytes(uint64(avgSpeed))+"/sec")

	// Flux captures.
	captures, err := in.FluxCaptures()
	switch {
	case err == nil:
		o.printf("Copying %d flux captures...\n", len(captures))
		for _, c := range captures {
			index, data, err := in.ReadFluxCapture(c.FluxCaptureKey)
			if errors.Is(err, aaru.ErrFluxDataNotFound) {
				continue
			}
			if err != nil {
				o.printf("Error %d when reading flux capture (head=%d, track=%d, subtrack=%d, index=%d) from input image.\n",
					aaru.ErrorCode(err), c.Head, c.Track, c.Subtrack, c.CaptureIndex)
				return closeConvertOnError(in, out, &inClosed, &outClosed, aaru.ErrorCode(err))
			}
			if err := out.WriteFluxCapture(c.FluxCaptureKey, c.DataResolution, c.IndexResolution, data,
				index); err != nil {
				o.printf("Error %d when writing flux capture (head=%d, track=%d, subtrack=%d, index=%d) to output image.\n",
					aaru.ErrorCode(err), c.Head, c.Track, c.Subtrack, c.CaptureIndex)
				return closeConvertOnError(in, out, &inClosed, &outClosed, aaru.ErrorCode(err))
			}
		}
		o.printf("Successfully copied %d flux captures.\n", len(captures))
	case !errors.Is(err, aaru.ErrFluxDataNotFound):
		o.printf("\nError %d when getting flux captures from input image.\n", aaru.ErrorCode(err))
	}

	o.print("\n")
	inClosed = true
	in.Close()
	outClosed = true
	res := aaru.ErrorCode(out.Close())
	o.print("\n")

	if res == 0 && lastError == 0 {
		o.print(coreAnsiGreen + coreAnsiBold + "  " + strings.Repeat("═", 78) + "\n")
		o.print("                          ✓ CONVERSION COMPLETED SUCCESSFULLY\n")
		o.print("  " + strings.Repeat("═", 78) + coreAnsiReset + "\n\n")
		return 0
	}
	code := lastError
	if res != 0 {
		code = res
	}
	o.print(coreAnsiRed + coreAnsiBold + "  " + strings.Repeat("═", 78) + "\n")
	o.print("                             ✗ CONVERSION FAILED\n")
	o.print("  " + strings.Repeat("═", 78) + coreAnsiReset + "\n")
	u.failure(fmt.Sprintf("Conversion failed with error %d", code))
	o.print("\n")
	return code
}

// convertReadSector reads a sector for convert. In long mode it falls back
// to the plain sector when the medium has no long sectors and when only the CD
// prefix/suffix of the sector was not dumped (so that its user data is
// kept); long reports whether data is a long sector.
func convertReadSector(in *aaru.Image, sector uint64, useLong bool) (data []byte, status aaru.SectorStatus,
	long bool, err error) {
	if useLong {
		data, status, err = in.ReadSectorLong(sector, false)
		if !errors.Is(err, aaru.ErrIncorrectMediaType) && !errors.Is(err, aaru.ErrSectorNotDumped) {
			return data, status, true, err
		}
	}
	data, status, err = in.ReadSector(sector, false)
	return data, status, false, err
}

// convertWriteSector writes a sector read by convertReadSector. A "long"
// sector that is no bigger than the image's sector size is a plain sector
// (images without the tags needed to build long sectors); if the output
// rejects it as a long sector it is written as a plain one.
func convertWriteSector(out *aaru.Image, sector uint64, data []byte, status aaru.SectorStatus, long bool,
	sectorSize uint32) error {
	if !long {
		return out.WriteSector(sector, false, data, status)
	}
	err := out.WriteSectorLong(sector, false, data, status)
	if errors.Is(err, aaru.ErrIncorrectMediaType) ||
		(errors.Is(err, aaru.ErrIncorrectDataSize) && len(data) <= int(sectorSize)) {
		err = out.WriteSector(sector, false, data, status)
	}
	return err
}

// closeConvertOnError closes both images (finalizing the output) and
// returns code.
func closeConvertOnError(in, out *aaru.Image, inClosed, outClosed *bool, code int) int {
	*inClosed, *outClosed = true, true
	in.Close()
	out.Close()
	return code
}
