package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// BTreeEntry.IsSoftLink coverage
// ---------------------------------------------------------------------------

// TestIsSoftLink_BTreeEntry tests the IsSoftLink method on BTreeEntry.
func TestIsSoftLink_BTreeEntry(t *testing.T) {
	tests := []struct {
		name      string
		cacheType uint32
		expected  bool
	}{
		{
			name:      "soft link",
			cacheType: CacheTypeSoftLink,
			expected:  true,
		},
		{
			name:      "not soft link - none",
			cacheType: CacheTypeNone,
			expected:  false,
		},
		{
			name:      "not soft link - symbol table",
			cacheType: CacheTypeSymbolTable,
			expected:  false,
		},
		{
			name:      "not soft link - arbitrary value",
			cacheType: 42,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &BTreeEntry{
				CacheType: tt.cacheType,
			}
			require.Equal(t, tt.expected, entry.IsSoftLink())
		})
	}
}

// TestIsSoftLink_BTreeEntry_WithFields tests IsSoftLink with populated fields.
func TestIsSoftLink_BTreeEntry_WithFields(t *testing.T) {
	entry := &BTreeEntry{
		LinkNameOffset:       100,
		ObjectAddress:        0xFFFFFFFFFFFFFFFF, // HADDR_UNDEF for soft links
		CacheType:            CacheTypeSoftLink,
		CachedSoftLinkOffset: 42,
	}
	require.True(t, entry.IsSoftLink())
	require.Equal(t, uint32(42), entry.CachedSoftLinkOffset)
}

// ---------------------------------------------------------------------------
// SymbolTableEntry.IsSoftLink coverage
// ---------------------------------------------------------------------------

// TestIsSoftLink_SymbolTableEntry tests the IsSoftLink method on SymbolTableEntry.
func TestIsSoftLink_SymbolTableEntry(t *testing.T) {
	tests := []struct {
		name      string
		cacheType uint32
		expected  bool
	}{
		{
			name:      "soft link",
			cacheType: CacheTypeSoftLink,
			expected:  true,
		},
		{
			name:      "not soft link - none",
			cacheType: CacheTypeNone,
			expected:  false,
		},
		{
			name:      "not soft link - symbol table",
			cacheType: CacheTypeSymbolTable,
			expected:  false,
		},
		{
			name:      "not soft link - large value",
			cacheType: 0xFFFFFFFF,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &SymbolTableEntry{
				CacheType: tt.cacheType,
			}
			require.Equal(t, tt.expected, entry.IsSoftLink())
		})
	}
}

// TestIsSoftLink_SymbolTableEntry_WithFields tests IsSoftLink with populated fields.
func TestIsSoftLink_SymbolTableEntry_WithFields(t *testing.T) {
	entry := &SymbolTableEntry{
		LinkNameOffset:       200,
		ObjectAddress:        0xFFFFFFFFFFFFFFFF,
		CacheType:            CacheTypeSoftLink,
		CachedSoftLinkOffset: 55,
	}
	require.True(t, entry.IsSoftLink())
	require.Equal(t, uint32(55), entry.CachedSoftLinkOffset)

	// Non-soft-link entry with symbol table cache.
	entry2 := &SymbolTableEntry{
		LinkNameOffset:  300,
		ObjectAddress:   0x1000,
		CacheType:       CacheTypeSymbolTable,
		CachedBTreeAddr: 0x2000,
		CachedHeapAddr:  0x3000,
	}
	require.False(t, entry2.IsSoftLink())
}

// ---------------------------------------------------------------------------
// SearchRecord coverage
// ---------------------------------------------------------------------------

// TestSearchRecord_Found tests finding an existing record by name.
func TestSearchRecord_Found(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Insert a record.
	err := bt.InsertRecord("test_link", 0x1234567890ABCDEF)
	require.NoError(t, err)

	// Search for it.
	heapID, found := bt.SearchRecord("test_link")
	require.True(t, found, "record should be found")
	require.NotNil(t, heapID)
	require.Len(t, heapID, 8, "heap ID should be 8 bytes")

	// Verify the first 7 bytes match the inserted value.
	var expected [8]byte
	binary.LittleEndian.PutUint64(expected[:], 0x1234567890ABCDEF)
	require.Equal(t, expected[:7], heapID[:7], "first 7 bytes of heap ID should match")
}

// TestSearchRecord_NotFound tests searching for a non-existent record.
func TestSearchRecord_NotFound(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Insert some records.
	err := bt.InsertRecord("link_a", 0x1111)
	require.NoError(t, err)
	err = bt.InsertRecord("link_b", 0x2222)
	require.NoError(t, err)

	// Search for non-existent record.
	heapID, found := bt.SearchRecord("nonexistent_link")
	require.False(t, found, "record should not be found")
	require.Nil(t, heapID)
}

// TestSearchRecord_EmptyTree tests searching in an empty B-tree.
func TestSearchRecord_EmptyTree(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	heapID, found := bt.SearchRecord("anything")
	require.False(t, found)
	require.Nil(t, heapID)
}

// TestSearchRecord_MultipleRecords tests searching among multiple records.
func TestSearchRecord_MultipleRecords(t *testing.T) {
	bt := NewWritableBTreeV2(4096)

	// Insert multiple records.
	links := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, name := range links {
		err := bt.InsertRecord(name, uint64(i+1)*0x100)
		require.NoError(t, err)
	}

	// Search for each one.
	for _, name := range links {
		heapID, found := bt.SearchRecord(name)
		require.True(t, found, "should find record for %s", name)
		require.NotNil(t, heapID)
		require.Len(t, heapID, 8)
	}

	// Search for non-existent.
	_, found := bt.SearchRecord("zeta")
	require.False(t, found)
}

// ---------------------------------------------------------------------------
// readUint64 coverage (from btreev2_write.go)
// ---------------------------------------------------------------------------

// TestReadUint64_AllSizes tests reading uint64 with different sizes.
func TestReadUint64_AllSizes(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		value    uint64
		expected uint64
	}{
		{
			name:     "1 byte",
			size:     1,
			value:    0x42,
			expected: 0x42,
		},
		{
			name:     "2 bytes",
			size:     2,
			value:    0x1234,
			expected: 0x1234,
		},
		{
			name:     "4 bytes",
			size:     4,
			value:    0x12345678,
			expected: 0x12345678,
		},
		{
			name:     "8 bytes",
			size:     8,
			value:    0x123456789ABCDEF0,
			expected: 0x123456789ABCDEF0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write value to buffer using writeUint64.
			buf := make([]byte, 8)
			writeUint64(buf, tt.value, tt.size, binary.LittleEndian)

			// Read it back using readUint64.
			result := readUint64(buf, tt.size, binary.LittleEndian)
			require.Equal(t, tt.expected, result, "readUint64 should match written value")
		})
	}
}

// TestReadUint64_BigEndian tests readUint64 with big-endian byte order.
func TestReadUint64_BigEndian(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		value    uint64
		expected uint64
	}{
		{
			name:     "2 bytes big-endian",
			size:     2,
			value:    0xABCD,
			expected: 0xABCD,
		},
		{
			name:     "4 bytes big-endian",
			size:     4,
			value:    0xDEADBEEF,
			expected: 0xDEADBEEF,
		},
		{
			name:     "8 bytes big-endian",
			size:     8,
			value:    0xFEDCBA9876543210,
			expected: 0xFEDCBA9876543210,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 8)
			writeUint64(buf, tt.value, tt.size, binary.BigEndian)

			result := readUint64(buf, tt.size, binary.BigEndian)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestReadUint64_DefaultCase tests readUint64 with unsupported size returns 0.
func TestReadUint64_DefaultCase(t *testing.T) {
	buf := make([]byte, 8)
	result := readUint64(buf, 3, binary.LittleEndian)
	require.Equal(t, uint64(0), result, "unsupported size should return 0")

	result = readUint64(buf, 0, binary.LittleEndian)
	require.Equal(t, uint64(0), result, "size 0 should return 0")

	result = readUint64(buf, 5, binary.LittleEndian)
	require.Equal(t, uint64(0), result, "size 5 should return 0")
}

// ---------------------------------------------------------------------------
// ReadObject coverage (fractalheap.go)
// ---------------------------------------------------------------------------

// TestReadObject_InvalidAddress tests ReadObject with invalid heap address.
func TestReadObject_InvalidAddress(t *testing.T) {
	_, err := OpenFractalHeap(
		bytes.NewReader(make([]byte, 256)),
		0, // Invalid address
		8, 8,
		binary.LittleEndian,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid fractal heap address")
}

// TestReadObject_InvalidSignature tests ReadObject when heap signature is wrong.
func TestReadObject_InvalidSignature(t *testing.T) {
	data := make([]byte, 256)
	copy(data[100:104], "XXXX") // Wrong signature at address 100

	_, err := OpenFractalHeap(
		bytes.NewReader(data),
		100,
		8, 8,
		binary.LittleEndian,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid fractal heap signature")
}

// TestReadObject_UnsupportedVersion tests ReadObject with unsupported version.
func TestReadObject_UnsupportedVersion(t *testing.T) {
	data := make([]byte, 256)
	copy(data[100:104], "FRHP")
	data[104] = 99 // Unsupported version

	_, err := OpenFractalHeap(
		bytes.NewReader(data),
		100,
		8, 8,
		binary.LittleEndian,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported fractal heap version")
}

// TestReadObject_HugeObjectError tests that huge objects return error.
func TestReadObject_HugeObjectError(t *testing.T) {
	// Build a minimal valid fractal heap in memory.
	heap := buildMinimalFractalHeap(t)

	// Try to read with a huge object heap ID (type=1).
	hugeHeapID := []byte{0x10, 0, 0, 0, 0, 0, 0, 0} // Type bits 4-5 = 1 (huge)
	_, err := heap.ReadObject(hugeHeapID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "huge objects not supported")
}

// TestReadObject_EmptyHeapID tests ReadObject with empty heap ID.
func TestReadObject_EmptyHeapID(t *testing.T) {
	heap := buildMinimalFractalHeap(t)

	_, err := heap.ReadObject([]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

// TestReadObject_UnsupportedIDVersion tests ReadObject with bad version in heap ID.
func TestReadObject_UnsupportedIDVersion(t *testing.T) {
	heap := buildMinimalFractalHeap(t)

	// Heap ID with version=1 (bits 6-7).
	badVersionID := []byte{0x40, 0, 0, 0, 0, 0, 0, 0}
	_, err := heap.ReadObject(badVersionID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported heap ID version")
}

// TestReadObject_TinyObject tests reading a tiny object from heap ID.
func TestReadObject_TinyObject(t *testing.T) {
	heap := buildMinimalFractalHeap(t)

	// Tiny heap ID: type=2 in bits 4-5, data follows.
	tinyID := []byte{0x20, 'h', 'e', 'l', 'l', 'o'}
	data, err := heap.ReadObject(tinyID)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
}

// TestReadObject_TinyObjectEmpty tests reading an empty tiny object.
func TestReadObject_TinyObjectEmpty(t *testing.T) {
	heap := buildMinimalFractalHeap(t)

	// Tiny heap ID with only flag byte.
	tinyID := []byte{0x20}
	data, err := heap.ReadObject(tinyID)
	require.NoError(t, err)
	require.Empty(t, data)
}

// ---------------------------------------------------------------------------
// WriteUint64 / ReadUint64 roundtrip coverage
// ---------------------------------------------------------------------------

// TestWriteUint64ReadUint64_Roundtrip tests write/read roundtrip for all sizes.
func TestWriteUint64ReadUint64_Roundtrip(t *testing.T) {
	for _, endianness := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, size := range []int{1, 2, 4, 8} {
			buf := make([]byte, 8)
			// Use a value that fits in the given size.
			var value uint64
			switch size {
			case 1:
				value = 0xAB
			case 2:
				value = 0xABCD
			case 4:
				value = 0xABCDEF01
			case 8:
				value = 0xABCDEF0123456789
			}

			writeUint64(buf, value, size, endianness)
			result := readUint64(buf, size, endianness)
			require.Equal(t, value, result, "roundtrip failed for size=%d endianness=%v", size, endianness)
		}
	}
}

// ---------------------------------------------------------------------------
// CacheType constants coverage
// ---------------------------------------------------------------------------

// TestCacheTypeConstants verifies cache type constant values.
func TestCacheTypeConstants(t *testing.T) {
	require.Equal(t, uint32(0), CacheTypeNone)
	require.Equal(t, uint32(1), CacheTypeSymbolTable)
	require.Equal(t, uint32(2), CacheTypeSoftLink)
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// buildMinimalFractalHeap creates a minimal FractalHeap for testing ReadObject dispatch.
func buildMinimalFractalHeap(t *testing.T) *FractalHeap {
	t.Helper()

	// We need a valid heap header structure, but ReadObject only uses it for
	// parseHeapID dispatch logic. Build just enough to pass validation.
	header := &FractalHeapHeader{
		Signature:            [4]byte{'F', 'R', 'H', 'P'},
		Version:              0,
		HeapIDLen:            8,
		MaxManagedObjSize:    4096,
		MaxDirectBlockSize:   4096,
		MaxHeapSize:          32,
		HeapOffsetSize:       4,
		HeapLengthSize:       2,
		StartingBlockSize:    4096,
		RootBlockAddr:        0xFFFFFFFFFFFFFFFF, // No root block
		CurrentRowCount:      0,
		ChecksumDirectBlocks: false,
	}

	return &FractalHeap{
		Header:     header,
		reader:     bytes.NewReader(make([]byte, 4096)),
		headerAddr: 100,
		sizeofSize: 8,
		sizeofAddr: 8,
		endianness: binary.LittleEndian,
	}
}
