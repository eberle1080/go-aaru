// Package udf is a tolerant, archival-grade reader for the Universal Disk
// Format (ECMA-167 with the OSTA UDF 1.02 to 2.60 profiles), the file system
// of DVDs, Blu-rays and packet-written CDs. It enumerates a whole volume
// (directories, files, named streams and every attribute the disc records)
// through an io.ReaderAt and reads file contents, for importing the contents
// of optical disc images (see Image.UserDataReaderAt in the parent package).
//
// # Supported
//
//   - Volume recognition (BEA01/NSR02/NSR03/TEA01), anchors at block 256,
//     the last block and 256 before it, and block 512, multisession volumes
//     via Options.SessionStart; block sizes of 512 to 4096 bytes, detected.
//   - Main and reserve volume descriptor sequences with descriptor pointers:
//     primary, implementation use, partition, logical volume, unallocated
//     space and terminating descriptors (the highest sequence number wins),
//     and the logical volume integrity sequence (file and directory counts,
//     revisions, open/closed state).
//   - Partition maps: physical (type 1), sparable (sparing tables, remapped
//     packets), virtual (UDF 1.50 and 2.00+ virtual allocation tables of
//     write-once discs) and metadata (UDF 2.50+, with the metadata mirror
//     file as a fallback).
//   - File set descriptor chains, the root and system stream directories.
//   - ICB strategies 4 and 4096 (indirect entries to the newest entry), file
//     entries and extended file entries, short, long and extended allocation
//     descriptors, data embedded in the file entry, allocation extent
//     descriptor chains, unrecorded and unallocated extents (read as zeros).
//   - File identifiers across extent boundaries, hidden, deleted and parent
//     entries; OSTA CS0 names (compression IDs 8, 16, 254 and 255).
//   - Timestamps with their time zone; extended attributes (all kept raw;
//     device specifications, file times, Mac Finder info decoded); named
//     streams; symbolic links; hard links.
//
// Not supported: the unallocated space and freed space tables and bitmaps
// (they describe free space, not files), real-time files as such, and
// writing.
//
// # Tolerance
//
// A volume or entry is never rejected because of a cosmetic or optional
// problem: bad tag checksums, CRCs or locations when the descriptor is
// otherwise sane, odd timestamps, unknown entity identifiers, malformed
// names and the like are recorded as warnings (Volume.Warnings). Only
// structural impossibilities are errors, and they are reported per entry
// or per directory (Walk carries on), so the rest of the volume can still
// be read. Directories are read block by block when a read fails, and
// unparseable directory data is skipped up to the next file identifier. No
// input causes a panic; allocations are bounded by the volume size and the
// limits in Options, and every chain (descriptor pointers, integrity
// extents, file set descriptors, indirect entries, allocation extents,
// directories) is loop-checked.
//
// # Write-once, sparable and metadata partitions
//
// The virtual allocation table of a write-once disc is the file entry in the
// last recorded block of the session. Open searches backwards from
// Options.LastBlock (by default the end of the source) for up to
// Options.MaxVATSearch blocks, so an image with unrecorded blocks after the
// last session still opens; pass LastBlock when the session end is known.
//
// A sparable partition's packets are remapped through the first valid
// sparing table. A metadata partition is read through its metadata file;
// when that file's entry is unreadable the mirror file is used instead, and
// a block that cannot be read through one is read through the other.
//
// # Hard links and streams
//
// Several file identifiers may refer to the same file entry: each becomes
// an Entry, with the same ICB (and UniqueID), so a consumer can link them.
// A directory reached twice is reported by Walk as a loop.
//
// Named streams (extended file entries' stream directories, UDF 2.00+:
// macOS records extended attributes and resource forks there) are listed in
// Entry.Streams with paths of the form "/file:stream" and read with
// OpenStream; Walk does not visit them. The file set's system streams are
// listed by Volume.SystemStreams.
//
// # Permissions
//
// Entry.Mode maps the UDF owner, group and other read, write and execute
// bits to their POSIX equivalents (the change-attribute and delete bits
// have none and are only in Entry.Permissions), setuid, setgid and sticky
// from the ICB flags, and the file type to the fs.FileMode type bits.
//
// # Concurrency
//
// A Volume is not safe for concurrent use: ReadDir and Walk append to the
// warning list, and the source reader may not be safe either (an aaru.Image
// is not). Files opened with OpenFile share the volume's reader.
package udf
