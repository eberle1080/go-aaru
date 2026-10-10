package main

import (
	"errors"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// cmdVerify implements "verify [--recover] <filename>".
func cmdVerify(args []string) int { return cmdVerifyCommon(args, false) }

// cmdVerifySectors implements "verify_sectors <filename>".
func cmdVerifySectors(args []string) int { return cmdVerifyCommon(args, true) }

// cmdVerifyCommon parses the arguments of verify and verify_sectors.
// --recover is accepted by both but only used by verify.
func cmdVerifyCommon(args []string, sectorsMode bool) int {
	verb, usage := "verify", usageVerify
	if sectorsMode {
		verb, usage = "verify_sectors", usageVerifySectors
	}
	va := newVerbArgs(verb, usage)
	recover := va.fs.Bool("recover", false, "Attempt erasure coding recovery of corrupt blocks")
	pos, ok := va.parse(args, 1, 1)
	if !ok {
		return -1
	}
	return withCoreOutput(func(o *coreOutput) int {
		switch {
		case *recover && !sectorsMode:
			return verifyRecover(o, pos[0])
		case sectorsMode:
			return verifySectors(o, pos[0])
		default:
			return verifyImage(o, pos[0])
		}
	})
}

// verifyImage checks the CRC64 of every block of the image.
func verifyImage(o *coreOutput, path string) int {
	img, err := aaru.Open(path)
	if err != nil {
		code := coreOpenErrno(err)
		o.printf("Error %d when opening AaruFormat image.\n", code)
		return code
	}
	defer img.Close()

	err = img.Verify()
	switch {
	case err == nil:
		o.printf("Image blocks contain no errors.\n")
	case errors.Is(err, aaru.ErrInvalidBlockCRC):
		o.printf("A block contains an invalid CRC value.\n")
	default:
		o.printf("Error %d verifying image.\n", aaru.ErrorCode(err))
	}
	return aaru.ErrorCode(err)
}

// verifyRecover reads every sector of an image with erasure coding, letting
// the library reconstruct damaged blocks, and reports the sectors that cannot
// be read.
func verifyRecover(o *coreOutput, path string) int {
	img, err := aaru.Open(path)
	if err != nil {
		code := coreOpenErrno(err)
		o.printf("Error %d when opening AaruFormat image.\n", code)
		return code
	}
	defer img.Close()

	ec, ok := img.ErasureCoding()
	if !ok {
		o.printf("Image does not contain erasure coding recovery data.\n")
		return -1
	}
	o.printf("Erasure coding recovery data found (K=%d, M=%d).\n", ec.K, ec.M)
	o.printf("Verifying all sectors with recovery...\n")

	total := img.Info().Sectors
	var okCount, errorCount uint64
	for sector := uint64(0); sector < total; sector++ {
		if _, _, err := img.ReadSector(sector, false); err == nil {
			okCount++
		} else {
			errorCount++
			o.printf("  Sector %d: unrecoverable (error %d)\n", sector, aaru.ErrorCode(err))
		}
		if sector > 0 && sector%10000 == 0 {
			o.printf("  Progress: %d / %d sectors...\r", sector, total)
			o.flush()
		}
	}

	// Recovery happens transparently inside ReadSector, so recovered sectors
	// cannot be told apart from intact ones.
	o.printf("\nVerification complete:\n")
	o.printf("  Total sectors:   %d\n", total)
	o.printf("  Readable:        %d\n", okCount)
	o.printf("  Unrecoverable:   %d\n", errorCount)
	if errorCount == 0 {
		o.printf("All sectors readable (recovery applied transparently where needed).\n")
		return 0
	}
	o.printf("WARNING: %d sectors could not be recovered.\n", errorCount)
	return -1
}

// verifySectors checks the EDC/ECC of every sector of an optical disc image
// and reports EDC and ECC P/Q failures separately.
func verifySectors(o *coreOutput, path string) int {
	img, err := aaru.Open(path)
	if err != nil {
		code := coreOpenErrno(err)
		o.printf("Error %d when opening AaruFormat image.\n", code)
		return code
	}
	defer img.Close()

	ii := img.Info()
	if ii.MetadataMediaType != media.OpticalDisc {
		o.printf("Image sectors do not contain checksums, cannot verify.\n")
		return 0
	}

	warn := func(format string, a ...any) { o.eprintf(format, a...) }
	var errorsFound, unknowns uint64
	anyError := false
	for s := uint64(0); s < ii.Sectors; s++ {
		o.printf("\rVerifying sector %d...", s)
		sector, _, err := img.ReadSectorLong(s, false)
		if err != nil {
			o.eprintf("\rError %d reading sector %d.\n", aaru.ErrorCode(err), s)
			continue
		}

		ok, r := checkCDSectorChannel(sector, warn)
		if ok {
			continue
		}
		if r.unknown {
			unknowns++
			o.printf("\rSector %d cannot be verified.\n", s)
			continue
		}
		if r.hasEDC && !r.edcCorrect {
			o.printf("\rSector %d has an incorrect EDC value.\n", s)
		}
		if r.hasECCP && !r.eccPCorrect {
			o.printf("\rSector %d has an incorrect ECC P value.\n", s)
		}
		if r.hasECCQ && !r.eccQCorrect {
			o.printf("\rSector %d has an incorrect ECC Q value.\n", s)
		}
		errorsFound++
		anyError = true
	}

	if anyError {
		o.printf("\rSome sectors had incorrect checksums.\n")
	} else {
		o.printf("\rAll sector checksums are correct.\n")
	}
	o.printf("Total sectors........... %d\n", ii.Sectors)
	o.printf("Total errors............ %d\n", errorsFound)
	o.printf("Total unknowns.......... %d\n", unknowns)
	o.printf("Total errors+unknowns... %d\n", errorsFound+unknowns)
	return 0
}
