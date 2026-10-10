package aaru

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// writeScriptImage runs s and fails the test unless every operation
// succeeds, returning the image path.
func writeScriptImage(t *testing.T, s *writeScript) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.aif")
	for i, rc := range s.runGo(path) {
		if rc != 0 {
			t.Fatalf("op %d (%s): rc %d", i, s.ops[i].kind, rc)
		}
	}
	return path
}

func TestUserDataDDTHeader(t *testing.T) {
	// Single level, LZMA compressed table.
	img, err := Open(cdTestImage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	h, ok := img.UserDataDDTHeader()
	if !ok || h.Identifier != BlockTypeDeDuplicationTable2 || h.Type != DataTypeUserData ||
		h.Compression != CompressionLzma || h.Levels != 1 || h.TableLevel != 0 || h.Blocks != 300 ||
		h.Entries != 300 || h.Length != 300*8 || h.CmpLength == 0 || h.CmpLength >= h.Length ||
		h.BlockAlignmentShift != 9 || h.DataShift != 12 || h.TableShift != 0 || h.Negative != 0 || h.Overflow != 0 {
		t.Errorf("single level: %+v %v", h, ok)
	}

	// Multi-level, with negative and overflow sectors: the primary table has
	// one entry per 1<<TableShift sectors.
	ml, err := Open(writeScriptImage(t, blockScenario(30, "table_shift=2", 300, 16, 8, true)))
	if err != nil {
		t.Fatal(err)
	}
	defer ml.Close()
	if h, ok := ml.UserDataDDTHeader(); !ok || h.Levels != 2 || h.TableShift != 2 || h.Negative != 16 ||
		h.Overflow != 8 || h.Blocks != 324 || h.Entries != 81 {
		t.Errorf("multi-level: %+v %v", h, ok)
	}

	v1, err := OpenReader(bytes.NewReader(v1CDMode1Image()), int64(len(v1CDMode1Image())))
	if err != nil {
		t.Fatal(err)
	}
	defer v1.Close()
	if _, ok := v1.UserDataDDTHeader(); ok {
		t.Error("version 1 image has a DDT2 header")
	}

	fluxPath := filepath.Join(t.TempDir(), "flux.aif")
	flux := createFluxOnly(t, fluxPath, "compress=false")
	if err := flux.WriteFluxCapture(FluxCaptureKey{}, 25000000, 25000000, fillPattern(4096, 1), fillPattern(64, 2)); err != nil {
		t.Fatal(err)
	}
	mustClose(t, flux)
	fimg, err := Open(fluxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fimg.Close()
	if _, ok := fimg.UserDataDDTHeader(); ok {
		t.Error("flux-only image has a user data DDT")
	}
}

func TestErasureCodingInfo(t *testing.T) {
	// 400 uncompressed sectors in blocks of 32: 13 data blocks, protected in
	// stripes of K=4.
	path := filepath.Join(t.TempDir(), "ec.aif")
	p := app()
	p.MediaType = media.GENERIC_HDD
	p.SectorSize = 512
	p.UserSectors = 400
	w, err := Create(path, p, optsPtr("compress=false;deduplicate=false;data_shift=5"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetErasureCoding(ErasureCodingRsVandermonde, 4, 2); err != nil {
		t.Fatal(err)
	}
	for i, s := range seededSectors(31, 400, 512, 0) {
		if err := w.WriteSector(uint64(i), false, s, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	ec, ok := img.ErasureCoding()
	const biggestBlock = blockHeaderSize + 32*512
	if !ok || ec.Algorithm != ErasureCodingRsVandermonde || ec.K != 4 || ec.M != 2 || ec.DataStripes != 4 ||
		ec.ShardSize < biggestBlock {
		t.Errorf("erasure coded image: %+v %v", ec, ok)
	}

	raw, err := Open(blockTestImage(t, media.GENERIC_HDD, 64))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if ec, ok := raw.ErasureCoding(); ok || ec != (ErasureCodingInfo{}) {
		t.Errorf("image without erasure coding: %+v %v", ec, ok)
	}

	// The algorithm recorded by SetErasureCoding is reported (XOR needs M=1).
	path = filepath.Join(t.TempDir(), "xor.aif")
	w, err = Create(path, CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512, UserSectors: 64,
		Application: "go-aaru-test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetErasureCoding(ErasureCodingXor, 3, 1); err != nil {
		t.Fatal(err)
	}
	sector := make([]byte, 512)
	for i := uint64(0); i < 64; i++ {
		sector[0] = byte(i)
		sector[1] = byte(i * 7)
		if err := w.WriteSector(i, false, sector, SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if ec, ok := r.ErasureCoding(); !ok || ec.Algorithm != ErasureCodingXor || ec.K != 3 || ec.M != 1 {
		t.Errorf("xor: %+v %v", ec, ok)
	}
}
