package iso9660

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
)

// SectorSize is the size of an ISO 9660 logical sector, the unit volume
// descriptors are addressed in and directory records never straddle.
const SectorSize = 2048

// Errors returned by this package, usually wrapped with context. Test them
// with errors.Is.
var (
	// ErrNilReader means Open was given no reader.
	ErrNilReader = errors.New("iso9660: nil reader")
	// ErrNotISO9660 means no ISO 9660 volume descriptor was found at
	// BaseLBA+16.
	ErrNotISO9660 = errors.New("iso9660: not an ISO 9660 volume")
	// ErrNoVolume means the descriptor set has neither a primary nor a
	// supplementary volume descriptor.
	ErrNoVolume = errors.New("iso9660: no primary or supplementary volume descriptor")
	// ErrNamespaceUnavailable means the requested namespace is not present.
	ErrNamespaceUnavailable = errors.New("iso9660: namespace not present on this volume")
	// ErrOutOfRange means an extent lies (partly) outside the source.
	ErrOutOfRange = errors.New("iso9660: extent outside the volume")
	// ErrLoop means a directory was reached a second time during a walk.
	ErrLoop = errors.New("iso9660: directory loop")
	// ErrTooDeep means a directory exceeds Options.MaxDepth.
	ErrTooDeep = errors.New("iso9660: directories nested too deep")
	// ErrTooManyEntries means a walk exceeded Options.MaxEntries.
	ErrTooManyEntries = errors.New("iso9660: too many entries")
	// ErrNotDirectory means ReadDir was called on a file.
	ErrNotDirectory = errors.New("iso9660: not a directory")
	// ErrIsDirectory means OpenFile was called on a directory.
	ErrIsDirectory = errors.New("iso9660: is a directory")
	// ErrInterleave means a file's interleave parameters are inconsistent.
	ErrInterleave = errors.New("iso9660: invalid interleave parameters")
	// ErrCorrupt means a structure is malformed beyond use.
	ErrCorrupt = errors.New("iso9660: corrupt structure")
)

// Namespace selects which view of the volume is read.
type Namespace int

const (
	// NamespaceAuto picks the richest namespace present (see the package
	// documentation).
	NamespaceAuto Namespace = iota
	// NamespaceISO9660 is the primary volume descriptor's tree with plain
	// ISO 9660 names and no SUSP processing (XA system use is still read).
	NamespaceISO9660
	// NamespaceJoliet is the Joliet supplementary volume descriptor's tree.
	NamespaceJoliet
	// NamespaceRockRidge is the primary volume descriptor's tree with Rock
	// Ridge names, attributes and directory relocation applied.
	NamespaceRockRidge
)

func (n Namespace) String() string {
	switch n {
	case NamespaceAuto:
		return "auto"
	case NamespaceISO9660:
		return "ISO 9660"
	case NamespaceJoliet:
		return "Joliet"
	case NamespaceRockRidge:
		return "Rock Ridge"
	}
	return fmt.Sprintf("Namespace(%d)", int(n))
}

// Options configures Open. The zero value (or a nil *Options) is valid: it
// reads the first session, picks the namespace automatically and applies
// the default limits.
type Options struct {
	// BaseLBA is the first sector of the session whose volume descriptors
	// are read (they start at BaseLBA+16). Extent addresses on multisession
	// discs are absolute, so only the descriptors move.
	BaseLBA uint32
	// Namespace selects the view; NamespaceAuto picks one.
	Namespace Namespace
	// ShowRelocated also lists Rock Ridge relocated (RE) directories in
	// their physical parent (normally "rr_moved"). Walk then reaches them
	// twice and reports the second visit as ErrLoop.
	ShowRelocated bool

	// MaxDescriptors bounds the volume descriptors read (default 64).
	MaxDescriptors int
	// MaxDepth bounds directory nesting during Walk (default 255).
	MaxDepth int
	// MaxEntries bounds the entries visited by one Walk (default 1<<24).
	MaxEntries int
	// MaxDirSize bounds the bytes read for one directory (default 64 MiB).
	MaxDirSize int64
	// MaxContinuations bounds the SUSP continuation areas followed for one
	// directory record (default 64).
	MaxContinuations int
	// MaxWarnings bounds the warnings kept (default 10000).
	MaxWarnings int
}

func (o *Options) withDefaults() Options {
	var r Options
	if o != nil {
		r = *o
	}
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&r.MaxDescriptors, 64)
	def(&r.MaxDepth, 255)
	def(&r.MaxEntries, 1<<24)
	def(&r.MaxContinuations, 64)
	def(&r.MaxWarnings, 10000)
	if r.MaxDirSize <= 0 {
		r.MaxDirSize = 64 << 20
	}
	return r
}

// Warning is a recoverable irregularity found while reading the volume.
type Warning struct {
	// Path is the entry concerned ("/" for the root), or "" for volume-level
	// warnings.
	Path    string
	Message string
}

func (w Warning) String() string {
	if w.Path == "" {
		return w.Message
	}
	return w.Path + ": " + w.Message
}

// tree describes one directory hierarchy (the PVD's or an SVD's) and how its
// records are interpreted.
type tree struct {
	info      *VolumeInfo
	joliet    bool
	blockSize int64
	// susp: SUSP entries are parsed (an SP entry was found in the root).
	susp     bool
	suspSkip int
	// rrNames/rrReloc: Rock Ridge NM names and CL/PL/RE relocation apply.
	rrNames bool
	rrReloc bool
	// rrip: an ER entry or Rock Ridge entries identify Rock Ridge.
	rrip       bool
	extensions []Extension
	// rootXA and rootSU are the XA field and the SUSP area (starting at
	// its SP entry) of the root's "." record.
	rootXA *XAInfo
	rootSU []byte
}

// Volume is an open ISO 9660 volume. It is not safe for concurrent use.
type Volume struct {
	r    io.ReaderAt
	size int64 // source size; math.MaxInt64 when unknown
	opts Options

	descriptors []Descriptor
	primary     *VolumeInfo
	joliet      *VolumeInfo
	supp        []VolumeInfo
	boot        []BootRecord

	namespaces []Namespace
	ns         Namespace
	tree       *tree
	root       *Entry

	warnings []Warning
	dropped  int
}

// Open reads the volume descriptors of the ISO 9660 volume in r (size bytes
// long; size <= 0 means unknown, in which case reads past the end fail
// individually) and prepares the chosen namespace's root directory.
//
// It fails only when no usable volume descriptor exists or the requested
// namespace is absent; everything else is recorded in Warnings.
func Open(r io.ReaderAt, size int64, opts *Options) (*Volume, error) {
	if r == nil {
		return nil, ErrNilReader
	}
	v := &Volume{r: r, size: size, opts: opts.withDefaults()}
	if size <= 0 {
		v.size = math.MaxInt64
	}
	if err := v.readDescriptors(); err != nil {
		return nil, err
	}
	if v.primary == nil && len(v.supp) == 0 {
		return nil, ErrNoVolume
	}
	if err := v.chooseNamespace(); err != nil {
		return nil, err
	}
	return v, nil
}

// Info returns the primary volume descriptor's information, or that of the
// first supplementary descriptor when the volume has no PVD.
func (v *Volume) Info() VolumeInfo {
	if v.primary != nil {
		return *v.primary
	}
	return v.supp[0]
}

// Joliet returns the Joliet supplementary volume descriptor's information
// (identifiers decoded from UCS-2), if the volume has one.
func (v *Volume) Joliet() (VolumeInfo, bool) {
	if v.joliet == nil {
		return VolumeInfo{}, false
	}
	return *v.joliet, true
}

// Supplementary returns every supplementary and enhanced volume descriptor,
// Joliet or not, in disc order.
func (v *Volume) Supplementary() []VolumeInfo { return append([]VolumeInfo(nil), v.supp...) }

// Descriptors lists every volume descriptor read, in disc order, including
// the terminator.
func (v *Volume) Descriptors() []Descriptor { return append([]Descriptor(nil), v.descriptors...) }

// BootRecords returns the boot record descriptors (El Torito and others).
func (v *Volume) BootRecords() []BootRecord { return append([]BootRecord(nil), v.boot...) }

// Namespaces lists the namespaces present on the volume.
func (v *Volume) Namespaces() []Namespace { return append([]Namespace(nil), v.namespaces...) }

// Namespace returns the namespace being read.
func (v *Volume) Namespace() Namespace { return v.ns }

// Extensions returns the SUSP extensions (ER entries) declared in the root
// directory of the tree being read.
func (v *Volume) Extensions() []Extension { return append([]Extension(nil), v.tree.extensions...) }

// BlockSize returns the logical block size extents are addressed in.
func (v *Volume) BlockSize() int { return int(v.tree.blockSize) }

// Root returns the root directory of the chosen namespace.
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
	return fmt.Errorf("iso9660: reading %d bytes at offset %d: %w", len(p), off, err)
}

// chooseNamespace probes the trees present and selects one.
func (v *Volume) chooseNamespace() error {
	var pvdTree, jolietTree *tree
	pvdOK, jolietOK := false, false
	rrNamesUsed := false

	if v.primary != nil {
		pvdTree = v.newTree(v.primary, false)
		var err error
		rrNamesUsed, err = v.probeTree(pvdTree)
		pvdOK = err == nil
		if err != nil {
			v.warn("", "primary volume's root directory: %v", err)
		}
	}
	if v.joliet != nil {
		jolietTree = v.newTree(v.joliet, true)
		_, err := v.probeTree(jolietTree)
		jolietOK = err == nil
		if err != nil {
			v.warn("", "Joliet root directory: %v", err)
		}
	}

	if v.primary != nil {
		v.namespaces = append(v.namespaces, NamespaceISO9660)
	}
	if v.joliet != nil {
		v.namespaces = append(v.namespaces, NamespaceJoliet)
	}
	if pvdTree != nil && pvdTree.susp && pvdTree.rrip {
		v.namespaces = append(v.namespaces, NamespaceRockRidge)
	}
	has := func(n Namespace) bool {
		for _, x := range v.namespaces {
			if x == n {
				return true
			}
		}
		return false
	}

	ns := v.opts.Namespace
	switch ns {
	case NamespaceAuto:
		hasRR := has(NamespaceRockRidge)
		switch {
		case hasRR && rrNamesUsed && pvdOK:
			ns = NamespaceRockRidge
		case v.joliet != nil && (jolietOK || !pvdOK):
			ns = NamespaceJoliet
		case hasRR:
			ns = NamespaceRockRidge
		case v.primary != nil:
			ns = NamespaceISO9660
		default:
			ns = NamespaceISO9660 // first SVD read as plain ISO 9660
		}
	case NamespaceISO9660, NamespaceJoliet, NamespaceRockRidge:
		if !has(ns) && !(ns == NamespaceISO9660 && v.primary == nil && len(v.supp) > 0) {
			return fmt.Errorf("%w: %v", ErrNamespaceUnavailable, ns)
		}
	default:
		return fmt.Errorf("%w: %v", ErrNamespaceUnavailable, ns)
	}

	var t *tree
	switch ns {
	case NamespaceJoliet:
		t = jolietTree
	case NamespaceRockRidge:
		t = pvdTree
		t.rrNames, t.rrReloc = true, true
	default:
		if pvdTree != nil {
			t = pvdTree
		} else {
			t = v.newTree(&v.supp[0], false)
		}
		t.susp = false // plain ISO 9660: no SUSP
	}
	v.ns, v.tree = ns, t
	v.root = v.rootEntry(t)
	return nil
}

// newTree prepares a tree for the descriptor vi.
func (v *Volume) newTree(vi *VolumeInfo, joliet bool) *tree {
	bs := int64(vi.LogicalBlockSize)
	if bs < 512 || bs > 32768 || bs&(bs-1) != 0 {
		v.warn("", "logical block size %d is invalid; using %d", bs, SectorSize)
		bs = SectorSize
	}
	return &tree{info: vi, joliet: joliet, blockSize: bs}
}

// probeTree reads the root directory of t: it detects SUSP (an SP entry in
// the root's "." record, possibly after an XA field), collects the ER
// extensions, and reports whether any root entry carries a Rock Ridge NM
// name.
func (v *Volume) probeTree(t *tree) (rrNames bool, err error) {
	root := Extent{LBA: t.info.RootLBA, Length: t.info.RootSize}
	if r, perr := parseRecord(t.info.rootRecord[:]); perr == nil {
		root.EARLength = r.earLen
	}
	if root.Length == 0 {
		return false, fmt.Errorf("%w: root directory is empty", ErrCorrupt)
	}
	data, rerr := v.readDirBytes(t, root)
	if len(data) == 0 || data[0] < minRecordLen || int(data[0]) > len(data) {
		if rerr == nil {
			rerr = fmt.Errorf("%w: root directory has no \".\" record", ErrCorrupt)
		}
		return false, rerr
	}
	dot, perr := parseRecord(data[:data[0]])
	if perr != nil || !isDotRecord(dot.id) || dot.id[0] != 0 {
		return false, fmt.Errorf("%w: root directory does not start with a \".\" record", ErrCorrupt)
	}
	t.rootXA = parseXA(dot.su)
	su := dot.su
	if len(su) < 7 || string(su[:2]) != "SP" {
		if parseXA(su) != nil && len(su) >= 21 && string(su[14:16]) == "SP" {
			su = su[14:]
		} else {
			return false, nil
		}
	}
	if su[2] < 7 || su[4] != 0xBE || su[5] != 0xEF {
		v.warn("/", "malformed SUSP SP entry ignored")
		return false, nil
	}
	t.susp, t.suspSkip = true, int(su[6])
	t.rootSU = bytes.Clone(su)
	res := v.parseSUSP(t, su)
	t.extensions = res.ext
	for _, x := range res.ext {
		if rripIDs[x.ID] {
			t.rrip = true
		}
	}
	if len(res.ext) == 0 && res.sawRRIP {
		t.rrip = true // RRIP 1.09 discs may omit ER
	}

	recs := v.splitRecords("/", data)
	for _, r := range recs {
		if isDotRecord(r.id) {
			continue
		}
		s, _ := suspOf(t, r.su)
		if len(s) == 0 {
			continue
		}
		res := v.parseSUSP(t, s)
		if res.sawRRIP {
			if len(res.ext) == 0 && len(t.extensions) == 0 {
				t.rrip = true
			}
		}
		if res.rr != nil && res.rr.Name != "" {
			rrNames = true
			break
		}
	}
	return rrNames, nil
}
