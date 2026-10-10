// Package ngcw implements the Nintendo GameCube / Wii junk-fill machinery used
// by AaruFormat: the Lagged Fibonacci Generator (LFG) that produces the
// pseudorandom padding on GameCube and Wii discs, seed recovery from junk data,
// the junk map media tag and junk sector regeneration.
//
// See the GameCube/Wii junk appendix of the AaruFormat specification.
package ngcw

import (
	"encoding/binary"
	"math/bits"
)

const (
	// LFGWords is the number of 32-bit words in the LFG state (K).
	LFGWords = 521
	// lfgJ is the LFG second tap.
	lfgJ = 32
	// SeedWords is the number of 32-bit words in an LFG seed.
	SeedWords = 17
	// LFGQuantum is the number of bytes produced per LFG state (LFGWords*4).
	LFGQuantum = LFGWords * 4
	// MinSeedData is the minimum number of whole, aligned bytes of junk
	// FindSeed needs to recover a seed.
	MinSeedData = LFGQuantum
)

// Seed is a 17-word LFG seed. Each word holds the value obtained by decoding
// the corresponding 4 bytes of the on-disc / media tag representation as a
// big-endian uint32.
type Seed [SeedWords]uint32

// putBytes writes the 68-byte big-endian representation of s to b.
func (s *Seed) putBytes(b []byte) {
	for i, w := range s {
		binary.BigEndian.PutUint32(b[i*4:], w)
	}
}

// seedFromBytes decodes a 68-byte big-endian seed.
func seedFromBytes(b []byte) Seed {
	var s Seed
	for i := range s {
		s[i] = binary.BigEndian.Uint32(b[i*4:])
	}

	return s
}

// LFG is the Lagged Fibonacci Generator used for GameCube/Wii junk data. The
// zero value is not seeded; use NewLFG or Reset. An LFG implements io.Reader
// (reads never fail).
type LFG struct {
	buf [LFGWords]uint32 // state, byte-swapped relative to the big-endian stream words
	out [LFGQuantum]byte // byte view of buf, refreshed whenever buf changes
	pos int              // byte position within out
}

// NewLFG returns a generator initialized from seed and positioned at stream
// offset 0.
func NewLFG(seed Seed) *LFG {
	g := new(LFG)
	g.Reset(seed)

	return g
}

// Reset re-seeds the generator and rewinds it to stream offset 0.
func (g *LFG) Reset(seed Seed) {
	g.pos = 0
	copy(g.buf[:SeedWords], seed[:])
	g.initialize(false)
	g.sync()
}

// Read fills p with the next len(p) bytes of the stream. It always returns
// len(p), nil.
func (g *LFG) Read(p []byte) (int, error) {
	n := len(p)

	for len(p) > 0 {
		c := copy(p, g.out[g.pos:])
		g.pos += c
		p = p[c:]

		if g.pos == LFGQuantum {
			g.forward()
			g.sync()
			g.pos = 0
		}
	}

	return n, nil
}

// Discard advances the stream by n bytes without producing output. It is
// equivalent to reading and dropping n bytes.
func (g *LFG) Discard(n uint64) {
	total := uint64(g.pos) + n
	steps := total / LFGQuantum

	for i := uint64(0); i < steps; i++ {
		g.forward()
	}

	if steps > 0 {
		g.sync()
	}

	g.pos = int(total % LFGQuantum)
}

// sync refreshes the byte view of the state (little-endian word layout).
func (g *LFG) sync() {
	for i, w := range g.buf {
		binary.LittleEndian.PutUint32(g.out[i*4:], w)
	}
}

func (g *LFG) forward() {
	b := &g.buf

	for i := 0; i < lfgJ; i++ {
		b[i] ^= b[i+LFGWords-lfgJ]
	}

	for i := lfgJ; i < LFGWords; i++ {
		b[i] ^= b[i-lfgJ]
	}
}

func (g *LFG) backward(startWord, endWord int) {
	b := &g.buf

	loopEnd := max(lfgJ, startWord)
	upper := min(endWord, LFGWords)

	for i := upper; i > loopEnd; i-- {
		b[i-1] ^= b[i-1-lfgJ]
	}

	upper2 := min(endWord, lfgJ)

	for i := upper2; i > startWord; i-- {
		b[i-1] ^= b[i-1+LFGWords-lfgJ]
	}
}

// initialize expands the seed in buf[0:17] into the full state. With
// checkExisting it instead verifies that buf already satisfies the
// recurrence, returning false if it does not.
func (g *LFG) initialize(checkExisting bool) bool {
	b := &g.buf

	for i := SeedWords; i < LFGWords; i++ {
		calculated := (b[i-17] << 23) ^ (b[i-16] >> 9) ^ b[i-1]

		if checkExisting {
			actual := (b[i] & 0xFF00FFFF) | ((b[i] << 2) & 0x00FC0000)
			if (calculated & 0xFFFCFFFF) != actual {
				return false
			}
		}

		b[i] = calculated
	}

	// Shift-by-18-instead-of-16 quirk, then byte swap.
	for i := range b {
		b[i] = bits.ReverseBytes32((b[i] & 0xFF00FFFF) | ((b[i] >> 2) & 0x00FF0000))
	}

	for range 4 {
		g.forward()
	}

	return true
}

// reinitialize recovers the seed from a state rewound to stream offset 0 and
// re-expands it, verifying consistency.
func (g *LFG) reinitialize() (Seed, bool) {
	b := &g.buf

	for range 4 {
		g.backward(0, LFGWords)
	}

	for i := range b {
		b[i] = bits.ReverseBytes32(b[i])
	}

	// Reconstruct bits lost by the shift quirk.
	for i := 0; i < SeedWords; i++ {
		b[i] = (b[i] & 0xFF00FFFF) | ((b[i] << 2) & 0x00FC0000) | (((b[i+16] ^ b[i+15]) << 9) & 0x00030000)
	}

	var seed Seed
	copy(seed[:], b[:SeedWords])

	return seed, g.initialize(true)
}

// FindSeed tries to recover the LFG seed that produced data, where data[0] is
// at byte offset dataOffset of the LFG stream (0x8000-aligned block relative
// for Wii, arbitrary for GameCube). At least MinSeedData whole 32-bit words
// (aligned to the stream) of junk are required.
//
// It returns the seed and the number of consecutive bytes from data[0] that
// the seed reproduces; matched is 0 (and seed is zero) if data does not look
// like LFG output.
func FindSeed(data []byte, dataOffset uint64) (seed Seed, matched int) {
	// Work on whole words aligned to the stream.
	skip := ((dataOffset + 3) &^ 3) - dataOffset
	if skip > uint64(len(data)) {
		return Seed{}, 0
	}

	words := data[skip:]
	if len(words)/4 < LFGWords {
		return Seed{}, 0
	}

	word := func(i int) uint32 { return binary.LittleEndian.Uint32(words[i*4:]) }
	wordOffset := (dataOffset + skip) / 4

	// Quick sanity check: the shift quirk leaves a fixed pattern in bits 22-23.
	for i := 0; i < LFGWords; i++ {
		x := bits.ReverseBytes32(word(i))
		if (x & 0x00C00000) != ((x >> 2) & 0x00C00000) {
			return Seed{}, 0
		}
	}

	var g LFG

	modK := int(wordOffset % LFGWords)
	divK := wordOffset / LFGWords
	firstPart := LFGWords - modK

	for i := 0; i < firstPart; i++ {
		g.buf[modK+i] = word(i)
	}

	for i := 0; i < modK; i++ {
		g.buf[i] = word(firstPart + i)
	}

	g.backward(0, modK)

	for i := uint64(0); i < divK; i++ {
		g.backward(0, LFGWords)
	}

	seed, ok := g.reinitialize()
	if !ok {
		return Seed{}, 0
	}

	for i := uint64(0); i < divK; i++ {
		g.forward()
	}

	g.sync()
	g.pos = int(dataOffset % LFGQuantum)

	for _, b := range data {
		if b != g.out[g.pos] {
			break
		}

		matched++
		g.pos++

		if g.pos == LFGQuantum {
			g.forward()
			g.sync()
			g.pos = 0
		}
	}

	if matched == 0 {
		return Seed{}, 0
	}

	return seed, matched
}
