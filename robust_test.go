package aaru

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// Robustness: truncated, corrupted and fuzzed images must produce errors,
// never panics, hangs or huge allocations.

// rdRobustSeedNames lists the small images (built by rdRobustSeed) covering
// every reader feature: version 1 and version 2 block media, CD with
// prefix/suffix/subchannel (version 1 correction tables, version 2 tables),
// multi-level DDT with negative and overflow sectors.
var rdRobustSeedNames = []string{"floptical_v1", "hifd_v1", "cdmode1_v1", "floptical", "gigamo", "tiny_cd", "tiny_ml"}

// rdScriptBytes runs s and returns the image.
func rdScriptBytes(tb testing.TB, s *writeScript) []byte {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "seed.aif")
	for i, rc := range s.runGo(path) {
		if rc != 0 {
			tb.Fatalf("op %d (%s): rc %d", i, s.ops[i].kind, rc)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// rdBlockScript writes n sectors of size bytes on media mt.
func rdBlockScript(mt media.Type, size uint32, n int, opts string) *writeScript {
	s := &writeScript{}
	p := app()
	p.MediaType = mt
	p.SectorSize = size
	p.UserSectors = uint64(n)
	s.create(p, opts)
	for i, d := range seededSectors(uint64(n), n, int(size), 3) {
		s.ws(uint64(i), false, SectorStatusDumped, d)
	}
	s.close()
	return s
}

// rdTinyCD is a 12-sector CD with lead-in, lead-out, subchannel, a Mode 1
// track and an audio track.
func rdTinyCD() *writeScript {
	r := rand.New(rand.NewPCG(50, 6))
	s := &writeScript{}
	p := app()
	p.MediaType = media.CD
	p.SectorSize = 2352
	p.UserSectors = 12
	p.NegativeSectors = 2
	p.OverflowSectors = 2
	s.create(p, "compress=false")
	s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 7, Session: 1, Flags: 4},
		TrackEntry{Sequence: 2, Type: TrackTypeAudio, Start: 8, End: 13, Session: 1})
	s.mediaTag(MediaTagCdToc, compressible(r, 40))
	for i := uint64(1); i <= 2; i++ {
		s.ws(i, true, SectorStatusDumped, compressible(r, 2352))
	}
	for i := uint64(0); i < 14; i++ {
		tt := TrackTypeCdMode1
		if i >= 8 {
			tt = TrackTypeAudio
		}
		sec := cdSector(r, tt, int64(i), false)
		if i == 3 {
			sec[2100] ^= 0xFF // stored suffix
		}
		s.wl(i, false, SectorStatusDumped, sec)
		s.wt(i, false, SectorTagCdSubchannel, compressible(r, 96))
	}
	s.close()
	return s
}

// rdRobustSeed builds the seed image name.
func rdRobustSeed(tb testing.TB, name string) []byte {
	tb.Helper()
	switch name {
	case "floptical_v1":
		b, _ := v1BlockImage(media.Floptical, 512, seededSectors(51, 100, 512, 3), CompressionNone)
		return b
	case "hifd_v1":
		b, _ := v1BlockImage(media.HiFD, 512, seededSectors(52, 400, 512, 3), CompressionLzma)
		return b
	case "cdmode1_v1":
		b, _ := v1CDImage(rawCDSectors(53, 24, false, map[int]bool{3: true, 4: true}), false, map[int]bool{9: true})
		return b
	case "floptical":
		return rdScriptBytes(tb, rdBlockScript(media.Floptical, 512, 300, "compress=true"))
	case "gigamo":
		return rdScriptBytes(tb, rdBlockScript(media.GigaMo, 2048, 100, "compress=true;data_shift=4"))
	case "tiny_cd":
		return rdScriptBytes(tb, rdTinyCD())
	case "tiny_ml":
		return rdScriptBytes(tb, blockScenario(54, "compress=false;table_shift=2", 64, 4, 4, true))
	}
	tb.Fatalf("unknown seed %q", name)
	return nil
}

// rdExercise calls every read entry point on img, bounded to limit
// sectors per range. Errors are expected; only panics matter.
func rdExercise(img *Image, limit uint64) {
	info := img.Info()
	neg := min(uint64(img.userDataDDTHeader.Negative)+1, limit)
	pos := min(info.Sectors+uint64(img.userDataDDTHeader.Overflow)+1, limit)
	tags := []SectorTagType{SectorTagAppleSony, SectorTagCdSubchannel, SectorTagCdTrackFlags, SectorTagCdTrackIsrc,
		SectorTagDvdSectorNumber, SectorTagDvdSectorIed, SectorTagPriamDataTower, SectorTagCdSync}
	for n := uint64(1); n <= neg; n++ {
		img.ReadSector(n, true)
		img.ReadSectorLong(n, true)
		img.ReadSectorTag(n, true, tags[n%uint64(len(tags))])
	}
	for a := uint64(0); a < pos; a++ {
		img.ReadSector(a, false)
		img.ReadSectorLong(a, false)
		img.ReadSectorTag(a, false, tags[a%uint64(len(tags))])
	}
	img.ReadSector(1<<40, false)
	img.ReadSectorLong(^uint64(0), true)
	for t := MediaTagType(-1); t < 90; t++ {
		img.ReadMediaTag(t)
	}
	if tracks, err := img.Tracks(); err == nil {
		for _, t := range tracks {
			img.ReadTrackSector(0, t.Sequence)
			img.ReadTrackSector(1, t.Sequence)
		}
	}
	img.Verify()
	img.Close()
}

func rdOpenBytes(b []byte) (*Image, error) {
	return OpenReader(bytes.NewReader(b), int64(len(b)))
}

func TestOpenTruncated(t *testing.T) {
	for _, name := range rdRobustSeedNames {
		data := rdRobustSeed(t, name)
		step := max(1, len(data)/300)
		cuts := []int{0, 1, 7, 8, headerV1Size - 1, headerV1Size, headerSize - 1, headerSize, len(data) - 1}
		for n := 0; n < len(data); n += step {
			cuts = append(cuts, n)
		}
		for _, n := range cuts {
			if n < 0 || n > len(data) {
				continue
			}
			img, err := rdOpenBytes(data[:n])
			if err != nil {
				continue
			}
			if n < len(data)-1 && n < headerSize {
				t.Errorf("%s truncated to %d bytes opened", name, n)
			}
			rdExercise(img, 64)
		}
	}
}

func TestOpenCorrupted(t *testing.T) {
	iterations := 400
	if testing.Short() {
		iterations = 50
	}
	r := rand.New(rand.NewPCG(1, 2))
	for _, name := range rdRobustSeedNames {
		orig := rdRobustSeed(t, name)
		for i := 0; i < iterations; i++ {
			data := append([]byte(nil), orig...)
			switch i % 4 {
			case 0: // flip random bytes
				for k := 0; k < 1+r.IntN(8); k++ {
					data[r.IntN(len(data))] ^= byte(1 + r.IntN(255))
				}
			case 1: // overwrite a random 4-byte word with an extreme value
				p := r.IntN(len(data) - 4)
				binary.LittleEndian.PutUint32(data[p:], []uint32{0, 0xFFFFFFFF, 0x7FFFFFFF, 0x80000000, uint32(r.Uint64())}[r.IntN(5)])
			case 2: // overwrite a random 8-byte word (offsets, counts)
				p := r.IntN(len(data) - 8)
				binary.LittleEndian.PutUint64(data[p:], []uint64{0, ^uint64(0), 1 << 40, uint64(len(data)), r.Uint64()}[r.IntN(5)])
			case 3: // zero a random range
				p := r.IntN(len(data))
				clear(data[p:min(len(data), p+1+r.IntN(256))])
			}
			img, err := rdOpenBytes(data)
			if err != nil {
				continue
			}
			rdExercise(img, 48)
		}
	}
}

// TestOpenHostileHeaders crafts headers with extreme values.
func TestOpenHostileHeaders(t *testing.T) {
	base := rdRobustSeed(t, "tiny_ml")
	mutate := func(f func(b []byte)) []byte {
		b := append([]byte(nil), base...)
		f(b)
		return b
	}
	cases := map[string][]byte{
		"index offset past EOF":   mutate(func(b []byte) { binary.LittleEndian.PutUint64(b[80:], uint64(len(b))+10) }),
		"index offset max":        mutate(func(b []byte) { binary.LittleEndian.PutUint64(b[80:], ^uint64(0)) }),
		"version 3":               mutate(func(b []byte) { b[72] = 3 }),
		"unknown incompat":        mutate(func(b []byte) { binary.LittleEndian.PutUint64(b[139:], 0x2) }),
		"bad magic, no footer":    mutate(func(b []byte) { b[0] = 'X' }),
		"only header":             base[:headerSize],
		"huge index entry count":  mutate(func(b []byte) { binary.LittleEndian.PutUint64(b[binary.LittleEndian.Uint64(b[80:])+4:], 1<<60) }),
		"self-referencing subidx": selfSubindex(base),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if img, err := rdOpenBytes(data); err == nil {
				rdExercise(img, 64)
				t.Fatalf("opened")
			}
		})
	}
	// Unknown read-only compatible features open for reading only.
	ro := mutate(func(b []byte) { binary.LittleEndian.PutUint64(b[131:], 0x80) })
	img, err := rdOpenBytes(ro)
	if err != nil {
		t.Fatalf("unknown RO-compat feature should open read-only: %v", err)
	}
	img.Close()
}

// selfSubindex makes the IDX3 index of b point to itself as a subindex.
func selfSubindex(base []byte) []byte {
	b := append([]byte(nil), base...)
	off := binary.LittleEndian.Uint64(b[80:])
	e := off + indexHeader3Size
	binary.LittleEndian.PutUint32(b[e:], uint32(BlockTypeIndexBlock3))
	binary.LittleEndian.PutUint64(b[e+6:], off)
	return b
}

// FuzzOpenReader opens arbitrary bytes as an image and exercises every read
// entry point on whatever opens.
func FuzzOpenReader(f *testing.F) {
	for _, name := range rdRobustSeedNames {
		f.Add(rdRobustSeed(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Keep allocations driven by plausible compression ratios bounded.
		if len(data) > 1<<16 {
			return
		}
		img, err := rdOpenBytes(data)
		if err != nil {
			return
		}
		rdExercise(img, 32)
	})
}
