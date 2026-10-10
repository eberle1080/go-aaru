package aaru

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// This file implements reading of the checksum block.
// The checksum block itself is written by writeChecksumBlock (close_write.go).

// Digest lengths of the whole-image checksums.
const (
	md5DigestLength    = 16
	sha1DigestLength   = 20
	sha256DigestLength = 32
	blake3DigestLength = 32
)

// processChecksumBlock loads the whole-image checksums from the checksum
// block (CKSM) described by e. Entries with an
// unexpected digest length or an unknown algorithm are skipped; parsing
// stops at the first entry that does not fit in the block. BLAKE3 entries
// are loaded too.
func (img *Image) processChecksumBlock(e IndexEntry) error {
	var hb [checksumHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		return fmt.Errorf("reading checksum block header at %d: %w", e.Offset, ErrCannotReadHeader)
	}
	h, _ := unmarshal[ChecksumHeader](hb[:])
	if h.Identifier != BlockTypeChecksumBlock {
		return fmt.Errorf("checksum block at %d has identifier %#x: %w", e.Offset, uint32(h.Identifier),
			ErrCannotReadBlock)
	}
	data, err := img.readAt(e.Offset+checksumHeaderSize, uint64(h.Length))
	if err != nil {
		return fmt.Errorf("reading checksum block at %d: %w: %w", e.Offset, ErrCannotReadBlock, err)
	}

	c := &img.checksums
	pos := uint64(0)
	for j := 0; j < int(h.Entries); j++ {
		if pos+checksumEntrySize > uint64(len(data)) {
			break
		}
		typ := ChecksumAlgorithm(data[pos])
		n := uint64(binary.LittleEndian.Uint32(data[pos+1:]))
		pos += checksumEntrySize
		if pos+n > uint64(len(data)) {
			break
		}
		digest := data[pos : pos+n]
		switch typ {
		case ChecksumMd5:
			if n == md5DigestLength {
				c.MD5 = bytes.Clone(digest)
			}
		case ChecksumSha1:
			if n == sha1DigestLength {
				c.SHA1 = bytes.Clone(digest)
			}
		case ChecksumSha256:
			if n == sha256DigestLength {
				c.SHA256 = bytes.Clone(digest)
			}
		case ChecksumSpamSum:
			// Stored NUL-terminated: anything after a NUL is not part of it.
			if i := bytes.IndexByte(digest, 0); i >= 0 {
				digest = digest[:i]
			}
			c.SpamSum = string(digest)
		case ChecksumBlake3:
			if n == blake3DigestLength {
				c.Blake3 = bytes.Clone(digest)
			}
		}
		pos += n
	}
	return nil
}

// Checksums returns the whole-image checksums stored in the image (MD5,
// SHA-1, SHA-256, SpamSum and BLAKE3; absent ones are nil or empty). For
// images being written they are only available after Close; the checksums
// of resumed images are those loaded from the file.
func (img *Image) Checksums() Checksums {
	c := img.checksums
	return Checksums{
		MD5:     bytes.Clone(c.MD5),
		SHA1:    bytes.Clone(c.SHA1),
		SHA256:  bytes.Clone(c.SHA256),
		Blake3:  bytes.Clone(c.Blake3),
		SpamSum: c.SpamSum,
	}
}
