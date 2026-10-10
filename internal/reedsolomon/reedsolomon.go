// Package reedsolomon implements the systematic Reed-Solomon erasure code over
// GF(2^8) used by AaruFormat for parity blocks.
//
// The coding matrix is (K+M) x K. Its top K rows are the identity (data shards
// pass through unchanged) and its bottom M rows are the generator matrix used
// to compute parity. It is derived from the (K+M) x K Vandermonde matrix
// V[i][j] = i^j by multiplying V by the inverse of its top K x K submatrix.
// Parity is persisted on disk, so the generator matrix must not change; it
// is fully determined by this construction.
//
// Note that, contrary to the specification appendix, the generator
// coefficients for M = 1 are NOT all 1 (e.g. for K=4 they are 1b 1c 12 14):
// M = 1 uses the same construction as every other M.
package reedsolomon

import (
	"errors"
	"fmt"

	"github.com/eberle1080/go-aaru/internal/gf256"
)

// MaxShards is the maximum total number of shards (K+M) supported over GF(2^8).
const MaxShards = 255

var (
	// ErrInvalidShardCounts is returned by New when K or M is zero or K+M > 255.
	ErrInvalidShardCounts = errors.New("reedsolomon: invalid shard counts")
	// ErrTooManyErasures is returned by Decode when more than M shards are
	// missing.
	ErrTooManyErasures = errors.New("reedsolomon: too many erasures")
	// ErrSingularMatrix is returned when a matrix that should be invertible is
	// not. With a valid coding matrix this cannot happen.
	ErrSingularMatrix = errors.New("reedsolomon: singular matrix")
	// ErrShardCount is returned when the number of shards or presence flags
	// passed to Decode/Encode does not match the codec.
	ErrShardCount = errors.New("reedsolomon: wrong number of shards")
	// ErrShardSize is returned when shards passed to Decode/Encode differ in size.
	ErrShardSize = errors.New("reedsolomon: shard size mismatch")
)

// Codec is an RS(K, M) codec. It is immutable after creation and safe for
// concurrent use.
type Codec struct {
	k, m   int
	coding []byte // (K+M) x K, row-major; rows K.. form the generator matrix
}

// New creates a codec for k data shards and m parity shards.
// It requires k >= 1, m >= 1 and k+m <= 255.
func New(k, m int) (*Codec, error) {
	if k <= 0 || m <= 0 || k+m > MaxShards {
		return nil, fmt.Errorf("%w: K=%d M=%d", ErrInvalidShardCounts, k, m)
	}

	n := k + m
	v := vandermonde(n, k)

	topInv := make([]byte, k*k)
	if err := invert(v[:k*k], topInv, k); err != nil {
		return nil, err
	}

	coding := make([]byte, n*k)
	for i := range n {
		row := v[i*k : (i+1)*k]
		for j := range k {
			var val byte
			for x := range k {
				val ^= gf256.Mul(row[x], topInv[x*k+j])
			}
			coding[i*k+j] = val
		}
	}

	return &Codec{k: k, m: m, coding: coding}, nil
}

// DataShards returns K.
func (c *Codec) DataShards() int { return c.k }

// ParityShards returns M.
func (c *Codec) ParityShards() int { return c.m }

// TotalShards returns K+M.
func (c *Codec) TotalShards() int { return c.k + c.m }

// Coefficient returns the generator matrix coefficient for parity shard m and
// data shard k. It panics if m or k is out of range, like a slice index.
func (c *Codec) Coefficient(m, k int) byte {
	if m < 0 || m >= c.m || k < 0 || k >= c.k {
		panic(fmt.Sprintf("reedsolomon: coefficient index (%d,%d) out of range for K=%d M=%d", m, k, c.k, c.m))
	}
	return c.coding[(c.k+m)*c.k+k]
}

// EncodeIncremental accumulates one data shard's contribution into one parity
// shard: parity[i] ^= coeff*data[i] The parity
// accumulator must start zeroed. It processes min(len(data), len(parity))
// bytes, so a data shard shorter than the parity buffer behaves as if it were
// zero-padded.
func EncodeIncremental(coeff byte, data, parity []byte) {
	gf256.MulRegion(parity, data, coeff)
}

// Encode computes all M parity shards from K data shards. parity[m] is
// overwritten. Data shards may be shorter than the parity shards (they are
// treated as zero-padded); all parity shards must have the same length.
func (c *Codec) Encode(data, parity [][]byte) error {
	if len(data) != c.k || len(parity) != c.m {
		return fmt.Errorf("%w: got %d data/%d parity, want %d/%d", ErrShardCount, len(data), len(parity), c.k, c.m)
	}
	size := len(parity[0])
	for _, p := range parity {
		if len(p) != size {
			return ErrShardSize
		}
	}
	for _, d := range data {
		if len(d) > size {
			return ErrShardSize
		}
	}
	for m, p := range parity {
		clear(p)
		for k, d := range data {
			EncodeIncremental(c.Coefficient(m, k), d, p)
		}
	}
	return nil
}

// Decode reconstructs erased shards in place.
//
// shards holds K+M shards (data first, then parity) and present reports which
// of them are valid. Every present shard must have the same length. Erased
// shards are overwritten with the reconstructed contents; an erased entry
// whose capacity is too small (e.g. nil) is replaced by a newly allocated
// slice. If nothing is erased Decode is a no-op. If more than M shards are
// erased it returns ErrTooManyErasures without modifying anything.
func (c *Codec) Decode(shards [][]byte, present []bool) error {
	k, n := c.k, c.k+c.m
	if len(shards) != n || len(present) != n {
		return fmt.Errorf("%w: got %d shards/%d flags, want %d", ErrShardCount, len(shards), len(present), n)
	}

	erased := 0
	size := -1
	for i, ok := range present {
		if !ok {
			erased++
			continue
		}
		if size < 0 {
			size = len(shards[i])
		} else if len(shards[i]) != size {
			return ErrShardSize
		}
	}
	if erased == 0 {
		return nil
	}
	if erased > c.m {
		return ErrTooManyErasures
	}

	// The first K surviving shards (in index order) form a K x K system.
	surviving := make([]int, 0, k)
	for i := 0; i < n && len(surviving) < k; i++ {
		if present[i] {
			surviving = append(surviving, i)
		}
	}
	if len(surviving) < k {
		return ErrTooManyErasures
	}

	sub := make([]byte, k*k)
	for i, s := range surviving {
		copy(sub[i*k:(i+1)*k], c.coding[s*k:(s+1)*k])
	}
	subInv := make([]byte, k*k)
	if err := invert(sub, subInv, k); err != nil {
		return err
	}

	// shard[e] = sum_x (sum_j coding[e][j] * subInv[j][x]) * surviving[x]
	for e := range n {
		if present[e] {
			continue
		}
		if cap(shards[e]) >= size {
			shards[e] = shards[e][:size]
			clear(shards[e])
		} else {
			shards[e] = make([]byte, size)
		}

		row := c.coding[e*k : (e+1)*k]
		for x := range k {
			var coeff byte
			for j := range k {
				coeff ^= gf256.Mul(row[j], subInv[j*k+x])
			}
			if coeff != 0 {
				gf256.MulRegion(shards[e], shards[surviving[x]], coeff)
			}
		}
	}

	return nil
}

// vandermonde builds the rows x cols matrix V[i][j] = i^j over GF(2^8).
func vandermonde(rows, cols int) []byte {
	v := make([]byte, rows*cols)
	for i := range rows {
		val := byte(1)
		for j := range cols {
			v[i*cols+j] = val
			val = gf256.Mul(val, byte(i))
		}
	}
	return v
}

// invert computes the inverse of the k x k matrix mat into inv using
// Gauss-Jordan elimination over GF(2^8). mat is not modified.
func invert(mat, inv []byte, k int) error {
	work := make([]byte, k*k)
	copy(work, mat)

	clear(inv)
	for i := range k {
		inv[i*k+i] = 1
	}

	for col := range k {
		pivot := col
		for pivot < k && work[pivot*k+col] == 0 {
			pivot++
		}
		if pivot == k {
			return ErrSingularMatrix
		}

		if pivot != col {
			for j := range k {
				work[col*k+j], work[pivot*k+j] = work[pivot*k+j], work[col*k+j]
				inv[col*k+j], inv[pivot*k+j] = inv[pivot*k+j], inv[col*k+j]
			}
		}

		if diag := work[col*k+col]; diag != 1 {
			invDiag := gf256.Inv(diag)
			for j := range k {
				work[col*k+j] = gf256.Mul(work[col*k+j], invDiag)
				inv[col*k+j] = gf256.Mul(inv[col*k+j], invDiag)
			}
		}

		for row := range k {
			if row == col {
				continue
			}
			factor := work[row*k+col]
			if factor == 0 {
				continue
			}
			for j := range k {
				work[row*k+j] ^= gf256.Mul(factor, work[col*k+j])
				inv[row*k+j] ^= gf256.Mul(factor, inv[col*k+j])
			}
		}
	}

	return nil
}
