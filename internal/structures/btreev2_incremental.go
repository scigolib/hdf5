// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"fmt"
	"sync"
	"time"

	"github.com/scigolib/hdf5/internal/utils"
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

	// ProgressCallback is called after each rebalancing session with progress info,
	// on the rebalancer's goroutine and with no lock held, so it may call the
	// tree's methods, StopIncrementalRebalancing included.
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
	btree      *WritableBTreeV2
	config     IncrementalRebalancingConfig
	timeSource utils.Clock // the lazy state's clock, taken when the rebalancer is created

	// State tracking
	mu               sync.Mutex
	nodesRebalanced  int
	running          bool
	stopping         bool // stopChan is closed; guards against a second close
	inCallback       bool // the loop is inside ProgressCallback
	stopChan         chan struct{}
	stoppedChan      chan struct{}
	lastSessionTime  time.Duration
	estimatedTimeETA time.Duration
}

// clock returns the rebalancer's time source, or the system clock for a
// rebalancer built without one (EnableIncrementalRebalancing always sets it).
func (ir *IncrementalRebalancer) clock() utils.Clock {
	if ir.timeSource == nil {
		return utils.RealClock{}
	}
	return ir.timeSource
}

// isRunning reports whether the background loop is running. The loop clears
// the flag itself when it exits, so it is read under ir.mu.
func (ir *IncrementalRebalancer) isRunning() bool {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return ir.running
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
	if bt.incrementalRebalancer != nil && bt.incrementalRebalancer.isRunning() {
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
	// The rebalancer reads time from the lazy state's clock (lazy rebalancing
	// is a prerequisite, checked above), so a tree has one time source.
	bt.incrementalRebalancer = &IncrementalRebalancer{
		btree:       bt,
		config:      config,
		timeSource:  bt.lazyClock(),
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
// Called from ProgressCallback, it does not wait for step 2 (that would wait
// for its own goroutine): the loop exits as soon as the callback returns.
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

	// Stop the goroutine. No lock is held while waiting: a session in
	// progress takes bt.lazyMu per node and must be able to finish.
	bt.incrementalRebalancer.Stop()

	// Perform final rebalancing of any remaining nodes
	bt.lazyMu.Lock()
	var err error
	if bt.lazyState != nil && len(bt.lazyState.UnderflowNodes) > 0 {
		err = bt.batchRebalanceLocked()
	}
	bt.lazyMu.Unlock()
	if err != nil {
		return fmt.Errorf("final rebalancing failed: %w", err)
	}

	// Clean up
	bt.incrementalRebalancer = nil

	return nil
}

// IsIncrementalRebalancingEnabled checks if incremental rebalancing is active.
func (bt *WritableBTreeV2) IsIncrementalRebalancingEnabled() bool {
	return bt.incrementalRebalancer != nil && bt.incrementalRebalancer.isRunning()
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

// Stop stops the background rebalancing goroutine.
//
// Guarantee: after Stop returns, the rebalancer reads and writes no tree
// state (lazy state, queued nodes) and runs no further session, so the
// caller may use the tree without any lock against it.
//
// Normally Stop waits for the goroutine to exit. Called while the loop is
// inside ProgressCallback (from the callback itself, or from another
// goroutine at that moment), Stop cannot tell the two apart and must not wait
// for the goroutine it may be running on, so it only signals the loop and
// returns. The guarantee still holds then: a session finishes all its tree
// work before the callback runs, and when the callback returns the loop sees
// the signal and exits without touching the tree again. Only the goroutine's
// exit itself may come after Stop returns. Stop is idempotent and safe to
// call from several goroutines.
func (ir *IncrementalRebalancer) Stop() {
	ir.mu.Lock()
	if !ir.running {
		ir.mu.Unlock()
		return
	}
	// Signal stop
	if !ir.stopping {
		ir.stopping = true
		close(ir.stopChan)
	}
	inCallback := ir.inCallback
	ir.mu.Unlock()

	if inCallback {
		return
	}

	// Wait for goroutine to finish
	<-ir.stoppedChan
}

// rebalancingLoop is the main background loop that performs incremental rebalancing.
func (ir *IncrementalRebalancer) rebalancingLoop() {
	defer close(ir.stoppedChan)

	ticker := ir.clock().NewTicker(ir.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			// A tick and the stop signal can be ready together, and select
			// picks either; a Stop that already returned (see Stop) must not
			// be followed by another session.
			if ir.stopRequested() {
				ir.markStopped()
				return
			}
			ir.rebalanceIncremental()

		case <-ir.stopChan:
			ir.markStopped()
			return
		}
	}
}

// stopRequested reports whether Stop has signaled the loop.
func (ir *IncrementalRebalancer) stopRequested() bool {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return ir.stopping
}

// markStopped records that the loop is exiting. It touches no tree state.
func (ir *IncrementalRebalancer) markStopped() {
	ir.mu.Lock()
	ir.running = false
	ir.mu.Unlock()
}

// rebalanceIncremental performs one incremental rebalancing session.
//
// This processes underflow nodes until the time budget is exhausted.
// Then it pauses until the next interval.
//
// It runs on the rebalancer's goroutine while the tree's owner may call
// DeleteRecordLazy, BatchRebalance and the other lazy-state methods, so every
// access to the lazy state holds bt.lazyMu. The lock is taken per node, not
// for the whole session, so a caller waits at most for one node, and it is
// released before the progress callback, which may call back into the tree.
// Lock order: bt.lazyMu before ir.mu.
func (ir *IncrementalRebalancer) rebalanceIncremental() {
	bt := ir.btree
	start := ir.clock().Now()

	// Check if there's work to do
	bt.lazyMu.Lock()
	hasWork := bt.lazyState != nil && len(bt.lazyState.UnderflowNodes) > 0
	bt.lazyMu.Unlock()
	if !hasWork {
		// No work, skip this session
		return
	}

	// Track nodes rebalanced in this session
	sessionNodesRebalanced := 0

	// Rebalance until time budget exhausted or no more work
	for ir.clock().Now().Sub(start) < ir.config.Budget {
		bt.lazyMu.Lock()
		if bt.lazyState == nil || len(bt.lazyState.UnderflowNodes) == 0 {
			bt.lazyMu.Unlock()
			break
		}
		// For MVP (single-leaf B-tree): Rebalancing is a no-op
		// Just remove from underflow list
		// Future (multi-level trees): Actually rebalance the node
		// (nodeAddr := bt.lazyState.UnderflowNodes[0], then rebalance it).
		bt.lazyState.UnderflowNodes = bt.lazyState.UnderflowNodes[1:]

		sessionNodesRebalanced++
		ir.mu.Lock()
		ir.nodesRebalanced++
		ir.mu.Unlock()
		bt.lazyMu.Unlock()
	}

	// Update session stats
	sessionDuration := ir.clock().Now().Sub(start)

	// All tree work of the session ends here, before the callback: a Stop
	// that returns while the callback runs relies on it (see Stop). If the
	// queue is drained, reset the lazy stats now.
	bt.lazyMu.Lock()
	remainingNodes := 0
	if bt.lazyState != nil {
		remainingNodes = len(bt.lazyState.UnderflowNodes)
		if remainingNodes == 0 {
			bt.lazyState.UnderflowCount = 0
			bt.lazyState.PendingDeletes = 0
			bt.lazyState.LastRebalance = ir.clock().Now()
		}
	}
	bt.lazyMu.Unlock()

	// Estimate remaining time (if we have nodes left)
	var eta time.Duration
	if remainingNodes > 0 && sessionNodesRebalanced > 0 {
		// ETA = (remaining nodes / nodes per session) * (session time + interval)
		nodesPerSession := sessionNodesRebalanced
		sessionsRemaining := (remainingNodes + nodesPerSession - 1) / nodesPerSession
		eta = time.Duration(sessionsRemaining) * (sessionDuration + ir.config.Interval)
	}

	ir.mu.Lock()
	ir.lastSessionTime = sessionDuration
	ir.estimatedTimeETA = eta
	ir.mu.Unlock()

	// Report progress (if callback provided). No lock is held here. The
	// inCallback flag lets Stop return instead of waiting for this goroutine;
	// nothing after the callback touches the tree, and the loop checks the
	// stop signal before the next session.
	if ir.config.ProgressCallback != nil {
		progress := ir.GetProgress()
		ir.mu.Lock()
		ir.inCallback = true
		ir.mu.Unlock()
		ir.config.ProgressCallback(progress)
		ir.mu.Lock()
		ir.inCallback = false
		ir.mu.Unlock()
	}
}

// GetProgress returns current rebalancing progress. It is safe to call from
// any goroutine, the progress callback included; it must not be called with
// bt.lazyMu held (lock order: bt.lazyMu before ir.mu).
func (ir *IncrementalRebalancer) GetProgress() RebalancingProgress {
	var nodesRemaining int
	ir.btree.lazyMu.Lock()
	if ir.btree.lazyState != nil {
		nodesRemaining = len(ir.btree.lazyState.UnderflowNodes)
	}
	ir.btree.lazyMu.Unlock()

	ir.mu.Lock()
	defer ir.mu.Unlock()

	return RebalancingProgress{
		NodesRebalanced:    ir.nodesRebalanced,
		NodesRemaining:     nodesRemaining,
		SessionDuration:    ir.lastSessionTime,
		EstimatedRemaining: ir.estimatedTimeETA,
		IsComplete:         nodesRemaining == 0,
	}
}
