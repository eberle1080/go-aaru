package iso9660

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/eberle1080/go-aaru/console/ps3/sfo"
)

// isoNode is a file (data) or directory (children) for buildVolume.
type isoNode struct {
	name     string // on-disc identifier; files normally carry a ";1" suffix
	data     []byte
	children []*isoNode
	dir      bool
}

func isoFile(name string, data []byte) *isoNode { return &isoNode{name: name, data: data} }

func isoDir(name string, children ...*isoNode) *isoNode {
	return &isoNode{name: name, dir: true, children: children}
}

// dirRecord encodes one ISO 9660 directory record.
func dirRecord(lba, size uint32, flags byte, id string) []byte {
	l := 33 + len(id)
	if l%2 == 1 {
		l++
	}

	r := make([]byte, l)
	r[0] = byte(l)
	binary.LittleEndian.PutUint32(r[2:], lba)
	binary.BigEndian.PutUint32(r[6:], lba)
	binary.LittleEndian.PutUint32(r[10:], size)
	binary.BigEndian.PutUint32(r[14:], size)
	r[25] = flags
	r[32] = byte(len(id))
	copy(r[33:], id)

	return r
}

// buildVolume lays out a plain ISO 9660 volume: system area, primary volume
// descriptor at sector 16, terminator at 17, then directory extents and file
// data. Directory records never straddle a sector boundary, so large
// directories span several sectors.
func buildVolume(root *isoNode) []byte {
	type extent struct{ lba, size uint32 }

	ext := map[*isoNode]extent{}
	next := uint32(18)

	sectors := func(n int) uint32 { return uint32((n + SectorSize - 1) / SectorSize) }

	// dirLayout returns the records of a directory split into sector-sized
	// pages. Page count depends only on record lengths, so it can be called
	// before extents are assigned; the final call must come after.
	dirLayout := func(n, parent *isoNode) [][]byte {
		recs := [][]byte{
			dirRecord(ext[n].lba, ext[n].size, 2, "\x00"),
			dirRecord(ext[parent].lba, ext[parent].size, 2, "\x01"),
		}

		for _, c := range n.children {
			var flags byte
			if c.dir {
				flags = 2
			}

			recs = append(recs, dirRecord(ext[c].lba, ext[c].size, flags, c.name))
		}

		var pages [][]byte

		cur := []byte{}

		for _, r := range recs {
			if len(cur)+len(r) > SectorSize {
				pages = append(pages, cur)
				cur = []byte{}
			}

			cur = append(cur, r...)
		}

		return append(pages, cur)
	}

	// Assign directory extents first (sizes depend only on record lengths),
	// then file data.
	var dirs, files []*isoNode

	parents := map[*isoNode]*isoNode{root: root}

	var walk func(n *isoNode)

	walk = func(n *isoNode) {
		if n.dir {
			dirs = append(dirs, n)

			for _, c := range n.children {
				parents[c] = n
				walk(c)
			}
		} else {
			files = append(files, n)
		}
	}
	walk(root)

	for _, d := range dirs {
		size := uint32(len(dirLayout(d, parents[d]))) * SectorSize
		ext[d] = extent{next, size}
		next += size / SectorSize
	}

	for _, f := range files {
		ext[f] = extent{next, uint32(len(f.data))}
		next += max(sectors(len(f.data)), 1)
	}

	img := make([]byte, int(next)*SectorSize)

	pvd := img[16*SectorSize:]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	pvd[6] = 1
	copy(pvd[pvdRootDirOffset:], dirRecord(ext[root].lba, ext[root].size, 2, "\x00"))

	term := img[17*SectorSize:]
	term[0] = 0xFF
	copy(term[1:], "CD001")
	term[6] = 1

	for _, d := range dirs {
		for i, page := range dirLayout(d, parents[d]) {
			copy(img[(int(ext[d].lba)+i)*SectorSize:], page)
		}
	}

	for _, f := range files {
		copy(img[int(ext[f].lba)*SectorSize:], f.data)
	}

	return img
}

// patterned returns n bytes derived from tag, distinct per tag.
func patterned(n int, tag byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) ^ tag
	}

	return b
}

// buildParamSFO builds a PARAM.SFO with string parameters (sorted by key),
// in the layout sfo.Parse reads.
func buildParamSFO(params map[string]string) []byte {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var keyTab, dataTab []byte

	index := make([]byte, 16*len(keys))

	for i, k := range keys {
		v := []byte(params[k])
		idx := index[16*i:]
		binary.LittleEndian.PutUint16(idx, uint16(len(keyTab)))
		binary.LittleEndian.PutUint16(idx[2:], sfo.FormatUTF8)
		binary.LittleEndian.PutUint32(idx[4:], uint32(len(v)))
		binary.LittleEndian.PutUint32(idx[8:], uint32(len(v)))
		binary.LittleEndian.PutUint32(idx[12:], uint32(len(dataTab)))

		keyTab = append(append(keyTab, k...), 0)
		dataTab = append(dataTab, v...)
	}

	hdr := make([]byte, 20)
	binary.LittleEndian.PutUint32(hdr, sfo.Magic)
	binary.LittleEndian.PutUint32(hdr[4:], 0x101)
	binary.LittleEndian.PutUint32(hdr[8:], uint32(20+len(index)))
	binary.LittleEndian.PutUint32(hdr[12:], uint32(20+len(index)+len(keyTab)))
	binary.LittleEndian.PutUint32(hdr[16:], uint32(len(keys)))

	out := append(hdr, index...)
	out = append(out, keyTab...)

	return append(out, dataTab...)
}

// ps3Params are the PARAM.SFO values of the PS3 layout volume. DETAIL is long
// enough to make PARAM.SFO span two sectors.
var ps3Params = map[string]string{
	"TITLE":    "Test Game Title",
	"TITLE_ID": "BLES99999",
	"CATEGORY": "DG",
	"DETAIL":   strings.Repeat("D", 2500),
}

// ps3Volume builds a PS3 disc layout:
//
//	/PS3_DISC.SFB
//	/F00.DAT .. /F59.DAT   (fillers: the root directory spans two sectors)
//	/PS3_GAME/ICON0.PNG    (3000 bytes)
//	/PS3_GAME/PARAM.SFO
//	/PS3_GAME/USRDIR/EBOOT.BIN
func ps3Volume() (img, paramSFO []byte) {
	paramSFO = buildParamSFO(ps3Params)

	root := isoDir("\x00", isoFile("PS3_DISC.SFB;1", patterned(64, 1)))

	for i := 0; i < 60; i++ {
		root.children = append(root.children, isoFile(fmt.Sprintf("F%02d.DAT;1", i), patterned(10, byte(i))))
	}

	root.children = append(root.children, isoDir("PS3_GAME",
		isoFile("ICON0.PNG;1", patterned(3000, 0x55)),
		isoFile("PARAM.SFO;1", paramSFO),
		isoDir("USRDIR", isoFile("EBOOT.BIN;1", patterned(100, 0xEB))),
	))

	return buildVolume(root), paramSFO
}
