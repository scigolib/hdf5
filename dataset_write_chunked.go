package hdf5

import (
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/writer"
)

// createChunkedDataset creates a dataset with chunked storage layout.
//
// Implementation steps:
// 1. Validate chunk dimensions
// 2. Get datatype info
// 3. Create chunk coordinator
// 4. Write empty B-tree (will be populated on Write())
// 5. Encode messages (datatype, dataspace, chunked layout)
// 6. Write object header
// 7. Add link to group
//
// For MVP (Phase 1):
// - No compression (filter pipeline empty)
// - B-tree v1 for chunk indexing
// - Single-level B-tree (no splits).
//
//nolint:gocognit,gocyclo,cyclop,funlen // Complex by nature: chunked dataset creation involves many steps
func (fw *FileWriter) createChunkedDataset(name string, dtype Datatype, dims []uint64, config *datasetConfig) (*DatasetWriter, error) {
	// 1. Validate chunk dimensions
	if len(config.chunkDims) != len(dims) {
		return nil, fmt.Errorf("chunk dimensions (%d) must match dataset dimensions (%d)",
			len(config.chunkDims), len(dims))
	}

	for i, chunkDim := range config.chunkDims {
		if chunkDim == 0 {
			return nil, fmt.Errorf("chunk dimension %d cannot be zero", i)
		}
		if chunkDim > dims[i] {
			return nil, fmt.Errorf("chunk dimension %d (%d) cannot exceed dataset dimension (%d)",
				i, chunkDim, dims[i])
		}
	}

	// 2. Get datatype info
	dtInfo, err := getDatatypeInfo(dtype, config)
	if err != nil {
		return nil, fmt.Errorf("invalid datatype: %w", err)
	}

	// 3. Create chunk coordinator
	chunkCoordinator, err := writer.NewChunkCoordinator(dims, config.chunkDims)
	if err != nil {
		return nil, fmt.Errorf("failed to create chunk coordinator: %w", err)
	}

	// 4. B-tree address will be 0 initially (written during Write())
	// This is standard HDF5 practice for empty chunked datasets
	btreeAddress := uint64(0)

	// 5. Encode datatype message
	handler := datatypeRegistry[dtype]
	datatypeData, err := handler.EncodeDatatypeMessage(dtInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to encode datatype: %w", err)
	}

	// 6. Create dataspace message
	dataspaceData, err := core.EncodeDataspaceMessage(dims, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encode dataspace: %w", err)
	}

	// 7. Create chunked layout message
	layoutData, err := core.EncodeLayoutMessage(
		core.LayoutChunked,
		0,            // dataSize not used for chunked
		btreeAddress, // B-tree address (0 for now)
		fw.file.sb,
		config.chunkDims,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encode chunked layout: %w", err)
	}

	// 8. Setup filter pipeline if configured
	if config.pipeline != nil || config.enableShuffle {
		// Create pipeline if needed
		if config.pipeline == nil {
			config.pipeline = writer.NewFilterPipeline()
		}

		// Add shuffle filter at beginning if requested
		if config.enableShuffle {
			// Element size from datatype
			shuffleFilter := writer.NewShuffleFilter(dtInfo.size)
			config.pipeline.AddFilterAtStart(shuffleFilter)
		}
	}

	// 9. Create object header with optional filter pipeline
	ohw := &core.ObjectHeaderWriter{
		Version: 2,
		Flags:   0, // Minimal flags
		Messages: []core.MessageWriter{
			{Type: core.MsgDatatype, Data: datatypeData},
			{Type: core.MsgDataspace, Data: dataspaceData},
			{Type: core.MsgDataLayout, Data: layoutData},
		},
	}

	// Add filter pipeline message if present
	if config.pipeline != nil && !config.pipeline.IsEmpty() {
		pipelineData, err := config.pipeline.EncodePipelineMessage()
		if err != nil {
			return nil, fmt.Errorf("failed to encode filter pipeline: %w", err)
		}

		ohw.Messages = append(ohw.Messages, core.MessageWriter{
			Type: core.MsgFilterPipeline,
			Data: pipelineData,
		})
	}

	// Calculate header size
	headerSize, err := calculateObjectHeaderSize(ohw)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate header size: %w", err)
	}

	// Allocate and write header
	headerAddress, err := fw.writer.Allocate(headerSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate header: %w", err)
	}

	writtenSize, err := ohw.WriteTo(fw.writer, headerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	if writtenSize != headerSize {
		return nil, fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	// 9. Link to parent group
	parent, datasetName := parsePath(name)
	if err := fw.linkToParent(parent, datasetName, headerAddress); err != nil {
		return nil, fmt.Errorf("failed to link dataset: %w", err)
	}

	// 10. Create DatasetWriter
	var dsMsgForWriter *core.DatatypeMessage
	if dtInfo.baseType != nil {
		// For array/enum, use base type for data writing
		dsMsgForWriter = &core.DatatypeMessage{
			Class:   dtInfo.baseType.class,
			Version: 1,
			Size:    dtInfo.baseType.size,
		}
	} else {
		// For simple types, use the datatype itself
		dsMsgForWriter = &core.DatatypeMessage{
			Class:   dtInfo.class,
			Version: 1,
			Size:    dtInfo.size,
		}
	}

	totalElements := calculateTotalElements(dims)
	dataSize := totalElements * uint64(dtInfo.size)

	return &DatasetWriter{
		fileWriter:       fw,
		name:             name,
		address:          headerAddress,
		dataAddress:      btreeAddress, // Will be updated on Write()
		dataSize:         dataSize,
		dtype:            dsMsgForWriter,
		dims:             dims,
		isChunked:        true,
		chunkCoordinator: chunkCoordinator,
		chunkDims:        config.chunkDims,
		pipeline:         config.pipeline, // Filter pipeline
	}, nil
}

// writeChunkedData writes data to chunked dataset.
//
// Implementation steps:
// 1. Extract chunks using ChunkCoordinator
// 2. Write each chunk to file
// 3. Build B-tree index
// 4. Write B-tree to file
// 5. Update object header with B-tree address
//
// For MVP (Phase 1):
// - All chunks written at once (no partial writes)
// - No compression
// - Simple B-tree v1.
func (dw *DatasetWriter) writeChunkedData(buf []byte) error {
	if !dw.isChunked {
		return fmt.Errorf("writeChunkedData called on non-chunked dataset")
	}

	if uint64(len(buf)) != dw.dataSize {
		return fmt.Errorf("data size mismatch: expected %d bytes, got %d", dw.dataSize, len(buf))
	}

	elemSize := dw.dtype.Size

	// 1. Create B-tree writer
	dimensionality := len(dw.dims)
	btreeWriter := structures.NewChunkBTreeWriter(dimensionality)

	// 2. Process each chunk
	totalChunks := dw.chunkCoordinator.GetTotalChunks()

	for i := uint64(0); i < totalChunks; i++ {
		// Get chunk coordinate
		coord := dw.chunkCoordinator.GetChunkCoordinate(i)

		// Extract chunk data
		chunkData := dw.chunkCoordinator.ExtractChunkData(buf, coord, elemSize)

		// Apply filters to chunk (if pipeline configured)
		if dw.pipeline != nil && !dw.pipeline.IsEmpty() {
			filtered, err := dw.pipeline.Apply(chunkData)
			if err != nil {
				return fmt.Errorf("filter application failed for chunk %v: %w", coord, err)
			}
			chunkData = filtered
		}

		// Allocate space for chunk (filtered size may differ from original)
		chunkAddr, err := dw.fileWriter.writer.Allocate(uint64(len(chunkData)))
		if err != nil {
			return fmt.Errorf("failed to allocate chunk %v: %w", coord, err)
		}

		// Write chunk data (filtered)
		if err := dw.fileWriter.writer.WriteAtAddress(chunkData, chunkAddr); err != nil {
			return fmt.Errorf("failed to write chunk %v: %w", coord, err)
		}

		// Add to B-tree index
		if err := btreeWriter.AddChunk(coord, chunkAddr); err != nil {
			return fmt.Errorf("failed to add chunk %v to index: %w", coord, err)
		}
	}

	// 3. Write B-tree
	btreeAddr, err := btreeWriter.WriteToFile(dw.fileWriter.writer, dw.fileWriter.writer.Allocator())
	if err != nil {
		return fmt.Errorf("failed to write B-tree: %w", err)
	}

	// 4. Store B-tree address
	dw.dataAddress = btreeAddr

	// Note: For MVP, we accept that the layout message in the object header
	// will have address 0, and the actual B-tree address is only known after
	// Write() is called. This is acceptable for Phase 1.
	// In a future version, we could re-write the layout message with the correct address.

	return nil
}
