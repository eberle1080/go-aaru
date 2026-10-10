package iso9660

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// A byte-level ISO 9660 image builder for the tests. It lays out a volume
// descriptor set at sectors 16-23 (PVD, optional Joliet SVD, optional boot
// record, terminator), then directories (PVD tree, then Joliet tree), then
// file data. Directory records never straddle a sector boundary, so large
// directories span several sectors with zero padding. Path tables are not
// written: the reader must not need them.

// node is a file or directory of a test volume.
type node struct {
	id    string // ISO 9660 identifier as recorded ("NAME.EXT;1", "DIR")
	jname string // Joliet identifier; "" derives one from id
	dir   bool
	data  []byte
	kids  []*node

	flags byte // extra record flags (hidden, associated, ...)
	date  [7]byte
	// su returns the system use bytes of this entry's record in its parent
	// (PVD tree). It is called during layout (LBAs not yet known) and when
	// writing; the length must not change.
	su func(b *builder) []byte
	// dotSU / dotdotSU: system use of a directory's "." and ".." records.
	dotSU    func(b *builder) []byte
	dotdotSU func(b *builder) []byte
	// fix patches this entry's encoded encRecord(s) in the PVD tree.
	fix func(b *builder, rec []byte)

	ear       uint8 // extended attribute record blocks
	unit, gap uint8 // interleaving
	split     []int // multi-extent: data split into extents of these sizes
	noJoliet  bool

	// Assigned by layout.
	lba    []uint32 // file extents (one per split part) or dir extent
	size   uint32   // directory size
	jlba   uint32
	jsize  uint32
	parent *node
}

func file(id string, data []byte) *node { return &node{id: id, data: data} }

func dir(id string, kids ...*node) *node { return &node{id: id, dir: true, kids: kids} }

type builder struct {
	img  []byte
	next uint32

	joliet bool
	boot   bool

	sysID, volID string
	jVolID       string
	dates        [4][17]byte // creation, modification, expiration, effective
	pvdHook      func(pvd []byte)
	svdHook      func(svd []byte)
	blockSize    uint16
}

func newBuilder() *builder {
	b := &builder{next: 24, sysID: "TESTSYS", volID: "TESTVOL", jVolID: "Test Volume", blockSize: 2048}
	for i := range b.dates {
		copy(b.dates[i][:], "0000000000000000")
	}
	copy(b.dates[0][:], "2001020304050600")
	b.dates[0][16] = 4 // GMT+1
	return b
}

func (b *builder) grow(end int) {
	if len(b.img) < end {
		b.img = append(b.img, make([]byte, end-len(b.img))...)
	}
}

// alloc reserves n sectors and returns the first.
func (b *builder) alloc(n int) uint32 {
	if n == 0 {
		n = 1 // give empty extents a distinct, valid address
	}
	l := b.next
	b.next += uint32(n)
	b.grow(int(b.next) * SectorSize)
	return l
}

// put writes data at sector lba (plus off bytes).
func (b *builder) put(lba uint32, off int, data []byte) {
	start := int(lba)*SectorSize + off
	b.grow(start + len(data))
	copy(b.img[start:], data)
}

// reserve allocates sectors holding data and returns the first.
func (b *builder) reserve(data []byte) uint32 {
	l := b.alloc((len(data) + SectorSize - 1) / SectorSize)
	b.put(l, 0, data)
	return l
}

func sectors(n int) int { return (n + SectorSize - 1) / SectorSize }

var defaultDate = [7]byte{99, 12, 31, 23, 59, 58, 0}

// encRecord encodes a directory record.
func encRecord(lba, size uint32, date [7]byte, flags byte, ear, unit, gap byte, id []byte, su []byte) []byte {
	l := 33 + len(id)
	if len(id)%2 == 0 {
		l++
	}
	l += len(su)
	if l%2 == 1 {
		l++
	}
	r := make([]byte, l)
	r[0] = byte(l)
	r[1] = ear
	binary.LittleEndian.PutUint32(r[2:], lba)
	binary.BigEndian.PutUint32(r[6:], lba)
	binary.LittleEndian.PutUint32(r[10:], size)
	binary.BigEndian.PutUint32(r[14:], size)
	copy(r[18:25], date[:])
	r[25] = flags
	r[26] = unit
	r[27] = gap
	binary.LittleEndian.PutUint16(r[28:], 1)
	binary.BigEndian.PutUint16(r[30:], 1)
	r[32] = byte(len(id))
	copy(r[33:], id)
	suStart := 33 + len(id)
	if len(id)%2 == 0 {
		suStart++
	}
	copy(r[suStart:], su)
	return r
}

func ucs2be(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(u))
	for i, c := range u {
		binary.BigEndian.PutUint16(out[2*i:], c)
	}
	return out
}

func (n *node) joliet() string {
	if n.jname != "" {
		return n.jname
	}
	return strings.ToLower(n.id)
}

func call(f func(*builder) []byte, b *builder) []byte {
	if f == nil {
		return nil
	}
	return f(b)
}

func (n *node) dateOr() [7]byte {
	if n.date == ([7]byte{}) {
		return defaultDate
	}
	return n.date
}

// fileExtents returns the (lba, length) of each extent of a file node.
func (n *node) parts() []int {
	if len(n.split) > 0 {
		return n.split
	}
	return []int{len(n.data)}
}

// records returns the encoded records of directory d in the given tree.
func (b *builder) records(d *node, joliet bool) [][]byte {
	selfLBA, selfSize := uint32(0), uint32(0)
	parLBA, parSize := uint32(0), uint32(0)
	p := d.parent
	if p == nil {
		p = d
	}
	if joliet {
		selfLBA, selfSize, parLBA, parSize = d.jlba, d.jsize, p.jlba, p.jsize
	} else if len(d.lba) > 0 && len(p.lba) > 0 {
		selfLBA, selfSize, parLBA, parSize = d.lba[0], d.size, p.lba[0], p.size
	}
	var dotSU, dotdotSU []byte
	if !joliet {
		dotSU, dotdotSU = call(d.dotSU, b), call(d.dotdotSU, b)
	}
	recs := [][]byte{
		encRecord(selfLBA, selfSize, d.dateOr(), FlagDirectory, 0, 0, 0, []byte{0}, dotSU),
		encRecord(parLBA, parSize, d.dateOr(), FlagDirectory, 0, 0, 0, []byte{1}, dotdotSU),
	}
	for _, c := range d.kids {
		if joliet && c.noJoliet {
			continue
		}
		id := []byte(c.id)
		var su []byte
		if joliet {
			id = ucs2be(c.joliet())
		} else {
			su = call(c.su, b)
		}
		if c.dir {
			lba, size := uint32(0), uint32(0)
			if joliet {
				lba, size = c.jlba, c.jsize
			} else if len(c.lba) > 0 {
				lba, size = c.lba[0], c.size
			}
			r := encRecord(lba, size, c.dateOr(), FlagDirectory|c.flags, 0, 0, 0, id, su)
			if !joliet && c.fix != nil {
				c.fix(b, r)
			}
			recs = append(recs, r)
			continue
		}
		parts := c.parts()
		for i, sz := range parts {
			fl := c.flags
			if i < len(parts)-1 {
				fl |= FlagMultiExtent
			}
			lba := uint32(0)
			if i < len(c.lba) {
				lba = c.lba[i]
			}
			ear := byte(0)
			if i == 0 {
				ear = c.ear
			}
			r := encRecord(lba, uint32(sz), c.dateOr(), fl, ear, c.unit, c.gap, id, su)
			if !joliet && c.fix != nil {
				c.fix(b, r)
			}
			recs = append(recs, r)
		}
	}
	return recs
}

// paginate places records into sectors without straddling boundaries.
func paginate(recs [][]byte) []byte {
	var out []byte
	for _, r := range recs {
		if len(out)/SectorSize != (len(out)+len(r)-1)/SectorSize {
			out = append(out, make([]byte, SectorSize-len(out)%SectorSize)...)
		}
		out = append(out, r...)
	}
	return out
}

func walkNodes(n *node, fn func(*node)) {
	fn(n)
	for _, c := range n.kids {
		c.parent = n
		walkNodes(c, fn)
	}
}

// build lays out the volume and returns its bytes.
func (b *builder) build(root *node) []byte {
	walkNodes(root, func(*node) {}) // set parents
	// Directory extents (sizes depend only on record lengths).
	walkNodes(root, func(n *node) {
		if n.dir {
			data := paginate(b.records(n, false))
			n.size = uint32(sectors(len(data)) * SectorSize)
			n.lba = []uint32{b.alloc(sectors(len(data)))}
		}
	})
	if b.joliet {
		walkNodes(root, func(n *node) {
			if n.dir && !n.noJoliet {
				data := paginate(b.records(n, true))
				n.jsize = uint32(sectors(len(data)) * SectorSize)
				n.jlba = b.alloc(sectors(len(data)))
			}
		})
	}
	// File data.
	walkNodes(root, func(n *node) {
		if n.dir {
			return
		}
		data := n.data
		for i, sz := range n.parts() {
			chunk := data[:sz]
			data = data[sz:]
			ear := 0
			if i == 0 {
				ear = int(n.ear)
			}
			blocks := sectors(len(chunk))
			if n.unit > 0 && blocks > 0 {
				u, g := int(n.unit), int(n.gap)
				units := (blocks + u - 1) / u
				phys := (units-1)*(u+g) + blocks - (units-1)*u
				lba := b.alloc(ear + phys)
				for j := 0; j < units*(u+g) && j < phys; j++ {
					b.put(lba+uint32(ear+j), 0, bytes.Repeat([]byte{0xEE}, SectorSize))
				}
				for k := 0; k < units; k++ {
					lo := k * u * SectorSize
					hi := min(lo+u*SectorSize, len(chunk))
					b.put(lba+uint32(ear+k*(u+g)), 0, chunk[lo:hi])
				}
				n.lba = append(n.lba, lba)
			} else {
				lba := b.alloc(ear + blocks)
				b.put(lba+uint32(ear), 0, chunk)
				n.lba = append(n.lba, lba)
			}
			if ear > 0 {
				b.put(n.lba[i], 0, bytes.Repeat([]byte{0xEA}, ear*SectorSize))
			}
		}
	})
	// Directory contents.
	walkNodes(root, func(n *node) {
		if !n.dir {
			return
		}
		b.put(n.lba[0], 0, paginate(b.records(n, false)))
		if b.joliet && !n.noJoliet {
			b.put(n.jlba, 0, paginate(b.records(n, true)))
		}
	})
	b.grow(int(b.next) * SectorSize)

	// Volume descriptors.
	sec := uint32(16)
	pvd := b.descriptor(1, root.lba[0], root.size, root.dateOr(), false)
	if b.pvdHook != nil {
		b.pvdHook(pvd)
	}
	b.put(sec, 0, pvd)
	sec++
	if b.joliet {
		svd := b.descriptor(2, root.jlba, root.jsize, root.dateOr(), true)
		if b.svdHook != nil {
			b.svdHook(svd)
		}
		b.put(sec, 0, svd)
		sec++
	}
	if b.boot {
		br := make([]byte, SectorSize)
		br[0] = 0
		copy(br[1:], "CD001\x01")
		copy(br[7:], "EL TORITO SPECIFICATION")
		binary.LittleEndian.PutUint32(br[71:], 1234)
		b.put(sec, 0, br)
		sec++
	}
	term := make([]byte, SectorSize)
	term[0] = 255
	copy(term[1:], "CD001\x01")
	b.put(sec, 0, term)
	return b.img
}

func pad(s string, n int, c byte) []byte {
	out := bytes.Repeat([]byte{c}, n)
	copy(out, s)
	return out
}

func padUCS2(s string, n int) []byte {
	out := make([]byte, n)
	for i := 0; i+1 < n; i += 2 {
		out[i], out[i+1] = 0, ' '
	}
	copy(out, ucs2be(s))
	return out
}

func (b *builder) descriptor(typ byte, rootLBA, rootSize uint32, rootDate [7]byte, joliet bool) []byte {
	d := make([]byte, SectorSize)
	d[0] = typ
	copy(d[1:], "CD001\x01")
	str := func(off, n int, s string) {
		if joliet {
			copy(d[off:], padUCS2(s, n))
		} else {
			copy(d[off:], pad(s, n, ' '))
		}
	}
	str(8, 32, b.sysID)
	if joliet {
		str(40, 32, b.jVolID)
		copy(d[88:], "%/E")
	} else {
		str(40, 32, b.volID)
	}
	both32 := func(off int, v uint32) {
		binary.LittleEndian.PutUint32(d[off:], v)
		binary.BigEndian.PutUint32(d[off+4:], v)
	}
	both16 := func(off int, v uint16) {
		binary.LittleEndian.PutUint16(d[off:], v)
		binary.BigEndian.PutUint16(d[off+2:], v)
	}
	both32(80, b.next)
	both16(120, 1)
	both16(124, 1)
	both16(128, b.blockSize)
	copy(d[156:], encRecord(rootLBA, rootSize, rootDate, FlagDirectory, 0, 0, 0, []byte{0}, nil))
	for _, f := range []int{190, 318, 446, 574} {
		str(f, 128, "")
	}
	str(574, 128, "GO-AARU TEST")
	for _, f := range []int{702, 739, 776} {
		str(f, 37, "")
	}
	for i, off := range []int{813, 830, 847, 864} {
		copy(d[off:], b.dates[i][:])
	}
	d[881] = 1
	return d
}

// ---- SUSP / Rock Ridge entry encoders ----

func suspEntry(sig string, ver byte, parts ...[]byte) []byte {
	var d []byte
	for _, p := range parts {
		d = append(d, p...)
	}
	return append([]byte{sig[0], sig[1], byte(4 + len(d)), ver}, d...)
}

func both32(v uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, v)
	binary.BigEndian.PutUint32(b[4:], v)
	return b
}

func spEntry(skip byte) []byte { return suspEntry("SP", 1, []byte{0xBE, 0xEF, skip}) }

func erEntry(id, des, src string) []byte {
	return suspEntry("ER", 1, []byte{byte(len(id)), byte(len(des)), byte(len(src)), 1}, []byte(id+des+src))
}

func pxEntry(mode, nlink, uid, gid uint32, ino ...uint32) []byte {
	d := [][]byte{both32(mode), both32(nlink), both32(uid), both32(gid)}
	for _, i := range ino {
		d = append(d, both32(i))
	}
	return suspEntry("PX", 1, d...)
}

func nmEntry(flags byte, name string) []byte { return suspEntry("NM", 1, []byte{flags}, []byte(name)) }

type slComp struct {
	flags byte
	text  string
}

func slEntry(flags byte, comps ...slComp) []byte {
	d := []byte{flags}
	for _, c := range comps {
		d = append(d, c.flags, byte(len(c.text)))
		d = append(d, c.text...)
	}
	return suspEntry("SL", 1, d)
}

func ceEntry(lba, off, length uint32) []byte {
	return suspEntry("CE", 1, both32(lba), both32(off), both32(length))
}

func tfEntry(flags byte, stamps ...[]byte) []byte {
	return suspEntry("TF", 1, append([]byte{flags}, bytes.Join(stamps, nil)...))
}

func clEntry(lba uint32) []byte { return suspEntry("CL", 1, both32(lba)) }
func plEntry(lba uint32) []byte { return suspEntry("PL", 1, both32(lba)) }
func reEntry() []byte           { return suspEntry("RE", 1) }
func pnEntry(hi, lo uint32) []byte {
	return suspEntry("PN", 1, both32(hi), both32(lo))
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// rrRoot returns system use for the root "." record: SP and the RRIP ER.
func rrRoot(extra ...[]byte) func(*builder) []byte {
	return func(*builder) []byte {
		return cat(append([][]byte{spEntry(0), erEntry("RRIP_1991A", "ROCK RIDGE", "SRC")}, extra...)...)
	}
}

func fixed(b []byte) func(*builder) []byte { return func(*builder) []byte { return b } }
