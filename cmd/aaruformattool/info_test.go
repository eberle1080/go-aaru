package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// infoImages are the test images whose info output is compared with
// testdata/expected/core/info_<name>.txt.
var infoImages = []string{
	"cdmode1", "cdmode2", "cdmode1_v1", "mf2hd", "floptical", "cd_raw", "cd_lzma", "cd_tags", "tiny_cd", "dvd_long",
	"bd_long", "ps3_bd", "blk_ml", "blk_idx2", "blk_ec", "ml_subindex", "apple_sony", "priam", "flux_only",
	"app_name_full", "dic_magic", "meta_checksums", "meta_all", "meta_dumphw", "meta_tape", "meta_resume_title",
	"meta_overread",
}

// TestCoreInfoOutput compares info with the expected outputs.
func TestCoreInfoOutput(t *testing.T) {
	coreUTC(t)
	for _, name := range infoImages {
		t.Run(name, func(t *testing.T) {
			code, out, errOut := coreRun(t, "info", fixture(t, name))
			if code != 0 || errOut != "" {
				t.Fatalf("info = %d, stderr %q", code, errOut)
			}
			checkExpected(t, "core/info_"+name+".txt", out)
		})
	}

	// Colored output (enabled on terminals).
	for _, name := range []string{"cdmode1", "meta_all"} {
		t.Run("color_"+name, func(t *testing.T) {
			path := fixture(t, name)
			code, out, _ := coreCapture(func(o *coreOutput) int { return info(o, path, true) })
			if code != 0 {
				t.Fatalf("info = %d", code)
			}
			checkExpected(t, "core/infocolor_"+name+".txt", out)
			// The colored output has the same lines as the plain one.
			_, plain, _ := coreCapture(func(o *coreOutput) int { return info(o, path, false) })
			if !strings.Contains(out, coreAnsiGreen) || strings.Count(out, "\n") != strings.Count(plain, "\n") {
				t.Errorf("colored output has no colors or a different number of lines")
			}
		})
	}
}

// TestCoreInfoContents checks the values info shows for the test images,
// independently of the stored outputs.
func TestCoreInfoContents(t *testing.T) {
	coreUTC(t)
	info := func(name string) string {
		t.Helper()
		code, out, _ := coreRun(t, "info", fixture(t, name))
		if code != 0 {
			t.Fatalf("info %s = %d", name, code)
		}
		return out
	}
	contains := func(out, s string, want bool) {
		t.Helper()
		if strings.Contains(out, s) != want {
			t.Errorf("output contains %q = %v, want %v", s, !want, want)
		}
	}

	// Header fields.
	out := info("cdmode1")
	for _, s := range []string{
		"│ Format Version:      2.0 ", "│ Identifier:          AARUFRMT ", "│ Application:         go-aaru-test ",
		"│ Created:             2024-01-17 21:20:00 ", "│ Last Modified:       2024-01-17 22:20:00 ",
		"│ Media Type:          CD-ROM (Yellow Book) ", "│ Total Sectors:       600 ", "│ Sector Size:         2048 bytes ",
		"│ Has Partitions:      Yes ", "Track #1 : Seq=1 Type=2 Start=0 End=599 Session=1",
		"CD Table of Contents (12 bytes)",
	} {
		contains(out, s, true)
	}

	// Version 1 headers store the application name in UTF-16.
	out = info("cdmode1_v1")
	contains(out, "│ Application:         go-aaru-test ", true)
	contains(out, "│ Format Version:      1.0 ", true)

	// The full 64-byte application name and the DiscImageChef identifier.
	contains(info("app_name_full"), "│ Application:         0123456789012345678901234567890123456789012345678901", true)
	contains(info("dic_magic"), "│ Identifier:          DICMFRMT ", true)

	// Geometry.
	contains(info("mf2hd"), "│ Geometry:            C:80 / H:2 / S:18 ", true)

	// Flux captures are listed.
	out = info("flux_only")
	contains(out, "Image contains 1 flux captures.\nFlux capture details:\n\tStatistics:\n\t\tUnique heads: 1\n", true)
	contains(out, "\t\t\tIndex resolution: 25000 picoseconds (25.000 nanoseconds)\n", true)

	// The Apple Sony and Priam readable tags are named correctly.
	contains(info("apple_sony"), "│  Apple Sony Tag  ", true)
	contains(info("priam"), "│  Priam DataTower Tag  ", true)

	// Erasure coding parity blocks are not media tags.
	out = info("blk_ec")
	contains(out, "MEDIA TAGS", false)
	for _, s := range []string{
		"│ Algorithm Reed-Solomon (Vandermonde)", "│ Data blocks per stripe (K) 4 ",
		"│ Parity blocks per stripe (M) 2 ", "│ Parity overhead 50.0% (before compression) ",
		"│ Fault tolerance Tolerates up to 2 corrupted block(s) per stripe ",
	} {
		contains(out, s, true)
	}

	// Metadata, dump hardware, checksums and media tags.
	out = info("meta_all")
	for _, s := range []string{"│ Media Sequence:      2 of 3 ", "│ Creator:             Creator value ", "│ Media Title:         MediaTitle value ",
		"DUMP HARDWARE (1 device(s))", "│   Manufacturer:      Maker ", "│   Extent:            0 - 119 "} {
		contains(out, s, true)
	}
	contains(info("meta_dumphw"), "│   Extents:           4 extent(s) ", true)
	out = info("meta_checksums")
	for _, s := range []string{"│ MD5:", "│ SHA-1:", "│ SHA-256:", "│ SpamSum:"} {
		contains(out, s, true)
	}
	contains(info("cd_raw"), "MEDIA TAGS (4 total)", true)

	// A metadata block that ends on an alignment boundary, followed by a short
	// index, is shown.
	contains(info("meta_overread"), "│ Creator:             cccc", true)

	// An index that references a subindex from the middle of its entry list,
	// and a version 2 index.
	contains(info("ml_subindex"), "│ Table Shift:              2 (2^2 = 4 sectors/entry)", true)
	contains(info("blk_idx2"), "│ Total Sectors:       600 ", true)
}

func TestCoreInfoOpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.aif")
	code, out, _ := coreRun(t, "info", missing)
	if code != 2 || out != "\n❌ Error: Cannot open AaruFormat image (error code: 2)\n\n" {
		t.Errorf("info missing = %d %q", code, out)
	}
	code, out, _ = coreRun(t, "info", coreNotAnImage(t))
	if code != -1 || out != "\n❌ Error: Cannot open AaruFormat image (error code: -1)\n\n" {
		t.Errorf("info on a non-image = %d %q", code, out)
	}
	code, out, _ = coreCapture(func(o *coreOutput) int { return info(o, missing, true) })
	if code != 2 || out != coreAnsiRed+"\n❌ Error: Cannot open AaruFormat image (error code: 2)\n\n"+coreAnsiReset+coreAnsiReset {
		t.Errorf("info missing (color) = %d %q", code, out)
	}
}

func TestCoreInfoHelpers(t *testing.T) {
	coreUTC(t)
	if got := infoFormatFiletime(0); got != "0" {
		t.Errorf("infoFormatFiletime(0) = %q", got)
	}
	if got := infoFormatFiletime(116444736000000000 + 10000000*86400); got != "1970-01-02 00:00:00" {
		t.Errorf("infoFormatFiletime(1 day) = %q", got)
	}
	if got := infoIdentifierString(0x544D524655524141); got != "AARUFRMT" {
		t.Errorf("identifier = %q", got)
	}
	if got := infoIdentifierString(0x41); got != "       A" {
		t.Errorf("short identifier = %q", got)
	}
	// Fields are truncated to fit the box.
	_, out, _ := coreCapture(func(o *coreOutput) int {
		(&infoPrinter{o: o}).field("Label:", strings.Repeat("x", 100), 20)
		return 0
	})
	if want := "│ " + "Label:" + strings.Repeat(" ", 15) + strings.Repeat("x", 56) + " │\n"; out != want {
		t.Errorf("long field = %q, want %q", out, want)
	}
	if got := coreTruncateUTF8("aé", 2); got != "a" {
		t.Errorf("coreTruncateUTF8 = %q", got)
	}
}
