package aescbc

import (
	"bytes"
	"errors"
	"testing"
)

// Standard vectors: NIST SP 800-38A F.2.1 / F.2.2 (CBC-AES128).

var (
	nistKey = [16]byte{0x2b, 0x7e, 0x15, 0x16, 0x28, 0xae, 0xd2, 0xa6, 0xab, 0xf7, 0x15, 0x88, 0x09, 0xcf, 0x4f, 0x3c}
	nistIV  = [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}

	nistPlaintext = []byte{
		0x6b, 0xc1, 0xbe, 0xe2, 0x2e, 0x40, 0x9f, 0x96, 0xe9, 0x3d, 0x7e, 0x11, 0x73, 0x93, 0x17, 0x2a,
		0xae, 0x2d, 0x8a, 0x57, 0x1e, 0x03, 0xac, 0x9c, 0x9e, 0xb7, 0x6f, 0xac, 0x45, 0xaf, 0x8e, 0x51,
		0x30, 0xc8, 0x1c, 0x46, 0xa3, 0x5c, 0xe4, 0x11, 0xe5, 0xfb, 0xc1, 0x19, 0x1a, 0x0a, 0x52, 0xef,
		0xf6, 0x9f, 0x24, 0x45, 0xdf, 0x4f, 0x9b, 0x17, 0xad, 0x2b, 0x41, 0x7b, 0xe6, 0x6c, 0x37, 0x10,
	}

	nistCiphertext = []byte{
		0x76, 0x49, 0xab, 0xac, 0x81, 0x19, 0xb2, 0x46, 0xce, 0xe9, 0x8e, 0x9b, 0x12, 0xe9, 0x19, 0x7d,
		0x50, 0x86, 0xcb, 0x9b, 0x50, 0x72, 0x19, 0xee, 0x95, 0xdb, 0x11, 0x3a, 0x91, 0x76, 0x78, 0xb2,
		0x73, 0xbe, 0xd6, 0xb8, 0xe3, 0xc1, 0x74, 0x3b, 0x71, 0x16, 0xe6, 0x9e, 0x22, 0x22, 0x95, 0x16,
		0x3f, 0xf1, 0xca, 0xa1, 0x68, 0x1f, 0xac, 0x09, 0x12, 0x0e, 0xca, 0x30, 0x75, 0x86, 0xe1, 0xa7,
	}
)

func TestCbcEncryptNist(t *testing.T) {
	data := bytes.Clone(nistPlaintext)
	if err := Encrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, nistCiphertext) {
		t.Fatalf("ciphertext mismatch: %x", data)
	}
}

func TestCbcDecryptNist(t *testing.T) {
	data := bytes.Clone(nistCiphertext)
	if err := Decrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, nistPlaintext) {
		t.Fatalf("plaintext mismatch: %x", data)
	}
}

func TestCbcRoundTrip(t *testing.T) {
	data := bytes.Clone(nistPlaintext)
	if err := Encrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(data, nistPlaintext) {
		t.Fatal("encryption did not change data")
	}

	if err := Decrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, nistPlaintext) {
		t.Fatal("round trip mismatch")
	}
}

func TestCbcSingleBlock(t *testing.T) {
	data := bytes.Clone(nistPlaintext[:16])
	if err := Encrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, nistCiphertext[:16]) {
		t.Fatalf("first block mismatch: %x", data)
	}

	if err := Decrypt(nistKey, nistIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, nistPlaintext[:16]) {
		t.Fatal("single block round trip mismatch")
	}
}

func TestCbcZeroIv(t *testing.T) {
	var zeroIV [16]byte

	original := make([]byte, 32)
	for i := range original {
		original[i] = byte(i*7 + 3)
	}

	data := bytes.Clone(original)
	if err := Encrypt(nistKey, zeroIV, data); err != nil {
		t.Fatal(err)
	}

	if err := Decrypt(nistKey, zeroIV, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, original) {
		t.Fatal("zero IV round trip mismatch")
	}
}

func TestCbcSectorSize(t *testing.T) {
	key := testKey(0x5C)
	iv := [16]byte{15: 0x01}

	original := make([]byte, 2048)
	for i := range original {
		original[i] = byte(i)
	}

	data := bytes.Clone(original)
	if err := Encrypt(key, iv, data); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(data, original) {
		t.Fatal("encryption did not change data")
	}

	if err := Decrypt(key, iv, data); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, original) {
		t.Fatal("sector round trip mismatch")
	}
}

func TestInvalidLength(t *testing.T) {
	for _, n := range []int{1, 15, 17, 2047} {
		if err := Encrypt(nistKey, nistIV, make([]byte, n)); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("Encrypt(len=%d) = %v, want ErrInvalidLength", n, err)
		}

		if err := Decrypt(nistKey, nistIV, make([]byte, n)); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("Decrypt(len=%d) = %v, want ErrInvalidLength", n, err)
		}
	}

	if err := Encrypt(nistKey, nistIV, nil); err != nil {
		t.Errorf("Encrypt(nil) = %v", err)
	}
}

func testKey(seed byte) [16]byte {
	var k [16]byte
	for i := range k {
		k[i] = seed + byte(i*29)
	}

	return k
}

// Decrypting with a different key or IV does not reproduce the plaintext. A
// wrong IV only corrupts the first block (CBC: P[i] = D(C[i]) ^ C[i-1]).
func TestWrongKeyOrIV(t *testing.T) {
	plain := make([]byte, 4*BlockSize)
	for i := range plain {
		plain[i] = byte(i * 13)
	}

	enc := bytes.Clone(plain)
	if err := Encrypt(nistKey, nistIV, enc); err != nil {
		t.Fatal(err)
	}

	wrongKey := bytes.Clone(enc)
	if err := Decrypt(testKey(1), nistIV, wrongKey); err != nil {
		t.Fatal(err)
	}

	for b := 0; b < len(plain); b += BlockSize {
		if bytes.Equal(wrongKey[b:b+BlockSize], plain[b:b+BlockSize]) {
			t.Errorf("wrong key reproduced block %d", b/BlockSize)
		}
	}

	wrongIV := bytes.Clone(enc)
	iv := nistIV
	iv[0] ^= 0x80

	if err := Decrypt(nistKey, iv, wrongIV); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(wrongIV[:BlockSize], plain[:BlockSize]) {
		t.Error("wrong IV reproduced the first block")
	}

	if wrongIV[0]^plain[0] != 0x80 || !bytes.Equal(wrongIV[1:], plain[1:]) {
		t.Error("wrong IV must flip exactly the IV bits of the first block")
	}
}

// Encryption chains blocks: changing one plaintext block changes that
// ciphertext block and every later one, but none before it.
func TestChaining(t *testing.T) {
	plain := make([]byte, 8*BlockSize)

	base := bytes.Clone(plain)
	if err := Encrypt(nistKey, nistIV, base); err != nil {
		t.Fatal(err)
	}

	plain[3*BlockSize+5] ^= 1
	if err := Encrypt(nistKey, nistIV, plain); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plain[:3*BlockSize], base[:3*BlockSize]) {
		t.Error("blocks before the change differ")
	}

	for b := 3; b < 8; b++ {
		if bytes.Equal(plain[b*BlockSize:(b+1)*BlockSize], base[b*BlockSize:(b+1)*BlockSize]) {
			t.Errorf("block %d unchanged after an earlier plaintext change", b)
		}
	}

	// Identical plaintext blocks encrypt to distinct ciphertext blocks.
	for b := 1; b < 8; b++ {
		if bytes.Equal(base[:BlockSize], base[b*BlockSize:(b+1)*BlockSize]) {
			t.Errorf("block %d repeats block 0", b)
		}
	}
}
