package hdf5

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// Reads whose cost grows far beyond the file (review findings H2, H3, H4, M1,
// M2, M3). Each case is a small file that used to make a reader allocate or
// loop by orders of magnitude more than the file justifies; the claims here are
// megabytes, and the tests show the read is refused, or done, within a budget.

// patchDataspaceDims rewrites the dimensions (and the maximum dimensions, when
// present) of dataset dsPath's dataspace message in place. Growing the
// dataspace of a chunked dataset makes it sparse: the new chunks are unwritten,
// which is legal and reads as the fill value (H5Dchunk.c:3041-3046).
func patchDataspaceDims(t *testing.T, src, dsPath string, dims []uint64) string {
	t.Helper()

	f, err := Open(src)
	require.NoError(t, err)
	addr := findDatasetPath(t, f, dsPath).Address()
	msg := headerMessageData(t, f, addr, core.MsgDataspace)
	require.NoError(t, f.Close())

	ds, err := core.ParseDataspaceMessage(msg)
	require.NoError(t, err)
	require.Len(t, ds.Dimensions, len(dims))

	offset := 8 // Version 1 header.
	if msg[0] == 2 {
		offset = 4
	}
	hasMax := msg[2]&1 != 0
	require.Len(t, msg, offset+len(dims)*8*map[bool]int{false: 1, true: 2}[hasMax], "8-byte dimensions expected")

	patched := append([]byte(nil), msg...)
	for i, d := range dims {
		binary.LittleEndian.PutUint64(patched[offset+i*8:], d)
		if hasMax {
			binary.LittleEndian.PutUint64(patched[offset+(len(dims)+i)*8:], d)
		}
	}

	raw, err := os.ReadFile(src)
	require.NoError(t, err)
	rel := bytes.Index(raw[addr:], msg)
	require.GreaterOrEqual(t, rel, 0, "dataspace message of %s not found after its header", dsPath)
	copy(raw[int(addr)+rel:], patched)

	dst := filepath.Join(t.TempDir(), "patched-dims.h5")
	require.NoError(t, os.WriteFile(dst, raw, 0o600))

	return dst
}

// openDataset opens path and returns the file and dataset dsPath.
func openDataset(t *testing.T, path, dsPath string) (*File, *Dataset) {
	t.Helper()

	f, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	return f, findDatasetPath(t, f, dsPath)
}

// TestRead_DeflateBombChunk (H4): a chunk of 1024 float64 values (8 KiB)
// whose stored deflate stream expands to 8 MiB. Filters used to decompress
// with io.ReadAll, so the output was bounded by nothing; the C library sizes
// the filter output from the chunk (H5Z_pipeline). The output may not exceed
// the chunk's byte size: a valid pipeline reproduces exactly that.
func TestRead_DeflateBombChunk(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{1024}, WithChunkDims([]uint64{1024}), WithGZIPCompression(6))
	raw, root := chunkIndexRoot(t, path)
	require.EqualValues(t, 1, binary.LittleEndian.Uint16(raw[root+6:]), "one chunk expected")

	var bomb bytes.Buffer
	zw, err := zlib.NewWriterLevel(&bomb, zlib.BestCompression)
	require.NoError(t, err)
	_, err = zw.Write(make([]byte, 8<<20))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	// Point entry 0 at the bomb, appended to the file: nbytes, then the child.
	bombAddr := uint64(len(raw))
	raw = append(raw, bomb.Bytes()...)
	binary.LittleEndian.PutUint32(raw[root+btreeHeaderBytes:], uint32(bomb.Len()))
	binary.LittleEndian.PutUint64(raw[root+btreeHeaderBytes+btreeKeyBytes:], bombAddr)

	dst := filepath.Join(t.TempDir(), "bomb.h5")
	require.NoError(t, os.WriteFile(dst, raw, 0o600))
	_, ds := openDataset(t, dst, "/x")

	var readErr error
	allocated := allocatedBy(func() { _, readErr = ds.Read() })
	t.Logf("compressed %d bytes, allocated %d bytes, error: %v", bomb.Len(), allocated, readErr)
	require.ErrorIs(t, readErr, ErrCorrupt)
	require.Contains(t, readErr.Error(), "decompressed data exceeds the chunk size 8192")
	require.Less(t, allocated, uint64(allocBudget))

	_, err = ds.ReadSlice([]uint64{0}, []uint64{16})
	require.ErrorIs(t, err, ErrCorrupt)
}

// compoundPairType is {a float64, b float64}, 16 bytes.
func compoundPairType(t *testing.T) *core.DatatypeMessage {
	t.Helper()

	f64, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 8)
	require.NoError(t, err)
	ct, err := core.CreateCompoundTypeFromFields([]core.CompoundFieldDef{
		{Name: "a", Offset: 0, Type: f64},
		{Name: "b", Offset: 8, Type: f64},
	})
	require.NoError(t, err)

	return ct
}

// writeCompoundPairs writes n {a, b} elements to /x.
func writeCompoundPairs(t *testing.T, n uint64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "compound.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateCompoundDataset("/x", compoundPairType(t), []uint64{n})
	require.NoError(t, err)

	data := make([]byte, n*16)
	for i := uint64(0); i < n; i++ {
		binary.LittleEndian.PutUint64(data[i*16:], math.Float64bits(float64(i)))
		binary.LittleEndian.PutUint64(data[i*16+8:], math.Float64bits(-float64(i)))
	}
	require.NoError(t, dw.WriteRaw(data))
	require.NoError(t, fw.Close())

	return path
}

// TestReadCompound_MapsCountedAgainstLimit (H2): a compound element decodes
// into a map[string]any costing about 700 bytes for 16 raw bytes, so a limit
// that counted only the raw bytes (or 8 bytes per element) let a sparse
// chunked dataset that passed it allocate tens of times more. 16,384 elements
// are 256 KiB raw and about 11 MiB as maps.
//
// The writer has no chunked compound datasets, so the sparse case is the C
// library's tcompound2.h5 /dset1 ({int32, float32, float64}, 16 bytes, chunks
// of 2, 6 elements written), copied by h5copy 1.14.6 into
// testdata/compound_chunked.h5 (the original has a version 1 layout message,
// which this reader does not decode), with its extent patched to n: 3 stored
// chunks of a grid of 8192.
func TestReadCompound_MapsCountedAgainstLimit(t *testing.T) {
	const n = 16384

	sparse := patchDataspaceDims(t, filepath.Join("testdata", "compound_chunked.h5"), "/dset1", []uint64{n})
	contiguous := writeCompoundPairs(t, n)

	for _, tc := range []struct{ name, path, ds, member string }{
		{"sparse chunked", sparse, "/dset1", "c_name"},
		{"contiguous", contiguous, "/x", "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, ds := openDataset(t, tc.path, tc.ds)
			f.SetReadLimit(1 << 20)

			var readErr error
			allocated := allocatedBy(func() { _, readErr = ds.ReadCompound() })
			t.Logf("allocated %d bytes, error: %v", allocated, readErr)
			require.ErrorIs(t, readErr, ErrLimit)
			require.Contains(t, readErr.Error(), "read limit of 1048576 bytes")
			require.Less(t, allocated, uint64(allocBudget))

			// Within a limit that covers the maps the same read succeeds.
			f.SetReadLimit(64 << 20)
			values, err := ds.ReadCompound()
			require.NoError(t, err)
			require.Len(t, values, n)
			require.Contains(t, values[n-1], tc.member)
		})
	}
}

// TestReadVLenBytes_SharedObjectCountedAgainstLimit (H3): every element of a
// variable-length dataset references the same 1 MiB heap object. The reader
// used to copy the object per element (and re-read its whole collection per
// element), so 64 references, 1 KiB of raw data, returned 64 MiB; 65,536
// references in a 2 MB file returned 64 GiB. The bytes handed out are now
// counted against the read limit.
func TestReadVLenBytes_SharedObjectCountedAgainstLimit(t *testing.T) {
	const n = 64

	path := filepath.Join(t.TempDir(), "vlen.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/x", VLenUint8, []uint64{n})
	require.NoError(t, err)
	data := make([][]byte, n)
	data[0] = bytes.Repeat([]byte{0xAB}, 1<<20)
	for i := 1; i < n; i++ {
		data[i] = []byte{byte(i)}
	}
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	// Copy element 0's heap reference over every element.
	f, err := Open(path)
	require.NoError(t, err)
	ds := findDatasetPath(t, f, "/x")
	layout, err := core.ParseDataLayoutMessage(headerMessageData(t, f, ds.Address(), core.MsgDataLayout), f.Superblock())
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, core.LayoutContiguous, layout.Class)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	refs := raw[layout.DataAddress : layout.DataAddress+n*16]
	for i := 1; i < n; i++ {
		copy(refs[i*16:(i+1)*16], refs[:16])
	}
	dst := filepath.Join(t.TempDir(), "vlen-shared.h5")
	require.NoError(t, os.WriteFile(dst, raw, 0o600))

	g, gds := openDataset(t, dst, "/x")
	g.SetReadLimit(4 << 20)

	var readErr error
	allocated := allocatedBy(func() { _, readErr = gds.ReadVLenBytes() })
	t.Logf("allocated %d bytes, error: %v", allocated, readErr)
	require.ErrorIs(t, readErr, ErrLimit)
	require.Contains(t, readErr.Error(), "read limit of 4194304 bytes")
	require.Less(t, allocated, uint64(16<<20), "more than the limit was copied")

	g.SetReadLimit(128 << 20)
	values, err := gds.ReadVLenBytes()
	require.NoError(t, err)
	require.Len(t, values, n)
	require.Len(t, values[n-1], 1<<20)
}

// TestReadSlice_SparseGridNotMaterialized (M1): a chunked int8 dataset of 4M
// elements with 16 stored chunks of one element. The hyperslab reader used to
// build a []uint64 and a formatted map key per chunk of the grid the selection
// overlaps (4M of each, about 300 MB and seconds of Sprintf) before reading
// the 16 chunks that exist. Only the stored chunks are visited now, so the
// read allocates its result (4 MiB raw + 32 MiB of float64) and little more.
func TestReadSlice_SparseGridNotMaterialized(t *testing.T) {
	const n = 4_000_000

	path := filepath.Join(t.TempDir(), "int8.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/x", Int8, []uint64{16}, WithChunkDims([]uint64{1}))
	require.NoError(t, err)
	vals := make([]int8, 16)
	for i := range vals {
		vals[i] = int8(i + 1)
	}
	require.NoError(t, dw.Write(vals))
	require.NoError(t, fw.Close())

	_, ds := openDataset(t, patchDataspaceDims(t, path, "/x", []uint64{n}), "/x")

	var got interface{}
	var readErr error
	allocated := allocatedBy(func() { got, readErr = ds.ReadSlice([]uint64{0}, []uint64{n}) })
	t.Logf("allocated %d bytes", allocated)
	require.NoError(t, readErr)
	values := got.([]float64)
	require.Len(t, values, n)
	require.InDelta(t, 16.0, values[15], 0)
	require.InDelta(t, 0.0, values[n-1], 0)

	const result = n + n*8 // Raw bytes and the float64 result.
	require.Less(t, allocated, uint64(result+8<<20), "the read allocated far more than its result")
}

// TestReadHyperslab_OneLongBlockPerChunk: one block (Count 1) may have a
// stride below its length; validation allows it, as H5Shyper.c does, because
// the stride is never used. The chunked reader mapped such elements to the
// wrong output position (stride arithmetic) and silently dropped them.
func TestReadHyperslab_OneLongBlockPerChunk(t *testing.T) {
	_, ds := openDataset(t, writeFloat64Dataset(t, []uint64{12}, WithChunkDims([]uint64{4})), "/x")

	got, err := ds.ReadHyperslab(&HyperslabSelection{
		Start: []uint64{2}, Count: []uint64{1}, Stride: []uint64{5}, Block: []uint64{8},
	})
	require.NoError(t, err)
	require.Equal(t, []float64{2, 3, 4, 5, 6, 7, 8, 9}, got)
}

// TestExtractChunkPortion_WorkIsPerChunk (M2): extracting a chunk's part of a
// selection used to walk the selection from its start for every chunk, so the
// work was chunks × selected elements (10^5 chunks × 10^7 elements is 10^12
// steps). Here a selection of 2^40 elements meets the last chunk of a dataset;
// walking from the selection's start would not finish, so a deadline fails the
// test instead of hanging it. Both shapes of a long selection are covered:
// many one-element blocks, and one block as long as the dimension.
func TestExtractChunkPortion_WorkIsPerChunk(t *testing.T) {
	const dim = uint64(1) << 40

	chunk := []byte{1, 2, 3, 4}
	lastChunk := []uint64{dim/4 - 1}

	for _, tc := range []struct {
		name       string
		huge, tail *HyperslabSelection // The whole dimension; its last 8 elements.
	}{
		{
			name: "many blocks of one",
			huge: &HyperslabSelection{Start: []uint64{0}, Count: []uint64{dim}, Stride: []uint64{1}, Block: []uint64{1}},
			tail: &HyperslabSelection{Start: []uint64{dim - 8}, Count: []uint64{8}, Stride: []uint64{1}, Block: []uint64{1}},
		},
		{
			name: "one block of all",
			huge: &HyperslabSelection{Start: []uint64{0}, Count: []uint64{1}, Stride: []uint64{1}, Block: []uint64{dim}},
			tail: &HyperslabSelection{Start: []uint64{dim - 8}, Count: []uint64{1}, Stride: []uint64{1}, Block: []uint64{8}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The chunk's elements land at output offsets near 2^40, outside
			// this small buffer, and the copy's bounds check skips them: only
			// the walk's length is under test here.
			done := make(chan struct{})
			go func() {
				defer close(done)
				extractChunkPortion(chunk, lastChunk, []uint64{4}, []uint64{dim}, tc.huge, 1, make([]byte, 8))
			}()

			// No work counter reaches this walk (a pure loop over the
			// selection: no reader, no callback), so the guard is time, bounded
			// by the test's own deadline: it fires only when the run is about
			// to time out anyway, never because the machine is slow, and then
			// names the defect. Without a deadline (-timeout 0) it waits.
			var expired <-chan time.Time
			if deadline, ok := t.Deadline(); ok {
				timer := time.NewTimer(time.Until(deadline) * 9 / 10)
				defer timer.Stop()
				expired = timer.C
			}
			select {
			case <-done:
			case <-expired:
				t.Fatal("extracting one chunk walked the whole selection")
			}

			// The last 8 elements of the dataset, of which the chunk holds the last 4.
			out := make([]byte, 8)
			extractChunkPortion(chunk, lastChunk, []uint64{4}, []uint64{dim}, tc.tail, 1, out)
			require.Equal(t, []byte{0, 0, 0, 0, 1, 2, 3, 4}, out)
		})
	}
}

// TestRead_ResultBeyondAddressSpace (M3): on a 32-bit target a result larger
// than the address space must be refused, whatever the read limit, before the
// raw buffer is allocated. 600M int8 elements (one stored chunk) are 600 MB raw
// and 4.8 GB as []float64; before the fix the read allocated the raw buffer
// and then panicked in make([]float64) on 386.
//
// 64-bit targets can address the result and skip this case; CI runs it on 386.
func TestRead_ResultBeyondAddressSpace(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("needs a 32-bit target (GOARCH=386 or arm)")
	}

	const n = 600_000_000

	path := filepath.Join(t.TempDir(), "int8.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/x", Int8, []uint64{16}, WithChunkDims([]uint64{16}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(make([]int8, 16)))
	require.NoError(t, fw.Close())

	f, ds := openDataset(t, patchDataspaceDims(t, path, "/x", []uint64{n}), "/x")
	f.SetReadLimit(math.MaxUint64)

	var readErr error
	allocated := allocatedBy(func() { _, readErr = ds.Read() })
	require.ErrorIs(t, readErr, ErrLimit)
	require.Contains(t, readErr.Error(), "more than this platform can address")
	require.Less(t, allocated, uint64(allocBudget))
}
