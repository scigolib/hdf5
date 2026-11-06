package core

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// implicitInt8 is an implicit-index (H5Dnone.c) int8 dataset of n one-byte
// chunks stored at address 0 of an n-byte image whose byte i is i.
func implicitInt8(n uint64) (*DatasetDescriptor, *bytes.Reader) {
	img := make([]byte, n)
	for i := range img {
		img[i] = byte(i)
	}
	d := &DatasetDescriptor{
		datatype:  &DatatypeMessage{Class: DatatypeFixed, Size: 1},
		dataspace: &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{n}, MaxDims: []uint64{n}},
		layout: &DataLayoutMessage{
			Version: 4, Class: LayoutChunked, ChunkIndexType: ChunkIndexNone,
			ChunkSize: []uint64{1, 1}, DataAddress: 0,
		},
		fill:       &FillValueMessage{},
		nelmts:     n,
		elemSize:   1,
		dataBytes:  n,
		chunkBytes: 1,
	}

	return d, bytes.NewReader(img)
}

// TestCollectChunks_EntryBudget: the chunk iterator keeps one entry per stored
// chunk, so the entries count against the read limit, 48 bytes plus 8 per
// layout dimension each, and more is utils.ErrLimit (TASK-058; one policy for
// every index type). Red before TASK-058: the implicit index was refused as
// unsupported.
func TestCollectChunks_EntryBudget(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	const perEntry = chunkEntryOverhead + 8*2

	d, r := implicitInt8(100)
	got, err := d.CollectChunks(r, sb, 100*perEntry)
	require.NoError(t, err)
	require.Len(t, got, 100)
	require.Equal(t, uint64(99), got[99].Address)
	require.Equal(t, []uint64{99, 0}, got[99].Key.Scaled)

	_, err = d.CollectChunks(r, sb, 100*perEntry-1)
	require.ErrorIs(t, err, utils.ErrLimit)
	require.NotErrorIs(t, err, utils.ErrCorrupt)

	// A million chunks against a 64 KB limit stop at the limit: the entries
	// kept never outgrow it.
	d, r = implicitInt8(1 << 20)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = d.CollectChunks(r, sb, 64<<10)
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, utils.ErrLimit)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "the collector allocated past its limit")
}

// TestReadChunkedData_ImplicitResultOverLimit: an implicit index allocates
// nothing for its grid (each chunk's address is computed), so a whole read of
// many chunks is bounded by its result, refused before any chunk is read.
func TestReadChunkedData_ImplicitResultOverLimit(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	d, r := implicitInt8(1 << 20)

	_, err := readChunkedData(r, sb, d, 8, 1<<20)
	require.ErrorIs(t, err, utils.ErrLimit)

	got, err := readChunkedData(r, sb, d, 1, 1<<20)
	require.NoError(t, err)
	require.Len(t, got, 1<<20)
	require.Equal(t, byte(0xFF), got[1<<20-1])
}
