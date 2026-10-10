package main

// Helpers shared by the maintenance verbs (upgrade-ddt-to-alpha21,
// repair-cd-arena, inject-media-tag), which work on the raw on-disk
// structures of an image instead of going through the library.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

// POSIX error numbers the maintenance verbs return as exit status.
const (
	maintEIO    = 5
	maintEINVAL = 22
)

// maintErrno returns the system error number carried by err, or EIO when err
// carries none.
func maintErrno(err error) int {
	var e syscall.Errno
	if errors.As(err, &e) {
		return int(e)
	}
	return maintEIO
}

// maintStrerror formats err as the system error text ("No such file or
// directory").
func maintStrerror(err error) string {
	msg := err.Error()
	var e syscall.Errno
	if errors.As(err, &e) {
		msg = e.Error()
	}
	if msg != "" && msg[0] >= 'a' && msg[0] <= 'z' {
		msg = string(msg[0]-'a'+'A') + msg[1:]
	}
	return msg
}

// maintReadLine reads a confirmation line: at most 255 bytes are consumed, the
// string ends at the first NUL and one trailing '\n' is removed. ok is false
// when nothing could be read.
func maintReadLine(r *bufio.Reader) (line string, ok bool) {
	var b []byte
	for len(b) < 255 {
		c, err := r.ReadByte()
		if err != nil {
			break
		}
		b = append(b, c)
		if c == '\n' {
			break
		}
	}
	if len(b) == 0 {
		return "", false
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSuffix(string(b), "\n"), true
}

// maintIsYes reports whether a confirmation answer is accepted.
func maintIsYes(s string) bool { return s == "yes" || s == "YES" || s == "y" || s == "Y" }

// maintReadStruct decodes the little-endian packed structure v from r at off.
// It returns the number of bytes read, which is less than binary.Size(v) when
// the read was short (then v is left unchanged).
func maintReadStruct(r io.ReaderAt, off int64, v any) (int, error) {
	buf := make([]byte, binary.Size(v))
	n, err := r.ReadAt(buf, off)
	if n < len(buf) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return n, err
	}
	_, err = binary.Decode(buf, binary.LittleEndian, v)
	return n, err
}

// maintWriteStruct encodes v little-endian packed and writes it to w at off.
func maintWriteStruct(w io.WriterAt, off int64, v any) (int, error) {
	buf, err := binary.Append(nil, binary.LittleEndian, v)
	if err != nil {
		return 0, err
	}
	return w.WriteAt(buf, off)
}

// maintFileSize returns the current size of f.
func maintFileSize(f *os.File) (int64, error) {
	return f.Seek(0, io.SeekEnd)
}

// maintAlignUp rounds pos up to a multiple of 2^shift.
func maintAlignUp(pos uint64, shift uint8) uint64 {
	mask := uint64(1)<<shift - 1
	return (pos + mask) &^ mask
}

// maintFlush flushes w when it is buffered (before prompting or exiting).
func maintFlush(w io.Writer) {
	if f, ok := w.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
}
