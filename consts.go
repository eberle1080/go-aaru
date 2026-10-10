package aaru

// File magics and versions.
const (
	// DICMagic is the identifier of images written by the old DiscImageChef
	// ("DICMFRMT" as a little-endian uint64).
	DICMagic uint64 = 0x544D52464D434944
	// AaruMagic is the identifier of AaruFormat images ("AARUFRMT").
	AaruMagic uint64 = 0x544D524655524141
	// RecoveryFooterMagic identifies the erasure-coding recovery footer at EOF.
	RecoveryFooterMagic uint64 = 0x52464D4345525641

	// FormatVersion is the newest image major version supported.
	FormatVersion = 2
	// FormatVersionV1 is the legacy (read-only) image major version.
	FormatVersionV1 = 1
	// FormatVersionV2 is the current image major version.
	FormatVersionV2 = 2

	// LibraryMajorVersion and LibraryMinorVersion are the API version of
	// this library.
	LibraryMajorVersion = 1
	LibraryMinorVersion = 0
)

// Sizes and limits.
const (
	// HeaderAppNameLen is the size of the application name field in the header.
	HeaderAppNameLen = 64
	// GUIDSize is the size of the image GUID.
	GUIDSize = 16
	// LZMAPropertiesLength is the size of the LZMA properties prefix stored in
	// front of LZMA-compressed payloads.
	LZMAPropertiesLength = 5
	// SamplesPerSector is the number of 16-bit stereo samples in a CD audio sector.
	SamplesPerSector = 588
	// MaxFLACBlock and MinFLACBlock bound the FLAC block size.
	MaxFLACBlock = 4608
	MinFLACBlock = 256

	maxCacheSize       = 536870912 // bytes of decompressed blocks cached
	maxHeaderCacheSize = 8388608   // bytes of block headers cached
	maxDDTEntryCache   = 16000000
)

// Masks for legacy (DDT v1) CD prefix/suffix tables.
const (
	cdXFixMask uint32 = 0xFF000000 // flags part
	cdDFixMask uint32 = 0x00FFFFFF // pointer part
)

// Feature flags (header featureCompatible / featureCompatibleRo /
// featureIncompatible).
const (
	// FeatureRWBlake3 (compatible): a BLAKE3 checksum is present.
	FeatureRWBlake3 uint64 = 0x1
	// FeatureIncompatZstd (incompatible): the image contains Zstandard blocks.
	FeatureIncompatZstd uint64 = 0x1
	// FeatureROCompatErasure (read-only compatible): the image contains erasure
	// coding parity blocks and recovery metadata.
	FeatureROCompatErasure uint64 = 0x1

	knownIncompatFeatures uint64 = 0x1
	knownROCompatFeatures uint64 = 0x1
	knownCompatFeatures   uint64 = 0x1
)

// Data position measurement constants.
const (
	// DPMUnitsPerTurn is the number of angle units in a full disc rotation.
	DPMUnitsPerTurn = 2560000
	// DPMFlagOppositeTrackPath marks layers using opposite track path.
	DPMFlagOppositeTrackPath = 0x01
)
