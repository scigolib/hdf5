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

// unallocatedIndex is the operations of a directly addressed index with
// nothing allocated: every lookup is a chunk never written, and iterate
// lists nothing: C does not iterate an index whose address is undefined
// (H5D__get_num_chunks, H5Dchunk.c:8041-8047).
func unallocatedIndex() chunkIndexOps {
	return chunkIndexOps{lookup: noChunks, iterate: func(*chunkSink) error { return nil }}
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
func (d *DatasetDescriptor) openSingle() (chunkIndexOps, error) {
	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return chunkIndexOps{}, err
	}
	if unlim != -1 {
		return chunkIndexOps{}, fmt.Errorf("%w: single chunk index on a dataspace with an unlimited dimension", utils.ErrCorrupt)
	}
	for i, m := range maxChunks {
		if m != 1 {
			return chunkIndexOps{}, fmt.Errorf("%w: single chunk index, but the maximum dataspace has %d chunks in dimension %d",
				utils.ErrCorrupt, m, i)
		}
	}

	addr := d.layout.DataAddress
	if addr == undefinedAddress {
		return unallocatedIndex(), nil
	}

	nbytes, mask := d.chunkBytes, uint32(0)
	if d.filtered() {
		if d.layout.ChunkFlags&chunkLayoutFlagSingleWithFilter == 0 {
			return chunkIndexOps{}, fmt.Errorf("%w: filtered single chunk index has no stored chunk size", utils.ErrCorrupt)
		}
		nbytes, mask = d.layout.Index.SingleNbytes, d.layout.Index.SingleFilterMask
	}

	// The grid is inside the one-chunk maximum grid: the only chunk is (0, ...).
	return chunkIndexOps{
		lookup: func([]uint64) (uint64, uint64, uint32, error) { return addr, nbytes, mask, nil },
		// H5D__single_idx_iterate (H5Dsingle.c:367) calls back once, for chunk 0.
		iterate: func(s *chunkSink) error { return s.add(make([]uint64, len(maxChunks)), addr, nbytes, mask) },
	}, nil
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
func (d *DatasetDescriptor) openImplicit(r io.ReaderAt, sb *Superblock) (chunkIndexOps, error) {
	if d.filtered() {
		return chunkIndexOps{}, fmt.Errorf("%w: implicit chunk index with a filter pipeline", utils.ErrCorrupt)
	}

	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return chunkIndexOps{}, err
	}
	if unlim != -1 {
		return chunkIndexOps{}, fmt.Errorf("%w: implicit chunk index on a dataspace with an unlimited dimension", utils.ErrCorrupt)
	}

	addr := d.layout.DataAddress
	if addr == undefinedAddress {
		return unallocatedIndex(), nil
	}

	lin, err := newChunkLinearizer(maxChunks, 0)
	if err != nil {
		return chunkIndexOps{}, err
	}

	storage, err := utils.SafeMultiply(lin.total, d.chunkBytes)
	if err != nil {
		return chunkIndexOps{}, fmt.Errorf("%w: implicit chunk storage of %d chunks overflows", utils.ErrCorrupt, lin.total)
	}
	if err := checkRegion(r, sb, addr, storage, "implicit chunk storage"); err != nil {
		return chunkIndexOps{}, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	size := d.chunkBytes

	// index(scaled) < lin.total and the storage did not overflow.
	lookup := func(scaled []uint64) (uint64, uint64, uint32, error) {
		return addr + lin.index(scaled)*size, size, 0, nil
	}

	// Every chunk of the maximum grid is allocated, and the storage of all of
	// them lies in the file, so the max_nchunks calls are bounded by the
	// file's size. H5D__none_idx_iterate (H5Dnone.c:302-360) walks the same
	// chunks; those outside the current grid are skipped by the sink.
	iterate := func(s *chunkSink) error {
		// The chunks are one allocation, freed as one (H5D__none_idx_delete,
		// H5Dnone.c:405-418).
		if err := s.indexBlock(addr, storage); err != nil {
			return err
		}
		scaled := make([]uint64, len(maxChunks))
		for i := uint64(0); i < lin.total; i++ {
			lin.coords(i, scaled)
			if err := s.add(scaled, addr+i*size, size, 0); err != nil {
				return err
			}
		}

		return nil
	}

	return chunkIndexOps{lookup: lookup, iterate: iterate}, nil
}
