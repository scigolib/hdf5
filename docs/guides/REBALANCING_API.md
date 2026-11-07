# Rebalancing API Reference

Reference for the B-tree rebalancing options of the HDF5 Go library.

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

### Rebalancing Modes

| Mode | Function | Use Case |
|------|----------|----------|
| **Default (immediate)** | (no options) | Few deletions; the C library's behavior |
| **None** | `WithBTreeRebalancing(false)` | Few deletions, or a batch followed by a manual rebalance |
| **Lazy** | `WithLazyRebalancing()` | Batch deletions |
| **Incremental** | `WithIncrementalRebalancing()` | Large files, continuous ops |
| **Smart** | `WithSmartRebalancing()` | Not implemented yet: accepted, no effect |

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

Lazy rebalancing accumulates deletions and triggers batch rebalancing when a threshold is reached.

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

The pause grows with the batch size; measure it on your files.

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

Incremental rebalancing processes underflow nodes in the **background** using a goroutine with time budgets. Deletes do not wait for it.

Note: it requires lazy rebalancing to be enabled first (prerequisite).

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
    hdf5.WithLazyRebalancing(),  // required by incremental mode
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond),
        hdf5.IncrementalInterval(5*time.Second),
    ),
)
if err != nil {
    return err
}
defer fw.Close()  // stops the background goroutine
```


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

The share of time spent rebalancing is at most about `budget / interval` (100ms every 5 seconds for the defaults).

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

**Description**: Sets a callback for progress updates, called after each rebalancing session.

The parameter type `RebalancingProgress` is declared in `internal/structures` (the real
signature is `func IncrementalProgressCallback(callback func(structures.RebalancingProgress)) IncrementalOption`).
Code outside this module cannot import an internal package, so it cannot write a function of
that type: use [`FileWriter.GetIncrementalRebalancingProgress`](#progress-monitoring) to poll the
progress instead.

---

## Smart Rebalancing API

`WithSmartRebalancing` and the options `SmartAutoDetect`, `SmartAutoSwitch`, `SmartMinFileSize`,
`SmartAllowedModes` and `SmartOnModeChange` are part of the API, but smart rebalancing is not
implemented: `WithSmartRebalancing` accepts its options and does nothing. A file created with it
rebalances exactly like one created without options. Select `WithLazyRebalancing` or
`WithIncrementalRebalancing` explicitly.

---

## Progress Monitoring

### FileWriter.GetIncrementalRebalancingProgress

```go
func (fw *FileWriter) GetIncrementalRebalancingProgress() (structures.RebalancingProgress, error)
```

Returns the progress of the background rebalancing. The result type is declared in an internal
package of this module; its fields are usable from outside:

| Field | Type | Description |
|-------|------|-------------|
| `NodesRebalanced` | `int` | Nodes rebalanced so far |
| `NodesRemaining` | `int` | Underflow nodes still pending |
| `SessionDuration` | `time.Duration` | How long the last session took |
| `EstimatedRemaining` | `time.Duration` | Estimated time to finish the pending work |
| `IsComplete` | `bool` | True when all pending rebalancing is done |

```go
progress, err := fw.GetIncrementalRebalancingProgress()
if err != nil {
    log.Printf("progress: %v", err)
} else if progress.NodesRemaining > 1000 {
    log.Printf("rebalancing backlog: %d nodes", progress.NodesRemaining)
}
```

Other state queries: `IsIncrementalRebalancingEnabled`, `IsLazyRebalancingEnabled`,
`GetLazyRebalancingStats` and `RebalancingEnabled`.

---

## Complete Examples

### Example 1: No Rebalancing

```go
package main

import (
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // No options = rebalance on every delete, as the HDF5 C library does for
    // version 2 B-trees. WithBTreeRebalancing(false) turns rebalancing off.
    fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
        hdf5.WithBTreeRebalancing(false),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()

    // Create dataset
    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
    if err != nil {
        log.Fatal(err)
    }

    // Write and delete: no rebalancing on delete
    if err := ds.WriteAttribute("attr1", int32(42)); err != nil {
        log.Fatal(err)
    }
    if err := ds.DeleteAttribute("attr1"); err != nil {
        log.Fatal(err)
    }
}
```

Use case: few deletions, or a batch followed by a manual rebalance.

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
        if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
            log.Fatal(err)
        }
    }

    // Delete many; rebalancing happens in batches when the threshold is reached
    for i := 0; i < 500; i++ {
        if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
            log.Fatal(err)
        }
    }

    log.Println("Batch deletions complete")
}
```

Use case: batch deletion workloads.

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
        hdf5.WithLazyRebalancing(), // required by incremental mode
        hdf5.WithIncrementalRebalancing(
            hdf5.IncrementalBudget(100*time.Millisecond),
            hdf5.IncrementalInterval(5*time.Second),
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close() // stops the background goroutine

    ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10000})
    if err != nil {
        log.Fatal(err)
    }

    for i := 0; i < 10000; i++ {
        if err := ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i)); err != nil {
            log.Fatal(err)
        }
    }

    // Rebalancing runs in the background goroutine
    for i := 0; i < 5000; i++ {
        if err := ds.DeleteAttribute(fmt.Sprintf("attr_%d", i)); err != nil {
            log.Fatal(err)
        }
    }

    progress, err := fw.GetIncrementalRebalancingProgress()
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("rebalanced %d nodes, %d remaining", progress.NodesRebalanced, progress.NodesRemaining)
}
```

Use case: large files with continuous operations.

---

## Migration Guide

### From Default (Immediate) to Lazy

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

Impact:
- Deletes no longer rebalance one by one; batches are rebalanced when the threshold is reached
- A pause while each batch runs
- The B-tree stays compact between batches

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
    hdf5.WithLazyRebalancing(),        // still required
    hdf5.WithIncrementalRebalancing(), // added
)
defer fw.Close() // stops the background goroutine
```

Impact:
- Rebalancing runs in a background goroutine instead of in batches inside delete calls
- The goroutine uses CPU, bounded by `IncrementalBudget` per `IncrementalInterval`
- `Close()` must be called to stop the goroutine

---

## API Stability

The library is at version 0.x, so the API can still change between minor versions; changes are
listed in [CHANGELOG.md](../../CHANGELOG.md). Two parts of the rebalancing API are incomplete:
- `WithSmartRebalancing` and the `Smart*` options are accepted and have no effect
- `IncrementalProgressCallback` takes a type declared in an internal package; see
  [Progress Monitoring](#progress-monitoring)

---

## See Also

- [Performance Tuning Guide](PERFORMANCE_TUNING.md): when to use each mode, and troubleshooting
- [FAQ](FAQ.md): common questions
- **[Examples](../../examples/)**: Working code examples

---

**Last Updated**: 2026-11-07
