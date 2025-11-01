// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBTreeV2LoadFromFile tests B-tree v2 read-modify-write workflow.
//
// Workflow:
// 1. Create B-tree with 5 records
// 2. Write to buffer
// 3. Load from buffer
// 4. Add 3 more records
// 5. Write back to buffer
// 6. Verify all 8 records exist.
func TestBTreeV2LoadFromFile(t *testing.T) {
	// Create test superblock
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Phase 1: Create B-tree with initial records
	t.Run("CreateAndWrite", func(t *testing.T) {
		bt := NewWritableBTreeV2(4096)

		// Insert 5 initial records
		names := []string{"attr0", "attr1", "attr2", "attr3", "attr4"}
		heapIDs := []uint64{0x1000, 0x2000, 0x3000, 0x4000, 0x5000}

		for i, name := range names {
			err := bt.InsertRecord(name, heapIDs[i])
			require.NoError(t, err, "Failed to insert record %d", i)
		}

		// Verify initial state
		assert.Equal(t, 5, len(bt.records))
		assert.Equal(t, uint64(5), bt.header.TotalRecords)
		assert.Equal(t, uint16(5), bt.header.NumRecordsRoot)

		// Write to buffer
		writer := &testBTreeWriter{buf: make([]byte, 100000)}
		allocator := &testBTreeAllocator{nextAddr: 1024}

		headerAddr, err := bt.WriteToFile(writer, allocator, sb)
		require.NoError(t, err)
		require.Greater(t, headerAddr, uint64(0))

		t.Logf("B-tree written at header address 0x%X", headerAddr)
		t.Logf("Root (leaf) address: 0x%X", bt.header.RootNodeAddr)

		// Phase 2: Load from buffer and append more records
		t.Run("LoadAndAppend", func(t *testing.T) {
			// Create new B-tree instance
			bt2 := NewWritableBTreeV2(4096)

			// Load from buffer
			err := bt2.LoadFromFile(writer, headerAddr, sb)
			require.NoError(t, err, "Failed to load B-tree from buffer")

			// Verify loaded state
			assert.Equal(t, 5, len(bt2.records), "Should have 5 records after load")
			assert.Equal(t, uint64(5), bt2.header.TotalRecords)
			assert.Equal(t, uint16(5), bt2.header.NumRecordsRoot)

			// Verify records are correctly loaded
			for i, name := range names {
				expectedHash := jenkinsHash(name)
				found := false
				for _, record := range bt2.records {
					if record.NameHash == expectedHash {
						found = true
						break
					}
				}
				assert.True(t, found, "Record %d (%s) should exist after load", i, name)
			}

			// Add 3 more records (this is the "modify" in RMW!)
			newNames := []string{"attr5", "attr6", "attr7"}
			newHeapIDs := []uint64{0x6000, 0x7000, 0x8000}

			for i, name := range newNames {
				err := bt2.InsertRecord(name, newHeapIDs[i])
				require.NoError(t, err, "Failed to insert new record %d", i)
			}

			// Verify updated state
			assert.Equal(t, 8, len(bt2.records), "Should have 8 records after append")
			assert.Equal(t, uint64(8), bt2.header.TotalRecords)
			assert.Equal(t, uint16(8), bt2.header.NumRecordsRoot)

			// Write back to buffer (at new location)
			writer2 := &testBTreeWriter{buf: make([]byte, 100000)}
			allocator2 := &testBTreeAllocator{nextAddr: 2048}

			headerAddr2, err := bt2.WriteToFile(writer2, allocator2, sb)
			require.NoError(t, err)
			require.Greater(t, headerAddr2, uint64(0))

			t.Logf("Updated B-tree written at header address 0x%X", headerAddr2)

			// Phase 3: Verify round-trip (load again and check all 8 records)
			t.Run("VerifyRoundTrip", func(t *testing.T) {
				bt3 := NewWritableBTreeV2(4096)

				err := bt3.LoadFromFile(writer2, headerAddr2, sb)
				require.NoError(t, err, "Failed to load updated B-tree")

				// Verify all 8 records exist
				assert.Equal(t, 8, len(bt3.records), "Should have 8 records after final load")
				assert.Equal(t, uint64(8), bt3.header.TotalRecords)

				// Verify all names are present
				allNames := make([]string, 0, len(names)+len(newNames))
				allNames = append(allNames, names...)
				allNames = append(allNames, newNames...)
				for i, name := range allNames {
					expectedHash := jenkinsHash(name)
					found := false
					for _, record := range bt3.records {
						if record.NameHash == expectedHash {
							found = true
							break
						}
					}
					assert.True(t, found, "Record %d (%s) should exist in final tree", i, name)
				}

				// Verify records are still sorted by hash
				for i := 0; i < len(bt3.records)-1; i++ {
					assert.LessOrEqual(t, bt3.records[i].NameHash, bt3.records[i+1].NameHash,
						"Records should remain sorted after RMW")
				}

				t.Logf("✅ Round-trip successful: all 8 records verified!")
			})
		})
	})
}

// TestBTreeV2LoadFromFile_EmptyTree tests loading an empty B-tree.
func TestBTreeV2LoadFromFile_EmptyTree(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create empty B-tree
	bt := NewWritableBTreeV2(4096)

	// Write empty tree
	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}

	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Load empty tree
	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	require.NoError(t, err)

	// Verify empty state
	assert.Equal(t, 0, len(bt2.records))
	assert.Equal(t, uint64(0), bt2.header.TotalRecords)
	assert.Equal(t, uint16(0), bt2.header.NumRecordsRoot)

	// Add records to loaded empty tree
	err = bt2.InsertRecord("first", 0x1000)
	require.NoError(t, err)

	assert.Equal(t, 1, len(bt2.records))
	assert.Equal(t, uint64(1), bt2.header.TotalRecords)
}

// TestBTreeV2LoadFromFile_SingleRecord tests RMW with single record.
func TestBTreeV2LoadFromFile_SingleRecord(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create tree with one record
	bt := NewWritableBTreeV2(4096)
	err := bt.InsertRecord("only_one", 0x9999)
	require.NoError(t, err)

	// Write
	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Load
	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, 1, len(bt2.records))
	assert.Equal(t, jenkinsHash("only_one"), bt2.records[0].NameHash)

	// Append second record
	err = bt2.InsertRecord("second", 0xAAAA)
	require.NoError(t, err)

	assert.Equal(t, 2, len(bt2.records))
}

// TestBTreeV2LoadFromFile_LargeTree tests RMW with 100+ records.
func TestBTreeV2LoadFromFile_LargeTree(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create tree with 100 records
	bt := NewWritableBTreeV2(8192) // Larger node for 100+ records
	for i := 0; i < 100; i++ {
		name := string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		heapID := uint64(i * 0x100)
		err := bt.InsertRecord(name, heapID)
		require.NoError(t, err)
	}

	// Write
	writer := &testBTreeWriter{buf: make([]byte, 1000000)} // Larger buffer
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Load
	bt2 := NewWritableBTreeV2(8192)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, 100, len(bt2.records))
	assert.Equal(t, uint64(100), bt2.header.TotalRecords)

	// Add 10 more
	for i := 100; i < 110; i++ {
		name := string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		heapID := uint64(i * 0x100)
		err := bt2.InsertRecord(name, heapID)
		require.NoError(t, err)
	}

	assert.Equal(t, 110, len(bt2.records))
}

// TestBTreeV2LoadFromFile_HeaderValidation tests header validation during load.
func TestBTreeV2LoadFromFile_HeaderValidation(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Test invalid signature
	t.Run("InvalidSignature", func(t *testing.T) {
		buf := make([]byte, 100)
		copy(buf[0:4], "XXXX") // Wrong signature

		bt := NewWritableBTreeV2(4096)
		err := bt.LoadFromFile(bytes.NewReader(buf), 0, sb)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid B-tree header signature")
	})

	// Test invalid version
	t.Run("InvalidVersion", func(t *testing.T) {
		buf := make([]byte, 100)
		copy(buf[0:4], "BTHD")
		buf[4] = 99 // Invalid version

		bt := NewWritableBTreeV2(4096)
		err := bt.LoadFromFile(bytes.NewReader(buf), 0, sb)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported B-tree version")
	})

	// Test non-zero depth (not supported in MVP)
	t.Run("NonZeroDepth", func(t *testing.T) {
		// Create valid header but with depth > 0
		bt := NewWritableBTreeV2(4096)
		bt.header.Depth = 2 // Multi-level tree

		writer := &testBTreeWriter{buf: make([]byte, 100000)}

		// Manually encode header with depth = 2
		bt.header.RootNodeAddr = 1024
		headerData, _ := bt.encodeHeader(sb)
		_ = writer.WriteAtAddress(headerData, 2048)

		// Try to load - should fail
		bt2 := NewWritableBTreeV2(4096)
		err := bt2.LoadFromFile(writer, 2048, sb)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "single-leaf B-trees")
	})
}

// TestBTreeV2LoadFromFile_LeafValidation tests leaf validation during load.
func TestBTreeV2LoadFromFile_LeafValidation(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create valid B-tree and write
	bt := NewWritableBTreeV2(4096)
	bt.InsertRecord("test", 0x1234)

	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Corrupt leaf signature
	leafAddr := bt.header.RootNodeAddr
	copy(writer.buf[leafAddr:leafAddr+4], "XXXX")

	// Try to load - should fail
	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid B-tree leaf signature")
}

// TestBTreeV2LoadFromFile_ChecksumValidation tests checksum validation.
func TestBTreeV2LoadFromFile_ChecksumValidation(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create valid B-tree
	bt := NewWritableBTreeV2(4096)
	bt.InsertRecord("test", 0x1234)

	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Corrupt header checksum
	writer.buf[headerAddr+34]++ // Corrupt last byte before checksum

	// Try to load - should fail
	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

// TestBTreeV2LoadFromFile_NilParameters tests nil parameter handling.
func TestBTreeV2LoadFromFile_NilParameters(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	bt := NewWritableBTreeV2(4096)

	// Nil reader
	err := bt.LoadFromFile(nil, 0, sb)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reader is nil")

	// Nil superblock
	reader := bytes.NewReader(make([]byte, 100))
	err = bt.LoadFromFile(reader, 0, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "superblock is nil")
}

// TestBTreeV2LoadFromFile_RecordSorting tests that records remain sorted after RMW.
func TestBTreeV2LoadFromFile_RecordSorting(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Insert records in non-sorted order
	bt := NewWritableBTreeV2(4096)
	names := []string{"zebra", "apple", "mango", "banana"}
	for i, name := range names {
		err := bt.InsertRecord(name, uint64(i*0x1000))
		require.NoError(t, err)
	}

	// Write
	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	// Load
	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	require.NoError(t, err)

	// Insert more records in different order
	moreNames := []string{"cherry", "grape", "avocado"}
	for i, name := range moreNames {
		err := bt2.InsertRecord(name, uint64((i+10)*0x1000))
		require.NoError(t, err)
	}

	// Verify all 7 records are sorted by hash
	for i := 0; i < len(bt2.records)-1; i++ {
		assert.LessOrEqual(t, bt2.records[i].NameHash, bt2.records[i+1].NameHash,
			"Records at positions %d and %d are not sorted", i, i+1)
	}
}

// TestBTreeV2LoadFromFile_HeapIDPreservation tests that heap IDs are preserved during RMW.
func TestBTreeV2LoadFromFile_HeapIDPreservation(t *testing.T) {
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create tree with specific heap IDs
	bt := NewWritableBTreeV2(4096)
	names := []string{"attr0", "attr1", "attr2"}
	heapIDs := []uint64{0x123456789ABCDEF0, 0xFEDCBA9876543210, 0x1111111111111111}

	for i, name := range names {
		err := bt.InsertRecord(name, heapIDs[i])
		require.NoError(t, err)
	}

	// Write and load
	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1024}
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)

	bt2 := NewWritableBTreeV2(4096)
	err = bt2.LoadFromFile(writer, headerAddr, sb)
	require.NoError(t, err)

	// Verify heap IDs are preserved (first 7 bytes)
	for i, name := range names {
		expectedHash := jenkinsHash(name)
		var expectedHeapID [7]byte
		var temp [8]byte
		binary.LittleEndian.PutUint64(temp[:], heapIDs[i])
		copy(expectedHeapID[:], temp[:7])

		// Find record
		found := false
		for _, record := range bt2.records {
			if record.NameHash == expectedHash {
				assert.Equal(t, expectedHeapID, record.HeapID,
					"Heap ID for %s should be preserved", name)
				found = true
				break
			}
		}
		assert.True(t, found, "Record %s should exist", name)
	}
}
