package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// le16s encodes values as consecutive little-endian uint16s.
func le16s(vals ...uint16) []byte {
	b := make([]byte, 2*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	return b
}

// le32s encodes values as consecutive little-endian uint32s.
func le32s(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[4*i:], v)
	}
	return b
}

// TestParseFilterPipeline_V2 decodes version 2 messages laid out as
// H5O__pline_encode writes them (H5Opline.c:285-340): a filter below 256 has
// no name length field, a filter at or above 256 has one and an unpadded name,
// client data is unpadded.
//
// Red on 40c6d35: the decoder demanded 8 header bytes per version 2 filter and
// never read a version 2 name, so fletcher32 last (6 bytes) was "truncated"
// and an LZF filter (id 32000) was misread.
func TestParseFilterPipeline_V2(t *testing.T) {
	t.Run("shuffle deflate fletcher32", func(t *testing.T) {
		msg := cat([]byte{2, 3},
			le16s(uint16(FilterShuffle), 0, 1), le32s(4),
			le16s(uint16(FilterDeflate), 0, 1), le32s(6),
			le16s(uint16(FilterFletcher), 0, 0))
		p, err := ParseFilterPipelineMessage(msg)
		require.NoError(t, err)
		require.Len(t, p.Filters, 3)
		require.Equal(t, []uint32{4}, p.Filters[0].ClientData)
		require.Equal(t, []uint32{6}, p.Filters[1].ClientData)
		require.Equal(t, FilterFletcher, p.Filters[2].ID)
		require.Empty(t, p.Filters[2].ClientData)
	})

	t.Run("third-party filter with a name", func(t *testing.T) {
		name := []byte("lzf\x00")
		msg := cat([]byte{2, 2},
			le16s(uint16(FilterLZF), uint16(len(name)), 1, 3), name, le32s(4, 261, 0),
			le16s(uint16(FilterFletcher), 0, 0))
		p, err := ParseFilterPipelineMessage(msg)
		require.NoError(t, err)
		require.Len(t, p.Filters, 2)
		require.Equal(t, FilterLZF, p.Filters[0].ID)
		require.Equal(t, "lzf", p.Filters[0].Name)
		require.Equal(t, uint16(1), p.Filters[0].Flags)
		require.Equal(t, []uint32{4, 261, 0}, p.Filters[0].ClientData)
		require.Equal(t, FilterFletcher, p.Filters[1].ID)
	})
}

// TestParseFilterPipeline_CChecks covers the range checks of
// H5O__pline_decode; each violation is utils.ErrCorrupt.
func TestParseFilterPipeline_CChecks(t *testing.T) {
	cases := map[string][]byte{
		"bad version":            {3, 0},
		"more than 32 filters":   {2, 33},
		"v1 reserved truncated":  {1, 0, 0, 0},
		"v1 name not 8-multiple": cat([]byte{1, 1, 0, 0, 0, 0, 0, 0}, le16s(1, 5, 0, 0), []byte("abcd\x00")),
		"name not terminated":    cat([]byte{2, 1}, le16s(300, 4, 0, 0), []byte("abcd")),
		"name past the end":      cat([]byte{2, 1}, le16s(300, 16, 0, 0), []byte("ab\x00")),
		"v2 flags truncated":     cat([]byte{2, 1}, le16s(uint16(FilterDeflate)), []byte{0}),
		"client data truncated":  cat([]byte{2, 1}, le16s(uint16(FilterDeflate), 0, 2), le32s(6)),
		"huge client data count": cat([]byte{2, 1}, le16s(uint16(FilterDeflate), 0, 0xFFFF)),
		"v1 padding missing":     cat([]byte{1, 1, 0, 0, 0, 0, 0, 0}, le16s(1, 0, 0, 1), le32s(6)),
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFilterPipelineMessage(msg)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}

	// Version 1 with padding, as C writes it, still decodes.
	ok := cat([]byte{1, 1, 0, 0, 0, 0, 0, 0}, le16s(1, 8, 0, 1), []byte("deflate\x00"), le32s(6, 0))
	p, err := ParseFilterPipelineMessage(ok)
	require.NoError(t, err)
	require.Equal(t, "deflate", p.Filters[0].Name)
	require.Equal(t, []uint32{6}, p.Filters[0].ClientData)
}
