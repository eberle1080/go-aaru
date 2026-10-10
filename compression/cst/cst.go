// Package cst implements the CD Subchannel Transform used by
// AaruFormat to make Compact Disc subchannel data compressible (compression
// types LzmaCst and ZstdCst).
//
// Raw subchannel bytes carry one bit of each of the eight channels P..W
// (bit 7 = P ... bit 0 = W). Transform de-interleaves every group of eight
// bytes into one byte per channel and lays the channels out sequentially
// (all P bytes, then all Q bytes, ..., then all W bytes); Untransform
// reverses it. The layout follows the "CD Subchannel Transform"
// appendix of the AaruFormat specification.
package cst

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors returned by the transform functions.
var (
	// ErrInvalidLength reports an input whose length is not a multiple of
	// 8, which the transform requires (CD subchannel data is 96 bytes per
	// sector).
	ErrInvalidLength = errors.New("cst: length is not a multiple of 8")

	// ErrBufferTooSmall reports a destination buffer shorter than the
	// source.
	ErrBufferTooSmall = errors.New("cst: destination buffer too small")
)

// Transform converts interleaved subchannel data into the sequential
// (per-channel) layout. len(interleaved) must be a multiple of 8.
func Transform(interleaved []byte) ([]byte, error) {
	if err := checkLen(interleaved); err != nil {
		return nil, err
	}
	out := make([]byte, len(interleaved))
	transform(out, interleaved)
	return out, nil
}

// Untransform converts sequential (per-channel) subchannel data back into
// the interleaved layout. len(sequential) must be a multiple of 8.
func Untransform(sequential []byte) ([]byte, error) {
	if err := checkLen(sequential); err != nil {
		return nil, err
	}
	out := make([]byte, len(sequential))
	untransform(out, sequential)
	return out, nil
}

// TransformInto is like Transform but writes the result into
// dst[:len(src)]. dst may be the same slice as src (in-place
// operation); otherwise the two must not overlap.
func TransformInto(dst, src []byte) error {
	if err := checkInto(dst, src); err != nil {
		return err
	}
	if aliased(dst, src) {
		src = append([]byte(nil), src...)
	}
	transform(dst[:len(src)], src)
	return nil
}

// UntransformInto is like Untransform but writes the result into
// dst[:len(src)]. dst may be the same slice as src (in-place
// operation); otherwise the two must not overlap.
func UntransformInto(dst, src []byte) error {
	if err := checkInto(dst, src); err != nil {
		return err
	}
	if aliased(dst, src) {
		src = append([]byte(nil), src...)
	}
	untransform(dst[:len(src)], src)
	return nil
}

func checkLen(b []byte) error {
	if len(b)%8 != 0 {
		return fmt.Errorf("%w: %d", ErrInvalidLength, len(b))
	}
	return nil
}

func checkInto(dst, src []byte) error {
	if err := checkLen(src); err != nil {
		return err
	}
	if len(dst) < len(src) {
		return fmt.Errorf("%w: %d < %d", ErrBufferTooSmall, len(dst), len(src))
	}
	return nil
}

func aliased(dst, src []byte) bool {
	return len(src) > 0 && &dst[0] == &src[0]
}

// transpose8 transposes the 8x8 bit matrix held in x, where byte k (counting
// from the most significant byte) is row k and bit 7-j of a row is column
// j. Row j of the result therefore holds bit 7-j of every input byte,
// MSB-first, which is exactly one CST channel byte. The transpose is its
// own inverse. (Hacker's Delight, transpose8rS64.)
func transpose8(x uint64) uint64 {
	t := (x ^ (x >> 7)) & 0x00AA00AA00AA00AA
	x ^= t ^ (t << 7)
	t = (x ^ (x >> 14)) & 0x0000CCCC0000CCCC
	x ^= t ^ (t << 14)
	t = (x ^ (x >> 28)) & 0x00000000F0F0F0F0
	x ^= t ^ (t << 28)
	return x
}

// transform requires len(dst) == len(src), len(src)%8 == 0, no overlap.
func transform(dst, src []byte) {
	n := len(src) / 8
	p, q, r, s := dst[0*n:1*n], dst[1*n:2*n], dst[2*n:3*n], dst[3*n:4*n]
	t, u, v, w := dst[4*n:5*n], dst[5*n:6*n], dst[6*n:7*n], dst[7*n:8*n]
	for i := range n {
		x := transpose8(binary.BigEndian.Uint64(src[i*8:]))
		p[i] = byte(x >> 56)
		q[i] = byte(x >> 48)
		r[i] = byte(x >> 40)
		s[i] = byte(x >> 32)
		t[i] = byte(x >> 24)
		u[i] = byte(x >> 16)
		v[i] = byte(x >> 8)
		w[i] = byte(x)
	}
}

// untransform requires len(dst) == len(src), len(src)%8 == 0, no overlap.
func untransform(dst, src []byte) {
	n := len(src) / 8
	p, q, r, s := src[0*n:1*n], src[1*n:2*n], src[2*n:3*n], src[3*n:4*n]
	t, u, v, w := src[4*n:5*n], src[5*n:6*n], src[6*n:7*n], src[7*n:8*n]
	for i := range n {
		x := uint64(p[i])<<56 | uint64(q[i])<<48 | uint64(r[i])<<40 | uint64(s[i])<<32 |
			uint64(t[i])<<24 | uint64(u[i])<<16 | uint64(v[i])<<8 | uint64(w[i])
		binary.BigEndian.PutUint64(dst[i*8:], transpose8(x))
	}
}
