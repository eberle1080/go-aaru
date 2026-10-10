package iso9660

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/eberle1080/go-aaru/console/ps3/sfo"
)

// The tests read volumes built in memory (see builder_test.go).

func loadISO(t *testing.T) ReadSectorFunc {
	t.Helper()

	img, _ := ps3Volume()

	return memReader(img)
}

func memReader(img []byte) ReadSectorFunc {
	return func(lba uint64, buf []byte) error {
		off := lba * SectorSize
		if off+SectorSize > uint64(len(img)) {
			return errors.New("read past end of image")
		}

		copy(buf, img[off:off+SectorSize])

		return nil
	}
}

func TestReadParamSfo(t *testing.T) {
	img, want := ps3Volume()

	data, err := ReadFile(memReader(img), "/PS3_GAME/PARAM.SFO")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("PARAM.SFO: got %d bytes, want the %d bytes written", len(data), len(want))
	}

	f, err := sfo.Parse(data)
	if err != nil {
		t.Fatalf("sfo.Parse: %v", err)
	}

	if len(f.Entries) != len(ps3Params) {
		t.Fatalf("SFO entries = %d, want %d", len(f.Entries), len(ps3Params))
	}

	for k, v := range ps3Params {
		if got, ok := f.Get(k); !ok || got != v {
			t.Errorf("%s = %q, %v; want %q", k, got, ok, v)
		}
	}
}

// Every file of the PS3 layout reads back exactly as written, including a
// multi-sector file and entries in the second sector of the root directory.
func TestReadAllFiles(t *testing.T) {
	rd := loadISO(t)

	files := map[string][]byte{
		"/PS3_DISC.SFB":              patterned(64, 1),
		"/F00.DAT":                   patterned(10, 0),
		"/F59.DAT":                   patterned(10, 59),
		"/PS3_GAME/ICON0.PNG":        patterned(3000, 0x55),
		"/PS3_GAME/USRDIR/EBOOT.BIN": patterned(100, 0xEB),
	}

	for p, want := range files {
		got, err := ReadFile(rd, p)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: got %d bytes, %v; want %d bytes", p, len(got), err, len(want))
		}
	}

	// A directory read as a file yields its extent.
	if got, err := ReadFile(rd, "/PS3_GAME"); err != nil || len(got) != SectorSize {
		t.Errorf("/PS3_GAME: got %d bytes, %v", len(got), err)
	}
}

func TestEmptyFile(t *testing.T) {
	img := buildVolume(isoDir("\x00", isoFile("EMPTY.BIN;1", nil), isoFile("DOT.;1", []byte("x"))))

	got, err := ReadFile(memReader(img), "/EMPTY.BIN")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}

	// Trailing '.' after the version suffix is stripped.
	if got, err := ReadFile(memReader(img), "/DOT"); err != nil || string(got) != "x" {
		t.Fatalf("DOT: got %q, %v", got, err)
	}
}

// Truncated images fail cleanly.
func TestTruncatedImage(t *testing.T) {
	img, _ := ps3Volume()

	for _, n := range []int{0, 16 * SectorSize, 18 * SectorSize, len(img) - SectorSize} {
		if _, err := ReadFile(memReader(img[:n]), "/PS3_GAME/USRDIR/EBOOT.BIN"); err == nil {
			t.Errorf("image truncated to %d bytes: no error", n)
		}
	}
}

func TestReadWithoutLeadingSlash(t *testing.T) {
	data, err := ReadFile(loadISO(t), "PS3_GAME/PARAM.SFO")
	if err != nil || len(data) == 0 {
		t.Fatalf("got %d bytes, %v", len(data), err)
	}
}

func TestCaseInsensitive(t *testing.T) {
	data, err := ReadFile(loadISO(t), "/ps3_game/param.sfo")
	if err != nil || len(data) == 0 {
		t.Fatalf("got %d bytes, %v", len(data), err)
	}
}

func TestRedundantSlashes(t *testing.T) {
	data, err := ReadFile(loadISO(t), "//PS3_GAME//PARAM.SFO")
	if err != nil || len(data) == 0 {
		t.Fatalf("got %d bytes, %v", len(data), err)
	}
}

func TestFileNotFound(t *testing.T) {
	data, err := ReadFile(loadISO(t), "/NONEXISTENT/FILE.TXT")
	if !errors.Is(err, ErrNotFound) || data != nil {
		t.Fatalf("got %v, %v; want nil, ErrNotFound", data, err)
	}
}

func TestDirNotFound(t *testing.T) {
	if _, err := ReadFile(loadISO(t), "/PS3_GAME/NONEXISTENT.BIN"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFileAsDirectory(t *testing.T) {
	if _, err := ReadFile(loadISO(t), "/PS3_GAME/PARAM.SFO/X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNilArgs(t *testing.T) {
	if _, err := ReadFile(nil, "/test"); !errors.Is(err, ErrNilReader) {
		t.Fatalf("err = %v, want ErrNilReader", err)
	}
}

func TestReadFailure(t *testing.T) {
	failing := func(uint64, []byte) error { return errors.New("boom") }

	if _, err := ReadFile(failing, "/PS3_GAME/PARAM.SFO"); err == nil {
		t.Fatal("read failure not reported")
	}
}

func TestNotISO(t *testing.T) {
	if _, err := ReadFile(memReader(make([]byte, 20*SectorSize)), "/X"); !errors.Is(err, ErrNotISO9660) {
		t.Fatalf("err = %v, want ErrNotISO9660", err)
	}
}

// buildISO builds a tiny image: root directory at LBA 18 containing a
// directory "DIR" (LBA 19) which contains "FILE.BIN;1" (LBA 20, 3000 bytes),
// plus optional malformed records.
func buildISO(malformed bool) []byte {
	img := make([]byte, 23*SectorSize)

	pvd := img[16*SectorSize:]
	pvd[0] = 1
	copy(pvd[1:], "CD001")

	rec := func(dst []byte, lba, size uint32, flags byte, id string) int {
		l := 33 + len(id)
		if l%2 == 1 {
			l++
		}

		dst[0] = byte(l)
		binary.LittleEndian.PutUint32(dst[2:], lba)
		binary.LittleEndian.PutUint32(dst[10:], size)
		dst[25] = flags
		dst[32] = byte(len(id))
		copy(dst[33:], id)

		return l
	}

	rec(pvd[156:], 18, SectorSize, 2, "\x00")

	root := img[18*SectorSize:]
	p := rec(root, 18, SectorSize, 2, "\x00")
	p += rec(root[p:], 18, SectorSize, 2, "\x01")

	if malformed {
		// Identifier length overrunning its record: must be skipped safely.
		root[p] = 34
		root[p+32] = 200
		p += 34
	}

	rec(root[p:], 19, SectorSize, 2, "DIR")

	dir := img[19*SectorSize:]
	p = rec(dir, 19, SectorSize, 2, "\x00")
	p += rec(dir[p:], 18, SectorSize, 2, "\x01")
	rec(dir[p:], 20, 3000, 0, "FILE.BIN;1")

	for i := 0; i < 3000; i++ {
		img[20*SectorSize+i] = byte(i * 3)
	}

	return img
}

func TestSyntheticISO(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		data, err := ReadFile(memReader(buildISO(malformed)), "/dir/file.bin")
		if err != nil {
			t.Fatalf("malformed=%v: %v", malformed, err)
		}

		if len(data) != 3000 {
			t.Fatalf("len = %d, want 3000", len(data))
		}

		for i, b := range data {
			if b != byte(i*3) {
				t.Fatalf("byte %d = %d", i, b)
			}
		}
	}
}
