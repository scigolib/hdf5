package hdf5

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadLimit_DefaultAndReset(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{8})
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	const fourGiB = 4 << 30
	require.Equal(t, uint64(fourGiB), f.ReadLimit())

	f.SetReadLimit(1234)
	require.Equal(t, uint64(1234), f.ReadLimit())

	f.SetReadLimit(0) // Restores the default.
	require.Equal(t, uint64(fourGiB), f.ReadLimit())
}

// TestReadLimit_Chunked covers the limit on chunked reads: a chunked dataset
// may be larger than its file (unwritten chunks read as the fill value), so the
// result is bounded by the caller's limit, and refusing it wraps ErrLimit, not
// ErrCorrupt.
func TestReadLimit_Chunked(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{1024}, WithChunkDims([]uint64{128}))
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	f.SetReadLimit(4096) // 1024 float64 values need 8192 bytes.

	_, err = ds.Read()
	require.ErrorIs(t, err, ErrLimit)
	require.False(t, errors.Is(err, ErrCorrupt), "a refused read of a valid file is not corruption")
	require.Contains(t, err.Error(), "read limit of 4096 bytes")
	require.Contains(t, err.Error(), "SetReadLimit")

	_, err = ds.ReadSlice([]uint64{0}, []uint64{1024})
	require.ErrorIs(t, err, ErrLimit)

	// A selection within the limit still reads.
	part, err := ds.ReadSlice([]uint64{100}, []uint64{8})
	require.NoError(t, err)
	require.Equal(t, []float64{100, 101, 102, 103, 104, 105, 106, 107}, part)

	// The limit covers the result and the read's chunk bookkeeping: one bit per
	// chunk of the walked v1 index (8 chunks, 1 byte; TASK-058). At exactly the
	// result size the bookkeeping does not fit.
	f.SetReadLimit(8192)
	_, err = ds.Read()
	require.ErrorIs(t, err, ErrLimit)

	f.SetReadLimit(8192 + 1)
	all, err := ds.Read()
	require.NoError(t, err)
	require.Len(t, all, 1024)
	require.InDelta(t, 1023.0, all[1023], 0)
}

// TestReadLimit_ContiguousUnaffected: contiguous storage was measured against
// the file, so the limit does not refuse it.
func TestReadLimit_ContiguousUnaffected(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{1024})
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	f.SetReadLimit(1)

	all, err := ds.Read()
	require.NoError(t, err)
	require.Len(t, all, 1024)
}

// TestReadLimit_Concurrent changes the limit while other goroutines read; run
// with -race.
func TestReadLimit_Concurrent(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{256}, WithChunkDims([]uint64{64}))
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := ds.Read(); err != nil && !errors.Is(err, ErrLimit) {
					t.Errorf("read: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		f.SetReadLimit(uint64(1024 * (i%3 + 1)))
		_ = f.ReadLimit()
	}
	wg.Wait()
}
