package hdf5

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// createChunkedTestFile creates a test file with a chunked dataset.
func createChunkedTestFile(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_test.h5")

	// Create file with chunked dataset.
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	// Create chunked dataset: 100x100 with 10x10 chunks = 100 chunks total.
	dw, err := fw.CreateDataset("/chunked_data", Float64, []uint64{100, 100},
		WithChunkDims([]uint64{10, 10}),
	)
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write data.
	data := make([]float64, 100*100)
	for i := range data {
		data[i] = float64(i)
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	return filename
}

// findFirstDataset looks for any dataset in the file.
func findFirstDataset(f *File) *Dataset {
	var result *Dataset
	f.Walk(func(_ string, obj Object) {
		if result != nil {
			return
		}
		if ds, ok := obj.(*Dataset); ok {
			result = ds
		}
	})
	return result
}

// TestChunkIteratorBasic tests basic chunk iteration.
func TestChunkIteratorBasic(t *testing.T) {
	// Create test file with chunked dataset.
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Get dataset directly by finding it.
	var ds *Dataset
	file.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && ds == nil {
			ds = d
		}
	})
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	// Create iterator.
	iter, err := ds.ChunkIterator()
	if err != nil {
		t.Fatalf("ChunkIterator failed: %v", err)
	}

	// Count chunks.
	chunkCount := 0
	for iter.Next() {
		chunk, err := iter.Chunk()
		if err != nil {
			t.Fatalf("Chunk() failed at index %d: %v", chunkCount, err)
		}

		// Verify chunk is not empty.
		if chunk == nil {
			t.Fatalf("Chunk %d returned nil data", chunkCount)
		}

		chunkCount++
	}

	if err := iter.Err(); err != nil {
		t.Fatalf("Iterator error: %v", err)
	}

	if chunkCount == 0 {
		t.Error("Expected at least one chunk")
	}

	// Verify Total() matches.
	if iter.Total() != chunkCount {
		t.Errorf("Total() = %d, but iterated %d chunks", iter.Total(), chunkCount)
	}
}

// TestChunkIteratorProgress tests progress callback.
func TestChunkIteratorProgress(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	iter, err := ds.ChunkIterator()
	if err != nil {
		t.Fatalf("ChunkIterator failed: %v", err)
	}

	// Track progress calls.
	progressCalls := 0
	lastCurrent := 0
	iter.OnProgress(func(current, total int) {
		progressCalls++
		if current <= lastCurrent {
			t.Errorf("Progress went backwards: %d -> %d", lastCurrent, current)
		}
		lastCurrent = current
		if current > total {
			t.Errorf("Current %d exceeds total %d", current, total)
		}
	})

	for iter.Next() {
		_, _ = iter.Chunk()
	}

	if progressCalls != iter.Total() {
		t.Errorf("Expected %d progress calls, got %d", iter.Total(), progressCalls)
	}
}

// TestChunkIteratorContextCancellation tests context cancellation.
func TestChunkIteratorContextCancellation(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	// Create canceled context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	iter, err := ds.ChunkIteratorWithContext(ctx)
	if err != nil {
		t.Fatalf("ChunkIteratorWithContext failed: %v", err)
	}

	// Process first chunk, then cancel.
	chunksProcessed := 0
	for iter.Next() {
		_, _ = iter.Chunk()
		chunksProcessed++
		if chunksProcessed == 1 {
			cancel() // Cancel after first chunk.
		}
	}

	// Should have stopped early if there were multiple chunks.
	if iter.Total() > 1 && chunksProcessed >= iter.Total() {
		t.Errorf("Expected early stop, but processed all %d chunks", chunksProcessed)
	}

	// Error should be context.Canceled (if we stopped early).
	if iter.Total() > 1 && !errors.Is(iter.Err(), context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", iter.Err())
	}
}

// TestChunkIteratorReset tests reset functionality.
func TestChunkIteratorReset(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	iter, err := ds.ChunkIterator()
	if err != nil {
		t.Fatalf("ChunkIterator failed: %v", err)
	}

	// First pass.
	pass1Count := 0
	for iter.Next() {
		_, _ = iter.Chunk()
		pass1Count++
	}

	// Reset and iterate again.
	iter.Reset()

	pass2Count := 0
	for iter.Next() {
		_, _ = iter.Chunk()
		pass2Count++
	}

	if pass1Count != pass2Count {
		t.Errorf("Pass counts differ: %d vs %d", pass1Count, pass2Count)
	}
}

// TestChunkIteratorNonChunked tests error for non-chunked dataset.
func TestChunkIteratorNonChunked(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "contiguous_test.h5")

	// Create file with contiguous (non-chunked) dataset.
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	// Create contiguous dataset (no WithChunks option).
	dw, err := fw.CreateDataset("/contiguous_data", Float64, []uint64{100})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i)
	}
	if err := dw.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	file, err := Open(filename)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Find the dataset.
	var ds *Dataset
	file.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && ds == nil {
			ds = d
		}
	})

	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	// ChunkIterator should fail for non-chunked dataset.
	_, err = ds.ChunkIterator()
	if err == nil {
		t.Error("Expected error for non-chunked dataset, got nil")
	}
}

// TestChunkIteratorChunkCoords tests ChunkCoords method.
func TestChunkIteratorChunkCoords(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	iter, err := ds.ChunkIterator()
	if err != nil {
		t.Fatalf("ChunkIterator failed: %v", err)
	}

	// Before Next(), ChunkCoords should be nil.
	if coords := iter.ChunkCoords(); coords != nil {
		t.Errorf("ChunkCoords before Next() should be nil, got %v", coords)
	}

	if iter.Next() {
		coords := iter.ChunkCoords()
		if coords == nil {
			t.Error("ChunkCoords returned nil after Next()")
		}

		// Verify coordinates are reasonable (non-negative).
		for i, c := range coords {
			if c > 1000000 { // Sanity check.
				t.Errorf("Coordinate %d seems unreasonable: %d", i, c)
			}
		}
	}
}

// TestChunkIteratorTimeout tests context timeout.
func TestChunkIteratorTimeout(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	// Create context with very short timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait a bit for timeout.
	time.Sleep(10 * time.Millisecond)

	iter, err := ds.ChunkIteratorWithContext(ctx)
	if err != nil {
		t.Fatalf("ChunkIteratorWithContext failed: %v", err)
	}

	// Next() should return false due to context deadline.
	if iter.Next() {
		t.Error("Expected Next() to return false due to timeout")
	}

	if !errors.Is(iter.Err(), context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded, got: %v", iter.Err())
	}
}

// TestChunkIteratorDims tests ChunkDims and DatasetDims methods.
func TestChunkIteratorDims(t *testing.T) {
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	ds := findFirstDataset(file)
	if ds == nil {
		t.Fatal("No dataset found in file")
	}

	iter, err := ds.ChunkIterator()
	if err != nil {
		t.Fatalf("ChunkIterator failed: %v", err)
	}

	// Check dimensions are returned.
	chunkDims := iter.ChunkDims()
	if len(chunkDims) == 0 {
		t.Error("ChunkDims returned empty slice")
	}

	datasetDims := iter.DatasetDims()
	if len(datasetDims) == 0 {
		t.Error("DatasetDims returned empty slice")
	}

	// Dimensions should match.
	if len(chunkDims) != len(datasetDims) {
		t.Errorf("ChunkDims (%d) and DatasetDims (%d) have different lengths",
			len(chunkDims), len(datasetDims))
	}

	// Dataset dims should be >= chunk dims in each dimension.
	for i := range chunkDims {
		if datasetDims[i] < chunkDims[i] && datasetDims[i] > 0 {
			// This is actually valid for partial chunks, so just log.
			t.Logf("DatasetDim[%d]=%d < ChunkDim[%d]=%d (partial chunk)", i, datasetDims[i], i, chunkDims[i])
		}
	}
}
