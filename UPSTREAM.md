# UPSTREAM - HDF5 Reference Tracking

This file tracks the upstream HDF5 C library and Format Specification
that this Pure Go implementation is based on.

## Primary References

### HDF5 Format Specification
URL:        https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html
Version:    Format Specification v4.0 (HDF5 Library 2.0.0)
Date:       2025-05-01
Status:     Fully supported (superblock v0, v2, v3)

### HDF5 C Library (Reference Implementation)
Repository: https://github.com/HDFGroup/hdf5
Tag:        2.2.0
Commit:     [`49df1b4e5`](https://github.com/HDFGroup/hdf5/commit/49df1b4e5ca4108d56e80f06321a749da1535268)
Date:       2026-07-28 (release)
Local Copy: Clone repository for development reference

## Upstream Sync History

### Latest Sync: 2026-11-14

**Reference**: tag [2.2.0](https://github.com/HDFGroup/hdf5/commit/49df1b4e5ca4108d56e80f06321a749da1535268)
(2026-07-28); releases in the range: 2.1.0 (2026-03-02), 2.1.1 (2026-03-23, packaging only), 2.2.0
**Previous sync**: develop [`bc8f6bb02a`](https://github.com/HDFGroup/hdf5/commit/bc8f6bb02aaa8b162bf04b42f2b59bcbbbfcf9f8) (2025-12-19)
**Commits Analyzed**: 242 since the previous sync (301 from tag hdf5_2.0.0)

No serialized metadata layout changed in the range: the chunk index, B-tree, fractal and local heap,
object header, attribute, layout and datatype message readers had no format change, only hardening,
one reader relaxation and new predefined datatypes.

#### New CVEs Identified

| CVE | File | Status |
|-----|------|--------|
| CVE-2025-44904 | H5Dchunk.c | ✅ Not affected (stored chunk size is bounded by the file and the chunk maximum; short data is rejected) |
| CVE-2025-2309 | H5Odtype.c | ✅ Not affected (the same bitfield offset and precision checks are in place) |
| CVE-2025-2308 | H5Zscaleoffset.c | ✅ Not affected (filter not implemented) |
| CVE-2026-17572 | H5SMcache.c | ✅ Not affected (shared message index not decoded) |
| CVE-2026-17574 | H5Odtype.c | ⚠️ Strictness gap: the variable-length type is not validated yet (TASK-103) |
| CVE-2025-9274 | H5A.c | ✅ Not affected (C API only) |

#### Format and Reader Changes

- 2.1.0 added the predefined FP6 (E2M3, E3M2) and FP4 (E2M1) datatypes: float class, one byte,
  precision 6 or 4, ordinary datatype messages on disk. The decoder reads them; conversion to
  float32/float64 is not implemented yet (TASK-106).
- 2.1.0 allows filters on variable-length datatypes
  ([`b074586d9`](https://github.com/HDFGroup/hdf5/commit/b074586d9ea853bf509ac97c9cf8f17c60e73a40));
  the filter pipeline is datatype-agnostic.
- 2.2.0 accepts layout messages whose chunk dimensions are encoded in more bytes than necessary
  ([`e124c36c5`](https://github.com/HDFGroup/hdf5/commit/e124c36c55bf69e9087352a91c77e62ad52f4306));
  the layout parser already accepts any stored width from 1 to 8.
- 2.2.0 validates the superblock base, the stored EOF and the driver and extension addresses
  ([`1da455e53`](https://github.com/HDFGroup/hdf5/commit/1da455e53a3fe11698815415b0012ee3cec8c03a));
  aligning the decoder is TASK-105.
- 2.2.0 hardened the chunk-size checks for unfiltered chunks (CVE-2025-44904,
  [`d069f1526`](https://github.com/HDFGroup/hdf5/commit/d069f15269509d14f86479f760b2edc62c0c6e9b));
  comparing the stored size with the computed chunk size is TASK-104.

#### Tasks Created

- **TASK-103**: validate the variable-length datatype type at decode (CVE-2026-17574 parity)
- **TASK-104**: compare an unfiltered chunk's stored size with the computed chunk size
  (CVE-2025-44904 parity)
- **TASK-105**: align superblock decode validation with HDF5 2.2.0
- **TASK-106**: interop fixtures for the 2.1/2.2 reader changes; decide FP6/FP4 value support

---

### Previous Sync: 2025-12-19

**Commit**: [`bc8f6bb02a`](https://github.com/HDFGroup/hdf5/commit/bc8f6bb02aaa8b162bf04b42f2b59bcbbbfcf9f8)
**Commits Analyzed**: 34 new commits since last sync

#### New CVEs Identified

| CVE | Severity | File | Status |
|-----|----------|------|--------|
| [CVE-2025-2308](https://github.com/HDFGroup/hdf5/pull/5960) | HIGH | H5Zscaleoffset.c | ✅ Not affected (filter not implemented) |
| [CVE-2025-2309](https://github.com/HDFGroup/hdf5/pull/5963) | HIGH | H5Odtype.c | ✅ Not affected (bitfield data conversion not implemented) |

#### Other Notable Changes
- R-tree optimizations ([#6039](https://github.com/HDFGroup/hdf5/pull/6039))
- Zero-element reads on virtual datasets fix ([#6083](https://github.com/HDFGroup/hdf5/pull/6083))
- HDF5 2.0.0 HISTORY files updated

#### Tasks Created
- **TASK-034**: CVE-2025-2309 bitfield datatype security review (MEDIUM) ✅ DONE - Not affected
- **TASK-035**: CVE-2025-2308 documentation (N/A - not affected) ✅ DONE
- **TASK-036**: R-tree optimizations review (LOW)

---

### Earlier Sync: 2025-11-13 (v0.13.0 Release)

**Commit**: `e99a49585760aeb34f2bb2144d8e80e207a68ec4`
**Version**: HDF5 2.0.0 (Format Spec v4.0)
**Commits Analyzed**: 54 commits

#### CVEs Incorporated (v0.13.0)
- CVE-2025-7067: Buffer overflow in chunk reading (HIGH) ✅
- CVE-2025-6269: Heap overflow in fractal heap (MEDIUM) ✅
- CVE-2025-2926: Stack overflow in B-tree recursion (MEDIUM) ✅
- CVE-2025-44905: Integer overflow in dataspace (MEDIUM) ✅

#### Features Added
- 64-bit chunk dimensions (breaking change, internal API)
- FP8 (E4M3, E5M2) and bfloat16 datatypes

---

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
| All Datatypes       | ✅        | ✅         | Including FP8, bfloat16, FP6 and FP4 (FP6/FP4 decode only, no float conversion yet) |
| Bitfield Datatype   | ✅        | ❌         | Not supported (explicit rejection) |
| Chunked + Filters   | ✅        | ✅         | GZIP, Shuffle, Fletcher32, LZF |
| BZIP2 Filter        | ✅        | ⚠️         | Read only (stdlib) |
| Scale-Offset Filter | ✅        | ❌         | Not implemented |
| Dense Attributes    | ✅        | ✅         | Fractal heap + B-tree v2 |
| Soft/External Links | ✅        | ✅         | Full support |
| SWMR Mode           | ✅        | ❌         | Planned v0.14.0+ |
| Parallel I/O (MPI)  | ✅        | ❌         | Planned v0.14.0+ |
| SZIP Compression    | ✅        | ❌         | Stub only (requires libaec) |
| Virtual Datasets    | ✅        | ❌         | Planned v0.14.0+ |

## Sync Workflow

When syncing with upstream changes:

1. **Check HDF5 releases**: https://github.com/HDFGroup/hdf5/releases
2. **Review security advisories**: Check for CVEs affecting our supported formats
3. **Pull latest**: `git pull origin develop` (in your local HDF5 clone)
4. **Analyze relevant commits**: Focus on format changes, not C-specific code
5. **Update this file**: Document what was synced and when
6. **Create tasks**: Add implementation tasks to docs/dev/backlog/

### Files to Monitor in C Library
```
src/H5Fsuper.c          # Superblock parsing
src/H5Oattribute.c      # Attribute handling
src/H5Dchunk.c          # Chunked dataset I/O
src/H5HFdblock.c        # Fractal heap direct blocks
src/H5B2*.c             # B-tree v2 implementation
src/H5Tconv.c           # Datatype conversions
src/H5Odtype.c          # Datatype object header messages
src/H5Zscaleoffset.c    # Scale-offset filter (not implemented)
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
Last Updated: 2025-12-19
Maintainer: Claude (Autonomous Developer)
