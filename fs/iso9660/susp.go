package iso9660

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// XA attribute bits (XAInfo.Attributes).
const (
	XAOwnerRead    = 0x0001
	XAOwnerExecute = 0x0004
	XAGroupRead    = 0x0010
	XAGroupExecute = 0x0040
	XAWorldRead    = 0x0100
	XAWorldExecute = 0x0400
	XAForm1        = 0x0800 // file sectors are Mode 2 Form 1
	XAForm2        = 0x1000 // file sectors are Mode 2 Form 2 (2324-byte user data)
	XAInterleaved  = 0x2000
	XACDDA         = 0x4000 // file is CD-DA audio
	XADirectory    = 0x8000
)

// XAInfo is the CD-ROM XA system use field of a directory record.
type XAInfo struct {
	GroupID    uint16
	UserID     uint16
	Attributes uint16
	FileNumber byte
}

// Extension is a SUSP extension reference (ER entry).
type Extension struct {
	ID         string // e.g. "RRIP_1991A", "IEEE_P1282", "IEEE_1282"
	Descriptor string
	Source     string
	Version    byte
}

// SUSPEntry is a raw System Use Sharing Protocol entry this package does not
// interpret.
type SUSPEntry struct {
	Signature string
	Version   byte
	Data      []byte // the entry's bytes after its 4-byte header
}

// AppleInfo is the Apple ISO 9660 extension ("AA" version 2): Finder type,
// creator and flags.
type AppleInfo struct {
	Type        [4]byte
	Creator     [4]byte
	FinderFlags uint16
}

// RRTimes holds the Rock Ridge TF timestamps; nil means absent.
type RRTimes struct {
	Creation   *time.Time
	Modify     *time.Time
	Access     *time.Time
	Attributes *time.Time // attribute change (ctime)
	Backup     *time.Time
	Expiration *time.Time
	Effective  *time.Time
}

// RRInfo holds the Rock Ridge attributes of an entry. Each Has* flag tells
// whether the corresponding entry was recorded.
type RRInfo struct {
	// PX: POSIX file attributes.
	HasPX bool
	// Mode is PosixMode converted to an fs.FileMode, including the type bits
	// (directory, symlink, device, char device, named pipe, socket) and
	// setuid/setgid/sticky.
	Mode      fs.FileMode
	PosixMode uint32
	Nlink     uint32
	UID       uint32
	GID       uint32
	// Inode is the file serial number (RRIP 1.12 PX only).
	HasInode bool
	Inode    uint32

	// PN: device number. Per RRIP, a zero DeviceHigh means DeviceLow holds
	// the whole (old-style) dev_t.
	HasDevice  bool
	DeviceHigh uint32
	DeviceLow  uint32

	// SL: symbolic link target, components joined with "/".
	HasSymlink    bool
	SymlinkTarget string

	// NM: alternate name. Name is empty when NM was absent or unusable (the
	// ISO 9660 name is used then).
	Name      string
	NameFlags byte

	Times RRTimes

	// CL/PL/RE: directory relocation.
	HasChildLink  bool
	ChildLink     uint32
	HasParentLink bool
	ParentLink    uint32
	Relocated     bool // RE: this is a relocated directory

	// SF: sparse file (recorded only; contents are not expanded).
	HasSparse    bool
	SparseSize   uint64
	SparseDepth  byte
	HasZF        bool // ZF: zisofs-compressed (not decompressed)
	ZFAlgorithm  string
	ZFHeaderSize byte // in 4-byte units
	ZFBlockLog2  byte
	ZFUncompSize uint32
	RRFlags      byte // the RRIP 1.09 "RR" entry's flags, if present
	HasRREntry   bool
}

// POSIX file type bits.
const (
	sIFMT   = 0o170000
	sIFSOCK = 0o140000
	sIFLNK  = 0o120000
	sIFREG  = 0o100000
	sIFBLK  = 0o060000
	sIFDIR  = 0o040000
	sIFCHR  = 0o020000
	sIFIFO  = 0o010000
)

// posixToFileMode converts a POSIX st_mode to an fs.FileMode.
func posixToFileMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & sIFMT {
	case sIFSOCK:
		mode |= fs.ModeSocket
	case sIFLNK:
		mode |= fs.ModeSymlink
	case sIFBLK:
		mode |= fs.ModeDevice
	case sIFDIR:
		mode |= fs.ModeDir
	case sIFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case sIFIFO:
		mode |= fs.ModeNamedPipe
	case sIFREG, 0:
	default:
		mode |= fs.ModeIrregular
	}
	if m&0o4000 != 0 {
		mode |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		mode |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}

// rripIDs are the ER identifiers of Rock Ridge.
var rripIDs = map[string]bool{"RRIP_1991A": true, "IEEE_P1282": true, "IEEE_1282": true}

// parseXA detects the 14-byte CD-ROM XA system use field.
func parseXA(su []byte) *XAInfo {
	if len(su) < 14 || su[6] != 'X' || su[7] != 'A' {
		return nil
	}
	return &XAInfo{
		GroupID:    binary.BigEndian.Uint16(su[0:]),
		UserID:     binary.BigEndian.Uint16(su[2:]),
		Attributes: binary.BigEndian.Uint16(su[4:]),
		FileNumber: su[8],
	}
}

// suspResult collects what the SUSP entries of one directory record say.
type suspResult struct {
	sp      bool
	spSkip  int
	ext     []Extension
	rr      *RRInfo
	apple   *AppleInfo
	other   []SUSPEntry
	nm      []byte
	nmBad   bool
	sl      slBuilder
	warns   []string
	sawRRIP bool // any Rock Ridge entry
}

func (r *suspResult) rri() *RRInfo {
	if r.rr == nil {
		r.rr = &RRInfo{}
	}
	r.sawRRIP = true
	return r.rr
}

func (r *suspResult) warn(format string, args ...any) {
	if len(r.warns) < 32 {
		r.warns = append(r.warns, fmt.Sprintf(format, args...))
	}
}

// slBuilder assembles a symbolic link target from SL components.
type slBuilder struct {
	b         strings.Builder
	started   bool
	continue_ bool // the previous component continues into the next
}

func (s *slBuilder) add(flags byte, content []byte) {
	switch {
	case flags&0x08 != 0 || flags&0x10 != 0: // root (or obsolete volume root)
		if !s.started {
			s.b.WriteByte('/')
		} else if !strings.HasSuffix(s.b.String(), "/") {
			s.b.WriteByte('/')
		}
		s.started, s.continue_ = true, false
		return
	}
	if s.started && !s.continue_ && !strings.HasSuffix(s.b.String(), "/") {
		s.b.WriteByte('/')
	}
	switch {
	case flags&0x02 != 0:
		s.b.WriteString(".")
	case flags&0x04 != 0:
		s.b.WriteString("..")
	default: // a name (or a host name, flag 0x20, kept as text)
		s.b.WriteString(decodeBytes(content))
	}
	s.started = true
	s.continue_ = flags&0x01 != 0
}

// ceRef is a pending continuation area.
type ceRef struct{ block, offset, length uint32 }

// parseSUSP parses the system use field su of a record (XA and SP skip
// bytes already removed), following CE continuation areas.
func (v *Volume) parseSUSP(t *tree, su []byte) *suspResult {
	res := &suspResult{}
	area := su
	seen := map[int64]bool{}
	for hops := 0; ; hops++ {
		ce := v.parseArea(t, area, res)
		if ce == nil {
			break
		}
		if hops >= v.opts.MaxContinuations {
			res.warn("more than %d SUSP continuation areas; ignoring the rest", v.opts.MaxContinuations)
			break
		}
		off := int64(ce.block)*t.blockSize + int64(ce.offset)
		if seen[off] {
			res.warn("SUSP continuation area loop at block %d offset %d", ce.block, ce.offset)
			break
		}
		seen[off] = true
		if ce.length == 0 {
			break
		}
		// A continuation area lies within one logical block (Linux rejects
		// anything larger), which also bounds the work per record.
		if int64(ce.length) > t.blockSize {
			res.warn("SUSP continuation area of %d bytes exceeds a logical block; ignored", ce.length)
			break
		}
		buf := make([]byte, ce.length)
		if err := v.readAt(buf, off); err != nil {
			res.warn("SUSP continuation area at block %d: %v", ce.block, err)
			break
		}
		area = buf
	}
	if res.rr != nil {
		if len(res.nm) > 0 && !res.nmBad {
			name := decodeBytes(res.nm)
			if strings.ContainsAny(name, "/\x00") {
				res.warn("Rock Ridge name %q contains '/' or NUL; using the ISO 9660 name", name)
			} else {
				res.rr.Name = name
			}
		}
		if res.sl.started {
			res.rr.HasSymlink = true
			res.rr.SymlinkTarget = res.sl.b.String()
		}
	}
	return res
}

// parseArea parses one system use area or continuation area, returning the
// CE entry found in it, if any.
func (v *Volume) parseArea(t *tree, a []byte, res *suspResult) *ceRef {
	var ce *ceRef
	for p := 0; p+4 <= len(a); {
		sig := string(a[p : p+2])
		l := int(a[p+2])
		ver := a[p+3]
		if l < 4 || l > len(a)-p {
			if !allZero(a[p:]) {
				res.warn("malformed SUSP entry %q (length %d, %d bytes left); ignoring the rest", sig, l, len(a)-p)
			}
			break
		}
		d := a[p+4 : p+l]
		p += l
		short := func(n int) bool {
			if len(d) < n {
				res.warn("SUSP %s entry of %d bytes is too short", sig, l)
				return true
			}
			return false
		}
		switch sig {
		case "SP":
			if !short(3) && d[0] == 0xBE && d[1] == 0xEF {
				res.sp, res.spSkip = true, int(d[2])
			}
		case "CE":
			if short(24) {
				continue
			}
			if ce != nil {
				res.warn("more than one CE entry in a system use area")
				continue
			}
			ce = &ceRef{block: binary.LittleEndian.Uint32(d[0:]), offset: binary.LittleEndian.Uint32(d[8:]),
				length: binary.LittleEndian.Uint32(d[16:])}
		case "ST":
			return ce
		case "PD":
		case "ER":
			if short(4) {
				continue
			}
			li, ld, ls := int(d[0]), int(d[1]), int(d[2])
			if 4+li+ld+ls > len(d) {
				res.warn("SUSP ER entry lengths overrun the entry")
				continue
			}
			s := d[4:]
			res.ext = append(res.ext, Extension{ID: decodeBytes(s[:li]), Descriptor: decodeBytes(s[li : li+ld]),
				Source: decodeBytes(s[li+ld : li+ld+ls]), Version: d[3]})
		case "RR":
			if !short(1) {
				r := res.rri()
				r.HasRREntry, r.RRFlags = true, d[0]
			}
		case "PX":
			if short(32) {
				continue
			}
			r := res.rri()
			r.HasPX = true
			r.PosixMode = binary.LittleEndian.Uint32(d[0:])
			r.Mode = posixToFileMode(r.PosixMode)
			r.Nlink = binary.LittleEndian.Uint32(d[8:])
			r.UID = binary.LittleEndian.Uint32(d[16:])
			r.GID = binary.LittleEndian.Uint32(d[24:])
			if len(d) >= 40 {
				r.HasInode, r.Inode = true, binary.LittleEndian.Uint32(d[32:])
			}
		case "PN":
			if short(16) {
				continue
			}
			r := res.rri()
			r.HasDevice = true
			r.DeviceHigh = binary.LittleEndian.Uint32(d[0:])
			r.DeviceLow = binary.LittleEndian.Uint32(d[8:])
		case "SL":
			if short(1) {
				continue
			}
			res.rri()
			c := d[1:]
			for len(c) >= 2 {
				fl, cl := c[0], int(c[1])
				if 2+cl > len(c) {
					res.warn("SUSP SL component overruns its entry")
					break
				}
				res.sl.add(fl, c[2:2+cl])
				c = c[2+cl:]
			}
		case "NM":
			if short(1) {
				continue
			}
			r := res.rri()
			r.NameFlags |= d[0]
			if d[0]&0x06 != 0 { // CURRENT or PARENT: not a usable name
				res.nmBad = true
			}
			if len(res.nm)+len(d)-1 > 4096 {
				res.warn("Rock Ridge name longer than 4096 bytes; truncated")
				continue
			}
			res.nm = append(res.nm, d[1:]...)
		case "CL":
			if !short(8) {
				r := res.rri()
				r.HasChildLink, r.ChildLink = true, binary.LittleEndian.Uint32(d)
			}
		case "PL":
			if !short(8) {
				r := res.rri()
				r.HasParentLink, r.ParentLink = true, binary.LittleEndian.Uint32(d)
			}
		case "RE":
			res.rri().Relocated = true
		case "TF":
			if short(1) {
				continue
			}
			r := res.rri()
			fl := d[0]
			size := 7
			if fl&0x80 != 0 {
				size = 17
			}
			dst := []**time.Time{&r.Times.Creation, &r.Times.Modify, &r.Times.Access, &r.Times.Attributes,
				&r.Times.Backup, &r.Times.Expiration, &r.Times.Effective}
			rest := d[1:]
			for i, ptr := range dst {
				if fl&(1<<i) == 0 {
					continue
				}
				if len(rest) < size {
					res.warn("SUSP TF entry is too short for its flags %#x", fl)
					break
				}
				var tm *time.Time
				var msg string
				if size == 7 {
					tm, msg = shortDateTime(rest[:7])
				} else {
					tm, msg = decDateTime(rest[:17])
				}
				if msg != "" {
					res.warn("Rock Ridge timestamp %d %s", i, msg)
				}
				*ptr = tm
				rest = rest[size:]
			}
		case "SF":
			if short(16) {
				continue
			}
			r := res.rri()
			r.HasSparse = true
			r.SparseSize = uint64(binary.LittleEndian.Uint32(d[0:]))<<32 | uint64(binary.LittleEndian.Uint32(d[8:]))
			if len(d) >= 17 {
				r.SparseDepth = d[16]
			}
		case "ZF":
			if short(12) {
				continue
			}
			r := res.rri()
			r.HasZF = true
			r.ZFAlgorithm = string(d[0:2])
			r.ZFHeaderSize, r.ZFBlockLog2 = d[2], d[3]
			r.ZFUncompSize = binary.LittleEndian.Uint32(d[4:])
		case "AA":
			if ver == 2 && len(d) >= 10 {
				a := &AppleInfo{FinderFlags: binary.BigEndian.Uint16(d[8:])}
				copy(a.Type[:], d[0:4])
				copy(a.Creator[:], d[4:8])
				res.apple = a
				continue
			}
			fallthrough
		default:
			if len(res.other) < 64 {
				res.other = append(res.other, SUSPEntry{Signature: sig, Version: ver, Data: bytes.Clone(d)})
			}
		}
	}
	return ce
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
