package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DataspaceType represents the type of dataspace.
type DataspaceType uint8

// Dataspace type constants define the dimensionality of datasets.
const (
	DataspaceScalar DataspaceType = 0 // Scalar (single value).
	DataspaceSimple DataspaceType = 1 // Simple (N-dimensional array).
	DataspaceNull   DataspaceType = 2 // Null (no data).
)

// DataspaceMessage represents HDF5 dataspace message.
type DataspaceMessage struct {
	Version    uint8
	Type       DataspaceType
	Dimensions []uint64
	MaxDims    []uint64 // Maximum dimensions (optional, for resizable datasets).
}

// ParseDataspaceMessage parses a dataspace message from header message data.
func ParseDataspaceMessage(data []byte) (*DataspaceMessage, error) {
	if len(data) < 2 {
		return nil, errors.New("dataspace message too short")
	}

	version := data[0]

	// Support both version 1 and version 2.
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("unsupported dataspace version: %d", version)
	}

	dimensionality := data[1]
	flags := data[2]

	// Bit 0 indicates max dimensions present.
	hasMaxDims := (flags & 0x01) != 0

	// Bit 1 indicates permutation indices present (rarely used).
	// We'll skip permutation for now.

	ds := &DataspaceMessage{
		Version: version,
	}

	// Determine dataspace type based on dimensionality.
	if dimensionality == 0 {
		// Scalar dataspace.
		ds.Type = DataspaceScalar
		ds.Dimensions = []uint64{1} // Treat scalar as 1-element array.
		return ds, nil
	}

	// Simple dataspace.
	ds.Type = DataspaceSimple

	// Determine offset based on version.
	var offset int
	if version == 1 {
		// Version 1: version(1) + dimensionality(1) + flags(1) + reserved(5) = 8 bytes.
		offset = 8
	} else {
		// Version 2: version(1) + dimensionality(1) + flags(1) + type(1) = 4 bytes.
		offset = 4
	}

	// Auto-detect dimension size based on message length.
	// Version 1 spec says 4 bytes, but some files (v0 superblock) use 8 bytes.
	totalDimsCount := int(dimensionality)
	if hasMaxDims {
		totalDimsCount *= 2 // dimensions + max dimensions.
	}

	expectedSize4 := offset + totalDimsCount*4
	expectedSize8 := offset + totalDimsCount*8

	var dimSize int
	//nolint:gocritic // ifElseChain: length comparison, not suitable for switch
	if len(data) >= expectedSize8 {
		// Enough space for 8-byte dimensions (common in v0 files).
		dimSize = 8
	} else if len(data) >= expectedSize4 {
		// Standard 4-byte dimensions for version 1.
		dimSize = 4
	} else {
		return nil, fmt.Errorf("dataspace message too short: %d bytes, need %d", len(data), expectedSize4)
	}

	// Read dimensions.
	ds.Dimensions = make([]uint64, dimensionality)
	for i := 0; i < int(dimensionality); i++ {
		if offset+dimSize > len(data) {
			return nil, errors.New("dataspace message truncated (dimensions)")
		}

		if dimSize == 4 {
			ds.Dimensions[i] = uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
		} else {
			ds.Dimensions[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
		}
		offset += dimSize
	}

	// Read max dimensions if present.
	if hasMaxDims {
		ds.MaxDims = make([]uint64, dimensionality)
		for i := 0; i < int(dimensionality); i++ {
			if offset+dimSize > len(data) {
				return nil, errors.New("dataspace message truncated (max dims)")
			}

			if dimSize == 4 {
				ds.MaxDims[i] = uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
			} else {
				ds.MaxDims[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
			}
			offset += dimSize
		}
	}

	return ds, nil
}

// TotalElements calculates total number of elements in dataspace.
func (ds *DataspaceMessage) TotalElements() uint64 {
	if ds.Type == DataspaceNull {
		return 0
	}

	if ds.Type == DataspaceScalar {
		return 1
	}

	total := uint64(1)
	for _, dim := range ds.Dimensions {
		total *= dim
	}
	return total
}

// String returns human-readable dataspace description.
func (ds *DataspaceMessage) String() string {
	switch ds.Type {
	case DataspaceScalar:
		return "scalar"
	case DataspaceNull:
		return "null"
	case DataspaceSimple:
		switch len(ds.Dimensions) {
		case 1:
			return fmt.Sprintf("1D array [%d]", ds.Dimensions[0])
		case 2:
			return fmt.Sprintf("2D array [%d x %d]", ds.Dimensions[0], ds.Dimensions[1])
		default:
			return fmt.Sprintf("%dD array %v", len(ds.Dimensions), ds.Dimensions)
		}
	default:
		return "unknown"
	}
}

// IsScalar returns true if dataspace is scalar (single value).
func (ds *DataspaceMessage) IsScalar() bool {
	return ds.Type == DataspaceScalar
}

// Is1D returns true if dataspace is 1-dimensional array.
func (ds *DataspaceMessage) Is1D() bool {
	return ds.Type == DataspaceSimple && len(ds.Dimensions) == 1
}

// Is2D returns true if dataspace is 2-dimensional array (matrix).
func (ds *DataspaceMessage) Is2D() bool {
	return ds.Type == DataspaceSimple && len(ds.Dimensions) == 2
}
