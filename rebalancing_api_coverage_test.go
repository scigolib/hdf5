// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
	"github.com/stretchr/testify/require"
)

// TestDisableLazyRebalancing tests that DisableLazyRebalancing returns no error.
func TestDisableLazyRebalancing(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "disable_lazy.h5")

	fw, err := CreateForWrite(filename, CreateTruncate,
		WithLazyRebalancing(
			LazyThreshold(0.05),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// DisableLazyRebalancing is currently a no-op MVP stub returning nil.
	err = fw.DisableLazyRebalancing()
	require.NoError(t, err)
}

// TestIsLazyRebalancingEnabled tests the lazy rebalancing status check.
func TestIsLazyRebalancingEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "is_lazy.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// MVP stub always returns false.
	require.False(t, fw.IsLazyRebalancingEnabled())
}

// TestForceBatchRebalance tests the force batch rebalance method.
func TestForceBatchRebalance(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "force_batch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// MVP stub returns nil (no-op).
	err = fw.ForceBatchRebalance()
	require.NoError(t, err)
}

// TestGetLazyRebalancingStats tests the lazy rebalancing stats retrieval.
func TestGetLazyRebalancingStats(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "lazy_stats.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	totalUnderflow, totalPending, oldestRebalance := fw.GetLazyRebalancingStats()

	// MVP stub returns zeros.
	require.Equal(t, 0, totalUnderflow)
	require.Equal(t, 0, totalPending)
	require.Equal(t, time.Duration(0), oldestRebalance)
}

// TestEnableLazyRebalancing_ValidConfig tests enabling lazy rebalancing with a valid config.
func TestEnableLazyRebalancing_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_lazy_valid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultLazyConfig()
	err = fw.EnableLazyRebalancing(config)
	require.NoError(t, err)
}

// TestEnableLazyRebalancing_InvalidThreshold tests validation of threshold.
func TestEnableLazyRebalancing_InvalidThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_lazy_bad_thresh.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultLazyConfig()
	config.Threshold = 0 // Invalid: must be > 0.
	err = fw.EnableLazyRebalancing(config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid threshold")

	config.Threshold = -0.5 // Negative is also invalid.
	err = fw.EnableLazyRebalancing(config)
	require.Error(t, err)
}

// TestEnableLazyRebalancing_InvalidMaxDelay tests validation of max delay.
func TestEnableLazyRebalancing_InvalidMaxDelay(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_lazy_bad_delay.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultLazyConfig()
	config.MaxDelay = 0 // Invalid: must be > 0.
	err = fw.EnableLazyRebalancing(config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid max delay")
}

// TestIsIncrementalRebalancingEnabled tests the incremental rebalancing status check.
func TestIsIncrementalRebalancingEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "is_incremental.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// MVP stub always returns false.
	require.False(t, fw.IsIncrementalRebalancingEnabled())
}

// TestGetIncrementalRebalancingProgress tests the incremental progress retrieval.
func TestGetIncrementalRebalancingProgress(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "incr_progress.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	progress, err := fw.GetIncrementalRebalancingProgress()
	// MVP returns an error since incremental is not enabled.
	require.Error(t, err)
	require.Contains(t, err.Error(), "incremental rebalancing not enabled")
	require.Equal(t, 0, progress.NodesRebalanced)
	require.Equal(t, 0, progress.NodesRemaining)
}

// TestEnableIncrementalRebalancing_ValidConfig tests enabling incremental rebalancing.
func TestEnableIncrementalRebalancing_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_incr_valid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultIncrementalConfig()
	err = fw.EnableIncrementalRebalancing(config)
	require.NoError(t, err)
}

// TestEnableIncrementalRebalancing_InvalidBudget tests validation of budget.
func TestEnableIncrementalRebalancing_InvalidBudget(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_incr_bad_budget.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultIncrementalConfig()
	config.Budget = 0 // Invalid: must be > 0.
	err = fw.EnableIncrementalRebalancing(config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid budget")
}

// TestEnableIncrementalRebalancing_InvalidInterval tests validation of interval.
func TestEnableIncrementalRebalancing_InvalidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enable_incr_bad_interval.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	config := structures.DefaultIncrementalConfig()
	config.Interval = 0 // Invalid: must be > 0.
	err = fw.EnableIncrementalRebalancing(config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid interval")
}

// TestStopIncrementalRebalancing tests the stop method.
func TestStopIncrementalRebalancing(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "stop_incr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// MVP stub returns nil (no-op).
	err = fw.StopIncrementalRebalancing()
	require.NoError(t, err)
}

// ============================================
// Smart Rebalancing Option Tests
// ============================================

// TestSmartAllowedModes tests the SmartAllowedModes option.
func TestSmartAllowedModes(t *testing.T) {
	cfg := &SmartRebalancingConfig{}
	opt := SmartAllowedModes("lazy", "incremental")
	opt(cfg)

	require.Equal(t, []string{"lazy", "incremental"}, cfg.AllowedModes)
}

// TestSmartOnModeChange tests the SmartOnModeChange callback option.
func TestSmartOnModeChange(t *testing.T) {
	cfg := &SmartRebalancingConfig{}
	called := false
	opt := SmartOnModeChange(func(d ModeDecision) {
		called = true
		_ = d.SelectedMode
	})
	opt(cfg)

	require.NotNil(t, cfg.OnModeChange)

	// Invoke callback to verify it works.
	cfg.OnModeChange(ModeDecision{SelectedMode: "lazy", Reason: "test"})
	require.True(t, called)
}

// TestSmartAutoDetect_Full tests SmartAutoDetect with both true and false.
func TestSmartAutoDetect_Full(t *testing.T) {
	cfg := &SmartRebalancingConfig{}

	SmartAutoDetect(true)(cfg)
	require.True(t, cfg.AutoDetect)

	SmartAutoDetect(false)(cfg)
	require.False(t, cfg.AutoDetect)
}

// TestSmartAutoSwitch_Full tests SmartAutoSwitch with both true and false.
func TestSmartAutoSwitch_Full(t *testing.T) {
	cfg := &SmartRebalancingConfig{}

	SmartAutoSwitch(true)(cfg)
	require.True(t, cfg.AutoSwitch)

	SmartAutoSwitch(false)(cfg)
	require.False(t, cfg.AutoSwitch)
}

// TestSmartMinFileSize_Full tests SmartMinFileSize with various values.
func TestSmartMinFileSize_Full(t *testing.T) {
	cfg := &SmartRebalancingConfig{}

	SmartMinFileSize(100 * MB)(cfg)
	require.Equal(t, uint64(100*MB), cfg.MinFileSize)

	SmartMinFileSize(0)(cfg)
	require.Equal(t, uint64(0), cfg.MinFileSize)
}

// TestSmartAllowedModes_Empty tests SmartAllowedModes with no modes.
func TestSmartAllowedModes_Empty(t *testing.T) {
	cfg := &SmartRebalancingConfig{}
	SmartAllowedModes()(cfg)
	require.Empty(t, cfg.AllowedModes)
}

// TestSmartAllowedModes_Single tests SmartAllowedModes with a single mode.
func TestSmartAllowedModes_Single(t *testing.T) {
	cfg := &SmartRebalancingConfig{}
	SmartAllowedModes("none")(cfg)
	require.Equal(t, []string{"none"}, cfg.AllowedModes)
}

// TestSmartOnModeChange_Nil tests SmartOnModeChange with nil callback.
func TestSmartOnModeChange_Nil(t *testing.T) {
	cfg := &SmartRebalancingConfig{}
	SmartOnModeChange(nil)(cfg)
	require.Nil(t, cfg.OnModeChange)
}

// ============================================
// ChunkIterator.Progress() Test
// ============================================

// TestChunkIterator_Progress tests the Progress() method on ChunkIterator.
func TestChunkIterator_Progress(t *testing.T) {
	// Create a chunked file using the helper already in the codebase.
	testFile := createChunkedTestFile(t)

	file, err := Open(testFile)
	require.NoError(t, err)
	defer file.Close()

	ds := findFirstDataset(file)
	require.NotNil(t, ds, "No dataset found in file")

	iter, err := ds.ChunkIterator()
	require.NoError(t, err)

	// Before any iteration, current should be 0.
	current, total := iter.Progress()
	require.Equal(t, 0, current, "current should be 0 before iteration")
	require.Greater(t, total, 0, "total should be > 0 for a chunked dataset")

	// Iterate all chunks and verify progress increments.
	chunkCount := 0
	for iter.Next() {
		chunkCount++
		cur, tot := iter.Progress()
		require.Equal(t, chunkCount, cur, "current should match iteration count")
		require.Equal(t, total, tot, "total should remain constant during iteration")
	}
	require.NoError(t, iter.Err())

	// After iteration, current is total+1 because Next() increments before
	// the bounds check (the final Next() call that returns false still increments).
	finalCur, finalTot := iter.Progress()
	require.Equal(t, finalTot+1, finalCur, "current should be total+1 after full iteration")
}

// ============================================
// Combined lifecycle tests
// ============================================

// TestRebalancingAPI_FullLifecycle tests the full lifecycle of rebalancing APIs.
func TestRebalancingAPI_FullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "lifecycle.h5")

	fw, err := CreateForWrite(filename, CreateTruncate,
		WithLazyRebalancing(
			LazyThreshold(0.10),
			LazyMaxDelay(10*time.Minute),
			LazyBatchSize(50),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Check lazy status (MVP: always false).
	require.False(t, fw.IsLazyRebalancingEnabled())

	// Get stats (MVP: zeros).
	u, p, d := fw.GetLazyRebalancingStats()
	require.Equal(t, 0, u)
	require.Equal(t, 0, p)
	require.Equal(t, time.Duration(0), d)

	// Force batch (MVP: no-op).
	require.NoError(t, fw.ForceBatchRebalance())

	// Disable lazy (MVP: no-op).
	require.NoError(t, fw.DisableLazyRebalancing())

	// Check incremental status.
	require.False(t, fw.IsIncrementalRebalancingEnabled())

	// Get incremental progress (MVP: error).
	_, err = fw.GetIncrementalRebalancingProgress()
	require.Error(t, err)

	// Stop incremental (MVP: no-op).
	require.NoError(t, fw.StopIncrementalRebalancing())
}

// TestWithSmartRebalancing_AllOptions tests WithSmartRebalancing applies all options.
func TestWithSmartRebalancing_AllOptions(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "smart_all.h5")

	modeChanged := false

	fw, err := CreateForWrite(filename, CreateTruncate,
		WithSmartRebalancing(
			SmartAutoDetect(true),
			SmartAutoSwitch(true),
			SmartMinFileSize(1*GB),
			SmartAllowedModes("lazy", "incremental"),
			SmartOnModeChange(func(d ModeDecision) {
				modeChanged = true
				_ = d
			}),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Smart rebalancing is a Phase 3 placeholder; just verify the file was created.
	require.False(t, modeChanged, "callback should not be invoked during creation")
}
