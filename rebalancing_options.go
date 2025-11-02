// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5

import (
	"time"

	"github.com/scigolib/hdf5/internal/structures"
)

// FileWriterOption configures a FileWriter during creation.
// This follows the Functional Options Pattern (Go standard 2025).
//
// Example:
//
//	fw := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
//	    hdf5.WithLazyRebalancing(
//	        hdf5.LazyThreshold(0.05),
//	    ),
//	)
type FileWriterOption func(*FileWriter) error

// ============================================
// Lazy Rebalancing Options
// ============================================

// WithLazyRebalancing enables lazy (batch) rebalancing mode.
//
// Lazy rebalancing accumulates deletions and triggers batch rebalancing
// when a threshold is reached. This is 10-100x faster than immediate
// rebalancing for deletion-heavy workloads.
//
// Default configuration if no options provided:
//   - Threshold: 0.05 (5% underflow)
//   - MaxDelay: 5 minutes
//   - BatchSize: 100 nodes
//
// Example:
//
//	fw := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
//	    hdf5.WithLazyRebalancing(
//	        hdf5.LazyThreshold(0.05),
//	        hdf5.LazyMaxDelay(5*time.Minute),
//	    ),
//	)
//
// Reference: docs/dev/BTREE_PERFORMANCE_ANALYSIS.md.
func WithLazyRebalancing(opts ...LazyOption) FileWriterOption {
	return func(fw *FileWriter) error {
		// Start with default config
		config := structures.DefaultLazyConfig()

		// Apply user options
		for _, opt := range opts {
			opt(&config)
		}

		// Enable lazy rebalancing on FileWriter
		// Note: This is a placeholder - actual enabling happens
		// when B-tree is created (in dataset operations)
		fw.lazyRebalancingConfig = &config

		return nil
	}
}

// LazyOption configures lazy rebalancing behavior.
type LazyOption func(*structures.LazyRebalancingConfig)

// LazyThreshold sets the underflow threshold for triggering batch rebalancing.
//
// The threshold is a ratio of underflow nodes to total nodes.
// When (underflow_nodes / total_nodes) >= threshold, batch rebalancing triggers.
//
// Range: 0.01 (1%) to 0.20 (20%)
// Default: 0.05 (5%)
//
// Example:
//
//	hdf5.LazyThreshold(0.10)  // Trigger at 10% underflow
func LazyThreshold(threshold float64) LazyOption {
	return func(c *structures.LazyRebalancingConfig) {
		c.Threshold = threshold
	}
}

// LazyMaxDelay sets the maximum time before forcing batch rebalancing.
//
// Even if the threshold is not reached, rebalancing will trigger after
// this duration. This prevents indefinite delay in write-only workloads.
//
// Default: 5 minutes
//
// Example:
//
//	hdf5.LazyMaxDelay(10*time.Minute)  // Force rebalance after 10 min
func LazyMaxDelay(delay time.Duration) LazyOption {
	return func(c *structures.LazyRebalancingConfig) {
		c.MaxDelay = delay
	}
}

// LazyBatchSize sets the number of nodes to rebalance per batch operation.
//
// Larger batches = more work per rebalancing, but fewer total operations.
//
// Default: 100 nodes
//
// Example:
//
//	hdf5.LazyBatchSize(200)  // Process 200 nodes per batch
func LazyBatchSize(size int) LazyOption {
	return func(c *structures.LazyRebalancingConfig) {
		c.BatchSize = size
	}
}

// ============================================
// Incremental Rebalancing Options
// ============================================

// WithIncrementalRebalancing enables incremental (background) rebalancing mode.
//
// Incremental rebalancing processes underflow nodes in the background using
// a goroutine with time budgets. This provides ZERO user-visible pause for
// TB-scale scientific data.
//
// IMPORTANT: Requires lazy rebalancing to be enabled first (prerequisite).
//
// Default configuration if no options provided:
//   - Budget: 100ms per session
//   - Interval: 5 seconds between sessions
//   - ProgressCallback: nil
//
// Example:
//
//	fw := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
//	    hdf5.WithLazyRebalancing(),  // Prerequisite!
//	    hdf5.WithIncrementalRebalancing(
//	        hdf5.IncrementalBudget(100*time.Millisecond),
//	        hdf5.IncrementalInterval(5*time.Second),
//	    ),
//	)
//	defer fw.Close()  // Automatically stops background goroutine
//
// Reference: docs/dev/BTREE_PERFORMANCE_ANALYSIS.md lines 397-446.
func WithIncrementalRebalancing(opts ...IncrementalOption) FileWriterOption {
	return func(fw *FileWriter) error {
		// Start with default config
		config := structures.DefaultIncrementalConfig()

		// Apply user options
		for _, opt := range opts {
			opt(&config)
		}

		// Enable incremental rebalancing on FileWriter
		fw.incrementalRebalancingConfig = &config

		return nil
	}
}

// IncrementalOption configures incremental rebalancing behavior.
type IncrementalOption func(*structures.IncrementalRebalancingConfig)

// IncrementalBudget sets the time budget per rebalancing session.
//
// The background goroutine will rebalance for this duration, then pause.
//
// Smaller = less CPU impact, Larger = faster rebalancing
// Default: 100ms
//
// Example:
//
//	hdf5.IncrementalBudget(200*time.Millisecond)  // 200ms per session
func IncrementalBudget(budget time.Duration) IncrementalOption {
	return func(c *structures.IncrementalRebalancingConfig) {
		c.Budget = budget
	}
}

// IncrementalInterval sets how often to run rebalancing sessions.
//
// Smaller = more frequent rebalancing, Larger = more batching
// Default: 5 seconds
//
// Example:
//
//	hdf5.IncrementalInterval(10*time.Second)  // Every 10 seconds
func IncrementalInterval(interval time.Duration) IncrementalOption {
	return func(c *structures.IncrementalRebalancingConfig) {
		c.Interval = interval
	}
}

// IncrementalProgressCallback sets a callback for progress updates.
//
// The callback is called after each rebalancing session with progress info.
// Optional: Can be nil for no progress reporting.
//
// Example:
//
//	hdf5.IncrementalProgressCallback(func(p structures.RebalancingProgress) {
//	    fmt.Printf("Rebalanced: %d, Remaining: %d, ETA: %v\n",
//	        p.NodesRebalanced, p.NodesRemaining, p.EstimatedRemaining)
//	})
func IncrementalProgressCallback(callback func(structures.RebalancingProgress)) IncrementalOption {
	return func(c *structures.IncrementalRebalancingConfig) {
		c.ProgressCallback = callback
	}
}

// ============================================
// Smart Rebalancing Options (Phase 3!)
// ============================================

// WithSmartRebalancing enables smart (auto-tuning) rebalancing mode.
//
// Smart rebalancing automatically detects workload patterns and selects
// the optimal rebalancing mode (none, lazy, or incremental) based on:
//   - File size
//   - Operation patterns (delete ratio, batch size)
//   - Resource constraints (CPU, memory limits)
//
// This is the "auto-pilot" mode for scientific data workflows.
//
// IMPORTANT: This is OPTIONAL and must be explicitly enabled.
// By default (no options), NO rebalancing is performed (like C library).
//
// Example:
//
//	fw := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
//	    hdf5.WithSmartRebalancing(
//	        hdf5.SmartAutoDetect(true),
//	        hdf5.SmartAutoSwitch(true),
//	        hdf5.SmartAllowedModes("lazy", "incremental"),
//	    ),
//	)
//
// Reference: Phase 3 design (2025 best practices).
func WithSmartRebalancing(opts ...SmartOption) FileWriterOption {
	return func(_ *FileWriter) error {
		// Smart rebalancing configuration will be implemented in Phase 3.
		// For now, this is a placeholder showing the API design.
		//
		// Implementation plan:
		// config := NewSmartRebalancingConfig()
		// for _, opt := range opts {
		//     opt(config)
		// }
		// fw.smartRebalancingConfig = config

		_ = opts // Mark as used for now
		return nil
	}
}

// SmartOption configures smart rebalancing behavior.
type SmartOption func(*SmartRebalancingConfig)

// SmartRebalancingConfig configures smart (auto-tuning) rebalancing.
//
// This will be fully implemented in Phase 3.
type SmartRebalancingConfig struct {
	// Auto-detection settings
	AutoDetect bool // Detect workload patterns automatically
	AutoSwitch bool // Automatically switch between modes

	// Constraints
	MinFileSize   uint64   // Minimum file size for auto-rebalancing
	AllowedModes  []string // Allowed rebalancing modes
	MaxCPUPercent int      // Maximum CPU usage percentage

	// Callbacks
	OnModeChange func(decision ModeDecision) // Called when mode changes

	// Future fields (Phase 3 implementation):
	// - Safety constraints (CPU/memory limits)
	// - Adaptive optimizer settings (learning rate, etc.)
	// - Metrics configuration (Prometheus-style)
}

// ModeDecision explains why a rebalancing mode was selected.
//
// This provides explainability for auto-tuning decisions.
type ModeDecision struct {
	SelectedMode string             // Mode selected ("none", "lazy", "incremental")
	Reason       string             // Human-readable reason
	Confidence   float64            // Confidence level [0, 1]
	Factors      map[string]float64 // Factors that influenced decision
	Timestamp    time.Time          // When decision was made
}

// SmartAutoDetect enables automatic workload pattern detection.
func SmartAutoDetect(enabled bool) SmartOption {
	return func(c *SmartRebalancingConfig) {
		c.AutoDetect = enabled
	}
}

// SmartAutoSwitch enables automatic mode switching based on detected patterns.
func SmartAutoSwitch(enabled bool) SmartOption {
	return func(c *SmartRebalancingConfig) {
		c.AutoSwitch = enabled
	}
}

// SmartMinFileSize sets the minimum file size for enabling auto-rebalancing.
//
// Files smaller than this size will not trigger automatic rebalancing.
func SmartMinFileSize(size uint64) SmartOption {
	return func(c *SmartRebalancingConfig) {
		c.MinFileSize = size
	}
}

// SmartAllowedModes restricts which rebalancing modes can be auto-selected.
//
// Modes: "none", "lazy", "incremental"
//
// Example:
//
//	hdf5.SmartAllowedModes("lazy", "incremental")  // Don't use "none"
func SmartAllowedModes(modes ...string) SmartOption {
	return func(c *SmartRebalancingConfig) {
		c.AllowedModes = modes
	}
}

// SmartOnModeChange sets a callback for mode change notifications.
//
// The callback receives a ModeDecision explaining the change.
//
// Example:
//
//	hdf5.SmartOnModeChange(func(d hdf5.ModeDecision) {
//	    log.Printf("Mode: %s (confidence: %.2f%%)", d.SelectedMode, d.Confidence*100)
//	    log.Printf("Reason: %s", d.Reason)
//	})
func SmartOnModeChange(callback func(ModeDecision)) SmartOption {
	return func(c *SmartRebalancingConfig) {
		c.OnModeChange = callback
	}
}

// ============================================
// Constants for smart rebalancing
// ============================================

const (
	// KB represents kilobyte size for smart configuration.
	KB = 1024
	// MB represents megabyte size for smart configuration.
	MB = 1024 * KB
	// GB represents gigabyte size for smart configuration.
	GB = 1024 * MB
)
