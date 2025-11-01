// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"fmt"
	"testing"

	"github.com/scigolib/hdf5"
)

// BenchmarkBTreeRebalancing_SmallDataset benchmarks rebalancing small B-tree (100 attributes).
//
// Expected performance (MVP single-leaf):
//   - <1ms (no-op, single leaf already optimal)
func BenchmarkBTreeRebalancing_SmallDataset(b *testing.B) {
	// 100 attributes
	benchmarkRebalancing(b, 100)
}

// BenchmarkBTreeRebalancing_MediumDataset benchmarks rebalancing medium B-tree (1000 attributes).
//
// Expected performance (MVP single-leaf):
//   - <1ms (no-op, single leaf already optimal)
//
// Future (multi-level):
//   - ~10-50ms
func BenchmarkBTreeRebalancing_MediumDataset(b *testing.B) {
	// 1000 attributes
	benchmarkRebalancing(b, 1000)
}

// BenchmarkBTreeRebalancing_LargeDataset benchmarks rebalancing large B-tree (10000 attributes).
//
// Expected performance (MVP single-leaf):
//   - <1ms (no-op, single leaf already optimal)
//
// Future (multi-level):
//   - ~100-500ms
func BenchmarkBTreeRebalancing_LargeDataset(b *testing.B) {
	// 10000 attributes
	benchmarkRebalancing(b, 10000)
}

// benchmarkRebalancing is the common benchmark implementation for rebalancing.
func benchmarkRebalancing(b *testing.B, numAttrs int) {
	b.StopTimer()

	// Setup: Create file with many attributes
	filename := fmt.Sprintf("%s/bench_rebalance_%d.h5", b.TempDir(), numAttrs)
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false), // Disable auto-rebalancing
	)
	if err != nil {
		b.Fatal(err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
	if err != nil {
		b.Fatal(err)
	}

	// Create attributes (triggers dense storage at 8+)
	for i := 0; i < numAttrs; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			b.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Delete half (makes B-tree sparse)
	for i := 0; i < numAttrs/2; i++ {
		if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
			b.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	b.StartTimer()

	// Benchmark: Manual rebalancing
	for i := 0; i < b.N; i++ {
		// Rebalance B-tree
		err := ds.RebalanceAttributeBTree()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBTreeDeletion_WithRebalancing benchmarks deletion with auto-rebalancing enabled.
//
// Expected performance (MVP single-leaf):
//   - ~5-10ms per deletion (includes B-tree search + record removal + rebalancing check)
//
// Future (multi-level):
//   - ~5-20ms per deletion (includes full rebalancing)
func BenchmarkBTreeDeletion_WithRebalancing(b *testing.B) {
	benchmarkDeletion(b, true, 1000)
}

// BenchmarkBTreeDeletion_WithoutRebalancing benchmarks deletion without auto-rebalancing.
//
// Expected performance (MVP single-leaf):
//   - ~0.5-1ms per deletion (only B-tree search + record removal, no rebalancing)
//
// Speedup vs with-rebalancing:
//   - ~5-10x faster (MVP)
//   - ~5-20x faster (future multi-level)
func BenchmarkBTreeDeletion_WithoutRebalancing(b *testing.B) {
	benchmarkDeletion(b, false, 1000)
}

// benchmarkDeletion is the common benchmark implementation for deletion operations.
func benchmarkDeletion(b *testing.B, rebalance bool, numAttrs int) {
	b.StopTimer()

	// Setup
	filename := fmt.Sprintf("%s/bench_delete_rebalance_%v.h5", b.TempDir(), rebalance)
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(rebalance),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
	if err != nil {
		b.Fatal(err)
	}

	// Create initial batch of attributes
	for j := 0; j < numAttrs; j++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
			b.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Reset timer to measure only deletions
	b.ResetTimer()
	b.StartTimer()

	// Benchmark: Delete attributes
	deletionCount := 0
	for i := 0; i < b.N; i++ {
		// Create attributes if we've deleted them all
		if deletionCount >= numAttrs {
			b.StopTimer()
			// Recreate attributes
			for j := 0; j < numAttrs; j++ {
				if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
					b.Fatalf("WriteAttribute failed: %v", err)
				}
			}
			deletionCount = 0
			b.StartTimer()
		}

		// Delete one attribute
		if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", deletionCount)); err != nil {
			b.Fatalf("DeleteAttribute failed: %v", err)
		}
		deletionCount++
	}
}

// BenchmarkBTreeDeletion_BatchWithManualRebalance benchmarks batch deletion pattern.
//
// Pattern:
//  1. Disable rebalancing
//  2. Delete N attributes (fast, no rebalancing)
//  3. Manual rebalance once
//
// Expected total time for 1000 deletions:
//   - MVP: ~1000 * 0.5ms + 1ms = ~501ms (vs ~10s with auto-rebalancing)
//   - Future: ~1000 * 1ms + 100ms = ~1.1s (vs ~20s with auto-rebalancing)
//
// Speedup: ~10-20x faster than auto-rebalancing!
func BenchmarkBTreeDeletion_BatchWithManualRebalance(b *testing.B) {
	b.StopTimer()

	numAttrs := 1000

	// Setup
	filename := fmt.Sprintf("%s/bench_batch_manual.h5", b.TempDir())
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		b.Fatal(err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		// Create attributes
		b.StopTimer()
		for j := 0; j < numAttrs; j++ {
			if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
				b.Fatalf("WriteAttribute failed: %v", err)
			}
		}
		b.StartTimer()

		// Disable rebalancing
		fw.DisableRebalancing()

		// Delete all attributes (fast, no rebalancing)
		for j := 0; j < numAttrs; j++ {
			if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", j)); err != nil {
				b.Fatalf("DeleteAttribute failed: %v", err)
			}
		}

		// Manual rebalance once
		if err := ds.RebalanceAttributeBTree(); err != nil {
			b.Fatalf("RebalanceAttributeBTree failed: %v", err)
		}

		// Re-enable for next iteration
		fw.EnableRebalancing()
	}
}

// BenchmarkBTreeDeletion_Comparison benchmarks deletion patterns side-by-side.
//
// This benchmark provides direct comparison of:
//  1. Auto-rebalancing (default)
//  2. No rebalancing (fast but leaves sparse tree)
//  3. Batch + manual rebalancing (best of both worlds)
//
//nolint:gocognit // Benchmark complexity is acceptable
func BenchmarkBTreeDeletion_Comparison(b *testing.B) {
	numAttrs := 100

	b.Run("AutoRebalance", func(b *testing.B) {
		benchmarkDeletion(b, true, numAttrs)
	})

	b.Run("NoRebalance", func(b *testing.B) {
		benchmarkDeletion(b, false, numAttrs)
	})

	b.Run("BatchManual", func(b *testing.B) {
		b.StopTimer()

		// Setup
		filename := fmt.Sprintf("%s/bench_comparison_batch.h5", b.TempDir())
		fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
		if err != nil {
			b.Fatal(err)
		}
		defer fw.Close()

		ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		b.StartTimer()

		for i := 0; i < b.N; i++ {
			// Create attributes
			b.StopTimer()
			for j := 0; j < numAttrs; j++ {
				if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
					b.Fatalf("WriteAttribute failed: %v", err)
				}
			}
			fw.DisableRebalancing()
			b.StartTimer()

			// Delete all
			for j := 0; j < numAttrs; j++ {
				if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", j)); err != nil {
					b.Fatalf("DeleteAttribute failed: %v", err)
				}
			}

			// Rebalance once
			if err := ds.RebalanceAttributeBTree(); err != nil {
				b.Fatalf("RebalanceAttributeBTree failed: %v", err)
			}

			fw.EnableRebalancing()
		}
	})
}

// BenchmarkFileWriter_RebalanceAllBTrees benchmarks global rebalancing.
//
// This benchmarks the FileWriter.RebalanceAllBTrees() method that
// rebalances B-trees for all datasets in the file.
//
// Expected performance (MVP):
//   - Small files (<10 datasets): <1ms
//   - Medium files (10-100 datasets): 1-10ms
//   - Large files (100+ datasets): 10-100ms
func BenchmarkFileWriter_RebalanceAllBTrees(b *testing.B) {
	numDatasets := 10

	b.StopTimer()

	// Setup: Create file with multiple datasets
	filename := fmt.Sprintf("%s/bench_rebalance_all.h5", b.TempDir())
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer fw.Close()

	// Create multiple datasets with attributes
	for i := 0; i < numDatasets; i++ {
		ds, err := fw.CreateDataset(fmt.Sprintf("/dataset_%d", i), hdf5.Float64, []uint64{10})
		if err != nil {
			b.Fatal(err)
		}

		// Add 20 attributes to each dataset
		for j := 0; j < 20; j++ {
			if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
				b.Fatalf("WriteAttribute failed: %v", err)
			}
		}

		// Delete 10 attributes (make B-tree sparse)
		for j := 0; j < 10; j++ {
			if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", j)); err != nil {
				b.Fatalf("DeleteAttribute failed: %v", err)
			}
		}
	}

	b.StartTimer()

	// Benchmark: Rebalance all B-trees
	for i := 0; i < b.N; i++ {
		if err := fw.RebalanceAllBTrees(); err != nil {
			b.Fatal(err)
		}
	}
}

// Example benchmark run command:
//
//   go test -bench=BenchmarkBTree -benchmem -benchtime=10s
//
// Expected output (MVP single-leaf B-trees):
//
//   BenchmarkBTreeRebalancing_SmallDataset-8          100000000       0.01 ns/op      0 B/op    0 allocs/op
//   BenchmarkBTreeRebalancing_MediumDataset-8         100000000       0.01 ns/op      0 B/op    0 allocs/op
//   BenchmarkBTreeRebalancing_LargeDataset-8          100000000       0.01 ns/op      0 B/op    0 allocs/op
//   BenchmarkBTreeDeletion_WithRebalancing-8          200             5000000 ns/op   X B/op    Y allocs/op
//   BenchmarkBTreeDeletion_WithoutRebalancing-8       2000            500000 ns/op    X B/op    Y allocs/op
//
// Speedup analysis:
//   - Deletion without rebalancing: ~10x faster
//   - Batch + manual rebalance: ~9x faster overall (vs auto-rebalance for all deletions)
