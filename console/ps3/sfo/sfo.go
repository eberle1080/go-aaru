// Package sfo parses PlayStation PARAM.SFO files (key/value metadata such as
// TITLE and TITLE_ID).
package sfo

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Magic is the SFO magic ("\x00PSF") read as a little-endian uint32.
const Magic = 0x46535000

// Entry data formats.
const (
	FormatUTF8        = 0x0004 // UTF-8 string, not NUL-terminated.
	FormatUTF8Special = 0x0204 // UTF-8 string, NUL-terminated.
	FormatInt32       = 0x0404 // 32-bit little-endian integer.
)

const headerSize = 20

// Errors returned by Parse.
var (
	// ErrTruncated means the buffer is smaller than its header or index table.
	ErrTruncated = errors.New("sfo: buffer too small or invalid header")
	// ErrInvalidMagic means the data does not start with the SFO magic.
	ErrInvalidMagic = errors.New("sfo: invalid magic")
)

// Entry is one parameter of a PARAM.SFO file.
type Entry struct {
	Key      string // Parameter name ("" if the key offset is out of range).
	Format   uint16 // Data format (FormatUTF8, FormatUTF8Special, FormatInt32, ...).
	Value    string // String value (string formats only), up to the first NUL.
	IntValue int32  // Integer value (FormatInt32 only).
	HasValue bool   // Whether Value / IntValue was decoded.
}

// File is a parsed PARAM.SFO.
type File struct {
	Entries []Entry
}

// Parse parses a PARAM.SFO held in memory.
func Parse(data []byte) (*File, error) {
	if len(data) < headerSize {
		return nil, ErrTruncated
	}

	if binary.LittleEndian.Uint32(data) != Magic {
		return nil, ErrInvalidMagic
	}

	n := uint64(len(data))
	keyTable := uint64(binary.LittleEndian.Uint32(data[8:]))
	dataTable := uint64(binary.LittleEndian.Uint32(data[12:]))
	count := uint64(binary.LittleEndian.Uint32(data[16:]))

	f := &File{}
	if count == 0 {
		return f, nil
	}

	if headerSize+count*16 > n || keyTable > n || dataTable > n {
		return nil, ErrTruncated
	}

	f.Entries = make([]Entry, count)

	for i := range f.Entries {
		idx := data[headerSize+i*16:]
		e := &f.Entries[i]

		keyOff := uint64(binary.LittleEndian.Uint16(idx))
		e.Format = binary.LittleEndian.Uint16(idx[2:])
		dataLen := uint64(binary.LittleEndian.Uint32(idx[4:]))
		dataOff := uint64(binary.LittleEndian.Uint32(idx[12:]))

		if abs := keyTable + keyOff; abs < n {
			k := data[abs:]
			if j := bytes.IndexByte(k, 0); j >= 0 {
				k = k[:j]
			}

			e.Key = string(k)
		}

		abs := dataTable + dataOff
		if abs >= n || dataLen == 0 {
			continue
		}

		dataLen = min(dataLen, n-abs)
		v := data[abs : abs+dataLen]

		switch {
		case e.Format == FormatUTF8 || e.Format == FormatUTF8Special:
			if j := bytes.IndexByte(v, 0); j >= 0 {
				v = v[:j]
			}

			e.Value = string(v)
			e.HasValue = true
		case e.Format == FormatInt32 && dataLen >= 4:
			e.IntValue = int32(binary.LittleEndian.Uint32(v))
			e.HasValue = true
		}
	}

	return f, nil
}

// Lookup returns the first entry whose key is key.
func (f *File) Lookup(key string) (Entry, bool) {
	if f == nil || key == "" {
		return Entry{}, false
	}

	for _, e := range f.Entries {
		if e.Key == key {
			return e, true
		}
	}

	return Entry{}, false
}

// Get returns the string value of key. ok is false if the key is absent or
// the first entry with that key has no string value.
func (f *File) Get(key string) (value string, ok bool) {
	e, found := f.Lookup(key)
	if !found || !e.HasValue || (e.Format != FormatUTF8 && e.Format != FormatUTF8Special) {
		return "", false
	}

	return e.Value, true
}

// GetInt returns the integer value of key. ok is false if the key is absent or
// the first entry with that key has no integer value.
func (f *File) GetInt(key string) (value int32, ok bool) {
	e, found := f.Lookup(key)
	if !found || !e.HasValue || e.Format != FormatInt32 {
		return 0, false
	}

	return e.IntValue, true
}
