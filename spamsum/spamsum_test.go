package spamsum

import (
	"bytes"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// testData returns n deterministic pseudo-random bytes for the given seed.
func testData(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 0x7370616d73756d))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// loadData returns a 1 MiB deterministic buffer and a copy of it that starts
// one byte into its backing array.
func loadData(t testing.TB) (buf, misaligned []byte) {
	t.Helper()
	buf = testData(1, 1<<20)
	m := make([]byte, len(buf)+1)
	copy(m[1:], buf)
	return buf, m[1:]
}

func writeChunked(h *Hasher, data []byte, chunk int) {
	if chunk <= 0 {
		chunk = len(data)
	}
	for len(data) > 0 {
		n := min(chunk, len(data))
		h.Write(data[:n])
		data = data[n:]
	}
}

// parsed is a digest split into its three fields.
type parsed struct {
	blockSize uint64
	h1, h2    string
}

// checkFormat verifies that d has the form "blocksize:hash1:hash2", that
// blocksize is 3<<k, that both hashes use the base64 alphabet and are at
// most 64 characters long, and that the total length is within
// MaxDigestLength.
func checkFormat(t *testing.T, d string) parsed {
	t.Helper()
	if len(d) > MaxDigestLength {
		t.Fatalf("%q: length %d exceeds MaxDigestLength %d", d, len(d), MaxDigestLength)
	}
	parts := strings.Split(d, ":")
	if len(parts) != 3 {
		t.Fatalf("%q does not have 3 fields", d)
	}
	bs, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("%q: block size: %v", d, err)
	}
	ok := false
	for i := range 30 {
		if bs == uint64(minBlocksize)<<i {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("%q: block size %d is not 3<<k", d, bs)
	}
	for _, p := range parts[1:] {
		if len(p) > spamsumLength {
			t.Fatalf("%q: hash %q longer than %d", d, p, spamsumLength)
		}
		for _, c := range p {
			if !strings.ContainsRune(b64, c) {
				t.Fatalf("%q contains %q", d, c)
			}
		}
	}
	return parsed{bs, parts[1], parts[2]}
}

// initialBlockSize is the smallest block size whose 64-character digest
// covers n bytes, i.e. the block size Sum starts from before adapting to the
// actual digest lengths.
func initialBlockSize(n int) uint64 {
	bs := uint64(minBlocksize)
	for bs*spamsumLength < uint64(n) {
		bs *= 2
	}
	return bs
}

// The digest of a buffer is the same for every entry point, alignment and
// call.
func TestDeterministic(t *testing.T) {
	buf, misaligned := loadData(t)
	for _, n := range []int{1, 15, 31, 63, 2352, len(buf)} {
		want := HashBytes(buf[:n])
		checkFormat(t, want)
		if got := HashBytes(buf[:n]); got != want {
			t.Errorf("len %d: second call %q, want %q", n, got, want)
		}
		if got := HashBytes(misaligned[:n]); got != want {
			t.Errorf("len %d: misaligned %q, want %q", n, got, want)
		}
		h := New()
		if w, err := h.Write(buf[:n]); w != n || err != nil {
			t.Fatalf("Write = %d, %v", w, err)
		}
		if got := h.Sum(); got != want {
			t.Errorf("len %d: Hasher %q, want %q", n, got, want)
		}
	}
}

// Different inputs give different digests.
func TestDistinctInputs(t *testing.T) {
	a := HashBytes(testData(1, 100000))
	b := HashBytes(testData(2, 100000))
	if a == b {
		t.Fatalf("two unrelated inputs share digest %q", a)
	}
}

// Every way of splitting the input gives the same digest as one Write.
func TestStreamingMatchesOneShot(t *testing.T) {
	buf, _ := loadData(t)
	data := buf[:2352]
	want := HashBytes(data)
	for i := 0; i <= len(data); i += 7 {
		h := New()
		h.Write(data[:i])
		h.Write(data[i:])
		if got := h.Sum(); got != want {
			t.Fatalf("split at %d: got %q, want %q", i, got, want)
		}
	}
	want = HashBytes(buf[1:])
	for _, chunk := range []int{1, 7, 4096, 65536} {
		h := New()
		writeChunked(h, buf[1:], chunk)
		if got := h.Sum(); got != want {
			t.Errorf("chunk %d: got %q, want %q", chunk, got, want)
		}
	}
}

// Streaming equality also holds for inputs that exercise the final-character
// sequence elimination (periodic data) and the zero rolling hash branch
// (data ending in zero bytes).
func TestStreamingMatchesOneShotStructured(t *testing.T) {
	for _, data := range structuredInputs() {
		want := HashBytes(data)
		checkFormat(t, want)
		h := New()
		writeChunked(h, data, 1)
		if got := h.Sum(); got != want {
			t.Errorf("len %d: byte-wise %q, one-shot %q", len(data), got, want)
		}
	}
}

// structuredInputs returns periodic inputs, with and without a trailing run
// of zero bytes.
func structuredInputs() [][]byte {
	var out [][]byte
	for _, period := range []int{3, 5, 7, 9, 13, 21, 50, 64, 77, 1000} {
		p := testData(uint64(100+period), period)
		for _, n := range []int{164, 239, 502, 997, 1743, 4096, 100000} {
			b := make([]byte, n)
			for i := range b {
				b[i] = p[i%period]
			}
			out = append(out, b)
			z := bytes.Clone(b)
			clear(z[max(0, n-16):])
			out = append(out, z)
		}
	}
	return out
}

func TestEmpty(t *testing.T) {
	if got := HashBytes(nil); got != "3::" {
		t.Errorf("HashBytes(nil) = %q", got)
	}
	if got := New().Sum(); got != "3::" {
		t.Errorf("New().Sum() = %q", got)
	}
	var z Hasher
	if got := z.Sum(); got != "3::" {
		t.Errorf("zero Hasher Sum = %q", got)
	}
}

// All-zero input keeps the rolling hash at 0, which never reaches a trigger
// point (h % bs == bs-1) and adds no trailing character.
func TestAllZeros(t *testing.T) {
	if got := HashBytes(make([]byte, 1<<20)); got != "3::" {
		t.Errorf("1 MiB of zeros = %q, want \"3::\"", got)
	}
}

func TestZeroValueUsable(t *testing.T) {
	buf, _ := loadData(t)
	var h Hasher
	h.Write(buf[:2352])
	if got, want := h.Sum(), HashBytes(buf[:2352]); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSumDoesNotModifyState(t *testing.T) {
	buf, _ := loadData(t)
	h := New()
	h.Write(buf[:15])
	first := h.Sum()
	if second := h.Sum(); second != first {
		t.Fatalf("second Sum = %q, first %q", second, first)
	}
	h.Write(buf[15:31])
	h.Sum()
	h.Write(buf[31:2352])
	if got, want := h.Sum(), HashBytes(buf[:2352]); got != want {
		t.Fatalf("Sum after interleaved Sums = %q, want %q", got, want)
	}
}

func TestReset(t *testing.T) {
	buf, _ := loadData(t)
	h := New()
	h.Write(buf)
	h.Reset()
	if got := h.Sum(); got != "3::" {
		t.Fatalf("after Reset: %q", got)
	}
	h.Write(buf[:63])
	if got, want := h.Sum(), HashBytes(buf[:63]); got != want {
		t.Fatalf("after Reset+Write: %q, want %q", got, want)
	}
}

func TestIOWriter(t *testing.T) {
	buf, _ := loadData(t)
	var w io.Writer = New()
	if _, err := io.Copy(w, bytes.NewReader(buf)); err != nil {
		t.Fatal(err)
	}
	if got, want := w.(*Hasher).Sum(), HashBytes(buf); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Digest structure over a range of input sizes, including the block size
// selection rule: Sum starts from the smallest block size whose 64-character
// digest covers the input and steps down only while the first hash would be
// shorter than 32 characters. So the chosen block size never exceeds the
// initial one, and whenever it is smaller the second hash (computed with
// twice the block size, i.e. the rejected one) is at most 32 characters.
func TestBlockSizeSelection(t *testing.T) {
	sizes := []int{0, 1, 7, 100, 192, 193, 1000, 5000, 12345, 100000, 1 << 20, 3 << 20}
	if !testing.Short() {
		sizes = append(sizes, 16<<20)
	}
	for _, n := range sizes {
		data := testData(uint64(n)+7, n)
		p := checkFormat(t, HashBytes(data))
		init := initialBlockSize(n)
		if p.blockSize > init {
			t.Errorf("len %d: block size %d exceeds initial %d", n, p.blockSize, init)
		}
		if p.blockSize < init && len(p.h2) > spamsumLength/2 {
			t.Errorf("len %d: stepped down to %d but second hash has %d chars", n, p.blockSize, len(p.h2))
		}
		// For pseudo-random input the first hash has roughly n/bs
		// characters, so the selection lands on the initial block size or
		// one step below it.
		if n >= 1000 && p.blockSize != init && p.blockSize != init/2 {
			t.Errorf("len %d: block size %d, want %d or %d", n, p.blockSize, init, init/2)
		}
		if n >= 1000 && len(p.h1) < spamsumLength/4 {
			t.Errorf("len %d: first hash %q unexpectedly short", n, p.h1)
		}
	}
}

// Inputs up to 192 bytes always use the minimum block size.
func TestSmallInputsUseMinimumBlockSize(t *testing.T) {
	data := testData(3, 192)
	for n := range len(data) + 1 {
		if p := checkFormat(t, HashBytes(data[:n])); p.blockSize != minBlocksize {
			t.Fatalf("len %d: block size %d", n, p.blockSize)
		}
	}
}

// A one-byte change in a large input leaves most of the digest intact.
func TestSimilarInputsSimilarDigests(t *testing.T) {
	buf, _ := loadData(t)
	a := checkFormat(t, HashBytes(buf))
	mod := bytes.Clone(buf)
	mod[len(mod)/2] ^= 0x5A
	b := checkFormat(t, HashBytes(mod))
	if a.blockSize != b.blockSize {
		t.Fatalf("block size changed: %d vs %d", a.blockSize, b.blockSize)
	}
	if a.h1 == b.h1 {
		t.Fatalf("first hash unchanged by a modification: %q", a.h1)
	}
	if common := commonPrefix(a.h1, b.h1) + commonSuffix(a.h1, b.h1); common < len(a.h1)-4 {
		t.Errorf("hashes share only %d of %d characters:\n %q\n %q", common, len(a.h1), a.h1, b.h1)
	}
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// Periodic input produces long runs of one character in the hash body. The
// trailing character is dropped when the body already ends in three copies
// of it, so a hash never ends in more identical characters than its body.
func TestPeriodicInputs(t *testing.T) {
	for _, data := range structuredInputs() {
		h := New()
		h.Write(data)
		p := checkFormat(t, h.Sum())
		for i, bs := range blockSizes {
			if bs != p.blockSize {
				continue
			}
			body := string(h.bh[i].digest[:h.bh[i].dLen])
			if len(p.h1) == len(body)+1 && len(body) >= 3 && strings.Count(body[len(body)-3:], p.h1[len(p.h1)-1:]) == 3 {
				t.Errorf("len %d: %q extends a run of three", len(data), p.h1)
			}
		}
	}
}

func TestAppendElim(t *testing.T) {
	cases := []struct {
		out   string
		start int
		c     byte
		want  string
	}{
		{"3:", 2, 'A', "3:A"},
		{"3:AAA", 2, 'A', "3:AAA"},
		{"3:AAA", 2, 'B', "3:AAAB"},
		{"3:AA", 2, 'A', "3:AAA"},
		{"3:xAAA", 2, 'A', "3:xAAA"},
		{"3:AAxA", 2, 'A', "3:AAxAA"},
		// Runs are only counted inside the current part.
		{"3:AA:AA", 5, 'A', "3:AA:AAA"},
		{"3:AA:AAA", 5, 'A', "3:AA:AAA"},
	}
	for _, c := range cases {
		if got := string(appendElim([]byte(c.out), c.start, c.c)); got != c.want {
			t.Errorf("appendElim(%q, %d, %q) = %q, want %q", c.out, c.start, c.c, got, c.want)
		}
	}
}

// Sum assembles each hash from the stored digest characters of the chosen
// block size plus at most one trailing character.
func TestSumUsesBlockhashState(t *testing.T) {
	inputs := append(structuredInputs(), testData(9, 12345), testData(10, 1<<20))
	for _, data := range inputs {
		h := New()
		h.Write(data)
		p := checkFormat(t, h.Sum())
		bi := -1
		for i, bs := range blockSizes {
			if bs == p.blockSize {
				bi = i
			}
		}
		if bi < int(h.bhStart) || bi >= int(h.bhEnd) {
			t.Fatalf("len %d: block index %d outside live range [%d,%d)", len(data), bi, h.bhStart, h.bhEnd)
		}
		body := string(h.bh[bi].digest[:h.bh[bi].dLen])
		if !strings.HasPrefix(p.h1, body) || len(p.h1) > len(body)+1 {
			t.Errorf("len %d: first hash %q does not extend body %q by at most one char", len(data), p.h1, body)
		}
		if bi+1 < int(h.bhEnd) {
			body2 := string(h.bh[bi+1].digest[:h.bh[bi+1].dLen])
			if !strings.HasPrefix(p.h2, body2) || len(p.h2) > len(body2)+1 {
				t.Errorf("len %d: second hash %q does not extend body %q by at most one char", len(data), p.h2, body2)
			}
		} else if len(p.h2) > 1 {
			t.Errorf("len %d: second hash %q longer than 1 without a larger block hash", len(data), p.h2)
		}
	}
}

func TestBlockSizes(t *testing.T) {
	for i := range 30 {
		if want := uint64(3) << i; blockSizes[i] != want {
			t.Errorf("blockSizes[%d] = %d, want %d", i, blockSizes[i], want)
		}
	}
	// uint64(int32(3 << 30)), sign-extended.
	if blockSizes[30] != 0xFFFFFFFFC0000000 {
		t.Errorf("blockSizes[30] = %#x", blockSizes[30])
	}
}

// The following tests craft Hasher states directly to observe the block
// index 30 overflow behavior (see the package documentation).

// A rolling hash of 0xBFFFFFFF (== 3<<30 - 1) triggers block index 29 but
// not index 30: bh[29].dLen=1, bh[30].dLen=0.
func TestBlockIndex30NeverTriggers(t *testing.T) {
	h := New()
	h.bhStart, h.bhEnd = 29, 31
	for i := 29; i < 31; i++ {
		h.bh[i] = blockhash{h: hashInit, halfH: hashInit}
	}
	h.roll.h2 = 0xBFFFFFFF
	h.Write([]byte{0})
	if sum := h.roll.h1 + h.roll.h2 + h.roll.h3; sum != 3221225471 {
		t.Fatalf("rolling sum = %d", sum)
	}
	if h.bh[29].dLen != 1 || h.bh[30].dLen != 0 {
		t.Fatalf("dLen[29]=%d dLen[30]=%d, want 1 and 0", h.bh[29].dLen, h.bh[30].dLen)
	}
}

// Forcing block index 30 to be selected prints the sign-extended block size,
// and a total size beyond 192 GiB does not fail. The crafted state has 40
// digest characters 'A'+i%26 at every index and a zero rolling hash, so no
// trailing characters are added.
func TestBlockIndex30Selected(t *testing.T) {
	const want = "18446744072635809792:EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE:"
	for _, total := range []uint64{110000000000, 210000000000} {
		h := New()
		h.bhStart, h.bhEnd = 0, 31
		for i := range h.bh {
			b := &h.bh[i]
			b.dLen = 40
			for j := range 40 {
				b.digest[j] = byte('A' + i%26)
			}
			b.digest[40] = 0
			b.h, b.halfH = 5, 6
		}
		h.totalSize = total
		if got := h.Sum(); got != want {
			t.Errorf("total %d: got %q, want %q", total, got, want)
		}
	}
}

var sink string

func BenchmarkHashBytes1M(b *testing.B) {
	data := testData(1, 1<<20)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		sink = HashBytes(data)
	}
}

func BenchmarkWrite4K(b *testing.B) {
	data := testData(1, 1<<20)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		h := New()
		writeChunked(h, data, 4096)
		sink = h.Sum()
	}
}
