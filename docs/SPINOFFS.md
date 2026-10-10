# Spin-off candidates (code written for go-aaru that could generalize)

Rating: high = clear gap in the Go ecosystem; medium = useful but alternatives exist; low = niche.

| Package | What it is | Rating | Generalization work needed |
|---|---|---|---|
| compression/flac | Pure-Go FLAC encoder (LPC, fixed, Rice partitioning, all standard apodization windows) + decoder | high | any channels/bit depth/rate, streaming io.Reader/Writer API, metadata blocks (Vorbis comment, seektable, picture), presets -0..-8, optional MD5 verify, official FLAC conformance streams |
| compression/lzma | Pure-Go raw LZMA encoder+decoder | high | streaming API, .lzma/.xz container support or adapters, LZMA2, multi-threading |
| cdecc | CD-ROM EDC/ECC (Mode 1, Mode 2 Form 1/2) check/generate, sector reconstruction, LBA<->MSF | medium-high | generic CD sector types (incl. Mode 0, CD-i), scrambling, subchannel Q CRC; tidy API |
| console/ps3, console/wii, console/wiiu, console/ngcw (+ ird, sfo, WUD/WUX reader) | Console disc crypto: PS3 disc key/sector AES, Wii group crypto + H0-H3 layout, Wii U partition crypto, GameCube/Wii junk LFG (seed find/regenerate), IRD/SFO parsers | medium | one "go-consoledisc" module; partition table parsing, ticket/TMD handling, full disc readers |
| fs/iso9660 | Tolerant ISO 9660 reader (Joliet, Rock Ridge incl. relocation and CE chains, XA, multi-extent, interleaved files) over an io.ReaderAt | medium-high | UDF beside it, io/fs.FS adapter, zisofs (ZF) decompression |
| spamsum | ssdeep/SpamSum fuzzy hash (ssdeep-compatible digests, hash.Hash-style writer) | medium | add digest comparison (match score), check vs other Go ssdeep libs |
| internal/reedsolomon + gf256 | Vandermonde RS erasure codec over GF(2^8) poly 0x11D, incremental encode | low-medium | only valuable if specific matrix compatibility needed (klauspost/reedsolomon exists) |
| crc64 | CRC-64/XZ slicing-by-16 (~20% faster than stdlib) | low | could be upstreamed as a stdlib perf CL instead |
| compression/cst, lisatag, media | Aaru-specific (subchannel transform, Apple Lisa tags, media type table) | low | none |
| cmd console converters (Wii U TOC/FST/ticket, Wii partition/ticket, GC/Wii FST data maps + junk detection) | belongs with the console-disc spin-off | medium | move parsers into console/* |
