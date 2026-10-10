package aaru

import (
	"fmt"
	"io"

	"github.com/eberle1080/go-aaru/compression/cst"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/compression/zstd"
	"github.com/eberle1080/go-aaru/crc64"
)

// maxBlockPayload bounds single allocations driven by on-disk length fields
// that are not otherwise limited by the file size (decompressed lengths).
const maxBlockPayload = 1 << 32

// readFullAt fills p from the backing storage at off.
func (img *Image) readFullAt(p []byte, off uint64) error {
	if len(p) == 0 {
		return nil
	}
	if off > uint64(1<<63-1) {
		return io.ErrUnexpectedEOF
	}
	n, err := img.r.ReadAt(p, int64(off))
	if n == len(p) {
		return nil
	}
	if err == nil || err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// readAt reads n bytes at off. n is validated against the file size so that
// corrupt length fields cannot trigger huge allocations.
func (img *Image) readAt(off uint64, n uint64) ([]byte, error) {
	if img.size >= 0 && (off > uint64(img.size) || n > uint64(img.size)-off) {
		return nil, io.ErrUnexpectedEOF
	}
	p := make([]byte, n)
	if err := img.readFullAt(p, off); err != nil {
		return nil, err
	}
	return p, nil
}

// writeAt writes p at off and keeps img.size up to date.
func (img *Image) writeAt(p []byte, off uint64) error {
	if img.w == nil {
		return ErrReadOnly
	}
	if _, err := img.w.WriteAt(p, int64(off)); err != nil {
		return err
	}
	if end := int64(off) + int64(len(p)); end > img.size {
		img.size = end
	}
	return nil
}

// alignmentShift returns the block alignment shift in effect.
func (img *Image) alignmentShift() uint8 {
	if img.noUserDataDDT || img.userDataDDTHeader.BlockAlignmentShift == 0 {
		return img.header.BlockAlignmentShift
	}
	return img.userDataDDTHeader.BlockAlignmentShift
}

// appendOffset returns the aligned end of file, where close-time blocks are
// appended.
func (img *Image) appendOffset() uint64 {
	return alignUp(uint64(img.size), img.alignmentShift())
}

// setIndexEntry removes any index entry with the same block and data type
// and appends a new one pointing at off.
func (img *Image) setIndexEntry(bt BlockType, dt DataType, off uint64) {
	img.removeIndexEntries(bt, dt)
	img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: bt, DataType: dt, Offset: off})
	img.dirty.indexBlock = true
}

// removeIndexEntries removes every index entry with the given block and data
// type.
func (img *Image) removeIndexEntries(bt BlockType, dt DataType) {
	out := img.indexEntries[:0]
	for _, e := range img.indexEntries {
		if e.BlockType == bt && e.DataType == dt {
			continue
		}
		out = append(out, e)
	}
	img.indexEntries = out
}

// readBlockHeaderAt reads and decodes the BlockHeader at off.
func (img *Image) readBlockHeaderAt(off uint64) (BlockHeader, error) {
	var b [blockHeaderSize]byte
	if err := img.readFullAt(b[:], off); err != nil {
		return BlockHeader{}, fmt.Errorf("reading block header at %d: %w: %w", off, ErrCannotReadHeader, err)
	}
	return decodeBlockHeader(b[:])
}

// decodePayload decompresses raw (the on-disk payload, including the LZMA
// properties prefix when applicable) into exactly length bytes.
func decodePayload(c CompressionType, dt DataType, raw []byte, length uint64) ([]byte, error) {
	if length > maxBlockPayload {
		return nil, fmt.Errorf("payload length %d too large: %w", length, ErrCannotDecompressBlock)
	}
	switch c {
	case CompressionNone:
		if uint64(len(raw)) < length {
			return nil, fmt.Errorf("short uncompressed payload: %w", ErrCannotReadBlock)
		}
		return raw[:length:length], nil
	case CompressionLzma, CompressionLzmaCst:
		if c == CompressionLzmaCst && dt != DataTypeCdSubchannel {
			return nil, fmt.Errorf("LZMA+CST on data type %v: %w", dt, ErrUnsupportedCompression)
		}
		if len(raw) < LZMAPropertiesLength {
			return nil, fmt.Errorf("LZMA payload too short: %w", ErrCannotDecompressBlock)
		}
		if length == 0 {
			return []byte{}, nil
		}
		out, err := lzma.DecodePayload(raw, int(length))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCannotDecompressBlock, err)
		}
		if c == CompressionLzmaCst {
			return untransformCST(out)
		}
		return out, nil
	case CompressionZstd, CompressionZstdCst:
		if c == CompressionZstdCst && dt != DataTypeCdSubchannel {
			return nil, fmt.Errorf("Zstd+CST on data type %v: %w", dt, ErrUnsupportedCompression)
		}
		if length == 0 {
			return []byte{}, nil
		}
		out, err := zstd.Decode(raw, int(length))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCannotDecompressBlock, err)
		}
		if uint64(len(out)) != length {
			return nil, fmt.Errorf("zstd produced %d bytes, want %d: %w", len(out), length, ErrCannotDecompressBlock)
		}
		if c == CompressionZstdCst {
			return untransformCST(out)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("compression %v: %w", c, ErrUnsupportedCompression)
	}
}

func untransformCST(b []byte) ([]byte, error) {
	out, err := cst.Untransform(b)
	if err != nil {
		return nil, fmt.Errorf("CST: %w: %w", ErrCannotDecompressBlock, err)
	}
	return out, nil
}

// encodedPayload is a block payload ready to be written.
type encodedPayload struct {
	Compression CompressionType
	Props       []byte // LZMA properties (only for LZMA compression)
	Stream      []byte // compressed stream, or the raw data when uncompressed
}

// cmpLength returns the on-disk payload length (stream + LZMA properties).
func (p *encodedPayload) cmpLength() uint64 { return uint64(len(p.Props) + len(p.Stream)) }

// lzmaOptions returns the LZMA encoder settings used for
// non-audio blocks (level 9, configured dictionary, lc=4 lp=0 pb=2 fb=273).
func (img *Image) lzmaOptions() lzma.Options {
	dict := img.options.Dictionary
	if dict == 0 {
		dict = DefaultOptions().Dictionary
	}
	return lzma.Options{Level: 9, DictSize: dict, LC: 4, LP: 0, PB: 2, FB: 273, NumThreads: img.options.Threads}
}

// compressPayload compresses data according to the image options: LZMA (or
// Zstandard when enabled) when compression is on, optionally after the CD
// Subchannel Transform (subchannel data only). If compression is disabled,
// fails, or does not shrink the data, the payload is stored uncompressed.
func (img *Image) compressPayload(data []byte, useCST bool) encodedPayload {
	none := encodedPayload{Compression: CompressionNone, Stream: data}
	if !img.options.Compress || len(data) == 0 {
		return none
	}
	src := data
	if useCST {
		t, err := cst.Transform(data)
		if err != nil {
			return none
		}
		src = t
	}
	if img.options.Zstd {
		out, err := zstd.Encode(src, img.options.ZstdLevel)
		if err != nil || len(out) >= len(data) {
			return none
		}
		c := CompressionZstd
		if useCST {
			c = CompressionZstdCst
		}
		img.hasZstdBlocks = true
		return encodedPayload{Compression: c, Stream: out}
	}
	stream, props, err := lzma.Encode(src, img.lzmaOptions())
	if err != nil || len(stream) >= len(data) {
		return none
	}
	c := CompressionLzma
	if useCST {
		c = CompressionLzmaCst
	}
	return encodedPayload{Compression: c, Props: props[:], Stream: stream}
}

// writeBlockAt writes a data block (header + payload) at off. hdr's
// Compression, CmpLength and CmpCRC64 are filled from p; the caller sets
// Identifier, Type, SectorSize, Length and CRC64. It returns the on-disk size.
func (img *Image) writeBlockAt(off uint64, hdr *BlockHeader, p encodedPayload) (uint64, error) {
	hdr.Compression = p.Compression
	hdr.CmpLength = uint32(p.cmpLength())
	if p.Compression == CompressionNone {
		hdr.CmpCRC64 = hdr.CRC64
	} else {
		hdr.CmpCRC64 = crc64.Checksum(p.Stream)
	}
	buf := make([]byte, 0, blockHeaderSize+int(p.cmpLength()))
	buf = hdr.appendTo(buf)
	buf = append(buf, p.Props...)
	buf = append(buf, p.Stream...)
	if err := img.writeAt(buf, off); err != nil {
		return 0, fmt.Errorf("writing block at %d: %w: %w", off, ErrCannotWriteBlockData, err)
	}
	return uint64(len(buf)), nil
}
