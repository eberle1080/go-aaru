package main

import "github.com/eberle1080/go-aaru"

// ddtHeader2Alpha20 is the version 2 deduplication table header as written by
// Aaru alpha 16 and earlier, before the negative and overflow sector counts
// were widened to 32 bits. Packed little-endian it is 81 bytes long, 4 bytes
// shorter than aaru.DDTHeader2.
type ddtHeader2Alpha20 struct {
	Identifier          aaru.BlockType       // Block identifier, must be DeDuplicationTable2.
	Type                aaru.DataType        // Data classification of the sectors referenced.
	Compression         aaru.CompressionType // Compression of the table body.
	Levels              uint8                // Total number of hierarchy levels.
	TableLevel          uint8                // Level of this table (0 = root).
	PreviousLevelOffset uint64               // Offset of the parent table, 0 if root.
	Negative            uint16               // Leading negative LBA count.
	Blocks              uint64               // Total internal span (negative + usable + overflow).
	Overflow            uint16               // Trailing overflow sectors.
	Start               uint64               // Base internal index covered by this table.
	BlockAlignmentShift uint8                // log2 of block alignment.
	DataShift           uint8                // log2 of sectors per block index increment.
	TableShift          uint8                // log2 of sectors per primary entry.
	Entries             uint64               // Number of entries in the table.
	CmpLength           uint64               // Compressed payload size.
	Length              uint64               // Uncompressed payload size.
	CmpCRC64            uint64               // CRC64 of the compressed payload.
	CRC64               uint64               // CRC64 of the uncompressed payload.
}

// upgrade converts an alpha 20 header to the current layout, field by field.
func (h *ddtHeader2Alpha20) upgrade() aaru.DDTHeader2 {
	return aaru.DDTHeader2{
		Identifier:          h.Identifier,
		Type:                h.Type,
		Compression:         h.Compression,
		Levels:              h.Levels,
		TableLevel:          h.TableLevel,
		PreviousLevelOffset: h.PreviousLevelOffset,
		Negative:            uint32(h.Negative),
		Blocks:              h.Blocks,
		Overflow:            uint32(h.Overflow),
		Start:               h.Start,
		BlockAlignmentShift: h.BlockAlignmentShift,
		DataShift:           h.DataShift,
		TableShift:          h.TableShift,
		Entries:             h.Entries,
		CmpLength:           h.CmpLength,
		Length:              h.Length,
		CmpCRC64:            h.CmpCRC64,
		CRC64:               h.CRC64,
	}
}
