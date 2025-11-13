package core

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// ReadDatasetFloat64 reads a dataset and returns values as float64 array.
// This is the main entry point for reading numerical datasets.
func ReadDatasetFloat64(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]float64, error) {
	// 1. Extract required messages from object header.
	var datatypeMsg, dataspaceMsg, layoutMsg, filterPipelineMsg *HeaderMessage

	for _, msg := range header.Messages {
		switch msg.Type {
		case MsgDatatype:
			datatypeMsg = msg
		case MsgDataspace:
			dataspaceMsg = msg
		case MsgDataLayout:
			layoutMsg = msg
		case MsgFilterPipeline:
			filterPipelineMsg = msg
		}
	}

	// Validate we have all required messages.
	if datatypeMsg == nil {
		return nil, errors.New("datatype message not found")
	}
	if dataspaceMsg == nil {
		return nil, errors.New("dataspace message not found")
	}
	if layoutMsg == nil {
		return nil, errors.New("data layout message not found")
	}

	// 2. Parse datatype.
	datatype, err := ParseDatatypeMessage(datatypeMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse datatype: %w", err)
	}

	// 3. Parse dataspace.
	dataspace, err := ParseDataspaceMessage(dataspaceMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	// 4. Parse layout.
	layout, err := ParseDataLayoutMessage(layoutMsg.Data, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout: %w", err)
	}

	// 5. Parse filter pipeline (optional, for compression).
	var filterPipeline *FilterPipelineMessage
	if filterPipelineMsg != nil {
		filterPipeline, err = ParseFilterPipelineMessage(filterPipelineMsg.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filter pipeline: %w", err)
		}
	}

	// 6. Calculate total number of elements.
	totalElements := dataspace.TotalElements()
	if totalElements == 0 {
		return []float64{}, nil
	}

	// 6. Read data based on layout type.
	var rawData []byte

	switch {
	case layout.IsCompact():
		// Data is stored directly in the layout message.
		rawData = layout.CompactData

	case layout.IsContiguous():
		// Data is stored contiguously at specific address.
		dataSize := totalElements * uint64(datatype.Size)
		rawData = make([]byte, dataSize)

		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		_, err := r.ReadAt(rawData, int64(layout.DataAddress))
		if err != nil {
			return nil, fmt.Errorf("failed to read contiguous data: %w", err)
		}

	case layout.IsChunked():
		// Data is stored in chunks indexed by B-tree.
		rawData, err = readChunkedData(r, layout, dataspace, datatype, sb, filterPipeline)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunked data: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", layout.Class)
	}

	// 7. Convert raw bytes to float64 based on datatype.
	return convertToFloat64(rawData, datatype, totalElements)
}

// convertToFloat64 converts raw bytes to float64 array based on datatype.
func convertToFloat64(rawData []byte, datatype *DatatypeMessage, numElements uint64) ([]float64, error) {
	result := make([]float64, numElements)
	byteOrder := datatype.GetByteOrder()

	switch {
	case datatype.IsFloat64():
		// IEEE 754 double precision (64-bit).
		for i := uint64(0); i < numElements; i++ {
			offset := i * 8
			if offset+8 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (float64)")
			}

			bits := byteOrder.Uint64(rawData[offset : offset+8])
			result[i] = math.Float64frombits(bits)
		}

	case datatype.IsFloat32():
		// IEEE 754 single precision (32-bit).
		for i := uint64(0); i < numElements; i++ {
			offset := i * 4
			if offset+4 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (float32)")
			}

			bits := byteOrder.Uint32(rawData[offset : offset+4])
			result[i] = float64(math.Float32frombits(bits))
		}

	case datatype.IsInt32():
		// 32-bit signed integer.
		for i := uint64(0); i < numElements; i++ {
			offset := i * 4
			if offset+4 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (int32)")
			}

			//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
			val := int32(byteOrder.Uint32(rawData[offset : offset+4]))
			result[i] = float64(val)
		}

	case datatype.IsInt64():
		// 64-bit signed integer.
		for i := uint64(0); i < numElements; i++ {
			offset := i * 8
			if offset+8 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (int64)")
			}

			//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
			val := int64(byteOrder.Uint64(rawData[offset : offset+8]))
			result[i] = float64(val)
		}

	default:
		return nil, fmt.Errorf("unsupported datatype for conversion to float64: %s", datatype)
	}

	return result, nil
}

// ReadDatasetInfo returns dataset metadata without reading actual data.
func ReadDatasetInfo(header *ObjectHeader, sb *Superblock) (*DatasetInfo, error) {
	var datatypeMsg, dataspaceMsg, layoutMsg *HeaderMessage

	for _, msg := range header.Messages {
		switch msg.Type {
		case MsgDatatype:
			datatypeMsg = msg
		case MsgDataspace:
			dataspaceMsg = msg
		case MsgDataLayout:
			layoutMsg = msg
		}
	}

	if datatypeMsg == nil || dataspaceMsg == nil || layoutMsg == nil {
		return nil, errors.New("missing required messages")
	}

	datatype, err := ParseDatatypeMessage(datatypeMsg.Data)
	if err != nil {
		return nil, err
	}

	dataspace, err := ParseDataspaceMessage(dataspaceMsg.Data)
	if err != nil {
		return nil, err
	}

	layout, err := ParseDataLayoutMessage(layoutMsg.Data, sb)
	if err != nil {
		return nil, err
	}

	return &DatasetInfo{
		Datatype:  datatype,
		Dataspace: dataspace,
		Layout:    layout,
	}, nil
}

// DatasetInfo holds metadata about a dataset.
type DatasetInfo struct {
	Datatype  *DatatypeMessage
	Dataspace *DataspaceMessage
	Layout    *DataLayoutMessage
}

// String returns human-readable dataset info.
func (di *DatasetInfo) String() string {
	return fmt.Sprintf(
		"Dataset: %s, %s, %s",
		di.Datatype.String(),
		di.Dataspace.String(),
		di.Layout.String(),
	)
}

// readChunkedData reads data from chunked layout.
func readChunkedData(r io.ReaderAt, layout *DataLayoutMessage, dataspace *DataspaceMessage, datatype *DatatypeMessage, sb *Superblock, filterPipeline *FilterPipelineMessage) ([]byte, error) {
	// Parse B-tree to get chunk index.
	// Note: chunk dimensions may include an extra dimension for datatype size.
	// (HDF5 stores "fastest-varying dimension" as bytes, see H5Dbtree.c comments).
	ndims := len(layout.ChunkSize)
	btree, err := ParseBTreeV1Node(r, layout.DataAddress, sb.OffsetSize, ndims, layout.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to parse B-tree: %w", err)
	}

	// Calculate total data size.
	totalElements := dataspace.TotalElements()
	elementSize := uint64(datatype.Size)

	// CVE-2025-7067 fix: Check for overflow in total size calculation.
	totalBytes, err := utils.SafeMultiply(totalElements, elementSize)
	if err != nil {
		return nil, fmt.Errorf("dataset size overflow: %w", err)
	}

	// Validate total size is within reasonable limits.
	if err := utils.ValidateBufferSize(totalBytes, utils.MaxChunkSize*1024, "dataset"); err != nil {
		return nil, fmt.Errorf("dataset too large: %w", err)
	}

	// Allocate output buffer.
	rawData := make([]byte, totalBytes)

	// Collect all chunks from B-tree (handles both leaf and non-leaf nodes).
	chunks, err := btree.CollectAllChunks(r, sb.OffsetSize, layout.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to collect chunks: %w", err)
	}

	// Read each chunk and copy to correct position.
	for _, chunk := range chunks {
		chunkKey := chunk.Key
		chunkAddr := chunk.Address

		// CVE-2025-7067 fix: Validate chunk size before allocation to prevent buffer overflow.
		if err := utils.ValidateBufferSize(uint64(chunkKey.Nbytes), utils.MaxChunkSize, "chunk data"); err != nil {
			return nil, fmt.Errorf("invalid chunk size at 0x%x: %w", chunkAddr, err)
		}

		// Read chunk data.
		chunkData := make([]byte, chunkKey.Nbytes)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		_, err := r.ReadAt(chunkData, int64(chunkAddr))
		if err != nil {
			return nil, fmt.Errorf("failed to read chunk at 0x%x: %w", chunkAddr, err)
		}

		// Apply filters (decompression, etc) if present.
		if filterPipeline != nil {
			chunkData, err = filterPipeline.ApplyFilters(chunkData)
			if err != nil {
				return nil, fmt.Errorf("failed to apply filters to chunk at 0x%x: %w", chunkAddr, err)
			}
		}

		// Calculate where this chunk goes in the output array.
		// For N-dimensional dataset, chunk [i0, i1, ...] maps to elements:
		// [i0*chunk[0] : (i0+1)*chunk[0], i1*chunk[1] : (i1+1)*chunk[1], ...].

		// Trim chunk dimensions to match dataset dimensions.
		// (chunk may have extra dimension for datatype size).
		dataDims := dataspace.Dimensions
		actualChunkDims := layout.ChunkSize[:len(dataDims)]
		actualChunkCoords := chunkKey.Scaled[:len(dataDims)]

		err = copyChunkToArray(chunkData, rawData, actualChunkCoords, actualChunkDims, dataDims, elementSize)
		if err != nil {
			return nil, fmt.Errorf("failed to copy chunk %v: %w", actualChunkCoords, err)
		}
	}

	return rawData, nil
}

// copyChunkToArray copies chunk data to the correct position in full array.
// This handles multi-dimensional indexing and partial chunks at boundaries.
func copyChunkToArray(chunkData, fullData []byte, chunkCoords, chunkSize, dataDims []uint64, elemSize uint64) error {
	ndims := len(chunkCoords)
	if ndims != len(chunkSize) || ndims != len(dataDims) {
		return errors.New("dimension mismatch")
	}

	// Use general N-dimensional algorithm.
	return copyNDChunk(chunkData, fullData, chunkCoords, chunkSize, dataDims, elemSize)
}

// copyNDChunk copies an N-dimensional chunk to the full N-dimensional array.
// Uses general algorithm that works for any number of dimensions.
func copyNDChunk(chunkData, fullData []byte, chunkCoords, chunkSize, dataDims []uint64, elemSize uint64) error {
	ndims := len(chunkCoords)

	// Calculate strides for both chunk and full array.
	// Stride[i] = product of all dimensions after i.
	chunkStrides := make([]uint64, ndims)
	dataStrides := make([]uint64, ndims)

	chunkStrides[ndims-1] = 1
	dataStrides[ndims-1] = 1
	for i := ndims - 2; i >= 0; i-- {
		chunkStrides[i] = chunkStrides[i+1] * chunkSize[i+1]
		dataStrides[i] = dataStrides[i+1] * dataDims[i+1]
	}

	// Calculate actual dimensions to copy (may be less than chunk size at boundaries).
	copyDims := make([]uint64, ndims)
	for i := 0; i < ndims; i++ {
		// Starting position of this chunk in dataset.
		startPos := chunkCoords[i] * chunkSize[i]
		// Maximum elements we can copy in this dimension.
		maxCopy := chunkSize[i]
		if startPos+maxCopy > dataDims[i] {
			maxCopy = dataDims[i] - startPos
		}
		copyDims[i] = maxCopy
	}

	// Calculate starting offset in full array for this chunk.
	dataOffset := uint64(0)
	for i := 0; i < ndims; i++ {
		dataOffset += chunkCoords[i] * chunkSize[i] * dataStrides[i]
	}

	// Use recursive N-dimensional iteration to copy elements.
	indices := make([]uint64, ndims)
	return copyNDChunkRecursive(chunkData, fullData, indices, 0, copyDims, chunkStrides, dataStrides, dataOffset, elemSize)
}

// copyNDChunkRecursive recursively iterates through N-dimensional indices.
func copyNDChunkRecursive(chunkData, fullData []byte, indices []uint64, dim int, copyDims, chunkStrides, dataStrides []uint64, dataBaseOffset, elemSize uint64) error {
	ndims := len(indices)

	if dim == ndims-1 {
		// Base case: copy a contiguous row.
		numElements := copyDims[dim]

		// Calculate source offset in chunk.
		chunkOffset := uint64(0)
		for i := 0; i < ndims; i++ {
			chunkOffset += indices[i] * chunkStrides[i]
		}
		chunkOffset *= elemSize

		// Calculate destination offset in full array.
		dataOffset := dataBaseOffset
		for i := 0; i < ndims-1; i++ {
			dataOffset += indices[i] * dataStrides[i]
		}
		dataOffset *= elemSize

		numBytes := numElements * elemSize

		// Bounds check.
		if chunkOffset+numBytes > uint64(len(chunkData)) {
			return fmt.Errorf("chunk data truncated: need %d bytes at offset %d, have %d total",
				numBytes, chunkOffset, len(chunkData))
		}
		if dataOffset+numBytes > uint64(len(fullData)) {
			return fmt.Errorf("full data overflow: need %d bytes at offset %d, have %d total",
				numBytes, dataOffset, len(fullData))
		}

		// Copy the row.
		copy(fullData[dataOffset:dataOffset+numBytes], chunkData[chunkOffset:chunkOffset+numBytes])
		return nil
	}

	// Recursive case: iterate through this dimension.
	for indices[dim] = 0; indices[dim] < copyDims[dim]; indices[dim]++ {
		err := copyNDChunkRecursive(chunkData, fullData, indices, dim+1, copyDims, chunkStrides, dataStrides, dataBaseOffset, elemSize)
		if err != nil {
			return err
		}
	}

	return nil
}
