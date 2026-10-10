package aaru

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/compression/lzma"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Version 1 images built in test code.
//
// go-aaru only writes version 2 images but reads version 1 ones. v1Builder
// lays a version 1 image out with the exported on-disk structures:
//   - the 104-byte header, with the application name in UTF-16LE;
//   - data blocks and DDTs whose CRC64s are stored byte-swapped, the CRC of
//     LZMA payloads covering the properties too;
//   - version 1 DDTs (DDT*), whose user data entries hold absolute block
//     offsets (entry = blockOffset<<shift | index);
//   - CD prefix/suffix correction tables (32-bit entries: CdFixFlags in the
//     high byte, 1-based index into the stored prefix/suffix arena below);
//   - an INDX index (16-bit entry count).

// v1CreationTime and v1LastWrittenTime are the FILETIMEs stamped into the
// version 1 test images.
const (
	v1CreationTime    int64 = 133986244248621873
	v1LastWrittenTime int64 = 133986244253238013
)

type v1Builder struct {
	hdr       Header
	buf       []byte
	entries   []IndexEntry
	imageSize uint64 // what ImageInfo.ImageSize must report
}

// newV1 starts a version 1 image (identifier DICMagic or AaruMagic).
func newV1(mt media.Type, magic uint64) *v1Builder {
	b := &v1Builder{buf: make([]byte, headerV1Size)}
	b.hdr = Header{
		Identifier:              magic,
		ImageMajorVersion:       FormatVersionV1,
		ApplicationMajorVersion: 1,
		ApplicationMinorVersion: 2,
		MediaType:               uint32(mt),
		CreationTime:            v1CreationTime,
		LastWrittenTime:         v1LastWrittenTime,
	}
	for i, c := range []byte("go-aaru-test") {
		b.hdr.Application[2*i] = c // UTF-16LE
	}
	return b
}

// add appends a block and indexes it.
func (b *v1Builder) add(bt BlockType, dt DataType, raw []byte) uint64 {
	off := uint64(len(b.buf))
	b.buf = append(b.buf, raw...)
	b.entries = append(b.entries, IndexEntry{BlockType: bt, DataType: dt, Offset: off})
	return off
}

// v1Payload compresses data, returning the on-disk payload and its
// (swapped) CRC64.
func v1Payload(data []byte, c CompressionType) ([]byte, uint64) {
	payload := data
	if c == CompressionLzma {
		p, err := lzma.EncodePayload(data, lzma.DefaultOptions())
		if err != nil {
			panic(err)
		}
		payload = p
	}
	return payload, bswap64(crc64.Checksum(payload))
}

// block appends a data block holding data (sectors of sectorSize bytes, or a
// single item when sectorSize is 0).
func (b *v1Builder) block(dt DataType, c CompressionType, sectorSize uint32, data []byte) uint64 {
	payload, cmpCRC := v1Payload(data, c)
	h := BlockHeader{Identifier: BlockTypeDataBlock, Type: dt, Compression: c, SectorSize: sectorSize,
		CmpLength: uint32(len(payload)), Length: uint32(len(data)), CmpCRC64: cmpCRC,
		CRC64: bswap64(crc64.Checksum(data))}
	b.imageSize += uint64(len(payload))
	return b.add(BlockTypeDataBlock, dt, append(h.appendTo(nil), payload...))
}

// ddt appends a version 1 DDT of the given data type holding raw (packed
// 64-bit user data entries, or 32-bit CD correction entries).
func (b *v1Builder) ddt(dt DataType, c CompressionType, shift uint8, entries uint64, raw []byte) {
	payload, cmpCRC := v1Payload(raw, c)
	h := DDTHeader{Identifier: BlockTypeDeDuplicationTable, Type: dt, Compression: c, Shift: shift,
		Entries: entries, CmpLength: uint64(len(payload)), Length: uint64(len(raw)), CmpCRC64: cmpCRC,
		CRC64: bswap64(crc64.Checksum(raw))}
	b.imageSize += uint64(len(payload))
	b.add(BlockTypeDeDuplicationTable, dt, append(marshal(h), payload...))
}

// userData stores sectors (nil = never dumped) in blocks of at most
// 1<<shift sectors, deduplicating identical sectors, and appends the user
// data DDT.
func (b *v1Builder) userData(sectorSize uint32, sectors [][]byte, c CompressionType, shift uint8) {
	type loc struct {
		blk int
		pos uint64
	}
	var blocks [][]byte
	locs := make([]*loc, len(sectors))
	seen := map[string]*loc{}
	for i, s := range sectors {
		if s == nil {
			continue
		}
		if l, ok := seen[string(s)]; ok {
			locs[i] = l
			continue
		}
		if len(blocks) == 0 || uint64(len(blocks[len(blocks)-1]))/uint64(sectorSize) == 1<<shift {
			blocks = append(blocks, nil)
		}
		k := len(blocks) - 1
		l := &loc{blk: k, pos: uint64(len(blocks[k])) / uint64(sectorSize)}
		blocks[k] = append(blocks[k], s...)
		seen[string(s)] = l
		locs[i] = l
	}
	offs := make([]uint64, len(blocks))
	for k, blk := range blocks {
		offs[k] = b.block(DataTypeUserData, c, sectorSize, blk)
	}
	entries := make([]uint64, len(sectors))
	for i, l := range locs {
		if l != nil {
			entries[i] = offs[l.blk]<<shift | l.pos
		}
	}
	b.ddt(DataTypeUserData, c, shift, uint64(len(entries)), encodeUint64s(entries))
}

// tracks appends a tracks block.
func (b *v1Builder) tracks(tracks ...TrackEntry) {
	body := marshal(tracks)
	h := TracksHeader{Identifier: BlockTypeTracksBlock, Entries: uint16(len(tracks)),
		CRC64: bswap64(crc64.Checksum(body))}
	b.imageSize += uint64(len(body))
	b.add(BlockTypeTracksBlock, 0, append(marshal(h), body...))
}

// bytes appends the INDX index and the header.
func (b *v1Builder) bytes() []byte {
	idx := uint64(len(b.buf))
	body := marshal(b.entries)
	ih := IndexHeader{Identifier: BlockTypeIndexBlock, Entries: uint16(len(b.entries)),
		CRC64: bswap64(crc64.Checksum(body))}
	b.buf = append(b.buf, marshal(ih)...)
	b.buf = append(b.buf, body...)
	b.hdr.IndexOffset = idx
	copy(b.buf, marshal(b.hdr)[:headerV1Size])
	return b.buf
}

// v1CDCorrections splits raw CD sectors into user data and the version 1
// prefix/suffix correction tables: regenerable prefixes and suffixes only
// get a flag, the others are stored in the correction arenas. Sectors listed
// in notDumped get NotDumped prefix and suffix entries (their user data is
// stored). For Mode 2 Form 1 sectors the subheaders are returned too.
func v1CDCorrections(raw [][]byte, mode2 bool, notDumped map[int]bool) (user [][]byte, prefixDDT, suffixDDT []uint32,
	prefixes, suffixes, subheaders []byte,
) {
	for i, s := range raw {
		lba := int64(i)
		var p, x uint32
		want := bytes.Clone(s)
		tt := cdecc.TrackTypeCdMode1
		if mode2 {
			tt = cdecc.TrackTypeCdMode2Form1
		}
		_ = cdecc.ReconstructPrefix(want, tt, lba)
		switch {
		case notDumped[i]:
			p = uint32(CdFixNotDumped)
		case bytes.Equal(want[:16], s[:16]):
			p = uint32(CdFixCorrect)
		default:
			prefixes = append(prefixes, s[:16]...)
			p = uint32(len(prefixes) / 16)
		}
		// The suffix is regenerated over the prefix as stored.
		want = bytes.Clone(s)
		_ = cdecc.Reconstruct(want, tt)
		regenerable := bytes.Equal(want[16:], s[16:])
		if mode2 {
			subheaders = append(subheaders, s[16:24]...)
			user = append(user, bytes.Clone(s[24:24+2048]))
			switch {
			case notDumped[i]:
				x = uint32(CdFixNotDumped)
			case regenerable:
				x = uint32(CdFixMode2Form1Ok)
			default:
				suffix := make([]byte, 288)
				copy(suffix, s[2072:])
				suffixes = append(suffixes, suffix...)
				x = uint32(len(suffixes) / 288)
			}
		} else {
			user = append(user, bytes.Clone(s[16:16+2048]))
			switch {
			case notDumped[i]:
				x = uint32(CdFixNotDumped)
			case regenerable:
				x = uint32(CdFixCorrect)
			default:
				suffixes = append(suffixes, s[2064:]...)
				x = uint32(len(suffixes) / 288)
			}
		}
		prefixDDT = append(prefixDDT, p)
		suffixDDT = append(suffixDDT, x)
	}
	return
}

func encodeUint32s(v []uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	return b
}

// v1BlockImage builds a version 1 block media image of the given sectors.
func v1BlockImage(mt media.Type, sectorSize uint32, sectors [][]byte, c CompressionType) ([]byte, uint64) {
	b := newV1(mt, DICMagic)
	b.userData(sectorSize, sectors, c, 12)
	return b.bytes(), b.imageSize
}

// v1CDImage builds a version 1 CD image (one data track of Mode 1 or Mode 2
// Form 1 sectors) from raw 2352-byte sectors, with LZMA compressed data and
// correction tables and a CD TOC media tag.
func v1CDImage(raw [][]byte, mode2 bool, notDumped map[int]bool) ([]byte, uint64) {
	mt, tt := media.CDROM, TrackTypeCdMode1
	if mode2 {
		mt, tt = media.CDROMXA, TrackTypeCdMode2Form1
	}
	b := newV1(mt, AaruMagic)
	user, pddt, sddt, prefixes, suffixes, subheaders := v1CDCorrections(raw, mode2, notDumped)
	b.userData(2048, user, CompressionLzma, 8)
	b.ddt(DataTypeCdSectorPrefixCorrected, CompressionLzma, 0, uint64(len(pddt)), encodeUint32s(pddt))
	b.ddt(DataTypeCdSectorSuffixCorrected, CompressionLzma, 0, uint64(len(sddt)), encodeUint32s(sddt))
	if len(prefixes) > 0 {
		b.block(DataTypeCdSectorPrefixCorrected, CompressionNone, 16, prefixes)
	}
	if len(suffixes) > 0 {
		b.block(DataTypeCdSectorSuffixCorrected, CompressionLzma, 288, suffixes)
	}
	if mode2 {
		b.block(DataTypeCdSubHeader, CompressionLzma, 8, subheaders)
	}
	b.block(DataTypeCdToc, CompressionNone, 0, []byte("version 1 table of contents"))
	b.tracks(TrackEntry{Sequence: 1, Type: tt, Start: 0, End: int64(len(raw)) - 1, Session: 1, Flags: 4})
	return b.bytes(), b.imageSize
}

// seededSectors returns n deterministic sectors of size bytes; every
// dupEvery-th sector repeats an earlier one (0: none), so deduplication has
// work to do.
func seededSectors(seed uint64, n, size, dupEvery int) [][]byte {
	r := rand.New(rand.NewPCG(seed, 0x5eed))
	out := make([][]byte, n)
	for i := range out {
		if dupEvery > 0 && i > 0 && i%dupEvery == 0 {
			out[i] = out[r.IntN(i)]
			continue
		}
		out[i] = compressible(r, size)
	}
	return out
}

// rawCDSectors returns n raw 2352-byte sectors (Mode 1 or Mode 2 Form 1)
// with correct prefixes and suffixes, except for the sectors listed in
// damaged, whose prefix or suffix is altered so it must be stored.
func rawCDSectors(seed uint64, n int, mode2 bool, damaged map[int]bool) [][]byte {
	r := rand.New(rand.NewPCG(seed, 0xcd))
	out := make([][]byte, n)
	for i := range out {
		tt := TrackTypeCdMode1
		if mode2 {
			tt = TrackTypeCdMode2Formless
		}
		s := cdSector(r, tt, int64(i), false)
		if damaged[i] {
			if i%2 == 0 {
				s[2200] ^= 0x5A // suffix
			} else {
				s[3] = 0x12 // sync
			}
		}
		out[i] = s
	}
	return out
}
