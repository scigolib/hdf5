// Package hdf5 provides a pure Go implementation for reading HDF5 files.
// It supports HDF5 format versions 0, 2, and 3, with capabilities for
// reading datasets, groups, attributes, and various data layouts.
package hdf5

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// File represents an open HDF5 file with its metadata and root group.
type File struct {
	osFile *os.File
	sb     *core.Superblock
	root   *Group
}

// Open opens an HDF5 file for reading and returns a File handle.
// The file must be a valid HDF5 file with a supported format version.
func Open(filename string) (*File, error) {
	//nolint:gosec // G304: User-provided filename is intentional for HDF5 file library
	f, err := os.Open(filename)
	if err != nil {
		return nil, utils.WrapError("file open failed", err)
	}

	// Verify HDF5 signature before reading superblock.
	if !isHDF5File(f) {
		_ = f.Close()
		return nil, errors.New("not an HDF5 file")
	}

	// Get file size for address validation.
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, utils.WrapError("file stat failed", err)
	}
	fileSize := fi.Size()

	sb, err := core.ReadSuperblock(f)
	if err != nil {
		_ = f.Close()
		return nil, utils.WrapError("superblock read failed", err)
	}

	file := &File{
		osFile: f,
		sb:     sb,
	}

	// Validate root group address.
	//nolint:gosec // G115: File size is always positive, safe to convert int64 to uint64
	if sb.RootGroup >= uint64(fileSize) {
		_ = f.Close()
		return nil, fmt.Errorf("root group address %d beyond file size %d",
			sb.RootGroup, fileSize)
	}

	// For all versions, sb.RootGroup now contains the correct object header address.
	file.root, err = loadGroup(file, sb.RootGroup)
	if err != nil {
		_ = f.Close()
		return nil, utils.WrapError("root group load failed", err)
	}

	// Ensure root group always has name "/" (may be empty from object header)
	file.root.name = "/"

	return file, nil
}

// isHDF5File verifies HDF5 file signature.
func isHDF5File(r utils.ReaderAt) bool {
	buf := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, 0); err != nil {
		return false
	}
	return string(buf) == core.Signature
}

// Close closes the HDF5 file and releases associated resources.
// It is safe to call Close multiple times.
func (f *File) Close() error {
	if f.osFile == nil {
		return nil // Already closed.
	}
	err := f.osFile.Close()
	f.osFile = nil // Prevent double close.
	return err
}

// Root returns the root group of the HDF5 file.
func (f *File) Root() *Group {
	return f.root
}

// Walk traverses the entire file structure, calling fn for each object.
// Objects are visited in depth-first order starting from the root group.
func (f *File) Walk(fn func(path string, obj Object)) {
	walkGroup(f.root, "/", fn)
}

func walkGroup(g *Group, currentPath string, fn func(string, Object)) {
	fn(currentPath, g)

	for _, child := range g.Children() {
		childPath := currentPath + child.Name()

		if childGroup, ok := child.(*Group); ok {
			walkGroup(childGroup, childPath+"/", fn)
		} else {
			fn(childPath, child)
		}
	}
}

// SuperblockVersion returns the HDF5 superblock format version (0, 2, or 3).
func (f *File) SuperblockVersion() uint8 {
	return f.sb.Version
}

// Superblock returns the file's superblock metadata structure.
func (f *File) Superblock() *core.Superblock {
	return f.sb
}

// Reader returns the underlying file reader for low-level access.
func (f *File) Reader() io.ReaderAt {
	return f.osFile
}

// readSignature reads 4 bytes at address and returns string.
func readSignature(r io.ReaderAt, address uint64) string {
	buf := make([]byte, 4)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(address)); err != nil {
		return ""
	}
	return string(buf)
}
