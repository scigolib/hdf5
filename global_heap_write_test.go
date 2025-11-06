package hdf5

import (
	"bytes"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestWriteToGlobalHeap tests writing a single object to global heap.
func TestWriteToGlobalHeap(t *testing.T) {
	// Create test file
	filename := "test_global_heap_single.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Write data to global heap
	data := []byte("Hello, HDF5!")
	heapID, err := fw.globalHeapWriter.WriteToGlobalHeap(data)
	if err != nil {
		t.Fatalf("WriteToGlobalHeap failed: %v", err)
	}

	// Verify heap ID
	if heapID.CollectionAddress == 0 {
		t.Error("Expected non-zero collection address")
	}
	if heapID.ObjectIndex != 1 {
		t.Errorf("Expected object index 1, got %d", heapID.ObjectIndex)
	}

	// Verify current heap state
	if fw.globalHeapWriter.currentHeap == nil {
		t.Fatal("Expected current heap to exist")
	}
	if len(fw.globalHeapWriter.currentHeap.objects) != 1 {
		t.Errorf("Expected 1 object in heap, got %d", len(fw.globalHeapWriter.currentHeap.objects))
	}

	// Close and flush
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back and verify
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Read global heap collection
	ghc, err := core.ReadGlobalHeapCollection(f.Reader(), heapID.CollectionAddress, 8)
	if err != nil {
		t.Fatalf("ReadGlobalHeapCollection failed: %v", err)
	}

	// Verify object
	obj, err := ghc.GetObject(uint32(heapID.ObjectIndex))
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if !bytes.Equal(obj.Data, data) {
		t.Errorf("Data mismatch: expected %q, got %q", string(data), string(obj.Data))
	}
}

// TestMultipleHeapObjects tests writing multiple objects to the same heap collection.
func TestMultipleHeapObjects(t *testing.T) {
	filename := "test_global_heap_multiple.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Write multiple objects
	objects := [][]byte{
		[]byte("First string"),
		[]byte("Second string is longer"),
		[]byte("Third"),
	}

	heapIDs := make([]HeapID, len(objects))
	for i, data := range objects {
		hid, err := fw.globalHeapWriter.WriteToGlobalHeap(data)
		if err != nil {
			t.Fatalf("WriteToGlobalHeap[%d] failed: %v", i, err)
		}
		heapIDs[i] = hid
	}

	// Verify all objects in same collection
	for i := 1; i < len(heapIDs); i++ {
		if heapIDs[i].CollectionAddress != heapIDs[0].CollectionAddress {
			t.Errorf("Object %d in different collection: %d vs %d",
				i, heapIDs[i].CollectionAddress, heapIDs[0].CollectionAddress)
		}
	}

	// Verify indices are sequential
	for i, hid := range heapIDs {
		expectedIndex := uint16(i + 1)
		if hid.ObjectIndex != expectedIndex {
			t.Errorf("Object %d: expected index %d, got %d", i, expectedIndex, hid.ObjectIndex)
		}
	}

	// Close and verify
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back and verify all objects
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	ghc, err := core.ReadGlobalHeapCollection(f.Reader(), heapIDs[0].CollectionAddress, 8)
	if err != nil {
		t.Fatalf("ReadGlobalHeapCollection failed: %v", err)
	}

	for i, hid := range heapIDs {
		obj, err := ghc.GetObject(uint32(hid.ObjectIndex))
		if err != nil {
			t.Fatalf("GetObject[%d] failed: %v", i, err)
		}

		if !bytes.Equal(obj.Data, objects[i]) {
			t.Errorf("Object[%d] data mismatch: expected %q, got %q",
				i, string(objects[i]), string(obj.Data))
		}
	}
}

// TestLargeHeapObject tests writing an object larger than the default heap size.
func TestLargeHeapObject(t *testing.T) {
	filename := "test_global_heap_large.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Create data larger than 4KB (default heap size)
	largeData := make([]byte, 8192)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	// Write large object
	heapID, err := fw.globalHeapWriter.WriteToGlobalHeap(largeData)
	if err != nil {
		t.Fatalf("WriteToGlobalHeap failed: %v", err)
	}

	// Close and verify
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back and verify
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	ghc, err := core.ReadGlobalHeapCollection(f.Reader(), heapID.CollectionAddress, 8)
	if err != nil {
		t.Fatalf("ReadGlobalHeapCollection failed: %v", err)
	}

	obj, err := ghc.GetObject(uint32(heapID.ObjectIndex))
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if len(obj.Data) != len(largeData) {
		t.Errorf("Data length mismatch: expected %d, got %d", len(largeData), len(obj.Data))
	}

	// Verify data content
	for i := range largeData {
		if obj.Data[i] != largeData[i] {
			t.Errorf("Data mismatch at byte %d: expected %d, got %d", i, largeData[i], obj.Data[i])
			break
		}
	}
}

// TestEmptyHeapObject tests writing an empty (zero-length) object.
func TestEmptyHeapObject(t *testing.T) {
	filename := "test_global_heap_empty.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Write empty data
	emptyData := []byte{}
	heapID, err := fw.globalHeapWriter.WriteToGlobalHeap(emptyData)
	if err != nil {
		t.Fatalf("WriteToGlobalHeap failed: %v", err)
	}

	// Verify heap ID
	if heapID.CollectionAddress == 0 {
		t.Error("Expected non-zero collection address")
	}

	// Close and verify
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back and verify
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	ghc, err := core.ReadGlobalHeapCollection(f.Reader(), heapID.CollectionAddress, 8)
	if err != nil {
		t.Fatalf("ReadGlobalHeapCollection failed: %v", err)
	}

	obj, err := ghc.GetObject(uint32(heapID.ObjectIndex))
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if len(obj.Data) != 0 {
		t.Errorf("Expected empty data, got %d bytes", len(obj.Data))
	}
}

// TestMultipleHeapCollections tests creating multiple heap collections when space runs out.
func TestMultipleHeapCollections(t *testing.T) {
	filename := "test_global_heap_multiple_collections.h5"
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer os.Remove(filename)
	defer fw.Close()

	// Set small minimum heap size for testing
	fw.globalHeapWriter.minCollectionSize = 512

	// Write objects until we trigger a new collection
	// Each object: 16 bytes header + data + padding
	// With 512 byte heap, we can fit ~10-12 small objects
	var heapIDs []HeapID
	for i := 0; i < 20; i++ {
		data := []byte("Test data for object")
		hid, err := fw.globalHeapWriter.WriteToGlobalHeap(data)
		if err != nil {
			t.Fatalf("WriteToGlobalHeap[%d] failed: %v", i, err)
		}
		heapIDs = append(heapIDs, hid)
	}

	// Verify we created multiple collections
	collections := make(map[uint64]bool)
	for _, hid := range heapIDs {
		collections[hid.CollectionAddress] = true
	}

	if len(collections) < 2 {
		t.Errorf("Expected multiple collections, got %d", len(collections))
	}

	// Close and verify all objects are readable
	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Verify each object
	for i, hid := range heapIDs {
		ghc, err := core.ReadGlobalHeapCollection(f.Reader(), hid.CollectionAddress, 8)
		if err != nil {
			t.Fatalf("ReadGlobalHeapCollection[%d] failed: %v", i, err)
		}

		obj, err := ghc.GetObject(uint32(hid.ObjectIndex))
		if err != nil {
			t.Fatalf("GetObject[%d] failed: %v", i, err)
		}

		expectedData := "Test data for object"
		if string(obj.Data) != expectedData {
			t.Errorf("Object[%d] data mismatch: expected %q, got %q",
				i, expectedData, string(obj.Data))
		}
	}
}

// TestHeapIDEncoding tests encoding heap IDs to 16-byte format.
func TestHeapIDEncoding(t *testing.T) {
	heapID := HeapID{
		CollectionAddress: 0x123456789ABCDEF0,
		ObjectIndex:       0x1234,
	}

	encoded := heapID.Encode()

	// Should be 16 bytes
	if len(encoded) != 16 {
		t.Errorf("Expected 16 bytes, got %d", len(encoded))
	}

	// First 8 bytes: address (little-endian)
	if encoded[0] != 0xF0 || encoded[7] != 0x12 {
		t.Errorf("Address encoding incorrect: %x", encoded[0:8])
	}

	// Next 4 bytes: index (little-endian)
	if encoded[8] != 0x34 || encoded[9] != 0x12 {
		t.Errorf("Index encoding incorrect: %x", encoded[8:12])
	}

	// Last 4 bytes: padding (zeros)
	for i := 12; i < 16; i++ {
		if encoded[i] != 0 {
			t.Errorf("Padding byte %d is not zero: %d", i, encoded[i])
		}
	}
}
