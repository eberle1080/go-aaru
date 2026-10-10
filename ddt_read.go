package aaru

import (
	"fmt"

	"github.com/eberle1080/go-aaru/crc64"
)

// Version 2 deduplication tables, read half.

const (
	ddtEntryStatusShift = 60
	ddtEntryPointerMask = 0x0FFFFFFFFFFFFFFF
)

// loadDDT2Table reads, decompresses and CRC-checks the payload of the DDT v2
// table whose header h is at off: skip is true when the table must be
// ignored, err non-nil when opening must
// fail.
func (img *Image) loadDDT2Table(off uint64, h *DDTHeader2) (entries []uint64, skip bool, err error) {
	payloadOff := off + ddtHeader2Size
	var data []byte
	switch h.Compression {
	case CompressionLzma, CompressionZstd:
		if h.Compression == CompressionZstd && h.CmpLength == 0 {
			return nil, false, fmt.Errorf("zstd DDT payload of 0 bytes: %w", ErrCannotDecompressBlock)
		}
		d, skip, err := img.readDDTPayload(payloadOff, h.Compression, h.CmpLength, h.Length)
		if err != nil || skip {
			return nil, skip, err
		}
		data = d
	case CompressionNone:
		d, rerr := img.readAt(payloadOff, h.Length)
		if rerr != nil {
			return nil, true, nil
		}
		data = d
	default:
		return nil, true, nil
	}
	if crc64.Checksum(data) != h.CRC64 {
		return nil, false, fmt.Errorf("DDT at %d: %w", off, ErrInvalidBlockCRC)
	}
	return decodeUint64s(data), false, nil
}

// processDDTv2 loads a version 2 deduplication table: the
// primary (or single-level) user data table, or the CD prefix/suffix tables.
// Secondary tables are loaded on demand by decodeDDTEntryV2.
func (img *Image) processDDTv2(e IndexEntry, found *bool) error {
	var hb [ddtHeader2Size]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading DDT header at %d: %w", e.Offset, ErrCannotReadBlock)
	}
	h, _ := unmarshal[DDTHeader2](hb[:])
	img.info.ImageSize += h.CmpLength

	switch e.DataType {
	case DataTypeUserData:
		// User area sectors: all blocks minus the negative and overflow ones.
		img.info.Sectors = h.Blocks - uint64(h.Negative) - uint64(h.Overflow)
		img.userDataDDTHeader = h
		img.ddtVersion = 2
		img.primaryDDTOffset = e.Offset

		switch h.Compression {
		case CompressionLzma, CompressionZstd, CompressionNone:
		default:
			*found = false
			return nil
		}
		entries, skip, err := img.loadDDT2Table(e.Offset, &h)
		if err != nil {
			return err
		}
		if skip {
			return nil
		}
		img.userDataDDT2 = entries
		img.inMemoryDDT = true
		*found = true

	case DataTypeCdSectorPrefix, DataTypeCdSectorSuffix:
		entries, skip, err := img.loadDDT2Table(e.Offset, &h)
		if err != nil {
			return err
		}
		if skip {
			return nil
		}
		if e.DataType == DataTypeCdSectorPrefix {
			img.sectorPrefixDDT2 = entries
		} else {
			img.sectorSuffixDDT2 = entries
		}
	}
	return nil
}

// ddtSectorIndex converts a sector address into an index of the DDT v2
// tables, which start at the first negative sector (uint64 arithmetic wraps
// on purpose; callers bound-check the result).
func (img *Image) ddtSectorIndex(addr uint64, negative bool) uint64 {
	if negative {
		return uint64(img.userDataDDTHeader.Negative) - addr
	}
	return addr + uint64(img.userDataDDTHeader.Negative)
}

// decodeDDT2Entry splits a DDT v2 entry into the sector offset inside its
// block, the block file offset and the sector status.
func (img *Image) decodeDDT2Entry(entry uint64) (offset, blockOffset uint64, status SectorStatus) {
	if entry == 0 {
		return 0, 0, SectorStatusNotDumped
	}
	h := &img.userDataDDTHeader
	status = SectorStatus(entry >> ddtEntryStatusShift)
	entry &= ddtEntryPointerMask
	offset = entry & (uint64(1)<<h.DataShift - 1)
	blockOffset = (entry >> h.DataShift) << h.BlockAlignmentShift
	return offset, blockOffset, status
}

// decodeDDTEntryV2 looks up a sector in the version 2 user data tables,
// using the multi-level path when the primary table
// has a table shift. If a secondary table cannot be loaded and erasure
// coding recovery data is available, the table is recovered and the lookup
// retried.
func (img *Image) decodeDDTEntryV2(addr uint64, negative bool) (offset, blockOffset uint64, status SectorStatus, err error) {
	if img.userDataDDTHeader.TableShift == 0 {
		return img.decodeDDTSingleLevelV2(addr, negative)
	}
	offset, blockOffset, status, err = img.decodeDDTMultiLevelV2(addr, negative)
	if err != nil && img.ecRecoveryAvailable() {
		if img.recoverSecondaryDDT(addr, negative) {
			return img.decodeDDTMultiLevelV2(addr, negative)
		}
	}
	return offset, blockOffset, status, err
}

// decodeDDTSingleLevelV2 looks up a sector in a single-level table.
func (img *Image) decodeDDTSingleLevelV2(addr uint64, negative bool) (offset, blockOffset uint64, status SectorStatus, err error) {
	if img.userDataDDTHeader.TableShift != 0 {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("DDT table shift is not zero: %w", ErrCannotReadBlock)
	}
	idx := img.ddtSectorIndex(addr, negative)
	if idx >= uint64(len(img.userDataDDT2)) {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("sector %d beyond the DDT: %w", addr, ErrSectorOutOfBounds)
	}
	offset, blockOffset, status = img.decodeDDT2Entry(img.userDataDDT2[idx])
	return offset, blockOffset, status, nil
}

// secondaryDDTOffset returns the file offset of the secondary table covering
// table index idx and the number of sectors per secondary table.
func (img *Image) secondaryDDTOffset(idx uint64) (off, items uint64, err error) {
	h := &img.userDataDDTHeader
	if h.TableShift >= 64 {
		return 0, 0, fmt.Errorf("invalid DDT table shift %d: %w", h.TableShift, ErrCannotReadBlock)
	}
	items = uint64(1) << h.TableShift
	pos := idx / items
	if pos >= uint64(len(img.userDataDDT2)) {
		return 0, items, fmt.Errorf("primary DDT entry %d out of range: %w", pos, ErrSectorOutOfBounds)
	}
	return img.userDataDDT2[pos] << h.BlockAlignmentShift, items, nil
}

// decodeDDTMultiLevelV2 looks up a sector in a two-level table: the primary table
// holds the (aligned) offsets of secondary tables, one per 1<<tableShift
// sectors; the last used secondary table is cached.
//
// A primary entry of zero (no secondary table was ever written for that
// range) yields SectorStatusNotDumped.
func (img *Image) decodeDDTMultiLevelV2(addr uint64, negative bool) (offset, blockOffset uint64, status SectorStatus, err error) {
	if img.userDataDDTHeader.TableShift == 0 {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("DDT table shift is zero: %w", ErrCannotReadBlock)
	}
	idx := img.ddtSectorIndex(addr, negative)
	secOff, items, err := img.secondaryDDTOffset(idx)
	if err != nil {
		return 0, 0, SectorStatusNotDumped, err
	}
	if secOff == 0 && (img.cachedDDTOffset != 0 || img.cachedSecondaryDDT2 == nil) {
		return 0, 0, SectorStatusNotDumped, nil
	}

	if img.cachedDDTOffset != secOff {
		entries, err := img.loadSecondaryDDT(secOff)
		if err != nil {
			return 0, 0, SectorStatusNotDumped, err
		}
		img.cachedSecondaryDDT2 = entries
		img.cachedDDTOffset = secOff
	}

	i := idx % items
	if i >= uint64(len(img.cachedSecondaryDDT2)) {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("secondary DDT entry %d out of range: %w", i, ErrSectorOutOfBounds)
	}
	offset, blockOffset, status = img.decodeDDT2Entry(img.cachedSecondaryDDT2[i])
	return offset, blockOffset, status, nil
}

// loadSecondaryDDT reads, decompresses and CRC-checks the secondary table at
// off (the loading part of decodeDDTMultiLevelV2).
func (img *Image) loadSecondaryDDT(off uint64) ([]uint64, error) {
	var hb [ddtHeader2Size]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return nil, fmt.Errorf("reading secondary DDT header at %d: %w", off, ErrCannotReadBlock)
	}
	h, _ := unmarshal[DDTHeader2](hb[:])
	if (h.Identifier != BlockTypeDeDuplicationTableSecondary && h.Identifier != BlockTypeDeDuplicationTableSAlpha) ||
		h.Type != DataTypeUserData {
		return nil, fmt.Errorf("invalid secondary DDT header at %d: %w", off, ErrCannotReadBlock)
	}
	payloadOff := off + ddtHeader2Size
	var data []byte
	switch h.Compression {
	case CompressionLzma, CompressionZstd:
		if (h.Compression == CompressionLzma && h.CmpLength <= LZMAPropertiesLength) ||
			(h.Compression == CompressionZstd && h.CmpLength == 0) {
			return nil, fmt.Errorf("secondary DDT payload of %d bytes: %w", h.CmpLength, ErrCannotDecompressBlock)
		}
		raw, err := img.readAt(payloadOff, h.CmpLength)
		if err != nil {
			return nil, fmt.Errorf("reading secondary DDT at %d: %w", off, ErrCannotDecompressBlock)
		}
		if h.Length > maxBlockPayload || !plausibleLength(h.CmpLength, h.Length) {
			return nil, fmt.Errorf("secondary DDT length %d: %w", h.Length, ErrCannotDecompressBlock)
		}
		data, err = decodePayload(h.Compression, DataTypeUserData, raw, h.Length)
		if err != nil {
			return nil, fmt.Errorf("decompressing secondary DDT at %d: %w", off, err)
		}
	case CompressionNone:
		raw, err := img.readAt(payloadOff, h.Length)
		if err != nil {
			return nil, fmt.Errorf("reading secondary DDT at %d: %w", off, ErrCannotReadBlock)
		}
		data = raw
	default:
		return nil, fmt.Errorf("secondary DDT compression %v: %w", h.Compression, ErrCannotReadBlock)
	}
	if crc64.Checksum(data) != h.CRC64 {
		return nil, fmt.Errorf("secondary DDT at %d: %w", off, ErrInvalidBlockCRC)
	}
	return decodeUint64s(data), nil
}

// recoverSecondaryDDT reconstructs the secondary table covering the sector
// through erasure coding and installs it in the cache (the recovery branch
// of decodeDDTEntryV2). The recovered table's CRC is not checked, but
// undecodable payloads are rejected.
func (img *Image) recoverSecondaryDDT(addr uint64, negative bool) bool {
	secOff, _, err := img.secondaryDDTOffset(img.ddtSectorIndex(addr, negative))
	if err != nil {
		return false
	}
	raw, err := img.ecRecoverDDTBlock(secOff)
	if err != nil || len(raw) < ddtHeader2Size {
		return false
	}
	h, err := unmarshal[DDTHeader2](raw)
	if err != nil {
		return false
	}
	payload := raw[ddtHeader2Size:]
	var data []byte
	switch {
	case h.Compression == CompressionNone && uint64(len(payload)) >= h.Length:
		data = payload[:h.Length]
	case h.Compression == CompressionLzma && len(payload) > LZMAPropertiesLength,
		h.Compression == CompressionZstd && len(payload) > 0:
		if h.Length > maxBlockPayload || !plausibleLength(uint64(len(payload)), h.Length) {
			return false
		}
		data, err = decodePayload(h.Compression, DataTypeUserData, payload, h.Length)
		if err != nil {
			return false
		}
	default:
		return false
	}
	img.cachedSecondaryDDT2 = decodeUint64s(data)
	img.cachedDDTOffset = secOff
	return true
}
