package ps3

import (
	"encoding/binary"
	"errors"
)

// MaxPlaintextRegions is the maximum number of plaintext regions accepted in
// an encryption map.
const MaxPlaintextRegions = 64

// Errors returned by the encryption map functions.
var (
	// ErrTruncated means the buffer is too short for the declared region count.
	ErrTruncated = errors.New("ps3: encryption map truncated")
	// ErrTooManyRegions means the region count exceeds MaxPlaintextRegions.
	ErrTooManyRegions = errors.New("ps3: too many plaintext regions")
	// ErrInvalidRegion means a region has its start sector after its end sector.
	ErrInvalidRegion = errors.New("ps3: plaintext region start is after its end")
)

// PlaintextRegion is a range of unencrypted sectors on a PS3 disc. Both
// bounds are inclusive.
type PlaintextRegion struct {
	StartSector uint32 // First plaintext sector (inclusive).
	EndSector   uint32 // Last plaintext sector (inclusive).
}

// EncryptionMap is the list of plaintext regions of a PS3 disc. Every sector
// not covered by a region is encrypted.
//
// Its binary (media tag) form, produced by MarshalBinary, is little-endian:
//
//	[4] region count
//	count × { [4] start sector, [4] end sector }
type EncryptionMap []PlaintextRegion

// ParseEncryptionMap parses the big-endian encryption map stored at the start
// of disc sector 0:
//
//	[4] region count (BE), [4] unknown, count × { [4] start (BE), [4] end (BE) }
//
// A map with zero regions is returned as nil.
func ParseEncryptionMap(sector0 []byte) (EncryptionMap, error) {
	if len(sector0) < 8 {
		return nil, ErrTruncated
	}

	count := binary.BigEndian.Uint32(sector0)
	if count > MaxPlaintextRegions {
		return nil, ErrTooManyRegions
	}

	if count == 0 {
		return nil, nil
	}

	if len(sector0) < 8+int(count)*8 {
		return nil, ErrTruncated
	}

	m := make(EncryptionMap, count)
	p := sector0[8:]

	for i := range m {
		m[i].StartSector = binary.BigEndian.Uint32(p)
		m[i].EndSector = binary.BigEndian.Uint32(p[4:])
		p = p[8:]

		if m[i].StartSector > m[i].EndSector {
			return nil, ErrInvalidRegion
		}
	}

	return m, nil
}

// MarshalBinary serializes the map to its little-endian media tag form
// (MediaTagPs3EncryptionMap).
func (m EncryptionMap) MarshalBinary() ([]byte, error) {
	if len(m) > MaxPlaintextRegions {
		return nil, ErrTooManyRegions
	}

	buf := make([]byte, 4+len(m)*8)
	binary.LittleEndian.PutUint32(buf, uint32(len(m)))

	for i, r := range m {
		binary.LittleEndian.PutUint32(buf[4+i*8:], r.StartSector)
		binary.LittleEndian.PutUint32(buf[8+i*8:], r.EndSector)
	}

	return buf, nil
}

// UnmarshalBinary deserializes the little-endian media tag form produced by
// MarshalBinary. Trailing bytes are ignored. Region bounds are
// not validated. A map with zero regions decodes to nil.
func (m *EncryptionMap) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return ErrTruncated
	}

	count := binary.LittleEndian.Uint32(data)
	if count > MaxPlaintextRegions {
		return ErrTooManyRegions
	}

	if count == 0 {
		*m = nil

		return nil
	}

	if len(data) < 4+int(count)*8 {
		return ErrTruncated
	}

	out := make(EncryptionMap, count)
	for i := range out {
		out[i].StartSector = binary.LittleEndian.Uint32(data[4+i*8:])
		out[i].EndSector = binary.LittleEndian.Uint32(data[8+i*8:])
	}

	*m = out

	return nil
}

// IsSectorEncrypted reports whether sector lies outside every plaintext
// region. With an empty map every sector is reported as encrypted.
func (m EncryptionMap) IsSectorEncrypted(sector uint64) bool {
	for _, r := range m {
		if sector >= uint64(r.StartSector) && sector <= uint64(r.EndSector) {
			return false
		}
	}

	return true
}
