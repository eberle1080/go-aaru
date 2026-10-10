package udf

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// A UDF 2.50 volume with a metadata partition stored in an AaruFormat
// Blu-ray image is read through Image.UserDataReaderAt; a sector that was
// never dumped surfaces as aaru.ErrSectorNotDumped for the file that holds
// it only.
func TestThroughAaruImage(t *testing.T) {
	b := newUB()
	b.rev, b.efe, b.kind = 0x0250, true, kindMetadata
	big := ufile("big.bin", randData(20, 5*2048+300))
	lost := ufile("lost.bin", randData(21, 3*2048))
	root := udir("", big, lost, udir("sub", ufile("a.txt", []byte("inside"))))
	img := b.build(root)
	missing := lost.dataAbs[0] + 1

	path := filepath.Join(t.TempDir(), "disc.aif")
	opts := aaru.ParseOptions("compress=true;deduplicate=true")
	w, err := aaru.Create(path, aaru.CreateParams{MediaType: media.BDROM, SectorSize: 2048,
		UserSectors: uint64(len(img) / 2048), Application: "go-aaru-test"}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	for lba := 0; lba < len(img)/2048; lba++ {
		if uint32(lba) == missing {
			continue
		}
		if err := w.WriteSector(uint64(lba), false, img[lba*2048:(lba+1)*2048], aaru.SectorStatusDumped); err != nil {
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
	got := walkAll(t, v)
	if !bytes.Equal(got.data["/big.bin"], big.data) || string(got.data["/sub/a.txt"]) != "inside" {
		t.Errorf("contents differ: %v", got.order)
	}
	if err := got.errs["/lost.bin"]; !errors.Is(err, aaru.ErrSectorNotDumped) {
		t.Errorf("lost.bin: %v", err)
	}
	if len(got.errs) != 1 {
		t.Errorf("errors %v", got.errs)
	}
}

// fileInfo is what a reference implementation says about one entry.
type fileInfo struct {
	dir     bool
	mode    fs.FileMode
	size    int64
	mtime   time.Time
	uid     uint32
	gid     uint32
	target  string
	sum     [32]byte
	streams []string
}

// snapshot records a directory tree as the operating system sees it.
func snapshot(t *testing.T, root string, withXattr bool) map[string]fileInfo {
	t.Helper()
	out := map[string]fileInfo{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			rel = "/"
		} else {
			rel = "/" + filepath.ToSlash(rel)
		}
		if strings.HasPrefix(rel, "/.") { // .fseventsd, .Trashes and the like
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		fi := fileInfo{dir: st.IsDir(), mode: st.Mode(), size: st.Size(), mtime: st.ModTime()}
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			fi.uid, fi.gid = sys.Uid, sys.Gid
		}
		switch {
		case st.Mode()&fs.ModeSymlink != 0:
			if fi.target, err = os.Readlink(p); err != nil {
				return err
			}
		case st.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fi.sum = sha256.Sum256(data)
		}
		if withXattr && (st.Mode().IsRegular() || st.IsDir()) {
			if o, err := exec.Command("xattr", p).Output(); err == nil {
				for _, l := range strings.Split(strings.TrimSpace(string(o)), "\n") {
					if l != "" {
						fi.streams = append(fi.streams, l)
					}
				}
			}
		}
		out[rel] = fi
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// compareVolume checks every entry of v against the reference snapshot.
// checkMeta also compares modes, modification times and owners.
func compareVolume(t *testing.T, v *Volume, want map[string]fileInfo, checkMeta bool) {
	t.Helper()
	got := map[string]bool{}
	err := v.Walk(func(p string, e *Entry, err error) error {
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		got[p] = true
		w, ok := want[p]
		if !ok {
			// AppleDouble files hold extended attributes on revisions
			// without streams; macOS hides them, but they are on the disc.
			if !strings.HasPrefix(e.Name, "._") {
				t.Errorf("%s: not in the reference", p)
			}
			return nil
		}
		if e.IsDir != w.dir {
			t.Errorf("%s: directory %v, want %v", p, e.IsDir, w.dir)
		}
		if e.IsSymlink != (w.mode&fs.ModeSymlink != 0) || e.SymlinkTarget != w.target {
			t.Errorf("%s: symlink %v %q, want %q", p, e.IsSymlink, e.SymlinkTarget, w.target)
		}
		if w.mode.IsRegular() {
			if e.Size != w.size {
				t.Errorf("%s: size %d, want %d", p, e.Size, w.size)
			}
			f, err := v.OpenFile(e)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				return nil
			}
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				t.Errorf("%s: %v", p, err)
			}
			if !bytes.Equal(h.Sum(nil), w.sum[:]) {
				t.Errorf("%s: contents differ", p)
			}
		}
		if checkMeta {
			if !e.ModTime().Equal(w.mtime) {
				t.Errorf("%s: mtime %v, want %v", p, e.ModTime(), w.mtime)
			}
			if e.Mode() != w.mode {
				t.Errorf("%s: mode %v, want %v", p, e.Mode(), w.mode)
			}
			if !e.HasUID || e.UID != w.uid || !e.HasGID || e.GID != w.gid {
				t.Errorf("%s: owner %d:%d (%v %v), want %d:%d", p, e.UID, e.GID, e.HasUID, e.HasGID, w.uid, w.gid)
			}
		}
		var names []string
		for _, s := range e.Streams {
			names = append(names, s.Name)
			f, err := v.OpenStream(s)
			if err != nil {
				t.Errorf("%s: %v", s.Path, err)
			} else if _, err := io.Copy(io.Discard, f); err != nil {
				t.Errorf("%s: %v", s.Path, err)
			}
		}
		for _, x := range w.streams {
			// macOS presents the Mac FinderInfo extended attribute as
			// com.apple.FinderInfo.
			if x == "com.apple.FinderInfo" && len(e.MacFinderInfo) == 32 {
				continue
			}
			// Before UDF 2.00 there are no streams: macOS keeps extended
			// attributes in the AppleDouble files listed beside.
			if !slices.Contains(names, x) && v.Info().Revision >= 0x0200 {
				t.Errorf("%s: extended attribute %q not among the streams %q", p, x, names)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for p := range want {
		if !got[p] {
			t.Errorf("%s: missing", p)
		}
	}
	for _, w := range v.Warnings() {
		t.Errorf("warning: %v", w)
	}
}

// Volumes written by macOS hdiutil makehybrid (UDF 1.02 and 1.50 bridge
// discs, with ISO 9660 and Joliet) read like the source tree.
func TestHdiutilMakehybridUDF(t *testing.T) {
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		t.Skip("hdiutil not available")
	}
	src := filepath.Join(t.TempDir(), "src")
	for name, data := range map[string][]byte{
		"readme.txt":                        []byte("hello\n"),
		"Sub Dir/blob.bin":                  randData(30, 300000),
		"A long file name, with spaces.txt": []byte("long"),
		"Grüße ☃.txt":                       []byte("unicode"),
		"empty.dat":                         nil,
		"a/b/c/d/e/f/g/h/i/j/deep.txt":      []byte("deep"),
	} {
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
	want := snapshot(t, src, false)
	for _, ver := range []string{"1.02", "1.50"} {
		t.Run(ver, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "hybrid.iso")
			cmd := exec.Command(hdiutil, "makehybrid", "-udf", "-udf-version", ver, "-iso", "-joliet",
				"-default-volume-name", "UDFHYBRID", "-o", out, src)
			if msg, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("hdiutil makehybrid failed: %v\n%s", err, msg)
			}
			img, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			v := mustOpen(t, img, nil)
			if want := map[string]uint16{"1.02": 0x0102, "1.50": 0x0150}[ver]; v.Info().Revision != want {
				t.Errorf("revision %#x", v.Info().Revision)
			}
			// makehybrid's POSIX attributes are its own, so only names,
			// sizes and contents are compared, plus one known mtime.
			compareVolume(t, v, want, false)
			if e := walkAll(t, v).entry(t, "/readme.txt"); !e.ModTime().Equal(mtime) {
				t.Errorf("readme.txt mtime %v", e.ModTime())
			}
		})
	}
}

var lastWritten = regexp.MustCompile(`last written block address: (\d+)`)

// Write-once volumes made by newfs_udf (a virtual partition with a UDF 1.50
// or 2.00+ virtual allocation table). macOS can mount such image files but
// not populate them, so the empty volume is checked; no mount is needed.
func TestNewfsUDFWriteOnce(t *testing.T) {
	newfs, err := exec.LookPath("newfs_udf")
	if err != nil {
		t.Skip("newfs_udf not available")
	}
	mkfile, err := exec.LookPath("mkfile")
	if err != nil {
		t.Skip("mkfile not available")
	}
	for _, rev := range []string{"1.50", "2.01", "2.50", "2.60"} {
		t.Run(rev, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wo.img")
			if msg, err := exec.Command(mkfile, "-n", "4m", path).CombinedOutput(); err != nil {
				t.Fatalf("mkfile: %v %s", err, msg)
			}
			msg, err := exec.Command(newfs, "-r", rev, "-t", "wo", "-v", "WriteOnce", path).CombinedOutput()
			if err != nil {
				t.Skipf("newfs_udf: %v %s", err, msg)
			}
			m := lastWritten.FindSubmatch(msg)
			if m == nil {
				t.Fatalf("no last written block in %q", msg)
			}
			last, _ := strconv.Atoi(string(m[1]))
			img, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// The whole (mostly unwritten) image, told where the session
			// ends, and the image truncated to what was written.
			for _, c := range []struct {
				img  []byte
				opts *Options
			}{{img, &Options{LastBlock: uint32(last)}}, {img[:(last+1)*2048], nil}} {
				v := mustOpen(t, c.img, c.opts)
				noWarnings(t, v)
				info := v.Info()
				if info.LogicalVolumeID != "WriteOnce" || len(info.Partitions) != 2 ||
					info.Partitions[1].Type != PartitionVirtual || info.Partitions[0].AccessType != AccessWriteOnce {
					t.Errorf("info %+v", info)
				}
				w := walkAll(t, v)
				if len(w.order) != 1 || len(w.errs) != 0 {
					t.Errorf("walk %v %v", w.order, w.errs)
				}
			}
			// Without LastBlock, the table is found by searching back from
			// the end of the image.
			v := mustOpen(t, img, nil)
			if !hasWarning(v, "blocks before the last block") || v.Info().LogicalVolumeID != "WriteOnce" {
				t.Errorf("search back: %v", v.Warnings())
			}
		})
	}
}

// Freshly formatted volumes of other block sizes (hdiutil cannot attach
// those raw images, so they stay empty) open with the right block size.
func TestNewfsUDFBlockSizes(t *testing.T) {
	newfs, err := exec.LookPath("newfs_udf")
	if err != nil {
		t.Skip("newfs_udf not available")
	}
	for _, bs := range []int{512, 1024, 4096} {
		for _, rev := range []string{"1.02", "2.01", "2.50"} {
			path := filepath.Join(t.TempDir(), "bs.img")
			if err := os.WriteFile(path, make([]byte, 8<<20), 0o644); err != nil {
				t.Fatal(err)
			}
			msg, err := exec.Command(newfs, "-r", rev, "-b", strconv.Itoa(bs), "-v", "Blocks", path).CombinedOutput()
			if err != nil {
				t.Errorf("newfs_udf -b %d -r %s: %v %s", bs, rev, err, msg)
				continue
			}
			img, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			v := mustOpen(t, img, nil)
			noWarnings(t, v)
			if v.BlockSize() != bs || v.Info().LogicalVolumeID != "Blocks" {
				t.Errorf("-b %d -r %s: block size %d, %+v", bs, rev, v.BlockSize(), v.Info())
			}
			if w := walkAll(t, v); len(w.order) != 1 || len(w.errs) != 0 {
				t.Errorf("-b %d -r %s: walk %v %v", bs, rev, w.order, w.errs)
			}
		}
	}
}

// attach attaches a raw image with hdiutil at a private mount point and
// returns the mount point and a function detaching it (which also runs when
// the test ends, if not called before).
func attach(t *testing.T, hdiutil, img string, readonly bool) (string, func()) {
	t.Helper()
	mnt := filepath.Join(t.TempDir(), "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"attach", "-imagekey", "diskimage-class=CRawDiskImage", "-nobrowse", "-mountpoint", mnt}
	if readonly {
		args = append(args, "-readonly")
	}
	out, err := exec.Command(hdiutil, append(args, img)...).CombinedOutput()
	dev := ""
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.HasPrefix(f[0], "/dev/disk") {
			dev = strings.TrimSpace(f[0])
			break
		}
	}
	done := false
	release := func() {
		if !done && dev != "" {
			done = true
			detach(t, hdiutil, dev)
		}
	}
	t.Cleanup(release)
	if err != nil || dev == "" {
		t.Fatalf("hdiutil attach: %v\n%s", err, out)
	}
	return mnt, release
}

func detach(t *testing.T, hdiutil, dev string) {
	for i := range 6 {
		args := []string{"detach", dev}
		if i >= 3 {
			args = append(args, "-force")
		}
		out, err := exec.Command(hdiutil, args...).CombinedOutput()
		if err == nil || strings.Contains(string(out), "No such file") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Errorf("could not detach %s", dev)
}

// populate writes a test tree into a mounted volume. Operations the volume
// does not support (hard links, FIFOs, extended attributes) are skipped.
func populate(t *testing.T, mnt string) {
	t.Helper()
	write := func(name string, data []byte, perm fs.FileMode) {
		p := filepath.Join(mnt, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, perm); err != nil {
			t.Fatal(err)
		}
	}
	write("readme.txt", []byte("hello\n"), 0o644)
	write("empty.dat", nil, 0o644)
	write("secret.txt", []byte("secret"), 0o600)
	write("run.sh", []byte("#!/bin/sh\necho hi\n"), 0o755)
	write("Sub Dir/blob.bin", randData(31, 300000), 0o644)
	write("Sub Dir/deeper/deep.txt", []byte("deep"), 0o640)
	// Characters without decompositions: macOS returns names in NFD while
	// the disc (and this package) has them as recorded.
	write("Snow ☃ ß ø 😀.txt", []byte("unicode"), 0o644)
	write("A long file name, with spaces and punctuation (1)!.txt", []byte("long"), 0o644)
	write("big.bin", randData(32, 2<<20+12345), 0o644)
	for name, target := range map[string]string{"rel link": "Sub Dir/blob.bin", "abs link": "/etc/hosts",
		"Sub Dir/up": "../readme.txt", "Sub Dir/dot": "./deeper/deep.txt", "dangling": "nowhere/at/all"} {
		if err := os.Symlink(target, filepath.Join(mnt, name)); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Link(filepath.Join(mnt, "readme.txt"), filepath.Join(mnt, "hard.txt"))
	_ = syscall.Mkfifo(filepath.Join(mnt, "fifo"), 0o644)
	_ = exec.Command("xattr", "-w", "com.example.note", "a note", filepath.Join(mnt, "readme.txt")).Run()
	_ = exec.Command("xattr", "-w", "com.example.dir", "dir note", filepath.Join(mnt, "Sub Dir")).Run()
	if err := os.Chmod(filepath.Join(mnt, "Sub Dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Times last: writing changes them.
	when := time.Date(2004, 5, 1, 19, 0, 0, 0, time.UTC)
	for i, name := range []string{"readme.txt", "secret.txt", "Sub Dir/blob.bin", "Sub Dir/deeper/deep.txt",
		"Sub Dir/deeper", "Sub Dir"} {
		tm := when.Add(time.Duration(i)*time.Hour + 123456789*time.Nanosecond)
		if err := os.Chtimes(filepath.Join(mnt, name), tm, tm); err != nil {
			t.Fatal(err)
		}
	}
}

// Volumes formatted by newfs_udf in every revision and variant macOS
// writes (file entries or extended file entries; physical, sparable or
// metadata partitions), populated through a mount, read back like a
// read-only mount of the same image (the macOS driver's view). Mounting
// has side effects, so the test only runs with GO_AARU_MOUNT_TESTS=1.
func TestNewfsUDFMounted(t *testing.T) {
	if os.Getenv("GO_AARU_MOUNT_TESTS") != "1" {
		t.Skip("set GO_AARU_MOUNT_TESTS=1 to mount test volumes with hdiutil")
	}
	newfs, err := exec.LookPath("newfs_udf")
	if err != nil {
		t.Skip("newfs_udf not available")
	}
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		t.Skip("hdiutil not available")
	}
	for _, variant := range [][]string{
		{"-r", "1.02"}, {"-r", "1.50"}, {"-r", "2.00"}, {"-r", "2.01"}, {"-r", "2.50"}, {"-r", "2.60"},
		{"-r", "2.01", "-e"}, {"-r", "1.50", "-d"}, {"-r", "2.01", "-d"}, {"-r", "2.60", "--dup-meta-part-file"},
	} {
		t.Run(strings.Join(variant, ""), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "udf.img")
			if msg, err := exec.Command("mkfile", "-n", "16m", path).CombinedOutput(); err != nil {
				t.Fatalf("mkfile: %v %s", err, msg)
			}
			args := append(append([]string{}, variant...), "-v", "GoAaruTest", path)
			if msg, err := exec.Command(newfs, args...).CombinedOutput(); err != nil {
				t.Skipf("newfs_udf %v: %v %s", variant, err, msg)
			}
			// Names, contents and extended attributes come from a read-only
			// mount of the image as written; modes from the read-write mount,
			// since the read-only one reports every file as executable.
			mnt, release := attach(t, hdiutil, path, false)
			populate(t, mnt)
			rw := snapshot(t, mnt, false)
			release()
			mnt, release = attach(t, hdiutil, path, true)
			want := snapshot(t, mnt, true)
			release()
			for p, w := range want {
				if x, ok := rw[p]; ok {
					w.mode = x.mode
					want[p] = w
				}
			}
			img, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			v := mustOpen(t, img, nil)
			if v.Info().LogicalVolumeID != "GoAaruTest" {
				t.Errorf("volume %q", v.Info().LogicalVolumeID)
			}
			compareVolume(t, v, want, true)
			if hard, ok := want["/hard.txt"]; ok && !hard.dir {
				w := walkAll(t, v)
				if a, b := w.entry(t, "/readme.txt"), w.entry(t, "/hard.txt"); a.ICB != b.ICB {
					t.Errorf("hard link ICBs %v %v", a.ICB, b.ICB)
				}
			}
			var types []string
			for _, p := range v.Info().Partitions {
				types = append(types, p.Type)
			}
			t.Logf("%d entries, block size %d, partitions %v", len(want), v.BlockSize(), types)
		})
	}
}
