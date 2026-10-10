package aaru

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/eberle1080/go-aaru/crc64"
)

// Image verification.

// verifyChunkSize is the read size used while checksumming blocks.
const verifyChunkSize = 1 << 20

// Verify checks the integrity of the image without decompressing it:
// the index CRC64, then, for every indexed data block,
// deduplication table, tracks block and data position measurement block, the
// CRC64 of its stored payload. It also requires a user data DDT or flux data
// to be indexed.
//
// It returns nil when everything matches, an error wrapping
// ErrInvalidBlockCRC on a checksum mismatch, ErrCannotReadHeader /
// ErrCannotReadIndex / ErrCannotReadBlock when structures cannot be read.
func (img *Image) Verify() error {
	if img == nil {
		return ErrNotAaruFormat
	}
	if img.closed {
		return ErrClosed
	}
	if img.r == nil {
		return ErrNotAaruFormat
	}
	off := img.header.IndexOffset
	var sig [4]byte
	if off > math.MaxInt64 {
		return fmt.Errorf("index offset %d: %w", off, ErrCannotReadHeader)
	}
	if err := img.readFullAt(sig[:], off); err != nil {
		return fmt.Errorf("reading index signature at %d: %w", off, ErrCannotReadHeader)
	}
	bt := BlockType(binary.LittleEndian.Uint32(sig[:]))
	if bt != BlockTypeIndexBlock && bt != BlockTypeIndexBlock2 && bt != BlockTypeIndexBlock3 {
		return fmt.Errorf("incorrect index signature %q: %w", sig[:], ErrCannotReadIndex)
	}
	if err := img.verifyIndex(bt); err != nil {
		return err
	}

	var entries []IndexEntry
	var err error
	switch bt {
	case BlockTypeIndexBlock:
		entries, err = img.processIndexV1()
	case BlockTypeIndexBlock2:
		entries, err = img.processIndexV2()
	default:
		entries, err = img.processIndexV3()
	}
	if err != nil {
		return fmt.Errorf("processing index: %w: %w", ErrCannotReadIndex, err)
	}

	buf := make([]byte, verifyChunkSize)
	v1 := img.header.ImageMajorVersion <= FormatVersionV1
	hasDDT, hasFlux := false, false

	for _, e := range entries {
		if e.BlockType == BlockTypeDeDuplicationTable || e.BlockType == BlockTypeDeDuplicationTable2 {
			hasDDT = true
		}
		if e.BlockType == BlockTypeFluxDataBlock {
			hasFlux = true
		}
		if e.Offset > math.MaxInt64 {
			return fmt.Errorf("block offset %d: %w", e.Offset, ErrCannotReadBlock)
		}

		var (
			hdrSize    uint64
			crcLength  uint64
			compr      CompressionType
			skipProps  bool
			expected   uint64
			swap       = v1
			what       string
			headerRead []byte
		)
		switch e.BlockType {
		case BlockTypeDataBlock:
			hdrSize, what = blockHeaderSize, "data block"
		case BlockTypeDeDuplicationTable:
			hdrSize, what = ddtHeaderSize, "DDT block"
		case BlockTypeDeDuplicationTable2:
			hdrSize, what = ddtHeader2Size, "DDT2 block"
		case BlockTypeTracksBlock:
			hdrSize, what = tracksHeaderSize, "tracks block"
		case BlockTypeDataPositionMeasurementBlock:
			hdrSize, what = dpmHeaderSize, "DPM block"
		default:
			continue
		}
		headerRead = buf[:hdrSize]
		if err := img.readFullAt(headerRead, e.Offset); err != nil {
			return fmt.Errorf("reading %s header at %d: %w", what, e.Offset, ErrCannotReadBlock)
		}

		switch e.BlockType {
		case BlockTypeDataBlock:
			h, _ := decodeBlockHeader(headerRead)
			crcLength, compr, expected = uint64(h.CmpLength), h.Compression, h.CmpCRC64
			// Version 1 included the LZMA properties in the CRC; later ones do not.
			skipProps = (compr == CompressionLzma || compr == CompressionLzmaCst) && !v1
		case BlockTypeDeDuplicationTable:
			h, _ := unmarshal[DDTHeader](headerRead)
			crcLength, compr, expected = h.CmpLength, h.Compression, h.CmpCRC64
			skipProps = (compr == CompressionLzma || compr == CompressionLzmaCst) && !v1
		case BlockTypeDeDuplicationTable2:
			h, _ := unmarshal[DDTHeader2](headerRead)
			crcLength, compr, expected = h.CmpLength, h.Compression, h.CmpCRC64
			skipProps = compr == CompressionLzma || compr == CompressionLzmaCst
			swap = false
		case BlockTypeTracksBlock:
			h, _ := unmarshal[TracksHeader](headerRead)
			crcLength, expected = uint64(h.Entries)*trackEntrySize, h.CRC64
		case BlockTypeDataPositionMeasurementBlock:
			h, _ := unmarshal[DPMHeader](headerRead)
			if h.Identifier != BlockTypeDataPositionMeasurementBlock {
				return fmt.Errorf("reading DPM header at %d: %w", e.Offset, ErrCannotReadBlock)
			}
			if h.Length != verifyDPMPayloadLength(&h) {
				return fmt.Errorf("DPM header length %d does not match its contents: %w", h.Length, ErrCannotReadBlock)
			}
			crcLength, expected = h.Length, h.CRC64
			swap = false
		}

		pos := e.Offset + hdrSize
		if skipProps {
			// The properties must be readable even though they are skipped.
			if err := img.readFullAt(buf[:LZMAPropertiesLength], pos); err != nil {
				return fmt.Errorf("reading LZMA properties of %s at %d: %w", what, e.Offset, ErrCannotReadBlock)
			}
			pos += LZMAPropertiesLength
			crcLength -= LZMAPropertiesLength // may wrap; the read below then fails
		}
		crc, err := img.streamCRC64(pos, crcLength, buf)
		if err != nil {
			return fmt.Errorf("reading %s at %d: %w", what, e.Offset, ErrCannotReadBlock)
		}
		if swap {
			crc = bswap64(crc)
		}
		if crc != expected {
			return fmt.Errorf("%s at %d: expected CRC %#016x, got %#016x: %w", what, e.Offset, expected, crc, ErrInvalidBlockCRC)
		}
	}

	if !hasDDT && !hasFlux {
		return fmt.Errorf("image contains neither a user data DDT nor flux data: %w", ErrCannotReadIndex)
	}
	return nil
}

// streamCRC64 computes the CRC64 of n bytes at off, reading in chunks of
// len(buf) bytes.
func (img *Image) streamCRC64(off, n uint64, buf []byte) (uint64, error) {
	if img.size >= 0 && (off > uint64(img.size) || n > uint64(img.size)-off) {
		return 0, fmt.Errorf("%d bytes at %d past the end of the image", n, off)
	}
	h := crc64.New()
	for n > 0 {
		chunk := uint64(len(buf))
		if n < chunk {
			chunk = n
		}
		if err := img.readFullAt(buf[:chunk], off); err != nil {
			return 0, err
		}
		h.Write(buf[:chunk])
		off += chunk
		n -= chunk
	}
	return h.Sum64(), nil
}

// verifyDPMPayloadLength is the payload length a DPM header describes.
func verifyDPMPayloadLength(h *DPMHeader) uint64 {
	return uint64(h.LayerEnds)*8 + uint64(h.Entries)*dpmEntrySize + uint64(h.Calibrations)*dpmCalibrationSize
}
