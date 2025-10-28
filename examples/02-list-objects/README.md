# Example 02: List Objects

> **Advanced file navigation and object hierarchy exploration**

## What This Example Demonstrates

- Detailed object traversal
- Reading group children
- Exploring nested structures
- Understanding object relationships

## Quick Start

```bash
go run main.go
```

## What You'll See

**Output**:
```
Superblock version: 2

Objects in file:
  Group: / (children: 1)
    - data
  Dataset: /data
```

## Code Breakdown

### Exploring Groups

```go
case *hdf5.Group:
    fmt.Printf("  Group: %s (children: %d)\n",
        path, len(v.Children()))

    // List all children
    for _, child := range v.Children() {
        fmt.Printf("    - %s\n", child.Name())
    }
```

### Identifying Datasets

```go
case *hdf5.Dataset:
    fmt.Printf("  Dataset: %s\n", path)
```

## Use Cases

### 1. Count Objects by Type

```go
var groups, datasets int

file.Walk(func(path string, obj hdf5.Object) {
    switch obj.(type) {
    case *hdf5.Group:
        groups++
    case *hdf5.Dataset:
        datasets++
    }
})

fmt.Printf("Groups: %d, Datasets: %d\n", groups, datasets)
```

### 2. Build Object Index

```go
index := make(map[string]hdf5.Object)

file.Walk(func(path string, obj hdf5.Object) {
    index[path] = obj
})

// Later: access any object by path
if obj, ok := index["/data"]; ok {
    fmt.Printf("Found: %s\n", obj.Name())
}
```

### 3. Find Deeply Nested Objects

```go
file.Walk(func(path string, obj hdf5.Object) {
    depth := strings.Count(path, "/") - 1
    if depth > 3 {
        fmt.Printf("Deep object: %s (depth: %d)\n",
            path, depth)
    }
})
```

## Next Steps

- **[Example 03](../03-read-dataset/)** - Read dataset values
- **[Reading Data Guide](../../docs/guides/READING_DATA.md)** - Complete guide

---

*Part of the HDF5 Go Library v0.10.0-beta*
