package hdf5

import "github.com/scigolib/hdf5/internal/utils"

// SetReadLimit sets the most bytes one read of a chunked dataset may allocate
// for its result. The larger of two allocations counts: the raw element bytes,
// and the converted result (8 bytes per element for Read's []float64, so a
// uint8 dataset needs eight times its stored size). A read that would need more
// fails with an error that wraps ErrLimit and names the requested size and the
// limit; the file itself may be valid, so raising the limit lets the read
// proceed.
//
// The limit exists because a chunked dataset can legitimately be larger than
// its file (chunks that were never written read as the fill value), so a few
// kilobytes of corrupt or hostile metadata could otherwise claim a buffer of
// any size, and a failed allocation ends a Go program instead of returning an
// error. Contiguous and compact datasets are bounded by the storage measured in
// the file and are not affected.
//
// The default is 4 GiB. Passing 0 restores the default; passing math.MaxUint64
// removes the limit, so the library allocates whatever the file's metadata
// claims, as the C library would let a caller do. It is safe to call
// SetReadLimit concurrently with reads; a read uses the limit in effect when it
// starts.
func (f *File) SetReadLimit(bytes uint64) {
	if bytes == 0 {
		bytes = utils.DefaultReadLimit
	}

	f.readLimit.Store(bytes)
}

// ReadLimit returns the most bytes one read of a chunked dataset may allocate
// for its result (see SetReadLimit).
func (f *File) ReadLimit() uint64 {
	if limit := f.readLimit.Load(); limit != 0 {
		return limit
	}

	return utils.DefaultReadLimit
}
