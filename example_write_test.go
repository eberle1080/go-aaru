package aaru_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// Create a 1.44 MB floppy image, write its sectors and finalize it.
func ExampleCreate() {
	dir, err := os.MkdirTemp("", "go-aaru-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	opts := aaru.ParseOptions("compress=true;deduplicate=true;md5=true")
	img, err := aaru.Create(filepath.Join(dir, "floppy.aif"), aaru.CreateParams{
		MediaType:   media.Type(1),
		SectorSize:  512,
		UserSectors: 2880,
		Application: "example",
	}, &opts)
	if err != nil {
		log.Fatal(err)
	}

	sector := make([]byte, 512)
	for lba := uint64(0); lba < 2880; lba++ {
		sector[0] = byte(lba)
		if err := img.WriteSector(lba, false, sector, aaru.SectorStatusDumped); err != nil {
			log.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(img.Info().Sectors, img.Info().SectorSize)
	// Output: 2880 512
}
