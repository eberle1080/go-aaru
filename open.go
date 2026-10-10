package aaru

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Open opens the AaruFormat image at path for reading. The returned Image
// must be closed with Close.
//
// Errors opening the file are returned as is (e.g. a *fs.PathError wrapping
// fs.ErrNotExist); format problems are reported with the package sentinel
// errors (ErrNotAaruFormat, ErrFileTooSmall, ErrIncompatibleVersion,
// ErrIncompatibleFeatures, ErrCannotReadIndex, ...), possibly wrapped.
func Open(path string) (*Image, error) {
	return openFile(path, false)
}

// OpenReader opens an AaruFormat image stored in r, which holds size bytes,
// for reading. Close does not close r.
func OpenReader(r io.ReaderAt, size int64) (*Image, error) {
	if r == nil {
		return nil, fmt.Errorf("aaru: nil reader: %w", ErrNotAaruFormat)
	}
	if size < 0 {
		return nil, fmt.Errorf("aaru: negative image size %d: %w", size, ErrFileTooSmall)
	}
	return openImage(r, nil, nil, size, false)
}

// openFile opens path and parses the image (everything up to the resume-mode
// specific setup). When writable is true the file is opened
// read-write and the version/feature checks for writing are applied; the
// returned Image then has img.w set (the file) but img.writable still false:
// Resume completes the writable setup.
func openFile(path string, writable bool) (*Image, error) {
	if path == "" {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}
	flag := os.O_RDONLY
	if writable {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	var w io.WriterAt
	if writable {
		w = f
	}
	img, err := openImage(f, w, f, st.Size(), writable)
	if err != nil {
		f.Close()
		return nil, err
	}
	return img, nil
}

// decodeImageHeader decodes a header from b, which holds either a version 1
// (104 bytes) or a version 2 (147 bytes) header; missing trailing fields are
// zero.
func decodeImageHeader(b []byte) Header {
	var full [headerSize]byte
	copy(full[:], b)
	h, _ := unmarshal[Header](full[:]) // cannot fail: full has the exact size
	return h
}

// readRecoveryHeader tries to obtain the header from the recovery footer at
// the end of the file (written by images with erasure coding). It reports
// whether a valid backup header was found.
func (img *Image) readRecoveryHeader() (Header, bool) {
	if img.size < recoveryFooterSize {
		return Header{}, false
	}
	var fb [recoveryFooterSize]byte
	if err := img.readFullAt(fb[:], uint64(img.size)-recoveryFooterSize); err != nil {
		return Header{}, false
	}
	footer, err := unmarshal[RecoveryFooter](fb[:])
	if err != nil || footer.FooterMagic != RecoveryFooterMagic {
		return Header{}, false
	}
	const backupOff = 24 // ECMBOffset, ECMBLength, HeaderCRC64
	if crc64.Checksum(fb[backupOff:backupOff+headerSize]) != footer.HeaderCRC64 {
		return Header{}, false
	}
	b := footer.BackupHeader
	if b.Identifier != DICMagic && b.Identifier != AaruMagic {
		return Header{}, false
	}
	return b, true
}

// applicationName decodes the creator name from the header: UTF-16LE (low
// bytes only) for version 1 images, UTF-8 for version 2.
func applicationName(h *Header) string {
	if h.ImageMajorVersion >= FormatVersionV2 {
		name := h.Application[:HeaderAppNameLen-1]
		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		return string(name)
	}
	name := make([]byte, 0, HeaderAppNameLen/2)
	for j := 0; j < HeaderAppNameLen && len(name) < HeaderAppNameLen-1; j += 2 {
		if h.Application[j] == 0 {
			break
		}
		name = append(name, h.Application[j])
	}
	return string(name)
}

// openImage parses the image held in r (everything up to the resume-mode
// setup).
func openImage(r io.ReaderAt, w io.WriterAt, closer io.Closer, size int64, writable bool) (*Image, error) {
	img := &Image{r: r, w: w, closer: closer, size: size}

	// Header (version 1 layout first).
	var hb [headerSize]byte
	if err := img.readFullAt(hb[:headerV1Size], 0); err != nil {
		return nil, fmt.Errorf("reading header: %w", ErrFileTooSmall)
	}
	img.header = decodeImageHeader(hb[:headerV1Size])

	recovered := false
	if img.header.Identifier != DICMagic && img.header.Identifier != AaruMagic {
		// Primary header corrupt: try the backup header in the recovery footer.
		h, ok := img.readRecoveryHeader()
		if !ok {
			return nil, fmt.Errorf("incorrect identifier %#016x: %w", img.header.Identifier, ErrNotAaruFormat)
		}
		img.header = h
		recovered = true
	}

	if img.header.ImageMajorVersion < FormatVersionV2 && writable {
		return nil, fmt.Errorf("cannot write to version %d images: %w", img.header.ImageMajorVersion, ErrIncompatibleVersion)
	}

	// Version 2 header. (After recovering the backup header, the recovered
	// header is kept.)
	if img.header.ImageMajorVersion >= FormatVersionV2 && !recovered {
		if err := img.readFullAt(hb[:], 0); err != nil {
			return nil, fmt.Errorf("reading version 2 header: %w", ErrFileTooSmall)
		}
		img.header = decodeImageHeader(hb[:])
	}

	if img.header.ImageMajorVersion > FormatVersion {
		return nil, fmt.Errorf("image version %d.%d, maximum supported is %d.0: %w",
			img.header.ImageMajorVersion, img.header.ImageMinorVersion, FormatVersion, ErrIncompatibleVersion)
	}

	if img.header.ImageMajorVersion >= FormatVersionV2 {
		if unknown := img.header.FeatureIncompatible &^ knownIncompatFeatures; unknown != 0 {
			return nil, fmt.Errorf("unsupported incompatible features %#016x: %w", unknown, ErrIncompatibleFeatures)
		}
		if unknown := img.header.FeatureCompatibleRO &^ knownROCompatFeatures; unknown != 0 && writable {
			return nil, fmt.Errorf("unsupported read-only compatible features %#016x: %w", unknown, ErrIncompatibleFeatures)
		}
	}

	img.info.Application = applicationName(&img.header)
	img.info.ApplicationVersion = strconv.Itoa(int(img.header.ApplicationMajorVersion)) + "." +
		strconv.Itoa(int(img.header.ApplicationMinorVersion))
	img.info.Version = strconv.Itoa(int(img.header.ImageMajorVersion)) + "." +
		strconv.Itoa(int(img.header.ImageMinorVersion))
	img.info.MediaType = media.Type(img.header.MediaType)

	// Index.
	entries, err := img.readIndex()
	if err != nil {
		return nil, err
	}
	img.indexEntries = entries

	// Load the erasure coding recovery map before processing the blocks, so
	// damaged blocks can be recovered while they are processed.
	img.ecLoadECMB()

	foundUserDataDDT := false
	hasFluxBlock := false
	img.info.ImageSize = 0
	for _, e := range entries {
		if e.Offset > math.MaxInt64 {
			continue // cannot seek there
		}
		switch e.BlockType {
		case BlockTypeDataBlock:
			if e.DataType == DataTypeUserData && img.header.BiggestSectorSize > 0 {
				break
			}
			if err := img.processDataBlock(e); err != nil {
				return nil, err
			}
		case BlockTypeDeDuplicationTable:
			if err := img.processDDTv1(e, &foundUserDataDDT); err != nil {
				return nil, err
			}
		case BlockTypeDeDuplicationTable2:
			if err := img.processDDTv2(e, &foundUserDataDDT); err != nil {
				return nil, err
			}
			switch e.DataType {
			case DataTypeCdSectorPrefix, DataTypeCdSectorPrefixCorrected:
				img.readableSectorTags[SectorTagCdSync] = true
				img.readableSectorTags[SectorTagCdHeader] = true
			case DataTypeCdSectorSuffix, DataTypeCdSectorSuffixCorrected:
				img.readableSectorTags[SectorTagCdSubHeader] = true
				img.readableSectorTags[SectorTagCdEcc] = true
				img.readableSectorTags[SectorTagCdEccP] = true
				img.readableSectorTags[SectorTagCdEccQ] = true
				img.readableSectorTags[SectorTagCdEdc] = true
			}
		case BlockTypeGeometryBlock:
			_ = img.processGeometryBlock(e)
		case BlockTypeMetadataBlock:
			_ = img.processMetadataBlock(e)
		case BlockTypeTracksBlock:
			img.processTracksBlock(e)
		case BlockTypeCicmBlock:
			_ = img.processCICMBlock(e)
		case BlockTypeAaruMetadataJsonBlock:
			_ = img.processJSONBlock(e)
		case BlockTypeDataPositionMeasurementBlock:
			_ = img.processDPMBlock(e)
		case BlockTypeDumpHardwareBlock:
			_ = img.processDumpHardwareBlock(e)
		case BlockTypeChecksumBlock:
			_ = img.processChecksumBlock(e)
		case BlockTypeTapeFileBlock:
			_ = img.processTapeFilesBlock(e)
		case BlockTypeTapePartitionBlock:
			_ = img.processTapePartitionsBlock(e)
		case BlockTypeFluxDataBlock:
			// Flux entries are loaded lazily; only note the block.
			hasFluxBlock = true
			if img.fluxOffset == 0 {
				img.fluxOffset = e.Offset
			}
		}
	}

	if !foundUserDataDDT {
		if !hasFluxBlock {
			return nil, fmt.Errorf("no user data deduplication table or flux data: %w", ErrCannotReadIndex)
		}
		// Flux-only image: sector APIs return ErrUserDataNotPresent.
		img.noUserDataDDT = true
	}

	if img.header.BiggestSectorSize != 0 {
		img.info.SectorSize = uint32(img.header.BiggestSectorSize)
	}

	img.info.CreationTime = FiletimeToTime(img.header.CreationTime)
	img.info.LastModificationTime = FiletimeToTime(img.header.LastWrittenTime)
	img.info.MetadataMediaType = media.Type(img.header.MediaType).MetadataType()

	if img.geometry.Identifier != BlockTypeGeometryBlock && img.info.MetadataMediaType == media.BlockMedia {
		img.cylinders = uint32(img.info.Sectors / 16 / 63)
		img.heads = 16
		img.sectorsPerTrack = 63
	}

	// Both caches are bounded by the bytes they hold.
	img.blockHeaderCache = newLRUCache(maxHeaderCacheSize)
	img.blockCache = newLRUCache(maxCacheSize)

	return img, nil
}

// readIndex reads the index (v1, v2 or v3, including IDX3 subindexes) at
// header.IndexOffset.
func (img *Image) readIndex() ([]IndexEntry, error) {
	off := img.header.IndexOffset
	if off > math.MaxInt64 {
		return nil, fmt.Errorf("index offset %d: %w", off, ErrCannotReadIndex)
	}
	var sig [4]byte
	if err := img.readFullAt(sig[:], off); err != nil {
		return nil, fmt.Errorf("reading index signature at %d: %w", off, ErrCannotReadIndex)
	}
	var entries []IndexEntry
	var err error
	switch BlockType(binary.LittleEndian.Uint32(sig[:])) {
	case BlockTypeIndexBlock:
		entries, err = img.processIndexV1()
	case BlockTypeIndexBlock2:
		entries, err = img.processIndexV2()
	case BlockTypeIndexBlock3:
		entries, err = img.processIndexV3()
	default:
		return nil, fmt.Errorf("incorrect index identifier %q: %w", sig[:], ErrCannotReadIndex)
	}
	if err != nil {
		return nil, fmt.Errorf("processing index: %w: %w", ErrCannotReadIndex, err)
	}
	return entries, nil
}
