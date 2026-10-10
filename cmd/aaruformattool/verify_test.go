package main

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru/cdecc"
)

func TestCoreVerify(t *testing.T) {
	tests := []struct {
		image string
		code  int
		out   string
	}{
		{"cdmode1", 0, "Image blocks contain no errors.\n"},
		{"cdmode1_v1", 0, "Image blocks contain no errors.\n"},
		{"mf2hd_v1", 0, "Image blocks contain no errors.\n"},
		{"blk_ec", 0, "Image blocks contain no errors.\n"},
		{"blk_idx2", 0, "Image blocks contain no errors.\n"},
		{"bad_blocks", -18, "A block contains an invalid CRC value.\n"},
		{"ddt_bad_crc", -18, "Error -18 when opening AaruFormat image.\n"},
		{"flux_only", 0, "Image blocks contain no errors.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.image, func(t *testing.T) {
			code, out, _ := coreRun(t, "verify", fixture(t, tc.image))
			if code != tc.code || out != tc.out {
				t.Errorf("verify = %d %q, want %d %q", code, out, tc.code, tc.out)
			}
		})
	}

	corrupt := fixtureCopy(t, "blk_raw")
	coreCorruptFirstUserDataBlock(t, corrupt)
	code, out, _ := coreRun(t, "verify", corrupt)
	if code != -18 || out != "A block contains an invalid CRC value.\n" {
		t.Errorf("verify corrupted = %d %q", code, out)
	}

	code, out, _ = coreRun(t, "verify", filepath.Join(t.TempDir(), "missing.aif"))
	if code != 2 || out != "Error 2 when opening AaruFormat image.\n" {
		t.Errorf("verify missing = %d %q", code, out)
	}
}

func TestCoreVerifyRecover(t *testing.T) {
	// Sectors 300-309 of blk_ec were never dumped.
	var unreadable strings.Builder
	for s := 300; s < 310; s++ {
		fmt.Fprintf(&unreadable, "  Sector %d: unrecoverable (error 1)\n", s)
	}
	want := "Erasure coding recovery data found (K=4, M=2).\nVerifying all sectors with recovery...\n" +
		unreadable.String() +
		"\nVerification complete:\n  Total sectors:   400\n  Readable:        390\n  Unrecoverable:   10\n" +
		"WARNING: 10 sectors could not be recovered.\n"
	ec := fixture(t, "blk_ec")
	code, out, _ := coreRun(t, "verify", "--recover", ec)
	if code != -1 || out != want {
		t.Errorf("verify --recover = %d\n%s", code, out)
	}

	// A damaged data block is reconstructed from the parity.
	damaged := fixtureCopy(t, "blk_ec")
	coreCorruptFirstUserDataBlock(t, damaged)
	if code, out, _ := coreRun(t, "verify", damaged); code != -18 {
		t.Errorf("verify damaged = %d %q", code, out)
	}
	code, out, _ = coreRun(t, "verify", "--recover", damaged)
	if code != -1 || out != want {
		t.Errorf("verify --recover damaged = %d\n%s", code, out)
	}
	orig, _ := coreReadAll(t, ec, false)
	recovered, _ := coreReadAll(t, damaged, false)
	for s := range orig {
		if string(orig[s]) != string(recovered[s]) {
			t.Fatalf("sector %d not recovered", s)
		}
	}
	for s := 0; s < 300; s++ {
		if string(orig[s]) != string(fxBlockSector(uint64(s))) {
			t.Fatalf("sector %d does not read as written", s)
		}
	}

	code, out, _ = coreRun(t, "verify", "--recover", fixture(t, "cdmode1"))
	if code != -1 || out != "Image does not contain erasure coding recovery data.\n" {
		t.Errorf("verify --recover without EC = %d %q", code, out)
	}
}

func TestCoreVerifySectors(t *testing.T) {
	summary := func(sectors, errs, unknowns int) string {
		return fmt.Sprintf("Total sectors........... %d\nTotal errors............ %d\nTotal unknowns.......... %d\n"+
			"Total errors+unknowns... %d\n", sectors, errs, unknowns, errs+unknowns)
	}

	// Mode 1 and Mode 2 (Form 1 and Form 2) discs, version 2 and 1.
	for _, img := range []string{"cdmode1", "cdmode2", "cdmode1_v1", "cdmode2_v1"} {
		code, out, errOut := coreRun(t, "verify_sectors", fixture(t, img))
		want := "\rAll sector checksums are correct.\n" + summary(600, 0, 0)
		if code != 0 || !strings.HasSuffix(out, want) || errOut != "" {
			t.Errorf("%s: verify_sectors = %d, tail %q, stderr %q", img, code, out[max(0, len(out)-300):], errOut)
		}
	}

	// tiny_cd: Mode 1 sectors 0-5 (3 with a damaged ECC byte, 4 with a damaged
	// header) and audio sectors 6-11.
	code, out, errOut := coreRun(t, "verify_sectors", fixture(t, "tiny_cd"))
	if code != 0 {
		t.Errorf("tiny_cd: exit status %d", code)
	}
	var want strings.Builder
	for s := 0; s < 12; s++ {
		fmt.Fprintf(&want, "\rVerifying sector %d...", s)
		switch {
		case s == 3:
			want.WriteString("\rSector 3 has an incorrect ECC P value.\n\rSector 3 has an incorrect ECC Q value.\n")
		case s == 4:
			want.WriteString("\rSector 4 has an incorrect EDC value.\n\rSector 4 has an incorrect ECC P value.\n" +
				"\rSector 4 has an incorrect ECC Q value.\n")
		case s >= 6:
			fmt.Fprintf(&want, "\rSector %d cannot be verified.\n", s)
		}
	}
	want.WriteString("\rSome sectors had incorrect checksums.\n" + summary(12, 2, 6))
	if out != want.String() {
		t.Errorf("tiny_cd: verify_sectors output\n%q\nwant\n%q", out, want.String())
	}
	wantErr := "Mode 1 sector at address:  0: 2: 3, fails ECC P check.\n" +
		"Mode 1 sector at address:  0: 2: 3, fails ECC Q check.\n"
	if !strings.HasPrefix(errOut, wantErr) || !strings.Contains(errOut, "Mode 1 sector at address: 10: 2: 4, got CRC") {
		t.Errorf("tiny_cd: stderr %q", errOut)
	}

	// Not dumped sectors are reported on stderr and not counted. cd_gap's
	// sectors 5 and 9 have damaged data, and its audio track (400-799) cannot
	// be verified.
	code, out, errOut = coreRun(t, "verify_sectors", fixture(t, "cd_gap"))
	if code != 0 || !strings.HasSuffix(out, "\rSome sectors had incorrect checksums.\n"+summary(800, 2, 400)) ||
		!strings.Contains(errOut, "\rError 1 reading sector 87.\n") {
		t.Errorf("cd_gap: %d %q %q", code, out[max(0, len(out)-200):], errOut[:min(len(errOut), 200)])
	}

	code, out, _ = coreRun(t, "verify_sectors", fixture(t, "mf2hd"))
	if code != 0 || out != "Image sectors do not contain checksums, cannot verify.\n" {
		t.Errorf("mf2hd: verify_sectors = %d %q", code, out)
	}
}

// coreCDSector builds a raw sector of the given track type for lba with
// correct sync, header, EDC and ECC.
func coreCDSector(t *testing.T, tt cdecc.TrackType, lba int64, fill byte) []byte {
	t.Helper()
	s := make([]byte, cdecc.SectorSize)
	for i := 0x10; i < len(s); i++ {
		s[i] = fill + byte(i)
	}
	if tt == cdecc.TrackTypeCdMode2Form1 || tt == cdecc.TrackTypeCdMode2Form2 {
		sub := []byte{0, 0, 0x08, 0}
		if tt == cdecc.TrackTypeCdMode2Form2 {
			sub[2] = 0x20
		}
		copy(s[0x10:], sub)
		copy(s[0x14:], sub)
	}
	if err := cdecc.ReconstructPrefix(s, tt, lba); err != nil {
		t.Fatal(err)
	}
	if err := cdecc.Reconstruct(s, tt); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCoreCheckCDSectorChannel(t *testing.T) {
	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }
	check := func(s []byte) (bool, cdSectorCheck) {
		warnings = nil
		return checkCDSectorChannel(s, warn)
	}

	mode1 := coreCDSector(t, cdecc.TrackTypeCdMode1, 100, 1)
	if ok, r := check(mode1); !ok || r != (cdSectorCheck{hasEDC: true, edcCorrect: true, hasECCP: true,
		eccPCorrect: true, hasECCQ: true, eccQCorrect: true}) || len(warnings) != 0 {
		t.Errorf("mode 1: %v %+v %q", ok, r, warnings)
	}
	bad := append([]byte(nil), mode1...)
	bad[0x200] ^= 1
	if ok, r := check(bad); ok || r.edcCorrect || r.eccPCorrect || r.eccQCorrect || len(warnings) != 3 {
		t.Errorf("mode 1 damaged data: %v %+v %q", ok, r, warnings)
	}
	bad = append([]byte(nil), mode1...)
	bad[0x815] = 1
	if ok, r := check(bad); ok || r.hasEDC || len(warnings) != 1 ||
		!strings.HasPrefix(warnings[0], "Mode 1 with data in reserved bytes") {
		t.Errorf("mode 1 reserved: %v %+v %q", ok, r, warnings)
	}

	// Mode 2 Form 1: the ECC uses a zero header, so a different address must
	// not matter, and the EDC is the full 32-bit value at 0x818.
	form1 := coreCDSector(t, cdecc.TrackTypeCdMode2Form1, 200, 2)
	if ok, r := check(form1); !ok || !r.hasEDC || !r.edcCorrect || !r.eccPCorrect || !r.eccQCorrect {
		t.Errorf("form 1: %v %+v %q", ok, r, warnings)
	}
	bad = append([]byte(nil), form1...)
	bad[0x819] ^= 0x40 // second EDC byte (the ECC covers it too)
	if ok, r := check(bad); ok || r.edcCorrect {
		t.Errorf("form 1 EDC high byte: %v %+v %q", ok, r, warnings)
	}

	// Mode 2 Form 2: EDC over 0x010-0x92B stored at 0x92C.
	form2 := coreCDSector(t, cdecc.TrackTypeCdMode2Form2, 300, 3)
	if binary.LittleEndian.Uint32(form2[0x92C:]) == 0 {
		t.Fatal("test sector has no EDC")
	}
	if ok, r := check(form2); !ok || r != (cdSectorCheck{hasEDC: true, edcCorrect: true}) || len(warnings) != 0 {
		t.Errorf("form 2: %v %+v %q", ok, r, warnings)
	}
	bad = append([]byte(nil), form2...)
	bad[0x900] ^= 1
	if ok, r := check(bad); ok || !r.hasEDC || r.edcCorrect || len(warnings) != 1 {
		t.Errorf("form 2 damaged: %v %+v %q", ok, r, warnings)
	}
	noEDC := append([]byte(nil), bad...)
	binary.LittleEndian.PutUint32(noEDC[0x92C:], 0)
	if ok, r := check(noEDC); !ok || r.hasEDC {
		t.Errorf("form 2 without EDC: %v %+v", ok, r)
	}
	bad = append([]byte(nil), form2...)
	bad[0x14] ^= 0xFF
	if check(bad); len(warnings) != 2 || !strings.HasPrefix(warnings[0], "Subheader copies differ") {
		t.Errorf("form 2 subheader mismatch: %q", warnings)
	}

	// Mode 0, unknown modes, missing sync, short buffers.
	mode0 := make([]byte, cdecc.SectorSize)
	copy(mode0, mode1[:0x0F])
	if ok, _ := check(mode0); !ok {
		t.Errorf("mode 0 rejected: %q", warnings)
	}
	mode0[0x500] = 1
	if ok, _ := check(mode0); ok || len(warnings) != 1 || !strings.HasPrefix(warnings[0], "Mode 0 sector with error") {
		t.Errorf("mode 0 with data: %q", warnings)
	}
	mode3 := append([]byte(nil), mode1...)
	mode3[0x0F] = 3
	if ok, r := check(mode3); ok || r.unknown || len(warnings) != 1 || !strings.HasPrefix(warnings[0], "Unknown mode 3") {
		t.Errorf("mode 3: %v %+v %q", ok, r, warnings)
	}
	audio := make([]byte, cdecc.SectorSize)
	if ok, r := check(audio); ok || !r.unknown {
		t.Errorf("audio: %v %+v", ok, r)
	}
	if ok, r := check(mode1[:2064]); ok || !r.unknown {
		t.Errorf("short sector: %v %+v", ok, r)
	}
}
