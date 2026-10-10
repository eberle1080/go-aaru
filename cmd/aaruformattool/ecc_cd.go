package main

import (
	"encoding/binary"

	"github.com/eberle1080/go-aaru/cdecc"
)

// cdSectorCheck is the detailed result of checkCDSectorChannel.
type cdSectorCheck struct {
	unknown     bool // not a recognizable data sector (no sync, short buffer)
	hasEDC      bool
	edcCorrect  bool
	hasECCP     bool
	eccPCorrect bool
	hasECCQ     bool
	eccQCorrect bool
}

// checkCDSectorChannel verifies the EDC and ECC of a raw 2352-byte CD sector.
// It returns whether the sector is correct; details are in the returned
// cdSectorCheck and diagnostics are written with warn.
//
//   - Mode 2 Form 1 ECC is checked with an address of four zero bytes.
//   - The Mode 2 EDCs are compared against the 32-bit little-endian stored
//     value.
//   - The Mode 2 Form 2 EDC covers 0x010-0x92B and is stored at 0x92C, where
//     0 means "no EDC".
//   - Buffers shorter than a raw sector are reported as unknown.
func checkCDSectorChannel(sector []byte, warn func(format string, a ...any)) (bool, cdSectorCheck) {
	var r cdSectorCheck
	if len(sector) < cdecc.SectorSize {
		r.unknown = true
		return false, r
	}
	if sector[0x000] != 0x00 || sector[0x00B] != 0x00 {
		r.unknown = true
		return false, r
	}
	for i := 0x001; i <= 0x00A; i++ {
		if sector[i] != 0xFF {
			r.unknown = true
			return false, r
		}
	}

	m, s, f := sector[0x00C], sector[0x00D], sector[0x00E]
	le := binary.LittleEndian

	switch sector[0x00F] & 0x03 {
	case 0x00: // Mode 0
		for i := 0x010; i < 0x930; i++ {
			if sector[i] != 0x00 {
				warn("Mode 0 sector with error at address: %2X:%2X:%2X.\n", m, s, f)
				return false, r
			}
		}
		return true, r

	case 0x01: // Mode 1
		for i := 0x814; i <= 0x81B; i++ {
			if sector[i] != 0x00 {
				warn("Mode 1 with data in reserved bytes at address: %2X:%2X:%2X.\n", m, s, f)
				return false, r
			}
		}
		r.hasEDC, r.hasECCP, r.hasECCQ = true, true, true
		r.eccPCorrect = cdecc.CheckECC(sector[0x00C:], sector[0x010:], sector[0x81C:], cdecc.ECCP)
		r.eccQCorrect = cdecc.CheckECC(sector[0x00C:], sector[0x010:], sector[0x8C8:], cdecc.ECCQ)
		stored := le.Uint32(sector[0x810:])
		calculated := cdecc.ComputeEDC(0, sector[:0x810])
		r.edcCorrect = calculated == stored
		if !r.edcCorrect {
			warn("Mode 1 sector at address: %2X:%2X:%2X, got CRC 0x%8X expected 0x%8X\n", m, s, f, calculated,
				stored)
		}
		if !r.eccPCorrect {
			warn("Mode 1 sector at address: %2X:%2X:%2X, fails ECC P check.\n", m, s, f)
		}
		if !r.eccQCorrect {
			warn("Mode 1 sector at address: %2X:%2X:%2X, fails ECC Q check.\n", m, s, f)
		}
		return r.edcCorrect && r.eccPCorrect && r.eccQCorrect, r

	case 0x02: // Mode 2
		if sector[0x012]&0x20 == 0x20 { // Form 2
			if sector[0x010] != sector[0x014] || sector[0x011] != sector[0x015] ||
				sector[0x012] != sector[0x016] || sector[0x013] != sector[0x017] {
				warn("Subheader copies differ in mode 2 form 2 sector at address: %2X:%2X:%2X\n", m, s, f)
			}
			stored := le.Uint32(sector[0x92C:])
			if stored == 0 { // the Form 2 EDC is optional
				return true, r
			}
			r.hasEDC = true
			calculated := cdecc.ComputeEDC(0, sector[0x010:0x92C])
			r.edcCorrect = calculated == stored
			if !r.edcCorrect {
				warn("Mode 2 sector at address: %2X:%2X:%2X, got CRC 0x%8X expected 0x%8X\n", m, s, f,
					calculated, stored)
			}
			return r.edcCorrect, r
		}

		// Form 1: the ECC covers a zeroed header.
		var zeroAddress [4]byte
		r.hasEDC, r.hasECCP, r.hasECCQ = true, true, true
		r.eccPCorrect = cdecc.CheckECC(zeroAddress[:], sector[0x010:], sector[0x81C:], cdecc.ECCP)
		r.eccQCorrect = cdecc.CheckECC(zeroAddress[:], sector[0x010:], sector[0x8C8:], cdecc.ECCQ)
		stored := le.Uint32(sector[0x818:])
		calculated := cdecc.ComputeEDC(0, sector[0x010:0x818])
		r.edcCorrect = calculated == stored
		if !r.edcCorrect {
			warn("Mode 2 sector at address: %2X:%2X:%2X, got CRC 0x%8X expected 0x%8X\n", m, s, f, calculated,
				stored)
		}
		if !r.eccPCorrect {
			warn("Mode 2 sector at address: %2X:%2X:%2X, fails ECC P check.\n", m, s, f)
		}
		if !r.eccQCorrect {
			warn("Mode 2 sector at address: %2X:%2X:%2X, fails ECC Q check.\n", m, s, f)
		}
		return r.edcCorrect && r.eccPCorrect && r.eccQCorrect, r
	}

	warn("Unknown mode %d sector at address: %2X:%2X:%2X\n", sector[0x00F], m, s, f)
	return false, r
}
