package main

// repair-cd-arena recovers the CD sector prefix/suffix arenas of AaruFormat v2
// images damaged by a resume bug in older AaruFormat writers: a resumed dump
// session restarted the custom prefix/suffix arena at offset 0, so the final
// deduplication table (DDT2) of the arena references slot indexes that point
// past the (shrunken) final data block, or at slots that now hold another
// sector's bytes, and errored sectors read back as garbage.
//
// Such an image still contains the prefix/suffix data block and DDT2 of every
// dump session (old generations are orphaned, not overwritten). The tool scans
// the file for all generations and, for every custom entry of the final DDT2,
// takes the slot from the first generation whose DDT2 held the exact same
// (status, index) value for that sector and whose data block holds the slot.
// It then appends a fresh compact arena and DDT2 with renumbered indexes and
// a new index pointing at them.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/compression/zstd"
	"github.com/eberle1080/go-aaru/crc64"
)

// Bytes stored per custom CD prefix (sync + header) and suffix (EDC/ECC) slot.
const (
	arenaPrefixSlot = 16
	arenaSuffixSlot = 288
)

func usageRepairCDArena() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool repair-cd-arena [--dry-run] <filename>\n\n")
	fmt.Printf("Repairs CD sector prefix/suffix arenas in a v2 image whose resumed dump sessions left the\n")
	fmt.Printf("final deduplication table referencing slots past the shrunken data block (garbage reads on\n")
	fmt.Printf("errored sectors). Recovers the slots from the orphaned dump generations still in the file.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  --dry-run        Only report how many slots are recoverable; write nothing.\n")
	fmt.Printf("  <filename>       Path to the AaruFormat image to repair (modified in place).\n")
}

func cmdRepairCDArena(args []string) int {
	va := newVerbArgs("repair-cd-arena", usageRepairCDArena)
	dryRun := va.fs.Bool("dry-run", false, "Only report recoverable/lost slots, write nothing")
	pos, ok := va.parse(args, 1, 1)
	if !ok {
		return -1
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	return repairCDArena(out, pos[0], *dryRun)
}

// arenaGeneration is a decoded prefix/suffix generation: one data block and
// the DDT2 of the same data type written with it.
type arenaGeneration struct {
	dataType    aaru.DataType
	blockOffset uint64   // file offset of the data block (session order == file order)
	block       []byte   // decoded arena
	ddtOffset   uint64   // file offset of the DDT2
	ddt         []uint64 // decoded DDT2 entries, one per internal sector
}

const (
	arenaStatusShift = 60
	arenaIndexMask   = 0x0FFFFFFFFFFFFFFF
)

// isCustomArenaEntry reports whether a prefix/suffix DDT2 entry references a
// stored slot rather than a regenerable or not dumped status.
func isCustomArenaEntry(entry uint64) bool {
	switch aaru.SectorStatus(entry >> arenaStatusShift) {
	case aaru.SectorStatusNotDumped, aaru.SectorStatusMode1Correct, aaru.SectorStatusMode2Form1Ok,
		aaru.SectorStatusMode2Form2Ok, aaru.SectorStatusMode2Form2NoCrc:
		return false
	}
	return true
}

// decodeArenaPayload decodes a data block or DDT2 payload (none, LZMA or
// Zstandard) of cmpLength bytes starting at payload, returning nil unless it
// decodes to exactly length bytes with the given CRC64.
//
// LZMA payloads start with the 5 properties bytes, which cmpLength includes.
func decodeArenaPayload(payload []byte, compression aaru.CompressionType, cmpLength, length, crc uint64) []byte {
	var out []byte
	switch compression {
	case aaru.CompressionNone:
		// Bound length by what the file holds.
		if length > uint64(len(payload)) {
			return nil
		}
		out = append([]byte(nil), payload[:length]...)
	case aaru.CompressionLzma:
		if cmpLength < lzma.PropsLen || cmpLength > uint64(len(payload)) {
			return nil
		}
		var err error
		out, err = lzma.Decode(payload[lzma.PropsLen:cmpLength], payload[:lzma.PropsLen], int(length))
		if err != nil || uint64(len(out)) != length {
			return nil
		}
	case aaru.CompressionZstd:
		if cmpLength > uint64(len(payload)) {
			return nil
		}
		var err error
		out, err = zstd.Decode(payload[:cmpLength], int(length))
		if err != nil || uint64(len(out)) != length {
			return nil
		}
	default:
		return nil
	}
	if crc64.Checksum(out) != crc {
		return nil
	}
	return out
}

// scanArenaGenerations scans the whole image for CD prefix/suffix data blocks
// and DDT2s, decoding and CRC-validating each, and pairs every DDT2 with the
// closest preceding data block of the same type.
func scanArenaGenerations(img []byte) []arenaGeneration {
	type decoded struct {
		dataType aaru.DataType
		offset   uint64
		data     []byte
	}
	var blocks, ddts []decoded
	size := uint64(len(img))

	for pos := uint64(0); pos+maintBlockHeaderSize <= size; pos++ {
		// Both identifiers ("DBLK", "DDT2") start with 'D': skip to the next one.
		i := bytes.IndexByte(img[pos:size-maintBlockHeaderSize+1], 'D')
		if i < 0 {
			break
		}
		pos += uint64(i)

		switch aaru.BlockType(binary.LittleEndian.Uint32(img[pos:])) {
		case aaru.BlockTypeDataBlock:
			var bh aaru.BlockHeader
			_, _ = binary.Decode(img[pos:], binary.LittleEndian, &bh)
			if bh.Type != aaru.DataTypeCdSectorPrefix && bh.Type != aaru.DataTypeCdSectorSuffix {
				continue
			}
			if bh.Length == 0 || uint64(bh.Length) > size {
				continue
			}
			payload := pos + maintBlockHeaderSize
			if payload+uint64(bh.CmpLength) > size {
				continue
			}
			data := decodeArenaPayload(img[payload:], bh.Compression, uint64(bh.CmpLength), uint64(bh.Length), bh.CRC64)
			if data == nil {
				continue
			}
			blocks = append(blocks, decoded{bh.Type, pos, data})

		case aaru.BlockTypeDeDuplicationTable2:
			if pos+maintDDTHeader2Size > size {
				continue
			}
			var dh aaru.DDTHeader2
			_, _ = binary.Decode(img[pos:], binary.LittleEndian, &dh)
			if dh.Type != aaru.DataTypeCdSectorPrefix && dh.Type != aaru.DataTypeCdSectorSuffix {
				continue
			}
			if dh.Length == 0 || dh.Length > size || dh.Length%8 != 0 {
				continue
			}
			payload := pos + maintDDTHeader2Size
			if dh.CmpLength > size || payload+dh.CmpLength > size {
				continue
			}
			data := decodeArenaPayload(img[payload:], dh.Compression, dh.CmpLength, dh.Length, dh.CRC64)
			if data == nil {
				continue
			}
			ddts = append(ddts, decoded{dh.Type, pos, data})
		}
	}

	var gens []arenaGeneration
	for _, d := range ddts {
		var best *decoded
		for j := range blocks {
			b := &blocks[j]
			if b.dataType == d.dataType && b.offset < d.offset && (best == nil || b.offset > best.offset) {
				best = b
			}
		}
		if best == nil {
			continue
		}
		entries := make([]uint64, len(d.data)/8)
		for i := range entries {
			entries[i] = binary.LittleEndian.Uint64(d.data[i*8:])
		}
		gens = append(gens, arenaGeneration{
			dataType:    d.dataType,
			blockOffset: best.offset,
			block:       best.data,
			ddtOffset:   d.offset,
			ddt:         entries,
		})
	}
	return gens
}

// findArenaSlot locates the slot bytes of one custom entry: the first
// generation of the type whose DDT2 holds the same entry for the sector and
// whose block contains the slot. It returns nil if the slot is unrecoverable.
func findArenaSlot(gens []arenaGeneration, dataType aaru.DataType, sector, entry uint64, slotSize uint64) []byte {
	idx := entry & arenaIndexMask
	for g := range gens {
		gen := &gens[g]
		if gen.dataType != dataType || sector >= uint64(len(gen.ddt)) || gen.ddt[sector] != entry {
			continue
		}
		if idx >= uint64(len(gen.block))/slotSize {
			continue
		}
		return gen.block[idx*slotSize : (idx+1)*slotSize]
	}
	return nil
}

// rebuildArena rebuilds one arena from the final DDT2, returning the new
// compact arena, the renumbered DDT2 entries and the recovered/lost counts.
// Unrecoverable custom entries become not dumped so they read back as gaps
// instead of garbage.
func rebuildArena(gens []arenaGeneration, dataType aaru.DataType, final []uint64,
	slotSize uint64) (arena []byte, ddt []uint64, recovered, lost uint64) {
	ddt = make([]uint64, len(final))
	for s, entry := range final {
		if !isCustomArenaEntry(entry) {
			ddt[s] = entry
			continue
		}
		slot := findArenaSlot(gens, dataType, uint64(s), entry, slotSize)
		if slot == nil {
			ddt[s] = uint64(aaru.SectorStatusNotDumped) << arenaStatusShift
			lost++
			continue
		}
		ddt[s] = entry>>arenaStatusShift<<arenaStatusShift | recovered
		arena = append(arena, slot...)
		recovered++
	}
	return arena, ddt, recovered, lost
}

// appendArenaBlock appends an uncompressed data block holding data at the
// aligned end of f and returns its offset.
func appendArenaBlock(f *os.File, dataType aaru.DataType, data []byte, shift uint8) (uint64, error) {
	end, err := maintFileSize(f)
	if err != nil {
		return 0, err
	}
	pos := maintAlignUp(uint64(end), shift)

	slot := uint32(arenaSuffixSlot)
	if dataType == aaru.DataTypeCdSectorPrefix {
		slot = arenaPrefixSlot
	}
	bh := aaru.BlockHeader{
		Identifier:  aaru.BlockTypeDataBlock,
		Type:        dataType,
		Compression: aaru.CompressionNone,
		SectorSize:  slot,
		CmpLength:   uint32(len(data)),
		Length:      uint32(len(data)),
		CRC64:       crc64.Checksum(data),
	}
	bh.CmpCRC64 = bh.CRC64

	buf, err := binary.Append(nil, binary.LittleEndian, bh)
	if err != nil {
		return 0, err
	}
	if _, err := f.WriteAt(append(buf, data...), int64(pos)); err != nil {
		return 0, err
	}
	return pos, nil
}

// appendArenaDDT appends an uncompressed DDT2 at the aligned end of f,
// cloning the geometry of templ, and returns its offset.
func appendArenaDDT(f *os.File, templ aaru.DDTHeader2, dataType aaru.DataType, ddt []uint64) (uint64, error) {
	end, err := maintFileSize(f)
	if err != nil {
		return 0, err
	}
	pos := maintAlignUp(uint64(end), templ.BlockAlignmentShift)

	payload, err := binary.Append(nil, binary.LittleEndian, ddt)
	if err != nil {
		return 0, err
	}
	dh := templ
	dh.Identifier = aaru.BlockTypeDeDuplicationTable2
	dh.Type = dataType
	dh.Compression = aaru.CompressionNone
	dh.Entries = uint64(len(ddt))
	dh.CmpLength = uint64(len(payload))
	dh.Length = uint64(len(payload))
	dh.CRC64 = crc64.Checksum(payload)
	dh.CmpCRC64 = dh.CRC64

	buf, err := binary.Append(nil, binary.LittleEndian, dh)
	if err != nil {
		return 0, err
	}
	if _, err := f.WriteAt(append(buf, payload...), int64(pos)); err != nil {
		return 0, err
	}
	return pos, nil
}

// arenaJob is the repair of one arena (prefix or suffix).
type arenaJob struct {
	dataType    aaru.DataType
	name        string
	finalDDT    uint64 // offset of the final DDT2 referenced by the index
	slot        uint64
	newBlockOff uint64
	newDDTOff   uint64
}

// repairCDArena runs the repair-cd-arena verb. It returns 0 or a system error
// number.
func repairCDArena(w io.Writer, path string, dryRun bool) int {
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) }

	suffix := ""
	if dryRun {
		suffix = " (dry run)"
	}
	p("repair-cd-arena on '%s'%s\n\n", path, suffix)

	flag := os.O_RDWR
	if dryRun {
		flag = os.O_RDONLY
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		p("ERROR: cannot open '%s': %s\n", path, maintStrerror(err))
		return maintErrno(err)
	}
	defer f.Close()

	var header aaru.Header
	if n, _ := maintReadStruct(f, 0, &header); n != maintHeaderSize {
		p("ERROR: cannot read header\n")
		return maintEIO
	}
	if header.Identifier != aaru.AaruMagic || header.ImageMajorVersion != aaru.FormatVersionV2 {
		p("ERROR: not an AaruFormat v2 image\n")
		return maintEINVAL
	}

	// Load the whole image for scanning/decoding.
	size, err := maintFileSize(f)
	if err != nil {
		p("ERROR: cannot read image into memory\n")
		return maintEIO
	}
	img := make([]byte, size)
	if n, _ := f.ReadAt(img, 0); int64(n) != size {
		p("ERROR: cannot read image into memory\n")
		return maintEIO
	}

	// Read the current index (IndexBlock3 chain). A chain that loops back is
	// followed only once.
	var index []aaru.IndexEntry
	visited := map[uint64]bool{}
	for off := header.IndexOffset; off != 0 && !visited[off]; {
		visited[off] = true
		var ih aaru.IndexHeader3
		if off > uint64(size) {
			p("ERROR: cannot read index at %d\n", off)
			return maintEIO
		}
		if n, _ := maintReadStruct(f, int64(off), &ih); n != maintIndexHeader3Size ||
			ih.Identifier != aaru.BlockTypeIndexBlock3 {
			p("ERROR: cannot read index at %d\n", off)
			return maintEIO
		}
		entries, _ := maintReadIndexEntries(f, int64(off)+maintIndexHeader3Size, ih.Entries, size)
		index = append(index, entries...)
		off = ih.Previous
	}

	// Locate the final prefix/suffix DDT2 referenced by the index.
	jobs := []arenaJob{
		{dataType: aaru.DataTypeCdSectorPrefix, name: "prefix", slot: arenaPrefixSlot},
		{dataType: aaru.DataTypeCdSectorSuffix, name: "suffix", slot: arenaSuffixSlot},
	}
	for _, e := range index {
		if e.BlockType != aaru.BlockTypeDeDuplicationTable2 {
			continue
		}
		switch e.DataType {
		case aaru.DataTypeCdSectorPrefix:
			jobs[0].finalDDT = e.Offset
		case aaru.DataTypeCdSectorSuffix:
			jobs[1].finalDDT = e.Offset
		}
	}

	if jobs[0].finalDDT == 0 && jobs[1].finalDDT == 0 {
		p("Image has no CD sector prefix/suffix DDT2; nothing to repair.\n")
		return 0
	}

	gens := scanArenaGenerations(img)
	p("Found %d prefix/suffix generations.\n", len(gens))

	rc := 0
	for j := range jobs {
		job := &jobs[j]
		if job.finalDDT == 0 {
			continue
		}

		var final *arenaGeneration
		for g := range gens {
			if gens[g].dataType == job.dataType && gens[g].ddtOffset == job.finalDDT {
				final = &gens[g]
			}
		}
		if final == nil {
			p("WARNING: final %s DDT2 could not be decoded; skipping.\n", job.name)
			continue
		}

		// The final DDT2 decoded, so its header is inside the file.
		var templ aaru.DDTHeader2
		_, _ = binary.Decode(img[job.finalDDT:], binary.LittleEndian, &templ)

		arena, ddt, recovered, lost := rebuildArena(gens, job.dataType, final.ddt, job.slot)

		p("  %s: %d custom slots recovered, %d unrecoverable (marked not-dumped).\n", job.name, recovered, lost)

		if !dryRun {
			job.newBlockOff, err = appendArenaBlock(f, job.dataType, arena, templ.BlockAlignmentShift)
			if err == nil {
				job.newDDTOff, err = appendArenaDDT(f, templ, job.dataType, ddt)
			}
			if err != nil {
				rc = maintEIO
				break
			}
		}
	}

	switch {
	case !dryRun && rc == 0:
		// Rewrite the index pointing at the new blocks and update the header.
		for i := range index {
			e := &index[i]
			switch {
			case e.BlockType == aaru.BlockTypeDataBlock && e.DataType == aaru.DataTypeCdSectorPrefix &&
				jobs[0].newBlockOff != 0:
				e.Offset = jobs[0].newBlockOff
			case e.BlockType == aaru.BlockTypeDeDuplicationTable2 && e.DataType == aaru.DataTypeCdSectorPrefix &&
				jobs[0].newDDTOff != 0:
				e.Offset = jobs[0].newDDTOff
			case e.BlockType == aaru.BlockTypeDataBlock && e.DataType == aaru.DataTypeCdSectorSuffix &&
				jobs[1].newBlockOff != 0:
				e.Offset = jobs[1].newBlockOff
			case e.BlockType == aaru.BlockTypeDeDuplicationTable2 && e.DataType == aaru.DataTypeCdSectorSuffix &&
				jobs[1].newDDTOff != 0:
				e.Offset = jobs[1].newDDTOff
			}
		}

		end, err := maintFileSize(f)
		if err != nil {
			rc = maintEIO
			break
		}
		newIndex := maintAlignUp(uint64(end), header.BlockAlignmentShift)

		entryBytes, _ := binary.Append(nil, binary.LittleEndian, index)
		ih := aaru.IndexHeader3{
			Identifier: aaru.BlockTypeIndexBlock3,
			Entries:    uint64(len(index)),
			CRC64:      crc64.Checksum(entryBytes),
			Previous:   0, // single self-contained index segment
		}
		buf, _ := binary.Append(nil, binary.LittleEndian, ih)
		if _, err := f.WriteAt(append(buf, entryBytes...), int64(newIndex)); err != nil {
			rc = maintEIO
		} else {
			header.IndexOffset = newIndex
			if _, err := maintWriteStruct(f, 0, header); err != nil {
				rc = maintEIO
			}
		}

		if rc == 0 {
			p("\nRepair written. New index at %d.\n", newIndex)
		}
	case dryRun:
		p("\nDry run only; no changes written.\n")
	}

	if rc != 0 {
		p("ERROR: repair failed (%d)\n", rc)
	}
	return rc
}
