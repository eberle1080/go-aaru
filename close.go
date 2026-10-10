package aaru

import "errors"

// Close finalizes a writable image (flushing pending blocks and writing the
// deduplication tables, metadata, checksums and index) and releases the
// underlying file. The image must not be used afterwards. Close on an already
// closed image returns ErrClosed.
//
// The backing file is closed even if finalization fails; the finalization
// error is returned.
func (img *Image) Close() error {
	if img == nil || img.closed {
		return ErrClosed
	}
	var errs []error
	if img.writable {
		if err := img.finalizeWrite(); err != nil {
			errs = append(errs, err)
		}
	}
	img.closed = true
	if img.closer != nil {
		if err := img.closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	img.blockCache.clear()
	img.blockHeaderCache.clear()
	return errors.Join(errs...)
}
