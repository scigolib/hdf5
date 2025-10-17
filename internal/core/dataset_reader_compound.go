package core

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// CompoundValue represents a single compound structure instance as a map of field names to values.
type CompoundValue map[string]interface{}

// ReadDatasetCompound reads a dataset with compound datatype and returns array of compound values.
func ReadDatasetCompound(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]CompoundValue, error) {
	// 1. Extract required messages.
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

	// Validate.
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

	if !datatype.IsCompound() {
		return nil, errors.New("not a compound datatype")
	}

	// Parse compound structure.
	compoundType, err := ParseCompoundType(datatype)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compound type: %w", err)
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

	// 5. Parse filter pipeline (optional).
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
		return []CompoundValue{}, nil
	}

	// 7. Read raw data based on layout.
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
		rawData, err = readChunkedData(r, layout, dataspace, datatype, sb, filterPipeline)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunked data: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", layout.Class)
	}

	// 8. Convert raw bytes to compound values.
	return parseCompoundData(rawData, compoundType, totalElements, r, sb)
}

// parseCompoundData parses raw bytes into array of compound values.
func parseCompoundData(rawData []byte, compoundType *CompoundType, numElements uint64, r io.ReaderAt, sb *Superblock) ([]CompoundValue, error) {
	result := make([]CompoundValue, numElements)
	structSize := uint64(compoundType.Size)

	for i := uint64(0); i < numElements; i++ {
		structOffset := i * structSize

		if structOffset+structSize > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated at element %d", i)
		}

		structData := rawData[structOffset : structOffset+structSize]
		value := make(CompoundValue)

		// Parse each member.
		for _, member := range compoundType.Members {
			memberData := structData[member.Offset:]

			memberValue, err := parseMemberValue(memberData, member.Type, r, sb)
			if err != nil {
				return nil, fmt.Errorf("failed to parse member %s: %w", member.Name, err)
			}

			value[member.Name] = memberValue
		}

		result[i] = value
	}

	return result, nil
}

// parseMemberValue parses a single member value based on its datatype.
func parseMemberValue(data []byte, datatype *DatatypeMessage, r io.ReaderAt, sb *Superblock) (interface{}, error) {
	byteOrder := datatype.GetByteOrder()

	switch {
	case datatype.IsFloat64():
		if len(data) < 8 {
			return nil, errors.New("insufficient data for float64")
		}
		bits := byteOrder.Uint64(data[0:8])
		return math.Float64frombits(bits), nil

	case datatype.IsFloat32():
		if len(data) < 4 {
			return nil, errors.New("insufficient data for float32")
		}
		bits := byteOrder.Uint32(data[0:4])
		return math.Float32frombits(bits), nil

	case datatype.IsInt32():
		if len(data) < 4 {
			return nil, errors.New("insufficient data for int32")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
		return int32(byteOrder.Uint32(data[0:4])), nil

	case datatype.IsInt64():
		if len(data) < 8 {
			return nil, errors.New("insufficient data for int64")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
		return int64(byteOrder.Uint64(data[0:8])), nil

	case datatype.IsFixedString():
		//nolint:gosec // G115: Safe length comparison
		if uint32(len(data)) < datatype.Size {
			return nil, errors.New("insufficient data for string")
		}
		// Extract string based on padding type.
		str := extractString(data[0:datatype.Size], datatype.GetStringPadding())
		return str, nil

	case datatype.IsVariableString():
		// Variable-length strings are stored as global heap references.
		// Parse the reference: heap_address (offset_size bytes) + object_index (4 bytes).
		return readVariableString(r, data, sb)

	case datatype.IsCompound():
		// Nested compound - recursive parse.
		nestedCompound, err := ParseCompoundType(datatype)
		if err != nil {
			return nil, fmt.Errorf("failed to parse nested compound: %w", err)
		}
		values, err := parseCompoundData(data, nestedCompound, 1, r, sb)
		if err != nil {
			return nil, err
		}
		return values[0], nil

	default:
		return nil, fmt.Errorf("unsupported member datatype: %s", datatype)
	}
}

// extractString extracts a string from fixed-length byte array based on padding type.
func extractString(data []byte, paddingType uint8) string {
	switch paddingType {
	case 0: // Null-terminated.
		for i, b := range data {
			if b == 0 {
				return string(data[:i])
			}
		}
		return string(data)

	case 1: // Null-padded.
		end := len(data)
		for end > 0 && data[end-1] == 0 {
			end--
		}
		return string(data[:end])

	case 2: // Space-padded.
		end := len(data)
		for end > 0 && (data[end-1] == ' ' || data[end-1] == 0) {
			end--
		}
		return string(data[:end])

	default:
		return string(data)
	}
}

// readVariableString reads a variable-length string from the Global Heap.
// The data contains a Global Heap reference: heap_address (offset_size bytes) + object_index (4 bytes).
func readVariableString(r io.ReaderAt, data []byte, sb *Superblock) (string, error) {
	// Parse the global heap reference.
	offsetSize := int(sb.OffsetSize)
	ref, err := ParseGlobalHeapReference(data, offsetSize)
	if err != nil {
		return "", fmt.Errorf("failed to parse global heap reference: %w", err)
	}

	// Check for null reference (address 0 typically means empty/null string).
	if ref.HeapAddress == 0 {
		return "", nil
	}

	// Read the global heap collection.
	collection, err := ReadGlobalHeapCollection(r, ref.HeapAddress, offsetSize)
	if err != nil {
		return "", fmt.Errorf("failed to read global heap collection at 0x%X: %w", ref.HeapAddress, err)
	}

	// Get the object from the collection.
	obj, err := collection.GetObject(ref.ObjectIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get object %d from heap collection: %w", ref.ObjectIndex, err)
	}

	// Convert object data to string.
	// Variable-length strings are typically null-terminated.
	str := string(obj.Data)

	// Remove trailing null bytes if present.
	for str != "" && str[len(str)-1] == 0 {
		str = str[:len(str)-1]
	}

	return str, nil
}
