package core

import (
	"errors"
	"fmt"
	"io"
)

// ReadDatasetVLenBytes reads a variable-length dataset and returns values as [][]byte.
// Each element in the outer slice corresponds to one dataset element; each inner slice
// contains the raw bytes of that variable-length sequence.
//
// This works for any VLen datatype (sequences and strings). For VLen strings the
// returned bytes are the raw UTF-8/ASCII characters without a null terminator.
//
// C Reference: H5Tvlen.c, H5HG.c (global heap object retrieval).
func ReadDatasetVLenBytes(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([][]byte, error) {
	// 1. Extract required messages from object header.
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

	// Verify it's a variable-length type.
	if datatype.Class != DatatypeVarLen {
		return nil, fmt.Errorf("datatype is not variable-length: class=%d", datatype.Class)
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

	// 5. Calculate total number of elements.
	totalElements := dataspace.TotalElements()
	if totalElements == 0 {
		return [][]byte{}, nil
	}

	// 6. Read raw data (heap IDs) based on layout type.
	// Each VLen element is stored as a global heap reference:
	//   heap_address (OffsetSize bytes) + object_index (4 bytes) + padding to 16 bytes.
	// datatype.Size is 16 (heap ID size).
	var rawData []byte

	switch {
	case layout.IsCompact():
		rawData = layout.CompactData

	case layout.IsContiguous():
		dataSize := totalElements * uint64(datatype.Size)
		rawData = make([]byte, dataSize)

		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		_, err := r.ReadAt(rawData, int64(layout.DataAddress))
		if err != nil {
			return nil, fmt.Errorf("failed to read contiguous data: %w", err)
		}

	case layout.IsChunked():
		// Extract filter pipeline if present.
		var filterPipeline *FilterPipelineMessage
		for _, msg := range header.Messages {
			if msg.Type == MsgFilterPipeline {
				filterPipeline, err = ParseFilterPipelineMessage(msg.Data)
				if err != nil {
					return nil, fmt.Errorf("failed to parse filter pipeline: %w", err)
				}
				break
			}
		}

		rawData, err = readChunkedData(r, layout, dataspace, datatype, sb, filterPipeline)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunked data: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", layout.Class)
	}

	// 7. Dereference global heap IDs to retrieve actual data.
	offsetSize := int(sb.OffsetSize)
	heapIDSize := uint64(datatype.Size) // Typically 16 bytes.
	result := make([][]byte, totalElements)

	// Cache global heap collections to avoid re-reading the same collection.
	heapCache := make(map[uint64]*GlobalHeapCollection)

	for i := uint64(0); i < totalElements; i++ {
		idStart := i * heapIDSize
		if idStart+heapIDSize > uint64(len(rawData)) {
			return nil, fmt.Errorf("heap ID %d extends beyond data", i)
		}

		// VLen on-disk format (C ref: H5Tvlen.c:300, H5Tvlen.c:728):
		//   Bytes 0-3:  seq_len (uint32 LE) — number of elements in sequence
		//   Bytes 4-11: heap_address (offset_size bytes)
		//   Bytes 12-15: object_index (4 bytes)
		// Skip the first 4 bytes (seq_len) to get to the global heap reference.
		heapRef, err := ParseGlobalHeapReference(rawData[idStart+4:idStart+heapIDSize], offsetSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse heap reference for element %d: %w", i, err)
		}

		// Null reference means empty/nil sequence.
		if heapRef.HeapAddress == 0 && heapRef.ObjectIndex == 0 {
			result[i] = []byte{}
			continue
		}

		// Look up or read the global heap collection.
		collection, ok := heapCache[heapRef.HeapAddress]
		if !ok {
			collection, err = ReadGlobalHeapCollection(r, heapRef.HeapAddress, offsetSize)
			if err != nil {
				return nil, fmt.Errorf("failed to read global heap at 0x%X for element %d: %w",
					heapRef.HeapAddress, i, err)
			}
			heapCache[heapRef.HeapAddress] = collection
		}

		obj, err := collection.GetObject(heapRef.ObjectIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get heap object %d for element %d: %w",
				heapRef.ObjectIndex, i, err)
		}

		// Copy the data to avoid holding references to the heap collection buffer.
		data := make([]byte, len(obj.Data))
		copy(data, obj.Data)
		result[i] = data
	}

	return result, nil
}
