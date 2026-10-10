package flac

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"strconv"
	"testing"
)

// testOptions are a non-default parameter set: a single full-block Hann
// window (partial_tukey with zero parts degrades to tukey(1.0)) and no
// application block unless appID is given.
func testOptions(appID string) EncoderOptions {
	o := DefaultEncoderOptions()
	o.BlockSize = 4608
	o.Apodization = "partial_tukey(0/1.0/1.0)"
	o.MaxLPCOrder = 12
	o.QLPCoeffPrecision = 0
	o.QLPCoeffPrecSearch = true
	o.ExhaustiveModelSearch = false
	o.MinResidualPartitionOrder = 0
	o.MaxResidualPartitionOrder = 8
	o.ApplicationID = nil
	if appID != "" {
		o.ApplicationID = []byte(appID)
	}
	return o
}

// --- Basic codec tests ---

func TestDecodeGarbageReturnsZero(t *testing.T) {
	garbage := bytes.Repeat([]byte{0xAB}, 4096)
	out := make([]byte, 65536)
	n, err := DecodeRedbookInto(out, garbage)
	if n != 0 || err == nil {
		t.Fatalf("got n=%d err=%v, want 0 and an error", n, err)
	}
	if !errors.Is(err, ErrNotFLAC) {
		t.Errorf("err = %v, want ErrNotFLAC", err)
	}
}

func TestDecodeTruncatedFLACReturnsZero(t *testing.T) {
	truncated := make([]byte, 64)
	copy(truncated, "fLaC")
	out := make([]byte, 4096)
	n, err := DecodeRedbookInto(out, truncated)
	if n != 0 || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got n=%d err=%v, want 0 and ErrCorrupt", n, err)
	}
}

func TestDecodeEmptyInputReturnsZero(t *testing.T) {
	out := make([]byte, 4096)
	n, err := DecodeRedbookInto(out, nil)
	if n != 0 || err == nil {
		t.Fatalf("got n=%d err=%v, want 0 and an error", n, err)
	}
}

func TestDecodeZeroLengthReturnsZero(t *testing.T) {
	src := []byte{0}
	out := make([]byte, 4096)
	n, err := DecodeRedbookInto(out, src[:0])
	if n != 0 || err == nil {
		t.Fatalf("got n=%d err=%v, want 0 and an error", n, err)
	}
}

func TestEncodeZeroLengthNoCrash(t *testing.T) {
	out, err := EncodeRedbook(nil, testOptions(""))
	if err != nil {
		t.Fatal(err)
	}
	// A valid (empty) stream: header bytes only.
	if len(out) == 0 || len(out) >= 256 {
		t.Fatalf("encoded length %d, want 0 < n < 256", len(out))
	}
	pcm, err := DecodeRedbook(out)
	if err != nil || len(pcm) != 0 {
		t.Fatalf("decoding empty stream: %d bytes, %v", len(pcm), err)
	}
}

func TestEncodeGarbageRoundtrip(t *testing.T) {
	src := bytes.Repeat([]byte{0xDE}, 2352*4)
	cmp, err := EncodeRedbook(src, testOptions(""))
	if err != nil || len(cmp) == 0 {
		t.Fatalf("encode: %d bytes, %v", len(cmp), err)
	}
	decoded := make([]byte, len(src))
	n, err := DecodeRedbookInto(decoded, cmp)
	if err != nil || n != len(src) {
		t.Fatalf("decode: n=%d err=%v, want %d", n, err, len(src))
	}
	if !bytes.Equal(src, decoded) {
		t.Fatal("round trip mismatch")
	}
}

func TestDecodeDstTooSmall(t *testing.T) {
	src := make([]byte, 2352*4) // silence
	cmp, err := EncodeRedbook(src, testOptions(""))
	if err != nil || len(cmp) == 0 {
		t.Fatalf("encode: %d bytes, %v", len(cmp), err)
	}
	var tiny [64]byte
	n, err := DecodeRedbookInto(tiny[:], cmp)
	// The excess is discarded; it must never exceed dst.
	if n > len(tiny) {
		t.Fatalf("n = %d exceeds dst", n)
	}
	if err != nil || n != len(tiny) {
		t.Errorf("n=%d err=%v, want %d and nil", n, err, len(tiny))
	}
}

// musicSeconds returns the length of the synthetic music used by the long
// round trip tests.
func musicSeconds() int {
	if testing.Short() {
		return 5
	}
	return 30
}

// A long round trip of synthetic music with the parameters AaruFormat
// writers use and with testOptions: lossless, a well-formed stream, and the
// encoder uses LPC subframes and every stereo decorrelation mode.
func TestRoundTripMusic(t *testing.T) {
	pcm := musicPCM(1, 44100*musicSeconds())
	for _, o := range []EncoderOptions{DefaultEncoderOptions(), testOptions("Aaru.Compression.Native.Tests")} {
		cmp, err := EncodeRedbook(pcm, o)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]byte, len(pcm))
		n, err := DecodeRedbookInto(out, cmp)
		if err != nil || n != len(pcm) || !bytes.Equal(out, pcm) {
			t.Fatalf("round trip failed: n=%d err=%v", n, err)
		}
		checkStreamInfo(t, cmp, pcm, o)
		if whole, err := DecodeRedbook(cmp); err != nil || !bytes.Equal(whole, pcm) {
			t.Fatalf("DecodeRedbook: %v", err)
		}

		// The signal is tonal content plus at most 64 LSB of uniform noise
		// per channel (about 6 of 16 bits), so a working predictor must get
		// well below half the PCM size.
		if len(cmp) > len(pcm)/2 {
			t.Errorf("%d -> %d bytes (%.3f), want under 0.5", len(pcm), len(cmp), float64(len(cmp))/float64(len(pcm)))
		}

		var stats decodeStats
		if err := decodeStream(cmp, func(l, r []int32) {}, nil, &stats); err != nil {
			t.Fatal(err)
		}
		if stats.subframes[subframeLPC] == 0 {
			t.Errorf("expected LPC subframes, got %+v", stats)
		}
		for a, c := range stats.assignments {
			if c == 0 {
				t.Errorf("channel assignment %d never chosen: %+v", a, stats.assignments)
			}
		}
	}
}

// The stream starts with the "fLaC" marker followed by a STREAMINFO block
// header (type 0, length 34).
func TestStreamMarker(t *testing.T) {
	cmp, err := EncodeRedbook(musicPCM(2, 1000), DefaultEncoderOptions())
	if err != nil {
		t.Fatal(err)
	}
	if string(cmp[:4]) != "fLaC" || cmp[4]&0x7F != metaStreamInfo || cmp[5] != 0 || cmp[6] != 0 || cmp[7] != 34 {
		t.Fatalf("stream header % x", cmp[:8])
	}
	// The MD5 field is the RFC 1321 digest of the PCM input.
	info, _, err := parseStreamHeader(cmp)
	if err != nil {
		t.Fatal(err)
	}
	if info.md5 != md5.Sum(musicPCM(2, 1000)) {
		t.Fatal("STREAMINFO MD5 is not the MD5 of the input")
	}
}

// checkStreamInfo verifies the metadata EncodeRedbook writes.
func checkStreamInfo(t *testing.T, stream, pcm []byte, o EncoderOptions) {
	t.Helper()
	info, pos, err := parseStreamHeader(stream)
	if err != nil {
		t.Fatal(err)
	}
	total := uint64(len(pcm) / 4)
	if info.minBlockSize != uint32(o.BlockSize) || info.maxBlockSize != uint32(o.BlockSize) ||
		info.sampleRate != 44100 || info.channels != 2 || info.bitsPerSample != 16 ||
		info.totalSamples != total || info.md5 != md5.Sum(pcm) {
		t.Errorf("STREAMINFO %+v", info)
	}

	// Metadata layout: STREAMINFO, then the APPLICATION block whose ID is
	// zero and whose data is ApplicationID (the layout AaruFormat uses).
	want := 4 + 4 + streamInfoLen
	if len(o.ApplicationID) > 0 {
		app := stream[4+4+streamInfoLen:]
		l := 4 + len(o.ApplicationID)
		hdr := []byte{0x80 | metaApplication, byte(l >> 16), byte(l >> 8), byte(l), 0, 0, 0, 0}
		if !bytes.HasPrefix(app, append(hdr, o.ApplicationID...)) {
			t.Errorf("APPLICATION block % x", app[:min(len(app), 8+len(o.ApplicationID))])
		}
		want += 4 + l
		if stream[4]&0x80 != 0 {
			t.Error("STREAMINFO marked last despite APPLICATION block")
		}
	} else if stream[4]&0x80 == 0 {
		t.Error("STREAMINFO not marked last")
	}
	if pos != want {
		t.Errorf("frames start at %d, want %d", pos, want)
	}

	// Frame sizes: walk the frames and compare with min/max.
	r := newBitReader(stream)
	r.pos = pos
	d := frameDecoder{info: &info, r: r, src: stream}
	minSize, maxSize := 0, 0
	for r.remainingBits() > 0 {
		start := r.bytePos()
		if _, err := d.decodeFrame(); err != nil {
			t.Fatal(err)
		}
		size := r.bytePos() - start
		if minSize == 0 || size < minSize {
			minSize = size
		}
		maxSize = max(maxSize, size)
	}
	if info.minFrameSize != uint32(minSize) || info.maxFrameSize != uint32(maxSize) {
		t.Errorf("frame sizes %d..%d, STREAMINFO says %d..%d", minSize, maxSize, info.minFrameSize, info.maxFrameSize)
	}
}

// xorshift is a tiny deterministic generator for synthetic test audio.
type xorshift uint32

func (x *xorshift) next() uint32 {
	v := uint32(*x)
	v ^= v << 13
	v ^= v >> 17
	v ^= v << 5
	*x = xorshift(v)
	return v
}

func stereo(n int, f func(i int) (int16, int16)) []byte {
	b := make([]byte, 0, 4*n)
	for i := range n {
		l, r := f(i)
		b = binary.LittleEndian.AppendUint16(b, uint16(l))
		b = binary.LittleEndian.AppendUint16(b, uint16(r))
	}
	return b
}

// edgeInputs are awkward inputs: odd sizes, tiny blocks, silence, full
// scale noise and square waves, extreme stereo, wasted bits.
func edgeInputs(t *testing.T) map[string][]byte {
	audio := musicPCM(3, 420000)
	rng := xorshift(0x2545F491)
	m := map[string][]byte{}
	for _, n := range []int{1, 2, 3, 4, 5, 15, 16, 17, 31, 255, 256, 257} {
		m["audio_"+strconv.Itoa(n)] = audio[400000*4 : (400000+n)*4]
	}
	m["audio_13sectors"] = audio[:588*13*4]
	m["audio_odd"] = audio[4*12345 : 4*(12345+30011)]
	m["silence"] = make([]byte, 2352*4)
	m["dede"] = bytes.Repeat([]byte{0xDE}, 2352*4)
	m["noise"] = stereo(588*10, func(int) (int16, int16) { return int16(rng.next()), int16(rng.next()) })
	m["noise_tiny"] = stereo(7, func(int) (int16, int16) { return int16(rng.next()), int16(rng.next()) })
	m["square"] = stereo(5000, func(i int) (int16, int16) {
		if i&1 == 0 {
			return 32767, -32768
		}
		return -32768, 32767
	})
	m["extreme_const"] = stereo(5000, func(int) (int16, int16) { return 32767, -32768 })
	m["inverted"] = stereo(9000, func(i int) (int16, int16) {
		v := max(int16(binary.LittleEndian.Uint16(audio[4*(50000+i):])), -32767)
		return v, -v
	})
	m["left_only"] = stereo(9000, func(i int) (int16, int16) {
		return int16(binary.LittleEndian.Uint16(audio[4*i:])), 0
	})
	m["wasted"] = stereo(10000, func(i int) (int16, int16) {
		return int16(binary.LittleEndian.Uint16(audio[4*(70000+i):])) &^ 0xFF,
			int16(binary.LittleEndian.Uint16(audio[4*(70000+i)+2:])) &^ 0xFF
	})
	m["ramp"] = stereo(20000, func(i int) (int16, int16) { return int16(i * 3), int16(-i * 5) })
	// A predictable signal with a burst of full scale noise: the noisy
	// partitions need Rice parameters above 14 (RICE2).
	m["burst"] = stereo(9216, func(i int) (int16, int16) {
		l := int16(binary.LittleEndian.Uint16(audio[4*(90000+i):]))
		r := int16(binary.LittleEndian.Uint16(audio[4*(90000+i)+2:]))
		if i >= 3000 && i < 3100 {
			l, r = int16(rng.next()), int16(rng.next())
		}
		return l, r
	})
	return m
}

type optionVariant struct {
	name string
	mod  func(o *EncoderOptions)
}

var optionVariants = []optionVariant{
	{"default", func(o *EncoderOptions) {}},
	{"test", func(o *EncoderOptions) { *o = testOptions("Aaru.Compression.Native.Tests") }},
	{"bs588", func(o *EncoderOptions) { o.BlockSize = 588 }},
	{"bs256", func(o *EncoderOptions) { o.BlockSize = 256 }},
	{"bs16", func(o *EncoderOptions) { o.BlockSize = 16 }},
	{"bs1000", func(o *EncoderOptions) { o.BlockSize = 1000 }},
	{"bs4096", func(o *EncoderOptions) { o.BlockSize = 4096 }},
	{"bs65535", func(o *EncoderOptions) { o.BlockSize = 65535; o.MaxResidualPartitionOrder = 15 }},
	{"bs32768", func(o *EncoderOptions) { o.BlockSize = 32768; o.MaxResidualPartitionOrder = 15 }},
	{"exhaustive", func(o *EncoderOptions) { o.ExhaustiveModelSearch = true }},
	{"loose", func(o *EncoderOptions) { o.LooseMidSide = true; o.BlockSize = 1024 }},
	{"nomidside", func(o *EncoderOptions) { o.MidSide = false; o.ApplicationID = nil }},
	{"nolpc", func(o *EncoderOptions) { o.MaxLPCOrder = 0 }},
	{"lpc32", func(o *EncoderOptions) { o.MaxLPCOrder = 32; o.ExhaustiveModelSearch = true }},
	{"prec5", func(o *EncoderOptions) { o.QLPCoeffPrecSearch = false; o.QLPCoeffPrecision = 5 }},
	{"prec15", func(o *EncoderOptions) { o.QLPCoeffPrecSearch = false; o.QLPCoeffPrecision = 15; o.MaxLPCOrder = 32 }},
	{"part0", func(o *EncoderOptions) { o.MaxResidualPartitionOrder = 0 }},
	{"part4_8", func(o *EncoderOptions) { o.MinResidualPartitionOrder = 4 }},
	{"allwin", func(o *EncoderOptions) {
		o.Apodization = "bartlett;bartlett_hann;blackman;blackman_harris_4term_92db;connes;flattop;gauss(0.2);" +
			"hamming;hann;kaiser_bessel;nuttall;rectangle;triangle;tukey(0.5);welch"
	}},
	{"multitukey", func(o *EncoderOptions) { o.Apodization = "tukey(5e-1);partial_tukey(2);punchout_tukey(3)" }},
	{"sequential", func(o *EncoderOptions) { o.Workers = 1; o.BlockSize = 256 }},
}

// Every awkward input round-trips bit-exactly under every option set.
func TestRoundTripEdgeCases(t *testing.T) {
	inputs := edgeInputs(t)
	var all decodeStats
	for name, pcm := range inputs {
		for _, v := range optionVariants {
			if testing.Short() && v.name != "default" && v.name != "bs16" {
				continue
			}
			o := DefaultEncoderOptions()
			v.mod(&o)
			cmp, err := EncodeRedbook(pcm, o)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, v.name, err)
			}
			out := make([]byte, len(pcm))
			n, err := DecodeRedbookInto(out, cmp)
			if err != nil || n != len(pcm) || !bytes.Equal(out, pcm) {
				t.Fatalf("%s/%s: round trip failed: n=%d err=%v", name, v.name, n, err)
			}
			if err := decodeStream(cmp, func(l, r []int32) {}, nil, &all); err != nil {
				t.Fatal(err)
			}
			if v.name == "default" || v.name == "bs1000" {
				checkStreamInfo(t, cmp, pcm, o)
			}
		}
	}
	// The matrix must exercise every coding feature the encoder has.
	for k, c := range all.subframes {
		if c == 0 {
			t.Errorf("subframe kind %d never produced: %+v", k, all)
		}
	}
	if all.wasted == 0 || all.rice2 == 0 {
		t.Errorf("wasted bits / RICE2 never produced: %+v", all)
	}
}

// Silence and the 0xDE pattern collapse to constant subframes.
func TestConstantSubframes(t *testing.T) {
	for _, pcm := range [][]byte{make([]byte, 2352*4), bytes.Repeat([]byte{0xDE}, 2352*4)} {
		cmp, err := EncodeRedbook(pcm, DefaultEncoderOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(cmp) > 100 {
			t.Errorf("constant input compressed to %d bytes", len(cmp))
		}
	}
}

// Full-scale noise must not expand by more than the per-frame overhead
// (verbatim subframes are the fallback).
func TestNoiseDoesNotExplode(t *testing.T) {
	rng := xorshift(7)
	pcm := stereo(4608*4, func(int) (int16, int16) { return int16(rng.next()), int16(rng.next()) })
	cmp, err := EncodeRedbook(pcm, DefaultEncoderOptions())
	if err != nil {
		t.Fatal(err)
	}
	if limit := len(pcm) + 4*32 + 64; len(cmp) > limit {
		t.Errorf("noise %d -> %d bytes", len(pcm), len(cmp))
	}
}

// The output must not depend on the number of workers.
func TestWorkersDeterministic(t *testing.T) {
	pcm := musicPCM(4, 4608*37+1234)
	var ref []byte
	for _, w := range []int{1, 2, 3, 8, 0} {
		o := DefaultEncoderOptions()
		o.Workers = w
		cmp, err := EncodeRedbook(pcm, o)
		if err != nil {
			t.Fatal(err)
		}
		if ref == nil {
			ref = cmp
		} else if !bytes.Equal(ref, cmp) {
			t.Errorf("Workers=%d output differs from Workers=1", w)
		}
	}
}

// Streams shaped like the ones AaruFormat writers produce: block sizes from
// BlockSizeForSectors, the "Aaru" application block, very short blocks and
// constant data. Plus a stream with LPC orders above 12 and partition order
// up to 15 from an exhaustive search.
func TestWriterStyleStreams(t *testing.T) {
	audio := musicPCM(5, 588*1001+20000)
	lpc32 := bytes.Clone(audio[4*10000 : 4*19000])
	clear(lpc32[4*2000 : 4*2600])
	x := xorshift(0x12345678)
	for i := 4 * 5000; i < 4*5400; i++ {
		lpc32[i] = byte(x.next())
	}
	writer := func(sectors int, app string) EncoderOptions {
		o := DefaultEncoderOptions()
		o.BlockSize = BlockSizeForSectors(sectors)
		o.ApplicationID = []byte(app)
		return o
	}
	high := DefaultEncoderOptions()
	high.MaxLPCOrder = 32
	high.ExhaustiveModelSearch = true
	high.MaxResidualPartitionOrder = 15
	high.ApplicationID = nil
	tests := []struct {
		name string
		pcm  []byte
		opts EncoderOptions
	}{
		{"13sectors", audio[:588*13*4], writer(13, "Aaru")},
		{"1sector", audio[4*588*1000 : 4*588*1001], writer(1, "Aaru")},
		{"3samples", audio[4*300000 : 4*300003], writer(1, "Aaru")},
		{"dede", bytes.Repeat([]byte{0xDE}, 9408), writer(4, "Aaru.Compression.Native.Tests")},
		{"lpc32", lpc32, high},
	}
	for _, tc := range tests {
		src, err := EncodeRedbook(tc.pcm, tc.opts)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]byte, len(tc.pcm))
		n, err := DecodeRedbookInto(out, src)
		if err != nil || n != len(tc.pcm) || !bytes.Equal(out, tc.pcm) {
			t.Errorf("%s: n=%d err=%v, want exact %d bytes", tc.name, n, err, len(tc.pcm))
		}
		if tc.opts.BlockSize <= len(tc.pcm)/4 || tc.name == "lpc32" {
			checkStreamInfo(t, src, tc.pcm, tc.opts)
		}
		var stats decodeStats
		if err := decodeStream(src, func(l, r []int32) {}, nil, &stats); err != nil {
			t.Fatal(err)
		}
		if tc.name == "lpc32" && stats.maxLPCOrder <= 12 {
			t.Errorf("%s: no high order LPC subframes: %+v", tc.name, stats)
		}
		if tc.name == "dede" && stats.subframes[subframeConstant] == 0 {
			t.Errorf("%s: no constant subframes: %+v", tc.name, stats)
		}
	}
}

// A destination that is not a whole number of samples is filled exactly
// (no whole 4-byte sample may be written past its end).
func TestDecodePartialSample(t *testing.T) {
	pcm := musicPCM(6, 1000)
	cmp, err := EncodeRedbook(pcm, DefaultEncoderOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 2, 3, 5, 6, 7, 3999} {
		out := make([]byte, size+8)
		n, err := DecodeRedbookInto(out[:size], cmp)
		if err != nil || n != size || !bytes.Equal(out[:size], pcm[:size]) {
			t.Errorf("size %d: n=%d err=%v", size, n, err)
		}
		if !bytes.Equal(out[size:], make([]byte, 8)) {
			t.Errorf("size %d: wrote past dst", size)
		}
	}
	// A larger dst is filled with what the stream holds.
	out := make([]byte, len(pcm)+100)
	if n, err := DecodeRedbookInto(out, cmp); err != nil || n != len(pcm) {
		t.Errorf("oversized dst: n=%d err=%v", n, err)
	}
}

func TestEncodeInvalid(t *testing.T) {
	if _, err := EncodeRedbook(make([]byte, 6), DefaultEncoderOptions()); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("odd length: %v", err)
	}
	bad := []func(o *EncoderOptions){
		func(o *EncoderOptions) { o.BlockSize = 15 },
		func(o *EncoderOptions) { o.BlockSize = 65536 },
		func(o *EncoderOptions) { o.BlockSize = -1 },
		func(o *EncoderOptions) { o.MaxLPCOrder = 33 },
		func(o *EncoderOptions) { o.MaxLPCOrder = -1 },
		func(o *EncoderOptions) { o.BlockSize = 16; o.MaxLPCOrder = 17 },
		func(o *EncoderOptions) { o.QLPCoeffPrecision = 4 },
		func(o *EncoderOptions) { o.QLPCoeffPrecision = 16 },
		func(o *EncoderOptions) { o.MinResidualPartitionOrder = -1 },
		func(o *EncoderOptions) { o.MaxResidualPartitionOrder = -1 },
		func(o *EncoderOptions) { o.Workers = -1 },
	}
	for i, mod := range bad {
		o := DefaultEncoderOptions()
		mod(&o)
		if _, err := EncodeRedbook(make([]byte, 64), o); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("case %d: err = %v, want ErrInvalidOptions", i, err)
		}
	}

	// Values the encoder adjusts rather than rejects.
	o := DefaultEncoderOptions()
	o.BlockSize = 0
	o.MaxResidualPartitionOrder = 99
	o.MinResidualPartitionOrder = 50
	pcm := musicPCM(7, 10000)
	cmp, err := EncodeRedbook(pcm, o)
	if err != nil {
		t.Fatal(err)
	}
	if info, _, _ := parseStreamHeader(cmp); info.maxBlockSize != 4096 {
		t.Errorf("default block size %d, want 4096", info.maxBlockSize)
	}
	if got, err := DecodeRedbook(cmp); err != nil || !bytes.Equal(got, pcm) {
		t.Errorf("round trip: %v", err)
	}
}

func TestBlockSizeForSectors(t *testing.T) {
	for sectors, want := range map[int]int{0: 256, 1: 588, 2: 1176, 7: 4116, 8: 4608, 64: 4608} {
		if got := BlockSizeForSectors(sectors); got != want {
			t.Errorf("BlockSizeForSectors(%d) = %d, want %d", sectors, got, want)
		}
	}
}

func BenchmarkEncodeRedbook(b *testing.B) {
	pcm := musicPCM(8, 4608*100)
	for _, w := range []int{1, 0} {
		o := DefaultEncoderOptions()
		o.Workers = w
		b.Run("workers="+strconv.Itoa(w), func(b *testing.B) {
			b.SetBytes(int64(len(pcm)))
			for b.Loop() {
				if _, err := EncodeRedbook(pcm, o); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeRedbook(b *testing.B) {
	pcm := musicPCM(9, 44100*30)
	src, err := EncodeRedbook(pcm, DefaultEncoderOptions())
	if err != nil {
		b.Fatal(err)
	}
	out := make([]byte, len(pcm))
	b.SetBytes(int64(len(pcm)))
	for b.Loop() {
		if _, err := DecodeRedbookInto(out, src); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzEncodeRoundTrip(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4}, uint8(0x11), uint16(4608))
	f.Add(bytes.Repeat([]byte{0x7F, 0xFF, 0x00, 0x80}, 100), uint8(0xFF), uint16(0))
	f.Add(bytes.Repeat([]byte{0x00, 0x01, 0x00, 0xFF}, 300), uint8(0xC9), uint16(100))
	f.Fuzz(func(t *testing.T, data []byte, flags uint8, bs uint16) {
		data = data[:len(data)&^3]
		o := DefaultEncoderOptions()
		o.BlockSize = 16 + int(bs)%1000
		o.MidSide = flags&1 != 0
		o.LooseMidSide = flags&2 != 0
		o.ExhaustiveModelSearch = flags&4 != 0
		o.QLPCoeffPrecSearch = flags&8 != 0
		o.MaxLPCOrder = min(int(flags>>4)*2, o.BlockSize)
		o.Workers = 1 + int(flags>>6)
		cmp, err := EncodeRedbook(data, o)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]byte, len(data))
		n, err := DecodeRedbookInto(out, cmp)
		if err != nil || n != len(data) || !bytes.Equal(out, data) {
			t.Fatalf("round trip failed: n=%d err=%v", n, err)
		}
	})
}
