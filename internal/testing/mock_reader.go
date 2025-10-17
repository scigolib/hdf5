// Package testing provides test utilities for HDF5 library testing.
package testing

import "errors"

// MockReaderAt is a mock implementation of io.ReaderAt for testing.
type MockReaderAt struct {
	data []byte
}

// NewMockReaderAt creates a new mock reader with the given data.
func NewMockReaderAt(data []byte) *MockReaderAt {
	return &MockReaderAt{data: data}
}

// ReadAt implements io.ReaderAt interface for the mock reader.
func (m *MockReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}

	if off >= int64(len(m.data)) {
		return 0, errors.New("offset beyond EOF")
	}

	n = copy(p, m.data[off:])
	if n < len(p) {
		err = errors.New("short read")
	}
	return
}
