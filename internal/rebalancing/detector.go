// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

// Package rebalancing provides intelligent B-tree rebalancing strategies for HDF5 files.
//
// This package implements workload detection, adaptive configuration selection,
// and smart rebalancing algorithms to optimize performance for scientific data operations.
package rebalancing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Workload Detection System for HDF5 B-tree Rebalancing Optimization.
//
// This system analyzes file operation patterns to inform intelligent rebalancing decisions.
// By detecting batch deletions, frequent writes, and other workload characteristics,
// we can automatically tune rebalancing strategies for optimal performance.
//
// Design Principles (DDD + Go Best Practices 2025):
//   - Rich domain models with behavior (not anemic data structures)
//   - Thread-safe (concurrent operation recording)
//   - Memory-bounded (ring buffer with configurable capacity)
//   - Performance-critical (O(1) recording, efficient feature extraction)
//   - Testable (clock interface for deterministic tests)
//
// Architecture:
//   - WorkloadDetector: Main component, tracks operations in sliding window
//   - WorkloadFeatures: Immutable value object with extracted metrics
//   - WorkloadType: Classification result (BatchDeletion, MixedRW, etc.)
//   - OperationEvent: Individual operation record
//
// Usage:
//
//	detector := NewWorkloadDetector(
//	    WithWindowSize(5*time.Minute),
//	    WithMinSampleSize(10),
//	    WithCapacity(10000),
//	)
//	defer detector.Close()
//
//	// Record operations
//	detector.RecordOperation(context.Background(), OpDelete, fileSize)
//	detector.RecordOperation(context.Background(), OpWrite, fileSize)
//
//	// Extract features
//	features := detector.ExtractFeatures()
//	workloadType := detector.DetectWorkloadType()
//
//	// Use for decision making
//	if workloadType == WorkloadBatchDeletion {
//	    // Enable lazy rebalancing
//	}
//
// References:
//   - Phase 3: Smart Rebalancing API design (2025 best practices)
//   - λ-Tune/Centrum auto-tuning research (observability, safety)
//   - docs/dev/STATUS.md - Current project status

// OperationType represents the type of file operation.
type OperationType int

const (
	// OpRead represents a read operation.
	OpRead OperationType = iota
	// OpWrite represents a write operation.
	OpWrite
	// OpDelete represents a delete operation.
	OpDelete
)

const unknownOpType = "Unknown"

// String returns the string representation of OperationType.
func (o OperationType) String() string {
	switch o {
	case OpRead:
		return "Read"
	case OpWrite:
		return "Write"
	case OpDelete:
		return "Delete"
	default:
		return unknownOpType
	}
}

// OperationEvent represents a single operation event in the sliding window.
type OperationEvent struct {
	Type      OperationType // Type of operation
	Timestamp time.Time     // When the operation occurred
	FileSize  uint64        // File size at time of operation (bytes)
}

// WorkloadType represents the classified workload pattern.
type WorkloadType int

const (
	// WorkloadUnknown indicates insufficient data or unclassified pattern.
	WorkloadUnknown WorkloadType = iota
	// WorkloadBatchDeletion indicates high delete ratio with burst pattern.
	WorkloadBatchDeletion
	// WorkloadFrequentWrites indicates continuous high write ratio.
	WorkloadFrequentWrites
	// WorkloadMixedRW indicates balanced read/write operations.
	WorkloadMixedRW
	// WorkloadReadHeavy indicates mostly read operations.
	WorkloadReadHeavy
	// WorkloadAppendOnly indicates only writes, no deletes.
	WorkloadAppendOnly
)

// String returns the string representation of WorkloadType.
func (w WorkloadType) String() string {
	switch w {
	case WorkloadUnknown:
		return unknownOpType
	case WorkloadBatchDeletion:
		return "BatchDeletion"
	case WorkloadFrequentWrites:
		return "FrequentWrites"
	case WorkloadMixedRW:
		return "MixedRW"
	case WorkloadReadHeavy:
		return "ReadHeavy"
	case WorkloadAppendOnly:
		return "AppendOnly"
	default:
		return unknownOpType
	}
}

// WorkloadFeatures represents extracted features from operation history.
//
// This is an immutable value object containing metrics used for workload classification
// and rebalancing strategy selection.
type WorkloadFeatures struct {
	// Operation ratios (all in range [0, 1])
	DeleteRatio float64 // Deletions / Total operations
	WriteRatio  float64 // Writes / Total operations
	ReadRatio   float64 // Reads / Total operations

	// Performance metrics
	OperationRate float64 // Operations per second

	// Pattern detection
	BurstDetected bool // True if burst pattern detected

	// Context
	FileSize       uint64        // Current file size (bytes)
	WindowDuration time.Duration // Analysis window duration
	SampleSize     int           // Number of operations analyzed

	// Timestamp
	ExtractedAt time.Time // When features were extracted
}

// IsValid checks if features have sufficient data for classification.
func (f WorkloadFeatures) IsValid() bool {
	return f.SampleSize > 0
}

// String returns a human-readable representation of WorkloadFeatures.
func (f WorkloadFeatures) String() string {
	return fmt.Sprintf("WorkloadFeatures{Delete: %.2f, Write: %.2f, Read: %.2f, "+
		"Rate: %.2f ops/s, Burst: %v, FileSize: %d bytes, Samples: %d}",
		f.DeleteRatio, f.WriteRatio, f.ReadRatio,
		f.OperationRate, f.BurstDetected, f.FileSize, f.SampleSize)
}

// Clock is an interface for time operations (allows mocking in tests).
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using actual system time.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// WorkloadDetector analyzes operation patterns in a sliding time window.
//
// This is the main component of the workload detection system.
// It maintains a ring buffer of recent operations and extracts features on demand.
//
// Thread Safety:
//   - All public methods are thread-safe
//   - Uses mutex to protect internal state
//   - Safe for concurrent RecordOperation calls
//
// Memory Management:
//   - Ring buffer with fixed capacity (default: 10000 operations)
//   - Automatic eviction of old operations
//   - No unbounded growth
//
// Performance:
//   - O(1) operation recording (ring buffer insert)
//   - O(n) feature extraction where n = operations in window
//   - O(1) eviction of old operations
type WorkloadDetector struct {
	// Configuration (immutable after creation)
	windowSize    time.Duration // Sliding window duration
	minSampleSize int           // Minimum samples for valid classification
	capacity      int           // Ring buffer capacity

	// State (protected by mutex)
	mu        sync.RWMutex
	events    []OperationEvent // Ring buffer of events
	head      int              // Ring buffer head index
	size      int              // Current number of events
	closed    bool             // True if detector is closed
	lastFlush time.Time        // Last time old events were flushed

	// Dependencies
	clock Clock // Time source (mockable for tests)
}

// DetectorOption is a functional option for configuring WorkloadDetector.
type DetectorOption func(*WorkloadDetector)

// WithWindowSize sets the sliding window duration.
//
// This determines how far back in time to look when analyzing operations.
// Larger windows provide more stable classifications but slower adaptation.
//
// Default: 5 minutes.
// Recommended: 1-10 minutes for scientific workloads.
func WithWindowSize(duration time.Duration) DetectorOption {
	return func(d *WorkloadDetector) {
		if duration > 0 {
			d.windowSize = duration
		}
	}
}

// WithMinSampleSize sets the minimum number of operations required for valid classification.
//
// This prevents unreliable classifications from too little data.
//
// Default: 10 operations.
// Recommended: 5-50 depending on workload variability.
func WithMinSampleSize(size int) DetectorOption {
	return func(d *WorkloadDetector) {
		if size > 0 {
			d.minSampleSize = size
		}
	}
}

// WithCapacity sets the ring buffer capacity.
//
// This is the maximum number of operations to keep in memory.
// Larger capacity = more historical data but more memory usage.
//
// Default: 10000 operations.
// Recommended: 1000-100000 depending on operation rate.
func WithCapacity(capacity int) DetectorOption {
	return func(d *WorkloadDetector) {
		if capacity > 0 {
			d.capacity = capacity
		}
	}
}

// WithClock sets a custom clock (for testing).
func WithClock(clock Clock) DetectorOption {
	return func(d *WorkloadDetector) {
		if clock != nil {
			d.clock = clock
		}
	}
}

// NewWorkloadDetector creates a new workload detector with the given options.
//
// Default configuration:
//   - Window size: 5 minutes
//   - Min sample size: 10 operations
//   - Capacity: 10000 operations
//   - Clock: System time
//
// Example:
//
//	detector := NewWorkloadDetector(
//	    WithWindowSize(5*time.Minute),
//	    WithMinSampleSize(10),
//	    WithCapacity(10000),
//	)
//	defer detector.Close()
func NewWorkloadDetector(options ...DetectorOption) *WorkloadDetector {
	// Default configuration
	d := &WorkloadDetector{
		windowSize:    5 * time.Minute,
		minSampleSize: 10,
		capacity:      10000,
		clock:         RealClock{},
	}

	// Apply options
	for _, opt := range options {
		opt(d)
	}

	// Initialize ring buffer
	d.events = make([]OperationEvent, d.capacity)
	d.head = 0
	d.size = 0
	d.lastFlush = d.clock.Now()

	return d
}

// RecordOperation records a new operation event.
//
// This method is thread-safe and can be called concurrently from multiple goroutines.
// Operations are recorded with the current timestamp and stored in a ring buffer.
//
// Parameters:
//   - ctx: Context for cancellation (checked before recording)
//   - opType: Type of operation (Read, Write, Delete)
//   - fileSize: Current file size in bytes
//
// Returns:
//   - error: If detector is closed or context is canceled
//
// Performance: O(1) time complexity
//
// Example:
//
//	if err := detector.RecordOperation(ctx, OpDelete, 1024*1024); err != nil {
//	    // Handle error
//	}
func (d *WorkloadDetector) RecordOperation(ctx context.Context, opType OperationType, fileSize uint64) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if closed
	if d.closed {
		return fmt.Errorf("detector is closed")
	}

	// Create event
	event := OperationEvent{
		Type:      opType,
		Timestamp: d.clock.Now(),
		FileSize:  fileSize,
	}

	// Add to ring buffer
	d.events[d.head] = event
	d.head = (d.head + 1) % d.capacity
	if d.size < d.capacity {
		d.size++
	}

	return nil
}

// ExtractFeatures extracts workload features from the sliding window.
//
// This method analyzes operations within the time window and calculates metrics
// like delete ratio, write ratio, operation rate, and burst detection.
//
// Thread Safety: Safe for concurrent calls (uses read lock during analysis)
//
// Performance: O(n) where n = number of operations in window
//
// Returns:
//   - WorkloadFeatures: Extracted features (immutable value object)
//
// Example:
//
//	features := detector.ExtractFeatures()
//	fmt.Printf("Delete ratio: %.2f%%\n", features.DeleteRatio*100)
func (d *WorkloadDetector) ExtractFeatures() WorkloadFeatures {
	d.mu.RLock()
	defer d.mu.RUnlock()

	now := d.clock.Now()
	cutoff := now.Add(-d.windowSize)

	// Count operations in window
	var deletes, writes, reads int
	var firstTimestamp, lastTimestamp time.Time
	var currentFileSize uint64
	validEvents := 0

	// Scan ring buffer for events in window
	for i := 0; i < d.size; i++ {
		// Calculate actual index in ring buffer
		idx := (d.head - d.size + i + d.capacity) % d.capacity
		event := d.events[idx]

		// Skip events outside window
		if event.Timestamp.Before(cutoff) {
			continue
		}

		validEvents++
		currentFileSize = event.FileSize // Keep updating to get latest

		// Track timestamps for rate calculation
		if firstTimestamp.IsZero() || event.Timestamp.Before(firstTimestamp) {
			firstTimestamp = event.Timestamp
		}
		if lastTimestamp.IsZero() || event.Timestamp.After(lastTimestamp) {
			lastTimestamp = event.Timestamp
		}

		// Count by type
		switch event.Type {
		case OpDelete:
			deletes++
		case OpWrite:
			writes++
		case OpRead:
			reads++
		}
	}

	// Calculate ratios
	total := float64(validEvents)
	var deleteRatio, writeRatio, readRatio float64
	if total > 0 {
		deleteRatio = float64(deletes) / total
		writeRatio = float64(writes) / total
		readRatio = float64(reads) / total
	}

	// Calculate operation rate (ops per second)
	var operationRate float64
	if !firstTimestamp.IsZero() && !lastTimestamp.IsZero() {
		duration := lastTimestamp.Sub(firstTimestamp)
		if duration > 0 {
			operationRate = float64(validEvents) / duration.Seconds()
		}
	}

	// Detect burst pattern
	burstDetected := d.detectBurst(validEvents, firstTimestamp, lastTimestamp)

	return WorkloadFeatures{
		DeleteRatio:    deleteRatio,
		WriteRatio:     writeRatio,
		ReadRatio:      readRatio,
		OperationRate:  operationRate,
		BurstDetected:  burstDetected,
		FileSize:       currentFileSize,
		WindowDuration: d.windowSize,
		SampleSize:     validEvents,
		ExtractedAt:    now,
	}
}

// detectBurst detects if operations show a burst pattern.
//
// Burst pattern: Operations concentrated in a short time period, then pause.
// Example: 1000 deletions in 10 seconds, then nothing for 5 minutes.
//
// This is internal and assumes caller holds appropriate lock.
func (d *WorkloadDetector) detectBurst(validEvents int, firstTimestamp, lastTimestamp time.Time) bool {
	// Need sufficient data
	if validEvents < d.minSampleSize {
		return false
	}

	// If operations span less than 20% of window, it's a burst
	if !firstTimestamp.IsZero() && !lastTimestamp.IsZero() {
		actualDuration := lastTimestamp.Sub(firstTimestamp)
		if actualDuration < d.windowSize/5 { // Operations in < 20% of window
			return true
		}
	}

	return false
}

// DetectWorkloadType classifies the current workload based on extracted features.
//
// Classification Rules (checked in order):
//   - BatchDeletion: High delete ratio (>60%) + burst pattern
//   - AppendOnly: Writes only, no deletes (delete ratio < 5%)
//   - FrequentWrites: High write ratio (>60%) + continuous (no burst)
//   - ReadHeavy: High read ratio (>70%)
//   - MixedRW: Balanced read/write, low deletes
//   - Unknown: Insufficient data
//
// Returns:
//   - WorkloadType: Classification result
//
// Example:
//
//	workloadType := detector.DetectWorkloadType()
//	if workloadType == WorkloadBatchDeletion {
//	    // Enable lazy rebalancing for better performance
//	}
func (d *WorkloadDetector) DetectWorkloadType() WorkloadType {
	features := d.ExtractFeatures()

	// Check if we have sufficient data
	if features.SampleSize < d.minSampleSize {
		return WorkloadUnknown
	}

	// Classification rules (tuned for scientific data workloads)
	// Order matters: Check more specific patterns first

	// Rule 1: Batch Deletion (high delete ratio + burst)
	if features.DeleteRatio > 0.6 && features.BurstDetected {
		return WorkloadBatchDeletion
	}

	// Rule 2: Append Only (writes only, no deletes) - Check before FrequentWrites
	if features.WriteRatio > 0.5 && features.DeleteRatio < 0.05 {
		return WorkloadAppendOnly
	}

	// Rule 3: Frequent Writes (high write ratio, continuous)
	if features.WriteRatio > 0.6 && !features.BurstDetected {
		return WorkloadFrequentWrites
	}

	// Rule 4: Read Heavy (mostly reads)
	if features.ReadRatio > 0.7 {
		return WorkloadReadHeavy
	}

	// Rule 5: Mixed Read/Write (balanced, low deletes)
	if features.DeleteRatio < 0.2 {
		return WorkloadMixedRW
	}

	// Default: Unknown (doesn't fit clear pattern)
	return WorkloadUnknown
}

// GetStats returns current detector statistics.
//
// Returns:
//   - totalEvents: Total events in ring buffer
//   - eventsInWindow: Events within time window
//   - windowSize: Current window size
func (d *WorkloadDetector) GetStats() (totalEvents, eventsInWindow int, windowSize time.Duration) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	now := d.clock.Now()
	cutoff := now.Add(-d.windowSize)

	eventsInWindow = 0
	for i := 0; i < d.size; i++ {
		idx := (d.head - d.size + i + d.capacity) % d.capacity
		if d.events[idx].Timestamp.After(cutoff) {
			eventsInWindow++
		}
	}

	return d.size, eventsInWindow, d.windowSize
}

// Close cleans up detector resources.
//
// After closing, RecordOperation will return an error.
// This method is idempotent (safe to call multiple times).
func (d *WorkloadDetector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}

	d.closed = true
	d.events = nil // Help GC

	return nil
}

// IsClosed returns true if detector is closed.
func (d *WorkloadDetector) IsClosed() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.closed
}
