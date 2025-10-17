package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// ReadDatasetStrings reads a string dataset and returns values as string array.
// Supports both fixed-length and variable-length strings.
func ReadDatasetStrings(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]string, error) {
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

	// Verify it's a string type.
	if !datatype.IsString() {
		return nil, fmt.Errorf("datatype is not string: %s", datatype)
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
		return []string{}, nil
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
		// TODO: Add filter pipeline support for string datasets.
		rawData, err = readChunkedData(r, layout, dataspace, datatype, sb, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunked data: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", layout.Class)
	}

	// 7. Convert raw bytes to string array based on string type.
	return convertToStrings(rawData, datatype, totalElements)
}

// convertToStrings converts raw bytes to string array based on string datatype.
func convertToStrings(rawData []byte, datatype *DatatypeMessage, numElements uint64) ([]string, error) {
	result := make([]string, numElements)

	//nolint:gocritic // ifElseChain: method call conditions, not suitable for switch
	if datatype.IsFixedString() {
		// Fixed-length strings.
		stringSize := uint64(datatype.Size)
		paddingType := datatype.GetStringPadding()

		for i := uint64(0); i < numElements; i++ {
			offset := i * stringSize
			if offset+stringSize > uint64(len(rawData)) {
				return nil, errors.New("data truncated (fixed string)")
			}

			stringBytes := rawData[offset : offset+stringSize]
			result[i] = decodeFixedString(stringBytes, paddingType)
		}
	} else if datatype.IsVariableString() {
		// Variable-length strings.
		// Format: each element is (global_heap_id, size, index).
		// For now, return error - variable-length strings require global heap support.
		return nil, errors.New("variable-length strings not yet supported")
	} else {
		return nil, errors.New("unknown string type")
	}

	return result, nil
}

// decodeFixedString decodes a fixed-length string based on padding type.
// paddingType: 0 = null-terminated, 1 = null-padded, 2 = space-padded.
func decodeFixedString(data []byte, paddingType uint8) string {
	switch paddingType {
	case 0: // Null-terminated.
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
