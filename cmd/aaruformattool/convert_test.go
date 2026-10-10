package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
)

// coreConvertNormalize removes what changes between runs from convert's
// output: the paths, the progress bar and the timings.
func coreConvertNormalize(out, in, dst string) string {
	out = strings.ReplaceAll(out, in, "IN")
	out = strings.ReplaceAll(out, dst, "OUT")
	out = regexp.MustCompile("\r\x1b\\[36m  \\[[^\n]*").ReplaceAllString(out, "")
	return regexp.MustCompile("((?:Time elapsed|Average speed):\\s*\x1b\\[0m) [^\n]*").ReplaceAllString(out, "$1 <t>")
}

// coreConvert converts the fixture src with the given options and returns the
// output path and the tool's output.
func coreConvert(t *testing.T, src string, opts ...string) (dst, out string) {
	t.Helper()
	in := fixture(t, src)
	dst = filepath.Join(t.TempDir(), "out.aif")
	args := append(append([]string{"convert"}, opts...), in, dst)
	code, out, errOut := coreRun(t, args...)
	if code != 0 || errOut != "" {
		t.Fatalf("convert %v = %d, stderr %q\n%s", opts, code, errOut, coreConvertNormalize(out, in, dst))
	}
	if !strings.Contains(out, "✓ CONVERSION COMPLETED SUCCESSFULLY") {
		t.Fatalf("no success banner:\n%s", coreConvertNormalize(out, in, dst))
	}
	return dst, coreConvertNormalize(out, in, dst)
}

// coreCheckConverted checks that dst holds everything convert copies from
// src: sectors (plain and, if long, long), tracks, media tags, metadata,
// geometry, dump hardware, Aaru JSON and flux captures.
func coreCheckConverted(t *testing.T, src, dst string, long bool) {
	t.Helper()
	s := openCore(t, fixture(t, src))
	d := openCore(t, dst)
	if err := d.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	si, di := s.Info(), d.Info()
	if si.Sectors != di.Sectors || si.SectorSize != di.SectorSize || si.MediaType != di.MediaType {
		t.Errorf("info %+v, want %+v", di, si)
	}
	if di.Application != "aaruformattool" || di.ApplicationVersion != "1.0" {
		t.Errorf("application %q %q", di.Application, di.ApplicationVersion)
	}

	for sector := uint64(0); sector < si.Sectors; sector++ {
		sd, ss, serr := s.ReadSector(sector, false)
		dd, ds, derr := d.ReadSector(sector, false)
		if serr != nil {
			if !errors.Is(serr, aaru.ErrSectorNotDumped) || !errors.Is(derr, aaru.ErrSectorNotDumped) {
				t.Fatalf("sector %d: source error %v, output error %v", sector, serr, derr)
			}
			continue
		}
		if derr != nil || !bytes.Equal(sd, dd) || ss != ds {
			t.Fatalf("sector %d: %v status %v, want status %v", sector, derr, ds, ss)
		}
		if !long {
			continue
		}
		sl, _, serr := s.ReadSectorLong(sector, false)
		dl, _, derr := d.ReadSectorLong(sector, false)
		if (serr == nil) != (derr == nil) || !bytes.Equal(sl, dl) {
			t.Fatalf("long sector %d: %v/%v, %d/%d bytes", sector, serr, derr, len(sl), len(dl))
		}
	}

	st, serr := s.Tracks()
	dt, derr := d.Tracks()
	if (serr == nil) != (derr == nil) || len(st) != len(dt) {
		t.Errorf("tracks %v/%v", serr, derr)
	}
	for i := range min(len(st), len(dt)) {
		if st[i] != dt[i] {
			t.Errorf("track %d: %+v, want %+v", i, dt[i], st[i])
		}
	}

	stags, dtags := s.ReadableMediaTags(), d.ReadableMediaTags()
	if len(stags) != len(dtags) {
		t.Errorf("media tags %v, want %v", dtags, stags)
	}
	for _, tag := range stags {
		a, _ := s.ReadMediaTag(tag)
		b, err := d.ReadMediaTag(tag)
		if err != nil || !bytes.Equal(a, b) {
			t.Errorf("media tag %v: %v", tag, err)
		}
	}

	for f := aaru.MetadataCreator; f <= aaru.MetadataDriveFirmwareRevision; f++ {
		a, _ := s.MetadataRaw(f)
		b, _ := d.MetadataRaw(f)
		if !bytes.Equal(a, b) {
			t.Errorf("metadata %v: %q, want %q", f, b, a)
		}
	}
	if s1, l1, _ := s.MediaSequence(); s1 > 0 {
		if s2, l2, _ := d.MediaSequence(); s1 != s2 || l1 != l2 {
			t.Errorf("media sequence %d/%d, want %d/%d", s2, l2, s1, l1)
		}
	}
	if c, h, sp, err := s.Geometry(); err == nil {
		if c2, h2, sp2, err := d.Geometry(); err != nil || c != c2 || h != h2 || sp != sp2 {
			t.Errorf("geometry %d/%d/%d %v", c2, h2, sp2, err)
		}
	}
	if a, err := s.DumpHardwareRaw(); err == nil {
		if b, err := d.DumpHardwareRaw(); err != nil || !bytes.Equal(a, b) {
			t.Errorf("dump hardware differs: %v", err)
		}
	}
	if a, err := s.AaruJSONMetadata(); err == nil {
		if b, err := d.AaruJSONMetadata(); err != nil || !bytes.Equal(a, b) {
			t.Errorf("JSON metadata differs: %v", err)
		}
	}
	sc, _ := s.FluxCaptures()
	dc, _ := d.FluxCaptures()
	if len(sc) != len(dc) {
		t.Errorf("%d flux captures, want %d", len(dc), len(sc))
	}
	for _, c := range sc {
		i1, d1, _ := s.ReadFluxCapture(c.FluxCaptureKey)
		i2, d2, err := d.ReadFluxCapture(c.FluxCaptureKey)
		if err != nil || !bytes.Equal(i1, i2) || !bytes.Equal(d1, d2) {
			t.Errorf("flux capture %+v: %v", c.FluxCaptureKey, err)
		}
	}
}

// TestCoreConvertOutput compares convert's output with the expected outputs
// and checks the converted images.
func TestCoreConvertOutput(t *testing.T) {
	tests := []struct {
		name, src string
		opts      []string
	}{
		{"meta_all", "meta_all", nil},
		{"tiny_cd_long_ec", "tiny_cd", []string{"-l", "--erasure-coding=4,2"}},
		{"meta_dumphw", "meta_dumphw", nil},
		{"flux_only", "flux_only", nil},
		{"bd_long", "bd_long", []string{"-l"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst, out := coreConvert(t, tc.src, tc.opts...)
			checkExpected(t, "core/convert_"+tc.name+".txt", out)
			coreCheckConverted(t, tc.src, dst, len(tc.opts) > 0 && tc.opts[0] == "-l")
		})
	}
}

func TestCoreConvert(t *testing.T) {
	tests := []struct {
		src  string
		opts []string
	}{
		{"tiny_cd", nil},
		{"cd_raw", []string{"-l"}},
		{"dvd_long", []string{"-l"}},
		{"apple_sony", []string{"-l"}},
		{"apple_profile", []string{"-l"}},
		{"priam", []string{"-l"}},
		{"ps3_bd", nil},
		{"flux_lzma", nil},
		// Media without long sectors: plain sectors.
		{"mf2hd", []string{"-l"}},
		// Not dumped sectors are skipped.
		{"blk_ec", []string{"--erasure-coding=2,1"}},
		{"statuses", []string{"-l"}},
		// Images without CD prefix/suffix data: plain sectors in long mode.
		{"cd_tags", []string{"-l"}},
	}
	for _, tc := range tests {
		t.Run(tc.src+strings.Join(tc.opts, ""), func(t *testing.T) {
			dst, _ := coreConvert(t, tc.src, tc.opts...)
			coreCheckConverted(t, tc.src, dst, len(tc.opts) > 0 && tc.opts[0] == "-l")
		})
	}

	// Erasure coding settings reach the output.
	dst, out := coreConvert(t, "blk_ec", "--erasure-coding=2,1")
	if !strings.Contains(out, "RS(2,1) — 2 data + 1 parity blocks per stripe") {
		t.Errorf("no erasure coding line:\n%s", out)
	}
	if ec, ok := openCore(t, dst).ErasureCoding(); !ok || ec.K != 2 || ec.M != 1 ||
		ec.Algorithm != aaru.ErasureCodingRsVandermonde {
		t.Errorf("output erasure coding %+v %v", ec, ok)
	}
}

func TestCoreConvertErrors(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := coreRun(t, "convert", filepath.Join(dir, "missing.aif"), filepath.Join(dir, "out.aif"))
	if code != 2 || !strings.Contains(out, "✗ Cannot open input image (error 2)") {
		t.Errorf("missing input: %d\n%s", code, out)
	}
	code, out, _ = coreRun(t, "convert", fixture(t, "tiny_cd"), filepath.Join(dir, "nodir", "out.aif"))
	if code != 2 || !strings.Contains(out, "✗ Cannot create output image (error 2)") {
		t.Errorf("bad output path: %d\n%s", code, out)
	}
	code, out, _ = coreRun(t, "convert", "--erasure-coding=200,100", fixture(t, "tiny_cd"),
		filepath.Join(dir, "ec.aif"))
	if code != -26 || !strings.Contains(out, "✗ Cannot enable erasure coding (error -26)") {
		t.Errorf("K+M > 255: %d\n%s", code, out)
	}
}

func TestCoreConvertHelpers(t *testing.T) {
	for _, c := range []struct {
		n    uint64
		want string
	}{{0, "0 B"}, {1023, "1023 B"}, {1024, "1.00 KiB"}, {1536, "1.50 KiB"}, {5 << 30, "5.00 GiB"},
		{1 << 62, "4096.00 PiB"}} {
		if got := convertFormatBytes(c.n); got != c.want {
			t.Errorf("convertFormatBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		s    float64
		want string
	}{{0, "0.0 sec"}, {59.94, "59.9 sec"}, {61, "1 min 1 sec"}, {3600, "1 hr 0 min 0 sec"}, {3725.5, "1 hr 2 min 5 sec"}} {
		if got := convertFormatDuration(c.s); got != c.want {
			t.Errorf("convertFormatDuration(%v) = %q, want %q", c.s, got, c.want)
		}
	}
	for _, c := range []struct {
		arg  string
		k, m uint16
		ok   bool
	}{{"4,2", 4, 2, true}, {"16,1", 16, 1, true}, {" 8, 2", 8, 2, true}, {"0,1", 0, 0, false}, {"1,0", 0, 0, false},
		{"70000,1", 0, 0, false}, {"4", 0, 0, false}, {"", 0, 0, false}} {
		k, m, ok := parseErasureCodingArg(c.arg)
		if k != c.k || m != c.m || ok != c.ok {
			t.Errorf("parseErasureCodingArg(%q) = %d,%d,%v", c.arg, k, m, ok)
		}
	}
}
