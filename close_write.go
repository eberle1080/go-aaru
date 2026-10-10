package aaru

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/eberle1080/go-aaru/compression/cst"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/compression/zstd"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
	"github.com/eberle1080/go-aaru/spamsum"
)

// This file implements image finalization: finalizeWrite and the
// serialization of the deduplication tables, checksums, per-sector
// auxiliary data, media tags and the index. The metadata blocks (geometry,
// metadata, dump hardware, CICM, JSON, DPM, tape files and partitions, flux)
// and the tracks block are written by their owners' functions, called here
// in a fixed order.

// finalizeWrite flushes and finalizes a writable image. Failures of the
// optional block writers do not stop finalization; the first such failure
// is returned after the index has been written.
func (img *Image) finalizeWrite() error {
	var deferred error
	note := func(err error) {
		if err != nil && deferred == nil {
			deferred = err
		}
	}

	// The Zstandard feature bit only reflects the user
	// data blocks written so far: tables and auxiliary blocks compressed
	// below with Zstandard do not set it.
	if img.hasZstdBlocks {
		img.header.FeatureIncompatible |= FeatureIncompatZstd
	}

	if err := img.writeHeader(); err != nil {
		return err
	}

	if img.blockOpen() {
		if err := img.closeCurrentBlock(); err != nil {
			return err
		}
	}

	if img.isTape {
		if img.dirty.tapeDDT {
			if err := img.writeTapeDDT(); err != nil {
				return err
			}
		}
	} else {
		if img.dirty.secondaryDDT {
			if err := img.writeCachedSecondaryDDT(); err != nil {
				return err
			}
		}
		if img.dirty.primaryDDT {
			if err := img.writePrimaryDDT(); err != nil {
				return err
			}
		}
		if img.dirty.singleLevelDDT {
			if err := img.writeSingleLevelDDT(); err != nil {
				return err
			}
		}
	}

	if img.dirty.checksumBlock {
		note(img.writeChecksumBlock())
	}
	if img.dirty.tracksBlock {
		note(img.writeTracksBlock())
	}
	if img.dirty.fluxBlock {
		if err := img.writeFluxBlocks(); err != nil {
			return err
		}
	}
	if img.dirty.mode2SubheadersBlock {
		note(img.writeMode2SubheadersBlock())
	}
	if img.dirty.sectorPrefixBlock {
		note(img.writeSectorPrefix())
	}
	if img.dirty.sectorPrefixDDT {
		note(img.writeSectorPrefixDDT())
	}
	if img.dirty.sectorSuffixBlock {
		note(img.writeSectorSuffix())
	}
	if img.dirty.sectorSuffixDDT {
		note(img.writeSectorSuffixDDT())
	}
	if img.dirty.sectorSubchannelBlock {
		note(img.writeSectorSubchannel())
	}
	if img.dirty.dvdLongSectorBlocks {
		note(img.writeDVDLongSectorBlocks())
	}
	if img.dirty.bdSectorEDCBlock {
		note(img.writeBDSectorEDCBlock())
	}
	if img.dirty.dvdTitleKeyDecryptedBlock {
		note(img.writeDVDTitleKeyDecryptedBlock())
	}
	if img.dirty.mediaTags {
		note(img.writeMediaTags())
	}
	if img.dirty.tapeFileBlock {
		note(img.writeTapeFileBlock())
	}
	if img.dirty.tapePartitionBlock {
		note(img.writeTapePartitionBlock())
	}
	if img.dirty.geometryBlock {
		note(img.writeGeometryBlock())
	}
	if img.dirty.metadataBlock {
		note(img.writeMetadataBlock())
	}
	if img.dirty.dumpHWBlock {
		note(img.writeDumpHardwareBlock())
	}
	if img.dirty.cicmBlock {
		note(img.writeCICMBlock())
	}
	if img.dirty.jsonBlock {
		note(img.writeJSONBlock())
	}
	if img.dirty.dpmBlock {
		note(img.writeDPMBlock())
	}

	if img.dirty.indexBlock {
		if err := img.writeIndexBlock(); err != nil {
			return err
		}
	}

	note(img.ecFinalize())

	img.sectorHashMap = nil
	return deferred
}

// writeHeader writes the image header at offset 0.
func (img *Image) writeHeader() error {
	if err := img.writeAt(marshal(&img.header), 0); err != nil {
		return fmt.Errorf("writing header: %w: %w", ErrCannotWriteHeader, err)
	}
	return nil
}

// closeAppendOffset returns where close-time blocks are appended: the end
// of the file aligned with the user data table's block alignment.
func (img *Image) closeAppendOffset() uint64 {
	return alignUp(uint64(img.size), img.userDataDDTHeader.BlockAlignmentShift)
}

// encodePayloadAlways compresses data with LZMA (or Zstandard when enabled)
// regardless of the Compress option, falling back to storing it
// uncompressed when that does not shrink it. Apple/Priam tag blocks are
// always compressed this way.
func (img *Image) encodePayloadAlways(data []byte) encodedPayload {
	none := encodedPayload{Compression: CompressionNone, Stream: data}
	if len(data) == 0 {
		return none
	}
	if img.options.Zstd {
		out, err := zstd.Encode(data, img.options.ZstdLevel)
		if err != nil || len(out) >= len(data) {
			return none
		}
		img.hasZstdBlocks = true
		return encodedPayload{Compression: CompressionZstd, Stream: out}
	}
	stream, props, err := lzma.Encode(data, img.lzmaOptions())
	if err != nil || len(stream) >= len(data) {
		return none
	}
	return encodedPayload{Compression: CompressionLzma, Props: props[:], Stream: stream}
}

// appendBlock writes data as a data block of type dt at the aligned end of
// the file, replacing the index entry of the same type.
func (img *Image) appendBlock(dt DataType, data []byte, p encodedPayload) error {
	off := img.closeAppendOffset()
	hdr := BlockHeader{
		Identifier: BlockTypeDataBlock,
		Type:       dt,
		Length:     uint32(len(data)),
		CRC64:      crc64.Checksum(data),
	}
	if _, err := img.writeBlockAt(off, &hdr, p); err != nil {
		return err
	}
	// The block of an empty payload (e.g. a CD prefix arena without stored
	// prefixes) is written but not indexed, for compatibility with existing
	// AaruFormat images.

	if len(p.Stream) == 0 {
		return nil
	}
	img.setIndexEntry(BlockTypeDataBlock, dt, off)
	return nil
}

// appendCompressedBlock is appendBlock compressing per the image options.
func (img *Image) appendCompressedBlock(dt DataType, data []byte) error {
	return img.appendBlock(dt, data, img.compressPayload(data, false))
}

// exactly returns b resized to n bytes (zero padded or truncated).
func exactly(b []byte, n uint64) []byte {
	if uint64(len(b)) == n {
		return b
	}
	out := make([]byte, n)
	copy(out, b)
	return out
}

// exactlyTable returns t encoded as exactly n little-endian entries.
func exactlyTable(t []uint64, n uint64) []byte {
	if uint64(len(t)) != n {
		u := make([]uint64, n)
		copy(u, t)
		t = u
	}
	return encodeUint64s(t)
}

// writeCachedSecondaryDDT writes the cached secondary table, if any, and
// points the primary table at it.
func (img *Image) writeCachedSecondaryDDT() error {
	if img.noUserDataDDT {
		return nil
	}
	h := &img.userDataDDTHeader
	if h.TableShift == 0 || img.cachedSecondaryDDT2 == nil {
		return nil
	}
	oldOffset := img.cachedDDTOffset
	eof, err := img.flushCachedSecondaryDDT(img.cachedSecondaryPosition())
	if err != nil {
		return err
	}
	if oldOffset != 0 {
		img.removeSecondaryIndexEntry(oldOffset)
	}
	img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDeDuplicationTableSecondary,
		DataType: DataTypeUserData, Offset: eof})
	img.dirty.indexBlock = true

	img.cachedSecondaryDDT2 = nil
	img.cachedDDTOffset = 0
	return nil
}

// writePrimaryDDT writes the primary table of a two-level DDT in place,
// uncompressed.
func (img *Image) writePrimaryDDT() error {
	if img.noUserDataDDT {
		return nil
	}
	h := &img.userDataDDTHeader
	if h.TableShift == 0 || img.userDataDDT2 == nil {
		return nil
	}
	table := exactlyTable(img.userDataDDT2, h.Entries)
	crc := crc64.Checksum(table)
	h.Identifier = BlockTypeDeDuplicationTable2
	h.Type = DataTypeUserData
	h.Compression = CompressionNone
	h.CRC64 = crc
	h.CmpCRC64 = crc
	h.Length = uint64(len(table))
	h.CmpLength = uint64(len(table))

	if err := img.writeAt(marshal(h), img.primaryDDTOffset); err != nil {
		return fmt.Errorf("writing primary DDT header: %w: %w", ErrCannotWriteHeader, err)
	}
	if err := img.writeAt(table, img.primaryDDTOffset+ddtHeader2Size); err != nil {
		return fmt.Errorf("writing primary DDT: %w: %w", ErrCannotWriteHeader, err)
	}
	img.setIndexEntry(BlockTypeDeDuplicationTable2, DataTypeUserData, img.primaryDDTOffset)
	return nil
}

// writeSingleLevelDDT writes a single-level table at the end of the file.
func (img *Image) writeSingleLevelDDT() error {
	if img.noUserDataDDT {
		return nil
	}
	h := &img.userDataDDTHeader
	if h.TableShift != 0 || img.userDataDDT2 == nil {
		return nil
	}
	h.Identifier = BlockTypeDeDuplicationTable2
	h.Type = DataTypeUserData
	h.Levels = 1
	h.TableLevel = 0
	h.PreviousLevelOffset = 0

	off := img.closeAppendOffset()
	if err := img.writeDDT2At(off, h, exactlyTable(img.userDataDDT2, h.Entries), img.compressPayload); err != nil {
		return err
	}
	img.setIndexEntry(BlockTypeDeDuplicationTable2, DataTypeUserData, off)
	return nil
}

// writeTapeDDT converts the sparse tape table into a single-level table and
// writes it.
func (img *Image) writeTapeDDT() error {
	if !img.isTape || img.noUserDataDDT {
		return nil
	}
	var maxKey uint64
	for k := range img.tapeDDT {
		maxKey = max(maxKey, k)
	}
	if maxKey >= maxDDTEntries {
		return fmt.Errorf("tape block %d too far for the deduplication table: %w", maxKey, ErrNotEnoughMemory)
	}

	h := &img.userDataDDTHeader
	h.Identifier = BlockTypeDeDuplicationTable2
	h.Type = DataTypeUserData
	if img.options.Compress {
		h.Compression = CompressionLzma
		if img.options.Zstd {
			h.Compression = CompressionZstd
		}
	} else {
		h.Compression = CompressionNone
	}
	h.Levels = 1
	h.TableLevel = 0
	h.PreviousLevelOffset = 0
	h.Negative = 0
	h.Overflow = 0
	h.TableShift = 0
	h.Entries = maxKey + 1
	h.Blocks = maxKey + 1
	h.Start = 0
	h.Length = h.Entries * 8
	h.CmpLength = h.Length

	img.userDataDDT2 = make([]uint64, h.Entries)
	for k, v := range img.tapeDDT {
		if k < h.Blocks {
			img.userDataDDT2[k] = v
		}
	}
	return img.writeSingleLevelDDT()
}

// writeChecksumBlock finalizes the running checksums and writes the
// checksum block.
func (img *Image) writeChecksumBlock() error {
	c := &img.checksums
	if img.calculatingMD5 {
		c.MD5 = img.md5Hasher.Sum(nil)
	}
	if img.calculatingSHA1 {
		c.SHA1 = img.sha1Hasher.Sum(nil)
	}
	if img.calculatingSHA256 {
		c.SHA256 = img.sha256Hasher.Sum(nil)
	}
	if img.calculatingSpamSum {
		if s, ok := img.spamsumHasher.(*spamsum.Hasher); ok {
			c.SpamSum = s.Sum()
		}
	}
	if img.calculatingBlake3 {
		c.Blake3 = img.blake3Hasher.Sum(nil)
	}

	if c.MD5 == nil && c.SHA1 == nil && c.SHA256 == nil && c.SpamSum == "" && c.Blake3 == nil {
		return nil
	}

	off := img.closeAppendOffset()
	var body []byte
	var entries uint8
	add := func(t ChecksumAlgorithm, digest []byte) {
		body = append(body, byte(t))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(digest)))
		body = append(body, digest...)
		entries++
	}
	if c.MD5 != nil {
		add(ChecksumMd5, c.MD5)
	}
	if c.SHA1 != nil {
		add(ChecksumSha1, c.SHA1)
	}
	if c.SHA256 != nil {
		add(ChecksumSha256, c.SHA256)
	}
	if c.SpamSum != "" {
		add(ChecksumSpamSum, []byte(c.SpamSum))
	}
	if c.Blake3 != nil {
		add(ChecksumBlake3, c.Blake3)
		img.header.FeatureCompatible |= FeatureRWBlake3
	}

	buf := appendMarshal(nil, &ChecksumHeader{Identifier: BlockTypeChecksumBlock, Length: uint32(len(body)),
		Entries: entries})
	buf = append(buf, body...)
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing checksum block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeChecksumBlock, 0, off)
	return nil
}

// writeMode2SubheadersBlock writes the CD Mode 2 subheaders.
func (img *Image) writeMode2SubheadersBlock() error {
	if img.mode2Subheaders == nil {
		return nil
	}
	return img.appendCompressedBlock(DataTypeCdSubHeader, exactly(img.mode2Subheaders, img.totalSectors()*8))
}

// writeSectorPrefix writes the stored CD sync/header prefixes.
func (img *Image) writeSectorPrefix() error {
	if img.sectorPrefix == nil {
		return nil
	}
	return img.appendCompressedBlock(DataTypeCdSectorPrefix, exactly(img.sectorPrefix, img.sectorPrefixOffset))
}

// writeSectorSuffix writes the stored CD EDC/ECC suffixes.
func (img *Image) writeSectorSuffix() error {
	if img.sectorSuffix == nil {
		return nil
	}
	return img.appendCompressedBlock(DataTypeCdSectorSuffix, exactly(img.sectorSuffix, img.sectorSuffixOffset))
}

// writeCDTableDDT writes a CD prefix or suffix status table.
func (img *Image) writeCDTableDDT(dt DataType, table []uint64) error {
	if table == nil {
		return nil
	}
	h := &img.userDataDDTHeader
	hdr := DDTHeader2{
		Identifier:          BlockTypeDeDuplicationTable2,
		Type:                dt,
		Levels:              1,
		TableLevel:          0,
		Negative:            h.Negative,
		Overflow:            h.Overflow,
		BlockAlignmentShift: h.BlockAlignmentShift,
		DataShift:           h.DataShift,
		TableShift:          0,
		Entries:             img.totalSectors(),
		Blocks:              h.Blocks,
		Start:               0,
	}
	off := img.closeAppendOffset()
	if err := img.writeDDT2At(off, &hdr, exactlyTable(table, hdr.Entries), img.compressPayload); err != nil {
		return err
	}
	img.setIndexEntry(BlockTypeDeDuplicationTable2, dt, off)
	return nil
}

func (img *Image) writeSectorPrefixDDT() error {
	return img.writeCDTableDDT(DataTypeCdSectorPrefix, img.sectorPrefixDDT2)
}

func (img *Image) writeSectorSuffixDDT() error {
	return img.writeCDTableDDT(DataTypeCdSectorSuffix, img.sectorSuffixDDT2)
}

// writeSectorSubchannel writes the CD subchannel, or the Apple/Priam tags
// of block media. CD subchannel is compressed after
// the CD Subchannel Transform when compression is enabled; tags are
// always compressed.
func (img *Image) writeSectorSubchannel() error {
	if img.sectorSubchannel == nil {
		return nil
	}
	var (
		dt   DataType
		data []byte
		p    encodedPayload
	)
	switch img.info.MetadataMediaType {
	case media.OpticalDisc:
		dt = DataTypeCdSubchannel
		data = exactly(img.sectorSubchannel, uint64(uint32(img.totalSectors())*96))
		p = encodedPayload{Compression: CompressionNone, Stream: data}
		if img.options.Compress {
			p = img.encodeCST(data)
		}
	case media.BlockMedia:
		var size uint64
		switch img.info.MediaType {
		case media.AppleProfile, media.AppleFileWare:
			dt, size = DataTypeAppleProfileTag, 20
		case media.AppleSonyDS, media.AppleSonySS:
			dt, size = DataTypeAppleSonyTag, 12
		case media.PriamDataTower:
			dt, size = DataTypePriamDataTowerTag, 24
		default:
			return nil
		}
		n := uint64(uint32(img.info.Sectors+uint64(img.userDataDDTHeader.Overflow)) * uint32(size))
		data = exactly(img.sectorSubchannel, n)
		p = img.encodePayloadAlways(data)
	default:
		return nil
	}
	return img.appendBlock(dt, data, p)
}

// encodeCST compresses CD subchannel data after the CD Subchannel
// Transform (LzmaCst or ZstdCst), falling back to uncompressed storage.
func (img *Image) encodeCST(data []byte) encodedPayload {
	none := encodedPayload{Compression: CompressionNone, Stream: data}
	t, err := cst.Transform(data)
	if err != nil || len(data) == 0 {
		return none
	}
	if img.options.Zstd {
		out, err := zstd.Encode(t, img.options.ZstdLevel)
		if err != nil || len(out) >= len(data) {
			return none
		}
		img.hasZstdBlocks = true
		return encodedPayload{Compression: CompressionZstdCst, Stream: out}
	}
	stream, props, err := lzma.Encode(t, img.lzmaOptions())
	if err != nil || len(stream) >= len(data) {
		return none
	}
	return encodedPayload{Compression: CompressionLzmaCst, Props: props[:], Stream: stream}
}

// writeDVDLongSectorBlocks writes the DVD sector ID, IED, CPR_MAI and EDC
// blocks, all or none.
func (img *Image) writeDVDLongSectorBlocks() error {
	if img.sectorID == nil || img.sectorIED == nil || img.sectorCPRMAI == nil || img.sectorEDC == nil {
		return nil
	}
	total := uint64(uint32(img.totalSectors()))
	return errors.Join(
		img.appendCompressedBlock(DataTypeDvdSectorId, exactly(img.sectorID, total*4)),
		img.appendCompressedBlock(DataTypeDvdSectorIed, exactly(img.sectorIED, total*2)),
		img.appendCompressedBlock(DataTypeDvdSectorCprMai, exactly(img.sectorCPRMAI, total*6)),
		img.appendCompressedBlock(DataTypeDvdSectorEdc, exactly(img.sectorEDC, total*4)),
	)
}

// writeBDSectorEDCBlock writes the Blu-ray sector EDC block. It is also
// written for DVD long sectors, whose EDC shares the buffer.
func (img *Image) writeBDSectorEDCBlock() error {
	if img.sectorEDC == nil {
		return nil
	}
	return img.appendCompressedBlock(DataTypeBdSectorEdc, exactly(img.sectorEDC, uint64(uint32(img.totalSectors()))*4))
}

// writeDVDTitleKeyDecryptedBlock writes the decrypted DVD title keys.
func (img *Image) writeDVDTitleKeyDecryptedBlock() error {
	if img.sectorDecryptedTitleKey == nil {
		return nil
	}
	return img.appendCompressedBlock(DataTypeDvdTitleKeyDecrypted,
		exactly(img.sectorDecryptedTitleKey, uint64(uint32(img.totalSectors()))*5))
}

// mediaTagWriteOrder returns the media tags in write order: tags loaded
// from the image in index order, then the ones
// written in this session in insertion order.
func (img *Image) mediaTagWriteOrder() []MediaTagType {
	written := make(map[MediaTagType]bool, len(img.mediaTagOrder))
	for _, t := range img.mediaTagOrder {
		if _, ok := img.mediaTags[t]; ok {
			written[t] = true
		}
	}
	order := make([]MediaTagType, 0, len(img.mediaTags))
	seen := make(map[MediaTagType]bool, len(img.mediaTags))
	for _, e := range img.indexEntries {
		if e.BlockType != BlockTypeDataBlock {
			continue
		}
		t, ok := dataTypeToMediaTag[e.DataType]
		if !ok || seen[t] || written[t] {
			continue
		}
		if _, ok := img.mediaTags[t]; ok {
			order = append(order, t)
			seen[t] = true
		}
	}
	var rest []MediaTagType
	for t := range img.mediaTags {
		if !seen[t] && !written[t] {
			rest = append(rest, t)
		}
	}
	slices.Sort(rest)
	order = append(order, rest...)
	for _, t := range img.mediaTagOrder {
		if written[t] {
			order = append(order, t)
			delete(written, t)
		}
	}
	return order
}

// writeMediaTags writes one data block per media tag.
func (img *Image) writeMediaTags() error {
	var errs []error
	for _, t := range img.mediaTagWriteOrder() {
		data := img.mediaTags[t]
		dt, ok := mediaTagToDataType[t]
		if !ok {
			dt = DataType(0xFFFF) // unknown media tag type
		}
		errs = append(errs, img.appendCompressedBlock(dt, data))
	}
	return errors.Join(errs...)
}

// writeIndexBlock writes the index (IDX3) at the end of the file and
// rewrites the header to point at it. The header is only updated when the
// index has entries.
func (img *Image) writeIndexBlock() error {
	off := img.closeAppendOffset()
	entries := make([]byte, 0, len(img.indexEntries)*indexEntrySize)
	for i := range img.indexEntries {
		e := &img.indexEntries[i]
		entries = binary.LittleEndian.AppendUint32(entries, uint32(e.BlockType))
		entries = binary.LittleEndian.AppendUint16(entries, uint16(e.DataType))
		entries = binary.LittleEndian.AppendUint64(entries, e.Offset)
	}
	hdr := IndexHeader3{Identifier: BlockTypeIndexBlock3, Entries: uint64(len(img.indexEntries))}
	if len(entries) > 0 {
		hdr.CRC64 = crc64.Checksum(entries)
	}
	buf := appendMarshal(make([]byte, 0, indexHeader3Size+len(entries)), &hdr)
	buf = append(buf, entries...)
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing index: %w: %w", ErrCannotWriteHeader, err)
	}
	if len(img.indexEntries) == 0 {
		return nil
	}
	img.header.IndexOffset = off
	return img.writeHeader()
}
