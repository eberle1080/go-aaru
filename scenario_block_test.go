package aaru

import (
	"errors"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// blockScenario writes n sectors of 512 bytes drawn from a pool of distinct
// sectors (so that deduplication has work to do) plus negative and overflow
// sectors, with mixed statuses.
func blockScenario(seed uint64, opts string, n, neg, ovf uint64, compressibleData bool) *writeScript {
	r := rand.New(rand.NewPCG(seed, 1))
	pool := make([][]byte, 64)
	for i := range pool {
		if compressibleData {
			pool[i] = compressible(r, 512)
		} else {
			pool[i] = randBytes(r, 512)
		}
	}
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	p.UserSectors = n
	p.NegativeSectors = neg
	p.OverflowSectors = ovf
	s.create(p, opts)
	statuses := []SectorStatus{SectorStatusDumped, SectorStatusDumped, SectorStatusDumped, SectorStatusErrored,
		SectorStatusUnrecorded}
	for i := uint64(1); i <= neg; i++ {
		s.ws(i, true, SectorStatusDumped, pool[r.IntN(len(pool))])
	}
	for i := uint64(0); i < n; i++ {
		if i%97 == 13 { // leave holes: never written sectors
			continue
		}
		s.ws(i, false, statuses[r.IntN(len(statuses))], pool[r.IntN(len(pool))])
	}
	for i := uint64(0); i < ovf; i++ {
		s.ws(n+i, false, SectorStatusDumped, pool[r.IntN(len(pool))])
	}
	s.close()
	return s
}

func TestScenarioBlockUncompressed(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=false",
		"deduplicate=true;compress=false",
		"deduplicate=true;compress=false;md5=true;sha1=true;sha256=true;blake3=true;spamsum=true",
		"deduplicate=false;compress=false;data_shift=4;block_alignment=12",
		"deduplicate=true;compress=false;data_shift=3;block_alignment=0",
	} {
		t.Run(opts, func(t *testing.T) {
			s := blockScenario(1, opts, 3000, 0, 0, false)
			expectSectors(t, s, checkScenario(t, s, scenarioCheck{}))
		})
	}
}

func TestScenarioBlockNegativeOverflow(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=false;md5=true",
		"deduplicate=true;compress=false;data_shift=5",
		"deduplicate=true;compress=true",
		"deduplicate=true;compress=true;zstd=true",
	} {
		t.Run(opts, func(t *testing.T) {
			s := blockScenario(2, opts, 2000, 300, 50, true)
			expectSectors(t, s, checkScenario(t, s, scenarioCheck{}))
		})
	}
}

func TestScenarioBlockCompressed(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=true",
		"deduplicate=true;compress=true;md5=true;sha1=true;sha256=true;blake3=true;spamsum=true",
		"deduplicate=true;compress=true;zstd=true;zstd_level=19",
		"deduplicate=true;compress=true;zstd=true;zstd_level=3;data_shift=6",
		"deduplicate=false;compress=true;dictionary=65536",
	} {
		t.Run(opts, func(t *testing.T) {
			checkScenario(t, blockScenario(3, opts, 4000, 0, 0, true), scenarioCheck{})
		})
	}
}

// Random data does not compress: blocks fall back to being stored
// uncompressed, so no Zstandard feature bit may be set.
func TestScenarioBlockIncompressible(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=true",
		"deduplicate=false;compress=true;zstd=true",
		"deduplicate=true;compress=false;zstd=true",
	} {
		t.Run(opts, func(t *testing.T) {
			checkScenario(t, blockScenario(4, opts, 600, 0, 0, false), scenarioCheck{})
		})
	}
}

// Multi-level tables with tiny secondary tables, written sequentially (only
// overflow sectors: negative sectors are stored in reverse table order).
func TestScenarioBlockMultiLevel(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=false;table_shift=4",
		"deduplicate=true;compress=false;table_shift=6;data_shift=3",
		"deduplicate=true;compress=true;table_shift=5",
		"deduplicate=true;compress=true;zstd=true;table_shift=7",
	} {
		t.Run(opts, func(t *testing.T) {
			s := blockScenario(5, opts, 3000, 0, 20, true)
			expectSectors(t, s, checkScenario(t, s, scenarioCheck{}))
		})
	}
}

// Writing again into a secondary table that was already flushed (negative
// sectors, whose tables are visited in reverse order, then the user area;
// or any out-of-order write) reloads the table, so every sector reads back.
func TestMultiLevelRevisitedTables(t *testing.T) {
	for _, opts := range []string{
		"deduplicate=false;compress=false;table_shift=4",
		"deduplicate=true;compress=true;table_shift=5;data_shift=2",
		"deduplicate=true;compress=true;zstd=true;table_shift=3",
	} {
		t.Run(opts, func(t *testing.T) {
			s := blockScenario(9, opts, 1500, 100, 20, true)
			// Out-of-order rewrites across tables.
			r := rand.New(rand.NewPCG(10, 1))
			close := s.ops[len(s.ops)-1]
			s.ops = s.ops[:len(s.ops)-1]
			for i := 0; i < 200; i++ {
				s.ws(uint64(r.IntN(1500)), false, SectorStatusDumped, compressible(r, 512))
			}
			s.ops = append(s.ops, close)

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

// Sector size changes close the current block.
func TestScenarioBlockVariableSectorSize(t *testing.T) {
	r := rand.New(rand.NewPCG(6, 1))
	for _, opts := range []string{"compress=false", "compress=true"} {
		t.Run(opts, func(t *testing.T) {
			s := &writeScript{}
			p := app()
			p.MediaType = media.Type(1)
			p.SectorSize = 1024
			p.UserSectors = 400
			s.create(p, opts)
			for i := uint64(0); i < 400; i++ {
				size := []int{512, 512, 1024, 256, 2048, 513}[i/70]
				s.ws(i, false, SectorStatusDumped, compressible(r, size))
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}

// Error paths: out of bounds addresses, invalid sizes, wrong media.
func TestScenarioBlockErrors(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 1))
	s := &writeScript{}
	p := app()
	p.MediaType = media.Type(1)
	p.SectorSize = 512
	p.UserSectors = 100
	p.NegativeSectors = 10
	p.OverflowSectors = 5
	s.create(p, "compress=false")
	d := randBytes(r, 512)
	s.ws(0, false, SectorStatusDumped, d)
	s.ws(10, true, SectorStatusDumped, d) // = negative count: accepted
	s.ws(11, true, SectorStatusDumped, d) // out of bounds
	s.fails(ErrSectorOutOfBounds)
	s.ws(104, false, SectorStatusDumped, d) // last overflow
	s.ws(105, false, SectorStatusDumped, d) // out of bounds
	s.fails(ErrSectorOutOfBounds)
	s.ws(1, false, SectorStatusDumped, make([]byte, 70000))
	s.fails(ErrInvalidSectorLength)
	s.wl(2, false, SectorStatusDumped, d) // block media without tags
	s.fails(ErrIncorrectMediaType)
	s.wt(3, false, SectorTagCdSubchannel, make([]byte, 96)) // optical tag on block media
	s.fails(ErrIncorrectMediaType)
	s.wt(3, false, SectorTagAppleSony, make([]byte, 11)) // wrong size
	s.fails(ErrIncorrectDataSize)
	s.wt(3, false, SectorTagType(99), make([]byte, 4)) // unknown tag
	s.fails(ErrInvalidTag)
	s.wt(3, false, SectorTagBdSectorEdc, []byte{1, 2, 3, 4}) // accepted on block media, but not readable
	s.writeOnly()
	s.mediaTag(MediaTagCdToc, []byte{1})
	s.close()
	img, err := Open(checkScenario(t, s, scenarioCheck{}))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if _, err := img.ReadSectorTag(3, false, SectorTagBdSectorEdc); !errors.Is(err, ErrInvalidTag) {
		t.Errorf("ReadSectorTag(BD EDC) on block media = %v, want ErrInvalidTag", err)
	}
}

// Media tags, including a replaced one, and a tag type without data type.
func TestScenarioMediaTags(t *testing.T) {
	r := rand.New(rand.NewPCG(8, 1))
	for _, opts := range []string{"compress=false", "compress=true", "compress=true;zstd=true"} {
		t.Run(opts, func(t *testing.T) {
			s := &writeScript{}
			p := app()
			p.MediaType = media.DVDROM
			p.SectorSize = 2048
			p.UserSectors = 16
			s.create(p, opts)
			s.mediaTag(MediaTagType(6), compressible(r, 2048)) // DVD PFI
			s.mediaTag(MediaTagType(7), compressible(r, 2048)) // DVD DMI
			s.mediaTag(MediaTagType(8), randBytes(r, 188))     // BCA
			s.mediaTag(MediaTagType(6), compressible(r, 1000)) // replaces PFI, moves it last
			s.mediaTag(MediaTagType(45), []byte("INQUIRY DATA"))
			for i := uint64(0); i < 16; i++ {
				s.ws(i, false, SectorStatusDumped, compressible(r, 2048))
			}
			s.close()
			checkScenario(t, s, scenarioCheck{})
		})
	}
}
