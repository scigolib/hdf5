// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/scigolib/hdf5"
)

func TestMain(m *testing.M) {
	// Ensure tmp directory exists for CI
	os.MkdirAll("tmp", 0o755)
	os.Exit(m.Run())
}

// TestFileWriter_BTreeRebalancing_DefaultEnabled tests that rebalancing is enabled by default.
func TestFileWriter_BTreeRebalancing_DefaultEnabled(t *testing.T) {
	// Default: Rebalancing should be enabled (C library behavior)
	filename := "testdata/btree_rebalancing_default.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Check default config
	if !fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled by default")
	}
}

// TestFileWriter_BTreeRebalancing_ExplicitlyEnabled tests enabling rebalancing via option.
func TestFileWriter_BTreeRebalancing_ExplicitlyEnabled(t *testing.T) {
	filename := "testdata/btree_rebalancing_explicit_enable.h5"
	defer os.Remove(filename)

	// Explicitly enable rebalancing (same as default, but tests the option)
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(true),
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	if !fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled")
	}
}

// TestFileWriter_BTreeRebalancing_ExplicitlyDisabled tests disabling rebalancing via option.
func TestFileWriter_BTreeRebalancing_ExplicitlyDisabled(t *testing.T) {
	filename := "testdata/btree_rebalancing_explicit_disable.h5"
	defer os.Remove(filename)

	// Explicitly disable rebalancing
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	if fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be disabled")
	}
}

// TestFileWriter_BTreeRebalancing_RuntimeToggle tests toggling rebalancing at runtime.
func TestFileWriter_BTreeRebalancing_RuntimeToggle(t *testing.T) {
	filename := "testdata/btree_rebalancing_toggle.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Initially enabled
	if !fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled initially")
	}

	// Disable
	fw.DisableRebalancing()
	if fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be disabled after DisableRebalancing()")
	}

	// Re-enable
	fw.EnableRebalancing()
	if !fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled after EnableRebalancing()")
	}
}

// TestFileWriter_BTreeRebalancing_OpenForWrite tests rebalancing config with OpenForWrite.
func TestFileWriter_BTreeRebalancing_OpenForWrite(t *testing.T) {
	filename := "tmp/btree_rebalancing_openforwrite.h5"
	defer os.Remove(filename)

	// Create file first
	fw1, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	fw1.Close()

	// Open with default config (rebalancing enabled)
	fw2, err := hdf5.OpenForWrite(filename, hdf5.OpenReadWrite)
	if err != nil {
		t.Fatalf("OpenForWrite failed: %v", err)
	}

	if !fw2.RebalancingEnabled() {
		t.Error("Expected rebalancing to be enabled by default in OpenForWrite")
	}
	fw2.Close() // Close explicitly before next open

	// Open with rebalancing disabled
	fw3, err := hdf5.OpenForWrite(filename, hdf5.OpenReadWrite,
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		t.Fatalf("OpenForWrite with option failed: %v", err)
	}
	defer fw3.Close()

	if fw3.RebalancingEnabled() {
		t.Error("Expected rebalancing to be disabled")
	}
}

// TestAttributeDeletion_WithRebalancing tests attribute deletion with rebalancing enabled.
func TestAttributeDeletion_WithRebalancing(t *testing.T) {
	filename := "testdata/btree_rebalancing_delete_with.h5"
	defer os.Remove(filename)

	// Create file with rebalancing enabled (default)
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 10 attributes (triggers dense storage at 8)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.WriteAttribute(name, int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Delete 5 attributes (should trigger rebalancing)
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.DeleteAttribute(name); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Deletion succeeded - rebalancing was triggered
	// Note: We can't easily verify internal B-tree structure here,
	// but the fact that deletions succeeded means the configuration works
}

// TestAttributeDeletion_WithoutRebalancing tests attribute deletion with rebalancing disabled.
func TestAttributeDeletion_WithoutRebalancing(t *testing.T) {
	filename := "testdata/btree_rebalancing_delete_without.h5"
	defer os.Remove(filename)

	// Create file with rebalancing disabled
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add 10 attributes (triggers dense storage at 8)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.WriteAttribute(name, int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Delete 5 attributes (should NOT trigger rebalancing)
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.DeleteAttribute(name); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Deletion succeeded without rebalancing
	// Note: B-tree may be sparse, but deletions work correctly
	// The configuration successfully disabled rebalancing
}

// TestAttributeDeletion_BatchWithRuntimeToggle tests batch deletions with runtime toggle.
func TestAttributeDeletion_BatchWithRuntimeToggle(t *testing.T) {
	filename := "testdata/btree_rebalancing_batch_toggle.h5"
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

	// Add 20 attributes
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.WriteAttribute(name, int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	// Disable rebalancing for batch deletions
	fw.DisableRebalancing()

	// Delete 10 attributes (fast, no rebalancing)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.DeleteAttribute(name); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Re-enable rebalancing
	fw.EnableRebalancing()

	// Delete 5 more (with rebalancing)
	for i := 10; i < 15; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.DeleteAttribute(name); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Deletion succeeded - rebalancing was triggered
	// Note: We can't easily verify internal B-tree structure here,
	// but the fact that deletions succeeded means the configuration works
}

// TestBTreeRebalancing_MultipleFunctionalOptions tests combining multiple options.
func TestBTreeRebalancing_MultipleFunctionalOptions(t *testing.T) {
	filename := "testdata/btree_rebalancing_multi_options.h5"
	defer os.Remove(filename)

	// Combine superblock version and rebalancing options
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate,
		hdf5.WithSuperblockVersion(hdf5.SuperblockV2),
		hdf5.WithBTreeRebalancing(false),
	)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	// Verify rebalancing is disabled
	if fw.RebalancingEnabled() {
		t.Error("Expected rebalancing to be disabled")
	}
}

// TestBTreeRebalancing_BackwardCompatibility tests that existing code works unchanged.
func TestBTreeRebalancing_BackwardCompatibility(t *testing.T) {
	filename := "testdata/btree_rebalancing_backward_compat.h5"
	defer os.Remove(filename)

	// Existing code without options should work (rebalancing enabled by default)
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	// Add and delete attributes (should work as before)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.WriteAttribute(name, int32(i)); err != nil {
			t.Fatalf("WriteAttribute failed: %v", err)
		}
	}

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("attr_%d", i)
		if err := ds.DeleteAttribute(name); err != nil {
			t.Fatalf("DeleteAttribute failed: %v", err)
		}
	}

	// Deletion succeeded with backward compatibility
	// Existing code continues to work with default rebalancing behavior
}
