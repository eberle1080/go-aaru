// AaruFormat enumeration constants.

package aaru

import "fmt"

// CompressionType identifies the compression algorithm of a block payload.
type CompressionType uint16

const (
	// CompressionNone: Not compressed.
	CompressionNone CompressionType = 0
	// CompressionLzma: LZMA compression.
	CompressionLzma CompressionType = 1
	// CompressionFlac: FLAC compression.
	CompressionFlac CompressionType = 2
	// CompressionLzmaCst: LZMA applied to CD Subchannel Transform processed data.
	CompressionLzmaCst CompressionType = 3
	// CompressionZstd: Zstandard compression.
	CompressionZstd CompressionType = 4
	// CompressionZstdCst: Zstandard applied to CD Subchannel Transform processed data.
	CompressionZstdCst CompressionType = 5
)

var _CompressionTypeNames = map[CompressionType]string{
	CompressionNone:    "CompressionNone",
	CompressionLzma:    "CompressionLzma",
	CompressionFlac:    "CompressionFlac",
	CompressionLzmaCst: "CompressionLzmaCst",
	CompressionZstd:    "CompressionZstd",
	CompressionZstdCst: "CompressionZstdCst",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t CompressionType) String() string {
	if s, ok := _CompressionTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("CompressionType(%d)", uint16(t))
}

// DataType classifies the content of a block.
type DataType uint16

const (
	// DataTypeNone: No data.
	DataTypeNone DataType = 0
	// DataTypeUserData: User (main) data.
	DataTypeUserData DataType = 1
	// DataTypeCdToc: Compact Disc partial Table of Contents.
	DataTypeCdToc DataType = 2
	// DataTypeSessionInfo: Compact Disc session information.
	DataTypeSessionInfo DataType = 3
	// DataTypeFullToc: Compact Disc full Table of Contents.
	DataTypeFullToc DataType = 4
	// DataTypeCdPma: Compact Disc Power Management Area (PMA).
	DataTypeCdPma DataType = 5
	// DataTypeCdAtip: Compact Disc Absolute Time In Pregroove (ATIP).
	DataTypeCdAtip DataType = 6
	// DataTypeCdText: Compact Disc lead-in CD-Text.
	DataTypeCdText DataType = 7
	// DataTypeDvdPfi: DVD Physical Format Information.
	DataTypeDvdPfi DataType = 8
	// DataTypeDvdCmi: DVD lead-in Copyright Management Information (CMI).
	DataTypeDvdCmi DataType = 9
	// DataTypeDvdDiscKey: DVD disc key.
	DataTypeDvdDiscKey DataType = 10
	// DataTypeDvdBca: DVD Burst Cutting Area (BCA).
	DataTypeDvdBca DataType = 11
	// DataTypeDvdDmi: DVD Disc Manufacturing Information (DMI).
	DataTypeDvdDmi DataType = 12
	// DataTypeDvdMediaIdentifier: DVD media identifier.
	DataTypeDvdMediaIdentifier DataType = 13
	// DataTypeDvdMkb: DVD Media Key Block (MKB).
	DataTypeDvdMkb DataType = 14
	// DataTypeDvdRamDds: DVD-RAM Disc Definition Structure (DDS).
	DataTypeDvdRamDds DataType = 15
	// DataTypeDvdRamMediumStatus: DVD-RAM medium status.
	DataTypeDvdRamMediumStatus DataType = 16
	// DataTypeDvdRamSpareArea: DVD-RAM spare area information.
	DataTypeDvdRamSpareArea DataType = 17
	// DataTypeDvdrRmd: DVD-R RMD (Recording Management Data).
	DataTypeDvdrRmd DataType = 18
	// DataTypeDvdrPreRecordedInfo: DVD-R pre‑recorded information.
	DataTypeDvdrPreRecordedInfo DataType = 19
	// DataTypeDvdrMediaIdentifier: DVD-R media identifier.
	DataTypeDvdrMediaIdentifier DataType = 20
	// DataTypeDvdrPfi: DVD-R Physical Format Information.
	DataTypeDvdrPfi DataType = 21
	// DataTypeDvdAdip: DVD Address In Pregroove (ADIP).
	DataTypeDvdAdip DataType = 22
	// DataTypeHddvdCpi: HD DVD Copy Protection Information (CPI).
	DataTypeHddvdCpi DataType = 23
	// DataTypeHddvdMediumStatus: HD DVD medium status.
	DataTypeHddvdMediumStatus DataType = 24
	// DataTypeDvddlLayerCapacity: DVD dual-layer capacity.
	DataTypeDvddlLayerCapacity DataType = 25
	// DataTypeDvddlMiddleZoneAddress: DVD dual-layer middle zone address.
	DataTypeDvddlMiddleZoneAddress DataType = 26
	// DataTypeDvddlJumpIntervalSize: DVD dual-layer jump interval size.
	DataTypeDvddlJumpIntervalSize DataType = 27
	// DataTypeDvddlManualLayerJumpLba: DVD dual-layer manual layer jump LBA.
	DataTypeDvddlManualLayerJumpLba DataType = 28
	// DataTypeBlurayDi: Blu-ray Disc Information (DI).
	DataTypeBlurayDi DataType = 29
	// DataTypeBlurayBca: Blu-ray Burst Cutting Area (BCA).
	DataTypeBlurayBca DataType = 30
	// DataTypeBlurayDds: Blu-ray Disc Definition Structure (DDS).
	DataTypeBlurayDds DataType = 31
	// DataTypeBlurayCartridgeStatus: Blu-ray cartridge status.
	DataTypeBlurayCartridgeStatus DataType = 32
	// DataTypeBluraySpareArea: Blu-ray spare area information.
	DataTypeBluraySpareArea DataType = 33
	// DataTypeAacsVolumeIdentifier: AACS volume identifier.
	DataTypeAacsVolumeIdentifier DataType = 34
	// DataTypeAacsSerialNumber: AACS serial number.
	DataTypeAacsSerialNumber DataType = 35
	// DataTypeAacsMediaIdentifier: AACS media identifier.
	DataTypeAacsMediaIdentifier DataType = 36
	// DataTypeAacsMkb: AACS Media Key Block (MKB).
	DataTypeAacsMkb DataType = 37
	// DataTypeAacsDataKeys: AACS data keys.
	DataTypeAacsDataKeys DataType = 38
	// DataTypeAacsLbaExtents: AACS LBA extents.
	DataTypeAacsLbaExtents DataType = 39
	// DataTypeCprmMkb: CPRM Media Key Block (MKB).
	DataTypeCprmMkb DataType = 40
	// DataTypeHybridRecognizedLayers: Recognized layers (hybrid media).
	DataTypeHybridRecognizedLayers DataType = 41
	// DataTypeMmcWriteProtection: MMC write-protection data.
	DataTypeMmcWriteProtection DataType = 42
	// DataTypeMmcDiscInformation: MMC disc information.
	DataTypeMmcDiscInformation DataType = 43
	// DataTypeMmcTrackResourcesInformation: MMC track resources information.
	DataTypeMmcTrackResourcesInformation DataType = 44
	// DataTypeMmcPowResourcesInformation: MMC POW (Persistent Optical Write?) resources information.
	DataTypeMmcPowResourcesInformation DataType = 45
	// DataTypeScsiInquiry: SCSI INQUIRY response.
	DataTypeScsiInquiry DataType = 46
	// DataTypeScsiModePage2A: SCSI MODE PAGE 2Ah.
	DataTypeScsiModePage2A DataType = 47
	// DataTypeAtaIdentify: ATA IDENTIFY DEVICE data.
	DataTypeAtaIdentify DataType = 48
	// DataTypeAtapiIdentify: ATAPI IDENTIFY PACKET DEVICE data.
	DataTypeAtapiIdentify DataType = 49
	// DataTypePcmciaCis: PCMCIA Card Information Structure (CIS).
	DataTypePcmciaCis DataType = 50
	// DataTypeSdCid: Secure Digital CID register.
	DataTypeSdCid DataType = 51
	// DataTypeSdCsd: Secure Digital CSD register.
	DataTypeSdCsd DataType = 52
	// DataTypeSdScr: Secure Digital SCR register.
	DataTypeSdScr DataType = 53
	// DataTypeSdOcr: Secure Digital OCR register.
	DataTypeSdOcr DataType = 54
	// DataTypeMmcCid: MultiMediaCard CID register.
	DataTypeMmcCid DataType = 55
	// DataTypeMmcCsd: MultiMediaCard CSD register.
	DataTypeMmcCsd DataType = 56
	// DataTypeMmcOcr: MultiMediaCard OCR register.
	DataTypeMmcOcr DataType = 57
	// DataTypeExtendedCsd: MultiMediaCard Extended CSD register.
	DataTypeExtendedCsd DataType = 58
	// DataTypeXboxSecuritySector: Xbox Security Sector.
	DataTypeXboxSecuritySector DataType = 59
	// DataTypeFloppyLeadOut: Floppy lead‑out data.
	DataTypeFloppyLeadOut DataType = 60
	// DataTypeDiscControlBlock: DVD Disc Control Block.
	DataTypeDiscControlBlock DataType = 61
	// DataTypeCdFirstTrackPregap: Compact Disc first track pre-gap.
	DataTypeCdFirstTrackPregap DataType = 62
	// DataTypeCdLeadOut: Compact Disc lead‑out.
	DataTypeCdLeadOut DataType = 63
	// DataTypeScsiModeSense6: SCSI MODE SENSE (6) response.
	DataTypeScsiModeSense6 DataType = 64
	// DataTypeScsiModeSense10: SCSI MODE SENSE (10) response.
	DataTypeScsiModeSense10 DataType = 65
	// DataTypeUsbDescriptors: USB descriptors set.
	DataTypeUsbDescriptors DataType = 66
	// DataTypeXboxDmi: Xbox DMI.
	DataTypeXboxDmi DataType = 67
	// DataTypeXboxPfi: Xbox Physical Format Information (PFI).
	DataTypeXboxPfi DataType = 68
	// DataTypeCdSectorPrefix: Compact Disc sector prefix (sync, header).
	DataTypeCdSectorPrefix DataType = 69
	// DataTypeCdSectorSuffix: Compact Disc sector suffix (EDC, ECC P, ECC Q).
	DataTypeCdSectorSuffix DataType = 70
	// DataTypeCdSubchannel: Compact Disc subchannel data.
	DataTypeCdSubchannel DataType = 71
	// DataTypeAppleProfileTag: Apple Profile (20‑byte) tag.
	DataTypeAppleProfileTag DataType = 72
	// DataTypeAppleSonyTag: Apple Sony (12‑byte) tag.
	DataTypeAppleSonyTag DataType = 73
	// DataTypePriamDataTowerTag: Priam Data Tower (24‑byte) tag.
	DataTypePriamDataTowerTag DataType = 74
	// DataTypeCdMcn: Compact Disc Media Catalogue Number (lead‑in, 13 ASCII bytes).
	DataTypeCdMcn DataType = 75
	// DataTypeCdSectorPrefixCorrected: Compact Disc sector prefix (sync, header) corrected-only stored.
	DataTypeCdSectorPrefixCorrected DataType = 76
	// DataTypeCdSectorSuffixCorrected: Compact Disc sector suffix (EDC, ECC P, ECC Q) corrected-only stored.
	DataTypeCdSectorSuffixCorrected DataType = 77
	// DataTypeCdSubHeader: Compact Disc MODE 2 subheader.
	DataTypeCdSubHeader DataType = 78
	// DataTypeCdLeadIn: Compact Disc lead‑in.
	DataTypeCdLeadIn DataType = 79
	// DataTypeDvdDiscKeyDecrypted: Decrypted DVD Disc Key
	DataTypeDvdDiscKeyDecrypted DataType = 80
	// DataTypeDvdSectorCprMai: DVD Copyright Management Information (CPR_MAI)
	DataTypeDvdSectorCprMai DataType = 81
	// DataTypeDvdTitleKeyDecrypted: Decrypted DVD Title Key
	DataTypeDvdTitleKeyDecrypted DataType = 82
	// DataTypeDvdSectorId: DVD Identification Data (ID)
	DataTypeDvdSectorId DataType = 83
	// DataTypeDvdSectorIed: DVD ID Error Detection Code (IED)
	DataTypeDvdSectorIed DataType = 84
	// DataTypeDvdSectorEdc: DVD Error Detection Code (EDC)
	DataTypeDvdSectorEdc DataType = 85
	// DataTypeDvdSectorEccPi: DVD Error Correction Code (ECC) Parity of Inner Code (PI)
	DataTypeDvdSectorEccPi DataType = 86
	// DataTypeDvdEccBlockPo: DVD Error Correction Code (ECC) Parity of Outer Code (PO)
	DataTypeDvdEccBlockPo DataType = 87
	// DataTypeDvdPfi2ndLayer: DVD Physical Format Information for the second layer
	DataTypeDvdPfi2ndLayer DataType = 88
	// DataTypeFluxData: Flux data.
	DataTypeFluxData DataType = 89
	// DataTypeBitstreamData: Bitstream data.
	DataTypeBitstreamData DataType = 90
	// DataTypeFloppyWriteProtect: Floppy write-protect status.
	DataTypeFloppyWriteProtect DataType = 91
	// DataTypeWiiUDiscKey: Nintendo Wii U disc key (16 bytes, from non-readable disc area)
	DataTypeWiiUDiscKey DataType = 92
	// DataTypePs3DiscKey: PS3 derived disc key (16 bytes)
	DataTypePs3DiscKey DataType = 93
	// DataTypePs3Data1: PS3 data1 key (16 bytes, from disc)
	DataTypePs3Data1 DataType = 94
	// DataTypePs3Data2: PS3 data2 key (16 bytes, from disc)
	DataTypePs3Data2 DataType = 95
	// DataTypePs3Pic: PS3 PIC data (115 bytes, from disc lead-in)
	DataTypePs3Pic DataType = 96
	// DataTypePs3EncryptionMap: PS3 encryption region map (serialized from sector 0)
	DataTypePs3EncryptionMap DataType = 97
	// DataTypeWiiUPartitionKeyMap: Nintendo Wii U partition-to-key mapping with regions
	DataTypeWiiUPartitionKeyMap DataType = 98
	// DataTypeWiiPartitionKeyMap: Nintendo Wii partition-to-key mapping with regions
	DataTypeWiiPartitionKeyMap DataType = 99
	// DataTypeNgcwJunkMap: Nintendo GameCube/Wii junk region map with LFG seeds
	DataTypeNgcwJunkMap DataType = 100
	// DataTypeAacsMediaKey: AACS Media Key
	DataTypeAacsMediaKey DataType = 101
	// DataTypeAacsVolumeUniqueKey: AACS Volume Unique Key
	DataTypeAacsVolumeUniqueKey DataType = 102
	// DataTypeBdSectorEdc: Blu-ray Sector EDC
	DataTypeBdSectorEdc DataType = 103
	// DataTypeErasureParity: Erasure coding parity shard for data blocks.
	DataTypeErasureParity DataType = 104
	// DataTypeErasureParityDdt: Erasure coding parity shard for DDT secondary blocks.
	DataTypeErasureParityDdt DataType = 105
	// DataTypeErasureParityDdtPrimary: Erasure coding parity replica for DDT primary block.
	DataTypeErasureParityDdtPrimary DataType = 106
	// DataTypeErasureParityMeta: Erasure coding parity shard for metadata blocks.
	DataTypeErasureParityMeta DataType = 107
	// DataTypeErasureParityIndex: Erasure coding parity replica for index block.
	DataTypeErasureParityIndex DataType = 108
)

var _DataTypeNames = map[DataType]string{
	DataTypeNone:                         "DataTypeNone",
	DataTypeUserData:                     "DataTypeUserData",
	DataTypeCdToc:                        "DataTypeCdToc",
	DataTypeSessionInfo:                  "DataTypeSessionInfo",
	DataTypeFullToc:                      "DataTypeFullToc",
	DataTypeCdPma:                        "DataTypeCdPma",
	DataTypeCdAtip:                       "DataTypeCdAtip",
	DataTypeCdText:                       "DataTypeCdText",
	DataTypeDvdPfi:                       "DataTypeDvdPfi",
	DataTypeDvdCmi:                       "DataTypeDvdCmi",
	DataTypeDvdDiscKey:                   "DataTypeDvdDiscKey",
	DataTypeDvdBca:                       "DataTypeDvdBca",
	DataTypeDvdDmi:                       "DataTypeDvdDmi",
	DataTypeDvdMediaIdentifier:           "DataTypeDvdMediaIdentifier",
	DataTypeDvdMkb:                       "DataTypeDvdMkb",
	DataTypeDvdRamDds:                    "DataTypeDvdRamDds",
	DataTypeDvdRamMediumStatus:           "DataTypeDvdRamMediumStatus",
	DataTypeDvdRamSpareArea:              "DataTypeDvdRamSpareArea",
	DataTypeDvdrRmd:                      "DataTypeDvdrRmd",
	DataTypeDvdrPreRecordedInfo:          "DataTypeDvdrPreRecordedInfo",
	DataTypeDvdrMediaIdentifier:          "DataTypeDvdrMediaIdentifier",
	DataTypeDvdrPfi:                      "DataTypeDvdrPfi",
	DataTypeDvdAdip:                      "DataTypeDvdAdip",
	DataTypeHddvdCpi:                     "DataTypeHddvdCpi",
	DataTypeHddvdMediumStatus:            "DataTypeHddvdMediumStatus",
	DataTypeDvddlLayerCapacity:           "DataTypeDvddlLayerCapacity",
	DataTypeDvddlMiddleZoneAddress:       "DataTypeDvddlMiddleZoneAddress",
	DataTypeDvddlJumpIntervalSize:        "DataTypeDvddlJumpIntervalSize",
	DataTypeDvddlManualLayerJumpLba:      "DataTypeDvddlManualLayerJumpLba",
	DataTypeBlurayDi:                     "DataTypeBlurayDi",
	DataTypeBlurayBca:                    "DataTypeBlurayBca",
	DataTypeBlurayDds:                    "DataTypeBlurayDds",
	DataTypeBlurayCartridgeStatus:        "DataTypeBlurayCartridgeStatus",
	DataTypeBluraySpareArea:              "DataTypeBluraySpareArea",
	DataTypeAacsVolumeIdentifier:         "DataTypeAacsVolumeIdentifier",
	DataTypeAacsSerialNumber:             "DataTypeAacsSerialNumber",
	DataTypeAacsMediaIdentifier:          "DataTypeAacsMediaIdentifier",
	DataTypeAacsMkb:                      "DataTypeAacsMkb",
	DataTypeAacsDataKeys:                 "DataTypeAacsDataKeys",
	DataTypeAacsLbaExtents:               "DataTypeAacsLbaExtents",
	DataTypeCprmMkb:                      "DataTypeCprmMkb",
	DataTypeHybridRecognizedLayers:       "DataTypeHybridRecognizedLayers",
	DataTypeMmcWriteProtection:           "DataTypeMmcWriteProtection",
	DataTypeMmcDiscInformation:           "DataTypeMmcDiscInformation",
	DataTypeMmcTrackResourcesInformation: "DataTypeMmcTrackResourcesInformation",
	DataTypeMmcPowResourcesInformation:   "DataTypeMmcPowResourcesInformation",
	DataTypeScsiInquiry:                  "DataTypeScsiInquiry",
	DataTypeScsiModePage2A:               "DataTypeScsiModePage2A",
	DataTypeAtaIdentify:                  "DataTypeAtaIdentify",
	DataTypeAtapiIdentify:                "DataTypeAtapiIdentify",
	DataTypePcmciaCis:                    "DataTypePcmciaCis",
	DataTypeSdCid:                        "DataTypeSdCid",
	DataTypeSdCsd:                        "DataTypeSdCsd",
	DataTypeSdScr:                        "DataTypeSdScr",
	DataTypeSdOcr:                        "DataTypeSdOcr",
	DataTypeMmcCid:                       "DataTypeMmcCid",
	DataTypeMmcCsd:                       "DataTypeMmcCsd",
	DataTypeMmcOcr:                       "DataTypeMmcOcr",
	DataTypeExtendedCsd:                  "DataTypeExtendedCsd",
	DataTypeXboxSecuritySector:           "DataTypeXboxSecuritySector",
	DataTypeFloppyLeadOut:                "DataTypeFloppyLeadOut",
	DataTypeDiscControlBlock:             "DataTypeDiscControlBlock",
	DataTypeCdFirstTrackPregap:           "DataTypeCdFirstTrackPregap",
	DataTypeCdLeadOut:                    "DataTypeCdLeadOut",
	DataTypeScsiModeSense6:               "DataTypeScsiModeSense6",
	DataTypeScsiModeSense10:              "DataTypeScsiModeSense10",
	DataTypeUsbDescriptors:               "DataTypeUsbDescriptors",
	DataTypeXboxDmi:                      "DataTypeXboxDmi",
	DataTypeXboxPfi:                      "DataTypeXboxPfi",
	DataTypeCdSectorPrefix:               "DataTypeCdSectorPrefix",
	DataTypeCdSectorSuffix:               "DataTypeCdSectorSuffix",
	DataTypeCdSubchannel:                 "DataTypeCdSubchannel",
	DataTypeAppleProfileTag:              "DataTypeAppleProfileTag",
	DataTypeAppleSonyTag:                 "DataTypeAppleSonyTag",
	DataTypePriamDataTowerTag:            "DataTypePriamDataTowerTag",
	DataTypeCdMcn:                        "DataTypeCdMcn",
	DataTypeCdSectorPrefixCorrected:      "DataTypeCdSectorPrefixCorrected",
	DataTypeCdSectorSuffixCorrected:      "DataTypeCdSectorSuffixCorrected",
	DataTypeCdSubHeader:                  "DataTypeCdSubHeader",
	DataTypeCdLeadIn:                     "DataTypeCdLeadIn",
	DataTypeDvdDiscKeyDecrypted:          "DataTypeDvdDiscKeyDecrypted",
	DataTypeDvdSectorCprMai:              "DataTypeDvdSectorCprMai",
	DataTypeDvdTitleKeyDecrypted:         "DataTypeDvdTitleKeyDecrypted",
	DataTypeDvdSectorId:                  "DataTypeDvdSectorId",
	DataTypeDvdSectorIed:                 "DataTypeDvdSectorIed",
	DataTypeDvdSectorEdc:                 "DataTypeDvdSectorEdc",
	DataTypeDvdSectorEccPi:               "DataTypeDvdSectorEccPi",
	DataTypeDvdEccBlockPo:                "DataTypeDvdEccBlockPo",
	DataTypeDvdPfi2ndLayer:               "DataTypeDvdPfi2ndLayer",
	DataTypeFluxData:                     "DataTypeFluxData",
	DataTypeBitstreamData:                "DataTypeBitstreamData",
	DataTypeFloppyWriteProtect:           "DataTypeFloppyWriteProtect",
	DataTypeWiiUDiscKey:                  "DataTypeWiiUDiscKey",
	DataTypePs3DiscKey:                   "DataTypePs3DiscKey",
	DataTypePs3Data1:                     "DataTypePs3Data1",
	DataTypePs3Data2:                     "DataTypePs3Data2",
	DataTypePs3Pic:                       "DataTypePs3Pic",
	DataTypePs3EncryptionMap:             "DataTypePs3EncryptionMap",
	DataTypeWiiUPartitionKeyMap:          "DataTypeWiiUPartitionKeyMap",
	DataTypeWiiPartitionKeyMap:           "DataTypeWiiPartitionKeyMap",
	DataTypeNgcwJunkMap:                  "DataTypeNgcwJunkMap",
	DataTypeAacsMediaKey:                 "DataTypeAacsMediaKey",
	DataTypeAacsVolumeUniqueKey:          "DataTypeAacsVolumeUniqueKey",
	DataTypeBdSectorEdc:                  "DataTypeBdSectorEdc",
	DataTypeErasureParity:                "DataTypeErasureParity",
	DataTypeErasureParityDdt:             "DataTypeErasureParityDdt",
	DataTypeErasureParityDdtPrimary:      "DataTypeErasureParityDdtPrimary",
	DataTypeErasureParityMeta:            "DataTypeErasureParityMeta",
	DataTypeErasureParityIndex:           "DataTypeErasureParityIndex",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t DataType) String() string {
	if s, ok := _DataTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("DataType(%d)", uint16(t))
}

// BlockType is the 4-byte identifier of an on-disk block.
type BlockType uint32

const (
	// BlockTypeDataBlock: Block containing data.
	BlockTypeDataBlock BlockType = 0x4B4C4244
	// BlockTypeDeDuplicationTable: Block containing a deduplication table (v1).
	BlockTypeDeDuplicationTable BlockType = 0x2A544444
	// BlockTypeDeDuplicationTable2: Block containing a deduplication table v2.
	BlockTypeDeDuplicationTable2 BlockType = 0x32544444
	// BlockTypeDeDuplicationTableSAlpha: Block containing a secondary deduplication table (v2) (mistake).
	BlockTypeDeDuplicationTableSAlpha BlockType = 0x53545444
	// BlockTypeDeDuplicationTableSecondary: Block containing a secondary deduplication table (v2).
	BlockTypeDeDuplicationTableSecondary BlockType = 0x53544444
	// BlockTypeIndexBlock: Block containing the index (v1).
	BlockTypeIndexBlock BlockType = 0x58444E49
	// BlockTypeIndexBlock2: Block containing the index v2.
	BlockTypeIndexBlock2 BlockType = 0x32584449
	// BlockTypeIndexBlock3: Block containing the index v3.
	BlockTypeIndexBlock3 BlockType = 0x33584449
	// BlockTypeGeometryBlock: Block containing logical geometry.
	BlockTypeGeometryBlock BlockType = 0x4D4F4547
	// BlockTypeMetadataBlock: Block containing metadata.
	BlockTypeMetadataBlock BlockType = 0x4154454D
	// BlockTypeTracksBlock: Block containing optical disc tracks.
	BlockTypeTracksBlock BlockType = 0x534B5254
	// BlockTypeCicmBlock: Block containing CICM XML metadata.
	BlockTypeCicmBlock BlockType = 0x4D434943
	// BlockTypeChecksumBlock: Block containing contents checksums.
	BlockTypeChecksumBlock BlockType = 0x4D534B43
	// BlockTypeDataPositionMeasurementBlock: Block containing data position measurements (reserved / TODO).
	BlockTypeDataPositionMeasurementBlock BlockType = 0x2A4D5044
	// BlockTypeSnapshotBlock: Block containing a snapshot index (reserved / TODO).
	BlockTypeSnapshotBlock BlockType = 0x50414E53
	// BlockTypeParentBlock: Block describing how to locate the parent image (reserved / TODO).
	BlockTypeParentBlock BlockType = 0x50524E54
	// BlockTypeDumpHardwareBlock: Block containing an array of hardware used to create the image.
	BlockTypeDumpHardwareBlock BlockType = 0x2A504D44
	// BlockTypeTapeFileBlock: Block containing list of files for a tape image.
	BlockTypeTapeFileBlock BlockType = 0x454C4654
	// BlockTypeTapePartitionBlock: Block containing list of partitions for a tape image.
	BlockTypeTapePartitionBlock BlockType = 0x54425054
	// BlockTypeAaruMetadataJsonBlock: Block containing JSON version of Aaru Metadata
	BlockTypeAaruMetadataJsonBlock BlockType = 0x444D534A
	// BlockTypeFluxDataBlock: Block containing flux data metadata.
	BlockTypeFluxDataBlock BlockType = 0x58554C46
	// BlockTypeDataStreamPayloadBlock: Block containing compressed data stream payload (e.g., flux data, bitstreams).
	BlockTypeDataStreamPayloadBlock BlockType = 0x4C505344
	// BlockTypeErasureCodingMapBlock: Block containing erasure coding stripe map and recovery metadata.
	BlockTypeErasureCodingMapBlock BlockType = 0x424D4345
)

var _BlockTypeNames = map[BlockType]string{
	BlockTypeDataBlock:                    "BlockTypeDataBlock",
	BlockTypeDeDuplicationTable:           "BlockTypeDeDuplicationTable",
	BlockTypeDeDuplicationTable2:          "BlockTypeDeDuplicationTable2",
	BlockTypeDeDuplicationTableSAlpha:     "BlockTypeDeDuplicationTableSAlpha",
	BlockTypeDeDuplicationTableSecondary:  "BlockTypeDeDuplicationTableSecondary",
	BlockTypeIndexBlock:                   "BlockTypeIndexBlock",
	BlockTypeIndexBlock2:                  "BlockTypeIndexBlock2",
	BlockTypeIndexBlock3:                  "BlockTypeIndexBlock3",
	BlockTypeGeometryBlock:                "BlockTypeGeometryBlock",
	BlockTypeMetadataBlock:                "BlockTypeMetadataBlock",
	BlockTypeTracksBlock:                  "BlockTypeTracksBlock",
	BlockTypeCicmBlock:                    "BlockTypeCicmBlock",
	BlockTypeChecksumBlock:                "BlockTypeChecksumBlock",
	BlockTypeDataPositionMeasurementBlock: "BlockTypeDataPositionMeasurementBlock",
	BlockTypeSnapshotBlock:                "BlockTypeSnapshotBlock",
	BlockTypeParentBlock:                  "BlockTypeParentBlock",
	BlockTypeDumpHardwareBlock:            "BlockTypeDumpHardwareBlock",
	BlockTypeTapeFileBlock:                "BlockTypeTapeFileBlock",
	BlockTypeTapePartitionBlock:           "BlockTypeTapePartitionBlock",
	BlockTypeAaruMetadataJsonBlock:        "BlockTypeAaruMetadataJsonBlock",
	BlockTypeFluxDataBlock:                "BlockTypeFluxDataBlock",
	BlockTypeDataStreamPayloadBlock:       "BlockTypeDataStreamPayloadBlock",
	BlockTypeErasureCodingMapBlock:        "BlockTypeErasureCodingMapBlock",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t BlockType) String() string {
	if s, ok := _BlockTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("BlockType(%d)", uint32(t))
}

// ChecksumAlgorithm identifies a whole-image checksum algorithm.
type ChecksumAlgorithm uint8

const (
	// ChecksumInvalid: Invalid / unspecified algorithm.
	ChecksumInvalid ChecksumAlgorithm = 0
	// ChecksumMd5: MD5 hash.
	ChecksumMd5 ChecksumAlgorithm = 1
	// ChecksumSha1: SHA-1 hash.
	ChecksumSha1 ChecksumAlgorithm = 2
	// ChecksumSha256: SHA-256 hash.
	ChecksumSha256 ChecksumAlgorithm = 3
	// ChecksumSpamSum: SpamSum (context-triggered piecewise hash).
	ChecksumSpamSum ChecksumAlgorithm = 4
	// ChecksumBlake3: BLAKE3 hash.
	ChecksumBlake3 ChecksumAlgorithm = 5
)

var _ChecksumAlgorithmNames = map[ChecksumAlgorithm]string{
	ChecksumInvalid: "ChecksumInvalid",
	ChecksumMd5:     "ChecksumMd5",
	ChecksumSha1:    "ChecksumSha1",
	ChecksumSha256:  "ChecksumSha256",
	ChecksumSpamSum: "ChecksumSpamSum",
	ChecksumBlake3:  "ChecksumBlake3",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t ChecksumAlgorithm) String() string {
	if s, ok := _ChecksumAlgorithmNames[t]; ok {
		return s
	}
	return fmt.Sprintf("ChecksumAlgorithm(%d)", uint8(t))
}

// CdFixFlags are the flags stored in the high byte of legacy (DDT v1) CD prefix/suffix tables.
type CdFixFlags uint32

const (
	// CdFixNotDumped: Sector(s) have not yet been dumped.
	CdFixNotDumped CdFixFlags = 0x10000000
	// CdFixCorrect: Sector(s) contain valid MODE 1 data with regenerable suffix/prefix.
	CdFixCorrect CdFixFlags = 0x20000000
	// CdFixMode2Form1Ok: Sector suffix valid for MODE 2 Form 1; regenerable.
	CdFixMode2Form1Ok CdFixFlags = 0x30000000
	// CdFixMode2Form2Ok: Sector suffix valid for MODE 2 Form 2 with correct CRC.
	CdFixMode2Form2Ok CdFixFlags = 0x40000000
	// CdFixMode2Form2NoCrc: Sector suffix valid for MODE 2 Form 2 but CRC absent/empty.
	CdFixMode2Form2NoCrc CdFixFlags = 0x50000000
)

var _CdFixFlagsNames = map[CdFixFlags]string{
	CdFixNotDumped:       "CdFixNotDumped",
	CdFixCorrect:         "CdFixCorrect",
	CdFixMode2Form1Ok:    "CdFixMode2Form1Ok",
	CdFixMode2Form2Ok:    "CdFixMode2Form2Ok",
	CdFixMode2Form2NoCrc: "CdFixMode2Form2NoCrc",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t CdFixFlags) String() string {
	if s, ok := _CdFixFlagsNames[t]; ok {
		return s
	}
	return fmt.Sprintf("CdFixFlags(%d)", uint32(t))
}

// TrackType is the type of an optical disc track.
type TrackType uint8

const (
	// TrackTypeAudio: Audio track.
	TrackTypeAudio TrackType = 0
	// TrackTypeData: Generic data track (not further specified).
	TrackTypeData TrackType = 1
	// TrackTypeCdMode1: Compact Disc Mode 1 data track.
	TrackTypeCdMode1 TrackType = 2
	// TrackTypeCdMode2Formless: Compact Disc Mode 2 (formless) data track.
	TrackTypeCdMode2Formless TrackType = 3
	// TrackTypeCdMode2Form1: Compact Disc Mode 2 Form 1 data track.
	TrackTypeCdMode2Form1 TrackType = 4
	// TrackTypeCdMode2Form2: Compact Disc Mode 2 Form 2 data track.
	TrackTypeCdMode2Form2 TrackType = 5
)

var _TrackTypeNames = map[TrackType]string{
	TrackTypeAudio:           "TrackTypeAudio",
	TrackTypeData:            "TrackTypeData",
	TrackTypeCdMode1:         "TrackTypeCdMode1",
	TrackTypeCdMode2Formless: "TrackTypeCdMode2Formless",
	TrackTypeCdMode2Form1:    "TrackTypeCdMode2Form1",
	TrackTypeCdMode2Form2:    "TrackTypeCdMode2Form2",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t TrackType) String() string {
	if s, ok := _TrackTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("TrackType(%d)", uint8(t))
}

// SectorStatus is the per-sector status nibble stored in DDT v2 entries.
type SectorStatus uint8

const (
	// SectorStatusNotDumped: Sector(s) not yet acquired during image dumping.
	SectorStatusNotDumped SectorStatus = 0x0
	// SectorStatusDumped: Sector(s) successfully dumped without error.
	SectorStatusDumped SectorStatus = 0x1
	// SectorStatusErrored: Error during dumping; data may be incomplete or corrupt.
	SectorStatusErrored SectorStatus = 0x2
	// SectorStatusMode1Correct: Valid MODE 1 data with regenerable suffix/prefix.
	SectorStatusMode1Correct SectorStatus = 0x3
	// SectorStatusMode2Form1Ok: Suffix verified/regenerable for MODE 2 Form 1.
	SectorStatusMode2Form1Ok SectorStatus = 0x4
	// SectorStatusMode2Form2Ok: Suffix matches MODE 2 Form 2 with valid CRC.
	SectorStatusMode2Form2Ok SectorStatus = 0x5
	// SectorStatusMode2Form2NoCrc: Suffix matches MODE 2 Form 2 but CRC empty/missing.
	SectorStatusMode2Form2NoCrc SectorStatus = 0x6
	// SectorStatusTwin: Pointer references a twin sector table.
	SectorStatusTwin SectorStatus = 0x7
	// SectorStatusUnrecorded: Sector physically unrecorded; repeated reads non-deterministic.
	SectorStatusUnrecorded SectorStatus = 0x8
	// SectorStatusEncrypted: Content encrypted and stored encrypted in image.
	SectorStatusEncrypted SectorStatus = 0x9
	// SectorStatusUnencrypted: Content originally encrypted but stored decrypted in image.
	SectorStatusUnencrypted SectorStatus = 0xA
	// SectorStatusGenerable: Content can be generated using a known algorithm.
	SectorStatusGenerable SectorStatus = 0xB
)

var _SectorStatusNames = map[SectorStatus]string{
	SectorStatusNotDumped:       "SectorStatusNotDumped",
	SectorStatusDumped:          "SectorStatusDumped",
	SectorStatusErrored:         "SectorStatusErrored",
	SectorStatusMode1Correct:    "SectorStatusMode1Correct",
	SectorStatusMode2Form1Ok:    "SectorStatusMode2Form1Ok",
	SectorStatusMode2Form2Ok:    "SectorStatusMode2Form2Ok",
	SectorStatusMode2Form2NoCrc: "SectorStatusMode2Form2NoCrc",
	SectorStatusTwin:            "SectorStatusTwin",
	SectorStatusUnrecorded:      "SectorStatusUnrecorded",
	SectorStatusEncrypted:       "SectorStatusEncrypted",
	SectorStatusUnencrypted:     "SectorStatusUnencrypted",
	SectorStatusGenerable:       "SectorStatusGenerable",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t SectorStatus) String() string {
	if s, ok := _SectorStatusNames[t]; ok {
		return s
	}
	return fmt.Sprintf("SectorStatus(%d)", uint8(t))
}

// ErasureCodingAlgorithm selects the erasure coding scheme.
type ErasureCodingAlgorithm uint8

const (
	// ErasureCodingXor: Simple XOR parity (M must be 1).
	ErasureCodingXor ErasureCodingAlgorithm = 0
	// ErasureCodingRsVandermonde: Reed-Solomon with Vandermonde generator matrix over GF(2^8).
	ErasureCodingRsVandermonde ErasureCodingAlgorithm = 1
)

var _ErasureCodingAlgorithmNames = map[ErasureCodingAlgorithm]string{
	ErasureCodingXor:           "ErasureCodingXor",
	ErasureCodingRsVandermonde: "ErasureCodingRsVandermonde",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t ErasureCodingAlgorithm) String() string {
	if s, ok := _ErasureCodingAlgorithmNames[t]; ok {
		return s
	}
	return fmt.Sprintf("ErasureCodingAlgorithm(%d)", uint8(t))
}

// ErasureCodingGroupType identifies an erasure coding protection group.
type ErasureCodingGroupType uint8

const (
	// ECGroupData: User data blocks (DBLK).
	ECGroupData ErasureCodingGroupType = 0
	// ECGroupDdtSecondary: Secondary DDT subtables.
	ECGroupDdtSecondary ErasureCodingGroupType = 1
	// ECGroupDdtPrimary: Primary DDT (K=1, M replicas).
	ECGroupDdtPrimary ErasureCodingGroupType = 2
	// ECGroupMetadata: Metadata/media tag blocks.
	ECGroupMetadata ErasureCodingGroupType = 3
	// ECGroupIndex: Index block (K=1, M replicas).
	ECGroupIndex ErasureCodingGroupType = 4
)

var _ErasureCodingGroupTypeNames = map[ErasureCodingGroupType]string{
	ECGroupData:         "ECGroupData",
	ECGroupDdtSecondary: "ECGroupDdtSecondary",
	ECGroupDdtPrimary:   "ECGroupDdtPrimary",
	ECGroupMetadata:     "ECGroupMetadata",
	ECGroupIndex:        "ECGroupIndex",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t ErasureCodingGroupType) String() string {
	if s, ok := _ErasureCodingGroupTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("ErasureCodingGroupType(%d)", uint8(t))
}

// DpmEntryStatus is the status of a data position measurement bin.
type DpmEntryStatus uint8

const (
	// DpmStatusUnknown: Unknown, e.g. imported from a format that does not record it.
	DpmStatusUnknown DpmEntryStatus = 0
	// DpmStatusMeasured: Measured on the first attempt.
	DpmStatusMeasured DpmEntryStatus = 1
	// DpmStatusRemeasured: Measured again because the first measurement was an outlier.
	DpmStatusRemeasured DpmEntryStatus = 2
	// DpmStatusInterpolated: Measurement failed, interpolated from its neighbours.
	DpmStatusInterpolated DpmEntryStatus = 3
	// DpmStatusUnreadable: Sectors could not be read, interpolated from its neighbours.
	DpmStatusUnreadable DpmEntryStatus = 4
	// DpmStatusLayerBreak: Bin crosses a layer boundary or is too short, density copied from the bin before.
	DpmStatusLayerBreak DpmEntryStatus = 5
)

var _DpmEntryStatusNames = map[DpmEntryStatus]string{
	DpmStatusUnknown:      "DpmStatusUnknown",
	DpmStatusMeasured:     "DpmStatusMeasured",
	DpmStatusRemeasured:   "DpmStatusRemeasured",
	DpmStatusInterpolated: "DpmStatusInterpolated",
	DpmStatusUnreadable:   "DpmStatusUnreadable",
	DpmStatusLayerBreak:   "DpmStatusLayerBreak",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t DpmEntryStatus) String() string {
	if s, ok := _DpmEntryStatusNames[t]; ok {
		return s
	}
	return fmt.Sprintf("DpmEntryStatus(%d)", uint8(t))
}

// SectorTagType identifies a per-sector tag (subchannel, CD sync, DVD IED, ...).
type SectorTagType int32

const (
	// SectorTagAppleSony: Apple's Sony sector tags, 12 bytes (address prolog + checksum)
	SectorTagAppleSony SectorTagType = 0
	// SectorTagCdSync: 12-byte CD sync pattern (00 FF*10 00)
	SectorTagCdSync SectorTagType = 1
	// SectorTagCdHeader: 4-byte CD header (minute, second, frame, mode)
	SectorTagCdHeader SectorTagType = 2
	// SectorTagCdSubHeader: Mode 2 Form subheader (8 bytes: copy, submode, channel)
	SectorTagCdSubHeader SectorTagType = 3
	// SectorTagCdEdc: 32-bit CRC (EDC)
	SectorTagCdEdc SectorTagType = 4
	// SectorTagCdEccP: 172 bytes Reed-Solomon ECC (P)
	SectorTagCdEccP SectorTagType = 5
	// SectorTagCdEccQ: 104 bytes Reed-Solomon ECC (Q)
	SectorTagCdEccQ SectorTagType = 6
	// SectorTagCdEcc: Combined P+Q ECC (276 bytes)
	SectorTagCdEcc SectorTagType = 7
	// SectorTagCdSubchannel: 96 raw subchannel bytes (P-W)
	SectorTagCdSubchannel SectorTagType = 8
	// SectorTagCdTrackIsrc: Track ISRC (12 ASCII chars, no terminator)
	SectorTagCdTrackIsrc SectorTagType = 9
	// SectorTagCdTrackText: Track text (CD-Text fragment, 13 bytes)
	SectorTagCdTrackText SectorTagType = 10
	// SectorTagCdTrackFlags: Track flags (audio/data, copy permitted, pre-emphasis)
	SectorTagCdTrackFlags SectorTagType = 11
	// SectorTagDvdCmi: DVD Copyright Management Information (CSS)
	SectorTagDvdCmi SectorTagType = 12
	// SectorTagFloppyAddressMark: Raw address mark & sync preamble (format dependent)
	SectorTagFloppyAddressMark SectorTagType = 13
	// SectorTagDvdTitleKey: DVD sector title key, 5 bytes
	SectorTagDvdTitleKey SectorTagType = 14
	// SectorTagDvdTitleKeyDecrypted: Decrypted DVD sector title key, 5 bytes
	SectorTagDvdTitleKeyDecrypted SectorTagType = 15
	// SectorTagDvdSectorInformation: DVD sector information, 1 bytes
	SectorTagDvdSectorInformation SectorTagType = 16
	// SectorTagDvdSectorNumber: DVD sector number, 3 bytes
	SectorTagDvdSectorNumber SectorTagType = 17
	// SectorTagDvdSectorIed: DVD sector ID error detection, 2 bytes
	SectorTagDvdSectorIed SectorTagType = 18
	// SectorTagDvdSectorEdc: DVD sector EDC, 4 bytes
	SectorTagDvdSectorEdc SectorTagType = 19
	// SectorTagAppleProfile: Apple's Profile sector tags, 20 bytes
	SectorTagAppleProfile SectorTagType = 20
	// SectorTagPriamDataTower: Priam DataTower sector tags, 24 bytes
	SectorTagPriamDataTower SectorTagType = 21
	// SectorTagBdSectorEdc: Blu-ray sector EDC, 4 bytes
	SectorTagBdSectorEdc SectorTagType = 22
)

var _SectorTagTypeNames = map[SectorTagType]string{
	SectorTagAppleSony:            "SectorTagAppleSony",
	SectorTagCdSync:               "SectorTagCdSync",
	SectorTagCdHeader:             "SectorTagCdHeader",
	SectorTagCdSubHeader:          "SectorTagCdSubHeader",
	SectorTagCdEdc:                "SectorTagCdEdc",
	SectorTagCdEccP:               "SectorTagCdEccP",
	SectorTagCdEccQ:               "SectorTagCdEccQ",
	SectorTagCdEcc:                "SectorTagCdEcc",
	SectorTagCdSubchannel:         "SectorTagCdSubchannel",
	SectorTagCdTrackIsrc:          "SectorTagCdTrackIsrc",
	SectorTagCdTrackText:          "SectorTagCdTrackText",
	SectorTagCdTrackFlags:         "SectorTagCdTrackFlags",
	SectorTagDvdCmi:               "SectorTagDvdCmi",
	SectorTagFloppyAddressMark:    "SectorTagFloppyAddressMark",
	SectorTagDvdTitleKey:          "SectorTagDvdTitleKey",
	SectorTagDvdTitleKeyDecrypted: "SectorTagDvdTitleKeyDecrypted",
	SectorTagDvdSectorInformation: "SectorTagDvdSectorInformation",
	SectorTagDvdSectorNumber:      "SectorTagDvdSectorNumber",
	SectorTagDvdSectorIed:         "SectorTagDvdSectorIed",
	SectorTagDvdSectorEdc:         "SectorTagDvdSectorEdc",
	SectorTagAppleProfile:         "SectorTagAppleProfile",
	SectorTagPriamDataTower:       "SectorTagPriamDataTower",
	SectorTagBdSectorEdc:          "SectorTagBdSectorEdc",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t SectorTagType) String() string {
	if s, ok := _SectorTagTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("SectorTagType(%d)", int32(t))
}

// MediaTagType identifies a whole-media tag (TOC, PFI, ATIP, ...).
type MediaTagType int32

const (
	// MediaTagCdToc: Standard CD Table Of Contents (lead-in, first session)
	MediaTagCdToc MediaTagType = 0
	// MediaTagSessionInfo: Per-session summary (start/end addresses, track count)
	MediaTagSessionInfo MediaTagType = 1
	// MediaTagFullToc: Complete multi-session TOC including hidden tracks
	MediaTagFullToc MediaTagType = 2
	// MediaTagCdPma: Program Memory Area (temporary track info before finalization)
	MediaTagCdPma MediaTagType = 3
	// MediaTagCdAtip: Absolute Time In Pregroove (writable media timing & power metadata)
	MediaTagCdAtip MediaTagType = 4
	// MediaTagCdText: CD-Text blocks (titles, performers, etc.)
	MediaTagCdText MediaTagType = 5
	// MediaTagCdMcn: Media Catalogue Number (EAN/UPC style identifier)
	MediaTagCdMcn MediaTagType = 6
	// MediaTagDvdPfi: Physical Format Information (layer geometry & book type)
	MediaTagDvdPfi MediaTagType = 7
	// MediaTagDvdCmi: Copyright Management Information (CSS/CPRM flags)
	MediaTagDvdCmi MediaTagType = 8
	// MediaTagDvdDiscKey: Encrypted disc key block (CSS)
	MediaTagDvdDiscKey MediaTagType = 9
	// MediaTagDvdBca: Burst Cutting Area (etched manufacturer / AACS info)
	MediaTagDvdBca MediaTagType = 10
	// MediaTagDvdDmi: Disc Manufacturer Information (lead-in descriptor)
	MediaTagDvdDmi MediaTagType = 11
	// MediaTagDvdMediaIdentifier: Writable media dye / manufacturer ID
	MediaTagDvdMediaIdentifier MediaTagType = 12
	// MediaTagDvdMkb: Media Key Block (AACS/DVD)
	MediaTagDvdMkb MediaTagType = 13
	// MediaTagDvdRamDds: Defect Data Structure (DVD-RAM mapping)
	MediaTagDvdRamDds MediaTagType = 14
	// MediaTagDvdRamMediumStatus: Medium Status (allocated spare info)
	MediaTagDvdRamMediumStatus MediaTagType = 15
	// MediaTagDvdRamSpareArea: Spare area descriptors
	MediaTagDvdRamSpareArea MediaTagType = 16
	// MediaTagDvdrRmd: Recorded Media Data (RMD) last border-out
	MediaTagDvdrRmd MediaTagType = 17
	// MediaTagDvdrPreRecordedInfo: Pre-recorded info area (lead-in)
	MediaTagDvdrPreRecordedInfo MediaTagType = 18
	// MediaTagDvdrMediaIdentifier: DVD-R/-RW writable media identifier
	MediaTagDvdrMediaIdentifier MediaTagType = 19
	// MediaTagDvdrPfi: DVD-R physical format (layer data)
	MediaTagDvdrPfi MediaTagType = 20
	// MediaTagDvdAdip: Address In Pregroove (DVD+ / wobble timing)
	MediaTagDvdAdip MediaTagType = 21
	// MediaTagHddvdCpi: Content Protection Info (HD DVD)
	MediaTagHddvdCpi MediaTagType = 22
	// MediaTagHddvdMediumStatus: HD DVD Medium status (spares/defects)
	MediaTagHddvdMediumStatus MediaTagType = 23
	// MediaTagDvddlLayerCapacity: Dual layer capacity & break info
	MediaTagDvddlLayerCapacity MediaTagType = 24
	// MediaTagDvddlMiddleZoneAddress: Middle zone start LBA
	MediaTagDvddlMiddleZoneAddress MediaTagType = 25
	// MediaTagDvddlJumpIntervalSize: Jump interval size (opposite track path)
	MediaTagDvddlJumpIntervalSize MediaTagType = 26
	// MediaTagDvddlManualLayerJumpLba: Manual layer jump LBA (OTP)
	MediaTagDvddlManualLayerJumpLba MediaTagType = 27
	// MediaTagBlurayDi: Disc Information (BD)
	MediaTagBlurayDi MediaTagType = 28
	// MediaTagBlurayBca: Blu-ray Burst Cutting Area
	MediaTagBlurayBca MediaTagType = 29
	// MediaTagBlurayDds: Disc Definition Structure (recordable)
	MediaTagBlurayDds MediaTagType = 30
	// MediaTagBlurayCartridgeStatus: Cartridge presence / write protect (BD-RE/BD-R in caddy)
	MediaTagBlurayCartridgeStatus MediaTagType = 31
	// MediaTagBluraySpareArea: BD spare area allocation map
	MediaTagBluraySpareArea MediaTagType = 32
	// MediaTagAacsVolumeIdentifier: AACS Volume Identifier
	MediaTagAacsVolumeIdentifier MediaTagType = 33
	// MediaTagAacsSerialNumber: Pre-recorded media serial number (AACS)
	MediaTagAacsSerialNumber MediaTagType = 34
	// MediaTagAacsMediaIdentifier: AACS Media Identifier (unique per disc)
	MediaTagAacsMediaIdentifier MediaTagType = 35
	// MediaTagAacsMkb: AACS Media Key Block
	MediaTagAacsMkb MediaTagType = 36
	// MediaTagAacsDataKeys: Extracted AACS title/volume keys (when decrypted)
	MediaTagAacsDataKeys MediaTagType = 37
	// MediaTagAacsLbaExtents: LBA extents requiring bus encryption
	MediaTagAacsLbaExtents MediaTagType = 38
	// MediaTagCprmMkb: CPRM Media Key Block
	MediaTagCprmMkb MediaTagType = 39
	// MediaTagHybridRecognizedLayers: Hybrid disc recognized layer combinations (e.g. CD/DVD/BD)
	MediaTagHybridRecognizedLayers MediaTagType = 40
	// MediaTagMmcWriteProtection: Write protection status (MMC GET CONFIG)
	MediaTagMmcWriteProtection MediaTagType = 41
	// MediaTagMmcDiscInformation: Disc Information (recordable status, erasable, last session)
	MediaTagMmcDiscInformation MediaTagType = 42
	// MediaTagMmcTrackResourcesInformation: Track Resources (allocated/open track data)
	MediaTagMmcTrackResourcesInformation MediaTagType = 43
	// MediaTagMmcPowResourcesInformation: Pseudo OverWrite resources (BD-R POW)
	MediaTagMmcPowResourcesInformation MediaTagType = 44
	// MediaTagScsiInquiry: SCSI INQUIRY standard data (SPC-*)
	MediaTagScsiInquiry MediaTagType = 45
	// MediaTagScsiModePage2A: SCSI Mode Page 2Ah (CD/DVD capabilities)
	MediaTagScsiModePage2A MediaTagType = 46
	// MediaTagAtaIdentify: ATA IDENTIFY DEVICE (512 bytes)
	MediaTagAtaIdentify MediaTagType = 47
	// MediaTagAtapiIdentify: ATA PACKET IDENTIFY DEVICE
	MediaTagAtapiIdentify MediaTagType = 48
	// MediaTagPcmciaCis: PCMCIA/CardBus CIS tuple chain
	MediaTagPcmciaCis MediaTagType = 49
	// MediaTagSdCid: SecureDigital Card ID register
	MediaTagSdCid MediaTagType = 50
	// MediaTagSdCsd: SecureDigital Card Specific Data
	MediaTagSdCsd MediaTagType = 51
	// MediaTagSdScr: SecureDigital Configuration Register
	MediaTagSdScr MediaTagType = 52
	// MediaTagSdOcr: SecureDigital Operation Conditions (voltage)
	MediaTagSdOcr MediaTagType = 53
	// MediaTagMmcCid: MMC Card ID
	MediaTagMmcCid MediaTagType = 54
	// MediaTagMmcCsd: MMC Card Specific Data
	MediaTagMmcCsd MediaTagType = 55
	// MediaTagMmcOcr: MMC Operation Conditions
	MediaTagMmcOcr MediaTagType = 56
	// MediaTagExtendedCsd: MMC Extended CSD (512 bytes)
	MediaTagExtendedCsd MediaTagType = 57
	// MediaTagXboxSecuritySector: Xbox/Xbox 360 Security Sector (SS.bin)
	MediaTagXboxSecuritySector MediaTagType = 58
	// MediaTagFloppyLeadOut: Manufacturer / duplication cylinder (floppy special data)
	MediaTagFloppyLeadOut MediaTagType = 59
	// MediaTagDiscControlBlock: DVD Disc Control Blocks
	MediaTagDiscControlBlock MediaTagType = 60
	// MediaTagCdFirstTrackPregap: First track pregap (index 0)
	MediaTagCdFirstTrackPregap MediaTagType = 61
	// MediaTagCdLeadOut: Lead-out area contents
	MediaTagCdLeadOut MediaTagType = 62
	// MediaTagScsiModeSense6: Raw MODE SENSE (6) data
	MediaTagScsiModeSense6 MediaTagType = 63
	// MediaTagScsiModeSense10: Raw MODE SENSE (10) data
	MediaTagScsiModeSense10 MediaTagType = 64
	// MediaTagUsbDescriptors: Concatenated USB descriptors (device/config/interface)
	MediaTagUsbDescriptors MediaTagType = 65
	// MediaTagXboxDmi: Xbox Disc Manufacturing Info (DMI)
	MediaTagXboxDmi MediaTagType = 66
	// MediaTagXboxPfi: Xbox Physical Format Information (PFI)
	MediaTagXboxPfi MediaTagType = 67
	// MediaTagCdLeadIn: Raw lead-in (TOC frames)
	MediaTagCdLeadIn MediaTagType = 68
	// MediaTagMiniDiscType: 8 bytes response that seems to define type of MiniDisc
	MediaTagMiniDiscType MediaTagType = 69
	// MediaTagMiniDiscD5: 4 bytes response to vendor command D5h
	MediaTagMiniDiscD5 MediaTagType = 70
	// MediaTagMiniDiscUtoc: User TOC, contains fragments, track names, and can be from 1 to 3 sectors of 2336 bytes
	MediaTagMiniDiscUtoc MediaTagType = 71
	// MediaTagMiniDiscDtoc: Not entirely clear kind of TOC that only appears on MD-DATA discs
	MediaTagMiniDiscDtoc MediaTagType = 72
	// MediaTagDvdDiscKeyDecrypted: Decrypted DVD disc key,
	MediaTagDvdDiscKeyDecrypted MediaTagType = 73
	// MediaTagDvdPfi2ndLayer: DVD Physical Format Information for the second layer
	MediaTagDvdPfi2ndLayer MediaTagType = 74
	// MediaTagFloppyWriteProtect: Write protection status of the floppy disk
	MediaTagFloppyWriteProtect MediaTagType = 75
	// MediaTagWiiUDiscKey: Nintendo Wii U disc key (16 bytes, from non-readable disc area)
	MediaTagWiiUDiscKey MediaTagType = 76
	// MediaTagPs3DiscKey: PS3 derived disc key (16 bytes)
	MediaTagPs3DiscKey MediaTagType = 77
	// MediaTagPs3Data1: PS3 data1 key (16 bytes, from disc)
	MediaTagPs3Data1 MediaTagType = 78
	// MediaTagPs3Data2: PS3 data2 key (16 bytes, from disc)
	MediaTagPs3Data2 MediaTagType = 79
	// MediaTagPs3Pic: PS3 PIC data (115 bytes, from disc lead-in)
	MediaTagPs3Pic MediaTagType = 80
	// MediaTagPs3EncryptionMap: PS3 encryption region map (serialized from sector 0)
	MediaTagPs3EncryptionMap MediaTagType = 81
	// MediaTagWiiUPartitionKeyMap: Nintendo Wii U partition-to-key mapping with regions
	MediaTagWiiUPartitionKeyMap MediaTagType = 82
	// MediaTagWiiPartitionKeyMap: Nintendo Wii partition-to-key mapping with regions
	MediaTagWiiPartitionKeyMap MediaTagType = 83
	// MediaTagNgcwJunkMap: Nintendo GameCube/Wii junk region map with LFG seeds
	MediaTagNgcwJunkMap MediaTagType = 84
	// MediaTagAacsMediaKey: AACS Media Key
	MediaTagAacsMediaKey MediaTagType = 85
	// MediaTagAacsVolumeUniqueKey: AACS Volume Unique Key
	MediaTagAacsVolumeUniqueKey MediaTagType = 86
)

var _MediaTagTypeNames = map[MediaTagType]string{
	MediaTagCdToc:                        "MediaTagCdToc",
	MediaTagSessionInfo:                  "MediaTagSessionInfo",
	MediaTagFullToc:                      "MediaTagFullToc",
	MediaTagCdPma:                        "MediaTagCdPma",
	MediaTagCdAtip:                       "MediaTagCdAtip",
	MediaTagCdText:                       "MediaTagCdText",
	MediaTagCdMcn:                        "MediaTagCdMcn",
	MediaTagDvdPfi:                       "MediaTagDvdPfi",
	MediaTagDvdCmi:                       "MediaTagDvdCmi",
	MediaTagDvdDiscKey:                   "MediaTagDvdDiscKey",
	MediaTagDvdBca:                       "MediaTagDvdBca",
	MediaTagDvdDmi:                       "MediaTagDvdDmi",
	MediaTagDvdMediaIdentifier:           "MediaTagDvdMediaIdentifier",
	MediaTagDvdMkb:                       "MediaTagDvdMkb",
	MediaTagDvdRamDds:                    "MediaTagDvdRamDds",
	MediaTagDvdRamMediumStatus:           "MediaTagDvdRamMediumStatus",
	MediaTagDvdRamSpareArea:              "MediaTagDvdRamSpareArea",
	MediaTagDvdrRmd:                      "MediaTagDvdrRmd",
	MediaTagDvdrPreRecordedInfo:          "MediaTagDvdrPreRecordedInfo",
	MediaTagDvdrMediaIdentifier:          "MediaTagDvdrMediaIdentifier",
	MediaTagDvdrPfi:                      "MediaTagDvdrPfi",
	MediaTagDvdAdip:                      "MediaTagDvdAdip",
	MediaTagHddvdCpi:                     "MediaTagHddvdCpi",
	MediaTagHddvdMediumStatus:            "MediaTagHddvdMediumStatus",
	MediaTagDvddlLayerCapacity:           "MediaTagDvddlLayerCapacity",
	MediaTagDvddlMiddleZoneAddress:       "MediaTagDvddlMiddleZoneAddress",
	MediaTagDvddlJumpIntervalSize:        "MediaTagDvddlJumpIntervalSize",
	MediaTagDvddlManualLayerJumpLba:      "MediaTagDvddlManualLayerJumpLba",
	MediaTagBlurayDi:                     "MediaTagBlurayDi",
	MediaTagBlurayBca:                    "MediaTagBlurayBca",
	MediaTagBlurayDds:                    "MediaTagBlurayDds",
	MediaTagBlurayCartridgeStatus:        "MediaTagBlurayCartridgeStatus",
	MediaTagBluraySpareArea:              "MediaTagBluraySpareArea",
	MediaTagAacsVolumeIdentifier:         "MediaTagAacsVolumeIdentifier",
	MediaTagAacsSerialNumber:             "MediaTagAacsSerialNumber",
	MediaTagAacsMediaIdentifier:          "MediaTagAacsMediaIdentifier",
	MediaTagAacsMkb:                      "MediaTagAacsMkb",
	MediaTagAacsDataKeys:                 "MediaTagAacsDataKeys",
	MediaTagAacsLbaExtents:               "MediaTagAacsLbaExtents",
	MediaTagCprmMkb:                      "MediaTagCprmMkb",
	MediaTagHybridRecognizedLayers:       "MediaTagHybridRecognizedLayers",
	MediaTagMmcWriteProtection:           "MediaTagMmcWriteProtection",
	MediaTagMmcDiscInformation:           "MediaTagMmcDiscInformation",
	MediaTagMmcTrackResourcesInformation: "MediaTagMmcTrackResourcesInformation",
	MediaTagMmcPowResourcesInformation:   "MediaTagMmcPowResourcesInformation",
	MediaTagScsiInquiry:                  "MediaTagScsiInquiry",
	MediaTagScsiModePage2A:               "MediaTagScsiModePage2A",
	MediaTagAtaIdentify:                  "MediaTagAtaIdentify",
	MediaTagAtapiIdentify:                "MediaTagAtapiIdentify",
	MediaTagPcmciaCis:                    "MediaTagPcmciaCis",
	MediaTagSdCid:                        "MediaTagSdCid",
	MediaTagSdCsd:                        "MediaTagSdCsd",
	MediaTagSdScr:                        "MediaTagSdScr",
	MediaTagSdOcr:                        "MediaTagSdOcr",
	MediaTagMmcCid:                       "MediaTagMmcCid",
	MediaTagMmcCsd:                       "MediaTagMmcCsd",
	MediaTagMmcOcr:                       "MediaTagMmcOcr",
	MediaTagExtendedCsd:                  "MediaTagExtendedCsd",
	MediaTagXboxSecuritySector:           "MediaTagXboxSecuritySector",
	MediaTagFloppyLeadOut:                "MediaTagFloppyLeadOut",
	MediaTagDiscControlBlock:             "MediaTagDiscControlBlock",
	MediaTagCdFirstTrackPregap:           "MediaTagCdFirstTrackPregap",
	MediaTagCdLeadOut:                    "MediaTagCdLeadOut",
	MediaTagScsiModeSense6:               "MediaTagScsiModeSense6",
	MediaTagScsiModeSense10:              "MediaTagScsiModeSense10",
	MediaTagUsbDescriptors:               "MediaTagUsbDescriptors",
	MediaTagXboxDmi:                      "MediaTagXboxDmi",
	MediaTagXboxPfi:                      "MediaTagXboxPfi",
	MediaTagCdLeadIn:                     "MediaTagCdLeadIn",
	MediaTagMiniDiscType:                 "MediaTagMiniDiscType",
	MediaTagMiniDiscD5:                   "MediaTagMiniDiscD5",
	MediaTagMiniDiscUtoc:                 "MediaTagMiniDiscUtoc",
	MediaTagMiniDiscDtoc:                 "MediaTagMiniDiscDtoc",
	MediaTagDvdDiscKeyDecrypted:          "MediaTagDvdDiscKeyDecrypted",
	MediaTagDvdPfi2ndLayer:               "MediaTagDvdPfi2ndLayer",
	MediaTagFloppyWriteProtect:           "MediaTagFloppyWriteProtect",
	MediaTagWiiUDiscKey:                  "MediaTagWiiUDiscKey",
	MediaTagPs3DiscKey:                   "MediaTagPs3DiscKey",
	MediaTagPs3Data1:                     "MediaTagPs3Data1",
	MediaTagPs3Data2:                     "MediaTagPs3Data2",
	MediaTagPs3Pic:                       "MediaTagPs3Pic",
	MediaTagPs3EncryptionMap:             "MediaTagPs3EncryptionMap",
	MediaTagWiiUPartitionKeyMap:          "MediaTagWiiUPartitionKeyMap",
	MediaTagWiiPartitionKeyMap:           "MediaTagWiiPartitionKeyMap",
	MediaTagNgcwJunkMap:                  "MediaTagNgcwJunkMap",
	MediaTagAacsMediaKey:                 "MediaTagAacsMediaKey",
	MediaTagAacsVolumeUniqueKey:          "MediaTagAacsVolumeUniqueKey",
}

// String returns the constant name of t, or a numeric representation if unknown.
func (t MediaTagType) String() string {
	if s, ok := _MediaTagTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("MediaTagType(%d)", int32(t))
}

// MaxSectorTag is the highest defined SectorTagType.
const MaxSectorTag = SectorTagBdSectorEdc

// MaxMediaTag is the highest defined MediaTagType.
const MaxMediaTag = MediaTagAacsVolumeUniqueKey

var dataTypeToMediaTag = map[DataType]MediaTagType{
	DataTypeCdToc:                        MediaTagCdToc,
	DataTypeSessionInfo:                  MediaTagSessionInfo,
	DataTypeFullToc:                      MediaTagFullToc,
	DataTypeCdPma:                        MediaTagCdPma,
	DataTypeCdAtip:                       MediaTagCdAtip,
	DataTypeCdText:                       MediaTagCdText,
	DataTypeDvdPfi:                       MediaTagDvdPfi,
	DataTypeDvdPfi2ndLayer:               MediaTagDvdPfi2ndLayer,
	DataTypeDvdCmi:                       MediaTagDvdCmi,
	DataTypeDvdDiscKey:                   MediaTagDvdDiscKey,
	DataTypeDvdBca:                       MediaTagDvdBca,
	DataTypeDvdDmi:                       MediaTagDvdDmi,
	DataTypeDvdMediaIdentifier:           MediaTagDvdMediaIdentifier,
	DataTypeDvdMkb:                       MediaTagDvdMkb,
	DataTypeDvdRamDds:                    MediaTagDvdRamDds,
	DataTypeDvdRamMediumStatus:           MediaTagDvdRamMediumStatus,
	DataTypeDvdRamSpareArea:              MediaTagDvdRamSpareArea,
	DataTypeDvdrRmd:                      MediaTagDvdrRmd,
	DataTypeDvdrPreRecordedInfo:          MediaTagDvdrPreRecordedInfo,
	DataTypeDvdrMediaIdentifier:          MediaTagDvdrMediaIdentifier,
	DataTypeDvdrPfi:                      MediaTagDvdrPfi,
	DataTypeDvdAdip:                      MediaTagDvdAdip,
	DataTypeHddvdCpi:                     MediaTagHddvdCpi,
	DataTypeHddvdMediumStatus:            MediaTagHddvdMediumStatus,
	DataTypeDvddlLayerCapacity:           MediaTagDvddlLayerCapacity,
	DataTypeDvddlMiddleZoneAddress:       MediaTagDvddlMiddleZoneAddress,
	DataTypeDvddlJumpIntervalSize:        MediaTagDvddlJumpIntervalSize,
	DataTypeDvddlManualLayerJumpLba:      MediaTagDvddlManualLayerJumpLba,
	DataTypeBlurayDi:                     MediaTagBlurayDi,
	DataTypeBlurayBca:                    MediaTagBlurayBca,
	DataTypeBlurayDds:                    MediaTagBlurayDds,
	DataTypeBlurayCartridgeStatus:        MediaTagBlurayCartridgeStatus,
	DataTypeBluraySpareArea:              MediaTagBluraySpareArea,
	DataTypeAacsVolumeIdentifier:         MediaTagAacsVolumeIdentifier,
	DataTypeAacsSerialNumber:             MediaTagAacsSerialNumber,
	DataTypeAacsMediaIdentifier:          MediaTagAacsMediaIdentifier,
	DataTypeAacsMkb:                      MediaTagAacsMkb,
	DataTypeAacsDataKeys:                 MediaTagAacsDataKeys,
	DataTypeAacsLbaExtents:               MediaTagAacsLbaExtents,
	DataTypeCprmMkb:                      MediaTagCprmMkb,
	DataTypeHybridRecognizedLayers:       MediaTagHybridRecognizedLayers,
	DataTypeMmcWriteProtection:           MediaTagMmcWriteProtection,
	DataTypeMmcDiscInformation:           MediaTagMmcDiscInformation,
	DataTypeMmcTrackResourcesInformation: MediaTagMmcTrackResourcesInformation,
	DataTypeMmcPowResourcesInformation:   MediaTagMmcPowResourcesInformation,
	DataTypeScsiInquiry:                  MediaTagScsiInquiry,
	DataTypeScsiModePage2A:               MediaTagScsiModePage2A,
	DataTypeAtaIdentify:                  MediaTagAtaIdentify,
	DataTypeAtapiIdentify:                MediaTagAtapiIdentify,
	DataTypePcmciaCis:                    MediaTagPcmciaCis,
	DataTypeSdCid:                        MediaTagSdCid,
	DataTypeSdCsd:                        MediaTagSdCsd,
	DataTypeSdScr:                        MediaTagSdScr,
	DataTypeSdOcr:                        MediaTagSdOcr,
	DataTypeMmcCid:                       MediaTagMmcCid,
	DataTypeMmcCsd:                       MediaTagMmcCsd,
	DataTypeMmcOcr:                       MediaTagMmcOcr,
	DataTypeExtendedCsd:                  MediaTagExtendedCsd,
	DataTypeXboxSecuritySector:           MediaTagXboxSecuritySector,
	DataTypeFloppyLeadOut:                MediaTagFloppyLeadOut,
	DataTypeDiscControlBlock:             MediaTagDiscControlBlock,
	DataTypeCdFirstTrackPregap:           MediaTagCdFirstTrackPregap,
	DataTypeCdLeadOut:                    MediaTagCdLeadOut,
	DataTypeScsiModeSense6:               MediaTagScsiModeSense6,
	DataTypeScsiModeSense10:              MediaTagScsiModeSense10,
	DataTypeUsbDescriptors:               MediaTagUsbDescriptors,
	DataTypeXboxDmi:                      MediaTagXboxDmi,
	DataTypeXboxPfi:                      MediaTagXboxPfi,
	DataTypeCdMcn:                        MediaTagCdMcn,
	DataTypeCdLeadIn:                     MediaTagCdLeadIn,
	DataTypeDvdDiscKeyDecrypted:          MediaTagDvdDiscKeyDecrypted,
	DataTypeFloppyWriteProtect:           MediaTagFloppyWriteProtect,
	DataTypeWiiUDiscKey:                  MediaTagWiiUDiscKey,
	DataTypePs3DiscKey:                   MediaTagPs3DiscKey,
	DataTypePs3Data1:                     MediaTagPs3Data1,
	DataTypePs3Data2:                     MediaTagPs3Data2,
	DataTypePs3Pic:                       MediaTagPs3Pic,
	DataTypePs3EncryptionMap:             MediaTagPs3EncryptionMap,
	DataTypeWiiUPartitionKeyMap:          MediaTagWiiUPartitionKeyMap,
	DataTypeWiiPartitionKeyMap:           MediaTagWiiPartitionKeyMap,
	DataTypeNgcwJunkMap:                  MediaTagNgcwJunkMap,
	DataTypeAacsMediaKey:                 MediaTagAacsMediaKey,
	DataTypeAacsVolumeUniqueKey:          MediaTagAacsVolumeUniqueKey,
}

var mediaTagToDataType = map[MediaTagType]DataType{
	MediaTagCdToc:                        DataTypeCdToc,
	MediaTagSessionInfo:                  DataTypeSessionInfo,
	MediaTagFullToc:                      DataTypeFullToc,
	MediaTagCdPma:                        DataTypeCdPma,
	MediaTagCdAtip:                       DataTypeCdAtip,
	MediaTagCdText:                       DataTypeCdText,
	MediaTagDvdPfi:                       DataTypeDvdPfi,
	MediaTagDvdPfi2ndLayer:               DataTypeDvdPfi2ndLayer,
	MediaTagDvdCmi:                       DataTypeDvdCmi,
	MediaTagDvdDiscKey:                   DataTypeDvdDiscKey,
	MediaTagDvdBca:                       DataTypeDvdBca,
	MediaTagDvdDmi:                       DataTypeDvdDmi,
	MediaTagDvdMediaIdentifier:           DataTypeDvdMediaIdentifier,
	MediaTagDvdMkb:                       DataTypeDvdMkb,
	MediaTagDvdRamDds:                    DataTypeDvdRamDds,
	MediaTagDvdRamMediumStatus:           DataTypeDvdRamMediumStatus,
	MediaTagDvdRamSpareArea:              DataTypeDvdRamSpareArea,
	MediaTagDvdrRmd:                      DataTypeDvdrRmd,
	MediaTagDvdrPreRecordedInfo:          DataTypeDvdrPreRecordedInfo,
	MediaTagDvdrMediaIdentifier:          DataTypeDvdrMediaIdentifier,
	MediaTagDvdrPfi:                      DataTypeDvdrPfi,
	MediaTagDvdAdip:                      DataTypeDvdAdip,
	MediaTagHddvdCpi:                     DataTypeHddvdCpi,
	MediaTagHddvdMediumStatus:            DataTypeHddvdMediumStatus,
	MediaTagDvddlLayerCapacity:           DataTypeDvddlLayerCapacity,
	MediaTagDvddlMiddleZoneAddress:       DataTypeDvddlMiddleZoneAddress,
	MediaTagDvddlJumpIntervalSize:        DataTypeDvddlJumpIntervalSize,
	MediaTagDvddlManualLayerJumpLba:      DataTypeDvddlManualLayerJumpLba,
	MediaTagBlurayDi:                     DataTypeBlurayDi,
	MediaTagBlurayBca:                    DataTypeBlurayBca,
	MediaTagBlurayDds:                    DataTypeBlurayDds,
	MediaTagBlurayCartridgeStatus:        DataTypeBlurayCartridgeStatus,
	MediaTagBluraySpareArea:              DataTypeBluraySpareArea,
	MediaTagAacsVolumeIdentifier:         DataTypeAacsVolumeIdentifier,
	MediaTagAacsSerialNumber:             DataTypeAacsSerialNumber,
	MediaTagAacsMediaIdentifier:          DataTypeAacsMediaIdentifier,
	MediaTagAacsMkb:                      DataTypeAacsMkb,
	MediaTagAacsDataKeys:                 DataTypeAacsDataKeys,
	MediaTagAacsLbaExtents:               DataTypeAacsLbaExtents,
	MediaTagCprmMkb:                      DataTypeCprmMkb,
	MediaTagHybridRecognizedLayers:       DataTypeHybridRecognizedLayers,
	MediaTagMmcWriteProtection:           DataTypeMmcWriteProtection,
	MediaTagMmcDiscInformation:           DataTypeMmcDiscInformation,
	MediaTagMmcTrackResourcesInformation: DataTypeMmcTrackResourcesInformation,
	MediaTagMmcPowResourcesInformation:   DataTypeMmcPowResourcesInformation,
	MediaTagScsiInquiry:                  DataTypeScsiInquiry,
	MediaTagScsiModePage2A:               DataTypeScsiModePage2A,
	MediaTagAtaIdentify:                  DataTypeAtaIdentify,
	MediaTagAtapiIdentify:                DataTypeAtapiIdentify,
	MediaTagPcmciaCis:                    DataTypePcmciaCis,
	MediaTagSdCid:                        DataTypeSdCid,
	MediaTagSdCsd:                        DataTypeSdCsd,
	MediaTagSdScr:                        DataTypeSdScr,
	MediaTagSdOcr:                        DataTypeSdOcr,
	MediaTagMmcCid:                       DataTypeMmcCid,
	MediaTagMmcCsd:                       DataTypeMmcCsd,
	MediaTagMmcOcr:                       DataTypeMmcOcr,
	MediaTagExtendedCsd:                  DataTypeExtendedCsd,
	MediaTagXboxSecuritySector:           DataTypeXboxSecuritySector,
	MediaTagFloppyLeadOut:                DataTypeFloppyLeadOut,
	MediaTagDiscControlBlock:             DataTypeDiscControlBlock,
	MediaTagCdFirstTrackPregap:           DataTypeCdFirstTrackPregap,
	MediaTagCdLeadOut:                    DataTypeCdLeadOut,
	MediaTagScsiModeSense6:               DataTypeScsiModeSense6,
	MediaTagScsiModeSense10:              DataTypeScsiModeSense10,
	MediaTagUsbDescriptors:               DataTypeUsbDescriptors,
	MediaTagXboxDmi:                      DataTypeXboxDmi,
	MediaTagXboxPfi:                      DataTypeXboxPfi,
	MediaTagCdMcn:                        DataTypeCdMcn,
	MediaTagCdLeadIn:                     DataTypeCdLeadIn,
	MediaTagDvdDiscKeyDecrypted:          DataTypeDvdDiscKeyDecrypted,
	MediaTagFloppyWriteProtect:           DataTypeFloppyWriteProtect,
	MediaTagWiiUDiscKey:                  DataTypeWiiUDiscKey,
	MediaTagPs3DiscKey:                   DataTypePs3DiscKey,
	MediaTagPs3Data1:                     DataTypePs3Data1,
	MediaTagPs3Data2:                     DataTypePs3Data2,
	MediaTagPs3Pic:                       DataTypePs3Pic,
	MediaTagPs3EncryptionMap:             DataTypePs3EncryptionMap,
	MediaTagWiiUPartitionKeyMap:          DataTypeWiiUPartitionKeyMap,
	MediaTagWiiPartitionKeyMap:           DataTypeWiiPartitionKeyMap,
	MediaTagNgcwJunkMap:                  DataTypeNgcwJunkMap,
	MediaTagAacsMediaKey:                 DataTypeAacsMediaKey,
	MediaTagAacsVolumeUniqueKey:          DataTypeAacsVolumeUniqueKey,
}
