package hdf5

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/writer"
)

// Datatype represents HDF5 datatype for creating datasets.
type Datatype int

const (
	// Int8 represents 8-bit signed integer type.
	Int8 Datatype = iota
	// Int16 represents 16-bit signed integer type.
	Int16
	// Int32 represents 32-bit signed integer type.
	Int32
	// Int64 represents 64-bit signed integer type.
	Int64
	// Uint8 represents 8-bit unsigned integer type.
	Uint8
	// Uint16 represents 16-bit unsigned integer type.
	Uint16
	// Uint32 represents 32-bit unsigned integer type.
	Uint32
	// Uint64 represents 64-bit unsigned integer type.
	Uint64
	// Float32 represents 32-bit floating point type.
	Float32
	// Float64 represents 64-bit floating point type.
	Float64
	// String represents fixed-length string type (use with WithStringSize option).
	String
)

// datatypeInfo contains metadata about a datatype.
type datatypeInfo struct {
	class         core.DatatypeClass
	size          uint32
	classBitField uint32
}

// getDatatypeInfo returns HDF5 datatype information for a Go datatype.
func getDatatypeInfo(dt Datatype, stringSize uint32) (*datatypeInfo, error) {
	switch dt {
	case Int8:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          1,
			classBitField: 0x00, // Little-endian, signed
		}, nil
	case Int16:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          2,
			classBitField: 0x00,
		}, nil
	case Int32:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          4,
			classBitField: 0x00,
		}, nil
	case Int64:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          8,
			classBitField: 0x00,
		}, nil
	case Uint8:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          1,
			classBitField: 0x00, // Unsigned will be handled in encoding
		}, nil
	case Uint16:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          2,
			classBitField: 0x00,
		}, nil
	case Uint32:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          4,
			classBitField: 0x00,
		}, nil
	case Uint64:
		return &datatypeInfo{
			class:         core.DatatypeFixed,
			size:          8,
			classBitField: 0x00,
		}, nil
	case Float32:
		return &datatypeInfo{
			class:         core.DatatypeFloat,
			size:          4,
			classBitField: 0x00, // Little-endian
		}, nil
	case Float64:
		return &datatypeInfo{
			class:         core.DatatypeFloat,
			size:          8,
			classBitField: 0x00,
		}, nil
	case String:
		if stringSize == 0 {
			return nil, fmt.Errorf("string datatype requires size > 0 (use WithStringSize option)")
		}
		return &datatypeInfo{
			class:         core.DatatypeString,
			size:          stringSize,
			classBitField: 0x00, // Null-terminated ASCII
		}, nil
	default:
		return nil, fmt.Errorf("unsupported datatype: %d", dt)
	}
}

// FileWriter represents an HDF5 file opened for writing.
// It wraps a File handle and provides write operations.
type FileWriter struct {
	file     *File
	writer   *writer.FileWriter
	filename string

	// Root group metadata for linking objects
	rootGroupAddr  uint64 // Address of root group object header
	rootBTreeAddr  uint64 // Address of root group B-tree
	rootHeapAddr   uint64 // Address of root group local heap
	rootStNodeAddr uint64 // Address of root group symbol table node
}

// CreateForWrite creates a new HDF5 file for writing.
// Unlike Create(), this keeps the file open in write mode.
//
// Parameters:
//   - filename: Path to the file to create
//   - mode: Creation mode (truncate or exclusive)
//
// Returns:
//   - *FileWriter: Handle for writing datasets
//   - error: If creation fails
//
// Example:
//
//	fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	if err != nil {
//	    return err
//	}
//	defer fw.Close()
//
//	// Create datasets and write data
//	ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
//	ds.Write(myData)
func CreateForWrite(filename string, mode CreateMode) (*FileWriter, error) {
	// Map CreateMode to writer.CreateMode and create basic writer
	fw, err := initializeFileWriter(filename, mode)
	if err != nil {
		return nil, err
	}

	// Ensure cleanup on error
	var cleanupOnError = true
	defer func() {
		if cleanupOnError {
			_ = fw.Close()
		}
	}()

	// Create root group with Symbol Table structure
	rootInfo, err := createRootGroupStructure(fw)
	if err != nil {
		return nil, err
	}

	// Step 3: Create Superblock v2
	sb := &core.Superblock{
		Version:        core.Version2,
		OffsetSize:     8,
		LengthSize:     8,
		BaseAddress:    0,
		RootGroup:      rootInfo.groupAddr,
		Endianness:     binary.LittleEndian,
		SuperExtension: 0,
		DriverInfo:     0,
	}

	// Calculate end-of-file address
	eofAddress := uint64(48) + rootInfo.groupSize

	// Step 4: Write superblock at offset 0
	if err := sb.WriteTo(fw, eofAddress); err != nil {
		return nil, fmt.Errorf("failed to write superblock: %w", err)
	}

	// Step 5: Flush to disk
	if err := fw.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush file: %w", err)
	}

	// Prevent cleanup - we'll return the writer
	cleanupOnError = false

	// Create FileWriter wrapper
	// Note: File object is minimal for now (will be enhanced later)
	fileObj := &File{
		sb: sb,
	}

	return &FileWriter{
		file:           fileObj,
		writer:         fw,
		filename:       filename,
		rootGroupAddr:  rootInfo.groupAddr,
		rootBTreeAddr:  rootInfo.btreeAddr,
		rootHeapAddr:   rootInfo.heapAddr,
		rootStNodeAddr: rootInfo.stNodeAddr,
	}, nil
}

// CreateDataset creates a new dataset in the HDF5 file.
// The dataset will use contiguous storage layout.
//
// Parameters:
//   - name: Dataset name (must start with "/" for root-level datasets)
//   - dtype: Data type (Int32, Float64, etc.)
//   - dims: Dimensions (e.g., []uint64{10} for 1D, []uint64{3,4} for 2D)
//
// Returns:
//   - *DatasetWriter: Handle for writing data to the dataset
//   - error: If creation fails
//
// Example:
//
//	// Create file
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create 1D dataset
//	ds, _ := fw.CreateDataset("/temperature", hdf5.Float64, []uint64{100})
//
//	// Write data
//	data := make([]float64, 100)
//	// ... fill data ...
//	ds.Write(data)
//
// Limitations for MVP (v0.11.0-beta):
//   - Only contiguous layout (no chunking)
//   - No compression
//   - Dataset must be in root group (no nested groups yet)
//   - No resizable datasets (maxDims not supported)
func (fw *FileWriter) CreateDataset(name string, dtype Datatype, dims []uint64, opts ...DatasetOption) (*DatasetWriter, error) {
	// Validate inputs
	if name == "" {
		return nil, fmt.Errorf("dataset name cannot be empty")
	}

	if len(dims) == 0 {
		return nil, fmt.Errorf("dimensions cannot be empty (use []uint64{1} for scalar)")
	}

	// For MVP, only support root-level datasets
	if name != "" && name[0] != '/' {
		return nil, fmt.Errorf("dataset name must start with '/' (got %q)", name)
	}

	// Apply options
	config := &datasetConfig{
		stringSize: 0,
	}
	for _, opt := range opts {
		opt(config)
	}

	// Get datatype info
	dtInfo, err := getDatatypeInfo(dtype, config.stringSize)
	if err != nil {
		return nil, fmt.Errorf("invalid datatype: %w", err)
	}

	// Calculate total data size
	totalElements := uint64(1)
	for _, dim := range dims {
		if dim == 0 {
			return nil, fmt.Errorf("dimension cannot be 0")
		}
		totalElements *= dim
	}
	dataSize := totalElements * uint64(dtInfo.size)

	// Allocate space for dataset data
	dataAddress, err := fw.writer.Allocate(dataSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate space for data: %w", err)
	}

	// Create datatype message
	datatypeMsg := &core.DatatypeMessage{
		Class:         dtInfo.class,
		Version:       1,
		Size:          dtInfo.size,
		ClassBitField: dtInfo.classBitField,
	}
	datatypeData, err := core.EncodeDatatypeMessage(datatypeMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode datatype: %w", err)
	}

	// Create dataspace message
	dataspaceData, err := core.EncodeDataspaceMessage(dims, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encode dataspace: %w", err)
	}

	// Create layout message
	layoutData, err := core.EncodeLayoutMessage(
		core.LayoutContiguous,
		dataSize,
		dataAddress,
		fw.file.sb,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encode layout: %w", err)
	}

	// Create object header with messages
	ohw := &core.ObjectHeaderWriter{
		Version: 2,
		Flags:   0, // Minimal flags
		Messages: []core.MessageWriter{
			{Type: core.MsgDatatype, Data: datatypeData},
			{Type: core.MsgDataspace, Data: dataspaceData},
			{Type: core.MsgDataLayout, Data: layoutData},
		},
	}

	// Allocate space for object header
	// We need to calculate size first
	headerSize, err := calculateObjectHeaderSize(ohw)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate header size: %w", err)
	}

	headerAddress, err := fw.writer.Allocate(headerSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate space for object header: %w", err)
	}

	// Write object header
	writtenSize, err := ohw.WriteTo(fw.writer, headerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to write object header: %w", err)
	}

	if writtenSize != headerSize {
		return nil, fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	// Link dataset to parent group's symbol table
	// Parse path to get parent and dataset name
	parent, datasetName := parsePath(name)
	if err := fw.linkToParent(parent, datasetName, headerAddress); err != nil {
		return nil, fmt.Errorf("failed to link dataset to parent: %w", err)
	}

	// Create DatasetWriter
	dsw := &DatasetWriter{
		fileWriter:  fw,
		name:        name,
		address:     headerAddress,
		dataAddress: dataAddress,
		dataSize:    dataSize,
		dtype:       datatypeMsg,
		dims:        dims,
	}

	return dsw, nil
}

// calculateObjectHeaderSize calculates the size of an object header before writing.
// This is needed for pre-allocation.
func calculateObjectHeaderSize(ohw *core.ObjectHeaderWriter) (uint64, error) {
	if ohw.Version != 2 {
		return 0, fmt.Errorf("only object header version 2 supported")
	}

	// Calculate message data size
	var messageDataSize uint64
	for _, msg := range ohw.Messages {
		// Each message: Type (1) + Size (2) + Flags (1) + Data (variable)
		messageDataSize += 1 + 2 + 1 + uint64(len(msg.Data))
	}

	// Validate chunk size fits in 1 byte (MVP limitation)
	if messageDataSize > 255 {
		return 0, fmt.Errorf("message data size %d exceeds 255 bytes (MVP limitation)", messageDataSize)
	}

	// Header: Signature (4) + Version (1) + Flags (1) + Chunk Size (1) + Messages
	headerSize := 4 + 1 + 1 + 1 + messageDataSize

	return headerSize, nil
}

// DatasetWriter provides write access to a dataset.
type DatasetWriter struct {
	fileWriter  *FileWriter
	name        string
	address     uint64 // Object header address
	dataAddress uint64 // Data storage address
	dataSize    uint64 // Total data size in bytes
	dtype       *core.DatatypeMessage
	dims        []uint64
}

// Write writes data to the dataset.
// The data must match the dataset's datatype and dimensions.
//
// Parameters:
//   - data: Data to write (type must match dataset datatype)
//
// Supported types:
//   - []int8, []int16, []int32, []int64
//   - []uint8, []uint16, []uint32, []uint64
//   - []float32, []float64
//   - []string (for fixed-length string datasets)
//
// For multi-dimensional datasets, data should be flattened in row-major order.
//
// Example:
//
//	// 1D dataset
//	ds, _ := fw.CreateDataset("/data", hdf5.Int32, []uint64{5})
//	ds.Write([]int32{1, 2, 3, 4, 5})
//
//	// 2D dataset (3x4 matrix)
//	ds2, _ := fw.CreateDataset("/matrix", hdf5.Float64, []uint64{3, 4})
//	// Flatten row-major: [[1,2,3,4], [5,6,7,8], [9,10,11,12]]
//	ds2.Write([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
func (dw *DatasetWriter) Write(data interface{}) error {
	// Convert data to bytes based on datatype
	var buf []byte
	var err error

	switch dw.dtype.Class {
	case core.DatatypeFixed:
		buf, err = encodeFixedPointData(data, dw.dtype.Size, dw.dataSize)
	case core.DatatypeFloat:
		buf, err = encodeFloatData(data, dw.dtype.Size, dw.dataSize)
	case core.DatatypeString:
		buf, err = encodeStringData(data, dw.dtype.Size, dw.dataSize)
	default:
		return fmt.Errorf("unsupported datatype class for writing: %d", dw.dtype.Class)
	}

	if err != nil {
		return fmt.Errorf("failed to encode data: %w", err)
	}

	// Verify size matches
	if uint64(len(buf)) != dw.dataSize {
		return fmt.Errorf("data size mismatch: expected %d bytes, got %d bytes", dw.dataSize, len(buf))
	}

	// Write data to file
	if err := dw.fileWriter.writer.WriteAtAddress(buf, dw.dataAddress); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// encodeFixedPointData encodes integer data to bytes.
func encodeFixedPointData(data interface{}, elemSize uint32, expectedSize uint64) ([]byte, error) {
	// Validate data size matches expected size
	dataLen, err := getIntegerSliceLength(data)
	if err != nil {
		return nil, err
	}

	actualSize := uint64(dataLen) * uint64(elemSize) //nolint:gosec // Safe: dataLen from slice length always fits in uint64
	if actualSize != expectedSize {
		return nil, fmt.Errorf("data size mismatch: expected %d bytes, got %d bytes", expectedSize, actualSize)
	}

	buf := make([]byte, expectedSize)

	switch elemSize {
	case 1:
		return encode1ByteIntegers(data, buf)
	case 2:
		return encode2ByteIntegers(data, buf)
	case 4:
		return encode4ByteIntegers(data, buf)
	case 8:
		return encode8ByteIntegers(data, buf)
	default:
		return nil, fmt.Errorf("unsupported integer size: %d", elemSize)
	}
}

// getIntegerSliceLength returns the length of integer slice or error if type is unsupported.
func getIntegerSliceLength(data interface{}) (int, error) {
	switch v := data.(type) {
	case []int8:
		return len(v), nil
	case []uint8:
		return len(v), nil
	case []int16:
		return len(v), nil
	case []uint16:
		return len(v), nil
	case []int32:
		return len(v), nil
	case []uint32:
		return len(v), nil
	case []int64:
		return len(v), nil
	case []uint64:
		return len(v), nil
	default:
		return 0, fmt.Errorf("unsupported data type: %T", data)
	}
}

// encode1ByteIntegers encodes []int8 or []uint8 to buffer.
func encode1ByteIntegers(data interface{}, buf []byte) ([]byte, error) {
	switch v := data.(type) {
	case []int8:
		for i, val := range v {
			buf[i] = byte(val)
		}
	case []uint8:
		copy(buf, v)
	default:
		return nil, fmt.Errorf("expected []int8 or []uint8, got %T", data)
	}
	return buf, nil
}

// encode2ByteIntegers encodes []int16 or []uint16 to buffer.
func encode2ByteIntegers(data interface{}, buf []byte) ([]byte, error) {
	switch v := data.(type) {
	case []int16:
		for i, val := range v {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(val)) //nolint:gosec // Safe: int16 always fits in uint16
		}
	case []uint16:
		for i, val := range v {
			binary.LittleEndian.PutUint16(buf[i*2:], val)
		}
	default:
		return nil, fmt.Errorf("expected []int16 or []uint16, got %T", data)
	}
	return buf, nil
}

// encode4ByteIntegers encodes []int32 or []uint32 to buffer.
func encode4ByteIntegers(data interface{}, buf []byte) ([]byte, error) {
	switch v := data.(type) {
	case []int32:
		for i, val := range v {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(val)) //nolint:gosec // Safe: int32 always fits in uint32
		}
	case []uint32:
		for i, val := range v {
			binary.LittleEndian.PutUint32(buf[i*4:], val)
		}
	default:
		return nil, fmt.Errorf("expected []int32 or []uint32, got %T", data)
	}
	return buf, nil
}

// encode8ByteIntegers encodes []int64 or []uint64 to buffer.
func encode8ByteIntegers(data interface{}, buf []byte) ([]byte, error) {
	switch v := data.(type) {
	case []int64:
		for i, val := range v {
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(val)) //nolint:gosec // Safe: int64 always fits in uint64
		}
	case []uint64:
		for i, val := range v {
			binary.LittleEndian.PutUint64(buf[i*8:], val)
		}
	default:
		return nil, fmt.Errorf("expected []int64 or []uint64, got %T", data)
	}
	return buf, nil
}

// encodeFloatData encodes floating-point data to bytes.
func encodeFloatData(data interface{}, elemSize uint32, expectedSize uint64) ([]byte, error) {
	// Validate data size
	var dataLen int
	switch v := data.(type) {
	case []float32:
		dataLen = len(v)
	case []float64:
		dataLen = len(v)
	default:
		return nil, fmt.Errorf("expected []float32 or []float64, got %T", data)
	}

	actualSize := uint64(dataLen) * uint64(elemSize)
	if actualSize != expectedSize {
		return nil, fmt.Errorf("data size mismatch: expected %d bytes, got %d bytes", expectedSize, actualSize)
	}

	buf := make([]byte, expectedSize)

	switch elemSize {
	case 4:
		// float32
		v, ok := data.([]float32)
		if !ok {
			return nil, fmt.Errorf("expected []float32, got %T", data)
		}
		for i, val := range v {
			bits := binary.LittleEndian.Uint32((*(*[4]byte)(unsafe.Pointer(&val)))[:])
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}

	case 8:
		// float64
		v, ok := data.([]float64)
		if !ok {
			return nil, fmt.Errorf("expected []float64, got %T", data)
		}
		for i, val := range v {
			bits := binary.LittleEndian.Uint64((*(*[8]byte)(unsafe.Pointer(&val)))[:])
			binary.LittleEndian.PutUint64(buf[i*8:], bits)
		}

	default:
		return nil, fmt.Errorf("unsupported float size: %d", elemSize)
	}

	return buf, nil
}

// encodeStringData encodes string data to bytes (fixed-length).
func encodeStringData(data interface{}, elemSize uint32, expectedSize uint64) ([]byte, error) {
	v, ok := data.([]string)
	if !ok {
		return nil, fmt.Errorf("expected []string, got %T", data)
	}

	// Validate size
	actualSize := uint64(len(v)) * uint64(elemSize)
	if actualSize != expectedSize {
		return nil, fmt.Errorf("data size mismatch: expected %d bytes (%d strings), got %d bytes (%d strings)",
			expectedSize, expectedSize/uint64(elemSize), actualSize, len(v))
	}

	buf := make([]byte, expectedSize)
	offset := 0

	for _, str := range v {
		// Copy string, null-terminate or truncate
		strBytes := []byte(str)
		if len(strBytes) >= int(elemSize) {
			// Truncate if too long
			copy(buf[offset:offset+int(elemSize)], strBytes[:elemSize])
		} else {
			// Copy and null-terminate
			copy(buf[offset:], strBytes)
			// Remaining bytes are already zero (null-terminated)
		}
		offset += int(elemSize)
	}

	return buf, nil
}

// Close closes the dataset writer.
// For MVP, this is a no-op (no per-dataset resources to release).
func (dw *DatasetWriter) Close() error {
	// No resources to release for MVP
	return nil
}

// DatasetOption is a functional option for customizing dataset creation.
type DatasetOption func(*datasetConfig)

// datasetConfig holds dataset creation options.
type datasetConfig struct {
	stringSize uint32
}

// WithStringSize sets the fixed string size for String datasets.
// This is required when creating a String dataset.
//
// Example:
//
//	ds, _ := fw.CreateDataset("/names", hdf5.String, []uint64{10}, hdf5.WithStringSize(32))
func WithStringSize(size uint32) DatasetOption {
	return func(cfg *datasetConfig) {
		cfg.stringSize = size
	}
}

// Close closes the file writer and flushes all data to disk.
func (fw *FileWriter) Close() error {
	if fw.writer == nil {
		return nil
	}

	// Flush buffered writes
	if err := fw.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Close writer
	if err := fw.writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	fw.writer = nil
	return nil
}

// initializeFileWriter creates and initializes a new FileWriter with the given mode.
func initializeFileWriter(filename string, mode CreateMode) (*writer.FileWriter, error) {
	var writerMode writer.CreateMode
	switch mode {
	case CreateTruncate:
		writerMode = writer.ModeTruncate
	case CreateExclusive:
		writerMode = writer.ModeExclusive
	default:
		return nil, fmt.Errorf("invalid create mode: %d", mode)
	}

	// Superblock v2 is 48 bytes
	fw, err := writer.NewFileWriter(filename, writerMode, 48)
	if err != nil {
		return nil, fmt.Errorf("failed to create writer: %w", err)
	}

	return fw, nil
}

// rootGroupInfo contains information about the created root group structure.
type rootGroupInfo struct {
	groupAddr  uint64 // Root group object header address
	groupSize  uint64 // Root group object header size
	btreeAddr  uint64 // B-tree address
	heapAddr   uint64 // Local heap address
	stNodeAddr uint64 // Symbol table node address
}

// createRootGroupStructure creates the root group with Symbol Table structure.
// Returns information about the created root group structure.
func createRootGroupStructure(fw *writer.FileWriter) (*rootGroupInfo, error) {
	const offsetSize = 8
	const lengthSize = 8

	// Create local heap for root group names
	rootHeap := structures.NewLocalHeap(256) // Initial capacity for ~10-20 names
	rootHeapAddr, err := fw.Allocate(rootHeap.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to allocate root heap: %w", err)
	}

	// Create and write symbol table node
	rootStNodeAddr, err := createSymbolTableNode(fw, offsetSize)
	if err != nil {
		return nil, err
	}

	// Create and write B-tree
	rootBTreeAddr, err := createBTreeNode(fw, rootStNodeAddr, offsetSize)
	if err != nil {
		return nil, err
	}

	// Write local heap
	if err := rootHeap.WriteTo(fw, rootHeapAddr); err != nil {
		return nil, fmt.Errorf("failed to write root heap: %w", err)
	}

	// Create and write root group object header
	rootGroupAddr, rootGroupSize, err := writeRootGroupHeader(fw, rootBTreeAddr, rootHeapAddr, offsetSize, lengthSize)
	if err != nil {
		return nil, err
	}

	return &rootGroupInfo{
		groupAddr:  rootGroupAddr,
		groupSize:  rootGroupSize,
		btreeAddr:  rootBTreeAddr,
		heapAddr:   rootHeapAddr,
		stNodeAddr: rootStNodeAddr,
	}, nil
}

// createSymbolTableNode creates and writes a symbol table node for a group.
// Returns the address where the node was written.
func createSymbolTableNode(fw *writer.FileWriter, offsetSize int) (uint64, error) {
	rootStNode := structures.NewSymbolTableNode(32) // Standard capacity (2*K where K=16)

	// Calculate symbol table node size
	// Format: 8-byte header + 32 * entrySize
	// entrySize = 2*offsetSize + 4 + 4 + 16 = 2*8 + 24 = 40 bytes
	entrySize := 2*offsetSize + 4 + 4 + 16
	stNodeSize := uint64(8 + 32*entrySize) //nolint:gosec // Safe: constant calculation always fits in uint64

	rootStNodeAddr, err := fw.Allocate(stNodeSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate root symbol table node: %w", err)
	}

	// Write symbol table node (empty initially)
	if err := rootStNode.WriteAt(fw, rootStNodeAddr, uint8(offsetSize), 32, binary.LittleEndian); err != nil { //nolint:gosec // Safe: offsetSize validated to be 8
		return 0, fmt.Errorf("failed to write root symbol table node: %w", err)
	}

	return rootStNodeAddr, nil
}

// createBTreeNode creates and writes a B-tree node for a group.
// Returns the address where the node was written.
func createBTreeNode(fw *writer.FileWriter, stNodeAddr uint64, offsetSize int) (uint64, error) {
	rootBTree := structures.NewBTreeNodeV1(0, 16) // Type 0 = group symbol table, K=16

	// Add symbol table node address as child (with key 0 for empty group)
	if err := rootBTree.AddKey(0, stNodeAddr); err != nil {
		return 0, fmt.Errorf("failed to add root B-tree key: %w", err)
	}

	// Calculate B-tree size
	// Header: 4 (sig) + 1 (type) + 1 (level) + 2 (entries) + 2*8 (siblings) = 24 bytes
	// Keys: (2K+1) * offsetSize = 33 * 8 = 264 bytes
	// Children: 2K * offsetSize = 32 * 8 = 256 bytes
	btreeSize := uint64(24 + (2*16+1)*offsetSize + 2*16*offsetSize) //nolint:gosec // Safe: constant calculation always fits in uint64

	rootBTreeAddr, err := fw.Allocate(btreeSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate root B-tree: %w", err)
	}

	// Write B-tree
	if err := rootBTree.WriteAt(fw, rootBTreeAddr, uint8(offsetSize), 16, binary.LittleEndian); err != nil { //nolint:gosec // Safe: offsetSize validated to be 8
		return 0, fmt.Errorf("failed to write root B-tree: %w", err)
	}

	return rootBTreeAddr, nil
}

// writeRootGroupHeader creates and writes the root group object header.
// Returns the address where the header was written and its size.
func writeRootGroupHeader(fw *writer.FileWriter, btreeAddr, heapAddr uint64, offsetSize, lengthSize int) (uint64, uint64, error) {
	stMsg := core.EncodeSymbolTableMessage(btreeAddr, heapAddr, offsetSize, lengthSize)

	rootGroupHeader := &core.ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgSymbolTable, Data: stMsg},
		},
	}

	// Calculate root group object header size
	rootGroupSize := rootGroupHeader.Size()

	// Allocate space for root group object header
	rootGroupAddr, err := fw.Allocate(rootGroupSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to allocate root group header: %w", err)
	}

	// Write root group object header
	writtenSize, err := rootGroupHeader.WriteTo(fw, rootGroupAddr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to write root group header: %w", err)
	}

	if writtenSize != rootGroupSize {
		return 0, 0, fmt.Errorf("root group size mismatch: expected %d, wrote %d", rootGroupSize, writtenSize)
	}

	return rootGroupAddr, rootGroupSize, nil
}
