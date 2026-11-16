package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"

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
func (fw *FileWriter) createChunkedDataset(name string, dtype Datatype, dims []uint64, config *datasetConfig) (*DatasetWriter, error) {
	// 1. Validate chunk dimensions
	if err := validateChunkDims(dims, config.chunkDims, config.maxDims); err != nil {
		return nil, err
	}

	// 2. Get datatype info
	dtInfo, err := getDatatypeInfo(dtype, config)
	if err != nil {
		return nil, fmt.Errorf("invalid datatype: %w", err)
	}

	// 3. Encode datatype message
	handler := datatypeRegistry[dtype]
	datatypeData, err := handler.EncodeDatatypeMessage(dtInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to encode datatype: %w", err)
	}

	// 4. The datatype Write encodes values with
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

	return fw.createChunkedDatasetEncoded(name, dims, config, datatypeData, dtInfo.size, dsMsgForWriter)
}

// validateChunkDims checks that chunkDims has one non-zero extent per
// dataset dimension and, as H5D__chunk_construct does (H5Dchunk.c:1073-1081),
// that no chunk extent exceeds the maximum size of a fixed-size dimension.
// Without maxDims every dimension is fixed at its current size (as with
// H5Screate_simple and a NULL maxdims); a dimension whose maximum is
// Unlimited, or larger than the current size, may have a chunk larger than
// its current extent, so a growing table can start with one row in a large
// chunk.
func validateChunkDims(dims, chunkDims, maxDims []uint64) error {
	if len(chunkDims) != len(dims) {
		return fmt.Errorf("chunk dimensions (%d) must match dataset dimensions (%d)",
			len(chunkDims), len(dims))
	}

	for i, chunkDim := range chunkDims {
		if chunkDim == 0 {
			return fmt.Errorf("chunk dimension %d cannot be zero", i)
		}
		if len(maxDims) == 0 {
			if chunkDim > dims[i] {
				return fmt.Errorf("chunk dimension %d (%d) cannot exceed dataset dimension (%d)",
					i, chunkDim, dims[i])
			}
			continue
		}
		if maxDims[i] != Unlimited && chunkDim > maxDims[i] {
			return fmt.Errorf("chunk dimension %d (%d) cannot exceed maximum dimension (%d) of a fixed-size dimension",
				i, chunkDim, maxDims[i])
		}
	}
	return nil
}

// maxV1BTreeChunkSize is the largest chunk, in bytes, that a v1 B-tree chunk
// index can record: its chunk size field is 32 bits (H5Dchunk.c:1197-1199).
const maxV1BTreeChunkSize = math.MaxUint32

// chunkedDatasetByteSize returns the data size of a chunked dataset, after
// checking that the sizes of the data and of one chunk fit: a chunk must be
// below 4 GiB.
func chunkedDatasetByteSize(dims, chunkDims []uint64, elemSize uint32) (uint64, error) {
	dataSize, err := datasetByteSize(dims, elemSize)
	if err != nil {
		return 0, err
	}
	chunkSize, err := datasetByteSize(chunkDims, elemSize)
	if err != nil {
		return 0, fmt.Errorf("chunk dimensions: %w", err)
	}
	if chunkSize > maxV1BTreeChunkSize {
		// H5D__chunk_init: "chunk size must be < 4GB with v1 b-tree index"
		// (H5Dchunk.c:1197-1199). A larger chunk needs the layout version 5
		// indexes of HDF5 2.0, which this writer does not produce.
		return 0, fmt.Errorf("chunk size must be < 4GB with v1 b-tree index: chunk %v of %d-byte elements is %d bytes",
			chunkDims, elemSize, chunkSize)
	}
	return dataSize, nil
}

// createChunkedDatasetEncoded creates a chunked dataset whose datatype message
// is already encoded: datatypeData is the message, elemSize the size of one
// element, and writerDtype the datatype DatasetWriter encodes values with.
// The chunk dimensions must have been checked with validateChunkDims.
func (fw *FileWriter) createChunkedDatasetEncoded(name string, dims []uint64, config *datasetConfig,
	datatypeData []byte, elemSize uint32, writerDtype *core.DatatypeMessage) (*DatasetWriter, error) {
	// Sizes first, before anything is allocated or written.
	dataSize, err := chunkedDatasetByteSize(dims, config.chunkDims, elemSize)
	if err != nil {
		return nil, err
	}

	// The parent must exist and must not hold the name yet, checked before
	// anything is allocated (link_create.go).
	parent, datasetName := parsePath(name)
	if err := fw.checkLinkNameFree(parent, datasetName); err != nil {
		return nil, err
	}

	// Create chunk coordinator
	// An empty extendible dataset has no chunks to coordinate until it grows.
	var chunkCoordinator *writer.ChunkCoordinator
	if !hasZeroDim(dims) {
		chunkCoordinator, err = writer.NewChunkCoordinator(dims, config.chunkDims)
		if err != nil {
			return nil, fmt.Errorf("failed to create chunk coordinator: %w", err)
		}
	}

	// No chunk is written yet, so there is no chunk index: its address is
	// undefined until Write builds the B-tree, as C leaves it HADDR_UNDEF
	// until the first chunk is allocated: no index space while the address is
	// undefined (H5D__btree_idx_is_space_alloc, H5Dbtree.c:950-958).
	btreeAddress := undefinedAddress

	// 6. Create dataspace message
	dataspaceData, err := fw.encodeDatasetDataspace(dims, config.maxDims)
	if err != nil {
		return nil, fmt.Errorf("failed to encode dataspace: %w", err)
	}

	// 7. Create chunked layout message
	// Per C reference (H5Dchunk.c:909-913), layout stores ndims+1 dimensions
	// where the last dimension is the datatype element size.
	layoutData, err := core.EncodeLayoutMessage(
		core.LayoutChunked,
		0,            // dataSize not used for chunked
		btreeAddress, // chunk index address: undefined until Write builds it
		fw.file.sb,
		config.chunkDims,
		elemSize, // element size for trailing dimension
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encode chunked layout: %w", err)
	}

	// 8. Setup filter pipeline if configured
	config.setupShuffle(elemSize)

	// 9. Create object header with optional filter pipeline
	messages, err := fw.datasetHeaderMessages(datatypeData, dataspaceData, layoutData, core.LayoutChunked, config.pipeline)
	if err != nil {
		return nil, err
	}
	ohw := &core.ObjectHeaderWriter{
		Version:  2,
		Flags:    0, // Minimal flags
		Messages: messages,
	}

	// Pre-allocate OHDR with padding for future attributes.
	ohw.PadToSize(core.MinOHDRAllocSize)

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
	fw.headerAllocs[headerAddress] = headerSize

	writtenSize, err := ohw.WriteTo(fw.writer, headerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	if writtenSize != headerSize {
		return nil, fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	// 9. Link to parent group; a dataset that could not be linked is deleted
	// again.
	if err := fw.linkToParent(parent, datasetName, headerAddress); err != nil {
		return nil, fw.discardUnlinkedObject(headerAddress, fmt.Errorf("failed to link dataset: %w", err))
	}

	// 10. Create DatasetWriter
	return &DatasetWriter{
		fileWriter:       fw,
		name:             name,
		address:          headerAddress,
		dataAddress:      btreeAddress, // Will be updated on Write()
		dataSize:         dataSize,
		elementSize:      elemSize,
		dtype:            writerDtype,
		dims:             dims,
		maxDims:          config.maxDims, // Maximum dimensions for resize support
		isChunked:        true,
		chunkCoordinator: chunkCoordinator,
		chunkDims:        config.chunkDims,
		pipeline:         config.pipeline, // Filter pipeline
	}, nil
}

// setupShuffle puts the shuffle filter for elements of elemSize bytes at the
// start of the filter pipeline when it was requested, creating the pipeline
// if no other filter made one.
func (config *datasetConfig) setupShuffle(elemSize uint32) {
	if !config.enableShuffle {
		return
	}
	if config.pipeline == nil {
		config.pipeline = writer.NewFilterPipeline()
	}
	config.pipeline.AddFilterAtStart(writer.NewShuffleFilter(elemSize))
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
	if dw.appender != nil {
		return errAppending
	}
	if dw.chunkCoordinator == nil {
		return fmt.Errorf("dataset %q is empty: grow it with AppendRows", dw.name)
	}

	if uint64(len(buf)) != dw.dataSize {
		return fmt.Errorf("data size mismatch: expected %d bytes, got %d", dw.dataSize, len(buf))
	}

	// The on-disk element size: for an array datatype the whole array
	// element, not the base type that dtype describes.
	elemSize := dw.elementSize

	// 1. Create B-tree writer
	// Per C reference (H5Dbtree.c:687-690), B-tree keys store byte offsets,
	// so the writer needs chunk dimensions for the conversion.
	dimensionality := len(dw.dims)
	btreeWriter := structures.NewChunkBTreeWriter(dimensionality, dw.chunkDims, elemSize)

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

		// Add to B-tree index with chunk size. The raw chunk is below 4 GiB
		// (checked at create time), but a filter may enlarge it: the v1
		// B-tree records the stored size in 32 bits (H5Dchunk.c:1197-1199).
		if uint64(len(chunkData)) > maxV1BTreeChunkSize {
			return fmt.Errorf("chunk %v: filtered size %d bytes must be < 4GB with v1 b-tree index", coord, len(chunkData))
		}
		//nolint:gosec // G115: checked against maxV1BTreeChunkSize (MaxUint32) above
		if err := btreeWriter.AddChunkWithSize(coord, chunkAddr, uint32(len(chunkData))); err != nil {
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

	// 5. Record the B-tree address in the layout message, so that the file
	// can be read after closing.
	return dw.setChunkIndexAddress(btreeAddr)
}

// setChunkIndexAddress stores addr as the chunk index address of the
// dataset's layout message. The header is read, changed in its message and
// written back whole, so the checksum covers the header as it is now, after
// attribute calls may have changed its size and message offsets.
func (dw *DatasetWriter) setChunkIndexAddress(addr uint64) error {
	fw := dw.fileWriter
	sb := fw.file.sb
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), dw.address, sb)
	if err != nil {
		return fmt.Errorf("read object header: %w", err)
	}

	for _, msg := range oh.Messages {
		if msg.Type != core.MsgDataLayout {
			continue
		}
		// A version 3 chunked layout holds version, class and dimensionality,
		// then the address (H5Olayout.c:637-645).
		const addrOffset = 3
		end := addrOffset + int(sb.OffsetSize)
		if len(msg.Data) < end || msg.Data[0] != 3 || msg.Data[1] != byte(core.LayoutChunked) {
			return fmt.Errorf("dataset %q: layout message is not a version 3 chunked layout", dw.name)
		}
		switch sb.OffsetSize {
		case 8:
			binary.LittleEndian.PutUint64(msg.Data[addrOffset:end], addr)
		case 4:
			binary.LittleEndian.PutUint32(msg.Data[addrOffset:end], uint32(addr)) //nolint:gosec // G115: the allocator stays below 4 GiB for 4-byte offsets
		default:
			return fmt.Errorf("unsupported offset size: %d", sb.OffsetSize)
		}
		return fw.rewriteObjectHeader(dw.address, oh)
	}

	return fmt.Errorf("dataset %q: object header has no layout message", dw.name)
}

// hasZeroDim reports whether any dimension of dims is 0.
func hasZeroDim(dims []uint64) bool {
	for _, d := range dims {
		if d == 0 {
			return true
		}
	}
	return false
}
