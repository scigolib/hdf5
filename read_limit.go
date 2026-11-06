package hdf5

import "github.com/scigolib/hdf5/internal/utils"

// SetReadLimit sets the most bytes one dataset read may allocate for data
// whose size the file's metadata claims but the file itself does not bound. A
// read that would need more fails with an error that wraps ErrLimit and names
// the requested size and the limit; the file itself may be valid, so raising
// the limit lets the read proceed.
//
// The limit exists because a failed allocation ends a Go program instead of
// returning an error, and a few bytes of corrupt or hostile metadata can claim
// a result of any size. It applies to the reads of Dataset (Read, ReadSlice,
// ReadHyperslab, ReadStrings, ReadCompound, ReadVLenBytes and the chunk
// iterator) and bounds:
//
//   - the result of a read of storage that is not in the file: chunked data
//     (chunks that were never written read as the fill value, so a dataset can
//     be larger than its file), contiguous data with no storage allocated yet
//     (it reads as the fill value) and external storage (bytes past the end of
//     an external file read as zeros). The larger of two allocations counts:
//     the raw element bytes, and the converted result (8 bytes per element for
//     Read's []float64, so a uint8 dataset needs eight times its stored size);
//   - the result of a read of contiguous or compact storage that is in the
//     file, only when the converted result is more than eight times the stored
//     bytes (ReadCompound's maps cost tens of times their raw bytes); a smaller
//     result is bounded by the file and the limit does not refuse it;
//   - the bookkeeping of a chunked read (one bit per chunk of the dataset's
//     chunk grid, within what the result leaves of the limit) and the chunk
//     list the chunk iterator keeps;
//   - for variable-length data of any layout, the global heap collections one
//     read caches and the bytes it hands out, since every element may refer
//     to the same large heap object.
//
// The same limit bounds, for the whole File, the metadata kept to resolve
// shared messages: committed datatypes referred to by attributes and datasets
// (their message bytes and an estimate of the decoded type) and the name heaps
// of External File Lists. Each is read and charged once per File; a file whose
// references need more fails with an error that wraps ErrLimit, and raising
// the limit lets a later read resolve them.
//
// The default is 4 GiB (512 MiB on a 32-bit platform). Passing 0 restores the
// default; passing math.MaxUint64 removes the limit, so the library allocates
// whatever the file's metadata claims, as the C library would let a caller do;
// a result larger than the platform can address still fails with ErrLimit. It
// is safe to call SetReadLimit concurrently with reads; a read uses the limit
// in effect when it starts.
func (f *File) SetReadLimit(bytes uint64) {
	if bytes == 0 {
		bytes = utils.DefaultReadLimit
	}

	f.readLimit.Store(bytes)
}

// ReadLimit returns the read limit in effect: the most bytes one dataset read
// may allocate for data the file does not bound, and the budget of the
// File's shared message cache (see SetReadLimit for what it covers).
func (f *File) ReadLimit() uint64 {
	if limit := f.readLimit.Load(); limit != 0 {
		return limit
	}

	return utils.DefaultReadLimit
}
