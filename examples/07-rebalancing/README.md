# Rebalancing Examples

This directory contains working examples demonstrating HDF5 B-tree rebalancing strategies.

## Examples

### 01-default.go - Default (No Rebalancing)

**Use Case**: Append-only workloads, small files, maximum write performance

```bash
go run 01-default.go
```

**What it demonstrates**:
- Default behavior (no rebalancing, like HDF5 C library)
- Fastest deletion (0% overhead)
- B-tree may become sparse after many deletions

---

### 02-lazy.go - Lazy Rebalancing

**Use Case**: Batch deletion workloads, medium/large files (100-500MB)

```bash
go run 02-lazy.go
```

**What it demonstrates**:
- Lazy (batch) rebalancing mode
- 10-100x faster than immediate rebalancing
- Occasional pauses (100-500ms) during batch processing
- Tuning parameters: threshold, max delay, batch size

---

### 03-incremental.go - Incremental Rebalancing

**Use Case**: Large files (>500MB), continuous operations, zero-pause requirement

```bash
go run 03-incremental.go
```

**What it demonstrates**:
- Incremental (background) rebalancing mode
- ZERO user-visible pause (all rebalancing in background)
- Progress monitoring via callback
- Tuning parameters: budget, interval

---

### 04-smart.go - Smart Rebalancing (Auto-Tuning)

**Use Case**: Unknown workloads, mixed operations, want auto-pilot mode

```bash
go run 04-smart.go
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
| **01-default** | None | 0% | None | Append-only, small files |
| **02-lazy** | Lazy (batch) | ~2% | 100-500ms batches | Batch deletions |
| **03-incremental** | Incremental (background) | ~4% | None (background) | Large files, continuous ops |
| **04-smart** | Smart (auto) | ~6% | Varies | Unknown workloads |

---

## Running All Examples

```bash
# Run each example individually
go run 01-default.go
go run 02-lazy.go
go run 03-incremental.go
go run 04-smart.go

# Or run all at once
for f in *.go; do echo "Running $f..."; go run "$f"; echo; done
```

**Output Files**:
- `01-default-output.h5` - File with no rebalancing
- `02-lazy-output.h5` - File with lazy rebalancing
- `03-incremental-output.h5` - File with incremental rebalancing
- `04-smart-output.h5` - File with smart rebalancing

All files are valid HDF5 and can be opened with:
- `h5dump` (C library tool)
- Python `h5py`
- This library's `hdf5.Open()`

---

## Performance Tips

1. **Start with default** (01-default.go) unless you know you need rebalancing
2. **Use lazy** (02-lazy.go) for batch deletion workloads (10-100x faster)
3. **Use incremental** (03-incremental.go) for large files where pauses are unacceptable
4. **Use smart** (04-smart.go) only if workload is truly unknown

---

## Further Reading

- **[Performance Tuning Guide](../../docs/guides/performance-tuning.md)**: Comprehensive guide with benchmarks, recommendations, troubleshooting
- **[Rebalancing API Reference](../../docs/guides/rebalancing-api.md)**: Complete API documentation
- **[FAQ](../../docs/guides/FAQ.md)**: Common questions

---

**Version**: v0.11.3-beta
**Last Updated**: 2025-11-02
