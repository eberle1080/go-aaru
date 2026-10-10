package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/eberle1080/go-aaru"
)

// ANSI escape sequences used by info and convert.
const (
	coreAnsiReset  = "\033[0m"
	coreAnsiBold   = "\033[1m"
	coreAnsiCyan   = "\033[36m"
	coreAnsiYellow = "\033[33m"
	coreAnsiWhite  = "\033[37m"
	coreAnsiBlue   = "\033[34m"
	coreAnsiGreen  = "\033[32m"
	coreAnsiRed    = "\033[31m"
)

// Box color pairs of info.
const (
	infoColorHeader = 1
	infoColorBox    = 4
)

// infoMaxListed is how many tracks, media tags and sector tags info lists.
const infoMaxListed = 20

// infoFiletimeEpochDiff is the FILETIME of 1970-01-01 00:00:00 UTC.
const infoFiletimeEpochDiff = 116444736000000000

// cmdInfo implements "info <filename>".
func cmdInfo(args []string) int {
	pos, ok := newVerbArgs("info", usageInfo).parse(args, 1, 1)
	if !ok {
		return -1
	}
	colors := coreTerminalSupportsColor(os.Stdout)
	return withCoreOutput(func(o *coreOutput) int { return info(o, pos[0], colors) })
}

// infoPrinter draws the boxed sections of info.
type infoPrinter struct {
	o      *coreOutput
	colors bool
}

// boxColor selects the color of a box border.
func (p *infoPrinter) boxColor(colorPair int) {
	if !p.colors {
		return
	}
	switch colorPair {
	case infoColorHeader:
		p.o.print(coreAnsiCyan + coreAnsiBold)
	case infoColorBox:
		p.o.print(coreAnsiBlue + coreAnsiBold)
	default:
		p.o.print(coreAnsiBold)
	}
}

// boxTop draws the top border of a box with its title.
func (p *infoPrinter) boxTop(title string, colorPair int) {
	p.boxColor(colorPair)
	p.o.printf("┌─ %s ", title)
	if padding := 80 - 5 - len(title) - 1; padding > 0 {
		p.o.print(strings.Repeat("─", padding))
	}
	p.o.print("┐\n")
	if p.colors {
		p.o.print(coreAnsiReset)
	}
}

// boxBottom draws the bottom border of a box.
func (p *infoPrinter) boxBottom(colorPair int) {
	p.boxColor(colorPair)
	p.o.print("└" + strings.Repeat("─", 77) + "┘\n\n")
	if p.colors {
		p.o.print(coreAnsiReset)
	}
}

// field prints a label and its value inside a box. Widths count
// bytes.
func (p *infoPrinter) field(label, value string, labelWidth int) {
	if p.colors {
		p.o.print(coreAnsiBlue + "│ " + coreAnsiReset)
		p.o.print(coreAnsiYellow + corePadBytes(label, labelWidth) + coreAnsiReset)
		p.o.print(" ")
		p.o.print(coreAnsiWhite + corePadBytes(value, 76-labelWidth-1) + coreAnsiReset)
		p.o.print(coreAnsiBlue + "│\n" + coreAnsiReset)
		return
	}
	line := coreTruncateUTF8(corePadBytes(label, labelWidth)+" "+value, 77)
	p.o.print("│ " + corePadBytes(line, 76) + " │\n")
}

// infoFormatFiletime formats a FILETIME as local "YYYY-MM-DD HH:MM:SS", or as a
// number if it predates the Unix epoch.
func infoFormatFiletime(ft int64) string {
	u := uint64(ft)
	if u < infoFiletimeEpochDiff {
		return fmt.Sprintf("%d", u)
	}
	secs := (u - infoFiletimeEpochDiff) / 10000000
	if secs > math.MaxInt64 {
		return fmt.Sprintf("%d", u)
	}
	return time.Unix(int64(secs), 0).Local().Format("2006-01-02 15:04:05")
}

// infoMetadataFields are the metadata strings info prints, in order.
var infoMetadataFields = []struct {
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

// infoHasMetadataString reports whether the image stores a non-empty metadata
// string f.
func infoHasMetadataString(img *aaru.Image, f aaru.MetadataField) bool {
	raw, err := img.MetadataRaw(f)
	return err == nil && len(raw) > 0
}

// info prints everything known about an image.
func info(o *coreOutput, path string, colors bool) int {
	p := &infoPrinter{o: o, colors: colors}

	img, err := aaru.Open(path)
	if err != nil {
		code := coreOpenErrno(err)
		if colors {
			o.printf(coreAnsiRed+"\n❌ Error: Cannot open AaruFormat image (error code: %d)\n\n"+coreAnsiReset, code)
		} else {
			o.printf("\n❌ Error: Cannot open AaruFormat image (error code: %d)\n\n", code)
		}
		if colors {
			o.print(coreAnsiReset)
		}
		return code
	}
	defer img.Close()

	hdr := img.Header()
	ii := img.Info()

	o.print("\n")
	if colors {
		o.print(coreAnsiGreen + coreAnsiBold)
	}
	o.print("================================================================================\n")
	o.print("                        AARUFORMAT IMAGE INFORMATION\n")
	o.print("================================================================================\n\n")
	if colors {
		o.print(coreAnsiReset)
	}

	// Image format.
	p.boxTop("IMAGE FORMAT", infoColorBox)
	// ctx->magic is always AARU_MAGIC once an image is open.
	p.field("Magic:", infoIdentifierString(aaru.AaruMagic), 20)
	p.field("Format Version:", fmt.Sprintf("%d.%d", hdr.ImageMajorVersion, hdr.ImageMinorVersion), 20)
	p.field("Library Version:", fmt.Sprintf("%d.%d", aaru.LibraryMajorVersion, aaru.LibraryMinorVersion), 20)
	p.field("Identifier:", infoIdentifierString(hdr.Identifier), 20)
	p.boxBottom(infoColorBox)

	// Creator application.
	p.boxTop("CREATOR APPLICATION", infoColorHeader)
	p.field("Application:", ii.Application, 20)
	p.field("Version:", fmt.Sprintf("%d.%d", hdr.ApplicationMajorVersion, hdr.ApplicationMinorVersion), 20)
	p.field("Created:", infoFormatFiletime(hdr.CreationTime), 20)
	p.field("Last Modified:", infoFormatFiletime(hdr.LastWrittenTime), 20)
	p.boxBottom(infoColorHeader)

	// Media information.
	p.boxTop("MEDIA INFORMATION", infoColorHeader)
	p.field("Media Type:", mediaTypeString(img.MediaType()), 20)
	if c, h, s, err := img.Geometry(); err == nil {
		p.field("Geometry:", fmt.Sprintf("C:%d / H:%d / S:%d", c, h, s), 20)
	}
	p.field("Image Size:", fmt.Sprintf("%d bytes", ii.ImageSize), 20)
	p.field("Total Sectors:", fmt.Sprintf("%d", ii.Sectors), 20)
	p.field("Sector Size:", fmt.Sprintf("%d bytes", ii.SectorSize), 20)
	p.field("Has Partitions:", infoYesNo(ii.HasPartitions), 20)
	p.field("Has Sessions:", infoYesNo(ii.HasSessions), 20)
	p.boxBottom(infoColorHeader)

	if ddt, ok := img.UserDataDDTHeader(); ok {
		infoDDT2(p, ddt)
	}

	infoMetadata(p, img)

	if tracks, err := img.Tracks(); err == nil && len(tracks) > 0 {
		p.boxTop(fmt.Sprintf("TRACKS (%d total)", len(tracks)), infoColorHeader)
		for i, t := range tracks {
			if i >= infoMaxListed {
				break
			}
			p.field("", fmt.Sprintf("Track #%-2d: Seq=%d Type=%d Start=%d End=%d Session=%d", i+1, t.Sequence,
				t.Type, t.Start, t.End, t.Session), 0)
		}
		if len(tracks) > infoMaxListed {
			p.field("", fmt.Sprintf("... and %d more track(s)", len(tracks)-infoMaxListed), 0)
		}
		p.boxBottom(infoColorHeader)
	}

	if hw, err := img.DumpHardware(); err == nil && len(hw) > 0 {
		p.boxTop(fmt.Sprintf("DUMP HARDWARE (%d device(s))", len(hw)), infoColorHeader)
		for i, d := range hw {
			p.field(fmt.Sprintf("Device #%d:", i+1), "", 20)
			if d.Manufacturer != "" {
				p.field("  Manufacturer:", d.Manufacturer, 20)
			}
			if d.Model != "" {
				p.field("  Model:", d.Model, 20)
			}
			if d.SoftwareName != "" {
				p.field("  Software:", d.SoftwareName, 20)
			}
			switch n := len(d.Extents); {
			case n > 0 && n <= 3:
				for _, e := range d.Extents {
					p.field("  Extent:", fmt.Sprintf("%d - %d", e.Start, e.End), 20)
				}
			case n > 3:
				p.field("  Extents:", fmt.Sprintf("%d extent(s)", n), 20)
			}
		}
		p.boxBottom(infoColorHeader)
	}

	infoFlux(o, img)

	if cs := img.Checksums(); cs.MD5 != nil || cs.SHA1 != nil || cs.SHA256 != nil || cs.SpamSum != "" {
		p.boxTop("CHECKSUMS", infoColorHeader)
		if cs.MD5 != nil {
			p.field("MD5:", byteArrayToHexString(cs.MD5), 20)
		}
		if cs.SHA1 != nil {
			p.field("SHA-1:", byteArrayToHexString(cs.SHA1), 20)
		}
		if cs.SHA256 != nil {
			p.field("SHA-256:", byteArrayToHexString(cs.SHA256), 20)
		}
		if cs.SpamSum != "" {
			p.field("SpamSum:", cs.SpamSum, 20)
		}
		p.boxBottom(infoColorHeader)
	}

	if tags := img.ReadableMediaTags(); len(tags) > 0 {
		p.boxTop(fmt.Sprintf("MEDIA TAGS (%d total)", len(tags)), infoColorHeader)
		for i, t := range tags {
			if i >= infoMaxListed {
				break
			}
			data, _ := img.ReadMediaTag(t)
			p.field("", fmt.Sprintf("%s (%d bytes)", mediaTagTypeString(t), len(data)), 0)
		}
		if len(tags) > infoMaxListed {
			p.field("", fmt.Sprintf("... and %d more tag(s)", len(tags)-infoMaxListed), 0)
		}
		p.boxBottom(infoColorHeader)
	}

	// The readable sector tags box is always printed.
	p.boxTop("READABLE SECTOR TAGS", infoColorHeader)
	if tags := img.ReadableSectorTags(); len(tags) > 0 {
		for i, t := range tags {
			p.field("", sectorTagTypeString(t), 0)
			if i+1 >= infoMaxListed {
				if len(tags) > infoMaxListed {
					p.field("", fmt.Sprintf("... and %d more tag(s)", len(tags)-infoMaxListed), 0)
				}
				break
			}
		}
	} else {
		p.field("", "(No readable sector tags)", 0)
	}
	p.boxBottom(infoColorHeader)

	if ec, ok := img.ErasureCoding(); ok {
		p.boxTop("ERASURE CODING RECOVERY", infoColorHeader)
		algo := "Reed-Solomon (Vandermonde)"
		if ec.Algorithm == aaru.ErasureCodingXor {
			algo = "XOR"
		}
		p.field("Algorithm", algo, 0)
		p.field("Data blocks per stripe (K)", fmt.Sprintf("%d", ec.K), 0)
		p.field("Parity blocks per stripe (M)", fmt.Sprintf("%d", ec.M), 0)
		p.field("Shard size", fmt.Sprintf("%d bytes", ec.ShardSize), 0)
		p.field("Data stripes", fmt.Sprintf("%d", ec.DataStripes), 0)
		overhead := 0.0
		if ec.K > 0 {
			overhead = float64(ec.M) / float64(ec.K) * 100.0
		}
		p.field("Parity overhead", fmt.Sprintf("%.1f%% (before compression)", overhead), 0)
		p.field("Fault tolerance", fmt.Sprintf("Tolerates up to %d corrupted block(s) per stripe", ec.M), 0)
		p.boxBottom(infoColorHeader)
	}

	o.print("================================================================================\n\n")
	if colors {
		o.print(coreAnsiReset)
	}
	return 0
}

// infoIdentifierString renders an 8-byte little-endian identifier as text,
// right-aligned in 8 characters and cut at the first NUL byte.
func infoIdentifierString(id uint64) string {
	var b [8]byte
	n := 0
	for i := range b {
		c := byte(id >> (8 * i))
		if c == 0 {
			break
		}
		b[i] = c
		n++
	}
	return fmt.Sprintf("%8s", string(b[:n]))
}

// infoYesNo formats a boolean as info does.
func infoYesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

// infoDDT2 prints the user data DDT v2 header section.
func infoDDT2(p *infoPrinter, h aaru.DDTHeader2) {
	p.boxTop("DDT2 USER DATA HEADER", infoColorHeader)

	dataType := fmt.Sprintf("%d", h.Type)
	if h.Type == aaru.DataTypeUserData {
		dataType = "UserData"
	}
	p.field("Data Type:", dataType, 25)

	var compression string
	switch h.Compression {
	case aaru.CompressionNone:
		compression = "None"
	case aaru.CompressionLzma:
		compression = "LZMA"
	case aaru.CompressionFlac:
		compression = "FLAC"
	case aaru.CompressionLzmaCst:
		compression = "LZMA+CST"
	default:
		compression = fmt.Sprintf("%d", h.Compression)
	}
	p.field("Compression:", compression, 25)

	p.field("Hierarchy Levels:", fmt.Sprintf("%d (current level: %d)", h.Levels, h.TableLevel), 25)
	if h.PreviousLevelOffset > 0 {
		p.field("Previous Level Offset:", fmt.Sprintf("0x%X", h.PreviousLevelOffset), 25)
	}
	p.field("Total Blocks:", fmt.Sprintf("%d", h.Blocks), 25)
	p.field("Negative Sectors:", fmt.Sprintf("%d", h.Negative), 25)
	p.field("Overflow Sectors:", fmt.Sprintf("%d", h.Overflow), 25)
	p.field("User Sectors:", fmt.Sprintf("%d", h.Blocks-uint64(h.Negative)-uint64(h.Overflow)), 25)
	if h.Start > 0 {
		p.field("Start Index:", fmt.Sprintf("%d", h.Start), 25)
	}
	p.field("Block Alignment Shift:", fmt.Sprintf("%d (2^%d = %d bytes)", h.BlockAlignmentShift,
		h.BlockAlignmentShift, uint32(1)<<h.BlockAlignmentShift), 25)
	p.field("Data Shift:", fmt.Sprintf("%d (2^%d = %d sectors/block)", h.DataShift, h.DataShift,
		uint32(1)<<h.DataShift), 25)
	if h.TableShift > 0 {
		p.field("Table Shift:", fmt.Sprintf("%d (2^%d = %d sectors/entry)", h.TableShift, h.TableShift,
			uint32(1)<<h.TableShift), 25)
	} else {
		p.field("Table Shift:", "0 (single-level)", 25)
	}
	p.field("Entries:", fmt.Sprintf("%d", h.Entries), 25)
	p.field("Table Size:", fmt.Sprintf("%d bytes (compressed: %d bytes)", h.Length, h.CmpLength), 25)
	p.field("CRC64 (uncompressed):", fmt.Sprintf("0x%016X", h.CRC64), 25)
	p.field("CRC64 (compressed):", fmt.Sprintf("0x%016X", h.CmpCRC64), 25)

	p.boxBottom(infoColorHeader)
}

// infoMetadata prints the metadata section if the image has a media sequence
// or any metadata string.
func infoMetadata(p *infoPrinter, img *aaru.Image) {
	seq, lastSeq, seqErr := img.MediaSequence()
	hasSequence := seqErr == nil && seq > 0
	has := hasSequence
	for _, f := range infoMetadataFields {
		has = has || infoHasMetadataString(img, f.field)
	}
	if !has {
		return
	}

	p.boxTop("METADATA", infoColorHeader)
	if hasSequence {
		p.field("Media Sequence:", fmt.Sprintf("%d of %d", seq, lastSeq), 20)
	}
	for _, f := range infoMetadataFields {
		if !infoHasMetadataString(img, f.field) {
			continue
		}
		if s, err := img.MetadataString(f.field); err == nil {
			p.field(f.label, s, 20)
		}
	}
	p.boxBottom(infoColorHeader)
}

// infoFlux prints the flux capture summary (outside any box).
func infoFlux(o *coreOutput, img *aaru.Image) {
	captures, err := img.FluxCaptures()
	if err != nil || len(captures) == 0 {
		return
	}
	o.printf("Image contains %d flux captures.\n", len(captures))
	o.printf("Flux capture details:\n")

	minIndex, maxIndex := uint64(math.MaxUint64), uint64(0)
	minData, maxData := uint64(math.MaxUint64), uint64(0)
	tracks := map[uint16]bool{}
	heads := map[uint32]bool{}
	for _, c := range captures {
		minIndex = min(minIndex, c.IndexResolution)
		maxIndex = max(maxIndex, c.IndexResolution)
		minData = min(minData, c.DataResolution)
		maxData = max(maxData, c.DataResolution)
		tracks[c.Track] = true
		heads[c.Head] = true
	}

	o.printf("\tStatistics:\n")
	o.printf("\t\tUnique heads: %d\n", len(heads))
	o.printf("\t\tUnique tracks: %d\n", len(tracks))
	if minIndex != math.MaxUint64 {
		o.printf("\t\tIndex resolution: %d - %d picoseconds (%.3f - %.3f nanoseconds)\n", minIndex, maxIndex,
			float64(minIndex)/1000.0, float64(maxIndex)/1000.0)
	}
	if minData != math.MaxUint64 {
		o.printf("\t\tData resolution: %d - %d picoseconds (%.3f - %.3f nanoseconds)\n", minData, maxData,
			float64(minData)/1000.0, float64(maxData)/1000.0)
	}

	o.printf("\tCaptures:\n")
	for i, c := range captures {
		o.printf("\t\tCapture %d:\n", i+1)
		o.printf("\t\t\tHead: %d\n", c.Head)
		o.printf("\t\t\tTrack: %d\n", c.Track)
		o.printf("\t\t\tSubtrack: %d\n", c.Subtrack)
		o.printf("\t\t\tCapture index: %d\n", c.CaptureIndex)
		o.printf("\t\t\tIndex resolution: %d picoseconds (%.3f nanoseconds)\n", c.IndexResolution,
			float64(c.IndexResolution)/1000.0)
		o.printf("\t\t\tData resolution: %d picoseconds (%.3f nanoseconds)\n", c.DataResolution,
			float64(c.DataResolution)/1000.0)
	}
}
