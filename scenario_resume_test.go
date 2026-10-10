package aaru

import (
	"bytes"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// resumeScenario writes sectors [0, split) in a first session and
// [split, n) after resuming.
func resumeScenario(seed uint64, createOpts, resumeOpts string, n, split uint64) *writeScript {
	r := rand.New(rand.NewPCG(seed, 4))
	pool := make([][]byte, 32)
	for i := range pool {
		pool[i] = compressible(r, 512)
	}
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	p.UserSectors = n
	s.create(p, createOpts)
	s.mediaTag(MediaTagType(45), []byte("first session"))
	for i := uint64(0); i < split; i++ {
		s.ws(i, false, SectorStatusDumped, pool[r.IntN(len(pool))])
	}
	s.close()
	s.resume(resumeOpts)
	for i := split; i < n; i++ {
		s.ws(i, false, SectorStatusDumped, pool[r.IntN(len(pool))])
	}
	s.mediaTag(MediaTagType(47), []byte("second session"))
	s.close()
	return s
}

func TestScenarioResume(t *testing.T) {
	for _, c := range []struct {
		name               string
		createOpts, resume string
		n, split           uint64
	}{
		{"single-uncompressed", "compress=false;deduplicate=false", "compress=false;deduplicate=false", 2000, 1000},
		{"single-dedup", "compress=false", "compress=false", 2000, 777},
		{"single-lzma", "compress=true", "compress=true", 2000, 1000},
		{"zstd-then-resume", "compress=true;zstd=true", "compress=true;zstd=true", 2000, 1000},
		{"multi-aligned", "compress=false;table_shift=4", "compress=false", 2000, 1024},
		{"multi-aligned-lzma", "compress=true;table_shift=5", "compress=true", 2000, 1024},
		{"nothing-new", "compress=false", "compress=false", 500, 500},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := resumeScenario(30, c.createOpts, c.resume, c.n, c.split)
			expectSectors(t, s, checkScenario(t, s, scenarioCheck{}))
		})
	}
}

// Resuming a multi-level image in the middle of a secondary table: the
// table written by the first session must be reloaded.
func TestResumeMultiLevelMidTable(t *testing.T) {
	for _, opts := range []string{"compress=false;table_shift=6", "compress=true;table_shift=5;deduplicate=false"} {
		t.Run(opts, func(t *testing.T) {
			s := resumeScenario(31, opts, "compress=true", 1500, 700)
			path := filepath.Join(t.TempDir(), "go.aif")
			for i, rc := range s.runGo(path) {
				if rc != 0 {
					t.Fatalf("op %d: rc %d", i, rc)
				}
			}
			expectSectors(t, s, path)
		})
	}
}

// CD prefix/suffix arenas across sessions.
func TestScenarioResumeCD(t *testing.T) {
	for _, opts := range []string{"compress=false", "compress=true"} {
		t.Run(opts, func(t *testing.T) {
			r := rand.New(rand.NewPCG(32, 4))
			s := &writeScript{}
			p := app()
			p.MediaType = media.CDROMXA
			p.SectorSize = 2048
			p.UserSectors = 60
			s.create(p, "deduplicate=false;"+opts)
			s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 29, Session: 1, Flags: 4},
				TrackEntry{Sequence: 2, Type: TrackTypeCdMode2Formless, Start: 30, End: 59, Session: 1, Flags: 4})
			write := func(from, to uint64) {
				for i := from; i < to; i++ {
					tt := TrackTypeCdMode1
					if i >= 30 {
						tt = TrackTypeCdMode2Formless
					}
					sec := cdSector(r, tt, int64(i), i%2 == 0)
					switch i % 4 {
					case 1:
						sec[5] ^= 0xFF // junk prefix
					case 2:
						sec[2340] ^= 0xFF // junk suffix
					}
					s.wl(i, false, SectorStatusDumped, sec)
				}
			}
			write(0, 20)
			write(30, 40)
			s.close()
			s.resume(opts)
			write(20, 30)
			write(40, 60)
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// DVD long sectors written after resuming must reach the image.
func TestResumeDVDLongSectors(t *testing.T) {
	r := rand.New(rand.NewPCG(33, 4))
	s := &writeScript{}
	p := app()
	p.MediaType = media.DVDROM
	p.SectorSize = 2048
	p.UserSectors = 20
	s.create(p, "compress=false")
	ids := make([][]byte, 20)
	write := func(from, to uint64) {
		for i := from; i < to; i++ {
			long := append(randBytes(r, 12), append(compressible(r, 2048), randBytes(r, 4)...)...)
			ids[i] = long[:4]
			s.wl(i, false, SectorStatusDumped, long)
		}
	}
	write(0, 10)
	s.close()
	s.resume("compress=false")
	write(10, 20)
	s.close()
	path := filepath.Join(t.TempDir(), "go.aif")
	for i, rc := range s.runGo(path) {
		if rc != 0 {
			t.Fatalf("op %d: rc %d", i, rc)
		}
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for i := uint64(0); i < 20; i++ {
		// DVD sector number: bytes 1-3 of the ID.
		got, err := img.ReadSectorTag(i, false, SectorTagDvdSectorNumber)
		if err != nil || !bytes.Equal(got, ids[i][1:4]) {
			t.Errorf("sector %d: sector number %x (%v), want %x", i, got, err, ids[i][1:4])
		}
	}
}
