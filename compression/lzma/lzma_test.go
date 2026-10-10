package lzma

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
)

// randomData returns n deterministic pseudo-random (incompressible) bytes.
func randomData(seed uint64, n int) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x6c7a6d61))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// compressibleData returns n deterministic bytes that resemble program text:
// words from a small vocabulary, punctuation, numbers and occasional random
// bytes, so the encoder finds literals, short and long matches and repeated
// distances.
func compressibleData(seed uint64, n int) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x636f6465))
	words := []string{"func", "return", "if", "err", "nil", "for", "range", "byte", "int", "len",
		"data", "block", "sector", "image", "header", "offset", "size", "buf", "the", "of"}
	var b bytes.Buffer
	for b.Len() < n {
		switch r := rng.IntN(20); {
		case r < 14:
			b.WriteString(words[rng.IntN(len(words))])
			b.WriteByte(" \n\t(.,"[rng.IntN(6)])
		case r < 18:
			fmt.Fprintf(&b, "%d ", rng.IntN(100000))
		default:
			b.WriteByte(byte(rng.Uint32()))
		}
	}
	return b.Bytes()[:n]
}

// audioData returns n bytes of 16-bit little-endian stereo samples of two
// sine-like waves plus noise, a stand-in for CD audio.
func audioData(seed uint64, n int) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x617564))
	b := make([]byte, n)
	var phaseL, phaseR int32
	for i := 0; i+4 <= n; i += 4 {
		phaseL += 37
		phaseR += 53
		l := triangle(phaseL) + int32(rng.IntN(64)) - 32
		r := triangle(phaseR) + int32(rng.IntN(64)) - 32
		binary.LittleEndian.PutUint16(b[i:], uint16(int16(l)))
		binary.LittleEndian.PutUint16(b[i+2:], uint16(int16(r)))
	}
	return b
}

func triangle(phase int32) int32 {
	p := phase % 4096
	if p < 2048 {
		return (p - 1024) * 16
	}
	return (3072 - p) * 16
}

// mixedData concatenates segments of text-like, random, zero and audio-like
// data, like a disc image.
func mixedData(seed uint64, n int) []byte {
	var b []byte
	for i := 0; len(b) < n; i++ {
		seg := 64<<10 + int(seed+uint64(i))%3*32<<10
		switch i % 4 {
		case 0:
			b = append(b, compressibleData(seed+uint64(i), seg)...)
		case 1:
			b = append(b, randomData(seed+uint64(i), seg/4)...)
		case 2:
			b = append(b, make([]byte, seg)...)
		default:
			b = append(b, audioData(seed+uint64(i), seg)...)
		}
	}
	return b[:n]
}

func encodePayloadT(t testing.TB, src []byte, o Options) []byte {
	t.Helper()
	p, err := EncodePayload(src, o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkRoundTrip(t *testing.T, src []byte, o Options) []byte {
	t.Helper()
	payload := encodePayloadT(t, src, o)
	got, err := DecodePayload(payload, len(src))
	if err != nil {
		t.Fatalf("%+v: DecodePayload: %v", o, err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("%+v: round trip mismatch", o)
	}
	return payload
}

// encodeEndMark encodes like EncodePayload but with an end-of-stream marker
// after the data (AaruFormat never writes end markers, but decoders must
// accept them).
func encodeEndMark(t testing.TB, src []byte, o Options) []byte {
	t.Helper()
	n, err := o.normalize()
	if err != nil {
		t.Fatal(err)
	}
	ep := n.params()
	ep.writeEndMark = true
	props := n.props()
	return encodeTo(props[:], src, ep)
}

// sample is a 64 KiB payload encoded with the AaruFormat default options,
// shared by the decoder tests.
var sample = sync.OnceValues(func() ([]byte, []byte) {
	src := compressibleData(1, 64<<10)
	p, err := EncodePayload(src, DefaultOptions())
	if err != nil {
		panic(err)
	}
	return src, p
})

func TestRoundTrip(t *testing.T) {
	inputs := map[string][]byte{
		"empty":  {},
		"one":    {0x42},
		"short":  []byte("abcabcabcabc"),
		"zeros":  make([]byte, 1<<20),
		"random": randomData(2, 200<<10),
		"text":   bytes.Repeat([]byte("AaruFormat LZMA round trip test. "), 3000),
		"code":   compressibleData(3, 300<<10),
		"audio":  audioData(4, 300<<10),
		"mixed":  mixedData(5, 2<<20),
	}
	optsList := map[string]Options{
		"default":  DefaultOptions(),
		"lc3":      {Level: 9, DictSize: 1 << 20, LC: 3, LP: 0, PB: 2, FB: 273},
		"lc0lp4":   {Level: 5, DictSize: 1 << 16, LC: 0, LP: 4, PB: 4, FB: 32},
		"lc8":      {Level: 1, DictSize: 1 << 16, LC: 8, LP: 0, PB: 0, FB: 5},
		"lc0lp2":   {Level: 9, DictSize: 1 << 25, LC: 0, LP: 2, PB: 2, FB: 273},
		"dict4k":   {Level: 9, DictSize: 4096, LC: 4, LP: 0, PB: 2, FB: 273},
		"level0":   {Level: 0, LC: -1, LP: -1, PB: -1, FB: -1},
		"zero":     {},
		"hugeDict": {Level: 9, DictSize: 0xFFFFFFFF, LC: 4, PB: 2, FB: 273},
	}
	for in, src := range inputs {
		for on, o := range optsList {
			t.Run(in+"/"+on, func(t *testing.T) {
				if testing.Short() && len(src) > 1<<20 && on != "default" && on != "level0" {
					t.Skip("large input")
				}
				checkRoundTrip(t, src, o)
			})
		}
	}
}

// Every level, with its default parameters, and every lc/lp/pb combination
// round trips.
func TestRoundTripLevelsAndLiteralParams(t *testing.T) {
	src := mixedData(6, 300<<10)
	for level := -1; level <= 9; level++ {
		checkRoundTrip(t, src, Options{Level: level, LC: -1, LP: -1, PB: -1, FB: -1})
	}
	small := compressibleData(7, 20000)
	for lc := 0; lc <= MaxLC; lc++ {
		for lp := 0; lp <= MaxLP; lp++ {
			for pb := 0; pb <= MaxPB; pb++ {
				checkRoundTrip(t, small, Options{Level: 9, DictSize: 1 << 16, LC: lc, LP: lp, PB: pb, FB: 64})
				if lc%4 == 0 {
					checkRoundTrip(t, small, Options{Level: 1, DictSize: 1 << 16, LC: lc, LP: lp, PB: pb, FB: 16})
				}
			}
		}
	}
}

// Dictionary sizes around the property rounding boundaries, tiny
// dictionaries (window wrap-around) and out of range fast byte counts.
func TestRoundTripDictSizesAndFastBytes(t *testing.T) {
	src := compressibleData(8, 400<<10)
	for _, d := range []uint32{1, 4095, 4096, 4097, 6145, 65536, 100000, 1 << 20, 2097153, 1 << 25} {
		checkRoundTrip(t, src, Options{Level: 9, DictSize: d, LC: 3, PB: 2, FB: 273})
		checkRoundTrip(t, src, Options{Level: 3, DictSize: d, LC: 3, PB: 2, FB: 32})
	}
	for _, fb := range []int{0, 1, 5, 6, 64, 273, 274, 1000} {
		checkRoundTrip(t, src, Options{Level: 9, DictSize: 1 << 16, LC: 3, PB: 2, FB: fb})
		checkRoundTrip(t, src, Options{Level: 2, DictSize: 1 << 16, LC: 3, PB: 2, FB: fb})
	}
}

// Encoding is deterministic: the same input and options always give the
// same payload, whatever NumThreads says (it is documented to be ignored).
func TestEncodeDeterministic(t *testing.T) {
	src := mixedData(9, 1<<20)
	for _, o := range []Options{
		DefaultOptions(),
		{Level: 1, DictSize: 1 << 16, LC: 3, PB: 2, FB: 32},
		{Level: 9, DictSize: 4096, LC: 0, LP: 4, PB: 4, FB: 5},
	} {
		want := encodePayloadT(t, src, o)
		for _, threads := range []int{-1, 0, 1, 2, 8} {
			o2 := o
			o2.NumThreads = threads
			if got := encodePayloadT(t, src, o2); !bytes.Equal(got, want) {
				t.Errorf("%+v: output differs from NumThreads=%d", o2, o.NumThreads)
			}
		}
	}
}

// Sanity bounds on compression: highly redundant input shrinks to a tiny
// fraction, text-like input to well under half, random input grows only by
// the range coder overhead, and the optimal parser does at least about as
// well as the fast one.
func TestCompressionRatio(t *testing.T) {
	zeros := encodePayloadT(t, make([]byte, 1<<20), DefaultOptions())
	if len(zeros) > 1<<20/500 {
		t.Errorf("1 MiB of zeros compressed to %d bytes", len(zeros))
	}
	code := compressibleData(10, 1<<20)
	normal := encodePayloadT(t, code, DefaultOptions())
	fast := encodePayloadT(t, code, Options{Level: 1, LC: -1, LP: -1, PB: -1, FB: -1})
	if len(normal) > len(code)*45/100 {
		t.Errorf("text-like data compressed to %d of %d bytes", len(normal), len(code))
	}
	if len(normal) > len(fast)*102/100 {
		t.Errorf("level 9 (%d bytes) worse than level 1 (%d bytes)", len(normal), len(fast))
	}
	random := randomData(11, 1<<20)
	if p := encodePayloadT(t, random, DefaultOptions()); len(p) > len(random)*103/100 {
		t.Errorf("random data expanded to %d bytes", len(p))
	}
}

// expectedDictField is the dictionary size field Encode writes: sizes of
// 2 MiB and above are rounded up to a multiple of 1 MiB (saturating at the
// requested size if that overflows), smaller sizes up to the next 2^n or
// 3*2^n, with a minimum of 4 KiB.
func expectedDictField(d uint32) uint32 {
	if d >= 1<<21 {
		v := uint64(d+(1<<20-1)) &^ (1<<20 - 1)
		if v < uint64(d) || d+(1<<20-1) < d {
			return d
		}
		return uint32(v)
	}
	for v := uint64(4096); ; {
		if v >= uint64(d) {
			return uint32(v)
		}
		if 3*v/2 >= uint64(d) && v&(v-1) == 0 {
			return uint32(3 * v / 2)
		}
		v *= 2
	}
}

// The properties block holds (pb*5+lp)*9+lc and the dictionary size field
// described above, and normalize applies the documented defaults.
func TestProps(t *testing.T) {
	src := compressibleData(12, 1000)
	for _, d := range []uint32{0, 1, 4096, 4097, 6144, 6145, 8192, 65536, 100000, 1048576, 2097151, 2097152,
		2097153, 3000000, 33554432, 33554433, MaxDictSize, 0xFFFFFFFF} {
		o := Options{Level: 9, DictSize: d, LC: 4, LP: 0, PB: 2, FB: 273}
		stream, props, err := Encode(src, o)
		if err != nil {
			t.Fatal(err)
		}
		eff := d
		if d == 0 {
			eff = 1 << 26 // level 9 default
		}
		eff = min(eff, MaxDictSize)
		if props[0] != (2*5+0)*9+4 {
			t.Errorf("dict %d: props[0] = %#x", d, props[0])
		}
		if got, want := binary.LittleEndian.Uint32(props[1:]), expectedDictField(eff); got != want {
			t.Errorf("dict %d: dictionary field %d, want %d", d, got, want)
		}
		if got, err := Decode(stream, props[:], len(src)); err != nil || !bytes.Equal(got, src) {
			t.Fatalf("dict %d: round trip failed: %v", d, err)
		}
	}

	// Default dictionary size per level, lc=3 lp=0 pb=2 by default.
	levelDict := func(l int) uint32 {
		switch {
		case l <= 3:
			return 1 << (l*2 + 16)
		case l <= 6:
			return 1 << (l + 19)
		case l == 7:
			return 1 << 25
		}
		return 1 << 26
	}
	for l := -1; l <= 9; l++ {
		_, props, err := Encode(src, Options{Level: l, LC: -1, LP: -1, PB: -1, FB: -1})
		if err != nil {
			t.Fatal(err)
		}
		el := l
		if l < 0 {
			el = 5
		}
		if props[0] != (2*5+0)*9+3 || binary.LittleEndian.Uint32(props[1:]) != levelDict(el) {
			t.Errorf("level %d: props %x", l, props)
		}
	}
	for _, c := range []struct{ lc, lp, pb int }{{8, 4, 4}, {0, 0, 0}, {0, 4, 4}, {8, 0, 0}, {2, 2, 1}} {
		_, props, err := Encode(src, Options{Level: 9, LC: c.lc, LP: c.lp, PB: c.pb, FB: 273})
		if err != nil {
			t.Fatal(err)
		}
		if want := byte((c.pb*5+c.lp)*9 + c.lc); props[0] != want {
			t.Errorf("%+v: props[0] = %#x, want %#x", c, props[0], want)
		}
	}
	// The zero Options value means level 0 with lc=lp=pb=0.
	if _, props, _ := Encode(src, Options{}); props != [5]byte{0, 0, 0, 1, 0} {
		t.Errorf("zero Options: props %x", props)
	}
}

func TestDefaultOptions(t *testing.T) {
	// The parameters AaruFormat writers use for every LZMA block: level 9,
	// the "dictionary" option (default 33554432), lc 4, lp 0, pb 2, fb 273.
	want := Options{Level: 9, DictSize: 33554432, LC: 4, LP: 0, PB: 2, FB: 273, NumThreads: 1}
	if got := DefaultOptions(); got != want {
		t.Fatalf("DefaultOptions() = %+v, want %+v", got, want)
	}
}

func TestEncodeInvalidOptions(t *testing.T) {
	// These are out of range.
	for _, o := range []Options{
		{Level: 9, LC: 9},
		{Level: 9, LP: 5},
		{Level: 9, PB: 5},
	} {
		if _, _, err := Encode([]byte("x"), o); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("Encode(%+v) error = %v, want ErrInvalidOptions", o, err)
		}
		if _, err := EncodePayload([]byte("x"), o); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("EncodePayload(%+v) error = %v, want ErrInvalidOptions", o, err)
		}
	}
}

func TestEncodeEmpty(t *testing.T) {
	// Empty input encodes as the 5-byte range coder flush.
	stream, props, err := Encode(nil, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream, make([]byte, 5)) {
		t.Fatalf("stream = %x, want 0000000000", stream)
	}
	if props != [5]byte{0x5e, 0, 0, 0, 2} {
		t.Fatalf("props = %x", props)
	}
	out, err := Decode(stream, props[:], 0)
	if err != nil || len(out) != 0 {
		t.Fatalf("Decode = %x, %v", out, err)
	}
	payload, err := EncodePayload(nil, DefaultOptions())
	if err != nil || !bytes.Equal(payload, []byte{0x5e, 0, 0, 0, 2, 0, 0, 0, 0, 0}) {
		t.Fatalf("EncodePayload = %x, %v", payload, err)
	}
}

func TestSmallDictionaryWindow(t *testing.T) {
	// A 4 KiB dictionary on a large, repetitive input exercises the
	// encoder's and decoder's window wrap-around.
	src := bytes.Repeat(compressibleData(13, 3000), 400)
	o := Options{Level: 9, DictSize: 4096, LC: 3, PB: 2, FB: 273}
	stream, props, err := Encode(src, o)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(props[1:]) != 4096 {
		t.Fatalf("props dict = %d", binary.LittleEndian.Uint32(props[1:]))
	}
	got, err := Decode(stream, props[:], len(src))
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("round trip failed: %v", err)
	}
	// Dictionary sizes below 4 KiB are treated as 4 KiB.
	props[1], props[2], props[3], props[4] = 0, 0, 0, 0
	got, err = Decode(stream, props[:], len(src))
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("with zero dictionary size in props: %v", err)
	}
}

// TestMatchFinderNormalize lowers maxValForNormalize so that the match finder
// rebases its positions every MiB; the output must not change.
func TestMatchFinderNormalize(t *testing.T) {
	src := mixedData(14, 3<<20)
	if testing.Short() {
		src = src[:1<<20+300000]
	}
	opts := []Options{
		{Level: 9, DictSize: 1 << 16, LC: 4, LP: 0, PB: 2, FB: 273},
		{Level: 1, DictSize: 1 << 16, LC: 3, LP: 0, PB: 2, FB: 32},
	}
	var want [][]byte
	for _, o := range opts {
		want = append(want, encodePayloadT(t, src, o))
	}
	maxValForNormalize = 1<<20 + 0xFFF
	defer func() { maxValForNormalize = 0 }()
	for i, o := range opts {
		if got := encodePayloadT(t, src, o); !bytes.Equal(got, want[i]) {
			t.Errorf("%+v: output changed by normalization", o)
		}
	}
}

// A large input with a dictionary much smaller than the input, as images
// with a reduced "dictionary" option are written.
func TestLargeRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	src := mixedData(15, 8<<20)
	checkRoundTrip(t, src, Options{Level: 9, DictSize: 1 << 20, LC: 3, LP: 0, PB: 2, FB: 273, NumThreads: 2})
	checkRoundTrip(t, src, DefaultOptions())
}

func TestDecodeTrailingData(t *testing.T) {
	src, payload := sample()
	junk := append(bytes.Clone(payload), 0xde, 0xad, 0xbe, 0xef, 0x01)
	if got, err := DecodePayload(junk, len(src)); err != nil || !bytes.Equal(got, src) {
		t.Fatalf("with trailing data: %v", err)
	}
	if got, err := Decode(payload[PropsLen:], payload[:PropsLen], len(src)); err != nil || !bytes.Equal(got, src) {
		t.Fatalf("Decode: %v", err)
	}
}

func TestDecodeIgnoresHugeDictionary(t *testing.T) {
	// Decoding goes straight into the destination and must never allocate
	// the dictionary announced in the properties.
	src, payload := sample()
	payload = bytes.Clone(payload)
	binary.LittleEndian.PutUint32(payload[1:], 0xFFFFFFFF)
	got, err := DecodePayload(payload, len(src))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, src) {
		t.Fatal("mismatch")
	}
}

func TestDecodeEOSMarker(t *testing.T) {
	// Streams that carry an end-of-stream marker after exactly the
	// expected number of bytes are accepted.
	src := compressibleData(16, 16384)
	for _, o := range []Options{
		DefaultOptions(),
		{Level: 1, DictSize: 1 << 16, LC: 3, LP: 0, PB: 2, FB: 32},
		{Level: 9, DictSize: 4096, LC: 0, LP: 4, PB: 4, FB: 5},
		{Level: 0, LC: -1, LP: -1, PB: -1, FB: -1},
	} {
		payload := encodeEndMark(t, src, o)
		if plain := encodePayloadT(t, src, o); len(payload) <= len(plain) {
			t.Fatalf("%+v: end marker stream (%d bytes) not longer than plain (%d)", o, len(payload), len(plain))
		}
		got, err := DecodePayload(payload, len(src))
		if err != nil || !bytes.Equal(got, src) {
			t.Fatalf("%+v: Decode with EOS marker: %v", o, err)
		}
		// An EOS marker before the expected size is an error (a short
		// output).
		if _, err := DecodePayload(payload, len(src)+1); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%+v: early EOS marker error = %v, want ErrCorrupt", o, err)
		}
		// Stopping before the marker is fine.
		if got, err := DecodePayload(payload, len(src)-1); err != nil || !bytes.Equal(got, src[:len(src)-1]) {
			t.Fatalf("%+v: size-1: %v", o, err)
		}
		// A stream cut within the data is truncated.
		if _, err := DecodePayload(payload[:len(payload)/2], len(src)); !errors.Is(err, ErrTruncated) {
			t.Fatalf("%+v: cut stream error = %v, want ErrTruncated", o, err)
		}
	}

	// A stream holding nothing but an end marker.
	empty := encodeEndMark(t, nil, DefaultOptions())
	if len(empty) <= PropsLen+5 {
		t.Fatalf("empty end marker stream is only %d bytes", len(empty))
	}
	if out, err := DecodePayload(empty, 0); err != nil || len(out) != 0 {
		t.Fatalf("empty stream with EOS marker: %v", err)
	}
	if _, err := DecodePayload(empty, 1); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("empty stream with EOS marker, size 1: error = %v, want ErrCorrupt", err)
	}
}

// TestDecodeOverlongStream checks that decoding stops once the requested
// size is reached even though the stream goes on, as for blocks whose
// stream encodes more than the block length.
func TestDecodeOverlongStream(t *testing.T) {
	long := mixedData(17, 200000)
	payload := encodePayloadT(t, long, DefaultOptions())
	got, err := DecodePayload(payload, 155136)
	if err != nil || !bytes.Equal(got, long[:155136]) {
		t.Fatalf("DecodePayload(155136): %v", err)
	}

	// Any prefix of a stream decodes, including sizes that end within a
	// match.
	src, p := sample()
	stream, props := p[PropsLen:], p[:PropsLen]
	sizes := []int{1, 2, 3, 100, 1000, 4096, 30000, 60000, len(src) - 1}
	rng := rand.New(rand.NewPCG(3, 4))
	for range 50 {
		sizes = append(sizes, 1+rng.IntN(len(src)-1))
	}
	for _, size := range sizes {
		got, err := Decode(stream, props, size)
		if err != nil || !bytes.Equal(got, src[:size]) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	// The input only needs to hold the symbols that produce those bytes.
	cut := len(stream) * 2 / 3
	got, err = Decode(stream[:cut], props, len(src)/3)
	if err != nil || !bytes.Equal(got, src[:len(src)/3]) {
		t.Fatalf("prefix of the input: %v", err)
	}
}

func TestDecodeDistanceBeyondDictionary(t *testing.T) {
	// Matches reaching further back than the dictionary size announced in
	// the properties are rejected, even when the output buffer would hold
	// the data.
	src := bytes.Repeat(randomData(18, 6000), 3)
	p := checkRoundTrip(t, src, Options{Level: 9, DictSize: 1 << 16, LC: 3, PB: 2, FB: 273})
	binary.LittleEndian.PutUint32(p[1:], 4096)
	if _, err := DecodePayload(p, len(src)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("error = %v, want ErrCorrupt", err)
	}
}

func TestDecodeRepMatchFirst(t *testing.T) {
	// A first symbol that is a rep match (range coder code >= badRepCode)
	// is rejected up front.
	for _, code := range []uint32{0xBFFFFC00, 0xFFFFFFFF} {
		src := make([]byte, 64)
		binary.BigEndian.PutUint32(src[1:], code)
		if _, err := Decode(src, []byte{0x5d, 0, 0, 1, 0}, 10); !errors.Is(err, ErrCorrupt) {
			t.Errorf("code %#x: error = %v, want ErrCorrupt", code, err)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	src, payload := sample()
	n := len(src)
	props, stream := payload[:PropsLen], payload[PropsLen:]

	tests := []struct {
		name  string
		src   []byte
		props []byte
		size  int
		want  error
	}{
		{"short props", stream, props[:4], n, ErrInvalidProperties},
		{"nil props", stream, nil, n, ErrInvalidProperties},
		{"props byte 225", stream, []byte{225, 0, 0, 0, 2}, n, ErrInvalidProperties},
		{"props byte 255", stream, []byte{255, 0, 0, 0, 2}, n, ErrInvalidProperties},
		{"empty src", nil, props, n, ErrTruncated},
		{"4 byte src", stream[:4], props, 0, ErrTruncated},
		{"truncated", stream[:len(stream)/2], props, n, ErrTruncated},
		{"one byte short", stream[:len(stream)-1], props, n, ErrTruncated},
		{"size too large", stream, props, n + 1, ErrTruncated},
		{"first byte not zero", append([]byte{1}, stream[1:]...), props, n, ErrCorrupt},
		{"first byte not zero, size 0", []byte{1, 0, 0, 0, 0}, props, 0, ErrCorrupt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.src, tc.props, tc.size)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	if _, err := Decode(stream, props, -1); err == nil {
		t.Error("negative size accepted")
	}
	if _, err := DecodePayload(payload[:4], 0); !errors.Is(err, ErrInvalidProperties) {
		t.Errorf("DecodePayload(4 bytes) error = %v, want ErrInvalidProperties", err)
	}
	// Size 0 with a valid range coder start succeeds.
	if out, err := Decode(stream, props, 0); err != nil || len(out) != 0 {
		t.Errorf("Decode(size 0) = %x, %v", out, err)
	}
}

func TestDecodeInto(t *testing.T) {
	src, payload := sample()
	dst := make([]byte, len(src))
	if err := DecodeInto(dst, payload[PropsLen:], payload[:PropsLen]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst, src) {
		t.Fatal("mismatch")
	}
}

func TestDecodeCorruptNoPanic(t *testing.T) {
	src, payload := sample()
	rng := rand.New(rand.NewPCG(9, 10))
	detected := 0
	for range 300 {
		b := bytes.Clone(payload)
		i := PropsLen + 1 + rng.IntN(len(b)-PropsLen-1)
		b[i] ^= byte(1 << rng.IntN(8))
		got, err := DecodePayload(b, len(src))
		if err != nil {
			detected++
			continue
		}
		if len(got) != len(src) {
			t.Fatalf("flip at %d: %d bytes without error", i, len(got))
		}
		if !bytes.Equal(got, src) {
			detected++
		}
	}
	if detected == 0 {
		t.Fatal("no bit flip was detected")
	}

	garbage := randomData(19, 5000)
	garbage[0] = 0
	for _, p := range [][]byte{{0x5d, 0, 0, 1, 0}, {0xe0, 0, 0x10, 0, 0}, {0, 0, 0, 0, 0}} {
		if _, err := Decode(garbage, p, 1<<20); err == nil {
			t.Errorf("garbage decoded without error with props %x", p)
		}
	}
}

func TestConcurrent(t *testing.T) {
	data := mixedData(20, 16*65536+100000)
	want, payload := sample()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := data[i*65536 : i*65536+100000]
			p, err := EncodePayload(src, DefaultOptions())
			if err != nil {
				errs <- err
				return
			}
			got, err := DecodePayload(p, len(src))
			if err != nil || !bytes.Equal(got, src) {
				errs <- fmt.Errorf("goroutine %d: round trip failed: %v", i, err)
				return
			}
			if p2, _ := EncodePayload(src, DefaultOptions()); !bytes.Equal(p2, p) {
				errs <- fmt.Errorf("goroutine %d: concurrent encodes differ", i)
				return
			}
			got, err = DecodePayload(payload, len(want))
			if err != nil || !bytes.Equal(got, want) {
				errs <- fmt.Errorf("goroutine %d: shared payload decode failed: %v", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func FuzzDecode(f *testing.F) {
	_, payload := sample()
	f.Add(payload[:600], 1000)
	f.Add([]byte{0x5d, 0, 0, 1, 0, 0, 0, 0, 0, 0}, 0)
	for _, o := range []Options{
		{Level: 1, DictSize: 1 << 16, LC: 8, LP: 0, PB: 0, FB: 5},
		{Level: 9, DictSize: 1 << 20, LC: 0, LP: 4, PB: 4, FB: 273},
	} {
		p, err := EncodePayload(compressibleData(21, 16<<10), o)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(p[:min(len(p), 600)], 1000)
	}
	f.Add(encodeEndMark(f, compressibleData(22, 16<<10), DefaultOptions())[:2000], 5000)
	f.Add(encodeEndMark(f, nil, DefaultOptions()), 5000)
	f.Fuzz(func(t *testing.T, payload []byte, size int) {
		if size < 0 || size > 1<<20 {
			return
		}
		out, err := DecodePayload(payload, size)
		if err == nil && len(out) != size {
			t.Fatalf("decoded %d bytes without error, want %d", len(out), size)
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("hello hello hello"), uint8(3), uint8(0), uint8(2), uint8(9), uint16(273), uint8(16))
	f.Add([]byte{}, uint8(8), uint8(4), uint8(4), uint8(1), uint16(5), uint8(12))
	f.Add(bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 100), uint8(0), uint8(0), uint8(0), uint8(4), uint16(6), uint8(0))
	f.Fuzz(func(t *testing.T, src []byte, lc, lp, pb, level uint8, fb uint16, dictLog uint8) {
		o := Options{
			Level:    int(level % 10),
			DictSize: uint32(1) << (dictLog % 25),
			LC:       int(lc % 9),
			LP:       int(lp % 5),
			PB:       int(pb % 5),
			FB:       int(fb % 300),
		}
		payload, err := EncodePayload(src, o)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodePayload(payload, len(src))
		if err != nil || !bytes.Equal(got, src) {
			t.Fatalf("round trip failed: %v", err)
		}
	})
}

func benchmarkDecode(b *testing.B, src []byte) {
	p, err := EncodePayload(src, DefaultOptions())
	if err != nil {
		b.Fatal(err)
	}
	dst := make([]byte, len(src))
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for b.Loop() {
		if err := DecodeInto(dst, p[PropsLen:], p[:PropsLen]); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkEncode(b *testing.B, src []byte, o Options) {
	b.SetBytes(int64(len(src)))
	var n int
	for b.Loop() {
		p, err := EncodePayload(src, o)
		if err != nil {
			b.Fatal(err)
		}
		n = len(p)
	}
	b.ReportMetric(float64(n)/float64(len(src)), "ratio")
}

func BenchmarkDecodeData4M(b *testing.B) { benchmarkDecode(b, mixedData(1, 4<<20)) }
func BenchmarkDecodeData8M(b *testing.B) { benchmarkDecode(b, mixedData(1, 8<<20)) }
func BenchmarkDecodeAudio(b *testing.B)  { benchmarkDecode(b, audioData(1, 8<<20)) }
func BenchmarkDecodeRandom(b *testing.B) { benchmarkDecode(b, randomData(1, 1<<20)) }
func BenchmarkEncode(b *testing.B)       { benchmarkEncode(b, mixedData(1, 2<<20), DefaultOptions()) }
func BenchmarkEncodeData4M(b *testing.B) { benchmarkEncode(b, mixedData(1, 4<<20), DefaultOptions()) }
func BenchmarkEncodeAudio(b *testing.B)  { benchmarkEncode(b, audioData(1, 8<<20), DefaultOptions()) }
func BenchmarkEncodeRandom(b *testing.B) { benchmarkEncode(b, randomData(1, 1<<20), DefaultOptions()) }
func BenchmarkEncodeZero(b *testing.B)   { benchmarkEncode(b, make([]byte, 1<<20), DefaultOptions()) }
func BenchmarkEncode64K(b *testing.B) {
	benchmarkEncode(b, compressibleData(1, 64<<10), DefaultOptions())
}

func BenchmarkEncodeLevel1(b *testing.B) {
	benchmarkEncode(b, mixedData(1, 4<<20), Options{Level: 1, LC: -1, LP: -1, PB: -1, FB: -1})
}
