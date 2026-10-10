package aaru

import (
	"strconv"
	"strings"
)

// Options control how images are created (and, partially, resumed). The zero
// value is NOT the default; use DefaultOptions or ParseOptions.
type Options struct {
	// Compress enables block compression (LZMA or Zstandard for data, FLAC
	// for CD audio). Default true.
	Compress bool
	// Deduplicate shares storage between sectors with identical content.
	// Default true.
	Deduplicate bool
	// Dictionary is the LZMA dictionary size in bytes. Default 32 MiB.
	Dictionary uint32
	// TableShift is the DDT table shift (multi-level fan-out exponent). A
	// negative value (default -1) selects it automatically from the image
	// size.
	TableShift int8
	// DataShift is the log2 of items per DDT block-index increment.
	// Default 12.
	DataShift uint8
	// BlockAlignment is the log2 of the on-disk block alignment. Default 9
	// (512 bytes).
	BlockAlignment uint8
	// MD5, SHA1, SHA256, Blake3 and SpamSum request whole-image checksums to
	// be computed while writing and stored on close.
	MD5     bool
	SHA1    bool
	SHA256  bool
	Blake3  bool
	SpamSum bool
	// Zstd selects Zstandard instead of LZMA for data blocks. Default false.
	Zstd bool
	// ZstdLevel is the Zstandard compression level (1-22). Default 19.
	ZstdLevel int
	// Threads is the number of compression worker threads. Default 1.
	Threads int
}

// DefaultOptions returns the default options.
func DefaultOptions() Options {
	return Options{
		Compress:       true,
		Deduplicate:    true,
		Dictionary:     33554432,
		TableShift:     -1,
		DataShift:      12,
		BlockAlignment: 9,
		ZstdLevel:      19,
		Threads:        1,
	}
}

// ParseOptions parses an AaruFormat options string: semicolon separated
// key=value pairs such as "compress=true;deduplicate=false;md5=true".
// Recognised keys are compress, deduplicate, dictionary, table_shift,
// data_shift, block_alignment, md5, sha1, sha256, blake3, spamsum, zstd,
// zstd_level and threads; booleans are true only when the value starts with
// "true". Keys are matched by prefix; unknown keys and malformed values are
// ignored.
func ParseOptions(s string) Options {
	o := DefaultOptions()
	if len(s) > 1023 { // option strings are limited to 1023 bytes
		s = s[:1023]
	}
	for _, tok := range strings.Split(s, ";") {
		if tok == "" {
			continue
		}
		key, value, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		bval := strings.HasPrefix(value, "true")
		switch {
		case strings.HasPrefix(key, "compress"):
			o.Compress = bval
		case strings.HasPrefix(key, "deduplicate"):
			o.Deduplicate = bval
		case strings.HasPrefix(key, "dictionary"):
			o.Dictionary = uint32(parseUintPrefix(value))
			if o.Dictionary == 0 {
				o.Dictionary = 33554432
			}
		case strings.HasPrefix(key, "table_shift"):
			if v, ok := parseIntPrefix(value); ok {
				o.TableShift = int8(max(-128, min(127, v)))
			}
		case strings.HasPrefix(key, "data_shift"):
			if v, ok := parseIntPrefix(value); ok && v >= 0 {
				o.DataShift = uint8(min(255, v))
				if o.DataShift == 0 {
					o.DataShift = 12
				}
			}
		case strings.HasPrefix(key, "block_alignment"):
			if v, ok := parseIntPrefix(value); ok && v >= 0 {
				o.BlockAlignment = uint8(min(255, v))
				if o.BlockAlignment == 0 {
					o.BlockAlignment = 9
				}
			}
		case strings.HasPrefix(key, "md5"):
			o.MD5 = bval
		case strings.HasPrefix(key, "sha1"):
			o.SHA1 = bval
		case strings.HasPrefix(key, "sha256"):
			o.SHA256 = bval
		case strings.HasPrefix(key, "blake3"):
			o.Blake3 = bval
		case strings.HasPrefix(key, "spamsum"):
			o.SpamSum = bval
		case strings.HasPrefix(key, "zstd_level"):
			v, _ := parseIntPrefix(value)
			o.ZstdLevel = int(max(1, min(22, v)))
		case strings.HasPrefix(key, "zstd"):
			o.Zstd = bval
		case strings.HasPrefix(key, "threads"):
			v, _ := parseIntPrefix(value)
			o.Threads = int(max(1, v))
		}
	}
	return o
}

// String formats the options in the syntax accepted by ParseOptions.
func (o Options) String() string {
	b := func(v bool) string { return strconv.FormatBool(v) }
	parts := []string{
		"compress=" + b(o.Compress),
		"deduplicate=" + b(o.Deduplicate),
		"dictionary=" + strconv.FormatUint(uint64(o.Dictionary), 10),
		"table_shift=" + strconv.Itoa(int(o.TableShift)),
		"data_shift=" + strconv.Itoa(int(o.DataShift)),
		"block_alignment=" + strconv.Itoa(int(o.BlockAlignment)),
		"md5=" + b(o.MD5),
		"sha1=" + b(o.SHA1),
		"sha256=" + b(o.SHA256),
		"blake3=" + b(o.Blake3),
		"spamsum=" + b(o.SpamSum),
		"zstd=" + b(o.Zstd),
		"zstd_level=" + strconv.Itoa(o.ZstdLevel),
		"threads=" + strconv.Itoa(o.Threads),
	}
	return strings.Join(parts, ";")
}

// explicitTableShift reports whether TableShift should be honoured instead of
// being chosen automatically (table_shift given and != -1). Negative values
// other than -1 are honoured as 0.
func (o *Options) explicitTableShift() bool {
	return o.TableShift != -1
}

// parseIntPrefix parses a leading (optionally signed) decimal integer like
// strtol, reporting whether any digits were consumed. Values are clamped to
// the int64 range.
func parseIntPrefix(s string) (int64, bool) {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	var v int64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if v < (1<<62)/5 {
			v = v*10 + int64(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// parseUintPrefix parses a leading decimal integer like strtoul (32-bit wrap
// semantics are not reproduced; overflowing values saturate).
func parseUintPrefix(s string) uint64 {
	v, ok := parseIntPrefix(s)
	if !ok {
		return 0
	}
	if v < 0 {
		return uint64(uint32(v)) // strtoul negates modulo 2^N; callers truncate to uint32
	}
	return uint64(v)
}
