package udf

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// WalkFunc is called by Walk for every entry. See Walk.
type WalkFunc func(path string, e *Entry, err error) error

// Walk visits the volume's entries depth first in disc order, starting with
// the root ("/"), calling fn for each. Named streams are not visited (see
// Entry.Streams).
//
// The semantics follow fs.WalkDir: returning fs.SkipDir from fn for a
// directory skips its contents (for a file, the rest of its directory);
// fs.SkipAll stops the walk; any other non-nil error stops the walk and is
// returned. An entry whose file entry cannot be read is passed with that
// error (and not entered). When a directory cannot be (fully) read, fn is
// called a second time for it with the error; if that call returns nil,
// the entries that could be read are still walked. A directory reached a
// second time (a loop, or a hard link to a directory) is reported with an
// error wrapping ErrLoop, and one nested deeper than Options.MaxDepth with
// ErrTooDeep; neither is entered. Walk stops with ErrTooManyEntries after
// Options.MaxEntries entries.
func (v *Volume) Walk(fn WalkFunc) error {
	w := walker{v: v, fn: fn, visited: map[addr]bool{}}
	root := v.root
	err := fn(root.Path, root, root.err)
	if err == nil && root.err == nil {
		w.visited[root.loc] = true
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
	visited map[addr]bool
	count   int
}

func (w *walker) dir(d *Entry, depth int) error {
	ents, rerr := w.v.readDir(d, "")
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
		cerr := e.err
		if e.IsDir && cerr == nil {
			switch {
			case w.visited[e.loc]:
				cerr = fmt.Errorf("%w: %s is a directory already visited (block %d)", ErrLoop, e.Path, e.loc.block)
			case depth+1 > w.v.opts.MaxDepth:
				cerr = fmt.Errorf("%w: %s is deeper than %d levels", ErrTooDeep, e.Path, w.v.opts.MaxDepth)
			default:
				w.visited[e.loc] = true
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

// File reads a file's (or a named stream's) data. It implements io.Reader,
// io.ReaderAt and io.Seeker. Unrecorded parts read as zeros. Reads go to
// the volume's source reader, so a File shares the Volume's (lack of)
// concurrency safety.
type File struct {
	v     *Volume
	entry *Entry
	pos   int64
}

// OpenFile opens a file entry (a regular file, symbolic link, device, named
// stream...) for reading. It fails with ErrIsDirectory for a directory, and
// with the entry's error when its file entry could not be read. Extents
// outside the volume fail when read (ErrOutOfRange, ErrPartition).
func (v *Volume) OpenFile(e *Entry) (*File, error) {
	if e == nil || e.vol != v {
		return nil, fmt.Errorf("%w: entry of another volume", ErrCorrupt)
	}
	if e.err != nil {
		return nil, e.err
	}
	if e.IsDir {
		return nil, fmt.Errorf("%w: %s", ErrIsDirectory, e.Path)
	}
	if e.data == nil {
		return nil, fmt.Errorf("%w: %s has no data", ErrCorrupt, e.Path)
	}
	return &File{v: v, entry: e}, nil
}

// OpenStream opens a named stream (an element of Entry.Streams or of
// SystemStreams) for reading. It fails with ErrNotStream for other entries.
func (v *Volume) OpenStream(e *Entry) (*File, error) {
	if e == nil || !e.Stream {
		return nil, ErrNotStream
	}
	return v.OpenFile(e)
}

// Size returns the file size.
func (f *File) Size() int64 { return f.entry.data.size }

// Entry returns the entry the file was opened from.
func (f *File) Entry() *Entry { return f.entry }

// ReadAt implements io.ReaderAt.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.v.readContent(f.entry.data, p, off)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("udf: %s at offset %d: %w", f.entry.Path, off+int64(n), err)
	}
	return n, err
}

// Read implements io.Reader.
func (f *File) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
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
		offset += f.Size()
	default:
		return 0, fmt.Errorf("udf: invalid whence %d", whence)
	}
	if offset < 0 {
		return 0, fmt.Errorf("udf: negative position %d", offset)
	}
	f.pos = offset
	return offset, nil
}
