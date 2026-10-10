package aaru

import "fmt"

// Legacy (version 1) deduplication tables.

// readDDTPayload reads and decompresses a DDT payload of cmpLength bytes at
// off into length bytes. LZMA payloads must hold more than the properties.
// skip is true when the table must be ignored (read failure); err is non-nil
// when opening must fail (decompression failure).
func (img *Image) readDDTPayload(off uint64, c CompressionType, cmpLength, length uint64) (data []byte, skip bool, err error) {
	switch c {
	case CompressionLzma:
		if cmpLength <= LZMAPropertiesLength {
			return nil, false, fmt.Errorf("LZMA DDT payload of %d bytes: %w", cmpLength, ErrCannotDecompressBlock)
		}
	case CompressionZstd:
	default:
		return nil, true, nil
	}
	raw, rerr := img.readAt(off, cmpLength)
	if rerr != nil {
		return nil, true, nil
	}
	if length > maxBlockPayload || !plausibleLength(cmpLength, length) {
		return nil, false, fmt.Errorf("DDT length %d: %w", length, ErrCannotDecompressBlock)
	}
	data, err = decodePayload(c, DataTypeUserData, raw, length)
	if err != nil {
		return nil, false, fmt.Errorf("decompressing DDT at %d: %w", off, err)
	}
	return data, false, nil
}

// processDDTv1 loads a version 1 deduplication table: the
// user data table, or the CD prefix/suffix correction tables. Any DDT v1
// block counts as the user data table being found
// unless the user data table uses an unknown compression.
func (img *Image) processDDTv1(e IndexEntry, found *bool) error {
	var hb [ddtHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading DDT header at %d: %w", e.Offset, ErrCannotReadBlock)
	}
	h, _ := unmarshal[DDTHeader](hb[:])
	*found = true
	img.info.ImageSize += h.CmpLength
	payloadOff := e.Offset + ddtHeaderSize

	switch e.DataType {
	case DataTypeUserData:
		img.info.Sectors = h.Entries
		img.shift = h.Shift
		img.ddtVersion = 1
		var data []byte
		switch h.Compression {
		case CompressionLzma, CompressionZstd:
			d, skip, err := img.readDDTPayload(payloadOff, h.Compression, h.CmpLength, h.Length)
			if err != nil {
				return err
			}
			if skip {
				return nil
			}
			data = d
		case CompressionNone:
			if h.Entries > uint64(img.size)/8 {
				return nil
			}
			d, err := img.readAt(payloadOff, h.Entries*8)
			if err != nil {
				return nil
			}
			data = d
		default:
			*found = false
			return nil
		}
		img.userDataDDT = decodeUint64s(data)
		img.inMemoryDDT = true

	case DataTypeCdSectorPrefixCorrected, DataTypeCdSectorSuffixCorrected:
		var data []byte
		switch h.Compression {
		case CompressionLzma, CompressionZstd:
			d, skip, err := img.readDDTPayload(payloadOff, h.Compression, h.CmpLength, h.Length)
			if err != nil {
				return err
			}
			if skip {
				return nil
			}
			data = d
		case CompressionNone:
			if h.Entries > uint64(img.size)/4 {
				return nil
			}
			d, err := img.readAt(payloadOff, h.Entries*4)
			if err != nil {
				return nil
			}
			data = d
		default:
			return nil
		}
		if e.DataType == DataTypeCdSectorPrefixCorrected {
			img.sectorPrefixDDT = decodeUint32s(data)
		} else {
			img.sectorSuffixDDT = decodeUint32s(data)
		}
	}
	return nil
}

// decodeDDTEntryV1 looks up a sector in the version 1 user data table.
// Version 1 tables store absolute block offsets.
func (img *Image) decodeDDTEntryV1(addr uint64) (offset, blockOffset uint64, status SectorStatus, err error) {
	if img.userDataDDT == nil {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("user data DDT not loaded: %w", ErrNotAaruFormat)
	}
	if img.shift >= 64 {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("invalid DDT shift %d: %w", img.shift, ErrIncorrectDataSize)
	}
	if addr >= uint64(len(img.userDataDDT)) {
		return 0, 0, SectorStatusNotDumped, fmt.Errorf("sector %d beyond the DDT: %w", addr, ErrSectorOutOfBounds)
	}
	entry := img.userDataDDT[addr]
	offset = entry & (uint64(1)<<img.shift - 1)
	blockOffset = entry >> img.shift
	// Partially written image: the real sector size cannot be known.
	if entry == 0 {
		status = SectorStatusNotDumped
	} else {
		status = SectorStatusDumped
	}
	return offset, blockOffset, status, nil
}
