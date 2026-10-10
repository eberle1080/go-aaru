package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/eberle1080/go-aaru"
)

// byteArrayToHexString formats b as upper-case hexadecimal digits. The
// human-readable name tables are in names_gen.go.
func byteArrayToHexString(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

// coreOpenErrno returns the number the tool prints and returns when opening
// or creating an image fails: the system error number, or the AaruFormat
// error code of err.
func coreOpenErrno(err error) int {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return int(errno)
	}
	return aaru.ErrorCode(err)
}

// coreOutput is where the core verbs write: stdout is buffered (bulk output),
// stderr is not; writing to stderr flushes stdout first so that messages
// interleave as with a line-buffered stdout.
type coreOutput struct {
	out *bufio.Writer
	err io.Writer
}

// newCoreOutput wraps stdout and stderr.
func newCoreOutput(stdout, stderr io.Writer) *coreOutput {
	return &coreOutput{out: bufio.NewWriterSize(stdout, 64*1024), err: stderr}
}

// printf writes to stdout.
func (o *coreOutput) printf(format string, a ...any) { fmt.Fprintf(o.out, format, a...) }

// print writes s to stdout.
func (o *coreOutput) print(s string) { _, _ = o.out.WriteString(s) }

// eprintf writes to stderr after flushing stdout.
func (o *coreOutput) eprintf(format string, a ...any) {
	_ = o.out.Flush()
	fmt.Fprintf(o.err, format, a...)
}

// flush flushes stdout.
func (o *coreOutput) flush() { _ = o.out.Flush() }

// withCoreOutput runs a verb body with buffered stdout and plain stderr.
func withCoreOutput(body func(o *coreOutput) int) int {
	o := newCoreOutput(os.Stdout, os.Stderr)
	defer o.flush()
	return body(o)
}

// coreTerminalSupportsColor reports whether f is a terminal that likely supports
// ANSI colors: TERM unset, or containing "color",
// "xterm", "screen" or "tmux", or equal to "linux" or "vt100"; NO_COLOR
// disables colors.
func coreTerminalSupportsColor(f *os.File) bool {
	if !term.IsTerminal(int(f.Fd())) {
		return false
	}
	use := false
	if t, ok := os.LookupEnv("TERM"); !ok {
		use = true
	} else if strings.Contains(t, "color") || strings.Contains(t, "xterm") || strings.Contains(t, "screen") ||
		strings.Contains(t, "tmux") || t == "linux" || t == "vt100" {
		use = true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		use = false
	}
	return use
}

// corePadBytes pads s with spaces to at least n bytes, counting bytes, not
// characters.
func corePadBytes(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// coreTruncateUTF8 cuts s to at most n bytes, backing off to a UTF-8 character
// boundary.
func coreTruncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
