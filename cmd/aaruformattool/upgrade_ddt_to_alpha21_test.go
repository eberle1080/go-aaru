package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

func TestMaintStructSizes(t *testing.T) {
	for _, c := range []struct {
		v    any
		size int
	}{
		{aaru.Header{}, maintHeaderSize},
		{aaru.IndexHeader3{}, maintIndexHeader3Size},
		{aaru.IndexEntry{}, maintIndexEntrySize},
		{aaru.DDTHeader2{}, maintDDTHeader2Size},
		{ddtHeader2Alpha20{}, maintDDTAlpha20Size},
		{aaru.BlockHeader{}, maintBlockHeaderSize},
	} {
		if got := binary.Size(c.v); got != c.size {
			t.Errorf("%T: %d bytes, want %d", c.v, got, c.size)
		}
	}
}

// makeAlpha20 returns the contents of the image src with every DDT2 block of
// its index rewritten in the alpha 20 layout: the 81-byte
// header with 16-bit negative/overflow counts immediately followed by the
// unchanged payload. mutate, if not nil, may alter each header.
func makeAlpha20(t *testing.T, src string, mutate func(aaru.DataType, *ddtHeader2Alpha20)) *maintRaw {
	t.Helper()
	r := maintParseRaw(t, maintReadFile(t, src))
	for _, e := range r.index {
		if e.BlockType == aaru.BlockTypeDeDuplicationTableSecondary {
			t.Fatalf("%s: multi-level DDTs are not handled by upgrade-ddt-to-alpha21", src)
		}
		if e.BlockType != aaru.BlockTypeDeDuplicationTable2 {
			continue
		}
		var h aaru.DDTHeader2
		if _, err := binary.Decode(r.data[e.Offset:], binary.LittleEndian, &h); err != nil {
			t.Fatal(err)
		}
		if h.TableShift != 0 || h.Negative > math.MaxUint16 || h.Overflow > math.MaxUint16 {
			t.Fatalf("%s: DDT2 at %d cannot be expressed in the alpha 20 layout: %+v", src, e.Offset, h)
		}
		old := ddtHeader2Alpha20{
			Identifier: h.Identifier, Type: h.Type, Compression: h.Compression, Levels: h.Levels,
			TableLevel: h.TableLevel, PreviousLevelOffset: h.PreviousLevelOffset, Negative: uint16(h.Negative),
			Blocks: h.Blocks, Overflow: uint16(h.Overflow), Start: h.Start, BlockAlignmentShift: h.BlockAlignmentShift,
			DataShift: h.DataShift, TableShift: h.TableShift, Entries: h.Entries, CmpLength: h.CmpLength,
			Length: h.Length, CmpCRC64: h.CmpCRC64, CRC64: h.CRC64,
		}
		if mutate != nil {
			mutate(e.DataType, &old)
		}
		payload := bytes.Clone(r.data[e.Offset+maintDDTHeader2Size : e.Offset+maintDDTHeader2Size+h.CmpLength])
		r.put(e.Offset, old)
		copy(r.data[e.Offset+maintDDTAlpha20Size:], payload)
		clear(r.data[e.Offset+maintDDTAlpha20Size+h.CmpLength : e.Offset+maintDDTHeader2Size+h.CmpLength])
	}
	return r
}

// dvdAlpha20Bug sets the block count of the alpha 20 DVD images that the
// upgrade offers to fix: overflow 15000 and 0x30000 + 15000 extra blocks.
func dvdAlpha20Bug(_ aaru.DataType, h *ddtHeader2Alpha20) {
	if h.Blocks != 0 {
		h.Overflow = 15000
		h.Blocks += 0x30000 + 15000
	}
}

func runUpgrade(path, answers string) (string, int) {
	var out bytes.Buffer
	rc := upgradeDDTToAlpha21(&out, bufio.NewReader(strings.NewReader(answers)), path)
	return out.String(), rc
}

type upgradeCase struct {
	name    string
	src     string                                  // fixture the input is derived from
	alpha20 bool                                    // convert the fixture to the alpha 20 layout
	mutate  func(aaru.DataType, *ddtHeader2Alpha20) // alter the alpha 20 DDT headers
	patch   func(*maintRaw)                         // alter the input image
	input   []byte                                  // literal input (src unset); nil and no src: missing file
	answers string                                  // standard input
	rc      int                                     // expected return value
	fixed   bool                                    // the result must read exactly like src
	output  string                                  // text the output must contain
}

func TestUpgradeDDTToAlpha21(t *testing.T) {
	cases := []upgradeCase{
		{name: "cdmode1", src: "cdmode1", alpha20: true, answers: "yes\n", fixed: true},
		{name: "cdmode2", src: "cdmode2", alpha20: true, answers: "y\n", fixed: true},
		{name: "floptical", src: "floptical", alpha20: true, answers: "YES\n", fixed: true},
		{name: "mf2hd", src: "mf2hd", alpha20: true, answers: "Y\n", fixed: true},
		{name: "cd_lzma", src: "cd_lzma", alpha20: true, answers: "yes\n", fixed: true},
		{name: "cd_zstd", src: "cd_zstd", alpha20: true, answers: "yes\n", fixed: true},
		{name: "blk_zstd", src: "blk_zstd", alpha20: true, answers: "yes\n", fixed: true},
		{name: "tiny_cd", src: "tiny_cd", alpha20: true, answers: "yes\n", fixed: true},
		{name: "dic_magic", src: "dic_magic", alpha20: true, answers: "yes\n", fixed: true},
		{name: "dvd_fix", src: "dvd_long", alpha20: true, mutate: dvdAlpha20Bug, answers: "yes\nyes\n",
			fixed: true, output: "*** INVALID BLOCK COUNT DETECTED ***"},
		{name: "dvd_fix_declined", src: "dvd_long", alpha20: true, mutate: dvdAlpha20Bug,
			answers: "yes\nno\n", output: "User declined to fix block count. Upgrade cancelled."},
		{name: "dvd_fix_eof", src: "dvd_long", alpha20: true, mutate: dvdAlpha20Bug, answers: "yes\n",
			rc: maintEIO, output: "ERROR: Failed to read user input"},
		{name: "cancelled", src: "floptical", alpha20: true, answers: "no\n", output: "Upgrade cancelled by user."},
		{name: "crlf_is_no", src: "floptical", alpha20: true, answers: "yes\r\n",
			output: "Upgrade cancelled by user."},
		{name: "eof", src: "floptical", alpha20: true, answers: "", rc: maintEIO,
			output: "ERROR: Failed to read user input"},
		{name: "dvdram", src: "dvd_long", alpha20: true, answers: "yes\n", rc: maintEINVAL,
			patch:  func(r *maintRaw) { r.header.MediaType = uint32(media.DVDRAM); r.put(0, r.header) },
			output: "ERROR: Cannot upgrade DVD-RAM media type"},
		{name: "overflow_without_negative", src: "floptical", alpha20: true, answers: "yes\n", rc: maintEINVAL,
			mutate: func(dt aaru.DataType, h *ddtHeader2Alpha20) { h.Overflow = 5 },
			output: "ERROR: Invalid sector configuration (from UserData DDT)"},
		{name: "no_userdata_ddt", src: "cdmode1", alpha20: true, answers: "yes\n", rc: maintEINVAL,
			patch: func(r *maintRaw) {
				for i, e := range r.index {
					if e.BlockType == aaru.BlockTypeDeDuplicationTable2 && e.DataType == aaru.DataTypeUserData {
						r.index[i].DataType = aaru.DataTypeNone
					}
				}
				r.writeIndex()
			},
			output: "ERROR: No UserData DDT v2 block found in this image."},
		{name: "no_ddt", src: "no_ddt", answers: "yes\n",
			output: "WARNING: No DDT v2 blocks found in index"},
		{name: "bad_index", src: "floptical", alpha20: true, answers: "yes\n", rc: maintEINVAL,
			patch:  func(r *maintRaw) { r.put(r.header.IndexOffset, uint32(0x12345678)) },
			output: "ERROR: Expected IndexBlock3 identifier, found 0x12345678"},
		{name: "index_past_eof", src: "floptical", alpha20: true, answers: "yes\n", rc: maintEIO,
			patch:  func(r *maintRaw) { r.header.IndexOffset = 1 << 40; r.put(0, r.header) },
			output: "ERROR: Failed to read index header (read 0 bytes, expected 28)"},
		{name: "truncated_index", src: "floptical", alpha20: true, answers: "yes\n", rc: maintEIO,
			patch:  func(r *maintRaw) { r.data = r.data[:r.header.IndexOffset+maintIndexHeader3Size+20] },
			output: "ERROR: Failed to read index entries (read 1, expected"},
		{name: "v1", src: "cdmode1_v1", answers: "yes\n", rc: maintEINVAL,
			output: "ERROR: This command only works with AaruFormat v2 images"},
		{name: "not_aaruformat", input: bytes.Repeat([]byte("not an image "), 20), answers: "yes\n",
			rc: maintEINVAL, output: "ERROR: File is not an AaruFormat image (invalid magic identifier)"},
		{name: "short", input: []byte("AARUFRMT"), answers: "yes\n", rc: maintEIO,
			output: "ERROR: Failed to read header from file (read 8 bytes, expected 104)"},
		{name: "short_v2", src: "short_v2_header", answers: "yes\n", rc: maintEIO,
			output: "ERROR: Failed to read complete v2 header from file"},
		{name: "missing", answers: "yes\n", rc: 2, output: "No such file or directory"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "image.aif")

			var input []byte
			switch {
			case c.src != "" && c.alpha20:
				r := makeAlpha20(t, fixture(t, c.src), c.mutate)
				if c.patch != nil {
					c.patch(r)
				}
				input = r.data
			case c.src != "":
				input = maintReadFile(t, fixture(t, c.src))
				if c.patch != nil {
					r := maintParseRaw(t, input)
					c.patch(r)
					input = r.data
				}
			default:
				input = c.input
			}
			if input != nil {
				maintWriteFile(t, path, input)
			}

			if c.alpha20 && c.fixed {
				if maintDump(path) == maintDump(fixture(t, c.src)) {
					t.Fatal("the alpha 20 image already reads like the original")
				}
			}

			out, rc := runUpgrade(path, c.answers)
			if rc != c.rc {
				t.Errorf("rc = %d, want %d\n%s", rc, c.rc, out)
			}
			if !strings.Contains(out, portable(c.output)) {
				t.Errorf("output lacks %q:\n%s", c.output, out)
			}

			switch {
			case c.fixed:
				maintCheckReads(t, path, maintDump(fixture(t, c.src)))
			case input != nil:
				if !bytes.Equal(maintReadFile(t, path), input) {
					t.Error("the image was modified")
				}
			}

			got := maintNormalize(out, path, "IMAGE")
			checkExpected(t, "maint/upgrade_"+c.name+".txt", got)
		})
	}
}

func TestUpgradeDDTToAlpha21Args(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.aif")
	maintWriteFile(t, path, makeAlpha20(t, fixture(t, "floptical"), nil).data)

	out := captureMaintStdout(t, func() {
		if rc := cmdUpgradeDDTToAlpha21(nil); rc != -1 {
			t.Errorf("no arguments: rc = %d", rc)
		}
		if rc := cmdUpgradeDDTToAlpha21([]string{path, path}); rc != -1 {
			t.Errorf("two arguments: rc = %d", rc)
		}
		if rc := cmdUpgradeDDTToAlpha21([]string{"--help"}); rc != -1 {
			t.Errorf("--help: rc = %d", rc)
		}
	})
	if n := strings.Count(out, "aaruformattool upgrade-ddt-to-alpha21 <filename>"); n != 3 {
		t.Errorf("usage printed %d times:\n%s", n, out)
	}

	stdin := filepath.Join(dir, "stdin")
	maintWriteFile(t, stdin, []byte("yes\n"))
	f, err := os.Open(stdin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	saved := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = saved }()
	out = captureMaintStdout(t, func() {
		if rc := cmdUpgradeDDTToAlpha21([]string{path}); rc != 0 {
			t.Errorf("rc = %d", rc)
		}
	})
	if !strings.Contains(out, "UPGRADE COMPLETED SUCCESSFULLY") {
		t.Errorf("unexpected output:\n%s", out)
	}
	maintCheckReads(t, path, maintDump(fixture(t, "floptical")))
}

// captureMaintStdout returns what fn prints to os.Stdout.
func captureMaintStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		var b bytes.Buffer
		_, _ = b.ReadFrom(r)
		done <- b.Bytes()
	}()
	defer func() { os.Stdout = saved }()
	fn()
	_ = w.Close()
	os.Stdout = saved
	return string(<-done)
}
