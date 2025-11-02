package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsScalar tests scalar dataspace detection.
func TestIsScalar(t *testing.T) {
	tests := []struct {
		name string
		ds   *DataspaceMessage
		want bool
	}{
		{
			name: "scalar dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceScalar,
				Dimensions: []uint64{1},
			},
			want: true,
		},
		{
			name: "simple 1D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10},
			},
			want: false,
		},
		{
			name: "null dataspace",
			ds: &DataspaceMessage{
				Type: DataspaceNull,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.IsScalar()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIs1D tests 1D dataspace detection.
func TestIs1D(t *testing.T) {
	tests := []struct {
		name string
		ds   *DataspaceMessage
		want bool
	}{
		{
			name: "1D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10},
			},
			want: true,
		},
		{
			name: "2D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10, 20},
			},
			want: false,
		},
		{
			name: "scalar dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceScalar,
				Dimensions: []uint64{1},
			},
			want: false,
		},
		{
			name: "3D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{5, 10, 15},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.Is1D()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIs2D tests 2D dataspace detection.
func TestIs2D(t *testing.T) {
	tests := []struct {
		name string
		ds   *DataspaceMessage
		want bool
	}{
		{
			name: "2D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10, 20},
			},
			want: true,
		},
		{
			name: "1D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10},
			},
			want: false,
		},
		{
			name: "3D dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{5, 10, 15},
			},
			want: false,
		},
		{
			name: "scalar dataspace",
			ds: &DataspaceMessage{
				Type:       DataspaceScalar,
				Dimensions: []uint64{1},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.Is2D()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestDataspaceString tests String() method.
func TestDataspaceString(t *testing.T) {
	tests := []struct {
		name string
		ds   *DataspaceMessage
		want string
	}{
		{
			name: "scalar",
			ds: &DataspaceMessage{
				Type:       DataspaceScalar,
				Dimensions: []uint64{1},
			},
			want: "scalar",
		},
		{
			name: "null",
			ds: &DataspaceMessage{
				Type: DataspaceNull,
			},
			want: "null",
		},
		{
			name: "1D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{100},
			},
			want: "1D array [100]",
		},
		{
			name: "2D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10, 20},
			},
			want: "2D array [10 x 20]",
		},
		{
			name: "3D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{5, 10, 15},
			},
			want: "3D array [5 10 15]",
		},
		{
			name: "4D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{2, 3, 4, 5},
			},
			want: "4D array [2 3 4 5]",
		},
		{
			name: "unknown type",
			ds: &DataspaceMessage{
				Type: 99,
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.String()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestTotalElements tests total elements calculation.
func TestTotalElements(t *testing.T) {
	tests := []struct {
		name string
		ds   *DataspaceMessage
		want uint64
	}{
		{
			name: "scalar",
			ds: &DataspaceMessage{
				Type:       DataspaceScalar,
				Dimensions: []uint64{1},
			},
			want: 1,
		},
		{
			name: "null dataspace",
			ds: &DataspaceMessage{
				Type: DataspaceNull,
			},
			want: 0,
		},
		{
			name: "1D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{100},
			},
			want: 100,
		},
		{
			name: "2D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{10, 20},
			},
			want: 200,
		},
		{
			name: "3D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{5, 10, 15},
			},
			want: 750,
		},
		{
			name: "4D array",
			ds: &DataspaceMessage{
				Type:       DataspaceSimple,
				Dimensions: []uint64{2, 3, 4, 5},
			},
			want: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.TotalElements()
			require.Equal(t, tt.want, got)
		})
	}
}
