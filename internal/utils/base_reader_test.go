package utils

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBaseReaderShiftsOffsets pins the contract the HDF5 format relies on:
// metadata addresses are relative to the file's base address, and the reader
// adds the base exactly once, at the I/O boundary (H5FDint.c:267).
func TestBaseReaderShiftsOffsets(t *testing.T) {
	raw := []byte("0123456789ABCDEF")
	r := NewBaseReader(bytes.NewReader(raw), 8)

	buf := make([]byte, 4)

	_, err := r.ReadAt(buf, 0)
	require.NoError(t, err, "relative address 0 must read the byte at the base")
	require.Equal(t, "89AB", string(buf))

	_, err = r.ReadAt(buf, 4)
	require.NoError(t, err)
	require.Equal(t, "CDEF", string(buf))

	_, err = r.ReadAt(buf, 8)
	require.Error(t, err, "relative address past the relative end must fail")
}

func TestBaseReaderEndOfFileIsRelative(t *testing.T) {
	bounded := NewBoundedReader(bytes.NewReader(make([]byte, 32)), 32)
	r := NewBaseReader(bounded, 8)

	size, ok := ReaderSize(r)
	require.True(t, ok, "base reader must report its size to ReaderSize")
	require.Equal(t, uint64(24), size, "size must be the relative end, not the file size")
}

func TestBaseReaderZeroBaseIsTransparent(t *testing.T) {
	raw := []byte("01234567")
	r := NewBaseReader(bytes.NewReader(raw), 0)

	buf := make([]byte, len(raw))
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, len(raw), n)
	require.Equal(t, raw, buf)
}

func TestBaseReaderRejectsNegativeAndOverflowingOffsets(t *testing.T) {
	r := NewBaseReader(bytes.NewReader(make([]byte, 16)), 8)

	buf := make([]byte, 2)

	_, err := r.ReadAt(buf, -1)
	require.Error(t, err)

	_, err = r.ReadAt(buf, math.MaxInt64-3)
	require.Error(t, err, "an offset whose sum with the base overflows int64 must be rejected")
}

func TestBaseReaderRejectsNilUnderlyingReader(t *testing.T) {
	r := NewBaseReader(nil, 8)

	_, err := r.ReadAt(make([]byte, 1), 0)
	require.Error(t, err)
}

func TestBaseReaderEmptyRelativeSpace(t *testing.T) {
	// A base equal to the file size leaves nothing addressable: the relative
	// size is zero and any read must fail rather than wrap.
	bounded := NewBoundedReader(bytes.NewReader(make([]byte, 8)), 8)
	r := NewBaseReader(bounded, 8)

	size, ok := ReaderSize(r)
	require.True(t, ok)
	require.Equal(t, uint64(0), size)

	_, err := r.ReadAt(make([]byte, 1), 0)
	require.Error(t, err)
}
