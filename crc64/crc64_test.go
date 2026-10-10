package crc64

import (
	"bytes"
	"encoding/binary"
	stdcrc64 "hash/crc64"
	"math/bits"
	"math/rand/v2"
	"testing"
)

// checkValue is the published check value of CRC-64/XZ (reflected
// ECMA-182) for the ASCII string "123456789".
const checkValue = 0x995DC9BBDF1939FA

// testData returns n deterministic pseudo-random bytes.
func testData(n int) []byte {
	r := rand.New(rand.NewPCG(0x6372633634, 0x74657374))
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
	buf = testData(1 << 20)
	m := make([]byte, len(buf)+1)
	copy(m[1:], buf)
	return buf, m[1:]
}

// bitwiseUpdate is a direct, bit-at-a-time transcription of the reflected
// CRC definition with polynomial Poly. It shares no code with the table
// driven implementation.
func bitwiseUpdate(crc uint64, data []byte) uint64 {
	for _, b := range data {
		crc ^= uint64(b)
		for range 8 {
			if crc&1 == 1 {
				crc = crc>>1 ^ Poly
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func bitwiseChecksum(data []byte) uint64 { return bitwiseUpdate(Seed, data) ^ Seed }

// variants are four ways of computing the same function with New, Update
// and Checksum.
var variants = []struct {
	name string
	fn   func(data []byte) uint64
}{
	{"streaming", func(data []byte) uint64 {
		h := New()
		h.Write(data)
		return h.Sum64()
	}},
	{"update", func(data []byte) uint64 {
		crc := Seed
		crc = Update(crc, data)
		return crc ^ Seed
	}},
	{"inverted", func(data []byte) uint64 {
		// A function that takes and returns the inverted register:
		// inv(prev, d) == ^Update(^prev, d).
		inv := func(prev uint64, d []byte) uint64 { return ^Update(^prev, d) }
		crc := ^inv(^Seed, data)
		return crc ^ Seed
	}},
	{"checksum", Checksum},
}

// The standard check value for "123456789".
func TestCheckValue(t *testing.T) {
	for _, v := range variants {
		if got := v.fn([]byte("123456789")); got != checkValue {
			t.Errorf("%s: got 0x%016X, want 0x%016X", v.name, got, uint64(checkValue))
		}
	}
	if got := bitwiseChecksum([]byte("123456789")); got != checkValue {
		t.Errorf("bitwise: got 0x%016X, want 0x%016X", got, uint64(checkValue))
	}
}

// Every variant agrees with the bitwise definition for the full buffer, a
// misaligned copy, and lengths around the 16-byte main loop boundaries.
func TestMatchesBitwise(t *testing.T) {
	buf, misaligned := loadData(t)
	want := bitwiseChecksum(buf)
	cases := []struct {
		name string
		data []byte
		want uint64
	}{
		{"full", buf, want},
		{"misaligned", misaligned, want},
	}
	for _, n := range []int{1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 255, 256, 2352, 4095, 4096, 4097, 65537} {
		cases = append(cases, struct {
			name string
			data []byte
			want uint64
		}{name: "prefix", data: buf[:n], want: bitwiseChecksum(buf[:n])})
	}
	for _, v := range variants {
		for _, c := range cases {
			if got := v.fn(c.data); got != c.want {
				t.Errorf("%s %s len=%d: got 0x%016X, want 0x%016X", v.name, c.name, len(c.data), got, c.want)
			}
		}
	}
}

// Every alignment and every length that exercises the 16-byte main loop and
// the byte tail, plus non-standard starting registers.
func TestUpdateMatchesBitwiseAllAlignments(t *testing.T) {
	buf := testData(1200)
	for off := range 16 {
		for n := 0; n <= 1100; n += 1 + n/64 {
			data := buf[off : off+n]
			for _, reg := range []uint64{Seed, 0, 0x0123456789ABCDEF} {
				if got, want := Update(reg, data), bitwiseUpdate(reg, data); got != want {
					t.Fatalf("offset=%d len=%d reg=%X: got 0x%016X, bitwise 0x%016X", off, n, reg, got, want)
				}
			}
		}
	}
}

// The slicing tables follow from their definition: table[0][b] is the
// register after feeding byte b into a zero register, and table[k][b] is the
// register after feeding b followed by k zero bytes.
func TestTablesMatchDefinition(t *testing.T) {
	for k := range len(table) {
		for b := range 256 {
			in := make([]byte, 1+k)
			in[0] = byte(b)
			if got, want := table[k][b], bitwiseUpdate(0, in); got != want {
				t.Fatalf("table[%d][0x%02X] = 0x%016X, want 0x%016X", k, b, got, want)
			}
		}
	}
	// table[0][0x80] is the polynomial itself: the single set bit reaches
	// bit 0 after seven shifts and the eighth shift XORs in Poly.
	if table[0][0x80] != Poly {
		t.Errorf("table[0][0x80] = 0x%016X, want Poly", table[0][0x80])
	}
	// Linearity over GF(2): table[k][a^b] == table[k][a] ^ table[k][b].
	for k := range len(table) {
		for a := range 256 {
			for _, b := range []int{1, 0x55, 0x80, 0xFF} {
				if table[k][a^b] != table[k][a]^table[k][b] {
					t.Fatalf("table[%d] not linear at a=%#x b=%#x", k, a, b)
				}
			}
		}
	}
}

// The checksum of empty input is 0.
func TestEmpty(t *testing.T) {
	if got := Checksum(nil); got != 0 {
		t.Errorf("Checksum(nil) = 0x%016X, want 0", got)
	}
	if got := Update(Seed, nil); got != Seed {
		t.Errorf("Update(Seed, nil) = 0x%016X, want Seed", got)
	}
	if got := Update(0x0123456789ABCDEF, []byte{}); got != 0x0123456789ABCDEF {
		t.Errorf("Update(x, empty) = 0x%016X, want x", got)
	}
	if got := New().Sum64(); got != 0 {
		t.Errorf("New().Sum64() = 0x%016X, want 0", got)
	}
}

// The package documentation states that Checksum equals the standard
// library's hash/crc64 with the ECMA table, and documents how Update relates
// to hash/crc64.Update.
func TestStdlibRelationship(t *testing.T) {
	buf, _ := loadData(t)
	ecma := stdcrc64.MakeTable(stdcrc64.ECMA)
	for _, n := range []int{0, 1, 15, 16, 17, 1000, len(buf)} {
		if got, want := Checksum(buf[:n]), stdcrc64.Checksum(buf[:n], ecma); got != want {
			t.Fatalf("len=%d: got 0x%016X, stdlib 0x%016X", n, got, want)
		}
	}
	c := stdcrc64.Checksum(buf[:100], ecma)
	if got, want := Update(c^Seed, buf[100:5000])^Seed, stdcrc64.Update(c, ecma, buf[100:5000]); got != want {
		t.Fatalf("Update relation: got 0x%016X, stdlib 0x%016X", got, want)
	}
}

// Update composes: feeding a buffer in any split equals feeding it whole.
func TestUpdateSplits(t *testing.T) {
	data := testData(300)
	want := Checksum(data)
	for i := 0; i <= len(data); i++ {
		for _, j := range []int{i, (i + len(data)) / 2, len(data)} {
			crc := Update(Seed, data[:i])
			crc = Update(crc, data[i:j])
			crc = Update(crc, data[j:])
			if got := crc ^ Seed; got != want {
				t.Fatalf("split %d/%d: got 0x%016X, want 0x%016X", i, j, got, want)
			}
		}
	}
	// Extending a finalized checksum.
	if got := Update(Checksum(data[:123])^Seed, data[123:]) ^ Seed; got != want {
		t.Fatalf("extend finalized: got 0x%016X, want 0x%016X", got, want)
	}
}

// CRC linearity: for equal-length a, b, c,
// Checksum(a^b^c) == Checksum(a) ^ Checksum(b) ^ Checksum(c).
func TestLinearity(t *testing.T) {
	src := testData(3 * 1000)
	a, b, c := src[:1000], src[1000:2000], src[2000:]
	x := make([]byte, 1000)
	for i := range x {
		x[i] = a[i] ^ b[i] ^ c[i]
	}
	if got, want := Checksum(x), Checksum(a)^Checksum(b)^Checksum(c); got != want {
		t.Fatalf("got 0x%016X, want 0x%016X", got, want)
	}
}

// Any single-bit error changes the checksum.
func TestDetectsSingleBitErrors(t *testing.T) {
	data := testData(512)
	want := Checksum(data)
	for i := range len(data) * 8 {
		data[i/8] ^= 1 << (i % 8)
		if Checksum(data) == want {
			t.Fatalf("bit %d flip not detected", i)
		}
		data[i/8] ^= 1 << (i % 8)
	}
}

// Streaming in any chunk size equals the one-shot checksum.
func TestHashStreaming(t *testing.T) {
	buf, _ := loadData(t)
	want := Checksum(buf)
	for _, chunk := range []int{1, 3, 15, 16, 17, 512, 2352, 4096, 65537} {
		h := New()
		for p := buf; len(p) > 0; {
			n := min(chunk, len(p))
			if w, err := h.Write(p[:n]); w != n || err != nil {
				t.Fatalf("Write = %d, %v", w, err)
			}
			p = p[n:]
		}
		if got := h.Sum64(); got != want {
			t.Errorf("chunk %d: got 0x%016X, want 0x%016X", chunk, got, want)
		}
	}
}

func TestHashInterface(t *testing.T) {
	buf, _ := loadData(t)
	h := New()
	if h.Size() != Size || Size != 8 {
		t.Errorf("Size() = %d", h.Size())
	}
	if h.BlockSize() != 1 {
		t.Errorf("BlockSize() = %d", h.BlockSize())
	}
	h.Write(buf[:2352])
	want2352 := Checksum(buf[:2352])

	// Sum appends big-endian and does not change the state.
	prefix := []byte{0xAA, 0xBB}
	sum := h.Sum(prefix)
	want := binary.BigEndian.AppendUint64([]byte{0xAA, 0xBB}, want2352)
	if !bytes.Equal(sum, want) {
		t.Errorf("Sum = %X, want %X", sum, want)
	}
	if got := h.Sum64(); got != want2352 {
		t.Errorf("Sum64 after Sum = 0x%016X", got)
	}
	h.Write(buf[2352:])
	if got, want := h.Sum64(), Checksum(buf); got != want {
		t.Errorf("continued Sum64 = 0x%016X, want 0x%016X", got, want)
	}

	h.Reset()
	if got := h.Sum64(); got != 0 {
		t.Errorf("Sum64 after Reset = 0x%016X", got)
	}
	h.Write([]byte("123456789"))
	if got := h.Sum64(); got != checkValue {
		t.Errorf("Sum64 after Reset+Write = 0x%016X", got)
	}
}

// Documents the on-disk relationships described in the package doc.
func TestOnDiskRepresentation(t *testing.T) {
	data := testData(2352)
	crc := Checksum(data)

	// V2+ images: the uint64 field is stored little-endian.
	field := binary.LittleEndian.AppendUint64(nil, crc)
	if binary.LittleEndian.Uint64(field) != crc {
		t.Fatal("little-endian round trip failed")
	}

	// V1 images: the stored value is the byte-swapped checksum, whose
	// little-endian encoding equals the hash.Hash64 Sum (big-endian) bytes.
	h := New()
	h.Write(data)
	v1Field := binary.LittleEndian.AppendUint64(nil, bits.ReverseBytes64(crc))
	if !bytes.Equal(v1Field, h.Sum(nil)) {
		t.Fatalf("V1 field %X != Sum %X", v1Field, h.Sum(nil))
	}
}

var sink uint64

func benchmarkChecksum(b *testing.B, n int) {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*7 + 3)
	}
	b.SetBytes(int64(n))
	for b.Loop() {
		sink = Checksum(data)
	}
}

func BenchmarkChecksum64(b *testing.B)   { benchmarkChecksum(b, 64) }
func BenchmarkChecksum2352(b *testing.B) { benchmarkChecksum(b, 2352) }
func BenchmarkChecksum1M(b *testing.B)   { benchmarkChecksum(b, 1<<20) }
