// Media type constants of the AaruFormat format.

package media

import "fmt"

// Type identifies the kind of physical medium an image represents.
type Type uint32

const (
	// UnknownMedia: Unknown disk type
	UnknownMedia Type = 0
	// UnknownMO: Unknown magneto-optical
	UnknownMO Type = 1
	// GENERIC_HDD: Generic hard disk
	GENERIC_HDD Type = 2
	// Microdrive: Microdrive type hard disk
	Microdrive Type = 3
	// Zone_HDD: Zoned hard disk
	Zone_HDD Type = 4
	// FlashDrive: USB flash drives
	FlashDrive Type = 5
	// CD: Any unknown or standard violating CD
	CD Type = 10
	// CDDA: CD Digital Audio (Red Book)
	CDDA Type = 11
	// CDG: CD+G (Red Book)
	CDG Type = 12
	// CDEG: CD+EG (Red Book)
	CDEG Type = 13
	// CDI: CD-i (Green Book)
	CDI Type = 14
	// CDROM: CD-ROM (Yellow Book)
	CDROM Type = 15
	// CDROMXA: CD-ROM XA (Yellow Book)
	CDROMXA Type = 16
	// CDPLUS: CD+ (Blue Book)
	CDPLUS Type = 17
	// CDMO: CD-MO (Orange Book)
	CDMO Type = 18
	// CDR: CD-Recordable (Orange Book)
	CDR Type = 19
	// CDRW: CD-ReWritable (Orange Book)
	CDRW Type = 20
	// CDMRW: Mount-Rainier CD-RW
	CDMRW Type = 21
	// VCD: Video CD (White Book)
	VCD Type = 22
	// SVCD: Super Video CD (White Book)
	SVCD Type = 23
	// PCD: Photo CD (Beige Book)
	PCD Type = 24
	// SACD: Super Audio CD (Scarlet Book)
	SACD Type = 25
	// DDCD: Double-Density CD-ROM (Purple Book)
	DDCD Type = 26
	// DDCDR: DD CD-R (Purple Book)
	DDCDR Type = 27
	// DDCDRW: DD CD-RW (Purple Book)
	DDCDRW Type = 28
	// DTSCD: DTS audio CD (non-standard)
	DTSCD Type = 29
	// CDMIDI: CD-MIDI (Red Book)
	CDMIDI Type = 30
	// CDV: CD-Video (ISO/IEC 61104)
	CDV Type = 31
	// PD650: 120mm, Phase-Change, 1298496 sectors, 512 bytes/sector, PD650, ECMA-240, ISO 15485
	PD650 Type = 32
	// PD650_WORM: 120mm, Write-Once, 1281856 sectors, 512 bytes/sector, PD650, ECMA-240, ISO 15485
	PD650_WORM Type = 33
	// CDIREADY: CD-i Ready, contains a track before the first TOC track, in mode 2, and all TOC tracks are Audio. Subchannel marks track as audio pause.
	CDIREADY Type = 34
	// FMTOWNS: Fujitsu FM Towns bootable CD (mixed-mode proprietary extensions)
	FMTOWNS Type = 35
	// DVDROM: DVD-ROM (applies to DVD Video and DVD Audio)
	DVDROM Type = 40
	// DVDR: DVD-R
	DVDR Type = 41
	// DVDRW: DVD-RW
	DVDRW Type = 42
	// DVDPR: DVD+R
	DVDPR Type = 43
	// DVDPRW: DVD+RW
	DVDPRW Type = 44
	// DVDPRWDL: DVD+RW DL
	DVDPRWDL Type = 45
	// DVDRDL: DVD-R DL
	DVDRDL Type = 46
	// DVDPRDL: DVD+R DL
	DVDPRDL Type = 47
	// DVDRAM: DVD-RAM
	DVDRAM Type = 48
	// DVDRWDL: DVD-RW DL
	DVDRWDL Type = 49
	// DVDDownload: DVD-Download
	DVDDownload Type = 50
	// HDDVDROM: HD DVD-ROM (applies to HD DVD Video)
	HDDVDROM Type = 51
	// HDDVDRAM: HD DVD-RAM
	HDDVDRAM Type = 52
	// HDDVDR: HD DVD-R
	HDDVDR Type = 53
	// HDDVDRW: HD DVD-RW
	HDDVDRW Type = 54
	// HDDVDRDL: HD DVD-R DL
	HDDVDRDL Type = 55
	// HDDVDRWDL: HD DVD-RW DL
	HDDVDRWDL Type = 56
	// BDROM: BD-ROM (and BD Video)
	BDROM Type = 60
	// BDR: BD-R
	BDR Type = 61
	// BDRE: BD-RE
	BDRE Type = 62
	// BDRXL: BD-R XL
	BDRXL Type = 63
	// BDREXL: BD-RE XL
	BDREXL Type = 64
	// UHDBD: Ultra HD Blu-ray
	UHDBD Type = 65
	// EVD: Enhanced Versatile Disc
	EVD Type = 70
	// FVD: Forward Versatile Disc
	FVD Type = 71
	// HVD: Holographic Versatile Disc
	HVD Type = 72
	// CBHD: China Blue High Definition
	CBHD Type = 73
	// HDVMD: High Definition Versatile Multilayer Disc
	HDVMD Type = 74
	// VCDHD: Versatile Compact Disc High Density
	VCDHD Type = 75
	// SVOD: Stacked Volumetric Optical Disc
	SVOD Type = 76
	// FDDVD: Five Dimensional disc
	FDDVD Type = 77
	// LD: Pioneer LaserDisc
	LD Type = 80
	// LDROM: Pioneer LaserDisc data
	LDROM  Type = 81
	LDROM2 Type = 82
	LVROM  Type = 83
	MegaLD Type = 84
	// HiMD: Sony Hi-MD
	HiMD Type = 90
	// MD: Sony MiniDisc
	MD Type = 91
	// MDData: MiniDisc DATA (HiFD style data-only variant)
	MDData Type = 92
	// MDData2: High-capacity MiniDisc DATA 2
	MDData2 Type = 93
	// MD60: Sony MiniDisc, 60 minutes, formatted with Hi-MD format
	MD60 Type = 94
	// MD74: Sony MiniDisc, 74 minutes, formatted with Hi-MD format
	MD74 Type = 95
	// MD80: Sony MiniDisc, 80 minutes, formatted with Hi-MD format
	MD80 Type = 96
	// UDO: 5.25", Phase-Change, 1834348 sectors, 8192 bytes/sector, Ultra Density Optical, ECMA-350, ISO 17345
	UDO Type = 100
	// UDO2: 5.25", Phase-Change, 3669724 sectors, 8192 bytes/sector, Ultra Density Optical 2, ECMA-380, ISO 11976
	UDO2 Type = 101
	// UDO2_WORM: 5.25", Write-Once, 3668759 sectors, 8192 bytes/sector, Ultra Density Optical 2, ECMA-380, ISO 11976
	UDO2_WORM Type = 102
	// PlayStationMemoryCard: Sony PlayStation (PS1) memory card (128 KiB, 8 KB blocks)
	PlayStationMemoryCard Type = 110
	// PlayStationMemoryCard2: Sony PlayStation 2 memory card (MagicGate, 8 MiB)
	PlayStationMemoryCard2 Type = 111
	// PS1CD: Sony PlayStation game CD
	PS1CD Type = 112
	// PS2CD: Sony PlayStation 2 game CD
	PS2CD Type = 113
	// PS2DVD: Sony PlayStation 2 game DVD
	PS2DVD Type = 114
	// PS3DVD: Sony PlayStation 3 game DVD
	PS3DVD Type = 115
	// PS3BD: Sony PlayStation 3 game Blu-ray
	PS3BD Type = 116
	// PS4BD: Sony PlayStation 4 game Blu-ray
	PS4BD Type = 117
	// UMD: Sony PlayStation Portable Universal Media Disc (ECMA-365)
	UMD Type = 118
	// PlayStationVitaGameCard: PS Vita NV memory card (proprietary flash)
	PlayStationVitaGameCard Type = 119
	// PS5BD: Sony PlayStation 5 game Blu-ray
	PS5BD Type = 120
	// XGD: Microsoft X-box Game Disc
	XGD Type = 130
	// XGD2: Microsoft X-box 360 Game Disc
	XGD2 Type = 131
	// XGD3: Microsoft X-box 360 Game Disc
	XGD3 Type = 132
	// XGD4: Microsoft X-box One Game Disc
	XGD4 Type = 133
	// MEGACD: Sega MegaCD
	MEGACD Type = 150
	// SATURNCD: Sega Saturn disc
	SATURNCD Type = 151
	// GDROM: Sega/Yamaha Gigabyte Disc
	GDROM Type = 152
	// GDR: Sega/Yamaha recordable Gigabyte Disc
	GDR Type = 153
	// SegaCard: Sega My Card / Sega Card (SG-1000 / Mark III)
	SegaCard Type = 154
	// MilCD: Sega Dreamcast MIL-CD enhanced multimedia disc
	MilCD Type = 155
	// HuCard: PC-Engine / TurboGrafx cartridge
	HuCard Type = 170
	// SuperCDROM2: PC-Engine / TurboGrafx CD
	SuperCDROM2 Type = 171
	// JaguarCD: Atari Jaguar CD
	JaguarCD Type = 172
	// ThreeDO: 3DO CD
	ThreeDO Type = 173
	// PCFX: NEC PC-FX
	PCFX Type = 174
	// NeoGeoCD: NEO-GEO CD
	NeoGeoCD Type = 175
	// CDTV: Commodore CDTV
	CDTV Type = 176
	// CD32: Amiga CD32
	CD32 Type = 177
	// Nuon: Nuon (DVD based videogame console)
	Nuon Type = 178
	// Playdia: Bandai Playdia
	Playdia Type = 179
	// Apple32SS: 5.25", SS, DD, 35 tracks, 13 spt, 256 bytes/sector, GCR
	Apple32SS Type = 180
	// Apple32DS: 5.25", DS, DD, 35 tracks, 13 spt, 256 bytes/sector, GCR
	Apple32DS Type = 181
	// Apple33SS: 5.25", SS, DD, 35 tracks, 16 spt, 256 bytes/sector, GCR
	Apple33SS Type = 182
	// Apple33DS: 5.25", DS, DD, 35 tracks, 16 spt, 256 bytes/sector, GCR
	Apple33DS Type = 183
	// AppleSonySS: 3.5", SS, DD, 80 tracks, 8 to 12 spt, 512 bytes/sector, GCR
	AppleSonySS Type = 184
	// AppleSonyDS: 3.5", DS, DD, 80 tracks, 8 to 12 spt, 512 bytes/sector, GCR
	AppleSonyDS Type = 185
	// AppleFileWare: 5.25", DS, ?D, ?? tracks, ?? spt, 512 bytes/sector, GCR, opposite side heads, aka Twiggy
	AppleFileWare Type = 186
	// DOS_525_SS_DD_8: 5.25", SS, DD, 40 tracks, 8 spt, 512 bytes/sector, MFM
	DOS_525_SS_DD_8 Type = 190
	// DOS_525_SS_DD_9: 5.25", SS, DD, 40 tracks, 9 spt, 512 bytes/sector, MFM
	DOS_525_SS_DD_9 Type = 191
	// DOS_525_DS_DD_8: 5.25", DS, DD, 40 tracks, 8 spt, 512 bytes/sector, MFM
	DOS_525_DS_DD_8 Type = 192
	// DOS_525_DS_DD_9: 5.25", DS, DD, 40 tracks, 9 spt, 512 bytes/sector, MFM
	DOS_525_DS_DD_9 Type = 193
	// DOS_525_HD: 5.25", DS, HD, 80 tracks, 15 spt, 512 bytes/sector, MFM
	DOS_525_HD Type = 194
	// DOS_35_SS_DD_8: 3.5", SS, DD, 80 tracks, 8 spt, 512 bytes/sector, MFM
	DOS_35_SS_DD_8 Type = 195
	// DOS_35_SS_DD_9: 3.5", SS, DD, 80 tracks, 9 spt, 512 bytes/sector, MFM
	DOS_35_SS_DD_9 Type = 196
	// DOS_35_DS_DD_8: 3.5", DS, DD, 80 tracks, 8 spt, 512 bytes/sector, MFM
	DOS_35_DS_DD_8 Type = 197
	// DOS_35_DS_DD_9: 3.5", DS, DD, 80 tracks, 9 spt, 512 bytes/sector, MFM
	DOS_35_DS_DD_9 Type = 198
	// DOS_35_HD: 3.5", DS, HD, 80 tracks, 18 spt, 512 bytes/sector, MFM
	DOS_35_HD Type = 199
	// DOS_35_ED: 3.5", DS, ED, 80 tracks, 36 spt, 512 bytes/sector, MFM
	DOS_35_ED Type = 200
	// DMF: 3.5", DS, HD, 80 tracks, 21 spt, 512 bytes/sector, MFM
	DMF Type = 201
	// DMF_82: 3.5", DS, HD, 82 tracks, 21 spt, 512 bytes/sector, MFM
	DMF_82 Type = 202
	// XDF_525: 5.25", DS, HD, 80 tracks, ? spt, ??? + ??? + ??? bytes/sector, MFM track 0 = ??15 sectors, 512 bytes/sector, falsified to DOS as 19 spt, 512 bps
	XDF_525 Type = 203
	// XDF_35: 3.5", DS, HD, 80 tracks, 4 spt, 8192 + 2048 + 1024 + 512 bytes/sector, MFM track 0 = 19 sectors, 512 bytes/sector, falsified to DOS as 23 spt, 512 bps
	XDF_35 Type = 204
	// IBM23FD: 8", SS, SD, 32 tracks, 8 spt, 319 bytes/sector, FM
	IBM23FD Type = 210
	// IBM33FD_128: 8", SS, SD, 73 tracks, 26 spt, 128 bytes/sector, FM
	IBM33FD_128 Type = 211
	// IBM33FD_256: 8", SS, SD, 74 tracks, 15 spt, 256 bytes/sector, FM, track 0 = 26 sectors, 128 bytes/sector
	IBM33FD_256 Type = 212
	// IBM33FD_512: 8", SS, SD, 74 tracks, 8 spt, 512 bytes/sector, FM, track 0 = 26 sectors, 128 bytes/sector
	IBM33FD_512 Type = 213
	// IBM43FD_128: 8", DS, SD, 74 tracks, 26 spt, 128 bytes/sector, FM, track 0 = 26 sectors, 128 bytes/sector
	IBM43FD_128 Type = 214
	// IBM43FD_256: 8", DS, SD, 74 tracks, 26 spt, 256 bytes/sector, FM, track 0 = 26 sectors, 128 bytes/sector
	IBM43FD_256 Type = 215
	// IBM53FD_256: 8", DS, DD, 74 tracks, 26 spt, 256 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	IBM53FD_256 Type = 216
	// IBM53FD_512: 8", DS, DD, 74 tracks, 15 spt, 512 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	IBM53FD_512 Type = 217
	// IBM53FD_1024: 8", DS, DD, 74 tracks, 8 spt, 1024 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	IBM53FD_1024 Type = 218
	// RX01: 8", SS, DD, 77 tracks, 26 spt, 128 bytes/sector, FM
	RX01 Type = 220
	// RX02: 8", SS, DD, 77 tracks, 26 spt, 256 bytes/sector, FM/MFM
	RX02 Type = 221
	// RX03: 8", DS, DD, 77 tracks, 26 spt, 256 bytes/sector, FM/MFM
	RX03 Type = 222
	// RX50: 5.25", SS, DD, 80 tracks, 10 spt, 512 bytes/sector, MFM
	RX50 Type = 223
	// ACORN_525_SS_SD_40: 5,25", SS, SD, 40 tracks, 10 spt, 256 bytes/sector, FM
	ACORN_525_SS_SD_40 Type = 230
	// ACORN_525_SS_SD_80: 5,25", SS, SD, 80 tracks, 10 spt, 256 bytes/sector, FM
	ACORN_525_SS_SD_80 Type = 231
	// ACORN_525_SS_DD_40: 5,25", SS, DD, 40 tracks, 16 spt, 256 bytes/sector, MFM
	ACORN_525_SS_DD_40 Type = 232
	// ACORN_525_SS_DD_80: 5,25", SS, DD, 80 tracks, 16 spt, 256 bytes/sector, MFM
	ACORN_525_SS_DD_80 Type = 233
	// ACORN_525_DS_DD: 5,25", DS, DD, 80 tracks, 16 spt, 256 bytes/sector, MFM
	ACORN_525_DS_DD Type = 234
	// ACORN_35_DS_DD: 3,5", DS, DD, 80 tracks, 5 spt, 1024 bytes/sector, MFM
	ACORN_35_DS_DD Type = 235
	// ACORN_35_DS_HD: 3,5", DS, HD, 80 tracks, 10 spt, 1024 bytes/sector, MFM
	ACORN_35_DS_HD Type = 236
	// ATARI_525_SD: 5,25", SS, SD, 40 tracks, 18 spt, 128 bytes/sector, FM
	ATARI_525_SD Type = 240
	// ATARI_525_ED: 5,25", SS, ED, 40 tracks, 26 spt, 128 bytes/sector, MFM
	ATARI_525_ED Type = 241
	// ATARI_525_DD: 5,25", SS, DD, 40 tracks, 18 spt, 256 bytes/sector, MFM
	ATARI_525_DD Type = 242
	// ATARI_35_SS_DD: 3,5", SS, DD, 80 tracks, 10 spt, 512 bytes/sector, MFM
	ATARI_35_SS_DD Type = 243
	// ATARI_35_DS_DD: 3,5", DS, DD, 80 tracks, 10 spt, 512 bytes/sector, MFM
	ATARI_35_DS_DD Type = 244
	// ATARI_35_SS_DD_11: 3,5", SS, DD, 80 tracks, 11 spt, 512 bytes/sector, MFM
	ATARI_35_SS_DD_11 Type = 245
	// ATARI_35_DS_DD_11: 3,5", DS, DD, 80 tracks, 11 spt, 512 bytes/sector, MFM
	ATARI_35_DS_DD_11 Type = 246
	// CBM_35_DD: 3,5", DS, DD, 80 tracks, 10 spt, 512 bytes/sector, MFM (1581)
	CBM_35_DD Type = 250
	// CBM_AMIGA_35_DD: 3,5", DS, DD, 80 tracks, 11 spt, 512 bytes/sector, MFM (Amiga)
	CBM_AMIGA_35_DD Type = 251
	// CBM_AMIGA_35_HD: 3,5", DS, HD, 80 tracks, 22 spt, 512 bytes/sector, MFM (Amiga)
	CBM_AMIGA_35_HD Type = 252
	// CBM_1540: 5,25", SS, DD, 35 tracks, GCR
	CBM_1540 Type = 253
	// CBM_1540_Ext: 5,25", SS, DD, 40 tracks, GCR
	CBM_1540_Ext Type = 254
	// CBM_1571: 5,25", DS, DD, 35 tracks, GCR
	CBM_1571 Type = 255
	// NEC_8_SD: 8", DS, SD, 77 tracks, 26 spt, 128 bytes/sector, FM
	NEC_8_SD Type = 260
	// NEC_8_DD: 8", DS, DD, 77 tracks, 26 spt, 256 bytes/sector, MFM
	NEC_8_DD Type = 261
	// NEC_525_SS: 5.25", SS, SD, 80 tracks, 16 spt, 256 bytes/sector, FM
	NEC_525_SS Type = 262
	// NEC_525_DS: 5.25", DS, SD, 80 tracks, 16 spt, 256 bytes/sector, MFM
	NEC_525_DS Type = 263
	// NEC_525_HD: 5,25", DS, HD, 77 tracks, 8 spt, 1024 bytes/sector, MFM
	NEC_525_HD Type = 264
	// NEC_35_HD_8: 3,5", DS, HD, 77 tracks, 8 spt, 1024 bytes/sector, MFM, aka mode 3
	NEC_35_HD_8 Type = 265
	// NEC_35_HD_15: 3,5", DS, HD, 80 tracks, 15 spt, 512 bytes/sector, MFM
	NEC_35_HD_15 Type = 266
	// NEC_35_TD: 3,5", DS, TD, 240 tracks, 38 spt, 512 bytes/sector, MFM
	NEC_35_TD Type = 267
	// SHARP_525: 5,25", DS, HD, 77 tracks, 8 spt, 1024 bytes/sector, MFM
	SHARP_525 Type = NEC_525_HD
	// SHARP_525_9: 3,5", DS, HD, 80 tracks, 9 spt, 1024 bytes/sector, MFM
	SHARP_525_9 Type = 268
	// SHARP_35: 3,5", DS, HD, 77 tracks, 8 spt, 1024 bytes/sector, MFM, aka mode 3
	SHARP_35 Type = NEC_35_HD_8
	// SHARP_35_9: 3,5", DS, HD, 80 tracks, 9 spt, 1024 bytes/sector, MFM
	SHARP_35_9 Type = 269
	// ECMA_99_8: 5,25", DS, DD, 80 tracks, 8 spt, 1024 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_99_8 Type = 270
	// ECMA_99_15: 5,25", DS, DD, 77 tracks, 15 spt, 512 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_99_15 Type = 271
	// ECMA_99_26: 5,25", DS, DD, 77 tracks, 26 spt, 256 bytes/sector, MFM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_99_26 Type = 272
	// ECMA_100: 3,5", DS, DD, 80 tracks, 9 spt, 512 bytes/sector, MFM
	ECMA_100 Type = DOS_35_DS_DD_9
	// ECMA_125: 3,5", DS, HD, 80 tracks, 18 spt, 512 bytes/sector, MFM
	ECMA_125 Type = DOS_35_HD
	// ECMA_147: 3,5", DS, ED, 80 tracks, 36 spt, 512 bytes/sector, MFM
	ECMA_147 Type = DOS_35_ED
	// ECMA_54: 8", SS, SD, 77 tracks, 26 spt, 128 bytes/sector, FM
	ECMA_54 Type = 273
	// ECMA_59: 8", DS, SD, 77 tracks, 26 spt, 128 bytes/sector, FM
	ECMA_59 Type = 274
	// ECMA_66: 5,25", SS, DD, 35 tracks, 9 spt, 256 bytes/sector, FM, track 0 side 0 = 16 sectors, 128 bytes/sector
	ECMA_66 Type = 275
	// ECMA_69_8: 8", DS, DD, 77 tracks, 8 spt, 1024 bytes/sector, FM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_69_8 Type = 276
	// ECMA_69_15: 8", DS, DD, 77 tracks, 15 spt, 512 bytes/sector, FM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_69_15 Type = 277
	// ECMA_69_26: 8", DS, DD, 77 tracks, 26 spt, 256 bytes/sector, FM, track 0 side 0 = 26 sectors, 128 bytes/sector, track 0 side 1 = 26 sectors, 256 bytes/sector
	ECMA_69_26 Type = 278
	// ECMA_70: 5,25", DS, DD, 40 tracks, 16 spt, 256 bytes/sector, FM, track 0 side 0 = 16 sectors, 128 bytes/sector, track 0 side 1 = 16 sectors, 256 bytes/sector
	ECMA_70 Type = 279
	ECMA_78 Type = 280
	// ECMA_78_2: 5,25", DS, DD, 80 tracks, 9 spt, 512 bytes/sector, FM
	ECMA_78_2 Type = 281
	// FDFORMAT_525_DD: 5,25", DS, DD, 82 tracks, 10 spt, 512 bytes/sector, MFM
	FDFORMAT_525_DD Type = 290
	// FDFORMAT_525_HD: 5,25", DS, HD, 82 tracks, 17 spt, 512 bytes/sector, MFM
	FDFORMAT_525_HD Type = 291
	// FDFORMAT_35_DD: 3,5", DS, DD, 82 tracks, 10 spt, 512 bytes/sector, MFM
	FDFORMAT_35_DD Type = 292
	// FDFORMAT_35_HD: 3,5", DS, HD, 82 tracks, 21 spt, 512 bytes/sector, MFM
	FDFORMAT_35_HD Type = 293
	// Apricot_35: 3.5", DS, DD, 70 tracks, 9 spt, 512 bytes/sector, MFM
	Apricot_35 Type = 309
	ADR2120    Type = 310
	ADR260     Type = 311
	ADR30      Type = 312
	ADR50      Type = 313
	AIT1       Type = 320
	AIT1Turbo  Type = 321
	AIT2       Type = 322
	AIT2Turbo  Type = 323
	AIT3       Type = 324
	AIT3Ex     Type = 325
	AIT3Turbo  Type = 326
	AIT4       Type = 327
	AIT5       Type = 328
	AITETurbo  Type = 329
	SAIT1      Type = 330
	SAIT2      Type = 331
	Bernoulli  Type = 340
	Bernoulli2 Type = 341
	Ditto      Type = 342
	DittoMax   Type = 343
	Jaz        Type = 344
	Jaz2       Type = 345
	PocketZip  Type = 346
	REV120     Type = 347
	REV35      Type = 348
	REV70      Type = 349
	ZIP100     Type = 350
	ZIP250     Type = 351
	ZIP750     Type = 352
	// Bernoulli35: 5⅓" Bernoulli Box II disk with 35Mb capacity
	Bernoulli35 Type = 353
	// Bernoulli44: 5⅓" Bernoulli Box II disk with 44Mb capacity
	Bernoulli44 Type = 354
	// Bernoulli65: 5⅓" Bernoulli Box II disk with 65Mb capacity
	Bernoulli65 Type = 355
	// Bernoulli90: 5⅓" Bernoulli Box II disk with 90Mb capacity
	Bernoulli90 Type = 356
	// Bernoulli105: 5⅓" Bernoulli Box II disk with 105Mb capacity
	Bernoulli105 Type = 357
	// Bernoulli150: 5⅓" Bernoulli Box II disk with 150Mb capacity
	Bernoulli150 Type = 358
	// Bernoulli230: 5⅓" Bernoulli Box II disk with 230Mb capacity
	Bernoulli230    Type = 359
	CompactCassette Type = 360
	Data8           Type = 361
	MiniDV          Type = 362
	// Dcas25: D/CAS-25: Digital data on Compact Cassette form factor, special magnetic media, 9-track
	Dcas25 Type = 363
	// Dcas85: D/CAS-85: Digital data on Compact Cassette form factor, special magnetic media, 17-track
	Dcas85 Type = 364
	// Dcas103: D/CAS-103: Digital data on Compact Cassette form factor, special magnetic media, 21-track
	Dcas103           Type = 365
	CFast             Type = 370
	CompactFlash      Type = 371
	CompactFlashType2 Type = 372
	DigitalAudioTape  Type = 380
	DAT160            Type = 381
	DAT320            Type = 382
	DAT72             Type = 383
	DDS1              Type = 384
	DDS2              Type = 385
	DDS3              Type = 386
	DDS4              Type = 387
	CompactTapeI      Type = 390
	CompactTapeII     Type = 391
	DECtapeII         Type = 392
	DLTtapeIII        Type = 393
	DLTtapeIIIxt      Type = 394
	DLTtapeIV         Type = 395
	DLTtapeS4         Type = 396
	SDLT1             Type = 397
	SDLT2             Type = 398
	VStapeI           Type = 399
	Exatape15m        Type = 400
	Exatape22m        Type = 401
	Exatape22mAME     Type = 402
	Exatape28m        Type = 403
	Exatape40m        Type = 404
	Exatape45m        Type = 405
	Exatape54m        Type = 406
	Exatape75m        Type = 407
	Exatape76m        Type = 408
	Exatape80m        Type = 409
	Exatape106m       Type = 410
	Exatape160mXL     Type = 411
	Exatape112m       Type = 412
	Exatape125m       Type = 413
	Exatape150m       Type = 414
	Exatape170m       Type = 415
	Exatape225m       Type = 416
	ExpressCard34     Type = 420
	ExpressCard54     Type = 421
	PCCardTypeI       Type = 422
	PCCardTypeII      Type = 423
	PCCardTypeIII     Type = 424
	PCCardTypeIV      Type = 425
	EZ135             Type = 430
	EZ230             Type = 431
	Quest             Type = 432
	SparQ             Type = 433
	SQ100             Type = 434
	SQ200             Type = 435
	SQ300             Type = 436
	SQ310             Type = 437
	SQ327             Type = 438
	SQ400             Type = 439
	SQ800             Type = 440
	SQ1500            Type = 441
	SQ2000            Type = 442
	SyJet             Type = 443
	// FamicomGamePak: Nintendo Famicom cartridge
	FamicomGamePak Type = 450
	// GameBoyAdvanceGamePak: Nintendo Game Boy Advance cartridge
	GameBoyAdvanceGamePak Type = 451
	// GameBoyGamePak: Nintendo Game Boy / Color cartridge
	GameBoyGamePak Type = 452
	// GOD: Nintendo GameCube Optical Disc
	GOD   Type = 453
	N64DD Type = 454
	// N64GamePak: Nintendo 64 cartridge
	N64GamePak Type = 455
	// NESGamePak: Nintendo NES cartridge
	NESGamePak Type = 456
	// Nintendo3DSGameCard: Nintendo 3DS ROM card
	Nintendo3DSGameCard Type = 457
	// NintendoDiskCard: Famicom Disk System disk
	NintendoDiskCard Type = 458
	// NintendoDSGameCard: Nintendo DS ROM card
	NintendoDSGameCard Type = 459
	// NintendoDSiGameCard: Nintendo DSi enhanced ROM card
	NintendoDSiGameCard Type = 460
	// SNESGamePak: Nintendo SNES (PAL/JPN) cartridge
	SNESGamePak Type = 461
	// SNESGamePakUS: Nintendo SNES (US) cartridge (different shell)
	SNESGamePakUS Type = 462
	// WOD: Nintendo Wii Optical Disc
	WOD Type = 463
	// WUOD: Nintendo Wii U Optical Disc
	WUOD Type = 464
	// SwitchGameCard: Nintendo Switch Game Card (NV flash)
	SwitchGameCard    Type = 465
	IBM3470           Type = 470
	IBM3480           Type = 471
	IBM3490           Type = 472
	IBM3490E          Type = 473
	IBM3592           Type = 474
	LTO               Type = 480
	LTO2              Type = 481
	LTO3              Type = 482
	LTO3WORM          Type = 483
	LTO4              Type = 484
	LTO4WORM          Type = 485
	LTO5              Type = 486
	LTO5WORM          Type = 487
	LTO6              Type = 488
	LTO6WORM          Type = 489
	LTO7              Type = 490
	LTO7WORM          Type = 491
	MemoryStick       Type = 510
	MemoryStickDuo    Type = 511
	MemoryStickMicro  Type = 512
	MemoryStickPro    Type = 513
	MemoryStickProDuo Type = 514
	// MicroSD: microSD / microSDHC / microSDXC card
	MicroSD Type = 520
	// MiniSD: miniSD card
	MiniSD Type = 521
	// SecureDigital: Full-size SD / SDHC / SDXC card
	SecureDigital Type = 522
	// MMC: MultiMediaCard (legacy)
	MMC Type = 530
	// MMCmicro: MMCmicro (RS-MMC form)
	MMCmicro Type = 531
	// RSMMC: Reduced Size MMC
	RSMMC Type = 532
	// MMCplus: MMCplus (high speed)
	MMCplus Type = 533
	// MMCmobile: MMCmobile (dual voltage)
	MMCmobile   Type = 534
	MLR1        Type = 540
	MLR1SL      Type = 541
	MLR3        Type = 542
	SLR1        Type = 543
	SLR2        Type = 544
	SLR3        Type = 545
	SLR32       Type = 546
	SLR32SL     Type = 547
	SLR4        Type = 548
	SLR5        Type = 549
	SLR5SL      Type = 550
	SLR6        Type = 551
	SLRtape7    Type = 552
	SLRtape7SL  Type = 553
	SLRtape24   Type = 554
	SLRtape24SL Type = 555
	SLRtape40   Type = 556
	SLRtape50   Type = 557
	SLRtape60   Type = 558
	SLRtape75   Type = 559
	SLRtape100  Type = 560
	SLRtape140  Type = 561
	QIC11       Type = 570
	QIC120      Type = 571
	QIC1350     Type = 572
	QIC150      Type = 573
	QIC24       Type = 574
	QIC3010     Type = 575
	QIC3020     Type = 576
	QIC3080     Type = 577
	QIC3095     Type = 578
	QIC320      Type = 579
	QIC40       Type = 580
	QIC525      Type = 581
	QIC80       Type = 582
	STK4480     Type = 590
	STK4490     Type = 591
	STK9490     Type = 592
	T9840A      Type = 593
	T9840B      Type = 594
	T9840C      Type = 595
	T9840D      Type = 596
	T9940A      Type = 597
	T9940B      Type = 598
	T10000A     Type = 599
	T10000B     Type = 600
	T10000C     Type = 601
	T10000D     Type = 602
	Travan1     Type = 610
	Travan1Ex   Type = 611
	Travan3     Type = 612
	Travan3Ex   Type = 613
	Travan4     Type = 614
	Travan5     Type = 615
	Travan7     Type = 616
	VXA1        Type = 620
	VXA2        Type = 621
	VXA3        Type = 622
	// ECMA_153: 5,25", M.O., ??? sectors, 1024 bytes/sector, ECMA-153, ISO 11560
	ECMA_153 Type = 630
	// ECMA_153_512: 5,25", M.O., ??? sectors, 512 bytes/sector, ECMA-153, ISO 11560
	ECMA_153_512 Type = 631
	// ECMA_154: 3,5", M.O., 249850 sectors, 512 bytes/sector, ECMA-154, ISO 10090
	ECMA_154 Type = 632
	// ECMA_183_512: 5,25", M.O., 904995 sectors, 512 bytes/sector, ECMA-183, ISO 13481
	ECMA_183_512 Type = 633
	// ECMA_183: 5,25", M.O., 498526 sectors, 1024 bytes/sector, ECMA-183, ISO 13481
	ECMA_183 Type = 634
	// ECMA_184_512: 5,25", M.O., 1128772 or 1163337 sectors, 512 bytes/sector, ECMA-183, ISO 13549
	ECMA_184_512 Type = 635
	// ECMA_184: 5,25", M.O., 603466 or 637041 sectors, 1024 bytes/sector, ECMA-183, ISO 13549
	ECMA_184 Type = 636
	// ECMA_189: 300mm, M.O., ??? sectors, 1024 bytes/sector, ECMA-189, ISO 13614
	ECMA_189 Type = 637
	// ECMA_190: 300mm, M.O., ??? sectors, 1024 bytes/sector, ECMA-190, ISO 13403
	ECMA_190 Type = 638
	// ECMA_195: 5,25", M.O., 936921 or 948770 sectors, 1024 bytes/sector, ECMA-195, ISO 13842
	ECMA_195 Type = 639
	// ECMA_195_512: 5,25", M.O., 1644581 or 1647371 sectors, 512 bytes/sector, ECMA-195, ISO 13842
	ECMA_195_512 Type = 640
	// ECMA_201: 3,5", M.O., 446325 sectors, 512 bytes/sector, ECMA-201, ISO 13963
	ECMA_201 Type = 641
	// ECMA_201_ROM: 3,5", M.O., 429975 sectors, 512 bytes/sector, embossed, ISO 13963
	ECMA_201_ROM Type = 642
	// ECMA_223: 3,5", M.O., 371371 sectors, 1024 bytes/sector, ECMA-223
	ECMA_223 Type = 643
	// ECMA_223_512: 3,5", M.O., 694929 sectors, 512 bytes/sector, ECMA-223
	ECMA_223_512 Type = 644
	// ECMA_238: 5,25", M.O., 1244621 sectors, 1024 bytes/sector, ECMA-238, ISO 15486
	ECMA_238 Type = 645
	// ECMA_239: 3,5", M.O., 318988, 320332 or 321100 sectors, 2048 bytes/sector, ECMA-239, ISO 15498
	ECMA_239 Type = 646
	// ECMA_260: 356mm, M.O., 14476734 sectors, 1024 bytes/sector, ECMA-260, ISO 15898
	ECMA_260 Type = 647
	// ECMA_260_Double: 356mm, M.O., 24445990 sectors, 1024 bytes/sector, ECMA-260, ISO 15898
	ECMA_260_Double Type = 648
	// ECMA_280: 5,25", M.O., 1128134 sectors, 2048 bytes/sector, ECMA-280, ISO 18093
	ECMA_280 Type = 649
	// ECMA_317: 300mm, M.O., 7355716 sectors, 2048 bytes/sector, ECMA-317, ISO 20162
	ECMA_317 Type = 650
	// ECMA_322: 5,25", M.O., 1095840 sectors, 4096 bytes/sector, ECMA-322, ISO 22092
	ECMA_322 Type = 651
	// ECMA_322_2k: 5,25", M.O., 2043664 sectors, 2048 bytes/sector, ECMA-322, ISO 22092
	ECMA_322_2k Type = 652
	// GigaMo: 3,5", M.O., 605846 sectors, 2048 bytes/sector, Cherry Book, GigaMo, ECMA-351, ISO 17346
	GigaMo Type = 653
	// GigaMo2: 3,5", M.O., 1063146 sectors, 2048 bytes/sector, Cherry Book 2, GigaMo 2, ECMA-353, ISO 22533
	GigaMo2       Type = 654
	CompactFloppy Type = 660
	DemiDiskette  Type = 661
	// Floptical: 3.5", 652 tracks, 2 sides, 512 bytes/sector, Floptical, ECMA-207, ISO 14169
	Floptical      Type = 662
	HiFD           Type = 663
	QuickDisk      Type = 664
	UHD144         Type = 665
	VideoFloppy    Type = 666
	Wafer          Type = 667
	ZXMicrodrive   Type = 668
	BeeCard        Type = 670
	Borsu          Type = 671
	DataStore      Type = 672
	DIR            Type = 673
	DST            Type = 674
	DTF            Type = 675
	DTF2           Type = 676
	Flextra3020    Type = 677
	Flextra3225    Type = 678
	HiTC1          Type = 679
	HiTC2          Type = 680
	LT1            Type = 681
	MiniCard       Type = 872
	Orb            Type = 683
	Orb5           Type = 684
	SmartMedia     Type = 685
	XD             Type = 686
	XQD            Type = 687
	DataPlay       Type = 688
	AppleProfile   Type = 690
	AppleWidget    Type = 691
	AppleHD20      Type = 692
	PriamDataTower Type = 693
	Pippin         Type = 694
	// RA60: 2382 cylinders, 4 tracks/cylinder, 42 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 204890112 bytes
	RA60 Type = 700
	// RA80: 546 cylinders, 14 tracks/cylinder, 31 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 121325568 bytes
	RA80 Type = 701
	// RA81: 1248 cylinders, 14 tracks/cylinder, 51 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 456228864 bytes
	RA81 Type = 702
	// RC25: 302 cylinders, 4 tracks/cylinder, 42 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 25976832 bytes
	RC25 Type = 703
	// RD31: 615 cylinders, 4 tracks/cylinder, 17 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 21411840 bytes
	RD31 Type = 704
	// RD32: 820 cylinders, 6 tracks/cylinder, 17 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 42823680 bytes
	RD32 Type = 705
	// RD51: 306 cylinders, 4 tracks/cylinder, 17 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 10653696 bytes
	RD51 Type = 706
	// RD52: 480 cylinders, 7 tracks/cylinder, 18 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 30965760 bytes
	RD52 Type = 707
	// RD53: 1024 cylinders, 7 tracks/cylinder, 18 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 75497472 bytes
	RD53 Type = 708
	// RD54: 1225 cylinders, 8 tracks/cylinder, 18 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 159936000 bytes
	RD54 Type = 709
	// RK06: 411 cylinders, 3 tracks/cylinder, 22 sectors/track, 256 words/sector, 16 bits/word, 512 bytes/sector, 13888512 bytes
	RK06 Type = 710
	// RK06_18: 411 cylinders, 3 tracks/cylinder, 20 sectors/track, 256 words/sector, 18 bits/word, 576 bytes/sector, 14204160 bytes
	RK06_18 Type = 711
	// RK07: 815 cylinders, 3 tracks/cylinder, 22 sectors/track, 256 words/sector, 16 bits/word, 512 bytes/sector, 27540480 bytes
	RK07 Type = 712
	// RK07_18: 815 cylinders, 3 tracks/cylinder, 20 sectors/track, 256 words/sector, 18 bits/word, 576 bytes/sector, 28166400 bytes
	RK07_18 Type = 713
	// RM02: 823 cylinders, 5 tracks/cylinder, 32 sectors/track, 256 words/sector, 16 bits/word, 512 bytes/sector, 67420160 bytes
	RM02 Type = 714
	// RM03: 823 cylinders, 5 tracks/cylinder, 32 sectors/track, 256 words/sector, 16 bits/word, 512 bytes/sector, 67420160 bytes
	RM03 Type = 715
	// RM05: 823 cylinders, 19 tracks/cylinder, 32 sectors/track, 256 words/sector, 16 bits/word, 512 bytes/sector, 256196608 bytes
	RM05 Type = 716
	// RP02: 203 cylinders, 10 tracks/cylinder, 22 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 22865920 bytes
	RP02 Type = 717
	// RP02_18: 203 cylinders, 10 tracks/cylinder, 20 sectors/track, 128 words/sector, 36 bits/word, 576 bytes/sector, 23385600 bytes
	RP02_18 Type = 718
	// RP03: 400 cylinders, 10 tracks/cylinder, 22 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 45056000 bytes
	RP03 Type = 719
	// RP03_18: 400 cylinders, 10 tracks/cylinder, 20 sectors/track, 128 words/sector, 36 bits/word, 576 bytes/sector, 46080000 bytes
	RP03_18 Type = 720
	// RP04: 411 cylinders, 19 tracks/cylinder, 22 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 87960576 bytes
	RP04 Type = 721
	// RP04_18: 411 cylinders, 19 tracks/cylinder, 20 sectors/track, 128 words/sector, 36 bits/word, 576 bytes/sector, 89959680 bytes
	RP04_18 Type = 722
	// RP05: 411 cylinders, 19 tracks/cylinder, 22 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 87960576 bytes
	RP05 Type = 723
	// RP05_18: 411 cylinders, 19 tracks/cylinder, 20 sectors/track, 128 words/sector, 36 bits/word, 576 bytes/sector, 89959680 bytes
	RP05_18 Type = 724
	// RP06: 815 cylinders, 19 tracks/cylinder, 22 sectors/track, 128 words/sector, 32 bits/word, 512 bytes/sector, 174423040 bytes
	RP06 Type = 725
	// RP06_18: 815 cylinders, 19 tracks/cylinder, 20 sectors/track, 128 words/sector, 36 bits/word, 576 bytes/sector, 178387200 bytes
	RP06_18 Type = 726
	// LS120: Imation LS-120 SuperDisk
	LS120 Type = 730
	// LS240: Imation LS-240 SuperDisk
	LS240 Type = 731
	// FD32MB: MF2HD formatted as 32MiB disk in Imation LS-240 drive
	FD32MB Type = 732
	// RDX: Tandberg / Imation RDX removable disk cartridge
	RDX Type = 733
	// RDX320: Imation 320Gb RDX
	RDX320 Type = 734
	// VideoNow: Hasbro VideoNow 85 mm proprietary video disc
	VideoNow Type = 740
	// VideoNowColor: Hasbro VideoNow Color disc
	VideoNowColor Type = 741
	// VideoNowXp: Hasbro VideoNow XP higher capacity disc
	VideoNowXp Type = 742
	// Bernoulli10: 8"x11" Bernoulli Box disk with 10Mb capacity 8"x11" Bernoulli Box disk with 20Mb capacity
	Bernoulli10 Type = 750
	// Bernoulli20: 5⅓" Bernoulli Box II disk with 20Mb capacity
	Bernoulli20      Type = 751
	BernoulliBox2_20 Type = 752
	// KodakVerbatim3: Kodak/Verbatim (3Mb)
	KodakVerbatim3 Type = 760
	// KodakVerbatim6: Kodak/Verbatim (6Mb)
	KodakVerbatim6 Type = 761
	// KodakVerbatim12: Kodak/Verbatim (12Mb)
	KodakVerbatim12 Type = 762
	// ProfessionalDisc: Professional Disc for video, single layer, rewritable, 23Gb
	ProfessionalDisc Type = 770
	// ProfessionalDiscDual: Professional Disc for video, dual layer, rewritable, 50Gb
	ProfessionalDiscDual Type = 771
	// ProfessionalDiscTriple: Professional Disc for video, triple layer, rewritable, 100Gb
	ProfessionalDiscTriple Type = 772
	// ProfessionalDiscQuad: Professional Disc for video, quad layer, write once, 128Gb
	ProfessionalDiscQuad Type = 773
	// PDD: Professional Disc for DATA, single layer, rewritable, 23Gb
	PDD Type = 774
	// PDD_WORM: Professional Disc for DATA, single layer, write once, 23Gb
	PDD_WORM Type = 775
	// ArchivalDisc: Archival Disc, 1st gen., 300Gb
	ArchivalDisc Type = 776
	// ArchivalDisc2: Archival Disc, 2nd gen., 500Gb
	ArchivalDisc2 Type = 777
	// ArchivalDisc3: Archival Disc, 3rd gen., 1Tb
	ArchivalDisc3 Type = 778
	// ODC300R: Optical Disc archive, 1st gen., write once, 300Gb
	ODC300R Type = 779
	// ODC300RE: Optical Disc archive, 1st gen., rewritable, 300Gb
	ODC300RE Type = 780
	// ODC600R: Optical Disc archive, 2nd gen., write once, 600Gb
	ODC600R Type = 781
	// ODC600RE: Optical Disc archive, 2nd gen., rewritable, 600Gb
	ODC600RE Type = 782
	// ODC1200RE: Optical Disc archive, 3rd gen., rewritable, 1200Gb
	ODC1200RE Type = 783
	// ODC1500R: Optical Disc archive, 3rd gen., write once, 1500Gb
	ODC1500R Type = 784
	// ODC3300R: Optical Disc archive, 4th gen., write once, 3300Gb
	ODC3300R Type = 785
	// ODC5500R: Optical Disc archive, 5th gen., write once, 5500Gb
	ODC5500R Type = 786
	// ECMA_322_1k: 5,25", M.O., 4383356 sectors, 1024 bytes/sector, ECMA-322, ISO 22092, 9.1Gb/cart
	ECMA_322_1k Type = 800
	// ECMA_322_512: 5,25", M.O., ??????? sectors, 512 bytes/sector, ECMA-322, ISO 22092, 9.1Gb/cart
	ECMA_322_512 Type = 801
	// ISO_14517: 5,25", M.O., 1273011 sectors, 1024 bytes/sector, ISO 14517, 2.6Gb/cart
	ISO_14517 Type = 802
	// ISO_14517_512: 5,25", M.O., 2244958 sectors, 512 bytes/sector, ISO 14517, 2.3Gb/cart
	ISO_14517_512 Type = 803
	// ISO_15041_512: 3,5", M.O., 1041500 sectors, 512 bytes/sector, ISO 15041, 540Mb/cart
	ISO_15041_512 Type = 804
	// HSM650: 3,5", M.O., ??????? sectors, proprietary, 650Mb/cart, Sony HyperStorage
	HSM650 Type = 805
	// MetaFloppy_Mod_I: 5.25", SS, DD, 35 tracks, 16 spt, 256 bytes/sector, MFM, 48 tpi, ???rpm
	MetaFloppy_Mod_I Type = 820
	// HF12: HyperFlex (12Mb), 5.25", DS, 301 tracks, 78 spt, 256 bytes/sector, MFM, 333 tpi, 600rpm
	HF12 Type = 823
	// HF24: HyperFlex (24Mb), 5.25", DS, 506 tracks, 78 spt, 256 bytes/sector, MFM, 666 tpi, 720rpm
	HF24 Type = 824
	// AtariLynxCard: Atari Lynx card
	AtariLynxCard Type = 821
	// AtariJaguarCartridge: Atari Jaguar cartridge
	AtariJaguarCartridge Type = 822
)

var _TypeNames = map[Type]string{
	UnknownMedia:            "UnknownMedia",
	UnknownMO:               "UnknownMO",
	GENERIC_HDD:             "GENERIC_HDD",
	Microdrive:              "Microdrive",
	Zone_HDD:                "Zone_HDD",
	FlashDrive:              "FlashDrive",
	CD:                      "CD",
	CDDA:                    "CDDA",
	CDG:                     "CDG",
	CDEG:                    "CDEG",
	CDI:                     "CDI",
	CDROM:                   "CDROM",
	CDROMXA:                 "CDROMXA",
	CDPLUS:                  "CDPLUS",
	CDMO:                    "CDMO",
	CDR:                     "CDR",
	CDRW:                    "CDRW",
	CDMRW:                   "CDMRW",
	VCD:                     "VCD",
	SVCD:                    "SVCD",
	PCD:                     "PCD",
	SACD:                    "SACD",
	DDCD:                    "DDCD",
	DDCDR:                   "DDCDR",
	DDCDRW:                  "DDCDRW",
	DTSCD:                   "DTSCD",
	CDMIDI:                  "CDMIDI",
	CDV:                     "CDV",
	PD650:                   "PD650",
	PD650_WORM:              "PD650_WORM",
	CDIREADY:                "CDIREADY",
	FMTOWNS:                 "FMTOWNS",
	DVDROM:                  "DVDROM",
	DVDR:                    "DVDR",
	DVDRW:                   "DVDRW",
	DVDPR:                   "DVDPR",
	DVDPRW:                  "DVDPRW",
	DVDPRWDL:                "DVDPRWDL",
	DVDRDL:                  "DVDRDL",
	DVDPRDL:                 "DVDPRDL",
	DVDRAM:                  "DVDRAM",
	DVDRWDL:                 "DVDRWDL",
	DVDDownload:             "DVDDownload",
	HDDVDROM:                "HDDVDROM",
	HDDVDRAM:                "HDDVDRAM",
	HDDVDR:                  "HDDVDR",
	HDDVDRW:                 "HDDVDRW",
	HDDVDRDL:                "HDDVDRDL",
	HDDVDRWDL:               "HDDVDRWDL",
	BDROM:                   "BDROM",
	BDR:                     "BDR",
	BDRE:                    "BDRE",
	BDRXL:                   "BDRXL",
	BDREXL:                  "BDREXL",
	UHDBD:                   "UHDBD",
	EVD:                     "EVD",
	FVD:                     "FVD",
	HVD:                     "HVD",
	CBHD:                    "CBHD",
	HDVMD:                   "HDVMD",
	VCDHD:                   "VCDHD",
	SVOD:                    "SVOD",
	FDDVD:                   "FDDVD",
	LD:                      "LD",
	LDROM:                   "LDROM",
	LDROM2:                  "LDROM2",
	LVROM:                   "LVROM",
	MegaLD:                  "MegaLD",
	HiMD:                    "HiMD",
	MD:                      "MD",
	MDData:                  "MDData",
	MDData2:                 "MDData2",
	MD60:                    "MD60",
	MD74:                    "MD74",
	MD80:                    "MD80",
	UDO:                     "UDO",
	UDO2:                    "UDO2",
	UDO2_WORM:               "UDO2_WORM",
	PlayStationMemoryCard:   "PlayStationMemoryCard",
	PlayStationMemoryCard2:  "PlayStationMemoryCard2",
	PS1CD:                   "PS1CD",
	PS2CD:                   "PS2CD",
	PS2DVD:                  "PS2DVD",
	PS3DVD:                  "PS3DVD",
	PS3BD:                   "PS3BD",
	PS4BD:                   "PS4BD",
	UMD:                     "UMD",
	PlayStationVitaGameCard: "PlayStationVitaGameCard",
	PS5BD:                   "PS5BD",
	XGD:                     "XGD",
	XGD2:                    "XGD2",
	XGD3:                    "XGD3",
	XGD4:                    "XGD4",
	MEGACD:                  "MEGACD",
	SATURNCD:                "SATURNCD",
	GDROM:                   "GDROM",
	GDR:                     "GDR",
	SegaCard:                "SegaCard",
	MilCD:                   "MilCD",
	HuCard:                  "HuCard",
	SuperCDROM2:             "SuperCDROM2",
	JaguarCD:                "JaguarCD",
	ThreeDO:                 "ThreeDO",
	PCFX:                    "PCFX",
	NeoGeoCD:                "NeoGeoCD",
	CDTV:                    "CDTV",
	CD32:                    "CD32",
	Nuon:                    "Nuon",
	Playdia:                 "Playdia",
	Apple32SS:               "Apple32SS",
	Apple32DS:               "Apple32DS",
	Apple33SS:               "Apple33SS",
	Apple33DS:               "Apple33DS",
	AppleSonySS:             "AppleSonySS",
	AppleSonyDS:             "AppleSonyDS",
	AppleFileWare:           "AppleFileWare",
	DOS_525_SS_DD_8:         "DOS_525_SS_DD_8",
	DOS_525_SS_DD_9:         "DOS_525_SS_DD_9",
	DOS_525_DS_DD_8:         "DOS_525_DS_DD_8",
	DOS_525_DS_DD_9:         "DOS_525_DS_DD_9",
	DOS_525_HD:              "DOS_525_HD",
	DOS_35_SS_DD_8:          "DOS_35_SS_DD_8",
	DOS_35_SS_DD_9:          "DOS_35_SS_DD_9",
	DOS_35_DS_DD_8:          "DOS_35_DS_DD_8",
	DOS_35_DS_DD_9:          "DOS_35_DS_DD_9",
	DOS_35_HD:               "DOS_35_HD",
	DOS_35_ED:               "DOS_35_ED",
	DMF:                     "DMF",
	DMF_82:                  "DMF_82",
	XDF_525:                 "XDF_525",
	XDF_35:                  "XDF_35",
	IBM23FD:                 "IBM23FD",
	IBM33FD_128:             "IBM33FD_128",
	IBM33FD_256:             "IBM33FD_256",
	IBM33FD_512:             "IBM33FD_512",
	IBM43FD_128:             "IBM43FD_128",
	IBM43FD_256:             "IBM43FD_256",
	IBM53FD_256:             "IBM53FD_256",
	IBM53FD_512:             "IBM53FD_512",
	IBM53FD_1024:            "IBM53FD_1024",
	RX01:                    "RX01",
	RX02:                    "RX02",
	RX03:                    "RX03",
	RX50:                    "RX50",
	ACORN_525_SS_SD_40:      "ACORN_525_SS_SD_40",
	ACORN_525_SS_SD_80:      "ACORN_525_SS_SD_80",
	ACORN_525_SS_DD_40:      "ACORN_525_SS_DD_40",
	ACORN_525_SS_DD_80:      "ACORN_525_SS_DD_80",
	ACORN_525_DS_DD:         "ACORN_525_DS_DD",
	ACORN_35_DS_DD:          "ACORN_35_DS_DD",
	ACORN_35_DS_HD:          "ACORN_35_DS_HD",
	ATARI_525_SD:            "ATARI_525_SD",
	ATARI_525_ED:            "ATARI_525_ED",
	ATARI_525_DD:            "ATARI_525_DD",
	ATARI_35_SS_DD:          "ATARI_35_SS_DD",
	ATARI_35_DS_DD:          "ATARI_35_DS_DD",
	ATARI_35_SS_DD_11:       "ATARI_35_SS_DD_11",
	ATARI_35_DS_DD_11:       "ATARI_35_DS_DD_11",
	CBM_35_DD:               "CBM_35_DD",
	CBM_AMIGA_35_DD:         "CBM_AMIGA_35_DD",
	CBM_AMIGA_35_HD:         "CBM_AMIGA_35_HD",
	CBM_1540:                "CBM_1540",
	CBM_1540_Ext:            "CBM_1540_Ext",
	CBM_1571:                "CBM_1571",
	NEC_8_SD:                "NEC_8_SD",
	NEC_8_DD:                "NEC_8_DD",
	NEC_525_SS:              "NEC_525_SS",
	NEC_525_DS:              "NEC_525_DS",
	NEC_525_HD:              "NEC_525_HD",
	NEC_35_HD_8:             "NEC_35_HD_8",
	NEC_35_HD_15:            "NEC_35_HD_15",
	NEC_35_TD:               "NEC_35_TD",
	SHARP_525_9:             "SHARP_525_9",
	SHARP_35_9:              "SHARP_35_9",
	ECMA_99_8:               "ECMA_99_8",
	ECMA_99_15:              "ECMA_99_15",
	ECMA_99_26:              "ECMA_99_26",
	ECMA_54:                 "ECMA_54",
	ECMA_59:                 "ECMA_59",
	ECMA_66:                 "ECMA_66",
	ECMA_69_8:               "ECMA_69_8",
	ECMA_69_15:              "ECMA_69_15",
	ECMA_69_26:              "ECMA_69_26",
	ECMA_70:                 "ECMA_70",
	ECMA_78:                 "ECMA_78",
	ECMA_78_2:               "ECMA_78_2",
	FDFORMAT_525_DD:         "FDFORMAT_525_DD",
	FDFORMAT_525_HD:         "FDFORMAT_525_HD",
	FDFORMAT_35_DD:          "FDFORMAT_35_DD",
	FDFORMAT_35_HD:          "FDFORMAT_35_HD",
	Apricot_35:              "Apricot_35",
	ADR2120:                 "ADR2120",
	ADR260:                  "ADR260",
	ADR30:                   "ADR30",
	ADR50:                   "ADR50",
	AIT1:                    "AIT1",
	AIT1Turbo:               "AIT1Turbo",
	AIT2:                    "AIT2",
	AIT2Turbo:               "AIT2Turbo",
	AIT3:                    "AIT3",
	AIT3Ex:                  "AIT3Ex",
	AIT3Turbo:               "AIT3Turbo",
	AIT4:                    "AIT4",
	AIT5:                    "AIT5",
	AITETurbo:               "AITETurbo",
	SAIT1:                   "SAIT1",
	SAIT2:                   "SAIT2",
	Bernoulli:               "Bernoulli",
	Bernoulli2:              "Bernoulli2",
	Ditto:                   "Ditto",
	DittoMax:                "DittoMax",
	Jaz:                     "Jaz",
	Jaz2:                    "Jaz2",
	PocketZip:               "PocketZip",
	REV120:                  "REV120",
	REV35:                   "REV35",
	REV70:                   "REV70",
	ZIP100:                  "ZIP100",
	ZIP250:                  "ZIP250",
	ZIP750:                  "ZIP750",
	Bernoulli35:             "Bernoulli35",
	Bernoulli44:             "Bernoulli44",
	Bernoulli65:             "Bernoulli65",
	Bernoulli90:             "Bernoulli90",
	Bernoulli105:            "Bernoulli105",
	Bernoulli150:            "Bernoulli150",
	Bernoulli230:            "Bernoulli230",
	CompactCassette:         "CompactCassette",
	Data8:                   "Data8",
	MiniDV:                  "MiniDV",
	Dcas25:                  "Dcas25",
	Dcas85:                  "Dcas85",
	Dcas103:                 "Dcas103",
	CFast:                   "CFast",
	CompactFlash:            "CompactFlash",
	CompactFlashType2:       "CompactFlashType2",
	DigitalAudioTape:        "DigitalAudioTape",
	DAT160:                  "DAT160",
	DAT320:                  "DAT320",
	DAT72:                   "DAT72",
	DDS1:                    "DDS1",
	DDS2:                    "DDS2",
	DDS3:                    "DDS3",
	DDS4:                    "DDS4",
	CompactTapeI:            "CompactTapeI",
	CompactTapeII:           "CompactTapeII",
	DECtapeII:               "DECtapeII",
	DLTtapeIII:              "DLTtapeIII",
	DLTtapeIIIxt:            "DLTtapeIIIxt",
	DLTtapeIV:               "DLTtapeIV",
	DLTtapeS4:               "DLTtapeS4",
	SDLT1:                   "SDLT1",
	SDLT2:                   "SDLT2",
	VStapeI:                 "VStapeI",
	Exatape15m:              "Exatape15m",
	Exatape22m:              "Exatape22m",
	Exatape22mAME:           "Exatape22mAME",
	Exatape28m:              "Exatape28m",
	Exatape40m:              "Exatape40m",
	Exatape45m:              "Exatape45m",
	Exatape54m:              "Exatape54m",
	Exatape75m:              "Exatape75m",
	Exatape76m:              "Exatape76m",
	Exatape80m:              "Exatape80m",
	Exatape106m:             "Exatape106m",
	Exatape160mXL:           "Exatape160mXL",
	Exatape112m:             "Exatape112m",
	Exatape125m:             "Exatape125m",
	Exatape150m:             "Exatape150m",
	Exatape170m:             "Exatape170m",
	Exatape225m:             "Exatape225m",
	ExpressCard34:           "ExpressCard34",
	ExpressCard54:           "ExpressCard54",
	PCCardTypeI:             "PCCardTypeI",
	PCCardTypeII:            "PCCardTypeII",
	PCCardTypeIII:           "PCCardTypeIII",
	PCCardTypeIV:            "PCCardTypeIV",
	EZ135:                   "EZ135",
	EZ230:                   "EZ230",
	Quest:                   "Quest",
	SparQ:                   "SparQ",
	SQ100:                   "SQ100",
	SQ200:                   "SQ200",
	SQ300:                   "SQ300",
	SQ310:                   "SQ310",
	SQ327:                   "SQ327",
	SQ400:                   "SQ400",
	SQ800:                   "SQ800",
	SQ1500:                  "SQ1500",
	SQ2000:                  "SQ2000",
	SyJet:                   "SyJet",
	FamicomGamePak:          "FamicomGamePak",
	GameBoyAdvanceGamePak:   "GameBoyAdvanceGamePak",
	GameBoyGamePak:          "GameBoyGamePak",
	GOD:                     "GOD",
	N64DD:                   "N64DD",
	N64GamePak:              "N64GamePak",
	NESGamePak:              "NESGamePak",
	Nintendo3DSGameCard:     "Nintendo3DSGameCard",
	NintendoDiskCard:        "NintendoDiskCard",
	NintendoDSGameCard:      "NintendoDSGameCard",
	NintendoDSiGameCard:     "NintendoDSiGameCard",
	SNESGamePak:             "SNESGamePak",
	SNESGamePakUS:           "SNESGamePakUS",
	WOD:                     "WOD",
	WUOD:                    "WUOD",
	SwitchGameCard:          "SwitchGameCard",
	IBM3470:                 "IBM3470",
	IBM3480:                 "IBM3480",
	IBM3490:                 "IBM3490",
	IBM3490E:                "IBM3490E",
	IBM3592:                 "IBM3592",
	LTO:                     "LTO",
	LTO2:                    "LTO2",
	LTO3:                    "LTO3",
	LTO3WORM:                "LTO3WORM",
	LTO4:                    "LTO4",
	LTO4WORM:                "LTO4WORM",
	LTO5:                    "LTO5",
	LTO5WORM:                "LTO5WORM",
	LTO6:                    "LTO6",
	LTO6WORM:                "LTO6WORM",
	LTO7:                    "LTO7",
	LTO7WORM:                "LTO7WORM",
	MemoryStick:             "MemoryStick",
	MemoryStickDuo:          "MemoryStickDuo",
	MemoryStickMicro:        "MemoryStickMicro",
	MemoryStickPro:          "MemoryStickPro",
	MemoryStickProDuo:       "MemoryStickProDuo",
	MicroSD:                 "microSD",
	MiniSD:                  "miniSD",
	SecureDigital:           "SecureDigital",
	MMC:                     "MMC",
	MMCmicro:                "MMCmicro",
	RSMMC:                   "RSMMC",
	MMCplus:                 "MMCplus",
	MMCmobile:               "MMCmobile",
	MLR1:                    "MLR1",
	MLR1SL:                  "MLR1SL",
	MLR3:                    "MLR3",
	SLR1:                    "SLR1",
	SLR2:                    "SLR2",
	SLR3:                    "SLR3",
	SLR32:                   "SLR32",
	SLR32SL:                 "SLR32SL",
	SLR4:                    "SLR4",
	SLR5:                    "SLR5",
	SLR5SL:                  "SLR5SL",
	SLR6:                    "SLR6",
	SLRtape7:                "SLRtape7",
	SLRtape7SL:              "SLRtape7SL",
	SLRtape24:               "SLRtape24",
	SLRtape24SL:             "SLRtape24SL",
	SLRtape40:               "SLRtape40",
	SLRtape50:               "SLRtape50",
	SLRtape60:               "SLRtape60",
	SLRtape75:               "SLRtape75",
	SLRtape100:              "SLRtape100",
	SLRtape140:              "SLRtape140",
	QIC11:                   "QIC11",
	QIC120:                  "QIC120",
	QIC1350:                 "QIC1350",
	QIC150:                  "QIC150",
	QIC24:                   "QIC24",
	QIC3010:                 "QIC3010",
	QIC3020:                 "QIC3020",
	QIC3080:                 "QIC3080",
	QIC3095:                 "QIC3095",
	QIC320:                  "QIC320",
	QIC40:                   "QIC40",
	QIC525:                  "QIC525",
	QIC80:                   "QIC80",
	STK4480:                 "STK4480",
	STK4490:                 "STK4490",
	STK9490:                 "STK9490",
	T9840A:                  "T9840A",
	T9840B:                  "T9840B",
	T9840C:                  "T9840C",
	T9840D:                  "T9840D",
	T9940A:                  "T9940A",
	T9940B:                  "T9940B",
	T10000A:                 "T10000A",
	T10000B:                 "T10000B",
	T10000C:                 "T10000C",
	T10000D:                 "T10000D",
	Travan1:                 "Travan1",
	Travan1Ex:               "Travan1Ex",
	Travan3:                 "Travan3",
	Travan3Ex:               "Travan3Ex",
	Travan4:                 "Travan4",
	Travan5:                 "Travan5",
	Travan7:                 "Travan7",
	VXA1:                    "VXA1",
	VXA2:                    "VXA2",
	VXA3:                    "VXA3",
	ECMA_153:                "ECMA_153",
	ECMA_153_512:            "ECMA_153_512",
	ECMA_154:                "ECMA_154",
	ECMA_183_512:            "ECMA_183_512",
	ECMA_183:                "ECMA_183",
	ECMA_184_512:            "ECMA_184_512",
	ECMA_184:                "ECMA_184",
	ECMA_189:                "ECMA_189",
	ECMA_190:                "ECMA_190",
	ECMA_195:                "ECMA_195",
	ECMA_195_512:            "ECMA_195_512",
	ECMA_201:                "ECMA_201",
	ECMA_201_ROM:            "ECMA_201_ROM",
	ECMA_223:                "ECMA_223",
	ECMA_223_512:            "ECMA_223_512",
	ECMA_238:                "ECMA_238",
	ECMA_239:                "ECMA_239",
	ECMA_260:                "ECMA_260",
	ECMA_260_Double:         "ECMA_260_Double",
	ECMA_280:                "ECMA_280",
	ECMA_317:                "ECMA_317",
	ECMA_322:                "ECMA_322",
	ECMA_322_2k:             "ECMA_322_2k",
	GigaMo:                  "GigaMo",
	GigaMo2:                 "GigaMo2",
	CompactFloppy:           "CompactFloppy",
	DemiDiskette:            "DemiDiskette",
	Floptical:               "Floptical",
	HiFD:                    "HiFD",
	QuickDisk:               "QuickDisk",
	UHD144:                  "UHD144",
	VideoFloppy:             "VideoFloppy",
	Wafer:                   "Wafer",
	ZXMicrodrive:            "ZXMicrodrive",
	BeeCard:                 "BeeCard",
	Borsu:                   "Borsu",
	DataStore:               "DataStore",
	DIR:                     "DIR",
	DST:                     "DST",
	DTF:                     "DTF",
	DTF2:                    "DTF2",
	Flextra3020:             "Flextra3020",
	Flextra3225:             "Flextra3225",
	HiTC1:                   "HiTC1",
	HiTC2:                   "HiTC2",
	LT1:                     "LT1",
	MiniCard:                "MiniCard",
	Orb:                     "Orb",
	Orb5:                    "Orb5",
	SmartMedia:              "SmartMedia",
	XD:                      "xD",
	XQD:                     "XQD",
	DataPlay:                "DataPlay",
	AppleProfile:            "AppleProfile",
	AppleWidget:             "AppleWidget",
	AppleHD20:               "AppleHD20",
	PriamDataTower:          "PriamDataTower",
	Pippin:                  "Pippin",
	RA60:                    "RA60",
	RA80:                    "RA80",
	RA81:                    "RA81",
	RC25:                    "RC25",
	RD31:                    "RD31",
	RD32:                    "RD32",
	RD51:                    "RD51",
	RD52:                    "RD52",
	RD53:                    "RD53",
	RD54:                    "RD54",
	RK06:                    "RK06",
	RK06_18:                 "RK06_18",
	RK07:                    "RK07",
	RK07_18:                 "RK07_18",
	RM02:                    "RM02",
	RM03:                    "RM03",
	RM05:                    "RM05",
	RP02:                    "RP02",
	RP02_18:                 "RP02_18",
	RP03:                    "RP03",
	RP03_18:                 "RP03_18",
	RP04:                    "RP04",
	RP04_18:                 "RP04_18",
	RP05:                    "RP05",
	RP05_18:                 "RP05_18",
	RP06:                    "RP06",
	RP06_18:                 "RP06_18",
	LS120:                   "LS120",
	LS240:                   "LS240",
	FD32MB:                  "FD32MB",
	RDX:                     "RDX",
	RDX320:                  "RDX320",
	VideoNow:                "VideoNow",
	VideoNowColor:           "VideoNowColor",
	VideoNowXp:              "VideoNowXp",
	Bernoulli10:             "Bernoulli10",
	Bernoulli20:             "Bernoulli20",
	BernoulliBox2_20:        "BernoulliBox2_20",
	KodakVerbatim3:          "KodakVerbatim3",
	KodakVerbatim6:          "KodakVerbatim6",
	KodakVerbatim12:         "KodakVerbatim12",
	ProfessionalDisc:        "ProfessionalDisc",
	ProfessionalDiscDual:    "ProfessionalDiscDual",
	ProfessionalDiscTriple:  "ProfessionalDiscTriple",
	ProfessionalDiscQuad:    "ProfessionalDiscQuad",
	PDD:                     "PDD",
	PDD_WORM:                "PDD_WORM",
	ArchivalDisc:            "ArchivalDisc",
	ArchivalDisc2:           "ArchivalDisc2",
	ArchivalDisc3:           "ArchivalDisc3",
	ODC300R:                 "ODC300R",
	ODC300RE:                "ODC300RE",
	ODC600R:                 "ODC600R",
	ODC600RE:                "ODC600RE",
	ODC1200RE:               "ODC1200RE",
	ODC1500R:                "ODC1500R",
	ODC3300R:                "ODC3300R",
	ODC5500R:                "ODC5500R",
	ECMA_322_1k:             "ECMA_322_1k",
	ECMA_322_512:            "ECMA_322_512",
	ISO_14517:               "ISO_14517",
	ISO_14517_512:           "ISO_14517_512",
	ISO_15041_512:           "ISO_15041_512",
	HSM650:                  "HSM650",
	MetaFloppy_Mod_I:        "MetaFloppy_Mod_I",
	HF12:                    "HF12",
	HF24:                    "HF24",
	AtariLynxCard:           "AtariLynxCard",
	AtariJaguarCartridge:    "AtariJaguarCartridge",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t Type) String() string {
	if s, ok := _TypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("Type(%d)", uint32(t))
}

var opticalTypes = map[Type]bool{
	CD:            true,
	CDDA:          true,
	CDG:           true,
	CDEG:          true,
	CDI:           true,
	CDIREADY:      true,
	CDROM:         true,
	CDROMXA:       true,
	CDPLUS:        true,
	CDMO:          true,
	CDR:           true,
	CDRW:          true,
	CDMRW:         true,
	VCD:           true,
	SVCD:          true,
	PCD:           true,
	SACD:          true,
	DDCD:          true,
	DDCDR:         true,
	DDCDRW:        true,
	DTSCD:         true,
	CDMIDI:        true,
	CDV:           true,
	DVDROM:        true,
	DVDR:          true,
	DVDRW:         true,
	DVDPR:         true,
	DVDPRW:        true,
	DVDPRWDL:      true,
	DVDRDL:        true,
	DVDPRDL:       true,
	DVDRAM:        true,
	DVDRWDL:       true,
	DVDDownload:   true,
	HDDVDROM:      true,
	HDDVDRAM:      true,
	HDDVDR:        true,
	HDDVDRW:       true,
	HDDVDRDL:      true,
	HDDVDRWDL:     true,
	BDROM:         true,
	BDR:           true,
	BDRE:          true,
	BDRXL:         true,
	BDREXL:        true,
	UHDBD:         true,
	EVD:           true,
	FVD:           true,
	HVD:           true,
	CBHD:          true,
	HDVMD:         true,
	VCDHD:         true,
	SVOD:          true,
	FDDVD:         true,
	LD:            true,
	LDROM:         true,
	LDROM2:        true,
	LVROM:         true,
	MegaLD:        true,
	PS1CD:         true,
	PS2CD:         true,
	PS2DVD:        true,
	PS3DVD:        true,
	PS3BD:         true,
	PS4BD:         true,
	PS5BD:         true,
	UMD:           true,
	XGD:           true,
	XGD2:          true,
	XGD3:          true,
	XGD4:          true,
	MEGACD:        true,
	SATURNCD:      true,
	GDROM:         true,
	GDR:           true,
	SuperCDROM2:   true,
	JaguarCD:      true,
	ThreeDO:       true,
	PCFX:          true,
	NeoGeoCD:      true,
	GOD:           true,
	WOD:           true,
	WUOD:          true,
	CDTV:          true,
	CD32:          true,
	Nuon:          true,
	Playdia:       true,
	Pippin:        true,
	FMTOWNS:       true,
	MilCD:         true,
	VideoNow:      true,
	VideoNowColor: true,
	VideoNowXp:    true,
}
