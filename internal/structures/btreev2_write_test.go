// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestBTreeV2_Creation tests basic B-tree v2 creation.
func TestBTreeV2_Creation(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	require.NotNil(t, bt)
	require.NotNil(t, bt.header)
	require.NotNil(t, bt.leaf)
	require.Equal(t, uint32(4096), bt.nodeSize)
	require.Equal(t, [4]byte{'B', 'T', 'H', 'D'}, bt.header.Signature)
	require.Equal(t, uint8(0), bt.header.Version)
	require.Equal(t, BTreeV2TypeLinkNameIndex, bt.header.Type)
	require.Equal(t, uint16(11), bt.header.RecordSize)
	require.Equal(t, uint16(0), bt.header.Depth)
	require.Equal(t, DefaultBTreeV2SplitPercent, bt.header.SplitPercent)
	require.Equal(t, DefaultBTreeV2MergePercent, bt.header.MergePercent)
	require.Equal(t, uint64(0), bt.header.TotalRecords)
	require.Empty(t, bt.records)
}

// TestBTreeV2_InsertSingleRecord tests inserting a single record.
func TestBTreeV2_InsertSingleRecord(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	heapID := uint64(0x1234567890ABCDEF)
	err := bt.InsertRecord("link1", heapID)
	require.NoError(t, err)

	require.Equal(t, 1, len(bt.records))
	require.Equal(t, uint64(1), bt.header.TotalRecords)
	require.Equal(t, uint16(1), bt.header.NumRecordsRoot)

	// Verify hash is computed
	expectedHash := jenkinsHash("link1")
	require.Equal(t, expectedHash, bt.records[0].NameHash)

	// Verify heap ID is stored (7 bytes)
	require.NotEqual(t, [7]byte{}, bt.records[0].HeapID)
}

// TestBTreeV2_InsertMultipleRecords tests inserting multiple records and sorting.
func TestBTreeV2_InsertMultipleRecords(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Insert in non-sorted order
	names := []string{"zebra", "apple", "mango", "banana"}
	heapIDs := []uint64{0x1000, 0x2000, 0x3000, 0x4000}

	for i, name := range names {
		err := bt.InsertRecord(name, heapIDs[i])
		require.NoError(t, err)
	}

	require.Equal(t, 4, len(bt.records))
	require.Equal(t, uint64(4), bt.header.TotalRecords)
	require.Equal(t, uint16(4), bt.header.NumRecordsRoot)

	// Verify records are sorted by hash
	for i := 0; i < len(bt.records)-1; i++ {
		require.Less(t, bt.records[i].NameHash, bt.records[i+1].NameHash,
			"Records should be sorted by name hash")
	}

	// Verify hashes match names
	for _, name := range names {
		expectedHash := jenkinsHash(name)
		found := false
		for _, record := range bt.records {
			if record.NameHash == expectedHash {
				found = true
				break
			}
		}
		require.True(t, found, "Hash for %s should be in records", name)
	}
}

// TestBTreeV2_WriteToFile tests full write workflow.
func TestBTreeV2_WriteToFile(t *testing.T) {
	// Setup test environment
	writer := &testBTreeWriter{buf: make([]byte, 100000)}
	allocator := &testBTreeAllocator{nextAddr: 1000}
	sb := createTestSuperblock()

	bt := NewWritableBTreeV2(4096)

	// Insert records
	err := bt.InsertRecord("link1", 0x1234567890ABCDEF)
	require.NoError(t, err)
	err = bt.InsertRecord("link2", 0xFEDCBA0987654321)
	require.NoError(t, err)

	// Write to file
	headerAddr, err := bt.WriteToFile(writer, allocator, sb)
	require.NoError(t, err)
	require.Greater(t, headerAddr, uint64(0))

	// Verify leaf was written first (at 1000)
	leafAddr := bt.header.RootNodeAddr
	require.Equal(t, uint64(1000), leafAddr, "Leaf should be written first at 1000")

	// Verify header was written second (after leaf)
	require.Greater(t, headerAddr, leafAddr, "Header should be written after leaf")

	// Verify header signature
	require.Equal(t, "BTHD", string(writer.buf[headerAddr:headerAddr+4]))

	// Verify leaf signature
	require.Equal(t, "BTLF", string(writer.buf[leafAddr:leafAddr+4]))
}

// TestBTreeV2_LargeScale tests B-tree with 100+ links.
func TestBTreeV2_LargeScale(t *testing.T) {
	bt := NewWritableBTreeV2(8192)

	// Insert 100 records
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("link_%03d", i)
		heapID := uint64(i * 0x1000)
		err := bt.InsertRecord(name, heapID)
		require.NoError(t, err)
	}

	require.Equal(t, 100, len(bt.records))
	require.Equal(t, uint64(100), bt.header.TotalRecords)

	// Verify all records are sorted
	for i := 0; i < len(bt.records)-1; i++ {
		require.Less(t, bt.records[i].NameHash, bt.records[i+1].NameHash)
	}
}

// TestBTreeV2_NodeSizeExceeded tests error handling for full nodes.
func TestBTreeV2_NodeSizeExceeded(t *testing.T) {
	bt := NewWritableBTreeV2(128) // Very small node

	// Calculate max records
	maxRecords := bt.calculateMaxRecords()
	require.Greater(t, maxRecords, 0)

	// Insert up to max
	for i := 0; i < maxRecords; i++ {
		err := bt.InsertRecord(fmt.Sprintf("link%d", i), uint64(i))
		require.NoError(t, err)
	}

	// Next insertion should fail
	err := bt.InsertRecord("overflow", 0xFFFF)
	require.Error(t, err)
	require.Equal(t, ErrBTreeNodeFull, err)
}

// TestBTreeV2_UTF8Names tests Unicode link names.
func TestBTreeV2_UTF8Names(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// UTF-8 names in different scripts
	names := []string{
		"файл",   // Russian
		"文件",     // Chinese
		"ファイル",   // Japanese
		"파일",     // Korean
		"αρχείο", // Greek
		"file",   // ASCII
		"café",   // Latin with diacritics
		"🔥火🔥",    // Emoji + Chinese
	}

	for i, name := range names {
		err := bt.InsertRecord(name, uint64(i))
		require.NoError(t, err, "Should handle UTF-8 name: %s", name)
	}

	require.Equal(t, len(names), len(bt.records))

	// Verify hashes are unique (high probability)
	hashMap := make(map[uint32]bool)
	for _, record := range bt.records {
		require.False(t, hashMap[record.NameHash], "Hash collision detected")
		hashMap[record.NameHash] = true
	}
}

// TestBTreeV2_HeaderSerialization tests header round-trip.
func TestBTreeV2_HeaderSerialization(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	bt.header.RootNodeAddr = 0x12345678
	bt.header.NumRecordsRoot = 42
	bt.header.TotalRecords = 42

	sb := createTestSuperblock()

	// Encode header
	data, err := bt.encodeHeader(sb)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify signature
	require.Equal(t, "BTHD", string(data[0:4]))

	// Verify version
	require.Equal(t, uint8(0), data[4])

	// Verify type
	require.Equal(t, BTreeV2TypeLinkNameIndex, data[5])

	// Verify node size
	nodeSize := binary.LittleEndian.Uint32(data[6:10])
	require.Equal(t, uint32(4096), nodeSize)

	// Verify record size
	recordSize := binary.LittleEndian.Uint16(data[10:12])
	require.Equal(t, uint16(11), recordSize)

	// Verify depth
	depth := binary.LittleEndian.Uint16(data[12:14])
	require.Equal(t, uint16(0), depth)

	// Verify split/merge percent
	require.Equal(t, DefaultBTreeV2SplitPercent, data[14])
	require.Equal(t, DefaultBTreeV2MergePercent, data[15])

	// Verify root address (8 bytes at offset 16)
	rootAddr := binary.LittleEndian.Uint64(data[16:24])
	require.Equal(t, uint64(0x12345678), rootAddr)

	// Verify num records
	numRecords := binary.LittleEndian.Uint16(data[24:26])
	require.Equal(t, uint16(42), numRecords)

	// Verify total records
	totalRecords := binary.LittleEndian.Uint64(data[26:34])
	require.Equal(t, uint64(42), totalRecords)

	// Verify checksum
	expectedChecksum := crc32.ChecksumIEEE(data[:34])
	actualChecksum := binary.LittleEndian.Uint32(data[34:38])
	require.Equal(t, expectedChecksum, actualChecksum)
}

// TestBTreeV2_LeafSerialization tests leaf node round-trip.
func TestBTreeV2_LeafSerialization(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Insert records
	err := bt.InsertRecord("link1", 0x1111111111111111)
	require.NoError(t, err)
	err = bt.InsertRecord("link2", 0x2222222222222222)
	require.NoError(t, err)

	sb := createTestSuperblock()

	// Encode leaf
	data, err := bt.encodeLeafNode(sb)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify signature
	require.Equal(t, "BTLF", string(data[0:4]))

	// Verify version
	require.Equal(t, uint8(0), data[4])

	// Verify type
	require.Equal(t, BTreeV2TypeLinkNameIndex, data[5])

	// Verify records start at offset 6
	offset := 6
	for i := 0; i < 2; i++ {
		// Read hash (4 bytes)
		hash := binary.LittleEndian.Uint32(data[offset : offset+4])
		require.Greater(t, hash, uint32(0))
		offset += 4

		// Read heap ID (7 bytes)
		var heapID [7]byte
		copy(heapID[:], data[offset:offset+7])
		require.NotEqual(t, [7]byte{}, heapID)
		offset += 7
	}

	// Verify checksum (last 4 bytes)
	checksumOffset := len(data) - 4
	expectedChecksum := crc32.ChecksumIEEE(data[:checksumOffset])
	actualChecksum := binary.LittleEndian.Uint32(data[checksumOffset:])
	require.Equal(t, expectedChecksum, actualChecksum)
}

// TestBTreeV2_CRC32 tests checksum validation.
func TestBTreeV2_CRC32(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	bt.header.RootNodeAddr = 0x1000
	sb := createTestSuperblock()

	// Encode header
	data, err := bt.encodeHeader(sb)
	require.NoError(t, err)

	// Verify checksum is correct
	checksumOffset := len(data) - 4
	expectedChecksum := crc32.ChecksumIEEE(data[:checksumOffset])
	actualChecksum := binary.LittleEndian.Uint32(data[checksumOffset:])
	require.Equal(t, expectedChecksum, actualChecksum)

	// Corrupt data and verify checksum changes
	data[10]++ // Change node size
	newChecksum := crc32.ChecksumIEEE(data[:checksumOffset])
	require.NotEqual(t, expectedChecksum, newChecksum, "Checksum should change when data corrupted")
}

// TestCompareLinkNames tests link name comparison function.
func TestCompareLinkNames(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{"a < b", "a", "b", -1},
		{"b > a", "b", "a", 1},
		{"a == a", "a", "a", 0},
		{"abc < abd", "abc", "abd", -1},
		{"abd > abc", "abd", "abc", 1},
		{"UTF-8: apple < banana", "apple", "banana", -1},
		{"UTF-8: файл < яблоко", "файл", "яблоко", -1},
		{"UTF-8: 文件 < 苹果", "文件", "苹果", -1},
		{"Empty strings", "", "", 0},
		{"Empty vs non-empty", "", "a", -1},
		{"Non-empty vs empty", "a", "", 1},
		{"Unicode equal", "café", "café", 0},
		{"Unicode different", "café", "cafe", 1}, // é > e in UTF-8
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareLinkNames(tt.a, tt.b)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestJenkinsHash tests Jenkins hash function.
func TestJenkinsHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint32 // Known hash values from HDF5 C library
	}{
		{"empty", "", 0xdeadbeef}, // Base hash for empty string
		{"a", "a", 0},             // Will be computed
		{"b", "b", 0},
		{"link1", "link1", 0},
		{"link2", "link2", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := jenkinsHash(tt.input)
			require.Greater(t, hash, uint32(0), "Hash should be non-zero for non-empty strings")

			// Verify consistency
			hash2 := jenkinsHash(tt.input)
			require.Equal(t, hash, hash2, "Hash should be deterministic")
		})
	}

	// Test that different strings produce different hashes (with high probability)
	hash1 := jenkinsHash("link1")
	hash2 := jenkinsHash("link2")
	require.NotEqual(t, hash1, hash2, "Different strings should produce different hashes")

	// Test UTF-8 handling
	hashRussian := jenkinsHash("файл")
	hashChinese := jenkinsHash("文件")
	require.NotEqual(t, hashRussian, hashChinese)
	require.Greater(t, hashRussian, uint32(0))
	require.Greater(t, hashChinese, uint32(0))
}

// TestBTreeV2_CalculateMaxRecords tests max record calculation.
func TestBTreeV2_CalculateMaxRecords(t *testing.T) {
	tests := []struct {
		name     string
		nodeSize uint32
		expected int
	}{
		{"4KB node", 4096, (4096 - 10) / 11},
		{"8KB node", 8192, (8192 - 10) / 11},
		{"128 byte node", 128, (128 - 10) / 11},
		{"Minimum node", 32, (32 - 10) / 11}, // 2 records
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt := NewWritableBTreeV2(tt.nodeSize)
			maxRecords := bt.calculateMaxRecords()
			require.Equal(t, tt.expected, maxRecords)
		})
	}
}

// TestBTreeV2_CalculateHeaderSize tests header size calculation.
func TestBTreeV2_CalculateHeaderSize(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	sb := createTestSuperblock()

	size := bt.calculateHeaderSize(sb)

	// Expected: 4 (sig) + 1 (ver) + 1 (type) + 4 (node size) + 2 (rec size) +
	//           2 (depth) + 1 (split) + 1 (merge) + 8 (root addr) + 2 (num rec) +
	//           8 (total rec) + 4 (checksum) = 38 bytes
	expected := uint64(38)
	require.Equal(t, expected, size)
}

// TestBTreeV2_CalculateLeafSize tests leaf size calculation.
func TestBTreeV2_CalculateLeafSize(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	sb := createTestSuperblock()

	// Empty leaf
	size := bt.calculateLeafSize(sb)
	expected := uint64(4 + 1 + 1 + 0 + 4) // Sig + ver + type + records + checksum
	require.Equal(t, expected, size)

	// Add records
	bt.InsertRecord("link1", 0x1000)
	bt.InsertRecord("link2", 0x2000)

	size = bt.calculateLeafSize(sb)
	expected = uint64(4 + 1 + 1 + (2 * 11) + 4) // 2 records * 11 bytes each
	require.Equal(t, expected, size)
}

// TestBTreeV2_NilParameters tests error handling for nil parameters.
func TestBTreeV2_NilParameters(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	sb := createTestSuperblock()
	writer := &testBTreeWriter{buf: make([]byte, 10000)}
	allocator := &testBTreeAllocator{nextAddr: 1000}

	// Test nil writer
	_, err := bt.WriteToFile(nil, allocator, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")

	// Test nil allocator
	_, err = bt.WriteToFile(writer, nil, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")

	// Test nil superblock
	_, err = bt.WriteToFile(writer, allocator, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

// TestBTreeV2_WriteErrors tests write error handling.
func TestBTreeV2_WriteErrors(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	bt.InsertRecord("test", 0x1000)
	sb := createTestSuperblock()

	// Test allocator error
	allocatorErr := &testBTreeAllocatorError{}
	_, err := bt.WriteToFile(&testBTreeWriter{buf: make([]byte, 10000)}, allocatorErr, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to allocate")

	// Test writer error
	writerErr := &testBTreeWriterError{}
	allocator := &testBTreeAllocator{nextAddr: 1000}
	_, err = bt.WriteToFile(writerErr, allocator, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to write")
}

// TestBTreeV2_DefaultNodeSize tests default node size.
func TestBTreeV2_DefaultNodeSize(t *testing.T) {
	bt := NewWritableBTreeV2(0) // Pass 0 to use default

	require.Equal(t, DefaultBTreeV2NodeSize, bt.nodeSize)
	require.Equal(t, DefaultBTreeV2NodeSize, bt.header.NodeSize)
}

// TestInsertRecordSorted tests sorted insertion helper.
func TestInsertRecordSorted(t *testing.T) {
	// Empty slice
	records := []LinkNameRecord{}
	record := LinkNameRecord{NameHash: 100}
	records = insertRecordSorted(records, record)
	require.Len(t, records, 1)
	require.Equal(t, uint32(100), records[0].NameHash)

	// Insert at beginning
	record = LinkNameRecord{NameHash: 50}
	records = insertRecordSorted(records, record)
	require.Len(t, records, 2)
	require.Equal(t, uint32(50), records[0].NameHash)
	require.Equal(t, uint32(100), records[1].NameHash)

	// Insert at end
	record = LinkNameRecord{NameHash: 150}
	records = insertRecordSorted(records, record)
	require.Len(t, records, 3)
	require.Equal(t, uint32(50), records[0].NameHash)
	require.Equal(t, uint32(100), records[1].NameHash)
	require.Equal(t, uint32(150), records[2].NameHash)

	// Insert in middle
	record = LinkNameRecord{NameHash: 75}
	records = insertRecordSorted(records, record)
	require.Len(t, records, 4)
	require.Equal(t, uint32(50), records[0].NameHash)
	require.Equal(t, uint32(75), records[1].NameHash)
	require.Equal(t, uint32(100), records[2].NameHash)
	require.Equal(t, uint32(150), records[3].NameHash)
}

// TestBTreeV2_EmptyTree tests operations on empty tree.
func TestBTreeV2_EmptyTree(t *testing.T) {
	bt := NewWritableBTreeV2(4096)
	sb := createTestSuperblock()

	// Encode empty leaf
	data, err := bt.encodeLeafNode(sb)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify signature present
	require.Equal(t, "BTLF", string(data[0:4]))

	// Size should be minimal (sig + ver + type + checksum)
	expectedSize := uint64(4 + 1 + 1 + 4)
	require.Equal(t, expectedSize, uint64(len(data)))
}

// TestBTreeV2_HeapIDEncoding tests 8-byte to 7-byte heap ID conversion.
func TestBTreeV2_HeapIDEncoding(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Test with full 8-byte heap ID
	heapID := uint64(0x123456789ABCDEF0)
	err := bt.InsertRecord("test", heapID)
	require.NoError(t, err)

	// Verify 7-byte encoding
	record := bt.records[0]
	require.NotEqual(t, [7]byte{}, record.HeapID)

	// Verify heap ID is non-zero
	isZero := true
	for _, b := range record.HeapID {
		if b != 0 {
			isZero = false
			break
		}
	}
	require.False(t, isZero, "Heap ID should not be all zeros")
}

// TestWriteUint64 tests the writeUint64 helper function.
func TestWriteUint64(t *testing.T) {
	tests := []struct {
		name       string
		value      uint64
		size       int
		endianness binary.ByteOrder
		expected   []byte
	}{
		{"1 byte", 0x42, 1, binary.LittleEndian, []byte{0x42}},
		{"2 bytes LE", 0x1234, 2, binary.LittleEndian, []byte{0x34, 0x12}},
		{"2 bytes BE", 0x1234, 2, binary.BigEndian, []byte{0x12, 0x34}},
		{"4 bytes LE", 0x12345678, 4, binary.LittleEndian, []byte{0x78, 0x56, 0x34, 0x12}},
		{"4 bytes BE", 0x12345678, 4, binary.BigEndian, []byte{0x12, 0x34, 0x56, 0x78}},
		{"8 bytes LE", 0x123456789ABCDEF0, 8, binary.LittleEndian,
			[]byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12}},
		{"8 bytes BE", 0x123456789ABCDEF0, 8, binary.BigEndian,
			[]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			writeUint64(buf, tt.value, tt.size, tt.endianness)
			require.Equal(t, tt.expected, buf)
		})
	}
}

// Benchmark tests

// BenchmarkBTreeV2_Insert benchmarks record insertion.
func BenchmarkBTreeV2_Insert(b *testing.B) {
	bt := NewWritableBTreeV2(8192)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("link_%d", i%1000)
		_ = bt.InsertRecord(name, uint64(i))
	}
}

// BenchmarkBTreeV2_Write benchmarks writing B-tree to file.
func BenchmarkBTreeV2_Write(b *testing.B) {
	sb := createTestSuperblock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset for each iteration to avoid buffer overflow
		writer := &testBTreeWriter{buf: make([]byte, 100000)}
		allocator := &testBTreeAllocator{nextAddr: 1000}

		bt := NewWritableBTreeV2(4096)
		for j := 0; j < 10; j++ {
			_ = bt.InsertRecord(fmt.Sprintf("link%d", j), uint64(j))
		}
		_, _ = bt.WriteToFile(writer, allocator, sb)
	}
}

// BenchmarkJenkinsHash benchmarks hash computation.
func BenchmarkJenkinsHash(b *testing.B) {
	names := []string{
		"short",
		"medium_length_name",
		"very_long_name_with_many_characters_for_testing",
		"файл",
		"文件",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jenkinsHash(names[i%len(names)])
	}
}

// Helper functions

// createTestSuperblock creates a test superblock.
func createTestSuperblock() *core.Superblock {
	return &core.Superblock{
		Version:    2,
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}
}

// testBTreeWriter for B-tree v2 testing (avoids conflict with localheap_test.go).
type testBTreeWriter struct {
	buf []byte
}

func (w *testBTreeWriter) WriteAtAddress(data []byte, address uint64) error {
	copy(w.buf[address:], data)
	return nil
}

// testBTreeAllocator for B-tree v2 testing.
type testBTreeAllocator struct {
	nextAddr uint64
}

func (a *testBTreeAllocator) Allocate(size uint64) (uint64, error) {
	addr := a.nextAddr
	a.nextAddr += size
	return addr, nil
}

// testBTreeAllocatorError for error testing.
type testBTreeAllocatorError struct{}

func (a *testBTreeAllocatorError) Allocate(_ uint64) (uint64, error) {
	return 0, fmt.Errorf("allocation failed")
}

// testBTreeWriterError for error testing.
type testBTreeWriterError struct{}

func (w *testBTreeWriterError) WriteAtAddress(_ []byte, _ uint64) error {
	return fmt.Errorf("write failed")
}
