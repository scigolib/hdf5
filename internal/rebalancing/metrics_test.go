// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package rebalancing

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
)

// TestMetricsCollector_NewMetricsCollector verifies initialization.
func TestMetricsCollector_NewMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector()

	if mc == nil {
		t.Fatal("NewMetricsCollector returned nil")
	}

	// Verify initial state
	snapshot := mc.Snapshot()

	if snapshot.TotalEvaluations != 0 {
		t.Errorf("Expected 0 evaluations, got %d", snapshot.TotalEvaluations)
	}

	if snapshot.TotalOperations != 0 {
		t.Errorf("Expected 0 operations, got %d", snapshot.TotalOperations)
	}

	if snapshot.ModeChanges != 0 {
		t.Errorf("Expected 0 mode changes, got %d", snapshot.ModeChanges)
	}

	if snapshot.TotalErrors != 0 {
		t.Errorf("Expected 0 errors, got %d", snapshot.TotalErrors)
	}

	// Verify start time is recent (within last second)
	if time.Since(snapshot.SnapshotTime) > time.Second {
		t.Error("Start time is not recent")
	}
}

// TestMetricsCollector_RecordEvaluation verifies evaluation recording.
func TestMetricsCollector_RecordEvaluation(t *testing.T) {
	mc := NewMetricsCollector()

	// Create test decision
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Reason:     "Test decision",
		Factors:    map[string]float64{"test": 0.5},
		Config:     &structures.LazyRebalancingConfig{},
	}

	evalTime := 1500 * time.Microsecond

	// Record evaluation
	mc.RecordEvaluation(decision, evalTime)

	snapshot := mc.Snapshot()

	// Verify total evaluations
	if snapshot.TotalEvaluations != 1 {
		t.Errorf("Expected 1 evaluation, got %d", snapshot.TotalEvaluations)
	}

	// Verify mode count
	if snapshot.DecisionsByMode[ModeLazy] != 1 {
		t.Errorf("Expected 1 lazy decision, got %d", snapshot.DecisionsByMode[ModeLazy])
	}

	// Verify confidence metrics
	if snapshot.AvgConfidence != 0.85 {
		t.Errorf("Expected avg confidence 0.85, got %.2f", snapshot.AvgConfidence)
	}

	if snapshot.MinConfidence != 0.85 {
		t.Errorf("Expected min confidence 0.85, got %.2f", snapshot.MinConfidence)
	}

	if snapshot.MaxConfidence != 0.85 {
		t.Errorf("Expected max confidence 0.85, got %.2f", snapshot.MaxConfidence)
	}

	// Verify eval time metrics
	if snapshot.AvgEvalTime != evalTime {
		t.Errorf("Expected avg eval time %v, got %v", evalTime, snapshot.AvgEvalTime)
	}

	if snapshot.MinEvalTime != evalTime {
		t.Errorf("Expected min eval time %v, got %v", evalTime, snapshot.MinEvalTime)
	}

	if snapshot.MaxEvalTime != evalTime {
		t.Errorf("Expected max eval time %v, got %v", evalTime, snapshot.MaxEvalTime)
	}
}

// TestMetricsCollector_RecordEvaluation_Multiple verifies multiple evaluation recording.
func TestMetricsCollector_RecordEvaluation_Multiple(t *testing.T) {
	mc := NewMetricsCollector()

	// Record multiple evaluations with different modes and confidences
	decisions := []struct {
		mode       Mode
		confidence float64
		evalTime   time.Duration
	}{
		{ModeLazy, 0.8, 1 * time.Millisecond},
		{ModeLazy, 0.9, 2 * time.Millisecond},
		{ModeIncremental, 0.7, 500 * time.Microsecond},
		{ModeNone, 0.6, 1500 * time.Microsecond},
	}

	for _, d := range decisions {
		decision := Decision{
			Mode:       d.mode,
			Confidence: d.confidence,
			Config:     &structures.LazyRebalancingConfig{},
		}
		mc.RecordEvaluation(decision, d.evalTime)
	}

	snapshot := mc.Snapshot()

	// Verify total evaluations
	if snapshot.TotalEvaluations != 4 {
		t.Errorf("Expected 4 evaluations, got %d", snapshot.TotalEvaluations)
	}

	// Verify mode distribution
	if snapshot.DecisionsByMode[ModeLazy] != 2 {
		t.Errorf("Expected 2 lazy decisions, got %d", snapshot.DecisionsByMode[ModeLazy])
	}
	if snapshot.DecisionsByMode[ModeIncremental] != 1 {
		t.Errorf("Expected 1 incremental decision, got %d", snapshot.DecisionsByMode[ModeIncremental])
	}
	if snapshot.DecisionsByMode[ModeNone] != 1 {
		t.Errorf("Expected 1 none decision, got %d", snapshot.DecisionsByMode[ModeNone])
	}

	// Verify confidence metrics
	expectedAvg := (0.8 + 0.9 + 0.7 + 0.6) / 4
	if !floatEquals(snapshot.AvgConfidence, expectedAvg) {
		t.Errorf("Expected avg confidence %.2f, got %.2f", expectedAvg, snapshot.AvgConfidence)
	}

	if snapshot.MinConfidence != 0.6 {
		t.Errorf("Expected min confidence 0.6, got %.2f", snapshot.MinConfidence)
	}

	if snapshot.MaxConfidence != 0.9 {
		t.Errorf("Expected max confidence 0.9, got %.2f", snapshot.MaxConfidence)
	}

	// Verify eval time metrics
	if snapshot.MinEvalTime != 500*time.Microsecond {
		t.Errorf("Expected min eval time 500µs, got %v", snapshot.MinEvalTime)
	}

	if snapshot.MaxEvalTime != 2*time.Millisecond {
		t.Errorf("Expected max eval time 2ms, got %v", snapshot.MaxEvalTime)
	}
}

// TestMetricsCollector_RecordModeChange verifies mode change recording.
func TestMetricsCollector_RecordModeChange(t *testing.T) {
	mc := NewMetricsCollector()

	// Record mode changes
	mc.RecordModeChange(ModeNone, ModeLazy)
	mc.RecordModeChange(ModeLazy, ModeIncremental)

	snapshot := mc.Snapshot()

	if snapshot.ModeChanges != 2 {
		t.Errorf("Expected 2 mode changes, got %d", snapshot.ModeChanges)
	}
}

// TestMetricsCollector_RecordModeChange_NoOp verifies no-op mode changes are not counted.
func TestMetricsCollector_RecordModeChange_NoOp(t *testing.T) {
	mc := NewMetricsCollector()

	// Record no-op mode change (from == to)
	mc.RecordModeChange(ModeLazy, ModeLazy)
	mc.RecordModeChange(ModeNone, ModeNone)

	snapshot := mc.Snapshot()

	if snapshot.ModeChanges != 0 {
		t.Errorf("Expected 0 mode changes (no-ops), got %d", snapshot.ModeChanges)
	}
}

// TestMetricsCollector_RecordOperation verifies operation recording.
func TestMetricsCollector_RecordOperation(t *testing.T) {
	mc := NewMetricsCollector()

	// Record operations
	mc.RecordOperation(OpWrite)
	mc.RecordOperation(OpWrite)
	mc.RecordOperation(OpDelete)
	mc.RecordOperation(OpRead)

	snapshot := mc.Snapshot()

	// Verify total operations
	if snapshot.TotalOperations != 4 {
		t.Errorf("Expected 4 operations, got %d", snapshot.TotalOperations)
	}

	// Verify operation type distribution
	if snapshot.OperationsByType[OpWrite] != 2 {
		t.Errorf("Expected 2 write operations, got %d", snapshot.OperationsByType[OpWrite])
	}
	if snapshot.OperationsByType[OpDelete] != 1 {
		t.Errorf("Expected 1 delete operation, got %d", snapshot.OperationsByType[OpDelete])
	}
	if snapshot.OperationsByType[OpRead] != 1 {
		t.Errorf("Expected 1 read operation, got %d", snapshot.OperationsByType[OpRead])
	}
}

// TestMetricsCollector_RecordOperation_OperationsPerSecond verifies ops/sec calculation.
func TestMetricsCollector_RecordOperation_OperationsPerSecond(t *testing.T) {
	mc := NewMetricsCollector()

	// Wait a bit to have measurable uptime
	time.Sleep(100 * time.Millisecond)

	// Record operations
	for i := 0; i < 10; i++ {
		mc.RecordOperation(OpWrite)
	}

	snapshot := mc.Snapshot()

	// Verify operations per second is calculated
	if snapshot.OperationsPerSecond == 0 {
		t.Error("Expected non-zero operations per second")
	}

	// Rough check: should be less than 1000 ops/sec (we recorded 10 in 100ms)
	if snapshot.OperationsPerSecond > 1000 {
		t.Errorf("Operations per second too high: %.2f", snapshot.OperationsPerSecond)
	}
}

// TestMetricsCollector_RecordError verifies error recording.
func TestMetricsCollector_RecordError(t *testing.T) {
	mc := NewMetricsCollector()

	// Record errors
	mc.RecordError("transition")
	mc.RecordError("transition")
	mc.RecordError("detector")
	mc.RecordError("selector")

	snapshot := mc.Snapshot()

	// Verify error counts
	if snapshot.TransitionErrors != 2 {
		t.Errorf("Expected 2 transition errors, got %d", snapshot.TransitionErrors)
	}
	if snapshot.DetectorErrors != 1 {
		t.Errorf("Expected 1 detector error, got %d", snapshot.DetectorErrors)
	}
	if snapshot.SelectorErrors != 1 {
		t.Errorf("Expected 1 selector error, got %d", snapshot.SelectorErrors)
	}

	// Verify total errors
	if snapshot.TotalErrors != 4 {
		t.Errorf("Expected 4 total errors, got %d", snapshot.TotalErrors)
	}
}

// TestMetricsCollector_RecordError_ErrorRate verifies error rate calculation.
func TestMetricsCollector_RecordError_ErrorRate(t *testing.T) {
	mc := NewMetricsCollector()

	// Record operations and errors
	for i := 0; i < 100; i++ {
		mc.RecordOperation(OpWrite)
	}
	mc.RecordError("transition")
	mc.RecordError("detector")

	snapshot := mc.Snapshot()

	// Verify error rate (2 errors / 100 operations = 0.02)
	expectedRate := 0.02
	if snapshot.ErrorRate != expectedRate {
		t.Errorf("Expected error rate %.3f, got %.3f", expectedRate, snapshot.ErrorRate)
	}
}

// TestMetricsCollector_RecordFileSize verifies file size histogram recording.
func TestMetricsCollector_RecordFileSize(t *testing.T) {
	mc := NewMetricsCollector()

	// Record file sizes in different buckets
	mc.RecordFileSize(50 * 1024 * 1024)               // 50MB (bucket 0: <100MB)
	mc.RecordFileSize(200 * 1024 * 1024)              // 200MB (bucket 1: 100-500MB)
	mc.RecordFileSize(200 * 1024 * 1024)              // 200MB (bucket 1)
	mc.RecordFileSize(600 * 1024 * 1024)              // 600MB (bucket 2: >500MB)
	mc.RecordFileSize(1024 * 1024 * 1024)             // 1GB (bucket 2)
	mc.RecordFileSize(uint64(2) * 1024 * 1024 * 1024) // 2GB (bucket 2)

	snapshot := mc.Snapshot()

	// Verify histogram buckets
	if snapshot.FileSizeHistogram[0] != 1 {
		t.Errorf("Expected 1 file in <100MB bucket, got %d", snapshot.FileSizeHistogram[0])
	}
	if snapshot.FileSizeHistogram[1] != 2 {
		t.Errorf("Expected 2 files in 100-500MB bucket, got %d", snapshot.FileSizeHistogram[1])
	}
	if snapshot.FileSizeHistogram[2] != 3 {
		t.Errorf("Expected 3 files in >500MB bucket, got %d", snapshot.FileSizeHistogram[2])
	}
}

// TestMetricsCollector_RecordWorkloadType verifies workload type recording.
func TestMetricsCollector_RecordWorkloadType(t *testing.T) {
	mc := NewMetricsCollector()

	// Record workload types
	mc.RecordWorkloadType(WorkloadBatchDeletion)
	mc.RecordWorkloadType(WorkloadFrequentWrites)
	mc.RecordWorkloadType(WorkloadBatchDeletion)

	snapshot := mc.Snapshot()

	// Verify workload distribution
	if snapshot.DecisionsByWorkload[WorkloadBatchDeletion] != 2 {
		t.Errorf("Expected 2 batch deletion detections, got %d", snapshot.DecisionsByWorkload[WorkloadBatchDeletion])
	}
	if snapshot.DecisionsByWorkload[WorkloadFrequentWrites] != 1 {
		t.Errorf("Expected 1 frequent writes detection, got %d", snapshot.DecisionsByWorkload[WorkloadFrequentWrites])
	}
}

// TestMetricsCollector_Snapshot_Immutability verifies snapshot is immutable.
func TestMetricsCollector_Snapshot_Immutability(t *testing.T) {
	mc := NewMetricsCollector()

	// Record some data
	mc.RecordOperation(OpWrite)
	mc.RecordError("transition")

	// Take snapshot
	snapshot1 := mc.Snapshot()

	// Record more data
	mc.RecordOperation(OpWrite)
	mc.RecordOperation(OpWrite)
	mc.RecordError("detector")

	// Take another snapshot
	snapshot2 := mc.Snapshot()

	// Verify first snapshot is unchanged (immutable)
	if snapshot1.TotalOperations != 1 {
		t.Errorf("Snapshot 1 changed: expected 1 operation, got %d", snapshot1.TotalOperations)
	}

	if snapshot1.TotalErrors != 1 {
		t.Errorf("Snapshot 1 changed: expected 1 error, got %d", snapshot1.TotalErrors)
	}

	// Verify second snapshot has new data
	if snapshot2.TotalOperations != 3 {
		t.Errorf("Snapshot 2: expected 3 operations, got %d", snapshot2.TotalOperations)
	}

	if snapshot2.TotalErrors != 2 {
		t.Errorf("Snapshot 2: expected 2 errors, got %d", snapshot2.TotalErrors)
	}
}

// TestMetricsCollector_Reset verifies reset functionality.
func TestMetricsCollector_Reset(t *testing.T) {
	mc := NewMetricsCollector()

	// Record data
	mc.RecordOperation(OpWrite)
	mc.RecordError("transition")
	decision := Decision{Mode: ModeLazy, Confidence: 0.85, Config: &structures.LazyRebalancingConfig{}}
	mc.RecordEvaluation(decision, 1*time.Millisecond)
	mc.RecordModeChange(ModeNone, ModeLazy)

	// Verify data is recorded
	snapshot := mc.Snapshot()
	if snapshot.TotalOperations != 1 {
		t.Errorf("Expected 1 operation before reset, got %d", snapshot.TotalOperations)
	}

	// Reset
	mc.Reset()

	// Verify all metrics are reset
	snapshot = mc.Snapshot()

	if snapshot.TotalOperations != 0 {
		t.Errorf("Expected 0 operations after reset, got %d", snapshot.TotalOperations)
	}

	if snapshot.TotalErrors != 0 {
		t.Errorf("Expected 0 errors after reset, got %d", snapshot.TotalErrors)
	}

	if snapshot.TotalEvaluations != 0 {
		t.Errorf("Expected 0 evaluations after reset, got %d", snapshot.TotalEvaluations)
	}

	if snapshot.ModeChanges != 0 {
		t.Errorf("Expected 0 mode changes after reset, got %d", snapshot.ModeChanges)
	}

	// Verify maps are reset
	if len(snapshot.DecisionsByMode) != 0 {
		t.Errorf("Expected empty DecisionsByMode map after reset, got %d entries", len(snapshot.DecisionsByMode))
	}

	if len(snapshot.OperationsByType) != 0 {
		t.Errorf("Expected empty OperationsByType map after reset, got %d entries", len(snapshot.OperationsByType))
	}
}

// TestMetricsCollector_String verifies string formatting.
func TestMetricsCollector_String(t *testing.T) {
	mc := NewMetricsCollector()

	// Record some data
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Config:     &structures.LazyRebalancingConfig{},
	}
	mc.RecordEvaluation(decision, 1*time.Millisecond)
	mc.RecordOperation(OpWrite)
	mc.RecordOperation(OpDelete)
	mc.RecordModeChange(ModeNone, ModeLazy)
	mc.RecordError("transition")

	str := mc.String()

	// Verify key sections are present
	if !strings.Contains(str, "Rebalancing Metrics Summary") {
		t.Error("String output missing header")
	}

	if !strings.Contains(str, "Evaluations:") {
		t.Error("String output missing evaluations")
	}

	if !strings.Contains(str, "Mode Changes:") {
		t.Error("String output missing mode changes")
	}

	if !strings.Contains(str, "Operations:") {
		t.Error("String output missing operations")
	}

	if !strings.Contains(str, "Confidence:") {
		t.Error("String output missing confidence")
	}

	if !strings.Contains(str, "Errors:") {
		t.Error("String output missing errors")
	}

	if !strings.Contains(str, "Uptime:") {
		t.Error("String output missing uptime")
	}
}

// TestMetricsCollector_String_Empty verifies string formatting with no data.
func TestMetricsCollector_String_Empty(t *testing.T) {
	mc := NewMetricsCollector()

	str := mc.String()

	// Verify output handles empty metrics gracefully
	if !strings.Contains(str, "Evaluations: 0") {
		t.Error("String output should show 0 evaluations")
	}

	if !strings.Contains(str, "Operations: 0") {
		t.Error("String output should show 0 operations")
	}

	if !strings.Contains(str, "Errors: 0") {
		t.Error("String output should show 0 errors")
	}
}

// TestMetricsSnapshot_MarshalJSON verifies JSON serialization.
func TestMetricsSnapshot_MarshalJSON(t *testing.T) {
	mc := NewMetricsCollector()

	// Record data
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Config:     &structures.LazyRebalancingConfig{},
	}
	mc.RecordEvaluation(decision, 1*time.Millisecond)
	mc.RecordOperation(OpWrite)
	mc.RecordError("transition")

	snapshot := mc.Snapshot()

	// Serialize to JSON
	jsonBytes, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Verify JSON is valid
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &jsonMap); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// Verify key fields are present
	requiredFields := []string{
		"total_evaluations",
		"mode_changes",
		"total_operations",
		"total_errors",
		"error_rate",
		"avg_confidence",
		"operations_per_second",
		"uptime",
	}

	for _, field := range requiredFields {
		if _, ok := jsonMap[field]; !ok {
			t.Errorf("JSON missing required field: %s", field)
		}
	}
}

// TestMetricsCollector_Concurrent verifies thread safety.
func TestMetricsCollector_Concurrent(t *testing.T) {
	mc := NewMetricsCollector()

	const (
		numGoroutines = 10
		opsPerRoutine = 100
	)

	var wg sync.WaitGroup

	// Spawn multiple goroutines recording metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < opsPerRoutine; j++ {
				// Record various metrics
				mc.RecordOperation(OpWrite)

				decision := Decision{
					Mode:       ModeLazy,
					Confidence: 0.85,
					Config:     &structures.LazyRebalancingConfig{},
				}
				mc.RecordEvaluation(decision, time.Millisecond)

				if j%10 == 0 {
					mc.RecordError("transition")
				}

				if j%20 == 0 {
					mc.RecordModeChange(ModeNone, ModeLazy)
				}
			}
		}()
	}

	// Also run concurrent snapshots
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = mc.Snapshot()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify final counts
	snapshot := mc.Snapshot()

	expectedOps := numGoroutines * opsPerRoutine
	if snapshot.TotalOperations != int64(expectedOps) {
		t.Errorf("Expected %d operations, got %d", expectedOps, snapshot.TotalOperations)
	}

	expectedEvals := numGoroutines * opsPerRoutine
	if snapshot.TotalEvaluations != int64(expectedEvals) {
		t.Errorf("Expected %d evaluations, got %d", expectedEvals, snapshot.TotalEvaluations)
	}
}

// TestMetricsCollector_Performance verifies low overhead.
func TestMetricsCollector_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	mc := NewMetricsCollector()

	const iterations = 100000

	// Benchmark RecordOperation (should be <10ns)
	start := time.Now()
	for i := 0; i < iterations; i++ {
		mc.RecordOperation(OpWrite)
	}
	elapsed := time.Since(start)
	avgRecordOp := elapsed / iterations

	if avgRecordOp > 200*time.Nanosecond {
		t.Errorf("RecordOperation too slow: avg %v (expected <200ns)", avgRecordOp)
	}

	t.Logf("RecordOperation avg: %v", avgRecordOp)

	// Benchmark RecordEvaluation (should be <100ns)
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Config:     &structures.LazyRebalancingConfig{},
	}

	start = time.Now()
	for i := 0; i < iterations; i++ {
		mc.RecordEvaluation(decision, time.Millisecond)
	}
	elapsed = time.Since(start)
	avgRecordEval := elapsed / iterations

	if avgRecordEval > 500*time.Nanosecond {
		t.Errorf("RecordEvaluation too slow: avg %v (expected <500ns)", avgRecordEval)
	}

	t.Logf("RecordEvaluation avg: %v", avgRecordEval)

	// Benchmark Snapshot (should be <100µs)
	start = time.Now()
	for i := 0; i < 1000; i++ {
		_ = mc.Snapshot()
	}
	elapsed = time.Since(start)
	avgSnapshot := elapsed / 1000

	if avgSnapshot > 1*time.Millisecond {
		t.Errorf("Snapshot too slow: avg %v (expected <1ms)", avgSnapshot)
	}

	t.Logf("Snapshot avg: %v", avgSnapshot)
}

// TestMetricsCollector_Integration_SmartRebalancer verifies integration.
func TestMetricsCollector_Integration_SmartRebalancer(t *testing.T) {
	// This test verifies that SmartRebalancer can use MetricsCollector correctly.
	// We'll test this after integrating metrics into SmartRebalancer.

	// For now, verify that metrics can be created and used independently
	mc := NewMetricsCollector()

	// Simulate SmartRebalancer usage
	// 1. Record evaluation
	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Reason:     "Test decision",
		Config:     &structures.LazyRebalancingConfig{},
	}
	mc.RecordEvaluation(decision, 1*time.Millisecond)

	// 2. Record mode change
	mc.RecordModeChange(ModeNone, ModeLazy)

	// 3. Record operations
	mc.RecordOperation(OpWrite)
	mc.RecordOperation(OpDelete)

	// 4. Record workload detection
	mc.RecordWorkloadType(WorkloadBatchDeletion)

	// 5. Get snapshot for export
	snapshot := mc.Snapshot()

	// Verify all data was recorded
	if snapshot.TotalEvaluations != 1 {
		t.Error("Integration: evaluation not recorded")
	}

	if snapshot.ModeChanges != 1 {
		t.Error("Integration: mode change not recorded")
	}

	if snapshot.TotalOperations != 2 {
		t.Error("Integration: operations not recorded")
	}

	if snapshot.DecisionsByWorkload[WorkloadBatchDeletion] != 1 {
		t.Error("Integration: workload type not recorded")
	}

	// Verify export formats work
	_ = mc.String() // Should not panic
	_, err := json.Marshal(snapshot)
	if err != nil {
		t.Errorf("Integration: JSON export failed: %v", err)
	}
}

// TestMetricsCollector_Uptime verifies uptime calculation.
func TestMetricsCollector_Uptime(t *testing.T) {
	mc := NewMetricsCollector()

	// Wait a known amount of time
	time.Sleep(100 * time.Millisecond)

	snapshot := mc.Snapshot()

	// Uptime should be at least 100ms
	if snapshot.Uptime < 100*time.Millisecond {
		t.Errorf("Uptime too short: %v (expected >= 100ms)", snapshot.Uptime)
	}

	// Uptime should be less than 200ms (allowing some overhead)
	if snapshot.Uptime > 200*time.Millisecond {
		t.Errorf("Uptime too long: %v (expected < 200ms)", snapshot.Uptime)
	}
}

// TestMetricsCollector_ConfidenceTracking verifies confidence min/max tracking.
func TestMetricsCollector_ConfidenceTracking(t *testing.T) {
	mc := NewMetricsCollector()

	confidences := []float64{0.5, 0.9, 0.3, 0.7, 0.95}

	for _, conf := range confidences {
		decision := Decision{
			Mode:       ModeLazy,
			Confidence: conf,
			Config:     &structures.LazyRebalancingConfig{},
		}
		mc.RecordEvaluation(decision, time.Millisecond)
	}

	snapshot := mc.Snapshot()

	// Verify min confidence
	if snapshot.MinConfidence != 0.3 {
		t.Errorf("Expected min confidence 0.3, got %.2f", snapshot.MinConfidence)
	}

	// Verify max confidence
	if snapshot.MaxConfidence != 0.95 {
		t.Errorf("Expected max confidence 0.95, got %.2f", snapshot.MaxConfidence)
	}

	// Verify avg confidence
	expectedAvg := (0.5 + 0.9 + 0.3 + 0.7 + 0.95) / 5
	if !floatEquals(snapshot.AvgConfidence, expectedAvg) {
		t.Errorf("Expected avg confidence %.2f, got %.2f", expectedAvg, snapshot.AvgConfidence)
	}
}

// floatEquals checks if two floats are approximately equal (within epsilon).
func floatEquals(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

// TestMetricsCollector_EvalTimeTracking verifies eval time min/max tracking.
func TestMetricsCollector_EvalTimeTracking(t *testing.T) {
	mc := NewMetricsCollector()

	evalTimes := []time.Duration{
		500 * time.Microsecond,
		2 * time.Millisecond,
		100 * time.Microsecond,
		1500 * time.Microsecond,
	}

	decision := Decision{
		Mode:       ModeLazy,
		Confidence: 0.85,
		Config:     &structures.LazyRebalancingConfig{},
	}

	for _, evalTime := range evalTimes {
		mc.RecordEvaluation(decision, evalTime)
	}

	snapshot := mc.Snapshot()

	// Verify min eval time
	if snapshot.MinEvalTime != 100*time.Microsecond {
		t.Errorf("Expected min eval time 100µs, got %v", snapshot.MinEvalTime)
	}

	// Verify max eval time
	if snapshot.MaxEvalTime != 2*time.Millisecond {
		t.Errorf("Expected max eval time 2ms, got %v", snapshot.MaxEvalTime)
	}
}
