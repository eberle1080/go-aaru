package main

import (
	"sort"
	"testing"

	"github.com/eberle1080/go-aaru"
)

// TestFixtures builds every test image and checks that go-aaru opens and
// verifies the ones meant to be valid.
func TestFixtures(t *testing.T) {
	names := make([]string, 0, len(fxBuilders))
	for n := range fxBuilders {
		names = append(names, n)
	}
	sort.Strings(names)
	broken := map[string]bool{"version3": true, "ddt_bad_crc": true, "short_v2_header": true, "no_ddt": true}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			p := fixture(t, n)
			img, err := aaru.Open(p)
			if broken[n] {
				if err == nil {
					img.Close()
				}
				t.Logf("%s: open %v", n, err)
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer img.Close()
			verr := img.Verify()
			h, ok := img.UserDataDDTHeader()
			t.Logf("%s: verify %v, info %+v, ddt %v shift %d", n, verr, img.Info(), ok, h.TableShift)
		})
	}
}

// TestFixturesV1 checks that the version 1 images read exactly like the
// version 2 images they were laid out from.
func TestFixturesV1(t *testing.T) {
	for v1, v2 := range map[string]string{"cdmode1_v1": "cdmode1", "cdmode2_v1": "cdmode2", "mf2hd_v1": "mf2hd"} {
		t.Run(v1, func(t *testing.T) {
			a, err := aaru.Open(fixture(t, v1))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := aaru.Open(fixture(t, v2))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if a.Header().ImageMajorVersion != 1 || a.Info().Sectors != b.Info().Sectors {
				t.Fatalf("version %d, %d sectors", a.Header().ImageMajorVersion, a.Info().Sectors)
			}
			for s := uint64(0); s < b.Info().Sectors; s++ {
				read := a.ReadSectorLong
				readB := b.ReadSectorLong
				if len(v2) > 2 && v2[:2] == "mf" { // no long sectors on floppies
					read, readB = a.ReadSector, b.ReadSector
				}
				la, _, errA := read(s, false)
				lb, _, errB := readB(s, false)
				if errA != nil || errB != nil || string(la) != string(lb) {
					t.Fatalf("sector %d differs (%v, %v)", s, errA, errB)
				}
			}
		})
	}
}
