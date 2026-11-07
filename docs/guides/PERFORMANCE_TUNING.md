# Performance Tuning Guide

This guide explains the B-tree rebalancing strategies of the library and when to use each, particularly for deletion-heavy workloads on large files. It gives no timings: they depend on the machine and the file. Measure your workload with the benchmarks in `btree_rebalancing_bench_test.go`.

---

## Table of Contents

1. [Introduction](#introduction)
2. [Understanding B-tree Rebalancing](#understanding-b-tree-rebalancing)
3. [Rebalancing Modes](#rebalancing-modes)
4. [Workload-Specific Recommendations](#workload-specific-recommendations)
5. [Configuration Guide](#configuration-guide)
6. [Troubleshooting](#troubleshooting)
7. [Advanced Topics](#advanced-topics)

---

## Introduction

### What is B-tree Rebalancing?

HDF5 uses version 2 B-trees to store dense attribute collections (8 or more attributes) and other metadata. When attributes are deleted, records are removed from B-tree nodes, which can leave nodes underutilized (sparse).

Rebalancing reorganizes the nodes after deletions to:
- Keep all leaf nodes at the same depth
- Merge underfull nodes to reduce wasted space
- Keep searches short

### Why It Matters

1. Without rebalancing, B-trees become increasingly sparse: lookups slow down and disk space is wasted.
2. With immediate rebalancing (the default), every deletion restructures the tree, which costs time when many records are deleted in a row.
3. Lazy and incremental rebalancing defer that work to batches or to a background goroutine.

The library provides three rebalancing strategies in addition to the default.

### When to Use Each Mode

Quick decision tree:
```
Are you deleting attributes?
├─ No → keep the default
└─ Yes
   ├─ Few deletions → keep the default (immediate rebalancing)
   └─ Many deletions
      ├─ Batch deletions (delete many, then continue) → lazy rebalancing
      ├─ Continuous operation (pauses not acceptable) → incremental rebalancing
      └─ A one-off batch followed by RebalanceAttributeBTree → WithBTreeRebalancing(false)
```

---

## Understanding B-tree Rebalancing

### B-tree Basics

A **B-tree v2** is a self-balancing tree structure used by HDF5 for:
- Dense attribute storage (8+ attributes per object)
- Link name index in groups
- Other metadata collections

**Key Properties**:
- All leaf nodes at same depth (balanced)
- Each node ≥50% full (except root)
- Records sorted by hash for fast lookup

### What Happens During Deletion

**Without Rebalancing**:
```
Initial B-tree (3 nodes, well-balanced):
    [Node A: 80% full]
         /        \
[Node B: 70% full] [Node C: 75% full]

After 50% deletion (NO rebalancing):
    [Node A: 40% full]  ← underfull
         /        \
[Node B: 35% full] [Node C: 40% full]  ← both underfull

Problem: Sparse tree, wasted space, slower searches
```

**With Rebalancing**:
```
After 50% deletion + rebalancing:
    [Node A: 75% full]
         |
    (Merged B + C → single well-filled node)

Result: Compact tree, efficient searches
```

### The Performance Dilemma

Immediate rebalancing (the default, as in the C library for version 2 B-trees):
- Keeps the B-tree compact after every delete
- Each deletion can trigger node merging, parent updates and disk writes, which adds up in batch deletions

No rebalancing (`WithBTreeRebalancing(false)`):
- A delete only removes the record
- The B-tree becomes sparse over time, which wastes disk space and slows later lookups

Deferred strategies (lazy and incremental, below) sit between the two.

---

## Rebalancing Modes

Besides the default (immediate rebalancing), the library offers three modes. In the examples
errors are not always checked, to keep them short; check them in real code.

### 1. No Rebalancing (`WithBTreeRebalancing(false)`)

What it does: never rebalances automatically. B-trees can become sparse. Without this option,
every delete rebalances immediately (the C library does the same for version 2 B-trees).

When to use:
- Few or no deletions
- A batch of deletions followed by one manual rebalance (`DatasetWriter.RebalanceAttributeBTree` or `FileWriter.RebalanceAllBTrees`)

Trade-offs:
- No rebalancing cost on delete
- The B-tree becomes sparse if many deletions occur, which can waste disk space

Example:
```go
// No rebalancing (without the option, every delete rebalances immediately)
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false),
)
if err != nil {
    return err
}
defer fw.Close()

// Write and delete: no automatic rebalancing
ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
ds.WriteAttribute("attr1", int32(42))
ds.DeleteAttribute("attr1")
```

---

### 2. Lazy Rebalancing (Batch Processing)

What it does: accumulates deletions and rebalances in batches when a threshold is reached.

How it works:
1. Deletions are tracked (underflow nodes are counted)
2. When `underflow_nodes / total_nodes` reaches the threshold, a batch rebalancing runs
3. Several nodes are processed in a single operation
4. A batch also runs after `MaxDelay`, so rebalancing is not postponed indefinitely

When to use:
- Batch deletion workloads: many attributes are deleted, then work continues
- You can tolerate an occasional pause while a batch is rebalanced

Trade-offs:
- Batching amortizes the restructuring cost
- The pause while a batch runs grows with `LazyBatchSize`
- The B-tree stays reasonably compact between batches

Example:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),           // Trigger at 5% underflow
        hdf5.LazyMaxDelay(5*time.Minute),   // Force rebalance after 5 min
        hdf5.LazyBatchSize(100),            // Process 100 nodes per batch
    ),
)
if err != nil {
    return err
}
defer fw.Close()

// Delete many attributes - rebalancing happens in batches
ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
for i := 0; i < 1000; i++ {
    ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
}

// Delete 100 attributes - lazy rebalancing automatically batches
for i := 0; i < 100; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
    // Rebalancing happens automatically when threshold reached
}
```

**Tuning Parameters**:

| Parameter | Default | Range | Effect |
|-----------|---------|-------|--------|
| `LazyThreshold` | 0.05 (5%) | 0.01-0.20 | Lower = more frequent rebalancing, tighter tree |
| `LazyMaxDelay` | 5 minutes | 1s-1h | Forces rebalance even if threshold not met |
| `LazyBatchSize` | 100 nodes | 10-1000 | Larger = fewer rebalancing events, longer pauses |

Suggestions:
- Fewer, longer pauses: `LazyThreshold(0.10)`, `LazyBatchSize(200)`
- A tighter tree, with more frequent and shorter rebalancing: `LazyThreshold(0.02)`, `LazyBatchSize(50)`
- Write-heavy sessions: `LazyMaxDelay(10*time.Minute)` to avoid interrupting them

---

### 3. Incremental Rebalancing (Background Processing)

What it does: rebalances B-trees in the background, in a goroutine with a time budget per session.

How it works:
1. Lazy rebalancing is a prerequisite (it tracks the underflow nodes)
2. The background goroutine wakes up every `Interval` (default 5 seconds)
3. It rebalances for up to `Budget` (default 100ms), then pauses
4. This continues until all underflow nodes are processed
5. Deletes do not wait for it; rebalancing runs between operations

When to use:
- Large files where the pause of a lazy batch is noticeable
- High delete ratios
- Continuous operation, where pauses are not acceptable

Trade-offs:
- Deletes are not blocked by rebalancing; the tree is brought into shape over time
- The background goroutine costs CPU and synchronization; `Budget` and `Interval` set how much
- `Close` stops the goroutine

Example:
```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(), // required by incremental mode
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond), // up to 100ms per session
        hdf5.IncrementalInterval(5*time.Second),      // every 5 seconds
    ),
)
if err != nil {
    return err
}
defer fw.Close() // stops the background goroutine

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000})
for i := 0; i < 10000; i++ {
    ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
}

// Rebalancing happens in the background goroutine
for i := 0; i < 5000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
}

// Progress of the background work
progress, err := fw.GetIncrementalRebalancingProgress()
if err == nil {
    fmt.Printf("rebalanced %d nodes, %d remaining\n", progress.NodesRebalanced, progress.NodesRemaining)
}
```

`IncrementalProgressCallback` takes a function whose parameter type is declared in an internal
package of this module, so code outside the module cannot write such a function. Poll
`FileWriter.GetIncrementalRebalancingProgress` as above instead.

Tuning parameters:

| Parameter | Default | Effect |
|-----------|---------|--------|
| `IncrementalBudget` | 100ms | Time spent rebalancing per session |
| `IncrementalInterval` | 5 seconds | How often a session runs |

A larger budget or a shorter interval rebalances faster and uses more CPU; a smaller budget or a
longer interval does the opposite. Pick values from your own measurements.

---

### 4. Smart Rebalancing (placeholder)

`WithSmartRebalancing` and the `Smart*` options exist in the API, but smart rebalancing is not
implemented yet: the option is accepted and has no effect, so a file opened with it rebalances
exactly as one opened without options. Choose one of the three modes above explicitly.

---

## Workload-Specific Recommendations

### Few Deletions or Append-Only Workloads

Characteristics: mostly inserts, few or no deletions (logging, sensor data, time series).

Recommendation: the default (immediate rebalancing).

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
// No options: every delete rebalances, and there are few deletes.
```

Why: without deletions the B-tree never becomes sparse, so there is nothing to defer.

---

### Batch Deletion Workloads

Characteristics: many attributes are written, many are deleted in a batch, and work continues; brief pauses are acceptable (data cleaning, batch pipelines).

Recommendation: lazy rebalancing.

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),         // 5% underflow
        hdf5.LazyMaxDelay(5*time.Minute), // force after 5 minutes
        hdf5.LazyBatchSize(100),          // 100 nodes per batch
    ),
)
```

Why: batching amortizes the restructuring cost, and the tree stays reasonably compact.

Tuning: a larger `LazyBatchSize` and `LazyThreshold` give fewer, longer pauses; smaller values keep the tree tighter.

---

### Large Files with Continuous Deletes

Characteristics: a large file, deletes mixed with other operations, and no natural pause points for a lazy batch.

Recommendation: incremental rebalancing.

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(), // required by incremental mode
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond),
        hdf5.IncrementalInterval(5*time.Second),
    ),
)
defer fw.Close() // stops the background goroutine
```

Why: deletes do not wait for rebalancing, which runs in the background.

Tuning: a smaller `IncrementalBudget` with a longer `IncrementalInterval` uses less CPU; the reverse rebalances faster. Check `FileWriter.GetIncrementalRebalancingProgress` to see whether the backlog (`NodesRemaining`) keeps growing; if it does, raise the budget or shorten the interval.

---

### Mixed or Unknown Workloads

Smart rebalancing, which would choose a mode from the workload, is not implemented (see mode 4 above). Start with the default; if a measurement on your workload shows deletes are slow, move to lazy rebalancing, or to incremental rebalancing if pauses matter.

---

## Configuration Guide

### Lazy Rebalancing Parameters

#### LazyThreshold

**What it controls**: When to trigger batch rebalancing

**Formula**: `(underflow_nodes / total_nodes) ≥ threshold`

**Range**: 0.01 (1%) to 0.20 (20%)

**Default**: 0.05 (5%)

**Examples**:

```go
// Conservative (tight tree, more frequent rebalancing)
hdf5.LazyThreshold(0.02)  // Trigger at 2% underflow

// Default (balanced)
hdf5.LazyThreshold(0.05)  // Trigger at 5% underflow

// Aggressive (loose tree, less frequent rebalancing)
hdf5.LazyThreshold(0.10)  // Trigger at 10% underflow
```

**When to adjust**:
- **Decrease (0.02)** if: Disk space is limited, search performance critical
- **Increase (0.10)** if: Write performance is critical, disk space abundant

---

#### LazyMaxDelay

**What it controls**: Maximum time before forcing rebalancing

**Purpose**: Prevents indefinite delay in write-heavy workloads

**Range**: 1 second to 1 hour

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

**When to adjust**:
- **Decrease (1 min)** if: Want predictable rebalancing, file size growth is concern
- **Increase (30 min)** if: Long write sessions, can't afford interruptions

---

#### LazyBatchSize

**What it controls**: How many nodes to rebalance per batch

**Trade-off**: Larger batches = fewer events but longer pauses

**Range**: 10 to 1000 nodes

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

**When to adjust**:
- **Decrease (50)** if: Very latency-sensitive, can tolerate more frequent pauses
- **Increase (200)** if: Batch jobs, want fewer interruptions

---

### Incremental Rebalancing Parameters

#### IncrementalBudget

**What it controls**: Time spent rebalancing per background session

**Trade-off**: Larger budget = more CPU per session, faster rebalancing

**Range**: 10ms to 1 second

**Default**: 100ms

**Examples**:

```go
// Low CPU impact (minimal overhead, slower rebalancing)
hdf5.IncrementalBudget(50*time.Millisecond)

// Default (balanced)
hdf5.IncrementalBudget(100*time.Millisecond)

// High throughput (faster rebalancing, higher overhead)
hdf5.IncrementalBudget(200*time.Millisecond)
```

The share of time spent rebalancing is about `Budget / Interval` at most (for the defaults, 100ms every 5 seconds).

**When to adjust**:
- **Decrease (50ms)** if: CPU constrained, rebalancing is low priority
- **Increase (200ms)** if: Need aggressive rebalancing, CPU available

---

#### IncrementalInterval

**What it controls**: How often to run rebalancing sessions

**Trade-off**: Shorter interval = more frequent rebalancing, higher overhead

**Range**: 1 second to 1 minute

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

**When to adjust**:
- **Decrease (2s)** if: High delete rate, need to prevent backlog
- **Increase (10s)** if: Low delete rate, minimize overhead

---

#### IncrementalProgressCallback

What it controls: a callback for monitoring rebalancing progress after each session.

The callback's parameter type, `RebalancingProgress`, is declared in an internal package of this
module, so code outside the module cannot write the function. Poll
`FileWriter.GetIncrementalRebalancingProgress` instead; it returns the same fields
(`NodesRebalanced`, `NodesRemaining`, `SessionDuration`, `EstimatedRemaining`, `IsComplete`):

```go
progress, err := fw.GetIncrementalRebalancingProgress()
if err != nil {
    log.Printf("progress: %v", err)
} else if progress.NodesRemaining > 1000 {
    log.Printf("rebalancing backlog: %d nodes", progress.NodesRemaining)
}
```

---

## Troubleshooting

### Slow Deletion Performance

Symptom: a batch of deletions takes much longer than expected.

Likely cause: the default, immediate rebalancing, restructures the tree on every delete.

Solution:
1. Establish a baseline without rebalancing:
   ```go
   fw, _ := hdf5.CreateForWrite("test.h5", hdf5.CreateTruncate,
       hdf5.WithBTreeRebalancing(false))
   ```
2. If the baseline is much faster, switch to lazy or incremental rebalancing.
3. If lazy rebalancing is already on, check that the threshold is not too low (the default is 0.05).

---

### Excessive Disk Space Usage

Symptom: the file is much larger than expected after deletions.

Likely cause: rebalancing is off (`WithBTreeRebalancing(false)`) and the B-tree has become sparse.

Solution: call `RebalanceAttributeBTree` or `RebalanceAllBTrees` after the deletions, or enable lazy rebalancing:

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),
    ),
)
```

Check the file size after the deletions:
```go
info, err := os.Stat("data.h5")
if err != nil {
    log.Fatal(err)
}
log.Printf("File size: %d MB", info.Size()/1024/1024)
```

---

### Rebalancing Pauses Too Long

Symptom: the pause while lazy rebalancing runs a batch is too long.

Likely cause: the batch size is too large or the threshold too high.

Solution 1: decrease the batch size:
```go
hdf5.WithLazyRebalancing(
    hdf5.LazyBatchSize(50), // smaller batches, shorter pauses
)
```

Solution 2: switch to incremental rebalancing, which runs in the background:
```go
hdf5.WithLazyRebalancing(),
hdf5.WithIncrementalRebalancing(
    hdf5.IncrementalBudget(100*time.Millisecond),
    hdf5.IncrementalInterval(5*time.Second),
)
```

---

### High CPU Usage

Symptom: background rebalancing uses too much CPU.

Likely cause: the incremental budget is too large or the interval too short.

Solution: decrease the CPU impact:

```go
hdf5.WithIncrementalRebalancing(
    hdf5.IncrementalBudget(50*time.Millisecond),  // Lower budget
    hdf5.IncrementalInterval(10*time.Second),     // Less frequent
)
```

To see where the CPU goes, profile the program with `runtime/pprof` or `go tool pprof`.

---

### Rebalancing Backlog Building Up

Symptom: `NodesRemaining` from `FileWriter.GetIncrementalRebalancingProgress` keeps increasing.

Likely cause: the delete rate exceeds what the background sessions rebalance.

Solution: give rebalancing more time:

```go
hdf5.WithIncrementalRebalancing(
    hdf5.IncrementalBudget(200*time.Millisecond), // more time per session
    hdf5.IncrementalInterval(2*time.Second),      // more frequent sessions
)
```

Alternative: switch to lazy rebalancing, which processes the backlog in batches:
```go
hdf5.WithLazyRebalancing(
    hdf5.LazyThreshold(0.05),
)
```

---

## Advanced Topics

### Custom Thresholds for Scientific Workloads

Scenario: simulation data with a high delete ratio, processed in batches.

Configuration:

```go
// Workload characteristics:
// - large files
// - a high delete ratio (trimming failed runs)
// - batch processing (pauses are acceptable)
// - a tight tree is wanted (disk space is expensive)

fw, err := hdf5.CreateForWrite("simulation.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.03),          // Tight threshold (3%)
        hdf5.LazyMaxDelay(2*time.Minute),  // Frequent forced rebalancing
        hdf5.LazyBatchSize(200),           // Large batches (acceptable pauses)
    ),
)
```

Why these values:
- A low threshold (3%) keeps the B-tree compact
- A short `LazyMaxDelay` (2 minutes) forces regular rebalancing
- A large `LazyBatchSize` (200) amortizes the cost of rebalancing large trees

---

### Comparison with HDF5 C Library

**C Library Behavior**:
- **Version 2 B-trees** (dense attributes and links): nodes are merged and redistributed on every delete (`H5B2__remove_internal`, `H5B2internal.c`). This is this library's default too.
- **Version 1 B-trees** (old-style groups and chunk indexes): never rebalanced (`H5B.c`)

**What this library adds**:
- Lazy mode: rebalancing in batches
- Incremental mode: rebalancing in a background goroutine

**Compatibility**: the file format is unchanged in every mode, so the files are readable by the C library.

---

## Summary

### Quick Reference

| Workload | Mode | Key parameters |
|----------|------|----------------|
| Few deletes or append-only | Default (immediate) | none |
| Batch deletes | Lazy | `LazyThreshold(0.05)`, `LazyBatchSize(100)` |
| Large files, continuous deletes | Incremental | `IncrementalBudget(100ms)`, `IncrementalInterval(5s)` |
| One batch, then a manual rebalance | `WithBTreeRebalancing(false)` | `RebalanceAttributeBTree` afterward |

### Best Practices

1. Start with the default and change it only when a measurement shows deletes are slow
2. Use lazy rebalancing for batch deletion workloads
3. Use incremental rebalancing where pauses are not acceptable
4. Watch `GetIncrementalRebalancingProgress` for a growing backlog
5. Benchmark on your own data before deploying

### Tips

1. Lazy rebalancing: a larger `LazyBatchSize` gives fewer, longer pauses
2. Incremental rebalancing: adjust `IncrementalBudget` and `IncrementalInterval` to trade CPU for speed

### Reporting a Problem

Open an issue at https://github.com/scigolib/hdf5/issues if:
- the rebalancing backlog keeps growing
- the file size does not decrease after rebalancing

---

**Last Updated**: 2026-11-07
**Related Guides**: [Rebalancing API Reference](REBALANCING_API.md), [FAQ](FAQ.md)
