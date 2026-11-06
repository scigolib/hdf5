package core

import (
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// chunkLookup is get_addr: a chunk's address, stored size and filter mask, or
// an undefined address for a chunk never written.
type chunkLookup = func(scaled []uint64) (addr, nbytes uint64, mask uint32, err error)

// noChunks is the lookup of an index with nothing allocated.
func noChunks([]uint64) (addr, nbytes uint64, mask uint32, err error) {
	return undefinedAddress, 0, 0, nil
}

// openSingle opens the single chunk index (H5Dsingle.c): the layout's index
// address is the address of the dataset's only chunk, undefined when the
// chunk was never written. A filtered dataset stores the chunk's size and
// filter mask in the layout (flag H5O_LAYOUT_CHUNK_SINGLE_INDEX_WITH_FILTER);
// otherwise the chunk has the full chunk size and no mask
// (H5D__single_idx_get_addr, H5Dsingle.c:309-333).
//
// Deviations, stricter than C, which only asserts them
// (H5Dsingle.c:276, :318-319):
//   - the maximum dataspace holds exactly one chunk (max_nchunks == 1);
//   - a filtered dataset has the flag set. C sets the flag itself when it
//     opens a filtered dataset (H5D__single_idx_init, H5Dsingle.c:126-131) and
//     then reads a stored size of 0, so a missing flag cannot be a valid file.
//
// A flag without filters is ignored, as C clears it (H5Dsingle.c:133-134).
func (d *DatasetDescriptor) openSingle() (chunkLookup, error) {
	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return nil, err
	}
	if unlim != -1 {
		return nil, fmt.Errorf("%w: single chunk index on a dataspace with an unlimited dimension", utils.ErrCorrupt)
	}
	for i, m := range maxChunks {
		if m != 1 {
			return nil, fmt.Errorf("%w: single chunk index, but the maximum dataspace has %d chunks in dimension %d",
				utils.ErrCorrupt, m, i)
		}
	}

	addr := d.layout.DataAddress
	if addr == undefinedAddress {
		return noChunks, nil
	}

	nbytes, mask := d.chunkBytes, uint32(0)
	if d.filtered() {
		if d.layout.ChunkFlags&chunkLayoutFlagSingleWithFilter == 0 {
			return nil, fmt.Errorf("%w: filtered single chunk index has no stored chunk size", utils.ErrCorrupt)
		}
		nbytes, mask = d.layout.Index.SingleNbytes, d.layout.Index.SingleFilterMask
	}

	// The grid is inside the one-chunk maximum grid: the only chunk is (0, ...).
	return func([]uint64) (uint64, uint64, uint32, error) { return addr, nbytes, mask, nil }, nil
}

// openImplicit opens the implicit index (H5Dnone.c): every chunk of the
// maximum dataspace was allocated at once at the index address, so chunk i in
// row-major order over the maximum grid is at addr + i × chunk size and has
// the full chunk size (H5D__none_idx_get_addr, H5Dnone.c:243-266). An
// undefined address means nothing was allocated.
//
// The storage C allocated, max_nchunks × chunk size (H5Dnone.c:136), must lie
// in the file. The index is used only without filters and with fixed maximum
// dimensions (H5Dchunk.c:1009-1031); C asserts the first (H5Dnone.c:252),
// here both are utils.ErrCorrupt.
func (d *DatasetDescriptor) openImplicit(r io.ReaderAt, sb *Superblock) (chunkLookup, error) {
	if d.filtered() {
		return nil, fmt.Errorf("%w: implicit chunk index with a filter pipeline", utils.ErrCorrupt)
	}

	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return nil, err
	}
	if unlim != -1 {
		return nil, fmt.Errorf("%w: implicit chunk index on a dataspace with an unlimited dimension", utils.ErrCorrupt)
	}

	addr := d.layout.DataAddress
	if addr == undefinedAddress {
		return noChunks, nil
	}

	lin, err := newChunkLinearizer(maxChunks, 0)
	if err != nil {
		return nil, err
	}

	storage, err := utils.SafeMultiply(lin.total, d.chunkBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: implicit chunk storage of %d chunks overflows", utils.ErrCorrupt, lin.total)
	}
	if err := checkRegion(r, sb, addr, storage, "implicit chunk storage"); err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	size := d.chunkBytes

	// index(scaled) < lin.total and the storage did not overflow.
	return func(scaled []uint64) (uint64, uint64, uint32, error) {
		return addr + lin.index(scaled)*size, size, 0, nil
	}, nil
}
