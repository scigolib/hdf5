# HDF5 Go Library

> **Pure Go implementation of the HDF5 file format** - No CGo required

[![Release](https://img.shields.io/github/v/release/scigolib/hdf5?include_prereleases&style=flat-square&logo=github&color=blue&label=version)](https://github.com/scigolib/hdf5/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/scigolib/hdf5?style=flat-square&logo=go)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/scigolib/hdf5?style=flat-square)](https://goreportcard.com/report/github.com/scigolib/hdf5)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue?style=flat-square&logo=go)](https://pkg.go.dev/github.com/scigolib/hdf5)
[![CI](https://img.shields.io/github/actions/workflow/status/scigolib/hdf5/test.yml?branch=main&style=flat-square&logo=github&label=tests)](https://github.com/scigolib/hdf5/actions)
[![codecov](https://codecov.io/gh/scigolib/hdf5/graph/badge.svg)](https://codecov.io/gh/scigolib/hdf5)
[![License](https://img.shields.io/github/license/scigolib/hdf5?style=flat-square&color=blue)](https://github.com/scigolib/hdf5/blob/main/LICENSE)
[![Stars](https://img.shields.io/github/stars/scigolib/hdf5?style=flat-square&logo=github)](https://github.com/scigolib/hdf5/stargazers)
[![Discussions](https://img.shields.io/github/discussions/scigolib/hdf5?style=flat-square&logo=github&label=discussions)](https://github.com/scigolib/hdf5/discussions)

A modern, pure Go library for reading and writing HDF5 files without CGo dependencies. **v0.13.5: Critical checksum fix - Jenkins lookup3 algorithm for h5dump/h5py compatibility!**

---

## ✨ Features

- ✅ **Pure Go** - No CGo, no C dependencies, cross-platform
- ✅ **Modern Design** - Built with Go 1.25+ best practices
- ✅ **HDF5 2.0.0 Compatibility** - Read/Write: v0, v2, v3 superblocks | Format Spec v4.0 with checksum validation
- ✅ **Full Dataset Reading** - Compact, contiguous, chunked layouts with GZIP
- ✅ **Rich Datatypes** - Integers, floats, strings (fixed/variable), compounds
- ✅ **Memory Efficient** - Buffer pooling and smart memory management
- ✅ **Production Ready** - Read support feature-complete
- ✍️ **Comprehensive Write Support** - Datasets, groups, attributes + Smart Rebalancing!

---

## 🚀 Quick Start

### Installation

```bash
go get github.com/scigolib/hdf5
```

### Basic Usage

```go
package main

import (
    "fmt"
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // Open HDF5 file
    file, err := hdf5.Open("data.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Walk through file structure
    file.Walk(func(path string, obj hdf5.Object) {
        switch v := obj.(type) {
        case *hdf5.Group:
            fmt.Printf("📁 %s (%d children)\n", path, len(v.Children()))
        case *hdf5.Dataset:
            fmt.Printf("📊 %s\n", path)
        }
    })
}
```

**Output**:
```
📁 / (2 children)
📊 /temperature
📁 /experiments/ (3 children)
```

[More examples →](examples/)

---

## 📚 Documentation

### Getting Started
- **[Installation Guide](docs/guides/INSTALLATION.md)** - Install and verify the library
- **[Quick Start Guide](docs/guides/QUICKSTART.md)** - Get started in 5 minutes
- **[Reading Data](docs/guides/READING_DATA.md)** - Comprehensive guide to reading datasets and attributes

### Reference
- **[Datatypes Guide](docs/guides/DATATYPES.md)** - HDF5 to Go type mapping
- **[Troubleshooting](docs/guides/TROUBLESHOOTING.md)** - Common issues and solutions
- **[FAQ](docs/guides/FAQ.md)** - Frequently asked questions
- **[API Reference](https://pkg.go.dev/github.com/scigolib/hdf5)** - GoDoc documentation

### Advanced
- **[Architecture Overview](docs/architecture/OVERVIEW.md)** - How it works internally
- **[Performance Tuning](docs/guides/PERFORMANCE_TUNING.md)** - B-tree rebalancing strategies for optimal performance
- **[Rebalancing API](docs/guides/REBALANCING_API.md)** - Complete API reference for rebalancing options
- **[Examples](examples/)** - Working code examples (7 examples with detailed documentation)

---

## ⚡ Performance Tuning

When deleting many attributes, B-trees can become **sparse** (wasted disk space, slower searches). This library offers **4 rebalancing strategies**:

### 1. Default (No Rebalancing)

**Fast deletions, but B-tree may become sparse**

```go
// No options = no rebalancing (like HDF5 C library)
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
```

**Use for**: Append-only workloads, small files (<100MB)

---

### 2. Lazy Rebalancing (10-100x faster than immediate)

**Batch processing: rebalances when threshold reached**

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),         // Trigger at 5% underflow
        hdf5.LazyMaxDelay(5*time.Minute), // Force rebalance after 5 min
    ),
)
```

**Use for**: Batch deletion workloads, medium/large files (100-500MB)

**Performance**: ~2% overhead, occasional 100-500ms pauses

---

### 3. Incremental Rebalancing (ZERO pause)

**Background processing: rebalances in background goroutine**

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(),  // Prerequisite!
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond),
        hdf5.IncrementalInterval(5*time.Second),
    ),
)
defer fw.Close()  // Stops background goroutine
```

**Use for**: Large files (>500MB), continuous operations, TB-scale data

**Performance**: ~4% overhead, **zero user-visible pause**

---

### 4. Smart Rebalancing (Auto-Pilot)

**Auto-tuning: library detects workload and selects optimal mode**

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithSmartRebalancing(
        hdf5.SmartAutoDetect(true),
        hdf5.SmartAutoSwitch(true),
    ),
)
```

**Use for**: Unknown workloads, mixed operations, research environments

**Performance**: ~6% overhead, adapts automatically

---

### Performance Comparison

| Mode | Deletion Speed | Pause Time | Use Case |
|------|----------------|------------|----------|
| **Default** | 100% (baseline) | None | Append-only, small files |
| **Lazy** | 95% (10-100x faster than immediate!) | 100-500ms batches | Batch deletions |
| **Incremental** | 92% | None (background) | Large files, continuous ops |
| **Smart** | 88% | Varies | Unknown workloads |

**Learn more**:
- **[Performance Tuning Guide](docs/guides/PERFORMANCE_TUNING.md)**: Comprehensive guide with benchmarks, recommendations, troubleshooting
- **[Rebalancing API Reference](docs/guides/REBALANCING_API.md)**: Complete API documentation
- **[Examples](examples/07-rebalancing/)**: 4 working examples demonstrating each mode

---

## 🎯 Current Status

**Version**: v0.13.5 (RELEASED 2026-02-02 - Jenkins Checksum Fix) ✅

**Critical Fix: Jenkins lookup3 checksums for h5dump/h5py compatibility. HDF5 2.0.0 Ready with 86.1% coverage!** 🎉

### ✅ Fully Implemented
- **File Structure**:
  - Superblock parsing (v0, v2, v3) with checksum validation (CRC32)
  - Object headers v1 (legacy HDF5 < 1.8) with continuations
  - Object headers v2 (modern HDF5 >= 1.8) with continuations
  - Groups (traditional symbol tables + modern object headers)
  - B-trees (leaf + non-leaf nodes for large files)
  - Local heaps (string storage)
  - Global Heap (variable-length data)
  - Fractal heap (direct blocks for dense attributes) ✨ NEW

- **Dataset Reading**:
  - Compact layout (data in object header)
  - Contiguous layout (sequential storage)
  - Chunked layout with B-tree indexing
  - GZIP/Deflate compression
  - LZF compression (h5py/PyTables compatible) ✨ NEW
  - Filter pipeline for compressed data

- **Datatypes** (Read + Write):
  - **Basic types**: int8-64, uint8-64, float32/64
  - **AI/ML types**: FP8 (E4M3, E5M2), bfloat16 - IEEE 754 compliant ✨ NEW
  - **Strings**: Fixed-length (null/space/null-padded), variable-length (via Global Heap)
  - **Advanced types**: Arrays, Enums, References (object/region), Opaque
  - **Compound types**: Struct-like with nested members

- **Attributes**:
  - Compact attributes (in object header) ✨ NEW
  - Dense attributes (fractal heap foundation) ✨ NEW
  - Attribute reading for groups and datasets ✨ NEW
  - Full attribute API (Group.Attributes(), Dataset.Attributes()) ✨ NEW

- **Navigation**: Full file tree traversal via Walk()

- **Code Quality**:
  - Test coverage: 86.1% overall (target: >70%) ✅
  - Lint issues: 0 (34+ linters) ✅
  - TODO items: 0 (all resolved) ✅
  - Official HDF5 test suite: 433 files, 100% pass rate ✅

- **Security** ✨ NEW:
  - 4 CVEs fixed (CVE-2025-7067, CVE-2025-6269, CVE-2025-2926, CVE-2025-44905) ✅
  - Overflow protection throughout (SafeMultiply, buffer validation) ✅
  - Security limits: 1GB chunks, 64MB attributes, 16MB strings ✅
  - 39 security test cases, all passing ✅

### ✍️ Write Support - Feature Complete!
**Production-ready write support with all features!** ✅

**Dataset Operations**:
- ✅ Create datasets (all layouts: contiguous, chunked, compact)
- ✅ Write data (all datatypes including compound)
- ✅ Dataset resizing with unlimited dimensions
- ✅ Variable-length datatypes: strings, ragged arrays
- ✅ Compression (GZIP, Shuffle, Fletcher32)
- ✅ Array and enum datatypes
- ✅ References and opaque types
- ✅ Attribute writing (dense & compact storage)
- ✅ Attribute modification/deletion

**Links**:
- ✅ Hard links (full support)
- ✅ Soft links (symbolic references - full support)
- ✅ External links (cross-file references - full support)

**Read Enhancements**:
- ✅ Hyperslab selection (data slicing) - 10-250x faster!
- ✅ Efficient partial dataset reading
- ✅ Stride and block support
- ✅ Chunk-aware reading (reads ONLY needed chunks)
- ✅ **ChunkIterator API** - Memory-efficient iteration over large datasets

**Validation**:
- ✅ Official HDF5 Test Suite: 100% pass rate (378/378 files)
- ✅ Production quality confirmed

**Future Enhancements**:
- ✅ LZF filter (read + write, Pure Go) ✨ NEW
- ✅ BZIP2 filter (read only, stdlib)
- ⚠️ SZIP filter (stub - requires libaec)
- ⚠️ Thread-safety with mutexes + SWMR mode
- ⚠️ Parallel I/O

### ❌ Planned Features

**Next Steps** - See [ROADMAP.md](ROADMAP.md) for complete timeline and versioning strategy.

---

## 🔧 Development

### Requirements
- Go 1.25 or later
- No external dependencies for the library

### Building

```bash
# Clone repository
git clone https://github.com/scigolib/hdf5.git
cd hdf5

# Run tests
go test ./...

# Build examples
go build ./examples/...

# Build tools
go build ./cmd/...
```

### Testing

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## 🤝 Contributing

Contributions are welcome! This is an early-stage project and we'd love your help.

**Before contributing**:
1. Read [CONTRIBUTING.md](CONTRIBUTING.md) - Git workflow and development guidelines
2. Check [open issues](https://github.com/scigolib/hdf5/issues)
3. Review the [Architecture Overview](docs/architecture/OVERVIEW.md)

**Ways to contribute**:
- 🐛 Report bugs
- 💡 Suggest features
- 📝 Improve documentation
- 🔧 Submit pull requests
- ⭐ Star the project

---

## 🗺️ Comparison with Other Libraries

| Feature | This Library | gonum/hdf5 | go-hdf5/hdf5 |
|---------|-------------|------------|--------------|
| Pure Go | ✅ Yes | ❌ CGo wrapper | ✅ Yes |
| Reading | ✅ Full | ✅ Full | ❌ Limited |
| Writing | ✅ Full | ✅ Full | ❌ No |
| HDF5 1.8+ | ✅ Yes | ⚠️ Limited | ❌ No |
| Advanced Datatypes | ✅ All | ✅ Yes | ❌ No |
| Test Suite Validation | ✅ 100% (378/378) | ⚠️ Unknown | ❌ No |
| Maintained | ✅ Active | ⚠️ Slow | ❌ Inactive |
| Thread-safe | ⚠️ User must sync* | ⚠️ Conditional | ❌ No |

\* Different `File` instances are independent. Concurrent access to same `File` requires user synchronization (standard Go practice). Full thread-safety with mutexes + SWMR mode planned for future releases.

---

## 📖 HDF5 Resources

- [HDF5 Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
- [Official HDF5 Library](https://github.com/HDFGroup/hdf5)
- [HDF Group](https://www.hdfgroup.org/)

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 🙏 Acknowledgments

- The HDF Group for the HDF5 format specification
- gonum/hdf5 for inspiration
- All contributors to this project

### Special Thanks

**Professor Ancha Baranova** - This project would not have been possible without her invaluable help and support. Her assistance was crucial in bringing this library to life.

---

## 📞 Support

- 📖 [Documentation](docs/) - Architecture and guides
- 🐛 [Issue Tracker](https://github.com/scigolib/hdf5/issues)
- 💬 [Discussions](https://github.com/scigolib/hdf5/discussions) - Community Q&A and announcements
- 🌐 [HDF Group Forum](https://forum.hdfgroup.org/t/pure-go-hdf5-library-production-release-with-hdf5-2-0-0-compatibility/13584) - Official HDF5 community discussion

---

**Status**: Stable - HDF5 2.0.0 compatible with security hardening
**Version**: v0.13.5 (Critical checksum fix - Jenkins lookup3 for h5dump/h5py compatibility)
**Last Updated**: 2026-02-02

---

*Built with ❤️ by the HDF5 Go community*
*Recognized by [HDF Group Forum](https://forum.hdfgroup.org/t/pure-go-hdf5-library-production-release-with-hdf5-2-0-0-compatibility/13584)* ⭐
