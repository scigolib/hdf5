package hdf5

import (
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// sieveBufSize is the size of the buffer that serves small element runs of a
// contiguous dataset, the default raw-data sieve buffer of the C library
// (H5F_ACS_SIEVE_BUF_SIZE_DEF, H5Pfapl.c:121).
const sieveBufSize = 64 * 1024

// HyperslabSelection represents a rectangular selection in N-dimensional space.
// It follows the HDF5 hyperslab specification with start, count, stride, and block parameters.
//
// Parameters:
//   - Start: Starting coordinates in each dimension (0-based indexing)
//   - Count: Number of blocks to select in each dimension
//   - Stride: Step between blocks in each dimension (nil = default to all 1s)
//   - Block: Size of each block in each dimension (nil = default to all 1s)
//
// The total number of elements selected is: product(Count[i] * Block[i]) for all dimensions.
// Blocks must not overlap: where Count[i] > 1, Stride[i] must be at least Block[i].
//
// Example 1 - Simple slice (start=100, count=50 in 1D array):
//
//	sel := &HyperslabSelection{
//	    Start: []uint64{100},
//	    Count: []uint64{50},
//	}
//
// Example 2 - Strided selection (every 2nd element):
//
//	sel := &HyperslabSelection{
//	    Start:  []uint64{0, 0},
//	    Count:  []uint64{25, 25},  // 25 blocks in each dimension
//	    Stride: []uint64{2, 2},     // Every 2nd element
//	    Block:  []uint64{1, 1},     // Each block is 1x1
//	}
type HyperslabSelection struct {
	Start  []uint64
	Count  []uint64
	Stride []uint64 // nil means all 1s (contiguous selection)
	Block  []uint64 // nil means all 1s (single element blocks)
}

// ReadSlice reads a rectangular block from the dataset using simple start/count parameters.
// This is a convenience method for the common case of reading a contiguous rectangular region.
//
// Parameters:
//   - start: Starting coordinates in each dimension (0-based)
//   - count: Number of elements to read in each dimension
//
// The number of dimensions in start and count must match the dataset's dimensionality.
//
// Example (2D dataset):
//
//	// Read 50x50 block starting at position (100, 200)
//	data, err := dataset.ReadSlice([]uint64{100, 200}, []uint64{50, 50})
//
// Returns:
//   - interface{}: The selected data in the dataset's native type ([]float64, []int32, etc.)
//   - error: Error if selection is invalid or reading fails
func (d *Dataset) ReadSlice(start, count []uint64) (interface{}, error) {
	desc, err := d.descriptor()
	if err != nil {
		return nil, err
	}

	dims := desc.Dims()

	// Validate dimensions match
	if len(start) != len(dims) {
		return nil, fmt.Errorf("start dimensions (%d) != dataset dimensions (%d)",
			len(start), len(dims))
	}
	if len(count) != len(dims) {
		return nil, fmt.Errorf("count dimensions (%d) != dataset dimensions (%d)",
			len(count), len(dims))
	}

	// Validate bounds (start + count must not exceed dataset dimensions). The
	// subtraction form cannot wrap, unlike start+count.
	for i := range start {
		if count[i] > dims[i] || start[i] > dims[i]-count[i] {
			return nil, fmt.Errorf("selection out of bounds in dimension %d: start=%d + count=%d > size=%d",
				i, start[i], count[i], dims[i])
		}
	}

	// Create simple hyperslab selection (stride=1, block=1)
	selection := &HyperslabSelection{
		Start: start,
		Count: count,
	}
	fillHyperslabDefaults(selection, len(dims))

	return d.readHyperslab(selection, desc)
}

// ReadHyperslab reads data with full hyperslab parameters including stride and block.
// This provides complete control over the selection pattern, allowing strided and blocked selections.
//
// Parameters:
//   - selection: The hyperslab selection specification
//
// The selection is validated against the dataset's dimensions before reading.
//
// Example (read every 2nd element in 2D):
//
//	sel := &HyperslabSelection{
//	    Start:  []uint64{100, 200},
//	    Count:  []uint64{25, 25},   // 25 blocks
//	    Stride: []uint64{2, 2},      // Every 2nd element
//	    Block:  []uint64{1, 1},      // 1x1 blocks
//	}
//	data, err := dataset.ReadHyperslab(sel)
//
// Returns:
//   - interface{}: The selected data in the dataset's native type
//   - error: Error if selection is invalid or reading fails
func (d *Dataset) ReadHyperslab(selection *HyperslabSelection) (interface{}, error) {
	desc, err := d.descriptor()
	if err != nil {
		return nil, err
	}

	if err := validateHyperslabSelection(selection, desc.Dims()); err != nil {
		return nil, fmt.Errorf("invalid selection: %w", err)
	}

	return d.readHyperslab(selection, desc)
}

// validateHyperslabSelection validates a hyperslab selection against dataset dimensions.
// It checks dimension counts, bounds, and fills in default values for nil Stride/Block.
func validateHyperslabSelection(sel *HyperslabSelection, dims []uint64) error {
	ndims := len(dims)

	// Validate dimensionality
	if err := validateSelectionDimensions(sel, ndims); err != nil {
		return err
	}

	// Fill in defaults for nil Stride and Block
	fillHyperslabDefaults(sel, ndims)

	// Validate bounds for each dimension
	return validateHyperslabBounds(sel, dims)
}

// validateSelectionDimensions checks that selection arrays match dataset dimensionality.
func validateSelectionDimensions(sel *HyperslabSelection, ndims int) error {
	if len(sel.Start) != ndims {
		return fmt.Errorf("start dimensions (%d) != dataset dimensions (%d)",
			len(sel.Start), ndims)
	}
	if len(sel.Count) != ndims {
		return fmt.Errorf("count dimensions (%d) != dataset dimensions (%d)",
			len(sel.Count), ndims)
	}
	if sel.Stride != nil && len(sel.Stride) != ndims {
		return fmt.Errorf("stride dimensions (%d) != dataset dimensions (%d)",
			len(sel.Stride), ndims)
	}
	if sel.Block != nil && len(sel.Block) != ndims {
		return fmt.Errorf("block dimensions (%d) != dataset dimensions (%d)",
			len(sel.Block), ndims)
	}
	return nil
}

// fillHyperslabDefaults fills nil Stride and Block arrays with default values (all 1s).
func fillHyperslabDefaults(sel *HyperslabSelection, ndims int) {
	if sel.Stride == nil {
		sel.Stride = make([]uint64, ndims)
		for i := range sel.Stride {
			sel.Stride[i] = 1
		}
	}
	if sel.Block == nil {
		sel.Block = make([]uint64, ndims)
		for i := range sel.Block {
			sel.Block[i] = 1
		}
	}
}

// validateHyperslabBounds checks that selection parameters are valid and within bounds.
func validateHyperslabBounds(sel *HyperslabSelection, dims []uint64) error {
	// CVE-2025-44905 fix: Validate hyperslab bounds with overflow checking.
	if err := utils.ValidateHyperslabBounds(sel.Start, sel.Count, sel.Stride, dims); err != nil {
		return fmt.Errorf("hyperslab bounds validation failed: %w", err)
	}

	// CVE-2025-44905 fix: Calculate total elements with overflow check.
	if _, err := utils.CalculateHyperslabElements(sel.Count); err != nil {
		return fmt.Errorf("hyperslab too large: %w", err)
	}

	for i := range dims {
		if err := validateDimensionBounds(sel, dims, i); err != nil {
			return err
		}
	}
	return nil
}

// validateDimensionBounds validates a single dimension's bounds.
//
// Overlapping blocks are rejected as the C library does (H5Shyper.c:10018-10019):
// besides selecting an element twice, they would let Count×Block exceed the
// dimension, and the output size is computed from that product. The end
// coordinate is computed with overflow checks, because Start, Stride and Block
// come from the caller and a wrapped sum would pass the comparison.
func validateDimensionBounds(sel *HyperslabSelection, dims []uint64, dim int) error {
	if sel.Count[dim] == 0 {
		return fmt.Errorf("count must be > 0 in dimension %d", dim)
	}
	if sel.Stride[dim] == 0 {
		return fmt.Errorf("stride must be > 0 in dimension %d", dim)
	}
	if sel.Block[dim] == 0 {
		return fmt.Errorf("block must be > 0 in dimension %d", dim)
	}
	if sel.Count[dim] > 1 && sel.Stride[dim] < sel.Block[dim] {
		return fmt.Errorf("hyperslab blocks overlap in dimension %d: stride=%d < block=%d",
			dim, sel.Stride[dim], sel.Block[dim])
	}

	// Check bounds: start + (count-1)*stride + block must not exceed dimension
	lastCoord, err := hyperslabEnd(sel.Start[dim], sel.Count[dim], sel.Stride[dim], sel.Block[dim])
	if err != nil || lastCoord > dims[dim] {
		return fmt.Errorf("selection out of bounds in dimension %d: "+
			"start=%d + (count-1)*stride + block > size=%d",
			dim, sel.Start[dim], dims[dim])
	}
	return nil
}

// hyperslabEnd returns start + (count-1)*stride + block, the coordinate one
// past the last selected element, rejecting overflow.
func hyperslabEnd(start, count, stride, block uint64) (uint64, error) {
	span, err := utils.SafeMultiply(count-1, stride)
	if err != nil {
		return 0, err
	}

	end, err := utils.SafeAdd(start, span)
	if err != nil {
		return 0, err
	}

	return utils.SafeAdd(end, block)
}

// readHyperslab dispatches a validated selection to the reader of the
// dataset's storage layout. The layout was validated when the descriptor was
// built, so every reader works from measured storage (ADR-004 §1).
func (d *Dataset) readHyperslab(selection *HyperslabSelection, desc *core.DatasetDescriptor) (interface{}, error) {
	limit := d.file.ReadLimit()

	// Every layout converts to float64 at the end; refuse a datatype without
	// that conversion before any storage is read or filled.
	if err := core.CheckFloat64Convertible(desc.Datatype()); err != nil {
		return nil, err
	}

	switch desc.Layout().Class {
	case core.LayoutCompact:
		src := &compactSource{data: desc.Layout().CompactData}
		return readHyperslabRuns(selection, desc, src, limit)
	case core.LayoutContiguous:
		if efl := desc.ExternalFiles(); efl != nil {
			// The external files, read through the same sieve as storage in
			// the file; offsets are from the start of the dataset's bytes.
			src := &contiguousSource{
				r:       efl.Reader(d.file.sb.ExternalFiles),
				storage: desc.StorageSize(),
			}
			return readHyperslabRuns(selection, desc, src, limit)
		}
		if !desc.ContiguousAllocated() {
			return readHyperslabRuns(selection, desc, fillSource{desc: desc}, limit)
		}
		src := newContiguousSource(d.file.reader, desc)
		return readHyperslabRuns(selection, desc, src, limit)
	case core.LayoutChunked:
		return d.readHyperslabChunked(selection, desc, limit)
	default:
		return nil, fmt.Errorf("unsupported layout class: %d", desc.Layout().Class)
	}
}

// calculateHyperslabOutputSize calculates the total number of elements in the hyperslab selection.
// For a hyperslab with stride and block parameters, the total is: product(Count[i] * Block[i]).
//
// The product cannot overflow for a validated selection: blocks do not overlap
// and lie inside the dataset, so Count[i]*Block[i] ≤ dims[i], and the product
// of the dimensions was overflow-checked when the descriptor was built.
func calculateHyperslabOutputSize(sel *HyperslabSelection) uint64 {
	if len(sel.Count) == 0 {
		return 0
	}

	total := uint64(1)
	for i := range sel.Count {
		blockSize := sel.Block[i]
		if blockSize == 0 {
			blockSize = 1 // Default if not set
		}
		total *= sel.Count[i] * blockSize
	}

	return total
}

// runSource reads a run of bytes of a dataset's storage, addressed by byte
// offset from the start of the storage.
type runSource interface {
	readRun(dst []byte, off uint64) error
}

// compactSource serves runs from compact data held in the layout message. The
// descriptor validated that it holds exactly the dataset's bytes
// (H5D__compact_init).
type compactSource struct {
	data []byte
}

func (s *compactSource) readRun(dst []byte, off uint64) error {
	n := uint64(len(dst))
	if off > uint64(len(s.data)) || n > uint64(len(s.data))-off {
		return fmt.Errorf("compact data run at offset %d size %d exceeds %d bytes", off, n, len(s.data))
	}

	copy(dst, s.data[off:off+n])
	return nil
}

// fillSource serves runs of unallocated contiguous storage: every run reads as
// the fill value (H5D__read, H5Dio.c:239-279). Runs hold whole elements.
type fillSource struct {
	desc *core.DatasetDescriptor
}

func (s fillSource) readRun(dst []byte, _ uint64) error {
	return s.desc.FillUnallocated(dst)
}

// contiguousSource serves runs of a contiguous dataset through a sieve buffer,
// the way H5D__contig_readvv_sieve_cb (H5Dcontig.c:1034) does: a run larger
// than the buffer is read straight into the result, a smaller one is copied
// from a buffer refilled at the run's offset. Small strided runs therefore cost
// one read per buffer instead of one per element, and memory stays bounded.
type contiguousSource struct {
	r       io.ReaderAt
	addr    uint64 // Storage address; undefined means not allocated.
	storage uint64 // Validated storage size in bytes (H5D__contig_check).

	sieve    []byte
	sieveOff uint64
}

func newContiguousSource(r io.ReaderAt, desc *core.DatasetDescriptor) *contiguousSource {
	return &contiguousSource{
		r:       r,
		addr:    desc.Layout().DataAddress,
		storage: desc.StorageSize(),
	}
}

func (s *contiguousSource) readRun(dst []byte, off uint64) error {
	n := uint64(len(dst))
	if off > s.storage || n > s.storage-off {
		return fmt.Errorf("contiguous run at offset %d size %d exceeds the dataset storage of %d bytes",
			off, n, s.storage)
	}

	if s.sieve != nil && off >= s.sieveOff && off+n <= s.sieveOff+uint64(len(s.sieve)) {
		start := off - s.sieveOff
		copy(dst, s.sieve[start:start+n])
		return nil
	}

	if n > sieveBufSize {
		return readFullAt(s.r, dst, s.addr+off)
	}

	// Refill the sieve at this offset, never past the end of the storage.
	// The buffer never exceeds the storage, as in C (H5Dcontig.c:559-562).
	fill := min(uint64(sieveBufSize), s.storage-off)
	if s.sieve == nil {
		s.sieve = make([]byte, 0, min(uint64(sieveBufSize), s.storage))
	}
	s.sieve = s.sieve[:fill]
	if err := readFullAt(s.r, s.sieve, s.addr+off); err != nil {
		s.sieve = nil
		return err
	}
	s.sieveOff = off

	copy(dst, s.sieve[:n])
	return nil
}

// readFullAt reads len(dst) bytes at addr; a short read is an error, as in
// core.ReadBlock. The address lies inside storage the descriptor validated
// against the file, so it fits in int64.
func readFullAt(r io.ReaderAt, dst []byte, addr uint64) error {
	//nolint:gosec // G115: addr is inside validated storage, which lies inside the file
	n, err := r.ReadAt(dst, int64(addr))
	if n == len(dst) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("dataset data: read at offset %d size %d: %w", addr, len(dst), err)
	}
	return fmt.Errorf("dataset data: short read at offset %d: got %d of %d bytes", addr, n, len(dst))
}

// readHyperslabRuns reads a selection from contiguous or compact storage.
//
// Storage is row-major, so the selection decomposes into runs of consecutive
// elements. Trailing dimensions that are selected in full merge into each run,
// and so do abutting blocks of the last partly selected dimension, so a
// selection that is one contiguous range costs one read. Runs are visited in
// the selection's row-major order, which is the order of the result.
func readHyperslabRuns(
	sel *HyperslabSelection,
	desc *core.DatasetDescriptor,
	src runSource,
	limit uint64,
) (interface{}, error) {
	nelmts := calculateHyperslabOutputSize(sel)
	if nelmts == 0 {
		return []float64{}, nil
	}

	resultElemSize, err := core.Float64ResultSize(desc.Datatype())
	if err != nil {
		return nil, err
	}
	outSize, err := desc.ResultSize(nelmts, resultElemSize, limit)
	if err != nil {
		return nil, err
	}

	out := make([]byte, outSize)
	w := &runWriter{
		sel:   sel,
		dims:  desc.Dims(),
		esize: desc.ElementSize(),
		src:   src,
		out:   out,
	}
	w.plan()

	coords := make([]uint64, len(w.dims))
	if err := w.walk(coords, 0); err != nil {
		return nil, err
	}

	return core.ConvertToFloat64(out, desc.Datatype(), nelmts)
}

// runWriter copies the runs of one selection into the result buffer.
type runWriter struct {
	sel   *HyperslabSelection
	dims  []uint64
	esize uint64
	src   runSource
	out   []byte
	pos   uint64 // Next byte to fill in out.

	runDim int    // The dimension whose blocks form the runs.
	inner  uint64 // Elements per unit of runDim: the product of the trailing dimensions.
	merged bool   // All blocks of runDim abut, so one run covers them.
}

// plan finds the run dimension: the last dimension, from the end, that is not
// selected in full. Dimensions after it contribute whole rows to every run.
func (w *runWriter) plan() {
	w.inner = 1
	w.runDim = len(w.dims) - 1

	for w.runDim > 0 && w.fullySelected(w.runDim) {
		w.inner *= w.dims[w.runDim]
		w.runDim--
	}

	d := w.runDim
	w.merged = w.sel.Count[d] == 1 || w.sel.Stride[d] == w.sel.Block[d]
}

// fullySelected reports whether dimension d is selected from 0 to its end with
// no gaps.
func (w *runWriter) fullySelected(d int) bool {
	s := w.sel
	gapless := s.Count[d] == 1 || s.Stride[d] == s.Block[d]

	return s.Start[d] == 0 && gapless && s.Count[d]*s.Block[d] == w.dims[d]
}

// walk visits every selected coordinate of the dimensions before the run
// dimension in row-major order and reads the runs of each.
func (w *runWriter) walk(coords []uint64, dim int) error {
	if dim == w.runDim {
		return w.readRuns(coords)
	}

	s := w.sel
	for c := uint64(0); c < s.Count[dim]; c++ {
		for b := uint64(0); b < s.Block[dim]; b++ {
			coords[dim] = s.Start[dim] + c*s.Stride[dim] + b
			if err := w.walk(coords, dim+1); err != nil {
				return err
			}
		}
	}

	return nil
}

// readRuns reads the runs of the run dimension for one prefix coordinate.
// coords[runDim+1:] stay zero, so the linear offset of coords is the offset of
// the run's first element.
func (w *runWriter) readRuns(coords []uint64) error {
	s := w.sel
	d := w.runDim

	runs, runUnits := s.Count[d], s.Block[d]
	if w.merged {
		runs, runUnits = 1, s.Count[d]*s.Block[d]
	}

	runBytes := runUnits * w.inner * w.esize
	for c := uint64(0); c < runs; c++ {
		coords[d] = s.Start[d] + c*s.Stride[d]
		off := calculateLinearOffset(coords, w.dims) * w.esize

		if err := w.src.readRun(w.out[w.pos:w.pos+runBytes], off); err != nil {
			return err
		}
		w.pos += runBytes
	}

	return nil
}

// readHyperslabChunked reads hyperslab from chunked layout dataset.
// Chunked layout stores data in separate chunks indexed by a B-tree.
//
// Only the stored chunks are visited: the B-tree entries (validated and
// bounded by DatasetDescriptor.CollectChunks) are filtered by the range of
// chunks the selection overlaps. The grid of overlapping chunk coordinates is
// never materialized, which used to cost a slice and a formatted map key per
// grid cell even for chunks that were never written (review finding M1).
// Missing chunks leave their elements zero.
//
// The result is sized under the read limit (DatasetDescriptor.ResultSize): a
// chunked dataset may legitimately be larger than the file, because unwritten
// chunks read as the fill value (H5Dchunk.c:3041-3046).
func (d *Dataset) readHyperslabChunked(
	selection *HyperslabSelection,
	desc *core.DatasetDescriptor,
	limit uint64,
) (interface{}, error) {
	dims := desc.Dims()
	chunkDims := desc.ChunkDims()

	// Calculate output size
	outputElements := calculateHyperslabOutputSize(selection)
	if outputElements == 0 {
		return []float64{}, nil
	}

	resultElemSize, err := core.Float64ResultSize(desc.Datatype())
	if err != nil {
		return nil, err
	}
	outSize, err := desc.ResultSize(outputElements, resultElemSize, limit)
	if err != nil {
		return nil, err
	}

	first, last, err := chunkRange(selection, chunkDims, dims)
	if err != nil {
		return nil, fmt.Errorf("failed to compute overlapping chunks: %w", err)
	}

	chunks, err := desc.CollectChunks(d.file.reader, d.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk index: %w", err)
	}

	outputData := make([]byte, outSize)

	// Chunks never written read as the fill value (H5Dchunk.c:2936-2949,
	// 3041-3046): fill the result first, the stored chunks overwrite it below.
	if chunksMissingInRange(chunks, first, last, len(dims)) {
		if err := desc.FillUnallocated(outputData); err != nil {
			return nil, err
		}
	}

	// Each element is placed at the output offset computed from its
	// coordinates (see extractChunkPortionRecursive), NOT a running counter:
	// the chunks are visited in B-tree order, which is not the selection's
	// row-major order, and missing sparse chunks leave gaps.
	for i := range chunks {
		coord := chunks[i].Key.Scaled[:len(dims)]
		if !chunkInRange(coord, first, last) {
			continue
		}

		if err := d.extractFromChunk(&chunks[i], chunkDims, dims, selection, desc, outputData); err != nil {
			return nil, fmt.Errorf("failed to extract from chunk %v: %w", coord, err)
		}
	}

	// Convert bytes to float64
	return core.ConvertToFloat64(outputData, desc.Datatype(), outputElements)
}

// chunkRange returns, per dimension, the first and last chunk indices the
// hyperslab selection overlaps (scaled chunk indices, not element indices).
func chunkRange(sel *HyperslabSelection, chunkDims, datasetDims []uint64) (first, last []uint64, err error) {
	ndims := len(sel.Start)

	// The selection and the dataset must agree on the rank, and the chunk grid
	// must cover it (the descriptor checks rank+1 layout dimensions); zero
	// dimensions are rejected at layout parse, but the check is cheap and keeps
	// this function safe on its own.
	if len(datasetDims) != ndims || len(chunkDims) < ndims {
		return nil, nil, fmt.Errorf("rank mismatch: selection %d, chunk %d, dataset %d",
			ndims, len(chunkDims), len(datasetDims))
	}

	for i, dim := range chunkDims {
		if dim == 0 {
			return nil, nil, fmt.Errorf("chunk dimension %d is zero", i)
		}
	}

	first = make([]uint64, ndims)
	last = make([]uint64, ndims)

	for i := 0; i < ndims; i++ {
		first[i] = sel.Start[i] / chunkDims[i]

		// End position = start + (count-1)*stride + block - 1, within the
		// dataset (the selection was validated against it).
		endPos := sel.Start[i] + (sel.Count[i]-1)*sel.Stride[i] + sel.Block[i] - 1
		if endPos >= datasetDims[i] {
			endPos = datasetDims[i] - 1
		}

		last[i] = endPos / chunkDims[i]
	}

	return first, last, nil
}

// chunksMissingInRange reports whether the chunk grid cells in [first, last]
// outnumber the stored chunks there, i.e. some chunk the selection may touch
// was never written. The descriptor keeps every chunk key inside the grid.
func chunksMissingInRange(chunks []core.ChunkEntry, first, last []uint64, rank int) bool {
	cells := uint64(1)
	for i := range first {
		var err error
		if cells, err = utils.SafeMultiply(cells, last[i]-first[i]+1); err != nil {
			return true
		}
	}

	var present uint64
	for i := range chunks {
		if chunkInRange(chunks[i].Key.Scaled[:rank], first, last) {
			present++
		}
	}

	return present < cells
}

// chunkInRange reports whether chunk coordinates lie within [first, last] in
// every dimension.
func chunkInRange(coord, first, last []uint64) bool {
	for i := range first {
		if coord[i] < first[i] || coord[i] > last[i] {
			return false
		}
	}

	return true
}

// extractFromChunk reads a single chunk, decompresses if needed, and extracts relevant portion.
func (d *Dataset) extractFromChunk(
	chunk *core.ChunkEntry,
	chunkDims []uint64,
	datasetDims []uint64,
	selection *HyperslabSelection,
	desc *core.DatasetDescriptor,
	outputData []byte,
) error {
	// Read chunk data. The size is a uint32 from the B-tree key, i.e. file
	// metadata, so it goes through the block funnel: the sibling whole-dataset
	// path validates the same field, and on a 32-bit target make() would panic
	// above MaxInt.
	chunkData, err := core.ReadBlock(d.file.reader, d.file.sb, chunk.Address,
		uint64(chunk.Key.Nbytes), utils.MaxChunkSize, "chunk data")
	if err != nil {
		return fmt.Errorf("failed to read chunk data: %w", err)
	}

	// Decode through the filter pipeline; no filter may produce more than the
	// chunk's byte size (review finding H4).
	if filterPipeline := desc.FilterPipeline(); filterPipeline != nil {
		chunkData, err = filterPipeline.ApplyFiltersMasked(chunkData, chunk.Key.FilterMask, desc.ChunkBytes())
		if err != nil {
			return fmt.Errorf("failed to apply filters: %w", err)
		}
	}

	// Extract portion of this chunk that intersects with selection
	extractChunkPortion(
		chunkData, chunk.Key.Scaled[:len(datasetDims)], chunkDims, datasetDims,
		selection, desc.ElementSize(),
		outputData,
	)

	return nil
}

// extractChunkPortion extracts the portion of a chunk that intersects with the selection.
// This is the complex part - determining which elements from this chunk are in the selection.
func extractChunkPortion(
	chunkData []byte,
	chunkCoord []uint64,
	chunkDims []uint64,
	datasetDims []uint64,
	selection *HyperslabSelection,
	elementSize uint64,
	outputData []byte,
) {
	ndims := len(chunkCoord)

	// Calculate chunk's position in dataset
	chunkStart := make([]uint64, ndims)
	chunkEnd := make([]uint64, ndims)
	for i := 0; i < ndims; i++ {
		chunkStart[i] = chunkCoord[i] * chunkDims[i]
		chunkEnd[i] = chunkStart[i] + chunkDims[i]
		if chunkEnd[i] > datasetDims[i] {
			chunkEnd[i] = datasetDims[i]
		}
	}

	// Iterate through selection and copy elements that are in this chunk
	coords := make([]uint64, ndims)
	copy(coords, selection.Start)

	extractChunkPortionRecursive(
		chunkData, chunkStart, chunkEnd, chunkDims,
		selection, coords, 0,
		elementSize, outputData,
	)
}

// selectionOutputIndex returns the row-major position of a dataset
// coordinate within the hyperslab selection's output buffer. This decouples
// an element's output slot from the order chunks happen to be visited, so a
// selection that spans multiple chunks (or skips a missing sparse chunk)
// still lands every element in its correct place. The per-dimension output
// extent is Count*Block; for the common stride=1, block=1 ReadSlice this is
// simply coord-start.
func selectionOutputIndex(coords []uint64, sel *HyperslabSelection) uint64 {
	idx := uint64(0)
	stride := uint64(1)
	for i := len(coords) - 1; i >= 0; i-- {
		rel := coords[i] - sel.Start[i]
		// One block: the stride is not used and may be below the block length.
		var pos uint64
		if sel.Count[i] == 1 || sel.Stride[i] <= 1 {
			pos = rel
		} else {
			pos = (rel/sel.Stride[i])*sel.Block[i] + rel%sel.Stride[i]
		}
		idx += pos * stride
		stride *= sel.Count[i] * sel.Block[i]
	}
	return idx
}

// extractChunkPortionRecursive recursively extracts elements from chunk.
//
// In each dimension the walk starts at the first selected coordinate that can
// lie in the chunk, not at selection.Start: restarting at the selection's
// start for every chunk made the work O(chunks × selected elements) (review
// finding M2). With blocks that do not overlap (stride ≥ block, enforced by
// validateHyperslabBounds as H5Shyper.c does), block c covers
// [start+c·stride, start+c·stride+block), so the first block reaching
// chunkStart is c = (chunkStart-start)/stride, and inside a block that starts
// before the chunk the walk skips to chunkStart directly. The work per chunk is
// then the number of selected elements inside it.
//
//nolint:gocognit // Complex recursive algorithm for N-dimensional chunk extraction
func extractChunkPortionRecursive(
	chunkData []byte,
	chunkStart, chunkEnd []uint64,
	chunkDims []uint64,
	selection *HyperslabSelection,
	coords []uint64,
	dim int,
	elementSize uint64,
	outputData []byte,
) {
	ndims := len(coords)

	if dim == ndims {
		// Base case: check if this coordinate is in the current chunk
		for i := 0; i < ndims; i++ {
			if coords[i] < chunkStart[i] || coords[i] >= chunkEnd[i] {
				return
			}
		}

		// Calculate offset within chunk (relative to chunk start)
		chunkOffset := uint64(0)
		chunkStride := uint64(1)
		for i := ndims - 1; i >= 0; i-- {
			relCoord := coords[i] - chunkStart[i]
			chunkOffset += relCoord * chunkStride
			chunkStride *= chunkDims[i]
		}

		// Copy element from chunk to its coordinate-derived output slot.
		// Using the row-major position within the selection (not a running
		// counter) keeps the output correct regardless of chunk visitation
		// order or missing sparse chunks.
		srcOffset := chunkOffset * elementSize
		dstOffset := selectionOutputIndex(coords, selection) * elementSize

		if srcOffset+elementSize <= uint64(len(chunkData)) &&
			dstOffset+elementSize <= uint64(len(outputData)) {
			copy(outputData[dstOffset:dstOffset+elementSize],
				chunkData[srcOffset:srcOffset+elementSize])
		}

		return
	}

	start, stride := selection.Start[dim], selection.Stride[dim]
	// With one block the stride is meaningless (and may be below the block
	// length, which validation allows only then), so the formula applies only
	// to several blocks, where stride ≥ block holds.
	firstBlock := uint64(0)
	if selection.Count[dim] > 1 && chunkStart[dim] > start {
		firstBlock = (chunkStart[dim] - start) / stride
	}

	// Recursive case: iterate through the blocks of this dimension that can
	// intersect [chunkStart, chunkEnd).
	for c := firstBlock; c < selection.Count[dim]; c++ {
		blockStart := start + c*stride
		if blockStart >= chunkEnd[dim] {
			return // Every later block lies beyond this chunk.
		}

		// Skip the part of the block before the chunk in one step: a block
		// may be as long as the dimension.
		firstInBlock := uint64(0)
		if chunkStart[dim] > blockStart {
			firstInBlock = chunkStart[dim] - blockStart
		}

		for b := firstInBlock; b < selection.Block[dim]; b++ {
			coords[dim] = blockStart + b
			if coords[dim] >= chunkEnd[dim] {
				break
			}

			extractChunkPortionRecursive(
				chunkData, chunkStart, chunkEnd, chunkDims,
				selection, coords, dim+1,
				elementSize, outputData,
			)
		}
	}
}

// calculateLinearOffset calculates the linear byte offset for N-dimensional coordinates.
// Uses row-major (C-style) indexing: last dimension varies fastest.
func calculateLinearOffset(coords, dims []uint64) uint64 {
	offset := uint64(0)
	stride := uint64(1)

	// Start from last dimension (varies fastest in row-major order)
	for i := len(coords) - 1; i >= 0; i-- {
		offset += coords[i] * stride
		stride *= dims[i]
	}

	return offset
}

// (datatype conversion is delegated to core.ConvertToFloat64 — see the
// chunked/contiguous readers above — so the hyperslab path supports exactly
// the same datatypes as the whole-dataset Read(), including fixed-point
// integers of every width and sign.)
