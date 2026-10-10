package aaru

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// rdHeaderStream returns a crafted 104-byte AaruHeader with the given magic and
// major version.
func rdHeaderStream(magic uint64, major uint8) *bytes.Reader {
	b := make([]byte, headerV1Size)
	binary.LittleEndian.PutUint64(b, magic)
	b[72] = major // imageMajorVersion
	return bytes.NewReader(b)
}

// rdIdentifyFixtures writes a version 2 image, a version 1 image and a file
// of random bytes.
func rdIdentifyFixtures(t *testing.T) map[string]string {
	t.Helper()
	v1, _ := v1FloppyImage()
	return map[string]string{
		"v2":     blockTestImage(t, media.DOS_35_HD, 64),
		"v1":     writeTempFile(t, "v1.aif", v1),
		"random": writeTempFile(t, "random", seededRandom(1, 1<<20)),
	}
}

func rdOpenFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(rdIdentifyFixtures(t)[name])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestIdentifyStream(t *testing.T) {
	cases := []struct {
		name string
		r    func(t *testing.T) (int, error)
		want int
	}{
		{"StreamValidV2", func(t *testing.T) (int, error) { return IdentifyReader(rdOpenFixture(t, "v2"), -1) }, 100},
		{"StreamValidV1", func(t *testing.T) (int, error) { return IdentifyReader(rdOpenFixture(t, "v1"), -1) }, 100},
		{"StreamNull", func(t *testing.T) (int, error) { return IdentifyReader(nil, 0) }, 0},
		{"StreamNonAaru", func(t *testing.T) (int, error) { return IdentifyReader(rdOpenFixture(t, "random"), -1) }, 0},
		{"StreamEmptyFile", func(t *testing.T) (int, error) { return IdentifyReader(bytes.NewReader(nil), 0) }, 0},
		{"StreamTruncatedHeader", func(t *testing.T) (int, error) {
			// Only the 8-byte magic: not enough for a full 104-byte AaruHeader.
			b := binary.LittleEndian.AppendUint64(nil, AaruMagic)
			return IdentifyReader(bytes.NewReader(b), int64(len(b)))
		}, 0},
		{"StreamInvalidMagic", func(t *testing.T) (int, error) {
			return IdentifyReader(rdHeaderStream(0xDEADBEEFDEADBEEF, 2), headerV1Size)
		}, 0},
		{"StreamFutureVersion", func(t *testing.T) (int, error) { return IdentifyReader(rdHeaderStream(AaruMagic, 255), headerV1Size) }, 0},
		{"StreamDicMagic", func(t *testing.T) (int, error) { return IdentifyReader(rdHeaderStream(DICMagic, 1), headerV1Size) }, 100},
		{"StreamMidPosition", func(t *testing.T) (int, error) {
			// identify reads from offset 0 regardless of any stream position.
			f := rdOpenFixture(t, "v2")
			if _, err := f.Seek(50, 0); err != nil {
				t.Fatal(err)
			}
			return IdentifyReader(f, -1)
		}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.r(t)
			if err != nil {
				t.Fatalf("IdentifyReader: %v", err)
			}
			if got != tc.want {
				t.Errorf("IdentifyReader = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIdentifyFile(t *testing.T) {
	fixtures := rdIdentifyFixtures(t)
	cases := []struct {
		name, path string
		want       int
	}{
		{"FileValidV2", fixtures["v2"], 100},
		{"FileValidV1", fixtures["v1"], 100},
		{"FileNonAaru", fixtures["random"], 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Identify(tc.path)
			if err != nil {
				t.Fatalf("Identify: %v", err)
			}
			if got != tc.want {
				t.Errorf("Identify = %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("FileNull", func(t *testing.T) {
		if _, err := Identify(""); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Identify(\"\") error = %v, want fs.ErrInvalid", err)
		}
	})
	t.Run("FileNonExistent", func(t *testing.T) {
		if _, err := Identify("/tmp/go-aaru_test_nonexistent_identify.aif"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Identify(missing) error = %v, want fs.ErrNotExist", err)
		}
	})
}
