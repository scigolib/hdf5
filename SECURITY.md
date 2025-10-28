# Security Policy

## Supported Versions

HDF5 Go Library is currently in beta. We provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.9.0-beta | :white_check_mark: |
| < 0.9.0 | :x:                |

Future stable releases (v1.0+) will follow semantic versioning with LTS support.

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability in HDF5 Go Library, please report it responsibly.

### How to Report

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please report security issues by:

1. **Private Security Advisory** (preferred):
   https://github.com/scigolib/hdf5/security/advisories/new

2. **Email** to maintainers:
   Create a private GitHub issue or contact via discussions

### What to Include

Please include the following information in your report:

- **Description** of the vulnerability
- **Steps to reproduce** the issue (include malicious HDF5 test file if applicable)
- **Affected versions** (which versions are impacted)
- **Potential impact** (DoS, information disclosure, code execution, etc.)
- **Suggested fix** (if you have one)
- **Your contact information** (for follow-up questions)

### Response Timeline

- **Initial Response**: Within 48-72 hours
- **Triage & Assessment**: Within 1 week
- **Fix & Disclosure**: Coordinated with reporter

We aim to:
1. Acknowledge receipt within 72 hours
2. Provide an initial assessment within 1 week
3. Work with you on a coordinated disclosure timeline
4. Credit you in the security advisory (unless you prefer to remain anonymous)

## Security Considerations for HDF5 Parsing

HDF5 files are complex binary formats. This library parses untrusted binary data, which introduces security risks.

### 1. Malicious HDF5 Files

**Risk**: Crafted HDF5 files can exploit parsing vulnerabilities.

**Attack Vectors**:
- Integer overflow in chunk sizes, dataset dimensions, or buffer allocations
- Buffer overflow when reading superblock, object headers, or data
- Infinite loops in B-tree traversal or group iteration
- Resource exhaustion via deeply nested groups or massive datasets
- Compression bomb attacks (GZIP decompression)

**Mitigation in Library**:
- ✅ Bounds checking on all size fields
- ✅ Validation of HDF5 signatures and magic numbers
- ✅ Sanity checks on dimension sizes and offsets
- ✅ Resource limits on decompression
- 🔄 Ongoing fuzzing and security testing (planned for v1.0)

**User Recommendations**:
```go
// ❌ BAD - Don't trust untrusted HDF5 files without validation
file, _ := hdf5.Open(userUploadedFile)

// ✅ GOOD - Validate file size and structure first
if fileSize > maxAllowedSize {
    return errors.New("file too large")
}
file, err := hdf5.Open(trustedFile)
if err != nil {
    // Parsing failed - potentially malicious file
    return err
}
```

### 2. Integer Overflow Vulnerabilities

**Risk**: HDF5 uses various integer sizes (8/16/32/64-bit) for sizes and offsets. Overflow can lead to incorrect buffer allocations.

**Example Attack**:
```
Chunk size: 0xFFFFFFFFFFFFFFFF (uint64)
After cast to int: -1 or overflow
Result: Small buffer allocated, large data read → buffer overflow
```

**Mitigation**:
- All size fields validated before use
- Safe integer conversions with overflow checks
- Maximum reasonable limits enforced

**Current Limits**:
- Max dataset dimensions: 2^31 per dimension
- Max chunk size: 2GB
- Max string length: 1GB

### 3. Compression Vulnerabilities

**Risk**: GZIP compression can be exploited via compression bombs (small compressed size, huge decompressed size).

**Example Attack**:
```
Compressed chunk: 1 KB
Decompressed size claim: 10 GB
Result: Memory exhaustion, DoS
```

**Mitigation**:
- Decompression ratio limits enforced
- Memory allocation limits
- Streaming decompression with size validation

**Current Limits**:
- Max decompression ratio: 1000:1
- Max decompressed chunk: 2GB

### 4. Resource Exhaustion

**Risk**: HDF5 files can contain deeply nested structures or large numbers of objects.

**Attack Vectors**:
- Deeply nested groups (stack overflow)
- Millions of datasets (memory exhaustion)
- Circular references in object headers (infinite loops)

**Mitigation**:
- Recursion depth limits (max 1000 levels)
- Object count limits during traversal
- Cycle detection in structure traversal

### 5. Path Traversal

**Risk**: Group names and dataset paths could contain `..` or absolute paths.

**Mitigation**:
- Path validation and sanitization
- No filesystem operations based on HDF5 internal names
- User responsible for validating extracted paths

**User Best Practices**:
```go
// ❌ BAD - Don't use HDF5 paths directly for filesystem operations
datasetName := dataset.Name() // Could be "../../etc/passwd"
os.Create(datasetName)

// ✅ GOOD - Sanitize and validate paths
safeName := filepath.Base(dataset.Name())
if !isValidName(safeName) {
    return errors.New("invalid dataset name")
}
```

## Security Best Practices for Users

### Input Validation

Always validate HDF5 files from untrusted sources:

```go
// Validate file size before opening
fileInfo, err := os.Stat(filename)
if err != nil || fileInfo.Size() > maxAllowedSize {
    return errors.New("invalid file")
}

// Open with error handling
file, err := hdf5.Open(filename)
if err != nil {
    // File failed validation - potentially malicious
    log.Printf("Failed to open HDF5 file: %v", err)
    return err
}
defer file.Close()
```

### Resource Limits

Set limits when processing untrusted files:

```go
// Limit dataset sizes
dataset, err := file.OpenDataset("/data")
if err != nil {
    return err
}

dims := dataset.Dataspace().Dims()
totalElements := 1
for _, dim := range dims {
    totalElements *= dim
}

if totalElements > maxAllowedElements {
    return errors.New("dataset too large")
}
```

### Error Handling

Always check errors - parsing failures may indicate malicious files:

```go
// ❌ BAD - Ignoring errors
file, _ := hdf5.Open(filename)
dataset, _ := file.OpenDataset("/data")
data, _ := dataset.Read()

// ✅ GOOD - Proper error handling
file, err := hdf5.Open(filename)
if err != nil {
    return fmt.Errorf("file open failed: %w", err)
}
defer file.Close()

dataset, err := file.OpenDataset("/data")
if err != nil {
    return fmt.Errorf("dataset open failed: %w", err)
}

data, err := dataset.Read()
if err != nil {
    return fmt.Errorf("read failed: %w", err)
}
```

## Known Security Considerations

### 1. Binary Parsing Vulnerabilities

**Status**: Active mitigation via bounds checking and validation.

**Risk Level**: Medium to High

**Description**: Parsing binary HDF5 format involves reading sizes, offsets, and pointers from untrusted data. Malformed files can trigger buffer overflows or integer overflows.

**Mitigation**:
- All reads bounds-checked
- Integer overflow checks before allocations
- Signature validation at all structure boundaries

### 2. Compression Bomb (ZIP Bomb)

**Status**: Partially mitigated.

**Risk Level**: Medium

**Description**: GZIP-compressed chunks with extreme compression ratios can exhaust memory.

**Mitigation**:
- Decompression ratio limits (1000:1)
- Streaming decompression with size checks
- 🔄 **TODO (v1.0)**: Additional memory limits and monitoring

### 3. B-tree Parsing

**Status**: Active development and testing.

**Risk Level**: Medium

**Description**: B-tree structures index large datasets. Malformed B-trees can cause infinite loops or crashes.

**Mitigation**:
- Cycle detection in tree traversal
- Maximum depth limits
- Node validation at each level

### 4. Dependency Security

HDF5 Go Library has minimal dependencies:

- `github.com/stretchr/testify` (dev only) - Testing
- `golang.org/x/sys` (optional) - Platform-specific optimizations

**Monitoring**:
- 🔄 Dependabot enabled (when repository goes public)
- 🔄 Weekly dependency audit (planned)
- ✅ No C dependencies (pure Go)

## Security Testing

### Current Testing

- ✅ Unit tests with malformed data
- ✅ Integration tests with real HDF5 files
- ✅ Linting with 34+ security-focused linters

### Planned for v1.0

- 🔄 Fuzzing with go-fuzz or libFuzzer
- 🔄 Static analysis with gosec
- 🔄 SAST/DAST scanning in CI
- 🔄 Comparison testing against HDF5 C library

## Security Disclosure History

No security vulnerabilities have been reported or fixed yet (project is in beta).

When vulnerabilities are addressed, they will be listed here with:
- **CVE ID** (if assigned)
- **Affected versions**
- **Fixed in version**
- **Severity** (Critical/High/Medium/Low)
- **Credit** to reporter

## Security Contact

- **GitHub Security Advisory**: https://github.com/scigolib/hdf5/security/advisories/new
- **Public Issues** (for non-sensitive bugs): https://github.com/scigolib/hdf5/issues
- **Discussions**: https://github.com/scigolib/hdf5/discussions

## Bug Bounty Program

HDF5 Go Library does not currently have a bug bounty program. We rely on responsible disclosure from the security community.

If you report a valid security vulnerability:
- ✅ Public credit in security advisory (if desired)
- ✅ Acknowledgment in CHANGELOG
- ✅ Our gratitude and recognition in README
- ✅ Priority review and quick fix

---

**Thank you for helping keep HDF5 Go Library secure!** 🔒

*Security is a journey, not a destination. We continuously improve our security posture with each release.*
