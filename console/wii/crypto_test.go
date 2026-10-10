package wii

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"math/bits"
	"reflect"
	"testing"

	"github.com/eberle1080/go-aaru/console/ngcw"
)

// Wii crypto tests. Expected values are computed from the documented layout
// and construction (crypto/aes, ngcw.LFG), not stored.

func fillKey(b byte) [16]byte {
	var k [16]byte
	for i := range k {
		k[i] = b
	}

	return k
}

func TestKeyMapSerializeDeserializeRoundTrip(t *testing.T) {
	in := PartitionKeyMap{
		{StartSector: 100, EndSector: 500, Key: fillKey(0x11)},
		{StartSector: 600, EndSector: 1000, Key: fillKey(0x22)},
	}

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 52 { // 4 + 2*24
		t.Fatalf("len = %d, want 52", len(data))
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

func TestKeyMapInvalidRangeFails(t *testing.T) {
	data, err := PartitionKeyMap{{StartSector: 500, EndSector: 100}}.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var out PartitionKeyMap
	if err := out.UnmarshalBinary(data); !errors.Is(err, ErrInvalidRegion) {
		t.Fatalf("err = %v, want ErrInvalidRegion", err)
	}

	// start == end is also invalid.
	data, _ = PartitionKeyMap{{StartSector: 5, EndSector: 5}}.MarshalBinary()
	if err := out.UnmarshalBinary(data); !errors.Is(err, ErrInvalidRegion) {
		t.Fatalf("err = %v, want ErrInvalidRegion", err)
	}
}

func TestKeyMapErrors(t *testing.T) {
	if _, err := make(PartitionKeyMap, MaxPartitions+1).MarshalBinary(); !errors.Is(err, ErrTooManyPartitions) {
		t.Errorf("marshal too many: %v", err)
	}

	var m PartitionKeyMap

	if err := m.UnmarshalBinary([]byte{1, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{33, 0, 0, 0}); !errors.Is(err, ErrTooManyPartitions) {
		t.Errorf("too many: %v", err)
	}

	if err := m.UnmarshalBinary([]byte{1, 0, 0, 0, 1, 0, 0, 0}); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated: %v", err)
	}

	m = PartitionKeyMap{{}}
	if err := m.UnmarshalBinary([]byte{0, 0, 0, 0}); err != nil || m != nil {
		t.Errorf("empty: %v, %v", m, err)
	}
}

func testGroup() (plain []byte) {
	plain = make([]byte, GroupSize)
	for i := 0; i < HashSize; i++ {
		plain[i] = byte(i)
	}

	for i := 0; i < DataSize; i++ {
		plain[HashSize+i] = byte(i*7 + 3)
	}

	return plain
}

func TestGroupEncryptDecryptRoundTrip(t *testing.T) {
	key := fillKey(0x42)
	plain := testGroup()

	group := bytes.Clone(plain)
	if err := EncryptGroup(key, group); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(group[:HashSize], plain[:HashSize]) {
		t.Fatal("hash block not encrypted")
	}

	// The hash block is AES-128-CBC with a zero IV; the user data uses the
	// IV at 0x3D0 of the encrypted hash block.
	if want := cbcEncrypt(t, key, [16]byte{}, plain[:HashSize]); !bytes.Equal(group[:HashSize], want) {
		t.Fatal("hash block differs from AES-128-CBC with a zero IV")
	}

	var iv [16]byte
	copy(iv[:], group[0x3D0:0x3E0])

	if want := cbcEncrypt(t, key, iv, plain[HashSize:]); !bytes.Equal(group[HashSize:], want) {
		t.Fatal("user data differs from AES-128-CBC with the hash block IV")
	}

	// Wrong key does not decrypt.
	wrong := bytes.Clone(group)
	if err := DecryptGroup(fillKey(0x43), wrong); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(wrong[:HashSize], plain[:HashSize]) || bytes.Equal(wrong[HashSize:], plain[HashSize:]) {
		t.Fatal("wrong key reproduced the plaintext")
	}

	// Changing the hash block changes the user data ciphertext (through the
	// IV) even when the user data is identical.
	other := bytes.Clone(plain)
	other[0x3D0] ^= 1

	if err := EncryptGroup(key, other); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(other[HashSize:HashSize+16], group[HashSize:HashSize+16]) {
		t.Fatal("user data IV does not depend on the hash block")
	}

	if err := DecryptGroup(key, group); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(group[:HashSize], plain[:HashSize]) {
		t.Fatal("hash block round trip mismatch")
	}

	if !bytes.Equal(group[HashSize:], plain[HashSize:]) {
		t.Fatal("user data round trip mismatch")
	}
}

func TestGroupInvalidSize(t *testing.T) {
	for _, n := range []int{0, GroupSize - 16, GroupSize + 16} {
		if err := EncryptGroup(fillKey(1), make([]byte, n)); !errors.Is(err, ErrInvalidGroupSize) {
			t.Errorf("EncryptGroup(len=%d) = %v", n, err)
		}

		if err := DecryptGroup(fillKey(1), make([]byte, n)); !errors.Is(err, ErrInvalidGroupSize) {
			t.Errorf("DecryptGroup(len=%d) = %v", n, err)
		}
	}
}

// TestSectorKeyLookup checks per-sector key lookup. Unlike Wii U, the first
// group of a Wii partition region (StartSector) is not a plaintext header: it
// is the first group of the partition's *encrypted data area* (partition
// offset + data offset, see the Wii encryption appendix of the AaruFormat
// specification), so that group is encrypted and must yield the key.
func TestSectorKeyLookup(t *testing.T) {
	m := PartitionKeyMap{
		{StartSector: 10, EndSector: 100, Key: fillKey(0xAA)},
		{StartSector: 200, EndSector: 300, Key: fillKey(0xBB)},
	}

	tests := []struct {
		name    string
		sector  uint64
		wantKey byte // 0 means plaintext
	}{
		{"first group of first partition data area (corrected: encrypted)", 10 * LogicalPerGroup, 0xAA},
		{"last logical sector of first group", 10*LogicalPerGroup + 15, 0xAA},
		{"group before first partition", 9*LogicalPerGroup + 15, 0},
		{"first partition data", 11 * LogicalPerGroup, 0xAA},
		{"last group of first partition", 99*LogicalPerGroup + 15, 0xAA},
		{"end of first partition (exclusive)", 100 * LogicalPerGroup, 0},
		{"second partition", 201 * LogicalPerGroup, 0xBB},
		{"first group of second partition", 200 * LogicalPerGroup, 0xBB},
		{"between partitions", 150 * LogicalPerGroup, 0},
		{"disc header", 0, 0},
		{"after all partitions", 300 * LogicalPerGroup, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := m.SectorKey(tt.sector)

			if tt.wantKey == 0 {
				if ok {
					t.Fatalf("SectorKey(%d) returned key %x, want plaintext", tt.sector, key)
				}

				return
			}

			if !ok {
				t.Fatalf("SectorKey(%d) = plaintext, want key %#x", tt.sector, tt.wantKey)
			}

			if key != fillKey(tt.wantKey) {
				t.Fatalf("SectorKey(%d) = %x, want %#x...", tt.sector, key, tt.wantKey)
			}
		})
	}

	if !m.IsSectorEncrypted(50 * LogicalPerGroup) {
		t.Error("sector in partition should be encrypted")
	}

	if m.IsSectorEncrypted(150 * LogicalPerGroup) {
		t.Error("sector between partitions should be plaintext")
	}

	if PartitionKeyMap(nil).IsSectorEncrypted(50 * LogicalPerGroup) {
		t.Error("empty map should report plaintext")
	}
}

func seedFromSwapped(words [ngcw.SeedWords]uint32) ngcw.Seed {
	var s ngcw.Seed
	for i, w := range words {
		s[i] = bits.ReverseBytes32(w)
	}

	return s
}

// cbcEncrypt is an independent AES-128-CBC encryption built on crypto/aes.
func cbcEncrypt(t *testing.T, key, iv [16]byte, data []byte) []byte {
	t.Helper()

	blk, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}

	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(blk, iv[:]).CryptBlocks(out, data)

	return out
}

// Junk chunks are overwritten with the entry's LFG stream at position
// (groupIndexInPartition*DataSize + chunkOffset) mod GroupSize; everything else
// is left as stored. The result then survives an encrypt/decrypt round trip.
func TestReconstructGroupJunk(t *testing.T) {
	swappedTestSeed := [ngcw.SeedWords]uint32{
		0x01234567, 0x89ABCDEF, 0xFEDCBA98, 0x76543210, 0xDEADBEEF, 0xCAFEBABE, 0x12345678, 0x9ABCDEF0, 0x0FEDCBA9,
		0x87654321, 0xBEEFCAFE, 0xBABEFACE, 0x11223344, 0x55667788, 0x99AABBCC, 0xDDEEFF00, 0xA5A5A5A5,
	}

	var reversed [ngcw.SeedWords]uint32
	for i := range reversed {
		reversed[i] = swappedTestSeed[ngcw.SeedWords-1-i]
	}

	regions := PartitionKeyMap{
		{StartSector: 10, EndSector: 100, Key: fillKey(0x33)},
		{StartSector: 200, EndSector: 300, Key: fillKey(0x44)},
	}

	for _, physGroup := range []uint64{10, 12, 13, 201} {
		groupOff := physGroup * GroupSize
		part, _ := regions.Partition(physGroup)
		groupIdx := physGroup - uint64(regions[part].StartSector)

		// Chunks 2-4 from one seed; chunk 14 and the half chunk 15 from another.
		junk := ngcw.JunkMap{
			{Offset: groupOff + HashSize + 2048*2, Length: 2048 * 3, PartitionIndex: uint16(part), Seed: seedFromSwapped(swappedTestSeed)},
			{Offset: groupOff + HashSize + 2048*14, Length: 0x400 + 2048, PartitionIndex: uint16(part), Seed: seedFromSwapped(reversed)},
		}

		group := make([]byte, GroupSize)
		for i := range group {
			group[i] = byte(i*5 + 1)
		}

		original := bytes.Clone(group)

		if err := ReconstructGroupJunk(group, physGroup, regions, junk); err != nil {
			t.Fatal(err)
		}

		want := bytes.Clone(original)

		for off := uint64(0); off < DataSize; off += SectorSize {
			e, ok := junk.Find(groupOff + HashSize + off)
			if !ok {
				continue
			}

			pos := (groupIdx*DataSize + off) % GroupSize
			chunk := min(uint64(DataSize)-off, SectorSize)

			stream := make([]byte, pos+chunk)
			_, _ = ngcw.NewLFG(junk[e].Seed).Read(stream)
			copy(want[HashSize+off:], stream[pos:])
		}

		if !bytes.Equal(group, want) {
			t.Fatalf("group %d: reconstruction differs from the LFG stream", physGroup)
		}

		// Sanity: the expected regions did change, the rest did not.
		for _, c := range []struct {
			lo, hi  int
			changed bool
		}{
			{0, HashSize + 2*2048, false},
			{HashSize + 2*2048, HashSize + 5*2048, true},
			{HashSize + 5*2048, HashSize + 14*2048, false},
			{HashSize + 14*2048, GroupSize, true},
		} {
			if bytes.Equal(group[c.lo:c.hi], original[c.lo:c.hi]) == c.changed {
				t.Errorf("group %d: [%#x,%#x) changed=%v, want %v", physGroup, c.lo, c.hi, !c.changed, c.changed)
			}
		}

		reconstructed := bytes.Clone(group)

		if err := EncryptGroup(regions[part].Key, group); err != nil {
			t.Fatal(err)
		}

		if err := DecryptGroup(regions[part].Key, group); err != nil || !bytes.Equal(group, reconstructed) {
			t.Fatalf("group %d: re-encryption round trip failed (err=%v)", physGroup, err)
		}
	}
}

func TestReconstructGroupJunkNoop(t *testing.T) {
	regions := PartitionKeyMap{{StartSector: 10, EndSector: 100, Key: fillKey(0x33)}}
	junk := ngcw.JunkMap{{Offset: 0, Length: 1 << 40}}

	group := testGroup()
	original := bytes.Clone(group)

	// Outside every partition: untouched.
	if err := ReconstructGroupJunk(group, 5, regions, junk); err != nil || !bytes.Equal(group, original) {
		t.Fatalf("group outside partitions modified (err=%v)", err)
	}

	// No junk: untouched.
	if err := ReconstructGroupJunk(group, 12, regions, nil); err != nil || !bytes.Equal(group, original) {
		t.Fatalf("group modified without junk (err=%v)", err)
	}

	if err := ReconstructGroupJunk(make([]byte, 10), 12, regions, junk); !errors.Is(err, ErrInvalidGroupSize) {
		t.Fatalf("short group: %v", err)
	}
}
