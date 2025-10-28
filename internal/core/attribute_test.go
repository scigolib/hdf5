package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAttributeInfoMessage(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		offsetSize  uint8
		wantVersion uint8
		wantFlags   uint8
		wantFHeap   uint64
		wantBTree   uint64
		wantErr     bool
	}{
		{
			name: "basic attribute info with 8-byte offsets",
			data: func() []byte {
				d := make([]byte, 18) // 2 (header) + 8 (heap addr) + 8 (btree addr)
				d[0] = 0              // version
				d[1] = 0              // flags (no creation order tracking)
				// Fractal heap address (8 bytes)
				binary.LittleEndian.PutUint64(d[2:10], 0x1000)
				// B-tree name index address (8 bytes)
				binary.LittleEndian.PutUint64(d[10:18], 0x2000)
				return d
			}(),
			offsetSize:  8,
			wantVersion: 0,
			wantFlags:   0,
			wantFHeap:   0x1000,
			wantBTree:   0x2000,
			wantErr:     false,
		},
		{
			name: "with creation order tracking",
			data: func() []byte {
				d := make([]byte, 20) // 2 + 2 (max idx) + 8 + 8
				d[0] = 0              // version
				d[1] = 0x01           // flags: track creation order
				// Max creation index (2 bytes)
				binary.LittleEndian.PutUint16(d[2:4], 42)
				// Fractal heap address
				binary.LittleEndian.PutUint64(d[4:12], 0x3000)
				// B-tree name index address
				binary.LittleEndian.PutUint64(d[12:20], 0x4000)
				return d
			}(),
			offsetSize:  8,
			wantVersion: 0,
			wantFlags:   0x01,
			wantFHeap:   0x3000,
			wantBTree:   0x4000,
			wantErr:     false,
		},
		{
			name: "with 4-byte offsets",
			data: func() []byte {
				d := make([]byte, 10) // 2 + 4 + 4
				d[0] = 0              // version
				d[1] = 0              // flags
				// Fractal heap address (4 bytes)
				binary.LittleEndian.PutUint32(d[2:6], 0x1000)
				// B-tree name index address (4 bytes)
				binary.LittleEndian.PutUint32(d[6:10], 0x2000)
				return d
			}(),
			offsetSize:  4,
			wantVersion: 0,
			wantFlags:   0,
			wantFHeap:   0x1000,
			wantBTree:   0x2000,
			wantErr:     false,
		},
		{
			name:       "too short",
			data:       []byte{0}, // Only 1 byte
			offsetSize: 8,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := &Superblock{
				Endianness: binary.LittleEndian,
				OffsetSize: tt.offsetSize,
			}

			msg, err := ParseAttributeInfoMessage(tt.data, sb)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, msg)
			require.Equal(t, tt.wantVersion, msg.Version)
			require.Equal(t, tt.wantFlags, msg.Flags)
			require.Equal(t, tt.wantFHeap, msg.FractalHeapAddr)
			require.Equal(t, tt.wantBTree, msg.BTreeNameIndexAddr)
		})
	}
}

func TestParseAttributeMessage_Basic(t *testing.T) {
	// Test basic attribute message parsing
	// Version 1, no encoding byte

	// Calculate exact size needed:
	// 1 (version) + 1 (flags) + 2 (name size) + 2 (datatype size) + 2 (dataspace size)
	// + 5 (name "test\0") + 8 (datatype) + 8 (dataspace) + 4 (data)
	// = 33 bytes minimum
	data := make([]byte, 64) // Use larger buffer to be safe
	offset := 0

	// Version 1
	data[offset] = 1
	offset++

	// Flags (reserved)
	data[offset] = 0
	offset++

	// Name size (including null terminator)
	binary.LittleEndian.PutUint16(data[offset:offset+2], 5) // "test\0"
	offset += 2

	// Datatype size
	binary.LittleEndian.PutUint16(data[offset:offset+2], 8) // 8 bytes for int32 datatype
	offset += 2

	// Dataspace size
	binary.LittleEndian.PutUint16(data[offset:offset+2], 8) // 8 bytes for scalar dataspace
	offset += 2

	// Name: "test\0"
	copy(data[offset:], "test\x00")
	offset += 5

	// Datatype message (simplified - int32)
	data[offset] = 0                                          // Class: fixed-point
	data[offset+1] = 1                                        // Flags
	binary.LittleEndian.PutUint32(data[offset+4:offset+8], 4) // Size: 4 bytes (FIXED: proper slice bounds)
	offset += 8

	// Dataspace message (scalar)
	data[offset] = 1   // Version
	data[offset+1] = 0 // Dimensionality: 0 = scalar
	offset += 8

	// Attribute value (int32: 42)
	binary.LittleEndian.PutUint32(data[offset:offset+4], 42)
	offset += 4

	// Trim to actual size used
	data = data[:offset]

	attr, err := ParseAttributeMessage(data, binary.LittleEndian)
	require.NoError(t, err)
	require.NotNil(t, attr)
	require.Equal(t, "test", attr.Name)
	require.NotNil(t, attr.Datatype)
	require.NotNil(t, attr.Dataspace)
}

func TestParseAttributeMessage_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errMsg  string
	}{
		{
			name:    "too short - less than 8 bytes",
			data:    []byte{1, 0, 0, 5},
			wantErr: true,
			errMsg:  "attribute message too short",
		},
		{
			name: "name extends beyond message",
			data: func() []byte {
				d := make([]byte, 10)
				d[0] = 1                                   // version
				d[1] = 0                                   // flags
				binary.LittleEndian.PutUint16(d[2:4], 100) // name size way too large
				binary.LittleEndian.PutUint16(d[4:6], 8)   // datatype size
				binary.LittleEndian.PutUint16(d[6:8], 8)   // dataspace size
				return d
			}(),
			wantErr: true,
			errMsg:  "name extends beyond message",
		},
		{
			name: "datatype extends beyond message",
			data: func() []byte {
				d := make([]byte, 20)
				d[0] = 1                                   // version
				d[1] = 0                                   // flags
				binary.LittleEndian.PutUint16(d[2:4], 5)   // name size
				binary.LittleEndian.PutUint16(d[4:6], 100) // datatype size way too large
				binary.LittleEndian.PutUint16(d[6:8], 8)   // dataspace size
				copy(d[8:], "test\x00")
				return d
			}(),
			wantErr: true,
			errMsg:  "datatype extends beyond message",
		},
		{
			name: "dataspace extends beyond message",
			data: func() []byte {
				d := make([]byte, 30)
				d[0] = 1                                  // version
				d[1] = 0                                  // flags
				binary.LittleEndian.PutUint16(d[2:4], 5)  // name size
				binary.LittleEndian.PutUint16(d[4:6], 8)  // datatype size
				binary.LittleEndian.PutUint16(d[6:8], 50) // dataspace size too large
				copy(d[8:], "test\x00")
				// Add minimal datatype
				d[13] = 0 // class
				return d
			}(),
			wantErr: true,
			errMsg:  "dataspace extends beyond message",
		},
		{
			name: "empty name (nameSize = 0)",
			data: func() []byte {
				// Need space for: version(1) + flags(1) + name_size(2) + dt_size(2) + ds_size(2)
				// + datatype(8) + dataspace(8) = 24 bytes minimum
				d := make([]byte, 24)
				d[0] = 1                                 // version
				d[1] = 0                                 // flags
				binary.LittleEndian.PutUint16(d[2:4], 0) // name size = 0 (empty name)
				binary.LittleEndian.PutUint16(d[4:6], 8) // datatype size
				binary.LittleEndian.PutUint16(d[6:8], 8) // dataspace size
				// No name bytes (size = 0)
				// Datatype message at offset 8
				d[8] = 0                                   // class
				d[9] = 1                                   // flags
				binary.LittleEndian.PutUint32(d[12:16], 4) // size
				// Dataspace message at offset 16
				d[16] = 1 // version
				d[17] = 0 // dimensionality
				return d
			}(),
			wantErr: false, // Empty name is allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr, err := ParseAttributeMessage(tt.data, binary.LittleEndian)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
				require.Nil(t, attr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, attr)
			}
		})
	}
}

func TestParseAttributeMessage_Version3(t *testing.T) {
	// Test version 3 with encoding byte
	data := make([]byte, 64)
	offset := 0

	// Version 3
	data[offset] = 3
	offset++

	// Flags (reserved)
	data[offset] = 0
	offset++

	// Name size (including null terminator)
	binary.LittleEndian.PutUint16(data[offset:offset+2], 5) // "test\0"
	offset += 2

	// Datatype size
	binary.LittleEndian.PutUint16(data[offset:offset+2], 8)
	offset += 2

	// Dataspace size
	binary.LittleEndian.PutUint16(data[offset:offset+2], 8)
	offset += 2

	// Name encoding (1 byte) - ASCII = 0
	data[offset] = 0
	offset++

	// Name: "test\0"
	copy(data[offset:], "test\x00")
	offset += 5

	// Datatype message
	data[offset] = 0
	data[offset+1] = 1
	binary.LittleEndian.PutUint32(data[offset+4:offset+8], 4)
	offset += 8

	// Dataspace message (scalar)
	data[offset] = 1
	data[offset+1] = 0
	offset += 8

	// Attribute value
	binary.LittleEndian.PutUint32(data[offset:offset+4], 99)
	offset += 4

	data = data[:offset]

	attr, err := ParseAttributeMessage(data, binary.LittleEndian)
	require.NoError(t, err)
	require.NotNil(t, attr)
	require.Equal(t, "test", attr.Name)
}

func TestAttributeReadValue_EmptyAttribute(t *testing.T) {
	// Test empty attribute (0 elements)
	attr := &Attribute{
		Name: "empty",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Dimensions: []uint64{0}, // 0 elements
		},
		Data: []byte{},
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.NotNil(t, val)
	// Should return empty slice, not nil
	require.Equal(t, []interface{}{}, val)
}

func TestAttributeReadValue_MissingMetadata(t *testing.T) {
	tests := []struct {
		name string
		attr *Attribute
	}{
		{
			name: "missing datatype",
			attr: &Attribute{
				Name:      "test",
				Datatype:  nil,
				Dataspace: &DataspaceMessage{},
				Data:      []byte{1, 2, 3, 4},
			},
		},
		{
			name: "missing dataspace",
			attr: &Attribute{
				Name:      "test",
				Datatype:  &DatatypeMessage{},
				Dataspace: nil,
				Data:      []byte{1, 2, 3, 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.attr.ReadValue()
			require.Error(t, err)
			require.Nil(t, val)
			require.Contains(t, err.Error(), "missing datatype or dataspace")
		})
	}
}

func TestAttributeReadValue_ScalarTypes(t *testing.T) {
	tests := []struct {
		name      string
		datatype  *DatatypeMessage
		data      []byte
		wantValue interface{}
	}{
		{
			name: "scalar int32",
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			data:      []byte{0x2A, 0x00, 0x00, 0x00}, // 42 in little-endian
			wantValue: int32(42),
		},
		{
			name: "scalar int64",
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  8,
			},
			data:      []byte{0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // 100
			wantValue: int64(100),
		},
		{
			name: "scalar float32",
			datatype: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  4,
			},
			data:      []byte{0x00, 0x00, 0x80, 0x3F}, // 1.0 in IEEE 754
			wantValue: float32(1.0),
		},
		{
			name: "scalar float64",
			datatype: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			data:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F}, // 1.0
			wantValue: float64(1.0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := &Attribute{
				Name:     "test",
				Datatype: tt.datatype,
				Dataspace: &DataspaceMessage{
					Type:       DataspaceScalar, // CRITICAL: Scalar type!
					Dimensions: []uint64{},      // Scalar = empty dimensions
				},
				Data: tt.data,
			}

			val, err := attr.ReadValue()
			require.NoError(t, err)
			require.Equal(t, tt.wantValue, val)
		})
	}
}

func TestAttributeReadValue_ArrayTypes(t *testing.T) {
	tests := []struct {
		name       string
		datatype   *DatatypeMessage
		dimensions []uint64
		data       []byte
		wantValue  interface{}
	}{
		{
			name: "array of int32",
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			dimensions: []uint64{3},
			data: []byte{
				0x01, 0x00, 0x00, 0x00, // 1
				0x02, 0x00, 0x00, 0x00, // 2
				0x03, 0x00, 0x00, 0x00, // 3
			},
			wantValue: []int32{1, 2, 3},
		},
		{
			name: "array of float32",
			datatype: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  4,
			},
			dimensions: []uint64{2},
			data: []byte{
				0x00, 0x00, 0x80, 0x3F, // 1.0
				0x00, 0x00, 0x00, 0x40, // 2.0
			},
			wantValue: []float32{1.0, 2.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := &Attribute{
				Name:     "test",
				Datatype: tt.datatype,
				Dataspace: &DataspaceMessage{
					Type:       DataspaceSimple, // CRITICAL: Must set type to Simple for arrays!
					Dimensions: tt.dimensions,
				},
				Data: tt.data,
			}

			val, err := attr.ReadValue()
			require.NoError(t, err)
			require.Equal(t, tt.wantValue, val)
		})
	}
}

func TestAttributeReadValue_Errors(t *testing.T) {
	tests := []struct {
		name      string
		attr      *Attribute
		wantError string
	}{
		{
			name: "data too short for int32 array",
			attr: &Attribute{
				Name: "test",
				Datatype: &DatatypeMessage{
					Class: DatatypeFixed,
					Size:  4,
				},
				Dataspace: &DataspaceMessage{
					Type:       DataspaceSimple, // CRITICAL: Must set type!
					Dimensions: []uint64{3},     // Expects 12 bytes
				},
				Data: []byte{1, 2, 3, 4}, // Only 4 bytes
			},
			wantError: "data too short for element",
		},
		{
			name: "unsupported datatype class",
			attr: &Attribute{
				Name: "test",
				Datatype: &DatatypeMessage{
					Class: DatatypeClass(99), // Invalid class
					Size:  4,
				},
				Dataspace: &DataspaceMessage{
					Dimensions: []uint64{1},
				},
				Data: []byte{1, 2, 3, 4},
			},
			wantError: "unsupported datatype class",
		},
		{
			name: "unsupported size for fixed type",
			attr: &Attribute{
				Name: "test",
				Datatype: &DatatypeMessage{
					Class: DatatypeFixed,
					Size:  3, // Not 4 or 8
				},
				Dataspace: &DataspaceMessage{
					Dimensions: []uint64{1},
				},
				Data: []byte{1, 2, 3},
			},
			wantError: "unsupported datatype class",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.attr.ReadValue()
			require.Error(t, err)
			require.Nil(t, val)
			require.Contains(t, err.Error(), tt.wantError)
		})
	}
}
