// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/scigolib/hdf5"
)

// TestDatasetWriter_RebalanceAttributeBTree_CompactStorage tests rebalancing on compact storage.
//
// Compact storage (0-7 attributes) doesn't use B-trees, so rebalancing should be a no-op.
func TestDatasetWriter_RebalanceAttributeBTree_CompactStorage(t *testing.T) {
	filename := "testdata/rebalance_compact.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 5 attributes (compact storage)
	for i := 0; i < 5; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Rebalance should work (no-op for compact storage)
	if err := ds.RebalanceAttributeBTree(); err != nil {
		t.Errorf("RebalanceAttributeBTree failed: %v", err)
	}
}

// TestDatasetWriter_RebalanceAttributeBTree_DenseStorage tests rebalancing on dense storage.
//
// Dense storage (8+ attributes) uses B-trees. For MVP (single-leaf), rebalancing is a no-op
// but should execute without errors.
func TestDatasetWriter_RebalanceAttributeBTree_DenseStorage(t *testing.T) {
	filename := "testdata/rebalance_dense.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 10 attributes (triggers dense storage at 8)
	for i := 0; i < 10; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Rebalance should work
	if err := ds.RebalanceAttributeBTree(); err != nil {
		t.Errorf("RebalanceAttributeBTree failed: %v", err)
	}
}

// TestDatasetWriter_RebalanceAttributeBTree_AfterDeletion tests rebalancing after batch deletions.
//
// This is the primary use case: disable rebalancing, delete many attributes, then rebalance manually.
func TestDatasetWriter_RebalanceAttributeBTree_AfterDeletion(t *testing.T) {
	filename := "testdata/rebalance_after_deletion.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false), // Disable auto-rebalancing
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 20 attributes
	for i := 0; i < 20; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Delete 10 attributes (fast, no rebalancing)
	for i := 0; i < 10; i++ {
		if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Manual rebalance
	if err := ds.RebalanceAttributeBTree(); err != nil {
		t.Errorf("RebalanceAttributeBTree failed: %v", err)
	}

	// Verify remaining attributes still accessible (validate B-tree integrity)
	// Note: This would require Read API, which is not in write-only package
	// For now, just verify no errors during rebalancing
}

// TestDatasetWriter_RebalanceAttributeBTree_NoAttributes tests rebalancing on dataset with no attributes.
//
// Should be a no-op without errors.
func TestDatasetWriter_RebalanceAttributeBTree_NoAttributes(t *testing.T) {
	filename := "testdata/rebalance_no_attrs.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// No attributes added

	// Rebalance should work (no-op)
	if err := ds.RebalanceAttributeBTree(); err != nil {
		t.Errorf("RebalanceAttributeBTree failed: %v", err)
	}
}

// TestDatasetWriter_RebalanceAttributeBTree_OpenForWrite tests rebalancing on reopened file.
//
// Tests the RMW (read-modify-write) path with cached object header.
func TestDatasetWriter_RebalanceAttributeBTree_OpenForWrite(t *testing.T) {
	filename := "tmp/rebalance_open_for_write.h5"
	defer os.Remove(filename)

	// Create file with dense attributes
	{
		fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
		if err != nil {
			t.Fatalf("CreateForWrite failed: %v", err)
		}

		ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
		if err != nil {
			t.Fatalf("CreateDataset failed: %v", err)
		}

		// Add 10 attributes (triggers dense storage)
		for i := 0; i < 10; i++ {
			if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
				t.Fatalf("WriteAttribute failed: %v", err)
			}
		}

		fw.Close()
	}

	// Reopen and test rebalancing
	{
		fw, err := hdf5.OpenForWrite(filename, hdf5.OpenReadWrite)
		if err != nil {
			t.Fatalf("OpenForWrite failed: %v", err)
		}
		defer fw.Close()

		ds, err := fw.OpenDataset("/data")
		if err != nil {
			t.Fatalf("OpenDataset failed: %v", err)
		}

		// Rebalance should work with cached object header
		if err := ds.RebalanceAttributeBTree(); err != nil {
			t.Errorf("RebalanceAttributeBTree failed: %v", err)
		}
	}
}

// TestFileWriter_RebalanceAllBTrees tests global rebalancing.
//
// This tests the FileWriter.RebalanceAllBTrees() method that rebalances
// all datasets in the file.
func TestFileWriter_RebalanceAllBTrees(t *testing.T) {
	filename := "testdata/rebalance_all_btrees.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Create multiple datasets with attributes
	for i := 0; i < 3; i++ {
		ds, err := fw.CreateDataset(fmt.Sprintf("/dataset_%d", i), hdf5.Float64, []uint64{10})
		if err != nil {
			t.Fatalf("CreateDataset failed: %v", err)
		}

		// Add 15 attributes to each dataset
		for j := 0; j < 15; j++ {
			if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", j), int32(j)); err != nil {
				t.Fatalf("WriteAttribute failed: %v", err)
			}
		}

		// Delete 7 attributes
		for j := 0; j < 7; j++ {
			if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", j)); err != nil {
				t.Fatalf("DeleteAttribute failed: %v", err)
			}
		}
	}

	// Rebalance all B-trees
	if err := fw.RebalanceAllBTrees(); err != nil {
		t.Errorf("RebalanceAllBTrees failed: %v", err)
	}
}

// TestFileWriter_RebalanceAllBTrees_EmptyFile tests rebalancing on empty file.
//
// Should be a no-op without errors.
func TestFileWriter_RebalanceAllBTrees_EmptyFile(t *testing.T) {
	filename := "testdata/rebalance_empty_file.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// No datasets created

	// Rebalance should work (no-op)
	if err := fw.RebalanceAllBTrees(); err != nil {
		t.Errorf("RebalanceAllBTrees failed: %v", err)
	}
}

// TestBatchDeletionWorkflow tests the recommended batch deletion workflow.
//
// This demonstrates the optimal pattern for batch deletions:
//  1. Disable rebalancing
//  2. Delete many attributes (fast)
//  3. Manual rebalance once
//  4. Re-enable rebalancing
func TestBatchDeletionWorkflow(t *testing.T) {
	filename := "testdata/batch_deletion_workflow.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 100 attributes
	for i := 0; i < 100; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Step 1: Disable rebalancing
	fw.DisableRebalancing()
	if fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be disabled")
	}

	// Step 2: Delete 50 attributes (fast, no rebalancing)
	for i := 0; i < 50; i++ {
		if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Step 3: Manual rebalance once
	if err := ds.RebalanceAttributeBTree(); err != nil {
		t.Errorf("RebalanceAttributeBTree failed: %v", err)
	}

	// Step 4: Re-enable rebalancing
	fw.EnableRebalancing()
	if !fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled")
	}

	// Continue with normal operations (auto-rebalancing now active)
	// Delete a few more (with rebalancing)
	for i := 50; i < 55; i++ {
		if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}
}

// TestRebalancing_MultipleInvocations tests that rebalancing can be called multiple times.
//
// Calling RebalanceAttributeBTree() multiple times should be safe (idempotent for MVP).
func TestRebalancing_MultipleInvocations(t *testing.T) {
	filename := "testdata/rebalance_multiple.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 10 attributes
	for i := 0; i < 10; i++ {
		if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Call rebalance multiple times (should be safe)
	for i := 0; i < 5; i++ {
		if err := ds.RebalanceAttributeBTree(); err != nil {
			t.Errorf("RebalanceAttributeBTree invocation %d failed: %v", i+1, err)
		}
	}

	// Also test FileWriter global rebalancing
	for i := 0; i < 3; i++ {
		if err := fw.RebalanceAllBTrees(); err != nil {
			t.Errorf("RebalanceAllBTrees invocation %d failed: %v", i+1, err)
		}
	}
}
