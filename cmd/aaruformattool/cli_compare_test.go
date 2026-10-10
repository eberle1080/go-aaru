package main

import (
	"strings"
	"testing"
)

// TestCoreCLICompareOutput compares cli-compare's stdout and stderr with the
// expected outputs. The images are given as paths relative to the fixtures
// directory, so the output does not depend on where it is.
func TestCoreCLICompareOutput(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		long bool
		code int
	}{
		{"blk_raw_ec", "blk_raw", "blk_ec", false, 1},
		{"blk_raw_ec_long", "blk_raw", "blk_ec", true, 0},
		{"tiny_cd_cd_raw", "tiny_cd", "cd_raw", false, 1},
		{"tiny_cd_cd_raw_long", "tiny_cd", "cd_raw", true, 1},
		{"statuses_blk_raw", "statuses", "blk_raw", false, 1},
		{"cd_raw_lzma_long", "cd_raw", "cd_lzma", true, 0},
		{"apple_sony_profile_long", "apple_sony", "apple_profile", true, 1},
		{"wii_gamecube", "wii", "gamecube", false, 1},
		{"nonexistent", "nonexistent", "blk_raw", false, -1},
	}
	for _, tc := range tests {
		for _, n := range []string{tc.a, tc.b} {
			if n != "nonexistent" {
				fixture(t, n)
			}
		}
	}
	// The tool runs from the fixtures directory; the outputs are checked
	// afterwards, from the package directory.
	type result struct {
		code     int
		out, err string
	}
	results := make([]result, len(tests))
	t.Run("run", func(t *testing.T) {
		t.Chdir(fxDir)
		for i, tc := range tests {
			args := []string{"cli-compare", tc.a + ".aif", tc.b + ".aif"}
			if tc.long {
				args = []string{"cli-compare", "-l", tc.a + ".aif", tc.b + ".aif"}
			}
			r := &results[i]
			r.code, r.out, r.err = coreRun(t, args...)
		}
	})
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := results[i]
			if r.code != tc.code {
				t.Errorf("exit status %d, want %d", r.code, tc.code)
			}
			checkExpected(t, "core/clicompare_"+tc.name+".out", r.out)
			checkExpected(t, "core/clicompare_"+tc.name+".err", r.err)
		})
	}
}

func TestCoreCLICompareIdentical(t *testing.T) {
	img := fixture(t, "cdmode2")
	for _, long := range []bool{false, true} {
		args := []string{"cli-compare", img, img}
		if long {
			args = []string{"cli-compare", "-l", img, img}
		}
		code, out, _ := coreRun(t, args...)
		if code != 0 || !coreContainsAll(out, "Different sectors: 0\n", "Identical sectors: 600\n", "✓ Images are identical!\n") {
			t.Errorf("long=%v: %d %q", long, code, out[max(0, len(out)-200):])
		}
	}
	code, _, errOut := coreRun(t, "cli-compare", img, fixture(t, "mf2hd"))
	if code != -1 || errOut != "Warning: Images have different number of sectors\nError: Images have different sector sizes (2324 vs 512)\n" {
		t.Errorf("different sector sizes: %d %q", code, errOut)
	}
}

// coreContainsAll reports whether s contains every substring.
func coreContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
