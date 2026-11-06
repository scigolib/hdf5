package hdf5

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// Regression tests for issue #75 (ADR-004 §1): every dataset reader must size
// its result from validated storage, never from datatype.Size × selection
// before a check. Each fixture is a file written by this library with one
// datatype size patched, so the dataset claims megabytes of storage in a file
// of a few kilobytes.
//
// The claims are kept to megabytes (ADR-003 rule 6) so the code before the fix
// cannot exhaust memory: there each reader allocates the claimed buffer first
// and only then fails, or returns, on the short file. The tests therefore
// measure the bytes the read allocated and require far fewer than the claim.
// They use only API that predates the fix, so they compile and fail on the base
// commit.

// patchedElemSize is the datatype size written into the fixtures: 1 MiB + 8
// bytes per element, so a dozen elements claim about 12 MB.
const patchedElemSize = 0x100008

// allocBudget is the most a rejected read may allocate. It is far below any
// claim made here, and far above the bookkeeping a read needs to reject one.
const allocBudget = 1 << 20

// writeFloat64Dataset writes a float64 dataset 0, 1, 2, … with the given
// shape and returns the file path.
func writeFloat64Dataset(t *testing.T, dims []uint64, opts ...DatasetOption) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "clean.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)

	n := uint64(1)
	for _, d := range dims {
		n *= d
	}
	vals := make([]float64, n)
	for i := range vals {
		vals[i] = float64(i)
	}

	dw, err := fw.CreateDataset("/x", Float64, dims, opts...)
	require.NoError(t, err)
	require.NoError(t, dw.Write(vals))
	require.NoError(t, fw.Close())

	return path
}

// patchDatatypeSize copies src to a temporary file in which the datatype size
// of the dataset at dsPath is newSize, and returns the copy's path. The
// datatype message is located through the dataset's own object header, and the
// patch is verified by parsing the header of the copy.
func patchDatatypeSize(t *testing.T, src, dsPath string, newSize uint32) string {
	t.Helper()

	f, err := Open(src)
	require.NoError(t, err)
	ds := findDatasetPath(t, f, dsPath)
	addr := ds.Address()
	dtData := datatypeMessageData(t, f, addr)
	require.NoError(t, f.Close())

	raw, err := os.ReadFile(src)
	require.NoError(t, err)

	// The datatype message follows the object header's prefix, so its first
	// occurrence at or after the header address is this dataset's.
	rel := bytes.Index(raw[addr:], dtData)
	require.GreaterOrEqual(t, rel, 0, "datatype message of %s not found after its header", dsPath)
	at := int(addr) + rel
	binary.LittleEndian.PutUint32(raw[at+4:at+8], newSize)

	dst := filepath.Join(t.TempDir(), "patched.h5")
	require.NoError(t, os.WriteFile(dst, raw, 0o600))

	g, err := Open(dst)
	require.NoError(t, err)
	defer func() { _ = g.Close() }()
	dt, err := core.ParseDatatypeMessage(datatypeMessageData(t, g, addr))
	require.NoError(t, err)
	require.Equal(t, newSize, dt.Size, "patch did not reach the datatype size")

	return dst
}

// datatypeMessageData returns the raw datatype message of the object header at addr.
func datatypeMessageData(t *testing.T, f *File, addr uint64) []byte {
	t.Helper()

	header, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	for _, msg := range header.Messages {
		if msg.Type == core.MsgDatatype {
			return append([]byte(nil), msg.Data...)
		}
	}
	t.Fatalf("no datatype message at 0x%X", addr)
	return nil
}

// findDatasetPath returns the dataset at path.
func findDatasetPath(t *testing.T, f *File, path string) *Dataset {
	t.Helper()

	var found *Dataset
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == path {
			found = d
		}
	})
	require.NotNil(t, found, "dataset %s not found", path)

	return found
}

// allocatedBy returns the bytes the heap allocated while fn ran.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)

	return after.TotalAlloc - before.TotalAlloc
}

// requireBoundedRejection opens path, runs read on the dataset at dsPath and
// requires an error and an allocation far below claim.
func requireBoundedRejection(t *testing.T, path, dsPath string, claim uint64, read func(*Dataset) error) {
	t.Helper()

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, dsPath)

	var readErr error
	allocated := allocatedBy(func() { readErr = read(ds) })

	t.Logf("claimed %d bytes, allocated %d bytes, error: %v", claim, allocated, readErr)
	require.Error(t, readErr, "a dataset claiming %d bytes in a %s file must be rejected", claim, path)
	require.Less(t, allocated, uint64(allocBudget),
		"the read allocated %d bytes for a claim of %d bytes: the result was sized before validation", allocated, claim)
}

func readSliceFn(start, count []uint64) func(*Dataset) error {
	return func(ds *Dataset) error {
		_, err := ds.ReadSlice(start, count)
		return err
	}
}

// TestReadSlice_CorruptSize_Contiguous1D: the 1-D contiguous path sized its
// buffer as count × datatype.Size (dataset_read_hyperslab.go:483 on 257fb7d).
func TestReadSlice_CorruptSize_Contiguous1D(t *testing.T) {
	path := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{12}), "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 12*patchedElemSize, readSliceFn([]uint64{0}, []uint64{12}))
}

// TestReadSlice_CorruptSize_ContiguousND: a multi-dimensional selection with
// the last dimension in full took the "contiguous" branch (:503).
func TestReadSlice_CorruptSize_ContiguousND(t *testing.T) {
	path := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{4, 3}), "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 12*patchedElemSize, readSliceFn([]uint64{0, 0}, []uint64{4, 3}))
}

// TestReadSlice_CorruptSize_RowByRow3D: a 3-D selection with a partial last
// dimension took the row-by-row path (:533 output, :558 bounding box).
func TestReadSlice_CorruptSize_RowByRow3D(t *testing.T) {
	path := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{2, 3, 2}), "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 6*patchedElemSize, readSliceFn([]uint64{0, 0, 0}, []uint64{2, 3, 1}))
}

// TestReadSlice_CorruptSize_2D: a 2-D selection with a partial last dimension
// took the per-element 2-D path (:533, then :595).
func TestReadSlice_CorruptSize_2D(t *testing.T) {
	path := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{4, 3}), "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 8*patchedElemSize, readSliceFn([]uint64{0, 0}, []uint64{4, 2}))
}

// TestReadSlice_CorruptSize_Chunked: the chunked hyperslab path sized its
// output from the selection (:707) although the chunk layout records the real
// element size (H5Dchunk.c:827-831).
func TestReadSlice_CorruptSize_Chunked(t *testing.T) {
	src := writeFloat64Dataset(t, []uint64{12}, WithChunkDims([]uint64{4}))
	path := patchDatatypeSize(t, src, "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 12*patchedElemSize, readSliceFn([]uint64{0}, []uint64{12}))
}

// TestRead_CorruptSize_Chunked: the whole-dataset chunked reader
// (readChunkedData) capped its buffer only at 1 TB.
func TestRead_CorruptSize_Chunked(t *testing.T) {
	src := writeFloat64Dataset(t, []uint64{12}, WithChunkDims([]uint64{4}))
	path := patchDatatypeSize(t, src, "/x", patchedElemSize)
	requireBoundedRejection(t, path, "/x", 12*patchedElemSize, func(ds *Dataset) error {
		_, err := ds.Read()
		return err
	})
}

// TestReadSlice_CorruptSize_Compact: the compact path sized its output from
// the selection (:1069) instead of the data stored in the layout message,
// which C requires to equal nelmts × size (H5Dcompact.c:253-270). The fixture
// is the official suite's compact dataset (20×10 int32).
func TestReadSlice_CorruptSize_Compact(t *testing.T) {
	const compactElemSize = 0x10004 // 64 KiB + 4 bytes: 200 elements claim ~13 MB.

	path := patchDatatypeSize(t, filepath.Join("testdata", "hdf5_official", "tfilters.h5"), "/compact", compactElemSize)
	requireBoundedRejection(t, path, "/compact", 200*compactElemSize, readSliceFn([]uint64{0, 0}, []uint64{20, 10}))
}
