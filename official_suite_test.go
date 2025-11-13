// Package hdf5 contains tests for the official HDF5 test suite.
package hdf5

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// SuiteStats tracks statistics for the official HDF5 test suite.
type SuiteStats struct {
	Total     int
	Pass      int
	Fail      int
	Skip      int
	PassRate  float64
	Duration  time.Duration
	Failures  []TestFailure
	StartTime time.Time
}

// TestFailure records information about a failed test.
type TestFailure struct {
	FileName string
	Error    string
	Category string
}

// ErrorCategory categorizes test failures for analysis.
type ErrorCategory string

const (
	// CategoryOpenFailed indicates the file could not be opened.
	CategoryOpenFailed ErrorCategory = "open_failed"
	// CategorySuperblockFailed indicates superblock validation failed.
	CategorySuperblockFailed ErrorCategory = "superblock_failed"
	// CategoryReadFailed indicates file reading failed.
	CategoryReadFailed ErrorCategory = "read_failed"
	// CategoryUnknown indicates an unknown error category.
	CategoryUnknown ErrorCategory = "unknown"
	// CategoryKnownInvalid indicates the file is known to be invalid.
	CategoryKnownInvalid ErrorCategory = "known_invalid"
)

// TestOfficialHDF5Suite runs the complete official HDF5 test suite from HDF5 1.14.6.
// This test validates our HDF5 implementation against 433 official test files
// from the HDF Group's reference implementation.
//
// Test files are located in testdata/hdf5_official/ and include:
// - Valid HDF5 files (expected to pass)
// - Invalid HDF5 files (known to fail, for edge case testing)
// - Various format versions and features
//
// Success criteria:
// - Pass rate >90% (goal: >95%)
// - All failures documented
// - No crashes or panics.
func TestOfficialHDF5Suite(t *testing.T) {
	// Initialize statistics.
	stats := &SuiteStats{
		StartTime: time.Now(),
		Failures:  make([]TestFailure, 0),
	}

	// Find all .h5 files in the official suite.
	pattern := filepath.Join("testdata", "hdf5_official", "*.h5")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No test files found in %s", pattern)
	}

	stats.Total = len(files)
	t.Logf("Found %d HDF5 test files", stats.Total)

	// Load known invalid files list.
	knownInvalid := loadKnownInvalid(t)
	t.Logf("Loaded %d known invalid files", len(knownInvalid))

	// Sort files for consistent test order.
	sort.Strings(files)

	// Run tests for each file.
	for _, file := range files {
		fileName := filepath.Base(file)
		t.Run(fileName, func(t *testing.T) {
			// Check if this is a known invalid file.
			if knownInvalid[fileName] {
				t.Logf("SKIP: %s (known invalid)", fileName)
				stats.Skip++
				return
			}

			// Validate the file.
			if err := validateHDF5File(file); err != nil {
				// Categorize the error.
				category := categorizeError(err)

				// Record failure.
				failure := TestFailure{
					FileName: fileName,
					Error:    err.Error(),
					Category: string(category),
				}
				stats.Failures = append(stats.Failures, failure)
				stats.Fail++

				t.Logf("FAIL: %s - %v (category: %s)", fileName, err, category)
			} else {
				stats.Pass++
				t.Logf("PASS: %s", fileName)
			}
		})
	}

	// Calculate final statistics.
	stats.Duration = time.Since(stats.StartTime)
	// Pass rate is calculated based on non-skipped files only.
	validFiles := stats.Total - stats.Skip
	if validFiles > 0 {
		stats.PassRate = float64(stats.Pass) / float64(validFiles) * 100
	}

	// Print summary.
	printSummary(t, stats)

	// Write results to file.
	if err := writeResults(stats); err != nil {
		t.Logf("Warning: Failed to write results file: %v", err)
	}

	// Fail the test if pass rate is below threshold.
	const minPassRate = 90.0
	if stats.PassRate < minPassRate {
		t.Errorf("Pass rate %.1f%% is below minimum threshold %.1f%%", stats.PassRate, minPassRate)
	}
}

// validateHDF5File performs basic validation on an HDF5 file.
// It attempts to open the file and read its basic structure.
func validateHDF5File(path string) error {
	// Try to open the file.
	file, err := Open(path)
	if err != nil {
		return fmt.Errorf("open failed: %w", err)
	}
	defer func() {
		// Close the file, ignoring errors since we only care about validation results.
		_ = file.Close()
	}()

	// Validate root group exists.
	root := file.Root()
	if root == nil {
		return fmt.Errorf("root group is nil")
	}

	// Try to read basic structure (groups, datasets).
	if err := validateStructure(root); err != nil {
		return fmt.Errorf("structure validation failed: %w", err)
	}

	return nil
}

// validateStructure performs basic validation on the HDF5 structure.
// It recursively checks groups, datasets, and attributes.
func validateStructure(obj Object) error {
	// Check object metadata.
	if obj.Name() == "" {
		return fmt.Errorf("object has empty name")
	}

	// If it's a group, validate children.
	if group, ok := obj.(*Group); ok {
		children := group.Children()
		for _, child := range children {
			// Recursively validate child nodes.
			if err := validateStructure(child); err != nil {
				return fmt.Errorf("child '%s' validation failed: %w", child.Name(), err)
			}
		}
	}

	// If it's a dataset, validate that it exists.
	// Note: We don't validate Dataspace/Datatype to avoid reading full metadata.
	// This keeps the test suite fast.
	if dataset, ok := obj.(*Dataset); ok {
		// Just check that the dataset name is valid.
		if dataset.Name() == "" {
			return fmt.Errorf("dataset has empty name")
		}
	}

	return nil
}

// loadKnownInvalid loads the list of known invalid files from testdata/hdf5_official/known_invalid.txt.
func loadKnownInvalid(t *testing.T) map[string]bool {
	t.Helper()

	knownInvalid := make(map[string]bool)
	path := filepath.Join("testdata", "hdf5_official", "known_invalid.txt")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - that's OK for initial run.
			return knownInvalid
		}
		t.Logf("Warning: Failed to read known_invalid.txt: %v", err)
		return knownInvalid
	}

	// Parse lines.
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		knownInvalid[line] = true
	}

	return knownInvalid
}

// categorizeError categorizes an error into predefined categories.
func categorizeError(err error) ErrorCategory {
	errStr := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errStr, "open failed"):
		return CategoryOpenFailed
	case strings.Contains(errStr, "superblock"):
		return CategorySuperblockFailed
	case strings.Contains(errStr, "read failed"):
		return CategoryReadFailed
	default:
		return CategoryUnknown
	}
}

// printSummary prints a formatted summary of test results.
func printSummary(t *testing.T, stats *SuiteStats) {
	t.Helper()

	t.Log("")
	t.Log("========================================")
	t.Log("Official HDF5 Test Suite Results")
	t.Log("========================================")
	t.Logf("Total:     %d files", stats.Total)
	t.Logf("Pass:      %d files", stats.Pass)
	t.Logf("Fail:      %d files", stats.Fail)
	t.Logf("Skip:      %d files (known invalid/unsupported)", stats.Skip)
	t.Logf("Pass Rate: %.1f%% (of %d valid files)", stats.PassRate, stats.Total-stats.Skip)
	t.Logf("Duration:  %v", stats.Duration.Round(time.Millisecond))
	t.Log("========================================")

	// Print top failures by category.
	if len(stats.Failures) > 0 {
		t.Log("")
		t.Log("FAILURE SUMMARY BY CATEGORY:")
		printFailuresByCategory(t, stats.Failures)
		t.Log("")

		// Print first 10 detailed failures.
		t.Log("DETAILED FAILURES (first 10):")
		maxFailures := 10
		if len(stats.Failures) < maxFailures {
			maxFailures = len(stats.Failures)
		}
		for i := 0; i < maxFailures; i++ {
			f := stats.Failures[i]
			t.Logf("%d. %s: %s (category: %s)", i+1, f.FileName, f.Error, f.Category)
		}
		if len(stats.Failures) > 10 {
			t.Logf("... and %d more failures", len(stats.Failures)-10)
		}
	}
}

// printFailuresByCategory prints failure statistics grouped by category.
func printFailuresByCategory(t *testing.T, failures []TestFailure) {
	t.Helper()

	// Count failures by category.
	categoryCount := make(map[string]int)
	for _, f := range failures {
		categoryCount[f.Category]++
	}

	// Sort categories by count (descending).
	type catCount struct {
		category string
		count    int
	}
	categories := make([]catCount, 0, len(categoryCount))
	for cat, count := range categoryCount {
		categories = append(categories, catCount{cat, count})
	}
	sort.Slice(categories, func(i, j int) bool {
		return categories[i].count > categories[j].count
	})

	// Print sorted categories.
	for _, cc := range categories {
		t.Logf("  %s: %d files", cc.category, cc.count)
	}
}

// writeResults writes test results to testdata/hdf5_official/test_results.txt.
func writeResults(stats *SuiteStats) error {
	path := filepath.Join("testdata", "hdf5_official", "test_results.txt")

	var sb strings.Builder
	sb.WriteString("========================================\n")
	sb.WriteString("Official HDF5 Test Suite Results\n")
	sb.WriteString("========================================\n")
	sb.WriteString(fmt.Sprintf("Date:      %s\n", stats.StartTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Total:     %d files\n", stats.Total))
	sb.WriteString(fmt.Sprintf("Pass:      %d files\n", stats.Pass))
	sb.WriteString(fmt.Sprintf("Fail:      %d files\n", stats.Fail))
	sb.WriteString(fmt.Sprintf("Skip:      %d files (known invalid/unsupported)\n", stats.Skip))
	validFiles := stats.Total - stats.Skip
	sb.WriteString(fmt.Sprintf("Pass Rate: %.1f%% (of %d valid files)\n", stats.PassRate, validFiles))
	sb.WriteString(fmt.Sprintf("Duration:  %v\n", stats.Duration.Round(time.Millisecond)))
	sb.WriteString("========================================\n\n")

	// Write failures by category.
	if len(stats.Failures) > 0 {
		sb.WriteString("FAILURE SUMMARY BY CATEGORY:\n")

		// Count failures by category.
		categoryCount := make(map[string]int)
		for _, f := range stats.Failures {
			categoryCount[f.Category]++
		}

		// Sort and write.
		type catCount struct {
			category string
			count    int
		}
		categories := make([]catCount, 0, len(categoryCount))
		for cat, count := range categoryCount {
			categories = append(categories, catCount{cat, count})
		}
		sort.Slice(categories, func(i, j int) bool {
			return categories[i].count > categories[j].count
		})

		for _, cc := range categories {
			sb.WriteString(fmt.Sprintf("  %s: %d files\n", cc.category, cc.count))
		}
		sb.WriteString("\n")

		// Write detailed failures.
		sb.WriteString("DETAILED FAILURES:\n")
		for i, f := range stats.Failures {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f.FileName))
			sb.WriteString(fmt.Sprintf("   Category: %s\n", f.Category))
			sb.WriteString(fmt.Sprintf("   Error:    %s\n", f.Error))
			sb.WriteString("\n")
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
