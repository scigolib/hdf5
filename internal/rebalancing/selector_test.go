// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package rebalancing

import (
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
)

// TestDecisionString tests Decision.String() method.
func TestDecisionString(t *testing.T) {
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Reason:     "Test reason",
	}

	got := decision.String()
	want := "Decision{Mode: lazy, Confidence: 0.85, Reason: Test reason}"

	if got != want {
		t.Errorf("Decision.String() = %q, want %q", got, want)
	}
}

// TestModeString tests Mode.String() method.
func TestModeString(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeNone, "none"},
		{ModeLazy, "lazy"},
		{ModeIncremental, "incremental"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("Mode.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSafetyConstraintsValidate tests SafetyConstraints.Validate() method.
func TestSafetyConstraintsValidate(t *testing.T) {
	tests := []struct {
		name        string
		constraints SafetyConstraints
		wantErr     bool
	}{
		{
			name:        "valid default constraints",
			constraints: DefaultSafetyConstraints(),
			wantErr:     false,
		},
		{
			name: "valid custom constraints",
			constraints: SafetyConstraints{
				MaxCPUPercent:      30,
				MaxMemoryMB:        50,
				MinStabilityPeriod: 10 * time.Second,
				MinConfidence:      0.8,
			},
			wantErr: false,
		},
		{
			name: "invalid CPU percent (too low)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      0,
				MaxMemoryMB:        100,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      0.7,
			},
			wantErr: true,
		},
		{
			name: "invalid CPU percent (too high)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      101,
				MaxMemoryMB:        100,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      0.7,
			},
			wantErr: true,
		},
		{
			name: "invalid memory (zero)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      50,
				MaxMemoryMB:        0,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      0.7,
			},
			wantErr: true,
		},
		{
			name: "invalid stability period (negative)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      50,
				MaxMemoryMB:        100,
				MinStabilityPeriod: -1 * time.Second,
				MinConfidence:      0.7,
			},
			wantErr: true,
		},
		{
			name: "invalid confidence (too low)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      50,
				MaxMemoryMB:        100,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      -0.1,
			},
			wantErr: true,
		},
		{
			name: "invalid confidence (too high)",
			constraints: SafetyConstraints{
				MaxCPUPercent:      50,
				MaxMemoryMB:        100,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      1.1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.constraints.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSafetyConstraintsIsAllowed tests SafetyConstraints.IsAllowed() method.
func TestSafetyConstraintsIsAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedModes []Mode
		testMode     Mode
		want         bool
	}{
		{
			name:         "nil allowed modes (all allowed)",
			allowedModes: nil,
			testMode:     ModeLazy,
			want:         true,
		},
		{
			name:         "empty allowed modes (all allowed)",
			allowedModes: []Mode{},
			testMode:     ModeIncremental,
			want:         true,
		},
		{
			name:         "mode in allowed list",
			allowedModes: []Mode{ModeLazy, ModeIncremental},
			testMode:     ModeLazy,
			want:         true,
		},
		{
			name:         "mode not in allowed list",
			allowedModes: []Mode{ModeLazy},
			testMode:     ModeIncremental,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := SafetyConstraints{
				MaxCPUPercent:      50,
				MaxMemoryMB:        100,
				MinStabilityPeriod: 30 * time.Second,
				MinConfidence:      0.7,
				AllowedModes:       tt.allowedModes,
			}

			if got := constraints.IsAllowed(tt.testMode); got != tt.want {
				t.Errorf("IsAllowed(%v) = %v, want %v", tt.testMode, got, tt.want)
			}
		})
	}
}

// TestRuleBasedStrategy_DecisionRules tests all decision rules in RuleBasedStrategy.
func TestRuleBasedStrategy_DecisionRules(t *testing.T) {
	runDecisionRuleTest(t, getBatchDeletionTests())
	runDecisionRuleTest(t, getAppendOnlyTests())
	runDecisionRuleTest(t, getFrequentWritesTests())
	runDecisionRuleTest(t, getReadHeavyTests())
	runDecisionRuleTest(t, getMixedRWTests())
	runDecisionRuleTest(t, getUnknownWorkloadTests())
}

// Helper function to run decision rule tests.
func runDecisionRuleTest(t *testing.T, tests []decisionRuleTest) {
	t.Helper()
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	for i := range tests {
		tt := &tests[i] // Use pointer to avoid copying
		t.Run(tt.name, func(t *testing.T) {
			strategy := &RuleBasedStrategy{clock: clock}
			decision := strategy.Select(tt.features, tt.workloadType)

			verifyDecisionMode(t, decision, tt.wantMode)
			verifyDecisionConfig(t, decision, tt.wantMode, tt.wantConfig)
			verifyDecisionExplainability(t, decision)
		})
	}
}

func verifyDecisionMode(t *testing.T, decision Decision, wantMode Mode) {
	t.Helper()
	if decision.Mode != wantMode {
		t.Errorf("Select() mode = %v, want %v", decision.Mode, wantMode)
	}
}

func verifyDecisionConfig(t *testing.T, decision Decision, wantMode Mode, wantConfig bool) {
	t.Helper()
	hasConfig := decision.Config != nil
	if hasConfig != wantConfig {
		t.Errorf("Select() config non-nil = %v, want %v", hasConfig, wantConfig)
	}

	if !wantConfig {
		return
	}

	// Verify config type matches mode
	switch wantMode {
	case ModeLazy:
		if _, ok := decision.Config.(*structures.LazyRebalancingConfig); !ok {
			t.Errorf("Select() config type = %T, want *structures.LazyRebalancingConfig", decision.Config)
		}
	case ModeIncremental:
		if _, ok := decision.Config.(*structures.IncrementalRebalancingConfig); !ok {
			t.Errorf("Select() config type = %T, want *structures.IncrementalRebalancingConfig", decision.Config)
		}
	}
}

func verifyDecisionExplainability(t *testing.T, decision Decision) {
	t.Helper()

	if decision.Reason == "" {
		t.Error("Select() reason is empty, want non-empty explanation")
	}

	if decision.Confidence < 0.0 || decision.Confidence > 1.0 {
		t.Errorf("Select() confidence = %.2f, want in range [0, 1]", decision.Confidence)
	}

	if decision.Factors == nil {
		t.Error("Select() factors is nil, want non-nil map")
	}

	// Verify factors exist
	expectedFactors := []string{"file_size", "delete_ratio", "burst_pattern", "operation_rate"}
	for _, factor := range expectedFactors {
		if _, ok := decision.Factors[factor]; !ok {
			t.Errorf("Select() factors missing %q", factor)
		}
	}
}

type decisionRuleTest struct {
	name         string
	features     WorkloadFeatures
	workloadType WorkloadType
	wantMode     Mode
	wantConfig   bool // true if config should be non-nil
}

func getBatchDeletionTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "batch deletion - small file",
			features: WorkloadFeatures{
				DeleteRatio:   0.7,
				WriteRatio:    0.2,
				ReadRatio:     0.1,
				BurstDetected: true,
				FileSize:      50 * 1024 * 1024, // 50MB
				SampleSize:    100,
			},
			workloadType: WorkloadBatchDeletion,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
		{
			name: "batch deletion - large file",
			features: WorkloadFeatures{
				DeleteRatio:   0.8,
				WriteRatio:    0.1,
				ReadRatio:     0.1,
				BurstDetected: true,
				FileSize:      1024 * 1024 * 1024, // 1GB
				SampleSize:    200,
			},
			workloadType: WorkloadBatchDeletion,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
	}
}

func getAppendOnlyTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "append only - no deletes",
			features: WorkloadFeatures{
				DeleteRatio:   0.0,
				WriteRatio:    0.6,
				ReadRatio:     0.4,
				BurstDetected: false,
				FileSize:      200 * 1024 * 1024, // 200MB
				SampleSize:    100,
			},
			workloadType: WorkloadAppendOnly,
			wantMode:     ModeNone,
			wantConfig:   false,
		},
		{
			name: "append only - very few deletes",
			features: WorkloadFeatures{
				DeleteRatio:   0.02,
				WriteRatio:    0.7,
				ReadRatio:     0.28,
				BurstDetected: false,
				FileSize:      500 * 1024 * 1024, // 500MB
				SampleSize:    150,
			},
			workloadType: WorkloadAppendOnly,
			wantMode:     ModeNone,
			wantConfig:   false,
		},
	}
}

func getFrequentWritesTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "frequent writes - large file",
			features: WorkloadFeatures{
				DeleteRatio:   0.1,
				WriteRatio:    0.7,
				ReadRatio:     0.2,
				BurstDetected: false,
				FileSize:      600 * 1024 * 1024, // 600MB (> medium threshold)
				SampleSize:    100,
			},
			workloadType: WorkloadFrequentWrites,
			wantMode:     ModeIncremental,
			wantConfig:   true,
		},
		{
			name: "frequent writes - small file",
			features: WorkloadFeatures{
				DeleteRatio:   0.1,
				WriteRatio:    0.7,
				ReadRatio:     0.2,
				BurstDetected: false,
				FileSize:      50 * 1024 * 1024, // 50MB (< medium threshold)
				SampleSize:    100,
			},
			workloadType: WorkloadFrequentWrites,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
	}
}

func getReadHeavyTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "read heavy - small file",
			features: WorkloadFeatures{
				DeleteRatio:   0.1,
				WriteRatio:    0.1,
				ReadRatio:     0.8,
				BurstDetected: false,
				FileSize:      100 * 1024 * 1024, // 100MB
				SampleSize:    100,
			},
			workloadType: WorkloadReadHeavy,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
		{
			name: "read heavy - large file",
			features: WorkloadFeatures{
				DeleteRatio:   0.05,
				WriteRatio:    0.05,
				ReadRatio:     0.9,
				BurstDetected: false,
				FileSize:      2 * 1024 * 1024 * 1024, // 2GB
				SampleSize:    200,
			},
			workloadType: WorkloadReadHeavy,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
	}
}

func getMixedRWTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "mixed rw - large file",
			features: WorkloadFeatures{
				DeleteRatio:   0.15,
				WriteRatio:    0.45,
				ReadRatio:     0.4,
				BurstDetected: false,
				FileSize:      800 * 1024 * 1024, // 800MB (> medium threshold)
				SampleSize:    100,
			},
			workloadType: WorkloadMixedRW,
			wantMode:     ModeIncremental,
			wantConfig:   true,
		},
		{
			name: "mixed rw - small file",
			features: WorkloadFeatures{
				DeleteRatio:   0.15,
				WriteRatio:    0.45,
				ReadRatio:     0.4,
				BurstDetected: false,
				FileSize:      80 * 1024 * 1024, // 80MB (< medium threshold)
				SampleSize:    100,
			},
			workloadType: WorkloadMixedRW,
			wantMode:     ModeLazy,
			wantConfig:   true,
		},
	}
}

func getUnknownWorkloadTests() []decisionRuleTest {
	return []decisionRuleTest{
		{
			name: "unknown workload - insufficient data",
			features: WorkloadFeatures{
				DeleteRatio:   0.3,
				WriteRatio:    0.3,
				ReadRatio:     0.4,
				BurstDetected: false,
				FileSize:      100 * 1024 * 1024,
				SampleSize:    5, // Below minimum
			},
			workloadType: WorkloadUnknown,
			wantMode:     ModeNone,
			wantConfig:   false,
		},
	}
}

// TestRuleBasedStrategy_ConfidenceCalculation tests confidence calculation.
func TestRuleBasedStrategy_ConfidenceCalculation(t *testing.T) {
	strategy := &RuleBasedStrategy{clock: RealClock{}}

	tests := []struct {
		name          string
		features      WorkloadFeatures
		minConfidence float64
		maxConfidence float64
		wantHigher    bool // If true, expect confidence > minConfidence
		wantStrongSig bool // If true, expect clarity bonus
	}{
		{
			name: "high confidence - many samples + strong delete signal",
			features: WorkloadFeatures{
				DeleteRatio:   0.8, // Strong signal
				BurstDetected: true,
				SampleSize:    1000, // Many samples
			},
			minConfidence: 0.85,
			maxConfidence: 1.0,
			wantHigher:    true,
			wantStrongSig: true,
		},
		{
			name: "moderate confidence - good samples",
			features: WorkloadFeatures{
				DeleteRatio:   0.3,
				BurstDetected: false,
				SampleSize:    100,
			},
			minConfidence: 0.70,
			maxConfidence: 0.85,
			wantHigher:    true,
			wantStrongSig: false,
		},
		{
			name: "low confidence - few samples",
			features: WorkloadFeatures{
				DeleteRatio:   0.5,
				BurstDetected: false,
				SampleSize:    5,
			},
			minConfidence: 0.0,
			maxConfidence: 0.5,
			wantHigher:    false,
			wantStrongSig: false,
		},
		{
			name: "zero confidence - no samples",
			features: WorkloadFeatures{
				DeleteRatio:   0.5,
				BurstDetected: false,
				SampleSize:    0,
			},
			minConfidence: 0.0,
			maxConfidence: 0.0,
			wantHigher:    false,
			wantStrongSig: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := strategy.calculateConfidence(tt.features)

			if confidence < tt.minConfidence || confidence > tt.maxConfidence {
				t.Errorf("calculateConfidence() = %.2f, want in range [%.2f, %.2f]", confidence, tt.minConfidence, tt.maxConfidence)
			}
		})
	}
}

// TestConfigSelector_BasicSelection tests basic config selection.
func TestConfigSelector_BasicSelection(t *testing.T) {
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	selector := NewConfigSelector(
		WithSelectorClock(clock),
		WithSafetyConstraints(SafetyConstraints{
			MaxCPUPercent:      50,
			MaxMemoryMB:        100,
			MinStabilityPeriod: 0, // Disable for this test
			MinConfidence:      0.7,
			AllowedModes:       nil,
		}),
	)

	// Test: High confidence batch deletion → Lazy
	features := WorkloadFeatures{
		DeleteRatio:   0.7,
		BurstDetected: true,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    100,
	}

	decision := selector.SelectConfig(features, WorkloadBatchDeletion)

	if decision.Mode != ModeLazy {
		t.Errorf("SelectConfig() mode = %v, want %v", decision.Mode, ModeLazy)
	}

	if decision.Config == nil {
		t.Error("SelectConfig() config is nil, want non-nil")
	}

	if decision.Confidence < 0.7 {
		t.Errorf("SelectConfig() confidence = %.2f, want >= 0.7", decision.Confidence)
	}
}

// TestConfigSelector_ConfidenceThreshold tests confidence threshold enforcement.
func TestConfigSelector_ConfidenceThreshold(t *testing.T) {
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	selector := NewConfigSelector(
		WithSelectorClock(clock),
		WithSafetyConstraints(SafetyConstraints{
			MaxCPUPercent:      50,
			MaxMemoryMB:        100,
			MinStabilityPeriod: 0,
			MinConfidence:      0.9, // Very high threshold
			AllowedModes:       nil,
		}),
	)

	// Features with moderate confidence (< 0.9)
	features := WorkloadFeatures{
		DeleteRatio:   0.7,
		BurstDetected: false,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    50, // Moderate samples → moderate confidence
	}

	decision := selector.SelectConfig(features, WorkloadBatchDeletion)

	// Should fall back to ModeNone due to low confidence
	if decision.Mode != ModeNone {
		t.Errorf("SelectConfig() mode = %v, want %v (low confidence fallback)", decision.Mode, ModeNone)
	}

	if decision.Config != nil {
		t.Error("SelectConfig() config should be nil for low confidence")
	}

	// Verify reason mentions confidence
	if decision.Reason == "" {
		t.Error("SelectConfig() reason is empty, should explain low confidence")
	}
}

// TestConfigSelector_ModeRestriction tests mode restriction enforcement.
func TestConfigSelector_ModeRestriction(t *testing.T) {
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	selector := NewConfigSelector(
		WithSelectorClock(clock),
		WithSafetyConstraints(SafetyConstraints{
			MaxCPUPercent:      50,
			MaxMemoryMB:        100,
			MinStabilityPeriod: 0,
			MinConfidence:      0.7,
			AllowedModes:       []Mode{ModeLazy}, // Only lazy allowed
		}),
	)

	// Features that would normally select Incremental
	features := WorkloadFeatures{
		DeleteRatio:   0.1,
		WriteRatio:    0.7,
		BurstDetected: false,
		FileSize:      800 * 1024 * 1024, // Large file
		SampleSize:    100,
	}

	decision := selector.SelectConfig(features, WorkloadFrequentWrites)

	// Should fall back to ModeNone (Incremental not allowed)
	if decision.Mode != ModeNone {
		t.Errorf("SelectConfig() mode = %v, want %v (mode restriction)", decision.Mode, ModeNone)
	}
}

// TestConfigSelector_StabilityPeriod tests stability period enforcement.
func TestConfigSelector_StabilityPeriod(t *testing.T) {
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	selector := NewConfigSelector(
		WithSelectorClock(clock),
		WithSafetyConstraints(SafetyConstraints{
			MaxCPUPercent:      50,
			MaxMemoryMB:        100,
			MinStabilityPeriod: 30 * time.Second,
			MinConfidence:      0.7,
			AllowedModes:       nil,
		}),
	)

	// First decision: Lazy mode
	features1 := WorkloadFeatures{
		DeleteRatio:   0.7,
		BurstDetected: true,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    100,
	}
	decision1 := selector.SelectConfig(features1, WorkloadBatchDeletion)

	if decision1.Mode != ModeLazy {
		t.Errorf("First decision mode = %v, want %v", decision1.Mode, ModeLazy)
	}

	// Advance time by 10 seconds (< stability period)
	clock.current = clock.current.Add(10 * time.Second)

	// Second decision: Try to switch to Incremental
	features2 := WorkloadFeatures{
		DeleteRatio:   0.1,
		WriteRatio:    0.7,
		BurstDetected: false,
		FileSize:      800 * 1024 * 1024,
		SampleSize:    100,
	}
	decision2 := selector.SelectConfig(features2, WorkloadFrequentWrites)

	// Should keep previous mode (ModeLazy) due to stability period
	if decision2.Mode != ModeLazy {
		t.Errorf("Second decision mode = %v, want %v (stability enforced)", decision2.Mode, ModeLazy)
	}

	// Advance time by 25 more seconds (total 35s > stability period)
	clock.current = clock.current.Add(25 * time.Second)

	// Third decision: Now switch should be allowed
	decision3 := selector.SelectConfig(features2, WorkloadFrequentWrites)

	if decision3.Mode != ModeIncremental {
		t.Errorf("Third decision mode = %v, want %v (stability period passed)", decision3.Mode, ModeIncremental)
	}
}

// TestConfigSelector_StabilityPeriod_SameMode tests stability period with same mode.
func TestConfigSelector_StabilityPeriod_SameMode(t *testing.T) {
	clock := &MockClock{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	selector := NewConfigSelector(
		WithSelectorClock(clock),
		WithSafetyConstraints(SafetyConstraints{
			MaxCPUPercent:      50,
			MaxMemoryMB:        100,
			MinStabilityPeriod: 30 * time.Second,
			MinConfidence:      0.7,
			AllowedModes:       nil,
		}),
	)

	features := WorkloadFeatures{
		DeleteRatio:   0.7,
		BurstDetected: true,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    100,
	}

	// First decision
	decision1 := selector.SelectConfig(features, WorkloadBatchDeletion)
	if decision1.Mode != ModeLazy {
		t.Fatalf("First decision mode = %v, want %v", decision1.Mode, ModeLazy)
	}

	// Advance time by 5 seconds (< stability period)
	clock.current = clock.current.Add(5 * time.Second)

	// Second decision: Same features → same mode
	decision2 := selector.SelectConfig(features, WorkloadBatchDeletion)

	// Should allow same mode even within stability period
	if decision2.Mode != ModeLazy {
		t.Errorf("Second decision mode = %v, want %v (same mode allowed)", decision2.Mode, ModeLazy)
	}
}

// TestNewConfigSelector tests selector creation.
func TestNewConfigSelector(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		selector := NewConfigSelector()

		if selector.constraints.MaxCPUPercent != 50 {
			t.Errorf("Default MaxCPUPercent = %d, want 50", selector.constraints.MaxCPUPercent)
		}

		if selector.constraints.MaxMemoryMB != 100 {
			t.Errorf("Default MaxMemoryMB = %d, want 100", selector.constraints.MaxMemoryMB)
		}

		if selector.constraints.MinConfidence != 0.7 {
			t.Errorf("Default MinConfidence = %.2f, want 0.7", selector.constraints.MinConfidence)
		}
	})

	t.Run("custom config", func(t *testing.T) {
		customConstraints := SafetyConstraints{
			MaxCPUPercent:      30,
			MaxMemoryMB:        50,
			MinStabilityPeriod: 10 * time.Second,
			MinConfidence:      0.8,
		}

		selector := NewConfigSelector(
			WithSafetyConstraints(customConstraints),
		)

		if selector.constraints.MaxCPUPercent != 30 {
			t.Errorf("Custom MaxCPUPercent = %d, want 30", selector.constraints.MaxCPUPercent)
		}

		if selector.constraints.MinConfidence != 0.8 {
			t.Errorf("Custom MinConfidence = %.2f, want 0.8", selector.constraints.MinConfidence)
		}
	})
}

// TestFormatBytes tests byte formatting.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes uint64
		want  string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"kilobytes", 5 * 1024, "5.0 KB"},
		{"megabytes", 100 * 1024 * 1024, "100.0 MB"},
		{"gigabytes", 2 * 1024 * 1024 * 1024, "2.00 GB"},
		{"large", 5*1024*1024*1024 + 512*1024*1024, "5.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

// TestRuleBasedStrategy_NormalizeFileSize tests file size normalization.
func TestRuleBasedStrategy_NormalizeFileSize(t *testing.T) {
	strategy := &RuleBasedStrategy{clock: RealClock{}}

	tests := []struct {
		name     string
		fileSize uint64
		minValue float64
		maxValue float64
	}{
		{"small file (10MB)", 10 * 1024 * 1024, 0.0, 0.3},
		{"small threshold (100MB)", 100 * 1024 * 1024, 0.29, 0.31},
		{"medium file (300MB)", 300 * 1024 * 1024, 0.3, 0.6},
		{"medium threshold (500MB)", 500 * 1024 * 1024, 0.59, 0.61},
		{"large file (800MB)", 800 * 1024 * 1024, 0.6, 0.8},
		{"large threshold (1GB)", 1024 * 1024 * 1024, 0.79, 0.81},
		{"very large (2GB)", 2 * 1024 * 1024 * 1024, 0.8, 1.0},
		{"huge (10GB)", 10 * 1024 * 1024 * 1024, 0.8, 1.0}, // Capped at 1.0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.normalizeFileSize(tt.fileSize)
			if got < tt.minValue || got > tt.maxValue {
				t.Errorf("normalizeFileSize(%d) = %.2f, want in range [%.2f, %.2f]", tt.fileSize, got, tt.minValue, tt.maxValue)
			}
		})
	}
}

// TestRuleBasedStrategy_NormalizeOperationRate tests operation rate normalization.
func TestRuleBasedStrategy_NormalizeOperationRate(t *testing.T) {
	strategy := &RuleBasedStrategy{clock: RealClock{}}

	tests := []struct {
		name     string
		rate     float64
		minValue float64
		maxValue float64
	}{
		{"slow (1 op/s)", 1.0, 0.0, 0.3},
		{"slow threshold (10 op/s)", 10.0, 0.29, 0.31},
		{"moderate (50 op/s)", 50.0, 0.3, 0.7},
		{"moderate threshold (100 op/s)", 100.0, 0.69, 0.71},
		{"fast (150 op/s)", 150.0, 0.7, 1.0},
		{"very fast (500 op/s)", 500.0, 0.7, 1.0}, // Capped at 1.0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.normalizeOperationRate(tt.rate)
			if got < tt.minValue || got > tt.maxValue {
				t.Errorf("normalizeOperationRate(%.1f) = %.2f, want in range [%.2f, %.2f]", tt.rate, got, tt.minValue, tt.maxValue)
			}
		})
	}
}

// Benchmark tests

// BenchmarkConfigSelector_SelectConfig benchmarks config selection.
func BenchmarkConfigSelector_SelectConfig(b *testing.B) {
	selector := NewConfigSelector()

	features := WorkloadFeatures{
		DeleteRatio:   0.7,
		WriteRatio:    0.2,
		ReadRatio:     0.1,
		BurstDetected: true,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = selector.SelectConfig(features, WorkloadBatchDeletion)
	}
}

// BenchmarkRuleBasedStrategy_Select benchmarks strategy selection.
func BenchmarkRuleBasedStrategy_Select(b *testing.B) {
	strategy := &RuleBasedStrategy{clock: RealClock{}}

	features := WorkloadFeatures{
		DeleteRatio:   0.7,
		WriteRatio:    0.2,
		ReadRatio:     0.1,
		BurstDetected: true,
		FileSize:      100 * 1024 * 1024,
		SampleSize:    100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.Select(features, WorkloadBatchDeletion)
	}
}
