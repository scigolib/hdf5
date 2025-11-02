# Frequently Asked Questions (FAQ)

> **Quick answers to common questions about the HDF5 Go library**

---

## 📚 Table of Contents

- [General Questions](#general-questions)
- [Features and Capabilities](#features-and-capabilities)
- [Performance](#performance)
- [Compatibility](#compatibility)
- [Development and Contributing](#development-and-contributing)
- [Roadmap and Future](#roadmap-and-future)

---

## 🌟 General Questions

### What is this library?

This is a **pure Go implementation** of the HDF5 file format for reading and writing HDF5 files. It requires no CGo or C dependencies, making it fully cross-platform and easy to deploy.

### Why use this instead of existing Go HDF5 libraries?

**Advantages**:
- ✅ **Pure Go** - No CGo, no C dependencies
- ✅ **Cross-platform** - Works on any Go-supported platform (Windows, Linux, macOS, ARM, etc.)
- ✅ **Easy deployment** - Single binary, no library dependencies
- ✅ **Modern** - Built with Go 1.25+ best practices
- ✅ **Actively maintained** - Regular updates and improvements

**Trade-offs**:
- ⚠️ **Write support advancing** - v0.11.4-beta has smart rebalancing + attribute modification complete, more features coming in v0.11.5+
- ⚠️ **Some advanced features missing** - Compound write, virtual datasets, parallel I/O, SWMR (planned for v0.12.0-rc.1)
- ⚠️ **Slightly slower** - Pure Go is 2-3x slower than C for some operations (but fast enough for most use cases)

### Why pure Go? Why not use CGo?

**Pure Go Benefits**:
1. **Cross-compilation** - Compile for any platform from any platform
2. **No dependencies** - Users don't need HDF5 C library installed
3. **Easier deployment** - Single static binary
4. **Better debugging** - Go debugger works perfectly
5. **Memory safety** - No C memory management issues
6. **Simpler build** - Just `go build`, no complex makefiles

**CGo Drawbacks**:
- Requires HDF5 C library installation
- Complex cross-compilation
- Potential memory leaks at Go/C boundary
- Slower function calls across CGo boundary
- Harder to debug

**Decision**: Pure Go provides better user experience with acceptable performance for both reading and writing. See [ADR-001](../../docs/dev/decisions/ADR-001-pure-go-implementation.md) for details.

### Is it production-ready?

**For reading**: **Feature-complete!** ✅ Production-ready for reading HDF5 files.

**For writing**: **Advancing rapidly!** ✅ v0.11.4-beta has smart rebalancing + attribute modification complete.

**Read Support**:
- ✅ All datatypes (integers, floats, strings, compounds, arrays, enums, references, opaque)
- ✅ All dataset layouts (compact, contiguous, chunked)
- ✅ GZIP compression
- ✅ Groups and hierarchies
- ✅ Attributes (compact and dense)
- ✅ Both old (pre-1.8) and modern (1.8+) HDF5 files

**Write Support (v0.11.4-beta)**:
- ✅ File creation (Truncate/Exclusive modes)
- ✅ Dataset writing (contiguous + chunked layouts, all datatypes)
- ✅ Chunked datasets (B-tree v1 indexing, chunk storage)
- ✅ Compression (GZIP/deflate, Shuffle filter, Fletcher32 checksum)
- ✅ Group creation (symbol table + dense groups with automatic transition)
- ✅ Attribute writing (compact 0-7 + dense 8+ with automatic transition)
- ✅ **Dense storage RMW** (Read-Modify-Write for existing dense attributes) ✨ NEW
- ✅ Advanced datatypes (arrays, enums, references, opaque)
- ✅ Legacy formats (Superblock v0 + Object Header v1)

**Limitations (v0.11.4-beta)**:
- ⚠️ Attribute modification/deletion (write-once only)
- ⚠️ Soft/external links not yet supported
- ⚠️ Compound datatypes write support
- ⚠️ Other compression formats (SZIP, LZF) - planned for v1.1.0+

**Quality metrics**:
- Test coverage: 86.1%
- Lint issues: 0 (34+ linters)
- 57 reference test files
- 200+ test cases

### Who is this library for?

**Perfect for**:
- 📊 **Data scientists** reading HDF5 datasets in Go
- 🔬 **Researchers** processing scientific data (astronomy, climate, physics)
- 🏢 **Developers** building data analysis tools
- 🚀 **DevOps** needing cross-platform HDF5 readers
- 🐳 **Docker users** wanting minimal container dependencies

**Not ideal for** (yet):
- Applications requiring all advanced HDF5 features (virtual datasets, parallel I/O, SWMR)
- Performance-critical loops requiring C-level speed
- Attribute modification/deletion (write-once only for now)

---

## 🎯 Features and Capabilities

### Can I read dataset values?

**Yes!** Full dataset reading is supported for:

- ✅ **Numeric types**: int32, int64, float32, float64 → `[]float64`
- ✅ **Strings**: Fixed and variable-length → `[]string`
- ✅ **Compound types**: Struct-like data → `[]map[string]interface{}`

```go
// Numeric datasets
data, err := ds.Read()  // Returns []float64

// String datasets
strings, err := ds.ReadStrings()  // Returns []string

// Compound datasets
compounds, err := ds.ReadCompound()  // Returns []map[string]interface{}
```

See [Reading Data Guide](READING_DATA.md) for details.

### Can I write HDF5 files?

**Yes! Write support advancing rapidly in v0.11.4-beta.** ✅

**What's supported (v0.11.4-beta)**:
```go
// Create new HDF5 file
fw, err := hdf5.CreateForWrite("output.h5", hdf5.CreateTruncate)
if err != nil {
    log.Fatal(err)
}
defer fw.Close()

// Create groups
grp, _ := fw.CreateGroup("/experiments")

// Write datasets (all datatypes supported)
ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
ds.Write(myFloat64Data)

// Advanced datatypes
arrDs, _ := fw.CreateDataset("/arrays", hdf5.ArrayFloat32, []uint64{10},
    hdf5.WithArrayDims([]uint64{3, 3}))  // Array of 3x3 float32

enumDs, _ := fw.CreateDataset("/status", hdf5.EnumInt8, []uint64{5},
    hdf5.WithEnumValues([]string{"OK", "ERROR"}, []int64{0, 1}))
```

**Current limitations (v0.11.4-beta)**:
- Attribute modification/deletion (write-once only)
- Soft/external links not yet supported
- Compound datatype write support

**Coming soon**:
- **v0.11.4-beta**: Links support, attribute modifications
- **v0.12.0-rc.1**: Feature complete, API freeze, community testing
- **v1.0.0**: Production-ready write support

See [ROADMAP.md](../../ROADMAP.md) for details.

### Can I read attributes?

**Yes!** Full attribute reading support:

```go
// Group attributes
attrs, err := group.Attributes()

// Dataset attributes
attrs, err := dataset.Attributes()

// Access attribute values
for _, attr := range attrs {
    fmt.Printf("%s = %v (type: %T)\n", attr.Name, attr.Value, attr.Value)
}
```

**Supported**:
- ✅ Compact attributes (in object header)
- ✅ Dense attributes (fractal heap direct blocks)

**Limitation**: Dense attributes in B-tree v2 (rare, <10% of files) deferred to v0.12.0-rc.1.

### What datatypes are supported?

**Fully Supported (Read + Write)**:

| HDF5 Type | Go Type | Read | Write |
|-----------|---------|------|-------|
| H5T_INTEGER | int8-64, uint8-64 | ✅ | ✅ |
| H5T_FLOAT | float32, float64 | ✅ | ✅ |
| H5T_STRING | string | ✅ | ✅ |
| H5T_ARRAY | fixed arrays | ✅ | ✅ |
| H5T_ENUM | named integers | ✅ | ✅ |
| H5T_REFERENCE | object/region refs | ✅ | ✅ |
| H5T_OPAQUE | binary blobs | ✅ | ✅ |

**Partial Support**:
- H5T_COMPOUND: ✅ Read, ❌ Write (planned)
- H5T_VLEN: ✅ Read, ❌ Write (planned)

**Not Supported**:
- H5T_TIME - deprecated in HDF5 since v1.4, never fully implemented

See [Datatypes Guide](DATATYPES.md) for detailed type mapping.

### What compression formats work?

**Supported**:
- ✅ **GZIP/Deflate** (filter ID 1) - Covers 95%+ of files

**Not Yet Supported**:
- ❌ SZIP (filter ID 2)
- ❌ LZF (filter ID 32000)
- ❌ BZIP2 (filter ID 307)
- ❌ Blosc, LZ4, Zstd (custom filters)

**Workaround**: Convert files to GZIP:

```bash
h5repack -f GZIP=6 input.h5 output.h5
```

GZIP compression fully supported (both reading and writing).

### What HDF5 versions are supported?

**Superblock Versions**:
- ✅ **Version 0** (HDF5 1.0 - 1.6)
- ❌ Version 1 (rare, not needed)
- ✅ **Version 2** (HDF5 1.8+)
- ✅ **Version 3** (HDF5 1.10+ with SWMR)

**Object Header Versions**:
- ✅ **Version 1** (pre-HDF5 1.8)
- ✅ **Version 2** (HDF5 1.8+)

**File Formats**:
- ✅ Traditional groups (symbol tables)
- ✅ Modern groups (object headers)
- ✅ Both old and new B-tree formats

**Compatibility**: Reads and writes files compatible with HDF5 1.0 (1998) through latest HDF5 1.14+ (2024). Ready for future HDF5 formats (will be added in v1.x.x updates).

### Does it support large files?

**Yes**, with some considerations:

**File Size**:
- ✅ Files up to several GB work well
- ✅ Files up to 100+ GB can be read (not all loaded into memory at once)
- ⚠️ Memory usage scales with number of objects, not file size

**Large Datasets**:
- ✅ Chunked datasets can be any size (read chunk-by-chunk)
- ⚠️ Entire dataset loaded into memory on `Read()` (streaming API planned for v0.12.0-rc.1)

**Best Practices**:
- Process datasets one at a time
- Use `Walk()` efficiently (don't repeat)
- Close files promptly

**Example**:
```go
// Good: Process incrementally
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, _ := ds.Read()
        processData(data)  // Process immediately
        // data will be garbage collected
    }
})
```

---

## ⚡ Performance

### How fast is it compared to the C library?

**Reading Speed**: Typically **2-3x slower** than C library for raw I/O.

**Why acceptable**:
1. For most applications, I/O is not the bottleneck
2. Decompression (GZIP) is already fast in Go
3. Easier deployment and maintenance worth the trade-off
4. Sufficient for scientific data analysis workflows

**Performance**:
- C-based libraries (gonum/hdf5, Python h5py): Fast (native C implementation)
- This library: Slower (pure Go implementation)

**Expected trade-off**:
- Pure Go is typically 2-3x slower than C for I/O-heavy operations
- For most scientific data analysis, file I/O is not the bottleneck
- Decompression (GZIP) and computation dominate processing time

**Why pure Go is still worth it**:
- Cross-platform deployment (single binary, no dependencies)
- Easier to build, maintain, and distribute
- Sufficient for typical scientific workflows
- Future optimization: SIMD, assembly, better algorithms

**Note**: Formal benchmarks planned for future releases. Performance varies by operation type, dataset size, and compression.

### Is it thread-safe?

**Currently**: **No** - Each `File` instance should be used from a single goroutine.

**Workaround**: Open separate file handles per goroutine:

```go
// Each goroutine opens its own handle
func processDataset(filename string, datasetPath string) {
    file, _ := hdf5.Open(filename)
    defer file.Close()

    // Find and process dataset
    // ...
}

// Concurrent processing
var wg sync.WaitGroup
for _, dsPath := range datasetPaths {
    wg.Add(1)
    go func(path string) {
        defer wg.Done()
        processDataset("data.h5", path)
    }(dsPath)
}
wg.Wait()
```

**Future**: Full thread-safety with mutexes + SWMR mode planned for v0.12.0-rc.1.

### Can I stream large datasets?

**Current**: No - entire dataset read into memory.

**Future**: Streaming/chunked reading API planned for v0.12.0-rc.1:

```go
// Future API (not available yet)
reader, _ := ds.ChunkReader()
for reader.Next() {
    chunk := reader.Chunk()  // Process one chunk at a time
    processChunk(chunk)
}
```

**Workaround**: Use `Info()` to check size before reading:

```go
info, _ := ds.Info()
fmt.Println(info)  // Check "Total size" before reading

// Only read if size is acceptable
if /* size < threshold */ {
    data, _ := ds.Read()
    processData(data)
}
```

---

## 🔧 Compatibility

### Which operating systems are supported?

**All Go-supported platforms**:
- ✅ **Windows** (7, 10, 11, Server)
- ✅ **Linux** (Ubuntu, Debian, CentOS, Fedora, Arch, etc.)
- ✅ **macOS** (Intel and Apple Silicon)
- ✅ **FreeBSD, OpenBSD, NetBSD**
- ✅ **Solaris, AIX**

**Architectures**:
- ✅ amd64 (x86_64)
- ✅ arm64 (Apple Silicon, ARM servers)
- ✅ 386 (32-bit x86)
- ✅ arm (32-bit ARM)
- ✅ All other Go-supported architectures

Pure Go = runs anywhere Go runs!

### Can I use it with Docker?

**Yes!** Perfect for Docker due to no C dependencies.

**Minimal Dockerfile**:
```dockerfile
FROM golang:1.25-alpine

WORKDIR /app
COPY . .

RUN go get github.com/scigolib/hdf5
RUN go build -o myapp .

CMD ["./myapp"]
```

**Benefits**:
- No need for HDF5 C library in image
- Smaller image size
- Faster builds
- Cross-platform containers

### Does it work with Python h5py-created files?

**Yes!** Fully compatible with files created by Python h5py.

**Tested with**:
- h5py versions 2.x and 3.x
- NumPy arrays
- Pandas DataFrames (via to_hdf)

**Example**:
```python
# Create file with Python
import h5py
import numpy as np

with h5py.File('data.h5', 'w') as f:
    f.create_dataset('numbers', data=np.arange(100))
    f.create_dataset('strings', data=['hello', 'world'])
```

```go
// Read with Go
file, _ := hdf5.Open("data.h5")
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    // Works perfectly!
})
```

### Can I read files created by MATLAB, IDL, or other tools?

**Usually yes**, if they follow HDF5 standard format.

**Tested with**:
- ✅ MATLAB (save with '-v7.3' flag)
- ✅ IDL (HDF5 format)
- ✅ NASA HDF5 files
- ✅ Climate/weather model outputs (NetCDF4-based HDF5)

**MATLAB Example**:
```matlab
% Save as HDF5 format
data = rand(100, 100);
save('data.mat', 'data', '-v7.3');  % -v7.3 uses HDF5
```

```go
// Read with Go library
file, _ := hdf5.Open("data.mat")
// Works! MATLAB .mat v7.3 is HDF5 format
```

**Note**: Some tools add proprietary metadata. Core data reading works, but some metadata may not be fully parsed.

### Can I read NetCDF4 files?

**Partially**. NetCDF4 is built on HDF5, so basic reading works:

```go
// NetCDF4 files have .nc extension but use HDF5 format
file, err := hdf5.Open("climate.nc")
if err == nil {
    // Can read datasets
    // NetCDF metadata in attributes
}
```

**Limitations**: NetCDF-specific conventions (dimensions, coordinate variables) are not interpreted. You'll see raw HDF5 structure.

**Future**: Dedicated NetCDF4 support may be added in future versions.

---

## 👥 Development and Contributing

### How can I contribute?

**Ways to contribute**:
1. 🐛 **Report bugs** - Open issues with detailed reproduction
2. 💡 **Suggest features** - Request features via issues or discussions
3. 📝 **Improve documentation** - Fix typos, add examples
4. 🔧 **Submit pull requests** - Add features or fix bugs
5. ⭐ **Star the project** - Show support on GitHub
6. 📢 **Spread the word** - Tell others about the library

**Getting started**:
1. Read [CONTRIBUTING.md](../../CONTRIBUTING.md)
2. Check [open issues](https://github.com/scigolib/hdf5/issues)
3. Join discussions on GitHub

### What's the development workflow?

**Git Flow**:
- `main` branch: Stable releases only
- `develop` branch: Active development (default)
- Feature branches: `feature/object-header-v1`
- Release branches: `release/v0.11.x-beta`

**Process**:
1. Fork repository
2. Create feature branch from `develop`
3. Implement with tests
4. Run `golangci-lint run ./...`
5. Ensure `go test ./...` passes
6. Open pull request to `develop`

### What's the testing strategy?

**Test Coverage**: 76.3% (target: >70%)

**Types of tests**:
1. **Unit tests**: Test individual functions
2. **Integration tests**: Test with real HDF5 files
3. **Reference tests**: Compare with `h5dump` output

**Test files**:
- 57 reference HDF5 files covering various formats
- Generated with Python h5py for reproducibility

**Quality checks**:
- 34+ linters (golangci-lint)
- Race detector (`go test -race`)
- Cross-platform testing (Windows, Linux, macOS)

### How is the library documented?

**Multiple levels**:
1. **User guides**: docs/guides/ (Installation, Reading Data, etc.)
2. **Architecture docs**: docs/architecture/ (How it works)
3. **API reference**: GoDoc (pkg.go.dev)
4. **Examples**: examples/ (Working code)
5. **Development docs**: docs/dev/ (for contributors, private)

**Documentation principles**:
- Clear examples
- Explain "why" not just "how"
- Keep up-to-date with code changes

---

## 🗺️ Roadmap and Future

### What's the current write support status?

**Already Available** (v0.11.4-beta):
- ✅ File creation with multiple superblock formats (v0, v2)
- ✅ Dataset writing: contiguous and chunked layouts
- ✅ Compression: GZIP, Shuffle filter, Fletcher32 checksum
- ✅ Groups: symbol table and dense formats
- ✅ Attributes: compact (0-7) and dense (8+) storage
- ✅ **Dense storage RMW** (add to existing after reopen) ✨ NEW
- ✅ Advanced datatypes: arrays, enums, references, opaque
- ✅ Legacy format support (v0 superblock + Object Header v1)

**Coming Soon** (v0.11.4-beta):
- Attribute modification and deletion
- Links support (soft/external)
- Indirect blocks for fractal heap

See [ROADMAP.md](../../ROADMAP.md) for complete roadmap.

### What features are planned?

**Near-term** (v0.11.x-beta releases):
- ✅ MVP write support
- ✅ Chunked datasets + compression
- ✅ Dense groups and attributes
- ✅ Legacy format support (v0 superblock)
- ✅ Dense storage RMW ✨ COMPLETE
- ⏳ Attribute modification/deletion (next)
- ⏳ Links support (soft/external) (next)

**Feature Complete** (v0.12.0-rc.1 - Q1 2026):
- Compound datatypes write
- Thread-safety with mutexes
- SWMR (Single Writer Multiple Reader)
- Streaming API for large datasets
- All remaining HDF5 features
- API freeze

**Stable Release** (v1.0.0 - Mid-late 2026):
- Community validated
- Production-ready
- Long-term support (2+ years)
- ALL HDF5 formats supported (v0, v2, v3)
- Ready for HDF5 2.0.0 (currently in development, will be added in v1.x.x updates)

### Will v1.0.0 break the API?

**Goal**: Minimal breaking changes.

**Promise**:
- Current reading API will remain stable
- Only additions and optional features
- Deprecations will be announced in advance

**Version strategy**:
- v0.x.x: Beta (may have breaking changes)
- v1.0.0: Stable API guarantee
- v1.x.x: Backward compatible
- v2.0.0: Next major version (only if necessary)

See [ROADMAP.md](../../ROADMAP.md) for versioning strategy.

### Is commercial support available?

**Currently**: Community support via GitHub issues and discussions.

**Future**: Commercial support, consulting, and training may be available. Contact via GitHub if interested.

### How can I stay updated?

**Follow development**:
- ⭐ **Star the repo**: https://github.com/scigolib/hdf5
- 👁️ **Watch releases**: Get notified of new versions
- 📖 **Read CHANGELOG**: See what's new
- 💬 **Join discussions**: Share ideas and feedback

**Communication channels**:
- GitHub Issues: Bug reports and feature requests
- GitHub Discussions: Q&A and community (coming soon)
- Release notes: Detailed changelog with each version

---

## ❓ Still Have Questions?

### Check these resources:

- **[Installation Guide](INSTALLATION.md)** - Setup and verification
- **[Quick Start Guide](QUICKSTART.md)** - Get started in 5 minutes
- **[Reading Data Guide](READING_DATA.md)** - Comprehensive reading guide
- **[Datatypes Guide](DATATYPES.md)** - Type conversion details
- **[Troubleshooting](TROUBLESHOOTING.md)** - Common issues and solutions
- **[ROADMAP](../../ROADMAP.md)** - Future plans

### Get help:

- **GitHub Issues**: https://github.com/scigolib/hdf5/issues
- **GitHub Discussions**: https://github.com/scigolib/hdf5/discussions
- **Documentation**: https://github.com/scigolib/hdf5/tree/main/docs

---

*Last Updated: 2025-11-02*
*Version: 0.11.4-beta*
