package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBTreeNodeV1(t *testing.T) {
	tests := []struct {
		name     string
		nodeType uint8
		K        uint16
	}{
		{"group btree default", 0, 16},
		{"group btree small", 0, 8},
		{"group btree large", 0, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewBTreeNodeV1(tt.nodeType, tt.K)

			require.NotNil(t, node)
			assert.Equal(t, [4]byte{'T', 'R', 'E', 'E'}, node.Signature)
			assert.Equal(t, tt.nodeType, node.NodeType)
			assert.Equal(t, uint8(0), node.NodeLevel) // Always leaf for MVP
			assert.Equal(t, uint16(0), node.EntriesUsed)
			assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), node.LeftSibling)
			assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), node.RightSibling)

			// Check capacity: 2K+1 keys, 2K children
			assert.Equal(t, 2*int(tt.K)+1, cap(node.Keys))
			assert.Equal(t, 2*int(tt.K), cap(node.ChildPointers))
			assert.Empty(t, node.Keys)
			assert.Empty(t, node.ChildPointers)
		})
	}
}

func TestBTreeNodeV1_AddKey(t *testing.T) {
	t.Run("add to empty node", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 16) // K=16, capacity=33 keys

		err := node.AddKey(100, 1000)
		require.NoError(t, err)

		assert.Equal(t, uint16(1), node.EntriesUsed)
		assert.Len(t, node.Keys, 1)
		assert.Len(t, node.ChildPointers, 1)
		assert.Equal(t, uint64(100), node.Keys[0])
		assert.Equal(t, uint64(1000), node.ChildPointers[0])
	})

	t.Run("add multiple keys", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 16)

		for i := 0; i < 10; i++ {
			err := node.AddKey(uint64(i*10), uint64(i*100))
			require.NoError(t, err)
		}

		assert.Equal(t, uint16(10), node.EntriesUsed)
		assert.Len(t, node.Keys, 10)
		assert.Len(t, node.ChildPointers, 10)
	})

	t.Run("node full error", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 2) // Small capacity: 2K+1 = 5 keys

		// Fill to capacity
		for i := 0; i < 5; i++ {
			err := node.AddKey(uint64(i), uint64(i*10))
			require.NoError(t, err)
		}

		// Try to add one more
		err := node.AddKey(999, 9990)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "full")
	})
}

func TestBTreeNodeV1_WriteAt(t *testing.T) {
	t.Run("empty node", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 16) // K=16

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 16, binary.LittleEndian)
		require.NoError(t, err)

		data := buf.Bytes()

		// Check signature
		assert.Equal(t, "TREE", string(data[0:4]))

		// Check node type (0 = group)
		assert.Equal(t, uint8(0), data[4])

		// Check node level (0 = leaf)
		assert.Equal(t, uint8(0), data[5])

		// Check entries used
		entriesUsed := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(0), entriesUsed)

		// Check left sibling (should be UNDEF)
		leftSibling := binary.LittleEndian.Uint64(data[8:16])
		assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), leftSibling)

		// Check right sibling (should be UNDEF)
		rightSibling := binary.LittleEndian.Uint64(data[16:24])
		assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), rightSibling)

		// Calculate expected size:
		// Header: 8 bytes
		// Siblings: 2 * 8 = 16 bytes
		// Keys: (2K+1) * 8 = 33 * 8 = 264 bytes
		// Children: 2K * 8 = 32 * 8 = 256 bytes
		// Total: 8 + 16 + 264 + 256 = 544 bytes
		expectedSize := 8 + 16 + 264 + 256
		assert.Equal(t, expectedSize, len(data))
	})

	t.Run("node with keys and children", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 16)

		// Add 3 keys and children
		keys := []uint64{100, 200, 300}
		children := []uint64{1000, 2000, 3000}

		for i := 0; i < 3; i++ {
			err := node.AddKey(keys[i], children[i])
			require.NoError(t, err)
		}

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 16, binary.LittleEndian)
		require.NoError(t, err)

		data := buf.Bytes()

		// Check header
		assert.Equal(t, "TREE", string(data[0:4]))
		assert.Equal(t, uint8(0), data[4])
		assert.Equal(t, uint8(0), data[5])

		entriesUsed := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(3), entriesUsed)

		// Keys and children are interleaved after siblings
		// Position after header and siblings: 24 bytes
		pos := 24

		// First key
		key0 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(100), key0)
		pos += 8

		// First child
		child0 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(1000), child0)
		pos += 8

		// Second key
		key1 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(200), key1)
		pos += 8

		// Second child
		child1 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(2000), child1)
		pos += 8

		// Third key
		key2 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(300), key2)
		pos += 8

		// Third child
		child2 := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(3000), child2)
	})

	t.Run("round-trip write and read", func(t *testing.T) {
		// Create and write a node
		node := NewBTreeNodeV1(0, 16)
		node.AddKey(123, 456)

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 16, binary.LittleEndian)
		require.NoError(t, err)

		// Read it back
		data := buf.Bytes()
		assert.Equal(t, "TREE", string(data[0:4]))
		assert.Equal(t, uint8(0), data[4]) // Node type
		assert.Equal(t, uint8(0), data[5]) // Node level

		entriesUsed := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(1), entriesUsed)

		// Check first key and child (at position 24)
		key := binary.LittleEndian.Uint64(data[24:32])
		assert.Equal(t, uint64(123), key)

		child := binary.LittleEndian.Uint64(data[32:40])
		assert.Equal(t, uint64(456), child)
	})

	t.Run("non-zero address", func(t *testing.T) {
		node := NewBTreeNodeV1(0, 16)
		node.AddKey(999, 9999)

		buf := &bytes.Buffer{}
		// Pre-fill buffer to simulate non-zero offset
		offset := int64(500)
		buf.Write(make([]byte, offset))

		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, uint64(offset), 8, 16, binary.LittleEndian)
		require.NoError(t, err)

		data := buf.Bytes()

		// Check signature at offset
		assert.Equal(t, "TREE", string(data[offset:offset+4]))

		// Check entries used at offset+6
		entriesUsed := binary.LittleEndian.Uint16(data[offset+6 : offset+8])
		assert.Equal(t, uint16(1), entriesUsed)
	})
}

func TestWriteAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     uint64
		size     int
		expected []byte
	}{
		{
			name:     "1-byte address",
			addr:     42,
			size:     1,
			expected: []byte{42},
		},
		{
			name:     "2-byte address",
			addr:     0xABCD,
			size:     2,
			expected: []byte{0xCD, 0xAB}, // Little-endian
		},
		{
			name:     "4-byte address",
			addr:     0x12345678,
			size:     4,
			expected: []byte{0x78, 0x56, 0x34, 0x12},
		},
		{
			name:     "8-byte address",
			addr:     0x123456789ABCDEF0,
			size:     8,
			expected: []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12},
		},
		{
			name:     "undefined address",
			addr:     0xFFFFFFFFFFFFFFFF,
			size:     8,
			expected: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			writeAddr(buf, tt.addr, tt.size, binary.LittleEndian)
			assert.Equal(t, tt.expected, buf)
		})
	}
}

func TestBTreeNodeV1_Integration(t *testing.T) {
	t.Run("create group index structure", func(t *testing.T) {
		// Simulate creating a B-tree to index a group with 3 members
		btree := NewBTreeNodeV1(0, 16) // Node type 0 = group

		// Add keys (heap offsets for link names) and children (symbol table node addresses)
		// Key is the heap offset where the link name is stored
		// Child is the address of the symbol table node containing the entry
		err := btree.AddKey(0, 1024) // First link at heap offset 0, symbol table at 1024
		require.NoError(t, err)

		err = btree.AddKey(20, 1024) // Second link at heap offset 20
		require.NoError(t, err)

		err = btree.AddKey(40, 1024) // Third link at heap offset 40
		require.NoError(t, err)

		// Write to buffer
		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err = btree.WriteAt(bufAt, 0, 8, 16, binary.LittleEndian)
		require.NoError(t, err)

		// Verify structure
		data := buf.Bytes()
		assert.Equal(t, "TREE", string(data[0:4]))

		entriesUsed := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(3), entriesUsed)

		// All children should point to same symbol table node (MVP - single node)
		child0 := binary.LittleEndian.Uint64(data[32:40])
		child1 := binary.LittleEndian.Uint64(data[48:56])
		child2 := binary.LittleEndian.Uint64(data[64:72])

		assert.Equal(t, uint64(1024), child0)
		assert.Equal(t, uint64(1024), child1)
		assert.Equal(t, uint64(1024), child2)
	})
}
