package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eberle1080/go-aaru"
)

// Test helpers of the core verbs (identify, info, read, read_long, verify,
// verify_sectors, compare, cli-compare, convert). Expected outputs are in
// testdata/expected/core (times printed in UTC, see coreUTC).

// coreBanner is what run prints before any verb.
var coreBanner = fmt.Sprintf("AaruFormat Tool version %d.%d\ngo-aaru version %d.%d\n\n",
	toolMajorVersion, toolMinorVersion, aaru.LibraryMajorVersion, aaru.LibraryMinorVersion)

// coreRun runs the tool like main does and returns its exit status and what
// it wrote to stdout (without the banner) and stderr.
func coreRun(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outF, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errF, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	func() {
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		code = run(args)
	}()
	outF.Close()
	errF.Close()
	o, _ := os.ReadFile(filepath.Join(dir, "stdout"))
	e, _ := os.ReadFile(filepath.Join(dir, "stderr"))
	so := string(o)
	if !strings.HasPrefix(so, coreBanner) {
		t.Fatalf("output does not start with the banner: %q", so[:min(len(so), 200)])
	}
	return code, strings.TrimPrefix(so, coreBanner), string(e)
}

// coreCapture runs body with a coreOutput writing to buffers.
func coreCapture(body func(o *coreOutput) int) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	o := newCoreOutput(&out, &errb)
	code = body(o)
	o.flush()
	return code, out.String(), errb.String()
}

// coreUTC makes info print times in UTC (like the expected outputs) for the test.
func coreUTC(t *testing.T) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
}

// coreCorruptFirstUserDataBlock flips a payload byte of the first user data
// block (DBLK of type UserData) of the image at path.
func coreCorruptFirstUserDataBlock(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const blockHeaderSize = 36
	for off := 0; off+blockHeaderSize+64 < len(b); off++ {
		if string(b[off:off+4]) == "DBLK" && b[off+4] == byte(aaru.DataTypeUserData) && b[off+5] == 0 {
			b[off+blockHeaderSize+16] ^= 0xFF
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("no user data block found")
}

// coreReadAll reads every user sector (plain or long) of an image.
func coreReadAll(t *testing.T, path string, long bool) (data [][]byte, errs []error) {
	t.Helper()
	img, err := aaru.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	n := img.Info().Sectors
	data, errs = make([][]byte, n), make([]error, n)
	for s := uint64(0); s < n; s++ {
		if long {
			data[s], _, errs[s] = img.ReadSectorLong(s, false)
		} else {
			data[s], _, errs[s] = img.ReadSector(s, false)
		}
	}
	return data, errs
}

// coreNotAnImage writes a file of deterministic bytes that is not an image
// and returns its path.
func coreNotAnImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, fxData(99, 0, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
