package udf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// pathName returns name made safe for use as a path element.
func pathName(name string) (string, bool) {
	switch name {
	case "":
		return "_", false
	case ".", "..":
		return "_" + name, false
	}
	if strings.ContainsAny(name, "/\x00") {
		return strings.NewReplacer("/", "_", "\x00", "_").Replace(name), false
	}
	return name, true
}

func childPath(parent, name string) string {
	if parent == "/" || parent == "" {
		return "/" + name
	}
	return parent + "/" + name
}

// ReadDir returns the entries of directory d in disc order, without the
// parent entry and without deleted entries. On a damaged directory it
// returns the entries it could read together with an error; an entry whose
// file entry cannot be read is returned too (with what its file identifier
// says), and its error is part of the returned error.
func (v *Volume) ReadDir(d *Entry) ([]*Entry, error) {
	ents, err := v.readDirChecked(d)
	errs := []error{err}
	for _, e := range ents {
		errs = append(errs, e.err)
	}
	return ents, errors.Join(errs...)
}

func (v *Volume) readDirChecked(d *Entry) ([]*Entry, error) {
	if d == nil || d.vol != v || !d.IsDir || d.Stream {
		return nil, ErrNotDirectory
	}
	if d.err != nil {
		return nil, d.err
	}
	return v.readDir(d, "")
}

// maxStreams bounds the named streams read for one entry.
const maxStreams = 1024

// readDir reads the file identifiers of directory d. For a stream
// directory, streamOf is the owner's path.
func (v *Volume) readDir(d *Entry, streamOf string) ([]*Entry, error) {
	if d.data == nil {
		return nil, d.err
	}
	data, rerr := v.dirBytes(d)
	if rerr != nil {
		rerr = fmt.Errorf("udf: directory %s: %w", d.Path, rerr)
	}
	limit := v.opts.MaxEntries
	if streamOf != "" {
		limit = maxStreams
	}
	return v.parseFIDs(d, data, streamOf, limit), rerr
}

// dirBytes reads a directory's data, block by block when a single read
// fails, so that unreadable blocks lose only the identifiers they held.
func (v *Volume) dirBytes(d *Entry) ([]byte, error) {
	c := d.data
	var errs []error
	n := c.size
	lim := v.opts.MaxDirSize
	if v.size != math.MaxInt64 {
		lim = min(lim, v.size)
	}
	if n > lim {
		errs = append(errs, fmt.Errorf("%w: directory of %d bytes exceeds the %d-byte limit; truncated", ErrCorrupt, n, lim))
		n = lim
	}
	buf := make([]byte, n)
	if _, err := v.readContent(c, buf, 0); err == nil || errors.Is(err, io.EOF) {
		return buf, errors.Join(errs...)
	}
	bad, total := 0, 0
	var first error
	for off := int64(0); off < n; off += v.bs {
		total++
		chunk := buf[off:min(off+v.bs, n)]
		if _, err := v.readContent(c, chunk, off); err != nil && !errors.Is(err, io.EOF) {
			clear(chunk)
			bad++
			if first == nil {
				first = err
			}
		}
	}
	if bad > 0 {
		errs = append(errs, fmt.Errorf("%d of %d directory blocks unreadable: %w", bad, total, first))
	}
	return buf, errors.Join(errs...)
}

// parseFIDs decodes the file identifier descriptors of a directory (ECMA-167
// 4/14.4), which form a byte stream over the directory's extents (an
// identifier may straddle two extents), and loads each entry's file entry.
// Unparseable data is skipped up to the next valid identifier tag.
func (v *Volume) parseFIDs(d *Entry, data []byte, streamOf string, limit int) []*Entry {
	var out []*Entry
	names := map[string]bool{}
	for pos := 0; pos < len(data); {
		if len(out) >= limit {
			v.warn(d.Path, "more than %d entries; the rest are ignored", limit)
			break
		}
		rest := data[pos:]
		if len(rest) < 38 || le16(rest) != tagFID {
			if allZero(rest) {
				break
			}
			np := resync(data, pos+4)
			if np < 0 {
				v.warn(d.Path, "unparseable directory data from offset %d", pos)
				break
			}
			v.warn(d.Path, "skipped %d bytes of unparseable directory data at offset %d", np-pos, pos)
			pos = np
			continue
		}
		lfi, liu := int(rest[19]), int(le16(rest[36:]))
		total := 38 + liu + lfi
		if total > len(rest) {
			v.warn(d.Path, "file identifier at offset %d overruns the directory", pos)
			break
		}
		padded := min((total+3)&^3, len(rest))
		f := rest[:total]
		loc, ok := d.data.blockAt(int64(pos), v.bs, d.loc.block)
		if !ok {
			loc = le32(rest[12:]) // nothing to check against
		}
		_ = v.checkTag(d.Path, "file identifier", rest[:padded], tagFID, loc)
		pos += padded
		char := f[18]
		if char&(CharDeleted|CharParent) != 0 {
			continue
		}
		icb := parseLongAD(f[20:36])
		raw := f[38+liu : total]
		name, msg := decodeCS0(raw)
		e := &Entry{Name: name, RawName: bytes.Clone(raw), Characteristics: char, Hidden: char&CharHidden != 0,
			IsDir: char&CharDirectory != 0, FileVersion: le16(f[16:]), ICB: ICB{Partition: icb.part, Block: icb.block}, vol: v}
		pn, safe := pathName(name)
		if streamOf != "" {
			e.Path = streamOf + ":" + pn
		} else {
			e.Path = childPath(d.Path, pn)
		}
		if msg != "" {
			v.warn(e.Path, "file identifier: %s", msg)
		}
		if !safe {
			v.warn(e.Path, "name %q cannot be used in a path", name)
		}
		if names[name] {
			v.warn(e.Path, "duplicate name in the directory")
		}
		names[name] = true
		if icb.size() == 0 {
			v.warn(e.Path, "file identifier's ICB has length 0")
		}
		v.loadEntry(e, icb, streamOf != "")
		out = append(out, e)
	}
	return out
}

// resync finds the next 4-byte aligned offset at or after from that holds
// a file identifier tag with a valid checksum, or -1.
func resync(data []byte, from int) int {
	for p := (from + 3) &^ 3; p+38 <= len(data); p += 4 {
		if tagValid(data[p:p+16], tagFID) {
			return p
		}
	}
	return -1
}

// SystemStreams returns the streams of the file set's system stream
// directory (UDF 2.00+), such as "*UDF Unique ID Mapping Data", or nil
// when it has none. Their paths start with ":".
func (v *Volume) SystemStreams() ([]*Entry, error) {
	if !v.info.HasSystemStreams {
		return nil, nil
	}
	sd := &Entry{Path: "", IsDir: true, Characteristics: CharDirectory, vol: v}
	v.loadEntry(sd, v.sysStreams, true)
	if sd.err != nil {
		return nil, sd.err
	}
	ents, err := v.readDir(sd, "")
	for _, e := range ents {
		e.Stream = true
		e.Path = ":" + strings.TrimPrefix(e.Path, "/")
	}
	return ents, err
}
