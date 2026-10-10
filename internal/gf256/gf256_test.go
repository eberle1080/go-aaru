package gf256

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

// randomBytes returns n deterministic pseudo-random bytes for seed.
func randomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 0x6766323536))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// slowMul is an independent carry-less multiply followed by reduction.
func slowMul(a, b byte) byte {
	var p uint16
	x, y := uint16(a), uint16(b)
	for y != 0 {
		if y&1 != 0 {
			p ^= x
		}
		x <<= 1
		if x&0x100 != 0 {
			x ^= Polynomial
		}
		y >>= 1
	}
	return byte(p)
}

func TestMulIdentity(t *testing.T) {
	for a := range 256 {
		if got := Mul(byte(a), 1); got != byte(a) {
			t.Errorf("Mul(%d,1) = %d", a, got)
		}
		if got := Mul(1, byte(a)); got != byte(a) {
			t.Errorf("Mul(1,%d) = %d", a, got)
		}
	}
}

func TestMulZero(t *testing.T) {
	for a := range 256 {
		if Mul(byte(a), 0) != 0 || Mul(0, byte(a)) != 0 {
			t.Errorf("Mul with zero for a=%d is not zero", a)
		}
	}
}

func TestMulCommutative(t *testing.T) {
	for a := 0; a < 256; a += 7 {
		for b := 0; b < 256; b += 11 {
			if Mul(byte(a), byte(b)) != Mul(byte(b), byte(a)) {
				t.Errorf("Mul not commutative for %d,%d", a, b)
			}
		}
	}
}

func TestMulInverse(t *testing.T) {
	for a := 1; a < 256; a++ {
		if got := Mul(byte(a), Inv(byte(a))); got != 1 {
			t.Errorf("a*inv(a) = %d for a=%d", got, a)
		}
	}
}

func TestDivRoundTrip(t *testing.T) {
	for a := 1; a < 256; a += 3 {
		for b := 1; b < 256; b += 5 {
			if q := Div(Mul(byte(a), byte(b)), byte(b)); q != byte(a) {
				t.Errorf("a=%d b=%d: got %d", a, b, q)
			}
		}
	}
}

func TestMulMatchesSlowMul(t *testing.T) {
	for a := range 256 {
		for b := range 256 {
			if got, want := Mul(byte(a), byte(b)), slowMul(byte(a), byte(b)); got != want {
				t.Fatalf("Mul(%d,%d) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestMulAssociativeDistributive(t *testing.T) {
	for a := range 256 {
		for b := range 256 {
			for _, c := range []byte{0, 1, 2, 3, 0x1D, 0x53, 0x8E, 0xCA, 0xFF} {
				x, y := byte(a), byte(b)
				if Mul(Mul(x, y), c) != Mul(x, Mul(y, c)) {
					t.Fatalf("Mul not associative for %d,%d,%d", a, b, c)
				}
				if Mul(x^y, c) != Mul(x, c)^Mul(y, c) {
					t.Fatalf("Mul not distributive for %d,%d,%d", a, b, c)
				}
			}
		}
	}
}

// 2 generates the multiplicative group: its powers 2^0..2^254 enumerate
// every non-zero element exactly once and 2^255 == 1.
func TestGeneratorOrder(t *testing.T) {
	seen := map[byte]bool{}
	x := byte(1)
	for i := range 255 {
		if seen[x] {
			t.Fatalf("2^%d = %d repeats", i, x)
		}
		seen[x] = true
		if expTable[i] != x || expTable[i+255] != x {
			t.Fatalf("expTable[%d] = %d, want %d", i, expTable[i], x)
		}
		if logTable[x] != byte(i) {
			t.Fatalf("logTable[%d] = %d, want %d", x, logTable[x], i)
		}
		x = Mul(x, 2)
	}
	if x != 1 || seen[0] || len(seen) != 255 {
		t.Fatalf("2^255 = %d, %d distinct powers", x, len(seen))
	}
}

// Div and Inv agree with Mul for every non-zero divisor, and follow their
// documented behavior for zero.
func TestDivInvDefinition(t *testing.T) {
	for a := range 256 {
		for b := 1; b < 256; b++ {
			q := Div(byte(a), byte(b))
			if Mul(q, byte(b)) != byte(a) {
				t.Fatalf("Div(%d,%d)*%d != %d", a, b, b, a)
			}
			if q != Mul(byte(a), Inv(byte(b))) {
				t.Fatalf("Div(%d,%d) != %d*Inv(%d)", a, b, a, b)
			}
		}
		if got := Div(byte(a), 0); got != byte(a) {
			t.Errorf("Div(%d, 0) = %d, want %d", a, got, a)
		}
	}
	for a := 1; a < 256; a++ {
		if Inv(Inv(byte(a))) != byte(a) {
			t.Errorf("Inv(Inv(%d)) != %d", a, a)
		}
	}
	if Inv(0) != 1 {
		t.Errorf("Inv(0) = %d, want 1", Inv(0))
	}
	if Inv(1) != 1 || Mul(2, Inv(2)) != 1 {
		t.Error("Inv(1) or Inv(2) wrong")
	}
}

// Accumulating every coefficient into one region, with shifted sources so
// both the 8-byte loop and the tail see varied alignments, equals the
// scalar computation.
func TestMulRegionAccumulateAllCoefficients(t *testing.T) {
	src := randomBytes(0x6F256, 1031)
	dst := randomBytes(0x6F257, 1031)
	want := bytes.Clone(dst)
	for c := range 256 {
		s := src[c%7 : c%7+len(src)-7]
		MulRegion(dst, s, byte(c))
		for i := range s {
			want[i] ^= slowMul(s[i], byte(c))
		}
	}
	if !bytes.Equal(dst, want) {
		t.Fatal("region accumulation differs from scalar computation")
	}
}

const regionSize = 4096

func TestXorRegion(t *testing.T) {
	src := randomBytes(42, regionSize)
	dst := make([]byte, regionSize)
	XorRegion(dst, src)
	if !bytes.Equal(dst, src) {
		t.Fatal("dst != src after XOR into zero")
	}
	XorRegion(dst, src)
	if !bytes.Equal(dst, make([]byte, regionSize)) {
		t.Fatal("double XOR did not yield zeros")
	}
}

func TestMulRegionCoeff1IsXor(t *testing.T) {
	src := randomBytes(42, regionSize)
	dst := make([]byte, regionSize)
	MulRegion(dst, src, 1)
	if !bytes.Equal(dst, src) {
		t.Fatal("coefficient 1 is not XOR")
	}
}

func TestMulRegionCoeff0IsNoop(t *testing.T) {
	src := randomBytes(42, regionSize)
	dst := bytes.Repeat([]byte{0xAA}, regionSize)
	ref := bytes.Clone(dst)
	MulRegion(dst, src, 0)
	if !bytes.Equal(dst, ref) {
		t.Fatal("coefficient 0 modified dst")
	}
}

func TestMulRegionMatchesScalar(t *testing.T) {
	src := randomBytes(42, regionSize)
	for coeff := 2; coeff < 256; coeff += 37 {
		dst := make([]byte, regionSize)
		MulRegion(dst, src, byte(coeff))
		for i := range src {
			if want := Mul(src[i], byte(coeff)); dst[i] != want {
				t.Fatalf("coeff=%d i=%d: got %d want %d", coeff, i, dst[i], want)
			}
		}
	}
}

func TestMulRegionAccumulates(t *testing.T) {
	src := randomBytes(42, regionSize)
	dst := bytes.Repeat([]byte{0x55}, regionSize)
	ref := bytes.Clone(dst)
	const coeff = 0x37
	MulRegion(dst, src, coeff)
	for i := range src {
		if want := ref[i] ^ Mul(src[i], coeff); dst[i] != want {
			t.Fatalf("i=%d: got %d want %d", i, dst[i], want)
		}
	}
}

func TestMulRegionOddSizes(t *testing.T) {
	src := randomBytes(42, regionSize)
	const coeff = 0xAB
	for _, sz := range []int{1, 15, 17, 31, 33, 63, 65, 100, 255} {
		dst := make([]byte, sz)
		MulRegion(dst, src[:sz], coeff)
		for i := range sz {
			if want := Mul(src[i], coeff); dst[i] != want {
				t.Fatalf("sz=%d i=%d: got %d want %d", sz, i, dst[i], want)
			}
		}
	}
}

func TestRegionLengthMismatchUsesMinimum(t *testing.T) {
	src := randomBytes(7, 100)
	for _, c := range []byte{1, 0x8E} {
		dst := make([]byte, 50)
		MulRegion(dst, src, c) // longer src: only 50 bytes processed, no panic
		for i := range dst {
			if dst[i] != Mul(src[i], c) {
				t.Fatalf("c=%d i=%d mismatch", c, i)
			}
		}
		long := make([]byte, 120)
		MulRegion(long, src, c) // longer dst: tail untouched
		if !bytes.Equal(long[100:], make([]byte, 20)) {
			t.Fatalf("c=%d: tail beyond src modified", c)
		}
	}
}

func BenchmarkMulRegion(b *testing.B) {
	for _, n := range []int{4096, 1 << 20} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := randomBytes(1, n)
			dst := make([]byte, n)
			b.SetBytes(int64(n))
			for b.Loop() {
				MulRegion(dst, src, 0x8E)
			}
		})
	}
}

func BenchmarkXorRegion(b *testing.B) {
	src := randomBytes(1, 1<<20)
	dst := make([]byte, 1<<20)
	b.SetBytes(1 << 20)
	for b.Loop() {
		XorRegion(dst, src)
	}
}
