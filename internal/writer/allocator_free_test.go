package writer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TASK-042: Allocator.Free() tests
// ---------------------------------------------------------------------------

func TestFree_BasicReuse(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate 3 blocks: [0,100), [100,300), [300,400).
	a1, err := alloc.Allocate(100)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), a1)

	a2, err := alloc.Allocate(200)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), a2)

	a3, err := alloc.Allocate(100)
	require.NoError(t, err)
	assert.Equal(t, uint64(300), a3)

	assert.Equal(t, uint64(400), alloc.EndOfFile())

	// Free the middle block [100,300).
	err = alloc.Free(100, 200)
	require.NoError(t, err)

	// Free list should have one entry.
	freeBlocks := alloc.FreeBlocks()
	require.Len(t, freeBlocks, 1)
	assert.Equal(t, uint64(100), freeBlocks[0].Offset)
	assert.Equal(t, uint64(200), freeBlocks[0].Size)

	// EOF should remain 400 (freed block is not at the tail).
	assert.Equal(t, uint64(400), alloc.EndOfFile())

	// Allocate a block that fits in the freed space (exact fit).
	a4, err := alloc.Allocate(200)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), a4, "should reuse freed block")

	// Free list should now be empty.
	assert.Empty(t, alloc.FreeBlocks())
}

func TestFree_PartialReuse(t *testing.T) {
	alloc := NewAllocator(0)

	_, err := alloc.Allocate(100)
	require.NoError(t, err)
	_, err = alloc.Allocate(200)
	require.NoError(t, err)
	_, err = alloc.Allocate(100)
	require.NoError(t, err)

	// Free middle block.
	err = alloc.Free(100, 200)
	require.NoError(t, err)

	// Allocate smaller block — should use part of the freed space.
	a, err := alloc.Allocate(50)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), a, "should reuse start of freed block")

	// Free list should have remainder.
	freeBlocks := alloc.FreeBlocks()
	require.Len(t, freeBlocks, 1)
	assert.Equal(t, uint64(150), freeBlocks[0].Offset, "remainder starts at 150")
	assert.Equal(t, uint64(150), freeBlocks[0].Size, "remainder is 150 bytes")
}

func TestFree_EOFShrink(t *testing.T) {
	alloc := NewAllocator(0)

	_, err := alloc.Allocate(100)
	require.NoError(t, err)
	_, err = alloc.Allocate(200)
	require.NoError(t, err)

	assert.Equal(t, uint64(300), alloc.EndOfFile())

	// Free the last block [100,300). This should shrink EOF.
	err = alloc.Free(100, 200)
	require.NoError(t, err)

	assert.Equal(t, uint64(100), alloc.EndOfFile(), "EOF should shrink when freeing tail block")
	assert.Empty(t, alloc.FreeBlocks(), "no free list entries needed after EOF shrink")
}

func TestFree_CoalesceAdjacent(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate 3 blocks: [0,100), [100,200), [200,300), [300,400).
	for i := 0; i < 4; i++ {
		_, err := alloc.Allocate(100)
		require.NoError(t, err)
	}

	// Free [100,200) then [200,300). They should coalesce.
	err := alloc.Free(100, 100)
	require.NoError(t, err)

	freeBlocks := alloc.FreeBlocks()
	require.Len(t, freeBlocks, 1)
	assert.Equal(t, uint64(100), freeBlocks[0].Offset)
	assert.Equal(t, uint64(100), freeBlocks[0].Size)

	err = alloc.Free(200, 100)
	require.NoError(t, err)

	freeBlocks = alloc.FreeBlocks()
	require.Len(t, freeBlocks, 1, "should coalesce into one block")
	assert.Equal(t, uint64(100), freeBlocks[0].Offset)
	assert.Equal(t, uint64(200), freeBlocks[0].Size, "coalesced block is 200 bytes")
}

func TestFree_CoalesceReverse(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate 3 blocks: [0,100), [100,200), [200,300), [300,400).
	for i := 0; i < 4; i++ {
		_, err := alloc.Allocate(100)
		require.NoError(t, err)
	}

	// Free [200,300) first, then [100,200). Should still coalesce.
	require.NoError(t, alloc.Free(200, 100))
	require.NoError(t, alloc.Free(100, 100))

	freeBlocks := alloc.FreeBlocks()
	require.Len(t, freeBlocks, 1, "should coalesce even when freed in reverse order")
	assert.Equal(t, uint64(100), freeBlocks[0].Offset)
	assert.Equal(t, uint64(200), freeBlocks[0].Size)
}

func TestFree_CoalesceWithEOFShrink(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate 3 blocks: [0,100), [100,200), [200,300).
	for i := 0; i < 3; i++ {
		_, err := alloc.Allocate(100)
		require.NoError(t, err)
	}

	// Free [100,200) (goes to free list).
	require.NoError(t, alloc.Free(100, 100))
	assert.Equal(t, uint64(300), alloc.EndOfFile())

	// Free [200,300) (at EOF). This shrinks EOF to 200.
	// Then the trailing free block [100,200) is also at the new tail, so EOF shrinks to 100.
	require.NoError(t, alloc.Free(200, 100))

	assert.Equal(t, uint64(100), alloc.EndOfFile(), "should cascade-shrink through free list")
	assert.Empty(t, alloc.FreeBlocks(), "all free blocks absorbed into EOF shrink")
}

func TestFree_ZeroSize(t *testing.T) {
	alloc := NewAllocator(0)
	err := alloc.Free(100, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot free zero bytes")
}

func TestFree_BestFit(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate 5 blocks of different sizes interspersed with padding.
	_, _ = alloc.Allocate(50)  // [0,50)     — keep
	_, _ = alloc.Allocate(200) // [50,250)   — will free (large gap)
	_, _ = alloc.Allocate(50)  // [250,300)  — keep
	_, _ = alloc.Allocate(100) // [300,400)  — will free (medium gap)
	_, _ = alloc.Allocate(50)  // [400,450)  — keep
	_, _ = alloc.Allocate(60)  // [450,510)  — will free (small gap)
	_, _ = alloc.Allocate(50)  // [510,560)  — keep

	// Free 3 blocks to create gaps of different sizes.
	require.NoError(t, alloc.Free(50, 200))  // 200 byte gap
	require.NoError(t, alloc.Free(300, 100)) // 100 byte gap
	require.NoError(t, alloc.Free(450, 60))  // 60 byte gap

	// Allocate 55 bytes — should pick the 60-byte gap (best fit).
	addr, err := alloc.Allocate(55)
	require.NoError(t, err)
	assert.Equal(t, uint64(450), addr, "should pick best-fit (60-byte gap for 55-byte request)")

	// Allocate 100 bytes — should pick the 100-byte gap (exact fit).
	addr, err = alloc.Allocate(100)
	require.NoError(t, err)
	assert.Equal(t, uint64(300), addr, "should pick exact-fit 100-byte gap")
}

func TestFree_MultipleFreeAndReuse(t *testing.T) {
	alloc := NewAllocator(0)

	// Allocate and free multiple times to test robustness.
	for i := 0; i < 10; i++ {
		addr, err := alloc.Allocate(100)
		require.NoError(t, err)

		if i%2 == 0 {
			require.NoError(t, alloc.Free(addr, 100))
		}
	}

	// Should have no validation errors.
	err := alloc.ValidateNoOverlaps()
	assert.NoError(t, err)
}

func TestFreeBlocks_ReturnsCopy(t *testing.T) {
	alloc := NewAllocator(0)
	_, _ = alloc.Allocate(100)
	_, _ = alloc.Allocate(100)
	_, _ = alloc.Allocate(100) // keep this one

	require.NoError(t, alloc.Free(0, 100))

	fb1 := alloc.FreeBlocks()
	require.Len(t, fb1, 1)

	// Modify the returned slice.
	fb1[0].Size = 9999

	// Get again — should be unmodified.
	fb2 := alloc.FreeBlocks()
	require.Len(t, fb2, 1)
	assert.Equal(t, uint64(100), fb2[0].Size)
}

// Benchmark free + reuse cycle.
func BenchmarkFreeAndReuse(b *testing.B) {
	alloc := NewAllocator(0)

	// Pre-allocate a pool of blocks.
	addrs := make([]uint64, 100)
	for i := range addrs {
		addr, _ := alloc.Allocate(1024)
		addrs[i] = addr
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % len(addrs)
		_ = alloc.Free(addrs[idx], 1024)
		addr, _ := alloc.Allocate(1024)
		addrs[idx] = addr
	}
}
