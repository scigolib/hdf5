package hdf5

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVLenStringAttribute_RoundTrip writes []string as a group attribute,
// closes the file, reopens it, and verifies the strings are read back correctly.
func TestVLenStringAttribute_RoundTrip(t *testing.T) {
	testFile := "test_vlen_string_attr_roundtrip.h5"
	defer func() { _ = os.Remove(testFile) }()

	topics := []string{"camera_front", "camera_back", "lidar"}

	// Write.
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/sensors")
	require.NoError(t, err)

	err = group.WriteAttribute("topics", topics)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back and verify.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	var foundGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "sensors" {
			foundGroup = g
			break
		}
	}
	require.NotNil(t, foundGroup, "group 'sensors' not found")

	attrs, err := foundGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "topics", attrs[0].Name)

	val, err := attrs[0].ReadValue()
	require.NoError(t, err)

	got, ok := val.([]string)
	require.True(t, ok, "expected []string, got %T", val)
	require.Equal(t, topics, got)
}

// TestVLenStringAttribute_SingleElement verifies a single-element []string attribute.
// Note: HDF5 reader returns scalar string (not []string) when dataspace has 1 element.
func TestVLenStringAttribute_SingleElement(t *testing.T) {
	testFile := "test_vlen_string_attr_single.h5"
	defer func() { _ = os.Remove(testFile) }()

	input := []string{"only_one"}

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/grp")
	require.NoError(t, err)

	err = group.WriteAttribute("single", input)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var foundGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "grp" {
			foundGroup = g
			break
		}
	}
	require.NotNil(t, foundGroup)

	attrs, err := foundGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 1)

	val, err := attrs[0].ReadValue()
	require.NoError(t, err)

	// HDF5 reader returns scalar string for 1-element vlen string arrays
	// (isScalar=true when totalElements==1).
	got, ok := val.(string)
	require.True(t, ok, "expected string, got %T", val)
	require.Equal(t, "only_one", got)
}

// TestVLenStringAttribute_OnDataset verifies []string attribute on a dataset (not just groups).
func TestVLenStringAttribute_OnDataset(t *testing.T) {
	testFile := "test_vlen_string_attr_dataset.h5"
	defer func() { _ = os.Remove(testFile) }()

	labels := []string{"x_axis", "y_axis", "z_axis"}

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.NoError(t, err)

	err = ds.WriteAttribute("labels", labels)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var foundDS *Dataset
	f.Walk(func(path string, obj Object) {
		if d, ok := obj.(*Dataset); ok && path == "/data" {
			foundDS = d
		}
	})
	require.NotNil(t, foundDS, "dataset '/data' not found")

	attrs, err := foundDS.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "labels", attrs[0].Name)

	val, err := attrs[0].ReadValue()
	require.NoError(t, err)

	got, ok := val.([]string)
	require.True(t, ok, "expected []string, got %T", val)
	require.Equal(t, labels, got)
}

// TestVLenStringAttribute_Empty verifies that empty []string is rejected with a clear error.
func TestVLenStringAttribute_Empty(t *testing.T) {
	testFile := "test_vlen_string_attr_empty.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	group, err := fw.CreateGroup("/grp")
	require.NoError(t, err)

	err = group.WriteAttribute("empty", []string{})
	require.Error(t, err, "should reject empty []string")
	require.Contains(t, err.Error(), "empty")
}

// TestVLenStringAttribute_WithEmptyStrings verifies that empty strings within the array work.
func TestVLenStringAttribute_WithEmptyStrings(t *testing.T) {
	testFile := "test_vlen_string_attr_empties.h5"
	defer func() { _ = os.Remove(testFile) }()

	input := []string{"hello", "", "world"}

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/grp")
	require.NoError(t, err)

	err = group.WriteAttribute("mixed", input)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var foundGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "grp" {
			foundGroup = g
			break
		}
	}
	require.NotNil(t, foundGroup)

	attrs, err := foundGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 1)

	val, err := attrs[0].ReadValue()
	require.NoError(t, err)

	got, ok := val.([]string)
	require.True(t, ok, "expected []string, got %T", val)
	require.Equal(t, input, got)
}

// TestVLenStringAttribute_MixedWithOtherAttrs verifies []string alongside scalar attributes.
func TestVLenStringAttribute_MixedWithOtherAttrs(t *testing.T) {
	testFile := "test_vlen_string_attr_mixed.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/experiment")
	require.NoError(t, err)

	// Write scalar attributes alongside vlen string array.
	err = group.WriteAttribute("version", int32(2))
	require.NoError(t, err)
	err = group.WriteAttribute("channels", []string{"RGB", "Depth", "IR"})
	require.NoError(t, err)
	err = group.WriteAttribute("rate_hz", float64(30.0))
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back and verify all attributes.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var foundGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "experiment" {
			foundGroup = g
			break
		}
	}
	require.NotNil(t, foundGroup)

	attrs, err := foundGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 3)

	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		val, readErr := attr.ReadValue()
		require.NoError(t, readErr, "failed to read attribute %q", attr.Name)
		attrMap[attr.Name] = val
	}

	// Verify vlen strings.
	channels, ok := attrMap["channels"].([]string)
	require.True(t, ok, "expected []string for 'channels', got %T", attrMap["channels"])
	require.Equal(t, []string{"RGB", "Depth", "IR"}, channels)
}

// TestVLenStringAttribute_H5dump verifies that h5dump can read the file correctly.
// This test is skipped if h5dump is not available.
func TestVLenStringAttribute_H5dump(t *testing.T) {
	h5dumpPath := `C:\Program Files\HDF_Group\HDF5\1.14.6\bin\h5dump.exe`
	if _, err := os.Stat(h5dumpPath); os.IsNotExist(err) {
		t.Skip("h5dump not available at", h5dumpPath)
	}

	testFile := "test_vlen_string_attr_h5dump.h5"
	defer func() { _ = os.Remove(testFile) }()

	topics := []string{"camera_front", "camera_back", "lidar"}

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/sensors")
	require.NoError(t, err)
	err = group.WriteAttribute("topics", topics)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Run h5dump.
	cmd := exec.Command(h5dumpPath, testFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "h5dump failed: %s", string(output))

	outputStr := string(output)

	// Verify the attribute appears in h5dump output.
	require.True(t, strings.Contains(outputStr, "topics"),
		"h5dump output should contain attribute name 'topics':\n%s", outputStr)

	for _, topic := range topics {
		require.True(t, strings.Contains(outputStr, topic),
			"h5dump output should contain string %q:\n%s", topic, outputStr)
	}
}
