package flac

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
)

// Bit sizes of fixed subframe fields.
const (
	subframeHeaderBits = 1 + 6 + 1 // zero pad, type, wasted-bits flag
	riceParamBits      = 4
	rice2ParamBits     = 5
	residualHeaderBits = 2 + 4 // coding method, partition order
	qlpPrecisionBits   = 4
	qlpShiftBits       = 5
)

// normalizedOptions are EncoderOptions after validation and the standard
// FLAC defaulting rules.
type normalizedOptions struct {
	blockSize      int
	midSide        bool
	looseMidSide   bool
	apodizations   []apodization
	maxLPCOrder    int
	qlpPrecision   uint
	precSearch     bool
	exhaustive     bool
	minPartOrder   int
	maxPartOrder   int
	application    []byte
	hasApplication bool
	workers        int
}

func (o EncoderOptions) normalize() (normalizedOptions, error) {
	n := normalizedOptions{
		blockSize:    o.BlockSize,
		midSide:      o.MidSide,
		looseMidSide: o.MidSide && o.LooseMidSide,
		apodizations: parseApodization(o.Apodization),
		maxLPCOrder:  o.MaxLPCOrder,
		precSearch:   o.QLPCoeffPrecSearch,
		exhaustive:   o.ExhaustiveModelSearch,
		minPartOrder: o.MinResidualPartitionOrder,
		maxPartOrder: o.MaxResidualPartitionOrder,
	}
	if n.maxLPCOrder < 0 || n.maxLPCOrder > maxLPCOrder {
		return n, fmt.Errorf("%w: MaxLPCOrder %d not in [0,%d]", ErrInvalidOptions, o.MaxLPCOrder, maxLPCOrder)
	}
	if n.blockSize == 0 {
		n.blockSize = 4096
		if n.maxLPCOrder == 0 {
			n.blockSize = 1152
		}
	}
	if n.blockSize < flacMinBlockSize || n.blockSize > flacMaxBlockSize {
		return n, fmt.Errorf("%w: BlockSize %d not in [%d,%d]", ErrInvalidOptions, o.BlockSize, flacMinBlockSize, flacMaxBlockSize)
	}
	if n.blockSize < n.maxLPCOrder {
		return n, fmt.Errorf("%w: BlockSize %d smaller than MaxLPCOrder %d", ErrInvalidOptions, n.blockSize, n.maxLPCOrder)
	}
	switch p := o.QLPCoeffPrecision; {
	case p == 0:
		// The standard automatic choice for 16-bit audio.
		switch bs := n.blockSize; {
		case bs <= 192:
			n.qlpPrecision = 7
		case bs <= 384:
			n.qlpPrecision = 8
		case bs <= 576:
			n.qlpPrecision = 9
		case bs <= 1152:
			n.qlpPrecision = 10
		case bs <= 2304:
			n.qlpPrecision = 11
		case bs <= 4608:
			n.qlpPrecision = 12
		default:
			n.qlpPrecision = 13
		}
	case p < minQLPCoeffPrec || p > maxQLPCoeffPrec:
		return n, fmt.Errorf("%w: QLPCoeffPrecision %d not 0 or in [%d,%d]", ErrInvalidOptions, p, minQLPCoeffPrec, maxQLPCoeffPrec)
	default:
		n.qlpPrecision = uint(p)
	}
	if n.minPartOrder < 0 || n.maxPartOrder < 0 {
		return n, fmt.Errorf("%w: negative residual partition order", ErrInvalidOptions)
	}
	n.maxPartOrder = min(n.maxPartOrder, maxRicePartOrder)
	switch {
	case o.Workers < 0:
		return n, fmt.Errorf("%w: negative Workers", ErrInvalidOptions)
	case o.Workers == 0:
		n.workers = runtime.GOMAXPROCS(0)
	default:
		n.workers = o.Workers
	}
	n.minPartOrder = min(n.minPartOrder, n.maxPartOrder)
	if len(o.ApplicationID) > 0 {
		if len(o.ApplicationID) > 1<<24-1-4 {
			return n, fmt.Errorf("%w: ApplicationID too long", ErrInvalidOptions)
		}
		n.application = o.ApplicationID
		n.hasApplication = true
	}
	return n, nil
}

// subframe is a candidate encoding of one channel of one frame.
type subframe struct {
	kind      int
	order     int
	value     int32 // constant subframes
	precision uint  // LPC
	shift     int   // LPC
	coeffs    [maxLPCOrder]int32
	partOrder uint
	rice2     bool
	params    []uint32
	residual  []int32
}

// channelState holds the per-signal working data of a frame.
type channelState struct {
	signal []int32 // after removing wasted bits
	bps    uint    // effective bits per sample of signal
	wasted uint
	ws     [2]subframe
	best   int
	bits   int
}

type encoder struct {
	o normalizedOptions
	w bitWriter

	frameNumber uint64

	// Signals: 0 left, 1 right, 2 mid, 3 side.
	ch [4]channelState

	windows       map[int][][]float64
	windowed      []float64
	autoc         [maxLPCOrder + 1]float64
	lp            [maxLPCOrder][maxLPCOrder]float64
	lpErr         [maxLPCOrder]float64
	partSums      []uint64
	partParams    [2][]uint32
	looseFrames   int
	looseCount    int
	lastAssign    int
	minFrameBytes int
	maxFrameBytes int
}

func newEncoder(o normalizedOptions) *encoder {
	e := &encoder{
		o:       o,
		windows: make(map[int][][]float64),
	}
	bs := o.blockSize
	for i := range e.ch {
		e.ch[i].signal = make([]int32, bs)
		for j := range e.ch[i].ws {
			e.ch[i].ws[j].params = make([]uint32, 1<<o.maxPartOrder)
			e.ch[i].ws[j].residual = make([]int32, bs)
		}
	}
	e.windowed = make([]float64, bs)
	e.partSums = make([]uint64, 2<<o.maxPartOrder)
	e.partParams[0] = make([]uint32, 1<<o.maxPartOrder)
	e.partParams[1] = make([]uint32, 1<<o.maxPartOrder)
	e.looseFrames = int(float64(redbookSampleRate)*0.4/float64(bs) + 0.5)
	if e.looseFrames == 0 {
		e.looseFrames = 1
	}
	e.minFrameBytes = -1
	return e
}

// encodeStream produces the complete FLAC stream for pcm.
//
// Frames are independent, so (except in loose mid/side mode, whose stereo
// decisions depend on previous frames) contiguous runs of frames are
// encoded concurrently by separate encoders and concatenated in order. The
// output does not depend on the number of workers.
func encodeStream(pcm []byte, o normalizedOptions) []byte {
	total := len(pcm) / redbookBytesPerSamp
	frames := (total + o.blockSize - 1) / o.blockSize

	// Metadata: STREAMINFO (filled in at the end) and the optional
	// APPLICATION block. The "application id" is stored as the APPLICATION
	// block *data*, as in existing AaruFormat images; the 4-byte registered
	// ID stays zero.
	out := make([]byte, 0, len(pcm)*3/4+64+len(o.application))
	out = append(out, "fLaC"...)
	lastFlag := byte(0x80)
	if o.hasApplication {
		lastFlag = 0
	}
	out = append(out, lastFlag|metaStreamInfo, 0, 0, streamInfoLen)
	siPos := len(out)
	out = append(out, make([]byte, streamInfoLen)...)
	if o.hasApplication {
		l := 4 + len(o.application)
		out = append(out, 0x80|metaApplication, byte(l>>16), byte(l>>8), byte(l), 0, 0, 0, 0)
		out = append(out, o.application...)
	}

	workers := min(o.workers, frames/minFramesPerWorker)
	if o.looseMidSide {
		workers = 1
	}
	minFrame, maxFrame := -1, 0
	var sum [md5.Size]byte
	if workers <= 1 {
		e := newEncoder(o)
		e.w.buf = out
		e.encodeFrames(pcm, 0, frames)
		out = e.w.bytes()
		minFrame, maxFrame = e.minFrameBytes, e.maxFrameBytes
		sum = md5.Sum(pcm)
	} else {
		encs := make([]*encoder, workers)
		var wg sync.WaitGroup
		wg.Go(func() { sum = md5.Sum(pcm) })
		for i := range workers {
			first, last := frames*i/workers, frames*(i+1)/workers
			e := newEncoder(o)
			encs[i] = e
			wg.Go(func() {
				span := (min(last*o.blockSize, total) - first*o.blockSize) * redbookBytesPerSamp
				e.w.buf = make([]byte, 0, span*3/4+64)
				e.encodeFrames(pcm, first, last)
			})
		}
		wg.Wait()
		for _, e := range encs {
			out = append(out, e.w.bytes()...)
			if minFrame < 0 || e.minFrameBytes < minFrame {
				minFrame = e.minFrameBytes
			}
			maxFrame = max(maxFrame, e.maxFrameBytes)
		}
	}
	if minFrame < 0 {
		minFrame = 0 // no frames: unknown
	}

	si := out[siPos : siPos+streamInfoLen]
	binary.BigEndian.PutUint16(si[0:], uint16(o.blockSize))
	binary.BigEndian.PutUint16(si[2:], uint16(o.blockSize))
	si[4], si[5], si[6] = byte(minFrame>>16), byte(minFrame>>8), byte(minFrame)
	si[7], si[8], si[9] = byte(maxFrame>>16), byte(maxFrame>>8), byte(maxFrame)
	v := uint64(redbookSampleRate)<<44 | uint64(redbookChannels-1)<<41 | uint64(redbookBitsPerSamp-1)<<36 | uint64(total)
	binary.BigEndian.PutUint64(si[10:], v)
	copy(si[18:], sum[:])
	return out
}

// minFramesPerWorker is the least number of frames worth handing to a
// separate goroutine.
const minFramesPerWorker = 4

// encodeFrames encodes frames [first, last) of pcm into e.w.
func (e *encoder) encodeFrames(pcm []byte, first, last int) {
	total := len(pcm) / redbookBytesPerSamp
	e.frameNumber = uint64(first)
	for f := first; f < last; f++ {
		start := f * e.o.blockSize
		n := min(e.o.blockSize, total-start)
		e.encodeFrame(pcm[start*redbookBytesPerSamp:(start+n)*redbookBytesPerSamp], n)
	}
}

// encodeFrame encodes n interleaved stereo samples from pcm as one frame.
func (e *encoder) encodeFrame(pcm []byte, n int) {
	left, right := e.ch[0].signal[:n], e.ch[1].signal[:n]
	for i := range n {
		left[i] = int32(int16(binary.LittleEndian.Uint16(pcm[4*i:])))
		right[i] = int32(int16(binary.LittleEndian.Uint16(pcm[4*i+2:])))
	}

	doIndependent, doMidSide := true, e.o.midSide
	if e.o.looseMidSide && e.looseCount > 0 {
		doIndependent = e.lastAssign == assignIndependent
		doMidSide = !doIndependent
	}
	if doMidSide {
		mid, side := e.ch[2].signal[:n], e.ch[3].signal[:n]
		for i := range n {
			mid[i] = (left[i] + right[i]) >> 1
			side[i] = left[i] - right[i]
		}
	}

	// Partition orders are limited by how often n can be halved.
	maxPart := min(e.o.maxPartOrder, bits.TrailingZeros(uint(n)))
	minPart := min(e.o.minPartOrder, maxPart)

	for c := range e.ch {
		if c < 2 && !doIndependent || c >= 2 && !doMidSide {
			continue
		}
		cs := &e.ch[c]
		cs.wasted = wastedBits(cs.signal[:n])
		cs.bps = redbookBitsPerSamp - cs.wasted
		if c == 3 {
			cs.bps++ // side channel needs one extra bit
		}
		cs.best, cs.bits = e.processSubframe(cs, n, minPart, maxPart)
	}

	assign := assignIndependent
	if doMidSide {
		if !doIndependent {
			assign = assignMidSide
		} else {
			b := [4]int{
				e.ch[0].bits + e.ch[1].bits,
				e.ch[0].bits + e.ch[3].bits,
				e.ch[1].bits + e.ch[3].bits,
				e.ch[2].bits + e.ch[3].bits,
			}
			for a := 1; a < 4; a++ {
				if b[a] < b[assign] {
					assign = a
				}
			}
		}
	}
	chans := [...][2]int{
		assignIndependent: {0, 1},
		assignLeftSide:    {0, 3},
		assignRightSide:   {3, 1},
		assignMidSide:     {2, 3},
	}[assign]

	w := &e.w
	start := len(w.bytes())
	e.writeFrameHeader(n, assign)
	for _, c := range chans {
		cs := &e.ch[c]
		e.writeSubframe(&cs.ws[cs.best], cs.signal[:n], cs.bps, cs.wasted)
	}
	w.padToByte()
	w.writeBits(uint64(crc16(w.bytes()[start:])), 16)

	size := len(w.bytes()) - start
	if e.minFrameBytes < 0 || size < e.minFrameBytes {
		e.minFrameBytes = size
	}
	e.maxFrameBytes = max(e.maxFrameBytes, size)

	if e.o.looseMidSide {
		e.looseCount++
		if e.looseCount >= e.looseFrames {
			e.looseCount = 0
		}
	}
	e.lastAssign = assign
	e.frameNumber++
}

// wastedBits removes and returns the number of trailing zero bits common to
// all samples of sig (0 for an all-zero signal).
func wastedBits(sig []int32) uint {
	var x int32
	for _, s := range sig {
		x |= s
		if x&1 != 0 {
			return 0
		}
	}
	if x == 0 {
		return 0
	}
	shift := uint(bits.TrailingZeros32(uint32(x)))
	for i := range sig {
		sig[i] >>= shift
	}
	return shift
}

func (e *encoder) writeFrameHeader(n, assign int) {
	w := &e.w
	start := len(w.bytes())
	w.writeBits(frameSync, 14)
	w.writeBits(0, 1) // reserved
	w.writeBits(0, 1) // fixed block size stream

	var code, hintBits uint
	switch n {
	case 192:
		code = 1
	case 576, 1152, 2304, 4608:
		code = 2 + uint(bits.TrailingZeros(uint(n/576)))
	case 256, 512, 1024, 2048, 4096, 8192, 16384, 32768:
		code = 8 + uint(bits.TrailingZeros(uint(n/256)))
	default:
		if n <= 256 {
			code, hintBits = 6, 8
		} else {
			code, hintBits = 7, 16
		}
	}
	w.writeBits(uint64(code), 4)
	w.writeBits(9, 4) // 44.1 kHz
	switch assign {
	case assignIndependent:
		w.writeBits(redbookChannels-1, 4)
	default:
		w.writeBits(uint64(7+assign), 4)
	}
	w.writeBits(4, 3) // 16 bits per sample
	w.writeBits(0, 1) // reserved
	writeUTF8(w, e.frameNumber)
	if hintBits > 0 {
		w.writeBits(uint64(n-1), hintBits)
	}
	w.writeBits(uint64(crc8(w.bytes()[start:])), 8)
}

// writeUTF8 writes v using FLAC's extended UTF-8 coding (up to 36 bits).
func writeUTF8(w *bitWriter, v uint64) {
	if v < 0x80 {
		w.writeBits(v, 8)
		return
	}
	n := 2 // total bytes
	for v >= 1<<(5*n+1) && n < 7 {
		n++
	}
	lead := uint64(0xFF00>>n) & 0xFF
	w.writeBits(lead|v>>(6*(n-1)), 8)
	for i := n - 2; i >= 0; i-- {
		w.writeBits(0x80|v>>(6*i)&0x3F, 8)
	}
}

func (e *encoder) writeSubframe(sf *subframe, sig []int32, bps, wasted uint) {
	w := &e.w
	var typ uint64
	switch sf.kind {
	case subframeConstant:
		typ = 0
	case subframeVerbatim:
		typ = 1
	case subframeFixed:
		typ = 8 | uint64(sf.order)
	case subframeLPC:
		typ = 32 | uint64(sf.order-1)
	}
	flag := uint64(0)
	if wasted > 0 {
		flag = 1
	}
	w.writeBits(typ<<1|flag, 8)
	if wasted > 0 {
		w.writeUnary(wasted - 1)
	}

	switch sf.kind {
	case subframeConstant:
		w.writeSigned(int64(sf.value), bps)
	case subframeVerbatim:
		for _, s := range sig {
			w.writeSigned(int64(s), bps)
		}
	case subframeFixed:
		for _, s := range sig[:sf.order] {
			w.writeSigned(int64(s), bps)
		}
		e.writeResidual(sf, len(sig))
	case subframeLPC:
		for _, s := range sig[:sf.order] {
			w.writeSigned(int64(s), bps)
		}
		w.writeBits(uint64(sf.precision-1), qlpPrecisionBits)
		w.writeSigned(int64(sf.shift), qlpShiftBits)
		for _, c := range sf.coeffs[:sf.order] {
			w.writeSigned(int64(c), sf.precision)
		}
		e.writeResidual(sf, len(sig))
	}
}

func (e *encoder) writeResidual(sf *subframe, n int) {
	w := &e.w
	pbits := uint(riceParamBits)
	if sf.rice2 {
		w.writeBits(1, 2)
		pbits = rice2ParamBits
	} else {
		w.writeBits(0, 2)
	}
	w.writeBits(uint64(sf.partOrder), 4)
	partSamples := n >> sf.partOrder
	pos := 0
	for p := range 1 << sf.partOrder {
		cnt := partSamples
		if p == 0 {
			cnt -= sf.order
		}
		k := sf.params[p]
		w.writeBits(uint64(k), pbits)
		w.writeRiceBlock(sf.residual[pos:pos+cnt], uint(k))
		pos += cnt
	}
}

// processSubframe finds the smallest encoding of cs.signal[:n]. It returns
// the index of the winning workspace and its size in bits.
func (e *encoder) processSubframe(cs *channelState, n, minPart, maxPart int) (int, int) {
	sig := cs.signal[:n]
	bps := cs.bps
	base := subframeHeaderBits + int(cs.wasted)

	best := 0
	cs.ws[0].kind = subframeVerbatim
	bestBits := base + n*int(bps)

	try := func(bits int) {
		if bits < bestBits {
			best = 1 - best
			bestBits = bits
		}
	}

	if isConstant(sig) {
		c := &cs.ws[1-best]
		c.kind = subframeConstant
		c.value = sig[0]
		try(base + int(bps))
		return best, bestBits
	}
	if n <= maxFixedOrder {
		return best, bestBits
	}

	var fixedBits [maxFixedOrder + 1]float64
	guess := fixedBestPredictor(sig, &fixedBits)
	minFixed, maxFixed := guess, guess
	if e.o.exhaustive {
		minFixed, maxFixed = 0, maxFixedOrder
	}
	maxFixed = min(maxFixed, n-1)
	for order := minFixed; order <= maxFixed; order++ {
		if fixedBits[order] >= float64(bps) {
			continue
		}
		c := &cs.ws[1-best]
		c.kind = subframeFixed
		c.order = order
		fixedResidual(sig, order, c.residual)
		resBits := e.findBestPartitioning(c, n, minPart, maxPart)
		try(base + order*int(bps) + resBits)
	}

	maxOrder := min(e.o.maxLPCOrder, n-1)
	if maxOrder <= 0 {
		return best, bestBits
	}
	windowed := e.windowed[:n]
	for _, win := range e.windowsFor(n) {
		for i, s := range sig {
			windowed[i] = float64(s) * win[i]
		}
		autoc := e.autoc[:maxOrder+1]
		autocorrelation(windowed, autoc)
		if autoc[0] == 0 {
			continue
		}
		maxOrderThis := computeLPCoefficients(autoc, maxOrder, &e.lp, e.lpErr[:])
		minOrder := 1
		if !e.o.exhaustive {
			overhead := bps + e.o.qlpPrecision
			if e.o.precSearch {
				overhead = bps + minQLPCoeffPrec
			}
			g := bestLPCOrder(e.lpErr[:], maxOrderThis, n, overhead)
			minOrder, maxOrderThis = g, g
		}
		for order := minOrder; order <= maxOrderThis; order++ {
			if expectedBitsPerResidualSample(e.lpErr[order-1], 0.5/float64(n-order)) >= float64(bps) {
				continue
			}
			// Keep the coefficient precision small enough that decoding
			// needs only 32-bit arithmetic.
			limit := uint(maxQLPCoeffPrec)
			if bps <= 17 {
				limit = min(limit, 32-bps-ilog2(uint(order)))
			}
			minPrec, maxPrec := e.o.qlpPrecision, e.o.qlpPrecision
			if e.o.precSearch {
				minPrec, maxPrec = minQLPCoeffPrec, max(limit, minQLPCoeffPrec)
			}
			for prec := minPrec; prec <= maxPrec; prec++ {
				p := min(prec, limit)
				c := &cs.ws[1-best]
				shift, ok := quantizeCoefficients(e.lp[order-1][:order], p, c.coeffs[:order])
				if !ok {
					continue
				}
				if !lpcResidual(sig, c.coeffs[:order], uint(shift), c.residual) {
					continue
				}
				c.kind = subframeLPC
				c.order = order
				c.precision = p
				c.shift = shift
				resBits := e.findBestPartitioning(c, n, minPart, maxPart)
				try(base + qlpPrecisionBits + qlpShiftBits + order*int(p+bps) + resBits)
			}
		}
	}
	return best, bestBits
}

func isConstant(sig []int32) bool {
	for _, s := range sig[1:] {
		if s != sig[0] {
			return false
		}
	}
	return true
}

// windowsFor returns the analysis windows for a block of n samples.
func (e *encoder) windowsFor(n int) [][]float64 {
	if ws, ok := e.windows[n]; ok {
		return ws
	}
	ws := make([][]float64, len(e.o.apodizations))
	for i, a := range e.o.apodizations {
		ws[i] = make([]float64, n)
		computeWindow(ws[i], a)
	}
	e.windows[n] = ws
	return ws
}

// findBestPartitioning chooses the Rice partition order and parameters for
// sf.residual (n-sf.order samples) and returns the estimated residual size
// in bits.
func (e *encoder) findBestPartitioning(sf *subframe, n, minPart, maxPart int) int {
	order := sf.order
	res := sf.residual[:n-order]
	for maxPart > 0 && n>>maxPart <= order {
		maxPart--
	}
	minPart = min(minPart, maxPart)

	// Sums of absolute residuals at the finest partitioning, then merged
	// pairwise for each coarser order.
	parts := 1 << maxPart
	partSamples := n >> maxPart
	sums := e.partSums
	pos := 0
	for p := range parts {
		cnt := partSamples
		if p == 0 {
			cnt -= order
		}
		var s uint64
		for _, r := range res[pos : pos+cnt] {
			s += uint64(abs32(r))
		}
		sums[p] = s
		pos += cnt
	}
	from, to := 0, parts
	for po := maxPart - 1; po >= minPart; po-- {
		for i := range 1 << po {
			sums[to+i] = sums[from+2*i] + sums[from+2*i+1]
		}
		from += 2 << po
		to += 1 << po
	}

	bestBits := -1
	bestIdx := 0
	bestOrder := 0
	offset := 0
	for po := maxPart; po >= minPart; po-- {
		cur := 1 - bestIdx
		params := e.partParams[cur]
		total := residualHeaderBits
		rice2 := false
		ps := n >> po
		for p := range 1 << po {
			cnt := ps
			if p == 0 {
				cnt -= order
			}
			k, b := riceParameter(sums[offset+p], cnt)
			if k >= riceEscapeParam {
				rice2 = true
			}
			params[p] = uint32(k)
			total += b
		}
		if rice2 {
			total += 1 << po
		}
		if bestBits < 0 || total < bestBits {
			bestBits = total
			bestIdx = cur
			bestOrder = po
		}
		offset += 1 << po
	}

	sf.partOrder = uint(bestOrder)
	copy(sf.params, e.partParams[bestIdx][:1<<bestOrder])
	sf.rice2 = false
	for _, k := range sf.params[:1<<bestOrder] {
		if k >= riceEscapeParam {
			sf.rice2 = true
			break
		}
	}
	return bestBits
}

func abs32(v int32) uint32 {
	if v < 0 {
		return uint32(-v)
	}
	return uint32(v)
}

// riceParameter picks the Rice parameter for a partition of cnt residuals
// whose absolute values sum to sum, and estimates the partition size in
// bits (including the 4-bit parameter).
func riceParameter(sum uint64, cnt int) (k uint, bits int) {
	if cnt > 0 {
		for uint64(cnt)<<k < sum && k < rice2EscapeParam-1 {
			k++
		}
	}
	var mag uint64
	if k > 0 {
		mag = sum >> (k - 1)
	} else {
		mag = sum << 1
	}
	est := uint64(riceParamBits) + uint64(1+k)*uint64(cnt) + mag - uint64(cnt>>1)
	if est > 1<<31 {
		est = 1 << 31
	}
	return k, int(est)
}
