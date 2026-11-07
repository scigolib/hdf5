# Examples

Runnable programs for the HDF5 Go library. Each directory is a `main` package of the module
`github.com/scigolib/hdf5`, so they build with the rest of the repository (`go build ./...`).

Most examples read the sample files in [`../testdata/`](../testdata/) through a relative path, so the
directory you run them from matters. The table gives the working directory for each.

| Example | What it does | Run from | Command |
|---------|--------------|----------|---------|
| [01-basic](01-basic/) | Opens five sample files (superblock versions 0, 2 and 3, a file with groups, an HDF5 1.12 file), prints the superblock version and the object tree. If Python with h5py and numpy is installed, it first regenerates some of the sample files | repository root | `go run ./examples/01-basic` |
| [02-list-objects](02-list-objects/) | Walks `simple_float64.h5` and lists its groups and datasets | `examples/` | `go run ./02-list-objects` |
| [03-read-dataset](03-read-dataset/) | Reads numeric datasets (a 1D float64 array, a 2x3 matrix, several datasets in one file) with `Dataset.Read` and prints the values | `examples/` | `go run ./03-read-dataset` |
| [04-vlen-strings](04-vlen-strings/) | Opens `vlen_strings.h5`, which holds a compound dataset with variable-length string members, and prints the superblock and the dataset's address and a description of how variable-length strings are stored in the global heap. It does not print the string values | `examples/` | `go run ./04-vlen-strings` |
| [05-comprehensive](05-comprehensive/) | Opens several sample files and prints their superblock, object tree and dataset information | `examples/05-comprehensive` | `go run .` |
| [06-write-dataset](06-write-dataset/) | Creates `output.h5` in the current directory with integer, float32 and float64 datasets, one of them a 2D matrix, written with `FileWriter.CreateDataset` and `DatasetWriter.Write` | anywhere | `go run ./examples/06-write-dataset` |
| [07-rebalancing](07-rebalancing/) | The B-tree rebalancing options (`default`, `lazy`, `incremental`) for deleting many attributes; `smart` shows `WithSmartRebalancing`, which is accepted and has no effect yet | `examples/07-rebalancing` | `go run ./default` (also `./lazy`, `./incremental`, `./smart`) |

Examples 01 to 05 have a README with a walkthrough of the code; [07-rebalancing](07-rebalancing/README.md)
explains each rebalancing program.

Examples 06 and 07 write their output files (`output.h5`, `01-default-output.h5` and similar) into the
current directory; delete them when you are done.

Build all of them:

```bash
go build ./examples/...
```

## Sample files

The read examples use files from `testdata/`:

| File | Content |
|------|---------|
| `v0.h5`, `v2.h5`, `v3.h5` | small files with superblock versions 0, 2 and 3 |
| `with_groups.h5` | nested groups and datasets |
| `simple_float64.h5`, `matrix_2x3.h5`, `multiple_datasets.h5` | numeric datasets |
| `vlen_strings.h5` | a compound dataset with variable-length string members |
| `test_v1.12_simple.h5` | a file from HDF5 1.12 |

Example 01 can regenerate some of these with Python (`pip install h5py numpy`); Python is optional,
and without it the example uses the files already in the repository.

## Using the API

A minimum read and write, as the examples do it:

```go
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        values, err := ds.Read() // numeric data as []float64
        if err != nil {
            log.Printf("%s: %v", path, err)
            return
        }
        fmt.Println(path, values)
    }
})
```

```go
fw, err := hdf5.CreateForWrite("out.h5", hdf5.CreateTruncate)
if err != nil {
    log.Fatal(err)
}
defer fw.Close()

ds, err := fw.CreateDataset("/temperature", hdf5.Float64, []uint64{3})
if err != nil {
    log.Fatal(err)
}
if err := ds.Write([]float64{20.5, 21.0, 19.8}); err != nil {
    log.Fatal(err)
}
```

Strings are read with `ReadStrings` and compound data with `ReadCompound`.

## More

- [Quick Start](../docs/guides/QUICKSTART.md), [Reading Data](../docs/guides/READING_DATA.md) and
  [Datatypes](../docs/guides/DATATYPES.md)
- [Troubleshooting](../docs/guides/TROUBLESHOOTING.md) and [FAQ](../docs/guides/FAQ.md)
- [API reference](https://pkg.go.dev/github.com/scigolib/hdf5)
- [Architecture overview](../docs/architecture/OVERVIEW.md)
