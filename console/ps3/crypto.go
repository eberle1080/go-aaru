// Package ps3 implements Sony PlayStation 3 disc encryption as used by
// AaruFormat: disc key derivation from data1, per-sector AES-128-CBC
// encryption/decryption, and the plaintext region map found in sector 0
// (and its little-endian media tag serialization, MediaTagPs3EncryptionMap).
//
// See the PS3 encryption appendix of the AaruFormat specification.
package ps3

import (
	"encoding/binary"
	"errors"

	"github.com/eberle1080/go-aaru/console/internal/aescbc"
)

// SectorSize is the PS3 disc sector size in bytes.
const SectorSize = 2048

// ErrInvalidLength is returned when sector data is not a multiple of the AES
// block size (16 bytes).
var ErrInvalidLength = errors.New("ps3: data length is not a multiple of 16")

// erk is the publicly known PS3 Encryption Round Key.
var erk = [16]byte{0x38, 0x0B, 0xCF, 0x0B, 0x53, 0x45, 0x5B, 0x3C, 0x78, 0x17, 0xAB, 0x4F, 0xA3, 0xBA, 0x90, 0xED}

// erkIV is the publicly known IV associated with erk.
var erkIV = [16]byte{0x69, 0x47, 0x47, 0x72, 0xAF, 0x6F, 0xDA, 0xB3, 0x42, 0x74, 0x3A, 0xEF, 0xAA, 0x18, 0x62, 0x87}

// DeriveDiscKey derives the per-disc AES key from the disc's data1 key:
//
//	disc_key = AES-128-CBC-Encrypt(ERK, ERK_IV, data1)
func DeriveDiscKey(data1 [16]byte) [16]byte {
	key := data1
	_ = aescbc.Encrypt(erk, erkIV, key[:]) // 16 bytes: cannot fail

	return key
}

// DeriveIV returns the AES IV for a sector: the sector number as a 128-bit
// big-endian integer.
func DeriveIV(sector uint64) [16]byte {
	var iv [16]byte
	binary.BigEndian.PutUint64(iv[8:], sector)

	return iv
}

// EncryptSector encrypts data (normally SectorSize bytes) in place with the
// disc key, using the IV derived from sector.
func EncryptSector(key [16]byte, sector uint64, data []byte) error {
	if len(data)%aescbc.BlockSize != 0 {
		return ErrInvalidLength
	}

	return aescbc.Encrypt(key, DeriveIV(sector), data)
}

// DecryptSector decrypts data (normally SectorSize bytes) in place with the
// disc key, using the IV derived from sector.
func DecryptSector(key [16]byte, sector uint64, data []byte) error {
	if len(data)%aescbc.BlockSize != 0 {
		return ErrInvalidLength
	}

	return aescbc.Decrypt(key, DeriveIV(sector), data)
}
