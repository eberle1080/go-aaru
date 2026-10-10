package ird

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The tests parse IRD files built in memory by buildIRD.

// irdSpec describes an IRD file for buildIRD.
type irdSpec struct {
	version                    byte
	gameID, name               string
	updateVer, gameVer, appVer string
	header, footer             []byte
	regions, files             int
	data1, data2               [16]byte
	pic                        [PICSize]byte
}

func seq(n int, start byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}

	return b
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()

	return buf.Bytes()
}

// sampleSpec returns a version-9 spec with distinct keys, PIC and gzip
// header/footer blobs.
func sampleSpec() irdSpec {
	s := irdSpec{
		version:   9,
		gameID:    "BLES99999",
		name:      "Sample Game: Edition",
		updateVer: "4.21",
		gameVer:   "01.00",
		appVer:    "01.02",
		header:    gzipBytes(seq(40000, 3)),
		footer:    gzipBytes(seq(300, 9)),
		regions:   3,
		files:     5,
	}

	copy(s.data1[:], seq(16, 0x10))
	copy(s.data2[:], seq(16, 0xA0))
	copy(s.pic[:], seq(PICSize, 0x40))

	return s
}

// buildIRD encodes s in the IRD layout: "3IRD", version, game ID (9),
// 7-bit-length-prefixed name, update (4), game (5) and app (5) versions,
// a 4-byte ID for v7, length-prefixed header and footer blobs, region hashes
// (count byte + 16 each), file entries (u32 count + 24 each), 4 bytes of
// extra config/attachments, then data1, data2 and PIC (PIC first for v9).
func buildIRD(s irdSpec) []byte {
	var b bytes.Buffer

	b.WriteString("3IRD")
	b.WriteByte(s.version)
	b.Write(append([]byte(s.gameID), make([]byte, 9)...)[:9])

	n := len(s.name)
	for n >= 0x80 {
		b.WriteByte(byte(n) | 0x80)
		n >>= 7
	}

	b.WriteByte(byte(n))
	b.WriteString(s.name)
	b.WriteString(s.updateVer)
	b.WriteString(s.gameVer)
	b.WriteString(s.appVer)

	if s.version == 7 {
		b.Write([]byte{1, 2, 3, 4})
	}

	for _, blob := range [][]byte{s.header, s.footer} {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(blob)))
		b.Write(blob)
	}

	b.WriteByte(byte(s.regions))
	b.Write(bytes.Repeat([]byte{0xEE}, 16*s.regions))
	_ = binary.Write(&b, binary.LittleEndian, uint32(s.files))
	b.Write(bytes.Repeat([]byte{0xDD}, 24*s.files))
	b.Write(make([]byte, 4)) // extra config + attachments

	if s.version >= 9 {
		b.Write(s.pic[:])
	}

	b.Write(s.data1[:])
	b.Write(s.data2[:])

	if s.version < 9 {
		b.Write(s.pic[:])
	}

	return b.Bytes()
}

func checkParsed(t *testing.T, f *File, s irdSpec) {
	t.Helper()

	if f.Version != s.version || f.GameID != s.gameID || f.GameName != s.name || f.UpdateVersion != s.updateVer ||
		f.GameVersion != s.gameVer || f.AppVersion != s.appVer {
		t.Errorf("v%d: strings = %q %q %q %q %q (version %d)", s.version, f.GameID, f.GameName,
			f.UpdateVersion, f.GameVersion, f.AppVersion, f.Version)
	}

	if !bytes.Equal(f.HeaderGz, s.header) || !bytes.Equal(f.FooterGz, s.footer) {
		t.Errorf("v%d: header/footer blobs differ from those built", s.version)
	}

	if f.Data1 != s.data1 || f.Data2 != s.data2 {
		t.Errorf("v%d: data1 = %x, data2 = %x; want %x, %x", s.version, f.Data1, f.Data2, s.data1, s.data2)
	}

	if !f.HasPIC || f.PIC != s.pic {
		t.Errorf("v%d: PIC = %x (has %v), want %x", s.version, f.PIC, f.HasPIC, s.pic)
	}
}

func TestParseBuilt(t *testing.T) {
	s := sampleSpec()
	checkParsed(t, mustParse(t, buildIRD(s)), s)
}

func mustParse(t *testing.T, data []byte) *File {
	t.Helper()

	f, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	return f
}

// The header and footer blobs are passed through untouched and still
// decompress to what was written.
func TestHeaderFooterBlobsDecompress(t *testing.T) {
	f := mustParse(t, buildIRD(sampleSpec()))

	for _, c := range []struct {
		blob []byte
		want []byte
	}{{f.HeaderGz, seq(40000, 3)}, {f.FooterGz, seq(300, 9)}} {
		zr, err := gzip.NewReader(bytes.NewReader(c.blob))
		if err != nil {
			t.Fatal(err)
		}

		got, err := io.ReadAll(zr)
		if err != nil || !bytes.Equal(got, c.want) {
			t.Fatalf("blob decompressed to %d bytes, %v; want %d", len(got), err, len(c.want))
		}
	}
}

// A gzip-wrapped IRD parses identically to the raw form, via ParseBytes,
// Parse and ParseFile.
func TestGzipAndReader(t *testing.T) {
	s := sampleSpec()
	raw := buildIRD(s)
	gz := gzipBytes(raw)

	path := filepath.Join(t.TempDir(), "sample.ird")
	if err := os.WriteFile(path, gz, 0o600); err != nil {
		t.Fatal(err)
	}

	fromGz := mustParse(t, gz)

	fromReader, err := Parse(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}

	fromFile, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range []*File{fromGz, fromReader, fromFile} {
		checkParsed(t, f, s)
	}

	// Every truncation of the raw file must either parse or fail cleanly.
	for i := 0; i < len(raw); i += 97 {
		_, _ = ParseBytes(raw[:i])
	}

	// A truncated gzip stream fails.
	if _, err := ParseBytes(gz[:len(gz)/2]); err == nil {
		t.Error("truncated gzip accepted")
	}
}

// Files cut off after the version strings or blobs return the fields read so
// far, with keys and PIC left zero.
func TestPartialFile(t *testing.T) {
	s := sampleSpec()
	raw := buildIRD(s)

	// Cut right after the PIC (v9 order): keys absent.
	cut := len(raw) - 32
	f := mustParse(t, raw[:cut])

	if !f.HasPIC || f.PIC != s.pic || f.Data1 != ([16]byte{}) {
		t.Errorf("cut before keys: HasPIC=%v data1=%x", f.HasPIC, f.Data1)
	}

	// Cut inside the PIC: no PIC.
	f = mustParse(t, raw[:cut-1])
	if f.HasPIC {
		t.Errorf("cut inside PIC: HasPIC=%v", f.HasPIC)
	}

	// Header blob longer than the file: nil header, nothing after it.
	hdrStart := 5 + 9 + 1 + len(s.name) + 14
	f = mustParse(t, raw[:hdrStart+4+10])

	if f.HeaderGz != nil || f.FooterGz != nil || f.HasPIC || f.AppVersion != s.appVer {
		t.Errorf("cut inside header blob: %+v", f)
	}
}

func TestSyntheticVersions(t *testing.T) {
	long := string(bytes.Repeat([]byte{'N'}, 300)) // needs a 2-byte 7-bit length

	for _, v := range []byte{6, 7, 8, 9} {
		for _, name := range []string{"Short", long, ""} {
			s := sampleSpec()
			s.version, s.name = v, name
			s.header, s.footer = []byte{0xA, 0xB, 0xC}, []byte{0xD, 0xE}
			s.regions, s.files = 1, 1

			checkParsed(t, mustParse(t, buildIRD(s)), s)
		}
	}
}

func TestErrors(t *testing.T) {
	if _, err := ParseFile(""); err == nil {
		t.Error("empty path accepted")
	}

	if _, err := ParseFile(filepath.Join(t.TempDir(), "nonexistent_ird_file_12345.ird")); err == nil {
		t.Error("nonexistent file accepted") // NonExistentFile
	}

	if _, err := ParseBytes(nil); !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("nil: %v", err)
	}

	bad := buildIRD(sampleSpec())
	bad[0] = 'X'

	if _, err := ParseBytes(bad); !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("bad magic: %v", err)
	}

	for _, v := range []byte{5, 10} {
		s := sampleSpec()
		s.version = v

		if _, err := ParseBytes(buildIRD(s)); !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("version %d: %v", v, err)
		}
	}

	if _, err := ParseBytes([]byte{0x1F, 0x8B, 0, 0}); err == nil {
		t.Error("corrupt gzip accepted")
	}

	// Name longer than the file.
	short := sampleSpec()
	short.name = "abc"
	trunc := buildIRD(short)[:20]
	trunc[14] = 100

	if _, err := ParseBytes(trunc); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated name: %v", err)
	}
}

func FuzzParseBytes(f *testing.F) {
	v7 := sampleSpec()
	v7.version = 7

	f.Add(buildIRD(sampleSpec()))
	f.Add(buildIRD(v7))
	f.Add([]byte("3IRD\x09BLES99999\x05"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseBytes(data)
	})
}
