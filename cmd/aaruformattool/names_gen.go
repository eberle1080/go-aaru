// Human-readable names of media tag, data, media and sector tag types.

package main

import (
	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// mediaTagTypeString returns the human-readable name of t.
func mediaTagTypeString(t aaru.MediaTagType) string {
	switch t {
	case aaru.MediaTagCdToc:
		return "CD Table of Contents"
	case aaru.MediaTagSessionInfo:
		return "CD Session Information"
	case aaru.MediaTagFullToc:
		return "CD Full TOC (multi-session)"
	case aaru.MediaTagCdPma:
		return "CD Program Memory Area"
	case aaru.MediaTagCdAtip:
		return "CD Absolute Time in Pregroove"
	case aaru.MediaTagCdText:
		return "CD-Text"
	case aaru.MediaTagCdMcn:
		return "CD Media Catalogue Number"
	case aaru.MediaTagDvdPfi:
		return "DVD Physical Format Information"
	case aaru.MediaTagDvdCmi:
		return "DVD Copyright Management Information"
	case aaru.MediaTagDvdDiscKey:
		return "DVD Disc Key"
	case aaru.MediaTagDvdBca:
		return "DVD Burst Cutting Area"
	case aaru.MediaTagDvdDmi:
		return "DVD Disc Manufacturing Information"
	case aaru.MediaTagDvdMediaIdentifier:
		return "DVD Media Identifier"
	case aaru.MediaTagDvdMkb:
		return "DVD Media Key Block"
	case aaru.MediaTagDvdRamDds:
		return "DVD-RAM Defect Data Structure"
	case aaru.MediaTagDvdRamMediumStatus:
		return "DVD-RAM Medium Status"
	case aaru.MediaTagDvdRamSpareArea:
		return "DVD-RAM Spare Area"
	case aaru.MediaTagDvdrRmd:
		return "DVD-R Recording Management Data"
	case aaru.MediaTagDvdrPreRecordedInfo:
		return "DVD-R Pre-recorded Information"
	case aaru.MediaTagDvdrMediaIdentifier:
		return "DVD-R Media Identifier"
	case aaru.MediaTagDvdrPfi:
		return "DVD-R Physical Format Information"
	case aaru.MediaTagDvdAdip:
		return "DVD Address in Pregroove"
	case aaru.MediaTagHddvdCpi:
		return "HD DVD Content Protection Information"
	case aaru.MediaTagHddvdMediumStatus:
		return "HD DVD Medium Status"
	case aaru.MediaTagDvddlLayerCapacity:
		return "DVD Dual Layer - Layer Capacity"
	case aaru.MediaTagDvddlMiddleZoneAddress:
		return "DVD Dual Layer - Middle Zone Address"
	case aaru.MediaTagDvddlJumpIntervalSize:
		return "DVD Dual Layer - Jump Interval Size"
	case aaru.MediaTagDvddlManualLayerJumpLba:
		return "DVD Dual Layer - Manual Layer Jump LBA"
	case aaru.MediaTagBlurayDi:
		return "Blu-ray Disc Information"
	case aaru.MediaTagBlurayBca:
		return "Blu-ray Burst Cutting Area"
	case aaru.MediaTagBlurayDds:
		return "Blu-ray Disc Definition Structure"
	case aaru.MediaTagBlurayCartridgeStatus:
		return "Blu-ray Cartridge Status"
	case aaru.MediaTagBluraySpareArea:
		return "Blu-ray Spare Area"
	case aaru.MediaTagAacsVolumeIdentifier:
		return "AACS Volume Identifier"
	case aaru.MediaTagAacsSerialNumber:
		return "AACS Serial Number"
	case aaru.MediaTagAacsMediaIdentifier:
		return "AACS Media Identifier"
	case aaru.MediaTagAacsMkb:
		return "AACS Media Key Block"
	case aaru.MediaTagAacsDataKeys:
		return "AACS Data Keys"
	case aaru.MediaTagAacsLbaExtents:
		return "AACS LBA Extents"
	case aaru.MediaTagCprmMkb:
		return "CPRM Media Key Block"
	case aaru.MediaTagHybridRecognizedLayers:
		return "Hybrid Disc - Recognized Layers"
	case aaru.MediaTagMmcWriteProtection:
		return "MMC Write Protection Status"
	case aaru.MediaTagMmcDiscInformation:
		return "MMC Disc Information"
	case aaru.MediaTagMmcTrackResourcesInformation:
		return "MMC Track Resources Information"
	case aaru.MediaTagMmcPowResourcesInformation:
		return "MMC POW Resources Information"
	case aaru.MediaTagScsiInquiry:
		return "SCSI INQUIRY Data"
	case aaru.MediaTagScsiModePage2A:
		return "SCSI Mode Page 2Ah (CD/DVD Capabilities)"
	case aaru.MediaTagAtaIdentify:
		return "ATA IDENTIFY DEVICE"
	case aaru.MediaTagAtapiIdentify:
		return "ATAPI IDENTIFY DEVICE"
	case aaru.MediaTagPcmciaCis:
		return "PCMCIA Card Information Structure"
	case aaru.MediaTagSdCid:
		return "Secure Digital Card ID"
	case aaru.MediaTagSdCsd:
		return "Secure Digital Card Specific Data"
	case aaru.MediaTagSdScr:
		return "Secure Digital Configuration Register"
	case aaru.MediaTagSdOcr:
		return "Secure Digital Operation Conditions"
	case aaru.MediaTagMmcCid:
		return "MultiMediaCard Card ID"
	case aaru.MediaTagMmcCsd:
		return "MultiMediaCard Card Specific Data"
	case aaru.MediaTagMmcOcr:
		return "MultiMediaCard Operation Conditions"
	case aaru.MediaTagExtendedCsd:
		return "MultiMediaCard Extended CSD"
	case aaru.MediaTagXboxSecuritySector:
		return "Xbox Security Sector"
	case aaru.MediaTagFloppyLeadOut:
		return "Floppy Lead-out"
	case aaru.MediaTagDiscControlBlock:
		return "DVD Disc Control Block"
	case aaru.MediaTagCdFirstTrackPregap:
		return "CD First Track Pre-gap"
	case aaru.MediaTagCdLeadOut:
		return "CD Lead-out"
	case aaru.MediaTagScsiModeSense6:
		return "SCSI MODE SENSE (6)"
	case aaru.MediaTagScsiModeSense10:
		return "SCSI MODE SENSE (10)"
	case aaru.MediaTagUsbDescriptors:
		return "USB Descriptors"
	case aaru.MediaTagXboxDmi:
		return "Xbox Disc Manufacturing Information"
	case aaru.MediaTagXboxPfi:
		return "Xbox Physical Format Information"
	case aaru.MediaTagCdLeadIn:
		return "CD Lead-in"
	case aaru.MediaTagMiniDiscType:
		return "MiniDisc Type"
	case aaru.MediaTagMiniDiscD5:
		return "MiniDisc D5h Response"
	case aaru.MediaTagMiniDiscUtoc:
		return "MiniDisc User TOC"
	case aaru.MediaTagMiniDiscDtoc:
		return "MiniDisc Data TOC"
	case aaru.MediaTagDvdDiscKeyDecrypted:
		return "DVD Disc Key (decrypted)"
	case aaru.MediaTagDvdPfi2ndLayer:
		return "DVD Physical Format Information (2nd layer)"
	case aaru.MediaTagFloppyWriteProtect:
		return "Floppy Write-Protect Status"
	case aaru.MediaTagAacsMediaKey:
		return "AACS Media Key"
	case aaru.MediaTagAacsVolumeUniqueKey:
		return "AACS Volume Unique Key"
	default:
		return "Unknown Media Tag"
	}
}

// dataTypeString returns the human-readable name of t.
func dataTypeString(t aaru.DataType) string {
	switch t {
	case aaru.DataTypeUserData:
		return "User Data"
	case aaru.DataTypeCdToc:
		return "CD Partial TOC"
	case aaru.DataTypeSessionInfo:
		return "CD Session Information"
	case aaru.DataTypeFullToc:
		return "CD Table of Contents"
	case aaru.DataTypeCdPma:
		return "CD Power Management Area"
	case aaru.DataTypeCdAtip:
		return "CD Absolute Time in Pregroove"
	case aaru.DataTypeCdText:
		return "CD Lead-in CD-TEXT"
	case aaru.DataTypeDvdPfi:
		return "DVD Physical Format Information"
	case aaru.DataTypeDvdCmi:
		return "DVD Lead-in Copyright Management Information"
	case aaru.DataTypeDvdDiscKey:
		return "DVD Disc Key"
	case aaru.DataTypeDvdBca:
		return "DVD Burst Cutting Area"
	case aaru.DataTypeDvdDmi:
		return "DVD Disc Manufacturing Information"
	case aaru.DataTypeDvdMediaIdentifier:
		return "DVD Media Identifier"
	case aaru.DataTypeDvdMkb:
		return "DVD Media Key Block"
	case aaru.DataTypeDvdRamDds:
		return "DVD-RAM Disc Definition Structure"
	case aaru.DataTypeDvdRamMediumStatus:
		return "DVD-RAM Medium Status"
	case aaru.DataTypeDvdRamSpareArea:
		return "DVD-RAM Spare Area Information"
	case aaru.DataTypeDvdrRmd:
		return "DVD-R Recording Management Data"
	case aaru.DataTypeDvdrPreRecordedInfo:
		return "DVD-R Pre-recorded Information"
	case aaru.DataTypeDvdrMediaIdentifier:
		return "DVD-R Media Identifier"
	case aaru.DataTypeDvdrPfi:
		return "DVD-R Physical Format Information"
	case aaru.DataTypeDvdAdip:
		return "DVD Address in Pregroove"
	case aaru.DataTypeHddvdCpi:
		return "HD DVD Content Protection Information"
	case aaru.DataTypeHddvdMediumStatus:
		return "HD DVD Medium Status"
	case aaru.DataTypeDvddlLayerCapacity:
		return "DVD Dual Layer - Layer Capacity"
	case aaru.DataTypeDvddlMiddleZoneAddress:
		return "DVD Dual Layer - Middle Zone Address"
	case aaru.DataTypeDvddlJumpIntervalSize:
		return "DVD Dual Layer - Jump Interval Size"
	case aaru.DataTypeDvddlManualLayerJumpLba:
		return "DVD Dual Layer - Manual Layer Jump LBA"
	case aaru.DataTypeBlurayDi:
		return "Blu-ray Disc Information"
	case aaru.DataTypeBlurayBca:
		return "Blu-ray Burst Cutting Area"
	case aaru.DataTypeBlurayDds:
		return "Blu-ray Disc Definition Structure"
	case aaru.DataTypeBlurayCartridgeStatus:
		return "Blu-ray Cartridge Status"
	case aaru.DataTypeBluraySpareArea:
		return "Blu-ray Spare Area Information"
	case aaru.DataTypeAacsVolumeIdentifier:
		return "AACS Volume Identifier"
	case aaru.DataTypeAacsMediaIdentifier:
		return "AACS Media Identifier"
	case aaru.DataTypeAacsMkb:
		return "AACS Media Key Block"
	case aaru.DataTypeAacsDataKeys:
		return "AACS Data Keys"
	case aaru.DataTypeAacsLbaExtents:
		return "AACS LBA Extents"
	case aaru.DataTypeCprmMkb:
		return "CPRM Media Key Block"
	case aaru.DataTypeHybridRecognizedLayers:
		return "Hybrid Disc - Recognized Layers"
	case aaru.DataTypeMmcWriteProtection:
		return "SCSI MMC Write Protection"
	case aaru.DataTypeMmcDiscInformation:
		return "SCSI MMC Disc Information"
	case aaru.DataTypeMmcTrackResourcesInformation:
		return "SCSI MMC Track Resources Information"
	case aaru.DataTypeMmcPowResourcesInformation:
		return "SCSI MMC POW Resources Information"
	case aaru.DataTypeScsiInquiry:
		return "SCSI INQUIRY Response"
	case aaru.DataTypeScsiModePage2A:
		return "SCSI MODE PAGE 2Ah"
	case aaru.DataTypeAtaIdentify:
		return "ATA IDENTIFY DEVICE"
	case aaru.DataTypeAtapiIdentify:
		return "ATAPI IDENTIFY PACKET DEVICE"
	case aaru.DataTypePcmciaCis:
		return "PCMCIA Card Information Structure"
	case aaru.DataTypeSdCid:
		return "Secure Digital CID Register"
	case aaru.DataTypeSdCsd:
		return "Secure Digital CSD Register"
	case aaru.DataTypeSdScr:
		return "Secure Digital SCR Register"
	case aaru.DataTypeSdOcr:
		return "Secure Digital OCR Register"
	case aaru.DataTypeMmcCid:
		return "MultiMediaCard CID Register"
	case aaru.DataTypeMmcCsd:
		return "MultiMediaCard CSD Register"
	case aaru.DataTypeExtendedCsd:
		return "MultiMediaCard Extended CSD Register"
	case aaru.DataTypeMmcOcr:
		return "MultiMediaCard OCR Register"
	case aaru.DataTypeXboxSecuritySector:
		return "Xbox Security Sector"
	case aaru.DataTypeFloppyLeadOut:
		return "Floppy Lead-out"
	case aaru.DataTypeDiscControlBlock:
		return "DVD Disc Control Block"
	case aaru.DataTypeCdFirstTrackPregap:
		return "CD First Track Pre-gap"
	case aaru.DataTypeCdLeadOut:
		return "CD Lead-out"
	case aaru.DataTypeScsiModeSense6:
		return "SCSI MODE SENSE (6)"
	case aaru.DataTypeScsiModeSense10:
		return "SCSI MODE SENSE (10)"
	case aaru.DataTypeUsbDescriptors:
		return "USB Descriptors"
	case aaru.DataTypeXboxDmi:
		return "Xbox Disc Manufacturing Information"
	case aaru.DataTypeXboxPfi:
		return "Xbox Physical Format Information"
	case aaru.DataTypeCdSectorPrefix:
		return "CD Sector Prefix (sync + header)"
	case aaru.DataTypeCdSectorSuffix:
		return "CD Sector Suffix (EDC + ECC)"
	case aaru.DataTypeCdSubchannel:
		return "CD Sector Subchannel"
	case aaru.DataTypeAppleProfileTag:
		return "Apple Profile Tag"
	case aaru.DataTypeAppleSonyTag:
		return "Apple Sony Tag"
	case aaru.DataTypePriamDataTowerTag:
		return "Priam DataTower Tag"
	case aaru.DataTypeCdMcn:
		return "CD Media Catalogue Number"
	case aaru.DataTypeCdSectorPrefixCorrected:
		return "CD Sector Prefix (corrected)"
	case aaru.DataTypeCdSectorSuffixCorrected:
		return "CD Sector Suffix (corrected)"
	case aaru.DataTypeCdSubHeader:
		return "CD Mode 2 Subheader"
	case aaru.DataTypeCdLeadIn:
		return "CD Lead-in"
	case aaru.DataTypeDvdDiscKeyDecrypted:
		return "DVD Disc Key (decrypted)"
	case aaru.DataTypeDvdSectorCprMai:
		return "DVD Sector Copyright Management Info"
	case aaru.DataTypeDvdTitleKeyDecrypted:
		return "DVD Title Key (decrypted)"
	case aaru.DataTypeDvdSectorId:
		return "DVD Sector ID"
	case aaru.DataTypeDvdSectorIed:
		return "DVD Sector ID Error Detection"
	case aaru.DataTypeDvdSectorEdc:
		return "DVD Sector Error Detection Code"
	case aaru.DataTypeDvdSectorEccPi:
		return "DVD Sector ECC Parity Inner"
	case aaru.DataTypeDvdEccBlockPo:
		return "DVD ECC Block Parity Outer"
	case aaru.DataTypeDvdPfi2ndLayer:
		return "DVD Physical Format Info (2nd layer)"
	case aaru.DataTypeFloppyWriteProtect:
		return "Floppy Write-Protect Status"
	case aaru.DataTypeAacsMediaKey:
		return "AACS Media Key"
	case aaru.DataTypeAacsVolumeUniqueKey:
		return "AACS Volume Unique Key"
	case aaru.DataTypeBdSectorEdc:
		return "Blu-ray Sector EDC"
	default:
		return "Unknown Data Type"
	}
}

// mediaTypeString returns the human-readable name of t.
func mediaTypeString(t media.Type) string {
	switch t {
	case media.UnknownMedia:
		return "Unknown"
	case media.UnknownMO:
		return "Unknown Magneto-Optical"
	case media.GENERIC_HDD:
		return "Generic Hard Disk"
	case media.Microdrive:
		return "Microdrive"
	case media.Zone_HDD:
		return "Zoned Hard Disk"
	case media.FlashDrive:
		return "USB Flash Drive"
	case media.CD:
		return "Compact Disc"
	case media.CDDA:
		return "CD Digital Audio (Red Book)"
	case media.CDG:
		return "CD+G"
	case media.CDEG:
		return "CD+EG"
	case media.CDI:
		return "CD-i (Green Book)"
	case media.CDROM:
		return "CD-ROM (Yellow Book)"
	case media.CDROMXA:
		return "CD-ROM XA"
	case media.CDPLUS:
		return "CD+ (Blue Book)"
	case media.CDMO:
		return "CD-MO (Orange Book)"
	case media.CDR:
		return "CD-R"
	case media.CDRW:
		return "CD-RW"
	case media.CDMRW:
		return "Mount-Rainier CD-RW"
	case media.VCD:
		return "Video CD"
	case media.SVCD:
		return "Super Video CD"
	case media.PCD:
		return "Photo CD"
	case media.SACD:
		return "Super Audio CD"
	case media.DDCD:
		return "Double-Density CD-ROM"
	case media.DDCDR:
		return "DD CD-R"
	case media.DDCDRW:
		return "DD CD-RW"
	case media.DTSCD:
		return "DTS CD"
	case media.CDMIDI:
		return "CD-MIDI"
	case media.CDV:
		return "CD-Video"
	case media.PD650:
		return "PD650"
	case media.PD650_WORM:
		return "PD650 WORM"
	case media.CDIREADY:
		return "CD-i Ready"
	case media.FMTOWNS:
		return "FM Towns CD"
	case media.DVDROM:
		return "DVD-ROM"
	case media.DVDR:
		return "DVD-R"
	case media.DVDRW:
		return "DVD-RW"
	case media.DVDPR:
		return "DVD+R"
	case media.DVDPRW:
		return "DVD+RW"
	case media.DVDPRWDL:
		return "DVD+RW DL"
	case media.DVDRDL:
		return "DVD-R DL"
	case media.DVDPRDL:
		return "DVD+R DL"
	case media.DVDRAM:
		return "DVD-RAM"
	case media.DVDRWDL:
		return "DVD-RW DL"
	case media.DVDDownload:
		return "DVD-Download"
	case media.HDDVDROM:
		return "HD DVD-ROM"
	case media.HDDVDRAM:
		return "HD DVD-RAM"
	case media.HDDVDR:
		return "HD DVD-R"
	case media.HDDVDRW:
		return "HD DVD-RW"
	case media.HDDVDRDL:
		return "HD DVD-R DL"
	case media.HDDVDRWDL:
		return "HD DVD-RW DL"
	case media.BDROM:
		return "BD-ROM"
	case media.BDR:
		return "BD-R"
	case media.BDRE:
		return "BD-RE"
	case media.BDRXL:
		return "BD-R XL"
	case media.BDREXL:
		return "BD-RE XL"
	case media.EVD:
		return "Enhanced Versatile Disc"
	case media.FVD:
		return "Forward Versatile Disc"
	case media.HVD:
		return "Holographic Versatile Disc"
	case media.CBHD:
		return "China Blue High Definition"
	case media.HDVMD:
		return "High Definition Versatile Multilayer Disc"
	case media.VCDHD:
		return "Versatile Compact Disc High Density"
	case media.SVOD:
		return "Stacked Volumetric Optical Disc"
	case media.FDDVD:
		return "Five Dimensional Disc"
	case media.LD:
		return "LaserDisc"
	case media.LDROM:
		return "LaserDisc Data"
	case media.LDROM2:
		return "LaserDisc ROM 2"
	case media.LVROM:
		return "LVROM"
	case media.MegaLD:
		return "MegaLD"
	case media.HiMD:
		return "Hi-MD"
	case media.MD:
		return "MiniDisc"
	case media.MDData:
		return "MiniDisc DATA"
	case media.MDData2:
		return "MiniDisc DATA 2"
	case media.UDO:
		return "Ultra Density Optical"
	case media.UDO2:
		return "Ultra Density Optical 2"
	case media.UDO2_WORM:
		return "Ultra Density Optical 2 WORM"
	case media.PlayStationMemoryCard:
		return "PlayStation Memory Card"
	case media.PlayStationMemoryCard2:
		return "PlayStation 2 Memory Card"
	case media.PS1CD:
		return "PlayStation Game CD"
	case media.PS2CD:
		return "PlayStation 2 Game CD"
	case media.PS2DVD:
		return "PlayStation 2 Game DVD"
	case media.PS3DVD:
		return "PlayStation 3 Game DVD"
	case media.PS3BD:
		return "PlayStation 3 Game Blu-ray"
	case media.PS4BD:
		return "PlayStation 4 Game Blu-ray"
	case media.UMD:
		return "Universal Media Disc"
	case media.PlayStationVitaGameCard:
		return "PS Vita Game Card"
	case media.XGD:
		return "Xbox Game Disc"
	case media.XGD2:
		return "Xbox 360 Game Disc"
	case media.XGD3:
		return "Xbox 360 Game Disc v3"
	case media.XGD4:
		return "Xbox One Game Disc"
	case media.MEGACD:
		return "Mega CD"
	case media.SATURNCD:
		return "Saturn CD"
	case media.GDROM:
		return "GD-ROM"
	case media.GDR:
		return "GD-R"
	case media.SegaCard:
		return "Sega Card"
	case media.MilCD:
		return "MIL-CD"
	case media.HuCard:
		return "HuCard"
	case media.SuperCDROM2:
		return "Super CD-ROM²"
	case media.JaguarCD:
		return "Jaguar CD"
	case media.ThreeDO:
		return "3DO CD"
	case media.PCFX:
		return "PC-FX"
	case media.NeoGeoCD:
		return "Neo Geo CD"
	case media.CDTV:
		return "CDTV"
	case media.CD32:
		return "CD32"
	case media.Nuon:
		return "Nuon"
	case media.Playdia:
		return "Playdia"
	case media.Apple32SS:
		return "Apple 5.25\" SS DD 13 spt"
	case media.Apple32DS:
		return "Apple 5.25\" DS DD 13 spt"
	case media.Apple33SS:
		return "Apple 5.25\" SS DD 16 spt"
	case media.Apple33DS:
		return "Apple 5.25\" DS DD 16 spt"
	case media.AppleSonySS:
		return "Apple 3.5\" SS DD GCR"
	case media.AppleSonyDS:
		return "Apple 3.5\" DS DD GCR"
	case media.AppleFileWare:
		return "Apple FileWare (Twiggy)"
	case media.DOS_525_SS_DD_8:
		return "5.25\" SS DD 8 spt"
	case media.DOS_525_SS_DD_9:
		return "5.25\" SS DD 9 spt"
	case media.DOS_525_DS_DD_8:
		return "5.25\" DS DD 8 spt"
	case media.DOS_525_DS_DD_9:
		return "5.25\" DS DD 9 spt"
	case media.DOS_525_HD:
		return "5.25\" DS HD"
	case media.DOS_35_SS_DD_8:
		return "3.5\" SS DD 8 spt"
	case media.DOS_35_SS_DD_9:
		return "3.5\" SS DD 9 spt"
	case media.DOS_35_DS_DD_8:
		return "3.5\" DS DD 8 spt"
	case media.DOS_35_DS_DD_9:
		return "3.5\" DS DD 9 spt"
	case media.DOS_35_HD:
		return "3.5\" DS HD"
	case media.DOS_35_ED:
		return "3.5\" DS ED"
	case media.DMF:
		return "DMF (3.5\" 21 spt)"
	case media.DMF_82:
		return "DMF 82 tracks"
	case media.XDF_525:
		return "XDF 5.25\""
	case media.XDF_35:
		return "XDF 3.5\""
	case media.IBM23FD:
		return "IBM 23FD"
	case media.IBM33FD_128:
		return "IBM 33FD 128 bps"
	case media.IBM33FD_256:
		return "IBM 33FD 256 bps"
	case media.IBM33FD_512:
		return "IBM 33FD 512 bps"
	case media.IBM43FD_128:
		return "IBM 43FD 128 bps"
	case media.IBM43FD_256:
		return "IBM 43FD 256 bps"
	case media.IBM53FD_256:
		return "IBM 53FD 256 bps"
	case media.IBM53FD_512:
		return "IBM 53FD 512 bps"
	case media.IBM53FD_1024:
		return "IBM 53FD 1024 bps"
	case media.RX01:
		return "RX01"
	case media.RX02:
		return "RX02"
	case media.RX03:
		return "RX03"
	case media.RX50:
		return "RX50"
	case media.ACORN_525_SS_SD_40:
		return "Acorn 5.25\" SS SD 40 tracks"
	case media.ACORN_525_SS_SD_80:
		return "Acorn 5.25\" SS SD 80 tracks"
	case media.ACORN_525_SS_DD_40:
		return "Acorn 5.25\" SS DD 40 tracks"
	case media.ACORN_525_SS_DD_80:
		return "Acorn 5.25\" SS DD 80 tracks"
	case media.ACORN_525_DS_DD:
		return "Acorn 5.25\" DS DD"
	case media.ACORN_35_DS_DD:
		return "Acorn 3.5\" DS DD"
	case media.ACORN_35_DS_HD:
		return "Acorn 3.5\" DS HD"
	case media.ATARI_525_SD:
		return "Atari 5.25\" SD"
	case media.ATARI_525_ED:
		return "Atari 5.25\" ED"
	case media.ATARI_525_DD:
		return "Atari 5.25\" DD"
	case media.ATARI_35_SS_DD:
		return "Atari 3.5\" SS DD"
	case media.ATARI_35_DS_DD:
		return "Atari 3.5\" DS DD"
	case media.ATARI_35_SS_DD_11:
		return "Atari 3.5\" SS DD 11 spt"
	case media.ATARI_35_DS_DD_11:
		return "Atari 3.5\" DS DD 11 spt"
	case media.CBM_35_DD:
		return "Commodore 3.5\" DD (1581)"
	case media.CBM_AMIGA_35_DD:
		return "Amiga 3.5\" DD"
	case media.CBM_AMIGA_35_HD:
		return "Amiga 3.5\" HD"
	case media.CBM_1540:
		return "Commodore 1540/1541"
	case media.CBM_1540_Ext:
		return "Commodore 1540 Extended"
	case media.CBM_1571:
		return "Commodore 1571"
	case media.NEC_8_SD:
		return "NEC 8\" SD"
	case media.NEC_8_DD:
		return "NEC 8\" DD"
	case media.NEC_525_SS:
		return "NEC 5.25\" SS"
	case media.NEC_525_DS:
		return "NEC 5.25\" DS"
	case media.NEC_525_HD:
		return "NEC 5.25\" HD"
	case media.NEC_35_HD_8:
		return "NEC 3.5\" HD 8 spt"
	case media.NEC_35_HD_15:
		return "NEC 3.5\" HD 15 spt"
	case media.NEC_35_TD:
		return "NEC 3.5\" TD"
	case media.SHARP_525_9:
		return "Sharp 5.25\" 9 spt"
	case media.SHARP_35_9:
		return "Sharp 3.5\" 9 spt"
	case media.ECMA_99_8:
		return "ECMA-99 8 spt"
	case media.ECMA_99_15:
		return "ECMA-99 15 spt"
	case media.ECMA_99_26:
		return "ECMA-99 26 spt"
	case media.ECMA_54:
		return "ECMA-54"
	case media.ECMA_59:
		return "ECMA-59"
	case media.ECMA_66:
		return "ECMA-66"
	case media.ECMA_69_8:
		return "ECMA-69 8 spt"
	case media.ECMA_69_15:
		return "ECMA-69 15 spt"
	case media.ECMA_69_26:
		return "ECMA-69 26 spt"
	case media.ECMA_70:
		return "ECMA-70"
	case media.ECMA_78:
		return "ECMA-78"
	case media.ECMA_78_2:
		return "ECMA-78-2"
	case media.FDFORMAT_525_DD:
		return "FDFORMAT 5.25\" DD"
	case media.FDFORMAT_525_HD:
		return "FDFORMAT 5.25\" HD"
	case media.FDFORMAT_35_DD:
		return "FDFORMAT 3.5\" DD"
	case media.FDFORMAT_35_HD:
		return "FDFORMAT 3.5\" HD"
	case media.Apricot_35:
		return "Apricot 3.5\""
	case media.ADR2120:
		return "ADR 2120"
	case media.ADR260:
		return "ADR 260"
	case media.ADR30:
		return "ADR 30"
	case media.ADR50:
		return "ADR 50"
	case media.AIT1:
		return "AIT-1"
	case media.AIT1Turbo:
		return "AIT-1 Turbo"
	case media.AIT2:
		return "AIT-2"
	case media.AIT2Turbo:
		return "AIT-2 Turbo"
	case media.AIT3:
		return "AIT-3"
	case media.AIT3Ex:
		return "AIT-3 Ex"
	case media.AIT3Turbo:
		return "AIT-3 Turbo"
	case media.AIT4:
		return "AIT-4"
	case media.AIT5:
		return "AIT-5"
	case media.AITETurbo:
		return "AITE Turbo"
	case media.SAIT1:
		return "SAIT-1"
	case media.SAIT2:
		return "SAIT-2"
	case media.Bernoulli:
		return "Bernoulli"
	case media.Bernoulli2:
		return "Bernoulli II"
	case media.Ditto:
		return "Ditto"
	case media.DittoMax:
		return "Ditto Max"
	case media.Jaz:
		return "Jaz"
	case media.Jaz2:
		return "Jaz 2"
	case media.PocketZip:
		return "PocketZip"
	case media.REV120:
		return "REV 120"
	case media.REV35:
		return "REV 35"
	case media.REV70:
		return "REV 70"
	case media.ZIP100:
		return "Zip 100"
	case media.ZIP250:
		return "Zip 250"
	case media.ZIP750:
		return "Zip 750"
	case media.CompactCassette:
		return "Compact Cassette"
	case media.Data8:
		return "Data8"
	case media.MiniDV:
		return "MiniDV"
	case media.Dcas25:
		return "D/CAS-25"
	case media.Dcas85:
		return "D/CAS-85"
	case media.Dcas103:
		return "D/CAS-103"
	case media.CFast:
		return "CFast"
	case media.CompactFlash:
		return "CompactFlash"
	case media.CompactFlashType2:
		return "CompactFlash Type II"
	case media.DigitalAudioTape:
		return "Digital Audio Tape"
	case media.DAT160:
		return "DAT 160"
	case media.DAT320:
		return "DAT 320"
	case media.DAT72:
		return "DAT 72"
	case media.DDS1:
		return "DDS-1"
	case media.DDS2:
		return "DDS-2"
	case media.DDS3:
		return "DDS-3"
	case media.DDS4:
		return "DDS-4"
	case media.CompactTapeI:
		return "CompactTape I"
	case media.CompactTapeII:
		return "CompactTape II"
	case media.DECtapeII:
		return "DECtape II"
	case media.DLTtapeIII:
		return "DLTtape III"
	case media.DLTtapeIIIxt:
		return "DLTtape IIIxt"
	case media.DLTtapeIV:
		return "DLTtape IV"
	case media.DLTtapeS4:
		return "DLTtape S4"
	case media.SDLT1:
		return "SDLT-1"
	case media.SDLT2:
		return "SDLT-2"
	case media.VStapeI:
		return "VStape I"
	case media.Exatape15m:
		return "Exatape 15m"
	case media.Exatape22m:
		return "Exatape 22m"
	case media.Exatape22mAME:
		return "Exatape 22m AME"
	case media.Exatape28m:
		return "Exatape 28m"
	case media.Exatape40m:
		return "Exatape 40m"
	case media.Exatape45m:
		return "Exatape 45m"
	case media.Exatape54m:
		return "Exatape 54m"
	case media.Exatape75m:
		return "Exatape 75m"
	case media.Exatape76m:
		return "Exatape 76m"
	case media.Exatape80m:
		return "Exatape 80m"
	case media.Exatape106m:
		return "Exatape 106m"
	case media.Exatape160mXL:
		return "Exatape 160m XL"
	case media.Exatape112m:
		return "Exatape 112m"
	case media.Exatape125m:
		return "Exatape 125m"
	case media.Exatape150m:
		return "Exatape 150m"
	case media.Exatape170m:
		return "Exatape 170m"
	case media.Exatape225m:
		return "Exatape 225m"
	case media.ExpressCard34:
		return "ExpressCard/34"
	case media.ExpressCard54:
		return "ExpressCard/54"
	case media.PCCardTypeI:
		return "PC Card Type I"
	case media.PCCardTypeII:
		return "PC Card Type II"
	case media.PCCardTypeIII:
		return "PC Card Type III"
	case media.PCCardTypeIV:
		return "PC Card Type IV"
	case media.EZ135:
		return "EZ 135"
	case media.EZ230:
		return "EZ 230"
	case media.Quest:
		return "Quest"
	case media.SparQ:
		return "SparQ"
	case media.SQ100:
		return "SyQuest 100"
	case media.SQ200:
		return "SyQuest 200"
	case media.SQ300:
		return "SyQuest 300"
	case media.SQ310:
		return "SyQuest 310"
	case media.SQ327:
		return "SyQuest 327"
	case media.SQ400:
		return "SyQuest 400"
	case media.SQ800:
		return "SyQuest 800"
	case media.SQ1500:
		return "SyQuest 1500"
	case media.SQ2000:
		return "SyQuest 2000"
	case media.SyJet:
		return "SyJet"
	case media.FamicomGamePak:
		return "Famicom Cartridge"
	case media.GameBoyAdvanceGamePak:
		return "Game Boy Advance Cartridge"
	case media.GameBoyGamePak:
		return "Game Boy Cartridge"
	case media.GOD:
		return "GameCube Optical Disc"
	case media.N64DD:
		return "Nintendo 64DD"
	case media.N64GamePak:
		return "Nintendo 64 Cartridge"
	case media.NESGamePak:
		return "NES Cartridge"
	case media.Nintendo3DSGameCard:
		return "Nintendo 3DS Game Card"
	case media.NintendoDiskCard:
		return "Famicom Disk System"
	case media.NintendoDSGameCard:
		return "Nintendo DS Game Card"
	case media.NintendoDSiGameCard:
		return "Nintendo DSi Game Card"
	case media.SNESGamePak:
		return "SNES Cartridge"
	case media.SNESGamePakUS:
		return "SNES Cartridge (US)"
	case media.WOD:
		return "Wii Optical Disc"
	case media.WUOD:
		return "Wii U Optical Disc"
	case media.SwitchGameCard:
		return "Switch Game Card"
	case media.IBM3470:
		return "IBM 3470"
	case media.IBM3480:
		return "IBM 3480"
	case media.IBM3490:
		return "IBM 3490"
	case media.IBM3490E:
		return "IBM 3490E"
	case media.IBM3592:
		return "IBM 3592"
	case media.LTO:
		return "LTO Ultrium"
	case media.LTO2:
		return "LTO-2"
	case media.LTO3:
		return "LTO-3"
	case media.LTO3WORM:
		return "LTO-3 WORM"
	case media.LTO4:
		return "LTO-4"
	case media.LTO4WORM:
		return "LTO-4 WORM"
	case media.LTO5:
		return "LTO-5"
	case media.LTO5WORM:
		return "LTO-5 WORM"
	case media.LTO6:
		return "LTO-6"
	case media.LTO6WORM:
		return "LTO-6 WORM"
	case media.LTO7:
		return "LTO-7"
	case media.LTO7WORM:
		return "LTO-7 WORM"
	case media.MemoryStick:
		return "Memory Stick"
	case media.MemoryStickDuo:
		return "Memory Stick Duo"
	case media.MemoryStickMicro:
		return "Memory Stick Micro"
	case media.MemoryStickPro:
		return "Memory Stick PRO"
	case media.MemoryStickProDuo:
		return "Memory Stick PRO Duo"
	case media.MicroSD:
		return "microSD"
	case media.MiniSD:
		return "miniSD"
	case media.SecureDigital:
		return "Secure Digital"
	case media.MMC:
		return "MultiMediaCard"
	case media.MMCmicro:
		return "MMCmicro"
	case media.RSMMC:
		return "RS-MMC"
	case media.MMCplus:
		return "MMCplus"
	case media.MMCmobile:
		return "MMCmobile"
	case media.MLR1:
		return "MLR1"
	case media.MLR1SL:
		return "MLR1 SL"
	case media.MLR3:
		return "MLR3"
	case media.SLR1:
		return "SLR1"
	case media.SLR2:
		return "SLR2"
	case media.SLR3:
		return "SLR3"
	case media.SLR32:
		return "SLR32"
	case media.SLR32SL:
		return "SLR32 SL"
	case media.SLR4:
		return "SLR4"
	case media.SLR5:
		return "SLR5"
	case media.SLR5SL:
		return "SLR5 SL"
	case media.SLR6:
		return "SLR6"
	case media.SLRtape7:
		return "SLRtape 7"
	case media.SLRtape7SL:
		return "SLRtape 7 SL"
	case media.SLRtape24:
		return "SLRtape 24"
	case media.SLRtape24SL:
		return "SLRtape 24 SL"
	case media.SLRtape40:
		return "SLRtape 40"
	case media.SLRtape50:
		return "SLRtape 50"
	case media.SLRtape60:
		return "SLRtape 60"
	case media.SLRtape75:
		return "SLRtape 75"
	case media.SLRtape100:
		return "SLRtape 100"
	case media.SLRtape140:
		return "SLRtape 140"
	case media.QIC11:
		return "QIC-11"
	case media.QIC120:
		return "QIC-120"
	case media.QIC1350:
		return "QIC-1350"
	case media.QIC150:
		return "QIC-150"
	case media.QIC24:
		return "QIC-24"
	case media.QIC3010:
		return "QIC-3010"
	case media.QIC3020:
		return "QIC-3020"
	case media.QIC3080:
		return "QIC-3080"
	case media.QIC3095:
		return "QIC-3095"
	case media.QIC320:
		return "QIC-320"
	case media.QIC40:
		return "QIC-40"
	case media.QIC525:
		return "QIC-525"
	case media.QIC80:
		return "QIC-80"
	case media.STK4480:
		return "STK 4480"
	case media.STK4490:
		return "STK 4490"
	case media.STK9490:
		return "STK 9490"
	case media.T9840A:
		return "T9840A"
	case media.T9840B:
		return "T9840B"
	case media.T9840C:
		return "T9840C"
	case media.T9840D:
		return "T9840D"
	case media.T9940A:
		return "T9940A"
	case media.T9940B:
		return "T9940B"
	case media.T10000A:
		return "T10000A"
	case media.T10000B:
		return "T10000B"
	case media.T10000C:
		return "T10000C"
	case media.T10000D:
		return "T10000D"
	case media.Travan1:
		return "Travan"
	case media.Travan1Ex:
		return "Travan-1 Ex"
	case media.Travan3:
		return "Travan-3"
	case media.Travan3Ex:
		return "Travan-3 Ex"
	case media.Travan4:
		return "Travan-4"
	case media.Travan5:
		return "Travan-5"
	case media.Travan7:
		return "Travan-7"
	case media.VXA1:
		return "VXA-1"
	case media.VXA2:
		return "VXA-2"
	case media.VXA3:
		return "VXA-3"
	case media.ECMA_153:
		return "ECMA-153 (M.O.)"
	case media.ECMA_153_512:
		return "ECMA-153 512 bps (M.O.)"
	case media.ECMA_154:
		return "ECMA-154 (M.O.)"
	case media.ECMA_183_512:
		return "ECMA-183 512 bps (M.O.)"
	case media.ECMA_183:
		return "ECMA-183 (M.O.)"
	case media.ECMA_184_512:
		return "ECMA-184 512 bps (M.O.)"
	case media.ECMA_184:
		return "ECMA-184 (M.O.)"
	case media.ECMA_189:
		return "ECMA-189 (M.O.)"
	case media.ECMA_190:
		return "ECMA-190 (M.O.)"
	case media.ECMA_195:
		return "ECMA-195 (M.O.)"
	case media.ECMA_195_512:
		return "ECMA-195 512 bps (M.O.)"
	case media.ECMA_201:
		return "ECMA-201 (M.O.)"
	case media.ECMA_201_ROM:
		return "ECMA-201 ROM (M.O.)"
	case media.ECMA_223:
		return "ECMA-223 (M.O.)"
	case media.ECMA_223_512:
		return "ECMA-223 512 bps (M.O.)"
	case media.ECMA_238:
		return "ECMA-238 (M.O.)"
	case media.ECMA_239:
		return "ECMA-239 (M.O.)"
	case media.ECMA_260:
		return "ECMA-260 (M.O.)"
	case media.ECMA_260_Double:
		return "ECMA-260 Double (M.O.)"
	case media.ECMA_280:
		return "ECMA-280 (M.O.)"
	case media.ECMA_317:
		return "ECMA-317 (M.O.)"
	case media.ECMA_322:
		return "ECMA-322 (M.O.)"
	case media.ECMA_322_2k:
		return "ECMA-322 2k (M.O.)"
	case media.GigaMo:
		return "GigaMo"
	case media.GigaMo2:
		return "GigaMo 2"
	case media.CompactFloppy:
		return "Compact Floppy"
	case media.DemiDiskette:
		return "Demi Diskette"
	case media.Floptical:
		return "Floptical"
	case media.HiFD:
		return "HiFD"
	case media.QuickDisk:
		return "QuickDisk"
	case media.UHD144:
		return "UHD 144"
	case media.VideoFloppy:
		return "Video Floppy"
	case media.Wafer:
		return "Wafer"
	case media.ZXMicrodrive:
		return "ZX Microdrive"
	case media.BeeCard:
		return "BeeCard"
	case media.Borsu:
		return "Borsu"
	case media.DataStore:
		return "DataStore"
	case media.DIR:
		return "DIR"
	case media.DST:
		return "DST"
	case media.DTF:
		return "DTF"
	case media.DTF2:
		return "DTF2"
	case media.Flextra3020:
		return "Flextra 3020"
	case media.Flextra3225:
		return "Flextra 3225"
	case media.HiTC1:
		return "HiTC1"
	case media.HiTC2:
		return "HiTC2"
	case media.LT1:
		return "LT-1"
	case media.MiniCard:
		return "MiniCard"
	case media.Orb:
		return "Orb"
	case media.Orb5:
		return "Orb 5"
	case media.SmartMedia:
		return "SmartMedia"
	case media.XD:
		return "xD-Picture Card"
	case media.XQD:
		return "XQD"
	case media.DataPlay:
		return "DataPlay"
	case media.AppleProfile:
		return "Apple Profile"
	case media.AppleWidget:
		return "Apple Widget"
	case media.AppleHD20:
		return "Apple HD20"
	case media.PriamDataTower:
		return "Priam DataTower"
	case media.Pippin:
		return "Pippin"
	case media.RA60:
		return "RA60"
	case media.RA80:
		return "RA80"
	case media.RA81:
		return "RA81"
	case media.RC25:
		return "RC25"
	case media.RD31:
		return "RD31"
	case media.RD32:
		return "RD32"
	case media.RD51:
		return "RD51"
	case media.RD52:
		return "RD52"
	case media.RD53:
		return "RD53"
	case media.RD54:
		return "RD54"
	case media.RK06:
		return "RK06"
	case media.RK06_18:
		return "RK06 18-bit"
	case media.RK07:
		return "RK07"
	case media.RK07_18:
		return "RK07 18-bit"
	case media.RM02:
		return "RM02"
	case media.RM03:
		return "RM03"
	case media.RM05:
		return "RM05"
	case media.RP02:
		return "RP02"
	case media.RP02_18:
		return "RP02 18-bit"
	case media.RP03:
		return "RP03"
	case media.RP03_18:
		return "RP03 18-bit"
	case media.RP04:
		return "RP04"
	case media.RP04_18:
		return "RP04 18-bit"
	case media.RP05:
		return "RP05"
	case media.RP05_18:
		return "RP05 18-bit"
	case media.RP06:
		return "RP06"
	case media.RP06_18:
		return "RP06 18-bit"
	case media.LS120:
		return "LS-120 SuperDisk"
	case media.LS240:
		return "LS-240 SuperDisk"
	case media.FD32MB:
		return "32MB Floppy"
	case media.RDX:
		return "RDX"
	case media.RDX320:
		return "RDX 320"
	case media.VideoNow:
		return "VideoNow"
	case media.VideoNowColor:
		return "VideoNow Color"
	case media.VideoNowXp:
		return "VideoNow XP"
	case media.Bernoulli10:
		return "Bernoulli Box (10Mb)"
	case media.Bernoulli20:
		return "Bernoulli Box (20Mb)"
	case media.BernoulliBox2_20:
		return "Bernoulli Box II (20Mb)"
	case media.KodakVerbatim3:
		return "Kodak/Verbatim (3Mb)"
	case media.KodakVerbatim6:
		return "Kodak/Verbatim (6Mb)"
	case media.KodakVerbatim12:
		return "Kodak/Verbatim (12Mb)"
	case media.ProfessionalDisc:
		return "Professional Disc for video"
	case media.ProfessionalDiscDual:
		return "Professional Disc for video"
	case media.ProfessionalDiscTriple:
		return "Professional Disc for video"
	case media.ProfessionalDiscQuad:
		return "Professional Disc for video"
	case media.PDD:
		return "Professional Disc for DATA"
	case media.PDD_WORM:
		return "Professional Disc for DATA"
	case media.ArchivalDisc:
		return "Archival Disc"
	case media.ArchivalDisc2:
		return "Archival Disc"
	case media.ArchivalDisc3:
		return "Archival Disc"
	case media.ODC300R:
		return "Optical Disc archive"
	case media.ODC300RE:
		return "Optical Disc archive"
	case media.ODC600R:
		return "Optical Disc archive"
	case media.ODC600RE:
		return "Optical Disc archive"
	case media.ODC1200RE:
		return "Optical Disc archive"
	case media.ODC1500R:
		return "Optical Disc archive"
	case media.ODC3300R:
		return "Optical Disc archive"
	case media.ODC5500R:
		return "Optical Disc archive"
	case media.ECMA_322_1k:
		return "5,25\", M.O., 4383356 sectors, 1024 bytes/sector, ECMA-322, ISO 22092, 9.1Gb/cart"
	case media.ECMA_322_512:
		return "5,25\", M.O., ??????? sectors, 512 bytes/sector, ECMA-322, ISO 22092, 9.1Gb/cart"
	case media.ISO_14517:
		return "5,25\", M.O., 1273011 sectors, 1024 bytes/sector, ISO 14517, 2.6Gb/cart"
	case media.ISO_14517_512:
		return "5,25\", M.O., 2244958 sectors, 512 bytes/sector, ISO 14517, 2.3Gb/cart"
	case media.ISO_15041_512:
		return "3,5\", M.O., 1041500 sectors, 512 bytes/sector, ISO 15041, 540Mb/cart"
	case media.HSM650:
		return "Sony HyperStorage"
	case media.MetaFloppy_Mod_I:
		return "5.25\", SS, DD, 35 tracks, 16 spt, 256 bytes/sector, MFM, 48 tpi, ???rpm"
	case media.HF12:
		return "HyperFlex (12Mb)"
	case media.HF24:
		return "HyperFlex (24Mb)"
	case media.AtariLynxCard:
		return "Atari Lynx card"
	case media.AtariJaguarCartridge:
		return "Atari Jaguar cartridge"
	default:
		return "Unknown Media Type"
	}
}

// sectorTagTypeString returns the human-readable name of t.
func sectorTagTypeString(t aaru.SectorTagType) string {
	switch t {
	case aaru.SectorTagAppleSony:
		return "Apple Sony Tag"
	case aaru.SectorTagCdSync:
		return "CD Sector Sync"
	case aaru.SectorTagCdHeader:
		return "CD Sector Header"
	case aaru.SectorTagCdSubHeader:
		return "CD Sector Sub-Header"
	case aaru.SectorTagCdEdc:
		return "CD Sector EDC"
	case aaru.SectorTagCdEccP:
		return "CD Sector ECC P"
	case aaru.SectorTagCdEccQ:
		return "CD Sector ECC Q"
	case aaru.SectorTagCdEcc:
		return "CD Sector ECC (P+Q)"
	case aaru.SectorTagCdSubchannel:
		return "CD Sector Subchannel"
	case aaru.SectorTagCdTrackIsrc:
		return "CD Track ISRC"
	case aaru.SectorTagCdTrackText:
		return "CD Track Text"
	case aaru.SectorTagCdTrackFlags:
		return "CD Track Flags"
	case aaru.SectorTagDvdCmi:
		return "DVD Copyright Management Information"
	case aaru.SectorTagFloppyAddressMark:
		return "Floppy Address Mark"
	case aaru.SectorTagDvdTitleKey:
		return "DVD Sector Title Key"
	case aaru.SectorTagDvdTitleKeyDecrypted:
		return "DVD Title Key (Decrypted)"
	case aaru.SectorTagDvdSectorInformation:
		return "DVD Sector Information"
	case aaru.SectorTagDvdSectorNumber:
		return "DVD Sector Number"
	case aaru.SectorTagDvdSectorIed:
		return "DVD Sector IED"
	case aaru.SectorTagDvdSectorEdc:
		return "DVD Sector EDC"
	case aaru.SectorTagAppleProfile:
		return "Apple Profile Tag"
	case aaru.SectorTagPriamDataTower:
		return "Priam DataTower Tag"
	case aaru.SectorTagBdSectorEdc:
		return "Blu-ray Sector EDC"
	default:
		return "Unknown Sector Tag"
	}
}
