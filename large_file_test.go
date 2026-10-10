package aaru

// Large file I/O: writing and reading back past 4 GiB through the image's
// backing file (a sparse file), and an image whose blocks lie beyond 4 GiB.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

func TestLargeFileIOPositionalPastFourGiB(t *testing.T) {
	if testing.Short() {
		t.Skip("creates a sparse file larger than 4 GiB")
	}
	img, err := Create(filepath.Join(t.TempDir(), "large.aif"), CreateParams{MediaType: media.Type(1),
		SectorSize: 512, UserSectors: 8, Application: "go-aaru-test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	const target = 3<<30 + 17 + 1<<32
	if err := img.writeAt([]byte{0x5A}, target); err != nil {
		t.Fatalf("writing at %d: %v", uint64(target), err)
	}
	if img.size != target+1 {
		t.Fatalf("size %d, want %d", img.size, uint64(target+1))
	}
	var b [1]byte
	if err := img.readFullAt(b[:], target); err != nil || b[0] != 0x5A {
		t.Fatalf("read back %#x, %v", b[0], err)
	}
}

// largeImageScript writes an image whose data blocks start beyond 4 GiB.
func largeImage(t *testing.T, path string) *writeScript {
	t.Helper()
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	p.UserSectors = 64
	img, err := Create(path, p, optsPtr2("compress=true;deduplicate=false;md5=true"))
	if err != nil {
		t.Fatal(err)
	}
	img.nextBlockPosition = 5 << 30 // as if 5 GiB of blocks had been written
	for i := uint64(0); i < 64; i++ {
		data := []byte(strings.Repeat(string(rune('a'+i%26)), 512))
		s.ws(i, false, SectorStatusDumped, data)
		if err := img.WriteSector(i, false, data, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	if img.size < 5<<30 {
		t.Fatalf("image size %d", img.size)
	}
	return s
}

func optsPtr2(s string) *Options {
	o := ParseOptions(s)
	return &o
}

func TestLargeImageReadBack(t *testing.T) {
	if testing.Short() {
		t.Skip("creates a sparse file larger than 4 GiB")
	}
	path := filepath.Join(t.TempDir(), "large.aif")
	s := largeImage(t, path)
	expectSectors(t, s, path)
}
