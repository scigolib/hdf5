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

// Benchmark allocation performance.
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

// ==================== COMPREHENSIVE TESTS (Component 5) ====================

// TestAllocator_StressTest validates allocator under high load.
func TestAllocator_StressTest(t *testing.T) {
	t.Run("10000 small allocations", func(t *testing.T) {
		alloc := NewAllocator(0)
		const numAllocs = 10000
		const allocSize = 64

		addrs := make([]uint64, numAllocs)

		// Allocate 10,000 blocks
		for i := 0; i < numAllocs; i++ {
			addr, err := alloc.Allocate(allocSize)
			require.NoError(t, err)
			addrs[i] = addr
		}

		// Verify all addresses are unique
		seen := make(map[uint64]bool)
		for i, addr := range addrs {
			assert.False(t, seen[addr], "duplicate address %d at index %d", addr, i)
			seen[addr] = true
		}

		// Verify sequential allocation (no gaps)
		for i := 0; i < numAllocs-1; i++ {
			expected := addrs[i] + allocSize
			assert.Equal(t, expected, addrs[i+1],
				"allocation %d: addresses not sequential", i)
		}

		// Verify no overlaps
		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)

		// Verify total space used
		expectedEOF := uint64(numAllocs * allocSize)
		assert.Equal(t, expectedEOF, alloc.EndOfFile())
	})

	t.Run("mixed size allocations", func(t *testing.T) {
		alloc := NewAllocator(48) // Start after superblock
		sizes := []uint64{10, 100, 1000, 50, 500, 25, 250}

		var expectedEOF uint64 = 48
		for _, size := range sizes {
			addr, err := alloc.Allocate(size)
			require.NoError(t, err)
			assert.Equal(t, expectedEOF, addr)
			expectedEOF += size
		}

		assert.Equal(t, expectedEOF, alloc.EndOfFile())
		assert.NoError(t, alloc.ValidateNoOverlaps())
	})
}

// TestAllocator_LargeAllocations tests allocations of various large sizes.
func TestAllocator_LargeAllocations(t *testing.T) {
	tests := []struct {
		name string
		size uint64
	}{
		{"1 MB", 1024 * 1024},
		{"10 MB", 10 * 1024 * 1024},
		{"100 MB", 100 * 1024 * 1024},
		{"1 GB", 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alloc := NewAllocator(0)

			addr, err := alloc.Allocate(tt.size)
			require.NoError(t, err)
			assert.Equal(t, uint64(0), addr)
			assert.Equal(t, tt.size, alloc.EndOfFile())

			// Verify block is tracked
			blocks := alloc.Blocks()
			require.Len(t, blocks, 1)
			assert.Equal(t, uint64(0), blocks[0].Offset)
			assert.Equal(t, tt.size, blocks[0].Size)
		})
	}
}

// TestAllocator_Blocks_Complete tests the Blocks() method thoroughly.
func TestAllocator_Blocks_Complete(t *testing.T) {
	t.Run("blocks are returned sorted", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate blocks (they're already sequential, but test sorting anyway)
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(200)
		_, _ = alloc.Allocate(50)

		blocks := alloc.Blocks()
		require.Len(t, blocks, 3)

		// Verify sorted by offset
		for i := 0; i < len(blocks)-1; i++ {
			assert.Less(t, blocks[i].Offset, blocks[i+1].Offset,
				"blocks should be sorted by offset")
		}
	})

	t.Run("blocks returns copy not reference", func(t *testing.T) {
		alloc := NewAllocator(0)
		_, _ = alloc.Allocate(100)

		// Get blocks
		blocks1 := alloc.Blocks()
		require.Len(t, blocks1, 1)

		// Modify the returned slice
		blocks1[0].Size = 999
		blocks1[0].Offset = 888

		// Get blocks again - should be unchanged
		blocks2 := alloc.Blocks()
		require.Len(t, blocks2, 1)
		assert.Equal(t, uint64(100), blocks2[0].Size, "size should be unchanged")
		assert.Equal(t, uint64(0), blocks2[0].Offset, "offset should be unchanged")
	})

	t.Run("blocks with many allocations", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate 100 blocks
		for i := 0; i < 100; i++ {
			_, _ = alloc.Allocate(10)
		}

		blocks := alloc.Blocks()
		assert.Len(t, blocks, 100)

		// Verify all blocks are present and sorted
		for i := 0; i < 100; i++ {
			expectedOffset := uint64(i * 10)
			assert.Equal(t, expectedOffset, blocks[i].Offset)
			assert.Equal(t, uint64(10), blocks[i].Size)
		}
	})
}

// TestAllocator_ValidateNoOverlaps_Complete tests overlap validation.
func TestAllocator_ValidateNoOverlaps_Complete(t *testing.T) {
	t.Run("validates sequential allocations pass", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate many sequential blocks
		for i := 0; i < 100; i++ {
			_, _ = alloc.Allocate(100)
		}

		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err, "sequential allocations should never overlap")
	})

	t.Run("detects overlaps if blocks are manually corrupted", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate normal blocks
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(100)

		// Manually corrupt internal state to create overlap (for testing detection)
		// This simulates a bug in allocation logic
		alloc.blocks[1].Offset = 50 // Overlaps with first block [0, 100)

		err := alloc.ValidateNoOverlaps()
		assert.Error(t, err, "should detect overlap")
		assert.Contains(t, err.Error(), "overlap detected")
	})

	t.Run("validates empty allocator", func(t *testing.T) {
		alloc := NewAllocator(0)
		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)
	})

	t.Run("validates single block", func(t *testing.T) {
		alloc := NewAllocator(0)
		_, _ = alloc.Allocate(100)

		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err)
	})

	t.Run("validates adjacent blocks (no gaps)", func(t *testing.T) {
		alloc := NewAllocator(0)

		// These blocks are perfectly adjacent [0,100), [100,200), [200,250)
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(100)
		_, _ = alloc.Allocate(50)

		err := alloc.ValidateNoOverlaps()
		assert.NoError(t, err, "adjacent blocks should not be considered overlapping")
	})
}

// TestAllocator_EdgeCases tests various edge cases.
func TestAllocator_EdgeCases(t *testing.T) {
	t.Run("allocate size 1", func(t *testing.T) {
		alloc := NewAllocator(0)
		addr, err := alloc.Allocate(1)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), addr)
		assert.Equal(t, uint64(1), alloc.EndOfFile())
	})

	t.Run("allocate max uint64 size", func(t *testing.T) {
		alloc := NewAllocator(0)

		// This might cause overflow in real implementation, but test it
		// In practice, filesystems can't handle this, but allocator should try
		size := uint64(1<<63 - 1) // Very large but not overflow-causing
		addr, err := alloc.Allocate(size)

		// Should succeed (allocator doesn't validate size limits in MVP)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), addr)
	})

	t.Run("allocate from non-zero initial offset", func(t *testing.T) {
		initialOffset := uint64(12345)
		alloc := NewAllocator(initialOffset)

		addr, err := alloc.Allocate(100)
		require.NoError(t, err)
		assert.Equal(t, initialOffset, addr)
		assert.Equal(t, initialOffset+100, alloc.EndOfFile())
	})

	t.Run("many allocations preserve order", func(t *testing.T) {
		alloc := NewAllocator(0)

		// Allocate with varying sizes
		sizes := []uint64{10, 20, 5, 100, 1, 50}
		addrs := make([]uint64, len(sizes))

		for i, size := range sizes {
			addr, err := alloc.Allocate(size)
			require.NoError(t, err)
			addrs[i] = addr
		}

		// Verify addresses are in ascending order (sequential allocation)
		for i := 0; i < len(addrs)-1; i++ {
			assert.Less(t, addrs[i], addrs[i+1],
				"addresses should be sequential and ascending")
		}
	})
}

// TestAllocator_GetTotalAllocated tests total space tracking.
func TestAllocator_GetTotalAllocated(t *testing.T) {
	t.Run("empty allocator", func(t *testing.T) {
		alloc := NewAllocator(100)

		// Get blocks and calculate total
		blocks := alloc.Blocks()
		var total uint64
		for _, block := range blocks {
			total += block.Size
		}

		assert.Equal(t, uint64(0), total)
	})

	t.Run("after allocations", func(t *testing.T) {
		alloc := NewAllocator(100)

		sizes := []uint64{100, 200, 50}
		for _, size := range sizes {
			_, _ = alloc.Allocate(size)
		}

		// Get blocks and calculate total
		blocks := alloc.Blocks()
		var total uint64
		for _, block := range blocks {
			total += block.Size
		}

		expectedTotal := uint64(100 + 200 + 50)
		assert.Equal(t, expectedTotal, total)
	})
}

// TestAllocator_IsAllocated_Comprehensive adds more overlap tests.
func TestAllocator_IsAllocated_Comprehensive(t *testing.T) {
	alloc := NewAllocator(0)

	// Create blocks: [0,100), [100,300), [300,350)
	_, _ = alloc.Allocate(100)
	_, _ = alloc.Allocate(200)
	_, _ = alloc.Allocate(50)

	tests := []struct {
		name     string
		offset   uint64
		size     uint64
		expected bool
	}{
		// Full containment
		{"contains entire first block", 0, 100, true},
		{"contains entire second block", 100, 200, true},
		{"contains all blocks", 0, 350, true},

		// Partial containment
		{"partially overlaps first", 50, 100, true},
		{"spans two blocks", 50, 150, true},
		{"spans all blocks", 0, 400, true},

		// No overlap - before
		{"way before (impossible in our sequential scheme)", 0, 0, false},

		// No overlap - after
		{"just after last block", 350, 100, false},
		{"way after last block", 1000, 100, false},

		// Edge cases - touching boundaries
		{"starts at block end (next block start)", 100, 50, true}, // Overlaps second block
		{"ends at block start", 0, 0, false},                      // Zero size
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alloc.IsAllocated(tt.offset, tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestAllocator_ConcurrentAccess documents thread-safety limitation.
//
// This test is skipped because the allocator is NOT thread-safe in MVP.
// Thread safety will be added in v0.11.0-RC if needed.
//
// If thread safety were added, this test would verify:
//   - Concurrent allocations produce unique addresses
//   - No overlaps occur with concurrent access
//   - All allocations are tracked correctly
func TestAllocator_ConcurrentAccess(t *testing.T) {
	t.Skip("Allocator is NOT thread-safe in MVP - this is a documented limitation")

	// This test is skipped but documents the expected behavior.
	// In v0.11.0-RC, we might add thread safety and enable this test.
}

// BenchmarkAllocate_Sequential benchmarks sequential allocations.
func BenchmarkAllocate_Sequential(b *testing.B) {
	alloc := NewAllocator(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = alloc.Allocate(1024)
	}
}

// BenchmarkBlocks benchmarks retrieving all blocks.
func BenchmarkBlocks(b *testing.B) {
	alloc := NewAllocator(0)

	// Pre-allocate 1000 blocks
	for i := 0; i < 1000; i++ {
		_, _ = alloc.Allocate(1024)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = alloc.Blocks()
	}
}

// BenchmarkValidateNoOverlaps benchmarks overlap validation.
func BenchmarkValidateNoOverlaps(b *testing.B) {
	alloc := NewAllocator(0)

	// Pre-allocate 1000 blocks
	for i := 0; i < 1000; i++ {
		_, _ = alloc.Allocate(1024)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = alloc.ValidateNoOverlaps()
	}
}
