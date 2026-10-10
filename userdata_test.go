package aaru

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

func TestUserDataReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dvd.aif")
	img, err := Create(path, CreateParams{MediaType: media.DVDROM, SectorSize: 2048, UserSectors: 10,
		Application: "go-aaru-test"}, optsPtr("compress=true"))
	if err != nil {
		t.Fatal(err)
	}
	want := seededRandom(0x05e2, 10*2048)
	for lba := uint64(0); lba < 10; lba++ {
		if lba == 5 {
			continue // never dumped
		}
		st := SectorStatusDumped
		if lba == 7 {
			st = SectorStatusErrored
		}
		if err := img.WriteSector(lba, false, want[lba*2048:(lba+1)*2048], st); err != nil {
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
	defer img.Close()
	r, err := img.UserDataReaderAt()
	if err != nil {
		t.Fatal(err)
	}
	if r.Size() != 10*2048 {
		t.Fatalf("size %d", r.Size())
	}
	statuses := map[uint64]SectorStatus{}
	r.OnSector = func(lba uint64, st SectorStatus) { statuses[lba] = st }

	buf := make([]byte, 3000)
	if n, err := r.ReadAt(buf, 1000); n != 3000 || err != nil || !bytes.Equal(buf, want[1000:4000]) {
		t.Errorf("ReadAt across sectors: %d, %v", n, err)
	}
	n, err := r.ReadAt(buf, 4*2048+100)
	if !errors.Is(err, ErrSectorNotDumped) || n != 2048-100 || !bytes.Equal(buf[:n], want[4*2048+100:5*2048]) {
		t.Errorf("not-dumped sector: %d, %v", n, err)
	}
	if n, err := r.ReadAt(buf[:200], 7*2048); n != 200 || err != nil || statuses[7] != SectorStatusErrored {
		t.Errorf("errored sector: %d, %v, status %v", n, err, statuses[7])
	}
	if n, err := r.ReadAt(buf, 10*2048-100); n != 100 || err != io.EOF {
		t.Errorf("read at the end: %d, %v", n, err)
	}
	if _, err := r.ReadAt(buf, -1); err == nil {
		t.Error("negative offset accepted")
	}
}

func TestUserDataReaderSectorSizes(t *testing.T) {
	// Block media must have 2048-byte sectors.
	path := filepath.Join(t.TempDir(), "floppy.aif")
	img, err := Create(path, testParams(media.DOS_35_HD, 512, 16, 0), optsPtr("compress=false"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := img.UserDataReaderAt(); !errors.Is(err, ErrInvalidSectorLength) {
		t.Errorf("512-byte sectors: %v", err)
	}
	img.Close()
	if _, err := img.UserDataReaderAt(); !errors.Is(err, ErrClosed) {
		t.Errorf("closed: %v", err)
	}

	// A mixed-mode CD (SectorSize 2352): data sectors read, audio sectors
	// fail.
	b := rdScriptBytes(t, rdTinyCD())
	cd, err := OpenReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := cd.UserDataReaderAt()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	if _, err := r.ReadAt(buf, 0); err != nil {
		t.Errorf("data sector: %v", err)
	}
	if _, err := r.ReadAt(buf, 8*2048); !errors.Is(err, ErrInvalidSectorLength) {
		t.Errorf("audio sector: %v", err)
	}
}
