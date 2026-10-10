package main

// inject-media-tag appends a media tag (TOC, PFI, ATIP, ...)
// read from a file to an existing AaruFormat v2 image as an uncompressed data
// block, replacing any previous block of the same data type in the index, and
// writes a new index.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/crc64"
)

func usageInjectMediaTag() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool inject-media-tag <tag-type> <media-tag-file> <image-file>\n\n")
	fmt.Printf("Injects a media tag into an AaruFormat image.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <tag-type>       The type of media tag to inject.\n")
	fmt.Printf("  <media-tag-file> Path to the file containing the media tag data.\n")
	fmt.Printf("  <image-file>     Path to the AaruFormat image file.\n")
}

func cmdInjectMediaTag(args []string) int {
	va := newVerbArgs("inject-media-tag", usageInjectMediaTag)
	pos, ok := va.parse(args, 3, 3)
	if !ok {
		return -1
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	return injectMediaTag(out, os.Stderr, pos[0], pos[1], pos[2])
}

// injectTagNames maps the tag type names accepted on the command line to their
// values, in the order they are listed. The
// MiniDisc tags are listed although no data type exists for them, and the
// console disc keys/maps (76-84) are not listed.
var injectTagNames = []struct {
	name string
	tag  aaru.MediaTagType
}{
	{"MediaTagCdToc", aaru.MediaTagCdToc},
	{"MediaTagSessionInfo", aaru.MediaTagSessionInfo},
	{"MediaTagFullToc", aaru.MediaTagFullToc},
	{"MediaTagCdPma", aaru.MediaTagCdPma},
	{"MediaTagCdAtip", aaru.MediaTagCdAtip},
	{"MediaTagCdText", aaru.MediaTagCdText},
	{"MediaTagCdMcn", aaru.MediaTagCdMcn},
	{"MediaTagDvdPfi", aaru.MediaTagDvdPfi},
	{"MediaTagDvdCmi", aaru.MediaTagDvdCmi},
	{"MediaTagDvdDiscKey", aaru.MediaTagDvdDiscKey},
	{"MediaTagDvdBca", aaru.MediaTagDvdBca},
	{"MediaTagDvdDmi", aaru.MediaTagDvdDmi},
	{"MediaTagDvdMediaIdentifier", aaru.MediaTagDvdMediaIdentifier},
	{"MediaTagDvdMkb", aaru.MediaTagDvdMkb},
	{"MediaTagDvdRamDds", aaru.MediaTagDvdRamDds},
	{"MediaTagDvdRamMediumStatus", aaru.MediaTagDvdRamMediumStatus},
	{"MediaTagDvdRamSpareArea", aaru.MediaTagDvdRamSpareArea},
	{"MediaTagDvdrRmd", aaru.MediaTagDvdrRmd},
	{"MediaTagDvdrPreRecordedInfo", aaru.MediaTagDvdrPreRecordedInfo},
	{"MediaTagDvdrMediaIdentifier", aaru.MediaTagDvdrMediaIdentifier},
	{"MediaTagDvdrPfi", aaru.MediaTagDvdrPfi},
	{"MediaTagDvdAdip", aaru.MediaTagDvdAdip},
	{"MediaTagHddvdCpi", aaru.MediaTagHddvdCpi},
	{"MediaTagHddvdMediumStatus", aaru.MediaTagHddvdMediumStatus},
	{"MediaTagDvddlLayerCapacity", aaru.MediaTagDvddlLayerCapacity},
	{"MediaTagDvddlMiddleZoneAddress", aaru.MediaTagDvddlMiddleZoneAddress},
	{"MediaTagDvddlJumpIntervalSize", aaru.MediaTagDvddlJumpIntervalSize},
	{"MediaTagDvddlManualLayerJumpLba", aaru.MediaTagDvddlManualLayerJumpLba},
	{"MediaTagBlurayDi", aaru.MediaTagBlurayDi},
	{"MediaTagBlurayBca", aaru.MediaTagBlurayBca},
	{"MediaTagBlurayDds", aaru.MediaTagBlurayDds},
	{"MediaTagBlurayCartridgeStatus", aaru.MediaTagBlurayCartridgeStatus},
	{"MediaTagBluraySpareArea", aaru.MediaTagBluraySpareArea},
	{"MediaTagAacsVolumeIdentifier", aaru.MediaTagAacsVolumeIdentifier},
	{"MediaTagAacsSerialNumber", aaru.MediaTagAacsSerialNumber},
	{"MediaTagAacsMediaIdentifier", aaru.MediaTagAacsMediaIdentifier},
	{"MediaTagAacsMkb", aaru.MediaTagAacsMkb},
	{"MediaTagAacsDataKeys", aaru.MediaTagAacsDataKeys},
	{"MediaTagAacsLbaExtents", aaru.MediaTagAacsLbaExtents},
	{"MediaTagCprmMkb", aaru.MediaTagCprmMkb},
	{"MediaTagHybridRecognizedLayers", aaru.MediaTagHybridRecognizedLayers},
	{"MediaTagMmcWriteProtection", aaru.MediaTagMmcWriteProtection},
	{"MediaTagMmcDiscInformation", aaru.MediaTagMmcDiscInformation},
	{"MediaTagMmcTrackResourcesInformation", aaru.MediaTagMmcTrackResourcesInformation},
	{"MediaTagMmcPowResourcesInformation", aaru.MediaTagMmcPowResourcesInformation},
	{"MediaTagScsiInquiry", aaru.MediaTagScsiInquiry},
	{"MediaTagScsiModePage2A", aaru.MediaTagScsiModePage2A},
	{"MediaTagAtaIdentify", aaru.MediaTagAtaIdentify},
	{"MediaTagAtapiIdentify", aaru.MediaTagAtapiIdentify},
	{"MediaTagPcmciaCis", aaru.MediaTagPcmciaCis},
	{"MediaTagSdCid", aaru.MediaTagSdCid},
	{"MediaTagSdCsd", aaru.MediaTagSdCsd},
	{"MediaTagSdScr", aaru.MediaTagSdScr},
	{"MediaTagSdOcr", aaru.MediaTagSdOcr},
	{"MediaTagMmcCid", aaru.MediaTagMmcCid},
	{"MediaTagMmcCsd", aaru.MediaTagMmcCsd},
	{"MediaTagMmcOcr", aaru.MediaTagMmcOcr},
	{"MediaTagExtendedCsd", aaru.MediaTagExtendedCsd},
	{"MediaTagXboxSecuritySector", aaru.MediaTagXboxSecuritySector},
	{"MediaTagFloppyLeadOut", aaru.MediaTagFloppyLeadOut},
	{"MediaTagDiscControlBlock", aaru.MediaTagDiscControlBlock},
	{"MediaTagCdFirstTrackPregap", aaru.MediaTagCdFirstTrackPregap},
	{"MediaTagCdLeadOut", aaru.MediaTagCdLeadOut},
	{"MediaTagScsiModeSense6", aaru.MediaTagScsiModeSense6},
	{"MediaTagScsiModeSense10", aaru.MediaTagScsiModeSense10},
	{"MediaTagUsbDescriptors", aaru.MediaTagUsbDescriptors},
	{"MediaTagXboxDmi", aaru.MediaTagXboxDmi},
	{"MediaTagXboxPfi", aaru.MediaTagXboxPfi},
	{"MediaTagCdLeadIn", aaru.MediaTagCdLeadIn},
	{"MediaTagMiniDiscType", aaru.MediaTagMiniDiscType},
	{"MediaTagMiniDiscD5", aaru.MediaTagMiniDiscD5},
	{"MediaTagMiniDiscUtoc", aaru.MediaTagMiniDiscUtoc},
	{"MediaTagMiniDiscDtoc", aaru.MediaTagMiniDiscDtoc},
	{"MediaTagDvdDiscKeyDecrypted", aaru.MediaTagDvdDiscKeyDecrypted},
	{"MediaTagDvdPfi2ndLayer", aaru.MediaTagDvdPfi2ndLayer},
	{"MediaTagFloppyWriteProtect", aaru.MediaTagFloppyWriteProtect},
	{"MediaTagAacsMediaKey", aaru.MediaTagAacsMediaKey},
	{"MediaTagAacsVolumeUniqueKey", aaru.MediaTagAacsVolumeUniqueKey},
}

// injectTagDataTypes maps each media tag type to the data type of its block.
var injectTagDataTypes = map[aaru.MediaTagType]aaru.DataType{
	aaru.MediaTagCdToc:                        aaru.DataTypeCdToc,
	aaru.MediaTagSessionInfo:                  aaru.DataTypeSessionInfo,
	aaru.MediaTagFullToc:                      aaru.DataTypeFullToc,
	aaru.MediaTagCdPma:                        aaru.DataTypeCdPma,
	aaru.MediaTagCdAtip:                       aaru.DataTypeCdAtip,
	aaru.MediaTagCdText:                       aaru.DataTypeCdText,
	aaru.MediaTagDvdPfi:                       aaru.DataTypeDvdPfi,
	aaru.MediaTagDvdPfi2ndLayer:               aaru.DataTypeDvdPfi2ndLayer,
	aaru.MediaTagDvdCmi:                       aaru.DataTypeDvdCmi,
	aaru.MediaTagDvdDiscKey:                   aaru.DataTypeDvdDiscKey,
	aaru.MediaTagDvdBca:                       aaru.DataTypeDvdBca,
	aaru.MediaTagDvdDmi:                       aaru.DataTypeDvdDmi,
	aaru.MediaTagDvdMediaIdentifier:           aaru.DataTypeDvdMediaIdentifier,
	aaru.MediaTagDvdMkb:                       aaru.DataTypeDvdMkb,
	aaru.MediaTagDvdRamDds:                    aaru.DataTypeDvdRamDds,
	aaru.MediaTagDvdRamMediumStatus:           aaru.DataTypeDvdRamMediumStatus,
	aaru.MediaTagDvdRamSpareArea:              aaru.DataTypeDvdRamSpareArea,
	aaru.MediaTagDvdrRmd:                      aaru.DataTypeDvdrRmd,
	aaru.MediaTagDvdrPreRecordedInfo:          aaru.DataTypeDvdrPreRecordedInfo,
	aaru.MediaTagDvdrMediaIdentifier:          aaru.DataTypeDvdrMediaIdentifier,
	aaru.MediaTagDvdrPfi:                      aaru.DataTypeDvdrPfi,
	aaru.MediaTagDvdAdip:                      aaru.DataTypeDvdAdip,
	aaru.MediaTagHddvdCpi:                     aaru.DataTypeHddvdCpi,
	aaru.MediaTagHddvdMediumStatus:            aaru.DataTypeHddvdMediumStatus,
	aaru.MediaTagDvddlLayerCapacity:           aaru.DataTypeDvddlLayerCapacity,
	aaru.MediaTagDvddlMiddleZoneAddress:       aaru.DataTypeDvddlMiddleZoneAddress,
	aaru.MediaTagDvddlJumpIntervalSize:        aaru.DataTypeDvddlJumpIntervalSize,
	aaru.MediaTagDvddlManualLayerJumpLba:      aaru.DataTypeDvddlManualLayerJumpLba,
	aaru.MediaTagBlurayDi:                     aaru.DataTypeBlurayDi,
	aaru.MediaTagBlurayBca:                    aaru.DataTypeBlurayBca,
	aaru.MediaTagBlurayDds:                    aaru.DataTypeBlurayDds,
	aaru.MediaTagBlurayCartridgeStatus:        aaru.DataTypeBlurayCartridgeStatus,
	aaru.MediaTagBluraySpareArea:              aaru.DataTypeBluraySpareArea,
	aaru.MediaTagAacsVolumeIdentifier:         aaru.DataTypeAacsVolumeIdentifier,
	aaru.MediaTagAacsSerialNumber:             aaru.DataTypeAacsSerialNumber,
	aaru.MediaTagAacsMediaIdentifier:          aaru.DataTypeAacsMediaIdentifier,
	aaru.MediaTagAacsMkb:                      aaru.DataTypeAacsMkb,
	aaru.MediaTagAacsDataKeys:                 aaru.DataTypeAacsDataKeys,
	aaru.MediaTagAacsLbaExtents:               aaru.DataTypeAacsLbaExtents,
	aaru.MediaTagCprmMkb:                      aaru.DataTypeCprmMkb,
	aaru.MediaTagHybridRecognizedLayers:       aaru.DataTypeHybridRecognizedLayers,
	aaru.MediaTagMmcWriteProtection:           aaru.DataTypeMmcWriteProtection,
	aaru.MediaTagMmcDiscInformation:           aaru.DataTypeMmcDiscInformation,
	aaru.MediaTagMmcTrackResourcesInformation: aaru.DataTypeMmcTrackResourcesInformation,
	aaru.MediaTagMmcPowResourcesInformation:   aaru.DataTypeMmcPowResourcesInformation,
	aaru.MediaTagScsiInquiry:                  aaru.DataTypeScsiInquiry,
	aaru.MediaTagScsiModePage2A:               aaru.DataTypeScsiModePage2A,
	aaru.MediaTagAtaIdentify:                  aaru.DataTypeAtaIdentify,
	aaru.MediaTagAtapiIdentify:                aaru.DataTypeAtapiIdentify,
	aaru.MediaTagPcmciaCis:                    aaru.DataTypePcmciaCis,
	aaru.MediaTagSdCid:                        aaru.DataTypeSdCid,
	aaru.MediaTagSdCsd:                        aaru.DataTypeSdCsd,
	aaru.MediaTagSdScr:                        aaru.DataTypeSdScr,
	aaru.MediaTagSdOcr:                        aaru.DataTypeSdOcr,
	aaru.MediaTagMmcCid:                       aaru.DataTypeMmcCid,
	aaru.MediaTagMmcCsd:                       aaru.DataTypeMmcCsd,
	aaru.MediaTagMmcOcr:                       aaru.DataTypeMmcOcr,
	aaru.MediaTagExtendedCsd:                  aaru.DataTypeExtendedCsd,
	aaru.MediaTagXboxSecuritySector:           aaru.DataTypeXboxSecuritySector,
	aaru.MediaTagFloppyLeadOut:                aaru.DataTypeFloppyLeadOut,
	aaru.MediaTagDiscControlBlock:             aaru.DataTypeDiscControlBlock,
	aaru.MediaTagCdFirstTrackPregap:           aaru.DataTypeCdFirstTrackPregap,
	aaru.MediaTagCdLeadOut:                    aaru.DataTypeCdLeadOut,
	aaru.MediaTagScsiModeSense6:               aaru.DataTypeScsiModeSense6,
	aaru.MediaTagScsiModeSense10:              aaru.DataTypeScsiModeSense10,
	aaru.MediaTagUsbDescriptors:               aaru.DataTypeUsbDescriptors,
	aaru.MediaTagXboxDmi:                      aaru.DataTypeXboxDmi,
	aaru.MediaTagXboxPfi:                      aaru.DataTypeXboxPfi,
	aaru.MediaTagCdMcn:                        aaru.DataTypeCdMcn,
	aaru.MediaTagCdLeadIn:                     aaru.DataTypeCdLeadIn,
	aaru.MediaTagDvdDiscKeyDecrypted:          aaru.DataTypeDvdDiscKeyDecrypted,
	aaru.MediaTagFloppyWriteProtect:           aaru.DataTypeFloppyWriteProtect,
	aaru.MediaTagAacsMediaKey:                 aaru.DataTypeAacsMediaKey,
	aaru.MediaTagAacsVolumeUniqueKey:          aaru.DataTypeAacsVolumeUniqueKey,
}

// tagIsSpace reports whether c is an ASCII white space character.
func tagIsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// tagParseInt reports in ok whether s is a complete, in-range base 10
// integer (leading white space and a sign are allowed). A string without
// digits, including the empty string, is not a number.
func tagParseInt(s string) (v int64, ok bool) {
	i := 0
	for i < len(s) && tagIsSpace(s[i]) {
		i++
	}
	num := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return 0, false
	}
	if i != len(s) {
		return 0, false
	}
	v, err := strconv.ParseInt(s[num:], 10, 64)
	return v, err == nil
}

// tagEqualFoldASCII reports whether a and b are equal under ASCII case
// folding.
func tagEqualFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// parseMediaTagType parses a media tag type: s is either a number in
// [0, MaxMediaTag] or one of the injectTagNames (case-insensitive).
func parseMediaTagType(s string) (aaru.MediaTagType, bool) {
	if v, ok := tagParseInt(s); ok {
		if v >= 0 && v <= int64(aaru.MaxMediaTag) {
			return aaru.MediaTagType(v), true
		}
		return 0, false
	}
	for _, m := range injectTagNames {
		if tagEqualFoldASCII(s, m.name) {
			return m.tag, true
		}
	}
	return 0, false
}

// printValidMediaTagTypes lists the accepted media tag type names.
func printValidMediaTagTypes(w io.Writer) {
	fmt.Fprintf(w, "\nValid media tag types:\n")
	for _, m := range injectTagNames {
		fmt.Fprintf(w, "  %3d  %s\n", m.tag, m.name)
	}
}

// injectMediaTag runs the inject-media-tag verb. Progress goes to stdout and
// errors to stderr; it returns 0 on success and -1 on error.
func injectMediaTag(stdout, stderr io.Writer, tagTypeStr, tagFile, imageFile string) int {
	p := func(format string, a ...any) { fmt.Fprintf(stdout, format, a...) }
	e := func(format string, a ...any) {
		maintFlush(stdout)
		fmt.Fprintf(stderr, format, a...)
	}

	tagType, ok := parseMediaTagType(tagTypeStr)
	if !ok {
		e("ERROR: Invalid media tag type: '%s'\n", tagTypeStr)
		printValidMediaTagTypes(stdout)
		return -1
	}

	dataType, ok := injectTagDataTypes[tagType]
	if !ok {
		e("ERROR: Cannot convert media tag type '%s' to data type.\n", tagTypeStr)
		return -1
	}

	// Read the media tag file.
	tf, err := os.Open(tagFile)
	if err != nil {
		e("ERROR: Cannot open media tag file: '%s'\n", tagFile)
		e("       %s\n", maintStrerror(err))
		return -1
	}
	tagSize, err := tf.Seek(0, io.SeekEnd)
	if err != nil {
		tagSize = -1
	}
	if tagSize <= 0 {
		e("ERROR: Media tag file is empty or cannot determine size: '%s'\n", tagFile)
		_ = tf.Close()
		return -1
	}
	// A data block stores 32-bit lengths.
	if tagSize > math.MaxUint32 {
		e("ERROR: Media tag file is too big (%d bytes): '%s'\n", tagSize, tagFile)
		_ = tf.Close()
		return -1
	}
	tagData := make([]byte, tagSize)
	if _, err := tf.ReadAt(tagData, 0); err != nil {
		e("ERROR: Cannot read media tag file: '%s'\n", tagFile)
		_ = tf.Close()
		return -1
	}
	_ = tf.Close()

	// Validate the image.
	img, err := os.Open(imageFile)
	if err != nil {
		e("ERROR: Cannot open image file: '%s'\n", imageFile)
		e("       %s\n", maintStrerror(err))
		return -1
	}
	defer func() { _ = img.Close() }()

	var v1 [maintHeaderV1Size]byte
	if n, _ := img.ReadAt(v1[:], 0); n != len(v1) {
		e("ERROR: Cannot read image header from: '%s'\n", imageFile)
		e("       File may be too small or corrupted.\n")
		return -1
	}
	if ident := binary.LittleEndian.Uint64(v1[:]); ident != aaru.AaruMagic && ident != aaru.DICMagic {
		e("ERROR: '%s' is not a valid AaruFormat image.\n", imageFile)
		return -1
	}
	if major, minor := v1[72], v1[73]; major < aaru.FormatVersionV2 {
		e("ERROR: Image version %d.%d is not supported.\n", major, minor)
		e("       Only AaruFormat version 2 or higher images are supported.\n")
		e("       Please upgrade the image using the 'convert' command first.\n")
		return -1
	}

	var header aaru.Header
	if n, _ := maintReadStruct(img, 0, &header); n != maintHeaderSize {
		e("ERROR: Cannot read v2 header from: '%s'\n", imageFile)
		return -1
	}

	imageSize, err := maintFileSize(img)
	if err != nil {
		e("ERROR: Cannot seek to index offset %d: %s\n", header.IndexOffset, maintStrerror(err))
		return -1
	}
	if header.IndexOffset > math.MaxInt64 {
		e("ERROR: Cannot seek to index offset %d: %s\n", header.IndexOffset, maintStrerror(syscall.EINVAL))
		return -1
	}

	var indexHeader aaru.IndexHeader3
	if n, _ := maintReadStruct(img, int64(header.IndexOffset), &indexHeader); n != maintIndexHeader3Size {
		e("ERROR: Cannot read index header from: '%s'\n", imageFile)
		return -1
	}
	if indexHeader.Identifier != aaru.BlockTypeIndexBlock3 {
		e("ERROR: Expected IndexBlock3 identifier, found 0x%08X\n", uint32(indexHeader.Identifier))
		return -1
	}

	entries, ok := maintReadIndexEntries(img, int64(header.IndexOffset)+maintIndexHeader3Size, indexHeader.Entries,
		imageSize)
	if !ok {
		e("ERROR: Cannot read index entries (read %d, expected %d)\n", len(entries), indexHeader.Entries)
		return -1
	}

	// The user data DDT2 supplies the block alignment when the header lacks it.
	var userData *aaru.IndexEntry
	for i := range entries {
		if entries[i].BlockType == aaru.BlockTypeDeDuplicationTable2 && entries[i].DataType == aaru.DataTypeUserData {
			userData = &entries[i]
			break
		}
	}
	if userData == nil {
		e("ERROR: No UserData DDT2 block found in index.\n")
		e("       This image appears to be invalid or corrupted.\n")
		return -1
	}

	if userData.Offset > math.MaxInt64 {
		e("ERROR: Cannot seek to DDT2 offset %d: %s\n", userData.Offset, maintStrerror(syscall.EINVAL))
		return -1
	}
	var ddtHeader aaru.DDTHeader2
	if n, _ := maintReadStruct(img, int64(userData.Offset), &ddtHeader); n != maintDDTHeader2Size {
		e("ERROR: Cannot read DDT2 header from offset %d\n", userData.Offset)
		return -1
	}
	if ddtHeader.Identifier != aaru.BlockTypeDeDuplicationTable2 {
		e("ERROR: Expected DeDuplicationTable2 identifier at offset %d, found 0x%08X\n", userData.Offset,
			uint32(ddtHeader.Identifier))
		return -1
	}

	shift := header.BlockAlignmentShift
	if shift == 0 {
		shift = ddtHeader.BlockAlignmentShift
		p("  Note: Header blockAlignmentShift is 0, using value %d from DDT2 header.\n", shift)
	}

	tagName := maintMediaTagTypeString(int32(tagType))
	p("Injecting media tag into image...\n")
	p("  tag-type: %s (%d) -> DataType %d\n", tagName, tagType, dataType)
	p("  media-tag-file: %s (%d bytes)\n", tagFile, tagSize)
	p("  image-file: %s\n", imageFile)
	p("  index entries: %d\n", indexHeader.Entries)
	p("  blockAlignmentShift: %d\n", shift)

	// Drop any existing data block of the same data type from the index.
	kept := entries[:0:0]
	removed := false
	for _, ent := range entries {
		if ent.BlockType == aaru.BlockTypeDataBlock && ent.DataType == dataType {
			p("  Removing existing DataBlock with dataType %d at offset %d\n", dataType, ent.Offset)
			removed = true
			continue
		}
		kept = append(kept, ent)
	}
	if removed {
		p("  Removed existing media tag entry from index.\n")
	}

	_ = img.Close()
	img, err = os.OpenFile(imageFile, os.O_RDWR, 0)
	if err != nil {
		e("ERROR: Cannot open image file for writing: '%s'\n", imageFile)
		e("       %s\n", maintStrerror(err))
		return -1
	}

	end, err := maintFileSize(img)
	if err != nil {
		e("ERROR: Cannot write block header.\n")
		return -1
	}
	blockPos := maintAlignUp(uint64(end), shift)
	p("  Writing data block at offset %d\n", blockPos)

	bh := aaru.BlockHeader{
		Identifier:  aaru.BlockTypeDataBlock,
		Type:        dataType,
		Compression: aaru.CompressionNone,
		SectorSize:  1, // media tags have no sectors
		Length:      uint32(tagSize),
		CmpLength:   uint32(tagSize),
		CRC64:       crc64.Checksum(tagData),
	}
	bh.CmpCRC64 = bh.CRC64

	if _, err := maintWriteStruct(img, int64(blockPos), bh); err != nil {
		e("ERROR: Cannot write block header.\n")
		return -1
	}
	if _, err := img.WriteAt(tagData, int64(blockPos)+maintBlockHeaderSize); err != nil {
		e("ERROR: Cannot write tag data.\n")
		return -1
	}
	p("  Written data block (%d + %d bytes)\n", maintBlockHeaderSize, tagSize)

	kept = append(kept, aaru.IndexEntry{BlockType: aaru.BlockTypeDataBlock, DataType: dataType, Offset: blockPos})
	p("  Added new index entry: blockType=DataBlock, dataType=%d, offset=%d\n", dataType, blockPos)

	indexPos := maintAlignUp(blockPos+maintBlockHeaderSize+uint64(tagSize), shift)
	p("  Writing new index at offset %d with %d entries\n", indexPos, len(kept))

	entryBytes, _ := binary.Append(nil, binary.LittleEndian, kept)
	newIndexHeader := aaru.IndexHeader3{
		Identifier: aaru.BlockTypeIndexBlock3,
		Entries:    uint64(len(kept)),
		Previous:   0,
		CRC64:      crc64.Checksum(entryBytes),
	}
	if _, err := maintWriteStruct(img, int64(indexPos), newIndexHeader); err != nil {
		e("ERROR: Cannot write index header.\n")
		return -1
	}
	if _, err := img.WriteAt(entryBytes, int64(indexPos)+maintIndexHeader3Size); err != nil {
		e("ERROR: Cannot write index entries.\n")
		return -1
	}
	p("  Written index header and %d entries\n", len(kept))

	header.IndexOffset = indexPos
	header.LastWrittenTime = aaru.TimeToFiletime(time.Now())
	header.BlockAlignmentShift = shift

	if _, err := maintWriteStruct(img, 0, header); err != nil {
		e("ERROR: Cannot write updated header.\n")
		return -1
	}
	p("  Updated header with new index offset %d\n", indexPos)

	if err := img.Close(); err != nil {
		e("ERROR: Cannot write updated header.\n")
		return -1
	}

	p("\nSuccessfully injected media tag '%s' into image.\n", tagName)
	return 0
}
