package aaru

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

func newTestImage(t *testing.T, p CreateParams, opts string) *Image {
	t.Helper()
	if p.Application == "" {
		p.Application = "go-aaru-test"
	}
	o := ParseOptions(opts)
	img, err := Create(filepath.Join(t.TempDir(), "t.aif"), p, &o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { img.Close() })
	return img
}

// The table layout chosen by Create.
func TestCreateDDTLayout(t *testing.T) {
	cases := []struct {
		blocks, neg, ovf uint64
		opts             string
		shift            uint8
		levels           uint8
		entries          uint64
		next             uint64
	}{
		{1000, 0, 0, "", 0, 1, 1000, 512},
		{8388607, 0, 0, "", 0, 1, 8388607, 512},
		{8388608, 7, 1, "", 22, 2, 2, alignUp(512+ddtHeader2Size+16, 9)},
		{1 << 23, 0, 0, "", 22, 2, 2, alignUp(512+ddtHeader2Size+16, 9)},
		{1<<23 + 1, 0, 0, "block_alignment=12", 22, 2, 3, alignUp(4096+ddtHeader2Size+24, 12)},
		{1000, 0, 0, "table_shift=4", 4, 2, 63, alignUp(512+ddtHeader2Size+63*8, 9)},
		{1024, 0, 0, "table_shift=4", 4, 2, 64, alignUp(512+ddtHeader2Size+64*8, 9)},
		{1000, 0, 0, "table_shift=0", 0, 1, 1000, 512},
		{1000, 0, 0, "table_shift=-5", 0, 1, 1000, 512},
		{1000, 0, 0, "table_shift=70", 62, 2, 1, alignUp(512+ddtHeader2Size+8, 9)},
		{0, 1, 0, "", 0, 1, 1, 512},
	}
	for _, c := range cases {
		if testing.Short() && c.blocks > 1<<20 {
			continue // closing compresses a multi-MiB DDT; too slow under -race
		}
		img := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: c.blocks - c.neg - c.ovf,
			NegativeSectors: c.neg, OverflowSectors: c.ovf}, c.opts)
		h := img.userDataDDTHeader
		if h.TableShift != c.shift || h.Levels != c.levels || h.Entries != c.entries ||
			img.nextBlockPosition != c.next || uint64(len(img.userDataDDT2)) != c.entries {
			t.Errorf("%d sectors %q: shift=%d levels=%d entries=%d next=%d, want %d %d %d %d", c.blocks, c.opts,
				h.TableShift, h.Levels, h.Entries, img.nextBlockPosition, c.shift, c.levels, c.entries, c.next)
		}
		if h.Blocks != c.blocks || h.Identifier != BlockTypeDeDuplicationTable2 || h.Type != DataTypeUserData {
			t.Errorf("%d sectors: header %+v", c.blocks, h)
		}
	}

	tape := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: 10, Tape: true}, "")
	if !tape.isTape || tape.nextBlockPosition != 512 || tape.userDataDDT2 != nil {
		t.Errorf("tape: isTape=%v next=%d", tape.isTape, tape.nextBlockPosition)
	}
	flux := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512}, "block_alignment=12")
	if !flux.noUserDataDDT || flux.nextBlockPosition != 4096 || flux.dirty.singleLevelDDT {
		t.Errorf("flux-only: noUserDataDDT=%v next=%d", flux.noUserDataDDT, flux.nextBlockPosition)
	}
}

func TestCreateErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := Create(filepath.Join(dir, "a.aif"), CreateParams{Application: strings.Repeat("x", 65)}, nil)
	if !errors.Is(err, ErrInvalidAppNameLength) {
		t.Errorf("long application name: %v", err)
	}
	if _, err := Create(filepath.Join(dir, "b.aif"), CreateParams{Application: strings.Repeat("x", 64),
		UserSectors: 1}, nil); err != nil {
		t.Errorf("64-byte application name: %v", err)
	}
	_, err = Create(filepath.Join(dir, "missing", "c.aif"), CreateParams{UserSectors: 1}, nil)
	if !errors.Is(err, ErrCannotCreateFile) || ErrorCode(err) != -19 {
		t.Errorf("unwritable path: %v", err)
	}
	o := DefaultOptions()
	o.DataShift = 40
	if _, err := Create(filepath.Join(dir, "d.aif"), CreateParams{UserSectors: 1}, &o); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("data shift 40: %v", err)
	}
}

func TestCreateInfo(t *testing.T) {
	img := newTestImage(t, CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 10,
		Application: "app\x00junk", ApplicationMajorVersion: 3, ApplicationMinorVersion: 14}, "")
	info := img.Info()
	if info.Application != "app" || info.ApplicationVersion != "3.14" || info.Version != "2.0" ||
		info.MediaType != media.CDROM || info.MetadataMediaType != media.OpticalDisc || info.Sectors != 10 ||
		info.SectorSize != 2048 || info.CreationTime.IsZero() {
		t.Errorf("info %+v", info)
	}
	h := img.Header()
	if h.Identifier != AaruMagic || h.ImageMajorVersion != 2 || string(h.Application[:8]) != "app\x00junk" ||
		h.GUID == [16]byte{} {
		t.Errorf("header %+v", h)
	}
	if !img.Writable() {
		t.Error("not writable")
	}
}

func TestWriteAfterClose(t *testing.T) {
	img := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: 4}, "")
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		img.WriteSector(0, false, make([]byte, 512), SectorStatusDumped),
		img.WriteSectorLong(0, false, make([]byte, 512), SectorStatusDumped),
		img.WriteSectorTag(0, false, SectorTagAppleSony, make([]byte, 12)),
		img.WriteMediaTag(MediaTagCdToc, []byte{1}),
	} {
		if !errors.Is(err, ErrClosed) {
			t.Errorf("after close: %v", err)
		}
	}
}

func TestFluxOnlyRejectsSectors(t *testing.T) {
	img := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512}, "")
	for _, err := range []error{
		img.WriteSector(0, false, make([]byte, 512), SectorStatusDumped),
		img.WriteSectorLong(0, false, make([]byte, 512), SectorStatusDumped),
		img.WriteSectorTag(0, false, SectorTagAppleSony, make([]byte, 12)),
	} {
		if !errors.Is(err, ErrUserDataNotPresent) {
			t.Errorf("flux-only: %v", err)
		}
	}
	if err := img.WriteMediaTag(MediaTagCdToc, []byte{1}); err != nil {
		t.Errorf("media tag on flux-only image: %v", err)
	}
}

func TestMediaTagWriteOrder(t *testing.T) {
	img := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: 4}, "")
	for _, tag := range []MediaTagType{7, 3, 9, 3, 1} {
		if err := img.WriteMediaTag(tag, []byte{byte(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	// Tags loaded from an image come first, in index order.
	img.mediaTags[MediaTagCdToc] = []byte{0}
	img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDataBlock,
		DataType: mediaTagToDataType[MediaTagCdToc]})
	if got, want := img.mediaTagWriteOrder(), []MediaTagType{MediaTagCdToc, 7, 9, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if err := img.WriteMediaTag(5, nil); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("empty media tag: %v", err)
	}
}

func TestCDHeaderMatchesLBA(t *testing.T) {
	hdr := func(m, s, f byte) []byte {
		b := make([]byte, 16)
		b[12], b[13], b[14] = m, s, f
		return b
	}
	cases := []struct {
		b    []byte
		lba  int64
		want bool
	}{
		{hdr(0x00, 0x02, 0x00), 0, true},
		{hdr(0x00, 0x00, 0x00), -150, true},
		{hdr(0x00, 0x01, 0x74), -1, true},
		{hdr(0x00, 0x10, 0x00), 600, true},
		{hdr(0x00, 0x0A, 0x00), 600, false},  // non-BCD alias of 10
		{hdr(0x00, 0x60, 0x00), 4350, false}, // second 60
		{hdr(0x00, 0x02, 0x75), 75, false},   // frame 75
		{hdr(0x79, 0x59, 0x74), 79*4500 + 59*75 + 74 - 150, true},
		{hdr(0x00, 0x02, 0x01), 0, false},
	}
	for _, c := range cases {
		if got := cdHeaderMatchesLBA(c.b, c.lba); got != c.want {
			t.Errorf("%x lba %d: %v", c.b[12:15], c.lba, got)
		}
	}
}

func TestMakeDDTEntry(t *testing.T) {
	img := &Image{}
	img.userDataDDTHeader.DataShift = 12
	img.userDataDDTHeader.BlockAlignmentShift = 9
	e, err := img.makeDDTEntry(5, 512*7)
	if err != nil || e != 5|7<<12 {
		t.Errorf("entry %#x, %v", e, err)
	}
	if e, _ := img.makeDDTEntry(4096+3, 0); e != 3 {
		t.Errorf("offset not masked: %#x", e)
	}
	if _, err := img.makeDDTEntry(0, 1<<57); !errors.Is(err, ErrCannotSetDDTEntry) {
		t.Errorf("overflow: %v", err)
	}
}

// Sector address bounds applied on write.
func TestWriteBounds(t *testing.T) {
	img := newTestImage(t, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: 10, NegativeSectors: 3,
		OverflowSectors: 2}, "compress=false")
	d := make([]byte, 512)
	for _, c := range []struct {
		addr uint64
		neg  bool
		ok   bool
	}{{0, false, true}, {11, false, true}, {12, false, false}, {3, true, true}, {4, true, false}} {
		err := img.WriteSector(c.addr, c.neg, d, SectorStatusDumped)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrSectorOutOfBounds)) {
			t.Errorf("sector %d negative=%v: %v", c.addr, c.neg, err)
		}
	}
	if err := img.WriteSector(0, false, make([]byte, 65536), SectorStatusDumped); !errors.Is(err, ErrInvalidSectorLength) {
		t.Errorf("65536-byte sector: %v", err)
	}
}
