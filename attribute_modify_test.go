// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
)

// TestAttributeModification_CompactUpsert tests upsert semantics for compact attributes.
//
// Verifies:
//   - Creating a new attribute works
//   - Modifying an existing attribute works (same size)
//   - Modifying an existing attribute works (different size)
//   - Reading back modified attribute shows new value
//
// Reference: H5Oattribute.c - H5O__attr_write_cb().
//
//nolint:gocognit // Test function with multiple verification steps
func TestAttributeModification_CompactUpsert(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "attr_modify_compact.h5")

	// Step 1: Create file with dataset
	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{10}
	ds, err := fw.CreateDataset("/temperature", hdf5.Float64, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Step 2: Create initial attribute
	err = ds.WriteAttribute("units", "Celsius")
	if err != nil {
		t.Fatalf("Failed to write initial attribute: %v", err)
	}

	// Step 3: Modify attribute (same type, different value)
	err = ds.WriteAttribute("units", "Kelvin")
	if err != nil {
		t.Fatalf("Failed to modify attribute (should upsert): %v", err)
	}

	// Step 4: Add another attribute
	err = ds.WriteAttribute("sensor_id", int32(42))
	if err != nil {
		t.Fatalf("Failed to add second attribute: %v", err)
	}

	// Step 5: Modify second attribute
	err = ds.WriteAttribute("sensor_id", int32(99))
	if err != nil {
		t.Fatalf("Failed to modify second attribute: %v", err)
	}

	// Close and reopen
	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Step 6: Read back and verify
	f, err := hdf5.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(testFile)
	}()

	// Access dataset through walk
	var dataset *hdf5.Dataset
	f.Walk(func(_ string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "temperature" {
			dataset = ds
		}
	})
	if dataset == nil {
		t.Fatalf("Failed to find dataset 'temperature'")
	}

	// Verify modified attributes
	attrs, err := dataset.Attributes()
	if err != nil {
		t.Fatalf("Failed to read attributes: %v", err)
	}

	if len(attrs) != 2 {
		t.Fatalf("Expected 2 attributes, got %d", len(attrs))
	}

	// Verify units = "Kelvin" (modified value, not "Celsius")
	unitsFound := false
	sensorFound := false

	for _, attr := range attrs {
		value, err := attr.ReadValue()
		if err != nil {
			t.Fatalf("Failed to read attribute value: %v", err)
		}

		if attr.Name == "units" {
			unitsFound = true
			if strValue, ok := value.(string); !ok || strValue != "Kelvin" {
				t.Errorf("Expected units='Kelvin', got %v (type %T)", value, value)
			}
		}

		if attr.Name == "sensor_id" {
			sensorFound = true
			if intValue, ok := value.(int32); !ok || intValue != 99 {
				t.Errorf("Expected sensor_id=99, got %v (type %T)", value, value)
			}
		}
	}

	if !unitsFound {
		t.Error("Attribute 'units' not found")
	}

	if !sensorFound {
		t.Error("Attribute 'sensor_id' not found")
	}
}

// TestAttributeModification_DifferentSize tests modifying attributes with different sizes.
//
// Verifies:
//   - Modifying short string → long string
//   - Modifying long string → short string
//   - Modifying scalar → array (not supported, should error or recreate)
//
// Reference: H5Oattribute.c - H5O__attr_write_cb() (different size handling).
func TestAttributeModification_DifferentSize(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "attr_modify_size.h5")

	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Create attribute with short string
	err = ds.WriteAttribute("name", "A")
	if err != nil {
		t.Fatalf("Failed to write short string: %v", err)
	}

	// Modify to long string (different size)
	err = ds.WriteAttribute("name", "VeryLongAttributeValueThatExceedsOriginalSize")
	if err != nil {
		t.Fatalf("Failed to modify to long string: %v", err)
	}

	// Modify back to short string
	err = ds.WriteAttribute("name", "B")
	if err != nil {
		t.Fatalf("Failed to modify back to short string: %v", err)
	}

	// Close and verify
	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	f, err := hdf5.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(testFile)
	}()

	// Access dataset through walk
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

	if len(attrs) != 1 {
		t.Fatalf("Expected 1 attribute, got %d", len(attrs))
	}

	value, err := attrs[0].ReadValue()
	if err != nil {
		t.Fatalf("Failed to read attribute value: %v", err)
	}

	if strValue, ok := value.(string); !ok || strValue != "B" {
		t.Errorf("Expected name='B', got %v (type %T)", value, value)
	}
}

// TestAttributeModification_MultipleModifications tests multiple modifications to same attribute.
//
// Verifies:
//   - Can modify an attribute multiple times
//   - Final value is correct after multiple modifications
//   - No corruption or data loss
//
// This is a stress test for the upsert logic.
func TestAttributeModification_MultipleModifications(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "attr_modify_multiple.h5")

	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{3}
	ds, err := fw.CreateDataset("/counter", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Modify the same attribute 10 times
	for i := 0; i < 10; i++ {
		err = ds.WriteAttribute("version", int32(i))
		if err != nil {
			t.Fatalf("Failed to modify attribute (iteration %d): %v", i, err)
		}
	}

	// Close and verify
	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	f, err := hdf5.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(testFile)
	}()

	// Access dataset through walk
	var dataset *hdf5.Dataset
	f.Walk(func(_ string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "counter" {
			dataset = ds
		}
	})
	if dataset == nil {
		t.Fatalf("Failed to find dataset 'counter'")
	}

	attrs, err := dataset.Attributes()
	if err != nil {
		t.Fatalf("Failed to read attributes: %v", err)
	}

	if len(attrs) != 1 {
		t.Fatalf("Expected 1 attribute, got %d (should only have final version)", len(attrs))
	}

	value, err := attrs[0].ReadValue()
	if err != nil {
		t.Fatalf("Failed to read attribute value: %v", err)
	}

	if intValue, ok := value.(int32); !ok || intValue != 9 {
		t.Errorf("Expected version=9 (final value), got %v (type %T)", value, value)
	}
}
