package aaru

// Metadata write scenarios.
//
// Every scenario is a script of operations (create/resume, sector writes,
// geometry, media sequence, metadata strings, JSON, dump hardware, DPM, tape
// files and partitions, flux captures, ...) executed by go-aaru. While the
// script is built, every operation is also applied to a model of the
// metadata the image must hold (metaModel). The image is then opened, and
// the output of every metadata getter is checked against the model; the
// image must verify, and opening it twice must read the same metadata.

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"lukechampine.com/blake3"

	"github.com/eberle1080/go-aaru/crc64"
	"github.com/eberle1080/go-aaru/media"
	"github.com/eberle1080/go-aaru/spamsum"
)

type mop struct {
	name string
	run  func(st *mstate) error
}

type mstate struct {
	img  *Image
	path string
}

// metaFlux is a flux capture of the model.
type metaFlux struct {
	key         FluxCaptureKey
	dres, ires  uint64
	data, index []byte
}

type metaTagKey struct {
	addr uint64
	tag  SectorTagType
}

// metaModel is the metadata the image written by a script must hold.
type metaModel struct {
	params     CreateParams
	checksums  string // the checksum options of the last create/resume
	geom       *[3]uint32
	metaBlock  bool
	seq        [2]int32
	strs       [metadataFieldCount][]byte
	json       []byte
	dumpHW     []byte // serialized dump hardware block, nil if none
	dpm        []byte // serialized DPM block, nil if none
	sectors    map[uint64][]byte
	sectorTags map[metaTagKey][]byte
	mediaTags  map[MediaTagType][]byte
	tapeFiles  []TapeFileEntry
	tapeParts  []TapePartitionEntry
	flux       []metaFlux // captures stored in the file
	newFlux    []metaFlux // captures written since the last create/resume
}

// metaStringsEmpty reports whether the metadata block holds nothing (it is
// then dropped).
func (m *metaModel) metaStringsEmpty() bool {
	if m.seq != [2]int32{} {
		return false
	}
	for _, s := range m.strs {
		if len(s) > 0 {
			return false
		}
	}
	return true
}

type mscript struct {
	ops []mop
	m   metaModel
}

func (s *mscript) add(name string, run func(st *mstate) error) {
	s.ops = append(s.ops, mop{name: name, run: run})
}

func metaOptions(opts string) *Options {
	if opts == "" {
		return nil
	}
	v := ParseOptions(opts)
	return &v
}

func (s *mscript) create(p CreateParams, opts string) {
	s.m = metaModel{params: p, checksums: opts, sectors: map[uint64][]byte{}, sectorTags: map[metaTagKey][]byte{},
		mediaTags: map[MediaTagType][]byte{}}
	s.add("create", func(st *mstate) error {
		img, err := Create(st.path, p, metaOptions(opts))
		st.img = img
		return err
	})
}

func (s *mscript) resume(opts string) {
	s.m.checksums = opts
	s.add("resume", func(st *mstate) error {
		img, err := Resume(st.path, metaOptions(opts))
		st.img = img
		return err
	})
}

func (s *mscript) close() {
	s.m.flux = append(s.m.flux, s.m.newFlux...)
	s.m.newFlux = nil
	s.add("close", func(st *mstate) error { return st.img.Close() })
}

func (s *mscript) ws(addr uint64, data []byte) {
	s.m.sectors[addr] = data
	s.add("ws", func(st *mstate) error {
		return st.img.WriteSector(addr, false, data, SectorStatusDumped)
	})
}

func (s *mscript) wt(addr uint64, tag SectorTagType, data []byte) {
	s.m.sectorTags[metaTagKey{addr, tag}] = data
	s.add("wt", func(st *mstate) error {
		return st.img.WriteSectorTag(addr, false, tag, data)
	})
}

func (s *mscript) mtag(tag MediaTagType, data []byte) {
	s.m.mediaTags[tag] = data
	s.add("mtag", func(st *mstate) error {
		return st.img.WriteMediaTag(tag, data)
	})
}

func (s *mscript) setTracks(tracks ...TrackEntry) {
	s.add("settracks", func(st *mstate) error { return st.img.SetTracks(tracks) })
}

func (s *mscript) geom(c, h, sp uint32) {
	s.m.geom = &[3]uint32{c, h, sp}
	s.add("geom", func(st *mstate) error { return st.img.SetGeometry(c, h, sp) })
}

func (s *mscript) seq(a, b int32) {
	s.m.metaBlock = true
	s.m.seq = [2]int32{a, b}
	s.add("seq", func(st *mstate) error { return st.img.SetMediaSequence(a, b) })
}

func (s *mscript) clrseq() {
	if s.m.metaBlock {
		s.m.seq = [2]int32{}
		s.m.metaBlock = !s.m.metaStringsEmpty()
	}
	s.add("clrseq", func(st *mstate) error { return st.img.ClearMediaSequence() })
}

// str sets a metadata string through the Go string API.
func (s *mscript) str(f MetadataField, v string) {
	s.m.metaBlock = true
	s.m.strs[f] = EncodeUTF16(v)
	s.add("str", func(st *mstate) error {
		return st.img.SetMetadataString(f, v)
	})
}

func (s *mscript) rawstr(f MetadataField, b []byte) {
	s.m.metaBlock = true
	s.m.strs[f] = b
	s.add("str", func(st *mstate) error { return st.img.SetMetadataRaw(f, b) })
}

func (s *mscript) clrstr(f MetadataField) {
	if s.m.metaBlock {
		s.m.strs[f] = nil
		s.m.metaBlock = !s.m.metaStringsEmpty()
	}
	s.add("clrstr", func(st *mstate) error { return st.img.ClearMetadata(f) })
}

func (s *mscript) json(b []byte) {
	s.m.json = b
	s.add("json", func(st *mstate) error { return st.img.SetAaruJSONMetadata(b) })
}

// dumphw sets the dump hardware; the model holds its serialized form.
func (s *mscript) dumphw(hw []DumpHardware) {
	s.m.dumpHW = nil
	if len(hw) > 0 {
		raw, err := EncodeDumpHardware(hw)
		if err != nil {
			panic(err)
		}
		s.m.dumpHW = raw
	}
	s.add("dumphw", func(st *mstate) error { return st.img.SetDumpHardware(hw) })
}

// dumphwRaw sets the dump hardware from a raw block; want is the dump
// hardware the image must then hold.
func (s *mscript) dumphwRaw(raw []byte, want []DumpHardware) {
	enc, err := EncodeDumpHardware(want)
	if err != nil {
		panic(err)
	}
	s.m.dumpHW = enc
	s.add("dumphw", func(st *mstate) error { return st.img.SetDumpHardwareRaw(raw) })
}

func (s *mscript) dpm(d *DPM) {
	raw, err := d.MarshalBinary()
	if err != nil {
		panic(err)
	}
	s.m.dpm = raw
	s.add("dpm", func(st *mstate) error { return st.img.SetDPM(d) })
}

func (s *mscript) clrdpm() {
	s.m.dpm = nil
	s.add("clrdpm", func(st *mstate) error { return st.img.ClearDPM() })
}

func (s *mscript) tfile(p uint8, f uint32, a, b uint64) {
	e := TapeFileEntry{File: f, Partition: p, FirstBlock: a, LastBlock: b}
	s.m.tapeFiles = slices.DeleteFunc(s.m.tapeFiles, func(x TapeFileEntry) bool {
		return x.Partition == p && x.File == f
	})
	s.m.tapeFiles = append(s.m.tapeFiles, e)
	s.add("tfile", func(st *mstate) error {
		return st.img.SetTapeFile(p, f, a, b)
	})
}

func (s *mscript) tpart(p uint8, a, b uint64) {
	e := TapePartitionEntry{Number: p, FirstBlock: a, LastBlock: b}
	s.m.tapeParts = slices.DeleteFunc(s.m.tapeParts, func(x TapePartitionEntry) bool { return x.Number == p })
	s.m.tapeParts = append(s.m.tapeParts, e)
	s.add("tpart", func(st *mstate) error {
		return st.img.SetTapePartition(p, a, b)
	})
}

func (s *mscript) flux(k FluxCaptureKey, dres, ires uint64, data, index []byte) {
	s.m.newFlux = append(s.m.newFlux, metaFlux{key: k, dres: dres, ires: ires, data: data, index: index})
	s.add("flux", func(st *mstate) error {
		return st.img.WriteFluxCapture(k, dres, ires, data, index)
	})
}

// clrflux discards the captures written since the last create/resume.
func (s *mscript) clrflux() {
	s.m.newFlux = nil
	s.add("clrflux", func(st *mstate) error { return st.img.ClearFluxCaptures() })
}

// runGo executes the script with go-aaru and returns the error of every
// operation.
func (s *mscript) runGo(path string) []error {
	st := &mstate{path: path}
	var errs []error
	for _, o := range s.ops {
		err := o.run(st)
		if err != nil {
			err = fmt.Errorf("%s: %w", o.name, err)
		}
		errs = append(errs, err)
		if err != nil && (o.name == "create" || o.name == "resume") {
			break
		}
	}
	if st.img != nil && !st.img.closed {
		st.img.Close()
	}
	return errs
}

// metaCheckAbsent checks that err reports missing data with want.
func metaCheckAbsent(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: err = %v, want %v", what, err, want)
	}
}

// metaCheckModel checks every metadata getter of img against m.
func metaCheckModel(t *testing.T, img *Image, m *metaModel) {
	t.Helper()
	info := img.Info()
	// (Tape images have no fixed sector count.)
	if info.MediaType != m.params.MediaType || (!m.params.Tape && (info.Sectors != m.params.UserSectors ||
		(m.params.UserSectors > 0 && info.SectorSize != m.params.SectorSize))) {
		t.Errorf("Info = media %v sectors %d sector size %d, want %v/%d/%d", info.MediaType, info.Sectors,
			info.SectorSize, m.params.MediaType, m.params.UserSectors, m.params.SectorSize)
	}
	if info.Application != m.params.Application {
		t.Errorf("Application = %q, want %q", info.Application, m.params.Application)
	}

	// Geometry.
	c, h, sp, err := img.Geometry()
	if m.geom == nil {
		metaCheckAbsent(t, "Geometry", err, ErrCannotReadBlock)
	} else if err != nil || [3]uint32{c, h, sp} != *m.geom {
		t.Errorf("Geometry = %d/%d/%d, %v; want %v", c, h, sp, err, *m.geom)
	}

	// Metadata block: media sequence and strings.
	seq, last, err := img.MediaSequence()
	if !m.metaBlock {
		metaCheckAbsent(t, "MediaSequence", err, ErrMetadataNotPresent)
	} else if err != nil || [2]int32{seq, last} != m.seq {
		t.Errorf("MediaSequence = %d/%d, %v; want %v", seq, last, err, m.seq)
	}
	for f := MetadataField(0); f < metadataFieldCount; f++ {
		b, err := img.MetadataRaw(f)
		want := m.strs[f]
		if !m.metaBlock || len(want) == 0 {
			metaCheckAbsent(t, "MetadataRaw("+f.String()+")", err, ErrMetadataNotPresent)
			continue
		}
		if err != nil || !bytes.Equal(b, want) {
			t.Errorf("MetadataRaw(%v) = %x, %v; want %x", f, b, err, want)
		}
		if s, err := img.MetadataString(f); err != nil || s != DecodeUTF16(want) {
			t.Errorf("MetadataString(%v) = %q, %v; want %q", f, s, err, DecodeUTF16(want))
		}
	}

	_, err = img.CICMMetadata()
	metaCheckAbsent(t, "CICMMetadata", err, ErrCannotReadBlock)
	b, err := img.AaruJSONMetadata()
	if len(m.json) == 0 {
		metaCheckAbsent(t, "AaruJSONMetadata", err, ErrCannotReadBlock)
	} else if err != nil || !bytes.Equal(b, m.json) {
		t.Errorf("AaruJSONMetadata = %q, %v; want %q", b, err, m.json)
	}

	b, err = img.DumpHardwareRaw()
	if m.dumpHW == nil {
		metaCheckAbsent(t, "DumpHardwareRaw", err, ErrCannotReadBlock)
	} else if err != nil || !bytes.Equal(b, m.dumpHW) {
		t.Errorf("DumpHardwareRaw = %x, %v; want %x", b, err, m.dumpHW)
	}

	b, err = img.DPMRaw()
	if m.dpm == nil {
		if err == nil {
			t.Errorf("DPMRaw = %d bytes, want none", len(b))
		}
	} else if err != nil || !bytes.Equal(b, m.dpm) {
		t.Errorf("DPMRaw = %d bytes, %v; want %d bytes", len(b), err, len(m.dpm))
	}

	metaCheckChecksums(t, img, m)

	// Sectors, sector tags and media tags.
	for addr, want := range m.sectors {
		got, _, err := img.ReadSector(addr, false)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("ReadSector(%d) = %d bytes, %v", addr, len(got), err)
		}
	}
	for k, want := range m.sectorTags {
		got, err := img.ReadSectorTag(k.addr, false, k.tag)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("ReadSectorTag(%d, %v) = %x, %v; want %x", k.addr, k.tag, got, err, want)
		}
		if !slices.Contains(img.ReadableSectorTags(), k.tag) {
			t.Errorf("ReadableSectorTags = %v, missing %v", img.ReadableSectorTags(), k.tag)
		}
	}
	if len(m.sectorTags) == 0 && len(img.ReadableSectorTags()) != 0 {
		t.Errorf("ReadableSectorTags = %v, want none", img.ReadableSectorTags())
	}
	var wantTags []MediaTagType
	for tag, want := range m.mediaTags {
		wantTags = append(wantTags, tag)
		got, err := img.ReadMediaTag(tag)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("ReadMediaTag(%v) = %x, %v; want %x", tag, got, err, want)
		}
	}
	slices.Sort(wantTags)
	if got := img.ReadableMediaTags(); !(len(got) == 0 && len(wantTags) == 0) && !reflect.DeepEqual(got, wantTags) {
		t.Errorf("ReadableMediaTags = %v, want %v", got, wantTags)
	}

	// Tape.
	files, err := img.TapeFiles()
	if len(m.tapeFiles) == 0 {
		if len(files) != 0 {
			t.Errorf("TapeFiles = %+v, want none", files)
		}
	} else if err != nil || !reflect.DeepEqual(files, m.tapeFiles) {
		t.Errorf("TapeFiles = %+v, %v; want %+v", files, err, m.tapeFiles)
	}
	for _, f := range m.tapeFiles {
		if a, b, err := img.TapeFile(f.Partition, f.File); err != nil || a != f.FirstBlock || b != f.LastBlock {
			t.Errorf("TapeFile(%d, %d) = %d-%d, %v; want %+v", f.Partition, f.File, a, b, err, f)
		}
	}
	parts, err := img.TapePartitions()
	if len(m.tapeParts) == 0 {
		if len(parts) != 0 {
			t.Errorf("TapePartitions = %+v, want none", parts)
		}
	} else if err != nil || !reflect.DeepEqual(parts, m.tapeParts) {
		t.Errorf("TapePartitions = %+v, %v; want %+v", parts, err, m.tapeParts)
	}
	for _, p := range m.tapeParts {
		if a, b, err := img.TapePartition(p.Number); err != nil || a != p.FirstBlock || b != p.LastBlock {
			t.Errorf("TapePartition(%d) = %d-%d, %v; want %+v", p.Number, a, b, err, p)
		}
	}

	metaCheckFlux(t, img, m.flux)
}

// metaCheckChecksums checks the checksums block against digests of the
// sectors computed here.
func metaCheckChecksums(t *testing.T, img *Image, m *metaModel) {
	t.Helper()
	opts := ParseOptions(m.checksums)
	var all []byte
	addrs := make([]uint64, 0, len(m.sectors))
	for a := range m.sectors {
		addrs = append(addrs, a)
	}
	slices.Sort(addrs)
	for _, a := range addrs {
		all = append(all, m.sectors[a]...)
	}
	c := img.Checksums()
	check := func(name string, enabled bool, got, want []byte) {
		if !enabled {
			if got != nil {
				t.Errorf("%s = %x, want none", name, got)
			}
			return
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s = %x, want %x", name, got, want)
		}
	}
	m5, s1, s2, b3 := md5.Sum(all), sha1.Sum(all), sha256.Sum256(all), blake3.Sum256(all)
	check("MD5", opts.MD5, c.MD5, m5[:])
	check("SHA1", opts.SHA1, c.SHA1, s1[:])
	check("SHA256", opts.SHA256, c.SHA256, s2[:])
	check("BLAKE3", opts.Blake3, c.Blake3, b3[:])
	want := ""
	if opts.SpamSum {
		want = spamsum.HashBytes(all)
	}
	if c.SpamSum != want {
		t.Errorf("SpamSum = %q, want %q", c.SpamSum, want)
	}
}

// metaCheckFlux checks the flux captures: every capture is listed in order,
// and lookups return the last capture of a key.
func metaCheckFlux(t *testing.T, img *Image, want []metaFlux) {
	t.Helper()
	caps, err := img.FluxCaptures()
	if len(want) == 0 {
		metaCheckAbsent(t, "FluxCaptures", err, ErrFluxDataNotFound)
		return
	}
	if err != nil || len(caps) != len(want) {
		t.Fatalf("FluxCaptures = %d captures, %v; want %d", len(caps), err, len(want))
	}
	last := map[FluxCaptureKey]metaFlux{}
	for i, w := range want {
		last[w.key] = w
		if caps[i] != (FluxCaptureInfo{FluxCaptureKey: w.key, IndexResolution: w.ires, DataResolution: w.dres}) {
			t.Errorf("capture %d = %+v, want %+v", i, caps[i], w)
		}
	}
	for k, w := range last {
		index, data, err := img.ReadFluxCapture(k)
		if err != nil || !bytes.Equal(data, w.data) || !bytes.Equal(index, w.index) {
			t.Errorf("ReadFluxCapture(%+v) = %d/%d bytes, %v; want %d/%d", k, len(data), len(index), err,
				len(w.data), len(w.index))
		}
	}
}

// metaDump returns the output of every metadata getter for the image at
// path, used to check that reading is repeatable.
func metaDump(path string) []string {
	img, err := Open(path)
	if err != nil {
		return []string{"OPEN " + err.Error()}
	}
	defer img.Close()
	var out []string
	p := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	p("INFO %+v", img.Info())
	c, h, s, err := img.Geometry()
	p("GEOM %v %d %d %d", err, c, h, s)
	seq, last, err := img.MediaSequence()
	p("SEQ %v %d %d", err, seq, last)
	for f := MetadataField(0); f < metadataFieldCount; f++ {
		b, err := img.MetadataRaw(f)
		p("STR %d %v %x", f, err, b)
	}
	b, err := img.AaruJSONMetadata()
	p("JSON %v %x", err, b)
	b, err = img.DumpHardwareRaw()
	p("DUMPHW %v %x", err, b)
	b, err = img.DPMRaw()
	p("DPM %v %x", err, b)
	p("CKS %+v", img.Checksums())
	p("TAGS %v %v", img.ReadableSectorTags(), img.ReadableMediaTags())
	files, err := img.TapeFiles()
	p("TAPEFILES %v %+v", err, files)
	parts, err := img.TapePartitions()
	p("TAPEPARTS %v %+v", err, parts)
	caps, err := img.FluxCaptures()
	p("FLUX %v %+v", err, caps)
	for _, m := range caps {
		index, data, err := img.ReadFluxCapture(m.FluxCaptureKey)
		p("FLUXREAD %v %016x %016x", err, crc64.Checksum(data), crc64.Checksum(index))
	}
	return out
}

// metaScenario is a metadata write scenario.
type metaScenario struct {
	name  string
	build func(s *mscript)
}

func metaBlockParams(sectors uint64) CreateParams {
	p := app()
	p.MediaType = media.GENERIC_HDD
	p.SectorSize = 512
	p.UserSectors = sectors
	return p
}

func metaWriteSectors(s *mscript, r *rand.Rand, n uint64) {
	for i := uint64(0); i < n; i++ {
		s.ws(i, compressible(r, 512))
	}
}

func metaAllStrings(s *mscript) {
	values := []string{"Jane Doe", "Comments with ünïcödé and an emoji 💾", "Media title 日本語",
		"Sony", "MFD-2HD", "SN-0001", "123456789012", "PN 42", "Teac", "FD-235HF", "DRV-77", "1.2.3"}
	for i, v := range values {
		s.str(MetadataField(i), v)
	}
}

func metaHardware() []DumpHardware {
	return []DumpHardware{
		{Manufacturer: "Teac", Model: "FD-235HF", Revision: "A", Firmware: "1.0", Serial: "1234",
			SoftwareName: "Aaru", SoftwareVersion: "6.0", SoftwareOperatingSystem: "Linux",
			Extents: []DumpExtent{{Start: 8, End: 15}, {Start: 0, End: 7}}},
		{Manufacturer: "Ünïcode Corp", SoftwareName: "go-aaru", Extents: []DumpExtent{{Start: 3, End: 3}}},
		{},
	}
}

func metaDPM(entries int, seed uint64) *DPM {
	d := &DPM{NominalSpacing: 50, TimingUnit: 1, Speed: 0xFFFF, Flags: DPMFlagOppositeTrackPath,
		LayerEnds: []uint64{1000}, Calibrations: []DPMCalibration{{LBA: 0, RotationPeriod: 7500000,
			SectorsPerTurnX1000: 9718000}}}
	for i := 0; i < entries; i++ {
		st := DpmStatusMeasured
		if i == 0 {
			st = DpmStatusUnknown
		}
		d.Entries = append(d.Entries, DPMEntry{LBA: uint64(i) * 50, Angle: uint64(i) * seed, Status: st})
	}
	return d
}

func fluxPattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i*7)
	}
	return b
}

var metaScenarios = []metaScenario{
	{
		name: "block_all",
		build: func(s *mscript) {
			r := rand.New(rand.NewPCG(1, 2))
			s.create(metaBlockParams(16), "compress=false;deduplicate=false")
			metaWriteSectors(s, r, 16)
			s.geom(1, 2, 8)
			s.seq(2, 5)
			metaAllStrings(s)
			s.json([]byte(`{"AaruMetadata":{"Title":"x"}}`))
			s.dumphw(metaHardware())
			s.dpm(metaDPM(20, 131700))
			s.close()
		},
	},
	{
		name: "block_clear",
		build: func(s *mscript) {
			r := rand.New(rand.NewPCG(3, 4))
			s.create(metaBlockParams(16), "compress=false;deduplicate=true")
			metaWriteSectors(s, r, 16)
			s.seq(1, 1)
			metaAllStrings(s)
			s.clrseq()
			s.clrstr(MetadataCreator)
			s.clrstr(MetadataDriveFirmwareRevision)
			s.str(MetadataComments, "replaced")
			s.rawstr(MetadataMediaBarcode, []byte{})          // present but empty
			s.rawstr(MetadataMediaModel, []byte{'n', 0, 'o'}) // odd length, no terminator
			s.dpm(metaDPM(3, 1))
			s.clrdpm()
			s.geom(10, 1, 1)
			s.geom(80, 2, 18)
			s.close()
		},
	},
	{
		name: "block_clear_all",
		build: func(s *mscript) {
			s.create(metaBlockParams(8), "compress=false")
			metaWriteSectors(s, rand.New(rand.NewPCG(5, 6)), 8)
			s.seq(1, 2)
			s.str(MetadataCreator, "x")
			s.clrstr(MetadataCreator)
			s.clrseq()
			s.clrstr(MetadataComments) // no metadata block any more
			s.dumphw(nil)
			s.close()
		},
	},
	{
		// The metadata block ends on an alignment boundary and is followed
		// by an index of fewer than 112 bytes.
		name: "block_meta_overread",
		build: func(s *mscript) {
			s.create(metaBlockParams(16), "compress=false;deduplicate=false")
			metaWriteSectors(s, rand.New(rand.NewPCG(7, 8)), 16)
			s.str(MetadataCreator, strings.Repeat("c", 199)) // 112 + 400 = 512 bytes
			s.close()
		},
	},
	{
		name: "dumphw_raw",
		build: func(s *mscript) {
			s.create(metaBlockParams(4), "compress=false")
			metaWriteSectors(s, rand.New(rand.NewPCG(9, 10)), 4)
			// Strings without terminator: their last byte is replaced by NUL.
			h := DumpHardwareHeader{Identifier: BlockTypeDumpHardwareBlock, Entries: 1}
			eh := DumpHardwareEntry{ManufacturerLength: 4, ModelLength: 2, Extents: 2}
			payload := appendMarshal(nil, &eh)
			payload = append(payload, "TeacXY"...)
			payload = appendMarshal(payload, []DumpExtent{{Start: 9, End: 10}, {Start: 1, End: 2}})
			h.Length = uint32(len(payload))
			h.CRC64 = crc64.Checksum(payload)
			s.dumphwRaw(append(marshal(&h), payload...), []DumpHardware{{Manufacturer: "Tea", Model: "X",
				Extents: []DumpExtent{{Start: 1, End: 2}, {Start: 9, End: 10}}}})
			s.json([]byte("{}"))
			s.json([]byte(`{"second":true}`))
			s.close()
		},
	},
	{
		name: "cd_tags",
		build: func(s *mscript) {
			r := rand.New(rand.NewPCG(11, 12))
			p := app()
			p.MediaType = media.CDROM
			p.SectorSize = 2048
			p.UserSectors = 4
			s.create(p, "compress=false")
			s.setTracks(TrackEntry{Sequence: 1, Type: TrackTypeCdMode1, Start: 0, End: 3, Session: 1, Flags: 4})
			for i := uint64(0); i < 4; i++ {
				s.ws(i, compressible(r, 2048))
				s.wt(i, SectorTagCdSubchannel, compressible(r, 96))
			}
			s.mtag(MediaTagCdToc, []byte{0, 10, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0})
			s.mtag(MediaTagScsiInquiry, compressible(r, 36))
			s.seq(1, 2)
			s.close()
		},
	},
	{
		name: "checksums",
		build: func(s *mscript) {
			s.create(metaBlockParams(8), "compress=false;md5=true;sha1=true;sha256=true;spamsum=true;blake3=true")
			metaWriteSectors(s, rand.New(rand.NewPCG(13, 14)), 8)
			s.close()
		},
	},
	{
		name: "tape",
		build: func(s *mscript) {
			p := app()
			p.MediaType = media.LTO
			p.SectorSize = 512
			p.Tape = true
			s.create(p, "compress=false")
			s.tfile(0, 1, 0, 9)
			s.tfile(0, 2, 10, 19)
			s.tfile(1, 1, 0, 4)
			s.tfile(0, 1, 0, 8) // redefined: moves to the end
			s.tpart(0, 0, 19)
			s.tpart(1, 0, 4)
			s.tpart(0, 0, 20)
			s.close()
		},
	},
	{
		name: "flux_none",
		build: func(s *mscript) {
			p := app()
			p.MediaType = 1
			p.SectorSize = 512
			s.create(p, "compress=false")
			s.flux(FluxCaptureKey{0, 0, 0, 0}, 25000000, 25000000, fluxPattern(4096, 0xA5), fluxPattern(64, 0x5A))
			s.flux(FluxCaptureKey{1, 0, 0, 0}, 25000, 50000, fluxPattern(1000, 1), nil)
			s.flux(FluxCaptureKey{0, 1, 2, 3}, 1, 2, nil, fluxPattern(10, 3))
			s.flux(FluxCaptureKey{0, 2, 0, 0}, 1, 2, nil, nil)
			s.flux(FluxCaptureKey{0, 0, 0, 0}, 7, 7, fluxPattern(100, 9), fluxPattern(5, 9)) // duplicate key
			s.close()
		},
	},
	{
		name: "flux_lzma",
		build: func(s *mscript) {
			p := app()
			p.MediaType = 1
			p.SectorSize = 512
			s.create(p, "compress=true")
			s.flux(FluxCaptureKey{0, 0, 0, 0}, 25000000, 25000000, bytes.Repeat([]byte{1, 2, 3, 4}, 2048),
				fluxPattern(64, 0x5A))
			s.flux(FluxCaptureKey{0, 1, 0, 0}, 25000000, 25000000, fluxPattern(300, 7), nil) // incompressible
			s.close()
		},
	},
	{
		name: "flux_zstd",
		build: func(s *mscript) {
			p := app()
			p.MediaType = 1
			p.SectorSize = 512
			s.create(p, "compress=true;zstd=true")
			s.flux(FluxCaptureKey{3, 40, 1, 2}, 1000, 2000, bytes.Repeat([]byte{9, 8, 7}, 3000),
				bytes.Repeat([]byte{1}, 100))
			s.clrflux()
			s.flux(FluxCaptureKey{3, 41, 1, 2}, 1000, 2000, bytes.Repeat([]byte{5, 8, 7}, 3000), nil)
			s.close()
		},
	},
	{
		// Metadata changed after resuming: every change is persisted, also
		// those of setters that do not otherwise mark the metadata block
		// dirty.
		name: "resume",
		build: func(s *mscript) {
			metaResumeBase(s)
			s.str(MetadataCreator, "resumed creator")
			s.clrstr(MetadataComments)
			s.seq(2, 3)
			s.geom(2, 1, 4)
			s.json([]byte(`{"v":2}`))
			s.dumphw(metaHardware()[1:2])
			s.clrdpm()
			s.close()
		},
	},
	{
		// Setting the media title after resuming.
		name: "resume_title",
		build: func(s *mscript) {
			metaResumeBase(s)
			s.str(MetadataCreator, "resumed creator")
			s.clrstr(MetadataComments)
			s.str(MetadataMediaTitle, "resumed title")
			s.seq(2, 3)
			s.dpm(metaDPM(7, 3))
			s.close()
		},
	},
	{
		// Captures added to a resumed flux image are stored with the
		// existing ones.
		name: "flux_resume",
		build: func(s *mscript) {
			p := app()
			p.MediaType = 1
			p.SectorSize = 512
			s.create(p, "compress=false")
			s.flux(FluxCaptureKey{0, 0, 0, 0}, 10, 20, fluxPattern(500, 1), fluxPattern(20, 2))
			s.close()
			s.resume("compress=false")
			s.flux(FluxCaptureKey{0, 1, 0, 0}, 10, 20, fluxPattern(300, 3), fluxPattern(30, 4))
			s.close()
		},
	},
}

func metaResumeBase(s *mscript) {
	s.create(metaBlockParams(8), "compress=false")
	metaWriteSectors(s, rand.New(rand.NewPCG(15, 16)), 8)
	metaAllStrings(s)
	s.seq(1, 3)
	s.geom(1, 1, 8)
	s.json([]byte(`{"v":1}`))
	s.dumphw(metaHardware()[:1])
	s.dpm(metaDPM(5, 9))
	s.close()
	s.resume("compress=false")
}

func TestMetadataScenarios(t *testing.T) {
	for _, sc := range metaScenarios {
		t.Run(sc.name, func(t *testing.T) {
			s := &mscript{}
			sc.build(s)
			path := filepath.Join(t.TempDir(), "meta.aif")
			for _, err := range s.runGo(path) {
				if err != nil {
					t.Fatal(err)
				}
			}
			img, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer img.Close()
			if err := img.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
			metaCheckModel(t, img, &s.m)
			if a, b := metaDump(path), metaDump(path); !reflect.DeepEqual(a, b) {
				t.Errorf("reading the image twice differs:\n%s\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
			}
		})
	}
}
