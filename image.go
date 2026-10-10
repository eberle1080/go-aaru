package aaru

import (
	"hash"
	"io"
	"time"

	"github.com/eberle1080/go-aaru/media"
)

// ImageInfo summarizes an opened image.
type ImageInfo struct {
	HasPartitions        bool       // The image contains partitions (or tracks for optical media).
	HasSessions          bool       // The image contains sessions (optical media).
	ImageSize            uint64     // Size of the stored (compressed) payload in bytes.
	Sectors              uint64     // Number of user sectors.
	SectorSize           uint32     // Size of the biggest sector, in bytes.
	Version              string     // Container format version, e.g. "2.0".
	Application          string     // Creator application name.
	ApplicationVersion   string     // Creator application version, e.g. "1.0".
	CreationTime         time.Time  // Image creation time (UTC).
	LastModificationTime time.Time  // Image last modification time (UTC).
	MediaType            media.Type // Media type.
	// MetadataMediaType is the coarse classification of MediaType.
	MetadataMediaType media.MetadataType
}

// Checksums holds whole-image checksums read from (or written to) the
// checksum block. Nil slices / empty strings mean "not present".
type Checksums struct {
	MD5     []byte // 16 bytes
	SHA1    []byte // 20 bytes
	SHA256  []byte // 32 bytes
	Blake3  []byte // 32 bytes
	SpamSum string // ASCII SpamSum signature
}

// Image is an open AaruFormat image, either for reading (Open, OpenReader)
// or for writing (Create, Resume).
//
// An Image is not safe for concurrent use; callers must serialize access.
// Close must be called to release resources and, for writable images, to
// finalize the file (writing tables, metadata and the index).
type Image struct {
	// ---- backing storage ----
	r      io.ReaderAt // always set
	w      io.WriterAt // nil when read-only
	closer io.Closer   // closed by Close; may be nil
	size   int64       // current size of the backing file (end of data)
	closed bool

	// ---- header / summary ----
	header Header
	info   ImageInfo

	// ---- deduplication tables ----
	ddtVersion        int        // 1 = legacy flat DDT, 2 = DDT v2
	shift             uint8      // DDT v1 shift
	userDataDDT       []uint64   // DDT v1 flat table (user data)
	sectorPrefixDDT   []uint32   // DDT v1 CD prefix table
	sectorSuffixDDT   []uint32   // DDT v1 CD suffix table
	userDataDDTHeader DDTHeader2 // DDT v2 header of the user data table (primary)
	userDataDDT2      []uint64   // DDT v2 primary (or single-level) table entries
	sectorPrefixDDT2  []uint64   // DDT v2 CD prefix table entries
	sectorSuffixDDT2  []uint64   // DDT v2 CD suffix table entries
	// Secondary table cache (multi-level DDT v2).
	cachedSecondaryDDT2 []uint64
	cachedDDTOffset     uint64 // file offset of the cached secondary table (0 = none)
	cachedDDTPosition   uint64 // primary entry index of the cached secondary table
	primaryDDTOffset    uint64 // file offset of the primary DDT v2 table
	inMemoryDDT         bool
	noUserDataDDT       bool              // flux-only image: no user data DDT at all
	tapeDDT             map[uint64]uint64 // tape images: sector -> DDT entry

	// ---- per-sector tag arenas (raw concatenated data) ----
	sectorPrefix            []byte // CD sync+header (16 bytes per sector, or compact arena for DDT v2)
	sectorPrefixCorrected   []byte
	sectorSuffix            []byte // CD EDC/ECC (288 bytes per sector, or compact arena)
	sectorSuffixCorrected   []byte
	sectorSubchannel        []byte // CD subchannel (96 bytes/sector) or Apple/Priam tags
	mode2Subheaders         []byte // CD MODE 2 subheaders (8 bytes per sector)
	sectorID                []byte // DVD sector ID (4 bytes per sector)
	sectorIED               []byte // DVD sector IED (2 bytes per sector)
	sectorCPRMAI            []byte // DVD CPR_MAI (6 bytes per sector)
	sectorEDC               []byte // DVD/BD sector EDC (4 bytes per sector)
	sectorDecryptedTitleKey []byte // DVD decrypted title key (5 bytes per sector)
	sectorPrefixOffset      uint64 // write position inside sectorPrefix arena
	sectorSuffixOffset      uint64 // write position inside sectorSuffix arena

	readableSectorTags [MaxSectorTag + 1]bool
	mediaTags          map[MediaTagType][]byte
	mediaTagOrder      []MediaTagType // WriteMediaTag insertion order, for close

	// ---- metadata ----
	geometry            GeometryBlockHeader
	cylinders           uint32
	heads               uint32
	sectorsPerTrack     uint32
	metadataBlockHeader MetadataBlockHeader
	metadataBlock       []byte // raw META block (header + strings)
	mediaSequence       int32
	lastMediaSequence   int32
	// Raw UTF-16LE metadata strings (as stored on disk, including NUL), nil if absent.
	creator               []byte
	comments              []byte
	mediaTitle            []byte
	mediaManufacturer     []byte
	mediaModel            []byte
	mediaSerialNumber     []byte
	mediaBarcode          []byte
	mediaPartNumber       []byte
	driveManufacturer     []byte
	driveModel            []byte
	driveSerialNumber     []byte
	driveFirmwareRevision []byte
	cicmBlock             []byte // CICM XML payload
	jsonBlock             []byte // Aaru JSON metadata payload
	dumpHardware          []DumpHardware
	dumpHWHeader          DumpHardwareHeader
	dpmBlock              []byte // DPM block including its header, nil if absent
	dpmDamagedOffset      uint64 // offset of a DPM block that failed to load (EC recovery), 0 if none
	checksums             Checksums

	// ---- optical ----
	tracksHeader TracksHeader
	tracks       []TrackEntry
	dataTracks   []TrackEntry

	// ---- tape ----
	isTape         bool
	tapeFiles      map[uint64]TapeFileEntry     // key: partition<<32 | file
	tapePartitions map[uint8]TapePartitionEntry // key: partition number
	// Insertion order of tapeFiles / tapePartitions (replaced keys move to
	// the end), which is the order the blocks are written in.
	tapeFileOrder      []uint64
	tapePartitionOrder []uint8

	// ---- flux ----
	fluxHeader   FluxHeader
	fluxEntries  []FluxEntry
	fluxLoaded   bool                   // flux entries loaded from disk (lazy)
	fluxOffset   uint64                 // offset of the FLUX block, 0 if none
	fluxCaptures []fluxCaptureRecord    // pending captures (write path)
	fluxMap      map[FluxCaptureKey]int // capture key -> index in fluxEntries/fluxCaptures

	// ---- index ----
	indexEntries []IndexEntry

	// ---- read caches ----
	blockHeaderCache *lruCache // block offset -> BlockHeader
	blockCache       *lruCache // block offset -> decompressed []byte

	// ---- write state ----
	writable           bool
	options            Options
	nextBlockPosition  uint64
	lastWrittenBlock   uint64
	currentBlockHeader BlockHeader
	writingBuffer      []byte
	currentBlockOffset int
	currentTrackType   TrackType
	rewound            bool // checksums cannot be computed (out-of-order or resumed writes)
	writingLong        bool
	blockZeroWritten   bool
	hasZstdBlocks      bool
	sectorHashMap      map[uint64]uint64 // dedup: content hash -> DDT entry
	md5Hasher          hash.Hash
	sha1Hasher         hash.Hash
	sha256Hasher       hash.Hash
	blake3Hasher       hash.Hash
	spamsumHasher      io.Writer // *spamsum.Hasher; see checksum code
	calculatingMD5     bool
	calculatingSHA1    bool
	calculatingSHA256  bool
	calculatingSpamSum bool
	calculatingBlake3  bool
	dirty              dirtyFlags

	// ---- console crypto (lazy) ----
	console consoleState

	// ---- erasure coding ----
	ec ecState
}

// dirtyFlags record which structures must be (re)written on close.
type dirtyFlags struct {
	secondaryDDT, primaryDDT, singleLevelDDT               bool
	checksumBlock, tracksBlock, mode2SubheadersBlock       bool
	sectorPrefixBlock, sectorPrefixDDT                     bool
	sectorSuffixBlock, sectorSuffixDDT                     bool
	sectorSubchannelBlock, dvdLongSectorBlocks             bool
	bdSectorEDCBlock, dvdTitleKeyDecryptedBlock, mediaTags bool
	tapeDDT, tapeFileBlock, tapePartitionBlock             bool
	geometryBlock, metadataBlock, dumpHWBlock              bool
	cicmBlock, jsonBlock, dpmBlock, fluxBlock, indexBlock  bool
}

// all marks every structure dirty (new images).
func (d *dirtyFlags) all(hasDDT bool) {
	*d = dirtyFlags{
		secondaryDDT: hasDDT, primaryDDT: hasDDT, singleLevelDDT: hasDDT,
		checksumBlock: true, tracksBlock: true, mode2SubheadersBlock: true,
		sectorPrefixBlock: true, sectorPrefixDDT: true, sectorSuffixBlock: true, sectorSuffixDDT: true,
		sectorSubchannelBlock: true, dvdLongSectorBlocks: true, bdSectorEDCBlock: true,
		dvdTitleKeyDecryptedBlock: true, mediaTags: true, tapeDDT: true, tapeFileBlock: true,
		tapePartitionBlock: true, geometryBlock: true, metadataBlock: true, dumpHWBlock: true,
		cicmBlock: true, jsonBlock: true, dpmBlock: true, fluxBlock: true, indexBlock: true,
	}
}

// DumpHardware describes a device (and software) used to dump the media,
// together with the sector extents it read.
type DumpHardware struct {
	Manufacturer            string
	Model                   string
	Revision                string
	Firmware                string
	Serial                  string
	SoftwareName            string
	SoftwareVersion         string
	SoftwareOperatingSystem string
	Extents                 []DumpExtent
}

// FluxCaptureKey identifies a flux capture.
type FluxCaptureKey struct {
	Head         uint32
	Track        uint16
	Subtrack     uint8
	CaptureIndex uint32
}

// fluxCaptureRecord is a flux capture pending to be written.
type fluxCaptureRecord struct {
	entry FluxEntry
	data  []byte
	index []byte
}

// Header returns a copy of the image header.
func (img *Image) Header() Header { return img.header }

// Info returns the image summary.
func (img *Image) Info() ImageInfo { return img.info }

// MediaType returns the media type of the image.
func (img *Image) MediaType() media.Type { return media.Type(img.header.MediaType) }

// Writable reports whether the image was opened for writing.
func (img *Image) Writable() bool { return img.writable }

// alignUp rounds off up to the next multiple of 1<<shift.
func alignUp(off uint64, shift uint8) uint64 {
	mask := uint64(1)<<shift - 1
	return (off + mask) &^ mask
}

// crcMatches reports whether a computed CRC64 matches a stored one, taking the
// version 1 byte-swap quirk into account.
func (img *Image) crcMatches(computed, stored uint64) bool {
	if img.header.ImageMajorVersion <= FormatVersionV1 {
		computed = bswap64(computed)
	}
	return computed == stored
}

func bswap64(v uint64) uint64 {
	return v>>56 | (v>>40)&0xFF00 | (v>>24)&0xFF0000 | (v>>8)&0xFF000000 |
		(v<<8)&0xFF00000000 | (v<<24)&0xFF0000000000 | (v<<40)&0xFF000000000000 | v<<56
}
