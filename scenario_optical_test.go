package aaru

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/media"
)

// cdSector builds a raw 2352-byte CD sector of the given track type for
// lba, with a correct prefix and suffix and user data from fill.
func cdSector(r *rand.Rand, tt TrackType, lba int64, form2 bool) []byte {
	s := make([]byte, 2352)
	switch tt {
	case TrackTypeAudio:
		return compressibleAudio(r, 2352)
	case TrackTypeCdMode1:
		copy(s[16:], compressible(r, 2048))
		_ = cdecc.ReconstructPrefix(s, cdecc.TrackTypeCdMode1, lba)
		_ = cdecc.Reconstruct(s, cdecc.TrackTypeCdMode1)
	default:
		sub := []byte{1, 2, 0x08, 0}
		if form2 {
			sub[2] |= 0x20
		}
		copy(s[16:], sub)
		copy(s[20:], sub)
		if form2 {
			copy(s[24:], compressible(r, 2324))
			_ = cdecc.ReconstructPrefix(s, cdecc.TrackTypeCdMode2Form2, lba)
			_ = cdecc.Reconstruct(s, cdecc.TrackTypeCdMode2Form2)
		} else {
			copy(s[24:], compressible(r, 2048))
			_ = cdecc.ReconstructPrefix(s, cdecc.TrackTypeCdMode2Form1, lba)
			_ = cdecc.Reconstruct(s, cdecc.TrackTypeCdMode2Form1)
		}
	}
	return s
}

// compressibleAudio returns a smooth 16-bit stereo signal.
func compressibleAudio(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	phase := r.Float64()
	for i := 0; i+3 < n; i += 4 {
		v := int16(8000 * sin(phase+float64(i)/400))
		w := int16(6000 * sin(phase*2+float64(i)/250))
		binary.LittleEndian.PutUint16(b[i:], uint16(v+int16(r.IntN(16))))
		binary.LittleEndian.PutUint16(b[i+2:], uint16(w+int16(r.IntN(16))))
	}
	return b
}

// sin is a cheap sine approximation (Bhaskara), enough for test signals.
func sin(x float64) float64 {
	const pi = 3.141592653589793
	x -= float64(int64(x/(2*pi))) * 2 * pi
	if x < 0 {
		x += 2 * pi
	}
	sign := 1.0
	if x > pi {
		x -= pi
		sign = -1
	}
	return sign * 16 * x * (pi - x) / (5*pi*pi - 4*x*(pi-x))
}

// cdScenario writes a CD with Mode 1, Mode 2 and audio tracks, including
// every kind of anomaly the long sector writer distinguishes.
func cdScenario(seed uint64, opts string, withSubchannel bool) *writeScript {
	r := rand.New(rand.NewPCG(seed, 2))
	s := &writeScript{}
	p := app()
	p.MediaType = media.CDROMXA
	p.SectorSize = 2352
	p.UserSectors = 300
	p.NegativeSectors = 20
	p.OverflowSectors = 10
	s.create(p, opts)
	s.setTracks(
		TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 119, Session: 1, Flags: 4},
		TrackEntry{Sequence: 2, Type: TrackTypeCdMode2Formless, Start: 120, End: 239, Session: 1, Flags: 4},
		TrackEntry{Sequence: 3, Type: TrackTypeAudio, Start: 240, End: 309, Session: 1},
	)
	s.mediaTag(MediaTagCdToc, compressible(r, 100))

	// Lead-in: no track covers it, stored as data.
	for i := uint64(1); i <= 20; i++ {
		s.wl(i, true, SectorStatusDumped, cdSector(r, TrackTypeCdMode1, -int64(i), false))
	}

	for i := uint64(0); i < 310; i++ {
		lba := int64(i)
		var sec []byte
		switch {
		case i < 120:
			sec = cdSector(r, TrackTypeCdMode1, lba, false)
			switch i % 10 {
			case 1: // bad sync
				sec[3] = 0x12
			case 2: // wrong address
				sec[14] ^= 0x01
			case 3: // non-BCD alias of the right address (e.g. 0x0F for frame 15)
				if _, _, f := cdecc.LBAToMSF(lba); f >= 10 && f%10 < 6 {
					sec[14] = byte(f/10-1)<<4 | byte(f%10+10)
				}
			case 4: // bad suffix
				sec[2100] ^= 0xFF
			case 5: // empty (not dumped)
				sec = make([]byte, 2352)
			case 6: // bad reserved bytes
				sec[0x815] = 1
			}
		case i < 240:
			form2 := i%3 == 0
			sec = cdSector(r, TrackTypeCdMode2Formless, lba, form2)
			switch i % 11 {
			case 1: // Form 2 without EDC
				if form2 {
					binary.LittleEndian.PutUint32(sec[2348:], 0)
				}
			case 2: // errored suffix
				if form2 {
					binary.LittleEndian.PutUint32(sec[2348:], 0xDEADBEEF)
				} else {
					sec[2200] ^= 0x55
				}
			case 3: // disagreeing subheaders: second copy says Form 2
				sec[18] &^= 0x20
				sec[22] |= 0x20
			case 4:
				sec = make([]byte, 2352)
			case 5: // bad prefix mode byte
				sec[15] = 0x01
			case 6: // Form 1 with wrong EDC but right ECC
				if !form2 {
					sec[0x818] ^= 1
				}
			}
		default:
			sec = cdSector(r, TrackTypeAudio, lba, false)
		}
		st := SectorStatusDumped
		if i == 77 {
			st = SectorStatusErrored
		}
		s.wl(i, false, st, sec)
		if i < 240 && bytes.Equal(sec, make([]byte, 2352)) {
			// An empty data sector is stored as not dumped.
			s.readsAs(ErrSectorNotDumped)
		}
	}

	if withSubchannel {
		for i := uint64(1); i <= 20; i++ {
			s.wt(i, true, SectorTagCdSubchannel, compressible(r, 96))
		}
		for i := uint64(0); i < 310; i += 1 + uint64(i%3) {
			s.wt(i, false, SectorTagCdSubchannel, compressible(r, 96))
		}
	}
	s.close()
	return s
}

func TestScenarioCD(t *testing.T) {
	for _, c := range []struct {
		opts string
		sub  bool
	}{
		{"compress=false;deduplicate=false", false},
		{"compress=false;deduplicate=true", true},
		{"compress=false;deduplicate=true;md5=true;sha1=true", true},
		{"compress=true;deduplicate=true", true},
		{"compress=true;deduplicate=false;zstd=true", true},
	} {
		t.Run(c.opts, func(t *testing.T) {
			checkScenario(t, cdScenario(11, c.opts, c.sub), scenarioCheck{})
		})
	}
}

// Audio CD: FLAC compressed audio blocks, JaguarCD and VideoNow exceptions.
func TestScenarioAudio(t *testing.T) {
	for _, c := range []struct {
		name    string
		mt      media.Type
		session uint8
		opts    string
	}{
		{"cdda-flac", media.CDDA, 1, "compress=true"},
		{"cdda-uncompressed", media.CDDA, 1, "compress=false"},
		{"cdda-flac-dedup-small-blocks", media.CDDA, 1, "compress=true;data_shift=3"},
		{"jaguar-session2-lzma", media.JaguarCD, 2, "compress=true"},
		{"jaguar-session1-flac", media.JaguarCD, 1, "compress=true"},
		{"videonow-lzma", media.VideoNow, 1, "compress=true"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(12, 2))
			s := &writeScript{}
			p := app()
			p.MediaType = c.mt
			p.SectorSize = 2352
			p.UserSectors = 100
			s.create(p, c.opts)
			s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeAudio, Start: 0, End: 99, Session: c.session})
			for i := uint64(0); i < 100; i++ {
				data := compressibleAudio(r, 2352)
				if i%17 == 3 {
					data = make([]byte, 2352) // silence
				}
				if i%23 == 5 {
					data = randBytes(r, 2352) // noise: FLAC cannot shrink it alone
				}
				s.wl(i, false, SectorStatusDumped, data)
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// Track flags and ISRC written as sector tags after the tracks were set.
func TestScenarioTrackTags(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 2))
	s := &writeScript{}
	p := app()
	p.MediaType = media.CDDA
	p.SectorSize = 2352
	p.UserSectors = 40
	s.create(p, "compress=false")
	s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeAudio, Start: 0, End: 19, Session: 1},
		TrackEntry{Sequence: 2, Type: TrackTypeAudio, Start: 20, End: 39, Session: 1})
	s.wt(25, false, SectorTagCdTrackFlags, []byte{0x08})
	s.wt(3, false, SectorTagCdTrackIsrc, []byte("USABC1234567"))
	s.wt(45, false, SectorTagCdTrackFlags, []byte{1}) // out of bounds
	s.fails(ErrSectorOutOfBounds)
	s.wt(3, false, SectorTagCdTrackIsrc, []byte("SHORT")) // wrong size
	s.fails(ErrIncorrectDataSize)
	for i := uint64(0); i < 40; i++ {
		s.wl(i, false, SectorStatusDumped, compressibleAudio(r, 2352))
	}
	s.close()
	// The tracks CRC is updated after the track tags, so the image verifies
	// (checkScenario).
	checkScenario(t, s, scenarioCheck{})
}

// DVD long sectors (ID, IED, CPR_MAI, EDC) and DVD sector tags.
func TestScenarioDVD(t *testing.T) {
	for _, opts := range []string{"compress=false", "compress=true;md5=true", "compress=true;zstd=true"} {
		t.Run(opts, func(t *testing.T) {
			r := rand.New(rand.NewPCG(14, 2))
			s := &writeScript{}
			p := app()
			p.MediaType = media.DVDROM
			p.SectorSize = 2048
			p.UserSectors = 200
			p.NegativeSectors = 10
			p.OverflowSectors = 5
			s.create(p, opts)
			for i := uint64(1); i <= 10; i++ {
				s.wl(i, true, SectorStatusDumped, append(randBytes(r, 12), append(compressible(r, 2048), randBytes(r, 4)...)...))
			}
			for i := uint64(0); i < 205; i++ {
				long := append(randBytes(r, 12), append(compressible(r, 2048), randBytes(r, 4)...)...)
				s.wl(i, false, SectorStatusDumped, long)
				if i%5 == 0 {
					s.wt(i, false, SectorTagDvdTitleKeyDecrypted, randBytes(r, 5))
				}
				if i%7 == 0 {
					s.wt(i, false, SectorTagDvdCmi, randBytes(r, 1))
					s.wt(i, false, SectorTagDvdSectorInformation, randBytes(r, 1))
					s.wt(i, false, SectorTagDvdSectorNumber, randBytes(r, 3))
					s.wt(i, false, SectorTagDvdSectorIed, randBytes(r, 2))
					s.wt(i, false, SectorTagDvdSectorEdc, randBytes(r, 4))
				}
			}
			s.wl(3, false, SectorStatusDumped, make([]byte, 2052)) // wrong length
			s.fails(ErrIncorrectDataSize)
			s.wt(3, false, SectorTagDvdSectorIed, randBytes(r, 3)) // wrong length
			s.fails(ErrIncorrectDataSize)
			s.mediaTag(MediaTagType(6), compressible(r, 2048))
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// DVD title keys and sector tags without long sectors (only some of the
// four DVD buffers exist: the DVD blocks are all-or-nothing).
func TestScenarioDVDTagsOnly(t *testing.T) {
	r := rand.New(rand.NewPCG(15, 2))
	s := &writeScript{}
	p := app()
	p.MediaType = media.DVDROM
	p.SectorSize = 2048
	p.UserSectors = 50
	s.create(p, "compress=false")
	for i := uint64(0); i < 50; i++ {
		s.ws(i, false, SectorStatusDumped, compressible(r, 2048))
		s.wt(i, false, SectorTagDvdTitleKeyDecrypted, randBytes(r, 5))
		s.wt(i, false, SectorTagDvdSectorIed, randBytes(r, 2))
		s.writeOnly() // the IED alone is not stored
	}
	s.close()
	checkScenario(t, s, scenarioCheck{})
}

// Blu-ray long sectors (2048 bytes + 4 bytes EDC).
func TestScenarioBD(t *testing.T) {
	for _, opts := range []string{"compress=false", "compress=true"} {
		t.Run(opts, func(t *testing.T) {
			r := rand.New(rand.NewPCG(16, 2))
			s := &writeScript{}
			p := app()
			p.MediaType = media.BDROM
			p.SectorSize = 2048
			p.UserSectors = 100
			s.create(p, opts)
			for i := uint64(0); i < 100; i++ {
				s.wl(i, false, SectorStatusDumped, append(compressible(r, 2048), randBytes(r, 4)...))
			}
			s.wt(3, false, SectorTagBdSectorEdc, randBytes(r, 4)) // only accepted on block media
			s.fails(ErrIncorrectMediaType)
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}
