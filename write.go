package aaru

import (
	"encoding/binary"
	"fmt"

	"github.com/cespare/xxhash/v2"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/compression/flac"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/compression/zstd"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/lisatag"
	"github.com/eberle1080/go-aaru/media"
)

// This file implements sector, long sector, media tag and sector tag
// writing, and the closing of user data blocks.

// maxSectorLength is the biggest sector WriteSector accepts.
const maxSectorLength = 0xFFFF

// cdSync is the 12-byte synchronization pattern of raw CD data sectors.
var cdSync = [12]byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

// checkWritable returns the error for images that cannot be written.
func (img *Image) checkWritable() error {
	if img == nil || img.closed {
		return ErrClosed
	}
	if !img.writable {
		return ErrReadOnly
	}
	return nil
}

// checkSectorAddress applies the write-side bounds: negative
// addresses up to (and including) the number of negative sectors, positive
// ones below user + overflow sectors.
func (img *Image) checkSectorAddress(addr uint64, negative bool) error {
	if negative && addr > uint64(img.userDataDDTHeader.Negative) {
		return fmt.Errorf("negative sector %d: %w", addr, ErrSectorOutOfBounds)
	}
	// Unsigned arithmetic: no upper bound when there are no sectors.
	if !negative && addr > img.info.Sectors+uint64(img.userDataDDTHeader.Overflow)-1 {
		return fmt.Errorf("sector %d: %w", addr, ErrSectorOutOfBounds)
	}
	return nil
}

// correctedAddress maps a (possibly negative) sector address to its index
// in the per-sector tables (negative sectors first).
func (img *Image) correctedAddress(addr uint64, negative bool) uint64 {
	if negative {
		return uint64(img.userDataDDTHeader.Negative) - addr
	}
	return addr + uint64(img.userDataDDTHeader.Negative)
}

// totalSectors returns negative + user + overflow sectors.
func (img *Image) totalSectors() uint64 {
	return uint64(img.userDataDDTHeader.Negative) + img.info.Sectors + uint64(img.userDataDDTHeader.Overflow)
}

// disableChecksums stops all running whole-image checksums.
func (img *Image) disableChecksums() {
	img.calculatingMD5 = false
	img.calculatingSHA1 = false
	img.calculatingSHA256 = false
	img.calculatingSpamSum = false
	img.calculatingBlake3 = false
}

// calculatingAny reports whether any whole-image checksum is running.
func (img *Image) calculatingAny() bool {
	return img.calculatingMD5 || img.calculatingSHA1 || img.calculatingSHA256 || img.calculatingSpamSum ||
		img.calculatingBlake3
}

// trackRewind detects out-of-order writes, which make streaming checksums
// meaningless.
func (img *Image) trackRewind(addr uint64) {
	if addr <= img.lastWrittenBlock {
		if addr == 0 && !img.blockZeroWritten {
			img.blockZeroWritten = true
		} else {
			img.rewound = true
			img.disableChecksums()
		}
	} else {
		img.lastWrittenBlock = addr
	}
}

// updateChecksums feeds data to the running whole-image checksums.
func (img *Image) updateChecksums(data []byte) {
	if img.calculatingMD5 {
		img.md5Hasher.Write(data)
	}
	if img.calculatingSHA1 {
		img.sha1Hasher.Write(data)
	}
	if img.calculatingSHA256 {
		img.sha256Hasher.Write(data)
	}
	if img.calculatingSpamSum {
		img.spamsumHasher.Write(data)
	}
	if img.calculatingBlake3 {
		img.blake3Hasher.Write(data)
	}
}

// blockOpen reports whether a user data block is being assembled.
func (img *Image) blockOpen() bool { return img.currentBlockHeader.Identifier == BlockTypeDataBlock }

// WriteSector writes the user data of a sector.
// Negative addresses (negative == true) refer to sectors before sector 0,
// such as an optical lead-in; addresses at or after the number of user
// sectors refer to the overflow area.
//
// Sectors are buffered into blocks that are compressed and written when full,
// when the sector size changes, and on Close. With deduplication enabled,
// sectors with identical contents are stored only once. Whole-image checksums
// are computed only while sectors are written in increasing order.
//
// The status is stored in the deduplication table. For PlayStation 3 media,
// sectors written with SectorStatusUnencrypted are decrypted with the disc
// key (from the PS3 disc key or data1 media tag) before being stored; for
// Nintendo GameCube/Wii media, SectorStatusGenerable sectors only record
// their status (their content is regenerated on read).
func (img *Image) WriteSector(addr uint64, negative bool, data []byte, status SectorStatus) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if img.noUserDataDDT {
		return fmt.Errorf("writing sector to flux-only image: %w", ErrUserDataNotPresent)
	}
	if err := img.checkSectorAddress(addr, negative); err != nil {
		return err
	}
	if len(data) > maxSectorLength {
		return fmt.Errorf("sector of %d bytes: %w", len(data), ErrInvalidSectorLength)
	}
	length := uint32(len(data))

	if length > uint32(img.header.BiggestSectorSize) {
		img.header.BiggestSectorSize = uint16(length)
	}

	if !img.rewound {
		// Decrypted or generable (NGCW) sectors make the checksums meaningless.
		if status == SectorStatusUnencrypted || status == SectorStatusGenerable {
			img.rewound = true
			img.disableChecksums()
		} else {
			img.trackRewind(addr)
		}
	}

	// Checksums cover user sectors (and, through <=, the first overflow
	// sector) and are computed on the data as given.
	if !negative && addr <= img.info.Sectors && !img.writingLong {
		if img.calculatingAny() {
			img.updateChecksums(data)
			img.dirty.checksumBlock = true
		}
	}

	mt := media.Type(img.header.MediaType)
	writeData := data

	// PS3: encrypted sectors marked "to be stored decrypted".
	if status == SectorStatusUnencrypted && (mt == media.PS3DVD || mt == media.PS3BD) {
		if dec, ok := img.ps3DecryptForWrite(addr, data); ok {
			writeData = dec
		}
	}

	// NGCW: generable sectors only get a DDT entry; no data is stored.
	if status == SectorStatusGenerable && (mt == media.GOD || mt == media.WOD) {
		var entry uint64
		if err := img.setDDTEntryV2(addr, negative, 0, 0, status, &entry); err != nil {
			return err
		}
		return nil
	}

	// Close the current block when the sector size changes or it is full.
	if img.blockOpen() && (img.currentBlockHeader.SectorSize != length ||
		uint64(img.currentBlockOffset) == uint64(1)<<img.userDataDDTHeader.DataShift) {
		if err := img.closeCurrentBlock(); err != nil {
			return err
		}
	}

	var entry uint64
	if img.options.Deduplicate {
		if img.sectorHashMap == nil {
			img.sectorHashMap = make(map[uint64]uint64)
		}
		hash := xxhash.Sum64(writeData)
		existing, found := img.sectorHashMap[hash]
		if found {
			entry = existing
		}
		if err := img.setDDTEntryV2(addr, negative, uint64(img.currentBlockOffset), img.nextBlockPosition, status,
			&entry); err != nil {
			return err
		}
		if found {
			return nil
		}
		img.sectorHashMap[hash] = entry
	} else if err := img.setDDTEntryV2(addr, negative, uint64(img.currentBlockOffset), img.nextBlockPosition,
		status, &entry); err != nil {
		return err
	}

	if len(img.writingBuffer) == 0 {
		img.startBlock(addr, length)
	}

	img.writingBuffer = append(img.writingBuffer, writeData...)
	img.currentBlockOffset++
	return nil
}

// startBlock initializes the header of a new user data block, choosing its
// compression from the track the first sector belongs to.
func (img *Image) startBlock(addr uint64, length uint32) {
	h := &img.currentBlockHeader
	h.Identifier = BlockTypeDataBlock
	h.Type = DataTypeUserData
	h.SectorSize = length

	mt := img.info.MediaType
	dataCompression := CompressionNone
	if img.options.Compress {
		dataCompression = CompressionLzma
		if img.options.Zstd {
			dataCompression = CompressionZstd
		}
	}

	if img.info.MetadataMediaType == media.OpticalDisc && img.tracks != nil {
		var track *TrackEntry
		for i := range img.tracks {
			if addr >= uint64(img.tracks[i].Start) && addr <= uint64(img.tracks[i].End) {
				track = &img.tracks[i]
				break
			}
		}

		if track != nil {
			img.currentTrackType = track.Type
			if track.Sequence == 0 && track.Start == 0 && track.End == 0 {
				img.currentTrackType = TrackTypeData
			}
		} else {
			img.currentTrackType = TrackTypeData
		}

		// JaguarCD stores data in audio tracks of the second session and
		// VideoNow stores video in audio tracks: LZMA does better than FLAC.
		if img.currentTrackType == TrackTypeAudio &&
			(mt == media.JaguarCD && track.Session > 1 || mt == media.VideoNow || mt == media.VideoNowColor ||
				mt == media.VideoNowXp) {
			img.currentTrackType = TrackTypeData
		}

		if img.options.Compress && img.currentTrackType == TrackTypeAudio {
			h.Compression = CompressionFlac
		} else {
			h.Compression = dataCompression
		}
	} else {
		img.currentTrackType = TrackTypeData
		h.Compression = dataCompression
	}

	if cap(img.writingBuffer) == 0 {
		want := (uint64(1) << img.userDataDDTHeader.DataShift) * uint64(length)
		if want > 64<<20 {
			want = 64 << 20
		}
		img.writingBuffer = make([]byte, 0, want)
	}
}

// closeCurrentBlock compresses and writes the block being assembled and
// registers it in the index.
func (img *Image) closeCurrentBlock() error {
	if err := img.checkWritable(); err != nil {
		return err
	}

	h := &img.currentBlockHeader
	h.Length = uint32(img.currentBlockOffset) * h.SectorSize
	if int(h.Length) > len(img.writingBuffer) { // only reachable with inconsistent state
		return fmt.Errorf("block of %d bytes holds %d bytes: %w", h.Length, len(img.writingBuffer),
			ErrCannotWriteBlockData)
	}
	raw := img.writingBuffer[:h.Length]
	h.CRC64 = crc64.Checksum(raw)

	var props []byte
	payload := raw
	switch h.Compression {
	case CompressionNone:
	case CompressionFlac:
		opts := flac.DefaultEncoderOptions()
		opts.BlockSize = flac.BlockSizeForSectors(img.currentBlockOffset)
		out, err := flac.EncodeRedbook(raw, opts)
		if err != nil || len(out) >= len(raw) {
			h.Compression = CompressionNone
		} else {
			payload = out
		}
	case CompressionLzma:
		stream, p, err := lzma.Encode(raw, img.lzmaOptions())
		if err != nil || len(stream) >= len(raw) {
			h.Compression = CompressionNone
		} else {
			payload = stream
			props = p[:]
		}
	case CompressionZstd:
		out, err := zstd.Encode(raw, img.options.ZstdLevel)
		if err != nil || len(out) == 0 || len(out) >= len(raw) {
			h.Compression = CompressionNone
		} else {
			payload = out
			img.hasZstdBlocks = true
		}
	default:
		return fmt.Errorf("invalid compression %v: %w", h.Compression, ErrCannotWriteBlockData)
	}

	if h.Compression == CompressionNone {
		h.CmpCRC64 = h.CRC64
		h.CmpLength = h.Length
		payload = raw
	} else {
		h.CmpCRC64 = crc64.Checksum(payload)
		h.CmpLength = uint32(len(payload) + len(props))
	}

	off := img.nextBlockPosition
	img.indexEntries = append(img.indexEntries,
		IndexEntry{BlockType: BlockTypeDataBlock, DataType: DataTypeUserData, Offset: off})
	img.dirty.indexBlock = true

	var hb [blockHeaderSize + LZMAPropertiesLength]byte
	head := h.appendTo(hb[:0])
	head = append(head, props...)
	if err := img.writeAt(head, off); err != nil {
		return fmt.Errorf("writing block header at %d: %w: %w", off, ErrCannotWriteBlockHeader, err)
	}
	if err := img.writeAt(payload, off+uint64(len(head))); err != nil {
		return fmt.Errorf("writing block data at %d: %w: %w", off, ErrCannotWriteBlockData, err)
	}

	img.ecAccumulateDataBlock(h, props, payload, off)

	// This advances nextBlockPosition (not off): when the
	// block completed an erasure coding stripe, ecAccumulateDataBlock moved
	// it past the parity blocks, and a block-sized gap is left after them.
	img.nextBlockPosition = alignUp(img.nextBlockPosition+blockHeaderSize+uint64(h.CmpLength),
		img.userDataDDTHeader.BlockAlignmentShift)

	img.writingBuffer = img.writingBuffer[:0]
	img.currentBlockOffset = 0
	img.currentBlockHeader = BlockHeader{}
	return nil
}

// isDVDLongMedia reports the media whose 2064-byte long sectors carry the
// DVD ID, IED, CPR_MAI and EDC fields.
func isDVDLongMedia(t media.Type) bool {
	switch t {
	case media.DVDROM, media.PS2DVD, media.SACD, media.PS3DVD, media.DVDR, media.DVDRW, media.DVDPR,
		media.DVDPRW, media.DVDPRWDL, media.DVDRDL, media.DVDPRDL, media.DVDRAM, media.DVDRWDL,
		media.DVDDownload, media.Nuon:
		return true
	}
	return false
}

// isBDLongMedia reports the media whose 2052-byte long sectors carry a
// Blu-ray EDC.
func isBDLongMedia(t media.Type) bool {
	switch t {
	case media.BDROM, media.BDR, media.BDRE, media.BDRXL, media.BDREXL, media.UHDBD, media.XGD4, media.PS3BD,
		media.PS4BD, media.PS5BD:
		return true
	}
	return false
}

// cdHeaderMatchesLBA reports whether a CD sector header (bytes 0x0C-0x0E)
// is valid BCD and encodes exactly lba. Non-BCD headers that merely alias
// the address are stored verbatim instead of being regenerated on read.
func cdHeaderMatchesLBA(data []byte, lba int64) bool {
	for i := 0x0C; i <= 0x0E; i++ {
		if data[i]>>4 > 9 || data[i]&0x0F > 9 {
			return false
		}
	}
	minute := int64(data[0x0C]>>4)*10 + int64(data[0x0C]&0x0F)
	second := int64(data[0x0D]>>4)*10 + int64(data[0x0D]&0x0F)
	frame := int64(data[0x0E]>>4)*10 + int64(data[0x0E]&0x0F)
	if second > 59 || frame > 74 {
		return false
	}
	return minute*60*75+second*75+frame-150 == lba
}

// putAt copies src into dst at off, failing instead of overflowing.
func putAt(dst []byte, off uint64, src []byte) error {
	if off > uint64(len(dst)) || uint64(len(src)) > uint64(len(dst))-off {
		return fmt.Errorf("per-sector buffer offset %d: %w", off, ErrSectorOutOfBounds)
	}
	copy(dst[off:], src)
	return nil
}

// ensureBuffer allocates a zeroed per-sector buffer of n bytes if *b is nil.
func ensureBuffer(b *[]byte, n uint64) error {
	if *b != nil {
		return nil
	}
	if n > maxBlockPayload {
		return fmt.Errorf("per-sector buffer of %d bytes: %w", n, ErrNotEnoughMemory)
	}
	*b = make([]byte, n)
	return nil
}

// ensureTable allocates a zeroed per-sector table of n entries if *t is nil.
func ensureTable(t *[]uint64, n uint64) error {
	if *t != nil {
		return nil
	}
	if n > maxBlockPayload/8 {
		return fmt.Errorf("per-sector table of %d entries: %w", n, ErrNotEnoughMemory)
	}
	*t = make([]uint64, n)
	return nil
}

// setTableEntry stores v at index i of t, failing instead of overflowing.
func setTableEntry(t []uint64, i, v uint64) error {
	if i >= uint64(len(t)) {
		return fmt.Errorf("per-sector table index %d: %w", i, ErrSectorOutOfBounds)
	}
	t[i] = v
	return nil
}

// appendArena appends slot bytes to a CD prefix/suffix arena at *offset,
// copying src at the start of the slot and zero-filling the rest, and
// returns the slot index. The arena only holds the custom (non
// regenerable) entries, so it is compact.
func appendArena(arena *[]byte, offset *uint64, src []byte, slot int) uint64 {
	if uint64(len(*arena)) > *offset {
		*arena = (*arena)[:*offset]
	}
	for uint64(len(*arena)) < *offset { // never shorter in practice
		*arena = append(*arena, 0)
	}
	idx := *offset / uint64(slot)
	start := len(*arena)
	*arena = append(*arena, make([]byte, slot)...)
	copy((*arena)[start:], src)
	*offset += uint64(slot)
	return idx
}

// WriteSectorLong writes a complete raw sector,
// splitting it into user data (stored like WriteSector) and the
// structural fields, which are stored separately and only when they cannot
// be regenerated:
//
//   - CD (2352 bytes): sync/header prefix, Mode 2 subheader and EDC/ECC
//     suffix; correct prefixes and suffixes are only flagged, anomalous ones
//     are stored verbatim. Audio and data tracks are stored as is.
//   - DVD (2064 bytes): sector ID, IED, CPR_MAI and EDC.
//   - Blu-ray (2052 bytes): EDC.
//   - Apple Lisa/Macintosh and Priam media (512 bytes + 12, 20 or 24 byte
//     tag): the tag, converted to the media's native tag format.
//
// Other media return ErrIncorrectMediaType; unexpected lengths return
// ErrIncorrectDataSize.
func (img *Image) WriteSectorLong(addr uint64, negative bool, data []byte, status SectorStatus) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if img.noUserDataDDT {
		return fmt.Errorf("writing long sector to flux-only image: %w", ErrUserDataNotPresent)
	}
	if err := img.checkSectorAddress(addr, negative); err != nil {
		return err
	}
	length := uint64(len(data))
	mt := img.info.MediaType

	switch img.info.MetadataMediaType {
	case media.OpticalDisc:
		var track TrackEntry
		for _, t := range img.tracks {
			if addr >= uint64(t.Start) && addr <= uint64(t.End) {
				track = t
				break
			}
		}
		if track.Sequence == 0 && track.Start == 0 && track.End == 0 {
			track.Type = TrackTypeData
		}

		corrected := img.correctedAddress(addr, negative)
		total := img.totalSectors()
		lba := int64(addr)
		if negative {
			lba = -lba
		}

		// DVD long sector
		if length == 2064 && isDVDLongMedia(mt) {
			if err := img.storeDVDLongFields(corrected, total, data[0:4], data[4:6], data[6:12],
				data[2060:2064]); err != nil {
				return err
			}
			return img.WriteSector(addr, negative, data[12:12+2048], status)
		}

		// Nintendo DVD long sector.
		if length == 2064 && (mt == media.GOD || mt == media.WOD) {
			if err := img.storeDVDLongFields(corrected, total, data[0:4], data[4:6], data[2054:2060],
				data[2060:2064]); err != nil {
				return err
			}
			return img.WriteSector(addr, negative, data[6:6+2048], status)
		}

		// Blu-ray long sector
		if length == 2052 && isBDLongMedia(mt) {
			if err := ensureBuffer(&img.sectorEDC, 4*total); err != nil {
				return err
			}
			if err := putAt(img.sectorEDC, corrected*4, data[2048:2052]); err != nil {
				return err
			}
			img.dirty.bdSectorEDCBlock = true
			return img.WriteSector(addr, negative, data[:2048], status)
		}

		if length != 2352 {
			return fmt.Errorf("optical long sector of %d bytes: %w", length, ErrIncorrectDataSize)
		}

		img.writingLong = true

		if !img.rewound {
			img.trackRewind(addr)
		}

		if !negative && addr <= img.info.Sectors {
			img.updateChecksums(data)
		}

		switch track.Type {
		case TrackTypeAudio, TrackTypeData:
			return img.WriteSector(addr, negative, data, status)
		case TrackTypeCdMode1:
			return img.writeCDMode1(addr, negative, corrected, total, lba, data)
		case TrackTypeCdMode2Form1, TrackTypeCdMode2Form2, TrackTypeCdMode2Formless:
			return img.writeCDMode2(addr, negative, corrected, total, lba, data)
		}
		return fmt.Errorf("track type %v: %w", track.Type, ErrIncorrectMediaType)

	case media.BlockMedia:
		var target lisatag.Format
		switch mt {
		case media.AppleProfile, media.AppleFileWare:
			target = lisatag.FormatProfile
		case media.PriamDataTower:
			target = lisatag.FormatPriam
		case media.AppleSonyDS, media.AppleSonySS:
			target = lisatag.FormatSony
		case media.AppleWidget:
			// Tags are accepted but not stored.
		default:
			return fmt.Errorf("long sectors on media type %v: %w", mt, ErrIncorrectMediaType)
		}

		if length < 512 {
			return fmt.Errorf("block long sector of %d bytes: %w", length, ErrIncorrectDataSize)
		}
		switch length - 512 {
		case 0:
			target = 0
		case lisatag.SonyTagSize, lisatag.ProfileTagSize, lisatag.PriamTagSize:
		default:
			return fmt.Errorf("block long sector of %d bytes: %w", length, ErrIncorrectDataSize)
		}
		if target == 0 {
			return img.WriteSector(addr, negative, data[:512], status)
		}

		tag, err := lisatag.Convert(data[512:], target)
		if err != nil {
			return fmt.Errorf("converting sector tag: %w: %w", ErrIncorrectDataSize, err)
		}
		size := uint64(target.Size())
		if err := ensureBuffer(&img.sectorSubchannel, size*(img.info.Sectors+
			uint64(img.userDataDDTHeader.Overflow))); err != nil {
			return err
		}
		if err := putAt(img.sectorSubchannel, addr*size, tag); err != nil {
			return err
		}
		img.dirty.sectorSubchannelBlock = true
		return img.WriteSector(addr, negative, data[:512], status)
	}

	return fmt.Errorf("long sectors on media type %v: %w", mt, ErrIncorrectMediaType)
}

// storeDVDLongFields records the auxiliary fields of a DVD long sector.
func (img *Image) storeDVDLongFields(corrected, total uint64, id, ied, cprMai, edc []byte) error {
	if err := ensureBuffer(&img.sectorID, 4*total); err != nil {
		return err
	}
	if err := ensureBuffer(&img.sectorIED, 2*total); err != nil {
		return err
	}
	if err := ensureBuffer(&img.sectorCPRMAI, 6*total); err != nil {
		return err
	}
	if err := ensureBuffer(&img.sectorEDC, 4*total); err != nil {
		return err
	}
	if err := putAt(img.sectorID, corrected*4, id); err != nil {
		return err
	}
	if err := putAt(img.sectorIED, corrected*2, ied); err != nil {
		return err
	}
	if err := putAt(img.sectorCPRMAI, corrected*6, cprMai); err != nil {
		return err
	}
	if err := putAt(img.sectorEDC, corrected*4, edc); err != nil {
		return err
	}
	img.dirty.dvdLongSectorBlocks = true
	return nil
}

// ensureCDTables allocates the CD prefix/suffix tables and arenas.
func (img *Image) ensureCDTables(total uint64) error {
	if err := ensureTable(&img.sectorPrefixDDT2, total); err != nil {
		return err
	}
	if err := ensureTable(&img.sectorSuffixDDT2, total); err != nil {
		return err
	}
	if img.sectorPrefix == nil {
		img.sectorPrefix = make([]byte, 0, 4096)
		img.sectorPrefixOffset = 0
	}
	if img.sectorSuffix == nil {
		img.sectorSuffix = make([]byte, 0, 16*288)
		img.sectorSuffixOffset = 0
	}
	return nil
}

// isEmptySector reports whether every byte of data is zero.
func isEmptySector(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// cdPrefixCorrect reports whether a raw sector starts with the sync
// pattern, has the given mode byte and a canonical header for lba.
func cdPrefixCorrect(data []byte, mode byte, lba int64) bool {
	if [12]byte(data[:12]) != cdSync || data[0x0F] != mode {
		return false
	}
	return cdHeaderMatchesLBA(data, lba)
}

// storePrefix records the prefix of a CD sector: only a status when it can
// be regenerated, the 16 bytes otherwise.
func (img *Image) storePrefix(corrected uint64, data []byte, correct bool, okStatus SectorStatus) error {
	if corrected >= uint64(len(img.sectorPrefixDDT2)) {
		return fmt.Errorf("prefix table index %d: %w", corrected, ErrSectorOutOfBounds)
	}
	if correct {
		img.sectorPrefixDDT2[corrected] = uint64(okStatus) << 60
	} else {
		idx := appendArena(&img.sectorPrefix, &img.sectorPrefixOffset, data[:16], 16)
		img.sectorPrefixDDT2[corrected] = idx | uint64(SectorStatusErrored)<<60
		img.dirty.sectorPrefixBlock = true
	}
	img.dirty.sectorPrefixDDT = true
	return nil
}

// storeSuffix records the suffix status of a CD sector, appending a
// 288-byte slot holding src when it cannot be regenerated (errored).
func (img *Image) storeSuffix(corrected uint64, status SectorStatus, src []byte) error {
	if corrected >= uint64(len(img.sectorSuffixDDT2)) {
		return fmt.Errorf("suffix table index %d: %w", corrected, ErrSectorOutOfBounds)
	}
	if status != SectorStatusErrored {
		img.sectorSuffixDDT2[corrected] = uint64(status) << 60
		return nil
	}
	idx := appendArena(&img.sectorSuffix, &img.sectorSuffixOffset, src, 288)
	img.sectorSuffixDDT2[corrected] = idx | uint64(SectorStatusErrored)<<60
	img.dirty.sectorSuffixBlock = true
	return nil
}

// markNotDumpedCD flags an all-zero CD sector in both prefix and suffix
// tables.
func (img *Image) markNotDumpedCD(corrected uint64) error {
	if err := setTableEntry(img.sectorPrefixDDT2, corrected, uint64(SectorStatusNotDumped)<<60); err != nil {
		return err
	}
	if err := setTableEntry(img.sectorSuffixDDT2, corrected, uint64(SectorStatusNotDumped)<<60); err != nil {
		return err
	}
	img.dirty.sectorPrefixDDT = true
	img.dirty.sectorSuffixDDT = true
	return nil
}

// writeCDMode1 splits a raw Mode 1 sector.
func (img *Image) writeCDMode1(addr uint64, negative bool, corrected, total uint64, lba int64, data []byte) error {
	if err := img.ensureCDTables(total); err != nil {
		return err
	}

	if isEmptySector(data) {
		if err := img.markNotDumpedCD(corrected); err != nil {
			return err
		}
		return img.WriteSector(addr, negative, data[16:16+2048], SectorStatusNotDumped)
	}

	prefixCorrect := cdPrefixCorrect(data, 0x01, lba)
	if err := img.storePrefix(corrected, data, prefixCorrect, SectorStatusMode1Correct); err != nil {
		return err
	}

	suffixCorrect := cdecc.IsSuffixCorrect(data)
	suffixStatus := SectorStatusMode1Correct
	if !suffixCorrect {
		suffixStatus = SectorStatusErrored
	}
	if err := img.storeSuffix(corrected, suffixStatus, data[2064:2352]); err != nil {
		return err
	}
	img.dirty.sectorSuffixDDT = true

	status := SectorStatusErrored
	if prefixCorrect && suffixCorrect {
		status = SectorStatusMode1Correct
	}
	return img.WriteSector(addr, negative, data[16:16+2048], status)
}

// writeCDMode2 splits a raw Mode 2 (formless, Form 1 or Form 2) sector.
func (img *Image) writeCDMode2(addr uint64, negative bool, corrected, total uint64, lba int64, data []byte) error {
	if err := img.ensureCDTables(total); err != nil {
		return err
	}

	if isEmptySector(data) {
		if err := img.markNotDumpedCD(corrected); err != nil {
			return err
		}
		return img.WriteSector(addr, negative, data[16:16+2328], SectorStatusNotDumped)
	}

	form2 := data[18]&0x20 == 0x20 || data[22]&0x20 == 0x20

	prefixCorrect := cdPrefixCorrect(data, 0x02, lba)
	okStatus := SectorStatusMode2Form1Ok
	if form2 {
		okStatus = SectorStatusMode2Form2Ok
	}
	if err := img.storePrefix(corrected, data, prefixCorrect, okStatus); err != nil {
		return err
	}

	if err := ensureBuffer(&img.mode2Subheaders, 8*total); err != nil {
		return err
	}

	if form2 {
		computed := cdecc.ComputeEDC(0, data[0x10:0x10+0x91C])
		edc := binary.LittleEndian.Uint32(data[0x92C:])
		correct := computed == edc

		suffixStatus := SectorStatusErrored
		switch {
		case correct:
			suffixStatus = SectorStatusMode2Form2Ok
		case edc == 0:
			suffixStatus = SectorStatusMode2Form2NoCrc
		}
		if err := img.storeSuffix(corrected, suffixStatus, data[2348:2352]); err != nil {
			return err
		}

		if err := putAt(img.mode2Subheaders, corrected*8, data[0x10:0x18]); err != nil {
			return err
		}
		img.dirty.sectorPrefixDDT = true
		img.dirty.sectorSuffixDDT = true
		img.dirty.mode2SubheadersBlock = true

		status := SectorStatusErrored
		switch {
		case edc == 0:
			status = SectorStatusMode2Form2NoCrc
		case correct:
			status = SectorStatusMode2Form2Ok
		}
		return img.WriteSector(addr, negative, data[24:24+2324], status)
	}

	correctECC := cdecc.IsSuffixCorrectMode2(data)
	computed := cdecc.ComputeEDC(0, data[0x10:0x10+0x808])
	correctEDC := computed == binary.LittleEndian.Uint32(data[0x818:])

	suffixStatus := SectorStatusErrored
	if correctECC && correctEDC {
		suffixStatus = SectorStatusMode2Form1Ok
	}
	if err := img.storeSuffix(corrected, suffixStatus, data[2072:2352]); err != nil {
		return err
	}

	if err := putAt(img.mode2Subheaders, corrected*8, data[0x10:0x18]); err != nil {
		return err
	}
	img.dirty.sectorPrefixDDT = true
	img.dirty.sectorSuffixDDT = true
	img.dirty.mode2SubheadersBlock = true
	return img.WriteSector(addr, negative, data[24:24+2048], suffixStatus)
}

// WriteMediaTag stores a whole-medium tag (TOC, PFI, INQUIRY, ...),
// replacing any previous tag of the same type. Tags
// are written to the image on Close. The data is copied.
func (img *Image) WriteMediaTag(tag MediaTagType, data []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty media tag: %w", ErrIncorrectDataSize)
	}
	if img.mediaTags == nil {
		img.mediaTags = make(map[MediaTagType][]byte)
	}
	img.mediaTags[tag] = append([]byte(nil), data...)

	// Keep insertion order: replaced tags move to the end.
	for i, t := range img.mediaTagOrder {
		if t == tag {
			img.mediaTagOrder = append(img.mediaTagOrder[:i], img.mediaTagOrder[i+1:]...)
			break
		}
	}
	img.mediaTagOrder = append(img.mediaTagOrder, tag)
	img.dirty.mediaTags = true
	return nil
}

// WriteSectorTag stores per-sector auxiliary data:
// CD subchannel (96 bytes), track flags (1) and ISRC (12, stored in the
// track containing the sector), DVD CPR_MAI (only the CMI byte, 1), sector
// information (1), sector number (3), IED (2), EDC (4) and decrypted title
// key (5), and Apple Sony (12), Apple ProFile (20) and Priam DataTower (24)
// tags. The data length must match the tag. Tags are written to the image on
// Close.
func (img *Image) WriteSectorTag(addr uint64, negative bool, tag SectorTagType, data []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if img.noUserDataDDT {
		return fmt.Errorf("writing sector tag to flux-only image: %w", ErrUserDataNotPresent)
	}
	if err := img.checkSectorAddress(addr, negative); err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty sector tag: %w", ErrIncorrectDataSize)
	}

	corrected := img.correctedAddress(addr, negative)
	total := img.totalSectors()

	// tagSize: the exact length required; class: the media class required.
	check := func(class media.MetadataType, size int) error {
		if img.info.MetadataMediaType != class {
			return fmt.Errorf("sector tag %v on %v media: %w", tag, img.info.MetadataMediaType, ErrIncorrectMediaType)
		}
		if len(data) != size {
			return fmt.Errorf("sector tag %v of %d bytes: %w", tag, len(data), ErrIncorrectDataSize)
		}
		return nil
	}
	store := func(buf *[]byte, unit uint64, at uint64) error {
		if err := ensureBuffer(buf, unit*total); err != nil {
			return err
		}
		return putAt(*buf, at, data)
	}

	switch tag {
	case SectorTagCdTrackFlags, SectorTagCdTrackIsrc:
		size := 1
		if tag == SectorTagCdTrackIsrc {
			size = 12
		}
		if err := check(media.OpticalDisc, size); err != nil {
			return err
		}
		for i := range img.tracks {
			if addr >= uint64(img.tracks[i].Start) && addr <= uint64(img.tracks[i].End) {
				if tag == SectorTagCdTrackFlags {
					img.tracks[i].Flags = data[0]
				} else {
					copy(img.tracks[i].ISRC[:], data[:12])
				}
				// Recompute the CRC so the tracks block verifies once
				// reopened.
				img.tracksHeader.CRC64 = crc64.Checksum(marshal(img.tracks[:min(len(img.tracks),
					int(img.tracksHeader.Entries))]))
				img.dirty.tracksBlock = true
				return nil
			}
		}
		return fmt.Errorf("no track contains sector %d: %w", addr, ErrTrackNotFound)
	case SectorTagCdSubchannel:
		if err := check(media.OpticalDisc, 96); err != nil {
			return err
		}
		if err := store(&img.sectorSubchannel, 96, corrected*96); err != nil {
			return err
		}
		img.dirty.sectorSubchannelBlock = true
	case SectorTagDvdCmi:
		if err := check(media.OpticalDisc, 1); err != nil {
			return err
		}
		if err := store(&img.sectorCPRMAI, 6, corrected*6); err != nil {
			return err
		}
		img.dirty.dvdLongSectorBlocks = true
	case SectorTagDvdSectorInformation:
		if err := check(media.OpticalDisc, 1); err != nil {
			return err
		}
		if err := store(&img.sectorID, 4, corrected*4); err != nil {
			return err
		}
		img.dirty.dvdLongSectorBlocks = true
	case SectorTagDvdSectorNumber:
		if err := check(media.OpticalDisc, 3); err != nil {
			return err
		}
		if err := store(&img.sectorID, 4, corrected*4+1); err != nil {
			return err
		}
		img.dirty.dvdLongSectorBlocks = true
	case SectorTagDvdSectorIed:
		if err := check(media.OpticalDisc, 2); err != nil {
			return err
		}
		if err := store(&img.sectorIED, 2, corrected*2); err != nil {
			return err
		}
		img.dirty.dvdLongSectorBlocks = true
	case SectorTagDvdSectorEdc:
		if err := check(media.OpticalDisc, 4); err != nil {
			return err
		}
		if err := store(&img.sectorEDC, 4, corrected*4); err != nil {
			return err
		}
		img.dirty.dvdLongSectorBlocks = true
	case SectorTagDvdTitleKeyDecrypted:
		if err := check(media.OpticalDisc, 5); err != nil {
			return err
		}
		if err := store(&img.sectorDecryptedTitleKey, 5, corrected*5); err != nil {
			return err
		}
		img.dirty.dvdTitleKeyDecryptedBlock = true
	case SectorTagAppleSony, SectorTagAppleProfile, SectorTagPriamDataTower:
		size := map[SectorTagType]int{SectorTagAppleSony: 12, SectorTagAppleProfile: 20,
			SectorTagPriamDataTower: 24}[tag]
		if err := check(media.BlockMedia, size); err != nil {
			return err
		}
		if err := store(&img.sectorSubchannel, uint64(size), corrected*uint64(size)); err != nil {
			return err
		}
		img.dirty.sectorSubchannelBlock = true
	case SectorTagBdSectorEdc:
		// Block media is required here, so Blu-ray discs (optical media)
		// cannot use this tag; kept for compatibility.
		if err := check(media.BlockMedia, 4); err != nil {
			return err
		}
		if err := store(&img.sectorEDC, 4, corrected*4); err != nil {
			return err
		}
		img.dirty.bdSectorEDCBlock = true
	default:
		return fmt.Errorf("sector tag %v: %w", tag, ErrInvalidTag)
	}
	return nil
}
