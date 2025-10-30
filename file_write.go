package hdf5

import (
	"encoding/binary"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/writer"
)

// CreateMode specifies how to create a new HDF5 file.
type CreateMode int

const (
	// CreateTruncate creates a new file, overwriting if it exists.
	// This is the default mode, equivalent to os.Create() behavior.
	CreateTruncate CreateMode = iota

	// CreateExclusive creates a new file, failing if it already exists.
	// Useful when you want to ensure a file doesn't get accidentally overwritten.
	CreateExclusive
)

// Create creates a new HDF5 file with a minimal structure.
// The file will contain:
//   - Superblock v2 (48 bytes at offset 0)
//   - Minimal root group (empty, with Link Info message)
//
// The created file is a valid, minimal HDF5 file that can be:
//   - Reopened with Open() for reading
//   - Validated with h5dump
//   - Extended with groups, datasets, and attributes (in future versions)
//
// Parameters:
//   - filename: Path to the file to create
//   - mode: Creation mode (truncate or exclusive)
//
// Returns:
//   - *File: Handle to the created file (in read-only mode for MVP)
//   - error: If file creation or initialization fails
//
// Example:
//
//	f, err := hdf5.Create("myfile.h5", hdf5.CreateTruncate)
//	if err != nil {
//	    return err
//	}
//	defer f.Close()
//
// For MVP (v0.11.0-beta):
//   - File is created but returned in read-only mode
//   - Write operations (datasets, groups, attributes) are not yet supported
//   - The returned File can only be used for reading the structure
func Create(filename string, mode CreateMode) (*File, error) {
	// Map CreateMode to writer.CreateMode
	var writerMode writer.CreateMode
	switch mode {
	case CreateTruncate:
		writerMode = writer.ModeTruncate
	case CreateExclusive:
		writerMode = writer.ModeExclusive
	default:
		return nil, fmt.Errorf("invalid create mode: %d", mode)
	}

	// Step 1: Create FileWriter
	// Superblock v2 is 48 bytes, so initial offset for allocations is 48
	fw, err := writer.NewFileWriter(filename, writerMode, 48)
	if err != nil {
		return nil, fmt.Errorf("failed to create writer: %w", err)
	}

	// Ensure cleanup on error
	var cleanupOnError = true
	defer func() {
		if cleanupOnError {
			_ = fw.Close()
		}
	}()

	// Step 2: Create minimal root group
	rootGroupHeader := core.NewMinimalRootGroupHeader()

	// Allocate space for root group at offset 48 (after superblock)
	rootGroupAddr := uint64(48)
	rootGroupSize, err := rootGroupHeader.WriteTo(fw, rootGroupAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to write root group: %w", err)
	}

	// Step 3: Create Superblock v2
	sb := &core.Superblock{
		Version:        core.Version2,
		OffsetSize:     8,
		LengthSize:     8,
		BaseAddress:    0,
		RootGroup:      rootGroupAddr,
		Endianness:     binary.LittleEndian,
		SuperExtension: 0, // No superblock extension (will be encoded as UNDEF)
		DriverInfo:     0, // No driver info
	}

	// Calculate end-of-file address
	// EOF = superblock size + root group size
	eofAddress := uint64(48) + rootGroupSize

	// Step 4: Write superblock at offset 0
	if err := sb.WriteTo(fw, eofAddress); err != nil {
		return nil, fmt.Errorf("failed to write superblock: %w", err)
	}

	// Step 5: Flush to disk
	if err := fw.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush file: %w", err)
	}

	// Step 6: Close writer
	if err := fw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// Prevent cleanup on success
	cleanupOnError = false

	// Step 7: Reopen file in read mode
	// For MVP, we return a read-only File handle
	// Future versions will support keeping the file open for writing
	return Open(filename)
}
