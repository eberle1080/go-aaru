// LZMA decoder.
//
// The decoder is specialised for one-shot use: the output buffer is the
// dictionary, so it is never allocated separately and no data is ever copied
// out of a window.

package lzma

import "encoding/binary"

const (
	numStates2   = 16
	numLitStates = 7
	lenLowBase   = 0
	lenChoice    = lenLowBase
	lenChoice2   = lenLowBase + lenNumLowSymbols
	lenHighBase  = lenLowBase + 2*(pbStatesMax<<lenNumLowBits)
	numLenProbs  = lenHighBase + lenNumHighSymbols
	litSize      = 0x300

	// badRepCode: a first code value at or above it means
	// the stream starts with a rep match, which is invalid.
	range0     = 0xFFFFFFFF
	bound0     = (range0 >> numBitModelBits) << (numBitModelBits - 1)
	badRepCode = bound0 + (((range0 - bound0) >> numBitModelBits) << (numBitModelBits - 1))
)

// decProbs holds the adaptive probabilities of the decoder, one array per
// context kind. The per-state arrays have 16
// entries so that indexing them with state&15 needs no bounds check.
type decProbs struct {
	isMatch    [numStates2 << MaxPB]uint16 // [posState<<4 + state]
	isRep0Long [numStates2 << MaxPB]uint16 // [posState<<4 + state]
	isRep      [numStates2]uint16
	isRepG0    [numStates2]uint16
	isRepG1    [numStates2]uint16
	isRepG2    [numStates2]uint16
	posSlot    [numLenToPosStates << numPosSlotBits]uint16
	specPos    [numFullDistances]uint16
	align      [alignTableSize]uint16
	lenCoder   [numLenProbs]uint16
	repLen     [numLenProbs]uint16
}

func (p *decProbs) init() {
	fill := func(s []uint16) {
		for i := range s {
			s[i] = probInitValue
		}
	}
	fill(p.isMatch[:])
	fill(p.isRep0Long[:])
	fill(p.isRep[:])
	fill(p.isRepG0[:])
	fill(p.isRepG1[:])
	fill(p.isRepG2[:])
	fill(p.posSlot[:])
	fill(p.specPos[:])
	fill(p.align[:])
	fill(p.lenCoder[:])
	fill(p.repLen[:])
}

// The range decoder state (range, code and the input position) lives in
// local variables of decodeRaw so that it stays in registers; norm and bit
// (range normalisation and bit decoding with probability update) are
// functions small enough to be inlined.

// norm normalises the range. Reading past the end of the input yields zero bytes;
// the caller detects it through the input position.
func norm(rng, code uint32, in []byte, ip int) (uint32, uint32, int) {
	if rng < topValue {
		rng <<= 8
		code <<= 8
		if uint(ip) < uint(len(in)) {
			code |= uint32(in[ip])
		}
		ip++
	}
	return rng, code, ip
}

// bit decodes one bit with the adaptive probability *p. It is branch free:
// the bits of literals and distances are hard to predict.
func bit(p *uint16, rng, code uint32) (uint32, uint32, uint32) {
	// bitT, written out to stay within the inlining budget.
	ttt := uint32(*p)
	bound := (rng >> numBitModelBits) * ttt
	mask := uint32(int64(uint64(code)-uint64(bound)) >> 63)
	rng = bound&mask | (rng-bound)&^mask
	code -= bound &^ mask
	*p = uint16(ttt + uint32(int32(mask&(bitModelTotal-(1<<numMoveBits-1))+(1<<numMoveBits-1)-ttt)>>numMoveBits))
	return mask + 1, rng, code
}

// bitT decodes one bit with the probability value ttt, typically loaded
// ahead of time. It returns mask (all ones for a 0 bit, else zero) and the
// updated probability, which the caller stores. It is branch free.
func bitT(ttt, rng, code uint32) (uint32, uint32, uint32, uint32) {
	bound := (rng >> numBitModelBits) * ttt
	mask := uint32(int64(uint64(code)-uint64(bound)) >> 63) // code < bound
	rng = bound&mask | (rng-bound)&^mask
	code -= bound &^ mask
	// Probability update, branch free like the encoder's encodeBit.
	ttt += uint32(int32(mask&(bitModelTotal-(1<<numMoveBits-1))+(1<<numMoveBits-1)-ttt) >> numMoveBits)
	return mask, ttt, rng, code
}

// Result of decodeRaw.
type decodeStatus int

const (
	statusOK        decodeStatus = iota
	statusTruncated              // ran out of input
	statusCorrupt                // bad distance or early end marker
)

// newLitProbs returns the initialised literal probabilities for lc+lp bits.
// They are padded by 0x100 entries so that every context can be viewed as
// a [0x400]uint16 array (see decodeRaw).
func newLitProbs(lclp uint32) []uint16 {
	lit := make([]uint16, litSize<<lclp+0x100)
	for i := range lit {
		lit[i] = probInitValue
	}
	return lit
}

// decodeRaw decodes the raw LZMA stream in (starting with the 5 range coder
// initialisation bytes, in[0] == 0 already checked) into exactly len(out)
// bytes, len(out) > 0. lit comes from newLitProbs(lc+lp).
//
// Decoding stops once len(out) bytes are produced, whatever follows in the
// stream. An end marker before that is statusCorrupt.
func decodeRaw(out, in []byte, lc, lp, pb, dictSize uint32, probs *decProbs, lit []uint16) decodeStatus {
	rng := uint32(0xFFFFFFFF)
	code := uint32(in[1])<<24 | uint32(in[2])<<16 | uint32(in[3])<<8 | uint32(in[4])
	if code >= badRepCode {
		return statusCorrupt
	}
	ip := 5
	n := len(out)
	pbMask := uint32(1)<<pb - 1
	lpMask := uint32(0x100)<<lp - uint32(0x100)>>lc
	var state uint32
	rep0, rep1, rep2, rep3 := uint32(1), uint32(1), uint32(1), uint32(1)
	dp := 0
	var b uint32
	var prev uint32 // out[dp-1], or 0 at the start

	for dp < n {
		if ip > len(in) {
			return statusTruncated
		}
		processedPos := uint32(dp)
		posState := (processedPos & pbMask) << 4

		rng, code, ip = norm(rng, code, in, ip)
		pm := &probs.isMatch[(posState+state)&0xFF]
		ttt := uint32(*pm)
		bound := (rng >> numBitModelBits) * ttt
		if code < bound {
			rng = bound
			*pm = uint16(ttt + (bitModelTotal-ttt)>>numMoveBits)
			// Literal.
			off := 3 * ((((processedPos << 8) + prev) & lpMask) << lc)
			lit := (*[0x400]uint16)(lit[off : off+0x400])
			sym := uint32(1)
			if state < numLitStates {
				if state < 4 {
					state = 0
				} else {
					state -= 3
				}
				// Both possible next probabilities are loaded ahead
				// so that the next bit does not wait for a load.
				ttt := uint32(lit[1])
				for sym < 0x100 {
					t0 := uint32(lit[(sym<<1)&0x3FF])
					t1 := uint32(lit[(sym<<1|1)&0x3FF])
					var mask, nt uint32
					rng, code, ip = norm(rng, code, in, ip)
					mask, nt, rng, code = bitT(ttt, rng, code)
					lit[sym] = uint16(nt)
					b = mask + 1
					sym = sym<<1 | b
					ttt = t1 ^ (t0^t1)&mask
				}
			} else {
				matchByte := uint32(out[dp-int(rep0)])
				const offs uint32 = 0x100
				if state < 10 {
					state -= 3
				} else {
					state -= 6
				}
				// Matched literal decoding with offs split into bt (the value
				// before masking) and o = bt & matchByte, again loading
				// both possible next probabilities ahead.
				mb := matchByte << 1
				bt := offs
				o := bt & mb
				ttt := uint32(lit[(o+bt+1)&0x3FF])
				for sym < 0x100 {
					offs0 := bt &^ mb // offs after a 0 bit
					offs1 := o        // offs after a 1 bit
					mbn := mb << 1
					t0 := uint32(lit[(offs0&mbn+offs0+sym<<1)&0x3FF])
					t1 := uint32(lit[(offs1&mbn+offs1+sym<<1|1)&0x3FF])
					var mask, nt uint32
					rng, code, ip = norm(rng, code, in, ip)
					mask, nt, rng, code = bitT(ttt, rng, code)
					lit[(o+bt+sym)&0x3FF] = uint16(nt)
					b = mask + 1
					sym = sym<<1 | b
					bt = offs1 ^ (offs0^offs1)&mask
					mb = mbn
					o = bt & mb
					ttt = t1 ^ (t0^t1)&mask
				}
			}
			out[dp] = byte(sym)
			prev = sym & 0xFF
			dp++
			continue
		}
		rng -= bound
		code -= bound
		*pm = uint16(ttt - ttt>>numMoveBits)

		rng, code, ip = norm(rng, code, in, ip)
		b, rng, code = bit(&probs.isRep[state&15], rng, code)
		isMatch := b == 0
		var lenProbs *[numLenProbs]uint16
		if isMatch {
			lenProbs = &probs.lenCoder
		} else {
			rng, code, ip = norm(rng, code, in, ip)
			b, rng, code = bit(&probs.isRepG0[state&15], rng, code)
			if b == 0 {
				rng, code, ip = norm(rng, code, in, ip)
				b, rng, code = bit(&probs.isRep0Long[(posState+state)&0xFF], rng, code)
				if b == 0 {
					// Short rep.
					c := out[dp-int(rep0)]
					out[dp] = c
					prev = uint32(c)
					dp++
					if state < numLitStates {
						state = 9
					} else {
						state = 11
					}
					continue
				}
			} else {
				var dist uint32
				rng, code, ip = norm(rng, code, in, ip)
				b, rng, code = bit(&probs.isRepG1[state&15], rng, code)
				if b == 0 {
					dist = rep1
				} else {
					rng, code, ip = norm(rng, code, in, ip)
					b, rng, code = bit(&probs.isRepG2[state&15], rng, code)
					if b == 0 {
						dist = rep2
					} else {
						dist = rep3
						rep3 = rep2
					}
					rep2 = rep1
				}
				rep1 = rep0
				rep0 = dist
			}
			if state < numLitStates {
				state = 8
			} else {
				state = 11
			}
			lenProbs = &probs.repLen
		}

		// Length.
		var l uint32
		rng, code, ip = norm(rng, code, in, ip)
		b, rng, code = bit(&lenProbs[lenChoice], rng, code)
		if b == 0 {
			low := (*[16]uint16)(lenProbs[lenLowBase+posState:])
			l = 1
			ttt := uint32(low[1])
			for range 3 {
				t0 := uint32(low[(l<<1)&15])
				t1 := uint32(low[(l<<1|1)&15])
				var mask, nt uint32
				rng, code, ip = norm(rng, code, in, ip)
				mask, nt, rng, code = bitT(ttt, rng, code)
				low[l&15] = uint16(nt)
				l = l<<1 | (mask + 1)
				ttt = t1 ^ (t0^t1)&mask
			}
			l -= 8
		} else {
			rng, code, ip = norm(rng, code, in, ip)
			b, rng, code = bit(&lenProbs[lenChoice2], rng, code)
			if b == 0 {
				mid := (*[16]uint16)(lenProbs[lenLowBase+posState+lenNumLowSymbols:])
				l = 1
				ttt := uint32(mid[1])
				for range 3 {
					t0 := uint32(mid[(l<<1)&15])
					t1 := uint32(mid[(l<<1|1)&15])
					var mask, nt uint32
					rng, code, ip = norm(rng, code, in, ip)
					mask, nt, rng, code = bitT(ttt, rng, code)
					mid[l&15] = uint16(nt)
					l = l<<1 | (mask + 1)
					ttt = t1 ^ (t0^t1)&mask
				}
			} else {
				high := (*[lenNumHighSymbols]uint16)(lenProbs[lenHighBase:])
				l = 1
				ttt := uint32(high[1])
				for l < lenNumHighSymbols {
					t0 := uint32(high[(l<<1)&0xFF])
					t1 := uint32(high[(l<<1|1)&0xFF])
					var mask, nt uint32
					rng, code, ip = norm(rng, code, in, ip)
					mask, nt, rng, code = bitT(ttt, rng, code)
					high[l&0xFF] = uint16(nt)
					l = l<<1 | (mask + 1)
					ttt = t1 ^ (t0^t1)&mask
				}
				l = l - lenNumHighSymbols + lenNumLowSymbols*2
			}
		}

		if isMatch {
			// Distance.
			slots := (*[1 << numPosSlotBits]uint16)(probs.posSlot[min(l, numLenToPosStates-1)<<numPosSlotBits:])
			slot := uint32(1)
			ttt := uint32(slots[1])
			for range numPosSlotBits {
				t0 := uint32(slots[(slot<<1)&63])
				t1 := uint32(slots[(slot<<1|1)&63])
				var mask, nt uint32
				rng, code, ip = norm(rng, code, in, ip)
				mask, nt, rng, code = bitT(ttt, rng, code)
				slots[slot&63] = uint16(nt)
				slot = slot<<1 | (mask + 1)
				ttt = t1 ^ (t0^t1)&mask
			}
			slot -= 1 << numPosSlotBits
			dist := slot
			if slot >= startPosModelIndex {
				numDirectBits := (slot >> 1) - 1
				dist = 2 | slot&1
				if slot < endPosModelIndex {
					dist <<= numDirectBits
					sp := &probs.specPos
					base := dist
					m := uint32(1)
					ttt := uint32(sp[(base+1)&(numFullDistances-1)])
					for i := range numDirectBits {
						t0 := uint32(sp[(base+m<<1)&(numFullDistances-1)])
						t1 := uint32(sp[(base+m<<1|1)&(numFullDistances-1)])
						var mask, nt uint32
						rng, code, ip = norm(rng, code, in, ip)
						mask, nt, rng, code = bitT(ttt, rng, code)
						sp[(base+m)&(numFullDistances-1)] = uint16(nt)
						b = mask + 1
						m = m<<1 | b
						dist |= b << i
						ttt = t1 ^ (t0^t1)&mask
					}
				} else {
					for range numDirectBits - numAlignBits {
						rng, code, ip = norm(rng, code, in, ip)
						rng >>= 1
						code -= rng
						t := -(code >> 31)
						dist = dist<<1 + t + 1
						code += rng & t
					}
					dist <<= numAlignBits
					al := &probs.align
					m := uint32(1)
					ttt := uint32(al[1])
					for i := range uint32(numAlignBits) {
						t0 := uint32(al[(m<<1)&15])
						t1 := uint32(al[(m<<1|1)&15])
						var mask, nt uint32
						rng, code, ip = norm(rng, code, in, ip)
						mask, nt, rng, code = bitT(ttt, rng, code)
						al[m&15] = uint16(nt)
						b = mask + 1
						m = m<<1 | b
						dist |= b << i
						ttt = t1 ^ (t0^t1)&mask
					}
					if dist == 0xFFFFFFFF {
						// End marker before len(out) bytes.
						if ip > len(in) {
							return statusTruncated
						}
						return statusCorrupt
					}
				}
			}
			rep3, rep2, rep1, rep0 = rep2, rep1, rep0, dist+1
			if state < numLitStates {
				state = numLitStates
			} else {
				state = numLitStates + 3
			}
			if uint64(dist) >= min(uint64(dp), uint64(dictSize)) {
				if ip > len(in) {
					return statusTruncated
				}
				return statusCorrupt
			}
		}

		// Copy the match. Decoding stops
		// silently when the output is full, even within a match.
		l += matchLenMin
		if n-dp < int(l) {
			l = uint32(n - dp)
		}
		src := dp - int(rep0)
		switch {
		case rep0 >= 8 && l <= 64 && n-dp >= int(l)+8:
			// Copy 8 bytes at a time without calling memmove (a call
			// spills every register). This may write up to 7 bytes past
			// the match, which later symbols overwrite before anything
			// reads them; a distance of at least 8 makes the overlapping
			// copy behave like the byte by byte one.
			for i := 0; i < int(l); i += 8 {
				binary.LittleEndian.PutUint64(out[dp+i:], binary.LittleEndian.Uint64(out[src+i:]))
			}
		case int(rep0) >= int(l):
			copy(out[dp:dp+int(l)], out[src:src+int(l)])
		default:
			o := out[src : dp+int(l)]
			for i := 0; i < int(l); i++ {
				o[int(rep0)+i] = o[i]
			}
		}
		dp += int(l)
		prev = uint32(out[dp-1])
	}

	// Final normalisation: the input must hold the last symbol completely.
	// What follows in the stream (more data, an end marker or nothing) is
	// not looked at.
	_, _, ip = norm(rng, code, in, ip)
	if ip > len(in) {
		return statusTruncated
	}
	return statusOK
}
