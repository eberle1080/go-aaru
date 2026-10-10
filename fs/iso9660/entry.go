package iso9660

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// Directory record flags (Entry.Flags).
const (
	FlagHidden      = 0x01 // "existence": hide the entry from the user
	FlagDirectory   = 0x02
	FlagAssociated  = 0x04 // associated file (e.g. a Macintosh resource fork)
	FlagRecord      = 0x08 // record format given in the extended attribute record
	FlagProtection  = 0x10 // permissions given in the extended attribute record
	FlagMultiExtent = 0x80 // not the final record of a multi-extent file
)

// Extent is one directory record's extent. A file's data is the
// concatenation of its extents' data.
type Extent struct {
	// LBA is the extent location as recorded, in logical blocks. The
	// extended attribute record, if any, occupies its first EARLength
	// blocks; the data follows.
	LBA       uint32
	EARLength uint8
	// Length is the number of data bytes (excluding the extended attribute
	// record and interleave gaps).
	Length uint32
	// FileUnitSize and InterleaveGap are in logical blocks; both are 0 for
	// a file that is not interleaved.
	FileUnitSize  uint8
	InterleaveGap uint8
}

// Entry is a file or directory.
type Entry struct {
	// Name is the name in the volume's namespace: the Rock Ridge NM name,
	// the Joliet name, or the ISO 9660 identifier without its ";version"
	// suffix (and without the trailing '.' of a file with no extension).
	// The root's Name is "".
	Name string
	// Path is the "/"-joined path from the root ("/" for the root). Names
	// that cannot appear in a path ("", ".", "..", or containing '/') are
	// replaced by a placeholder in Path, with a warning.
	Path string
	// RawName is the identifier exactly as recorded (UCS-2 for Joliet).
	RawName []byte
	// Version is the ";N" file version number, or 0 when absent.
	Version uint16

	IsDir bool
	// Size is the total data size over all extents.
	Size    int64
	Extents []Extent

	// Flags is the raw flags byte of the (first) directory record.
	Flags       byte
	Hidden      bool
	Associated  bool
	Record      bool
	Protection  bool
	MultiExtent bool

	// RecordedTime is the directory record's recording date, nil if unset.
	RecordedTime   *time.Time
	VolumeSequence uint16

	XA        *XAInfo
	RockRidge *RRInfo
	Apple     *AppleInfo
	// SUSP holds system use entries this package does not interpret.
	SUSP []SUSPEntry

	// AssociatedFile is set on a main file whose directory also holds an
	// associated file (FlagAssociated) of the same name.
	AssociatedFile *Entry
	// Relocated is set on a Rock Ridge directory reached through a CL
	// child link: its extent and attributes are the relocated directory's.
	Relocated bool

	tree *tree
}

// Mode returns the entry's permissions and type: the Rock Ridge PX mode when
// present, else read-only (and executable for directories).
func (e *Entry) Mode() fs.FileMode {
	if e.RockRidge != nil && e.RockRidge.HasPX {
		m := e.RockRidge.Mode
		if e.IsDir && m.Type() == 0 {
			m |= fs.ModeDir
		}
		return m
	}
	if e.IsDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// ModTime returns the Rock Ridge modification time when present, else the
// recording time, else the zero time.
func (e *Entry) ModTime() time.Time {
	if e.RockRidge != nil && e.RockRidge.Times.Modify != nil {
		return *e.RockRidge.Times.Modify
	}
	if e.RecordedTime != nil {
		return *e.RecordedTime
	}
	return time.Time{}
}

// record is a parsed directory record.
type record struct {
	earLen    byte
	lba, size uint32
	date      []byte
	flags     byte
	unit, gap byte
	volSeq    uint16
	id        []byte
	su        []byte
	mismatch  bool // a both-endian field's halves differ
}

const minRecordLen = 34 // 33 fixed bytes and at least one identifier byte

// parseRecord parses the directory record b (len(b) == b[0]).
func parseRecord(b []byte) (record, error) {
	if len(b) < minRecordLen {
		return record{}, fmt.Errorf("%w: directory record of %d bytes", ErrCorrupt, len(b))
	}
	idLen := int(b[32])
	if idLen == 0 || 33+idLen > len(b) {
		return record{}, fmt.Errorf("%w: identifier of %d bytes in a %d-byte record", ErrCorrupt, idLen, len(b))
	}
	r := record{
		earLen: b[1],
		lba:    binary.LittleEndian.Uint32(b[2:]),
		size:   binary.LittleEndian.Uint32(b[10:]),
		date:   b[18:25],
		flags:  b[25],
		unit:   b[26],
		gap:    b[27],
		volSeq: binary.LittleEndian.Uint16(b[28:]),
		id:     b[33 : 33+idLen],
	}
	r.mismatch = r.lba != binary.BigEndian.Uint32(b[6:]) || r.size != binary.BigEndian.Uint32(b[14:])
	suStart := 33 + idLen
	if idLen%2 == 0 {
		suStart++ // padding byte
	}
	if suStart < len(b) {
		r.su = b[suStart:]
	}
	return r, nil
}

func isDotRecord(id []byte) bool { return len(id) == 1 && (id[0] == 0 || id[0] == 1) }

// splitName separates a ";N" version suffix.
func splitName(name string) (string, uint16) {
	i := strings.LastIndexByte(name, ';')
	if i < 0 || i == len(name)-1 || len(name)-i-1 > 5 {
		return name, 0
	}
	v := 0
	for _, c := range name[i+1:] {
		if c < '0' || c > '9' {
			return name, 0
		}
		v = v*10 + int(c-'0')
	}
	if v > 0xFFFF {
		return name, 0
	}
	return name[:i], uint16(v)
}

// decodeName decodes an identifier in tree t.
func decodeName(t *tree, id []byte, dir bool) (string, uint16) {
	var s string
	if t.joliet {
		s = ucs2(id)
	} else {
		s = decodeBytes(id)
	}
	if dir {
		return s, 0
	}
	name, ver := splitName(s)
	if len(name) > 1 && strings.HasSuffix(name, ".") {
		name = name[:len(name)-1]
	}
	return name, ver
}

// pathName returns name made safe for use as a path element.
func pathName(name string) (string, bool) {
	switch name {
	case "":
		return "_", false
	case ".", "..":
		return "_" + name, false
	}
	if strings.ContainsRune(name, '/') {
		return strings.ReplaceAll(name, "/", "_"), false
	}
	return name, true
}

func childPath(parent, name string) string {
	if parent == "/" || parent == "" {
		return "/" + name
	}
	return parent + "/" + name
}

// suspOf returns the SUSP part of a record's system use field and the XA
// information.
func suspOf(t *tree, su []byte) ([]byte, *XAInfo) {
	xa := parseXA(su)
	if !t.susp {
		return nil, xa
	}
	skip := t.suspSkip
	if xa != nil && skip < 14 {
		skip = 14
	}
	if skip >= len(su) {
		return nil, xa
	}
	return su[skip:], xa
}

// applySUSP parses and attaches the SUSP entries of a record.
func (v *Volume) applySUSP(t *tree, e *Entry, su []byte) {
	if len(su) == 0 {
		return
	}
	res := v.parseSUSP(t, su)
	for _, w := range res.warns {
		v.warn(e.Path, "%s", w)
	}
	e.RockRidge, e.Apple, e.SUSP = res.rr, res.apple, res.other
}

// newEntry builds the entry for record r in directory parent.
func (v *Volume) newEntry(t *tree, parent *Entry, r record) *Entry {
	dir := r.flags&FlagDirectory != 0
	e := &Entry{
		RawName:        bytes.Clone(r.id),
		IsDir:          dir,
		Flags:          r.flags,
		Hidden:         r.flags&FlagHidden != 0,
		Associated:     r.flags&FlagAssociated != 0,
		Record:         r.flags&FlagRecord != 0,
		Protection:     r.flags&FlagProtection != 0,
		MultiExtent:    r.flags&FlagMultiExtent != 0,
		VolumeSequence: r.volSeq,
		Extents:        []Extent{recordExtent(r)},
		tree:           t,
	}
	e.Name, e.Version = decodeName(t, r.id, dir)
	e.Path = childPath(parent.Path, e.Name) // provisional, for warnings
	su, xa := suspOf(t, r.su)
	e.XA = xa
	v.applySUSP(t, e, su)
	if t.rrNames && e.RockRidge != nil && e.RockRidge.Name != "" {
		e.Name = e.RockRidge.Name
	}
	pn, ok := pathName(e.Name)
	e.Path = childPath(parent.Path, pn)
	if !ok {
		v.warn(e.Path, "name %q cannot be used in a path", e.Name)
	}
	var msg string
	e.RecordedTime, msg = shortDateTime(r.date)
	if msg != "" {
		v.warn(e.Path, "recording date %s", msg)
	}
	if r.mismatch {
		v.warn(e.Path, "both-endian extent fields disagree; using the little-endian values")
	}
	if r.unit == 0 && r.gap != 0 {
		v.warn(e.Path, "interleave gap %d without a file unit size", r.gap)
	}
	return e
}

func recordExtent(r record) Extent {
	return Extent{LBA: r.lba, EARLength: r.earLen, Length: r.size, FileUnitSize: r.unit, InterleaveGap: r.gap}
}

// rootEntry builds the root directory entry of tree t.
func (v *Volume) rootEntry(t *tree) *Entry {
	e := &Entry{Path: "/", RawName: []byte{0}, IsDir: true, Flags: FlagDirectory, tree: t,
		Extents: []Extent{{LBA: t.info.RootLBA, Length: t.info.RootSize}}}
	if r, err := parseRecord(t.info.rootRecord[:]); err == nil {
		e.Flags = r.flags | FlagDirectory
		e.Hidden = r.flags&FlagHidden != 0
		e.VolumeSequence = r.volSeq
		e.Extents[0].EARLength = r.earLen
		var msg string
		e.RecordedTime, msg = shortDateTime(r.date)
		if msg != "" {
			v.warn("/", "recording date %s", msg)
		}
	}
	e.Size = int64(t.info.RootSize)
	e.XA = t.rootXA
	if t.susp {
		v.applySUSP(t, e, t.rootSU)
	}
	return e
}

// splitRecords returns the directory records in a directory's data. Zero
// length bytes are padding up to the next sector; a malformed record skips
// the rest of its sector.
func (v *Volume) splitRecords(path string, data []byte) []record {
	var recs []record
	for pos := 0; pos < len(data); {
		l := int(data[pos])
		next := (pos/SectorSize + 1) * SectorSize
		if l == 0 {
			pos = next
			continue
		}
		if pos+l > len(data) {
			v.warn(path, "directory record at offset %d overruns the directory", pos)
			break
		}
		r, err := parseRecord(data[pos : pos+l])
		if err != nil {
			v.warn(path, "directory record at offset %d: %v; skipping to the next sector", pos, err)
			pos = next
			continue
		}
		recs = append(recs, r)
		pos += l
	}
	return recs
}

// readDirBytes reads the data of directory extent x, sector by sector when
// a single read fails, so that unreadable sectors lose only their records.
func (v *Volume) readDirBytes(t *tree, x Extent) ([]byte, error) {
	var errs []error
	start := (int64(x.LBA) + int64(x.EARLength)) * t.blockSize
	n := int64(x.Length)
	if n > v.opts.MaxDirSize {
		errs = append(errs, fmt.Errorf("%w: directory of %d bytes exceeds the %d-byte limit; truncated",
			ErrCorrupt, n, v.opts.MaxDirSize))
		n = v.opts.MaxDirSize
	}
	if start >= v.size {
		return nil, fmt.Errorf("%w: directory at block %d", ErrOutOfRange, x.LBA)
	}
	if n > v.size-start {
		errs = append(errs, fmt.Errorf("%w: directory at block %d runs past the end of the volume; truncated",
			ErrOutOfRange, x.LBA))
		n = v.size - start
	}
	buf := make([]byte, n)
	if err := v.readAt(buf, start); err == nil {
		return buf, errors.Join(errs...)
	}
	bad, total := 0, 0
	var first error
	for off := int64(0); off < n; off += SectorSize {
		total++
		chunk := buf[off:min(off+SectorSize, n)]
		if err := v.readAt(chunk, start+off); err != nil {
			clear(chunk)
			bad++
			if first == nil {
				first = err
			}
		}
	}
	if bad > 0 {
		errs = append(errs, fmt.Errorf("%d of %d directory sectors unreadable: %w", bad, total, first))
	}
	return buf, errors.Join(errs...)
}

// ReadDir returns the entries of directory d in disc order, without "." and
// "..". Multi-extent files are merged into one entry. On a damaged
// directory it returns the entries it could read together with an error.
func (v *Volume) ReadDir(d *Entry) ([]*Entry, error) {
	if d == nil || !d.IsDir || d.tree == nil {
		return nil, ErrNotDirectory
	}
	t := d.tree
	if len(d.Extents) == 0 || d.Extents[0].Length == 0 {
		return nil, nil
	}
	data, rerr := v.readDirBytes(t, d.Extents[0])
	if rerr != nil {
		rerr = fmt.Errorf("iso9660: directory %s: %w", d.Path, rerr)
	}
	recs := v.splitRecords(d.Path, data)

	var out []*Entry
	var pending *Entry // multi-extent file awaiting its final record
	finish := func(e *Entry) {
		for _, x := range e.Extents {
			e.Size += int64(x.Length)
		}
		e.MultiExtent = len(e.Extents) > 1 || e.Flags&FlagMultiExtent != 0
		out = append(out, e)
	}
	for _, r := range recs {
		if isDotRecord(r.id) {
			continue
		}
		if pending != nil {
			if bytes.Equal(r.id, pending.RawName) {
				pending.Extents = append(pending.Extents, recordExtent(r))
				if r.flags&FlagMultiExtent == 0 {
					finish(pending)
					pending = nil
				}
				continue
			}
			v.warn(pending.Path, "multi-extent file ends without a final record")
			finish(pending)
			pending = nil
		}
		e := v.newEntry(t, d, r)
		if r.flags&FlagMultiExtent != 0 {
			if e.IsDir {
				v.warn(e.Path, "directory marked multi-extent; only its first extent is read")
			} else {
				pending = e
				continue
			}
		}
		finish(e)
	}
	if pending != nil {
		v.warn(pending.Path, "multi-extent file ends without a final record")
		finish(pending)
	}

	if t.rrReloc {
		kept := out[:0]
		for _, e := range out {
			rr := e.RockRidge
			if rr != nil && rr.Relocated && e.IsDir && !v.opts.ShowRelocated {
				continue
			}
			if rr != nil && rr.HasChildLink {
				v.resolveChildLink(t, e)
			}
			kept = append(kept, e)
		}
		clear(out[len(kept):])
		out = kept
	}

	// Pair associated files with their main file.
	main := map[string]*Entry{}
	for _, e := range out {
		if !e.Associated {
			if _, ok := main[e.Name]; !ok {
				main[e.Name] = e
			}
		}
	}
	for _, e := range out {
		if e.Associated {
			if m := main[e.Name]; m != nil && m.AssociatedFile == nil {
				m.AssociatedFile = e
			}
		}
	}
	return out, rerr
}

// resolveChildLink turns a Rock Ridge CL placeholder into the directory it
// points to, taking the extent and attributes from that directory's "."
// record. On failure the placeholder is left as it is, with a warning.
func (v *Volume) resolveChildLink(t *tree, e *Entry) {
	lba := e.RockRidge.ChildLink
	buf := make([]byte, SectorSize)
	off := int64(lba) * t.blockSize
	if err := v.readAt(buf, off); err != nil {
		v.warn(e.Path, "Rock Ridge child link to block %d: %v", lba, err)
		return
	}
	l := int(buf[0])
	if l < minRecordLen {
		v.warn(e.Path, "Rock Ridge child link to block %d: no directory there", lba)
		return
	}
	r, err := parseRecord(buf[:l])
	if err != nil || len(r.id) != 1 || r.id[0] != 0 || r.flags&FlagDirectory == 0 {
		v.warn(e.Path, "Rock Ridge child link to block %d: no directory there", lba)
		return
	}
	if r.lba != lba {
		v.warn(e.Path, "Rock Ridge child link to block %d whose \".\" says block %d", lba, r.lba)
	}
	old := e.RockRidge
	e.IsDir, e.Relocated = true, true
	e.Extents = []Extent{{LBA: lba, EARLength: r.earLen, Length: r.size}}
	e.Size = int64(r.size)
	if tm, _ := shortDateTime(r.date); tm != nil {
		e.RecordedTime = tm
	}
	su, _ := suspOf(t, r.su)
	if len(su) > 0 {
		res := v.parseSUSP(t, su)
		for _, w := range res.warns {
			v.warn(e.Path, "%s", w)
		}
		if res.rr != nil {
			rr := *res.rr
			rr.Name, rr.NameFlags = old.Name, old.NameFlags
			rr.HasChildLink, rr.ChildLink = true, lba
			e.RockRidge = &rr
		}
	}
}

// ExtendedAttributeRecord returns the raw extended attribute record of an
// entry's first extent, or nil when it has none.
func (v *Volume) ExtendedAttributeRecord(e *Entry) ([]byte, error) {
	if e == nil || e.tree == nil || len(e.Extents) == 0 || e.Extents[0].EARLength == 0 {
		return nil, nil
	}
	x := e.Extents[0]
	buf := make([]byte, int64(x.EARLength)*e.tree.blockSize)
	if err := v.readAt(buf, int64(x.LBA)*e.tree.blockSize); err != nil {
		return nil, fmt.Errorf("iso9660: extended attribute record of %s: %w", e.Path, err)
	}
	return buf, nil
}
