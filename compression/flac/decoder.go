package flac

import (
	"encoding/binary"
	"fmt"
)

// Internal error values; all wrap one of the exported sentinels.
var (
	errCorrupt       = ErrCorrupt
	errTruncated     = fmt.Errorf("%w: unexpected end of stream", ErrCorrupt)
	errHeaderCRC     = fmt.Errorf("%w: frame header CRC-8 mismatch", ErrCorrupt)
	errFrameCRC      = fmt.Errorf("%w: frame CRC-16 mismatch", ErrCorrupt)
	errLostSync      = fmt.Errorf("%w: frame sync code not found", ErrCorrupt)
	errBadStreamInfo = fmt.Errorf("%w: missing or malformed STREAMINFO", ErrCorrupt)
)

const (
	metaStreamInfo  = 0
	metaApplication = 2
	metaInvalid     = 127

	streamInfoLen = 34

	maxLPCOrder         = 32
	maxFixedOrder       = 4
	maxRicePartOrder    = 15
	riceEscapeParam     = 15
	rice2EscapeParam    = 31
	minQLPCoeffPrec     = 5
	maxQLPCoeffPrec     = 15
	subframeConstant    = 0
	subframeVerbatim    = 1
	subframeFixed       = 2
	subframeLPC         = 3
	assignIndependent   = 0
	assignLeftSide      = 1
	assignRightSide     = 2
	assignMidSide       = 3
	frameSync           = 0x3FFE
	flacMinBlockSize    = 16
	flacMaxBlockSize    = 65535
	redbookSampleRate   = 44100
	redbookChannels     = 2
	redbookBitsPerSamp  = 16
	redbookBytesPerSamp = redbookChannels * redbookBitsPerSamp / 8
)

// streamInfo is the decoded STREAMINFO metadata block.
type streamInfo struct {
	minBlockSize, maxBlockSize uint32
	minFrameSize, maxFrameSize uint32
	sampleRate                 uint32
	channels                   uint32
	bitsPerSample              uint32
	totalSamples               uint64
	md5                        [16]byte
}

// frameHeader is a decoded frame header.
type frameHeader struct {
	blockSize     int
	sampleRate    uint32
	channels      int
	assignment    int // assignIndependent..assignMidSide
	bitsPerSample uint
}

// parseStreamHeader validates the "fLaC" marker, walks all metadata blocks
// and returns the STREAMINFO contents and the offset of the first frame.
func parseStreamHeader(src []byte) (streamInfo, int, error) {
	var info streamInfo
	pos := skipID3v2(src)
	if len(src)-pos < 4 || string(src[pos:pos+4]) != "fLaC" {
		return info, 0, ErrNotFLAC
	}
	pos += 4

	first := true
	for {
		if len(src)-pos < 4 {
			return info, 0, errTruncated
		}
		hdr := binary.BigEndian.Uint32(src[pos:])
		last := hdr>>31 != 0
		typ := hdr >> 24 & 0x7F
		length := int(hdr & 0xFFFFFF)
		pos += 4
		if len(src)-pos < length {
			return info, 0, errTruncated
		}
		body := src[pos : pos+length]
		pos += length

		switch {
		case first && (typ != metaStreamInfo || length != streamInfoLen):
			return info, 0, errBadStreamInfo
		case first:
			info = decodeStreamInfo(body)
		case typ == metaStreamInfo || typ == metaInvalid:
			return info, 0, fmt.Errorf("%w: unexpected metadata block type %d", ErrCorrupt, typ)
		}
		first = false
		if last {
			return info, pos, nil
		}
	}
}

// skipID3v2 returns the length of a leading ID3v2 tag, if any. FLAC
// decoders tolerate such tags in front of the "fLaC" marker.
func skipID3v2(src []byte) int {
	if len(src) < 10 || string(src[:3]) != "ID3" {
		return 0
	}
	size := 0
	for _, b := range src[6:10] {
		if b&0x80 != 0 {
			return 0
		}
		size = size<<7 | int(b)
	}
	size += 10
	if src[5]&0x10 != 0 {
		size += 10 // footer present
	}
	if size > len(src) {
		return 0
	}
	return size
}

func decodeStreamInfo(b []byte) streamInfo {
	var si streamInfo
	si.minBlockSize = uint32(binary.BigEndian.Uint16(b[0:]))
	si.maxBlockSize = uint32(binary.BigEndian.Uint16(b[2:]))
	si.minFrameSize = uint32(b[4])<<16 | uint32(b[5])<<8 | uint32(b[6])
	si.maxFrameSize = uint32(b[7])<<16 | uint32(b[8])<<8 | uint32(b[9])
	v := binary.BigEndian.Uint64(b[10:])
	si.sampleRate = uint32(v >> 44)
	si.channels = uint32(v>>41&7) + 1
	si.bitsPerSample = uint32(v>>36&0x1F) + 1
	si.totalSamples = v & (1<<36 - 1)
	copy(si.md5[:], b[18:34])
	return si
}

// frameDecoder decodes FLAC frames into per-channel sample buffers.
type frameDecoder struct {
	redbook bool // reject frames that are not 16-bit stereo
	info    *streamInfo
	r       *bitReader
	src     []byte
	samples [8][]int32
	stats   *decodeStats // optional, for tests
}

// decodeStats counts coding features seen while decoding (used by tests to
// check which code paths a stream exercises).
type decodeStats struct {
	frames      int
	assignments [4]int // by assignIndependent..assignMidSide
	subframes   [4]int // by subframeConstant..subframeLPC
	wasted      int    // subframes with wasted bits
	rice2       int    // residuals using 5-bit Rice parameters
	escapes     int    // escaped (raw) partitions
	variable    int    // frames of variable block size streams
	maxLPCOrder int
}

// readFrameHeader parses a frame header starting at the (byte-aligned)
// current position.
func (d *frameDecoder) readFrameHeader() (frameHeader, error) {
	var h frameHeader
	r := d.r
	start := r.bytePos()

	v, err := r.readBits(32)
	if err != nil {
		return h, err
	}
	if v>>18 != frameSync {
		return h, errLostSync
	}
	if v>>17&1 != 0 { // reserved bit
		return h, errLostSync
	}
	variable := v>>16&1 != 0
	if d.stats != nil && variable {
		d.stats.variable++
	}
	bsCode := v >> 12 & 0xF
	srCode := v >> 8 & 0xF
	chCode := v >> 4 & 0xF
	bpsCode := v >> 1 & 0x7
	if v&1 != 0 { // reserved bit
		return h, fmt.Errorf("%w: reserved frame header bit set", ErrCorrupt)
	}

	// Frame or sample number, UTF-8 style coded. Its value is not needed.
	maxBytes := 6
	if variable {
		maxBytes = 7
	}
	if err := d.skipUTF8Number(maxBytes); err != nil {
		return h, err
	}

	switch {
	case bsCode == 0:
		return h, fmt.Errorf("%w: reserved block size code", ErrCorrupt)
	case bsCode == 1:
		h.blockSize = 192
	case bsCode <= 5:
		h.blockSize = 576 << (bsCode - 2)
	case bsCode == 6:
		x, err := r.readBits(8)
		if err != nil {
			return h, err
		}
		h.blockSize = int(x) + 1
	case bsCode == 7:
		x, err := r.readBits(16)
		if err != nil {
			return h, err
		}
		if x == 0xFFFF {
			return h, fmt.Errorf("%w: invalid block size", ErrCorrupt)
		}
		h.blockSize = int(x) + 1
	default:
		h.blockSize = 256 << (bsCode - 8)
	}

	switch srCode {
	case 0:
		h.sampleRate = d.info.sampleRate
	case 12:
		x, err := r.readBits(8)
		if err != nil {
			return h, err
		}
		h.sampleRate = uint32(x) * 1000
	case 13:
		x, err := r.readBits(16)
		if err != nil {
			return h, err
		}
		h.sampleRate = uint32(x)
	case 14:
		x, err := r.readBits(16)
		if err != nil {
			return h, err
		}
		h.sampleRate = uint32(x) * 10
	case 15:
		return h, fmt.Errorf("%w: invalid sample rate code", ErrCorrupt)
	default:
		h.sampleRate = [...]uint32{0, 88200, 176400, 192000, 8000, 16000, 22050, 24000, 32000, 44100, 48000, 96000}[srCode]
	}

	switch {
	case chCode < 8:
		h.channels = int(chCode) + 1
		h.assignment = assignIndependent
	case chCode <= 10:
		h.channels = 2
		h.assignment = int(chCode) - 7
	default:
		return h, fmt.Errorf("%w: reserved channel assignment", ErrCorrupt)
	}

	switch bpsCode {
	case 0:
		h.bitsPerSample = uint(d.info.bitsPerSample)
	case 3:
		return h, fmt.Errorf("%w: reserved sample size code", ErrCorrupt)
	default:
		h.bitsPerSample = [...]uint{0, 8, 12, 0, 16, 20, 24, 32}[bpsCode]
	}

	end := r.bytePos()
	crc, err := r.readBits(8)
	if err != nil {
		return h, err
	}
	if uint8(crc) != crc8(d.src[start:end]) {
		return h, errHeaderCRC
	}
	return h, nil
}

// skipUTF8Number consumes an extended UTF-8 coded frame/sample number.
func (d *frameDecoder) skipUTF8Number(maxBytes int) error {
	b, err := d.r.readBits(8)
	if err != nil {
		return err
	}
	n := 0
	for mask := uint64(0x80); b&mask != 0 && mask != 0; mask >>= 1 {
		n++
	}
	switch {
	case n == 0:
		return nil
	case n == 1 || n > maxBytes:
		return fmt.Errorf("%w: invalid coded frame number", ErrCorrupt)
	}
	for range n - 1 {
		c, err := d.r.readBits(8)
		if err != nil {
			return err
		}
		if c&0xC0 != 0x80 {
			return fmt.Errorf("%w: invalid coded frame number", ErrCorrupt)
		}
	}
	return nil
}

// decodeFrame decodes one complete frame (header, subframes, footer) into
// d.samples and returns its header.
func (d *frameDecoder) decodeFrame() (frameHeader, error) {
	start := d.r.bytePos()
	h, err := d.readFrameHeader()
	if err != nil {
		return h, err
	}
	if h.bitsPerSample == 0 || h.bitsPerSample > 32 ||
		d.redbook && (h.channels != redbookChannels || h.bitsPerSample != redbookBitsPerSamp) {
		return h, fmt.Errorf("%w: %d channels, %d bits per sample", ErrUnsupported, h.channels, h.bitsPerSample)
	}
	for ch := range h.channels {
		if len(d.samples[ch]) < h.blockSize {
			d.samples[ch] = make([]int32, h.blockSize)
		}
		bps := h.bitsPerSample
		if h.assignment == assignLeftSide && ch == 1 ||
			h.assignment == assignRightSide && ch == 0 ||
			h.assignment == assignMidSide && ch == 1 {
			bps++
		}
		if bps > 32 {
			return h, fmt.Errorf("%w: 33-bit side channel", ErrUnsupported)
		}
		if err := d.decodeSubframe(d.samples[ch][:h.blockSize], bps); err != nil {
			return h, err
		}
	}

	if pad := d.r.alignToByte(); pad != 0 {
		return h, fmt.Errorf("%w: non-zero frame padding", ErrCorrupt)
	}
	end := d.r.bytePos()
	crc, err := d.r.readBits(16)
	if err != nil {
		return h, err
	}
	if uint16(crc) != crc16(d.src[start:end]) {
		return h, errFrameCRC
	}

	if d.stats != nil {
		d.stats.frames++
		d.stats.assignments[h.assignment]++
	}
	n := h.blockSize
	switch h.assignment {
	case assignLeftSide:
		l, s := d.samples[0][:n], d.samples[1][:n]
		for i := range l {
			s[i] = l[i] - s[i]
		}
	case assignRightSide:
		s, rt := d.samples[0][:n], d.samples[1][:n]
		for i := range s {
			s[i] += rt[i]
		}
	case assignMidSide:
		m, s := d.samples[0][:n], d.samples[1][:n]
		for i := range m {
			mid := m[i]<<1 | s[i]&1
			side := s[i]
			m[i] = (mid + side) >> 1
			s[i] = (mid - side) >> 1
		}
	}
	return h, nil
}

// decodeSubframe decodes one subframe of len(out) samples at bps bits.
func (d *frameDecoder) decodeSubframe(out []int32, bps uint) error {
	r := d.r
	x, err := r.readBits(8)
	if err != nil {
		return err
	}
	if x&0x80 != 0 {
		return fmt.Errorf("%w: subframe padding bit set", ErrCorrupt)
	}
	typ := x >> 1 & 0x3F
	var wasted uint
	if x&1 != 0 {
		k, err := r.readUnary(32)
		if err != nil {
			return err
		}
		wasted = uint(k) + 1
		if wasted >= bps {
			return fmt.Errorf("%w: too many wasted bits", ErrCorrupt)
		}
		bps -= wasted
		if d.stats != nil {
			d.stats.wasted++
		}
	}
	if d.stats != nil {
		switch {
		case typ == 0:
			d.stats.subframes[subframeConstant]++
		case typ == 1:
			d.stats.subframes[subframeVerbatim]++
		case typ >= 8 && typ <= 12:
			d.stats.subframes[subframeFixed]++
		case typ >= 32:
			d.stats.subframes[subframeLPC]++
			d.stats.maxLPCOrder = max(d.stats.maxLPCOrder, int(typ-31))
		}
	}

	switch {
	case typ == 0: // constant
		v, err := r.readSigned(bps)
		if err != nil {
			return err
		}
		for i := range out {
			out[i] = int32(v)
		}
	case typ == 1: // verbatim
		for i := range out {
			v, err := r.readSigned(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
	case typ >= 8 && typ <= 12: // fixed
		order := int(typ - 8)
		if order > len(out) {
			return fmt.Errorf("%w: predictor order exceeds block size", ErrCorrupt)
		}
		for i := range order {
			v, err := r.readSigned(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
		if err := d.readResidual(out, order); err != nil {
			return err
		}
		restoreFixed(out, order)
	case typ >= 32: // LPC
		order := int(typ-32) + 1
		if order > len(out) {
			return fmt.Errorf("%w: predictor order exceeds block size", ErrCorrupt)
		}
		for i := range order {
			v, err := r.readSigned(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
		p, err := r.readBits(4)
		if err != nil {
			return err
		}
		if p == 15 {
			return fmt.Errorf("%w: invalid QLP coefficient precision", ErrCorrupt)
		}
		precision := uint(p) + 1
		shift, err := r.readSigned(5)
		if err != nil {
			return err
		}
		if shift < 0 {
			return fmt.Errorf("%w: negative LPC shift", ErrCorrupt)
		}
		var coeffs [maxLPCOrder]int32
		for i := range order {
			c, err := r.readSigned(precision)
			if err != nil {
				return err
			}
			coeffs[i] = int32(c)
		}
		if err := d.readResidual(out, order); err != nil {
			return err
		}
		restoreLPC(out, coeffs[:order], uint(shift))
	default:
		return fmt.Errorf("%w: reserved subframe type %d", ErrCorrupt, typ)
	}

	if wasted > 0 {
		for i := range out {
			out[i] <<= wasted
		}
	}
	return nil
}

// readResidual reads the partitioned Rice coded residual of a predicted
// subframe directly into out[order:].
func (d *frameDecoder) readResidual(out []int32, order int) error {
	r := d.r
	x, err := r.readBits(6)
	if err != nil {
		return err
	}
	method := x >> 4
	partOrder := uint(x & 0xF)
	var paramBits uint
	var escape uint64
	switch method {
	case 0:
		paramBits, escape = 4, riceEscapeParam
	case 1:
		paramBits, escape = 5, rice2EscapeParam
		if d.stats != nil {
			d.stats.rice2++
		}
	default:
		return fmt.Errorf("%w: reserved residual coding method", ErrCorrupt)
	}
	blockSize := len(out)
	partSamples := blockSize >> partOrder
	if partSamples<<partOrder != blockSize || partSamples < order {
		return fmt.Errorf("%w: invalid residual partition order", ErrCorrupt)
	}
	pos := order
	for p := range 1 << partOrder {
		n := partSamples
		if p == 0 {
			n -= order
		}
		param, err := r.readBits(paramBits)
		if err != nil {
			return err
		}
		dst := out[pos : pos+n]
		if param == escape {
			if d.stats != nil {
				d.stats.escapes++
			}
			nb, err := r.readBits(5)
			if err != nil {
				return err
			}
			for i := range dst {
				v, err := r.readSigned(uint(nb))
				if err != nil {
					return err
				}
				dst[i] = int32(v)
			}
		} else if err := r.readRiceBlock(dst, uint(param)); err != nil {
			return err
		}
		pos += n
	}
	return nil
}

// restoreFixed reconstructs a fixed-predictor subframe in place; out[order:]
// holds the residual on entry.
func restoreFixed(out []int32, order int) {
	switch order {
	case 1:
		for i := 1; i < len(out); i++ {
			out[i] += out[i-1]
		}
	case 2:
		for i := 2; i < len(out); i++ {
			out[i] += 2*out[i-1] - out[i-2]
		}
	case 3:
		for i := 3; i < len(out); i++ {
			out[i] += 3*out[i-1] - 3*out[i-2] + out[i-3]
		}
	case 4:
		for i := 4; i < len(out); i++ {
			out[i] += 4*out[i-1] - 6*out[i-2] + 4*out[i-3] - out[i-4]
		}
	}
}

// restoreLPC reconstructs an LPC subframe in place; out[len(coeffs):] holds
// the residual on entry.
func restoreLPC(out []int32, coeffs []int32, shift uint) {
	order := len(coeffs)
	for i := order; i < len(out); i++ {
		var sum int64
		hist := out[i-order : i]
		for j, c := range coeffs {
			sum += int64(c) * int64(hist[order-1-j])
		}
		out[i] += int32(sum >> shift)
	}
}
