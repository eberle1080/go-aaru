package aaru

import (
	"fmt"

	"github.com/eberle1080/go-aaru/crc64"
)

// Data block processing at open time.

// maxExpansionRatio bounds the ratio between the decompressed and the
// compressed size of a payload that the read path accepts. Real LZMA, Zstd
// and FLAC streams stay far below it (LZMA peaks around 7000:1, Zstd RLE
// blocks around 43000:1); corrupt length fields above it are rejected as
// undecompressible instead of triggering huge allocations.
const maxExpansionRatio = 1 << 16

// plausibleLength reports whether a payload of cmpLen bytes may decompress
// to length bytes.
func plausibleLength(cmpLen, length uint64) bool {
	if length <= 1<<20 {
		return true
	}
	return cmpLen >= length/maxExpansionRatio
}

// processDataBlock loads the data block referenced by e when it holds a media
// tag or a per-sector tag arena. Unreadable or damaged blocks are skipped
// (nil error); only decompression
// failures are fatal.
func (img *Image) processDataBlock(e IndexEntry) error {
	// NOP block, or user data handled through the DDT.
	if e.DataType == DataTypeNone || (e.DataType == DataTypeUserData && img.header.BiggestSectorSize > 0) {
		return nil
	}
	hdr, err := img.readBlockHeaderAt(e.Offset)
	if err != nil {
		return nil
	}
	img.info.ImageSize += uint64(hdr.CmpLength)

	if e.DataType == DataTypeUserData {
		if hdr.SectorSize > img.info.SectorSize {
			img.info.SectorSize = hdr.SectorSize
		}
		return nil
	}
	if hdr.Identifier != e.BlockType || hdr.Type != e.DataType {
		return nil
	}

	data, ok, err := img.loadDataBlockPayload(e.Offset, &hdr)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if hdr.Length > 0 && !img.crcMatches(crc64.Checksum(data), hdr.CRC64) {
		// Attempt erasure coding recovery; skip the block if it fails.
		rec, ok := img.recoverMetaDataBlock(e.Offset)
		if !ok {
			return nil
		}
		hdr, data = rec.hdr, rec.data
	}

	img.storeDataBlock(e.DataType, &hdr, data)
	return nil
}

// loadDataBlockPayload reads and decompresses the payload of the block whose
// header hdr is at off, with the failure semantics of processDataBlock:
// ok is false when the block must be skipped, err is non-nil when opening
// must fail.
func (img *Image) loadDataBlockPayload(off uint64, hdr *BlockHeader) (data []byte, ok bool, err error) {
	length := uint64(hdr.Length)
	payloadOff := off + blockHeaderSize
	decompressErr := func(cause error) error {
		return fmt.Errorf("decompressing %v block at %d: %w: %w", hdr.Type, off, ErrCannotDecompressBlock, cause)
	}

	switch hdr.Compression {
	case CompressionLzma, CompressionLzmaCst:
		if hdr.Compression == CompressionLzmaCst && hdr.Type != DataTypeCdSubchannel {
			return nil, false, nil
		}
		if hdr.CmpLength < LZMAPropertiesLength {
			return nil, false, decompressErr(fmt.Errorf("payload of %d bytes", hdr.CmpLength))
		}
		raw, rerr := img.readAt(payloadOff, uint64(hdr.CmpLength))
		if rerr != nil {
			return nil, false, nil
		}
		if length == 0 {
			if hdr.CmpLength != LZMAPropertiesLength {
				return nil, false, nil
			}
			return nil, true, nil
		}
		if !plausibleLength(uint64(hdr.CmpLength), length) {
			return nil, false, decompressErr(fmt.Errorf("implausible length %d", length))
		}
		data, err = decodePayload(hdr.Compression, hdr.Type, raw, length)
		if err != nil {
			return nil, false, decompressErr(err)
		}
		return data, true, nil

	case CompressionZstd, CompressionZstdCst:
		if hdr.Compression == CompressionZstdCst && hdr.Type != DataTypeCdSubchannel {
			return nil, false, nil
		}
		raw, rerr := img.readAt(payloadOff, uint64(hdr.CmpLength))
		if rerr != nil {
			return nil, false, nil
		}
		if length == 0 {
			return nil, true, nil
		}
		if !plausibleLength(uint64(hdr.CmpLength), length) {
			return nil, false, decompressErr(fmt.Errorf("implausible length %d", length))
		}
		data, err = decodePayload(hdr.Compression, hdr.Type, raw, length)
		if err != nil {
			return nil, false, decompressErr(err)
		}
		return data, true, nil

	case CompressionNone:
		if length == 0 {
			return nil, true, nil
		}
		raw, rerr := img.readAt(payloadOff, length)
		if rerr != nil {
			return nil, false, nil
		}
		return raw, true, nil

	default:
		return nil, false, nil
	}
}

// recoveredBlock is a data block reconstructed by erasure coding.
type recoveredBlock struct {
	hdr  BlockHeader
	data []byte
}

// recoverMetaDataBlock asks the erasure coding layer for the raw bytes of the
// damaged block at off and decodes them (the recovery branch of
// processDataBlock). A recovered payload that does not decompress to the
// advertised length is rejected.
func (img *Image) recoverMetaDataBlock(off uint64) (recoveredBlock, bool) {
	raw, err := img.ecRecoverMetaBlock(off)
	if err != nil || len(raw) <= blockHeaderSize {
		return recoveredBlock{}, false
	}
	hdr, err := decodeBlockHeader(raw)
	if err != nil {
		return recoveredBlock{}, false
	}
	payload := raw[blockHeaderSize:]
	length := uint64(hdr.Length)
	var data []byte
	switch hdr.Compression {
	case CompressionNone:
		if uint64(len(payload)) < length {
			return recoveredBlock{}, false
		}
		data = payload[:length:length]
	case CompressionLzma, CompressionZstd:
		if !plausibleLength(uint64(len(payload)), length) {
			return recoveredBlock{}, false
		}
		data, err = decodePayload(hdr.Compression, hdr.Type, payload, length)
		if err != nil {
			return recoveredBlock{}, false
		}
	default:
		return recoveredBlock{}, false
	}
	return recoveredBlock{hdr: hdr, data: data}, true
}

// storeDataBlock files a decoded data block as a per-sector tag arena or a
// media tag and marks the corresponding sector tags readable.
func (img *Image) storeDataBlock(dt DataType, hdr *BlockHeader, data []byte) {
	t := &img.readableSectorTags
	switch dt {
	case DataTypeCdSectorPrefix, DataTypeCdSectorPrefixCorrected:
		if dt == DataTypeCdSectorPrefixCorrected {
			img.sectorPrefixCorrected = data
		} else {
			img.sectorPrefix = data
		}
		t[SectorTagCdSync] = true
		t[SectorTagCdHeader] = true
	case DataTypeCdSectorSuffix, DataTypeCdSectorSuffixCorrected:
		if dt == DataTypeCdSectorSuffixCorrected {
			img.sectorSuffixCorrected = data
		} else {
			img.sectorSuffix = data
		}
		t[SectorTagCdSubHeader] = true
		t[SectorTagCdEcc] = true
		t[SectorTagCdEccP] = true
		t[SectorTagCdEccQ] = true
		t[SectorTagCdEdc] = true
	case DataTypeCdSubchannel:
		img.sectorSubchannel = data
		t[SectorTagCdSubchannel] = true
	case DataTypeAppleProfileTag:
		img.sectorSubchannel = data
		t[SectorTagAppleProfile] = true
	case DataTypeAppleSonyTag:
		img.sectorSubchannel = data
		t[SectorTagAppleSony] = true
	case DataTypePriamDataTowerTag:
		img.sectorSubchannel = data
		t[SectorTagPriamDataTower] = true
	case DataTypeCdSubHeader:
		img.mode2Subheaders = data
	case DataTypeDvdSectorId:
		img.sectorID = data
		t[SectorTagDvdSectorNumber] = true
		t[SectorTagDvdSectorInformation] = true
	case DataTypeDvdSectorIed:
		img.sectorIED = data
		t[SectorTagDvdSectorIed] = true
	case DataTypeDvdSectorCprMai:
		img.sectorCPRMAI = data
		t[SectorTagDvdCmi] = true
	case DataTypeDvdSectorEdc:
		img.sectorEDC = data
		t[SectorTagDvdSectorEdc] = true
	case DataTypeDvdTitleKeyDecrypted:
		img.sectorDecryptedTitleKey = data
		t[SectorTagDvdTitleKeyDecrypted] = true
	case DataTypeBdSectorEdc:
		img.sectorEDC = data
		t[SectorTagBdSectorEdc] = true
	default:
		tag, ok := dataTypeToMediaTag[hdr.Type]
		if !ok {
			tag = -1 // unknown
		}
		if img.mediaTags == nil {
			img.mediaTags = make(map[MediaTagType][]byte)
		}
		if data == nil {
			data = []byte{}
		}
		img.mediaTags[tag] = data
	}
}
