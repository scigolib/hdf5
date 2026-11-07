package writer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFree_RefusesOverlapWithFreeSpace: Free refuses a block that overlaps
// space already free, or passes the end of the file, and leaves the
// allocator as it was. Without the check a crafted file whose structures
// share space (review R5: the blocks a delete frees and those the unlink from
// the parent frees) puts overlapping blocks on the free list, and two later
// allocations get the same bytes. Red before the change: Free accepted all
// three.
func TestFree_RefusesOverlapWithFreeSpace(t *testing.T) {
	alloc := NewAllocator(0)
	for range 4 {
		_, err := alloc.Allocate(100)
		require.NoError(t, err)
	}
	require.NoError(t, alloc.Free(100, 100))
	free0, eof0 := alloc.FreeBlocks(), alloc.EndOfFile()

	for _, tc := range []struct {
		name         string
		offset, size uint64
	}{
		{"same block twice", 100, 100},
		{"overlaps its end", 150, 100},
		{"overlaps its start", 50, 100},
		{"passes the end of the file", 350, 100},
	} {
		require.Error(t, alloc.Free(tc.offset, tc.size), tc.name)
		require.Equal(t, free0, alloc.FreeBlocks(), "%s: the free list changed", tc.name)
		require.Equal(t, eof0, alloc.EndOfFile(), "%s: the end of file changed", tc.name)
	}

	// Adjacent blocks still free and coalesce.
	require.NoError(t, alloc.Free(0, 100))
	require.NoError(t, alloc.Free(200, 100))
	require.Equal(t, []FreeBlock{{Offset: 0, Size: 300}}, alloc.FreeBlocks())
}
