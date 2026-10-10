package aaru

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/cdecc"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// ---------------------------------------------------------------------------
// Erasure coding round trips

// ecSource describes the sectors an erasure coded test image is written
// from. Long sources hold raw 2352-byte CD sectors written with
// WriteSectorLong; the others hold user data written with WriteSector.
type ecSource struct {
	mediaType  media.Type
	sectorSize uint32
	tracks     []TrackEntry
	long       bool
	sectors    [][]byte
	statuses   []SectorStatus
}

// userData returns the user data of sector i as ReadSector returns it.
func (s *ecSource) userData(i int) []byte {
	if s.long {
		return s.sectors[i][16 : 16+2048]
	}
	return s.sectors[i]
}

// ecFloppySource is a 3.5" HD floppy (2880 sectors of 512 bytes) of
// deterministic data: zero sectors, text-like sectors and random sectors.
func ecFloppySource() *ecSource {
	r := rand.New(rand.NewPCG(0xF1077, 0x9))
	s := &ecSource{mediaType: media.DOS_35_HD, sectorSize: 512}
	for i := range 2880 {
		d := make([]byte, 512)
		switch i % 7 {
		case 0, 1: // zero-filled
		case 2, 3, 4:
			v := byte('a')
			for j := range d {
				if r.IntN(8) == 0 {
					v = byte(r.IntN(16)) + 'a'
				}
				d[j] = v
			}
		default:
			for j := range d {
				d[j] = byte(r.Uint32())
			}
		}
		s.sectors = append(s.sectors, d)
		s.statuses = append(s.statuses, SectorStatusDumped)
	}
	return s
}

// ecMode1Sector builds a raw CD Mode 1 sector at lba with a valid prefix,
// EDC and ECC around user.
func ecMode1Sector(lba int64, user []byte) []byte {
	s := make([]byte, cdecc.SectorSize)
	copy(s[16:16+2048], user)
	_ = cdecc.ReconstructPrefix(s, cdecc.TrackTypeCdMode1, lba)
	_ = cdecc.Reconstruct(s, cdecc.TrackTypeCdMode1)
	return s
}

// ecCDSource is a CD-ROM with one Mode 1 data track of n raw sectors. Every
// 50th sector has a damaged ECC (stored as an errored sector, so its suffix
// is kept verbatim); the others have a regenerable prefix and suffix.
func ecCDSource(n int) *ecSource {
	r := rand.New(rand.NewPCG(0xCD, 0x1))
	s := &ecSource{mediaType: media.CDROM, sectorSize: 2048, long: true,
		tracks: []TrackEntry{{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: int64(n - 1), Session: 1, Flags: 4}}}
	for i := range n {
		user := make([]byte, 2048)
		for j := range user {
			if j%4 == 0 {
				user[j] = byte(r.Uint32())
			} else {
				user[j] = byte(j / 64)
			}
		}
		sec := ecMode1Sector(int64(i), user)
		st := SectorStatusDumped
		if i%50 == 25 {
			sec[2100] ^= 0x5A
			st = SectorStatusErrored
		}
		s.sectors = append(s.sectors, sec)
		s.statuses = append(s.statuses, st)
	}
	return s
}

// ecCreateImage writes src as an LZMA compressed, erasure coded image at
// dst and returns the CRC64 of the user data of all its sectors.
func ecCreateImage(t *testing.T, src *ecSource, dst string, k, m uint16) uint64 {
	t.Helper()
	want := crc64.New()
	opts := ParseOptions("compress=true;deduplicate=false")
	out, err := Create(dst, CreateParams{MediaType: src.mediaType, SectorSize: src.sectorSize,
		UserSectors: uint64(len(src.sectors)), Application: "go-aaru-test", ApplicationMinorVersion: 1}, &opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := out.SetErasureCoding(ErasureCodingRsVandermonde, k, m); err != nil {
		t.Fatalf("SetErasureCoding: %v", err)
	}
	if src.tracks != nil {
		if err := out.SetTracks(src.tracks); err != nil {
			t.Fatalf("SetTracks: %v", err)
		}
	}
	for i, d := range src.sectors {
		if src.long {
			err = out.WriteSectorLong(uint64(i), false, d, src.statuses[i])
		} else {
			err = out.WriteSector(uint64(i), false, d, src.statuses[i])
		}
		if err != nil {
			t.Fatalf("writing sector %d: %v", i, err)
		}
		want.Write(src.userData(i))
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return want.Sum64()
}

// ecImageCRC reads every sector of the image.
func ecImageCRC(t *testing.T, path string) (uint64, bool) {
	t.Helper()
	img, err := Open(path)
	if err != nil {
		t.Logf("Open: %v", err)
		return 0, false
	}
	defer img.Close()
	h := crc64.New()
	for i := range img.Info().Sectors {
		d, _, err := img.ReadSector(i, false)
		if err != nil {
			t.Logf("ReadSector(%d): %v", i, err)
			return 0, false
		}
		h.Write(d)
	}
	return h.Sum64(), true
}

// ecCheckLong reads every long sector of the image at path and compares it
// with the raw sectors of src.
func ecCheckLong(t *testing.T, path string, src *ecSource) {
	t.Helper()
	img, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer img.Close()
	bad := 0
	for i, want := range src.sectors {
		// Sectors with a correct prefix and suffix are recorded as such.
		wantSt := src.statuses[i]
		if wantSt == SectorStatusDumped {
			wantSt = SectorStatusMode1Correct
		}
		got, st, err := img.ReadSectorLong(uint64(i), false)
		if err != nil || st != wantSt || !bytes.Equal(got, want) {
			if bad++; bad <= 5 {
				t.Errorf("long sector %d: st=%v err=%v equal=%v, want st=%v", i, st, err, bytes.Equal(got, want), wantSt)
			}
		}
	}
	if bad > 5 {
		t.Errorf("%d long sectors differ", bad)
	}
}

// ecCorruptFile overwrites n bytes at off with 0xDE ^ i.
func ecCorruptFile(t *testing.T, path string, off int64, n int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := make([]byte, n)
	for i := range b {
		b[i] = 0xDE ^ byte(i)
	}
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
}

func ecCopyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ecFindBlock returns the offset of the first block header after the 128
// byte header with identifier DBLK and data type dt.
func ecFindBlock(t *testing.T, path string, dt DataType) int64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for pos := 128; pos+6 <= len(b); pos++ {
		if BlockType(binary.LittleEndian.Uint32(b[pos:])) == BlockTypeDataBlock &&
			DataType(binary.LittleEndian.Uint16(b[pos+4:])) == dt {
			return int64(pos)
		}
	}
	return -1
}

func ecTestPaths(t *testing.T) (string, string) {
	dir := t.TempDir()
	return filepath.Join(dir, "ec_test_output.aif"), filepath.Join(dir, "ec_test_corrupt.aif")
}

func TestErasureCodingCreateAndVerifyUncorrupted(t *testing.T) {
	out, _ := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 2)
	crc, ok := ecImageCRC(t, out)
	if !ok {
		t.Fatal("failed to read all sectors from EC image")
	}
	if crc != want {
		t.Errorf("CRC mismatch on uncorrupted EC image")
	}
	img, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.Header().FeatureCompatibleRO&FeatureROCompatErasure == 0 {
		t.Error("erasure coding feature bit not set")
	}
	if !img.ec.recoveryAvailable || img.ec.data == nil || img.ec.data.k != 4 || img.ec.data.m != 2 {
		t.Errorf("recovery data not loaded: %+v", img.ec)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestErasureCodingRecoverCorruptedDataBlock(t *testing.T) {
	out, bad := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 2)
	ecCopyFile(t, out, bad)
	// The first block is at 512: damage its compressed payload.
	if off := ecFindBlock(t, bad, DataTypeUserData); off != 512 {
		t.Fatalf("first user data block at %d, want 512", off)
	}
	ecCorruptFile(t, bad, 550, 128)
	crc, ok := ecImageCRC(t, bad)
	if !ok {
		t.Fatal("EC recovery failed: could not read all sectors from the corrupted image")
	}
	if crc != want {
		t.Error("CRC mismatch after EC recovery")
	}
}

func TestErasureCodingRecoverCorruptedBlockHeader(t *testing.T) {
	out, bad := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 2)
	ecCopyFile(t, out, bad)
	ecCorruptFile(t, bad, 512, 32)
	crc, ok := ecImageCRC(t, bad)
	if !ok {
		t.Fatal("EC recovery failed on corrupted BlockHeader")
	}
	if crc != want {
		t.Error("CRC mismatch after EC recovery")
	}
}

func TestErasureCodingParityCorruptionDoesNotAffectReads(t *testing.T) {
	out, bad := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 1)
	ecCopyFile(t, out, bad)
	pos := ecFindBlock(t, bad, DataTypeErasureParity)
	if pos < 0 {
		t.Fatal("no parity block found in image")
	}
	f, err := os.OpenFile(bad, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{0xFF}, 32), pos+40); err != nil {
		t.Fatal(err)
	}
	f.Close()
	crc, ok := ecImageCRC(t, bad)
	if !ok {
		t.Fatal("could not read all sectors")
	}
	if crc != want {
		t.Error("CRC mismatch")
	}
}

func TestErasureCodingRecoverWithXORM1(t *testing.T) {
	out, bad := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 1)
	ecCopyFile(t, out, bad)
	off := ecFindBlock(t, bad, DataTypeUserData)
	if off <= 0 {
		t.Fatal("no data block found")
	}
	ecCorruptFile(t, bad, off+40, 64)
	crc, ok := ecImageCRC(t, bad)
	if !ok {
		t.Fatal("XOR recovery failed")
	}
	if crc != want {
		t.Error("CRC mismatch")
	}
}

func TestErasureCodingBackupHeaderRecovery(t *testing.T) {
	out, bad := ecTestPaths(t)
	want := ecCreateImage(t, ecFloppySource(), out, 4, 2)
	ecCopyFile(t, out, bad)
	ecCorruptFile(t, bad, 0, 64)
	crc, ok := ecImageCRC(t, bad)
	if !ok {
		t.Fatal("backup header recovery failed: could not open/read the corrupted image")
	}
	if crc != want {
		t.Error("CRC mismatch after backup header recovery")
	}
	img, err := Open(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if h := img.Header(); h.Identifier != AaruMagic || h.MediaType != uint32(media.DOS_35_HD) {
		t.Errorf("recovered header: identifier %#x media type %d", h.Identifier, h.MediaType)
	}
}

// TestErasureCodingCDSectorSizeImage writes raw Mode 1 sectors to an erasure
// coded CD image: the user data, and the long sectors rebuilt from the
// regenerated or stored prefixes and suffixes, read back unchanged, also
// after damaging a user data block.
func TestErasureCodingCDSectorSizeImage(t *testing.T) {
	out, bad := ecTestPaths(t)
	src := ecCDSource(600)
	want := ecCreateImage(t, src, out, 4, 2)
	crc, ok := ecImageCRC(t, out)
	if !ok {
		t.Fatal("failed to read all sectors from CD EC image")
	}
	if crc != want {
		t.Error("CRC mismatch on CD EC image")
	}
	ecCheckLong(t, out, src)

	ecCopyFile(t, out, bad)
	off := ecFindBlock(t, bad, DataTypeUserData)
	if off <= 0 {
		t.Fatal("no data block found")
	}
	ecCorruptFile(t, bad, off+60, 100)
	if crc, ok := ecImageCRC(t, bad); !ok || crc != want {
		t.Errorf("damaged CD EC image: read ok %v, CRC match %v", ok, crc == want)
	}
	ecCheckLong(t, bad, src)
}

// ---------------------------------------------------------------------------
// Tracks

func TestErasureCodingRecoverCorruptedTracksBlockOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ec_tracks_output.aif")
	opts := ParseOptions("deduplicate=false;compress=false")
	img, err := Create(path, CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 64,
		Application: "go-aaru-test", ApplicationMinorVersion: 1}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); err != nil {
		t.Fatal(err)
	}
	track := TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 63, Session: 1, Flags: 0x04}
	if err := img.SetTracks([]TrackEntry{track}); err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		if err := img.WriteSector(uint64(i), false, bytes.Repeat([]byte{byte(i)}, 2048), SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var off uint64
	for _, e := range img.indexEntries {
		if e.BlockType == BlockTypeTracksBlock {
			off = e.Offset
		}
	}
	img.Close()
	if off == 0 {
		t.Fatal("no tracks block")
	}

	// Damage a track entry, so its CRC fails.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[off+tracksHeaderSize+2] ^= 0xFF
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	tracks, err := img.Tracks()
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(tracks) != 1 || tracks[0] != track {
		t.Errorf("tracks = %+v, want %+v (the tracks must be recovered on open)", tracks, track)
	}
}

// ---------------------------------------------------------------------------
// Stored LZMA image

// ecStoredImage is an erasure coded block image written by ecStoredImageBytes,
// with what was written to it.
type ecStoredImage struct {
	raw      []byte
	sectors  [][]byte // nil: not dumped
	statuses []SectorStatus
	tag      []byte // MediaTagAtaIdentify
}

// ecStoredImageBytes writes an LZMA compressed, deduplicated image of 400
// 512-byte sectors in 32-sector blocks with RS(4,2) erasure coding and a
// media tag. Some sectors are duplicates, errored or not dumped.
func ecStoredImageBytes(t *testing.T) *ecStoredImage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ec_stored.aif")
	o := ParseOptions("compress=true;deduplicate=true;data_shift=5")
	img, err := Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 400,
		Application: "go-aaru-test", ApplicationMajorVersion: 3, ApplicationMinorVersion: 7}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); err != nil {
		t.Fatal(err)
	}
	s := &ecStoredImage{sectors: make([][]byte, 400), statuses: make([]SectorStatus, 400),
		tag: ecPattern(512, 0xA7A)}
	if err := img.WriteMediaTag(MediaTagAtaIdentify, s.tag); err != nil {
		t.Fatal(err)
	}
	for i := range 400 {
		s.statuses[i] = SectorStatusNotDumped
		if i%61 == 17 {
			continue // not dumped
		}
		d, st := ecPattern(512, uint64(i)+500), SectorStatusDumped
		if i%29 == 4 {
			d = ecPattern(512, 4) // duplicate
		}
		if i%43 == 9 {
			st = SectorStatusErrored
		}
		if err := img.WriteSector(uint64(i), false, d, st); err != nil {
			t.Fatal(err)
		}
		s.sectors[i], s.statuses[i] = d, st
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	if s.raw, err = os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	return s
}

// check reads every sector and long sector of img and the media tag, and
// compares them with what was written.
func (s *ecStoredImage) check(t *testing.T, img *Image) {
	t.Helper()
	bad := 0
	for i, want := range s.sectors {
		got, st, err := img.ReadSector(uint64(i), false)
		ok := st == s.statuses[i] && bytes.Equal(got, want)
		if want == nil {
			ok = ok && errors.Is(err, ErrSectorNotDumped)
		} else {
			ok = ok && err == nil
		}
		if _, _, lerr := img.ReadSectorLong(uint64(i), false); !errors.Is(lerr, ErrIncorrectMediaType) {
			ok = false
		}
		if !ok {
			if bad++; bad <= 5 {
				t.Errorf("sector %d: st=%v err=%v equal=%v, want st=%v", i, st, err, bytes.Equal(got, want), s.statuses[i])
			}
		}
	}
	if bad > 5 {
		t.Errorf("%d sectors differ", bad)
	}
	if _, _, err := img.ReadSector(400, false); !errors.Is(err, ErrSectorOutOfBounds) {
		t.Errorf("ReadSector(400) = %v, want ErrSectorOutOfBounds", err)
	}
	if tag, err := img.ReadMediaTag(MediaTagAtaIdentify); err != nil || !bytes.Equal(tag, s.tag) {
		t.Errorf("media tag: %v", err)
	}
}

// TestErasureCodingStoredImageDamage damages an erasure coded image (LZMA,
// deduplication, RS(4,2), 32-sector blocks) in various ways: go-aaru must
// read it like the undamaged image.
func TestErasureCodingStoredImageDamage(t *testing.T) {
	s := ecStoredImageBytes(t)
	orig := s.raw
	clean, err := OpenReader(bytes.NewReader(orig), int64(len(orig)))
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Close()
	if !clean.ec.recoveryAvailable {
		t.Fatal("no recovery data loaded from the image")
	}
	s.check(t, clean)
	if err := clean.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	var blocks []uint64
	for _, e := range clean.indexEntries {
		if e.BlockType == BlockTypeDataBlock && e.DataType == DataTypeUserData {
			blocks = append(blocks, e.Offset)
		}
	}
	if len(blocks) < 8 {
		t.Fatalf("only %d data blocks", len(blocks))
	}
	ecmb := binary.LittleEndian.Uint64(orig[len(orig)-recoveryFooterSize:])
	stripeOf := func(off uint64) ecShardRef { return clean.ec.data.lookup[off] }

	// Two blocks of the same stripe.
	a := blocks[4]
	var b2 uint64
	for _, o := range blocks {
		if o != a && stripeOf(o).stripe == stripeOf(a).stripe {
			b2 = o
			break
		}
	}
	if b2 == 0 {
		t.Fatal("no two blocks in the same stripe")
	}

	tests := []struct {
		name   string
		damage func(b []byte)
	}{
		{"payload", func(b []byte) { ecFlip(b, blocks[0]+60, 200) }},
		{"header", func(b []byte) { ecFlip(b, blocks[1], blockHeaderSize) }},
		{"two-in-stripe", func(b []byte) { ecFlip(b, a+100, 50); ecFlip(b, b2+4, 4) }},
		{"every-stripe", func(b []byte) {
			done := map[uint32]bool{}
			for _, o := range blocks {
				if s := stripeOf(o).stripe; !done[s] {
					done[s] = true
					ecFlip(b, o+80, 300)
				}
			}
		}},
		{"primary-ecmb", func(b []byte) { ecFlip(b, blocks[0]+60, 200); ecFlip(b, ecmb+50, 10) }},
		{"primary-header-and-ecmb", func(b []byte) {
			ecFlip(b, 0, 64)
			ecFlip(b, ecmb, 4)
			ecFlip(b, blocks[2]+36, 10)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(orig)
			tc.damage(b)
			img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			s.check(t, img)
		})
	}

	// Beyond M damaged blocks in a stripe the block cannot be recovered.
	t.Run("too-many", func(t *testing.T) {
		b := bytes.Clone(orig)
		ref := stripeOf(blocks[0])
		st := clean.ec.data.stripes[ref.stripe]
		if len(st.entries) < 3 {
			t.Fatalf("stripe of %d blocks", len(st.entries))
		}
		for _, e := range st.entries[:3] {
			ecFlip(b, e.Offset+60, 100)
		}
		img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer img.Close()
		if _, _, err := img.ReadSector(0, false); !errors.Is(err, ErrCannotDecompressBlock) {
			t.Errorf("ReadSector(0) error = %v, want ErrCannotDecompressBlock", err)
		}
	})

	// Without either ECMB copy there is nothing to recover from, but the
	// image still opens and the undamaged sectors read.
	t.Run("no-ecmb", func(t *testing.T) {
		b := bytes.Clone(orig)
		ecFlip(b, ecmb, 4)
		ecmbLen := binary.LittleEndian.Uint64(orig[len(orig)-recoveryFooterSize+8:])
		ecFlip(b, uint64(len(b))-recoveryFooterSize-ecmbLen+45, 4)
		img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer img.Close()
		if img.ec.recoveryAvailable {
			t.Error("recovery available without a valid ECMB")
		}
		s.check(t, img)
	})
}

// ---------------------------------------------------------------------------
// Configuration

func TestSetErasureCodingValidation(t *testing.T) {
	newImg := func(t *testing.T) *Image {
		img, err := Create(filepath.Join(t.TempDir(), "x.aif"), CreateParams{MediaType: media.GENERIC_HDD,
			SectorSize: 512, UserSectors: 10}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { img.Close() })
		return img
	}
	bad := []struct {
		alg  ErasureCodingAlgorithm
		k, m uint16
	}{
		{ErasureCodingRsVandermonde, 0, 2},
		{ErasureCodingRsVandermonde, 4, 0},
		{ErasureCodingRsVandermonde, 250, 6},
		{ErasureCodingXor, 4, 2},
		{ErasureCodingAlgorithm(2), 4, 2},
	}
	for _, c := range bad {
		img := newImg(t)
		if err := img.SetErasureCoding(c.alg, c.k, c.m); !errors.Is(err, ErrIncorrectDataSize) {
			t.Errorf("SetErasureCoding(%v, %d, %d) = %v, want ErrIncorrectDataSize", c.alg, c.k, c.m, err)
		}
		if img.ec.enabled || img.header.FeatureCompatibleRO != 0 {
			t.Errorf("SetErasureCoding(%v, %d, %d) enabled erasure coding", c.alg, c.k, c.m)
		}
	}
	for _, c := range []struct {
		alg  ErasureCodingAlgorithm
		k, m uint16
	}{{ErasureCodingRsVandermonde, 1, 1}, {ErasureCodingRsVandermonde, 250, 5}, {ErasureCodingXor, 7, 1}} {
		img := newImg(t)
		if err := img.SetErasureCoding(c.alg, c.k, c.m); err != nil {
			t.Errorf("SetErasureCoding(%v, %d, %d) = %v", c.alg, c.k, c.m, err)
		}
		if img.header.FeatureCompatibleRO&FeatureROCompatErasure == 0 {
			t.Error("feature bit not set")
		}
	}

	stored := filepath.Join(t.TempDir(), "stored.aif")
	if err := os.WriteFile(stored, ecStoredImageBytes(t).raw, 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := Open(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only image: %v, want ErrReadOnly", err)
	}
	if err := img.SetErasureCodingAuto(10); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only image (auto): %v, want ErrReadOnly", err)
	}
	img.Close()
	if err := img.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); !errors.Is(err, ErrClosed) {
		t.Errorf("closed image: %v, want ErrClosed", err)
	}
}

// TestErasureCodingAutoParameters checks the table of the specification.
func TestErasureCodingAutoParameters(t *testing.T) {
	for _, c := range []struct {
		pct  uint8
		k, m uint16
	}{{1, 200, 2}, {5, 40, 2}, {10, 20, 2}, {15, 20, 3}, {25, 20, 5}, {50, 16, 8}, {100, 8, 8}, {33, 21, 7}, {2, 100, 2}} {
		k, m, err := ecAutoParameters(c.pct)
		if err != nil || k != c.k || m != c.m {
			t.Errorf("%d%%: K=%d M=%d err=%v, want K=%d M=%d", c.pct, k, m, err, c.k, c.m)
		}
	}
	for p := 1; p <= 100; p++ {
		k, m, err := ecAutoParameters(uint8(p))
		if err != nil || k == 0 || m < 2 || m > 8 || int(k)+int(m) > 255 {
			t.Errorf("%d%%: K=%d M=%d err=%v", p, k, m, err)
		}
	}
	for _, p := range []uint8{0, 101, 255} {
		if _, _, err := ecAutoParameters(p); !errors.Is(err, ErrIncorrectDataSize) {
			t.Errorf("%d%%: %v, want ErrIncorrectDataSize", p, err)
		}
	}
	img, err := Create(filepath.Join(t.TempDir(), "x.aif"), CreateParams{MediaType: media.GENERIC_HDD,
		SectorSize: 512, UserSectors: 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if err := img.SetErasureCodingAuto(0); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("SetErasureCodingAuto(0) = %v", err)
	}
	if err := img.SetErasureCodingAuto(25); err != nil || img.ec.k != 20 || img.ec.m != 5 ||
		img.ec.algorithm != ErasureCodingRsVandermonde {
		t.Errorf("SetErasureCodingAuto(25) = %v, K=%d M=%d", err, img.ec.k, img.ec.m)
	}
}

// ---------------------------------------------------------------------------
// Write path details

// ecWriteImage writes an erasure coded block image with the given options.
func ecWriteImage(t *testing.T, path, opts string, sectors int, k, m uint16) [][]byte {
	t.Helper()
	o := ParseOptions(opts)
	img, err := Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512,
		UserSectors: uint64(sectors), Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetErasureCoding(ErasureCodingRsVandermonde, k, m); err != nil {
		t.Fatal(err)
	}
	data := make([][]byte, sectors)
	for i := range data {
		data[i] = ecPattern(512, uint64(i))
		if err := img.WriteSector(uint64(i), false, data[i], SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return data
}

// TestErasureCodingLayout checks the structures written at the end of the
// file: ECMB, duplicate ECMB and recovery footer.
func TestErasureCodingLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.aif")
	ecWriteImage(t, path, "compress=false;deduplicate=false;data_shift=4", 150, 4, 2)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	footer, err := unmarshal[RecoveryFooter](b[len(b)-recoveryFooterSize:])
	if err != nil || footer.FooterMagic != RecoveryFooterMagic {
		t.Fatalf("footer: %+v %v", footer, err)
	}
	hdr, _ := unmarshal[Header](b)
	if footer.BackupHeader != hdr || footer.HeaderCRC64 != crc64.Checksum(b[:headerSize]) {
		t.Error("backup header does not match the header")
	}
	if hdr.FeatureCompatibleRO != FeatureROCompatErasure {
		t.Errorf("featureCompatibleRo = %#x", hdr.FeatureCompatibleRO)
	}
	if footer.ECMBOffset%512 != 0 {
		t.Errorf("ECMB at %d not aligned", footer.ECMBOffset)
	}
	primary := b[footer.ECMBOffset : footer.ECMBOffset+footer.ECMBLength]
	dup := alignUp(footer.ECMBOffset+footer.ECMBLength, 9)
	if dup+footer.ECMBLength != uint64(len(b))-recoveryFooterSize {
		t.Errorf("duplicate ECMB at %d does not end at the footer", dup)
	}
	if !bytes.Equal(primary, b[dup:dup+footer.ECMBLength]) {
		t.Error("duplicate ECMB differs")
	}
	mh, _ := unmarshal[ErasureCodingMapHeader](primary)
	if mh.Identifier != BlockTypeErasureCodingMapBlock || mh.Algorithm != ErasureCodingRsVandermonde ||
		mh.Compression != CompressionNone || mh.Length != mh.CmpLength || mh.CRC64 != mh.CmpCRC64 ||
		mh.Length+ecMapHeaderSize != footer.ECMBLength || crc64.Checksum(primary[ecMapHeaderSize:]) != mh.CRC64 {
		t.Errorf("ECMB header %+v", mh)
	}
	groups := parseECMBPayload(primary[ecMapHeaderSize:], int(mh.StripeGroupCount))
	if len(groups) != int(mh.StripeGroupCount) {
		t.Fatalf("%d groups parsed, header says %d", len(groups), mh.StripeGroupCount)
	}
	// 150 sectors in 16-sector blocks: 10 blocks, stripes of 4 -> 3 stripes.
	d := groups[0]
	if d.desc.GroupType != ECGroupData || d.desc.K != 4 || d.desc.M != 2 || d.desc.StripeCount != 3 ||
		d.desc.InterleaveDepth != 4 || len(d.group.stripes[2].entries) != 2 {
		t.Errorf("data group %+v", d.desc)
	}
	types := []ErasureCodingGroupType{}
	for _, g := range groups {
		types = append(types, g.desc.GroupType)
	}
	if want := []ErasureCodingGroupType{ECGroupData, ECGroupDdtPrimary, ECGroupIndex}; !equalSlices(types, want) {
		t.Errorf("groups %v, want %v", types, want)
	}

	// Every parity block is a valid uncompressed data block of the stripe's
	// biggest block size, and every data shard CRC matches.
	for _, g := range groups {
		for _, st := range g.group.stripes {
			var maxSize uint32
			for _, e := range st.entries {
				maxSize = max(maxSize, e.OnDiskSize)
				var crc uint64
				if g.desc.GroupType == ECGroupData {
					crc = crc64.Checksum(b[e.Offset : e.Offset+uint64(e.OnDiskSize)])
				} else {
					shard := make([]byte, g.desc.ShardSize)
					copy(shard, b[e.Offset:e.Offset+uint64(e.OnDiskSize)])
					crc = crc64.Checksum(shard)
				}
				if crc != e.ShardCRC64 {
					t.Errorf("group %v: shard CRC of block at %d", g.desc.GroupType, e.Offset)
				}
			}
			for _, po := range st.parity {
				ph, _ := decodeBlockHeader(b[po:])
				wantType := map[ErasureCodingGroupType]DataType{ECGroupData: DataTypeErasureParity,
					ECGroupDdtPrimary: DataTypeErasureParityDdtPrimary, ECGroupIndex: DataTypeErasureParityIndex}
				if ph.Identifier != BlockTypeDataBlock || ph.Type != wantType[g.desc.GroupType] ||
					ph.Compression != CompressionNone || ph.Length != maxSize || ph.CmpLength != maxSize ||
					crc64.Checksum(b[po+blockHeaderSize:po+blockHeaderSize+uint64(maxSize)]) != ph.CRC64 {
					t.Errorf("group %v: parity block at %d: %+v", g.desc.GroupType, po, ph)
				}
			}
		}
	}
}

func equalSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestErasureCodingRecoversEveryBlock damages, one at a time, every user
// data block, every parity block (plus a data block of its stripe), and the
// secondary DDTs of an uncompressed multi-level image.
func TestErasureCodingRecoversEveryBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.aif")
	data := ecWriteImage(t, path, "compress=false;deduplicate=false;data_shift=3;table_shift=5", 200, 3, 2)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := OpenReader(bytes.NewReader(orig), int64(len(orig)))
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Close()

	check := func(t *testing.T, b []byte) {
		t.Helper()
		img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer img.Close()
		for i, want := range data {
			got, st, err := img.ReadSector(uint64(i), false)
			if err != nil || st != SectorStatusDumped || !bytes.Equal(got, want) {
				t.Fatalf("sector %d: st=%v err=%v equal=%v", i, st, err, bytes.Equal(got, want))
			}
		}
	}
	for _, st := range clean.ec.data.stripes {
		for k, e := range st.entries {
			b := bytes.Clone(orig)
			ecFlip(b, e.Offset+blockHeaderSize+uint64(k)*7, 20) // uncompressed: caught by the block CRC
			check(t, b)
		}
		for _, po := range st.parity {
			b := bytes.Clone(orig)
			ecFlip(b, po+blockHeaderSize+10, 20)
			ecFlip(b, st.entries[0].Offset, 8)
			check(t, b)
		}
	}
	if clean.ec.ddt == nil || len(clean.ec.ddt.stripes) != 1 {
		t.Fatal("no secondary DDT group")
	}
	for _, e := range clean.ec.ddt.stripes[0].entries {
		b := bytes.Clone(orig)
		ecFlip(b, e.Offset+ddtHeader2Size+8, 16)
		check(t, b)
	}
}

// TestErasureCodingRecursionGuard checks that no recovery starts while one
// is in progress.
func TestErasureCodingRecursionGuard(t *testing.T) {
	raw := ecStoredImageBytes(t).raw
	img, err := OpenReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if !img.ecRecoveryAvailable() {
		t.Fatal("recovery not available")
	}
	off := img.ec.data.stripes[0].entries[0].Offset
	img.ec.recoveryInProgress = true
	if img.ecRecoveryAvailable() {
		t.Error("recovery available while in progress")
	}
	if _, err := img.ecRecoverDataBlock(off, 0, SectorStatusDumped); !errors.Is(err, ErrCannotDecompressBlock) {
		t.Errorf("ecRecoverDataBlock = %v", err)
	}
	if _, err := img.ecRecoverMetaBlock(off); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("ecRecoverMetaBlock = %v", err)
	}
	img.ec.recoveryInProgress = false
	if _, err := img.ecRecoverDataBlock(off, 0, SectorStatusDumped); err != nil {
		t.Errorf("ecRecoverDataBlock = %v", err)
	}
	if img.ec.recoveryInProgress {
		t.Error("guard left set")
	}
	// Blocks outside the groups are not recoverable.
	if _, err := img.ecRecoverDDTBlock(off); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("ecRecoverDDTBlock = %v", err)
	}
	if _, err := img.ecRecoverDataBlock(off+1, 0, SectorStatusDumped); !errors.Is(err, ErrCannotDecompressBlock) {
		t.Errorf("ecRecoverDataBlock(unknown) = %v", err)
	}
}

// TestErasureCodingNotEnabled checks that images without erasure coding have
// neither parity nor footer.
func TestErasureCodingNotEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 64}, &o)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		if err := img.WriteSector(uint64(i), false, ecPattern(512, uint64(i)), SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte("AVRECMFR")) || bytes.Contains(b, []byte("ECMB")) {
		t.Error("recovery structures written without erasure coding")
	}
	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.ecRecoveryAvailable() || img.header.FeatureCompatibleRO != 0 {
		t.Error("erasure coding reported on a plain image")
	}
}

// TestErasureCodingMalformedECMB feeds damaged and random ECMB payloads
// (with valid CRCs) to the loader and the recovery: nothing may panic and
// recovery must fail cleanly.
func TestErasureCodingMalformedECMB(t *testing.T) {
	orig := ecStoredImageBytes(t).raw
	footerOff := len(orig) - recoveryFooterSize
	ecmbOff := binary.LittleEndian.Uint64(orig[footerOff:])
	ecmbLen := binary.LittleEndian.Uint64(orig[footerOff+8:])
	payload := orig[ecmbOff+ecMapHeaderSize : ecmbOff+ecmbLen]

	// rebuild returns an image whose two ECMBs carry p.
	rebuild := func(p []byte) []byte {
		b := bytes.Clone(orig[:ecmbOff])
		mh, _ := unmarshal[ErasureCodingMapHeader](orig[ecmbOff:])
		mh.Length, mh.CmpLength = uint64(len(p)), uint64(len(p))
		mh.CRC64 = crc64.Checksum(p)
		mh.CmpCRC64 = mh.CRC64
		blk := append(marshal(&mh), p...)
		b = append(b, blk...)
		b = append(b, blk...)
		f, _ := unmarshal[RecoveryFooter](orig[footerOff:])
		f.ECMBLength = uint64(len(blk))
		return append(b, marshal(&f)...)
	}
	try := func(b []byte) {
		img, err := OpenReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return
		}
		defer img.Close()
		for _, g := range []*ecGroup{img.ec.data, img.ec.ddt, img.ec.meta} {
			if g == nil {
				continue
			}
			for off := range g.lookup {
				img.ecRecoverShard(g, off)
			}
		}
		for i := range uint64(40) {
			img.ReadSector(i, false)
		}
	}

	rng := rand.New(rand.NewPCG(1, 2))
	set16 := func(p []byte, off int, v uint16) []byte {
		p = bytes.Clone(p)
		if off+2 <= len(p) {
			binary.LittleEndian.PutUint16(p[off:], v)
		}
		return p
	}
	set32 := func(p []byte, off int, v uint32) []byte {
		p = bytes.Clone(p)
		if off+4 <= len(p) {
			binary.LittleEndian.PutUint32(p[off:], v)
		}
		return p
	}
	cases := [][]byte{
		set16(payload, 1, 0),     // K = 0
		set16(payload, 1, 300),   // K + M > 255
		set16(payload, 3, 0),     // M = 0
		set32(payload, 5, 1<<31), // huge shard size
		set32(payload, 5, 3),     // tiny shard size
		set32(payload, 9, 1<<31), // huge stripe count
		set16(payload, 15, 200),  // actualK > K
		set16(payload, 15, 0),    // empty stripe
		payload[:len(payload)/2], // truncated
		payload[:stripeGroupDescriptorSize],
	}
	for range 300 {
		p := bytes.Clone(payload)
		for range 1 + rng.IntN(8) {
			p[rng.IntN(len(p))] = byte(rng.Uint32())
		}
		cases = append(cases, p)
	}
	for range 50 {
		p := make([]byte, rng.IntN(200))
		for i := range p {
			p[i] = byte(rng.Uint32())
		}
		cases = append(cases, p)
	}
	for _, p := range cases {
		try(rebuild(p))
	}

	// Footer pointing anywhere.
	for range 100 {
		b := bytes.Clone(orig)
		binary.LittleEndian.PutUint64(b[footerOff:], rng.Uint64N(uint64(len(b))+100))
		binary.LittleEndian.PutUint64(b[footerOff+8:], rng.Uint64())
		try(b)
	}
}

// TestErasureCodingResumeWithoutEC resumes an erasure coded image without
// enabling erasure coding: the old recovery data is no longer at the end of
// the file and the image reads normally.
func TestErasureCodingResumeWithoutEC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.aif")
	data := ecWriteImage(t, path, "compress=true;deduplicate=false;data_shift=4", 100, 4, 2)
	img, err := Resume(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !img.ec.recoveryAvailable || img.ec.enabled {
		t.Errorf("resumed image: recovery %v, enabled %v", img.ec.recoveryAvailable, img.ec.enabled)
	}
	if err := img.WriteSector(5, false, data[7], SectorStatusDumped); err != nil {
		t.Fatal(err)
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	data[5] = data[7]
	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.ecRecoveryAvailable() {
		t.Error("recovery data found at the end of the resumed image")
	}
	for i, want := range data {
		if got, _, err := img.ReadSector(uint64(i), false); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("sector %d: %v", i, err)
		}
	}
}
