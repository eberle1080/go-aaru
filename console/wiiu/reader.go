package wiiu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	// WUXMagic is the WUX file magic ("WUX0" read as a little-endian uint32).
	WUXMagic = 0x30585557

	wuxHeaderSize = 0x20
)

// ErrInvalidWUX is returned for files that carry the WUX magic but have an
// unusable header or index table.
var ErrInvalidWUX = errors.New("wiiu: invalid WUX image")

// Reader provides random access to the uncompressed contents of a Wii U disc
// image stored either as a raw WUD (a flat dump) or as a WUX (sector-level
// deduplicated) file. It implements io.ReaderAt.
//
// WUX layout (little-endian): a 32-byte header {magic "WUX0", reserved,
// sector size (0x8000), reserved, uncompressed size (8 bytes), reserved (8
// bytes)}, followed by a uint32 index per 0x8000-byte disc sector mapping it to
// a stored sector, followed (at the next 0x8000-aligned offset) by the stored
// sectors.
type Reader struct {
	ra     io.ReaderAt
	closer io.Closer
	size   int64

	wux        bool
	index      []uint32
	dataOffset int64
}

// OpenReader opens a WUD or WUX file, detecting the format from its magic.
func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	st, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, err
	}

	r, err := NewReader(f, st.Size())
	if err != nil {
		_ = f.Close()

		return nil, err
	}

	r.closer = f

	return r, nil
}

// NewReader returns a Reader over ra, which holds size bytes of a WUD or WUX
// image. Data starting with the WUX magic is parsed as WUX; anything else is
// treated as a raw WUD.
func NewReader(ra io.ReaderAt, size int64) (*Reader, error) {
	var hdr [wuxHeaderSize]byte

	n, err := ra.ReadAt(hdr[:], 0)
	if n == len(hdr) && binary.LittleEndian.Uint32(hdr[:]) == WUXMagic {
		return newWUXReader(ra, size, hdr[:])
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if size <= 0 {
		return nil, fmt.Errorf("wiiu: empty disc image")
	}

	return &Reader{ra: ra, size: size}, nil
}

func newWUXReader(ra io.ReaderAt, fileSize int64, hdr []byte) (*Reader, error) {
	sectorSize := binary.LittleEndian.Uint32(hdr[8:])
	if sectorSize != PhysicalSectorSize {
		return nil, fmt.Errorf("%w: sector size %#x is not %#x", ErrInvalidWUX, sectorSize, PhysicalSectorSize)
	}

	discSize := binary.LittleEndian.Uint64(hdr[16:])
	if discSize > math.MaxInt64 {
		return nil, fmt.Errorf("%w: uncompressed size %d too large", ErrInvalidWUX, discSize)
	}

	count := discSize / PhysicalSectorSize

	indexBytes := count * 4
	if fileSize < wuxHeaderSize || indexBytes > uint64(fileSize-wuxHeaderSize) {
		return nil, fmt.Errorf("%w: index table of %d entries exceeds file size", ErrInvalidWUX, count)
	}

	raw := make([]byte, indexBytes)
	if _, err := ra.ReadAt(raw, wuxHeaderSize); err != nil {
		return nil, fmt.Errorf("wiiu: reading WUX index table: %w", err)
	}

	index := make([]uint32, count)
	for i := range index {
		index[i] = binary.LittleEndian.Uint32(raw[i*4:])
	}

	dataOffset := (int64(wuxHeaderSize) + int64(indexBytes) + PhysicalSectorSize - 1) &^ (PhysicalSectorSize - 1)

	return &Reader{
		ra:         ra,
		size:       int64(discSize),
		wux:        true,
		index:      index,
		dataOffset: dataOffset,
	}, nil
}

// Size returns the uncompressed disc size in bytes.
func (r *Reader) Size() int64 { return r.size }

// IsWUX reports whether the image is a WUX file.
func (r *Reader) IsWUX() bool { return r.wux }

// ReadAt reads len(p) bytes of the uncompressed disc at offset off.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("wiiu: negative offset")
	}

	if !r.wux {
		return r.ra.ReadAt(p, off)
	}

	done := 0

	for done < len(p) {
		cur := off + int64(done)
		if cur >= r.size {
			break
		}

		logical := uint64(cur) / PhysicalSectorSize
		if logical >= uint64(len(r.index)) {
			break
		}

		within := cur % PhysicalSectorSize
		chunk := min(int64(PhysicalSectorSize)-within, int64(len(p)-done), r.size-cur)
		fileOff := r.dataOffset + int64(r.index[logical])*PhysicalSectorSize + within

		n, err := r.ra.ReadAt(p[done:done+int(chunk)], fileOff)
		done += n

		if n < int(chunk) {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}

			return done, fmt.Errorf("wiiu: reading WUX sector %d: %w", logical, err)
		}
	}

	if done < len(p) {
		return done, io.EOF
	}

	return done, nil
}

// Close closes the underlying file if the Reader was created by OpenReader.
func (r *Reader) Close() error {
	if r.closer == nil {
		return nil
	}

	err := r.closer.Close()
	r.closer = nil

	return err
}
