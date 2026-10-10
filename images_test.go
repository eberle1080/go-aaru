package aaru

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/eberle1080/go-aaru/console/ngcw"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/console/wii"
	"github.com/eberle1080/go-aaru/console/wiiu"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Images written by go-aaru at test time, then altered in ways go-aaru never
// writes them (other index layouts, damaged tracks), plus console images
// whose sectors the reader re-encrypts or regenerates.

// compareSectorTags are the sector tags compareImages reads.
var compareSectorTags = []SectorTagType{
	SectorTagAppleSony, SectorTagCdSync, SectorTagCdHeader, SectorTagCdSubHeader, SectorTagCdEcc,
	SectorTagCdEccP, SectorTagCdEccQ, SectorTagCdEdc, SectorTagCdTrackFlags, SectorTagCdTrackIsrc,
	SectorTagCdSubchannel, SectorTagDvdCmi, SectorTagDvdSectorNumber, SectorTagDvdSectorIed,
	SectorTagDvdSectorEdc, SectorTagDvdTitleKeyDecrypted, SectorTagAppleProfile, SectorTagPriamDataTower,
	SectorTagType(99),
}

// compareImages checks that got reads exactly like want: info, verification
// result, tracks, media tags, and every sector, long sector and sector tag
// (including the first addresses out of bounds).
func compareImages(t *testing.T, got, want *Image) {
	t.Helper()
	if g, w := fmt.Sprintf("%+v", got.Info()), fmt.Sprintf("%+v", want.Info()); g != w {
		t.Errorf("info = %s, want %s", g, w)
	}
	if g, w := ErrorCode(got.Verify()), ErrorCode(want.Verify()); g != w {
		t.Errorf("Verify = %d, want %d", g, w)
	}
	gt, gerr := got.Tracks()
	wt, werr := want.Tracks()
	if fmt.Sprint(gt) != fmt.Sprint(wt) || ErrorCode(gerr) != ErrorCode(werr) {
		t.Errorf("Tracks = %v, %v; want %v, %v", gt, gerr, wt, werr)
	}
	for tag := MediaTagType(-1); tag < 128; tag++ {
		g, gerr := got.ReadMediaTag(tag)
		w, werr := want.ReadMediaTag(tag)
		if !bytes.Equal(g, w) || ErrorCode(gerr) != ErrorCode(werr) {
			t.Errorf("ReadMediaTag(%v) = %d bytes, %v; want %d bytes, %v", tag, len(g), gerr, len(w), werr)
		}
	}
	failures := 0
	check := func(what string, addr uint64, neg bool, g, w []byte, gs, ws SectorStatus, gerr, werr error) {
		t.Helper()
		if bytes.Equal(g, w) && gs == ws && ErrorCode(gerr) == ErrorCode(werr) {
			return
		}
		if failures++; failures <= 10 {
			t.Errorf("%s(%d, neg=%v) = %d bytes, status %v, %v; want %d bytes, status %v, %v", what, addr, neg, len(g),
				gs, gerr, len(w), ws, werr)
		}
	}
	sector := func(addr uint64, neg bool) {
		g, gs, gerr := got.ReadSector(addr, neg)
		w, ws, werr := want.ReadSector(addr, neg)
		check("ReadSector", addr, neg, g, w, gs, ws, gerr, werr)
		g, gs, gerr = got.ReadSectorLong(addr, neg)
		w, ws, werr = want.ReadSectorLong(addr, neg)
		check("ReadSectorLong", addr, neg, g, w, gs, ws, gerr, werr)
		for _, tag := range compareSectorTags {
			g, gerr := got.ReadSectorTag(addr, neg, tag)
			w, werr := want.ReadSectorTag(addr, neg, tag)
			check(fmt.Sprint("ReadSectorTag/", tag), addr, neg, g, w, 0, 0, gerr, werr)
		}
	}
	for a := uint64(1); a <= uint64(want.NegativeSectors())+1; a++ {
		sector(a, true)
	}
	for a := uint64(0); a <= want.Info().Sectors+uint64(want.OverflowSectors()); a++ {
		sector(a, false)
	}
	if failures > 10 {
		t.Errorf("%d mismatches in total", failures)
	}
}

// openBytes opens an in-memory image, closing it at the end of the test.
func openBytes(t *testing.T, b []byte) *Image {
	t.Helper()
	img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	t.Cleanup(func() { img.Close() })
	return img
}

// scriptImageBytes runs s (every operation must succeed) and returns the
// image.
func scriptImageBytes(t *testing.T, s *writeScript) []byte {
	t.Helper()
	b, err := os.ReadFile(writeScriptImage(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// imageIndex returns the IDX3 entries (no subindexes) of an image written by
// go-aaru, and the index offset.
func imageIndex(t *testing.T, b []byte) ([]IndexEntry, uint64) {
	t.Helper()
	off := binary.LittleEndian.Uint64(b[80:])
	h, _ := unmarshal[IndexHeader3](b[off:])
	if h.Identifier != BlockTypeIndexBlock3 {
		t.Fatalf("index at %d is not an IDX3 block", off)
	}
	entries, err := unmarshalSlice[IndexEntry](b[off+indexHeader3Size:], int(h.Entries))
	if err != nil {
		t.Fatal(err)
	}
	return entries, off
}

// appendIndex3 appends an IDX3 block holding entries (512-byte aligned) and
// returns its offset.
func appendIndex3(b *[]byte, entries []IndexEntry) uint64 {
	off := alignUp(uint64(len(*b)), 9)
	*b = append(*b, make([]byte, off-uint64(len(*b)))...)
	body := marshal(entries)
	*b = append(*b, marshal(IndexHeader3{Identifier: BlockTypeIndexBlock3, Entries: uint64(len(entries)),
		CRC64: crc64.Checksum(body)})...)
	*b = append(*b, body...)
	return off
}

// withIndexV2 replaces the index of an image by an equivalent IDX2 index.
func withIndexV2(t *testing.T, orig []byte) []byte {
	entries, _ := imageIndex(t, orig)
	b := append([]byte(nil), orig...)
	off := alignUp(uint64(len(b)), 9)
	b = append(b, make([]byte, off-uint64(len(b)))...)
	body := marshal(entries)
	b = append(b, marshal(IndexHeader2{Identifier: BlockTypeIndexBlock2, Entries: uint64(len(entries)),
		CRC64: crc64.Checksum(body)})...)
	b = append(b, body...)
	binary.LittleEndian.PutUint64(b[80:], off)
	return b
}

// withSubindexes replaces the index of an image by an IDX3 index whose middle
// entries live in a subindex, itself ending with a nested subindex.
func withSubindexes(t *testing.T, orig []byte) []byte {
	entries, _ := imageIndex(t, orig)
	if len(entries) < 5 {
		t.Fatalf("only %d index entries", len(entries))
	}
	b := append([]byte(nil), orig...)
	first, middle, last := entries[:1], entries[1:len(entries)-1], entries[len(entries)-1:]
	half := len(middle) / 2
	nested := appendIndex3(&b, middle[half:])
	sub := appendIndex3(&b, append(append([]IndexEntry(nil), middle[:half]...),
		IndexEntry{BlockType: BlockTypeIndexBlock3, Offset: nested}))
	top := append(append(append([]IndexEntry(nil), first...), IndexEntry{BlockType: BlockTypeIndexBlock3, Offset: sub}),
		last...)
	off := appendIndex3(&b, top)
	binary.LittleEndian.PutUint64(b[80:], off)
	return b
}

// TestIndexLayouts reads images whose index was rewritten as an IDX2 index,
// or as an IDX3 index referencing a subindex (with a nested subindex) from
// the middle of its entry list: they must read exactly like the original
// images, and the flattened entries must be the original ones in order.
func TestIndexLayouts(t *testing.T) {
	images := map[string]func(t *testing.T) []byte{
		"block": func(t *testing.T) []byte {
			s := blockScenario(40, "compress=true;deduplicate=true;md5=true;data_shift=4", 300, 16, 8, true)
			s.ops = s.ops[:len(s.ops)-1] // reopen the script before its close
			s.mediaTag(MediaTagAtaIdentify, []byte("identify data"))
			s.close()
			return scriptImageBytes(t, s)
		},
		"multi-level": func(t *testing.T) []byte {
			return scriptImageBytes(t, blockScenario(41, "table_shift=2;compress=false", 64, 4, 4, true))
		},
		"cd": func(t *testing.T) []byte { return scriptImageBytes(t, cdScenario(42, "compress=true", true)) },
	}
	for name, build := range images {
		orig := build(t)
		want := openBytes(t, orig)
		for layout, rewrite := range map[string]func(*testing.T, []byte) []byte{
			"idx2": withIndexV2, "subindex": withSubindexes,
		} {
			t.Run(name+"/"+layout, func(t *testing.T) {
				img := openBytes(t, rewrite(t, orig))
				if fmt.Sprint(img.indexEntries) != fmt.Sprint(want.indexEntries) {
					t.Fatalf("flattened index = %v, want %v", img.indexEntries, want.indexEntries)
				}
				compareImages(t, img, want)
			})
		}
	}
}

// TestTracksBlockCRCMismatch: a tracks block whose entries no longer match
// their CRC is kept for Tracks, but the image gets neither partitions nor
// data tracks (long sectors cannot be rebuilt), and does not verify.
func TestTracksBlockCRCMismatch(t *testing.T) {
	s := cdScenario(43, "compress=true", false)
	orig := scriptImageBytes(t, s)
	want := openBytes(t, orig)
	var off uint64
	for _, e := range want.indexEntries {
		if e.BlockType == BlockTypeTracksBlock {
			off = e.Offset
		}
	}
	tracks, _ := want.Tracks()
	stale := append([]TrackEntry(nil), tracks...)
	stale[2].End -= 5
	b := append([]byte(nil), orig...)
	copy(b[off+tracksHeaderSize:], marshal(stale))

	img := openBytes(t, b)
	got, err := img.Tracks()
	if err != nil || fmt.Sprint(got) != fmt.Sprint(stale) {
		t.Errorf("Tracks = %v, %v; want the stored entries %v", got, err, stale)
	}
	if info := img.Info(); info.HasPartitions || info.HasSessions {
		t.Error("image with a damaged tracks block has partitions")
	}
	if err := img.Verify(); !errors.Is(err, ErrInvalidBlockCRC) {
		t.Errorf("Verify = %v, want ErrInvalidBlockCRC", err)
	}
	for _, a := range []uint64{0, 130, 250} {
		g, gs, gerr := img.ReadSector(a, false)
		w, ws, werr := want.ReadSector(a, false)
		if !bytes.Equal(g, w) || gs != ws || ErrorCode(gerr) != ErrorCode(werr) {
			t.Errorf("ReadSector(%d) differs from the undamaged image", a)
		}
	}
	if _, _, err := img.ReadSectorLong(0, false); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("ReadSectorLong = %v, want ErrTrackNotFound", err)
	}
}

// TestScenarioBlockAligned2K: 2048-byte block media with 2 KiB alignment and
// media tags.
func TestScenarioBlockAligned2K(t *testing.T) {
	r := rand.New(rand.NewPCG(44, 5))
	for _, opts := range []string{"compress=false;block_alignment=11", "compress=true;block_alignment=11;data_shift=4"} {
		t.Run(opts, func(t *testing.T) {
			s := &writeScript{}
			p := app()
			p.MediaType = media.CompactFlash
			p.SectorSize = 2048
			p.UserSectors = 300
			s.create(p, opts)
			s.mediaTag(MediaTagAtaIdentify, randBytes(r, 512))
			s.mediaTag(MediaTagPcmciaCis, compressible(r, 300))
			for i := uint64(0); i < 300; i++ {
				if i%50 == 7 {
					continue
				}
				s.ws(i, false, SectorStatusDumped, compressible(r, 2048))
			}
			s.close()
			path := checkScenario(t, s, scenarioCheck{})
			expectSectors(t, s, path)
			img, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			if img.header.BlockAlignmentShift != 11 {
				t.Errorf("block alignment shift = %d", img.header.BlockAlignmentShift)
			}
			for _, e := range img.indexEntries {
				if e.Offset%2048 != 0 {
					t.Errorf("block %v/%v at %d is not 2 KiB aligned", e.BlockType, e.DataType, e.Offset)
				}
			}
		})
	}
}

// consoleImage writes a console disc image of len(sectors) 2048-byte sectors
// with the given media tags and sector statuses, and opens it twice: as is,
// and as a generic disk (raw) to see the stored data. Sectors for which
// transformed is true read back re-encrypted or regenerated: checkScenario
// does not compare them with the written data (the caller does).
func consoleImage(t *testing.T, mt media.Type, tags map[MediaTagType][]byte, sectors [][]byte,
	statuses []SectorStatus, transformed func(i int) bool,
) (img, raw *Image) {
	t.Helper()
	s := &writeScript{}
	p := app()
	p.MediaType = mt
	p.SectorSize = 2048
	p.UserSectors = uint64(len(sectors))
	s.create(p, "compress=true")
	for _, tag := range []MediaTagType{MediaTagPs3DiscKey, MediaTagPs3Data1, MediaTagPs3EncryptionMap,
		MediaTagWiiUDiscKey, MediaTagWiiUPartitionKeyMap, MediaTagWiiPartitionKeyMap, MediaTagNgcwJunkMap} {
		if d, ok := tags[tag]; ok {
			s.mediaTag(tag, d)
		}
	}
	for i, d := range sectors {
		s.ws(uint64(i), false, statuses[i], d)
		if transformed != nil && transformed(i) {
			s.writeOnly()
		}
	}
	s.close()
	path := checkScenario(t, s, scenarioCheck{})
	open := func() *Image {
		i, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { i.Close() })
		return i
	}
	img, raw = open(), open()
	raw.header.MediaType = uint32(media.GENERIC_HDD)
	return img, raw
}

func mustMarshal(t *testing.T, m interface{ MarshalBinary() ([]byte, error) }) []byte {
	t.Helper()
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readOK(t *testing.T, img *Image, addr uint64) ([]byte, SectorStatus) {
	t.Helper()
	d, st, err := img.ReadSector(addr, false)
	if err != nil {
		t.Fatalf("ReadSector(%d): %v", addr, err)
	}
	return d, st
}

func fillKey16(b byte) [16]byte {
	var k [16]byte
	for i := range k {
		k[i] = b + byte(i)*3
	}
	return k
}

func testSeed(b byte) ngcw.Seed {
	var s ngcw.Seed
	for i := range s {
		s[i] = uint32(b)*0x01010101 + uint32(i)*0x9E3779B1
	}
	return s
}

// TestConsoleCrypto checks that the console paths really run: PS3, Wii U and
// Wii sectors stored decrypted are re-encrypted, and GameCube/Wii junk is
// regenerated.
func TestConsoleCrypto(t *testing.T) {
	r := rand.New(rand.NewPCG(45, 5))
	plain := func(n int) [][]byte {
		out := make([][]byte, n)
		for i := range out {
			out[i] = compressible(r, 2048)
		}
		return out
	}
	statuses := func(n int, st SectorStatus) []SectorStatus {
		out := make([]SectorStatus, n)
		for i := range out {
			out[i] = st
		}
		return out
	}

	for _, c := range []struct {
		name   string
		mt     media.Type
		keyTag MediaTagType
	}{{"ps3-bd-disc-key", media.PS3BD, MediaTagPs3DiscKey}, {"ps3-dvd-data1", media.PS3DVD, MediaTagPs3Data1}} {
		t.Run(c.name, func(t *testing.T) {
			key := fillKey16(7)
			dk := key
			if c.keyTag == MediaTagPs3Data1 {
				dk = ps3.DeriveDiscKey(key)
			}
			m := ps3.EncryptionMap{{StartSector: 0, EndSector: 9}, {StartSector: 40, EndSector: 44}}
			sectors := plain(200)
			disc := make([][]byte, len(sectors)) // the disc contents written
			for i, p := range sectors {
				disc[i] = bytes.Clone(p)
				if m.IsSectorEncrypted(uint64(i)) {
					_ = ps3.EncryptSector(dk, uint64(i), disc[i])
				}
			}
			img, raw := consoleImage(t, c.mt, map[MediaTagType][]byte{c.keyTag: key[:],
				MediaTagPs3EncryptionMap: mustMarshal(t, m)}, disc, statuses(200, SectorStatusUnencrypted), nil)

			enc, st := readOK(t, img, 20) // outside the plaintext regions
			stored, rst := readOK(t, raw, 20)
			if st != SectorStatusDumped || rst != SectorStatusUnencrypted || !bytes.Equal(enc, disc[20]) ||
				!bytes.Equal(stored, sectors[20]) {
				t.Fatalf("sector 20 not stored decrypted and re-encrypted (status %v / %v)", st, rst)
			}
			if img.console.ps3Key == nil || *img.console.ps3Key != dk || len(img.console.ps3Map) != 2 {
				t.Fatalf("PS3 crypto state not loaded: %+v", img.console.consoleCrypto)
			}
			for _, a := range []uint64{5, 42} { // plaintext regions
				got, st := readOK(t, img, a)
				stored, _ := readOK(t, raw, a)
				if st != SectorStatusDumped || !bytes.Equal(got, sectors[a]) || !bytes.Equal(stored, sectors[a]) {
					t.Errorf("plaintext region sector %d was modified", a)
				}
			}
		})
	}

	t.Run("wiiu", func(t *testing.T) {
		k0, k1 := fillKey16(1), fillKey16(2)
		m := wiiu.PartitionKeyMap{{StartSector: 3, EndSector: 6, Key: k0}, {StartSector: 6, EndSector: 12, Key: k1}}
		sectors := plain(192)
		dk := fillKey16(3)
		img, raw := consoleImage(t, media.WUOD, map[MediaTagType][]byte{MediaTagWiiUDiscKey: dk[:],
			MediaTagWiiUPartitionKeyMap: mustMarshal(t, m)}, sectors, statuses(192, SectorStatusUnencrypted),
			func(i int) bool { return m.IsSectorEncrypted(uint64(i)) })
		physical := func(phys int, key [16]byte) []byte {
			b := bytes.Join(sectors[phys*16:(phys+1)*16], nil)
			if err := wiiu.EncryptPhysicalSector(key, b); err != nil {
				t.Fatal(err)
			}
			return b
		}
		for _, c := range []struct {
			addr int
			key  *[16]byte
		}{{70, &k0}, {120, &k1}, {191, &k1}, {49, nil}, {100, nil}, {5, nil}} {
			got, st := readOK(t, img, uint64(c.addr))
			stored, _ := readOK(t, raw, uint64(c.addr))
			want := sectors[c.addr]
			if c.key != nil {
				slice := c.addr % 16
				want = physical(c.addr/16, *c.key)[slice*2048 : (slice+1)*2048]
			}
			if st != SectorStatusDumped || !bytes.Equal(got, want) || !bytes.Equal(stored, sectors[c.addr]) {
				t.Errorf("sector %d (encrypted %v): wrong contents", c.addr, c.key != nil)
			}
		}
		if len(img.console.wiiuMap) != 2 || !img.console.wiiuCacheValid {
			t.Error("Wii U crypto state not used")
		}
	})

	t.Run("wii", func(t *testing.T) {
		k0, k1 := fillKey16(4), fillKey16(5)
		m := wii.PartitionKeyMap{{StartSector: 2, EndSector: 5, Key: k0}, {StartSector: 5, EndSector: 10, Key: k1}}
		junk := ngcw.JunkMap{
			{Offset: 3 * 2048, Length: 2 * 2048, PartitionIndex: ngcw.NoPartition, Seed: testSeed(1)},
			{Offset: 20 * 2048, Length: 2048, PartitionIndex: ngcw.NoPartition, Seed: testSeed(2)},
			{Offset: 3*wii.GroupSize + wii.HashSize, Length: 4096, PartitionIndex: 0, Seed: testSeed(3)},
			{Offset: 6*wii.GroupSize + wii.HashSize + 8192, Length: 2048, PartitionIndex: 1, Seed: testSeed(4)},
		}
		sectors := plain(160)
		sts := statuses(160, SectorStatusUnencrypted)
		for _, a := range []int{3, 4, 20} { // junk outside the partitions
			sectors[a] = make([]byte, 2048)
			sts[a] = SectorStatusGenerable
		}
		img, raw := consoleImage(t, media.WOD, map[MediaTagType][]byte{MediaTagWiiPartitionKeyMap: mustMarshal(t, m),
			MediaTagNgcwJunkMap: mustMarshal(t, junk)}, sectors, sts,
			func(i int) bool { return m.IsSectorEncrypted(uint64(i)) || sts[i] == SectorStatusGenerable })
		group := func(g int, key [16]byte) []byte {
			b := bytes.Join(sectors[g*16:(g+1)*16], nil)
			if err := wii.ReconstructGroupJunk(b, uint64(g), m, junk); err != nil {
				t.Fatal(err)
			}
			if err := wii.EncryptGroup(key, b); err != nil {
				t.Fatal(err)
			}
			return b
		}
		for _, c := range []struct {
			addr int
			key  *[16]byte
		}{{2*16 + 3, &k0}, {3*16 + 1, &k0}, {6*16 + 5, &k1}, {9*16 + 15, &k1}, {10, nil}} {
			got, st := readOK(t, img, uint64(c.addr))
			want := sectors[c.addr]
			if c.key != nil {
				slice := c.addr % 16
				want = group(c.addr/16, *c.key)[slice*2048 : (slice+1)*2048]
			}
			if st != SectorStatusDumped || !bytes.Equal(got, want) {
				t.Errorf("sector %d (encrypted %v): wrong contents", c.addr, c.key != nil)
			}
			if stored, _ := readOK(t, raw, uint64(c.addr)); !bytes.Equal(stored, sectors[c.addr]) {
				t.Errorf("sector %d: stored data differs from the written data", c.addr)
			}
		}
		for _, a := range []uint64{3, 4, 20} {
			got, st := readOK(t, img, a)
			want := make([]byte, 2048)
			if err := junk.Regenerate(a*2048, want); err != nil {
				t.Fatal(err)
			}
			if st != SectorStatusDumped || !bytes.Equal(got, want) || bytes.Equal(got, make([]byte, 2048)) {
				t.Errorf("generable sector %d is not the regenerated junk", a)
			}
		}
		if len(img.console.wiiMap) != 2 || len(img.console.ngcwJunk) != 4 {
			t.Error("Wii crypto state not loaded")
		}
	})

	t.Run("gamecube", func(t *testing.T) {
		junk := ngcw.JunkMap{
			{Offset: 10 * 2048, Length: 3 * 2048, PartitionIndex: ngcw.NoPartition, Seed: testSeed(5)},
			{Offset: 40 * 2048, Length: 2048, PartitionIndex: ngcw.NoPartition, Seed: testSeed(6)},
			{Offset: 70*2048 + 100, Length: 4000, PartitionIndex: ngcw.NoPartition, Seed: testSeed(7)},
			{Offset: 150 * 2048, Length: 10 * 2048, PartitionIndex: ngcw.NoPartition, Seed: testSeed(8)},
		}
		sectors := plain(160)
		sts := statuses(160, SectorStatusDumped)
		generable := []uint64{1, 10, 11, 12, 40, 155}
		for _, a := range generable {
			sectors[a] = make([]byte, 2048)
			sts[a] = SectorStatusGenerable
		}
		img, _ := consoleImage(t, media.GOD, map[MediaTagType][]byte{MediaTagNgcwJunkMap: mustMarshal(t, junk)},
			sectors, sts, func(i int) bool { return sts[i] == SectorStatusGenerable })
		for _, a := range generable {
			got, st := readOK(t, img, a)
			want := make([]byte, 2048)
			if a != 1 { // sector 1 is outside the junk map: zero-filled
				if err := junk.Regenerate(a*2048, want); err != nil {
					t.Fatal(err)
				}
			}
			if st != SectorStatusDumped || !bytes.Equal(got, want) {
				t.Errorf("generable sector %d is not the regenerated junk", a)
			}
		}
		if got, st := readOK(t, img, 13); st != SectorStatusDumped || !bytes.Equal(got, sectors[13]) {
			t.Error("dumped sector modified")
		}
		if len(img.console.ngcwJunk) != 4 {
			t.Errorf("junk map has %d entries", len(img.console.ngcwJunk))
		}
	})
}

func TestConsoleStateReset(t *testing.T) {
	key := fillKey16(9)
	m := ps3.EncryptionMap{{StartSector: 0, EndSector: 3}}
	sectors := make([][]byte, 16)
	for i := range sectors {
		sectors[i] = make([]byte, 2048)
	}
	sts := make([]SectorStatus, 16)
	for i := range sts {
		sts[i] = SectorStatusDumped
	}
	img, _ := consoleImage(t, media.PS3BD, map[MediaTagType][]byte{MediaTagPs3DiscKey: key[:],
		MediaTagPs3EncryptionMap: mustMarshal(t, m)}, sectors, sts, nil)
	img.ps3LazyInit()
	k := img.console.ps3Key
	if k == nil || *k != key {
		t.Fatal("no PS3 key loaded")
	}
	img.console.reset()
	if *k != ([16]byte{}) || img.console.ps3Key != nil || img.console.ps3Initialized {
		t.Error("reset did not clear the PS3 state")
	}
	img.ps3LazyInit() // reloads from the media tags
	if img.console.ps3Key == nil || *img.console.ps3Key != key {
		t.Error("ps3LazyInit after reset did not reload the key")
	}
}
