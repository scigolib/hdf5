# Installation Guide

How to install and verify the HDF5 Go library.

---

## Requirements

### Minimum Requirements
- **Go**: Version 1.25 or later
- **Operating System**: Windows, Linux, macOS, or any Go-supported platform
- **Architecture**: amd64, arm64, or any Go-supported architecture
- **Dependencies**: none outside the Go standard library (pure Go, no cgo)

### Optional (for contributors)
- **golangci-lint**: for code quality checks
- **Python/h5py**: for generating reference test files (test files can also be written with this library)

---

## Installation Methods

### Method 1: Using `go get` (Recommended)

The simplest way to install the library:

```bash
go get github.com/scigolib/hdf5
```

This downloads the latest release into the module cache and adds it to the `go.mod` of the module
in the current directory, so programs of that module can import it.

### Method 2: Using `go.mod` (For Projects)

Add the library to your project's `go.mod`:

```bash
# Initialize a Go module if you haven't already
go mod init myproject

# Add the library
go get github.com/scigolib/hdf5
```

Or add the import to your code and run `go mod tidy`, which adds the `require` line with the latest
release.

### Method 3: Clone and Build (For Development)

If you want to contribute or modify the library:

```bash
# Clone the repository
git clone https://github.com/scigolib/hdf5.git
cd hdf5

# Verify installation
go test ./...
```

---

## Verifying Installation

### Basic Verification

Create a file `test_install.go`:

```go
package main

import (
    "fmt"

    "github.com/scigolib/hdf5"
)

func main() {
    // Unlimited is a constant of the package, so this fails to build if the import is wrong.
    fmt.Printf("hdf5 imported, Unlimited = %#x\n", hdf5.Unlimited)
}
```

Run it inside a module that requires the library:
```bash
go run test_install.go
```

Expected output:
```
hdf5 imported, Unlimited = 0xffffffffffffffff
```

The installed version is shown by `go list -m github.com/scigolib/hdf5`.

### Functional Verification

Test with an actual HDF5 file:

```go
package main

import (
    "fmt"
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    // Replace with path to your HDF5 file
    file, err := hdf5.Open("yourfile.h5")
    if err != nil {
        log.Fatalf("Failed to open file: %v", err)
    }
    defer file.Close()

    fmt.Printf("Opened HDF5 file\n")
    fmt.Printf("   Superblock version: %d\n", file.SuperblockVersion())

    // Count objects
    var groupCount, datasetCount int
    file.Walk(func(path string, obj hdf5.Object) {
        switch obj.(type) {
        case *hdf5.Group:
            groupCount++
        case *hdf5.Dataset:
            datasetCount++
        }
    })

    fmt.Printf("   Groups: %d\n", groupCount)
    fmt.Printf("   Datasets: %d\n", datasetCount)
}
```

---

## Platform-Specific Notes

### Windows

No special requirements. The library is pure Go and has no C dependencies.

```powershell
# PowerShell
go get github.com/scigolib/hdf5

# Verify
go version  # Should show Go 1.25+
```

**Common Windows issues**:
- Ensure Go is properly installed and in PATH
- Use forward slashes `/` or escaped backslashes `\\` in file paths

### Linux

No special requirements.

```bash
# Verify Go version
go version  # Should show Go 1.25+

# Install library
go get github.com/scigolib/hdf5
```

CI runs the tests on Ubuntu, macOS and Windows runners.

### macOS

No special requirements.

```bash
# Verify Go version
go version  # Should show Go 1.25+

# Install library
go get github.com/scigolib/hdf5
```

---

## Development Setup

### For Contributors

If you're contributing to the library:

```bash
# Clone repository
git clone https://github.com/scigolib/hdf5.git
cd hdf5

# Install development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run tests
go test ./...

# Run linter
golangci-lint run ./...

# Check test coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Setting Up Python (Optional)

For generating test HDF5 files:

```bash
# Install Python dependencies
pip install h5py numpy

# Verify installation
python -c "import h5py, numpy; print('h5py', h5py.__version__)"
```

---

## Docker Usage

If you prefer using Docker:

```dockerfile
# Dockerfile
FROM golang:1.25-alpine

WORKDIR /app

# Copy your Go code
COPY . .

# Build your application (go.mod already requires the library)
RUN go build -o myapp .

CMD ["./myapp"]
```

Build and run:
```bash
docker build -t myapp .
docker run myapp
```

---

## Updating the Library

### Update to Latest Version

```bash
go get -u github.com/scigolib/hdf5
```

### Update to Specific Version

```bash
go get github.com/scigolib/hdf5@vX.Y.Z   # a tag from the releases page
```

### Check Current Version

```bash
go list -m github.com/scigolib/hdf5
```

---

## Troubleshooting Installation

### "module not found" Error

**Problem**:
```
go: github.com/scigolib/hdf5@vX.Y.Z: reading github.com/scigolib/hdf5/go.mod at revision vX.Y.Z: unknown revision vX.Y.Z
```

**Solution**:
```bash
# Use latest version (recommended)
go get github.com/scigolib/hdf5@latest

# Or without version tag
go get github.com/scigolib/hdf5
```

### "Go version too old" Error

**Problem**:
```
go: github.com/scigolib/hdf5 requires go >= 1.25
```

**Solution**:
Update Go to version 1.25 or later:
- **Official Go downloads**: https://golang.org/dl/
- **Ubuntu/Debian**: `sudo snap install go --classic`
- **macOS**: `brew install go`
- **Windows**: Download from https://golang.org/dl/

### Import Path Issues

**Problem**:
```
package github.com/scigolib/hdf5: cannot find package
```

**Solution**:
```bash
# Ensure GOPATH is set correctly
echo $GOPATH  # Should show a path like /home/user/go

# Run go mod tidy to resolve dependencies
go mod tidy

# Clear Go module cache if needed
go clean -modcache
```

### Network/Proxy Issues

**Problem**: Cannot download the library due to network restrictions.

**Solution**:
```bash
# Set up Go proxy
export GOPROXY=https://proxy.golang.org,direct

# Or use a different proxy
export GOPROXY=https://goproxy.cn,direct

# Then try again
go get github.com/scigolib/hdf5
```

---

## Next Steps

After installation, explore:

1. [Quick Start Guide](QUICKSTART.md): first programs that read and write files
2. [Reading Data](READING_DATA.md): datasets, attributes and partial reads
3. [Examples](../../examples/): seven runnable programs
4. [API Reference](https://pkg.go.dev/github.com/scigolib/hdf5): GoDoc

---

## Getting Help

- **Documentation**: https://github.com/scigolib/hdf5/tree/main/docs
- **Issues**: https://github.com/scigolib/hdf5/issues
- **Discussions**: https://github.com/scigolib/hdf5/discussions

---

*Last Updated: 2026-11-07*
