// Package flac implements the FLAC codec used by AaruFormat images to
// compress Red Book (CD-DA) audio: 2 channels, 16 bits per sample,
// 44100 Hz, stored as interleaved little-endian PCM.
//
// The codec is pure Go. Streams are complete native FLAC streams: the
// "fLaC" marker, a STREAMINFO block, an optional APPLICATION block and the
// audio frames. The encoder performs the standard FLAC analysis
// (wasted bits, constant/verbatim/fixed/LPC subframes, windowed
// autocorrelation with Levinson-Durbin, coefficient quantization and
// precision search, stereo decorrelation and partitioned Rice coding). The
// decoder reads native FLAC streams of 16-bit stereo audio.
package flac

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors returned by the codec. Errors describing a specific problem wrap
// one of these sentinels; test with errors.Is.
var (
	// ErrNotFLAC means the input does not start with the "fLaC" marker.
	ErrNotFLAC = errors.New("flac: not a FLAC stream")
	// ErrCorrupt means the stream is malformed, truncated or fails a CRC.
	ErrCorrupt = errors.New("flac: corrupt stream")
	// ErrUnsupported means the stream is valid FLAC but not 16-bit stereo.
	ErrUnsupported = errors.New("flac: unsupported stream format")
	// ErrInvalidOptions means the EncoderOptions are out of range.
	ErrInvalidOptions = errors.New("flac: invalid encoder options")
	// ErrInvalidLength means the PCM input is not a whole number of
	// 4-byte stereo sample frames.
	ErrInvalidLength = errors.New("flac: PCM length is not a multiple of 4")
)

// Red Book block sizing used by AaruFormat writers.
const (
	// SamplesPerSector is the number of stereo samples in one CD-DA sector.
	SamplesPerSector = 588
	// MinFLACBlock and MaxFLACBlock bound the FLAC block size AaruFormat
	// writers use.
	MinFLACBlock = 256
	MaxFLACBlock = 4608
)

// BlockSizeForSectors returns the FLAC block size AaruFormat writers use for a
// data block holding the given number of audio sectors:
// sectors*SamplesPerSector clamped to [MinFLACBlock, MaxFLACBlock].
func BlockSizeForSectors(sectors int) int {
	return min(max(sectors*SamplesPerSector, MinFLACBlock), MaxFLACBlock)
}

// EncoderOptions holds the FLAC encoder parameters.
type EncoderOptions struct {
	// BlockSize is the number of samples per channel in each frame,
	// 16..65535. Zero selects the standard default (4096, or 1152 when
	// MaxLPCOrder is 0).
	BlockSize int
	// MidSide enables stereo decorrelation: every frame is encoded as the
	// smallest of left/right, left/side, right/side and mid/side.
	MidSide bool
	// LooseMidSide (only with MidSide) re-evaluates the stereo mode only
	// about every 0.4 s of audio, reusing the last decision otherwise.
	LooseMidSide bool
	// Apodization is a FLAC apodization window specification, e.g. "hamming",
	// "tukey(0.5)", "partial_tukey(2)" or "hann;punchout_tukey(3)". All
	// standard FLAC windows are supported: bartlett, bartlett_hann, blackman,
	// blackman_harris_4term_92db, connes, flattop, gauss(STDDEV), hamming,
	// hann, kaiser_bessel, nuttall, rectangle, triangle, tukey(P),
	// partial_tukey(n[/ov[/P]]), punchout_tukey(n[/ov[/P]]) and welch.
	// Unknown entries are ignored; an empty result means tukey(0.5).
	Apodization string
	// MaxLPCOrder is the maximum LPC predictor order, 0..32 (0 disables
	// LPC subframes).
	MaxLPCOrder int
	// QLPCoeffPrecision is the quantized LPC coefficient precision in
	// bits, 5..15, or 0 to choose it from the block size.
	// Ignored when QLPCoeffPrecSearch is set.
	QLPCoeffPrecision int
	// QLPCoeffPrecSearch tries every coefficient precision and keeps the
	// smallest result.
	QLPCoeffPrecSearch bool
	// ExhaustiveModelSearch tries every fixed and LPC order instead of
	// estimating the best one.
	ExhaustiveModelSearch bool
	// MinResidualPartitionOrder and MaxResidualPartitionOrder bound the
	// Rice partition order search (max 15).
	MinResidualPartitionOrder int
	MaxResidualPartitionOrder int
	// ApplicationID, when non-empty, is stored in an APPLICATION metadata
	// block. As in existing AaruFormat images, the bytes become the
	// block's data and the 4-byte registered application ID is zero.
	ApplicationID []byte
	// Workers limits how many goroutines encode frames concurrently; 0
	// means runtime.GOMAXPROCS(0). The output is identical for any value.
	Workers int
}

// DefaultEncoderOptions returns the settings AaruFormat writers use: block
// size 4608 (use BlockSizeForSectors for short blocks),
// mid/side stereo, "hamming" apodization, LPC order 12, precision 15 with
// precision search, partition orders 0..8 and application data "Aaru".
func DefaultEncoderOptions() EncoderOptions {
	return EncoderOptions{
		BlockSize:                 MaxFLACBlock,
		MidSide:                   true,
		LooseMidSide:              false,
		Apodization:               "hamming",
		MaxLPCOrder:               12,
		QLPCoeffPrecision:         15,
		QLPCoeffPrecSearch:        true,
		ExhaustiveModelSearch:     false,
		MinResidualPartitionOrder: 0,
		MaxResidualPartitionOrder: 8,
		ApplicationID:             []byte("Aaru"),
	}
}

// EncodeRedbook compresses interleaved 16-bit little-endian stereo PCM into
// a complete FLAC stream. The STREAMINFO block records the block size, the
// minimum and maximum frame sizes, the total sample count and the MD5 of
// pcm. An empty input yields a valid stream without audio frames.
func EncodeRedbook(pcm []byte, opts EncoderOptions) ([]byte, error) {
	if len(pcm)%redbookBytesPerSamp != 0 {
		return nil, ErrInvalidLength
	}
	if uint64(len(pcm)/redbookBytesPerSamp) >= 1<<36 {
		return nil, fmt.Errorf("%w: too many samples for STREAMINFO", ErrInvalidLength)
	}
	n, err := opts.normalize()
	if err != nil {
		return nil, err
	}
	return encodeStream(pcm, n), nil
}

// DecodeRedbookInto decodes the FLAC stream src into dst as interleaved
// 16-bit little-endian stereo PCM and returns the number of bytes written.
//
// Decoding stops filling dst once it is full: audio
// beyond len(dst) is discarded (the rest of the stream is still decoded
// and verified). Callers that know the uncompressed size should pass a dst
// of exactly that size and check that n == len(dst). On error n is 0.
//
// The STREAMINFO MD5 signature is not verified; frame header CRC-8 and frame
// CRC-16 are always verified.
func DecodeRedbookInto(dst, src []byte) (int, error) {
	n := 0
	err := decodeStream(src, func(l, r []int32) {
		n += interleave(dst[n:], l, r)
	}, nil, nil)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DecodeRedbook decodes the whole FLAC stream src and returns its audio as
// interleaved 16-bit little-endian stereo PCM. The output size is bounded
// only by the stream (constant frames compress about 20000:1); prefer
// DecodeRedbookInto for untrusted input whose decoded size is known.
func DecodeRedbook(src []byte) ([]byte, error) {
	var out []byte
	err := decodeStream(src, func(l, r []int32) {
		for i := range l {
			out = binary.LittleEndian.AppendUint16(out, uint16(l[i]))
			out = binary.LittleEndian.AppendUint16(out, uint16(r[i]))
		}
	}, func(info *streamInfo) {
		// Pre-size from STREAMINFO, bounded so a hostile header cannot
		// force a huge allocation.
		if total := info.totalSamples * redbookBytesPerSamp; total <= 64<<20 {
			out = make([]byte, 0, total)
		}
	}, nil)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// decodeStream parses src and calls emit with the left and right channel
// samples of every frame, in order.
func decodeStream(src []byte, emit func(l, r []int32), start func(*streamInfo), stats *decodeStats) error {
	info, pos, err := parseStreamHeader(src)
	if err != nil {
		return err
	}
	if start != nil {
		start(&info)
	}
	r := newBitReader(src)
	r.pos = pos
	d := frameDecoder{redbook: true, info: &info, r: r, src: src, stats: stats}
	var decoded uint64
	for r.remainingBits() > 0 {
		// Stop once STREAMINFO's sample count has been
		// reached, ignoring anything that follows.
		if info.totalSamples > 0 && decoded >= info.totalSamples {
			break
		}
		h, err := d.decodeFrame()
		if err != nil {
			return err
		}
		emit(d.samples[0][:h.blockSize], d.samples[1][:h.blockSize])
		decoded += uint64(h.blockSize)
	}
	return nil
}

// interleave writes l/r as 16-bit little-endian stereo pairs into dst, up
// to len(dst) bytes (a trailing partial pair is written partially), and
// returns the number of bytes written.
func interleave(dst []byte, l, r []int32) int {
	n := 0
	for i := range l {
		if len(dst)-n < redbookBytesPerSamp {
			var tmp [redbookBytesPerSamp]byte
			binary.LittleEndian.PutUint16(tmp[0:], uint16(l[i]))
			binary.LittleEndian.PutUint16(tmp[2:], uint16(r[i]))
			return n + copy(dst[n:], tmp[:])
		}
		binary.LittleEndian.PutUint16(dst[n:], uint16(l[i]))
		binary.LittleEndian.PutUint16(dst[n+2:], uint16(r[i]))
		n += redbookBytesPerSamp
	}
	return n
}
