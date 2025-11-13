package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDatasetInfoString tests DatasetInfo String() method.
func TestDatasetInfoString(t *testing.T) {
	tests := []struct {
		name string
		info *DatasetInfo
		want string
	}{
		{
			name: "float64 1D contiguous dataset",
			info: &DatasetInfo{
				Datatype: &DatatypeMessage{
					Class: DatatypeFloat,
					Size:  8,
				},
				Dataspace: &DataspaceMessage{
					Type:       DataspaceSimple,
					Dimensions: []uint64{100},
				},
				Layout: &DataLayoutMessage{
					Class:       LayoutContiguous,
					DataAddress: 0x1000,
					DataSize:    800,
				},
			},
			want: "Dataset: float (size=8 bytes), 1D array [100], contiguous (address=0x1000, size=800)",
		},
		{
			name: "int32 2D chunked dataset",
			info: &DatasetInfo{
				Datatype: &DatatypeMessage{
					Class: DatatypeFixed,
					Size:  4,
				},
				Dataspace: &DataspaceMessage{
					Type:       DataspaceSimple,
					Dimensions: []uint64{50, 100},
				},
				Layout: &DataLayoutMessage{
					Class:     LayoutChunked,
					ChunkSize: []uint64{10, 20},
				},
			},
			want: "Dataset: integer (size=4 bytes), 2D array [50 x 100], chunked (chunks=[10 20])",
		},
		{
			name: "string scalar compact dataset",
			info: &DatasetInfo{
				Datatype: &DatatypeMessage{
					Class: DatatypeString,
					Size:  10,
				},
				Dataspace: &DataspaceMessage{
					Type:       DataspaceScalar,
					Dimensions: []uint64{1},
				},
				Layout: &DataLayoutMessage{
					Class:    LayoutCompact,
					DataSize: 10,
				},
			},
			want: "Dataset: string (size=10 bytes), scalar, compact (size=10)",
		},
		{
			name: "compound type 3D dataset",
			info: &DatasetInfo{
				Datatype: &DatatypeMessage{
					Class: DatatypeCompound,
					Size:  24,
				},
				Dataspace: &DataspaceMessage{
					Type:       DataspaceSimple,
					Dimensions: []uint64{10, 20, 30},
				},
				Layout: &DataLayoutMessage{
					Class:       LayoutContiguous,
					DataAddress: 0x5000,
					DataSize:    144000,
				},
			},
			want: "Dataset: compound (size=24 bytes), 3D array [10 20 30], contiguous (address=0x5000, size=144000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.String()
			require.Equal(t, tt.want, got)
		})
	}
}
