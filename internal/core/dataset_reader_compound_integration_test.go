package core

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadDatasetCompound_RealFile tests with actual compound dataset file.
func TestReadDatasetCompound_RealFile(t *testing.T) {
	file := "../../testdata/compound_test.h5"

	f, err := os.Open(file)
	if os.IsNotExist(err) {
		t.Skipf("Test file not found: %s", file)
	}
	require.NoError(t, err)
	defer f.Close()

	// Read superblock
	sb, err := ReadSuperblock(f)
	require.NoError(t, err)
	require.NotNil(t, sb)

	// This test is skipped for now as it requires object path resolution
	// Once symbol table walking is implemented, this can be completed
	t.Skip("requires symbol table walking to find dataset")

	// Future implementation:
	// objHeader, err := findObjectByPath(f, sb, "/compound_dataset")
	// require.NoError(t, err)
	//
	// data, err := ReadDatasetCompound(f, objHeader, sb)
	// require.NoError(t, err)
	// require.NotEmpty(t, data)
}

// TestReadDatasetCompound_ErrorCases tests error handling.
func TestReadDatasetCompound_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		header  *ObjectHeader
		sb      *Superblock
		wantErr string
	}{
		{
			name: "missing datatype message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDataspace,
						Data: buildSimpleDataspaceMessage([]uint64{10}),
					},
					{
						Type: MsgDataLayout,
						Data: buildContiguousLayoutMessage(0x1000, 100),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: "datatype message not found",
		},
		{
			name: "missing dataspace message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildSimpleCompoundDatatypeMessage(),
					},
					{
						Type: MsgDataLayout,
						Data: buildContiguousLayoutMessage(0x1000, 100),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: "dataspace message not found",
		},
		{
			name: "missing layout message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildSimpleCompoundDatatypeMessage(),
					},
					{
						Type: MsgDataspace,
						Data: buildSimpleDataspaceMessage([]uint64{10}),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: "data layout message not found",
		},
		{
			name: "not a compound datatype",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildFloat64DatatypeMessage(), // Not compound!
					},
					{
						Type: MsgDataspace,
						Data: buildSimpleDataspaceMessage([]uint64{10}),
					},
					{
						Type: MsgDataLayout,
						Data: buildContiguousLayoutMessage(0x1000, 80),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: "not a compound datatype",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emptyReader := &emptyReaderAt{}

			data, err := ReadDatasetCompound(emptyReader, tt.header, tt.sb)

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
			require.Nil(t, data)
		})
	}
}

// TestReadDatasetCompound_EmptyDataset tests reading empty compound datasets.
func TestReadDatasetCompound_EmptyDataset(t *testing.T) {
	t.Skip("requires valid compound datatype message with member definitions - complex to build")

	// Building a valid compound datatype message requires:
	// - Version byte
	// - Class and flags
	// - Size (4 bytes)
	// - Number of members (2 bytes)
	// - For each member:
	//   - Name (null-terminated string, padded to multiple of 8)
	//   - Byte offset (4 bytes, variable depending on version)
	//   - Dimensionality (1 byte)
	//   - Reserved bytes
	//   - Member datatype definition (recursive structure)
	//
	// This is too complex for a simple mock - integration tests with real files are better
}

// TestParseMemberValue_AllTypes tests parsing individual member values.
func TestParseMemberValue_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		datatype *DatatypeMessage
		want     interface{}
		wantErr  bool
	}{
		{
			name: "float64",
			data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f}, // 1.0
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0,
			},
			want:    float64(1.0),
			wantErr: false,
		},
		{
			name: "float32",
			data: []byte{0x00, 0x00, 0x80, 0x3f}, // 1.0
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          4,
				ClassBitField: 0,
			},
			want:    float32(1.0),
			wantErr: false,
		},
		{
			name: "int32",
			data: []byte{0x2a, 0x00, 0x00, 0x00}, // 42
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0,
			},
			want:    int32(42),
			wantErr: false,
		},
		{
			name: "int64",
			data: []byte{0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // 100
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          8,
				ClassBitField: 0,
			},
			want:    int64(100),
			wantErr: false,
		},
		{
			name: "fixed string",
			data: []byte{'h', 'e', 'l', 'l', 'o', 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          8,
				ClassBitField: 0, // Null-terminated
			},
			want:    "hello",
			wantErr: false,
		},
		{
			name: "insufficient data for float64",
			data: []byte{0x00, 0x00, 0x00}, // Only 3 bytes
			datatype: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "insufficient data for int32",
			data: []byte{0x00, 0x00}, // Only 2 bytes
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
			emptyReader := &emptyReaderAt{}

			result, err := parseMemberValue(tt.data, tt.datatype, emptyReader, sb)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

// TestExtractString_Compound tests string extraction with different padding.
func TestExtractString_Compound(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		paddingType uint8
		want        string
	}{
		{
			name:        "null-terminated",
			data:        []byte{'t', 'e', 's', 't', 0, 0, 0},
			paddingType: 0,
			want:        "test",
		},
		{
			name:        "null-padded",
			data:        []byte{'a', 'b', 'c', 0, 0},
			paddingType: 1,
			want:        "abc",
		},
		{
			name:        "space-padded",
			data:        []byte{'h', 'i', ' ', ' ', ' '},
			paddingType: 2,
			want:        "hi",
		},
		{
			name:        "unknown padding - return as-is",
			data:        []byte{'x', 'y', 'z'},
			paddingType: 99,
			want:        "xyz",
		},
		{
			name:        "no null in null-terminated",
			data:        []byte{'f', 'u', 'l', 'l'},
			paddingType: 0,
			want:        "full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractString(tt.data, tt.paddingType)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestParseCompoundData_DataTruncation tests handling of truncated data.
func TestParseCompoundData_DataTruncation(t *testing.T) {
	// Create compound type: { int32 a; float32 b; } = 8 bytes per element
	compoundType := &CompoundType{
		Size: 8,
		Members: []CompoundMember{
			{
				Name:   "a",
				Offset: 0,
				Type: &DatatypeMessage{
					Class: DatatypeFixed,
					Size:  4,
				},
			},
			{
				Name:   "b",
				Offset: 4,
				Type: &DatatypeMessage{
					Class: DatatypeFloat,
					Size:  4,
				},
			},
		},
	}

	// Provide only 12 bytes for 2 elements (should be 16)
	rawData := make([]byte, 12)

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	emptyReader := &emptyReaderAt{}

	result, err := parseCompoundData(rawData, compoundType, 2, emptyReader, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "data truncated")
	require.Nil(t, result)
}

// Helper functions

// buildSimpleCompoundDatatypeMessage creates a basic compound datatype message.
// This is a simplified version - real compound types have member definitions.
func buildSimpleCompoundDatatypeMessage() []byte {
	// Compound datatype (version 1):
	// Byte 0-3: class (6 = compound) | version << 4
	// Byte 4-7: size (e.g., 12 bytes)
	// Note: Real compound type includes member definitions, but we skip for error testing
	data := make([]byte, 8)
	classAndVersion := uint32(DatatypeCompound) | (1 << 4)
	binary.LittleEndian.PutUint32(data[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(data[4:8], 12) // Compound size
	return data
}

// TestCompoundValue_MapStructure tests CompoundValue as map.
func TestCompoundValue_MapStructure(t *testing.T) {
	// CompoundValue is map[string]interface{}
	cv := CompoundValue{
		"id":    int32(123),
		"name":  "test",
		"value": float64(3.14),
	}

	require.Equal(t, int32(123), cv["id"])
	require.Equal(t, "test", cv["name"])
	require.Equal(t, float64(3.14), cv["value"])
}

// TestReadDatasetCompound_UnsupportedMemberType tests unsupported member types.
func TestReadDatasetCompound_UnsupportedMemberType(t *testing.T) {
	t.Skip("requires creating compound type with unsupported member - complex setup")

	// Future implementation would create a compound type with:
	// - A member of unsupported datatype (e.g., bitfield)
	// - Expect error when parsing member value
}
