package aaru

// Tests of raw CD sector storage (prefix/suffix classification, Mode 2
// forms, errored suffixes) and its persistence across resumed sessions.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/media"
)

const rawCDSector = 2352

func toBCD(v int) byte { return byte(v/10)<<4 | byte(v%10) }

// writeCDPrefix writes a correct sync and BCD header for lba with mode.
func writeCDPrefix(sector []byte, lba int64, mode byte) {
	copy(sector, cdSync[:])
	abs := lba + 150
	sector[12] = toBCD(int(abs / (60 * 75)))
	sector[13] = toBCD(int(abs / 75 % 60))
	sector[14] = toBCD(int(abs % 75))
	sector[15] = mode
}

func buildCorrectMode1Sector(lba int64, fill byte) []byte {
	s := make([]byte, rawCDSector)
	writeCDPrefix(s, lba, 0x01)
	for i := 16; i < 16+2048; i++ {
		s[i] = fill
	}
	if err := cdecc.Reconstruct(s, cdecc.TrackTypeCdMode1); err != nil {
		panic(err)
	}
	return s
}

// buildJunkMode1Sector has junk sync/header and ECC/EDC, so every part must
// be stored.
func buildJunkMode1Sector(fill byte) []byte {
	s := make([]byte, rawCDSector)
	for i := 0; i < 16; i++ {
		s[i] = fill + byte(i)
	}
	for i := 16; i < 16+2048; i++ {
		s[i] = fill
	}
	for i := 2064; i < rawCDSector; i++ {
		s[i] = fill ^ byte(i)
	}
	return s
}

func createCDImage(t *testing.T, path string, mt media.Type, sectors uint64, tt TrackType) *Image {
	t.Helper()
	img, err := Create(path, CreateParams{MediaType: mt, SectorSize: 2048, UserSectors: sectors, Application: "go-aaru-test"},
		optsPtr("deduplicate=false;compress=false"))
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: tt, Start: 0, End: int64(sectors) - 1, Session: 1,
		Flags: 0x04}}); err != nil {
		t.Fatalf("SetTracks: %v", err)
	}
	return img
}

func writeLong(t *testing.T, img *Image, lba uint64, data []byte) {
	t.Helper()
	if err := img.WriteSectorLong(lba, false, data, SectorStatusDumped); err != nil {
		t.Fatalf("writing long sector %d: %v", lba, err)
	}
}

func readLong(t *testing.T, img *Image, lba uint64) ([]byte, SectorStatus) {
	t.Helper()
	data, st, err := img.ReadSectorLong(lba, false)
	if err != nil {
		t.Fatalf("reading long sector %d: %v", lba, err)
	}
	if len(data) != rawCDSector {
		t.Fatalf("long sector %d: length %d", lba, len(data))
	}
	return data, st
}

func expectRoundTrip(t *testing.T, img *Image, lba uint64, want []byte) {
	t.Helper()
	got, _ := readLong(t, img, lba)
	if !bytes.Equal(got, want) {
		t.Errorf("sector %d does not round-trip", lba)
	}
}

// CustomPrefixSuffixSurviveResume: junk sectors written in two sessions must
// all read back byte-exact.
func TestCdPrefixResumeCustomPrefixSuffixSurviveResume(t *testing.T) {
	const sectors, first = 12, 5
	path := filepath.Join(t.TempDir(), "test_cd_prefix_resume.aif")
	expected := make([][]byte, sectors)
	for i := range expected {
		expected[i] = buildJunkMode1Sector(byte(0x10 + i))
	}

	img := createCDImage(t, path, media.CDROM, sectors, TrackTypeCdMode1)
	for i := 0; i < first; i++ {
		writeLong(t, img, uint64(i), expected[i])
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err := Resume(path, optsPtr("compress=false"))
	if err != nil {
		t.Fatal(err)
	}
	for i := first; i < sectors; i++ {
		writeLong(t, img, uint64(i), expected[i])
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		expectRoundTrip(t, img, uint64(i), expected[i])
	}
}

// ResumeWithoutNewCustomsKeepsArena: a resumed session that only adds
// regenerable sectors must leave the stored arena intact.
func TestCdPrefixResumeWithoutNewCustomsKeepsArena(t *testing.T) {
	const sectors = 6
	path := filepath.Join(t.TempDir(), "test_cd_prefix_resume_noop.aif")
	expected := make([][]byte, sectors)
	for i := range expected {
		if i < 3 {
			expected[i] = buildJunkMode1Sector(byte(0x40 + i))
		} else {
			expected[i] = buildCorrectMode1Sector(int64(i), byte(0x40+i))
		}
	}

	img := createCDImage(t, path, media.CDROM, sectors, TrackTypeCdMode1)
	for i := 0; i < 3; i++ {
		writeLong(t, img, uint64(i), expected[i])
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err := Resume(path, optsPtr("compress=false"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 3; i < sectors; i++ {
		writeLong(t, img, uint64(i), expected[i])
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		expectRoundTrip(t, img, uint64(i), expected[i])
	}
}

// NonBcdHeaderIsStoredNotRegenerated: a header that is not valid BCD but
// decodes to the right address is stored verbatim.
func TestCdPrefixNonBcdHeaderIsStoredNotRegenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_cd_prefix_nonbcd.aif")
	correct := buildCorrectMode1Sector(0, 0x11)
	aliased := buildCorrectMode1Sector(600, 0x22)
	if aliased[13] != 0x10 {
		t.Fatalf("header byte 13 = %#x", aliased[13])
	}
	aliased[13] = 0x0A // non-BCD, decodes to 10 as well; ECC/EDC now stale

	img := createCDImage(t, path, media.CDROM, 601, TrackTypeCdMode1)
	writeLong(t, img, 0, correct)
	writeLong(t, img, 600, aliased)
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	expectRoundTrip(t, img, 0, correct)
	expectRoundTrip(t, img, 600, aliased)
	if _, st := readLong(t, img, 0); st != SectorStatusMode1Correct {
		t.Errorf("sector 0 status %v, want Mode1Correct", st)
	}
	if _, st := readLong(t, img, 600); st != SectorStatusErrored {
		t.Errorf("sector 600 status %v, want Errored", st)
	}
}

// buildMode2Form2Sector builds a Mode 2 Form 2 sector with the given EDC.
func buildMode2Form2Sector(lba int64, fill byte, edc uint32) []byte {
	s := make([]byte, rawCDSector)
	writeCDPrefix(s, lba, 0x02)
	s[18] = 0x20
	s[22] = 0x20
	for i := 24; i < 24+2324; i++ {
		s[i] = fill
	}
	binary.LittleEndian.PutUint32(s[2348:], edc)
	return s
}

func form2EDC(s []byte) uint32 { return cdecc.ComputeEDC(0, s[0x10:0x10+0x91C]) }

func checkFill(t *testing.T, data []byte, from, n int, fill byte, lba uint64) {
	t.Helper()
	for j := from; j < from+n; j++ {
		if data[j] != fill {
			t.Errorf("sector %d: byte %d = %#x, want %#x", lba, j, data[j], fill)
			return
		}
	}
}

const mediaCDROMXA = media.CDROMXA

// NoCrcEdcBytesAreZero: Form 2 sectors written with a zero EDC read back
// with zero EDC bytes.
func TestMode2NoCrcEdcBytesAreZero(t *testing.T) {
	const sectors = 4
	path := filepath.Join(t.TempDir(), "test_mode2_nocrc.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		writeLong(t, img, uint64(i), buildMode2Form2Sector(int64(i), byte(0x41+i), 0))
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		if !bytes.Equal(data[2348:2352], []byte{0, 0, 0, 0}) {
			t.Errorf("sector %d: EDC bytes %x, want zero", i, data[2348:2352])
		}
		checkFill(t, data, 24, 2324, byte(0x41+i), uint64(i))
		if data[18]&0x20 == 0 && data[22]&0x20 == 0 {
			t.Errorf("sector %d: Form 2 subheader bit lost", i)
		}
	}
}

// ValidEdcSurvivesRoundtrip: Form 2 sectors with a valid EDC keep it.
func TestMode2ValidEdcSurvivesRoundtrip(t *testing.T) {
	const sectors = 2
	path := filepath.Join(t.TempDir(), "test_mode2_valid_edc.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		s := buildMode2Form2Sector(int64(i), byte(0x55+i), 0)
		binary.LittleEndian.PutUint32(s[2348:], form2EDC(s))
		writeLong(t, img, uint64(i), s)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		edc := binary.LittleEndian.Uint32(data[2348:])
		if edc == 0 {
			t.Errorf("sector %d: EDC is zero", i)
		}
		if edc != form2EDC(data) {
			t.Errorf("sector %d: EDC %#x, want %#x", i, edc, form2EDC(data))
		}
		checkFill(t, data, 24, 2324, byte(0x55+i), uint64(i))
	}
}

// MixedNoCrcAndValidEdcPerSector: NoCrc and valid-EDC sectors coexist.
func TestMode2MixedNoCrcAndValidEdcPerSector(t *testing.T) {
	const sectors = 8
	path := filepath.Join(t.TempDir(), "test_mode2_mixed.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		s := buildMode2Form2Sector(int64(i), byte(0x30+i), 0)
		if i%2 != 0 {
			binary.LittleEndian.PutUint32(s[2348:], form2EDC(s))
		}
		writeLong(t, img, uint64(i), s)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		edc := binary.LittleEndian.Uint32(data[2348:])
		if i%2 == 0 {
			if edc != 0 {
				t.Errorf("even sector %d: EDC %#x, want 0", i, edc)
			}
		} else if edc == 0 || edc != form2EDC(data) {
			t.Errorf("odd sector %d: EDC %#x, want %#x", i, edc, form2EDC(data))
		}
		checkFill(t, data, 24, 2324, byte(0x30+i), uint64(i))
	}
}

func buildMode2Form1ErroredSector(lba int64, dataFill, suffixFill byte) []byte {
	s := make([]byte, rawCDSector)
	writeCDPrefix(s, lba, 0x02)
	for i := 24; i < 24+2048; i++ {
		s[i] = dataFill
	}
	for i := 2072; i < rawCDSector; i++ {
		s[i] = suffixFill
	}
	return s
}

func buildMode2DisagreeingSector(lba int64, fill byte, wrongEDC uint32) []byte {
	s := buildMode2Form2Sector(lba, fill, wrongEDC)
	s[18] = 0x00
	s[22] = 0x20
	return s
}

// ErroredForm2SuffixRestored: a wrong Form 2 EDC is stored and restored at
// offset 2348.
func TestMode2ErroredForm2SuffixRestored(t *testing.T) {
	const sectors = 4
	const wrongEDC = 0xDEADBEEF
	path := filepath.Join(t.TempDir(), "test_mode2_errored_form2.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		writeLong(t, img, uint64(i), buildMode2Form2Sector(int64(i), byte(0xA0+i), wrongEDC))
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		if edc := binary.LittleEndian.Uint32(data[2348:]); edc != wrongEDC {
			t.Errorf("sector %d: EDC %#x, want %#x", i, edc, uint32(wrongEDC))
		}
		checkFill(t, data, 24, 2324, byte(0xA0+i), uint64(i))
		if data[18]&0x20 == 0 || data[22]&0x20 == 0 {
			t.Errorf("sector %d: Form 2 subheader bits lost", i)
		}
	}
}

// ErroredForm1SuffixRestored: a bad Form 1 suffix is restored at offset 2072.
func TestMode2ErroredForm1SuffixRestored(t *testing.T) {
	const sectors = 4
	path := filepath.Join(t.TempDir(), "test_mode2_errored_form1.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		writeLong(t, img, uint64(i), buildMode2Form1ErroredSector(int64(i), byte(0xB0+i), byte(0xC0+i)))
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		checkFill(t, data, 24, 2048, byte(0xB0+i), uint64(i))
		checkFill(t, data, 2072, 280, byte(0xC0+i), uint64(i))
		if data[18]&0x20 != 0 || data[22]&0x20 != 0 {
			t.Errorf("sector %d: subheader should say Form 1", i)
		}
	}
}

// MixedErroredForm1AndForm2: even sectors errored Form 2, odd errored Form 1.
func TestMode2MixedErroredForm1AndForm2(t *testing.T) {
	const sectors = 8
	const wrongEDC = 0xCAFEBABE
	path := filepath.Join(t.TempDir(), "test_mode2_errored_mixed.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		var s []byte
		if i%2 == 0 {
			s = buildMode2Form2Sector(int64(i), byte(0x50+i), wrongEDC)
		} else {
			s = buildMode2Form1ErroredSector(int64(i), byte(0x50+i), byte(0x60+i))
		}
		writeLong(t, img, uint64(i), s)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		if i%2 == 0 {
			checkFill(t, data, 24, 2324, byte(0x50+i), uint64(i))
			if edc := binary.LittleEndian.Uint32(data[2348:]); edc != wrongEDC {
				t.Errorf("sector %d: EDC %#x", i, edc)
			}
		} else {
			checkFill(t, data, 24, 2048, byte(0x50+i), uint64(i))
			checkFill(t, data, 2072, 280, byte(0x60+i), uint64(i))
		}
	}
}

// DisagreeingSubheaderUsesForm2: a second subheader copy saying Form 2 wins.
func TestMode2DisagreeingSubheaderUsesForm2(t *testing.T) {
	const sectors = 4
	const wrongEDC = 0xBAADF00D
	path := filepath.Join(t.TempDir(), "test_mode2_disagreeing.aif")
	img := createCDImage(t, path, mediaCDROMXA, sectors, TrackTypeCdMode2Formless)
	for i := 0; i < sectors; i++ {
		writeLong(t, img, uint64(i), buildMode2DisagreeingSector(int64(i), byte(0xD0+i), wrongEDC))
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := 0; i < sectors; i++ {
		data, _ := readLong(t, img, uint64(i))
		if data[18]&0x20 != 0 || data[22]&0x20 == 0 {
			t.Errorf("sector %d: subheader copies changed", i)
		}
		checkFill(t, data, 24, 2324, byte(0xD0+i), uint64(i))
		if edc := binary.LittleEndian.Uint32(data[2348:]); edc != wrongEDC {
			t.Errorf("sector %d: EDC %#x", i, edc)
		}
	}
}

// Track flags and ISRC written as sector tags reach the tracks block, which
// still verifies.
func TestTrackTagsVerify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tracktags.aif")
	img, err := Create(path, CreateParams{MediaType: media.CDDA, SectorSize: 2352, UserSectors: 20,
		Application: "go-aaru-test"}, optsPtr("compress=false"))
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeAudio, Start: 0, End: 9, Session: 1},
		{Sequence: 2, Type: TrackTypeAudio, Start: 10, End: 19, Session: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := img.WriteSectorTag(12, false, SectorTagCdTrackFlags, []byte{0x08}); err != nil {
		t.Fatal(err)
	}
	if err := img.WriteSectorTag(3, false, SectorTagCdTrackIsrc, []byte("USABC1234567")); err != nil {
		t.Fatal(err)
	}
	if err := img.WriteSectorTag(30, false, SectorTagCdTrackFlags, []byte{1}); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("out of bounds track flags: %v", err)
	}
	for i := uint64(0); i < 20; i++ {
		if err := img.WriteSectorLong(i, false, bytes.Repeat([]byte{byte(i)}, rawCDSector), SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	tracks, err := img.Tracks()
	if err != nil || len(tracks) != 2 {
		t.Fatalf("Tracks: %v, %v", tracks, err)
	}
	if tracks[1].Flags != 0x08 || string(tracks[0].ISRC[:12]) != "USABC1234567" {
		t.Errorf("tracks %+v", tracks)
	}
	if isrc, err := img.ReadSectorTag(3, false, SectorTagCdTrackIsrc); err != nil || string(isrc) != "USABC1234567" {
		t.Errorf("ISRC tag %q, %v", isrc, err)
	}
}
