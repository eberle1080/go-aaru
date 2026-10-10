package aaru

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/media"
)

// Opening and reading version 1 images (built by v1Builder) and the same
// contents written by go-aaru as version 2 images.

type openImageCase struct {
	name       string
	mediaType  media.Type
	sectorSize uint32
	sectors    [][]byte // user data; nil = never dumped
	raw        [][]byte // CD only: the raw sectors
	mode2      bool
	notDumped  map[int]bool // CD only: prefix/suffix not dumped
	v1         []byte
	imageSize  uint64 // version 1 image size
}

func openImageCases() []openImageCase {
	var cases []openImageCase
	for i, c := range []struct {
		name       string
		mt         media.Type
		sectorSize uint32
		n          int
		comp       CompressionType
	}{
		{"floppy", media.DOS_35_HD, 512, 2880, CompressionLzma},
		{"floptical", media.Floptical, 512, 1500, CompressionNone},
		{"gigamo", media.GigaMo, 2048, 700, CompressionLzma},
		{"hifd", media.HiFD, 512, 1200, CompressionNone},
		{"mo", media.UnknownMO, 512, 2100, CompressionLzma},
		{"mo640", media.ECMA_239, 2048, 900, CompressionNone},
	} {
		sectors := seededSectors(uint64(100+i), c.n, int(c.sectorSize), 5)
		sectors[c.n/3] = nil // never dumped
		v1, size := v1BlockImage(c.mt, c.sectorSize, sectors, c.comp)
		cases = append(cases, openImageCase{name: c.name, mediaType: c.mt, sectorSize: c.sectorSize, sectors: sectors,
			v1: v1, imageSize: size})
	}
	for i, mode2 := range []bool{false, true} {
		const n = 400
		damaged := map[int]bool{7: true, 8: true, 101: true, 250: true}
		notDumped := map[int]bool{33: true}
		raw := rawCDSectors(uint64(200+i), n, mode2, damaged)
		v1, size := v1CDImage(raw, mode2, notDumped)
		user, _, _, _, _, _ := v1CDCorrections(raw, mode2, nil)
		name, mt := "cdmode1", media.CDROM
		if mode2 {
			name, mt = "cdmode2", media.CDROMXA
		}
		cases = append(cases, openImageCase{name: name, mediaType: mt, sectorSize: 2048, sectors: user, raw: raw,
			mode2: mode2, notDumped: notDumped, v1: v1, imageSize: size})
	}
	return cases
}

// writeV2 writes the contents of c with go-aaru.
func (c *openImageCase) writeV2(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), c.name+".aif")
	p := app()
	p.MediaType = c.mediaType
	p.SectorSize = c.sectorSize
	p.UserSectors = uint64(len(c.sectors))
	if c.raw != nil {
		p.SectorSize = 2352
	}
	img, err := Create(path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.raw != nil {
		tt := TrackTypeCdMode1
		if c.mode2 {
			tt = TrackTypeCdMode2Formless
		}
		if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: tt, Start: 0, End: int64(len(c.raw)) - 1,
			Session: 1, Flags: 4}}); err != nil {
			t.Fatal(err)
		}
		for i, s := range c.raw {
			if c.notDumped[i] {
				continue
			}
			if err := img.WriteSectorLong(uint64(i), false, s, SectorStatusDumped); err != nil {
				t.Fatalf("WriteSectorLong(%d): %v", i, err)
			}
		}
	} else {
		for i, s := range c.sectors {
			if s == nil {
				continue
			}
			if err := img.WriteSector(uint64(i), false, s, SectorStatusDumped); err != nil {
				t.Fatalf("WriteSector(%d): %v", i, err)
			}
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// check reads the whole image back.
func (c *openImageCase) check(t *testing.T, img *Image, v1 bool) {
	t.Helper()
	info := img.Info()
	optical := c.raw != nil
	if info.HasPartitions != optical || info.HasSessions != optical {
		t.Errorf("HasPartitions/HasSessions = %v/%v, want %v", info.HasPartitions, info.HasSessions, optical)
	}
	if info.Sectors != uint64(len(c.sectors)) {
		t.Errorf("Sectors = %d, want %d", info.Sectors, len(c.sectors))
	}
	if info.SectorSize != c.sectorSize {
		t.Errorf("SectorSize = %d, want %d", info.SectorSize, c.sectorSize)
	}
	wantVersion := "2.0"
	if v1 {
		wantVersion = "1.0"
	}
	if info.Version != wantVersion || info.Application != "go-aaru-test" || info.ApplicationVersion != "1.2" {
		t.Errorf("Version/Application/ApplicationVersion = %q/%q/%q", info.Version, info.Application,
			info.ApplicationVersion)
	}
	if info.MediaType != c.mediaType || info.MetadataMediaType != c.mediaType.MetadataType() {
		t.Errorf("MediaType/MetadataMediaType = %d/%d, want %d/%d", info.MediaType, info.MetadataMediaType,
			c.mediaType, c.mediaType.MetadataType())
	}
	if v1 {
		if TimeToFiletime(info.CreationTime) != v1CreationTime ||
			TimeToFiletime(info.LastModificationTime) != v1LastWrittenTime {
			t.Errorf("times = %d/%d", TimeToFiletime(info.CreationTime), TimeToFiletime(info.LastModificationTime))
		}
		if info.ImageSize != c.imageSize {
			t.Errorf("ImageSize = %d, want %d", info.ImageSize, c.imageSize)
		}
		if _, ok := img.UserDataDDTHeader(); ok {
			t.Error("version 1 image reports a DDT2 header")
		}
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}

	for i, want := range c.sectors {
		data, st, err := img.ReadSector(uint64(i), false)
		if want == nil {
			if !errors.Is(err, ErrSectorNotDumped) || st != SectorStatusNotDumped {
				t.Errorf("sector %d: status %v, %v; want not dumped", i, st, err)
			}
			continue
		}
		if c.notDumped[i] && !v1 {
			continue // not written at all to the version 2 image
		}
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("sector %d: %d bytes, %v; want the written data", i, len(data), err)
		}
		if v1 && st != SectorStatusDumped {
			t.Errorf("sector %d: status %v", i, st)
		}
		if !optical {
			continue
		}
		long, _, err := img.ReadSectorLong(uint64(i), false)
		if c.notDumped[i] {
			if !errors.Is(err, ErrSectorNotDumped) {
				t.Errorf("long sector %d with prefix/suffix not dumped: %v", i, err)
			}
			continue
		}
		if err != nil || !bytes.Equal(long, c.raw[i]) {
			t.Fatalf("long sector %d: %d bytes, %v; want the raw sector", i, len(long), err)
		}
		if len(long) != cdecc.SectorSize {
			t.Fatalf("long sector %d: %d bytes", i, len(long))
		}
	}
	if _, _, err := img.ReadSector(uint64(len(c.sectors)), false); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("sector past the end: %v", err)
	}
	if optical {
		tracks, err := img.Tracks()
		if err != nil || len(tracks) != 1 || tracks[0].End != int64(len(c.raw))-1 {
			t.Errorf("Tracks = %+v, %v", tracks, err)
		}
		if v1 {
			toc, err := img.ReadMediaTag(MediaTagCdToc)
			if err != nil || string(toc) != "version 1 table of contents" {
				t.Errorf("CD TOC = %q, %v", toc, err)
			}
		}
	} else if _, _, err := img.ReadSectorLong(0, false); !errors.Is(err, ErrIncorrectMediaType) {
		t.Errorf("ReadSectorLong on %v = %v, want ErrIncorrectMediaType", c.mediaType, err)
	}
}

func TestOpenImage(t *testing.T) {
	for _, c := range openImageCases() {
		t.Run(c.name+"_v1", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.name+"_v1.aif")
			if err := os.WriteFile(path, c.v1, 0o644); err != nil {
				t.Fatal(err)
			}
			img, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			c.check(t, img, true)
			if err := img.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
		t.Run(c.name, func(t *testing.T) {
			img, err := Open(c.writeV2(t))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			c.check(t, img, false)
			if err := img.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
	}
}

// TestOpenImageV1Quirks covers version 1 specifics: version 1 images cannot
// be resumed, the byte-swapped CRCs are checked by Verify, and the
// application name is UTF-16LE.
func TestOpenImageV1Quirks(t *testing.T) {
	sectors := seededSectors(9, 64, 512, 0)
	data, _ := v1BlockImage(media.DOS_35_HD, 512, sectors, CompressionLzma)
	path := filepath.Join(t.TempDir(), "v1.aif")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(path, nil); !errors.Is(err, ErrIncompatibleVersion) {
		t.Errorf("Resume of a version 1 image = %v, want ErrIncompatibleVersion", err)
	}

	// A CRC stored unswapped fails verification.
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ddtOff := uint64(0)
	for _, e := range img.indexEntries {
		if e.BlockType == BlockTypeDeDuplicationTable {
			ddtOff = e.Offset
		}
	}
	img.Close()
	h, _ := unmarshal[DDTHeader](data[ddtOff:])
	h.CmpCRC64 = bswap64(h.CmpCRC64)
	bad := append([]byte(nil), data...)
	copy(bad[ddtOff:], marshal(h))
	img, err = OpenReader(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatal(err)
	}
	if err := img.Verify(); !errors.Is(err, ErrInvalidBlockCRC) {
		t.Errorf("Verify with an unswapped DDT CRC = %v, want ErrInvalidBlockCRC", err)
	}
	if got, _, err := img.ReadSector(5, false); err != nil || !bytes.Equal(got, sectors[5]) {
		t.Errorf("ReadSector(5) = %v", err)
	}
	img.Close()
}

func TestOpenNonExistentFile(t *testing.T) {
	img, err := Open("/nonexistent/path/to/file.aif")
	if img != nil || err == nil {
		t.Fatalf("Open of a non-existent file = %v, %v; want nil, error", img, err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v does not wrap fs.ErrNotExist", err)
	}
}

func TestOpenNullPath(t *testing.T) {
	img, err := Open("")
	if img != nil || err == nil {
		t.Fatalf("Open(\"\") = %v, %v; want nil, error", img, err)
	}
}
