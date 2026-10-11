package udf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// fileEntry is a decoded file entry or extended file entry (ECMA-167
// 4/14.9 and 4/14.17).
type fileEntry struct {
	loc       addr
	efe       bool
	strategy  uint16
	fileType  byte
	flags     uint16
	uid, gid  uint32
	perms     uint32
	links     uint16
	infoLen   uint64
	blocks    uint64
	atime     []byte
	mtime     []byte
	ctime     []byte
	attrTime  []byte
	eaICB     longAD
	streamICB longAD
	impl      string
	uniqueID  uint64
	ea, ad    []byte
}

const maxICBHops = 1024

// readFE reads the file entry of the ICB at a. For strategy 4096 it follows
// indirect entries (in the second block of each ICB) to the most recent
// direct entry; a broken or looping chain keeps the last entry found.
func (v *Volume) readFE(path string, a addr) (*fileEntry, error) {
	seen := map[addr]bool{}
	var best *fileEntry
	cur := a
	fail := func(err error) (*fileEntry, error) {
		if best != nil {
			v.warn(path, "ICB continuation: %v; using the last file entry found", err)
			return best, nil
		}
		return nil, err
	}
	for hop := 0; ; hop++ {
		if hop >= maxICBHops || seen[cur] {
			return fail(fmt.Errorf("%w: ICB chain revisits block %d", ErrLoop, cur.block))
		}
		seen[cur] = true
		b, err := v.readLogical(cur)
		if err != nil {
			return fail(err)
		}
		switch id := le16(b); id {
		case tagFE, tagEFE:
			fe, err := v.parseFE(path, b, cur)
			if err != nil {
				return fail(err)
			}
			best = fe
			if fe.strategy != 4096 || cur.block == math.MaxUint32 {
				return best, nil
			}
			// Strategy 4096: the second entry of the ICB is either
			// unrecorded or an indirect entry to a newer ICB.
			nb, err := v.readLogical(addr{cur.part, cur.block + 1})
			if err != nil || !tagValid(nb, tagIE) {
				return best, nil
			}
			_ = v.checkTag(path, "indirect entry", nb, tagIE, cur.block+1)
			nx, err := v.indirectTarget(nb)
			if err != nil {
				return fail(err)
			}
			cur = nx
		case tagIE:
			_ = v.checkTag(path, "indirect entry", b, tagIE, cur.block)
			nx, err := v.indirectTarget(b)
			if err != nil {
				return fail(err)
			}
			cur = nx
		case tagTE:
			return fail(fmt.Errorf("%w: terminal entry at block %d", ErrCorrupt, cur.block))
		case tagUSE:
			return fail(fmt.Errorf("%w: unallocated space entry at block %d, not a file entry", ErrCorrupt, cur.block))
		default:
			return fail(fmt.Errorf("%w: no file entry at block %d (tag %d)", ErrCorrupt, cur.block, id))
		}
	}
}

// indirectTarget returns the ICB an indirect entry points to.
func (v *Volume) indirectTarget(b []byte) (addr, error) {
	l := parseLongAD(b[36:52])
	part, err := v.partRef(l.part)
	if err != nil {
		return addr{}, err
	}
	if l.size() == 0 {
		return addr{}, fmt.Errorf("%w: indirect entry with an empty target", ErrCorrupt)
	}
	return addr{part, l.block}, nil
}

// parseFE decodes the file entry or extended file entry b recorded at loc.
func (v *Volume) parseFE(path string, b []byte, loc addr) (*fileEntry, error) {
	id := le16(b)
	fe := &fileEntry{loc: loc, efe: id == tagEFE}
	what := "file entry"
	if fe.efe {
		what = "extended file entry"
	}
	if err := v.checkTag(path, what, b, id, loc.block); err != nil {
		return nil, err
	}
	icb := b[16:36]
	fe.strategy, fe.fileType, fe.flags = le16(icb[4:]), icb[11], le16(icb[18:])
	fe.uid, fe.gid, fe.perms, fe.links = le32(b[36:]), le32(b[40:]), le32(b[44:]), le16(b[48:])
	fe.infoLen = le64(b[56:])
	var base int
	var lea, lad uint32
	if fe.efe {
		fe.blocks = le64(b[72:])
		fe.atime, fe.mtime, fe.ctime, fe.attrTime = b[80:92], b[92:104], b[104:116], b[116:128]
		fe.eaICB, fe.streamICB = parseLongAD(b[136:152]), parseLongAD(b[152:168])
		fe.impl = parseRegid(b[168:200]).id
		fe.uniqueID = le64(b[200:])
		lea, lad, base = le32(b[208:]), le32(b[212:]), 216
	} else {
		fe.blocks = le64(b[64:])
		fe.atime, fe.mtime, fe.attrTime = b[72:84], b[84:96], b[96:108]
		fe.eaICB = parseLongAD(b[112:128])
		fe.impl = parseRegid(b[128:160]).id
		fe.uniqueID = le64(b[160:])
		lea, lad, base = le32(b[168:]), le32(b[172:]), 176
	}
	room := uint32(len(b) - base)
	if lea > room {
		v.warn(path, "%s at block %d: extended attributes length %d exceeds the entry", what, loc.block, lea)
		lea = room
	}
	if lad > room-lea {
		v.warn(path, "%s at block %d: allocation descriptors length %d exceeds the entry", what, loc.block, lad)
		lad = room - lea
	}
	fe.ea = b[base : base+int(lea)]
	fe.ad = b[base+int(lea) : base+int(lea)+int(lad)]
	return fe, nil
}

// extent is one allocation descriptor's extent.
type extent struct {
	length uint32 // bytes
	kind   byte   // 0 recorded, 1 allocated but not recorded, 2 not allocated
	part   int    // -1 for an invalid partition reference
	block  uint32
}

const (
	maxExtents = 1 << 20
	maxAEDs    = 1 << 16
)

// allocation decodes a file entry's allocation descriptors (short, long or
// extended), following allocation extent descriptor chains. On failure it
// returns the extents read so far with the error.
func (v *Volume) allocation(path string, fe *fileEntry) ([]extent, error) {
	adType := fe.flags & 7
	var size int
	switch adType {
	case 0:
		size = 8
	case 1:
		size = 16
	case 2:
		size = 20
	default:
		return nil, fmt.Errorf("%w: allocation descriptor type %d", ErrCorrupt, adType)
	}
	buf := fe.ad
	var exts []extent
	seen := map[addr]bool{}
	for {
		next := false
		for pos := 0; pos+size <= len(buf) && !next; pos += size {
			d := buf[pos : pos+size]
			lf := le32(d)
			kind, l := byte(lf>>30), lf&0x3FFFFFFF
			if l == 0 {
				return exts, nil
			}
			part, blk := fe.loc.part, le32(d[4:])
			var perr error
			switch adType {
			case 1:
				part, perr = v.partRef(le16(d[8:]))
			case 2:
				blk = le32(d[12:])
				part, perr = v.partRef(le16(d[16:]))
			}
			if kind == 3 {
				if perr != nil {
					return exts, perr
				}
				a := addr{part, blk}
				if seen[a] || len(seen) >= maxAEDs {
					return exts, fmt.Errorf("%w: allocation extent descriptors revisit block %d", ErrLoop, blk)
				}
				seen[a] = true
				// An allocation extent descriptor occupies at most one block.
				aed := make([]byte, min(max(int64(l), 24), v.bs))
				if err := v.readPart(part, int64(blk)*v.bs, aed); err != nil {
					return exts, fmt.Errorf("allocation extent descriptor at block %d: %w", blk, err)
				}
				if err := v.checkTag(path, "allocation extent descriptor", aed, tagAED, blk); err != nil {
					return exts, err
				}
				lad := le32(aed[20:])
				if uint64(lad) > uint64(len(aed)-24) {
					v.warn(path, "allocation extent descriptor at block %d: length %d exceeds the extent", blk, lad)
					lad = uint32(len(aed) - 24)
				}
				buf = aed[24 : 24+lad]
				next = true
				continue
			}
			if perr != nil {
				if kind == 0 {
					v.warn(path, "extent at block %d: %v", blk, perr)
				}
				part = -1
			}
			if adType == 2 && kind == 0 {
				// An extended descriptor may record only part of its extent.
				if rec := le32(d[4:]) & 0x3FFFFFFF; rec < l {
					if rec > 0 {
						exts = append(exts, extent{length: rec, kind: 0, part: part, block: blk})
					}
					kind, l = 1, l-rec
				}
			}
			exts = append(exts, extent{length: l, kind: kind, part: part, block: blk})
			if len(exts) > maxExtents {
				return exts, fmt.Errorf("%w: more than %d extents", ErrCorrupt, maxExtents)
			}
		}
		if !next {
			return exts, nil
		}
	}
}

// span maps file bytes [logical, logical+length) to partition space.
type span struct {
	logical, length int64
	zero            bool
	part            int
	block           uint32
}

// content is the layout of a file's data.
type content struct {
	size     int64
	spans    []span
	embedded bool
	emb      []byte
	sparse   bool
	tailErr  error // reading past the spans fails with this error
}

// contentOf lays out a file entry's data.
func (v *Volume) contentOf(path string, fe *fileEntry) (*content, error) {
	size := int64(min(fe.infoLen, math.MaxInt64))
	c := &content{size: size}
	limit := func(have int64) {
		if c.size > have && c.size > v.size {
			v.warn(path, "information length %d exceeds the volume; reading %d bytes", c.size, have)
			c.size = have
		}
	}
	switch t := fe.flags & 7; {
	case t == 3:
		c.embedded, c.emb = true, bytes.Clone(fe.ad)
		if int64(len(c.emb)) < c.size {
			limit(int64(len(c.emb)))
			if c.size > int64(len(c.emb)) {
				v.warn(path, "information length %d exceeds the %d bytes embedded in the file entry; the rest reads as zeros",
					c.size, len(c.emb))
				c.sparse = true
			}
		}
		return c, nil
	case t > 3:
		return nil, fmt.Errorf("%w: allocation descriptor type %d", ErrCorrupt, t)
	}
	exts, err := v.allocation(path, fe)
	var cur int64
	for _, x := range exts {
		if cur >= c.size {
			break
		}
		l := min(int64(x.length), c.size-cur)
		if x.kind != 0 {
			c.sparse = true
		}
		c.spans = append(c.spans, span{logical: cur, length: l, zero: x.kind != 0, part: x.part, block: x.block})
		cur += l
	}
	switch {
	case err != nil && cur < c.size:
		c.tailErr = fmt.Errorf("udf: %s: allocation descriptors: %w", path, err)
		v.warn(path, "allocation descriptors unreadable after %d bytes: %v", cur, err)
	case err != nil:
		v.warn(path, "allocation descriptors: %v", err)
	case cur < c.size:
		limit(cur)
		if cur < c.size {
			v.warn(path, "information length %d exceeds the %d bytes allocated; the rest reads as zeros", c.size, cur)
			c.sparse = true
		}
	}
	return c, nil
}

// readContent reads file data at off.
func (v *Volume) readContent(c *content, p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("udf: negative offset %d", off)
	}
	n := 0
	for len(p) > 0 {
		if off >= c.size {
			return n, io.EOF
		}
		var k int
		if c.embedded {
			k = int(min(int64(len(p)), c.size-off))
			if off < int64(len(c.emb)) {
				k = copy(p[:k], c.emb[off:])
			} else {
				clear(p[:k])
			}
		} else {
			i := sort.Search(len(c.spans), func(i int) bool { return c.spans[i].logical+c.spans[i].length > off })
			if i == len(c.spans) {
				if c.tailErr != nil {
					return n, c.tailErr
				}
				k = int(min(int64(len(p)), c.size-off))
				clear(p[:k])
			} else {
				s := c.spans[i]
				rel := off - s.logical
				k = int(min(int64(len(p)), s.length-rel))
				if s.zero {
					clear(p[:k])
				} else if err := v.readPart(s.part, int64(s.block)*v.bs+rel, p[:k]); err != nil {
					return n, err
				}
			}
		}
		n += k
		off += int64(k)
		p = p[k:]
	}
	return n, nil
}

// blockAt returns the partition block holding byte off of the content, for
// checking the tag locations of file identifiers.
func (c *content) blockAt(off, bs int64, icb uint32) (uint32, bool) {
	if c.embedded {
		return icb, true
	}
	i := sort.Search(len(c.spans), func(i int) bool { return c.spans[i].logical+c.spans[i].length > off })
	if i == len(c.spans) || c.spans[i].zero {
		return 0, false
	}
	s := c.spans[i]
	return s.block + uint32((off-s.logical)/bs), true
}

// loadEntry reads the file entry of e at icb and fills in its attributes.
// Failures are recorded in e.err.
func (v *Volume) loadEntry(e *Entry, icb longAD, inStreamDir bool) {
	part, err := v.partRef(icb.part)
	if err != nil {
		e.err = fmt.Errorf("udf: %s: %w", e.Path, err)
		return
	}
	fe, err := v.readFE(e.Path, addr{part, icb.block})
	if err != nil {
		e.err = fmt.Errorf("udf: %s: %w", e.Path, err)
		return
	}
	e.loc = fe.loc
	e.FileType = fe.fileType
	e.ICBFlags = fe.flags
	e.UID, e.GID = fe.uid, fe.gid
	e.HasUID, e.HasGID = fe.uid != math.MaxUint32, fe.gid != math.MaxUint32
	e.Permissions = fe.perms
	e.LinkCount = fe.links
	e.UniqueID = fe.uniqueID
	e.ImplementationID = fe.impl
	e.ExtendedFileEntry = fe.efe
	e.LogicalBlocksRecorded = fe.blocks
	if fe.infoLen > math.MaxInt64 {
		v.warn(e.Path, "information length %d is too large", fe.infoLen)
	}
	for _, t := range []struct {
		dst  **time.Time
		b    []byte
		what string
	}{
		{&e.Times.Access, fe.atime, "access time"},
		{&e.Times.Modification, fe.mtime, "modification time"},
		{&e.Times.Attribute, fe.attrTime, "attribute time"},
		{&e.Times.Creation, fe.ctime, "creation time"},
	} {
		if t.b == nil {
			continue
		}
		tm, msg := timestamp(t.b)
		if msg != "" {
			v.warn(e.Path, "%s: %s", t.what, msg)
		}
		*t.dst = tm
	}

	fidDir := e.Characteristics&CharDirectory != 0
	switch fe.fileType {
	case FileTypeDirectory, FileTypeStreamDirectory:
		e.IsDir = true
	case FileTypeUnspecified:
		e.IsDir = fidDir
	default:
		e.IsDir = false
	}
	if e.IsDir != fidDir && fe.fileType != FileTypeUnspecified {
		v.warn(e.Path, "file identifier and file entry (type %d) disagree on whether this is a directory; using the file entry", fe.fileType)
	}
	if fe.flags&ICBFlagStream != 0 && !inStreamDir {
		v.warn(e.Path, "file entry is flagged as a stream")
	}

	c, err := v.contentOf(e.Path, fe)
	if err != nil {
		e.err = fmt.Errorf("udf: %s: %w", e.Path, err)
		return
	}
	e.Size = c.size
	e.data = c
	e.Sparse, e.Embedded = c.sparse, c.embedded

	v.parseEAs(e, fe.ea, fe.loc.block)
	if fe.eaICB.size() != 0 {
		v.readEAFile(e, fe.eaICB)
	}
	if fe.fileType == FileTypeSymlink {
		v.readSymlink(e)
	}
	if fe.efe && fe.streamICB.size() != 0 && !inStreamDir {
		v.loadStreams(e, fe.streamICB)
	}
}

// parseEAs decodes an extended attribute space: an extended attribute
// header descriptor followed by the attributes (ECMA-167 4/14.10).
func (v *Volume) parseEAs(e *Entry, b []byte, loc uint32) {
	if len(b) == 0 {
		return
	}
	if len(b) < 24 {
		v.warn(e.Path, "extended attribute space of %d bytes is too short", len(b))
		return
	}
	if err := v.checkTag(e.Path, "extended attribute header", b[:24], tagEAHD, loc); err != nil {
		v.warn(e.Path, "%v", err)
		return
	}
	for pos := 24; pos+12 <= len(b); {
		typ, sub, l := le32(b[pos:]), b[pos+4], int64(le32(b[pos+8:]))
		if l < 12 || l > int64(len(b)-pos) {
			if typ != 0 || l != 0 {
				v.warn(e.Path, "extended attribute at offset %d has invalid length %d", pos, l)
			}
			return
		}
		raw := b[pos : pos+int(l)]
		pos += int(l)
		ea := ExtendedAttribute{Type: typ, Subtype: sub, Raw: bytes.Clone(raw), Data: bytes.Clone(raw[12:])}
		body := raw[12:]
		switch typ {
		case EAImplementationUse, EAApplicationUse:
			if len(body) < 36 {
				v.warn(e.Path, "extended attribute type %d truncated", typ)
				break
			}
			iul := int64(le32(body))
			ea.ImplementationID = parseRegid(body[4:36]).id
			iu := body[36:]
			if iul > int64(len(iu)) {
				v.warn(e.Path, "extended attribute %q: implementation use length %d exceeds the attribute", ea.ImplementationID, iul)
				iul = int64(len(iu))
			}
			ea.Data = bytes.Clone(iu[:iul])
			if ea.ImplementationID == "*UDF Mac FinderInfo" {
				if len(ea.Data) >= 40 {
					e.MacFinderInfo = bytes.Clone(ea.Data[8:40])
				} else {
					v.warn(e.Path, "Mac FinderInfo extended attribute of %d bytes is too short", len(ea.Data))
				}
			}
		case EAFileTimes:
			// Data length, existence bits (0: creation, 1: deletion, 2:
			// effective, 3: last backup), then the times present.
			if len(body) >= 20 && le32(body[4:])&1 != 0 && e.Times.Creation == nil {
				tm, msg := timestamp(body[8:20])
				if msg != "" {
					v.warn(e.Path, "creation time attribute: %s", msg)
				}
				e.Times.Creation = tm
			}
		case EADeviceSpecification:
			if len(body) < 12 {
				v.warn(e.Path, "device specification extended attribute truncated")
				break
			}
			e.DeviceMajor, e.DeviceMinor, e.HasDevice = le32(body[4:]), le32(body[8:]), true
		}
		e.ExtendedAttributes = append(e.ExtendedAttributes, ea)
	}
}

// maxEAFile bounds the bytes read from an extended attribute file.
const maxEAFile = 1 << 16

// readEAFile reads the extended attribute file an entry's EA ICB points to.
func (v *Volume) readEAFile(e *Entry, icb longAD) {
	part, err := v.partRef(icb.part)
	if err != nil {
		v.warn(e.Path, "extended attribute file: %v", err)
		return
	}
	fe, err := v.readFE(e.Path, addr{part, icb.block})
	if err != nil {
		v.warn(e.Path, "extended attribute file: %v", err)
		return
	}
	c, err := v.contentOf(e.Path, fe)
	if err != nil {
		v.warn(e.Path, "extended attribute file: %v", err)
		return
	}
	n := min(c.size, maxEAFile)
	if c.size > n {
		v.warn(e.Path, "extended attribute file of %d bytes truncated to %d", c.size, n)
	}
	b := make([]byte, n)
	if _, err := v.readContent(c, b, 0); err != nil && !errors.Is(err, io.EOF) {
		v.warn(e.Path, "extended attribute file: %v", err)
		return
	}
	loc, ok := c.blockAt(0, v.bs, fe.loc.block)
	if !ok {
		loc = fe.loc.block
	}
	v.parseEAs(e, b, loc)
}

// readSymlink decodes a symbolic link's path components (ECMA-167
// 4/14.16).
func (v *Volume) readSymlink(e *Entry) {
	b := make([]byte, min(e.data.size, 1<<16))
	if _, err := v.readContent(e.data, b, 0); err != nil && !errors.Is(err, io.EOF) {
		v.warn(e.Path, "symbolic link: %v", err)
		return
	}
	target, msgs := symlinkTarget(b)
	for _, m := range msgs {
		v.warn(e.Path, "symbolic link: %s", m)
	}
	e.SymlinkTarget, e.IsSymlink = target, true
}

// symlinkTarget joins path components (ECMA-167 4/14.16.1.1): types 1 (with
// no identifier) and 2 are the root, 3 the parent (".."), 4 the current
// directory (".") and 5 a named component.
func symlinkTarget(b []byte) (string, []string) {
	var parts, msgs []string
	abs := false
	for pos := 0; pos < len(b); {
		if allZero(b[pos:]) {
			break
		}
		if pos+4 > len(b) {
			msgs = append(msgs, "truncated path component")
			break
		}
		typ, l := b[pos], int(b[pos+1])
		if pos+4+l > len(b) {
			msgs = append(msgs, "truncated path component")
			break
		}
		id := b[pos+4 : pos+4+l]
		pos += 4 + l
		switch typ {
		case 1, 2:
			if typ == 1 && l > 0 {
				msgs = append(msgs, fmt.Sprintf("implementation-specific root %q treated as /", latin1(id)))
			}
			parts, abs = parts[:0], true
		case 3:
			parts = append(parts, "..")
		case 4:
			parts = append(parts, ".")
		case 5:
			name, msg := decodeCS0(id)
			if msg != "" {
				msgs = append(msgs, msg)
			}
			if name == "" {
				msgs = append(msgs, "empty path component")
			}
			parts = append(parts, name)
		default:
			msgs = append(msgs, fmt.Sprintf("path component of unknown type %d ignored", typ))
		}
	}
	s := strings.Join(parts, "/")
	if abs {
		s = "/" + s
	}
	return s, msgs
}

// loadStreams reads an entry's stream directory into e.Streams.
func (v *Volume) loadStreams(e *Entry, icb longAD) {
	if v.streamBudget <= 0 {
		v.warn(e.Path, "named streams not read: more than %d in the directory", streamBudget)
		return
	}
	sd := &Entry{Path: e.Path, IsDir: true, Characteristics: CharDirectory, vol: v}
	v.loadEntry(sd, icb, true)
	if sd.err != nil {
		v.warn(e.Path, "stream directory: %v", sd.err)
		return
	}
	if sd.FileType != FileTypeStreamDirectory {
		v.warn(e.Path, "stream directory has file type %d", sd.FileType)
	}
	ents, err := v.readDir(sd, e.Path)
	if err != nil {
		v.warn(e.Path, "stream directory: %v", err)
	}
	for _, s := range ents {
		s.Stream = true
		if s.err != nil {
			v.warn(s.Path, "%v", s.err)
		}
	}
	v.streamBudget -= max(len(ents), 1)
	e.Streams = ents
}
