package writer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllocator_TryExtend covers the cases of H5MF_try_extend (H5MF.c:1250):
// a block at the end of the file, a block followed by a large-enough free
// section, a too-small free section, and a block with no free neighbor.
func TestAllocator_TryExtend(t *testing.T) {
	t.Run("block at EOF", func(t *testing.T) {
		a := NewAllocator(100)
		addr, err := a.Allocate(50)
		require.NoError(t, err)

		require.True(t, a.TryExtend(addr, 50, 30))
		require.Equal(t, uint64(180), a.EndOfFile())
		require.Equal(t, []AllocatedBlock{{Offset: 100, Size: 80}}, a.Blocks())

		next, err := a.Allocate(10)
		require.NoError(t, err)
		require.Equal(t, uint64(180), next, "the extension is not handed out again")
		require.NoError(t, a.ValidateNoOverlaps())
	})

	t.Run("followed by a large enough free block", func(t *testing.T) {
		a := NewAllocator(0)
		first, _ := a.Allocate(40)
		second, _ := a.Allocate(64)
		_, _ = a.Allocate(8) // keeps the freed block away from EOF
		require.NoError(t, a.Free(second, 64))

		require.True(t, a.TryExtend(first, 40, 24))
		require.Equal(t, []FreeBlock{{Offset: 64, Size: 40}}, a.FreeBlocks())
		require.NoError(t, a.ValidateNoOverlaps())

		require.True(t, a.TryExtend(first, 64, 40), "an exact fit takes the whole free block")
		require.Empty(t, a.FreeBlocks())
		require.NoError(t, a.ValidateNoOverlaps())
	})

	t.Run("free block too small", func(t *testing.T) {
		a := NewAllocator(0)
		first, _ := a.Allocate(40)
		second, _ := a.Allocate(16)
		_, _ = a.Allocate(8)
		require.NoError(t, a.Free(second, 16))

		require.False(t, a.TryExtend(first, 40, 17))
		require.Equal(t, []FreeBlock{{Offset: 40, Size: 16}}, a.FreeBlocks(), "nothing changes on failure")
		require.Equal(t, uint64(64), a.EndOfFile())
	})

	t.Run("no free neighbor", func(t *testing.T) {
		a := NewAllocator(0)
		first, _ := a.Allocate(40)
		_, _ = a.Allocate(16)
		require.False(t, a.TryExtend(first, 40, 1))
		require.Equal(t, uint64(56), a.EndOfFile())
		require.Empty(t, a.FreeBlocks())
	})

	t.Run("untracked block at EOF of an opened file", func(t *testing.T) {
		a := NewAllocator(4096) // a file opened for writing: its content is not tracked
		require.True(t, a.TryExtend(4000, 96, 64))
		require.Equal(t, uint64(4160), a.EndOfFile())
		require.Equal(t, []AllocatedBlock{{Offset: 4096, Size: 64}}, a.Blocks())
	})
}
