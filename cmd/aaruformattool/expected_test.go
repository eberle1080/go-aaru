package main

// Snapshot support: expected tool outputs are stored under testdata/expected
// and were produced by running this tool. Run the tests with -update to
// rewrite them after an intended output change:
//
//	go test ./cmd/aaruformattool -run <Test> -update

import (
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the expected outputs under testdata/expected")

// expectedDir is where the expected outputs live.
var expectedDir = filepath.Join("testdata", "expected")

// checkExpected compares got with testdata/expected/<name> (or rewrites the
// file with -update). The OS's missing-file text is adapted with portable.
func checkExpected(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(expectedDir, filepath.FromSlash(name))
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected output %s: %v (run with -update to create it)", name, err)
	}
	want := portable(string(b))
	if got == want {
		return
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < max(len(g), len(w)); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			t.Errorf("output differs from %s at line %d:\n got: %q\nwant: %q", path, i+1, gl, wl)
			return
		}
	}
	t.Errorf("output differs from %s", path)
}

// Fixed header values stamped into test images so that their contents (and
// the tool's output) do not depend on when the test runs.
const (
	stampCreationTime    int64 = 133500000000000000 // 2024-01-17 21:20:00 UTC
	stampLastWrittenTime int64 = 133500036000000000 // 2024-01-17 22:20:00 UTC
)

var stampGUID = [16]byte{0x61, 0x61, 0x72, 0x75, 0x67, 0x6f, 0x2d, 0x74, 0x65, 0x73, 0x74, 0x2d, 0x67, 0x75, 0x69, 0x64}

// Offsets of the header fields that go-aaru fills with the current time and a
// random GUID (the header has no checksum).
const (
	headerCreationTimeOff    = 88
	headerLastWrittenTimeOff = 96
	headerGUIDOff            = 104
)

// stampHeader replaces the creation and last written times (and, for
// version 2 headers, the GUID) of the image at path with fixed values.
func stampHeader(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var hdr [headerGUIDOff + 16]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(hdr[headerCreationTimeOff:], uint64(stampCreationTime))
	binary.LittleEndian.PutUint64(hdr[headerLastWrittenTimeOff:], uint64(stampLastWrittenTime))
	n := headerGUIDOff
	if hdr[72] >= 2 { // ImageMajorVersion: version 1 headers end at the GUID offset
		copy(hdr[headerGUIDOff:], stampGUID[:])
		n += 16
	}
	if _, err := f.WriteAt(hdr[:n], 0); err != nil {
		t.Fatal(err)
	}
}
