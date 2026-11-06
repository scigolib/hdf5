package hdf5

import (
	"path/filepath"
	"testing"
)

// Read-path benchmarks for TASK-062 (ADR-004 §5: no regression > 5 %). They
// use only API that predates the descriptor, so the same file runs on the base
// commit for the before numbers.

// benchFile writes a float64 dataset of the given shape once per benchmark and
// returns the opened dataset.
func benchFile(b *testing.B, dims []uint64, opts ...DatasetOption) *Dataset {
	b.Helper()

	path := filepath.Join(b.TempDir(), "bench.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	if err != nil {
		b.Fatal(err)
	}

	n := uint64(1)
	for _, d := range dims {
		n *= d
	}
	vals := make([]float64, n)
	for i := range vals {
		vals[i] = float64(i)
	}

	dw, err := fw.CreateDataset("/x", Float64, dims, opts...)
	if err != nil {
		b.Fatal(err)
	}
	if err := dw.Write(vals); err != nil {
		b.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		b.Fatal(err)
	}

	f, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = f.Close() })

	var ds *Dataset
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/x" {
			ds = d
		}
	})
	if ds == nil {
		b.Fatal("dataset /x not found")
	}

	return ds
}

func benchReadSlice(b *testing.B, ds *Dataset, start, count []uint64) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ds.ReadSlice(start, count); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRead(b *testing.B, ds *Dataset) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ds.Read(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadSlice_Contig2DPartial: a 200×50 window of a 1000×1000 dataset,
// the shape the former 2-D path read one element per ReadAt.
func BenchmarkReadSlice_Contig2DPartial(b *testing.B) {
	benchReadSlice(b, benchFile(b, []uint64{1000, 1000}), []uint64{100, 100}, []uint64{200, 50})
}

// BenchmarkReadSlice_Contig2DRows: whole rows, one contiguous range.
func BenchmarkReadSlice_Contig2DRows(b *testing.B) {
	benchReadSlice(b, benchFile(b, []uint64{1000, 1000}), []uint64{100, 0}, []uint64{200, 1000})
}

// BenchmarkReadSlice_Contig1D: a 10,000-element window of a 1,000,000-element dataset.
func BenchmarkReadSlice_Contig1D(b *testing.B) {
	benchReadSlice(b, benchFile(b, []uint64{1_000_000}), []uint64{500_000}, []uint64{10_000})
}

// BenchmarkReadSlice_Contig3D: a 10×20×30 box of a 50×50×50 dataset.
func BenchmarkReadSlice_Contig3D(b *testing.B) {
	benchReadSlice(b, benchFile(b, []uint64{50, 50, 50}), []uint64{0, 0, 0}, []uint64{10, 20, 30})
}

// BenchmarkReadSlice_Chunked2D: a 200×50 window of a 1000×1000 dataset in
// 100×100 chunks.
func BenchmarkReadSlice_Chunked2D(b *testing.B) {
	ds := benchFile(b, []uint64{1000, 1000}, WithChunkDims([]uint64{100, 100}))
	benchReadSlice(b, ds, []uint64{100, 100}, []uint64{200, 50})
}

// BenchmarkRead_Contig: the whole 1000×1000 dataset (8 MB).
func BenchmarkRead_Contig(b *testing.B) {
	benchRead(b, benchFile(b, []uint64{1000, 1000}))
}

// BenchmarkRead_Chunked: the whole 1000×1000 dataset in 100×100 chunks.
func BenchmarkRead_Chunked(b *testing.B) {
	benchRead(b, benchFile(b, []uint64{1000, 1000}, WithChunkDims([]uint64{100, 100})))
}

// BenchmarkRead_ReferenceStripmine: thyperslab.h5 /stripmine of the official
// suite (131,104 contiguous float64 values).
func BenchmarkRead_ReferenceStripmine(b *testing.B) {
	f, err := Open(filepath.Join("testdata", "hdf5_official", "thyperslab.h5"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = f.Close() })

	var ds *Dataset
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/stripmine" {
			ds = d
		}
	})
	if ds == nil {
		b.Fatal("/stripmine not found")
	}

	benchRead(b, ds)
}
