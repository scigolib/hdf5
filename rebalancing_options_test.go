package hdf5

import (
	"os"
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/structures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFunctionalOptions_Default tests default behavior (NO rebalancing).
func TestFunctionalOptions_Default(t *testing.T) {
	tempFile := "testdata/functional_options_default.h5"
	defer os.Remove(tempFile)

	// Default: NO rebalancing (like C library)
	fw, err := CreateForWrite(tempFile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Verify NO rebalancing configs
	assert.Nil(t, fw.lazyRebalancingConfig, "Default should have NO lazy rebalancing")
	assert.Nil(t, fw.incrementalRebalancingConfig, "Default should have NO incremental rebalancing")
	assert.Nil(t, fw.smartRebalancingConfig, "Default should have NO smart rebalancing")
}

// TestFunctionalOptions_LazyRebalancing tests lazy rebalancing options.
func TestFunctionalOptions_LazyRebalancing(t *testing.T) {
	tempFile := "testdata/functional_options_lazy.h5"
	defer os.Remove(tempFile)

	// Enable lazy rebalancing with custom config
	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithLazyRebalancing(
			LazyThreshold(0.10),
			LazyMaxDelay(10*time.Minute),
			LazyBatchSize(200),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Verify lazy config is set
	require.NotNil(t, fw.lazyRebalancingConfig)
	assert.Equal(t, 0.10, fw.lazyRebalancingConfig.Threshold)
	assert.Equal(t, 10*time.Minute, fw.lazyRebalancingConfig.MaxDelay)
	assert.Equal(t, 200, fw.lazyRebalancingConfig.BatchSize)
}

// TestFunctionalOptions_IncrementalRebalancing tests incremental rebalancing options.
func TestFunctionalOptions_IncrementalRebalancing(t *testing.T) {
	tempFile := "testdata/functional_options_incremental.h5"
	defer os.Remove(tempFile)

	// Enable incremental rebalancing with custom config
	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithIncrementalRebalancing(
			IncrementalBudget(200*time.Millisecond),
			IncrementalInterval(10*time.Second),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Verify incremental config is set
	require.NotNil(t, fw.incrementalRebalancingConfig)
	assert.Equal(t, 200*time.Millisecond, fw.incrementalRebalancingConfig.Budget)
	assert.Equal(t, 10*time.Second, fw.incrementalRebalancingConfig.Interval)
}

// TestFunctionalOptions_Combined tests combining multiple options.
func TestFunctionalOptions_Combined(t *testing.T) {
	tempFile := "testdata/functional_options_combined.h5"
	defer os.Remove(tempFile)

	// Combine lazy + incremental (prerequisite for incremental)
	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithLazyRebalancing(
			LazyThreshold(0.05),
		),
		WithIncrementalRebalancing(
			IncrementalBudget(100*time.Millisecond),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Verify both configs are set
	assert.NotNil(t, fw.lazyRebalancingConfig)
	assert.NotNil(t, fw.incrementalRebalancingConfig)
	assert.Equal(t, 0.05, fw.lazyRebalancingConfig.Threshold)
	assert.Equal(t, 100*time.Millisecond, fw.incrementalRebalancingConfig.Budget)
}

// TestFunctionalOptions_SmartRebalancing tests smart rebalancing options (placeholder).
func TestFunctionalOptions_SmartRebalancing(t *testing.T) {
	tempFile := "testdata/functional_options_smart.h5"
	defer os.Remove(tempFile)

	// Smart rebalancing (Phase 3 - not fully implemented yet)
	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithSmartRebalancing(
			SmartAutoDetect(true),
			SmartAutoSwitch(true),
			SmartMinFileSize(10*MB),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// For now, smart rebalancing is a placeholder
	// Full implementation in Phase 3
	t.Log("Smart rebalancing API tested (placeholder for Phase 3)")
}

// TestFunctionalOptions_DefaultLazyConfig tests default lazy configuration.
func TestFunctionalOptions_DefaultLazyConfig(t *testing.T) {
	tempFile := "testdata/functional_options_default_lazy.h5"
	defer os.Remove(tempFile)

	// Use default lazy config (no options)
	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithLazyRebalancing(), // No options = defaults
	)
	require.NoError(t, err)
	defer fw.Close()

	// Verify default values
	require.NotNil(t, fw.lazyRebalancingConfig)
	assert.Equal(t, 0.05, fw.lazyRebalancingConfig.Threshold)         // Default 5%
	assert.Equal(t, 5*time.Minute, fw.lazyRebalancingConfig.MaxDelay) // Default 5 min
	assert.Equal(t, 100, fw.lazyRebalancingConfig.BatchSize)          // Default 100
}

// TestFunctionalOptions_ProgressCallback tests incremental progress callback.
func TestFunctionalOptions_ProgressCallback(t *testing.T) {
	tempFile := "testdata/functional_options_callback.h5"
	defer os.Remove(tempFile)

	fw, err := CreateForWrite(tempFile, CreateTruncate,
		WithIncrementalRebalancing(
			IncrementalProgressCallback(func(p structures.RebalancingProgress) {
				t.Logf("Progress: %d nodes rebalanced, %d remaining", p.NodesRebalanced, p.NodesRemaining)
			}),
		),
	)
	require.NoError(t, err)
	defer fw.Close()

	// Verify callback is set (won't be called in this test since no rebalancing happens)
	require.NotNil(t, fw.incrementalRebalancingConfig)
	assert.NotNil(t, fw.incrementalRebalancingConfig.ProgressCallback)

	// Note: Callback won't actually be called in this test since we don't trigger rebalancing
	// This just verifies the API works
	t.Log("Progress callback API tested (callback won't fire without rebalancing operations)")
}
