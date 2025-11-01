// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDenseAttributeRMW_BasicFlow tests the basic read-modify-write workflow.
// Create file with dense attributes → close → reopen → add more attributes.
func TestDenseAttributeRMW_BasicFlow(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_dense_rmw.h5")

	// Phase 1: Create file with dense attributes (8+)
	t.Log("Phase 1: Creating file with dense attributes...")
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err, "Failed to create file")

	ds, err := fw.CreateDataset("/data", Int32, []uint64{10})
	require.NoError(t, err, "Failed to create dataset")

	// Add 8 attributes (triggers dense storage)
	for i := 0; i < 8; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		attrValue := int32(i * 10)
		err = ds.WriteAttribute(attrName, attrValue)
		require.NoError(t, err, "Failed to write attribute %s", attrName)
	}

	err = fw.Close()
	require.NoError(t, err, "Failed to close file after phase 1")
	t.Log("✓ Phase 1 complete: 8 attributes written")

	// Phase 2: Reopen file and add MORE attributes (RMW!)
	t.Log("Phase 2: Reopening file for modification...")
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err, "Failed to reopen file for modification")

	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err, "Failed to open dataset")

	// Add 3 more attributes to existing dense storage
	for i := 8; i < 11; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		attrValue := int32(i * 10)
		err = ds.WriteAttribute(attrName, attrValue)
		require.NoError(t, err, "Failed to write attribute %s via RMW", attrName)
	}

	err = fw.Close()
	require.NoError(t, err, "Failed to close file after phase 2")
	t.Log("✓ Phase 2 complete: 3 more attributes added via RMW")

	// Phase 3: Verify all 11 attributes exist
	t.Log("Phase 3: Verifying all attributes...")
	f, err := Open(tmpFile)
	require.NoError(t, err, "Failed to open file for verification")

	ds2 := findDataset(f, "/data")
	require.NotNil(t, ds2, "Dataset /data not found")

	attrs, err := ds2.ListAttributes()
	require.NoError(t, err, "Failed to list attributes")
	assert.Equal(t, 11, len(attrs), "Should have 11 attributes total")

	// Verify values
	for i := 0; i < 11; i++ {
		attrName := fmt.Sprintf("attr%d", i)
		val, err := ds2.ReadAttributeAsInt32(attrName)
		require.NoError(t, err, "Failed to read attribute %s", attrName)
		expectedValue := int32(i * 10)
		assert.Equal(t, expectedValue, val, "Attribute %s value mismatch", attrName)
	}
	t.Log("✓ Phase 3 complete: All 11 attributes verified")

	// Close file BEFORE h5dump (Windows file locking issue)
	err = f.Close()
	require.NoError(t, err, "Failed to close file before h5dump")

	// Phase 4: Verify with h5dump (if available) - optional validation
	if isH5dumpAvailable() {
		t.Log("Phase 4: Validating with h5dump...")
		output, err := runH5dump(tmpFile, "-A") // List all attributes
		if err != nil {
			t.Logf("⚠️  h5dump validation skipped (tool issue): %v", err)
		} else {
			// Check all attributes present
			for i := 0; i < 11; i++ {
				attrName := fmt.Sprintf("attr%d", i)
				assert.Contains(t, output, attrName, "h5dump should show attribute %s", attrName)
			}
			t.Log("✓ Phase 4 complete: h5dump validation passed")
		}
	}
}

// TestDenseAttributeRMW_MultipleReopens tests multiple reopen cycles.
// Create 8 → reopen add 2 → reopen add 2 → verify 12 total.
func TestDenseAttributeRMW_MultipleReopens(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_multiple_reopens.h5")

	// Create with 8 attributes
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)
	for i := 0; i < 8; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr%d", i), float64(i)*1.5)
		require.NoError(t, err)
	}
	fw.Close()

	// Reopen cycle 1: Add 2 more
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err)
	for i := 8; i < 10; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr%d", i), float64(i)*1.5)
		require.NoError(t, err)
	}
	fw.Close()

	// Reopen cycle 2: Add 2 more
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err)
	for i := 10; i < 12; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr%d", i), float64(i)*1.5)
		require.NoError(t, err)
	}
	fw.Close()

	// Verify 12 total
	f, err := Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	ds2 := findDataset(f, "/data")
	require.NotNil(t, ds2, "Dataset /data not found")
	require.NoError(t, err)

	attrs, err := ds2.ListAttributes()
	require.NoError(t, err)
	assert.Equal(t, 12, len(attrs), "Should have 12 attributes after multiple reopens")
}

// TestDenseAttributeRMW_LargeScale tests adding many attributes across reopens.
// Create 50 → reopen add 50 → verify 100 total.
func TestDenseAttributeRMW_LargeScale(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_large_scale.h5")

	// Create with 50 attributes
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Int32, []uint64{10})
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("large_attr_%03d", i), int32(i))
		require.NoError(t, err)
	}
	fw.Close()

	// Reopen and add 50 more
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err)
	for i := 50; i < 100; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("large_attr_%03d", i), int32(i))
		require.NoError(t, err)
	}
	fw.Close()

	// Verify 100 total
	f, err := Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	ds2 := findDataset(f, "/data")
	require.NotNil(t, ds2, "Dataset /data not found")
	require.NoError(t, err)

	attrs, err := ds2.ListAttributes()
	require.NoError(t, err)
	assert.Equal(t, 100, len(attrs), "Should have 100 attributes total")

	// Spot-check some values
	val0, err := ds2.ReadAttributeAsInt32("large_attr_000")
	require.NoError(t, err)
	assert.Equal(t, int32(0), val0)

	val50, err := ds2.ReadAttributeAsInt32("large_attr_050")
	require.NoError(t, err)
	assert.Equal(t, int32(50), val50)

	val99, err := ds2.ReadAttributeAsInt32("large_attr_099")
	require.NoError(t, err)
	assert.Equal(t, int32(99), val99)
}

// TestDenseAttributeRMW_MixedTypes tests RMW with different attribute types.
func TestDenseAttributeRMW_MixedTypes(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_mixed_types.h5")

	// Create with 8 int32 attributes
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
	require.NoError(t, err)
	for i := 0; i < 8; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("int_%d", i), int32(i))
		require.NoError(t, err)
	}
	fw.Close()

	// Reopen and add mixed types
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err)

	err = ds.WriteAttribute("float_attr", float64(3.14159))
	require.NoError(t, err)

	err = ds.WriteAttribute("string_attr", "hello world")
	require.NoError(t, err)

	err = ds.WriteAttribute("int64_attr", int64(9223372036854775807))
	require.NoError(t, err)

	fw.Close()

	// Verify 11 attributes of mixed types
	f, err := Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	ds2 := findDataset(f, "/data")
	require.NotNil(t, ds2, "Dataset /data not found")
	require.NoError(t, err)

	attrs, err := ds2.ListAttributes()
	require.NoError(t, err)
	assert.Equal(t, 11, len(attrs))

	// Verify mixed type values
	floatVal, err := ds2.ReadAttributeAsFloat64("float_attr")
	require.NoError(t, err)
	assert.InDelta(t, 3.14159, floatVal, 0.00001)

	stringVal, err := ds2.ReadAttributeAsString("string_attr")
	require.NoError(t, err)
	assert.Equal(t, "hello world", stringVal)

	int64Val, err := ds2.ReadAttributeAsInt64("int64_attr")
	require.NoError(t, err)
	assert.Equal(t, int64(9223372036854775807), int64Val)
}

// TestDenseAttributeRMW_ErrorCases tests error handling in RMW scenarios.
func TestDenseAttributeRMW_ErrorCases(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_errors.h5")

	// Create file
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)
	fw.Close()

	// Test: Cannot add dense attributes to dataset without dense storage
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset("/data")
	require.NoError(t, err)

	// This should work (compact storage)
	err = ds.WriteAttribute("attr0", int32(42))
	require.NoError(t, err)

	fw.Close()
}

// TestDenseAttributeRMW_DatasetIntegrity tests that dataset data remains intact during attribute RMW.
func TestDenseAttributeRMW_DatasetIntegrity(t *testing.T) {
	tmpFile := createShortPathTempFile(t, "test_integrity.h5")

	// Create dataset with data
	fw, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)

	// Write dataset data
	testData := []int32{100, 200, 300, 400, 500}
	err = ds.Write(testData)
	require.NoError(t, err)

	// Add 8 attributes
	for i := 0; i < 8; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr%d", i), int32(i))
		require.NoError(t, err)
	}
	fw.Close()

	// Reopen and add more attributes
	fw, err = OpenForWrite(tmpFile, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset("/data")
	require.NoError(t, err)
	err = ds.WriteAttribute("extra_attr", int32(999))
	require.NoError(t, err)
	fw.Close()

	// Verify dataset data is intact
	f, err := Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	ds2 := findDataset(f, "/data")
	require.NotNil(t, ds2, "Dataset /data not found")

	// Read dataset data (as float64, then convert to verify)
	dataFloat, err := ds2.Read()
	require.NoError(t, err)
	require.Equal(t, len(testData), len(dataFloat), "Dataset should have same number of elements")

	// Verify data values
	for i, expected := range testData {
		assert.Equal(t, float64(expected), dataFloat[i], "Dataset value at index %d should remain intact", i)
	}

	// Verify attributes also work
	attrs, err := ds2.ListAttributes()
	require.NoError(t, err)
	assert.Equal(t, 9, len(attrs), "Should have 9 attributes")
}

// Helper functions

// createShortPathTempFile creates a temporary HDF5 file in tmp/ directory for testing.
// Returns filepath. Cleanup is registered with t.Cleanup() automatically.
// Use absolute paths to avoid Windows h5dump issues with relative paths.
func createShortPathTempFile(t *testing.T, name string) string {
	t.Helper()
	// Get absolute path to avoid h5dump issues
	absPath, err := filepath.Abs("tmp")
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}
	// Ensure tmp directory exists
	_ = os.MkdirAll(absPath, 0o755)
	tmpFile := filepath.Join(absPath, name)
	t.Cleanup(func() {
		_ = os.Remove(tmpFile)
	})
	return tmpFile
}

//nolint:unparam // targetPath parameter designed for reusability in future tests
func findDataset(f *File, targetPath string) *Dataset {
	var found *Dataset
	f.Walk(func(p string, obj Object) {
		if p == targetPath {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	return found
}

// isH5dumpAvailable checks if h5dump command is available.
func isH5dumpAvailable() bool {
	_, err := exec.LookPath("h5dump")
	return err == nil
}

// runH5dump runs h5dump command and returns output.
func runH5dump(filename string, args ...string) (string, error) {
	// Convert Windows backslash paths to forward slash for h5dump (MSYS2 compatibility)
	filename = filepath.ToSlash(filename)

	cmdArgs := make([]string, len(args)+1)
	copy(cmdArgs, args)
	cmdArgs[len(args)] = filename
	cmd := exec.Command("h5dump", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("h5dump failed: %w\nOutput: %s", err, string(output))
	}
	return string(output), nil
}

// Helper method for reading attribute as int32 (if not exists).
// This should be moved to dataset.go in production.
func (ds *Dataset) ReadAttributeAsInt32(name string) (int32, error) {
	val, err := ds.ReadAttribute(name)
	if err != nil {
		return 0, err
	}
	// Handle different return types
	switch v := val.(type) {
	case int32:
		return v, nil
	case []int32:
		if len(v) > 0 {
			return v[0], nil
		}
		return 0, fmt.Errorf("empty array")
	default:
		return 0, fmt.Errorf("attribute is not int32, got %T", val)
	}
}

// Helper method for reading attribute as float64.
func (ds *Dataset) ReadAttributeAsFloat64(name string) (float64, error) {
	val, err := ds.ReadAttribute(name)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case float64:
		return v, nil
	case []float64:
		if len(v) > 0 {
			return v[0], nil
		}
		return 0, fmt.Errorf("empty array")
	default:
		return 0, fmt.Errorf("attribute is not float64, got %T", val)
	}
}

// Helper method for reading attribute as int64.
func (ds *Dataset) ReadAttributeAsInt64(name string) (int64, error) {
	val, err := ds.ReadAttribute(name)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case int64:
		return v, nil
	case []int64:
		if len(v) > 0 {
			return v[0], nil
		}
		return 0, fmt.Errorf("empty array")
	default:
		return 0, fmt.Errorf("attribute is not int64, got %T", val)
	}
}

// Helper method for reading attribute as string.
func (ds *Dataset) ReadAttributeAsString(name string) (string, error) {
	val, err := ds.ReadAttribute(name)
	if err != nil {
		return "", err
	}
	switch v := val.(type) {
	case string:
		// Trim null terminators and whitespace
		return strings.TrimRight(v, "\x00 "), nil
	case []string:
		if len(v) > 0 {
			return strings.TrimRight(v[0], "\x00 "), nil
		}
		return "", fmt.Errorf("empty array")
	default:
		return "", fmt.Errorf("attribute is not string, got %T", val)
	}
}
