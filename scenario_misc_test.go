package aaru

import (
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/media"
)

// Apple Lisa/Macintosh and Priam long sectors: tags are converted to the
// media's native format and always LZMA compressed.
func TestScenarioAppleTags(t *testing.T) {
	for _, c := range []struct {
		mt   media.Type
		opts string
	}{
		{media.AppleProfile, "compress=false"},
		{media.AppleFileWare, "compress=true"},
		{media.AppleSonyDS, "compress=false"},
		{media.AppleSonySS, "compress=false;zstd=true"},
		{media.PriamDataTower, "compress=true"},
		{media.AppleWidget, "compress=false"},
	} {
		t.Run(c.mt.String()+"/"+c.opts, func(t *testing.T) {
			r := rand.New(rand.NewPCG(20, 3))
			s := &writeScript{}
			p := app()
			p.MediaType = c.mt
			p.SectorSize = 512
			p.UserSectors = 120
			s.create(p, c.opts)
			for i := uint64(0); i < 120; i++ {
				tagLen := []int{12, 20, 24, 0}[i%4]
				tag := randBytes(r, tagLen)
				if tagLen > 0 {
					// Plausible chained block numbers so conversions are lossless-ish.
					tag[0] = 0
				}
				s.wl(i, false, SectorStatusDumped, append(compressible(r, 512), tag...))
			}
			s.wl(5, false, SectorStatusDumped, make([]byte, 600)) // bad tag size
			s.fails(ErrIncorrectDataSize)
			s.wl(5, false, SectorStatusDumped, make([]byte, 100)) // shorter than a sector
			s.fails(ErrIncorrectDataSize)
			switch c.mt {
			case media.AppleProfile, media.AppleFileWare:
				s.wt(7, false, SectorTagAppleProfile, randBytes(r, 20))
			case media.PriamDataTower:
				s.wt(7, false, SectorTagPriamDataTower, randBytes(r, 24))
			default:
				s.wt(7, false, SectorTagAppleSony, randBytes(r, 12))
				if c.mt == media.AppleWidget {
					s.writeOnly() // Widget tags are accepted but not stored
				}
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// Long sectors on media without a long format.
func TestScenarioLongUnsupportedMedia(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 3))
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	p.UserSectors = 4
	s.create(p, "compress=false")
	s.wl(0, false, SectorStatusDumped, randBytes(r, 532))
	s.fails(ErrIncorrectMediaType)
	s.ws(1, false, SectorStatusDumped, randBytes(r, 512))
	s.close()
	checkScenario(t, s, scenarioCheck{})
}

// Nintendo GameCube/Wii: 2064-byte long sectors and generable (junk)
// sectors, which only get a table entry.
func TestScenarioNintendo(t *testing.T) {
	for _, mt := range []media.Type{media.GOD, media.WOD} {
		t.Run(mt.String(), func(t *testing.T) {
			r := rand.New(rand.NewPCG(22, 3))
			s := &writeScript{}
			p := app()
			p.MediaType = mt
			p.SectorSize = 2048
			p.UserSectors = 64
			s.create(p, "compress=true;md5=true")
			for i := uint64(0); i < 64; i++ {
				switch {
				case i%8 == 3:
					s.ws(i, false, SectorStatusGenerable, make([]byte, 2048))
				case i%2 == 0:
					s.wl(i, false, SectorStatusDumped, append(randBytes(r, 6), append(compressible(r, 2048),
						randBytes(r, 10)...)...))
				default:
					s.ws(i, false, SectorStatusDumped, compressible(r, 2048))
				}
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// PlayStation 3: sectors written as SectorStatusUnencrypted are stored
// decrypted (outside the plaintext regions) and re-encrypted on read.
func TestScenarioPS3(t *testing.T) {
	for _, keyTag := range []MediaTagType{MediaTagPs3DiscKey, MediaTagPs3Data1} {
		t.Run(keyTag.String(), func(t *testing.T) {
			r := rand.New(rand.NewPCG(23, 3))
			s := &writeScript{}
			p := app()
			p.MediaType = media.PS3BD
			p.SectorSize = 2048
			p.UserSectors = 64
			s.create(p, "compress=true")
			key := randBytes(r, 16)
			s.mediaTag(keyTag, key)
			m := ps3.EncryptionMap{{StartSector: 0, EndSector: 9}, {StartSector: 40, EndSector: 44}}
			mb, _ := m.MarshalBinary()
			s.mediaTag(MediaTagPs3EncryptionMap, mb)
			var dk [16]byte
			if keyTag == MediaTagPs3DiscKey {
				copy(dk[:], key)
			} else {
				dk = ps3.DeriveDiscKey([16]byte(key))
			}
			for i := uint64(0); i < 64; i++ {
				plain := compressible(r, 2048)
				st := SectorStatusDumped
				if i%3 != 0 {
					st = SectorStatusUnencrypted
					if m.IsSectorEncrypted(i) {
						_ = ps3.EncryptSector(dk, i, plain)
					}
				}
				s.ws(i, false, st, plain)
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// Flux-only images (all sector counts zero) reject sector writes.
func TestScenarioFluxOnly(t *testing.T) {
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	s.create(p, "compress=false")
	s.ws(0, false, SectorStatusDumped, make([]byte, 512))
	s.fails(ErrUserDataNotPresent)
	s.wl(0, false, SectorStatusDumped, make([]byte, 512))
	s.fails(ErrUserDataNotPresent)
	s.wt(0, false, SectorTagAppleSony, make([]byte, 12))
	s.fails(ErrUserDataNotPresent)
	s.mediaTag(MediaTagType(45), []byte("inquiry"))
	s.close()
	checkScenario(t, s, scenarioCheck{noData: true})
}

// Tape images: every block must read back.
func TestTapeImageReadBack(t *testing.T) {
	r := rand.New(rand.NewPCG(24, 3))
	for _, opts := range []string{"compress=false;deduplicate=false", "compress=true"} {
		t.Run(opts, func(t *testing.T) {
			s := &writeScript{}
			p := app()
			p.MediaType = media.Type(1)
			p.SectorSize = 512
			p.UserSectors = 1000
			p.Tape = true
			s.create(p, opts)
			for i := uint64(0); i < 300; i++ {
				if i == 17 {
					continue
				}
				s.ws(i, false, SectorStatusDumped, compressible(r, 512))
			}
			s.close()
			path := filepath.Join(t.TempDir(), "tape.aif")
			for i, rc := range s.runGo(path) {
				if rc != 0 {
					t.Fatalf("op %d: rc %d", i, rc)
				}
			}
			expectSectors(t, s, path)
		})
	}
}
