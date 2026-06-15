package hdf5

import (
	"path/filepath"
	"testing"
)

// TestReadHyperslabChunkedStrided covers the strided path of the chunked
// reader: with Stride > 1 the output position of an element is no longer
// just coord-start, so selectionOutputIndex must map (coord) →
// (count*block-extent) row-major slot. The selection also spans multiple
// chunks in both dimensions.
func TestReadHyperslabChunkedStrided(t *testing.T) {
	const rows, cols = 20, 30
	path := filepath.Join(t.TempDir(), "strided_chunked.h5")

	fw, err := CreateForWrite(path, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite: %v", err)
	}
	ds, err := fw.CreateDataset("/d", Int32, []uint64{rows, cols}, WithChunkDims([]uint64{5, 10}))
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	data := make([]int32, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			data[r*cols+c] = int32(r*cols + c)
		}
	}
	if err := ds.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	rds, found := findDatasetByName(f, "d")
	if !found {
		t.Fatal("dataset not found")
	}

	// Every 2nd row/col: rows 0,2,…,18 (10), cols 0,2,…,28 (15) — crosses
	// all 4 row chunks and all 3 column chunks.
	const sn, scn = 10, 15
	sel := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{sn, scn},
		Stride: []uint64{2, 2},
		Block:  []uint64{1, 1},
	}
	r, err := rds.ReadHyperslab(sel)
	if err != nil {
		t.Fatalf("ReadHyperslab (strided, chunked): %v", err)
	}
	out := r.([]float64)
	if len(out) != sn*scn {
		t.Fatalf("len = %d, want %d", len(out), sn*scn)
	}
	for rr := 0; rr < sn; rr++ {
		for cc := 0; cc < scn; cc++ {
			got := out[rr*scn+cc]
			want := float64(data[(rr*2)*cols+(cc*2)])
			if got != want {
				t.Fatalf("out[%d,%d] (dataset [%d,%d]) = %v, want %v", rr, cc, rr*2, cc*2, got, want)
			}
		}
	}
}

// runChunkedTypeCase writes a (20×30) dataset of the given datatype,
// chunked (5×10) so a sub-block spans multiple chunks, then asserts a
// multi-chunk ReadSlice matches Read() element-for-element. val builds the
// i-th element; f64 is its reference float64 value.
func runChunkedTypeCase[T any](t *testing.T, dt Datatype, val func(i int) T, f64 func(T) float64) {
	t.Helper()
	const rows, cols = 20, 30
	path := filepath.Join(t.TempDir(), "typed_chunked.h5")

	fw, err := CreateForWrite(path, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite: %v", err)
	}
	ds, err := fw.CreateDataset("/d", dt, []uint64{rows, cols}, WithChunkDims([]uint64{5, 10}))
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	data := make([]T, rows*cols)
	for i := range data {
		data[i] = val(i)
	}
	if err := ds.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	rds, found := findDatasetByName(f, "d")
	if !found {
		t.Fatal("dataset not found")
	}

	full, err := rds.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i := range data {
		if full[i] != f64(data[i]) {
			t.Fatalf("Read[%d] = %v, want %v", i, full[i], f64(data[i]))
		}
	}

	const r0, c0, rn, cn = 3, 5, 12, 20 // spans 3 column chunks
	rb, err := rds.ReadSlice([]uint64{r0, c0}, []uint64{rn, cn})
	if err != nil {
		t.Fatalf("ReadSlice: %v", err)
	}
	band := rb.([]float64)
	for rr := 0; rr < rn; rr++ {
		for cc := 0; cc < cn; cc++ {
			if got, want := band[rr*cn+cc], f64(data[(rr+r0)*cols+(cc+c0)]); got != want {
				t.Fatalf("band[%d,%d] = %v, want %v", rr, cc, got, want)
			}
		}
	}
}

// TestReadSliceChunkedAllFixedPointWidths confirms the hyperslab reader,
// now delegating to core.ConvertToFloat64, covers every fixed-point width
// and sign plus float32/64 across chunk boundaries — the same set the
// whole-dataset Read() path supports. (int16 has its own focused test
// above; this is the breadth check.)
func TestReadSliceChunkedAllFixedPointWidths(t *testing.T) {
	t.Run("int8", func(t *testing.T) {
		runChunkedTypeCase(t, Int8, func(i int) int8 { return int8(i%200 - 100) }, func(v int8) float64 { return float64(v) })
	})
	t.Run("uint8", func(t *testing.T) {
		runChunkedTypeCase(t, Uint8, func(i int) uint8 { return uint8(i % 256) }, func(v uint8) float64 { return float64(v) })
	})
	t.Run("uint16", func(t *testing.T) {
		runChunkedTypeCase(t, Uint16, func(i int) uint16 { return uint16(i * 7) }, func(v uint16) float64 { return float64(v) })
	})
	t.Run("int32", func(t *testing.T) {
		runChunkedTypeCase(t, Int32, func(i int) int32 { return int32(i*100000 - 30000000) }, func(v int32) float64 { return float64(v) })
	})
	t.Run("uint32", func(t *testing.T) {
		runChunkedTypeCase(t, Uint32, func(i int) uint32 { return uint32(i * 100000) }, func(v uint32) float64 { return float64(v) })
	})
	t.Run("int64", func(t *testing.T) {
		runChunkedTypeCase(t, Int64, func(i int) int64 { return int64(i)*1_000_000 - 300_000_000 }, func(v int64) float64 { return float64(v) })
	})
	t.Run("uint64", func(t *testing.T) {
		runChunkedTypeCase(t, Uint64, func(i int) uint64 { return uint64(i) * 1_000_000 }, func(v uint64) float64 { return float64(v) })
	})
	t.Run("float32", func(t *testing.T) {
		runChunkedTypeCase(t, Float32, func(i int) float32 { return float32(i)*0.5 - 100 }, func(v float32) float64 { return float64(v) })
	})
	t.Run("float64", func(t *testing.T) {
		runChunkedTypeCase(t, Float64, func(i int) float64 { return float64(i)*0.25 - 50 }, func(v float64) float64 { return v })
	})
}

// TestReadSliceChunkedInt16 guards the chunked hyperslab path for a
// fixed-point (int16) datatype across multiple chunks. The hyperslab
// reader previously had its own narrow float64 converter that only knew
// float64/float32/int32/int64, so a chunked int16 dataset (e.g. H SAF
// H40B /rr) errored with "unsupported datatype for conversion to float64";
// it now delegates to core.ConvertToFloat64, the same path Read() uses.
// Combined with the coordinate-based placement fix, a multi-chunk int16
// ReadSlice must match Read() element-for-element.
func TestReadSliceChunkedInt16(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "i16_chunked.h5")

	const rows, cols = 20, 30
	fw, err := CreateForWrite(path, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite: %v", err)
	}
	// Chunked (uncompressed) is enough to exercise the multi-chunk path;
	// the int16+DEFLATE write round-trip has an unrelated writer issue, and
	// the real compressed H40B file is covered by the backend's banded test.
	ds, err := fw.CreateDataset("/i16", Int16, []uint64{rows, cols},
		WithChunkDims([]uint64{5, 10}))
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	data := make([]int16, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			data[r*cols+c] = int16(r*cols + c - 300) // span negatives too
		}
	}
	if err := ds.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	rds, found := findDatasetByName(f, "i16")
	if !found {
		t.Fatal("i16 not found")
	}

	full, err := rds.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i, v := range data { // Read() itself must decode int16 correctly
		if full[i] != float64(v) {
			t.Fatalf("Read[%d] = %v, want %v", i, full[i], v)
		}
	}

	// Multi-chunk sub-block: rows [3,15) cols [5,25) → 3 column chunks.
	const r0, c0, rn, cn = 3, 5, 12, 20
	rb, err := rds.ReadSlice([]uint64{r0, c0}, []uint64{rn, cn})
	if err != nil {
		t.Fatalf("ReadSlice (int16, chunked): %v", err)
	}
	band := rb.([]float64)
	for rr := 0; rr < rn; rr++ {
		for cc := 0; cc < cn; cc++ {
			got := band[rr*cn+cc]
			want := float64(data[(rr+r0)*cols+(cc+c0)])
			if got != want {
				t.Fatalf("band[%d,%d] = %v, want %v", rr, cc, got, want)
			}
		}
	}
}

// TestReadSliceChunkedMultiChunkOrdering guards a chunked-hyperslab
// regression: a selection wider than a chunk spans several chunks, and the
// reader once emitted elements in chunk-visitation order instead of the
// selection's row-major order — scrambling every multi-chunk read (and
// shifting everything after a missing sparse chunk).
//
// testdata/gzip_test.h5 "compressed_2d" is a (20, 30) float64 dataset
// chunked (5, 10) with DEFLATE, so any selection wider than 10 columns
// crosses 2–3 column chunks. ds.Read() (whole-dataset, row-major) is the
// reference; ReadSlice must agree element-for-element.
func TestReadSliceChunkedMultiChunkOrdering(t *testing.T) {
	f, err := Open("testdata/gzip_test.h5")
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "compressed_2d")
	if !found {
		t.Skip("compressed_2d not found in fixture")
	}

	const rows, cols = 20, 30
	full, err := ds.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(full) != rows*cols {
		t.Fatalf("Read len = %d, want %d", len(full), rows*cols)
	}

	// Full-extent ReadSlice must equal Read exactly.
	r, err := ds.ReadSlice([]uint64{0, 0}, []uint64{rows, cols})
	if err != nil {
		t.Fatalf("full ReadSlice: %v", err)
	}
	fe, ok := r.([]float64)
	if !ok {
		t.Fatalf("ReadSlice type = %T, want []float64", r)
	}
	for i := range full {
		if fe[i] != full[i] {
			t.Fatalf("full-extent ReadSlice[%d] = %v, want %v (row %d col %d)",
				i, fe[i], full[i], i/cols, i%cols)
		}
	}

	// Sub-block spanning all three column chunks (cols 5..24) and four row
	// chunks (rows 3..14): rows [3,15), cols [5,25).
	const r0, c0, rn, cn = 3, 5, 12, 20
	rb, err := ds.ReadSlice([]uint64{r0, c0}, []uint64{rn, cn})
	if err != nil {
		t.Fatalf("band ReadSlice: %v", err)
	}
	band := rb.([]float64)
	for rr := 0; rr < rn; rr++ {
		for cc := 0; cc < cn; cc++ {
			got := band[rr*cn+cc]
			want := full[(rr+r0)*cols+(cc+c0)]
			if got != want {
				t.Fatalf("band[%d,%d] = %v, want %v", rr, cc, got, want)
			}
		}
	}
}
