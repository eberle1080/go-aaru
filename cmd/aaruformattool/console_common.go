package main

// Helpers shared by the console disc converters (convert-ps3, convert-wiiu and
// convert-ngcw).
//
// Every package-level identifier here is prefixed with "con" so that it cannot
// clash with the helpers of the other verbs.

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// ANSI escape sequences (the ANSI_* macros of the converters).
const (
	conReset  = "\033[0m"
	conBold   = "\033[1m"
	conRed    = "\033[31m"
	conGreen  = "\033[32m"
	conYellow = "\033[33m"
	conCyan   = "\033[36m"
	conWhite  = "\033[37m"
	conBlue   = "\033[34m"
)

const (
	// conProgressBarWidth is PROGRESS_BAR_WIDTH.
	conProgressBarWidth = 40
	// conSectorSize is the logical sector size of every console disc
	// (PS3_SECTOR_SIZE, WIIU_LOGICAL_SECTOR_SIZE, NGC_SECTOR_SIZE).
	conSectorSize = 2048
	// conAppName is the application name stored in the images created.
	conAppName = "aaruformattool"

	conDoubleRule = "════════════════════════════════════════════════════════════════════════════════"
	conRule       = "────────────────────────────────────────────────────────────────────────────────"
)

// conOut is where a converter writes its report: buffered standard output
// for the normal messages and the progress bar, and an unbuffered error
// stream for the error messages.
type conOut struct {
	w  *bufio.Writer
	ew io.Writer
}

func newConOut(w, ew io.Writer) *conOut { return &conOut{w: bufio.NewWriter(w), ew: ew} }

func (o *conOut) flush() { _ = o.w.Flush() }

func (o *conOut) printf(format string, a ...any) { fmt.Fprintf(o.w, format, a...) }

func (o *conOut) print(s string) { _, _ = o.w.WriteString(s) }

// errorf prints an error line. Standard output is flushed
// first so that both streams stay in order on a terminal.
func (o *conOut) errorf(format string, a ...any) {
	o.flush()
	fmt.Fprintf(o.ew, conRed+"  ✗ %s"+conReset+"\n", fmt.Sprintf(format, a...))
}

// successf prints a success line.
func (o *conOut) successf(format string, a ...any) {
	fmt.Fprintf(o.w, conGreen+"  ✓ %s"+conReset+"\n", fmt.Sprintf(format, a...))
}

// info prints a label/value line.
func (o *conOut) info(label, value string) {
	fmt.Fprintf(o.w, "  "+conYellow+"%-20s"+conReset+" %s\n", label, value)
}

// warn prints a yellow warning line.
func (o *conOut) warn(msg string) { fmt.Fprintf(o.w, "  "+conYellow+"⚠ %s"+conReset+"\n", msg) }

// banner prints the converter title box; title is the centred title line.
func (o *conOut) banner(title string) {
	o.print("\n" + conBold + conCyan + conDoubleRule + "\n" + title + "\n" + conDoubleRule + conReset + "\n")
}

// section prints a section heading followed by a rule.
func (o *conOut) section(title string) {
	o.print("\n" + conBold + conCyan + "  " + title + conReset + "\n")
	o.print(conBlue + conRule + conReset + "\n")
}

// progress draws the progress bar (the loop bodies of the converters).
// bytesProcessed and start give the speed shown next to the percentage.
func (o *conOut) progress(percentage float64, bytesProcessed uint64, start time.Time) {
	elapsed := time.Since(start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(bytesProcessed) / elapsed
	}
	speedStr := conFormatBytes(uint64(speed))

	filled := int(percentage / 100.0 * conProgressBarWidth)
	if filled > conProgressBarWidth {
		filled = conProgressBarWidth
	}

	var b strings.Builder
	b.WriteString("\r  " + conCyan + "[")
	for i := 0; i < conProgressBarWidth; i++ {
		switch {
		case i < filled:
			b.WriteString(conGreen + "█")
		case i == filled:
			b.WriteString(conYellow + "▓")
		default:
			b.WriteString(conWhite + "░")
		}
	}
	o.printf("%s"+conCyan+"] "+conWhite+"%5.1f%%"+conReset+" │ %s/s   ", b.String(), percentage, speedStr)
	o.flush()
}

// finish prints the final verdict of a conversion and returns result.
func (o *conOut) finish(result int) int {
	if result == 0 {
		o.print("\n" + conGreen + "  ✓ Conversion completed successfully!" + conReset + "\n\n")
	} else {
		o.print("\n" + conRed + "  ✗ Conversion failed." + conReset + "\n\n")
	}
	return result
}

// conFormatBytes formats a byte count with binary units.
func conFormatBytes(bytes uint64) string {
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	size := float64(bytes)
	for size >= 1024.0 && unit < len(units)-1 {
		size /= 1024.0
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.2f %s", size, units[unit])
}

// conHexDigit returns the value of the hexadecimal digit c.
func conHexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// conParseHexKey parses a 32-character hexadecimal key into key.
// The key bytes before an invalid pair are stored, and a pair whose second
// character is not a hex digit yields the value of its first digit.
func conParseHexKey(hex string, key *[16]byte) bool {
	if len(hex) != 32 {
		return false
	}
	for i := range key {
		hi, ok := conHexDigit(hex[i*2])
		if !ok {
			return false
		}
		if lo, ok := conHexDigit(hex[i*2+1]); ok {
			hi = hi<<4 | lo
		}
		key[i] = hi
	}
	return true
}

// conReplaceExt returns path with its extension replaced by suffix, if the
// last '.' is part of the file name (after the last path separator).
func conReplaceExt(path, suffix string) (string, bool) {
	dot := strings.LastIndexByte(path, '.')
	sep := strings.LastIndexByte(path, '/')
	if os.PathSeparator != '/' {
		sep = max(sep, strings.LastIndexByte(path, os.PathSeparator))
	}
	if dot < 0 || dot < sep {
		return "", false
	}
	return path[:dot] + suffix, true
}

// conTrySidecar reads the first n bytes of the sidecar file of inputPath
// with the given suffix: first "<input><suffix>", then
// "<input without extension><suffix>". The second name is only tried when
// the first cannot be opened, and a sidecar shorter than n bytes is rejected.
func conTrySidecar(inputPath, suffix string, n int) ([]byte, bool) {
	f, err := os.Open(inputPath + suffix)
	if err != nil {
		alt, ok := conReplaceExt(inputPath, suffix)
		if !ok {
			return nil, false
		}
		if f, err = os.Open(alt); err != nil {
			return nil, false
		}
	}
	defer f.Close()
	buf := make([]byte, n)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, false
	}
	return buf, true
}

// conStrerror renders err as the system error text when it wraps a system
// error (e.g. "No such file or directory"), else as its Go message.
func conStrerror(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		s := errno.Error()
		if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError {
			return string(unicode.ToUpper(r)) + s[size:]
		}
		return s
	}
	return err.Error()
}

// conMediaTypeString names a media type in the converters' messages. The
// console media types get descriptive names; other types are shown by their
// constant name.
func conMediaTypeString(t media.Type) string { return mediaTypeString(t) }

// conNulString returns b up to its first NUL byte (a NUL-terminated string
// in a fixed buffer).
func conNulString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// conOptString returns the value of the string option name if it was given
// on the command line, else nil.
func conOptString(va *verbArgs, name string) *string {
	var v *string
	va.fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			s := f.Value.String()
			v = &s
		}
	})
	return v
}

// conReadSector reads sector addr of img into a len(buf)-byte buffer: it
// returns the AaruFormat status code (0 on success), refuses sectors larger
// than buf (ErrBufferTooSmall) and leaves the tail of buf untouched when
// the sector is shorter.
func conReadSector(img *aaru.Image, addr uint64, buf []byte) int {
	data, _, err := img.ReadSector(addr, false)
	if err != nil {
		return aaru.ErrorCode(err)
	}
	if len(data) > len(buf) {
		return aaru.ErrorCode(aaru.ErrBufferTooSmall)
	}
	copy(buf, data)
	return 0
}

// conCreateOutput creates the destination image of a converter
// (default options, application "aaruformattool" 1.0).
func conCreateOutput(path string, mt media.Type, sectors uint64) (*aaru.Image, error) {
	return aaru.Create(path, aaru.CreateParams{
		MediaType:               mt,
		SectorSize:              conSectorSize,
		UserSectors:             sectors,
		Application:             conAppName,
		ApplicationMajorVersion: 1,
		ApplicationMinorVersion: 0,
	}, nil)
}

// conCopySourceMetadata copies the media tags (except those skip reports),
// dump hardware, Aaru JSON metadata, creator, comments and media sequence of
// an AaruFormat source image to the destination (the "Copying Source
// Metadata" step). Creator and
// comments are copied as raw (UTF-16LE) bytes.
func conCopySourceMetadata(o *conOut, in, out *aaru.Image, skip func(aaru.MediaTagType) bool) {
	copied := 0
	for _, t := range in.ReadableMediaTags() {
		if skip(t) {
			continue
		}
		data, err := in.ReadMediaTag(t)
		if err != nil || len(data) == 0 {
			continue
		}
		if out.WriteMediaTag(t, data) == nil {
			copied++
		}
	}
	if copied > 0 {
		o.successf("Copied %d additional media tag(s)", copied)
	}

	if hw, err := in.DumpHardwareRaw(); err == nil && len(hw) > 0 {
		if out.SetDumpHardwareRaw(hw) == nil {
			o.successf("Copied dump hardware information")
		}
	}

	if js, err := in.AaruJSONMetadata(); err == nil && len(js) > 0 {
		if out.SetAaruJSONMetadata(js) == nil {
			o.successf("Copied Aaru JSON metadata")
		}
	}

	for _, f := range []aaru.MetadataField{aaru.MetadataCreator, aaru.MetadataComments} {
		if raw, err := in.MetadataRaw(f); err == nil && len(raw) > 0 {
			_ = out.SetMetadataRaw(f, raw)
		}
	}

	if seq, last, err := in.MediaSequence(); err == nil && seq > 0 {
		_ = out.SetMediaSequence(seq, last)
	}
}

// conCloseOutput closes the destination image. A failure to finalize the
// image is reported and turns the conversion into a failure.
func conCloseOutput(o *conOut, out *aaru.Image, result int) int {
	if out == nil {
		return result
	}
	if err := out.Close(); err != nil {
		o.errorf("Error closing output image (error %d): %v", aaru.ErrorCode(err), err)
		return -1
	}
	return result
}
