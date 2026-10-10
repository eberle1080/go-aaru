package aaru

import (
	"bytes"
	"fmt"
	"math"
	"slices"

	"github.com/eberle1080/go-aaru/crc64"
)

// This file implements the dump hardware block: reading, the accessors and
// writeDumpHardwareBlock.
//
// The dump hardware block (DMP*) is a DumpHardwareHeader followed, for every
// entry, by a DumpHardwareEntry, its eight strings (UTF-8, in field order,
// each present only if its length is not zero) and its extents. By the
// convention of Aaru (the last byte of every string is forced to NUL) the
// string lengths include a NUL terminator.
//
// Internally img.dumpHardware holds the entries with the strings exactly as
// stored on disk (terminator included) so that images are rewritten byte for
// byte; DumpHardware decodes them.

// dumpHardwareStrings returns pointers to the eight strings of d, in on-disk
// order.
func dumpHardwareStrings(d *DumpHardware) [8]*string {
	return [8]*string{&d.Manufacturer, &d.Model, &d.Revision, &d.Firmware, &d.Serial, &d.SoftwareName,
		&d.SoftwareVersion, &d.SoftwareOperatingSystem}
}

// dumpHardwareEntryHeader returns the fixed part of a raw entry.
func dumpHardwareEntryHeader(d *DumpHardware) DumpHardwareEntry {
	return DumpHardwareEntry{
		ManufacturerLength:            uint32(len(d.Manufacturer)),
		ModelLength:                   uint32(len(d.Model)),
		RevisionLength:                uint32(len(d.Revision)),
		FirmwareLength:                uint32(len(d.Firmware)),
		SerialLength:                  uint32(len(d.Serial)),
		SoftwareNameLength:            uint32(len(d.SoftwareName)),
		SoftwareVersionLength:         uint32(len(d.SoftwareVersion)),
		SoftwareOperatingSystemLength: uint32(len(d.SoftwareOperatingSystem)),
		Extents:                       uint32(len(d.Extents)),
	}
}

func (e *DumpHardwareEntry) stringLengths() [8]uint32 {
	return [8]uint32{e.ManufacturerLength, e.ModelLength, e.RevisionLength, e.FirmwareLength, e.SerialLength,
		e.SoftwareNameLength, e.SoftwareVersionLength, e.SoftwareOperatingSystemLength}
}

// sortExtents orders extents by their first sector.
func sortExtents(x []DumpExtent) {
	slices.SortStableFunc(x, func(a, b DumpExtent) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		}
		return 0
	})
}

// parseDumpHardwareEntries decodes n raw entries from payload. With
// forceNUL the last byte of every string is replaced by NUL, as
// SetDumpHardwareRaw does. It returns the entries and the number of bytes
// used.
func parseDumpHardwareEntries(payload []byte, n int, forceNUL bool) ([]DumpHardware, int, error) {
	out := make([]DumpHardware, n)
	pos := 0
	for i := range out {
		if len(payload)-pos < dumpHardwareEntrySize {
			return nil, 0, fmt.Errorf("dump hardware entry %d truncated: %w", i, ErrIncorrectDataSize)
		}
		eh, _ := unmarshal[DumpHardwareEntry](payload[pos:])
		pos += dumpHardwareEntrySize
		d := &out[i]
		for k, p := range dumpHardwareStrings(d) {
			l := eh.stringLengths()[k]
			if l == 0 {
				continue
			}
			if uint64(l) > uint64(len(payload)-pos) {
				return nil, 0, fmt.Errorf("dump hardware entry %d string %d truncated: %w", i, k, ErrIncorrectDataSize)
			}
			s := bytes.Clone(payload[pos : pos+int(l)])
			if forceNUL {
				s[len(s)-1] = 0
			}
			*p = string(s)
			pos += int(l)
		}
		if eh.Extents > 0 {
			nb := uint64(eh.Extents) * dumpExtentSize
			if nb > uint64(len(payload)-pos) {
				return nil, 0, fmt.Errorf("dump hardware entry %d extents truncated: %w", i, ErrIncorrectDataSize)
			}
			d.Extents, _ = unmarshalSlice[DumpExtent](payload[pos:], int(eh.Extents))
			pos += int(nb)
			sortExtents(d.Extents)
		}
	}
	return out, pos, nil
}

// resetDumpHardware forgets any dump hardware information.
func (img *Image) resetDumpHardware() {
	img.dumpHardware = nil
	img.dumpHWHeader = DumpHardwareHeader{}
}

// processDumpHardwareBlock loads the dump hardware block (DMP*) described by
// e. Any inconsistency (bad identifier, CRC, or
// entries not fitting in the payload) discards the whole block.
func (img *Image) processDumpHardwareBlock(e IndexEntry) error {
	var hb [dumpHardwareHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		img.resetDumpHardware()
		return fmt.Errorf("reading dump hardware header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[DumpHardwareHeader](hb[:])
	if h.Identifier != BlockTypeDumpHardwareBlock || (h.Entries > 0 && h.Length == 0) {
		img.resetDumpHardware()
		return fmt.Errorf("invalid dump hardware block at %d: %w", e.Offset, ErrCannotReadBlock)
	}
	var payload []byte
	if h.Length > 0 {
		p, err := img.readAt(e.Offset+dumpHardwareHeaderSize, uint64(h.Length))
		if err != nil {
			img.resetDumpHardware()
			return fmt.Errorf("reading dump hardware block at %d: %w: %w", e.Offset, ErrCannotReadBlock, err)
		}
		if !img.crcMatches(crc64.Checksum(p), h.CRC64) {
			img.resetDumpHardware()
			return fmt.Errorf("dump hardware block at %d: %w", e.Offset, ErrInvalidBlockCRC)
		}
		payload = p
	}
	if h.Entries == 0 {
		img.resetDumpHardware()
		img.dumpHWHeader = h
		return nil
	}
	entries, _, err := parseDumpHardwareEntries(payload, int(h.Entries), false)
	if err != nil {
		img.resetDumpHardware()
		return fmt.Errorf("dump hardware block at %d: %w", e.Offset, err)
	}
	img.dumpHardware = entries
	img.dumpHWHeader = h
	return nil
}

// hasDumpHardware reports whether dump hardware information is present.
func (img *Image) hasDumpHardware() bool {
	return len(img.dumpHardware) > 0 && img.dumpHWHeader.Entries > 0 &&
		img.dumpHWHeader.Identifier == BlockTypeDumpHardwareBlock
}

// serializeDumpHardware builds the raw dump hardware block (header and
// payload). The payload is header.Length bytes long
// (zero padded) and its CRC64 is recomputed.
func (img *Image) serializeDumpHardware() ([]byte, error) {
	h := &img.dumpHWHeader
	buf := make([]byte, dumpHardwareHeaderSize+uint64(h.Length))
	pos := uint64(dumpHardwareHeaderSize)
	for i := range img.dumpHardware {
		d := &img.dumpHardware[i]
		eh := dumpHardwareEntryHeader(d)
		size := uint64(dumpHardwareEntrySize) + uint64(len(d.Extents))*dumpExtentSize
		for _, l := range eh.stringLengths() {
			size += uint64(l)
		}
		if pos+size > uint64(len(buf)) {
			return nil, fmt.Errorf("dump hardware entries exceed the block length %d: %w", h.Length,
				ErrBufferTooSmall)
		}
		b := appendMarshal(buf[:pos], &eh)
		for _, s := range dumpHardwareStrings(d) {
			b = append(b, *s...)
		}
		if len(d.Extents) > 0 {
			b = appendMarshal(b, d.Extents)
		}
		pos = uint64(len(b))
	}
	h.CRC64 = crc64.Checksum(buf[dumpHardwareHeaderSize:])
	copy(buf, marshal(h))
	return buf, nil
}

// DumpHardwareRaw returns the dump hardware block in its on-disk
// serialized form (DumpHardwareHeader, then every DumpHardwareEntry followed
// by its strings and extents). It returns
// ErrCannotReadBlock if the image has no dump hardware information.
func (img *Image) DumpHardwareRaw() ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if !img.hasDumpHardware() {
		return nil, fmt.Errorf("no dump hardware information: %w", ErrCannotReadBlock)
	}
	return img.serializeDumpHardware()
}

// SetDumpHardwareRaw replaces the dump hardware information with a block in
// its on-disk serialized form. The block must be
// consistent: correct identifier (else ErrCannotReadBlock), total length
// and entries (else ErrIncorrectDataSize) and payload CRC64 (else
// ErrInvalidBlockCRC). The last byte of every non-empty
// string is replaced by NUL (strings are expected to be NUL terminated) and
// the extents are sorted by start sector. A block with no entries clears the
// information.
func (img *Image) SetDumpHardwareRaw(data []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if len(data) < dumpHardwareHeaderSize {
		return fmt.Errorf("dump hardware block of %d bytes: %w", len(data), ErrIncorrectDataSize)
	}
	h, _ := unmarshal[DumpHardwareHeader](data)
	if h.Identifier != BlockTypeDumpHardwareBlock {
		return fmt.Errorf("dump hardware block identifier %#x: %w", uint32(h.Identifier), ErrCannotReadBlock)
	}
	if uint64(len(data)) != dumpHardwareHeaderSize+uint64(h.Length) {
		return fmt.Errorf("dump hardware block of %d bytes, header says %d: %w", len(data),
			dumpHardwareHeaderSize+uint64(h.Length), ErrIncorrectDataSize)
	}
	payload := data[dumpHardwareHeaderSize:]
	if crc64.Checksum(payload) != h.CRC64 {
		return fmt.Errorf("dump hardware block: %w", ErrInvalidBlockCRC)
	}
	entries, used, err := parseDumpHardwareEntries(payload, int(h.Entries), true)
	if err != nil {
		return err
	}
	if used != len(payload) {
		return fmt.Errorf("dump hardware block has %d trailing bytes: %w", len(payload)-used, ErrIncorrectDataSize)
	}
	if h.Entries == 0 {
		entries = nil
	}
	img.dumpHardware = entries
	img.dumpHWHeader = h
	img.dirty.dumpHWBlock = true
	return nil
}

// DumpHardware returns the devices used to dump the media, decoded from
// the dump hardware block. Strings end at their first NUL
// byte. It returns ErrCannotReadBlock if the image has none.
func (img *Image) DumpHardware() ([]DumpHardware, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if !img.hasDumpHardware() {
		return nil, fmt.Errorf("no dump hardware information: %w", ErrCannotReadBlock)
	}
	out := make([]DumpHardware, len(img.dumpHardware))
	for i := range img.dumpHardware {
		d := img.dumpHardware[i]
		for _, s := range dumpHardwareStrings(&d) {
			if j := bytes.IndexByte([]byte(*s), 0); j >= 0 {
				*s = (*s)[:j]
			}
		}
		d.Extents = slices.Clone(d.Extents)
		out[i] = d
	}
	return out, nil
}

// EncodeDumpHardware serializes dump hardware entries in the on-disk
// format (the format of DumpHardwareRaw): every non-empty string is stored
// as UTF-8 followed by a NUL terminator, and extents are sorted by start
// sector.
func EncodeDumpHardware(hw []DumpHardware) ([]byte, error) {
	if len(hw) > math.MaxUint16 {
		return nil, fmt.Errorf("%d dump hardware entries: %w", len(hw), ErrIncorrectDataSize)
	}
	payload := make([]byte, 0, 256*len(hw))
	for i := range hw {
		d := hw[i]
		for _, s := range dumpHardwareStrings(&d) {
			if *s != "" {
				*s += "\x00"
			}
		}
		if uint64(len(d.Extents)) > math.MaxUint32 {
			return nil, fmt.Errorf("%d extents: %w", len(d.Extents), ErrIncorrectDataSize)
		}
		d.Extents = slices.Clone(d.Extents)
		sortExtents(d.Extents)
		eh := dumpHardwareEntryHeader(&d)
		payload = appendMarshal(payload, &eh)
		for _, s := range dumpHardwareStrings(&d) {
			payload = append(payload, *s...)
		}
		if len(d.Extents) > 0 {
			payload = appendMarshal(payload, d.Extents)
		}
	}
	if uint64(len(payload)) > math.MaxUint32 {
		return nil, fmt.Errorf("dump hardware payload of %d bytes: %w", len(payload), ErrIncorrectDataSize)
	}
	h := DumpHardwareHeader{Identifier: BlockTypeDumpHardwareBlock, Entries: uint16(len(hw)),
		Length: uint32(len(payload)), CRC64: crc64.Checksum(payload)}
	out := appendMarshal(make([]byte, 0, dumpHardwareHeaderSize+len(payload)), &h)
	return append(out, payload...), nil
}

// SetDumpHardware replaces the dump hardware information (SetDumpHardwareRaw
// with a block built by EncodeDumpHardware). An empty slice removes it.
func (img *Image) SetDumpHardware(hw []DumpHardware) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	raw, err := EncodeDumpHardware(hw)
	if err != nil {
		return err
	}
	return img.SetDumpHardwareRaw(raw)
}

// writeDumpHardwareBlock writes the dump hardware block.
// When there is none, the index entry of the block of a resumed image is
// removed.
func (img *Image) writeDumpHardwareBlock() error {
	if !img.hasDumpHardware() {
		img.removeStaleIndexEntries(BlockTypeDumpHardwareBlock)
		return nil
	}
	buf, err := img.serializeDumpHardware()
	if err != nil {
		return err
	}
	off := img.closeAppendOffset()
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing dump hardware block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeDumpHardwareBlock, 0, off)
	return nil
}
