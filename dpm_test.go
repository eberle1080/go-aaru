package aaru

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
)

// Tests of storing the Data Position Measurement block on new
// images, keeping, replacing and removing it on resumed images, validating
// it, and verifying its CRC.

const dpmTestSectors = 64

// buildTestDPM builds a serialized DPM block with the given number of
// entries, one layer end and one calibration, angles scaled by seed
// (BuildDpm). It is built byte by byte, independently of DPM.MarshalBinary.
func buildTestDPM(entries uint32, seed uint64) []byte {
	le := binary.LittleEndian
	var payload []byte
	payload = le.AppendUint64(payload, 1000) // layer end
	for i := uint32(0); i < entries; i++ {
		payload = le.AppendUint64(payload, uint64(i)*50)
		payload = le.AppendUint64(payload, uint64(i)*seed)
		status := byte(DpmStatusMeasured)
		if i == 0 {
			status = byte(DpmStatusUnknown)
		}
		payload = append(payload, status)
	}
	payload = le.AppendUint64(payload, 0)
	payload = le.AppendUint64(payload, 7500000)
	payload = le.AppendUint64(payload, 9718000)

	var b []byte
	b = le.AppendUint32(b, uint32(BlockTypeDataPositionMeasurementBlock))
	b = le.AppendUint32(b, entries)
	b = le.AppendUint32(b, 1)      // calibrations
	b = le.AppendUint32(b, 50)     // nominal spacing
	b = le.AppendUint16(b, 1)      // timing unit
	b = le.AppendUint16(b, 0xFFFF) // speed
	b = append(b, 1, DPMFlagOppositeTrackPath)
	b = le.AppendUint16(b, 0)
	b = le.AppendUint64(b, uint64(len(payload)))
	b = le.AppendUint64(b, crc64.Checksum(payload))
	return append(b, payload...)
}

// createDPMTestImage creates a Mode 1 CD image with all its sectors written,
// leaving it open for writing (CreateImage).
func createDPMTestImage(t *testing.T, path string, erasureCoding bool) *Image {
	t.Helper()
	opts := ParseOptions("deduplicate=false;compress=false")
	img, err := Create(path, CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: dpmTestSectors,
		Application: "go-aaru-test", ApplicationMajorVersion: 0, ApplicationMinorVersion: 0}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if erasureCoding {
		if err := img.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); err != nil {
			img.Close()
			t.Fatal(err)
		}
	}
	if err := img.SetTracks([]TrackEntry{{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: dpmTestSectors - 1,
		Session: 1, Flags: 0x04}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < dpmTestSectors; i++ {
		if err := img.WriteSector(uint64(i), false, bytes.Repeat([]byte{byte(i)}, 2048), SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

// getTestDPM returns the DPM block of an image, nil if it has none (GetDpm).
func getTestDPM(img *Image) []byte {
	b, err := img.DPMRaw()
	if err != nil {
		return nil
	}
	return b
}

// countDPMIndexEntries counts the DPM index entries of an image and returns
// the offset of the last one (CountDpmIndexEntries).
func countDPMIndexEntries(img *Image) (int, uint64) {
	n, off := 0, uint64(0)
	for _, e := range img.indexEntries {
		if e.BlockType == BlockTypeDataPositionMeasurementBlock {
			n++
			off = e.Offset
		}
	}
	return n, off
}

func mustOpen(t *testing.T, path string) *Image {
	t.Helper()
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func mustResume(t *testing.T, path, opts string) *Image {
	t.Helper()
	o := ParseOptions(opts)
	img, err := Resume(path, &o)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func mustClose(t *testing.T, img *Image) {
	t.Helper()
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDPMRoundTripsOnNewImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_roundtrip.aif")
	written := buildTestDPM(100, 131700)

	img := createDPMTestImage(t, path, false)
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	if got := getTestDPM(img); !bytes.Equal(got, written) {
		t.Error("DPM must be readable while writing")
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if got := getTestDPM(img); !bytes.Equal(got, written) {
		t.Error("DPM differs after reopening")
	}
	if n, _ := countDPMIndexEntries(img); n != 1 {
		t.Errorf("%d DPM index entries, want 1", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestDPMImageWithoutDpmReportsNotPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_absent.aif")
	mustClose(t, createDPMTestImage(t, path, false))

	img := mustOpen(t, path)
	defer img.Close()
	b, err := img.DPMRaw()
	if !errors.Is(err, ErrMetadataNotPresent) || len(b) != 0 {
		t.Errorf("DPMRaw = %d bytes, %v; want ErrMetadataNotPresent", len(b), err)
	}
	if n, _ := countDPMIndexEntries(img); n != 0 {
		t.Errorf("%d DPM index entries, want 0", n)
	}
}

// SizeQuery: the API returns a right-sized slice.
func TestDPMSizeQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_size.aif")
	written := buildTestDPM(10, 131700)
	img := createDPMTestImage(t, path, false)
	defer img.Close()
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	b, err := img.DPMRaw()
	if err != nil || len(b) != len(written) {
		t.Errorf("DPMRaw = %d bytes, %v; want %d bytes", len(b), err, len(written))
	}
}

func TestDPMSetRejectsInvalidBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_invalid.aif")
	img := createDPMTestImage(t, path, false)
	defer img.Close()

	block := buildTestDPM(10, 131700)
	block[dpmHeaderSize+20]++
	if err := img.SetDPMRaw(block); !errors.Is(err, ErrInvalidBlockCRC) {
		t.Errorf("corrupted payload: %v, want ErrInvalidBlockCRC", err)
	}

	block = buildTestDPM(10, 131700)
	if err := img.SetDPMRaw(block[:len(block)-1]); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("truncated block: %v, want ErrIncorrectDataSize", err)
	}

	block = buildTestDPM(10, 131700)
	binary.LittleEndian.PutUint32(block[4:], binary.LittleEndian.Uint32(block[4:])+1) // entries++
	if err := img.SetDPMRaw(block); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("wrong entry count: %v, want ErrIncorrectDataSize", err)
	}

	block = buildTestDPM(10, 131700)
	binary.LittleEndian.PutUint32(block, uint32(BlockTypeDumpHardwareBlock))
	if err := img.SetDPMRaw(block); !errors.Is(err, ErrCannotReadBlock) {
		t.Errorf("wrong identifier: %v, want ErrCannotReadBlock", err)
	}
	if err := img.SetDPMRaw(block[:4]); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("4-byte block: %v, want ErrIncorrectDataSize", err)
	}

	if _, err := img.DPMRaw(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("DPMRaw after rejected sets: %v, want ErrMetadataNotPresent", err)
	}
}

func TestDPMReadOnlyImageRefusesChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_readonly.aif")
	written := buildTestDPM(10, 131700)
	mustClose(t, createDPMTestImage(t, path, false))

	img := mustOpen(t, path)
	defer img.Close()
	if err := img.SetDPMRaw(written); !errors.Is(err, ErrReadOnly) {
		t.Errorf("SetDPMRaw: %v, want ErrReadOnly", err)
	}
	if err := img.ClearDPM(); !errors.Is(err, ErrReadOnly) {
		t.Errorf("ClearDPM: %v, want ErrReadOnly", err)
	}
}

func TestDPMResumeKeepsDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_resume_keep.aif")
	written := buildTestDPM(100, 131700)
	img := createDPMTestImage(t, path, false)
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	if got := getTestDPM(img); !bytes.Equal(got, written) {
		t.Error("DPM must be readable on resumed images")
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if got := getTestDPM(img); !bytes.Equal(got, written) {
		t.Error("DPM lost after resuming")
	}
	if n, _ := countDPMIndexEntries(img); n != 1 {
		t.Errorf("%d DPM index entries, want 1", n)
	}
}

func TestDPMResumeAddsDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_resume_add.aif")
	written := buildTestDPM(100, 131700)
	mustClose(t, createDPMTestImage(t, path, false))

	img := mustResume(t, path, "compress=false")
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if got := getTestDPM(img); !bytes.Equal(got, written) {
		t.Error("DPM added on resume not found")
	}
	if n, _ := countDPMIndexEntries(img); n != 1 {
		t.Errorf("%d DPM index entries, want 1", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestDPMResumeReplacesDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_resume_replace.aif")
	first := buildTestDPM(100, 131700)
	second := buildTestDPM(150, 224800)
	img := createDPMTestImage(t, path, false)
	if err := img.SetDPMRaw(first); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	if err := img.SetDPMRaw(second); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if got := getTestDPM(img); !bytes.Equal(got, second) {
		t.Error("DPM not replaced")
	}
	if n, _ := countDPMIndexEntries(img); n != 1 {
		t.Errorf("%d DPM index entries, want 1 (the old DPM must not stay in the index)", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestDPMResumeClearsDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_resume_clear.aif")
	written := buildTestDPM(100, 131700)
	img := createDPMTestImage(t, path, false)
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustResume(t, path, "compress=false")
	if err := img.ClearDPM(); err != nil {
		t.Fatal(err)
	}
	if _, err := img.DPMRaw(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("DPMRaw after ClearDPM: %v, want ErrMetadataNotPresent", err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if _, err := img.DPMRaw(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("DPMRaw after reopening: %v, want ErrMetadataNotPresent", err)
	}
	if n, _ := countDPMIndexEntries(img); n != 0 {
		t.Errorf("%d DPM index entries, want 0", n)
	}
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// flipByte xors the byte at off of the file with 0xFF, returning its
// original value.
func flipByte(t *testing.T, path string, off int64) byte {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var b [1]byte
	if _, err := f.ReadAt(b[:], off); err != nil {
		t.Fatal(err)
	}
	orig := b[0]
	b[0] ^= 0xFF
	if _, err := f.WriteAt(b[:], off); err != nil {
		t.Fatal(err)
	}
	return orig
}

func writeByte(t *testing.T, path string, off int64, v byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt([]byte{v}, off); err != nil {
		t.Fatal(err)
	}
}

func TestDPMVerifyDetectsCorruptedDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_corrupt.aif")
	written := buildTestDPM(100, 131700)
	img := createDPMTestImage(t, path, false)
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	n, off := countDPMIndexEntries(img)
	mustClose(t, img)
	if n != 1 {
		t.Fatalf("%d DPM index entries, want 1", n)
	}

	flipByte(t, path, int64(off)+dpmHeaderSize+30)

	img = mustOpen(t, path)
	defer img.Close()
	if _, err := img.DPMRaw(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("DPMRaw: %v, want ErrMetadataNotPresent (a corrupted DPM must be ignored on open)", err)
	}
	if err := img.Verify(); !errors.Is(err, ErrInvalidBlockCRC) {
		t.Errorf("Verify: %v, want ErrInvalidBlockCRC", err)
	}
}

func TestDPMErasureCodingRecoversCorruptedDpm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_dpm_erasure.aif")
	written := buildTestDPM(100, 131700)
	img := createDPMTestImage(t, path, true)
	if err := img.SetDPMRaw(written); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	n, off := countDPMIndexEntries(img)
	mustClose(t, img)
	if n != 1 {
		t.Fatalf("%d DPM index entries, want 1", n)
	}

	// Damage the DPM payload, and separately its header.
	for _, damage := range []int64{int64(off) + dpmHeaderSize + 30, int64(off) + 4} {
		orig := flipByte(t, path, damage)
		img = mustOpen(t, path)
		if got := getTestDPM(img); !bytes.Equal(got, written) {
			t.Errorf("damage at %d: the DPM must be recovered from the erasure coding parity", damage)
		}
		mustClose(t, img)
		writeByte(t, path, damage, orig)
	}
}

// The decoded API matches the serialized form.
func TestDPMDecoded(t *testing.T) {
	raw := buildTestDPM(10, 131700)
	d, err := ParseDPM(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := metaDPM(10, 131700)
	if !reflect.DeepEqual(d, want) {
		t.Errorf("ParseDPM = %+v, want %+v", d, want)
	}
	b, err := d.MarshalBinary()
	if err != nil || !bytes.Equal(b, raw) {
		t.Errorf("MarshalBinary = %x, %v; want %x", b, err, raw)
	}
	empty, err := (&DPM{}).MarshalBinary()
	if err != nil || len(empty) != dpmHeaderSize || ValidateDPM(empty) != nil {
		t.Errorf("empty DPM: %x, %v, valid %v", empty, err, ValidateDPM(empty))
	}

	path := filepath.Join(t.TempDir(), "dpm_decoded.aif")
	img := createDPMTestImage(t, path, false)
	if _, err := img.DPM(); !errors.Is(err, ErrMetadataNotPresent) {
		t.Errorf("DPM on a new image: %v", err)
	}
	if err := img.SetDPM(want); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	got, err := img.DPM()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("DPM = %+v, %v; want %+v", got, err, want)
	}
	if err := img.SetDPM(want); !errors.Is(err, ErrReadOnly) {
		t.Errorf("SetDPM on a read-only image: %v", err)
	}
	if _, err := (&DPM{LayerEnds: make([]uint64, 256)}).MarshalBinary(); !errors.Is(err, ErrIncorrectDataSize) {
		t.Errorf("256 layer ends: %v", err)
	}
}
