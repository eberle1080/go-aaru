package udf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

func randData(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 9))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func mustOpen(t testing.TB, img []byte, o *Options) *Volume {
	t.Helper()
	v, err := Open(bytes.NewReader(img), int64(len(img)), o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

// walked is the result of walking a volume and reading every file.
type walked struct {
	entries map[string]*Entry
	data    map[string][]byte
	errs    map[string]error
	order   []string
}

func walkAll(t testing.TB, v *Volume) walked {
	t.Helper()
	w := walked{entries: map[string]*Entry{}, data: map[string][]byte{}, errs: map[string]error{}}
	err := v.Walk(func(p string, e *Entry, err error) error {
		if err != nil {
			w.errs[p] = err
			return nil
		}
		w.entries[p] = e
		w.order = append(w.order, p)
		if !e.IsDir {
			f, err := v.OpenFile(e)
			if err != nil {
				w.errs[p] = err
				return nil
			}
			b, err := io.ReadAll(f)
			if err != nil {
				w.errs[p] = err
			}
			w.data[p] = b
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return w
}

func (w walked) entry(t testing.TB, p string) *Entry {
	t.Helper()
	e := w.entries[p]
	if e == nil {
		t.Fatalf("no entry %q; have %v (errors %v)", p, w.order, w.errs)
	}
	return e
}

func noWarnings(t testing.TB, v *Volume) {
	t.Helper()
	if w := v.Warnings(); len(w) > 0 {
		t.Errorf("unexpected warnings: %v", w)
	}
}

func hasWarning(v *Volume, sub string) bool {
	for _, w := range v.Warnings() {
		if strings.Contains(w.String(), sub) {
			return true
		}
	}
	return false
}

// checkFiles verifies that every file of want reads back, and that the walk
// had no errors.
func checkFiles(t testing.TB, w walked, want map[string][]byte) {
	t.Helper()
	for p, d := range want {
		got, ok := w.data[p]
		if !ok || !bytes.Equal(got, d) {
			t.Errorf("%s: present %v, %d bytes, want %d (have %v, errors %v)", p, ok, len(got), len(d), w.order, w.errs)
		}
	}
	if len(w.errs) > 0 {
		t.Errorf("errors: %v", w.errs)
	}
}

func TestPlain102(t *testing.T) {
	b := newUB()
	b.rev = 0x0102
	big := ufile("big.bin", randData(1, 3*2048+100))
	multi := ufile("multi.bin", randData(2, 5000))
	multi.split = []int{4096, 904}
	hidden := ufile("hidden.txt", []byte("h"))
	hidden.char = CharHidden
	gone := ufile("deleted.txt", []byte("gone"))
	gone.char = CharDeleted
	uni := ufile("Grüße ☃.txt", []byte("unicode"))
	exec := ufile("run.sh", []byte("#!/bin/sh\n"))
	exec.perms = perm755
	exec.uid, exec.gid = 0xFFFFFFFF, 0xFFFFFFFF
	root := udir("",
		ufile("readme.txt", []byte("hello\n")),
		ufile("empty", nil),
		big, multi, hidden, gone, uni, exec,
		udir("sub", ufile("a.txt", []byte("A")), udir("deeper", ufile("b.txt", []byte("B")))),
	)
	img := b.build(root)
	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	info := v.Info()
	if info.Revision != 0x0102 || info.NSR != 2 || info.VolumeID != "TESTVOL" || info.LogicalVolumeID != "Test Volume" ||
		info.FileSetID != "Test File Set" || info.CopyrightFile != "COPYING" || info.VolumeSetID != "Test Volume Set" ||
		info.ImplementationID != "*go-aaru test" || info.ApplicationID != "*go-aaru test app" || info.DomainID != "*OSTA UDF Compliant" {
		t.Errorf("info %+v", info)
	}
	if !info.HasCounts || info.NumFiles != 9 || info.NumDirectories != 3 || info.MinReadRevision != 0x0102 || info.IntegrityOpen {
		t.Errorf("counts %+v", info)
	}
	if len(info.Partitions) != 1 || info.Partitions[0].Type != PartitionPhysical || info.Partitions[0].Start != 272 ||
		info.Partitions[0].AccessType != AccessOverwritable {
		t.Errorf("partitions %+v", info.Partitions)
	}
	if info.Recorded == nil || !info.Recorded.Equal(testCTime) || info.FileSetRecorded == nil {
		t.Errorf("recorded %v %v", info.Recorded, info.FileSetRecorded)
	}
	if v.BlockSize() != 2048 || info.VolumeSpaceBlocks != uint32(len(img)/2048) {
		t.Errorf("block size %d, %d blocks", v.BlockSize(), info.VolumeSpaceBlocks)
	}
	w := walkAll(t, v)
	checkFiles(t, w, map[string][]byte{
		"/readme.txt": []byte("hello\n"), "/empty": {}, "/big.bin": big.data, "/multi.bin": multi.data,
		"/hidden.txt": []byte("h"), "/Grüße ☃.txt": []byte("unicode"), "/sub/a.txt": []byte("A"),
		"/sub/deeper/b.txt": []byte("B"),
	})
	if _, ok := w.entries["/deleted.txt"]; ok {
		t.Error("deleted entry returned")
	}
	if fmt.Sprint(w.order[:4]) != "[/ /readme.txt /empty /big.bin]" {
		t.Errorf("order %v", w.order)
	}
	e := w.entry(t, "/readme.txt")
	if e.Mode() != 0o644 || !e.ModTime().Equal(testMTime) || e.UID != 1000 || e.GID != 100 || !e.HasUID || !e.HasGID ||
		e.FileType != FileTypeRegular || e.ExtendedFileEntry || e.Times.Creation != nil || e.LinkCount != 1 {
		t.Errorf("readme %+v mode %v", e, e.Mode())
	}
	if _, off := e.ModTime().Zone(); off != 3600 || e.ModTime().Nanosecond() != 780_000_000 {
		t.Errorf("mtime zone %d, %v", off, e.ModTime())
	}
	if e := w.entry(t, "/run.sh"); e.Mode() != 0o755 || e.HasUID || e.HasGID {
		t.Errorf("run.sh %v %v %v", e.Mode(), e.HasUID, e.HasGID)
	}
	if d := w.entry(t, "/sub"); d.Mode() != fs.ModeDir|0o755 || !d.IsDir || d.Path != "/sub" || d.Name != "sub" {
		t.Errorf("sub %+v", d)
	}
	if !w.entry(t, "/hidden.txt").Hidden {
		t.Error("hidden flag")
	}
	if r := w.entry(t, "/"); r.Name != "" || !r.IsDir || r.Path != "/" {
		t.Errorf("root %+v", r)
	}
	// Reading files: Seek, ReadAt, Size, Entry.
	f, err := v.OpenFile(w.entry(t, "/multi.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() != 5000 || f.Entry().Name != "multi.bin" {
		t.Error("size/entry")
	}
	buf := make([]byte, 200)
	if n, err := f.ReadAt(buf, 4000); n != 200 || err != nil || !bytes.Equal(buf, multi.data[4000:4200]) {
		t.Errorf("ReadAt across extents: %d %v", n, err)
	}
	if p, _ := f.Seek(-10, io.SeekEnd); p != 4990 {
		t.Error("seek")
	}
	if n, err := f.Read(buf); n != 10 || err != nil {
		t.Errorf("read at end %d %v", n, err)
	}
	if _, err := f.Read(buf); err != io.EOF {
		t.Errorf("EOF: %v", err)
	}
	if _, err := f.Seek(0, 7); err == nil {
		t.Error("bad whence accepted")
	}
	if _, err := v.OpenFile(w.entry(t, "/sub")); !errors.Is(err, ErrIsDirectory) {
		t.Error(err)
	}
	if _, err := v.ReadDir(w.entry(t, "/readme.txt")); !errors.Is(err, ErrNotDirectory) {
		t.Error(err)
	}
	ents, err := v.ReadDir(v.Root())
	if err != nil || len(ents) != 8 {
		t.Errorf("ReadDir root: %d %v", len(ents), err)
	}
}

func TestEFELongADsEmbeddedAED(t *testing.T) {
	b := newUB()
	b.efe, b.ad = true, adLong
	emb := ufile("tiny.txt", []byte("embedded data"))
	emb.adType = adEmbedded
	chained := ufile("chained.bin", randData(3, 7*2048+33))
	chained.split = []int{2048, 2048, 2048, 2048, 2048, 2048, 2048 + 33}
	chained.aedPer = 2
	ext := ufile("ext.bin", randData(4, 3000))
	ext.adType = adExt
	sparse := ufile("sparse.bin", append(append(randData(5, 2048), make([]byte, 4096)...), randData(6, 100)...))
	sparse.split = []int{2048, 2048, 2048, 100}
	sparse.kinds = []byte{0, 1, 2, 0}
	short := ufile("short.bin", randData(7, 3000))
	short.adType = adShort
	embDir := udir("embdir", ufile("x", []byte("x")))
	embDir.adType = adEmbedded
	root := udir("", emb, chained, ext, sparse, short, embDir)
	v := mustOpen(t, b.build(root), nil)
	noWarnings(t, v)
	w := walkAll(t, v)
	checkFiles(t, w, map[string][]byte{"/tiny.txt": []byte("embedded data"), "/chained.bin": chained.data,
		"/ext.bin": ext.data, "/sparse.bin": sparse.data, "/short.bin": short.data, "/embdir/x": []byte("x")})
	e := w.entry(t, "/tiny.txt")
	if !e.Embedded || !e.ExtendedFileEntry || e.Times.Creation == nil || !e.Times.Creation.Equal(testCTime) {
		t.Errorf("tiny %+v", e)
	}
	if !w.entry(t, "/sparse.bin").Sparse || w.entry(t, "/chained.bin").Sparse {
		t.Error("sparse flag")
	}
	if v.Info().Revision != 0x0201 || v.Info().NSR != 3 {
		t.Errorf("revision %x", v.Info().Revision)
	}
}

func TestSparable(t *testing.T) {
	b := newUB()
	b.kind = kindSparable
	b.spare = []uint32{0} // the packet holding the file set descriptor and root
	big := ufile("big.bin", randData(8, 40*2048))
	root := udir("", ufile("a.txt", []byte("sparable")), big)
	img := b.build(root)
	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	p := v.Info().Partitions
	if len(p) != 1 || p[0].Type != PartitionSparable || p[0].PacketLength != 32 || p[0].SparedPackets != 1 ||
		p[0].Identifier != "*UDF Sparable Partition" {
		t.Errorf("partitions %+v", p)
	}
	want := map[string][]byte{"/a.txt": []byte("sparable"), "/big.bin": big.data}
	checkFiles(t, walkAll(t, v), want)

	// A damaged first sparing table: the second one is used.
	img2 := bytes.Clone(img)
	clear(img2[int(b.tableLocs[0])*2048:][:64])
	v = mustOpen(t, img2, nil)
	if !hasWarning(v, "is not a sparing table") {
		t.Errorf("warnings %v", v.Warnings())
	}
	checkFiles(t, walkAll(t, v), want)

	// No valid table at all: the packet is read unmapped, i.e. garbage.
	clear(img2[int(b.tableLocs[1])*2048:][:64])
	if _, err := Open(bytes.NewReader(img2), int64(len(img2)), nil); !errors.Is(err, ErrNoFileSet) {
		t.Errorf("without sparing: %v", err)
	}
}

func TestVAT(t *testing.T) {
	for _, kind := range []string{kindVAT150, kindVAT200} {
		t.Run(kind, func(t *testing.T) {
			b := newUB()
			b.rev, b.kind = 0x0150, kind
			if kind == kindVAT200 {
				b.rev, b.efe = 0x0201, true
			}
			b.lvidOpen = true
			big := ufile("big.bin", randData(9, 5000))
			root := udir("", ufile("a.txt", []byte("vat")), big, udir("d", ufile("b.txt", []byte("b"))))
			img := b.build(root)
			v := mustOpen(t, img, nil)
			noWarnings(t, v)
			info := v.Info()
			if len(info.Partitions) != 2 || info.Partitions[1].Type != PartitionVirtual || info.Partitions[0].AccessType != AccessWriteOnce {
				t.Errorf("partitions %+v", info.Partitions)
			}
			if kind == kindVAT200 && (info.LogicalVolumeID != "VAT Volume" || info.NumFiles != 3 || info.NumDirectories != 2) {
				t.Errorf("VAT header not applied: %+v", info)
			}
			if !info.IntegrityOpen {
				t.Error("integrity should be open")
			}
			want := map[string][]byte{"/a.txt": []byte("vat"), "/big.bin": big.data, "/d/b.txt": []byte("b")}
			checkFiles(t, walkAll(t, v), want)

			// Unrecorded blocks after the VAT: found by searching back.
			padded := append(bytes.Clone(img), make([]byte, 20*2048)...)
			v = mustOpen(t, padded, nil)
			if !hasWarning(v, "20 blocks before the last block") {
				t.Errorf("warnings %v", v.Warnings())
			}
			checkFiles(t, walkAll(t, v), want)
			// Too far back for the search: the virtual partition is unusable.
			if _, err := Open(bytes.NewReader(padded), int64(len(padded)), &Options{MaxVATSearch: 5}); !errors.Is(err, ErrNoFileSet) {
				t.Errorf("short search: %v", err)
			}
			// LastBlock overrides the size; an unknown size needs it.
			last := uint32(len(img)/2048 - 1)
			v = mustOpen(t, padded, &Options{LastBlock: last})
			noWarnings(t, v)
			if v, err := Open(bytes.NewReader(img), 0, &Options{LastBlock: last}); err != nil {
				t.Error(err)
			} else {
				checkFiles(t, walkAll(t, v), want)
			}
			if _, err := Open(bytes.NewReader(img), 0, nil); !errors.Is(err, ErrNoFileSet) {
				t.Errorf("unknown size: %v", err)
			}
		})
	}
}

type failingReader struct {
	r   io.ReaderAt
	bad map[int64]bool
}

var errInjected = errors.New("injected read error")

func (f failingReader) ReadAt(p []byte, off int64) (int, error) {
	for s := off / 2048; s <= (off+int64(len(p))-1)/2048; s++ {
		if f.bad[s] {
			return 0, errInjected
		}
	}
	return f.r.ReadAt(p, off)
}

func TestMetadataPartition(t *testing.T) {
	build := func(mod func(b *ubuilder)) (*ubuilder, []byte, map[string][]byte) {
		b := newUB()
		b.rev, b.efe, b.kind, b.mdSplit = 0x0250, true, kindMetadata, true
		if mod != nil {
			mod(b)
		}
		var kids []*unode
		want := map[string][]byte{}
		for i := range 40 { // enough to fill both halves of the metadata file
			n := fmt.Sprintf("file %02d with a long name to fill the directory.txt", i)
			kids = append(kids, ufile(n, []byte(n)))
			want["/many/"+n] = []byte(n)
		}
		big := ufile("big.bin", randData(10, 9000))
		want["/big.bin"] = big.data
		root := udir("", big, udir("many", kids...))
		return b, b.build(root), want
	}
	b, img, want := build(nil)
	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	p := v.Info().Partitions
	if len(p) != 2 || p[0].Type != PartitionPhysical || p[1].Type != PartitionMetadata || p[1].Start != 272 {
		t.Errorf("partitions %+v", p)
	}
	checkFiles(t, walkAll(t, v), want)
	if v.Info().Revision != 0x0250 {
		t.Errorf("revision %x", v.Info().Revision)
	}

	// The main metadata file's entry is destroyed: the mirror is used.
	img2 := bytes.Clone(img)
	clear(img2[int(b.partStart+b.mdFE)*2048:][:2048])
	v = mustOpen(t, img2, nil)
	if !hasWarning(v, "using the metadata mirror file") {
		t.Errorf("warnings %v", v.Warnings())
	}
	checkFiles(t, walkAll(t, v), want)

	// A block of the metadata file is unreadable: it is read from the
	// mirror.
	bad := failingReader{r: bytes.NewReader(img), bad: map[int64]bool{int64(b.partStart + b.mdRegions[1]): true}}
	v, err := Open(bad, int64(len(img)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkFiles(t, walkAll(t, v), want)
	if !hasWarning(v, "read from the metadata mirror") {
		t.Errorf("warnings %v", v.Warnings())
	}

	// Without a mirror, a destroyed metadata file loses the volume.
	b, img, _ = build(func(b *ubuilder) { b.noMirror = true })
	v = mustOpen(t, img, nil)
	noWarnings(t, v)
	clear(img[int(b.partStart+b.mdFE)*2048:][:2048])
	if _, err := Open(bytes.NewReader(img), int64(len(img)), nil); !errors.Is(err, ErrNoFileSet) {
		t.Errorf("no metadata: %v", err)
	}
}

func TestStrategy4096(t *testing.T) {
	b := newUB()
	b.efe = true
	f := ufile("current.txt", []byte("current contents"))
	f.strategy = true
	loop := ufile("loop.txt", []byte("looping"))
	loop.icbLoop = true
	d := udir("dir", ufile("in.txt", []byte("in")))
	d.strategy = true
	v := mustOpen(t, b.build(udir("", f, loop, d)), nil)
	w := walkAll(t, v)
	checkFiles(t, w, map[string][]byte{"/current.txt": []byte("current contents"), "/loop.txt": []byte("looping"),
		"/dir/in.txt": []byte("in")})
	if e := w.entry(t, "/current.txt"); e.ICB.Block != f.icb || e.loc.block != f.icb2 {
		t.Errorf("ICB %v, read at %v", e.ICB, e.loc)
	}
	if !hasWarning(v, "ICB chain revisits") || len(v.Warnings()) != 1 {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestNamedStreams(t *testing.T) {
	b := newUB()
	b.rev, b.efe = 0x0200, true
	rsrc := ufile("com.apple.ResourceFork", randData(11, 3000))
	xattr := ufile("user.comment", []byte("a comment"))
	f := ufile("doc.txt", []byte("document"))
	f.streams = []*unode{rsrc, xattr}
	d := udir("folder")
	d.streams = []*unode{ufile("com.apple.FinderInfo", make([]byte, 32))}
	b.sysStreams = []*unode{ufile("*UDF Unique ID Mapping Data", []byte("uid map"))}
	v := mustOpen(t, b.build(udir("", f, d)), nil)
	noWarnings(t, v)
	w := walkAll(t, v)
	if len(w.order) != 3 {
		t.Errorf("streams walked: %v", w.order)
	}
	e := w.entry(t, "/doc.txt")
	if len(e.Streams) != 2 || e.Streams[0].Path != "/doc.txt:com.apple.ResourceFork" || e.Streams[1].Name != "user.comment" ||
		!e.Streams[0].Stream || e.Streams[0].ICBFlags&ICBFlagStream == 0 {
		t.Fatalf("streams %+v", e.Streams)
	}
	sf, err := v.OpenStream(e.Streams[0])
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(sf); !bytes.Equal(got, rsrc.data) {
		t.Error("resource fork contents")
	}
	if _, err := v.OpenStream(e); !errors.Is(err, ErrNotStream) {
		t.Error(err)
	}
	if ds := w.entry(t, "/folder").Streams; len(ds) != 1 || ds[0].Size != 32 {
		t.Errorf("directory streams %+v", ds)
	}
	if !v.Info().HasSystemStreams {
		t.Error("system streams not recorded")
	}
	ss, err := v.SystemStreams()
	if err != nil || len(ss) != 1 || ss[0].Path != ":*UDF Unique ID Mapping Data" {
		t.Fatalf("system streams %v %+v", err, ss)
	}
	if f, err := v.OpenStream(ss[0]); err != nil {
		t.Error(err)
	} else if got, _ := io.ReadAll(f); string(got) != "uid map" {
		t.Errorf("system stream %q", got)
	}
	if _, err := v.ReadDir(e.Streams[0]); !errors.Is(err, ErrNotDirectory) {
		t.Error(err)
	}
}

func TestSymlinks(t *testing.T) {
	link := func(name string, comps ...[]byte) *unode {
		n := ufile(name, bytes.Join(comps, nil))
		n.ftype = FileTypeSymlink
		n.perms = perm755
		return n
	}
	root := udir("",
		link("abs", pathComp(2, ""), pathComp(5, "etc"), pathComp(5, "hosts")),
		link("rel", pathComp(3, ""), pathComp(3, ""), pathComp(5, "x"), pathComp(5, "y.txt")),
		link("cur", pathComp(4, ""), pathComp(5, "here")),
		link("root1", pathComp(1, "")),
		link("unicode", pathComp(5, "☃")),
		link("weird", pathComp(1, "vol"), pathComp(9, ""), pathComp(5, "a")),
	)
	v := mustOpen(t, newUB().build(root), nil)
	w := walkAll(t, v)
	for p, want := range map[string]string{"/abs": "/etc/hosts", "/rel": "../../x/y.txt", "/cur": "./here",
		"/root1": "/", "/unicode": "☃", "/weird": "/a"} {
		e := w.entry(t, p)
		if !e.IsSymlink || e.SymlinkTarget != want || e.Mode() != fs.ModeSymlink|0o755 {
			t.Errorf("%s: %q %v", p, e.SymlinkTarget, e.Mode())
		}
	}
	if !hasWarning(v, "implementation-specific root") || !hasWarning(v, "unknown type 9") {
		t.Errorf("warnings %v", v.Warnings())
	}
	if s, msgs := symlinkTarget([]byte{5, 10, 0}); s != "" || len(msgs) != 1 {
		t.Errorf("truncated: %q %v", s, msgs)
	}
}

func TestDevicesAndAttributes(t *testing.T) {
	b := newUB()
	finder := make([]byte, 48)
	copy(finder[8:], "TEXTttxt")
	special := func(name string, ft byte, eas ...[]byte) *unode {
		n := ufile(name, nil)
		n.ftype, n.eas = ft, eas
		return n
	}
	sticky := udir("tmp")
	sticky.icbFlags = ICBFlagSticky
	sticky.perms = perm755 | PermGroupWrite | PermOtherWrite
	suid := ufile("suid", []byte("x"))
	suid.icbFlags = ICBFlagSetuid | ICBFlagSetgid
	suid.perms = perm755 | PermOwnerChAttr | PermOwnerDelete // no POSIX equivalent
	mac := ufile("mac.txt", []byte("mac"))
	mac.eas = [][]byte{implEA(EAImplementationUse, "*UDF Mac FinderInfo", finder), implEA(EAApplicationUse, "*app thing", []byte("app"))}
	eaf := ufile("eafile.txt", []byte("e"))
	eaf.eas = [][]byte{implEA(EAImplementationUse, "*UDF Mac FinderInfo", finder)}
	eaf.eaFile = true
	created := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	timesEA := eaAttr(EAFileTimes, 1, append(append(binary.LittleEndian.AppendUint32(nil, 12), 1, 0, 0, 0), ts(created)...))
	withTimes := ufile("times.txt", []byte("t"))
	withTimes.eas = [][]byte{timesEA}
	root := udir("",
		special("tty", FileTypeCharDevice, devEA(4, 64)),
		special("sda", FileTypeBlockDevice, devEA(8, 0)),
		special("fifo", FileTypeFIFO),
		special("sock", FileTypeSocket),
		sticky, suid, mac, eaf, withTimes,
	)
	v := mustOpen(t, b.build(root), nil)
	noWarnings(t, v)
	w := walkAll(t, v)
	for p, want := range map[string]fs.FileMode{
		"/tty": fs.ModeDevice | fs.ModeCharDevice | 0o644, "/sda": fs.ModeDevice | 0o644, "/fifo": fs.ModeNamedPipe | 0o644,
		"/sock": fs.ModeSocket | 0o644, "/tmp": fs.ModeDir | fs.ModeSticky | 0o777, "/suid": fs.ModeSetuid | fs.ModeSetgid | 0o755,
	} {
		if m := w.entry(t, p).Mode(); m != want {
			t.Errorf("%s: mode %v, want %v", p, m, want)
		}
	}
	if e := w.entry(t, "/tty"); !e.HasDevice || e.DeviceMajor != 4 || e.DeviceMinor != 64 || len(e.ExtendedAttributes) != 1 {
		t.Errorf("tty %+v", e)
	}
	e := w.entry(t, "/mac.txt")
	if string(e.MacFinderInfo[:8]) != "TEXTttxt" || len(e.MacFinderInfo) != 32 || len(e.ExtendedAttributes) != 2 ||
		e.ExtendedAttributes[1].ImplementationID != "*app thing" || string(e.ExtendedAttributes[1].Data) != "app" ||
		e.ExtendedAttributes[1].Type != EAApplicationUse || len(e.ExtendedAttributes[0].Raw) != 12+36+48 {
		t.Errorf("mac.txt %+v", e.ExtendedAttributes)
	}
	if e := w.entry(t, "/eafile.txt"); len(e.MacFinderInfo) < 8 || string(e.MacFinderInfo[:8]) != "TEXTttxt" {
		t.Errorf("EA file: %+v", e.ExtendedAttributes)
	}
	if e := w.entry(t, "/times.txt"); e.Times.Creation == nil || !e.Times.Creation.Equal(created) {
		t.Errorf("creation time from EA: %v", e.Times.Creation)
	}
}

// Many entries sharing one large stream directory cannot multiply the
// work: the streams read per directory are bounded.
func TestStreamBudget(t *testing.T) {
	b := newUB()
	b.efe = true
	f := ufile("f", []byte("f"))
	for i := range 900 {
		f.streams = append(f.streams, ufile(fmt.Sprintf("s%03d", i), nil))
	}
	kids := []*unode{f}
	for i := range 20 {
		l := ufile(fmt.Sprintf("link%02d", i), nil)
		l.linkTo = f
		kids = append(kids, l)
	}
	v := mustOpen(t, b.build(udir("", kids...)), nil)
	w := walkAll(t, v)
	if len(w.order) != 22 || len(w.errs) != 0 || len(w.entry(t, "/f").Streams) != 900 {
		t.Errorf("walk %d entries, %v", len(w.order), w.errs)
	}
	if !hasWarning(v, "named streams not read") || len(w.entry(t, "/link19").Streams) != 0 {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestDirectoryAcrossExtents(t *testing.T) {
	b := newUB()
	var kids []*unode
	want := map[string][]byte{}
	for i := range 60 {
		n := fmt.Sprintf("a rather long file name number %03d.txt", i)
		kids = append(kids, ufile(n, []byte{byte(i)}))
		want["/d/"+n] = []byte{byte(i)}
	}
	d := udir("d", kids...)
	// A parent FID of 40 bytes and 60 of 80 bytes: three blocks, in three
	// separate extents, so identifiers straddle extent boundaries.
	d.split = []int{2048, 2048, 40 + 60*80 - 4096}
	v := mustOpen(t, b.build(udir("", d)), nil)
	noWarnings(t, v)
	checkFiles(t, walkAll(t, v), want)
}

func TestNames(t *testing.T) {
	named := func(marker byte, raw ...byte) *unode {
		n := ufile("", []byte{marker})
		n.raw = raw
		return n
	}
	empty := ufile("", []byte("e"))
	empty.raw = []byte{}
	root := udir("",
		named(1, 8, 'a', 'b'),
		named(2, 16, 0, 'c', 0xD8, 0),             // lone high surrogate
		named(3, 16, 0xD8, 0x3D, 0xDE, 0x00, 'A'), // a valid pair, then an odd byte
		named(4, 254, 'd', 'e', 'l'),              // 254/255: as 8/16
		named(5, 255, 0, 'w', 0, 'i', 0, 'd'),
		named(6, 7, 'x'), // unknown compression ID
		named(7, 8, 'a', '/', 'b'),
		named(8, 8, 'n', 0, 'l'),
		named(9, 8, '.', '.'),
		named(10, 8, 'd', 'u', 'p'),
		named(11, 8, 'd', 'u', 'p'),
		empty,
	)
	v := mustOpen(t, newUB().build(root), nil)
	w := walkAll(t, v)
	for p, want := range map[string]byte{"/ab": 1, "/c�": 2, "/😀": 3, "/del": 4, "/wid": 5, "/x": 6,
		"/a_b": 7, "/n_l": 8, "/_..": 9, "/_": 'e'} {
		if d := w.data[p]; len(d) != 1 || d[0] != want {
			t.Errorf("%s: %v (have %v)", p, d, w.order)
		}
	}
	if e := w.entry(t, "/a_b"); e.Name != "a/b" || !bytes.Equal(e.RawName, []byte{8, 'a', '/', 'b'}) {
		t.Errorf("a/b: %+v", e)
	}
	for _, msg := range []string{"surrogates", "odd length", "unknown compression ID 7", "cannot be used in a path", "duplicate name"} {
		if !hasWarning(v, msg) {
			t.Errorf("no %q warning: %v", msg, v.Warnings())
		}
	}
}

func TestTagDamage(t *testing.T) {
	b := newUB()
	b.efe = true
	f := ufile("a.txt", []byte("aaa"))
	f.feFix = func(d []byte) { d[4] ^= 0xFF } // checksum
	g := ufile("b.txt", []byte("bbb"))
	g.feFix = func(d []byte) { d[44] ^= PermOtherExec } // CRC
	h := ufile("c.txt", []byte("ccc"))
	h.fidFix = func(d []byte) { d[16] ^= 1 } // FID CRC
	k := ufile("d.txt", []byte("ddd"))
	k.feFix = func(d []byte) { binary.LittleEndian.PutUint32(d[12:], 999); retag(d) } // location
	img := b.build(udir("", f, g, h, k))
	// Destroy the anchor at 256: the one at the end is used.
	clear(img[256*2048 : 257*2048])
	// Damage the PVD's CRC.
	img[32*2048+30] ^= 1
	v := mustOpen(t, img, nil)
	checkFiles(t, walkAll(t, v), map[string][]byte{"/a.txt": []byte("aaa"), "/b.txt": []byte("bbb"),
		"/c.txt": []byte("ccc"), "/d.txt": []byte("ddd")})
	for _, msg := range []string{"/a.txt: extended file entry at block", "tag checksum mismatch", "descriptor CRC mismatch",
		"/: file identifier at block", "tag location says block 999", "primary volume descriptor at block 32: descriptor CRC"} {
		if !hasWarning(v, msg) {
			t.Errorf("no %q warning: %v", msg, v.Warnings())
		}
	}
	// The remaining anchor with a bad checksum but a sane structure is
	// tolerated.
	img2 := bytes.Clone(img)
	last := len(img2)/2048 - 1
	img2[last*2048+4] ^= 0xFF
	v = mustOpen(t, img2, nil)
	if !hasWarning(v, "anchor volume descriptor pointer at block") {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestLoopsAndLimits(t *testing.T) {
	b := newUB()
	root := udir("")
	loop := udir("loop")
	loop.linkTo = root
	aed := ufile("aed.bin", randData(12, 6*2048))
	aed.split = []int{2048, 2048, 2048, 2048, 2048, 2048}
	aed.aedPer = 2
	aed.aedLoop = true
	broken := ufile("broken.bin", randData(15, 6*2048))
	broken.split = aed.split
	broken.aedPer = 2
	hard := ufile("hard.txt", []byte("same"))
	link := ufile("link.txt", nil)
	link.linkTo = hard
	missing := ufile("missing.txt", []byte("never written"))
	missing.noFE = true
	far := ufile("far.bin", []byte("far"))
	far.feFix = func(d []byte) { binary.LittleEndian.PutUint32(d[176+4:], 0x7FFFFFF0); retag(d) }
	badRef := ufile("badref.bin", []byte("ref"))
	badRef.adType = adLong
	badRef.feFix = func(d []byte) { binary.LittleEndian.PutUint16(d[176+8:], 7); retag(d) }
	root.kids = []*unode{loop, aed, broken, hard, link, missing, far, badRef, ufile("ok.txt", []byte("ok"))}
	b.lvidNext = &extentAD{length: 2048, loc: 128}
	img := b.build(root)
	clear(img[int(broken.aedAbs[1])*2048:][:2048])
	v := mustOpen(t, img, nil)
	w := walkAll(t, v)
	if !errors.Is(w.errs["/loop"], ErrLoop) {
		t.Errorf("directory loop: %v", w.errs)
	}
	// A looping chain after the last extent loses nothing; a broken one
	// loses the extents it held.
	if !bytes.Equal(w.data["/aed.bin"], aed.data) || w.errs["/aed.bin"] != nil || !hasWarning(v, "revisit block") {
		t.Errorf("AED loop: %v, %d bytes", w.errs["/aed.bin"], len(w.data["/aed.bin"]))
	}
	if !errors.Is(w.errs["/broken.bin"], ErrCorrupt) || !bytes.Equal(w.data["/broken.bin"], broken.data[:4*2048]) {
		t.Errorf("broken AED chain: %v, %d bytes", w.errs["/broken.bin"], len(w.data["/broken.bin"]))
	}
	if !errors.Is(w.errs["/missing.txt"], ErrCorrupt) {
		t.Errorf("missing file entry: %v", w.errs["/missing.txt"])
	}
	if e := w.entries["/missing.txt"]; e != nil {
		t.Error("an entry with an error was also passed without")
	}
	if !errors.Is(w.errs["/far.bin"], ErrOutOfRange) || !errors.Is(w.errs["/badref.bin"], ErrPartition) {
		t.Errorf("bad extents: %v", w.errs)
	}
	if string(w.data["/ok.txt"]) != "ok" || string(w.data["/link.txt"]) != "same" || string(w.data["/hard.txt"]) != "same" {
		t.Errorf("intact files: %v", w.order)
	}
	if h, l := w.entry(t, "/hard.txt"), w.entry(t, "/link.txt"); h.ICB != l.ICB || h.UniqueID != l.UniqueID {
		t.Errorf("hard link ICBs %v %v", h.ICB, l.ICB)
	}
	if !hasWarning(v, "integrity sequence loops") {
		t.Errorf("warnings %v", v.Warnings())
	}
	// ReadDir reports the entries' errors too.
	ents, err := v.ReadDir(v.Root())
	if len(ents) != 9 || !errors.Is(err, ErrCorrupt) {
		t.Errorf("ReadDir: %d entries, %v", len(ents), err)
	}

	// Depth and entry limits.
	deep := udir("D9")
	for i := 8; i >= 0; i-- {
		deep = udir(fmt.Sprintf("D%d", i), deep)
	}
	img2 := newUB().build(udir("", deep))
	v = mustOpen(t, img2, &Options{MaxDepth: 5})
	w = walkAll(t, v)
	if !errors.Is(w.errs["/D0/D1/D2/D3/D4/D5"], ErrTooDeep) || len(w.errs) != 1 {
		t.Errorf("depth: %v", w.errs)
	}
	v = mustOpen(t, img2, &Options{MaxEntries: 3})
	if err := v.Walk(func(string, *Entry, error) error { return nil }); !errors.Is(err, ErrTooManyEntries) {
		t.Errorf("entries: %v", err)
	}
	if v.opts.MaxDepth != 255 || v.opts.MaxDirSize != 64<<20 {
		t.Errorf("defaults %+v", v.opts)
	}
	// Directory size and entry limits.
	var kids []*unode
	for i := range 100 {
		kids = append(kids, ufile(fmt.Sprintf("f%03d", i), nil))
	}
	img3 := newUB().build(udir("", kids...))
	v = mustOpen(t, img3, &Options{MaxDirSize: 1000})
	ents, err = v.ReadDir(v.Root())
	if !errors.Is(err, ErrCorrupt) || len(ents) == 0 || len(ents) >= 100 {
		t.Errorf("dir size limit: %d entries, %v", len(ents), err)
	}
	v = mustOpen(t, img3, &Options{MaxEntries: 10})
	if ents, _ := v.ReadDir(v.Root()); len(ents) != 10 || !hasWarning(v, "more than 10 entries") {
		t.Errorf("entry limit: %d entries, %v", len(ents), v.Warnings())
	}
}

func TestDamagedDirectory(t *testing.T) {
	b := newUB()
	var kids []*unode
	for i := range 150 {
		kids = append(kids, ufile(fmt.Sprintf("file%03d.txt", i), []byte("x")))
	}
	big := udir("big", kids...)
	ok := ufile("ok.txt", []byte("ok"))
	img := b.build(udir("", big, ok))
	bad := failingReader{r: bytes.NewReader(img), bad: map[int64]bool{int64(big.dataAbs[0]) + 1: true, int64(ok.feAbs): true}}
	v, err := Open(bad, int64(len(img)), nil)
	if err != nil {
		t.Fatal(err)
	}
	w := walkAll(t, v)
	if !errors.Is(w.errs["/big"], errInjected) || !errors.Is(w.errs["/ok.txt"], errInjected) {
		t.Errorf("errors %v", w.errs)
	}
	n := 0
	for p := range w.entries {
		if strings.HasPrefix(p, "/big/") {
			n++
		}
	}
	if n < 50 || n >= 150 {
		t.Errorf("salvaged %d of 150 entries", n)
	}
	if !hasWarning(v, "unparseable directory data") {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestWalkSkip(t *testing.T) {
	img := newUB().build(udir("",
		udir("A", ufile("A1", nil), ufile("A2", nil)),
		udir("B", ufile("B1", nil), ufile("B2", nil)),
		ufile("C", nil)))
	v := mustOpen(t, img, nil)
	var seen []string
	err := v.Walk(func(p string, e *Entry, err error) error {
		seen = append(seen, p)
		switch p {
		case "/A":
			return fs.SkipDir
		case "/B/B1":
			return fs.SkipDir
		case "/C":
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || fmt.Sprint(seen) != "[/ /A /B /B/B1 /C]" {
		t.Errorf("seen %v, err %v", seen, err)
	}
	stop := errors.New("stop")
	if err := v.Walk(func(p string, e *Entry, err error) error { return stop }); err != stop {
		t.Errorf("error not returned: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(nil, 0, nil); !errors.Is(err, ErrNilReader) {
		t.Error(err)
	}
	zero := make([]byte, 600*2048)
	if _, err := Open(bytes.NewReader(zero), int64(len(zero)), nil); !errors.Is(err, ErrNotUDF) {
		t.Error(err)
	}
	if _, err := Open(bytes.NewReader(zero[:100]), 100, nil); !errors.Is(err, ErrNotUDF) {
		t.Error(err)
	}
	if _, err := Open(bytes.NewReader(zero), int64(len(zero)), &Options{BlockSize: 1000}); err == nil {
		t.Error("invalid block size accepted")
	}
	vrs := bytes.Clone(zero)
	copy(vrs[32768:], "\x00BEA01\x01")
	copy(vrs[34816:], "\x00NSR03\x01")
	copy(vrs[36864:], "\x00TEA01\x01")
	if _, err := Open(bytes.NewReader(vrs), int64(len(vrs)), nil); !errors.Is(err, ErrNoAnchor) {
		t.Error(err)
	}
	// No logical volume descriptor in either sequence.
	img := newUB().build(udir("", ufile("a", nil)))
	for _, at := range []int{35, 51} {
		clear(img[at*2048 : at*2048+16])
	}
	if _, err := Open(bytes.NewReader(img), int64(len(img)), nil); !errors.Is(err, ErrNoVolume) {
		t.Error(err)
	}
	// No file set descriptor.
	img = newUB().build(udir("", ufile("a", nil)))
	clear(img[272*2048 : 273*2048])
	if _, err := Open(bytes.NewReader(img), int64(len(img)), nil); !errors.Is(err, ErrNoFileSet) {
		t.Error(err)
	}
	// Main volume descriptor sequence destroyed: the reserve is used.
	img = newUB().build(udir("", ufile("a", []byte("a"))))
	clear(img[32*2048 : 48*2048])
	v := mustOpen(t, img, nil)
	if !hasWarning(v, "reserve") {
		t.Errorf("warnings %v", v.Warnings())
	}
	checkFiles(t, walkAll(t, v), map[string][]byte{"/a": []byte("a")})
	// Unknown size: the anchor at 256 suffices.
	v, err := Open(bytes.NewReader(img), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkFiles(t, walkAll(t, v), map[string][]byte{"/a": []byte("a")})
	// No volume recognition sequence: a warning only.
	b := newUB()
	b.noVRS = true
	v = mustOpen(t, b.build(udir("", ufile("a", nil))), nil)
	if !hasWarning(v, "no NSR02/NSR03") || v.Info().NSR != 0 {
		t.Errorf("warnings %v", v.Warnings())
	}
	// Unreadable root directory: Open succeeds, the walk reports it.
	r := udir("", ufile("a", nil))
	img = newUB().build(r)
	clear(img[int(r.feAbs)*2048:][:2048])
	v = mustOpen(t, img, nil)
	var rootErr error
	if err := v.Walk(func(p string, e *Entry, err error) error { rootErr = err; return nil }); err != nil {
		t.Error(err)
	}
	if !errors.Is(rootErr, ErrCorrupt) {
		t.Errorf("root error %v", rootErr)
	}
	if _, err := v.ReadDir(v.Root()); !errors.Is(err, ErrCorrupt) {
		t.Errorf("ReadDir root: %v", err)
	}
}

func TestBlockSizesAndSessions(t *testing.T) {
	for _, bs := range []int{512, 1024, 4096} {
		b := newUB()
		b.bs = bs
		big := ufile("big.bin", randData(13, 3*bs+7))
		img := b.build(udir("", big, udir("d", ufile("x", []byte("x")))))
		v := mustOpen(t, img, nil)
		noWarnings(t, v)
		if v.BlockSize() != bs {
			t.Errorf("block size %d, want %d", v.BlockSize(), bs)
		}
		checkFiles(t, walkAll(t, v), map[string][]byte{"/big.bin": big.data, "/d/x": []byte("x")})
		if _, err := Open(bytes.NewReader(img), int64(len(img)), &Options{BlockSize: 2048}); err == nil {
			t.Errorf("%d-byte volume opened with 2048-byte blocks", bs)
		}
	}
	// A last session starting at block 1000: its anchor is at 1256.
	b := newUB()
	b.session = 1000
	b.tailAVDP = false
	img := b.build(udir("", ufile("s", []byte("session"))))
	img = append(img, make([]byte, 10*2048)...)
	if _, err := Open(bytes.NewReader(img), int64(len(img)), nil); err == nil {
		t.Error("opened without SessionStart")
	}
	v := mustOpen(t, img, &Options{SessionStart: 1000})
	noWarnings(t, v)
	checkFiles(t, walkAll(t, v), map[string][]byte{"/s": []byte("session")})
}

func TestTimestamps(t *testing.T) {
	enc := func(typ uint16, off int, y int, rest ...byte) []byte {
		b := make([]byte, 12)
		binary.LittleEndian.PutUint16(b, typ<<12|uint16(int16(off))&0xFFF)
		binary.LittleEndian.PutUint16(b[2:], uint16(y))
		copy(b[4:], rest)
		return b
	}
	for i, c := range []struct {
		b    []byte
		want string // RFC 3339 with nanoseconds, "" for nil
		warn bool
	}{
		{make([]byte, 12), "", false},
		{enc(1, 0, 0, 1, 1), "", false},
		{enc(1, -2047, 2001, 2, 3, 4, 5, 6, 7, 8, 9), "2001-02-03T04:05:06.070809Z", false},
		{enc(1, 90, 2001, 2, 3, 4, 5, 6), "2001-02-03T04:05:06+01:30", false},
		{enc(1, -300, 2001, 2, 3, 4, 5, 6), "2001-02-03T04:05:06-05:00", false},
		{enc(0, 300, 2001, 2, 3, 4, 5, 6), "2001-02-03T04:05:06Z", false},
		{enc(1, 2000, 2001, 2, 3, 4, 5, 6), "2001-02-03T04:05:06Z", true},
		{enc(1, 0, 2001, 2, 30, 4, 5, 6), "", true},
		{enc(1, 0, 2001, 13, 3, 4, 5, 6), "", true},
		{enc(1, 0, 2001, 2, 3, 4, 5, 6, 100), "2001-02-03T04:05:06Z", true},
		{enc(5, 0, 2001, 2, 3, 4, 5, 6), "2001-02-03T04:05:06Z", true},
		{enc(1, 0, -5, 2, 3, 4, 5, 6), "", true},
		{[]byte{1}, "", true},
	} {
		tm, msg := timestamp(c.b)
		got := ""
		if tm != nil {
			got = tm.Format(time.RFC3339Nano)
		}
		if got != c.want || (msg != "") != c.warn {
			t.Errorf("%d: %q %q, want %q warn %v", i, got, msg, c.want, c.warn)
		}
	}
}

func TestStrings(t *testing.T) {
	for i, c := range []struct {
		b    []byte
		want string
		warn bool
	}{
		{dstr("Volume", 32), "Volume", false},
		{dstr("Grüße ☃", 32), "Grüße ☃", false},
		{make([]byte, 32), "", false},
		{append([]byte{8, 'a', 'b', 'c'}, make([]byte, 28)...), "abc", true},        // length byte 0
		{append([]byte{8, 'a', 'b'}, append(make([]byte, 28), 200)...), "ab", true}, // length too long
		{[]byte{0}, "", false},
	} {
		got, msg := dstring(c.b)
		if got != c.want || (msg != "") != c.warn {
			t.Errorf("%d: %q %q", i, got, msg)
		}
	}
	if r := parseRegid(regidBytes("*OSTA UDF Compliant", 0x50, 0x02)); r.id != "*OSTA UDF Compliant" || r.udfRevision() != 0x0250 {
		t.Errorf("regid %+v", r)
	}
	// CRC-ITU-T as ECMA-167 specifies it is CRC-16/XMODEM: check value 0x31C3.
	if c := crcITU([]byte("123456789")); c != 0x31C3 {
		t.Errorf("crc %#x", c)
	}
}

// seedImages returns small volumes covering the features, for fuzzing.
func seedImages() [][]byte {
	var out [][]byte
	mk := func(mod func(b *ubuilder), root *unode) {
		b := newUB()
		b.bs = 512
		b.partStart = 280
		if mod != nil {
			mod(b)
		}
		out = append(out, b.build(root))
	}
	link := ufile("l", bytes.Join([][]byte{pathComp(2, ""), pathComp(5, "x")}, nil))
	link.ftype = FileTypeSymlink
	mac := ufile("m", []byte("m"))
	mac.eas = [][]byte{implEA(EAImplementationUse, "*UDF Mac FinderInfo", make([]byte, 48)), devEA(1, 2)}
	chained := ufile("c", randData(14, 6*512))
	chained.split = []int{512, 512, 512, 512, 512, 512}
	chained.aedPer = 2
	mk(func(b *ubuilder) { b.efe, b.ad = true, adLong }, udir("", link, mac, chained, udir("d", ufile("x", []byte("x")))))
	s := ufile("s", []byte("s"))
	s.streams = []*unode{ufile("st", []byte("stream"))}
	st := ufile("t", []byte("t"))
	st.strategy = true
	mk(func(b *ubuilder) { b.rev, b.efe, b.kind, b.mdLen = 0x0250, true, kindMetadata, 16 }, udir("", s, st))
	mk(func(b *ubuilder) { b.rev, b.kind = 0x0150, kindVAT150 }, udir("", ufile("v", []byte("v"))))
	mk(func(b *ubuilder) { b.kind, b.packetLen, b.spare = kindSparable, 8, []uint32{0} }, udir("", ufile("p", []byte("p"))))
	return out
}

func FuzzOpenWalk(f *testing.F) {
	for _, s := range seedImages() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, img []byte) {
		v, err := Open(bytes.NewReader(img), int64(len(img)), &Options{MaxEntries: 5000, MaxDirSize: 1 << 20})
		if err != nil {
			return
		}
		read := func(e *Entry) {
			if fl, err := v.OpenFile(e); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(fl, 1<<20))
			}
		}
		_ = v.Walk(func(p string, e *Entry, err error) error {
			if err != nil || e.IsDir {
				return nil
			}
			read(e)
			for _, s := range e.Streams {
				read(s)
			}
			_ = e.Mode()
			return nil
		})
		if ss, _ := v.SystemStreams(); len(ss) > 0 {
			read(ss[0])
		}
		_ = v.Info()
		_ = v.Warnings()
	})
}

// Mutating seed images byte by byte must never panic.
func TestMutations(t *testing.T) {
	r := rand.New(rand.NewPCG(8, 8))
	n := 300
	if testing.Short() {
		n = 50
	}
	for _, seed := range seedImages() {
		for range n {
			img := bytes.Clone(seed)
			for range 1 + r.IntN(20) {
				img[r.IntN(len(img))] = byte(r.Uint32())
			}
			v, err := Open(bytes.NewReader(img), int64(len(img)), &Options{MaxEntries: 5000, MaxDirSize: 1 << 20})
			if err != nil {
				continue
			}
			_ = v.Walk(func(p string, e *Entry, err error) error {
				if err == nil && !e.IsDir {
					if fl, err := v.OpenFile(e); err == nil {
						_, _ = io.Copy(io.Discard, io.LimitReader(fl, 1<<20))
					}
				}
				return nil
			})
		}
	}
}
