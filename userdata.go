package aaru

import (
	"fmt"
	"io"

	"github.com/eberle1080/go-aaru/media"
)

// UserDataSectorSize is the sector size served by UserDataReader: the 2048
// bytes of user data of a CD Mode 1 / Mode 2 Form 1, DVD or Blu-ray sector,
// the logical sector of ISO 9660 and UDF.
const UserDataSectorSize = 2048

// UserDataReader is an io.ReaderAt over the user data of an image's sectors,
// laid end to end from LBA 0: byte off is byte off%2048 of sector
// off/2048. It lets file system readers such as fs/iso9660 read a disc
// image. Only the user sectors are covered (negative and overflow sectors
// are not).
//
// Reading a sector that was never dumped fails with an error wrapping
// ErrSectorNotDumped (and naming the sector); a sector whose user data is
// not 2048 bytes long (CD audio, Mode 2 Form 2) fails with
// ErrInvalidSectorLength. Sectors with any other status, including
// SectorStatusErrored, are returned as stored; set OnSector to observe the
// statuses.
//
// Like the Image it reads, a UserDataReader is not safe for concurrent use.
type UserDataReader struct {
	img  *Image
	size int64

	// OnSector, when set, is called with the address and status of every
	// sector read from the image (cached re-reads of the same sector are
	// not reported again).
	OnSector func(lba uint64, status SectorStatus)

	lastLBA  uint64
	last     []byte
	haveLast bool
}

// UserDataReaderAt returns a UserDataReader over img. It fails for an image
// without user data and for an image whose sectors cannot hold 2048 bytes of
// user data: block media must have 2048-byte sectors, while optical images
// (whose SectorSize is that of their largest sector, e.g. 2352 with audio
// tracks) are accepted and checked sector by sector.
func (img *Image) UserDataReaderAt() (*UserDataReader, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if img.noUserDataDDT {
		return nil, ErrUserDataNotPresent
	}
	ss := img.info.SectorSize
	optical := img.info.MetadataMediaType == media.OpticalDisc
	if ss != UserDataSectorSize && !(optical && ss > UserDataSectorSize) {
		return nil, fmt.Errorf("user data reader needs %d-byte sectors, image has %d: %w",
			UserDataSectorSize, ss, ErrInvalidSectorLength)
	}
	return &UserDataReader{img: img, size: int64(img.info.Sectors) * UserDataSectorSize}, nil
}

// Size returns the number of bytes covered: user sectors × 2048.
func (r *UserDataReader) Size() int64 { return r.size }

// sector returns the user data of sector lba.
func (r *UserDataReader) sector(lba uint64) ([]byte, error) {
	if r.haveLast && r.lastLBA == lba {
		return r.last, nil
	}
	data, status, err := r.img.ReadSector(lba, false)
	if r.OnSector != nil {
		r.OnSector(lba, status)
	}
	if err != nil {
		return nil, fmt.Errorf("sector %d: %w", lba, err)
	}
	if len(data) != UserDataSectorSize {
		return nil, fmt.Errorf("sector %d holds %d bytes of user data, not %d: %w",
			lba, len(data), UserDataSectorSize, ErrInvalidSectorLength)
	}
	r.last, r.lastLBA, r.haveLast = data, lba, true
	return data, nil
}

// ReadAt implements io.ReaderAt. It returns io.EOF when the read reaches
// the end of the user sectors.
func (r *UserDataReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("user data reader: negative offset %d", off)
	}
	n := 0
	for len(p) > 0 {
		if off >= r.size {
			return n, io.EOF
		}
		lba := uint64(off / UserDataSectorSize)
		in := int(off % UserDataSectorSize)
		data, err := r.sector(lba)
		if err != nil {
			return n, err
		}
		k := copy(p, data[in:])
		n += k
		off += int64(k)
		p = p[k:]
	}
	return n, nil
}
