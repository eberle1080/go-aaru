// Package ird parses PlayStation 3 IRD ("ISO Rebuild Data") files, versions
// 6 to 9, which carry the data1/data2 disc keys and PIC needed to decrypt and
// preserve PS3 discs. Both raw and gzip-compressed IRD files are accepted.
package ird

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// PICSize is the size of the PIC (disc lead-in information) block.
const PICSize = 115

// Errors returned by Parse.
var (
	// ErrInvalidMagic means the data does not start with "3IRD".
	ErrInvalidMagic = errors.New("ird: invalid magic")
	// ErrUnsupportedVersion means the IRD version is outside 6..9.
	ErrUnsupportedVersion = errors.New("ird: unsupported version")
	// ErrTruncated means the file ends before its mandatory fields.
	ErrTruncated = errors.New("ird: truncated file")
)

// File is a parsed IRD file. Fields that are absent from a truncated file are
// left at their zero value.
type File struct {
	Version       uint8         // IRD format version (6-9).
	GameID        string        // Game ID, e.g. "BLES00905".
	GameName      string        // Game name.
	UpdateVersion string        // PS3 system/update version (4 chars).
	GameVersion   string        // Game version (5 chars).
	AppVersion    string        // App version (5 chars).
	HeaderGz      []byte        // Gzip-compressed ISO header blob (nil if absent).
	FooterGz      []byte        // Gzip-compressed ISO footer blob (nil if absent).
	Data1         [16]byte      // data1 key, from which the disc key is derived.
	Data2         [16]byte      // data2 key.
	PIC           [PICSize]byte // PIC data, valid only if HasPIC.
	HasPIC        bool          // Whether PIC data was present.
}

// ParseFile reads and parses the IRD file at path.
func ParseFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ParseBytes(data)
}

// Parse reads an IRD file from r and parses it.
func Parse(r io.Reader) (*File, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return ParseBytes(data)
}

// ParseBytes parses an IRD file held in memory. Gzip-compressed input (magic
// 1F 8B) is decompressed first.
func ParseBytes(data []byte) (*File, error) {
	if len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("ird: %w", err)
		}

		zr.Multistream(false)

		data, err = io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("ird: %w", err)
		}
	}

	return parse(data)
}

// cstr returns b up to its first NUL byte.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}

	return string(b)
}

// read7BitInt decodes a .NET BinaryReader 7-bit encoded int32.
func read7BitInt(data []byte, pos int) (value, next int, ok bool) {
	shift := 0

	for i := 0; i < 5; i++ {
		if pos >= len(data) {
			return 0, pos, false
		}

		b := data[pos]
		pos++

		value |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			return value, pos, true
		}

		shift += 7
	}

	return 0, pos, false
}

func parse(data []byte) (*File, error) {
	if len(data) < 20 || !bytes.Equal(data[:4], []byte("3IRD")) {
		return nil, ErrInvalidMagic
	}

	f := &File{Version: data[4]}
	if f.Version < 6 || f.Version > 9 {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, f.Version)
	}

	pos := 5
	n := len(data)

	f.GameID = cstr(data[pos : pos+9]) // len(data) >= 20
	pos += 9

	// Game name: .NET length-prefixed string.
	nameLen, pos, ok := read7BitInt(data, pos)
	if !ok || nameLen > n-pos {
		return nil, ErrTruncated
	}

	f.GameName = cstr(data[pos : pos+nameLen])
	pos += nameLen

	// Fixed-width version strings.
	if pos+4 <= n {
		f.UpdateVersion = cstr(data[pos : pos+4])
		pos += 4
	}

	if pos+5 <= n {
		f.GameVersion = cstr(data[pos : pos+5])
		pos += 5
	}

	if pos+5 <= n {
		f.AppVersion = cstr(data[pos : pos+5])
		pos += 5
	}

	// v7: extra 4-byte ID field.
	if f.Version == 7 {
		pos += 4
	}

	// Header and footer: uint32 LE length + gzip data.
	blob := func() ([]byte, bool) {
		if pos+4 > n {
			return nil, false
		}

		l := int64(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4

		if int64(pos)+l > int64(n) {
			return nil, true
		}

		b := bytes.Clone(data[pos : pos+int(l)])
		pos += int(l)

		return b, true
	}

	var more bool

	if f.HeaderGz, more = blob(); !more {
		return f, nil
	}

	if f.FooterGz, more = blob(); !more {
		return f, nil
	}

	// Region hashes (skipped).
	if pos < n {
		rc := int(data[pos])
		pos += 1 + rc*16
	}

	// File entries (skipped): key(8) + md5(16) each.
	if pos+4 <= n {
		fc := int64(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4

		if int64(pos)+fc*24 > int64(n) {
			pos = n + 1
		} else {
			pos += int(fc * 24)
		}
	}

	// Extra config + attachments (4 bytes).
	pos += 4

	// v9: PIC then d1, d2. v<9: d1, d2 then PIC.
	readPIC := func() {
		if pos+PICSize <= n {
			copy(f.PIC[:], data[pos:])
			f.HasPIC = true
			pos += PICSize
		}
	}

	if f.Version >= 9 {
		readPIC()
	}

	if pos+32 <= n {
		copy(f.Data1[:], data[pos:])
		copy(f.Data2[:], data[pos+16:])
		pos += 32
	}

	if f.Version < 9 {
		readPIC()
	}

	return f, nil
}
