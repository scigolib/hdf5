// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package rebalancing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockClock is a controllable clock for testing.
type MockClock struct {
	current time.Time
}

// Now returns the current mock time.
func (m *MockClock) Now() time.Time {
	return m.current
}

// Advance advances the mock time by the given duration.
func (m *MockClock) Advance(d time.Duration) {
	m.current = m.current.Add(d)
}

// NewMockClock creates a new mock clock starting at the given time.
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{current: start}
}

// TestNewWorkloadDetector tests detector creation with various options.
func TestNewWorkloadDetector(t *testing.T) {
	tests := []struct {
		name        string
		options     []DetectorOption
		wantWindow  time.Duration
		wantMinSize int
		wantCap     int
	}{
		{
			name:        "default configuration",
			options:     nil,
			wantWindow:  5 * time.Minute,
			wantMinSize: 10,
			wantCap:     10000,
		},
		{
			name: "custom window size",
			options: []DetectorOption{
				WithWindowSize(10 * time.Minute),
			},
			wantWindow:  10 * time.Minute,
			wantMinSize: 10,
			wantCap:     10000,
		},
		{
			name: "custom min sample size",
			options: []DetectorOption{
				WithMinSampleSize(50),
			},
			wantWindow:  5 * time.Minute,
			wantMinSize: 50,
			wantCap:     10000,
		},
		{
			name: "custom capacity",
			options: []DetectorOption{
				WithCapacity(1000),
			},
			wantWindow:  5 * time.Minute,
			wantMinSize: 10,
			wantCap:     1000,
		},
		{
			name: "all custom",
			options: []DetectorOption{
				WithWindowSize(3 * time.Minute),
				WithMinSampleSize(25),
				WithCapacity(5000),
			},
			wantWindow:  3 * time.Minute,
			wantMinSize: 25,
			wantCap:     5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewWorkloadDetector(tt.options...)
			defer detector.Close()

			assert.Equal(t, tt.wantWindow, detector.windowSize)
			assert.Equal(t, tt.wantMinSize, detector.minSampleSize)
			assert.Equal(t, tt.wantCap, detector.capacity)
			assert.NotNil(t, detector.events)
			assert.Equal(t, 0, detector.size)
			assert.False(t, detector.IsClosed())
		})
	}
}

// TestRecordOperation tests operation recording.
func TestRecordOperation(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(startTime)

	detector := NewWorkloadDetector(
		WithClock(clock),
		WithCapacity(10),
	)
	defer detector.Close()

	tests := []struct {
		name     string
		opType   OperationType
		fileSize uint64
		wantErr  bool
	}{
		{"delete operation", OpDelete, 1024, false},
		{"write operation", OpWrite, 2048, false},
		{"read operation", OpRead, 3072, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detector.RecordOperation(ctx, tt.opType, tt.fileSize)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	// Verify operations were recorded
	total, inWindow, _ := detector.GetStats()
	assert.Equal(t, 3, total)
	assert.Equal(t, 3, inWindow)
}

// TestRecordOperation_ContextCancellation tests context cancellation handling.
func TestRecordOperation_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	detector := NewWorkloadDetector()
	defer detector.Close()

	err := detector.RecordOperation(ctx, OpDelete, 1024)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// TestRecordOperation_Closed tests recording after close.
func TestRecordOperation_Closed(t *testing.T) {
	ctx := context.Background()
	detector := NewWorkloadDetector()

	// Close detector
	err := detector.Close()
	require.NoError(t, err)

	// Try to record operation
	err = detector.RecordOperation(ctx, OpDelete, 1024)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestRingBuffer tests ring buffer wrapping behavior.
func TestRingBuffer(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(startTime)

	detector := NewWorkloadDetector(
		WithClock(clock),
		WithCapacity(5), // Small capacity to test wrapping
	)
	defer detector.Close()

	// Record 10 operations (2x capacity)
	for i := 0; i < 10; i++ {
		err := detector.RecordOperation(ctx, OpWrite, uint64(i*1024))
		require.NoError(t, err)
		clock.Advance(1 * time.Second)
	}

	// Should only keep last 5 (capacity)
	total, _, _ := detector.GetStats()
	assert.Equal(t, 5, total)

	// Verify it's the last 5 operations (file sizes 5120-9216)
	features := detector.ExtractFeatures()
	assert.Equal(t, uint64(9*1024), features.FileSize) // Last recorded
}

// TestExtractFeatures tests feature extraction.
func TestExtractFeatures(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(startTime)

	detector := NewWorkloadDetector(
		WithClock(clock),
		WithWindowSize(1*time.Minute),
	)
	defer detector.Close()

	// Record operations: 6 deletes, 3 writes, 1 read (60% delete, 30% write, 10% read)
	for i := 0; i < 6; i++ {
		err := detector.RecordOperation(ctx, OpDelete, 1024)
		require.NoError(t, err)
		clock.Advance(1 * time.Second)
	}
	for i := 0; i < 3; i++ {
		err := detector.RecordOperation(ctx, OpWrite, 2048)
		require.NoError(t, err)
		clock.Advance(1 * time.Second)
	}
	err := detector.RecordOperation(ctx, OpRead, 3072)
	require.NoError(t, err)

	// Extract features
	features := detector.ExtractFeatures()

	assert.Equal(t, 10, features.SampleSize)
	assert.InDelta(t, 0.6, features.DeleteRatio, 0.01)
	assert.InDelta(t, 0.3, features.WriteRatio, 0.01)
	assert.InDelta(t, 0.1, features.ReadRatio, 0.01)
	assert.Equal(t, uint64(3072), features.FileSize)
	assert.Greater(t, features.OperationRate, 0.0)
	assert.True(t, features.IsValid())
}

// TestExtractFeatures_TimeWindow tests that old operations are excluded.
func TestExtractFeatures_TimeWindow(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(startTime)

	detector := NewWorkloadDetector(
		WithClock(clock),
		WithWindowSize(1*time.Minute),
	)
	defer detector.Close()

	// Record 5 old operations (outside window)
	for i := 0; i < 5; i++ {
		err := detector.RecordOperation(ctx, OpDelete, 1024)
		require.NoError(t, err)
		clock.Advance(1 * time.Second)
	}

	// Advance time past window
	clock.Advance(2 * time.Minute)

	// Record 3 new operations (inside window)
	for i := 0; i < 3; i++ {
		err := detector.RecordOperation(ctx, OpWrite, 2048)
		require.NoError(t, err)
		clock.Advance(1 * time.Second)
	}

	// Extract features - should only see new operations
	features := detector.ExtractFeatures()
	assert.Equal(t, 3, features.SampleSize)
	assert.InDelta(t, 1.0, features.WriteRatio, 0.01)  // 100% writes
	assert.InDelta(t, 0.0, features.DeleteRatio, 0.01) // 0% deletes
}

// TestDetectWorkloadType tests workload classification.
func TestDetectWorkloadType(t *testing.T) {
	tests := []struct {
		name       string
		operations []OperationType
		burst      bool // If true, compress time to create burst
		wantType   WorkloadType
	}{
		{
			name:       "batch deletion (high deletes + burst)",
			operations: []OperationType{OpDelete, OpDelete, OpDelete, OpDelete, OpDelete, OpDelete, OpDelete, OpWrite, OpWrite, OpRead},
			burst:      true,
			wantType:   WorkloadBatchDeletion,
		},
		{
			name:       "frequent writes (high writes, continuous)",
			operations: []OperationType{OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpRead, OpRead, OpDelete},
			burst:      false,
			wantType:   WorkloadFrequentWrites,
		},
		{
			name:       "append only (writes only, no deletes)",
			operations: []OperationType{OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite, OpWrite},
			burst:      false,
			wantType:   WorkloadAppendOnly,
		},
		{
			name:       "read heavy (mostly reads)",
			operations: []OperationType{OpRead, OpRead, OpRead, OpRead, OpRead, OpRead, OpRead, OpRead, OpWrite, OpDelete},
			burst:      false,
			wantType:   WorkloadReadHeavy,
		},
		{
			name:       "mixed read/write (balanced, low deletes)",
			operations: []OperationType{OpRead, OpWrite, OpRead, OpWrite, OpRead, OpWrite, OpRead, OpWrite, OpRead, OpWrite},
			burst:      false,
			wantType:   WorkloadMixedRW,
		},
		{
			name:       "unknown (insufficient data)",
			operations: []OperationType{OpRead, OpWrite, OpDelete}, // Only 3 ops, below minSampleSize
			burst:      false,
			wantType:   WorkloadUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			clock := NewMockClock(startTime)

			detector := NewWorkloadDetector(
				WithClock(clock),
				WithWindowSize(1*time.Minute),
				WithMinSampleSize(10),
			)
			defer detector.Close()

			// Record operations
			for _, opType := range tt.operations {
				err := detector.RecordOperation(ctx, opType, 1024)
				require.NoError(t, err)

				if tt.burst {
					// Burst: compress into 10 seconds (< 20% of 1 minute window)
					clock.Advance(1 * time.Second)
				} else {
					// Continuous: spread across 50 seconds (> 20% of window)
					clock.Advance(5 * time.Second)
				}
			}

			// Detect workload type
			workloadType := detector.DetectWorkloadType()
			assert.Equal(t, tt.wantType, workloadType, "Expected %s, got %s", tt.wantType, workloadType)
		})
	}
}

// TestGetStats tests statistics retrieval.
func TestGetStats(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(startTime)

	detector := NewWorkloadDetector(
		WithClock(clock),
		WithWindowSize(1*time.Minute),
	)
	defer detector.Close()

	// Record 5 operations
	for i := 0; i < 5; i++ {
		err := detector.RecordOperation(ctx, OpWrite, 1024)
		require.NoError(t, err)
		clock.Advance(10 * time.Second)
	}

	total, inWindow, windowSize := detector.GetStats()
	assert.Equal(t, 5, total)
	assert.Equal(t, 5, inWindow)
	assert.Equal(t, 1*time.Minute, windowSize)

	// Advance time to move 3 operations outside window
	clock.Advance(2 * time.Minute)

	// Add 2 more operations (now 7 total, but only 2 in window)
	for i := 0; i < 2; i++ {
		err := detector.RecordOperation(ctx, OpDelete, 2048)
		require.NoError(t, err)
		clock.Advance(5 * time.Second)
	}

	total, inWindow, windowSize = detector.GetStats()
	assert.Equal(t, 7, total)
	assert.Equal(t, 2, inWindow) // Only recent 2 in window
	assert.Equal(t, 1*time.Minute, windowSize)
}

// TestClose tests detector cleanup.
func TestClose(t *testing.T) {
	detector := NewWorkloadDetector()

	// Close once
	err := detector.Close()
	assert.NoError(t, err)
	assert.True(t, detector.IsClosed())

	// Close again (idempotent)
	err = detector.Close()
	assert.NoError(t, err)
	assert.True(t, detector.IsClosed())
}

// TestWorkloadFeatures_String tests string representation.
func TestWorkloadFeatures_String(t *testing.T) {
	features := WorkloadFeatures{
		DeleteRatio:    0.6,
		WriteRatio:     0.3,
		ReadRatio:      0.1,
		OperationRate:  10.5,
		BurstDetected:  true,
		FileSize:       1024 * 1024,
		WindowDuration: 5 * time.Minute,
		SampleSize:     100,
		ExtractedAt:    time.Now(),
	}

	str := features.String()
	assert.Contains(t, str, "Delete: 0.60")
	assert.Contains(t, str, "Write: 0.30")
	assert.Contains(t, str, "Read: 0.10")
	assert.Contains(t, str, "Burst: true")
	assert.Contains(t, str, "Samples: 100")
}

// TestOperationType_String tests operation type string conversion.
func TestOperationType_String(t *testing.T) {
	assert.Equal(t, "Read", OpRead.String())
	assert.Equal(t, "Write", OpWrite.String())
	assert.Equal(t, "Delete", OpDelete.String())
	assert.Equal(t, "Unknown", OperationType(999).String())
}

// TestWorkloadType_String tests workload type string conversion.
func TestWorkloadType_String(t *testing.T) {
	assert.Equal(t, "Unknown", WorkloadUnknown.String())
	assert.Equal(t, "BatchDeletion", WorkloadBatchDeletion.String())
	assert.Equal(t, "FrequentWrites", WorkloadFrequentWrites.String())
	assert.Equal(t, "MixedRW", WorkloadMixedRW.String())
	assert.Equal(t, "ReadHeavy", WorkloadReadHeavy.String())
	assert.Equal(t, "AppendOnly", WorkloadAppendOnly.String())
}

// BenchmarkRecordOperation benchmarks operation recording performance.
func BenchmarkRecordOperation(b *testing.B) {
	ctx := context.Background()
	detector := NewWorkloadDetector()
	defer detector.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detector.RecordOperation(ctx, OpWrite, uint64(i))
	}
}

// BenchmarkExtractFeatures benchmarks feature extraction performance.
func BenchmarkExtractFeatures(b *testing.B) {
	ctx := context.Background()
	detector := NewWorkloadDetector()
	defer detector.Close()

	// Pre-populate with 1000 operations
	for i := 0; i < 1000; i++ {
		_ = detector.RecordOperation(ctx, OpWrite, uint64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detector.ExtractFeatures()
	}
}

// BenchmarkDetectWorkloadType benchmarks workload classification performance.
func BenchmarkDetectWorkloadType(b *testing.B) {
	ctx := context.Background()
	detector := NewWorkloadDetector()
	defer detector.Close()

	// Pre-populate with 1000 operations
	for i := 0; i < 1000; i++ {
		_ = detector.RecordOperation(ctx, OpWrite, uint64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detector.DetectWorkloadType()
	}
}

// BenchmarkConcurrentRecording benchmarks concurrent operation recording.
func BenchmarkConcurrentRecording(b *testing.B) {
	ctx := context.Background()
	detector := NewWorkloadDetector()
	defer detector.Close()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = detector.RecordOperation(ctx, OpWrite, 1024)
		}
	})
}
