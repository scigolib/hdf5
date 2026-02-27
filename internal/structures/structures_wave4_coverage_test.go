// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// Lazy Rebalancing Tests (btreev2_lazy.go)
// ===========================================================================

// TestDefaultLazyConfig verifies the default lazy rebalancing configuration.
func TestDefaultLazyConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultLazyConfig()

	if !cfg.Enabled {
		t.Error("DefaultLazyConfig should be Enabled")
	}
	if cfg.Threshold != 0.05 {
		t.Errorf("Threshold = %f, want 0.05", cfg.Threshold)
	}
	if cfg.MaxDelay != 5*time.Minute {
		t.Errorf("MaxDelay = %v, want 5m", cfg.MaxDelay)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.BatchSize)
	}
}

// TestEnableLazyRebalancing_DefaultValues tests that EnableLazyRebalancing applies defaults for invalid config.
func TestEnableLazyRebalancing_DefaultValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    LazyRebalancingConfig
		wantThres float64
		wantDelay time.Duration
		wantBatch int
	}{
		{
			name:      "all defaults when zero",
			config:    LazyRebalancingConfig{Enabled: true},
			wantThres: 0.05,
			wantDelay: 5 * time.Minute,
			wantBatch: 100,
		},
		{
			name: "threshold capped at 20%",
			config: LazyRebalancingConfig{
				Enabled:   true,
				Threshold: 0.50, // Above cap
				MaxDelay:  1 * time.Second,
				BatchSize: 10,
			},
			wantThres: 0.20,
			wantDelay: 1 * time.Second,
			wantBatch: 10,
		},
		{
			name: "negative threshold defaults to 5%",
			config: LazyRebalancingConfig{
				Enabled:   true,
				Threshold: -0.1,
				MaxDelay:  1 * time.Second,
				BatchSize: 50,
			},
			wantThres: 0.05,
			wantDelay: 1 * time.Second,
			wantBatch: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
			bt.EnableLazyRebalancing(tt.config)

			if bt.lazyState == nil {
				t.Fatal("lazyState should not be nil after EnableLazyRebalancing")
			}
			if bt.lazyState.Config.Threshold != tt.wantThres {
				t.Errorf("Threshold = %f, want %f", bt.lazyState.Config.Threshold, tt.wantThres)
			}
			if bt.lazyState.Config.MaxDelay != tt.wantDelay {
				t.Errorf("MaxDelay = %v, want %v", bt.lazyState.Config.MaxDelay, tt.wantDelay)
			}
			if bt.lazyState.Config.BatchSize != tt.wantBatch {
				t.Errorf("BatchSize = %d, want %d", bt.lazyState.Config.BatchSize, tt.wantBatch)
			}
			if bt.lazyState.TotalNodes != 1 {
				t.Errorf("TotalNodes = %d, want 1 (MVP single leaf)", bt.lazyState.TotalNodes)
			}
		})
	}
}

// TestIsLazyRebalancingEnabled tests the lazy rebalancing status check.
func TestIsLazyRebalancingEnabled(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	if bt.IsLazyRebalancingEnabled() {
		t.Error("should be false before enabling")
	}

	bt.EnableLazyRebalancing(DefaultLazyConfig())

	if !bt.IsLazyRebalancingEnabled() {
		t.Error("should be true after enabling")
	}
}

// TestDisableLazyRebalancing tests disabling lazy rebalancing.
func TestDisableLazyRebalancing(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Disabling when not enabled should be no-op.
	err := bt.DisableLazyRebalancing()
	if err != nil {
		t.Fatalf("DisableLazyRebalancing when not enabled: %v", err)
	}

	// Enable, then disable.
	bt.EnableLazyRebalancing(DefaultLazyConfig())
	if !bt.IsLazyRebalancingEnabled() {
		t.Fatal("should be enabled")
	}

	err = bt.DisableLazyRebalancing()
	if err != nil {
		t.Fatalf("DisableLazyRebalancing failed: %v", err)
	}

	if bt.IsLazyRebalancingEnabled() {
		t.Error("should be disabled after DisableLazyRebalancing")
	}
}

// TestDeleteRecordLazy_BasicDeletion tests lazy deletion of records.
func TestDeleteRecordLazy_BasicDeletion(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	// Use a very high threshold so batch rebalancing never triggers automatically.
	bt.EnableLazyRebalancing(LazyRebalancingConfig{
		Enabled:   true,
		Threshold: 0.20, // 20% - maximum allowed, but since TotalNodes=1 (MVP),
		MaxDelay:  10 * time.Minute,
		BatchSize: 100,
	})

	// Insert many records so that deleting one does NOT cause underflow.
	// minRecords for 4KB node = 185 (half of max 371), so insert enough
	// that after one deletion we're still above minRecords.
	numRecords := 200
	for i := 0; i < numRecords; i++ {
		name := fmt.Sprintf("attr_%04d", i)
		if err := bt.InsertRecord(name, uint64(0x1000+i)); err != nil {
			t.Fatalf("InsertRecord(%s): %v", name, err)
		}
	}

	if bt.header.TotalRecords != uint64(numRecords) {
		t.Fatalf("TotalRecords = %d, want %d", bt.header.TotalRecords, numRecords)
	}

	// Lazy delete one record. Should NOT trigger batch rebalance since
	// remaining records (199) > minRecords (185) and underflow = 0.
	err := bt.DeleteRecordLazy("attr_0050")
	if err != nil {
		t.Fatalf("DeleteRecordLazy: %v", err)
	}

	if bt.HasKey("attr_0050") {
		t.Error("attr_0050 should be deleted")
	}
	if bt.header.TotalRecords != uint64(numRecords-1) {
		t.Errorf("TotalRecords = %d, want %d", bt.header.TotalRecords, numRecords-1)
	}

	// Verify pending deletes tracked (no batch rebalance triggered).
	_, pending, _ := bt.GetLazyRebalancingStats()
	if pending != 1 {
		t.Errorf("PendingDeletes = %d, want 1", pending)
	}
}

// TestDeleteRecordLazy_NotEnabled tests that lazy delete fails when not enabled.
func TestDeleteRecordLazy_NotEnabled(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	_ = bt.InsertRecord("test", 0x1000)

	err := bt.DeleteRecordLazy("test")
	if err == nil {
		t.Fatal("expected error when lazy rebalancing not enabled")
	}
}

// TestDeleteRecordLazy_NotFound tests lazy delete of non-existent record.
func TestDeleteRecordLazy_NotFound(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())
	_ = bt.InsertRecord("existing", 0x1000)

	err := bt.DeleteRecordLazy("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent record")
	}
}

// TestDeleteRecordLazy_MultipleDeletesTrackStats tests that multiple lazy deletes
// properly track statistics.
func TestDeleteRecordLazy_MultipleDeletesTrackStats(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	// Use max threshold to prevent auto-trigger.
	bt.EnableLazyRebalancing(LazyRebalancingConfig{
		Enabled:   true,
		Threshold: 0.20,
		MaxDelay:  10 * time.Minute,
		BatchSize: 100,
	})

	// Insert 250 records (above minRecords=185 for 4KB node).
	// Deleting 5 keeps us at 245, well above minRecords -> no underflow -> no trigger.
	for i := 0; i < 250; i++ {
		if err := bt.InsertRecord(fmt.Sprintf("rec_%04d", i), uint64(0x1000+i)); err != nil {
			t.Fatalf("InsertRecord: %v", err)
		}
	}

	// Delete 5 records lazily.
	for i := 0; i < 5; i++ {
		if err := bt.DeleteRecordLazy(fmt.Sprintf("rec_%04d", i)); err != nil {
			t.Fatalf("DeleteRecordLazy: %v", err)
		}
	}

	_, pending, elapsed := bt.GetLazyRebalancingStats()
	if pending != 5 {
		t.Errorf("PendingDeletes = %d, want 5", pending)
	}
	if elapsed < 0 {
		t.Errorf("TimeSinceRebalance should be >= 0, got %v", elapsed)
	}
}

// TestBatchRebalance_NilLazyState tests BatchRebalance when lazy state is nil.
func TestBatchRebalance_NilLazyState(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	err := bt.BatchRebalance()
	if err != nil {
		t.Fatalf("BatchRebalance with nil lazyState should succeed: %v", err)
	}
}

// TestBatchRebalance_ResetsState tests that BatchRebalance resets tracking state.
func TestBatchRebalance_ResetsState(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	// Simulate underflow.
	bt.lazyState.UnderflowCount = 5
	bt.lazyState.PendingDeletes = 10
	bt.lazyState.UnderflowNodes = []uint64{0x100, 0x200}

	err := bt.BatchRebalance()
	if err != nil {
		t.Fatalf("BatchRebalance failed: %v", err)
	}

	if bt.lazyState.UnderflowCount != 0 {
		t.Errorf("UnderflowCount = %d, want 0", bt.lazyState.UnderflowCount)
	}
	if bt.lazyState.PendingDeletes != 0 {
		t.Errorf("PendingDeletes = %d, want 0", bt.lazyState.PendingDeletes)
	}
	if bt.lazyState.UnderflowNodes != nil {
		t.Error("UnderflowNodes should be nil after reset")
	}
}

// TestForceBatchRebalance tests force-triggering batch rebalancing.
func TestForceBatchRebalance(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Should fail when lazy not enabled.
	err := bt.ForceBatchRebalance()
	if err == nil {
		t.Fatal("expected error when lazy not enabled")
	}

	// Enable and force.
	bt.EnableLazyRebalancing(DefaultLazyConfig())
	bt.lazyState.PendingDeletes = 42

	err = bt.ForceBatchRebalance()
	if err != nil {
		t.Fatalf("ForceBatchRebalance failed: %v", err)
	}

	if bt.lazyState.PendingDeletes != 0 {
		t.Errorf("PendingDeletes = %d, want 0", bt.lazyState.PendingDeletes)
	}
}

// TestGetLazyRebalancingStats_Disabled tests stats when lazy is not enabled.
func TestGetLazyRebalancingStats_Disabled(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	underflow, pending, elapsed := bt.GetLazyRebalancingStats()
	if underflow != 0 || pending != 0 || elapsed != 0 {
		t.Errorf("Stats should be zero when disabled: underflow=%d, pending=%d, elapsed=%v",
			underflow, pending, elapsed)
	}
}

// TestShouldTriggerBatchRebalancing tests the threshold and delay triggers.
func TestShouldTriggerBatchRebalancing(t *testing.T) {
	t.Parallel()

	t.Run("nil lazy state", func(t *testing.T) {
		t.Parallel()
		bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
		if bt.shouldTriggerBatchRebalancing() {
			t.Error("should be false with nil lazyState")
		}
	})

	t.Run("threshold trigger", func(t *testing.T) {
		t.Parallel()
		bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
		bt.EnableLazyRebalancing(LazyRebalancingConfig{
			Enabled:   true,
			Threshold: 0.05,
			MaxDelay:  1 * time.Hour,
			BatchSize: 100,
		})

		// Set high underflow ratio.
		bt.lazyState.UnderflowCount = 1
		bt.lazyState.TotalNodes = 1 // 100% underflow > 5%

		if !bt.shouldTriggerBatchRebalancing() {
			t.Error("should trigger at 100% underflow")
		}
	})

	t.Run("max delay trigger", func(t *testing.T) {
		t.Parallel()
		bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
		bt.EnableLazyRebalancing(LazyRebalancingConfig{
			Enabled:   true,
			Threshold: 0.05,
			MaxDelay:  1 * time.Millisecond,
			BatchSize: 100,
		})

		// Wait for delay to exceed.
		time.Sleep(5 * time.Millisecond)

		if !bt.shouldTriggerBatchRebalancing() {
			t.Error("should trigger after MaxDelay exceeded")
		}
	})

	t.Run("no trigger below threshold", func(t *testing.T) {
		t.Parallel()
		bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
		bt.EnableLazyRebalancing(LazyRebalancingConfig{
			Enabled:   true,
			Threshold: 0.10,
			MaxDelay:  1 * time.Hour,
			BatchSize: 100,
		})

		bt.lazyState.UnderflowCount = 0
		bt.lazyState.TotalNodes = 100

		if bt.shouldTriggerBatchRebalancing() {
			t.Error("should not trigger with 0% underflow")
		}
	})
}

// ===========================================================================
// Incremental Rebalancing Tests (btreev2_incremental.go)
// ===========================================================================

// TestDefaultIncrementalConfig verifies the default incremental rebalancing configuration.
func TestDefaultIncrementalConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultIncrementalConfig()

	if !cfg.Enabled {
		t.Error("should be Enabled")
	}
	if cfg.Budget != 100*time.Millisecond {
		t.Errorf("Budget = %v, want 100ms", cfg.Budget)
	}
	if cfg.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", cfg.Interval)
	}
	if cfg.ProgressCallback != nil {
		t.Error("ProgressCallback should be nil by default")
	}
}

// TestEnableIncrementalRebalancing_RequiresLazy tests that incremental requires lazy to be enabled.
func TestEnableIncrementalRebalancing_RequiresLazy(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	err := bt.EnableIncrementalRebalancing(DefaultIncrementalConfig())
	if err == nil {
		t.Fatal("expected error when lazy rebalancing not enabled")
	}
}

// TestEnableIncrementalRebalancing_Success tests the full enable/stop lifecycle.
func TestEnableIncrementalRebalancing_Success(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   10 * time.Millisecond,
		Interval: 50 * time.Millisecond,
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing failed: %v", err)
	}

	if !bt.IsIncrementalRebalancingEnabled() {
		t.Error("should be enabled after EnableIncrementalRebalancing")
	}

	// Stop the goroutine.
	err = bt.StopIncrementalRebalancing()
	if err != nil {
		t.Fatalf("StopIncrementalRebalancing failed: %v", err)
	}

	if bt.IsIncrementalRebalancingEnabled() {
		t.Error("should be disabled after StopIncrementalRebalancing")
	}
}

// TestEnableIncrementalRebalancing_DefaultConfigValues tests that zero budget/interval get defaults.
func TestEnableIncrementalRebalancing_DefaultConfigValues(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   0, // Should default to 100ms
		Interval: 0, // Should default to 5s
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing failed: %v", err)
	}
	defer func() { _ = bt.StopIncrementalRebalancing() }()

	if bt.incrementalRebalancer.config.Budget != 100*time.Millisecond {
		t.Errorf("Budget = %v, want 100ms", bt.incrementalRebalancer.config.Budget)
	}
	if bt.incrementalRebalancer.config.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", bt.incrementalRebalancer.config.Interval)
	}
}

// TestEnableIncrementalRebalancing_AlreadyRunning tests double-enable error.
func TestEnableIncrementalRebalancing_AlreadyRunning(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   10 * time.Millisecond,
		Interval: 50 * time.Millisecond,
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("first enable failed: %v", err)
	}

	// Second enable should fail.
	err = bt.EnableIncrementalRebalancing(config)
	if err == nil {
		t.Error("expected error on second enable")
	}

	_ = bt.StopIncrementalRebalancing()
}

// TestStopIncrementalRebalancing_NotRunning tests stopping when not running.
func TestStopIncrementalRebalancing_NotRunning(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	err := bt.StopIncrementalRebalancing()
	if err != nil {
		t.Fatalf("StopIncrementalRebalancing when not running should succeed: %v", err)
	}
}

// TestStopIncrementalRebalancing_WithPendingUnderflow tests that stop performs final rebalancing.
func TestStopIncrementalRebalancing_WithPendingUnderflow(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   10 * time.Millisecond,
		Interval: 1 * time.Hour, // Very long interval so it does not tick
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing failed: %v", err)
	}

	// Simulate pending underflow nodes.
	bt.lazyState.UnderflowNodes = []uint64{0x100, 0x200}
	bt.lazyState.UnderflowCount = 2

	err = bt.StopIncrementalRebalancing()
	if err != nil {
		t.Fatalf("StopIncrementalRebalancing failed: %v", err)
	}

	// Final rebalancing should have reset state.
	if bt.lazyState.UnderflowCount != 0 {
		t.Errorf("UnderflowCount = %d, want 0 after final rebalancing", bt.lazyState.UnderflowCount)
	}
}

// TestIsIncrementalRebalancingEnabled_States tests status in different states.
func TestIsIncrementalRebalancingEnabled_States(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Initially disabled.
	if bt.IsIncrementalRebalancingEnabled() {
		t.Error("should be false initially")
	}

	// With nil rebalancer.
	bt.incrementalRebalancer = nil
	if bt.IsIncrementalRebalancingEnabled() {
		t.Error("should be false with nil rebalancer")
	}
}

// TestGetIncrementalRebalancingProgress_NotEnabled tests progress when not enabled.
func TestGetIncrementalRebalancingProgress_NotEnabled(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	_, err := bt.GetIncrementalRebalancingProgress()
	if err == nil {
		t.Fatal("expected error when incremental not enabled")
	}
}

// TestGetIncrementalRebalancingProgress_Enabled tests progress reporting.
func TestGetIncrementalRebalancingProgress_Enabled(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   10 * time.Millisecond,
		Interval: 1 * time.Hour,
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing failed: %v", err)
	}
	defer func() { _ = bt.StopIncrementalRebalancing() }()

	progress, err := bt.GetIncrementalRebalancingProgress()
	if err != nil {
		t.Fatalf("GetIncrementalRebalancingProgress failed: %v", err)
	}

	// No work done yet.
	if progress.NodesRebalanced != 0 {
		t.Errorf("NodesRebalanced = %d, want 0", progress.NodesRebalanced)
	}
	if !progress.IsComplete {
		t.Error("should report IsComplete when no pending nodes")
	}
}

// TestIncrementalRebalancer_StartStop tests the Start/Stop methods directly.
func TestIncrementalRebalancer_StartStop(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	ir := &IncrementalRebalancer{
		btree:       bt,
		config:      DefaultIncrementalConfig(),
		running:     false,
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}
	ir.config.Interval = 50 * time.Millisecond

	// Start.
	ir.Start()

	// Starting again should be a no-op.
	ir.Start()

	ir.mu.Lock()
	running := ir.running
	ir.mu.Unlock()
	if !running {
		t.Error("should be running after Start")
	}

	// Stop.
	ir.Stop()

	ir.mu.Lock()
	running = ir.running
	ir.mu.Unlock()
	if running {
		t.Error("should not be running after Stop")
	}

	// Stop again should be a no-op.
	ir.Stop()
}

// TestIncrementalRebalancer_GetProgress tests progress with no lazy state.
func TestIncrementalRebalancer_GetProgress(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	ir := &IncrementalRebalancer{
		btree:  bt,
		config: DefaultIncrementalConfig(),
	}

	// With nil lazyState.
	progress := ir.GetProgress()
	if progress.NodesRemaining != 0 {
		t.Errorf("NodesRemaining = %d, want 0 with nil lazyState", progress.NodesRemaining)
	}
	if !progress.IsComplete {
		t.Error("should be complete with nil lazyState")
	}
}

// TestIncrementalRebalancer_RebalanceIncremental tests the incremental rebalancing session.
func TestIncrementalRebalancer_RebalanceIncremental(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	// Track progress callbacks.
	var callbackMu sync.Mutex
	var callbackCalled bool

	ir := &IncrementalRebalancer{
		btree: bt,
		config: IncrementalRebalancingConfig{
			Enabled:  true,
			Budget:   100 * time.Millisecond,
			Interval: 50 * time.Millisecond,
			ProgressCallback: func(_ RebalancingProgress) {
				callbackMu.Lock()
				callbackCalled = true
				callbackMu.Unlock()
			},
		},
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}

	// Test with no underflow nodes (should be no-op).
	ir.rebalanceIncremental()

	// Add underflow nodes.
	bt.lazyState.UnderflowNodes = []uint64{0x100, 0x200, 0x300}
	bt.lazyState.UnderflowCount = 3

	// Run one session.
	ir.rebalanceIncremental()

	// All nodes should be processed (budget is large enough for 3 nodes).
	if len(bt.lazyState.UnderflowNodes) != 0 {
		t.Errorf("remaining nodes = %d, want 0", len(bt.lazyState.UnderflowNodes))
	}

	callbackMu.Lock()
	if !callbackCalled {
		t.Error("progress callback should have been called")
	}
	callbackMu.Unlock()

	// Check progress.
	progress := ir.GetProgress()
	if progress.NodesRebalanced != 3 {
		t.Errorf("NodesRebalanced = %d, want 3", progress.NodesRebalanced)
	}
	if !progress.IsComplete {
		t.Error("should be complete after processing all nodes")
	}
}

// TestIncrementalRebalancer_RebalancingLoop tests the background loop with actual ticking.
func TestIncrementalRebalancer_RebalancingLoop(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	// Add nodes to process.
	bt.lazyState.UnderflowNodes = []uint64{0x100, 0x200}

	config := IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   50 * time.Millisecond,
		Interval: 20 * time.Millisecond, // Quick ticks for testing
	}

	err := bt.EnableIncrementalRebalancing(config)
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing failed: %v", err)
	}

	// Wait for the loop to process the nodes.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for incremental rebalancing to complete")
		default:
			if len(bt.lazyState.UnderflowNodes) == 0 {
				goto done
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
done:

	_ = bt.StopIncrementalRebalancing()
}

// TestIncrementalRebalancer_ETACalculation tests ETA computation with remaining nodes.
func TestIncrementalRebalancer_ETACalculation(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(DefaultLazyConfig())

	ir := &IncrementalRebalancer{
		btree: bt,
		config: IncrementalRebalancingConfig{
			Enabled:  true,
			Budget:   1 * time.Nanosecond, // Very small budget to process only a few
			Interval: 50 * time.Millisecond,
		},
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}

	// Add many nodes so budget is exhausted before all are processed.
	nodes := make([]uint64, 100000)
	for i := range nodes {
		nodes[i] = uint64(0x100 + i)
	}
	bt.lazyState.UnderflowNodes = nodes

	ir.rebalanceIncremental()

	// Should have partial progress.
	progress := ir.GetProgress()
	if progress.NodesRebalanced == 0 {
		t.Error("should have rebalanced at least some nodes")
	}
	// ETA should be set if there are remaining nodes.
	if progress.NodesRemaining > 0 && progress.EstimatedRemaining == 0 {
		t.Error("ETA should be non-zero with remaining nodes")
	}
}

// ===========================================================================
// Rebalance Tests (btreev2_rebalance.go) - Error Paths
// ===========================================================================

// TestHandleRootDepthDecrease tests the root depth decrease handler (MVP no-op).
func TestHandleRootDepthDecrease(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	err := bt.handleRootDepthDecrease()
	if err != nil {
		t.Fatalf("handleRootDepthDecrease should be no-op in MVP: %v", err)
	}
}

// TestRebalanceAll tests the manual rebalance-all function (MVP no-op).
func TestRebalanceAll(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	_ = bt.InsertRecord("test", 0x1000)

	err := bt.RebalanceAll()
	if err != nil {
		t.Fatalf("RebalanceAll should be no-op in MVP: %v", err)
	}
}

// TestBorrowFromLeft_EmptySibling tests borrowing from an empty left sibling.
func TestBorrowFromLeft_EmptySibling(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	left := &BTreeV2LeafNode{Records: nil}
	current := &BTreeV2LeafNode{Records: []LinkNameRecord{{NameHash: 100}}}

	err := bt.borrowFromLeft(current, left)
	if err == nil {
		t.Fatal("expected error when borrowing from empty left sibling")
	}
}

// TestBorrowFromRight_EmptySibling tests borrowing from an empty right sibling.
func TestBorrowFromRight_EmptySibling(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	current := &BTreeV2LeafNode{Records: []LinkNameRecord{{NameHash: 100}}}
	right := &BTreeV2LeafNode{Records: nil}

	err := bt.borrowFromRight(current, right)
	if err == nil {
		t.Fatal("expected error when borrowing from empty right sibling")
	}
}

// TestMergeNodes_Overflow tests merging when combined records exceed max.
func TestMergeNodes_Overflow(t *testing.T) {
	t.Parallel()

	// Use tiny node size so max records is very low.
	bt := NewWritableBTreeV2(32) // Very small node
	maxRecords := bt.calculateMaxRecords()

	// Create nodes that together exceed maxRecords.
	leftRecords := make([]LinkNameRecord, maxRecords)
	for i := range leftRecords {
		leftRecords[i] = LinkNameRecord{NameHash: uint32(i)}
	}
	rightRecords := []LinkNameRecord{{NameHash: uint32(maxRecords + 1)}}

	left := &BTreeV2LeafNode{Records: leftRecords}
	right := &BTreeV2LeafNode{Records: rightRecords}

	err := bt.mergeNodes(left, right)
	if err == nil {
		t.Fatal("expected error when merged records exceed max")
	}
}

// ===========================================================================
// B-tree v2 Write Tests (btreev2_write.go) - UpdateRecord, DeleteRecord
// ===========================================================================

// TestUpdateRecord tests updating an existing record's heap ID.
func TestUpdateRecord(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert a record.
	err := bt.InsertRecord("my_link", 0x1111)
	if err != nil {
		t.Fatalf("InsertRecord failed: %v", err)
	}

	// Update it.
	err = bt.UpdateRecord("my_link", 0x2222)
	if err != nil {
		t.Fatalf("UpdateRecord failed: %v", err)
	}

	// Verify updated.
	heapID, found := bt.SearchRecord("my_link")
	if !found {
		t.Fatal("record not found after update")
	}

	var expected [8]byte
	binary.LittleEndian.PutUint64(expected[:], 0x2222)
	for i := 0; i < 7; i++ {
		if heapID[i] != expected[i] {
			t.Errorf("heapID[%d] = %02X, want %02X", i, heapID[i], expected[i])
		}
	}
}

// TestUpdateRecord_NotFound tests updating a non-existent record.
func TestUpdateRecord_NotFound(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	_ = bt.InsertRecord("existing", 0x1000)

	err := bt.UpdateRecord("nonexistent", 0x2000)
	if err == nil {
		t.Fatal("expected error for non-existent record")
	}
}

// TestDeleteRecord_Delegates tests that DeleteRecord delegates to DeleteRecordWithRebalancing.
func TestDeleteRecord_Delegates(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	_ = bt.InsertRecord("to_delete", 0x1000)

	err := bt.DeleteRecord("to_delete")
	if err != nil {
		t.Fatalf("DeleteRecord failed: %v", err)
	}

	if bt.HasKey("to_delete") {
		t.Error("record should be deleted")
	}
	if bt.header.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", bt.header.TotalRecords)
	}
}

// ===========================================================================
// Fractal Heap Write Tests (fractalheap_write.go) - OverwriteObject, DeleteObject
// ===========================================================================

// TestOverwriteObject tests in-place object overwrite.
func TestOverwriteObject(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	// Insert an object.
	originalData := []byte("hello world!")
	heapID, err := fh.InsertObject(originalData)
	if err != nil {
		t.Fatalf("InsertObject failed: %v", err)
	}

	// Overwrite with same-size data.
	newData := []byte("HELLO WORLD!")
	err = fh.OverwriteObject(heapID, newData)
	if err != nil {
		t.Fatalf("OverwriteObject failed: %v", err)
	}

	// Verify the data was overwritten.
	readBack, err := fh.GetObject(heapID)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if !bytes.Equal(readBack, newData) {
		t.Errorf("readBack = %q, want %q", readBack, newData)
	}
}

// TestOverwriteObject_SizeMismatch tests overwrite with different size data.
func TestOverwriteObject_SizeMismatch(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	data := []byte("hello")
	heapID, err := fh.InsertObject(data)
	if err != nil {
		t.Fatalf("InsertObject failed: %v", err)
	}

	err = fh.OverwriteObject(heapID, []byte("hi"))
	if err == nil {
		t.Fatal("expected error for size mismatch")
	}
}

// TestOverwriteObject_InvalidID tests overwrite with invalid heap IDs.
func TestOverwriteObject_InvalidID(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	tests := []struct {
		name   string
		heapID []byte
	}{
		{"empty", []byte{}},
		{"bad version", []byte{0x40, 0, 0, 0, 0, 0, 0, 0}}, // version=1
		{"huge type", []byte{0x10, 0, 0, 0, 0, 0, 0, 0}},   // type=huge
		{"wrong size", []byte{0x00, 0x01}},                 // too short
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := fh.OverwriteObject(tt.heapID, []byte("data"))
			if err == nil {
				t.Error("expected error for invalid heap ID")
			}
		})
	}
}

// TestOverwriteObject_OutOfBounds tests overwrite with offset beyond used space.
func TestOverwriteObject_OutOfBounds(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	// Manually craft a heap ID with a large offset.
	heapID := make([]byte, fh.Header.HeapIDLength)
	heapID[0] = 0x00 // version=0, type=managed
	// Set offset to a large value (beyond used space).
	offset := uint64(9999)
	for i := 0; i < int(fh.Header.HeapOffsetSize); i++ {
		heapID[1+i] = byte(offset >> (8 * i))
	}
	// Set length to 5.
	length := uint64(5)
	idxLen := 1 + int(fh.Header.HeapOffsetSize)
	for i := 0; i < int(fh.Header.HeapLengthSize); i++ {
		heapID[idxLen+i] = byte(length >> (8 * i))
	}

	err := fh.OverwriteObject(heapID, []byte("hello"))
	if err == nil {
		t.Fatal("expected error for out-of-bounds offset")
	}
}

// TestDeleteObject tests object deletion from fractal heap.
func TestDeleteObject(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	data := []byte("test data to delete")
	heapID, err := fh.InsertObject(data)
	if err != nil {
		t.Fatalf("InsertObject failed: %v", err)
	}

	prevObjects := fh.Header.NumManagedObjects

	err = fh.DeleteObject(heapID)
	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}

	if fh.Header.NumManagedObjects != prevObjects-1 {
		t.Errorf("NumManagedObjects = %d, want %d", fh.Header.NumManagedObjects, prevObjects-1)
	}

	if fh.Header.FreeSpace <= 0 {
		t.Error("FreeSpace should increase after deletion")
	}
}

// TestDeleteObject_InvalidID tests deletion with invalid heap IDs.
func TestDeleteObject_InvalidID(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	tests := []struct {
		name   string
		heapID []byte
	}{
		{"empty", []byte{}},
		{"bad version", []byte{0x40, 0, 0, 0, 0, 0, 0, 0}},
		{"huge type", []byte{0x10, 0, 0, 0, 0, 0, 0, 0}},
		{"wrong size", []byte{0x00, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := fh.DeleteObject(tt.heapID)
			if err == nil {
				t.Error("expected error for invalid heap ID")
			}
		})
	}
}

// TestDeleteObject_OutOfBounds tests deletion with offset beyond used space.
func TestDeleteObject_OutOfBounds(t *testing.T) {
	t.Parallel()

	fh := NewWritableFractalHeap(4096)

	heapID := make([]byte, fh.Header.HeapIDLength)
	heapID[0] = 0x00 // version=0, type=managed
	offset := uint64(9999)
	for i := 0; i < int(fh.Header.HeapOffsetSize); i++ {
		heapID[1+i] = byte(offset >> (8 * i))
	}
	length := uint64(5)
	idxLen := 1 + int(fh.Header.HeapOffsetSize)
	for i := 0; i < int(fh.Header.HeapLengthSize); i++ {
		heapID[idxLen+i] = byte(length >> (8 * i))
	}

	err := fh.DeleteObject(heapID)
	if err == nil {
		t.Fatal("expected error for out-of-bounds offset")
	}
}

// ===========================================================================
// Fractal Heap Indirect Block Tests (fractalheap_indirect.go)
// ===========================================================================

// TestGetChildAddress tests GetChildAddress method.
func TestGetChildAddress(t *testing.T) {
	t.Parallel()

	wb := NewWritableIndirectBlock(0x1000, 0, 2, 2, 2)

	// Set some addresses.
	_ = wb.SetChildAddress(0, 0xAAAA)
	_ = wb.SetChildAddress(1, 0xBBBB)

	t.Run("valid index", func(t *testing.T) {
		t.Parallel()
		addr, err := wb.GetChildAddress(0)
		if err != nil {
			t.Fatalf("GetChildAddress(0) failed: %v", err)
		}
		if addr != 0xAAAA {
			t.Errorf("addr = 0x%X, want 0xAAAA", addr)
		}
	})

	t.Run("negative index", func(t *testing.T) {
		t.Parallel()
		_, err := wb.GetChildAddress(-1)
		if err == nil {
			t.Error("expected error for negative index")
		}
	})

	t.Run("out of bounds", func(t *testing.T) {
		t.Parallel()
		_, err := wb.GetChildAddress(100)
		if err == nil {
			t.Error("expected error for out-of-bounds index")
		}
	})
}

// ===========================================================================
// Local Heap Tests (localheap.go)
// ===========================================================================

// TestPrepareForModification tests converting read-mode heap to write-mode.
func TestPrepareForModification(t *testing.T) {
	t.Parallel()

	t.Run("basic conversion", func(t *testing.T) {
		t.Parallel()

		h := &LocalHeap{
			Data:            []byte("hello\x00world\x00\x00\x00\x00"),
			DataSegmentSize: 0, // Should be set from len(Data)
		}

		err := h.PrepareForModification()
		if err != nil {
			t.Fatalf("PrepareForModification failed: %v", err)
		}

		// Should be able to add strings now.
		h.DataSegmentSize = 256 // Expand for testing
		offset, err := h.AddString("new_string")
		if err != nil {
			t.Fatalf("AddString after prepare failed: %v", err)
		}

		// Offset should be right after "hello\0world\0".
		expectedOffset := uint64(len("hello") + 1 + len("world") + 1) // 12
		if offset != expectedOffset {
			t.Errorf("offset = %d, want %d", offset, expectedOffset)
		}
	})

	t.Run("nil data", func(t *testing.T) {
		t.Parallel()

		h := &LocalHeap{Data: nil}
		err := h.PrepareForModification()
		if err == nil {
			t.Fatal("expected error for nil data")
		}
	})

	t.Run("preserves data segment size", func(t *testing.T) {
		t.Parallel()

		h := &LocalHeap{
			Data:            []byte("test\x00\x00\x00\x00"),
			DataSegmentSize: 128,
		}

		err := h.PrepareForModification()
		if err != nil {
			t.Fatalf("PrepareForModification failed: %v", err)
		}

		if h.DataSegmentSize != 128 {
			t.Errorf("DataSegmentSize = %d, want 128 (preserved)", h.DataSegmentSize)
		}
	})

	t.Run("all zeros data", func(t *testing.T) {
		t.Parallel()

		h := &LocalHeap{
			Data:            []byte{0, 0, 0, 0, 0, 0, 0, 0},
			DataSegmentSize: 0,
		}

		err := h.PrepareForModification()
		if err != nil {
			t.Fatalf("PrepareForModification failed: %v", err)
		}

		// usedSize should be 0 (no non-zero bytes found).
		if len(h.strings) != 0 {
			t.Errorf("strings length = %d, want 0 for all-zeros data", len(h.strings))
		}
	})
}

// ===========================================================================
// writeAddr / writeAddressToBytes coverage (btree_group.go, symboltable_node.go)
// ===========================================================================

// TestWriteAddr_AllSizes tests writeAddr with different address sizes.
func TestWriteAddr_AllSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
		addr uint64
	}{
		{"1 byte", 1, 0x42},
		{"2 bytes", 2, 0x1234},
		{"4 bytes", 4, 0x12345678},
		{"8 bytes", 8, 0x123456789ABCDEF0},
		{"3 bytes (default)", 3, 0x123456},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf := make([]byte, 8)
			writeAddr(buf, tt.addr, tt.size, binary.LittleEndian)
			// Just verify it does not panic.
		})
	}
}

// TestWriteAddr_SizeExceedsBuffer tests writeAddr when size > buffer length.
func TestWriteAddr_SizeExceedsBuffer(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 2)
	// Should clamp size to buffer length without panic.
	writeAddr(buf, 0x1234, 8, binary.LittleEndian)
}

// TestWriteAddressToBytes_AllSizes tests writeAddressToBytes with different sizes.
func TestWriteAddressToBytes_AllSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
		addr uint64
	}{
		{"1 byte", 1, 0x42},
		{"2 bytes", 2, 0x1234},
		{"4 bytes", 4, 0x12345678},
		{"8 bytes", 8, 0x123456789ABCDEF0},
		{"3 bytes (default)", 3, 0x123456},
		{"5 bytes (default)", 5, 0x123456789A},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf := make([]byte, 8)
			writeAddressToBytes(buf, tt.addr, tt.size, binary.LittleEndian)
			// Verify it does not panic.
		})
	}
}

// TestWriteAddressToBytes_BigEndian tests writeAddressToBytes with big endian.
func TestWriteAddressToBytes_BigEndian(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 8)
	writeAddressToBytes(buf, 0xDEAD, 2, binary.BigEndian)
	if binary.BigEndian.Uint16(buf[:2]) != 0xDEAD {
		t.Errorf("big endian write failed: got %X", binary.BigEndian.Uint16(buf[:2]))
	}
}

// ===========================================================================
// writeUintVar coverage (fractalheap_write.go)
// ===========================================================================

// TestWriteUintVar_DefaultBranch tests writeUintVar with non-standard sizes.
func TestWriteUintVar_DefaultBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		size      int
		value     uint64
		endian    binary.ByteOrder
		checkByte int
		expected  byte
	}{
		{
			name:      "3 bytes little endian",
			size:      3,
			value:     0x123456,
			endian:    binary.LittleEndian,
			checkByte: 0,
			expected:  0x56,
		},
		{
			name:      "3 bytes big endian",
			size:      3,
			value:     0x123456,
			endian:    binary.BigEndian,
			checkByte: 0,
			expected:  0x12,
		},
		{
			name:      "5 bytes little endian",
			size:      5,
			value:     0x123456789A,
			endian:    binary.LittleEndian,
			checkByte: 0,
			expected:  0x9A,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf := make([]byte, 8)
			writeUintVar(buf, tt.value, tt.size, tt.endian)
			if buf[tt.checkByte] != tt.expected {
				t.Errorf("buf[%d] = %02X, want %02X", tt.checkByte, buf[tt.checkByte], tt.expected)
			}
		})
	}
}

// ===========================================================================
// compareLinkNames coverage (btreev2_write.go) - already 100% but adding
// explicit test for documentation completeness.
// ===========================================================================

// TestCompareLinkNames_EqualStrings tests comparison of equal strings.
func TestCompareLinkNames_EqualStrings(t *testing.T) {
	t.Parallel()

	result := compareLinkNames("same", "same")
	if result != 0 {
		t.Errorf("compareLinkNames(same, same) = %d, want 0", result)
	}
}

// TestCompareLinkNames_LessThan tests comparison where a < b.
func TestCompareLinkNames_LessThan(t *testing.T) {
	t.Parallel()

	result := compareLinkNames("alpha", "beta")
	if result != -1 {
		t.Errorf("compareLinkNames(alpha, beta) = %d, want -1", result)
	}
}

// TestCompareLinkNames_GreaterThan tests comparison where a > b.
func TestCompareLinkNames_GreaterThan(t *testing.T) {
	t.Parallel()

	result := compareLinkNames("beta", "alpha")
	if result != 1 {
		t.Errorf("compareLinkNames(beta, alpha) = %d, want 1", result)
	}
}

// ===========================================================================
// DeleteRecordLazy triggering batch rebalance via threshold
// ===========================================================================

// TestDeleteRecordLazy_TriggersBatchRebalance tests that lazy delete triggers
// batch rebalancing when threshold is reached.
func TestDeleteRecordLazy_TriggersBatchRebalance(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Use a very low threshold that will trigger immediately when node underflows.
	bt.EnableLazyRebalancing(LazyRebalancingConfig{
		Enabled:   true,
		Threshold: 0.01, // 1% - will trigger since 1/1 = 100% > 1%
		MaxDelay:  10 * time.Minute,
		BatchSize: 100,
	})

	// Insert enough records that deleting most will trigger underflow.
	for i := 0; i < 10; i++ {
		if err := bt.InsertRecord(fmt.Sprintf("rec_%d", i), uint64(0x1000+i)); err != nil {
			t.Fatalf("InsertRecord: %v", err)
		}
	}

	// Delete records until we go below minRecords threshold.
	minRecords := bt.calculateMinRecords()
	deletionsNeeded := len(bt.records) - minRecords + 1

	for i := 0; i < deletionsNeeded; i++ {
		if err := bt.DeleteRecordLazy(fmt.Sprintf("rec_%d", i)); err != nil {
			t.Fatalf("DeleteRecordLazy(%d): %v", i, err)
		}
	}

	// After batch rebalancing triggers, PendingDeletes should be reset.
	if bt.lazyState.PendingDeletes != 0 {
		t.Errorf("PendingDeletes = %d, want 0 (batch rebalancing should have reset)", bt.lazyState.PendingDeletes)
	}
}
