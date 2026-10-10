// LZMA encoder.
//
// The output depends only on the input and the parameters. There is no
// multi-threaded match finder; it would not change the output.

package lzma

import "math/bits"

const (
	numTopBits         = 24
	topValue           = 1 << numTopBits
	numBitModelBits    = 11
	bitModelTotal      = 1 << numBitModelBits
	numMoveBits        = 5
	probInitValue      = bitModelTotal >> 1
	numMoveReduceBits  = 4
	numBitPriceShift   = 4
	repLenCount        = 64
	numReps            = 4
	numOpts            = 1 << 11
	numLenToPosStates  = 4
	numPosSlotBits     = 6
	dicLogSizeMax      = 32
	distTableSizeMax   = dicLogSizeMax * 2
	numAlignBits       = 4
	alignTableSize     = 1 << numAlignBits
	alignMask          = alignTableSize - 1
	startPosModelIndex = 4
	endPosModelIndex   = 14
	numFullDistances   = 1 << (endPosModelIndex >> 1)
	pbStatesMax        = 1 << MaxPB
	lenNumLowBits      = 3
	lenNumLowSymbols   = 1 << lenNumLowBits
	lenNumHighBits     = 8
	lenNumHighSymbols  = 1 << lenNumHighBits
	lenNumSymbolsTotal = lenNumLowSymbols*2 + lenNumHighSymbols
	matchLenMin        = 2
	matchLenMax        = matchLenMin + lenNumSymbolsTotal - 1
	numStates          = 12
	infinityPrice      = 1 << 30
	markLit            = 0xFFFFFFFF
	stateLitAfterMatch = 4
	stateLitAfterRep   = 5
	stateMatchAfterLit = 7
	stateRepAfterLit   = 8
)

var (
	literalNextStates  = [numStates]uint8{0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 4, 5}
	matchNextStates    = [numStates]uint8{7, 7, 7, 7, 7, 7, 7, 10, 10, 10, 10, 10}
	repNextStates      = [numStates]uint8{8, 8, 8, 8, 8, 8, 8, 11, 11, 11, 11, 11}
	shortRepNextStates = [numStates]uint8{9, 9, 9, 9, 9, 9, 9, 11, 11, 11, 11, 11}
)

// probPrices holds the price (in 1/16 bit) of coding a bit whose
// probability is the index << numMoveReduceBits.
var probPrices = func() (t [bitModelTotal >> numMoveReduceBits]uint32) {
	for i := range t {
		w := uint32(i)<<numMoveReduceBits + 1<<(numMoveReduceBits-1)
		bitCount := uint32(0)
		for range numBitPriceShift {
			w *= w
			bitCount <<= 1
			for w >= 1<<16 {
				w >>= 1
				bitCount++
			}
		}
		t[i] = numBitModelBits<<numBitPriceShift - 15 - bitCount
	}
	return t
}()

func price0(prob uint16) uint32 { return probPrices[(prob>>numMoveReduceBits)&127] }
func price1(prob uint16) uint32 {
	return probPrices[((prob^(bitModelTotal-1))>>numMoveReduceBits)&127]
}
func priceBit(prob uint16, bit uint32) uint32 {
	return probPrices[((uint32(prob)^(-bit&(bitModelTotal-1)))>>numMoveReduceBits)&127]
}

// getPosSlot returns the distance slot of dist.
func getPosSlot(dist uint32) uint32 {
	if dist < 2 {
		return dist
	}
	zz := uint32(30 - bits.LeadingZeros32(dist))
	return zz + zz + dist>>zz
}

func isLitState(s uint32) bool { return s < 7 }

func lenToPosState(l uint32) uint32 {
	if l < numLenToPosStates+1 {
		return l - 2
	}
	return numLenToPosStates - 1
}

type optimal struct {
	price uint32
	state uint16
	extra uint16 // 0: normal; 1: LIT : MATCH; > 1: MATCH (extra-1) : LIT : REP0 (len)
	len   uint32
	dist  uint32
	reps  [numReps]uint32
}

type lenEnc struct {
	low  [pbStatesMax << (lenNumLowBits + 1)]uint16
	high [lenNumHighSymbols]uint16
}

func (p *lenEnc) init() {
	for i := range p.low {
		p.low[i] = probInitValue
	}
	for i := range p.high {
		p.high[i] = probInitValue
	}
}

type lenPriceEnc struct {
	tableSize uint32
	prices    [pbStatesMax][lenNumSymbolsTotal]uint32
}

func (p *lenPriceEnc) price(posState, l uint32) uint32 {
	return p.prices[posState&(pbStatesMax-1)][l-matchLenMin]
}

// rangeEncoder is the range encoder, writing to a growing slice.
type rangeEncoder struct {
	rng       uint32
	cache     uint32
	low       uint64
	cacheSize uint64
	out       []byte
}

func (rc *rangeEncoder) init() {
	rc.rng = 0xFFFFFFFF
	rc.cache = 0
	rc.low = 0
	rc.cacheSize = 0
}

func (rc *rangeEncoder) shiftLow() {
	low := uint32(rc.low)
	high := uint32(rc.low >> 32)
	rc.low = uint64(low << 8)
	if low < 0xFF000000 || high != 0 {
		rc.out = append(rc.out, byte(rc.cache+high))
		rc.cache = low >> 24
		if rc.cacheSize == 0 {
			return
		}
		high += 0xFF
		for {
			rc.out = append(rc.out, byte(high))
			rc.cacheSize--
			if rc.cacheSize == 0 {
				return
			}
		}
	}
	rc.cacheSize++
}

func (rc *rangeEncoder) flushData() {
	for range 5 {
		rc.shiftLow()
	}
}

// encodeBit encodes bit with the adaptive probability *prob. It is branch
// free.
func (rc *rangeEncoder) encodeBit(prob *uint16, bit uint32) {
	ttt := uint32(*prob)
	newBound := (rc.rng >> numBitModelBits) * ttt
	mask := -bit // all ones for a 1 bit
	rc.low += uint64(newBound & mask)
	rc.rng = newBound&^mask | (rc.rng-newBound)&mask
	*prob = uint16(ttt + uint32(int32(^mask&(bitModelTotal-(1<<numMoveBits-1))+(1<<numMoveBits-1)-ttt)>>numMoveBits))
	if rc.rng < topValue {
		rc.rng <<= 8
		rc.shiftLow()
	}
}

func (rc *rangeEncoder) encodeBit0(prob *uint16) {
	ttt := uint32(*prob)
	rc.rng = (rc.rng >> numBitModelBits) * ttt
	*prob = uint16(ttt + (bitModelTotal-ttt)>>numMoveBits)
	if rc.rng < topValue {
		rc.rng <<= 8
		rc.shiftLow()
	}
}

func (rc *rangeEncoder) encodeBit1(prob *uint16) {
	ttt := uint32(*prob)
	newBound := (rc.rng >> numBitModelBits) * ttt
	rc.low += uint64(newBound)
	rc.rng -= newBound
	*prob = uint16(ttt - ttt>>numMoveBits)
	if rc.rng < topValue {
		rc.rng <<= 8
		rc.shiftLow()
	}
}

// encodeDirect encodes the numBits low bits of v, most significant first,
// with probability 1/2 each.
func (rc *rangeEncoder) encodeDirect(v uint32, numBits uint32) {
	for numBits != 0 {
		numBits--
		rc.rng >>= 1
		rc.low += uint64(rc.rng & -((v >> numBits) & 1))
		if rc.rng < topValue {
			rc.rng <<= 8
			rc.shiftLow()
		}
	}
}

// litEncode encodes the byte sym with the 8-bit tree probs. The coder state
// is kept in locals.
func (rc *rangeEncoder) litEncode(probs []uint16, sym uint32) {
	probs = probs[:0x100]
	rng, low := rc.rng, rc.low
	sym |= 0x100
	for {
		prob := &probs[(sym>>8)&0xFF]
		mask := -((sym >> 7) & 1)
		sym <<= 1
		ttt := uint32(*prob)
		newBound := (rng >> numBitModelBits) * ttt
		low += uint64(newBound & mask)
		rng = newBound&^mask | (rng-newBound)&mask
		*prob = uint16(ttt + uint32(int32(^mask&(bitModelTotal-(1<<numMoveBits-1))+(1<<numMoveBits-1)-ttt)>>numMoveBits))
		if rng < topValue {
			rng <<= 8
			rc.low = low
			rc.shiftLow()
			low = rc.low
		}
		if sym >= 0x10000 {
			break
		}
	}
	rc.rng, rc.low = rng, low
}

// litEncodeMatched encodes the byte sym as a matched literal against matchByte.
func (rc *rangeEncoder) litEncodeMatched(probs []uint16, sym, matchByte uint32) {
	probs = probs[:0x300]
	rng, low := rc.rng, rc.low
	offs := uint32(0x100)
	sym |= 0x100
	for {
		matchByte <<= 1
		prob := &probs[offs+(matchByte&offs)+(sym>>8)]
		mask := -((sym >> 7) & 1)
		sym <<= 1
		offs &^= matchByte ^ sym
		ttt := uint32(*prob)
		newBound := (rng >> numBitModelBits) * ttt
		low += uint64(newBound & mask)
		rng = newBound&^mask | (rng-newBound)&mask
		*prob = uint16(ttt + uint32(int32(^mask&(bitModelTotal-(1<<numMoveBits-1))+(1<<numMoveBits-1)-ttt)>>numMoveBits))
		if rng < topValue {
			rng <<= 8
			rc.low = low
			rc.shiftLow()
			low = rc.low
		}
		if sym >= 0x10000 {
			break
		}
	}
	rc.rng, rc.low = rng, low
}

func (rc *rangeEncoder) reverseEncode(probs []uint16, numBits, sym uint32) {
	m := uint32(1)
	for {
		bit := sym & 1
		sym >>= 1
		rc.encodeBit(&probs[m], bit)
		m = m<<1 | bit
		numBits--
		if numBits == 0 {
			return
		}
	}
}

func (rc *rangeEncoder) lenEncode(p *lenEnc, sym, posState uint32) {
	probs := p.low[:]
	if sym >= lenNumLowSymbols {
		rc.encodeBit1(&probs[0])
		probs = probs[lenNumLowSymbols:]
		if sym >= lenNumLowSymbols*2 {
			rc.encodeBit1(&probs[0])
			rc.litEncode(p.high[:], sym-lenNumLowSymbols*2)
			return
		}
		sym -= lenNumLowSymbols
	}
	rc.encodeBit0(&probs[0])
	probs = probs[posState<<(1+lenNumLowBits):]
	bit := sym >> 2
	rc.encodeBit(&probs[1], bit)
	m := 2 + bit
	bit = (sym >> 1) & 1
	rc.encodeBit(&probs[m], bit)
	m = m<<1 + bit
	bit = sym & 1
	rc.encodeBit(&probs[m], bit)
}

func litGetPrice(probs []uint16, sym uint32) uint32 {
	probs = probs[:0x300]
	price := uint32(0)
	sym |= 0x100
	for {
		bit := sym & 1
		sym >>= 1
		price += priceBit(probs[sym], bit)
		if sym < 2 {
			return price
		}
	}
}

func litMatchedGetPrice(probs []uint16, sym, matchByte uint32) uint32 {
	probs = probs[:0x300]
	price := uint32(0)
	offs := uint32(0x100)
	sym |= 0x100
	for {
		matchByte <<= 1
		price += priceBit(probs[offs+(matchByte&offs)+(sym>>8)], (sym>>7)&1)
		sym <<= 1
		offs &^= matchByte ^ sym
		if sym >= 0x10000 {
			return price
		}
	}
}

func setPrices3(probs []uint16, startPrice uint32, prices []uint32) {
	probs = probs[:8]
	prices = prices[:8]
	for i := uint32(0); i < 8; i += 2 {
		price := startPrice
		price += priceBit(probs[1], i>>2)
		price += priceBit(probs[2+(i>>2)], (i>>1)&1)
		prob := probs[4+(i>>1)]
		prices[i] = price + price0(prob)
		prices[i+1] = price + price1(prob)
	}
}

// updateTables recomputes the length price tables.
func (p *lenPriceEnc) updateTables(numPosStates uint32, enc *lenEnc) {
	prob := enc.low[0]
	b := price1(prob)
	a := price0(prob)
	c := b + price0(enc.low[lenNumLowSymbols])
	for posState := uint32(0); posState < numPosStates; posState++ {
		prices := p.prices[posState][:]
		probs := enc.low[posState<<(1+lenNumLowBits):]
		setPrices3(probs, a, prices)
		setPrices3(probs[lenNumLowSymbols:], c, prices[lenNumLowSymbols:])
	}

	i := p.tableSize
	if i > lenNumLowSymbols*2 {
		probs := enc.high[:]
		prices := p.prices[0][lenNumLowSymbols*2:]
		i -= lenNumLowSymbols*2 - 1
		i >>= 1
		b += price1(enc.low[lenNumLowSymbols])
		for {
			i--
			sym := i + 1<<(lenNumHighBits-1)
			price := b
			for {
				bit := sym & 1
				sym >>= 1
				price += priceBit(probs[sym], bit)
				if sym < 2 {
					break
				}
			}
			prob := probs[i+1<<(lenNumHighBits-1)]
			prices[i*2] = price + price0(prob)
			prices[i*2+1] = price + price1(prob)
			if i == 0 {
				break
			}
		}
		num := p.tableSize - lenNumLowSymbols*2
		src := p.prices[0][lenNumLowSymbols*2 : lenNumLowSymbols*2+num]
		for posState := uint32(1); posState < numPosStates; posState++ {
			copy(p.prices[posState][lenNumLowSymbols*2:], src)
		}
	}
}

// encoder is the LZMA encoder state.
type encoder struct {
	mf matchFinder

	optCur, optEnd uint32

	longestMatchLen uint32
	numPairs        uint32
	numAvail        uint32

	state            uint32
	numFastBytes     uint32
	additionalOffset uint32
	reps             [numReps]uint32
	lpMask, pbMask   uint32
	litProbs         []uint16
	rc               rangeEncoder

	backRes uint32

	lc, lp, pb uint32

	fastMode     bool
	writeEndMark bool

	matchPriceCount uint32
	repLenEncCount  int32
	distTableSize   uint32
	dictSize        uint32

	matches        [matchLenMax*2 + 2]uint32
	alignPrices    [alignTableSize]uint32
	posSlotPrices  [numLenToPosStates][distTableSizeMax]uint32
	distancesPrice [numLenToPosStates][numFullDistances]uint32

	posAlignEncoder [1 << numAlignBits]uint16
	isRep           [numStates]uint16
	isRepG0         [numStates]uint16
	isRepG1         [numStates]uint16
	isRepG2         [numStates]uint16
	isMatch         [numStates][pbStatesMax]uint16
	isRep0Long      [numStates][pbStatesMax]uint16
	posSlotEncoder  [numLenToPosStates][1 << numPosSlotBits]uint16
	posEncoders     [numFullDistances]uint16

	lenProbs    lenEnc
	repLenProbs lenEnc

	lenEnc    lenPriceEnc
	repLenEnc lenPriceEnc

	opt [numOpts]optimal
}

// encParams are the normalized parameters the encoder needs.
type encParams struct {
	dictSize     uint32
	lc, lp, pb   uint32
	fb           uint32 // already clamped to [MinFB, MaxFB]
	fastMode     bool
	btMode       bool
	numHashBytes uint32
	mc           uint32
	writeEndMark bool
}

// encodeStream compresses src into a raw LZMA stream appended to dst.
func encodeStream(dst, src []byte, ep encParams) []byte {
	p := new(encoder)
	p.dictSize = ep.dictSize
	p.numFastBytes = ep.fb
	p.lc, p.lp, p.pb = ep.lc, ep.lp, ep.pb
	p.fastMode = ep.fastMode
	p.writeEndMark = ep.writeEndMark
	p.mf.btMode = ep.btMode
	p.mf.numHashBytes = ep.numHashBytes
	p.mf.cutValue = ep.mc
	p.rc.out = dst

	// Allocation and initialisation.
	i := uint32(endPosModelIndex / 2)
	for ; i < dicLogSizeMax; i++ {
		if uint64(p.dictSize) <= uint64(1)<<i {
			break
		}
	}
	p.distTableSize = i * 2

	p.litProbs = make([]uint16, 0x300<<(p.lc+p.lp))
	dictSize := p.dictSize
	if dictSize == 2<<30 || dictSize == 3<<30 {
		dictSize--
	}
	p.mf.create(src, dictSize, p.numFastBytes, matchLenMax+1)

	p.init()
	p.initPrices()
	p.codeAll()
	return p.rc.out
}

// init resets the coder state and probabilities.
func (p *encoder) init() {
	p.state = 0
	p.reps = [numReps]uint32{1, 1, 1, 1}
	p.rc.init()
	for i := range p.posAlignEncoder {
		p.posAlignEncoder[i] = probInitValue
	}
	for i := range numStates {
		for j := range pbStatesMax {
			p.isMatch[i][j] = probInitValue
			p.isRep0Long[i][j] = probInitValue
		}
		p.isRep[i] = probInitValue
		p.isRepG0[i] = probInitValue
		p.isRepG1[i] = probInitValue
		p.isRepG2[i] = probInitValue
	}
	for i := range p.posSlotEncoder {
		for j := range p.posSlotEncoder[i] {
			p.posSlotEncoder[i][j] = probInitValue
		}
	}
	for i := range p.posEncoders {
		p.posEncoders[i] = probInitValue
	}
	for i := range p.litProbs {
		p.litProbs[i] = probInitValue
	}
	p.lenProbs.init()
	p.repLenProbs.init()
	p.optEnd = 0
	p.optCur = 0
	for i := range p.opt {
		p.opt[i].price = infinityPrice
	}
	p.additionalOffset = 0
	p.pbMask = 1<<p.pb - 1
	p.lpMask = 0x100<<p.lp - 0x100>>p.lc
}

// initPrices fills the initial price tables.
func (p *encoder) initPrices() {
	if !p.fastMode {
		p.fillDistancesPrices()
		p.fillAlignPrices()
	}
	p.lenEnc.tableSize = p.numFastBytes + 1 - matchLenMin
	p.repLenEnc.tableSize = p.lenEnc.tableSize
	p.repLenEncCount = repLenCount
	p.lenEnc.updateTables(1<<p.pb, &p.lenProbs)
	p.repLenEnc.updateTables(1<<p.pb, &p.repLenProbs)
}

func (p *encoder) fillAlignPrices() {
	probs := &p.posAlignEncoder
	for i := uint32(0); i < alignTableSize/2; i++ {
		price := uint32(0)
		sym := i
		m := uint32(1)
		for range 3 {
			bit := sym & 1
			sym >>= 1
			price += priceBit(probs[m], bit)
			m = m<<1 + bit
		}
		prob := probs[m]
		p.alignPrices[i] = price + price0(prob)
		p.alignPrices[i+8] = price + price1(prob)
	}
}

func (p *encoder) fillDistancesPrices() {
	var tempPrices [numFullDistances]uint32
	p.matchPriceCount = 0

	for i := uint32(startPosModelIndex / 2); i < numFullDistances/2; i++ {
		posSlot := getPosSlot(i)
		footerBits := (posSlot >> 1) - 1
		base := (2 | (posSlot & 1)) << footerBits
		probs := p.posEncoders[base*2:]
		price := uint32(0)
		m := uint32(1)
		sym := i
		offset := uint32(1) << footerBits
		base += i
		for ; footerBits != 0; footerBits-- {
			bit := sym & 1
			sym >>= 1
			price += priceBit(probs[m], bit)
			m = m<<1 + bit
		}
		prob := probs[m]
		tempPrices[base] = price + price0(prob)
		tempPrices[base+offset] = price + price1(prob)
	}

	for lps := range numLenToPosStates {
		distTableSize2 := (p.distTableSize + 1) >> 1
		posSlotPrices := &p.posSlotPrices[lps]
		probs := &p.posSlotEncoder[lps]
		for slot := uint32(0); slot < distTableSize2; slot++ {
			sym := slot + 1<<(numPosSlotBits-1)
			price := uint32(0)
			for range 5 {
				bit := sym & 1
				sym >>= 1
				price += priceBit(probs[sym], bit)
			}
			prob := probs[slot+1<<(numPosSlotBits-1)]
			posSlotPrices[slot*2] = price + price0(prob)
			posSlotPrices[slot*2+1] = price + price1(prob)
		}
		delta := uint32((endPosModelIndex/2-1)-numAlignBits) << numBitPriceShift
		for slot := uint32(endPosModelIndex / 2); slot < distTableSize2; slot++ {
			posSlotPrices[slot*2] += delta
			posSlotPrices[slot*2+1] += delta
			delta += 1 << numBitPriceShift
		}
		dp := &p.distancesPrice[lps]
		dp[0] = posSlotPrices[0]
		dp[1] = posSlotPrices[1]
		dp[2] = posSlotPrices[2]
		dp[3] = posSlotPrices[3]
		for i := uint32(4); i < numFullDistances; i += 2 {
			slotPrice := posSlotPrices[getPosSlot(i)]
			dp[i] = slotPrice + tempPrices[i]
			dp[i+1] = slotPrice + tempPrices[i+1]
		}
	}
}

// movePos skips num positions in the match finder.
func (p *encoder) movePos(num uint32) {
	p.additionalOffset += num
	p.mf.skip(num)
}

// readMatchDistances returns the longest
// match length and the number of values stored in p.matches.
func (p *encoder) readMatchDistances() (uint32, uint32) {
	p.additionalOffset++
	p.numAvail = p.mf.avail()
	numPairs := uint32(p.mf.getMatches(p.matches[:]))
	if numPairs == 0 {
		return 0, 0
	}
	l := p.matches[numPairs-2]
	if l != p.numFastBytes {
		return l, numPairs
	}
	numAvail := min(p.numAvail, matchLenMax)
	buf := p.mf.buf
	p1 := p.mf.bufPos - 1
	dist := int(p.matches[numPairs-1]) + 1
	a := buf[p1 : p1+int(numAvail)]
	b := buf[p1-dist : p1-dist+len(a)]
	i := matchLen(a, b, int(l))
	return uint32(i), numPairs
}

func (p *encoder) priceShortRep(state, posState uint32) uint32 {
	return price0(p.isRepG0[state]) + price0(p.isRep0Long[state][posState])
}

func (p *encoder) priceRep0(state, posState uint32) uint32 {
	return price1(p.isMatch[state][posState]) + price1(p.isRep0Long[state][posState]) +
		price1(p.isRep[state]) + price0(p.isRepG0[state])
}

func (p *encoder) pricePureRep(repIndex, state, posState uint32) uint32 {
	prob := p.isRepG0[state]
	if repIndex == 0 {
		return price0(prob) + price1(p.isRep0Long[state][posState])
	}
	price := price1(prob)
	prob = p.isRepG1[state]
	if repIndex == 1 {
		return price + price0(prob)
	}
	price += price1(prob)
	return price + priceBit(p.isRepG2[state], repIndex-2)
}

// backward turns the chain of optimal decisions ending
// at cur into a forward list in p.opt and returns the first length.
func (p *encoder) backward(cur uint32) uint32 {
	wr := cur + 1
	p.optEnd = wr
	for {
		dist := p.opt[cur].dist
		l := p.opt[cur].len
		extra := uint32(p.opt[cur].extra)
		cur -= l
		if extra != 0 {
			wr--
			p.opt[wr].len = l
			cur -= extra
			l = extra
			if extra == 1 {
				p.opt[wr].dist = dist
				dist = markLit
			} else {
				p.opt[wr].dist = 0
				l--
				wr--
				p.opt[wr].dist = markLit
				p.opt[wr].len = 1
			}
		}
		if cur == 0 {
			p.backRes = dist
			p.optCur = wr
			return l
		}
		wr--
		p.opt[wr].dist = dist
		p.opt[wr].len = l
	}
}

// litProbsAt returns the literal probabilities for pos and prevByte.
func (p *encoder) litProbsAt(pos uint32, prevByte byte) []uint16 {
	off := 3 * ((((pos << 8) + uint32(prevByte)) & p.lpMask) << p.lc)
	return p.litProbs[off : off+0x300]
}

// getOptimum is the optimal parser of normal mode.
func (p *encoder) getOptimum(position uint32) uint32 {
	var reps [numReps]uint32
	var repLens [numReps]uint32
	var last, cur uint32
	buf := p.mf.buf
	matches := p.matches[:]

	{
		var mainLen, numPairs uint32
		p.optCur = 0
		p.optEnd = 0
		if p.additionalOffset == 0 {
			mainLen, numPairs = p.readMatchDistances()
		} else {
			mainLen = p.longestMatchLen
			numPairs = p.numPairs
		}

		numAvail := p.numAvail
		if numAvail < 2 {
			p.backRes = markLit
			return 1
		}
		if numAvail > matchLenMax {
			numAvail = matchLenMax
		}

		data := p.mf.bufPos - 1
		repMaxIndex := uint32(0)
		for i := range uint32(numReps) {
			reps[i] = p.reps[i]
			data2 := data - int(reps[i])
			if buf[data] != buf[data2] || buf[data+1] != buf[data2+1] {
				repLens[i] = 0
				continue
			}
			a := buf[data : data+int(numAvail)]
			b := buf[data2 : data2+len(a)]
			l := matchLen(a, b, 2)
			repLens[i] = uint32(l)
			if uint32(l) > repLens[repMaxIndex] {
				repMaxIndex = i
			}
			if l == matchLenMax {
				break
			}
		}

		if repLens[repMaxIndex] >= p.numFastBytes {
			p.backRes = repMaxIndex
			l := repLens[repMaxIndex]
			p.movePos(l - 1)
			return l
		}

		if mainLen >= p.numFastBytes {
			p.backRes = matches[numPairs-1] + numReps
			p.movePos(mainLen - 1)
			return mainLen
		}

		curByte := buf[data]
		matchByte := buf[data-int(reps[0])]

		last = repLens[repMaxIndex]
		if last <= mainLen {
			last = mainLen
		}

		if last < 2 && curByte != matchByte {
			p.backRes = markLit
			return 1
		}

		p.opt[0].state = uint16(p.state)
		posState := position & p.pbMask

		{
			probs := p.litProbsAt(position, buf[data-1])
			price := price0(p.isMatch[p.state][posState])
			if !isLitState(p.state) {
				price += litMatchedGetPrice(probs, uint32(curByte), uint32(matchByte))
			} else {
				price += litGetPrice(probs, uint32(curByte))
			}
			p.opt[1].price = price
		}
		p.opt[1].dist = markLit
		p.opt[1].extra = 0

		matchPrice := price1(p.isMatch[p.state][posState])
		repMatchPrice := matchPrice + price1(p.isRep[p.state])

		if matchByte == curByte && repLens[0] == 0 {
			shortRepPrice := repMatchPrice + p.priceShortRep(p.state, posState)
			if shortRepPrice < p.opt[1].price {
				p.opt[1].price = shortRepPrice
				p.opt[1].dist = 0
				p.opt[1].extra = 0
			}
			if last < 2 {
				p.backRes = p.opt[1].dist
				return 1
			}
		}

		p.opt[1].len = 1
		p.opt[0].reps = reps

		// REP
		for i := range uint32(numReps) {
			repLen := repLens[i]
			if repLen < 2 {
				continue
			}
			price := repMatchPrice + p.pricePureRep(i, p.state, posState)
			for {
				price2 := price + p.repLenEnc.price(posState, repLen)
				opt := &p.opt[repLen]
				if price2 < opt.price {
					opt.price = price2
					opt.len = repLen
					opt.dist = i
					opt.extra = 0
				}
				repLen--
				if repLen < 2 {
					break
				}
			}
		}

		// MATCH
		{
			l := repLens[0] + 1
			if l <= mainLen {
				offs := uint32(0)
				normalMatchPrice := matchPrice + price0(p.isRep[p.state])
				if l < 2 {
					l = 2
				} else {
					for l > matches[offs] {
						offs += 2
					}
				}
				for ; ; l++ {
					dist := matches[offs+1]
					price := normalMatchPrice + p.lenEnc.price(posState, l)
					lps := lenToPosState(l)
					if dist < numFullDistances {
						price += p.distancesPrice[lps][dist&(numFullDistances-1)]
					} else {
						slot := getPosSlot(dist)
						price += p.alignPrices[dist&alignMask]
						price += p.posSlotPrices[lps][slot]
					}
					opt := &p.opt[l]
					if price < opt.price {
						opt.price = price
						opt.len = l
						opt.dist = dist + numReps
						opt.extra = 0
					}
					if l == matches[offs] {
						offs += 2
						if offs == numPairs {
							break
						}
					}
				}
			}
		}
		cur = 0
	}

	// Optimal parsing.
	for {
		cur++
		if cur == last {
			break
		}

		if cur >= numOpts-64 {
			price := p.opt[cur].price
			best := cur
			for j := cur + 1; j <= last; j++ {
				price2 := p.opt[j].price
				if price >= price2 {
					price = price2
					best = j
				}
			}
			if delta := best - cur; delta != 0 {
				p.movePos(delta)
			}
			cur = best
			break
		}

		newLen, numPairs := p.readMatchDistances()
		if newLen >= p.numFastBytes {
			p.numPairs = numPairs
			p.longestMatchLen = newLen
			break
		}

		curOpt := &p.opt[cur]
		position++

		prev := cur - curOpt.len
		var state uint32
		if curOpt.len == 1 {
			state = uint32(p.opt[prev].state)
			if curOpt.dist == 0 {
				state = uint32(shortRepNextStates[state])
			} else {
				state = uint32(literalNextStates[state])
			}
		} else {
			dist := curOpt.dist
			if curOpt.extra != 0 {
				prev -= uint32(curOpt.extra)
				state = stateRepAfterLit
				if curOpt.extra == 1 {
					if dist < numReps {
						state = stateRepAfterLit
					} else {
						state = stateMatchAfterLit
					}
				}
			} else {
				state = uint32(p.opt[prev].state)
				if dist < numReps {
					state = uint32(repNextStates[state])
				} else {
					state = uint32(matchNextStates[state])
				}
			}

			prevOpt := &p.opt[prev]
			b0 := prevOpt.reps[0]
			if dist < numReps {
				if dist == 0 {
					reps[0] = b0
					reps[1] = prevOpt.reps[1]
					reps[2] = prevOpt.reps[2]
					reps[3] = prevOpt.reps[3]
				} else {
					reps[1] = b0
					b0 = prevOpt.reps[1]
					if dist == 1 {
						reps[0] = b0
						reps[2] = prevOpt.reps[2]
						reps[3] = prevOpt.reps[3]
					} else {
						reps[2] = b0
						reps[0] = prevOpt.reps[dist]
						reps[3] = prevOpt.reps[dist^1]
					}
				}
			} else {
				reps[0] = dist - numReps + 1
				reps[1] = b0
				reps[2] = prevOpt.reps[1]
				reps[3] = prevOpt.reps[2]
			}
		}

		curOpt.state = uint16(state)
		curOpt.reps = reps

		data := p.mf.bufPos - 1
		curByte := buf[data]
		matchByte := buf[data-int(reps[0])]
		posState := position & p.pbMask

		var litPrice, matchPrice uint32
		{
			curPrice := curOpt.price
			prob := p.isMatch[state][posState]
			matchPrice = curPrice + price1(prob)
			litPrice = curPrice + price0(prob)
		}

		nextOpt := &p.opt[cur+1]
		nextIsLit := false

		if (nextOpt.price < infinityPrice && matchByte == curByte) || litPrice > nextOpt.price {
			litPrice = 0
		} else {
			probs := p.litProbsAt(position, buf[data-1])
			if !isLitState(state) {
				litPrice += litMatchedGetPrice(probs, uint32(curByte), uint32(matchByte))
			} else {
				litPrice += litGetPrice(probs, uint32(curByte))
			}
			if litPrice < nextOpt.price {
				nextOpt.price = litPrice
				nextOpt.len = 1
				nextOpt.dist = markLit
				nextOpt.extra = 0
				nextIsLit = true
			}
		}

		repMatchPrice := matchPrice + price1(p.isRep[state])

		numAvailFull := p.numAvail
		if temp := numOpts - 1 - cur; numAvailFull > temp {
			numAvailFull = temp
		}

		// SHORT_REP
		if isLitState(state) && matchByte == curByte && repMatchPrice < nextOpt.price &&
			(nextOpt.len < 2 || nextOpt.dist != 0) {
			shortRepPrice := repMatchPrice + p.priceShortRep(state, posState)
			if shortRepPrice < nextOpt.price {
				nextOpt.price = shortRepPrice
				nextOpt.len = 1
				nextOpt.dist = 0
				nextOpt.extra = 0
				nextIsLit = false
			}
		}

		if numAvailFull < 2 {
			continue
		}
		numAvail := min(numAvailFull, p.numFastBytes)

		// LIT : REP_0
		if !nextIsLit && litPrice != 0 && matchByte != curByte && numAvailFull > 2 {
			data2 := data - int(reps[0])
			if buf[data+1] == buf[data2+1] && buf[data+2] == buf[data2+2] {
				limit := min(p.numFastBytes+1, numAvailFull)
				a := buf[data : data+int(limit)]
				b := buf[data2 : data2+len(a)]
				l := matchLen(a, b, 3)
				state2 := uint32(literalNextStates[state])
				posState2 := (position + 1) & p.pbMask
				price := litPrice + p.priceRep0(state2, posState2)
				offset := cur + uint32(l)
				if last < offset {
					last = offset
				}
				l--
				price2 := price + p.repLenEnc.price(posState2, uint32(l))
				opt := &p.opt[offset]
				if price2 < opt.price {
					opt.price = price2
					opt.len = uint32(l)
					opt.dist = 0
					opt.extra = 1
				}
			}
		}

		startLen := uint32(2)

		// REP
		for repIndex := range uint32(numReps) {
			data2 := data - int(reps[repIndex])
			if buf[data] != buf[data2] || buf[data+1] != buf[data2+1] {
				continue
			}
			var l uint32
			{
				a := buf[data : data+int(numAvail)]
				b := buf[data2 : data2+len(a)]
				i := matchLen(a, b, 2)
				l = uint32(i)
			}
			if offset := cur + l; last < offset {
				last = offset
			}
			price := repMatchPrice + p.pricePureRep(repIndex, state, posState)
			for len2 := l; ; {
				price2 := price + p.repLenEnc.price(posState, len2)
				opt := &p.opt[cur+len2]
				if price2 < opt.price {
					opt.price = price2
					opt.len = len2
					opt.dist = repIndex
					opt.extra = 0
				}
				len2--
				if len2 < 2 {
					break
				}
			}

			if repIndex == 0 {
				startLen = l + 1
			}

			// REP : LIT : REP_0
			len2 := l + 1
			limit := min(len2+p.numFastBytes, numAvailFull)
			len2 += 2
			if len2 <= limit && buf[data+int(len2)-2] == buf[data2+int(len2)-2] &&
				buf[data+int(len2)-1] == buf[data2+int(len2)-1] {
				state2 := uint32(repNextStates[state])
				posState2 := (position + l) & p.pbMask
				price += p.repLenEnc.price(posState, l) +
					price0(p.isMatch[state2][posState2]) +
					litMatchedGetPrice(p.litProbsAt(position+l, buf[data+int(l)-1]),
						uint32(buf[data+int(l)]), uint32(buf[data2+int(l)]))
				state2 = stateLitAfterRep
				posState2 = (posState2 + 1) & p.pbMask
				price += p.priceRep0(state2, posState2)

				{
					a := buf[data : data+int(limit)]
					b := buf[data2 : data2+len(a)]
					i := matchLen(a, b, int(len2))
					len2 = uint32(i)
				}
				len2 -= l
				offset := cur + l + len2
				if last < offset {
					last = offset
				}
				len2--
				price2 := price + p.repLenEnc.price(posState2, len2)
				opt := &p.opt[offset]
				if price2 < opt.price {
					opt.price = price2
					opt.len = len2
					opt.extra = uint16(l + 1)
					opt.dist = repIndex
				}
			}
		}

		// MATCH
		if newLen > numAvail {
			newLen = numAvail
			numPairs = 0
			for newLen > matches[numPairs] {
				numPairs += 2
			}
			matches[numPairs] = newLen
			numPairs += 2
		}

		if newLen >= startLen {
			normalMatchPrice := matchPrice + price0(p.isRep[state])
			if offset := cur + newLen; last < offset {
				last = offset
			}
			offs := uint32(0)
			for startLen > matches[offs] {
				offs += 2
			}
			dist := matches[offs+1]
			posSlot := getPosSlot(dist)

			for l := startLen; ; l++ {
				price := normalMatchPrice + p.lenEnc.price(posState, l)
				lenNorm := l - 2
				if lenNorm >= numLenToPosStates {
					lenNorm = numLenToPosStates - 1
				}
				if dist < numFullDistances {
					price += p.distancesPrice[lenNorm][dist&(numFullDistances-1)]
				} else {
					price += p.posSlotPrices[lenNorm][posSlot] + p.alignPrices[dist&alignMask]
				}
				opt := &p.opt[cur+l]
				if price < opt.price {
					opt.price = price
					opt.len = l
					opt.dist = dist + numReps
					opt.extra = 0
				}

				if l == matches[offs] {
					// MATCH : LIT : REP_0
					data2 := data - int(dist) - 1
					len2 := l + 1
					limit := min(len2+p.numFastBytes, numAvailFull)
					len2 += 2
					if len2 <= limit && buf[data+int(len2)-2] == buf[data2+int(len2)-2] &&
						buf[data+int(len2)-1] == buf[data2+int(len2)-1] {
						{
							a := buf[data : data+int(limit)]
							b := buf[data2 : data2+len(a)]
							i := matchLen(a, b, int(len2))
							len2 = uint32(i)
						}
						len2 -= l
						state2 := uint32(matchNextStates[state])
						posState2 := (position + l) & p.pbMask
						price += price0(p.isMatch[state2][posState2])
						price += litMatchedGetPrice(p.litProbsAt(position+l, buf[data+int(l)-1]),
							uint32(buf[data+int(l)]), uint32(buf[data2+int(l)]))
						state2 = stateLitAfterMatch
						posState2 = (posState2 + 1) & p.pbMask
						price += p.priceRep0(state2, posState2)
						offset := cur + l + len2
						if last < offset {
							last = offset
						}
						len2--
						price2 := price + p.repLenEnc.price(posState2, len2)
						opt := &p.opt[offset]
						if price2 < opt.price {
							opt.price = price2
							opt.len = len2
							opt.extra = uint16(l + 1)
							opt.dist = dist + numReps
						}
					}
					offs += 2
					if offs == numPairs {
						break
					}
					dist = matches[offs+1]
					posSlot = getPosSlot(dist)
				}
			}
		}
	}

	for {
		p.opt[last].price = infinityPrice
		last--
		if last == 0 {
			break
		}
	}
	return p.backward(cur)
}

func changePair(smallDist, bigDist uint32) bool { return bigDist>>7 > smallDist }

// getOptimumFast is the greedy parser of fast mode.
func (p *encoder) getOptimumFast() uint32 {
	var mainLen, numPairs uint32
	if p.additionalOffset == 0 {
		mainLen, numPairs = p.readMatchDistances()
	} else {
		mainLen = p.longestMatchLen
		numPairs = p.numPairs
	}

	numAvail := p.numAvail
	p.backRes = markLit
	if numAvail < 2 {
		return 1
	}
	if numAvail > matchLenMax {
		numAvail = matchLenMax
	}
	buf := p.mf.buf
	data := p.mf.bufPos - 1
	var repLen, repIndex uint32

	for i := range uint32(numReps) {
		data2 := data - int(p.reps[i])
		if buf[data] != buf[data2] || buf[data+1] != buf[data2+1] {
			continue
		}
		a := buf[data : data+int(numAvail)]
		b := buf[data2 : data2+len(a)]
		l := matchLen(a, b, 2)
		if uint32(l) >= p.numFastBytes {
			p.backRes = i
			p.movePos(uint32(l) - 1)
			return uint32(l)
		}
		if uint32(l) > repLen {
			repIndex = i
			repLen = uint32(l)
		}
	}

	if mainLen >= p.numFastBytes {
		p.backRes = p.matches[numPairs-1] + numReps
		p.movePos(mainLen - 1)
		return mainLen
	}

	mainDist := uint32(0)
	if mainLen >= 2 {
		mainDist = p.matches[numPairs-1]
		for numPairs > 2 {
			if mainLen != p.matches[numPairs-4]+1 {
				break
			}
			dist2 := p.matches[numPairs-3]
			if !changePair(dist2, mainDist) {
				break
			}
			numPairs -= 2
			mainLen--
			mainDist = dist2
		}
		if mainLen == 2 && mainDist >= 0x80 {
			mainLen = 1
		}
	}

	if repLen >= 2 &&
		(repLen+1 >= mainLen ||
			(repLen+2 >= mainLen && mainDist >= 1<<9) ||
			(repLen+3 >= mainLen && mainDist >= 1<<15)) {
		p.backRes = repIndex
		p.movePos(repLen - 1)
		return repLen
	}

	if mainLen < 2 || numAvail <= 2 {
		return 1
	}

	{
		len1, np := p.readMatchDistances()
		p.numPairs = np
		p.longestMatchLen = len1
		if len1 >= 2 {
			newDist := p.matches[np-1]
			if (len1 >= mainLen && newDist < mainDist) ||
				(len1 == mainLen+1 && !changePair(mainDist, newDist)) ||
				len1 > mainLen+1 ||
				(len1+1 >= mainLen && mainLen >= 3 && changePair(newDist, mainDist)) {
				return 1
			}
		}
	}

	data = p.mf.bufPos - 1
	for i := range uint32(numReps) {
		data2 := data - int(p.reps[i])
		if buf[data] != buf[data2] || buf[data+1] != buf[data2+1] {
			continue
		}
		limit := mainLen - 1
		for l := uint32(2); ; l++ {
			if l >= limit {
				return 1
			}
			if buf[data+int(l)] != buf[data2+int(l)] {
				break
			}
		}
	}

	p.backRes = mainDist + numReps
	if mainLen != 2 {
		p.movePos(mainLen - 2)
	}
	return mainLen
}

func (p *encoder) writeEndMarker(posState uint32) {
	rc := &p.rc
	rc.encodeBit1(&p.isMatch[p.state][posState])
	rc.encodeBit0(&p.isRep[p.state])
	p.state = uint32(matchNextStates[p.state])
	rc.lenEncode(&p.lenProbs, 0, posState)
	probs := &p.posSlotEncoder[0]
	for m := uint32(1); m < 1<<numPosSlotBits; m = m<<1 + 1 {
		rc.encodeBit1(&probs[m])
	}
	rc.encodeDirect(1<<(30-numAlignBits)-1, 30-numAlignBits)
	for m := uint32(1); m < alignTableSize; m = m<<1 + 1 {
		rc.encodeBit1(&p.posAlignEncoder[m])
	}
}

// codeAll encodes a single in-memory block: it encodes the whole input and
// flushes the coder.
func (p *encoder) codeAll() {
	mf := &p.mf
	rc := &p.rc
	mf.init()
	nowPos32 := uint32(0)

	if mf.avail() == 0 {
		p.flush(nowPos32)
		return
	}
	p.readMatchDistances()
	rc.encodeBit0(&p.isMatch[0][0])
	curByte := mf.buf[mf.bufPos-int(p.additionalOffset)]
	rc.litEncode(p.litProbs, uint32(curByte))
	p.additionalOffset--
	nowPos32++

	if mf.avail() != 0 {
		for {
			var l uint32
			if p.fastMode {
				l = p.getOptimumFast()
			} else {
				oci := p.optCur
				if p.optEnd == oci {
					l = p.getOptimum(nowPos32)
				} else {
					opt := &p.opt[oci]
					l = opt.len
					p.backRes = opt.dist
					p.optCur = oci + 1
				}
			}

			posState := nowPos32 & p.pbMask
			dist := p.backRes
			if dist == markLit {
				rc.encodeBit0(&p.isMatch[p.state][posState])
				data := mf.bufPos - int(p.additionalOffset)
				probs := p.litProbsAt(nowPos32, mf.buf[data-1])
				curByte := mf.buf[data]
				state := p.state
				p.state = uint32(literalNextStates[state])
				if isLitState(state) {
					rc.litEncode(probs, uint32(curByte))
				} else {
					rc.litEncodeMatched(probs, uint32(curByte), uint32(mf.buf[data-int(p.reps[0])]))
				}
			} else {
				rc.encodeBit1(&p.isMatch[p.state][posState])
				if dist < numReps {
					rc.encodeBit1(&p.isRep[p.state])
					if dist == 0 {
						rc.encodeBit0(&p.isRepG0[p.state])
						if l != 1 {
							rc.encodeBit1(&p.isRep0Long[p.state][posState])
						} else {
							rc.encodeBit0(&p.isRep0Long[p.state][posState])
							p.state = uint32(shortRepNextStates[p.state])
						}
					} else {
						rc.encodeBit1(&p.isRepG0[p.state])
						if dist == 1 {
							rc.encodeBit0(&p.isRepG1[p.state])
							dist = p.reps[1]
						} else {
							rc.encodeBit1(&p.isRepG1[p.state])
							if dist == 2 {
								rc.encodeBit0(&p.isRepG2[p.state])
								dist = p.reps[2]
							} else {
								rc.encodeBit1(&p.isRepG2[p.state])
								dist = p.reps[3]
								p.reps[3] = p.reps[2]
							}
							p.reps[2] = p.reps[1]
						}
						p.reps[1] = p.reps[0]
						p.reps[0] = dist
					}
					if l != 1 {
						rc.lenEncode(&p.repLenProbs, l-matchLenMin, posState)
						p.repLenEncCount--
						p.state = uint32(repNextStates[p.state])
					}
				} else {
					rc.encodeBit0(&p.isRep[p.state])
					p.state = uint32(matchNextStates[p.state])
					rc.lenEncode(&p.lenProbs, l-matchLenMin, posState)
					dist -= numReps
					p.reps[3] = p.reps[2]
					p.reps[2] = p.reps[1]
					p.reps[1] = p.reps[0]
					p.reps[0] = dist + 1
					p.matchPriceCount++
					posSlot := getPosSlot(dist)
					{
						sym := posSlot + 1<<numPosSlotBits
						probs := &p.posSlotEncoder[lenToPosState(l)]
						for {
							bit := (sym >> (numPosSlotBits - 1)) & 1
							rc.encodeBit(&probs[(sym>>numPosSlotBits)&(1<<numPosSlotBits-1)], bit)
							sym <<= 1
							if sym >= 1<<(numPosSlotBits*2) {
								break
							}
						}
					}
					if dist >= startPosModelIndex {
						footerBits := (posSlot >> 1) - 1
						if dist < numFullDistances {
							base := (2 | (posSlot & 1)) << footerBits
							rc.reverseEncode(p.posEncoders[base:], footerBits, dist)
						} else {
							rc.encodeDirect(dist>>numAlignBits, footerBits-numAlignBits)
							rc.reverseEncode(p.posAlignEncoder[:], numAlignBits, dist&alignMask)
						}
					}
				}
			}

			nowPos32 += l
			p.additionalOffset -= l

			if p.additionalOffset == 0 {
				if !p.fastMode {
					if p.matchPriceCount >= 64 {
						p.fillAlignPrices()
						p.fillDistancesPrices()
						p.lenEnc.updateTables(1<<p.pb, &p.lenProbs)
					}
					if p.repLenEncCount <= 0 {
						p.repLenEncCount = repLenCount
						p.repLenEnc.updateTables(1<<p.pb, &p.repLenProbs)
					}
				}
				if mf.avail() == 0 {
					break
				}
			}
		}
	}
	p.flush(nowPos32)
}

func (p *encoder) flush(nowPos uint32) {
	if p.writeEndMark {
		p.writeEndMarker(nowPos & p.pbMask)
	}
	p.rc.flushData()
}
