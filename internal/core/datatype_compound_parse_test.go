package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCompoundType tests compound type parsing.
func TestParseCompoundType(t *testing.T) {
	tests := []struct {
		name        string
		dt          *DatatypeMessage
		wantErr     bool
		errContains string
	}{
		{
			name: "not a compound type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
			},
			wantErr:     true,
			errContains: "not a compound datatype",
		},
		{
			name: "properties too short",
			dt: &DatatypeMessage{
				Class:      DatatypeCompound,
				Properties: []byte{0x01}, // Only 1 byte
			},
			wantErr:     true,
			errContains: "too short",
		},
		{
			name: "unsupported version",
			dt: &DatatypeMessage{
				Class:      DatatypeCompound,
				Version:    2,
				Properties: []byte{0x00, 0x00},
			},
			wantErr:     true,
			errContains: "unsupported compound datatype version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCompoundType(tt.dt)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
		})
	}
}

// TestParseCompoundType_Version1 tests version 1 compound parsing.
func TestParseCompoundType_Version1(t *testing.T) {
	// Create v1 compound with 1 member: "x" (int32 at offset 0)
	// Format: name (8-byte aligned) + offset(4) + array info(28) + member datatype(8+)
	properties := make([]byte, 0, 100)

	// Member name "x" + null, padded to 8 bytes
	properties = append(properties, []byte("x\x00\x00\x00\x00\x00\x00\x00")...)

	// Offset (4 bytes)
	offsetBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(offsetBuf, 0)
	properties = append(properties, offsetBuf...)

	// Array info (28 bytes) - all zeros for scalar
	properties = append(properties, make([]byte, 28)...)

	// Member datatype (int32)
	// Class and version packed (4 bytes)
	classAndVer := uint32(DatatypeFixed) | (1 << 4) // class=0, version=1
	dtBuf := make([]byte, 8)
	binary.LittleEndian.PutUint32(dtBuf[0:4], classAndVer)
	binary.LittleEndian.PutUint32(dtBuf[4:8], 4) // size = 4
	properties = append(properties, dtBuf...)

	dt := &DatatypeMessage{
		Class:         DatatypeCompound,
		Version:       1,
		Size:          4,
		ClassBitField: 1, // 1 member in bits 0-15
		Properties:    properties,
	}

	got, err := ParseCompoundType(dt)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint32(4), got.Size)
	require.Equal(t, 1, len(got.Members))
	require.Equal(t, "x", got.Members[0].Name)
	require.Equal(t, uint32(0), got.Members[0].Offset)
	require.Equal(t, DatatypeFixed, got.Members[0].Type.Class)
	require.Equal(t, uint32(4), got.Members[0].Type.Size)
}

// TestParseCompoundType_Version3 tests version 3 compound parsing.
func TestParseCompoundType_Version3(t *testing.T) {
	// Version 3 format: num members(4) + [name + offset(4) + datatype(8+)]*
	properties := make([]byte, 0, 100)

	// Number of members (4 bytes in v3)
	numMembers := make([]byte, 4)
	binary.LittleEndian.PutUint32(numMembers, 1)
	properties = append(properties, numMembers...)

	// Member: "value"
	properties = append(properties, []byte("value\x00")...) // Null-terminated

	// Offset (4 bytes)
	offsetBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(offsetBuf, 0)
	properties = append(properties, offsetBuf...)

	// Member datatype (float64)
	classAndVer := uint32(DatatypeFloat) | (1 << 4) // class=1, version=1
	dtBuf := make([]byte, 8)
	binary.LittleEndian.PutUint32(dtBuf[0:4], classAndVer)
	binary.LittleEndian.PutUint32(dtBuf[4:8], 8) // size = 8
	properties = append(properties, dtBuf...)

	dt := &DatatypeMessage{
		Class:      DatatypeCompound,
		Version:    3,
		Size:       8,
		Properties: properties,
	}

	got, err := ParseCompoundType(dt)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint32(8), got.Size)
	require.Equal(t, 1, len(got.Members))
	require.Equal(t, "value", got.Members[0].Name)
	require.Equal(t, uint32(0), got.Members[0].Offset)
	require.Equal(t, DatatypeFloat, got.Members[0].Type.Class)
	require.Equal(t, uint32(8), got.Members[0].Type.Size)
}

// TestParseCompoundType_EmptyCompound tests parsing empty compound.
func TestParseCompoundType_EmptyCompound(t *testing.T) {
	// V3 format with 0 members
	properties := make([]byte, 4)
	binary.LittleEndian.PutUint32(properties, 0)

	dt := &DatatypeMessage{
		Class:      DatatypeCompound,
		Version:    3,
		Size:       0,
		Properties: properties,
	}

	got, err := ParseCompoundType(dt)
	require.NoError(t, err)
	require.Equal(t, 0, len(got.Members))
	require.Equal(t, uint32(0), got.Size)
}
