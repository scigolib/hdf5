package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// ReadDatasetStrings reads a string dataset and returns values as string array.
// It builds the dataset's descriptor and applies the default read limit;
// callers that hold a descriptor use ReadStrings.
func ReadDatasetStrings(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]string, error) {
	desc, err := NewDatasetDescriptor(r, header, sb)
	if err != nil {
		return nil, err
	}

	return ReadStrings(r, sb, desc, utils.DefaultReadLimit)
}

// ReadStrings reads every element of a string dataset. Fixed-length strings
// are decoded; variable-length strings are reported as unsupported here (the
// vlen reader handles them).
//
// An array of strings reads as its strings flattened in C order, array
// dimensions innermost (flattenArray), as for numeric arrays.
func ReadStrings(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, limit uint64) ([]string, error) {
	base, count, err := flattenArray(desc.datatype)
	if err != nil {
		return nil, err
	}

	if !base.IsString() {
		return nil, fmt.Errorf("datatype is not string: %s", desc.datatype)
	}

	if desc.nelmts == 0 {
		return []string{}, nil
	}

	// No overflow: count is at most the array's 32-bit size.
	rawData, err := readRawData(r, sb, desc, ResultString*count, limit)
	if err != nil {
		return nil, err
	}

	values, err := utils.SafeMultiply(desc.nelmts, count)
	if err != nil {
		return nil, fmt.Errorf("string data size overflow: %w", err)
	}

	return convertToStrings(rawData, base, values)
}

// convertToStrings converts raw bytes to string array based on string datatype.
func convertToStrings(rawData []byte, datatype *DatatypeMessage, numElements uint64) ([]string, error) {
	// Reject an unsupported string class before the storage guard: a zero Size
	// would otherwise surface as "element size is zero" and hide the type error.
	if !datatype.IsFixedString() && !datatype.IsVariableString() {
		return nil, errors.New("unknown string type")
	}

	// The count is metadata-derived; validate it against the bytes read before
	// sizing the result slice (ADR-003, same class as parseCompoundData).
	if err := validateElementStorage(numElements, uint64(datatype.Size), rawData, "string data"); err != nil {
		return nil, err
	}

	result := make([]string, numElements)

	if !datatype.IsFixedString() {
		// Variable-length strings.
		// Format: each element is (global_heap_id, size, index).
		// For now, return error - variable-length strings require global heap support.
		return nil, errors.New("variable-length strings not yet supported")
	}

	// Fixed-length strings.
	stringSize := uint64(datatype.Size)
	paddingType := datatype.GetStringPadding()

	// CVE-2025-2926 fix: Validate string size before processing.
	if err := utils.ValidateBufferSize(stringSize, utils.MaxStringSize, "string element"); err != nil {
		return nil, fmt.Errorf("string element too large: %w", err)
	}

	for i := uint64(0); i < numElements; i++ {
		offset := i * stringSize
		if offset+stringSize > uint64(len(rawData)) {
			return nil, errors.New("data truncated (fixed string)")
		}

		stringBytes := rawData[offset : offset+stringSize]
		result[i] = decodeFixedString(stringBytes, paddingType)
	}

	return result, nil
}

// decodeFixedString decodes a fixed-length string based on padding type.
// paddingType: 0 = null-terminated, 1 = null-padded, 2 = space-padded.
func decodeFixedString(data []byte, paddingType uint8) string {
	switch paddingType {
	case 0: // Null-terminated.
		// CVE-2025-2926 fix: Enforce null terminator for NULLTERM padding.
		// According to HDF5 spec, NULLTERM strings MUST end with null.
		// However, some old files may not enforce this strictly, so we're lenient
		// and just trim to first null if present.
		// Find first null byte.
		idx := bytes.IndexByte(data, 0)
		if idx >= 0 {
			return string(data[:idx])
		}
		return string(data)

	case 1: // Null-padded.
		// Trim trailing nulls.
		data = bytes.TrimRight(data, "\x00")
		return string(data)

	case 2: // Space-padded.
		// Trim trailing spaces.
		data = bytes.TrimRight(data, " ")
		return string(data)

	default:
		// Unknown padding, just convert as-is.
		return string(data)
	}
}
