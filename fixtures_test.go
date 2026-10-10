package aaru

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// Test inputs built at test time.

// writeTempFile writes data to a new file in a per-test directory.
func writeTempFile(t testing.TB, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seededRandom returns n bytes of deterministic pseudo-random data.
func seededRandom(seed uint64, n int) []byte {
	return randBytes(rand.New(rand.NewPCG(seed, 0xda7a)), n)
}

// v1FloppyImage returns a version 1 LZMA compressed 1.44 MB floppy image
// (DICM magic) and its sectors. Callers must not modify them.
var v1FloppyImage = sync.OnceValues(func() ([]byte, [][]byte) {
	sectors := seededSectors(77, 2880, 512, 4)
	b, _ := v1BlockImage(media.DOS_35_HD, 512, sectors, CompressionLzma)
	return b, sectors
})

// v1CDMode1Image builds a version 1 CD Mode 1 image with stored and
// regenerated prefixes and suffixes.
func v1CDMode1Image() []byte {
	raw := rawCDSectors(78, 300, false, map[int]bool{4: true, 9: true})
	b, _ := v1CDImage(raw, false, nil)
	return b
}

// testIndexEntry returns the first index entry of block type bt (and data
// type dt, unless it is DataTypeNone) of the image at path.
func testIndexEntry(t testing.TB, path string, bt BlockType, dt DataType) IndexEntry {
	t.Helper()
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for _, e := range img.indexEntries {
		if e.BlockType == bt && (dt == DataTypeNone || e.DataType == dt) {
			return e
		}
	}
	t.Fatalf("no %v/%v block in %s", bt, dt, path)
	return IndexEntry{}
}
