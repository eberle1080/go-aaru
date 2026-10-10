// Package cdecc implements Compact Disc EDC/ECC handling for raw 2352-byte
// CD-ROM sectors: suffix (EDC + ECC P/Q) verification for Mode 1 and Mode 2,
// ECC generation, and reconstruction of a sector's prefix (sync + header) and
// suffix from its track type.
//
// The three constant lookup tables are package-level and built once at
// package initialisation, so no context is needed and every function is safe
// for concurrent use.
//
// Raw sector layout (offsets in hex):
//
//	000-00B sync (00 FF*10 00)   00C-00E BCD MSF address   00F mode
//	Mode 1:        010-80F user data, 810-813 EDC, 814-81B reserved (zero),
//	               81C-8C7 ECC P, 8C8-92F ECC Q
//	Mode 2 Form 1: 010-017 subheader (2 copies), 018-817 user data,
//	               818-81B EDC, 81C-8C7 ECC P, 8C8-92F ECC Q
//	Mode 2 Form 2: 010-017 subheader, 018-92B user data, 92C-92F EDC
package cdecc

import (
	"encoding/binary"
	"errors"
)

// SectorSize is the size of a raw CD sector.
const SectorSize = 2352

// TrackType is the AaruFormat track type; the values are identical to the root package's TrackType so callers can convert with
// cdecc.TrackType(t). It is defined locally to avoid an import cycle.
type TrackType uint8

// Track types (same numeric values as stored in AaruFormat images).
const (
	TrackTypeAudio           TrackType = 0
	TrackTypeData            TrackType = 1
	TrackTypeCdMode1         TrackType = 2
	TrackTypeCdMode2Formless TrackType = 3
	TrackTypeCdMode2Form1    TrackType = 4
	TrackTypeCdMode2Form2    TrackType = 5
)

var (
	// ErrShortSector is returned when a sector buffer is too short for the
	// requested operation.
	ErrShortSector = errors.New("cdecc: sector buffer too short")
	// ErrUnsupportedTrackType is returned when a reconstruction is requested
	// for a track type it does not apply to.
	ErrUnsupportedTrackType = errors.New("cdecc: unsupported track type")
	// ErrInvalidECCParams is returned by WriteECC when the parameters are
	// inconsistent or the buffers are too short for them.
	ErrInvalidECCParams = errors.New("cdecc: invalid ECC parameters or short buffer")
)

// ECCParams describes one Reed-Solomon product code pass (P or Q) over the
// sector.
type ECCParams struct {
	MajorCount int
	MinorCount int
	MajorMult  int
	MinorInc   int
}

var (
	// ECCP is the ECC P pass (86 columns of 24 bytes; 172 ECC bytes).
	ECCP = ECCParams{MajorCount: 86, MinorCount: 24, MajorMult: 2, MinorInc: 86}
	// ECCQ is the ECC Q pass (52 diagonals of 43 bytes; 104 ECC bytes).
	ECCQ = ECCParams{MajorCount: 52, MinorCount: 43, MajorMult: 86, MinorInc: 88}
)

const (
	headerOffset = 0x00C
	dataOffset   = 0x010
	eccPOffset   = 0x81C
	eccQOffset   = eccPOffset + 0xAC

	mode1EDCOffset = 0x810
	form1EDCOffset = 0x818
	form2EDCOffset = 0x92C
	mode1EDCSize   = 0x810 // bytes 0x000-0x80F
	form1EDCSize   = 0x808 // bytes 0x010-0x817
	form2EDCSize   = 0x91C // bytes 0x010-0x92B

	edcPoly = 0xD8018001
)

var (
	eccF     [256]byte
	eccB     [256]byte
	edcTable [8][256]uint32 // edcTable[0] is the byte-wise table; 1..7 are slicing-by-8 tables
)

func init() {
	for i := range 256 {
		j := uint32(i) << 1
		if i&0x80 != 0 {
			j ^= 0x11D
		}
		eccF[i] = byte(j)
		eccB[byte(i)^byte(j)] = byte(i)

		edc := uint32(i)
		for range 8 {
			if edc&1 != 0 {
				edc = edc>>1 ^ edcPoly
			} else {
				edc >>= 1
			}
		}
		edcTable[0][i] = edc
	}
	for i := range 256 {
		v := edcTable[0][i]
		for k := 1; k < 8; k++ {
			v = v>>8 ^ edcTable[0][v&0xFF]
			edcTable[k][i] = v
		}
	}
}

// ComputeEDC updates the CD EDC (a reflected CRC-32 with polynomial
// 0xD8018001, no inversion) edc with data and returns it. The EDC of a block is
// ComputeEDC(0, block).
func ComputeEDC(edc uint32, data []byte) uint32 {
	for len(data) >= 8 {
		edc ^= binary.LittleEndian.Uint32(data)
		edc = edcTable[0][data[7]] ^ edcTable[1][data[6]] ^ edcTable[2][data[5]] ^ edcTable[3][data[4]] ^
			edcTable[4][edc>>24] ^ edcTable[5][edc>>16&0xFF] ^ edcTable[6][edc>>8&0xFF] ^ edcTable[7][edc&0xFF]
		data = data[8:]
	}
	for _, b := range data {
		edc = edc>>8 ^ edcTable[0][byte(edc)^b]
	}
	return edc
}

// valid reports whether p is self-consistent and the buffers are long enough
// for it: indices produced by the walk stay below MajorCount*MinorCount, the
// first 4 come from address and the rest from data[idx-4].
func (p ECCParams) valid(address, data, ecc []byte) bool {
	const limit = 1 << 16
	if p.MajorCount <= 0 || p.MinorCount <= 0 || p.MajorMult < 0 || p.MinorInc < 0 ||
		p.MajorCount > limit || p.MinorCount > limit || p.MajorMult > limit {
		return false
	}
	size := p.MajorCount * p.MinorCount
	// Starting indices are (major>>1)*MajorMult + (major&1); the walk then
	// stays below size as long as every start does and MinorInc <= size.
	last := p.MajorCount - 1
	maxStart := (last >> 1) * p.MajorMult
	if last >= 1 {
		odd := last
		if odd&1 == 0 {
			odd--
		}
		maxStart = max(maxStart, (odd>>1)*p.MajorMult+1)
	}
	if p.MinorInc > size || maxStart >= size {
		return false
	}
	return len(address) >= min(4, size) && len(data) >= size-4 && len(ecc) >= 2*p.MajorCount
}

// computeECC computes the two ECC bytes of one major column/diagonal.
func (p ECCParams) computeECC(address, data []byte, major int) (a, b byte) {
	size := p.MajorCount * p.MinorCount
	idx := (major>>1)*p.MajorMult + major&1
	for range p.MinorCount {
		var temp byte
		if idx < 4 {
			temp = address[idx]
		} else {
			temp = data[idx-4]
		}
		idx += p.MinorInc
		if idx >= size {
			idx -= size
		}
		a ^= temp
		b ^= temp
		a = eccF[a]
	}
	a = eccB[eccF[a]^b]
	return a, a ^ b
}

// CheckECC verifies one ECC pass.
//
// address holds the 4 header bytes covered by the ECC (the sector header for
// Mode 1, four zero bytes for Mode 2), data holds the bytes that follow the
// header (i.e. sector[0x10:]) and ecc holds the ECC bytes of this pass
// (sector[0x81C:] for P, sector[0x8C8:] for Q). It returns false if the
// buffers are too short.
func CheckECC(address, data, ecc []byte, p ECCParams) bool {
	if !p.valid(address, data, ecc) {
		return false
	}
	for major := range p.MajorCount {
		a, b := p.computeECC(address, data, major)
		if ecc[major] != a || ecc[major+p.MajorCount] != b {
			return false
		}
	}
	return true
}

// WriteECC computes one ECC pass into ecc. The arguments
// are as for CheckECC; ecc may alias data (as it does within a sector).
func WriteECC(address, data, ecc []byte, p ECCParams) error {
	if !p.valid(address, data, ecc) {
		return ErrInvalidECCParams
	}
	for major := range p.MajorCount {
		a, b := p.computeECC(address, data, major)
		ecc[major] = a
		ecc[major+p.MajorCount] = b
	}
	return nil
}

// WriteSectorECC computes ECC P into ecc[0:172] and then ECC Q into
// ecc[172:276]. Typically address is the header
// (or four zero bytes), data is sector[0x10:] and ecc is sector[0x81C:].
func WriteSectorECC(address, data, ecc []byte) error {
	if len(ecc) < 0xAC {
		return ErrInvalidECCParams
	}
	if err := WriteECC(address, data, ecc, ECCP); err != nil {
		return err
	}
	return WriteECC(address, data, ecc[0xAC:], ECCQ)
}

// IsSuffixCorrect reports whether a Mode 1 sector's suffix is correct: the
// reserved bytes are zero, ECC P and Q match, and the EDC over bytes
// 0x000-0x80F matches the stored one. It
// returns false for buffers shorter than SectorSize.
func IsSuffixCorrect(sector []byte) bool {
	if len(sector) < SectorSize {
		return false
	}
	for _, b := range sector[0x814:0x81C] {
		if b != 0 {
			return false
		}
	}
	header := sector[headerOffset:dataOffset]
	data := sector[dataOffset:]
	if !CheckECC(header, data, sector[eccPOffset:], ECCP) || !CheckECC(header, data, sector[eccQOffset:], ECCQ) {
		return false
	}
	stored := binary.LittleEndian.Uint32(sector[mode1EDCOffset:])
	return ComputeEDC(0, sector[:mode1EDCSize]) == stored
}

var zeroAddress [4]byte

// IsSuffixCorrectMode2 reports whether a Mode 2 sector's suffix is correct:
// ECC P and Q (computed with a zero
// header) match and the EDC matches. If the Form 2 bit of the first subheader
// copy (byte 0x12, bit 5) is set, the Form 2 EDC (over 0x010-0x92B, stored at
// 0x92C) is checked, otherwise the Form 1 EDC (over 0x010-0x817, stored at
// 0x818).
//
// ECC P/Q are checked even for Form 2 sectors, which have
// no ECC (that area is user data), so a Form 2 sector practically never passes;
// for Form 2 compare ComputeEDC(0, sector[0x10:0x92C]) with the stored EDC
// instead, as the library's write path does. It returns false for buffers
// shorter than SectorSize.
func IsSuffixCorrectMode2(sector []byte) bool {
	if len(sector) < SectorSize {
		return false
	}
	data := sector[dataOffset:]
	if !CheckECC(zeroAddress[:], data, sector[eccPOffset:], ECCP) ||
		!CheckECC(zeroAddress[:], data, sector[eccQOffset:], ECCQ) {
		return false
	}
	var stored, computed uint32
	if sector[0x12]&0x20 != 0 {
		stored = binary.LittleEndian.Uint32(sector[form2EDCOffset:])
		computed = ComputeEDC(0, sector[dataOffset:dataOffset+form2EDCSize])
	} else {
		stored = binary.LittleEndian.Uint32(sector[form1EDCOffset:])
		computed = ComputeEDC(0, sector[dataOffset:dataOffset+form1EDCSize])
	}
	return computed == stored
}

// LBAToMSF converts a logical block address to the absolute minute, second
// and frame of a CD (LBA 0 is 00:02:00) using truncating arithmetic (values
// outside the representable range wrap).
func LBAToMSF(lba int64) (minute, second, frame uint8) {
	pos := lba + 150
	return uint8(pos / 75 / 60), uint8(pos / 75 % 60), uint8(pos % 75)
}

func toBCD(v uint8) uint8 { return (v/10)<<4 + v%10 }

// ReconstructPrefix writes the sync pattern, BCD MSF header for lba and mode
// byte into sector. For Mode 2 track types
// (formless, Form 1 and Form 2) it also copies the second subheader copy
// (0x014-0x017) over the first (0x010-0x013).
//
// sector must hold at least 16 bytes for Mode 1 and 24 bytes for Mode 2.
// Unsupported track types return ErrUnsupportedTrackType and leave sector
// untouched.
func ReconstructPrefix(sector []byte, t TrackType, lba int64) error {
	need := 0x10
	switch t {
	case TrackTypeCdMode1:
	case TrackTypeCdMode2Formless, TrackTypeCdMode2Form1, TrackTypeCdMode2Form2:
		need = 0x18
	default:
		return ErrUnsupportedTrackType
	}
	if len(sector) < need {
		return ErrShortSector
	}

	sector[0x000] = 0x00
	for i := 0x001; i <= 0x00A; i++ {
		sector[i] = 0xFF
	}
	sector[0x00B] = 0x00

	minute, second, frame := LBAToMSF(lba)
	sector[0x00C] = toBCD(minute)
	sector[0x00D] = toBCD(second)
	sector[0x00E] = toBCD(frame)

	if t == TrackTypeCdMode1 {
		sector[0x00F] = 0x01
		return nil
	}
	sector[0x00F] = 0x02
	copy(sector[0x010:0x014], sector[0x014:0x018])
	return nil
}

// Reconstruct regenerates the suffix of a raw sector from its contents:
//
//   - TrackTypeCdMode1: EDC at 0x810, zeroed reserved bytes, ECC P/Q over the
//     header and data.
//   - TrackTypeCdMode2Form1: EDC at 0x818 and ECC P/Q computed with a zero
//     header.
//   - TrackTypeCdMode2Form2: EDC at 0x92C only.
//
// Other track types return ErrUnsupportedTrackType without modifying sector.
// sector must be at least SectorSize bytes long.
func Reconstruct(sector []byte, t TrackType) error {
	switch t {
	case TrackTypeCdMode1, TrackTypeCdMode2Form1, TrackTypeCdMode2Form2:
	default:
		return ErrUnsupportedTrackType
	}
	if len(sector) < SectorSize {
		return ErrShortSector
	}

	switch t {
	case TrackTypeCdMode1:
		binary.LittleEndian.PutUint32(sector[mode1EDCOffset:], ComputeEDC(0, sector[:mode1EDCSize]))
		clear(sector[0x814:0x81C])
		return WriteSectorECC(sector[headerOffset:dataOffset], sector[dataOffset:], sector[eccPOffset:])
	case TrackTypeCdMode2Form1:
		binary.LittleEndian.PutUint32(sector[form1EDCOffset:],
			ComputeEDC(0, sector[dataOffset:dataOffset+form1EDCSize]))
		return WriteSectorECC(zeroAddress[:], sector[dataOffset:], sector[eccPOffset:])
	default: // TrackTypeCdMode2Form2
		binary.LittleEndian.PutUint32(sector[form2EDCOffset:],
			ComputeEDC(0, sector[dataOffset:dataOffset+form2EDCSize]))
		return nil
	}
}
