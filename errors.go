package aaru

import "errors"

// Error is an AaruFormat error. Every sentinel error exported by this package
// is an *Error carrying a numeric AaruFormat status code. Use errors.Is to
// test for a
// specific condition (errors returned by this package are frequently wrapped
// with additional context).
type Error struct {
	// Code is the AaruFormat status code (negative for errors, positive for
	// sector status conditions such as ErrSectorNotDumped).
	Code int
	msg  string
}

func (e *Error) Error() string { return e.msg }

func newError(code int, msg string) *Error { return &Error{Code: code, msg: msg} }

// Sentinel errors, one per AaruFormat status code.
var (
	ErrNotAaruFormat          = newError(-1, "not an AaruFormat image")
	ErrFileTooSmall           = newError(-2, "file too small")
	ErrIncompatibleVersion    = newError(-3, "incompatible image version")
	ErrCannotReadIndex        = newError(-4, "cannot read index")
	ErrSectorOutOfBounds      = newError(-5, "sector out of bounds")
	ErrCannotReadHeader       = newError(-6, "cannot read header")
	ErrCannotReadBlock        = newError(-7, "cannot read block")
	ErrUnsupportedCompression = newError(-8, "unsupported compression")
	ErrNotEnoughMemory        = newError(-9, "not enough memory")
	ErrBufferTooSmall         = newError(-10, "buffer too small")
	ErrMediaTagNotPresent     = newError(-11, "media tag not present")
	ErrIncorrectMediaType     = newError(-12, "incorrect media type")
	ErrTrackNotFound          = newError(-13, "track not found")
	ErrReachedUnreachableCode = newError(-14, "internal unreachable code reached")
	ErrInvalidTrackFormat     = newError(-15, "invalid track format")
	ErrSectorTagNotPresent    = newError(-16, "sector tag not present")
	ErrCannotDecompressBlock  = newError(-17, "cannot decompress block")
	ErrInvalidBlockCRC        = newError(-18, "invalid block CRC")
	ErrCannotCreateFile       = newError(-19, "cannot create file")
	ErrInvalidAppNameLength   = newError(-20, "invalid application name length")
	ErrCannotWriteHeader      = newError(-21, "cannot write header")
	ErrReadOnly               = newError(-22, "read-only image")
	ErrCannotWriteBlockHeader = newError(-23, "cannot write block header")
	ErrCannotWriteBlockData   = newError(-24, "cannot write block data")
	ErrCannotSetDDTEntry      = newError(-25, "cannot set DDT entry")
	ErrIncorrectDataSize      = newError(-26, "incorrect data size")
	ErrInvalidTag             = newError(-27, "invalid tag")
	ErrTapeFileNotFound       = newError(-28, "tape file not found")
	ErrTapePartitionNotFound  = newError(-29, "tape partition not found")
	ErrMetadataNotPresent     = newError(-30, "metadata not present")
	ErrInvalidSectorLength    = newError(-31, "invalid sector length")
	ErrFluxDataNotFound       = newError(-32, "flux data not found")
	ErrIncompatibleFeatures   = newError(-33, "image requires unsupported features")
	ErrCannotEncryptSector    = newError(-34, "cannot encrypt sector")
	ErrCannotDecryptSector    = newError(-35, "cannot decrypt sector")
	ErrMissingEncryptionKey   = newError(-36, "missing encryption key")
	ErrUserDataNotPresent     = newError(-37, "user data not present (flux-only image)")

	// ErrSectorNotDumped is returned when reading a sector that was never
	// captured. The accompanying SectorStatus is SectorStatusNotDumped.
	ErrSectorNotDumped = newError(1, "sector not dumped")
	// ErrSectorWithErrors is returned for a sector dumped with errors.
	ErrSectorWithErrors = newError(2, "sector with errors")
	// ErrSectorDeleted is returned for a deleted sector.
	ErrSectorDeleted = newError(3, "sector deleted")

	// ErrClosed is returned when operating on an image that has been closed.
	ErrClosed = errors.New("aaru: image is closed")
)

// ErrorCode returns the AaruFormat status code associated with err: 0 for
// nil, the Code of the first *Error found in err's chain, or -1
// (ErrNotAaruFormat) for foreign errors such as I/O failures.
func ErrorCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}
