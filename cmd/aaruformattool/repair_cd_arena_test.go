package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/media"
)

func runRepair(path string, dryRun bool) (string, int) {
	var out bytes.Buffer
	rc := repairCDArena(&out, path, dryRun)
	return out.String(), rc
}

// buildArenaTestSector returns a raw Mode 1 sector for lba. A non-zero junk
// replaces its prefix and suffix with bytes that must be stored as custom
// slots.
func buildArenaTestSector(t *testing.T, lba uint64, junk byte) []byte {
	t.Helper()
	s := make([]byte, cdecc.SectorSize)
	for i := 16; i < 2064; i++ {
		s[i] = byte(lba*7 + uint64(i))
	}
	if err := cdecc.ReconstructPrefix(s, cdecc.TrackTypeCdMode1, int64(lba)); err != nil {
		t.Fatal(err)
	}
	if err := cdecc.Reconstruct(s, cdecc.TrackTypeCdMode1); err != nil {
		t.Fatal(err)
	}
	if junk != 0 {
		for i := 0; i < 16; i++ {
			s[i] = junk + byte(lba) + byte(i)
		}
		for i := 2064; i < len(s); i++ {
			s[i] = junk ^ byte(lba) ^ byte(i)
		}
	}
	return s
}

// resumeBugImage is a CD image damaged by the resume bug, with the long
// sectors it must read back after the repair.
type resumeBugImage struct {
	path string
	want map[uint64][]byte // expected raw sectors
	lost []uint64          // sectors whose custom slots no generation holds
}

const (
	arenaTestSectors = 40
	arenaLostSector  = 30
)

var (
	arenaSession1 = []uint64{3, 7, 11, 20} // custom prefix/suffix in the first session
	arenaSession2 = []uint64{5, 9, 25}     // custom prefix/suffix in the resumed session
)

// makeResumeBugImage writes a 40-sector CD image in two dump sessions the way
// older writers did before the resume fix: session 1 (written with opts by
// go-aaru) stores custom prefixes/suffixes for arenaSession1; session 2 is
// appended as the buggy resume wrote it, with arenas that restart at slot 0
// holding only arenaSession2's slots (compressed with comp) and DDT2s that
// keep session 1's indexes. With lost, sector arenaLostSector also references
// a slot no generation holds.
func makeResumeBugImage(t *testing.T, dir, opts string, comp aaru.CompressionType, lost bool) resumeBugImage {
	t.Helper()
	path := filepath.Join(dir, "resume_bug.aif")
	o := aaru.ParseOptions(opts)
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048,
		UserSectors: arenaTestSectors, Application: "maint"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0,
		End: arenaTestSectors - 1, Session: 1, Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	res := resumeBugImage{path: path, want: map[uint64][]byte{}}
	for lba := uint64(0); lba < arenaTestSectors; lba++ {
		var junk byte
		if slices.Contains(arenaSession1, lba) {
			junk = 0xA0
		}
		s := buildArenaTestSector(t, lba, junk)
		if err := img.WriteSectorLong(lba, false, s, aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
		res.want[lba] = s
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	r := maintParseRaw(t, maintReadFile(t, path))
	for _, a := range []struct {
		dt       aaru.DataType
		slot     uint32
		from, to int
	}{
		{aaru.DataTypeCdSectorPrefix, arenaPrefixSlot, 0, 16},
		{aaru.DataTypeCdSectorSuffix, arenaSuffixSlot, 2064, cdecc.SectorSize},
	} {
		templ, entries := r.ddt2(t, r.find(aaru.BlockTypeDeDuplicationTable2, a.dt))
		var arena []byte
		for j, lba := range arenaSession2 {
			s := buildArenaTestSector(t, lba, 0xB0)
			arena = append(arena, s[a.from:a.to]...)
			entries[lba] = uint64(aaru.SectorStatusErrored)<<arenaStatusShift | uint64(j)
			res.want[lba] = s
		}
		if lost {
			entries[arenaLostSector] = uint64(aaru.SectorStatusErrored)<<arenaStatusShift | 9
		}
		r.setIndexOffset(aaru.BlockTypeDataBlock, a.dt, r.appendDataBlock(t, a.dt, a.slot, comp, arena))
		r.setIndexOffset(aaru.BlockTypeDeDuplicationTable2, a.dt, r.appendDDT2(t, templ, comp, entries))
	}
	if lost {
		res.lost = []uint64{arenaLostSector}
	}
	r.writeIndex()
	maintWriteFile(t, path, r.data)
	return res
}

// checkArenaSectors verifies the long sectors of a repaired image.
func checkArenaSectors(t *testing.T, img resumeBugImage) {
	t.Helper()
	im, err := aaru.Open(img.path)
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	if err := im.Verify(); err != nil {
		t.Errorf("verify: %v", err)
	}
	for lba := uint64(0); lba < arenaTestSectors; lba++ {
		data, _, err := im.ReadSectorLong(lba, false)
		if slices.Contains(img.lost, lba) {
			if !errors.Is(err, aaru.ErrSectorNotDumped) {
				t.Errorf("sector %d: err = %v, want not dumped", lba, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("sector %d: %v", lba, err)
			continue
		}
		if !bytes.Equal(data, img.want[lba]) {
			t.Errorf("sector %d reads wrong", lba)
		}
	}
}

// damagedSectors counts the sectors of session 1 that read wrongly.
func damagedSectors(t *testing.T, img resumeBugImage) int {
	t.Helper()
	im, err := aaru.Open(img.path)
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	n := 0
	for _, lba := range arenaSession1 {
		data, _, err := im.ReadSectorLong(lba, false)
		if err != nil || !bytes.Equal(data, img.want[lba]) {
			n++
		}
	}
	return n
}

func TestRepairCDArenaResumeBug(t *testing.T) {
	for _, c := range []struct {
		name     string
		opts     string
		comp     aaru.CompressionType
		lost     bool
		snapshot bool // the output is compared with a stored expected output
	}{
		{name: "none", opts: "compress=false", comp: aaru.CompressionNone, snapshot: true},
		{name: "none_lost", opts: "compress=false", comp: aaru.CompressionNone, lost: true, snapshot: true},
		{name: "lzma", opts: "compress=true", comp: aaru.CompressionLzma},
		{name: "zstd", opts: "compress=true;zstd=true", comp: aaru.CompressionZstd, snapshot: true},
		{name: "lzma_lost", opts: "compress=true", comp: aaru.CompressionLzma, lost: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			img := makeResumeBugImage(t, dir, c.opts, c.comp, c.lost)
			if damagedSectors(t, img) == 0 {
				t.Fatal("the damaged image reads correctly")
			}
			damaged := maintReadFile(t, img.path)

			lost := 0
			if c.lost {
				lost = 1
			}
			counts := fmt.Sprintf("  prefix: 7 custom slots recovered, %d unrecoverable (marked not-dumped).\n"+
				"  suffix: 7 custom slots recovered, %d unrecoverable (marked not-dumped).\n", lost, lost)

			for _, dry := range []bool{true, false} {
				out, rc := runRepair(img.path, dry)
				if rc != 0 {
					t.Fatalf("dry run %v: rc = %d\n%s", dry, rc, out)
				}
				if !strings.Contains(out, "Found 4 prefix/suffix generations.\n"+counts) {
					t.Errorf("dry run %v: unexpected output:\n%s", dry, out)
				}
				if dry && !bytes.Equal(maintReadFile(t, img.path), damaged) {
					t.Error("the dry run modified the image")
				}

				if c.snapshot {
					name := "repair_resume_" + c.name
					if dry {
						name += "_dry"
					}
					checkExpected(t, "maint/"+name+".txt", maintNormalize(out, img.path, "IMAGE"))
				}
			}

			checkArenaSectors(t, img)
			maintCheckReads(t, img.path, maintDump(img.path))

			// Repairing again changes nothing that reads.
			before := maintDump(img.path)
			if out, rc := runRepair(img.path, false); rc != 0 {
				t.Fatalf("second repair: rc = %d\n%s", rc, out)
			}
			maintCheckReads(t, img.path, before)
		})
	}
}

// writeResumedCDImage writes a 40-sector CD image in two dump sessions with
// go-aaru (Create, then Resume): session 1 stores custom prefixes/suffixes for
// arenaSession1, session 2 rewrites arenaSession2 with custom ones.
func writeResumedCDImage(t *testing.T, path, opts string) {
	t.Helper()
	o := aaru.ParseOptions(opts)
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048,
		UserSectors: arenaTestSectors, Application: "maint"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0,
		End: arenaTestSectors - 1, Session: 1, Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	for lba := uint64(0); lba < arenaTestSectors; lba++ {
		var junk byte
		if slices.Contains(arenaSession1, lba) {
			junk = 0xA0
		}
		if err := img.WriteSectorLong(lba, false, buildArenaTestSector(t, lba, junk),
			aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err = aaru.Resume(path, &o)
	if err != nil {
		t.Fatal(err)
	}
	for _, lba := range arenaSession2 {
		if err := img.WriteSectorLong(lba, false, buildArenaTestSector(t, lba, 0xB0),
			aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
}

// injectResumeBug rewrites the final prefix/suffix generation of a resumed
// image the way the buggy resume wrote it: the arena restarts at slot 0 and
// holds only the slots of the sectors written in the resumed session, whose
// DDT2 entries are renumbered from 0, while the entries of the first
// session's sectors keep their old indexes. The correct final generation is
// orphaned with its block identifiers cleared, so the file holds the same two
// generations as an image written by the buggy writer.
func injectResumeBug(t *testing.T, path string, comp aaru.CompressionType) {
	t.Helper()
	r := maintParseRaw(t, maintReadFile(t, path))
	for _, a := range []struct {
		dt   aaru.DataType
		slot uint64
	}{{aaru.DataTypeCdSectorPrefix, arenaPrefixSlot}, {aaru.DataTypeCdSectorSuffix, arenaSuffixSlot}} {
		ddtOff := r.find(aaru.BlockTypeDeDuplicationTable2, a.dt)
		blkOff := r.find(aaru.BlockTypeDataBlock, a.dt)
		if ddtOff == 0 || blkOff == 0 {
			t.Fatalf("no %v generation", a.dt)
		}
		templ, entries := r.ddt2(t, ddtOff)
		arena := maintBlockPayload(t, r, blkOff)
		var buggy []byte
		for j, lba := range arenaSession2 {
			e := entries[lba]
			if !isCustomArenaEntry(e) {
				t.Fatalf("%v: sector %d has no custom slot (entry %#x)", a.dt, lba, e)
			}
			idx := e & arenaIndexMask
			buggy = append(buggy, arena[idx*a.slot:(idx+1)*a.slot]...)
			entries[lba] = e&^arenaIndexMask | uint64(j)
		}
		r.setIndexOffset(aaru.BlockTypeDataBlock, a.dt, r.appendDataBlock(t, a.dt, uint32(a.slot), comp, buggy))
		r.setIndexOffset(aaru.BlockTypeDeDuplicationTable2, a.dt, r.appendDDT2(t, templ, comp, entries))
		r.put(ddtOff, uint32(0))
		r.put(blkOff, uint32(0))
	}
	r.writeIndex()
	maintWriteFile(t, path, r.data)
}

// maintBlockPayload decodes the data block at off.
func maintBlockPayload(t *testing.T, r *maintRaw, off uint64) []byte {
	t.Helper()
	var h aaru.BlockHeader
	if _, err := binary.Decode(r.data[off:], binary.LittleEndian, &h); err != nil {
		t.Fatal(err)
	}
	start := off + maintBlockHeaderSize
	out := decodeArenaPayload(r.data[start:], h.Compression, uint64(h.CmpLength), uint64(h.Length), h.CRC64)
	if out == nil {
		t.Fatalf("cannot decode data block at %d", off)
	}
	return out
}

// TestRepairCDArenaResumedImage repairs a two-session image written by go-aaru
// and then given the resume bug's arena damage, and compares it with the
// undamaged image.
func TestRepairCDArenaResumedImage(t *testing.T) {
	for _, c := range []struct {
		name, opts string
		comp       aaru.CompressionType
	}{
		{"lzma", "compress=true", aaru.CompressionLzma},
		{"zstd", "compress=true;zstd=true", aaru.CompressionZstd},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			undamaged := filepath.Join(dir, "undamaged.aif")
			writeResumedCDImage(t, undamaged, c.opts)
			want := maintDump(undamaged)
			path := filepath.Join(dir, "image.aif")
			maintCopy(t, undamaged, path)
			injectResumeBug(t, path, c.comp)
			if maintDump(path) == want {
				t.Fatal("the damaged image reads like the undamaged one")
			}
			out, rc := runRepair(path, false)
			if rc != 0 {
				t.Fatalf("rc = %d\n%s", rc, out)
			}
			for _, s := range []string{"Found 4 prefix/suffix generations.",
				"prefix: 7 custom slots recovered, 0 unrecoverable", "suffix: 7 custom slots recovered, 0 unrecoverable",
				"Repair written. New index at "} {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q:\n%s", s, out)
				}
			}
			maintCheckReads(t, path, want)
		})
	}
}

func TestRepairCDArena(t *testing.T) {
	cases := []struct {
		name       string
		src        string          // fixture (empty: missing file)
		input      []byte          // literal input
		patch      func(*maintRaw) // alter the input image
		dry        bool
		rc         int
		output     string
		same       bool // the image must read exactly like src afterwards
		noSnapshot bool // no stored expected output
	}{
		{name: "healthy_lzma", src: "cd_lzma", rc: 0, same: true, noSnapshot: true,
			output: "Repair written. New index at "},
		{name: "healthy_zstd_dry", src: "cd_zstd", dry: true, rc: 0, same: true,
			output: "Dry run only; no changes written."},
		{name: "healthy_raw", src: "cd_raw", rc: 0, same: true, output: "Repair written. New index at "},
		{name: "no_arena", src: "floptical", rc: 0, same: true,
			output: "Image has no CD sector prefix/suffix DDT2; nothing to repair."},
		{name: "v1", src: "cdmode1_v1", rc: maintEINVAL, output: "ERROR: not an AaruFormat v2 image"},
		{name: "dic_magic", src: "dic_magic", rc: maintEINVAL,
			output: "ERROR: not an AaruFormat v2 image"},
		{name: "short", input: []byte("AARUFRMT"), rc: maintEIO, output: "ERROR: cannot read header"},
		{name: "bad_index", src: "floptical", rc: maintEIO,
			patch:  func(r *maintRaw) { r.put(r.header.IndexOffset, uint32(0x12345678)) },
			output: "ERROR: cannot read index at "},
		{name: "index_loop", src: "cd_raw", rc: 0, same: true, noSnapshot: true,
			patch: func(r *maintRaw) {
				// An index segment whose previous segment is itself.
				r.put(r.header.IndexOffset+20, r.header.IndexOffset)
			},
			output: "Repair written. New index at "},
		{name: "missing", rc: 2, output: "ERROR: cannot open 'IMAGE': No such file or directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "image.aif")
			input := c.input
			if c.src != "" {
				input = maintReadFile(t, fixture(t, c.src))
				if c.patch != nil {
					r := maintParseRaw(t, input)
					c.patch(r)
					input = r.data
				}
			}
			if input != nil {
				maintWriteFile(t, path, input)
			}

			out, rc := runRepair(path, c.dry)
			got := maintNormalize(out, path, "IMAGE")
			if rc != c.rc {
				t.Errorf("rc = %d, want %d\n%s", rc, c.rc, out)
			}
			if !strings.Contains(got, portable(c.output)) {
				t.Errorf("output lacks %q:\n%s", c.output, got)
			}
			if c.same {
				maintCheckReads(t, path, maintDump(fixture(t, c.src)))
			} else if input != nil && !bytes.Equal(maintReadFile(t, path), input) {
				t.Error("the image was modified")
			}

			if !c.noSnapshot {
				checkExpected(t, "maint/repair_"+c.name+".txt", got)
			}
		})
	}
}

func TestRepairCDArenaArgs(t *testing.T) {
	dir := t.TempDir()
	img := makeResumeBugImage(t, dir, "compress=false", aaru.CompressionNone, false)
	damaged := maintReadFile(t, img.path)

	out := captureMaintStdout(t, func() {
		if rc := cmdRepairCDArena(nil); rc != -1 {
			t.Errorf("no arguments: rc = %d", rc)
		}
		if rc := cmdRepairCDArena([]string{"--dry-run"}); rc != -1 {
			t.Errorf("no file: rc = %d", rc)
		}
		if rc := cmdRepairCDArena([]string{"--bogus", img.path}); rc != -1 {
			t.Errorf("unknown option: rc = %d", rc)
		}
	})
	if n := strings.Count(out, "aaruformattool repair-cd-arena [--dry-run] <filename>"); n != 3 {
		t.Errorf("usage printed %d times:\n%s", n, out)
	}

	// Options may follow the file name.
	out = captureMaintStdout(t, func() {
		if rc := cmdRepairCDArena([]string{img.path, "--dry-run"}); rc != 0 {
			t.Errorf("dry run: rc = %d", rc)
		}
	})
	if !strings.Contains(out, " (dry run)\n") || !bytes.Equal(maintReadFile(t, img.path), damaged) {
		t.Errorf("--dry-run after the file name was not honoured:\n%s", out)
	}

	out = captureMaintStdout(t, func() {
		if rc := cmdRepairCDArena([]string{img.path}); rc != 0 {
			t.Errorf("repair: rc = %d", rc)
		}
	})
	if !strings.Contains(out, "Repair written.") {
		t.Errorf("unexpected output:\n%s", out)
	}
	checkArenaSectors(t, img)
}

// TestRepairCDArenaNoCustomSlots runs the repair on an image whose
// prefix/suffix tables hold no custom entries. Without an arena data block
// there is no generation to pair the final DDT2 with, so the repair reports it
// as undecodable and only rewrites the index.
func TestRepairCDArenaNoCustomSlots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.aif")
	o := aaru.ParseOptions("compress=false")
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048,
		UserSectors: 8, Application: "maint"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 7,
		Session: 1, Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	for lba := uint64(0); lba < 8; lba++ {
		if err := img.WriteSectorLong(lba, false, buildArenaTestSector(t, lba, 0),
			aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	want := maintDump(path)

	out, rc := runRepair(path, false)
	if rc != 0 || !strings.Contains(out, "Found 0 prefix/suffix generations.\n"+
		"WARNING: final prefix DDT2 could not be decoded; skipping.\n"+
		"WARNING: final suffix DDT2 could not be decoded; skipping.\n") {
		t.Fatalf("rc = %d\n%s", rc, out)
	}
	maintCheckReads(t, path, want)
}

// TestRepairCDArenaSharedBlock repairs an image whose resumed session added
// no custom prefix/suffix: its DDT2s pair with the first session's data
// blocks.
func TestRepairCDArenaSharedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.aif")
	o := aaru.ParseOptions("compress=false")
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048,
		UserSectors: 8, Application: "maint"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 7,
		Session: 1, Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	for lba := uint64(0); lba < 8; lba++ {
		var junk byte
		if lba == 3 {
			junk = 0xA0
		}
		if err := img.WriteSectorLong(lba, false, buildArenaTestSector(t, lba, junk),
			aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err = aaru.Resume(path, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.WriteSectorLong(5, false, buildArenaTestSector(t, 5, 0), aaru.SectorStatusDumped); err != nil {
		t.Fatal(err)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	want := maintDump(path)

	out, rc := runRepair(path, false)
	if rc != 0 || !strings.Contains(out, "Found 4 prefix/suffix generations.\n"+
		"  prefix: 1 custom slots recovered, 0 unrecoverable (marked not-dumped).\n"+
		"  suffix: 1 custom slots recovered, 0 unrecoverable (marked not-dumped).\n") {
		t.Fatalf("rc = %d\n%s", rc, out)
	}
	maintCheckReads(t, path, want)
}
