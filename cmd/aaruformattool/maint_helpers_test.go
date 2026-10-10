package main

// Shared helpers for the maintenance verb tests (upgrade-ddt-to-alpha21,
// repair-cd-arena, inject-media-tag).
//
// The verb output is compared with the expected outputs in
// testdata/expected/maint (see checkExpected).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/compression/zstd"
	"github.com/eberle1080/go-aaru/crc64"
)

func maintCopy(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func maintReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func maintWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// maintDump prints what go-aaru reads from an image: the verification result,
// the geometry, every sector (user data and long, including negative and
// overflow sectors) and every media tag.
func maintDump(path string) string {
	img, err := aaru.Open(path)
	if err != nil {
		return "open failed\n"
	}
	defer img.Close()

	var b strings.Builder
	line := func(kind string, addr uint64, data []byte, st aaru.SectorStatus, err error) {
		if rc := aaru.ErrorCode(err); rc != 0 {
			fmt.Fprintf(&b, "%s %d rc=%d\n", kind, addr, rc)
			return
		}
		fmt.Fprintf(&b, "%s %d status=%d len=%d crc=%016x\n", kind, addr, st, len(data), crc64.Checksum(data))
	}

	fmt.Fprintf(&b, "verify rc=%d\n", aaru.ErrorCode(img.Verify()))
	info := img.Info()
	neg, ovf := uint64(img.NegativeSectors()), uint64(img.OverflowSectors())
	fmt.Fprintf(&b, "sectors=%d size=%d media=%d negative=%d overflow=%d\n", info.Sectors, info.SectorSize,
		uint32(info.MediaType), neg, ovf)
	for a := uint64(1); a <= neg; a++ {
		data, st, err := img.ReadSector(a, true)
		line("N", a, data, st, err)
		data, st, err = img.ReadSectorLong(a, true)
		line("NL", a, data, st, err)
	}
	for a := uint64(0); a < info.Sectors+ovf; a++ {
		data, st, err := img.ReadSector(a, false)
		line("S", a, data, st, err)
		data, st, err = img.ReadSectorLong(a, false)
		line("L", a, data, st, err)
	}
	for t := aaru.MediaTagType(0); t <= aaru.MaxMediaTag; t++ {
		data, err := img.ReadMediaTag(t)
		if errors.Is(err, aaru.ErrMediaTagNotPresent) {
			continue
		}
		line("T", uint64(t), data, 0, err)
	}
	return b.String()
}

// maintFirstDiff describes the first line where a and b differ.
func maintFirstDiff(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return fmt.Sprintf("line %d: %q != %q", i+1, x, y)
		}
	}
	return "equal"
}

// maintCheckReads verifies that path reads exactly as described by want.
func maintCheckReads(t *testing.T, path, want string) {
	t.Helper()
	if got := maintDump(path); got != want {
		t.Errorf("go-aaru reads %s differently: %s", filepath.Base(path), maintFirstDiff(got, want))
	}
}

// maintExitStatus is the process exit status of a verb's return value.
func maintExitStatus(rc int) int { return rc & 0xFF }

// normalize replaces the given paths (pairs of path, placeholder).
func maintNormalize(s string, paths ...string) string {
	return strings.NewReplacer(paths...).Replace(s)
}

// --- raw image access ---

// maintRaw gives access to the on-disk structures of an image in memory.
type maintRaw struct {
	data   []byte
	header aaru.Header
	index  []aaru.IndexEntry
}

func maintParseRaw(t *testing.T, data []byte) *maintRaw {
	t.Helper()
	r := &maintRaw{data: data}
	if _, err := binary.Decode(data, binary.LittleEndian, &r.header); err != nil {
		t.Fatal(err)
	}
	var ih aaru.IndexHeader3
	if _, err := binary.Decode(data[r.header.IndexOffset:], binary.LittleEndian, &ih); err != nil {
		t.Fatal(err)
	}
	if ih.Identifier != aaru.BlockTypeIndexBlock3 || ih.Previous != 0 {
		t.Fatalf("unexpected index header %+v", ih)
	}
	r.index = make([]aaru.IndexEntry, ih.Entries)
	if _, err := binary.Decode(data[r.header.IndexOffset+maintIndexHeader3Size:], binary.LittleEndian,
		r.index); err != nil {
		t.Fatal(err)
	}
	return r
}

// find returns the offset of the index entry for (bt, dt), or 0.
func (r *maintRaw) find(bt aaru.BlockType, dt aaru.DataType) uint64 {
	for _, e := range r.index {
		if e.BlockType == bt && e.DataType == dt {
			return e.Offset
		}
	}
	return 0
}

func (r *maintRaw) put(off uint64, v any) {
	buf, err := binary.Append(nil, binary.LittleEndian, v)
	if err != nil {
		panic(err)
	}
	copy(r.data[off:], buf)
}

// appendAligned appends block at the aligned end of the image and returns
// its offset.
func (r *maintRaw) appendAligned(block []byte) uint64 {
	off := maintAlignUp(uint64(len(r.data)), r.header.BlockAlignmentShift)
	r.data = append(r.data, make([]byte, off-uint64(len(r.data)))...)
	r.data = append(r.data, block...)
	return off
}

// writeIndex appends the index and points the header at it.
func (r *maintRaw) writeIndex() {
	entries, _ := binary.Append(nil, binary.LittleEndian, r.index)
	ih, _ := binary.Append(nil, binary.LittleEndian, aaru.IndexHeader3{Identifier: aaru.BlockTypeIndexBlock3,
		Entries: uint64(len(r.index)), CRC64: crc64.Checksum(entries)})
	r.header.IndexOffset = r.appendAligned(append(ih, entries...))
	r.put(0, r.header)
}

// ddt2 returns the DDT2 header at off and its decoded entries.
func (r *maintRaw) ddt2(t *testing.T, off uint64) (aaru.DDTHeader2, []uint64) {
	t.Helper()
	var h aaru.DDTHeader2
	if _, err := binary.Decode(r.data[off:], binary.LittleEndian, &h); err != nil {
		t.Fatal(err)
	}
	payload := r.data[off+maintDDTHeader2Size : off+maintDDTHeader2Size+h.CmpLength]
	var raw []byte
	var err error
	switch h.Compression {
	case aaru.CompressionNone:
		raw = payload
	case aaru.CompressionLzma:
		raw, err = lzma.DecodePayload(payload, int(h.Length))
	case aaru.CompressionZstd:
		raw, err = zstd.Decode(payload, int(h.Length))
	default:
		t.Fatalf("DDT2 compression %v", h.Compression)
	}
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]uint64, len(raw)/8)
	_, _ = binary.Decode(raw, binary.LittleEndian, entries)
	return h, entries
}

// maintEncodePayload compresses data as AaruFormat stores it, returning the
// on-disk payload and its CRC64 (for LZMA without the properties).
func maintEncodePayload(t *testing.T, comp aaru.CompressionType, data []byte) ([]byte, uint64) {
	t.Helper()
	switch comp {
	case aaru.CompressionNone:
		return data, crc64.Checksum(data)
	case aaru.CompressionLzma:
		p, err := lzma.EncodePayload(data, lzma.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		return p, crc64.Checksum(p[lzma.PropsLen:])
	case aaru.CompressionZstd:
		p, err := zstd.Encode(data, 19)
		if err != nil {
			t.Fatal(err)
		}
		return p, crc64.Checksum(p)
	}
	t.Fatalf("compression %v", comp)
	return nil, 0
}

// appendDataBlock appends a data block holding data and returns its offset.
func (r *maintRaw) appendDataBlock(t *testing.T, dt aaru.DataType, sectorSize uint32,
	comp aaru.CompressionType, data []byte) uint64 {
	payload, cmpCRC := maintEncodePayload(t, comp, data)
	bh, _ := binary.Append(nil, binary.LittleEndian, aaru.BlockHeader{
		Identifier: aaru.BlockTypeDataBlock, Type: dt, Compression: comp, SectorSize: sectorSize,
		CmpLength: uint32(len(payload)), Length: uint32(len(data)), CmpCRC64: cmpCRC, CRC64: crc64.Checksum(data),
	})
	return r.appendAligned(append(bh, payload...))
}

// appendDDT2 appends a DDT2 with the geometry of templ holding entries and
// returns its offset.
func (r *maintRaw) appendDDT2(t *testing.T, templ aaru.DDTHeader2, comp aaru.CompressionType,
	entries []uint64) uint64 {
	raw, _ := binary.Append(nil, binary.LittleEndian, entries)
	payload, cmpCRC := maintEncodePayload(t, comp, raw)
	h := templ
	h.Compression = comp
	h.Entries = uint64(len(entries))
	h.CmpLength = uint64(len(payload))
	h.Length = uint64(len(raw))
	h.CmpCRC64 = cmpCRC
	h.CRC64 = crc64.Checksum(raw)
	hb, _ := binary.Append(nil, binary.LittleEndian, h)
	return r.appendAligned(append(hb, payload...))
}

// setIndexOffset points the index entry for (bt, dt) at off.
func (r *maintRaw) setIndexOffset(bt aaru.BlockType, dt aaru.DataType, off uint64) {
	for i := range r.index {
		if r.index[i].BlockType == bt && r.index[i].DataType == dt {
			r.index[i].Offset = off
		}
	}
}
