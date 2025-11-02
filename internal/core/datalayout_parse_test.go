package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDataLayoutMessage tests data layout message parsing.
func TestParseDataLayoutMessage(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	tests := []struct {
		name        string
		data        []byte
		wantClass   DataLayoutClass
		wantVersion uint8
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty data",
			data:        []byte{},
			wantErr:     true,
			errContains: "too short",
		},
		{
			name:        "unsupported version 0",
			data:        []byte{0x00, 0x01},
			wantErr:     true,
			errContains: "unsupported data layout version",
		},
		{
			name:        "unsupported version 5",
			data:        []byte{0x05, 0x01},
			wantErr:     true,
			errContains: "unsupported data layout version",
		},
		{
			name: "version 3 compact layout",
			data: func() []byte {
				d := make([]byte, 4+10) // version + class + size(2) + data(10)
				d[0] = 3                // version 3
				d[1] = byte(LayoutCompact)
				binary.LittleEndian.PutUint16(d[2:4], 10) // size
				copy(d[4:], "0123456789")                 // compact data
				return d
			}(),
			wantVersion: 3,
			wantClass:   LayoutCompact,
			wantErr:     false,
		},
		{
			name: "version 3 contiguous layout",
			data: func() []byte {
				d := make([]byte, 2+8+8) // version + class + address + size
				d[0] = 3                 // version 3
				d[1] = byte(LayoutContiguous)
				binary.LittleEndian.PutUint64(d[2:10], 0x1000)  // data address
				binary.LittleEndian.PutUint64(d[10:18], 0x4000) // data size
				return d
			}(),
			wantVersion: 3,
			wantClass:   LayoutContiguous,
			wantErr:     false,
		},
		{
			name: "version 3 chunked layout 1D",
			data: func() []byte {
				// version + class + dimensionality + btree address + chunk dims
				d := make([]byte, 2+1+8+4)
				d[0] = 3 // version 3
				d[1] = byte(LayoutChunked)
				d[2] = 1                                       // dimensionality
				binary.LittleEndian.PutUint64(d[3:11], 0x2000) // btree address
				binary.LittleEndian.PutUint32(d[11:15], 100)   // chunk size[0]
				return d
			}(),
			wantVersion: 3,
			wantClass:   LayoutChunked,
			wantErr:     false,
		},
		{
			name: "version 3 chunked layout 2D",
			data: func() []byte {
				d := make([]byte, 2+1+8+8) // 2D needs 2*4 bytes for chunk sizes
				d[0] = 3                   // version 3
				d[1] = byte(LayoutChunked)
				d[2] = 2                                       // dimensionality
				binary.LittleEndian.PutUint64(d[3:11], 0x3000) // btree address
				binary.LittleEndian.PutUint32(d[11:15], 50)    // chunk size[0]
				binary.LittleEndian.PutUint32(d[15:19], 100)   // chunk size[1]
				return d
			}(),
			wantVersion: 3,
			wantClass:   LayoutChunked,
			wantErr:     false,
		},
		{
			name: "version 4 contiguous",
			data: func() []byte {
				d := make([]byte, 2+8+8)
				d[0] = 4 // version 4
				d[1] = byte(LayoutContiguous)
				binary.LittleEndian.PutUint64(d[2:10], 0x5000)
				binary.LittleEndian.PutUint64(d[10:18], 0x8000)
				return d
			}(),
			wantVersion: 4,
			wantClass:   LayoutContiguous,
			wantErr:     false,
		},
		{
			name: "version 3 unsupported class",
			data: func() []byte {
				d := make([]byte, 2)
				d[0] = 3  // version 3
				d[1] = 99 // invalid class
				return d
			}(),
			wantErr:     true,
			errContains: "unsupported layout class",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDataLayoutMessage(tt.data, sb)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tt.wantVersion, got.Version)
			require.Equal(t, tt.wantClass, got.Class)
		})
	}
}

// TestParseDataLayoutMessage_CompactDetails tests compact layout details.
func TestParseDataLayoutMessage_CompactDetails(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	testData := []byte("HelloWorld")
	data := make([]byte, 4+len(testData))
	data[0] = 3 // version 3
	data[1] = byte(LayoutCompact)
	binary.LittleEndian.PutUint16(data[2:4], uint16(len(testData)))
	copy(data[4:], testData)

	got, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.True(t, got.IsCompact())
	require.Equal(t, uint64(len(testData)), got.DataSize)
	require.Equal(t, testData, got.CompactData)
}

// TestParseDataLayoutMessage_ContiguousDetails tests contiguous layout details.
func TestParseDataLayoutMessage_ContiguousDetails(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	data := make([]byte, 2+8+8)
	data[0] = 3
	data[1] = byte(LayoutContiguous)
	binary.LittleEndian.PutUint64(data[2:10], 0xABCD)
	binary.LittleEndian.PutUint64(data[10:18], 0x12345)

	got, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.True(t, got.IsContiguous())
	require.Equal(t, uint64(0xABCD), got.DataAddress)
	require.Equal(t, uint64(0x12345), got.DataSize)
}

// TestParseDataLayoutMessage_ChunkedDetails tests chunked layout details.
func TestParseDataLayoutMessage_ChunkedDetails(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	data := make([]byte, 2+1+8+12) // 3D chunked
	data[0] = 3
	data[1] = byte(LayoutChunked)
	data[2] = 3 // dimensionality
	binary.LittleEndian.PutUint64(data[3:11], 0x7000)
	binary.LittleEndian.PutUint32(data[11:15], 10)
	binary.LittleEndian.PutUint32(data[15:19], 20)
	binary.LittleEndian.PutUint32(data[19:23], 30)

	got, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.True(t, got.IsChunked())
	require.Equal(t, uint64(0x7000), got.DataAddress)
	require.Equal(t, []uint32{10, 20, 30}, got.ChunkSize)
}

// TestParseDataLayoutMessage_SmallOffsets tests with 4-byte offsets.
func TestParseDataLayoutMessage_SmallOffsets(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 4,
		LengthSize: 4,
		Endianness: binary.LittleEndian,
	}

	data := make([]byte, 2+4+4)
	data[0] = 3
	data[1] = byte(LayoutContiguous)
	binary.LittleEndian.PutUint32(data[2:6], 0x1234)
	binary.LittleEndian.PutUint32(data[6:10], 0x5678)

	got, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0x1234), got.DataAddress)
	require.Equal(t, uint64(0x5678), got.DataSize)
}
