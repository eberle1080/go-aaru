package aaru

import (
	"fmt"

	"github.com/eberle1080/go-aaru/crc64"
)

// This file implements the write half of the version 2 deduplication
// tables: setDDTEntryV2, setDDTSingleLevelV2, setDDTMultiLevelV2 and
// setDDTTape, plus the serialization of secondary tables shared with
// close_write.go.

// ddtPointerMask selects the pointer part of a DDT v2 entry (the top nibble is
// the sector status).
const ddtPointerMask = 0x0FFFFFFFFFFFFFFF

// makeDDTEntry builds the pointer part of a DDT v2 entry for the sector at
// item offset inside the block written at blockOffset.
func (img *Image) makeDDTEntry(offset, blockOffset uint64) (uint64, error) {
	h := &img.userDataDDTHeader
	blockIndex := blockOffset >> h.BlockAlignmentShift
	e := offset&(uint64(1)<<h.DataShift-1) | blockIndex<<h.DataShift
	if e > ddtPointerMask {
		return 0, fmt.Errorf("DDT overflow: media does not fit in the table: %w", ErrCannotSetDDTEntry)
	}
	return e, nil
}

// setDDTEntryV2 records where a sector is stored.
// *entry is either an existing entry (deduplicated sector, only its status
// is replaced) or 0 to build a new one from offset and blockOffset; the
// resulting entry is returned through it.
//
// Tape images use the sparse tape table.
func (img *Image) setDDTEntryV2(addr uint64, negative bool, offset, blockOffset uint64, status SectorStatus,
	entry *uint64) error {
	var err error
	switch {
	case img.isTape:
		err = img.setDDTTape(addr, offset, blockOffset, status, entry)
	case img.userDataDDTHeader.TableShift > 0:
		err = img.setDDTMultiLevelV2(addr, negative, offset, blockOffset, status, entry)
	default:
		err = img.setDDTSingleLevelV2(addr, negative, offset, blockOffset, status, entry)
	}
	if err != nil {
		return fmt.Errorf("setting DDT entry for sector %d: %w: %w", addr, ErrCannotSetDDTEntry, err)
	}
	return nil
}

// setDDTSingleLevelV2 updates a single-level table.
func (img *Image) setDDTSingleLevelV2(addr uint64, negative bool, offset, blockOffset uint64,
	status SectorStatus, entry *uint64) error {
	if img.userDataDDTHeader.TableShift != 0 {
		return fmt.Errorf("multi-level table in single-level path")
	}
	addr = img.correctedAddress(addr, negative)

	if *entry == 0 {
		e, err := img.makeDDTEntry(offset, blockOffset)
		if err != nil {
			return err
		}
		*entry = e
	}
	// The status can differ from the deduplicated sector's.
	*entry = *entry&ddtPointerMask | uint64(status)<<60

	if addr >= uint64(len(img.userDataDDT2)) {
		return fmt.Errorf("table index %d of %d: %w", addr, len(img.userDataDDT2), ErrSectorOutOfBounds)
	}
	img.userDataDDT2[addr] = *entry
	img.dirty.singleLevelDDT = true
	return nil
}

// setDDTMultiLevelV2 updates a two-level table.
// One secondary table is cached in memory; moving to a sector of another
// table writes the cached one at the end of the file and points the primary
// table at it.
//
// A secondary table that already exists on disk (resumed images) is loaded
// instead of being replaced by an empty one, so no other entry of that table
// is lost; and evicting a table loaded from disk points the primary entry of
// that table (not of the sector being written) at the new copy, writing its
// header before its LZMA properties.
func (img *Image) setDDTMultiLevelV2(addr uint64, negative bool, offset, blockOffset uint64,
	status SectorStatus, entry *uint64) error {
	h := &img.userDataDDTHeader
	if h.TableShift == 0 {
		return fmt.Errorf("single-level table in multi-level path")
	}
	addr = img.correctedAddress(addr, negative)

	// Step 1: the secondary table the sector belongs to.
	perEntry := uint64(1) << h.TableShift
	position := addr / perEntry
	if position >= uint64(len(img.userDataDDT2)) {
		return fmt.Errorf("primary table index %d of %d: %w", position, len(img.userDataDDT2), ErrSectorOutOfBounds)
	}
	secondaryOffset := img.userDataDDT2[position] << h.BlockAlignmentShift

	// Step 2: it is the cached table.
	if img.cachedSecondaryDDT2 != nil && img.cachedDDTOffset == secondaryOffset && secondaryOffset != 0 {
		img.cachedDDTPosition = position
		return img.setCachedEntry(addr%perEntry, offset, blockOffset, status, entry)
	}

	// Step 2.5: the cached table was never written and belongs elsewhere.
	if img.cachedDDTOffset == 0 && img.cachedSecondaryDDT2 != nil && position != img.cachedDDTPosition {
		if img.blockOpen() {
			if err := img.closeCurrentBlock(); err != nil {
				return err
			}
		}
		eof, err := img.flushCachedSecondaryDDT(img.cachedDDTPosition)
		if err != nil {
			return err
		}
		img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDeDuplicationTableSecondary,
			DataType: DataTypeUserData, Offset: eof})
		img.dirty.indexBlock = true
		img.dirty.primaryDDT = true

		// The next data block goes after the table.
		img.nextBlockPosition = alignUp(eof+ddtHeader2Size+perEntry*8, h.BlockAlignmentShift)
		blockOffset = img.nextBlockPosition
		offset = 0

		img.cachedSecondaryDDT2 = nil
		img.cachedDDTOffset = 0
		img.cachedDDTPosition = 0
	}

	// Step 3: the cached table came from disk and belongs elsewhere.
	if img.cachedDDTOffset != 0 {
		if img.blockOpen() {
			if err := img.closeCurrentBlock(); err != nil {
				return err
			}
		}
		oldOffset := img.cachedDDTOffset
		eof, err := img.flushCachedSecondaryDDT(img.cachedSecondaryPosition())
		if err != nil {
			return err
		}
		img.removeSecondaryIndexEntry(oldOffset)
		img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDeDuplicationTableSecondary,
			DataType: DataTypeUserData, Offset: eof})
		img.dirty.indexBlock = true
		img.dirty.primaryDDT = true

		img.nextBlockPosition = alignUp(eof+ddtHeader2Size+perEntry*8, h.BlockAlignmentShift)
		blockOffset = img.nextBlockPosition
		offset = 0

		img.cachedSecondaryDDT2 = nil
		img.cachedDDTOffset = 0
	}

	// Step 5: load the sector's table, or start a new one.
	if img.cachedSecondaryDDT2 == nil {
		if secondaryOffset != 0 {
			table, err := img.reloadSecondaryDDT(secondaryOffset, perEntry)
			if err != nil {
				return err
			}
			img.cachedSecondaryDDT2 = table
			img.cachedDDTOffset = secondaryOffset
		} else {
			img.cachedSecondaryDDT2 = make([]uint64, perEntry)
			img.cachedDDTOffset = 0
		}
		img.cachedDDTPosition = position
	}

	// Step 6: update the entry.
	return img.setCachedEntry(addr%perEntry, offset, blockOffset, status, entry)
}

// setCachedEntry stores an entry in the cached secondary table.
func (img *Image) setCachedEntry(index, offset, blockOffset uint64, status SectorStatus, entry *uint64) error {
	if *entry == 0 {
		e, err := img.makeDDTEntry(offset, blockOffset)
		if err != nil {
			return err
		}
		*entry = e
	}
	*entry = *entry&ddtPointerMask | uint64(status)<<60
	if index >= uint64(len(img.cachedSecondaryDDT2)) {
		return fmt.Errorf("secondary table index %d: %w", index, ErrSectorOutOfBounds)
	}
	img.cachedSecondaryDDT2[index] = *entry
	img.dirty.secondaryDDT = true
	return nil
}

// cachedSecondaryPosition returns the primary table index of the cached
// secondary table. Tables cached by the read path only record their file
// offset, so it is looked up in the primary table when needed.
func (img *Image) cachedSecondaryPosition() uint64 {
	h := &img.userDataDDTHeader
	p := img.cachedDDTPosition
	if img.cachedDDTOffset == 0 {
		return p
	}
	if p < uint64(len(img.userDataDDT2)) && img.userDataDDT2[p]<<h.BlockAlignmentShift == img.cachedDDTOffset {
		return p
	}
	for i, e := range img.userDataDDT2 {
		if e != 0 && e<<h.BlockAlignmentShift == img.cachedDDTOffset {
			return uint64(i)
		}
	}
	return p
}

// removeSecondaryIndexEntry removes the first index entry of a secondary
// table at off.
func (img *Image) removeSecondaryIndexEntry(off uint64) {
	for i, e := range img.indexEntries {
		if e.Offset == off && (e.BlockType == BlockTypeDeDuplicationTableSecondary ||
			e.BlockType == BlockTypeDeDuplicationTableSAlpha) {
			img.indexEntries = append(img.indexEntries[:i], img.indexEntries[i+1:]...)
			return
		}
	}
}

// flushCachedSecondaryDDT writes the cached secondary table (belonging to
// primary entry position) at the aligned end of the file, points the
// primary table at it and rewrites the primary table data in place. It
// returns the table offset.
func (img *Image) flushCachedSecondaryDDT(position uint64) (uint64, error) {
	h := &img.userDataDDTHeader
	perEntry := uint64(1) << h.TableShift
	eof := alignUp(uint64(img.size), h.BlockAlignmentShift)

	hdr := DDTHeader2{
		Identifier:          BlockTypeDeDuplicationTableSecondary,
		Type:                DataTypeUserData,
		Levels:              h.Levels,
		TableLevel:          h.TableLevel + 1,
		PreviousLevelOffset: img.primaryDDTOffset,
		Negative:            h.Negative,
		Blocks:              perEntry,
		Overflow:            h.Overflow,
		Start:               position * perEntry,
		BlockAlignmentShift: h.BlockAlignmentShift,
		DataShift:           h.DataShift,
		TableShift:          0,
		Entries:             perEntry,
	}
	table := img.cachedSecondaryDDT2
	if uint64(len(table)) != perEntry {
		t := make([]uint64, perEntry)
		copy(t, table)
		table = t
	}
	if err := img.writeDDT2At(eof, &hdr, encodeUint64s(table), img.compressPayload); err != nil {
		return 0, err
	}

	if position >= uint64(len(img.userDataDDT2)) {
		return 0, fmt.Errorf("primary table index %d: %w", position, ErrSectorOutOfBounds)
	}
	img.userDataDDT2[position] = eof >> h.BlockAlignmentShift

	if err := img.writeAt(encodeUint64s(img.userDataDDT2), img.primaryDDTOffset+ddtHeader2Size); err != nil {
		return 0, fmt.Errorf("flushing primary DDT: %w: %w", ErrCannotWriteHeader, err)
	}
	return eof, nil
}

// writeDDT2At fills the length, CRC and compression fields of hdr for the
// table payload, compresses it with compress and writes header and payload
// at off.
func (img *Image) writeDDT2At(off uint64, hdr *DDTHeader2, payload []byte,
	compress func([]byte, bool) encodedPayload) error {
	hdr.Length = uint64(len(payload))
	hdr.CRC64 = crc64.Checksum(payload)
	p := compress(payload, false)
	hdr.Compression = p.Compression
	hdr.CmpLength = p.cmpLength()
	if p.Compression == CompressionNone {
		hdr.CmpCRC64 = hdr.CRC64
	} else {
		hdr.CmpCRC64 = crc64.Checksum(p.Stream)
	}
	buf := make([]byte, 0, ddtHeader2Size+int(hdr.CmpLength))
	buf = appendMarshal(buf, hdr)
	buf = append(buf, p.Props...)
	buf = append(buf, p.Stream...)
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing DDT at %d: %w: %w", off, ErrCannotWriteHeader, err)
	}
	return nil
}

// reloadSecondaryDDT reads the secondary table at off.
func (img *Image) reloadSecondaryDDT(off, perEntry uint64) ([]uint64, error) {
	hb, err := img.readAt(off, ddtHeader2Size)
	if err != nil {
		return nil, fmt.Errorf("reading secondary DDT header at %d: %w: %w", off, ErrCannotReadBlock, err)
	}
	hdr, err := unmarshal[DDTHeader2](hb)
	if err != nil {
		return nil, fmt.Errorf("secondary DDT at %d: %w: %w", off, ErrCannotReadBlock, err)
	}
	if hdr.Identifier != BlockTypeDeDuplicationTableSecondary && hdr.Identifier != BlockTypeDeDuplicationTableSAlpha &&
		hdr.Identifier != BlockTypeDeDuplicationTable2 {
		return nil, fmt.Errorf("block at %d is not a secondary DDT: %w", off, ErrCannotReadBlock)
	}
	if hdr.Length > perEntry*8 || hdr.CmpLength > maxBlockPayload {
		return nil, fmt.Errorf("secondary DDT at %d too big: %w", off, ErrCannotReadBlock)
	}
	raw, err := img.readAt(off+ddtHeader2Size, hdr.CmpLength)
	if err != nil {
		return nil, fmt.Errorf("reading secondary DDT at %d: %w: %w", off, ErrCannotReadBlock, err)
	}
	data, err := decodePayload(hdr.Compression, DataTypeUserData, raw, hdr.Length)
	if err != nil {
		return nil, fmt.Errorf("secondary DDT at %d: %w", off, err)
	}
	if !img.crcMatches(crc64.Checksum(data), hdr.CRC64) {
		return nil, fmt.Errorf("secondary DDT at %d: %w", off, ErrInvalidBlockCRC)
	}
	table := make([]uint64, perEntry)
	copy(table, decodeUint64s(data))
	return table, nil
}

// setDDTTape records a tape block in the sparse tape table.
// Unlike the disk tables, the status of an existing (deduplicated) entry is
// kept.
func (img *Image) setDDTTape(addr uint64, offset, blockOffset uint64, status SectorStatus, entry *uint64) error {
	if !img.isTape {
		return fmt.Errorf("not a tape image")
	}
	if *entry == 0 {
		e, err := img.makeDDTEntry(offset, blockOffset)
		if err != nil {
			return err
		}
		*entry = e | uint64(status)<<60
	}
	if img.tapeDDT == nil {
		img.tapeDDT = make(map[uint64]uint64)
	}
	img.tapeDDT[addr] = *entry
	img.dirty.tapeDDT = true
	return nil
}
