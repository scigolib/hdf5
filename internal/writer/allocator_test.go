package writer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAllocator(t *testing.T) {
	tests := []struct {
		name          string
		initialOffset uint64
		wantOffset    uint64
	}{
		{"zero offset", 0, 0},
		{"after superblock v2", 48, 48},
		{"custom offset", 1024, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alloc := NewAllocator(tt.initialOffset)
			assert.NotNil(t, alloc)
			assert.Equal(t, tt.wantOffset, alloc.EndOfFile())
			assert.Empty(t, alloc.blocks)
		})
	}
}

func TestAllocate(t *testing.T) {
	t.Run("sequential allocations", func(t *testing.T) {
		alloc := NewAllocator(48) // After superblock v2

		// First allocation
		addr1, err := alloc.Allocate(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(48), addr1)
		assert.Equal(t, uint64(148), alloc.EndOfFile())

		// Second allocation (should be contiguous)
		addr2, err := alloc.Allocate(200)
		require.NoError(t, err)
		assert.Equal(t, uint64(148), addr2)
		assert.Equal(t, uint64(348), alloc.EndOfFile())

		// Third allocation
		addr3, err := alloc.Allocate(50)
		require.NoError(t, err)
		assert.Equal(t, uint64(348), addr3)
		assert.Equal(t, uint64(398), alloc.EndOfFile())
	})

	t.Run("zero size allocation fails", func(t *testing.T) {
		alloc := NewAllocator(0)

		addr, err := alloc.Allocate(0)
		assert.Error(t, err)
		assert.Equal(t, uint64(0), addr)
		assert.Contains(t, err.Error(), "cannot allocate zero bytes")
	})

	t.Run("large allocation", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate 10MB
		size := uint64(10 * 1024 * 1024)
		addr, err := alloc.Allocate(size)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), addr)
		assert.Equal(t, size, alloc.EndOfFile())
	})
}

func TestIsAllocated(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate blocks: [0-100), [100-300), [300-350)
	_, _ = alloc.Allocate(100)
	_, _ = alloc.Allocate(200)
	_, _ = alloc.Allocate(50)

	tests := []struct {
		name     string
		offset   uint64
		size     uint64
		expected bool
	}{
		// Exact matches
		{"first block exact", 0, 100, true},
		{"second block exact", 100, 200, true},
		{"third block exact", 300, 50, true},

		// Partial overlaps
		{"overlap start of first", 0, 50, true},
		{"overlap end of first", 50, 100, true},
		{"overlap across blocks", 50, 200, true},
		{"overlap start of second", 100, 50, true},

		// No overlaps
		{"before all blocks", 0, 0, false},        // Zero size
		{"after all blocks", 350, 100, false},     // After last block
		{"between blocks (none)", 1000, 0, false}, // Zero size, no overlap

		// Edge cases
		{"zero size at allocated address", 50, 0, false}, // Zero size never overlaps
		{"just before first block", 0, 0, false},         // Zero size
		{"just after last block", 350, 0, false},         // Zero size
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alloc.IsAllocated(tt.offset, tt.size)
			assert.Equal(t, tt.expected, result,
				"IsAllocated(%d, %d) = %v, want %v",
				tt.offset, tt.size, result, tt.expected)
		})
	}
}

func TestBlocks(t *testing.T) {
	t.Run("empty allocator", func(t *testing.T) {
		alloc := NewAllocator(0)
		blocks := alloc.Blocks()
		assert.Empty(t, blocks)
	})

	t.Run("sorted blocks", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate in order
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(200)
		_, _ = alloc.Allocate(50)

		blocks := alloc.Blocks()
		require.Len(t, blocks, 3)

		// Should be sorted by offset
		assert.Equal(t, uint64(0), blocks[0].Offset)
		assert.Equal(t, uint64(100), blocks[0].Size)

		assert.Equal(t, uint64(100), blocks[1].Offset)
		assert.Equal(t, uint64(200), blocks[1].Size)

		assert.Equal(t, uint64(300), blocks[2].Offset)
		assert.Equal(t, uint64(50), blocks[2].Size)
	})

	t.Run("blocks are copy", func(t *testing.T) {
		alloc := NewAllocator(0)
		_, _ = alloc.Allocate(100)

		blocks := alloc.Blocks()
		require.Len(t, blocks, 1)

		// Modify returned slice (should not affect allocator)
		blocks[0].Size = 999

		// Get blocks again - should be unchanged
		blocks2 := alloc.Blocks()
		require.Len(t, blocks2, 1)
		assert.Equal(t, uint64(100), blocks2[0].Size)
	})
}

func TestValidateNoOverlaps(t *testing.T) {
	t.Run("no overlaps", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate sequential blocks
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(200)
		_, _ = alloc.Allocate(50)

		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)
	})

	t.Run("empty allocator", func(t *testing.T) {
		alloc := NewAllocator(0)
		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)
	})

	t.Run("single block", func(t *testing.T) {
		alloc := NewAllocator(0)
		_, _ = alloc.Allocate(100)

		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)
	})
}

func TestAllocatorEndOfFile(t *testing.T) {
	tests := []struct {
		name          string
		initialOffset uint64
		allocations   []uint64
		expectedEOF   uint64
	}{
		{
			name:          "no allocations",
			initialOffset: 48,
			allocations:   []uint64{},
			expectedEOF:   48,
		},
		{
			name:          "single allocation",
			initialOffset: 48,
			allocations:   []uint64{100},
			expectedEOF:   148,
		},
		{
			name:          "multiple allocations",
			initialOffset: 48,
			allocations:   []uint64{100, 200, 50},
			expectedEOF:   398,
		},
		{
			name:          "large allocations",
			initialOffset: 0,
			allocations:   []uint64{1024, 2048, 4096},
			expectedEOF:   7168,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alloc := NewAllocator(tt.initialOffset)

			for _, size := range tt.allocations {
				_, err := alloc.Allocate(size)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedEOF, alloc.EndOfFile())
		})
	}
}

// Benchmark allocation performance
func BenchmarkAllocate(b *testing.B) {
	alloc := NewAllocator(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = alloc.Allocate(1024)
	}
}

func BenchmarkIsAllocated(b *testing.B) {
	alloc := NewAllocator(0)

	// Pre-allocate 1000 blocks
	for i := 0; i < 1000; i++ {
		_, _ = alloc.Allocate(1024)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = alloc.IsAllocated(500*1024, 1024)
	}
}
