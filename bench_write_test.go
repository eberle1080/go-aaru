package aaru

import (
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru/media"
)

// BenchmarkWriteSector measures the per-sector cost of the write path
// without compression.
func BenchmarkWriteSector(b *testing.B) {
	for _, opts := range []string{"compress=false;deduplicate=false", "compress=false;deduplicate=true"} {
		b.Run(opts, func(b *testing.B) {
			r := rand.New(rand.NewPCG(1, 1))
			data := randBytes(r, 512*1024)
			o := ParseOptions(opts)
			img, err := Create(filepath.Join(b.TempDir(), "b.aif"), CreateParams{MediaType: media.Type(1),
				SectorSize: 512, UserSectors: uint64(b.N) + 1, Application: "bench"}, &o)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(512)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				off := i % 1024 * 512
				if err := img.WriteSector(uint64(i), false, data[off:off+512], SectorStatusDumped); err != nil {
					b.Fatal(err)
				}
			}
			if err := img.Close(); err != nil {
				b.Fatal(err)
			}
		})
	}
}
