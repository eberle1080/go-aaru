package udf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	le16 = binary.LittleEndian.Uint16
	le32 = binary.LittleEndian.Uint32
	le64 = binary.LittleEndian.Uint64
)

// Descriptor tag identifiers (ECMA-167 3/7.2.1 and 4/7.2.1).
const (
	tagSparingTable = 0
	tagPVD          = 1
	tagAVDP         = 2
	tagVDP          = 3
	tagIUVD         = 4
	tagPD           = 5
	tagLVD          = 6
	tagUSD          = 7
	tagTD           = 8
	tagLVID         = 9
	tagFSD          = 256
	tagFID          = 257
	tagAED          = 258
	tagIE           = 259
	tagTE           = 260
	tagFE           = 261
	tagEAHD         = 262
	tagUSE          = 263
	tagEFE          = 266
)

// tag is a descriptor tag (ECMA-167 3/7.2).
type tag struct {
	id      uint16
	version uint16
	serial  uint16
	crc     uint16
	crcLen  uint16
	loc     uint32
}

func parseTag(b []byte) tag {
	if len(b) < 16 {
		return tag{id: 0xFFFF}
	}
	return tag{id: le16(b), version: le16(b[2:]), serial: le16(b[6:]), crc: le16(b[8:]),
		crcLen: le16(b[10:]), loc: le32(b[12:])}
}

// tagChecksum is the sum modulo 256 of the tag bytes other than byte 4.
func tagChecksum(b []byte) byte {
	var s byte
	for i := range 16 {
		if i != 4 {
			s += b[i]
		}
	}
	return s
}

// tagValid reports whether b starts with a tag with identifier id and a
// correct checksum (and is not all zeros), for probing.
func tagValid(b []byte, id uint16) bool {
	if len(b) < 16 || le16(b) != id || tagChecksum(b) != b[4] {
		return false
	}
	return !allZero(b[:16])
}

// crcTable is the CRC-ITU-T (CCITT, polynomial 0x1021, MSB first, initial
// value 0) table ECMA-167 7.2.6 specifies for descriptor CRCs.
var crcTable = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return
}()

func crcITU(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c = c<<8 ^ crcTable[byte(c>>8)^x]
	}
	return c
}

// checkTag verifies that b holds a descriptor with identifier id recorded at
// block loc. A wrong identifier is an error; a bad checksum, CRC or location
// is only a warning.
func (v *Volume) checkTag(path, what string, b []byte, id uint16, loc uint32) error {
	if len(b) < 16 {
		return fmt.Errorf("%w: %s at block %d is truncated", ErrCorrupt, what, loc)
	}
	t := parseTag(b)
	if t.id != id {
		return fmt.Errorf("%w: no %s at block %d (tag identifier %d)", ErrCorrupt, what, loc, t.id)
	}
	if tagChecksum(b) != b[4] {
		v.warn(path, "%s at block %d: tag checksum mismatch", what, loc)
	}
	switch {
	case int(t.crcLen) > len(b)-16:
		v.warn(path, "%s at block %d: CRC length %d exceeds the descriptor", what, loc, t.crcLen)
	case crcITU(b[16:16+int(t.crcLen)]) != t.crc:
		v.warn(path, "%s at block %d: descriptor CRC mismatch", what, loc)
	}
	if t.loc != loc {
		v.warn(path, "%s at block %d: tag location says block %d", what, loc, t.loc)
	}
	return nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// decodeCS0 decodes an OSTA Compressed Unicode string (compression ID byte
// first). It returns a message when the string had to be repaired.
func decodeCS0(b []byte) (string, string) {
	if len(b) == 0 {
		return "", ""
	}
	switch b[0] {
	case 8, 254: // 254 (UDF 2.50+): the same encoding as 8
		return latin1(b[1:]), ""
	case 16, 255:
		u := b[1:]
		var msgs []string
		if len(u)%2 == 1 {
			msgs = append(msgs, "16-bit identifier of odd length; last byte dropped")
			u = u[:len(u)-1]
		}
		s, bad := utf16BE(u)
		if bad {
			msgs = append(msgs, "invalid UTF-16 surrogates replaced with U+FFFD")
		}
		return s, strings.Join(msgs, "; ")
	}
	return latin1(b[1:]), fmt.Sprintf("unknown compression ID %d; decoded as 8-bit", b[0])
}

// latin1 decodes 8-bit characters (Unicode code points 0-255).
func latin1(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c < utf8.RuneSelf {
			sb.WriteByte(c)
		} else {
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// utf16BE decodes big-endian UTF-16; unpaired surrogates become U+FFFD and
// are reported.
func utf16BE(b []byte) (string, bool) {
	var sb strings.Builder
	bad := false
	for i := 0; i+1 < len(b); i += 2 {
		c := rune(b[i])<<8 | rune(b[i+1])
		switch {
		case c >= 0xD800 && c < 0xDC00:
			if i+3 < len(b) {
				d := rune(b[i+2])<<8 | rune(b[i+3])
				if d >= 0xDC00 && d < 0xE000 {
					sb.WriteRune(utf16.DecodeRune(c, d))
					i += 2
					continue
				}
			}
			sb.WriteRune(utf8.RuneError)
			bad = true
		case c >= 0xDC00 && c < 0xE000:
			sb.WriteRune(utf8.RuneError)
			bad = true
		default:
			sb.WriteRune(c)
		}
	}
	return sb.String(), bad
}

// dstring decodes a fixed-length dstring field: CS0 bytes whose used length
// is in the last byte.
func dstring(b []byte) (string, string) {
	if len(b) < 2 {
		return "", ""
	}
	n := int(b[len(b)-1])
	msg := ""
	if n == 0 {
		// Some writers leave the length at zero; salvage the content.
		body := b[:len(b)-1]
		end := len(body)
		for end > 0 && body[end-1] == 0 {
			end--
		}
		if end <= 1 || (body[0] != 8 && body[0] != 16) {
			return "", ""
		}
		n, msg = end, "dstring length byte is 0"
		if body[0] == 16 && n%2 == 0 {
			n++
		}
		n = min(n, len(body))
	}
	if n > len(b)-1 {
		msg = fmt.Sprintf("dstring length %d exceeds its %d-byte field", n, len(b)-1)
		n = len(b) - 1
	}
	s, m := decodeCS0(b[:n])
	return strings.TrimRight(s, " \x00"), joinMsg(msg, m)
}

func joinMsg(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

// regid is an entity identifier (ECMA-167 1/7.4).
type regid struct {
	flags  byte
	id     string
	suffix [8]byte
}

func parseRegid(b []byte) regid {
	if len(b) < 32 {
		return regid{}
	}
	r := regid{flags: b[0], id: strings.TrimRight(latin1(b[1:24]), " \x00")}
	copy(r.suffix[:], b[24:32])
	return r
}

// udfRevision returns the UDF revision of a domain identifier suffix.
func (r regid) udfRevision() uint16 { return le16(r.suffix[:]) }

// timestamp decodes a 12-byte ECMA-167 timestamp (1/7.3). It returns nil
// for an unset time (all zeros, or year 0) and a message when the field is
// invalid. An unspecified time zone (-2047) is UTC.
func timestamp(b []byte) (*time.Time, string) {
	if len(b) < 12 {
		return nil, "truncated timestamp"
	}
	year := int(int16(le16(b[2:])))
	if allZero(b[:12]) || year == 0 {
		return nil, ""
	}
	tz := le16(b)
	typ := tz >> 12
	off := int(tz & 0xFFF)
	if off&0x800 != 0 {
		off -= 0x1000
	}
	var msgs []string
	loc := time.UTC
	switch {
	case typ == 0: // UTC; the offset field does not apply
	case off == -2047: // unspecified
	case off < -1440 || off > 1440:
		msgs = append(msgs, fmt.Sprintf("time zone offset %d minutes out of range; using UTC", off))
	case off != 0:
		loc = time.FixedZone("", off*60)
	}
	if typ > 2 {
		msgs = append(msgs, fmt.Sprintf("timestamp type %d unknown", typ))
	}
	cs, hus, us := int(b[9]), int(b[10]), int(b[11])
	ns := 0
	if cs > 99 || hus > 99 || us > 99 {
		msgs = append(msgs, fmt.Sprintf("sub-second fields %d/%d/%d out of range; ignored", cs, hus, us))
	} else {
		ns = cs*10_000_000 + hus*100_000 + us*1_000
	}
	mo, d, h, mi, s := int(b[4]), int(b[5]), int(b[6]), int(b[7]), int(b[8])
	if year < 1 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59 {
		return nil, joinMsg(strings.Join(msgs, "; "),
			fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d is not a valid time", year, mo, d, h, mi, s))
	}
	t := time.Date(year, time.Month(mo), d, h, mi, s, ns, loc)
	if t.Day() != d {
		return nil, joinMsg(strings.Join(msgs, "; "), fmt.Sprintf("%04d-%02d-%02d is not a valid date", year, mo, d))
	}
	return &t, strings.Join(msgs, "; ")
}

// longAD is a long allocation descriptor (ECMA-167 4/14.14.2).
type longAD struct {
	length uint32 // with the extent type in the top two bits
	block  uint32
	part   uint16
	impl   [6]byte
}

func parseLongAD(b []byte) longAD {
	if len(b) < 16 {
		return longAD{}
	}
	l := longAD{length: le32(b), block: le32(b[4:]), part: le16(b[8:])}
	copy(l.impl[:], b[10:16])
	return l
}

func (l longAD) size() uint32 { return l.length & 0x3FFFFFFF }

// extentAD is an extent descriptor (ECMA-167 3/7.1).
type extentAD struct{ length, loc uint32 }

func parseExtentAD(b []byte) extentAD { return extentAD{length: le32(b), loc: le32(b[4:])} }
