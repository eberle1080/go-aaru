package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCoreReadOutput compares read and read_long with the expected outputs
// and checks that the dumped bytes are the sector's contents.
func TestCoreReadOutput(t *testing.T) {
	tests := []struct {
		verb   string
		image  string
		sector uint64
		code   int
	}{
		{"read", "cdmode1", 16, 0},
		{"read_long", "cdmode1", 16, 0},
		{"read", "cdmode2", 0, 0},
		{"read_long", "cdmode2", 2, 0}, // Mode 2 Form 2
		{"read", "cdmode1_v1", 16, 0},
		{"read_long", "cdmode2_v1", 0, 0},
		{"read", "mf2hd", 0, 0},
		{"read", "mf2hd_v1", 2879, 0},
		{"read_long", "dvd_long", 5, 0},
		{"read_long", "bd_long", 3, 0},
		{"read_long", "apple_profile", 1, 0},
		{"read_long", "apple_sony", 1, 0},
		{"read_long", "priam", 1, 0},
		{"read", "blk_raw", 100000, -5}, // out of bounds
		{"read", "blk_ec", 300, 1},      // not dumped
		{"read", "tiny_cd", 7, 0},
		{"read_long", "tiny_cd", 3, 0}, // damaged ECC, stored as written
		{"read", "flux_only", 0, -37},  // no user data
	}
	for _, tc := range tests {
		name := fmt.Sprintf("%s_%s_%d", tc.verb, tc.image, tc.sector)
		t.Run(name, func(t *testing.T) {
			path := fixture(t, tc.image)
			code, out, _ := coreRun(t, tc.verb, fmt.Sprint(tc.sector), path)
			if code != tc.code {
				t.Errorf("exit status %d, want %d", code, tc.code)
			}
			checkExpected(t, "core/"+name+".txt", out)
			if code != 0 {
				return
			}
			data, _ := coreReadAll(t, path, tc.verb == "read_long")
			if got := coreParseHexDump(t, out); !bytes.Equal(got, data[tc.sector]) {
				t.Errorf("dumped %d bytes that differ from the %d-byte sector", len(got), len(data[tc.sector]))
			}
		})
	}
}

// coreParseHexDump returns the bytes of a printhex dump (16 bytes per row)
// in the output of read.
func coreParseHexDump(t *testing.T, out string) []byte {
	t.Helper()
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	var data []byte
	for _, line := range strings.Split(ansi.ReplaceAllString(out, ""), "\n") {
		if len(line) < 11 || line[6:9] != "   " {
			continue
		}
		if _, err := strconv.ParseUint(line[:6], 16, 32); err != nil {
			continue
		}
		for i := 0; i < 16 && 9+3*i+2 <= len(line); i++ {
			b, err := strconv.ParseUint(line[9+3*i:9+3*i+2], 16, 8)
			if err != nil {
				break
			}
			data = append(data, byte(b))
		}
	}
	return data
}

func TestCoreReadArguments(t *testing.T) {
	// Sector numbers accept a 0x prefix.
	cd := fixture(t, "cdmode1")
	_, hex, _ := coreRun(t, "read", "0x10", cd)
	_, dec, _ := coreRun(t, "read", "16", cd)
	if hex != dec || !strings.HasPrefix(dec, "Sector is 2048 bytes.\n") {
		t.Errorf("read 0x10 differs from read 16")
	}
	code, out, _ := coreRun(t, "read", "0", filepath.Join(t.TempDir(), "missing.aif"))
	if code != 2 || out != "Error 2 when opening AaruFormat image.\n" {
		t.Errorf("read missing = %d %q", code, out)
	}
	code, out, _ = coreRun(t, "read_long", "0", coreNotAnImage(t))
	if code != -1 || out != "Error -1 when opening AaruFormat image.\n" {
		t.Errorf("read_long on a non-image = %d %q", code, out)
	}
}

func TestCorePrintHex(t *testing.T) {
	data := []byte("AB\x00\x7f\x80\xa0\xff~ z")
	var b bytes.Buffer
	printhex(&b, data, 4, false)
	want := "Offset   00 01 02 03\n" +
		"000000   41 42 00 7F   AB..\n" +
		"000004   80 A0 FF 7E   .\xa0\xff~\n" +
		// The last row is padded with three spaces per missing byte.
		"000008   20 7A" + strings.Repeat("   ", 2) + "   " + " z\n"
	if b.String() != want {
		t.Errorf("printhex =\n%q\nwant\n%q", b.String(), want)
	}

	b.Reset()
	printhex(&b, []byte{1, 2}, 16, true)
	want = "\x1b[36mOffset   00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f\x1b[0m\n" +
		"\x1b[36m000000\x1b[0m   01 02" + strings.Repeat("   ", 14) + "   ..\n"
	if b.String() != want {
		t.Errorf("printhex color =\n%q\nwant\n%q", b.String(), want)
	}

	b.Reset()
	printhex(&b, nil, 16, false)
	if b.Len() != 0 {
		t.Errorf("printhex(nil) wrote %q", b.String())
	}
}
