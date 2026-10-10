package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// Hand-built streams exercising decoder paths that neither EncodeRedbook
// nor other AaruFormat writers produce (variable block size, escape partitions,
// explicit sample rates, ...) plus malformed input.

type frameHeaderSpec struct {
	variable                        bool
	bsCode, srCode, chCode, bpsCode uint
	number                          uint64
	extra                           func(w *bitWriter) // block size / sample rate tails
	reservedBit                     bool
}

// buildFrame assembles a frame with correct CRCs around body.
func buildFrame(h frameHeaderSpec, body func(w *bitWriter)) []byte {
	var w bitWriter
	w.writeBits(frameSync, 14)
	w.writeBits(0, 1)
	if h.variable {
		w.writeBits(1, 1)
	} else {
		w.writeBits(0, 1)
	}
	w.writeBits(uint64(h.bsCode), 4)
	w.writeBits(uint64(h.srCode), 4)
	w.writeBits(uint64(h.chCode), 4)
	w.writeBits(uint64(h.bpsCode), 3)
	if h.reservedBit {
		w.writeBits(1, 1)
	} else {
		w.writeBits(0, 1)
	}
	writeUTF8(&w, h.number)
	if h.extra != nil {
		h.extra(&w)
	}
	w.writeBits(uint64(crc8(w.bytes())), 8)
	body(&w)
	w.padToByte()
	w.writeBits(uint64(crc16(w.bytes())), 16)
	return w.bytes()
}

// buildStream prepends "fLaC" and a STREAMINFO block to frames.
func buildStream(bps uint32, channels uint32, total uint64, frames ...[]byte) []byte {
	b := []byte("fLaC")
	b = append(b, 0x80|metaStreamInfo, 0, 0, streamInfoLen)
	b = binary.BigEndian.AppendUint16(b, 16)
	b = binary.BigEndian.AppendUint16(b, 4608)
	b = append(b, 0, 0, 0, 0, 0, 0)
	b = binary.BigEndian.AppendUint64(b, uint64(44100)<<44|uint64(channels-1)<<41|uint64(bps-1)<<36|total)
	b = append(b, make([]byte, 16)...)
	for _, f := range frames {
		b = append(b, f...)
	}
	return b
}

func verbatimSubframe(w *bitWriter, bps uint, samples ...int32) {
	w.writeBits(subframeVerbatim<<1, 8)
	for _, s := range samples {
		w.writeSigned(int64(s), bps)
	}
}

func pcmOf(l, r []int32) []byte {
	var b []byte
	for i := range l {
		b = binary.LittleEndian.AppendUint16(b, uint16(l[i]))
		b = binary.LittleEndian.AppendUint16(b, uint16(r[i]))
	}
	return b
}

func hint8(v uint64) func(w *bitWriter)  { return func(w *bitWriter) { w.writeBits(v, 8) } }
func hint16(v uint64) func(w *bitWriter) { return func(w *bitWriter) { w.writeBits(v, 16) } }

func TestDecodeVariableBlockSizeAndHeaderCodes(t *testing.T) {
	var wantL, wantR []int32
	var frames [][]byte

	// Frame 1: variable strategy, 8-bit block size, sample rate and bps
	// from STREAMINFO, independent stereo.
	l1, r1 := []int32{1, -2, 32767}, []int32{-32768, 5, 0}
	frames = append(frames, buildFrame(frameHeaderSpec{variable: true, bsCode: 6, chCode: 1, number: 0, extra: hint8(2)},
		func(w *bitWriter) { verbatimSubframe(w, 16, l1...); verbatimSubframe(w, 16, r1...) }))
	wantL, wantR = append(wantL, l1...), append(wantR, r1...)

	// Frame 2: 16-bit block size, sample rate in kHz, left/side.
	l2 := make([]int32, 300)
	r2 := make([]int32, 300)
	side2 := make([]int32, 300)
	for i := range l2 {
		l2[i] = int32(i*97%65536 - 32768)
		r2[i] = int32(32767 - i*31)
		side2[i] = l2[i] - r2[i]
	}
	frames = append(frames, buildFrame(frameHeaderSpec{variable: true, bsCode: 7, srCode: 12, chCode: 8, bpsCode: 4, number: 3,
		extra: func(w *bitWriter) { w.writeBits(299, 16); w.writeBits(44, 8) }},
		func(w *bitWriter) { verbatimSubframe(w, 16, l2...); verbatimSubframe(w, 17, side2...) }))
	wantL, wantR = append(wantL, l2...), append(wantR, r2...)

	// Frame 3: sample rate in Hz, right/side.
	l3, r3 := []int32{-32768}, []int32{32767}
	frames = append(frames, buildFrame(frameHeaderSpec{variable: true, bsCode: 6, srCode: 13, chCode: 9, bpsCode: 4, number: 303,
		extra: func(w *bitWriter) { w.writeBits(0, 8); w.writeBits(44100, 16) }},
		func(w *bitWriter) { verbatimSubframe(w, 17, l3[0]-r3[0]); verbatimSubframe(w, 16, r3...) }))
	wantL, wantR = append(wantL, l3...), append(wantR, r3...)

	// Frame 4: sample rate in tens of Hz, mid/side, a large sample number
	// needing a 6-byte UTF-8 code.
	l4, r4 := []int32{100, -7, 32767, -32768}, []int32{-101, 8, -32768, 32767}
	var mid4, side4 []int32
	for i := range l4 {
		mid4 = append(mid4, (l4[i]+r4[i])>>1)
		side4 = append(side4, l4[i]-r4[i])
	}
	frames = append(frames, buildFrame(frameHeaderSpec{variable: true, bsCode: 6, srCode: 14, chCode: 10, bpsCode: 4, number: 1 << 34,
		extra: func(w *bitWriter) { w.writeBits(3, 8); w.writeBits(4410, 16) }},
		func(w *bitWriter) { verbatimSubframe(w, 16, mid4...); verbatimSubframe(w, 17, side4...) }))
	wantL, wantR = append(wantL, l4...), append(wantR, r4...)

	stream := buildStream(16, 2, 0, frames...)
	got, err := DecodeRedbook(stream)
	if err != nil {
		t.Fatal(err)
	}
	if want := pcmOf(wantL, wantR); !bytes.Equal(got, want) {
		t.Fatalf("decoded % x\nwant    % x", got[:min(len(got), 32)], want[:32])
	}
	var stats decodeStats
	if err := decodeStream(stream, func(l, r []int32) {}, nil, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.variable != 4 || stats.assignments != [4]int{1, 1, 1, 1} {
		t.Errorf("stats %+v", stats)
	}
}

func TestDecodeEscapedPartitionsAndRice2(t *testing.T) {
	// Fixed order 2 subframe, 8 samples, partition order 1: partition 0
	// holds 2 residuals, partition 1 holds 4.
	warm := []int32{10, 20}
	res0 := []int32{0, 0}
	res1 := []int32{-16, 15, 3, -1}
	res2 := []int32{1000, -1000}
	want := func(res ...[]int32) []int32 {
		out := append([]int32(nil), warm...)
		for _, r := range res {
			out = append(out, r...)
		}
		restoreFixed(out, 2)
		return out
	}
	frame := buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 1, bpsCode: 4, extra: hint8(7)},
		func(w *bitWriter) {
			// Left: RICE, partition 0 escaped with 0 bits (all zero),
			// partition 1 escaped with 5 raw bits.
			w.writeBits((8|2)<<1, 8)
			for _, s := range warm {
				w.writeSigned(int64(s), 16)
			}
			w.writeBits(0, 2)
			w.writeBits(1, 4)
			w.writeBits(riceEscapeParam, 4)
			w.writeBits(0, 5)
			w.writeBits(riceEscapeParam, 4)
			w.writeBits(5, 5)
			for _, r := range res1 {
				w.writeSigned(int64(r), 5)
			}
			// Right: RICE2, partition 0 Rice parameter 20, partition 1
			// escaped with 12 bits.
			w.writeBits((8|2)<<1, 8)
			for _, s := range warm {
				w.writeSigned(int64(s), 16)
			}
			w.writeBits(1, 2)
			w.writeBits(1, 4)
			w.writeBits(20, 5)
			w.writeRiceBlock(res2, 20)
			w.writeBits(rice2EscapeParam, 5)
			w.writeBits(12, 5)
			for _, r := range res1 {
				w.writeSigned(int64(r), 12)
			}
		})
	stream := buildStream(16, 2, 8, frame)
	got, err := DecodeRedbook(stream)
	if err != nil {
		t.Fatal(err)
	}
	if exp := pcmOf(want(res0, res1), want(res2, res1)); !bytes.Equal(got, exp) {
		t.Fatalf("got  % x\nwant % x", got, exp)
	}
	var stats decodeStats
	if err := decodeStream(stream, func(l, r []int32) {}, nil, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.escapes != 3 || stats.rice2 != 1 {
		t.Errorf("stats %+v", stats)
	}
}

func TestDecodeLPCAndWastedBits(t *testing.T) {
	// LPC order 2, precision 4, shift 1, coefficients {3, -1}: prediction
	// (3*s[i-1] - s[i-2]) >> 1. Right channel: constant with 3 wasted bits.
	warm := []int32{4, 6}
	res := []int32{1, -2, 0, 5, -3, 2}
	left := append([]int32(nil), warm...)
	left = append(left, res...)
	restoreLPC(left, []int32{3, -1}, 1)
	frame := buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 1, bpsCode: 4, extra: hint8(7)},
		func(w *bitWriter) {
			w.writeBits((32|1)<<1, 8)
			for _, s := range warm {
				w.writeSigned(int64(s), 16)
			}
			w.writeBits(4-1, 4)
			w.writeSigned(1, 5)
			w.writeSigned(3, 4)
			w.writeSigned(-1, 4)
			w.writeBits(0, 2)
			w.writeBits(0, 4)
			w.writeBits(2, 4)
			w.writeRiceBlock(res, 2)
			// Constant -5 << 3 with wasted-bits flag, k-1 = 2 in unary.
			w.writeBits(subframeConstant<<1|1, 8)
			w.writeUnary(2)
			w.writeSigned(-5, 13)
		})
	got, err := DecodeRedbook(buildStream(16, 2, 8, frame))
	if err != nil {
		t.Fatal(err)
	}
	right := []int32{-40, -40, -40, -40, -40, -40, -40, -40}
	if exp := pcmOf(left, right); !bytes.Equal(got, exp) {
		t.Fatalf("got  % x\nwant % x", got, exp)
	}
}

func TestDecodeID3v2Prefix(t *testing.T) {
	pcm := musicPCM(10, 700)
	cmp, err := EncodeRedbook(pcm, DefaultEncoderOptions())
	if err != nil {
		t.Fatal(err)
	}
	tag := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x0a"), make([]byte, 10)...)
	got, err := DecodeRedbook(append(tag, cmp...))
	if err != nil || !bytes.Equal(got, pcm) {
		t.Fatalf("err=%v equal=%v", err, bytes.Equal(got, pcm))
	}
}

func TestDecodeUnsupportedFormats(t *testing.T) {
	mono := buildStream(16, 1, 2, buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 0, bpsCode: 4, extra: hint8(1)},
		func(w *bitWriter) { verbatimSubframe(w, 16, 1, 2) }))
	eight := buildStream(8, 2, 2, buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 1, bpsCode: 1, extra: hint8(1)},
		func(w *bitWriter) { verbatimSubframe(w, 8, 1, 2); verbatimSubframe(w, 8, 3, 4) }))
	wide := buildStream(16, 2, 2, buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 1, bpsCode: 6, extra: hint8(1)},
		func(w *bitWriter) { verbatimSubframe(w, 24, 1, 2); verbatimSubframe(w, 24, 3, 4) }))
	side33 := buildStream(32, 2, 2, buildFrame(frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 8, bpsCode: 7, extra: hint8(1)},
		func(w *bitWriter) { verbatimSubframe(w, 32, 1, 2) }))
	for name, s := range map[string][]byte{"mono": mono, "8-bit": eight, "24-bit": wide, "33-bit side": side33} {
		if _, err := DecodeRedbookInto(make([]byte, 64), s); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestDecodeMalformedFrames(t *testing.T) {
	ok := func(w *bitWriter) { verbatimSubframe(w, 16, 1, 2); verbatimSubframe(w, 16, 3, 4) }
	std := frameHeaderSpec{bsCode: 6, srCode: 9, chCode: 1, bpsCode: 4, extra: hint8(1)}
	with := func(mod func(h *frameHeaderSpec)) frameHeaderSpec { h := std; mod(&h); return h }
	fixed2 := func(method, partOrder uint64) func(w *bitWriter) {
		return func(w *bitWriter) {
			w.writeBits((8|1)<<1, 8)
			w.writeSigned(0, 16)
			w.writeBits(method, 2)
			w.writeBits(partOrder, 4)
			w.writeBits(0, 4)
			w.writeBits(1, 1)
			verbatimSubframe(w, 16, 3, 4)
		}
	}
	tests := map[string][]byte{
		"reserved block size":  buildFrame(with(func(h *frameHeaderSpec) { h.bsCode = 0 }), ok),
		"invalid sample rate":  buildFrame(with(func(h *frameHeaderSpec) { h.srCode = 15 }), ok),
		"reserved channels":    buildFrame(with(func(h *frameHeaderSpec) { h.chCode = 11 }), ok),
		"reserved bps":         buildFrame(with(func(h *frameHeaderSpec) { h.bpsCode = 3 }), ok),
		"reserved header bit":  buildFrame(with(func(h *frameHeaderSpec) { h.reservedBit = true }), ok),
		"bad 16-bit blocksize": buildFrame(with(func(h *frameHeaderSpec) { h.bsCode = 7; h.extra = hint16(0xFFFF) }), ok),
		"reserved subframe": buildFrame(std, func(w *bitWriter) {
			w.writeBits(2<<1, 8)
		}),
		"subframe pad bit": buildFrame(std, func(w *bitWriter) {
			w.writeBits(0x80|subframeVerbatim<<1, 8)
		}),
		"too many wasted bits": buildFrame(std, func(w *bitWriter) {
			w.writeBits(subframeConstant<<1|1, 8)
			w.writeUnary(15)
		}),
		"LPC precision 15": buildFrame(std, func(w *bitWriter) {
			w.writeBits(32<<1, 8)
			w.writeSigned(0, 16)
			w.writeBits(15, 4)
		}),
		"negative LPC shift": buildFrame(std, func(w *bitWriter) {
			w.writeBits(32<<1, 8)
			w.writeSigned(0, 16)
			w.writeBits(3, 4)
			w.writeSigned(-1, 5)
			w.writeSigned(1, 4)
		}),
		"reserved residual method": buildFrame(std, fixed2(2, 0)),
		"partition order too big":  buildFrame(std, fixed2(0, 2)),
		"order beyond block": buildFrame(std, func(w *bitWriter) {
			w.writeBits((8|4)<<1, 8)
		}),
		"non-zero frame padding": buildFrame(std, func(w *bitWriter) {
			verbatimSubframe(w, 16, 1, 2)
			verbatimSubframe(w, 16, 3, 4)
			w.writeBits(1, 1)
		}),
	}
	good := buildFrame(std, ok)
	// The header is 6 bytes (sync, codes, frame number, block size), then
	// the CRC-8.
	tests["header CRC"] = func() []byte { b := append([]byte(nil), good...); b[6] ^= 1; return b }()
	tests["frame CRC"] = func() []byte { b := append([]byte(nil), good...); b[len(b)-1] ^= 1; return b }()
	tests["no sync"] = append([]byte{0, 0}, good...)
	tests["truncated frame"] = good[:len(good)-1]
	tests["bad UTF-8 number"] = func() []byte {
		var w bitWriter
		w.writeBits(0xFFF8, 16)
		w.writeBits(0x69, 8)
		w.writeBits(0x08, 8)
		w.writeBits(0x80, 8) // a continuation byte cannot start a number
		return w.bytes()
	}()

	if _, err := DecodeRedbook(buildStream(16, 2, 2, good)); err != nil {
		t.Fatalf("control frame: %v", err)
	}
	for name, frame := range tests {
		n, err := DecodeRedbookInto(make([]byte, 64), buildStream(16, 2, 0, frame))
		if n != 0 || !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: n=%d err=%v, want ErrCorrupt", name, n, err)
		}
	}
}

func TestDecodeMalformedMetadata(t *testing.T) {
	good := buildStream(16, 2, 0)
	tests := map[string][]byte{
		"no STREAMINFO": []byte("fLaC\x81\x00\x00\x00"),
		"short STREAMINFO": func() []byte {
			b := append([]byte(nil), good...)
			b[7] = 33
			return b[:len(b)-1]
		}(),
		"block past end": func() []byte {
			b := append([]byte(nil), good...)
			b[4] = metaStreamInfo // not last
			return append(b, 0x81, 0, 1, 0)
		}(),
		"second STREAMINFO": func() []byte {
			b := append([]byte(nil), good...)
			b[4] = metaStreamInfo
			return append(b, append([]byte{0x80, 0, 0, 34}, make([]byte, 34)...)...)
		}(),
		"invalid block type": func() []byte {
			b := append([]byte(nil), good...)
			b[4] = metaStreamInfo
			return append(b, 0xFF, 0, 0, 0)
		}(),
		"missing header": good[:6],
	}
	for name, s := range tests {
		if n, err := DecodeRedbookInto(make([]byte, 64), s); n != 0 || !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: n=%d err=%v, want ErrCorrupt", name, n, err)
		}
	}
	// Unknown and padding blocks are skipped.
	b := append([]byte(nil), good...)
	b[4] = metaStreamInfo
	b = append(b, 0x01, 0, 0, 3, 1, 2, 3, 0x85, 0, 0, 1, 9)
	if _, err := DecodeRedbook(b); err != nil {
		t.Errorf("padding/unknown blocks: %v", err)
	}
}

// Data after the last frame is ignored once STREAMINFO's sample count has
// been decoded and is an error otherwise.
func TestDecodeTrailingData(t *testing.T) {
	pcm := musicPCM(11, 1000)
	cmp, err := EncodeRedbook(pcm, DefaultEncoderOptions())
	if err != nil {
		t.Fatal(err)
	}
	junk := append(append([]byte(nil), cmp...), 0, 1, 2, 3)
	if got, err := DecodeRedbook(junk); err != nil || !bytes.Equal(got, pcm) {
		t.Errorf("known length + junk: err=%v", err)
	}
	// Zero the STREAMINFO total sample count (36 bits ending at byte 26).
	unknown := append([]byte(nil), junk...)
	unknown[21] &= 0xF0
	unknown[22], unknown[23], unknown[24], unknown[25] = 0, 0, 0, 0
	if _, err := DecodeRedbook(unknown); !errors.Is(err, ErrCorrupt) {
		t.Errorf("unknown length + junk: err=%v, want ErrCorrupt", err)
	}
	if got, err := DecodeRedbook(unknown[:len(cmp)]); err != nil || !bytes.Equal(got, pcm) {
		t.Errorf("unknown length: err=%v", err)
	}
}

// Truncating a stream anywhere must fail cleanly, except at a frame
// boundary where the frames so far are returned.
func TestDecodeTruncation(t *testing.T) {
	pcm := musicPCM(12, 700)
	o := DefaultEncoderOptions()
	o.BlockSize = 256
	cmp, err := EncodeRedbook(pcm, o)
	if err != nil {
		t.Fatal(err)
	}
	_, framesStart, _ := parseStreamHeader(cmp)
	boundaries := map[int]int{framesStart: 0}
	{
		info, _, _ := parseStreamHeader(cmp)
		r := newBitReader(cmp)
		r.pos = framesStart
		d := frameDecoder{info: &info, r: r, src: cmp}
		samples := 0
		for r.remainingBits() > 0 {
			h, err := d.decodeFrame()
			if err != nil {
				t.Fatal(err)
			}
			samples += h.blockSize
			boundaries[r.bytePos()] = samples
		}
	}
	for l := range len(cmp) {
		out := make([]byte, len(pcm))
		n, err := DecodeRedbookInto(out, cmp[:l])
		if samples, ok := boundaries[l]; ok {
			if err != nil || n != samples*4 || !bytes.Equal(out[:n], pcm[:n]) {
				t.Errorf("cut at frame boundary %d: n=%d err=%v", l, n, err)
			}
			continue
		}
		if err == nil || n != 0 {
			t.Errorf("cut at %d: n=%d err=%v, want an error", l, n, err)
		}
	}
}

// Every single-bit error inside the frames is caught by the CRCs, and no
// corruption anywhere causes a panic.
func TestDecodeBitFlips(t *testing.T) {
	pcm := musicPCM(13, 600)
	o := DefaultEncoderOptions()
	o.BlockSize = 256
	cmp, err := EncodeRedbook(pcm, o)
	if err != nil {
		t.Fatal(err)
	}
	_, framesStart, _ := parseStreamHeader(cmp)
	buf := make([]byte, len(cmp))
	out := make([]byte, len(pcm))
	for bit := range len(cmp) * 8 {
		copy(buf, cmp)
		buf[bit/8] ^= 0x80 >> (bit % 8)
		n, err := DecodeRedbookInto(out, buf)
		if bit/8 >= framesStart && err == nil {
			t.Errorf("flip of bit %d (byte %d) undetected (n=%d)", bit, bit/8, n)
		}
	}
}

func FuzzDecodeRedbook(f *testing.F) {
	for _, seed := range []struct {
		pcm     []byte
		sectors int
	}{
		{musicPCM(14, 3), 1},
		{bytes.Repeat([]byte{0xDE}, 9408), 4},
		{musicPCM(15, 588), 1},
	} {
		o := DefaultEncoderOptions()
		o.BlockSize = BlockSizeForSectors(seed.sectors)
		if b, err := EncodeRedbook(seed.pcm, o); err == nil {
			f.Add(b)
		}
	}
	pcm := make([]byte, 4*300)
	x := xorshift(99)
	for i := range pcm {
		pcm[i] = byte(x.next() >> 24 & 0x0F)
	}
	o := DefaultEncoderOptions()
	o.BlockSize = 64
	if b, err := EncodeRedbook(pcm, o); err == nil {
		f.Add(b)
	}
	dst := make([]byte, 1<<16)
	f.Fuzz(func(t *testing.T, data []byte) {
		n, err := DecodeRedbookInto(dst, data)
		if err != nil && n != 0 {
			t.Fatalf("n=%d with error %v", n, err)
		}
	})
}
