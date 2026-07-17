# HDF5 Go Library - Development Roadmap

**Current version**: v0.14.0 (see [CHANGELOG.md](CHANGELOG.md) for release history)

## 🎯 Vision

A pure-Go HDF5 library: no CGo, cross-platform, verified against the official
C library. Read support covers the default HDF5 format produced by any 1.x/2.x
release; write output is validated with h5dump/h5diff/h5repack in CI.

## 📅 Planned

### v0.15.x — Quick wins

- **LZF write support** — the pure-Go compressor exists in
  `internal/writer/filter_lzf.go`; wire it to a public `WithLZFCompression`
  option (h5py/PyTables compatible).
- **float16 read** — decode `H5T_IEEE_F16LE/BE` datasets and attributes
  (predefined since HDF5 1.14.4, common in ML-adjacent files).
- **Public compound-write API** — a builder for compound datatypes so users
  don't need internal packages.
- **Soft/external link resolution** — resolve links on explicit access
  (chain following, cycle detection, dangling-link errors).

### v0.16.x — HDF5 2.x "latest" format read support

- **Data layout message v4** with the modern chunk indexes:
  single chunk, implicit, fixed array, extensible array, v2 B-tree.
  This is what currently blocks reading files written with
  `H5F_LIBVER_LATEST` bounds (h5py `libver="latest"`, netCDF-4 defaults on
  new toolchains).
- **Complex datatypes** (datatype class 11, new in HDF5 2.0) — read as
  `[]complex128`/`[]complex64`.

### v1.0.0 — Long-term support

- API freeze after community validation of the v0.15/v0.16 features.
- Multi-level B-tree v2 (node splits/merges) if dense-group workloads
  demand it — deliberately deferred while trees fit one leaf.

## ❌ Not planned

- **SZIP write** — patent/libaec-encumbered; no pure-Go implementation.
- **BZIP2 write** — stdlib `compress/bzip2` is decompress-only; adding a
  dependency contradicts the zero-dep design. Read support stays.
- **User-configurable B-tree rebalancing** — the C library exposes no such
  API; deletions rebalance automatically. (An earlier speculative
  Smart/Lazy/Incremental rebalancing API was removed in the v0.15 cleanup.)

## 📚 Resources

- [CHANGELOG.md](CHANGELOG.md) — full release history
- [UPSTREAM.md](UPSTREAM.md) — format-spec and C-library version tracking
- [docs/architecture/OVERVIEW.md](docs/architecture/OVERVIEW.md) — internals
- [GitHub Discussions](https://github.com/scigolib/hdf5/discussions) — support
