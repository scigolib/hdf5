package core

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestParseCompoundType tests compound type parsing errors.
func TestParseCompoundType(t *testing.T) {
	tests := []struct {
		name        string
		dt          *DatatypeMessage
		errContains string
		corrupt     bool // error must wrap utils.ErrCorrupt
	}{
		{
			name: "not a compound type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
			},
			errContains: "not a compound datatype",
		},
		{
			// Before the codec: "too short" from the reader's own v3 layout.
			// The member count is in the class bit field for every version
			// (H5Odtype.c:358); zero members is invalid.
			// (Version 3 with flags 0 is the legacy hdf5-go marker, so
			// version 1 shows the C check.)
			name: "zero members",
			dt: &DatatypeMessage{
				Class:      DatatypeCompound,
				Version:    1,
				Size:       4,
				Properties: []byte{0x01},
			},
			errContains: "invalid number of members: 0",
			corrupt:     true,
		},
		{
			// Before the codec version 2 was "unsupported"; it is decoded now,
			// so a version-2 compound fails only on its bytes.
			name: "version 2 truncated member",
			dt: &DatatypeMessage{
				Class:         DatatypeCompound,
				Version:       2,
				Size:          4,
				ClassBitField: 1,
				Properties:    []byte{'x', 0x00},
			},
			errContains: "ran off end",
			corrupt:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCompoundType(tt.dt)
			require.Error(t, err)
			require.Nil(t, got)
			require.Contains(t, err.Error(), tt.errContains)
			if tt.corrupt {
				require.ErrorIs(t, err, utils.ErrCorrupt)
			}
		})
	}
}

// TestParseCompoundType_Version1 tests version 1 compound parsing.
func TestParseCompoundType_Version1(t *testing.T) {
	// Create v1 compound with 1 member: "x" (int32 at offset 0)
	// Format: name (8-byte aligned) + offset(4) + array info(28) + member datatype
	properties := make([]byte, 0, 100)

	// Member name "x" + null, padded to 8 bytes
	properties = append(properties, []byte("x\x00\x00\x00\x00\x00\x00\x00")...)

	// Offset (4 bytes)
	offsetBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(offsetBuf, 0)
	properties = append(properties, offsetBuf...)

	// Array info (28 bytes) - all zeros for scalar
	properties = append(properties, make([]byte, 28)...)

	// Member datatype (int32): header plus bit offset 0 and precision 32,
	// which the C decoder requires (H5Odtype.c:175-186).
	properties = append(properties, int32LEBytes...)

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
	// Version 3: the member count is in the class bit field (not in the
	// properties), names are not padded and the offset takes
	// limit_enc_size(8) = 1 byte (H5Odtype.c:353-358, :432-440).
	properties := make([]byte, 0, 100)
	properties = append(properties, []byte("value\x00")...)
	properties = append(properties, 0) // offset 0, 1 byte
	properties = append(properties, float64LEBytes...)

	dt := &DatatypeMessage{
		Class:         DatatypeCompound,
		Version:       3,
		Size:          8,
		ClassBitField: 1,
		Properties:    properties,
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

// TestParseCompoundType_EmptyCompound tests parsing an empty compound. The old
// reader accepted it; the C library rejects a compound without members
// (H5Odtype.c:358-360) and a datatype of size 0 (:163-164).
func TestParseCompoundType_EmptyCompound(t *testing.T) {
	dt := &DatatypeMessage{
		Class:   DatatypeCompound,
		Version: 3,
		Size:    0,
	}

	got, err := ParseCompoundType(dt)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Nil(t, got)
}
