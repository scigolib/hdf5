package utils

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// NewBaseReader wraps r so that relative address 0 is the byte at file offset
// base.
//
// It is the Go counterpart of the HDF5 VFD adding the file's base address
// before every I/O call (H5FDint.c:267, "addr + file->base_addr"). HDF5
// metadata stores addresses relative to the base address — the position of the
// superblock, i.e. the user block size — so the base is added exactly once,
// here, instead of in every decoder.
//
// EndOfFile reports the relative end of the addressable space (file size minus
// base), so the bounds checks in ReadBlock and ValidateRegion keep comparing
// relative addresses against a relative limit. The superblock's own EOF address
// is absolute and is not adjusted (H5Fsuper.c:573 compares eof + base against
// it).
func NewBaseReader(r io.ReaderAt, base uint64) io.ReaderAt {
	return &baseReader{r: r, base: base}
}

// baseReader is the io.ReaderAt implementation returned by NewBaseReader.
type baseReader struct {
	r    io.ReaderAt
	base uint64
}

// ReadAt implements io.ReaderAt. The caller's offset is relative to the base;
// negative offsets and offsets whose sum with the base overflows int64 are
// rejected before touching the underlying reader.
func (b *baseReader) ReadAt(p []byte, off int64) (int, error) {
	if isNilReader(b.r) {
		return 0, errors.New("base reader: nil underlying reader")
	}

	if off < 0 {
		return 0, fmt.Errorf("base reader: negative offset %d", off)
	}

	const maxInt64 = uint64(math.MaxInt64)
	if b.base > maxInt64 || uint64(off) > maxInt64-b.base {
		return 0, fmt.Errorf("base reader: offset %d with base %d overflows the address space", off, b.base)
	}

	return b.r.ReadAt(p, off+int64(b.base))
}

// EndOfFile reports the relative end of the addressable space: the underlying
// size minus the base. It reports zero when the size is unknown or the base is
// at or past the end, which callers treat as an empty region.
func (b *baseReader) EndOfFile() uint64 {
	size, ok := ReaderSize(b.r)
	if !ok || size <= b.base {
		return 0
	}

	return size - b.base
}
