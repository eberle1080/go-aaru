package ngcw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

// Junk map tests. Expected bytes are built from the documented layout.

func fillSeed(b byte) Seed {
	w := uint32(b) * 0x01010101

	var s Seed
	for i := range s {
		s[i] = w
	}

	return s
}

func TestJunkMapSerializeDeserializeEmpty(t *testing.T) {
	data, err := JunkMap(nil).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 8 {
		t.Fatalf("len = %d, want 8", len(data))
	}

	// version 1 (u16), count 0 (u32), seed words 17 (u16), little-endian.
	if want := []byte{1, 0, 0, 0, 0, 0, SeedWords, 0}; !bytes.Equal(data, want) {
		t.Fatalf("header = %x, want %x", data, want)
	}

	m := JunkMap{{}}
	if err := m.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}

	if m != nil {
		t.Fatalf("got %v, want nil", m)
	}
}

func TestJunkMapSerializeDeserializeRoundTrip(t *testing.T) {
	in := JunkMap{
		{Offset: 0x1000, Length: 0x800, PartitionIndex: 0xFFFF, Seed: fillSeed(0xAA)},
		{Offset: 0x8000, Length: 0x7C00, PartitionIndex: 0, Seed: fillSeed(0xBB)},
		{Offset: 0x100000, Length: 0x2000, PartitionIndex: 1, Seed: fillSeed(0xCC)},
	}

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 266 { // 8 header + 3*86
		t.Fatalf("len = %d, want 266", len(data))
	}

	var out JunkMap
	if err := out.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

func TestJunkMapDeserializeTruncatedFails(t *testing.T) {
	var m JunkMap

	if err := m.UnmarshalBinary([]byte{0x01, 0x00, 0x01, 0x00}); err == nil {
		t.Fatal("expected error for incomplete header")
	}
}

func TestJunkMapDeserializeErrors(t *testing.T) {
	good, _ := JunkMap{{Offset: 1, Length: 2, Seed: fillSeed(1)}}.MarshalBinary()

	var m JunkMap

	if err := m.UnmarshalBinary(good[:len(good)-1]); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated entry: %v", err)
	}

	bad := bytes.Clone(good)
	bad[0] = 2

	if err := m.UnmarshalBinary(bad); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("bad version: %v", err)
	}

	bad = bytes.Clone(good)
	bad[6] = 16

	if err := m.UnmarshalBinary(bad); !errors.Is(err, ErrUnsupportedSeedSize) {
		t.Errorf("bad seed size: %v", err)
	}

	// Huge declared count must not overflow / allocate.
	huge := []byte{1, 0, 0xFF, 0xFF, 0xFF, 0xFF, 17, 0}
	if err := m.UnmarshalBinary(huge); !errors.Is(err, ErrTruncated) {
		t.Errorf("huge count: %v", err)
	}
}

func TestJunkMapRegenerateMatchesLFG(t *testing.T) {
	expected := lfgBytes(testSeed, 2048)
	m := JunkMap{{Offset: 0, Length: 2048, PartitionIndex: 0xFFFF, Seed: testSeed}}

	out := make([]byte, 2048)
	if err := m.Regenerate(0, out); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(out, expected) {
		t.Fatal("regenerated junk mismatch")
	}
}

func TestJunkMapRegenerateAtOffset(t *testing.T) {
	expected := lfgBytes(testSeed, 4096+2048)[4096:]

	// The seed's position 0 is the start of the 0x8000-aligned block (offset
	// 0), not the entry offset, so regeneration at 4096 advances 4096 bytes.
	m := JunkMap{{Offset: 4096, Length: 2048, PartitionIndex: 0xFFFF, Seed: testSeed}}

	out := make([]byte, 2048)
	if err := m.Regenerate(4096, out); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(out, expected) {
		t.Fatal("regenerated junk mismatch")
	}
}

func TestJunkMapRegenerateNotFoundReturnsError(t *testing.T) {
	m := JunkMap{{Offset: 0x10000, Length: 0x800, PartitionIndex: 0xFFFF}}

	if err := m.Regenerate(0x5000, make([]byte, 2048)); !errors.Is(err, ErrNotJunk) {
		t.Fatalf("err = %v, want ErrNotJunk", err)
	}

	if err := JunkMap(nil).Regenerate(0, make([]byte, 2048)); !errors.Is(err, ErrNotJunk) {
		t.Fatalf("empty map err = %v, want ErrNotJunk", err)
	}
}

// expectedJunkMap encodes m by hand from the documented media tag layout.
func expectedJunkMap(m JunkMap) []byte {
	b := binary.LittleEndian.AppendUint16(nil, JunkMapVersion)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(m)))
	b = binary.LittleEndian.AppendUint16(b, SeedWords)

	for _, e := range m {
		b = binary.LittleEndian.AppendUint64(b, e.Offset)
		b = binary.LittleEndian.AppendUint64(b, e.Length)
		b = binary.LittleEndian.AppendUint16(b, e.PartitionIndex)

		for _, w := range e.Seed {
			b = binary.BigEndian.AppendUint32(b, w)
		}
	}

	return b
}

// Serialization follows the documented layout, and regeneration at any
// offset equals the entry's LFG stream advanced from the start of the entry's
// 0x8000-aligned block.
func TestJunkMapLayoutAndRegenerate(t *testing.T) {
	var reversed [SeedWords]uint32
	for i := range reversed {
		reversed[i] = swappedTestSeed[SeedWords-1-i]
	}

	m := JunkMap{
		{Offset: 0x9000, Length: 0x7000, PartitionIndex: 0xFFFF, Seed: testSeed},
		{Offset: 0x18000, Length: 0x8000, PartitionIndex: 3, Seed: seedFromSwapped(reversed)},
	}

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if want := expectedJunkMap(m); !bytes.Equal(data, want) {
		t.Fatalf("serialized = %x\nwant %x", data, want)
	}

	tests := []struct {
		off   uint64
		entry int
	}{
		{0x9000, 0},
		{0x9800, 0},
		{0xF800, 0},
		{0x18000, 1},
		{0x1F800, 1},
		{0x1C123, 1}, // unaligned
	}

	for _, tt := range tests {
		out := make([]byte, 2048)
		if err := m.Regenerate(tt.off, out); err != nil {
			t.Fatalf("Regenerate(%#x): %v", tt.off, err)
		}

		e := m[tt.entry]
		block := e.Offset &^ (JunkBlockSize - 1)
		want := lfgBytes(e.Seed, int(tt.off-block)+len(out))[tt.off-block:]

		if !bytes.Equal(out, want) {
			t.Errorf("Regenerate(%#x) differs from the entry's LFG stream", tt.off)
		}
	}

	// Entries are independent: regeneration of entry 1 is unaffected by
	// entry 0's seed.
	other := JunkMap{m[0], {Offset: m[1].Offset, Length: m[1].Length, PartitionIndex: 3, Seed: fillSeed(9)}}
	a, b := make([]byte, 2048), make([]byte, 2048)
	_ = m.Regenerate(0x9800, a)
	_ = other.Regenerate(0x9800, b)

	if !bytes.Equal(a, b) {
		t.Error("entry 0 output depends on entry 1")
	}

	_ = other.Regenerate(0x18800, b)
	_ = m.Regenerate(0x18800, a)

	if bytes.Equal(a, b) {
		t.Error("entry 1 output does not depend on its seed")
	}

	if err := m.Regenerate(0x10000, make([]byte, 2048)); !errors.Is(err, ErrNotJunk) {
		t.Errorf("Regenerate(0x10000) = %v, want ErrNotJunk", err)
	}
}

func TestJunkMapFind(t *testing.T) {
	m := JunkMap{
		{Offset: 0x1000, Length: 0x1000},
		{Offset: 0x4000, Length: 0x800},
		{Offset: 0x8000, Length: 0x8000},
	}

	tests := []struct {
		off   uint64
		index int
		ok    bool
	}{
		{0, -1, false},
		{0x1000, 0, true},
		{0x1FFF, 0, true},
		{0x2000, -1, false},
		{0x4000, 1, true},
		{0x47FF, 1, true},
		{0x4800, -1, false},
		{0xFFFF, 2, true},
		{0x10000, -1, false},
	}

	for _, tt := range tests {
		i, ok := m.Find(tt.off)
		if i != tt.index || ok != tt.ok {
			t.Errorf("Find(%#x) = %d,%v want %d,%v", tt.off, i, ok, tt.index, tt.ok)
		}
	}
}

func FuzzJunkMapUnmarshal(f *testing.F) {
	good, _ := JunkMap{{Offset: 0x8000, Length: 0x800, Seed: testSeed}}.MarshalBinary()
	f.Add(good, uint64(0x8000))
	f.Add([]byte{1, 0, 0, 0, 0, 0, 17, 0}, uint64(0))

	f.Fuzz(func(t *testing.T, data []byte, off uint64) {
		var m JunkMap
		if err := m.UnmarshalBinary(data); err != nil {
			return
		}

		again, err := m.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}

		if len(m) > 0 && !bytes.Equal(again, data[:len(again)]) {
			t.Fatal("re-marshal differs from input prefix")
		}

		// Keep the LFG advance bounded.
		if i, ok := m.Find(off); ok && off-(m[i].Offset&^(JunkBlockSize-1)) < 1<<20 {
			_ = m.Regenerate(off, make([]byte, 64))
		}
	})
}
