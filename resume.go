package aaru

// Resume opens an existing image to continue writing it. nil opts means
// DefaultOptions().
//
// Only Compress, Deduplicate and Dictionary are taken
// from opts: the table layout and block alignment are those of the image,
// Zstandard is not used for the blocks written in the resumed session, and no
// whole-image checksums are computed (the existing checksum block, if any, is
// kept as is). New blocks are appended at the end of the file and every
// structure that changes is rewritten on Close.
func Resume(path string, opts *Options) (*Image, error) {
	o := DefaultOptions()
	if opts != nil {
		o = *opts
	}

	img, err := openFile(path, true)
	if err != nil {
		return nil, err
	}

	img.header.LastWrittenTime = nowFiletime()
	img.info.LastModificationTime = FiletimeToTime(img.header.LastWrittenTime)

	// Flux-only images have no user data table header; use the container's
	// alignment.
	shift := img.userDataDDTHeader.BlockAlignmentShift
	if img.noUserDataDDT {
		shift = img.header.BlockAlignmentShift
	}
	img.nextBlockPosition = alignUp(uint64(img.size), shift)

	// The options applied when resuming. The others keep their zero values
	// (no Zstandard, no checksums).
	img.options = Options{
		Compress:       o.Compress,
		Deduplicate:    o.Deduplicate,
		Dictionary:     o.Dictionary,
		TableShift:     int8(img.userDataDDTHeader.TableShift),
		DataShift:      img.userDataDDTHeader.DataShift,
		BlockAlignment: img.userDataDDTHeader.BlockAlignmentShift,
		ZstdLevel:      DefaultOptions().ZstdLevel,
		Threads:        1,
	}
	if o.Deduplicate && !img.noUserDataDDT {
		img.sectorHashMap = make(map[uint64]uint64, min(img.userDataDDTHeader.Blocks/4, 1<<16))
	}

	// Loaded CD prefix/suffix arenas are compact (one slot per stored entry):
	// new entries are appended after the existing ones.
	img.sectorPrefixOffset = uint64(len(img.sectorPrefix))
	img.sectorSuffixOffset = uint64(len(img.sectorSuffix))

	// A resumed image cannot be checksummed.
	img.rewound = true
	img.writable = true
	return img, nil
}
