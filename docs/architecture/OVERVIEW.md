# Architecture Overview

How the pure Go HDF5 library is built. Supported features, validation results and limitations are
listed in the "Current Status" section of [README.md](../../README.md).

**Last Updated**: 2026-11-07

---

## Design Principles

1. Pure Go: no cgo, for portability
2. Read and write
3. Format accuracy: the HDF5 file format specification and the C library are the references
4. Idiomatic Go: readable, testable code
5. Errors instead of panics or unbounded allocations on damaged files: sizes and addresses read from a file are checked before use

---

## Package Structure

```
github.com/scigolib/hdf5/                 package hdf5, the public API
├── file.go, group.go, dataset_metadata.go   open a file, groups, dataset information
├── dataset_read_hyperslab.go                partial reads
├── dataset_chunk_iterator.go                chunk-by-chunk iteration
├── compound.go, compound_values.go          compound types
├── file_write.go, group_write.go            create files and groups, modify existing ones
├── dataset_write.go, dataset_write_chunked.go   dataset writing
├── attribute_write.go, dense_attribute_rmw.go   attribute writing and modification
├── link_create.go, link_write.go            hard, soft and external links
├── delete_write.go                          deleting links and objects
├── rebalancing_options.go                   B-tree rebalancing options
├── errors.go, warnings.go                   sentinel errors, reader warnings
├── read_limit.go                            the read limit
│
├── internal/core/        parsing and encoding of HDF5 structures: superblock, object headers
│                         (v1, v2), messages, datatypes, dataspace, layouts, filter pipeline,
│                         attributes, the dataset reader, chunk indexes
├── internal/structures/  B-trees (v1 and v2), fractal heap, local heap, symbol tables, links
├── internal/writer/      the write path: FileWriter (file I/O, free-space allocator) and the
│                         GZIP, Shuffle, Fletcher32, LZF filters
├── internal/rebalancing/ workload detection and mode selection for smart rebalancing (not
│                         connected to the public options yet)
├── internal/utils/       bounds checks, byte-order helpers, buffer pool, error sentinels
├── internal/testing/     mock readers for tests
│
├── cmd/dump_hdf5/        hex dumper of HDF5 files
├── examples/             seven runnable programs
└── testdata/             reference files
```

The tree lists packages and the roles of their main files; run `git ls-files` for the full list.

---

## Layers

1. Public API (package `hdf5`): `File`, `Group`, `Dataset` for reading; `FileWriter`,
   `DatasetWriter`, `GroupWriter` for writing. It manages file lifetime, navigation and
   user-facing errors.
2. Object model: groups and datasets are built when a file is opened, from the object headers
   found by following the root group.
3. Core structures (`internal/core`): binary parsing and encoding of the format's structures,
   with version-specific handling (superblock versions 0, 2 and 3; object headers 1 and 2).
4. Data structures (`internal/structures`): the indexes and heaps the format builds on, for
   reading and writing.

### Base address and user blocks

A file may begin with a user block (for example the 128-byte MAT header of a MATLAB v7.3 file);
the HDF5 superblock then starts at a base address equal to the user block size, and every
address in the file is relative to it. The reader applies the base with
`utils.NewBaseReader`. The writer applies it in `internal/writer.FileWriter`, which adds the
base before each I/O call, as the HDF5 file driver layer does (v0.15.0). `WithUserBlock` and
`FileWriter.WriteUserBlock` create and fill a user block; `OpenForWrite` keeps the user block
of an existing file unchanged.

---

## Data Flow

### Reading an HDF5 File

```
User Code
    ↓
hdf5.Open(filename)
    ↓
[1] File signature validation
    ↓
[2] Superblock parsing (core.ReadSuperblock)
    ├─→ Determine version (0, 2, or 3)
    ├─→ Validate checksum (v2/v3: CRC32)
    ├─→ Read offset/length sizes
    ├─→ Determine endianness
    └─→ Extract root group address
    ↓
[3] Load root group (loadGroup)
    ↓
[4] Parse object header (v1 or v2)
    ├─→ Read header messages
    ├─→ Handle continuation blocks (v1)
    └─→ Extract attributes (compact or dense)
    ↓
[5] Load children (symbol table or B-tree)
    ↓
[6] Recursively load child objects
    ↓
Return File object to user
```

### Writing an HDF5 File

```
User Code
    ↓
hdf5.CreateForWrite(filename, mode)
    ↓
[1] Create/truncate file
    ↓
[2] Write Superblock v2 (modern format, default)
    ├─→ Choose offset/length sizes
    ├─→ Initialize root group address
    └─→ Write checksum
    Note: v2/v3 read and write support fully implemented
    ↓
[3] Initialize space allocator
    ↓
CreateDataset(path, dtype, dims)
    ↓
[4] Allocate dataset space
    ├─→ Choose layout (contiguous/chunked)
    ├─→ Apply filters (GZIP, Shuffle)
    └─→ Allocate storage address
    ↓
[5] Create object header (v1 or v2)
    ├─→ Add Datatype message
    ├─→ Add Dataspace message
    ├─→ Add Layout message
    └─→ Add Filter Pipeline message (if compressed)
    ↓
[6] Update parent group
    ├─→ Add to symbol table (traditional)
    └─→ OR add to B-tree v2 (dense)
    ↓
WriteAttribute(name, value)
    ↓
[7] Choose storage (compact or dense)
    ├─→ If ≤7 attrs → Compact (in object header)
    └─→ If ≥8 attrs → Dense (fractal heap + B-tree v2)
    ↓
[8] Upsert semantics
    ├─→ If attribute exists → Replace (modify)
    └─→ If not exists → Add (create)
    ↓
Close()
    ↓
[9] Flush all buffers to disk
```

---

## Design Notes

- Attribute writes are upserts: `WriteAttribute` replaces an attribute of the same name and adds
  one otherwise, so callers do not check for existence first. Storage switches from compact
  (object header) to dense (fractal heap and version 2 B-tree) at eight attributes.
- Dense attribute storage can be modified in place: the version 2 B-tree and the fractal heap are
  read, changed and written back.
- Fields of 1, 2, 4 or 8 bytes: the sizes of addresses and lengths come from the superblock, and
  the readers take them as parameters instead of assuming 8 bytes.
- Structures are recognised by their 4-byte signatures (`OHDR`, `TREE`, `BTHD`, `FHDB`, `HEAP`,
  `SNOD`), and the superblock by the 8-byte file signature.
- Errors carry context with `%w`. Sentinels let callers tell the cause with `errors.Is`:
  `ErrCorrupt`, `ErrNotSupported`, `ErrLimit`, `ErrNoData`, `ErrNeedsRepack`.
- Buffers for reading binary data are pooled (`internal/utils`).

---

## Format Version Support

### Superblock Versions

| Version | Read | Write | Features |
|---------|------|-------|----------|
| 0 | yes | yes | Original format (HDF5 1.0-1.6) |
| 1 | no | no | Same as v0 with B-tree K values |
| 2 | yes | yes | Streamlined format (HDF5 1.8+) |
| 3 | yes | yes | HDF5 2.0.0 format (48-byte, CRC32 checksum) |

### Object Header Versions

| Version | Read | Write | Notes |
|---------|------|-------|-------|
| 1 | yes | yes | Legacy format (HDF5 < 1.8) with continuations |
| 2 | yes | yes | Modern format (HDF5 1.8+) |

### Attribute Storage

| Storage | Trigger | Read | Write | Modify |
|---------|---------|------|-------|--------|
| Compact | 0-7 attrs | yes | yes | yes |
| Dense | 8+ attrs | yes | yes | yes |

### Dataset Layouts

| Layout | Read | Write | Notes |
|--------|------|-------|-------|
| Compact | yes | yes | Small datasets stored in the object header |
| Contiguous | yes | yes | Simple flat layout |
| Chunked | yes | yes | With GZIP/Shuffle filters |

### Datatypes

| Type | Read | Write | Examples |
|------|------|-------|----------|
| Integer | yes | yes | int8, int16, int32, int64, uint* |
| Float | yes | yes | float32, float64 |
| String | yes | yes | Fixed-length, variable-length |
| Array | yes | yes | [3]float64, [2][2]int32 |
| Enum | yes | yes | Named integer constants |
| Reference | yes | yes | Object references |
| Opaque | yes | yes | Binary blobs with tag |
| Compound | yes | yes | Struct-like with nested members |

---

## API Design Principles

### 1. Simple operations are simple

```go
// Reading
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    fmt.Println(path)
})

// Writing
fw, err := hdf5.CreateForWrite("output.h5", hdf5.CreateTruncate)
if err != nil {
    log.Fatal(err)
}
defer fw.Close()

ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{3})
if err != nil {
    log.Fatal(err)
}
if err := ds.Write([]float64{1.0, 2.0, 3.0}); err != nil {
    log.Fatal(err)
}
if err := ds.WriteAttribute("units", "meters"); err != nil {
    log.Fatal(err)
}
```

More control is available through options:
```go
// A chunked dataset with Shuffle and GZIP
_, err = fw.CreateDataset("/bigdata", hdf5.Float64, []uint64{1000, 1000},
    hdf5.WithChunkDims([]uint64{100, 100}),
    hdf5.WithShuffle(),
    hdf5.WithGZIPCompression(6),
)
```

### 2. Fail early

Errors are returned as soon as they are detected:
- An invalid signature, an out-of-bounds address or an unsupported version fails the open
- Invalid metadata wraps `ErrCorrupt`, an over-limit read wraps `ErrLimit`, and valid HDF5 the library does not read (an undecodable filter, for example) wraps `ErrNotSupported`; callers match them with `errors.Is`

---

## Performance Considerations

Memory:
- Buffers for binary reads are pooled
- Chunked datasets are written chunk by chunk
- Partial reads (`ReadSlice`, `ReadHyperslab`) and `ChunkIterator` read only the chunks they need

I/O:
- The file is read through `io.ReaderAt`
- There is no read-ahead buffering and no parallel chunk I/O

Concurrency:
- Separate `File` values are independent; one `File` used from several goroutines needs the caller's synchronization
- There is no SWMR mode

No timings are given here; measure your workload with the benchmarks in `release_bench_test.go` and `btree_rebalancing_bench_test.go`.

---

## References

- [HDF5 File Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
- [HDF5 C library](https://github.com/HDFGroup/hdf5), the reference implementation
- [Go package naming](https://go.dev/blog/package-names)

---

*Last Updated: 2026-11-07*
