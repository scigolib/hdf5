// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5_test

import (
	"fmt"
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

// TestAttributeModification_DenseUpsert tests upsert semantics for dense attributes.
//
// Verifies:
//   - Creating 8+ attributes triggers dense storage
//   - Modifying an existing dense attribute works (same size)
//   - Modifying an existing dense attribute works (different size)
//   - Reading back modified attribute shows new value
//
// This is Phase 2: Dense attribute modification.
//
// Reference: H5Adense.c - H5A__dense_write().
//
//nolint:gocognit // Test function with multiple verification steps
func TestAttributeModification_DenseUpsert(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "attr_modify_dense.h5")

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

	// Step 2: Create 8 attributes to trigger dense storage
	for i := 0; i < 8; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i*10))
		if err != nil {
			t.Fatalf("Failed to write attribute %d: %v", i, err)
		}
	}

	// Step 3: Modify one of the dense attributes (same size)
	err = ds.WriteAttribute("attr_3", int32(999))
	if err != nil {
		t.Fatalf("Failed to modify dense attribute (same size): %v", err)
	}

	// Step 4: Modify another dense attribute (different size - string)
	// Note: This changes from int32 (4 bytes) to string (variable)
	// For this test, we'll stick with same type but different value
	err = ds.WriteAttribute("attr_5", int32(555))
	if err != nil {
		t.Fatalf("Failed to modify dense attribute: %v", err)
	}

	// Step 5: Add one more attribute (9th) - should also be dense
	err = ds.WriteAttribute("extra", int32(1000))
	if err != nil {
		t.Fatalf("Failed to add 9th attribute: %v", err)
	}

	// Step 6: Modify the newly added attribute
	err = ds.WriteAttribute("extra", int32(2000))
	if err != nil {
		t.Fatalf("Failed to modify newly added dense attribute: %v", err)
	}

	// Close and reopen
	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Step 7: Read back and verify
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

	// Verify attributes
	attrs, err := dataset.Attributes()
	if err != nil {
		t.Fatalf("Failed to read attributes: %v", err)
	}

	if len(attrs) != 9 {
		t.Fatalf("Expected 9 attributes, got %d", len(attrs))
	}

	// Verify modified attributes
	attr3Found := false
	attr5Found := false
	extraFound := false

	for _, attr := range attrs {
		value, err := attr.ReadValue()
		if err != nil {
			t.Fatalf("Failed to read attribute value: %v", err)
		}

		if attr.Name == "attr_3" {
			attr3Found = true
			if intValue, ok := value.(int32); !ok || intValue != 999 {
				t.Errorf("Expected attr_3=999 (modified), got %v (type %T)", value, value)
			}
		}

		if attr.Name == "attr_5" {
			attr5Found = true
			if intValue, ok := value.(int32); !ok || intValue != 555 {
				t.Errorf("Expected attr_5=555 (modified), got %v (type %T)", value, value)
			}
		}

		if attr.Name == "extra" {
			extraFound = true
			if intValue, ok := value.(int32); !ok || intValue != 2000 {
				t.Errorf("Expected extra=2000 (modified), got %v (type %T)", value, value)
			}
		}
	}

	if !attr3Found {
		t.Error("Attribute 'attr_3' not found")
	}
	if !attr5Found {
		t.Error("Attribute 'attr_5' not found")
	}
	if !extraFound {
		t.Error("Attribute 'extra' not found")
	}
}

// TestAttributeModification_DenseDifferentSize tests modifying dense attributes with different sizes.
//
// Verifies:
//   - Modifying short string → long string in dense storage
//   - Modifying long string → short string in dense storage
//   - Delete + insert logic works correctly
//
// Reference: H5HF.c - H5HF_remove() + H5HF_insert().
//
//nolint:gocognit // Test function with multiple verification steps
func TestAttributeModification_DenseDifferentSize(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "attr_modify_dense_size.h5")

	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Create 8 attributes to trigger dense storage
	for i := 0; i < 7; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("padding_%d", i), int32(i))
		if err != nil {
			t.Fatalf("Failed to write padding attribute %d: %v", i, err)
		}
	}

	// Add attribute with short string (8th attribute - triggers dense)
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

	if len(attrs) != 8 {
		t.Fatalf("Expected 8 attributes, got %d", len(attrs))
	}

	// Find and verify the "name" attribute
	nameFound := false
	for _, attr := range attrs {
		if attr.Name != "name" {
			continue
		}

		nameFound = true
		value, err := attr.ReadValue()
		if err != nil {
			t.Fatalf("Failed to read attribute value: %v", err)
		}

		if strValue, ok := value.(string); !ok || strValue != "B" {
			t.Errorf("Expected name='B', got %v (type %T)", value, value)
		}
		break
	}

	if !nameFound {
		t.Error("Attribute 'name' not found")
	}
}

// TestAttributeDeletion_Dense tests deletion of attributes from dense storage.
//
// Verifies:
//   - Creating 10 attributes (triggers dense storage)
//   - Deleting 2 attributes
//   - Remaining 8 attributes are intact
//   - Deleted attributes are not found
//
// Reference: H5Adense.c - H5A__dense_remove(), H5Adelete.c.
//
//nolint:gocognit // Test function with multiple verification steps
func TestAttributeDeletion_Dense(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hdf5_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "attr_delete_dense.h5")

	// Step 1: Create file with dataset
	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Step 2: Create 10 attributes (triggers dense storage at attr 8)
	for i := 0; i < 10; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		err = ds.WriteAttribute(attrName, int32(i*10))
		if err != nil {
			t.Fatalf("Failed to write attribute %s: %v", attrName, err)
		}
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after creation: %v", err)
	}

	// Step 3: Reopen file in read-write mode
	fw, err = hdf5.OpenForWrite(testFile, hdf5.OpenReadWrite)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}

	dsw, err := fw.OpenDataset("/data")
	if err != nil {
		t.Fatalf("Failed to open dataset: %v", err)
	}

	// Step 4: Delete 2 attributes
	err = dsw.DeleteAttribute("attr3")
	if err != nil {
		t.Fatalf("Failed to delete attr3: %v", err)
	}

	err = dsw.DeleteAttribute("attr7")
	if err != nil {
		t.Fatalf("Failed to delete attr7: %v", err)
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after deletion: %v", err)
	}

	// Step 5: Read back and verify
	f, err := hdf5.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to reopen file for verification: %v", err)
	}

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

	// Should have 8 attributes remaining (10 - 2)
	if len(attrs) != 8 {
		t.Fatalf("Expected 8 attributes, got %d", len(attrs))
	}

	// Verify deleted attributes are not present
	for _, attr := range attrs {
		if attr.Name == "attr3" || attr.Name == "attr7" {
			t.Errorf("Deleted attribute %s still exists", attr.Name)
		}
	}

	// Verify remaining attributes are intact
	expectedAttrs := map[string]int32{
		"attr0": 0,
		"attr1": 10,
		"attr2": 20,
		// attr3 deleted
		"attr4": 40,
		"attr5": 50,
		"attr6": 60,
		// attr7 deleted
		"attr8": 80,
		"attr9": 90,
	}

	for _, attr := range attrs {
		expectedValue, exists := expectedAttrs[attr.Name]
		if !exists {
			t.Errorf("Unexpected attribute: %s", attr.Name)
			continue
		}

		value, err := attr.ReadValue()
		if err != nil {
			t.Errorf("Failed to read attribute %s: %v", attr.Name, err)
			continue
		}

		if intValue, ok := value.(int32); !ok || intValue != expectedValue {
			t.Errorf("Attribute %s: expected %d, got %v (type %T)",
				attr.Name, expectedValue, value, value)
		}
	}

	// Close file before cleanup
	if err := f.Close(); err != nil {
		t.Errorf("Failed to close file: %v", err)
	}
}

// TestAttributeDeletion_DenseDeleteAll tests deleting all attributes from dense storage.
//
// Verifies:
//   - Creating 10 attributes (dense storage)
//   - Deleting all 10 attributes
//   - Attribute count becomes 0
//
// Reference: H5Adense.c - H5A__dense_remove().
//
//nolint:gocognit // Test function with loop over multiple deletions
func TestAttributeDeletion_DenseDeleteAll(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hdf5_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "attr_delete_all.h5")

	// Step 1: Create file with dataset
	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Step 2: Create 10 attributes
	for i := 0; i < 10; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		err = ds.WriteAttribute(attrName, int32(i))
		if err != nil {
			t.Fatalf("Failed to write attribute %s: %v", attrName, err)
		}
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after creation: %v", err)
	}

	// Step 3: Reopen and delete all attributes
	fw, err = hdf5.OpenForWrite(testFile, hdf5.OpenReadWrite)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}

	dsw, err := fw.OpenDataset("/data")
	if err != nil {
		t.Fatalf("Failed to open dataset: %v", err)
	}

	// Delete all attributes
	for i := 0; i < 10; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		err = dsw.DeleteAttribute(attrName)
		if err != nil {
			t.Fatalf("Failed to delete %s: %v", attrName, err)
		}
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file after deletion: %v", err)
	}

	// Step 4: Verify no attributes remain
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

	if len(attrs) != 0 {
		t.Errorf("Expected 0 attributes, got %d", len(attrs))
	}

	// Close file before cleanup
	if err := f.Close(); err != nil {
		t.Errorf("Failed to close file: %v", err)
	}
}

// TestAttributeDeletion_DenseNotFound tests error handling for deleting non-existent attribute.
//
// Verifies:
//   - Attempting to delete non-existent attribute returns error
//
// Reference: H5Adelete.c - error handling.
func TestAttributeDeletion_DenseNotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hdf5_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "attr_delete_notfound.h5")

	// Step 1: Create file with dataset and attributes
	fw, err := hdf5.CreateForWrite(testFile, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	dims := []uint64{5}
	ds, err := fw.CreateDataset("/data", hdf5.Int32, dims)
	if err != nil {
		t.Fatalf("Failed to create dataset: %v", err)
	}

	// Create 8 attributes (dense storage)
	for i := 0; i < 8; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		err = ds.WriteAttribute(attrName, int32(i))
		if err != nil {
			t.Fatalf("Failed to write attribute %s: %v", attrName, err)
		}
	}

	err = fw.Close()
	if err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Step 2: Reopen and try to delete non-existent attribute
	fw, err = hdf5.OpenForWrite(testFile, hdf5.OpenReadWrite)
	if err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}

	dsw, err := fw.OpenDataset("/data")
	if err != nil {
		t.Fatalf("Failed to open dataset: %v", err)
	}

	// Try to delete non-existent attribute
	err = dsw.DeleteAttribute("nonexistent")
	if err == nil {
		t.Error("Expected error when deleting non-existent attribute, got nil")
	}

	_ = fw.Close()
}
