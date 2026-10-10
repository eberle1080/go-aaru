package aaru

import (
	"fmt"
	"math"
)

// This file implements the metadata blocks (processMetadataBlock,
// processGeometryBlock, processCICMBlock, processJSONBlock) and their
// close-time writers (writeGeometryBlock, writeMetadataBlock,
// writeCICMBlock, writeJSONBlock).

// metadataStrings returns, for every MetadataField in order, the stored
// string and the offset and length fields of the metadata block header that
// describe it.
func (img *Image) metadataStrings() [metadataFieldCount]struct {
	data   *[]byte
	offset *uint32
	length *uint32
} {
	h := &img.metadataBlockHeader
	type ref = struct {
		data   *[]byte
		offset *uint32
		length *uint32
	}
	return [metadataFieldCount]ref{
		{&img.creator, &h.CreatorOffset, &h.CreatorLength},
		{&img.comments, &h.CommentsOffset, &h.CommentsLength},
		{&img.mediaTitle, &h.MediaTitleOffset, &h.MediaTitleLength},
		{&img.mediaManufacturer, &h.MediaManufacturerOffset, &h.MediaManufacturerLength},
		{&img.mediaModel, &h.MediaModelOffset, &h.MediaModelLength},
		{&img.mediaSerialNumber, &h.MediaSerialNumberOffset, &h.MediaSerialNumberLength},
		{&img.mediaBarcode, &h.MediaBarcodeOffset, &h.MediaBarcodeLength},
		{&img.mediaPartNumber, &h.MediaPartNumberOffset, &h.MediaPartNumberLength},
		{&img.driveManufacturer, &h.DriveManufacturerOffset, &h.DriveManufacturerLength},
		{&img.driveModel, &h.DriveModelOffset, &h.DriveModelLength},
		{&img.driveSerialNumber, &h.DriveSerialNumberOffset, &h.DriveSerialNumberLength},
		{&img.driveFirmwareRevision, &h.DriveFirmwareRevisionOffset, &h.DriveFirmwareRevisionLength},
	}
}

// processMetadataBlock loads the metadata block (META) described by e.
// Strings whose offset and length do not fit in the block are ignored.
// Only the bytes of the block itself are read (blockSize includes the
// header).
func (img *Image) processMetadataBlock(e IndexEntry) error {
	var hb [metadataBlockHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		img.metadataBlockHeader = MetadataBlockHeader{}
		return fmt.Errorf("reading metadata block header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[MetadataBlockHeader](hb[:])
	if h.Identifier != e.BlockType {
		img.metadataBlockHeader = MetadataBlockHeader{}
		return fmt.Errorf("metadata block at %d has identifier %#x: %w", e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	img.metadataBlockHeader = h
	img.info.ImageSize += uint64(h.BlockSize)

	block, err := img.readAt(e.Offset, max(uint64(h.BlockSize), metadataBlockHeaderSize))
	if err != nil {
		img.metadataBlockHeader = MetadataBlockHeader{}
		return fmt.Errorf("reading metadata block at %d: %w: %w", e.Offset, ErrCannotReadBlock, err)
	}
	img.metadataBlock = block

	if h.MediaSequence > 0 && h.LastMediaSequence > 0 {
		img.mediaSequence = h.MediaSequence
		img.lastMediaSequence = h.LastMediaSequence
	}

	for _, s := range img.metadataStrings() {
		off, n := uint64(*s.offset), uint64(*s.length)
		if n > 0 && off+n <= uint64(h.BlockSize) && off+n <= uint64(len(block)) {
			*s.data = append([]byte(nil), block[off:off+n]...)
		}
	}
	return nil
}

// processGeometryBlock loads the logical geometry block (GEOM).
func (img *Image) processGeometryBlock(e IndexEntry) error {
	var b [geometryBlockHeaderSize]byte
	if err := img.readFullAt(b[:], e.Offset); err != nil {
		img.geometry = GeometryBlockHeader{}
		return fmt.Errorf("reading geometry block at %d: %w", e.Offset, ErrCannotReadBlock)
	}
	g, _ := unmarshal[GeometryBlockHeader](b[:])
	if g.Identifier != BlockTypeGeometryBlock {
		img.geometry = GeometryBlockHeader{}
		return fmt.Errorf("geometry block at %d has identifier %#x: %w", e.Offset, uint32(g.Identifier),
			ErrCannotReadBlock)
	}
	img.geometry = g
	img.info.ImageSize += geometryBlockHeaderSize
	img.cylinders = g.Cylinders
	img.heads = g.Heads
	img.sectorsPerTrack = g.SectorsPerTrack
	return nil
}

// readLengthPrefixedBlock reads a CICM or JSON metadata block: an 8-byte
// header (identifier, length) followed by length bytes. A wrong identifier
// is treated as an empty block and the length is added
// to ImageSize before the payload is read.
func (img *Image) readLengthPrefixedBlock(e IndexEntry, want BlockType) ([]byte, error) {
	var b [cicmBlockHeaderSize]byte
	if err := img.readFullAt(b[:], e.Offset); err != nil {
		return nil, fmt.Errorf("reading %v header at %d: %w", want, e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[CICMBlockHeader](b[:])
	if h.Identifier != want {
		return nil, fmt.Errorf("%v at %d has identifier %#x: %w", want, e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	img.info.ImageSize += uint64(h.Length)
	payload, err := img.readAt(e.Offset+cicmBlockHeaderSize, uint64(h.Length))
	if err != nil {
		return nil, fmt.Errorf("reading %v at %d: %w: %w", want, e.Offset, ErrCannotReadBlock, err)
	}
	return payload, nil
}

// processCICMBlock loads the CICM XML metadata block.
func (img *Image) processCICMBlock(e IndexEntry) error {
	b, err := img.readLengthPrefixedBlock(e, BlockTypeCicmBlock)
	img.cicmBlock = b
	return err
}

// processJSONBlock loads the Aaru JSON metadata block.
func (img *Image) processJSONBlock(e IndexEntry) error {
	b, err := img.readLengthPrefixedBlock(e, BlockTypeAaruMetadataJsonBlock)
	img.jsonBlock = b
	return err
}

// writeGeometryBlock writes the geometry block, if any, at the aligned end
// of the file.
func (img *Image) writeGeometryBlock() error {
	if img.geometry.Identifier != BlockTypeGeometryBlock {
		return nil
	}
	off := img.closeAppendOffset()
	if err := img.writeAt(marshal(&img.geometry), off); err != nil {
		return fmt.Errorf("writing geometry block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeGeometryBlock, 0, off)
	return nil
}

// metadataEmpty reports whether the metadata block header describes no
// media sequence and no string (the condition under which the clear
// functions drop the block).
func (img *Image) metadataEmpty() bool {
	h := &img.metadataBlockHeader
	if h.MediaSequence != 0 || h.LastMediaSequence != 0 {
		return false
	}
	for _, s := range img.metadataStrings() {
		if *s.length != 0 {
			return false
		}
	}
	return true
}

// writeMetadataBlock serializes the metadata block:
// the header followed by the present strings in field order, at the aligned
// end of the file. The string offsets of the header are updated for the
// strings written; the others keep their previous values.
//
// When there is nothing to write, the index entry of any metadata block of
// a resumed image is removed, so that
// clearing all metadata of a resumed image takes effect (writeMetadataBlock
// is only called when the metadata changed).
func (img *Image) writeMetadataBlock() error {
	h := &img.metadataBlockHeader
	strs := img.metadataStrings()
	if h.Identifier != BlockTypeMetadataBlock && h.MediaSequence == 0 && h.LastMediaSequence == 0 {
		empty := true
		for _, s := range strs {
			if *s.data != nil {
				empty = false
				break
			}
		}
		if empty {
			img.removeStaleIndexEntries(BlockTypeMetadataBlock)
			return nil
		}
	}

	size := uint64(metadataBlockHeaderSize)
	for _, s := range strs {
		size += uint64(*s.length)
	}
	if size > math.MaxUint32 {
		return fmt.Errorf("metadata block of %d bytes: %w", size, ErrIncorrectDataSize)
	}
	h.BlockSize = uint32(size)
	h.Identifier = BlockTypeMetadataBlock

	buf := make([]byte, size)
	pos := uint32(metadataBlockHeaderSize)
	for _, s := range strs {
		if *s.data == nil || *s.length == 0 {
			continue
		}
		copy(buf[pos:pos+*s.length], *s.data)
		*s.offset = pos
		pos += *s.length
	}
	copy(buf, marshal(h))

	off := img.closeAppendOffset()
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing metadata block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeMetadataBlock, 0, off)
	return nil
}

// removeStaleIndexEntries removes the index entries of an optional block
// that is no longer present (only resumed images have such entries).
func (img *Image) removeStaleIndexEntries(bt BlockType) {
	n := len(img.indexEntries)
	img.removeIndexEntries(bt, 0)
	if len(img.indexEntries) != n {
		img.dirty.indexBlock = true
	}
}

// writeLengthPrefixedBlock writes a CICM or JSON metadata block.
func (img *Image) writeLengthPrefixedBlock(bt BlockType, payload []byte) error {
	off := img.closeAppendOffset()
	buf := appendMarshal(make([]byte, 0, cicmBlockHeaderSize+len(payload)),
		&CICMBlockHeader{Identifier: bt, Length: uint32(len(payload))})
	buf = append(buf, payload...)
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing %v: %w: %w", bt, ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(bt, 0, off)
	return nil
}

// writeCICMBlock writes the CICM XML metadata block, if any.
func (img *Image) writeCICMBlock() error {
	if len(img.cicmBlock) == 0 {
		return nil
	}
	return img.writeLengthPrefixedBlock(BlockTypeCicmBlock, img.cicmBlock)
}

// writeJSONBlock writes the Aaru JSON metadata block, if any.
// As for the metadata block, an emptied JSON block
// of a resumed image is removed from the index.
func (img *Image) writeJSONBlock() error {
	if len(img.jsonBlock) == 0 {
		img.removeStaleIndexEntries(BlockTypeAaruMetadataJsonBlock)
		return nil
	}
	return img.writeLengthPrefixedBlock(BlockTypeAaruMetadataJsonBlock, img.jsonBlock)
}
