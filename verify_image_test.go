package aaru

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// rdVerifyFixtures writes the images verified below: version 1 images (LZMA
// properties in the CRC, CRCs byte-swapped), and version 2 images written by
// go-aaru (properties excluded, no swap).
func rdVerifyFixtures(t *testing.T) map[string]string {
	t.Helper()
	v1, _ := v1FloppyImage()
	floptical, _ := v1BlockImage(media.Floptical, 512, seededSectors(79, 1000, 512, 3), CompressionNone)
	return map[string]string{
		"floptical_v1": writeTempFile(t, "floptical_v1.aif", floptical),
		"floppy_v1":    writeTempFile(t, "floppy_v1.aif", v1),
		"cdmode1_v1":   writeTempFile(t, "cdmode1_v1.aif", v1CDMode1Image()),
		"floptical":    blockTestImage(t, media.Floptical, 1000),
		"cdmode1":      cdTestImage(t),
	}
}

// cdTestImage writes a CD Mode 1 image (LZMA compressed) with go-aaru.
func cdTestImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cdmode1.aif")
	p := app()
	p.MediaType = media.CDROM
	p.SectorSize = 2352
	p.UserSectors = 300
	img, err := Create(path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 299, Session: 1,
		Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	for i, s := range rawCDSectors(80, 300, false, map[int]bool{4: true, 9: true}) {
		if err := img.WriteSectorLong(uint64(i), false, s, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// rdCorruptCopy copies an image to a temporary file and overwrites bytes at
// offset with garbage.
func rdCorruptCopy(t *testing.T, path string, offset uint64, garbage []byte) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copy(data[offset:], garbage)
	return writeTempFile(t, "corrupt.aif", data)
}

func TestVerifyImage(t *testing.T) {
	// A. Happy path.
	fixtures := rdVerifyFixtures(t)
	for _, name := range []string{"floptical_v1", "floppy_v1", "cdmode1_v1", "floptical", "cdmode1"} {
		t.Run(name, func(t *testing.T) {
			img, err := Open(fixtures[name])
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if err := img.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
			if err := img.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
	}
}

// B. Invalid context: a nil *Image and an Image that was not opened.
func TestVerifyNullContext(t *testing.T) {
	var img *Image
	if err := img.Verify(); !errors.Is(err, ErrNotAaruFormat) {
		t.Errorf("Verify on nil image = %v, want ErrNotAaruFormat", err)
	}
}

func TestVerifyGarbageContext(t *testing.T) {
	img := &Image{}
	if err := img.Verify(); !errors.Is(err, ErrNotAaruFormat) {
		t.Errorf("Verify on unopened image = %v, want ErrNotAaruFormat", err)
	}
}

// C. Data corruption detection.
func TestVerifyCorruption(t *testing.T) {
	fixtures := rdVerifyFixtures(t)
	cases := []struct {
		name    string
		file    string
		bt      BlockType
		dt      DataType
		skip    uint64 // bytes after the block header
		garbage []byte
	}{
		// Version 1 LZMA DDT: the CRC covers the full cmpLength, including
		// the LZMA properties.
		{"verify_corrupt_lzma_properties_v1", "floppy_v1", BlockTypeDeDuplicationTable, DataTypeUserData, 1,
			[]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"verify_corrupt_data_v1", "floptical_v1", BlockTypeDataBlock, DataTypeUserData, 5, []byte{0xDE, 0xAD}},
		// Version 2 data block: past the 5 bytes of LZMA properties.
		{"verify_corrupt_data_v2", "cdmode1", BlockTypeDataBlock, DataTypeUserData, LZMAPropertiesLength + 2,
			[]byte{0xDE, 0xAD}},
		{"verify_corrupt_tracks_block_v1", "cdmode1_v1", BlockTypeTracksBlock, DataTypeNone, 4,
			[]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"verify_corrupt_tracks_block_v2", "cdmode1", BlockTypeTracksBlock, DataTypeNone, 4,
			[]byte{0xDE, 0xAD, 0xBE, 0xEF}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := testIndexEntry(t, fixtures[tc.file], tc.bt, tc.dt)
			hdr := uint64(blockHeaderSize)
			switch tc.bt {
			case BlockTypeDeDuplicationTable:
				hdr = ddtHeaderSize
			case BlockTypeTracksBlock:
				hdr = tracksHeaderSize
			}
			path := rdCorruptCopy(t, fixtures[tc.file], e.Offset+hdr+tc.skip, tc.garbage)
			img, err := Open(path)
			if err != nil {
				t.Fatalf("Open should succeed on corrupted data (lenient open): %v", err)
			}
			defer img.Close()
			if err := img.Verify(); !errors.Is(err, ErrInvalidBlockCRC) {
				t.Errorf("Verify = %v, want ErrInvalidBlockCRC", err)
			}
		})
	}
}

// D. Index corruption (in memory).
func TestVerifyInvalidIndexOffset(t *testing.T) {
	img, err := Open(rdVerifyFixtures(t)["floptical_v1"])
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	img.header.IndexOffset = 0xFFFFFFFF00 // beyond the file
	if err := img.Verify(); !errors.Is(err, ErrCannotReadHeader) {
		t.Errorf("Verify = %v, want ErrCannotReadHeader", err)
	}
}

func TestVerifyBadIndexSignature(t *testing.T) {
	img, err := Open(rdVerifyFixtures(t)["floptical_v1"])
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	img.header.IndexOffset = 200 // middle of a data block payload
	if err := img.Verify(); !errors.Is(err, ErrCannotReadIndex) {
		t.Errorf("Verify = %v, want ErrCannotReadIndex", err)
	}
}

// E. Programmatic round trip: a freshly written
// compressed, deduplicated image verifies.
func TestVerifyCreatedImage(t *testing.T) {
	buf := seededRandom(2, 1<<20)
	const totalSectors, sectorSize = 2048, 512
	path := filepath.Join(t.TempDir(), "verify_roundtrip.aif")
	opts := ParseOptions("deduplicate=true;compress=true")
	img, err := Create(path, CreateParams{MediaType: 1, SectorSize: sectorSize, UserSectors: totalSectors,
		Application: "go-aaru-test"}, &opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for sector := 0; sector < totalSectors; sector++ {
		off := sector * sectorSize % len(buf)
		if err := img.WriteSector(uint64(sector), false, buf[off:off+sectorSize], SectorStatusDumped); err != nil {
			t.Fatalf("WriteSector %d: %v", sector, err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}
