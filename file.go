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
	osFile        *os.File
	reader        io.ReaderAt // Read source: size-bounded at open, or the live writer in a write session (ADR-003).
	sb            *core.Superblock
	root          *Group
	visitedBTrees map[uint64]bool // Track visited B-tree addresses to prevent cycles
}

// Open opens an HDF5 file for reading and returns a File handle.
// The file must be a valid HDF5 file with a supported format version.
func Open(filename string) (*File, error) {
	//nolint:gosec // G304: User-provided filename is intentional for HDF5 file library
	f, err := os.Open(filename)
	if err != nil {
		return nil, utils.WrapError("file open failed", err)
	}

	// Get file size for address validation.
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, utils.WrapError("file stat failed", err)
	}
	fileSize := fi.Size()

	// Bound every internal read to the real file size — the Go counterpart of
	// the VFD's get_eof contract (ADR-003). The size is already at hand from the
	// stat above, so no second syscall is needed. A non-positive size cannot
	// describe a byte range, and the per-site checks still apply without the
	// backstop.
	sb, err := core.ReadSuperblock(f)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, core.ErrNoSignature) {
			return nil, errors.New("not an HDF5 file")
		}
		return nil, utils.WrapError("superblock read failed", err)
	}

	// Every read below is bounded by the real file size (ADR-003) and, for a
	// file with a user block, shifted by the base address: HDF5 metadata stores
	// addresses relative to the superblock position (H5FDint.c:267).
	reader := io.ReaderAt(f)
	if fileSize > 0 {
		reader = utils.NewBoundedReader(f, uint64(fileSize))
	}
	if sb.BaseAddress > 0 {
		reader = utils.NewBaseReader(reader, sb.BaseAddress)
	}

	file := &File{
		osFile:        f,
		reader:        reader,
		sb:            sb,
		visitedBTrees: make(map[uint64]bool),
	}

	// Validate the root group address. It is relative to the base address, so
	// the check compares the absolute position with the file size.
	//nolint:gosec // G115: os.FileInfo.Size is non-negative for a regular file
	if !regionInFile(sb.BaseAddress, sb.RootGroup, 1, uint64(fileSize)) {
		_ = f.Close()
		return nil, fmt.Errorf("root group address %d (base %d) beyond file size %d",
			sb.RootGroup, sb.BaseAddress, fileSize)
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

// regionInFile reports whether a region of size bytes at the relative address
// addr — that is, at absolute position base+addr — lies within a file of the
// given size. The subtraction form avoids overflowing on a corrupt address.
func regionInFile(base, addr, size, fileSize uint64) bool {
	if base > fileSize {
		return false
	}

	rel := fileSize - base

	return addr <= rel && size <= rel-addr
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

// Reader returns a read-only view of the file for low-level access.
//
// Addresses are relative to the file's base address, which is zero unless the
// file carries a user block (Superblock.BaseAddress). Reads outside the file's
// bounds return an error instead of the platform reader's short read, because
// the returned reader is bounded by the file size (ADR-003). Close still
// releases the underlying file.
func (f *File) Reader() io.ReaderAt {
	return f.reader
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
