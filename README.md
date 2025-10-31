# HDF5 Go Library

> **Pure Go implementation of the HDF5 file format** - No CGo required

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/scigolib/hdf5)](https://goreportcard.com/report/github.com/scigolib/hdf5)
[![CI](https://github.com/scigolib/hdf5/actions/workflows/test.yml/badge.svg)](https://github.com/scigolib/hdf5/actions)
[![Coverage](https://img.shields.io/badge/coverage-89.7%25-brightgreen.svg)](https://github.com/scigolib/hdf5/actions)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-v0.11.2--beta-green.svg)](ROADMAP.md)
[![GoDoc](https://pkg.go.dev/badge/github.com/scigolib/hdf5.svg)](https://pkg.go.dev/github.com/scigolib/hdf5)

A modern, pure Go library for reading and writing HDF5 files without CGo dependencies. Read support is feature-complete, write support advancing rapidly!

---

## ✨ Features

- ✅ **Pure Go** - No CGo, no C dependencies, cross-platform
- ✅ **Modern Design** - Built with Go 1.25+ best practices
- ✅ **HDF5 Compatibility** - Read: v0, v2, v3 superblocks | Write: v0, v2 superblocks
- ✅ **Full Dataset Reading** - Compact, contiguous, chunked layouts with GZIP
- ✅ **Rich Datatypes** - Integers, floats, strings (fixed/variable), compounds
- ✅ **Memory Efficient** - Buffer pooling and smart memory management
- ✅ **Production Ready** - Read support feature-complete (v0.10.0-beta)
- ✍️ **Write Support Advancing** - v0.11.2-beta: Legacy format support (v0 superblock + Object Header v1)!

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
- **[Examples](examples/)** - Working code examples (5 examples with detailed documentation)

---

## 🎯 Current Status

**Version**: v0.11.2-beta (RELEASED 2025-11-01 - Legacy format support) ✅

**Production Readiness: Read support feature-complete! Write support advancing rapidly!** 🎉

### ✅ Fully Implemented
- **File Structure**:
  - Superblock parsing (v0, v2, v3)
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
  - Filter pipeline for compressed data ✨ NEW

- **Datatypes** (Read + Write):
  - **Basic types**: int8-64, uint8-64, float32/64
  - **Strings**: Fixed-length (null/space/null-padded), variable-length (via Global Heap)
  - **Advanced types**: Arrays, Enums, References (object/region), Opaque ✨ v0.11.0-beta
  - **Compound types**: Struct-like with nested members

- **Attributes**:
  - Compact attributes (in object header) ✨ NEW
  - Dense attributes (fractal heap foundation) ✨ NEW
  - Attribute reading for groups and datasets ✨ NEW
  - Full attribute API (Group.Attributes(), Dataset.Attributes()) ✨ NEW

- **Navigation**: Full file tree traversal via Walk()

- **Code Quality**:
  - Test coverage: 89.7% in internal/ (target: >70%) ✅
  - Lint issues: 0 (34+ linters) ✅
  - TODO items: 0 (all resolved) ✅
  - 57 reference HDF5 test files ✅

### ⚠️ Partial Support
- **Dense Attributes**: Infrastructure ready, B-tree v2 iteration deferred to v0.11.0-RC (<10% of files affected)

### ✍️ Write Support (v0.11.2-beta)
- ✅ **File creation** - CreateForWrite() with Truncate/Exclusive modes
- ✅ **Superblock formats** - v0 (legacy, HDF5 < 1.8) + v2 (modern, HDF5 >= 1.8) ✨ NEW
- ✅ **Object headers** - v1 (legacy, 16-byte) + v2 (modern, 4-byte min) ✨ NEW
- ✅ **Dataset writing** - Contiguous + chunked layouts, all datatypes
- ✅ **Chunked datasets** - Chunk storage with B-tree v1 indexing
- ✅ **Compression** - GZIP (deflate), Shuffle filter, Fletcher32 checksum
- ✅ **Groups** - Symbol table + dense groups (automatic transition at 8+ links)
- ✅ **Attributes** - Compact (0-7) + dense (8+) with automatic transition
- ✅ **Advanced datatypes** - Arrays, Enums, References, Opaque
- ✅ **Free space management** - End-of-file allocation (validated, 100% coverage)
- ✅ **Legacy compatibility** - Files readable by HDF5 1.0+ tools ✨ NEW

**Known Limitations (v0.11.2-beta)**:
- Dense storage read-modify-write (adding after file reopen - v0.11.3-beta)
- Attribute modification/deletion (write-once only)
- Some files not h5dump-readable yet (working on full compatibility)

### ❌ Planned Features

**v0.11.3-beta (Next)** - Continue Write Support:
- Dense storage read-modify-write (add to existing after reopen)
- Attribute modification/deletion
- Hard/soft/external links
- h5dump compatibility improvements

**v0.11.0-RC (Q1 2026)** - Feature Complete:
- Compound datatypes for attributes
- SWMR (Single Writer Multiple Reader)
- API freeze
- Community testing

**v1.1.0+ (After Stable)** - Extended Features:
- Other compression (SZIP, LZF, BZIP2)
- Virtual datasets / external files
- Parallel I/O
- Advanced filters (N-bit, Scale-offset)

See [ROADMAP.md](ROADMAP.md) for detailed timeline.

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
| Reading | ✅ Full (v0.10.0) | ✅ Full | ❌ Limited |
| Writing | ✅ MVP (v0.11.0) | ✅ Full | ❌ No |
| HDF5 1.8+ | ✅ Yes | ⚠️ Limited | ❌ No |
| Advanced Datatypes | ✅ Yes (v0.11.0) | ✅ Yes | ❌ No |
| Maintained | ✅ Active | ⚠️ Slow | ❌ Inactive |
| Thread-safe | 📋 Planned | ⚠️ Conditional | ❌ No |

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
- 💬 [Discussions](https://github.com/scigolib/hdf5/discussions) *(coming soon)*

---

**Status**: Beta - Read complete, Write support advancing
**Version**: v0.11.2-beta (Legacy format support: v0 superblock + Object Header v1)
**Last Updated**: 2025-11-01

---

*Built with ❤️ by the HDF5 Go community*
*Recognized by [HDF Group Forum](https://forum.hdfgroup.org/t/loking-for-an-hdf5-version-compatible-with-go1-9-2/10021/7)* ⭐
