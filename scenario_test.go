package aaru

// Write scenarios.
//
// Every scenario is a script of write operations executed by go-aaru. The
// status code of every operation must be the expected one (success, or the
// error the script expects); the image must open and verify, and read back
// with exactly what the script wrote (checkScenario).

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

type writeOp struct {
	kind   string
	params CreateParams
	opts   string // "-" selects the default options
	tracks []TrackEntry
	tag    int32
	addr   uint64
	neg    bool
	status SectorStatus
	off, n int
	want   error // expected result (nil: success)
	// readErr is the error reading the sector back must give (a long CD
	// sector written all zeros reads as not dumped).
	readErr  error
	skipRead bool // accepted but not readable back
}

type writeScript struct {
	ops  []writeOp
	blob []byte
}

func (s *writeScript) put(b []byte) (int, int) {
	off := len(s.blob)
	s.blob = append(s.blob, b...)
	return off, len(b)
}

func (s *writeScript) create(p CreateParams, opts string) {
	if opts == "" {
		opts = "-"
	}
	s.ops = append(s.ops, writeOp{kind: "create", params: p, opts: opts})
}

func (s *writeScript) resume(opts string) {
	if opts == "" {
		opts = "-"
	}
	s.ops = append(s.ops, writeOp{kind: "resume", opts: opts})
}

func (s *writeScript) setTracks(t ...TrackEntry) {
	s.ops = append(s.ops, writeOp{kind: "settracks", tracks: t})
}

func (s *writeScript) mediaTag(t MediaTagType, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, writeOp{kind: "mtag", tag: int32(t), off: off, n: n})
}

func (s *writeScript) ws(addr uint64, neg bool, st SectorStatus, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, writeOp{kind: "ws", addr: addr, neg: neg, status: st, off: off, n: n})
}

func (s *writeScript) wl(addr uint64, neg bool, st SectorStatus, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, writeOp{kind: "wl", addr: addr, neg: neg, status: st, off: off, n: n})
}

func (s *writeScript) wt(addr uint64, neg bool, tag SectorTagType, b []byte) {
	off, n := s.put(b)
	s.ops = append(s.ops, writeOp{kind: "wt", addr: addr, neg: neg, tag: int32(tag), off: off, n: n})
}

func (s *writeScript) close() { s.ops = append(s.ops, writeOp{kind: "close"}) }

// fails records that the last operation must fail with err.
func (s *writeScript) fails(err error) { s.ops[len(s.ops)-1].want = err }

// readsAs records that reading back what the last operation wrote must fail
// with err.
func (s *writeScript) readsAs(err error) { s.ops[len(s.ops)-1].readErr = err }

// writeOnly records that what the last operation wrote cannot be read back.
func (s *writeScript) writeOnly() { s.ops[len(s.ops)-1].skipRead = true }

// runGo executes the script with go-aaru, returning the status code of every
// operation (-1000 for failed create/resume).
func (s *writeScript) runGo(path string) []int {
	var img *Image
	var rcs []int
	for _, o := range s.ops {
		var err error
		data := s.blob[o.off : o.off+o.n]
		switch o.kind {
		case "create":
			var opts *Options
			if o.opts != "-" {
				v := ParseOptions(o.opts)
				opts = &v
			}
			img, err = Create(path, o.params, opts)
			if err != nil {
				rcs = append(rcs, -1000)
				continue
			}
		case "resume":
			var opts *Options
			if o.opts != "-" {
				v := ParseOptions(o.opts)
				opts = &v
			}
			img, err = Resume(path, opts)
			if err != nil {
				rcs = append(rcs, -1000)
				continue
			}
		case "settracks":
			err = img.SetTracks(o.tracks)
		case "mtag":
			err = img.WriteMediaTag(MediaTagType(o.tag), data)
		case "ws":
			err = img.WriteSector(o.addr, o.neg, data, o.status)
		case "wl":
			err = img.WriteSectorLong(o.addr, o.neg, data, o.status)
		case "wt":
			err = img.WriteSectorTag(o.addr, o.neg, SectorTagType(o.tag), data)
		case "close":
			err = img.Close()
		}
		rcs = append(rcs, ErrorCode(err))
	}
	return rcs
}

type scenarioCheck struct {
	anyRC  bool // operations may fail (random scripts): their status codes are not checked
	noData bool // the image holds no data and cannot be opened
}

// checkScenario runs s with go-aaru. Every operation must return what the
// script expects (unless c.anyRC), the script must close the image
// successfully, and the image must open, verify and read back with what was
// written (scenarioReadback). It returns the path of the image.
func checkScenario(t *testing.T, s *writeScript, c scenarioCheck) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go.aif")
	rcs := s.runGo(path)
	if !c.anyRC {
		for i, o := range s.ops {
			if i < len(rcs) && rcs[i] != ErrorCode(o.want) {
				t.Errorf("op %d (%s addr %d neg %v tag %d): status %d, want %d (%v)", i, o.kind, o.addr, o.neg, o.tag,
					rcs[i], ErrorCode(o.want), o.want)
			}
		}
	}
	if len(rcs) == 0 || rcs[len(rcs)-1] != 0 {
		t.Fatalf("close failed: status codes %v", rcs)
	}
	if c.noData {
		if _, err := Open(path); err == nil {
			t.Error("image without data opened")
		}
		return path
	}
	img, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	scenarioReadback(t, s, rcs, img)
	return path
}

// sectorKey identifies a sector.
type sectorKey struct {
	neg  bool
	addr uint64
}

// sectorTagKey identifies a sector tag.
type sectorTagKey struct {
	sectorKey
	tag SectorTagType
}

// expectedSector is a sector as the last successful ws or wl wrote it.
type expectedSector struct {
	long    bool
	data    []byte
	status  SectorStatus
	readErr error
}

// scenarioModel is what an image must contain after the successful
// operations of a script.
type scenarioModel struct {
	params     CreateParams
	tracks     []TrackEntry
	mediaTags  map[MediaTagType][]byte
	sectors    map[sectorKey]*expectedSector
	sectorTags map[sectorTagKey][]byte
}

// dvdLongTagOffsets locates the DVD sector tags inside a 2064-byte DVD long
// sector.
var dvdLongTagOffsets = map[SectorTagType]int{
	SectorTagDvdSectorInformation: 0, SectorTagDvdSectorNumber: 1, SectorTagDvdSectorIed: 4, SectorTagDvdCmi: 6,
	SectorTagDvdSectorEdc: 2060,
}

func (s *writeScript) model(rcs []int) *scenarioModel {
	m := &scenarioModel{mediaTags: map[MediaTagType][]byte{}, sectors: map[sectorKey]*expectedSector{},
		sectorTags: map[sectorTagKey][]byte{}}
	for i, o := range s.ops {
		if i >= len(rcs) || rcs[i] != 0 || o.skipRead {
			continue
		}
		data := s.blob[o.off : o.off+o.n]
		k := sectorKey{o.neg, o.addr}
		switch o.kind {
		case "create":
			m.params = o.params
		case "settracks":
			m.tracks = o.tracks
		case "mtag":
			m.mediaTags[MediaTagType(o.tag)] = data
		case "ws":
			m.sectors[k] = &expectedSector{data: data, status: o.status, readErr: o.readErr}
		case "wl":
			m.sectors[k] = &expectedSector{long: true, data: bytes.Clone(data), status: o.status, readErr: o.readErr}
			// A long sector carries the tags it is made of.
			for tk := range m.sectorTags {
				if tk.sectorKey == k && tk.tag != SectorTagCdSubchannel {
					delete(m.sectorTags, tk)
				}
			}
		case "wt":
			tag := SectorTagType(o.tag)
			m.sectorTags[sectorTagKey{k, tag}] = data
			// DVD sector tags are part of the DVD long sector.
			if e, ok := m.sectors[k]; ok && e.long && len(e.data) == 2064 {
				if off, ok := dvdLongTagOffsets[tag]; ok {
					copy(e.data[off:], data)
				}
			}
		}
	}
	return m
}

// appleTagLength is the native tag length of Apple and Priam media (0 for
// other media).
func appleTagLength(mt media.Type) int {
	switch mt {
	case media.AppleFileWare, media.AppleProfile:
		return 20
	case media.AppleSonySS, media.AppleSonyDS:
		return 12
	case media.PriamDataTower:
		return 24
	}
	return 0
}

// readStatus is the status a sector written with st reads back with: console
// sectors stored decrypted are re-encrypted, GameCube/Wii junk is
// regenerated, and both then read as dumped.
func readStatus(mt media.Type, st SectorStatus) SectorStatus {
	switch {
	case st == SectorStatusUnencrypted && (mt == media.PS3DVD || mt == media.PS3BD || mt == media.WOD ||
		mt == media.WUOD):
		return SectorStatusDumped
	case st == SectorStatusGenerable && (mt == media.GOD || mt == media.WOD):
		return SectorStatusDumped
	}
	return st
}

// scenarioReadback checks that img holds what the successful operations of s
// wrote: image info, tracks and track sectors, media tags (every other tag in
// -1..127 must be absent), every written sector and long sector, every
// written sector tag, and the bounds of the sector address space.
func scenarioReadback(t *testing.T, s *writeScript, rcs []int, img *Image) {
	t.Helper()
	m := s.model(rcs)
	p := m.params
	info := img.Info()
	if info.MediaType != p.MediaType || info.MetadataMediaType != p.MediaType.MetadataType() {
		t.Errorf("MediaType/MetadataMediaType = %v/%v, want %v/%v", info.MediaType, info.MetadataMediaType,
			p.MediaType, p.MediaType.MetadataType())
	}
	if !p.Tape && info.Sectors != p.UserSectors {
		t.Errorf("Sectors = %d, want %d", info.Sectors, p.UserSectors)
	}
	if !p.Tape && (img.NegativeSectors() != uint32(p.NegativeSectors) || img.OverflowSectors() != uint32(p.OverflowSectors)) {
		t.Errorf("negative/overflow = %d/%d, want %d/%d", img.NegativeSectors(), img.OverflowSectors(),
			p.NegativeSectors, p.OverflowSectors)
	}
	wantAppVersion := fmt.Sprintf("%d.%d", p.ApplicationMajorVersion, p.ApplicationMinorVersion)
	if info.Version != "2.0" || info.Application != p.Application || info.ApplicationVersion != wantAppVersion {
		t.Errorf("Version/Application/ApplicationVersion = %q/%q/%q, want 2.0/%q/%q", info.Version, info.Application,
			info.ApplicationVersion, p.Application, wantAppVersion)
	}
	if info.CreationTime.IsZero() || info.LastModificationTime.Before(info.CreationTime) {
		t.Errorf("CreationTime/LastModificationTime = %v/%v", info.CreationTime, info.LastModificationTime)
	}

	// Tracks.
	tracks, err := img.Tracks()
	optical := p.MediaType.MetadataType() == media.OpticalDisc
	switch {
	case len(m.tracks) == 0:
		if err == nil {
			t.Errorf("Tracks = %v, want none", tracks)
		}
	case err != nil || len(tracks) != len(m.tracks):
		t.Errorf("Tracks = %v, %v; want %v", tracks, err, m.tracks)
	default:
		for i, w := range m.tracks {
			g := tracks[i]
			if g.Sequence != w.Sequence || g.Type != w.Type || g.Start != w.Start || g.End != w.End ||
				g.Pregap != w.Pregap || g.Session != w.Session {
				t.Errorf("track %d = %+v, want %+v", i, g, w)
			}
		}
		if !info.HasPartitions || !info.HasSessions {
			t.Error("image with tracks has no partitions/sessions")
		}
	}
	for _, tr := range m.tracks {
		if tr.Sequence > maxDataTrackSequence {
			continue
		}
		for _, rel := range []uint64{0, 1, uint64(tr.End - tr.Start)} {
			want, wst, werr := img.ReadSector(uint64(tr.Start)+rel, false)
			got, gst, gerr := img.ReadTrackSector(rel, tr.Sequence)
			if !bytes.Equal(got, want) || gst != wst || ErrorCode(gerr) != ErrorCode(werr) {
				t.Errorf("ReadTrackSector(%d, track %d) = %d bytes %v %v; ReadSector gives %d bytes %v %v", rel,
					tr.Sequence, len(got), gst, gerr, len(want), wst, werr)
			}
		}
	}
	if !optical {
		if _, _, err := img.ReadTrackSector(0, 1); !errors.Is(err, ErrIncorrectMediaType) {
			t.Errorf("ReadTrackSector on %v = %v, want ErrIncorrectMediaType", p.MediaType, err)
		}
	}

	// Media tags.
	_, hasEC := img.ErasureCoding()
	for tag := MediaTagType(-1); tag < 128; tag++ {
		data, err := img.ReadMediaTag(tag)
		want, ok := m.mediaTags[tag]
		switch {
		case ok:
			if err != nil || !bytes.Equal(data, want) {
				t.Errorf("ReadMediaTag(%v) = %d bytes, %v; want the %d bytes written", tag, len(data), err, len(want))
			}
		case tag == -1 && hasEC: // erasure parity blocks have no media tag type
		case !errors.Is(err, ErrMediaTagNotPresent):
			t.Errorf("ReadMediaTag(%v) = %d bytes, %v; want not present", tag, len(data), err)
		}
	}

	// Sectors.
	failures := 0
	fail := func(format string, args ...any) {
		t.Helper()
		if failures++; failures <= 10 {
			t.Errorf(format, args...)
		}
	}
	tagLen := appleTagLength(p.MediaType)
	for k, e := range m.sectors {
		if !e.long {
			got, st, err := img.ReadSector(k.addr, k.neg)
			if want := readStatus(p.MediaType, e.status); err != nil || st != want || !bytes.Equal(got, e.data) {
				fail("ReadSector(%d, neg=%v) = %d bytes, status %v, %v; want %d bytes, status %v", k.addr, k.neg,
					len(got), st, err, len(e.data), want)
			}
			continue
		}
		got, _, err := img.ReadSectorLong(k.addr, k.neg)
		switch {
		case e.readErr != nil:
			if !errors.Is(err, e.readErr) {
				fail("ReadSectorLong(%d, neg=%v) = %d bytes, %v; want %v", k.addr, k.neg, len(got), err, e.readErr)
			}
		case p.MediaType == media.AppleWidget:
			// Widget tags are accepted but not stored.
			if err != nil || !bytes.Equal(got, e.data[:512]) {
				fail("ReadSectorLong(%d, neg=%v) = %d bytes, %v; want the user data alone", k.addr, k.neg, len(got), err)
			}
		case tagLen > 0 && len(e.data) != 512+tagLen:
			// Tags of another Apple format are converted to the native one:
			// only the user data is kept as is.
			if err != nil || len(got) != 512+tagLen || !bytes.Equal(got[:512], e.data[:512]) {
				fail("ReadSectorLong(%d, neg=%v) = %d bytes, %v; want the user data and a %d-byte tag", k.addr, k.neg,
					len(got), err, tagLen)
			}
		default:
			if err != nil || !bytes.Equal(got, e.data) {
				fail("ReadSectorLong(%d, neg=%v) = %d bytes, %v; want the %d bytes written", k.addr, k.neg, len(got),
					err, len(e.data))
			}
		}
	}
	for k, want := range m.sectorTags {
		got, err := img.ReadSectorTag(k.addr, k.neg, k.tag)
		if err != nil || !bytes.Equal(got, want) {
			fail("ReadSectorTag(%d, neg=%v, %v) = %x, %v; want %x", k.addr, k.neg, k.tag, got, err, want)
		}
	}
	if failures > 10 {
		t.Errorf("%d mismatches in total", failures)
	}

	// Bounds.
	if p.Tape {
		return
	}
	end := info.Sectors + uint64(img.OverflowSectors())
	if _, _, err := img.ReadSector(end, false); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("ReadSector(%d) past the end = %v, want ErrSectorOutOfBounds", end, err)
	}
	if _, _, err := img.ReadSectorLong(end, false); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("ReadSectorLong(%d) past the end = %v, want ErrSectorOutOfBounds", end, err)
	}
	neg := uint64(img.NegativeSectors()) + 1
	if _, _, err := img.ReadSector(neg, true); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("ReadSector(%d, negative) past the start = %v, want ErrSectorOutOfBounds", neg, err)
	}
	wrongTag := SectorTagCdSubchannel
	if optical {
		wrongTag = SectorTagAppleSony
	}
	if _, err := img.ReadSectorTag(0, false, wrongTag); !errors.Is(err, ErrIncorrectMediaType) {
		t.Errorf("ReadSectorTag(0, %v) on %v = %v, want ErrIncorrectMediaType", wrongTag, p.MediaType, err)
	}
}

// expectSectors reads the image at path back and checks every sector
// against the last WriteSector of its address in s; sectors never written
// must read as not dumped.
func expectSectors(t *testing.T, s *writeScript, path string) {
	t.Helper()
	type key struct {
		neg  bool
		addr uint64
	}
	want := map[key]writeOp{}
	for _, o := range s.ops {
		if o.kind == "ws" {
			want[key{o.neg, o.addr}] = o
		}
	}
	img, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer img.Close()
	check := func(neg bool, addr uint64) {
		got, st, err := img.ReadSector(addr, neg)
		o, ok := want[key{neg, addr}]
		if !ok {
			if !errors.Is(err, ErrSectorNotDumped) {
				t.Errorf("unwritten sector %d (neg=%v) reads err=%v st=%d", addr, neg, err, st)
			}
			return
		}
		data := s.blob[o.off : o.off+o.n]
		if err != nil || st != o.status || !bytes.Equal(got, data) {
			t.Errorf("sector %d (neg=%v): err=%v st=%d len=%d, want st=%d len=%d", addr, neg, err, st, len(got),
				o.status, len(data))
		}
	}
	for a := uint64(1); a <= uint64(img.NegativeSectors()); a++ {
		check(true, a)
	}
	for a := uint64(0); a < img.info.Sectors+uint64(img.OverflowSectors()); a++ {
		check(false, a)
	}
}

// ---------------------------------------------------------------------------
// Data generators

func randBytes(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// compressible returns data with low entropy (text-like), so that LZMA, Zstd
// and FLAC actually compress it.
func compressible(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	v := byte(r.Uint32())
	for i := range b {
		if r.IntN(8) == 0 {
			v = byte(r.IntN(16)) + 'a'
		}
		b[i] = v
	}
	return b
}

func app() CreateParams {
	return CreateParams{Application: "go-aaru-test", ApplicationMajorVersion: 1, ApplicationMinorVersion: 2}
}
