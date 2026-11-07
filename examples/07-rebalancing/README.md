# Rebalancing Examples

Runnable programs for the B-tree rebalancing options of dense attribute storage.

## Examples

### default/ - No rebalancing

Use it for append-only workloads and small files.

```bash
go run ./default
```

What it shows:
- `WithBTreeRebalancing(false)`: deletes do not rebalance. Without any option, every delete
  rebalances, as the HDF5 C library does for version 2 B-trees
- The B-tree may become sparse after many deletions

---

### lazy/ - Lazy rebalancing

Use it for batch deletions.

```bash
go run ./lazy
```

What it shows:
- Lazy (batch) rebalancing: nodes are rebalanced once a threshold is reached
- The tuning parameters: threshold, maximum delay, batch size

---

### incremental/ - Incremental rebalancing

Use it for large files and long-running processes that delete continuously.

```bash
go run ./incremental
```

What it shows:
- Incremental rebalancing in a background goroutine, within a time budget
- Progress reporting
- The tuning parameters: budget, interval

---

### smart/ - Smart rebalancing (not implemented)

```bash
go run ./smart
```

`WithSmartRebalancing` is a planned mode and has no effect yet: the program runs with the default
rebalancing and only shows the planned API.

---

## Comparison

| Example | Mode | Pauses | Use case |
|---------|------|--------|----------|
| default/ | none | none | append-only, small files |
| lazy/ | lazy (batch) | short, per batch | batch deletions |
| incremental/ | incremental (background) | none | large files, continuous deletes |
| smart/ | not implemented (default rebalancing) | — | — |

Measure your workload with the benchmarks in `btree_rebalancing_bench_test.go`.

---

## Running All Examples

```bash
# Run each example individually
go run ./default
go run ./lazy
go run ./incremental
go run ./smart

# Or build all examples
go build ./...

# Or run all examples at once
for dir in default lazy incremental smart; do
  echo "Running $dir..."
  go run ./$dir
  echo
done
```

**Output Files**:
- `default-output.h5` - File with no rebalancing
- `lazy-output.h5` - File with lazy rebalancing
- `incremental-output.h5` - File with incremental rebalancing
- `smart-output.h5` - File written with the default rebalancing

All files are valid HDF5 and can be opened with `h5dump`, h5py, or this library's `hdf5.Open()`.

---

## Choosing a mode

1. Keep the default unless deletes are frequent enough to measure
2. Use lazy (`./lazy`) for batch deletions
3. Use incremental (`./incremental`) for large files where pauses matter

---

## Further Reading

- [Performance tuning guide](../../docs/guides/PERFORMANCE_TUNING.md)
- [Rebalancing API reference](../../docs/guides/REBALANCING_API.md)
- [FAQ](../../docs/guides/FAQ.md)

---

**Last Updated**: 2026-11-07
