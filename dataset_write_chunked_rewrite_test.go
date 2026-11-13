package hdf5

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireDataReads reads the dataset /data, which every test in this file
// writes, from file and returns its elements.
func requireDataReads(t *testing.T, file string) []float64 {
	t.Helper()

	const path = "/data"

	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	var values []float64
	found := false
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == path {
			found = true
			values, err = d.Read()
			require.NoError(t, err)
		}
	})
	require.True(t, found, "dataset %s not found", path)
	return values
}

// TestChunkedRewrite_DoesNotGrowFile is issue #80: a Resize + Write(whole
// buffer) loop is today the only way to grow a chunked dataset. Before the
// fix every rewrite allocated a new block for every chunk and a new chunk
// B-tree and never freed the previous ones, so the file kept every
// generation: 24 MB of data made a 732 MB file (30.5x). A rewrite must reuse
// the blocks of chunks whose stored size did not change and free the rest,
// keeping the file close to the data size.
func TestChunkedRewrite_DoesNotGrowFile(t *testing.T) {
	const (
		rowsPerWrite = 100
		cols         = 1000
		rounds       = 10
	)

	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_rewrite.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data", Float32, []uint64{rowsPerWrite, cols},
		WithChunkDims([]uint64{rowsPerWrite, cols}),
		WithMaxDims([]uint64{Unlimited, cols}))
	require.NoError(t, err)

	var want []float32
	n := rowsPerWrite
	for i := 0; i < rounds; i++ {
		if i > 0 {
			n += rowsPerWrite
			require.NoError(t, ds.Resize([]uint64{uint64(n), cols}))
		}
		buf := make([]float32, n*cols)
		for j := range buf {
			buf[j] = float32(i*1_000_000 + j)
		}
		require.NoError(t, ds.Write(buf))
		want = buf
	}
	require.NoError(t, fw.Close())

	// The data grows every round and is n*cols*4 bytes at the end; the file
	// must stay close to it rather than keeping the previous generations
	// (a factor of two leaves room for allocator fragmentation).
	dataBytes := int64(n * cols * 4)
	info, err := os.Stat(filename)
	require.NoError(t, err)
	t.Logf("file %d bytes for %d bytes of data after %d writes", info.Size(), dataBytes, rounds)
	require.LessOrEqual(t, info.Size(), 2*dataBytes,
		"file is %d bytes for %d bytes of data (%.1fx); a rewrite must free the chunks and B-tree it replaces",
		info.Size(), dataBytes, float64(info.Size())/float64(dataBytes))

	requireH5dumpClean(t, filename)

	// The rewritten data must read back exactly as written last.
	got := requireDataReads(t, filename)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, float64(want[i]), got[i], 0, "element %d", i)
	}
}

// TestChunkedRewrite_FilteredData rewrites a dataset whose chunks go through
// a filter pipeline: the stored size of a chunk can change between writes, so
// such a chunk gets a new block and its old one is freed; the values must
// still read back exactly.
func TestChunkedRewrite_FilteredData(t *testing.T) {
	const elems = 1000

	filename := filepath.Join(t.TempDir(), "chunked_filtered_rewrite.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data", Float64, []uint64{elems},
		WithChunkDims([]uint64{100}), WithGZIPCompression(6))
	require.NoError(t, err)

	// Zeros compress to a much smaller chunk than the values that follow.
	require.NoError(t, ds.Write(make([]float64, elems)))

	want := make([]float64, elems)
	for i := range want {
		want[i] = float64(i) * math.Pi
	}
	require.NoError(t, ds.Write(want))
	require.NoError(t, fw.Close())

	requireH5dumpClean(t, filename)

	got := requireDataReads(t, filename)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, want[i], got[i], 0, "element %d", i)
	}
}

// TestChunkedRewrite_ShrinkThenGrowReusesBlocks shrinks a chunked dataset and
// grows it back several times: the blocks of the coordinates that a shrink
// drops must be freed, so the next growth writes into them instead of
// extending the file with a new copy of the data.
func TestChunkedRewrite_ShrinkThenGrowReusesBlocks(t *testing.T) {
	const (
		full    = 20_000 // chunk size 100: 200 chunks
		reduced = 1_000
		cycles  = 3
	)

	values := func(n, seed int) []float64 {
		v := make([]float64, n)
		for i := range v {
			v[i] = float64(seed*1_000_000 + i)
		}
		return v
	}

	tmpDir := t.TempDir()

	// One write at the full size shows what the file costs: data, index and
	// the metadata around them.
	onceFile := filepath.Join(tmpDir, "once.h5")
	func() {
		fw, err := CreateForWrite(onceFile, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/data", Float64, []uint64{full},
			WithChunkDims([]uint64{100}), WithMaxDims([]uint64{Unlimited}))
		require.NoError(t, err)
		require.NoError(t, ds.Write(values(full, 0)))
		require.NoError(t, fw.Close())
	}()
	onceInfo, err := os.Stat(onceFile)
	require.NoError(t, err)

	filename := filepath.Join(tmpDir, "shrink_grow.h5")
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{full},
		WithChunkDims([]uint64{100}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)

	require.NoError(t, ds.Write(values(full, 0)))
	var want []float64
	for c := 0; c < cycles; c++ {
		require.NoError(t, ds.Resize([]uint64{reduced}))
		require.NoError(t, ds.Write(values(reduced, 1)))
		require.NoError(t, ds.Resize([]uint64{full}))
		want = values(full, 2)
		require.NoError(t, ds.Write(want))
	}
	require.NoError(t, fw.Close())

	info, err := os.Stat(filename)
	require.NoError(t, err)
	t.Logf("one write: %d bytes; after %d shrink/grow cycles: %d bytes",
		onceInfo.Size(), cycles, info.Size())
	require.LessOrEqual(t, info.Size(), 2*onceInfo.Size(),
		"a shrink must free the blocks of the dropped chunks, so that growing back reuses them")

	requireH5dumpClean(t, filename)

	got := requireDataReads(t, filename)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, want[i], got[i], 0, "element %d", i)
	}
}

// TestChunkedRewrite_EdgeChunkChanges grows a dataset by a non-multiple of
// the chunk size: the edge chunk's stored size changes on every write, so it
// must move to a new block, and the values it holds must read back exactly.
func TestChunkedRewrite_EdgeChunkChanges(t *testing.T) {
	const (
		chunk  = 100
		step   = 37
		rounds = 12
	)

	values := func(n, seed int) []float64 {
		v := make([]float64, n)
		for i := range v {
			v[i] = float64(seed*1_000_000 + i)
		}
		return v
	}

	tmpDir := t.TempDir()

	// One write at the final size shows what the file costs.
	finalElems := step * rounds
	onceFile := filepath.Join(tmpDir, "once.h5")
	func() {
		fw, err := CreateForWrite(onceFile, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/data", Float64, []uint64{uint64(finalElems)},
			WithChunkDims([]uint64{chunk}))
		require.NoError(t, err)
		require.NoError(t, ds.Write(values(finalElems, 0)))
		require.NoError(t, fw.Close())
	}()
	onceInfo, err := os.Stat(onceFile)
	require.NoError(t, err)

	filename := filepath.Join(tmpDir, "edge_chunk.h5")
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{step},
		WithChunkDims([]uint64{chunk}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)

	var want []float64
	n := step
	for i := 0; i < rounds; i++ {
		if i > 0 {
			n += step
			require.NoError(t, ds.Resize([]uint64{uint64(n)}))
		}
		want = values(n, i)
		require.NoError(t, ds.Write(want))
	}
	require.NoError(t, fw.Close())

	info, err := os.Stat(filename)
	require.NoError(t, err)
	t.Logf("one write of %d elements: %d bytes; %d growing writes to %d: %d bytes",
		finalElems, onceInfo.Size(), rounds, n, info.Size())
	require.LessOrEqual(t, info.Size(), 2*onceInfo.Size(),
		"an edge chunk that changes size must move to a new block and give the old one back")

	requireH5dumpClean(t, filename)

	got := requireDataReads(t, filename)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, want[i], got[i], 0, "element %d", i)
	}
}

// TestChunkedRewrite_FilteredUnchanged writes the same filtered data twice: a
// filter gives the same stored size, so every chunk is written over its own
// block and the file grows by no more than the new index.
func TestChunkedRewrite_FilteredUnchanged(t *testing.T) {
	const elems = 1000 // chunk size 100: 10 chunks

	write := func(name string, rounds int) string {
		filename := filepath.Join(t.TempDir(), name)

		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/data", Float64, []uint64{elems},
			WithChunkDims([]uint64{100}), WithGZIPCompression(6))
		require.NoError(t, err)

		data := make([]float64, elems)
		for i := range data {
			data[i] = float64(i) * math.Pi
		}
		for r := 0; r < rounds; r++ {
			require.NoError(t, ds.Write(data))
		}
		require.NoError(t, fw.Close())
		return filename
	}

	onceFile := write("once.h5", 1)
	onceInfo, err := os.Stat(onceFile)
	require.NoError(t, err)

	file := write("twice.h5", 2)
	info, err := os.Stat(file)
	require.NoError(t, err)
	t.Logf("one write: %d bytes; two writes: %d bytes", onceInfo.Size(), info.Size())
	require.LessOrEqual(t, info.Size(), onceInfo.Size()+4096,
		"unchanged filtered chunks must be written over their own blocks; only the new index may add space")

	requireH5dumpClean(t, file)

	got := requireDataReads(t, file)
	require.Len(t, got, elems)
	for i := range got {
		require.InDelta(t, float64(i)*math.Pi, got[i], 0, "element %d", i)
	}
}

// TestChunkedRewrite_MultiLevelIndex rewrites a dataset whose chunk index is
// a multi-level v1 B-tree (more than 64 chunks): every node of the previous
// index must be freed, not only the root, so a dataset whose shape does not
// change stops growing after the first rewrite.
func TestChunkedRewrite_MultiLevelIndex(t *testing.T) {
	const (
		elems  = 200 // chunk size 2: 100 chunks, two levels
		rounds = 10
	)

	tmpDir := t.TempDir()

	writeFile := func(name string, rounds int) (string, []int32) {
		filename := filepath.Join(tmpDir, name)

		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/data", Int32, []uint64{elems}, WithChunkDims([]uint64{2}))
		require.NoError(t, err)

		var want []int32
		for i := 0; i < rounds; i++ {
			buf := make([]int32, elems)
			for j := range buf {
				buf[j] = int32(i*1000 + j)
			}
			require.NoError(t, ds.Write(buf))
			want = buf
		}
		require.NoError(t, fw.Close())
		return filename, want
	}

	onceFile, _ := writeFile("index_once.h5", 1)
	onceInfo, err := os.Stat(onceFile)
	require.NoError(t, err)

	file, want := writeFile("index_rewrite.h5", rounds)
	info, err := os.Stat(file)
	require.NoError(t, err)
	t.Logf("one write: %d bytes; %d writes: %d bytes", onceInfo.Size(), rounds, info.Size())
	require.LessOrEqual(t, info.Size(), 2*onceInfo.Size(),
		"a rewrite must reuse the blocks of the multi-level index: %d bytes after %d writes, %d bytes after one",
		info.Size(), rounds, onceInfo.Size())

	requireH5dumpClean(t, file)

	got := requireDataReads(t, file)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, float64(want[i]), got[i], 0, "element %d", i)
	}
}
