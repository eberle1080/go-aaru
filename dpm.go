package aaru

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/eberle1080/go-aaru/crc64"
)

// This file implements the DPM block (dpmPayloadLength, ValidateDPM,
// recoverDPMBlock, processDPMBlock), its accessors and the close-time
// removeDPMIndexEntries and writeDPMBlock.
//
// The data position measurement block (DPM*) records the physical angle of
// sectors along the spiral track of an optical disc. It is a DPMHeader
// followed, without padding, by the last sector of every layer but the last
// (uint64 each), the control points (DPMEntry) and the rotation calibrations
// (DPMCalibration).

// DPM is a decoded data position measurement block.
type DPM struct {
	// NominalSpacing is the nominal number of sectors between control
	// points.
	NominalSpacing uint32
	// TimingUnit is the number of sectors the drive reads as a whole (1 on
	// CD, 16 on DVD, 32 on BD), 0 if unknown.
	TimingUnit uint16
	// Speed is the requested speed as a multiple of the medium base speed;
	// 0xFFFF means maximum, 0 unknown.
	Speed uint16
	// Flags holds DPMFlagOppositeTrackPath; other bits are reserved.
	Flags uint8
	// Reserved must be 0.
	Reserved uint16
	// LayerEnds holds the last sector of every layer but the last (at most
	// 255 values).
	LayerEnds []uint64
	// Entries are the control points.
	Entries []DPMEntry
	// Calibrations are the rotation calibrations done while measuring.
	Calibrations []DPMCalibration
}

// dpmPayloadLength is the payload length a DPM header describes.
func dpmPayloadLength(h *DPMHeader) uint64 {
	return uint64(h.LayerEnds)*8 + uint64(h.Entries)*dpmEntrySize + uint64(h.Calibrations)*dpmCalibrationSize
}

// ValidateDPM checks a serialized DPM block (header and payload): it must
// hold at least a header
// (ErrIncorrectDataSize), have the DPM identifier (ErrCannotReadBlock), a
// length matching both the data and the counts of the header
// (ErrIncorrectDataSize) and a valid payload CRC64 (ErrInvalidBlockCRC).
func ValidateDPM(block []byte) error {
	if len(block) < dpmHeaderSize {
		return fmt.Errorf("DPM block of %d bytes: %w", len(block), ErrIncorrectDataSize)
	}
	h, _ := unmarshal[DPMHeader](block)
	if h.Identifier != BlockTypeDataPositionMeasurementBlock {
		return fmt.Errorf("DPM block identifier %#x: %w", uint32(h.Identifier), ErrCannotReadBlock)
	}
	if h.Length != uint64(len(block)-dpmHeaderSize) || h.Length != dpmPayloadLength(&h) || h.Length > math.MaxUint32 {
		return fmt.Errorf("DPM block length %d does not match its data (%d bytes) or contents: %w", h.Length,
			len(block)-dpmHeaderSize, ErrIncorrectDataSize)
	}
	if crc64.Checksum(block[dpmHeaderSize:]) != h.CRC64 {
		return fmt.Errorf("DPM block: %w", ErrInvalidBlockCRC)
	}
	return nil
}

// ParseDPM decodes a serialized DPM block after validating it (see
// ValidateDPM).
func ParseDPM(block []byte) (*DPM, error) {
	if err := ValidateDPM(block); err != nil {
		return nil, err
	}
	h, _ := unmarshal[DPMHeader](block)
	d := &DPM{NominalSpacing: h.NominalSpacing, TimingUnit: h.TimingUnit, Speed: h.Speed, Flags: h.Flags,
		Reserved: h.Reserved}
	p := block[dpmHeaderSize:]
	d.LayerEnds = decodeUint64s(p[:8*int(h.LayerEnds)])
	p = p[8*int(h.LayerEnds):]
	var err error
	if d.Entries, err = unmarshalSlice[DPMEntry](p, int(h.Entries)); err != nil {
		return nil, fmt.Errorf("DPM entries: %w: %w", ErrIncorrectDataSize, err)
	}
	p = p[int(h.Entries)*dpmEntrySize:]
	if d.Calibrations, err = unmarshalSlice[DPMCalibration](p, int(h.Calibrations)); err != nil {
		return nil, fmt.Errorf("DPM calibrations: %w: %w", ErrIncorrectDataSize, err)
	}
	return d, nil
}

// MarshalBinary serializes d as a DPM block (header and payload) with its
// length and CRC64 computed.
func (d *DPM) MarshalBinary() ([]byte, error) {
	if len(d.LayerEnds) > math.MaxUint8 || uint64(len(d.Entries)) > math.MaxUint32 ||
		uint64(len(d.Calibrations)) > math.MaxUint32 {
		return nil, fmt.Errorf("DPM with %d layer ends, %d entries and %d calibrations: %w", len(d.LayerEnds),
			len(d.Entries), len(d.Calibrations), ErrIncorrectDataSize)
	}
	h := DPMHeader{
		Identifier:     BlockTypeDataPositionMeasurementBlock,
		Entries:        uint32(len(d.Entries)),
		Calibrations:   uint32(len(d.Calibrations)),
		NominalSpacing: d.NominalSpacing,
		TimingUnit:     d.TimingUnit,
		Speed:          d.Speed,
		LayerEnds:      uint8(len(d.LayerEnds)),
		Flags:          d.Flags,
		Reserved:       d.Reserved,
	}
	h.Length = dpmPayloadLength(&h)
	if h.Length > math.MaxUint32 {
		return nil, fmt.Errorf("DPM payload of %d bytes: %w", h.Length, ErrIncorrectDataSize)
	}
	payload := make([]byte, 0, h.Length)
	for _, v := range d.LayerEnds {
		payload = binary.LittleEndian.AppendUint64(payload, v)
	}
	if len(d.Entries) > 0 {
		payload = appendMarshal(payload, d.Entries)
	}
	if len(d.Calibrations) > 0 {
		payload = appendMarshal(payload, d.Calibrations)
	}
	h.CRC64 = crc64.Checksum(payload)
	out := appendMarshal(make([]byte, 0, dpmHeaderSize+len(payload)), &h)
	return append(out, payload...), nil
}

// recoverDPMBlock reconstructs the DPM block that failed to load at
// img.dpmDamagedOffset from the erasure coding parity.
func (img *Image) recoverDPMBlock() error {
	if img.dpmDamagedOffset == 0 {
		return ErrCannotReadBlock
	}
	rec, err := img.ecRecoverMetaBlock(img.dpmDamagedOffset)
	if err != nil || rec == nil {
		return fmt.Errorf("recovering DPM block at %d: %w", img.dpmDamagedOffset, ErrCannotReadBlock)
	}
	if len(rec) < dpmHeaderSize {
		return fmt.Errorf("recovered DPM block too short: %w", ErrCannotReadBlock)
	}
	h, _ := unmarshal[DPMHeader](rec)
	if h.Length > math.MaxUint32 || uint64(len(rec)) < dpmHeaderSize+h.Length {
		return fmt.Errorf("recovered DPM block truncated: %w", ErrCannotReadBlock)
	}
	block := rec[:dpmHeaderSize+h.Length]
	if ValidateDPM(block) != nil {
		return fmt.Errorf("recovered DPM block invalid: %w", ErrCannotReadBlock)
	}
	img.dpmBlock = bytes.Clone(block)
	img.dpmDamagedOffset = 0
	return nil
}

// processDPMBlock loads the DPM block described by e.
// A block that cannot be read or is invalid is recovered with erasure
// coding when the image has it, and ignored otherwise.
func (img *Image) processDPMBlock(e IndexEntry) error {
	img.dpmDamagedOffset = e.Offset

	var hb [dpmHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return img.recoverDPMBlock()
	}
	h, _ := unmarshal[DPMHeader](hb[:])
	if h.Identifier != BlockTypeDataPositionMeasurementBlock || h.Length != dpmPayloadLength(&h) ||
		h.Length > math.MaxUint32 {
		return img.recoverDPMBlock()
	}
	payload, err := img.readAt(e.Offset+dpmHeaderSize, h.Length)
	if err != nil {
		return img.recoverDPMBlock()
	}
	block := append(hb[:], payload...)
	if ValidateDPM(block) != nil {
		return img.recoverDPMBlock()
	}
	img.dpmBlock = block
	img.dpmDamagedOffset = 0
	img.info.ImageSize += h.Length
	return nil
}

// DPMRaw returns the serialized DPM block, header included,
// or ErrMetadataNotPresent if the image has none.
func (img *Image) DPMRaw() ([]byte, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if len(img.dpmBlock) == 0 {
		return nil, fmt.Errorf("no DPM block: %w", ErrMetadataNotPresent)
	}
	return bytes.Clone(img.dpmBlock), nil
}

// DPM returns the decoded DPM block, or ErrMetadataNotPresent if the image
// has none.
func (img *Image) DPM() (*DPM, error) {
	raw, err := img.DPMRaw()
	if err != nil {
		return nil, err
	}
	return ParseDPM(raw)
}

// SetDPMRaw stores a serialized DPM block, header included,
// after validating it (see ValidateDPM). It replaces any previous block.
func (img *Image) SetDPMRaw(block []byte) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if err := ValidateDPM(block); err != nil {
		return err
	}
	img.dpmBlock = bytes.Clone(block)
	img.dirty.dpmBlock = true
	return nil
}

// SetDPM stores d as the DPM block.
func (img *Image) SetDPM(d *DPM) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	block, err := d.MarshalBinary()
	if err != nil {
		return err
	}
	return img.SetDPMRaw(block)
}

// ClearDPM removes the DPM block; on resumed images it is
// removed from the index on Close.
func (img *Image) ClearDPM() error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	img.dpmBlock = nil
	img.dirty.dpmBlock = true
	return nil
}

// removeDPMIndexEntries drops every DPM index entry.
func (img *Image) removeDPMIndexEntries() {
	img.removeStaleIndexEntries(BlockTypeDataPositionMeasurementBlock)
}

// writeDPMBlock writes the DPM block at the aligned end of the file,
// replacing the index entry of any previous one, or removes the index entry
// when the block was cleared.
func (img *Image) writeDPMBlock() error {
	if len(img.dpmBlock) == 0 {
		img.removeDPMIndexEntries()
		return nil
	}
	off := img.closeAppendOffset()
	if err := img.writeAt(img.dpmBlock, off); err != nil {
		return fmt.Errorf("writing DPM block: %w: %w", ErrCannotWriteBlockData, err)
	}
	img.removeDPMIndexEntries()
	img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDataPositionMeasurementBlock,
		Offset: off})
	img.dirty.indexBlock = true
	return nil
}
