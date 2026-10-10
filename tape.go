package aaru

import (
	"fmt"
	"slices"

	"github.com/eberle1080/go-aaru/crc64"
)

// This file implements the tape blocks (processTapeFilesBlock,
// processTapePartitionsBlock, TapeFile, SetTapeFile, TapePartition,
// SetTapePartition, TapeFiles, TapePartitions) and the close-time
// writeTapeFileBlock and writeTapePartitionBlock.
//
// The entry count of the tape file (TFLE) and tape partition (TPBT) block
// headers is written; when it is 0 (as in some existing images) the reader
// derives it from the payload length.

// tapeFileKey is the map key used for tape files.
func tapeFileKey(partition uint8, file uint32) uint64 { return uint64(partition)<<32 | uint64(file) }

// putTapeFile stores f (a replaced entry moves to the end of the iteration
// order).
func (img *Image) putTapeFile(f TapeFileEntry) {
	key := tapeFileKey(f.Partition, f.File)
	if img.tapeFiles == nil {
		img.tapeFiles = make(map[uint64]TapeFileEntry)
	}
	if _, ok := img.tapeFiles[key]; ok {
		if i := slices.Index(img.tapeFileOrder, key); i >= 0 {
			img.tapeFileOrder = slices.Delete(img.tapeFileOrder, i, i+1)
		}
	}
	img.tapeFiles[key] = f
	img.tapeFileOrder = append(img.tapeFileOrder, key)
	img.dirty.tapeFileBlock = true
}

// putTapePartition stores p (a replaced entry moves to the end of the
// iteration order).
func (img *Image) putTapePartition(p TapePartitionEntry) {
	if img.tapePartitions == nil {
		img.tapePartitions = make(map[uint8]TapePartitionEntry)
	}
	if _, ok := img.tapePartitions[p.Number]; ok {
		if i := slices.Index(img.tapePartitionOrder, p.Number); i >= 0 {
			img.tapePartitionOrder = slices.Delete(img.tapePartitionOrder, i, i+1)
		}
	}
	img.tapePartitions[p.Number] = p
	img.tapePartitionOrder = append(img.tapePartitionOrder, p.Number)
	img.dirty.tapePartitionBlock = true
}

// readTapeTable reads the entries of a tape file or partition block: n
// entries of size bytes at off, n being derived from length when the
// header count is 0. It verifies the CRC64
// of the entries.
func (img *Image) readTapeTable(off uint64, n uint64, length uint64, size uint64, crc uint64) ([]byte, error) {
	if n == 0 && length > 0 && length%size == 0 {
		n = length / size
	}
	buf, err := img.readAt(off, n*size)
	if err != nil {
		return nil, fmt.Errorf("reading %d tape entries at %d: %w: %w", n, off, ErrCannotReadBlock, err)
	}
	if crc64.Checksum(buf) != crc {
		return nil, fmt.Errorf("tape entries at %d: %w", off, ErrInvalidBlockCRC)
	}
	return buf, nil
}

// processTapeFilesBlock loads the tape file block (TFLE) described by e.
func (img *Image) processTapeFilesBlock(e IndexEntry) error {
	var hb [tapeFileHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading tape file block header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[TapeFileHeader](hb[:])
	if h.Identifier != BlockTypeTapeFileBlock {
		return fmt.Errorf("tape file block at %d has identifier %#x: %w", e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	img.info.ImageSize += tapeFileEntrySize * uint64(h.Entries)
	buf, err := img.readTapeTable(e.Offset+tapeFileHeaderSize, uint64(h.Entries), h.Length, tapeFileEntrySize, h.CRC64)
	if err != nil {
		return err
	}
	entries, err := unmarshalSlice[TapeFileEntry](buf, len(buf)/tapeFileEntrySize)
	if err != nil {
		return err
	}
	for _, f := range entries {
		img.putTapeFile(f)
	}
	return nil
}

// processTapePartitionsBlock loads the tape partition block (TPBT)
// described by e.
func (img *Image) processTapePartitionsBlock(e IndexEntry) error {
	var hb [tapePartitionHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading tape partition block header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[TapePartitionHeader](hb[:])
	if h.Identifier != BlockTypeTapePartitionBlock {
		return fmt.Errorf("tape partition block at %d has identifier %#x: %w", e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	img.info.ImageSize += tapePartitionEntrySize * uint64(h.Entries)
	buf, err := img.readTapeTable(e.Offset+tapePartitionHeaderSize, uint64(h.Entries), h.Length,
		tapePartitionEntrySize, h.CRC64)
	if err != nil {
		return err
	}
	entries, err := unmarshalSlice[TapePartitionEntry](buf, len(buf)/tapePartitionEntrySize)
	if err != nil {
		return err
	}
	for _, p := range entries {
		img.putTapePartition(p)
	}
	return nil
}

// TapeFile returns the first and last block of a tape file,
// or ErrTapeFileNotFound.
func (img *Image) TapeFile(partition uint8, file uint32) (firstBlock, lastBlock uint64, err error) {
	if img.closed {
		return 0, 0, ErrClosed
	}
	f, ok := img.tapeFiles[tapeFileKey(partition, file)]
	if !ok {
		return 0, 0, fmt.Errorf("tape file %d in partition %d: %w", file, partition, ErrTapeFileNotFound)
	}
	return f.FirstBlock, f.LastBlock, nil
}

// SetTapeFile defines (or redefines) a tape file as the blocks firstBlock
// to lastBlock (inclusive) of a partition.
func (img *Image) SetTapeFile(partition uint8, file uint32, firstBlock, lastBlock uint64) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	img.putTapeFile(TapeFileEntry{File: file, Partition: partition, FirstBlock: firstBlock, LastBlock: lastBlock})
	return nil
}

// TapePartition returns the first and last block of a tape partition,
// or ErrTapePartitionNotFound.
func (img *Image) TapePartition(partition uint8) (firstBlock, lastBlock uint64, err error) {
	if img.closed {
		return 0, 0, ErrClosed
	}
	p, ok := img.tapePartitions[partition]
	if !ok {
		return 0, 0, fmt.Errorf("tape partition %d: %w", partition, ErrTapePartitionNotFound)
	}
	return p.FirstBlock, p.LastBlock, nil
}

// SetTapePartition defines (or redefines) a tape partition as the blocks
// firstBlock to lastBlock (inclusive).
func (img *Image) SetTapePartition(partition uint8, firstBlock, lastBlock uint64) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	img.putTapePartition(TapePartitionEntry{Number: partition, FirstBlock: firstBlock, LastBlock: lastBlock})
	return nil
}

// TapeFiles returns all tape files, in insertion order (redefined files
// last), or
// ErrTapeFileNotFound if there are none.
func (img *Image) TapeFiles() ([]TapeFileEntry, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if len(img.tapeFiles) == 0 {
		return nil, fmt.Errorf("no tape files: %w", ErrTapeFileNotFound)
	}
	out := make([]TapeFileEntry, 0, len(img.tapeFileOrder))
	for _, k := range img.tapeFileOrder {
		out = append(out, img.tapeFiles[k])
	}
	return out, nil
}

// TapePartitions returns all tape partitions,
// in insertion order, or
// ErrTapePartitionNotFound if there are none.
func (img *Image) TapePartitions() ([]TapePartitionEntry, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if len(img.tapePartitions) == 0 {
		return nil, fmt.Errorf("no tape partitions: %w", ErrTapePartitionNotFound)
	}
	out := make([]TapePartitionEntry, 0, len(img.tapePartitionOrder))
	for _, k := range img.tapePartitionOrder {
		out = append(out, img.tapePartitions[k])
	}
	return out, nil
}

// writeTapeFileBlock writes the tape file block.
func (img *Image) writeTapeFileBlock() error {
	files, err := img.TapeFiles()
	if err != nil {
		return nil
	}
	payload := marshal(files)
	h := TapeFileHeader{Identifier: BlockTypeTapeFileBlock, Entries: uint32(len(files)),
		Length: uint64(uint32(len(payload))), CRC64: crc64.Checksum(payload)}
	return img.writeTapeBlock(BlockTypeTapeFileBlock, marshal(&h), payload)
}

// writeTapePartitionBlock writes the tape partition block.
// The header count holds at most 255
// partitions; the entries are always all written.
func (img *Image) writeTapePartitionBlock() error {
	parts, err := img.TapePartitions()
	if err != nil {
		return nil
	}
	payload := marshal(parts)
	h := TapePartitionHeader{Identifier: BlockTypeTapePartitionBlock, Entries: uint8(len(parts)),
		Length: uint64(uint32(len(payload))), CRC64: crc64.Checksum(payload)}
	return img.writeTapeBlock(BlockTypeTapePartitionBlock, marshal(&h), payload)
}

func (img *Image) writeTapeBlock(bt BlockType, header, payload []byte) error {
	off := img.closeAppendOffset()
	if err := img.writeAt(append(header, payload...), off); err != nil {
		return fmt.Errorf("writing %v: %w: %w", bt, ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(bt, 0, off)
	return nil
}
