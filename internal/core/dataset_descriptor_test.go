package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// Tests of the validated dataset descriptor (ADR-004 §1). Each validation rule
// cites the C check it mirrors; the error must wrap utils.ErrCorrupt so callers
// can tell damaged metadata from a refused read (utils.ErrLimit).

func descTestSB() *Superblock {
	return &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
}

func float64Type() *DatatypeMessage {
	return &DatatypeMessage{Class: DatatypeFloat, Size: 8, ClassBitField: 0x20}
}

func simpleSpace(dims ...uint64) *DataspaceMessage {
	return &DataspaceMessage{Type: DataspaceSimple, Dimensions: dims}
}

func chunkedLayout(dims ...uint64) *DataLayoutMessage {
	return &DataLayoutMessage{Version: 3, Class: LayoutChunked, ChunkSize: dims, ChunkIndexType: ChunkIndexBTREE}
}

func TestDatasetDescriptor_RejectsZeroDatatypeSize(t *testing.T) {
	// H5Dcontig.c:388 / H5Dcompact.c:253: a zero-sized datatype is rejected.
	dt := &DatatypeMessage{Class: DatatypeFloat, Size: 0}
	layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: 0}

	_, err := newDatasetDescriptor(bytes.NewReader(make([]byte, 64)), descTestSB(), dt, simpleSpace(4), layout, nil)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "datatype size is zero")
}

func TestDatasetDescriptor_RejectsDataspaceOverflow(t *testing.T) {
	// DataspaceMessage.TotalElements wraps silently; the descriptor must not.
	layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: 0}

	_, err := newDatasetDescriptor(bytes.NewReader(nil), descTestSB(), float64Type(),
		simpleSpace(1<<32, 1<<32), layout, nil)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "element count overflows")
}

func TestDatasetDescriptor_RejectsStorageOverflow(t *testing.T) {
	// H5Dcontig.c:392-396: nelmts × dt_size overflow.
	layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: 0}

	_, err := newDatasetDescriptor(bytes.NewReader(nil), descTestSB(), float64Type(),
		simpleSpace(1<<61), layout, nil)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "dataset size overflow")
}

func TestDatasetDescriptor_Contiguous(t *testing.T) {
	r := bytes.NewReader(make([]byte, 128))

	t.Run("inside the file", func(t *testing.T) {
		layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: 32}
		desc, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(12), layout, nil)
		require.NoError(t, err)
		require.Equal(t, uint64(96), desc.StorageSize())
		require.Equal(t, uint64(12), desc.NumElements())
		require.Equal(t, uint64(8), desc.ElementSize())
		require.Nil(t, desc.ChunkDims())
	})

	t.Run("past the end of the file", func(t *testing.T) {
		// H5Dcontig.c:408-411.
		layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: 40}
		_, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(12), layout, nil)
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.Contains(t, err.Error(), "read beyond end of file")
	})

	t.Run("unallocated storage", func(t *testing.T) {
		// H5Dcontig.c:399: the region is checked only for a defined address,
		// so the descriptor is valid; reading it is refused before allocating.
		layout := &DataLayoutMessage{Version: 3, Class: LayoutContiguous, DataAddress: undefinedAddress}
		desc, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(1<<40), layout, nil)
		require.NoError(t, err)

		_, err = desc.ResultSize(desc.NumElements(), ResultFloat64, utils.DefaultReadLimit)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not allocated")
	})
}

func TestDatasetDescriptor_CompactSizeMustMatch(t *testing.T) {
	// H5Dcompact.c:253-270: stored size must equal nelmts × dt_size exactly.
	for _, tc := range []struct {
		name   string
		stored int
		ok     bool
	}{
		{"exact", 32, true},
		{"short", 24, false},
		{"long", 40, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := &DataLayoutMessage{Version: 3, Class: LayoutCompact, CompactData: make([]byte, tc.stored)}
			_, err := newDatasetDescriptor(bytes.NewReader(nil), descTestSB(), float64Type(), simpleSpace(4), layout, nil)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), "size of compact dataset's data buffer")
		})
	}
}

func TestDatasetDescriptor_Chunked(t *testing.T) {
	r := bytes.NewReader(nil)

	cases := []struct {
		name   string
		space  *DataspaceMessage
		layout *DataLayoutMessage
		want   string // Empty: valid.
	}{
		{"valid", simpleSpace(100, 200), chunkedLayout(10, 20, 8), ""},
		{"element size dimension mismatch", simpleSpace(12), chunkedLayout(4, 4), "does not match datatype description"},
		{"grid shorter than rank+1", simpleSpace(8, 8), chunkedLayout(4, 8), "needs 3"},
		{"grid longer than rank+1", simpleSpace(8), chunkedLayout(4, 4, 8), "needs 2"},
		{"zero chunk dimension", simpleSpace(8), chunkedLayout(0, 8), "chunk dimension 0 is zero"},
		{"scalar dataspace", &DataspaceMessage{Type: DataspaceScalar}, chunkedLayout(8), "simple dataspace"},
		{"over 4 GB with v1 B-tree", simpleSpace(1 << 40), chunkedLayout(1<<30, 8), "< 4GB with v1 b-tree"},
		{"chunk size product overflow", simpleSpace(8, 8), chunkedLayout(1<<40, 1<<40, 8), "chunk size overflows"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newDatasetDescriptor(r, descTestSB(), float64Type(), tc.space, tc.layout, nil)
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("over the ADR-003 chunk cap", func(t *testing.T) {
		// 2 GB chunk with a v2 B-tree index: C accepts it, the Go reader caps
		// whole-chunk allocations at utils.MaxChunkSize (documented deviation).
		layout := chunkedLayout(1<<28, 8)
		layout.Version = 4
		layout.ChunkIndexType = ChunkIndexBT2
		_, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(1<<30), layout, nil)
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.Contains(t, err.Error(), "exceeds the maximum")
	})

	t.Run("chunk dims without element size", func(t *testing.T) {
		desc, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(100, 200), chunkedLayout(10, 20, 8), nil)
		require.NoError(t, err)
		require.Equal(t, []uint64{10, 20}, desc.ChunkDims())
	})
}

func TestDatasetDescriptor_ResultSize(t *testing.T) {
	r := bytes.NewReader(nil)

	t.Run("chunked over the read limit wraps ErrLimit", func(t *testing.T) {
		// A sparse 8 MB chunked dataset in an empty file: legitimate, but more
		// than the 1 MB limit given here.
		desc, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(1<<20), chunkedLayout(1024, 8), nil)
		require.NoError(t, err)

		_, err = desc.ResultSize(desc.NumElements(), 1, 1<<20)
		require.ErrorIs(t, err, utils.ErrLimit)
		require.NotErrorIs(t, err, utils.ErrCorrupt)
		require.Contains(t, err.Error(), "8388608 bytes")
		require.Contains(t, err.Error(), "read limit of 1048576 bytes")
		require.Contains(t, err.Error(), "SetReadLimit")

		size, err := desc.ResultSize(desc.NumElements(), 1, 8<<20)
		require.NoError(t, err)
		require.Equal(t, 8<<20, size)
	})

	t.Run("converted result counts against the limit", func(t *testing.T) {
		// 1 Mi uint8 values: 1 MiB raw, 8 MiB as []float64. The larger of the
		// two allocations is compared with the limit.
		u8 := &DatatypeMessage{Class: DatatypeFixed, Size: 1}
		desc, err := newDatasetDescriptor(r, descTestSB(), u8, simpleSpace(1<<20), chunkedLayout(4096, 1), nil)
		require.NoError(t, err)

		_, err = desc.ResultSize(desc.NumElements(), ResultFloat64, 4<<20)
		require.ErrorIs(t, err, utils.ErrLimit)
		require.Contains(t, err.Error(), "needs 8388608 bytes")

		size, err := desc.ResultSize(desc.NumElements(), 1, 4<<20)
		require.NoError(t, err)
		require.Equal(t, 1<<20, size, "the returned size is the raw buffer")

		_, err = desc.ResultSize(desc.NumElements(), math.MaxUint64, math.MaxUint64-1)
		require.ErrorIs(t, err, utils.ErrLimit, "an overflowing converted size is over any limit")
	})

	t.Run("selection larger than the dataset", func(t *testing.T) {
		layout := &DataLayoutMessage{Version: 3, Class: LayoutCompact, CompactData: make([]byte, 32)}
		desc, err := newDatasetDescriptor(r, descTestSB(), float64Type(), simpleSpace(4), layout, nil)
		require.NoError(t, err)

		_, err = desc.ResultSize(5, ResultFloat64, utils.DefaultReadLimit)
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds the dataset's 4 elements")

		size, err := desc.ResultSize(4, ResultFloat64, 0) // The limit applies to chunked data only.
		require.NoError(t, err)
		require.Equal(t, 32, size)
	})
}

func TestNewDatasetDescriptor_Messages(t *testing.T) {
	sb := descTestSB()
	r := bytes.NewReader(make([]byte, 256))
	dtype := buildFloat64DatatypeMessage()
	layout := buildContiguousLayoutMessage(0, 0)

	t.Run("missing messages wrap ErrCorrupt", func(t *testing.T) {
		header := &ObjectHeader{Messages: []*HeaderMessage{{Type: MsgDatatype, Data: dtype}}}
		_, err := NewDatasetDescriptor(r, header, sb)
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.Contains(t, err.Error(), "dataspace message not found")
	})

	t.Run("first message of a type wins", func(t *testing.T) {
		// H5O_msg_read returns the first message of the requested type.
		header := &ObjectHeader{Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtype},
			{Type: MsgDataspace, Data: buildDataspaceV1Message([]uint64{4})},
			{Type: MsgDataspace, Data: buildDataspaceV1Message([]uint64{1 << 30})},
			{Type: MsgDataLayout, Data: layout},
		}}
		desc, err := NewDatasetDescriptor(r, header, sb)
		require.NoError(t, err)
		require.Equal(t, []uint64{4}, desc.Dims())
	})

	t.Run("nil inputs", func(t *testing.T) {
		_, err := NewDatasetDescriptor(r, nil, sb)
		require.Error(t, err)
	})
}
