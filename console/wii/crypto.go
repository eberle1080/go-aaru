// Package wii implements Nintendo Wii disc partition encryption as used by
// AaruFormat: the partition key map media tag (MediaTagWiiPartitionKeyMap),
// per-sector key lookup, 0x8000-byte group encryption/decryption (hash block
// plus user data) and reconstruction of LFG junk inside a decrypted group
// before re-encryption.
//
// See the Wii encryption appendix of the AaruFormat specification.
package wii

import (
	"encoding/binary"
	"errors"

	"github.com/eberle1080/go-aaru/console/internal/aescbc"
)

const (
	// GroupSize is the size of an encrypted Wii group (32 KiB).
	GroupSize = 0x8000
	// HashSize is the size of the hash block at the start of a group.
	HashSize = 0x0400
	// DataSize is the size of the user data following the hash block.
	DataSize = 0x7C00
	// SectorSize is the logical sector size in bytes.
	SectorSize = 2048
	// LogicalPerGroup is the number of logical sectors per group.
	LogicalPerGroup = GroupSize / SectorSize
	// MaxPartitions is the maximum number of partitions in a key map.
	MaxPartitions = 32

	// dataIVOffset is where, in the encrypted hash block, the user data IV is.
	dataIVOffset = 0x3D0
	regionSize   = 24
)

// Errors returned by this package.
var (
	// ErrTruncated means the key map buffer is shorter than its declared contents.
	ErrTruncated = errors.New("wii: partition key map truncated")
	// ErrTooManyPartitions means the partition count exceeds MaxPartitions.
	ErrTooManyPartitions = errors.New("wii: too many partitions")
	// ErrInvalidRegion means a partition's start sector is not below its end sector.
	ErrInvalidRegion = errors.New("wii: partition start is not below its end")
	// ErrInvalidGroupSize means a group buffer is not exactly GroupSize bytes.
	ErrInvalidGroupSize = errors.New("wii: group buffer must be 0x8000 bytes")
)

// PartitionRegion is the encrypted data area of one partition and its title
// key. Sector numbers are in GroupSize (0x8000-byte) units. StartSector is the
// first group of the partition's *data area* (partition offset + data offset),
// so every group in [StartSector, EndSector) is encrypted.
type PartitionRegion struct {
	StartSector uint32   // First encrypted group (inclusive).
	EndSector   uint32   // End group (exclusive).
	Key         [16]byte // AES-128 title key.
}

// PartitionKeyMap lists the encrypted partition data areas of a Wii disc.
//
// Its binary (media tag) form, produced by MarshalBinary, is little-endian and
// identical to the Wii U partition key map:
//
//	[4] partition count
//	count × { [4] start sector  [4] end sector (exclusive)  [16] key }
type PartitionKeyMap []PartitionRegion

// Partition returns the index of the first partition whose data area contains
// the physical group physGroup.
func (m PartitionKeyMap) Partition(physGroup uint64) (int, bool) {
	for i, r := range m {
		if physGroup >= uint64(r.StartSector) && physGroup < uint64(r.EndSector) {
			return i, true
		}
	}

	return -1, false
}

// SectorKey returns the title key for the logical (2048-byte) sector, and
// whether the sector is encrypted. Every group inside a partition's data area,
// including its first group, is encrypted; sectors outside all partitions are
// plaintext.
func (m PartitionKeyMap) SectorKey(logicalSector uint64) ([16]byte, bool) {
	i, ok := m.Partition(logicalSector / LogicalPerGroup)
	if !ok {
		return [16]byte{}, false
	}

	return m[i].Key, true
}

// IsSectorEncrypted reports whether the logical (2048-byte) sector lies in an
// encrypted partition data area.
func (m PartitionKeyMap) IsSectorEncrypted(logicalSector uint64) bool {
	_, ok := m.SectorKey(logicalSector)

	return ok
}

// MarshalBinary serializes the map to its media tag form
// (MediaTagWiiPartitionKeyMap).
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

// EncryptGroup encrypts a plaintext group (hash block followed by user data,
// GroupSize bytes) in place. The hash block is encrypted with a zero IV; the
// user data IV is bytes 0x3D0..0x3DF of the encrypted hash block.
func EncryptGroup(key [16]byte, group []byte) error {
	if len(group) != GroupSize {
		return ErrInvalidGroupSize
	}

	if err := aescbc.Encrypt(key, [16]byte{}, group[:HashSize]); err != nil {
		return err
	}

	var iv [16]byte
	copy(iv[:], group[dataIVOffset:])

	return aescbc.Encrypt(key, iv, group[HashSize:])
}

// DecryptGroup decrypts an encrypted group (GroupSize bytes) in place,
// yielding the plaintext hash block in group[:HashSize] and the user data in
// group[HashSize:].
func DecryptGroup(key [16]byte, group []byte) error {
	if len(group) != GroupSize {
		return ErrInvalidGroupSize
	}

	// The data IV comes from the *encrypted* hash block.
	var iv [16]byte
	copy(iv[:], group[dataIVOffset:])

	if err := aescbc.Decrypt(key, [16]byte{}, group[:HashSize]); err != nil {
		return err
	}

	return aescbc.Decrypt(key, iv, group[HashSize:])
}
