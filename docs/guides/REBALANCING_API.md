# Rebalancing API Reference

> **Complete API documentation for HDF5 B-tree rebalancing options**

This document provides comprehensive API documentation for configuring B-tree rebalancing in the HDF5 Go library.

---

## Table of Contents

1. [Overview](#overview)
2. [Lazy Rebalancing API](#lazy-rebalancing-api)
3. [Incremental Rebalancing API](#incremental-rebalancing-api)
4. [Smart Rebalancing API](#smart-rebalancing-api)
5. [Progress Monitoring](#progress-monitoring)
6. [Complete Examples](#complete-examples)
7. [Migration Guide](#migration-guide)

---

## Overview

### Functional Options Pattern

This library uses the **Functional Options Pattern** (Go standard practice as of 2025) for configuring file writers:

```go
fw, err := hdf5.CreateForWrite(filename, mode, ...options)
```

**Benefits**:
- Optional configuration (sensible defaults)
- Composable (combine multiple options)
- Backward compatible (add new options without breaking API)
- Self-documenting (clear option names)

### Three Rebalancing Modes

| Mode | Function | Use Case |
|------|----------|----------|
| **Default** | (no options) | Append-only, small files |
| **Lazy** | `WithLazyRebalancing()` | Batch deletions |
| **Incremental** | `WithIncrementalRebalancing()` | Large files, continuous ops |
| **Smart** | `WithSmartRebalancing()` | Auto-tuning, unknown workloads |

**Import**:

```go
import "github.com/scigolib/hdf5"
```

---

## Lazy Rebalancing API

### WithLazyRebalancing

```go
func WithLazyRebalancing(opts ...LazyOption) FileWriterOption
```

**Description**: Enables lazy (batch) rebalancing mode.

Lazy rebalancing accumulates deletions and triggers batch rebalancing when a threshold is reached. This is **10-100x faster** than immediate rebalancing for deletion-heavy workloads.

**Parameters**:
- `opts ...LazyOption`: Optional configuration (defaults used if omitted)

**Default Configuration** (if no options provided):
- Threshold: 0.05 (5% underflow)
- MaxDelay: 5 minutes
- BatchSize: 100 nodes

**Returns**: `FileWriterOption` to pass to `CreateForWrite()`

**Example**:

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),
        hdf5.LazyMaxDelay(5*time.Minute),
        hdf5.LazyBatchSize(100),
    ),
)
if err != nil {
    return err
}
defer fw.Close()
```

**Reference**: [docs/dev/BTREE_PERFORMANCE_ANALYSIS.md](../dev/BTREE_PERFORMANCE_ANALYSIS.md)

---

### LazyThreshold

```go
func LazyThreshold(threshold float64) LazyOption
```

**Description**: Sets the underflow threshold for triggering batch rebalancing.

When `(underflow_nodes / total_nodes) ≥ threshold`, batch rebalancing is triggered.

**Parameters**:
- `threshold`: Ratio of underflow nodes to total nodes

**Range**: 0.01 (1%) to 0.20 (20%)

**Default**: 0.05 (5%)

**Examples**:

```go
// Conservative (rebalance more often, tighter tree)
hdf5.LazyThreshold(0.02)

// Default (balanced)
hdf5.LazyThreshold(0.05)

// Aggressive (rebalance less often, looser tree)
hdf5.LazyThreshold(0.10)
```

**Use Cases**:
- **Lower (0.02)**: Disk space limited, search performance critical
- **Higher (0.10)**: Write performance critical, disk space abundant

---

### LazyMaxDelay

```go
func LazyMaxDelay(delay time.Duration) LazyOption
```

**Description**: Sets the maximum time before forcing batch rebalancing.

Even if threshold is not reached, rebalancing will trigger after this duration. This prevents indefinite delay in write-only workloads.

**Parameters**:
- `delay`: Maximum duration before forced rebalancing

**Range**: 1 second to 1 hour (practical range)

**Default**: 5 minutes

**Examples**:

```go
// Short delay (ensure timely rebalancing)
hdf5.LazyMaxDelay(1*time.Minute)

// Default
hdf5.LazyMaxDelay(5*time.Minute)

// Long delay (minimize interruptions)
hdf5.LazyMaxDelay(30*time.Minute)
```

**Use Cases**:
- **Shorter (1 min)**: Predictable rebalancing, file size growth is concern
- **Longer (30 min)**: Long write sessions, can't afford frequent interruptions

---

### LazyBatchSize

```go
func LazyBatchSize(size int) LazyOption
```

**Description**: Sets the number of nodes to rebalance per batch operation.

Larger batches = more work per rebalancing, but fewer total operations.

**Parameters**:
- `size`: Number of nodes per batch

**Range**: 10 to 1000 nodes (practical range)

**Default**: 100 nodes

**Examples**:

```go
// Small batches (shorter pauses, more frequent)
hdf5.LazyBatchSize(50)

// Default
hdf5.LazyBatchSize(100)

// Large batches (longer pauses, fewer events)
hdf5.LazyBatchSize(200)
```

**Pause Time Estimates**:
- 50 nodes: ~50-100ms pause
- 100 nodes: ~100-200ms pause
- 200 nodes: ~200-500ms pause

**Use Cases**:
- **Smaller (50)**: Latency-sensitive applications
- **Larger (200)**: Batch processing jobs

---

## Incremental Rebalancing API

### WithIncrementalRebalancing

```go
func WithIncrementalRebalancing(opts ...IncrementalOption) FileWriterOption
```

**Description**: Enables incremental (background) rebalancing mode.

Incremental rebalancing processes underflow nodes in the **background** using a goroutine with time budgets. This provides **zero user-visible pause** for TB-scale scientific data.

**IMPORTANT**: Requires lazy rebalancing to be enabled first (prerequisite).

**Parameters**:
- `opts ...IncrementalOption`: Optional configuration (defaults used if omitted)

**Default Configuration** (if no options provided):
- Budget: 100ms per session
- Interval: 5 seconds between sessions
- ProgressCallback: nil (no callback)

**Returns**: `FileWriterOption` to pass to `CreateForWrite()`

**Example**:

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(),  // REQUIRED prerequisite!
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond),
        hdf5.IncrementalInterval(5*time.Second),
        hdf5.IncrementalProgressCallback(func(p hdf5.RebalancingProgress) {
            fmt.Printf("Rebalanced: %d/%d nodes\n", p.NodesRebalanced, p.NodesRemaining)
        }),
    ),
)
if err != nil {
    return err
}
defer fw.Close()  // IMPORTANT: Stops background goroutine!
```

**Reference**: [docs/dev/BTREE_PERFORMANCE_ANALYSIS.md](../dev/BTREE_PERFORMANCE_ANALYSIS.md) lines 397-446

---

### IncrementalBudget

```go
func IncrementalBudget(budget time.Duration) IncrementalOption
```

**Description**: Sets the time budget per rebalancing session.

The background goroutine will rebalance for this duration, then pause until the next interval.

**Parameters**:
- `budget`: Time budget per session

**Range**: 10ms to 1 second (practical range)

**Default**: 100ms

**Examples**:

```go
// Low CPU impact (slower rebalancing)
hdf5.IncrementalBudget(50*time.Millisecond)

// Default (balanced)
hdf5.IncrementalBudget(100*time.Millisecond)

// High throughput (faster rebalancing, higher overhead)
hdf5.IncrementalBudget(200*time.Millisecond)
```

**CPU Overhead Estimates**:
- 50ms budget, 10s interval: ~0.5% CPU
- 100ms budget, 5s interval: ~2% CPU
- 200ms budget, 2s interval: ~10% CPU

**Use Cases**:
- **Smaller (50ms)**: CPU constrained environments
- **Larger (200ms)**: Need aggressive rebalancing, CPU available

---

### IncrementalInterval

```go
func IncrementalInterval(interval time.Duration) IncrementalOption
```

**Description**: Sets how often to run rebalancing sessions.

The background goroutine wakes up every `interval` and rebalances for `budget` time.

**Parameters**:
- `interval`: Time between rebalancing sessions

**Range**: 1 second to 1 minute (practical range)

**Default**: 5 seconds

**Examples**:

```go
// Infrequent (low overhead, batching effect)
hdf5.IncrementalInterval(10*time.Second)

// Default (balanced)
hdf5.IncrementalInterval(5*time.Second)

// Frequent (aggressive rebalancing)
hdf5.IncrementalInterval(2*time.Second)
```

**Trade-off**:
- **Shorter interval**: More responsive rebalancing, higher overhead
- **Longer interval**: Lower overhead, more batching (may build backlog)

**Use Cases**:
- **Shorter (2s)**: High delete rate, need to prevent backlog
- **Longer (10s)**: Low delete rate, minimize overhead

---

### IncrementalProgressCallback

```go
func IncrementalProgressCallback(callback func(RebalancingProgress)) IncrementalOption
```

**Description**: Sets a callback for progress updates.

The callback is called after each rebalancing session with progress information. Optional: can be `nil` for no progress reporting.

**Parameters**:
- `callback`: Function called with progress updates

**Callback Signature**:

```go
type RebalancingProgress struct {
    NodesRebalanced    int           // Nodes rebalanced in this session
    NodesRemaining     int           // Nodes still waiting for rebalancing
    EstimatedRemaining time.Duration // ETA to complete (estimate)
}
```

**Example**:

```go
hdf5.IncrementalProgressCallback(func(p hdf5.RebalancingProgress) {
    log.Printf("Rebalancing: %d/%d nodes (ETA: %v)\n",
        p.NodesRebalanced,
        p.NodesRemaining,
        p.EstimatedRemaining,
    )

    // Alert if backlog is building up
    if p.NodesRemaining > 1000 {
        log.Warn("Large rebalancing backlog: %d nodes", p.NodesRemaining)
    }
})
```

**Best Practices**:
- **Always set callback** for production systems (visibility)
- **Alert on large backlogs** (may need to tune Budget/Interval)
- **Log progress periodically** (debugging performance issues)

---

## Smart Rebalancing API

### WithSmartRebalancing

```go
func WithSmartRebalancing(opts ...SmartOption) FileWriterOption
```

**Description**: Enables smart (auto-tuning) rebalancing mode.

Smart rebalancing automatically detects workload patterns and selects the optimal rebalancing mode (none, lazy, or incremental) based on:
- File size
- Operation patterns (delete ratio, batch size)
- Resource constraints (CPU, memory limits)

This is the **"auto-pilot" mode** for scientific data workflows.

**IMPORTANT**: This is **OPTIONAL** and must be explicitly enabled. By default (no options), NO rebalancing is performed (like C library).

**Parameters**:
- `opts ...SmartOption`: Optional configuration (defaults used if omitted)

**Default Configuration** (if no options provided):
- AutoDetect: true (enabled)
- AutoSwitch: true (enabled)
- MinFileSize: 10 MB
- AllowedModes: all ("none", "lazy", "incremental")
- OnModeChange: nil (no callback)

**Returns**: `FileWriterOption` to pass to `CreateForWrite()`

**Example**:

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithSmartRebalancing(
        hdf5.SmartAutoDetect(true),
        hdf5.SmartAutoSwitch(true),
        hdf5.SmartMinFileSize(10*hdf5.MB),
        hdf5.SmartAllowedModes("lazy", "incremental"),
        hdf5.SmartOnModeChange(func(d hdf5.ModeDecision) {
            log.Printf("Mode: %s (confidence: %.1f%%): %s",
                d.SelectedMode, d.Confidence*100, d.Reason)
        }),
    ),
)
if err != nil {
    return err
}
defer fw.Close()
```

**Reference**: Phase 3 design (2025 best practices)

---

### SmartAutoDetect

```go
func SmartAutoDetect(enabled bool) SmartOption
```

**Description**: Enables automatic workload pattern detection.

When enabled, the library tracks operations and extracts features to classify workload type.

**Parameters**:
- `enabled`: true to enable detection, false to disable

**Default**: true

**Example**:

```go
hdf5.SmartAutoDetect(true)  // Enable detection (default)
hdf5.SmartAutoDetect(false) // Disable detection (manual mode selection)
```

**When to disable**: If you want smart config but manual mode selection.

---

### SmartAutoSwitch

```go
func SmartAutoSwitch(enabled bool) SmartOption
```

**Description**: Enables automatic mode switching based on detected patterns.

When enabled, the library can switch rebalancing modes as workload changes.

**Parameters**:
- `enabled`: true to allow switching, false for initial selection only

**Default**: true

**Example**:

```go
hdf5.SmartAutoSwitch(true)  // Allow mode switching (default)
hdf5.SmartAutoSwitch(false) // Initial selection only, no switching
```

**When to disable**: If mode switching causes performance jitter, disable to keep initial selection stable.

---

### SmartMinFileSize

```go
func SmartMinFileSize(size uint64) SmartOption
```

**Description**: Sets the minimum file size for enabling auto-rebalancing.

Files smaller than this size will not trigger automatic rebalancing (overhead not worth it).

**Parameters**:
- `size`: Minimum file size in bytes

**Constants Available**:
- `hdf5.KB = 1024`
- `hdf5.MB = 1024 * KB`
- `hdf5.GB = 1024 * MB`

**Default**: 10 MB

**Examples**:

```go
hdf5.SmartMinFileSize(1*hdf5.MB)   // Aggressive (even small files)
hdf5.SmartMinFileSize(10*hdf5.MB)  // Default
hdf5.SmartMinFileSize(100*hdf5.MB) // Conservative (only large files)
```

**Use Cases**:
- **Lower (1 MB)**: Optimize even small files
- **Higher (100 MB)**: Only large files benefit from rebalancing

---

### SmartAllowedModes

```go
func SmartAllowedModes(modes ...string) SmartOption
```

**Description**: Restricts which rebalancing modes can be auto-selected.

**Parameters**:
- `modes`: List of allowed mode names

**Valid Mode Names**:
- `"none"`: No rebalancing
- `"lazy"`: Lazy (batch) rebalancing
- `"incremental"`: Incremental (background) rebalancing

**Default**: All modes allowed

**Examples**:

```go
// Only lazy and incremental (never "none")
hdf5.SmartAllowedModes("lazy", "incremental")

// Only incremental (force background rebalancing)
hdf5.SmartAllowedModes("incremental")

// All modes (default)
hdf5.SmartAllowedModes("none", "lazy", "incremental")
```

**Use Cases**:
- **Policy enforcement**: Force specific modes for organizational standards
- **Performance guarantee**: Ensure rebalancing always enabled

---

### SmartOnModeChange

```go
func SmartOnModeChange(callback func(ModeDecision)) SmartOption
```

**Description**: Sets a callback for mode change notifications.

The callback receives a `ModeDecision` explaining why a mode was selected or changed.

**Parameters**:
- `callback`: Function called when mode changes

**Callback Signature**:

```go
type ModeDecision struct {
    SelectedMode string             // Mode selected ("none", "lazy", "incremental")
    Reason       string             // Human-readable reason
    Confidence   float64            // Confidence level [0, 1]
    Factors      map[string]float64 // Factors that influenced decision
    Timestamp    time.Time          // When decision was made
}
```

**Example**:

```go
hdf5.SmartOnModeChange(func(d hdf5.ModeDecision) {
    log.Printf("Mode Change: %s (confidence: %.1f%%)\n",
        d.SelectedMode, d.Confidence*100)
    log.Printf("Reason: %s\n", d.Reason)
    log.Printf("Factors: %+v\n", d.Factors)
    log.Printf("Timestamp: %v\n", d.Timestamp)

    // Send to monitoring system
    metrics.RecordModeChange(d.SelectedMode, d.Confidence)
})
```

**Factors Map** (example values):

```go
{
    "delete_ratio": 0.25,      // 25% of operations are deletes
    "batch_size": 150,         // Average batch size
    "operation_rate": 1500,    // Operations per second
    "file_size": 5.0e8,        // 500 MB
}
```

**Best Practices**:
- **Always set callback** to understand auto-tuning decisions
- **Log decisions** for debugging
- **Record metrics** for monitoring

---

## Progress Monitoring

### RebalancingProgress Type

```go
type RebalancingProgress struct {
    NodesRebalanced    int           // Nodes rebalanced in this session
    NodesRemaining     int           // Nodes still waiting
    EstimatedRemaining time.Duration // ETA to complete (estimate)
}
```

**Description**: Provides progress information for incremental rebalancing.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `NodesRebalanced` | `int` | Number of nodes rebalanced in current session |
| `NodesRemaining` | `int` | Number of underflow nodes still waiting |
| `EstimatedRemaining` | `time.Duration` | Estimated time to complete rebalancing |

**Usage Example**:

```go
hdf5.IncrementalProgressCallback(func(p hdf5.RebalancingProgress) {
    totalNodes := p.NodesRebalanced + p.NodesRemaining
    percentComplete := float64(p.NodesRebalanced) / float64(totalNodes) * 100

    fmt.Printf("Rebalancing Progress: %.1f%% complete (%d/%d nodes)\n",
        percentComplete, p.NodesRebalanced, totalNodes)
    fmt.Printf("Estimated time remaining: %v\n", p.EstimatedRemaining)

    // Alert conditions
    if p.NodesRemaining > 1000 {
        log.Warn("Large backlog: %d nodes", p.NodesRemaining)
    }
})
```

---

### ModeDecision Type

```go
type ModeDecision struct {
    SelectedMode string             // "none", "lazy", or "incremental"
    Reason       string             // Human-readable explanation
    Confidence   float64            // 0.0 to 1.0
    Factors      map[string]float64 // Detection metrics
    Timestamp    time.Time          // When decision made
}
```

**Description**: Explains why a rebalancing mode was selected (smart rebalancing).

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `SelectedMode` | `string` | Mode selected: "none", "lazy", "incremental" |
| `Reason` | `string` | Human-readable explanation |
| `Confidence` | `float64` | Confidence level (0.0 = uncertain, 1.0 = certain) |
| `Factors` | `map[string]float64` | Metrics that influenced decision |
| `Timestamp` | `time.Time` | When decision was made |

**Example Values**:

```go
ModeDecision{
    SelectedMode: "lazy",
    Reason: "High delete ratio (25%) with large batch size (150 ops/batch)",
    Confidence: 0.85,
    Factors: map[string]float64{
        "delete_ratio":    0.25,
        "batch_size":      150,
        "operation_rate":  800,
        "file_size":       2.5e8,  // 250 MB
    },
    Timestamp: time.Now(),
}
```

---

## Complete Examples

### Example 1: Default (No Rebalancing)

```go
package main

import (
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // No options = no rebalancing (like HDF5 C library)
    fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()

    // Create dataset
    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
    if err != nil {
        log.Fatal(err)
    }

    // Write and delete - fast, no rebalancing overhead
    ds.WriteAttribute("attr1", 42)
    ds.DeleteAttribute("attr1")  // Fast! No rebalancing
}
```

**Use Case**: Append-only workloads, small files

---

### Example 2: Lazy Rebalancing

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/scigolib/hdf5"
)

func main() {
    // Enable lazy rebalancing for batch deletions
    fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
        hdf5.WithLazyRebalancing(
            hdf5.LazyThreshold(0.05),           // 5% underflow
            hdf5.LazyMaxDelay(5*time.Minute),   // Force after 5 min
            hdf5.LazyBatchSize(100),            // 100 nodes per batch
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()

    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
    if err != nil {
        log.Fatal(err)
    }

    // Create many attributes
    for i := 0; i < 1000; i++ {
        ds.WriteAttribute(fmt.Sprintf("attr_%d", i), i)
    }

    // Delete many - rebalancing happens in batches
    for i := 0; i < 500; i++ {
        ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
        // Automatic batch rebalancing when threshold reached
    }

    log.Println("Batch deletions complete (10-100x faster than immediate rebalancing)")
}
```

**Use Case**: Batch deletion workloads

---

### Example 3: Incremental Rebalancing

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/scigolib/hdf5"
)

func main() {
    // Enable incremental rebalancing for large files
    fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
        hdf5.WithLazyRebalancing(),  // Prerequisite!
        hdf5.WithIncrementalRebalancing(
            hdf5.IncrementalBudget(100*time.Millisecond),
            hdf5.IncrementalInterval(5*time.Second),
            hdf5.IncrementalProgressCallback(func(p hdf5.RebalancingProgress) {
                log.Printf("Rebalancing: %d/%d nodes (ETA: %v)\n",
                    p.NodesRebalanced, p.NodesRemaining, p.EstimatedRemaining)

                // Alert if backlog builds up
                if p.NodesRemaining > 1000 {
                    log.Printf("WARNING: Large backlog: %d nodes", p.NodesRemaining)
                }
            }),
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()  // Stops background goroutine

    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10000})
    if err != nil {
        log.Fatal(err)
    }

    // Create and delete - ZERO user-visible pause!
    for i := 0; i < 10000; i++ {
        ds.WriteAttribute(fmt.Sprintf("attr_%d", i), i)
    }

    for i := 0; i < 5000; i++ {
        ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
        // NO PAUSE! Rebalancing happens in background
    }

    log.Println("Deletions complete with zero pause (background rebalancing)")
}
```

**Use Case**: Large files (>500MB), continuous operations

---

### Example 4: Smart Rebalancing

```go
package main

import (
    "fmt"
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // Enable smart rebalancing for auto-tuning
    fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
        hdf5.WithSmartRebalancing(
            hdf5.SmartAutoDetect(true),
            hdf5.SmartAutoSwitch(true),
            hdf5.SmartMinFileSize(10*hdf5.MB),
            hdf5.SmartAllowedModes("lazy", "incremental"),
            hdf5.SmartOnModeChange(func(d hdf5.ModeDecision) {
                log.Printf("Mode: %s (confidence: %.1f%%)\n",
                    d.SelectedMode, d.Confidence*100)
                log.Printf("Reason: %s\n", d.Reason)
            }),
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()

    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
    if err != nil {
        log.Fatal(err)
    }

    // Phase 1: Batch writes (library auto-selects "none" or "lazy")
    log.Println("Phase 1: Batch writes")
    for i := 0; i < 1000; i++ {
        ds.WriteAttribute(fmt.Sprintf("attr_%d", i), i)
    }

    // Phase 2: Batch deletes (library may switch to "lazy")
    log.Println("Phase 2: Batch deletes")
    for i := 0; i < 500; i++ {
        ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
    }

    // Phase 3: Mixed operations (library may switch to "incremental")
    log.Println("Phase 3: Mixed operations")
    for i := 500; i < 1000; i++ {
        if i%2 == 0 {
            ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
        } else {
            ds.WriteAttribute(fmt.Sprintf("new_%d", i), i*2)
        }
    }

    log.Println("Complete - library auto-selected optimal modes")
}
```

**Use Case**: Unknown workloads, auto-pilot mode

---

## Migration Guide

### From Default (No Rebalancing) to Lazy

**Before**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
```

**After**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(),  // Use defaults
)
```

**Impact**:
- ~2% overhead
- Occasional 100-500ms pauses for batch rebalancing
- B-tree stays compact (disk space savings)

---

### From Lazy to Incremental

**Before**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(),
)
```

**After**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(),  // Still required!
    hdf5.WithIncrementalRebalancing(),  // Add this
)
defer fw.Close()  // IMPORTANT: stops background goroutine
```

**Impact**:
- Zero user-visible pause (all rebalancing in background)
- ~4% overhead (background goroutine)
- Must call `Close()` to stop background goroutine

---

### From Manual Configuration to Smart

**Before**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),
        // ... manual tuning ...
    ),
)
```

**After**:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithSmartRebalancing(),  // Auto-tuning!
)
```

**Impact**:
- ~6% overhead (detection + evaluation)
- Library adapts to workload automatically
- No manual tuning required
- Consider setting `OnModeChange` callback for transparency

---

## API Stability

**Stability Guarantee**:
- ✅ All APIs documented here are **stable** and production-ready
- ✅ Option names, signatures, and semantics will not change (backward compatible)
- ✅ New options may be added (won't break existing code)

**Deprecation Policy**:
- If an option needs to change, it will be deprecated first (1 major version)
- Deprecated options will continue to work (warnings only)
- Migration path will be clearly documented

---

## See Also

- **[Performance Tuning Guide](PERFORMANCE_TUNING.md)**: Comprehensive guide with benchmarks, recommendations, troubleshooting
- **[FAQ](FAQ.md)**: Common questions about rebalancing
- **[Examples](../../examples/)**: Working code examples

---

**Last Updated**: 2025-11-13
**API Status**: Stable
