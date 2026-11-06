package core

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/scigolib/hdf5/internal/utils"
)

// chunkEntryOverhead is the memory one collected ChunkEntry costs besides its
// coordinates: the Scaled slice header (24 bytes), Nbytes and FilterMask
// (8 bytes) and Address (8 bytes), rounded up to 48 for allocator slack. The
// coordinates add 8 bytes per layout dimension. CollectChunks charges this
// cost against the caller's read limit for every entry it keeps.
const chunkEntryOverhead = 48

// allFiltersSkipped is the filter mask of a chunk stored without filters: a
// set bit i skips filter i (H5Z_pipeline, H5Z.c:1395-1400), and a pipeline has
// at most H5Z_MAX_NFILTERS = 32 filters (H5Zpublic.h).
const allFiltersSkipped = math.MaxUint32

// ChunkRange is the chunk coordinates First through Last, inclusive.
type ChunkRange struct{ First, Last uint64 }

// ChunkSelection names the chunks a read visits: for each data dimension,
// ascending, disjoint ranges of chunk coordinates. A nil entry stands for
// every chunk along that dimension; a nil ChunkSelection is the whole grid.
// Ranges keep the selection's size proportional to its blocks, not to the
// chunks they cover.
type ChunkSelection [][]ChunkRange

// ChunkVisitor receives the chunks of a selection. Stored is called once for
// each chunk the index holds; Missing, when set, once for each selected chunk
// that was never written and reads as the fill value. Neither may keep the
// slices it is given. An error from either stops the visit and is returned.
//
// Interrupt, when set, is called every chunkVisitTick steps of the visit (an
// index entry, a lookup or a missing chunk); an error from it stops the visit
// and is returned wrapped. Callers pass a context's Err to make a long walk
// cancellable.
//
// Block, when set, is called once for each block of the index itself with
// the size C allocated for it, as the index's delete frees it: the fixed and
// extensible array headers and blocks, the v2 B-tree header and nodes, the
// v1 B-tree nodes, and for the implicit index its one allocation of every
// chunk of the maximum grid (H5Dnone.c:418), whose chunks Stored also
// reports. Block adds no read: it reports the blocks the visit reads anyway.
// It needs a visit of the whole grid without Missing, the one that walks the
// index (H5D__chunk_iter).
type ChunkVisitor struct {
	Stored    func(e *ChunkEntry) error
	Missing   func(scaled []uint64) error
	Interrupt func() error
	Block     func(addr, size uint64) error
}

// chunkVisitTick is how many steps a visit takes between two calls of
// ChunkVisitor.Interrupt: often enough to stop within microseconds, rare
// enough that a context check costs nothing next to the step itself.
const chunkVisitTick = 1024

// chunkIndexOps is the reading half of C's chunk index operations
// (H5D_chunk_ops_t, H5Dpkg.h): iterate for every index, get_addr for the
// indexes that find a chunk directly (single, implicit, fixed array).
type chunkIndexOps struct {
	// lookup returns a chunk's address, stored size and filter mask; an
	// undefined address is a chunk never written (get_addr). Nil for the
	// walked indexes.
	lookup chunkLookup
	// iterate passes every stored chunk to the sink (iterate). The work is
	// bounded by the index's bytes in the file, never by the chunk grid.
	iterate func(s *chunkSink) error
}

// VisitChunks visits the chunks of sel as C reads a selection: the indexes
// that compute or directly address a chunk (single, implicit, fixed array)
// are asked for each selected chunk, as H5D__chunk_lookup asks get_addr for
// every chunk of the selection's piece map (H5Dchunk.c:1855, :2070, :4004).
// The walked indexes (v1 B-tree, extensible array, v2 B-tree) are iterated
// once and filtered by the selection; that reads the whole index, where C
// searches it per chunk with its metadata cache in front.
//
// A visit of the whole grid (a nil sel) that wants no missing chunks (no
// Missing) lists what the index stores, as H5D__chunk_iter does through the
// index's iterate (H5Dchunk.c:8372-8415), for every index type: asking the
// directly addressed indexes for every cell of the grid would cost one lookup
// per chunk of the grid, 2^40 for a valid unwritten 2^20 × 2^20 dataset of
// 1 × 1 chunks, whatever the file holds. With a selection or a Missing
// callback the cells are the caller's to ask for: each one is part of the
// result it sized.
//
// Memory is proportional to the selected chunks, never to the grid. The
// walked indexes keep one bit per selected chunk, to refuse a chunk listed
// twice (see iterateBTreeV1 for why) and to find the missing chunks
// afterwards; those bits cost at most budget bytes, else utils.ErrLimit. The
// directly addressed indexes list each chunk once by construction.
//
// Shared checks, whatever the index:
//   - the walk of every index is bounded by its structure: depth, a visited
//     set and the number of entries, at most the chunks in the grid; and the
//     bytes of all the blocks it reads, at most the file's size (blockBudget);
//   - a stored size is nonzero and at most utils.MaxChunkSize, the ADR-003 cap
//     ReadBlock applies to every chunk;
//   - entries outside the current grid are skipped for the version 4 indexes,
//     which are addressed over the maximum grid (C never looks them up), and
//     are utils.ErrCorrupt for the version 1 B-tree, whose keys are the chunks
//     written under the current extent. A visit with Block set passes them to
//     Stored as well: it lists what a delete frees, and C's delete frees them.
//
// With the layout flag H5O_LAYOUT_CHUNK_DONT_FILTER_PARTIAL_BOUND_CHUNKS, a
// partial edge chunk is stored without filters: its filter mask is reported as
// allFiltersSkipped (H5Dchunk.c:4721-4728, H5D__chunk_is_partial_edge_chunk
// :7622-7643).
func (d *DatasetDescriptor) VisitChunks(r io.ReaderAt, sb *Superblock, sel ChunkSelection, budget uint64, v ChunkVisitor) error {
	if d.layout.Class != LayoutChunked {
		return fmt.Errorf("dataset is not chunked")
	}
	if v.Block != nil && (sel != nil || v.Missing != nil) {
		return fmt.Errorf("index blocks are reported only by a visit of the whole grid without Missing")
	}

	grid := d.ChunkGrid()
	space, err := newChunkSpace(grid, sel)
	if err != nil {
		return err
	}

	ops, err := d.openChunkIndex(r, sb)
	if err != nil {
		return err
	}

	s := &chunkSink{
		d:          d,
		grid:       grid,
		maxEntries: space.gridTotal,
		space:      space,
		strictGrid: d.layout.ChunkIndexType == ChunkIndexBTREE && d.layout.Version < 4,
		stored:     v.Stored,
		interrupt:  v.Interrupt,
		block:      v.Block,
	}

	switch {
	case ops.lookup != nil && (sel != nil || v.Missing != nil):
		return visitByLookup(space, s, ops.lookup, v)
	case ops.lookup != nil:
		return ops.iterate(s) // Each chunk once by construction: no seen bits.
	default:
		return visitWalked(space, s, ops.iterate, budget, v)
	}
}

// visitByLookup looks up every chunk of the selection (get_addr), passing the
// stored ones to the sink and the others to v's Missing callback.
func visitByLookup(space *chunkSpace, s *chunkSink, lookup chunkLookup, v ChunkVisitor) error {
	return space.forEach(func(_ uint64, scaled []uint64) error {
		if err := s.tick(); err != nil {
			return err
		}
		addr, nbytes, mask, err := lookup(scaled)
		if err != nil {
			return err
		}
		if addr == undefinedAddress {
			return v.missing(scaled)
		}

		return s.emit(scaled, addr, nbytes, mask)
	})
}

// visitWalked walks an index whose entries are found only by walking it,
// with one seen bit per chunk of the selection to refuse duplicates and to
// find the chunks v's Missing callback is called for.
func visitWalked(space *chunkSpace, s *chunkSink, iterate func(*chunkSink) error, budget uint64, v ChunkVisitor) error {
	if space.total == math.MaxUint64 || (space.total+7)/8 > budget {
		return fmt.Errorf("%w: reading %d chunks needs one bit each of bookkeeping, more than the %d bytes the read limit leaves after the result (raise it with SetReadLimit)",
			utils.ErrLimit, space.total, budget)
	}
	s.seen = make([]byte, (space.total+7)/8)

	if err := iterate(s); err != nil {
		return err
	}
	if v.Missing == nil {
		return nil
	}

	return space.forEach(func(pos uint64, scaled []uint64) error {
		if err := s.tick(); err != nil {
			return err
		}
		if s.seen[pos/8]&(1<<(pos%8)) != 0 {
			return nil
		}

		return v.Missing(scaled)
	})
}

func (v ChunkVisitor) missing(scaled []uint64) error {
	if v.Missing == nil {
		return nil
	}

	return v.Missing(scaled)
}

// CollectChunks returns every stored chunk of the dataset. Each kept entry
// costs chunkEntryOverhead + 8 bytes per layout dimension against limit;
// more is utils.ErrLimit. Readers of data use VisitChunks, which keeps no
// entries.
func (d *DatasetDescriptor) CollectChunks(r io.ReaderAt, sb *Superblock, limit uint64) ([]ChunkEntry, error) {
	return d.CollectChunksContext(context.Background(), r, sb, limit)
}

// CollectChunksContext is CollectChunks that stops when ctx is done: the
// walk checks ctx every chunkVisitTick steps and returns its error wrapped.
func (d *DatasetDescriptor) CollectChunksContext(ctx context.Context, r io.ReaderAt, sb *Superblock, limit uint64) ([]ChunkEntry, error) {
	perEntry := uint64(chunkEntryOverhead + 8*len(d.layout.ChunkSize))
	maxKept := limit / perEntry

	var chunks []ChunkEntry
	err := d.VisitChunks(r, sb, nil, limit, ChunkVisitor{
		Stored: func(e *ChunkEntry) error {
			if uint64(len(chunks)) >= maxKept {
				return fmt.Errorf("%w: the chunk index lists more than %d chunks, more than the read limit of %d bytes holds",
					utils.ErrLimit, maxKept, limit)
			}
			chunks = append(chunks, *e)

			return nil
		},
		Interrupt: ctx.Err,
	})
	if err != nil {
		return nil, err
	}

	return chunks, nil
}

// openChunkIndex reads and checks the index's own header and returns its
// operations (H5D__chunk_set_ops picks them by index type, H5Dchunk.c:950-1050).
func (d *DatasetDescriptor) openChunkIndex(r io.ReaderAt, sb *Superblock) (chunkIndexOps, error) {
	switch d.layout.ChunkIndexType {
	case ChunkIndexBTREE:
		return chunkIndexOps{iterate: func(s *chunkSink) error { return d.iterateBTreeV1(r, sb, s) }}, nil
	case ChunkIndexSingle:
		return d.openSingle()
	case ChunkIndexNone:
		return d.openImplicit(r, sb)
	case ChunkIndexFArray:
		return d.openFArray(r, sb)
	case ChunkIndexEArray:
		return chunkIndexOps{iterate: func(s *chunkSink) error { return d.iterateEArray(r, sb, s) }}, nil
	case ChunkIndexBT2:
		return chunkIndexOps{iterate: func(s *chunkSink) error { return d.iterateBT2(r, sb, s) }}, nil
	default:
		return chunkIndexOps{}, fmt.Errorf("%w: unknown chunk index type %d", utils.ErrCorrupt, d.layout.ChunkIndexType)
	}
}

// chunkAxis is the selection along one dimension: ascending, disjoint ranges
// of chunk coordinates and the number of selected chunks before each range.
type chunkAxis struct {
	ranges []ChunkRange
	before []uint64
	size   uint64
}

// chunkSpace is a ChunkSelection normalized against the current grid, with
// a linear position for each selected chunk.
type chunkSpace struct {
	axes      []chunkAxis
	total     uint64 // Selected chunks; math.MaxUint64 on overflow.
	gridTotal uint64 // Chunks in the grid; math.MaxUint64 on overflow.
}

func newChunkSpace(grid []uint64, sel ChunkSelection) (*chunkSpace, error) {
	if sel != nil && len(sel) != len(grid) {
		return nil, fmt.Errorf("chunk selection has %d dimensions, the grid %d", len(sel), len(grid))
	}

	s := &chunkSpace{axes: make([]chunkAxis, len(grid))}
	sizes := make([]uint64, len(grid))
	for i, g := range grid {
		ranges := []ChunkRange{{First: 0, Last: g - 1}}
		switch {
		case g == 0:
			ranges = nil
		case sel != nil && sel[i] != nil:
			ranges = sel[i]
		}

		ax := chunkAxis{ranges: ranges, before: make([]uint64, len(ranges))}
		for j, rg := range ranges {
			if rg.First > rg.Last || rg.Last >= g || (j > 0 && rg.First <= ranges[j-1].Last) {
				return nil, fmt.Errorf("chunk selection along dimension %d is not ascending inside the grid of %d", i, g)
			}
			ax.before[j] = ax.size
			ax.size += rg.Last - rg.First + 1 // At most g in all.
		}
		s.axes[i] = ax
		sizes[i] = ax.size
	}

	s.total = productOrMax(sizes)
	s.gridTotal = productOrMax(grid)

	return s, nil
}

// productOrMax multiplies vals, saturating at math.MaxUint64.
func productOrMax(vals []uint64) uint64 {
	p := uint64(1)
	for _, v := range vals {
		var err error
		if p, err = utils.SafeMultiply(p, v); err != nil {
			return math.MaxUint64
		}
	}

	return p
}

// pos returns the linear position of scaled among the selected chunks, or
// false when it is not selected. scaled lies inside the grid.
func (s *chunkSpace) pos(scaled []uint64) (uint64, bool) {
	p := uint64(0)
	for i, c := range scaled {
		ax := &s.axes[i]
		// The last range starting at or before c.
		k := sort.Search(len(ax.ranges), func(j int) bool { return ax.ranges[j].First > c }) - 1
		if k < 0 || c > ax.ranges[k].Last {
			return 0, false
		}
		p = p*ax.size + ax.before[k] + c - ax.ranges[k].First // Below total, which did not overflow when bits are kept.
	}

	return p, true
}

// forEach calls fn with the position and coordinates of every selected chunk
// in row-major order. fn must not keep the slice.
func (s *chunkSpace) forEach(fn func(pos uint64, scaled []uint64) error) error {
	rank := len(s.axes)
	k := make([]int, rank) // Current range along each dimension.
	scaled := make([]uint64, rank)
	for i := range s.axes {
		if s.axes[i].size == 0 {
			return nil
		}
		scaled[i] = s.axes[i].ranges[0].First
	}

	for pos := uint64(0); ; pos++ {
		if err := fn(pos, scaled); err != nil {
			return err
		}

		i := rank - 1
		for ; i >= 0; i-- {
			ax := &s.axes[i]
			if scaled[i] < ax.ranges[k[i]].Last {
				scaled[i]++
				break
			}
			if k[i]+1 < len(ax.ranges) {
				k[i]++
				scaled[i] = ax.ranges[k[i]].First
				break
			}
			k[i] = 0
			scaled[i] = ax.ranges[0].First
		}
		if i < 0 {
			return nil
		}
	}
}

// chunkSink receives the entries a walked index yields and applies the
// shared checks before passing each selected one on.
type chunkSink struct {
	d          *DatasetDescriptor
	grid       []uint64
	maxEntries uint64
	count      uint64
	space      *chunkSpace
	seen       []byte // One bit per selected chunk; nil when entries are unique by construction.
	strictGrid bool   // Out-of-grid entries are corruption (version 1 B-tree).
	stored     func(e *ChunkEntry) error
	interrupt  func() error                  // ChunkVisitor.Interrupt; nil: never stops.
	block      func(addr, size uint64) error // ChunkVisitor.Block; nil: not reported.
	steps      uint64
}

// indexBlock reports one block of the index, of the size C allocated for it,
// to ChunkVisitor.Block. A nil sink or hook reports nothing.
func (s *chunkSink) indexBlock(addr, size uint64) error {
	if s == nil || s.block == nil {
		return nil
	}

	return s.block(addr, size)
}

// tick counts one step of the visit and calls the interrupt every
// chunkVisitTick steps.
func (s *chunkSink) tick() error {
	s.steps++
	if s.interrupt == nil || s.steps%chunkVisitTick != 0 {
		return nil
	}
	if err := s.interrupt(); err != nil {
		return fmt.Errorf("chunk index walk stopped after %d steps: %w", s.steps, err)
	}

	return nil
}

// add takes one entry of an iterated index at chunk coordinates scaled.
func (s *chunkSink) add(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	if err := s.tick(); err != nil {
		return err
	}
	for i, g := range s.grid {
		if scaled[i] < g {
			continue
		}
		if s.strictGrid {
			return fmt.Errorf("%w: chunk at 0x%X has coordinate %d in dimension %d, the grid has %d chunks",
				utils.ErrCorrupt, addr, scaled[i], i, g)
		}
		if s.block != nil {
			// A visit for the index's blocks lists the file space a delete
			// frees, and the delete frees every chunk the index holds,
			// inside the current extent or not: the array indexes iterate
			// every element up to their maximum (H5D__farray_idx_delete,
			// H5Dfarray.c:1364, through H5D__farray_idx_delete_cb :1408;
			// H5D__earray_idx_delete, H5Dearray.c:1581-1598, through
			// H5D__earray_idx_delete_cb :1544) and the v2 B-tree removes
			// every record (H5B2_delete, H5Dbtree2.c:1350). Such a chunk is
			// in no selection and has no seen bit; its mask is passed as
			// stored, the partial-edge rule being about the current extent.
			return s.pass(scaled, addr, nbytes, mask)
		}

		return nil
	}

	s.count++
	if s.count > s.maxEntries {
		return fmt.Errorf("%w: chunk index has more than %d entries, the most its chunk grid allows",
			utils.ErrCorrupt, s.maxEntries)
	}

	pos, ok := s.space.pos(scaled)
	if !ok {
		return nil
	}
	if s.seen != nil {
		if s.seen[pos/8]&(1<<(pos%8)) != 0 {
			return fmt.Errorf("%w: chunk index lists the chunk at %v more than once", utils.ErrCorrupt, scaled)
		}
		s.seen[pos/8] |= 1 << (pos % 8)
	}

	return s.emit(scaled, addr, nbytes, mask)
}

// emit checks a selected stored chunk and passes it to the visitor.
func (s *chunkSink) emit(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	if err := checkStoredSize(scaled, addr, nbytes); err != nil {
		return err
	}
	if s.stored == nil {
		return nil
	}

	return s.deliver(scaled, addr, nbytes, s.d.storedMask(scaled, mask))
}

// pass checks a stored chunk outside the current grid and passes it to the
// visitor with its mask as stored.
func (s *chunkSink) pass(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	if err := checkStoredSize(scaled, addr, nbytes); err != nil {
		return err
	}
	if s.stored == nil {
		return nil
	}

	return s.deliver(scaled, addr, nbytes, mask)
}

// deliver passes a checked stored chunk to the visitor.
func (s *chunkSink) deliver(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	key := make([]uint64, len(scaled)+1) // The element-size coordinate stays 0.
	copy(key, scaled)
	e := ChunkEntry{
		Key:     ChunkKey{Scaled: key, Nbytes: uint32(nbytes), FilterMask: mask}, //nolint:gosec // G115: checkStoredSize caps nbytes at utils.MaxChunkSize.
		Address: addr,
	}

	return s.stored(&e)
}

// checkStoredSize applies the shared checks of a stored chunk's size.
func checkStoredSize(scaled []uint64, addr, nbytes uint64) error {
	if nbytes == 0 {
		return fmt.Errorf("%w: chunk %v at 0x%X has a stored size of zero", utils.ErrCorrupt, scaled, addr)
	}
	if nbytes > utils.MaxChunkSize {
		return fmt.Errorf("%w: chunk %v at 0x%X stores %d bytes, above the chunk maximum %d",
			utils.ErrCorrupt, scaled, addr, nbytes, uint64(utils.MaxChunkSize))
	}

	return nil
}

// storedMask returns the filter mask a chunk is read with: allFiltersSkipped
// for a partial edge chunk of a dataset that does not filter them, else mask.
// A chunk is a partial edge chunk when it extends past the current dataspace
// in some dimension (H5D__chunk_is_partial_edge_chunk, H5Dchunk.c:7622-7643).
func (d *DatasetDescriptor) storedMask(scaled []uint64, mask uint32) uint32 {
	if d.layout.ChunkFlags&chunkLayoutFlagDontFilterPartial == 0 || !d.filtered() {
		return mask
	}

	for i, c := range scaled {
		cs := d.layout.ChunkSize[i]
		// c < grid[i], so c × cs <= dim - 1: no overflow.
		if d.dataspace.Dimensions[i]-c*cs < cs {
			return allFiltersSkipped
		}
	}

	return mask
}

// addArrayRecords passes the n records in raw, the first of which has array
// index first, to the sink, skipping the undefined addresses of chunks never
// written.
func (s *chunkSink) addArrayRecords(raw []byte, first, n uint64, codec chunkElemCodec, lin *chunkLinearizer) error {
	recSize := codec.rawSize()
	scaled := make([]uint64, len(s.grid))
	for i := uint64(0); i < n; i++ {
		addr, nbytes, mask := codec.decode(raw[i*recSize:])
		if addr == undefinedAddress {
			continue
		}

		lin.coords(first+i, scaled)
		if err := s.add(scaled, addr, nbytes, mask); err != nil {
			return err
		}
	}

	return nil
}

// FillChunk returns one chunk of the dataset's fill value, as the readers
// place it for a chunk never written, and whether it is all zeros: a reader
// whose result starts zeroed can then skip missing chunks.
func (d *DatasetDescriptor) FillChunk() (chunk []byte, zero bool, err error) {
	chunk = make([]byte, d.chunkBytes)
	if err := d.FillUnallocated(chunk); err != nil {
		return nil, false, err
	}

	for _, b := range chunk {
		if b != 0 {
			return chunk, false, nil
		}
	}

	return chunk, true, nil
}
