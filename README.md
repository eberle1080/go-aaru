# go-aaru

[![Go Reference](https://pkg.go.dev/badge/github.com/eberle1080/go-aaru.svg)](https://pkg.go.dev/github.com/eberle1080/go-aaru)

**go-aaru** is a standalone, pure Go implementation of the **AaruFormat** disk image format, the image
format of the [Aaru Data Preservation Suite](https://github.com/aaru-dps/Aaru).

AaruFormat stores everything needed to preserve a physical medium: user data in compressed,
deduplicated blocks, plus the parts most formats throw away. That includes per-sector tags (CD
sync/headers/EDC/ECC, subchannel, DVD IED/EDC), whole-media tags (TOC, PFI, ATIP, ...), tracks,
geometry, dump hardware, checksums, flux captures, erasure-coding parity and console disc encryption
metadata.

- **Pure Go.** Builds for any platform Go supports.
- **Compatible with the AaruFormat format.** go-aaru reads existing images written by Aaru, and the images
  it writes follow the same on-disk format, so other AaruFormat readers can open them.
- **Feature-complete.** Read, create, resume, verify, identify, every block type, erasure coding,
  PS3/Wii/Wii U/GameCube support. The `aaruformattool` command line utility is included as well.
- **Hardened.** Malformed images return errors instead of crashing, and allocations are bounded by the
  file size. Parsers are fuzzed.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage guide](#usage-guide)
- [Command line tool](#command-line-tool)
- [Packages](#packages)
- [Compatibility and validation](#compatibility-and-validation)
- [Performance](#performance)
- [Testing](#testing)
- [Dependencies and licensing](#dependencies-and-licensing)

## Installation

```sh
go get github.com/eberle1080/go-aaru
```

Requires Go 1.26 or newer.

## Quick start

### Reading an image

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/eberle1080/go-aaru"
)

func main() {
	img, err := aaru.Open("disk.aif")
	if err != nil {
		log.Fatal(err)
	}
	defer img.Close()

	info := img.Info()
	fmt.Printf("%v: %d sectors of %d bytes, created by %s %s\n",
		info.MediaType, info.Sectors, info.SectorSize, info.Application, info.ApplicationVersion)

	for lba := uint64(0); lba < info.Sectors; lba++ {
		data, status, err := img.ReadSector(lba, false)
		switch {
		case errors.Is(err, aaru.ErrSectorNotDumped):
			continue // the sector was never captured
		case err != nil:
			log.Fatalf("sector %d: %v", lba, err)
		}
		_ = data   // user data of the sector
		_ = status // e.g. aaru.SectorStatusDumped, SectorStatusErrored, ...
	}
}
```

### Writing an image

```go
opts := aaru.ParseOptions("compress=true;deduplicate=true;md5=true;sha1=true")
// or: opts := aaru.DefaultOptions(); opts.MD5 = true

img, err := aaru.Create("floppy.aif", aaru.CreateParams{
	MediaType:               media.DOS_35_HD,
	SectorSize:              512,
	UserSectors:             2880,
	Application:             "myapp",
	ApplicationMajorVersion: 1,
}, &opts)
if err != nil {
	return err
}

for lba := uint64(0); lba < 2880; lba++ {
	if err := img.WriteSector(lba, false, sector(lba), aaru.SectorStatusDumped); err != nil {
		img.Close()
		return err
	}
}
img.SetGeometry(80, 2, 18)
img.SetCreator("Jane Doe")

// Close finalizes the image: it flushes the last block and writes the
// deduplication tables, metadata, checksums and index. Always check its error.
return img.Close()
```

## Usage guide

### Opening images

| Function | Purpose |
|---|---|
| `aaru.Open(path)` | Open an image read-only. |
| `aaru.OpenReader(r io.ReaderAt, size)` | Open an image from any `io.ReaderAt` (memory, network, archive...). |
| `aaru.Create(path, params, opts)` | Create a new image. |
| `aaru.Resume(path, opts)` | Reopen an existing image to keep writing (append sectors, change metadata). |
| `aaru.Identify(path)` / `IdentifyReader` | Return a 0–100 confidence that a file is an AaruFormat image. |

Both version 2 images and legacy version 1 (DiscImageChef) images can be read. Writing always
produces version 2. If the primary header is damaged, it is recovered from the backup header in the
erasure-coding recovery footer when one is present.

An `*Image` is not safe for concurrent use. Serialize access or open the file several times.

### Reading data

```go
data, status, err := img.ReadSector(lba, false)          // user data
data, status, err  = img.ReadSector(10, true)            // negative sector -10 (e.g. CD lead-in/pregap)
raw, status, err  := img.ReadSectorLong(lba, false)      // full raw sector (CD: 2352 bytes with sync/header/EDC/ECC)
data, status, err  = img.ReadTrackSector(lba, 2)         // sector relative to track 2
sub, err          := img.ReadSectorTag(lba, false, aaru.SectorTagCdSubchannel)
toc, err          := img.ReadMediaTag(aaru.MediaTagFullToc)
```

- **Negative and overflow sectors.** The `negative` flag addresses sectors before LBA 0, such as the CD
  lead-in. Sectors past the user area (overflow) are read with `negative=false` and an address ≥
  `UserSectors()`.
- **Long sectors are rebuilt.** CD prefixes and suffixes that were correct when dumped are regenerated
  from the stored data and the ECC, so `ReadSectorLong` returns the original 2352 bytes. DVD, Blu-ray,
  Apple and Priam long sectors work too.
- **Readable tags.** `img.ReadableSectorTags()` and `img.ReadableMediaTags()` list what an image contains.
- **Not-dumped sectors.** These return `ErrSectorNotDumped` with `SectorStatusNotDumped`. Every other
  status (`Errored`, `Unrecorded`, `Encrypted`, ...) is returned alongside the data.

### Optical media

```go
tracks, _ := img.Tracks()
for _, t := range tracks {
	fmt.Printf("track %2d  %-16v  %d-%d  session %d\n", t.Sequence, t.Type, t.Start, t.End, t.Session)
}
```

When writing optical images, call `SetTracks` before writing sectors. The writer uses the track types to
choose compression (FLAC for audio, LZMA or Zstandard for data). Then hand complete raw sectors to
`WriteSectorLong`:

```go
img.SetTracks([]aaru.TrackEntry{{
	Sequence: 1, Type: aaru.TrackTypeCdMode1, Start: 0, End: int64(len(raw)) - 1, Session: 1,
}})
for lba, sector := range raw { // 2352-byte raw sectors
	if err := img.WriteSectorLong(uint64(lba), false, sector, aaru.SectorStatusDumped); err != nil {
		return err
	}
}
```

The writer analyses each raw sector:
- Correct sync patterns, headers and EDC/ECC are stored as flags only.
- Anomalous ones are stored verbatim.
- Mode 2 Form 1/Form 2 and no-CRC sectors are detected.
- Subchannel data is compressed with the CD Subchannel Transform.

Use `WriteSectorTag` for subchannel, track flags and ISRC, and `WriteMediaTag` for TOC, ATIP, PFI, BCA
and so on.

### Disc file systems

`img.UserDataReaderAt()` exposes the 2048-byte user data of every sector as an `io.ReaderAt`, which the
`fs/iso9660` package reads as an ISO 9660 volume (Joliet, Rock Ridge, CD-ROM XA, multi-extent and
interleaved files) and the `fs/udf` package as a UDF volume (revisions 1.02 to 2.60: DVDs, Blu-rays,
packet-written and write-once discs). HFS and HFS+ are not supported.

```go
r, err := img.UserDataReaderAt()
if err != nil {
	return err
}
vol, err := iso9660.Open(r, r.Size(), nil) // picks Rock Ridge, Joliet or plain ISO 9660
if err != nil {
	return err
}
err = vol.Walk(func(path string, e *iso9660.Entry, err error) error {
	if err != nil {
		log.Printf("%s: %v", path, err) // a damaged directory or file does not stop the walk
		return nil
	}
	if e.IsDir {
		return nil
	}
	f, err := vol.OpenFile(e) // io.ReadSeeker + io.ReaderAt over all the file's extents
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, f) // errors.Is(err, aaru.ErrSectorNotDumped) for missing sectors
	return err
})
for _, w := range vol.Warnings() { // irregularities that did not prevent reading
	log.Print(w)
}
```

The reader never rejects a volume over a cosmetic field (NUL-filled dates, NUL-padded labels, a Rock
Ridge entry without a name...): such problems become warnings. `vol.Namespaces()` lists the views a
disc offers and `iso9660.Options.Namespace` picks one.

`fs/udf` has the same shape. On a UDF bridge disc (most DVDs) try UDF first: it carries the POSIX
attributes, named streams and long Unicode names the ISO 9660 side may lack.

```go
vol, err := udf.Open(r, r.Size(), nil) // block size, partitions (VAT, sparable, metadata) detected
if err != nil {
	return err
}
err = vol.Walk(func(path string, e *udf.Entry, err error) error {
	if err != nil {
		log.Printf("%s: %v", path, err)
		return nil
	}
	fmt.Println(path, e.Mode(), e.Size, e.ModTime(), e.SymlinkTarget)
	for _, s := range e.Streams { // named streams, e.g. macOS extended attributes
		f, err := vol.OpenStream(s)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, f)
	}
	return nil
})
```

Hard links show up as entries with the same `e.ICB`. For a write-once disc whose image extends past
the last session, pass `udf.Options{LastBlock: ...}` so the virtual allocation table is found.

### Metadata

```go
img.SetMediaTitle("My Disc")                  // string getters/setters for all 12 descriptive fields
title, err := img.MediaTitle()                // ErrMetadataNotPresent if absent
img.SetMediaSequence(1, 2)                    // disc 1 of 2
img.SetDumpHardware([]aaru.DumpHardware{{
	Manufacturer: "PLEXTOR", Model: "PX-760A", Firmware: "1.07",
	SoftwareName: "Aaru", SoftwareVersion: "6.0",
	Extents: []aaru.DumpExtent{{Start: 0, End: 359999}},
}})
img.SetAaruJSONMetadata(sidecarJSON)
sums := img.Checksums()                       // whole-image MD5/SHA-1/SHA-256/BLAKE3/SpamSum, if stored
```

- **Raw metadata access.** `MetadataRaw` / `SetMetadataRaw` read and write the stored UTF-16LE strings
  exactly as they are stored in the image.
- **Other metadata APIs:**
  - geometry
  - CICM XML
  - tape files and partitions
  - data position measurements (`DPM`, used for copy-protection angle data)
  - flux captures (`WriteFluxCapture` / `ReadFluxCapture` / `FluxCaptures`)

Flux-only images (no sector data) are created with zero sector counts.

### Options

`aaru.Options` controls image creation. `ParseOptions` accepts AaruFormat option strings such as
`"compress=true;md5=true"`, and `DefaultOptions()` returns the standard defaults.

| Option | Default | Meaning |
|---|---|---|
| `Compress` | `true` | Compress blocks (LZMA, or Zstandard; FLAC for CD audio) |
| `Deduplicate` | `true` | Store identical sectors once |
| `Zstd`, `ZstdLevel` | `false`, 19 | Use Zstandard instead of LZMA for data blocks |
| `Dictionary` | 32 MiB | LZMA dictionary size |
| `MD5`, `SHA1`, `SHA256`, `Blake3`, `SpamSum` | `false` | Whole-image checksums computed while writing |
| `DataShift` | 12 | log2 of sectors per data block |
| `BlockAlignment` | 9 | log2 of on-disk block alignment |
| `TableShift` | -1 (auto) | Multi-level deduplication table fan-out |

### Erasure coding

Erasure coding adds Reed-Solomon parity so damaged blocks are reconstructed transparently on read.
Enable it right after `Create`, before writing any sector:

```go
img.SetErasureCoding(aaru.ErasureCodingRsVandermonde, 8, 2) // RS(8,2): any 2 of every 10 blocks may be lost
// or
img.SetErasureCodingAuto(25) // ~25% overhead, picks K and M
```

Deduplication tables, metadata and the index are protected too. A recovery footer at the end of the file
keeps a backup of the header.

### Console discs

- **PS3, Wii U and Wii.** These images can store sectors decrypted (`SectorStatusUnencrypted`) together
  with the keys in media tags. `ReadSector` re-encrypts them on the fly, so readers get the original disc
  bytes.
- **GameCube and Wii junk.** Padding sectors (`SectorStatusGenerable`) are regenerated from their seeds
  instead of being stored.
- **Converting disc images.** The `aaruformattool convert-ps3`, `convert-wiiu` and `convert-ngcw` verbs
  build such images from ISO/WUD/WUX/GCM dumps. The crypto building blocks are in `console/...`.

### Verifying

```go
if err := img.Verify(); err != nil {
	log.Fatalf("image is damaged: %v", err) // errors.Is(err, aaru.ErrInvalidBlockCRC), ...
}
```

`Verify` checks the CRC64 of every indexed block.

### Errors

Errors are sentinel values of type `*aaru.Error`, one per AaruFormat status code, often wrapped with
context. Test them with `errors.Is`:

```go
_, _, err := img.ReadSector(lba, false)
if errors.Is(err, aaru.ErrSectorOutOfBounds) { ... }
code := aaru.ErrorCode(err) // the AaruFormat numeric status code, e.g. -5
```

I/O errors from the operating system are returned as-is, for example a `*fs.PathError` from `Open`.

## Command line tool

`cmd/aaruformattool` is a command line utility for inspecting, converting and repairing AaruFormat
images:

```sh
go install github.com/eberle1080/go-aaru/cmd/aaruformattool@latest
aaruformattool info disk.aif
aaruformattool <verb> --help
```

| Verb | Description |
|---|---|
| `identify` | Check whether a file is an AaruFormat image |
| `info` | Print header, media, tracks, metadata, tags, checksums, dump hardware, flux... |
| `read`, `read_long` | Hex-dump a sector (user data / raw long sector) |
| `verify [--recover]` | Verify block integrity; optionally exercise erasure-coding recovery |
| `verify_sectors` | Check every CD sector's EDC/ECC |
| `compare` | Interactive side-by-side comparison of two images (terminal UI) |
| `cli-compare [-l]` | List differing sectors between two images |
| `convert [-l] [--erasure-coding=K,M]` | Copy an image into a new one (optionally adding parity) |
| `convert-ps3` | PS3 ISO → AaruFormat with decrypted storage (`--disc-key`, `--data1-key`, `--ird`) |
| `convert-wiiu` | WUD/WUX → AaruFormat with decrypted storage (`--disc-key`) |
| `convert-ngcw` | GameCube/Wii disc → AaruFormat with junk detection and partition decryption |
| `inject-media-tag` | Add a media tag from a file to an existing image |
| `repair-cd-arena` | Repair CD prefix/suffix data damaged by a resume bug in older AaruFormat writers |
| `upgrade-ddt-to-alpha21` | Upgrade images written by the alpha20 format |

## Packages

| Package | Contents |
|---|---|
| [`go-aaru`](https://pkg.go.dev/github.com/eberle1080/go-aaru) | Image API, on-disk structures, enumerations, errors |
| `media` | Media type enumeration (`media.CDROM`, `media.DOS_35_HD`, `media.PS3BD`, ... — the Aaru names) |
| `crc64` | CRC64-ECMA (slicing-by-16) |
| `spamsum` | SpamSum / ssdeep fuzzy hash |
| `compression/lzma` | Raw LZMA codec |
| `compression/flac` | FLAC encoder and decoder for Red Book audio |
| `compression/zstd` | Zstandard codec (wraps klauspost/compress) |
| `compression/cst` | CD Subchannel Transform |
| `cdecc` | CD-ROM EDC/ECC computation, checking and sector reconstruction |
| `lisatag` | Apple Lisa / Macintosh (Sony, ProFile) and Priam sector tag conversions |
| `console/ps3`, `console/ps3/ird`, `console/ps3/sfo` | PS3 disc crypto, encryption maps, IRD and PARAM.SFO parsers |
| `console/wiiu` | Wii U disc crypto and WUD/WUX reader |
| `console/wii`, `console/ngcw` | Wii partition crypto, GameCube/Wii junk generator (LFG) and junk maps |
| `console/iso9660` | Minimal ISO 9660 file reader (locates PS3 PARAM.SFO) |
| `fs/iso9660` | Tolerant ISO 9660 reader: Joliet, Rock Ridge, CD-ROM XA, El Torito detection, multi-extent and interleaved files; whole-volume walks for archival import |
| `fs/udf` | Tolerant UDF 1.02-2.60 reader: physical, sparable, virtual (VAT) and metadata partitions, file entries and extended file entries, named streams, symlinks, extended attributes; whole-volume walks for archival import |

## Validation

All test inputs are generated by the tests themselves; there are no stored images or recorded
outputs from other software.

- **Scenarios.** Write scripts covering every media family, block type and option combination are run,
  and every image is read back against a model of what was written: info, tracks, media tags, every
  sector, long sector and sector tag, plus the expected status codes. Randomized scenarios run with
  multiple seeds.
- **Version 1 images and malformed images** are built byte by byte in the tests and must open, read
  and fail exactly as specified.
- **Round trips.** Images are written, resumed, read back and verified, including corrupted
  erasure-coded images that must be recovered.
- **Leaf packages** (checksums, codecs, CD EDC/ECC, Reed-Solomon, Lisa tags, console crypto) are
  tested against their mathematical definitions, round trips and published standard vectors
  (CRC-64/ECMA-182, RFC 1321, FIPS 180, NIST SP 800-38A).
- **Command line tool.** Every verb is tested against expected-output snapshots produced by the tool
  itself (`go test ./cmd/aaruformattool -update` rewrites them).
- **Fuzzing.** Image opening, the codecs, the console parsers, the ISO 9660 and UDF readers and
  erasure-coding maps are fuzzed. ISO 9660 and UDF test volumes are built byte by byte by the tests;
  when macOS `hdiutil` and `newfs_udf` are available, volumes they write are cross-checked too
  (`hdiutil makehybrid` bridge discs with files; freshly formatted `newfs_udf` volumes of every revision).
  The tests never mount anything: attaching UDF images panicked the macOS 27 kernel. A comparison of
  populated real-tool UDF volumes is still to do, on Linux.

Behavior notes (none affect the on-disk format):

- Malformed input never causes out-of-bounds access, panics or unbounded allocations; it returns errors.
- Erasure-coded images verify block CRCs on read, so silent corruption triggers recovery. Recovered
  sectors are re-encrypted like any other sector.
- Format quirks that existing images depend on are kept and documented in the code, for example the
  "XOR" erasure algorithm actually using Vandermonde coefficients, and the gap after each full parity
  stripe.

## Performance

Indicative numbers on an Apple M4 Max, single-threaded unless noted:

| Operation | Throughput |
|---|---|
| Write, uncompressed | ~1.2 GB/s, no per-sector allocations |
| LZMA decode | ~215 MB/s |
| Zstandard decode | ~800 MB/s |
| FLAC encode | ~32 MB/s single-threaded, ~230 MB/s with workers |
| CRC64 | ~2.8 GB/s |

## Testing

```sh
go test ./...                        # full suite (about a minute)
go test -short -race -timeout 60m ./...  # race detector; the root package alone takes ~25 minutes
```

The tests are self-contained: every input is generated at test time.

## Dependencies and licensing

The library depends only on permissively licensed, pure Go modules:

- [`github.com/klauspost/compress`](https://github.com/klauspost/compress) (BSD-3-Clause), for Zstandard.
- [`lukechampine.com/blake3`](https://github.com/lukechampine/blake3) (MIT).
- [`github.com/cespare/xxhash/v2`](https://github.com/cespare/xxhash) (MIT).

The command line tool also uses [`github.com/gdamore/tcell/v2`](https://github.com/gdamore/tcell)
(Apache-2.0) and [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) (BSD-3-Clause) for its
interactive compare UI.

go-aaru's own license is in [LICENSE](LICENSE).

Components that could be split out into standalone libraries are tracked in
[docs/SPINOFFS.md](docs/SPINOFFS.md).
