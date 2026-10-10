package aaru

import (
	"encoding/binary"
	"fmt"
)

// This file defines the on-disk structures of the AaruFormat container. All
// structures are little-endian and byte-packed (no padding); encoding/binary
// encodes them field by field without padding, so they can be (de)serialized
// with binary.Decode / binary.Append directly. Field order MUST NOT change.

// Header is the AaruFormat file header (AaruHeaderV2, 147 bytes). Version 1
// images use only the first 104 bytes (AaruHeader); the remaining fields are
// zero for them.
type Header struct {
	Identifier              uint64                 // AaruMagic or DICMagic.
	Application             [HeaderAppNameLen]byte // Creator name: UTF-16LE in v1, UTF-8 in v2.
	ImageMajorVersion       uint8                  // Container format major version.
	ImageMinorVersion       uint8                  // Container format minor version.
	ApplicationMajorVersion uint8                  // Creator application major version.
	ApplicationMinorVersion uint8                  // Creator application minor version.
	MediaType               uint32                 // media.Type of the imaged medium.
	IndexOffset             uint64                 // Absolute offset of the index block.
	CreationTime            int64                  // FILETIME (100 ns ticks since 1601-01-01 UTC).
	LastWrittenTime         int64                  // FILETIME of last modification.
	GUID                    [GUIDSize]byte         // Image GUID.
	BlockAlignmentShift     uint8                  // log2 of block alignment.
	BiggestSectorSize       uint16                 // Size of the biggest sector in the image.
	FeatureCompatible       uint64                 // Ignorable feature bits.
	FeatureCompatibleRO     uint64                 // Unknown bits force read-only access.
	FeatureIncompatible     uint64                 // Unknown bits prevent opening.
}

// BlockHeader precedes every data block (DBLK).
type BlockHeader struct {
	Identifier  BlockType       // BlockTypeDataBlock.
	Type        DataType        // Content classification.
	Compression CompressionType // Payload compression.
	SectorSize  uint32          // Size of each item in the block (0 for single-item blocks).
	CmpLength   uint32          // Size of the payload on disk (includes LZMA properties).
	Length      uint32          // Size of the decompressed payload.
	CmpCRC64    uint64          // CRC64 of the compressed payload (LZMA: excluding properties).
	CRC64       uint64          // CRC64 of the decompressed payload.
}

// GeometryBlockHeader is the logical geometry block (GEOM).
type GeometryBlockHeader struct {
	Identifier      BlockType
	Cylinders       uint32
	Heads           uint32
	SectorsPerTrack uint32
}

// DDTHeader is the deprecated version 1 deduplication table header (DDT*).
type DDTHeader struct {
	Identifier  BlockType
	Type        DataType
	Compression CompressionType
	Shift       uint8
	Entries     uint64
	CmpLength   uint64
	Length      uint64
	CmpCRC64    uint64
	CRC64       uint64
}

// DDTHeader2 is the version 2 deduplication table header (DDT2 / DDTS).
type DDTHeader2 struct {
	Identifier          BlockType       // DDT2 for primary tables, DDTS for secondary ones.
	Type                DataType        // Data type of the sectors referenced.
	Compression         CompressionType // Table payload compression.
	Levels              uint8           // Total number of levels.
	TableLevel          uint8           // Level of this table (0 = root).
	PreviousLevelOffset uint64          // Offset of the parent table, 0 for root.
	Negative            uint32          // Number of negative sectors.
	Blocks              uint64          // Total sectors (negative + user + overflow).
	Overflow            uint32          // Number of overflow sectors.
	Start               uint64          // First sector covered by this table.
	BlockAlignmentShift uint8           // log2 of block alignment.
	DataShift           uint8           // log2 of items per block index increment.
	TableShift          uint8           // log2 of sectors per primary entry (0 = single level).
	Entries             uint64          // Number of entries in the table.
	CmpLength           uint64          // Compressed payload length.
	Length              uint64          // Uncompressed payload length.
	CmpCRC64            uint64          // CRC64 of the compressed payload.
	CRC64               uint64          // CRC64 of the uncompressed payload.
}

// IndexHeader is the deprecated version 1 index header (INDX).
type IndexHeader struct {
	Identifier BlockType
	Entries    uint16
	CRC64      uint64
}

// IndexHeader2 is the version 2 index header (IDX2).
type IndexHeader2 struct {
	Identifier BlockType
	Entries    uint64
	CRC64      uint64
}

// IndexHeader3 is the version 3 index header (IDX3).
type IndexHeader3 struct {
	Identifier BlockType
	Entries    uint64
	CRC64      uint64
	Previous   uint64 // Offset of a previous IDX3 block, 0 if none.
}

// IndexEntry locates a block in the image.
type IndexEntry struct {
	BlockType BlockType // Identifier of the referenced block.
	DataType  DataType  // Data type of the referenced block (0 if untyped).
	Offset    uint64    // Absolute offset of the block header.
}

// MetadataBlockHeader is the header of the metadata block (META). String
// offsets are relative to the start of the block; strings are UTF-16LE and
// lengths include the terminating NUL.
type MetadataBlockHeader struct {
	Identifier                  BlockType
	BlockSize                   uint32
	MediaSequence               int32
	LastMediaSequence           int32
	CreatorOffset               uint32
	CreatorLength               uint32
	CommentsOffset              uint32
	CommentsLength              uint32
	MediaTitleOffset            uint32
	MediaTitleLength            uint32
	MediaManufacturerOffset     uint32
	MediaManufacturerLength     uint32
	MediaModelOffset            uint32
	MediaModelLength            uint32
	MediaSerialNumberOffset     uint32
	MediaSerialNumberLength     uint32
	MediaBarcodeOffset          uint32
	MediaBarcodeLength          uint32
	MediaPartNumberOffset       uint32
	MediaPartNumberLength       uint32
	DriveManufacturerOffset     uint32
	DriveManufacturerLength     uint32
	DriveModelOffset            uint32
	DriveModelLength            uint32
	DriveSerialNumberOffset     uint32
	DriveSerialNumberLength     uint32
	DriveFirmwareRevisionOffset uint32
	DriveFirmwareRevisionLength uint32
}

// CICMBlockHeader is the header of the CICM XML metadata block (CICM).
type CICMBlockHeader struct {
	Identifier BlockType
	Length     uint32
}

// JSONBlockHeader is the header of the Aaru JSON metadata block (JSMD).
type JSONBlockHeader struct {
	Identifier BlockType
	Length     uint32
}

// TracksHeader is the header of the optical tracks block (TRKS).
type TracksHeader struct {
	Identifier BlockType
	Entries    uint16
	CRC64      uint64
}

// TrackEntry describes an optical disc track.
type TrackEntry struct {
	Sequence uint8     // Track number (0 is a valid hidden track before track 1).
	Type     TrackType // Track type.
	Start    int64     // Inclusive starting LBA.
	End      int64     // Inclusive ending LBA.
	Pregap   int64     // Pregap length in sectors.
	Session  uint8     // Session number (1-based).
	ISRC     [13]byte  // Raw ISRC, all zeros if absent.
	Flags    uint8     // Track control flags.
}

// ChecksumHeader is the header of the checksum block (CKSM).
type ChecksumHeader struct {
	Identifier BlockType
	Length     uint32
	Entries    uint8
}

// ChecksumEntry precedes each digest in the checksum block.
type ChecksumEntry struct {
	Type   ChecksumAlgorithm
	Length uint32
}

// DumpHardwareHeader is the header of the dump hardware block (DMP*).
type DumpHardwareHeader struct {
	Identifier BlockType
	Entries    uint16
	Length     uint32
	CRC64      uint64
}

// DumpHardwareEntry is the fixed part of each dump hardware record; it is
// followed on disk by the UTF-8 strings (in field order) and the extents.
type DumpHardwareEntry struct {
	ManufacturerLength            uint32
	ModelLength                   uint32
	RevisionLength                uint32
	FirmwareLength                uint32
	SerialLength                  uint32
	SoftwareNameLength            uint32
	SoftwareVersionLength         uint32
	SoftwareOperatingSystemLength uint32
	Extents                       uint32
}

// DumpExtent is an inclusive range of sectors read by a dump device.
type DumpExtent struct {
	Start uint64
	End   uint64
}

// TapeFileHeader is the header of the tape file block (TFLE).
type TapeFileHeader struct {
	Identifier BlockType
	Entries    uint32
	Length     uint64
	CRC64      uint64
}

// TapeFileEntry describes a file on a tape partition.
type TapeFileEntry struct {
	File       uint32
	Partition  uint8
	FirstBlock uint64
	LastBlock  uint64
}

// TapePartitionHeader is the header of the tape partition block (TPBT).
type TapePartitionHeader struct {
	Identifier BlockType
	Entries    uint8
	Length     uint64
	CRC64      uint64
}

// TapePartitionEntry describes a tape partition.
type TapePartitionEntry struct {
	Number     uint8
	FirstBlock uint64
	LastBlock  uint64
}

// FluxHeader is the header of the flux data block (FLUX).
type FluxHeader struct {
	Identifier          BlockType
	Entries             uint16
	BlockAlignmentShift uint8
	CRC64               uint64
}

// FluxEntry describes a flux capture stored in a data stream payload block.
type FluxEntry struct {
	Head            uint32
	Track           uint16
	Subtrack        uint8
	CaptureIndex    uint32
	IndexResolution uint64 // picoseconds
	DataResolution  uint64 // picoseconds
	IndexOffset     uint64 // offset of the index buffer inside the payload
	PayloadOffset   uint64 // payload block offset >> FluxHeader.BlockAlignmentShift
}

// DataStreamPayloadHeader is the header of a data stream payload block (DSPL).
type DataStreamPayloadHeader struct {
	Identifier  BlockType
	DataType    DataType
	Compression CompressionType
	CmpLength   uint32
	Length      uint32
	CmpCRC64    uint64
	CRC64       uint64
}

// DPMHeader is the header of the data position measurement block (DPM*).
type DPMHeader struct {
	Identifier     BlockType
	Entries        uint32
	Calibrations   uint32
	NominalSpacing uint32
	TimingUnit     uint16
	Speed          uint16
	LayerEnds      uint8
	Flags          uint8
	Reserved       uint16
	Length         uint64
	CRC64          uint64
}

// DPMEntry is a data position measurement control point.
type DPMEntry struct {
	LBA    uint64
	Angle  uint64
	Status DpmEntryStatus
}

// DPMCalibration is a rotation calibration record.
type DPMCalibration struct {
	LBA                 uint64
	RotationPeriod      uint64 // nanoseconds
	SectorsPerTurnX1000 uint64
}

// ErasureCodingMapHeader is the header of the erasure coding map block (ECMB).
type ErasureCodingMapHeader struct {
	Identifier       BlockType
	Algorithm        ErasureCodingAlgorithm
	StripeGroupCount uint8
	Compression      CompressionType
	CmpLength        uint64
	Length           uint64
	CmpCRC64         uint64
	CRC64            uint64
}

// StripeGroupDescriptor describes an erasure coding protection group.
type StripeGroupDescriptor struct {
	GroupType       ErasureCodingGroupType
	K               uint16
	M               uint16
	ShardSize       uint32
	StripeCount     uint32
	InterleaveDepth uint16
}

// StripeDataBlockEntry describes a data shard of a stripe.
type StripeDataBlockEntry struct {
	Offset     uint64
	OnDiskSize uint32
	ShardCRC64 uint64
}

// StripeParityBlockEntry locates a parity shard of a stripe.
type StripeParityBlockEntry struct {
	Offset uint64
}

// RecoveryFooter is stored at the very end of images with erasure coding.
type RecoveryFooter struct {
	ECMBOffset   uint64
	ECMBLength   uint64
	HeaderCRC64  uint64
	BackupHeader Header
	FooterMagic  uint64
}

// On-disk sizes of the structures above.
const (
	headerV1Size               = 104
	headerSize                 = 147
	blockHeaderSize            = 36
	geometryBlockHeaderSize    = 16
	ddtHeaderSize              = 49
	ddtHeader2Size             = 85
	indexHeaderSize            = 14
	indexHeader2Size           = 20
	indexHeader3Size           = 28
	indexEntrySize             = 14
	metadataBlockHeaderSize    = 112
	cicmBlockHeaderSize        = 8
	jsonBlockHeaderSize        = 8
	tracksHeaderSize           = 14
	trackEntrySize             = 41
	checksumHeaderSize         = 9
	checksumEntrySize          = 5
	dumpHardwareHeaderSize     = 18
	dumpHardwareEntrySize      = 36
	dumpExtentSize             = 16
	tapeFileHeaderSize         = 24
	tapeFileEntrySize          = 21
	tapePartitionHeaderSize    = 21
	tapePartitionEntrySize     = 17
	fluxHeaderSize             = 15
	fluxEntrySize              = 43
	dataStreamPayloadHdrSize   = 32
	dpmHeaderSize              = 40
	dpmEntrySize               = 17
	dpmCalibrationSize         = 24
	ecMapHeaderSize            = 40
	stripeGroupDescriptorSize  = 15
	stripeDataBlockEntrySize   = 20
	stripeParityBlockEntrySize = 8
	recoveryFooterSize         = 179
)

// unmarshal decodes a packed little-endian structure (or slice of structures)
// from b. It fails if b is too short.
func unmarshal[T any](b []byte) (T, error) {
	var v T
	if _, err := binary.Decode(b, binary.LittleEndian, &v); err != nil {
		return v, fmt.Errorf("decoding %T: %w", v, err)
	}
	return v, nil
}

// unmarshalSlice decodes n consecutive packed structures from b.
func unmarshalSlice[T any](b []byte, n int) ([]T, error) {
	v := make([]T, n)
	if n == 0 {
		return v, nil
	}
	if _, err := binary.Decode(b, binary.LittleEndian, v); err != nil {
		var zero T
		return nil, fmt.Errorf("decoding []%T: %w", zero, err)
	}
	return v, nil
}

// marshal encodes a packed little-endian structure (or slice of structures).
// It panics only on programming errors (non fixed-size types).
func marshal(v any) []byte {
	b, err := binary.Append(nil, binary.LittleEndian, v)
	if err != nil {
		panic(fmt.Sprintf("aaru: marshal %T: %v", v, err))
	}
	return b
}

// appendMarshal appends the packed encoding of v to dst.
func appendMarshal(dst []byte, v any) []byte {
	b, err := binary.Append(dst, binary.LittleEndian, v)
	if err != nil {
		panic(fmt.Sprintf("aaru: marshal %T: %v", v, err))
	}
	return b
}

// Fast paths for the hottest structures.

func decodeBlockHeader(b []byte) (BlockHeader, error) {
	if len(b) < blockHeaderSize {
		return BlockHeader{}, fmt.Errorf("block header: short buffer (%d bytes)", len(b))
	}
	le := binary.LittleEndian
	return BlockHeader{
		Identifier:  BlockType(le.Uint32(b[0:])),
		Type:        DataType(le.Uint16(b[4:])),
		Compression: CompressionType(le.Uint16(b[6:])),
		SectorSize:  le.Uint32(b[8:]),
		CmpLength:   le.Uint32(b[12:]),
		Length:      le.Uint32(b[16:]),
		CmpCRC64:    le.Uint64(b[20:]),
		CRC64:       le.Uint64(b[28:]),
	}, nil
}

func (h *BlockHeader) appendTo(dst []byte) []byte {
	le := binary.LittleEndian
	dst = le.AppendUint32(dst, uint32(h.Identifier))
	dst = le.AppendUint16(dst, uint16(h.Type))
	dst = le.AppendUint16(dst, uint16(h.Compression))
	dst = le.AppendUint32(dst, h.SectorSize)
	dst = le.AppendUint32(dst, h.CmpLength)
	dst = le.AppendUint32(dst, h.Length)
	dst = le.AppendUint64(dst, h.CmpCRC64)
	return le.AppendUint64(dst, h.CRC64)
}

// decodeUint64s decodes little-endian uint64 values (DDT v2 entries).
func decodeUint64s(b []byte) []uint64 {
	v := make([]uint64, len(b)/8)
	for i := range v {
		v[i] = binary.LittleEndian.Uint64(b[i*8:])
	}
	return v
}

// encodeUint64s encodes v as little-endian bytes.
func encodeUint64s(v []uint64) []byte {
	b := make([]byte, len(v)*8)
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[i*8:], x)
	}
	return b
}

// decodeUint32s decodes little-endian uint32 values (legacy CD tables).
func decodeUint32s(b []byte) []uint32 {
	v := make([]uint32, len(b)/4)
	for i := range v {
		v[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return v
}
