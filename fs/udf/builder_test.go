package udf

import (
	"encoding/binary"
	"time"
	"unicode/utf16"
)

// A byte-level UDF volume builder for the tests. Layout (in blocks of bs
// bytes): volume recognition sequence at byte 32768, main volume descriptor
// sequence at 32, reserve at 48 (48 and 64 for 1024-byte blocks), integrity
// sequence at 128, anchors at 256 and at the last block, the partition from
// block 272. Inside the partition: the file set descriptor first, then file
// entries, directory and file data in allocation order. Variants add a
// sparable partition (with spare packets and two sparing tables after the
// partition), a virtual partition (with a UDF 1.50 or 2.00 VAT in the last
// block) or a metadata partition (metadata file and mirror in the physical
// partition).

const (
	kindPhysical = ""
	kindSparable = "sparable"
	kindVAT150   = "vat150"
	kindVAT200   = "vat200"
	kindMetadata = "metadata"
)

const (
	adShort    = 0
	adLong     = 1
	adExt      = 2
	adEmbedded = 3
)

// UDF permissions for 0644 and 0755.
const (
	perm644 = PermOwnerRead | PermOwnerWrite | PermGroupRead | PermOtherRead
	perm755 = perm644 | PermOwnerExec | PermGroupExec | PermOtherExec
)

// unode is a file, directory or other object of a test volume.
type unode struct {
	name  string
	raw   []byte // file identifier as recorded (overrides name)
	ftype byte   // 0: directory or regular file
	dir   bool
	data  []byte
	kids  []*unode

	char      byte // extra FID characteristics
	perms     uint32
	uid, gid  uint32
	icbFlags  uint16
	links     uint16
	mtime     []byte
	eas       [][]byte // extended attributes (with their headers)
	eaFile    bool     // record eas in an extended attribute file
	streams   []*unode
	adType    int   // adShort.. adEmbedded; -1: the builder's default
	split     []int // data extent sizes
	kinds     []byte
	aedPer    int  // allocation descriptors per FE/AED before a continuation
	aedLoop   bool // the last AED continues into the first
	strategy  bool // strategy 4096 with an older entry and an indirect entry
	icbLoop   bool // strategy 4096 indirect entry that points back
	linkTo    *unode
	feFix     func(fe []byte) // applied after tagging
	fidFix    func(fid []byte)
	efe       bool
	noFE      bool // the FID points at an unrecorded block
	dataSpace int  // -1 default; 0 forces the physical partition
	impl      []byte

	// assigned
	icb     uint32
	icb2    uint32 // strategy 4096: the newer ICB
	feAbs   uint32
	dataAbs []uint32
	aedAbs  []uint32
}

func ufile(name string, data []byte) *unode {
	return &unode{name: name, data: data, perms: perm644, uid: 1000, gid: 100, adType: -1, dataSpace: -1}
}

func udir(name string, kids ...*unode) *unode {
	return &unode{name: name, dir: true, kids: kids, perms: perm755, uid: 1000, gid: 100, adType: -1, dataSpace: -1}
}

// retag recomputes the checksum and CRC of the descriptor d after a change.
func retag(d []byte) {
	crcLen := int(le16(d[10:]))
	binary.LittleEndian.PutUint16(d[8:], crcITU(d[16:16+crcLen]))
	d[4] = tagChecksum(d)
}

// space is a partition as the builder allocates in it.
type space struct {
	ref   uint16
	alloc func(n int) uint32
	abs   func(blk uint32) uint32
}

type ubuilder struct {
	bs       int
	rev      uint16
	efe      bool
	ad       int
	kind     string
	img      []byte
	session  uint32
	tailAVDP bool
	vdsMain  uint32
	vdsRes   uint32

	partStart uint32
	pnext     uint32 // physical partition allocation cursor
	phys      *space
	fs        *space // where file entries and directories go
	vat       []uint32

	// sparable
	packetLen uint32
	spare     []uint32 // packets to remap
	tableLocs [2]uint32

	// metadata
	mdLen     uint32
	mdNext    uint32
	mdFE      uint32
	mirrorFE  uint32
	mdRegions [2]uint32 // physical starts of the two halves of the metadata file
	mirrorRgn uint32
	mdSplit   bool
	noMirror  bool

	uniq       uint64
	files      uint32
	dirs       uint32
	fsdBlock   uint32
	sysStreams []*unode
	sysICB     uint32

	volID, lvID, fsID                           string
	pvdHook, lvdHook, fsdHook, lvidHook, pdHook func(b []byte)
	lvidOpen                                    bool
	noVRS                                       bool
	lvidNext                                    *extentAD
}

func newUB() *ubuilder {
	return &ubuilder{bs: 2048, rev: 0x0201, ad: adShort, tailAVDP: true, partStart: 272, vdsMain: 32, vdsRes: 48,
		packetLen: 32, mdLen: 64, volID: "TESTVOL", lvID: "Test Volume", fsID: "Test File Set", uniq: 16}
}

func (b *ubuilder) grow(end int) {
	if len(b.img) < end {
		b.img = append(b.img, make([]byte, end-len(b.img))...)
	}
}

// put writes data at absolute block abs.
func (b *ubuilder) put(abs uint32, data []byte) {
	start := int(abs) * b.bs
	b.grow(start + len(data))
	copy(b.img[start:], data)
}

func (b *ubuilder) block(abs uint32) []byte {
	b.grow((int(abs) + 1) * b.bs)
	return b.img[int(abs)*b.bs : (int(abs)+1)*b.bs]
}

func (b *ubuilder) physAlloc(n int) uint32 {
	l := b.pnext
	b.pnext += uint32(n)
	b.grow(int(b.partStart+b.pnext) * b.bs)
	return l
}

func (b *ubuilder) setup() {
	b.phys = &space{ref: 0, alloc: b.physAlloc, abs: func(blk uint32) uint32 { return b.partStart + blk }}
	b.fs = b.phys
	switch b.kind {
	case kindVAT150, kindVAT200:
		b.fs = &space{ref: 1,
			alloc: func(n int) uint32 {
				first := uint32(len(b.vat))
				for range n {
					b.vat = append(b.vat, b.physAlloc(1))
				}
				return first
			},
			abs: func(blk uint32) uint32 { return b.partStart + b.vat[blk] }}
	case kindMetadata:
		b.mdFE = b.physAlloc(1)
		b.mirrorFE = b.physAlloc(1)
		half := b.mdLen / 2
		b.mdRegions[0] = b.physAlloc(int(half))
		if b.mdSplit {
			b.physAlloc(3) // a gap: the two extents are not contiguous
		}
		b.mdRegions[1] = b.physAlloc(int(b.mdLen - half))
		b.mirrorRgn = b.physAlloc(int(b.mdLen))
		b.fs = &space{ref: 1,
			alloc: func(n int) uint32 {
				l := b.mdNext
				b.mdNext += uint32(n)
				if b.mdNext > b.mdLen {
					panic("metadata partition full")
				}
				return l
			},
			abs: func(blk uint32) uint32 { return b.partStart + b.mdPhys(blk) }}
	}
}

func (b *ubuilder) mdPhys(blk uint32) uint32 {
	half := b.mdLen / 2
	if blk < half {
		return b.mdRegions[0] + blk
	}
	return b.mdRegions[1] + blk - half
}

// setTag fills in the descriptor tag of d (identifier, location, CRC over
// crcLen bytes after the tag, checksum).
func (b *ubuilder) setTag(d []byte, id uint16, loc uint32, crcLen int) {
	ver := uint16(2)
	if b.rev >= 0x0200 {
		ver = 3
	}
	binary.LittleEndian.PutUint16(d, id)
	binary.LittleEndian.PutUint16(d[2:], ver)
	binary.LittleEndian.PutUint16(d[6:], 1)
	binary.LittleEndian.PutUint16(d[8:], crcITU(d[16:16+crcLen]))
	binary.LittleEndian.PutUint16(d[10:], uint16(crcLen))
	binary.LittleEndian.PutUint32(d[12:], loc)
	d[4] = tagChecksum(d)
}

func regidBytes(id string, suffix ...byte) []byte {
	r := make([]byte, 32)
	copy(r[1:24], id)
	copy(r[24:], suffix)
	return r
}

func (b *ubuilder) domainID() []byte {
	return regidBytes("*OSTA UDF Compliant", byte(b.rev), byte(b.rev>>8))
}

func (b *ubuilder) udfRegid(id string) []byte {
	return regidBytes(id, byte(b.rev), byte(b.rev>>8), 6) // OS class 6: Unix
}

// cs0 encodes s as OSTA CS0: 8-bit when possible, else UTF-16BE.
func cs0(s string) []byte {
	if s == "" {
		return nil
	}
	narrow := true
	for _, r := range s {
		if r > 0xFF {
			narrow = false
		}
	}
	if narrow {
		out := []byte{8}
		for _, r := range s {
			out = append(out, byte(r))
		}
		return out
	}
	out := []byte{16}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

func dstr(s string, n int) []byte {
	d := make([]byte, n)
	c := cs0(s)
	copy(d, c)
	d[n-1] = byte(len(c))
	return d
}

// ts encodes a timestamp of type 1 (local time) with t's zone offset.
func ts(t time.Time) []byte {
	b := make([]byte, 12)
	_, off := t.Zone()
	tz := uint16(1<<12) | uint16(int16(off/60))&0xFFF
	binary.LittleEndian.PutUint16(b, tz)
	binary.LittleEndian.PutUint16(b[2:], uint16(t.Year()))
	b[4], b[5], b[6], b[7], b[8] = byte(t.Month()), byte(t.Day()), byte(t.Hour()), byte(t.Minute()), byte(t.Second())
	ns := t.Nanosecond()
	b[9], b[10], b[11] = byte(ns/10_000_000), byte(ns/100_000%100), byte(ns/1000%100)
	return b
}

var (
	testZone  = time.FixedZone("", 3600)
	testMTime = time.Date(2020, 2, 3, 4, 5, 6, 780_000_000, testZone)
	testCTime = time.Date(2019, 1, 2, 3, 4, 5, 0, time.UTC)
)

func longADBytes(length, kind uint32, block uint32, ref uint16) []byte {
	d := make([]byte, 16)
	binary.LittleEndian.PutUint32(d, length|kind<<30)
	binary.LittleEndian.PutUint32(d[4:], block)
	binary.LittleEndian.PutUint16(d[8:], ref)
	return d
}

// adBytes encodes one allocation descriptor of type t.
func adBytes(t int, length uint32, kind byte, block uint32, ref uint16) []byte {
	lf := length | uint32(kind)<<30
	switch t {
	case adShort:
		d := make([]byte, 8)
		binary.LittleEndian.PutUint32(d, lf)
		binary.LittleEndian.PutUint32(d[4:], block)
		return d
	case adLong:
		return longADBytes(length, uint32(kind), block, ref)
	default:
		d := make([]byte, 20)
		binary.LittleEndian.PutUint32(d, lf)
		rec := length
		if kind != 0 {
			rec = 0
		}
		binary.LittleEndian.PutUint32(d[4:], rec)
		binary.LittleEndian.PutUint32(d[8:], length)
		binary.LittleEndian.PutUint32(d[12:], block)
		binary.LittleEndian.PutUint16(d[16:], ref)
		return d
	}
}

// eaAttr encodes a generic extended attribute.
func eaAttr(typ uint32, sub byte, body []byte) []byte {
	l := (12 + len(body) + 3) &^ 3
	d := make([]byte, l)
	binary.LittleEndian.PutUint32(d, typ)
	d[4] = sub
	binary.LittleEndian.PutUint32(d[8:], uint32(l))
	copy(d[12:], body)
	return d
}

// implEA encodes an implementation-use (or application-use) attribute.
func implEA(typ uint32, id string, iu []byte) []byte {
	body := make([]byte, 36+len(iu))
	binary.LittleEndian.PutUint32(body, uint32(len(iu)))
	copy(body[4:], regidBytes(id))
	copy(body[36:], iu)
	return eaAttr(typ, 1, body)
}

func devEA(major, minor uint32) []byte {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[4:], major)
	binary.LittleEndian.PutUint32(body[8:], minor)
	return eaAttr(EADeviceSpecification, 1, body)
}

// eaSpace builds an extended attribute space recorded at block loc.
func (b *ubuilder) eaSpace(loc uint32, attrs [][]byte) []byte {
	d := make([]byte, 24)
	binary.LittleEndian.PutUint32(d[16:], 24)
	binary.LittleEndian.PutUint32(d[20:], 24)
	for _, a := range attrs {
		d = append(d, a...)
	}
	b.setTag(d[:24], tagEAHD, loc, 8)
	return d
}

// pathComp encodes a symbolic link path component.
func pathComp(typ byte, name string) []byte {
	c := cs0(name)
	return append([]byte{typ, byte(len(c)), 0, 0}, c...)
}

func (n *unode) fileType() byte {
	if n.ftype != 0 {
		return n.ftype
	}
	if n.dir {
		return FileTypeDirectory
	}
	return FileTypeRegular
}

func (n *unode) rawName() []byte {
	if n.raw != nil {
		return n.raw
	}
	return cs0(n.name)
}

func (n *unode) icbFinal() uint32 {
	if n.strategy {
		return n.icb2
	}
	return n.icb
}

// assign allocates the ICBs of n and its descendants and streams.
func (b *ubuilder) assign(n *unode) {
	switch {
	case n.noFE:
		n.icb = b.fs.alloc(1) // left unrecorded
	case n.linkTo == nil:
		k := 1
		if n.strategy || n.icbLoop {
			k = 2
		}
		n.icb = b.fs.alloc(k)
		if n.strategy {
			n.icb2 = b.fs.alloc(2)
		}
	}
	if n.linkTo == nil && n.char&CharDeleted == 0 {
		if n.dir {
			b.dirs++
		} else {
			b.files++
		}
	}
	for _, s := range n.streams {
		b.assign(s)
	}
	for _, k := range n.kids {
		b.assign(k)
	}
}

func fidLen(name []byte) int { return (38 + len(name) + 3) &^ 3 }

// fid encodes a file identifier descriptor at block loc.
func (b *ubuilder) fid(char byte, icb uint32, ref uint16, name []byte, loc uint32) []byte {
	d := make([]byte, fidLen(name))
	binary.LittleEndian.PutUint16(d[16:], 1)
	d[18] = char
	d[19] = byte(len(name))
	copy(d[20:], longADBytes(uint32(b.bs), 0, icb, ref))
	copy(d[38:], name)
	b.setTag(d, tagFID, loc, 38+len(name)-16)
	return d
}

// write records n (whose parent ICB is parent) and its descendants.
func (b *ubuilder) write(n *unode, parent uint32, sp *space) {
	if n.linkTo != nil || n.noFE {
		return
	}
	if !n.dir && n.ftype != FileTypeStreamDirectory {
		b.writeData(n, sp, n.data, nil)
		return
	}
	// Directory data: parent FID, then the children. Lay out first, then
	// encode with the tag locations of the blocks used.
	size := fidLen(nil)
	for _, k := range n.kids {
		size += fidLen(k.rawName())
	}
	b.writeData(n, sp, make([]byte, size), func(locOf func(off int) uint32) []byte {
		out := b.fid(CharDirectory|CharParent, parent, sp.ref, nil, locOf(0))
		for _, k := range n.kids {
			icb := k.icb
			if k.linkTo != nil {
				icb = k.linkTo.icb
			}
			char := k.char
			if k.dir {
				char |= CharDirectory
			}
			f := b.fid(char, icb, sp.ref, k.rawName(), locOf(len(out)))
			if k.fidFix != nil {
				k.fidFix(f)
			}
			out = append(out, f...)
		}
		return out
	})
	for _, k := range n.kids {
		b.write(k, n.icb, sp)
	}
}

// writeData records n's data (generated by gen once block locations are
// known, for directories) and its file entry.
func (b *ubuilder) writeData(n *unode, sp *space, data []byte, gen func(func(int) uint32) []byte) {
	adType := n.adType
	if adType < 0 {
		adType = b.ad
	}
	dsp := sp
	isDir := n.dir || n.ftype == FileTypeStreamDirectory
	if !isDir && (b.kind == kindVAT150 || b.kind == kindVAT200 || b.kind == kindMetadata) {
		dsp = b.phys // file data lives in the physical partition
	}
	if n.dataSpace == 0 {
		dsp = b.phys
	}
	if dsp.ref != sp.ref && adType == adShort {
		adType = adLong
	}
	if adType == adEmbedded {
		if gen != nil {
			data = gen(func(int) uint32 { return n.icbFinal() })
		}
		b.writeFE(n, sp, uint64(len(data)), data, adEmbedded)
		return
	}
	split := n.split
	if split == nil && len(data) > 0 {
		split = []int{len(data)}
	}
	type ext struct {
		off, length int
		blk         uint32
		kind        byte
	}
	var exts []ext
	off := 0
	for i, l := range split {
		kind := byte(0)
		if i < len(n.kinds) {
			kind = n.kinds[i]
		}
		e := ext{off: off, length: l, kind: kind}
		if kind != 2 {
			e.blk = dsp.alloc((l + b.bs - 1) / b.bs)
			if len(split) > 1 {
				dsp.alloc(1) // keep extents apart
			}
		}
		exts = append(exts, e)
		off += l
	}
	if gen != nil {
		data = gen(func(o int) uint32 {
			for _, e := range exts {
				if o >= e.off && o < e.off+e.length {
					return e.blk + uint32((o-e.off)/b.bs)
				}
			}
			return 0
		})
	}
	var ads [][]byte
	for _, e := range exts {
		ads = append(ads, adBytes(adType, uint32(e.length), e.kind, e.blk, dsp.ref))
		if e.kind == 2 {
			continue
		}
		for k := range (e.length + b.bs - 1) / b.bs {
			chunk := make([]byte, b.bs)
			if e.kind == 0 {
				copy(chunk, data[e.off+k*b.bs:min(e.off+e.length, len(data))])
			} else {
				for i := range chunk {
					chunk[i] = 0xAA // allocated, not recorded: must read as zeros
				}
			}
			abs := dsp.abs(e.blk + uint32(k))
			b.put(abs, chunk)
			if k == 0 {
				n.dataAbs = append(n.dataAbs, abs)
			}
		}
	}
	var adList []byte
	if n.aedPer > 0 && len(ads) > n.aedPer {
		adList = b.chainAEDs(n, sp, adType, ads)
	} else {
		for _, a := range ads {
			adList = append(adList, a...)
		}
	}
	b.writeFE(n, sp, uint64(len(data)), adList, adType)
}

// chainAEDs puts ads into allocation extent descriptors, n.aedPer per
// descriptor (and in the file entry), and returns the file entry's list.
func (b *ubuilder) chainAEDs(n *unode, sp *space, adType int, ads [][]byte) []byte {
	var groups [][][]byte
	for len(ads) > 0 {
		k := min(n.aedPer, len(ads))
		groups = append(groups, ads[:k])
		ads = ads[k:]
	}
	locs := make([]uint32, len(groups))
	for i := 1; i < len(groups); i++ {
		locs[i] = sp.alloc(1)
	}
	cont := func(i int) []byte { return adBytes(adType, uint32(b.bs), 3, locs[i], sp.ref) }
	flat := func(g [][]byte) []byte {
		var o []byte
		for _, a := range g {
			o = append(o, a...)
		}
		return o
	}
	for i := 1; i < len(groups); i++ {
		list := flat(groups[i])
		if i+1 < len(groups) {
			list = append(list, cont(i+1)...)
		} else if n.aedLoop {
			list = append(list, cont(1)...)
		}
		d := make([]byte, b.bs)
		binary.LittleEndian.PutUint32(d[20:], uint32(len(list)))
		copy(d[24:], list)
		b.setTag(d, tagAED, locs[i], 8+len(list))
		b.put(sp.abs(locs[i]), d)
		n.aedAbs = append(n.aedAbs, sp.abs(locs[i]))
	}
	list := flat(groups[0])
	if len(groups) > 1 {
		list = append(list, cont(1)...)
	}
	return list
}

// writeFE writes n's file entry (and, for strategy 4096, its older entry
// and the indirect entry).
func (b *ubuilder) writeFE(n *unode, sp *space, infoLen uint64, ads []byte, adType int) {
	var eaICB, streamICB, ea []byte
	loc := n.icbFinal()
	if len(n.eas) > 0 {
		if n.eaFile {
			eaLoc := sp.alloc(1)
			dataLoc := sp.alloc(1)
			space := b.eaSpace(dataLoc, n.eas)
			b.put(sp.abs(dataLoc), space)
			blk := b.feBytes(&unode{}, eaLoc, FileTypeExtendedAttributes, uint64(len(space)),
				adBytes(adShort, uint32(len(space)), 0, dataLoc, sp.ref), adShort, nil, nil, nil, false)
			b.put(sp.abs(eaLoc), blk)
			eaICB = longADBytes(uint32(b.bs), 0, eaLoc, sp.ref)
		} else {
			ea = b.eaSpace(loc, n.eas)
		}
	}
	efe := b.efe || n.efe
	if len(n.streams) > 0 {
		sdLoc := b.fs.alloc(1)
		sd := &unode{ftype: FileTypeStreamDirectory, kids: n.streams, adType: -1, dataSpace: -1, icb: sdLoc, perms: n.perms}
		for _, s := range n.streams {
			s.icbFlags |= ICBFlagStream
		}
		b.write(sd, loc, b.fs)
		streamICB = longADBytes(uint32(b.bs), 0, sdLoc, b.fs.ref)
		efe = true
	}
	blk := b.feBytes(n, loc, n.fileType(), infoLen, ads, adType, ea, eaICB, streamICB, efe)
	if n.icbLoop {
		// An ICB of strategy 4096 whose indirect entry points back at it.
		binary.LittleEndian.PutUint16(blk[16+4:], 4096)
		retag(blk)
		b.put(sp.abs(n.icb+1), b.indirect(n.icb+1, n.icb, sp.ref))
	}
	if n.feFix != nil {
		n.feFix(blk)
	}
	n.feAbs = sp.abs(loc)
	b.put(n.feAbs, blk)
	if n.strategy {
		// The older direct entry, with different contents, and an indirect
		// entry to the newer ICB.
		old := b.feBytes(n, n.icb, n.fileType(), 3, []byte("old"), adEmbedded, nil, nil, nil, efe)
		b.put(sp.abs(n.icb), old)
		b.put(sp.abs(n.icb+1), b.indirect(n.icb+1, n.icb2, sp.ref))
	}
}

func (b *ubuilder) indirect(loc, target uint32, ref uint16) []byte {
	d := make([]byte, b.bs)
	binary.LittleEndian.PutUint16(d[16+4:], 4096)
	binary.LittleEndian.PutUint16(d[16+8:], 2)
	d[16+11] = FileTypeIndirect
	copy(d[36:], longADBytes(uint32(b.bs), 0, target, ref))
	b.setTag(d, tagIE, loc, 52-16)
	return d
}

// feBytes encodes a file entry (or extended file entry) block.
func (b *ubuilder) feBytes(n *unode, loc uint32, ft byte, infoLen uint64, ads []byte, adType int, ea, eaICB, streamICB []byte, efe bool) []byte {
	d := make([]byte, b.bs)
	icb := d[16:36]
	strategy := uint16(4)
	if n.strategy {
		strategy = 4096
	}
	binary.LittleEndian.PutUint16(icb[4:], strategy)
	binary.LittleEndian.PutUint16(icb[8:], 1)
	icb[11] = ft
	binary.LittleEndian.PutUint16(icb[18:], uint16(adType)|n.icbFlags)
	binary.LittleEndian.PutUint32(d[36:], n.uid)
	binary.LittleEndian.PutUint32(d[40:], n.gid)
	binary.LittleEndian.PutUint32(d[44:], n.perms)
	binary.LittleEndian.PutUint16(d[48:], max(n.links, 1))
	binary.LittleEndian.PutUint64(d[56:], infoLen)
	mt := n.mtime
	if mt == nil {
		mt = ts(testMTime)
	}
	impl := n.impl
	if impl == nil {
		impl = b.udfRegid("*go-aaru test")
	}
	b.uniq++
	blocks := (infoLen + uint64(b.bs) - 1) / uint64(b.bs)
	var base int
	if efe {
		binary.LittleEndian.PutUint64(d[64:], infoLen)
		binary.LittleEndian.PutUint64(d[72:], blocks)
		copy(d[80:], mt)
		copy(d[92:], mt)
		copy(d[104:], ts(testCTime))
		copy(d[116:], mt)
		copy(d[136:], eaICB)
		copy(d[152:], streamICB)
		copy(d[168:], impl)
		binary.LittleEndian.PutUint64(d[200:], b.uniq)
		binary.LittleEndian.PutUint32(d[208:], uint32(len(ea)))
		binary.LittleEndian.PutUint32(d[212:], uint32(len(ads)))
		base = 216
	} else {
		binary.LittleEndian.PutUint64(d[64:], blocks)
		copy(d[72:], mt)
		copy(d[84:], mt)
		copy(d[96:], mt)
		copy(d[112:], eaICB)
		copy(d[128:], impl)
		binary.LittleEndian.PutUint64(d[160:], b.uniq)
		binary.LittleEndian.PutUint32(d[168:], uint32(len(ea)))
		binary.LittleEndian.PutUint32(d[172:], uint32(len(ads)))
		base = 176
	}
	if base+len(ea)+len(ads) > b.bs {
		panic("file entry overflows its block")
	}
	copy(d[base:], ea)
	copy(d[base+len(ea):], ads)
	id := uint16(tagFE)
	if efe {
		id = tagEFE
	}
	b.setTag(d, id, loc, base+len(ea)+len(ads)-16)
	return d
}

// build lays out the volume with root directory root.
func (b *ubuilder) build(root *unode) []byte {
	b.setup()
	b.fsdBlock = b.fs.alloc(2) // FSD and its terminator
	b.assign(root)
	var sysSD *unode
	if len(b.sysStreams) > 0 {
		sysSD = &unode{ftype: FileTypeStreamDirectory, kids: b.sysStreams, adType: -1, dataSpace: -1}
		sysSD.icb = b.fs.alloc(1)
		for _, s := range b.sysStreams {
			b.assign(s)
			s.icbFlags |= ICBFlagStream
		}
	}
	b.write(root, root.icb, b.fs)
	if sysSD != nil {
		b.write(sysSD, sysSD.icb, b.fs)
		b.sysICB = sysSD.icb
	}
	b.writeFSD(root)
	switch b.kind {
	case kindMetadata:
		b.writeMetadataFiles()
	case kindVAT150, kindVAT200:
		b.writeVAT()
	}
	partLen := b.pnext
	if b.kind == kindSparable {
		partLen = (partLen + b.packetLen - 1) / b.packetLen * b.packetLen
		b.grow(int(b.partStart+partLen) * b.bs)
		b.writeSparing(partLen)
	}
	if b.bs == 1024 { // keep clear of the recognition sequence at byte 32768
		b.vdsMain, b.vdsRes = 48, 64
	}
	b.writeVDS(b.vdsMain, partLen)
	b.writeVDS(b.vdsRes, partLen)
	b.writeLVID()
	n := max(uint32(len(b.img)/b.bs), 300)
	vat := b.kind == kindVAT150 || b.kind == kindVAT200
	if vat {
		n = uint32(len(b.img) / b.bs) // the VAT is the last block
	}
	b.writeAVDP(b.session + 256)
	if b.tailAVDP && !vat {
		b.writeAVDP(n)
		n++
	}
	b.grow(int(n) * b.bs)
	if !b.noVRS {
		step := max(b.bs, 2048)
		nsr := "NSR03"
		if b.rev < 0x0200 {
			nsr = "NSR02"
		}
		for i, id := range []string{"BEA01", nsr, "TEA01"} {
			off := int(b.session)*b.bs + 32768 + i*step
			b.grow(off + 7)
			copy(b.img[off:], "\x00"+id+"\x01")
		}
	}
	return b.img
}

func (b *ubuilder) writeFSD(root *unode) {
	d := make([]byte, b.bs)
	copy(d[16:], ts(testCTime))
	binary.LittleEndian.PutUint16(d[28:], 3)
	binary.LittleEndian.PutUint16(d[30:], 3)
	binary.LittleEndian.PutUint32(d[32:], 1)
	binary.LittleEndian.PutUint32(d[36:], 1)
	copy(d[112:], dstr(b.lvID, 128))
	copy(d[304:], dstr(b.fsID, 32))
	copy(d[336:], dstr("COPYING", 32))
	copy(d[400:], longADBytes(uint32(b.bs), 0, root.icb, b.fs.ref))
	copy(d[416:], b.domainID())
	if b.sysICB != 0 {
		copy(d[464:], longADBytes(uint32(b.bs), 0, b.sysICB, b.fs.ref))
	}
	if b.fsdHook != nil {
		b.fsdHook(d)
	}
	b.setTag(d, tagFSD, b.fsdBlock, 512-16)
	b.put(b.fs.abs(b.fsdBlock), d)
	td := make([]byte, b.bs)
	b.setTag(td, tagTD, b.fsdBlock+1, 512-16)
	b.put(b.fs.abs(b.fsdBlock+1), td)
}

func (b *ubuilder) writeMetadataFiles() {
	write := func(loc uint32, ft byte, regions [][2]uint32) {
		var ads []byte
		for _, r := range regions {
			ads = append(ads, adBytes(adShort, r[1]*uint32(b.bs), 0, r[0], 0)...)
		}
		blk := b.feBytes(&unode{}, loc, ft, uint64(b.mdLen)*uint64(b.bs), ads, adShort, nil, nil, nil, true)
		b.put(b.partStart+loc, blk)
	}
	half := b.mdLen / 2
	write(b.mdFE, FileTypeMetadata, [][2]uint32{{b.mdRegions[0], half}, {b.mdRegions[1], b.mdLen - half}})
	if !b.noMirror {
		// The mirror duplicates the metadata file's content.
		for k := range b.mdLen {
			src := append([]byte(nil), b.block(b.partStart+b.mdPhys(k))...)
			b.put(b.partStart+b.mirrorRgn+k, src)
		}
		write(b.mirrorFE, FileTypeMetadataMirror, [][2]uint32{{b.mirrorRgn, b.mdLen}})
	}
}

func (b *ubuilder) writeVAT() {
	var data []byte
	ft := byte(FileTypeVAT)
	if b.kind == kindVAT200 {
		h := make([]byte, 152)
		binary.LittleEndian.PutUint16(h, 152)
		copy(h[4:], dstr("VAT Volume", 128))
		binary.LittleEndian.PutUint32(h[132:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(h[136:], b.files)
		binary.LittleEndian.PutUint32(h[140:], b.dirs)
		binary.LittleEndian.PutUint16(h[144:], b.rev)
		binary.LittleEndian.PutUint16(h[146:], b.rev)
		binary.LittleEndian.PutUint16(h[148:], b.rev)
		data = h
	}
	for _, p := range b.vat {
		data = binary.LittleEndian.AppendUint32(data, p)
	}
	if b.kind == kindVAT150 {
		data = append(data, regidBytes("*UDF Virtual Alloc Tbl")...)
		data = binary.LittleEndian.AppendUint32(data, 0xFFFFFFFF)
		ft = FileTypeUnspecified
	}
	nb := (len(data) + b.bs - 1) / b.bs
	loc := b.physAlloc(nb)
	for k := range nb {
		chunk := make([]byte, b.bs)
		copy(chunk, data[k*b.bs:])
		b.put(b.partStart+loc+uint32(k), chunk)
	}
	feLoc := b.physAlloc(1)
	blk := b.feBytes(&unode{}, feLoc, ft, uint64(len(data)), adBytes(adShort, uint32(len(data)), 0, loc, 0), adShort,
		nil, nil, nil, b.rev >= 0x0200 && b.efe)
	b.put(b.partStart+feLoc, blk)
}

func (b *ubuilder) writeSparing(partLen uint32) {
	spareArea := b.partStart + partLen
	var entries []byte
	for i, p := range b.spare {
		loc := spareArea + uint32(i)*b.packetLen
		for k := range b.packetLen {
			src := append([]byte(nil), b.block(b.partStart+p+k)...)
			b.put(loc+k, src)
			junk := make([]byte, b.bs)
			for j := range junk {
				junk[j] = 0xEE
			}
			b.put(b.partStart+p+k, junk)
		}
		entries = binary.LittleEndian.AppendUint32(entries, p)
		entries = binary.LittleEndian.AppendUint32(entries, loc)
	}
	// One available spare packet.
	free := spareArea + uint32(len(b.spare))*b.packetLen
	entries = binary.LittleEndian.AppendUint32(entries, 0xFFFFFFFF)
	entries = binary.LittleEndian.AppendUint32(entries, free)
	b.grow(int(free+b.packetLen) * b.bs)
	tableBlocks := uint32((56 + len(entries) + b.bs - 1) / b.bs)
	for i := range 2 {
		loc := free + b.packetLen + uint32(i)*tableBlocks
		b.tableLocs[i] = loc
		d := make([]byte, int(tableBlocks)*b.bs)
		copy(d[16:], b.udfRegid("*UDF Sparing Table"))
		binary.LittleEndian.PutUint16(d[48:], uint16(len(entries)/8))
		binary.LittleEndian.PutUint32(d[52:], 1)
		copy(d[56:], entries)
		b.setTag(d, tagSparingTable, loc, 56+len(entries)-16)
		b.put(loc, d)
	}
}

// partitionMaps returns the partition map table and the number of maps.
func (b *ubuilder) partitionMaps() ([]byte, int) {
	type1 := []byte{1, 6, 1, 0, 0, 0}
	type2 := func(id string) []byte {
		m := make([]byte, 64)
		m[0], m[1] = 2, 64
		copy(m[4:], b.udfRegid(id))
		binary.LittleEndian.PutUint16(m[36:], 1)
		return m
	}
	switch b.kind {
	case kindSparable:
		m := type2("*UDF Sparable Partition")
		binary.LittleEndian.PutUint16(m[40:], uint16(b.packetLen))
		m[42] = 2
		binary.LittleEndian.PutUint32(m[44:], uint32(56+8*(len(b.spare)+1)))
		binary.LittleEndian.PutUint32(m[48:], b.tableLocs[0])
		binary.LittleEndian.PutUint32(m[52:], b.tableLocs[1])
		return m, 1
	case kindVAT150, kindVAT200:
		return append(type1, type2("*UDF Virtual Partition")...), 2
	case kindMetadata:
		m := type2("*UDF Metadata Partition")
		binary.LittleEndian.PutUint32(m[40:], b.mdFE)
		mirror := b.mirrorFE
		if b.noMirror {
			mirror = 0xFFFFFFFF
		}
		binary.LittleEndian.PutUint32(m[44:], mirror)
		binary.LittleEndian.PutUint32(m[48:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(m[52:], 32)
		binary.LittleEndian.PutUint16(m[56:], 1)
		return append(type1, m...), 2
	}
	return type1, 1
}

func (b *ubuilder) writeVDS(at uint32, partLen uint32) {
	seq := uint32(0)
	next := func() uint32 { seq++; return seq - 1 }
	// PVD
	d := make([]byte, b.bs)
	binary.LittleEndian.PutUint32(d[16:], next())
	copy(d[24:], dstr(b.volID, 32))
	binary.LittleEndian.PutUint16(d[56:], 1)
	binary.LittleEndian.PutUint16(d[58:], 1)
	binary.LittleEndian.PutUint16(d[60:], 2)
	binary.LittleEndian.PutUint16(d[62:], 3)
	binary.LittleEndian.PutUint32(d[64:], 1)
	binary.LittleEndian.PutUint32(d[68:], 1)
	copy(d[72:], dstr("Test Volume Set", 128))
	copy(d[200+1:], "OSTA Compressed Unicode")
	copy(d[264+1:], "OSTA Compressed Unicode")
	copy(d[344:], regidBytes("*go-aaru test app"))
	copy(d[376:], ts(testCTime))
	copy(d[388:], b.udfRegid("*go-aaru test"))
	if b.pvdHook != nil {
		b.pvdHook(d)
	}
	b.setTag(d, tagPVD, at, 512-16)
	b.put(at, d)
	// IUVD
	d = make([]byte, b.bs)
	binary.LittleEndian.PutUint32(d[16:], next())
	copy(d[20:], b.udfRegid("*UDF LV Info"))
	b.setTag(d, tagIUVD, at+1, 512-16)
	b.put(at+1, d)
	// PD
	d = make([]byte, b.bs)
	binary.LittleEndian.PutUint32(d[16:], next())
	binary.LittleEndian.PutUint16(d[20:], 1)
	nsr := "+NSR03"
	if b.rev < 0x0200 {
		nsr = "+NSR02"
	}
	copy(d[24:], regidBytes(nsr))
	access := uint32(AccessOverwritable)
	if b.kind == kindVAT150 || b.kind == kindVAT200 {
		access = AccessWriteOnce
	}
	binary.LittleEndian.PutUint32(d[184:], access)
	binary.LittleEndian.PutUint32(d[188:], b.partStart)
	binary.LittleEndian.PutUint32(d[192:], partLen)
	copy(d[196:], b.udfRegid("*go-aaru test"))
	if b.pdHook != nil {
		b.pdHook(d)
	}
	b.setTag(d, tagPD, at+2, 512-16)
	b.put(at+2, d)
	// LVD
	maps, nmaps := b.partitionMaps()
	d = make([]byte, max(b.bs, 440+len(maps)))
	binary.LittleEndian.PutUint32(d[16:], next())
	copy(d[20+1:], "OSTA Compressed Unicode")
	copy(d[84:], dstr(b.lvID, 128))
	binary.LittleEndian.PutUint32(d[212:], uint32(b.bs))
	copy(d[216:], b.domainID())
	copy(d[248:], longADBytes(uint32(2*b.bs), 0, b.fsdBlock, b.fs.ref))
	binary.LittleEndian.PutUint32(d[264:], uint32(len(maps)))
	binary.LittleEndian.PutUint32(d[268:], uint32(nmaps))
	copy(d[272:], b.udfRegid("*go-aaru test"))
	binary.LittleEndian.PutUint32(d[432:], uint32(2*b.bs))
	binary.LittleEndian.PutUint32(d[436:], 128)
	copy(d[440:], maps)
	if b.lvdHook != nil {
		b.lvdHook(d)
	}
	b.setTag(d, tagLVD, at+3, 440+len(maps)-16)
	b.put(at+3, d)
	// USD and TD
	d = make([]byte, b.bs)
	binary.LittleEndian.PutUint32(d[16:], next())
	b.setTag(d, tagUSD, at+4, 8)
	b.put(at+4, d)
	d = make([]byte, b.bs)
	b.setTag(d, tagTD, at+5, 512-16)
	b.put(at+5, d)
	b.grow(int(at+16) * b.bs)
}

func (b *ubuilder) writeLVID() {
	d := make([]byte, b.bs)
	copy(d[16:], ts(testCTime))
	if !b.lvidOpen {
		binary.LittleEndian.PutUint32(d[28:], 1)
	}
	if b.lvidNext != nil {
		binary.LittleEndian.PutUint32(d[32:], b.lvidNext.length)
		binary.LittleEndian.PutUint32(d[36:], b.lvidNext.loc)
	}
	binary.LittleEndian.PutUint64(d[40:], b.uniq+1)
	_, nmaps := b.partitionMaps()
	binary.LittleEndian.PutUint32(d[72:], uint32(nmaps))
	binary.LittleEndian.PutUint32(d[76:], 46)
	iu := 80 + 8*nmaps
	copy(d[iu:], b.udfRegid("*go-aaru test"))
	binary.LittleEndian.PutUint32(d[iu+32:], b.files)
	binary.LittleEndian.PutUint32(d[iu+36:], b.dirs)
	binary.LittleEndian.PutUint16(d[iu+40:], b.rev)
	binary.LittleEndian.PutUint16(d[iu+42:], b.rev)
	binary.LittleEndian.PutUint16(d[iu+44:], b.rev)
	if b.lvidHook != nil {
		b.lvidHook(d)
	}
	b.setTag(d, tagLVID, 128, iu+46-16)
	b.put(128, d)
	td := make([]byte, b.bs)
	b.setTag(td, tagTD, 129, 512-16)
	b.put(129, td)
}

func (b *ubuilder) writeAVDP(at uint32) {
	d := make([]byte, b.bs)
	binary.LittleEndian.PutUint32(d[16:], uint32(16*b.bs))
	binary.LittleEndian.PutUint32(d[20:], b.vdsMain)
	binary.LittleEndian.PutUint32(d[24:], uint32(16*b.bs))
	binary.LittleEndian.PutUint32(d[28:], b.vdsRes)
	b.setTag(d, tagAVDP, at, 512-16)
	b.put(at, d)
}
