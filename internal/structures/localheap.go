package structures

import (
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// LocalHeap represents an HDF5 local heap for storing short strings.
type LocalHeap struct {
	Data       []byte
	FreeList   uint64
	HeaderSize uint64
}

// LoadLocalHeap loads a local heap from the specified file address.
func LoadLocalHeap(r io.ReaderAt, address uint64, sb *core.Superblock) (*LocalHeap, error) {
	buf := utils.GetBuffer(16)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(address)); err != nil {
		return nil, utils.WrapError("local heap header read failed", err)
	}

	if string(buf[0:4]) != "HEAP" {
		return nil, errors.New("invalid local heap signature")
	}

	heap := &LocalHeap{
		HeaderSize: sb.Endianness.Uint64(buf[8:16]),
	}

	dataSize := heap.HeaderSize - 16
	heap.Data = make([]byte, dataSize)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(heap.Data, int64(address+16)); err != nil {
		return nil, utils.WrapError("local heap data read failed", err)
	}

	return heap, nil
}

// GetString retrieves a null-terminated string from the heap at the given offset.
func (h *LocalHeap) GetString(offset uint64) (string, error) {
	// The first 16 bytes of heap data contain free list metadata
	// Actual string data starts at offset 16 within the data section
	// So we need to add 16 to the provided offset
	dataOffset := offset + 16

	if dataOffset >= uint64(len(h.Data)) {
		return "", errors.New("offset beyond heap data")
	}

	end := dataOffset
	for end < uint64(len(h.Data)) && h.Data[end] != 0 {
		end++
	}

	if end >= uint64(len(h.Data)) {
		return "", errors.New("string not null-terminated")
	}

	return string(h.Data[dataOffset:end]), nil
}
