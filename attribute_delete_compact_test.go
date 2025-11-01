// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
)

// TestAttributeDeletion_Compact tests deletion of attributes from compact storage.
//
// Verifies:
//   - Creating 3 attributes in compact storage (< 8)
//   - Deleting 1 attribute
//   - Remaining 2 attributes are intact
//   - Deleted attribute is not found
//
// Reference: H5Adelete.c - H5A__delete(), H5O.c - H5O_msg_remove().
//
//nolint:gocognit // Test function with multiple verification steps
func TestAttributeDeletion_Compact(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hdf5_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "attr_delete_compact.h5")

	// Step 1: Create file with dataset and 3 attributes (compact storage)
	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Create 3 compact attributes
	err = ds.WriteAttribute("attr0", int32(0))
	if err != nil {
		t.Fatalf("Failed to write attr0: %v", err)
	}

	err = ds.WriteAttribute("attr1", int32(10))
	if err != nil {
		t.Fatalf("Failed to write attr1: %v", err)
	}

	err = ds.WriteAttribute("attr2", int32(20))
	if err != nil {
		t.Fatalf("Failed to write attr2: %v", err)
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after creation: %v", err)
	}

	// Step 2: Reopen file and delete one attribute
	fw, err = hdf5.OpenForWrite(testFile, hdf5.OpenReadWrite)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}

	dsw, err := fw.OpenDataset("/data")
	if err != nil {
		t.Fatalf("Failed to open dataset: %v", err)
	}

	// Delete middle attribute
	err = dsw.DeleteAttribute("attr1")
	if err != nil {
		t.Fatalf("Failed to delete attr1: %v", err)
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after deletion: %v", err)
	}

	// Step 3: Verify
	f, err := hdf5.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to reopen file for verification: %v", err)
	}

	var dataset *hdf5.Dataset
	f.Walk(func(_ string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "data" {
			dataset = ds
		}
	})
	if dataset == nil {
		t.Fatalf("Failed to find dataset 'data'")
	}

	attrs, err := dataset.Attributes()
	if err != nil {
		t.Fatalf("Failed to read attributes: %v", err)
	}

	if len(attrs) != 2 {
		t.Fatalf("Expected 2 attributes, got %d", len(attrs))
	}

	// Verify attr1 is gone, attr0 and attr2 remain
	attr0Found := false
	attr2Found := false

	for _, attr := range attrs {
		if attr.Name == "attr1" {
			t.Error("Deleted attribute 'attr1' still exists")
		}

		if attr.Name == "attr0" {
			attr0Found = true
			value, err := attr.ReadValue()
			if err != nil {
				t.Errorf("Failed to read attr0: %v", err)
			} else if intValue, ok := value.(int32); !ok || intValue != 0 {
				t.Errorf("Expected attr0=0, got %v (type %T)", value, value)
			}
		}

		if attr.Name == "attr2" {
			attr2Found = true
			value, err := attr.ReadValue()
			if err != nil {
				t.Errorf("Failed to read attr2: %v", err)
			} else if intValue, ok := value.(int32); !ok || intValue != 20 {
				t.Errorf("Expected attr2=20, got %v (type %T)", value, value)
			}
		}
	}

	if !attr0Found {
		t.Error("Attribute 'attr0' not found")
	}

	if !attr2Found {
		t.Error("Attribute 'attr2' not found")
	}

	// Close file before cleanup
	if err := f.Close(); err != nil {
		t.Errorf("Failed to close file: %v", err)
	}
}
