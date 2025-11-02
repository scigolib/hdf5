# Rebalancing Examples

This directory contains working examples demonstrating HDF5 B-tree rebalancing strategies.

## Examples

### default/ - Default (No Rebalancing)

**Use Case**: Append-only workloads, small files, maximum write performance

```bash
go run ./default
```

**What it demonstrates**:
- Default behavior (no rebalancing, like HDF5 C library)
- Fastest deletion (0% overhead)
- B-tree may become sparse after many deletions

---

### lazy/ - Lazy Rebalancing

**Use Case**: Batch deletion workloads, medium/large files (100-500MB)

```bash
go run ./lazy
```

**What it demonstrates**:
- Lazy (batch) rebalancing mode
- 10-100x faster than immediate rebalancing
- Occasional pauses (100-500ms) during batch processing
- Tuning parameters: threshold, max delay, batch size

---

### incremental/ - Incremental Rebalancing

**Use Case**: Large files (>500MB), continuous operations, zero-pause requirement

```bash
go run ./incremental
```

**What it demonstrates**:
- Incremental (background) rebalancing mode
- ZERO user-visible pause (all rebalancing in background)
- Progress monitoring via callback
- Tuning parameters: budget, interval

---

### smart/ - Smart Rebalancing (Auto-Tuning)

**Use Case**: Unknown workloads, mixed operations, want auto-pilot mode

```bash
go run ./smart
```

**What it demonstrates**:
- Smart (auto-tuning) rebalancing mode
- Automatic workload pattern detection
- Automatic mode selection and switching
- Explainability (confidence scores, reasoning)

---

## Quick Comparison

| Example | Mode | Overhead | Pause Time | Use Case |
|---------|------|----------|------------|----------|
| **default/** | None | 0% | None | Append-only, small files |
| **lazy/** | Lazy (batch) | ~2% | 100-500ms batches | Batch deletions |
| **incremental/** | Incremental (background) | ~4% | None (background) | Large files, continuous ops |
| **smart/** | Smart (auto) | ~6% | Varies | Unknown workloads |

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
- `smart-output.h5` - File with smart rebalancing

All files are valid HDF5 and can be opened with:
- `h5dump` (C library tool)
- Python `h5py`
- This library's `hdf5.Open()`

---

## Performance Tips

1. **Start with default** (`./default`) unless you know you need rebalancing
2. **Use lazy** (`./lazy`) for batch deletion workloads (10-100x faster)
3. **Use incremental** (`./incremental`) for large files where pauses are unacceptable
4. **Use smart** (`./smart`) only if workload is truly unknown

---

## Further Reading

- **[Performance Tuning Guide](../../docs/guides/performance-tuning.md)**: Comprehensive guide with benchmarks, recommendations, troubleshooting
- **[Rebalancing API Reference](../../docs/guides/rebalancing-api.md)**: Complete API documentation
- **[FAQ](../../docs/guides/FAQ.md)**: Common questions

---

**Version**: v0.11.3-beta
**Last Updated**: 2025-11-02
