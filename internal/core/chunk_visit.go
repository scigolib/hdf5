package core

import (
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
type ChunkVisitor struct {
	Stored  func(e *ChunkEntry) error
	Missing func(scaled []uint64) error
}

// chunkIndexOps is the reading half of C's chunk index operations
// (H5D_chunk_ops_t, H5Dpkg.h): get_addr for the indexes that find a chunk
// directly, iterate for those that are walked. Exactly one is set.
type chunkIndexOps struct {
	// lookup returns a chunk's address, stored size and filter mask; an
	// undefined address is a chunk never written (get_addr).
	lookup func(scaled []uint64) (addr, nbytes uint64, mask uint32, err error)
	// iterate passes every stored chunk to the sink (iterate).
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
// Memory is proportional to the selected chunks, never to the grid. The
// walked indexes keep one bit per selected chunk, to refuse a chunk listed
// twice (see iterateBTreeV1 for why) and to find the missing chunks
// afterwards; those bits cost at most budget bytes, else utils.ErrLimit.
//
// Shared checks, whatever the index:
//   - the walk of every index is bounded by its structure: depth, a visited
//     set and the number of entries, at most the chunks in the grid;
//   - a stored size is nonzero and at most utils.MaxChunkSize, the ADR-003 cap
//     ReadBlock applies to every chunk;
//   - entries outside the current grid are skipped for the version 4 indexes,
//     which are addressed over the maximum grid (C never looks them up), and
//     are utils.ErrCorrupt for the version 1 B-tree, whose keys are the chunks
//     written under the current extent.
//
// With the layout flag H5O_LAYOUT_CHUNK_DONT_FILTER_PARTIAL_BOUND_CHUNKS, a
// partial edge chunk is stored without filters: its filter mask is reported as
// allFiltersSkipped (H5Dchunk.c:4721-4728, H5D__chunk_is_partial_edge_chunk
// :7622-7643).
func (d *DatasetDescriptor) VisitChunks(r io.ReaderAt, sb *Superblock, sel ChunkSelection, budget uint64, v ChunkVisitor) error {
	if d.layout.Class != LayoutChunked {
		return fmt.Errorf("dataset is not chunked")
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
	}

	if ops.lookup != nil {
		return space.forEach(func(_ uint64, scaled []uint64) error {
			addr, nbytes, mask, err := ops.lookup(scaled)
			if err != nil {
				return err
			}
			if addr == undefinedAddress {
				return v.missing(scaled)
			}

			return s.emit(scaled, addr, nbytes, mask)
		})
	}

	if space.total == math.MaxUint64 || (space.total+7)/8 > budget {
		return fmt.Errorf("%w: reading %d chunks needs one bit each of bookkeeping, more than the %d bytes the read limit leaves after the result (raise it with SetReadLimit)",
			utils.ErrLimit, space.total, budget)
	}
	s.seen = make([]byte, (space.total+7)/8)

	if err := ops.iterate(s); err != nil {
		return err
	}
	if v.Missing == nil {
		return nil
	}

	return space.forEach(func(pos uint64, scaled []uint64) error {
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
	perEntry := uint64(chunkEntryOverhead + 8*len(d.layout.ChunkSize))
	maxKept := limit / perEntry

	var chunks []ChunkEntry
	err := d.VisitChunks(r, sb, nil, limit, ChunkVisitor{Stored: func(e *ChunkEntry) error {
		if uint64(len(chunks)) >= maxKept {
			return fmt.Errorf("%w: the chunk index lists more than %d chunks, more than the read limit of %d bytes holds",
				utils.ErrLimit, maxKept, limit)
		}
		chunks = append(chunks, *e)

		return nil
	}})
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
		lookup, err := d.openSingle()
		return chunkIndexOps{lookup: lookup}, err
	case ChunkIndexNone:
		lookup, err := d.openImplicit(r, sb)
		return chunkIndexOps{lookup: lookup}, err
	case ChunkIndexFArray:
		lookup, err := d.openFArray(r, sb)
		return chunkIndexOps{lookup: lookup}, err
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
	seen       []byte // One bit per selected chunk; nil on the lookup path.
	strictGrid bool   // Out-of-grid entries are corruption (version 1 B-tree).
	stored     func(e *ChunkEntry) error
}

// add takes one entry of a walked index at chunk coordinates scaled.
func (s *chunkSink) add(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	for i, g := range s.grid {
		if scaled[i] < g {
			continue
		}
		if s.strictGrid {
			return fmt.Errorf("%w: chunk at 0x%X has coordinate %d in dimension %d, the grid has %d chunks",
				utils.ErrCorrupt, addr, scaled[i], i, g)
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
	if s.seen[pos/8]&(1<<(pos%8)) != 0 {
		return fmt.Errorf("%w: chunk index lists the chunk at %v more than once", utils.ErrCorrupt, scaled)
	}
	s.seen[pos/8] |= 1 << (pos % 8)

	return s.emit(scaled, addr, nbytes, mask)
}

// emit checks a selected stored chunk and passes it to the visitor.
func (s *chunkSink) emit(scaled []uint64, addr, nbytes uint64, mask uint32) error {
	if nbytes == 0 {
		return fmt.Errorf("%w: chunk %v at 0x%X has a stored size of zero", utils.ErrCorrupt, scaled, addr)
	}
	if nbytes > utils.MaxChunkSize {
		return fmt.Errorf("%w: chunk %v at 0x%X stores %d bytes, above the chunk maximum %d",
			utils.ErrCorrupt, scaled, addr, nbytes, uint64(utils.MaxChunkSize))
	}
	if s.stored == nil {
		return nil
	}

	key := make([]uint64, len(scaled)+1) // The element-size coordinate stays 0.
	copy(key, scaled)
	e := ChunkEntry{
		Key:     ChunkKey{Scaled: key, Nbytes: uint32(nbytes), FilterMask: s.d.storedMask(scaled, mask)},
		Address: addr,
	}

	return s.stored(&e)
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
