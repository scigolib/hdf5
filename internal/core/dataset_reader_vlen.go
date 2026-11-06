package core

import (
	"bytes"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// ReadDatasetVLenBytes reads a variable-length dataset and returns values as [][]byte.
// Each element in the outer slice corresponds to one dataset element; each inner slice
// contains the raw bytes of that variable-length sequence. It builds the
// dataset's descriptor and applies the default read limit; callers that hold a
// descriptor use ReadVLenBytes.
//
// This works for any VLen datatype (sequences and strings). For VLen strings the
// returned bytes are the raw UTF-8/ASCII characters without a null terminator.
//
// C Reference: H5Tvlen.c, H5HG.c (global heap object retrieval).
func ReadDatasetVLenBytes(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([][]byte, error) {
	desc, err := NewDatasetDescriptor(r, header, sb)
	if err != nil {
		return nil, err
	}

	return ReadVLenBytes(r, sb, desc, utils.DefaultReadLimit)
}

// ReadVLenBytes reads every element of a variable-length dataset and
// dereferences its global heap references.
func ReadVLenBytes(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, limit uint64) ([][]byte, error) {
	datatype := desc.datatype

	// Verify it's a variable-length type.
	if datatype.Class != DatatypeVarLen {
		return nil, fmt.Errorf("datatype is not variable-length: class=%d", datatype.Class)
	}

	totalElements := desc.nelmts
	if totalElements == 0 {
		return [][]byte{}, nil
	}

	// Each VLen element is stored as a global heap reference:
	//   heap_address (OffsetSize bytes) + object_index (4 bytes) + padding to 16 bytes.
	// datatype.Size is 16 (heap ID size).
	rawData, err := readRawData(r, sb, desc, ResultBytes, limit)
	if err != nil {
		return nil, err
	}

	// Dereference global heap IDs to retrieve actual data.
	offsetSize := int(sb.OffsetSize)
	heapIDSize := uint64(datatype.Size) // Typically 16 bytes.

	// On disk a variable-length element is a global heap reference: a 4-byte
	// sequence length, an offsetSize-byte heap address and a 4-byte object
	// index (H5Tvlen.c:300; C stores sizeof(hvl_t) = 16 for 8-byte offsets).
	// The size comes from the file, and a value below the reference makes the
	// slice below run backwards — one corrupted byte panicked the caller.
	refSize := uint64(sb.OffsetSize) + 8
	if heapIDSize < refSize {
		return nil, fmt.Errorf("vlen element size %d is smaller than a heap reference (%d bytes)",
			heapIDSize, refSize)
	}

	// The count is metadata-derived; validate it against the bytes read before
	// sizing the result slice (ADR-003, same class as parseCompoundData).
	if err := validateElementStorage(totalElements, heapIDSize, rawData, "vlen data"); err != nil {
		return nil, err
	}

	result := make([][]byte, totalElements)

	heap := newHeapReader(r, sb, limit)

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

		// The collection is cached and the bytes handed out are counted
		// against the read limit (heapReader).
		obj, err := heap.object(heapRef.HeapAddress, heapRef.ObjectIndex)
		if err != nil {
			return nil, fmt.Errorf("heap object %d at 0x%X for element %d: %w",
				heapRef.ObjectIndex, heapRef.HeapAddress, i, err)
		}

		// Copy the data to avoid holding references to the heap collection buffer.
		result[i] = bytes.Clone(obj)
	}

	return result, nil
}
