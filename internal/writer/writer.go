package writer

import (
	"fmt"
	"io"
	"os"
)

// FileWriter wraps an os.File for writing HDF5 files.
// It provides:
// - Space allocation tracking (via Allocator)
// - Write-at-address operations
// - End-of-file tracking
// - Flush control
//
// Thread-safety: Not thread-safe. Caller must synchronize access.
type FileWriter struct {
	file      *os.File   // Underlying OS file
	allocator *Allocator // Space allocation tracker
}

// CreateMode specifies the file creation behavior.
type CreateMode int

const (
	// ModeTruncate creates a new file, truncating if it exists.
	// Equivalent to os.Create() behavior.
	ModeTruncate CreateMode = iota

	// ModeExclusive creates a new file, fails if it exists.
	// Equivalent to os.O_CREATE | os.O_EXCL.
	ModeExclusive
)

// NewFileWriter creates a writer for a new HDF5 file.
// The file is opened for reading and writing.
//
// Parameters:
//   - filename: Path to file to create
//   - mode: Creation mode (truncate or exclusive)
//   - initialOffset: Starting address for allocations (typically superblock size)
//
// For HDF5 files:
//   - Superblock v2 is 48 bytes, so initialOffset would be 48
//   - The superblock itself at offset 0 is not tracked by the allocator
//
// Returns:
//   - FileWriter ready for use
//   - Error if file creation fails
func NewFileWriter(filename string, mode CreateMode, initialOffset uint64) (*FileWriter, error) {
	var osFile *os.File
	var err error

	switch mode {
	case ModeTruncate:
		// Create or truncate file, read-write mode
		osFile, err = os.Create(filename)

	case ModeExclusive:
		// Create new file, fail if exists
		osFile, err = os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)

	default:
		return nil, fmt.Errorf("invalid create mode: %d", mode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return &FileWriter{
		file:      osFile,
		allocator: NewAllocator(initialOffset),
	}, nil
}

// Allocate reserves a block of space in the file.
// Returns the address where the block was allocated.
// The space is not zeroed - caller must write data to the allocated block.
//
// For MVP:
// - Allocation always occurs at end of file
// - No alignment requirements
//
// Example:
//
//	addr, err := writer.Allocate(1024)
//	if err != nil {
//	    return err
//	}
//	// Now write data at addr
//	err = writer.WriteAt(data, addr)
func (w *FileWriter) Allocate(size uint64) (uint64, error) {
	if w.file == nil {
		return 0, fmt.Errorf("writer is closed")
	}

	return w.allocator.Allocate(size)
}

// WriteAt writes data at a specific address in the file.
// Implements io.WriterAt interface.
//
// The address should typically be obtained from Allocate().
//
// Note: This does not automatically track the write as an allocation.
// For metadata tracking, use Allocate() first, then WriteAt().
//
// Example:
//
//	addr, _ := writer.Allocate(uint64(len(data)))
//	_, err := writer.WriteAt(data, int64(addr))
func (w *FileWriter) WriteAt(data []byte, offset int64) (int, error) {
	if w.file == nil {
		return 0, fmt.Errorf("writer is closed")
	}

	if len(data) == 0 {
		return 0, nil // Nothing to write
	}

	// Use os.File.WriteAt which handles seeking internally
	n, err := w.file.WriteAt(data, offset)
	if err != nil {
		return n, fmt.Errorf("write at address %d failed: %w", offset, err)
	}

	if n != len(data) {
		return n, fmt.Errorf("incomplete write at address %d: wrote %d of %d bytes", offset, n, len(data))
	}

	return n, nil
}

// WriteAtAddress writes data at a specific address (convenience method with uint64 address).
func (w *FileWriter) WriteAtAddress(data []byte, addr uint64) error {
	_, err := w.WriteAt(data, int64(addr))
	return err
}

// ReadAt reads data at a specific address.
// Useful for reading back metadata immediately after writing.
// Implements io.ReaderAt interface for compatibility.
func (w *FileWriter) ReadAt(buf []byte, addr int64) (int, error) {
	if w.file == nil {
		return 0, fmt.Errorf("writer is closed")
	}

	return w.file.ReadAt(buf, addr)
}

// EndOfFile returns the current end-of-file address.
// This is where the next allocation would occur.
func (w *FileWriter) EndOfFile() uint64 {
	return w.allocator.EndOfFile()
}

// Flush ensures all writes are committed to disk.
// This should be called before closing or when data durability is required.
func (w *FileWriter) Flush() error {
	if w.file == nil {
		return fmt.Errorf("writer is closed")
	}

	return w.file.Sync()
}

// Close closes the underlying file.
// This does NOT automatically flush - call Flush() first if needed.
// After Close(), the writer cannot be used.
func (w *FileWriter) Close() error {
	if w.file == nil {
		return nil // Already closed
	}

	err := w.file.Close()
	w.file = nil
	return err
}

// File returns the underlying *os.File.
// Use with caution - direct file operations may break allocation tracking.
// Primarily for reading operations or advanced use cases.
func (w *FileWriter) File() *os.File {
	return w.file
}

// Allocator returns the space allocator.
// Useful for debugging and testing allocation patterns.
func (w *FileWriter) Allocator() *Allocator {
	return w.allocator
}

// WriteAtWithAllocation is a convenience method that allocates space and writes data.
// Returns the address where data was written.
//
// This is equivalent to:
//
//	addr, err := writer.Allocate(uint64(len(data)))
//	if err != nil { return 0, err }
//	_, err = writer.WriteAt(data, int64(addr))
//	return addr, err
func (w *FileWriter) WriteAtWithAllocation(data []byte) (uint64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("cannot write empty data")
	}

	addr, err := w.Allocate(uint64(len(data)))
	if err != nil {
		return 0, err
	}

	if err := w.WriteAtAddress(data, addr); err != nil {
		return 0, err
	}

	return addr, nil
}

// Seek implements io.Seeker interface for compatibility.
// Note: HDF5 uses absolute addressing, so seeking is rarely needed.
func (w *FileWriter) Seek(offset int64, whence int) (int64, error) {
	if w.file == nil {
		return 0, fmt.Errorf("writer is closed")
	}

	return w.file.Seek(offset, whence)
}

// Ensure FileWriter implements io.ReaderAt and io.WriterAt
var (
	_ io.ReaderAt = (*FileWriter)(nil)
	_ io.WriterAt = (*FileWriter)(nil)
)
