package aaru

// Accessors used by cmd/aaruformattool (info, verify --recover) for internal
// image state.

// UserDataDDTHeader returns the header of the user data deduplication table
// of a version 2 image (for multi-level tables the primary table's header). ok is false for version 1 images, flux-only
// images and images whose table is not a DDT2 block.
func (img *Image) UserDataDDTHeader() (h DDTHeader2, ok bool) {
	if img.ddtVersion != 2 || img.userDataDDTHeader.Identifier != BlockTypeDeDuplicationTable2 {
		return DDTHeader2{}, false
	}
	return img.userDataDDTHeader, true
}

// ErasureCodingInfo describes the erasure coding recovery data of an opened
// image.
type ErasureCodingInfo struct {
	// Algorithm is the algorithm recorded in the erasure coding map.
	Algorithm ErasureCodingAlgorithm
	// K and M are the data and parity blocks per stripe of the user data
	// group (0 if the image only protects metadata).
	K, M uint16
	// ShardSize is the shard size of the user data group in bytes.
	ShardSize uint32
	// DataStripes is the number of user data stripes.
	DataStripes uint32
}

// ErasureCoding reports the erasure coding recovery data loaded when the image
// was opened. ok is false when the image has no usable recovery data.
func (img *Image) ErasureCoding() (info ErasureCodingInfo, ok bool) {
	if !img.ec.recoveryAvailable {
		return ErasureCodingInfo{}, false
	}
	if g := img.ec.data; g != nil {
		info.K, info.M, info.ShardSize, info.DataStripes = g.k, g.m, g.shardSize, uint32(len(g.stripes))
	}
	info.Algorithm = img.ecMapAlgorithm()
	return info, true
}

// ecMapAlgorithm re-reads the algorithm of the erasure coding map that
// ecLoadECMB used (the primary map, or the duplicate one before the recovery
// footer). It returns ErasureCodingRsVandermonde if neither can be read
// (which cannot happen when recovery data was loaded).
func (img *Image) ecMapAlgorithm() ErasureCodingAlgorithm {
	if img.size < recoveryFooterSize {
		return ErasureCodingRsVandermonde
	}
	var fb [recoveryFooterSize]byte
	if err := img.readFullAt(fb[:], uint64(img.size)-recoveryFooterSize); err != nil {
		return ErasureCodingRsVandermonde
	}
	footer, err := unmarshal[RecoveryFooter](fb[:])
	if err != nil {
		return ErasureCodingRsVandermonde
	}
	if mh, _, err := img.readECMB(footer.ECMBOffset); err == nil {
		return mh.Algorithm
	}
	end := uint64(img.size) - recoveryFooterSize
	if footer.ECMBLength <= end {
		if mh, _, err := img.readECMB(end - footer.ECMBLength); err == nil {
			return mh.Algorithm
		}
	}
	return ErasureCodingRsVandermonde
}
