// Package media enumerates the physical media types understood by AaruFormat
// and classifies them.
//
// Constant names intentionally mirror the Aaru names (e.g. CDROM,
// GENERIC_HDD, PS3BD) so that they can be cross-referenced with the
// specification and other Aaru implementations.
package media

// MetadataType is the coarse media classification used for metadata sidecars.
type MetadataType uint8

const (
	// OpticalDisc is a purely optical disc.
	OpticalDisc MetadataType = 0
	// BlockMedia is media that is physically block-based or abstracted like that.
	BlockMedia MetadataType = 1
	// LinearMedia is media that can be accessed by-byte or by-bit, like chips.
	LinearMedia MetadataType = 2
	// AudioMedia is media that can only store data when modulated to audio.
	AudioMedia MetadataType = 3
)

// String returns the name of the classification.
func (m MetadataType) String() string {
	switch m {
	case OpticalDisc:
		return "OpticalDisc"
	case BlockMedia:
		return "BlockMedia"
	case LinearMedia:
		return "LinearMedia"
	case AudioMedia:
		return "AudioMedia"
	}
	return "MetadataType(unknown)"
}

// MetadataType returns the metadata classification of the media type: optical
// disc types map to OpticalDisc and everything else to BlockMedia.
func (t Type) MetadataType() MetadataType {
	if opticalTypes[t] {
		return OpticalDisc
	}
	return BlockMedia
}

// IsOptical reports whether t is classified as an optical disc.
func (t Type) IsOptical() bool { return opticalTypes[t] }
