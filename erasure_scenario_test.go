package aaru

// Erasure coding write scenarios.
//
// Every scenario is a script of write operations (including
// SetErasureCoding) executed by go-aaru. The tests check that
//
//   - every operation succeeds, the image opens and verifies, reports the
//     erasure coding that was set, and reads back the tracks, media tags and
//     sectors that were written;
//   - damaged images are recovered and read back like the undamaged ones.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

type ecOp struct {
	kind   string
	params CreateParams
	opts   string
	alg    ErasureCodingAlgorithm
	k, m   uint16
	pct    uint8
	tracks []TrackEntry
	tag    int32
	addr   uint64
	neg    bool
	status SectorStatus
	off, n int
}

type ecScript struct {
	ops  []ecOp
	blob []byte
}

func (s *ecScript) put(b []byte) (int, int) {
	off := len(s.blob)
	s.blob = append(s.blob, b...)
	return off, len(b)
}

func (s *ecScript) create(p CreateParams, opts string) {
	if opts == "" {
		opts = "-"
	}
	s.ops = append(s.ops, ecOp{kind: "create", params: p, opts: opts})
}

func (s *ecScript) resume(opts string) {
	if opts == "" {
		opts = "-"
	}
	s.ops = append(s.ops, ecOp{kind: "resume", opts: opts})
}

func (s *ecScript) ec(alg ErasureCodingAlgorithm, k, m uint16) {
	s.ops = append(s.ops, ecOp{kind: "ec", alg: alg, k: k, m: m})
}

func (s *ecScript) ecAuto(pct uint8) { s.ops = append(s.ops, ecOp{kind: "ecauto", pct: pct}) }

func (s *ecScript) setTracks(t ...TrackEntry) {
	s.ops = append(s.ops, ecOp{kind: "settracks", tracks: t})
}

func (s *ecScript) mediaTag(t MediaTagType, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, ecOp{kind: "mtag", tag: int32(t), off: off, n: n})
}

func (s *ecScript) ws(addr uint64, st SectorStatus, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, ecOp{kind: "ws", addr: addr, status: st, off: off, n: n})
}

func (s *ecScript) close() { s.ops = append(s.ops, ecOp{kind: "close"}) }

// runGo executes the script with go-aaru, returning the status code of every
// operation (-1000 for a failed create/resume).
func (s *ecScript) runGo(path string) []int {
	var img *Image
	var rcs []int
	for _, o := range s.ops {
		var err error
		data := s.blob[o.off : o.off+o.n]
		var opts *Options
		if o.opts != "-" && o.opts != "" {
			v := ParseOptions(o.opts)
			opts = &v
		}
		switch o.kind {
		case "create":
			if img, err = Create(path, o.params, opts); err != nil {
				rcs = append(rcs, -1000)
				continue
			}
		case "resume":
			if img, err = Resume(path, opts); err != nil {
				rcs = append(rcs, -1000)
				continue
			}
		case "ec":
			err = img.SetErasureCoding(o.alg, o.k, o.m)
		case "ecauto":
			err = img.SetErasureCodingAuto(o.pct)
		case "settracks":
			err = img.SetTracks(o.tracks)
		case "mtag":
			err = img.WriteMediaTag(MediaTagType(o.tag), data)
		case "ws":
			err = img.WriteSector(o.addr, o.neg, data, o.status)
		case "wl":
			err = img.WriteSectorLong(o.addr, o.neg, data, o.status)
		case "close":
			err = img.Close()
		}
		rcs = append(rcs, ErrorCode(err))
	}
	return rcs
}

// ecGoDump describes an image read with go-aaru: tracks, media tags and
// sectors.
func ecGoDump(img *Image, openErr error) []string {
	if openErr != nil {
		return []string{"OPEN fail"}
	}
	out := []string{"OPEN ok"}
	tracks, err := img.Tracks()
	out = append(out, fmt.Sprintf("TRACKS rc=%d", ErrorCode(err)))
	for _, t := range tracks {
		out = append(out, fmt.Sprintf("TRACK %d %d %d %d %d %d %d", t.Sequence, t.Type, t.Start, t.End, t.Pregap,
			t.Session, t.Flags))
	}
	for mt := -1; mt < 120; mt++ {
		d, err := img.ReadMediaTag(MediaTagType(mt))
		if errorsIsNotPresent(err) {
			continue
		}
		var crc uint64
		if err == nil {
			crc = crc64.Checksum(d)
		}
		out = append(out, fmt.Sprintf("MTAG %d rc=%d len=%d crc=%016x", mt, ErrorCode(err), len(d), crc))
	}
	neg := uint64(img.userDataDDTHeader.Negative)
	user := img.info.Sectors + uint64(img.userDataDDTHeader.Overflow)
	line := func(n bool, a uint64) string {
		d, st, err := img.ReadSector(a, n)
		var crc uint64
		if err == nil {
			crc = crc64.Checksum(d)
		}
		return fmt.Sprintf("S %v %d rc=%d st=%d len=%d crc=%016x", n, a, ErrorCode(err), st, len(d), crc)
	}
	for a := uint64(1); a <= neg; a++ {
		out = append(out, line(true, a))
	}
	for a := uint64(0); a < user; a++ {
		out = append(out, line(false, a))
	}
	return out
}

func errorsIsNotPresent(err error) bool { return ErrorCode(err) == ErrorCode(ErrMediaTagNotPresent) }

// ecPattern fills a sector with data that compresses somewhat.
func ecPattern(n int, seed uint64) []byte {
	b := make([]byte, n)
	x := seed*0x9E3779B97F4A7C15 + 1
	for i := range b {
		if i%16 < 6 {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			b[i] = byte(x)
		} else {
			b[i] = byte(i / 16)
		}
	}
	return b
}

// ecScenario is an erasure coding write scenario.
type ecScenario struct {
	name   string
	script *ecScript
}

func ecBlockScript(opts string, sectors int, setup func(s *ecScript)) *ecScript {
	s := &ecScript{}
	s.create(CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: uint64(sectors),
		Application: "go-aaru-test", ApplicationMajorVersion: 1, ApplicationMinorVersion: 2}, opts)
	setup(s)
	for i := range sectors {
		st := SectorStatusDumped
		if i%37 == 5 {
			st = SectorStatusErrored
		}
		if i%53 == 7 {
			continue // not dumped
		}
		d := ecPattern(512, uint64(i))
		if i%41 == 3 {
			d = ecPattern(512, 3) // duplicates
		}
		s.ws(uint64(i), st, d)
	}
	s.close()
	return s
}

func ecCDScript(opts string, setup func(s *ecScript), tags bool) *ecScript {
	s := &ecScript{}
	s.create(CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 100, Application: "go-aaru-test",
		ApplicationMajorVersion: 1}, opts)
	setup(s)
	s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 99, Session: 1, Flags: 4})
	if tags {
		s.mediaTag(MediaTagCdToc, ecPattern(300, 99))
		s.mediaTag(MediaTagCdAtip, ecPattern(28, 98))
	}
	for i := range 100 {
		s.ws(uint64(i), SectorStatusDumped, ecPattern(2048, uint64(i)+1000))
	}
	s.close()
	return s
}

func ecScenarios() []ecScenario {
	rs := func(k, m uint16) func(*ecScript) {
		return func(s *ecScript) { s.ec(ErasureCodingRsVandermonde, k, m) }
	}
	resume := ecBlockScript("compress=false;deduplicate=false;data_shift=3", 200, rs(4, 2))
	resume.resume("compress=false;deduplicate=false")
	resume.ec(ErasureCodingRsVandermonde, 3, 2)
	for i := 150; i < 250; i++ {
		if i < 200 {
			resume.ws(uint64(i), SectorStatusDumped, ecPattern(512, uint64(i)*7))
		}
	}
	resume.close()

	return []ecScenario{
		{"block/uncompressed/K4M2", ecBlockScript("compress=false;deduplicate=false;data_shift=4", 300, rs(4, 2))},
		{"block/uncompressed/dedup/K5M3", ecBlockScript("compress=false;deduplicate=true;data_shift=3", 300, rs(5, 3))},
		{"block/uncompressed/xor/K3M1", ecBlockScript("compress=false;deduplicate=false;data_shift=4", 250,
			func(s *ecScript) { s.ec(ErasureCodingXor, 3, 1) })},
		{"block/uncompressed/auto25", ecBlockScript("compress=false;deduplicate=false;data_shift=2", 400,
			func(s *ecScript) { s.ecAuto(25) })},
		{"block/uncompressed/multilevel", ecBlockScript("compress=false;deduplicate=false;table_shift=4;data_shift=3",
			300, rs(4, 2))},
		{"block/uncompressed/align12", ecBlockScript("compress=false;deduplicate=false;data_shift=4;block_alignment=12",
			200, rs(2, 2))},
		{"block/uncompressed/md5", ecBlockScript("compress=false;deduplicate=false;data_shift=4;md5=true;sha1=true",
			100, rs(4, 2))},
		{"block/uncompressed/resume", resume},
		{"cd/uncompressed", ecCDScript("compress=false;deduplicate=false;data_shift=3", rs(4, 2), true)},
		{"cd/uncompressed/no-tags", ecCDScript("compress=false;deduplicate=false;data_shift=3", rs(3, 2), false)},
		{"block/lzma", ecBlockScript("compress=true;deduplicate=true;data_shift=4", 300, rs(4, 2))},
		{"block/lzma/multilevel", ecBlockScript("compress=true;deduplicate=false;table_shift=4;data_shift=3", 300,
			rs(4, 2))},
		{"block/zstd", ecBlockScript("compress=true;zstd=true;deduplicate=false;data_shift=4", 300, rs(3, 2))},
		{"cd/lzma", ecCDScript("compress=true;deduplicate=false;data_shift=4", rs(4, 2), true)},
		{"cd/lzma/one-block", ecCDScript("compress=true;deduplicate=false", rs(4, 2), false)},
	}
}

// ecExpect reads the image written by s back and checks it against the
// script: tracks, media tags (and no others), the erasure coding parameters
// of the last SetErasureCoding, and every sector (the last write of each
// address; unwritten sectors must read as not dumped).
func ecExpect(t *testing.T, s *ecScript, img *Image) {
	t.Helper()
	var tracks []TrackEntry
	tags := map[MediaTagType][]byte{}
	sectors := map[uint64]ecOp{}
	var last ecOp
	for _, o := range s.ops {
		switch o.kind {
		case "settracks":
			tracks = o.tracks
		case "mtag":
			tags[MediaTagType(o.tag)] = s.blob[o.off : o.off+o.n]
		case "ws":
			sectors[o.addr] = o
		case "ec", "ecauto":
			last = o
		}
	}

	got, err := img.Tracks()
	if tracks == nil {
		if err == nil {
			t.Errorf("Tracks = %+v, want none", got)
		}
	} else if err != nil || !equalSlices(got, tracks) {
		t.Errorf("Tracks = %+v, %v; want %+v", got, err, tracks)
	}
	// Parity blocks read as unknown media tags (-1), so only known tags are
	// checked.
	for mt := MediaTagType(0); mt < 128; mt++ {
		d, err := img.ReadMediaTag(mt)
		want, ok := tags[mt]
		if !ok {
			if !errors.Is(err, ErrMediaTagNotPresent) {
				t.Errorf("media tag %v present (%d bytes, %v)", mt, len(d), err)
			}
		} else if err != nil || !bytes.Equal(d, want) {
			t.Errorf("media tag %v: %d bytes, %v; want %d bytes", mt, len(d), err, len(want))
		}
	}

	info, ok := img.ErasureCoding()
	if !ok {
		t.Error("no erasure coding reported")
	} else {
		k, m, alg := last.k, last.m, last.alg
		if last.kind == "ecauto" {
			k, m, _ = ecAutoParameters(last.pct)
			alg = ErasureCodingRsVandermonde
		}
		if info.Algorithm != alg || (info.K != 0 && (info.K != k || info.M != m)) {
			t.Errorf("ErasureCoding = %+v, want %v K=%d M=%d", info, alg, k, m)
		}
	}

	bad := 0
	for a := range img.Info().Sectors {
		d, st, err := img.ReadSector(a, false)
		o, written := sectors[a]
		ok := true
		if !written {
			ok = errors.Is(err, ErrSectorNotDumped)
		} else if err != nil || st != o.status || !bytes.Equal(d, s.blob[o.off:o.off+o.n]) {
			ok = false
		}
		if !ok {
			if bad++; bad <= 5 {
				t.Errorf("sector %d: st=%v err=%v len=%d (written %v, st %v)", a, st, err, len(d), written, o.status)
			}
		}
	}
	if bad > 5 {
		t.Errorf("%d sectors differ", bad)
	}
}

// TestScenarioECWrite writes every scenario and reads the image back.
func TestScenarioECWrite(t *testing.T) {
	for _, sc := range ecScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.aif")
			rcs := sc.script.runGo(path)
			for i, rc := range rcs {
				if rc != 0 {
					t.Errorf("op %d (%s): rc %d", i, sc.script.ops[i].kind, rc)
				}
			}
			img, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer img.Close()
			if err := img.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
			if img.Header().FeatureCompatibleRO&FeatureROCompatErasure == 0 || !img.ec.recoveryAvailable {
				t.Error("erasure coding feature bit or recovery data missing")
			}
			ecExpect(t, sc.script, img)
		})
	}
}

func compareDumps(t *testing.T, what string, got, want []string) {
	t.Helper()
	n := 0
	for i := range max(len(got), len(want)) {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			if n < 10 {
				t.Errorf("%s: line %d:\n got: %s\nwant: %s", what, i, g, w)
			}
			n++
		}
	}
	if n > 0 {
		t.Errorf("%s: %d lines differ", what, n)
	}
}

// ecDamage describes a way to damage an erasure coded image.
type ecDamage struct {
	name string
	// apply damages b (the image file) and returns the offset of the
	// damaged block, or false if the image has nothing to damage.
	apply func(t *testing.T, img *Image, b []byte) (uint64, bool)
	// userData reports whether the damaged block is a user data block (or
	// the header), protected only if written after erasure coding was
	// enabled.
	userData bool
}

// ecFlip overwrites n bytes at off with a pattern.
func ecFlip(b []byte, off uint64, n int) {
	for i := range n {
		if off+uint64(i) < uint64(len(b)) {
			b[off+uint64(i)] ^= 0xDE ^ byte(i)
		}
	}
}

func ecIndexed(img *Image, bt BlockType, dt DataType, nth int) (uint64, bool) {
	for _, e := range img.indexEntries {
		if e.BlockType == bt && (bt != BlockTypeDataBlock || e.DataType == dt) {
			if nth == 0 {
				return e.Offset, true
			}
			nth--
		}
	}
	return 0, false
}

func ecDamages() []ecDamage {
	dataBlock := func(nth int, at uint64, n int) func(*testing.T, *Image, []byte) (uint64, bool) {
		return func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			off, ok := ecIndexed(img, BlockTypeDataBlock, DataTypeUserData, nth)
			if ok {
				ecFlip(b, off+at, n)
			}
			return off, ok
		}
	}
	return []ecDamage{
		{"payload", dataBlock(0, 50, 100), true},
		{"header", dataBlock(1, 0, blockHeaderSize), true},
		{"two-blocks", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			a, ok1 := ecIndexed(img, BlockTypeDataBlock, DataTypeUserData, 0)
			c, ok2 := ecIndexed(img, BlockTypeDataBlock, DataTypeUserData, 1)
			if !ok1 || !ok2 || img.ec.data == nil || img.ec.data.m < 2 {
				return 0, false
			}
			ecFlip(b, a+40, 64)
			ecFlip(b, c, 8)
			return a, true
		}, true},
		{"last-block", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			var last uint64
			for _, e := range img.indexEntries {
				if e.BlockType == BlockTypeDataBlock && e.DataType == DataTypeUserData {
					last = e.Offset
				}
			}
			ecFlip(b, last+60, 30)
			return last, last != 0
		}, true},
		{"tracks", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			off, ok := ecIndexed(img, BlockTypeTracksBlock, 0, 0)
			if ok {
				ecFlip(b, off+tracksHeaderSize+2, 1)
			}
			return off, ok
		}, false},
		{"media-tag", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			for _, e := range img.indexEntries {
				if e.BlockType == BlockTypeDataBlock && e.DataType == DataTypeCdToc {
					ecFlip(b, e.Offset+blockHeaderSize+3, 2)
					return e.Offset, true
				}
			}
			return 0, false
		}, false},
		{"secondary-ddt", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			off, ok := ecIndexed(img, BlockTypeDeDuplicationTableSecondary, 0, 1)
			if ok {
				ecFlip(b, off+ddtHeader2Size+4, 16)
			}
			return off, ok
		}, false},
		{"primary-header", func(t *testing.T, img *Image, b []byte) (uint64, bool) {
			ecFlip(b, 0, 64)
			return 0, true
		}, true},
	}
}

// TestScenarioECRecovery damages the image of every scenario and checks that
// it reads back like the undamaged image.
func TestScenarioECRecovery(t *testing.T) {
	for _, sc := range ecScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "go.aif")
			sc.script.runGo(path)
			orig, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			if !img.ec.recoveryAvailable {
				t.Fatal("no recovery data loaded")
			}
			clean := ecGoDump(img, nil)
			for _, d := range ecDamages() {
				b := bytes.Clone(orig)
				off, ok := d.apply(t, img, b)
				if !ok {
					continue
				}
				if _, protected := img.ec.data.lookup[off]; d.userData && off != 0 && !protected {
					continue // written before erasure coding was enabled
				}
				damaged := filepath.Join(dir, d.name+".aif")
				if err := os.WriteFile(damaged, b, 0o644); err != nil {
					t.Fatal(err)
				}
				gi, err := Open(damaged)
				compareDumps(t, fmt.Sprintf("reading damaged (%s) image", d.name), ecGoDump(gi, err), clean)
				if gi != nil {
					gi.Close()
				}
			}
		})
	}
}
