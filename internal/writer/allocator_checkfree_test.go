package writer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllocator_CheckFree checks the dry run Delete uses before it frees: a
// refused batch leaves the allocator unchanged, an accepted one is freeable.
func TestAllocator_CheckFree(t *testing.T) {
	// Layout: [100,200) [200,300) [300,400) [400,500) allocated, then
	// [200,300) freed, so the free list holds it and the end is 500.
	newAlloc := func(t *testing.T) *Allocator {
		t.Helper()
		a := NewAllocator(100)
		for range 4 {
			_, err := a.Allocate(100)
			require.NoError(t, err)
		}
		require.NoError(t, a.Free(200, 100))
		return a
	}

	tests := []struct {
		name    string
		ranges  []FreeBlock
		wantErr string
	}{
		{name: "empty batch", ranges: nil},
		{name: "disjoint, touching", ranges: []FreeBlock{{Offset: 300, Size: 100}, {Offset: 100, Size: 100}, {Offset: 400, Size: 100}}},
		{name: "adjacent to free block", ranges: []FreeBlock{{Offset: 150, Size: 50}, {Offset: 300, Size: 10}}},
		{name: "zero size", ranges: []FreeBlock{{Offset: 100, Size: 0}}, wantErr: "zero bytes"},
		{name: "past end of file", ranges: []FreeBlock{{Offset: 450, Size: 51}}, wantErr: "passes the end"},
		{name: "address overflow", ranges: []FreeBlock{{Offset: ^uint64(0) - 1, Size: 4}}, wantErr: "passes the end"},
		{name: "overlaps free block", ranges: []FreeBlock{{Offset: 150, Size: 51}}, wantErr: "overlaps free space"},
		{name: "inside free block", ranges: []FreeBlock{{Offset: 220, Size: 10}}, wantErr: "overlaps free space"},
		{name: "overlap within batch", ranges: []FreeBlock{{Offset: 300, Size: 100}, {Offset: 350, Size: 10}}, wantErr: "freed with it"},
		{name: "duplicate in batch", ranges: []FreeBlock{{Offset: 100, Size: 100}, {Offset: 100, Size: 100}}, wantErr: "freed with it"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAlloc(t)
			before := a.FreeBlocks()
			eof := a.EndOfFile()

			err := a.CheckFree(tt.ranges)
			require.Equal(t, before, a.FreeBlocks(), "CheckFree changed the free list")
			require.Equal(t, eof, a.EndOfFile(), "CheckFree changed the end of file")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			for _, r := range tt.ranges {
				require.NoError(t, a.Free(r.Offset, r.Size), "Free(%d, %d) after CheckFree", r.Offset, r.Size)
			}
		})
	}
}
