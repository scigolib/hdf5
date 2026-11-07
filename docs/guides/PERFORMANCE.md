# Performance Guide - HDF5 Go Library

How B-tree rebalancing affects attribute deletion, and how to batch deletions. No timings are given
here: they depend on the machine and the file, so measure your own workload with the benchmarks in
`btree_rebalancing_bench_test.go` (see [Running Benchmarks](#running-benchmarks)).

In the examples errors are not checked, to keep them short; check them in real code.

**Last Updated**: 2026-11-07

---

## Table of Contents

1. [B-tree Rebalancing](#b-tree-rebalancing)
2. [When to Disable Rebalancing](#when-to-disable-rebalancing)
3. [When to Keep Rebalancing Enabled](#when-to-keep-rebalancing-enabled)
4. [Batch Operations](#batch-operations)
5. [Large Files](#large-files)
6. [Running Benchmarks](#running-benchmarks)
7. [Best Practices Summary](#best-practices-summary)

---

## B-tree Rebalancing

### What is B-tree Rebalancing?

When you delete attributes from dense storage (8+ attributes), the B-tree index becomes sparse. Rebalancing:
- Merges sparse nodes to maintain at least 50% occupancy
- Redistributes records for a balanced tree structure

Default behavior: rebalancing is enabled. Every delete rebalances, as the HDF5 C library does for version 2 B-trees (`H5B2__remove_internal`, `H5B2internal.c`; only its version 1 B-tree never rebalances, `H5B.c`).

### Performance Impact

A delete without rebalancing skips the merge and redistribute work, so it costs less; how much less
depends on the tree. `BenchmarkBTreeDeletion_WithRebalancing` and
`BenchmarkBTreeDeletion_WithoutRebalancing` measure both on your machine.

Trade-off: without rebalancing the B-tree becomes sparse, which wastes space and slows lookups.

Batch mode: disable rebalancing, delete everything, rebalance once.

---

## When to Disable Rebalancing

### Use Case: Batch Deletions

**Disable** rebalancing (`WithBTreeRebalancing(false)`) when:
- Deleting many attributes (more than about 100)
- Delete speed matters more than the tree shape in the meantime
- You will rebalance manually afterward

### Example: Batch Deletions

```go
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false), // no rebalancing on delete
)
defer fw.Close()

ds, _ := fw.CreateDataset("/temperature", hdf5.Float64, []uint64{1000})

// Add 10,000 attributes
for i := 0; i < 10000; i++ {
    ds.WriteAttribute(fmt.Sprintf("old_%d", i), int32(i))
}

// Delete them without rebalancing after each delete
for i := 0; i < 10000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("old_%d", i))
}

// Rebalance once at the end
ds.RebalanceAttributeBTree()
```

### Example: Runtime Toggle

You can also enable/disable rebalancing dynamically:

```go
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
defer fw.Close()

// Initially enabled (default)
ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})

// Add some attributes
for i := 0; i < 20; i++ {
    ds.WriteAttribute(fmt.Sprintf("keep_%d", i), int32(i))
}

// Disable for batch deletions
fw.DisableRebalancing()

// Delete the old attributes (they were written earlier in the file)
for i := 0; i < 1000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("old_%d", i))
}

// Re-enable for normal operations
fw.EnableRebalancing()

// Rebalance the tree once
ds.RebalanceAttributeBTree()

// Later deletes rebalance again
ds.WriteAttribute("new_attr", int32(42))
```

---

## When to Keep Rebalancing Enabled

### Use Case: Interactive Operations

Keep it enabled (the default) when:
- Few attributes are deleted
- The process is long-running and the tree should stay compact
- The deletion pattern is not known
- The file will be read often

### Example: Normal Usage

```go
// Default: rebalancing enabled
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})

// Add and delete attributes; each delete rebalances
ds.WriteAttribute("experiment_1", "2025-01-15")
ds.WriteAttribute("experiment_2", "2025-01-16")
ds.DeleteAttribute("experiment_1")
```

Benefits:
- The tree stays at least 50% occupied
- No manual maintenance

---

## Batch Operations

### Recommended Pattern

For a batch of more than about 100 deletions:

```go
// 1. Disable rebalancing
fw.DisableRebalancing()

// 2. Perform the batch deletions
for i := 0; i < N; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
}

// 3. Rebalance once
ds.RebalanceAttributeBTree()

// 4. Re-enable (optional, for subsequent operations)
fw.EnableRebalancing()
```

### Per-Dataset vs Global Rebalancing

**Per-Dataset** (targeted):

```go
ds.RebalanceAttributeBTree()  // Rebalance this dataset only
```

**Global** (all datasets):

```go
fw.RebalanceAllBTrees()  // Rebalance all datasets in file
```

Use global rebalancing when:
- Several datasets were modified
- At the end of a session, before closing the file

---

## Large Files

For files with very many attributes, rebalancing a whole tree can take noticeable time, and the work is dominated by disk I/O: reading and writing B-tree nodes. How long it takes depends on the tree depth, the node size and the storage; measure it on your data.

### Example: Large Batch

```go
fw, _ := hdf5.CreateForWrite("big_data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false), // no rebalancing during the batch
)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000000})

// Delete 5 million attributes that were written earlier
for i := 0; i < 5000000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))

    // Progress tracking
    if i%100000 == 0 {
        fmt.Printf("Deleted %d/%d attributes\n", i, 5000000)
    }
}

fmt.Println("Rebalancing B-tree...")
ds.RebalanceAttributeBTree()
```

### Recommendations for Large Files

1. Disable rebalancing for batch operations
2. Log progress periodically
3. Run the final rebalance when the process can afford the pause
4. Delete in batches and rebalance after each, so an interrupted run leaves a tree in better shape

### Incremental Rebalancing Pattern

For very large files, rebalance in batches:

```go
fw.DisableRebalancing()

batchSize := 10000
totalDeletes := 1000000

for i := 0; i < totalDeletes; i += batchSize {
    // Delete batch
    for j := 0; j < batchSize && (i+j) < totalDeletes; j++ {
        ds.DeleteAttribute(fmt.Sprintf("attr_%d", i+j))
    }

    // Rebalance after each batch
    ds.RebalanceAttributeBTree()

    fmt.Printf("Progress: %d/%d\n", i+batchSize, totalDeletes)
}

fw.EnableRebalancing()
```

Benefits:
- Predictable pauses
- Progress tracking
- An interrupted run can be resumed

---

## Running Benchmarks

```bash
# Run all B-tree benchmarks
go test -run XXX -bench=BenchmarkBTree -benchmem
```

The benchmarks in `btree_rebalancing_bench_test.go`:
- `BenchmarkBTreeRebalancing_SmallDataset`, `_MediumDataset`, `_LargeDataset`: manual rebalancing
- `BenchmarkBTreeDeletion_WithRebalancing` and `BenchmarkBTreeDeletion_WithoutRebalancing`: one delete in each mode
- `BenchmarkBTreeDeletion_BatchWithManualRebalance` and `BenchmarkBTreeDeletion_Comparison`: batch deletion with one rebalance at the end
- `BenchmarkFileWriter_RebalanceAllBTrees`: global rebalancing

---

## Best Practices Summary

Do:

1. Disable rebalancing for batch deletions (more than about 100 attributes)
2. Rebalance manually after a batch (`ds.RebalanceAttributeBTree()`)
3. Use global rebalancing before closing the file (`fw.RebalanceAllBTrees()`)
4. Keep rebalancing enabled when only a few attributes are deleted (the default)
5. Log progress for long batches

Do not:

1. Leave rebalancing disabled without a manual rebalance afterward
2. Ignore sparse trees: they slow down lookups
3. Rebalance too often: each rebalance has a cost

Decision rule:

```
Deleting more than about 100 attributes?
  yes: disable rebalancing, delete, rebalance once, re-enable if needed
  no:  keep rebalancing enabled (default); the tree stays compact
```

---

## Examples

### Example 1: Small Batch (<100 deletions)

```go
// Keep rebalancing enabled (default)
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})

// Delete 50 attributes written earlier (the tree stays balanced automatically)
for i := 0; i < 50; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
}
```

### Example 2: Medium Batch (100-10000 deletions)

```go
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false), // disable
)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})

// Delete 1000 attributes written earlier
for i := 0; i < 1000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))
}

// Rebalance once
ds.RebalanceAttributeBTree()
```

### Example 3: Large Batch (>10000 deletions)

```go
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false),
)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000000})

// Delete 100,000 attributes with progress tracking
for i := 0; i < 100000; i++ {
    ds.DeleteAttribute(fmt.Sprintf("attr_%d", i))

    if i%10000 == 0 {
        fmt.Printf("Deleted %d/100000 attributes\n", i)
    }
}

// Rebalance once
fmt.Println("Rebalancing...")
ds.RebalanceAttributeBTree()
fmt.Println("Done")
```

### Example 4: Multiple Datasets

```go
fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false),
)
defer fw.Close()

// Delete attributes from multiple datasets
for i := 0; i < 10; i++ {
    ds, _ := fw.CreateDataset(fmt.Sprintf("/dataset_%d", i), hdf5.Float64, []uint64{100})

    // Delete many attributes written earlier to each dataset
    for j := 0; j < 500; j++ {
        ds.DeleteAttribute(fmt.Sprintf("attr_%d", j))
    }
}

// Rebalance all datasets at once
fw.RebalanceAllBTrees()
```

---

## Summary

- A few deletions: keep rebalancing enabled (the default)
- Batch deletions: disable rebalancing, delete, rebalance once
- Large files: batch mode, with progress logging

See also:
- [Performance Tuning](PERFORMANCE_TUNING.md): the lazy, incremental and smart modes
- [Rebalancing API](REBALANCING_API.md)
- [Benchmarks](../../btree_rebalancing_bench_test.go)

---

*Last Updated: 2026-11-07*
