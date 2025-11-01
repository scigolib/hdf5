// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scigolib/hdf5"
	"github.com/scigolib/hdf5/internal/structures"
)

// TestIncrementalRebalancing_WithDefer demonstrates BEST PRACTICE usage.
//
// Pattern: Enable incremental mode, defer Stop() for explicit cleanup.
// This is the recommended approach for production code.
func TestIncrementalRebalancing_WithDefer(t *testing.T) {
	filename := "test_incremental_defer.h5"
	defer os.Remove(filename)

	// Track goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Create file and enable lazy mode
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Enable lazy rebalancing (prerequisite for incremental)
	lazyConfig := structures.DefaultLazyConfig()
	if err := fw.EnableLazyRebalancing(lazyConfig); err != nil {
		t.Fatalf("Failed to enable lazy rebalancing: %v", err)
	}

	// Enable incremental rebalancing with progress callback
	var callbackCalled atomic.Int32
	config := structures.DefaultIncrementalConfig()
	config.Budget = 50 * time.Millisecond    // Fast for testing
	config.Interval = 100 * time.Millisecond // Frequent for testing
	config.ProgressCallback = func(p structures.RebalancingProgress) {
		callbackCalled.Add(1)
		t.Logf("Progress: rebalanced=%d, remaining=%d, complete=%v",
			p.NodesRebalanced, p.NodesRemaining, p.IsComplete)
	}

	if err := fw.EnableIncrementalRebalancing(config); err != nil {
		t.Fatalf("Failed to enable incremental rebalancing: %v", err)
	}

	// ✅ BEST PRACTICE: Explicit defer Stop()
	defer func() {
		if err := fw.StopIncrementalRebalancing(); err != nil {
			t.Errorf("Failed to stop incremental rebalancing: %v", err)
		}
	}()

	// Create dataset with attributes to trigger B-tree work
	ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{100})
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Write dataset data
	data := make([]int32, 100)
	for i := range data {
		data[i] = int32(i)
	}
	if err := ds.Write(data); err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	// Write many attributes (would trigger B-tree operations in dense storage)
	// Note: For MVP with compact storage, this won't trigger goroutine,
	// but demonstrates proper usage pattern
	for i := 0; i < 10; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("Failed to write attribute: %v", err)
		}
	}

	// Wait a bit to let goroutine run (if active)
	time.Sleep(200 * time.Millisecond)

	// Close file (which also calls Stop() as safety net)
	if err := fw.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Verify goroutines cleaned up
	// Allow small increase for runtime background goroutines
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Goroutine leak detected: initial=%d, final=%d",
			initialGoroutines, finalGoroutines)
	}

	t.Logf("✅ No goroutine leak: initial=%d, final=%d",
		initialGoroutines, finalGoroutines)

	// Note: Callback might not be called if no actual B-tree work happened (MVP)
	t.Logf("Callback called %d times", callbackCalled.Load())
}

// TestIncrementalRebalancing_AutomaticCleanup demonstrates SAFETY NET.
//
// Pattern: Enable incremental mode WITHOUT defer Stop().
// Close() should automatically stop goroutines (foolproof).
func TestIncrementalRebalancing_AutomaticCleanup(t *testing.T) {
	filename := "test_incremental_auto.h5"
	defer os.Remove(filename)

	// Track goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Create file
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Enable lazy + incremental
	lazyConfig := structures.DefaultLazyConfig()
	if err := fw.EnableLazyRebalancing(lazyConfig); err != nil {
		t.Fatalf("Failed to enable lazy rebalancing: %v", err)
	}

	config := structures.DefaultIncrementalConfig()
	config.Budget = 50 * time.Millisecond
	config.Interval = 100 * time.Millisecond

	if err := fw.EnableIncrementalRebalancing(config); err != nil {
		t.Fatalf("Failed to enable incremental rebalancing: %v", err)
	}

	// ⚠️ INTENTIONALLY NO defer Stop() - testing automatic cleanup!

	// Create dataset
	ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{50})
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	data := make([]int32, 50)
	if err := ds.Write(data); err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	// Just call Close() - should automatically stop goroutines
	if err := fw.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Verify no goroutine leak even without explicit Stop()
	time.Sleep(100 * time.Millisecond) // Let goroutines finish
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Goroutine leak detected (no defer): initial=%d, final=%d",
			initialGoroutines, finalGoroutines)
	}

	t.Logf("✅ Automatic cleanup works: initial=%d, final=%d",
		initialGoroutines, finalGoroutines)
}

// TestIncrementalRebalancing_MultipleStopCalls verifies Stop() is idempotent.
//
// Pattern: Call Stop() multiple times should be safe.
func TestIncrementalRebalancing_MultipleStopCalls(t *testing.T) {
	filename := "test_incremental_multiple.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	// Enable lazy + incremental
	if err := fw.EnableLazyRebalancing(structures.DefaultLazyConfig()); err != nil {
		t.Fatalf("Failed to enable lazy rebalancing: %v", err)
	}

	if err := fw.EnableIncrementalRebalancing(structures.DefaultIncrementalConfig()); err != nil {
		t.Fatalf("Failed to enable incremental rebalancing: %v", err)
	}

	// Call Stop() multiple times
	for i := 0; i < 3; i++ {
		if err := fw.StopIncrementalRebalancing(); err != nil {
			t.Errorf("Stop() call %d failed: %v", i+1, err)
		}
	}

	// Close() will call Stop() again internally - should be safe
	// (tested by defer fw.Close() above)
}

// TestIncrementalRebalancing_NotEnabled verifies Stop() on non-enabled file is safe.
func TestIncrementalRebalancing_NotEnabled(t *testing.T) {
	filename := "test_incremental_not_enabled.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	// Call Stop() without enabling incremental - should be safe (no-op)
	if err := fw.StopIncrementalRebalancing(); err != nil {
		t.Errorf("Stop() on non-enabled file failed: %v", err)
	}

	// Close() will also call Stop() - should be safe
}

// TestIncrementalRebalancing_ErrorCases tests error conditions.
//
// Note: For MVP, FileWriter methods are NO-OPs (incremental mode is per-dataset).
// This test documents expected behavior for future global tracking.
func TestIncrementalRebalancing_ErrorCases(t *testing.T) {
	filename := "test_incremental_errors.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	// Test 1: Enable incremental (MVP: no-op, returns nil)
	config := structures.DefaultIncrementalConfig()
	err = fw.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Errorf("EnableIncrementalRebalancing failed: %v", err)
	}
	t.Logf("✅ EnableIncrementalRebalancing (MVP no-op): %v", err)

	// Test 2: Enable lazy (MVP: no-op, returns nil)
	if err := fw.EnableLazyRebalancing(structures.DefaultLazyConfig()); err != nil {
		t.Errorf("EnableLazyRebalancing failed: %v", err)
	}

	// Test 3: Stop incremental (MVP: no-op, returns nil)
	err = fw.StopIncrementalRebalancing()
	if err != nil {
		t.Errorf("StopIncrementalRebalancing failed: %v", err)
	}
	t.Logf("✅ StopIncrementalRebalancing (MVP no-op): %v", err)

	// Future: When global BTree tracking implemented, test:
	// - Enable incremental without lazy mode (should error)
	// - Enable incremental twice (should error)
	// - Stop when not enabled (should succeed)
}

// BenchmarkIncrementalRebalancing_WithVsWithout compares performance.
//
// This benchmark demonstrates the performance benefit of incremental mode.
//
//nolint:gocognit // Benchmark complexity is acceptable
func BenchmarkIncrementalRebalancing_WithVsWithout(b *testing.B) {
	b.Run("Without_Incremental", func(b *testing.B) {
		filenames := make([]string, 0, b.N)
		defer func() {
			for _, fn := range filenames {
				os.Remove(fn)
			}
		}()

		for i := 0; i < b.N; i++ {
			filename := fmt.Sprintf("bench_no_inc_%d.h5", i)
			filenames = append(filenames, filename)

			fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
			if err != nil {
				b.Fatalf("Failed to create file: %v", err)
			}

			ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{100})
			if err != nil {
				b.Fatalf("Failed to create dataset: %v", err)
			}

			data := make([]int32, 100)
			if err := ds.Write(data); err != nil {
				b.Fatalf("Failed to write data: %v", err)
			}

			// Write attributes (immediate rebalancing)
			for j := 0; j < 50; j++ {
				if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
					b.Fatalf("Failed to write attribute: %v", err)
				}
			}

			if err := fw.Close(); err != nil {
				b.Fatalf("Failed to close file: %v", err)
			}
		}
	})

	b.Run("With_Incremental", func(b *testing.B) {
		filenames := make([]string, 0, b.N)
		defer func() {
			for _, fn := range filenames {
				os.Remove(fn)
			}
		}()

		for i := 0; i < b.N; i++ {
			filename := fmt.Sprintf("bench_with_inc_%d.h5", i)
			filenames = append(filenames, filename)

			fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
			if err != nil {
				b.Fatalf("Failed to create file: %v", err)
			}

			// Enable lazy + incremental
			if err := fw.EnableLazyRebalancing(structures.DefaultLazyConfig()); err != nil {
				b.Fatalf("Failed to enable lazy rebalancing: %v", err)
			}

			config := structures.DefaultIncrementalConfig()
			config.Budget = 10 * time.Millisecond
			config.Interval = 1 * time.Second // Infrequent for benchmark

			if err := fw.EnableIncrementalRebalancing(config); err != nil {
				b.Fatalf("Failed to enable incremental rebalancing: %v", err)
			}
			// Note: Close() will automatically stop incremental rebalancing
			// No defer needed in benchmark loop (automatic cleanup)

			ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{100})
			if err != nil {
				b.Fatalf("Failed to create dataset: %v", err)
			}

			data := make([]int32, 100)
			if err := ds.Write(data); err != nil {
				b.Fatalf("Failed to write data: %v", err)
			}

			// Write attributes (background rebalancing)
			for j := 0; j < 50; j++ {
				if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
					b.Fatalf("Failed to write attribute: %v", err)
				}
			}

			if err := fw.Close(); err != nil {
				b.Fatalf("Failed to close file: %v", err)
			}
		}
	})
}
