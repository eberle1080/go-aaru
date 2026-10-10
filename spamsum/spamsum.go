// Package spamsum implements the SpamSum (ssdeep) context-triggered piecewise
// fuzzy hash that AaruFormat images record, producing digests such as
//
//	24576:3dvzuAsHTQ16pc7O1Q/gS9qze+Swwn9s6IX:8/TQQpaVqze+JN6IX
//
// The digest has the form "blocksize:hash1:hash2" where hash1 is computed with
// the chosen block size and hash2 with twice that block size; each hash is at
// most 64 characters of the standard base64 alphabet.
//
// # Compatibility notes
//
// The block size of index i is computed as the signed 32-bit expression
// 3 << i. For the last index (30) that overflows and the value is
// sign-extended to 0xFFFFFFFFC0000000 when combined with 64-bit operands.
// Compared with evaluating 3 << i in unsigned arithmetic, this only changes
// results for inputs of several tens of gigabytes:
//
//   - the block hash at index 30 never reaches a trigger point, so for inputs
//     larger than 3<<28*64 bytes (48 GiB), where block index 29 is chosen,
//     the second hash is empty or a single character;
//   - inputs larger than 192 GiB do not fail with an overflow error
//     (block index 29 keeps being chosen);
//   - if block index 30 were ever selected its size would be printed as
//     18446744072635809792 instead of 3221225472 (not reachable in practice).
//
// The rolling-hash position counter is a uint32, so it wraps after 4 GiB.
package spamsum

import "strconv"

const (
	spamsumLength  = 64
	numBlockhashes = 31
	rollingWindow  = 7
	hashInit       = 0x28021967
	hashPrime      = 0x01000193
	minBlocksize   = 3

	// MaxDigestLength is the maximum length of a digest returned by
	// [Hasher.Sum].
	MaxDigestLength = 2*spamsumLength + 20 - 1
)

// b64 is the base64 alphabet used to encode hash values.
const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// blockSizes[i] is the block size of index i as a sign-extended 32-bit value:
// uint64(int32(3 << i)). Index 30 overflows int32 and becomes
// 0xFFFFFFFFC0000000; see the package documentation.
var blockSizes = func() (bs [numBlockhashes]uint64) {
	for i := range bs {
		bs[i] = uint64(int64(int32(uint32(minBlocksize) << i)))
	}
	return bs
}()

// blockhash is one piecewise hash at a given block size.
type blockhash struct {
	h          uint32
	halfH      uint32
	digest     [spamsumLength]byte
	halfDigest byte
	dLen       uint32
}

// rollState is the rolling hash over the last rollingWindow bytes.
type rollState struct {
	window [rollingWindow]byte
	h1     uint32
	h2     uint32
	h3     uint32
	n      uint32
}

// Hasher computes a SpamSum digest incrementally. It implements
// io.Writer. The zero value is ready to use; [New] is provided for symmetry.
// A Hasher is not safe for concurrent use.
type Hasher struct {
	bhStart   uint32
	bhEnd     uint32
	bh        [numBlockhashes]blockhash
	totalSize uint64
	roll      rollState
}

// New returns a Hasher ready to accept data.
func New() *Hasher {
	s := new(Hasher)
	s.Reset()
	return s
}

// Reset discards all state, returning s to its initial state.
func (s *Hasher) Reset() {
	*s = Hasher{}
	s.bhEnd = 1
	s.bh[0].h = hashInit
	s.bh[0].halfH = hashInit
}

// Write feeds p into the hash. It always returns
// len(p), nil. The digest does not depend on how the input is split across
// calls.
func (s *Hasher) Write(p []byte) (int, error) {
	if s.bhEnd == 0 {
		s.Reset()
	}

	// The rolling hash state lives in locals for speed and is stored back at
	// the end.
	h1, h2, h3, n, window := s.roll.h1, s.roll.h2, s.roll.h3, s.roll.n, s.roll.window

	for _, c := range p {
		// Update the rolling hash ...
		cc := uint32(c)
		h2 -= h1
		h2 += rollingWindow * cc
		w := n % rollingWindow
		h1 += cc
		h1 -= uint32(window[w])
		window[w] = c
		n++
		h3 = h3<<5 ^ cc
		h := uint64(h1 + h2 + h3)

		// ... and the piecewise hashes of every live block size ...
		bh := s.bh[s.bhStart:s.bhEnd]
		for i := range bh {
			bh[i].h = bh[i].h*hashPrime ^ cc
			bh[i].halfH = bh[i].halfH*hashPrime ^ cc
		}

		// ... then emit digest characters at trigger points. A trigger for a
		// larger block size implies one for every smaller block size, so the
		// smallest live one is checked here and the rest in emit.
		if bs := blockSizes[s.bhStart]; h%bs == bs-1 {
			s.emit(h)
		}

		// Adding the length per byte rather than after the whole buffer is
		// equivalent to writing one byte at a time, which yields the same
		// digest and lets unused small block sizes be dropped early even for
		// one huge Write.
		s.totalSize++
	}

	s.roll.h1, s.roll.h2, s.roll.h3, s.roll.n, s.roll.window = h1, h2, h3, n, window
	return len(p), nil
}

// emit handles the trigger points of the current byte, whose rolling hash is
// h.
func (s *Hasher) emit(h uint64) {
	for i := s.bhStart; i < s.bhEnd; i++ {
		bs := blockSizes[i]
		// Once this is false for one block size it is false for all larger
		// ones: h == -1 (mod 2*bs) implies h == -1 (mod bs).
		if h%bs != bs-1 {
			break
		}

		// We have hit a reset point: emit a hash of the piece of the message
		// since the previous reset point.
		b := &s.bh[i]
		if b.dLen == 0 {
			s.tryForkBlockhash()
		}

		b.digest[b.dLen] = b64[b.h%64]
		b.halfDigest = b64[b.halfH%64]

		if b.dLen < spamsumLength-1 {
			// Only reset the hash while there is room for more characters;
			// otherwise the tail pieces are folded into the last character.
			b.dLen++
			b.digest[b.dLen] = 0
			b.h = hashInit

			if b.dLen >= spamsumLength/2 {
				continue
			}

			b.halfH = hashInit
			b.halfDigest = 0
		} else {
			s.tryReduceBlockhash()
		}
	}
}

// tryReduceBlockhash drops the smallest block size once it can no longer be
// selected.
func (s *Hasher) tryReduceBlockhash() {
	if s.bhEnd-s.bhStart < 2 { // Need at least two working hashes.
		return
	}
	if blockSizes[s.bhStart]*spamsumLength >= s.totalSize {
		// The initial block size estimate would select this or a smaller one.
		return
	}
	if s.bh[s.bhStart+1].dLen < spamsumLength/2 {
		// The estimate adjustment would select this block size.
		return
	}
	s.bhStart++
}

// tryForkBlockhash starts the next larger block hash from the current largest
// one.
func (s *Hasher) tryForkBlockhash() {
	if s.bhEnd >= numBlockhashes {
		return
	}
	obh := &s.bh[s.bhEnd-1]
	nbh := &s.bh[s.bhEnd]
	nbh.h = obh.h
	nbh.halfH = obh.halfH
	nbh.digest[0] = 0
	nbh.halfDigest = 0
	nbh.dLen = 0
	s.bhEnd++
}

// appendElim appends c to out unless the current digest part (which starts
// at out[start]) already ends in three copies of c, eliminating long runs
// of the final character.
func appendElim(out []byte, start int, c byte) []byte {
	n := len(out)
	if n-start >= 3 && out[n-1] == c && out[n-2] == c && out[n-3] == c {
		return out
	}
	return append(out, c)
}

// Sum returns the SpamSum digest of the data written so far. It does not
// change the state, so more data may be
// written afterwards. The digest of empty input is "3::".
//
// No block size is large enough only for more than 2^64-2^36 bytes of input
// (see the package documentation for the block size arithmetic). Sum
// returns "" in that case.
func (s *Hasher) Sum() string {
	if s.bhEnd == 0 {
		s.Reset()
	}

	bi := s.bhStart
	h := s.roll.h1 + s.roll.h2 + s.roll.h3

	// Initial block size guess.
	for blockSizes[bi]*spamsumLength < s.totalSize {
		bi++
		if bi >= numBlockhashes {
			return ""
		}
	}

	// Adapt the block size guess to the actual digest lengths.
	for bi >= s.bhEnd {
		bi--
	}
	for bi > s.bhStart && s.bh[bi].dLen < spamsumLength/2 {
		bi--
	}

	out := make([]byte, 0, 2*spamsumLength+24)
	out = strconv.AppendUint(out, blockSizes[bi], 10)
	out = append(out, ':')

	b := &s.bh[bi]
	start := len(out)
	out = append(out, b.digest[:b.dLen]...)
	if h != 0 {
		out = appendElim(out, start, b64[b.h%64])
	} else if b.digest[b.dLen] != 0 {
		out = appendElim(out, start, b.digest[b.dLen])
	}
	out = append(out, ':')

	if bi < s.bhEnd-1 {
		b2 := &s.bh[bi+1]
		start = len(out)
		out = append(out, b2.digest[:b2.dLen]...)
		if h != 0 {
			out = appendElim(out, start, b64[b2.halfH%64])
		} else if b2.halfDigest != 0 {
			// For digests shorter than three characters the run check
			// reaches back to the ':' separator, so appendElim is equivalent.
			out = appendElim(out, start, b2.halfDigest)
		}
	} else if h != 0 {
		// This digest has length 1 at most; no sequence elimination needed.
		out = append(out, b64[b.h%64])
	}

	return string(out)
}

// HashBytes returns the SpamSum digest of data.
func HashBytes(data []byte) string {
	var s Hasher
	s.Write(data)
	return s.Sum()
}
