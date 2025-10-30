// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// MockWriter implements the Writer interface for testing.
type MockWriter struct {
	data map[uint64][]byte
}

func NewMockWriter() *MockWriter {
	return &MockWriter{
		data: make(map[uint64][]byte),
	}
}

func (m *MockWriter) WriteAtAddress(data []byte, address uint64) error {
	m.data[address] = make([]byte, len(data))
	copy(m.data[address], data)
	return nil
}

func (m *MockWriter) ReadAt(p []byte, off int64) (int, error) {
	data, ok := m.data[uint64(off)]
	if !ok {
		return 0, fmt.Errorf("no data at address 0x%X", off)
	}
	copy(p, data)
	return len(data), nil
}

// MockAllocator implements the Allocator interface for testing.
type MockAllocator struct {
	nextAddress uint64
}

func NewMockAllocator(startAddress uint64) *MockAllocator {
	return &MockAllocator{nextAddress: startAddress}
}

func (m *MockAllocator) Allocate(size uint64) (uint64, error) {
	addr := m.nextAddress
	m.nextAddress += size
	// Align to 8-byte boundary
	if m.nextAddress%8 != 0 {
		m.nextAddress = ((m.nextAddress / 8) + 1) * 8
	}
	return addr, nil
}

func TestNewWritableFractalHeap(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Check heap header initialization
	if heap.Header.Version != 0 {
		t.Errorf("Expected version 0, got %d", heap.Header.Version)
	}

	if heap.Header.HeapIDLength != DefaultHeapIDLength {
		t.Errorf("Expected heap ID length %d, got %d", DefaultHeapIDLength, heap.Header.HeapIDLength)
	}

	if heap.Header.MaxManagedObjectSize != DefaultMaxManagedObjectSize {
		t.Errorf("Expected max object size %d, got %d", DefaultMaxManagedObjectSize, heap.Header.MaxManagedObjectSize)
	}

	if heap.Header.FreeSpace != DefaultStartingBlockSize {
		t.Errorf("Expected free space %d, got %d", DefaultStartingBlockSize, heap.Header.FreeSpace)
	}

	if heap.Header.NumManagedObjects != 0 {
		t.Errorf("Expected 0 objects, got %d", heap.Header.NumManagedObjects)
	}

	// Check direct block initialization
	if heap.DirectBlock.Version != 0 {
		t.Errorf("Expected direct block version 0, got %d", heap.DirectBlock.Version)
	}

	if heap.DirectBlock.Size != DefaultStartingBlockSize {
		t.Errorf("Expected block size %d, got %d", DefaultStartingBlockSize, heap.DirectBlock.Size)
	}

	if heap.DirectBlock.FreeOffset != 0 {
		t.Errorf("Expected free offset 0, got %d", heap.DirectBlock.FreeOffset)
	}
}

func TestFractalHeapInsertObject(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		expectErr bool
		errType   error
	}{
		{
			name:      "insert small object",
			data:      []byte("dataset1"),
			expectErr: false,
		},
		{
			name:      "insert medium object",
			data:      bytes.Repeat([]byte("x"), 1024),
			expectErr: false,
		},
		{
			name:      "insert empty object",
			data:      []byte{},
			expectErr: true,
			errType:   ErrEmptyObject,
		},
		{
			name:      "insert too large object",
			data:      bytes.Repeat([]byte("x"), int(DefaultMaxManagedObjectSize)+1),
			expectErr: true,
			errType:   ErrObjectTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewWritableFractalHeap(DefaultStartingBlockSize)

			heapID, err := heap.InsertObject(tt.data)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify heap ID format
			if len(heapID) != int(heap.Header.HeapIDLength) {
				t.Errorf("Expected heap ID length %d, got %d", heap.Header.HeapIDLength, len(heapID))
			}

			// Verify first byte is flags (version 0, type managed)
			if heapID[0] != 0x00 {
				t.Errorf("Expected flags byte 0x00, got 0x%02X", heapID[0])
			}

			// Verify statistics updated
			if heap.Header.NumManagedObjects != 1 {
				t.Errorf("Expected 1 object, got %d", heap.Header.NumManagedObjects)
			}

			expectedFree := DefaultStartingBlockSize - uint64(len(tt.data))
			if heap.Header.FreeSpace != expectedFree {
				t.Errorf("Expected free space %d, got %d", expectedFree, heap.Header.FreeSpace)
			}
		})
	}
}

func TestFractalHeapMultipleInserts(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Test data for multiple link names
	linkNames := []string{
		"dataset1",
		"dataset2",
		"group1",
		"group2",
		"attribute_with_longer_name",
		"another_dataset",
		"metadata",
		"index",
		"table",
		"results",
	}

	heapIDs := make([][]byte, 0, len(linkNames))

	// Insert all objects
	for i, name := range linkNames {
		heapID, err := heap.InsertObject([]byte(name))
		if err != nil {
			t.Fatalf("Failed to insert object %d (%s): %v", i, name, err)
		}
		heapIDs = append(heapIDs, heapID)
	}

	// Verify statistics
	if heap.Header.NumManagedObjects != uint64(len(linkNames)) {
		t.Errorf("Expected %d objects, got %d", len(linkNames), heap.Header.NumManagedObjects)
	}

	// Calculate total size
	totalSize := 0
	for _, name := range linkNames {
		totalSize += len(name)
	}

	expectedFree := DefaultStartingBlockSize - uint64(totalSize)
	if heap.Header.FreeSpace != expectedFree {
		t.Errorf("Expected free space %d, got %d", expectedFree, heap.Header.FreeSpace)
	}

	// Verify we can retrieve all objects
	for i, heapID := range heapIDs {
		data, err := heap.GetObject(heapID)
		if err != nil {
			t.Errorf("Failed to get object %d: %v", i, err)
			continue
		}

		expected := linkNames[i]
		if string(data) != expected {
			t.Errorf("Object %d: expected %q, got %q", i, expected, string(data))
		}
	}
}

func TestFractalHeapGetObject(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Insert some test data
	testData := []byte("test object data")
	heapID, err := heap.InsertObject(testData)
	if err != nil {
		t.Fatalf("Failed to insert object: %v", err)
	}

	// Retrieve and verify
	retrieved, err := heap.GetObject(heapID)
	if err != nil {
		t.Fatalf("Failed to get object: %v", err)
	}

	if !bytes.Equal(retrieved, testData) {
		t.Errorf("Retrieved data mismatch: expected %q, got %q", testData, retrieved)
	}

	// Test invalid heap ID
	invalidID := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, err = heap.GetObject(invalidID)
	if err == nil {
		t.Error("Expected error for invalid heap ID, got nil")
	}
}

func TestFractalHeapSerialization(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Insert some test objects
	objects := [][]byte{
		[]byte("object1"),
		[]byte("object2"),
		[]byte("object3"),
	}

	for _, obj := range objects {
		_, err := heap.InsertObject(obj)
		if err != nil {
			t.Fatalf("Failed to insert object: %v", err)
		}
	}

	// Create mock writer and allocator
	writer := NewMockWriter()
	allocator := NewMockAllocator(0x1000)

	// Create a test superblock
	sb := &core.Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Write heap to "file"
	headerAddr, err := heap.WriteToFile(writer, allocator, sb)
	if err != nil {
		t.Fatalf("Failed to write heap: %v", err)
	}

	if headerAddr == 0 {
		t.Error("Expected non-zero header address")
	}

	// Verify header was written
	headerData, ok := writer.data[headerAddr]
	if !ok {
		t.Fatal("Header not written to expected address")
	}

	// Check header signature
	if string(headerData[:4]) != HeapSignature {
		t.Errorf("Invalid header signature: expected %q, got %q", HeapSignature, string(headerData[:4]))
	}

	// Verify direct block was written
	if heap.Header.RootBlockAddress == 0 {
		t.Error("Root block address not set")
	}

	directBlockData, ok := writer.data[heap.Header.RootBlockAddress]
	if !ok {
		t.Fatal("Direct block not written to expected address")
	}

	// Check direct block signature
	if string(directBlockData[:4]) != DirectBlockSignature {
		t.Errorf("Invalid direct block signature: expected %q, got %q", DirectBlockSignature, string(directBlockData[:4]))
	}
}

func TestFractalHeapRoundTrip(t *testing.T) {
	// Create heap and insert objects
	writeHeap := NewWritableFractalHeap(DefaultStartingBlockSize)

	testObjects := map[string][]byte{
		"link1": []byte("target_dataset_1"),
		"link2": []byte("target_group_1"),
		"link3": []byte("target_attribute_1"),
	}

	heapIDs := make(map[string][]byte)

	for name, target := range testObjects {
		heapID, err := writeHeap.InsertObject(target)
		if err != nil {
			t.Fatalf("Failed to insert object %s: %v", name, err)
		}
		heapIDs[name] = heapID
	}

	// Write to mock file
	writer := NewMockWriter()
	allocator := NewMockAllocator(0x1000)
	sb := &core.Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	headerAddr, err := writeHeap.WriteToFile(writer, allocator, sb)
	if err != nil {
		t.Fatalf("Failed to write heap: %v", err)
	}

	// Read back using the existing read implementation
	readHeap, err := OpenFractalHeap(writer, headerAddr, sb.LengthSize, sb.OffsetSize, sb.Endianness)
	if err != nil {
		t.Fatalf("Failed to open heap: %v", err)
	}

	// Verify all objects can be read back
	for name, heapID := range heapIDs {
		data, err := readHeap.ReadObject(heapID)
		if err != nil {
			t.Errorf("Failed to read object %s: %v", name, err)
			continue
		}

		expected := testObjects[name]
		if !bytes.Equal(data, expected) {
			t.Errorf("Object %s mismatch: expected %q, got %q", name, expected, data)
		}
	}
}

func TestFractalHeapLargeScale(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Insert 100 objects
	numObjects := 100
	heapIDs := make([][]byte, 0, numObjects)

	for i := 0; i < numObjects; i++ {
		name := fmt.Sprintf("link_%04d", i)
		heapID, err := heap.InsertObject([]byte(name))
		if err != nil {
			t.Fatalf("Failed to insert object %d: %v", i, err)
		}
		heapIDs = append(heapIDs, heapID)
	}

	// Verify statistics
	if heap.Header.NumManagedObjects != uint64(numObjects) {
		t.Errorf("Expected %d objects, got %d", numObjects, heap.Header.NumManagedObjects)
	}

	// Verify all objects can be retrieved
	for i, heapID := range heapIDs {
		data, err := heap.GetObject(heapID)
		if err != nil {
			t.Errorf("Failed to get object %d: %v", i, err)
			continue
		}

		expected := fmt.Sprintf("link_%04d", i)
		if string(data) != expected {
			t.Errorf("Object %d: expected %q, got %q", i, expected, string(data))
		}
	}
}

func TestFractalHeapFillBlock(t *testing.T) {
	blockSize := uint64(4096) // Use smaller block for testing
	heap := NewWritableFractalHeap(blockSize)

	// Calculate how many 100-byte objects we can fit
	objectSize := 100
	maxObjects := int(blockSize) / objectSize

	// Fill the block
	for i := 0; i < maxObjects; i++ {
		data := bytes.Repeat([]byte("x"), objectSize)
		_, err := heap.InsertObject(data)
		if err != nil {
			t.Fatalf("Failed to insert object %d: %v", i, err)
		}
	}

	// Try to insert one more - should fail
	data := bytes.Repeat([]byte("x"), objectSize)
	_, err := heap.InsertObject(data)
	if err == nil {
		t.Error("Expected heap full error, got nil")
	}

	// Verify free space is minimal
	if heap.Header.FreeSpace >= uint64(objectSize) {
		t.Errorf("Expected less than %d free bytes, got %d", objectSize, heap.Header.FreeSpace)
	}
}

func BenchmarkFractalHeapInsert(b *testing.B) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)
	data := []byte("benchmark_object_data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		heap.InsertObject(data)
		// Reset heap every 1000 insertions to avoid filling up
		if i%1000 == 999 {
			heap = NewWritableFractalHeap(DefaultStartingBlockSize)
		}
	}
}

func BenchmarkFractalHeapGetObject(b *testing.B) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)
	data := []byte("benchmark_object_data")
	heapID, _ := heap.InsertObject(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		heap.GetObject(heapID)
	}
}
