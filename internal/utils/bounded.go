package utils

import (
	"errors"
	"fmt"
	"io"
)

// NewBoundedReader wraps r so that every read must lie within [0, size).
//
// It is the Go counterpart of the HDF5 VFD's get_eof contract (ADR-003): the
// format layer can never read past the bytes the file actually has. A
// corrupted address therefore fails as an error, instead of relying on the
// platform ReadAt to report a short read after the caller has already
// allocated a buffer from the same untrusted metadata.
//
// A zero-length read at exactly size is allowed and returns no error; any read
// that ends past size is rejected. A nil or typed-nil r makes every read return
// an error instead of panicking.
func NewBoundedReader(r io.ReaderAt, size uint64) io.ReaderAt {
	return &boundedReader{r: r, size: size}
}

// boundedReader is the io.ReaderAt implementation returned by NewBoundedReader.
type boundedReader struct {
	r    io.ReaderAt
	size uint64
}

// ReadAt implements io.ReaderAt. It rejects a negative offset and any read
// that ends past the file size.
func (b *boundedReader) ReadAt(p []byte, off int64) (int, error) {
	if isNilReader(b.r) {
		return 0, errors.New("bounded reader: nil underlying reader")
	}

	if off < 0 {
		return 0, b.beyondEOF(off, uint64(len(p)))
	}

	// off is checked non-negative above, so it converts to uint64 exactly.
	// Writing the upper bound as a subtraction rejects both off+len wrapping
	// (which can happen in int64 arithmetic) and a read past the file size.
	uoff := uint64(off)
	length := uint64(len(p))
	if uoff > b.size || length > b.size-uoff {
		return 0, b.beyondEOF(off, length)
	}

	// A zero-length read inside the file, including one at exactly size,
	// succeeds without touching the underlying reader: bytes.Reader reports
	// io.EOF at its end even when there is no byte to read, and the caller
	// has asked for nothing.
	if length == 0 {
		return 0, nil
	}

	return b.r.ReadAt(p, off)
}

// EndOfFile reports the size the wrapper enforces. ReaderSize uses it as the
// region limit, so a read through File is bounded by the real file size rather
// than by an EOF address from a (possibly corrupted) superblock: without this
// method ReaderSize falls back to Superblock.EOFAddress, which is file metadata
// too (found by FuzzOpenFile: an 18 GB ReadBlock against an 8 KB file).
func (b *boundedReader) EndOfFile() uint64 {
	return b.size
}

// beyondEOF builds the error for a read outside [0, size). The message names
// the requested offset and length as well as the real file size, so a user can
// tell corruption from a truncated file.
func (b *boundedReader) beyondEOF(off int64, length uint64) error {
	return fmt.Errorf("read beyond end of file: offset=%d size=%d fileSize=%d", off, length, b.size)
}
