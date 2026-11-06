package hdf5

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// expectedHyperslab computes the values a hyperslab selection must return
// from a row-major dataset whose element i holds float64(i): every selected
// coordinate in row-major selection order, mapped to its linear index.
func expectedHyperslab(dims []uint64, sel *HyperslabSelection) []float64 {
	var out []float64

	coords := make([]uint64, len(dims))
	var walk func(d int)
	walk = func(d int) {
		if d == len(dims) {
			idx, stride := uint64(0), uint64(1)
			for i := len(dims) - 1; i >= 0; i-- {
				idx += coords[i] * stride
				stride *= dims[i]
			}
			out = append(out, float64(idx))
			return
		}
		for c := uint64(0); c < sel.Count[d]; c++ {
			for b := uint64(0); b < sel.Block[d]; b++ {
				coords[d] = sel.Start[d] + c*sel.Stride[d] + b
				walk(d + 1)
			}
		}
	}
	walk(0)

	return out
}

// TestReadHyperslab_ContiguousMatchesReference checks every element of
// contiguous hyperslab reads against values computed from the coordinates.
//
// The pre-fix contiguous readers returned wrong data for three shapes of
// selection (dataset_read_hyperslab.go on 257fb7d):
//   - a 1-D selection with Stride or Block > 1 was read as one contiguous run
//     (every 1-D selection went to readContiguousOptimized, :433, which ignores
//     Stride and Block);
//   - a selection whose last dimension is complete but whose middle dimension
//     is partial was also read as one run (isContiguousSelection, :441-458,
//     checks only the last dimension);
//   - a 3-D or higher selection with a nonzero start indexed its bounding box
//     with absolute offsets (:546-578), the "known bounding-box offset issue"
//     that TestReadHyperslab_3DContiguous documents.
func TestReadHyperslab_ContiguousMatchesReference(t *testing.T) {
	cases := []struct {
		name string
		dims []uint64
		sel  HyperslabSelection
	}{
		{"1D stride 2", []uint64{20}, HyperslabSelection{
			Start: []uint64{1}, Count: []uint64{5}, Stride: []uint64{2}, Block: []uint64{1},
		}},
		{"1D blocks", []uint64{20}, HyperslabSelection{
			Start: []uint64{0}, Count: []uint64{4}, Stride: []uint64{5}, Block: []uint64{2},
		}},
		{"1D plain", []uint64{20}, HyperslabSelection{
			Start: []uint64{3}, Count: []uint64{7}, Stride: []uint64{1}, Block: []uint64{1},
		}},
		{"2D full rows partial middle", []uint64{6, 5}, HyperslabSelection{
			Start: []uint64{1, 0}, Count: []uint64{2, 5}, Stride: []uint64{3, 1}, Block: []uint64{1, 1},
		}},
		{"2D partial columns", []uint64{6, 5}, HyperslabSelection{
			Start: []uint64{1, 1}, Count: []uint64{3, 2}, Stride: []uint64{2, 2}, Block: []uint64{1, 1},
		}},
		{"3D last full, middle partial", []uint64{2, 3, 4}, HyperslabSelection{
			Start: []uint64{0, 1, 0}, Count: []uint64{2, 1, 4}, Stride: []uint64{1, 1, 1}, Block: []uint64{1, 1, 1},
		}},
		{"3D nonzero start", []uint64{4, 5, 6}, HyperslabSelection{
			Start: []uint64{1, 2, 3}, Count: []uint64{2, 2, 2}, Stride: []uint64{1, 1, 1}, Block: []uint64{1, 1, 1},
		}},
		{"3D strided blocks", []uint64{4, 5, 6}, HyperslabSelection{
			Start: []uint64{1, 0, 1}, Count: []uint64{1, 2, 2}, Stride: []uint64{1, 3, 3}, Block: []uint64{2, 2, 2},
		}},
		{"4D nonzero start", []uint64{3, 3, 3, 3}, HyperslabSelection{
			Start: []uint64{1, 1, 1, 1}, Count: []uint64{2, 2, 2, 2}, Stride: []uint64{1, 1, 1, 1}, Block: []uint64{1, 1, 1, 1},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runs.h5")
			fw, err := CreateForWrite(path, CreateTruncate)
			require.NoError(t, err)

			n := uint64(1)
			for _, d := range tc.dims {
				n *= d
			}
			vals := make([]float64, n)
			for i := range vals {
				vals[i] = float64(i)
			}
			dw, err := fw.CreateDataset("/x", Float64, tc.dims)
			require.NoError(t, err)
			require.NoError(t, dw.Write(vals))
			require.NoError(t, fw.Close())

			f, err := Open(path)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()
			ds := findDatasetPath(t, f, "/x")

			sel := tc.sel
			got, err := ds.ReadHyperslab(&sel)
			require.NoError(t, err)

			want := expectedHyperslab(tc.dims, &tc.sel)
			require.Equal(t, want, got, "selection %s", fmt.Sprint(tc.sel))
		})
	}
}
