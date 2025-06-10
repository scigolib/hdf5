package structures

import (
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

type LocalHeap struct {
	Data       []byte
	FreeList   uint64
	HeaderSize uint64
}

func LoadLocalHeap(r io.ReaderAt, address uint64, sb *core.Superblock) (*LocalHeap, error) {
	buf := utils.GetBuffer(16)
	defer utils.ReleaseBuffer(buf)

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
	if _, err := r.ReadAt(heap.Data, int64(address+16)); err != nil {
		return nil, utils.WrapError("local heap data read failed", err)
	}

	return heap, nil
}

func (h *LocalHeap) GetString(offset uint64) (string, error) {
	if offset >= h.HeaderSize {
		return "", errors.New("offset beyond heap data")
	}

	end := offset
	for end < uint64(len(h.Data)) && h.Data[end] != 0 {
		end++
	}

	if end >= uint64(len(h.Data)) {
		return "", errors.New("string not null-terminated")
	}

	return string(h.Data[offset:end]), nil
}
