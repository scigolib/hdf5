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

A modern, pure Go library for reading and writing HDF5 files without CGo dependencies.

Reads files in the default HDF5 format produced by any 1.x/2.x library (superblocks v0/v2/v3). Files written with the 2.x "latest" library-version bounds (data layout v4 chunk indexes), complex datatypes, and float16 are not yet supported — see the roadmap.

---

## ✨ Features

- ✅ **Pure Go** - No CGo, no C dependencies, cross-platform
- ✅ **Modern Design** - Built with Go 1.25+ best practices
- ✅ **Superblocks v0/v2/v3** - Read and write, with checksum validation
- ✅ **Full Dataset Reading** - Compact, contiguous, chunked layouts with GZIP
- ✅ **Rich Datatypes** - Integers, floats, strings (fixed/variable), compounds
- ✅ **Memory Efficient** - Buffer pooling
- ✅ **C-Library Verified** - Write output validated against the official HDF5 tools (h5dump/h5diff/h5repack) in CI
- ✍️ **Comprehensive Write Support** - Datasets, groups, attributes, links, compression

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
- **[Examples](examples/)** - Working code examples (6 examples with detailed documentation)

---

## 🎯 Current Status

**Production-ready read/write for default-format HDF5 files.**

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
  - Test coverage: 88%+ library packages (target: >70%) ✅
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

- ✅ LZF filter (read, Pure Go)
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

| Feature               | This Library       | gonum/hdf5     | go-hdf5/hdf5 |
| --------------------- | ------------------ | -------------- | ------------ |
| Pure Go               | ✅ Yes             | ❌ CGo wrapper | ✅ Yes       |
| Reading               | ✅ Full            | ✅ Full        | ❌ Limited   |
| Writing               | ✅ Full            | ✅ Full        | ❌ No        |
| HDF5 1.8+             | ✅ Yes             | ⚠️ Limited     | ❌ No        |
| Advanced Datatypes    | ✅ All             | ✅ Yes         | ❌ No        |
| Test Suite Validation | ✅ 100% (378/378)  | ⚠️ Unknown     | ❌ No        |
| Maintained            | ✅ Active          | ⚠️ Slow        | ❌ Inactive  |
| Thread-safe           | ⚠️ User must sync* | ⚠️ Conditional | ❌ No        |

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

**Status**: Stable - reads/writes default-format HDF5 files, C-library verified, security hardened

---

_Built with ❤️ by the HDF5 Go community_
_Recognized by [HDF Group Forum](https://forum.hdfgroup.org/t/pure-go-hdf5-library-production-release-with-hdf5-2-0-0-compatibility/13584)_ ⭐
