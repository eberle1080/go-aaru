package aaru

import (
	"bytes"
	"fmt"
	"math"

	"github.com/eberle1080/go-aaru/crc64"
)

// This file implements flux captures: lazy loading of the flux data block,
// the capture lookup map, FluxCaptures, ReadFluxCapture, WriteFluxCapture,
// ClearFluxCaptures, and the close-time writeFluxCapturePayload and
// writeFluxBlocks.
//
// A flux capture is stored as a data stream payload block (DSPL, data type
// FluxData) holding the data stream followed by the index stream, possibly
// compressed. The flux data block (FLUX) lists the captures (FluxEntry): the
// key (head, track, subtrack, capture index), the resolutions, where the
// index stream starts inside the payload and where the payload block is
// (offset >> FluxHeader.BlockAlignmentShift). The FLUX block is loaded
// lazily, on the first access to the captures.
//
// Captures can be added to a resumed image:
//   - WriteFluxCapture on a resumed image keeps the stored captures; on Close
//     a single FLUX block listing the stored and the new captures replaces
//     the previous one.
//   - ReadFluxCapture of a capture written in the current session (not yet
//     stored) returns its data from memory.

// FluxCaptureInfo describes a flux capture.
type FluxCaptureInfo struct {
	FluxCaptureKey
	// IndexResolution and DataResolution are the sampling resolutions of the
	// index and data streams, in picoseconds.
	IndexResolution uint64
	DataResolution  uint64
}

func fluxKey(e *FluxEntry) FluxCaptureKey {
	return FluxCaptureKey{Head: e.Head, Track: e.Track, Subtrack: e.Subtrack, CaptureIndex: e.CaptureIndex}
}

// rebuildFluxMap indexes the loaded entries by key.
// Later duplicates win.
func (img *Image) rebuildFluxMap() {
	img.fluxMap = make(map[FluxCaptureKey]int, len(img.fluxEntries))
	for i := range img.fluxEntries {
		img.fluxMap[fluxKey(&img.fluxEntries[i])] = i
	}
}

// processFluxDataBlock loads the flux data block (FLUX) described by e,
// discarding any capture pending to be written.
// Entries whose CRC64 does not match are kept (but not
// indexed by key).
func (img *Image) processFluxDataBlock(e IndexEntry) error {
	img.fluxCaptures = nil
	img.fluxEntries = nil
	img.fluxLoaded = false
	img.fluxMap = nil
	img.fluxHeader = FluxHeader{}

	var hb [fluxHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading flux block header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[FluxHeader](hb[:])
	if h.Identifier != BlockTypeFluxDataBlock {
		return fmt.Errorf("flux block at %d has identifier %#x: %w", e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	img.fluxHeader = h
	img.info.ImageSize += fluxEntrySize * uint64(h.Entries)

	raw, err := img.readAt(e.Offset+fluxHeaderSize, fluxEntrySize*uint64(h.Entries))
	if err != nil {
		img.fluxHeader = FluxHeader{}
		return fmt.Errorf("reading flux entries at %d: %w: %w", e.Offset, ErrCannotReadBlock, err)
	}
	entries, err := unmarshalSlice[FluxEntry](raw, int(h.Entries))
	if err != nil {
		img.fluxHeader = FluxHeader{}
		return err
	}
	img.fluxEntries = entries
	img.fluxLoaded = true
	if crc64.Checksum(raw) != h.CRC64 {
		return fmt.Errorf("flux entries at %d: %w", e.Offset, ErrInvalidBlockCRC)
	}
	img.rebuildFluxMap()
	return nil
}

// ensureFluxLoaded loads the first FLUX block of the index unless the
// captures are already known.
func (img *Image) ensureFluxLoaded() error {
	if img.fluxLoaded || img.fluxHeader.Entries > 0 {
		return nil
	}
	for _, e := range img.indexEntries {
		if e.BlockType != BlockTypeFluxDataBlock {
			continue
		}
		_ = img.processFluxDataBlock(e)
		if !img.fluxLoaded || img.fluxHeader.Entries == 0 {
			return fmt.Errorf("no flux captures: %w", ErrFluxDataNotFound)
		}
		return nil
	}
	return fmt.Errorf("image has no flux data: %w", ErrFluxDataNotFound)
}

// FluxCaptures lists the flux captures of the image,
// including those written since the image was
// created or resumed. It returns ErrFluxDataNotFound if there are none.
func (img *Image) FluxCaptures() ([]FluxCaptureInfo, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if err := img.ensureFluxLoaded(); err != nil {
		return nil, err
	}
	if img.fluxHeader.Entries == 0 || len(img.fluxEntries) == 0 {
		return nil, fmt.Errorf("no flux captures: %w", ErrFluxDataNotFound)
	}
	out := make([]FluxCaptureInfo, len(img.fluxEntries))
	for i := range img.fluxEntries {
		e := &img.fluxEntries[i]
		out[i] = FluxCaptureInfo{FluxCaptureKey: fluxKey(e), IndexResolution: e.IndexResolution,
			DataResolution: e.DataResolution}
	}
	return out, nil
}

// findFluxEntry returns the index of the capture with the given key, or -1
// (map lookup, then linear search).
func (img *Image) findFluxEntry(key FluxCaptureKey) int {
	if i, ok := img.fluxMap[key]; ok && i < len(img.fluxEntries) {
		return i
	}
	for i := range img.fluxEntries {
		if fluxKey(&img.fluxEntries[i]) == key {
			return i
		}
	}
	return -1
}

// ReadFluxCapture returns the index and data streams of a flux capture.
// It returns ErrFluxDataNotFound if the image has
// no such capture, ErrCannotReadBlock, ErrUnsupportedCompression,
// ErrCannotDecompressBlock or ErrInvalidBlockCRC if its payload is damaged.
func (img *Image) ReadFluxCapture(key FluxCaptureKey) (index, data []byte, err error) {
	if img.closed {
		return nil, nil, ErrClosed
	}
	if err := img.ensureFluxLoaded(); err != nil {
		return nil, nil, err
	}
	if img.fluxHeader.Entries == 0 || len(img.fluxEntries) == 0 {
		return nil, nil, fmt.Errorf("no flux captures: %w", ErrFluxDataNotFound)
	}
	i := img.findFluxEntry(key)
	if i < 0 {
		return nil, nil, fmt.Errorf("flux capture %+v: %w", key, ErrFluxDataNotFound)
	}
	if stored := len(img.fluxEntries) - len(img.fluxCaptures); i >= stored {
		rec := &img.fluxCaptures[i-stored]
		return bytes.Clone(rec.index), bytes.Clone(rec.data), nil
	}
	return img.readFluxPayload(&img.fluxEntries[i])
}

// readFluxPayload reads, decompresses and checks the payload block of a
// stored capture and splits it into the index and data streams.
func (img *Image) readFluxPayload(e *FluxEntry) (index, data []byte, err error) {
	off := e.PayloadOffset << img.fluxHeader.BlockAlignmentShift
	var hb [dataStreamPayloadHdrSize]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return nil, nil, fmt.Errorf("reading flux payload header at %d: %w", off, ErrCannotReadBlock)
	}
	h, _ := unmarshal[DataStreamPayloadHeader](hb[:])
	if h.Identifier != BlockTypeDataStreamPayloadBlock || h.DataType != DataTypeFluxData {
		return nil, nil, fmt.Errorf("flux payload at %d has identifier %#x, data type %v: %w", off,
			uint32(h.Identifier), h.DataType, ErrCannotReadBlock)
	}

	cmpLen, rawLen := uint64(h.CmpLength), uint64(h.Length)
	switch h.Compression {
	case CompressionNone:
		if cmpLen != rawLen {
			return nil, nil, fmt.Errorf("uncompressed flux payload lengths %d/%d: %w", cmpLen, rawLen,
				ErrCannotDecompressBlock)
		}
	case CompressionLzma:
		if cmpLen <= LZMAPropertiesLength {
			return nil, nil, fmt.Errorf("LZMA flux payload of %d bytes: %w", cmpLen, ErrCannotDecompressBlock)
		}
	case CompressionZstd:
		if cmpLen == 0 {
			return nil, nil, fmt.Errorf("empty Zstandard flux payload: %w", ErrCannotDecompressBlock)
		}
	default:
		return nil, nil, fmt.Errorf("flux payload compression %v: %w", h.Compression, ErrUnsupportedCompression)
	}

	cmp, err := img.readAt(off+dataStreamPayloadHdrSize, cmpLen)
	if err != nil {
		return nil, nil, fmt.Errorf("reading flux payload at %d: %w: %w", off, ErrCannotReadBlock, err)
	}
	payload := cmp
	if h.Compression != CompressionNone && rawLen > 0 {
		if payload, err = decodePayload(h.Compression, DataTypeFluxData, cmp, rawLen); err != nil {
			return nil, nil, fmt.Errorf("flux payload at %d: %w", off, err)
		}
	} else if h.Compression != CompressionNone {
		payload = nil
	}

	var cmpCRC, rawCRC uint64
	if len(cmp) > 0 {
		cmpCRC = crc64.Checksum(cmp)
	}
	if rawLen > 0 && payload != nil {
		rawCRC = crc64.Checksum(payload)
	}
	if cmpCRC != h.CmpCRC64 || rawCRC != h.CRC64 {
		return nil, nil, fmt.Errorf("flux payload at %d: %w", off, ErrInvalidBlockCRC)
	}

	if e.IndexOffset > rawLen {
		return nil, nil, fmt.Errorf("flux index offset %d beyond payload length %d: %w", e.IndexOffset, rawLen,
			ErrInvalidBlockCRC)
	}
	if payload == nil {
		return []byte{}, []byte{}, nil
	}
	return payload[e.IndexOffset:], payload[:e.IndexOffset:e.IndexOffset], nil
}

// WriteFluxCapture adds a flux capture: the data
// stream and the index stream sampled at the given resolutions (in
// picoseconds). The buffers are copied; the capture is stored on Close.
// Captures are identified by key; adding a key twice stores both captures
// but lookups return the last one. At most 65535 captures can be stored, and
// data and index together must not exceed 4 GiB.
func (img *Image) WriteFluxCapture(key FluxCaptureKey, dataResolution, indexResolution uint64, data,
	index []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if uint64(len(data))+uint64(len(index)) > math.MaxUint32 {
		return fmt.Errorf("flux capture of %d bytes: %w", len(data)+len(index), ErrIncorrectDataSize)
	}
	// Keep the captures already stored in a resumed image.
	if !img.fluxLoaded && img.fluxHeader.Entries == 0 {
		_ = img.ensureFluxLoaded()
	}
	if len(img.fluxEntries) >= math.MaxUint16 {
		return fmt.Errorf("flux capture limit (%d) exceeded: %w", math.MaxUint16, ErrIncorrectDataSize)
	}

	entry := FluxEntry{Head: key.Head, Track: key.Track, Subtrack: key.Subtrack, CaptureIndex: key.CaptureIndex,
		IndexResolution: indexResolution, DataResolution: dataResolution, IndexOffset: uint64(len(data))}
	img.fluxCaptures = append(img.fluxCaptures, fluxCaptureRecord{entry: entry, data: bytes.Clone(data),
		index: bytes.Clone(index)})
	img.fluxEntries = append(img.fluxEntries, entry)
	img.fluxLoaded = true
	img.fluxHeader.Identifier = BlockTypeFluxDataBlock
	img.fluxHeader.Entries = uint16(len(img.fluxEntries))
	if img.fluxMap == nil {
		img.fluxMap = make(map[FluxCaptureKey]int)
	}
	img.fluxMap[key] = len(img.fluxEntries) - 1
	img.dirty.fluxBlock = true
	return nil
}

// ClearFluxCaptures discards the flux captures written since the image was
// created or resumed, and forgets the loaded ones.
// It does not remove captures already stored in the file
// (they are reloaded on the next access) and works on read-only images too.
func (img *Image) ClearFluxCaptures() error {
	if img.closed {
		return ErrClosed
	}
	img.fluxCaptures = nil
	img.fluxMap = nil
	img.fluxEntries = nil
	img.fluxLoaded = false
	img.fluxHeader = FluxHeader{}
	return nil
}

// writeFluxCapturePayload writes the payload block of a pending capture at
// the aligned end of the file, compressed per the image options, and
// returns its entry.
func (img *Image) writeFluxCapturePayload(rec *fluxCaptureRecord) (FluxEntry, error) {
	raw := make([]byte, 0, len(rec.data)+len(rec.index))
	raw = append(append(raw, rec.data...), rec.index...)
	if uint64(len(raw)) > math.MaxUint32 {
		return FluxEntry{}, fmt.Errorf("flux capture of %d bytes: %w", len(raw), ErrIncorrectDataSize)
	}
	var rawCRC uint64
	if len(raw) > 0 {
		rawCRC = crc64.Checksum(raw)
	}
	p := img.compressPayload(raw, false)
	cmpCRC := rawCRC
	if p.Compression != CompressionNone {
		h := crc64.New()
		h.Write(p.Props)
		h.Write(p.Stream)
		cmpCRC = h.Sum64()
	}

	off := img.closeAppendOffset()
	hdr := DataStreamPayloadHeader{
		Identifier:  BlockTypeDataStreamPayloadBlock,
		DataType:    DataTypeFluxData,
		Compression: p.Compression,
		CmpLength:   uint32(p.cmpLength()),
		Length:      uint32(len(raw)),
		CmpCRC64:    cmpCRC,
		CRC64:       rawCRC,
	}
	buf := appendMarshal(make([]byte, 0, dataStreamPayloadHdrSize+p.cmpLength()), &hdr)
	buf = append(append(buf, p.Props...), p.Stream...)
	if err := img.writeAt(buf, off); err != nil {
		return FluxEntry{}, fmt.Errorf("writing flux payload: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDataStreamPayloadBlock,
		DataType: DataTypeFluxData, Offset: off})

	e := rec.entry
	e.IndexOffset = uint64(len(rec.data))
	e.PayloadOffset = off >> img.userDataDDTHeader.BlockAlignmentShift
	return e, nil
}

// writeFluxBlocks writes the payloads of the pending captures and the flux
// data block listing all captures.
func (img *Image) writeFluxBlocks() error {
	if len(img.fluxCaptures) == 0 {
		return nil
	}
	if len(img.fluxEntries) > math.MaxUint16 {
		return fmt.Errorf("%d flux captures: %w", len(img.fluxEntries), ErrIncorrectDataSize)
	}
	shift := img.userDataDDTHeader.BlockAlignmentShift
	stored := len(img.fluxEntries) - len(img.fluxCaptures)
	entries := make([]FluxEntry, 0, len(img.fluxEntries))

	// Captures already stored in a resumed image keep their payload blocks;
	// their offsets are re-expressed in the alignment of the new FLUX block.
	for _, e := range img.fluxEntries[:stored] {
		abs := e.PayloadOffset << img.fluxHeader.BlockAlignmentShift
		if abs&(1<<shift-1) != 0 {
			return fmt.Errorf("flux payload at %d not aligned to %d bytes: %w", abs, 1<<shift, ErrIncorrectDataSize)
		}
		e.PayloadOffset = abs >> shift
		entries = append(entries, e)
	}
	for i := range img.fluxCaptures {
		e, err := img.writeFluxCapturePayload(&img.fluxCaptures[i])
		if err != nil {
			return err
		}
		entries = append(entries, e)
	}

	raw := marshal(entries)
	h := FluxHeader{Identifier: BlockTypeFluxDataBlock, Entries: uint16(len(entries)), BlockAlignmentShift: shift,
		CRC64: crc64.Checksum(raw)}
	off := img.closeAppendOffset()
	buf := appendMarshal(make([]byte, 0, fluxHeaderSize+len(raw)), &h)
	if err := img.writeAt(append(buf, raw...), off); err != nil {
		return fmt.Errorf("writing flux block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeFluxDataBlock, 0, off)
	img.fluxOffset = off

	img.fluxEntries = entries
	img.fluxHeader = h
	img.fluxLoaded = true
	img.fluxCaptures = nil
	img.rebuildFluxMap()
	return nil
}
