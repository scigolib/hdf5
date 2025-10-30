package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSymbolTableNode(t *testing.T) {
	tests := []struct {
		name     string
		capacity uint16
	}{
		{"default capacity", 32},
		{"small capacity", 8},
		{"large capacity", 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewSymbolTableNode(tt.capacity)

			require.NotNil(t, node)
			assert.Equal(t, uint8(1), node.Version)
			assert.Equal(t, uint16(0), node.NumSymbols)
			assert.Equal(t, int(tt.capacity), cap(node.Entries))
			assert.Empty(t, node.Entries)
		})
	}
}

func TestSymbolTableNode_AddEntry(t *testing.T) {
	t.Run("add to empty node", func(t *testing.T) {
		node := NewSymbolTableNode(32)

		entry := SymbolTableEntry{
			LinkNameOffset: 100,
			ObjectAddress:  200,
			CacheType:      0,
			Reserved:       0,
		}

		err := node.AddEntry(entry)
		require.NoError(t, err)

		assert.Equal(t, uint16(1), node.NumSymbols)
		assert.Len(t, node.Entries, 1)
		assert.Equal(t, entry, node.Entries[0])
	})

	t.Run("add multiple entries", func(t *testing.T) {
		node := NewSymbolTableNode(32)

		for i := 0; i < 10; i++ {
			entry := SymbolTableEntry{
				LinkNameOffset: uint64(i * 10),
				ObjectAddress:  uint64(i * 100),
			}

			err := node.AddEntry(entry)
			require.NoError(t, err)
		}

		assert.Equal(t, uint16(10), node.NumSymbols)
		assert.Len(t, node.Entries, 10)
	})

	t.Run("node full error", func(t *testing.T) {
		node := NewSymbolTableNode(3)

		// Fill to capacity
		for i := 0; i < 3; i++ {
			entry := SymbolTableEntry{LinkNameOffset: uint64(i)}
			err := node.AddEntry(entry)
			require.NoError(t, err)
		}

		// Try to add one more
		err := node.AddEntry(SymbolTableEntry{LinkNameOffset: 999})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "full")
	})
}

func TestSymbolTableNode_WriteAt(t *testing.T) {
	t.Run("empty node", func(t *testing.T) {
		node := NewSymbolTableNode(32)

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 32, binary.LittleEndian)
		require.NoError(t, err)

		data := buf.Bytes()

		// Check signature
		assert.Equal(t, "SNOD", string(data[0:4]))

		// Check version
		assert.Equal(t, uint8(1), data[4])

		// Check reserved
		assert.Equal(t, uint8(0), data[5])

		// Check num symbols
		numSymbols := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(0), numSymbols)

		// Total size should be 8 (header) + 32 * 40 (entries) = 1288 bytes
		// Entry size: 8 (link offset) + 8 (obj addr) + 4 (cache) + 4 (reserved) + 16 (scratch) = 40
		expectedSize := 8 + 32*40
		assert.Equal(t, expectedSize, len(data))
	})

	t.Run("node with entries", func(t *testing.T) {
		node := NewSymbolTableNode(32)

		// Add 3 entries
		entries := []SymbolTableEntry{
			{LinkNameOffset: 100, ObjectAddress: 1000, CacheType: 0, Reserved: 0},
			{LinkNameOffset: 200, ObjectAddress: 2000, CacheType: 0, Reserved: 0},
			{LinkNameOffset: 300, ObjectAddress: 3000, CacheType: 0, Reserved: 0},
		}

		for _, entry := range entries {
			err := node.AddEntry(entry)
			require.NoError(t, err)
		}

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 32, binary.LittleEndian)
		require.NoError(t, err)

		data := buf.Bytes()

		// Check header
		assert.Equal(t, "SNOD", string(data[0:4]))
		assert.Equal(t, uint8(1), data[4])
		numSymbols := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(3), numSymbols)

		// Check first entry
		pos := 8
		linkOffset := binary.LittleEndian.Uint64(data[pos : pos+8])
		assert.Equal(t, uint64(100), linkOffset)

		objAddr := binary.LittleEndian.Uint64(data[pos+8 : pos+16])
		assert.Equal(t, uint64(1000), objAddr)

		cacheType := binary.LittleEndian.Uint32(data[pos+16 : pos+20])
		assert.Equal(t, uint32(0), cacheType)

		// Verify remaining entries are zero-filled
		pos = 8 + 3*40 // Skip to 4th entry
		for i := 3; i < 32; i++ {
			entryData := data[pos : pos+40]
			allZero := true
			for _, b := range entryData {
				if b != 0 {
					allZero = false
					break
				}
			}
			assert.True(t, allZero, "Entry %d should be zero-filled", i)
			pos += 40
		}
	})

	t.Run("round-trip write and read", func(t *testing.T) {
		// Create and write a node
		node := NewSymbolTableNode(32)
		node.AddEntry(SymbolTableEntry{
			LinkNameOffset: 123,
			ObjectAddress:  456,
			CacheType:      0,
			Reserved:       0,
		})

		buf := &bytes.Buffer{}
		bufAt := newBytesWriterAt(buf)

		err := node.WriteAt(bufAt, 0, 8, 32, binary.LittleEndian)
		require.NoError(t, err)

		// Read it back
		data := buf.Bytes()
		assert.Equal(t, "SNOD", string(data[0:4]))
		assert.Equal(t, uint8(1), data[4])

		numSymbols := binary.LittleEndian.Uint16(data[6:8])
		assert.Equal(t, uint16(1), numSymbols)

		// Check entry data
		linkOffset := binary.LittleEndian.Uint64(data[8:16])
		assert.Equal(t, uint64(123), linkOffset)

		objAddr := binary.LittleEndian.Uint64(data[16:24])
		assert.Equal(t, uint64(456), objAddr)
	})
}

func TestWriteAddressToBytes(t *testing.T) {
	tests := []struct {
		name     string
		addr     uint64
		size     int
		expected []byte
	}{
		{
			name:     "1-byte address",
			addr:     255,
			size:     1,
			expected: []byte{255},
		},
		{
			name:     "2-byte address",
			addr:     0x1234,
			size:     2,
			expected: []byte{0x34, 0x12}, // Little-endian
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			writeAddressToBytes(buf, tt.addr, tt.size, binary.LittleEndian)
			assert.Equal(t, tt.expected, buf)
		})
	}
}

// bytesWriterAt wraps a bytes.Buffer to implement io.WriterAt.
type bytesWriterAt struct {
	buf *bytes.Buffer
}

func newBytesWriterAt(buf *bytes.Buffer) *bytesWriterAt {
	return &bytesWriterAt{buf: buf}
}

func (bwa *bytesWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	// Ensure buffer is large enough
	needed := int(off) + len(p)
	if needed > bwa.buf.Len() {
		// Grow buffer to needed size
		toAdd := needed - bwa.buf.Len()
		padding := make([]byte, toAdd)
		bwa.buf.Write(padding)
	}

	// Get current buffer content
	data := bwa.buf.Bytes()

	// Write at offset
	copy(data[off:], p)

	return len(p), nil
}
