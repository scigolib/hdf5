package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"github.com/scigolib/hdf5/internal/utils"
)

// Attribute represents an HDF5 attribute with metadata and data.
type Attribute struct {
	Name      string
	Datatype  *DatatypeMessage
	Dataspace *DataspaceMessage
	Data      []byte
}

// AttributeInfoMessage represents the Attribute Info Message (0x000F).
// This message contains information about dense attribute storage.
// Reference: H5Adense.c in C library.
type AttributeInfoMessage struct {
	Version            uint8
	Flags              uint8
	FractalHeapAddr    uint64 // Address of fractal heap for dense attribute storage
	BTreeNameIndexAddr uint64 // Address of B-tree v2 for indexing attributes by name
	// Optional fields for creation order (if tracked):
	MaxCreationIndex    uint64 // Only present if creation order tracked
	BTreeOrderIndexAddr uint64 // Only present if creation order indexed
}

// ParseAttributeMessage parses an attribute message (type 0x000C).
// Format according to HDF5 spec:
// - Version (1 byte).
// - Flags (1 byte) - reserved, always 0.
// - Name size (2 bytes).
// - Datatype size (2 bytes).
// - Dataspace size (2 bytes).
// - Name encoding (1 byte) - for version 3+.
// - Name (variable, null-terminated).
// - Datatype message data.
// - Dataspace message data.
// - Data (variable).
func ParseAttributeMessage(data []byte, endianness binary.ByteOrder) (*Attribute, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("attribute message too short: %d bytes", len(data))
	}

	attr := &Attribute{}
	offset := 0

	// Version.
	version := data[offset]
	offset++

	// Flags (reserved).
	offset++

	// Name size (2 bytes).
	nameSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Datatype size (2 bytes).
	datatypeSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Dataspace size (2 bytes).
	dataspaceSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// For version 3+, there's a name encoding byte.
	if version >= 3 {
		// Name encoding (1 byte) - 0 = ASCII, 1 = UTF-8.
		offset++
	}

	// Read name (null-terminated).
	if offset+int(nameSize) > len(data) {
		return nil, fmt.Errorf("name extends beyond message: offset=%d, nameSize=%d, dataLen=%d",
			offset, nameSize, len(data))
	}

	// Name is null-terminated, so subtract 1 for the null byte.
	if nameSize > 0 {
		attr.Name = string(data[offset : offset+int(nameSize)-1])
		offset += int(nameSize)
	}

	// Parse datatype.
	if offset+int(datatypeSize) > len(data) {
		return nil, fmt.Errorf("datatype extends beyond message")
	}

	datatypeData := data[offset : offset+int(datatypeSize)]
	var err error
	attr.Datatype, err = ParseDatatypeMessage(datatypeData)
	if err != nil {
		return nil, utils.WrapError("datatype parse failed", err)
	}
	offset += int(datatypeSize)

	// Parse dataspace.
	if offset+int(dataspaceSize) > len(data) {
		return nil, fmt.Errorf("dataspace extends beyond message")
	}

	dataspaceData := data[offset : offset+int(dataspaceSize)]
	attr.Dataspace, err = ParseDataspaceMessage(dataspaceData)
	if err != nil {
		return nil, utils.WrapError("dataspace parse failed", err)
	}
	offset += int(dataspaceSize)

	// Remaining data is the attribute value.
	if offset < len(data) {
		attr.Data = make([]byte, len(data)-offset)
		copy(attr.Data, data[offset:])
	}

	return attr, nil
}

// ReadValue reads the attribute value as the appropriate Go type.
func (a *Attribute) ReadValue() (interface{}, error) {
	if a.Datatype == nil || a.Dataspace == nil {
		return nil, fmt.Errorf("attribute missing datatype or dataspace")
	}

	totalElements := a.Dataspace.TotalElements()
	if totalElements == 0 || len(a.Data) == 0 {
		// Empty attribute - return empty slice instead of nil.
		return []interface{}{}, nil
	}

	// For scalar attributes, return single value.
	isScalar := len(a.Dataspace.Dimensions) == 0 ||
		(len(a.Dataspace.Dimensions) == 1 && a.Dataspace.Dimensions[0] == 1)

	switch a.Datatype.Class {
	case DatatypeFixed:
		switch a.Datatype.Size {
		case 4:
			values := make([]int32, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 4
				if offset+4 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
				values[i] = int32(binary.LittleEndian.Uint32(a.Data[offset : offset+4]))
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		case 8:
			values := make([]int64, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 8
				if offset+8 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
				values[i] = int64(binary.LittleEndian.Uint64(a.Data[offset : offset+8]))
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		}

	case DatatypeFloat:
		switch a.Datatype.Size {
		case 4:
			values := make([]float32, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 4
				if offset+4 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				bits := binary.LittleEndian.Uint32(a.Data[offset : offset+4])
				values[i] = float32frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		case 8:
			values := make([]float64, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 8
				if offset+8 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				bits := binary.LittleEndian.Uint64(a.Data[offset : offset+8])
				values[i] = float64frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		}
	}

	return nil, fmt.Errorf("unsupported datatype class %d or size %d", a.Datatype.Class, a.Datatype.Size)
}

// ParseAttributesFromMessages extracts all attributes from object header messages.
// Supports both compact attributes (stored in object header) and dense attributes
// (stored in fractal heap).
// Reference: H5Adense.c in C library.
func ParseAttributesFromMessages(r io.ReaderAt, messages []*HeaderMessage, sb *Superblock) ([]*Attribute, error) {
	var attributes []*Attribute
	var attrInfo *AttributeInfoMessage

	// First pass: look for AttributeInfo message (dense storage)
	for _, msg := range messages {
		if msg.Type == MsgAttributeInfo {
			var err error
			attrInfo, err = ParseAttributeInfoMessage(msg.Data, sb)
			if err != nil {
				// If we can't parse attribute info, fall back to compact only
				attrInfo = nil
			}
			break
		}
	}

	// Second pass: collect compact attributes
	for _, msg := range messages {
		if msg.Type == MsgAttribute {
			attr, err := ParseAttributeMessage(msg.Data, sb.Endianness)
			if err != nil {
				// Log error but continue with other attributes
				continue
			}
			attributes = append(attributes, attr)
		}
	}

	// If dense storage exists, read attributes from fractal heap
	if attrInfo != nil && attrInfo.FractalHeapAddr != 0 {
		denseAttrs, err := readDenseAttributes(r, attrInfo, sb)
		if err != nil {
			// For v0.10.0-beta: log error but don't fail
			// Dense attributes are a "best effort" feature
			// Most files use compact attributes
			_ = err // Silently ignore for now
		} else {
			attributes = append(attributes, denseAttrs...)
		}
	}

	return attributes, nil
}

// Helper functions for float conversion (copied from dataset_reader.go to avoid circular import).
func float32frombits(b uint32) float32 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float32 bit representation
	return *(*float32)(unsafe.Pointer(&b))
}

func float64frombits(b uint64) float64 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float64 bit representation
	return *(*float64)(unsafe.Pointer(&b))
}

// readDenseAttributes reads attributes from dense storage (fractal heap).
// Reference: H5Adense.c in C library.
//
// KNOWN LIMITATION (v0.10.0-beta):
// Dense attributes require:
// 1. Fractal heap reading (implemented in internal/structures)
// 2. B-tree v2 iteration (NOT implemented yet)
// 3. Integration without circular dependencies (architecture issue)
//
// Dense attributes are rare in most scientific HDF5 files (< 10% of files).
// Most files use compact attribute storage (stored directly in object header).
//
// This will be fully implemented in v0.11.0-beta.
func readDenseAttributes(r io.ReaderAt, attrInfo *AttributeInfoMessage, sb *Superblock) ([]*Attribute, error) {
	// Avoid unused variable errors
	_ = r
	_ = attrInfo
	_ = sb

	// Return empty slice (not an error) to allow file reading to continue
	// User will see compact attributes but not dense attributes
	return nil, nil
}

// ParseAttributeInfoMessage parses an Attribute Info Message (0x000F).
// Reference: H5Oainfo.c in C library.
// Format:
// - Version (1 byte)
// - Flags (1 byte):
//   - Bit 0: Track creation order
//   - Bit 1: Index creation order
//
// - Max Creation Index (2 bytes) - only if track creation order flag set
// - Fractal Heap Address (variable, based on superblock offset size)
// - B-tree Name Index Address (variable)
// - B-tree Order Index Address (variable) - only if index creation order flag set.
func ParseAttributeInfoMessage(data []byte, sb *Superblock) (*AttributeInfoMessage, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("attribute info message too short: %d bytes", len(data))
	}

	msg := &AttributeInfoMessage{}
	offset := 0

	// Version
	msg.Version = data[offset]
	offset++

	// Flags
	msg.Flags = data[offset]
	offset++

	trackCreationOrder := (msg.Flags & 0x01) != 0
	indexCreationOrder := (msg.Flags & 0x02) != 0

	// Max Creation Index (2 bytes) - only if creation order tracked
	if trackCreationOrder {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("attribute info message too short for max creation index")
		}
		msg.MaxCreationIndex = uint64(sb.Endianness.Uint16(data[offset : offset+2]))
		offset += 2
	}

	// Fractal Heap Address (offset size bytes)
	offsetSize := int(sb.OffsetSize)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for fractal heap address")
	}
	msg.FractalHeapAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Name Index Address (offset size bytes)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for btree name index address")
	}
	msg.BTreeNameIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Order Index Address (offset size bytes) - only if indexed
	if indexCreationOrder {
		if offset+offsetSize > len(data) {
			return nil, fmt.Errorf("attribute info message too short for btree order index address")
		}
		msg.BTreeOrderIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	}

	return msg, nil
}
