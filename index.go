package aaru

import (
	"errors"
	"fmt"

	"github.com/eberle1080/go-aaru/crc64"
)

// Index parsing and verification.

var (
	errBadIndexIdentifier = errors.New("incorrect index identifier")
	errIndexTooLarge      = errors.New("index entry count exceeds the image size")
	errSubindexLoop       = errors.New("IDX3 subindexes nest too deeply or form a loop")
)

// maxSubindexDepth bounds IDX3 subindex nesting (a self-referencing subindex
// would otherwise recurse without limit).
const maxSubindexDepth = 64

// readIndexEntries reads n packed index entries at off, validating the
// count against the image size first.
func (img *Image) readIndexEntries(off, n uint64) ([]IndexEntry, error) {
	if n > uint64(img.size)/indexEntrySize {
		return nil, errIndexTooLarge
	}
	raw, err := img.readAt(off, n*indexEntrySize)
	if err != nil {
		return nil, fmt.Errorf("reading %d index entries at %d: %w", n, off, err)
	}
	return unmarshalSlice[IndexEntry](raw, int(n))
}

// processIndexV1 reads a version 1 index (INDX).
func (img *Image) processIndexV1() ([]IndexEntry, error) {
	off := img.header.IndexOffset
	var hb [indexHeaderSize]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return nil, fmt.Errorf("reading index header at %d: %w", off, err)
	}
	h, _ := unmarshal[IndexHeader](hb[:])
	if h.Identifier != BlockTypeIndexBlock {
		return nil, errBadIndexIdentifier
	}
	return img.readIndexEntries(off+indexHeaderSize, uint64(h.Entries))
}

// processIndexV2 reads a version 2 index (IDX2).
func (img *Image) processIndexV2() ([]IndexEntry, error) {
	off := img.header.IndexOffset
	var hb [indexHeader2Size]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return nil, fmt.Errorf("reading index header at %d: %w", off, err)
	}
	h, _ := unmarshal[IndexHeader2](hb[:])
	if h.Identifier != BlockTypeIndexBlock2 {
		return nil, errBadIndexIdentifier
	}
	return img.readIndexEntries(off+indexHeader2Size, h.Entries)
}

// processIndexV3 reads a version 3 index (IDX3), replacing every entry that
// points to another IDX3 block (a subindex) by that subindex's entries,
// recursively. The Previous field
// is not followed.
//
// Each index is read as a whole, so entries following a subindex are read
// correctly.
func (img *Image) processIndexV3() ([]IndexEntry, error) {
	var out []IndexEntry
	if err := img.appendIndexV3(&out, img.header.IndexOffset, 0); err != nil {
		return nil, err
	}
	if out == nil {
		out = []IndexEntry{}
	}
	return out, nil
}

func (img *Image) appendIndexV3(out *[]IndexEntry, off uint64, depth int) error {
	if depth > maxSubindexDepth {
		return errSubindexLoop
	}
	var hb [indexHeader3Size]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return fmt.Errorf("reading index header at %d: %w", off, err)
	}
	h, _ := unmarshal[IndexHeader3](hb[:])
	if h.Identifier != BlockTypeIndexBlock3 {
		return errBadIndexIdentifier
	}
	entries, err := img.readIndexEntries(off+indexHeader3Size, h.Entries)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.BlockType == BlockTypeIndexBlock3 {
			if err := img.appendIndexV3(out, e.Offset, depth+1); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, e)
	}
	return nil
}

// verifyIndex checks the CRC64 of the index entries at header.IndexOffset.
// Subindexes are not verified.
func (img *Image) verifyIndex(bt BlockType) error {
	off := img.header.IndexOffset
	var (
		hdrSize  uint64
		entries  uint64
		crc      uint64
		expected BlockType
	)
	switch bt {
	case BlockTypeIndexBlock:
		hdrSize, expected = indexHeaderSize, BlockTypeIndexBlock
	case BlockTypeIndexBlock2:
		hdrSize, expected = indexHeader2Size, BlockTypeIndexBlock2
	default:
		hdrSize, expected = indexHeader3Size, BlockTypeIndexBlock3
	}
	hb := make([]byte, hdrSize)
	if err := img.readFullAt(hb, off); err != nil {
		return fmt.Errorf("reading index header at %d: %w", off, ErrCannotReadHeader)
	}
	switch bt {
	case BlockTypeIndexBlock:
		h, _ := unmarshal[IndexHeader](hb)
		if h.Identifier != expected {
			return fmt.Errorf("incorrect index identifier: %w", ErrCannotReadIndex)
		}
		entries, crc = uint64(h.Entries), h.CRC64
	case BlockTypeIndexBlock2:
		h, _ := unmarshal[IndexHeader2](hb)
		if h.Identifier != expected {
			return fmt.Errorf("incorrect index identifier: %w", ErrCannotReadIndex)
		}
		entries, crc = h.Entries, h.CRC64
	default:
		h, _ := unmarshal[IndexHeader3](hb)
		if h.Identifier != expected {
			return fmt.Errorf("incorrect index identifier: %w", ErrCannotReadIndex)
		}
		entries, crc = h.Entries, h.CRC64
	}
	if entries > uint64(img.size)/indexEntrySize {
		return fmt.Errorf("index entry count %d too large: %w", entries, ErrCannotReadIndex)
	}
	raw, err := img.readAt(off+hdrSize, entries*indexEntrySize)
	if err != nil {
		return fmt.Errorf("reading index entries: %w", ErrCannotReadIndex)
	}
	if !img.crcMatches(crc64.Checksum(raw), crc) {
		return fmt.Errorf("index CRC mismatch: %w", ErrInvalidBlockCRC)
	}
	return nil
}
