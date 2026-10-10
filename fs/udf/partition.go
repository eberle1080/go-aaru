package udf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

// addr is a logical block: a block of the partition with internal map
// index part (an index into Volume.parts).
type addr struct {
	part  int
	block uint32
}

// partDesc is a partition descriptor (ECMA-167 3/10.5).
type partDesc struct {
	number uint16
	flags  uint16
	access uint32
	start  uint32
	length uint32
	vdsn   uint32
}

// partMap is one partition map of the logical volume, ready for address
// translation.
type partMap struct {
	kind   string
	ident  string
	number uint16
	volSeq uint16
	err    error // why the map is unusable
	rawMap []byte

	// physical and sparable
	pd        *partDesc
	packetLen uint32
	sparing   map[uint32]uint32 // packet start (partition-relative) -> absolute block
	warnedEnd bool

	// virtual and metadata: the map their blocks are recorded in
	phys int
	// virtual
	vat []uint32
	// metadata: the metadata file's and its mirror's layout
	md, mirror                     *content
	mdFile, mirrorFile, bitmapFile uint32
}

// parsePartitionMaps decodes the logical volume descriptor's partition map
// table.
func (v *Volume) parsePartitionMaps(table []byte, n uint32) {
	pos := 0
	for i := uint32(0); i < n; i++ {
		if i >= 64 {
			v.warn("", "more than 64 partition maps; ignoring the rest")
			break
		}
		if pos+2 > len(table) {
			v.warn("", "partition map table ends after %d of %d maps", i, n)
			break
		}
		typ, l := table[pos], int(table[pos+1])
		if l < 2 || pos+l > len(table) {
			v.warn("", "partition map %d has invalid length %d", i, l)
			break
		}
		b := table[pos : pos+l]
		pos += l
		p := &partMap{kind: PartitionUnknown, phys: -1, rawMap: b}
		switch {
		case typ == 1 && l >= 6:
			p.kind = PartitionPhysical
			p.volSeq, p.number = le16(b[2:]), le16(b[4:])
		case typ == 2 && l >= 40:
			id := parseRegid(b[4:36])
			p.ident = id.id
			p.volSeq, p.number = le16(b[36:]), le16(b[38:])
			switch id.id {
			case "*UDF Virtual Partition":
				p.kind = PartitionVirtual
			case "*UDF Sparable Partition":
				p.kind = PartitionSparable
			case "*UDF Metadata Partition":
				p.kind = PartitionMetadata
			default:
				p.err = fmt.Errorf("%w: unsupported partition map %q", ErrPartition, id.id)
				v.warn("", "partition map %d: unsupported type %q", i, id.id)
			}
			if p.kind == PartitionSparable && l < 48 || p.kind == PartitionMetadata && l < 59 {
				p.kind = PartitionUnknown
				p.err = fmt.Errorf("%w: partition map %d is truncated", ErrCorrupt, i)
				v.warn("", "partition map %d (%s) is truncated", i, id.id)
			}
			switch p.kind {
			case PartitionSparable:
				p.packetLen = uint32(le16(b[40:]))
			case PartitionMetadata:
				p.mdFile, p.mirrorFile, p.bitmapFile = le32(b[40:]), le32(b[44:]), le32(b[48:])
			}
		default:
			p.err = fmt.Errorf("%w: partition map %d has type %d", ErrPartition, i, typ)
			v.warn("", "partition map %d: unknown type %d", i, typ)
		}
		v.parts = append(v.parts, p)
	}
	v.nmaps = len(v.parts)
}

// setupPartitions binds every map to its partition descriptor and reads the
// sparing tables, metadata files and virtual allocation table.
func (v *Volume) setupPartitions(pds map[uint16]*partDesc) {
	for i, p := range v.parts {
		if p.err != nil || p.kind == PartitionVirtual || p.kind == PartitionMetadata {
			continue
		}
		p.pd = pds[p.number]
		if p.pd == nil {
			p.err = fmt.Errorf("%w: no partition descriptor for partition %d", ErrPartition, p.number)
			v.warn("", "partition map %d: no partition descriptor for partition %d", i, p.number)
			continue
		}
		if p.kind == PartitionSparable {
			v.readSparing(i, p)
		}
	}
	// Virtual and metadata partitions live in a physical (or sparable)
	// partition with the same number.
	for i, p := range v.parts[:v.nmaps] {
		if p.err != nil || (p.kind != PartitionVirtual && p.kind != PartitionMetadata) {
			continue
		}
		p.phys = -1
		for j, q := range v.parts[:v.nmaps] {
			if j != i && q.err == nil && q.number == p.number && (q.kind == PartitionPhysical || q.kind == PartitionSparable) {
				p.phys = j
				break
			}
		}
		if p.phys < 0 {
			pd := pds[p.number]
			if pd == nil {
				p.err = fmt.Errorf("%w: no partition descriptor for partition %d", ErrPartition, p.number)
				v.warn("", "partition map %d: no partition descriptor for partition %d", i, p.number)
				continue
			}
			v.warn("", "partition map %d (%s) has no physical map for partition %d; using its descriptor", i, p.kind, p.number)
			v.parts = append(v.parts, &partMap{kind: PartitionPhysical, number: p.number, pd: pd, phys: -1})
			p.phys = len(v.parts) - 1
		}
	}
	for i, p := range v.parts[:v.nmaps] {
		if p.err == nil && p.kind == PartitionMetadata {
			v.readMetadataFiles(i, p)
		}
	}
	for i, p := range v.parts[:v.nmaps] {
		if p.err == nil && p.kind == PartitionVirtual {
			v.vat = true
			v.readVAT(i, p)
		}
	}
}

// readSparing reads a sparable partition's sparing tables (UDF 2.2.12) and
// uses the first valid one.
func (v *Volume) readSparing(i int, p *partMap) {
	b := p.rawMap
	if p.packetLen == 0 {
		v.warn("", "sparable partition map %d has packet length 0; sparing ignored", i)
		return
	}
	n := int(b[42])
	tableSize := le32(b[44:])
	if n > 4 || 48+4*n > len(b) {
		k := min(4, (len(b)-48)/4)
		v.warn("", "sparable partition map %d lists %d sparing tables; reading %d", i, n, k)
		n = k
	}
	size := int64(tableSize)
	if size < 56 || size > 1<<20 {
		v.warn("", "sparable partition map %d: sparing table size %d is invalid", i, tableSize)
		size = min(max(size, 56), 1<<20)
	}
	size = (size + v.bs - 1) / v.bs * v.bs
	var used []byte
	for k := range n {
		loc := le32(b[48+4*k:])
		buf := make([]byte, size)
		if err := v.readAt(buf, int64(loc)*v.bs); err != nil {
			// A table at the end of a short image may be partial.
			buf = buf[:v.bs]
			if err := v.readAt(buf, int64(loc)*v.bs); err != nil {
				v.warn("", "sparing table %d at block %d: %v", k, loc, err)
				continue
			}
		}
		if le16(buf) != tagSparingTable || parseRegid(buf[16:48]).id != "*UDF Sparing Table" {
			v.warn("", "sparing table %d at block %d is not a sparing table", k, loc)
			continue
		}
		if used != nil {
			if len(buf) == len(used) && !bytes.Equal(buf[16:], used[16:]) {
				v.warn("", "sparing table %d at block %d differs from the one in use", k, loc)
			}
			continue
		}
		_ = v.checkTag("", "sparing table", buf, tagSparingTable, loc)
		used = buf
		rtl := int(le16(buf[48:]))
		if 56+8*rtl > len(buf) {
			v.warn("", "sparing table at block %d: %d entries do not fit; reading %d", loc, rtl, (len(buf)-56)/8)
			rtl = (len(buf) - 56) / 8
		}
		p.sparing = map[uint32]uint32{}
		for e := range rtl {
			orig, mapped := le32(buf[56+8*e:]), le32(buf[60+8*e:])
			if orig >= 0xFFFFFFF0 {
				continue // available or defective spare
			}
			if orig%p.packetLen != 0 {
				v.warn("", "sparing table entry for block %d is not packet aligned", orig)
				continue
			}
			p.sparing[orig] = mapped
		}
	}
	if used == nil {
		v.warn("", "sparable partition map %d: no valid sparing table; reading without remapping", i)
	}
}

// readMetadataFiles reads the metadata file and its mirror (UDF 2.2.13).
// When the metadata file is unreadable the mirror is used instead, and when
// both are readable, blocks that cannot be read through the metadata file
// are read through the mirror (see readPart).
func (v *Volume) readMetadataFiles(i int, p *partMap) {
	load := func(loc uint32, want byte, what string) *content {
		fe, err := v.readFE("", addr{p.phys, loc})
		if err != nil {
			v.warn("", "%s at block %d: %v", what, loc, err)
			return nil
		}
		if fe.fileType != want {
			v.warn("", "%s at block %d has file type %d, not %d", what, loc, fe.fileType, want)
		}
		c, err := v.contentOf("", fe)
		if err != nil {
			v.warn("", "%s at block %d: %v", what, loc, err)
			return nil
		}
		if c.embedded {
			v.warn("", "%s at block %d has embedded data", what, loc)
			return nil
		}
		return c
	}
	p.md = load(p.mdFile, FileTypeMetadata, "metadata file")
	if p.mirrorFile != 0xFFFFFFFF && p.mirrorFile != p.mdFile {
		p.mirror = load(p.mirrorFile, FileTypeMetadataMirror, "metadata mirror file")
	}
	switch {
	case p.md == nil && p.mirror == nil:
		p.err = fmt.Errorf("%w: neither the metadata file nor its mirror is readable", ErrCorrupt)
		v.warn("", "metadata partition map %d unusable: %v", i, p.err)
	case p.md == nil:
		v.warn("", "metadata file unreadable; using the metadata mirror file")
		p.md, p.mirror = p.mirror, nil
	}
}

// readVAT locates and reads the virtual allocation table of a virtual
// partition (UDF 2.2.11): its file entry is in the last recorded block of
// the session, searched backwards from LastBlock.
func (v *Volume) readVAT(i int, p *partMap) {
	if !v.haveLast {
		p.err = fmt.Errorf("%w: the last block is unknown, so the virtual allocation table cannot be found", ErrPartition)
		v.warn("", "virtual partition map %d: %v", i, p.err)
		return
	}
	start := v.parts[p.phys].pd.start
	var lastErr error
	for k := 0; k < v.opts.MaxVATSearch; k++ {
		if uint64(k) > uint64(v.lastBlock) || v.lastBlock-uint32(k) < start {
			break
		}
		abs := v.lastBlock - uint32(k)
		b, err := v.readBlock(uint64(abs))
		if err != nil {
			lastErr = err
			continue
		}
		id := le16(b)
		if (id != tagFE && id != tagEFE) || tagChecksum(b) != b[4] {
			continue
		}
		ft := b[16+11]
		if ft != FileTypeVAT && ft != FileTypeUnspecified {
			continue
		}
		vat, err := v.parseVAT(addr{p.phys, abs - start}, ft)
		if err != nil {
			lastErr = err
			continue
		}
		p.vat = vat
		if k > 0 {
			v.warn("", "virtual allocation table found %d blocks before the last block", k)
		}
		return
	}
	if lastErr == nil {
		lastErr = errors.New("not found")
	}
	p.err = fmt.Errorf("%w: virtual allocation table: %v", ErrPartition, lastErr)
	v.warn("", "virtual partition map %d unusable: %v", i, p.err)
}

// vatHeader is the header of a UDF 2.00+ virtual allocation table.
type vatHeader struct {
	lvid                        string
	files, dirs                 uint32
	minRead, minWrite, maxWrite uint16
}

// parseVAT reads the VAT whose file entry is at a, of file type ft (248 for
// UDF 2.00+, 0 for the UDF 1.50 table with its trailing identifier).
func (v *Volume) parseVAT(a addr, ft byte) ([]uint32, error) {
	fe, err := v.readFE("", a)
	if err != nil {
		return nil, err
	}
	c, err := v.contentOf("", fe)
	if err != nil {
		return nil, err
	}
	if c.size > 256<<20 || c.size > v.size {
		return nil, fmt.Errorf("%w: virtual allocation table of %d bytes", ErrCorrupt, c.size)
	}
	data := make([]byte, c.size)
	if _, err := v.readContent(c, data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var entries []byte
	if ft == FileTypeVAT {
		if len(data) < 152 {
			return nil, fmt.Errorf("%w: virtual allocation table header truncated", ErrCorrupt)
		}
		lhd, liu := int(le16(data)), int(le16(data[2:]))
		if lhd < 152 || lhd > len(data) {
			return nil, fmt.Errorf("%w: virtual allocation table header length %d", ErrCorrupt, lhd)
		}
		if lhd != 152+liu {
			v.warn("", "virtual allocation table header length %d with %d bytes of implementation use", lhd, liu)
		}
		id, msg := dstring(data[4:132])
		if msg != "" {
			v.warn("", "VAT logical volume identifier: %s", msg)
		}
		v.vatInfo = &vatHeader{lvid: id, files: le32(data[136:]), dirs: le32(data[140:]),
			minRead: le16(data[144:]), minWrite: le16(data[146:]), maxWrite: le16(data[148:])}
		entries = data[lhd:]
	} else {
		if len(data) < 36 || parseRegid(data[len(data)-36:]).id != "*UDF Virtual Alloc Tbl" {
			return nil, fmt.Errorf("%w: no UDF 1.50 virtual allocation table identifier", ErrCorrupt)
		}
		entries = data[:len(data)-36]
	}
	vat := make([]uint32, len(entries)/4)
	for k := range vat {
		vat[k] = le32(entries[4*k:])
	}
	if len(entries)%4 != 0 {
		v.warn("", "virtual allocation table has %d trailing bytes", len(entries)%4)
	}
	return vat, nil
}

const maxRun = 1 << 16 // blocks resolved at once in a contiguous partition

// resolve translates logical block (part, block) to an absolute block and
// the number of blocks from there that are contiguous on the medium. For a
// metadata partition, mirror selects the metadata mirror file.
func (v *Volume) resolve(part int, block uint32, mirror bool, depth int) (uint64, uint32, error) {
	if part < 0 || part >= len(v.parts) {
		return 0, 0, fmt.Errorf("%w: partition reference %d", ErrPartition, part)
	}
	if depth > 4 {
		return 0, 0, fmt.Errorf("%w: partition translation nested too deep", ErrLoop)
	}
	p := v.parts[part]
	if p.err != nil {
		return 0, 0, p.err
	}
	switch p.kind {
	case PartitionPhysical, PartitionSparable:
		run := uint32(maxRun)
		if p.sparing != nil {
			pkt := block / p.packetLen * p.packetLen
			run = p.packetLen - (block - pkt)
			if m, ok := p.sparing[pkt]; ok {
				return uint64(m) + uint64(block-pkt), run, nil
			}
		}
		if block >= p.pd.length && !p.warnedEnd {
			p.warnedEnd = true
			v.warn("", "partition %d: block %d is beyond its length of %d blocks", p.number, block, p.pd.length)
		}
		return uint64(p.pd.start) + uint64(block), run, nil
	case PartitionVirtual:
		if uint64(block) >= uint64(len(p.vat)) {
			return 0, 0, fmt.Errorf("%w: virtual block %d beyond the %d-entry allocation table", ErrOutOfRange, block, len(p.vat))
		}
		t := p.vat[block]
		if t == 0xFFFFFFFF {
			return 0, 0, fmt.Errorf("%w: virtual block %d is not allocated", ErrCorrupt, block)
		}
		abs, _, err := v.resolve(p.phys, t, false, depth+1)
		return abs, 1, err
	case PartitionMetadata:
		c := p.md
		if mirror {
			if p.mirror == nil {
				return 0, 0, fmt.Errorf("%w: no metadata mirror", ErrCorrupt)
			}
			c = p.mirror
		}
		off := int64(block) * v.bs
		if off >= c.size {
			return 0, 0, fmt.Errorf("%w: metadata block %d beyond the %d-byte metadata file", ErrOutOfRange, block, c.size)
		}
		k := sort.Search(len(c.spans), func(k int) bool { return c.spans[k].logical+c.spans[k].length > off })
		if k == len(c.spans) || c.spans[k].zero {
			return 0, 0, fmt.Errorf("%w: metadata block %d is not recorded", ErrCorrupt, block)
		}
		s := c.spans[k]
		within := (off - s.logical) / v.bs
		if uint64(s.block)+uint64(within) > math.MaxUint32 {
			return 0, 0, fmt.Errorf("%w: metadata block %d", ErrOutOfRange, block)
		}
		abs, run, err := v.resolve(s.part, s.block+uint32(within), false, depth+1)
		left := (s.logical + s.length - off + v.bs - 1) / v.bs
		return abs, uint32(min(int64(run), left)), err
	}
	return 0, 0, fmt.Errorf("%w: partition map type %s", ErrPartition, p.kind)
}

// readPart reads len(p) bytes at byte offset off of partition part.
func (v *Volume) readPart(part int, off int64, p []byte) error {
	for len(p) > 0 {
		blk := off / v.bs
		if blk > math.MaxUint32 || off < 0 {
			return fmt.Errorf("%w: offset %d in partition %d", ErrOutOfRange, off, part)
		}
		k, err := v.readRun(part, uint32(blk), off%v.bs, p, false)
		if err != nil && part >= 0 && part < len(v.parts) && v.parts[part].kind == PartitionMetadata && v.parts[part].mirror != nil {
			k2, err2 := v.readRun(part, uint32(blk), off%v.bs, p, true)
			if err2 == nil {
				v.warnOnce("mirror", "", "metadata file unreadable at block %d (%v); read from the metadata mirror", blk, err)
				k, err = k2, nil
			}
		}
		if err != nil {
			return err
		}
		p = p[k:]
		off += int64(k)
	}
	return nil
}

func (v *Volume) readRun(part int, blk uint32, in int64, p []byte, mirror bool) (int, error) {
	abs, run, err := v.resolve(part, blk, mirror, 0)
	if err != nil {
		return 0, err
	}
	if abs > math.MaxInt64/uint64(v.bs)-1 {
		return 0, fmt.Errorf("%w: block %d", ErrOutOfRange, abs)
	}
	k := int(min(int64(len(p)), int64(max(run, 1))*v.bs-in))
	if err := v.readAt(p[:k], int64(abs)*v.bs+in); err != nil {
		return 0, err
	}
	return k, nil
}

// readBlock reads absolute block abs.
func (v *Volume) readBlock(abs uint64) ([]byte, error) {
	if abs > math.MaxInt64/uint64(v.bs)-1 {
		return nil, fmt.Errorf("%w: block %d", ErrOutOfRange, abs)
	}
	b := make([]byte, v.bs)
	return b, v.readAt(b, int64(abs)*v.bs)
}

// readLogical reads logical block a.
func (v *Volume) readLogical(a addr) ([]byte, error) {
	b := make([]byte, v.bs)
	return b, v.readPart(a.part, int64(a.block)*v.bs, b)
}

// partRef validates a recorded partition reference number.
func (v *Volume) partRef(ref uint16) (int, error) {
	if int(ref) >= v.nmaps {
		return 0, fmt.Errorf("%w: partition reference %d (the volume has %d maps)", ErrPartition, ref, v.nmaps)
	}
	return int(ref), nil
}

// partitionInfo describes the maps for VolumeInfo.
func (v *Volume) partitionInfo() []PartitionInfo {
	var out []PartitionInfo
	for _, p := range v.parts[:v.nmaps] {
		pi := PartitionInfo{Type: p.kind, Identifier: p.ident, Number: p.number, VolumeSequence: p.volSeq,
			PacketLength: p.packetLen, SparedPackets: len(p.sparing)}
		pd := p.pd
		if pd == nil && p.phys >= 0 && p.phys < len(v.parts) {
			pd = v.parts[p.phys].pd
		}
		if pd != nil {
			pi.Start, pi.Length, pi.AccessType = pd.start, pd.length, pd.access
		}
		if p.err != nil {
			pi.Error = p.err.Error()
		}
		out = append(out, pi)
	}
	return out
}
