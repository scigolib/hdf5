# Using HDF5 C Library as Reference Implementation

> **Key Advantage**: We're not inventing HDF5 from scratch - we're porting a battle-tested implementation to Go!

This guide explains how to effectively use the official HDF5 C library as a reference when implementing features in the Go library.

---

## 🎯 Why This Matters

### **Massive Time Savings**

**Without Reference** (from spec only):
```
1. Read HDF5 format spec (100+ pages)
2. Interpret ambiguous sections
3. Design algorithm from scratch
4. Implement
5. Debug edge cases (hardest part!)
6. Handle corner cases discovered in production
→ Weeks/months per feature
```

**With C Reference** (proven implementation):
```
1. Read relevant C source (~500-2000 lines)
2. Understand existing algorithm
3. Port to Go idioms
4. Test against C-generated files
5. Edge cases already handled!
→ Days/weeks per feature (3-5x faster!)
```

### **Quality Advantages**

✅ **Proven Algorithms**: 30+ years of production use
✅ **Edge Cases Handled**: Bugs already fixed in C
✅ **Best Practices**: Established patterns to follow
✅ **Test Cases Available**: Use C test suite
✅ **Community Knowledge**: Extensive documentation

---

## 📁 HDF5 C Library Structure

### **Clone the Repository**

```bash
# Clone official HDF5 C library
git clone https://github.com/HDFGroup/hdf5.git hdf5c
cd hdf5c

# Checkout stable version
git checkout hdf5_1_14_3  # Latest stable

# Browse source
cd src/
```

### **Key Directories**

```
hdf5c/
├── src/              # Main source code
│   ├── H5F*.c       # File operations
│   ├── H5D*.c       # Dataset operations
│   ├── H5G*.c       # Group operations
│   ├── H5A*.c       # Attribute operations
│   ├── H5T*.c       # Datatype operations
│   ├── H5S*.c       # Dataspace operations
│   ├── H5O*.c       # Object header operations
│   ├── H5B*.c       # B-tree operations
│   ├── H5FS*.c      # Free space management
│   ├── H5Z*.c       # Filter/compression
│   └── H5private.h  # Internal utilities
│
├── test/            # Test suite (gold mine!)
│   ├── tfile.c      # File tests
│   ├── dsets.c      # Dataset tests
│   ├── tattr.c      # Attribute tests
│   └── *.h5         # Test files
│
├── testfiles/       # Reference HDF5 files
└── doc/             # Documentation
```

---

## 🔍 How to Find Relevant Code

### **Strategy 1: Function Name Mapping**

Go feature → C function prefix:

| Go Feature | C Prefix | Example Files |
|-----------|----------|---------------|
| Open file | `H5F` | `H5Fopen.c` |
| Create file | `H5F` | `H5Fcreate.c` |
| Read dataset | `H5D` | `H5Dread.c` |
| Write dataset | `H5D` | `H5Dwrite.c` |
| Create group | `H5G` | `H5Gcreate.c` |
| Attributes | `H5A` | `H5Aint.c`, `H5Awrite.c` |
| Datatypes | `H5T` | `H5T*.c` |
| Dataspace | `H5S` | `H5S*.c` |
| Object header | `H5O` | `H5Oheader.c`, `H5Omessage.c` |
| B-trees | `H5B` | `H5B*.c` |
| Free space | `H5FS` | `H5FS*.c` |
| Compression | `H5Z` | `H5Zdeflate.c`, `H5Zszip.c` |

### **Strategy 2: Grep for Keywords**

```bash
cd hdf5c/src/

# Find file creation code
grep -r "H5F_create" *.c

# Find dataset write code
grep -r "H5D__write" *.c

# Find chunking implementation
grep -r "H5D_chunk_write" *.c

# Find B-tree insertion
grep -r "H5B_insert" *.c
```

### **Strategy 3: Follow Call Stack**

Example: Understanding how datasets are created

```bash
# 1. Start with public API
vim H5Dcreate.c
# → Calls H5D__create_named()

# 2. Follow internal function
vim H5Dint.c
# → Calls H5D__create()

# 3. Find layout creation
# → Calls H5D__layout_set_io_ops()

# 4. Understand chunking
vim H5Dchunk.c
# → Calls H5D__chunk_construct()
```

---

## 📖 Reading C Code Effectively

### **Understanding C Patterns**

#### **1. Public vs Internal Functions**

```c
// Public API (H5*.c)
hid_t H5Fcreate(const char *filename, unsigned flags, ...)
{
    // User-facing, documented
}

// Internal (H5*int.c, H5*pkg.c)
herr_t H5F__create(...)
{
    // Implementation details, our gold mine!
}
```

**For Go**: Focus on internal (`H5*__*`) functions - they have the real algorithms!

#### **2. Error Handling**

```c
// C pattern
if (some_operation() < 0)
    HGOTO_ERROR(H5E_FILE, H5E_CANTOPENFILE, NULL, "can't open file")

// Translate to Go
if err := someOperation(); err != nil {
    return nil, utils.WrapError("can't open file", err)
}
```

#### **3. Memory Management**

```c
// C: Manual allocation
uint8_t *buffer = malloc(size);
// ... use buffer
free(buffer);

// Go: Automatic + pooling
buf := utils.GetBuffer(size)
defer utils.ReleaseBuffer(buf)
// ... use buf
```

#### **4. Pointer Patterns**

```c
// C: Pointer for output
herr_t H5F_read(H5F_t *file, void *buffer)

// Go: Multiple returns
func (f *File) Read(buffer []byte) (int, error)
```

---

## 🔨 Porting C to Go: Step-by-Step

### **Example: Implementing Dataset Creation**

#### **Step 1: Locate C Implementation**

```bash
cd hdf5c/src/
vim H5Dcreate.c
```

Key function:
```c
hid_t H5Dcreate2(hid_t loc_id, const char *name, hid_t dtype_id,
                 hid_t space_id, hid_t lcpl_id, hid_t dcpl_id,
                 hid_t dapl_id)
{
    H5G_loc_t loc;
    H5D_t *dset = NULL;
    hid_t ret_value;

    // Validate parameters
    if (!name || !*name)
        HGOTO_ERROR(...)

    // Get location
    if (H5G_loc(loc_id, &loc) < 0)
        HGOTO_ERROR(...)

    // Create dataset
    if (NULL == (dset = H5D__create_named(&loc, name, ...)))
        HGOTO_ERROR(...)

    // Register and return ID
    ret_value = H5I_register(H5I_DATASET, dset, ...);

done:
    FUNC_LEAVE_API(ret_value)
}
```

#### **Step 2: Identify Core Algorithm**

```c
// Core function: H5D__create_named() in H5Dint.c
H5D_t *H5D__create_named(const H5G_loc_t *loc, const char *name, ...)
{
    H5D_t *ret_value = NULL;

    // 1. Allocate dataset structure
    if (NULL == (ret_value = H5FL_CALLOC(H5D_t)))
        HGOTO_ERROR(...)

    // 2. Set up layout (contiguous/chunked/compact)
    if (H5D__layout_set_io_ops(ret_value) < 0)
        HGOTO_ERROR(...)

    // 3. Create object header
    if (H5O_create(...) < 0)
        HGOTO_ERROR(...)

    // 4. Write datatype message
    if (H5O_msg_append(..., H5O_DTYPE_ID, ...) < 0)
        HGOTO_ERROR(...)

    // 5. Write dataspace message
    if (H5O_msg_append(..., H5O_SDSPACE_ID, ...) < 0)
        HGOTO_ERROR(...)

    // 6. Write layout message
    if (H5O_msg_append(..., H5O_LAYOUT_ID, ...) < 0)
        HGOTO_ERROR(...)

done:
    return ret_value;
}
```

#### **Step 3: Port to Go**

```go
// Go implementation
func (g *Group) CreateDataset(name string, datatype Datatype,
                               dataspace Dataspace, opts ...Option) (*Dataset, error) {
    // 1. Allocate dataset structure
    ds := &Dataset{
        file: g.file,
        name: path.Join(g.name, name),
    }

    // 2. Set up layout (from opts or default contiguous)
    layout := determineLayout(opts)
    ds.layout = layout

    // 3. Create object header at new address
    headerAddr, err := g.file.allocateSpace(objectHeaderSize)
    if err != nil {
        return nil, utils.WrapError("allocate object header", err)
    }

    // 4. Create and write messages (same order as C!)
    messages := []*HeaderMessage{
        createDatatypeMessage(datatype),    // H5O_DTYPE_ID
        createDataspaceMessage(dataspace),  // H5O_SDSPACE_ID
        createLayoutMessage(layout),        // H5O_LAYOUT_ID
    }

    // 5. Write object header
    if err := writeObjectHeader(g.file, headerAddr, messages); err != nil {
        return nil, utils.WrapError("write object header", err)
    }

    // 6. Link in parent group
    if err := g.addLink(name, headerAddr); err != nil {
        return nil, utils.WrapError("add link", err)
    }

    return ds, nil
}
```

#### **Step 4: Validate Against C**

```go
func TestDatasetCreateCompatibility(t *testing.T) {
    // Create with Go
    f, _ := hdf5.Create("go_test.h5")
    g := f.Root()
    ds, _ := g.CreateDataset("data", hdf5.Int32, hdf5.SimpleDataspace(100))
    ds.Write([]int32{1, 2, 3})
    f.Close()

    // Verify with C tools
    cmd := exec.Command("h5dump", "go_test.h5")
    output, _ := cmd.Output()

    // Should match expected structure
    require.Contains(t, string(output), "DATASET \"data\"")
    require.Contains(t, string(output), "DATATYPE  H5T_STD_I32LE")
}
```

---

## 🧪 Using C Test Suite

### **Gold Mine: test/ Directory**

The C library test suite is incredibly valuable:

```bash
cd hdf5c/test/

# Dataset tests
vim dsets.c
# → Hundreds of test cases!
# → Edge cases we need to handle
# → File generation code we can port
```

### **Porting Test Cases**

Example from `dsets.c`:

```c
// C test case
static herr_t test_simple_io(hid_t file) {
    hid_t dataset, dataspace;
    hsize_t dims[2] = {4, 6};
    int data[4][6];

    // Create dataspace
    dataspace = H5Screate_simple(2, dims, NULL);

    // Create dataset
    dataset = H5Dcreate2(file, "simple", H5T_NATIVE_INT,
                        dataspace, H5P_DEFAULT, H5P_DEFAULT, H5P_DEFAULT);

    // Write data
    H5Dwrite(dataset, H5T_NATIVE_INT, H5S_ALL, H5S_ALL,
            H5P_DEFAULT, data);

    // Close
    H5Dclose(dataset);
    H5Sclose(dataspace);

    return 0;
}
```

Port to Go:

```go
func TestSimpleIO(t *testing.T) {
    // Same test in Go
    f, err := hdf5.Create("test_simple_io.h5")
    require.NoError(t, err)
    defer f.Close()

    // Create dataspace (same dimensions!)
    space := hdf5.SimpleDataspace(4, 6)

    // Create dataset
    ds, err := f.Root().CreateDataset("simple", hdf5.Int32, space)
    require.NoError(t, err)

    // Write data (same pattern!)
    data := make([][]int32, 4)
    for i := range data {
        data[i] = make([]int32, 6)
    }

    err = ds.Write(data)
    require.NoError(t, err)

    // Verify with h5dump
    verifyWithH5Dump(t, "test_simple_io.h5", "simple")
}
```

---

## 📚 Key C Files for Write Support

### **Phase 1: File Creation**

| Feature | C File | Lines | Complexity |
|---------|--------|-------|------------|
| File creation | `H5Fcreate.c` | ~500 | Medium |
| Superblock write | `H5Fsuper.c` | ~1000 | Medium |
| Free space init | `H5FS.c`, `H5MF.c` | ~2000 | High |
| File descriptors | `H5FDcore.c` | ~800 | Low |

**Start here**: `H5Fcreate.c` → Follow call to `H5F__create()`

### **Phase 2: Dataset Writing**

| Feature | C File | Lines | Complexity |
|---------|--------|-------|------------|
| Dataset creation | `H5Dcreate.c` → `H5Dint.c` | ~1200 | Medium |
| Contiguous write | `H5Dcontig.c` | ~600 | Low |
| Chunked write | `H5Dchunk.c` | ~3000 | High |
| B-tree indexing | `H5B.c`, `H5B2.c` | ~2000 | High |
| Layout selection | `H5Dlayout.c` | ~400 | Low |

**Start here**: `H5Dwrite.c` → `H5D__write()` → layout-specific functions

### **Phase 3: Advanced Features**

| Feature | C File | Lines | Complexity |
|---------|--------|-------|------------|
| Attributes | `H5Aint.c`, `H5Awrite.c` | ~1500 | Medium |
| Compression | `H5Zdeflate.c` | ~300 | Low |
| Type conversion | `H5Tconv.c` | ~5000 | Very High |
| Dataspace selection | `H5Sselect.c` | ~2000 | High |

---

## 🔧 Tools and Workflows

### **1. Side-by-Side Development**

```bash
# Terminal 1: Edit Go code
cd ~/projects/hdf5
vim internal/core/dataset_writer.go

# Terminal 2: Reference C code
cd ~/projects/hdf5c/src
vim H5Dwrite.c

# Terminal 3: Test
cd ~/projects/hdf5
go test -run TestWrite
h5dump output.h5
```

### **2. Validation Workflow**

```bash
# 1. Generate test file with Go
go run examples/write_test.go

# 2. Validate structure
h5dump output.h5

# 3. Compare with C-generated equivalent
h5diff go_output.h5 c_output.h5

# 4. Detailed inspection
h5stat output.h5
h5ls -v output.h5
```

### **3. Debugging with h5debug**

```bash
# C library has built-in debug tool
h5debug output.h5 0  # Show superblock at offset 0
h5debug output.h5 96 # Show object header at offset 96

# Use to verify our writes are correct!
```

---

## 💡 Best Practices

### **DO's** ✅

1. **Read C implementation first** before writing Go code
2. **Keep same algorithm order** - easier to verify correctness
3. **Use same message order** in object headers (C has reasons!)
4. **Port test cases** - they cover edge cases
5. **Validate with C tools** after every major feature
6. **Document C references** in Go comments:
   ```go
   // WriteDataset writes data to chunked storage.
   // Reference: H5Dchunk.c::H5D__chunk_write()
   func (d *Dataset) WriteDataset(data interface{}) error {
   ```

### **DON'Ts** ❌

1. **Don't blindly copy C code** - adapt to Go idioms
2. **Don't ignore error handling** in C - they thought of edge cases
3. **Don't skip validation steps** - catch issues early
4. **Don't reinvent algorithms** - C version is proven
5. **Don't forget thread safety** - C uses locks, we need Go patterns

---

## 📊 Progress Tracking

### **Implementation Checklist Template**

For each feature:

```markdown
## Feature: Dataset Writing (Contiguous)

### C Reference Analysis
- [ ] Read `H5Dwrite.c` - entry point
- [ ] Read `H5D__write()` in `H5Dint.c` - dispatcher
- [ ] Read `H5D__contig_write()` in `H5Dcontig.c` - implementation
- [ ] Document algorithm flow
- [ ] Note edge cases and error handling

### Go Implementation
- [ ] Port main algorithm
- [ ] Adapt to Go idioms (errors, interfaces)
- [ ] Add proper error handling
- [ ] Write unit tests
- [ ] Write integration tests

### Validation
- [ ] Generate test file with Go
- [ ] Verify with `h5dump`
- [ ] Compare with C-generated equivalent using `h5diff`
- [ ] Test edge cases from C test suite
- [ ] Stress test (large files, performance)

### Documentation
- [ ] Add godoc comments with C reference
- [ ] Update architecture docs
- [ ] Add usage examples
```

---

## 🎓 Learning Path

### **Week 1-2: Foundation**
1. Clone and build C library
2. Read key files: `H5Fcreate.c`, `H5Fopen.c`
3. Understand file structure
4. Practice with h5dump, h5diff tools

### **Week 3-4: File Operations**
1. Port `H5Fcreate()` to Go
2. Implement superblock writing
3. Validate with C tools
4. Create test suite

### **Week 5-8: Dataset Writing**
1. Port contiguous write
2. Port chunked write
3. Port B-tree indexing
4. Extensive testing

### **Week 9-12: Advanced Features**
1. Compression
2. Attributes
3. Type conversion
4. Production readiness

---

## 📞 When to Reference C vs When to Innovate

### **Always Reference C**:
- File format structures (must match!)
- Object header layout
- Message types and order
- B-tree algorithms
- Checksums and validation

### **Can Innovate in Go**:
- Error handling (use Go errors)
- Concurrency (Go goroutines vs C threads)
- Memory management (Go GC vs C malloc)
- API design (Go interfaces vs C function pointers)
- Testing approach (table-driven tests)

---

## 🚀 Summary

**The C library is our superpower!**

- 🎯 **30+ years** of production-tested code
- 🔧 **Thousands** of edge cases already handled
- 📚 **Extensive** test suite to port
- ⚡ **3-5x faster** development
- ✅ **Proven** algorithms

**Our job**: Port algorithms, adapt to Go, maintain compatibility.

**Not our job**: Reinvent HDF5 from scratch!

---

**Last Updated**: 2025-10-17
**Next Review**: After v1.0 release, before starting v2.0-alpha

---

*Standing on the shoulders of giants - the HDF Group's amazing work!*
