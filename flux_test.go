package aaru

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/crc64"
)

// Tests of flux-only images and of the flux capture API.

// fillPattern fills a buffer with a deterministic pattern.
func fillPattern(n int, seed uint32) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(seed + uint32(i)*7)
	}
	return b
}

func createFluxOnly(t *testing.T, path, opts string) *Image {
	t.Helper()
	o := ParseOptions(opts)
	img, err := Create(path, CreateParams{MediaType: 1, SectorSize: 512, Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// create_open_read_verify_flux_only: create a flux-only image (all sector
// counts zero), write a flux capture, close, reopen, verify and read back
// the capture. Sector reads must be rejected with ErrUserDataNotPresent.
func TestFluxOnlyCreateOpenReadVerify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_flux_only.aif")
	img := createFluxOnly(t, path, "compress=false")

	const dataLen, indexLen = 4096, 64
	data := fillPattern(dataLen, 0xA5)
	index := fillPattern(indexLen, 0x5A)
	if err := img.WriteFluxCapture(FluxCaptureKey{}, 25000000, 25000000, data, index); err != nil {
		t.Fatalf("WriteFluxCapture: %v", err)
	}
	// Sector writes against a flux-only image must be rejected.
	if err := img.WriteSector(0, false, fillPattern(512, 0), SectorStatusDumped); !errors.Is(err, ErrUserDataNotPresent) {
		t.Errorf("WriteSector: %v, want ErrUserDataNotPresent", err)
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	gotIndex, gotData, err := img.ReadFluxCapture(FluxCaptureKey{})
	if err != nil {
		t.Fatalf("ReadFluxCapture: %v", err)
	}
	if len(gotData) != dataLen || len(gotIndex) != indexLen {
		t.Errorf("lengths %d/%d, want %d/%d", len(gotData), len(gotIndex), dataLen, indexLen)
	}
	if !bytes.Equal(gotData, data) || !bytes.Equal(gotIndex, index) {
		t.Error("flux round-trip mismatch")
	}

	if _, st, err := img.ReadSector(0, false); !errors.Is(err, ErrUserDataNotPresent) || st != SectorStatusNotDumped {
		t.Errorf("ReadSector: status %v, %v; want SectorStatusNotDumped, ErrUserDataNotPresent", st, err)
	}
	if _, _, err := img.ReadSectorLong(0, false); !errors.Is(err, ErrUserDataNotPresent) {
		t.Errorf("ReadSectorLong: %v, want ErrUserDataNotPresent", err)
	}
	if _, err := img.ReadSectorTag(0, false, SectorTagCdSubchannel); !errors.Is(err, ErrUserDataNotPresent) {
		t.Errorf("ReadSectorTag: %v, want ErrUserDataNotPresent", err)
	}
}

// regular_image_flux_read_returns_not_found: an image with sectors and no
// flux data answers flux reads with ErrFluxDataNotFound.
func TestFluxRegularImageReadReturnsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_regular_no_flux.aif")
	o := ParseOptions("compress=false")
	img, err := Create(path, CreateParams{MediaType: 1, SectorSize: 512, UserSectors: 16, Application: "go-aaru-test"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	sector := fillPattern(512, 0x42)
	for i := uint64(0); i < 16; i++ {
		if err := img.WriteSector(i, false, sector, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	if err := img.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if _, _, err := img.ReadFluxCapture(FluxCaptureKey{}); !errors.Is(err, ErrFluxDataNotFound) {
		t.Errorf("ReadFluxCapture: %v, want ErrFluxDataNotFound", err)
	}
	if _, err := img.FluxCaptures(); !errors.Is(err, ErrFluxDataNotFound) {
		t.Errorf("FluxCaptures: %v, want ErrFluxDataNotFound", err)
	}
}

type fluxCase struct {
	key         FluxCaptureKey
	dres, ires  uint64
	data, index []byte
}

func fluxCases() []fluxCase {
	return []fluxCase{
		{FluxCaptureKey{0, 0, 0, 0}, 25000000, 25000000, bytes.Repeat([]byte{1, 2, 3, 4}, 4096), fillPattern(64, 1)},
		{FluxCaptureKey{1, 79, 0, 0}, 25000, 50000, fillPattern(1000, 2), nil},
		{FluxCaptureKey{0, 1, 2, 3}, 1, 2, nil, fillPattern(10, 3)},
		{FluxCaptureKey{0, 2, 0, 0}, 1, 2, nil, nil},
		{FluxCaptureKey{0xFFFFFFFF, 0xFFFF, 0xFF, 0xFFFFFFFF}, 1 << 60, 3, fillPattern(5000, 4), fillPattern(300, 5)},
	}
}

func TestFluxRoundTripCompression(t *testing.T) {
	for _, opts := range []string{"compress=false", "compress=true", "compress=true;zstd=true;zstd_level=3",
		"compress=true;block_alignment=12"} {
		t.Run(opts, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "flux.aif")
			img := createFluxOnly(t, path, opts)
			if _, err := img.FluxCaptures(); !errors.Is(err, ErrFluxDataNotFound) {
				t.Errorf("FluxCaptures on a new image: %v", err)
			}
			cases := fluxCases()
			for _, c := range cases {
				if err := img.WriteFluxCapture(c.key, c.dres, c.ires, c.data, c.index); err != nil {
					t.Fatal(err)
				}
			}
			check := func(img *Image, when string) {
				caps, err := img.FluxCaptures()
				if err != nil || len(caps) != len(cases) {
					t.Fatalf("%s: FluxCaptures = %d, %v", when, len(caps), err)
				}
				for i, c := range cases {
					want := FluxCaptureInfo{FluxCaptureKey: c.key, IndexResolution: c.ires, DataResolution: c.dres}
					if caps[i] != want {
						t.Errorf("%s: capture %d = %+v, want %+v", when, i, caps[i], want)
					}
					index, data, err := img.ReadFluxCapture(c.key)
					if err != nil || !bytes.Equal(data, c.data) || !bytes.Equal(index, c.index) {
						t.Errorf("%s: ReadFluxCapture(%+v) = %d/%d bytes, %v", when, c.key, len(data), len(index), err)
					}
				}
			}
			check(img, "while writing")
			mustClose(t, img)
			img = mustOpen(t, path)
			defer img.Close()
			check(img, "after reopening")
			if err := img.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
			if _, _, err := img.ReadFluxCapture(FluxCaptureKey{Head: 9}); !errors.Is(err, ErrFluxDataNotFound) {
				t.Errorf("missing capture: %v", err)
			}
			if err := img.WriteFluxCapture(FluxCaptureKey{}, 1, 1, nil, nil); !errors.Is(err, ErrReadOnly) {
				t.Errorf("WriteFluxCapture on a read-only image: %v", err)
			}
		})
	}
}

// A key written twice: both captures are stored, lookups return the last.
func TestFluxDuplicateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	k := FluxCaptureKey{Head: 1, Track: 2}
	if err := img.WriteFluxCapture(k, 1, 1, []byte("first"), nil); err != nil {
		t.Fatal(err)
	}
	if err := img.WriteFluxCapture(k, 2, 2, []byte("second"), []byte("i")); err != nil {
		t.Fatal(err)
	}
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	caps, _ := img.FluxCaptures()
	if len(caps) != 2 {
		t.Fatalf("%d captures, want 2", len(caps))
	}
	index, data, err := img.ReadFluxCapture(k)
	if err != nil || string(data) != "second" || string(index) != "i" {
		t.Errorf("ReadFluxCapture = %q/%q, %v; want the last capture", data, index, err)
	}
}

func TestFluxClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	img.WriteFluxCapture(FluxCaptureKey{Head: 1}, 1, 1, []byte("discarded"), nil)
	if err := img.ClearFluxCaptures(); err != nil {
		t.Fatal(err)
	}
	if _, err := img.FluxCaptures(); !errors.Is(err, ErrFluxDataNotFound) {
		t.Errorf("FluxCaptures after clear: %v", err)
	}
	img.WriteFluxCapture(FluxCaptureKey{Head: 2}, 1, 1, []byte("kept"), nil)
	mustClose(t, img)

	img = mustOpen(t, path)
	caps, err := img.FluxCaptures()
	if err != nil || len(caps) != 1 || caps[0].Head != 2 {
		t.Errorf("FluxCaptures = %+v, %v; want only head 2", caps, err)
	}
	// On a read-only image ClearFluxCaptures only forgets the loaded
	// captures: they are loaded again on the next access.
	if err := img.ClearFluxCaptures(); err != nil {
		t.Errorf("ClearFluxCaptures on a read-only image: %v", err)
	}
	if caps, err := img.FluxCaptures(); err != nil || len(caps) != 1 {
		t.Errorf("FluxCaptures after clearing a read-only image = %d, %v", len(caps), err)
	}
	mustClose(t, img)
	if err := img.ClearFluxCaptures(); !errors.Is(err, ErrClosed) {
		t.Errorf("ClearFluxCaptures on a closed image: %v", err)
	}
}

// Captures added after resuming are kept together with the stored ones.
func TestFluxResume(t *testing.T) {
	for _, opts := range []string{"compress=false", "compress=true"} {
		t.Run(opts, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "flux.aif")
			img := createFluxOnly(t, path, opts)
			cases := fluxCases()
			for _, c := range cases[:2] {
				img.WriteFluxCapture(c.key, c.dres, c.ires, c.data, c.index)
			}
			mustClose(t, img)

			img = mustResume(t, path, opts)
			for _, c := range cases[2:] {
				if err := img.WriteFluxCapture(c.key, c.dres, c.ires, c.data, c.index); err != nil {
					t.Fatal(err)
				}
			}
			// The stored captures are readable while resuming, the new ones too.
			for _, c := range cases {
				index, data, err := img.ReadFluxCapture(c.key)
				if err != nil || !bytes.Equal(data, c.data) || !bytes.Equal(index, c.index) {
					t.Errorf("resumed: ReadFluxCapture(%+v): %v", c.key, err)
				}
			}
			mustClose(t, img)

			img = mustOpen(t, path)
			defer img.Close()
			caps, err := img.FluxCaptures()
			if err != nil || len(caps) != len(cases) {
				t.Fatalf("FluxCaptures = %d, %v; want %d", len(caps), err, len(cases))
			}
			for _, c := range cases {
				index, data, err := img.ReadFluxCapture(c.key)
				if err != nil || !bytes.Equal(data, c.data) || !bytes.Equal(index, c.index) {
					t.Errorf("reopened: ReadFluxCapture(%+v): %v", c.key, err)
				}
			}
			n := 0
			for _, e := range img.indexEntries {
				if e.BlockType == BlockTypeFluxDataBlock {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d FLUX index entries, want 1", n)
			}
			if err := img.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

// fluxPayloadOffset returns the absolute offset of the payload of the
// first capture.
func fluxPayloadOffset(t *testing.T, path string) (*Image, uint64) {
	t.Helper()
	img := mustOpen(t, path)
	if _, err := img.FluxCaptures(); err != nil {
		t.Fatal(err)
	}
	return img, img.fluxEntries[0].PayloadOffset << img.fluxHeader.BlockAlignmentShift
}

func TestFluxDamagedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	img.WriteFluxCapture(FluxCaptureKey{}, 1, 1, fillPattern(100, 1), fillPattern(10, 2))
	mustClose(t, img)

	img, off := fluxPayloadOffset(t, path)
	img.Close()

	cases := []struct {
		name string
		pos  int64
		want error
	}{
		{"payload", int64(off) + dataStreamPayloadHdrSize + 5, ErrInvalidBlockCRC},
		{"identifier", int64(off), ErrCannotReadBlock},
		{"data type", int64(off) + 4, ErrCannotReadBlock},
		{"compression", int64(off) + 6, ErrUnsupportedCompression},
		{"length", int64(off) + 12, ErrCannotDecompressBlock},
		{"compressed CRC", int64(off) + 16, ErrInvalidBlockCRC},
		{"CRC", int64(off) + 24, ErrInvalidBlockCRC},
	}
	for _, c := range cases {
		orig := flipByte(t, path, c.pos)
		img := mustOpen(t, path)
		if _, _, err := img.ReadFluxCapture(FluxCaptureKey{}); !errors.Is(err, c.want) {
			t.Errorf("%s damaged: %v, want %v", c.name, err, c.want)
		}
		img.Close()
		writeByte(t, path, c.pos, orig)
	}
}

// Flux entries whose CRC64 does not match are still used.
func TestFluxEntriesBadCRC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	img.WriteFluxCapture(FluxCaptureKey{Head: 3}, 1, 1, []byte("data"), []byte("index"))
	mustClose(t, img)

	img = mustOpen(t, path)
	var fluxOff uint64
	for _, e := range img.indexEntries {
		if e.BlockType == BlockTypeFluxDataBlock {
			fluxOff = e.Offset
		}
	}
	img.Close()
	flipByte(t, path, int64(fluxOff)+7) // header CRC64

	img = mustOpen(t, path)
	defer img.Close()
	index, data, err := img.ReadFluxCapture(FluxCaptureKey{Head: 3})
	if err != nil || string(data) != "data" || string(index) != "index" {
		t.Errorf("ReadFluxCapture = %q/%q, %v", data, index, err)
	}
	if img.fluxMap != nil {
		t.Error("entries with a bad CRC must not be indexed by key")
	}
}

// An index offset beyond the payload is reported as ErrInvalidBlockCRC.
func TestFluxIndexOffsetBeyondPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	img.WriteFluxCapture(FluxCaptureKey{}, 1, 1, []byte("data"), []byte("index"))
	mustClose(t, img)
	img = mustOpen(t, path)
	defer img.Close()
	if _, err := img.FluxCaptures(); err != nil {
		t.Fatal(err)
	}
	img.fluxEntries[0].IndexOffset = 100
	if _, _, err := img.ReadFluxCapture(FluxCaptureKey{}); !errors.Is(err, ErrInvalidBlockCRC) {
		t.Errorf("ReadFluxCapture: %v, want ErrInvalidBlockCRC", err)
	}
}

// The on-disk layout of a flux-only image written by go-aaru.
func TestFluxLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flux.aif")
	img := createFluxOnly(t, path, "compress=false")
	data, index := []byte("0123456789"), []byte("abc")
	img.WriteFluxCapture(FluxCaptureKey{Head: 1, Track: 2, Subtrack: 3, CaptureIndex: 4}, 5, 6, data, index)
	mustClose(t, img)

	img = mustOpen(t, path)
	defer img.Close()
	var payloads []IndexEntry
	var flux IndexEntry
	for _, e := range img.indexEntries {
		switch e.BlockType {
		case BlockTypeDataStreamPayloadBlock:
			payloads = append(payloads, e)
		case BlockTypeFluxDataBlock:
			flux = e
		}
	}
	if len(payloads) != 1 || payloads[0].DataType != DataTypeFluxData || flux.Offset == 0 {
		t.Fatalf("index: payloads %+v, flux %+v", payloads, flux)
	}
	raw, err := img.readAt(payloads[0].Offset, dataStreamPayloadHdrSize+13)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := unmarshal[DataStreamPayloadHeader](raw)
	all := append(append([]byte(nil), data...), index...)
	want := DataStreamPayloadHeader{Identifier: BlockTypeDataStreamPayloadBlock, DataType: DataTypeFluxData,
		Compression: CompressionNone, CmpLength: 13, Length: 13, CmpCRC64: crc64.Checksum(all), CRC64: crc64.Checksum(all)}
	if h != want || !bytes.Equal(raw[dataStreamPayloadHdrSize:], all) {
		t.Errorf("payload block = %+v %q, want %+v", h, raw[dataStreamPayloadHdrSize:], want)
	}
	fh, err := img.readAt(flux.Offset, fluxHeaderSize+fluxEntrySize)
	if err != nil {
		t.Fatal(err)
	}
	hdr, _ := unmarshal[FluxHeader](fh)
	entry, _ := unmarshal[FluxEntry](fh[fluxHeaderSize:])
	if hdr.Entries != 1 || hdr.BlockAlignmentShift != 9 || hdr.CRC64 != crc64.Checksum(fh[fluxHeaderSize:]) {
		t.Errorf("flux header = %+v", hdr)
	}
	wantEntry := FluxEntry{Head: 1, Track: 2, Subtrack: 3, CaptureIndex: 4, IndexResolution: 6, DataResolution: 5,
		IndexOffset: 10, PayloadOffset: payloads[0].Offset >> 9}
	if entry != wantEntry {
		t.Errorf("flux entry = %+v, want %+v", entry, wantEntry)
	}
	// Payload blocks are aligned.
	if payloads[0].Offset%512 != 0 || flux.Offset%512 != 0 {
		t.Errorf("unaligned blocks: %d %d", payloads[0].Offset, flux.Offset)
	}
}
