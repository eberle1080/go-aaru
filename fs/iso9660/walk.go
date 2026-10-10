package iso9660

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
)

// WalkFunc is called by Walk for every entry. See Walk.
type WalkFunc func(path string, e *Entry, err error) error

// Walk visits the volume's entries depth first in disc order, starting with
// the root ("/"), calling fn for each.
//
// The semantics follow fs.WalkDir: returning fs.SkipDir from fn for a
// directory skips its contents (for a file, the rest of its directory);
// fs.SkipAll stops the walk; any other non-nil error stops the walk and is
// returned. When a directory cannot be (fully) read, fn is called a second
// time for it with the error; if that call returns nil, the entries that
// could be read are still walked. A directory reached a second time (a loop
// or a shared extent) is reported with an error wrapping ErrLoop, and one
// nested deeper than Options.MaxDepth with ErrTooDeep; neither is entered.
// Walk stops with ErrTooManyEntries after Options.MaxEntries entries.
func (v *Volume) Walk(fn WalkFunc) error {
	w := walker{v: v, fn: fn, visited: map[uint32]bool{}}
	root := v.root
	w.visited[root.Extents[0].LBA] = true
	err := fn(root.Path, root, nil)
	if err == nil {
		err = w.dir(root, 0)
	}
	if errors.Is(err, fs.SkipDir) || errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}

type walker struct {
	v       *Volume
	fn      WalkFunc
	visited map[uint32]bool
	count   int
}

func (w *walker) dir(d *Entry, depth int) error {
	ents, rerr := w.v.ReadDir(d)
	if rerr != nil {
		if err := w.fn(d.Path, d, rerr); err != nil {
			if errors.Is(err, fs.SkipDir) {
				return nil
			}
			return err
		}
	}
	for _, e := range ents {
		w.count++
		if w.count > w.v.opts.MaxEntries {
			return fmt.Errorf("%w: more than %d", ErrTooManyEntries, w.v.opts.MaxEntries)
		}
		var cerr error
		if e.IsDir {
			lba := e.Extents[0].LBA
			switch {
			case w.visited[lba]:
				cerr = fmt.Errorf("%w: %s reuses the extent of a directory already visited (block %d)",
					ErrLoop, e.Path, lba)
			case depth+1 > w.v.opts.MaxDepth:
				cerr = fmt.Errorf("%w: %s is deeper than %d levels", ErrTooDeep, e.Path, w.v.opts.MaxDepth)
			default:
				w.visited[lba] = true
			}
		}
		err := w.fn(e.Path, e, cerr)
		if err != nil {
			if errors.Is(err, fs.SkipDir) {
				if e.IsDir {
					continue
				}
				return nil
			}
			return err
		}
		if e.IsDir && cerr == nil {
			if err := w.dir(e, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// File reads a file's data. It implements io.Reader, io.ReaderAt and
// io.Seeker. Reads go to the volume's source reader, so a File shares the
// Volume's (lack of) concurrency safety.
type File struct {
	v     *Volume
	entry *Entry
	exts  []fileExtent
	size  int64
	pos   int64
}

// fileExtent maps logical file bytes [logical, logical+length) to the
// source: data starts at start; with interleaving, units of unit bytes are
// separated by gap bytes.
type fileExtent struct {
	logical, length int64
	start           int64
	unit, gap       int64
}

// OpenFile opens a file entry for reading. Multi-extent files read as the
// concatenation of their extents; extended attribute records and interleave
// gaps are skipped. It fails with ErrIsDirectory, ErrInterleave or
// ErrOutOfRange (an extent beyond the end of the source).
func (v *Volume) OpenFile(e *Entry) (*File, error) {
	if e == nil || e.tree == nil {
		return nil, fmt.Errorf("%w: nil entry", ErrCorrupt)
	}
	if e.IsDir {
		return nil, fmt.Errorf("%w: %s", ErrIsDirectory, e.Path)
	}
	bs := e.tree.blockSize
	f := &File{v: v, entry: e}
	for i, x := range e.Extents {
		if x.Length == 0 {
			continue
		}
		fx := fileExtent{
			logical: f.size,
			length:  int64(x.Length),
			start:   (int64(x.LBA) + int64(x.EARLength)) * bs,
			unit:    int64(x.FileUnitSize) * bs,
			gap:     int64(x.InterleaveGap) * bs,
		}
		phys := fx.length
		if fx.unit == 0 && fx.gap != 0 {
			return nil, fmt.Errorf("%w: %s extent %d has an interleave gap but no file unit size", ErrInterleave, e.Path, i)
		}
		if fx.unit > 0 {
			units := (fx.length + fx.unit - 1) / fx.unit
			phys = (units-1)*(fx.unit+fx.gap) + fx.length - (units-1)*fx.unit
		}
		if fx.start > v.size || phys > v.size-fx.start {
			return nil, fmt.Errorf("%w: %s extent %d (block %d, %d bytes)", ErrOutOfRange, e.Path, i, x.LBA, x.Length)
		}
		f.exts = append(f.exts, fx)
		f.size += fx.length
	}
	return f, nil
}

// Size returns the file size.
func (f *File) Size() int64 { return f.size }

// Entry returns the entry the file was opened from.
func (f *File) Entry() *Entry { return f.entry }

// ReadAt implements io.ReaderAt.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("iso9660: negative offset %d", off)
	}
	n := 0
	for len(p) > 0 {
		if off >= f.size {
			return n, io.EOF
		}
		i := sort.Search(len(f.exts), func(i int) bool { return f.exts[i].logical+f.exts[i].length > off })
		x := f.exts[i]
		rel := off - x.logical
		phys, avail := x.start+rel, x.length-rel
		if x.unit > 0 {
			u, within := rel/x.unit, rel%x.unit
			phys = x.start + u*(x.unit+x.gap) + within
			avail = min(x.unit-within, avail)
		}
		k := int(min(int64(len(p)), avail))
		if err := f.v.readAt(p[:k], phys); err != nil {
			return n, fmt.Errorf("iso9660: %s at offset %d: %w", f.entry.Path, off, err)
		}
		n += k
		off += int64(k)
		p = p[k:]
	}
	return n, nil
}

// Read implements io.Reader.
func (f *File) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.pos)
	f.pos += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

// Seek implements io.Seeker.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.size
	default:
		return 0, fmt.Errorf("iso9660: invalid whence %d", whence)
	}
	if offset < 0 {
		return 0, fmt.Errorf("iso9660: negative position %d", offset)
	}
	f.pos = offset
	return offset, nil
}
