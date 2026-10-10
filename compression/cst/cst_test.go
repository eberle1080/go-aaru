package cst

import (
	"bytes"
	"errors"
	"math/bits"
	"math/rand/v2"
	"testing"
)

// testData returns n deterministic pseudo-random bytes.
func testData(n int) []byte {
	rng := rand.New(rand.NewPCG(0x637374, 0x74657374))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// naiveTransform is a literal transcription of the specification
// (appendix "CD Subchannel Transform"), used to cross-check the
// bit-matrix implementation.
func naiveTransform(in []byte) []byte {
	n := len(in)
	out := make([]byte, n)
	for c := range 8 { // P..W
		b := 7 - c
		for i := 0; i < n; i += 8 {
			var v byte
			for k := range 8 {
				v |= ((in[i+k] >> b) & 1) << (7 - k)
			}
			out[c*n/8+i/8] = v
		}
	}
	return out
}

func naiveUntransform(in []byte) []byte {
	n := len(in)
	out := make([]byte, n)
	for i := 0; i < n; i += 8 {
		for k := range 8 {
			for c := range 8 {
				b := 7 - c
				bit := (in[c*n/8+i/8] >> (7 - k)) & 1
				out[i+k] |= bit << b
			}
		}
	}
	return out
}

func TestWorkedExample(t *testing.T) {
	// Worked example from the CST appendix of the specification.
	in := []byte{0xC0, 0x80, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	want := []byte{0xC0, 0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	got, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Transform = %x, want %x", got, want)
	}
	back, err := Untransform(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, in) {
		t.Fatalf("Untransform = %x, want %x", back, in)
	}
}

func TestSingleBits(t *testing.T) {
	// Every single set bit in a 16-byte block must land in the right place.
	for pos := range 16 * 8 {
		in := make([]byte, 16)
		in[pos/8] = 0x80 >> (pos % 8)
		got, _ := Transform(in)
		if want := naiveTransform(in); !bytes.Equal(got, want) {
			t.Fatalf("bit %d: Transform = %x, want %x", pos, got, want)
		}
		got, _ = Untransform(in)
		if want := naiveUntransform(in); !bytes.Equal(got, want) {
			t.Fatalf("bit %d: Untransform = %x, want %x", pos, got, want)
		}
	}
}

// A buffer whose bytes all carry only channel c (bit 7-c) transforms into
// all-0xFF in the c-th eighth of the output and zero elsewhere.
func TestChannelSegments(t *testing.T) {
	const n = 96 * 3
	for c := range 8 {
		in := bytes.Repeat([]byte{0x80 >> c}, n)
		got, err := Transform(in)
		if err != nil {
			t.Fatal(err)
		}
		for seg := range 8 {
			want := byte(0)
			if seg == c {
				want = 0xFF
			}
			if !bytes.Equal(got[seg*n/8:(seg+1)*n/8], bytes.Repeat([]byte{want}, n/8)) {
				t.Fatalf("channel %d: segment %d = %x", c, seg, got[seg*n/8:(seg+1)*n/8])
			}
		}
	}
}

// Output byte i of channel c holds bit 7-c of input bytes 8i..8i+7,
// MSB first, and the transform is a bit permutation (it preserves the
// number of set bits).
func TestChannelBytesDefinition(t *testing.T) {
	in := testData(96 * 50)
	got, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	n := len(in)
	for c := range 8 {
		for i := range n / 8 {
			var want byte
			for k := range 8 {
				if in[8*i+k]&(0x80>>c) != 0 {
					want |= 0x80 >> k
				}
			}
			if g := got[c*n/8+i]; g != want {
				t.Fatalf("channel %d byte %d = %#x, want %#x", c, i, g, want)
			}
		}
	}
	if popcount(got) != popcount(in) {
		t.Fatal("transform changed the number of set bits")
	}
}

func popcount(b []byte) int {
	n := 0
	for _, v := range b {
		n += bits.OnesCount8(v)
	}
	return n
}

// The transform works per 8-byte group: changing one input byte changes
// only bits belonging to that group (at most one byte per channel).
func TestLocality(t *testing.T) {
	in := testData(96 * 4)
	base, _ := Transform(in)
	n := len(in)
	for _, pos := range []int{0, 7, 8, 100, n - 1} {
		mod := bytes.Clone(in)
		mod[pos] ^= 0xFF
		got, _ := Transform(mod)
		for j := range got {
			if got[j] != base[j] && j%(n/8) != pos/8 {
				t.Fatalf("flipping input byte %d changed output byte %d", pos, j)
			}
		}
	}
}

func TestMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 8, 16, 96, 96 * 7, 4096} {
		in := make([]byte, n)
		for i := range in {
			in[i] = byte(rng.Uint32())
		}
		got, err := Transform(in)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, naiveTransform(in)) {
			t.Fatalf("n=%d: Transform differs from spec", n)
		}
		got, err = Untransform(in)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, naiveUntransform(in)) {
			t.Fatalf("n=%d: Untransform differs from spec", n)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, n := range []int{0, 8, 96, 96 * 75, 96 * 1000} {
		in := make([]byte, n)
		for i := range in {
			in[i] = byte(rng.Uint32())
		}
		seq, err := Transform(in)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Untransform(seq)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(back, in) {
			t.Fatalf("n=%d: round trip mismatch", n)
		}
		// And the other direction.
		il, _ := Untransform(in)
		back, _ = Transform(il)
		if !bytes.Equal(back, in) {
			t.Fatalf("n=%d: reverse round trip mismatch", n)
		}
	}
}

func TestEmpty(t *testing.T) {
	for _, f := range []func([]byte) ([]byte, error){Transform, Untransform} {
		got, err := f(nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %x, %v; want empty, nil", got, err)
		}
	}
}

func TestInvalidLength(t *testing.T) {
	for _, n := range []int{1, 7, 9, 95, 97} {
		in := make([]byte, n)
		if _, err := Transform(in); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("Transform(len %d) error = %v, want ErrInvalidLength", n, err)
		}
		if _, err := Untransform(in); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("Untransform(len %d) error = %v, want ErrInvalidLength", n, err)
		}
		if err := TransformInto(make([]byte, 128), in); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("TransformInto(len %d) error = %v, want ErrInvalidLength", n, err)
		}
		if err := UntransformInto(make([]byte, 128), in); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("UntransformInto(len %d) error = %v, want ErrInvalidLength", n, err)
		}
	}
}

func TestInto(t *testing.T) {
	in := testData(96 * 10)
	want, _ := Transform(in)

	// Larger destination: only the first len(src) bytes are written.
	dst := bytes.Repeat([]byte{0xEE}, len(in)+8)
	if err := TransformInto(dst, in); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst[:len(in)], want) || !bytes.Equal(dst[len(in):], bytes.Repeat([]byte{0xEE}, 8)) {
		t.Fatal("TransformInto wrote wrong data")
	}
	back := make([]byte, len(in))
	if err := UntransformInto(back, want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, in) {
		t.Fatal("UntransformInto round trip mismatch")
	}

	if err := TransformInto(make([]byte, len(in)-8), in); !errors.Is(err, ErrBufferTooSmall) {
		t.Errorf("TransformInto short dst error = %v, want ErrBufferTooSmall", err)
	}
	if err := UntransformInto(nil, in); !errors.Is(err, ErrBufferTooSmall) {
		t.Errorf("UntransformInto nil dst error = %v, want ErrBufferTooSmall", err)
	}
}

func TestInPlace(t *testing.T) {
	// Identical source and destination slices must work.
	in := testData(96 * 4)
	want, _ := Transform(in)

	buf := append([]byte(nil), in...)
	if err := TransformInto(buf, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, want) {
		t.Fatal("in-place TransformInto mismatch")
	}
	if err := UntransformInto(buf, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, in) {
		t.Fatal("in-place UntransformInto mismatch")
	}
}

func TestInputNotModified(t *testing.T) {
	in := testData(96)
	orig := append([]byte(nil), in...)
	_, _ = Transform(in)
	_, _ = Untransform(in)
	if !bytes.Equal(in, orig) {
		t.Fatal("input modified")
	}
}

func BenchmarkTransform(b *testing.B) {
	in := make([]byte, 96*4096)
	b.SetBytes(int64(len(in)))
	for b.Loop() {
		_, _ = Transform(in)
	}
}

func BenchmarkUntransform(b *testing.B) {
	in := make([]byte, 96*4096)
	b.SetBytes(int64(len(in)))
	for b.Loop() {
		_, _ = Untransform(in)
	}
}
