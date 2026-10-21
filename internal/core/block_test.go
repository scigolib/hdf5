package core

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// opaqueReaderAt wraps data in a type utils.ReaderSize does not recognize, so
// its size is unknown and dataLimit must fall back to the superblock EOF
// address. That is the shape of every caller that hands in an opaque reader.
type opaqueReaderAt struct {
	data []byte
}

func (o *opaqueReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}

	if off >= int64(len(o.data)) {
		return 0, io.EOF
	}

	n := copy(p, o.data[off:])
	if n < len(p) {
		return n, io.EOF
	}

	return n, nil
}

// shortReaderAt returns fewer bytes than requested without reporting an error,
// violating the io.ReaderAt contract on purpose: ReadBlock must not trust n.
type shortReaderAt struct{}

func (shortReaderAt) ReadAt(p []byte, _ int64) (int, error) {
	return len(p) / 2, nil
}

// TestDataLimit_ReaderSizeKnown verifies that a reader which can report its
// size wins over the superblock EOF address, as ADR-003 prescribes.
func TestDataLimit_ReaderSizeKnown(t *testing.T) {
	r := bytes.NewReader(make([]byte, 64))
	sb := &Superblock{EOFAddress: 999}

	require.Equal(t, uint64(64), dataLimit(r, sb))
}

// TestDataLimit_EOFAddress verifies the fallback to the superblock EOF address
// when the reader cannot report its size.
func TestDataLimit_EOFAddress(t *testing.T) {
	r := &opaqueReaderAt{data: make([]byte, 32)}
	sb := &Superblock{EOFAddress: 4096}

	require.Equal(t, uint64(4096), dataLimit(r, sb))
}

// TestDataLimit_Unknown verifies that both-unknown resolves to 0, which tells
// the region check to skip the comparison and keep only the hard cap.
func TestDataLimit_Unknown(t *testing.T) {
	require.Equal(t, uint64(0), dataLimit(&opaqueReaderAt{}, nil))
	require.Equal(t, uint64(0), dataLimit(&opaqueReaderAt{}, &Superblock{}))
}

// TestReadBlock_ZeroSize verifies that a zero-length read is allowed even at an
// undefined address: there is no region to validate.
func TestReadBlock_ZeroSize(t *testing.T) {
	buf, err := ReadBlock(bytes.NewReader(nil), nil, undefinedAddress, 0, maxDatasetSize, "dataset data")

	require.NoError(t, err)
	require.NotNil(t, buf)
	require.Empty(t, buf)
}

// TestReadBlock_UndefinedAddress verifies the rejection of HDF5's undefined
// address sentinel before any limit arithmetic.
func TestReadBlock_UndefinedAddress(t *testing.T) {
	_, err := ReadBlock(bytes.NewReader(make([]byte, 16)), nil, undefinedAddress, 8, maxDatasetSize, "dataset data")

	require.ErrorContains(t, err, "undefined address")
}

// TestReadBlock_ReadsData verifies the normal path: a validated read returns
// exactly the requested bytes.
func TestReadBlock_ReadsData(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	buf, err := ReadBlock(bytes.NewReader(data), nil, 2, 4, maxDatasetSize, "dataset data")

	require.NoError(t, err)
	require.Equal(t, []byte{3, 4, 5, 6}, buf)
}

// TestReadBlock_ShortReadAtEOF verifies that a read running past the end of an
// opaque reader is reported, not zero-filled: the EOF address promised more
// data than the reader can deliver, which means corruption.
func TestReadBlock_ShortReadAtEOF(t *testing.T) {
	r := &opaqueReaderAt{data: make([]byte, 8)}
	sb := &Superblock{EOFAddress: 64}

	_, err := ReadBlock(r, sb, 0, 16, maxDatasetSize, "dataset data")

	require.ErrorContains(t, err, "dataset data")
	require.ErrorContains(t, err, "size 16")
	require.ErrorContains(t, err, "EOF")
}

// TestReadBlock_ShortReadWithoutError verifies that a reader returning fewer
// bytes with a nil error is still treated as a short read.
func TestReadBlock_ShortReadWithoutError(t *testing.T) {
	_, err := ReadBlock(shortReaderAt{}, nil, 0, 8, maxDatasetSize, "dataset data")

	require.ErrorContains(t, err, "short read")
	require.ErrorContains(t, err, "got 4 of 8 bytes")
}

// TestReadBlock_MaxSizeExceeded verifies the caller-supplied hard cap is
// enforced before allocation.
func TestReadBlock_MaxSizeExceeded(t *testing.T) {
	_, err := ReadBlock(bytes.NewReader(make([]byte, 1024)), nil, 0, 512, 256, "chunk data")

	require.ErrorContains(t, err, "chunk data")
	require.ErrorContains(t, err, "size 512 exceeds maximum 256")
}

// TestReadBlock_SizeBeyondPlatform verifies that a size no byte slice can hold
// is rejected with an error instead of reaching make, which would throw. The
// boundary expression is checked on every platform; nothing is allocated.
func TestReadBlock_SizeBeyondPlatform(t *testing.T) {
	_, err := ReadBlock(&opaqueReaderAt{}, nil, 0, uint64(math.MaxInt)+1, math.MaxUint64, "dataset data")

	require.ErrorContains(t, err, "size ")
	require.ErrorContains(t, err, "exceeds the platform limit")
}

// TestReadBlock_AddressBeyondPlatform verifies that an address above MaxInt64
// is rejected before the int64 cast turns it negative. Nothing is allocated.
func TestReadBlock_AddressBeyondPlatform(t *testing.T) {
	_, err := ReadBlock(&opaqueReaderAt{}, nil, uint64(math.MaxInt64)+1, 1, maxDatasetSize, "dataset data")

	require.ErrorContains(t, err, "address ")
	require.ErrorContains(t, err, "exceeds the platform limit")
}

// TestContigDataSize_Valid verifies the accepted case: eight 8-byte elements
// fit exactly in a 64-byte file.
func TestContigDataSize_Valid(t *testing.T) {
	r := bytes.NewReader(make([]byte, 64))

	size, err := contigDataSize(r, nil, 0, 8, 8, "dataset data")

	require.NoError(t, err)
	require.Equal(t, uint64(64), size)
}

// TestContigDataSize_Overflow verifies the overflow check mirrors
// H5D__contig_check: 1<<61 elements at 8 bytes each cannot be represented.
func TestContigDataSize_Overflow(t *testing.T) {
	_, err := contigDataSize(&opaqueReaderAt{}, nil, 0, 1<<61, 8, "dataset data")

	require.ErrorContains(t, err, "dataset size overflow")
}

// TestContigDataSize_ExceedsCap verifies the 1 TB dataset cap is applied before
// the region check, so no buffer sized from metadata can exceed it.
func TestContigDataSize_ExceedsCap(t *testing.T) {
	_, err := contigDataSize(&opaqueReaderAt{}, nil, 0, maxDatasetSize+1, 1, "dataset data")

	require.ErrorContains(t, err, "dataset data")
	require.ErrorContains(t, err, "exceeds maximum")
}

// TestContigDataSize_BeyondFile verifies the region check names both the
// requested size and the available bytes.
func TestContigDataSize_BeyondFile(t *testing.T) {
	r := bytes.NewReader(make([]byte, 64))

	_, err := contigDataSize(r, nil, 0, 9, 8, "dataset data")

	require.ErrorContains(t, err, "read beyond end of file")
	require.ErrorContains(t, err, "size=72")
	require.ErrorContains(t, err, "fileSize=64")
}

// TestContigDataSize_EOFAddress verifies the region check uses the superblock
// EOF address when the reader's size is unknown.
func TestContigDataSize_EOFAddress(t *testing.T) {
	r := &opaqueReaderAt{data: make([]byte, 32)}
	sb := &Superblock{EOFAddress: 32}

	_, err := contigDataSize(r, sb, 16, 3, 8, "dataset data")

	require.ErrorContains(t, err, "read beyond end of file")
	require.ErrorContains(t, err, "offset=16")
	require.ErrorContains(t, err, "fileSize=32")
}

// TestContigDataSize_Empty verifies that an empty dataspace needs no region
// check and reports a zero byte size.
func TestContigDataSize_Empty(t *testing.T) {
	size, err := contigDataSize(&opaqueReaderAt{}, nil, 0, 0, 8, "dataset data")

	require.NoError(t, err)
	require.Zero(t, size)
}

// TestContigDataSize_ErrorLabel verifies that a validation error carries the
// caller's description, so a log line names which block was being read.
func TestContigDataSize_ErrorLabel(t *testing.T) {
	r := bytes.NewReader(make([]byte, 8))

	_, err := contigDataSize(r, nil, 0, 2, 8, "hyperslab block")

	require.ErrorContains(t, err, "hyperslab block")
	require.ErrorContains(t, err, "read beyond end of file")
}
