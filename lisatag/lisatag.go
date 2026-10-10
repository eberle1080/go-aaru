// Package lisatag decodes, encodes and converts the per-block tags used by
// Apple Lisa storage devices: Sony 3.5" diskettes (12-byte tags), ProFile hard
// disks (20-byte tags) and Priam DataTower drives (24-byte tags).
//
// All multi-byte on-disk fields are big-endian. Sony tags have 11-bit chain
// block numbers where 0x7FF marks the end of a chain; ProFile and Priam tags
// have 24-bit block numbers where 0xFFFFFF has that meaning. The conversions
// translate between the two sentinels.
package lisatag

import (
	"errors"
	"fmt"
)

// On-disk tag sizes.
const (
	SonyTagSize    = 12
	ProfileTagSize = 20
	PriamTagSize   = 24
)

const (
	sonyBlockNumberMask  = 0x07FF
	sonyBlockValidFlag   = 0x8000
	sonyBlockAllowedMask = sonyBlockValidFlag | sonyBlockNumberMask
	sonyNoBlock          = sonyBlockNumberMask // 11-bit end-of-chain sentinel
	wideNoBlock          = 0xFFFFFF            // 24-bit end-of-chain sentinel
	mask24               = 0xFFFFFF
)

var (
	// ErrShortTag is returned when a buffer is too short for the tag being parsed.
	ErrShortTag = errors.New("lisatag: tag buffer too short")
	// ErrInvalidTagSize is returned by Convert for a tag whose length is not
	// 12, 20 or 24 bytes.
	ErrInvalidTagSize = errors.New("lisatag: invalid tag size")
	// ErrInvalidFormat is returned for an unknown Format value.
	ErrInvalidFormat = errors.New("lisatag: invalid tag format")
)

// Format identifies a tag layout.
type Format uint8

// Tag formats.
const (
	FormatSony    Format = iota + 1 // 12-byte Sony tag
	FormatProfile                   // 20-byte ProFile tag
	FormatPriam                     // 24-byte Priam DataTower tag
)

// Size returns the on-disk size of the format, or 0 for an invalid format.
func (f Format) Size() int {
	switch f {
	case FormatSony:
		return SonyTagSize
	case FormatProfile:
		return ProfileTagSize
	case FormatPriam:
		return PriamTagSize
	}
	return 0
}

// String returns the format name.
func (f Format) String() string {
	switch f {
	case FormatSony:
		return "Sony"
	case FormatProfile:
		return "Profile"
	case FormatPriam:
		return "Priam"
	}
	return fmt.Sprintf("Format(%d)", uint8(f))
}

// FormatForSize returns the tag format whose on-disk size is n.
func FormatForSize(n int) (Format, error) {
	switch n {
	case SonyTagSize:
		return FormatSony, nil
	case ProfileTagSize:
		return FormatProfile, nil
	case PriamTagSize:
		return FormatPriam, nil
	}
	return 0, fmt.Errorf("%w: %d", ErrInvalidTagSize, n)
}

// SonyTag is a 12-byte Sony diskette tag.
type SonyTag struct {
	Version   uint16 // 0x00: Lisa OS version number
	Kind      uint8  // 0x02 bits 7-6: kind of information in this block (2 bits)
	Reserved  uint8  // 0x02 bits 5-0: reserved (6 bits)
	Volume    uint8  // 0x03: disk volume number
	FileID    int16  // 0x04: file ID
	RelPage   uint16 // 0x06: relative page number
	NextBlock uint16 // 0x08: next block (11 bits, 0x7FF if last) | 0x8000 if block is valid
	PrevBlock uint16 // 0x0A: previous block (11 bits, 0x7FF if first) | 0x8000 valid flag
}

// ProfileTag is a 20-byte ProFile hard disk tag.
type ProfileTag struct {
	Version   uint16 // 0x00: Lisa OS version number
	Kind      uint8  // 0x02 bits 7-6: kind of information in this block (2 bits)
	Reserved  uint8  // 0x02 bits 5-0: reserved (6 bits)
	Volume    uint8  // 0x03: disk volume number
	FileID    int16  // 0x04: file ID
	ValidChk  bool   // 0x06 bit 7: checksum valid?
	UsedBytes uint16 // 0x06 bits 6-0 + 0x07: used bytes in block (15 bits)
	AbsPage   uint32 // 0x08: absolute page number (24 bits)
	Checksum  uint8  // 0x0B: checksum of data
	RelPage   uint16 // 0x0C: relative page number
	NextBlock uint32 // 0x0E: next block (24 bits), 0xFFFFFF if last
	PrevBlock uint32 // 0x11: previous block (24 bits), 0xFFFFFF if first
}

// PriamTag is a 24-byte Priam DataTower tag: a ProfileTag plus
// a disk size.
type PriamTag struct {
	Version   uint16 // 0x00: Lisa OS version number
	Kind      uint8  // 0x02 bits 7-6: kind of information in this block (2 bits)
	Reserved  uint8  // 0x02 bits 5-0: reserved (6 bits)
	Volume    uint8  // 0x03: disk volume number
	FileID    int16  // 0x04: file ID
	ValidChk  bool   // 0x06 bit 7: checksum valid?
	UsedBytes uint16 // 0x06 bits 6-0 + 0x07: used bytes in block (15 bits)
	AbsPage   uint32 // 0x08: absolute page number (24 bits)
	Checksum  uint8  // 0x0B: checksum of data
	RelPage   uint16 // 0x0C: relative page number
	NextBlock uint32 // 0x0E: next block (24 bits), 0xFFFFFF if last
	PrevBlock uint32 // 0x11: previous block (24 bits), 0xFFFFFF if first
	DiskSize  uint32 // 0x14: disk size
}

func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func be24(b []byte) uint32 { return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]) }
func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func put16(b []byte, v uint16) { b[0], b[1] = byte(v>>8), byte(v) }
func put24(b []byte, v uint32) { b[0], b[1], b[2] = byte(v>>16), byte(v>>8), byte(v) }
func put32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

// ParseSonyTag decodes a Sony tag from the first 12 bytes of b. The chain
// fields keep only the valid flag and the 11-bit block number.
func ParseSonyTag(b []byte) (SonyTag, error) {
	if len(b) < SonyTagSize {
		return SonyTag{}, fmt.Errorf("%w: %d bytes, need %d", ErrShortTag, len(b), SonyTagSize)
	}
	return SonyTag{
		Version:   be16(b[0:]),
		Kind:      b[2] >> 6,
		Reserved:  b[2] & 0x3F,
		Volume:    b[3],
		FileID:    int16(be16(b[4:])),
		RelPage:   be16(b[6:]),
		NextBlock: be16(b[8:]) & sonyBlockAllowedMask,
		PrevBlock: be16(b[10:]) & sonyBlockAllowedMask,
	}, nil
}

// ParseProfileTag decodes a ProFile tag from the first 20 bytes of b.
func ParseProfileTag(b []byte) (ProfileTag, error) {
	if len(b) < ProfileTagSize {
		return ProfileTag{}, fmt.Errorf("%w: %d bytes, need %d", ErrShortTag, len(b), ProfileTagSize)
	}
	return ProfileTag{
		Version:   be16(b[0:]),
		Kind:      b[2] >> 6,
		Reserved:  b[2] & 0x3F,
		Volume:    b[3],
		FileID:    int16(be16(b[4:])),
		ValidChk:  b[6]&0x80 != 0,
		UsedBytes: be16(b[6:]) & 0x7FFF,
		AbsPage:   be24(b[8:]),
		Checksum:  b[11],
		RelPage:   be16(b[12:]),
		NextBlock: be24(b[14:]),
		PrevBlock: be24(b[17:]),
	}, nil
}

// ParsePriamTag decodes a Priam tag from the first 24 bytes of b.
func ParsePriamTag(b []byte) (PriamTag, error) {
	if len(b) < PriamTagSize {
		return PriamTag{}, fmt.Errorf("%w: %d bytes, need %d", ErrShortTag, len(b), PriamTagSize)
	}
	p, _ := ParseProfileTag(b)
	t := p.ToPriam()
	t.DiskSize = be32(b[20:])
	return t, nil
}

// Bytes encodes the tag into its 12-byte on-disk form.
// Kind, Reserved and the chain fields are masked to their on-disk widths.
func (t SonyTag) Bytes() []byte {
	b := make([]byte, SonyTagSize)
	put16(b[0:], t.Version)
	b[2] = (t.Kind&0x3)<<6 | t.Reserved&0x3F
	b[3] = t.Volume
	put16(b[4:], uint16(t.FileID))
	put16(b[6:], t.RelPage)
	put16(b[8:], t.NextBlock&sonyBlockAllowedMask)
	put16(b[10:], t.PrevBlock&sonyBlockAllowedMask)
	return b
}

// putProfile writes the 20 bytes shared by the ProFile and Priam layouts.
func putProfile(b []byte, version uint16, kind, reserved, volume uint8, fileID int16, validChk bool,
	usedBytes uint16, absPage uint32, checksum uint8, relPage uint16, next, prev uint32,
) {
	put16(b[0:], version)
	b[2] = (kind&0x3)<<6 | reserved&0x3F
	b[3] = volume
	put16(b[4:], uint16(fileID))
	put16(b[6:], usedBytes&0x7FFF)
	if validChk {
		b[6] |= 0x80
	}
	put24(b[8:], absPage&mask24)
	b[11] = checksum
	put16(b[12:], relPage)
	put24(b[14:], next&mask24)
	put24(b[17:], prev&mask24)
}

// Bytes encodes the tag into its 20-byte on-disk form.
// Fields are masked to their on-disk widths.
func (t ProfileTag) Bytes() []byte {
	b := make([]byte, ProfileTagSize)
	putProfile(b, t.Version, t.Kind, t.Reserved, t.Volume, t.FileID, t.ValidChk, t.UsedBytes, t.AbsPage,
		t.Checksum, t.RelPage, t.NextBlock, t.PrevBlock)
	return b
}

// Bytes encodes the tag into its 24-byte on-disk form.
// Fields are masked to their on-disk widths.
func (t PriamTag) Bytes() []byte {
	b := make([]byte, PriamTagSize)
	putProfile(b, t.Version, t.Kind, t.Reserved, t.Volume, t.FileID, t.ValidChk, t.UsedBytes, t.AbsPage,
		t.Checksum, t.RelPage, t.NextBlock, t.PrevBlock)
	put32(b[20:], t.DiskSize)
	return b
}

// widenBlock maps an 11-bit Sony block number to 24 bits, translating the
// end-of-chain sentinel. The Sony valid flag is dropped.
func widenBlock(v uint16) uint32 {
	n := v & sonyBlockNumberMask
	if n == sonyNoBlock {
		return wideNoBlock
	}
	return uint32(n)
}

// narrowBlock maps a 24-bit block number to 11 bits, translating the
// end-of-chain sentinel and truncating other values.
func narrowBlock(v uint32) uint16 {
	n := v & mask24
	if n == wideNoBlock {
		return sonyNoBlock
	}
	return uint16(n & sonyBlockNumberMask)
}

// ToProfile converts the tag to a ProFile tag. The
// ProFile-only fields are zero; 0x7FF chain sentinels become 0xFFFFFF and the
// Sony valid flag is dropped.
func (t SonyTag) ToProfile() ProfileTag {
	return ProfileTag{
		Version:   t.Version,
		Kind:      t.Kind,
		Reserved:  t.Reserved,
		Volume:    t.Volume,
		FileID:    t.FileID,
		RelPage:   t.RelPage,
		NextBlock: widenBlock(t.NextBlock),
		PrevBlock: widenBlock(t.PrevBlock),
	}
}

// ToPriam converts the tag to a Priam tag. The
// Priam-only fields are zero; 0x7FF chain sentinels become 0xFFFFFF and the
// Sony valid flag is dropped.
func (t SonyTag) ToPriam() PriamTag {
	return PriamTag{
		Version:   t.Version,
		Kind:      t.Kind,
		Reserved:  t.Reserved,
		Volume:    t.Volume,
		FileID:    t.FileID,
		RelPage:   t.RelPage,
		NextBlock: widenBlock(t.NextBlock),
		PrevBlock: widenBlock(t.PrevBlock),
	}
}

// ToSony converts the tag to a Sony tag. Block numbers
// are narrowed to 11 bits (0xFFFFFF becomes 0x7FF); ProFile-only fields are
// dropped.
func (t ProfileTag) ToSony() SonyTag {
	return SonyTag{
		Version:   t.Version,
		Kind:      t.Kind,
		Reserved:  t.Reserved,
		Volume:    t.Volume,
		FileID:    t.FileID,
		RelPage:   t.RelPage,
		NextBlock: narrowBlock(t.NextBlock),
		PrevBlock: narrowBlock(t.PrevBlock),
	}
}

// ToPriam converts the tag to a Priam tag, copying all
// fields; DiskSize is zero.
func (t ProfileTag) ToPriam() PriamTag {
	return PriamTag{
		Version:   t.Version,
		Kind:      t.Kind,
		Reserved:  t.Reserved,
		Volume:    t.Volume,
		FileID:    t.FileID,
		ValidChk:  t.ValidChk,
		UsedBytes: t.UsedBytes,
		AbsPage:   t.AbsPage,
		Checksum:  t.Checksum,
		RelPage:   t.RelPage,
		NextBlock: t.NextBlock,
		PrevBlock: t.PrevBlock,
	}
}

// ToSony converts the tag to a Sony tag. Block numbers are
// narrowed to 11 bits (0xFFFFFF becomes 0x7FF); Priam-only fields are dropped.
func (t PriamTag) ToSony() SonyTag {
	return t.ToProfile().ToSony()
}

// ToProfile converts the tag to a ProFile tag, dropping
// DiskSize.
func (t PriamTag) ToProfile() ProfileTag {
	return ProfileTag{
		Version:   t.Version,
		Kind:      t.Kind,
		Reserved:  t.Reserved,
		Volume:    t.Volume,
		FileID:    t.FileID,
		ValidChk:  t.ValidChk,
		UsedBytes: t.UsedBytes,
		AbsPage:   t.AbsPage,
		Checksum:  t.Checksum,
		RelPage:   t.RelPage,
		NextBlock: t.NextBlock,
		PrevBlock: t.PrevBlock,
	}
}

// Convert re-encodes a raw tag into the format to, as the writer does before
// storing Lisa-family tags. The source format is inferred from len(tag) (12,
// 20 or 24 bytes). If source and target formats match, a verbatim copy is
// returned (the tag is not decoded); otherwise the tag is decoded, converted
// and encoded.
func Convert(tag []byte, to Format) ([]byte, error) {
	if to.Size() == 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidFormat, uint8(to))
	}
	from, err := FormatForSize(len(tag))
	if err != nil {
		return nil, err
	}
	if from == to {
		out := make([]byte, len(tag))
		copy(out, tag)
		return out, nil
	}

	switch from {
	case FormatSony:
		t, _ := ParseSonyTag(tag)
		if to == FormatProfile {
			return t.ToProfile().Bytes(), nil
		}
		return t.ToPriam().Bytes(), nil
	case FormatProfile:
		t, _ := ParseProfileTag(tag)
		if to == FormatSony {
			return t.ToSony().Bytes(), nil
		}
		return t.ToPriam().Bytes(), nil
	default: // FormatPriam
		t, _ := ParsePriamTag(tag)
		if to == FormatSony {
			return t.ToSony().Bytes(), nil
		}
		return t.ToProfile().Bytes(), nil
	}
}
