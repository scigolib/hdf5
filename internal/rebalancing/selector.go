// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package rebalancing

import (
	"fmt"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
)

// Config Selector - Decision engine for optimal rebalancing strategy selection.
//
// This component analyzes workload features and selects the best rebalancing strategy
// (lazy, incremental, or none) based on research-backed decision rules.
//
// Design Principles (DDD + Go 2025):
//   - Domain Service: Encapsulates decision logic
//   - Explainability: Every decision has clear reasoning
//   - Safety: Resource constraints prevent bad decisions
//   - Extensibility: Strategy pattern for future ML-based selection
//   - Performance: <1ms decision time (rule-based lookup)
//
// Architecture:
//   - ConfigSelector: Main decision engine
//   - Decision: Immutable result with explainability
//   - SafetyConstraints: Resource limits (CPU, memory, stability)
//   - SelectionStrategy: Pluggable interface (future extensibility)
//
// Usage:
//
//	selector := NewConfigSelector(
//	    WithSafetyConstraints(
//	        MaxCPU(50),
//	        MaxMemory(100*MB),
//	        MinConfidence(0.7),
//	    ),
//	)
//
//	decision := selector.SelectConfig(features, workloadType)
//	fmt.Printf("Mode: %s (confidence: %.1f%%)\n", decision.Mode, decision.Confidence*100)
//	fmt.Printf("Reason: %s\n", decision.Reason)
//
// References:
//   - Phase 3: Smart Rebalancing API design
//   - λ-Tune/Centrum auto-tuning research (safety constraints)
//   - docs/dev/STATUS.md - Current project status

// Mode represents the selected rebalancing mode.
type Mode string

const (
	// ModeNone means no rebalancing (append-only workload or insufficient data).
	ModeNone Mode = "none"
	// ModeLazy means lazy batch rebalancing (batch deletions, small files).
	ModeLazy Mode = "lazy"
	// ModeIncremental means incremental background rebalancing (large files, mixed ops).
	ModeIncremental Mode = "incremental"
)

// String returns the string representation of Mode.
func (m Mode) String() string {
	return string(m)
}

// Decision represents an immutable rebalancing strategy decision.
//
// This value object contains:
//   - The selected configuration (lazy/incremental/none)
//   - Explainability (reason, confidence, factors)
//   - Metadata (timestamp, mode)
//
// Decision is designed for:
//   - Observability: Log decisions for analysis
//   - Debugging: Understand why a mode was selected
//   - Trust: Researchers can verify reasoning
type Decision struct {
	// Config is the selected rebalancing configuration.
	// Type can be:
	//   - *structures.LazyRebalancingConfig
	//   - *structures.IncrementalRebalancingConfig
	//   - nil (no rebalancing)
	Config interface{}

	// Mode is the selected rebalancing mode.
	Mode Mode

	// Reason is a human-readable explanation of why this mode was selected.
	// Example: "Large file (500MB) with moderate deletes (25%) - background processing recommended"
	Reason string

	// Confidence is the confidence level of this decision [0, 1].
	// Higher confidence = stronger signal from features.
	// < 0.7 = uncertain, 0.7-0.85 = moderate, > 0.85 = high confidence
	Confidence float64

	// Factors contains the factors that influenced this decision.
	// Key: factor name, Value: normalized weight [0, 1]
	// Example: {"file_size": 0.8, "delete_ratio": 0.6, "burst_pattern": 1.0}
	Factors map[string]float64

	// Timestamp is when this decision was made.
	Timestamp time.Time
}

// String returns a human-readable representation of the decision.
func (d Decision) String() string {
	return fmt.Sprintf("Decision{Mode: %s, Confidence: %.2f, Reason: %s}",
		d.Mode, d.Confidence, d.Reason)
}

// SafetyConstraints defines resource limits and safety rules for rebalancing.
//
// These constraints prevent:
//   - CPU exhaustion (MaxCPUPercent)
//   - Memory exhaustion (MaxMemoryMB)
//   - Mode flapping (MinStabilityPeriod)
//   - Low-confidence decisions (MinConfidence)
//
// Design: Immutable value object, validated at creation.
type SafetyConstraints struct {
	// MaxCPUPercent is the maximum CPU usage allowed (percentage).
	// Example: 50 = allow up to 50% CPU for rebalancing
	// Range: 1-100
	// Default: 50
	MaxCPUPercent int

	// MaxMemoryMB is the maximum memory allowed for rebalancing (megabytes).
	// Example: 100 = allow up to 100MB for rebalancing buffers
	// Range: 1-unlimited
	// Default: 100
	MaxMemoryMB uint64

	// MinStabilityPeriod is the minimum time between mode changes.
	// This prevents rapid mode switching ("mode flapping").
	// Example: 30s = don't change mode more often than every 30 seconds
	// Default: 30 * time.Second
	MinStabilityPeriod time.Duration

	// MinConfidence is the minimum confidence required to enable rebalancing.
	// If decision confidence < this threshold, fall back to ModeNone.
	// Range: 0.0-1.0
	// Default: 0.7
	MinConfidence float64

	// AllowedModes restricts which modes can be selected.
	// Empty slice = all modes allowed.
	// Example: []Mode{ModeLazy} = only lazy mode allowed
	// Default: all modes allowed
	AllowedModes []Mode
}

// DefaultSafetyConstraints returns recommended safety constraints.
//
// These defaults are tuned for scientific data workloads:
//   - 50% CPU: Leave headroom for user operations
//   - 100MB memory: Reasonable for TB-scale files
//   - 30s stability: Prevent mode flapping
//   - 0.7 confidence: Only enable if reasonably certain
//   - All modes allowed: No artificial restrictions
func DefaultSafetyConstraints() SafetyConstraints {
	return SafetyConstraints{
		MaxCPUPercent:      50,
		MaxMemoryMB:        100,
		MinStabilityPeriod: 30 * time.Second,
		MinConfidence:      0.7,
		AllowedModes:       nil, // nil = all allowed
	}
}

// Validate checks if constraints are valid.
func (s SafetyConstraints) Validate() error {
	if s.MaxCPUPercent < 1 || s.MaxCPUPercent > 100 {
		return fmt.Errorf("MaxCPUPercent must be in range [1, 100], got: %d", s.MaxCPUPercent)
	}
	if s.MaxMemoryMB < 1 {
		return fmt.Errorf("MaxMemoryMB must be >= 1, got: %d", s.MaxMemoryMB)
	}
	if s.MinStabilityPeriod < 0 {
		return fmt.Errorf("MinStabilityPeriod must be >= 0, got: %v", s.MinStabilityPeriod)
	}
	if s.MinConfidence < 0.0 || s.MinConfidence > 1.0 {
		return fmt.Errorf("MinConfidence must be in range [0, 1], got: %.2f", s.MinConfidence)
	}
	return nil
}

// IsAllowed checks if a mode is allowed by these constraints.
func (s SafetyConstraints) IsAllowed(mode Mode) bool {
	// nil or empty = all modes allowed
	if len(s.AllowedModes) == 0 {
		return true
	}

	for _, allowed := range s.AllowedModes {
		if allowed == mode {
			return true
		}
	}
	return false
}

// ConfigSelector is the main decision engine for rebalancing strategy selection.
//
// This domain service:
//   - Analyzes workload features
//   - Applies decision rules
//   - Respects safety constraints
//   - Returns explainable decisions
//
// Thread Safety: ConfigSelector is stateless and safe for concurrent use.
type ConfigSelector struct {
	// Safety constraints
	constraints SafetyConstraints

	// Selection strategy (future extensibility)
	strategy SelectionStrategy

	// Last decision timestamp (for stability tracking)
	lastDecisionTime time.Time
	lastMode         Mode

	// Clock for testing
	clock Clock
}

// SelectionStrategy is an interface for pluggable selection strategies.
//
// This enables future extensibility:
//   - RuleBasedStrategy (default, current implementation)
//   - MLBasedStrategy (future: machine learning)
//   - CustomStrategy (user-provided logic)
type SelectionStrategy interface {
	// Select analyzes features and returns a decision.
	Select(features WorkloadFeatures, workloadType WorkloadType) Decision
}

// SelectorOption is a functional option for configuring ConfigSelector.
type SelectorOption func(*ConfigSelector)

// WithSafetyConstraints sets custom safety constraints.
//
// Example:
//
//	selector := NewConfigSelector(
//	    WithSafetyConstraints(SafetyConstraints{
//	        MaxCPUPercent:      30,
//	        MaxMemoryMB:        50,
//	        MinConfidence:      0.8,
//	        MinStabilityPeriod: 60 * time.Second,
//	    }),
//	)
func WithSafetyConstraints(constraints SafetyConstraints) SelectorOption {
	return func(s *ConfigSelector) {
		s.constraints = constraints
	}
}

// WithStrategy sets a custom selection strategy.
//
// Example:
//
//	selector := NewConfigSelector(
//	    WithStrategy(&MyCustomStrategy{}),
//	)
func WithStrategy(strategy SelectionStrategy) SelectorOption {
	return func(s *ConfigSelector) {
		if strategy != nil {
			s.strategy = strategy
		}
	}
}

// WithSelectorClock sets a custom clock (for testing).
func WithSelectorClock(clock Clock) SelectorOption {
	return func(s *ConfigSelector) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// NewConfigSelector creates a new config selector with the given options.
//
// Default configuration:
//   - Safety constraints: DefaultSafetyConstraints()
//   - Strategy: RuleBasedStrategy (default)
//   - Clock: RealClock (system time)
//
// Example:
//
//	selector := NewConfigSelector(
//	    WithSafetyConstraints(DefaultSafetyConstraints()),
//	)
func NewConfigSelector(options ...SelectorOption) *ConfigSelector {
	s := &ConfigSelector{
		constraints: DefaultSafetyConstraints(),
		clock:       RealClock{},
	}

	// Apply options
	for _, opt := range options {
		opt(s)
	}

	// Default to rule-based strategy if not set
	if s.strategy == nil {
		s.strategy = &RuleBasedStrategy{clock: s.clock}
	}

	return s
}

// SelectConfig selects the optimal rebalancing configuration.
//
// This method:
//  1. Delegates to selection strategy (default: rule-based)
//  2. Applies safety constraints
//  3. Checks stability period (prevent mode flapping)
//  4. Returns explainable decision
//
// Parameters:
//   - features: Workload features from detector
//   - workloadType: Classified workload type
//
// Returns:
//   - Decision: Immutable decision with config and explainability
//
// Example:
//
//	decision := selector.SelectConfig(features, workloadType)
//	if decision.Confidence >= 0.8 {
//	    // High confidence, use this mode
//	    switch config := decision.Config.(type) {
//	    case *structures.LazyRebalancingConfig:
//	        // Apply lazy config
//	    }
//	}
func (s *ConfigSelector) SelectConfig(features WorkloadFeatures, workloadType WorkloadType) Decision {
	// Get base decision from strategy
	decision := s.strategy.Select(features, workloadType)

	// Apply safety constraints

	// 1. Check confidence threshold
	if decision.Confidence < s.constraints.MinConfidence {
		return Decision{
			Config:     nil,
			Mode:       ModeNone,
			Reason:     fmt.Sprintf("Low confidence (%.2f < %.2f) - insufficient data for safe decision", decision.Confidence, s.constraints.MinConfidence),
			Confidence: decision.Confidence,
			Factors:    decision.Factors,
			Timestamp:  s.clock.Now(),
		}
	}

	// 2. Check if mode is allowed
	if !s.constraints.IsAllowed(decision.Mode) {
		return Decision{
			Config:     nil,
			Mode:       ModeNone,
			Reason:     fmt.Sprintf("Mode %s not allowed by safety constraints", decision.Mode),
			Confidence: decision.Confidence,
			Factors:    decision.Factors,
			Timestamp:  s.clock.Now(),
		}
	}

	// 3. Check stability period (prevent mode flapping)
	now := s.clock.Now()
	if !s.lastDecisionTime.IsZero() {
		timeSinceLastDecision := now.Sub(s.lastDecisionTime)
		if timeSinceLastDecision < s.constraints.MinStabilityPeriod {
			if decision.Mode != s.lastMode {
				// Mode change too soon - keep previous mode
				return Decision{
					Config:     nil,
					Mode:       s.lastMode,
					Reason:     fmt.Sprintf("Mode stability enforced (%.1fs < %.1fs) - keeping %s", timeSinceLastDecision.Seconds(), s.constraints.MinStabilityPeriod.Seconds(), s.lastMode),
					Confidence: decision.Confidence,
					Factors:    decision.Factors,
					Timestamp:  now,
				}
			}
		}
	}

	// Update last decision tracking
	s.lastDecisionTime = now
	s.lastMode = decision.Mode

	// Set timestamp
	decision.Timestamp = now

	return decision
}

// RuleBasedStrategy implements rule-based selection (default strategy).
//
// Decision Rules (from research, checked in order):
//
//  1. BatchDeletion + Any size + >60% deletes → Lazy
//     Reason: Batch rebalancing 10-100x faster
//
//  2. AppendOnly + Any size + <5% deletes → None
//     Reason: No deletes = no underflow = no rebalancing needed
//
//  3. FrequentWrites + >100MB + 20-60% deletes → Incremental
//     Reason: Large file + moderate deletes = background processing
//
//  4. FrequentWrites + <100MB + 20-60% deletes → Lazy
//     Reason: Small file = lazy is fast enough
//
//  5. ReadHeavy + Any size + <20% deletes → Lazy
//     Reason: Reads dominate, rebalance occasionally
//
//  6. MixedRW + >500MB + >20% deletes → Incremental
//     Reason: Large file + mixed ops = background
//
//  7. MixedRW + <500MB + >20% deletes → Lazy
//     Reason: Small file = lazy sufficient
//
//  8. Unknown → None
//     Reason: Insufficient data, default to no rebalancing
//
// File Size Thresholds:
//   - Small: < 100MB (lazy is fast enough)
//   - Medium: 100MB - 500MB (incremental starts to help)
//   - Large: > 500MB (incremental strongly recommended)
type RuleBasedStrategy struct {
	clock Clock
}

// File size thresholds (bytes).
const (
	smallFileThreshold  = 100 * 1024 * 1024  // 100MB
	mediumFileThreshold = 500 * 1024 * 1024  // 500MB
	largeFileThreshold  = 1024 * 1024 * 1024 // 1GB
)

// Select implements SelectionStrategy interface.
func (r *RuleBasedStrategy) Select(features WorkloadFeatures, workloadType WorkloadType) Decision {
	// Calculate confidence based on sample size and feature quality
	confidence := r.calculateConfidence(features)

	// Decision factors (normalized weights)
	factors := make(map[string]float64)

	// File size factor (normalized to [0, 1])
	fileSizeFactor := r.normalizeFileSize(features.FileSize)
	factors["file_size"] = fileSizeFactor

	// Delete ratio factor (already in [0, 1])
	factors["delete_ratio"] = features.DeleteRatio

	// Burst pattern factor (boolean → 0 or 1)
	if features.BurstDetected {
		factors["burst_pattern"] = 1.0
	} else {
		factors["burst_pattern"] = 0.0
	}

	// Operation rate factor (normalized)
	factors["operation_rate"] = r.normalizeOperationRate(features.OperationRate)

	// Apply decision rules (order matters!)

	// Rule 1: Batch Deletion
	if workloadType == WorkloadBatchDeletion {
		return Decision{
			Config:     r.createLazyConfig(),
			Mode:       ModeLazy,
			Reason:     fmt.Sprintf("Batch deletion pattern detected (%.1f%% deletes, burst pattern) - lazy rebalancing 10-100x faster", features.DeleteRatio*100),
			Confidence: confidence,
			Factors:    factors,
		}
	}

	// Rule 2: Append Only
	if workloadType == WorkloadAppendOnly {
		return Decision{
			Config:     nil,
			Mode:       ModeNone,
			Reason:     fmt.Sprintf("Append-only workload (%.1f%% deletes) - no rebalancing needed", features.DeleteRatio*100),
			Confidence: confidence,
			Factors:    factors,
		}
	}

	// Rule 3 & 4: Frequent Writes
	if workloadType == WorkloadFrequentWrites {
		if features.FileSize > mediumFileThreshold {
			// Large file → incremental
			return Decision{
				Config:     r.createIncrementalConfig(),
				Mode:       ModeIncremental,
				Reason:     fmt.Sprintf("Large file (%s) with frequent writes (%.1f%% write ratio) - incremental background rebalancing recommended", formatBytes(features.FileSize), features.WriteRatio*100),
				Confidence: confidence,
				Factors:    factors,
			}
		}
		// Small file → lazy
		return Decision{
			Config:     r.createLazyConfig(),
			Mode:       ModeLazy,
			Reason:     fmt.Sprintf("Small file (%s) with frequent writes - lazy rebalancing sufficient", formatBytes(features.FileSize)),
			Confidence: confidence,
			Factors:    factors,
		}
	}

	// Rule 5: Read Heavy
	if workloadType == WorkloadReadHeavy {
		return Decision{
			Config:     r.createLazyConfig(),
			Mode:       ModeLazy,
			Reason:     fmt.Sprintf("Read-heavy workload (%.1f%% reads) - lazy rebalancing on occasional deletes", features.ReadRatio*100),
			Confidence: confidence,
			Factors:    factors,
		}
	}

	// Rule 6 & 7: Mixed R/W
	if workloadType == WorkloadMixedRW {
		if features.FileSize > mediumFileThreshold {
			// Large file → incremental
			return Decision{
				Config:     r.createIncrementalConfig(),
				Mode:       ModeIncremental,
				Reason:     fmt.Sprintf("Large file (%s) with mixed operations - incremental background rebalancing recommended", formatBytes(features.FileSize)),
				Confidence: confidence,
				Factors:    factors,
			}
		}
		// Small file → lazy
		return Decision{
			Config:     r.createLazyConfig(),
			Mode:       ModeLazy,
			Reason:     fmt.Sprintf("Small file (%s) with mixed operations - lazy rebalancing sufficient", formatBytes(features.FileSize)),
			Confidence: confidence,
			Factors:    factors,
		}
	}

	// Rule 8: Unknown
	return Decision{
		Config:     nil,
		Mode:       ModeNone,
		Reason:     "Unknown workload pattern - insufficient data for safe decision",
		Confidence: confidence,
		Factors:    factors,
	}
}

// calculateConfidence calculates decision confidence based on feature quality.
//
// Confidence factors:
//   - Sample size (more samples = higher confidence)
//   - Feature clarity (strong signals = higher confidence)
//   - Pattern consistency (burst vs continuous)
//
// Returns confidence in range [0, 1].
func (r *RuleBasedStrategy) calculateConfidence(features WorkloadFeatures) float64 {
	if !features.IsValid() {
		return 0.0
	}

	// Base confidence from sample size (logarithmic scale)
	// 10 samples = 0.5, 100 samples = 0.75, 1000+ samples = 0.9
	sampleConfidence := 0.0
	switch {
	case features.SampleSize >= 1000:
		sampleConfidence = 0.9
	case features.SampleSize >= 100:
		sampleConfidence = 0.75
	case features.SampleSize >= 50:
		sampleConfidence = 0.65
	case features.SampleSize >= 10:
		sampleConfidence = 0.5
	default:
		sampleConfidence = 0.3
	}

	// Feature clarity bonus (strong signals increase confidence)
	clarityBonus := 0.0

	// Strong delete ratio signals (very high or very low)
	if features.DeleteRatio > 0.6 || features.DeleteRatio < 0.05 {
		clarityBonus += 0.1
	}

	// Burst pattern is a clear signal
	if features.BurstDetected {
		clarityBonus += 0.05
	}

	// Cap at 1.0
	confidence := sampleConfidence + clarityBonus
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// normalizeFileSize normalizes file size to [0, 1] range.
//
// Mapping:
//   - 0-100MB → 0.0-0.3 (small)
//   - 100MB-500MB → 0.3-0.6 (medium)
//   - 500MB-1GB → 0.6-0.8 (large)
//   - >1GB → 0.8-1.0 (very large)
func (r *RuleBasedStrategy) normalizeFileSize(fileSize uint64) float64 {
	switch {
	case fileSize < smallFileThreshold:
		// 0-100MB → 0.0-0.3
		return (float64(fileSize) / float64(smallFileThreshold)) * 0.3
	case fileSize < mediumFileThreshold:
		// 100MB-500MB → 0.3-0.6
		ratio := float64(fileSize-smallFileThreshold) / float64(mediumFileThreshold-smallFileThreshold)
		return 0.3 + (ratio * 0.3)
	case fileSize < largeFileThreshold:
		// 500MB-1GB → 0.6-0.8
		ratio := float64(fileSize-mediumFileThreshold) / float64(largeFileThreshold-mediumFileThreshold)
		return 0.6 + (ratio * 0.2)
	default:
		// >1GB → 0.8-1.0 (capped)
		ratio := float64(fileSize-largeFileThreshold) / float64(largeFileThreshold)
		normalized := 0.8 + (ratio * 0.2)
		if normalized > 1.0 {
			normalized = 1.0
		}
		return normalized
	}
}

// normalizeOperationRate normalizes operation rate to [0, 1] range.
//
// Mapping:
//   - 0-10 ops/s → 0.0-0.3 (slow)
//   - 10-100 ops/s → 0.3-0.7 (moderate)
//   - >100 ops/s → 0.7-1.0 (fast)
func (r *RuleBasedStrategy) normalizeOperationRate(rate float64) float64 {
	switch {
	case rate < 10.0:
		return (rate / 10.0) * 0.3
	case rate < 100.0:
		ratio := (rate - 10.0) / 90.0
		return 0.3 + (ratio * 0.4)
	default:
		ratio := (rate - 100.0) / 100.0
		normalized := 0.7 + (ratio * 0.3)
		if normalized > 1.0 {
			normalized = 1.0
		}
		return normalized
	}
}

// createLazyConfig creates a default lazy rebalancing config.
func (r *RuleBasedStrategy) createLazyConfig() *structures.LazyRebalancingConfig {
	config := structures.DefaultLazyConfig()
	return &config
}

// createIncrementalConfig creates a default incremental rebalancing config.
func (r *RuleBasedStrategy) createIncrementalConfig() *structures.IncrementalRebalancingConfig {
	config := structures.DefaultIncrementalConfig()
	return &config
}

// formatBytes formats bytes as human-readable string.
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes < KB:
		return fmt.Sprintf("%d B", bytes)
	case bytes < MB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	case bytes < GB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	}
}
