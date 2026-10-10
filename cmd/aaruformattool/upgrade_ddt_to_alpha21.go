package main

// upgrade-ddt-to-alpha21 rewrites the version 2 deduplication tables of an
// image written by Aaru alpha 16 or earlier (whose DDT2 headers stored the
// negative and overflow sector counts as 16-bit fields) with the current
// header layout, appends them at the end of the file and writes a new index
// pointing to them.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"syscall"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Packed on-disk sizes of the structures the maintenance verbs handle.
const (
	maintHeaderV1Size     = 104 // AaruHeader
	maintHeaderSize       = 147 // AaruHeaderV2
	maintIndexHeader3Size = 28  // IndexHeader3
	maintIndexEntrySize   = 14  // IndexEntry
	maintDDTHeader2Size   = 85  // DdtHeader2
	maintDDTAlpha20Size   = 81  // ddtHeader2Alpha20
	maintBlockHeaderSize  = 36  // BlockHeader
)

const maintRule = "================================================================================\n"

func usageUpgradeDDTToAlpha21() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool upgrade-ddt-to-alpha21 <filename>\n\n")
	fmt.Printf("Upgrades a DDT image to alpha21 format.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename>       Path to the DDT image file to upgrade.\n")
}

func cmdUpgradeDDTToAlpha21(args []string) int {
	va := newVerbArgs("upgrade-ddt-to-alpha21", usageUpgradeDDTToAlpha21)
	pos, ok := va.parse(args, 1, 1)
	if !ok {
		return -1
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	return upgradeDDTToAlpha21(out, bufio.NewReader(os.Stdin), pos[0])
}

// ddtUpgrade is one DDT2 block being upgraded.
type ddtUpgrade struct {
	old       ddtHeader2Alpha20
	payload   []byte
	oldOffset uint64
	newOffset uint64
	dataType  aaru.DataType
}

// alpha20IsDVD reports the media types the alpha 20 DVD block count fix
// applies to.
func alpha20IsDVD(mt media.Type) bool {
	switch mt {
	case media.DVDROM, media.DVDR, media.DVDRW, media.DVDPR, media.DVDPRW, media.DVDPRWDL, media.DVDRDL,
		media.DVDPRDL, media.DVDRWDL, media.DVDDownload, media.PS2DVD, media.PS3DVD, media.Nuon:
		return true
	}
	return false
}

// upgradeDDTToAlpha21 runs the upgrade-ddt-to-alpha21 verb. Confirmations are
// read from in; everything is printed to w. It returns 0 or a system error
// number.
func upgradeDDTToAlpha21(w io.Writer, in *bufio.Reader, path string) int {
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) }

	p("upgrade-ddt-to-alpha21 command called with path: %s\n", path)

	f, err := os.Open(path)
	if err != nil {
		p("ERROR: Failed to open file '%s': %s\n", path, maintStrerror(err))
		return maintErrno(err)
	}
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	// Read the initial (version 1 sized) header to check the version.
	var v1 [maintHeaderV1Size]byte
	n, _ := f.ReadAt(v1[:], 0)
	if n != len(v1) {
		p("ERROR: Failed to read header from file (read %d bytes, expected %d)\n", n, maintHeaderV1Size)
		return maintEIO
	}
	if ident := binary.LittleEndian.Uint64(v1[:]); ident != aaru.AaruMagic && ident != aaru.DICMagic {
		p("ERROR: File is not an AaruFormat image (invalid magic identifier)\n")
		return maintEINVAL
	}
	major, minor := v1[72], v1[73]
	p("Detected AaruFormat image version %d.%d\n", major, minor)

	if major != aaru.FormatVersionV2 {
		p("ERROR: This command only works with AaruFormat v2 images\n")
		p("       Found version %d.%d, expected version 2.x\n", major, minor)
		return maintEINVAL
	}

	var header aaru.Header
	if n, _ := maintReadStruct(f, 0, &header); n != maintHeaderSize {
		p("ERROR: Failed to read complete v2 header from file (read %d bytes, expected %d)\n", n, maintHeaderSize)
		return maintEIO
	}

	p("Successfully loaded AaruFormat v2 header\n")
	p("  Index offset: %d bytes\n", header.IndexOffset)
	p("  Media type: %d (%s)\n", header.MediaType, maintMediaTypeString(header.MediaType))
	p("  Block alignment shift: %d\n", header.BlockAlignmentShift)
	p("  Biggest sector size: %d bytes\n", header.BiggestSectorSize)

	p("\n")
	p("%s", maintRule)
	p("                           *** IMPORTANT WARNING ***\n")
	p("%s", maintRule)
	p("\n")
	p("This command is designed ONLY for images created with:\n")
	p("  - Aaru alpha 16 or earlier\n")
	p("\n")
	p("CRITICAL LIMITATIONS:\n")
	p("\n")
	p("  1. There is NO WAY to automatically determine if this image actually needs\n")
	p("     an upgrade. The file format version cannot distinguish between images\n")
	p("     created with Aaru alpha 16 and newer versions.\n")
	p("\n")
	p("  2. Attempting to upgrade images created with NEWER versions (Aaru alpha 17+)\n")
	p("     WILL CORRUPT THEM and make them UNREADABLE.\n")
	p("\n")
	p("  3. There is NO GUARANTEE of success. The upgrade process may fail or\n")
	p("     produce an invalid image even with correct input.\n")
	p("\n")
	p("  4. This command is provided as a LAST RESORT ONLY.\n")
	p("\n")
	p("RECOMMENDED SOLUTION:\n")
	p("\n")
	p("  The IDEAL and SAFEST solution is to re-dump the media using a current\n")
	p("  version of Aaru. This ensures data integrity and full compatibility with\n")
	p("  the latest format improvements.\n")
	p("\n")
	p("Before proceeding:\n")
	p("  - Make sure you have a BACKUP of the original image file\n")
	p("  - Verify the image was created with Aaru alpha 16 or earlier\n")
	p("  - Consider re-dumping the media if possible\n")
	p("\n")
	p("%s", maintRule)
	p("\n")

	p("Structure size comparison:\n")
	p("  Alpha20 DDT v2 header: %d bytes\n", maintDDTAlpha20Size)
	p("  Current library DdtHeader2: %d bytes\n", maintDDTHeader2Size)
	p("  Structure sizes differ - upgrade needed!\n")

	p("\n")
	p("Do you want to proceed with the upgrade? (yes/no): ")
	maintFlush(w)
	response, ok := maintReadLine(in)
	if !ok {
		p("\nERROR: Failed to read user input\n")
		return maintEIO
	}
	if !maintIsYes(response) {
		p("\nUpgrade cancelled by user.\n")
		return 0
	}

	p("\nProceeding with upgrade...\n")

	size, err := maintFileSize(f)
	if err != nil {
		p("ERROR: Failed to seek to end of file: %s\n", maintStrerror(err))
		return maintEIO
	}

	// Only offsets that do not fit in an int64 cannot be sought to.
	if header.IndexOffset > math.MaxInt64 {
		p("ERROR: Failed to seek to index offset %d: %s\n", header.IndexOffset, maintStrerror(syscall.EINVAL))
		return maintEIO
	}

	var indexHeader aaru.IndexHeader3
	if n, _ := maintReadStruct(f, int64(header.IndexOffset), &indexHeader); n != maintIndexHeader3Size {
		p("ERROR: Failed to read index header (read %d bytes, expected %d)\n", n, maintIndexHeader3Size)
		return maintEIO
	}

	if indexHeader.Identifier != aaru.BlockTypeIndexBlock3 {
		p("ERROR: Expected IndexBlock3 identifier, found 0x%08X\n", uint32(indexHeader.Identifier))
		return maintEINVAL
	}

	p("Reading index with %d entries...\n", indexHeader.Entries)

	entries, ok := maintReadIndexEntries(f, int64(header.IndexOffset)+maintIndexHeader3Size, indexHeader.Entries, size)
	if !ok {
		p("ERROR: Failed to read index entries (read %d, expected %d)\n", len(entries), indexHeader.Entries)
		return maintEIO
	}

	p("Successfully loaded %d index entries into hash table\n", indexHeader.Entries)

	var ddtCount uint64
	for _, e := range entries {
		if e.BlockType == aaru.BlockTypeDeDuplicationTable2 {
			ddtCount++
		}
	}

	p("Found %d DDT v2 blocks to upgrade\n", ddtCount)

	if ddtCount == 0 {
		p("WARNING: No DDT v2 blocks found in index\n")
		p("         Nothing to upgrade.\n")
		return 0
	}

	// Read every DDT2 block, removing it from the index (the remaining
	// entries keep their order; the upgraded ones are appended later).
	upgrades := make([]*ddtUpgrade, 0, ddtCount)
	remaining := make([]aaru.IndexEntry, 0, len(entries))
	for _, e := range entries {
		if e.BlockType != aaru.BlockTypeDeDuplicationTable2 {
			remaining = append(remaining, e)
			continue
		}

		p("Processing DDT v2 block (dataType=%s) at offset %d...\n", maintDataTypeString(uint16(e.DataType)), e.Offset)

		info := &ddtUpgrade{oldOffset: e.Offset, dataType: e.DataType}

		if e.Offset > math.MaxInt64 {
			p("ERROR: Failed to seek to DDT block at offset %d: %s\n", e.Offset, maintStrerror(syscall.EINVAL))
			return maintEIO
		}

		if n, _ := maintReadStruct(f, int64(e.Offset), &info.old); n != maintDDTAlpha20Size {
			p("ERROR: Failed to read old DDT header (read %d bytes, expected %d)\n", n, maintDDTAlpha20Size)
			return maintEIO
		}

		p("  Old DDT header read successfully\n")
		p("    Identifier: 0x%08X\n", uint32(info.old.Identifier))
		p("    Type: %d\n", info.old.Type)
		p("    Compression: %d\n", info.old.Compression)
		p("    Entries: %d\n", info.old.Entries)
		p("    Compressed length: %d bytes\n", info.old.CmpLength)
		p("    Uncompressed length: %d bytes\n", info.old.Length)

		payloadOffset := int64(e.Offset) + maintDDTAlpha20Size
		var n int
		info.payload, n = maintReadPayload(f, payloadOffset, info.old.CmpLength, size)
		if uint64(n) != info.old.CmpLength {
			p("ERROR: Failed to read DDT payload (read %d bytes, expected %d)\n", n, info.old.CmpLength)
			return maintEIO
		}

		p("  DDT payload read successfully (%d bytes)\n", info.old.CmpLength)
		p("  Removing DDT entry from index...\n")
		p("  DDT block processed and removed from index\n\n")
		upgrades = append(upgrades, info)
	}

	p("Processed %d DDT v2 block(s)\n", len(upgrades))

	// The UserData DDT drives the media type checks.
	var userData *ddtHeader2Alpha20
	for _, u := range upgrades {
		if u.dataType == aaru.DataTypeUserData {
			userData = &u.old
			break
		}
	}

	if userData == nil {
		p("ERROR: No UserData DDT v2 block found in this image.\n")
		p("       Images without a UserData DDT are invalid/corrupted.\n")
		p("       Cannot proceed with upgrade.\n")
		return maintEINVAL
	}

	p("\n%s", maintRule)
	p("                          DDT UPGRADE PROCESSING\n")
	p("%s\n", maintRule)

	mediaType := media.Type(header.MediaType)
	isDVD := alpha20IsDVD(mediaType)

	if isDVD && userData.Negative == 0 && userData.Overflow == 15000 {
		p("\n*** INVALID BLOCK COUNT DETECTED ***\n")
		p("Media type: %d (%s)\n", header.MediaType, maintMediaTypeString(header.MediaType))
		p("Current values (from UserData DDT):\n")
		p("  negative sectors: %d\n", userData.Negative)
		p("  overflow sectors: %d\n", userData.Overflow)
		p("  blocks: %d\n", userData.Blocks)
		p("\nThis configuration is invalid for DVD media.\n")
		p("To fix: overflow should be 0, and blocks should be reduced by 0x30000 + 15000\n\n")
		p("Do you want to fix the block count? (yes/no): ")
		maintFlush(w)

		response, ok := maintReadLine(in)
		if !ok {
			p("\nERROR: Failed to read user input\n")
			return maintEIO
		}
		if !maintIsYes(response) {
			p("\nUser declined to fix block count. Upgrade cancelled.\n")
			return 0
		}

		p("\nApplying block count fix to all DDT blocks:\n")
		for i, u := range upgrades {
			h := &u.old
			if h.Blocks == 0 && h.Negative == 0 && h.Overflow == 0 {
				continue
			}
			p("  DDT block %d (dataType=%s):\n", i+1, maintDataTypeString(uint16(u.dataType)))
			p("    Before: blocks=%d, negative=%d, overflow=%d\n", h.Blocks, h.Negative, h.Overflow)
			h.Overflow = 0
			if h.Blocks != 0 {
				h.Blocks = h.Blocks - 0x30000 - 15000
			}
			p("    After:  blocks=%d, negative=%d, overflow=%d\n", h.Blocks, h.Negative, h.Overflow)
		}
		p("\n")
	}

	if mediaType == media.DVDRAM {
		p("\nERROR: Cannot upgrade DVD-RAM media type\n")
		p("       DVD-RAM images cannot be upgraded with this tool.\n")
		return maintEINVAL
	}

	if userData.Negative == 0 && userData.Overflow > 0 && !isDVD {
		p("\nERROR: Invalid sector configuration (from UserData DDT)\n")
		p("       negative sectors: %d\n", userData.Negative)
		p("       overflow sectors: %d\n", userData.Overflow)
		p("\n       When negative sectors is 0 and overflow sectors is > 0,\n")
		p("       the image cannot be reliably upgraded.\n")
		return maintEINVAL
	}

	// Reopen the file for writing.
	_ = f.Close()
	f, err = os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		p("ERROR: Failed to reopen file in write mode: %s\n", maintStrerror(err))
		return maintErrno(err)
	}

	shift := header.BlockAlignmentShift

	p("\nWriting upgraded DDT blocks:\n")

	for i, u := range upgrades {
		end, err := maintFileSize(f)
		if err != nil {
			p("ERROR: Failed to get file size: %s\n", maintStrerror(err))
			return maintEIO
		}

		u.newOffset = maintAlignUp(uint64(end), shift)

		p("  DDT block %d (dataType=%s):\n", i+1, maintDataTypeString(uint16(u.dataType)))
		p("    Old offset: %d bytes\n", u.oldOffset)
		p("    New offset: %d bytes\n", u.newOffset)

		n, err := maintWriteStruct(f, int64(u.newOffset), u.old.upgrade())
		if err != nil {
			p("ERROR: Failed to write new DDT header (wrote %d bytes, expected %d)\n", n, maintDDTHeader2Size)
			return maintEIO
		}

		n, err = f.WriteAt(u.payload, int64(u.newOffset)+maintDDTHeader2Size)
		if err != nil {
			p("ERROR: Failed to write DDT payload (wrote %d bytes, expected %d)\n", n, u.old.CmpLength)
			return maintEIO
		}

		p("    Header written: %d bytes\n", maintDDTHeader2Size)
		p("    Payload written: %d bytes\n", u.old.CmpLength)

		remaining = append(remaining, aaru.IndexEntry{
			BlockType: aaru.BlockTypeDeDuplicationTable2,
			DataType:  u.dataType,
			Offset:    u.newOffset,
		})
		p("    Added to index\n\n")
	}

	p("All %d DDT block(s) upgraded successfully\n", len(upgrades))

	p("\n%s", maintRule)
	p("                       WRITING UPDATED INDEX\n")
	p("%s\n", maintRule)
	p("Updated index contains %d entries\n", len(remaining))

	end, err := maintFileSize(f)
	if err != nil {
		p("ERROR: Failed to get file size: %s\n", maintStrerror(err))
		return maintEIO
	}

	newIndexOffset := maintAlignUp(uint64(end), shift)

	p("Writing new index:\n")
	p("  Current file size: %d bytes\n", end)
	p("  Block alignment: %d bytes (2^%d)\n", uint64(1)<<shift, shift)
	p("  New index offset: %d bytes\n", newIndexOffset)

	entryBytes, _ := binary.Append(nil, binary.LittleEndian, remaining)
	newIndexHeader := aaru.IndexHeader3{
		Identifier: aaru.BlockTypeIndexBlock3,
		Entries:    uint64(len(remaining)),
		Previous:   indexHeader.Previous,
	}
	if len(remaining) > 0 {
		newIndexHeader.CRC64 = crc64.Checksum(entryBytes)
		p("  Calculated index CRC64: 0x%016X\n", newIndexHeader.CRC64)
	} else {
		p("  Index CRC64 set to 0 (empty index or CRC context unavailable)\n")
	}

	if n, err := maintWriteStruct(f, int64(newIndexOffset), newIndexHeader); err != nil {
		p("ERROR: Failed to write new index header (wrote %d bytes, expected %d)\n", n, maintIndexHeader3Size)
		return maintEIO
	}

	p("  New index header written (%d bytes)\n", maintIndexHeader3Size)

	if n, err := f.WriteAt(entryBytes, int64(newIndexOffset)+maintIndexHeader3Size); err != nil {
		p("ERROR: Failed to write index entries (wrote %d, expected %d)\n", n/maintIndexEntrySize, len(remaining))
		return maintEIO
	}

	p("  Index entries written (%d entries, %d bytes)\n", len(remaining), len(entryBytes))

	p("\n%s", maintRule)
	p("                       UPDATING FILE HEADER\n")
	p("%s\n", maintRule)
	p("Updating header index offset:\n")
	p("  Old index offset: %d bytes\n", header.IndexOffset)
	p("  New index offset: %d bytes\n", newIndexOffset)

	header.IndexOffset = newIndexOffset

	if n, err := maintWriteStruct(f, 0, header); err != nil {
		p("ERROR: Failed to write updated header (wrote %d bytes, expected %d)\n", n, maintHeaderSize)
		return maintEIO
	}

	p("  Updated header written (%d bytes)\n", maintHeaderSize)

	err = f.Close()
	f = nil
	if err != nil {
		p("WARNING: Failed to flush file: %s\n", maintStrerror(err))
	}

	p("\n%s", maintRule)
	p("                    UPGRADE COMPLETED SUCCESSFULLY\n")
	p("%s\n", maintRule)
	p("Summary of changes:\n")
	p("  1. %d DDT v2 block(s) upgraded and written\n", len(upgrades))
	p("  2. Updated index written at offset %d (%d + %d bytes)\n", newIndexOffset, maintIndexHeader3Size,
		len(entryBytes))
	p("  3. File header updated with new index offset\n")
	p("\nThe image has been successfully upgraded to alpha21 format.\n")
	p("You can now use this image with current versions of Aaru.\n\n")

	return 0
}

// maintReadIndexEntries reads count index entries at off of a file of the
// given size. It returns the whole entries that could be read and
// whether all of them were; it never allocates more than the file holds.
func maintReadIndexEntries(f io.ReaderAt, off int64, count uint64, size int64) ([]aaru.IndexEntry, bool) {
	avail := uint64(0)
	if off >= 0 && off < size {
		avail = uint64(size-off) / maintIndexEntrySize
	}
	want := min(count, avail)
	buf := make([]byte, want*maintIndexEntrySize)
	n, _ := f.ReadAt(buf, off)
	got := uint64(n) / maintIndexEntrySize
	entries := make([]aaru.IndexEntry, got)
	_, _ = binary.Decode(buf[:got*maintIndexEntrySize], binary.LittleEndian, entries)
	return entries, got == count
}

// maintReadPayload reads length bytes at off of a file of the given size and
// returns them with the number of bytes read (short at end of file); it
// never allocates more than the file holds.
func maintReadPayload(f io.ReaderAt, off int64, length uint64, size int64) ([]byte, int) {
	avail := uint64(0)
	if off >= 0 && off < size {
		avail = uint64(size - off)
	}
	buf := make([]byte, min(length, avail))
	n, _ := f.ReadAt(buf, off)
	return buf[:n], n
}
