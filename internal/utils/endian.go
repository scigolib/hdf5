package utils

import "encoding/binary"

// ReadUint64 reads a 64-bit value at specified offset.
func ReadUint64(r ReaderAt, offset int64, order binary.ByteOrder) (uint64, error) {
	buf := GetBuffer(8)
	defer ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, offset); err != nil {
		return 0, err
	}
	return order.Uint64(buf), nil
}

// ReaderAt is a simplified interface for io.ReaderAt.
type ReaderAt interface {
	ReadAt(p []byte, off int64) (n int, err error)
}
