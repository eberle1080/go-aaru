package ngcw

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/bits"
	"testing"
)

// LFG tests. No stored output: the generator is checked against the
// structural properties of the algorithm (the lag-521/32 XOR recurrence, the
// bit pattern left by the seed expansion, determinism, seek equivalence and
// seed recovery).

// swappedTestSeed is the test seed as byte-swapped words (i.e. byte-swapped
// relative to Seed's big-endian words).
var swappedTestSeed = [SeedWords]uint32{
	0x01234567, 0x89ABCDEF, 0xFEDCBA98, 0x76543210, 0xDEADBEEF, 0xCAFEBABE, 0x12345678, 0x9ABCDEF0, 0x0FEDCBA9,
	0x87654321, 0xBEEFCAFE, 0xBABEFACE, 0x11223344, 0x55667788, 0x99AABBCC, 0xDDEEFF00, 0xA5A5A5A5,
}

// seedFromSwapped converts byte-swapped seed words to a Seed.
func seedFromSwapped(words [SeedWords]uint32) Seed {
	var s Seed
	for i, w := range words {
		s[i] = bits.ReverseBytes32(w)
	}

	return s
}

var testSeed = seedFromSwapped(swappedTestSeed)

func lfgBytes(seed Seed, n int) []byte {
	b := make([]byte, n)
	_, _ = NewLFG(seed).Read(b)

	return b
}

func TestLFGSetSeedAndGenerate(t *testing.T) {
	out := lfgBytes(testSeed, 256)
	if bytes.Equal(out, make([]byte, 256)) {
		t.Fatal("output is all zeros")
	}
}

func TestLFGDeterministic(t *testing.T) {
	if !bytes.Equal(lfgBytes(testSeed, 1024), lfgBytes(testSeed, 1024)) {
		t.Fatal("output is not deterministic")
	}
}

func TestLFGChunkedOutputMatchesBulk(t *testing.T) {
	bulk := lfgBytes(testSeed, 4096)

	g := NewLFG(testSeed)
	chunked := make([]byte, 4096)
	chunkSizes := []int{1, 7, 13, 64, 100, 521*4 - 1, 3, 2048}

	for pos, ci := 0, 0; pos < len(chunked); ci++ {
		chunk := min(chunkSizes[ci%len(chunkSizes)], len(chunked)-pos)
		_, _ = g.Read(chunked[pos : pos+chunk])
		pos += chunk
	}

	if !bytes.Equal(bulk, chunked) {
		t.Fatal("chunked output differs from bulk output")
	}
}

// streamWord returns stream word n (4 bytes at 4n, read little-endian).
func streamWord(stream []byte, n int) uint32 { return binary.LittleEndian.Uint32(stream[4*n:]) }

// The output is a lagged Fibonacci sequence over XOR with lags (521, 32):
// x[n] = x[n-521] ^ x[n-32] for every n >= 521. XOR commutes with byte
// order, so this holds for words read in either endianness.
func TestLFGRecurrence(t *testing.T) {
	for _, seed := range []Seed{testSeed, fillSeed(0x5A), seedFromSwapped([SeedWords]uint32{0: 1})} {
		stream := lfgBytes(seed, 6*LFGQuantum)

		for n := LFGWords; n < len(stream)/4; n++ {
			if streamWord(stream, n) != streamWord(stream, n-LFGWords)^streamWord(stream, n-lfgJ) {
				t.Fatalf("seed %08x: recurrence fails at word %d", seed[0], n)
			}
		}
	}
}

// Seed expansion stores 16-bit fields shifted by 18, so bits 22-23 of every
// (byte-swapped) output word repeat bits 24-25. FindSeed depends on this.
func TestLFGExpansionBitPattern(t *testing.T) {
	stream := lfgBytes(testSeed, 4*LFGQuantum)

	for n := 0; n < len(stream)/4; n++ {
		x := bits.ReverseBytes32(streamWord(stream, n))
		if x&0x00C00000 != (x>>2)&0x00C00000 {
			t.Fatalf("word %d = %08x lacks the expansion bit pattern", n, x)
		}
	}
}

// The output depends on every seed word, and only on the seed.
func TestLFGSeedSensitivity(t *testing.T) {
	base := lfgBytes(testSeed, LFGQuantum)

	if !bytes.Equal(lfgBytes(Seed{}, LFGQuantum), make([]byte, LFGQuantum)) {
		t.Error("the all-zero seed must give all-zero output (XOR recurrence)")
	}

	for i := 0; i < SeedWords; i++ {
		s := testSeed
		s[i] ^= 0x01000000

		if bytes.Equal(lfgBytes(s, LFGQuantum), base) {
			t.Errorf("flipping a bit of seed word %d did not change the output", i)
		}
	}

	// A fresh generator and a reset one agree, as do two separate instances.
	g := NewLFG(fillSeed(1))
	_, _ = g.Read(make([]byte, 777))
	g.Reset(testSeed)

	got := make([]byte, LFGQuantum)
	_, _ = g.Read(got)

	if !bytes.Equal(got, base) {
		t.Error("output depends on generator history")
	}
}

func TestLFGDiscardMatchesRead(t *testing.T) {
	stream := lfgBytes(testSeed, 5*LFGQuantum+100)

	for _, skip := range []uint64{0, 1, 3, LFGQuantum - 1, LFGQuantum, LFGQuantum + 1, 2*LFGQuantum + 17, 4 * LFGQuantum} {
		g := NewLFG(testSeed)
		g.Discard(skip)

		got := make([]byte, 100)
		_, _ = g.Read(got)

		if !bytes.Equal(got, stream[skip:skip+100]) {
			t.Errorf("Discard(%d) then Read mismatch", skip)
		}
	}

	// Discard after a partial read.
	g := NewLFG(testSeed)
	_, _ = io.ReadFull(g, make([]byte, 1000))
	g.Discard(LFGQuantum + 50)

	got := make([]byte, 64)
	_, _ = g.Read(got)

	if want := stream[1000+LFGQuantum+50:][:64]; !bytes.Equal(got, want) {
		t.Error("Discard after partial read mismatch")
	}
}

func TestLFGReset(t *testing.T) {
	g := NewLFG(testSeed)
	_, _ = g.Read(make([]byte, 5000))
	g.Reset(testSeed)

	got := make([]byte, 64)
	_, _ = g.Read(got)

	if !bytes.Equal(got, lfgBytes(testSeed, 64)) {
		t.Fatal("Reset did not rewind the stream")
	}
}

func TestLFGSeedExtraction(t *testing.T) {
	bufSize := LFGWords * 4 * 2
	buf := lfgBytes(testSeed, bufSize)

	seed, matched := FindSeed(buf, 0)
	if matched == 0 {
		t.Fatal("FindSeed matched 0 bytes")
	}

	verify := lfgBytes(seed, bufSize)
	if !bytes.Equal(buf[:matched], verify[:matched]) {
		t.Fatal("extracted seed does not reproduce the data")
	}
}

func TestLFGSeedExtractionWithOffset(t *testing.T) {
	const offset = 2048

	bufSize := LFGWords * 4 * 2

	g := NewLFG(testSeed)
	_, _ = g.Read(make([]byte, offset))

	buf := make([]byte, bufSize)
	_, _ = g.Read(buf)

	seed, matched := FindSeed(buf, offset)
	if matched == 0 {
		t.Fatal("FindSeed matched 0 bytes")
	}

	g2 := NewLFG(seed)
	_, _ = g2.Read(make([]byte, offset))

	verify := make([]byte, matched)
	_, _ = g2.Read(verify)

	if !bytes.Equal(buf[:matched], verify) {
		t.Fatal("extracted seed does not reproduce the data")
	}
}

// FindSeed recovers a seed that regenerates the whole stream, from any
// sufficiently long window at any offset. The seed expansion discards bits
// 16-17 of each seed word, so the recovered seed may differ from the original
// only there; it is canonical (recovering again returns it unchanged).
func TestFindSeedRecovers(t *testing.T) {
	const streamLen = 3*0x8000 + 123

	stream := lfgBytes(testSeed, streamLen)

	tests := []struct {
		off, length int
		matched     int
	}{
		{0, MinSeedData, MinSeedData},
		{2048, 4168, 4168},
		{6, 5000, 5000},
		{0x7C00*3 + 1024, 2171, 2171},
		{3, 4000, 4000},
		{0x8000 - 7, 3 * LFGQuantum, 3 * LFGQuantum},
		{1, MinSeedData + 3, MinSeedData + 3}, // 3 bytes to align, then exactly 521 words
		{1, MinSeedData + 2, 0},               // too short once aligned
		{0, MinSeedData - 4, 0},               // too short
	}

	var first Seed

	for _, tt := range tests {
		seed, matched := FindSeed(stream[tt.off:tt.off+tt.length], uint64(tt.off))
		if matched != tt.matched {
			t.Errorf("FindSeed(off=%d,len=%d) matched = %d, want %d", tt.off, tt.length, matched, tt.matched)

			continue
		}

		if matched == 0 {
			if seed != (Seed{}) {
				t.Errorf("FindSeed(off=%d) returned seed %08x with no match", tt.off, seed)
			}

			continue
		}

		if first == (Seed{}) {
			first = seed
		} else if seed != first {
			t.Errorf("FindSeed(off=%d) seed differs between windows", tt.off)
		}

		if !bytes.Equal(lfgBytes(seed, streamLen), stream) {
			t.Errorf("FindSeed(off=%d) seed does not regenerate the whole stream", tt.off)
		}

		for i := range seed {
			if (seed[i]^testSeed[i])&^0x00030000 != 0 {
				t.Errorf("seed word %d = %08x, original %08x: differs outside the discarded bits", i, seed[i], testSeed[i])
			}
		}
	}

	if again, _ := FindSeed(lfgBytes(first, MinSeedData), 0); again != first {
		t.Errorf("recovered seed is not canonical: %08x then %08x", first, again)
	}

	// A corrupted byte stops the match at the corruption.
	corrupt := bytes.Clone(stream[:6000])
	corrupt[3000] ^= 1

	if _, matched := FindSeed(corrupt, 0); matched != 3000 {
		t.Errorf("corrupted FindSeed matched = %d, want 3000", matched)
	}
}

// Independent 0x8000 blocks, each with its own seed, are recovered
// independently.
func TestFindSeedPerBlock(t *testing.T) {
	seeds := []Seed{testSeed, fillSeed(0x3C), seedFromSwapped([SeedWords]uint32{5: 0xDEADBEEF, 16: 7})}

	disc := make([]byte, 0, len(seeds)*0x8000)
	for _, s := range seeds {
		disc = append(disc, lfgBytes(s, 0x8000)...)
	}

	for i := range seeds {
		block := disc[i*0x8000 : (i+1)*0x8000]

		seed, matched := FindSeed(block[0x1000:], 0x1000)
		if matched != 0x8000-0x1000 {
			t.Fatalf("block %d: matched %d", i, matched)
		}

		if !bytes.Equal(lfgBytes(seed, 0x8000), block) {
			t.Fatalf("block %d: recovered seed does not regenerate the block", i)
		}
	}

	// A window spanning two blocks matches only up to the block boundary.
	if _, matched := FindSeed(disc[0x8000-LFGQuantum-100:0x8000+500], 0x8000-LFGQuantum-100); matched != LFGQuantum+100 {
		t.Errorf("cross-block window matched %d, want %d", matched, LFGQuantum+100)
	}
}

func TestFindSeedRejectsNonJunk(t *testing.T) {
	// All-zero data is (degenerate) LFG output of the all-zero seed.
	if seed, matched := FindSeed(make([]byte, 0x8000), 0); matched != 0x8000 || seed != (Seed{}) {
		t.Fatalf("zero data: matched = %d, seed = %08x", matched, seed)
	}

	ascending := make([]byte, 0x8000)
	for i := range ascending {
		ascending[i] = byte(i * 31)
	}

	if _, matched := FindSeed(ascending, 0); matched != 0 {
		t.Fatalf("non-junk data matched %d bytes", matched)
	}

	if _, matched := FindSeed(nil, 0); matched != 0 {
		t.Fatal("nil data matched")
	}

	if _, matched := FindSeed([]byte{1, 2}, 1); matched != 0 {
		t.Fatal("tiny data matched")
	}
}

func FuzzFindSeed(f *testing.F) {
	stream := lfgBytes(testSeed, 3*LFGQuantum)
	f.Add(stream, uint16(0))
	f.Add(stream[5:], uint16(5))
	f.Add(make([]byte, 3000), uint16(7))

	f.Fuzz(func(t *testing.T, data []byte, off uint16) {
		seed, matched := FindSeed(data, uint64(off))
		if matched < 0 || matched > len(data) {
			t.Fatalf("matched = %d of %d", matched, len(data))
		}

		if matched == 0 {
			return
		}

		g := NewLFG(seed)
		g.Discard(uint64(off))

		got := make([]byte, matched)
		_, _ = g.Read(got)

		if !bytes.Equal(got, data[:matched]) {
			t.Fatal("recovered seed does not reproduce the matched bytes")
		}
	})
}
