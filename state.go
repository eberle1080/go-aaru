package aaru

import "github.com/eberle1080/go-aaru/internal/reedsolomon"

// consoleState holds lazily initialized console disc crypto state (PS3,
// Wii U, Nintendo GameCube/Wii). Owned by the console crypto integration;
// the fields are documented on consoleCrypto (console.go).
type consoleState struct{ consoleCrypto }

// ecState holds the erasure coding state of an image, for both the write
// path (enabled by SetErasureCoding) and the recovery path (loaded from the
// ECMB on open).
type ecState struct {
	// ---- write path ----
	enabled   bool
	algorithm ErasureCodingAlgorithm
	k, m      uint16
	codec     *reedsolomon.Codec
	// dataShardSize is the size of the parity accumulators, set from the
	// first block's on-disk size plus 25% and grown the same way when a
	// bigger block arrives. It is recorded in the ECMB data group.
	dataShardSize uint32
	parity        [][]byte // M parity accumulators of the open stripe (nil until the first block)
	offsets       []uint64 // file offsets of the blocks of the open stripe
	sizes         []uint32 // on-disk sizes of the blocks of the open stripe
	crcs          []uint64 // CRC64 of the on-disk bytes of the blocks of the open stripe
	count         uint16   // blocks in the open stripe
	stripeCount   uint32   // completed data stripes
	stripes       []byte   // serialized descriptors of the completed data stripes
	writeErr      error    // first failure writing parity, reported by ecFinalize

	// ---- recovery path ----
	recoveryAvailable  bool
	recoveryInProgress bool // recursion guard
	data, ddt, meta    *ecGroup
}
