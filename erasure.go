package aaru

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/eberle1080/go-aaru/compression/flac"
	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/internal/reedsolomon"
)

// Erasure coding.
//
// Parity is computed with Reed-Solomon over GF(2^8) on the raw on-disk bytes
// of the protected blocks (block header + compressed payload, zero-padded to
// the shard size), so that a block whose compressed payload or header is
// damaged can be reconstructed byte for byte and then decoded normally.
//
// Writing (SetErasureCoding): user data blocks are grouped into stripes of K
// consecutive blocks as they are written; when a stripe is full its M parity
// blocks (DBLK, DataTypeErasureParity, uncompressed) are appended. On close,
// after the index has been written, the last partial stripe is flushed and one
// batch stripe is computed for each of the secondary DDTs, the primary DDTs,
// the metadata blocks and the index. Finally the erasure coding map (ECMB) is
// written twice, followed by the recovery footer, which holds a backup of the
// header and locates the ECMB.
//
// Reading: the ECMB is loaded on open (through the footer). User data blocks,
// secondary DDTs and metadata blocks (media tags, tracks, DPM...) that fail to
// decode are reconstructed from the surviving members of their stripe.

// Erasure coding limits.
const (
	// ecmbMaxPayload is the largest ECMB payload accepted when loading
	// (a sanity limit).
	ecmbMaxPayload = 256 << 20
	// ecMaxShards is the largest number of shards (K+M) of a stripe.
	ecMaxShards = reedsolomon.MaxShards
)

// ecGroup is a protection group loaded from the ECMB.
type ecGroup struct {
	k, m      uint16
	shardSize uint32
	stripes   []ecStripe
	lookup    map[uint64]ecShardRef // block file offset -> stripe member
	codec     *reedsolomon.Codec    // created on first use
}

// ecStripe is a stripe of a protection group.
type ecStripe struct {
	entries []StripeDataBlockEntry // the actualK data shards
	parity  []uint64               // file offsets of the M parity blocks
}

// ecShardRef locates a data block inside a protection group.
type ecShardRef struct {
	stripe uint32
	pos    uint16
}

// ---------------------------------------------------------------------------
// Configuration

// SetErasureCoding enables erasure coding for an image being written.
// Every stripe of k consecutive user data blocks
// gets m parity blocks, so that up to m damaged blocks per stripe can be
// reconstructed when reading. The DDTs, the metadata blocks and the index are
// protected with m parity blocks each, and a recovery footer holding a backup
// of the header is appended to the file.
//
// It must be called before the first sector is written: blocks written
// earlier are not protected. k and m must be at least 1 and k+m at most 255;
// ErasureCodingXor requires m == 1. Note that, for compatibility with
// existing AaruFormat images, ErasureCodingXor only changes the algorithm
// recorded in the image: the parity is always computed with the
// Reed-Solomon generator matrix (whose coefficients for m == 1 are not
// all 1).
//
// Images with erasure coding have the FeatureROCompatErasure header bit set.
func (img *Image) SetErasureCoding(algorithm ErasureCodingAlgorithm, k, m uint16) error {
	if err := img.checkWritable(); err != nil {
		return err
	}
	if k == 0 || m == 0 || int(k)+int(m) > ecMaxShards {
		return fmt.Errorf("erasure coding K=%d M=%d: %w", k, m, ErrIncorrectDataSize)
	}
	switch algorithm {
	case ErasureCodingXor:
		if m != 1 {
			return fmt.Errorf("XOR erasure coding with M=%d: %w", m, ErrIncorrectDataSize)
		}
	case ErasureCodingRsVandermonde:
	default:
		return fmt.Errorf("erasure coding algorithm %v: %w", algorithm, ErrIncorrectDataSize)
	}
	codec, err := reedsolomon.New(int(k), int(m))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIncorrectDataSize, err)
	}

	ec := &img.ec
	ec.enabled = true
	ec.algorithm = algorithm
	ec.k, ec.m = k, m
	ec.codec = codec
	ec.dataShardSize = 0 // set by the first block
	ec.parity = nil      // allocated by the first block
	ec.offsets = make([]uint64, k)
	ec.sizes = make([]uint32, k)
	ec.crcs = make([]uint64, k)
	ec.count = 0
	ec.stripeCount = 0
	ec.stripes = nil
	ec.writeErr = nil

	img.header.FeatureCompatibleRO |= FeatureROCompatErasure
	return nil
}

// SetErasureCodingAuto enables Reed-Solomon erasure coding with K and M
// derived from the desired recoverable percentage of blocks, 1 to 100:
//
//	M = clamp(round(20 * percent / 100), 2, 8)
//	K = M * 100 / percent
//
// e.g. 1% gives RS(200,2), 10% RS(20,2), 25% RS(20,5), 50% RS(16,8) and 100%
// RS(8,8). See SetErasureCoding.
func (img *Image) SetErasureCodingAuto(recoveryPercent uint8) error {
	k, m, err := ecAutoParameters(recoveryPercent)
	if err != nil {
		return err
	}
	return img.SetErasureCoding(ErasureCodingRsVandermonde, k, m)
}

// ecAutoParameters computes K and M for SetErasureCodingAuto.
func ecAutoParameters(recoveryPercent uint8) (k, m uint16, err error) {
	if recoveryPercent == 0 || recoveryPercent > 100 {
		return 0, 0, fmt.Errorf("recovery percentage %d: %w", recoveryPercent, ErrIncorrectDataSize)
	}
	p := uint32(recoveryPercent)
	m = uint16((20*p + 50) / 100)
	m = min(max(m, 2), 8)
	k = uint16(uint32(m) * 100 / p)
	k = max(k, 1)
	if int(k)+int(m) > ecMaxShards {
		k = ecMaxShards - m
	}
	return k, m, nil
}

// ---------------------------------------------------------------------------
// Write path

// ecAccumulateDataBlock adds a user data block that has just been written at
// fileOffset to the parity of the open stripe, and writes the stripe's parity
// blocks when it is full. hdr is the block header
// as written; lzmaProps (LZMA blocks only) and payload are the bytes that
// follow it on disk. It does nothing unless erasure coding is enabled.
func (img *Image) ecAccumulateDataBlock(hdr *BlockHeader, lzmaProps, payload []byte, fileOffset uint64) {
	ec := &img.ec
	if !ec.enabled {
		return
	}
	if hdr.Compression != CompressionLzma {
		lzmaProps = nil
	}

	var hb [blockHeaderSize]byte
	head := hdr.appendTo(hb[:0])
	parts := [3][]byte{head, lzmaProps, payload}
	actual := uint32(len(head) + len(lzmaProps) + len(payload))

	// Parity accumulators are allocated lazily at the first block's size plus
	// 25%, and grown (zero-extended) whenever a bigger block arrives. (The
	// size arithmetic is uint32; the buffers are never shorter than the
	// block even if it wraps.)
	if ec.parity == nil {
		ec.dataShardSize = actual + actual/4
		ec.parity = make([][]byte, ec.m)
	} else if actual > ec.dataShardSize {
		ec.dataShardSize = actual + actual/4
	}
	if need := int(max(ec.dataShardSize, actual)); len(ec.parity[0]) < need {
		for i, p := range ec.parity {
			ec.parity[i] = append(p, make([]byte, need-len(p))...)
		}
	}

	pos := ec.count
	reg := crc64.Seed
	for _, p := range parts {
		reg = crc64.Update(reg, p)
	}
	ec.offsets[pos] = fileOffset
	ec.sizes[pos] = actual
	ec.crcs[pos] = reg ^ crc64.Seed

	// Only the actual bytes contribute: the zero padding up to the shard size
	// adds nothing to the parity.
	for m := range int(ec.m) {
		coeff := ec.codec.Coefficient(m, int(pos))
		dst := ec.parity[m]
		for _, p := range parts {
			reedsolomon.EncodeIncremental(coeff, p, dst)
			dst = dst[len(p):]
		}
	}

	ec.count++
	if ec.count == ec.k {
		img.ecFlushDataStripe()
	}
}

// ecFlushDataStripe writes the M parity blocks of the open data stripe at the
// end of the file and records the stripe descriptor.
//
// It moves nextBlockPosition past the parity blocks; as
// the caller (closeCurrentBlock) then adds the size of the block just
// written to it, a block-sized gap is left after the parity blocks of every
// full stripe.
func (img *Image) ecFlushDataStripe() {
	ec := &img.ec
	ak := ec.count
	if ak == 0 {
		return
	}

	// Parity bytes past the biggest block of the stripe are zero: only that
	// many are written.
	var maxSize uint32
	for _, s := range ec.sizes[:ak] {
		maxSize = max(maxSize, s)
	}
	if maxSize == 0 {
		maxSize = ec.dataShardSize
	}

	le := binary.LittleEndian
	desc := le.AppendUint16(ec.stripes, ak)
	for k := range int(ak) {
		desc = le.AppendUint64(desc, ec.offsets[k])
		desc = le.AppendUint32(desc, ec.sizes[k])
		desc = le.AppendUint64(desc, ec.crcs[k])
	}

	shift := img.userDataDDTHeader.BlockAlignmentShift
	for m := range int(ec.m) {
		p := ec.parity[m][:maxSize]
		off, err := img.ecWriteParityBlock(DataTypeErasureParity, p, shift)
		if err != nil && ec.writeErr == nil {
			ec.writeErr = err
		}
		desc = le.AppendUint64(desc, off)
		img.indexEntries = append(img.indexEntries,
			IndexEntry{BlockType: BlockTypeDataBlock, DataType: DataTypeErasureParity, Offset: off})
		img.dirty.indexBlock = true
		img.nextBlockPosition = alignUp(off+blockHeaderSize+uint64(maxSize), shift)
		clear(ec.parity[m])
	}

	ec.stripes = desc
	ec.stripeCount++
	clear(ec.offsets)
	clear(ec.sizes)
	clear(ec.crcs)
	ec.count = 0
}

// ecWriteParityBlock writes p as an uncompressed parity data block of type dt
// at the aligned end of the file and returns its offset.
func (img *Image) ecWriteParityBlock(dt DataType, p []byte, shift uint8) (uint64, error) {
	crc := crc64.Checksum(p)
	hdr := BlockHeader{
		Identifier:  BlockTypeDataBlock,
		Type:        dt,
		Compression: CompressionNone,
		Length:      uint32(len(p)),
		CmpLength:   uint32(len(p)),
		CRC64:       crc,
		CmpCRC64:    crc,
	}
	off := alignUp(uint64(img.size), shift)
	var hb [blockHeaderSize]byte
	if err := img.writeAt(hdr.appendTo(hb[:0]), off); err != nil {
		return off, fmt.Errorf("writing parity block header at %d: %w: %w", off, ErrCannotWriteBlockHeader, err)
	}
	if err := img.writeAt(p, off+blockHeaderSize); err != nil {
		return off, fmt.Errorf("writing parity block at %d: %w: %w", off, ErrCannotWriteBlockData, err)
	}
	return off, nil
}

// ecBatch is the result of the batch parity computation of a group.
type ecBatch struct {
	group StripeGroupDescriptor
	desc  []byte // serialized stripe descriptor
}

// ecWriteBatchParity computes the parity of blocks already on disk as a single
// stripe, appends its M parity blocks of type dt and returns the group.
// Only the first 255-M blocks are protected. The
// shard CRCs cover the blocks zero-padded to the shard size (data stripes
// cover only the on-disk bytes).
func (img *Image) ecWriteBatchParity(offsets []uint64, sizes []uint32, gt ErasureCodingGroupType,
	dt DataType) (*ecBatch, error) {
	if len(offsets) == 0 {
		return nil, nil
	}
	ec := &img.ec
	M := int(ec.m)
	ak := min(len(offsets), ecMaxShards-M)

	// The sizes come from on-disk lengths; none can really exceed the file
	// (this only bounds the allocation for damaged resumed images).
	var shardSize uint32
	for _, s := range sizes[:ak] {
		shardSize = max(shardSize, uint32(min(int64(s), max(img.size, 0))))
	}
	codec, err := reedsolomon.New(ak, M)
	if err != nil {
		return nil, err
	}

	parity := make([][]byte, M)
	for m := range parity {
		parity[m] = make([]byte, shardSize)
	}
	le := binary.LittleEndian
	desc := le.AppendUint16(nil, uint16(ak))
	shard := make([]byte, shardSize)
	for k := range ak {
		clear(shard)
		// A short read keeps what could be read.
		img.readAtPartial(shard[:min(sizes[k], shardSize)], offsets[k])
		desc = le.AppendUint64(desc, offsets[k])
		desc = le.AppendUint32(desc, sizes[k])
		desc = le.AppendUint64(desc, crc64.Checksum(shard))
		for m := range M {
			reedsolomon.EncodeIncremental(codec.Coefficient(m, k), shard, parity[m])
		}
	}

	var firstErr error
	shift := img.userDataDDTHeader.BlockAlignmentShift
	for m := range M {
		off, err := img.ecWriteParityBlock(dt, parity[m], shift)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		desc = le.AppendUint64(desc, off)
		img.indexEntries = append(img.indexEntries, IndexEntry{BlockType: BlockTypeDataBlock, DataType: dt, Offset: off})
		img.dirty.indexBlock = true
	}

	return &ecBatch{
		group: StripeGroupDescriptor{GroupType: gt, K: uint16(ak), M: ec.m, ShardSize: shardSize, StripeCount: 1,
			InterleaveDepth: 1},
		desc: desc,
	}, firstErr
}

// readAtPartial reads into p at off, keeping a short read: bytes that cannot
// be read are left untouched.
func (img *Image) readAtPartial(p []byte, off uint64) {
	if len(p) == 0 || off > math.MaxInt64 || img.r == nil {
		return
	}
	_, _ = img.r.ReadAt(p, int64(off))
}

// ecCollectDDTBlocks returns the offsets and on-disk sizes (DDT header +
// compressed table) of the indexed tables of block type bt.
func (img *Image) ecCollectDDTBlocks(bt BlockType) ([]uint64, []uint32) {
	var offsets []uint64
	var sizes []uint32
	for _, e := range img.indexEntries {
		if e.BlockType != bt {
			continue
		}
		size := uint32(ddtHeader2Size)
		var hb [ddtHeader2Size]byte
		if img.readFullAt(hb[:], e.Offset) == nil {
			h, _ := unmarshal[DDTHeader2](hb[:])
			size = uint32(ddtHeader2Size + h.CmpLength)
		}
		offsets = append(offsets, e.Offset)
		sizes = append(sizes, size)
	}
	return offsets, sizes
}

// ecIsMetadataBlock reports whether an index entry belongs to the metadata
// protection group: everything but user data, parity, DDTs and indexes.
func ecIsMetadataBlock(e IndexEntry) bool {
	switch e.BlockType {
	case BlockTypeDataBlock:
		return e.DataType != DataTypeUserData &&
			(e.DataType < DataTypeErasureParity || e.DataType > DataTypeErasureParityIndex)
	case BlockTypeDeDuplicationTable2, BlockTypeDeDuplicationTableSecondary, BlockTypeDeDuplicationTable,
		BlockTypeIndexBlock, BlockTypeIndexBlock2, BlockTypeIndexBlock3:
		return false
	}
	return true
}

// ecCollectMetadataBlocks returns the offsets and on-disk sizes of the
// metadata group blocks. Data blocks are sized from their header; the other
// block types (whose headers have different layouts) extend to the next
// indexed block, or to the index.
func (img *Image) ecCollectMetadataBlocks() ([]uint64, []uint32) {
	var offsets []uint64
	var sizes []uint32
	for _, e := range img.indexEntries {
		if !ecIsMetadataBlock(e) {
			continue
		}
		var size uint32
		if e.BlockType == BlockTypeDataBlock {
			size = blockHeaderSize
			if bh, err := img.readBlockHeaderAt(e.Offset); err == nil {
				size = blockHeaderSize + bh.CmpLength
			}
		} else {
			next := uint64(math.MaxUint64)
			for _, o := range img.indexEntries {
				if o.Offset > e.Offset && o.Offset < next {
					next = o.Offset
				}
			}
			switch {
			case next != math.MaxUint64:
				size = uint32(next - e.Offset)
			case img.header.IndexOffset > e.Offset:
				size = uint32(img.header.IndexOffset - e.Offset)
			default:
				size = 4096
			}
		}
		offsets = append(offsets, e.Offset)
		sizes = append(sizes, size)
	}
	return offsets, sizes
}

// ecFinalize flushes the last data stripe, writes the batch parity of the
// DDT, metadata and index groups, the ECMB (twice) and the recovery footer.
// It runs after the index has been written, so the parity
// blocks it writes are not indexed. It does nothing unless erasure coding is
// enabled.
func (img *Image) ecFinalize() error {
	ec := &img.ec
	if !ec.enabled {
		return nil
	}
	firstErr := ec.writeErr
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Group 0: user data.
	if ec.count > 0 {
		img.ecFlushDataStripe()
		note(ec.writeErr)
	}
	dataGroup := StripeGroupDescriptor{GroupType: ECGroupData, K: ec.k, M: ec.m, ShardSize: ec.dataShardSize,
		StripeCount: ec.stripeCount, InterleaveDepth: ec.k}

	// Group 1: secondary DDTs; group 2: primary DDTs.
	offs, sizes := img.ecCollectDDTBlocks(BlockTypeDeDuplicationTableSecondary)
	ddtSec, err := img.ecWriteBatchParity(offs, sizes, ECGroupDdtSecondary, DataTypeErasureParityDdt)
	note(err)
	offs, sizes = img.ecCollectDDTBlocks(BlockTypeDeDuplicationTable2)
	ddtPri, err := img.ecWriteBatchParity(offs, sizes, ECGroupDdtPrimary, DataTypeErasureParityDdtPrimary)
	note(err)

	// Group 3: metadata, only if it fits in a single stripe.
	var meta *ecBatch
	offs, sizes = img.ecCollectMetadataBlocks()
	if len(offs) > 0 && len(offs) <= ecMaxShards-int(ec.m) {
		meta, err = img.ecWriteBatchParity(offs, sizes, ECGroupMetadata, DataTypeErasureParityMeta)
		note(err)
	}

	// Group 4: the index.
	var index *ecBatch
	if idxOff := img.header.IndexOffset; idxOff > 0 {
		var hb [indexHeader3Size]byte
		if img.readFullAt(hb[:], idxOff) == nil {
			ih, _ := unmarshal[IndexHeader3](hb[:])
			size := uint32(indexHeader3Size + ih.Entries*indexEntrySize)
			index, err = img.ecWriteBatchParity([]uint64{idxOff}, []uint32{size}, ECGroupIndex, DataTypeErasureParityIndex)
			note(err)
		}
	}

	// The ECMB: the data group followed by the batch groups.
	payload := appendMarshal(nil, &dataGroup)
	payload = append(payload, ec.stripes...)
	groups := uint8(1)
	for _, b := range []*ecBatch{ddtSec, ddtPri, meta, index} {
		if b == nil {
			continue
		}
		payload = appendMarshal(payload, &b.group)
		payload = append(payload, b.desc...)
		groups++
	}
	crc := crc64.Checksum(payload)
	mh := ErasureCodingMapHeader{
		Identifier:       BlockTypeErasureCodingMapBlock,
		Algorithm:        ec.algorithm,
		StripeGroupCount: groups,
		Compression:      CompressionNone,
		CmpLength:        uint64(len(payload)),
		Length:           uint64(len(payload)),
		CmpCRC64:         crc,
		CRC64:            crc,
	}
	block := appendMarshal(make([]byte, 0, ecMapHeaderSize+len(payload)), &mh)
	block = append(block, payload...)

	shift := img.userDataDDTHeader.BlockAlignmentShift
	ecmbOffset := alignUp(uint64(img.size), shift)
	if err := img.writeAt(block, ecmbOffset); err != nil {
		note(fmt.Errorf("writing ECMB at %d: %w: %w", ecmbOffset, ErrCannotWriteBlockData, err))
	}
	// A duplicate follows.
	dupOffset := alignUp(ecmbOffset+uint64(len(block)), shift)
	if err := img.writeAt(block, dupOffset); err != nil {
		note(fmt.Errorf("writing ECMB copy at %d: %w: %w", dupOffset, ErrCannotWriteBlockData, err))
	}

	// The recovery footer, at the very end of the file.
	hdr := marshal(&img.header)
	footer := RecoveryFooter{
		ECMBOffset:   ecmbOffset,
		ECMBLength:   uint64(len(block)),
		HeaderCRC64:  crc64.Checksum(hdr),
		BackupHeader: img.header,
		FooterMagic:  RecoveryFooterMagic,
	}
	if err := img.writeAt(marshal(&footer), uint64(img.size)); err != nil {
		note(fmt.Errorf("writing recovery footer: %w: %w", ErrCannotWriteHeader, err))
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// Loading the ECMB

// ecLoadECMB loads the erasure coding map through the recovery footer at the
// end of the file, if there is one. It is never fatal: images
// without (valid) erasure coding data simply cannot be recovered. The
// duplicate ECMB is used when the primary one is damaged.
func (img *Image) ecLoadECMB() {
	img.ec.recoveryAvailable = false
	img.ec.data, img.ec.ddt, img.ec.meta = nil, nil, nil
	if img.size < recoveryFooterSize {
		return
	}
	var fb [recoveryFooterSize]byte
	if err := img.readFullAt(fb[:], uint64(img.size)-recoveryFooterSize); err != nil {
		return
	}
	footer, err := unmarshal[RecoveryFooter](fb[:])
	if err != nil || footer.FooterMagic != RecoveryFooterMagic {
		return
	}

	mh, payload, err := img.readECMB(footer.ECMBOffset)
	if err != nil {
		// The duplicate ECMB sits right before the footer.
		end := uint64(img.size) - recoveryFooterSize
		if footer.ECMBLength < ecMapHeaderSize || footer.ECMBLength > end {
			return
		}
		dup := end - footer.ECMBLength
		if dup == footer.ECMBOffset {
			return
		}
		if mh, payload, err = img.readECMB(dup); err != nil {
			return
		}
	}

	groups := parseECMBPayload(payload, int(mh.StripeGroupCount))
	for _, g := range groups {
		switch g.desc.GroupType {
		case ECGroupData:
			img.ec.data = g.group
		case ECGroupDdtSecondary:
			img.ec.ddt = g.group
		case ECGroupMetadata:
			img.ec.meta = g.group
		}
	}
	img.ec.recoveryAvailable = (img.ec.data != nil && len(img.ec.data.stripes) > 0) ||
		(img.ec.meta != nil && len(img.ec.meta.stripes) > 0)
}

// readECMB reads, decompresses and verifies the ECMB at off, returning its
// header and payload.
func (img *Image) readECMB(off uint64) (ErasureCodingMapHeader, []byte, error) {
	var hb [ecMapHeaderSize]byte
	if err := img.readFullAt(hb[:], off); err != nil {
		return ErasureCodingMapHeader{}, nil, err
	}
	h, _ := unmarshal[ErasureCodingMapHeader](hb[:])
	if h.Identifier != BlockTypeErasureCodingMapBlock {
		return h, nil, fmt.Errorf("ECMB identifier %#x: %w", uint32(h.Identifier), ErrCannotReadBlock)
	}
	if h.Length == 0 || h.Length > ecmbMaxPayload {
		return h, nil, fmt.Errorf("ECMB payload of %d bytes: %w", h.Length, ErrCannotReadBlock)
	}
	if h.Compression == CompressionNone && h.CmpLength != h.Length {
		return h, nil, fmt.Errorf("uncompressed ECMB lengths %d/%d: %w", h.CmpLength, h.Length, ErrCannotReadBlock)
	}
	raw, err := img.readAt(off+ecMapHeaderSize, h.CmpLength)
	if err != nil {
		return h, nil, err
	}
	var payload []byte
	switch h.Compression {
	case CompressionNone, CompressionLzma, CompressionZstd:
		if payload, err = decodePayload(h.Compression, DataTypeNone, raw, h.Length); err != nil {
			return h, nil, err
		}
	default:
		return h, nil, fmt.Errorf("ECMB compression %v: %w", h.Compression, ErrUnsupportedCompression)
	}
	if uint64(len(payload)) != h.Length || crc64.Checksum(payload) != h.CRC64 {
		return h, nil, fmt.Errorf("ECMB payload: %w", ErrInvalidBlockCRC)
	}
	return h, payload, nil
}

// parsedGroup is a protection group parsed from the ECMB payload.
type parsedGroup struct {
	desc  StripeGroupDescriptor
	group *ecGroup
}

// parseECMBPayload parses the (at most count) stripe groups of an ECMB
// payload. Parsing stops at the first group that does not fit in the
// payload.
func parseECMBPayload(p []byte, count int) []parsedGroup {
	le := binary.LittleEndian
	var out []parsedGroup
	for len(out) < count && len(p) >= stripeGroupDescriptorSize {
		d, _ := unmarshal[StripeGroupDescriptor](p)
		p = p[stripeGroupDescriptorSize:]
		// Every stripe takes at least its actualK field and M parity offsets.
		minStripe := 2 + int(d.M)*stripeParityBlockEntrySize
		if uint64(d.StripeCount)*uint64(minStripe) > uint64(len(p)) {
			return out
		}
		g := &ecGroup{k: d.K, m: d.M, shardSize: d.ShardSize, stripes: make([]ecStripe, d.StripeCount),
			lookup: make(map[uint64]ecShardRef)}
		for s := range g.stripes {
			if len(p) < 2 {
				return out
			}
			ak := int(le.Uint16(p))
			p = p[2:]
			n := ak*stripeDataBlockEntrySize + int(d.M)*stripeParityBlockEntrySize
			if len(p) < n {
				return out
			}
			st := &g.stripes[s]
			st.entries = make([]StripeDataBlockEntry, ak)
			for k := range st.entries {
				st.entries[k] = StripeDataBlockEntry{Offset: le.Uint64(p), OnDiskSize: le.Uint32(p[8:]),
					ShardCRC64: le.Uint64(p[12:])}
				g.lookup[st.entries[k].Offset] = ecShardRef{stripe: uint32(s), pos: uint16(k)}
				p = p[stripeDataBlockEntrySize:]
			}
			st.parity = make([]uint64, d.M)
			for m := range st.parity {
				st.parity[m] = le.Uint64(p)
				p = p[stripeParityBlockEntrySize:]
			}
		}
		out = append(out, parsedGroup{desc: d, group: g})
	}
	return out
}

// ---------------------------------------------------------------------------
// Recovery

// errECUnrecoverable reports that a block could not be reconstructed.
var errECUnrecoverable = errors.New("erasure coding cannot reconstruct the block")

// ecRecoveryAvailable reports whether recovery data was loaded and no
// recovery is in progress.
func (img *Image) ecRecoveryAvailable() bool {
	return img.ec.recoveryAvailable && !img.ec.recoveryInProgress
}

// rsCodec returns the Reed-Solomon codec of the group.
func (g *ecGroup) rsCodec() (*reedsolomon.Codec, error) {
	if g.codec == nil {
		c, err := reedsolomon.New(int(g.k), int(g.m))
		if err != nil {
			return nil, err
		}
		g.codec = c
	}
	return g.codec, nil
}

// ecRecoverShard reconstructs the on-disk bytes (onDiskSize of them) of the
// block at off, a member of group g.
//
// The surviving data shards are those whose CRC64 matches the ECMB.
// Parity blocks are used only when their own CRC64 matches, and the
// reconstructed block is verified against its shard CRC64. Shard CRCs are
// accepted both over the on-disk bytes (data stripes) and over the bytes
// zero-padded to the shard size (what is written for batch stripes).
func (img *Image) ecRecoverShard(g *ecGroup, off uint64) ([]byte, error) {
	if g == nil || !img.ecRecoveryAvailable() {
		return nil, errECUnrecoverable
	}
	ref, ok := g.lookup[off]
	if !ok || int(ref.stripe) >= len(g.stripes) {
		return nil, fmt.Errorf("block at %d is not protected: %w", off, errECUnrecoverable)
	}
	img.ec.recoveryInProgress = true
	defer func() { img.ec.recoveryInProgress = false }()

	codec, err := g.rsCodec()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errECUnrecoverable, err)
	}
	st := &g.stripes[ref.stripe]
	K, M := int(g.k), int(g.m)
	if len(st.entries) > K || int(ref.pos) >= len(st.entries) || len(st.parity) != M {
		return nil, fmt.Errorf("malformed stripe: %w", errECUnrecoverable)
	}
	target := st.entries[ref.pos]
	if target.OnDiskSize > g.shardSize {
		return nil, fmt.Errorf("block of %d bytes in %d-byte shards: %w", target.OnDiskSize, g.shardSize,
			errECUnrecoverable)
	}

	// Every byte past the biggest block of the stripe is zero in all shards,
	// so the RS math only needs that many.
	var work uint32
	for _, e := range st.entries {
		work = max(work, min(e.OnDiskSize, g.shardSize))
	}
	if work == 0 || int64(work) > img.size {
		return nil, fmt.Errorf("shard size %d: %w", work, errECUnrecoverable)
	}

	shards := make([][]byte, K+M)
	present := make([]bool, K+M)
	for k, e := range st.entries {
		buf := make([]byte, work)
		shards[k] = buf
		if e.OnDiskSize > g.shardSize || img.readFullAt(buf[:e.OnDiskSize], e.Offset) != nil {
			continue
		}
		present[k] = shardCRCMatches(buf, e.OnDiskSize, g.shardSize, e.ShardCRC64)
	}
	// Positions past actualK (partial stripe) are all-zero shards.
	if len(st.entries) < K {
		zero := make([]byte, work)
		for k := len(st.entries); k < K; k++ {
			shards[k] = zero
			present[k] = true
		}
	}
	for m, po := range st.parity {
		shards[K+m], present[K+m] = img.ecReadParity(po, work)
		if shards[K+m] == nil {
			shards[K+m] = make([]byte, work)
		}
	}

	if err := codec.Decode(shards, present); err != nil {
		return nil, fmt.Errorf("%w: %w", errECUnrecoverable, err)
	}
	rec := shards[ref.pos][:target.OnDiskSize]
	if !shardCRCMatches(shards[ref.pos], target.OnDiskSize, g.shardSize, target.ShardCRC64) {
		return nil, fmt.Errorf("reconstructed block at %d does not match its CRC: %w", off, errECUnrecoverable)
	}
	return rec, nil
}

// shardCRCMatches reports whether want is the CRC64 of the first size bytes
// of buf, or of buf zero-padded to shardSize bytes. buf holds the shard
// (zero beyond size); it may be shorter than shardSize.
func shardCRCMatches(buf []byte, size, shardSize uint32, want uint64) bool {
	if crc64.Checksum(buf[:size]) == want {
		return true
	}
	if shardSize <= size {
		return false
	}
	n := min(uint32(len(buf)), shardSize)
	reg := crc64.Update(crc64.Seed, buf[:n])
	var zeros [4096]byte
	for rest := shardSize - n; rest > 0; {
		c := min(rest, uint32(len(zeros)))
		reg = crc64.Update(reg, zeros[:c])
		rest -= c
	}
	return reg^crc64.Seed == want
}

// ecReadParity reads the parity block at off into a shard of work bytes. It
// reports false if the block cannot be read or fails its CRC.
func (img *Image) ecReadParity(off uint64, work uint32) ([]byte, bool) {
	hdr, err := img.readBlockHeaderAt(off)
	if err != nil || hdr.Identifier != BlockTypeDataBlock {
		return nil, false
	}
	raw, err := img.readAt(off+blockHeaderSize, uint64(hdr.CmpLength))
	if err != nil {
		return nil, false
	}
	var data []byte
	switch hdr.Compression {
	case CompressionNone, CompressionLzma, CompressionZstd:
		if hdr.Compression != CompressionNone && !plausibleLength(uint64(len(raw)), uint64(hdr.Length)) {
			return nil, false
		}
		if data, err = decodePayload(hdr.Compression, hdr.Type, raw, uint64(hdr.Length)); err != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	if crc64.Checksum(data) != hdr.CRC64 {
		return nil, false
	}
	shard := make([]byte, work)
	copy(shard, data)
	return shard, true
}

// ecRecoverMetaBlock reconstructs the raw on-disk bytes (header + payload) of
// the metadata group block at offset: media tags and
// other data blocks, tracks, DPM, checksums and the like.
func (img *Image) ecRecoverMetaBlock(offset uint64) ([]byte, error) {
	b, err := img.ecRecoverShard(img.ec.meta, offset)
	if err != nil {
		return nil, fmt.Errorf("recovering block at %d: %w: %w", offset, ErrCannotReadBlock, err)
	}
	return b, nil
}

// ecRecoverDDTBlock reconstructs the raw on-disk bytes (header + payload) of
// the secondary DDT at offset.
func (img *Image) ecRecoverDDTBlock(offset uint64) ([]byte, error) {
	b, err := img.ecRecoverShard(img.ec.ddt, offset)
	if err != nil {
		return nil, fmt.Errorf("recovering secondary DDT at %d: %w: %w", offset, ErrCannotReadBlock, err)
	}
	return b, nil
}

// ecRecoverUserDataBlock reconstructs the user data block at blockOffset and
// returns its header and decompressed contents, verified against the header
// CRC64.
func (img *Image) ecRecoverUserDataBlock(blockOffset uint64) (BlockHeader, []byte, error) {
	raw, err := img.ecRecoverShard(img.ec.data, blockOffset)
	if err != nil {
		return BlockHeader{}, nil, err
	}
	hdr, err := decodeBlockHeader(raw)
	if err != nil {
		return BlockHeader{}, nil, fmt.Errorf("%w: %w", errECUnrecoverable, err)
	}
	payload := raw[blockHeaderSize:]
	if uint64(hdr.CmpLength) > uint64(len(payload)) {
		return hdr, nil, fmt.Errorf("recovered block lengths: %w", errECUnrecoverable)
	}
	payload = payload[:hdr.CmpLength]
	length := uint64(hdr.Length)
	var block []byte
	switch hdr.Compression {
	case CompressionNone:
		if length > uint64(len(payload)) {
			return hdr, nil, fmt.Errorf("recovered block lengths: %w", errECUnrecoverable)
		}
		block = payload[:length:length]
	case CompressionLzma, CompressionZstd:
		if !plausibleLength(uint64(len(payload)), length) {
			return hdr, nil, fmt.Errorf("recovered block lengths: %w", errECUnrecoverable)
		}
		if block, err = decodePayload(hdr.Compression, hdr.Type, payload, length); err != nil {
			return hdr, nil, fmt.Errorf("%w: %w", errECUnrecoverable, err)
		}
	case CompressionFlac:
		if length > maxBlockPayload || !plausibleLength(uint64(len(payload)), length) {
			return hdr, nil, fmt.Errorf("recovered block lengths: %w", errECUnrecoverable)
		}
		block = make([]byte, length)
		if n, err := flac.DecodeRedbookInto(block, payload); err != nil || uint64(n) != length {
			return hdr, nil, fmt.Errorf("%w: FLAC", errECUnrecoverable)
		}
	default:
		return hdr, nil, fmt.Errorf("recovered block compression %v: %w", hdr.Compression, errECUnrecoverable)
	}
	if crc64.Checksum(block) != hdr.CRC64 {
		return hdr, nil, fmt.Errorf("recovered block: %w", ErrInvalidBlockCRC)
	}
	return hdr, block, nil
}

// ecRecoverDataBlock reconstructs the user data block at blockOffset, caches
// it (so the other sectors of the block are read normally) and returns a copy
// of its sector at index offset. status is the
// sector status from the DDT (unused).
func (img *Image) ecRecoverDataBlock(blockOffset, offset uint64, status SectorStatus) ([]byte, error) {
	_ = status
	hdr, block, err := img.ecRecoverUserDataBlock(blockOffset)
	if err != nil {
		return nil, fmt.Errorf("recovering data block at %d: %w: %w", blockOffset, ErrCannotDecompressBlock, err)
	}
	size := uint64(hdr.SectorSize)
	if size == 0 || offset >= uint64(len(block))/size {
		return nil, fmt.Errorf("sector %d of recovered block at %d: %w", offset, blockOffset, ErrCannotDecompressBlock)
	}
	img.blockCache.add(blockOffset, block, uint64(len(block)))
	img.blockHeaderCache.add(blockOffset, hdr, blockHeaderSize)
	return append([]byte(nil), block[offset*size:(offset+1)*size]...), nil
}

// ecCheckDataBlock verifies a freshly decompressed user data block against
// its header CRC64 when the image has erasure coding, and reconstructs it if
// it does not match (damage that decompression did not detect, e.g. in an
// uncompressed block). When the block cannot be recovered it is returned as
// is, unverified. *hdr is replaced with the
// recovered header.
func (img *Image) ecCheckDataBlock(off uint64, hdr *BlockHeader, block []byte) []byte {
	if !img.ecRecoveryAvailable() || img.crcMatches(crc64.Checksum(block), hdr.CRC64) {
		return block
	}
	rh, rb, err := img.ecRecoverUserDataBlock(off)
	if err != nil || rh.SectorSize == 0 || rh.SectorSize > maxDataBlockSectorSize {
		return block
	}
	*hdr = rh
	img.blockHeaderCache.add(off, rh, blockHeaderSize)
	return rb
}
