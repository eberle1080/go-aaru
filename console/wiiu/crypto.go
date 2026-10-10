// Package wiiu implements Nintendo Wii U disc encryption as used by
// AaruFormat: the partition key map media tag (MediaTagWiiUPartitionKeyMap),
// per-sector key lookup and 0x8000-byte physical sector encryption, plus a
// reader for raw WUD and compressed WUX disc images.
//
// See the Wii U encryption appendix of the AaruFormat specification.
package wiiu

import (
	"encoding/binary"
	"errors"

	"github.com/eberle1080/go-aaru/console/internal/aescbc"
)

const (
	// PhysicalSectorSize is the Wii U physical (encryption) sector size.
	PhysicalSectorSize = 0x8000
	// LogicalSectorSize is the logical sector size used by AaruFormat.
	LogicalSectorSize = 2048
	// LogicalPerPhysical is the number of logical sectors per physical sector.
	LogicalPerPhysical = PhysicalSectorSize / LogicalSectorSize
	// HeaderPhysicalSectors is the number of plaintext disc header sectors.
	HeaderPhysicalSectors = 3
	// MaxPartitions is the maximum number of partitions in a key map.
	MaxPartitions = 8

	regionSize = 24
)

// Errors returned by this package.
var (
	// ErrTruncated means the key map buffer is shorter than its declared contents.
	ErrTruncated = errors.New("wiiu: partition key map truncated")
	// ErrTooManyPartitions means the partition count exceeds MaxPartitions.
	ErrTooManyPartitions = errors.New("wiiu: too many partitions")
	// ErrInvalidRegion means a partition's start sector is not below its end sector.
	ErrInvalidRegion = errors.New("wiiu: partition start is not below its end")
	// ErrInvalidLength means sector data is not a multiple of 16 bytes.
	ErrInvalidLength = errors.New("wiiu: data length is not a multiple of 16")
)

// PartitionRegion is one Wii U partition and its AES key. Sector numbers are
// in PhysicalSectorSize (0x8000-byte) units. StartSector is the partition's
// plaintext header; sectors (StartSector, EndSector) are encrypted with Key.
type PartitionRegion struct {
	StartSector uint32   // First physical sector (plaintext partition header).
	EndSector   uint32   // End physical sector (exclusive).
	Key         [16]byte // AES-128 key (disc key or title key).
}

// PartitionKeyMap lists the partitions of a Wii U disc.
//
// Its binary (media tag) form, produced by MarshalBinary, is little-endian:
//
//	[4] partition count
//	count × { [4] start sector  [4] end sector (exclusive)  [16] key }
type PartitionKeyMap []PartitionRegion

// SectorKey returns the key for the logical (2048-byte) sector and whether the
// sector is encrypted. Disc header sectors (physical 0-2), each partition's
// first physical sector and sectors outside every partition are plaintext.
func (m PartitionKeyMap) SectorKey(logicalSector uint64) ([16]byte, bool) {
	phys := logicalSector / LogicalPerPhysical
	if phys < HeaderPhysicalSectors {
		return [16]byte{}, false
	}

	for _, r := range m {
		if phys >= uint64(r.StartSector) && phys < uint64(r.EndSector) {
			if phys == uint64(r.StartSector) {
				return [16]byte{}, false // partition header
			}

			return r.Key, true
		}
	}

	return [16]byte{}, false
}

// IsSectorEncrypted reports whether the logical (2048-byte) sector is
// encrypted.
func (m PartitionKeyMap) IsSectorEncrypted(logicalSector uint64) bool {
	_, ok := m.SectorKey(logicalSector)

	return ok
}

// MarshalBinary serializes the map to its media tag form
// (MediaTagWiiUPartitionKeyMap).
func (m PartitionKeyMap) MarshalBinary() ([]byte, error) {
	if len(m) > MaxPartitions {
		return nil, ErrTooManyPartitions
	}

	buf := make([]byte, 4+len(m)*regionSize)
	binary.LittleEndian.PutUint32(buf, uint32(len(m)))

	for i, r := range m {
		p := buf[4+i*regionSize:]
		binary.LittleEndian.PutUint32(p, r.StartSector)
		binary.LittleEndian.PutUint32(p[4:], r.EndSector)
		copy(p[8:24], r.Key[:])
	}

	return buf, nil
}

// UnmarshalBinary deserializes the media tag form produced by MarshalBinary.
// Trailing bytes are ignored. A map with zero partitions decodes to nil.
func (m *PartitionKeyMap) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return ErrTruncated
	}

	count := binary.LittleEndian.Uint32(data)
	if count > MaxPartitions {
		return ErrTooManyPartitions
	}

	if count == 0 {
		*m = nil

		return nil
	}

	if len(data) < 4+int(count)*regionSize {
		return ErrTruncated
	}

	out := make(PartitionKeyMap, count)
	for i := range out {
		p := data[4+i*regionSize:]
		out[i].StartSector = binary.LittleEndian.Uint32(p)
		out[i].EndSector = binary.LittleEndian.Uint32(p[4:])
		copy(out[i].Key[:], p[8:24])

		if out[i].StartSector >= out[i].EndSector {
			return ErrInvalidRegion
		}
	}

	*m = out

	return nil
}

// EncryptPhysicalSector encrypts data (normally PhysicalSectorSize bytes) in
// place with AES-128-CBC and an all-zero IV.
func EncryptPhysicalSector(key [16]byte, data []byte) error {
	if len(data)%aescbc.BlockSize != 0 {
		return ErrInvalidLength
	}

	return aescbc.Encrypt(key, [16]byte{}, data)
}

// DecryptPhysicalSector decrypts data (normally PhysicalSectorSize bytes) in
// place with AES-128-CBC and an all-zero IV.
func DecryptPhysicalSector(key [16]byte, data []byte) error {
	if len(data)%aescbc.BlockSize != 0 {
		return ErrInvalidLength
	}

	return aescbc.Decrypt(key, [16]byte{}, data)
}
