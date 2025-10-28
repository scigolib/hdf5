# HDF5 C Library Reference Test Files

**Source**: `D:\projects\scigolibs\hdf5c\test\testfiles\`
**Copied**: 2025-10-29
**Count**: 57 files
**Purpose**: Comprehensive testing against official HDF5 C library test suite

## File List

This directory contains official HDF5 C library test files used to validate our pure Go implementation.

See `file-list.txt` for the complete list of files.

## Usage

These files are used by `reference_test.go` in the root directory to ensure compatibility with the official HDF5 format specification and behavior.

## Test Coverage

The test suite attempts to:
- Open each file without errors
- Walk the entire object hierarchy
- Access all datasets and groups
- Read dataspace and datatype metadata
- Access attributes

## Expected Results

All 57 files should pass the reference tests. Any failures indicate:
1. Incomplete format support
2. Parsing bugs
3. Edge cases not yet handled

## Maintenance

When updating test files:
1. Copy new files from C library: `D:\projects\scigolibs\hdf5c\test\testfiles\`
2. Update count in this README
3. Regenerate file-list.txt: `ls -1 *.h5 > file-list.txt`
4. Run tests: `go test -v -run TestReference_AllFiles`
