package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/ngcw"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/console/ps3/ird"
	"github.com/eberle1080/go-aaru/console/wii"
	"github.com/eberle1080/go-aaru/console/wiiu"
	"github.com/eberle1080/go-aaru/media"
)

// conResult is the outcome of running a converter.
type conResult struct {
	code           int
	stdout, stderr string
}

func (r conResult) all() string { return r.stdout + r.stderr }

// conRun runs a converter, capturing its output with the ANSI escape
// sequences removed.
func conRun(fn func(o *conOut) int) conResult {
	var out, errOut bytes.Buffer
	o := newConOut(&out, &errOut)
	code := fn(o)
	o.flush()
	return conResult{code: code, stdout: conStripANSI(out.String()), stderr: conStripANSI(errOut.String())}
}

var conANSIRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func conStripANSI(s string) string { return conANSIRE.ReplaceAllString(s, "") }

func conStrp(s string) *string { return &s }

// conInfoLine is a label/value line as printed by conOut.info.
func conInfoLine(label, value string) string { return fmt.Sprintf("  %-20s %s\n", label, value) }

func conWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func conMustSucceed(t *testing.T, r conResult) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("conversion failed (%d):\n%s", r.code, r.all())
	}
	if !strings.Contains(r.stdout, "Conversion completed successfully!") {
		t.Fatalf("missing success message:\n%s", r.stdout)
	}
	if r.stderr != "" {
		t.Fatalf("unexpected errors:\n%s", r.stderr)
	}
}

func conMustFail(t *testing.T, r conResult, wantMsg string) {
	t.Helper()
	if r.code != -1 {
		t.Fatalf("exit code %d, want -1:\n%s", r.code, r.all())
	}
	wantMsg = portable(wantMsg)
	if !strings.Contains(r.all(), wantMsg) {
		t.Fatalf("output does not contain %q:\n%s", wantMsg, r.all())
	}
}

// conCheckImage opens an image written by a converter, verifies it and checks
// that reading every sector reproduces disc (the original, encrypted disc
// contents) byte for byte.
func conCheckImage(t *testing.T, path string, disc []byte, mt media.Type) *aaru.Image {
	t.Helper()
	img, err := aaru.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = img.Close() })

	info := img.Info()
	if info.MediaType != mt {
		t.Errorf("media type %v, want %v", info.MediaType, mt)
	}
	if info.SectorSize != conSectorSize {
		t.Errorf("sector size %d", info.SectorSize)
	}
	if want := uint64(len(disc) / conSectorSize); info.Sectors != want {
		t.Fatalf("%d sectors, want %d", info.Sectors, want)
	}
	if info.Application != conAppName || info.ApplicationVersion != "1.0" {
		t.Errorf("application %q %q", info.Application, info.ApplicationVersion)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("verify: %v", err)
	}

	bad := 0
	for s := uint64(0); s < info.Sectors; s++ {
		data, _, err := img.ReadSector(s, false)
		want := disc[s*conSectorSize : (s+1)*conSectorSize]
		if err != nil || !bytes.Equal(data, want) {
			if bad < 5 {
				t.Errorf("sector %d differs from the source disc (err %v)", s, err)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Fatalf("%d sectors differ", bad)
	}
	return img
}

func conMediaTag(t *testing.T, img *aaru.Image, tag aaru.MediaTagType) []byte {
	t.Helper()
	d, err := img.ReadMediaTag(tag)
	if err != nil {
		t.Fatalf("media tag %v: %v", tag, err)
	}
	return d
}

func conHasTag(img *aaru.Image, tag aaru.MediaTagType) bool {
	_, err := img.ReadMediaTag(tag)
	return err == nil
}

func conCheckString(t *testing.T, what string, get func() (string, error), want string) {
	t.Helper()
	got, err := get()
	if err != nil || got != want {
		t.Errorf("%s = %q (%v), want %q", what, got, err, want)
	}
}

func conFileSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// ---- convert-ps3 ----

func TestConvertPS3ISO(t *testing.T) {
	dir := t.TempDir()
	key := conRandomKey(conTestRand(1))
	d := buildPS3Disc(t, key, false)
	in := filepath.Join(dir, "game.iso")
	out := filepath.Join(dir, "game.aif")
	conWriteFile(t, in, d.iso)

	r := conRun(func(o *conOut) int { return convertPS3(o, in, out, conStrp(hex.EncodeToString(key[:])), nil, nil) })
	conMustSucceed(t, r)
	for _, want := range []string{
		"Disc key provided via --disc-key",
		"PlayStation 3 identification found in sector 1",
		"Raw ISO image: 160 sectors (0.00 GiB)",
		"Encryption map: 2 plaintext region(s)",
		"    Region 0: sectors 0–20\n",
		"    Region 1: sectors 60–67\n",
		"Written media tag: PS3 Encryption Map",
		"Converted 160 sectors (320.00 KiB) in ",
		"Test Game Title",
		"Metadata extracted from PARAM.SFO",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(r.stdout, "Copying Source Metadata") {
		t.Error("source metadata copied from an ISO")
	}

	img := conCheckImage(t, out, d.iso, media.PS3BD)
	if got := conMediaTag(t, img, aaru.MediaTagPs3DiscKey); !bytes.Equal(got, key[:]) {
		t.Errorf("disc key tag %x", got)
	}
	wantMap, _ := d.regions.MarshalBinary()
	if got := conMediaTag(t, img, aaru.MediaTagPs3EncryptionMap); !bytes.Equal(got, wantMap) {
		t.Errorf("encryption map tag %x, want %x", got, wantMap)
	}
	for _, tag := range []aaru.MediaTagType{aaru.MediaTagPs3Data1, aaru.MediaTagPs3Data2, aaru.MediaTagPs3Pic} {
		if conHasTag(img, tag) {
			t.Errorf("unexpected media tag %v", tag)
		}
	}
	conCheckString(t, "title", img.MediaTitle, "Test Game Title")
	if _, err := img.MediaPartNumber(); err != nil {
		t.Errorf("part number: %v", err)
	}
}

// Encrypted sectors are stored decrypted: with all-zero plaintext they
// deduplicate to a single stored sector.
func TestConvertPS3StoresDecrypted(t *testing.T) {
	dir := t.TempDir()
	key := conRandomKey(conTestRand(2))
	d := buildPS3Disc(t, key, true)
	in := filepath.Join(dir, "zero.iso")
	out := filepath.Join(dir, "zero.aif")
	conWriteFile(t, in, d.iso)

	conMustSucceed(t, conRun(func(o *conOut) int { return convertPS3(o, in, out, conStrp(hex.EncodeToString(key[:])), nil, nil) }))
	conCheckImage(t, out, d.iso, media.PS3BD)

	// 131 encrypted sectors (262 KiB of ciphertext) plus the plaintext
	// regions (8 random sectors) would not fit in 64 KiB if stored as is.
	if sz := conFileSize(t, out); sz > 64*1024 {
		t.Errorf("image is %d bytes: encrypted sectors were not stored decrypted", sz)
	}
}

func TestConvertPS3Data1Key(t *testing.T) {
	dir := t.TempDir()
	data1 := conRandomKey(conTestRand(4))
	key := ps3.DeriveDiscKey(data1)
	d := buildPS3Disc(t, key, false)
	in := filepath.Join(dir, "game.iso")
	out := filepath.Join(dir, "game.aif")
	conWriteFile(t, in, d.iso)

	r := conRun(func(o *conOut) int {
		// An invalid --disc-key is reported and the data1 key used instead.
		return convertPS3(o, in, out, conStrp("zz"), conStrp(strings.ToUpper(hex.EncodeToString(data1[:]))), nil)
	})
	if r.code != 0 || !strings.Contains(r.stderr, "Invalid --disc-key format (expected 32 hex chars)") ||
		!strings.Contains(r.stdout, "Disc key derived from --data1-key") {
		t.Fatalf("unexpected result %d:\n%s", r.code, r.all())
	}

	img := conCheckImage(t, out, d.iso, media.PS3BD)
	if got := conMediaTag(t, img, aaru.MediaTagPs3Data1); !bytes.Equal(got, data1[:]) {
		t.Errorf("data1 tag %x", got)
	}
	if got := conMediaTag(t, img, aaru.MediaTagPs3DiscKey); !bytes.Equal(got, key[:]) {
		t.Errorf("disc key tag %x", got)
	}
}

// conParseIRD writes a synthetic IRD file, parses it and checks that the
// parser returns what was written.
func conParseIRD(t *testing.T) (string, *ird.File) {
	t.Helper()
	want := conWriteIRD(t, t.TempDir())
	f, err := ird.ParseFile(want.path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 9 || f.GameID != conPS3TitleID || f.GameName != want.gameName || f.Data1 != want.data1 ||
		f.Data2 != want.data2 || !f.HasPIC || f.PIC != want.pic {
		t.Fatalf("synthetic IRD parses as %+v", f)
	}
	return want.path, f
}

func TestConvertPS3IRD(t *testing.T) {
	irdPath, f := conParseIRD(t)
	key := ps3.DeriveDiscKey(f.Data1)
	d := buildPS3Disc(t, key, false)

	for _, mode := range []string{"option", "sidecar-append", "sidecar-replace"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "game.iso")
			out := filepath.Join(dir, "game.aif")
			conWriteFile(t, in, d.iso)
			irdBytes, _ := os.ReadFile(irdPath)

			var irdOpt *string
			switch mode {
			case "option":
				irdOpt = conStrp(irdPath)
			case "sidecar-append":
				conWriteFile(t, in+".ird", irdBytes)
			case "sidecar-replace":
				conWriteFile(t, filepath.Join(dir, "game.ird"), irdBytes)
			}

			r := conRun(func(o *conOut) int { return convertPS3(o, in, out, nil, nil, irdOpt) })
			conMustSucceed(t, r)
			wantLoaded := "Sidecar IRD loaded: BLES00905 — " + f.GameName
			wantDerived := "Disc key derived from sidecar IRD data1"
			if mode == "option" {
				wantLoaded = "IRD loaded: BLES00905 — " + f.GameName + " (v9)"
				wantDerived = "Disc key derived from IRD data1"
			}
			for _, want := range []string{wantLoaded, wantDerived, "Written media tag: PS3 Data2", "IRD Game Name:",
				"IRD Game ID:"} {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("output lacks %q:\n%s", want, r.stdout)
				}
			}

			img := conCheckImage(t, out, d.iso, media.PS3BD)
			if got := conMediaTag(t, img, aaru.MediaTagPs3Data1); !bytes.Equal(got, f.Data1[:]) {
				t.Errorf("data1 tag %x", got)
			}
			if got := conMediaTag(t, img, aaru.MediaTagPs3Data2); !bytes.Equal(got, f.Data2[:]) {
				t.Errorf("data2 tag %x", got)
			}
			if f.HasPIC {
				if got := conMediaTag(t, img, aaru.MediaTagPs3Pic); !bytes.Equal(got, f.PIC[:]) {
					t.Errorf("PIC tag %x", got)
				}
			}
		})
	}
}

func TestConvertPS3KeySidecars(t *testing.T) {
	data1 := conRandomKey(conTestRand(5))
	key := ps3.DeriveDiscKey(data1)
	d := buildPS3Disc(t, key, false)

	tests := []struct {
		name, file string
		content    []byte
		msg        string
		data1      bool
	}{
		{"disc_key append", "game.iso.disc_key", key[:], "Disc key loaded from sidecar .disc_key file", false},
		{"disc_key replace", "game.disc_key", key[:], "Disc key loaded from sidecar .disc_key file", false},
		{"data1 append", "game.iso.data1", data1[:], "Disc key derived from sidecar .data1 file", true},
		{"data1 replace", "game.data1", append(data1[:], 0xFF), "Disc key derived from sidecar .data1 file", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "game.iso")
			out := filepath.Join(dir, "game.aif")
			conWriteFile(t, in, d.iso)
			conWriteFile(t, filepath.Join(dir, tt.file), tt.content)

			r := conRun(func(o *conOut) int { return convertPS3(o, in, out, nil, nil, nil) })
			conMustSucceed(t, r)
			if !strings.Contains(r.stdout, tt.msg) {
				t.Errorf("output lacks %q", tt.msg)
			}
			img := conCheckImage(t, out, d.iso, media.PS3BD)
			if conHasTag(img, aaru.MediaTagPs3Data1) != tt.data1 {
				t.Errorf("data1 tag presence != %v", tt.data1)
			}
		})
	}

	// A short sidecar is ignored.
	dir := t.TempDir()
	in := filepath.Join(dir, "game.iso")
	conWriteFile(t, in, d.iso)
	conWriteFile(t, in+".disc_key", key[:15])
	conMustFail(t, conRun(func(o *conOut) int { return convertPS3(o, in, filepath.Join(dir, "o.aif"), nil, nil, nil) }),
		"No disc key found. Provide --disc-key, --data1-key, --ird, or place sidecar files.")
}

// convert-ps3 from an AaruFormat image: keys come from the media tags and the
// source metadata is copied.
func TestConvertPS3FromAaruFormat(t *testing.T) {
	dir := t.TempDir()
	data1 := conRandomKey(conTestRand(6))
	key := ps3.DeriveDiscKey(data1)
	d := buildPS3Disc(t, key, false)
	in := filepath.Join(dir, "game.iso")
	mid := filepath.Join(dir, "mid.aif")
	out := filepath.Join(dir, "out.aif")
	conWriteFile(t, in, d.iso)

	conMustSucceed(t, conRun(func(o *conOut) int {
		return convertPS3(o, in, mid, nil, conStrp(hex.EncodeToString(data1[:])), nil)
	}))
	conAddSourceMetadata(t, mid)

	r := conRun(func(o *conOut) int { return convertPS3(o, mid, out, nil, nil, nil) })
	conMustSucceed(t, r)
	for _, want := range []string{
		"AaruFormat image: PlayStation 3 Game Blu-ray, 160 sectors",
		"Disc key loaded from source image media tags",
		"Copying Source Metadata",
		"Copied 2 additional media tag(s)",
		"Copied dump hardware information",
		"Copied Aaru JSON metadata",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}

	img := conCheckImage(t, out, d.iso, media.PS3BD)
	conCheckSourceMetadata(t, img)
	// The disc key came from the disc key tag: the source data1 tag is copied.
	if got := conMediaTag(t, img, aaru.MediaTagPs3Data1); !bytes.Equal(got, data1[:]) {
		t.Errorf("data1 tag %x", got)
	}
	conCheckString(t, "title", img.MediaTitle, "Test Game Title")
}

// The data2 and PIC tags of an AaruFormat source converted without the IRD
// are kept.
func TestConvertPS3FromAaruFormatKeepsIRDTags(t *testing.T) {
	irdPath, f := conParseIRD(t)
	d := buildPS3Disc(t, ps3.DeriveDiscKey(f.Data1), false)
	dir := t.TempDir()
	in := filepath.Join(dir, "game.iso")
	mid := filepath.Join(dir, "mid.aif")
	out := filepath.Join(dir, "out.aif")
	conWriteFile(t, in, d.iso)

	conMustSucceed(t, conRun(func(o *conOut) int { return convertPS3(o, in, mid, nil, nil, conStrp(irdPath)) }))
	r := conRun(func(o *conOut) int { return convertPS3(o, mid, out, nil, nil, nil) })
	conMustSucceed(t, r)
	if !strings.Contains(r.stdout, "Copied 3 additional media tag(s)") {
		t.Errorf("unexpected output:\n%s", r.stdout)
	}
	img := conCheckImage(t, out, d.iso, media.PS3BD)
	for tag, want := range map[aaru.MediaTagType][]byte{aaru.MediaTagPs3Data1: f.Data1[:],
		aaru.MediaTagPs3Data2: f.Data2[:], aaru.MediaTagPs3Pic: f.PIC[:]} {
		if got := conMediaTag(t, img, tag); !bytes.Equal(got, want) {
			t.Errorf("tag %v = %x, want %x", tag, got, want)
		}
	}
}

// conAddSourceMetadata resumes an image and adds metadata that the converters
// copy from AaruFormat sources.
func conAddSourceMetadata(t *testing.T, path string) {
	t.Helper()
	img, err := aaru.Resume(path, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(img.SetCreator("Test Creator"))
	must(img.SetComments("Source comments"))
	must(img.SetMediaSequence(3, 4))
	must(img.SetAaruJSONMetadata([]byte(`{"AaruMetadata":{}}`)))
	must(img.SetDumpHardware([]aaru.DumpHardware{{Manufacturer: "ACME", Model: "Drive", SoftwareName: "test",
		Extents: []aaru.DumpExtent{{Start: 0, End: 9}}}}))
	must(img.WriteMediaTag(aaru.MediaTagDvdDmi, bytes.Repeat([]byte{0xA5}, 2052)))
	must(img.Close())
}

func conCheckSourceMetadata(t *testing.T, img *aaru.Image) {
	t.Helper()
	conCheckString(t, "creator", img.Creator, "Test Creator")
	conCheckString(t, "comments", img.Comments, "Source comments")
	if seq, last, err := img.MediaSequence(); err != nil || seq != 3 || last != 4 {
		t.Errorf("media sequence %d/%d (%v)", seq, last, err)
	}
	if js, err := img.AaruJSONMetadata(); err != nil || string(js) != `{"AaruMetadata":{}}` {
		t.Errorf("JSON %q (%v)", js, err)
	}
	if hw, err := img.DumpHardware(); err != nil || len(hw) != 1 || hw[0].Manufacturer != "ACME" {
		t.Errorf("dump hardware %+v (%v)", hw, err)
	}
	if got := conMediaTag(t, img, aaru.MediaTagDvdDmi); !bytes.Equal(got, bytes.Repeat([]byte{0xA5}, 2052)) {
		t.Error("DMI tag not copied")
	}
}

func TestConvertPS3Errors(t *testing.T) {
	dir := t.TempDir()
	key := conRandomKey(conTestRand(8))
	keyHex := conStrp(hex.EncodeToString(key[:]))
	d := buildPS3Disc(t, key, false)
	out := filepath.Join(dir, "out.aif")
	cdImage := conMakeCDImage(t, dir)

	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		conWriteFile(t, p, data)
		return p
	}

	good := write("good.iso", d.iso)
	odd := write("odd.iso", d.iso[:len(d.iso)-1])
	notPS3 := bytes.Clone(d.iso)
	copy(notPS3[conSectorSize:], "PlayStation2")
	tooMany := bytes.Clone(d.iso)
	tooMany[3] = 65
	inverted := bytes.Clone(d.iso)
	inverted[11] = 0xFF // region 0 starts at 0xFF > end

	tests := []struct {
		name, in string
		key      *string
		want     string
	}{
		{"no key", good, nil, "No disc key found. Provide --disc-key, --data1-key, --ird, or place sidecar files."},
		{"missing input", filepath.Join(dir, "missing.iso"), keyHex, "Cannot open input file: No such file or directory"},
		{"odd size", odd, keyHex, "Invalid ISO file size (327679 bytes, not a multiple of 2048)"},
		{"not ps3", write("notps3.iso", notPS3), keyHex, "Sector 1 does not contain PlayStation 3 identification."},
		{"too many regions", write("many.iso", tooMany), keyHex, "Cannot parse encryption map from sector 0 (error -3)"},
		{"inverted region", write("inv.iso", inverted), keyHex, "Cannot parse encryption map from sector 0 (error -5)"},
		{"wrong media type", cdImage, keyHex, "Source image media type is "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := conRun(func(o *conOut) int { return convertPS3(o, tt.in, out, tt.key, nil, nil) })
			conMustFail(t, r, tt.want)
		})
	}

	r := conRun(func(o *conOut) int { return convertPS3(o, good, out, keyHex, nil, conStrp(odd)) })
	if r.code != 0 || !strings.Contains(r.stderr, "Cannot parse IRD file") {
		t.Errorf("bad --ird: %d\n%s", r.code, r.all())
	}
}

// ---- convert-wiiu ----

func wiiuExpectedKeyMap(d *wiiuTestDisc) []byte {
	m := wiiu.PartitionKeyMap{
		{StartSector: 4, EndSector: 7, Key: d.discKey},
		{StartSector: 7, EndSector: 9, Key: d.discKey},
		{StartSector: 9, EndSector: wiiuTestPhysical, Key: d.titleKey},
	}
	b, _ := m.MarshalBinary()
	return b
}

func checkWiiUImage(t *testing.T, out string, d *wiiuTestDisc) *aaru.Image {
	t.Helper()
	img := conCheckImage(t, out, d.wud, media.WUOD)
	if got := conMediaTag(t, img, aaru.MediaTagWiiUDiscKey); !bytes.Equal(got, d.discKey[:]) {
		t.Errorf("disc key tag %x", got)
	}
	if got, want := conMediaTag(t, img, aaru.MediaTagWiiUPartitionKeyMap), wiiuExpectedKeyMap(d); !bytes.Equal(got, want) {
		t.Errorf("key map tag\n%x, want\n%x", got, want)
	}
	conCheckString(t, "part number", img.MediaPartNumber, "WUP-P-ABCE")
	return img
}

func TestConvertWiiU(t *testing.T) {
	d := buildWUD(t)
	keyHex := hex.EncodeToString(d.discKey[:])

	for _, format := range []string{"wud", "wux"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "game."+format)
			out := filepath.Join(dir, "game.aif")
			data := d.wud
			if format == "wux" {
				data = buildWUX(d.wud)
			}
			conWriteFile(t, in, data)

			r := conRun(func(o *conOut) int { return convertWiiU(o, in, out, conStrp(keyHex)) })
			conMustSucceed(t, r)
			for _, want := range []string{
				strings.ToUpper(format) + " image: 12 physical sectors (0.00 GiB)",
				"Found 3 partition(s)",
				"    Partition 0: GM0005000010101A00 start_sector=9\n",
				"    Partition 1: SI                 start_sector=4\n",
				"Title key for GM0005000010101A00 extracted",
				"Extracted 1 title key(s)",
				"    Region 0: sectors 4–7 (SI)\n",
				"    Region 2: sectors 9–12 (GM0005000010101A00)\n",
				"Converted 12 physical sectors (384.00 KiB) in ",
				"Product Code:",
				"WUP-P-ABCE",
			} {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("output lacks %q:\n%s", want, r.stdout)
				}
			}
			img := checkWiiUImage(t, out, d)
			if seq, last, err := img.MediaSequence(); err != nil || seq != 1 || last != 1 {
				t.Errorf("media sequence %d/%d (%v)", seq, last, err)
			}
		})
	}
}

func TestConvertWiiUSidecarsAndAaruFormat(t *testing.T) {
	d := buildWUD(t)

	for _, sc := range []struct{ file, msg string }{
		{"game.wud.disckey", "Disc key loaded from sidecar .disckey file"},
		{"game.key", "Disc key loaded from sidecar .key file"},
	} {
		t.Run(sc.file, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "game.wud")
			out := filepath.Join(dir, "game.aif")
			conWriteFile(t, in, d.wud)
			conWriteFile(t, filepath.Join(dir, sc.file), d.discKey[:])
			r := conRun(func(o *conOut) int { return convertWiiU(o, in, out, nil) })
			conMustSucceed(t, r)
			if !strings.Contains(r.stdout, sc.msg) {
				t.Errorf("output lacks %q", sc.msg)
			}
			checkWiiUImage(t, out, d)
		})
	}

	t.Run("aaruformat", func(t *testing.T) {
		dir := t.TempDir()
		in := filepath.Join(dir, "game.wud")
		mid := filepath.Join(dir, "mid.aif")
		out := filepath.Join(dir, "out.aif")
		conWriteFile(t, in, d.wud)
		conMustSucceed(t, conRun(func(o *conOut) int { return convertWiiU(o, in, mid, conStrp(hex.EncodeToString(d.discKey[:]))) }))
		conAddSourceMetadata(t, mid)

		r := conRun(func(o *conOut) int { return convertWiiU(o, mid, out, nil) })
		conMustSucceed(t, r)
		for _, want := range []string{"AaruFormat WUOD image: 192 logical sectors", "Disc key loaded from source image media tags",
			"Title key for GM0005000010101A00 extracted", "Copied 1 additional media tag(s)"} {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("output lacks %q:\n%s", want, r.stdout)
			}
		}
		img := checkWiiUImage(t, out, d)
		conCheckSourceMetadata(t, img) // the source media sequence (3/4) wins
	})
}

func TestConvertWiiUErrors(t *testing.T) {
	dir := t.TempDir()
	d := buildWUD(t)
	in := filepath.Join(dir, "game.wud")
	out := filepath.Join(dir, "out.aif")
	conWriteFile(t, in, d.wud)
	wrong := d.discKey
	wrong[0] ^= 1

	r := conRun(func(o *conOut) int { return convertWiiU(o, in, out, conStrp(hex.EncodeToString(wrong[:]))) })
	conMustFail(t, r, "TOC decryption failed (signature 0x")
	conMustFail(t, r, "Check that the disc key is correct")
	conMustFail(t, r, "Cannot parse Wii U TOC")

	conMustFail(t, conRun(func(o *conOut) int { return convertWiiU(o, in, out, nil) }),
		"No disc key found. Provide --disc-key or place a sidecar .disckey / .key file.")

	short := filepath.Join(dir, "short.wud")
	conWriteFile(t, short, d.wud[:3*wiiu.PhysicalSectorSize])
	conMustFail(t, conRun(func(o *conOut) int { return convertWiiU(o, short, out, conStrp(hex.EncodeToString(d.discKey[:]))) }),
		"Cannot read TOC sector")

	conMustFail(t, conRun(func(o *conOut) int {
		return convertWiiU(o, conMakeCDImage(t, dir), out, conStrp(hex.EncodeToString(d.discKey[:])))
	}), "expected WUOD")

	r = conRun(func(o *conOut) int {
		return convertWiiU(o, filepath.Join(dir, "missing.wud"), out, conStrp(hex.EncodeToString(d.discKey[:])))
	})
	conMustFail(t, r, "Error: cannot open ")
	conMustFail(t, r, "Cannot open input file: No such file or directory")
}

// ---- convert-ngcw ----

func TestConvertNGCWGameCube(t *testing.T) {
	dir := t.TempDir()
	disc := buildGCDisc(t)
	in := filepath.Join(dir, "game.iso")
	out := filepath.Join(dir, "game.aif")
	conWriteFile(t, in, disc)
	bca := bytes.Repeat([]byte{0x5A}, 64)
	conWriteFile(t, filepath.Join(dir, "game.bca"), bca)

	r := conRun(func(o *conOut) int { return convertNGCW(o, in, out) })
	conMustSucceed(t, r)
	for _, want := range []string{
		"Nintendo GameCube",
		"Test GameCube Title",
		conInfoLine("Maker Code:", "01"),
		conInfoLine("Disc Number:", "2"),
		conInfoLine("Disc Version:", "3"),
		conInfoLine("Region:", "USA"),
		"Storing Junk Map",
		"Imported 64-byte BCA from sidecar file",
		"Metadata extracted",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "Partition") {
		t.Error("GameCube disc reported partitions")
	}

	img := conCheckImage(t, out, disc, media.GOD)
	var jm ngcw.JunkMap
	if err := jm.UnmarshalBinary(conMediaTag(t, img, aaru.MediaTagNgcwJunkMap)); err != nil || len(jm) == 0 {
		t.Fatalf("junk map: %v (%d entries)", err, len(jm))
	}
	var junk uint64
	for _, e := range jm {
		junk += e.Length
		if e.PartitionIndex != ngcw.NoPartition {
			t.Errorf("junk entry %+v has a partition", e)
		}
	}
	// Junk: block 0 from sector 5, block 1, block 3 from sector 2, blocks 4-5,
	// block 6 from sector 4, blocks 7-10, block 12 (partial).
	wantJunk := uint64(11+16+14+2*16+12+4*16+8) * conSectorSize
	if junk != wantJunk {
		t.Errorf("junk map covers %#x bytes, want %#x", junk, wantJunk)
	}
	if !strings.Contains(r.stdout, "Converted 200 sectors (400.00 KiB) in ") ||
		!strings.Contains(r.stdout, "43 data, 157 junk") {
		t.Errorf("unexpected sector summary:\n%s", r.stdout)
	}

	if got := conMediaTag(t, img, aaru.MediaTagDvdBca); !bytes.Equal(got, bca) {
		t.Error("BCA not imported")
	}
	conCheckString(t, "title", img.MediaTitle, "Test GameCube Title")
	conCheckString(t, "part number", img.MediaPartNumber, "GALE01")
	conCheckString(t, "comments", img.Comments, "Platform: Nintendo GameCube, Maker: 01, Version: 3")
	if seq, last, err := img.MediaSequence(); err != nil || seq != 2 || last != 2 {
		t.Errorf("media sequence %d/%d (%v)", seq, last, err)
	}

	// Junk is not stored: the image is much smaller than the disc data.
	if sz := conFileSize(t, out); sz > int64(len(disc))-int64(wantJunk)+32*1024 {
		t.Errorf("image is %d bytes", sz)
	}

	// Again from the AaruFormat image (junk regenerated on read, detected
	// again, source metadata copied).
	conAddSourceMetadata(t, out)
	out2 := filepath.Join(dir, "again.aif")
	r = conRun(func(o *conOut) int { return convertNGCW(o, out, out2) })
	conMustSucceed(t, r)
	if !strings.Contains(r.stdout, "AaruFormat image: 200 sectors") || !strings.Contains(r.stdout, "43 data, 157 junk") {
		t.Errorf("unexpected output:\n%s", r.stdout)
	}
	img2 := conCheckImage(t, out2, disc, media.GOD)
	if got, want := conMediaTag(t, img2, aaru.MediaTagNgcwJunkMap), conMediaTag(t, img, aaru.MediaTagNgcwJunkMap); !bytes.Equal(got, want) {
		t.Error("junk map differs after a second conversion")
	}
	conCheckSourceMetadata(t, img2)
	if got := conMediaTag(t, img2, aaru.MediaTagDvdBca); !bytes.Equal(got, bca) {
		t.Error("BCA not copied")
	}
}

func TestConvertNGCWWii(t *testing.T) {
	dir := t.TempDir()
	disc, parts := buildWiiDisc(t)
	in := filepath.Join(dir, "game.iso")
	out := filepath.Join(dir, "game.aif")
	conWriteFile(t, in, disc)

	r := conRun(func(o *conOut) int { return convertNGCW(o, in, out) })
	conMustSucceed(t, r)
	for _, want := range []string{
		"Nintendo Wii",
		"Found 2 partition(s)",
		"    Partition 0: game  offset=0x90000  data_size=0.2 MiB\n",
		"    Partition 1: update  offset=0x50000  data_size=0.1 MiB\n",
		"Written media tag: Wii Partition Key Map",
		"Storing Junk Map",
		"Converted 480 sectors (960.00 KiB) in ",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}

	img := conCheckImage(t, out, disc, media.WOD)

	var km wii.PartitionKeyMap
	if err := km.UnmarshalBinary(conMediaTag(t, img, aaru.MediaTagWiiPartitionKeyMap)); err != nil {
		t.Fatal(err)
	}
	want := wii.PartitionKeyMap{
		{StartSector: 0x70000 / wii.GroupSize, EndSector: 0x90000 / wii.GroupSize, Key: parts[1].titleKey},
		{StartSector: 0xB0000 / wii.GroupSize, EndSector: 0xE0000 / wii.GroupSize, Key: parts[0].titleKey},
	}
	if len(km) != len(want) || km[0] != want[0] || km[1] != want[1] {
		t.Errorf("key map %+v, want %+v", km, want)
	}

	var jm ngcw.JunkMap
	if err := jm.UnmarshalBinary(conMediaTag(t, img, aaru.MediaTagNgcwJunkMap)); err != nil || len(jm) == 0 {
		t.Fatalf("junk map: %v (%d entries)", err, len(jm))
	}
	for _, e := range jm {
		if e.PartitionIndex != 1 || e.Offset < 0xB0000 || e.Offset+e.Length > 0xE0000 {
			t.Errorf("junk entry outside the game partition: %+v", e)
		}
	}
	conCheckString(t, "comments", img.Comments, "Platform: Nintendo Wii, Maker: 01, Version: 1")
	conCheckString(t, "part number", img.MediaPartNumber, "RSPE01")

	// From the AaruFormat image: partition data is re-encrypted on read and
	// stored as read.
	out2 := filepath.Join(dir, "again.aif")
	r = conRun(func(o *conOut) int { return convertNGCW(o, out, out2) })
	conMustSucceed(t, r)
	if !strings.Contains(r.stdout, "Wii partition parsing from AaruFormat source") {
		t.Errorf("unexpected output:\n%s", r.stdout)
	}
	img2 := conCheckImage(t, out2, disc, media.WOD)
	if conHasTag(img2, aaru.MediaTagWiiPartitionKeyMap) || conHasTag(img2, aaru.MediaTagNgcwJunkMap) {
		t.Error("key or junk map copied from the source")
	}
}

func TestConvertNGCWErrors(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.aif")

	notDisc := filepath.Join(dir, "zero.iso")
	conWriteFile(t, notDisc, make([]byte, 0x8000))
	conMustFail(t, conRun(func(o *conOut) int { return convertNGCW(o, notDisc, out) }), "Not a valid GameCube or Wii disc image")

	short := filepath.Join(dir, "short.iso")
	conWriteFile(t, short, make([]byte, 0x43F))
	conMustFail(t, conRun(func(o *conOut) int { return convertNGCW(o, short, out) }), "Cannot read disc header")

	conMustFail(t, conRun(func(o *conOut) int { return convertNGCW(o, filepath.Join(dir, "missing.iso"), out) }),
		"Cannot open input file: No such file or directory")

	conMustFail(t, conRun(func(o *conOut) int { return convertNGCW(o, conMakeCDImage(t, dir), out) }),
		"expected GOD or WOD")

	// A Wii disc whose partition table points past the end of the file.
	disc, _ := buildWiiDisc(t)
	trunc := filepath.Join(dir, "trunc.iso")
	conWriteFile(t, trunc, disc[:0x50100])
	conMustFail(t, conRun(func(o *conOut) int { return convertNGCW(o, trunc, out) }), "Cannot parse Wii partitions")

	// Unwritable destination.
	gc := filepath.Join(dir, "gc.iso")
	conWriteFile(t, gc, buildGCDisc(t))
	r := conRun(func(o *conOut) int { return convertNGCW(o, gc, filepath.Join(dir, "no", "such", "dir.aif")) })
	conMustFail(t, r, "Cannot create output image: ")
	conMustFail(t, r, "Conversion failed.")
}

// ---- command line ----

func TestConsoleConvertCommandLine(t *testing.T) {
	for _, tt := range []struct {
		name string
		fn   func([]string) int
	}{{"convert-ps3", cmdConvertPS3}, {"convert-wiiu", cmdConvertWiiU}, {"convert-ngcw", cmdConvertNGCW}} {
		if code := tt.fn([]string{"only-one"}); code != -1 {
			t.Errorf("%s with one argument returned %d", tt.name, code)
		}
		if code := tt.fn([]string{"--help"}); code != -1 {
			t.Errorf("%s --help returned %d", tt.name, code)
		}
	}

	// Options before, between and after the positional arguments.
	dir := t.TempDir()
	key := conRandomKey(conTestRand(9))
	d := buildPS3Disc(t, key, false)
	in := filepath.Join(dir, "game.iso")
	conWriteFile(t, in, d.iso)
	for i, args := range [][]string{
		{"--disc-key=" + hex.EncodeToString(key[:]), in, filepath.Join(dir, "a.aif")},
		{in, "--disc-key", hex.EncodeToString(key[:]), filepath.Join(dir, "b.aif")},
		{in, filepath.Join(dir, "c.aif"), "-disc-key=" + hex.EncodeToString(key[:])},
	} {
		if code := cmdConvertPS3(args); code != 0 {
			t.Errorf("case %d: exit %d", i, code)
		}
	}
	conCheckImage(t, filepath.Join(dir, "b.aif"), d.iso, media.PS3BD)

	dw := buildWUD(t)
	wud := filepath.Join(dir, "game.wud")
	conWriteFile(t, wud, dw.wud)
	if code := cmdConvertWiiU([]string{wud, filepath.Join(dir, "w.aif"), "--disc-key=" + hex.EncodeToString(dw.discKey[:])}); code != 0 {
		t.Errorf("convert-wiiu exit %d", code)
	}

	gc := filepath.Join(dir, "gc.iso")
	conWriteFile(t, gc, buildGCDisc(t))
	if code := cmdConvertNGCW([]string{gc, filepath.Join(dir, "gc.aif")}); code != 0 {
		t.Errorf("convert-ngcw exit %d", code)
	}
}

func TestConParseHexKey(t *testing.T) {
	var k [16]byte
	if !conParseHexKey("000102030405060708090a0B0c0D0e0F", &k) || k != [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
		13, 14, 15} {
		t.Errorf("parsed %x", k)
	}
	for _, bad := range []string{"", "00", "000102030405060708090a0b0c0d0e0f00", "g00102030405060708090a0b0c0d0e0f"} {
		if conParseHexKey(bad, &k) {
			t.Errorf("%q accepted", bad)
		}
	}
	// A pair is parsed up to its first non-hex digit.
	if !conParseHexKey("0g0102030405060708090a0b0c0d0e0f", &k) || k[0] != 0 || k[1] != 1 {
		t.Errorf("parsed %x", k)
	}
}

func TestConFormatBytes(t *testing.T) {
	for _, tt := range []struct {
		n    uint64
		want string
	}{{0, "0 B"}, {1023, "1023 B"}, {1024, "1.00 KiB"}, {1536, "1.50 KiB"}, {5 << 30, "5.00 GiB"}, {3 << 50, "3072.00 TiB"}} {
		if got := conFormatBytes(tt.n); got != tt.want {
			t.Errorf("conFormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
