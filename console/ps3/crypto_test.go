package ps3

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"testing"
)

// PS3 crypto tests. Expected values are computed from the documented
// construction with crypto/aes, not stored.

func TestDeriveIV(t *testing.T) {
	tests := []struct {
		name   string
		sector uint64
		want   [16]byte
	}{
		{"IvDeriveSector0", 0, [16]byte{}},
		{"IvDeriveSector1", 1, [16]byte{15: 1}},
		{"IvDeriveSectorFF", 0xFF, [16]byte{15: 0xFF}},
		{"IvDeriveSectorMultiByte", 0x12345678, [16]byte{12: 0x12, 13: 0x34, 14: 0x56, 15: 0x78}},
		{"IvDeriveSectorLarge", 0xABCDEF0123456789,
			[16]byte{8: 0xAB, 9: 0xCD, 10: 0xEF, 11: 0x01, 12: 0x23, 13: 0x45, 14: 0x67, 15: 0x89}},
		{"Sector256", 256, [16]byte{14: 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveIV(tt.sector); got != tt.want {
				t.Fatalf("DeriveIV(%#x) = %x, want %x", tt.sector, got, tt.want)
			}
		})
	}
}

var (
	data1A = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	data1B = [16]byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8, 0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0}
	data1C = [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00}

	testDiscKey = [16]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
)

func TestDiscKeyDerivation(t *testing.T) {
	key := DeriveDiscKey(data1A)

	if key == ([16]byte{}) {
		t.Fatal("disc key is all zeros")
	}

	if key == data1A {
		t.Fatal("disc key equals data1")
	}

	if key2 := DeriveDiscKey(data1A); key2 != key {
		t.Fatal("derivation is not deterministic")
	}
}

func TestDiscKeyDifferentData1(t *testing.T) {
	if DeriveDiscKey(data1A) == DeriveDiscKey(data1B) {
		t.Fatal("different data1 produced identical disc keys")
	}
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

// The disc key is AES-128-CBC-Encrypt(ERK, ERK_IV, data1).
func TestDiscKeyMatchesConstruction(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))

	seen := map[[16]byte]bool{}

	for i := 0; i < 64; i++ {
		var d1 [16]byte
		for j := range d1 {
			d1[j] = byte(rng.Uint32())
		}

		if i < 3 {
			d1 = [][16]byte{data1A, data1B, data1C}[i]
		}

		key := DeriveDiscKey(d1)
		if want := cbcEncrypt(t, erk, erkIV, d1[:]); !bytes.Equal(key[:], want) {
			t.Fatalf("DeriveDiscKey(%x) = %x, want %x", d1, key, want)
		}

		// AES is a permutation: distinct data1 give distinct keys.
		if seen[key] {
			t.Fatalf("disc key %x repeated", key)
		}

		seen[key] = true
	}
}

func patternSector(f func(i int) byte) []byte {
	b := make([]byte, SectorSize)
	for i := range b {
		b[i] = f(i)
	}

	return b
}

func TestSectorRoundTrip(t *testing.T) {
	original := patternSector(func(i int) byte { return byte(i) })
	data := bytes.Clone(original)

	if err := EncryptSector(testDiscKey, 42, data); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(data, original) {
		t.Fatal("encryption did not change data")
	}

	if err := DecryptSector(testDiscKey, 42, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, original) {
		t.Fatal("round trip mismatch")
	}
}

func TestDifferentSectorsDifferentCiphertext(t *testing.T) {
	plain := patternSector(func(i int) byte { return byte(i) })
	a, b := bytes.Clone(plain), bytes.Clone(plain)

	if err := EncryptSector(testDiscKey, 100, a); err != nil {
		t.Fatal(err)
	}

	if err := EncryptSector(testDiscKey, 200, b); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(a, b) {
		t.Fatal("different sectors produced identical ciphertext")
	}
}

func TestFullPipelineRoundTrip(t *testing.T) {
	key := DeriveDiscKey(data1C)
	original := patternSector(func(i int) byte { return byte(i*3 + 7) })
	data := bytes.Clone(original)

	if err := EncryptSector(key, 1000, data); err != nil {
		t.Fatal(err)
	}

	if err := DecryptSector(key, 1000, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, original) {
		t.Fatal("round trip mismatch")
	}
}

// EncryptSector is AES-128-CBC with the disc key and the sector number as a
// big-endian 128-bit IV.
func TestEncryptSectorMatchesConstruction(t *testing.T) {
	tests := []struct {
		name   string
		key    [16]byte
		sector uint64
		plain  []byte
	}{
		{"sector42", testDiscKey, 42, patternSector(func(i int) byte { return byte(i) })},
		{"derived1000", DeriveDiscKey(data1C), 1000, patternSector(func(i int) byte { return byte(i*3 + 7) })},
		{"sector0", testDiscKey, 0, make([]byte, SectorSize)},
		{"maxSector", testDiscKey, ^uint64(0), patternSector(func(i int) byte { return byte(i >> 3) })},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := [16]byte{}
			binary.BigEndian.PutUint64(iv[8:], tt.sector)

			data := bytes.Clone(tt.plain)
			if err := EncryptSector(tt.key, tt.sector, data); err != nil {
				t.Fatal(err)
			}

			if want := cbcEncrypt(t, tt.key, iv, tt.plain); !bytes.Equal(data, want) {
				t.Fatal("ciphertext differs from AES-128-CBC with the sector IV")
			}

			if err := DecryptSector(tt.key, tt.sector, data); err != nil || !bytes.Equal(data, tt.plain) {
				t.Fatalf("round trip failed (err=%v)", err)
			}
		})
	}
}

// Decrypting with the wrong disc key or sector number does not reproduce the
// plaintext; a wrong sector number only corrupts the first AES block.
func TestDecryptWrongKeyOrSector(t *testing.T) {
	plain := patternSector(func(i int) byte { return byte(i * 5) })

	enc := bytes.Clone(plain)
	if err := EncryptSector(testDiscKey, 77, enc); err != nil {
		t.Fatal(err)
	}

	wrongKey := bytes.Clone(enc)
	if err := DecryptSector(DeriveDiscKey(data1A), 77, wrongKey); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(wrongKey, plain) {
		t.Fatal("wrong key reproduced the plaintext")
	}

	wrongSector := bytes.Clone(enc)
	if err := DecryptSector(testDiscKey, 78, wrongSector); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(wrongSector[:16], plain[:16]) || !bytes.Equal(wrongSector[16:], plain[16:]) {
		t.Fatal("wrong sector must corrupt exactly the first block")
	}
}

// Sectors are independent: encrypting a run of sectors one at a time equals
// encrypting each alone, and partial (16-byte multiple) buffers work.
func TestSectorIndependence(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))

	disc := make([]byte, 4*SectorSize)
	for i := range disc {
		disc[i] = byte(rng.Uint32())
	}

	enc := bytes.Clone(disc)
	for s := 0; s < 4; s++ {
		if err := EncryptSector(testDiscKey, uint64(100+s), enc[s*SectorSize:(s+1)*SectorSize]); err != nil {
			t.Fatal(err)
		}
	}

	// Decrypt in reverse order: no state carries between sectors.
	for s := 3; s >= 0; s-- {
		if err := DecryptSector(testDiscKey, uint64(100+s), enc[s*SectorSize:(s+1)*SectorSize]); err != nil {
			t.Fatal(err)
		}
	}

	if !bytes.Equal(enc, disc) {
		t.Fatal("per-sector round trip mismatch")
	}

	// A prefix of a sector encrypts to the prefix of the full ciphertext.
	full := bytes.Clone(disc[:SectorSize])
	part := bytes.Clone(disc[:256])

	if err := EncryptSector(testDiscKey, 5, full); err != nil {
		t.Fatal(err)
	}

	if err := EncryptSector(testDiscKey, 5, part); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(part, full[:256]) {
		t.Fatal("prefix encryption differs")
	}
}

func TestSectorInvalidLength(t *testing.T) {
	if err := EncryptSector(testDiscKey, 0, make([]byte, 2047)); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("EncryptSector = %v, want ErrInvalidLength", err)
	}

	if err := DecryptSector(testDiscKey, 0, make([]byte, 10)); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("DecryptSector = %v, want ErrInvalidLength", err)
	}
}
