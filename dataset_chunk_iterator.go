package hdf5

import (
	"context"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
)

// ChunkIterator provides memory-efficient iteration over dataset chunks.
// It reads one chunk at a time, allowing processing of datasets larger than available memory.
//
// Usage:
//
//	iter, err := dataset.ChunkIterator()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for iter.Next() {
//	    chunk, err := iter.Chunk()
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    processChunk(chunk)
//	}
//	if err := iter.Err(); err != nil {
//	    log.Fatal(err)
//	}
//
// The iterator follows the Go scanner pattern (bufio.Scanner).
// Only chunked datasets are supported; compact and contiguous datasets
// should use Read() or ReadSlice() directly.
type ChunkIterator struct {
	dataset     *Dataset
	chunkCoords [][]uint64
	chunkDims   []uint64
	datasetDims []uint64
	current     int
	err         error
	ctx         context.Context
	onProgress  func(current, total int)
}

// ChunkIterator returns an iterator for reading dataset chunks one at a time.
// This is memory-efficient for large chunked datasets.
//
// Returns an error if the dataset is not chunked (compact or contiguous layout).
// For non-chunked datasets, use Read() or ReadSlice() instead.
func (d *Dataset) ChunkIterator() (*ChunkIterator, error) {
	return d.ChunkIteratorWithContext(context.Background())
}

// ChunkIteratorWithContext returns an iterator with context support for cancellation.
// The context is checked before each Next() call, allowing graceful cancellation.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	iter, err := dataset.ChunkIteratorWithContext(ctx)
//	for iter.Next() {
//	    // Process chunk...
//	}
func (d *Dataset) ChunkIteratorWithContext(ctx context.Context) (*ChunkIterator, error) {
	// Read object header to get layout info.
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read object header: %w", err)
	}

	// Extract required messages.
	var layoutMsg, dataspaceMsg *core.HeaderMessage
	for _, msg := range header.Messages {
		switch msg.Type {
		case core.MsgDataLayout:
			layoutMsg = msg
		case core.MsgDataspace:
			dataspaceMsg = msg
		}
	}

	if layoutMsg == nil {
		return nil, errors.New("data layout message not found")
	}
	if dataspaceMsg == nil {
		return nil, errors.New("dataspace message not found")
	}

	// Parse layout.
	layout, err := core.ParseDataLayoutMessage(layoutMsg.Data, d.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout: %w", err)
	}

	// Validate layout is chunked.
	if !layout.IsChunked() {
		return nil, errors.New("ChunkIterator only supports chunked datasets; use Read() or ReadSlice() for compact/contiguous datasets")
	}

	// Parse dataspace.
	dataspace, err := core.ParseDataspaceMessage(dataspaceMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	// Get chunk coordinates from B-tree.
	chunkCoords, err := d.collectChunkCoordinates(layout, dataspace)
	if err != nil {
		return nil, fmt.Errorf("failed to collect chunk coordinates: %w", err)
	}

	return &ChunkIterator{
		dataset:     d,
		chunkCoords: chunkCoords,
		chunkDims:   layout.ChunkSize,
		datasetDims: dataspace.Dimensions,
		current:     0,
		ctx:         ctx,
	}, nil
}

// collectChunkCoordinates retrieves all chunk coordinates from the B-tree.
func (d *Dataset) collectChunkCoordinates(layout *core.DataLayoutMessage, dataspace *core.DataspaceMessage) ([][]uint64, error) {
	// Parse B-tree to get all chunks.
	btreeNode, err := core.ParseBTreeV1Node(
		d.file.osFile,
		layout.DataAddress,
		d.file.sb.OffsetSize,
		len(layout.ChunkSize),
		layout.ChunkSize,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chunk B-tree: %w", err)
	}

	allChunks, err := btreeNode.CollectAllChunks(d.file.osFile, d.file.sb.OffsetSize, layout.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to collect chunks: %w", err)
	}

	// Extract coordinates.
	ndims := len(dataspace.Dimensions)
	coords := make([][]uint64, 0, len(allChunks))
	for _, chunk := range allChunks {
		coord := make([]uint64, ndims)
		copy(coord, chunk.Key.Scaled[:ndims])
		coords = append(coords, coord)
	}

	return coords, nil
}

// Next advances to the next chunk. Returns false when iteration is complete
// or an error occurred. Check Err() after iteration to distinguish.
func (it *ChunkIterator) Next() bool {
	if it.err != nil {
		return false
	}

	// Check context for cancellation.
	if it.ctx != nil {
		if err := it.ctx.Err(); err != nil {
			it.err = err
			return false
		}
	}

	it.current++
	if it.current > len(it.chunkCoords) {
		return false
	}

	// Call progress callback if set.
	if it.onProgress != nil {
		it.onProgress(it.current, len(it.chunkCoords))
	}

	return true
}

// Chunk returns the data for the current chunk.
// Must be called after Next() returns true.
// Returns the chunk data as interface{} (typically []float64).
func (it *ChunkIterator) Chunk() (interface{}, error) {
	if it.current < 1 || it.current > len(it.chunkCoords) {
		return nil, errors.New("no current chunk: call Next() first")
	}

	coords := it.chunkCoords[it.current-1]
	start := make([]uint64, len(coords))
	count := make([]uint64, len(coords))

	for i := range coords {
		start[i] = coords[i] * it.chunkDims[i]
		count[i] = it.chunkDims[i]

		// Clamp to dataset bounds (last chunk may be partial).
		if start[i]+count[i] > it.datasetDims[i] {
			count[i] = it.datasetDims[i] - start[i]
		}
	}

	return it.dataset.ReadSlice(start, count)
}

// ChunkCoords returns the scaled coordinates of the current chunk.
// These are chunk indices, not element indices.
// For element indices, multiply by chunk dimensions.
func (it *ChunkIterator) ChunkCoords() []uint64 {
	if it.current < 1 || it.current > len(it.chunkCoords) {
		return nil
	}
	return it.chunkCoords[it.current-1]
}

// Progress returns the current chunk index and total chunk count.
// Useful for progress reporting.
func (it *ChunkIterator) Progress() (current, total int) {
	return it.current, len(it.chunkCoords)
}

// Total returns the total number of chunks in the dataset.
func (it *ChunkIterator) Total() int {
	return len(it.chunkCoords)
}

// Err returns any error that occurred during iteration.
// Should be checked after Next() returns false.
func (it *ChunkIterator) Err() error {
	return it.err
}

// OnProgress sets a callback function that is called after each Next().
// The callback receives the current chunk index (1-based) and total count.
//
// Example:
//
//	iter.OnProgress(func(current, total int) {
//	    fmt.Printf("Processing chunk %d/%d\n", current, total)
//	})
func (it *ChunkIterator) OnProgress(fn func(current, total int)) {
	it.onProgress = fn
}

// Reset resets the iterator to the beginning, allowing re-iteration.
func (it *ChunkIterator) Reset() {
	it.current = 0
	it.err = nil
}

// ChunkDims returns the chunk dimensions.
func (it *ChunkIterator) ChunkDims() []uint64 {
	return it.chunkDims
}

// DatasetDims returns the dataset dimensions.
func (it *ChunkIterator) DatasetDims() []uint64 {
	return it.datasetDims
}
