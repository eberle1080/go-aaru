package aaru

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"

	"lukechampine.com/blake3"

	"github.com/eberle1080/go-aaru/media"
	"github.com/eberle1080/go-aaru/spamsum"
)

// CreateParams describes a new image.
type CreateParams struct {
	// MediaType is the type of the imaged medium.
	MediaType media.Type
	// SectorSize is the (biggest) user data sector size in bytes, e.g. 512
	// for hard disks or 2048 for CD-ROM.
	SectorSize uint32
	// UserSectors is the number of user data sectors. For tapes this may
	// be an estimate.
	UserSectors uint64
	// NegativeSectors and OverflowSectors are the number of sectors before
	// sector 0 (e.g. optical lead-in) and after the user area (e.g. lead-out).
	// They are ignored for tapes.
	NegativeSectors uint64
	OverflowSectors uint64
	// Application is the name of the creating application (UTF-8, at most
	// 64 bytes).
	Application string
	// ApplicationMajorVersion and ApplicationMinorVersion are the version of
	// the creating application.
	ApplicationMajorVersion uint8
	ApplicationMinorVersion uint8
	// Tape creates a tape image: sectors are tracked in a sparse table that
	// grows as blocks are written, and the sector counts are not bounded by a
	// preallocated table.
	Tape bool
}

// Create creates a new AaruFormat image at path, overwriting any existing
// file, and returns it opened for writing. opts selects
// compression, deduplication, checksums and the table layout; nil means
// DefaultOptions().
//
// Block media get a version 2 deduplication table: a single-level table for
// images of fewer than 8388608 sectors (negative + user + overflow), a
// two-level table (table shift 22) otherwise, unless Options.TableShift is
// set explicitly. Tape images use a sparse table. When all sector counts are
// zero (and Tape is false) the image is flux-only: it has no user data table
// and the sector writing methods return ErrUserDataNotPresent.
//
// The image must be finalized with Close.
func Create(path string, p CreateParams, opts *Options) (*Image, error) {
	o := DefaultOptions()
	if opts != nil {
		o = *opts
	}

	if o.DataShift > maxDataShift || o.BlockAlignment > maxBlockAlignment {
		return nil, fmt.Errorf("data shift %d / block alignment %d out of range: %w", o.DataShift, o.BlockAlignment,
			ErrInvalidOptions)
	}
	if len(p.Application) > HeaderAppNameLen {
		return nil, fmt.Errorf("application name is %d bytes long (maximum %d): %w", len(p.Application),
			HeaderAppNameLen, ErrInvalidAppNameLength)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w: %w", path, ErrCannotCreateFile, err)
	}

	img, err := newWritableImage(p, o)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	img.r = f
	img.w = f
	img.closer = f
	img.size = 0
	return img, nil
}

// newWritableImage initializes an image for writing without any backing
// storage.
func newWritableImage(p CreateParams, o Options) (*Image, error) {
	img := &Image{
		mediaTags:        make(map[MediaTagType][]byte),
		blockHeaderCache: newLRUCache(maxHeaderCacheSize),
		blockCache:       newLRUCache(maxCacheSize),
		indexEntries:     make([]IndexEntry, 0, 64),
	}

	now := nowFiletime()
	h := &img.header
	h.Identifier = AaruMagic
	copy(h.Application[:], p.Application)
	h.ImageMajorVersion = FormatVersionV2
	h.ImageMinorVersion = 0
	h.ApplicationMajorVersion = p.ApplicationMajorVersion
	h.ApplicationMinorVersion = p.ApplicationMinorVersion
	h.MediaType = uint32(p.MediaType)
	h.IndexOffset = 0
	h.CreationTime = now
	h.LastWrittenTime = now
	if _, err := rand.Read(h.GUID[:]); err != nil {
		return nil, fmt.Errorf("generating image GUID: %w", err)
	}

	app := p.Application
	if len(app) > HeaderAppNameLen-1 {
		app = app[:HeaderAppNameLen-1]
	}
	if i := strings.IndexByte(app, 0); i >= 0 {
		app = app[:i]
	}
	img.info = ImageInfo{
		Application:          app,
		ApplicationVersion:   fmt.Sprintf("%d.%d", h.ApplicationMajorVersion, h.ApplicationMinorVersion),
		Version:              fmt.Sprintf("%d.%d", h.ImageMajorVersion, h.ImageMinorVersion),
		MediaType:            p.MediaType,
		ImageSize:            0,
		CreationTime:         FiletimeToTime(h.CreationTime),
		LastModificationTime: FiletimeToTime(h.LastWrittenTime),
		MetadataMediaType:    p.MediaType.MetadataType(),
		SectorSize:           p.SectorSize,
		Sectors:              p.UserSectors,
	}

	dh := &img.userDataDDTHeader
	fluxOnly := !p.Tape && p.UserSectors == 0 && p.NegativeSectors == 0 && p.OverflowSectors == 0

	switch {
	case fluxOnly:
		// Flux-only images carry no sector data: no DDT, blocks start right
		// after the header like on tapes.
		img.noUserDataDDT = true
		dh.BlockAlignmentShift = o.BlockAlignment
		h.BlockAlignmentShift = o.BlockAlignment
		dh.DataShift = o.DataShift
		img.nextBlockPosition = alignUp(headerSize, o.BlockAlignment)
	case !p.Tape:
		img.ddtVersion = 2
		img.inMemoryDDT = true
		dh.Identifier = BlockTypeDeDuplicationTable2
		dh.Type = DataTypeUserData
		dh.Compression = CompressionNone
		dh.TableLevel = 0
		dh.PreviousLevelOffset = 0
		dh.Negative = uint32(p.NegativeSectors)
		dh.Blocks = p.UserSectors + p.OverflowSectors + p.NegativeSectors
		dh.Overflow = uint32(p.OverflowSectors)
		dh.Start = 0
		dh.BlockAlignmentShift = o.BlockAlignment
		h.BlockAlignmentShift = o.BlockAlignment
		dh.DataShift = o.DataShift

		if !o.explicitTableShift() {
			if dh.Blocks < 8388608 {
				dh.TableShift = 0
			} else {
				dh.TableShift = 22
			}
		} else if o.TableShift > 0 {
			dh.TableShift = uint8(o.TableShift)
		} else {
			dh.TableShift = 0
		}
		if dh.TableShift > 0 {
			dh.Levels = 2
		} else {
			dh.Levels = 1
		}
		if dh.TableShift >= 63 {
			dh.TableShift = 62
		}

		perEntry := uint64(1) << dh.TableShift
		dh.Entries = dh.Blocks / perEntry
		if dh.Blocks%perEntry != 0 || dh.Entries == 0 {
			dh.Entries++
		}
		if dh.Entries > maxDDTEntries {
			return nil, fmt.Errorf("deduplication table of %d entries is too big: %w", dh.Entries,
				ErrNotEnoughMemory)
		}
		img.userDataDDT2 = make([]uint64, dh.Entries)

		// The primary table goes right after the header, block aligned.
		img.primaryDDTOffset = alignUp(headerSize, dh.BlockAlignmentShift)
		if dh.TableShift > 0 {
			img.nextBlockPosition = alignUp(img.primaryDDTOffset+ddtHeader2Size+dh.Entries*8,
				dh.BlockAlignmentShift)
		} else {
			// A single-level table is written at the end on close.
			img.nextBlockPosition = img.primaryDDTOffset
		}
	default: // tape
		dh.BlockAlignmentShift = o.BlockAlignment
		h.BlockAlignmentShift = o.BlockAlignment
		dh.DataShift = o.DataShift
		img.nextBlockPosition = alignUp(headerSize, o.BlockAlignment)
		img.isTape = true
		img.tapeDDT = make(map[uint64]uint64)
	}

	img.options = o
	if o.Deduplicate && !img.noUserDataDDT {
		img.sectorHashMap = make(map[uint64]uint64, min(dh.Blocks/4, 1<<16))
	}
	img.rewound = false
	img.lastWrittenBlock = 0

	if o.MD5 {
		img.calculatingMD5 = true
		img.md5Hasher = md5.New()
	}
	if o.SHA1 {
		img.calculatingSHA1 = true
		img.sha1Hasher = sha1.New()
	}
	if o.SHA256 {
		img.calculatingSHA256 = true
		img.sha256Hasher = sha256.New()
	}
	if o.SpamSum {
		img.calculatingSpamSum = true
		img.spamsumHasher = spamsum.New()
	}
	if o.Blake3 {
		img.calculatingBlake3 = true
		img.blake3Hasher = blake3.New(32, nil)
	}

	img.writable = true
	// Every structure is (re)written on close for new images; flux-only
	// images have no DDT to serialize.
	img.dirty.all(!img.noUserDataDDT)
	return img, nil
}

// ErrInvalidOptions is returned by Create for options the format cannot
// represent (shifts that overflow its arithmetic).
var ErrInvalidOptions = errors.New("aaru: invalid image options")

// Limits of the shift options: blocks hold at most 2^30 sectors and are
// aligned to at most 2^62 bytes.
const (
	maxDataShift      = 30
	maxBlockAlignment = 62
)

// maxDDTEntries bounds the in-memory primary/single-level table (2 GiB).
const maxDDTEntries = 1 << 28
