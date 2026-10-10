package reedsolomon

import (
	"bytes"
	"errors"
	"math/bits"
	"math/rand/v2"
	"testing"

	"github.com/eberle1080/go-aaru/internal/gf256"
)

const shardSize = 1024

// fillRandom fills b with deterministic pseudo-random bytes for seed.
func fillRandom(seed uint64, b []byte) {
	r := rand.New(rand.NewPCG(seed, 0x7273))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
}

// fillShard fills a shard with deterministic data derived from its index
// (seeded with index*12345+67890).
func fillShard(shard []byte, index int) {
	fillRandom(uint64(index)*12345+67890, shard)
}

type fixture struct {
	shards  [][]byte
	backup  [][]byte
	present []bool
}

// setupShards allocates K data shards filled with deterministic data and M
// zeroed parity shards.
func setupShards(k, m, size int) *fixture {
	f := &fixture{
		shards:  make([][]byte, k+m),
		backup:  make([][]byte, k),
		present: make([]bool, k+m),
	}
	for i := range f.shards {
		f.shards[i] = make([]byte, size)
		f.present[i] = true
	}
	for i := range k {
		fillShard(f.shards[i], i)
		f.backup[i] = bytes.Clone(f.shards[i])
	}
	return f
}

func (f *fixture) encode(c *Codec) {
	k := c.DataShards()
	for m := range c.ParityShards() {
		for i := range k {
			EncodeIncremental(c.Coefficient(m, i), f.shards[i], f.shards[k+m])
		}
	}
}

func (f *fixture) erase(i int) {
	clear(f.shards[i])
	f.present[i] = false
}

func mustNew(t *testing.T, k, m int) *Codec {
	t.Helper()
	c, err := New(k, m)
	if err != nil {
		t.Fatalf("New(%d,%d): %v", k, m, err)
	}
	return c
}

func TestCreateFree(t *testing.T) {
	c := mustNew(t, 4, 2)
	if c.DataShards() != 4 || c.ParityShards() != 2 || c.TotalShards() != 6 {
		t.Fatalf("unexpected dimensions %d/%d/%d", c.DataShards(), c.ParityShards(), c.TotalShards())
	}
}

func TestCreateInvalid(t *testing.T) {
	for _, tc := range []struct{ k, m int }{{0, 2}, {4, 0}, {200, 56}, {-1, 2}, {2, -1}} {
		if c, err := New(tc.k, tc.m); c != nil || !errors.Is(err, ErrInvalidShardCounts) {
			t.Errorf("New(%d,%d) = %v, %v; want nil, ErrInvalidShardCounts", tc.k, tc.m, c, err)
		}
	}
	if _, err := New(200, 55); err != nil { // 200+55 = 255, OK
		t.Errorf("New(200,55): %v", err)
	}
}

func TestEncodeDecodeXOR_M1(t *testing.T) {
	const k, m = 4, 1
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	for i := range k {
		EncodeIncremental(c.Coefficient(0, i), f.shards[i], f.shards[k])
	}
	f.erase(0)
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.shards[0], f.backup[0]) {
		t.Fatal("shard 0 not recovered")
	}
}

func TestEncodeDecodeSingleErasure(t *testing.T) {
	const k, m = 8, 2
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	f.encode(c)

	for e := range k {
		for i := range k {
			copy(f.shards[i], f.backup[i])
		}
		for i := range f.present {
			f.present[i] = true
		}
		f.erase(e)
		if err := c.Decode(f.shards, f.present); err != nil {
			t.Fatalf("e=%d: %v", e, err)
		}
		if !bytes.Equal(f.shards[e], f.backup[e]) {
			t.Fatalf("e=%d: not recovered", e)
		}
	}
}

func TestEncodeDecodeDoubleErasure(t *testing.T) {
	const k, m = 8, 2
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	f.encode(c)
	f.erase(2)
	f.erase(5)
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	for _, e := range []int{2, 5} {
		if !bytes.Equal(f.shards[e], f.backup[e]) {
			t.Fatalf("shard %d not recovered", e)
		}
	}
}

func TestEncodeDecodeMaxErasure(t *testing.T) {
	const k, m = 4, 4
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	f.encode(c)
	for e := range m { // erase all data shards; parity alone recovers them
		f.erase(e)
	}
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	for e := range m {
		if !bytes.Equal(f.shards[e], f.backup[e]) {
			t.Fatalf("shard %d not recovered", e)
		}
	}
}

func TestTooManyErasures(t *testing.T) {
	const k, m = 4, 2
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	f.encode(c)
	f.erase(0)
	f.erase(1)
	f.erase(2)
	if err := c.Decode(f.shards, f.present); !errors.Is(err, ErrTooManyErasures) {
		t.Fatalf("got %v, want ErrTooManyErasures", err)
	}
}

func TestParityErasure(t *testing.T) {
	const k, m = 4, 2
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	f.encode(c)
	parity0 := bytes.Clone(f.shards[k])
	f.erase(k)
	f.erase(1)
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.shards[1], f.backup[1]) {
		t.Fatal("data shard 1 not recovered")
	}
	if !bytes.Equal(f.shards[k], parity0) {
		t.Fatal("parity shard 0 not recovered")
	}
}

func TestNoErasureIsNoop(t *testing.T) {
	const k, m = 4, 2
	c := mustNew(t, k, m)
	f := setupShards(k, m, shardSize)
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	for i := range k {
		if !bytes.Equal(f.shards[i], f.backup[i]) {
			t.Fatalf("shard %d modified", i)
		}
	}
}

func TestBoundaryK1M1(t *testing.T) {
	c := mustNew(t, 1, 1)
	f := setupShards(1, 1, shardSize)
	EncodeIncremental(c.Coefficient(0, 0), f.shards[0], f.shards[1])
	f.erase(0)
	if err := c.Decode(f.shards, f.present); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.shards[0], f.backup[0]) {
		t.Fatal("shard 0 not recovered")
	}
}

func TestVariousKM(t *testing.T) {
	configs := []struct{ k, m int }{
		{2, 1}, {4, 1}, {8, 1}, {16, 1},
		{2, 2}, {4, 2}, {8, 2}, {16, 2},
		{4, 4}, {8, 4}, {16, 4},
	}
	for _, cfg := range configs {
		c := mustNew(t, cfg.k, cfg.m)
		f := setupShards(cfg.k, cfg.m, shardSize)
		f.encode(c)
		for e := range cfg.m {
			f.erase(e)
		}
		if err := c.Decode(f.shards, f.present); err != nil {
			t.Fatalf("K=%d M=%d: %v", cfg.k, cfg.m, err)
		}
		for e := range cfg.m {
			if !bytes.Equal(f.shards[e], f.backup[e]) {
				t.Fatalf("K=%d M=%d e=%d: not recovered", cfg.k, cfg.m, e)
			}
		}
	}
}

func TestLargeShard(t *testing.T) {
	const size = 65536
	const k, m = 4, 2
	c := mustNew(t, k, m)

	shards := make([][]byte, k+m)
	backup := make([][]byte, k)
	present := make([]bool, k+m)
	for i := range shards {
		shards[i] = make([]byte, size)
		present[i] = true
	}
	for i := range k {
		fillRandom(uint64(i)*54321, shards[i])
		backup[i] = bytes.Clone(shards[i])
	}
	for p := range m {
		for i := range k {
			EncodeIncremental(c.Coefficient(p, i), shards[i], shards[k+p])
		}
	}
	clear(shards[1])
	present[1] = false
	clear(shards[3])
	present[3] = false

	if err := c.Decode(shards, present); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shards[1], backup[1]) || !bytes.Equal(shards[3], backup[3]) {
		t.Fatal("shards not recovered")
	}
}

// TestAllErasurePatterns exhaustively checks every erasure pattern of up to M
// shards for a few small configurations.
func TestAllErasurePatterns(t *testing.T) {
	for _, cfg := range []struct{ k, m int }{{1, 3}, {3, 3}, {4, 3}, {5, 2}, {6, 4}} {
		c := mustNew(t, cfg.k, cfg.m)
		n := cfg.k + cfg.m
		f := setupShards(cfg.k, cfg.m, 257)
		f.encode(c)
		want := make([][]byte, n)
		for i := range n {
			want[i] = bytes.Clone(f.shards[i])
		}
		for mask := 1; mask < 1<<n; mask++ {
			if bits.OnesCount(uint(mask)) > cfg.m {
				continue
			}
			shards := make([][]byte, n)
			present := make([]bool, n)
			for i := range n {
				present[i] = mask&(1<<i) == 0
				if present[i] {
					shards[i] = bytes.Clone(want[i])
				} else if i%2 == 0 {
					shards[i] = bytes.Repeat([]byte{0xEE}, 257) // garbage to be overwritten
				} // odd erased shards stay nil and must be allocated by Decode
			}
			if err := c.Decode(shards, present); err != nil {
				t.Fatalf("K=%d M=%d mask=%b: %v", cfg.k, cfg.m, mask, err)
			}
			for i := range n {
				if !bytes.Equal(shards[i], want[i]) {
					t.Fatalf("K=%d M=%d mask=%b: shard %d mismatch", cfg.k, cfg.m, mask, i)
				}
			}
		}
	}
}

func TestCodingMatrixIsSystematic(t *testing.T) {
	for _, cfg := range []struct{ k, m int }{{1, 1}, {4, 2}, {17, 3}, {200, 55}} {
		c := mustNew(t, cfg.k, cfg.m)
		for i := range cfg.k {
			for j := range cfg.k {
				want := byte(0)
				if i == j {
					want = 1
				}
				if got := c.coding[i*cfg.k+j]; got != want {
					t.Fatalf("K=%d M=%d: coding[%d][%d] = %d, want %d", cfg.k, cfg.m, i, j, got, want)
				}
			}
		}
	}
}

func TestEncodeHelperMatchesIncremental(t *testing.T) {
	const k, m = 5, 3
	c := mustNew(t, k, m)
	f := setupShards(k, m, 300)
	f.encode(c)

	data := make([][]byte, k)
	for i := range k {
		data[i] = f.shards[i]
	}
	parity := make([][]byte, m)
	for i := range m {
		parity[i] = bytes.Repeat([]byte{0x77}, 300) // must be overwritten
	}
	if err := c.Encode(data, parity); err != nil {
		t.Fatal(err)
	}
	for i := range m {
		if !bytes.Equal(parity[i], f.shards[k+i]) {
			t.Fatalf("parity %d mismatch", i)
		}
	}

	// Short (zero-padded) data shards are accepted.
	short := make([][]byte, k)
	for i := range k {
		short[i] = data[i][:100+i]
	}
	if err := c.Encode(short, parity); err != nil {
		t.Fatal(err)
	}
	padded := make([][]byte, k+m)
	for i := range k {
		padded[i] = make([]byte, 300)
		copy(padded[i], short[i])
	}
	for i := range m {
		padded[k+i] = make([]byte, 300)
	}
	pf := &fixture{shards: padded}
	pf.encode(c)
	for i := range m {
		if !bytes.Equal(parity[i], padded[k+i]) {
			t.Fatalf("short-shard parity %d mismatch", i)
		}
	}

	if err := c.Encode(data[:k-1], parity); !errors.Is(err, ErrShardCount) {
		t.Errorf("wrong data count: got %v", err)
	}
	if err := c.Encode(data, [][]byte{parity[0], parity[1], parity[2][:10]}); !errors.Is(err, ErrShardSize) {
		t.Errorf("uneven parity: got %v", err)
	}
	if err := c.Encode([][]byte{make([]byte, 301), data[1], data[2], data[3], data[4]}, parity); !errors.Is(err, ErrShardSize) {
		t.Errorf("oversized data: got %v", err)
	}
}

func TestDecodeArgumentErrors(t *testing.T) {
	c := mustNew(t, 3, 2)
	f := setupShards(3, 2, 64)
	if err := c.Decode(f.shards[:4], f.present); !errors.Is(err, ErrShardCount) {
		t.Errorf("short shards: got %v", err)
	}
	if err := c.Decode(f.shards, f.present[:4]); !errors.Is(err, ErrShardCount) {
		t.Errorf("short present: got %v", err)
	}
	f.shards[2] = f.shards[2][:63]
	f.present[0] = false
	if err := c.Decode(f.shards, f.present); !errors.Is(err, ErrShardSize) {
		t.Errorf("uneven shards: got %v", err)
	}
}

func TestCoefficientPanicsOutOfRange(t *testing.T) {
	c := mustNew(t, 3, 2)
	for _, idx := range [][2]int{{2, 0}, {0, 3}, {-1, 0}, {0, -1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Coefficient(%d,%d) did not panic", idx[0], idx[1])
				}
			}()
			c.Coefficient(idx[0], idx[1])
		}()
	}
}

// pow returns x^e in GF(2^8), with 0^0 = 1.
func pow(x byte, e int) byte {
	r := byte(1)
	for range e {
		r = gf256.Mul(r, x)
	}
	return r
}

// The coding matrix C is defined as V * inverse(V[:K]) for the Vandermonde
// matrix V[i][j] = i^j, which is equivalent to C * V[:K] == V. Checking the
// product pins every coefficient without inverting anything.
func TestCodingMatrixMatchesDefinition(t *testing.T) {
	for _, cfg := range []struct{ k, m int }{
		{1, 1}, {2, 1}, {3, 1}, {4, 1}, {8, 1}, {16, 1}, {2, 2}, {4, 2}, {8, 2}, {16, 2},
		{4, 4}, {8, 4}, {16, 4}, {10, 10}, {32, 8}, {100, 20}, {200, 55}, {1, 254}, {254, 1},
	} {
		c := mustNew(t, cfg.k, cfg.m)
		k, n := cfg.k, cfg.k+cfg.m
		v := make([][]byte, n)
		for i := range n {
			v[i] = make([]byte, k)
			for j := range k {
				v[i][j] = pow(byte(i), j)
			}
		}
		for i := range n {
			for j := range k {
				var got byte
				for x := range k {
					got ^= gf256.Mul(c.coding[i*k+x], v[x][j])
				}
				if want := v[i][j]; got != want {
					t.Fatalf("K=%d M=%d: (C*V_top)[%d][%d] = %#x, want V[%d][%d] = %#x", k, cfg.m, i, j, got, i, j, want)
				}
			}
		}
	}
}

// The package documentation gives the K=4, M=1 generator row as an example;
// it follows from the definition checked above.
func TestDocumentedGeneratorExample(t *testing.T) {
	c := mustNew(t, 4, 1)
	want := []byte{0x1b, 0x1c, 0x12, 0x14}
	for i, w := range want {
		if got := c.Coefficient(0, i); got != w {
			t.Errorf("Coefficient(0, %d) = %#x, want %#x", i, got, w)
		}
	}
}

// Parity is linear: the parity of XORed data is the XOR of the parities.
func TestParityLinear(t *testing.T) {
	const k, m, size = 6, 3, 129
	c := mustNew(t, k, m)
	a, b, x := make([][]byte, k), make([][]byte, k), make([][]byte, k)
	for i := range k {
		a[i], b[i], x[i] = make([]byte, size), make([]byte, size), make([]byte, size)
		fillRandom(uint64(100+i), a[i])
		fillRandom(uint64(200+i), b[i])
		for j := range size {
			x[i][j] = a[i][j] ^ b[i][j]
		}
	}
	pa, pb, px := make([][]byte, m), make([][]byte, m), make([][]byte, m)
	for i := range m {
		pa[i], pb[i], px[i] = make([]byte, size), make([]byte, size), make([]byte, size)
	}
	for _, e := range []struct{ d, p [][]byte }{{a, pa}, {b, pb}, {x, px}} {
		if err := c.Encode(e.d, e.p); err != nil {
			t.Fatal(err)
		}
	}
	for i := range m {
		for j := range size {
			if px[i][j] != pa[i][j]^pb[i][j] {
				t.Fatalf("parity %d byte %d not linear", i, j)
			}
		}
	}
}

// Random erasure patterns of up to M shards are recovered for larger codecs
// than the exhaustive test covers.
func TestRandomErasurePatterns(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, cfg := range []struct{ k, m, trials int }{{20, 10, 50}, {64, 16, 20}, {200, 55, 3}} {
		c := mustNew(t, cfg.k, cfg.m)
		n := cfg.k + cfg.m
		f := setupShards(cfg.k, cfg.m, 64)
		f.encode(c)
		want := make([][]byte, n)
		for i := range n {
			want[i] = bytes.Clone(f.shards[i])
		}
		for range cfg.trials {
			shards := make([][]byte, n)
			present := make([]bool, n)
			for i := range n {
				shards[i] = bytes.Clone(want[i])
				present[i] = true
			}
			for _, e := range r.Perm(n)[:1+r.IntN(cfg.m)] {
				present[e] = false
				clear(shards[e])
			}
			if err := c.Decode(shards, present); err != nil {
				t.Fatalf("K=%d M=%d: %v", cfg.k, cfg.m, err)
			}
			for i := range n {
				if !bytes.Equal(shards[i], want[i]) {
					t.Fatalf("K=%d M=%d: shard %d mismatch", cfg.k, cfg.m, i)
				}
			}
		}
	}
}

func BenchmarkDecodeK16M4(b *testing.B) {
	const k, m, size = 16, 4, 1 << 16
	c, _ := New(k, m)
	f := setupShards(k, m, size)
	f.encode(c)
	b.SetBytes(int64(k * size))
	for b.Loop() {
		for e := range m {
			f.present[e] = false
		}
		_ = c.Decode(f.shards, f.present)
		for e := range m {
			f.present[e] = true
		}
	}
}
