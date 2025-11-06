package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
)

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
	// Read object header to get dataset metadata
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read object header: %w", err)
	}

	// Extract dataspace to validate dimensions
	var dataspaceMsg *core.HeaderMessage
	for _, msg := range header.Messages {
		if msg.Type == core.MsgDataspace {
			dataspaceMsg = msg
			break
		}
	}

	if dataspaceMsg == nil {
		return nil, fmt.Errorf("dataspace message not found in dataset")
	}

	dataspace, err := core.ParseDataspaceMessage(dataspaceMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	// Validate dimensions match
	if len(start) != len(dataspace.Dimensions) {
		return nil, fmt.Errorf("start dimensions (%d) != dataset dimensions (%d)",
			len(start), len(dataspace.Dimensions))
	}
	if len(count) != len(dataspace.Dimensions) {
		return nil, fmt.Errorf("count dimensions (%d) != dataset dimensions (%d)",
			len(count), len(dataspace.Dimensions))
	}

	// Validate bounds (start + count must not exceed dataset dimensions)
	for i := range start {
		if start[i]+count[i] > dataspace.Dimensions[i] {
			return nil, fmt.Errorf("selection out of bounds in dimension %d: start=%d + count=%d > size=%d",
				i, start[i], count[i], dataspace.Dimensions[i])
		}
	}

	// Create simple hyperslab selection (stride=1, block=1)
	selection := &HyperslabSelection{
		Start:  start,
		Count:  count,
		Stride: nil, // Default to all 1s (contiguous)
		Block:  nil, // Default to all 1s (single elements)
	}

	// Fill in defaults for Stride and Block
	fillHyperslabDefaults(selection, len(dataspace.Dimensions))

	return d.readHyperslab(selection, header)
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
	// Read object header to get dataset metadata
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read object header: %w", err)
	}

	// Extract dataspace to validate dimensions
	var dataspaceMsg *core.HeaderMessage
	for _, msg := range header.Messages {
		if msg.Type == core.MsgDataspace {
			dataspaceMsg = msg
			break
		}
	}

	if dataspaceMsg == nil {
		return nil, fmt.Errorf("dataspace message not found in dataset")
	}

	dataspace, err := core.ParseDataspaceMessage(dataspaceMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	// Validate selection
	if err := validateHyperslabSelection(selection, dataspace.Dimensions); err != nil {
		return nil, fmt.Errorf("invalid selection: %w", err)
	}

	return d.readHyperslab(selection, header)
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
	for i := range dims {
		if err := validateDimensionBounds(sel, dims, i); err != nil {
			return err
		}
	}
	return nil
}

// validateDimensionBounds validates a single dimension's bounds.
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

	// Check bounds: start + (count-1)*stride + block must not exceed dimension
	lastCoord := sel.Start[dim] + (sel.Count[dim]-1)*sel.Stride[dim] + sel.Block[dim]
	if lastCoord > dims[dim] {
		return fmt.Errorf("selection out of bounds in dimension %d: "+
			"start=%d + (count-1)*stride + block = %d > size=%d",
			dim, sel.Start[dim], lastCoord, dims[dim])
	}
	return nil
}

// readHyperslab is the internal implementation for hyperslab reading.
// It dispatches to the appropriate layout-specific reader based on the dataset's storage layout.
func (d *Dataset) readHyperslab(selection *HyperslabSelection, header *core.ObjectHeader) (interface{}, error) {
	// Extract and parse messages
	messages, err := extractHyperslabMessages(header)
	if err != nil {
		return nil, err
	}

	parsedMsgs, err := parseHyperslabMessages(messages, d.file.sb)
	if err != nil {
		return nil, err
	}

	// Dispatch to appropriate layout reader
	return d.dispatchHyperslabReader(selection, parsedMsgs)
}

// hyperslabMessages holds raw message data extracted from object header.
type hyperslabMessages struct {
	datatype       *core.HeaderMessage
	dataspace      *core.HeaderMessage
	layout         *core.HeaderMessage
	filterPipeline *core.HeaderMessage
}

// parsedHyperslabMessages holds parsed message structures.
type parsedHyperslabMessages struct {
	datatype       *core.DatatypeMessage
	dataspace      *core.DataspaceMessage
	layout         *core.DataLayoutMessage
	filterPipeline *core.FilterPipelineMessage
}

// extractHyperslabMessages extracts required messages from object header.
func extractHyperslabMessages(header *core.ObjectHeader) (*hyperslabMessages, error) {
	msgs := &hyperslabMessages{}

	for _, msg := range header.Messages {
		switch msg.Type {
		case core.MsgDatatype:
			msgs.datatype = msg
		case core.MsgDataspace:
			msgs.dataspace = msg
		case core.MsgDataLayout:
			msgs.layout = msg
		case core.MsgFilterPipeline:
			msgs.filterPipeline = msg
		}
	}

	// Validate required messages
	if msgs.datatype == nil {
		return nil, fmt.Errorf("datatype message not found")
	}
	if msgs.dataspace == nil {
		return nil, fmt.Errorf("dataspace message not found")
	}
	if msgs.layout == nil {
		return nil, fmt.Errorf("data layout message not found")
	}

	return msgs, nil
}

// parseHyperslabMessages parses raw messages into structured types.
func parseHyperslabMessages(msgs *hyperslabMessages, sb *core.Superblock) (*parsedHyperslabMessages, error) {
	parsed := &parsedHyperslabMessages{}

	var err error

	parsed.datatype, err = core.ParseDatatypeMessage(msgs.datatype.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse datatype: %w", err)
	}

	parsed.dataspace, err = core.ParseDataspaceMessage(msgs.dataspace.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	parsed.layout, err = core.ParseDataLayoutMessage(msgs.layout.Data, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout: %w", err)
	}

	// Parse filter pipeline (optional)
	if msgs.filterPipeline != nil {
		parsed.filterPipeline, err = core.ParseFilterPipelineMessage(msgs.filterPipeline.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filter pipeline: %w", err)
		}
	}

	return parsed, nil
}

// dispatchHyperslabReader dispatches to appropriate layout-specific reader.
func (d *Dataset) dispatchHyperslabReader(
	selection *HyperslabSelection,
	msgs *parsedHyperslabMessages,
) (interface{}, error) {
	switch {
	case msgs.layout.IsCompact():
		return d.readHyperslabCompact(selection, msgs.datatype, msgs.dataspace, msgs.layout)
	case msgs.layout.IsContiguous():
		return d.readHyperslabContiguous(selection, msgs.datatype, msgs.dataspace, msgs.layout)
	case msgs.layout.IsChunked():
		return d.readHyperslabChunked(selection, msgs.datatype, msgs.dataspace, msgs.layout, msgs.filterPipeline)
	default:
		return nil, fmt.Errorf("unsupported layout class: %d", msgs.layout.Class)
	}
}

// calculateHyperslabOutputSize calculates the total number of elements in the hyperslab selection.
// For a hyperslab with stride and block parameters, the total is: product(Count[i] * Block[i]).
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

// readHyperslabCompact reads hyperslab from compact layout dataset.
// Compact layout stores data directly in the object header.
func (d *Dataset) readHyperslabCompact(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
) (interface{}, error) {
	// Compact data is stored in layout.CompactData
	// We need to extract the selected region from this data
	return extractHyperslabFromRawData(selection, datatype, dataspace, layout.CompactData)
}

// readHyperslabContiguous reads hyperslab from contiguous layout dataset.
// Contiguous layout stores data in one continuous block in the file.
//
// OPTIMIZED: Reads ONLY the bytes needed for the selection, not the entire dataset.
// For N-dimensional data with row-major order, we read only the rows/slices that contain selected data.
func (d *Dataset) readHyperslabContiguous(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
) (interface{}, error) {
	ndims := len(dataspace.Dimensions)

	// For 1D or simple contiguous selections, optimize by reading minimal data
	if ndims == 1 || isContiguousSelection(selection, dataspace.Dimensions) {
		return d.readContiguousOptimized(selection, datatype, dataspace, layout)
	}

	// For complex multi-dimensional selections with stride/block, use row-by-row reading
	return d.readContiguousRowByRow(selection, datatype, dataspace, layout)
}

// isContiguousSelection checks if selection is contiguous in memory (last dimension fully selected).
func isContiguousSelection(sel *HyperslabSelection, dims []uint64) bool {
	if len(dims) == 0 {
		return true
	}

	// Check if last dimension is contiguous (stride=1, block=1, covers full range or starts at 0)
	lastDim := len(dims) - 1
	if sel.Stride[lastDim] != 1 || sel.Block[lastDim] != 1 {
		return false
	}

	// If selecting entire last dimension, it's contiguous
	if sel.Count[lastDim]*sel.Block[lastDim] == dims[lastDim] {
		return true
	}

	return false
}

// readContiguousOptimized reads contiguous selections efficiently in one or few I/O operations.
func (d *Dataset) readContiguousOptimized(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
) (interface{}, error) {
	elementSize := uint64(datatype.Size)
	dims := dataspace.Dimensions

	// Calculate output size
	outputElements := calculateHyperslabOutputSize(selection)
	if outputElements == 0 {
		return []float64{}, nil
	}

	// For 1D or fully contiguous, read in one operation
	if len(dims) == 1 {
		// 1D case: single contiguous read
		startOffset := selection.Start[0] * elementSize
		byteCount := outputElements * elementSize

		rawData := make([]byte, byteCount)
		fileOffset := layout.DataAddress + startOffset

		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		_, err := d.file.osFile.ReadAt(rawData, int64(fileOffset))
		if err != nil {
			return nil, fmt.Errorf("failed to read 1D contiguous data: %w", err)
		}

		return convertToFloat64(rawData, datatype, outputElements)
	}

	// Multi-dimensional contiguous case
	// Read row-major contiguous block
	// Calculate start offset for first element
	startCoords := selection.Start
	startLinearOffset := calculateLinearOffset(startCoords, dims)
	startByteOffset := startLinearOffset * elementSize

	// For contiguous multi-D, we can read the bounding box
	outputData := make([]byte, outputElements*elementSize)
	fileOffset := layout.DataAddress + startByteOffset

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	_, err := d.file.osFile.ReadAt(outputData, int64(fileOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read contiguous data: %w", err)
	}

	return convertToFloat64(outputData, datatype, outputElements)
}

// readContiguousRowByRow reads selections row-by-row for non-contiguous patterns.
// This handles stride/block selections efficiently by reading only necessary rows.
func (d *Dataset) readContiguousRowByRow(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
) (interface{}, error) {
	elementSize := uint64(datatype.Size)
	dims := dataspace.Dimensions
	ndims := len(dims)

	// Calculate output size
	outputElements := calculateHyperslabOutputSize(selection)
	if outputElements == 0 {
		return []float64{}, nil
	}

	outputData := make([]byte, outputElements*elementSize)
	outputIdx := uint64(0)

	// For 2D, optimize by reading rows
	if ndims == 2 {
		return d.readContiguous2DOptimized(selection, datatype, dataspace, layout)
	}

	// For 3D+, use recursive extraction with targeted reads
	// Read minimal bounding box that contains all selected elements
	minCoords := make([]uint64, ndims)
	maxCoords := make([]uint64, ndims)

	for i := 0; i < ndims; i++ {
		minCoords[i] = selection.Start[i]
		maxCoords[i] = selection.Start[i] + (selection.Count[i]-1)*selection.Stride[i] + selection.Block[i]
	}

	// Calculate bounding box size
	boundingElements := uint64(1)
	for i := 0; i < ndims; i++ {
		boundingElements *= (maxCoords[i] - minCoords[i])
	}

	// Read bounding box
	rawData := make([]byte, boundingElements*elementSize)
	startOffset := calculateLinearOffset(minCoords, dims) * elementSize
	fileOffset := layout.DataAddress + startOffset

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	_, err := d.file.osFile.ReadAt(rawData, int64(fileOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read bounding box: %w", err)
	}

	// Extract selection from bounding box
	coords := make([]uint64, ndims)
	copy(coords, selection.Start)

	extractHyperslabRecursive(
		rawData, outputData,
		dims, selection,
		coords, 0,
		elementSize, &outputIdx,
	)

	return convertToFloat64(outputData, datatype, outputElements)
}

// readContiguous2DOptimized handles 2D contiguous datasets with row-by-row reading.
//
//nolint:gocognit // Complex algorithm for efficient 2D hyperslab reading
func (d *Dataset) readContiguous2DOptimized(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
) (interface{}, error) {
	elementSize := uint64(datatype.Size)
	dims := dataspace.Dimensions

	outputElements := calculateHyperslabOutputSize(selection)
	outputData := make([]byte, outputElements*elementSize)
	outputIdx := uint64(0)

	// Iterate through selected rows
	for iCount := uint64(0); iCount < selection.Count[0]; iCount++ {
		for iBlock := uint64(0); iBlock < selection.Block[0]; iBlock++ {
			row := selection.Start[0] + iCount*selection.Stride[0] + iBlock

			if row >= dims[0] {
				continue // Skip out of bounds
			}

			// For this row, read the selected columns
			for jCount := uint64(0); jCount < selection.Count[1]; jCount++ {
				for jBlock := uint64(0); jBlock < selection.Block[1]; jBlock++ {
					col := selection.Start[1] + jCount*selection.Stride[1] + jBlock

					if col >= dims[1] {
						continue // Skip out of bounds
					}

					// Calculate file offset for this element
					linearOffset := row*dims[1] + col
					byteOffset := layout.DataAddress + linearOffset*elementSize

					// Read single element
					//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
					_, err := d.file.osFile.ReadAt(
						outputData[outputIdx*elementSize:(outputIdx+1)*elementSize],
						int64(byteOffset),
					)
					if err != nil {
						return nil, fmt.Errorf("failed to read element at [%d,%d]: %w", row, col, err)
					}

					outputIdx++
				}
			}
		}
	}

	return convertToFloat64(outputData, datatype, outputElements)
}

// readHyperslabChunked reads hyperslab from chunked layout dataset.
// Chunked layout stores data in separate chunks indexed by a B-tree.
//
// OPTIMIZED: Reads ONLY the chunks that overlap with the selection.
// For a small selection in a large dataset, this dramatically reduces I/O.
func (d *Dataset) readHyperslabChunked(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	layout *core.DataLayoutMessage,
	filterPipeline *core.FilterPipelineMessage,
) (interface{}, error) {
	elementSize := uint64(datatype.Size)
	dims := dataspace.Dimensions
	chunkDims := layout.ChunkSize

	// Calculate output size
	outputElements := calculateHyperslabOutputSize(selection)
	if outputElements == 0 {
		return []float64{}, nil
	}

	// Find which chunks overlap with the selection
	overlappingChunks := findOverlappingChunks(selection, chunkDims, dims)

	if len(overlappingChunks) == 0 {
		// No chunks overlap (empty selection)
		return []float64{}, nil
	}

	// Parse B-tree to get chunk addresses
	btreeNode, err := core.ParseBTreeV1Node(
		d.file.osFile,
		layout.DataAddress,
		d.file.sb.OffsetSize,
		len(chunkDims),
		chunkDims,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chunk B-tree: %w", err)
	}

	// Build chunk index (scaled coordinates -> file address)
	chunkIndex := make(map[string]chunkIndexEntry)
	allChunks, err := btreeNode.CollectAllChunks(d.file.osFile, d.file.sb.OffsetSize, chunkDims)
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk index: %w", err)
	}

	for _, chunk := range allChunks {
		key := chunkCoordsToKey(chunk.Key.Scaled[:len(dims)])
		chunkIndex[key] = chunkIndexEntry{
			address: chunk.Address,
			nbytes:  uint64(chunk.Key.Nbytes),
		}
	}

	// Allocate output buffer
	outputData := make([]byte, outputElements*elementSize)
	outputIdx := uint64(0)

	// Read each overlapping chunk and extract relevant data
	for _, chunkCoord := range overlappingChunks {
		err := d.extractFromChunk(
			chunkCoord, chunkIndex, chunkDims, dims,
			selection, datatype, filterPipeline,
			outputData, &outputIdx,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to extract from chunk %v: %w", chunkCoord, err)
		}
	}

	// Convert bytes to float64
	return convertToFloat64(outputData, datatype, outputElements)
}

// chunkIndexEntry stores chunk location information.
type chunkIndexEntry struct {
	address uint64
	nbytes  uint64
}

// findOverlappingChunks identifies all chunks that overlap with the hyperslab selection.
// Returns chunk coordinates (scaled chunk indices, not element indices).
func findOverlappingChunks(sel *HyperslabSelection, chunkDims []uint32, datasetDims []uint64) [][]uint64 {
	ndims := len(sel.Start)

	// Calculate first and last chunk indices for each dimension
	firstChunk := make([]uint64, ndims)
	lastChunk := make([]uint64, ndims)

	for i := 0; i < ndims; i++ {
		// First chunk containing start of selection
		firstChunk[i] = sel.Start[i] / uint64(chunkDims[i])

		// Last chunk containing end of selection
		// End position = start + (count-1)*stride + block - 1
		endPos := sel.Start[i] + (sel.Count[i]-1)*sel.Stride[i] + sel.Block[i] - 1

		// Ensure we don't go beyond dataset bounds
		if endPos >= datasetDims[i] {
			endPos = datasetDims[i] - 1
		}

		lastChunk[i] = endPos / uint64(chunkDims[i])
	}

	// Generate all combinations of chunk coordinates
	return generateChunkCoordinates(firstChunk, lastChunk)
}

// generateChunkCoordinates generates all chunk coordinates in the range [first, last].
func generateChunkCoordinates(first, last []uint64) [][]uint64 {
	ndims := len(first)
	if ndims == 0 {
		return nil
	}

	// Calculate total number of chunks
	totalChunks := 1
	for i := 0; i < ndims; i++ {
		//nolint:gosec // G115: Chunk count calculation, overflow extremely unlikely in practice
		totalChunks *= int(last[i] - first[i] + 1)
	}

	result := make([][]uint64, 0, totalChunks)
	current := make([]uint64, ndims)
	copy(current, first)

	// Recursively generate coordinates
	generateChunkCoordsRecursive(first, last, current, 0, &result)

	return result
}

// generateChunkCoordsRecursive recursively generates chunk coordinates.
func generateChunkCoordsRecursive(first, last, current []uint64, dim int, result *[][]uint64) {
	ndims := len(first)

	if dim == ndims {
		// Base case: copy current coordinate to result
		coord := make([]uint64, ndims)
		copy(coord, current)
		*result = append(*result, coord)
		return
	}

	// Iterate through range for this dimension
	for i := first[dim]; i <= last[dim]; i++ {
		current[dim] = i
		generateChunkCoordsRecursive(first, last, current, dim+1, result)
	}
}

// chunkCoordsToKey converts chunk coordinates to a string key for map lookup.
func chunkCoordsToKey(coords []uint64) string {
	// Simple string representation: "x,y,z"
	parts := make([]string, len(coords))
	for i, c := range coords {
		parts[i] = fmt.Sprintf("%d", c)
	}
	return strings.Join(parts, ",")
}

// extractFromChunk reads a single chunk, decompresses if needed, and extracts relevant portion.
func (d *Dataset) extractFromChunk(
	chunkCoord []uint64,
	chunkIndex map[string]chunkIndexEntry,
	chunkDims []uint32,
	datasetDims []uint64,
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	filterPipeline *core.FilterPipelineMessage,
	outputData []byte,
	outputIdx *uint64,
) error {
	// Look up chunk address
	key := chunkCoordsToKey(chunkCoord)
	chunkInfo, exists := chunkIndex[key]
	if !exists {
		// Chunk doesn't exist (sparse dataset) - skip
		// In HDF5, missing chunks are filled with fill values (typically 0)
		// For now, we leave output buffer as-is (initialized to 0)
		return nil
	}

	elementSize := uint64(datatype.Size)

	// Read chunk data (use nbytes from index)
	chunkData := make([]byte, chunkInfo.nbytes)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	_, err := d.file.osFile.ReadAt(chunkData, int64(chunkInfo.address))
	if err != nil {
		return fmt.Errorf("failed to read chunk data: %w", err)
	}

	// Decompress if needed (using existing FilterPipelineMessage.ApplyFilters)
	if filterPipeline != nil {
		chunkData, err = filterPipeline.ApplyFilters(chunkData)
		if err != nil {
			return fmt.Errorf("failed to apply filters: %w", err)
		}
	}

	// Extract portion of this chunk that intersects with selection
	extractChunkPortion(
		chunkData, chunkCoord, chunkDims, datasetDims,
		selection, elementSize,
		outputData, outputIdx,
	)

	return nil
}

// extractChunkPortion extracts the portion of a chunk that intersects with the selection.
// This is the complex part - determining which elements from this chunk are in the selection.
func extractChunkPortion(
	chunkData []byte,
	chunkCoord []uint64,
	chunkDims []uint32,
	datasetDims []uint64,
	selection *HyperslabSelection,
	elementSize uint64,
	outputData []byte,
	outputIdx *uint64,
) {
	ndims := len(chunkCoord)

	// Calculate chunk's position in dataset
	chunkStart := make([]uint64, ndims)
	chunkEnd := make([]uint64, ndims)
	for i := 0; i < ndims; i++ {
		chunkStart[i] = chunkCoord[i] * uint64(chunkDims[i])
		chunkEnd[i] = chunkStart[i] + uint64(chunkDims[i])
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
		elementSize, outputData, outputIdx,
	)
}

// extractChunkPortionRecursive recursively extracts elements from chunk.
//
//nolint:gocognit // Complex recursive algorithm for N-dimensional chunk extraction
func extractChunkPortionRecursive(
	chunkData []byte,
	chunkStart, chunkEnd []uint64,
	chunkDims []uint32,
	selection *HyperslabSelection,
	coords []uint64,
	dim int,
	elementSize uint64,
	outputData []byte,
	outputIdx *uint64,
) {
	ndims := len(coords)

	if dim == ndims {
		// Base case: check if this coordinate is in the current chunk
		inChunk := true
		for i := 0; i < ndims; i++ {
			if coords[i] < chunkStart[i] || coords[i] >= chunkEnd[i] {
				inChunk = false
				break
			}
		}

		if !inChunk {
			return
		}

		// Calculate offset within chunk (relative to chunk start)
		chunkOffset := uint64(0)
		chunkStride := uint64(1)
		for i := ndims - 1; i >= 0; i-- {
			relCoord := coords[i] - chunkStart[i]
			chunkOffset += relCoord * chunkStride
			chunkStride *= uint64(chunkDims[i])
		}

		// Copy element from chunk to output
		srcOffset := chunkOffset * elementSize
		dstOffset := (*outputIdx) * elementSize

		if srcOffset+elementSize <= uint64(len(chunkData)) &&
			dstOffset+elementSize <= uint64(len(outputData)) {
			copy(outputData[dstOffset:dstOffset+elementSize],
				chunkData[srcOffset:srcOffset+elementSize])
			(*outputIdx)++
		}

		return
	}

	// Recursive case: iterate through selection in this dimension
	for c := uint64(0); c < selection.Count[dim]; c++ {
		blockStart := selection.Start[dim] + c*selection.Stride[dim]

		for b := uint64(0); b < selection.Block[dim]; b++ {
			coords[dim] = blockStart + b

			if coords[dim] >= chunkEnd[dim] {
				// Beyond this chunk, skip rest of this dimension
				return
			}

			extractChunkPortionRecursive(
				chunkData, chunkStart, chunkEnd, chunkDims,
				selection, coords, dim+1,
				elementSize, outputData, outputIdx,
			)
		}
	}
}

// extractHyperslabFromRawData extracts a hyperslab selection from raw dataset bytes.
// This handles the N-dimensional indexing and stride/block logic.
//
// The raw data is assumed to be in row-major (C-style) order, where the last dimension
// varies fastest. The hyperslab selection is also in row-major order.
//
// For MVP, this returns []float64 (matching existing Read() method).
// Future versions will support all datatypes with interface{} return.
func extractHyperslabFromRawData(
	selection *HyperslabSelection,
	datatype *core.DatatypeMessage,
	dataspace *core.DataspaceMessage,
	rawData []byte,
) (interface{}, error) {
	elementSize := uint64(datatype.Size)
	ndims := len(dataspace.Dimensions)

	// Calculate output size
	outputElements := calculateHyperslabOutputSize(selection)
	if outputElements == 0 {
		// Return empty array
		return []float64{}, nil
	}

	// Allocate output buffer
	outputData := make([]byte, outputElements*elementSize)
	outputIdx := uint64(0)

	// Use recursive iteration to handle arbitrary dimensionality
	coords := make([]uint64, ndims)
	copy(coords, selection.Start)

	extractHyperslabRecursive(
		rawData, outputData,
		dataspace.Dimensions, selection,
		coords, 0,
		elementSize, &outputIdx,
	)

	// Convert bytes to float64 (matching existing Read() behavior)
	// Future: support other types based on datatype
	return convertToFloat64(outputData, datatype, outputElements)
}

// extractHyperslabRecursive recursively iterates through hyperslab selection dimensions.
// This handles arbitrary dimensionality with stride and block parameters.
func extractHyperslabRecursive(
	rawData, outputData []byte,
	dims []uint64,
	selection *HyperslabSelection,
	coords []uint64,
	dimIdx int,
	elementSize uint64,
	outputIdx *uint64,
) {
	ndims := len(dims)

	if dimIdx == ndims {
		// Base case: we have a complete coordinate, copy the element
		// Calculate linear offset in raw data (row-major order)
		offset := calculateLinearOffset(coords, dims)
		byteOffset := offset * elementSize

		// Bounds check
		if byteOffset+elementSize > uint64(len(rawData)) {
			return // Skip out-of-bounds reads
		}

		// Copy element to output
		outputOffset := (*outputIdx) * elementSize
		copy(outputData[outputOffset:outputOffset+elementSize],
			rawData[byteOffset:byteOffset+elementSize])
		(*outputIdx)++
		return
	}

	// Recursive case: iterate through current dimension
	// For each count, we advance by stride and read block elements
	for c := uint64(0); c < selection.Count[dimIdx]; c++ {
		// Start position for this block
		blockStart := selection.Start[dimIdx] + c*selection.Stride[dimIdx]

		// Iterate through block elements
		for b := uint64(0); b < selection.Block[dimIdx]; b++ {
			coords[dimIdx] = blockStart + b

			// Bounds check for this dimension
			if coords[dimIdx] >= dims[dimIdx] {
				continue
			}

			// Recurse to next dimension
			extractHyperslabRecursive(
				rawData, outputData,
				dims, selection,
				coords, dimIdx+1,
				elementSize, outputIdx,
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

// convertToFloat64 is a wrapper around core's private convertToFloat64 function.
// This converts raw bytes to float64 array based on datatype.
// For MVP, we only support float64 output (matching existing Read() method).
func convertToFloat64(rawData []byte, datatype *core.DatatypeMessage, numElements uint64) ([]float64, error) {
	byteOrder := datatype.GetByteOrder()

	switch {
	case datatype.IsFloat64():
		return convertBytesToFloat64Direct(rawData, byteOrder, numElements)
	case datatype.IsFloat32():
		return convertBytesToFloat32AsFloat64(rawData, byteOrder, numElements)
	case datatype.IsInt32():
		return convertBytesToInt32AsFloat64(rawData, byteOrder, numElements)
	case datatype.IsInt64():
		return convertBytesToInt64AsFloat64(rawData, byteOrder, numElements)
	default:
		return nil, fmt.Errorf("unsupported datatype for conversion to float64")
	}
}

// convertBytesToFloat64Direct converts IEEE 754 double precision bytes to float64.
func convertBytesToFloat64Direct(rawData []byte, byteOrder binary.ByteOrder, numElements uint64) ([]float64, error) {
	result := make([]float64, numElements)
	for i := uint64(0); i < numElements; i++ {
		offset := i * 8
		if offset+8 > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated (float64)")
		}
		bits := byteOrder.Uint64(rawData[offset : offset+8])
		result[i] = math.Float64frombits(bits)
	}
	return result, nil
}

// convertBytesToFloat32AsFloat64 converts IEEE 754 single precision bytes to float64.
func convertBytesToFloat32AsFloat64(rawData []byte, byteOrder binary.ByteOrder, numElements uint64) ([]float64, error) {
	result := make([]float64, numElements)
	for i := uint64(0); i < numElements; i++ {
		offset := i * 4
		if offset+4 > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated (float32)")
		}
		bits := byteOrder.Uint32(rawData[offset : offset+4])
		result[i] = float64(math.Float32frombits(bits))
	}
	return result, nil
}

// convertBytesToInt32AsFloat64 converts 32-bit signed integer bytes to float64.
func convertBytesToInt32AsFloat64(rawData []byte, byteOrder binary.ByteOrder, numElements uint64) ([]float64, error) {
	result := make([]float64, numElements)
	for i := uint64(0); i < numElements; i++ {
		offset := i * 4
		if offset+4 > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated (int32)")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
		val := int32(byteOrder.Uint32(rawData[offset : offset+4]))
		result[i] = float64(val)
	}
	return result, nil
}

// convertBytesToInt64AsFloat64 converts 64-bit signed integer bytes to float64.
func convertBytesToInt64AsFloat64(rawData []byte, byteOrder binary.ByteOrder, numElements uint64) ([]float64, error) {
	result := make([]float64, numElements)
	for i := uint64(0); i < numElements; i++ {
		offset := i * 8
		if offset+8 > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated (int64)")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
		val := int64(byteOrder.Uint64(rawData[offset : offset+8]))
		result[i] = float64(val)
	}
	return result, nil
}
