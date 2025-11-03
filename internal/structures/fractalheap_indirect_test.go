// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestIndirectBlockCreation tests NewWritableIndirectBlock initialization.
func TestIndirectBlockCreation(t *testing.T) {
	heapHeaderAddr := uint64(0x1000)
	blockOffset := uint64(0)
	numRows := uint16(1)
	tableWidth := uint16(2)
	maxDirectRows := uint16(1)

	iblock := NewWritableIndirectBlock(heapHeaderAddr, blockOffset, numRows, tableWidth, maxDirectRows)

	// Verify header initialization
	if iblock.Header.Version != IndirectBlockVersion {
		t.Errorf("Expected version %d, got %d", IndirectBlockVersion, iblock.Header.Version)
	}

	if iblock.Header.HeapHeaderAddr != heapHeaderAddr {
		t.Errorf("Expected heap header addr 0x%X, got 0x%X", heapHeaderAddr, iblock.Header.HeapHeaderAddr)
	}

	if iblock.Header.BlockOffset != blockOffset {
		t.Errorf("Expected block offset %d, got %d", blockOffset, iblock.Header.BlockOffset)
	}

	if iblock.Header.NumRows != numRows {
		t.Errorf("Expected %d rows, got %d", numRows, iblock.Header.NumRows)
	}

	if iblock.Header.TableWidth != tableWidth {
		t.Errorf("Expected table width %d, got %d", tableWidth, iblock.Header.TableWidth)
	}

	// Verify child addresses array is correct size
	expectedEntries := int(numRows) * int(tableWidth)
	if len(iblock.ChildAddresses) != expectedEntries {
		t.Errorf("Expected %d child entries, got %d", expectedEntries, len(iblock.ChildAddresses))
	}

	// Verify all entries are zero-initialized
	for i, addr := range iblock.ChildAddresses {
		if addr != 0 {
			t.Errorf("Entry %d should be zero, got 0x%X", i, addr)
		}
	}
}

// TestRootTransition tests automatic transition from direct to indirect block.
func TestRootTransition(t *testing.T) {
	blockSize := uint64(4096) // Small block for quick fill
	heap := NewWritableFractalHeap(blockSize)

	// Verify initial state (direct block root)
	if heap.RootIndirectBlock != nil {
		t.Fatal("Expected direct block root initially")
	}

	if heap.DirectBlock == nil {
		t.Fatal("Expected direct block to exist")
	}

	// Calculate objects needed to fill first block
	objectSize := 100
	maxObjects := int(blockSize) / objectSize

	// Insert objects to fill first block
	var heapIDs [][]byte
	for i := 0; i < maxObjects; i++ {
		data := bytes.Repeat([]byte("A"), objectSize)
		heapID, err := heap.InsertObject(data)
		if err != nil {
			t.Fatalf("Failed to insert object %d: %v", i, err)
		}
		heapIDs = append(heapIDs, heapID)
	}

	// Verify still using direct block
	if heap.RootIndirectBlock != nil {
		t.Error("Should still be direct block before overflow")
	}

	// Insert one more to trigger transition
	overflowData := bytes.Repeat([]byte("B"), objectSize)
	overflowID, err := heap.InsertObject(overflowData)
	if err != nil {
		t.Fatalf("Failed to trigger transition: %v", err)
	}
	heapIDs = append(heapIDs, overflowID)

	// Verify transition occurred
	if heap.RootIndirectBlock == nil {
		t.Fatal("Expected transition to indirect block root")
	}

	if heap.Header.CurrentNumRows != 1 {
		t.Errorf("Expected 1 row in indirect block, got %d", heap.Header.CurrentNumRows)
	}

	// Verify two blocks exist (original + new)
	if len(heap.DirectBlocks) != 2 {
		t.Fatalf("Expected 2 direct blocks, got %d", len(heap.DirectBlocks))
	}

	// Verify all objects are still readable
	for i, heapID := range heapIDs {
		data, err := heap.GetObject(heapID)
		if err != nil {
			t.Errorf("Failed to read object %d after transition: %v", i, err)
		}
		expectedLen := objectSize
		if len(data) != expectedLen {
			t.Errorf("Object %d: expected length %d, got %d", i, expectedLen, len(data))
		}
	}

	// Verify object count
	expectedCount := uint64(maxObjects + 1)
	if heap.Header.NumManagedObjects != expectedCount {
		t.Errorf("Expected %d objects, got %d", expectedCount, heap.Header.NumManagedObjects)
	}
}

// TestLargeAttribute tests handling of large attributes (>64KB).
func TestLargeAttribute(t *testing.T) {
	heap := NewWritableFractalHeap(DefaultStartingBlockSize)

	// Create 100KB attribute (larger than default max 64KB)
	// This should fail with current max managed object size
	largeData := bytes.Repeat([]byte("X"), 100*1024)

	_, err := heap.InsertObject(largeData)
	if err == nil {
		t.Error("Expected error for object exceeding max managed size")
	}

	// Test with 60KB object (should work)
	mediumData := bytes.Repeat([]byte("M"), 60*1024)
	heapID, err := heap.InsertObject(mediumData)
	if err != nil {
		t.Fatalf("Failed to insert 60KB object: %v", err)
	}

	// Verify retrieval
	retrieved, err := heap.GetObject(heapID)
	if err != nil {
		t.Fatalf("Failed to retrieve large object: %v", err)
	}

	if len(retrieved) != len(mediumData) {
		t.Errorf("Expected %d bytes, got %d", len(mediumData), len(retrieved))
	}

	if !bytes.Equal(retrieved, mediumData) {
		t.Error("Retrieved data doesn't match original")
	}
}

// TestManyAttributes tests insertion of 200+ small attributes.
func TestManyAttributes(t *testing.T) {
	blockSize := uint64(8192) // 8KB blocks
	heap := NewWritableFractalHeap(blockSize)

	numAttributes := 200
	attributeSize := 50 // 50 bytes each

	var heapIDs [][]byte

	// Insert 200 attributes
	for i := 0; i < numAttributes; i++ {
		// Create unique data for each attribute
		data := make([]byte, attributeSize)
		data[0] = byte(i >> 8)
		data[1] = byte(i & 0xFF)
		copy(data[2:], bytes.Repeat([]byte("a"), attributeSize-2))

		heapID, err := heap.InsertObject(data)
		if err != nil {
			t.Fatalf("Failed to insert attribute %d: %v", i, err)
		}
		heapIDs = append(heapIDs, heapID)
	}

	// Verify indirect block was created
	if heap.RootIndirectBlock == nil {
		t.Error("Expected indirect block for 200 attributes")
	}

	// Verify multiple child blocks exist
	if len(heap.DirectBlocks) < 2 {
		t.Errorf("Expected at least 2 child blocks, got %d", len(heap.DirectBlocks))
	}

	t.Logf("Created %d child blocks for %d attributes", len(heap.DirectBlocks), numAttributes)

	// Verify all attributes are readable
	for i, heapID := range heapIDs {
		data, err := heap.GetObject(heapID)
		if err != nil {
			t.Errorf("Failed to read attribute %d: %v", i, err)
		}

		if len(data) != attributeSize {
			t.Errorf("Attribute %d: expected %d bytes, got %d", i, attributeSize, len(data))
		}

		// Verify unique identifier
		if data[0] != byte(i>>8) || data[1] != byte(i&0xFF) {
			t.Errorf("Attribute %d: data mismatch", i)
		}
	}

	// Verify object count
	if heap.Header.NumManagedObjects != uint64(numAttributes) {
		t.Errorf("Expected %d objects, got %d", numAttributes, heap.Header.NumManagedObjects)
	}
}

// TestIndirectBlockSerialization tests WriteToFile/LoadFromFile round-trip.
func TestIndirectBlockSerialization(t *testing.T) {
	// Create indirect block
	heapHeaderAddr := uint64(0x1000)
	blockOffset := uint64(0)
	numRows := uint16(2)
	tableWidth := uint16(2)
	maxDirectRows := uint16(2)

	iblock := NewWritableIndirectBlock(heapHeaderAddr, blockOffset, numRows, tableWidth, maxDirectRows)

	// Set some child addresses
	if err := iblock.SetChildAddress(0, 0x2000); err != nil {
		t.Fatalf("Failed to set child 0: %v", err)
	}
	if err := iblock.SetChildAddress(1, 0x3000); err != nil {
		t.Fatalf("Failed to set child 1: %v", err)
	}
	if err := iblock.SetChildAddress(2, 0x4000); err != nil {
		t.Fatalf("Failed to set child 2: %v", err)
	}

	// Create mock writer and allocator
	writer := NewMockWriter()
	allocator := NewMockAllocator(0x5000)

	// Create mock superblock
	sb := &core.Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	heapOffsetSize := uint8(2) // 2-byte heap offsets

	// Write to file
	address, err := iblock.WriteToFile(writer, allocator, sb, heapOffsetSize)
	if err != nil {
		t.Fatalf("Failed to write indirect block: %v", err)
	}

	t.Logf("Indirect block written at address 0x%X", address)

	// Read back using ParseIndirectBlock
	readBlock, err := ParseIndirectBlock(writer, address, numRows, tableWidth,
		sb.OffsetSize, heapOffsetSize, sb.Endianness, heapHeaderAddr)
	if err != nil {
		t.Fatalf("Failed to parse indirect block: %v", err)
	}

	// Verify header fields
	if readBlock.Header.Version != iblock.Header.Version {
		t.Errorf("Version mismatch: expected %d, got %d", iblock.Header.Version, readBlock.Header.Version)
	}

	if readBlock.Header.HeapHeaderAddr != heapHeaderAddr {
		t.Errorf("Heap header addr mismatch: expected 0x%X, got 0x%X",
			heapHeaderAddr, readBlock.Header.HeapHeaderAddr)
	}

	if readBlock.Header.BlockOffset != blockOffset {
		t.Errorf("Block offset mismatch: expected %d, got %d", blockOffset, readBlock.Header.BlockOffset)
	}

	// Verify child addresses
	if len(readBlock.Entries) != len(iblock.ChildAddresses) {
		t.Fatalf("Entry count mismatch: expected %d, got %d",
			len(iblock.ChildAddresses), len(readBlock.Entries))
	}

	for i := range iblock.ChildAddresses {
		if readBlock.Entries[i] != iblock.ChildAddresses[i] {
			t.Errorf("Entry %d mismatch: expected 0x%X, got 0x%X",
				i, iblock.ChildAddresses[i], readBlock.Entries[i])
		}
	}
}

// TestMixedBlocks tests heap with both direct and indirect blocks working together.
func TestMixedBlocks(t *testing.T) {
	blockSize := uint64(2048) // Small blocks for testing
	heap := NewWritableFractalHeap(blockSize)

	var heapIDs [][]byte
	var expectedData [][]byte

	// Phase 1: Insert small objects (direct block)
	for i := 0; i < 10; i++ {
		data := bytes.Repeat([]byte("S"), 50)
		data[0] = byte(i) // Unique identifier
		heapID, err := heap.InsertObject(data)
		if err != nil {
			t.Fatalf("Phase 1: failed to insert object %d: %v", i, err)
		}
		heapIDs = append(heapIDs, heapID)
		expectedData = append(expectedData, data)
	}

	// Verify still using direct block
	if heap.RootIndirectBlock != nil {
		t.Error("Should be direct block after phase 1")
	}

	// Phase 2: Insert medium objects to trigger transition
	// Limit to avoid exceeding MVP's 2-block limit (1 row × 2 width)
	for i := 10; i < 20; i++ {
		data := bytes.Repeat([]byte("M"), 200)
		data[0] = byte(i) // Unique identifier
		heapID, err := heap.InsertObject(data)
		if err != nil {
			t.Fatalf("Phase 2: failed to insert object %d: %v", i, err)
		}
		heapIDs = append(heapIDs, heapID)
		expectedData = append(expectedData, data)
	}

	// Verify transition occurred
	if heap.RootIndirectBlock == nil {
		t.Error("Should have transitioned to indirect block")
	}

	// Phase 3: Insert more objects filling up second block
	// MVP limitation: Can only have 2 blocks (1 row × 2 width)
	// So we insert carefully to fill second block but not overflow
	for i := 20; i < 30; i++ {
		data := bytes.Repeat([]byte("L"), 100)
		data[0] = byte(i) // Unique identifier
		heapID, err := heap.InsertObject(data)
		if err != nil {
			// MVP limitation reached - this is expected
			t.Logf("Phase 3: reached MVP limit at object %d (expected)", i)
			break
		}
		heapIDs = append(heapIDs, heapID)
		expectedData = append(expectedData, data)
	}

	t.Logf("Total objects: %d", len(heapIDs))
	t.Logf("Direct blocks: %d", len(heap.DirectBlocks))
	t.Logf("Indirect block rows: %d", heap.Header.CurrentNumRows)

	// Verify all objects are readable in correct order
	for i, heapID := range heapIDs {
		data, err := heap.GetObject(heapID)
		if err != nil {
			t.Errorf("Failed to read object %d: %v", i, err)
			continue
		}

		if len(data) != len(expectedData[i]) {
			t.Errorf("Object %d: length mismatch (expected %d, got %d)",
				i, len(expectedData[i]), len(data))
			continue
		}

		if !bytes.Equal(data, expectedData[i]) {
			t.Errorf("Object %d: data mismatch", i)
		}
	}

	// Verify statistics
	if heap.Header.NumManagedObjects != uint64(len(heapIDs)) {
		t.Errorf("Object count mismatch: expected %d, got %d",
			len(heapIDs), heap.Header.NumManagedObjects)
	}
}

// TestIndirectBlockEntryCalculation tests CalculateEntryIndex helper.
func TestIndirectBlockEntryCalculation(t *testing.T) {
	tableWidth := uint16(2)
	iblock := NewWritableIndirectBlock(0, 0, 3, tableWidth, 3)

	tests := []struct {
		row      uint16
		col      uint16
		expected int
	}{
		{0, 0, 0}, // First entry
		{0, 1, 1}, // Second entry
		{1, 0, 2}, // Third entry
		{1, 1, 3}, // Fourth entry
		{2, 0, 4}, // Fifth entry
		{2, 1, 5}, // Sixth entry
	}

	for _, tt := range tests {
		result := iblock.CalculateEntryIndex(tt.row, tt.col)
		if result != tt.expected {
			t.Errorf("CalculateEntryIndex(%d, %d) = %d, expected %d",
				tt.row, tt.col, result, tt.expected)
		}
	}
}
