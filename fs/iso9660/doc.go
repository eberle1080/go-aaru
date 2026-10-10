// Package iso9660 is a tolerant, archival-grade ISO 9660 reader. It
// enumerates a whole volume (directories, files and every attribute the disc
// records) through an io.ReaderAt and reads file contents, for importing the
// contents of optical disc images.
//
// # Supported
//
//   - Volume descriptors: primary (PVD), supplementary and enhanced (SVD/EVD),
//     Joliet (UCS-2 levels 1-3), boot records (El Torito catalog location),
//     volume partition descriptors (presence only), multisession volumes via
//     Options.BaseLBA.
//   - Directory records: version numbers, hidden/associated/record/protection
//     flags, recording times with their GMT offset, extended attribute records
//     (file data starts after them), interleaved files (file unit size and
//     interleave gap) and multi-extent files (larger than 4 GiB).
//   - CD-ROM XA system use (owner, group, attributes, file number).
//   - SUSP: SP (with its skip length), CE continuation areas (chained), ST, PD,
//     ER, ES, plus the Apple "AA" Finder info extension.
//   - Rock Ridge (RRIP 1.09/1.10/1.12): PX, PN, SL, NM, CL/PL/RE (deep
//     directory relocation), TF (all seven timestamps, short and long forms),
//     SF and ZF (recorded, not interpreted) and the old RR entry.
//
// Not supported: UDF (a separate package will read it), High Sierra volumes,
// decompressing zisofs (ZF) files, sparse (SF) file contents, and CD-ROM XA
// Mode 2 Form 2 file data, which does not fit 2048-byte sectors (read it from
// raw sectors; Entry.XA tells such files apart).
//
// # Tolerance
//
// A volume or entry is never rejected because of a cosmetic or optional
// field: unparseable dates, NUL-padded identifiers, inconsistent both-endian
// fields, malformed SUSP entries and the like are recorded as warnings
// (Volume.Warnings). Only structural impossibilities are errors (no volume
// descriptor, extents outside the volume, directory loops), and they are
// reported per directory or per file, so the rest of the volume can still be
// read. Directories are salvaged sector by sector: an unreadable sector
// (for example one that was never dumped) loses only the records it held.
// No input causes a panic, and allocations are bounded by the volume size
// and the limits in Options.
//
// # Namespaces
//
// A disc can carry up to three views of the same files: plain ISO 9660 names
// in the PVD tree, Rock Ridge (POSIX names and attributes, also in the PVD
// tree) and Joliet (Unicode names in a separate SVD tree). Volume.Namespaces
// lists those present; Options.Namespace picks one. The default is Rock Ridge
// when its NM names are in use, else Joliet, else Rock Ridge attributes with
// ISO names, else plain ISO 9660. SUSP entries are honoured in whichever tree
// is read, so the Joliet tree of a disc that also records Rock Ridge
// attributes there (macOS hdiutil does) gets Joliet names and POSIX
// attributes.
//
// Joliet names are not correlated onto the PVD tree: the Joliet tree's
// directory records carry the same ISO 9660 metadata (dates, flags, extents)
// as the PVD's, so reading it loses only the 8.3 names and any XA system use.
// A caller that wants both can open the volume twice with different
// namespaces and match files by Entry.Extents[0].LBA.
//
// # Associated files and relocated directories
//
// Associated files (flag 0x04, typically Macintosh resource forks on hybrid
// discs) are returned by ReadDir as ordinary entries with Associated set,
// next to the main file of the same name; the main file's AssociatedFile
// points at it. A consumer mirroring the volume should store the associated
// entry as an attribute (for example a resource fork) of the main file rather
// than as a second file of the same path.
//
// In the Rock Ridge namespace, a directory that was relocated to keep the
// ISO 9660 tree within eight levels appears at its logical place: the CL
// placeholder becomes a directory entry (Entry.Relocated) whose extent and
// attributes are those of the real directory, and the real directory (marked
// RE) is hidden from its physical parent (normally "rr_moved", which then
// looks empty, as it does under Linux). Options.ShowRelocated lists it
// there too.
//
// # Concurrency
//
// A Volume is not safe for concurrent use: ReadDir and Walk append to the
// warning list, and the source reader may not be safe either (an aaru.Image
// is not). Files opened with OpenFile share the volume's reader.
package iso9660
