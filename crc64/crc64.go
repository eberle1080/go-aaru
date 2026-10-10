// Package crc64 implements the CRC-64/ECMA-182 checksum that AaruFormat
// stores for every checksummed block of an image.
//
// The parameters are those of the reflected ECMA-182 polynomial, also known
// as CRC-64/XZ or CRC-64/GO-ECMA:
//
//   - polynomial: 0xC96C5795D7870F42 (reflected form, [Poly])
//   - initial register value: 0xFFFFFFFFFFFFFFFF ([Seed])
//   - final XOR: 0xFFFFFFFFFFFFFFFF (the register is XORed with the seed)
//   - reflected input and output
//
// [Checksum] therefore equals hash/crc64.Checksum(data,
// crc64.MakeTable(crc64.ECMA)) from the standard library; the check value for
// "123456789" is 0x995DC9BBDF1939FA. This package uses its own pure-Go
// slicing-by-16 implementation, which is about 30% faster than the standard
// library's slicing-by-8 code.
//
// # API overview
//
//   - [Checksum](data) computes the finalized checksum of a buffer.
//   - [New], Write and Sum64 compute it incrementally.
//   - crc = [Update](crc, data) advances a raw (non-finalized) register.
//
// # On-disk representation
//
// Block headers store the CRC as a little-endian uint64 field, so the bytes on
// disk are binary.LittleEndian.AppendUint64(nil, Checksum(data)). Note that
// the hash.Hash64 returned by [New] follows the standard library convention
// and Sum appends the value big-endian, i.e. byte-reversed relative to the
// on-disk field; use Sum64 and encoding/binary when building headers.
//
// # Legacy (version 1) images
//
// Images whose imageMajorVersion is <= 1 were written by older versions of
// Aaru, which stored every block CRC64 byte-swapped. When verifying such
// images, compare bits.ReverseBytes64 of the computed checksum with the
// stored field, e.g.
//
//	crc := crc64.Checksum(payload)
//	if majorVersion <= 1 {
//		crc = bits.ReverseBytes64(crc)
//	}
//	ok := crc == header.Crc64
//
// Equivalently, the on-disk bytes of a version 1 CRC field are exactly what
// New().Sum(nil) returns.
package crc64

import (
	"encoding/binary"
	"hash"
)

const (
	// Size is the size of a CRC-64 checksum in bytes.
	Size = 8

	// Poly is the reflected ECMA-182 polynomial.
	Poly uint64 = 0xC96C5795D7870F42

	// Seed is the initial value of the CRC register. It is
	// also the value XORed into the register to produce the final checksum.
	Seed uint64 = 0xFFFFFFFFFFFFFFFF
)

// table holds the slicing-by-16 lookup tables. table[0] is the classic
// byte-at-a-time table; table[k][b] is the register contribution of byte b
// followed by k zero bytes.
var table = makeTables()

func makeTables() *[16][256]uint64 {
	t := new([16][256]uint64)
	for i := range 256 {
		c := uint64(i)
		for range 8 {
			if c&1 == 1 {
				c = c>>1 ^ Poly
			} else {
				c >>= 1
			}
		}
		t[0][i] = c
	}
	for i := range 256 {
		c := t[0][i]
		for k := 1; k < len(t); k++ {
			c = t[0][c&0xFF] ^ c>>8
			t[k][i] = c
		}
	}
	return t
}

// Checksum returns the CRC-64/ECMA-182 checksum of data, the value
// AaruFormat stores in block headers. The checksum of
// empty input is 0.
func Checksum(data []byte) uint64 {
	return Update(Seed, data) ^ Seed
}

// Update feeds data into the raw (non-finalized) CRC register crc and returns
// the new register value. No inversion is applied on entry or
// exit. Start from [Seed] and XOR the final register with [Seed] to obtain the
// checksum:
//
//	reg := crc64.Seed
//	reg = crc64.Update(reg, part1)
//	reg = crc64.Update(reg, part2)
//	sum := reg ^ crc64.Seed // == crc64.Checksum(append(part1, part2...))
//
// To extend an already finalized checksum c, use Update(c^Seed, more) ^ Seed.
//
// Note that this differs from hash/crc64.Update in the standard library, which
// takes and returns finalized checksums: crc64.Update(c, ecmaTable, p) ==
// Update(c^Seed, p)^Seed.
func Update(crc uint64, data []byte) uint64 {
	t := table
	for len(data) >= 16 {
		a := crc ^ binary.LittleEndian.Uint64(data)
		b := binary.LittleEndian.Uint64(data[8:])
		crc = t[15][a&0xFF] ^ t[14][a>>8&0xFF] ^ t[13][a>>16&0xFF] ^ t[12][a>>24&0xFF] ^
			t[11][a>>32&0xFF] ^ t[10][a>>40&0xFF] ^ t[9][a>>48&0xFF] ^ t[8][a>>56] ^
			t[7][b&0xFF] ^ t[6][b>>8&0xFF] ^ t[5][b>>16&0xFF] ^ t[4][b>>24&0xFF] ^
			t[3][b>>32&0xFF] ^ t[2][b>>40&0xFF] ^ t[1][b>>48&0xFF] ^ t[0][b>>56]
		data = data[16:]
	}
	for _, v := range data {
		crc = t[0][byte(crc)^v] ^ crc>>8
	}
	return crc
}

// digest is the streaming state; crc is the raw register.
type digest struct {
	crc uint64
}

// New returns a streaming CRC-64/ECMA-182 hash. Write never
// returns an error; Sum64 returns the finalized checksum
// without altering the state; Sum appends that value big-endian, following
// the hash.Hash64 convention (see the package documentation for how this
// relates to the little-endian on-disk field).
func New() hash.Hash64 {
	return &digest{crc: Seed}
}

func (d *digest) Write(p []byte) (int, error) {
	d.crc = Update(d.crc, p)
	return len(p), nil
}

func (d *digest) Sum64() uint64 { return d.crc ^ Seed }

func (d *digest) Sum(b []byte) []byte {
	return binary.BigEndian.AppendUint64(b, d.Sum64())
}

func (d *digest) Reset() { d.crc = Seed }

func (d *digest) Size() int { return Size }

func (d *digest) BlockSize() int { return 1 }
