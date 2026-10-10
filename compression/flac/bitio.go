package flac

import (
	"encoding/binary"
	"math/bits"
)

// bitReader reads MSB-first bit fields from an in-memory buffer.
//
// cache holds the next nbits bits of the stream left-aligned. Bits of cache
// beyond nbits are either zero or the true following stream bits (the fast
// refill path loads whole 64-bit words), so OR-ing new bytes in is always
// safe. pos is the index of the first byte not yet accounted for in nbits.
type bitReader struct {
	data  []byte
	pos   int
	cache uint64
	nbits uint
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

// refill tops up the cache to at least 56 bits, or as many as remain.
func (r *bitReader) refill() {
	if r.pos+8 <= len(r.data) {
		v := binary.BigEndian.Uint64(r.data[r.pos:])
		r.cache |= v >> r.nbits
		n := (63 - r.nbits) >> 3
		r.pos += int(n)
		r.nbits += n << 3
		return
	}
	for r.nbits <= 56 && r.pos < len(r.data) {
		r.cache |= uint64(r.data[r.pos]) << (56 - r.nbits)
		r.pos++
		r.nbits += 8
	}
}

// bitPos returns the number of bits consumed so far.
func (r *bitReader) bitPos() int {
	return r.pos*8 - int(r.nbits)
}

// bytePos returns the byte offset of the next unread bit's byte; only
// meaningful when the reader is byte-aligned.
func (r *bitReader) bytePos() int {
	return r.bitPos() >> 3
}

// remainingBits returns how many bits are left to read.
func (r *bitReader) remainingBits() int {
	return (len(r.data)-r.pos)*8 + int(r.nbits)
}

// alignToByte discards bits up to the next byte boundary and returns them.
func (r *bitReader) alignToByte() uint64 {
	n := r.nbits & 7
	v := r.cache >> (64 - n) // for n == 0 the shift by 64 yields 0
	r.cache <<= n
	r.nbits -= n
	return v
}

// readBits reads an n-bit unsigned value, 0 <= n <= 56.
func (r *bitReader) readBits(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if r.nbits < n {
		r.refill()
		if r.nbits < n {
			return 0, errTruncated
		}
	}
	v := r.cache >> (64 - n)
	r.cache <<= n
	r.nbits -= n
	return v, nil
}

// readSigned reads an n-bit two's complement value, 0 <= n <= 56.
func (r *bitReader) readSigned(n uint) (int64, error) {
	v, err := r.readBits(n)
	if err != nil || n == 0 {
		return 0, err
	}
	return int64(v<<(64-n)) >> (64 - n), nil
}

// readUnary counts zero bits up to and including the terminating one bit.
// It fails if the count would exceed limit.
func (r *bitReader) readUnary(limit uint64) (uint64, error) {
	var q uint64
	for {
		if r.nbits == 0 {
			r.refill()
			if r.nbits == 0 {
				return 0, errTruncated
			}
		}
		lz := uint(bits.LeadingZeros64(r.cache))
		if lz < r.nbits {
			r.cache <<= lz + 1
			r.nbits -= lz + 1
			q += uint64(lz)
			if q > limit {
				return 0, errCorrupt
			}
			return q, nil
		}
		q += uint64(r.nbits)
		if q > limit {
			return 0, errCorrupt
		}
		r.cache = 0
		r.nbits = 0
	}
}

// readRiceBlock decodes len(dst) Rice-coded, zig-zag folded residuals with
// parameter k (0 <= k <= 30).
func (r *bitReader) readRiceBlock(dst []int32, k uint) error {
	maxQ := uint64(0xFFFFFFFF) >> k
	for i := range dst {
		if r.nbits < 40 {
			r.refill()
		}
		var q uint64
		lz := uint(bits.LeadingZeros64(r.cache))
		if lz < r.nbits {
			r.cache <<= lz + 1
			r.nbits -= lz + 1
			q = uint64(lz)
		} else {
			var err error
			if q, err = r.readUnary(maxQ); err != nil {
				return err
			}
		}
		if q > maxQ {
			return errCorrupt
		}
		u := q << k
		if k > 0 {
			if r.nbits < k {
				r.refill()
				if r.nbits < k {
					return errTruncated
				}
			}
			u |= r.cache >> (64 - k)
			r.cache <<= k
			r.nbits -= k
		}
		dst[i] = int32(uint32(u>>1) ^ -uint32(u&1))
	}
	return nil
}

// bitWriter accumulates MSB-first bit fields into a byte slice.
type bitWriter struct {
	buf []byte
	acc uint64 // the low n bits are pending output
	n   uint   // always < 32 between calls
}

// writeBits appends the low n bits of v, 0 <= n <= 32.
func (w *bitWriter) writeBits(v uint64, n uint) {
	w.acc = w.acc<<n | v&(1<<n-1)
	w.n += n
	if w.n >= 32 {
		w.n -= 32
		w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(w.acc>>w.n))
	}
}

// writeSigned appends v as an n-bit two's complement value, n <= 32.
func (w *bitWriter) writeSigned(v int64, n uint) {
	w.writeBits(uint64(v), n)
}

// writeZeros appends n zero bits.
func (w *bitWriter) writeZeros(n uint) {
	for n > 32 {
		w.writeBits(0, 32)
		n -= 32
	}
	w.writeBits(0, n)
}

// writeUnary appends q zero bits followed by a one bit.
func (w *bitWriter) writeUnary(q uint) {
	w.writeZeros(q)
	w.writeBits(1, 1)
}

// writeRiceBlock Rice-codes residuals with parameter k (0 <= k <= 30).
func (w *bitWriter) writeRiceBlock(res []int32, k uint) {
	mask := uint64(1)<<k - 1
	for _, r := range res {
		u := uint64(uint32(r<<1) ^ uint32(r>>31))
		q := uint(u >> k)
		if q+1+k <= 32 {
			w.writeBits(1<<k|u&mask, q+1+k)
			continue
		}
		w.writeZeros(q)
		w.writeBits(1<<k|u&mask, k+1)
	}
}

// padToByte zero-pads to the next byte boundary.
func (w *bitWriter) padToByte() {
	if pad := w.n & 7; pad != 0 {
		w.writeBits(0, 8-pad)
	}
}

// bytes flushes all complete pending bytes and returns the output so far.
// The writer must be byte-aligned.
func (w *bitWriter) bytes() []byte {
	for w.n >= 8 {
		w.n -= 8
		w.buf = append(w.buf, byte(w.acc>>w.n))
	}
	return w.buf
}
