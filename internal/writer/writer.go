package writer

import (
	"fmt"
	"io"
	"math"
	"os"

	"github.com/scigolib/hdf5/internal/utils"
)

// FileWriter wraps an os.File for writing HDF5 files.
// It provides:
// - Space allocation tracking (via Allocator)
// - Write-at-address operations
// - End-of-file tracking
// - Flush control
//
// Addresses are relative to the file's base address: the size of its user
// block, 0 when it has none. The writer adds the base before every I/O call,
// as the HDF5 VFD does (H5FDint.c:267, "addr + file->base_addr"), so the
// allocator, the encoders and every caller work in HDF5 addresses only. The
// bytes before the base belong to the application (WriteUserBlock).
//
// Thread-safety: Not thread-safe. Caller must synchronize access.
type FileWriter struct {
	file      *os.File   // Underlying OS file
	allocator *Allocator // Space allocation tracker (relative addresses)
	base      uint64     // Base address: the user block size
}

// ValidUserBlockSize reports whether size is a user block size the C library
// accepts: 0, or a power of two of at least 512 (H5Pset_userblock,
// H5Pfcpl.c:321-329).
func ValidUserBlockSize(size uint64) bool {
	return size == 0 || (size >= 512 && size&(size-1) == 0)
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
	return NewFileWriterWithUserBlock(filename, mode, initialOffset, 0)
}

// NewFileWriterWithUserBlock is NewFileWriter for a file with a user block of
// userBlock bytes (ValidUserBlockSize): the block is written as zeros at the
// start of the file, and every address after it is relative to its end, as
// the C library sets the base address to the user block size
// (H5Fsuper.c:1194-1204). initialOffset stays relative.
func NewFileWriterWithUserBlock(filename string, mode CreateMode, initialOffset, userBlock uint64) (*FileWriter, error) {
	if !ValidUserBlockSize(userBlock) {
		return nil, fmt.Errorf("user block size %d: must be 0 or a power of two of at least 512", userBlock)
	}
	if userBlock > math.MaxInt64 {
		return nil, fmt.Errorf("user block size %d: past the largest file offset", userBlock)
	}
	w, err := newFileWriter(filename, mode, initialOffset)
	if err != nil {
		return nil, err
	}
	if userBlock == 0 {
		return w, nil
	}
	// The file is new and empty: extending it to the block size reads back
	// as zeros without holding the block in memory (C only reserves the
	// space, H5Fsuper.c:1197-1199).
	if err := w.file.Truncate(int64(userBlock)); err != nil {
		closeErr := w.Close()
		return nil, fmt.Errorf("reserve the user block of %d bytes: %w", userBlock, joinClose(err, closeErr))
	}
	w.base = userBlock
	return w, nil
}

// joinClose returns err, with closeErr appended when closing failed too.
func joinClose(err, closeErr error) error {
	if closeErr == nil {
		return err
	}
	return fmt.Errorf("%w (close: %w)", err, closeErr)
}

// newFileWriter creates the file of NewFileWriter.
func newFileWriter(filename string, mode CreateMode, initialOffset uint64) (*FileWriter, error) {
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
	return OpenFileWriterWithBase(filename, mode, initialOffset, 0)
}

// OpenFileWriterWithBase is OpenFileWriter for a file whose base address is
// base (its superblock's position, the user block size): addresses are
// relative to it, so the allocator starts at the file size minus the base.
// initialOffset is relative.
func OpenFileWriterWithBase(filename string, mode CreateMode, initialOffset, base uint64) (*FileWriter, error) {
	w, err := openFileWriter(filename, mode, initialOffset+base)
	if err != nil {
		return nil, err
	}
	if base == 0 {
		return w, nil
	}
	end := w.allocator.EndOfFile()
	if end < base {
		closeErr := w.Close()
		return nil, joinClose(fmt.Errorf("file of %d bytes ends before its base address %d", end, base), closeErr)
	}
	w.base = base
	w.allocator = NewAllocator(end - base)
	return w, nil
}

// openFileWriter opens the file of OpenFileWriter; initialOffset is absolute.
func openFileWriter(filename string, mode CreateMode, initialOffset uint64) (*FileWriter, error) {
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

// TryExtend grows the block [addr, addr+size) in place by extra bytes when the
// space after it is free, as H5MF_try_extend does (H5MF.c:1250). It reports
// whether the block was extended; a closed writer never extends.
func (w *FileWriter) TryExtend(addr, size, extra uint64) bool {
	if w.file == nil {
		return false
	}
	return w.allocator.TryExtend(addr, size, extra)
}

// Free returns the block [addr, addr+size) to the allocator, as H5MF_xfree
// does (H5MF.c).
func (w *FileWriter) Free(addr, size uint64) error {
	if w.file == nil {
		return fmt.Errorf("writer is closed")
	}
	return w.allocator.Free(addr, size)
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

	abs, err := w.absolute(offset)
	if err != nil {
		return 0, fmt.Errorf("write at address %d: %w", offset, err)
	}

	// Use os.File.WriteAt which handles seeking internally
	n, err := w.file.WriteAt(data, abs)
	if err != nil {
		return n, fmt.Errorf("write at address %d failed: %w", offset, err)
	}

	if n != len(data) {
		return n, fmt.Errorf("incomplete write at address %d: wrote %d of %d bytes", offset, n, len(data))
	}

	return n, nil
}

// absolute returns the file offset of the relative address offset: offset
// plus the base, refusing a negative offset and a sum past the int64 range.
func (w *FileWriter) absolute(offset int64) (int64, error) {
	if offset < 0 {
		return 0, fmt.Errorf("negative address %d", offset)
	}
	if w.base > math.MaxInt64 || uint64(offset) > math.MaxInt64-w.base {
		return 0, fmt.Errorf("address %d with base %d overflows the file offset range", offset, w.base)
	}
	return offset + int64(w.base), nil
}

// Base returns the file's base address: the size of its user block, 0 when
// it has none.
func (w *FileWriter) Base() uint64 {
	return w.base
}

// WriteUserBlock writes data at the start of the user block, the bytes
// before the base address that HDF5 leaves to the application. It refuses a
// file without a user block and data longer than the block.
func (w *FileWriter) WriteUserBlock(data []byte) error {
	if w.file == nil {
		return fmt.Errorf("writer is closed")
	}
	if w.base == 0 {
		return fmt.Errorf("the file has no user block")
	}
	if uint64(len(data)) > w.base {
		return fmt.Errorf("%d bytes do not fit the user block of %d bytes", len(data), w.base)
	}
	n, err := w.file.WriteAt(data, 0)
	if err != nil {
		return fmt.Errorf("write user block: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("incomplete user block write: wrote %d of %d bytes", n, len(data))
	}
	return nil
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
	abs, err := w.absolute(addr)
	if err != nil {
		return 0, fmt.Errorf("read at address %d: %w", addr, err)
	}

	return w.file.ReadAt(buf, abs)
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
// Its offsets are absolute: they include the base address (Base).
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
//
// Its addresses are relative to the base address, as the read side's are
// (utils.NewBaseReader); with no user block it is the file itself.
func (w *FileWriter) Reader() io.ReaderAt {
	if w.base == 0 {
		return w.file
	}
	return utils.NewBaseReader(w.file, w.base)
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
// Its offsets are absolute file offsets, like File's: they include the base
// address. HDF5 addresses go through WriteAt and ReadAt.
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
