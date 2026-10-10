package main

import (
	"fmt"
	"io"
	"strings"
)

// printhex writes data as a hexadecimal dump with width bytes per row,
// followed by the printable characters (bytes 32-126 and 160-255 as is,
// others as '.'); with color the offsets are cyan.
func printhex(w io.Writer, data []byte, width int, color bool) {
	if data == nil || width <= 0 {
		return
	}
	length := len(data)
	rows := length / width
	last := length % width
	offsetLength := len(fmt.Sprintf("%X", length))
	if last > 0 {
		rows++
	}
	if last == 0 {
		last = width
	}
	if offsetLength < len("Offset") {
		offsetLength = len("Offset")
	}

	var b strings.Builder
	if color {
		b.WriteString("\x1b[36m")
	}
	// The heading is always just "Offset", however wide the offsets are.
	b.WriteString("Offset")
	b.WriteString("  ")
	for i := 0; i < width; i++ {
		fmt.Fprintf(&b, " %02x", i)
	}
	if color {
		b.WriteString("\x1b[0m")
	}
	b.WriteString("\n")

	pos := 0
	for i := 0; i < rows; i++ {
		if color {
			b.WriteString("\x1b[36m")
		}
		fmt.Fprintf(&b, "%0*X", offsetLength, pos)
		if color {
			b.WriteString("\x1b[0m")
		}
		b.WriteString("  ")

		lastBytes := width
		if i == rows-1 {
			lastBytes = last
		}
		for j := 0; j < lastBytes; j++ {
			fmt.Fprintf(&b, " %02X", data[pos+j])
		}
		b.WriteString(strings.Repeat("   ", width-lastBytes))
		b.WriteString("   ")
		for j := 0; j < lastBytes; j++ {
			v := data[pos+j]
			if (v > 31 && v < 127) || v > 159 {
				b.WriteByte(v)
			} else {
				b.WriteByte('.')
			}
		}
		pos += lastBytes
		b.WriteString("\n")

		if b.Len() >= 64*1024 {
			_, _ = io.WriteString(w, b.String())
			b.Reset()
		}
	}
	_, _ = io.WriteString(w, b.String())
}
