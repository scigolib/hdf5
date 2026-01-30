package writer

import (
	"compress/bzip2"
	"errors"
	"fmt"
	"io"
)

// BZIP2Filter implements BZIP2 compression (FilterID = 307).
// BZIP2 is a high-quality compression algorithm designed by Julian Seward.
// It provides better compression than GZIP (typically 10-15% smaller) but is slower.
//
// BZIP2 is commonly used for scientific datasets where storage space is critical.
// Filter ID 307 is registered with the HDF Group.
//
// Reference: https://sourceware.org/bzip2/
// HDF5 Registration: https://github.com/HDFGroup/hdf5_plugins
type BZIP2Filter struct {
	blockSize int // Block size in 100KB units (1-9), default 9 = 900KB
}

// NewBZIP2Filter creates a BZIP2 compression filter.
// blockSize specifies compression level (1-9):
//   - 1 = fastest, lowest compression (100KB blocks)
//   - 9 = slowest, highest compression (900KB blocks) - default
func NewBZIP2Filter(blockSize int) *BZIP2Filter {
	if blockSize < 1 || blockSize > 9 {
		blockSize = 9 // Default to maximum compression
	}
	return &BZIP2Filter{blockSize: blockSize}
}

// ID returns the HDF5 filter identifier for BZIP2.
func (f *BZIP2Filter) ID() FilterID {
	return FilterBZIP2
}

// Name returns the HDF5 filter name.
func (f *BZIP2Filter) Name() string {
	return "bzip2"
}

// Apply compresses data using BZIP2 algorithm.
// Returns compressed data suitable for storage.
//
// NOTE: Go stdlib compress/bzip2 only provides decompression.
// For write support, consider using github.com/dsnet/compress/bzip2
// or waiting for future implementation.
func (f *BZIP2Filter) Apply(_ []byte) ([]byte, error) {
	// TODO: Implement BZIP2 compression when zero-dependency solution available.
	// Options:
	//   1. Use github.com/dsnet/compress/bzip2 (pure Go, adds dependency)
	//   2. Implement pure Go BZIP2 compressor (large effort ~2000+ lines)
	//   3. Wait for stdlib to add bzip2.Writer
	return nil, errors.New("bzip2 compression not implemented yet (stdlib only supports decompression)")
}

// Remove decompresses BZIP2-compressed data.
// Returns the original uncompressed data.
//
// This uses Go's stdlib compress/bzip2 for decompression.
func (f *BZIP2Filter) Remove(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	reader := bzip2.NewReader(io.NopCloser(io.NewSectionReader(
		&bytesReaderAt{data}, 0, int64(len(data)))))

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("bzip2 decompression failed: %w", err)
	}

	return decompressed, nil
}

// Encode returns the filter parameters for the Pipeline message.
//
// For BZIP2 in HDF5, the client data typically contains:
//   - cd_values[0]: Block size (1-9, in 100KB units)
//
// Reference: https://github.com/HDFGroup/hdf5_plugins/blob/master/BZIP2/src/H5Zbzip2.c
func (f *BZIP2Filter) Encode() (flags uint16, cdValues []uint32) {
	return 0, []uint32{uint32(f.blockSize)} //nolint:gosec // G115: blockSize is 1-9, always fits in uint32
}

// bytesReaderAt wraps []byte to implement io.ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off > int64(len(b.data)) {
		return 0, io.EOF
	}
	n = copy(p, b.data[off:])
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}
