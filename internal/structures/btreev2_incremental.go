// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"fmt"
	"sync"
	"time"
)

// Incremental B-tree v2 rebalancing - Zero user-wait performance for TB-scale data.
//
// Problem: Even lazy rebalancing has a pause when threshold reached.
//   - 100GB file with lazy: 10 seconds pause (better than 16 minutes, but still noticeable)
//   - TB-scale files: Minutes of pause even with batching
//   - User perception: "Why did the app freeze?"
//
// Solution: Incremental rebalancing in background goroutine.
//   - Accumulate deletions (lazy mode)
//   - Process rebalancing in small time slices (100ms every 5 seconds)
//   - Result: Zero user-visible pause, work happens in background
//
// References:
//   - docs/dev/BTREE_PERFORMANCE_ANALYSIS.md lines 397-446
//   - Background processing pattern (common in databases, GC, etc.)
//
// Usage:
//
//	config := IncrementalRebalancingConfig{
//	    Enabled:  true,
//	    Budget:   100 * time.Millisecond,  // Process 100ms per tick
//	    Interval: 5 * time.Second,         // Every 5 seconds
//	}
//	btree.EnableIncrementalRebalancing(config)
//	// Deletions happen instantly, rebalancing in background!

// IncrementalRebalancingConfig configures incremental rebalancing behavior.
type IncrementalRebalancingConfig struct {
	// Enabled enables incremental rebalancing mode
	Enabled bool

	// Budget is the time budget per rebalancing session.
	// The goroutine will rebalance for this duration, then pause.
	// Smaller = less CPU impact, Larger = faster rebalancing
	// Default: 100 * time.Millisecond
	Budget time.Duration

	// Interval is how often to run rebalancing sessions.
	// Smaller = more frequent rebalancing, Larger = more batching
	// Default: 5 * time.Second
	Interval time.Duration

	// ProgressCallback is called after each rebalancing session with progress info.
	// Optional: Can be nil for no progress reporting
	ProgressCallback func(progress RebalancingProgress)
}

// DefaultIncrementalConfig returns the recommended incremental rebalancing configuration.
//
// These defaults are tuned for scientific data (TB-scale):
//   - 100ms budget: Barely noticeable CPU spike
//   - 5 second interval: Good balance between responsiveness and batching
//   - No callback: User can add if needed
//
// For custom tuning, see docs/guides/PERFORMANCE.md.
func DefaultIncrementalConfig() IncrementalRebalancingConfig {
	return IncrementalRebalancingConfig{
		Enabled:          true,
		Budget:           100 * time.Millisecond,
		Interval:         5 * time.Second,
		ProgressCallback: nil,
	}
}

// RebalancingProgress contains progress information about incremental rebalancing.
type RebalancingProgress struct {
	// NodesRebalanced is the total number of nodes rebalanced so far
	NodesRebalanced int

	// NodesRemaining is the number of underflow nodes still pending
	NodesRemaining int

	// SessionDuration is how long the last rebalancing session took
	SessionDuration time.Duration

	// EstimatedRemaining is the estimated time to complete all pending rebalancing
	EstimatedRemaining time.Duration

	// IsComplete is true when all pending rebalancing is done
	IsComplete bool
}

// IncrementalRebalancer manages background incremental rebalancing.
type IncrementalRebalancer struct {
	btree  *WritableBTreeV2
	config IncrementalRebalancingConfig

	// State tracking
	mu               sync.Mutex
	nodesRebalanced  int
	running          bool
	stopChan         chan struct{}
	stoppedChan      chan struct{}
	lastSessionTime  time.Duration
	estimatedTimeETA time.Duration
}

// EnableIncrementalRebalancing enables incremental rebalancing mode on a B-tree.
//
// This starts a background goroutine that periodically rebalances underflow nodes.
// The goroutine respects time budgets to avoid blocking user operations.
//
// **IMPORTANT: Resource management**
//   - Background goroutine runs until StopIncrementalRebalancing() called
//   - ALWAYS call Stop() before closing file (or use defer)
//   - Goroutine will leak if not stopped!
//
// Parameters:
//   - config: incremental rebalancing configuration
//
// Returns:
//   - error: if already enabled or lazy rebalancing not enabled
//
// Example:
//
//	config := DefaultIncrementalConfig()
//	config.ProgressCallback = func(p RebalancingProgress) {
//	    fmt.Printf("Rebalanced: %d, Remaining: %d, ETA: %v\n",
//	        p.NodesRebalanced, p.NodesRemaining, p.EstimatedRemaining)
//	}
//	btree.EnableIncrementalRebalancing(config)
//	defer btree.StopIncrementalRebalancing()  // CRITICAL: Stop goroutine!
func (bt *WritableBTreeV2) EnableIncrementalRebalancing(config IncrementalRebalancingConfig) error {
	// Check if lazy rebalancing enabled (prerequisite)
	if !bt.IsLazyRebalancingEnabled() {
		return fmt.Errorf("lazy rebalancing must be enabled first (prerequisite for incremental)")
	}

	// Check if already running
	if bt.incrementalRebalancer != nil && bt.incrementalRebalancer.running {
		return fmt.Errorf("incremental rebalancing already enabled")
	}

	// Validate configuration
	if config.Budget <= 0 {
		config.Budget = 100 * time.Millisecond // Default
	}
	if config.Interval <= 0 {
		config.Interval = 5 * time.Second // Default
	}

	// Create rebalancer
	bt.incrementalRebalancer = &IncrementalRebalancer{
		btree:       bt,
		config:      config,
		running:     false,
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}

	// Start background goroutine
	bt.incrementalRebalancer.Start()

	return nil
}

// StopIncrementalRebalancing stops the background rebalancing goroutine.
//
// This method:
//  1. Signals the goroutine to stop
//  2. Waits for it to finish current session
//  3. Performs final rebalancing of any remaining nodes
//  4. Cleans up resources
//
// **CRITICAL**: Always call this before closing the file!
//
// Returns:
//   - error: if final rebalancing fails
//
// Example:
//
//	btree.EnableIncrementalRebalancing(config)
//	defer btree.StopIncrementalRebalancing()  // Ensures cleanup
func (bt *WritableBTreeV2) StopIncrementalRebalancing() error {
	if bt.incrementalRebalancer == nil {
		return nil // Not running
	}

	// Stop the goroutine
	bt.incrementalRebalancer.Stop()

	// Perform final rebalancing of any remaining nodes
	if bt.lazyState != nil && len(bt.lazyState.UnderflowNodes) > 0 {
		if err := bt.BatchRebalance(); err != nil {
			return fmt.Errorf("final rebalancing failed: %w", err)
		}
	}

	// Clean up
	bt.incrementalRebalancer = nil

	return nil
}

// IsIncrementalRebalancingEnabled checks if incremental rebalancing is active.
func (bt *WritableBTreeV2) IsIncrementalRebalancingEnabled() bool {
	return bt.incrementalRebalancer != nil && bt.incrementalRebalancer.running
}

// GetIncrementalRebalancingProgress returns current progress information.
//
// Returns:
//   - progress: current rebalancing progress
//   - error: if incremental rebalancing not enabled
func (bt *WritableBTreeV2) GetIncrementalRebalancingProgress() (RebalancingProgress, error) {
	if bt.incrementalRebalancer == nil {
		return RebalancingProgress{}, fmt.Errorf("incremental rebalancing not enabled")
	}

	return bt.incrementalRebalancer.GetProgress(), nil
}

// Start starts the background rebalancing goroutine.
func (ir *IncrementalRebalancer) Start() {
	ir.mu.Lock()
	if ir.running {
		ir.mu.Unlock()
		return
	}
	ir.running = true
	ir.mu.Unlock()

	go ir.rebalancingLoop()
}

// Stop stops the background rebalancing goroutine and waits for it to finish.
func (ir *IncrementalRebalancer) Stop() {
	ir.mu.Lock()
	if !ir.running {
		ir.mu.Unlock()
		return
	}
	ir.mu.Unlock()

	// Signal stop
	close(ir.stopChan)

	// Wait for goroutine to finish
	<-ir.stoppedChan
}

// rebalancingLoop is the main background loop that performs incremental rebalancing.
func (ir *IncrementalRebalancer) rebalancingLoop() {
	defer close(ir.stoppedChan)

	ticker := time.NewTicker(ir.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Perform one rebalancing session
			ir.rebalanceIncremental()

		case <-ir.stopChan:
			// Stop requested
			ir.mu.Lock()
			ir.running = false
			ir.mu.Unlock()
			return
		}
	}
}

// rebalanceIncremental performs one incremental rebalancing session.
//
// This processes underflow nodes until the time budget is exhausted.
// Then it pauses until the next interval.
func (ir *IncrementalRebalancer) rebalanceIncremental() {
	start := time.Now()

	// Check if there's work to do
	if ir.btree.lazyState == nil || len(ir.btree.lazyState.UnderflowNodes) == 0 {
		// No work, skip this session
		return
	}

	// Track nodes rebalanced in this session
	sessionNodesRebalanced := 0

	// Rebalance until time budget exhausted or no more work
	for time.Since(start) < ir.config.Budget && len(ir.btree.lazyState.UnderflowNodes) > 0 {
		// For MVP (single-leaf B-tree): Rebalancing is a no-op
		// Just remove from underflow list
		// Future (multi-level trees): Actually rebalance the node

		// Get next underflow node
		// nodeAddr := ir.btree.lazyState.UnderflowNodes[0]
		ir.btree.lazyState.UnderflowNodes = ir.btree.lazyState.UnderflowNodes[1:]

		// Rebalance the node (MVP: no-op, future: actual rebalancing)
		// err := ir.btree.rebalanceNode(nodeAddr)
		// if err != nil {
		//     // Log error, but continue (don't fail entire session)
		//     continue
		// }

		sessionNodesRebalanced++
		ir.mu.Lock()
		ir.nodesRebalanced++
		ir.mu.Unlock()
	}

	// Update session stats
	sessionDuration := time.Since(start)
	ir.mu.Lock()
	ir.lastSessionTime = sessionDuration
	ir.mu.Unlock()

	// Estimate remaining time (if we have nodes left)
	var eta time.Duration
	if len(ir.btree.lazyState.UnderflowNodes) > 0 && sessionNodesRebalanced > 0 {
		// ETA = (remaining nodes / nodes per session) * (session time + interval)
		nodesPerSession := sessionNodesRebalanced
		remainingNodes := len(ir.btree.lazyState.UnderflowNodes)
		sessionsRemaining := (remainingNodes + nodesPerSession - 1) / nodesPerSession
		eta = time.Duration(sessionsRemaining) * (sessionDuration + ir.config.Interval)
	}

	ir.mu.Lock()
	ir.estimatedTimeETA = eta
	ir.mu.Unlock()

	// Report progress (if callback provided)
	if ir.config.ProgressCallback != nil {
		progress := ir.GetProgress()
		ir.config.ProgressCallback(progress)
	}

	// If all done, reset stats
	if len(ir.btree.lazyState.UnderflowNodes) == 0 {
		ir.btree.lazyState.UnderflowCount = 0
		ir.btree.lazyState.PendingDeletes = 0
		ir.btree.lazyState.LastRebalance = time.Now()
	}
}

// GetProgress returns current rebalancing progress.
func (ir *IncrementalRebalancer) GetProgress() RebalancingProgress {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	var nodesRemaining int
	if ir.btree.lazyState != nil {
		nodesRemaining = len(ir.btree.lazyState.UnderflowNodes)
	}

	return RebalancingProgress{
		NodesRebalanced:    ir.nodesRebalanced,
		NodesRemaining:     nodesRemaining,
		SessionDuration:    ir.lastSessionTime,
		EstimatedRemaining: ir.estimatedTimeETA,
		IsComplete:         nodesRemaining == 0,
	}
}
