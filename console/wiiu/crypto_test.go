package wiiu

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"
)

// These tests follow the Wii U encryption appendix of the AaruFormat
// specification. Expected bytes are built from the documented layout and
// construction, not stored.

func fillKey(b byte) [16]byte {
	var k [16]byte
	for i := range k {
		k[i] = b
	}

	return k
}

func TestKeyMapRoundTrip(t *testing.T) {
	var key [16]byte
	for i := range key {
		key[i] = byte(i)
	}

	in := PartitionKeyMap{{StartSector: 4, EndSector: 0x1234, Key: key}, {StartSector: 0x2000, EndSector: 0xFFFFFFFF, Key: fillKey(0xC3)}}

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	// Layout: LE count, then { LE start, LE end, 16-byte key } per partition.
	want := binary.LittleEndian.AppendUint32(nil, uint32(len(in)))
	for _, r := range in {
		want = binary.LittleEndian.AppendUint32(want, r.StartSector)
		want = binary.LittleEndian.AppendUint32(want, r.EndSector)
		want = append(want, r.Key[:]...)
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("serialized = %x, want %x", data, want)
	}

	var out PartitionKeyMap
	if err := out.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(out, in) {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestKeyMapErrors(t *testing.T) {
	if _, err := make(PartitionKeyMap, MaxPartitions+1).MarshalBinary(); !errors.Is(err, ErrTooManyPartitions) {
		t.Errorf("marshal too many: %v", err)
	}

	var m PartitionKeyMap

	if err := m.UnmarshalBinary([]byte{1, 0, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{9, 0, 0, 0}); !errors.Is(err, ErrTooManyPartitions) {
		t.Errorf("too many: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{1, 0, 0, 0, 1, 0, 0, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated: %v", err)
	}

	bad, _ := PartitionKeyMap{{StartSector: 10, EndSector: 10}}.MarshalBinary()
	if err := m.UnmarshalBinary(bad); !errors.Is(err, ErrInvalidRegion) {
		t.Errorf("invalid region: %v", err)
	}

	m = PartitionKeyMap{{}}
	if err := m.UnmarshalBinary([]byte{0, 0, 0, 0}); err != nil || m != nil {
		t.Errorf("empty: %v, %v", m, err)
	}
}

func TestSectorKeyLookup(t *testing.T) {
	m := PartitionKeyMap{
		{StartSector: 3, EndSector: 10, Key: fillKey(0xAA)}, // e.g. SI partition right after the header
		{StartSector: 10, EndSector: 50, Key: fillKey(0xBB)},
		{StartSector: 100, EndSector: 200, Key: fillKey(0xCC)},
	}

	tests := []struct {
		name    string
		phys    uint64
		slice   uint64
		wantKey byte // 0 means plaintext
	}{
		{"disc header 0", 0, 0, 0},
		{"disc header 2 last slice", 2, 15, 0},
		{"first partition header", 3, 0, 0},
		{"first partition header last slice", 3, 15, 0},
		{"first partition data", 4, 0, 0xAA},
		{"first partition last sector", 9, 15, 0xAA},
		{"second partition header", 10, 7, 0},
		{"second partition data", 11, 0, 0xBB},
		{"gap", 50, 0, 0},
		{"gap 2", 99, 15, 0},
		{"third partition header", 100, 0, 0},
		{"third partition data", 150, 3, 0xCC},
		{"end exclusive", 200, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lba := tt.phys*LogicalPerPhysical + tt.slice
			key, ok := m.SectorKey(lba)

			if tt.wantKey == 0 {
				if ok || m.IsSectorEncrypted(lba) {
					t.Fatalf("SectorKey(%d) = %x, want plaintext", lba, key)
				}

				return
			}

			if !ok || key != fillKey(tt.wantKey) || !m.IsSectorEncrypted(lba) {
				t.Fatalf("SectorKey(%d) = %x,%v want %#x", lba, key, ok, tt.wantKey)
			}
		})
	}

	// A region overlapping the disc header never yields a key for header sectors.
	hdr := PartitionKeyMap{{StartSector: 0, EndSector: 10, Key: fillKey(1)}}
	if hdr.IsSectorEncrypted(1 * LogicalPerPhysical) {
		t.Error("disc header sector reported encrypted")
	}

	if !hdr.IsSectorEncrypted(3 * LogicalPerPhysical) {
		t.Error("sector 3 inside region should be encrypted")
	}

	if PartitionKeyMap(nil).IsSectorEncrypted(1000) {
		t.Error("empty map should report plaintext")
	}
}

// Physical sectors are AES-128-CBC with an all-zero IV, restarted for every
// sector.
func TestPhysicalSectorCrypto(t *testing.T) {
	key := fillKey(0x5A)

	rng := rand.New(rand.NewPCG(5, 6))

	plain := make([]byte, 2*PhysicalSectorSize)
	for i := range plain {
		plain[i] = byte(rng.Uint32())
	}

	blk, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}

	data := bytes.Clone(plain)

	for s := 0; s < 2; s++ {
		sec := data[s*PhysicalSectorSize : (s+1)*PhysicalSectorSize]
		if err := EncryptPhysicalSector(key, sec); err != nil {
			t.Fatal(err)
		}

		want := make([]byte, PhysicalSectorSize)
		cipher.NewCBCEncrypter(blk, make([]byte, 16)).CryptBlocks(want, plain[s*PhysicalSectorSize:(s+1)*PhysicalSectorSize])

		if !bytes.Equal(sec, want) {
			t.Fatalf("sector %d differs from AES-128-CBC with a zero IV", s)
		}
	}

	// Equal plaintext sectors encrypt identically (the IV does not depend on
	// the sector number).
	a, b := bytes.Clone(plain[:PhysicalSectorSize]), bytes.Clone(plain[:PhysicalSectorSize])
	_ = EncryptPhysicalSector(key, a)
	_ = EncryptPhysicalSector(key, b)

	if !bytes.Equal(a, b) {
		t.Fatal("encryption is not deterministic")
	}

	// Wrong key does not decrypt.
	wrong := bytes.Clone(data[:PhysicalSectorSize])
	if err := DecryptPhysicalSector(fillKey(0x5B), wrong); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(wrong, plain[:PhysicalSectorSize]) {
		t.Fatal("wrong key reproduced the plaintext")
	}

	for s := 0; s < 2; s++ {
		if err := DecryptPhysicalSector(key, data[s*PhysicalSectorSize:(s+1)*PhysicalSectorSize]); err != nil {
			t.Fatal(err)
		}
	}

	if !bytes.Equal(data, plain) {
		t.Fatal("round trip mismatch")
	}

	if err := EncryptPhysicalSector(key, make([]byte, 100)); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("EncryptPhysicalSector(100 bytes) = %v", err)
	}

	if err := DecryptPhysicalSector(key, make([]byte, 100)); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("DecryptPhysicalSector(100 bytes) = %v", err)
	}
}
