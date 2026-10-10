package wiiu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// These tests build synthetic WUD and WUX images.

func makeDisc(sectors int) []byte {
	disc := make([]byte, sectors*PhysicalSectorSize)
	for s := 0; s < sectors; s++ {
		for i := 0; i < PhysicalSectorSize; i++ {
			disc[s*PhysicalSectorSize+i] = byte(s*31 + i*7)
		}
	}

	// Make sectors 1 and 3 identical so the WUX deduplicates them.
	if sectors >= 4 {
		copy(disc[3*PhysicalSectorSize:4*PhysicalSectorSize], disc[1*PhysicalSectorSize:2*PhysicalSectorSize])
	}

	return disc
}

// makeWUX deduplicates disc into a WUX image.
func makeWUX(disc []byte) []byte {
	count := len(disc) / PhysicalSectorSize
	index := make([]uint32, count)

	var stored [][]byte

	for s := 0; s < count; s++ {
		sec := disc[s*PhysicalSectorSize : (s+1)*PhysicalSectorSize]
		found := -1

		for j, st := range stored {
			if bytes.Equal(st, sec) {
				found = j

				break
			}
		}

		if found < 0 {
			found = len(stored)
			stored = append(stored, sec)
		}

		index[s] = uint32(found)
	}

	var out bytes.Buffer

	hdr := make([]byte, wuxHeaderSize)
	binary.LittleEndian.PutUint32(hdr, WUXMagic)
	binary.LittleEndian.PutUint32(hdr[8:], PhysicalSectorSize)
	binary.LittleEndian.PutUint64(hdr[16:], uint64(len(disc)))
	out.Write(hdr)

	for _, v := range index {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}

	for out.Len()%PhysicalSectorSize != 0 {
		out.WriteByte(0)
	}

	for _, st := range stored {
		out.Write(st)
	}

	return out.Bytes()
}

func checkReads(t *testing.T, r *Reader, disc []byte) {
	t.Helper()

	if r.Size() != int64(len(disc)) {
		t.Fatalf("Size = %d, want %d", r.Size(), len(disc))
	}

	full := make([]byte, len(disc))
	if n, err := r.ReadAt(full, 0); n != len(disc) || err != nil {
		t.Fatalf("full ReadAt = %d, %v", n, err)
	}

	if !bytes.Equal(full, disc) {
		t.Fatal("full read mismatch")
	}

	// Reads crossing sector boundaries at odd offsets.
	for _, off := range []int64{1, PhysicalSectorSize - 5, 2*PhysicalSectorSize + 1234, int64(len(disc)) - 100} {
		buf := make([]byte, 100)

		n, err := r.ReadAt(buf, off)
		// io.ReaderAt may return io.EOF together with a full read at the end.
		if n != 100 || (err != nil && !errors.Is(err, io.EOF)) {
			t.Fatalf("ReadAt(%d) = %d, %v", off, n, err)
		}

		if !bytes.Equal(buf, disc[off:off+100]) {
			t.Fatalf("ReadAt(%d) mismatch", off)
		}
	}

	// Short read at the end.
	buf := make([]byte, 200)

	n, err := r.ReadAt(buf, int64(len(disc))-50)
	if n != 50 || !errors.Is(err, io.EOF) {
		t.Fatalf("tail ReadAt = %d, %v; want 50, EOF", n, err)
	}

	if !bytes.Equal(buf[:50], disc[len(disc)-50:]) {
		t.Fatal("tail read mismatch")
	}

	if n, err := r.ReadAt(buf, int64(len(disc))); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt(end) = %d, %v; want 0, EOF", n, err)
	}

	if _, err := r.ReadAt(buf, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
}

func TestReaderWUD(t *testing.T) {
	disc := makeDisc(5)

	r, err := NewReader(bytes.NewReader(disc), int64(len(disc)))
	if err != nil {
		t.Fatal(err)
	}

	if r.IsWUX() {
		t.Fatal("WUD detected as WUX")
	}

	checkReads(t, r, disc)
}

func TestReaderWUX(t *testing.T) {
	disc := makeDisc(5)
	wux := makeWUX(disc)

	// Dedup: 4 unique sectors stored after header+index aligned to 0x8000.
	if len(wux) != PhysicalSectorSize+4*PhysicalSectorSize {
		t.Fatalf("unexpected WUX size %d", len(wux))
	}

	r, err := NewReader(bytes.NewReader(wux), int64(len(wux)))
	if err != nil {
		t.Fatal(err)
	}

	if !r.IsWUX() {
		t.Fatal("WUX not detected")
	}

	checkReads(t, r, disc)
}

func TestOpenReaderFiles(t *testing.T) {
	dir := t.TempDir()
	disc := makeDisc(4)

	wudPath := filepath.Join(dir, "disc.wud")
	wuxPath := filepath.Join(dir, "disc.wux")

	if err := os.WriteFile(wudPath, disc, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(wuxPath, makeWUX(disc), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{wudPath, wuxPath} {
		r, err := OpenReader(p)
		if err != nil {
			t.Fatalf("OpenReader(%s): %v", p, err)
		}

		checkReads(t, r, disc)

		if err := r.Close(); err != nil {
			t.Fatal(err)
		}

		if err := r.Close(); err != nil {
			t.Fatal("second Close failed")
		}
	}

	if _, err := OpenReader(filepath.Join(dir, "missing.wud")); err == nil {
		t.Fatal("opening a missing file succeeded")
	}

	empty := filepath.Join(dir, "empty.wud")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenReader(empty); err == nil {
		t.Fatal("opening an empty file succeeded")
	}
}

func TestReaderInvalidWUX(t *testing.T) {
	disc := makeDisc(2)

	badSector := makeWUX(disc)
	binary.LittleEndian.PutUint32(badSector[8:], 0x800)

	if _, err := NewReader(bytes.NewReader(badSector), int64(len(badSector))); !errors.Is(err, ErrInvalidWUX) {
		t.Errorf("bad sector size: %v", err)
	}

	hugeSize := makeWUX(disc)
	binary.LittleEndian.PutUint64(hugeSize[16:], 1<<62)

	if _, err := NewReader(bytes.NewReader(hugeSize), int64(len(hugeSize))); !errors.Is(err, ErrInvalidWUX) {
		t.Errorf("huge size: %v", err)
	}

	// Index pointing past the stored data yields an error, not a panic.
	badIndex := makeWUX(disc)
	binary.LittleEndian.PutUint32(badIndex[wuxHeaderSize:], 1000)

	r, err := NewReader(bytes.NewReader(badIndex), int64(len(badIndex)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.ReadAt(make([]byte, 16), 0); err == nil || errors.Is(err, io.EOF) {
		t.Errorf("bad index read: %v", err)
	}
}
