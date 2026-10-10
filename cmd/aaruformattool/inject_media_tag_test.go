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

func runInject(tag, tagFile, image string) (stdout, stderr string, rc int) {
	var out, errOut bytes.Buffer
	rc = injectMediaTag(&out, &errOut, tag, tagFile, image)
	return out.String(), errOut.String(), rc
}

// tagTestData returns deterministic media tag contents.
func tagTestData(seed, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(seed*31 + i*7 + i>>8)
	}
	return b
}

// injectedTag reads a media tag back with go-aaru.
func injectedTag(t *testing.T, path string, tag aaru.MediaTagType) []byte {
	t.Helper()
	img, err := aaru.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("verify: %v", err)
	}
	data, err := img.ReadMediaTag(tag)
	if err != nil {
		t.Errorf("reading media tag %d: %v", tag, err)
	}
	return data
}

func TestParseMediaTagType(t *testing.T) {
	for _, c := range []struct {
		in  string
		tag aaru.MediaTagType
		ok  bool
	}{
		{"0", aaru.MediaTagCdToc, true},
		{"8", aaru.MediaTagDvdCmi, true},
		{"  +8", aaru.MediaTagDvdCmi, true},
		{"86", aaru.MediaTagAacsVolumeUniqueKey, true},
		{"76", aaru.MediaTagWiiUDiscKey, true}, // valid number, no data type
		{"87", 0, false},
		{"-1", 0, false},
		{"8 ", 0, false},
		{"0x8", 0, false},
		{"", 0, false},
		{" ", 0, false},
		{"+", 0, false},
		{"99999999999999999999", 0, false},
		{"MediaTagDvdCmi", aaru.MediaTagDvdCmi, true},
		{"MEDIATAGDVDCMI", aaru.MediaTagDvdCmi, true},
		{"mediatagcdatip", aaru.MediaTagCdAtip, true},
		{"DvdCmi", 0, false},
		{"DVD_CMI", 0, false},
		{"MediaTagWiiUDiscKey", 0, false}, // not in the name table
		{"MediaTagMiniDiscType", aaru.MediaTagMiniDiscType, true},
	} {
		tag, ok := parseMediaTagType(c.in)
		if ok != c.ok || tag != c.tag {
			t.Errorf("parseMediaTagType(%q) = %d, %v; want %d, %v", c.in, tag, ok, c.tag, c.ok)
		}
	}
}

// TestInjectMediaTagRoundTrip injects every media tag that maps to a data
// type and reads it back with go-aaru.
func TestInjectMediaTagRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for i, m := range injectTagNames {
		if _, ok := injectTagDataTypes[m.tag]; !ok {
			continue
		}
		t.Run(m.name, func(t *testing.T) {
			path := filepath.Join(dir, m.name+".aif")
			tagFile := filepath.Join(dir, m.name+".bin")
			maintCopy(t, fixture(t, "tiny_cd"), path)
			data := tagTestData(i, 100+i*13)
			maintWriteFile(t, tagFile, data)

			out, errOut, rc := runInject(m.name, tagFile, path)
			if rc != 0 || errOut != "" {
				t.Fatalf("rc = %d\n%s%s", rc, out, errOut)
			}
			if got := injectedTag(t, path, m.tag); !bytes.Equal(got, data) {
				t.Errorf("media tag reads back %d bytes, want %d", len(got), len(data))
			}
			maintCheckReads(t, path, maintDump(path))
		})
	}
}

func TestInjectMediaTag(t *testing.T) {
	type injectCase struct {
		name    string
		tag     string
		src     string          // image fixture; empty: missing image
		input   []byte          // literal image
		patch   func(*maintRaw) // alter the image
		tagData []byte          // nil: missing tag file
		rc      int
		stderr  string // text stderr must contain
		readTag aaru.MediaTagType
		check   func(t *testing.T, path string)
	}
	atip := tagTestData(1, 3000)
	cases := []injectCase{
		{name: "cdmode1_atip", tag: "MediaTagCdAtip", src: "cdmode1", tagData: atip,
			readTag: aaru.MediaTagCdAtip},
		{name: "numeric", tag: "8", src: "mf2hd", tagData: atip[:6], readTag: aaru.MediaTagDvdCmi},
		{name: "case_insensitive", tag: "MEDIATAGSCSIINQUIRY", src: "floptical", tagData: atip[:96],
			readTag: aaru.MediaTagScsiInquiry},
		{name: "replace", tag: "MediaTagCdToc", src: "cd_lzma", tagData: atip[:804],
			readTag: aaru.MediaTagCdToc,
			check: func(t *testing.T, path string) {
				r := maintParseRaw(t, maintReadFile(t, path))
				n := 0
				for _, e := range r.index {
					if e.BlockType == aaru.BlockTypeDataBlock && e.DataType == aaru.DataTypeCdToc {
						n++
					}
				}
				if n != 1 {
					t.Errorf("%d CD TOC blocks in the index", n)
				}
			}},
		{name: "header_shift_0", tag: "MediaTagCdMcn", src: "cd_zstd", tagData: atip[:13],
			readTag: aaru.MediaTagCdMcn,
			patch:   func(r *maintRaw) { r.header.BlockAlignmentShift = 0; r.put(0, r.header) },
			check: func(t *testing.T, path string) {
				r := maintParseRaw(t, maintReadFile(t, path))
				if r.header.BlockAlignmentShift == 0 || r.header.IndexOffset%(1<<r.header.BlockAlignmentShift) != 0 {
					t.Errorf("header alignment %d, index at %d", r.header.BlockAlignmentShift, r.header.IndexOffset)
				}
			}},
		{name: "invalid_tag", tag: "bogus", src: "floptical", tagData: atip, rc: -1,
			stderr: "ERROR: Invalid media tag type: 'bogus'"},
		{name: "out_of_range", tag: "87", src: "floptical", tagData: atip, rc: -1,
			stderr: "ERROR: Invalid media tag type: '87'"},
		{name: "no_data_type", tag: "76", src: "floptical", tagData: atip, rc: -1,
			stderr: "ERROR: Cannot convert media tag type '76' to data type."},
		{name: "minidisc", tag: "MediaTagMiniDiscUtoc", src: "floptical", tagData: atip, rc: -1,
			stderr: "ERROR: Cannot convert media tag type 'MediaTagMiniDiscUtoc' to data type."},
		{name: "missing_tag_file", tag: "MediaTagCdAtip", src: "floptical", rc: -1,
			stderr: "ERROR: Cannot open media tag file: 'TAG'\n       No such file or directory\n"},
		{name: "empty_tag_file", tag: "MediaTagCdAtip", src: "floptical", tagData: []byte{}, rc: -1,
			stderr: "ERROR: Media tag file is empty or cannot determine size: 'TAG'"},
		{name: "missing_image", tag: "MediaTagCdAtip", tagData: atip, rc: -1,
			stderr: "ERROR: Cannot open image file: 'IMAGE'\n       No such file or directory\n"},
		{name: "short_image", tag: "MediaTagCdAtip", input: []byte("AARUFRMT"), tagData: atip, rc: -1,
			stderr: "ERROR: Cannot read image header from: 'IMAGE'\n       File may be too small or corrupted.\n"},
		{name: "not_aaruformat", tag: "MediaTagCdAtip", input: bytes.Repeat([]byte("not an image "), 20),
			tagData: atip, rc: -1, stderr: "ERROR: 'IMAGE' is not a valid AaruFormat image."},
		{name: "v1", tag: "MediaTagCdAtip", src: "cdmode1_v1", tagData: atip, rc: -1,
			stderr: "ERROR: Image version 1.0 is not supported."},
		{name: "short_v2", tag: "MediaTagCdAtip", src: "short_v2_header", tagData: atip, rc: -1,
			stderr: "ERROR: Cannot read v2 header from: 'IMAGE'"},
		{name: "bad_index", tag: "MediaTagCdAtip", src: "floptical", tagData: atip, rc: -1,
			patch:  func(r *maintRaw) { r.put(r.header.IndexOffset, uint32(0x12345678)) },
			stderr: "ERROR: Expected IndexBlock3 identifier, found 0x12345678"},
		{name: "index_past_eof", tag: "MediaTagCdAtip", src: "floptical", tagData: atip, rc: -1,
			patch:  func(r *maintRaw) { r.header.IndexOffset = 1 << 40; r.put(0, r.header) },
			stderr: "ERROR: Cannot read index header from: 'IMAGE'"},
		{name: "truncated_index", tag: "MediaTagCdAtip", src: "floptical", tagData: atip, rc: -1,
			patch:  func(r *maintRaw) { r.data = r.data[:r.header.IndexOffset+maintIndexHeader3Size+20] },
			stderr: "ERROR: Cannot read index entries (read 1, expected "},
		{name: "no_ddt", tag: "MediaTagCdAtip", src: "no_ddt", tagData: atip, rc: -1,
			stderr: "ERROR: No UserData DDT2 block found in index."},
		{name: "bad_ddt", tag: "MediaTagCdAtip", src: "floptical", tagData: atip, rc: -1,
			patch: func(r *maintRaw) {
				r.put(r.find(aaru.BlockTypeDeDuplicationTable2, aaru.DataTypeUserData), uint32(0x41414141))
			},
			stderr: "found 0x41414141"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "image.aif")
			tagFile := filepath.Join(dir, "tag.bin")
			input := c.input
			if c.src != "" {
				input = maintReadFile(t, fixture(t, c.src))
				if c.patch != nil {
					r := maintParseRaw(t, input)
					c.patch(r)
					input = r.data
				}
			}
			if input != nil {
				maintWriteFile(t, path, input)
			}
			if c.tagData != nil {
				maintWriteFile(t, tagFile, c.tagData)
			}

			start := time.Now()
			out, errOut, rc := runInject(c.tag, tagFile, path)
			norm := func(s, image string) string { return maintNormalize(s, image, "IMAGE", tagFile, "TAG") }
			got := "== stdout\n" + norm(out, path) + "== stderr\n" + norm(errOut, path)
			if rc != c.rc {
				t.Errorf("rc = %d, want %d\n%s", rc, c.rc, got)
			}
			if !strings.Contains(norm(errOut, path), portable(c.stderr)) {
				t.Errorf("stderr lacks %q:\n%s", c.stderr, got)
			}

			if rc == 0 {
				if data := injectedTag(t, path, c.readTag); !bytes.Equal(data, c.tagData) {
					t.Errorf("media tag reads back %d bytes, want %d", len(data), len(c.tagData))
				}
				// Everything else reads as before.
				before := filepath.Join(dir, "before.aif")
				maintWriteFile(t, before, input)
				if want, got := dropTagLines(maintDump(before), c.readTag), dropTagLines(maintDump(path), c.readTag); want != got {
					t.Errorf("the image reads differently: %s", maintFirstDiff(got, want))
				}
				maintCheckReads(t, path, maintDump(path))
				r := maintParseRaw(t, maintReadFile(t, path))
				written := aaru.FiletimeToTime(r.header.LastWrittenTime)
				if written.Before(start.Add(-time.Second)) || written.After(time.Now().Add(time.Second)) {
					t.Errorf("last written time %v", written)
				}
				if c.check != nil {
					c.check(t, path)
				}
			} else if input != nil && !bytes.Equal(maintReadFile(t, path), input) {
				t.Error("the image was modified")
			}

			checkExpected(t, "maint/inject_"+c.name+".txt", got)
		})
	}
}

// dropTagLines removes the line of media tag tag from a maintDump output.
func dropTagLines(dump string, tag aaru.MediaTagType) string {
	prefix := fmt.Sprintf("T %d ", tag)
	var b strings.Builder
	for _, l := range strings.SplitAfter(dump, "\n") {
		if !strings.HasPrefix(l, prefix) {
			b.WriteString(l)
		}
	}
	return b.String()
}

func TestInjectMediaTagArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.aif")
	tagFile := filepath.Join(dir, "tag.bin")
	maintCopy(t, fixture(t, "floptical"), path)
	data := tagTestData(5, 512)
	maintWriteFile(t, tagFile, data)

	out := captureMaintStdout(t, func() {
		if rc := cmdInjectMediaTag([]string{"MediaTagCdToc", tagFile}); rc != -1 {
			t.Errorf("two arguments: rc = %d", rc)
		}
		if rc := cmdInjectMediaTag([]string{"MediaTagCdToc", tagFile, path, path}); rc != -1 {
			t.Errorf("four arguments: rc = %d", rc)
		}
	})
	if n := strings.Count(out, "aaruformattool inject-media-tag <tag-type> <media-tag-file> <image-file>"); n != 2 {
		t.Errorf("usage printed %d times:\n%s", n, out)
	}

	saved := os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	os.Stderr = devNull
	defer func() { os.Stderr = saved }()
	out = captureMaintStdout(t, func() {
		if rc := cmdInjectMediaTag([]string{"MediaTagAtaIdentify", tagFile, path}); rc != 0 {
			t.Errorf("rc = %d", rc)
		}
	})
	if !strings.Contains(out, "Successfully injected media tag 'ATA IDENTIFY DEVICE' into image.") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if got := injectedTag(t, path, aaru.MediaTagAtaIdentify); !bytes.Equal(got, data) {
		t.Error("media tag does not read back")
	}
}
