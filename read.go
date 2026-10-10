package aaru

import (
	"errors"
	"fmt"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/compression/flac"
	"github.com/eberle1080/go-aaru/media"
)

// Sector and media tag reading.

// maxDataBlockSectorSize is the largest sector size a data block header may
// declare before it is considered corrupt (a sanity check).
const maxDataBlockSectorSize = 65536

// errBlockCorrupt marks data block failures that are routed to erasure
// coding recovery (and reported as ErrCannotDecompressBlock).
var errBlockCorrupt = errors.New("corrupt data block")

// ReadMediaTag returns a copy of the media tag t, or
// ErrMediaTagNotPresent.
func (img *Image) ReadMediaTag(t MediaTagType) ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	d, ok := img.mediaTags[t]
	if !ok {
		return nil, fmt.Errorf("media tag %v: %w", t, ErrMediaTagNotPresent)
	}
	return append([]byte{}, d...), nil
}

// ReadSector reads the user data of a sector. Negative
// sectors (lead-in, before LBA 0) are addressed with negative set and addr
// counting backwards from 1; overflow sectors (lead-out, after the last user
// sector) follow the user sectors.
//
// It returns the data and the sector status recorded in the image. Sectors
// that were never dumped return (nil, SectorStatusNotDumped,
// ErrSectorNotDumped). Sectors stored decrypted from PS3, Wii and Wii U
// discs are re-encrypted, and generable GameCube/Wii junk is regenerated, so
// that the original disc contents are returned (with SectorStatusDumped).
func (img *Image) ReadSector(addr uint64, negative bool) ([]byte, SectorStatus, error) {
	if img.closed {
		return nil, SectorStatusNotDumped, ErrClosed
	}
	return img.readSector(addr, negative)
}

// checkSectorBounds applies the address checks shared by the sector read
// functions.
func (img *Image) checkSectorBounds(addr uint64, negative bool) error {
	if img.noUserDataDDT {
		return ErrUserDataNotPresent
	}
	if negative && addr > uint64(img.userDataDDTHeader.Negative) {
		return fmt.Errorf("negative sector %d: %w", addr, ErrSectorOutOfBounds)
	}
	// This wraps (accepting every address) when the image
	// has neither user nor overflow sectors; the DDT lookup bounds it then.
	if !negative && addr > img.info.Sectors+uint64(img.userDataDDTHeader.Overflow)-1 {
		return fmt.Errorf("sector %d: %w", addr, ErrSectorOutOfBounds)
	}
	return nil
}

// decodeSector looks up a sector in the deduplication tables.
func (img *Image) decodeSector(addr uint64, negative bool) (offset, blockOffset uint64, status SectorStatus, err error) {
	switch img.ddtVersion {
	case 1:
		if negative {
			return 0, 0, SectorStatusNotDumped, fmt.Errorf("negative sectors in a version 1 image: %w", ErrSectorOutOfBounds)
		}
		return img.decodeDDTEntryV1(addr)
	case 2:
		return img.decodeDDTEntryV2(addr, negative)
	}
	return 0, 0, SectorStatusNotDumped, nil
}

// readSector implements ReadSector.
func (img *Image) readSector(addr uint64, negative bool) ([]byte, SectorStatus, error) {
	if err := img.checkSectorBounds(addr, negative); err != nil {
		return nil, SectorStatusNotDumped, err
	}
	offset, blockOffset, status, err := img.decodeSector(addr, negative)
	if err != nil {
		return nil, status, err
	}
	if status == SectorStatusNotDumped {
		return nil, SectorStatusNotDumped, ErrSectorNotDumped
	}

	// GameCube/Wii: generable sectors are regenerated junk; no block exists.
	mt := media.Type(img.header.MediaType)
	if status == SectorStatusGenerable && (mt == media.GOD || mt == media.WOD) {
		data, err := img.readGenerable(addr)
		if err != nil {
			return nil, status, err
		}
		return data, SectorStatusDumped, nil
	}

	hdr, err := img.cachedBlockHeader(blockOffset)
	if err != nil {
		return nil, status, err
	}
	data, err := img.sectorFromBlock(blockOffset, &hdr, offset)
	if err != nil {
		if !errors.Is(err, errBlockCorrupt) {
			return nil, status, err
		}
		// Erasure coding recovery of a block that failed to decode. The
		// recovered sector is re-encrypted below like any other.
		var rec []byte
		rerr := err
		if img.ecRecoveryAvailable() {
			rec, rerr = img.ecRecoverDataBlock(blockOffset, offset, status)
		}
		if rerr != nil {
			return nil, status, fmt.Errorf("data block at %d: %w: %w", blockOffset, ErrCannotDecompressBlock, err)
		}
		data = rec
	}

	status = img.consoleReencrypt(addr, data, status)
	return data, status, nil
}

// cachedBlockHeader returns the data block header at off, through the block
// header cache.
func (img *Image) cachedBlockHeader(off uint64) (BlockHeader, error) {
	if v, ok := img.blockHeaderCache.get(off); ok {
		return v.(BlockHeader), nil
	}
	hdr, err := img.readBlockHeaderAt(off)
	if err != nil {
		return BlockHeader{}, err
	}
	img.blockHeaderCache.add(off, hdr, blockHeaderSize)
	return hdr, nil
}

// sectorFromBlock returns a copy of sector offset of the data block at off,
// decompressing (and caching) the block if needed. Errors wrapping
// errBlockCorrupt are eligible for erasure coding recovery.
func (img *Image) sectorFromBlock(off uint64, hdr *BlockHeader, offset uint64) ([]byte, error) {
	// A corrupt header has a garbage identifier or sector size.
	if hdr.Identifier != BlockTypeDataBlock || hdr.SectorSize == 0 || hdr.SectorSize > maxDataBlockSectorSize {
		return nil, fmt.Errorf("%w: bad header (identifier %#x, sector size %d)", errBlockCorrupt, uint32(hdr.Identifier), hdr.SectorSize)
	}
	var block []byte
	if v, ok := img.blockCache.get(off); ok {
		block = v.([]byte)
	} else {
		b, err := img.decompressDataBlock(off, hdr)
		if err != nil {
			return nil, err
		}
		block = img.ecCheckDataBlock(off, hdr, b)
		img.blockCache.add(off, block, uint64(len(block)))
	}
	size := uint64(hdr.SectorSize)
	if offset >= uint64(len(block))/size {
		return nil, fmt.Errorf("%w: sector %d beyond block of %d bytes", errBlockCorrupt, offset, len(block))
	}
	return append([]byte(nil), block[offset*size:(offset+1)*size]...), nil
}

// decompressDataBlock reads and decompresses the user data block at off
// (the decompression step of readSector). The block CRC is not verified
// here (Verify does that).
func (img *Image) decompressDataBlock(off uint64, hdr *BlockHeader) ([]byte, error) {
	payloadOff := off + blockHeaderSize
	length := uint64(hdr.Length)
	cmpLength := uint64(hdr.CmpLength)
	switch hdr.Compression {
	case CompressionNone:
		block, err := img.readAt(payloadOff, length)
		if err != nil {
			return nil, fmt.Errorf("reading block at %d: %w: %w", off, ErrCannotReadBlock, err)
		}
		return block, nil
	case CompressionLzma:
		if cmpLength <= LZMAPropertiesLength || length == 0 {
			return nil, fmt.Errorf("%w: LZMA lengths %d/%d", errBlockCorrupt, cmpLength, length)
		}
	case CompressionZstd:
		if cmpLength == 0 || length == 0 {
			return nil, fmt.Errorf("%w: zstd lengths %d/%d", errBlockCorrupt, cmpLength, length)
		}
	case CompressionFlac:
	default:
		return nil, fmt.Errorf("%w: unsupported compression %v", errBlockCorrupt, hdr.Compression)
	}
	if !plausibleLength(cmpLength, length) {
		return nil, fmt.Errorf("%w: implausible length %d for %d compressed bytes", errBlockCorrupt, length, cmpLength)
	}
	// A block never holds more sectors than its DDT entries can address
	// (blocks are closed at 1 << dataShift sectors).
	if shift := img.dataBlockShift(); shift < 48 && length > uint64(hdr.SectorSize)<<shift {
		return nil, fmt.Errorf("%w: block length %d exceeds %d sectors of %d bytes", errBlockCorrupt, length,
			uint64(1)<<shift, hdr.SectorSize)
	}
	raw, err := img.readAt(payloadOff, cmpLength)
	if err != nil {
		return nil, fmt.Errorf("%w: reading payload: %w", errBlockCorrupt, err)
	}
	if hdr.Compression == CompressionFlac {
		block := make([]byte, length)
		n, err := flac.DecodeRedbookInto(block, raw)
		if err != nil || uint64(n) != length {
			return nil, fmt.Errorf("%w: FLAC decoded %d of %d bytes: %v", errBlockCorrupt, n, length, err)
		}
		return block, nil
	}
	block, err := decodePayload(hdr.Compression, hdr.Type, raw, length)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBlockCorrupt, err)
	}
	return block, nil
}

// dataBlockShift returns log2 of the maximum number of sectors a user data
// block may hold: the DDT v2 data shift, or the DDT v1 shift.
func (img *Image) dataBlockShift() uint8 {
	if img.ddtVersion == 1 {
		return img.shift
	}
	return img.userDataDDTHeader.DataShift
}

// ReadTrackSector reads sector addr relative to the start of data track
// track of an optical disc.
func (img *Image) ReadTrackSector(addr uint64, track uint8) ([]byte, SectorStatus, error) {
	if img.closed {
		return nil, SectorStatusNotDumped, ErrClosed
	}
	if img.info.MetadataMediaType != media.OpticalDisc {
		return nil, SectorStatusNotDumped, ErrIncorrectMediaType
	}
	for _, t := range img.dataTracks {
		if t.Sequence == track {
			return img.readSector(uint64(t.Start)+addr, false)
		}
	}
	return nil, SectorStatusNotDumped, fmt.Errorf("track %d: %w", track, ErrTrackNotFound)
}

// sectorTagArena returns the size bytes of entry idx of a per-sector arena.
func sectorTagArena(arena []byte, idx uint64, size uint64) ([]byte, bool) {
	if arena == nil || idx >= uint64(len(arena))/size {
		return nil, false
	}
	return arena[idx*size : (idx+1)*size], true
}

var errArenaTooShort = fmt.Errorf("sector tag arena too short: %w", ErrCannotReadBlock)

// dvdLongMedia lists the media whose long sectors are 2064-byte DVD sectors
// with ID, IED, CPR_MAI and EDC.
var dvdLongMedia = map[media.Type]bool{
	media.DVDROM: true, media.PS2DVD: true, media.SACD: true, media.PS3DVD: true, media.DVDR: true,
	media.DVDRW: true, media.DVDPR: true, media.DVDPRW: true, media.DVDPRWDL: true, media.DVDRDL: true,
	media.DVDPRDL: true, media.DVDRAM: true, media.DVDRWDL: true, media.DVDDownload: true, media.Nuon: true,
}

// bdLongMedia lists the media whose long sectors are 2052-byte BD sectors
// (user data plus EDC).
var bdLongMedia = map[media.Type]bool{
	media.BDROM: true, media.BDR: true, media.BDRE: true, media.BDRXL: true, media.BDREXL: true,
	media.UHDBD: true, media.XGD4: true, media.PS3BD: true, media.PS4BD: true, media.PS5BD: true,
}

// ReadSectorLong reads a sector including its tags:
// 2352-byte raw CD sectors (sync, header, subheader, user data, EDC/ECC,
// regenerated when they were stored as correct), 2064-byte DVD and
// GameCube/Wii sectors, 2052-byte BD sectors and 512-byte Apple/Priam
// sectors followed by their tag. Media without long sectors, or images
// without the needed tag data, return the plain sector.
//
// When the user data is present but a CD prefix or suffix was not dumped the
// result is (nil, status, ErrSectorNotDumped).
func (img *Image) ReadSectorLong(addr uint64, negative bool) ([]byte, SectorStatus, error) {
	if img.closed {
		return nil, SectorStatusNotDumped, ErrClosed
	}
	return img.readSectorLong(addr, negative)
}

func (img *Image) readSectorLong(addr uint64, negative bool) ([]byte, SectorStatus, error) {
	if err := img.checkSectorBounds(addr, negative); err != nil {
		return nil, SectorStatusNotDumped, err
	}
	corrected := img.ddtSectorIndex(addr, negative)
	mt := media.Type(img.header.MediaType)

	switch img.info.MetadataMediaType {
	case media.OpticalDisc:
		switch {
		case dvdLongMedia[mt], mt == media.GOD || mt == media.WOD:
			if img.sectorID == nil || img.sectorIED == nil || img.sectorCPRMAI == nil || img.sectorEDC == nil {
				return img.readSector(addr, negative)
			}
			bare, status, err := img.readSector(addr, negative)
			if err != nil {
				return nil, status, err
			}
			if len(bare) == 0 {
				return nil, status, ErrIncorrectDataSize
			}
			id, ok1 := sectorTagArena(img.sectorID, corrected, 4)
			ied, ok2 := sectorTagArena(img.sectorIED, corrected, 2)
			cpr, ok3 := sectorTagArena(img.sectorCPRMAI, corrected, 6)
			edc, ok4 := sectorTagArena(img.sectorEDC, corrected, 4)
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return nil, status, errArenaTooShort
			}
			data := make([]byte, 2064)
			copy(data[0:4], id)
			copy(data[4:6], ied)
			if dvdLongMedia[mt] {
				copy(data[6:12], cpr)
				copy(data[12:2060], bare)
			} else {
				copy(data[6:2054], bare)
				copy(data[2054:2060], cpr)
			}
			copy(data[2060:2064], edc)
			return data, status, nil

		case bdLongMedia[mt]:
			if img.sectorEDC == nil {
				return img.readSector(addr, negative)
			}
			bare, status, err := img.readSector(addr, negative)
			if err != nil {
				return nil, status, err
			}
			if len(bare) == 0 {
				return nil, status, ErrIncorrectDataSize
			}
			edc, ok := sectorTagArena(img.sectorEDC, corrected, 4)
			if !ok {
				return nil, status, errArenaTooShort
			}
			data := make([]byte, 2052)
			copy(data[0:2048], bare)
			copy(data[2048:2052], edc)
			return data, status, nil
		}
		return img.readCDSectorLong(addr, negative, corrected)

	case media.BlockMedia:
		var tagLength uint64
		switch mt {
		case media.AppleFileWare, media.AppleProfile, media.AppleWidget:
			tagLength = 20
		case media.AppleSonySS, media.AppleSonyDS:
			tagLength = 12
		case media.PriamDataTower:
			tagLength = 24
		default:
			return nil, SectorStatusNotDumped, fmt.Errorf("long sectors of %v: %w", mt, ErrIncorrectMediaType)
		}
		if img.sectorSubchannel == nil {
			return img.readSector(addr, negative)
		}
		bare, status, err := img.readSector(addr, negative)
		if err != nil {
			return nil, status, err
		}
		if len(bare) > 512 {
			return nil, status, fmt.Errorf("sector of %d bytes: %w", len(bare), ErrBufferTooSmall)
		}
		if len(bare) != 512 {
			return nil, status, fmt.Errorf("sector of %d bytes, expected 512: %w", len(bare), ErrIncorrectDataSize)
		}
		tag, ok := sectorTagArena(img.sectorSubchannel, corrected, tagLength)
		if !ok {
			return nil, status, errArenaTooShort
		}
		data := make([]byte, 512+tagLength)
		copy(data, bare)
		copy(data[512:], tag)
		return data, status, nil
	}
	return nil, SectorStatusNotDumped, fmt.Errorf("long sectors of %v: %w", mt, ErrIncorrectMediaType)
}

// readCDSectorLong rebuilds a raw 2352-byte CD sector (the Compact Disc part
// of ReadSectorLong).
func (img *Image) readCDSectorLong(addr uint64, negative bool, corrected uint64) ([]byte, SectorStatus, error) {
	if (img.sectorSuffix == nil || img.sectorPrefix == nil) &&
		(img.sectorSuffixDDT == nil || img.sectorPrefixDDT == nil) &&
		(img.sectorSuffixDDT2 == nil || img.sectorPrefixDDT2 == nil) {
		return img.readSector(addr, negative)
	}

	bare, status, err := img.readSector(addr, negative)
	if err != nil {
		return nil, status, err
	}
	if len(bare) == 0 {
		return nil, status, ErrIncorrectDataSize
	}

	// The (unsigned, non negative-adjusted) address is compared with the
	// signed track bounds converted to unsigned.
	var trk TrackEntry
	found := false
	for _, t := range img.dataTracks {
		if addr >= uint64(t.Start) && addr <= uint64(t.End) {
			trk, found = t, true
			break
		}
	}
	if !found {
		return nil, status, fmt.Errorf("sector %d: %w", addr, ErrTrackNotFound)
	}

	// LBA as encoded in a CD sector header (lead-in sectors are negative).
	signedLBA := int64(addr)
	if negative {
		signedLBA = -int64(addr)
	}

	switch trk.Type {
	case TrackTypeAudio, TrackTypeData:
		return bare, status, nil
	case TrackTypeCdMode1, TrackTypeCdMode2Formless, TrackTypeCdMode2Form1, TrackTypeCdMode2Form2:
	default:
		return nil, status, fmt.Errorf("track type %v: %w", trk.Type, ErrInvalidTrackFormat)
	}

	data := make([]byte, cdecc.SectorSize)
	mode1 := trk.Type == TrackTypeCdMode1
	if mode1 {
		copy(data[16:16+2048], bare)
	}

	// Prefix (sync + header).
	notDumped, err := img.cdPrefix(data, trk.Type, corrected, addr, signedLBA)
	if err != nil {
		return nil, SectorStatusErrored, err
	}
	if notDumped {
		return nil, status, ErrSectorNotDumped
	}

	if mode1 {
		notDumped, err = img.cdMode1Suffix(data, corrected)
	} else {
		notDumped, err = img.cdMode2Body(data, bare, corrected)
	}
	if err != nil {
		return nil, SectorStatusErrored, err
	}
	if notDumped {
		return nil, status, ErrSectorNotDumped
	}
	return data, status, nil
}

// cdPrefix fills the 16-byte prefix of data from the prefix tables, or
// regenerates it. It reports whether the prefix was not dumped.
func (img *Image) cdPrefix(data []byte, tt TrackType, corrected, addr uint64, signedLBA int64) (bool, error) {
	ct := cdecc.TrackType(tt)
	switch {
	case img.sectorPrefixDDT2 != nil:
		if corrected >= uint64(len(img.sectorPrefixDDT2)) {
			return false, fmt.Errorf("sector prefix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorPrefixDDT2[corrected]
		st := SectorStatus(entry >> ddtEntryStatusShift)
		idx := entry & ddtEntryPointerMask
		regenerable := st == SectorStatusMode1Correct
		if tt != TrackTypeCdMode1 {
			regenerable = st == SectorStatusMode2Form1Ok || st == SectorStatusMode2Form2Ok
		}
		switch {
		case regenerable:
			_ = cdecc.ReconstructPrefix(data, ct, signedLBA)
		case st == SectorStatusNotDumped:
			return true, nil
		default:
			p, ok := sectorTagArena(img.sectorPrefix, idx, 16)
			if !ok {
				return false, fmt.Errorf("sector prefix index %d outside stored prefix block: %w", idx, ErrCannotReadBlock)
			}
			copy(data[:16], p)
		}
	case img.sectorPrefix != nil:
		p, ok := sectorTagArena(img.sectorPrefix, corrected, 16)
		if !ok {
			return false, errArenaTooShort
		}
		copy(data[:16], p)
	case img.sectorPrefixDDT != nil:
		if corrected >= uint64(len(img.sectorPrefixDDT)) {
			return false, fmt.Errorf("sector prefix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorPrefixDDT[corrected]
		switch CdFixFlags(entry & cdXFixMask) {
		case CdFixCorrect:
			_ = cdecc.ReconstructPrefix(data, ct, int64(addr))
		case CdFixNotDumped:
			return true, nil
		default:
			p, ok := sectorTagArena(img.sectorPrefixCorrected, uint64(entry&cdDFixMask)-1, 16)
			if !ok {
				return false, errArenaTooShort
			}
			copy(data[:16], p)
		}
	default:
		return false, ErrReachedUnreachableCode
	}
	return false, nil
}

// cdMode1Suffix fills the 288-byte Mode 1 suffix (EDC, reserved, ECC) of
// data. It reports whether the suffix was not dumped.
func (img *Image) cdMode1Suffix(data []byte, corrected uint64) (bool, error) {
	switch {
	case img.sectorSuffixDDT2 != nil:
		if corrected >= uint64(len(img.sectorSuffixDDT2)) {
			return false, fmt.Errorf("sector suffix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorSuffixDDT2[corrected]
		st := SectorStatus(entry >> ddtEntryStatusShift)
		idx := entry & ddtEntryPointerMask
		switch {
		case st == SectorStatusMode1Correct:
			_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode1)
		case st == SectorStatusNotDumped:
			return true, nil
		default:
			s, ok := sectorTagArena(img.sectorSuffix, idx, 288)
			if !ok {
				return false, fmt.Errorf("sector suffix index %d outside stored suffix block: %w", idx, ErrCannotReadBlock)
			}
			copy(data[2064:], s)
		}
	case img.sectorSuffix != nil:
		s, ok := sectorTagArena(img.sectorSuffix, corrected, 288)
		if !ok {
			return false, errArenaTooShort
		}
		copy(data[2064:], s)
	case img.sectorSuffixDDT != nil:
		if corrected >= uint64(len(img.sectorSuffixDDT)) {
			return false, fmt.Errorf("sector suffix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorSuffixDDT[corrected]
		switch CdFixFlags(entry & cdXFixMask) {
		case CdFixCorrect:
			_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode1)
		case CdFixNotDumped:
			return true, nil
		default:
			s, ok := sectorTagArena(img.sectorSuffixCorrected, uint64(entry&cdDFixMask)-1, 288)
			if !ok {
				return false, errArenaTooShort
			}
			copy(data[2064:], s)
		}
	default:
		return false, ErrReachedUnreachableCode
	}
	return false, nil
}

// cdMode2Body fills the subheader, user data and EDC/ECC of a Mode 2 sector.
// It reports whether the suffix was not dumped.
func (img *Image) cdMode2Body(data, bare []byte, corrected uint64) (bool, error) {
	// isForm2 checks both subheader copies, like the write path.
	isForm2 := func() bool { return data[0x12]&0x20 != 0 || data[0x16]&0x20 != 0 }

	switch {
	case img.mode2Subheaders != nil && img.sectorSuffixDDT2 != nil:
		sh, ok := sectorTagArena(img.mode2Subheaders, corrected, 8)
		if !ok {
			return false, errArenaTooShort
		}
		copy(data[16:24], sh)
		if corrected >= uint64(len(img.sectorSuffixDDT2)) {
			return false, fmt.Errorf("sector suffix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorSuffixDDT2[corrected]
		st := SectorStatus(entry >> ddtEntryStatusShift)
		idx := entry & ddtEntryPointerMask
		switch st {
		case SectorStatusMode2Form1Ok:
			copy(data[24:24+2048], bare)
			_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode2Form1)
		case SectorStatusMode2Form2Ok, SectorStatusMode2Form2NoCrc:
			copy(data[24:24+2324], bare)
			if st == SectorStatusMode2Form2Ok {
				_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode2Form2)
			} else {
				clear(data[2348:2352])
			}
		case SectorStatusNotDumped:
			return true, nil
		default:
			// ECC/EDC failed: restore user data and the stored suffix.
			s, ok := sectorTagArena(img.sectorSuffix, idx, 288)
			if !ok {
				return false, fmt.Errorf("sector suffix index %d outside stored suffix block: %w", idx, ErrCannotReadBlock)
			}
			if isForm2() {
				copy(data[24:24+2324], bare)
				copy(data[2348:2352], s[:4])
			} else {
				copy(data[24:24+2048], bare)
				copy(data[2072:2352], s[:280])
			}
		}
	case img.mode2Subheaders != nil && img.sectorSuffixDDT != nil:
		sh, ok := sectorTagArena(img.mode2Subheaders, corrected, 8)
		if !ok {
			return false, errArenaTooShort
		}
		copy(data[16:24], sh)
		if corrected >= uint64(len(img.sectorSuffixDDT)) {
			return false, fmt.Errorf("sector suffix entry %d out of range: %w", corrected, ErrCannotReadBlock)
		}
		entry := img.sectorSuffixDDT[corrected]
		switch flags := CdFixFlags(entry & cdXFixMask); flags {
		case CdFixMode2Form1Ok:
			copy(data[24:24+2048], bare)
			_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode2Form1)
		case CdFixMode2Form2Ok, CdFixMode2Form2NoCrc:
			copy(data[24:24+2324], bare)
			if flags == CdFixMode2Form2Ok {
				_ = cdecc.Reconstruct(data, cdecc.TrackTypeCdMode2Form2)
			} else {
				clear(data[2348:2352])
			}
		case CdFixNotDumped:
			return true, nil
		default:
			s, ok := sectorTagArena(img.sectorSuffixCorrected, uint64(entry&cdDFixMask)-1, 288)
			if !ok {
				return false, errArenaTooShort
			}
			if isForm2() {
				copy(data[24:24+2324], bare)
				copy(data[2348:2352], s[:4])
			} else {
				copy(data[24:24+2048], bare)
				copy(data[2072:2352], s[:280])
			}
		}
	case img.mode2Subheaders != nil:
		sh, ok := sectorTagArena(img.mode2Subheaders, corrected, 8)
		if !ok {
			return false, errArenaTooShort
		}
		copy(data[16:24], sh)
		copy(data[24:24+2328], bare)
	default:
		copy(data[16:16+2336], bare)
	}
	return false, nil
}

// ReadSectorTag returns a per-sector tag: CD track
// flags (1 byte), ISRC (12 bytes) and subchannel (96 bytes); DVD CMI (1),
// sector information (1), sector number (3), IED (2), EDC (4) and decrypted
// title key (5); Apple Sony (12), Apple ProFile (20) and Priam DataTower (24)
// tags. Other tags return ErrInvalidTag.
func (img *Image) ReadSectorTag(addr uint64, negative bool, tag SectorTagType) ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if img.noUserDataDDT {
		return nil, ErrUserDataNotPresent
	}
	if err := img.checkSectorBounds(addr, negative); err != nil {
		return nil, err
	}
	corrected := img.ddtSectorIndex(addr, negative)

	optical := func() error {
		if img.info.MetadataMediaType != media.OpticalDisc {
			return fmt.Errorf("tag %v on %v: %w", tag, img.info.MetadataMediaType, ErrIncorrectMediaType)
		}
		return nil
	}
	block := func() error {
		if img.info.MetadataMediaType != media.BlockMedia {
			return fmt.Errorf("tag %v on %v: %w", tag, img.info.MetadataMediaType, ErrIncorrectMediaType)
		}
		return nil
	}
	// fromArena copies n bytes at offset skip of entry corrected of an arena
	// whose entries are size bytes long.
	fromArena := func(arena []byte, size, skip, n uint64) ([]byte, error) {
		if arena == nil {
			return nil, fmt.Errorf("tag %v: %w", tag, ErrSectorTagNotPresent)
		}
		e, ok := sectorTagArena(arena, corrected, size)
		if !ok {
			return nil, fmt.Errorf("tag %v of sector %d: %w", tag, addr, ErrSectorTagNotPresent)
		}
		return append([]byte(nil), e[skip:skip+n]...), nil
	}
	// trackFor finds the track containing addr (unsigned comparison of the
	// signed bounds).
	trackFor := func() (TrackEntry, error) {
		for _, t := range img.tracks {
			if addr >= uint64(t.Start) && addr <= uint64(t.End) {
				return t, nil
			}
		}
		return TrackEntry{}, fmt.Errorf("sector %d: %w", addr, ErrTrackNotFound)
	}

	switch tag {
	case SectorTagCdTrackFlags:
		if err := optical(); err != nil {
			return nil, err
		}
		t, err := trackFor()
		if err != nil {
			return nil, err
		}
		return []byte{t.Flags}, nil
	case SectorTagCdTrackIsrc:
		if err := optical(); err != nil {
			return nil, err
		}
		t, err := trackFor()
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), t.ISRC[:12]...), nil
	case SectorTagCdSubchannel:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorSubchannel, 96, 0, 96)
	case SectorTagDvdCmi:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorCPRMAI, 6, 0, 1)
	case SectorTagDvdSectorInformation:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorID, 4, 0, 1)
	case SectorTagDvdSectorNumber:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorID, 4, 1, 3)
	case SectorTagDvdSectorIed:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorIED, 2, 0, 2)
	case SectorTagDvdSectorEdc:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorEDC, 4, 0, 4)
	case SectorTagDvdTitleKeyDecrypted:
		if err := optical(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorDecryptedTitleKey, 5, 0, 5)
	case SectorTagAppleSony:
		if err := block(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorSubchannel, 12, 0, 12)
	case SectorTagAppleProfile:
		if err := block(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorSubchannel, 20, 0, 20)
	case SectorTagPriamDataTower:
		if err := block(); err != nil {
			return nil, err
		}
		return fromArena(img.sectorSubchannel, 24, 0, 24)
	}
	return nil, fmt.Errorf("tag %v: %w", tag, ErrInvalidTag)
}
