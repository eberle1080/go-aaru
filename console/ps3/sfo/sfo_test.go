package sfo

import (
	"encoding/binary"
	"errors"
	"testing"
)

// The tests parse PARAM.SFO images built in memory by buildSFO.

// sampleEntries is a PS3-style PARAM.SFO parameter set.
var sampleEntries = []testEntry{
	{key: "TITLE", format: FormatUTF8, str: "Sample Disc Title"},
	{key: "TITLE_ID", format: FormatUTF8, str: "BLES99999"},
	{key: "APP_VER", format: FormatUTF8, str: "01.00"},
	{key: "VERSION", format: FormatUTF8, str: "02.50"},
	{key: "CATEGORY", format: FormatUTF8, str: "DG"},
	{key: "PS3_SYSTEM_VER", format: FormatUTF8Special, str: "04.21", raw: []byte("04.21\x00"), maxLen: 8},
	{key: "RESOLUTION", format: FormatInt32, ival: 63},
	{key: "SOUND_FORMAT", format: FormatInt32, ival: -2},
}

func parseSample(t *testing.T) *File {
	t.Helper()

	f, err := Parse(buildSFO(sampleEntries))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	return f
}

func TestParseSucceeds(t *testing.T) {
	if f := parseSample(t); len(f.Entries) != len(sampleEntries) {
		t.Fatalf("entry count = %d, want %d", len(f.Entries), len(sampleEntries))
	}
}

func TestStrings(t *testing.T) {
	f := parseSample(t)

	for _, e := range sampleEntries {
		if e.format == FormatInt32 {
			continue
		}

		got, ok := f.Get(e.key)
		if !ok || got != e.str {
			t.Errorf("Get(%q) = %q, %v; want %q", e.key, got, ok, e.str)
		}
	}
}

func TestNonExistentKey(t *testing.T) {
	if v, ok := parseSample(t).Get("NONEXISTENT"); ok {
		t.Fatalf("Get(NONEXISTENT) = %q, true", v)
	}
}

// Entry order, formats and integer values must match what was built.
func TestEntriesMatchBuilt(t *testing.T) {
	f := parseSample(t)

	for i, w := range sampleEntries {
		e := f.Entries[i]
		if e.Key != w.key || e.Format != w.format || e.Value != w.str || e.IntValue != w.ival || !e.HasValue {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
	}

	if v, ok := f.GetInt("RESOLUTION"); !ok || v != 63 {
		t.Errorf("GetInt(RESOLUTION) = %d, %v", v, ok)
	}

	if v, ok := f.GetInt("SOUND_FORMAT"); !ok || v != -2 {
		t.Errorf("GetInt(SOUND_FORMAT) = %d, %v", v, ok)
	}

	// Integer entries have no string value.
	if _, ok := f.Get("RESOLUTION"); ok {
		t.Error("Get(RESOLUTION) returned a string")
	}

	if _, ok := f.GetInt("TITLE"); ok {
		t.Error("GetInt(TITLE) returned an integer")
	}
}

// Duplicate keys resolve to the first entry; a NUL inside a string value ends
// it; an int entry shorter than 4 bytes has no value.
func TestBuiltEdgeCases(t *testing.T) {
	f, err := Parse(buildSFO([]testEntry{
		{key: "DUP", format: FormatUTF8, str: "first"},
		{key: "DUP", format: FormatUTF8, str: "second"},
		{key: "NUL", format: FormatUTF8, str: "ab\x00cd"},
		{key: "EMPTY", format: FormatUTF8, str: ""},
		{key: "SHORTINT", format: FormatInt32, raw: []byte{1, 2}},
		{key: "OTHER", format: 0x1234, str: "opaque"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	if v, ok := f.Get("DUP"); !ok || v != "first" {
		t.Errorf("Get(DUP) = %q, %v", v, ok)
	}

	if v, ok := f.Get("NUL"); !ok || v != "ab" {
		t.Errorf("Get(NUL) = %q, %v", v, ok)
	}

	if _, ok := f.Get("EMPTY"); ok {
		t.Error("zero-length value decoded")
	}

	if _, ok := f.GetInt("SHORTINT"); ok {
		t.Error("2-byte integer decoded")
	}

	if e, ok := f.Lookup("OTHER"); !ok || e.HasValue || e.Format != 0x1234 {
		t.Errorf("Lookup(OTHER) = %+v, %v", e, ok)
	}

	if _, ok := f.Get("OTHER"); ok {
		t.Error("unknown format decoded as string")
	}
}

// Every truncation of a built file parses or fails cleanly.
func TestTruncations(t *testing.T) {
	data := buildSFO(sampleEntries)

	for i := range data {
		if f, err := Parse(data[:i]); err == nil {
			for _, e := range f.Entries {
				f.Get(e.Key)
			}
		}
	}
}

func TestNilArgs(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Error("Parse(nil) succeeded")
	}
}

func TestTooSmall(t *testing.T) {
	if _, err := Parse(make([]byte, 10)); !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

func TestWrongMagic(t *testing.T) {
	buf := make([]byte, 20)
	buf[0] = 0xFF

	if _, err := Parse(buf); !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("err = %v, want ErrInvalidMagic", err)
	}
}

func TestGetNilFile(t *testing.T) {
	var f *File
	if _, ok := f.Get("TITLE"); ok {
		t.Error("nil File returned a value")
	}

	if _, ok := (&File{}).Get(""); ok {
		t.Error("empty key returned a value")
	}
}

func header(keyTable, dataTable, count uint32) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b, Magic)
	binary.LittleEndian.PutUint32(b[4:], 0x101)
	binary.LittleEndian.PutUint32(b[8:], keyTable)
	binary.LittleEndian.PutUint32(b[12:], dataTable)
	binary.LittleEndian.PutUint32(b[16:], count)

	return b
}

func TestZeroEntries(t *testing.T) {
	f, err := Parse(header(0, 0, 0))
	if err != nil || len(f.Entries) != 0 {
		t.Fatalf("got %+v, %v", f, err)
	}
}

func TestMalformed(t *testing.T) {
	// Index table larger than the buffer.
	if _, err := Parse(header(20, 20, 2)); !errors.Is(err, ErrTruncated) {
		t.Errorf("index overflow: %v", err)
	}

	// Huge entry count must not overflow.
	if _, err := Parse(header(20, 20, 0xFFFFFFFF)); !errors.Is(err, ErrTruncated) {
		t.Errorf("huge count: %v", err)
	}

	// Table offsets beyond the buffer.
	b := append(header(1000, 36, 1), make([]byte, 16)...)
	if _, err := Parse(b); !errors.Is(err, ErrTruncated) {
		t.Errorf("key table overflow: %v", err)
	}

	// One entry whose key is unterminated and whose value is clamped.
	b = header(36, 40, 1)
	idx := make([]byte, 16)
	binary.LittleEndian.PutUint16(idx[2:], FormatUTF8)
	binary.LittleEndian.PutUint32(idx[4:], 100) // longer than available
	b = append(b, idx...)
	b = append(b, "KEY!"...)
	b = append(b, "val"...)

	f, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}

	if e := f.Entries[0]; e.Key != "KEY!val" || e.Value != "val" {
		t.Errorf("entry = %+v", e)
	}

	// Value offset past the end leaves no value.
	binary.LittleEndian.PutUint32(b[20+12:], 1000)

	f, err = Parse(b)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := f.Get("KEY!val"); ok {
		t.Error("out-of-range value decoded")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(buildSFO(sampleEntries))

	f.Add(header(20, 20, 1))

	f.Fuzz(func(t *testing.T, data []byte) {
		if file, err := Parse(data); err == nil {
			for _, e := range file.Entries {
				file.Get(e.Key)
				file.GetInt(e.Key)
			}
		}
	})
}
