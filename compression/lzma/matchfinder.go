// Match finder for the LZMA encoder.
//
// Only the modes reachable through Options are implemented: the bt4 binary
// tree match finder (normal mode, level >= 5) and the hc5 hash chain match
// finder (fast mode, level < 5), both reading the whole input from memory
// ("direct input").

package lzma

import (
	"encoding/binary"
	"math/bits"
)

const (
	hash2Size    = 1 << 10
	hash3Size    = 1 << 16
	fix3HashSize = hash2Size
	fix4HashSize = hash2Size + hash3Size
	fix5HashSize = fix4HashSize
	lzHashShift1 = 5
	lzHashShift2 = 10
	emptyHashVal = 0
)

// crcTable is the CRC-32 table used to spread hash values.
var crcTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i)
		for range 8 {
			r = (r >> 1) ^ (0xEDB88320 & -(r & 1))
		}
		t[i] = r
	}
	return t
}()

// maxValForNormalize is the position at which the
// match finder rebases its positions. Zero means "when pos wraps around
// 2^32". Tests lower it to exercise normalize.
var maxValForNormalize uint32

// matchFinder is the match finder state in direct input mode: buf holds the
// whole input and bufPos is the index of the current byte.
type matchFinder struct {
	buf    []byte
	bufPos int

	pos              uint32
	posLimit         uint32
	streamPos        uint32
	lenLimit         uint32
	cyclicBufferPos  uint32
	cyclicBufferSize uint32

	streamEndWasReached bool
	btMode              bool

	matchMaxLen uint32
	hash        []uint32
	son         []uint32
	hashMask    uint32
	cutValue    uint32

	keepSizeAfter  uint32
	numHashBytes   uint32
	directInputRem uint64
	historySize    uint32
	maxValForNorm  uint32
}

// create allocates the match finder for direct input. It sizes the cyclic
// buffer for min(historySize, len(input)) positions: no
// match can reach further back than the input length, so this changes
// nothing but the amount of memory used.
func (p *matchFinder) create(src []byte, historySize, matchMaxLen, keepAddBufferAfter uint32) {
	expectedDataSize := uint64(len(src))
	keepAddBufferAfter += matchMaxLen
	if keepAddBufferAfter < p.numHashBytes {
		keepAddBufferAfter = p.numHashBytes
	}
	p.keepSizeAfter = keepAddBufferAfter
	p.matchMaxLen = matchMaxLen
	p.historySize = historySize

	var hs uint32 = 1<<16 - 1
	if p.numHashBytes != 2 {
		hs = historySize
		if uint64(hs) > expectedDataSize {
			hs = uint32(expectedDataSize)
		}
		if hs != 0 {
			hs--
		}
		hs |= hs >> 1
		hs |= hs >> 2
		hs |= hs >> 4
		hs |= hs >> 8
		hs >>= 1
		if hs >= 1<<24 {
			if p.numHashBytes == 3 {
				hs = 1<<24 - 1
			} else {
				hs >>= 1
			}
		}
		hs |= 1<<16 - 1
		if p.numHashBytes >= 5 {
			hs |= 256<<lzHashShift2 - 1
		}
	}
	p.hashMask = hs
	hs++
	var fixed uint32
	if p.numHashBytes > 2 {
		fixed += hash2Size
	}
	if p.numHashBytes > 3 {
		fixed += hash3Size
	}
	hs += fixed

	cyc := uint64(historySize)
	if cyc > expectedDataSize {
		cyc = expectedDataSize
	}
	p.cyclicBufferSize = uint32(cyc) + 1
	numSons := int(p.cyclicBufferSize)
	if p.btMode {
		numSons <<= 1
	}
	p.hash = make([]uint32, hs)
	p.son = make([]uint32, numSons)
	p.buf = src
	p.directInputRem = expectedDataSize
	p.maxValForNorm = maxValForNormalize
}

// init resets the match finder. The hash table is already zeroed.
func (p *matchFinder) init() {
	p.bufPos = 0
	p.pos = 1
	p.streamPos = 1
	p.streamEndWasReached = false
	p.readBlock()
	p.cyclicBufferPos = p.pos
	p.setLimits()
}

func (p *matchFinder) avail() uint32 { return p.streamPos - p.pos }

func (p *matchFinder) readBlock() {
	if p.streamEndWasReached {
		return
	}
	curSize := uint64(0xFFFFFFFF - p.avail())
	if curSize > p.directInputRem {
		curSize = p.directInputRem
	}
	p.directInputRem -= curSize
	p.streamPos += uint32(curSize)
	if p.directInputRem == 0 {
		p.streamEndWasReached = true
	}
}

func (p *matchFinder) setLimits() {
	n := p.maxValForNorm - p.pos
	if n == 0 {
		n = 0xFFFFFFFF
	}
	k := p.cyclicBufferSize - p.cyclicBufferPos
	if k < n {
		n = k
	}
	k = p.avail()
	ksa := p.keepSizeAfter
	mm := p.matchMaxLen
	if k > ksa {
		k -= ksa
	} else if k >= mm {
		k -= mm
		k++
	} else {
		mm = k
		if k != 0 {
			k = 1
		}
	}
	p.lenLimit = mm
	if k < n {
		n = k
	}
	p.posLimit = p.pos + n
}

// checkLimits refills, rebases and wraps the match finder as needed.
func (p *matchFinder) checkLimits() {
	if p.keepSizeAfter == p.avail() {
		p.readBlock()
	}
	if p.pos == p.maxValForNorm && p.avail() >= p.numHashBytes {
		subValue := p.pos - p.historySize - 1
		p.pos -= subValue
		p.streamPos -= subValue
		normalize(subValue, p.hash)
		normalize(subValue, p.son)
	}
	if p.cyclicBufferPos == p.cyclicBufferSize {
		p.cyclicBufferPos = 0
	}
	p.setLimits()
}

// normalize rebases items by subValue: a saturating subtraction.
func normalize(subValue uint32, items []uint32) {
	for i, v := range items {
		if v < subValue {
			v = subValue
		}
		items[i] = v - subValue
	}
}

func (p *matchFinder) movePos() {
	p.cyclicBufferPos++
	p.bufPos++
	p.pos++
	if p.pos == p.posLimit {
		p.checkLimits()
	}
}

// getMatches stores (len, dist-1) pairs of increasing length into d and
// returns the number of values stored.
func (p *matchFinder) getMatches(d []uint32) int {
	if p.btMode {
		return p.bt4GetMatches(d)
	}
	return p.hc5GetMatches(d)
}

func (p *matchFinder) skip(num uint32) {
	if p.btMode {
		p.bt4Skip(num)
	} else {
		p.hc5Skip(num)
	}
}

func (p *matchFinder) bt4GetMatches(d []uint32) int {
	lenLimit := p.lenLimit
	if lenLimit < 4 {
		p.movePos()
		return 0
	}
	buf := p.buf
	cur := p.bufPos
	c := buf[cur : cur+4 : cur+4]
	temp := crcTable[c[0]] ^ uint32(c[1])
	h2 := temp & (hash2Size - 1)
	temp ^= uint32(c[2]) << 8
	h3 := temp & (hash3Size - 1)
	hv := (temp ^ crcTable[c[3]]<<lzHashShift1) & p.hashMask

	hash := p.hash
	pos := p.pos
	d2 := pos - hash[h2]
	d3 := pos - hash[fix3HashSize+h3]
	curMatch := hash[fix4HashSize+hv]
	hash[h2] = pos
	hash[fix3HashSize+h3] = pos
	hash[fix4HashSize+hv] = pos

	mmm := p.cyclicBufferSize
	if pos < mmm {
		mmm = pos
	}

	n, dist := shortMatches(buf, cur, c, d2, d3, mmm, d)
	maxLen := uint32(3)
	if dist != 0 {
		maxLen = extendMatch(buf, cur, int(dist), maxLen, lenLimit)
		d[n-2] = maxLen
		if maxLen == lenLimit {
			p.skipMatchesSpec(lenLimit, curMatch)
			p.movePos()
			return n
		}
	}
	n = p.getMatchesSpec1(lenLimit, curMatch, d, n, maxLen)
	p.movePos()
	return n
}

// shortMatches is the common start of the bt4 and hc5 match searches: it
// records the matches found through the 2-
// and 3-byte hash tables (at distances d2 and d3, valid below mmm) into d
// and returns the number of values stored and the distance of the match of
// at least 3 bytes to extend, or 0 if there is none.
func shortMatches(buf []byte, cur int, c []byte, d2, d3, mmm uint32, d []uint32) (int, uint32) {
	if d2 < mmm && buf[cur-int(d2)] == c[0] {
		d[0] = 2
		d[1] = d2 - 1
		if buf[cur-int(d2)+2] == c[2] {
			return 2, d2
		}
		if d3 < mmm && buf[cur-int(d3)] == c[0] {
			d[3] = d3 - 1
			return 4, d3
		}
		return 2, 0
	}
	if d3 < mmm && buf[cur-int(d3)] == c[0] {
		d[1] = d3 - 1
		return 2, d3
	}
	return 0, 0
}

// extendMatch extends a match of maxLen bytes at
// distance dist up to lenLimit bytes.
func extendMatch(buf []byte, cur, dist int, maxLen, lenLimit uint32) uint32 {
	a := buf[cur-dist : cur-dist+int(lenLimit)]
	b := buf[cur : cur+len(a)]
	i := matchLen(a, b, int(maxLen))
	return uint32(i)
}

func (p *matchFinder) bt4Skip(num uint32) {
	for {
		lenLimit := p.lenLimit
		if lenLimit < 4 {
			p.movePos()
		} else {
			cur := p.bufPos
			c := p.buf[cur : cur+4 : cur+4]
			temp := crcTable[c[0]] ^ uint32(c[1])
			h2 := temp & (hash2Size - 1)
			temp ^= uint32(c[2]) << 8
			h3 := temp & (hash3Size - 1)
			hv := (temp ^ crcTable[c[3]]<<lzHashShift1) & p.hashMask
			hash := p.hash
			curMatch := hash[fix4HashSize+hv]
			pos := p.pos
			hash[h2] = pos
			hash[fix3HashSize+h3] = pos
			hash[fix4HashSize+hv] = pos
			p.skipMatchesSpec(lenLimit, curMatch)
			p.movePos()
		}
		num--
		if num == 0 {
			return
		}
	}
}

// getMatchesSpec1 walks the binary tree of
// earlier positions, records matches longer than maxLen into d[n:] and
// re-roots the tree at the current position.
func (p *matchFinder) getMatchesSpec1(lenLimit, curMatch uint32, d []uint32, n int, maxLen uint32) int {
	pos := p.pos
	buf := p.buf
	cur := p.bufPos
	son := p.son
	cbp := p.cyclicBufferPos
	cbs := p.cyclicBufferSize
	cutValue := p.cutValue

	ptr0 := int(cbp)<<1 + 1
	ptr1 := int(cbp) << 1
	var len0, len1 uint32

	cmCheck := pos - cbs
	if pos <= cbs {
		cmCheck = 0
	}
	if cmCheck < curMatch {
		// The current input; every comparison below stays within
		// lenLimit <= avail bytes of it.
		in := buf[cur : cur+int(lenLimit)]
		for {
			delta := pos - curMatch
			pi := cbp - delta
			if delta > cbp {
				pi += cbs
			}
			pi2 := int(pi) << 1
			pair := son[pi2 : pi2+2 : pi2+2]
			pb := buf[cur-int(delta) : cur-int(delta)+len(in)]
			l := min(len0, len1)
			pair0 := pair[0]
			if pb[l] == in[l] {
				l = uint32(matchLen(in, pb, int(l)+1))
				if maxLen < l {
					maxLen = l
					d[n] = l
					d[n+1] = delta - 1
					n += 2
					if l == lenLimit {
						son[ptr1] = pair0
						son[ptr0] = pair[1]
						return n
					}
				}
			}
			if pb[l] < in[l] {
				son[ptr1] = curMatch
				curMatch = pair[1]
				ptr1 = pi2 + 1
				len1 = l
			} else {
				son[ptr0] = curMatch
				curMatch = pair0
				ptr0 = pi2
				len0 = l
			}
			cutValue--
			if cutValue == 0 || cmCheck >= curMatch {
				break
			}
		}
	}
	son[ptr0] = emptyHashVal
	son[ptr1] = emptyHashVal
	return n
}

// skipMatchesSpec is getMatchesSpec1 without
// recording matches.
func (p *matchFinder) skipMatchesSpec(lenLimit, curMatch uint32) {
	pos := p.pos
	buf := p.buf
	cur := p.bufPos
	son := p.son
	cbp := p.cyclicBufferPos
	cbs := p.cyclicBufferSize
	cutValue := p.cutValue

	ptr0 := int(cbp)<<1 + 1
	ptr1 := int(cbp) << 1
	var len0, len1 uint32

	cmCheck := pos - cbs
	if pos <= cbs {
		cmCheck = 0
	}
	if cmCheck < curMatch {
		in := buf[cur : cur+int(lenLimit)]
		for {
			delta := pos - curMatch
			pi := cbp - delta
			if delta > cbp {
				pi += cbs
			}
			pi2 := int(pi) << 1
			pair := son[pi2 : pi2+2 : pi2+2]
			pb := buf[cur-int(delta) : cur-int(delta)+len(in)]
			l := min(len0, len1)
			if pb[l] == in[l] {
				l = uint32(matchLen(in, pb, int(l)+1))
				if l == lenLimit {
					son[ptr1] = pair[0]
					son[ptr0] = pair[1]
					return
				}
			}
			if pb[l] < in[l] {
				son[ptr1] = curMatch
				curMatch = pair[1]
				ptr1 = pi2 + 1
				len1 = l
			} else {
				son[ptr0] = curMatch
				curMatch = pair[0]
				ptr0 = pi2
				len0 = l
			}
			cutValue--
			if cutValue == 0 || cmCheck >= curMatch {
				break
			}
		}
	}
	son[ptr0] = emptyHashVal
	son[ptr1] = emptyHashVal
}

func (p *matchFinder) hc5GetMatches(d []uint32) int {
	lenLimit := p.lenLimit
	if lenLimit < 5 {
		p.movePos()
		return 0
	}
	buf := p.buf
	cur := p.bufPos
	c := buf[cur : cur+5 : cur+5]
	temp := crcTable[c[0]] ^ uint32(c[1])
	h2 := temp & (hash2Size - 1)
	temp ^= uint32(c[2]) << 8
	h3 := temp & (hash3Size - 1)
	temp ^= crcTable[c[3]] << lzHashShift1
	hv := (temp ^ crcTable[c[4]]<<lzHashShift2) & p.hashMask

	hash := p.hash
	pos := p.pos
	d2 := pos - hash[h2]
	d3 := pos - hash[fix3HashSize+h3]
	curMatch := hash[fix5HashSize+hv]
	hash[h2] = pos
	hash[fix3HashSize+h3] = pos
	hash[fix5HashSize+hv] = pos

	mmm := p.cyclicBufferSize
	if pos < mmm {
		mmm = pos
	}

	n, dist := shortMatches(buf, cur, c, d2, d3, mmm, d)
	maxLen := uint32(4)
	if dist != 0 {
		d[n-2] = 3
		if buf[cur-int(dist)+3] == c[3] {
			maxLen = extendMatch(buf, cur, int(dist), maxLen, lenLimit)
			d[n-2] = maxLen
			if maxLen == lenLimit {
				p.son[p.cyclicBufferPos] = curMatch
				p.movePos()
				return n
			}
		}
	}
	n = p.hcGetMatchesSpec(lenLimit, curMatch, d, n, maxLen)
	p.movePos()
	return n
}

// hcGetMatchesSpec follows the hash chain.
func (p *matchFinder) hcGetMatchesSpec(lenLimit, curMatch uint32, d []uint32, n int, maxLen uint32) int {
	pos := p.pos
	buf := p.buf
	cur := p.bufPos
	son := p.son
	cbp := p.cyclicBufferPos
	cbs := p.cyclicBufferSize
	cutValue := p.cutValue

	son[cbp] = curMatch
	in := buf[cur : cur+int(lenLimit)]
	for {
		if curMatch == 0 {
			break
		}
		delta := pos - curMatch
		if delta >= cbs {
			break
		}
		i := cbp - delta
		if delta > cbp {
			i += cbs
		}
		curMatch = son[i]
		pb := buf[cur-int(delta) : cur-int(delta)+len(in)]
		if in[maxLen] == pb[maxLen] {
			l := matchLen(in, pb, 0)
			if l == len(in) {
				d[n] = lenLimit
				d[n+1] = delta - 1
				return n + 2
			}
			if maxLen < uint32(l) {
				maxLen = uint32(l)
				d[n] = uint32(l)
				d[n+1] = delta - 1
				n += 2
			}
		}
		cutValue--
		if cutValue == 0 {
			break
		}
	}
	return n
}

func (p *matchFinder) hc5Skip(num uint32) {
	for {
		if p.lenLimit < 5 {
			p.movePos()
			num--
		} else {
			pos := p.pos
			num2 := num
			if rem := p.posLimit - pos; num2 > rem {
				num2 = rem
			}
			num -= num2
			sonIdx := p.cyclicBufferPos
			p.cyclicBufferPos += num2
			cur := p.bufPos
			buf := p.buf
			hash := p.hash
			son := p.son
			hashMask := p.hashMask
			for {
				c := buf[cur : cur+5 : cur+5]
				temp := crcTable[c[0]] ^ uint32(c[1])
				h2 := temp & (hash2Size - 1)
				temp ^= uint32(c[2]) << 8
				h3 := temp & (hash3Size - 1)
				temp ^= crcTable[c[3]] << lzHashShift1
				hv := (temp ^ crcTable[c[4]]<<lzHashShift2) & hashMask
				curMatch := hash[fix5HashSize+hv]
				hash[h2] = pos
				hash[fix3HashSize+h3] = pos
				hash[fix5HashSize+hv] = pos
				cur++
				pos++
				son[sonIdx] = curMatch
				sonIdx++
				num2--
				if num2 == 0 {
					break
				}
			}
			p.bufPos = cur
			p.pos = pos
			if pos == p.posLimit {
				p.checkLimits()
			}
		}
		if num == 0 {
			return
		}
	}
}

// matchLen returns the index of the first byte at or after i where a and b
// differ, or len(a) if there is none. len(b) must be at least len(a). It
// compares eight bytes at a time.
func matchLen(a, b []byte, i int) int {
	b = b[:len(a)]
	for i+8 <= len(a) {
		x := binary.LittleEndian.Uint64(a[i:]) ^ binary.LittleEndian.Uint64(b[i:])
		if x != 0 {
			return i + bits.TrailingZeros64(x)>>3
		}
		i += 8
	}
	for i < len(a) && a[i] == b[i] {
		i++
	}
	return i
}
