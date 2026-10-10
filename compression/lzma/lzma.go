// Package lzma implements the raw LZMA codec used by AaruFormat images.
//
// AaruFormat stores LZMA data as a 5-byte LZMA properties block (the
// standard LZMA properties: one lc/lp/pb byte followed by the little-endian
// dictionary size) and a raw LZMA stream without the 8-byte uncompressed size
// of the classic .lzma container and without an end-of-stream marker. The
// uncompressed size is always known from the enclosing block header. On disk
// the properties immediately precede the stream (see DecodePayload and
// EncodePayload).
//
// The codec is pure Go. Encode is deterministic: the same input and options
// always produce the same stream. Decode accepts conforming raw LZMA
// streams, with or without an end-of-stream marker.
package lzma

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// PropsLen is the length of the LZMA properties block.
const PropsLen = 5

// Limits of the LZMA parameters.
const (
	MaxLC = 8
	MaxLP = 4
	MaxPB = 4

	// MinFB and MaxFB bound the number of fast bytes. Out of range values
	// are clamped.
	MinFB = 5
	MaxFB = 273

	// MinDictSize is the smallest dictionary size; smaller values in
	// properties are treated as MinDictSize by decoders.
	MinDictSize = 1 << 12

	// MaxDictSize is the largest dictionary size the encoder accepts; larger
	// requests are clamped to it.
	MaxDictSize = 15 << 28
)

// Errors returned by the codec.
var (
	// ErrInvalidProperties reports a properties block that is too short or
	// has an lc/lp/pb byte out of range.
	ErrInvalidProperties = errors.New("lzma: invalid properties")

	// ErrTruncated reports that the compressed stream ended before the
	// requested number of bytes was decoded.
	ErrTruncated = errors.New("lzma: truncated stream")

	// ErrCorrupt reports malformed compressed data, including streams that
	// end with an end-of-stream marker before the expected size is reached
	// (including a short output size).
	ErrCorrupt = errors.New("lzma: corrupt stream")

	// ErrInvalidOptions reports encoder options out of range.
	ErrInvalidOptions = errors.New("lzma: invalid encoder options")
)

// Options configures the encoder. They follow the usual LZMA encoder
// convention that negative values select the defaults.
type Options struct {
	// Level is the compression level, 0..9 (negative: 5). It selects the
	// default DictSize and FB, and the algorithm: levels 5 and above use
	// the optimal parser with the bt4 binary tree match finder, lower
	// levels the fast parser with the hc5 hash chain match finder.
	Level int

	// DictSize is the dictionary size in bytes. Zero selects the
	// default for Level. Values above MaxDictSize are clamped. The encoder
	// never uses more dictionary memory than the input requires: its match
	// finder needs about 4 (fast mode) or 8 (normal mode) bytes per input
	// byte, plus a hash table of up to 4 bytes per input byte.
	DictSize uint32

	// LC is the number of literal context bits, 0..8 (negative: 3).
	LC int
	// LP is the number of literal position bits, 0..4 (negative: 0).
	LP int
	// PB is the number of position bits, 0..4 (negative: 2).
	PB int

	// FB is the number of fast bytes, 5..273 (negative: 32 below level 7,
	// else 64; out of range values are clamped). It also sets the match
	// finder's search depth to 16+FB/2 (halved in fast mode), computed before
	// clamping.
	FB int

	// NumThreads is accepted for API compatibility and is ignored:
	// encoding is single threaded. The thread count never affects the
	// output.
	NumThreads int
}

// DefaultOptions returns the parameters AaruFormat writers use for every
// LZMA block: level 9, 32 MiB dictionary (the "dictionary" image option
// default), lc=4, lp=0, pb=2, fb=273.
func DefaultOptions() Options {
	return Options{Level: 9, DictSize: 1 << 25, LC: 4, LP: 0, PB: 2, FB: 273, NumThreads: 1}
}

// normalized holds Options after applying the defaulting rules.
type normalized struct {
	level      int
	dictSize   uint32
	lc, lp, pb int
	fb         int    // clamped to [MinFB, MaxFB]
	mc         uint32 // match finder cut value
}

// params returns the encoder parameters.
func (n normalized) params() encParams {
	fast := n.level < 5 // algo 0
	ep := encParams{
		dictSize: n.dictSize,
		lc:       uint32(n.lc),
		lp:       uint32(n.lp),
		pb:       uint32(n.pb),
		fb:       uint32(n.fb),
		fastMode: fast,
		btMode:   !fast,
		mc:       n.mc,
	}
	if fast {
		ep.numHashBytes = 5
	} else {
		ep.numHashBytes = 4
	}
	return ep
}

// normalize applies the defaulting and clamping rules to o.
func (o Options) normalize() (normalized, error) {
	n := normalized{level: o.Level, dictSize: o.DictSize, lc: o.LC, lp: o.LP, pb: o.PB, fb: o.FB}
	if n.level < 0 {
		n.level = 5
	}
	if n.dictSize == 0 {
		switch {
		case n.level <= 3:
			n.dictSize = 1 << (n.level*2 + 16)
		case n.level <= 6:
			n.dictSize = 1 << (n.level + 19)
		case n.level <= 7:
			n.dictSize = 1 << 25
		default:
			n.dictSize = 1 << 26
		}
	}
	if n.lc < 0 {
		n.lc = 3
	}
	if n.lp < 0 {
		n.lp = 0
	}
	if n.pb < 0 {
		n.pb = 2
	}
	if n.fb < 0 {
		if n.level < 7 {
			n.fb = 32
		} else {
			n.fb = 64
		}
	}
	// mc, the match finder's search depth: computed from the unclamped fb
	// (capped to 32 bits) and halved for the hash
	// chain match finder of fast mode.
	n.mc = 16 + uint32(min(n.fb, math.MaxInt32))>>1
	if n.level < 5 {
		n.mc >>= 1
	}
	if n.lc > MaxLC || n.lp > MaxLP || n.pb > MaxPB {
		return n, fmt.Errorf("%w: lc=%d lp=%d pb=%d", ErrInvalidOptions, n.lc, n.lp, n.pb)
	}
	if n.dictSize > MaxDictSize {
		n.dictSize = MaxDictSize
	}
	n.fb = min(max(n.fb, MinFB), MaxFB)
	return n, nil
}

// props builds the properties block the standard way:
// dictionary sizes of 2 MiB and above are rounded up to a multiple of 1 MiB,
// smaller ones up to the next 2^n or 3*2^n.
func (n normalized) props() [PropsLen]byte {
	var p [PropsLen]byte
	p[0] = byte((n.pb*5+n.lp)*9 + n.lc)
	d := n.dictSize
	var v uint32
	if d >= 1<<21 {
		const mask = 1<<20 - 1
		v = (d + mask) &^ mask
		if v < d {
			v = d
		}
	} else {
		for i := uint(22); ; i++ {
			v = (2 + uint32(i&1)) << (i >> 1)
			if v >= d {
				break
			}
		}
	}
	binary.LittleEndian.PutUint32(p[1:], v)
	return p
}

// Encode compresses src into a raw LZMA stream (no header, no end marker)
// and returns it together with the 5-byte properties needed to decode it.
// The stream can be decoded with Decode given props and len(src).
func Encode(src []byte, opts Options) (stream []byte, props [PropsLen]byte, err error) {
	n, err := opts.normalize()
	if err != nil {
		return nil, props, err
	}
	props = n.props()
	stream, err = encode(nil, src, n)
	return stream, props, err
}

// EncodePayload compresses src and returns the on-disk AaruFormat payload:
// the 5-byte properties followed by the raw LZMA stream.
func EncodePayload(src []byte, opts Options) ([]byte, error) {
	n, err := opts.normalize()
	if err != nil {
		return nil, err
	}
	props := n.props()
	return encode(props[:], src, n)
}

// encode returns prefix followed by the raw stream for src.
func encode(prefix, src []byte, n normalized) ([]byte, error) {
	return encodeTo(prefix, src, n.params()), nil
}

func encodeTo(prefix, src []byte, ep encParams) []byte {
	dst := make([]byte, len(prefix), len(prefix)+len(src)/2+64)
	copy(dst, prefix)
	if len(src) == 0 && !ep.writeEndMark {
		// An empty stream is just the range coder flush.
		return append(dst, 0, 0, 0, 0, 0)
	}
	return encodeStream(dst, src, ep)
}

// Decode decompresses a raw LZMA stream described by props into exactly
// size bytes. It stops as soon as size bytes have been produced and ignores
// whatever follows: the end of the stream (as EncodePayload writes it), an
// end-of-stream marker, more encoded data (some existing images contain
// blocks whose stream encodes more than the block length), or unrelated
// trailing bytes. The input must however contain the symbol that completes
// the output entirely, or Decode reports ErrTruncated. An end-of-stream
// marker before size bytes is ErrCorrupt.
func Decode(src, props []byte, size int) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("lzma: negative size %d", size)
	}
	dst := make([]byte, size)
	if err := DecodeInto(dst, src, props); err != nil {
		return nil, err
	}
	return dst, nil
}

// DecodePayload decompresses an on-disk AaruFormat LZMA payload (5-byte
// properties followed by the raw stream) into exactly size bytes.
func DecodePayload(payload []byte, size int) ([]byte, error) {
	if len(payload) < PropsLen {
		return nil, fmt.Errorf("%w: payload of %d bytes has no properties", ErrInvalidProperties, len(payload))
	}
	return Decode(payload[PropsLen:], payload[:PropsLen], size)
}

// DecodeInto decompresses a raw LZMA stream described by props, filling all
// of dst. It fails if the stream does not decode to at least len(dst)
// bytes.
//
// The output buffer serves as the dictionary, so apart
// from the literal probability tables (at most 6 MiB, for lc+lp = 12) no
// memory is allocated, whatever dictionary size props announce.
func DecodeInto(dst, src, props []byte) error {
	if len(props) < PropsLen || props[0] >= 9*5*5 {
		return ErrInvalidProperties
	}
	// The range decoder needs its 5 initialisation bytes even for an empty
	// output.
	if len(src) < 5 {
		return ErrTruncated
	}
	if src[0] != 0 {
		return fmt.Errorf("%w: first byte of range coder is not zero", ErrCorrupt)
	}
	if len(dst) == 0 {
		return nil
	}

	d := uint32(props[0])
	lc, lp, pb := d%9, (d/9)%5, d/45
	dict := max(binary.LittleEndian.Uint32(props[1:PropsLen]), MinDictSize)

	probs := new(decProbs)
	probs.init()
	switch decodeRaw(dst, src, lc, lp, pb, dict, probs, newLitProbs(lc+lp)) {
	case statusOK:
		return nil
	case statusTruncated:
		return ErrTruncated
	default:
		return fmt.Errorf("%w: invalid data before %d bytes were decoded", ErrCorrupt, len(dst))
	}
}
