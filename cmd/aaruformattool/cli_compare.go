package main

import (
	"bytes"

	"github.com/eberle1080/go-aaru"
)

// cmdCLICompare implements "cli-compare [-l] <filename1> <filename2>".
func cmdCLICompare(args []string) int {
	va := newVerbArgs("cli-compare", usageCLICompare)
	useLong := va.fs.Bool("l", false, "Use long sector read/write (includes tags and metadata)")
	pos, ok := va.parse(args, 2, 2)
	if !ok {
		return -1
	}
	return withCoreOutput(func(o *coreOutput) int { return cliCompare(o, pos[0], pos[1], *useLong) })
}

// cliCompare compares two images sector by sector and lists the sectors that
// differ. It returns 0 if all compared sectors are identical,
// 1 if some differ and -1 on errors.
func cliCompare(o *coreOutput, path1, path2 string, useLong bool) int {
	o.printf("Opening first image: %s\n", path1)
	img1, err := aaru.Open(path1)
	if err != nil {
		o.eprintf("Error: Could not open first image '%s'\n", path1)
		return -1
	}
	defer img1.Close()

	o.printf("Opening second image: %s\n", path2)
	img2, err := aaru.Open(path2)
	if err != nil {
		o.eprintf("Error: Could not open second image '%s'\n", path2)
		return -1
	}
	defer img2.Close()

	info1, info2 := img1.Info(), img2.Info()
	o.printf("\nImage Information:\n")
	o.printf("Image 1: %d sectors, %d bytes per sector\n", info1.Sectors, info1.SectorSize)
	o.printf("Image 2: %d sectors, %d bytes per sector\n", info2.Sectors, info2.SectorSize)
	if useLong {
		o.printf("Mode: Long sector read (including tags and metadata)\n")
	} else {
		o.printf("Mode: Normal sector read\n")
	}

	total := info1.Sectors
	if info1.Sectors != info2.Sectors {
		o.eprintf("Warning: Images have different number of sectors\n")
		total = min(info1.Sectors, info2.Sectors)
		o.printf("Will compare first %d sectors\n", total)
	}

	if info1.SectorSize != info2.SectorSize {
		o.eprintf("Error: Images have different sector sizes (%d vs %d)\n", info1.SectorSize, info2.SectorSize)
		return -1
	}

	o.printf("\nStarting sector-by-sector comparison...\n")
	o.printf("Progress: 0%% (0/%d sectors)\r", total)
	o.flush()

	read := func(img *aaru.Image, sector uint64) ([]byte, aaru.SectorStatus, error) {
		if useLong {
			return img.ReadSectorLong(sector, false)
		}
		return img.ReadSector(sector, false)
	}

	var different, processed uint64
	for sector := uint64(0); sector < total; sector++ {
		data1, status1, err1 := read(img1, sector)
		data2, status2, err2 := read(img2, sector)
		available1, available2 := err1 == nil, err2 == nil

		var differs bool
		switch {
		case !available1 && !available2:
			// Both sectors are unavailable: consider them the same.
		case available1 != available2:
			differs = true
		case status1 != status2:
			differs = true
		default:
			differs = !bytes.Equal(data1, data2)
		}

		if differs {
			o.printf("Sector %d: DIFFERENT", sector)
			switch {
			case !available1:
				o.printf(" (missing in image 1)")
			case !available2:
				o.printf(" (missing in image 2)")
			case len(data1) != len(data2):
				o.printf(" (different sizes: %d vs %d)", len(data1), len(data2))
			}
			o.printf("\n")
			different++
		}

		processed++
		if processed%1000 == 0 || sector == total-1 {
			o.printf("Progress: %d%% (%d/%d sectors)\r", processed*100/total, processed, total)
			o.flush()
		}
	}

	o.printf("\n\nComparison completed!\n")
	o.printf("Total sectors compared: %d\n", total)
	o.printf("Different sectors: %d\n", different)
	o.printf("Identical sectors: %d\n", total-different)
	if different == 0 {
		o.printf("✓ Images are identical!\n")
		return 0
	}
	o.printf("✗ Images are different (%d sectors differ)\n", different)
	return 1
}
