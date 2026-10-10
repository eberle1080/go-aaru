package zstd

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	kzstd "github.com/klauspost/compress/zstd"
)

// randomData returns n deterministic pseudo-random (incompressible) bytes.
func randomData(seed uint64, n int) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x7a737464))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// compressibleData returns n deterministic bytes that resemble program text:
// words from a small vocabulary, punctuation, numbers and occasional random
// bytes, so the encoder finds both literals and matches.
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

// encodeT is Encode with test failure on error.
func encodeT(t testing.TB, src []byte, level int) []byte {
	t.Helper()
	enc, err := Encode(src, level)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// Frames built by hand from the Zstandard frame format (RFC 8878): magic,
// a single-segment frame header with a one-byte content size, and one last
// block that is either raw or RLE.
func TestDecodeHandBuiltFrames(t *testing.T) {
	magic := []byte{0x28, 0xB5, 0x2F, 0xFD}
	blockHeader := func(last bool, typ, size int) []byte {
		v := typ<<1 | size<<3
		if last {
			v |= 1
		}
		return []byte{byte(v), byte(v >> 8), byte(v >> 16)}
	}
	frame := func(fcs byte, blocks ...[]byte) []byte {
		f := append(bytes.Clone(magic), 0x20, fcs) // Single_Segment, FCS 1 byte
		for _, b := range blocks {
			f = append(f, b...)
		}
		return f
	}

	raw := frame(5, blockHeader(true, 0, 5), []byte("hello"))
	if got, err := Decode(raw, 5); err != nil || string(got) != "hello" {
		t.Errorf("raw block: %q, %v", got, err)
	}

	rle := frame(100, blockHeader(true, 1, 100), []byte{0xAB})
	if got, err := Decode(rle, 100); err != nil || !bytes.Equal(got, bytes.Repeat([]byte{0xAB}, 100)) {
		t.Errorf("RLE block: %x, %v", got, err)
	}

	two := frame(8, blockHeader(false, 0, 3), []byte("abc"), blockHeader(true, 1, 5), []byte{'z'})
	if got, err := Decode(two, 8); err != nil || string(got) != "abczzzzz" {
		t.Errorf("raw + RLE blocks: %q, %v", got, err)
	}

	empty := frame(0, blockHeader(true, 0, 0))
	if got, err := Decode(empty, 0); err != nil || len(got) != 0 {
		t.Errorf("empty frame: %x, %v", got, err)
	}

	// Two hand-built frames back to back.
	if got, err := Decode(append(bytes.Clone(raw), rle...), 105); err != nil || string(got[:5]) != "hello" || got[104] != 0xAB {
		t.Errorf("concatenated hand-built frames: %v", err)
	}

	// Reserved block type 3 is invalid.
	if _, err := Decode(frame(5, blockHeader(true, 3, 5), []byte("hello")), 5); !errors.Is(err, ErrCorrupt) {
		t.Errorf("reserved block type: %v, want ErrCorrupt", err)
	}
	// A raw block shorter than its header claims.
	if _, err := Decode(frame(5, blockHeader(true, 0, 5), []byte("hel")), 5); err == nil {
		t.Error("truncated raw block decoded without error")
	}
}

// Encoding is deterministic: the same input and level give the same frame.
func TestEncodeDeterministic(t *testing.T) {
	src := compressibleData(1, 200<<10)
	for _, level := range []int{1, 3, 7, DefaultLevel} {
		a, b := encodeT(t, src, level), encodeT(t, src, level)
		if !bytes.Equal(a, b) {
			t.Errorf("level %d: two encodings differ", level)
		}
	}
}

func TestDecodeWrongSize(t *testing.T) {
	src := encodeT(t, compressibleData(2, 64<<10), DefaultLevel)
	for _, size := range []int{0, 1, 64<<10 - 1, 64<<10 + 1, 1 << 20} {
		if _, err := Decode(src, size); !errors.Is(err, ErrSizeMismatch) {
			t.Errorf("Decode(size %d) error = %v, want ErrSizeMismatch", size, err)
		}
	}
	if _, err := Decode(src, -1); err == nil {
		t.Error("Decode(size -1) succeeded")
	}
}

func TestDecodeCorrupt(t *testing.T) {
	src := encodeT(t, compressibleData(2, 64<<10), DefaultLevel)

	// Truncations at many points must fail cleanly, never panic.
	for _, n := range []int{1, 4, 5, 8, 20, 100, len(src) / 2, len(src) - 1} {
		if _, err := Decode(src[:n], 64<<10); err == nil {
			t.Errorf("truncated to %d bytes: no error", n)
		}
	}

	// Bad magic.
	bad := append([]byte(nil), src...)
	bad[0] ^= 0xFF
	if _, err := Decode(bad, 64<<10); !errors.Is(err, ErrCorrupt) {
		t.Errorf("bad magic: error = %v, want ErrCorrupt", err)
	}

	// Random bit flips in the body must never panic. (Without a content
	// checksum some flips decode to different data; AaruFormat's block
	// CRC64 catches those.)
	rng := rand.New(rand.NewPCG(5, 6))
	for range 200 {
		b := append([]byte(nil), src...)
		i := 4 + rng.IntN(len(b)-4)
		b[i] ^= byte(1 << rng.IntN(8))
		got, err := Decode(b, 64<<10)
		if err == nil && len(got) != 64<<10 {
			t.Fatalf("flip at %d: %d bytes without error", i, len(got))
		}
	}

	// Pure garbage.
	garbage := make([]byte, 1000)
	for i := range garbage {
		garbage[i] = byte(rng.Uint32())
	}
	if _, err := Decode(garbage, 1000); err == nil {
		t.Error("garbage decoded without error")
	}
}

func TestDecodeEmptyInput(t *testing.T) {
	// An empty buffer decodes to 0 bytes.
	got, err := Decode(nil, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("Decode(nil, 0) = %x, %v", got, err)
	}
	if _, err := Decode(nil, 10); err == nil {
		t.Fatal("Decode(nil, 10) succeeded")
	}
}

func TestConcatenatedFrames(t *testing.T) {
	// Decode accepts concatenated frames.
	pa, pb := compressibleData(3, 16<<10), randomData(4, 4<<10)
	a := encodeT(t, pa, 1)
	b := encodeT(t, pb, DefaultLevel)
	want := append(bytes.Clone(pa), pb...)
	got, err := Decode(append(bytes.Clone(a), b...), len(want))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("concatenated frames decoded incorrectly")
	}
}

func TestRoundTripLevels(t *testing.T) {
	src := compressibleData(5, 256<<10)
	prev := 0
	for _, level := range []int{-5, 0, 1, 2, 3, 5, 6, 9, 10, 19, 22, 100} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			enc, err := Encode(src, level)
			if err != nil {
				t.Fatal(err)
			}
			dec, err := Decode(enc, len(src))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dec, src) {
				t.Fatal("round trip mismatch")
			}
			if len(enc) >= len(src) {
				t.Errorf("no compression: %d >= %d", len(enc), len(src))
			}
			// The frame must record the content size.
			var h kzstd.Header
			if err := h.Decode(enc); err != nil || !h.HasFCS || h.FrameContentSize != uint64(len(src)) {
				t.Errorf("frame header %+v, err %v", h, err)
			}
			if h.HasCheckSum {
				t.Error("frame has a content checksum; AaruFormat frames carry none")
			}
			if level >= 1 && prev > 0 && len(enc) > prev*11/10 {
				t.Errorf("level %d (%d bytes) much worse than a lower level (%d bytes)", level, len(enc), prev)
			}
			if level >= 1 {
				prev = len(enc)
			}
		})
	}
}

func TestRoundTripShapes(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	random := make([]byte, 300<<10)
	for i := range random {
		random[i] = byte(rng.Uint32())
	}
	inputs := map[string][]byte{
		"one":    {0x42},
		"zeros":  make([]byte, 1<<20),
		"random": random,
		"text":   bytes.Repeat([]byte("AaruFormat zstd round trip. "), 5000),
		"code":   compressibleData(6, 3<<20),
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			enc, err := Encode(src, DefaultLevel)
			if err != nil {
				t.Fatal(err)
			}
			dec, err := Decode(enc, len(src))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dec, src) {
				t.Fatal("round trip mismatch")
			}
		})
	}
}

func TestEncodeEmpty(t *testing.T) {
	// Encode emits a complete (9-byte) frame for empty input.
	enc, err := Encode(nil, DefaultLevel)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) == 0 {
		t.Fatal("empty input produced no frame")
	}
	dec, err := Decode(enc, 0)
	if err != nil || len(dec) != 0 {
		t.Fatalf("Decode = %x, %v", dec, err)
	}
}

func TestAppendEncode(t *testing.T) {
	src := compressibleData(7, 4096)
	prefix := []byte("prefix")
	out, err := AppendEncode(append([]byte(nil), prefix...), src, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, prefix) {
		t.Fatal("prefix lost")
	}
	dec, err := Decode(out[len(prefix):], len(src))
	if err != nil || !bytes.Equal(dec, src) {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestDecodeInto(t *testing.T) {
	want := compressibleData(8, 64<<10)
	src := encodeT(t, want, DefaultLevel)

	dst := make([]byte, len(want))
	if err := DecodeInto(dst, src); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst, want) {
		t.Fatal("DecodeInto mismatch")
	}

	// Extra capacity must not let the decoder accept a larger output.
	big := make([]byte, len(want)-1, len(want)+100)
	if err := DecodeInto(big, src); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("DecodeInto with short len error = %v, want ErrSizeMismatch", err)
	}
}

func TestConcurrent(t *testing.T) {
	src := compressibleData(9, 128<<10)
	shared := encodeT(t, src[:64<<10], DefaultLevel)
	sharedWant := src[:64<<10]
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			level := []int{1, 3, 7, 19}[i%4]
			chunk := src[(i%8)*1024:]
			enc, err := Encode(chunk, level)
			if err != nil {
				errs <- err
				return
			}
			dec, err := Decode(enc, len(chunk))
			if err != nil || !bytes.Equal(dec, chunk) {
				errs <- fmt.Errorf("goroutine %d: round trip failed: %v", i, err)
				return
			}
			dec, err = Decode(shared, len(sharedWant))
			if err != nil || !bytes.Equal(dec, sharedWant) {
				errs <- fmt.Errorf("goroutine %d: shared frame decode failed: %v", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestEncoderLevelMapping(t *testing.T) {
	tests := []struct {
		level int
		want  kzstd.EncoderLevel
	}{
		{-3, kzstd.SpeedFastest},
		{0, kzstd.SpeedFastest},
		{1, kzstd.SpeedFastest},
		{2, kzstd.SpeedFastest},
		{3, kzstd.SpeedDefault},
		{5, kzstd.SpeedDefault},
		{6, kzstd.SpeedBetterCompression},
		{9, kzstd.SpeedBetterCompression},
		{10, kzstd.SpeedBestCompression},
		{DefaultLevel, kzstd.SpeedBestCompression},
		{MaxLevel, kzstd.SpeedBestCompression},
	}
	for _, tc := range tests {
		if got := encoderLevel(tc.level); got != tc.want {
			t.Errorf("encoderLevel(%d) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	src := compressibleData(10, 2<<20)
	enc, err := Encode(src, DefaultLevel)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := Decode(enc, len(src)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	src := compressibleData(10, 2<<20)
	for _, level := range []int{1, 3, 7, DefaultLevel} {
		b.Run(fmt.Sprint(level), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			for b.Loop() {
				if _, err := Encode(src, level); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
