package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

const filterFlagOptional = 0x0001 // H5Z_FLAG_OPTIONAL.

// TestApplyFilters_FailsLikeC: on read, H5Z_pipeline fails for a filter it
// does not have, optional or not (H5Z.c:1408-1434), and for a filter that
// fails (H5Z.c:1475-1490). Red before TASK-068 (e): an optional filter was
// skipped and the still-encoded bytes were returned as data.
func TestApplyFilters_FailsLikeC(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	for _, id := range []FilterID{FilterNBit, FilterScaleOffset, FilterSZIP, 999} {
		for _, flags := range []uint16{0, filterFlagOptional} {
			fp := &FilterPipelineMessage{Filters: []Filter{{ID: id, Flags: flags}}}
			_, err := fp.ApplyFilters(data, 1024)
			require.Error(t, err, "filter %d flags %d", id, flags)
			require.ErrorIs(t, err, ErrFilterNotSupported)
			require.NotErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), filterName(id))
		}
	}

	// A supported filter that cannot decode the bytes: corruption, optional
	// or not.
	for _, flags := range []uint16{0, filterFlagOptional} {
		fp := &FilterPipelineMessage{Filters: []Filter{{ID: FilterDeflate, Flags: flags}}}
		_, err := fp.ApplyFilters(data, 1024)
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.False(t, errors.Is(err, ErrFilterNotSupported))
	}

	// Shuffle with a parameter count C rejects (H5Zshuffle.c:125-126).
	for _, cd := range [][]uint32{nil, {0}, {2, 2}} {
		fp := &FilterPipelineMessage{Filters: []Filter{{ID: FilterShuffle, Flags: filterFlagOptional, ClientData: cd}}}
		_, err := fp.ApplyFilters(data, 1024)
		require.ErrorIs(t, err, utils.ErrCorrupt, "cd %v", cd)
	}
}

// TestApplyFiltersMasked: a chunk's filter mask names the filters that were
// not applied when it was written (an optional filter that failed), and
// H5Z_pipeline skips exactly those on read (H5Z.c:1400-1403). Red before
// TASK-068 (e): the mask was ignored, which binfp64.h5 and binin8.h5 (raw
// chunks behind an optional deflate) hit once failures stopped being skipped.
func TestApplyFiltersMasked(t *testing.T) {
	raw := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	fp := &FilterPipelineMessage{Filters: []Filter{
		{ID: FilterShuffle, Flags: filterFlagOptional, ClientData: []uint32{2}},
		{ID: FilterDeflate, Flags: filterFlagOptional},
	}}

	// Deflate (index 1) skipped at write time: only the shuffle is undone.
	got, err := fp.ApplyFiltersMasked(raw, 1<<1, 1024)
	require.NoError(t, err)
	want, err := applyShuffle(raw, []uint32{2})
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Both skipped: the bytes are the data.
	got, err = fp.ApplyFiltersMasked(raw, 0b11, 1024)
	require.NoError(t, err)
	require.Equal(t, raw, got)

	// No mask: deflate must decode, and these bytes are not a zlib stream.
	_, err = fp.ApplyFiltersMasked(raw, 0, 1024)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestApplyShuffle_C covers the cases H5Z__filter_shuffle handles without
// failing (H5Zshuffle.c:133-136, 208-213): fewer than two elements or a
// 1-byte type leave the buffer as it is, and bytes past the last whole
// element are copied unchanged.
func TestApplyShuffle_C(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		size uint32
		want []byte
	}{
		"leftover":      {data: []byte{1, 2, 3, 0xA, 0xB, 0xC, 9}, size: 2, want: []byte{1, 0xA, 2, 0xB, 3, 0xC, 9}},
		"one element":   {data: []byte{1, 2, 3}, size: 2, want: []byte{1, 2, 3}},
		"size > buffer": {data: []byte{1, 2, 3, 4}, size: 100, want: []byte{1, 2, 3, 4}},
		"1-byte type":   {data: []byte{1, 2, 3}, size: 1, want: []byte{1, 2, 3}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := applyShuffle(tc.data, []uint32{tc.size})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
