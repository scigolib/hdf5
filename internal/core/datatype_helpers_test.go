package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsFloat64 tests float64 type detection.
func TestIsFloat64(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "float64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want: true,
		},
		{
			name: "float32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  4,
			},
			want: false,
		},
		{
			name: "int64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  8,
			},
			want: false,
		},
		{
			name: "string type",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  8,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsFloat64()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsFloat32 tests float32 type detection.
func TestIsFloat32(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "float32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  4,
			},
			want: true,
		},
		{
			name: "float64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want: false,
		},
		{
			name: "int32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsFloat32()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsInt32 tests int32 type detection.
func TestIsInt32(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "int32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: true,
		},
		{
			name: "int64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  8,
			},
			want: false,
		},
		{
			name: "float32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  4,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsInt32()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsInt64 tests int64 type detection.
func TestIsInt64(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "int64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  8,
			},
			want: true,
		},
		{
			name: "int32 type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: false,
		},
		{
			name: "float64 type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsInt64()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsString tests string type detection.
func TestIsString(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "string type",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  10,
			},
			want: true,
		},
		{
			name: "int type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: false,
		},
		{
			name: "float type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsString()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsFixedString tests fixed-length string detection.
func TestIsFixedString(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "fixed string with size 10",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  10,
			},
			want: true,
		},
		{
			name: "string with size 0",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  0,
			},
			want: false,
		},
		{
			name: "non-string type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  10,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsFixedString()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsVariableString tests variable-length string detection.
// Reference: HDF5 Format Specification III.A.2.4.d (Variable-Length Types).
// ClassBitField bits 0-3 contain the VL type: 0=Sequence, 1=String.
func TestIsVariableString(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "varlen string (type=1)",
			dt: &DatatypeMessage{
				Class:         DatatypeVarLen,
				ClassBitField: 0x0001, // Type = 1 (String)
			},
			want: true,
		},
		{
			name: "varlen string UTF-8 (type=1, charset=1)",
			dt: &DatatypeMessage{
				Class:         DatatypeVarLen,
				ClassBitField: 0x0101, // Type = 1 (String), Charset = 1 (UTF-8)
			},
			want: true,
		},
		{
			name: "varlen sequence (type=0)",
			dt: &DatatypeMessage{
				Class:         DatatypeVarLen,
				ClassBitField: 0x0000, // Type = 0 (Sequence)
			},
			want: false,
		},
		{
			name: "fixed string",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  10,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsVariableString()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsCompound tests compound type detection.
func TestIsCompound(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want bool
	}{
		{
			name: "compound type",
			dt: &DatatypeMessage{
				Class: DatatypeCompound,
				Size:  100,
			},
			want: true,
		},
		{
			name: "int type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.IsCompound()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestGetStringPadding tests string padding extraction.
func TestGetStringPadding(t *testing.T) {
	tests := []struct {
		name          string
		classBitField uint32
		want          uint8
	}{
		{
			name:          "null-terminated",
			classBitField: 0x00,
			want:          0,
		},
		{
			name:          "null-padded",
			classBitField: 0x01,
			want:          1,
		},
		{
			name:          "space-padded",
			classBitField: 0x02,
			want:          2,
		},
		{
			name:          "padding with other bits set",
			classBitField: 0xFF01,
			want:          1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := &DatatypeMessage{
				ClassBitField: tt.classBitField,
			}
			got := dt.GetStringPadding()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestGetByteOrder tests byte order extraction.
func TestGetByteOrder(t *testing.T) {
	tests := []struct {
		name          string
		classBitField uint32
		want          binary.ByteOrder
	}{
		{
			name:          "little-endian (bit 0 = 0)",
			classBitField: 0x00,
			want:          binary.LittleEndian,
		},
		{
			name:          "big-endian (bit 0 = 1)",
			classBitField: 0x01,
			want:          binary.BigEndian,
		},
		{
			name:          "little-endian with other bits",
			classBitField: 0xFE,
			want:          binary.LittleEndian,
		},
		{
			name:          "big-endian with other bits",
			classBitField: 0xFF,
			want:          binary.BigEndian,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := &DatatypeMessage{
				ClassBitField: tt.classBitField,
			}
			got := dt.GetByteOrder()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestGetEncodedSize tests encoded size calculation.
func TestGetEncodedSize(t *testing.T) {
	tests := []struct {
		name       string
		dt         *DatatypeMessage
		wantSize   int
		wantReason string
	}{
		{
			name: "integer type",
			dt: &DatatypeMessage{
				Class:      DatatypeFixed,
				Properties: []byte{},
			},
			wantSize:   12,
			wantReason: "8-byte header + 4 bytes properties",
		},
		{
			name: "float type",
			dt: &DatatypeMessage{
				Class:      DatatypeFloat,
				Properties: []byte{},
			},
			wantSize:   20,
			wantReason: "8-byte header + 12 bytes properties",
		},
		{
			name: "bitfield type",
			dt: &DatatypeMessage{
				Class:      DatatypeBitfield,
				Properties: []byte{},
			},
			wantSize:   12,
			wantReason: "8-byte header + 4 bytes properties",
		},
		{
			name: "time type",
			dt: &DatatypeMessage{
				Class:      DatatypeTime,
				Properties: []byte{},
			},
			wantSize:   10,
			wantReason: "8-byte header + 2 bytes properties",
		},
		{
			name: "string type",
			dt: &DatatypeMessage{
				Class:      DatatypeString,
				Properties: []byte{0x01, 0x02},
			},
			wantSize:   10,
			wantReason: "8-byte header + 2 bytes properties",
		},
		{
			name: "compound type",
			dt: &DatatypeMessage{
				Class:      DatatypeCompound,
				Properties: make([]byte, 50),
			},
			wantSize:   58,
			wantReason: "8-byte header + 50 bytes properties",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.GetEncodedSize()
			require.Equal(t, tt.wantSize, got, tt.wantReason)
		})
	}
}

// TestDatatypeString tests String() method.
func TestDatatypeString(t *testing.T) {
	tests := []struct {
		name string
		dt   *DatatypeMessage
		want string
	}{
		{
			name: "integer type",
			dt: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			want: "integer (size=4 bytes)",
		},
		{
			name: "float type",
			dt: &DatatypeMessage{
				Class: DatatypeFloat,
				Size:  8,
			},
			want: "float (size=8 bytes)",
		},
		{
			name: "string type",
			dt: &DatatypeMessage{
				Class: DatatypeString,
				Size:  10,
			},
			want: "string (size=10 bytes)",
		},
		{
			name: "compound type",
			dt: &DatatypeMessage{
				Class: DatatypeCompound,
				Size:  100,
			},
			want: "compound (size=100 bytes)",
		},
		{
			name: "array type",
			dt: &DatatypeMessage{
				Class: DatatypeArray,
				Size:  50,
			},
			want: "array (size=50 bytes)",
		},
		{
			name: "unknown type",
			dt: &DatatypeMessage{
				Class: 99,
				Size:  20,
			},
			want: "class_99 (size=20 bytes)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dt.String()
			require.Equal(t, tt.want, got)
		})
	}
}
