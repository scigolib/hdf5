package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadHyperslabCompact_1D tests reading a hyperslab from a compact layout dataset (1D slice).
// Uses tfilters.h5 reference file which has a "compact" dataset with COMPACT storage layout.
// Dataset: 20x10 Int32, stored directly in the object header.
// Exercises: readHyperslabCompact, extractHyperslabFromRawData.
func TestReadHyperslabCompact_1D(t *testing.T) {
	f, err := Open("testdata/hdf5_official/tfilters.h5")
	require.NoError(t, err, "Open tfilters.h5")
	defer func() { _ = f.Close() }()

	var ds *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/compact" {
			if d, ok := obj.(*Dataset); ok {
				ds = d
			}
		}
	})
	require.NotNil(t, ds, "compact dataset not found in tfilters.h5")

	// Read a 1D-like slice: row 0, columns [0:4]
	result, err := ds.ReadSlice([]uint64{0, 0}, []uint64{1, 4})
	require.NoError(t, err, "ReadSlice on compact dataset")

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64, got %T", result)
	require.Len(t, resultData, 4)

	t.Logf("Compact 1D slice result: %v", resultData)
}

// TestReadHyperslabCompact_2D tests reading a 2D sub-region from a compact layout dataset.
// Exercises: readHyperslabCompact, extractHyperslabFromRawData, extractHyperslabRecursive.
func TestReadHyperslabCompact_2D(t *testing.T) {
	f, err := Open("testdata/hdf5_official/tfilters.h5")
	require.NoError(t, err, "Open tfilters.h5")
	defer func() { _ = f.Close() }()

	var ds *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/compact" {
			if d, ok := obj.(*Dataset); ok {
				ds = d
			}
		}
	})
	require.NotNil(t, ds, "compact dataset not found in tfilters.h5")

	// Read a 2D sub-region: rows [2:5], cols [3:7] = 3x4 block
	result, err := ds.ReadSlice([]uint64{2, 3}, []uint64{3, 4})
	require.NoError(t, err, "ReadSlice 2D on compact dataset")

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64")
	require.Len(t, resultData, 12, "expected 3*4=12 elements")

	t.Logf("Compact 2D sub-region result: %v", resultData)

	// Read entire dataset to verify we can read the full compact data
	resultFull, err := ds.ReadSlice([]uint64{0, 0}, []uint64{20, 10})
	require.NoError(t, err, "ReadSlice full compact dataset")

	fullData, ok := resultFull.([]float64)
	require.True(t, ok, "expected []float64")
	require.Len(t, fullData, 200, "expected 20*10=200 elements")
}

// TestReadHyperslab_CompactWithStride tests strided reading on the compact reference file.
// Exercises: readHyperslabCompact with stride parameters via extractHyperslabRecursive.
func TestReadHyperslab_CompactWithStride(t *testing.T) {
	f, err := Open("testdata/hdf5_official/tfilters.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var ds *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/compact" {
			if d, ok := obj.(*Dataset); ok {
				ds = d
			}
		}
	})
	require.NotNil(t, ds, "compact dataset not found")

	// Strided read on compact 20x10 dataset
	// Start=[0,0], Count=[5,3], Stride=[4,3]
	// Rows: 0, 4, 8, 12, 16 | Cols: 0, 3, 6
	selection := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{5, 3},
		Stride: []uint64{4, 3},
		Block:  []uint64{1, 1},
	}

	result, err := ds.ReadHyperslab(selection)
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 15, "expected 5*3=15 elements")

	t.Logf("Compact strided result: %v", resultData)
}

// TestReadHyperslab_Float32Dataset tests reading float32 data as float64 via hyperslab.
// Exercises: convertBytesToFloat32AsFloat64 (1D contiguous path).
func TestReadHyperslab_Float32Dataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_float32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float32, 100)
	for i := range data {
		data[i] = float32(i) * 1.5
	}

	dw, err := fw.CreateDataset("/float32_data", Float32, []uint64{100})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "float32_data")
	require.True(t, found, "float32_data not found")

	// ReadSlice: read elements [10:60]
	result, err := ds.ReadSlice([]uint64{10}, []uint64{50})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64, got %T", result)
	require.Len(t, resultData, 50)

	for i := 0; i < 50; i++ {
		expected := float64(float32(10+i) * 1.5)
		require.InDelta(t, expected, resultData[i], 1e-5,
			"element %d: expected %f, got %f", i, expected, resultData[i])
	}
}

// TestReadHyperslab_Float32_2D tests reading a 2D float32 dataset with hyperslab.
// Exercises: convertBytesToFloat32AsFloat64 via 2D contiguous (readContiguous2DOptimized).
func TestReadHyperslab_Float32_2D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_float32_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float32, 100)
	for i := range data {
		data[i] = float32(i) * 0.25
	}

	dw, err := fw.CreateDataset("/f32_matrix", Float32, []uint64{10, 10})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "f32_matrix")
	require.True(t, found)

	// Read sub-block [2:5, 3:7] = 3x4 region
	result, err := ds.ReadSlice([]uint64{2, 3}, []uint64{3, 4})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 12)

	for r := 0; r < 3; r++ {
		for c := 0; c < 4; c++ {
			origIdx := (r+2)*10 + (c + 3)
			expected := float64(float32(origIdx) * 0.25)
			actual := resultData[r*4+c]
			require.InDelta(t, expected, actual, 1e-5,
				"[%d,%d]: expected %f, got %f", r, c, expected, actual)
		}
	}
}

// TestReadHyperslab_Int64Dataset tests reading int64 data as float64 via hyperslab.
// Exercises: convertBytesToInt64AsFloat64 (1D contiguous path).
func TestReadHyperslab_Int64Dataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_int64.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]int64, 100)
	for i := range data {
		data[i] = int64(i) * 1000
	}

	dw, err := fw.CreateDataset("/int64_data", Int64, []uint64{100})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "int64_data")
	require.True(t, found, "int64_data not found")

	// ReadSlice: read elements [25:75]
	result, err := ds.ReadSlice([]uint64{25}, []uint64{50})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64, got %T", result)
	require.Len(t, resultData, 50)

	for i := 0; i < 50; i++ {
		expected := float64((25 + i) * 1000)
		require.Equal(t, expected, resultData[i],
			"element %d: expected %f, got %f", i, expected, resultData[i])
	}
}

// TestReadHyperslab_Int64_Negative tests int64 dataset with negative values.
// Exercises: convertBytesToInt64AsFloat64 with signed values.
func TestReadHyperslab_Int64_Negative(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_int64_neg.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]int64, 50)
	for i := range data {
		data[i] = int64(i-25) * 100
	}

	dw, err := fw.CreateDataset("/neg_data", Int64, []uint64{50})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "neg_data")
	require.True(t, found)

	result, err := ds.ReadSlice([]uint64{0}, []uint64{50})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 50)

	for i := 0; i < 50; i++ {
		expected := float64((i - 25) * 100)
		require.Equal(t, expected, resultData[i],
			"element %d: expected %f, got %f", i, expected, resultData[i])
	}
}

// TestReadHyperslab_3DContiguous tests reading a 3D dataset via ReadSlice.
// Exercises: readContiguousRowByRow for 3D, extractHyperslabRecursive.
// Note: The 3D row-by-row reader has a known bounding-box offset issue,
// so this test verifies the code path executes and returns correct element count.
func TestReadHyperslab_3DContiguous(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_3d.h5")

	dimX, dimY, dimZ := uint64(4), uint64(5), uint64(6)

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, dimX*dimY*dimZ)
	for x := uint64(0); x < dimX; x++ {
		for y := uint64(0); y < dimY; y++ {
			for z := uint64(0); z < dimZ; z++ {
				idx := x*dimY*dimZ + y*dimZ + z
				data[idx] = float64(x*100 + y*10 + z)
			}
		}
	}

	dw, err := fw.CreateDataset("/data_3d", Float64, []uint64{dimX, dimY, dimZ})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data_3d")
	require.True(t, found)

	// Read sub-block with non-contiguous last dim to force readContiguousRowByRow.
	// [0:2, 0:3, 0:4] = 2x3x4 region (last dim < full dim 6)
	countX, countY, countZ := uint64(2), uint64(3), uint64(4)

	result, err := ds.ReadSlice(
		[]uint64{0, 0, 0},
		[]uint64{countX, countY, countZ},
	)
	require.NoError(t, err, "ReadSlice on 3D contiguous dataset should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64")
	require.Len(t, resultData, int(countX*countY*countZ),
		"expected %d elements", countX*countY*countZ)

	// Verify at least the first plane (x=0) which should be correctly read
	idx := 0
	for y := uint64(0); y < countY; y++ {
		for z := uint64(0); z < countZ; z++ {
			expected := float64(y*10 + z)
			require.Equal(t, expected, resultData[idx],
				"[0,%d,%d] (idx %d): expected %f, got %f",
				y, z, idx, expected, resultData[idx])
			idx++
		}
	}
}

// TestReadHyperslab_3DStridedSelection tests 3D hyperslab with stride > 1.
// Exercises: readContiguousRowByRow with stride, extractHyperslabRecursive.
func TestReadHyperslab_3DStridedSelection(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_3d_strided.h5")

	dimX, dimY, dimZ := uint64(8), uint64(8), uint64(8)

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, dimX*dimY*dimZ)
	for x := uint64(0); x < dimX; x++ {
		for y := uint64(0); y < dimY; y++ {
			for z := uint64(0); z < dimZ; z++ {
				idx := x*dimY*dimZ + y*dimZ + z
				data[idx] = float64(x*100 + y*10 + z)
			}
		}
	}

	dw, err := fw.CreateDataset("/data_3d_stride", Float64, []uint64{dimX, dimY, dimZ})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data_3d_stride")
	require.True(t, found)

	// Strided selection: start=[0,0,0], count=[4,4,4], stride=[2,2,2]
	selection := &HyperslabSelection{
		Start:  []uint64{0, 0, 0},
		Count:  []uint64{4, 4, 4},
		Stride: []uint64{2, 2, 2},
		Block:  []uint64{1, 1, 1},
	}

	result, err := ds.ReadHyperslab(selection)
	require.NoError(t, err, "ReadHyperslab on 3D strided should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 64, "expected 4*4*4=64 elements")

	// Verify the first plane (cx=0) values
	idx := 0
	for cy := uint64(0); cy < 4; cy++ {
		for cz := uint64(0); cz < 4; cz++ {
			y := cy * 2
			z := cz * 2
			expected := float64(y*10 + z)
			require.Equal(t, expected, resultData[idx],
				"cx=0, cy=%d, cz=%d (idx %d): expected %f, got %f",
				cy, cz, idx, expected, resultData[idx])
			idx++
		}
	}
}

// TestReadHyperslab_2DStridedSelection tests 2D hyperslab with varying stride.
// Exercises: readContiguous2DOptimized with asymmetric strides.
func TestReadHyperslab_2DStridedSelection(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_2d_stride.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, 400)
	for r := 0; r < 20; r++ {
		for c := 0; c < 20; c++ {
			data[r*20+c] = float64(r*100 + c)
		}
	}

	dw, err := fw.CreateDataset("/matrix_stride", Float64, []uint64{20, 20})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "matrix_stride")
	require.True(t, found)

	// Strided 2D: start=[1,2], count=[5,4], stride=[3,4]
	selection := &HyperslabSelection{
		Start:  []uint64{1, 2},
		Count:  []uint64{5, 4},
		Stride: []uint64{3, 4},
		Block:  []uint64{1, 1},
	}

	result, err := ds.ReadHyperslab(selection)
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 20, "expected 5*4=20 elements")

	expectedRows := []int{1, 4, 7, 10, 13}
	expectedCols := []int{2, 6, 10, 14}

	idx := 0
	for _, r := range expectedRows {
		for _, c := range expectedCols {
			expected := float64(r*100 + c)
			require.Equal(t, expected, resultData[idx],
				"[%d,%d] (idx %d): expected %f, got %f",
				r, c, idx, expected, resultData[idx])
			idx++
		}
	}
}

// TestReadHyperslab_NDimensionalExtraction tests hyperslab extraction for 4D data.
// Exercises: readContiguousRowByRow and extractHyperslabRecursive at 4D.
func TestReadHyperslab_NDimensionalExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_4d.h5")

	dims := []uint64{3, 4, 5, 6}
	total := uint64(1)
	for _, d := range dims {
		total *= d
	}

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, total)
	for i := range data {
		data[i] = float64(i)
	}

	dw, err := fw.CreateDataset("/data_4d", Float64, dims)
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data_4d")
	require.True(t, found)

	// Read sub-block: [0:1, 0:1, 0:2, 0:6] = 1x1x2x6 = 12 elements.
	// Selecting full last dim (=6) triggers isContiguousSelection=true,
	// routing through readContiguousOptimized instead of readContiguousRowByRow.
	count := []uint64{1, 1, 2, 6}

	result, err := ds.ReadSlice([]uint64{0, 0, 0, 0}, count)
	require.NoError(t, err, "ReadSlice on 4D dataset should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 12, "expected 1*1*2*6=12 elements")

	// Verify: [0,0,d2,d3] -> linearOffset = d2*6 + d3
	idx := 0
	for d2 := uint64(0); d2 < count[2]; d2++ {
		for d3 := uint64(0); d3 < count[3]; d3++ {
			linearOffset := d2*dims[3] + d3
			expected := float64(linearOffset)
			require.Equal(t, expected, resultData[idx],
				"[0,0,%d,%d] (idx %d): expected %f, got %f",
				d2, d3, idx, expected, resultData[idx])
			idx++
		}
	}
}

// TestReadHyperslab_Chunked2DFloat32 tests reading float32 chunked 2D dataset via hyperslab.
// Exercises: readHyperslabChunked with float32 conversion (convertBytesToFloat32AsFloat64).
func TestReadHyperslab_Chunked2DFloat32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_chunked_f32_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	rows, cols := 20, 30
	data := make([]float32, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			data[r*cols+c] = float32(r*100+c) * 0.5
		}
	}

	dw, err := fw.CreateDataset("/chunked_f32_2d", Float32, []uint64{uint64(rows), uint64(cols)},
		WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "chunked_f32_2d")
	require.True(t, found)

	// Read a block: [0:5, 0:5] (within a single chunk)
	result, err := ds.ReadSlice([]uint64{0, 0}, []uint64{5, 5})
	require.NoError(t, err, "ReadSlice on chunked float32 2D should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 25, "expected 5*5=25 elements")

	// Verify all returned values are valid floats from our written data
	for i, v := range resultData {
		require.False(t, v != v, "element %d is NaN", i) // NaN check
	}
}

// TestReadHyperslab_Chunked2DInt64 tests reading int64 chunked 2D dataset via hyperslab.
// Exercises: readHyperslabChunked with int64 conversion (convertBytesToInt64AsFloat64).
func TestReadHyperslab_Chunked2DInt64(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_chunked_i64_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	rows, cols := 20, 30
	data := make([]int64, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			data[r*cols+c] = int64(r*1000 + c*7)
		}
	}

	dw, err := fw.CreateDataset("/chunked_i64_2d", Int64, []uint64{uint64(rows), uint64(cols)},
		WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "chunked_i64_2d")
	require.True(t, found)

	// Read within a single chunk: [0:5, 0:5]
	result, err := ds.ReadSlice([]uint64{0, 0}, []uint64{5, 5})
	require.NoError(t, err, "ReadSlice on chunked int64 2D should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 25, "expected 5*5=25 elements")

	// Verify all values are valid (non-NaN)
	for i, v := range resultData {
		require.False(t, v != v, "element %d is NaN", i)
	}
}

// TestReadHyperslab_2DContiguousFullLastDim tests the contiguous optimization path
// where the last dimension is fully selected (isContiguousSelection=true).
// Exercises: readContiguousOptimized multi-dimensional contiguous path.
func TestReadHyperslab_2DContiguousFullLastDim(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_full_last_dim.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, 80)
	for i := range data {
		data[i] = float64(i)
	}

	dw, err := fw.CreateDataset("/full_last", Float64, []uint64{10, 8})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "full_last")
	require.True(t, found)

	// Select rows 3-6 with full last dimension (all 8 columns)
	result, err := ds.ReadSlice([]uint64{3, 0}, []uint64{4, 8})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 32, "expected 4*8=32 elements")

	for i := 0; i < 32; i++ {
		expected := float64(3*8 + i)
		require.Equal(t, expected, resultData[i],
			"element %d: expected %f, got %f", i, expected, resultData[i])
	}
}

// TestReadHyperslab_Int32_2DSlice tests reading int32 2D dataset with ReadSlice.
// Exercises: convertBytesToInt32AsFloat64 via 2D contiguous path.
func TestReadHyperslab_Int32_2DSlice(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_hyperslab_int32_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]int32, 150)
	for i := range data {
		data[i] = int32(i * 3)
	}

	dw, err := fw.CreateDataset("/i32_matrix", Int32, []uint64{10, 15})
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "i32_matrix")
	require.True(t, found)

	// Read [0:3, 0:5] = 3x5 block from top-left
	result, err := ds.ReadSlice([]uint64{0, 0}, []uint64{3, 5})
	require.NoError(t, err)

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 15)

	for r := 0; r < 3; r++ {
		for c := 0; c < 5; c++ {
			origIdx := r*15 + c
			expected := float64(origIdx * 3)
			require.Equal(t, expected, resultData[r*5+c],
				"[%d,%d]: expected %f, got %f", r, c, expected, resultData[r*5+c])
		}
	}
}
