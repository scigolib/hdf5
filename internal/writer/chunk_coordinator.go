package writer

import "fmt"

// ChunkCoordinator handles N-dimensional dataset chunking.
//
// This coordinator manages the mapping between:
// - Dataset dimensions and chunk dimensions
// - Linear chunk indices and N-dimensional chunk coordinates
// - Dataset data layout and chunk data extraction
//
// Key Concepts:
//   - Dataset dimensions: Total size of dataset in each dimension
//   - Chunk dimensions: Size of each chunk in each dimension
//   - Chunk coordinates: Scaled indices [dim0, dim1, ..., dimN]
//     where coordinate[i] = element_index[i] / chunk_dim[i]
//   - Edge chunks: Partial chunks at dataset boundaries
//
// Example (2D dataset):
//
//	Dataset: 25x35 elements
//	Chunks: 10x10 elements
//	Result: 3x4 = 12 total chunks
//	  - Chunk [0,0]: 10x10 (full)
//	  - Chunk [0,3]: 10x5 (partial in dim 1)
//	  - Chunk [2,0]: 5x10 (partial in dim 0)
//	  - Chunk [2,3]: 5x5 (partial in both dims)
type ChunkCoordinator struct {
	datasetDims []uint64 // Dataset dimensions [dim0, dim1, ..., dimN]
	chunkDims   []uint64 // Chunk dimensions [dim0, dim1, ..., dimN]
	numChunks   []uint64 // Number of chunks per dimension
}

// NewChunkCoordinator creates coordinator.
//
// Calculates the number of chunks needed in each dimension using
// ceiling division: numChunks[i] = ceil(datasetDims[i] / chunkDims[i])
//
// Parameters:
//   - datasetDims: Dataset size in each dimension
//   - chunkDims: Chunk size in each dimension
//
// Returns:
//   - ChunkCoordinator: Ready to use
//   - error: If dimensions mismatch
//
// Example:
//
//	// 2D dataset: 100x200 elements, chunks: 10x20
//	coord, err := NewChunkCoordinator(
//	    []uint64{100, 200},
//	    []uint64{10, 20},
//	)
//	// Result: 10x10 = 100 total chunks
func NewChunkCoordinator(datasetDims, chunkDims []uint64) (*ChunkCoordinator, error) {
	if len(datasetDims) != len(chunkDims) {
		return nil, fmt.Errorf("dimensions mismatch: dataset has %d dims, chunk has %d dims",
			len(datasetDims), len(chunkDims))
	}

	if len(datasetDims) == 0 {
		return nil, fmt.Errorf("dataset must have at least 1 dimension")
	}

	// Validate no zero dimensions
	for i, dim := range datasetDims {
		if dim == 0 {
			return nil, fmt.Errorf("dataset dimension %d cannot be zero", i)
		}
	}

	for i, dim := range chunkDims {
		if dim == 0 {
			return nil, fmt.Errorf("chunk dimension %d cannot be zero", i)
		}
	}

	// Calculate number of chunks per dimension (ceiling division)
	numChunks := make([]uint64, len(datasetDims))
	for i := range datasetDims {
		numChunks[i] = (datasetDims[i] + chunkDims[i] - 1) / chunkDims[i]
	}

	return &ChunkCoordinator{
		datasetDims: datasetDims,
		chunkDims:   chunkDims,
		numChunks:   numChunks,
	}, nil
}

// GetTotalChunks returns total chunk count.
//
// Calculates the total number of chunks by multiplying
// the number of chunks in each dimension.
//
// Returns:
//   - uint64: Total number of chunks in dataset
//
// Example:
//
//	// Dataset: 100x200, chunks: 10x20
//	// numChunks = [10, 10]
//	// total = 10 * 10 = 100
func (cc *ChunkCoordinator) GetTotalChunks() uint64 {
	total := uint64(1)
	for _, n := range cc.numChunks {
		total *= n
	}
	return total
}

// GetChunkCoordinate converts linear index to N-D coordinate.
//
// Uses row-major layout to convert a linear chunk index to
// its N-dimensional coordinate.
//
// Row-major layout means:
// - Rightmost dimension varies fastest
// - Leftmost dimension varies slowest
//
// Parameters:
//   - index: Linear chunk index (0 to GetTotalChunks()-1)
//
// Returns:
//   - []uint64: N-dimensional chunk coordinate
//
// Example (2D, 3x4 chunks):
//
//	index=0  → [0,0]
//	index=1  → [0,1]
//	index=3  → [0,3]
//	index=4  → [1,0]
//	index=11 → [2,3]
//
// Algorithm:
//
//	coord[N-1] = index % numChunks[N-1]
//	coord[N-2] = (index / numChunks[N-1]) % numChunks[N-2]
//	...
//	coord[0] = index / (numChunks[1] * numChunks[2] * ... * numChunks[N-1])
func (cc *ChunkCoordinator) GetChunkCoordinate(index uint64) []uint64 {
	coord := make([]uint64, len(cc.datasetDims))
	remaining := index

	// Convert from linear index to N-D coordinate (row-major)
	// Process from rightmost (fastest-varying) to leftmost (slowest-varying)
	for i := len(cc.numChunks) - 1; i >= 0; i-- {
		coord[i] = remaining % cc.numChunks[i]
		remaining /= cc.numChunks[i]
	}

	return coord
}

// GetChunkSize returns actual chunk size (may be partial).
//
// Edge chunks at dataset boundaries may be smaller than
// the nominal chunk size. This method calculates the actual
// size of a chunk given its coordinate.
//
// Parameters:
//   - coord: Chunk coordinate [dim0, dim1, ..., dimN]
//
// Returns:
//   - []uint64: Actual chunk size in each dimension
//
// Example (dataset 25x35, chunks 10x10):
//
//	[0,0] → [10,10] (full chunk)
//	[0,3] → [10,5]  (partial in dim 1)
//	[2,0] → [5,10]  (partial in dim 0)
//	[2,3] → [5,5]   (partial in both)
//
// Algorithm:
//
//	start[i] = coord[i] * chunkDims[i]
//	end[i] = min(start[i] + chunkDims[i], datasetDims[i])
//	size[i] = end[i] - start[i]
func (cc *ChunkCoordinator) GetChunkSize(coord []uint64) []uint64 {
	size := make([]uint64, len(coord))

	for i := range coord {
		// Calculate start and end of chunk in this dimension
		start := coord[i] * cc.chunkDims[i]
		end := start + cc.chunkDims[i]

		// Clip to dataset boundary
		if end > cc.datasetDims[i] {
			end = cc.datasetDims[i]
		}

		size[i] = end - start
	}

	return size
}

// ExtractChunkData extracts chunk data from full dataset.
//
// Extracts the data for a specific chunk from the full dataset buffer.
// The dataset is laid out in row-major order (C order), and the chunk
// data is extracted maintaining this layout.
//
// Parameters:
//   - data: Full dataset buffer (row-major layout)
//   - coord: Chunk coordinate to extract
//   - elemSize: Size of each element in bytes
//
// Returns:
//   - []byte: Extracted chunk data (contiguous buffer)
//
// Example (2D, dataset 20x30 uint32, chunks 10x10):
//
//	chunk [0,0]: extract data[0:10, 0:10]
//	chunk [0,1]: extract data[0:10, 10:20]
//	chunk [1,0]: extract data[10:20, 0:10]
//
// Algorithm:
//
//	For each element in chunk:
//	  1. Calculate position in dataset coordinates
//	  2. Calculate linear offset in dataset buffer
//	  3. Copy element to chunk buffer
func (cc *ChunkCoordinator) ExtractChunkData(data []byte, coord []uint64, elemSize uint32) []byte {
	chunkSize := cc.GetChunkSize(coord)

	// Calculate total number of elements in chunk
	numElements := uint64(1)
	for _, dim := range chunkSize {
		numElements *= dim
	}

	// Allocate buffer for chunk data
	chunkData := make([]byte, numElements*uint64(elemSize))

	// Extract data recursively
	cc.extractRecursive(data, chunkData, coord, 0, 0, 0, elemSize)

	return chunkData
}

// extractRecursive extracts chunk data recursively for each dimension.
//
// This recursive method traverses the dataset and chunk buffers dimension
// by dimension, copying elements from the dataset to the chunk buffer.
//
// Parameters:
//   - src: Source dataset buffer
//   - dst: Destination chunk buffer
//   - coord: Chunk coordinate
//   - dim: Current dimension being processed
//   - srcOff: Current offset in source buffer
//   - dstOff: Current offset in destination buffer
//   - elemSize: Size of each element in bytes
//
// Base case: dim == len(datasetDims) → copy single element
// Recursive case: iterate over chunk size in current dimension.
func (cc *ChunkCoordinator) extractRecursive(src, dst []byte, coord []uint64, dim int, srcOff, dstOff uint64, elemSize uint32) {
	// Base case: reached innermost dimension, copy element
	if dim == len(cc.datasetDims) {
		copy(dst[dstOff:dstOff+uint64(elemSize)], src[srcOff:srcOff+uint64(elemSize)])
		return
	}

	// Get actual chunk size in this dimension (may be partial)
	chunkSize := cc.GetChunkSize(coord)

	// Calculate strides for dataset and chunk
	// Dataset stride: number of bytes to skip to move one step in this dimension
	dsStride := uint64(1)
	for i := dim + 1; i < len(cc.datasetDims); i++ {
		dsStride *= cc.datasetDims[i]
	}
	dsStride *= uint64(elemSize)

	// Chunk stride: number of bytes to skip in chunk buffer
	chunkStride := uint64(1)
	for i := dim + 1; i < len(chunkSize); i++ {
		chunkStride *= chunkSize[i]
	}
	chunkStride *= uint64(elemSize)

	// Calculate starting position in dataset for this chunk
	start := coord[dim] * cc.chunkDims[dim]

	// Iterate over chunk size in this dimension
	for i := uint64(0); i < chunkSize[dim]; i++ {
		newSrc := srcOff + (start+i)*dsStride
		newDst := dstOff + i*chunkStride
		cc.extractRecursive(src, dst, coord, dim+1, newSrc, newDst, elemSize)
	}
}

// DatasetDims returns dataset dimensions (read-only copy).
func (cc *ChunkCoordinator) DatasetDims() []uint64 {
	dims := make([]uint64, len(cc.datasetDims))
	copy(dims, cc.datasetDims)
	return dims
}

// ChunkDims returns chunk dimensions (read-only copy).
func (cc *ChunkCoordinator) ChunkDims() []uint64 {
	dims := make([]uint64, len(cc.chunkDims))
	copy(dims, cc.chunkDims)
	return dims
}

// NumChunks returns number of chunks per dimension (read-only copy).
func (cc *ChunkCoordinator) NumChunks() []uint64 {
	nums := make([]uint64, len(cc.numChunks))
	copy(nums, cc.numChunks)
	return nums
}
