package iso9660

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// An ISO stored in an AaruFormat image is read through
// Image.UserDataReaderAt; a sector that was never dumped surfaces as
// aaru.ErrSectorNotDumped for the file that holds it only.
func TestThroughAaruImage(t *testing.T) {
	b := newBuilder()
	b.joliet = true
	big := file("BIG.BIN;1", randData(10, 3*SectorSize+500))
	big.jname = "Big File.bin"
	big.split = []int{2 * SectorSize, SectorSize + 500}
	lost := file("LOST.BIN;1", randData(11, 3*SectorSize))
	lost.jname = "lost.bin"
	sub := dir("SUB", file("A.TXT;1", []byte("inside")))
	sub.jname = "Sub Dir"
	img := b.build(dir("", big, lost, sub))
	missing := lost.lba[0] + 1

	path := filepath.Join(t.TempDir(), "disc.aif")
	opts := aaru.ParseOptions("compress=true;deduplicate=true")
	w, err := aaru.Create(path, aaru.CreateParams{MediaType: media.DVDROM, SectorSize: SectorSize,
		UserSectors: uint64(len(img) / SectorSize), Application: "go-aaru-test"}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	for lba := 0; lba < len(img)/SectorSize; lba++ {
		if uint32(lba) == missing {
			continue
		}
		if err := w.WriteSector(uint64(lba), false, img[lba*SectorSize:(lba+1)*SectorSize], aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	ai, err := aaru.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ai.Close()
	r, err := ai.UserDataReaderAt()
	if err != nil {
		t.Fatal(err)
	}
	v, err := Open(r, r.Size(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Namespace() != NamespaceJoliet {
		t.Errorf("namespace %v", v.Namespace())
	}
	got := walkAll(t, v)
	if !bytes.Equal(got.data["/Big File.bin"], big.data) || string(got.data["/Sub Dir/a.txt"]) != "inside" {
		t.Errorf("contents differ: %v", got.order)
	}
	if err := got.errs["/lost.bin"]; !errors.Is(err, aaru.ErrSectorNotDumped) {
		t.Errorf("lost.bin: %v", err)
	}
	if len(got.errs) != 1 {
		t.Errorf("errors %v", got.errs)
	}
}

// Cross-check against a volume written by macOS hdiutil (ISO 9660 + Joliet,
// with Rock Ridge PX/TF entries but no NM names, NUL-filled unset dates).
func TestHdiutilCrossCheck(t *testing.T) {
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		t.Skip("hdiutil not available")
	}
	src := filepath.Join(t.TempDir(), "src")
	files := map[string][]byte{
		"readme.txt":                        []byte("hello\n"),
		"Sub Dir/blob.bin":                  randData(12, 300000),
		"A long file name, with spaces.txt": []byte("long"),
		"empty.dat":                         nil,
		"a/b/c/d/e/f/g/h/i/j/deep.txt":      []byte("deep"),
	}
	for name, data := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mtime := time.Date(2004, 5, 1, 19, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "readme.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "test.iso")
	cmd := exec.Command(hdiutil, "makehybrid", "-iso", "-joliet", "-default-volume-name", "TESTDISC", "-o", out, src)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("hdiutil makehybrid failed: %v\n%s", err, msg)
	}
	img, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	v := mustOpen(t, img, nil)
	if v.Namespace() != NamespaceJoliet || v.Info().VolumeID != "TESTDISC" {
		t.Errorf("namespace %v, volume %q", v.Namespace(), v.Info().VolumeID)
	}
	w := walkAll(t, v)
	if len(w.errs) > 0 {
		t.Fatal(w.errs)
	}
	for name, data := range files {
		got, ok := w.data["/"+name]
		if !ok || !bytes.Equal(got, data) {
			t.Errorf("%s: present %v, %d bytes", name, ok, len(got))
		}
	}
	nfiles := 0
	for _, e := range w.entries {
		if !e.IsDir {
			nfiles++
		}
	}
	if nfiles != len(files) {
		t.Errorf("%d files, want %d: %v", nfiles, len(files), w.order)
	}
	readme := w.entry(t, "/readme.txt")
	if !readme.ModTime().Equal(mtime) || readme.Mode() != 0o644 {
		t.Errorf("readme.txt: %v %v", readme.ModTime(), readme.Mode())
	}
	if d := w.entry(t, "/Sub Dir"); d.Mode() != fs.ModeDir|0o755 {
		t.Errorf("Sub Dir mode %v", d.Mode())
	}

	// Every namespace reads without errors and finds the same files.
	for _, ns := range v.Namespaces() {
		v := mustOpen(t, img, &Options{Namespace: ns})
		w := walkAll(t, v)
		if len(w.errs) > 0 {
			t.Errorf("%v: %v", ns, w.errs)
		}
		n := 0
		for p, data := range w.data {
			if len(data) == 300000 && strings.HasSuffix(strings.ToLower(p), "blob.bin") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%v: blob.bin not found: %v", ns, w.order)
		}
	}
}
