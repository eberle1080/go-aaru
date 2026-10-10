package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCoreIdentify(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.aif")
	tests := []struct {
		name string
		path string
		code int
		out  string
	}{
		{"cdmode1", fixture(t, "cdmode1"), 0, "The specified file is a supported AaruFormat image.\n"},
		{"mf2hd_v1", fixture(t, "mf2hd_v1"), 0, "The specified file is a supported AaruFormat image.\n"},
		{"dic_magic", fixture(t, "dic_magic"), 0, "The specified file is a supported AaruFormat image.\n"},
		{"not_an_image", coreNotAnImage(t), 1, "The specified file is not a support AaruFormat image.\n"},
		{"version3", fixture(t, "version3"), 1, "The specified file is not a support AaruFormat image.\n"},
		// A missing file is an error, not "not enough confidence".
		{"missing", missing, int(syscall.ENOENT), "Error 2 while trying to identify file.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, _ := coreRun(t, "identify", tc.path)
			if code != tc.code || out != tc.out {
				t.Errorf("identify = %d %q, want %d %q", code, out, tc.code, tc.out)
			}
		})
	}
}

func TestCoreVerbArgumentErrors(t *testing.T) {
	tests := []struct {
		args      []string
		usage     string // expected start of the usage text on stdout
		stderrHas string
	}{
		{[]string{"identify"}, "\nUsage:\n  aaruformattool identify <filename>\n", "wrong number of arguments"},
		{[]string{"info", "a", "b"}, "\nUsage:\n  aaruformattool info <filename>\n", "wrong number of arguments"},
		{[]string{"read", "x", "img"}, "\nUsage:\n  aaruformattool read <sector_number> <filename>\n", ""},
		{[]string{"read_long", "-1", "img"}, "\nUsage:\n  aaruformattool read_long <sector_number> <filename>\n",
			"flag provided but not defined"},
		{[]string{"verify", "--bogus", "img"}, "\nUsage:\n  aaruformattool verify [--recover] <filename>\n",
			"flag provided but not defined"},
		{[]string{"verify_sectors"}, "\nUsage:\n  aaruformattool verify_sectors <filename>\n", ""},
		{[]string{"compare", "a"}, "\nUsage:\n  aaruformattool compare <filename1> <filename2>\n", ""},
		{[]string{"cli-compare", "--help"}, "\nUsage:\n  aaruformattool cli-compare [-l] <filename1> <filename2>\n", ""},
		{[]string{"convert", "in"}, "\nUsage:\n  aaruformattool convert [-l] [--erasure-coding=K,M] <input> <output>\n", ""},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, errOut := coreRun(t, tc.args...)
			if code != -1 {
				t.Errorf("exit status %d, want -1", code)
			}
			if !strings.HasPrefix(out, tc.usage) {
				t.Errorf("stdout = %q, want usage %q", out, tc.usage)
			}
			if !strings.Contains(errOut, tc.stderrHas) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tc.stderrHas)
			}
		})
	}

	// Invalid --erasure-coding values are rejected before converting.
	for _, ec := range []string{"0,2", "4,0", "4", "x,y", ""} {
		code, out, errOut := coreRun(t, "convert", "--erasure-coding="+ec, "in.aif", "out.aif")
		if code != -1 || out != "" || errOut != "Error: --erasure-coding must be K,M (e.g. 4,2)\n" {
			t.Errorf("convert --erasure-coding=%s = %d %q %q", ec, code, out, errOut)
		}
	}
}

func TestCoreNameTables(t *testing.T) {
	// Spot checks of the name tables.
	checks := []struct{ got, want string }{
		{mediaTypeString(15), "CD-ROM (Yellow Book)"},
		{mediaTypeString(2), "Generic Hard Disk"},
		{mediaTypeString(11), "CD Digital Audio (Red Book)"},
		{mediaTypeString(520), "microSD"},
		{mediaTypeString(0xFFFFFFF), "Unknown Media Type"},
		{mediaTagTypeString(0), "CD Table of Contents"},
		{mediaTagTypeString(-1), "Unknown Media Tag"},
		{dataTypeString(1), "User Data"},
		{dataTypeString(9999), "Unknown Data Type"},
		{sectorTagTypeString(0), "Apple Sony Tag"},
		{sectorTagTypeString(1), "CD Sector Sync"},
		{sectorTagTypeString(1000), "Unknown Sector Tag"},
		{byteArrayToHexString([]byte{0x00, 0x1a, 0xbc, 0xff}), "001ABCFF"},
		{byteArrayToHexString(nil), ""},
	}
	for i, c := range checks {
		if c.got != c.want {
			t.Errorf("check %d: got %q, want %q", i, c.got, c.want)
		}
	}
}
