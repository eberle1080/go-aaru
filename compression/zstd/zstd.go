// Package zstd implements the Zstandard codec used by AaruFormat images
// (compression types Zstd and ZstdCst).
//
// AaruFormat stores Zstandard payloads as standard, self-contained zstd
// frames without any prefix. This package is built on
// github.com/klauspost/compress/zstd. It writes standard zstd frames
// (RFC 8878) and decodes standard frames, including concatenated ones.
//
// All functions are safe for concurrent use. Encoders and the decoder are
// created lazily and shared.
package zstd

import (
	"errors"
	"fmt"
	"sync"

	kzstd "github.com/klauspost/compress/zstd"
)

// Compression levels, on the standard zstd level scale (1..22).
const (
	// MinLevel and MaxLevel bound the levels accepted by the AaruFormat
	// "zstd_level" option.
	MinLevel = 1
	MaxLevel = 22

	// DefaultLevel is the AaruFormat default "zstd_level".
	DefaultLevel = 19
)

// Errors returned by the codec.
var (
	// ErrCorrupt reports malformed compressed data.
	ErrCorrupt = errors.New("zstd: corrupt data")

	// ErrSizeMismatch reports compressed data that does not decode to the
	// expected number of bytes.
	ErrSizeMismatch = errors.New("zstd: decompressed size mismatch")
)

var (
	decoderOnce sync.Once
	decoder     *kzstd.Decoder
	decoderErr  error

	encoderMu sync.Mutex
	encoders  [4]*kzstd.Encoder // indexed by EncoderLevel-1
)

func getDecoder() (*kzstd.Decoder, error) {
	decoderOnce.Do(func() {
		// DecodeAll is safe for concurrent use; the concurrency option
		// bounds how many decodes may run at once. The cap limit makes
		// DecodeAll refuse to produce more than cap(dst) bytes, which
		// bounds memory for hostile frames.
		decoder, decoderErr = kzstd.NewReader(nil,
			kzstd.WithDecoderConcurrency(0),
			kzstd.WithDecodeAllCapLimit(true),
		)
	})
	return decoder, decoderErr
}

// encoderLevel maps a standard zstd level onto the closest klauspost
// EncoderLevel by compression ratio:
//
//	level <= 2  -> SpeedFastest           (≈ zstd 1)
//	level 3..5  -> SpeedDefault           (≈ zstd 3)
//	level 6..9  -> SpeedBetterCompression (≈ zstd 7)
//	level >= 10 -> SpeedBestCompression   (≈ zstd 11; also used for 19..22)
//
// Levels <= 0 (zstd's "default" and negative fast levels) map to
// SpeedFastest; there is no stronger encoder than SpeedBestCompression, so
// levels 10..22 all compress alike.
func encoderLevel(level int) kzstd.EncoderLevel {
	return kzstd.EncoderLevelFromZstd(level)
}

func getEncoder(level int) (*kzstd.Encoder, error) {
	l := encoderLevel(level)
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if e := encoders[l-1]; e != nil {
		return e, nil
	}
	e, err := kzstd.NewWriter(nil,
		kzstd.WithEncoderLevel(l),
		// No content checksum is written: AaruFormat blocks carry their
		// own CRC64.
		kzstd.WithEncoderCRC(false),
		// Emit a real (empty) frame for empty input.
		kzstd.WithZeroFrames(true),
	)
	if err != nil {
		return nil, fmt.Errorf("zstd: creating encoder: %w", err)
	}
	encoders[l-1] = e
	return e, nil
}

// Encode compresses src into a single zstd frame at the given zstd level
// (see MinLevel, MaxLevel and DefaultLevel). Levels outside 1..22 are
// accepted and mapped like the nearest level in range: lower levels
// compress faster, higher ones better. The frame records the
// uncompressed size.
func Encode(src []byte, level int) ([]byte, error) {
	return AppendEncode(nil, src, level)
}

// AppendEncode is like Encode but appends the frame to dst and returns the
// extended slice.
func AppendEncode(dst, src []byte, level int) ([]byte, error) {
	e, err := getEncoder(level)
	if err != nil {
		return nil, err
	}
	return e.EncodeAll(src, dst), nil
}

// Decode decompresses src, which must decode to exactly size bytes.
// Concatenated frames are accepted.
func Decode(src []byte, size int) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("zstd: negative size %d", size)
	}
	dst := make([]byte, size)
	if err := DecodeInto(dst, src); err != nil {
		return nil, err
	}
	return dst, nil
}

// DecodeInto decompresses src into dst, which must be exactly the
// uncompressed size. Data that would decode to more or fewer bytes than
// len(dst) is rejected with ErrSizeMismatch.
func DecodeInto(dst, src []byte) error {
	d, err := getDecoder()
	if err != nil {
		return fmt.Errorf("zstd: creating decoder: %w", err)
	}
	out, err := d.DecodeAll(src, dst[:0:len(dst)])
	if err != nil {
		if errors.Is(err, kzstd.ErrDecoderSizeExceeded) {
			return fmt.Errorf("%w: more than %d bytes", ErrSizeMismatch, len(dst))
		}
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(out) != len(dst) {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, len(out), len(dst))
	}
	if len(out) > 0 && &out[0] != &dst[0] {
		copy(dst, out)
	}
	return nil
}
