package aaru

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// randomScenario generates a random sequence of write operations (including
// invalid ones) on a random image layout.
func randomScenario(seed uint64) *writeScript {
	r := rand.New(rand.NewPCG(seed, 99))
	s := &writeScript{}
	p := app()
	optical := r.IntN(2) == 0
	compress := r.IntN(3) == 0
	opts := fmt.Sprintf("compress=%v;deduplicate=%v;data_shift=%d;block_alignment=%d", compress, r.IntN(2) == 0,
		1+r.IntN(8), []int{9, 9, 4, 12}[r.IntN(4)])
	// Multi-level scenarios only write sequentially (rewriting flushed
	// secondary tables is covered by TestMultiLevelRevisitedTables).
	multi := r.IntN(3) == 0
	if multi {
		opts += fmt.Sprintf(";table_shift=%d", 2+r.IntN(5))
	}
	if r.IntN(2) == 0 {
		opts += ";md5=true;sha1=true;sha256=true;spamsum=true;blake3=true"
	}
	user := uint64(50 + r.IntN(400))
	neg := uint64(0)
	if r.IntN(2) == 0 {
		neg = uint64(r.IntN(40))
	}
	ovf := uint64(r.IntN(20))
	p.UserSectors, p.NegativeSectors, p.OverflowSectors = user, neg, ovf

	pool := make([][]byte, 16)
	for i := range pool {
		pool[i] = compressible(r, 2352)
	}
	pick := func(n int) []byte {
		if r.IntN(10) == 0 {
			return randBytes(r, n)
		}
		return pool[r.IntN(len(pool))][:n]
	}
	addr := func() (uint64, bool) {
		if multi {
			return 0, false
		}
		if neg > 0 && r.IntN(8) == 0 {
			return uint64(1 + r.IntN(int(neg)+1)), true
		}
		return uint64(r.IntN(int(user + ovf + 1))), false
	}
	statuses := []SectorStatus{SectorStatusDumped, SectorStatusDumped, SectorStatusErrored, SectorStatusUnrecorded,
		SectorStatusEncrypted}

	if optical {
		p.MediaType = media.CDROMXA
		p.SectorSize = 2352
		s.create(p, opts)
		third := int64(user / 3)
		s.setTracks(
			TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: third - 1, Session: 1, Flags: 4},
			TrackEntry{Sequence: 2, Type: TrackTypeCdMode2Formless, Start: third, End: 2*third - 1, Session: 1},
			TrackEntry{Sequence: 3, Type: TrackTypeAudio, Start: 2 * third, End: int64(user+ovf) - 1, Session: 1},
		)
		next := uint64(0)
		for i := 0; i < 300; i++ {
			a, n := addr()
			if multi || r.IntN(3) > 0 { // mostly sequential
				a, n = next, false
				next++
			}
			switch r.IntN(10) {
			case 0:
				s.wt(a, n, SectorTagCdSubchannel, pick(96))
			case 1:
				s.ws(a, n, statuses[r.IntN(len(statuses))], pick(2352))
			default:
				var tt TrackType
				switch {
				case int64(a) < third:
					tt = TrackTypeCdMode1
				case int64(a) < 2*third:
					tt = TrackTypeCdMode2Formless
				default:
					tt = TrackTypeAudio
				}
				lba := int64(a)
				if n {
					lba = -lba
				}
				sec := cdSector(r, tt, lba, r.IntN(2) == 0)
				if r.IntN(4) == 0 {
					sec[r.IntN(2352)] ^= byte(1 + r.IntN(255))
				}
				empty := r.IntN(20) == 0
				if empty {
					sec = make([]byte, 2352)
				}
				s.wl(a, n, SectorStatusDumped, sec)
				if empty && (n || tt != TrackTypeAudio) {
					// An empty data sector is stored as not dumped.
					s.readsAs(ErrSectorNotDumped)
				}
			}
		}
	} else {
		p.MediaType = media.Type(1)
		p.SectorSize = 512
		s.create(p, opts)
		next := uint64(0)
		for i := 0; i < 600; i++ {
			a, n := addr()
			if multi || r.IntN(3) > 0 {
				a, n = next, false
				next++
			}
			size := 512
			if r.IntN(15) == 0 {
				size = []int{256, 1024, 70000}[r.IntN(3)]
			}
			if size > 2352 {
				s.ws(a, n, SectorStatusDumped, make([]byte, size))
				continue
			}
			s.ws(a, n, statuses[r.IntN(len(statuses))], pick(size))
		}
	}
	if r.IntN(2) == 0 {
		s.mediaTag(MediaTagType(r.IntN(70)), pick(100))
	}
	s.close()
	return s
}

func TestScenarioRandom(t *testing.T) {
	n := 40
	if v, err := strconv.Atoi(os.Getenv("AARUGO_RANDOM_SEEDS")); err == nil {
		n = v
	}
	if testing.Short() {
		n = 10
	}
	for seed := uint64(0); seed < uint64(n); seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			s := randomScenario(seed)
			checkScenario(t, s, scenarioCheck{anyRC: true})
		})
	}
}
