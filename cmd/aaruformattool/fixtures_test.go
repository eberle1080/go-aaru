package main

// Test images shared by the verb tests. Every image is built at test time,
// with the go-aaru writer when it can produce it and by patching or laying out
// the on-disk structures otherwise (version 1 images, odd headers, damaged
// tables). Images are built once per test binary run, on first use, and get
// fixed GUIDs and times (stampHeader) so that the tool's output is
// reproducible.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

var (
	fxMu    sync.Mutex
	fxDir   string
	fxBuilt = map[string]string{}
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aaruformattool-fixtures-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fxDir = dir
	// Tests may change the working directory.
	if abs, err := filepath.Abs(expectedDir); err == nil {
		expectedDir = abs
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fixture returns the path of the named test image, building it on first
// use. The file is shared: copy it (fixtureCopy) before modifying it.
func fixture(t *testing.T, name string) string {
	t.Helper()
	fxMu.Lock()
	defer fxMu.Unlock()
	return fxBuildLocked(t, name)
}

func fxBuildLocked(t *testing.T, name string) string {
	t.Helper()
	if p, ok := fxBuilt[name]; ok {
		return p
	}
	build, ok := fxBuilders[name]
	if !ok {
		t.Fatalf("unknown fixture %q", name)
	}
	path := filepath.Join(fxDir, name+".aif")
	build(t, path)
	fxBuilt[name] = path
	return path
}

// fixtureDep returns another fixture from inside a builder (the lock is
// already held).
func fixtureDep(t *testing.T, name string) string {
	t.Helper()
	return fxBuildLocked(t, name)
}

// fixtureCopy copies the named test image into a temporary directory and
// returns the copy's path.
func fixtureCopy(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), name+".aif")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// fixtureBytes returns the contents of the named test image.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fxBuilders builds every named test image at path.
// Other test files may add builders from their init functions.
var fxBuilders = map[string]func(t *testing.T, path string){}

func init() {
	for name, build := range fxBuilderTable() {
		fxBuilders[name] = build
	}
}

func fxBuilderTable() map[string]func(t *testing.T, path string) {
	return map[string]func(t *testing.T, path string){
		// Optical discs.
		"cdmode1":    func(t *testing.T, p string) { fxBuildCDROM(t, p, aaru.TrackTypeCdMode1, 600) },
		"cdmode2":    func(t *testing.T, p string) { fxBuildCDROM(t, p, aaru.TrackTypeCdMode2Formless, 600) },
		"tiny_cd":    fxBuildTinyCD,
		"cd_raw":     func(t *testing.T, p string) { fxBuildMixedCD(t, p, "compress=false", -1) },
		"cd_lzma":    func(t *testing.T, p string) { fxBuildMixedCD(t, p, "compress=true", -1) },
		"cd_zstd":    func(t *testing.T, p string) { fxBuildMixedCD(t, p, "compress=true;zstd=true", -1) },
		"cd_gap":     func(t *testing.T, p string) { fxBuildMixedCD(t, p, "compress=true", 87) },
		"cd_tags":    fxBuildCDTags,
		"dvd_long":   fxBuildDVDLong,
		"bd_long":    fxBuildBDLong,
		"ps3_bd":     fxBuildPS3BD,
		"gamecube":   func(t *testing.T, p string) { fxBuildNintendo(t, p, media.GOD, 11) },
		"wii":        func(t *testing.T, p string) { fxBuildNintendo(t, p, media.WOD, 12) },
		"cdmode1_v1": func(t *testing.T, p string) { fxBuildV1(t, fixtureDep(t, "cdmode1"), p) },
		"cdmode2_v1": func(t *testing.T, p string) { fxBuildV1(t, fixtureDep(t, "cdmode2"), p) },
		"mf2hd_v1":   func(t *testing.T, p string) { fxBuildV1(t, fixtureDep(t, "mf2hd"), p) },
		"floptical":  fxBuildFloptical,
		"mf2hd":      fxBuildMF2HD,
		"apple_sony": func(t *testing.T, p string) { fxBuildTagged(t, p, media.AppleSonyDS, 12) },
		"apple_profile": func(t *testing.T, p string) {
			fxBuildTagged(t, p, media.AppleProfile, 20)
		},
		"priam": func(t *testing.T, p string) { fxBuildTagged(t, p, media.PriamDataTower, 24) },

		// Block media.
		"blk_raw":       func(t *testing.T, p string) { fxBuildBlock(t, p, "compress=false", 600) },
		"blk_zstd":      func(t *testing.T, p string) { fxBuildBlock(t, p, "compress=true;zstd=true", 600) },
		"blk_ml":        func(t *testing.T, p string) { fxBuildBlock(t, p, "compress=true;table_shift=2", 2000) },
		"blk_ec":        fxBuildBlockEC,
		"ml_subindex":   fxBuildSubindex,
		"blk_idx2":      fxBuildIndex2,
		"meta_overread": fxBuildMetaOverread,
		"flux_only":     fxBuildFluxOnly,
		"flux_lzma":     fxBuildFluxLzma,
		"statuses":      fxBuildStatuses,
		"bad_blocks":    fxBuildBadBlocks,

		// Metadata.
		"meta_checksums":    fxBuildChecksums,
		"meta_all":          fxBuildMetadataAll,
		"meta_dumphw":       fxBuildDumpHardware,
		"meta_tape":         fxBuildTape,
		"meta_resume_title": fxBuildResumeTitle,

		// Headers and tables written by hand or patched.
		"app_name_full": func(t *testing.T, p string) {
			fxSmallBlock(t, p, "0123456789012345678901234567890123456789012345678901234567890123")
		},
		"dic_magic": func(t *testing.T, p string) {
			fxSmallBlock(t, p, "go-aaru-test")
			fxPatchHeader(t, p, func(h *aaru.Header) { h.Identifier = aaru.DICMagic })
		},
		"version3": func(t *testing.T, p string) {
			fxSmallBlock(t, p, "go-aaru-test")
			fxPatchHeader(t, p, func(h *aaru.Header) { h.ImageMajorVersion = 3 })
		},
		"ddt_bad_crc": fxBuildDDTBadCRC,
		"no_ddt":      fxBuildNoDDT,
		"short_v2_header": func(t *testing.T, p string) {
			fxSmallBlock(t, p, "go-aaru-test")
			data := fxRead(t, p)
			fxWrite(t, p, data[:120])
		},
	}
}

// ---- helpers ----

func fxRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fxWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fxRand is a deterministic generator for a given seed and stream.
func fxRand(seed, stream uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, stream^0x9E3779B97F4A7C15))
}

// fxData returns n bytes of compressible deterministic data (runs of
// letters) for sector lba of a data set seed.
func fxData(seed uint64, lba int64, n int) []byte {
	r := fxRand(seed, uint64(lba))
	b := make([]byte, n)
	for i := 0; i < n; {
		c := byte('a' + r.IntN(16))
		run := 1 + r.IntN(24)
		for j := 0; j < run && i < n; j++ {
			b[i] = c
			i++
		}
	}
	return b
}

// fxCreate creates an image with the given options string.
func fxCreate(t *testing.T, path string, p aaru.CreateParams, opts string) *aaru.Image {
	t.Helper()
	if p.Application == "" {
		p.Application = "go-aaru-test"
		p.ApplicationMajorVersion, p.ApplicationMinorVersion = 1, 0
	}
	o := aaru.ParseOptions(opts)
	img, err := aaru.Create(path, p, &o)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// fxClose closes an image being written and stamps its header.
func fxClose(t *testing.T, img *aaru.Image, path string) {
	t.Helper()
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	stampHeader(t, path)
}

func fxCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fxPatchHeader rewrites the header of the image at path.
func fxPatchHeader(t *testing.T, path string, fn func(*aaru.Header)) {
	t.Helper()
	data := fxRead(t, path)
	var h aaru.Header
	if _, err := binary.Decode(data, binary.LittleEndian, &h); err != nil {
		t.Fatal(err)
	}
	fn(&h)
	b, err := binary.Append(nil, binary.LittleEndian, h)
	if err != nil {
		t.Fatal(err)
	}
	copy(data, b)
	fxWrite(t, path, data)
}

// fxCDSector builds a raw 2352-byte sector of track type tt for lba with
// correct sync, header, EDC and ECC. Mode 2 sectors are Form 2 when form2.
func fxCDSector(tt aaru.TrackType, lba int64, seed uint64, form2 bool) []byte {
	switch tt {
	case aaru.TrackTypeAudio, aaru.TrackTypeData:
		return fxData(seed, lba, cdecc.SectorSize)
	}
	s := make([]byte, cdecc.SectorSize)
	ct := cdecc.TrackTypeCdMode1
	if tt == aaru.TrackTypeCdMode1 {
		copy(s[16:], fxData(seed, lba, 2048))
	} else {
		sub := []byte{0, 0, 0x08, 0}
		ct = cdecc.TrackTypeCdMode2Form1
		n := 2048
		if form2 {
			sub[2] = 0x20
			ct = cdecc.TrackTypeCdMode2Form2
			n = 2324
		}
		copy(s[0x10:], sub)
		copy(s[0x14:], sub)
		copy(s[0x18:], fxData(seed, lba, n))
	}
	if err := cdecc.ReconstructPrefix(s, ct, lba); err != nil {
		panic(err)
	}
	if err := cdecc.Reconstruct(s, ct); err != nil {
		panic(err)
	}
	return s
}

// ---- optical discs ----

// fxBuildCDROM writes a single-session CD-ROM with one data track of n raw
// sectors (Mode 1, or Mode 2 alternating Form 1 and every third sector Form
// 2), a TOC and an MCN.
func fxBuildCDROM(t *testing.T, path string, tt aaru.TrackType, n uint64) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: n}, "")
	fxCheck(t, img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: tt, Start: 0, End: int64(n) - 1,
		Session: 1, Flags: 4}}))
	for lba := uint64(0); lba < n; lba++ {
		s := fxCDSector(tt, int64(lba), 1, lba%3 == 2)
		fxCheck(t, img.WriteSectorLong(lba, false, s, aaru.SectorStatusDumped))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdToc, fxData(2, 0, 12)))
	fxClose(t, img, path)
}

// fxBuildTinyCD writes a 12-sector CD (2 lead-in and 2 lead-out sectors):
// Mode 1 sectors 0-5, sector 3 with a damaged ECC byte and sector 4 with a
// damaged header (minute 10), and audio sectors 6-11.
func fxBuildTinyCD(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.CD, SectorSize: 2352, UserSectors: 12,
		NegativeSectors: 2, OverflowSectors: 2}, "")
	fxCheck(t, img.SetTracks([]aaru.TrackEntry{
		{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 5, Session: 1, Flags: 4},
		{Sequence: 2, Type: aaru.TrackTypeAudio, Start: 6, End: 13, Session: 1},
	}))
	for _, a := range []uint64{1, 2} {
		fxCheck(t, img.WriteSectorLong(a, true, fxCDSector(aaru.TrackTypeCdMode1, -int64(a), 3, false),
			aaru.SectorStatusDumped))
	}
	for lba := uint64(0); lba < 14; lba++ {
		tt := aaru.TrackTypeCdMode1
		if lba >= 6 {
			tt = aaru.TrackTypeAudio
		}
		s := fxCDSector(tt, int64(lba), 3, false)
		switch lba {
		case 3:
			s[0x830] ^= 0x5A // in ECC P, which ECC Q covers: both fail, the EDC is right
		case 4:
			s[0x0C] = 0x10 // header minute: EDC, ECC P and ECC Q fail
		}
		fxCheck(t, img.WriteSectorLong(lba, false, s, aaru.SectorStatusDumped))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdToc, fxData(4, 0, 20)))
	fxClose(t, img, path)
}

// fxBuildMixedCD writes an 800-sector CD with 15 lead-in and 10 lead-out
// sectors: a Mode 1 track (0-399, sector 5 and 9 with damaged EDC/ECC, lead-in
// sector 4 not dumped) and an audio track (400-809), with TOC, full TOC,
// CD-Text and MCN media tags. Sector skip, if not negative, is not written.
func fxBuildMixedCD(t *testing.T, path, opts string, skip int64) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.CD, SectorSize: 2352, UserSectors: 800,
		NegativeSectors: 15, OverflowSectors: 10}, opts)
	fxCheck(t, img.SetTracks([]aaru.TrackEntry{
		{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 399, Session: 1, Flags: 4},
		{Sequence: 2, Type: aaru.TrackTypeAudio, Start: 400, End: 809, Pregap: 150, Session: 1},
	}))
	for a := uint64(1); a <= 15; a++ {
		if a == 4 {
			continue
		}
		fxCheck(t, img.WriteSectorLong(a, true, fxCDSector(aaru.TrackTypeCdMode1, -int64(a), 5, false),
			aaru.SectorStatusDumped))
	}
	for lba := int64(0); lba < 810; lba++ {
		if lba == skip {
			continue
		}
		tt := aaru.TrackTypeCdMode1
		if lba >= 400 {
			tt = aaru.TrackTypeAudio
		}
		s := fxCDSector(tt, lba, 5, false)
		st := aaru.SectorStatusDumped
		if lba == 5 || lba == 9 {
			s[0x400] ^= 0xFF
			st = aaru.SectorStatusErrored
		}
		fxCheck(t, img.WriteSectorLong(uint64(lba), false, s, st))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdToc, fxData(6, 0, 36)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagFullToc, fxData(6, 1, 400)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdText, fxData(6, 2, 300)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdMcn, []byte("0123456789012")))
	fxClose(t, img, path)
}

// fxBuildCDTags writes a CD-ROM with user data only (no prefix/suffix) and
// per-sector tags: subchannel, track flags and ISRC.
func fxBuildCDTags(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 64}, "")
	var isrc [13]byte
	copy(isrc[:], "USABC2400001")
	fxCheck(t, img.SetTracks([]aaru.TrackEntry{
		{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 31, Session: 1, Flags: 4},
		{Sequence: 2, Type: aaru.TrackTypeCdMode1, Start: 32, End: 63, Session: 1, Flags: 4, ISRC: isrc},
	}))
	for lba := uint64(0); lba < 64; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(7, int64(lba), 2048), aaru.SectorStatusDumped))
		fxCheck(t, img.WriteSectorTag(lba, false, aaru.SectorTagCdSubchannel, fxData(8, int64(lba), 96)))
	}
	fxCheck(t, img.WriteSectorTag(40, false, aaru.SectorTagCdTrackFlags, []byte{4}))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagCdMcn, []byte("1234567890123")))
	fxClose(t, img, path)
}

// fxBuildDVDLong writes a 400-sector DVD-ROM with 2064-byte long sectors
// (ID, IED, CPR_MAI, EDC) and PFI, CMI and DMI media tags.
func fxBuildDVDLong(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.DVDROM, SectorSize: 2048, UserSectors: 400}, "")
	for lba := uint64(0); lba < 400; lba++ {
		s := make([]byte, 2064)
		binary.BigEndian.PutUint32(s, uint32(0x30000+lba))
		copy(s[4:12], fxData(9, int64(lba), 8))
		copy(s[12:], fxData(10, int64(lba%50), 2048))
		copy(s[2060:], fxData(11, int64(lba), 4))
		st := aaru.SectorStatusDumped
		if lba%9 == 4 {
			st = aaru.SectorStatusErrored
		}
		fxCheck(t, img.WriteSectorLong(lba, false, s, st))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagDvdPfi, fxData(12, 0, 2048)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagDvdCmi, []byte{0, 0, 0, 0}))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagDvdDmi, fxData(12, 1, 2052)))
	fxClose(t, img, path)
}

// fxBuildBDLong writes a 300-sector BD-ROM with 2052-byte long sectors.
func fxBuildBDLong(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.BDROM, SectorSize: 2048, UserSectors: 300}, "")
	for lba := uint64(0); lba < 300; lba++ {
		s := append(fxData(13, int64(lba%40), 2048), fxData(14, int64(lba), 4)...)
		fxCheck(t, img.WriteSectorLong(lba, false, s, aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxBuildPS3BD writes a 200-sector PS3 Blu-ray with a disc key and an
// encryption map listing only the first sectors (stored in plaintext).
func fxBuildPS3BD(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.PS3BD, SectorSize: 2048, UserSectors: 200}, "")
	for lba := uint64(0); lba < 200; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(15, int64(lba), 2048), aaru.SectorStatusDumped))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagPs3DiscKey, fxData(16, 0, 16)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagPs3EncryptionMap, fxPS3EncryptionMap()))
	fxClose(t, img, path)
}

// fxPS3EncryptionMap is a serialized encryption map with one unencrypted
// region (sectors 0-19).
func fxPS3EncryptionMap() []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b, 1)
	binary.LittleEndian.PutUint32(b[4:], 0)
	binary.LittleEndian.PutUint32(b[8:], 19)
	return b
}

// fxBuildNintendo writes a 100-sector GameCube or Wii disc image (plain
// 2048-byte sectors).
func fxBuildNintendo(t *testing.T, path string, mt media.Type, seed uint64) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: mt, SectorSize: 2048, UserSectors: 100}, "")
	for lba := uint64(0); lba < 100; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(seed, int64(lba), 2048), aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// ---- block media ----

// fxBuildMF2HD writes a 1.44 MB floppy (2880 sectors, geometry 80/2/18)
// whose second half is blank.
func fxBuildMF2HD(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.DOS_35_HD, SectorSize: 512, UserSectors: 2880}, "")
	for lba := uint64(0); lba < 2880; lba++ {
		data := make([]byte, 512)
		if lba < 1440 || lba == 2879 {
			data = fxData(17, int64(lba), 512)
		}
		fxCheck(t, img.WriteSector(lba, false, data, aaru.SectorStatusDumped))
	}
	fxCheck(t, img.SetGeometry(80, 2, 18))
	fxClose(t, img, path)
}

// fxBuildFloptical writes a mostly blank 21 MB floptical image (only the
// first sectors hold data).
func fxBuildFloptical(t *testing.T, path string) {
	const n = 1600
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.Floptical, SectorSize: 512, UserSectors: n}, "")
	for lba := uint64(0); lba < n; lba++ {
		data := make([]byte, 512)
		if lba < 16 {
			data = fxData(18, int64(lba), 512)
		}
		fxCheck(t, img.WriteSector(lba, false, data, aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxBuildTagged writes a 200-sector Apple/Priam disk with tagSize-byte sector
// tags (long sectors of 512+tagSize bytes).
func fxBuildTagged(t *testing.T, path string, mt media.Type, tagSize int) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: mt, SectorSize: 512, UserSectors: 200}, "")
	for lba := uint64(0); lba < 200; lba++ {
		s := append(fxData(19, int64(lba), 512), fxData(20, int64(lba), tagSize)...)
		fxCheck(t, img.WriteSectorLong(lba, false, s, aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxBlockSector is the content of sector lba of the hard disk images: some
// sectors repeat (deduplicated), others are unique.
func fxBlockSector(lba uint64) []byte {
	if lba%7 == 3 {
		return fxData(21, 3, 512)
	}
	return fxData(21, int64(lba), 512)
}

// fxBuildBlock writes an n-sector hard disk with ATA IDENTIFY and PCMCIA CIS
// media tags (sector 17 errored).
func fxBuildBlock(t *testing.T, path, opts string, n uint64) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: n}, opts)
	for lba := uint64(0); lba < n; lba++ {
		st := aaru.SectorStatusDumped
		if lba == 17 {
			st = aaru.SectorStatusErrored
		}
		fxCheck(t, img.WriteSector(lba, false, fxBlockSector(lba), st))
	}
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagAtaIdentify, fxData(22, 0, 512)))
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagPcmciaCis, fxData(22, 1, 40)))
	fxClose(t, img, path)
}

// fxBuildBlockEC writes a 400-sector hard disk with Reed-Solomon erasure
// coding (K=4, M=2) whose sectors 300-309 were never dumped.
func fxBuildBlockEC(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 400},
		"compress=true;data_shift=6")
	fxCheck(t, img.SetErasureCoding(aaru.ErasureCodingRsVandermonde, 4, 2))
	for lba := uint64(0); lba < 400; lba++ {
		if lba >= 300 && lba < 310 {
			continue
		}
		fxCheck(t, img.WriteSector(lba, false, fxBlockSector(lba), aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxBuildSubindex writes a multi-level DDT image whose index lists some of its
// entries through a subindex referenced from the middle of the entry list.
func fxBuildSubindex(t *testing.T, path string) {
	fxBuildBlock(t, path, "compress=true;table_shift=2", 400)
	r := maintParseRaw(t, fxRead(t, path))
	if len(r.index) < 3 {
		t.Fatalf("only %d index entries", len(r.index))
	}
	// Move the last two entries to a subindex placed in the middle.
	sub := r.index[len(r.index)-2:]
	body, _ := binary.Append(nil, binary.LittleEndian, sub)
	ih, _ := binary.Append(nil, binary.LittleEndian, aaru.IndexHeader3{Identifier: aaru.BlockTypeIndexBlock3,
		Entries: uint64(len(sub)), CRC64: crc64.Checksum(body)})
	subOff := r.appendAligned(append(ih, body...))
	rest := append([]aaru.IndexEntry(nil), r.index[:len(r.index)-2]...)
	mid := len(rest) / 2
	r.index = append(append(append([]aaru.IndexEntry(nil), rest[:mid]...),
		aaru.IndexEntry{BlockType: aaru.BlockTypeIndexBlock3, Offset: subOff}), rest[mid:]...)
	r.writeIndex()
	fxWrite(t, path, r.data)
}

// fxBuildIndex2 writes an LZMA compressed hard disk whose index is a version
// 2 index (IDX2).
func fxBuildIndex2(t *testing.T, path string) {
	fxBuildBlock(t, path, "compress=true", 600)
	r := maintParseRaw(t, fxRead(t, path))
	body, _ := binary.Append(nil, binary.LittleEndian, r.index)
	ih, _ := binary.Append(nil, binary.LittleEndian, aaru.IndexHeader2{Identifier: aaru.BlockTypeIndexBlock2,
		Entries: uint64(len(r.index)), CRC64: crc64.Checksum(body)})
	r.header.IndexOffset = r.appendAligned(append(ih, body...))
	r.put(0, r.header)
	fxWrite(t, path, r.data)
}

// fxBuildMetaOverread writes a small hard disk whose last block is a 512-byte
// metadata block (a creator string filling it) ending on an alignment
// boundary, immediately followed by the short index.
func fxBuildMetaOverread(t *testing.T, path string) {
	fxSmallBlock(t, path, "go-aaru-test")
	r := maintParseRaw(t, fxRead(t, path))
	const blockSize = 512
	hdrSize := binary.Size(aaru.MetadataBlockHeader{})
	creator := aaru.EncodeUTF16(string(bytes.Repeat([]byte("c"), (blockSize-hdrSize)/2-1)))
	h := aaru.MetadataBlockHeader{Identifier: aaru.BlockTypeMetadataBlock, BlockSize: blockSize,
		CreatorOffset: uint32(hdrSize), CreatorLength: uint32(len(creator))}
	block, _ := binary.Append(nil, binary.LittleEndian, h)
	block = append(block, creator...)
	block = append(block, make([]byte, blockSize-len(block))...)
	r.index = append(r.index, aaru.IndexEntry{BlockType: aaru.BlockTypeMetadataBlock, Offset: r.appendAligned(block)})
	r.writeIndex()
	if r.header.IndexOffset%blockSize != 0 || r.index[len(r.index)-1].Offset+blockSize != r.header.IndexOffset {
		t.Fatal("the metadata block is not followed by the index")
	}
	fxWrite(t, path, r.data)
}

// fxBuildFluxOnly writes a floppy image holding only a flux capture.
func fxBuildFluxOnly(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.DOS_35_HD, SectorSize: 512}, "")
	fxCheck(t, img.WriteFluxCapture(aaru.FluxCaptureKey{Head: 0, Track: 0}, 25000, 25000,
		fxData(23, 0, 4000), fxData(23, 1, 64)))
	fxClose(t, img, path)
}

// fxBuildFluxLzma writes a floppy with sectors and LZMA compressed flux
// captures of two tracks.
func fxBuildFluxLzma(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.DOS_35_HD, SectorSize: 512, UserSectors: 72},
		"compress=true")
	for lba := uint64(0); lba < 72; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(24, int64(lba), 512), aaru.SectorStatusDumped))
	}
	for track := uint16(0); track < 2; track++ {
		fxCheck(t, img.WriteFluxCapture(aaru.FluxCaptureKey{Head: uint32(track % 2), Track: track}, 25000, 25000,
			fxData(25, int64(track), 6000), fxData(26, int64(track), 32)))
	}
	fxClose(t, img, path)
}

// fxSmallBlock writes a 4-sector hard disk created by app.
func fxSmallBlock(t *testing.T, path, app string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 4,
		Application: app, ApplicationMajorVersion: 1}, "compress=false")
	for lba := uint64(0); lba < 4; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(27, int64(lba), 512), aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxRewriteUserDDT replaces the entries of the (uncompressed, single level)
// user data DDT of the image at path.
func fxRewriteUserDDT(t *testing.T, path string, fn func(entries []uint64)) {
	t.Helper()
	r := maintParseRaw(t, fxRead(t, path))
	off := r.find(aaru.BlockTypeDeDuplicationTable2, aaru.DataTypeUserData)
	templ, entries := r.ddt2(t, off)
	fn(entries)
	r.setIndexOffset(aaru.BlockTypeDeDuplicationTable2, aaru.DataTypeUserData,
		r.appendDDT2(t, templ, aaru.CompressionNone, entries))
	r.writeIndex()
	fxWrite(t, path, r.data)
}

// fxBuildStatuses writes 6 sectors with odd statuses for block media:
// dumped, not dumped, Mode 1 correct, twin, generable and the undefined 15.
func fxBuildStatuses(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 6},
		"compress=false;deduplicate=false")
	for lba := uint64(0); lba < 6; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(28, int64(lba), 512), aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
	statuses := []aaru.SectorStatus{aaru.SectorStatusDumped, aaru.SectorStatusNotDumped,
		aaru.SectorStatusMode1Correct, aaru.SectorStatusTwin, aaru.SectorStatusGenerable, 15}
	fxRewriteUserDDT(t, path, func(entries []uint64) {
		for i, st := range statuses {
			if st == aaru.SectorStatusNotDumped {
				entries[i] = 0
				continue
			}
			entries[i] = entries[i]&(1<<60-1) | uint64(st)<<60
		}
	})
}

// fxBuildBadBlocks writes a hard disk whose first user data block has a bad
// CRC.
func fxBuildBadBlocks(t *testing.T, path string) {
	fxBuildBlock(t, path, "compress=false", 64)
	data := fxRead(t, path)
	const blockHeaderSize = 36
	for off := 0; off+blockHeaderSize+64 < len(data); off++ {
		if string(data[off:off+4]) == "DBLK" && data[off+4] == byte(aaru.DataTypeUserData) && data[off+5] == 0 {
			data[off+blockHeaderSize+16] ^= 0xFF
			fxWrite(t, path, data)
			return
		}
	}
	t.Fatal("no user data block")
}

// fxBuildDDTBadCRC writes a small hard disk whose user data DDT has a wrong
// CRC64.
func fxBuildDDTBadCRC(t *testing.T, path string) {
	fxSmallBlock(t, path, "go-aaru-test")
	r := maintParseRaw(t, fxRead(t, path))
	off := r.find(aaru.BlockTypeDeDuplicationTable2, aaru.DataTypeUserData)
	h, _ := r.ddt2(t, off)
	h.CRC64++
	r.put(off, h)
	fxWrite(t, path, r.data)
}

// fxBuildNoDDT writes an image whose index lists no DDT (and no user data).
func fxBuildNoDDT(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 4},
		"compress=false")
	fxCheck(t, img.WriteMediaTag(aaru.MediaTagScsiInquiry, fxData(29, 0, 36)))
	fxClose(t, img, path)
	r := maintParseRaw(t, fxRead(t, path))
	var kept []aaru.IndexEntry
	for _, e := range r.index {
		if e.BlockType != aaru.BlockTypeDeDuplicationTable2 && e.BlockType != aaru.BlockTypeDeDuplicationTableSecondary {
			kept = append(kept, e)
		}
	}
	r.index = kept
	r.writeIndex()
	fxWrite(t, path, r.data)
}

// ---- metadata ----

// fxBuildChecksums writes a hard disk with every whole-image checksum.
func fxBuildChecksums(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 100},
		"md5=true;sha1=true;sha256=true;blake3=true;spamsum=true")
	for lba := uint64(0); lba < 100; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxBlockSector(lba), aaru.SectorStatusDumped))
	}
	fxClose(t, img, path)
}

// fxBuildMetadataAll writes a hard disk with every metadata string, the media
// sequence, the geometry, dump hardware and Aaru JSON metadata.
func fxBuildMetadataAll(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 120}, "")
	for lba := uint64(0); lba < 120; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxBlockSector(lba), aaru.SectorStatusDumped))
	}
	for f := aaru.MetadataCreator; f <= aaru.MetadataDriveFirmwareRevision; f++ {
		fxCheck(t, img.SetMetadataString(f, fmt.Sprintf("%v value", f)))
	}
	fxCheck(t, img.SetMediaSequence(2, 3))
	fxCheck(t, img.SetGeometry(10, 4, 3))
	fxCheck(t, img.SetDumpHardware([]aaru.DumpHardware{{Manufacturer: "Maker", Model: "Drive 1",
		SoftwareName: "dumper", SoftwareVersion: "1.0", Extents: []aaru.DumpExtent{{Start: 0, End: 119}}}}))
	fxCheck(t, img.SetAaruJSONMetadata([]byte(`{"AaruMetadata":{"Title":"test"}}`)))
	fxClose(t, img, path)
}

// fxBuildDumpHardware writes a hard disk with raw dump hardware of two
// devices (one with many extents).
func fxBuildDumpHardware(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 50}, "")
	for lba := uint64(0); lba < 50; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxBlockSector(lba), aaru.SectorStatusDumped))
	}
	hw := []aaru.DumpHardware{
		{Manufacturer: "Maker", Model: "Reader", Revision: "A", Firmware: "1.2", Serial: "SN1",
			SoftwareName: "dumper", SoftwareVersion: "2.0", SoftwareOperatingSystem: "Linux",
			Extents: []aaru.DumpExtent{{Start: 0, End: 9}, {Start: 20, End: 29}}},
		{Model: "Second", Extents: []aaru.DumpExtent{{Start: 0, End: 1}, {Start: 3, End: 4}, {Start: 6, End: 7}, {Start: 9, End: 49}}},
	}
	raw, err := aaru.EncodeDumpHardware(hw)
	fxCheck(t, err)
	fxCheck(t, img.SetDumpHardwareRaw(raw))
	fxClose(t, img, path)
}

// fxBuildTape writes a tape image with two partitions and three files.
func fxBuildTape(t *testing.T, path string) {
	img := fxCreate(t, path, aaru.CreateParams{MediaType: media.LTO, SectorSize: 512, UserSectors: 30, Tape: true}, "")
	for lba := uint64(0); lba < 30; lba++ {
		fxCheck(t, img.WriteSector(lba, false, fxData(30, int64(lba), 512), aaru.SectorStatusDumped))
	}
	fxCheck(t, img.SetTapePartition(0, 0, 19))
	fxCheck(t, img.SetTapePartition(1, 20, 29))
	fxCheck(t, img.SetTapeFile(0, 0, 0, 9))
	fxCheck(t, img.SetTapeFile(0, 1, 10, 19))
	fxCheck(t, img.SetTapeFile(1, 0, 20, 29))
	fxClose(t, img, path)
}

// fxBuildResumeTitle writes a CD-ROM, then resumes it to add a media title.
func fxBuildResumeTitle(t *testing.T, path string) {
	fxBuildCDROM(t, path, aaru.TrackTypeCdMode1, 40)
	o := aaru.ParseOptions("")
	img, err := aaru.Resume(path, &o)
	fxCheck(t, err)
	fxCheck(t, img.SetMediaTitle("Resumed title"))
	fxClose(t, img, path)
}

// ---- version 1 images ----

// fxCRCv1 is a CRC64 as version 1 images store it (byte swapped).
func fxCRCv1(data []byte) uint64 { return bits.ReverseBytes64(crc64.Checksum(data)) }

// fxBuildV1 lays out the contents of the version 2 image src as a version 1
// image at dst: a 104-byte header with a UTF-16 application name, uncompressed
// user data blocks addressed by a DDT v1 (absolute block offsets), the CD
// prefixes and suffixes of every sector as raw arenas, the tracks and
// geometry blocks of src, and an INDX index. Version 1 CRC64 values are
// stored byte swapped.
func fxBuildV1(t *testing.T, src, dst string) {
	t.Helper()
	img, err := aaru.Open(src)
	fxCheck(t, err)
	defer img.Close()
	srcRaw := maintParseRaw(t, fxRead(t, src))
	info := img.Info()
	tracks, _ := img.Tracks()
	isCD := len(tracks) > 0

	const shift = 12
	buf := make([]byte, 512)
	var index []aaru.IndexEntry
	add := func(bt aaru.BlockType, dt aaru.DataType, block []byte) uint64 {
		off := uint64(len(buf))
		buf = append(buf, block...)
		buf = append(buf, make([]byte, (8-len(buf)%8)%8)...)
		index = append(index, aaru.IndexEntry{BlockType: bt, DataType: dt, Offset: off})
		return off
	}
	dataBlock := func(dt aaru.DataType, sectorSize uint32, data []byte) []byte {
		h, _ := binary.Append(nil, binary.LittleEndian, aaru.BlockHeader{Identifier: aaru.BlockTypeDataBlock,
			Type: dt, Compression: aaru.CompressionNone, SectorSize: sectorSize, CmpLength: uint32(len(data)),
			Length: uint32(len(data)), CmpCRC64: fxCRCv1(data), CRC64: fxCRCv1(data)})
		return append(h, data...)
	}

	ddt := make([]uint64, info.Sectors)
	var prefix, suffix []byte
	// Runs of sectors of the same size go to the same block (at most 256).
	var run [][]byte
	var runStart uint64
	var runSize uint32
	flush := func() {
		if len(run) == 0 {
			return
		}
		off := add(aaru.BlockTypeDataBlock, aaru.DataTypeUserData, dataBlock(aaru.DataTypeUserData, runSize,
			bytes.Join(run, nil)))
		for i := range run {
			ddt[runStart+uint64(i)] = off<<shift | uint64(i)
		}
		run = nil
	}
	for s := uint64(0); s < info.Sectors; s++ {
		var user []byte
		long, _, lerr := img.ReadSectorLong(s, false)
		plain, _, perr := img.ReadSector(s, false)
		if perr != nil {
			flush()
			if isCD {
				prefix = append(prefix, make([]byte, 16)...)
				suffix = append(suffix, make([]byte, 288)...)
			}
			continue
		}
		user = plain
		if isCD {
			var tt aaru.TrackType
			for _, tr := range tracks {
				if int64(s) >= tr.Start && int64(s) <= tr.End {
					tt = tr.Type
				}
			}
			switch {
			case lerr != nil || len(long) != cdecc.SectorSize || tt == aaru.TrackTypeAudio:
				prefix = append(prefix, make([]byte, 16)...)
				suffix = append(suffix, make([]byte, 288)...)
			case tt == aaru.TrackTypeCdMode1:
				prefix = append(prefix, long[:16]...)
				suffix = append(suffix, long[2064:]...)
			default: // Mode 2: the user data holds everything after the prefix
				prefix = append(prefix, long[:16]...)
				suffix = append(suffix, make([]byte, 288)...)
				user = long[16:]
			}
		}
		if len(run) == 256 || (len(run) > 0 && uint32(len(user)) != runSize) {
			flush()
		}
		if len(run) == 0 {
			runStart, runSize = s, uint32(len(user))
		}
		run = append(run, user)
	}
	flush()

	if isCD {
		add(aaru.BlockTypeDataBlock, aaru.DataTypeCdSectorPrefix, dataBlock(aaru.DataTypeCdSectorPrefix, 16, prefix))
		add(aaru.BlockTypeDataBlock, aaru.DataTypeCdSectorSuffix, dataBlock(aaru.DataTypeCdSectorSuffix, 288, suffix))
	}
	// Tracks and geometry blocks are the same in both versions.
	for _, e := range srcRaw.index {
		var size uint64
		switch e.BlockType {
		case aaru.BlockTypeTracksBlock:
			var h aaru.TracksHeader
			_, _ = binary.Decode(srcRaw.data[e.Offset:], binary.LittleEndian, &h)
			size = uint64(binary.Size(h)) + uint64(h.Entries)*uint64(binary.Size(aaru.TrackEntry{}))
		case aaru.BlockTypeGeometryBlock:
			size = uint64(binary.Size(aaru.GeometryBlockHeader{}))
		default:
			continue
		}
		block := bytes.Clone(srcRaw.data[e.Offset : e.Offset+size])
		if e.BlockType == aaru.BlockTypeTracksBlock {
			// The CRC64 field (after the identifier and the entry count).
			binary.LittleEndian.PutUint64(block[6:], fxCRCv1(block[binary.Size(aaru.TracksHeader{}):]))
		}
		add(e.BlockType, e.DataType, block)
	}
	ddtRaw, _ := binary.Append(nil, binary.LittleEndian, ddt)
	dh, _ := binary.Append(nil, binary.LittleEndian, aaru.DDTHeader{Identifier: aaru.BlockTypeDeDuplicationTable,
		Type: aaru.DataTypeUserData, Compression: aaru.CompressionNone, Shift: shift, Entries: uint64(len(ddt)),
		CmpLength: uint64(len(ddtRaw)), Length: uint64(len(ddtRaw)), CmpCRC64: fxCRCv1(ddtRaw),
		CRC64: fxCRCv1(ddtRaw)})
	add(aaru.BlockTypeDeDuplicationTable, aaru.DataTypeUserData, append(dh, ddtRaw...))

	entries, _ := binary.Append(nil, binary.LittleEndian, index)
	ih, _ := binary.Append(nil, binary.LittleEndian, aaru.IndexHeader{Identifier: aaru.BlockTypeIndexBlock,
		Entries: uint16(len(index)), CRC64: fxCRCv1(entries)})
	indexOff := uint64(len(buf))
	buf = append(buf, ih...)
	buf = append(buf, entries...)

	h := aaru.Header{Identifier: aaru.AaruMagic, ImageMajorVersion: 1, ImageMinorVersion: 0,
		ApplicationMajorVersion: 1, MediaType: uint32(info.MediaType), IndexOffset: indexOff,
		CreationTime: stampCreationTime, LastWrittenTime: stampLastWrittenTime}
	copy(h.Application[:], aaru.EncodeUTF16("go-aaru-test"))
	hb, _ := binary.Append(nil, binary.LittleEndian, h)
	copy(buf, hb[:104])
	fxWrite(t, dst, buf)
}
