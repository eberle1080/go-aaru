package aaru

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"unicode/utf16"
)

// This file implements the metadata accessors (getters, setters and clear
// functions), plus ReadableSectorTags and ReadableMediaTags.
//
// Metadata set on a writable image is stored on Close. Every metadata change
// made after Resume is persisted.

// MetadataField selects one of the descriptive strings stored in the
// metadata block (META).
type MetadataField int

// The metadata block strings, in on-disk order.
const (
	MetadataCreator               MetadataField = iota // Person or tool that created the image.
	MetadataComments                                   // Free-form comments.
	MetadataMediaTitle                                 // Title of the media.
	MetadataMediaManufacturer                          // Manufacturer of the media.
	MetadataMediaModel                                 // Model of the media.
	MetadataMediaSerialNumber                          // Serial number of the media.
	MetadataMediaBarcode                               // Barcode of the media.
	MetadataMediaPartNumber                            // Part number of the media.
	MetadataDriveManufacturer                          // Manufacturer of the drive used to dump the media.
	MetadataDriveModel                                 // Model of the drive.
	MetadataDriveSerialNumber                          // Serial number of the drive.
	MetadataDriveFirmwareRevision                      // Firmware revision of the drive.

	metadataFieldCount = iota
)

var metadataFieldNames = [metadataFieldCount]string{
	"Creator", "Comments", "MediaTitle", "MediaManufacturer", "MediaModel", "MediaSerialNumber",
	"MediaBarcode", "MediaPartNumber", "DriveManufacturer", "DriveModel", "DriveSerialNumber",
	"DriveFirmwareRevision",
}

// String returns the name of the field.
func (f MetadataField) String() string {
	if f < 0 || f >= metadataFieldCount {
		return "MetadataField(" + strconv.Itoa(int(f)) + ")"
	}
	return metadataFieldNames[f]
}

// errUnknownMetadataField is returned for MetadataField values out of range.
func errUnknownMetadataField(f MetadataField) error {
	return fmt.Errorf("unknown metadata field %d: %w", int(f), ErrMetadataNotPresent)
}

// EncodeUTF16 encodes s the way the metadata block stores strings:
// UTF-16LE followed by a NUL code unit (the length stored on disk includes
// the terminator). Invalid UTF-8 sequences are replaced by U+FFFD.
func EncodeUTF16(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2*len(units)+2)
	for _, u := range units {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return append(b, 0, 0)
}

// DecodeUTF16 decodes a UTF-16LE metadata string, stopping at the first NUL
// code unit (if any). A trailing odd byte is ignored; unpaired surrogates
// decode to U+FFFD.
func DecodeUTF16(b []byte) string {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}

// MetadataRaw returns the raw bytes stored for a metadata string, exactly as
// on disk (UTF-16LE, normally including the NUL terminator). It returns
// ErrMetadataNotPresent if the image has no metadata block or the string is
// absent.
func (img *Image) MetadataRaw(f MetadataField) ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if f < 0 || f >= metadataFieldCount {
		return nil, errUnknownMetadataField(f)
	}
	s := img.metadataStrings()[f]
	if img.metadataBlockHeader.Identifier != BlockTypeMetadataBlock || *s.data == nil || *s.length == 0 {
		return nil, fmt.Errorf("%v: %w", f, ErrMetadataNotPresent)
	}
	return bytes.Clone((*s.data)[:*s.length]), nil
}

// SetMetadataRaw stores data as the raw value of a metadata string. The
// bytes are opaque to the
// library; they should be UTF-16LE including a NUL terminator (see
// EncodeUTF16) for other tools to read them. The data is copied.
func (img *Image) SetMetadataRaw(f MetadataField, data []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if f < 0 || f >= metadataFieldCount {
		return errUnknownMetadataField(f)
	}
	if len(data) > math.MaxInt32 {
		return fmt.Errorf("%v of %d bytes: %w", f, len(data), ErrIncorrectDataSize)
	}
	s := img.metadataStrings()[f]
	img.metadataBlockHeader.Identifier = BlockTypeMetadataBlock
	*s.data = append(make([]byte, 0, len(data)), data...)
	*s.length = uint32(len(data))
	img.dirty.metadataBlock = true
	return nil
}

// ClearMetadata removes a metadata string. When nothing remains in the
// metadata block it
// is dropped altogether. Clearing an absent string is not an error.
func (img *Image) ClearMetadata(f MetadataField) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if f < 0 || f >= metadataFieldCount {
		return errUnknownMetadataField(f)
	}
	if img.metadataBlockHeader.Identifier != BlockTypeMetadataBlock {
		return nil
	}
	s := img.metadataStrings()[f]
	*s.data = nil
	*s.length = 0
	if img.metadataEmpty() {
		img.metadataBlockHeader.Identifier = 0
	}
	img.dirty.metadataBlock = true
	return nil
}

// MetadataString returns a metadata string decoded from UTF-16LE (see
// DecodeUTF16), or ErrMetadataNotPresent.
func (img *Image) MetadataString(f MetadataField) (string, error) {
	b, err := img.MetadataRaw(f)
	if err != nil {
		return "", err
	}
	return DecodeUTF16(b), nil
}

// SetMetadataString stores s as a metadata string, encoded as NUL
// terminated UTF-16LE (see EncodeUTF16), the encoding Aaru and other
// AaruFormat tools use.
func (img *Image) SetMetadataString(f MetadataField, s string) error {
	return img.SetMetadataRaw(f, EncodeUTF16(s))
}

// Creator returns the creator string.
func (img *Image) Creator() (string, error) { return img.MetadataString(MetadataCreator) }

// SetCreator sets the creator string.
func (img *Image) SetCreator(s string) error { return img.SetMetadataString(MetadataCreator, s) }

// ClearCreator removes the creator string.
func (img *Image) ClearCreator() error { return img.ClearMetadata(MetadataCreator) }

// Comments returns the comments.
func (img *Image) Comments() (string, error) { return img.MetadataString(MetadataComments) }

// SetComments sets the comments.
func (img *Image) SetComments(s string) error { return img.SetMetadataString(MetadataComments, s) }

// ClearComments removes the comments.
func (img *Image) ClearComments() error { return img.ClearMetadata(MetadataComments) }

// MediaTitle returns the media title.
func (img *Image) MediaTitle() (string, error) { return img.MetadataString(MetadataMediaTitle) }

// SetMediaTitle sets the media title.
func (img *Image) SetMediaTitle(s string) error { return img.SetMetadataString(MetadataMediaTitle, s) }

// ClearMediaTitle removes the media title.
func (img *Image) ClearMediaTitle() error { return img.ClearMetadata(MetadataMediaTitle) }

// MediaManufacturer returns the media manufacturer.
func (img *Image) MediaManufacturer() (string, error) {
	return img.MetadataString(MetadataMediaManufacturer)
}

// SetMediaManufacturer sets the media manufacturer.
func (img *Image) SetMediaManufacturer(s string) error {
	return img.SetMetadataString(MetadataMediaManufacturer, s)
}

// ClearMediaManufacturer removes the media manufacturer.
func (img *Image) ClearMediaManufacturer() error { return img.ClearMetadata(MetadataMediaManufacturer) }

// MediaModel returns the media model.
func (img *Image) MediaModel() (string, error) { return img.MetadataString(MetadataMediaModel) }

// SetMediaModel sets the media model.
func (img *Image) SetMediaModel(s string) error { return img.SetMetadataString(MetadataMediaModel, s) }

// ClearMediaModel removes the media model.
func (img *Image) ClearMediaModel() error { return img.ClearMetadata(MetadataMediaModel) }

// MediaSerialNumber returns the media serial number.
func (img *Image) MediaSerialNumber() (string, error) {
	return img.MetadataString(MetadataMediaSerialNumber)
}

// SetMediaSerialNumber sets the media serial number.
func (img *Image) SetMediaSerialNumber(s string) error {
	return img.SetMetadataString(MetadataMediaSerialNumber, s)
}

// ClearMediaSerialNumber removes the media serial number.
func (img *Image) ClearMediaSerialNumber() error { return img.ClearMetadata(MetadataMediaSerialNumber) }

// MediaBarcode returns the media barcode.
func (img *Image) MediaBarcode() (string, error) { return img.MetadataString(MetadataMediaBarcode) }

// SetMediaBarcode sets the media barcode.
func (img *Image) SetMediaBarcode(s string) error {
	return img.SetMetadataString(MetadataMediaBarcode, s)
}

// ClearMediaBarcode removes the media barcode.
func (img *Image) ClearMediaBarcode() error { return img.ClearMetadata(MetadataMediaBarcode) }

// MediaPartNumber returns the media part number.
func (img *Image) MediaPartNumber() (string, error) {
	return img.MetadataString(MetadataMediaPartNumber)
}

// SetMediaPartNumber sets the media part number.
func (img *Image) SetMediaPartNumber(s string) error {
	return img.SetMetadataString(MetadataMediaPartNumber, s)
}

// ClearMediaPartNumber removes the media part number.
func (img *Image) ClearMediaPartNumber() error { return img.ClearMetadata(MetadataMediaPartNumber) }

// DriveManufacturer returns the drive manufacturer.
func (img *Image) DriveManufacturer() (string, error) {
	return img.MetadataString(MetadataDriveManufacturer)
}

// SetDriveManufacturer sets the drive manufacturer.
func (img *Image) SetDriveManufacturer(s string) error {
	return img.SetMetadataString(MetadataDriveManufacturer, s)
}

// ClearDriveManufacturer removes the drive manufacturer.
func (img *Image) ClearDriveManufacturer() error { return img.ClearMetadata(MetadataDriveManufacturer) }

// DriveModel returns the drive model.
func (img *Image) DriveModel() (string, error) { return img.MetadataString(MetadataDriveModel) }

// SetDriveModel sets the drive model.
func (img *Image) SetDriveModel(s string) error { return img.SetMetadataString(MetadataDriveModel, s) }

// ClearDriveModel removes the drive model.
func (img *Image) ClearDriveModel() error { return img.ClearMetadata(MetadataDriveModel) }

// DriveSerialNumber returns the drive serial number.
func (img *Image) DriveSerialNumber() (string, error) {
	return img.MetadataString(MetadataDriveSerialNumber)
}

// SetDriveSerialNumber sets the drive serial number.
func (img *Image) SetDriveSerialNumber(s string) error {
	return img.SetMetadataString(MetadataDriveSerialNumber, s)
}

// ClearDriveSerialNumber removes the drive serial number.
func (img *Image) ClearDriveSerialNumber() error { return img.ClearMetadata(MetadataDriveSerialNumber) }

// DriveFirmwareRevision returns the drive firmware revision.
func (img *Image) DriveFirmwareRevision() (string, error) {
	return img.MetadataString(MetadataDriveFirmwareRevision)
}

// SetDriveFirmwareRevision sets the drive firmware revision.
func (img *Image) SetDriveFirmwareRevision(s string) error {
	return img.SetMetadataString(MetadataDriveFirmwareRevision, s)
}

// ClearDriveFirmwareRevision removes the drive firmware revision.
func (img *Image) ClearDriveFirmwareRevision() error {
	return img.ClearMetadata(MetadataDriveFirmwareRevision)
}

// MediaSequence returns the position of the media in a multi-media set and
// the number of media in the set. It returns
// ErrMetadataNotPresent if the image has no metadata block.
func (img *Image) MediaSequence() (sequence, lastSequence int32, err error) {
	if img.closed {
		return 0, 0, ErrClosed
	}
	h := &img.metadataBlockHeader
	if h.Identifier != BlockTypeMetadataBlock {
		return 0, 0, fmt.Errorf("no metadata block: %w", ErrMetadataNotPresent)
	}
	return h.MediaSequence, h.LastMediaSequence, nil
}

// SetMediaSequence sets the media sequence numbers.
func (img *Image) SetMediaSequence(sequence, lastSequence int32) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	h := &img.metadataBlockHeader
	h.Identifier = BlockTypeMetadataBlock
	h.MediaSequence = sequence
	h.LastMediaSequence = lastSequence
	img.dirty.metadataBlock = true
	return nil
}

// ClearMediaSequence resets the media sequence numbers to zero,
// dropping the metadata block when nothing
// else remains in it.
func (img *Image) ClearMediaSequence() error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	h := &img.metadataBlockHeader
	if h.Identifier != BlockTypeMetadataBlock {
		return nil
	}
	h.MediaSequence = 0
	h.LastMediaSequence = 0
	if img.metadataEmpty() {
		h.Identifier = 0
	}
	img.dirty.metadataBlock = true
	return nil
}

// Geometry returns the logical CHS geometry stored in the image.
// It returns ErrCannotReadBlock if the image has no geometry block (no
// default geometry is assumed for block media).
func (img *Image) Geometry() (cylinders, heads, sectorsPerTrack uint32, err error) {
	if img.closed {
		return 0, 0, 0, ErrClosed
	}
	g := &img.geometry
	if g.Identifier != BlockTypeGeometryBlock {
		return 0, 0, 0, fmt.Errorf("no geometry block: %w", ErrCannotReadBlock)
	}
	return g.Cylinders, g.Heads, g.SectorsPerTrack, nil
}

// SetGeometry sets the logical CHS geometry.
func (img *Image) SetGeometry(cylinders, heads, sectorsPerTrack uint32) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	img.geometry = GeometryBlockHeader{Identifier: BlockTypeGeometryBlock, Cylinders: cylinders, Heads: heads,
		SectorsPerTrack: sectorsPerTrack}
	img.cylinders = cylinders
	img.heads = heads
	img.sectorsPerTrack = sectorsPerTrack
	img.dirty.geometryBlock = true
	return nil
}

// CICMMetadata returns the CICM XML metadata, or
// ErrCannotReadBlock if the image has none. CICM metadata cannot be
// written; it is only read from existing images (and kept when they are
// resumed).
func (img *Image) CICMMetadata() ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if len(img.cicmBlock) == 0 {
		return nil, fmt.Errorf("no CICM metadata: %w", ErrCannotReadBlock)
	}
	return bytes.Clone(img.cicmBlock), nil
}

// AaruJSONMetadata returns the Aaru metadata JSON document,
// or ErrCannotReadBlock if the image has
// none.
func (img *Image) AaruJSONMetadata() ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if len(img.jsonBlock) == 0 {
		return nil, fmt.Errorf("no Aaru JSON metadata: %w", ErrCannotReadBlock)
	}
	return bytes.Clone(img.jsonBlock), nil
}

// SetAaruJSONMetadata stores the Aaru metadata JSON document.
// The bytes are stored as is (UTF-8 JSON,
// not validated). Setting an empty document removes it.
func (img *Image) SetAaruJSONMetadata(data []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if uint64(len(data)) > math.MaxUint32 {
		return fmt.Errorf("JSON metadata of %d bytes: %w", len(data), ErrIncorrectDataSize)
	}
	img.jsonBlock = bytes.Clone(data)
	img.dirty.jsonBlock = true
	return nil
}

// UserSectors returns the number of user data sectors (ImageInfo.Sectors),
// not counting negative or overflow sectors.
func (img *Image) UserSectors() uint64 { return img.info.Sectors }

// NegativeSectors returns the number of negative sectors (before sector 0).
func (img *Image) NegativeSectors() uint32 { return img.userDataDDTHeader.Negative }

// OverflowSectors returns the number of overflow sectors (after the user
// area).
func (img *Image) OverflowSectors() uint32 { return img.userDataDDTHeader.Overflow }

// ReadableSectorTags returns the sector tags that can be read from the image
// with ReadSectorTag, in ascending order.
func (img *Image) ReadableSectorTags() []SectorTagType {
	var tags []SectorTagType
	for t, ok := range img.readableSectorTags {
		if ok {
			tags = append(tags, SectorTagType(t))
		}
	}
	return tags
}

// ReadableMediaTags returns the media tags present in the image,
// in ascending order. Only tags 0 to MaxMediaTag are reported.
func (img *Image) ReadableMediaTags() []MediaTagType {
	var tags []MediaTagType
	for t := range img.mediaTags {
		if t >= 0 && t <= MaxMediaTag {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	return tags
}
