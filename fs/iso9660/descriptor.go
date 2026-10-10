package iso9660

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Volume descriptor types.
const (
	DescriptorBoot          = 0
	DescriptorPrimary       = 1
	DescriptorSupplementary = 2
	DescriptorPartition     = 3
	DescriptorTerminator    = 255
)

// Descriptor is one entry of the volume descriptor set.
type Descriptor struct {
	Type    byte
	Version byte
	// Sector is the descriptor's absolute 2048-byte sector.
	Sector uint32
}

// BootRecord is a boot record volume descriptor (type 0).
type BootRecord struct {
	Sector   uint32
	SystemID string // boot system identifier, e.g. "EL TORITO SPECIFICATION"
	BootID   string
	// ElTorito is set when SystemID names the El Torito specification;
	// CatalogLBA is then the boot catalog's sector.
	ElTorito   bool
	CatalogLBA uint32
	// SystemUse is the raw boot system use field (bytes 71-2047).
	SystemUse []byte
}

// VolumeInfo is the content of a primary, supplementary or enhanced volume
// descriptor. Identifiers have trailing spaces and NULs removed; Joliet
// identifiers are decoded from UCS-2. Dates that are unset (all '0' digits
// or all NULs) or unparseable are nil.
type VolumeInfo struct {
	Type    byte // DescriptorPrimary or DescriptorSupplementary
	Version byte // 1; 2 for an ISO 9660:1999 enhanced volume descriptor
	Sector  uint32
	// Flags is the SVD volume flags byte (bit 0: escape sequences not
	// registered per ISO 2375); 0 for a PVD.
	Flags byte
	// EscapeSequences is the SVD escape sequences field with trailing NULs
	// removed.
	EscapeSequences []byte
	// JolietLevel is 1, 2 or 3 for a Joliet SVD, else 0.
	JolietLevel int

	SystemID            string
	VolumeID            string
	VolumeSetID         string
	PublisherID         string
	DataPreparerID      string
	ApplicationID       string
	CopyrightFileID     string
	AbstractFileID      string
	BibliographicFileID string

	VolumeSpaceSize      uint32 // in logical blocks
	VolumeSetSize        uint16
	VolumeSequenceNumber uint16
	LogicalBlockSize     uint16

	PathTableSize             uint32
	LPathTable, OptLPathTable uint32
	MPathTable, OptMPathTable uint32

	// RootLBA and RootSize locate the root directory extent.
	RootLBA  uint32
	RootSize uint32

	Creation, Modification, Expiration, Effective *time.Time

	FileStructureVersion byte
	// ApplicationUse is the raw 512-byte application use field.
	ApplicationUse []byte

	rootRecord [34]byte
}

// readDescriptors reads the volume descriptor set and picks the Joliet SVD
// with the highest level.
func (v *Volume) readDescriptors() error {
	if err := v.readDescriptorSet(); err != nil {
		return err
	}
	for i := range v.supp {
		s := &v.supp[i]
		if s.JolietLevel > 0 && (v.joliet == nil || s.JolietLevel > v.joliet.JolietLevel) {
			v.joliet = s
		}
	}
	return nil
}

func (v *Volume) readDescriptorSet() error {
	buf := make([]byte, SectorSize)
	first := uint64(v.opts.BaseLBA) + 16
	for i := 0; i < v.opts.MaxDescriptors; i++ {
		sector := first + uint64(i)
		if sector > 0xFFFFFFFF {
			break
		}
		if err := v.readAt(buf, int64(sector)*SectorSize); err != nil {
			if i == 0 {
				return fmt.Errorf("%w: %w", ErrNotISO9660, err)
			}
			v.warn("", "volume descriptor set ends without a terminator: sector %d: %v", sector, err)
			return nil
		}
		if string(buf[1:6]) != "CD001" {
			if i == 0 {
				if string(buf[9:14]) == "CDROM" {
					return fmt.Errorf("%w: High Sierra volumes are not supported", ErrNotISO9660)
				}
				return ErrNotISO9660
			}
			v.warn("", "volume descriptor set ends without a terminator at sector %d", sector)
			return nil
		}
		d := Descriptor{Type: buf[0], Version: buf[6], Sector: uint32(sector)}
		v.descriptors = append(v.descriptors, d)
		switch d.Type {
		case DescriptorTerminator:
			return nil
		case DescriptorBoot:
			v.boot = append(v.boot, parseBootRecord(buf, d.Sector))
		case DescriptorPrimary:
			vi := v.parseVolumeInfo(buf, d)
			if v.primary != nil {
				v.warn("", "extra primary volume descriptor at sector %d ignored", sector)
				continue
			}
			v.primary = &vi
		case DescriptorSupplementary:
			vi := v.parseVolumeInfo(buf, d)
			v.supp = append(v.supp, vi)
		case DescriptorPartition:
			// Presence only (Descriptors lists it).
		default:
			v.warn("", "unknown volume descriptor type %d at sector %d", d.Type, sector)
		}
	}
	v.warn("", "volume descriptor set has no terminator within %d descriptors", v.opts.MaxDescriptors)
	return nil
}

func parseBootRecord(b []byte, sector uint32) BootRecord {
	br := BootRecord{
		Sector:    sector,
		SystemID:  decodeA(b[7:39]),
		BootID:    decodeA(b[39:71]),
		SystemUse: append([]byte(nil), b[71:]...),
	}
	if strings.HasPrefix(br.SystemID, "EL TORITO SPECIFICATION") {
		br.ElTorito = true
		br.CatalogLBA = binary.LittleEndian.Uint32(b[71:])
	}
	return br
}

// jolietLevel returns the Joliet level announced by escape sequences.
func jolietLevel(esc []byte) int {
	switch {
	case bytes.Contains(esc, []byte("%/E")):
		return 3
	case bytes.Contains(esc, []byte("%/C")):
		return 2
	case bytes.Contains(esc, []byte("%/@")):
		return 1
	}
	return 0
}

// parseVolumeInfo decodes a PVD/SVD (b is one 2048-byte sector).
func (v *Volume) parseVolumeInfo(b []byte, d Descriptor) VolumeInfo {
	vi := VolumeInfo{Type: d.Type, Version: d.Version, Sector: d.Sector}
	what := "primary volume descriptor"
	if d.Type == DescriptorSupplementary {
		vi.Flags = b[7]
		vi.EscapeSequences = append([]byte(nil), bytes.TrimRight(b[88:120], "\x00")...)
		vi.JolietLevel = jolietLevel(b[88:120])
		what = fmt.Sprintf("supplementary volume descriptor at sector %d", d.Sector)
	}
	str := decodeA
	if vi.JolietLevel > 0 {
		str = decodeUCS2
	}
	vi.SystemID = str(b[8:40])
	vi.VolumeID = str(b[40:72])
	vi.VolumeSetID = str(b[190:318])
	vi.PublisherID = str(b[318:446])
	vi.DataPreparerID = str(b[446:574])
	vi.ApplicationID = str(b[574:702])
	vi.CopyrightFileID = str(b[702:739])
	vi.AbstractFileID = str(b[739:776])
	vi.BibliographicFileID = str(b[776:813])

	both32 := func(off int, name string) uint32 {
		le, be := binary.LittleEndian.Uint32(b[off:]), binary.BigEndian.Uint32(b[off+4:])
		if le != be {
			v.warn("", "%s: %s little-endian %d and big-endian %d differ; using %d", what, name, le, be, le)
		}
		return le
	}
	both16 := func(off int, name string) uint16 {
		le, be := binary.LittleEndian.Uint16(b[off:]), binary.BigEndian.Uint16(b[off+2:])
		if le != be {
			v.warn("", "%s: %s little-endian %d and big-endian %d differ; using %d", what, name, le, be, le)
		}
		return le
	}
	vi.VolumeSpaceSize = both32(80, "volume space size")
	vi.VolumeSetSize = both16(120, "volume set size")
	vi.VolumeSequenceNumber = both16(124, "volume sequence number")
	vi.LogicalBlockSize = both16(128, "logical block size")
	vi.PathTableSize = both32(132, "path table size")
	vi.LPathTable = binary.LittleEndian.Uint32(b[140:])
	vi.OptLPathTable = binary.LittleEndian.Uint32(b[144:])
	vi.MPathTable = binary.BigEndian.Uint32(b[148:])
	vi.OptMPathTable = binary.BigEndian.Uint32(b[152:])
	root := b[156:190]
	copy(vi.rootRecord[:], root)
	vi.RootLBA = binary.LittleEndian.Uint32(root[2:])
	vi.RootSize = binary.LittleEndian.Uint32(root[10:])

	date := func(off int, name string) *time.Time {
		t, msg := decDateTime(b[off : off+17])
		if msg != "" {
			v.warn("", "%s: %s %s", what, name, msg)
		}
		return t
	}
	vi.Creation = date(813, "creation date")
	vi.Modification = date(830, "modification date")
	vi.Expiration = date(847, "expiration date")
	vi.Effective = date(864, "effective date")
	vi.FileStructureVersion = b[881]
	vi.ApplicationUse = append([]byte(nil), b[883:1395]...)
	if v.size != 0 && vi.LogicalBlockSize != 0 &&
		int64(vi.VolumeSpaceSize)*int64(vi.LogicalBlockSize) > v.size {
		v.warn("", "%s: volume space of %d blocks extends past the end of the source (%d bytes)",
			what, vi.VolumeSpaceSize, v.size)
	}
	return vi
}

// trimID removes the trailing spaces and NULs identifiers are padded with.
func trimID(s string) string { return strings.TrimRight(s, " \x00") }

// decodeBytes decodes an 8-bit identifier: UTF-8 when valid, else Latin-1.
func decodeBytes(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// decodeA decodes and trims an 8-bit descriptor identifier.
func decodeA(b []byte) string { return trimID(decodeBytes(b)) }

// ucs2 decodes big-endian UCS-2/UTF-16 (an odd trailing byte is dropped;
// unpaired surrogates become U+FFFD).
func ucs2(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// decodeUCS2 decodes and trims a Joliet descriptor identifier.
func decodeUCS2(b []byte) string { return trimID(ucs2(b)) }

// tzFor returns the zone for an offset in 15-minute units.
func tzFor(off int8) *time.Location {
	if off == 0 {
		return time.UTC
	}
	return time.FixedZone("", int(off)*15*60)
}

// validDate builds a time and reports whether its fields were in range
// (time.Date normalizes out-of-range fields).
func validDate(y, mo, d, h, mi, s, ns int, loc *time.Location) (time.Time, bool) {
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(mo), d, h, mi, s, ns, loc)
	if t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// decDateTime decodes a 17-byte dec-datetime. It returns nil for an unset
// date and a message when the field is unparseable or its offset is out of
// range.
func decDateTime(b []byte) (*time.Time, string) {
	if len(b) < 17 {
		return nil, "truncated"
	}
	unset := true
	for _, c := range b[:16] {
		if c != '0' && c != 0 && c != ' ' {
			unset = false
			break
		}
	}
	if unset {
		return nil, ""
	}
	var f [7]int
	widths := [7]int{4, 2, 2, 2, 2, 2, 2}
	p := 0
	for i, w := range widths {
		for _, c := range b[p : p+w] {
			if c < '0' || c > '9' {
				return nil, fmt.Sprintf("%q is not a date", b[:16])
			}
			f[i] = f[i]*10 + int(c-'0')
		}
		p += w
	}
	off := int8(b[16])
	msg := ""
	if off < -48 || off > 52 {
		msg = fmt.Sprintf("has GMT offset %d out of range; using UTC", off)
		off = 0
	}
	if f[0] == 0 {
		return nil, fmt.Sprintf("%q has year 0", b[:16])
	}
	t, ok := validDate(f[0], f[1], f[2], f[3], f[4], f[5], f[6]*10_000_000, tzFor(off))
	if !ok {
		return nil, fmt.Sprintf("%q is not a valid date", b[:16])
	}
	return &t, msg
}

// shortDateTime decodes a 7-byte directory record date. All-zero is unset.
func shortDateTime(b []byte) (*time.Time, string) {
	if len(b) < 7 {
		return nil, "truncated"
	}
	if bytes.Equal(b[:7], make([]byte, 7)) {
		return nil, ""
	}
	off := int8(b[6])
	msg := ""
	if off < -48 || off > 52 {
		msg = fmt.Sprintf("has GMT offset %d out of range; using UTC", off)
		off = 0
	}
	t, ok := validDate(1900+int(b[0]), int(b[1]), int(b[2]), int(b[3]), int(b[4]), int(b[5]), 0, tzFor(off))
	if !ok {
		return nil, fmt.Sprintf("% x is not a valid date", b[:6])
	}
	return &t, msg
}
