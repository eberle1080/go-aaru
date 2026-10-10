package aaru

// Image creation tests. They write 128 MiB block images and CD audio images,
// then read them back with Open.

import (
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

const createTestSectors = 128 * 1024 * 1024 / 512

// createTestRandom is the rolling buffer the block images are written from:
// 1 MiB of seeded pseudo-random data.
func createTestRandom() []byte { return seededRandom(0xc4ea7e, 1<<20) }

// createTestAudio returns 896 sectors (about 2 MiB) of generated CD audio:
// a smooth stereo signal with stretches of silence and noise.
func createTestAudio() []byte {
	r := rand.New(rand.NewPCG(0xa0d10, 1))
	var b []byte
	for s := 0; s < 896; s++ {
		switch s % 50 {
		case 10:
			b = append(b, make([]byte, 2352)...)
		case 20:
			b = append(b, randBytes(r, 2352)...)
		default:
			b = append(b, compressibleAudio(r, 2352)...)
		}
	}
	return b
}

func testParams(mt media.Type, sectorSize uint32, sectors, negative uint64) CreateParams {
	return CreateParams{MediaType: mt, SectorSize: sectorSize, UserSectors: sectors, NegativeSectors: negative,
		Application: "go-aaru-test"}
}

func optsPtr(s string) *Options {
	o := ParseOptions(s)
	return &o
}

// checkBlockInfo checks the image info of a created block image.
func checkBlockInfo(t *testing.T, img *Image, sectors uint64, full bool) {
	t.Helper()
	info := img.Info()
	if info.Sectors != sectors || info.SectorSize != 512 {
		t.Fatalf("sectors=%d sectorSize=%d", info.Sectors, info.SectorSize)
	}
	if !full {
		return
	}
	if info.HasPartitions || info.HasSessions {
		t.Errorf("HasPartitions=%v HasSessions=%v", info.HasPartitions, info.HasSessions)
	}
	if info.Version != "2.0" || info.Application != "go-aaru-test" || info.ApplicationVersion != "0.0" {
		t.Errorf("version=%q application=%q applicationVersion=%q", info.Version, info.Application,
			info.ApplicationVersion)
	}
	if info.MediaType != 1 || info.MetadataMediaType != media.BlockMedia {
		t.Errorf("mediaType=%d metadataMediaType=%d", info.MediaType, info.MetadataMediaType)
	}
}

// writeRandomSectors writes sectors [from, to) from the rolling 1 MiB random
// buffer, feeding them to crc.
func writeRandomSectors(t *testing.T, img *Image, buf []byte, from, to uint64, negative bool, crc *uint64) {
	t.Helper()
	for s := from; s < to; s++ {
		off := s * 512 % uint64(len(buf))
		if err := img.WriteSector(s, negative, buf[off:off+512], SectorStatusDumped); err != nil {
			t.Fatalf("writing sector %d (negative=%v): %v", s, negative, err)
		}
		*crc = crc64.Update(*crc, buf[off:off+512])
	}
}

// readSectorsCRC reads sectors [from, to) and feeds them to crc.
func readSectorsCRC(t *testing.T, img *Image, from, to uint64, negative bool, crc *uint64) {
	t.Helper()
	for s := from; s < to; s++ {
		data, _, err := img.ReadSector(s, negative)
		if err != nil {
			t.Fatalf("reading sector %d (negative=%v): %v", s, negative, err)
		}
		if len(data) != 512 {
			t.Fatalf("sector %d: length %d", s, len(data))
		}
		*crc = crc64.Update(*crc, data)
	}
}

func TestCreateImageBlock(t *testing.T) {
	cases := []struct {
		name     string
		opts     string
		negative bool
		fullInfo bool
	}{
		{"create_image_uncompresed_duplicated", "deduplicate=false;compress=false", false, true},
		{"create_image_uncompresed_deduplicated", "deduplicate=true;compress=false", false, true},
		{"create_image_compresed_duplicated", "deduplicate=false;compress=true", false, true},
		{"create_image_compresed_deduplicated", "deduplicate=true;compress=true", false, true},
		{"create_image_table_shift_9", "table_shift=9", false, true},
		{"create_image_negative_sectors", "deduplicate=true;compress=true", true, true},
		{"create_image_zstd_compresed_deduplicated", "deduplicate=true;compress=true;zstd=true;zstd_level=19", false,
			false},
		{"create_image_zstd_negative_sectors", "deduplicate=true;compress=true;zstd=true;zstd_level=19", true, false},
	}
	buf := createTestRandom()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if testing.Short() && c.name == "create_image_compresed_duplicated" {
				t.Skip("128 MiB of LZMA in -short mode")
			}
			path := filepath.Join(t.TempDir(), "test.aif")
			var negative uint64
			if c.negative {
				negative = 300
			}
			img, err := Create(path, testParams(1, 512, createTestSectors, negative), optsPtr(c.opts))
			if err != nil {
				t.Fatal(err)
			}
			crc := crc64.Seed
			if c.negative {
				for s := uint64(1); s <= 150; s++ {
					writeRandomSectors(t, img, buf, s, s+1, true, &crc)
				}
			}
			writeRandomSectors(t, img, buf, 0, createTestSectors, false, &crc)
			if err := img.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			img, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			checkBlockInfo(t, img, createTestSectors, c.fullInfo)
			got := crc64.Seed
			if c.negative {
				readSectorsCRC(t, img, 1, 151, true, &got)
			}
			readSectorsCRC(t, img, 0, createTestSectors, false, &got)
			if got != crc {
				t.Fatalf("CRC64 %x, want %x", got^crc64.Seed, crc^crc64.Seed)
			}
		})
	}
}

func TestCreateImageResume(t *testing.T) {
	buf := createTestRandom()
	path := filepath.Join(t.TempDir(), "test.aif")
	img, err := Create(path, testParams(1, 512, createTestSectors, 0), optsPtr("deduplicate=true;compress=true"))
	if err != nil {
		t.Fatal(err)
	}
	crc := crc64.Seed
	writeRandomSectors(t, img, buf, 0, createTestSectors/2, false, &crc)
	if err := img.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	img, err = Resume(path, optsPtr("deduplicate=true;compress=true"))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	writeRandomSectors(t, img, buf, createTestSectors/2, createTestSectors, false, &crc)
	if err := img.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	checkBlockInfo(t, img, createTestSectors, true)
	got := crc64.Seed
	readSectorsCRC(t, img, 0, createTestSectors, false, &got)
	if got != crc {
		t.Fatalf("CRC64 %x, want %x", got^crc64.Seed, crc^crc64.Seed)
	}
}

func TestCreateImageZstdFlagUncompressedNoFeatureBit(t *testing.T) {
	buf := createTestRandom()
	path := filepath.Join(t.TempDir(), "test_zstd_nocompress.aif")
	img, err := Create(path, testParams(1, 512, createTestSectors, 0), optsPtr("deduplicate=true;compress=false;zstd=true"))
	if err != nil {
		t.Fatal(err)
	}
	crc := crc64.Seed
	writeRandomSectors(t, img, buf, 0, createTestSectors, false, &crc)
	if err := img.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	img, err = Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer img.Close()
	if img.Header().FeatureIncompatible&FeatureIncompatZstd != 0 {
		t.Fatal("zstd feature bit set without zstd blocks")
	}
	checkBlockInfo(t, img, createTestSectors, false)
}

// TestCreateImageAudio writes three copies of the generated audio as an audio track,
// optionally with subchannel tags.
func TestCreateImageAudio(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		opts       *Options
		subchannel bool
		fullInfo   bool
	}{
		{"create_audio_image", "test_audio.aif", nil, false, true},
		{"create_subchannel_uncompressed_image", "test_audio.aif", optsPtr("compress=false"), true, true},
		{"create_subchannel_compressed_image", "test_audio.aif", optsPtr("compress=true"), true, true},
		{"create_subchannel_zstd_compressed_image", "test_audio_zstd.aif",
			optsPtr("compress=true;zstd=true;zstd_level=19"), true, false},
	}
	audio := createTestAudio()
	total := uint64(len(audio) * 3 / 2352)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.file)
			img, err := Create(path, testParams(media.CDDA, 2352, total, 0), c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeAudio, Start: 0,
				End: int64(len(audio)*3/2352) - 1, Session: 1}}); err != nil {
				t.Fatalf("SetTracks: %v", err)
			}
			crc := crc64.Seed
			for s := uint64(0); s < total; s++ {
				off := s * 2352 % uint64(len(audio))
				if err := img.WriteSectorLong(s, false, audio[off:off+2352], SectorStatusDumped); err != nil {
					t.Fatalf("writing sector %d: %v", s, err)
				}
				crc = crc64.Update(crc, audio[off:off+2352])
			}
			subCRC := crc64.Seed
			if c.subchannel {
				sub := make([]byte, 96)
				for s := uint64(0); s < total; s++ {
					for i := range sub {
						sub[i] = byte(s*96 + uint64(i))
					}
					if err := img.WriteSectorTag(s, false, SectorTagCdSubchannel, sub); err != nil {
						t.Fatalf("writing subchannel %d: %v", s, err)
					}
					subCRC = crc64.Update(subCRC, sub)
				}
			}
			if err := img.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			img, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			info := img.Info()
			if info.Sectors != total || info.SectorSize != 2352 {
				t.Fatalf("sectors=%d sectorSize=%d", info.Sectors, info.SectorSize)
			}
			if c.fullInfo {
				if !info.HasPartitions || !info.HasSessions {
					t.Errorf("HasPartitions=%v HasSessions=%v", info.HasPartitions, info.HasSessions)
				}
				if info.Version != "2.0" || info.Application != "go-aaru-test" || info.ApplicationVersion != "0.0" {
					t.Errorf("version=%q application=%q applicationVersion=%q", info.Version, info.Application,
						info.ApplicationVersion)
				}
				if info.MediaType != media.CDDA || info.MetadataMediaType != media.OpticalDisc {
					t.Errorf("mediaType=%d metadataMediaType=%d", info.MediaType, info.MetadataMediaType)
				}
			}
			got := crc64.Seed
			for s := uint64(0); s < total; s++ {
				data, _, err := img.ReadSectorLong(s, false)
				if err != nil {
					t.Fatalf("reading sector %d: %v", s, err)
				}
				if len(data) != 2352 {
					t.Fatalf("sector %d: length %d", s, len(data))
				}
				got = crc64.Update(got, data)
			}
			if got != crc {
				t.Fatalf("CRC64 %x, want %x", got^crc64.Seed, crc^crc64.Seed)
			}
			if !c.subchannel {
				return
			}
			gotSub := crc64.Seed
			for s := uint64(0); s < total; s++ {
				sub, err := img.ReadSectorTag(s, false, SectorTagCdSubchannel)
				if err != nil {
					t.Fatalf("reading subchannel %d: %v", s, err)
				}
				if len(sub) != 96 {
					t.Fatalf("subchannel %d: length %d", s, len(sub))
				}
				gotSub = crc64.Update(gotSub, sub)
			}
			if gotSub != subCRC {
				t.Fatalf("subchannel CRC64 %x, want %x", gotSub^crc64.Seed, subCRC^crc64.Seed)
			}
		})
	}
}
