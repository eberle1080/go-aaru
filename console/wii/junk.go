package wii

import "github.com/eberle1080/go-aaru/console/ngcw"

// ReconstructGroupJunk regenerates the LFG junk inside the user data area of a
// decrypted group (GroupSize bytes: hash block followed by user data), in
// place, prior to re-encryption with EncryptGroup.
//
// physGroup is the group's physical index on disc (disc offset / GroupSize).
// Groups outside every partition of regions are left untouched. For each
// 2048-byte chunk of user data whose disc offset
// (physGroup*GroupSize + HashSize + chunk offset) is covered by junk, the
// chunk is overwritten with LFG output from the entry's seed at stream
// position
//
//	(groupIndexInPartition*DataSize + chunkOffset) mod GroupSize
//
// because the Wii LFG stream covers user data only. Chunks not covered by
// junk keep their stored contents.
func ReconstructGroupJunk(group []byte, physGroup uint64, regions PartitionKeyMap, junk ngcw.JunkMap) error {
	if len(group) != GroupSize {
		return ErrInvalidGroupSize
	}

	if len(junk) == 0 {
		return nil
	}

	part, ok := regions.Partition(physGroup)
	if !ok {
		return nil
	}

	groupIdx := physGroup - uint64(regions[part].StartSector)
	groupDiscOff := physGroup * GroupSize

	var g ngcw.LFG

	for off := uint64(0); off < DataSize; off += SectorSize {
		e, found := junk.Find(groupDiscOff + HashSize + off)
		if !found {
			continue
		}

		g.Reset(junk[e].Seed)
		g.Discard((groupIdx*DataSize + off) % GroupSize)

		chunk := min(uint64(DataSize)-off, SectorSize)
		_, _ = g.Read(group[HashSize+off : HashSize+off+chunk])
	}

	return nil
}
