// Package iso9660 is a minimal ISO 9660 reader able to locate and read a file
// by path through a sector-read callback. It only understands plain ISO 9660
// (no Joliet, Rock Ridge or UDF) and is used to extract PARAM.SFO from PS3
// discs.
package iso9660

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// SectorSize is the ISO 9660 logical sector size.
const SectorSize = 2048

const (
	pvdSector        = 16
	pvdTypePrimary   = 1
	pvdRootDirOffset = 156
	dirFlagDirectory = 0x02
	minRecordLen     = 33
)

// Errors returned by ReadFile.
var (
	// ErrNotISO9660 means sector 16 is not a primary volume descriptor.
	ErrNotISO9660 = errors.New("iso9660: not an ISO 9660 volume")
	// ErrNotFound means a path component does not exist (or a non-final
	// component is not a directory).
	ErrNotFound = errors.New("iso9660: file not found")
	// ErrNilReader means no sector read function was supplied.
	ErrNilReader = errors.New("iso9660: nil sector reader")
)

// ReadSectorFunc reads the 2048-byte sector lba into buf (len(buf) ==
// SectorSize).
type ReadSectorFunc func(lba uint64, buf []byte) error

type fs struct {
	read ReadSectorFunc
	buf  [SectorSize]byte
}

func (f *fs) sector(lba uint64) ([]byte, error) {
	if err := f.read(lba, f.buf[:]); err != nil {
		return nil, fmt.Errorf("iso9660: reading sector %d: %w", lba, err)
	}

	return f.buf[:], nil
}

// ReadFile reads the file at path (components separated by '/', leading '/'
// optional, matched case-insensitively ignoring ";1" version suffixes and a
// trailing '.') from the volume accessed through readSector.
func ReadFile(readSector ReadSectorFunc, path string) ([]byte, error) {
	if readSector == nil {
		return nil, ErrNilReader
	}

	f := &fs{read: readSector}

	pvd, err := f.sector(pvdSector)
	if err != nil {
		return nil, err
	}

	if pvd[0] != pvdTypePrimary || !bytes.Equal(pvd[1:6], []byte("CD001")) {
		return nil, ErrNotISO9660
	}

	root := pvd[pvdRootDirOffset:]
	curLBA := binary.LittleEndian.Uint32(root[2:])
	curSize := binary.LittleEndian.Uint32(root[10:])

	components := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })

	for i, name := range components {
		lba, size, isDir, err := f.findEntry(curLBA, curSize, name)
		if err != nil {
			return nil, err
		}

		if i < len(components)-1 && !isDir {
			return nil, fmt.Errorf("%w: %q is not a directory", ErrNotFound, name)
		}

		curLBA, curSize = lba, size
	}

	// Grow as sectors are read rather than trusting the recorded size up front.
	out := make([]byte, 0, min(curSize, 16<<20))
	lba := uint64(curLBA)

	for uint32(len(out)) < curSize {
		sec, err := f.sector(lba)
		if err != nil {
			return nil, err
		}

		n := min(curSize-uint32(len(out)), SectorSize)
		out = append(out, sec[:n]...)
		lba++
	}

	return out, nil
}

// findEntry looks up name in the directory extent at dirLBA of dirSize bytes.
func (f *fs) findEntry(dirLBA, dirSize uint32, name string) (lba, size uint32, isDir bool, err error) {
	remaining := dirSize
	cur := uint64(dirLBA)

	for remaining > 0 {
		sec, err := f.sector(cur)
		if err != nil {
			return 0, 0, false, err
		}

		secBytes := min(remaining, SectorSize)

		for pos := uint32(0); pos < secBytes; {
			recLen := uint32(sec[pos])

			// Zero length: padding up to the next sector. Malformed: give up
			// on this sector.
			if recLen == 0 || recLen < minRecordLen || pos+recLen > secBytes {
				break
			}

			rec := sec[pos : pos+recLen]
			pos += recLen

			idLen := uint32(rec[32])
			if minRecordLen+idLen > recLen {
				continue // identifier overruns its record
			}

			id := rec[33 : 33+idLen]

			// Skip "." and "..".
			if idLen == 1 && (id[0] == 0 || id[0] == 1) {
				continue
			}

			// Strip ";1"-style version suffix, then a trailing '.'.
			if len(id) >= 2 && id[len(id)-2] == ';' {
				id = id[:len(id)-2]
			}

			if len(id) > 0 && id[len(id)-1] == '.' {
				id = id[:len(id)-1]
			}

			if asciiEqualFold(id, name) {
				return binary.LittleEndian.Uint32(rec[2:]), binary.LittleEndian.Uint32(rec[10:]),
					rec[25]&dirFlagDirectory != 0, nil
			}
		}

		cur++
		remaining -= secBytes
	}

	return 0, 0, false, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// asciiEqualFold compares a and b case-insensitively for ASCII letters only.
func asciiEqualFold(a []byte, b string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		x, y := a[i], b[i]

		if 'a' <= x && x <= 'z' {
			x -= 'a' - 'A'
		}

		if 'a' <= y && y <= 'z' {
			y -= 'a' - 'A'
		}

		if x != y {
			return false
		}
	}

	return true
}
