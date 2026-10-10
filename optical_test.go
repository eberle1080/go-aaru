package aaru

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/crc64"

	"github.com/eberle1080/go-aaru/media"
)

// rdScratchImage returns a writable, empty Image backed by a temporary file
// (enough for the block writers; not a valid image).
func rdScratchImage(t *testing.T) (*Image, *os.File) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "scratch.aif"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return &Image{r: f, w: f, size: 0}, f
}

// opticalTestTracks is a CD layout with data, audio and a lead-out entry.
var opticalTestTracks = []TrackEntry{
	{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 119, Pregap: 150, Session: 1, Flags: 4},
	{Sequence: 2, Type: TrackTypeCdMode2Formless, Start: 120, End: 239, Session: 1, Flags: 4},
	{Sequence: 3, Type: TrackTypeAudio, Start: 240, End: 309, Session: 2, ISRC: [13]byte{'U', 'S', 'X'}},
}

// TestTracksBlockLayout writes a tracks block with SetTracks +
// writeTracksBlock and checks its layout (TRKS header with the entry count
// and the CRC64 of the packed entries, then the entries), that it reads back,
// and that an image written by go-aaru stores the same block.
func TestTracksBlockLayout(t *testing.T) {
	tracks := opticalTestTracks
	w, f := rdScratchImage(t)
	if err := w.SetTracks(tracks); err != nil {
		t.Fatal(err)
	}
	if err := w.writeTracksBlock(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	body := marshal(tracks)
	if len(got) != tracksHeaderSize+len(body) || len(body) != trackEntrySize*len(tracks) {
		t.Fatalf("tracks block of %d bytes, want %d", len(got), tracksHeaderSize+len(body))
	}
	h, _ := unmarshal[TracksHeader](got)
	if h != (TracksHeader{Identifier: BlockTypeTracksBlock, Entries: uint16(len(tracks)), CRC64: crc64.Checksum(body)}) {
		t.Errorf("tracks header = %+v", h)
	}
	if !bytes.Equal(got[tracksHeaderSize:], body) {
		t.Errorf("track entries = % x, want % x", got[tracksHeaderSize:], body)
	}
	if len(w.indexEntries) != 1 || w.indexEntries[0] != (IndexEntry{BlockType: BlockTypeTracksBlock, Offset: 0}) {
		t.Errorf("index entries = %v", w.indexEntries)
	}

	// The written block reads back.
	r := &Image{r: f, size: int64(len(got)), header: Header{ImageMajorVersion: FormatVersionV2}}
	r.processTracksBlock(IndexEntry{BlockType: BlockTypeTracksBlock, Offset: 0})
	back, err := r.Tracks()
	if err != nil || fmt.Sprint(back) != fmt.Sprint(tracks) || !r.info.HasPartitions || len(r.dataTracks) != 3 {
		t.Fatalf("read back %v, %v (data tracks %d)", back, err, len(r.dataTracks))
	}

	// A complete image stores the same block.
	path := filepath.Join(t.TempDir(), "cd.aif")
	p := app()
	p.MediaType = media.CDROMXA
	p.SectorSize = 2352
	p.UserSectors = 310
	img, err := Create(path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks(tracks); err != nil {
		t.Fatal(err)
	}
	if err := img.WriteSector(0, false, make([]byte, 2048), SectorStatusDumped); err != nil {
		t.Fatal(err)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	var trksOff uint64
	for _, e := range img.indexEntries {
		if e.BlockType == BlockTypeTracksBlock {
			trksOff = e.Offset
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if trksOff == 0 || !bytes.Equal(raw[trksOff:trksOff+uint64(len(got))], got) {
		t.Error("the image's tracks block differs from the one writeTracksBlock writes")
	}
	if back, err := img.Tracks(); err != nil || fmt.Sprint(back) != fmt.Sprint(tracks) {
		t.Errorf("image tracks = %v, %v", back, err)
	}
}

func TestSetTracks(t *testing.T) {
	img, _ := rdScratchImage(t)
	img.header.MediaType = uint32(media.CD)
	img.info.MetadataMediaType = media.OpticalDisc

	if _, err := img.Tracks(); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("Tracks on an image without tracks = %v, want ErrTrackNotFound", err)
	}
	tracks := []TrackEntry{
		{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 99, Session: 1, Flags: 4},
		{Sequence: 2, Type: TrackTypeAudio, Start: 100, End: 199, Session: 1, ISRC: [13]byte{'A', 'B'}},
		{Sequence: 0xAA, Type: TrackTypeAudio, Start: 200, End: 209, Session: 1},
	}
	if err := img.SetTracks(tracks); err != nil {
		t.Fatal(err)
	}
	tracks[0].Flags = 99 // SetTracks copies
	got, err := img.Tracks()
	if err != nil || len(got) != 3 || got[0].Flags != 4 {
		t.Fatalf("Tracks = %v, %v", got, err)
	}
	got[1].Flags = 77 // Tracks returns a copy
	if img.tracks[1].Flags == 77 {
		t.Error("Tracks returned the internal slice")
	}
	if !img.info.HasPartitions || !img.info.HasSessions || !img.dirty.tracksBlock {
		t.Error("SetTracks did not mark partitions/sessions/dirty")
	}
	if len(img.dataTracks) != 2 {
		t.Errorf("data tracks = %d, want 2 (sequence 0xAA excluded)", len(img.dataTracks))
	}

	// Track flags and ISRC sector tags come from all tracks.
	img.noUserDataDDT = false
	img.info.Sectors = 210
	if flags, err := img.ReadSectorTag(205, false, SectorTagCdTrackFlags); err != nil || flags[0] != 0 {
		t.Errorf("track flags of lead-out sector = %v, %v", flags, err)
	}
	if isrc, err := img.ReadSectorTag(150, false, SectorTagCdTrackIsrc); err != nil || string(isrc[:2]) != "AB" || len(isrc) != 12 {
		t.Errorf("ISRC = %q, %v", isrc, err)
	}

	if err := img.SetTracks(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := img.Tracks(); !errors.Is(err, ErrTrackNotFound) || img.dataTracks != nil {
		t.Errorf("tracks not cleared: %v", err)
	}
	if err := img.SetTracks(make([]TrackEntry, 0x10000)); !errors.Is(err, ErrInvalidTrackFormat) {
		t.Errorf("SetTracks with 65536 tracks = %v, want ErrInvalidTrackFormat", err)
	}
	if err := img.writeTracksBlock(); err != nil { // nothing to write
		t.Errorf("writeTracksBlock without tracks = %v", err)
	}
}

// blockTestImage writes a block media image of n 512-byte sectors.
func blockTestImage(t *testing.T, mt media.Type, n uint64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "block.aif")
	p := app()
	p.MediaType = mt
	p.SectorSize = 512
	p.UserSectors = n
	img, err := Create(path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range seededSectors(n, int(n), 512, 3) {
		if err := img.WriteSector(uint64(i), false, s, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadTrackSectorWrongMedia(t *testing.T) {
	img, err := Open(blockTestImage(t, media.Floptical, 100))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if _, _, err := img.ReadTrackSector(0, 1); !errors.Is(err, ErrIncorrectMediaType) {
		t.Errorf("ReadTrackSector on block media = %v, want ErrIncorrectMediaType", err)
	}
	if _, err := img.Tracks(); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("Tracks = %v, want ErrTrackNotFound", err)
	}
}

// TestDefaultGeometry: block media without a geometry block get a 16 heads,
// 63 sectors per track geometry.
func TestDefaultGeometry(t *testing.T) {
	img, err := Open(blockTestImage(t, media.GENERIC_HDD, 1200))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.geometry.Identifier == BlockTypeGeometryBlock {
		t.Fatal("image has a geometry block")
	}
	if img.cylinders != 1200/16/63 || img.heads != 16 || img.sectorsPerTrack != 63 {
		t.Errorf("geometry = %d/%d/%d, want %d/16/63", img.cylinders, img.heads, img.sectorsPerTrack, 1200/16/63)
	}
}
