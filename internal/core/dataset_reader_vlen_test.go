package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadVariableLengthStrings(t *testing.T) {
	// Open test file with vlen strings in compound type
	filename := "../../testdata/vlen_strings.h5"
	file, err := os.Open(filename)
	require.NoError(t, err, "failed to open test file")
	defer file.Close()

	// Read superblock
	sb, err := ReadSuperblock(file)
	require.NoError(t, err, "failed to read superblock")

	// Find root group
	rootHeader, err := ReadObjectHeader(file, sb.RootGroup, sb)
	require.NoError(t, err, "failed to read root group header")

	// For now, this is a placeholder test
	// Full integration requires proper group traversal which is complex
	// We'll test the Global Heap functions separately
	_ = rootHeader

	t.Skip("Full integration test pending - needs dataset lookup through group links")
}

func TestGlobalHeapReference(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		offsetSize int
		wantAddr   uint64
		wantIndex  uint32
		wantErr    bool
	}{
		{
			name:       "valid reference with 8-byte offset",
			data:       []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x01, 0x00, 0x00, 0x00},
			offsetSize: 8,
			wantAddr:   0x8070605040302010,
			wantIndex:  1,
			wantErr:    false,
		},
		{
			name:       "valid reference with 4-byte offset",
			data:       []byte{0x10, 0x20, 0x30, 0x40, 0x02, 0x00, 0x00, 0x00},
			offsetSize: 4,
			wantAddr:   0x40302010,
			wantIndex:  2,
			wantErr:    false,
		},
		{
			name:       "null reference",
			data:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offsetSize: 8,
			wantAddr:   0,
			wantIndex:  0,
			wantErr:    false,
		},
		{
			name:       "insufficient data",
			data:       []byte{0x10, 0x20},
			offsetSize: 8,
			wantErr:    true,
		},
		{
			name:       "invalid offset size",
			data:       []byte{0x10, 0x20, 0x30, 0x40},
			offsetSize: 3,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseGlobalHeapReference(tt.data, tt.offsetSize)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, ref)
			require.Equal(t, tt.wantAddr, ref.HeapAddress, "heap address mismatch")
			require.Equal(t, tt.wantIndex, ref.ObjectIndex, "object index mismatch")
		})
	}
}

func TestGlobalHeapCollection(t *testing.T) {
	// This test would require a real HDF5 file with global heap
	// For now, we test the parsing logic separately
	t.Skip("Integration test - requires real HDF5 file with global heap")
}
