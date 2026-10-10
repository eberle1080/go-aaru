package ps3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

// Encryption map tests.

// buildSyntheticSector0 builds a big-endian sector 0 with two regions:
// [0,31] and [256,511].
func buildSyntheticSector0() []byte {
	buf := make([]byte, 2048)
	buf[3] = 2 // region_count = 2
	// region[0]: start=0, end=31
	buf[15] = 31
	// region[1]: start=256, end=511
	buf[18] = 1
	buf[22] = 1
	buf[23] = 0xFF

	return buf
}

func TestParseValid(t *testing.T) {
	m, err := ParseEncryptionMap(buildSyntheticSector0())
	if err != nil {
		t.Fatal(err)
	}

	want := EncryptionMap{{0, 31}, {256, 511}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %v, want %v", m, want)
	}
}

func TestParseZeroRegions(t *testing.T) {
	m, err := ParseEncryptionMap(make([]byte, 2048))
	if err != nil {
		t.Fatal(err)
	}

	if m != nil {
		t.Fatalf("got %v, want nil", m)
	}
}

func TestParseTooManyRegions(t *testing.T) {
	s := make([]byte, 2048)
	s[3] = 65

	if _, err := ParseEncryptionMap(s); !errors.Is(err, ErrTooManyRegions) {
		t.Fatalf("err = %v, want ErrTooManyRegions", err)
	}
}

func TestParseInvalidRegion(t *testing.T) {
	s := make([]byte, 2048)
	s[3] = 1
	s[11] = 100 // start = 100
	s[15] = 50  // end = 50

	if _, err := ParseEncryptionMap(s); !errors.Is(err, ErrInvalidRegion) {
		t.Fatalf("err = %v, want ErrInvalidRegion", err)
	}
}

func TestParseBufferTooShort(t *testing.T) {
	s := make([]byte, 8)
	s[3] = 1

	if _, err := ParseEncryptionMap(s); !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}

	if _, err := ParseEncryptionMap(make([]byte, 7)); !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}

	// Exactly the required size is accepted.
	s = make([]byte, 16)
	s[3] = 1
	s[15] = 9

	if m, err := ParseEncryptionMap(s); err != nil || !reflect.DeepEqual(m, EncryptionMap{{0, 9}}) {
		t.Fatalf("got %v, %v", m, err)
	}
}

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	in := EncryptionMap{{0, 31}, {256, 511}, {1024, 2047}}

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 4+3*8 {
		t.Fatalf("len = %d, want 28", len(data))
	}

	// Layout: LE count, then LE start/end pairs.
	want := binary.LittleEndian.AppendUint32(nil, 3)
	for _, r := range in {
		want = binary.LittleEndian.AppendUint32(want, r.StartSector)
		want = binary.LittleEndian.AppendUint32(want, r.EndSector)
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("serialized = %x, want %x", data, want)
	}

	var out EncryptionMap
	if err := out.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(out, in) {
		t.Fatalf("got %v, want %v", out, in)
	}
}

func TestSerializeZeroRegions(t *testing.T) {
	data, err := EncryptionMap(nil).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 4 || data[0] != 0 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
		t.Fatalf("got %x, want 00000000", data)
	}

	m := EncryptionMap{{1, 2}}
	if err := m.UnmarshalBinary(data); err != nil || m != nil {
		t.Fatalf("got %v, %v; want nil map", m, err)
	}
}

func TestSerializeTooMany(t *testing.T) {
	if _, err := make(EncryptionMap, 65).MarshalBinary(); !errors.Is(err, ErrTooManyRegions) {
		t.Fatalf("err = %v, want ErrTooManyRegions", err)
	}
}

func TestDeserializeErrors(t *testing.T) {
	var m EncryptionMap

	if err := m.UnmarshalBinary([]byte{1, 0, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short header: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{65, 0, 0, 0}); !errors.Is(err, ErrTooManyRegions) {
		t.Errorf("too many: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{2, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated entries: %v", err)
	}
}

func TestSectorEncryptedOutside(t *testing.T) {
	m := EncryptionMap{{0, 31}, {256, 511}}

	for _, s := range []uint64{32, 100, 255, 512, 10000} {
		if !m.IsSectorEncrypted(s) {
			t.Errorf("sector %d should be encrypted", s)
		}
	}
}

func TestSectorPlaintextInside(t *testing.T) {
	m := EncryptionMap{{0, 31}, {256, 511}}

	for _, s := range []uint64{0, 15, 31, 256, 400, 511} {
		if m.IsSectorEncrypted(s) {
			t.Errorf("sector %d should be plaintext", s)
		}
	}
}

func TestSectorEncryptedNoRegions(t *testing.T) {
	var m EncryptionMap

	if !m.IsSectorEncrypted(0) || !m.IsSectorEncrypted(100) {
		t.Fatal("empty map must report every sector as encrypted")
	}
}

func TestParseThenLookup(t *testing.T) {
	m, err := ParseEncryptionMap(buildSyntheticSector0())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		sector    uint64
		encrypted bool
	}{{0, false}, {50, true}, {300, false}, {600, true}}

	for _, tt := range tests {
		if got := m.IsSectorEncrypted(tt.sector); got != tt.encrypted {
			t.Errorf("IsSectorEncrypted(%d) = %v, want %v", tt.sector, got, tt.encrypted)
		}
	}
}

func TestLargeSectorNumber(t *testing.T) {
	m := EncryptionMap{{0, 0xFFFFFFFF}}

	if m.IsSectorEncrypted(0xFFFFFFFF) {
		t.Error("sector 0xFFFFFFFF should be plaintext")
	}

	if !m.IsSectorEncrypted(0x100000000) {
		t.Error("sector 0x100000000 should be encrypted (beyond uint32 range)")
	}
}
