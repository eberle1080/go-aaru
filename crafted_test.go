package aaru

import (
	"bytes"
	"errors"
	"testing"

	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Hand-crafted images exercising the edge cases of the open and read paths
// that a regular writer never produces (damaged blocks, odd compressions,
// unknown features...). craftedImages builds them; TestCraftedImages opens
// each one and checks what the reader makes of it.

// craftBuilder lays out a version 2 image: header, aligned blocks, IDX3.
type craftBuilder struct {
	hdr     Header
	buf     []byte
	entries []IndexEntry
}

func newCraft(mt media.Type) *craftBuilder {
	b := &craftBuilder{buf: make([]byte, 512)}
	b.hdr = Header{
		Identifier:              AaruMagic,
		ImageMajorVersion:       FormatVersionV2,
		ApplicationMajorVersion: 1,
		MediaType:               uint32(mt),
		BlockAlignmentShift:     9,
		BiggestSectorSize:       512,
		CreationTime:            133986244248621873,
		LastWrittenTime:         133986244253238013,
	}
	copy(b.hdr.Application[:], "go-aaru-test")
	return b
}

// add appends raw block bytes at an aligned offset and indexes them.
func (b *craftBuilder) add(bt BlockType, dt DataType, raw []byte) uint64 {
	off := alignUp(uint64(len(b.buf)), 9)
	b.buf = append(b.buf, make([]byte, off-uint64(len(b.buf)))...)
	b.buf = append(b.buf, raw...)
	if bt != 0 {
		b.entries = append(b.entries, IndexEntry{BlockType: bt, DataType: dt, Offset: off})
	}
	return off
}

// craftDBLK builds a data block with explicit header fields.
func craftDBLK(dt DataType, c CompressionType, sectorSize uint32, payload []byte, length uint32, crc uint64) []byte {
	h := BlockHeader{Identifier: BlockTypeDataBlock, Type: dt, Compression: c, SectorSize: sectorSize,
		CmpLength: uint32(len(payload)), Length: length, CmpCRC64: crc64.Checksum(payload), CRC64: crc}
	return append(h.appendTo(nil), payload...)
}

// craftPlainBlock is a valid uncompressed data block.
func craftPlainBlock(dt DataType, sectorSize uint32, data []byte) []byte {
	return craftDBLK(dt, CompressionNone, sectorSize, data, uint32(len(data)), crc64.Checksum(data))
}

// craftDDT2 builds a single-level user data DDT v2 (dataShift 12, alignment 9).
func craftDDT2(entries []uint64, crcDelta uint64, c CompressionType) []byte {
	raw := encodeUint64s(entries)
	payload := raw
	if c == CompressionLzma {
		p, err := lzma.EncodePayload(raw, lzma.DefaultOptions())
		if err != nil {
			panic(err)
		}
		payload = p
	}
	h := DDTHeader2{Identifier: BlockTypeDeDuplicationTable2, Type: DataTypeUserData, Compression: c, Levels: 1,
		Blocks: uint64(len(entries)), BlockAlignmentShift: 9, DataShift: 12, Entries: uint64(len(entries)),
		CmpLength: uint64(len(payload)), Length: uint64(len(raw)), CmpCRC64: crc64.Checksum(payload),
		CRC64: crc64.Checksum(raw) + crcDelta}
	return append(marshal(h), payload...)
}

// craftDDTEntry encodes a DDT v2 entry (dataShift 12, alignment 9).
func craftDDTEntry(status SectorStatus, blockOff, offset uint64) uint64 {
	return uint64(status)<<60 | (blockOff>>9)<<12 | offset
}

func (b *craftBuilder) bytes() []byte {
	idx := alignUp(uint64(len(b.buf)), 9)
	b.buf = append(b.buf, make([]byte, idx-uint64(len(b.buf)))...)
	body := marshal(b.entries)
	ih := IndexHeader3{Identifier: BlockTypeIndexBlock3, Entries: uint64(len(b.entries)), CRC64: crc64.Checksum(body)}
	b.buf = append(b.buf, marshal(ih)...)
	b.buf = append(b.buf, body...)
	b.hdr.IndexOffset = idx
	copy(b.buf, marshal(b.hdr))
	return b.buf
}

// userData adds a block of n 512-byte sectors and a DDT pointing at them.
func (b *craftBuilder) userData(n int, statuses ...SectorStatus) uint64 {
	data := make([]byte, 512*n)
	for i := range data {
		data[i] = byte(i*7 + i/512)
	}
	off := b.add(BlockTypeDataBlock, DataTypeUserData, craftPlainBlock(DataTypeUserData, 512, data))
	entries := make([]uint64, n)
	for i := range entries {
		st := SectorStatusDumped
		if i < len(statuses) {
			st = statuses[i]
		}
		entries[i] = craftDDTEntry(st, off, uint64(i))
	}
	b.add(BlockTypeDeDuplicationTable2, DataTypeUserData, craftDDT2(entries, 0, CompressionNone))
	return off
}

// craftedSector is sector i of a block written by craftBuilder.userData.
func craftedSector(i int) []byte {
	d := make([]byte, 512)
	for j := range d {
		k := i*512 + j
		d[j] = byte(k*7 + k/512)
	}
	return d
}

// craftedTag is the media tag stored in the crafted images.
var craftedTag = []byte("media tag contents for crafted images")

// craftedImages returns the crafted images by name.
func craftedImages() map[string][]byte {
	imgs := map[string][]byte{}
	tag := craftedTag

	// Media tag blocks with every skip/fail condition of the data block reader.
	b := newCraft(media.GENERIC_HDD)
	b.userData(8)
	b.add(BlockTypeDataBlock, DataTypeScsiInquiry, craftPlainBlock(DataTypeScsiInquiry, 0, tag))                                   // OK
	b.add(BlockTypeDataBlock, DataTypeAtaIdentify, craftDBLK(DataTypeAtaIdentify, CompressionNone, 0, tag, uint32(len(tag)), 1))   // bad CRC: skipped
	b.add(BlockTypeDataBlock, DataTypePcmciaCis, craftDBLK(DataTypePcmciaCis, CompressionNone, 0, nil, 0, 0))                      // empty tag
	b.add(BlockTypeDataBlock, DataTypeSdCid, craftDBLK(DataTypeSdCid, CompressionLzmaCst, 0, tag, uint32(len(tag)), 0))            // CST on a tag: skipped
	b.add(BlockTypeDataBlock, DataTypeSdCsd, craftDBLK(DataTypeSdCsd, 99, 0, tag, uint32(len(tag)), 0))                            // unknown compression: skipped
	b.add(BlockTypeDataBlock, DataTypeSdScr, craftPlainBlock(DataTypeSdOcr, 0, tag))                                               // type mismatch: skipped
	b.add(BlockTypeDataBlock, DataTypeMmcCid, craftDBLK(DataTypeMmcCid, CompressionLzma, 0, []byte{0x5d, 0, 0, 1, 0}, 0, 0))       // LZMA props only, length 0
	b.add(BlockTypeDataBlock, DataTypeMmcCsd, craftDBLK(DataTypeMmcCsd, CompressionLzma, 0, []byte{0x5d, 0, 0, 1, 0, 1, 2}, 0, 0)) // LZMA, length 0 but data: skipped
	b.add(BlockTypeDataBlock, DataTypeMmcOcr, craftDBLK(DataTypeMmcOcr, CompressionNone, 0, tag, uint32(len(tag))+100, 0))         // short payload: skipped
	b.add(BlockTypeDataBlock, DataTypeNone, craftPlainBlock(DataTypeNone, 0, tag))                                                 // NOP
	b.add(BlockTypeDataBlock, DataTypeXboxPfi, craftPlainBlock(DataTypeXboxPfi, 0, tag)[:20])                                      // truncated header (zero-padded): CRC mismatch, skipped
	imgs["crafted_tags"] = b.bytes()

	// Fatal decompression failure of a media tag.
	b = newCraft(media.GENERIC_HDD)
	b.userData(4)
	b.add(BlockTypeDataBlock, DataTypeScsiInquiry, craftDBLK(DataTypeScsiInquiry, CompressionLzma, 0, []byte{1, 2, 3}, 10, 0))
	imgs["crafted_tag_lzma_short"] = b.bytes()

	b = newCraft(media.GENERIC_HDD)
	b.userData(4)
	b.add(BlockTypeDataBlock, DataTypeScsiInquiry, craftDBLK(DataTypeScsiInquiry, CompressionZstd, 0, []byte{1, 2, 3, 4, 5, 6}, 10, 0))
	imgs["crafted_tag_zstd_bad"] = b.bytes()

	// User data DDT problems.
	b = newCraft(media.GENERIC_HDD)
	off := b.add(BlockTypeDataBlock, DataTypeUserData, craftPlainBlock(DataTypeUserData, 512, make([]byte, 1024)))
	b.add(BlockTypeDeDuplicationTable2, DataTypeUserData, craftDDT2([]uint64{craftDDTEntry(1, off, 0), craftDDTEntry(1, off, 1)}, 1, CompressionNone))
	imgs["crafted_ddt_bad_crc"] = b.bytes()

	b = newCraft(media.GENERIC_HDD)
	off = b.add(BlockTypeDataBlock, DataTypeUserData, craftPlainBlock(DataTypeUserData, 512, make([]byte, 1024)))
	b.add(BlockTypeDeDuplicationTable2, DataTypeUserData, craftDDT2([]uint64{craftDDTEntry(1, off, 0), craftDDTEntry(1, off, 1)}, 0, 7))
	imgs["crafted_ddt_unknown_compression"] = b.bytes()

	b = newCraft(media.GENERIC_HDD)
	b.add(BlockTypeDataBlock, DataTypeScsiInquiry, craftPlainBlock(DataTypeScsiInquiry, 0, tag))
	imgs["crafted_no_ddt"] = b.bytes()

	// LZMA compressed DDT and sectors pointing at damaged blocks.
	b = newCraft(media.GENERIC_HDD)
	good := b.add(BlockTypeDataBlock, DataTypeUserData, craftPlainBlock(DataTypeUserData, 512, make([]byte, 2048)))
	badHdr := b.add(0, 0, []byte("this is not a block header at all, just text"))
	badLzma := b.add(0, 0, craftDBLK(DataTypeUserData, CompressionLzma, 512, []byte{0x5d, 0, 0, 1, 0, 0, 9, 9, 9}, 1024, 0))
	zeroSize := b.add(0, 0, craftDBLK(DataTypeUserData, CompressionNone, 0, make([]byte, 512), 512, 0))
	cst := b.add(0, 0, craftDBLK(DataTypeUserData, CompressionLzmaCst, 512, make([]byte, 512), 512, 0))
	entries := []uint64{
		craftDDTEntry(SectorStatusDumped, good, 0), craftDDTEntry(SectorStatusErrored, good, 3), 0,
		craftDDTEntry(SectorStatusDumped, badHdr, 0), craftDDTEntry(SectorStatusDumped, badLzma, 0),
		craftDDTEntry(SectorStatusDumped, zeroSize, 0), craftDDTEntry(SectorStatusDumped, cst, 0),
		craftDDTEntry(SectorStatusDumped, 1<<30, 0), // past the end of the file
		craftDDTEntry(SectorStatusGenerable, good, 1), craftDDTEntry(SectorStatusUnencrypted, good, 2),
	}
	b.add(BlockTypeDeDuplicationTable2, DataTypeUserData, craftDDT2(entries, 0, CompressionLzma))
	imgs["crafted_bad_blocks"] = b.bytes()

	// Tracks block with a wrong identifier, on an optical disc.
	b = newCraft(media.CDROM)
	b.userData(4)
	te := marshal([]TrackEntry{{Sequence: 1, Type: TrackTypeData, Start: 0, End: 3, Session: 1}})
	th := TracksHeader{Identifier: BlockTypeTracksBlock + 1, Entries: 1, CRC64: crc64.Checksum(te)}
	b.add(BlockTypeTracksBlock, 0, append(marshal(th), te...))
	imgs["crafted_tracks_bad_identifier"] = b.bytes()

	// Tracks block with an empty track list.
	b = newCraft(media.CDROM)
	b.userData(4)
	b.add(BlockTypeTracksBlock, 0, marshal(TracksHeader{Identifier: BlockTypeTracksBlock}))
	imgs["crafted_tracks_empty"] = b.bytes()

	// Index entries that cannot be followed, and an unknown block type.
	b = newCraft(media.GENERIC_HDD)
	b.userData(4)
	b.entries = append(b.entries, IndexEntry{BlockType: BlockTypeDataBlock, DataType: DataTypeScsiInquiry, Offset: 1 << 63},
		IndexEntry{BlockType: BlockTypeTracksBlock, Offset: 1 << 40}, IndexEntry{BlockType: 0x12345678, Offset: 512},
		IndexEntry{BlockType: BlockTypeDataBlock, DataType: DataTypeScsiInquiry, Offset: 1 << 40})
	imgs["crafted_index_oddities"] = b.bytes()

	// Feature flags and versions.
	for name, f := range map[string]func(h *Header){
		"crafted_ro_compat_unknown": func(h *Header) { h.FeatureCompatibleRO = 0x100 },
		"crafted_incompat_unknown":  func(h *Header) { h.FeatureIncompatible = 0x100 },
		"crafted_compat_unknown":    func(h *Header) { h.FeatureCompatible = 0x100 },
		"crafted_version3":          func(h *Header) { h.ImageMajorVersion = 3 },
		"crafted_dic_magic":         func(h *Header) { h.Identifier = DICMagic },
		"crafted_app_name_full": func(h *Header) {
			copy(h.Application[:], "0123456789012345678901234567890123456789012345678901234567890123")
		},
	} {
		b = newCraft(media.GENERIC_HDD)
		b.userData(4)
		f(&b.hdr)
		imgs[name] = b.bytes()
	}

	// Header shorter than version 2 needs.
	b = newCraft(media.GENERIC_HDD)
	imgs["crafted_short_v2_header"] = b.bytes()[:120]

	// Not dumped and odd statuses on block media, plus negative/overflow
	// addressing on an image without them.
	b = newCraft(media.GENERIC_HDD)
	b.userData(6, SectorStatusDumped, SectorStatusNotDumped, SectorStatusMode1Correct, SectorStatusTwin,
		SectorStatusGenerable, 15)
	imgs["crafted_statuses"] = b.bytes()

	// A media tag of a data type without a media tag type.
	b = newCraft(media.GENERIC_HDD)
	b.userData(4)
	b.add(BlockTypeDataBlock, DataType(0x7FF0), craftPlainBlock(DataType(0x7FF0), 0, tag))
	imgs["crafted_unknown_media_tag"] = b.bytes()
	return imgs
}

// craftedSectorWant is what reading a sector of a crafted image must give.
type craftedSectorWant struct {
	data   []byte
	status SectorStatus
	err    error
}

// craftedWant is what the reader must make of a crafted image.
type craftedWant struct {
	open        error // Open fails with it
	mediaType   media.Type
	application string
	verify      error
	mediaTags   map[MediaTagType][]byte
	sectors     []craftedSectorWant // nil: every sector holds craftedSector(i), dumped
	n           int                 // user sectors
}

func craftedExpectations() map[string]craftedWant {
	dumped := func(n int) []craftedSectorWant {
		var w []craftedSectorWant
		for i := 0; i < n; i++ {
			w = append(w, craftedSectorWant{data: craftedSector(i), status: SectorStatusDumped})
		}
		return w
	}
	plain := craftedWant{mediaType: media.GENERIC_HDD, n: 4, sectors: dumped(4)}
	zero := make([]byte, 512)
	m := map[string]craftedWant{
		// Only the valid tag, the empty tag and the LZMA tag holding only
		// its properties (length 0) are kept; the image fails verification
		// because of the block with a bad CRC.
		"crafted_tags": {mediaType: media.GENERIC_HDD, n: 8, sectors: dumped(8), verify: ErrInvalidBlockCRC,
			mediaTags: map[MediaTagType][]byte{MediaTagScsiInquiry: craftedTag, MediaTagPcmciaCis: {},
				MediaTagMmcCid: {}}},
		"crafted_tag_lzma_short":          {open: ErrCannotDecompressBlock},
		"crafted_tag_zstd_bad":            {open: ErrCannotDecompressBlock},
		"crafted_ddt_bad_crc":             {open: ErrInvalidBlockCRC},
		"crafted_ddt_unknown_compression": {open: ErrCannotReadIndex},
		"crafted_no_ddt":                  {open: ErrCannotReadIndex},
		"crafted_bad_blocks": {mediaType: media.GENERIC_HDD, n: 10, verify: ErrInvalidBlockCRC,
			sectors: []craftedSectorWant{
				{data: zero, status: SectorStatusDumped},
				{data: zero, status: SectorStatusErrored},
				{status: SectorStatusNotDumped, err: ErrSectorNotDumped},
				{status: SectorStatusDumped, err: ErrCannotDecompressBlock}, // not a block header
				{status: SectorStatusDumped, err: ErrCannotDecompressBlock}, // corrupt LZMA stream
				{status: SectorStatusDumped, err: ErrCannotDecompressBlock}, // sector size 0
				{status: SectorStatusDumped, err: ErrCannotDecompressBlock}, // LZMA+CST on user data
				{status: SectorStatusDumped, err: ErrCannotReadHeader},      // past the end of the file
				{data: zero, status: SectorStatusGenerable},                 // no junk on block media
				{data: zero, status: SectorStatusUnencrypted},               // nothing to re-encrypt
			}},
		"crafted_tracks_bad_identifier": {mediaType: media.CDROM, n: 4, sectors: dumped(4)},
		"crafted_tracks_empty":          {mediaType: media.CDROM, n: 4, sectors: dumped(4)},
		"crafted_index_oddities": {mediaType: media.GENERIC_HDD, n: 4, sectors: dumped(4),
			verify: ErrCannotReadBlock},
		"crafted_ro_compat_unknown": plain,
		"crafted_incompat_unknown":  {open: ErrIncompatibleFeatures},
		"crafted_compat_unknown":    plain,
		"crafted_version3":          {open: ErrIncompatibleVersion},
		"crafted_dic_magic":         plain,
		"crafted_app_name_full": {mediaType: media.GENERIC_HDD, n: 4, sectors: dumped(4),
			application: "012345678901234567890123456789012345678901234567890123456789012"},
		"crafted_short_v2_header": {open: ErrFileTooSmall},
		"crafted_statuses": {mediaType: media.GENERIC_HDD, n: 6, sectors: []craftedSectorWant{
			{data: craftedSector(0), status: SectorStatusDumped},
			{status: SectorStatusNotDumped, err: ErrSectorNotDumped},
			{data: craftedSector(2), status: SectorStatusMode1Correct},
			{data: craftedSector(3), status: SectorStatusTwin},
			{data: craftedSector(4), status: SectorStatusGenerable},
			{data: craftedSector(5), status: 15},
		}},
		"crafted_unknown_media_tag": {mediaType: media.GENERIC_HDD, n: 4, sectors: dumped(4),
			mediaTags: map[MediaTagType][]byte{-1: craftedTag}},
	}
	return m
}

func TestCraftedImages(t *testing.T) {
	want := craftedExpectations()
	imgs := craftedImages()
	if len(imgs) != len(want) {
		t.Fatalf("%d crafted images, %d expectations", len(imgs), len(want))
	}
	for name, data := range imgs {
		t.Run(name, func(t *testing.T) {
			w, ok := want[name]
			if !ok {
				t.Fatal("no expectation")
			}
			path := writeTempFile(t, name+".aif", data)
			img, err := Open(path)
			if w.open != nil {
				if err == nil {
					img.Close()
					t.Fatalf("Open succeeded; want %v", w.open)
				}
				if !errors.Is(err, w.open) {
					t.Fatalf("Open = %v; want %v", err, w.open)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer img.Close()

			info := img.Info()
			app := w.application
			if app == "" {
				app = "go-aaru-test"
			}
			if info.Version != "2.0" || info.Application != app || info.ApplicationVersion != "1.0" ||
				info.MediaType != w.mediaType || info.Sectors != uint64(w.n) || info.SectorSize != 512 ||
				TimeToFiletime(info.CreationTime) != 133986244248621873 ||
				TimeToFiletime(info.LastModificationTime) != 133986244253238013 {
				t.Errorf("info = %+v", info)
			}
			if info.HasPartitions || info.HasSessions {
				t.Error("crafted image has partitions")
			}
			if err := img.Verify(); !errors.Is(err, w.verify) || (w.verify == nil && err != nil) {
				t.Errorf("Verify = %v, want %v", err, w.verify)
			}

			for tag := MediaTagType(-1); tag < 128; tag++ {
				got, err := img.ReadMediaTag(tag)
				if wt, ok := w.mediaTags[tag]; ok {
					if err != nil || !bytes.Equal(got, wt) {
						t.Errorf("ReadMediaTag(%v) = %q, %v; want %q", tag, got, err, wt)
					}
				} else if !errors.Is(err, ErrMediaTagNotPresent) {
					t.Errorf("ReadMediaTag(%v) = %q, %v; want not present", tag, got, err)
				}
			}

			for i, ws := range w.sectors {
				got, st, err := img.ReadSector(uint64(i), false)
				if st != ws.status || !bytes.Equal(got, ws.data) || !errors.Is(err, ws.err) ||
					(ws.err == nil && err != nil) {
					t.Errorf("ReadSector(%d) = %d bytes, status %v, %v; want %d bytes, status %v, %v", i, len(got), st,
						err, len(ws.data), ws.status, ws.err)
				}
			}
			if _, _, err := img.ReadSector(uint64(w.n), false); !errors.Is(err, ErrSectorOutOfBounds) {
				t.Errorf("ReadSector(%d) = %v, want ErrSectorOutOfBounds", w.n, err)
			}
			if _, _, err := img.ReadSector(1, true); !errors.Is(err, ErrSectorOutOfBounds) {
				t.Errorf("negative sector 1 = %v, want ErrSectorOutOfBounds", err)
			}

			if w.mediaType == media.CDROM {
				// The unusable tracks block is ignored: no tracks, and long
				// sectors are the plain sectors (nothing to rebuild them).
				if tracks, err := img.Tracks(); !errors.Is(err, ErrTrackNotFound) {
					t.Errorf("Tracks = %v, %v; want ErrTrackNotFound", tracks, err)
				}
				if _, _, err := img.ReadTrackSector(0, 1); !errors.Is(err, ErrTrackNotFound) {
					t.Errorf("ReadTrackSector = %v, want ErrTrackNotFound", err)
				}
				if got, _, err := img.ReadSectorLong(0, false); err != nil || !bytes.Equal(got, craftedSector(0)) {
					t.Errorf("ReadSectorLong(0) = %d bytes, %v", len(got), err)
				}
			} else if _, _, err := img.ReadSectorLong(0, false); !errors.Is(err, ErrIncorrectMediaType) {
				t.Errorf("ReadSectorLong on block media = %v, want ErrIncorrectMediaType", err)
			}
		})
	}

	// Unknown read-only compatible features: readable, but not writable.
	path := writeTempFile(t, "ro.aif", imgs["crafted_ro_compat_unknown"])
	if img, err := Resume(path, nil); !errors.Is(err, ErrIncompatibleFeatures) {
		if err == nil {
			img.Close()
		}
		t.Errorf("Resume with unknown read-only compatible features = %v, want ErrIncompatibleFeatures", err)
	}
}
