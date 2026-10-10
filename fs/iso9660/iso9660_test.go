package iso9660

import (
	"bytes"
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
		if e.Associated {
			p += "#assoc"
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

func TestPlainISO(t *testing.T) {
	readme := []byte("hello, world\n")
	nested := randData(1, 5000)
	b := newBuilder()
	img := b.build(dir("",
		file("DATA.;1", []byte("no extension")),
		file("README.TXT;1", readme),
		dir("SUBDIR", file("NESTED.BIN;1", nested), dir("EMPTY")),
	))
	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	if got := v.Namespaces(); len(got) != 1 || got[0] != NamespaceISO9660 || v.Namespace() != NamespaceISO9660 {
		t.Fatalf("namespaces %v, chosen %v", got, v.Namespace())
	}
	info := v.Info()
	if info.VolumeID != "TESTVOL" || info.SystemID != "TESTSYS" || info.ApplicationID != "GO-AARU TEST" ||
		info.PublisherID != "" || info.LogicalBlockSize != 2048 || info.VolumeSpaceSize != b.next {
		t.Errorf("info %+v", info)
	}
	want := time.Date(2001, 2, 3, 4, 5, 6, 0, time.FixedZone("", 3600))
	if info.Creation == nil || !info.Creation.Equal(want) {
		t.Errorf("creation %v, want %v", info.Creation, want)
	} else if _, off := info.Creation.Zone(); off != 3600 {
		t.Errorf("creation zone offset %d", off)
	}
	if info.Modification != nil || info.Expiration != nil || info.Effective != nil {
		t.Errorf("unset dates parsed: %v %v %v", info.Modification, info.Expiration, info.Effective)
	}
	w := walkAll(t, v)
	if len(w.errs) > 0 {
		t.Fatal(w.errs)
	}
	wantOrder := []string{"/", "/DATA", "/README.TXT", "/SUBDIR", "/SUBDIR/NESTED.BIN", "/SUBDIR/EMPTY"}
	if fmt.Sprint(w.order) != fmt.Sprint(wantOrder) {
		t.Errorf("order %v, want %v", w.order, wantOrder)
	}
	if !bytes.Equal(w.data["/README.TXT"], readme) || !bytes.Equal(w.data["/SUBDIR/NESTED.BIN"], nested) ||
		string(w.data["/DATA"]) != "no extension" {
		t.Error("file contents differ")
	}
	e := w.entry(t, "/README.TXT")
	if e.Version != 1 || string(e.RawName) != "README.TXT;1" || e.Size != int64(len(readme)) || e.IsDir {
		t.Errorf("entry %+v", e)
	}
	wantRec := time.Date(1999, 12, 31, 23, 59, 58, 0, time.UTC)
	if e.RecordedTime == nil || !e.RecordedTime.Equal(wantRec) || !e.ModTime().Equal(wantRec) {
		t.Errorf("recorded %v", e.RecordedTime)
	}
	if e.Mode() != 0o444 || w.entry(t, "/SUBDIR").Mode() != fs.ModeDir|0o555 {
		t.Errorf("modes %v %v", e.Mode(), w.entry(t, "/SUBDIR").Mode())
	}
	if _, err := v.ReadDir(e); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("ReadDir(file) = %v", err)
	}
	if _, err := v.OpenFile(w.entry(t, "/SUBDIR")); !errors.Is(err, ErrIsDirectory) {
		t.Errorf("OpenFile(dir) = %v", err)
	}
	if d := v.Descriptors(); len(d) != 2 || d[0].Type != DescriptorPrimary || d[1].Type != DescriptorTerminator {
		t.Errorf("descriptors %+v", d)
	}
	if _, ok := v.Joliet(); ok {
		t.Error("Joliet reported on a plain volume")
	}

	// Seek/Read/ReadAt behave like an io.ReadSeeker.
	f, err := v.OpenFile(w.entry(t, "/SUBDIR/NESTED.BIN"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(-100, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail, _ := io.ReadAll(f)
	if !bytes.Equal(tail, nested[len(nested)-100:]) {
		t.Error("tail after Seek differs")
	}
	buf := make([]byte, 10)
	if n, err := f.ReadAt(buf, int64(len(nested))-5); n != 5 || err != io.EOF {
		t.Errorf("ReadAt at the end: %d, %v", n, err)
	}
}

func TestDescriptorDatesAndLabels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		date  []byte
		warns bool
	}{
		{"nul", make([]byte, 17), false},
		{"zeros", []byte("0000000000000000\x00"), false},
		{"garbage", []byte("20X1020304050600\x00"), true},
		{"bad month", []byte("2001130304050600\x00"), true},
		{"feb 30", []byte("2001023004050600\x00"), true},
		{"bad offset", []byte("2001020304050600\x7f"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBuilder()
			for i := range b.dates {
				copy(b.dates[i][:], tc.date)
			}
			b.pvdHook = func(d []byte) {
				copy(d[40:72], pad("LABEL", 32, 0)) // NUL padding
				copy(d[8:40], pad("SYS", 32, 0))
				copy(d[318:446], pad("PUB  ", 128, 0)) // spaces then NULs
			}
			v := mustOpen(t, b.build(dir("", file("A.TXT;1", []byte("a")))), nil)
			info := v.Info()
			if info.VolumeID != "LABEL" || info.SystemID != "SYS" || info.PublisherID != "PUB" {
				t.Errorf("labels %q %q %q", info.VolumeID, info.SystemID, info.PublisherID)
			}
			if tc.name == "bad offset" {
				if info.Creation == nil || info.Creation.Location() != time.UTC {
					t.Errorf("creation %v", info.Creation)
				}
			} else if info.Creation != nil || info.Modification != nil {
				t.Errorf("dates %v %v", info.Creation, info.Modification)
			}
			if got := len(v.Warnings()) > 0; got != tc.warns {
				t.Errorf("warnings %v, want any=%v", v.Warnings(), tc.warns)
			}
			if w := walkAll(t, v); string(w.data["/A.TXT"]) != "a" {
				t.Error("volume not readable")
			}
		})
	}
}

func TestJoliet(t *testing.T) {
	names := []string{"Ünïcödé 日本語.txt", "emoji 🎉 file.dat", "a.very.long.name.with.many.dots-and spaces.text"}
	b := newBuilder()
	b.joliet = true
	var kids []*node
	for i, n := range names {
		f := file(fmt.Sprintf("FILE%d.TXT;1", i), []byte(n))
		f.jname = n + ";1"
		kids = append(kids, f)
	}
	sub := dir("SUB_DIR", file("X.BIN;1", []byte("x")))
	sub.jname = "Sub Dir"
	kids = append(kids, sub)
	img := b.build(dir("", kids...))

	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	if v.Namespace() != NamespaceJoliet || fmt.Sprint(v.Namespaces()) != "[ISO 9660 Joliet]" {
		t.Fatalf("namespace %v of %v", v.Namespace(), v.Namespaces())
	}
	j, ok := v.Joliet()
	if !ok || j.JolietLevel != 3 || j.VolumeID != "Test Volume" || j.SystemID != "TESTSYS" {
		t.Errorf("joliet info %+v", j)
	}
	if v.Info().VolumeID != "TESTVOL" {
		t.Errorf("Info should be the PVD's: %q", v.Info().VolumeID)
	}
	w := walkAll(t, v)
	for _, n := range names {
		e := w.entry(t, "/"+n)
		if e.Version != 1 || string(w.data["/"+n]) != n {
			t.Errorf("%q: version %d data %q", n, e.Version, w.data["/"+n])
		}
	}
	if string(w.data["/Sub Dir/x.bin"]) != "x" {
		t.Errorf("Sub Dir/x.bin missing: %v", w.order)
	}

	// The PVD tree stays reachable.
	v = mustOpen(t, img, &Options{Namespace: NamespaceISO9660})
	w = walkAll(t, v)
	if string(w.data["/SUB_DIR/X.BIN"]) != "x" || string(w.data["/FILE0.TXT"]) != names[0] {
		t.Errorf("ISO view: %v", w.order)
	}
	if _, err := Open(bytes.NewReader(img), int64(len(img)), &Options{Namespace: NamespaceRockRidge}); !errors.Is(err, ErrNamespaceUnavailable) {
		t.Errorf("Rock Ridge requested: %v", err)
	}
}

// date7 encodes a short (7-byte) date.
func date7(y, mo, d, h, mi, s int, off int8) []byte {
	return []byte{byte(y - 1900), byte(mo), byte(d), byte(h), byte(mi), byte(s), byte(off)}
}

func date17(s string, off int8) []byte { return append([]byte(s), byte(off)) }

func TestRockRidge(t *testing.T) {
	b := newBuilder()
	longName := strings.Repeat("long-name-", 30) + "end.txt" // 307 bytes: needs CE areas
	// The long name is split: part in the record, the rest across two
	// chained continuation areas.
	ce2 := b.reserve(cat(nmEntry(0, longName[200:])))
	ce1Data := cat(nmEntry(1, longName[100:200]), ceEntry(ce2, 0, uint32(len(nmEntry(0, longName[200:])))))
	// Put the first area at an offset inside its block.
	ce1Block := b.reserve(append(make([]byte, 100), ce1Data...))

	data := randData(2, 3000)
	root := dir("",
		&node{id: "LONGNAME.TXT;1", data: []byte("long"), su: fixed(cat(
			nmEntry(1, longName[:100]), pxEntry(0o100644, 1, 0, 0),
			ceEntry(ce1Block, 100, uint32(len(ce1Data)))))},
		&node{id: "FILE.TXT;1", data: data, su: fixed(cat(
			nmEntry(1, "Mixed"), nmEntry(0, "Case.txt"),
			pxEntry(0o104755, 1, 1000, 100, 77),
			tfEntry(0x0F, date7(2010, 6, 7, 8, 9, 10, -20), date7(2011, 1, 2, 3, 4, 5, 0),
				date7(2012, 1, 1, 0, 0, 0, 0), date7(2013, 1, 1, 0, 0, 0, 0))))},
		&node{id: "LONGTF.;1", data: []byte("x"), su: fixed(cat(
			nmEntry(0, "longtf"),
			tfEntry(0x80|0x02|0x40, date17("2020010203040599", 8), date17("2030123123595900", -4))))},
		&node{id: "NONAME.TXT;1", data: []byte("fallback"), su: fixed(pxEntry(0o100600, 1, 5, 6))},
		&node{id: "PLAIN.TXT;1", data: []byte("no susp at all")},
		&node{id: "LINK.;1", su: fixed(cat(nmEntry(0, "link"), pxEntry(0o120777, 1, 0, 0),
			slEntry(1, slComp{0x08, ""}, slComp{0, "usr"}, slComp{0x01, "sh"}),
			slEntry(0, slComp{0, "are"}, slComp{0x04, ""}, slComp{0x02, ""}, slComp{0, "x"})))},
		&node{id: "RELLINK.;1", su: fixed(cat(nmEntry(0, "rel"), pxEntry(0o120777, 1, 0, 0),
			slEntry(0, slComp{0x04, ""}, slComp{0, "a"}, slComp{0, "b"})))},
		&node{id: "DEV.;1", su: fixed(cat(nmEntry(0, "sda"), pxEntry(0o060660, 1, 0, 6), pnEntry(0, 0x0800)))},
		&node{id: "FIFO.;1", su: fixed(cat(nmEntry(0, "fifo"), pxEntry(0o010644, 1, 0, 0)))},
		&node{id: "SOCK.;1", su: fixed(cat(nmEntry(0, "sock"), pxEntry(0o140755, 1, 0, 0)))},
		&node{id: "CHR.;1", su: fixed(cat(nmEntry(0, "null"), pxEntry(0o020666, 1, 0, 0), pnEntry(1, 3)))},
		&node{id: "D", dir: true, su: fixed(cat(nmEntry(0, "dir"), pxEntry(0o041777, 2, 0, 0))),
			kids: []*node{{id: "IN.TXT;1", data: []byte("in"), su: fixed(nmEntry(0, "inside.txt"))}}},
	)
	root.dotSU = rrRoot(pxEntry(0o040755, 3, 0, 0))
	img := b.build(root)
	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	if v.Namespace() != NamespaceRockRidge {
		t.Fatalf("namespace %v of %v", v.Namespace(), v.Namespaces())
	}
	if x := v.Extensions(); len(x) != 1 || x[0].ID != "RRIP_1991A" || x[0].Descriptor != "ROCK RIDGE" {
		t.Errorf("extensions %+v", x)
	}
	w := walkAll(t, v)
	if len(w.errs) > 0 {
		t.Fatal(w.errs)
	}
	if r := w.entry(t, "/").RockRidge; r == nil || r.Mode != fs.ModeDir|0o755 || r.Nlink != 3 {
		t.Errorf("root rr %+v", r)
	}
	if string(w.data["/"+longName]) != "long" {
		t.Errorf("long name across CE areas not found: %v", w.order)
	}
	e := w.entry(t, "/MixedCase.txt")
	rr := e.RockRidge
	if !bytes.Equal(w.data["/MixedCase.txt"], data) || rr.UID != 1000 || rr.GID != 100 || !rr.HasInode ||
		rr.Inode != 77 || rr.Mode != fs.ModeSetuid|0o755 || rr.PosixMode != 0o104755 || e.Mode() != rr.Mode {
		t.Errorf("rr %+v", rr)
	}
	if ct := rr.Times.Creation; ct == nil || !ct.Equal(time.Date(2010, 6, 7, 8, 9, 10, 0, time.FixedZone("", -5*3600))) {
		t.Errorf("creation %v", ct)
	}
	if _, off := rr.Times.Creation.Zone(); off != -5*3600 {
		t.Errorf("creation zone %d", off)
	}
	if mt := rr.Times.Modify; mt == nil || !mt.Equal(time.Date(2011, 1, 2, 3, 4, 5, 0, time.UTC)) || !e.ModTime().Equal(*mt) {
		t.Errorf("modify %v", mt)
	}
	if rr.Times.Access == nil || rr.Times.Attributes == nil || rr.Times.Backup != nil {
		t.Errorf("times %+v", rr.Times)
	}
	lt := w.entry(t, "/longtf").RockRidge.Times
	if lt.Modify == nil || !lt.Modify.Equal(time.Date(2020, 1, 2, 3, 4, 5, 990_000_000, time.FixedZone("", 7200))) ||
		lt.Effective == nil || lt.Effective.Year() != 2030 || lt.Creation != nil {
		t.Errorf("long times %+v", lt)
	}
	nn := w.entry(t, "/NONAME.TXT")
	if string(w.data["/NONAME.TXT"]) != "fallback" || nn.RockRidge == nil || nn.RockRidge.Name != "" || nn.RockRidge.UID != 5 {
		t.Errorf("entry without NM: %+v", nn.RockRidge)
	}
	if p := w.entry(t, "/PLAIN.TXT"); p.RockRidge != nil || string(w.data["/PLAIN.TXT"]) != "no susp at all" {
		t.Errorf("plain entry rr %+v", p.RockRidge)
	}
	if l := w.entry(t, "/link").RockRidge; !l.HasSymlink || l.SymlinkTarget != "/usr/share/.././x" || l.Mode != fs.ModeSymlink|0o777 {
		t.Errorf("symlink %+v", l)
	}
	if l := w.entry(t, "/rel").RockRidge; l.SymlinkTarget != "../a/b" {
		t.Errorf("relative symlink %q", l.SymlinkTarget)
	}
	if d := w.entry(t, "/sda").RockRidge; !d.HasDevice || d.DeviceLow != 0x800 || d.Mode != fs.ModeDevice|0o660 {
		t.Errorf("device %+v", d)
	}
	if d := w.entry(t, "/null").RockRidge; d.DeviceHigh != 1 || d.DeviceLow != 3 || d.Mode.Type() != fs.ModeDevice|fs.ModeCharDevice {
		t.Errorf("char device %+v", d)
	}
	if w.entry(t, "/fifo").Mode().Type() != fs.ModeNamedPipe || w.entry(t, "/sock").Mode().Type() != fs.ModeSocket {
		t.Error("fifo/socket modes")
	}
	if d := w.entry(t, "/dir"); d.Mode() != fs.ModeDir|fs.ModeSticky|0o777 || string(w.data["/dir/inside.txt"]) != "in" {
		t.Errorf("dir %v %v", d.Mode(), w.order)
	}

	// The plain view ignores SUSP.
	v = mustOpen(t, img, &Options{Namespace: NamespaceISO9660})
	w = walkAll(t, v)
	if e := w.entry(t, "/FILE.TXT"); e.RockRidge != nil || w.entry(t, "/").RockRidge != nil {
		t.Error("plain view carries Rock Ridge")
	}
	if string(w.data["/D/IN.TXT"]) != "in" {
		t.Errorf("plain view: %v", w.order)
	}
}

// A disc like macOS hdiutil writes: SP, ER and PX/TF entries but no NM, with
// Joliet. The default namespace must be Joliet, and Rock Ridge (with ISO
// names) must still read.
func TestRockRidgeWithoutNames(t *testing.T) {
	for _, joliet := range []bool{true, false} {
		b := newBuilder()
		b.joliet = joliet
		sub := dir("SUB_DIR", &node{id: "BLOB.BIN;1", jname: "blob.bin", data: []byte("blob"),
			su: fixed(pxEntry(0o100644, 1, 501, 0))})
		sub.jname = "Sub Dir"
		sub.su = fixed(pxEntry(0o040755, 2, 501, 0))
		root := dir("", &node{id: "README.TXT;1", jname: "readme.txt", data: []byte("hi"),
			su: fixed(cat(suspEntry("AA", 2, []byte("TEXTttxt"), []byte{0x01, 0x00}),
				pxEntry(0o100644, 1, 501, 0), tfEntry(0x02, date7(2004, 5, 1, 19, 0, 0, 0))))}, sub)
		root.dotSU = fixed(cat(spEntry(0), erEntry("IEEE_P1282", "THE IEEE P1282 PROTOCOL", "IEEE")))
		v := mustOpen(t, b.build(root), nil)
		noWarnings(t, v)
		w := walkAll(t, v)
		if joliet {
			if v.Namespace() != NamespaceJoliet {
				t.Fatalf("namespace %v", v.Namespace())
			}
			if string(w.data["/readme.txt"]) != "hi" || string(w.data["/Sub Dir/blob.bin"]) != "blob" {
				t.Errorf("joliet names: %v", w.order)
			}
			continue
		}
		if v.Namespace() != NamespaceRockRidge {
			t.Fatalf("namespace %v", v.Namespace())
		}
		e := w.entry(t, "/README.TXT")
		if e.RockRidge == nil || e.RockRidge.UID != 501 || !e.ModTime().Equal(time.Date(2004, 5, 1, 19, 0, 0, 0, time.UTC)) {
			t.Errorf("rr %+v", e.RockRidge)
		}
		if e.Apple == nil || string(e.Apple.Type[:]) != "TEXT" || string(e.Apple.Creator[:]) != "ttxt" || e.Apple.FinderFlags != 0x100 {
			t.Errorf("apple %+v", e.Apple)
		}
		if string(w.data["/SUB_DIR/BLOB.BIN"]) != "blob" {
			t.Errorf("subdirectory: %v %v", w.order, w.errs)
		}
	}
}

func TestRelocatedDirectory(t *testing.T) {
	b := newBuilder()
	deepFile := &node{id: "F.TXT;1", data: []byte("deep file"), su: fixed(nmEntry(0, "f.txt"))}
	deep := &node{id: "DEEP", dir: true, kids: []*node{deepFile},
		su: fixed(cat(nmEntry(0, "deep"), reEntry()))}
	var bDir *node
	deep.dotSU = func(*builder) []byte { return cat(pxEntry(0o040700, 2, 42, 43)) }
	deep.dotdotSU = func(*builder) []byte {
		lba := uint32(0)
		if bDir != nil && len(bDir.lba) > 0 {
			lba = bDir.lba[0]
		}
		return plEntry(lba)
	}
	placeholder := &node{id: "DEEP.;1", su: func(*builder) []byte {
		lba := uint32(0)
		if len(deep.lba) > 0 {
			lba = deep.lba[0]
		}
		return cat(nmEntry(0, "deep"), pxEntry(0o100644, 1, 0, 0), clEntry(lba))
	}}
	bDir = &node{id: "B", dir: true, kids: []*node{placeholder}, su: fixed(nmEntry(0, "b"))}
	root := dir("",
		&node{id: "A", dir: true, kids: []*node{bDir}, su: fixed(nmEntry(0, "a"))},
		&node{id: "RR_MOVED", dir: true, kids: []*node{deep}, su: fixed(nmEntry(0, "rr_moved"))},
	)
	root.dotSU = rrRoot()
	img := b.build(root)

	v := mustOpen(t, img, nil)
	noWarnings(t, v)
	w := walkAll(t, v)
	if len(w.errs) > 0 {
		t.Fatal(w.errs)
	}
	d := w.entry(t, "/a/b/deep")
	if !d.IsDir || !d.Relocated || d.RockRidge.UID != 42 || d.Mode() != fs.ModeDir|0o700 || !d.RockRidge.HasChildLink {
		t.Errorf("relocated dir %+v rr %+v", d, d.RockRidge)
	}
	if string(w.data["/a/b/deep/f.txt"]) != "deep file" {
		t.Errorf("relocated contents: %v", w.order)
	}
	if ents, _ := v.ReadDir(w.entry(t, "/rr_moved")); len(ents) != 0 {
		t.Errorf("rr_moved lists %d entries", len(ents))
	}

	// ShowRelocated lists the directory in rr_moved too; Walk reaches it
	// twice and reports the second visit.
	v = mustOpen(t, img, &Options{ShowRelocated: true})
	w = walkAll(t, v)
	if !errors.Is(w.errs["/rr_moved/deep"], ErrLoop) {
		t.Errorf("errors %v", w.errs)
	}
	// The plain view shows the physical layout.
	v = mustOpen(t, img, &Options{Namespace: NamespaceISO9660})
	w = walkAll(t, v)
	if string(w.data["/RR_MOVED/DEEP/F.TXT"]) != "deep file" || w.entry(t, "/A/B/DEEP").IsDir {
		t.Errorf("plain view: %v", w.order)
	}
}

func TestMultiExtent(t *testing.T) {
	data := randData(3, 4096+2048+6144+1000)
	b := newBuilder()
	b.joliet = true
	f := file("BIG.BIN;1", data)
	f.split = []int{4096, 2048, 6144, 1000}
	img := b.build(dir("", f, file("AFTER.TXT;1", []byte("after"))))
	for _, ns := range []Namespace{NamespaceISO9660, NamespaceJoliet} {
		v := mustOpen(t, img, &Options{Namespace: ns})
		noWarnings(t, v)
		w := walkAll(t, v)
		name := "/BIG.BIN"
		if ns == NamespaceJoliet {
			name = "/big.bin"
		}
		e := w.entry(t, name)
		if len(e.Extents) != 4 || e.Size != int64(len(data)) || !e.MultiExtent {
			t.Errorf("%v: extents %+v size %d", ns, e.Extents, e.Size)
		}
		if !bytes.Equal(w.data[name], data) {
			t.Errorf("%v: data differs", ns)
		}
		if len(w.order) != 3 {
			t.Errorf("%v: entries %v", ns, w.order)
		}
		// Reads crossing extent boundaries.
		fl, _ := v.OpenFile(e)
		buf := make([]byte, 3000)
		if _, err := fl.ReadAt(buf, 3000); err != nil || !bytes.Equal(buf, data[3000:6000]) {
			t.Errorf("%v: ReadAt across extents: %v", ns, err)
		}
	}
}

func TestExtendedAttributeRecord(t *testing.T) {
	data := randData(4, 3000)
	b := newBuilder()
	f := file("EAR.BIN;1", data)
	f.ear = 2
	v := mustOpen(t, b.build(dir("", f)), nil)
	w := walkAll(t, v)
	e := w.entry(t, "/EAR.BIN")
	if !bytes.Equal(w.data["/EAR.BIN"], data) || e.Extents[0].EARLength != 2 {
		t.Errorf("data with EAR differs (EAR %d)", e.Extents[0].EARLength)
	}
	ear, err := v.ExtendedAttributeRecord(e)
	if err != nil || !bytes.Equal(ear, bytes.Repeat([]byte{0xEA}, 2*SectorSize)) {
		t.Errorf("EAR %d bytes, %v", len(ear), err)
	}
	if ear, err := v.ExtendedAttributeRecord(w.entry(t, "/")); ear != nil || err != nil {
		t.Errorf("root EAR %v %v", ear, err)
	}
}

func TestInterleaved(t *testing.T) {
	data := randData(5, 5*SectorSize+700)
	b := newBuilder()
	f := file("INTER.BIN;1", data)
	f.unit, f.gap = 2, 3
	bad := file("BAD.BIN;1", []byte("bad"))
	bad.fix = func(_ *builder, r []byte) { r[27] = 1 } // gap without unit size
	v := mustOpen(t, b.build(dir("", f, bad)), nil)
	w := walkAll(t, v)
	if !bytes.Equal(w.data["/INTER.BIN"], data) {
		t.Error("interleaved data differs")
	}
	if e := w.entry(t, "/INTER.BIN"); e.Extents[0].FileUnitSize != 2 || e.Extents[0].InterleaveGap != 3 {
		t.Errorf("extent %+v", e.Extents[0])
	}
	if !errors.Is(w.errs["/BAD.BIN"], ErrInterleave) {
		t.Errorf("bad interleave: %v", w.errs)
	}
	if len(v.Warnings()) != 1 {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestAssociatedFiles(t *testing.T) {
	b := newBuilder()
	res := file("APP.;1", []byte("resource fork"))
	res.flags = FlagAssociated
	hidden := file("HIDDEN.TXT;1", []byte("h"))
	hidden.flags = FlagHidden
	v := mustOpen(t, b.build(dir("", res, file("APP.;1", []byte("data fork")), hidden)), nil)
	ents, err := v.ReadDir(v.Root())
	if err != nil || len(ents) != 3 {
		t.Fatalf("ReadDir: %d entries, %v", len(ents), err)
	}
	if !ents[0].Associated || ents[1].Associated || ents[0].Name != "APP" || ents[1].Name != "APP" {
		t.Fatalf("entries %+v %+v", ents[0], ents[1])
	}
	if ents[1].AssociatedFile != ents[0] || ents[0].AssociatedFile != nil {
		t.Error("associated file not paired")
	}
	if !ents[2].Hidden {
		t.Error("hidden flag lost")
	}
	w := walkAll(t, v)
	if string(w.data["/APP"]) != "data fork" || string(w.data["/APP#assoc"]) != "resource fork" {
		t.Errorf("fork data %q %q", w.data["/APP"], w.data["/APP#assoc"])
	}
}

func TestLargeDirectory(t *testing.T) {
	b := newBuilder()
	b.joliet = true
	var kids []*node
	for i := range 300 {
		f := file(fmt.Sprintf("FILE%04d.DAT;1", i), []byte(fmt.Sprint(i)))
		f.jname = fmt.Sprintf("A rather long Joliet file name number %04d.dat", i)
		kids = append(kids, f)
	}
	root := dir("", kids...)
	img := b.build(root)
	if root.size <= 4*SectorSize || root.jsize <= 8*SectorSize {
		t.Fatalf("directory sizes %d/%d do not span sectors", root.size, root.jsize)
	}
	for _, ns := range []Namespace{NamespaceISO9660, NamespaceJoliet} {
		v := mustOpen(t, img, &Options{Namespace: ns})
		noWarnings(t, v)
		ents, err := v.ReadDir(v.Root())
		if err != nil || len(ents) != 300 {
			t.Fatalf("%v: %d entries, %v", ns, len(ents), err)
		}
		for i, e := range ents {
			f, _ := v.OpenFile(e)
			got, _ := io.ReadAll(f)
			if string(got) != fmt.Sprint(i) {
				t.Fatalf("%v: entry %d (%s) holds %q", ns, i, e.Name, got)
			}
		}
	}
}

// failingReader fails reads touching the given sectors.
type failingReader struct {
	r   io.ReaderAt
	bad map[int64]bool
}

var errInjected = errors.New("injected read error")

func (f failingReader) ReadAt(p []byte, off int64) (int, error) {
	for s := off / SectorSize; s <= (off+int64(len(p))-1)/SectorSize; s++ {
		if f.bad[s] {
			return 0, errInjected
		}
	}
	return f.r.ReadAt(p, off)
}

func TestDamagedVolumes(t *testing.T) {
	b := newBuilder()
	var kids []*node
	for i := range 150 {
		kids = append(kids, file(fmt.Sprintf("F%03d.TXT;1", i), []byte("x")))
	}
	big := dir("BIG", kids...)
	loop := dir("LOOP")
	selfLoop := dir("SELF")
	far := file("FAR.BIN;1", []byte("far"))
	far.fix = func(_ *builder, r []byte) { r[2], r[3], r[4], r[5] = 0, 0, 0, 0x10 } // LBA 0x10000000
	farDir := dir("FARDIR")
	farDir.fix = func(_ *builder, r []byte) { r[5] = 0x10 }
	short := file("SHORT.BIN;1", []byte("short"))
	var root *node
	loop.fix = func(b *builder, r []byte) { // points back at the root
		if root != nil && len(root.lba) > 0 {
			copy(r[2:], both32(root.lba[0]))
		}
	}
	root = dir("", big, loop, selfLoop, far, farDir, short, file("OK.TXT;1", []byte("ok")))
	img := b.build(root)
	// Truncate SHORT.BIN's sector away by growing its recorded size past
	// the end of the image.
	img = img[:len(img)-SectorSize+10]
	_ = short

	bad := failingReader{r: bytes.NewReader(img), bad: map[int64]bool{int64(big.lba[0]) + 1: true}}
	v, err := Open(bad, int64(len(img)), nil)
	if err != nil {
		t.Fatal(err)
	}
	w := walkAll(t, v)
	if !errors.Is(w.errs["/LOOP"], ErrLoop) {
		t.Errorf("loop: %v", w.errs)
	}
	if !errors.Is(w.errs["/FAR.BIN"], ErrOutOfRange) || !errors.Is(w.errs["/FARDIR"], ErrOutOfRange) {
		t.Errorf("out of range: %v", w.errs)
	}
	if !errors.Is(w.errs["/BIG"], errInjected) {
		t.Errorf("unreadable directory sector: %v", w.errs["/BIG"])
	}
	n := 0
	for p := range w.entries {
		if strings.HasPrefix(p, "/BIG/") {
			n++
		}
	}
	if n == 0 || n >= 150 {
		t.Errorf("salvaged %d of 150 entries", n)
	}
	if string(w.data["/OK.TXT"]) != "ok" {
		t.Error("intact file unreadable")
	}

	// Depth and entry limits.
	v = mustOpen(t, img, &Options{MaxDepth: 0})
	if v.opts.MaxDepth != 255 {
		t.Errorf("default depth %d", v.opts.MaxDepth)
	}
	deep := dir("D9")
	for i := 8; i >= 0; i-- {
		deep = dir(fmt.Sprintf("D%d", i), deep)
	}
	img2 := newBuilder().build(dir("", deep))
	v = mustOpen(t, img2, &Options{MaxDepth: 5})
	w = walkAll(t, v)
	if !errors.Is(w.errs["/D0/D1/D2/D3/D4/D5"], ErrTooDeep) {
		t.Errorf("depth: %v", w.errs)
	}
	v = mustOpen(t, img2, &Options{MaxEntries: 3})
	if err := v.Walk(func(string, *Entry, error) error { return nil }); !errors.Is(err, ErrTooManyEntries) {
		t.Errorf("entries: %v", err)
	}
}

func TestWalkSkip(t *testing.T) {
	img := newBuilder().build(dir("",
		dir("A", file("A1.TXT;1", nil), file("A2.TXT;1", nil)),
		dir("B", file("B1.TXT;1", nil), file("B2.TXT;1", nil)),
		file("C.TXT;1", nil)))
	v := mustOpen(t, img, nil)
	var seen []string
	err := v.Walk(func(p string, e *Entry, err error) error {
		seen = append(seen, p)
		switch p {
		case "/A":
			return fs.SkipDir
		case "/B/B1.TXT":
			return fs.SkipDir // skips the rest of /B
		case "/C.TXT":
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || fmt.Sprint(seen) != "[/ /A /B /B/B1.TXT /C.TXT]" {
		t.Errorf("seen %v, err %v", seen, err)
	}
	stop := errors.New("stop")
	if err := v.Walk(func(p string, e *Entry, err error) error { return stop }); err != stop {
		t.Errorf("error not returned: %v", err)
	}
}

func TestXA(t *testing.T) {
	xa := func(attr uint16, fileNo byte) []byte {
		return []byte{0, 1, 0, 2, byte(attr >> 8), byte(attr), 'X', 'A', fileNo, 0, 0, 0, 0, 0}
	}
	// Plain ISO with XA.
	b := newBuilder()
	root := dir("", &node{id: "AV.DAT;1", data: []byte("mpeg"), su: fixed(xa(XAForm2|XAWorldRead, 1))})
	root.dotSU = fixed(xa(XADirectory, 0))
	v := mustOpen(t, b.build(root), nil)
	noWarnings(t, v)
	w := walkAll(t, v)
	e := w.entry(t, "/AV.DAT")
	if e.XA == nil || e.XA.Attributes != XAForm2|XAWorldRead || e.XA.GroupID != 1 || e.XA.UserID != 2 || e.XA.FileNumber != 1 {
		t.Errorf("XA %+v", e.XA)
	}
	if r := w.entry(t, "/"); r.XA == nil || r.XA.Attributes != XADirectory {
		t.Errorf("root XA %+v", r.XA)
	}

	// XA followed by Rock Ridge, the SP after the XA field.
	b = newBuilder()
	root = dir("", &node{id: "AV.DAT;1", data: []byte("mpeg"), su: fixed(cat(xa(XAForm1, 0), nmEntry(0, "video.dat")))})
	root.dotSU = fixed(cat(xa(XADirectory, 0), spEntry(0), erEntry("RRIP_1991A", "RR", "S")))
	v = mustOpen(t, b.build(root), nil)
	noWarnings(t, v)
	w = walkAll(t, v)
	if e := w.entry(t, "/video.dat"); e.XA == nil || e.XA.Attributes != XAForm1 {
		t.Errorf("XA+RR %+v", e.XA)
	}
}

func TestMalformedSUSP(t *testing.T) {
	b := newBuilder()
	selfCE := b.alloc(1)
	// A continuation area that continues into itself.
	b.put(selfCE, 0, cat(nmEntry(1, "loop"), ceEntry(selfCE, 0, 64)))
	root := dir("",
		&node{id: "BADLEN.TXT;1", data: []byte("1"), su: fixed(cat(nmEntry(0, "ok"),
			[]byte{'P', 'X', 200, 1, 0, 0, 0, 0}))}, // overruns
		&node{id: "ZERO.TXT;1", data: []byte("2"), su: fixed([]byte{'N', 'M', 0, 1, 0, 'x'})},
		&node{id: "LOOP.TXT;1", data: []byte("3"), su: fixed(ceEntry(selfCE, 0, 64))},
		&node{id: "BADNM.TXT;1", data: []byte("4"), su: fixed(nmEntry(0, "a/b"))},
		&node{id: "DOTNM.TXT;1", data: []byte("5"), su: fixed(nmEntry(0x02, ""))},
		&node{id: "UNKNOWN.TXT;1", data: []byte("6"), su: fixed(cat(suspEntry("ZZ", 1, []byte("??")), nmEntry(0, "unk")))},
		&node{id: "CEOUT.TXT;1", data: []byte("7"), su: fixed(cat(nmEntry(1, "ce"), ceEntry(0xFFFFFF, 0, 10)))},
		&node{id: "SHORTPX.TXT;1", data: []byte("8"), su: fixed(cat(suspEntry("PX", 1, []byte{1, 2}), nmEntry(0, "spx")))},
	)
	root.dotSU = rrRoot()
	v := mustOpen(t, b.build(root), nil)
	w := walkAll(t, v)
	for p, want := range map[string]string{"/ok": "1", "/ZERO.TXT": "2", "/BADNM.TXT": "4", "/DOTNM.TXT": "5",
		"/unk": "6", "/spx": "8"} {
		if string(w.data[p]) != want {
			t.Errorf("%s: %q (have %v)", p, w.data[p], w.order)
		}
	}
	// The looping CE yields a name built from the area read once.
	if string(w.data["/looploop"]) != "3" && string(w.data["/loop"]) != "3" {
		t.Errorf("CE loop entry missing: %v", w.order)
	}
	if string(w.data["/ce"]) != "7" {
		t.Errorf("unreadable CE: %v", w.order)
	}
	if e := w.entry(t, "/unk"); len(e.SUSP) != 1 || e.SUSP[0].Signature != "ZZ" {
		t.Errorf("unknown SUSP %+v", e.SUSP)
	}
	if len(v.Warnings()) < 5 {
		t.Errorf("warnings %v", v.Warnings())
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(nil, 0, nil); !errors.Is(err, ErrNilReader) {
		t.Error(err)
	}
	zero := make([]byte, 20*SectorSize)
	if _, err := Open(bytes.NewReader(zero), int64(len(zero)), nil); !errors.Is(err, ErrNotISO9660) {
		t.Error(err)
	}
	if _, err := Open(bytes.NewReader(zero[:100]), 100, nil); !errors.Is(err, ErrNotISO9660) {
		t.Error(err)
	}
	hs := bytes.Clone(zero)
	copy(hs[16*SectorSize+9:], "CDROM")
	if _, err := Open(bytes.NewReader(hs), int64(len(hs)), nil); err == nil || !strings.Contains(err.Error(), "High Sierra") {
		t.Error(err)
	}
	// Only a terminator.
	term := bytes.Clone(zero)
	term[16*SectorSize] = 255
	copy(term[16*SectorSize+1:], "CD001")
	if _, err := Open(bytes.NewReader(term), int64(len(term)), nil); !errors.Is(err, ErrNoVolume) {
		t.Error(err)
	}
	img := newBuilder().build(dir("", file("A.;1", nil)))
	if _, err := Open(bytes.NewReader(img), int64(len(img)), &Options{Namespace: NamespaceJoliet}); !errors.Is(err, ErrNamespaceUnavailable) {
		t.Error(err)
	}
	// A root directory shorter than its own "." record (found by fuzzing).
	b := newBuilder()
	b.pvdHook = func(d []byte) { copy(d[156+10:], both32(20)) }
	img2 := b.build(dir("", file("A.;1", nil)))
	if v, err := Open(bytes.NewReader(img2), int64(len(img2)), nil); err != nil {
		t.Error(err)
	} else if _, err := v.ReadDir(v.Root()); err != nil || len(v.Warnings()) == 0 {
		t.Errorf("short root: %v, warnings %v", err, v.Warnings())
	}
	// Unknown size still works.
	v, err := Open(bytes.NewReader(img), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	walkAll(t, v)
}

func TestBootRecordAndMultisession(t *testing.T) {
	b := newBuilder()
	b.boot = true
	img := b.build(dir("", file("BOOT.CAT;1", []byte("catalog"))))
	v := mustOpen(t, img, nil)
	br := v.BootRecords()
	if len(br) != 1 || !br[0].ElTorito || br[0].CatalogLBA != 1234 || br[0].SystemID != "EL TORITO SPECIFICATION" {
		t.Errorf("boot records %+v", br)
	}
	// A second session: the same volume shifted by 1000 sectors, with its
	// (absolute) extents moved accordingly is too elaborate to build; check
	// that BaseLBA moves the descriptor read.
	shifted := append(make([]byte, 1000*SectorSize), img...)
	if _, err := Open(bytes.NewReader(shifted), int64(len(shifted)), &Options{BaseLBA: 1000}); err != nil {
		t.Errorf("BaseLBA: %v", err)
	}
	if _, err := Open(bytes.NewReader(shifted), int64(len(shifted)), nil); !errors.Is(err, ErrNotISO9660) {
		t.Errorf("without BaseLBA: %v", err)
	}
}

func TestDescriptorQuirks(t *testing.T) {
	b := newBuilder()
	b.blockSize = 3000                          // invalid: falls back to 2048
	b.pvdHook = func(d []byte) { d[83] = 0x7F } // both-endian volume space size mismatch
	v := mustOpen(t, b.build(dir("", file("A.;1", []byte("a")))), nil)
	if v.BlockSize() != 2048 || len(v.Warnings()) != 3 {
		t.Errorf("block size %d, warnings %v", v.BlockSize(), v.Warnings())
	}
	if w := walkAll(t, v); string(w.data["/A"]) != "a" {
		t.Error("file unreadable")
	}
}

// seedImages returns small volumes covering the features, for fuzzing.
func seedImages() [][]byte {
	var out [][]byte
	b := newBuilder()
	b.joliet = true
	f := file("BIG.BIN;1", randData(6, 2148))
	f.split = []int{2048, 100}
	out = append(out, b.build(dir("", f, dir("SUB", file("X.;1", []byte("x"))))))

	b = newBuilder()
	ce := b.reserve(nmEntry(0, "tail"))
	root := dir("",
		&node{id: "A.;1", data: []byte("a"), su: fixed(cat(nmEntry(1, "head"), ceEntry(ce, 0, 8),
			pxEntry(0o100644, 1, 0, 0), tfEntry(0x83, date17("2020010203040500", 0), date17("2020010203040500", 0))))},
		&node{id: "L.;1", su: fixed(slEntry(0, slComp{8, ""}, slComp{0, "x"}))},
		&node{id: "I.;1", data: randData(7, 2100), unit: 1, gap: 1, ear: 1},
	)
	root.dotSU = rrRoot()
	out = append(out, b.build(root))
	return out
}

func FuzzOpenWalk(f *testing.F) {
	for _, s := range seedImages() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, img []byte) {
		for _, ns := range []Namespace{NamespaceAuto, NamespaceISO9660, NamespaceJoliet, NamespaceRockRidge} {
			v, err := Open(bytes.NewReader(img), int64(len(img)), &Options{Namespace: ns, MaxEntries: 5000})
			if err != nil {
				continue
			}
			_ = v.Walk(func(p string, e *Entry, err error) error {
				if err != nil || e.IsDir {
					return nil
				}
				fl, err := v.OpenFile(e)
				if err != nil {
					return nil
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(fl, 1<<20))
				_, _ = v.ExtendedAttributeRecord(e)
				return nil
			})
			_ = v.Warnings()
		}
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
				// Mostly hit the metadata sectors.
				pos := 16*SectorSize + r.IntN(min(len(img)-16*SectorSize, 12*SectorSize))
				img[pos] = byte(r.Uint32())
			}
			v, err := Open(bytes.NewReader(img), int64(len(img)), &Options{MaxEntries: 5000})
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
