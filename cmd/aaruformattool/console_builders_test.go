package main

// Builders of small synthetic console disc images for the convert-ps3,
// convert-wiiu and convert-ngcw tests.

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/ngcw"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/console/wii"
	"github.com/eberle1080/go-aaru/console/wiiu"
	"github.com/eberle1080/go-aaru/media"
)

func conTestRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)) }

func conFillRandom(r *rand.Rand, b []byte) {
	for i := range b {
		b[i] = byte(r.Uint32())
	}
}

func conRandomKey(r *rand.Rand) (k [16]byte) {
	conFillRandom(r, k[:])
	return k
}

func conRandomSeed(r *rand.Rand) (s ngcw.Seed) {
	for i := range s {
		s[i] = r.Uint32()
	}
	return s
}

// conAESCBCEncrypt encrypts data (a multiple of 16 bytes) in place.
func conAESCBCEncrypt(key [16]byte, iv [16]byte, data []byte) {
	block, _ := aes.NewCipher(key[:])
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(data, data)
}

// ---- PS3 ----

// ps3TestDisc is a synthetic PS3 disc: the conPS3FileSystem ISO 9660 file
// system (with /PS3_GAME/PARAM.SFO) in a plaintext region, the "PlayStation3" marker in
// sector 1, an encryption map in sector 0 and encrypted sectors whose
// plaintext is random or (every third sector, or all of them with
// zeroPlain) all zeros.
type ps3TestDisc struct {
	iso     []byte
	discKey [16]byte
	regions ps3.EncryptionMap
}

const ps3TestSectors = 160

func buildPS3Disc(t *testing.T, discKey [16]byte, zeroPlain bool) *ps3TestDisc {
	t.Helper()
	base := conPS3FileSystem()
	r := conTestRand(3)

	iso := make([]byte, ps3TestSectors*conSectorSize)
	copy(iso, base)

	regions := ps3.EncryptionMap{{StartSector: 0, EndSector: uint32(len(base)/conSectorSize - 1)}, {StartSector: 60,
		EndSector: 67}}

	// Sector 0: big-endian encryption map.
	s0 := iso[:conSectorSize]
	clear(s0)
	binary.BigEndian.PutUint32(s0, uint32(len(regions)))
	for i, rg := range regions {
		binary.BigEndian.PutUint32(s0[8+i*8:], rg.StartSector)
		binary.BigEndian.PutUint32(s0[12+i*8:], rg.EndSector)
	}
	// Sector 1: identification.
	copy(iso[conSectorSize:], "PlayStation3\x00\x00\x00\x00BLES-00905 ")

	for s := uint64(len(base) / conSectorSize); s < ps3TestSectors; s++ {
		sec := iso[s*conSectorSize : (s+1)*conSectorSize]
		if !regions.IsSectorEncrypted(s) {
			conFillRandom(r, sec) // plaintext region
			continue
		}
		if s%3 != 0 && !zeroPlain {
			conFillRandom(r, sec)
		} else {
			clear(sec)
		}
		_ = ps3.EncryptSector(discKey, s, sec)
	}
	return &ps3TestDisc{iso: iso, discKey: discKey, regions: regions}
}

// conPS3Title and conPS3TitleID are the TITLE and TITLE_ID of the PARAM.SFO
// in conPS3FileSystem.
const (
	conPS3Title   = "Test Game Title"
	conPS3TitleID = "BLES00905"
)

// conParamSFO builds a PARAM.SFO: a 20-byte header (magic, version, key
// table offset, data table offset, entry count), 16-byte index entries (key
// offset u16, format u16, length u32, reserved length u32, data offset u32),
// the NUL-terminated keys padded to 4 bytes, then the values.
func conParamSFO(entries [][2]string) []byte {
	var keys, values []byte
	index := make([]byte, 16*len(entries))
	for i, e := range entries {
		v := append([]byte(e[1]), 0)
		reserved := (len(v) + 3) &^ 3
		idx := index[16*i:]
		binary.LittleEndian.PutUint16(idx, uint16(len(keys)))
		binary.LittleEndian.PutUint16(idx[2:], 0x0204) // NUL-terminated UTF-8
		binary.LittleEndian.PutUint32(idx[4:], uint32(len(v)))
		binary.LittleEndian.PutUint32(idx[8:], uint32(reserved))
		binary.LittleEndian.PutUint32(idx[12:], uint32(len(values)))
		keys = append(append(keys, e[0]...), 0)
		values = append(values, v...)
		values = append(values, make([]byte, reserved-len(v))...)
	}
	for len(keys)%4 != 0 {
		keys = append(keys, 0)
	}
	keyTable := 20 + len(index)
	out := make([]byte, 20, keyTable+len(keys)+len(values))
	binary.LittleEndian.PutUint32(out, 0x46535000) // "\x00PSF"
	binary.LittleEndian.PutUint32(out[4:], 0x101)
	binary.LittleEndian.PutUint32(out[8:], uint32(keyTable))
	binary.LittleEndian.PutUint32(out[12:], uint32(keyTable+len(keys)))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(entries)))
	out = append(append(out, index...), keys...)
	return append(out, values...)
}

// conISODirRecord encodes an ISO 9660 directory record.
func conISODirRecord(lba, size uint32, dir bool, id string) []byte {
	l := 33 + len(id)
	l += l % 2
	r := make([]byte, l)
	r[0] = byte(l)
	binary.LittleEndian.PutUint32(r[2:], lba)
	binary.BigEndian.PutUint32(r[6:], lba)
	binary.LittleEndian.PutUint32(r[10:], size)
	binary.BigEndian.PutUint32(r[14:], size)
	if dir {
		r[25] = 2
	}
	r[32] = byte(len(id))
	copy(r[33:], id)
	return r
}

// conPS3FileSystem returns a 21-sector ISO 9660 volume: the primary volume
// descriptor in sector 16, the terminator in 17, the root directory in 18,
// /PS3_GAME in 19 and /PS3_GAME/PARAM.SFO in 20. Sectors 0 and 1 (the
// system area) are left for the encryption map and the PS3 identification.
func conPS3FileSystem() []byte {
	const ss = conSectorSize
	const rootLBA, gameLBA, sfoLBA = 18, 19, 20
	sfo := conParamSFO([][2]string{{"CATEGORY", "DG"}, {"TITLE", conPS3Title}, {"TITLE_ID", conPS3TitleID},
		{"VERSION", "01.00"}})
	iso := make([]byte, 21*ss)

	pvd := iso[16*ss:]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	pvd[6] = 1
	copy(pvd[40:], "PS3VOLUME")
	copy(pvd[156:], conISODirRecord(rootLBA, ss, true, "\x00"))
	term := iso[17*ss:]
	term[0] = 0xFF
	copy(term[1:], "CD001")
	term[6] = 1

	dir := func(lba uint32, recs ...[]byte) {
		off := int(lba) * ss
		for _, r := range recs {
			off += copy(iso[off:], r)
		}
	}
	dir(rootLBA, conISODirRecord(rootLBA, ss, true, "\x00"), conISODirRecord(rootLBA, ss, true, "\x01"),
		conISODirRecord(gameLBA, ss, true, "PS3_GAME"))
	dir(gameLBA, conISODirRecord(gameLBA, ss, true, "\x00"), conISODirRecord(rootLBA, ss, true, "\x01"),
		conISODirRecord(sfoLBA, uint32(len(sfo)), false, "PARAM.SFO;1"))
	copy(iso[sfoLBA*ss:], sfo)
	return iso
}

// conIRD is a synthetic IRD file written by conWriteIRD.
type conIRD struct {
	path     string
	gameName string
	data1    [16]byte
	data2    [16]byte
	pic      [115]byte
}

// conWriteIRD writes a version 9 IRD file for game BLES00905 to dir: magic
// "3IRD", version, 9-byte game ID, a 7-bit length prefixed game name, the
// update/game/app versions, the gzip-compressed header and footer blobs
// (uint32 length each), region hashes (count byte + 16 bytes each), file
// entries (uint32 count + 24 bytes each), 4 bytes of extra config and
// attachments, the PIC, data1, data2, then the UID and CRC32 fields.
func conWriteIRD(t *testing.T, dir string) conIRD {
	t.Helper()
	r := conTestRand(41)
	d := conIRD{path: filepath.Join(dir, "BLES-00905.ird"), gameName: "Synthetic Test Game",
		data1: conRandomKey(r), data2: conRandomKey(r)}
	conFillRandom(r, d.pic[:])

	gz := func(data []byte) []byte {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		_, _ = w.Write(data)
		_ = w.Close()
		return b.Bytes()
	}
	blob := func(b []byte, data []byte) []byte {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
		return append(b, data...)
	}

	b := append([]byte("3IRD"), 9)
	b = append(b, conPS3TitleID...)
	b = append(b, byte(len(d.gameName)))
	b = append(b, d.gameName...)
	b = append(b, "4.00"...)
	b = append(b, "01.00"...)
	b = append(b, "01.00"...)
	b = blob(b, gz(conPS3FileSystem()[16*conSectorSize:]))
	b = blob(b, gz(make([]byte, conSectorSize)))
	region := make([]byte, 16)
	conFillRandom(r, region)
	b = append(append(b, 1), region...)
	file := make([]byte, 24)
	conFillRandom(r, file)
	b = append(binary.LittleEndian.AppendUint32(b, 1), file...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, d.pic[:]...)
	b = append(append(b, d.data1[:]...), d.data2[:]...)
	b = append(b, 0x11, 0x22, 0x33, 0x44) // UID
	b = append(b, 0, 0, 0, 0)             // CRC32 (not checked)
	conWriteFile(t, d.path, b)
	return d
}

// conMakeCDImage writes a small CD-ROM image (a media type no console
// converter accepts) to dir and returns its path.
func conMakeCDImage(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cdrom.aif")
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.CDROM, SectorSize: 2048, UserSectors: 16,
		Application: "contest"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := img.SetTracks([]aaru.TrackEntry{{Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: 15,
		Session: 1, Flags: 4}}); err != nil {
		t.Fatal(err)
	}
	for s := uint64(0); s < 16; s++ {
		if err := img.WriteSector(s, false, bytes.Repeat([]byte{byte(s)}, 2048),
			aaru.SectorStatusDumped); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- Wii U ----

type wiiuTestDisc struct {
	wud      []byte
	discKey  [16]byte
	titleKey [16]byte
}

const wiiuTestPhysical = 12

// buildWUD builds a 12-physical-sector Wii U disc: header (product code
// WUP-P-ABCE, disc 1), TOC in sector 3 (encrypted with the disc key) listing
// a GM partition at 9 (encrypted with a title key), an SI partition at 4
// (whose FST holds a TITLE.TIK carrying that title key) and a UP partition
// at 7.
func buildWUD(t *testing.T) *wiiuTestDisc {
	t.Helper()
	r := conTestRand(7)
	d := &wiiuTestDisc{discKey: conRandomKey(r), titleKey: conRandomKey(r)}
	titleID := [8]byte{0x00, 0x05, 0x00, 0x00, 0x10, 0x10, 0x1A, 0x00}

	const ps = wiiu.PhysicalSectorSize
	disc := make([]byte, wiiuTestPhysical*ps)
	conFillRandom(r, disc)
	sector := func(n int) []byte { return disc[n*ps : (n+1)*ps] }

	// Header.
	copy(disc, "WUP-P-ABCE\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	disc[0x15] = '0'

	// TOC (sector 3).
	toc := sector(3)
	binary.BigEndian.PutUint32(toc, wiiuTOCSignature)
	binary.BigEndian.PutUint32(toc[0x1C:], 3)
	for i, p := range []struct {
		id    string
		start uint32
	}{{"GM0005000010101A00", 9}, {"SI", 4}, {"UP", 7}} {
		e := toc[wiiuTOCEntriesOff+i*wiiuTOCEntrySize:][:wiiuTOCEntrySize]
		clear(e[:0x20])
		copy(e, p.id)
		binary.BigEndian.PutUint32(e[0x20:], p.start)
	}
	_ = wiiu.EncryptPhysicalSector(d.discKey, toc)

	// SI partition: header at 4, volume (FST + ticket) at 5-6.
	vol := disc[5*ps : 7*ps]
	clear(vol[:0x200])
	copy(vol, "FST\x00")
	binary.BigEndian.PutUint32(vol[4:], 0x20) // offset factor
	binary.BigEndian.PutUint32(vol[8:], 2)    // clusters
	binary.BigEndian.PutUint32(vol[0x20:], 1) // cluster 0 at volume 0
	binary.BigEndian.PutUint32(vol[0x40:], 2) // cluster 1 at volume 0x8000
	entries := vol[0x60:]
	entries[0] = 1 // root directory
	binary.BigEndian.PutUint32(entries[8:], 3)
	ent := entries[0x10:]
	binary.BigEndian.PutUint32(ent, 1) // file, name at 1
	binary.BigEndian.PutUint32(ent[4:], 0)
	binary.BigEndian.PutUint32(ent[8:], 0x350)
	binary.BigEndian.PutUint16(ent[0xE:], 1)
	ent = entries[0x20:]
	binary.BigEndian.PutUint32(ent, 11) // file, name at 11
	binary.BigEndian.PutUint32(ent[4:], 0x400>>5)
	binary.BigEndian.PutUint32(ent[8:], 0x400)
	binary.BigEndian.PutUint16(ent[0xE:], 0)
	copy(entries[0x30:], "\x00TITLE.TIK\x00title.tmd\x00")

	tik := vol[ps:]
	encKey := d.titleKey
	var iv [16]byte
	copy(iv[:], titleID[:])
	conAESCBCEncrypt(wiiuCommonKey, iv, encKey[:])
	copy(tik[0x1BF:], encKey[:])
	copy(tik[0x1DC:], titleID[:])

	for _, n := range []int{5, 6, 8} { // SI volume and UP data: disc key
		_ = wiiu.EncryptPhysicalSector(d.discKey, sector(n))
	}
	for _, n := range []int{10, 11} { // GM data: title key
		_ = wiiu.EncryptPhysicalSector(d.titleKey, sector(n))
	}
	d.wud = disc
	return d
}

// buildWUX packs a WUD into the WUX format (sector-level deduplication).
func buildWUX(wud []byte) []byte {
	const ps = wiiu.PhysicalSectorSize
	n := len(wud) / ps
	hdr := make([]byte, 0x20+4*n)
	binary.LittleEndian.PutUint32(hdr, wiiu.WUXMagic)
	binary.LittleEndian.PutUint32(hdr[8:], ps)
	binary.LittleEndian.PutUint64(hdr[16:], uint64(len(wud)))

	var stored [][]byte
	seen := map[string]uint32{}
	for i := 0; i < n; i++ {
		s := wud[i*ps : (i+1)*ps]
		idx, ok := seen[string(s)]
		if !ok {
			idx = uint32(len(stored))
			seen[string(s)] = idx
			stored = append(stored, s)
		}
		binary.LittleEndian.PutUint32(hdr[0x20+4*i:], idx)
	}
	out := make([]byte, (len(hdr)+ps-1)&^(ps-1))
	copy(out, hdr)
	for _, s := range stored {
		out = append(out, s...)
	}
	return out
}

// ---- GameCube / Wii ----

// conLFGFill writes LFG junk into disc[from:to): each 0x8000-byte block of the
// stream (positions relative to base) uses its own seed.
func conLFGFill(disc []byte, base, from, to uint64, seeds func(block uint64) ngcw.Seed) {
	for off := from; off < to; {
		blk := (off - base) / ngcw.JunkBlockSize
		end := min(to, base+(blk+1)*ngcw.JunkBlockSize)
		g := ngcw.NewLFG(seeds(blk))
		g.Discard((off - base) % ngcw.JunkBlockSize)
		_, _ = g.Read(disc[off:end])
		off = end
	}
}

type conFSTFile struct {
	name   string
	offset uint64 // byte offset (disc for GameCube, partition data for Wii)
	length uint32
}

// conBuildFST builds a flat FST (root directory plus files); file offsets are
// stored shifted right by shift.
func conBuildFST(files []conFSTFile, shift uint) []byte {
	n := len(files) + 1
	fst := make([]byte, n*12)
	fst[0] = 1
	binary.BigEndian.PutUint32(fst[8:], uint32(n))
	var names []byte
	for i, f := range files {
		e := fst[(i+1)*12:]
		binary.BigEndian.PutUint32(e, uint32(len(names))) // e[0] = 0: file
		binary.BigEndian.PutUint32(e[4:], uint32(f.offset>>shift))
		binary.BigEndian.PutUint32(e[8:], f.length)
		names = append(names, f.name...)
		names = append(names, 0)
	}
	return append(fst, names...)
}

// gcTestDiscSize is 12.5 junk blocks: the last block is partial.
const gcTestDiscSize = 0x64000

// buildGCDisc builds a GameCube disc: header (GALE01, disc 2, version 3),
// system area and FST up to sysEnd, three files (one crossing a block
// boundary, one ending mid-sector, one empty) and LFG junk everywhere else,
// except for an all-zero block (zero seed) and a block of non-LFG garbage.
func buildGCDisc(t *testing.T) []byte {
	t.Helper()
	r := conTestRand(11)
	disc := make([]byte, gcTestDiscSize)

	files := []conFSTFile{{"a.bin", 0x10000, 0x9000}, {"b.bin", 0x30000, 0x1801}, {"empty", 0x50000, 0}}
	fst := conBuildFST(files, 0)
	const fstOff = 0x2440
	sysEnd := uint64(fstOff + len(fst))

	seeds := make([]ngcw.Seed, gcTestDiscSize/ngcw.JunkBlockSize+1)
	for i := range seeds {
		seeds[i] = conRandomSeed(r)
	}
	conLFGFill(disc, 0, sysEnd, gcTestDiscSize, func(b uint64) ngcw.Seed { return seeds[b] })
	clear(disc[0x40000:0x48000])            // zero junk (zero seed)
	conFillRandom(r, disc[0x58000:0x58800]) // garbage in a junk area: kept as data

	// System area.
	conFillRandom(r, disc[:sysEnd])
	copy(disc, "GALE01")
	disc[6], disc[7] = 1, 3
	clear(disc[0x18:0x60])
	binary.BigEndian.PutUint32(disc[0x1C:], ngcwGCMagic)
	copy(disc[0x20:], "Test GameCube Title")
	binary.BigEndian.PutUint32(disc[0x424:], fstOff)
	binary.BigEndian.PutUint32(disc[0x428:], uint32(len(fst)))
	copy(disc[fstOff:], fst)

	for _, f := range files {
		conFillRandom(r, disc[f.offset:f.offset+uint64(f.length)])
	}
	return disc
}

// wiiTestPartition describes a partition of the synthetic Wii disc.
type wiiTestPartition struct {
	offset    uint64 // partition offset
	dataOff   uint64 // data offset relative to the partition
	groups    int
	typ       uint32
	keyIndex  byte
	titleKey  [16]byte
	titleID   [8]byte
	files     []conFSTFile // user data offsets
	withFST   bool
	userData  []byte
	junkSeeds []ngcw.Seed
}

const wiiTestDiscSize = 0xF0000

// buildWiiDisc builds a Wii disc with two partitions listed in reverse
// order of their data offsets: an update partition (common key, no FST: all
// data) and a game partition (Korean key) whose user data has a boot block,
// an FST, files and LFG junk (one seed per 0x8000-byte block of the
// partition's user data stream).
func buildWiiDisc(t *testing.T) ([]byte, []*wiiTestPartition) {
	t.Helper()
	r := conTestRand(13)
	disc := make([]byte, wiiTestDiscSize)
	conFillRandom(r, disc)

	copy(disc, "RSPE01")
	disc[6], disc[7] = 0, 1
	clear(disc[0x18:0x60])
	binary.BigEndian.PutUint32(disc[0x18:], ngcwWiiMagic)
	copy(disc[0x20:], "Test Wii Title")

	parts := []*wiiTestPartition{
		{offset: 0x90000, dataOff: 0x20000, groups: 6, typ: 0, keyIndex: 1, withFST: true,
			titleID: [8]byte{0, 1, 0, 0, 'R', 'S', 'P', 'E'},
			files:   []conFSTFile{{"main.dol", 0x9000, 0x5000}, {"small.bin", 0x20000, 0x100}, {"tail.bin", 0x2D000, 0x400}}},
		{offset: 0x50000, dataOff: 0x20000, groups: 4, typ: 1, keyIndex: 0,
			titleID: [8]byte{0, 1, 0, 0, 'U', 'P', 'D', 'T'}},
	}

	// Partition table: one partition in each of the first two tables.
	info := disc[ngcwPartTableInfo:]
	clear(info[:0x40])
	for i, p := range parts {
		binary.BigEndian.PutUint32(info[i*8:], 1)
		binary.BigEndian.PutUint32(info[i*8+4:], uint32((ngcwPartTableInfo+0x20+i*8)>>2))
		binary.BigEndian.PutUint32(info[0x20+i*8:], uint32(p.offset>>2))
		binary.BigEndian.PutUint32(info[0x20+i*8+4:], p.typ)
	}

	commonKeys := [2][16]byte{ngcwWiiCommonKey, ngcwWiiKoreanKey}
	for _, p := range parts {
		p.titleKey = conRandomKey(r)

		ticket := disc[p.offset:]
		enc := p.titleKey
		var iv [16]byte
		copy(iv[:], p.titleID[:])
		conAESCBCEncrypt(commonKeys[p.keyIndex], iv, enc[:])
		copy(ticket[0x1BF:], enc[:])
		copy(ticket[0x1DC:], p.titleID[:])
		ticket[0x1F1] = p.keyIndex
		binary.BigEndian.PutUint32(ticket[0x2B8:], uint32(p.dataOff>>2))
		binary.BigEndian.PutUint32(ticket[0x2BC:], uint32(p.groups*wii.GroupSize>>2))

		// User data stream.
		user := make([]byte, p.groups*wii.DataSize)
		conFillRandom(r, user)
		copy(user, "RSPE01")
		if p.withFST {
			fst := conBuildFST(p.files, 2)
			const fstOff = 0x2440
			sysEnd := uint64(fstOff + len(fst))
			for range (len(user) + ngcw.JunkBlockSize - 1) / ngcw.JunkBlockSize {
				p.junkSeeds = append(p.junkSeeds, conRandomSeed(r))
			}
			conLFGFill(user, 0, sysEnd, uint64(len(user)), func(b uint64) ngcw.Seed { return p.junkSeeds[b] })
			binary.BigEndian.PutUint32(user[0x424:], fstOff>>2)
			binary.BigEndian.PutUint32(user[0x428:], uint32(len(fst)>>2+1))
			copy(user[fstOff:], fst)
			for _, f := range p.files {
				conFillRandom(r, user[f.offset:f.offset+uint64(f.length)])
			}
		} else {
			binary.BigEndian.PutUint32(user[0x424:], 0x2440>>2)
			binary.BigEndian.PutUint32(user[0x428:], 0)
		}
		p.userData = user

		// Encrypted groups.
		group := make([]byte, wii.GroupSize)
		for g := 0; g < p.groups; g++ {
			conFillRandom(r, group[:wii.HashSize])
			copy(group[wii.HashSize:], user[g*wii.DataSize:])
			if err := wii.EncryptGroup(p.titleKey, group); err != nil {
				t.Fatal(err)
			}
			copy(disc[p.offset+p.dataOff+uint64(g)*wii.GroupSize:], group)
		}
	}
	return disc, parts
}
