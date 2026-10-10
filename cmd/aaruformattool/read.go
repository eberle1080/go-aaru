package main

import (
	"strconv"

	"github.com/eberle1080/go-aaru"
)

// cmdRead implements "read <sector> <filename>".
func cmdRead(args []string) int { return cmdReadCommon(args, false) }

// cmdReadLong implements "read_long <sector> <filename>".
func cmdReadLong(args []string) int { return cmdReadCommon(args, true) }

// cmdReadCommon parses the arguments of read and read_long: a
// non-negative sector number and the image path.
func cmdReadCommon(args []string, long bool) int {
	verb, usage := "read", usageRead
	if long {
		verb, usage = "read_long", usageReadLong
	}
	pos, ok := newVerbArgs(verb, usage).parse(args, 2, 2)
	if !ok {
		return -1
	}
	// The sector number may be decimal or have a 0x, 0o or 0b prefix.
	sector, err := strconv.ParseUint(pos[0], 0, 64)
	if err != nil {
		usage()
		return -1
	}
	return withCoreOutput(func(o *coreOutput) int { return readSector(o, sector, pos[1], long) })
}

// readSector prints the size and a hex dump of a sector.
func readSector(o *coreOutput, sector uint64, path string, long bool) int {
	img, err := aaru.Open(path)
	if err != nil {
		code := coreOpenErrno(err)
		o.printf("Error %d when opening AaruFormat image.\n", code)
		return code
	}
	defer img.Close()

	var data []byte
	if long {
		data, _, err = img.ReadSectorLong(sector, false)
	} else {
		data, _, err = img.ReadSector(sector, false)
	}
	if err != nil {
		// The size and the data are read at once, so every failure is
		// reported as a failure to determine the sector size.
		code := aaru.ErrorCode(err)
		o.printf("Error %d when guessing sector size.\n", code)
		return code
	}

	o.printf("Sector is %d bytes.\n", len(data))
	printhex(o.out, data, 16, true)
	return 0
}
