// Package aescbc provides the in-place AES-128-CBC primitive shared by the
// console disc encryption packages (PS3, Wii and Wii U all use AES-128-CBC
// without padding). It is a thin wrapper over crypto/aes and crypto/cipher.
package aescbc

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

// BlockSize is the AES block size in bytes.
const BlockSize = aes.BlockSize

// ErrInvalidLength is returned when the data length is not a multiple of
// BlockSize.
var ErrInvalidLength = errors.New("aescbc: data length is not a multiple of the AES block size")

func newBlock(key [16]byte) cipher.Block {
	b, err := aes.NewCipher(key[:])
	if err != nil {
		// A 16-byte key is always valid for AES-128.
		panic("aescbc: unexpected AES key error: " + err.Error())
	}

	return b
}

// Encrypt encrypts data in place with AES-128-CBC using key and iv. The length
// of data must be a multiple of BlockSize (zero is allowed).
func Encrypt(key, iv [16]byte, data []byte) error {
	if len(data)%BlockSize != 0 {
		return ErrInvalidLength
	}

	if len(data) == 0 {
		return nil
	}

	cipher.NewCBCEncrypter(newBlock(key), iv[:]).CryptBlocks(data, data)

	return nil
}

// Decrypt decrypts data in place with AES-128-CBC using key and iv. The length
// of data must be a multiple of BlockSize (zero is allowed).
func Decrypt(key, iv [16]byte, data []byte) error {
	if len(data)%BlockSize != 0 {
		return ErrInvalidLength
	}

	if len(data) == 0 {
		return nil
	}

	cipher.NewCBCDecrypter(newBlock(key), iv[:]).CryptBlocks(data, data)

	return nil
}
