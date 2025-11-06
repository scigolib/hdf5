package hdf5

import (
	"path/filepath"
	"testing"
)

// findDatasetByName finds a dataset by name in the root group's children.
func findDatasetByName(f *File, name string) (*Dataset, bool) {
	for _, child := range f.Root().Children() {
		if child.Name() == name {
			if ds, ok := child.(*Dataset); ok {
				return ds, true
			}
		}
	}
	return nil, false
}

// TestReadSlice1D tests reading a 1D slice from a dataset.
//
//nolint:gocognit // Table-driven test with multiple subtests - acceptable complexity for tests
func TestReadSlice1D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_read_slice_1d.h5")

	// Create file with 1D dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	// Create 1D dataset with 100 elements
	dw, err := fw.CreateDataset("/data", Int32, []uint64{100})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write known data: [0, 1, 2, ..., 99]
	data := make([]int32, 100)
	for i := range data {
		data[i] = int32(i)
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Open for reading
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data")
	if !found {
		t.Fatal("Dataset 'data' not found")
	}

	// Test 1: Read slice [20:70] (50 elements)
	t.Run("middle slice", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{20}, []uint64{50})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		// Verify result type and length
		resultData, ok := result.([]float64)
		if !ok {
			t.Fatalf("Expected []float64, got %T", result)
		}

		if len(resultData) != 50 {
			t.Errorf("Expected 50 elements, got %d", len(resultData))
		}

		// Verify values
		for i := 0; i < 50; i++ {
			expected := float64(20 + i)
			if resultData[i] != expected {
				t.Errorf("Element %d: expected %f, got %f", i, expected, resultData[i])
			}
		}
	})

	// Test 2: Read from beginning [0:10]
	t.Run("start slice", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{0}, []uint64{10})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 10 {
			t.Errorf("Expected 10 elements, got %d", len(resultData))
		}

		for i := 0; i < 10; i++ {
			expected := float64(i)
			if resultData[i] != expected {
				t.Errorf("Element %d: expected %f, got %f", i, expected, resultData[i])
			}
		}
	})

	// Test 3: Read to end [90:100]
	t.Run("end slice", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{90}, []uint64{10})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 10 {
			t.Errorf("Expected 10 elements, got %d", len(resultData))
		}

		for i := 0; i < 10; i++ {
			expected := float64(90 + i)
			if resultData[i] != expected {
				t.Errorf("Element %d: expected %f, got %f", i, expected, resultData[i])
			}
		}
	})

	// Test 4: Read single element [50:51]
	t.Run("single element", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{50}, []uint64{1})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 1 {
			t.Errorf("Expected 1 element, got %d", len(resultData))
		}

		if resultData[0] != 50.0 {
			t.Errorf("Expected 50.0, got %f", resultData[0])
		}
	})
}

// TestReadSlice2D tests reading a 2D slice from a dataset.
//
//nolint:gocognit // Table-driven test with multiple subtests - acceptable complexity for tests
func TestReadSlice2D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_read_slice_2d.h5")

	// Create file with 2D dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	// Create 2D dataset 100x100
	dw, err := fw.CreateDataset("/matrix", Int32, []uint64{100, 100})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write known data: value = row*100 + col
	data := make([]int32, 100*100)
	for row := 0; row < 100; row++ {
		for col := 0; col < 100; col++ {
			data[row*100+col] = int32(row*100 + col)
		}
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Open for reading
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "matrix")
	if !found {
		t.Fatal("Dataset 'matrix' not found")
	}

	// Test 1: Read slice [20:70, 30:80] (50x50 block)
	t.Run("middle block", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{20, 30}, []uint64{50, 50})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData, ok := result.([]float64)
		if !ok {
			t.Fatalf("Expected []float64, got %T", result)
		}

		// Verify shape: 50x50 = 2500 elements
		if len(resultData) != 2500 {
			t.Errorf("Expected 2500 elements (50x50), got %d", len(resultData))
		}

		// Verify values: value = (row+20)*100 + (col+30)
		for row := 0; row < 50; row++ {
			for col := 0; col < 50; col++ {
				idx := row*50 + col
				expected := float64((row+20)*100 + (col + 30))
				if resultData[idx] != expected {
					t.Errorf("Element [%d,%d]: expected %f, got %f",
						row, col, expected, resultData[idx])
				}
			}
		}
	})

	// Test 2: Read first 10x10 block
	t.Run("corner block", func(t *testing.T) {
		result, err := ds.ReadSlice([]uint64{0, 0}, []uint64{10, 10})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 100 {
			t.Errorf("Expected 100 elements (10x10), got %d", len(resultData))
		}

		// Verify values
		for row := 0; row < 10; row++ {
			for col := 0; col < 10; col++ {
				idx := row*10 + col
				expected := float64(row*100 + col)
				if resultData[idx] != expected {
					t.Errorf("Element [%d,%d]: expected %f, got %f",
						row, col, expected, resultData[idx])
				}
			}
		}
	})

	// Test 3: Read single row (slice of columns)
	t.Run("single row", func(t *testing.T) {
		// Read row 50, columns [25:75]
		result, err := ds.ReadSlice([]uint64{50, 25}, []uint64{1, 50})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 50 {
			t.Errorf("Expected 50 elements, got %d", len(resultData))
		}

		// Verify values: row 50, columns 25-74
		for col := 0; col < 50; col++ {
			expected := float64(50*100 + (25 + col))
			if resultData[col] != expected {
				t.Errorf("Element [50,%d]: expected %f, got %f",
					25+col, expected, resultData[col])
			}
		}
	})

	// Test 4: Read single column (slice of rows)
	t.Run("single column", func(t *testing.T) {
		// Read column 75, rows [10:20]
		result, err := ds.ReadSlice([]uint64{10, 75}, []uint64{10, 1})
		if err != nil {
			t.Fatalf("ReadSlice failed: %v", err)
		}

		resultData := result.([]float64)
		if len(resultData) != 10 {
			t.Errorf("Expected 10 elements, got %d", len(resultData))
		}

		// Verify values: rows 10-19, column 75
		for row := 0; row < 10; row++ {
			expected := float64((10+row)*100 + 75)
			if resultData[row] != expected {
				t.Errorf("Element [%d,75]: expected %f, got %f",
					10+row, expected, resultData[row])
			}
		}
	})
}

// TestReadHyperslabWithStride tests reading with stride parameter.
//
//nolint:gocognit // Table-driven test with multiple subtests - acceptable complexity for tests
func TestReadHyperslabWithStride(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_read_hyperslab_stride.h5")

	// Create file with 2D dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	// Create 2D dataset 100x100
	dw, err := fw.CreateDataset("/data", Int32, []uint64{100, 100})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write known data: value = row*100 + col
	data := make([]int32, 100*100)
	for row := 0; row < 100; row++ {
		for col := 0; col < 100; col++ {
			data[row*100+col] = int32(row*100 + col)
		}
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Open for reading
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data")
	if !found {
		t.Fatal("Dataset 'data' not found")
	}

	// Test 1: Read every 2nd element (stride=2x2)
	t.Run("stride 2x2", func(t *testing.T) {
		// Start at [0,0], read 25x25 blocks with stride [2,2]
		// This reads elements at [0,0], [0,2], [0,4], ..., [2,0], [2,2], etc.
		selection := &HyperslabSelection{
			Start:  []uint64{0, 0},
			Count:  []uint64{25, 25}, // 25 blocks in each dimension
			Stride: []uint64{2, 2},   // Every 2nd element
			Block:  []uint64{1, 1},   // 1x1 blocks
		}

		result, err := ds.ReadHyperslab(selection)
		if err != nil {
			t.Fatalf("ReadHyperslab failed: %v", err)
		}

		resultData, ok := result.([]float64)
		if !ok {
			t.Fatalf("Expected []float64, got %T", result)
		}

		// Verify size: 25*25 = 625 elements
		if len(resultData) != 625 {
			t.Errorf("Expected 625 elements (25x25), got %d", len(resultData))
		}

		// Verify values: should get elements at even row/col indices
		idx := 0
		for rowCount := 0; rowCount < 25; rowCount++ {
			for colCount := 0; colCount < 25; colCount++ {
				actualRow := rowCount * 2
				actualCol := colCount * 2
				expected := float64(actualRow*100 + actualCol)

				if resultData[idx] != expected {
					t.Errorf("Element [%d,%d] (index %d): expected %f, got %f",
						actualRow, actualCol, idx, expected, resultData[idx])
				}
				idx++
			}
		}
	})

	// Test 2: Read every 3rd element starting at offset
	t.Run("stride 3x3 with offset", func(t *testing.T) {
		// Start at [10,10], read 20x20 blocks with stride [3,3]
		selection := &HyperslabSelection{
			Start:  []uint64{10, 10},
			Count:  []uint64{20, 20},
			Stride: []uint64{3, 3},
			Block:  []uint64{1, 1},
		}

		result, err := ds.ReadHyperslab(selection)
		if err != nil {
			t.Fatalf("ReadHyperslab failed: %v", err)
		}

		resultData := result.([]float64)

		// Verify size: 20*20 = 400 elements
		if len(resultData) != 400 {
			t.Errorf("Expected 400 elements (20x20), got %d", len(resultData))
		}

		// Verify values
		idx := 0
		for rowCount := 0; rowCount < 20; rowCount++ {
			for colCount := 0; colCount < 20; colCount++ {
				actualRow := 10 + rowCount*3
				actualCol := 10 + colCount*3
				expected := float64(actualRow*100 + actualCol)

				if resultData[idx] != expected {
					t.Errorf("Element [%d,%d] (index %d): expected %f, got %f",
						actualRow, actualCol, idx, expected, resultData[idx])
				}
				idx++
			}
		}
	})

	// Test 3: Read with larger stride
	t.Run("stride 5x5", func(t *testing.T) {
		// Start at [0,0], read 10x10 with stride [5,5] and block [1,1]
		// This reads elements at [0,0], [0,5], [0,10], ..., [5,0], etc.
		selection := &HyperslabSelection{
			Start:  []uint64{0, 0},
			Count:  []uint64{10, 10}, // 10 elements per dimension
			Stride: []uint64{5, 5},   // Every 5th element
			Block:  []uint64{1, 1},   // Single elements
		}

		result, err := ds.ReadHyperslab(selection)
		if err != nil {
			t.Fatalf("ReadHyperslab failed: %v", err)
		}

		resultData := result.([]float64)

		// Verify size: 10*10 = 100 elements
		if len(resultData) != 100 {
			t.Errorf("Expected 100 elements (10x10), got %d", len(resultData))
		}

		// Verify first few elements
		// [0,0]=0, [0,5]=5, [0,10]=10, ..., [5,0]=500
		expectedFirst := []float64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45} // First row
		for i := 0; i < 10; i++ {
			if resultData[i] != expectedFirst[i] {
				t.Errorf("First row element %d: expected %f, got %f",
					i, expectedFirst[i], resultData[i])
			}
		}

		// Verify element at output position [5,5] (index 5*10+5=55)
		// With stride 5, this corresponds to input position [25,25]
		// Value at [25,25] = 25*100 + 25 = 2525
		expectedAt55 := float64(25*100 + 25) // = 2525
		if resultData[55] != expectedAt55 {
			t.Errorf("Element [5,5] (output index 55): expected %f, got %f",
				expectedAt55, resultData[55])
		}
	})

	// Test 4: Mixed stride in 2D (different strides per dimension)
	t.Run("asymmetric stride", func(t *testing.T) {
		// Start at [0,0], stride [2,3] - every 2nd row, every 3rd column
		selection := &HyperslabSelection{
			Start:  []uint64{0, 0},
			Count:  []uint64{20, 15}, // 20 rows, 15 columns
			Stride: []uint64{2, 3},   // Every 2nd row, every 3rd col
			Block:  []uint64{1, 1},
		}

		result, err := ds.ReadHyperslab(selection)
		if err != nil {
			t.Fatalf("ReadHyperslab failed: %v", err)
		}

		resultData := result.([]float64)

		// Verify size: 20*15 = 300 elements
		if len(resultData) != 300 {
			t.Errorf("Expected 300 elements (20x15), got %d", len(resultData))
		}

		// Verify first row: rows at stride 2, cols at stride 3
		// Row 0, columns 0,3,6,9,12,15,18,...
		expectedFirstRow := []float64{0, 3, 6, 9, 12, 15, 18, 21, 24, 27, 30, 33, 36, 39, 42}
		for i := 0; i < 15; i++ {
			if resultData[i] != expectedFirstRow[i] {
				t.Errorf("First row element %d: expected %f, got %f",
					i, expectedFirstRow[i], resultData[i])
			}
		}

		// Verify second output row (actual row 2, since stride=2)
		// Row 2, columns 0,3,6,9,12,15,18,...
		// Values: 200,203,206,209,...
		for i := 0; i < 15; i++ {
			expected := float64(2*100 + i*3)
			if resultData[15+i] != expected {
				t.Errorf("Second row element %d: expected %f, got %f",
					i, expected, resultData[15+i])
			}
		}
	})
}

// TestReadHyperslabOutOfBounds tests error handling for out-of-bounds selection.
//
//nolint:gocognit // Table-driven test with many error cases - acceptable complexity for tests
func TestReadHyperslabOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_read_hyperslab_bounds.h5")

	// Create file with 2D dataset 100x100
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	dw, err := fw.CreateDataset("/data", Int32, []uint64{100, 100})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write some data
	data := make([]int32, 100*100)
	for i := range data {
		data[i] = int32(i)
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Open for reading
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "data")
	if !found {
		t.Fatal("Dataset 'data' not found")
	}

	// Test 1: Start beyond bounds
	t.Run("start beyond bounds", func(t *testing.T) {
		_, err := ds.ReadSlice([]uint64{150, 0}, []uint64{10, 10})
		if err == nil {
			t.Error("Expected error for start beyond bounds, got nil")
		}
	})

	// Test 2: Count exceeds bounds
	t.Run("count exceeds bounds", func(t *testing.T) {
		// Start at [90, 90], try to read [20, 20] - would go to [110, 110]
		_, err := ds.ReadSlice([]uint64{90, 90}, []uint64{20, 20})
		if err == nil {
			t.Error("Expected error for count exceeding bounds, got nil")
		}
	})

	// Test 3: Dimension mismatch - start
	t.Run("dimension mismatch start", func(t *testing.T) {
		// 2D dataset, but 1D start/count
		_, err := ds.ReadSlice([]uint64{50}, []uint64{10})
		if err == nil {
			t.Error("Expected error for dimension mismatch, got nil")
		}
	})

	// Test 4: Dimension mismatch - count
	t.Run("dimension mismatch count", func(t *testing.T) {
		// 2D dataset, but 3D start/count
		_, err := ds.ReadSlice([]uint64{0, 0, 0}, []uint64{10, 10, 10})
		if err == nil {
			t.Error("Expected error for dimension mismatch, got nil")
		}
	})

	// Test 5: Zero count (invalid)
	t.Run("zero count", func(t *testing.T) {
		selection := &HyperslabSelection{
			Start: []uint64{0, 0},
			Count: []uint64{0, 10}, // Zero count in first dimension
		}
		_, err := ds.ReadHyperslab(selection)
		if err == nil {
			t.Error("Expected error for zero count, got nil")
		}
	})

	// Test 6: Zero stride (invalid)
	t.Run("zero stride", func(t *testing.T) {
		selection := &HyperslabSelection{
			Start:  []uint64{0, 0},
			Count:  []uint64{10, 10},
			Stride: []uint64{0, 1}, // Zero stride in first dimension
		}
		_, err := ds.ReadHyperslab(selection)
		if err == nil {
			t.Error("Expected error for zero stride, got nil")
		}
	})

	// Test 7: Zero block (invalid)
	t.Run("zero block", func(t *testing.T) {
		selection := &HyperslabSelection{
			Start: []uint64{0, 0},
			Count: []uint64{10, 10},
			Block: []uint64{0, 1}, // Zero block in first dimension
		}
		_, err := ds.ReadHyperslab(selection)
		if err == nil {
			t.Error("Expected error for zero block, got nil")
		}
	})

	// Test 8: Strided selection exceeds bounds
	t.Run("strided selection exceeds bounds", func(t *testing.T) {
		// Start at [50, 50], stride [10, 10], count [10, 10]
		// Last element would be at [50 + 9*10, 50 + 9*10] = [140, 140] - out of bounds
		selection := &HyperslabSelection{
			Start:  []uint64{50, 50},
			Count:  []uint64{10, 10},
			Stride: []uint64{10, 10},
			Block:  []uint64{1, 1},
		}
		_, err := ds.ReadHyperslab(selection)
		if err == nil {
			t.Error("Expected error for strided selection exceeding bounds, got nil")
		}
	})

	// Test 9: Block selection exceeds bounds
	t.Run("block selection exceeds bounds", func(t *testing.T) {
		// Start at [95, 95], block [10, 10] - would need to read to [105, 105]
		selection := &HyperslabSelection{
			Start:  []uint64{95, 95},
			Count:  []uint64{1, 1},
			Stride: []uint64{1, 1},
			Block:  []uint64{10, 10}, // Block extends beyond bounds
		}
		_, err := ds.ReadHyperslab(selection)
		if err == nil {
			t.Error("Expected error for block selection exceeding bounds, got nil")
		}
	})

	// Test 10: Verify error messages contain useful information
	t.Run("error message content", func(t *testing.T) {
		_, err := ds.ReadSlice([]uint64{200, 0}, []uint64{10, 10})
		if err == nil {
			t.Fatal("Expected error, got nil")
		}

		// Error message should mention dimension or bounds
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("Expected non-empty error message")
		}
		// Just verify we got an error with some content
		// Specific wording may vary based on implementation
	})
}

// TestValidateHyperslabSelection tests selection validation logic.
func TestValidateHyperslabSelection(t *testing.T) {
	tests := []struct {
		name    string
		sel     *HyperslabSelection
		dims    []uint64
		wantErr bool
	}{
		{
			name: "valid simple selection",
			sel: &HyperslabSelection{
				Start: []uint64{0, 0},
				Count: []uint64{10, 10},
			},
			dims:    []uint64{100, 100},
			wantErr: false,
		},
		{
			name: "valid with stride and block",
			sel: &HyperslabSelection{
				Start:  []uint64{0, 0},
				Count:  []uint64{10, 10},
				Stride: []uint64{2, 2},
				Block:  []uint64{1, 1},
			},
			dims:    []uint64{100, 100},
			wantErr: false,
		},
		{
			name: "dimension mismatch - start",
			sel: &HyperslabSelection{
				Start: []uint64{0},
				Count: []uint64{10, 10},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "dimension mismatch - count",
			sel: &HyperslabSelection{
				Start: []uint64{0, 0},
				Count: []uint64{10},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "out of bounds",
			sel: &HyperslabSelection{
				Start: []uint64{90, 90},
				Count: []uint64{20, 20},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "zero count",
			sel: &HyperslabSelection{
				Start: []uint64{0, 0},
				Count: []uint64{0, 10},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "zero stride",
			sel: &HyperslabSelection{
				Start:  []uint64{0, 0},
				Count:  []uint64{10, 10},
				Stride: []uint64{0, 1},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "zero block",
			sel: &HyperslabSelection{
				Start: []uint64{0, 0},
				Count: []uint64{10, 10},
				Block: []uint64{0, 1},
			},
			dims:    []uint64{100, 100},
			wantErr: true,
		},
		{
			name: "1D selection valid",
			sel: &HyperslabSelection{
				Start: []uint64{10},
				Count: []uint64{50},
			},
			dims:    []uint64{100},
			wantErr: false,
		},
		{
			name: "3D selection valid",
			sel: &HyperslabSelection{
				Start: []uint64{0, 0, 0},
				Count: []uint64{10, 10, 10},
			},
			dims:    []uint64{100, 100, 100},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHyperslabSelection(tt.sel, tt.dims)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHyperslabSelection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCalculateHyperslabOutputSize tests output size calculation.
func TestCalculateHyperslabOutputSize(t *testing.T) {
	tests := []struct {
		name string
		sel  *HyperslabSelection
		want uint64
	}{
		{
			name: "simple 1D",
			sel: &HyperslabSelection{
				Count: []uint64{50},
				Block: []uint64{1},
			},
			want: 50,
		},
		{
			name: "simple 2D",
			sel: &HyperslabSelection{
				Count: []uint64{10, 20},
				Block: []uint64{1, 1},
			},
			want: 200,
		},
		{
			name: "with block size",
			sel: &HyperslabSelection{
				Count: []uint64{10, 10},
				Block: []uint64{2, 2},
			},
			want: 400, // 10*2 * 10*2 = 20*20 = 400
		},
		{
			name: "3D",
			sel: &HyperslabSelection{
				Count: []uint64{5, 10, 20},
				Block: []uint64{1, 1, 1},
			},
			want: 1000,
		},
		{
			name: "empty selection",
			sel: &HyperslabSelection{
				Count: []uint64{},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateHyperslabOutputSize(tt.sel)
			if got != tt.want {
				t.Errorf("calculateHyperslabOutputSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCalculateLinearOffset tests N-dimensional coordinate to linear offset conversion.
func TestCalculateLinearOffset(t *testing.T) {
	tests := []struct {
		name   string
		coords []uint64
		dims   []uint64
		want   uint64
	}{
		{
			name:   "1D origin",
			coords: []uint64{0},
			dims:   []uint64{100},
			want:   0,
		},
		{
			name:   "1D middle",
			coords: []uint64{50},
			dims:   []uint64{100},
			want:   50,
		},
		{
			name:   "2D origin",
			coords: []uint64{0, 0},
			dims:   []uint64{10, 20},
			want:   0,
		},
		{
			name:   "2D first row",
			coords: []uint64{0, 5},
			dims:   []uint64{10, 20},
			want:   5,
		},
		{
			name:   "2D second row start",
			coords: []uint64{1, 0},
			dims:   []uint64{10, 20},
			want:   20, // Row 1 starts at offset 20 (row-major)
		},
		{
			name:   "2D middle",
			coords: []uint64{2, 3},
			dims:   []uint64{10, 20},
			want:   43, // 2*20 + 3 = 43
		},
		{
			name:   "3D origin",
			coords: []uint64{0, 0, 0},
			dims:   []uint64{5, 10, 20},
			want:   0,
		},
		{
			name:   "3D middle",
			coords: []uint64{1, 2, 3},
			dims:   []uint64{5, 10, 20},
			want:   243, // 1*10*20 + 2*20 + 3 = 200 + 40 + 3 = 243
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateLinearOffset(tt.coords, tt.dims)
			if got != tt.want {
				t.Errorf("calculateLinearOffset() = %v, want %v", got, tt.want)
			}
		})
	}
}
