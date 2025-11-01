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

// CreateMode specifies the file creation/opening behavior.
type CreateMode int

const (
	// ModeTruncate creates a new file, truncating if it exists.
	// Equivalent to os.Create() behavior.
	ModeTruncate CreateMode = iota

	// ModeExclusive creates a new file, fails if it exists.
	// Equivalent to os.O_CREATE | os.O_EXCL.
	ModeExclusive

	// ModeReadWrite opens an existing file for reading and writing.
	// Used for read-modify-write operations on existing HDF5 files.
	ModeReadWrite

	// ModeReadOnly opens an existing file for reading only.
	// Used when opening files without modification intent.
	ModeReadOnly
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
		osFile, err = os.Create(filename) //nolint:gosec // User-provided filename for HDF5 file creation

	case ModeExclusive:
		// Create new file, fail if exists
		osFile, err = os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // User-provided filename for HDF5 file creation

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

// OpenFileWriter opens an existing HDF5 file for read-modify-write operations.
// Unlike NewFileWriter which creates a new file, this opens an existing file.
//
// Parameters:
//   - filename: Path to existing HDF5 file
//   - mode: Open mode (ModeReadWrite or ModeReadOnly)
//   - initialOffset: Current end-of-file offset (for allocation tracking)
//
// For existing files:
//   - initialOffset should be set to the current file size
//   - New allocations will occur after existing data
//   - Allocator tracks next free address
//
// Returns:
//   - FileWriter ready for RMW operations
//   - Error if file doesn't exist or open fails
//
// Example:
//
//	// Open existing file for modification
//	fw, err := OpenFileWriter("data.h5", ModeReadWrite, existingFileSize)
//	if err != nil {
//	    return err
//	}
//	defer fw.Close()
//
//	// Now you can allocate new space and write data
//	addr, _ := fw.Allocate(1024)
//	fw.WriteAt(newData, int64(addr))
func OpenFileWriter(filename string, mode CreateMode, initialOffset uint64) (*FileWriter, error) {
	var osFile *os.File
	var err error

	switch mode {
	case ModeReadWrite:
		// Open existing file for reading and writing
		osFile, err = os.OpenFile(filename, os.O_RDWR, 0o666) //nolint:gosec // User-provided filename for HDF5 file modification

	case ModeReadOnly:
		// Open existing file for reading only
		osFile, err = os.OpenFile(filename, os.O_RDONLY, 0o666) //nolint:gosec // User-provided filename for HDF5 file reading

	default:
		return nil, fmt.Errorf("invalid open mode: %d (use ModeReadWrite or ModeReadOnly)", mode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	// Get actual file size to validate initialOffset
	stat, err := osFile.Stat()
	if err != nil {
		_ = osFile.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := uint64(stat.Size()) //nolint:gosec // Safe: file size conversion

	// Initialize allocator at file size (new allocations happen after existing data)
	allocatorOffset := fileSize
	if initialOffset > fileSize {
		// Caller provided larger offset than file size - trust it (for sparse files)
		allocatorOffset = initialOffset
	}

	return &FileWriter{
		file:      osFile,
		allocator: NewAllocator(allocatorOffset),
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
	_, err := w.WriteAt(data, int64(addr)) //nolint:gosec // Safe: address within file bounds
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

// Reader returns an io.ReaderAt interface for reading from the file.
// This is the preferred method for reading operations as it returns an interface
// rather than a concrete type, improving testability and following Go best practices.
//
// Use this for:
//   - Reading back written data
//   - Object header modifications
//   - Integration tests (can be mocked)
//
// Example:
//
//	reader := fw.Reader()
//	oh, err := core.ReadObjectHeader(reader, addr, sb)
func (w *FileWriter) Reader() io.ReaderAt {
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

// Ensure FileWriter implements io.ReaderAt and io.WriterAt.
var (
	_ io.ReaderAt = (*FileWriter)(nil)
	_ io.WriterAt = (*FileWriter)(nil)
)
