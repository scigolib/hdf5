package hdf5

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestWriteVLenStrings tests writing variable-length strings.
func TestWriteVLenStrings(t *testing.T) {
	filename := "test_vlen_strings.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Create VLen string dataset
	ds, err := fw.CreateDataset("/strings", VLenString, []uint64{3})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write variable-length strings
	strings := []string{"short", "medium length string", "very long string with lots of text"}
	if err := ds.Write(strings); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close and reopen to verify structure
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify file can be opened
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Check dataset exists
	found := false
	for _, child := range f.Root().Children() {
		if child.Name() == "strings" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Dataset not found")
	}
}

// TestWriteRaggedArrayInt32 tests ragged arrays (different lengths).
func TestWriteRaggedArrayInt32(t *testing.T) {
	filename := "test_ragged_int32.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Create VLen int32 dataset
	ds, err := fw.CreateDataset("/ragged", VLenInt32, []uint64{3})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write ragged array (different lengths)
	ragged := [][]int32{{1, 2}, {3, 4, 5}, {6}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestWriteEmptyVLenSequences tests empty sequences (length 0).
func TestWriteEmptyVLenSequences(t *testing.T) {
	filename := "test_vlen_empty.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/empty_strings", VLenString, []uint64{3})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Write strings with empty ones
	strings := []string{"", "nonempty", ""}
	if err := ds.Write(strings); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestWriteLargeVLenStrings tests strings >1KB.
func TestWriteLargeVLenStrings(t *testing.T) {
	filename := "test_vlen_large.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/large_strings", VLenString, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Create large strings
	large1 := make([]byte, 2048)
	large2 := make([]byte, 4096)
	for i := range large1 {
		large1[i] = byte('A' + i%26)
	}
	for i := range large2 {
		large2[i] = byte('Z' - i%26)
	}

	strings := []string{string(large1), string(large2)}
	if err := ds.Write(strings); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestWriteVLenInt64 tests int64 ragged arrays.
func TestWriteVLenInt64(t *testing.T) {
	filename := "test_vlen_int64.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/int64", VLenInt64, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	ragged := [][]int64{{1, 2, 3}, {4, 5}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestWriteVLenFloat32 tests float32 ragged arrays.
func TestWriteVLenFloat32(t *testing.T) {
	filename := "test_vlen_float32.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/float32", VLenFloat32, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	ragged := [][]float32{{1.5, 2.5}, {3.5, 4.5, 5.5}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestWriteVLenFloat64 tests float64 ragged arrays.
func TestWriteVLenFloat64(t *testing.T) {
	filename := "test_vlen_float64.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/float64", VLenFloat64, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	ragged := [][]float64{{1.5, 2.5}, {3.5}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestVLenHeapIDStorage tests that heap IDs are correctly stored in dataset.
func TestVLenHeapIDStorage(t *testing.T) {
	filename := "test_vlen_heap_ids.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)

	ds, err := fw.CreateDataset("/strings", VLenString, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	strings := []string{"first", "second"}
	if err := ds.Write(strings); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Get data address
	dataAddr := ds.dataAddress

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read heap IDs from file
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Read 32 bytes (2 heap IDs × 16 bytes each)
	heapIDData := make([]byte, 32)
	if _, err := f.Reader().ReadAt(heapIDData, int64(dataAddr)); err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}

	// Verify heap IDs are non-zero
	heapAddr1 := binary.LittleEndian.Uint64(heapIDData[0:8])
	heapIdx1 := binary.LittleEndian.Uint32(heapIDData[8:12])

	if heapAddr1 == 0 {
		t.Error("First heap address is zero")
	}
	if heapIdx1 == 0 {
		t.Error("First heap index is zero")
	}

	heapAddr2 := binary.LittleEndian.Uint64(heapIDData[16:24])
	heapIdx2 := binary.LittleEndian.Uint32(heapIDData[24:28])

	if heapAddr2 == 0 {
		t.Error("Second heap address is zero")
	}
	if heapIdx2 == 0 {
		t.Error("Second heap index is zero")
	}

	// Verify data is in global heap
	ghc, err := core.ReadGlobalHeapCollection(f.Reader(), heapAddr1, 8)
	if err != nil {
		t.Fatalf("ReadGlobalHeapCollection failed: %v", err)
	}

	// Check first string
	obj1, err := ghc.GetObject(heapIdx1)
	if err != nil {
		t.Fatalf("GetObject(1) failed: %v", err)
	}
	if string(obj1.Data) != "first" {
		t.Errorf("First string mismatch: expected 'first', got '%s'", string(obj1.Data))
	}

	// Check second string
	obj2, err := ghc.GetObject(heapIdx2)
	if err != nil {
		t.Fatalf("GetObject(2) failed: %v", err)
	}
	if string(obj2.Data) != "second" {
		t.Errorf("Second string mismatch: expected 'second', got '%s'", string(obj2.Data))
	}
}

// TestVLenSizeMismatch tests error when data size doesn't match dataset dimensions.
func TestVLenSizeMismatch(t *testing.T) {
	filename := "test_vlen_size_mismatch.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/strings", VLenString, []uint64{3})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Try to write wrong number of elements
	strings := []string{"only", "two"}
	err = ds.Write(strings)
	if err == nil {
		t.Error("Expected error for size mismatch, got nil")
	}
}

// TestVLenUint32 tests uint32 ragged arrays.
func TestVLenUint32(t *testing.T) {
	filename := "test_vlen_uint32.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/uint32", VLenUint32, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	ragged := [][]uint32{{1, 2, 3}, {4}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestVLenUint64 tests uint64 ragged arrays.
func TestVLenUint64(t *testing.T) {
	filename := "test_vlen_uint64.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	ds, err := fw.CreateDataset("/uint64", VLenUint64, []uint64{2})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	ragged := [][]uint64{{1, 2}, {3, 4, 5}}
	if err := ds.Write(ragged); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}
