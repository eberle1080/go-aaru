package aaru

import (
	"io"
	"os"
)

// identifyConfidence is the confidence returned by Identify for AaruFormat
// images.
const identifyConfidence = 100

// Identify reports how confident it is that the file at path is an
// AaruFormat image: 100 when the header identifier is
// AaruFormat's (or DiscImageChef's) and the version is supported, 0
// otherwise. Errors opening the file are returned (e.g. wrapping
// fs.ErrNotExist); an empty path yields an error wrapping fs.ErrInvalid.
func Identify(path string) (int, error) {
	if path == "" {
		return 0, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return IdentifyReader(f, -1)
}

// IdentifyReader is Identify for an image held in r.
// size is not needed and may be -1. A nil r, or a stream too short to hold
// a header, yields 0.
func IdentifyReader(r io.ReaderAt, size int64) (int, error) {
	if r == nil {
		return 0, nil
	}
	var b [headerV1Size]byte
	if n, _ := r.ReadAt(b[:], 0); n != len(b) {
		return 0, nil
	}
	h := decodeImageHeader(b[:])
	if (h.Identifier == DICMagic || h.Identifier == AaruMagic) && h.ImageMajorVersion <= FormatVersion {
		return identifyConfidence, nil
	}
	return 0, nil
}
