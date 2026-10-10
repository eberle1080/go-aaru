package aaru

import (
	"fmt"

	"github.com/eberle1080/go-aaru/crc64"
)

// Optical disc tracks.

// maxDataTrackSequence is the highest track sequence number considered a
// data track (the rest are, e.g., lead-in/lead-out pseudo tracks).
const maxDataTrackSequence = 99

// processTracksBlock loads the tracks block referenced by e.
// Problems are not fatal: the image is opened
// without tracks.
func (img *Image) processTracksBlock(e IndexEntry) {
	var hb [tracksHeaderSize]byte
	if err := img.readFullAt(hb[:], e.Offset); err != nil {
		img.tracksHeader = TracksHeader{}
		return
	}
	h, _ := unmarshal[TracksHeader](hb[:])
	if h.Identifier != BlockTypeTracksBlock || h.Entries == 0 {
		// A wrong identifier or an empty track list is treated as no tracks
		// block at all.
		img.tracksHeader = TracksHeader{}
		return
	}
	img.tracksHeader = h
	img.info.ImageSize += trackEntrySize * uint64(h.Entries)

	raw, err := img.readAt(e.Offset+tracksHeaderSize, trackEntrySize*uint64(h.Entries))
	if err != nil {
		img.tracksHeader = TracksHeader{}
		img.tracks = nil
		return
	}
	if !img.crcMatches(crc64.Checksum(raw), h.CRC64) {
		// Attempt erasure coding recovery. If it fails, the (damaged)
		// entries are kept, so Tracks and the track sector tags still
		// see them, but does not mark the image as having partitions nor
		// build the data track list.
		rec, rerr := img.ecRecoverMetaBlock(e.Offset)
		var rh TracksHeader
		size := 0
		if rerr == nil && len(rec) >= tracksHeaderSize {
			rh, _ = unmarshal[TracksHeader](rec)
			size = trackEntrySize * int(rh.Entries)
		}
		if rerr != nil || len(rec) < tracksHeaderSize || rh.Entries == 0 || len(rec) < tracksHeaderSize+size {
			img.tracks, _ = unmarshalSlice[TrackEntry](raw, int(h.Entries))
			return
		}
		img.tracksHeader = rh
		raw = rec[tracksHeaderSize : tracksHeaderSize+size]
	}

	tracks, err := unmarshalSlice[TrackEntry](raw, int(img.tracksHeader.Entries))
	if err != nil {
		img.tracksHeader = TracksHeader{}
		return
	}
	img.tracks = tracks
	img.info.HasPartitions = true
	img.info.HasSessions = true
	img.dataTracks = filterDataTracks(tracks)
}

// filterDataTracks returns the tracks whose sequence is at most 99.
func filterDataTracks(tracks []TrackEntry) []TrackEntry {
	var out []TrackEntry
	for _, t := range tracks {
		if t.Sequence <= maxDataTrackSequence {
			out = append(out, t)
		}
	}
	return out
}

// Tracks returns a copy of the optical disc tracks, or
// ErrTrackNotFound if the image has none.
func (img *Image) Tracks() ([]TrackEntry, error) {
	if img.closed {
		return nil, ErrClosed
	}
	if img.tracksHeader.Entries == 0 || len(img.tracks) == 0 {
		return nil, fmt.Errorf("image contains no tracks: %w", ErrTrackNotFound)
	}
	return append([]TrackEntry(nil), img.tracks...), nil
}

// SetTracks replaces the optical disc tracks. An empty
// slice clears them. The tracks block is written when the image is closed.
// At most 65535 tracks can be stored.
func (img *Image) SetTracks(tracks []TrackEntry) error {
	if img.closed {
		return ErrClosed
	}
	if len(tracks) == 0 {
		img.tracksHeader = TracksHeader{}
		img.tracks = nil
		img.dataTracks = nil
		return nil
	}
	if len(tracks) > 0xFFFF {
		return fmt.Errorf("%d tracks: %w", len(tracks), ErrInvalidTrackFormat)
	}
	img.tracks = append([]TrackEntry(nil), tracks...)
	img.tracksHeader = TracksHeader{
		Identifier: BlockTypeTracksBlock,
		Entries:    uint16(len(tracks)),
		CRC64:      crc64.Checksum(marshal(img.tracks)),
	}
	img.info.HasPartitions = true
	img.info.HasSessions = true
	img.dirty.tracksBlock = true
	img.dataTracks = filterDataTracks(img.tracks)
	return nil
}

// writeTracksBlock writes the TRKS block during finalization at the aligned
// end of the file and indexes it. Called by finalizeWrite when the tracks block is dirty.
func (img *Image) writeTracksBlock() error {
	if img.tracksHeader.Entries == 0 || len(img.tracks) == 0 {
		return nil
	}
	off := img.appendOffset()
	buf := make([]byte, 0, tracksHeaderSize+trackEntrySize*len(img.tracks))
	buf = appendMarshal(buf, img.tracksHeader)
	buf = appendMarshal(buf, img.tracks)
	if err := img.writeAt(buf, off); err != nil {
		return fmt.Errorf("writing tracks block at %d: %w: %w", off, ErrCannotWriteBlockData, err)
	}
	img.setIndexEntry(BlockTypeTracksBlock, 0, off)
	return nil
}
