package udf

import (
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// Volume is an open UDF volume. It is not safe for concurrent use.
type Volume struct {
	r    io.ReaderAt
	size int64 // source size; math.MaxInt64 when unknown
	opts Options
	bs   int64

	lastBlock uint32
	haveLast  bool

	info    VolumeInfo
	parts   []*partMap
	nmaps   int // maps recorded in the logical volume descriptor; parts beyond are internal
	vat     bool
	vatInfo *vatHeader

	root       *Entry
	sysStreams longAD

	warnings []Warning
	dropped  int
	once     map[string]bool
}

// Open reads the UDF volume in r (size bytes long; size <= 0 means unknown,
// in which case reads past the end fail individually and the anchors and
// virtual allocation table at the end of the medium are only found with
// Options.LastBlock). It locates an anchor, reads the volume descriptor
// sequence, the partition maps (with their sparing tables, metadata files
// or virtual allocation table), the integrity sequence and the file set
// descriptor, and prepares the root directory.
//
// It fails only when there is no UDF volume, no usable descriptor sequence
// or no file set descriptor; everything else is recorded in Warnings.
func Open(r io.ReaderAt, size int64, opts *Options) (*Volume, error) {
	if r == nil {
		return nil, ErrNilReader
	}
	o := opts.withDefaults()
	sizes := []int{2048, 512, 1024, 4096}
	if o.BlockSize != 0 {
		if o.BlockSize < 512 || o.BlockSize > 32768 || o.BlockSize&(o.BlockSize-1) != 0 {
			return nil, fmt.Errorf("udf: invalid block size %d", o.BlockSize)
		}
		sizes = []int{o.BlockSize}
	}
	var best error
	bestStage := -1
	for _, bs := range sizes {
		v := &Volume{r: r, size: size, opts: o, bs: int64(bs)}
		if size <= 0 {
			v.size = math.MaxInt64
		}
		stage, err := v.open()
		if err == nil {
			return v, nil
		}
		if stage > bestStage {
			best, bestStage = err, stage
		}
	}
	return nil, best
}

// Stages reached by open, to report the most relevant failure among the
// block sizes tried.
const (
	stageNone = iota
	stageRecognized
	stageAnchor
	stageVolume
	stageDone
)

func (v *Volume) open() (int, error) {
	switch {
	case v.opts.LastBlock != 0:
		v.lastBlock, v.haveLast = v.opts.LastBlock, true
	case v.size != math.MaxInt64 && v.size/v.bs > 0:
		v.lastBlock, v.haveLast = uint32(min(v.size/v.bs-1, math.MaxUint32)), true
	}
	if v.haveLast {
		v.info.VolumeSpaceBlocks = uint32(min(uint64(v.lastBlock)+1, math.MaxUint32))
	}
	v.info.NSR = v.readVRS()
	anchors := v.findAnchors()
	if len(anchors) == 0 {
		if v.info.NSR != 0 {
			return stageRecognized, fmt.Errorf("%w (block size %d)", ErrNoAnchor, v.bs)
		}
		return stageNone, fmt.Errorf("%w: no anchor volume descriptor pointer (block size %d)", ErrNotUDF, v.bs)
	}
	if v.info.NSR == 0 {
		v.warn("", "no NSR02/NSR03 volume recognition descriptor")
	}
	var set *vdsSet
	var lastErr error
	for i, a := range anchors {
		s, err := v.readVDSPair(a)
		if err != nil {
			lastErr = err
			v.warn("", "anchor at block %d: %v", a.block, err)
			continue
		}
		if i > 0 {
			v.warn("", "used the anchor at block %d", a.block)
		}
		_ = v.checkTag("", "anchor volume descriptor pointer", a.raw, tagAVDP, a.block)
		set = s
		break
	}
	if set == nil {
		return stageAnchor, lastErr
	}
	fsdLoc := v.applyVDS(set)
	v.readLVID(set.integrity)
	v.setupPartitions(set.pds)
	if h := v.vatInfo; h != nil {
		if h.lvid != "" {
			v.info.LogicalVolumeID = h.lvid
		}
		v.info.NumFiles, v.info.NumDirectories, v.info.HasCounts = h.files, h.dirs, true
		v.info.MinReadRevision, v.info.MinWriteRevision, v.info.MaxWriteRevision = h.minRead, h.minWrite, h.maxWrite
	}
	if v.info.IntegrityOpen && !v.vat {
		v.warn("", "logical volume integrity is open (the volume was not closed cleanly)")
	}
	v.info.Partitions = v.partitionInfo()
	if err := v.readFSD(fsdLoc); err != nil {
		return stageVolume, err
	}
	if v.info.Revision == 0 {
		v.info.Revision = v.info.MinReadRevision
	}
	return stageDone, nil
}

// Info returns the volume's identifiers, revisions, counts and partitions.
func (v *Volume) Info() VolumeInfo {
	i := v.info
	i.Partitions = append([]PartitionInfo(nil), v.info.Partitions...)
	return i
}

// BlockSize returns the logical block size.
func (v *Volume) BlockSize() int { return int(v.bs) }

// Root returns the root directory.
func (v *Volume) Root() *Entry { return v.root }

// Warnings returns the warnings recorded so far: those found by Open and by
// every ReadDir, Walk and OpenFile since (reading a directory twice records
// its warnings twice).
func (v *Volume) Warnings() []Warning {
	w := append([]Warning(nil), v.warnings...)
	if v.dropped > 0 {
		w = append(w, Warning{Message: fmt.Sprintf("%d further warnings dropped", v.dropped)})
	}
	return w
}

func (v *Volume) warn(path, format string, args ...any) {
	if len(v.warnings) >= v.opts.MaxWarnings {
		v.dropped++
		return
	}
	v.warnings = append(v.warnings, Warning{Path: path, Message: fmt.Sprintf(format, args...)})
}

// warnOnce records a warning only the first time key is seen.
func (v *Volume) warnOnce(key, path, format string, args ...any) {
	if v.once == nil {
		v.once = map[string]bool{}
	}
	if v.once[key] {
		return
	}
	v.once[key] = true
	v.warn(path, format, args...)
}

// readAt reads exactly len(p) bytes at off.
func (v *Volume) readAt(p []byte, off int64) error {
	if off < 0 || off > v.size || int64(len(p)) > v.size-off {
		return fmt.Errorf("%w: %d bytes at offset %d", ErrOutOfRange, len(p), off)
	}
	n, err := v.r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("udf: reading %d bytes at offset %d: %w", len(p), off, err)
}

// readVRS scans the volume recognition sequence at byte 32768 of the
// session (one descriptor per 2048 bytes, or per block for larger blocks)
// and returns 2 or 3 for an NSR02/NSR03 descriptor, else 0.
func (v *Volume) readVRS() int {
	step := max(v.bs, 2048)
	base := int64(v.opts.SessionStart)*v.bs + 32768
	nsr := 0
	b := make([]byte, 7)
	for i := range int64(64) {
		if err := v.readAt(b, base+i*step); err != nil {
			break
		}
		if b[0] != 0 && string(b[1:6]) != "CD001" {
			break
		}
		switch string(b[1:6]) {
		case "NSR02":
			nsr = 2
		case "NSR03":
			nsr = 3
		case "BEA01", "CD001", "CDW02", "BOOT2":
		default: // TEA01 or not a descriptor
			return nsr
		}
	}
	return nsr
}

// anchor is an anchor volume descriptor pointer.
type anchor struct {
	block         uint32
	main, reserve extentAD
	raw           []byte
}

// findAnchors reads the anchor locations (session start + 256, the last
// block and 256 before it, block 512) and returns the anchors found, those
// with a correct checksum and location first.
func (v *Volume) findAnchors() []anchor {
	s := uint64(v.opts.SessionStart)
	locs := []uint64{s + 256, 256}
	if v.haveLast {
		if v.lastBlock >= 256 {
			locs = append(locs, uint64(v.lastBlock)-256)
		}
		locs = append(locs, uint64(v.lastBlock))
	}
	locs = append(locs, s+512, 512)
	seen := map[uint64]bool{}
	var good, weak []anchor
	for _, l := range locs {
		if seen[l] || l > math.MaxUint32 {
			continue
		}
		seen[l] = true
		b, err := v.readBlock(l)
		if err != nil || le16(b) != tagAVDP {
			continue
		}
		a := anchor{block: uint32(l), main: parseExtentAD(b[16:]), reserve: parseExtentAD(b[24:]), raw: b}
		if a.main.length == 0 && a.reserve.length == 0 {
			continue
		}
		if tagChecksum(b) == b[4] && parseTag(b).loc == uint32(l) {
			good = append(good, a)
		} else {
			weak = append(weak, a)
		}
	}
	return append(good, weak...)
}

// vdsSet holds the prevailing descriptors of a volume descriptor sequence.
type vdsSet struct {
	pvd, lvd, iuvd []byte
	pvdSeq, lvdSeq uint32
	pvdLoc, lvdLoc uint32
	pds            map[uint16]*partDesc
	integrity      extentAD
}

const maxVDSBlocks = 4096

var descNames = map[uint16]string{tagPVD: "primary volume descriptor", tagLVD: "logical volume descriptor",
	tagPD: "partition descriptor", tagIUVD: "implementation use volume descriptor", tagUSD: "unallocated space descriptor"}

// readVDS reads one volume descriptor sequence into set, following volume
// descriptor pointers, and keeps the descriptors with the highest volume
// descriptor sequence numbers. It returns the first read error, if any.
func (v *Volume) readVDS(ext extentAD, set *vdsSet) error {
	seen := map[uint32]bool{}
	loc, length := ext.loc, ext.length
	for hop := 0; ; hop++ {
		if hop >= 64 || seen[loc] {
			v.warn("", "volume descriptor pointers loop at block %d", loc)
			return nil
		}
		seen[loc] = true
		n := max(int64(length)/v.bs, 1)
		if n > maxVDSBlocks {
			v.warn("", "volume descriptor sequence of %d blocks; reading %d", n, maxVDSBlocks)
			n = maxVDSBlocks
		}
		next := false
	blocks:
		for i := int64(0); i < n; i++ {
			blk := uint64(loc) + uint64(i)
			if blk > math.MaxUint32 {
				break
			}
			b, err := v.readBlock(blk)
			if err != nil {
				return err
			}
			id := le16(b)
			switch id {
			case tagTD:
				_ = v.checkTag("", "terminating descriptor", b, tagTD, uint32(blk))
				return nil
			case tagVDP:
				_ = v.checkTag("", "volume descriptor pointer", b, tagVDP, uint32(blk))
				nx := parseExtentAD(b[20:])
				loc, length, next = nx.loc, nx.length, true
				break blocks
			case tagPVD, tagLVD, tagPD, tagIUVD, tagUSD:
			default:
				if allZero(b) {
					v.warn("", "volume descriptor sequence at block %d ends without a terminating descriptor", loc)
					return nil
				}
				v.warn("", "unknown descriptor (tag %d) at block %d in the volume descriptor sequence", id, blk)
				continue
			}
			_ = v.checkTag("", descNames[id], b, id, uint32(blk))
			seq := le32(b[16:])
			switch id {
			case tagPVD:
				if set.pvd == nil || seq >= set.pvdSeq {
					set.pvd, set.pvdSeq, set.pvdLoc = b, seq, uint32(blk)
				}
			case tagIUVD:
				set.iuvd = b
			case tagPD:
				pd := &partDesc{flags: le16(b[20:]), number: le16(b[22:]), access: le32(b[184:]),
					start: le32(b[188:]), length: le32(b[192:]), vdsn: seq}
				if old := set.pds[pd.number]; old == nil || seq >= old.vdsn {
					set.pds[pd.number] = pd
				}
			case tagLVD:
				// The partition maps may continue into further blocks.
				total := 440 + int64(le32(b[264:]))
				if total > v.bs {
					if total > 1<<16 {
						v.warn("", "logical volume descriptor at block %d: partition map table of %d bytes truncated", blk, total-440)
						total = 1 << 16
					}
					full := make([]byte, (total+v.bs-1)/v.bs*v.bs)
					if err := v.readAt(full, int64(blk)*v.bs); err != nil {
						v.warn("", "logical volume descriptor at block %d: %v", blk, err)
					} else {
						b = full
						i += int64(len(full))/v.bs - 1
					}
				}
				if set.lvd == nil || seq >= set.lvdSeq {
					set.lvd, set.lvdSeq, set.lvdLoc = b, seq, uint32(blk)
				}
			}
		}
		if !next {
			return nil
		}
	}
}

// readVDSPair reads the main volume descriptor sequence of anchor a, and the
// reserve one when the main one lacks a primary, logical volume or
// partition descriptor.
func (v *Volume) readVDSPair(a anchor) (*vdsSet, error) {
	main := &vdsSet{pds: map[uint16]*partDesc{}}
	if err := v.readVDS(a.main, main); err != nil {
		v.warn("", "main volume descriptor sequence at block %d: %v", a.main.loc, err)
	}
	if main.lvd != nil && len(main.pds) > 0 && main.pvd != nil {
		return main, nil
	}
	res := &vdsSet{pds: map[uint16]*partDesc{}}
	if a.reserve.length > 0 {
		if err := v.readVDS(a.reserve, res); err != nil {
			v.warn("", "reserve volume descriptor sequence at block %d: %v", a.reserve.loc, err)
		}
	}
	used := false
	if main.lvd == nil && res.lvd != nil {
		main.lvd, main.lvdSeq, main.lvdLoc, used = res.lvd, res.lvdSeq, res.lvdLoc, true
	}
	if main.pvd == nil && res.pvd != nil {
		main.pvd, main.pvdSeq, main.pvdLoc, used = res.pvd, res.pvdSeq, res.pvdLoc, true
	}
	if main.iuvd == nil {
		main.iuvd = res.iuvd
	}
	for n, pd := range res.pds {
		if main.pds[n] == nil {
			main.pds[n], used = pd, true
		}
	}
	if used {
		v.warn("", "main volume descriptor sequence incomplete; used descriptors from the reserve sequence")
	}
	if main.lvd == nil {
		return nil, fmt.Errorf("%w in the main or reserve volume descriptor sequence", ErrNoVolume)
	}
	if main.pvd == nil {
		v.warn("", "no primary volume descriptor")
	}
	return main, nil
}

// applyVDS decodes the prevailing descriptors into the volume information
// and partition maps, and returns the file set descriptor's location.
func (v *Volume) applyVDS(set *vdsSet) longAD {
	str := func(what string, b []byte) string {
		s, msg := dstring(b)
		if msg != "" {
			v.warn("", "%s: %s", what, msg)
		}
		return s
	}
	ts := func(what string, b []byte) *time.Time {
		t, msg := timestamp(b)
		if msg != "" {
			v.warn("", "%s: %s", what, msg)
		}
		return t
	}
	if b := set.pvd; b != nil {
		v.info.VolumeID = str("volume identifier", b[24:56])
		v.info.VolumeSetID = str("volume set identifier", b[72:200])
		v.info.ApplicationID = parseRegid(b[344:376]).id
		v.info.Recorded = ts("primary volume descriptor recording time", b[376:388])
		v.info.ImplementationID = parseRegid(b[388:420]).id
	}
	b := set.lvd
	v.info.LogicalVolumeID = str("logical volume identifier", b[84:212])
	if lbs := le32(b[212:]); int64(lbs) != v.bs {
		v.warn("", "logical block size %d differs from the block size %d in use", lbs, v.bs)
	}
	dom := parseRegid(b[216:248])
	v.info.DomainID = dom.id
	if dom.id == "*OSTA UDF Compliant" {
		v.info.Revision = dom.udfRevision()
	} else if dom.id != "" {
		v.warn("", "domain identifier %q is not \"*OSTA UDF Compliant\"", dom.id)
	}
	if impl := parseRegid(b[272:304]).id; impl != "" {
		v.info.ImplementationID = impl
	}
	set.integrity = parseExtentAD(b[432:])
	mtl := le32(b[264:])
	table := b[440:]
	if uint64(mtl) < uint64(len(table)) {
		table = table[:mtl]
	} else if uint64(mtl) > uint64(len(table)) {
		v.warn("", "partition map table length %d exceeds the logical volume descriptor", mtl)
	}
	v.parsePartitionMaps(table, le32(b[268:]))
	return parseLongAD(b[248:264])
}

// readLVID follows the logical volume integrity sequence and records the
// last integrity descriptor's contents.
func (v *Volume) readLVID(ext extentAD) {
	if ext.length == 0 {
		v.warn("", "no logical volume integrity sequence")
		return
	}
	var last []byte
	var lastLoc uint32
	seen := map[uint32]bool{}
	for hop := 0; ext.length > 0; hop++ {
		if hop >= 64 || seen[ext.loc] {
			v.warn("", "logical volume integrity sequence loops at block %d", ext.loc)
			break
		}
		seen[ext.loc] = true
		n := min(max(int64(ext.length)/v.bs, 1), 1024)
		var next extentAD
	blocks:
		for i := int64(0); i < n; i++ {
			blk := uint64(ext.loc) + uint64(i)
			if blk > math.MaxUint32 {
				break
			}
			b, err := v.readBlock(blk)
			if err != nil {
				if last == nil {
					v.warn("", "logical volume integrity descriptor at block %d: %v", blk, err)
				}
				break
			}
			switch le16(b) {
			case tagLVID:
				last, lastLoc = b, uint32(blk)
				next = parseExtentAD(b[32:])
			case tagTD:
				break blocks
			default:
				if last == nil {
					v.warn("", "no logical volume integrity descriptor at block %d", blk)
				}
				break blocks
			}
		}
		ext = next
	}
	if last == nil {
		return
	}
	b := last
	_ = v.checkTag("", "logical volume integrity descriptor", b, tagLVID, lastLoc)
	t, msg := timestamp(b[16:28])
	if msg != "" {
		v.warn("", "integrity descriptor time: %s", msg)
	}
	v.info.IntegrityTime = t
	v.info.IntegrityOpen = le32(b[28:]) == 0
	v.info.NextUniqueID = le64(b[40:])
	np, liu := int64(le32(b[72:])), int64(le32(b[76:]))
	iu := 80 + 8*np
	if np > 64 || iu+liu > int64(len(b)) {
		v.warn("", "logical volume integrity descriptor: implementation use (%d partitions, %d bytes) does not fit", np, liu)
		return
	}
	if liu >= 46 {
		u := b[iu:]
		v.info.IntegrityImplementationID = parseRegid(u[:32]).id
		v.info.NumFiles, v.info.NumDirectories, v.info.HasCounts = le32(u[32:]), le32(u[36:]), true
		v.info.MinReadRevision, v.info.MinWriteRevision, v.info.MaxWriteRevision = le16(u[40:]), le16(u[42:]), le16(u[44:])
	}
}

// readFSD reads the file set descriptor sequence starting at loc, uses the
// descriptor with the highest file set descriptor number and prepares the
// root directory.
func (v *Volume) readFSD(loc longAD) error {
	part, err := v.partRef(loc.part)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoFileSet, err)
	}
	var best []byte
	var bestNum uint32
	var bestAt addr
	var firstErr error
	seen := map[addr]bool{}
	ext := loc
	for hop := 0; ; hop++ {
		a := addr{part, ext.block}
		if hop >= 64 || seen[a] {
			v.warn("", "file set descriptor chain loops at block %d", ext.block)
			break
		}
		seen[a] = true
		n := min(max(int64(ext.size())/v.bs, 1), 256)
		var next longAD
		stop := false
		for i := int64(0); i < n && !stop; i++ {
			if uint64(a.block)+uint64(i) > math.MaxUint32 {
				break
			}
			at := addr{a.part, a.block + uint32(i)}
			b, err := v.readLogical(at)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			switch le16(b) {
			case tagFSD:
				num := le32(b[44:])
				if best == nil || num >= bestNum {
					best, bestNum, bestAt = b, num, at
				}
				if nx := parseLongAD(b[448:]); nx.size() != 0 {
					next = nx
					stop = true
				}
			case tagTD:
				stop = true
			default:
				if best == nil && firstErr == nil {
					firstErr = fmt.Errorf("%w: no file set descriptor at block %d (tag %d)", ErrCorrupt, at.block, le16(b))
				}
				stop = true
			}
		}
		if next.size() == 0 {
			break
		}
		np, err := v.partRef(next.part)
		if err != nil {
			v.warn("", "file set descriptor next extent: %v", err)
			break
		}
		ext, part = next, np
	}
	if best == nil {
		if firstErr == nil {
			firstErr = errors.New("not found")
		}
		return fmt.Errorf("%w: %v", ErrNoFileSet, firstErr)
	}
	b := best
	_ = v.checkTag("", "file set descriptor", b, tagFSD, bestAt.block)
	str := func(what string, f []byte) string {
		s, msg := dstring(f)
		if msg != "" {
			v.warn("", "%s: %s", what, msg)
		}
		return s
	}
	t, msg := timestamp(b[16:28])
	if msg != "" {
		v.warn("", "file set recording time: %s", msg)
	}
	v.info.FileSetRecorded = t
	if v.info.LogicalVolumeID == "" {
		v.info.LogicalVolumeID = str("file set logical volume identifier", b[112:240])
	}
	v.info.FileSetID = str("file set identifier", b[304:336])
	v.info.CopyrightFile = str("copyright file identifier", b[336:368])
	v.info.AbstractFile = str("abstract file identifier", b[368:400])
	dom := parseRegid(b[416:448])
	if v.info.Revision == 0 && dom.id == "*OSTA UDF Compliant" {
		v.info.Revision = dom.udfRevision()
	}
	if ss := parseLongAD(b[464:480]); ss.size() != 0 && max(v.info.Revision, v.info.MinReadRevision) >= 0x0200 {
		v.sysStreams = ss
		v.info.HasSystemStreams = true
	}
	rootAD := parseLongAD(b[400:416])
	v.root = &Entry{Path: "/", IsDir: true, Characteristics: CharDirectory, vol: v,
		FileType: FileTypeDirectory, ICB: ICB{Partition: rootAD.part, Block: rootAD.block}}
	v.loadEntry(v.root, rootAD, false)
	if v.root.err != nil {
		v.warn("/", "root directory: %v", v.root.err)
	}
	return nil
}
