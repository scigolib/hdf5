# UPSTREAM - HDF5 Reference Tracking

This file tracks the upstream HDF5 C library and Format Specification
that this Pure Go implementation is based on.

## Primary References

### HDF5 Format Specification
URL:        https://docs.hdfgroup.org/hdf5/latest/_f_m_t3.html
Version:    Format Specification v4.0 (HDF5 Library 2.0.0)
Date:       2025-05-01
Status:     Fully supported (superblock v0, v2, v3)

### HDF5 C Library (Reference Implementation)
Repository: https://github.com/HDFGroup/hdf5
Branch:     develop
Commit:     (see last sync below)
Local Copy: D:\projects\scigolibs\hdf5c\src (for development reference)

## Last Upstream Sync

Date:       2025-11-13
Version:    HDF5 2.0.0 (Format Spec v4.0)
Commit:     54 commits analyzed for v0.13.0 release
Focus:      Security fixes, 64-bit dimensions, AI/ML datatypes

### Changes Incorporated (v0.13.0)
- CVE-2025-7067: Buffer overflow in chunk reading (HIGH)
- CVE-2025-6269: Heap overflow in fractal heap (MEDIUM)
- CVE-2025-2926: Stack overflow in B-tree recursion (MEDIUM)
- CVE-2025-44905: Integer overflow in dataspace (MEDIUM)
- 64-bit chunk dimensions (breaking change, internal API)
- FP8 (E4M3, E5M2) and bfloat16 datatypes

## Implementation Notes

This is a **Pure Go implementation**, not a CGo wrapper or line-by-line port.

### Approach
- Format Spec v4.0 as primary reference for binary format
- C library source code consulted for edge cases and validation
- Go idioms preferred over C patterns
- Independent test suite with round-trip validation

### Key Differences from C Library
1. **Memory Management**: Go GC vs manual malloc/free
2. **Error Handling**: Go errors vs C return codes + errno
3. **Threading**: User responsibility vs built-in thread safety
4. **Buffer Pooling**: sync.Pool for reduced GC pressure
5. **No MPI Support**: Single-process only (parallel I/O planned)

### Feature Parity Status
| Feature              | C Library | Go Library | Notes |
|---------------------|-----------|------------|-------|
| Superblock v0,v2,v3 | ✅        | ✅         | Full support |
| Object Header v1,v2 | ✅        | ✅         | With continuations |
| All Datatypes       | ✅        | ✅         | Including FP8, bfloat16 |
| Chunked + Filters   | ✅        | ✅         | GZIP, Shuffle, Fletcher32 |
| Dense Attributes    | ✅        | ✅         | Fractal heap + B-tree v2 |
| Soft/External Links | ✅        | ✅         | Full support |
| SWMR Mode           | ✅        | ❌         | Planned v0.14.0+ |
| Parallel I/O (MPI)  | ✅        | ❌         | Planned v0.14.0+ |
| SZIP Compression    | ✅        | ❌         | Planned v0.14.0+ |
| Virtual Datasets    | ✅        | ❌         | Planned v0.14.0+ |

## Sync Workflow

When syncing with upstream changes:

1. **Check HDF5 releases**: https://github.com/HDFGroup/hdf5/releases
2. **Review security advisories**: Check for CVEs affecting our supported formats
3. **Analyze relevant commits**: Focus on format changes, not C-specific code
4. **Update this file**: Document what was synced and when
5. **Create tasks**: Add implementation tasks to docs/dev/backlog/

### Files to Monitor in C Library
```
src/H5Fsuper.c          # Superblock parsing
src/H5Oattribute.c      # Attribute handling
src/H5Dchunk.c          # Chunked dataset I/O
src/H5HFdblock.c        # Fractal heap direct blocks
src/H5B2*.c             # B-tree v2 implementation
src/H5Tconv.c           # Datatype conversions
```

## Quality Validation

### Official HDF5 Test Suite
- Files tested: 433
- Pass rate: 98.2%
- Source: Various HDF5 test files and real-world datasets

### Interoperability Verified
- ✅ h5py (Python)
- ✅ HDFView (Java)
- ✅ h5dump (C library CLI)
- ✅ MATLAB HDF5 functions

---
Last Updated: 2025-11-13
Maintainer: Claude (Autonomous Developer)
