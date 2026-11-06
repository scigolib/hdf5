package core

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestNotSupportedSentinel: every "valid HDF5 this library does not read yet"
// error matches utils.ErrNotSupported and neither ErrCorrupt nor ErrLimit, so
// a caller can tell the three apart with errors.Is. Red before TASK-068: the
// sentinel did not exist.
func TestNotSupportedSentinel(t *testing.T) {
	for name, err := range map[string]error{
		"filter":           ErrFilterNotSupported,
		"SOHM heap":        errSOHMNotSupported,
		"committed lookup": errSharedDatatype,
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, err, utils.ErrNotSupported)
			require.NotErrorIs(t, err, utils.ErrCorrupt)
			require.NotErrorIs(t, err, utils.ErrLimit)
		})
	}

	isNotSupported := func(t *testing.T, err error) {
		t.Helper()
		require.ErrorIs(t, err, utils.ErrNotSupported)
		require.NotErrorIs(t, err, utils.ErrCorrupt)
	}
	isCorrupt := func(t *testing.T, err error) {
		t.Helper()
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.NotErrorIs(t, err, utils.ErrNotSupported)
	}

	t.Run("layout version", func(t *testing.T) {
		sb := &Superblock{Version: 2, OffsetSize: 8, LengthSize: 8}
		// Version 5 (HDF5 2.0, H5Oprivate.h:448-455) is decoded since TASK-058;
		// 6 is refused by C (H5Olayout.c:103-104).
		_, err := ParseDataLayoutMessage(append([]byte{6}, make([]byte, 32)...), sb)
		isCorrupt(t, err)
	})

	t.Run("superblock version", func(t *testing.T) {
		img := func(v byte) *bytes.Reader {
			b := make([]byte, 128)
			copy(b, Signature)
			b[8] = v
			return bytes.NewReader(b)
		}
		// Version 1 is valid (H5Fprivate.h:301); above 3 C refuses it
		// (H5Fsuper_cache.c:164-165).
		_, err := ReadSuperblock(img(1))
		isNotSupported(t, err)
		_, err = ReadSuperblock(img(4))
		isCorrupt(t, err)
	})

	t.Run("fractal heap ID type", func(t *testing.T) {
		// Huge and tiny objects are valid (H5HFpkg.h:141-144); type 3 is
		// reserved. Tiny objects are read (TASK-058), and huge objects whose
		// ID holds their address; a 7-byte ID cannot, so the object is
		// tracked by a v2 B-tree, which is not read.
		s := defaultFheapSpec()
		s.idLen = 7
		h, err := OpenFractalHeap(bytes.NewReader(s.bytes()), fheapTestSB, 0)
		require.NoError(t, err)
		_, err = h.Object([]byte{0x10, 1, 0, 0, 0, 0, 0})
		isNotSupported(t, err)
		got, err := h.Object([]byte{0x20, 't', 0, 0, 0, 0, 0})
		require.NoError(t, err)
		require.Equal(t, []byte("t"), got)
		_, err = h.Object([]byte{0x30, 0, 0, 0, 0, 0, 0})
		isCorrupt(t, err)
	})

	t.Run("chunk index", func(t *testing.T) {
		// Every index type of the format is read (TASK-058); a type past
		// H5D_CHUNK_IDX_BT2 is not a format feature but corruption.
		d := &DatasetDescriptor{
			dataspace: &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{4}},
			layout:    &DataLayoutMessage{Class: LayoutChunked, ChunkIndexType: 6, ChunkSize: []uint64{4, 1}},
		}
		_, err := d.CollectChunks(bytes.NewReader(nil), &Superblock{}, utils.DefaultReadLimit)
		isCorrupt(t, err)
	})

	t.Run("conversion", func(t *testing.T) {
		_, err := convertToFloat64([]byte("ab"), &DatatypeMessage{Class: DatatypeString, Size: 2}, 1)
		isNotSupported(t, err)
	})

	// Through the read path: an N-bit chunk.
	fp := &FilterPipelineMessage{Filters: []Filter{{ID: FilterNBit, Flags: filterFlagOptional}}}
	_, err := fp.ApplyFilters([]byte{1, 2, 3, 4}, 16)
	require.ErrorIs(t, err, utils.ErrNotSupported)
	require.ErrorIs(t, err, ErrFilterNotSupported)
	require.NotErrorIs(t, err, utils.ErrCorrupt)
}
