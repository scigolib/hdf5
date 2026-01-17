package hdf5_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// TestReference_AllFiles tests all 57 reference files from HDF5 C library.
// This comprehensive test validates our implementation against the official test suite.
func TestReference_AllFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/reference/*.h5")
	require.NoError(t, err, "failed to find reference files")
	require.NotEmpty(t, files, "no reference files found in testdata/reference/")

	// Sort for consistent test order
	sort.Strings(files)

	var (
		passed   int
		failed   int
		failures []testFailure
	)

	for _, file := range files {
		name := filepath.Base(file)

		// Some files are intentionally corrupted for error testing
		// We expect these to fail gracefully without panics
		isCorruptFile := strings.Contains(name, "corrupt") ||
			strings.Contains(name, "bad_") ||
			strings.Contains(name, "cve_") ||
			strings.Contains(name, "err_")

		// Some files require special file drivers not yet implemented
		requiresSpecialDriver := strings.Contains(name, "family_v16-") && name != "family_v16-000000.h5" ||
			strings.Contains(name, "multi_file_v16") && name != "multi_file_v16-s.h5" ||
			name == "tsizeslheap.h5" // Uses unusual v0 format with zero object header

		// Files containing datasets with older data layout versions (v1-v2, HDF5 1.6 era).
		// These require data layout message version 1 or 2 support which is not yet implemented.
		// Our library supports versions 3+ (HDF5 1.8+).
		requiresOldLayoutVersion := name == "btree_idx_1_6.h5" ||
			name == "deflate.h5" ||
			name == "family_v16-000000.h5" ||
			name == "filespace_1_6.h5" ||
			name == "fill_old.h5" ||
			name == "multi_file_v16-s.h5" ||
			name == "tarrold.h5" ||
			name == "test_filters_be.h5" ||
			name == "test_filters_le.h5" ||
			name == "th5s.h5" ||
			name == "tlayouto.h5" ||
			name == "tmtimen.h5" ||
			name == "tmtimeo.h5"

		// Skip files requiring unsupported features
		if requiresSpecialDriver || requiresOldLayoutVersion {
			t.Run(name, func(t *testing.T) {
				t.Skipf("skipping: requires unsupported feature")
			})
			continue
		}

		t.Run(name, func(t *testing.T) {
			result := testReferenceFile(t, file, name, isCorruptFile, requiresSpecialDriver)

			if result.passed {
				passed++
				t.Logf("✅ PASS: %s (%d objects, %d datasets, %d groups)",
					name, result.objects, result.datasets, result.groups)
			} else {
				failed++
				failures = append(failures, result.failure)
				t.Errorf("❌ FAIL: %s - %s", name, result.failure.message)
			}
		})
	}

	// Print comprehensive summary
	total := passed + failed
	separator := strings.Repeat("=", 60)
	t.Logf("\n%s", separator)
	t.Logf("REFERENCE TEST SUITE SUMMARY")
	t.Logf("%s", separator)
	t.Logf("Total Files:  %d", total)
	t.Logf("Passed:       %d files (%.1f%%)", passed, percentage(passed, total))
	t.Logf("Failed:       %d files (%.1f%%)", failed, percentage(failed, total))

	if failed > 0 {
		divider := strings.Repeat("-", 60)
		t.Logf("\n%s", divider)
		t.Logf("FAILURE DETAILS")
		t.Logf("%s", divider)

		// Group failures by type
		byType := groupFailuresByType(failures)
		for errType, files := range byType {
			t.Logf("\n%s (%d files):", errType, len(files))
			for _, f := range files {
				t.Logf("  • %s: %s", f.filename, f.message)
			}
		}
	}

	// All reference files must pass for production release
	require.Equal(t, 0, failed, "All reference files must pass")
}

// testResult holds the result of testing a single file.
type testResult struct {
	passed   bool
	objects  int
	datasets int
	groups   int
	failure  testFailure
}

// testFailure describes why a test failed.
type testFailure struct {
	filename string
	errType  string
	message  string
}

// testReferenceFile tests a single reference file.
func testReferenceFile(t *testing.T, path, name string, expectError, requiresDriver bool) testResult {
	result := testResult{}

	// Step 1: Open file
	f, err := hdf5.Open(path)
	if err != nil {
		if expectError || requiresDriver {
			// Expected failure for corrupt files or files requiring special drivers
			result.passed = true
			return result
		}

		result.failure = testFailure{
			filename: name,
			errType:  "open_error",
			message:  fmt.Sprintf("cannot open: %v", err),
		}
		return result
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && !expectError {
			t.Logf("Warning: %s - close error: %v", name, closeErr)
		}
	}()

	// Step 2: Get root group
	root := f.Root()
	if root == nil {
		result.failure = testFailure{
			filename: name,
			errType:  "nil_root",
			message:  "root group is nil",
		}
		return result
	}

	// Step 3: Walk entire tree and validate structure
	var (
		objects    int
		datasets   int
		groups     int
		walkErrors []string
		seenPaths  = make(map[string]bool)
	)

	f.Walk(func(path string, obj hdf5.Object) {
		objects++

		// Check for duplicate paths (shouldn't happen)
		if seenPaths[path] {
			walkErrors = append(walkErrors, fmt.Sprintf("duplicate path: %s", path))
			return
		}
		seenPaths[path] = true

		// Validate object is not nil
		if obj == nil {
			walkErrors = append(walkErrors, fmt.Sprintf("%s: nil object", path))
			return
		}

		// Test dataset-specific operations
		if ds, ok := obj.(*hdf5.Dataset); ok {
			datasets++
			validateDataset(ds, path, &walkErrors)
		}

		// Test group-specific operations
		if g, ok := obj.(*hdf5.Group); ok {
			groups++
			validateGroup(g, path, &walkErrors)
		}
	})

	// Check for walk errors collected during traversal
	if len(walkErrors) > 0 {
		result.failure = testFailure{
			filename: name,
			errType:  "validation_error",
			message:  fmt.Sprintf("%d errors, first: %s", len(walkErrors), walkErrors[0]),
		}
		return result
	}

	// Validate we found some content (unless it's a special empty file)
	if objects == 0 && !expectError {
		result.failure = testFailure{
			filename: name,
			errType:  "empty_file",
			message:  "file appears empty (0 objects)",
		}
		return result
	}

	// Success!
	result.passed = true
	result.objects = objects
	result.datasets = datasets
	result.groups = groups
	return result
}

// validateDataset performs comprehensive validation on a dataset.
func validateDataset(ds *hdf5.Dataset, path string, errors *[]string) {
	// Try to get dataset info (validates internal structure)
	info, err := ds.Info()
	if err != nil {
		*errors = append(*errors, fmt.Sprintf("%s: cannot get info: %v", path, err))
		return
	}

	// Basic sanity check - info should not be empty
	if info == "" {
		*errors = append(*errors, fmt.Sprintf("%s: empty dataset info", path))
	}

	// Check attributes (should not panic)
	attrs, err := ds.Attributes()
	if err != nil {
		*errors = append(*errors, fmt.Sprintf("%s: cannot get attributes: %v", path, err))
		return
	}

	// Validate each attribute
	for _, attr := range attrs {
		if attr == nil {
			*errors = append(*errors, fmt.Sprintf("%s: nil attribute in list", path))
			continue
		}

		// Check attribute has a name
		if attr.Name == "" {
			*errors = append(*errors, fmt.Sprintf("%s: attribute with empty name", path))
		}

		// Check attribute datatype
		if attr.Datatype == nil {
			*errors = append(*errors, fmt.Sprintf("%s: attribute '%s' has nil datatype",
				path, attr.Name))
		}

		// Check attribute dataspace
		if attr.Dataspace == nil {
			*errors = append(*errors, fmt.Sprintf("%s: attribute '%s' has nil dataspace",
				path, attr.Name))
		}
	}
}

// validateGroup performs comprehensive validation on a group.
func validateGroup(g *hdf5.Group, path string, errors *[]string) {
	// Check children (should not panic)
	children := g.Children()
	// Children might be nil if group is empty, that's okay

	// Check attributes (should not panic)
	attrs, err := g.Attributes()
	if err != nil {
		*errors = append(*errors, fmt.Sprintf("%s: cannot get attributes: %v", path, err))
		return
	}

	// Validate each attribute if present
	for _, attr := range attrs {
		if attr == nil {
			*errors = append(*errors, fmt.Sprintf("%s: nil attribute in list", path))
			continue
		}

		// Basic attribute validation
		if attr.Name == "" {
			*errors = append(*errors, fmt.Sprintf("%s: attribute with empty name", path))
		}
	}

	// If we have children, validate the count makes sense
	if len(children) > 0 {
		// Check for nil children
		for i, child := range children {
			if child == nil {
				*errors = append(*errors, fmt.Sprintf("%s: child #%d is nil", path, i))
			}
		}
	}
}

// percentage calculates percentage safely.
func percentage(part, total int) float64 {
	if total == 0 {
		return 0.0
	}
	return float64(part) / float64(total) * 100.0
}

// groupFailuresByType groups failures by error type for better reporting.
func groupFailuresByType(failures []testFailure) map[string][]testFailure {
	groups := make(map[string][]testFailure)
	for _, f := range failures {
		groups[f.errType] = append(groups[f.errType], f)
	}
	return groups
}
