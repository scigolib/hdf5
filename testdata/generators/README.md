# Test File Generators

This directory contains scripts and programs for generating HDF5 test files used in the library's test suite.

---

## 📁 Contents

### Python Generators

**Requirements**: Python 3.x, h5py, numpy

```bash
# Install dependencies
pip install h5py numpy
```

#### create_test_files.py
**Purpose**: Generate standard test files for all superblock versions

**Generated Files**:
- `v0.h5` - HDF5 1.0 format (superblock v0)
- `v2.h5` - HDF5 1.8 format (superblock v2)
- `v3.h5` - HDF5 1.10+ format (superblock v3)
- `with_groups.h5` - File with nested group structure

**Usage**:
```bash
python create_test_files.py
```

**Output**: Creates test files in parent directory (`testdata/`)

---

#### create_test_file.py
**Purpose**: Simple test file generator

**Usage**:
```bash
python create_test_file.py
```

---

#### create_minimal_file.py
**Purpose**: Generate minimal valid HDF5 file

**Usage**:
```bash
python create_minimal_file.py
```

**Output**: Creates `minimal.h5` with basic structure

---

### Go Generators

#### generate_test_files.go
**Purpose**: Go-based test file generation (currently basic)

**Usage**:
```bash
go run generate_test_files.go
```

**Note**: Most test files are currently generated using Python/h5py for compatibility with reference implementation

---

## 🚀 Quick Start

### Generate All Test Files

```bash
cd testdata/generators
python create_test_files.py
```

This creates all standard test files used by the test suite.

---

## 📋 Test File Descriptions

### v0.h5
- **Format**: Superblock version 0
- **Library Version**: HDF5 1.0-1.6
- **Features**: Traditional group structure
- **Tests**: Legacy format support

### v2.h5
- **Format**: Superblock version 2
- **Library Version**: HDF5 1.8+
- **Features**: Streamlined superblock
- **Tests**: Modern format support

### v3.h5
- **Format**: Superblock version 3
- **Library Version**: HDF5 1.10+
- **Features**: SWMR support
- **Tests**: Latest format features

### with_groups.h5
- **Format**: Superblock version 2
- **Features**: Nested groups, multiple datasets
- **Structure**:
  ```
  /
  ├── dataset1
  └── subgroup/
      ├── dataset2
      └── nested_group/
          └── nested_data
  ```
- **Tests**: Group traversal, hierarchy handling

---

## 🔧 Adding New Test Files

### Using Python

```python
import h5py
import numpy as np

# Create file with specific version
with h5py.File('test.h5', 'w', libver='v108') as f:
    # Add datasets
    f.create_dataset('data', data=np.arange(100))

    # Add groups
    grp = f.create_group('my_group')
    grp.create_dataset('sub_data', data=[1, 2, 3])
```

**libver options**:
- `'earliest'` - HDF5 1.0 (superblock v0)
- `'v108'` - HDF5 1.8 (superblock v2)
- `'latest'` - Latest version (superblock v3)

---

## 📚 Test File Best Practices

1. **Descriptive Names**: Use clear, descriptive filenames
   - ✅ `v3_with_compression.h5`
   - ❌ `test123.h5`

2. **Document Purpose**: Add file description to this README

3. **Minimal Size**: Keep test files as small as possible
   - Use small datasets
   - Only include necessary features

4. **Version Coverage**: Ensure coverage of all format versions
   - Superblock versions: 0, 2, 3
   - Object header versions: 1, 2

5. **Feature Testing**: Create specific files for each feature
   - Compression
   - Chunking
   - Attributes
   - External links

---

## 🧪 Verifying Generated Files

### Using h5dump (if available)
```bash
h5dump -H test.h5  # Show header only
h5dump test.h5     # Full dump
```

### Using Go Library
```bash
cd ../..
go run cmd/dump_hdf5/main.go testdata/test.h5
```

### Using Python
```python
import h5py

with h5py.File('test.h5', 'r') as f:
    print(f.keys())
    print(f['dataset'][:])
```

---

## 🐛 Troubleshooting

### "h5py not found"
```bash
pip install h5py numpy
```

### "ImportError: cannot import name 'Dataset'"
Update h5py:
```bash
pip install --upgrade h5py
```

### Files not created
- Check write permissions in `testdata/` directory
- Verify Python script runs without errors
- Check disk space

---

## 📖 References

- [h5py Documentation](https://docs.h5py.org/)
- [HDF5 Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
- [NumPy Documentation](https://numpy.org/doc/)

---

## 🔄 Maintenance

**When to regenerate files**:
- Adding new format version support
- Testing new features
- Bug fixes requiring specific file structures
- Reference implementation updates

**What to check**:
- File integrity (readable by h5py and our library)
- File size (keep minimal)
- Test coverage (all features tested)

---

*Last Updated: 2025-10-16*
*Maintained by: Project Contributors*
