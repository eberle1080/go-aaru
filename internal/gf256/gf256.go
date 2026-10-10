// Package gf256 implements arithmetic over the Galois field GF(2^8) as used by
// the AaruFormat Reed-Solomon erasure code.
//
// The field is defined by the irreducible polynomial x^8 + x^4 + x^3 + x^2 + 1
// (0x11D) with generator element 2.
// Addition is XOR. Scalar operations use log/anti-log tables; region
// operations use a precomputed 256x256 multiplication table so that each byte
// of a region costs a single table lookup.
package gf256

import (
	"crypto/subtle"
	"encoding/binary"
)

// Polynomial is the irreducible polynomial defining the field (x^8+x^4+x^3+x^2+1).
const Polynomial = 0x11D

var (
	// logTable[x] is the discrete logarithm base 2 of x. logTable[0] is 0 by
	// convention (it is never used by Mul, which short-circuits zero).
	logTable [256]byte
	// expTable[i] = 2^i. It has 512 entries so that sums of two logarithms
	// never need a modular reduction.
	expTable [512]byte
	// mulTable[c][x] = c*x.
	mulTable [256][256]byte
)

func init() {
	x := 1
	for i := range 255 {
		expTable[i] = byte(x)
		expTable[i+255] = byte(x)
		logTable[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= Polynomial
		}
	}
	logTable[0] = 0
	expTable[510] = expTable[0]
	expTable[511] = expTable[1]

	for c := 1; c < 256; c++ {
		lc := int(logTable[c])
		for v := 1; v < 256; v++ {
			mulTable[c][v] = expTable[lc+int(logTable[v])]
		}
	}
}

// Mul returns a*b in GF(2^8).
func Mul(a, b byte) byte {
	return mulTable[a][b]
}

// Div returns a/b in GF(2^8).
//
// b must be non-zero. No check is made: Div(a, 0)
// returns a (because log(0) is defined as 0); it never panics.
func Div(a, b byte) byte {
	if a == 0 {
		return 0
	}
	return expTable[int(logTable[a])+255-int(logTable[b])]
}

// Inv returns the multiplicative inverse of a in GF(2^8).
//
// a must be non-zero. Inv(0) returns 1; it never
// panics.
func Inv(a byte) byte {
	return expTable[255-int(logTable[a])]
}

// MulRegion multiply-accumulates a region: dst[i] ^= c*src[i].
//
// It operates on the first min(len(dst), len(src)) bytes. A coefficient of 0
// is a no-op and a coefficient of 1 is a plain XOR.
func MulRegion(dst, src []byte, c byte) {
	switch c {
	case 0:
		return
	case 1:
		XorRegion(dst, src)
		return
	}

	n := min(len(dst), len(src))
	dst, src = dst[:n], src[:n]
	t := &mulTable[c]

	for len(src) >= 8 && len(dst) >= 8 {
		s := src[:8:8]
		v := uint64(t[s[0]]) | uint64(t[s[1]])<<8 | uint64(t[s[2]])<<16 | uint64(t[s[3]])<<24 |
			uint64(t[s[4]])<<32 | uint64(t[s[5]])<<40 | uint64(t[s[6]])<<48 | uint64(t[s[7]])<<56
		binary.LittleEndian.PutUint64(dst, binary.LittleEndian.Uint64(dst)^v)
		dst, src = dst[8:], src[8:]
	}

	for i, b := range src {
		dst[i] ^= t[b]
	}
}

// XorRegion XORs a region: dst[i] ^= src[i].
//
// It operates on the first min(len(dst), len(src)) bytes.
func XorRegion(dst, src []byte) {
	n := min(len(dst), len(src))
	subtle.XORBytes(dst[:n], dst[:n], src[:n])
}
