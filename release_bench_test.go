package hdf5_test

// Release comparison benchmarks (v0.14.2 vs v0.14.3): the same file is copied
// into both trees and run with benchstat. Only API present in both versions.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
)

// buildManyDatasets writes a file with n small datasets, each with 3 attributes.
func buildManyDatasets(b *testing.B, n int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "many.h5")
	fw, err := hdf5.CreateForWrite(path, hdf5.CreateTruncate)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		ds, err := fw.CreateDataset(fmt.Sprintf("/d%04d", i), hdf5.Float64, []uint64{16})
		if err != nil {
			b.Fatal(err)
		}
		if err := ds.Write(make([]float64, 16)); err != nil {
			b.Fatal(err)
		}
		for a := 0; a < 3; a++ {
			if err := ds.WriteAttribute(fmt.Sprintf("a%d", a), int32(a)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := fw.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkPerf_OpenForWrite_200Datasets(b *testing.B) {
	path := buildManyDatasets(b, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fw, err := hdf5.OpenForWrite(path, hdf5.OpenReadWrite)
		if err != nil {
			b.Fatal(err)
		}
		if err := fw.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPerf_OpenWalk_200Datasets(b *testing.B) {
	path := buildManyDatasets(b, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := hdf5.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		f.Walk(func(string, hdf5.Object) {})
		if err := f.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// 80 children stays below the size where v0.14.2 wrote broken groups.
func BenchmarkPerf_CreateGroup80Children(b *testing.B) {
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fw, err := hdf5.CreateForWrite(filepath.Join(dir, "g.h5"), hdf5.CreateTruncate)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := fw.CreateGroup("/g"); err != nil {
			b.Fatal(err)
		}
		for c := 0; c < 80; c++ {
			if _, err := fw.CreateGroup(fmt.Sprintf("/g/c%03d", c)); err != nil {
				b.Fatal(err)
			}
		}
		if err := fw.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// buildChunked writes a 2-D chunked float64 dataset of rows×cols, chunks 8×cols.
func buildChunked(b *testing.B, rows, cols uint64) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "chunked.h5")
	fw, err := hdf5.CreateForWrite(path, hdf5.CreateTruncate)
	if err != nil {
		b.Fatal(err)
	}
	ds, err := fw.CreateDataset("/c", hdf5.Float64, []uint64{rows, cols}, hdf5.WithChunkDims([]uint64{8, cols}))
	if err != nil {
		b.Fatal(err)
	}
	if err := ds.Write(make([]float64, rows*cols)); err != nil {
		b.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// findDataset returns the dataset at path through Walk (API of both versions).
func findDataset(b *testing.B, f *hdf5.File, path string) *hdf5.Dataset {
	b.Helper()
	var ds *hdf5.Dataset
	f.Walk(func(p string, obj hdf5.Object) {
		if d, ok := obj.(*hdf5.Dataset); ok && p == path {
			ds = d
		}
	})
	if ds == nil {
		b.Fatalf("dataset %s not found", path)
	}
	return ds
}

// Many small slices of a chunked dataset: a per-slice cost that grows with the
// number of chunks shows up as a superlinear time between the two sizes.
func benchSlices(b *testing.B, rows uint64) {
	const cols = 64
	path := buildChunked(b, rows, cols)
	f, err := hdf5.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	ds := findDataset(b, f, "/c")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for r := uint64(0); r < rows; r += rows / 64 {
			if _, err := ds.ReadSlice([]uint64{r, 0}, []uint64{1, cols}); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPerf_ReadSlice64_Chunks256(b *testing.B)  { benchSlices(b, 256*8) }
func BenchmarkPerf_ReadSlice64_Chunks1024(b *testing.B) { benchSlices(b, 1024*8) }

func BenchmarkPerf_ReadAll_Chunked(b *testing.B) {
	path := buildChunked(b, 4096, 256)
	f, err := hdf5.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	ds := findDataset(b, f, "/c")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ds.Read(); err != nil {
			b.Fatal(err)
		}
	}
}
