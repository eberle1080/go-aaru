package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notExistText is how this OS describes a missing file in the tool's system
// error texts ("No such file or directory" on POSIX systems; Windows reports
// a different text). Expected outputs (see checkExpected) are adapted with
// portable so the tests pass on every OS.
var notExistText = func() string {
	_, err := os.Open(filepath.Join(os.TempDir(), "go-aaru-test-definitely-missing", "x"))
	if err == nil {
		return "No such file or directory"
	}
	return maintStrerror(err)
}()

func portable(s string) string {
	return strings.ReplaceAll(s, "No such file or directory", notExistText)
}

func TestPortableNotExistText(t *testing.T) {
	if notExistText == "" {
		t.Fatal("empty not-exist text")
	}
}
