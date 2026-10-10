package udf

import (
	"errors"
	"io/fs"
	"time"
)

// Errors returned by this package, usually wrapped with context. Test them
// with errors.Is.
var (
	// ErrNilReader means Open was given no reader.
	ErrNilReader = errors.New("udf: nil reader")
	// ErrNotUDF means neither a UDF volume recognition sequence nor an
	// anchor volume descriptor pointer was found.
	ErrNotUDF = errors.New("udf: not a UDF volume")
	// ErrNoAnchor means the volume recognition sequence announces UDF
	// (NSR02/NSR03) but no usable anchor volume descriptor pointer exists.
	ErrNoAnchor = errors.New("udf: no anchor volume descriptor pointer")
	// ErrNoVolume means no volume descriptor sequence yields a logical
	// volume descriptor.
	ErrNoVolume = errors.New("udf: no logical volume descriptor")
	// ErrNoFileSet means no file set descriptor could be read.
	ErrNoFileSet = errors.New("udf: no file set descriptor")
	// ErrPartition means a partition reference names no usable partition.
	ErrPartition = errors.New("udf: invalid partition reference")
	// ErrOutOfRange means an extent or block lies (partly) outside the
	// source or its partition.
	ErrOutOfRange = errors.New("udf: block outside the volume")
	// ErrLoop means a structure refers back to itself: a directory reached a
	// second time during a walk, or a chain of descriptors that loops.
	ErrLoop = errors.New("udf: loop")
	// ErrTooDeep means a directory exceeds Options.MaxDepth.
	ErrTooDeep = errors.New("udf: directories nested too deep")
	// ErrTooManyEntries means a walk exceeded Options.MaxEntries.
	ErrTooManyEntries = errors.New("udf: too many entries")
	// ErrNotDirectory means ReadDir was called on a file.
	ErrNotDirectory = errors.New("udf: not a directory")
	// ErrIsDirectory means OpenFile was called on a directory.
	ErrIsDirectory = errors.New("udf: is a directory")
	// ErrNotStream means OpenStream was called on an entry that is not a
	// named stream.
	ErrNotStream = errors.New("udf: not a named stream")
	// ErrCorrupt means a structure is malformed beyond use.
	ErrCorrupt = errors.New("udf: corrupt structure")
)

// Options configures Open. The zero value (or a nil *Options) is valid: it
// detects the block size, reads the whole medium as one session and applies
// the default limits.
type Options struct {
	// BlockSize is the logical block (sector) size. 0 detects it, trying
	// 2048, 512, 1024 and 4096 bytes in that order.
	BlockSize int
	// SessionStart is the first block of the last session of a multisession
	// disc: its anchor (SessionStart+256) and volume recognition sequence
	// are read. 0 is the start of the medium.
	SessionStart uint32
	// LastBlock is the last recorded block of the medium (or of the last
	// session); the anchors at LastBlock and LastBlock-256 and the virtual
	// allocation table of a write-once disc are searched there. 0 derives
	// it from the source size.
	LastBlock uint32

	// MaxDepth bounds directory nesting during Walk (default 255).
	MaxDepth int
	// MaxEntries bounds the entries visited by one Walk, and the entries
	// read from one directory (default 1<<24).
	MaxEntries int
	// MaxDirSize bounds the bytes read for one directory (default 64 MiB).
	MaxDirSize int64
	// MaxWarnings bounds the warnings kept (default 10000).
	MaxWarnings int
	// MaxVATSearch bounds the blocks searched backwards from LastBlock for
	// the virtual allocation table of a write-once volume (default 2048).
	MaxVATSearch int
}

func (o *Options) withDefaults() Options {
	var r Options
	if o != nil {
		r = *o
	}
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&r.MaxDepth, 255)
	def(&r.MaxEntries, 1<<24)
	def(&r.MaxWarnings, 10000)
	def(&r.MaxVATSearch, 2048)
	if r.MaxDirSize <= 0 {
		r.MaxDirSize = 64 << 20
	}
	return r
}

// Warning is a recoverable irregularity found while reading the volume.
type Warning struct {
	// Path is the entry concerned ("/" for the root), or "" for volume-level
	// warnings.
	Path    string
	Message string
}

func (w Warning) String() string {
	if w.Path == "" {
		return w.Message
	}
	return w.Path + ": " + w.Message
}

// Partition map types (PartitionInfo.Type).
const (
	PartitionPhysical = "physical"
	PartitionVirtual  = "virtual"
	PartitionSparable = "sparable"
	PartitionMetadata = "metadata"
	PartitionUnknown  = "unknown"
)

// Partition access types (PartitionInfo.AccessType).
const (
	AccessUnspecified  = 0
	AccessReadOnly     = 1
	AccessWriteOnce    = 2
	AccessRewritable   = 3
	AccessOverwritable = 4
)

// PartitionInfo describes one partition map of the logical volume, in map
// order (a long allocation descriptor's partition reference is an index
// into this list).
type PartitionInfo struct {
	// Type is PartitionPhysical, PartitionVirtual, PartitionSparable,
	// PartitionMetadata or PartitionUnknown.
	Type string
	// Identifier is a type 2 map's entity identifier, e.g. "*UDF Virtual
	// Partition"; "" for a type 1 map.
	Identifier     string
	Number         uint16
	VolumeSequence uint16
	// Start and Length are the partition descriptor's extent in blocks
	// (for virtual and metadata maps: the partition they live in).
	Start, Length uint32
	// AccessType is the partition descriptor's access type (Access*).
	AccessType uint32
	// PacketLength is a sparable partition's packet length in blocks, and
	// SparedPackets the number of packets its sparing table remaps.
	PacketLength  uint32
	SparedPackets int
	// Error is set when the map cannot be used (no partition descriptor,
	// no virtual allocation table, unreadable metadata file, ...).
	Error string
}

// VolumeInfo is the volume-level information of a UDF volume. Identifiers
// are decoded from OSTA CS0 and trimmed; implementation and application
// identifiers are entity identifiers (regid) such as "*Apple Mac OS X UDF FS".
type VolumeInfo struct {
	// Revision is the UDF revision as recorded, 0x0102 for 1.02 through
	// 0x0260 for 2.60: the logical volume's domain identifier suffix, else
	// the file set's, else the integrity descriptor's minimum read
	// revision. 0 if unknown.
	Revision uint16
	// MinReadRevision, MinWriteRevision and MaxWriteRevision come from the
	// logical volume integrity descriptor (or a UDF 2.00+ virtual
	// allocation table header).
	MinReadRevision, MinWriteRevision, MaxWriteRevision uint16
	// NSR is 2 or 3 for an NSR02/NSR03 volume recognition descriptor, 0
	// when none was found.
	NSR int

	VolumeID        string // primary volume descriptor's volume identifier
	VolumeSetID     string
	LogicalVolumeID string // logical volume descriptor (or VAT header)
	FileSetID       string
	CopyrightFile   string // file set descriptor's copyright file identifier
	AbstractFile    string
	// ImplementationID is the logical volume descriptor's implementation
	// identifier, else the primary volume descriptor's.
	ImplementationID string
	// IntegrityImplementationID identifies the implementation that last
	// wrote the volume (logical volume integrity descriptor).
	IntegrityImplementationID string
	ApplicationID             string
	DomainID                  string // e.g. "*OSTA UDF Compliant"

	Recorded        *time.Time // primary volume descriptor recording time
	FileSetRecorded *time.Time
	IntegrityTime   *time.Time

	// NumFiles and NumDirectories are the counts recorded in the integrity
	// descriptor (or VAT header); HasCounts says whether they were.
	NumFiles, NumDirectories uint32
	HasCounts                bool
	// IntegrityOpen is set when the last integrity descriptor is open (the
	// volume was not closed cleanly; normal for write-once media).
	IntegrityOpen bool
	// NextUniqueID is the integrity descriptor's next unique ID.
	NextUniqueID uint64

	Partitions []PartitionInfo
	// VolumeSpaceBlocks is the number of blocks of the medium as read
	// (LastBlock+1), 0 when unknown.
	VolumeSpaceBlocks uint32
	// HasSystemStreams is set when the file set records a system stream
	// directory (see Volume.SystemStreams).
	HasSystemStreams bool
}

// ICB locates an information control block (a file entry): a block in the
// partition with the given partition reference number (an index into
// VolumeInfo.Partitions). Entries with equal ICBs are hard links to the
// same file.
type ICB struct {
	Partition uint16
	Block     uint32
}

// Times holds a file entry's timestamps; nil when unset. The time zone of
// each is the offset the timestamp records (UTC when it records none).
type Times struct {
	Access       *time.Time
	Modification *time.Time
	// Attribute is the time of the last attribute change.
	Attribute *time.Time
	// Creation is recorded by extended file entries, or by a file times
	// extended attribute.
	Creation *time.Time
}

// Extended attribute types (ExtendedAttribute.Type).
const (
	EACharSetInfo          = 1
	EAAlternatePermissions = 3
	EAFileTimes            = 5
	EAInformationTimes     = 6
	EADeviceSpecification  = 12
	EAImplementationUse    = 2048
	EAApplicationUse       = 65536
)

// ExtendedAttribute is one extended attribute of a file, from its file
// entry's extended attribute space or its extended attribute file.
type ExtendedAttribute struct {
	Type    uint32
	Subtype byte
	// ImplementationID is the entity identifier of an implementation-use
	// or application-use attribute, e.g. "*UDF Mac FinderInfo".
	ImplementationID string
	// Data is the attribute's body after its 12-byte header; for
	// implementation-use and application-use attributes, after their length
	// and identifier fields (the implementation use bytes).
	Data []byte
	// Raw is the whole attribute as recorded, header included.
	Raw []byte
}

// File types (Entry.FileType), from the ICB tag.
const (
	FileTypeUnspecified        = 0
	FileTypeUnallocatedSpace   = 1
	FileTypePartitionIntegrity = 2
	FileTypeIndirect           = 3
	FileTypeDirectory          = 4
	FileTypeRegular            = 5
	FileTypeBlockDevice        = 6
	FileTypeCharDevice         = 7
	FileTypeExtendedAttributes = 8
	FileTypeFIFO               = 9
	FileTypeSocket             = 10
	FileTypeTerminal           = 11
	FileTypeSymlink            = 12
	FileTypeStreamDirectory    = 13
	FileTypeVAT                = 248
	FileTypeRealTime           = 249
	FileTypeMetadata           = 250
	FileTypeMetadataMirror     = 251
	FileTypeMetadataBitmap     = 252
)

// ICB flags (Entry.ICBFlags), above the allocation descriptor type bits.
const (
	ICBFlagSorted         = 1 << 3
	ICBFlagNonRelocatable = 1 << 4
	ICBFlagArchive        = 1 << 5
	ICBFlagSetuid         = 1 << 6
	ICBFlagSetgid         = 1 << 7
	ICBFlagSticky         = 1 << 8
	ICBFlagContiguous     = 1 << 9
	ICBFlagSystem         = 1 << 10
	ICBFlagTransformed    = 1 << 11
	ICBFlagMultiVersions  = 1 << 12
	ICBFlagStream         = 1 << 13
)

// Permission bits (Entry.Permissions): execute, write, read, change
// attributes and delete for other (bits 0-4), group (5-9) and owner (10-14).
const (
	PermOtherExec   = 1 << 0
	PermOtherWrite  = 1 << 1
	PermOtherRead   = 1 << 2
	PermOtherChAttr = 1 << 3
	PermOtherDelete = 1 << 4
	PermGroupExec   = 1 << 5
	PermGroupWrite  = 1 << 6
	PermGroupRead   = 1 << 7
	PermGroupChAttr = 1 << 8
	PermGroupDelete = 1 << 9
	PermOwnerExec   = 1 << 10
	PermOwnerWrite  = 1 << 11
	PermOwnerRead   = 1 << 12
	PermOwnerChAttr = 1 << 13
	PermOwnerDelete = 1 << 14
)

// File identifier characteristics (Entry.Characteristics).
const (
	CharHidden    = 1 << 0
	CharDirectory = 1 << 1
	CharDeleted   = 1 << 2
	CharParent    = 1 << 3
	CharMetadata  = 1 << 4
)

// Entry is a file, directory, symbolic link, device or named stream.
//
// Several directory entries may refer to the same file entry (hard links):
// each is a separate Entry with the same ICB.
type Entry struct {
	// Name is the CS0-decoded file identifier; the root's Name is "".
	Name string
	// Path is the "/"-joined path from the root ("/" for the root); a named
	// stream's Path is its owner's Path, ':' and the stream's Name. Names
	// that cannot appear in a path ("", ".", "..", or containing '/' or
	// NUL) are made safe in Path, with a warning.
	Path string
	// RawName is the file identifier exactly as recorded (compression ID
	// byte included).
	RawName []byte

	IsDir bool
	// Size is the information length (the file's data size).
	Size int64
	// FileType is the ICB tag's file type (FileType*).
	FileType byte

	// Characteristics is the file identifier's characteristics byte; Hidden,
	// Deleted and Parent decode it. ReadDir never returns deleted or parent
	// entries.
	Characteristics       byte
	Hidden, Deleted       bool
	Parent                bool
	FileVersion           uint16
	ICB                   ICB
	ICBFlags              uint16
	UniqueID              uint64
	LinkCount             uint16
	ImplementationID      string // the file entry's implementation identifier
	ExtendedFileEntry     bool
	LogicalBlocksRecorded uint64

	// UID and GID are the owner and group; HasUID/HasGID are false when the
	// entry records them as unspecified (0xFFFFFFFF).
	UID, GID       uint32
	HasUID, HasGID bool
	// Permissions holds the raw UDF permission bits (Perm*).
	Permissions uint32
	Times       Times

	// SymlinkTarget is the target of a symbolic link (FileTypeSymlink),
	// decoded from its path components.
	SymlinkTarget string
	IsSymlink     bool

	// DeviceMajor and DeviceMinor come from a device specification
	// extended attribute (block and character devices).
	DeviceMajor, DeviceMinor uint32
	HasDevice                bool

	// Streams lists the named streams of the entry (extended file entries
	// with a stream directory, UDF 2.00+), such as extended attributes
	// written by macOS ("com.apple.ResourceFork"). They are not walked as
	// children; read them with OpenStream.
	Streams []*Entry
	// Stream is set on a named stream entry.
	Stream bool

	// ExtendedAttributes lists every extended attribute recorded.
	ExtendedAttributes []ExtendedAttribute
	// MacFinderInfo is the 32 bytes of Finder information (FInfo/DInfo
	// followed by FXInfo/DXInfo, the layout of com.apple.FinderInfo) of a
	// "*UDF Mac FinderInfo" extended attribute.
	MacFinderInfo []byte

	// Sparse is set when part of the file is not recorded (allocated but
	// unrecorded or unallocated extents, or an information length beyond
	// the allocation descriptors); those bytes read as zeros.
	Sparse bool
	// Embedded is set when the data is recorded inside the file entry.
	Embedded bool

	vol  *Volume
	loc  addr     // location of the direct entry
	data *content // nil when the file entry could not be read
	err  error    // why the file entry could not be read
}

// Mode returns the entry's type and permissions: the UDF owner, group and
// other read/write/execute bits (the change-attribute and delete bits have
// no equivalent), setuid, setgid and sticky from the ICB flags, and the
// file type (directory, symbolic link, block or character device, FIFO,
// socket).
func (e *Entry) Mode() fs.FileMode {
	p := e.Permissions
	var m fs.FileMode
	for _, b := range [...]struct {
		udf  uint32
		mode fs.FileMode
	}{
		{PermOwnerRead, 0o400}, {PermOwnerWrite, 0o200}, {PermOwnerExec, 0o100},
		{PermGroupRead, 0o040}, {PermGroupWrite, 0o020}, {PermGroupExec, 0o010},
		{PermOtherRead, 0o004}, {PermOtherWrite, 0o002}, {PermOtherExec, 0o001},
	} {
		if p&b.udf != 0 {
			m |= b.mode
		}
	}
	if e.ICBFlags&ICBFlagSetuid != 0 {
		m |= fs.ModeSetuid
	}
	if e.ICBFlags&ICBFlagSetgid != 0 {
		m |= fs.ModeSetgid
	}
	if e.ICBFlags&ICBFlagSticky != 0 {
		m |= fs.ModeSticky
	}
	switch {
	case e.IsDir:
		m |= fs.ModeDir
	case e.IsSymlink:
		m |= fs.ModeSymlink
	default:
		switch e.FileType {
		case FileTypeBlockDevice:
			m |= fs.ModeDevice
		case FileTypeCharDevice:
			m |= fs.ModeDevice | fs.ModeCharDevice
		case FileTypeFIFO:
			m |= fs.ModeNamedPipe
		case FileTypeSocket:
			m |= fs.ModeSocket
		}
	}
	return m
}

// ModTime returns the modification time, or the zero time when unset.
func (e *Entry) ModTime() time.Time {
	if e.Times.Modification != nil {
		return *e.Times.Modification
	}
	return time.Time{}
}
