package ngcw

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	// JunkMapVersion is the junk map serialization version.
	JunkMapVersion = 1
	// NoPartition is the partition index used for GameCube junk and Wii
	// junk outside any partition.
	NoPartition = 0xFFFF
	// JunkBlockSize is the alignment of LFG blocks on disc: a seed's stream
	// offset 0 is the start of the 0x8000-aligned block containing the junk.
	JunkBlockSize = 0x8000

	junkHeaderSize = 8
	junkEntrySize  = 18 + SeedWords*4
)

// Errors returned by the junk map functions.
var (
	// ErrTruncated means the buffer is shorter than its declared contents.
	ErrTruncated = errors.New("ngcw: junk map truncated")
	// ErrUnsupportedVersion means the junk map version is not JunkMapVersion.
	ErrUnsupportedVersion = errors.New("ngcw: unsupported junk map version")
	// ErrUnsupportedSeedSize means the junk map seed size is not SeedWords.
	ErrUnsupportedSeedSize = errors.New("ngcw: unsupported junk map seed size")
	// ErrTooLarge means the junk map cannot be represented (more than 2^32-1
	// bytes or entries).
	ErrTooLarge = errors.New("ngcw: junk map too large")
	// ErrNotJunk means the requested disc offset is not covered by the map.
	ErrNotJunk = errors.New("ngcw: offset not in junk map")
)

// JunkEntry describes a contiguous region of LFG junk on disc.
type JunkEntry struct {
	Offset         uint64 // Disc byte offset where the junk starts.
	Length         uint64 // Length of the junk region in bytes.
	PartitionIndex uint16 // Partition index, or NoPartition.
	Seed           Seed   // LFG seed for the 0x8000-aligned block containing Offset.
}

// JunkMap is a list of junk regions sorted by Offset.
//
// Its binary (media tag) form, produced by MarshalBinary, is little-endian:
//
//	[2] version (1)  [4] entry count  [2] seed size in words (17)
//	count × { [8] offset  [8] length  [2] partition index  [68] seed (17 big-endian words) }
type JunkMap []JunkEntry

// MarshalBinary serializes the map to its media tag form
// (MediaTagNgcwJunkMap).
func (m JunkMap) MarshalBinary() ([]byte, error) {
	size := uint64(junkHeaderSize) + uint64(len(m))*junkEntrySize
	if size > math.MaxUint32 {
		return nil, ErrTooLarge
	}

	buf := make([]byte, size)
	binary.LittleEndian.PutUint16(buf, JunkMapVersion)
	binary.LittleEndian.PutUint32(buf[2:], uint32(len(m)))
	binary.LittleEndian.PutUint16(buf[6:], SeedWords)

	for i := range m {
		p := buf[junkHeaderSize+i*junkEntrySize:]
		binary.LittleEndian.PutUint64(p, m[i].Offset)
		binary.LittleEndian.PutUint64(p[8:], m[i].Length)
		binary.LittleEndian.PutUint16(p[16:], m[i].PartitionIndex)
		m[i].Seed.putBytes(p[18:])
	}

	return buf, nil
}

// UnmarshalBinary deserializes the media tag form produced by MarshalBinary.
// Trailing bytes are ignored. A map with zero entries decodes to nil.
func (m *JunkMap) UnmarshalBinary(data []byte) error {
	if len(data) < junkHeaderSize {
		return ErrTruncated
	}

	if binary.LittleEndian.Uint16(data) != JunkMapVersion {
		return ErrUnsupportedVersion
	}

	count := binary.LittleEndian.Uint32(data[2:])

	if binary.LittleEndian.Uint16(data[6:]) != SeedWords {
		return ErrUnsupportedSeedSize
	}

	if count == 0 {
		*m = nil

		return nil
	}

	if uint64(len(data)) < junkHeaderSize+uint64(count)*junkEntrySize {
		return ErrTruncated
	}

	out := make(JunkMap, count)
	for i := range out {
		p := data[junkHeaderSize+i*junkEntrySize:]
		out[i].Offset = binary.LittleEndian.Uint64(p)
		out[i].Length = binary.LittleEndian.Uint64(p[8:])
		out[i].PartitionIndex = binary.LittleEndian.Uint16(p[16:])
		out[i].Seed = seedFromBytes(p[18:])
	}

	*m = out

	return nil
}

// Find returns the index of the entry covering discOffset, using the same
// binary search as other AaruFormat readers (the map must be sorted by Offset).
func (m JunkMap) Find(discOffset uint64) (int, bool) {
	lo, hi := 0, len(m)-1

	for lo <= hi {
		mid := lo + (hi-lo)/2
		end := m[mid].Offset + m[mid].Length

		switch {
		case discOffset >= end:
			lo = mid + 1
		case discOffset < m[mid].Offset:
			hi = mid - 1
		default:
			return mid, true
		}
	}

	return -1, false
}

// Regenerate fills out with the junk bytes found at discOffset on the disc
// (normally one 2048-byte sector). The LFG is seeded from the covering entry
// and advanced from the start of the entry's 0x8000-aligned block. It returns
// ErrNotJunk if no entry covers discOffset.
//
// It is used for sectors stored as SectorStatusGenerable.
func (m JunkMap) Regenerate(discOffset uint64, out []byte) error {
	i, ok := m.Find(discOffset)
	if !ok {
		return ErrNotJunk
	}

	blockStart := m[i].Offset &^ (JunkBlockSize - 1)

	g := NewLFG(m[i].Seed)
	g.Discard(discOffset - blockStart)
	_, _ = g.Read(out)

	return nil
}
