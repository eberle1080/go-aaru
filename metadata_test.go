package aaru

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"lukechampine.com/blake3"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Unit tests of the metadata getters, setters and clear functions. See
// metadata_scenario_test.go for the write scenarios.

// metaStringAPI binds the per-field convenience methods to their field.
type metaStringAPI struct {
	field MetadataField
	get   func(*Image) (string, error)
	set   func(*Image, string) error
	clear func(*Image) error
}

var metaStringAPIs = []metaStringAPI{
	{MetadataCreator, (*Image).Creator, (*Image).SetCreator, (*Image).ClearCreator},
	{MetadataComments, (*Image).Comments, (*Image).SetComments, (*Image).ClearComments},
	{MetadataMediaTitle, (*Image).MediaTitle, (*Image).SetMediaTitle, (*Image).ClearMediaTitle},
	{MetadataMediaManufacturer, (*Image).MediaManufacturer, (*Image).SetMediaManufacturer, (*Image).ClearMediaManufacturer},
	{MetadataMediaModel, (*Image).MediaModel, (*Image).SetMediaModel, (*Image).ClearMediaModel},
	{MetadataMediaSerialNumber, (*Image).MediaSerialNumber, (*Image).SetMediaSerialNumber, (*Image).ClearMediaSerialNumber},
	{MetadataMediaBarcode, (*Image).MediaBarcode, (*Image).SetMediaBarcode, (*Image).ClearMediaBarcode},
	{MetadataMediaPartNumber, (*Image).MediaPartNumber, (*Image).SetMediaPartNumber, (*Image).ClearMediaPartNumber},
	{MetadataDriveManufacturer, (*Image).DriveManufacturer, (*Image).SetDriveManufacturer, (*Image).ClearDriveManufacturer},
	{MetadataDriveModel, (*Image).DriveModel, (*Image).SetDriveModel, (*Image).ClearDriveModel},
	{MetadataDriveSerialNumber, (*Image).DriveSerialNumber, (*Image).SetDriveSerialNumber, (*Image).ClearDriveSerialNumber},
	{MetadataDriveFirmwareRevision, (*Image).DriveFirmwareRevision, (*Image).SetDriveFirmwareRevision,
		(*Image).ClearDriveFirmwareRevision},
}

func metaValue(f MetadataField, round int) string {
	return strings.Repeat("ß", round) + f.String() + " value " + string(rune('A'+int(f))) + " 💿"
}

// newMetaImage creates a small block image with its sectors written.
func newMetaImage(t *testing.T, path string) *Image {
	t.Helper()
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 8,
		Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i < 8; i++ {
		if err := img.WriteSector(i, false, bytes.Repeat([]byte{byte(i)}, 512), SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

func checkMetaStrings(t *testing.T, img *Image, when string, want map[MetadataField]string) {
	t.Helper()
	for _, api := range metaStringAPIs {
		got, err := api.get(img)
		w, ok := want[api.field]
		switch {
		case !ok && !errors.Is(err, ErrMetadataNotPresent):
			t.Errorf("%s: %v = %q, %v; want ErrMetadataNotPresent", when, api.field, got, err)
		case ok && (err != nil || got != w):
			t.Errorf("%s: %v = %q, %v; want %q", when, api.field, got, err, w)
		}
		gs, errs := img.MetadataString(api.field)
		if gs != got || ErrorCode(errs) != ErrorCode(err) {
			t.Errorf("%s: MetadataString(%v) = %q, %v; field getter %q, %v", when, api.field, gs, errs, got, err)
		}
		raw, err := img.MetadataRaw(api.field)
		if ok && (err != nil || !bytes.Equal(raw, EncodeUTF16(w))) {
			t.Errorf("%s: MetadataRaw(%v) = %x, %v; want %x", when, api.field, raw, err, EncodeUTF16(w))
		}
	}
}

func TestMetadataStringsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	checkMetaStrings(t, img, "new image", nil)
	want := map[MetadataField]string{}
	for _, api := range metaStringAPIs {
		want[api.field] = metaValue(api.field, 1)
		if err := api.set(img, want[api.field]); err != nil {
			t.Fatalf("set %v: %v", api.field, err)
		}
	}
	checkMetaStrings(t, img, "while writing", want)
	mustClose(t, img)

	img = mustOpen(t, path)
	checkMetaStrings(t, img, "after reopening", want)
	mustClose(t, img)

	// Change half of them and clear the other half after resuming.
	img = mustResume(t, path, "compress=false")
	checkMetaStrings(t, img, "resumed", want)
	for i, api := range metaStringAPIs {
		if i%2 == 0 {
			want[api.field] = metaValue(api.field, 2)
			if err := api.set(img, want[api.field]); err != nil {
				t.Fatal(err)
			}
		} else {
			delete(want, api.field)
			if err := api.clear(img); err != nil {
				t.Fatal(err)
			}
		}
	}
	checkMetaStrings(t, img, "changed after resuming", want)
	mustClose(t, img)

	img = mustOpen(t, path)
	checkMetaStrings(t, img, "reopened after resuming", want)
	if n := countIndexEntries(img, BlockTypeMetadataBlock); n != 1 {
		t.Errorf("%d metadata index entries, want 1", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	mustClose(t, img)

	// Clearing everything removes the metadata block.
	img = mustResume(t, path, "compress=false")
	for _, api := range metaStringAPIs {
		if err := api.clear(img); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := img.MediaSequence(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("MediaSequence after clearing everything: %v", err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	checkMetaStrings(t, img, "all cleared", nil)
	if n := countIndexEntries(img, BlockTypeMetadataBlock); n != 0 {
		t.Errorf("%d metadata index entries, want 0", n)
	}
}

func countIndexEntries(img *Image, bt BlockType) int {
	n := 0
	for _, e := range img.indexEntries {
		if e.BlockType == bt {
			n++
		}
	}
	return n
}

func TestMetadataRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	opaque := []byte{0xFF, 0xFE, 'x', 0, 0, 0, 'y'} // not valid as a NUL-terminated string, odd length
	if err := img.SetMetadataRaw(MetadataComments, opaque); err != nil {
		t.Fatal(err)
	}
	opaque[0] = 0 // the data is copied
	if err := img.SetMetadataRaw(MetadataMediaTitle, []byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := img.MetadataRaw(MetadataMediaTitle); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("empty string: %v, want ErrMetadataNotPresent", err)
	}
	for _, f := range []MetadataField{-1, metadataFieldCount} {
		if _, err := img.MetadataRaw(f); !errors.Is(err, ErrMetadataNotPresent) {
			t.Errorf("MetadataRaw(%v): %v", f, err)
		}
		if err := img.SetMetadataRaw(f, nil); !errors.Is(err, ErrMetadataNotPresent) {
			t.Errorf("SetMetadataRaw(%v): %v", f, err)
		}
		if err := img.ClearMetadata(f); !errors.Is(err, ErrMetadataNotPresent) {
			t.Errorf("ClearMetadata(%v): %v", f, err)
		}
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	raw, err := img.MetadataRaw(MetadataComments)
	if err != nil || !bytes.Equal(raw, []byte{0xFF, 0xFE, 'x', 0, 0, 0, 'y'}) {
		t.Errorf("MetadataRaw = %x, %v", raw, err)
	}
	raw[0] = 0 // a copy is returned
	if again, _ := img.MetadataRaw(MetadataComments); again[0] != 0xFF {
		t.Error("MetadataRaw returned internal storage")
	}
	if s, err := img.Comments(); err != nil || s != "\ufeffx" {
		t.Errorf("Comments = %q, %v", s, err)
	}
	// A zero-length string writes the block, but it reads back as absent.
	if _, err := img.MediaTitle(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("MediaTitle: %v", err)
	}
	if MetadataDriveModel.String() != "DriveModel" || MetadataField(99).String() != "MetadataField(99)" {
		t.Error("MetadataField.String")
	}
}

func TestUTF16(t *testing.T) {
	cases := []struct {
		s   string
		enc []byte
	}{
		{"", []byte{0, 0}},
		{"A", []byte{'A', 0, 0, 0}},
		{"é💾", []byte{0xE9, 0, 0x3D, 0xD8, 0xBE, 0xDC, 0, 0}},
	}
	for _, c := range cases {
		if got := EncodeUTF16(c.s); !bytes.Equal(got, c.enc) {
			t.Errorf("EncodeUTF16(%q) = %x, want %x", c.s, got, c.enc)
		}
		if got := DecodeUTF16(c.enc); got != c.s {
			t.Errorf("DecodeUTF16(%x) = %q, want %q", c.enc, got, c.s)
		}
	}
	for _, c := range []struct {
		in   []byte
		want string
	}{
		{[]byte{'a', 0, 'b', 0}, "ab"},                 // no terminator
		{[]byte{'a', 0, 0, 0, 'b', 0}, "a"},            // stops at the first NUL
		{[]byte{'a', 0, 'b'}, "a"},                     // trailing odd byte ignored
		{[]byte{0x3D, 0xD8, 'a', 0}, "�a"},             // unpaired surrogate
		{nil, ""},                                      //
		{[]byte{0x00, 0xD8, 0x00, 0xDC}, "\U00010000"}, //
	} {
		if got := DecodeUTF16(c.in); got != c.want {
			t.Errorf("DecodeUTF16(%x) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := DecodeUTF16(EncodeUTF16("bad \xff utf8")); got != "bad � utf8" {
		t.Errorf("invalid UTF-8 round trip = %q", got)
	}
}

func TestMediaSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	if _, _, err := img.MediaSequence(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("MediaSequence on a new image: %v", err)
	}
	if err := img.ClearMediaSequence(); err != nil { // nothing to clear
		t.Errorf("ClearMediaSequence: %v", err)
	}
	if err := img.SetMediaSequence(3, 7); err != nil {
		t.Fatal(err)
	}
	if s, l, err := img.MediaSequence(); s != 3 || l != 7 || err != nil {
		t.Errorf("MediaSequence = %d/%d, %v", s, l, err)
	}
	// Clearing it while a string exists keeps the block.
	img.SetCreator("me")
	if err := img.ClearMediaSequence(); err != nil {
		t.Fatal(err)
	}
	if s, l, err := img.MediaSequence(); s != 0 || l != 0 || err != nil {
		t.Errorf("MediaSequence after clearing it = %d/%d, %v; want 0/0", s, l, err)
	}
	// Clearing the last string drops the block.
	img.ClearCreator()
	if _, _, err := img.MediaSequence(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("MediaSequence after clearing everything: %v", err)
	}
	// The sequence alone keeps the block.
	img.SetMediaSequence(-1, 2)
	img.SetCreator("x")
	img.ClearCreator()
	if s, l, err := img.MediaSequence(); s != -1 || l != 2 || err != nil {
		t.Errorf("MediaSequence = %d/%d, %v; want -1/2", s, l, err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	if s, l, err := img.MediaSequence(); s != -1 || l != 2 || err != nil {
		t.Errorf("reopened: MediaSequence = %d/%d, %v", s, l, err)
	}
	// Only positive sequences are kept in the image context.
	if img.mediaSequence != 0 || img.lastMediaSequence != 0 {
		t.Errorf("context sequence = %d/%d", img.mediaSequence, img.lastMediaSequence)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	if err := img.SetMediaSequence(4, 4); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if s, l, err := img.MediaSequence(); s != 4 || l != 4 || err != nil {
		t.Errorf("after resuming: MediaSequence = %d/%d, %v", s, l, err)
	}
	if img.mediaSequence != 4 || img.lastMediaSequence != 4 {
		t.Errorf("context sequence = %d/%d", img.mediaSequence, img.lastMediaSequence)
	}
}

func TestGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	if _, _, _, err := img.Geometry(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("Geometry on a new image: %v, want ErrCannotReadBlock", err)
	}
	if err := img.SetGeometry(80, 2, 18); err != nil {
		t.Fatal(err)
	}
	if c, h, s, err := img.Geometry(); c != 80 || h != 2 || s != 18 || err != nil {
		t.Errorf("Geometry = %d/%d/%d, %v", c, h, s, err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	if c, h, s, err := img.Geometry(); c != 80 || h != 2 || s != 18 || err != nil {
		t.Errorf("reopened: Geometry = %d/%d/%d, %v", c, h, s, err)
	}
	if img.cylinders != 80 || img.heads != 2 || img.sectorsPerTrack != 18 {
		t.Errorf("context geometry = %d/%d/%d", img.cylinders, img.heads, img.sectorsPerTrack)
	}
	mustClose(t, img)
	img = mustResume(t, path, "compress=false")
	img.SetGeometry(1, 1, 8)
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if c, h, s, err := img.Geometry(); c != 1 || h != 1 || s != 8 || err != nil {
		t.Errorf("after resuming: Geometry = %d/%d/%d, %v", c, h, s, err)
	}
	if n := countIndexEntries(img, BlockTypeGeometryBlock); n != 1 {
		t.Errorf("%d geometry index entries", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestAaruJSONMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	if _, err := img.AaruJSONMetadata(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("AaruJSONMetadata on a new image: %v", err)
	}
	doc := []byte(`{"AaruMetadata":{"Title":"日本"}}`)
	if err := img.SetAaruJSONMetadata(doc); err != nil {
		t.Fatal(err)
	}
	doc2 := bytes.Clone(doc)
	doc[0] = 'X' // copied
	if got, err := img.AaruJSONMetadata(); err != nil || !bytes.Equal(got, doc2) {
		t.Errorf("AaruJSONMetadata = %q, %v", got, err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	if got, err := img.AaruJSONMetadata(); err != nil || !bytes.Equal(got, doc2) {
		t.Errorf("reopened: AaruJSONMetadata = %q, %v", got, err)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	img.SetAaruJSONMetadata([]byte("{}"))
	mustClose(t, img)
	img = mustOpen(t, path)
	if got, err := img.AaruJSONMetadata(); err != nil || string(got) != "{}" {
		t.Errorf("replaced: AaruJSONMetadata = %q, %v", got, err)
	}
	mustClose(t, img)

	// Setting an empty document removes it.
	img = mustResume(t, path, "compress=false")
	img.SetAaruJSONMetadata(nil)
	if _, err := img.AaruJSONMetadata(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("after setting an empty document: %v", err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if _, err := img.AaruJSONMetadata(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("reopened after removal: %v", err)
	}
	if n := countIndexEntries(img, BlockTypeAaruMetadataJsonBlock); n != 0 {
		t.Errorf("%d JSON index entries", n)
	}
}

// CICM metadata cannot be written (there is no setter); it is read
// from crafted images and kept when they are resumed.
func TestCICMMetadata(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><CICMMetadata/>`)
	b := newCraft(media.GENERIC_HDD)
	b.userData(4)
	b.add(BlockTypeCicmBlock, 0, append(marshal(&CICMBlockHeader{Identifier: BlockTypeCicmBlock,
		Length: uint32(len(xml))}), xml...))
	path := filepath.Join(t.TempDir(), "cicm.aif")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	img := mustOpen(t, path)
	if got, err := img.CICMMetadata(); err != nil || !bytes.Equal(got, xml) {
		t.Errorf("CICMMetadata = %q, %v", got, err)
	}
	if img.info.ImageSize < uint64(len(xml)) {
		t.Errorf("ImageSize %d does not count the CICM block", img.info.ImageSize)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	img.SetCreator("resumed")
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if got, err := img.CICMMetadata(); err != nil || !bytes.Equal(got, xml) {
		t.Errorf("after resuming: CICMMetadata = %q, %v", got, err)
	}

	// A wrong identifier, or a truncated block, hides it.
	for name, raw := range map[string][]byte{
		"identifier": append(marshal(&CICMBlockHeader{Identifier: BlockTypeAaruMetadataJsonBlock,
			Length: uint32(len(xml))}), xml...),
		"truncated": marshal(&CICMBlockHeader{Identifier: BlockTypeCicmBlock, Length: 1 << 30}),
	} {
		b := newCraft(media.GENERIC_HDD)
		b.userData(4)
		b.add(BlockTypeCicmBlock, 0, raw)
		img, err := OpenReader(bytes.NewReader(b.bytes()), int64(len(b.buf)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := img.CICMMetadata(); !errors.Is(err, ErrCannotReadBlock) {
			t.Errorf("%s: CICMMetadata: %v", name, err)
		}
		img.Close()
	}
}

func TestDumpHardware(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	if _, err := img.DumpHardware(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("DumpHardware on a new image: %v", err)
	}
	if _, err := img.DumpHardwareRaw(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("DumpHardwareRaw on a new image: %v", err)
	}
	hw := metaHardware()
	if err := img.SetDumpHardware(hw); err != nil {
		t.Fatal(err)
	}
	want := metaHardware()
	slices.Reverse(want[0].Extents) // sorted by start
	check := func(img *Image, when string) {
		t.Helper()
		got, err := img.DumpHardware()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: DumpHardware = %+v, %v; want %+v", when, got, err, want)
		}
		raw, err := img.DumpHardwareRaw()
		enc, _ := EncodeDumpHardware(want)
		if err != nil || !bytes.Equal(raw, enc) {
			t.Errorf("%s: DumpHardwareRaw = %x, %v; want %x", when, raw, err, enc)
		}
	}
	check(img, "while writing")
	mustClose(t, img)
	img = mustOpen(t, path)
	check(img, "after reopening")
	mustClose(t, img)

	// The raw form: strings are NUL terminated, lengths include the NUL.
	raw, _ := EncodeDumpHardware(hw[1:2])
	h, _ := unmarshal[DumpHardwareHeader](raw)
	eh, _ := unmarshal[DumpHardwareEntry](raw[dumpHardwareHeaderSize:])
	if h.Entries != 1 || eh.ManufacturerLength != uint32(len("Ünïcode Corp")+1) || eh.ModelLength != 0 ||
		eh.SoftwareNameLength != uint32(len("go-aaru")+1) || eh.Extents != 1 || h.CRC64 != crc64.Checksum(raw[dumpHardwareHeaderSize:]) {
		t.Errorf("encoded header %+v, entry %+v", h, eh)
	}

	// Replace on resume, then remove.
	img = mustResume(t, path, "compress=false")
	want = hw[2:]
	if err := img.SetDumpHardware(want); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	check(img, "replaced after resuming")
	mustClose(t, img)
	img = mustResume(t, path, "compress=false")
	if err := img.SetDumpHardware(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := img.DumpHardware(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("after removal: %v", err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if _, err := img.DumpHardware(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("reopened after removal: %v", err)
	}
	if n := countIndexEntries(img, BlockTypeDumpHardwareBlock); n != 0 {
		t.Errorf("%d dump hardware index entries", n)
	}
}

func TestDumpHardwareRawValidation(t *testing.T) {
	img := newMetaImage(t, filepath.Join(t.TempDir(), "meta.aif"))
	defer img.Close()

	build := func(entry DumpHardwareEntry, rest []byte) []byte {
		payload := append(appendMarshal(nil, &entry), rest...)
		h := DumpHardwareHeader{Identifier: BlockTypeDumpHardwareBlock, Entries: 1, Length: uint32(len(payload)),
			CRC64: crc64.Checksum(payload)}
		return append(marshal(&h), payload...)
	}
	good := build(DumpHardwareEntry{ManufacturerLength: 3, SerialLength: 2, Extents: 2},
		append([]byte("abc12"), marshal([]DumpExtent{{5, 6}, {1, 2}})...))

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"empty", nil, ErrIncorrectDataSize},
		{"short", good[:dumpHardwareHeaderSize-1], ErrIncorrectDataSize},
		{"identifier", append(marshal(&DumpHardwareHeader{Identifier: BlockTypeCicmBlock}), good[18:]...), ErrCannotReadBlock},
		{"truncated", good[:len(good)-1], ErrIncorrectDataSize},
		{"trailing", build(DumpHardwareEntry{}, []byte{1}), ErrIncorrectDataSize},
		{"string past the end", build(DumpHardwareEntry{ModelLength: 10}, []byte("abc")), ErrIncorrectDataSize},
		{"extents past the end", build(DumpHardwareEntry{Extents: 1}, make([]byte, 15)), ErrIncorrectDataSize},
		{"entry past the end", func() []byte {
			h := DumpHardwareHeader{Identifier: BlockTypeDumpHardwareBlock, Entries: 1, Length: 3,
				CRC64: crc64.Checksum([]byte{1, 2, 3})}
			return append(marshal(&h), 1, 2, 3)
		}(), ErrIncorrectDataSize},
	}
	bad := bytes.Clone(good)
	bad[len(bad)-1] ^= 1
	cases = append(cases, struct {
		name string
		raw  []byte
		want error
	}{"crc", bad, ErrInvalidBlockCRC})
	for _, c := range cases {
		if err := img.SetDumpHardwareRaw(c.raw); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := img.DumpHardware(); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("rejected blocks were stored: %v", err)
	}

	// The last byte of every string is forced to NUL and the extents are
	// sorted.
	if err := img.SetDumpHardwareRaw(good); err != nil {
		t.Fatal(err)
	}
	got, err := img.DumpHardware()
	want := []DumpHardware{{Manufacturer: "ab", Serial: "1", Extents: []DumpExtent{{1, 2}, {5, 6}}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("DumpHardware = %+v, %v; want %+v", got, err, want)
	}
	raw, _ := img.DumpHardwareRaw()
	if !bytes.Contains(raw, []byte("ab\x001\x00")) {
		t.Errorf("raw strings not NUL terminated: %q", raw)
	}
	if h, _ := unmarshal[DumpHardwareHeader](raw); h.CRC64 != crc64.Checksum(raw[dumpHardwareHeaderSize:]) {
		t.Error("raw CRC not recomputed")
	}
}

// Damaged dump hardware blocks are ignored when opening.
func TestDumpHardwareDamaged(t *testing.T) {
	enc, _ := EncodeDumpHardware(metaHardware())
	for name, mutate := range map[string]func(b []byte) []byte{
		"crc":        func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		"identifier": func(b []byte) []byte { b[0] ^= 1; return b },
		"no length":  func(b []byte) []byte { clear(b[6:10]); return b[:18] },
		"entries": func(b []byte) []byte {
			b[4]++ // one entry more than the payload holds
			return b
		},
	} {
		b := newCraft(media.GENERIC_HDD)
		b.userData(4)
		b.add(BlockTypeDumpHardwareBlock, 0, mutate(bytes.Clone(enc)))
		img, err := OpenReader(bytes.NewReader(b.bytes()), int64(len(b.buf)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := img.DumpHardware(); !errors.Is(err, ErrCannotReadBlock) {
			t.Errorf("%s: DumpHardware: %v", name, err)
		}
		img.Close()
	}
}

func TestTape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.LTO, SectorSize: 512, Tape: true, Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := img.TapeFiles(); !errors.Is(err, ErrTapeFileNotFound) {
		t.Errorf("TapeFiles on a new image: %v", err)
	}
	if _, err := img.TapePartitions(); !errors.Is(err, ErrTapePartitionNotFound) {
		t.Errorf("TapePartitions on a new image: %v", err)
	}
	img.SetTapeFile(0, 1, 0, 9)
	img.SetTapeFile(0, 2, 10, 19)
	img.SetTapeFile(1, 1, 0, 4)
	img.SetTapeFile(0, 1, 0, 8) // redefined: moves to the end
	img.SetTapePartition(0, 0, 19)
	img.SetTapePartition(1, 0, 4)
	img.SetTapePartition(0, 0, 20)
	wantFiles := []TapeFileEntry{{2, 0, 10, 19}, {1, 1, 0, 4}, {1, 0, 0, 8}}
	wantParts := []TapePartitionEntry{{1, 0, 4}, {0, 0, 20}}
	check := func(img *Image, when string) {
		t.Helper()
		files, err := img.TapeFiles()
		if err != nil || !reflect.DeepEqual(files, wantFiles) {
			t.Errorf("%s: TapeFiles = %+v, %v; want %+v", when, files, err, wantFiles)
		}
		parts, err := img.TapePartitions()
		if err != nil || !reflect.DeepEqual(parts, wantParts) {
			t.Errorf("%s: TapePartitions = %+v, %v; want %+v", when, parts, err, wantParts)
		}
		if a, b, err := img.TapeFile(0, 1); a != 0 || b != 8 || err != nil {
			t.Errorf("%s: TapeFile(0, 1) = %d-%d, %v", when, a, b, err)
		}
		if _, _, err := img.TapeFile(2, 1); !errors.Is(err, ErrTapeFileNotFound) {
			t.Errorf("%s: TapeFile(2, 1): %v", when, err)
		}
		if a, b, err := img.TapePartition(1); a != 0 || b != 4 || err != nil {
			t.Errorf("%s: TapePartition(1) = %d-%d, %v", when, a, b, err)
		}
		if _, _, err := img.TapePartition(2); !errors.Is(err, ErrTapePartitionNotFound) {
			t.Errorf("%s: TapePartition(2): %v", when, err)
		}
	}
	check(img, "while writing")
	mustClose(t, img)
	img = mustOpen(t, path)
	check(img, "after reopening")
	// 8 bytes of (empty) tape DDT, plus the tape blocks' entries.
	if img.info.ImageSize != 8+3*tapeFileEntrySize+2*tapePartitionEntrySize {
		t.Errorf("ImageSize = %d", img.info.ImageSize)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	check(img, "resumed")
	img.SetTapeFile(0, 3, 20, 20)
	wantFiles = append(wantFiles, TapeFileEntry{3, 0, 20, 20})
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	check(img, "after resuming")
	if n := countIndexEntries(img, BlockTypeTapeFileBlock); n != 1 {
		t.Errorf("%d tape file index entries", n)
	}
}

// Tape blocks with an entry count of 0 are read using the payload length.
// go-aaru always writes the count, so the image is written with go-aaru and
// the counts are then cleared (the CRC64 covers the entries only).
func TestTapeBlocksWithoutEntryCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.LTO, SectorSize: 512, Tape: true,
		Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	img.SetTapeFile(0, 1, 0, 9)
	img.SetTapeFile(0, 2, 10, 19)
	img.SetTapeFile(1, 1, 0, 4)
	img.SetTapeFile(0, 1, 0, 8)
	img.SetTapePartition(0, 0, 19)
	img.SetTapePartition(1, 0, 4)
	mustClose(t, img)
	wantFiles := []TapeFileEntry{{2, 0, 10, 19}, {1, 1, 0, 4}, {1, 0, 0, 8}}
	wantParts := []TapePartitionEntry{{0, 0, 19}, {1, 0, 4}}

	// Clear the entry counts.
	img = mustOpen(t, path)
	var fileOff, partOff uint64
	for _, e := range img.indexEntries {
		switch e.BlockType {
		case BlockTypeTapeFileBlock:
			fileOff = e.Offset
		case BlockTypeTapePartitionBlock:
			partOff = e.Offset
		}
	}
	mustClose(t, img)
	if fileOff == 0 || partOff == 0 {
		t.Fatalf("tape blocks not found (%d, %d)", fileOff, partOff)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fh, _ := unmarshal[TapeFileHeader](raw[fileOff:])
	ph, _ := unmarshal[TapePartitionHeader](raw[partOff:])
	if fh.Entries != 3 || fh.Length != 3*tapeFileEntrySize || ph.Entries != 2 || ph.Length != 2*tapePartitionEntrySize {
		t.Fatalf("tape headers as written: %+v %+v", fh, ph)
	}
	fh.Entries, ph.Entries = 0, 0
	copy(raw[fileOff:], marshal(&fh))
	copy(raw[partOff:], marshal(&ph))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	img = mustOpen(t, path)
	defer img.Close()
	files, err := img.TapeFiles()
	if err != nil || !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("TapeFiles = %+v, %v; want %+v", files, err, wantFiles)
	}
	parts, err := img.TapePartitions()
	if err != nil || !reflect.DeepEqual(parts, wantParts) {
		t.Errorf("TapePartitions = %+v, %v; want %+v", parts, err, wantParts)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestChecksumsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cks.aif")
	o := ParseOptions("compress=false;md5=true;sha1=true;sha256=true;spamsum=true;blake3=true")
	img, err := Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 8,
		Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for i := uint64(0); i < 8; i++ {
		s := bytes.Repeat([]byte{byte(i * 3)}, 512)
		all = append(all, s...)
		img.WriteSector(i, false, s, SectorStatusDumped)
	}
	if c := img.Checksums(); c.MD5 != nil || c.SpamSum != "" {
		t.Errorf("checksums before closing: %+v", c)
	}
	mustClose(t, img)
	written := img.Checksums()

	img = mustOpen(t, path)
	defer img.Close()
	c := img.Checksums()
	m, s1, s2, b3 := md5.Sum(all), sha1.Sum(all), sha256.Sum256(all), blake3.Sum256(all)
	if !bytes.Equal(c.MD5, m[:]) || !bytes.Equal(c.SHA1, s1[:]) || !bytes.Equal(c.SHA256, s2[:]) ||
		!bytes.Equal(c.Blake3, b3[:]) || c.SpamSum == "" {
		t.Errorf("Checksums = %+v", c)
	}
	if !reflect.DeepEqual(c, written) {
		t.Errorf("Checksums after reopening %+v, after closing %+v", c, written)
	}
	c.MD5[0] ^= 1
	if img.Checksums().MD5[0] == c.MD5[0] {
		t.Error("Checksums returned internal storage")
	}
}

// Malformed checksum entries are skipped.
func TestChecksumsBlockMalformed(t *testing.T) {
	entry := func(typ ChecksumAlgorithm, digest []byte) []byte {
		return append(marshal(&ChecksumEntry{Type: typ, Length: uint32(len(digest))}), digest...)
	}
	var body []byte
	body = append(body, entry(ChecksumMd5, make([]byte, 15))...)             // wrong length: skipped
	body = append(body, entry(ChecksumSha1, bytes.Repeat([]byte{1}, 20))...) // OK
	body = append(body, entry(99, []byte("unknown"))...)                     // unknown: skipped
	body = append(body, entry(ChecksumSpamSum, []byte("3:abc:def\x00junk"))...)
	body = append(body, entry(ChecksumSha256, make([]byte, 32))[:20]...) // truncated: stops
	h := ChecksumHeader{Identifier: BlockTypeChecksumBlock, Length: uint32(len(body)), Entries: 6}
	b := newCraft(media.GENERIC_HDD)
	b.userData(2)
	b.add(BlockTypeChecksumBlock, 0, append(marshal(&h), body...))
	img, err := OpenReader(bytes.NewReader(b.bytes()), int64(len(b.buf)))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	c := img.Checksums()
	if c.MD5 != nil || !bytes.Equal(c.SHA1, bytes.Repeat([]byte{1}, 20)) || c.SpamSum != "3:abc:def" || c.SHA256 != nil {
		t.Errorf("Checksums = %+v", c)
	}
}

func TestSectorCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cd.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 10,
		NegativeSectors: 3, OverflowSectors: 2, Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if img.UserSectors() != 10 || img.NegativeSectors() != 3 || img.OverflowSectors() != 2 {
		t.Errorf("while writing: %d/%d/%d", img.UserSectors(), img.NegativeSectors(), img.OverflowSectors())
	}
	img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 9, Session: 1}})
	for i := uint64(0); i < 12; i++ {
		img.WriteSector(i, false, make([]byte, 2048), SectorStatusDumped)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if img.UserSectors() != 10 || img.NegativeSectors() != 3 || img.OverflowSectors() != 2 {
		t.Errorf("%d/%d/%d, want 10/3/2", img.UserSectors(), img.NegativeSectors(), img.OverflowSectors())
	}
	// Version 1 images.
	b, _ := v1FloppyImage()
	v1, err := OpenReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer v1.Close()
	if v1.UserSectors() != 2880 || v1.NegativeSectors() != 0 || v1.OverflowSectors() != 0 {
		t.Errorf("v1: %d/%d/%d", v1.UserSectors(), v1.NegativeSectors(), v1.OverflowSectors())
	}
}

func TestReadableTags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cd.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 4,
		Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 3, Session: 1}})
	for i := uint64(0); i < 4; i++ {
		img.WriteSector(i, false, make([]byte, 2048), SectorStatusDumped)
		img.WriteSectorTag(i, false, SectorTagCdSubchannel, make([]byte, 96))
	}
	img.WriteMediaTag(MediaTagScsiInquiry, []byte("inquiry"))
	img.WriteMediaTag(MediaTagCdToc, []byte("toc"))
	img.WriteMediaTag(-1, []byte("not reported"))
	if got := img.ReadableMediaTags(); !reflect.DeepEqual(got, []MediaTagType{MediaTagCdToc, MediaTagScsiInquiry}) {
		t.Errorf("ReadableMediaTags while writing = %v", got)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if got := img.ReadableMediaTags(); !reflect.DeepEqual(got, []MediaTagType{MediaTagCdToc, MediaTagScsiInquiry}) {
		t.Errorf("ReadableMediaTags = %v", got)
	}
	if got := img.ReadableSectorTags(); !slices.Contains(got, SectorTagCdSubchannel) {
		t.Errorf("ReadableSectorTags = %v, want the subchannel", got)
	}
	blkPath := filepath.Join(t.TempDir(), "blk.aif")
	mustClose(t, newMetaImage(t, blkPath))
	blk := mustOpen(t, blkPath)
	defer blk.Close()
	if got := blk.ReadableSectorTags(); len(got) != 0 {
		t.Errorf("ReadableSectorTags of a plain floppy = %v", got)
	}
}

// Every setter refuses read-only images and every accessor closed ones.
func TestMetadataReadOnlyAndClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	mustClose(t, newMetaImage(t, path))
	img := mustOpen(t, path)
	dpm, _ := metaDPM(2, 1).MarshalBinary()
	hw, _ := EncodeDumpHardware(metaHardware())
	setters := map[string]func() error{
		"SetGeometry":         func() error { return img.SetGeometry(1, 1, 1) },
		"SetMediaSequence":    func() error { return img.SetMediaSequence(1, 1) },
		"ClearMediaSequence":  img.ClearMediaSequence,
		"SetMetadataRaw":      func() error { return img.SetMetadataRaw(MetadataCreator, []byte{1, 0}) },
		"SetMetadataString":   func() error { return img.SetMetadataString(MetadataCreator, "x") },
		"ClearMetadata":       func() error { return img.ClearMetadata(MetadataCreator) },
		"SetAaruJSONMetadata": func() error { return img.SetAaruJSONMetadata([]byte("{}")) },
		"SetDumpHardware":     func() error { return img.SetDumpHardware(metaHardware()) },
		"SetDumpHardwareRaw":  func() error { return img.SetDumpHardwareRaw(hw) },
		"SetDPM":              func() error { return img.SetDPM(metaDPM(2, 1)) },
		"SetDPMRaw":           func() error { return img.SetDPMRaw(dpm) },
		"ClearDPM":            img.ClearDPM,
		"SetTapeFile":         func() error { return img.SetTapeFile(0, 0, 0, 0) },
		"SetTapePartition":    func() error { return img.SetTapePartition(0, 0, 0) },
		"WriteFluxCapture":    func() error { return img.WriteFluxCapture(FluxCaptureKey{}, 1, 1, nil, nil) },
	}
	for _, api := range metaStringAPIs {
		setters["Set"+api.field.String()] = func() error { return api.set(img, "x") }
		setters["Clear"+api.field.String()] = func() error { return api.clear(img) }
	}
	for name, f := range setters {
		if err := f(); !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s on a read-only image: %v, want ErrReadOnly", name, err)
		}
	}
	mustClose(t, img)

	getters := map[string]func() error{
		"Geometry":         func() error { _, _, _, err := img.Geometry(); return err },
		"MediaSequence":    func() error { _, _, err := img.MediaSequence(); return err },
		"MetadataRaw":      func() error { _, err := img.MetadataRaw(MetadataCreator); return err },
		"MetadataString":   func() error { _, err := img.MetadataString(MetadataCreator); return err },
		"CICMMetadata":     func() error { _, err := img.CICMMetadata(); return err },
		"AaruJSONMetadata": func() error { _, err := img.AaruJSONMetadata(); return err },
		"DumpHardware":     func() error { _, err := img.DumpHardware(); return err },
		"DumpHardwareRaw":  func() error { _, err := img.DumpHardwareRaw(); return err },
		"DPM":              func() error { _, err := img.DPM(); return err },
		"DPMRaw":           func() error { _, err := img.DPMRaw(); return err },
		"TapeFile":         func() error { _, _, err := img.TapeFile(0, 0); return err },
		"TapePartition":    func() error { _, _, err := img.TapePartition(0); return err },
		"TapeFiles":        func() error { _, err := img.TapeFiles(); return err },
		"TapePartitions":   func() error { _, err := img.TapePartitions(); return err },
		"FluxCaptures":     func() error { _, err := img.FluxCaptures(); return err },
		"ReadFluxCapture":  func() error { _, _, err := img.ReadFluxCapture(FluxCaptureKey{}); return err },
		"SetGeometry":      func() error { return img.SetGeometry(1, 1, 1) },
		"SetCreator":       func() error { return img.SetCreator("x") },
		"ClearDPM":         img.ClearDPM,
	}
	for name, f := range getters {
		if err := f(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s on a closed image: %v, want ErrClosed", name, err)
		}
	}
}

// Malformed metadata blocks never panic and only well-formed strings are
// loaded.
func TestMetadataBlockMalformed(t *testing.T) {
	build := func(h MetadataBlockHeader, body []byte) []byte {
		b := newCraft(media.GENERIC_HDD)
		b.userData(2)
		b.add(BlockTypeMetadataBlock, 0, append(marshal(&h), body...))
		return b.bytes()
	}
	str := EncodeUTF16("ok")
	base := MetadataBlockHeader{Identifier: BlockTypeMetadataBlock, BlockSize: metadataBlockHeaderSize + 6,
		MediaSequence: 1, LastMediaSequence: 2, CreatorOffset: metadataBlockHeaderSize, CreatorLength: 6}

	cases := []struct {
		name    string
		h       func(h *MetadataBlockHeader)
		body    []byte
		creator bool
		present bool
	}{
		{"valid", func(h *MetadataBlockHeader) {}, str, true, true},
		{"string past the block", func(h *MetadataBlockHeader) { h.CreatorLength = 8 }, str, false, true},
		{"offset overflow", func(h *MetadataBlockHeader) { h.CreatorOffset = 0xFFFFFFFE; h.CreatorLength = 4 }, str, false, true},
		{"string inside the header", func(h *MetadataBlockHeader) { h.CreatorOffset = 0 }, str, true, true},
		{"block past the file", func(h *MetadataBlockHeader) { h.BlockSize = 1 << 30 }, str, false, false},
		{"wrong identifier", func(h *MetadataBlockHeader) { h.Identifier = BlockTypeCicmBlock }, str, false, false},
		{"block smaller than its header", func(h *MetadataBlockHeader) { h.BlockSize = 4; h.CreatorLength = 0 }, nil, false, true},
	}
	for _, c := range cases {
		h := base
		c.h(&h)
		raw := build(h, c.body)
		img, err := OpenReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		_, _, err = img.MediaSequence()
		if (err == nil) != c.present {
			t.Errorf("%s: MediaSequence: %v", c.name, err)
		}
		creator, err := img.MetadataRaw(MetadataCreator)
		if (err == nil) != c.creator {
			t.Errorf("%s: creator = %x, %v", c.name, creator, err)
		}
		img.Close()
	}
}

func TestMetadataBlockLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.aif")
	img := newMetaImage(t, path)
	img.SetMediaSequence(1, 2)
	img.SetComments("c")
	img.SetDriveModel("dm")
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	h := img.metadataBlockHeader
	want := MetadataBlockHeader{Identifier: BlockTypeMetadataBlock, BlockSize: metadataBlockHeaderSize + 4 + 6,
		MediaSequence: 1, LastMediaSequence: 2, CommentsOffset: metadataBlockHeaderSize, CommentsLength: 4,
		DriveModelOffset: metadataBlockHeaderSize + 4, DriveModelLength: 6}
	if h != want {
		t.Errorf("metadata header = %+v, want %+v", h, want)
	}
	if img.info.ImageSize < uint64(want.BlockSize) {
		t.Errorf("ImageSize %d does not count the metadata block", img.info.ImageSize)
	}
}
