// Package aaru reads and writes AaruFormat disk images.
//
// AaruFormat is the native image format of the Aaru Data Preservation Suite.
// It stores the user data of a medium (floppy and hard disks, optical discs,
// tapes, flash media, ...) in compressed, deduplicated blocks together with
// everything needed to reproduce the original medium: per-sector tags (CD
// sync/headers/ECC, subchannel, DVD IED/EDC, Apple tags), whole-media tags
// (TOC, PFI, ATIP, ...), tracks, geometry, dump hardware, checksums, flux
// captures, optional erasure coding parity and console disc encryption
// metadata.
//
// This package is a pure Go implementation of the format. Images it writes
// can be read by other AaruFormat implementations, and it reads images
// written by them.
//
// Reading:
//
//	img, err := aaru.Open("disk.aif")
//	if err != nil { ... }
//	defer img.Close()
//	data, status, err := img.ReadSector(0, false)
//
// Writing:
//
//	opts := aaru.DefaultOptions()
//	img, err := aaru.Create("disk.aif", aaru.CreateParams{
//		MediaType:   media.DOS_35_HD,
//		SectorSize:  512,
//		UserSectors: 2880,
//		Application: "myapp",
//	}, &opts)
//	...
//	err = img.WriteSector(lba, false, sector, aaru.SectorStatusDumped)
//	...
//	err = img.Close() // finalizes the image
//
// Building blocks used by the format are available as separate packages:
// crc64 (CRC64-ECMA), spamsum, compression/lzma, compression/zstd,
// compression/flac, compression/cst, cdecc (CD EDC/ECC), lisatag (Apple Lisa
// sector tags), media (media types) and console/... (PS3, Wii U and
// GameCube/Wii disc crypto). The fs/iso9660 package reads the ISO 9660 file
// system of a disc image through Image.UserDataReaderAt.
package aaru
