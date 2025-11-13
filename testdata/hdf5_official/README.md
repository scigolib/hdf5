# Official HDF5 Test Suite

**Source**: HDF5 1.14.6 official distribution
**Location**: Copied from `D:\projects\scigolibs\hdf5c\`
**Purpose**: Comprehensive format validation and compatibility testing

## Contents

- **433 .h5 files** - Official HDF5 test files from C library
- **567 .ddl files** - Text dump files for validation

## Test File Categories

### Valid Files (Expected to Pass)
- Basic datasets (all datatypes)
- Groups and attributes
- Chunked and compressed datasets
- Complex structures
- Legacy formats (HDF5 1.0-1.6)
- Modern formats (HDF5 1.8+)

### Invalid Files (Expected to Fail)
- Intentionally corrupted files
- Unsupported features (rare)
- Edge cases for error handling

## Usage

Run the official test suite:
```bash
go test -v -run TestOfficialHDF5Suite
```

Run with coverage:
```bash
go test -v -coverprofile=coverage.out -run TestOfficialHDF5Suite
```

## Success Criteria

- **Pass rate**: >95% for valid files
- **No false positives**: All passes must be correct
- **Documented failures**: All failures explained
- **Performance**: Suite runs in <10 minutes

## References

- **HDF5 Source**: https://github.com/HDFGroup/hdf5
- **Version**: HDF5 1.14.6 (latest stable)
- **Recommendation**: dave.allured (HDF Forum expert, 2025-11-04)
- **Forum Thread**: https://forum.hdfgroup.org/t/13572

## Notes

Some files may be platform-specific or test edge cases that are out of scope for initial v0.12.0 release. All failures are documented in test output with explanations.

---

*Last Updated: 2025-11-13*
*For: HDF5 Go Library v0.12.0*
