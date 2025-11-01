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

	// Array datatypes - fixed-size homogeneous collections.
	// Use with WithArrayDims option to specify dimensions.
	// Example: [3]int32 = ArrayInt32 with dims=[3].

	// ArrayInt8 represents array of 8-bit signed integers.
	ArrayInt8 Datatype = 100 + iota
	// ArrayInt16 represents array of 16-bit signed integers.
	ArrayInt16
	// ArrayInt32 represents array of 32-bit signed integers.
	ArrayInt32
	// ArrayInt64 represents array of 64-bit signed integers.
	ArrayInt64
	// ArrayUint8 represents array of 8-bit unsigned integers.
	ArrayUint8
	// ArrayUint16 represents array of 16-bit unsigned integers.
	ArrayUint16
	// ArrayUint32 represents array of 32-bit unsigned integers.
	ArrayUint32
	// ArrayUint64 represents array of 64-bit unsigned integers.
	ArrayUint64
	// ArrayFloat32 represents array of 32-bit floating point values.
	ArrayFloat32
	// ArrayFloat64 represents array of 64-bit floating point values.
	ArrayFloat64

	// Enum datatypes - named integer constants.
	// Use with WithEnumValues option to specify name-value mappings.

	// EnumInt8 represents enumeration based on 8-bit signed integer.
	EnumInt8 Datatype = 200 + iota
	// EnumInt16 represents enumeration based on 16-bit signed integer.
	EnumInt16
	// EnumInt32 represents enumeration based on 32-bit signed integer.
	EnumInt32
	// EnumInt64 represents enumeration based on 64-bit signed integer.
	EnumInt64
	// EnumUint8 represents enumeration based on 8-bit unsigned integer.
	EnumUint8
	// EnumUint16 represents enumeration based on 16-bit unsigned integer.
	EnumUint16
	// EnumUint32 represents enumeration based on 32-bit unsigned integer.
	EnumUint32
	// EnumUint64 represents enumeration based on 64-bit unsigned integer.
	EnumUint64

	// Reference datatypes - point to objects or dataset regions.

	// ObjectReference represents reference to an object (group/dataset).
	// Value type: ObjectRef (uint64 - 8-byte object address).
	ObjectReference Datatype = 300

	// RegionReference represents reference to a dataset region.
	// Value type: RegionRef ([12]byte - 8-byte object addr + 4-byte region info).
	RegionReference Datatype = 301

	// Opaque datatype - uninterpreted byte sequences with descriptive tag.
	// Use with WithOpaqueTag option to specify tag and size.

	// Opaque represents opaque datatype (uninterpreted bytes with tag).
	// Example: JPEG image, binary blob, etc.
	Opaque Datatype = 400
)

// datatypeInfo contains metadata about a datatype.
type datatypeInfo struct {
	class         core.DatatypeClass
	size          uint32
	classBitField uint32
	// For advanced datatypes
	baseType   *datatypeInfo // Base type for arrays, enums
	arrayDims  []uint64      // Array dimensions
	enumNames  []string      // Enum names
	enumValues []int64       // Enum values
	opaqueTag  string        // Opaque tag
}

// datatypeHandler is the interface for handling different HDF5 datatypes.
// This follows the Go-idiomatic registry pattern used in stdlib (encoding/json, database/sql).
type datatypeHandler interface {
	// GetInfo returns datatype metadata for the given configuration.
	GetInfo(config *datasetConfig) (*datatypeInfo, error)

	// EncodeDatatypeMessage encodes the HDF5 datatype message bytes.
	EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error)
}

// basicTypeHandler handles simple fixed-point and float datatypes.
type basicTypeHandler struct {
	class         core.DatatypeClass
	size          uint32
	classBitField uint32
}

func (h *basicTypeHandler) GetInfo(_ *datasetConfig) (*datatypeInfo, error) {
	return &datatypeInfo{
		class:         h.class,
		size:          h.size,
		classBitField: h.classBitField,
	}, nil
}

func (h *basicTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	msg := &core.DatatypeMessage{
		Class:         info.class,
		Version:       1,
		Size:          info.size,
		ClassBitField: info.classBitField,
	}
	return core.EncodeDatatypeMessage(msg)
}

// stringTypeHandler handles fixed-length string datatypes.
type stringTypeHandler struct{}

func (h *stringTypeHandler) GetInfo(config *datasetConfig) (*datatypeInfo, error) {
	if config.stringSize == 0 {
		return nil, fmt.Errorf("string datatype requires size > 0 (use WithStringSize option)")
	}
	return &datatypeInfo{
		class:         core.DatatypeString,
		size:          config.stringSize,
		classBitField: 0x00,
	}, nil
}

func (h *stringTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	msg := &core.DatatypeMessage{
		Class:         info.class,
		Version:       1,
		Size:          info.size,
		ClassBitField: info.classBitField,
	}
	return core.EncodeDatatypeMessage(msg)
}

// arrayTypeHandler handles array datatypes (fixed-size collections of base types).
type arrayTypeHandler struct {
	baseType Datatype
}

func (h *arrayTypeHandler) GetInfo(config *datasetConfig) (*datatypeInfo, error) {
	if len(config.arrayDims) == 0 {
		return nil, fmt.Errorf("array datatype requires dimensions (use WithArrayDims option)")
	}

	// Get base type handler and info
	baseHandler, ok := datatypeRegistry[h.baseType]
	if !ok {
		return nil, fmt.Errorf("invalid array base type: %d", h.baseType)
	}

	baseInfo, err := baseHandler.GetInfo(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get array base type: %w", err)
	}

	// Calculate total array size (product of all dimensions * element size)
	arraySize := uint32(1)
	for _, dim := range config.arrayDims {
		arraySize *= uint32(dim) //nolint:gosec // Safe: dimension size limited
	}
	arraySize *= baseInfo.size

	return &datatypeInfo{
		class:     core.DatatypeArray,
		size:      arraySize,
		baseType:  baseInfo,
		arrayDims: config.arrayDims,
	}, nil
}

func (h *arrayTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	// Encode base type message first
	baseMsg := &core.DatatypeMessage{
		Class:         info.baseType.class,
		Version:       1,
		Size:          info.baseType.size,
		ClassBitField: info.baseType.classBitField,
	}
	baseData, err := core.EncodeDatatypeMessage(baseMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode array base type: %w", err)
	}

	// Encode array datatype message with dimensions
	return core.EncodeArrayDatatypeMessage(baseData, info.arrayDims, info.size)
}

// enumTypeHandler handles enumeration datatypes (named integer constants).
type enumTypeHandler struct {
	baseType Datatype
}

func (h *enumTypeHandler) GetInfo(config *datasetConfig) (*datatypeInfo, error) {
	if len(config.enumNames) == 0 || len(config.enumValues) == 0 {
		return nil, fmt.Errorf("enum datatype requires names and values (use WithEnumValues option)")
	}
	if len(config.enumNames) != len(config.enumValues) {
		return nil, fmt.Errorf("enum names and values must have same length (got %d names, %d values)",
			len(config.enumNames), len(config.enumValues))
	}

	// Get base type handler and info
	baseHandler, ok := datatypeRegistry[h.baseType]
	if !ok {
		return nil, fmt.Errorf("invalid enum base type: %d", h.baseType)
	}

	baseInfo, err := baseHandler.GetInfo(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get enum base type: %w", err)
	}

	return &datatypeInfo{
		class:      core.DatatypeEnum,
		size:       baseInfo.size, // Enum size = base type size
		baseType:   baseInfo,
		enumNames:  config.enumNames,
		enumValues: config.enumValues,
	}, nil
}

func (h *enumTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	// Encode base type message first
	baseMsg := &core.DatatypeMessage{
		Class:         info.baseType.class,
		Version:       1,
		Size:          info.baseType.size,
		ClassBitField: info.baseType.classBitField,
	}
	baseData, err := core.EncodeDatatypeMessage(baseMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode enum base type: %w", err)
	}

	// Convert enum values to bytes
	valueBytes := make([]byte, len(info.enumValues)*int(info.baseType.size))
	for i, val := range info.enumValues {
		offset := i * int(info.baseType.size)
		switch info.baseType.size {
		case 1:
			valueBytes[offset] = byte(val)
		case 2:
			binary.LittleEndian.PutUint16(valueBytes[offset:], uint16(val)) //nolint:gosec // Safe: validated size
		case 4:
			binary.LittleEndian.PutUint32(valueBytes[offset:], uint32(val)) //nolint:gosec // Safe: validated size
		case 8:
			binary.LittleEndian.PutUint64(valueBytes[offset:], uint64(val)) //nolint:gosec // Safe: validated size
		}
	}

	// Encode enum datatype message
	return core.EncodeEnumDatatypeMessage(baseData, info.enumNames, valueBytes, info.size)
}

// referenceTypeHandler handles reference datatypes (object and region references).
type referenceTypeHandler struct {
	size          uint32
	classBitField uint32
}

func (h *referenceTypeHandler) GetInfo(_ *datasetConfig) (*datatypeInfo, error) {
	return &datatypeInfo{
		class:         core.DatatypeReference,
		size:          h.size,
		classBitField: h.classBitField,
	}, nil
}

func (h *referenceTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	msg := &core.DatatypeMessage{
		Class:         info.class,
		Version:       1,
		Size:          info.size,
		ClassBitField: info.classBitField,
	}
	return core.EncodeDatatypeMessage(msg)
}

// opaqueTypeHandler handles opaque datatypes (uninterpreted byte sequences).
type opaqueTypeHandler struct{}

func (h *opaqueTypeHandler) GetInfo(config *datasetConfig) (*datatypeInfo, error) {
	if config.opaqueTag == "" || config.opaqueSize == 0 {
		return nil, fmt.Errorf("opaque datatype requires tag and size > 0 (use WithOpaqueTag option)")
	}
	return &datatypeInfo{
		class:     core.DatatypeOpaque,
		size:      config.opaqueSize,
		opaqueTag: config.opaqueTag,
	}, nil
}

func (h *opaqueTypeHandler) EncodeDatatypeMessage(info *datatypeInfo) ([]byte, error) {
	msg := &core.DatatypeMessage{
		Class:         core.DatatypeOpaque,
		Version:       1,
		Size:          info.size,
		ClassBitField: 0,
		Properties:    []byte(info.opaqueTag),
	}
	return core.EncodeDatatypeMessage(msg)
}

// datatypeRegistry is the global registry mapping Datatype constants to their handlers.
// This follows the Go stdlib pattern (encoding/json, database/sql, net/http).
var datatypeRegistry map[Datatype]datatypeHandler

// init initializes the datatype registry with all supported types.
func init() {
	datatypeRegistry = map[Datatype]datatypeHandler{
		// Basic integers (fixed-point)
		Int8:   &basicTypeHandler{core.DatatypeFixed, 1, 0x00},
		Int16:  &basicTypeHandler{core.DatatypeFixed, 2, 0x00},
		Int32:  &basicTypeHandler{core.DatatypeFixed, 4, 0x00},
		Int64:  &basicTypeHandler{core.DatatypeFixed, 8, 0x00},
		Uint8:  &basicTypeHandler{core.DatatypeFixed, 1, 0x00},
		Uint16: &basicTypeHandler{core.DatatypeFixed, 2, 0x00},
		Uint32: &basicTypeHandler{core.DatatypeFixed, 4, 0x00},
		Uint64: &basicTypeHandler{core.DatatypeFixed, 8, 0x00},

		// Floats
		Float32: &basicTypeHandler{core.DatatypeFloat, 4, 0x00},
		Float64: &basicTypeHandler{core.DatatypeFloat, 8, 0x00},

		// String
		String: &stringTypeHandler{},

		// Arrays
		ArrayInt8:    &arrayTypeHandler{Int8},
		ArrayInt16:   &arrayTypeHandler{Int16},
		ArrayInt32:   &arrayTypeHandler{Int32},
		ArrayInt64:   &arrayTypeHandler{Int64},
		ArrayUint8:   &arrayTypeHandler{Uint8},
		ArrayUint16:  &arrayTypeHandler{Uint16},
		ArrayUint32:  &arrayTypeHandler{Uint32},
		ArrayUint64:  &arrayTypeHandler{Uint64},
		ArrayFloat32: &arrayTypeHandler{Float32},
		ArrayFloat64: &arrayTypeHandler{Float64},

		// Enums
		EnumInt8:   &enumTypeHandler{Int8},
		EnumInt16:  &enumTypeHandler{Int16},
		EnumInt32:  &enumTypeHandler{Int32},
		EnumInt64:  &enumTypeHandler{Int64},
		EnumUint8:  &enumTypeHandler{Uint8},
		EnumUint16: &enumTypeHandler{Uint16},
		EnumUint32: &enumTypeHandler{Uint32},
		EnumUint64: &enumTypeHandler{Uint64},

		// References
		ObjectReference: &referenceTypeHandler{8, 0x00},
		RegionReference: &referenceTypeHandler{12, 0x01},

		// Opaque
		Opaque: &opaqueTypeHandler{},
	}
}

// getDatatypeInfo returns HDF5 datatype information for a Go datatype.
// Uses the registry pattern for O(1) lookup and simplified logic.
func getDatatypeInfo(dt Datatype, config *datasetConfig) (*datatypeInfo, error) {
	handler, ok := datatypeRegistry[dt]
	if !ok {
		return nil, fmt.Errorf("unsupported datatype: %d", dt)
	}
	return handler.GetInfo(config)
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

// Superblock version constants for file creation.
const (
	// SuperblockV0 (legacy format) - Maximum compatibility with older HDF5 tools.
	// Use this if you need files to be readable by h5dump, older Python h5py, or legacy C library.
	// This format doesn't have checksums but works with all HDF5 tools.
	SuperblockV0 = core.Version0

	// SuperblockV2 (modern format) - Default. Includes checksums for data integrity.
	// This is the recommended format for new files. Supported by HDF5 1.10+.
	SuperblockV2 = core.Version2

	// SuperblockV3 (latest format) - Future format, not yet implemented for writing.
	SuperblockV3 = core.Version3
)

// WriteOption is a functional option for configuring file creation.
type WriteOption func(*FileWriteConfig)

// FileWriteConfig holds configuration for file creation.
type FileWriteConfig struct {
	SuperblockVersion uint8 // HDF5 superblock version (0, 2, or 3)
}

// WithSuperblockVersion sets the HDF5 superblock version.
//
// Available versions:
//   - SuperblockV0: Legacy format, maximum compatibility with older tools (h5dump, etc.)
//   - SuperblockV2: Modern format with checksums (default)
//   - SuperblockV3: Latest format (not yet implemented for writing)
//
// Default: SuperblockV2 (modern format)
//
// Example for maximum compatibility:
//
//	fw, err := hdf5.CreateForWrite("file.h5", hdf5.CreateTruncate,
//	    hdf5.WithSuperblockVersion(hdf5.SuperblockV0))
func WithSuperblockVersion(version uint8) WriteOption {
	return func(cfg *FileWriteConfig) {
		cfg.SuperblockVersion = version
	}
}

// CreateForWrite creates a new HDF5 file for writing.
// Unlike Create(), this keeps the file open in write mode.
//
// Parameters:
//   - filename: Path to the file to create
//   - mode: Creation mode (truncate or exclusive)
//   - opts: Optional configuration (WithSuperblockVersion, etc.)
//
// Returns:
//   - *FileWriter: Handle for writing datasets
//   - error: If creation fails
//
// Example (default - modern format):
//
//	fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	if err != nil {
//	    return err
//	}
//	defer fw.Close()
//
// Example (legacy format for h5dump compatibility):
//
//	fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
//	    hdf5.WithSuperblockVersion(core.Version0))
func CreateForWrite(filename string, mode CreateMode, opts ...WriteOption) (*FileWriter, error) {
	// Apply default configuration
	cfg := &FileWriteConfig{
		SuperblockVersion: core.Version2, // Modern format by default
	}

	// Apply user options
	for _, opt := range opts {
		opt(cfg)
	}

	// Calculate superblock size based on version
	superblockSize := uint64(48) // v2/v3
	if cfg.SuperblockVersion == core.Version0 {
		superblockSize = 96 // v0 is larger
	}

	// Map CreateMode to writer.CreateMode and create basic writer
	fw, err := initializeFileWriter(filename, mode, superblockSize)
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
	rootInfo, err := createRootGroupStructure(fw, cfg.SuperblockVersion)
	if err != nil {
		return nil, err
	}

	// Step 3: Create Superblock with configured version
	sb := &core.Superblock{
		Version:        cfg.SuperblockVersion, // Use configured version
		OffsetSize:     8,
		LengthSize:     8,
		BaseAddress:    0,
		RootGroup:      rootInfo.groupAddr,
		Endianness:     binary.LittleEndian,
		SuperExtension: 0,
		DriverInfo:     0,
		// V0-specific cached addresses (required for h5dump compatibility)
		RootBTreeAddr: rootInfo.btreeAddr,
		RootHeapAddr:  rootInfo.heapAddr,
	}

	// Calculate end-of-file address
	var eofAddress uint64
	if cfg.SuperblockVersion == core.Version0 {
		// V0 uses fixed addresses - calculate from actual layout
		// EOF = last structure address + its size
		eofAddress = rootInfo.heapAddr + rootInfo.heapSize
	} else {
		// V2 uses allocator - get dynamic EOF
		eofAddress = fw.EndOfFile()
	}

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

// validateDatasetName validates that dataset name is not empty and starts with '/'.
func validateDatasetName(name string) error {
	if name == "" {
		return fmt.Errorf("dataset name cannot be empty")
	}
	if name[0] != '/' {
		return fmt.Errorf("dataset name must start with '/' (got %q)", name)
	}
	return nil
}

// validateDimensions validates that dimensions is not empty and no dimension is zero.
func validateDimensions(dims []uint64) error {
	if len(dims) == 0 {
		return fmt.Errorf("dimensions cannot be empty (use []uint64{1} for scalar)")
	}
	for i, dim := range dims {
		if dim == 0 {
			return fmt.Errorf("dimension %d cannot be 0", i)
		}
	}
	return nil
}

// calculateTotalElements calculates total number of elements from dimensions.
func calculateTotalElements(dims []uint64) uint64 {
	totalElements := uint64(1)
	for _, dim := range dims {
		totalElements *= dim
	}
	return totalElements
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
//
//nolint:gocyclo,cyclop // Complex by nature: dataset creation handles multiple layout types and options
func (fw *FileWriter) CreateDataset(name string, dtype Datatype, dims []uint64, opts ...DatasetOption) (*DatasetWriter, error) {
	// Validate inputs
	if err := validateDatasetName(name); err != nil {
		return nil, err
	}
	if err := validateDimensions(dims); err != nil {
		return nil, err
	}

	// Apply options
	config := &datasetConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// Check if chunked layout requested
	if len(config.chunkDims) > 0 {
		return fw.createChunkedDataset(name, dtype, dims, config)
	}

	// Get datatype info
	dtInfo, err := getDatatypeInfo(dtype, config)
	if err != nil {
		return nil, fmt.Errorf("invalid datatype: %w", err)
	}

	// Calculate total data size
	totalElements := calculateTotalElements(dims)
	dataSize := totalElements * uint64(dtInfo.size)

	// Allocate space for dataset data
	dataAddress, err := fw.writer.Allocate(dataSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate space for data: %w", err)
	}

	// Encode datatype message using handler (simplified from complex switch)
	handler := datatypeRegistry[dtype]
	datatypeData, err := handler.EncodeDatatypeMessage(dtInfo)
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
		nil, // No chunk dimensions for contiguous layout
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
	// For DatasetWriter, we need a simple DatatypeMessage for Write() operations
	// Advanced types will use the base type for data encoding
	var dsMsgForWriter *core.DatatypeMessage
	if dtInfo.baseType != nil {
		// For array/enum, use base type for data writing
		dsMsgForWriter = &core.DatatypeMessage{
			Class:   dtInfo.baseType.class,
			Version: 1,
			Size:    dtInfo.baseType.size,
		}
	} else {
		// For simple types, use the datatype itself
		dsMsgForWriter = &core.DatatypeMessage{
			Class:   dtInfo.class,
			Version: 1,
			Size:    dtInfo.size,
		}
	}

	dsw := &DatasetWriter{
		fileWriter:  fw,
		name:        name,
		address:     headerAddress,
		dataAddress: dataAddress,
		dataSize:    dataSize,
		dtype:       dsMsgForWriter,
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
	fileWriter       *FileWriter
	name             string
	address          uint64 // Object header address
	dataAddress      uint64 // Data storage address (contiguous) or B-tree address (chunked)
	dataSize         uint64 // Total data size in bytes
	dtype            *core.DatatypeMessage
	dims             []uint64
	isChunked        bool                     // True if using chunked layout
	chunkCoordinator *writer.ChunkCoordinator // For chunked datasets
	chunkDims        []uint64                 // Chunk dimensions
	pipeline         *writer.FilterPipeline   // Filter pipeline for chunked datasets

	// For RMW scenarios (files opened with OpenForWrite)
	objectHeader  *core.ObjectHeader         // Full object header (for attribute operations)
	denseAttrInfo *core.AttributeInfoMessage // Dense attribute storage info (nil if no dense storage)
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
	case core.DatatypeReference:
		// References are fixed-size types (8 or 12 bytes)
		buf, err = encodeFixedPointData(data, dw.dtype.Size, dw.dataSize)
	case core.DatatypeOpaque:
		// Opaque data is raw bytes
		buf, err = encodeOpaqueData(data, dw.dataSize)
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

	// Handle chunked vs contiguous layout
	if dw.isChunked {
		return dw.writeChunkedData(buf)
	}

	// Write data to file (contiguous layout)
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

	actualSize := uint64(dataLen) * uint64(elemSize) //nolint:gosec // Safe: slice length conversion
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
			bits := binary.LittleEndian.Uint32((*(*[4]byte)(unsafe.Pointer(&val)))[:]) //nolint:gosec // Safe: float32 to bits conversion
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}

	case 8:
		// float64
		v, ok := data.([]float64)
		if !ok {
			return nil, fmt.Errorf("expected []float64, got %T", data)
		}
		for i, val := range v {
			bits := binary.LittleEndian.Uint64((*(*[8]byte)(unsafe.Pointer(&val)))[:]) //nolint:gosec // Safe: float64 to bits conversion
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

// encodeOpaqueData encodes opaque data (raw bytes).
func encodeOpaqueData(data interface{}, expectedSize uint64) ([]byte, error) {
	// Opaque data must be []byte
	v, ok := data.([]byte)
	if !ok {
		return nil, fmt.Errorf("opaque data must be []byte, got %T", data)
	}

	// Validate size
	if uint64(len(v)) != expectedSize {
		return nil, fmt.Errorf("data size mismatch: expected %d bytes, got %d bytes", expectedSize, len(v))
	}

	// Return as-is (raw bytes)
	return v, nil
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
	stringSize    uint32
	arrayDims     []uint64               // For array datatypes
	enumNames     []string               // For enum datatypes
	enumValues    []int64                // For enum datatypes
	opaqueTag     string                 // For opaque datatypes
	opaqueSize    uint32                 // For opaque datatypes
	chunkDims     []uint64               // For chunked layout
	pipeline      *writer.FilterPipeline // Filter pipeline for chunked datasets
	enableShuffle bool                   // Add shuffle filter before compression
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

// WithArrayDims sets the dimensions for Array datatypes.
// This is required when creating an Array dataset.
//
// Array datatypes are fixed-size collections of a base type.
// The dimensions specify the shape of each array element.
//
// Example:
//
//	// Dataset of shape [10] where each element is [3]int32
//	ds, _ := fw.CreateDataset("/vectors", hdf5.ArrayInt32, []uint64{10}, hdf5.WithArrayDims([]uint64{3}))
//
//	// Dataset of shape [5] where each element is [2][3]float64 (2D array)
//	ds, _ := fw.CreateDataset("/matrices", hdf5.ArrayFloat64, []uint64{5}, hdf5.WithArrayDims([]uint64{2, 3}))
func WithArrayDims(dims []uint64) DatasetOption {
	return func(cfg *datasetConfig) {
		cfg.arrayDims = dims
	}
}

// WithEnumValues sets the name-value mappings for Enum datatypes.
// This is required when creating an Enum dataset.
//
// Enum datatypes map integer values to symbolic names.
// Both names and values slices must have the same length.
//
// Example:
//
//	// Create enum for days of week (0=Monday, 1=Tuesday, etc.)
//	names := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
//	values := []int64{0, 1, 2, 3, 4, 5, 6}
//	ds, _ := fw.CreateDataset("/days", hdf5.EnumInt8, []uint64{100}, hdf5.WithEnumValues(names, values))
func WithEnumValues(names []string, values []int64) DatasetOption {
	return func(cfg *datasetConfig) {
		cfg.enumNames = names
		cfg.enumValues = values
	}
}

// WithOpaqueTag sets the tag and size for Opaque datatypes.
// This is required when creating an Opaque dataset.
//
// Opaque datatypes are uninterpreted byte sequences with a descriptive tag.
// The tag describes the content (e.g., "JPEG image", "binary blob").
// The size specifies the number of bytes per element.
//
// Example:
//
//	// Dataset of 10 JPEG images, each 1MB
//	ds, _ := fw.CreateDataset("/images", hdf5.Opaque, []uint64{10}, hdf5.WithOpaqueTag("JPEG image", 1024*1024))
func WithOpaqueTag(tag string, size uint32) DatasetOption {
	return func(cfg *datasetConfig) {
		cfg.opaqueTag = tag
		cfg.opaqueSize = size
	}
}

// WithChunkDims enables chunked storage with specified chunk dimensions.
// When specified, the dataset will use chunked layout instead of contiguous.
//
// Chunk dimensions must match dataset rank and be > 0 in all dimensions.
// Chunks should be chosen for optimal I/O patterns (typical: 10KB-1MB per chunk).
//
// Example:
//
//	// 2D dataset 1000x2000, chunked as 100x200
//	ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000, 2000}, hdf5.WithChunkDims([]uint64{100, 200}))
func WithChunkDims(dims []uint64) DatasetOption {
	return func(cfg *datasetConfig) {
		cfg.chunkDims = dims
	}
}

// WithGZIPCompression enables GZIP compression with specified level (1-9).
// This option is only valid for chunked datasets (requires WithChunkDims).
//
// Compression levels:
//
//	1 = fastest compression, larger files
//	6 = balanced (default if invalid level)
//	9 = best compression, slower
//
// GZIP compression reduces storage size but adds CPU overhead during read/write.
// Best used with repetitive or structured data.
//
// Example:
//
//	// Create compressed dataset with level 6 compression
//	ds, _ := fw.CreateDataset("/data", hdf5.Int32, []uint64{1000},
//	    hdf5.WithChunkDims([]uint64{100}),
//	    hdf5.WithGZIPCompression(6))
func WithGZIPCompression(level int) DatasetOption {
	return func(cfg *datasetConfig) {
		if cfg.pipeline == nil {
			cfg.pipeline = writer.NewFilterPipeline()
		}
		cfg.pipeline.AddFilter(writer.NewGZIPFilter(level))
	}
}

// WithShuffle enables byte shuffle filter (improves compression).
// This option is only valid for chunked datasets (requires WithChunkDims).
//
// The shuffle filter reorders bytes to group similar values, significantly
// improving compression ratios for numeric data (typically 2-10x better).
//
// Shuffle should be combined with compression (e.g., GZIP) to be effective.
// It's automatically placed before compression in the filter pipeline.
//
// Best for:
//   - Integer arrays with slowly changing values
//   - Floating-point arrays with similar magnitudes
//   - Multi-dimensional arrays with spatial locality
//
// Example:
//
//	// Create dataset with shuffle+compression for best compression
//	ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{1000},
//	    hdf5.WithChunkDims([]uint64{100}),
//	    hdf5.WithShuffle(),
//	    hdf5.WithGZIPCompression(9))
func WithShuffle() DatasetOption {
	return func(cfg *datasetConfig) {
		if cfg.pipeline == nil {
			cfg.pipeline = writer.NewFilterPipeline()
		}
		// Shuffle will be inserted at the beginning of pipeline during dataset creation
		cfg.enableShuffle = true
	}
}

// WithFletcher32 enables Fletcher32 checksum for data integrity verification.
// This option is only valid for chunked datasets (requires WithChunkDims).
//
// The Fletcher32 filter adds a 4-byte checksum to each chunk, allowing detection
// of data corruption during storage or transmission.
//
// Overhead:
//   - Storage: +4 bytes per chunk (minimal)
//   - CPU: Low (faster than CRC32)
//
// Use when:
//   - Data integrity is critical
//   - Detecting corruption is more important than preventing it
//   - Working with unreliable storage or network
//
// Example:
//
//	// Create dataset with compression and checksum
//	ds, _ := fw.CreateDataset("/data", hdf5.Int32, []uint64{1000},
//	    hdf5.WithChunkDims([]uint64{100}),
//	    hdf5.WithGZIPCompression(6),
//	    hdf5.WithFletcher32())
func WithFletcher32() DatasetOption {
	return func(cfg *datasetConfig) {
		if cfg.pipeline == nil {
			cfg.pipeline = writer.NewFilterPipeline()
		}
		cfg.pipeline.AddFilter(writer.NewFletcher32Filter())
	}
}

// OpenMode specifies how to open an existing HDF5 file.
type OpenMode int

const (
	// OpenReadOnly opens the file for reading only.
	OpenReadOnly OpenMode = iota

	// OpenReadWrite opens the file for both reading and writing.
	// This enables read-modify-write operations like adding attributes
	// to existing dense storage.
	OpenReadWrite
)

// OpenForWrite opens an existing HDF5 file for modification.
// This function enables read-modify-write operations on existing files.
//
// Supported operations:
//   - Adding attributes to datasets with existing dense storage
//   - Creating new datasets in existing files
//   - Creating new groups (when group write support is added)
//
// Parameters:
//   - filename: Path to existing HDF5 file
//   - mode: Open mode (OpenReadOnly or OpenReadWrite)
//
// Returns:
//   - *FileWriter: Handle for modifying the file
//   - error: If file doesn't exist or isn't a valid HDF5 file
//
// Example:
//
//	// Reopen file to add more attributes
//	fw, err := hdf5.OpenForWrite("data.h5", hdf5.OpenReadWrite)
//	if err != nil {
//	    return err
//	}
//	defer fw.Close()
//
//	// Open existing dataset
//	ds, err := fw.OpenDataset("/temperature")
//	if err != nil {
//	    return err
//	}
//
//	// Add more attributes to existing dense storage
//	ds.WriteAttribute("calibration_date", "2025-11-01")
//	ds.WriteAttribute("sensor_location", "Lab A")
func OpenForWrite(filename string, mode OpenMode) (*FileWriter, error) {
	// Step 1: Open existing HDF5 file for reading (to load structure)
	f, err := Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	// Step 2: Create low-level writer for RMW operations
	var writerMode writer.CreateMode
	if mode == OpenReadWrite {
		writerMode = writer.ModeReadWrite // New mode for RMW
	} else {
		writerMode = writer.ModeReadOnly // Read-only mode
	}

	// Determine initial offset from superblock
	superblockSize := uint64(48) // v2/v3
	if f.sb.Version == core.Version0 {
		superblockSize = 96 // v0
	}

	fw, err := writer.OpenFileWriter(filename, writerMode, superblockSize)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to create writer: %w", err)
	}

	// Step 3: Extract root group information from existing file
	rootGroupAddr := f.sb.RootGroup
	rootBTreeAddr := f.sb.RootBTreeAddr // v0 only
	rootHeapAddr := f.sb.RootHeapAddr   // v0 only
	rootStNodeAddr := uint64(0)         // Will need to extract if needed

	// Step 4: Create FileWriter with loaded structures
	fileWriter := &FileWriter{
		file:           f,
		writer:         fw,
		filename:       filename,
		rootGroupAddr:  rootGroupAddr,
		rootBTreeAddr:  rootBTreeAddr,
		rootHeapAddr:   rootHeapAddr,
		rootStNodeAddr: rootStNodeAddr,
	}

	return fileWriter, nil
}

// OpenDataset opens an existing dataset for modification.
// This enables read-modify-write operations on datasets.
//
// Supported operations:
//   - WriteAttribute(): Add attributes to existing dense storage
//   - Write(): Overwrite dataset data (for contiguous layout)
//
// Parameters:
//   - path: Dataset path (e.g., "/temperature")
//
// Returns:
//   - *DatasetWriter: Handle for modifying the dataset
//   - error: If dataset doesn't exist
//
// Example:
//
//	fw, _ := hdf5.OpenForWrite("data.h5", hdf5.OpenReadWrite)
//	defer fw.Close()
//
//	ds, _ := fw.OpenDataset("/temperature")
//	ds.WriteAttribute("units", "Celsius")  // Works with existing dense storage!
//
//nolint:gocognit,gocyclo,cyclop // Complex navigation logic with multiple object types and error paths
func (fw *FileWriter) OpenDataset(path string) (*DatasetWriter, error) {
	// Step 1: Navigate to dataset using file.Walk()
	var foundDataset *Dataset
	fw.file.Walk(func(p string, obj Object) {
		if p == path {
			if ds, ok := obj.(*Dataset); ok {
				foundDataset = ds
			}
		}
	})

	if foundDataset == nil {
		return nil, fmt.Errorf("dataset %q not found", path)
	}

	// Step 2: Read object header to extract dataset metadata
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), foundDataset.Address(), fw.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read object header: %w", err)
	}

	// Step 3: Extract datatype, dataspace, layout, and attribute info messages
	var datatypeMsg *core.DatatypeMessage
	var dataspaceMsg *core.DataspaceMessage
	var layoutMsg *core.DataLayoutMessage
	var attrInfoMsg *core.AttributeInfoMessage

	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgDatatype:
			datatypeMsg, err = core.ParseDatatypeMessage(msg.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse datatype: %w", err)
			}
		case core.MsgDataspace:
			dataspaceMsg, err = core.ParseDataspaceMessage(msg.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse dataspace: %w", err)
			}
		case core.MsgDataLayout:
			layoutMsg, err = core.ParseDataLayoutMessage(msg.Data, fw.file.sb)
			if err != nil {
				return nil, fmt.Errorf("failed to parse layout: %w", err)
			}
		case core.MsgAttributeInfo:
			attrInfoMsg, err = core.ParseAttributeInfoMessage(msg.Data, fw.file.sb)
			if err != nil {
				return nil, fmt.Errorf("failed to parse attribute info: %w", err)
			}
		}
	}

	if datatypeMsg == nil || dataspaceMsg == nil || layoutMsg == nil {
		return nil, fmt.Errorf("dataset metadata incomplete (missing datatype, dataspace, or layout)")
	}

	// Step 4: Calculate data size
	totalElements := uint64(1)
	for _, dim := range dataspaceMsg.Dimensions {
		totalElements *= dim
	}
	dataSize := totalElements * uint64(datatypeMsg.Size)

	// Step 5: Create DatasetWriter
	dsw := &DatasetWriter{
		fileWriter:    fw,
		name:          path,
		address:       foundDataset.Address(),
		dataAddress:   layoutMsg.DataAddress, // Data address from layout message
		dataSize:      dataSize,
		dtype:         datatypeMsg,
		dims:          dataspaceMsg.Dimensions,
		objectHeader:  oh,          // Store object header for attribute operations
		denseAttrInfo: attrInfoMsg, // May be nil if no dense storage yet
	}

	return dsw, nil
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
func initializeFileWriter(filename string, mode CreateMode, superblockSize uint64) (*writer.FileWriter, error) {
	var writerMode writer.CreateMode
	switch mode {
	case CreateTruncate:
		writerMode = writer.ModeTruncate
	case CreateExclusive:
		writerMode = writer.ModeExclusive
	default:
		return nil, fmt.Errorf("invalid create mode: %d", mode)
	}

	// Superblock size passed from caller (48 for v2, 96 for v0)
	fw, err := writer.NewFileWriter(filename, writerMode, superblockSize)
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
	heapSize   uint64 // Local heap size (for v0 EOF calculation)
}

// createRootGroupStructure creates the root group with Symbol Table structure.
// Returns information about the created root group structure.
// createRootGroupStructure creates the root group structures.
// Dispatches to version-specific implementation based on superblock version.
func createRootGroupStructure(fw *writer.FileWriter, superblockVersion uint8) (*rootGroupInfo, error) {
	if superblockVersion == core.Version0 {
		return createRootGroupStructureV0(fw)
	}
	return createRootGroupStructureV2(fw)
}

// createRootGroupStructureV2 creates root group for modern format (v2/v3).
// Order: Heap → B-tree → Object Header (v2 doesn't cache addresses in superblock).
func createRootGroupStructureV2(fw *writer.FileWriter) (*rootGroupInfo, error) {
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
		heapSize:   rootHeap.Size(), // For v0 EOF calculation (v2 uses allocator)
	}, nil
}

// createRootGroupStructureV0 creates root group for legacy format (v0).
// Order: Object Header → B-tree → Heap (as per C library H5Gobj.c)
// This matches the reference implementation where:
// 1. H5O_create() creates object header first
// 2. H5G__stab_create_components() creates B-tree, then heap.
func createRootGroupStructureV0(fw *writer.FileWriter) (*rootGroupInfo, error) {
	const offsetSize = 8
	const lengthSize = 8

	// Step 1: Calculate sizes for pre-allocation
	// We need to know addresses before writing, so allocate space first

	// Object Header size for v0 group with symbol table message
	// Header: 16 bytes (signature + version + reserved + messages)
	// Symbol Table Message: 4 (type+size+flags+reserved) + 16 (btree_addr + heap_addr)
	// NULL message: 4 (type+size+flags+reserved) for padding
	objHeaderSize := uint64(16 + 20 + 4)

	// B-tree node size: signature(4) + node_type(1) + node_level(1) + entries_used(2) +
	//                   left_sibling(8) + right_sibling(8) + key+child pairs
	// For root with 1 child: 24 + (8+8)*2 = 56 bytes (minimum)
	btreeSize := uint64(56)

	// Symbol table node size: signature(4) + version(1) + reserved(1) + num_symbols(2) +
	//                          entries (40 bytes each, capacity 32)
	stNodeSize := uint64(8 + 32*40)

	// Local heap size: minimum ~256 bytes
	heapSize := uint64(256)

	// Step 2: Calculate fixed addresses (no Allocate - we write at fixed offsets)
	// Superblock v0: 0x00-0x5F (96 bytes)
	rootGroupAddr := uint64(96)                    // 0x60 - immediately after superblock
	rootBTreeAddr := rootGroupAddr + objHeaderSize // After object header
	rootStNodeAddr := rootBTreeAddr + btreeSize    // After B-tree
	rootHeapAddr := rootStNodeAddr + stNodeSize    // After symbol table node

	// Step 3: Write structures in ASCENDING ADDRESS ORDER
	// CRITICAL: Sequential write order prevents sparse file holes on Windows!
	// Order: Object Header (96) → B-tree (136) → SNOD (192) → Heap (1480)

	// 1. Write root group object header (offset 96)
	// V0 superblock requires Object Header v1 (not v2!)
	const objectHeaderVersion = 1
	actualObjHeaderSize, err := writeRootGroupHeaderAt(fw, rootGroupAddr, rootBTreeAddr, rootHeapAddr, offsetSize, lengthSize, objectHeaderVersion)
	if err != nil {
		return nil, err
	}

	// 2. Write B-tree (offset 136, immediately after object header)
	if err := writeBTreeNodeAt(fw, rootBTreeAddr, rootStNodeAddr, offsetSize); err != nil {
		return nil, err
	}

	// 3. Write symbol table node (offset 192, after B-tree)
	if err := writeSymbolTableNodeAt(fw, rootStNodeAddr, offsetSize); err != nil {
		return nil, err
	}

	// 4. Write local heap (offset 1480, after symbol table node)
	rootHeap := structures.NewLocalHeap(256)
	if err := rootHeap.WriteTo(fw, rootHeapAddr); err != nil {
		return nil, fmt.Errorf("failed to write root heap: %w", err)
	}

	return &rootGroupInfo{
		groupAddr:  rootGroupAddr,
		groupSize:  actualObjHeaderSize,
		btreeAddr:  rootBTreeAddr,
		heapAddr:   rootHeapAddr,
		stNodeAddr: rootStNodeAddr,
		heapSize:   heapSize, // For v0 EOF calculation
	}, nil
}

// writeSymbolTableNodeAt writes a symbol table node at the specified address.
func writeSymbolTableNodeAt(fw *writer.FileWriter, addr uint64, offsetSize int) error {
	rootStNode := structures.NewSymbolTableNode(32) // Standard capacity (2*K where K=16)

	// Write symbol table node (empty initially)
	if err := rootStNode.WriteAt(fw, addr, uint8(offsetSize), 32, binary.LittleEndian); err != nil { //nolint:gosec // Safe: offsetSize validated to be 8
		return fmt.Errorf("failed to write symbol table node: %w", err)
	}

	return nil
}

// writeBTreeNodeAt writes a B-tree node at the specified address.
func writeBTreeNodeAt(fw *writer.FileWriter, addr, stNodeAddr uint64, offsetSize int) error {
	rootBTree := structures.NewBTreeNodeV1(0, 16) // Type 0 = group symbol table, K=16

	// Add symbol table node address as child (with key 0 for empty group)
	if err := rootBTree.AddKey(0, stNodeAddr); err != nil {
		return fmt.Errorf("failed to add B-tree key: %w", err)
	}

	// Write B-tree
	if err := rootBTree.WriteAt(fw, addr, uint8(offsetSize), 16, binary.LittleEndian); err != nil { //nolint:gosec // Safe: offsetSize validated to be 8
		return fmt.Errorf("failed to write B-tree: %w", err)
	}

	return nil
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

// writeRootGroupHeaderAt writes the root group object header at the specified address.
// Returns the actual size written.
// The objectHeaderVersion parameter determines which object header format to use (1 or 2).
func writeRootGroupHeaderAt(fw *writer.FileWriter, addr, btreeAddr, heapAddr uint64, offsetSize, lengthSize int, objectHeaderVersion uint8) (uint64, error) {
	stMsg := core.EncodeSymbolTableMessage(btreeAddr, heapAddr, offsetSize, lengthSize)

	rootGroupHeader := &core.ObjectHeaderWriter{
		Version: objectHeaderVersion,
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgSymbolTable, Data: stMsg},
		},
		RefCount: 1, // Always 1 for new files (used by v1, ignored by v2)
	}

	// Write root group object header
	writtenSize, err := rootGroupHeader.WriteTo(fw, addr)
	if err != nil {
		return 0, fmt.Errorf("failed to write root group header: %w", err)
	}

	return writtenSize, nil
}

// writeRootGroupHeader creates and writes the root group object header.
// Returns the address where the header was written and its size.
// Uses Object Header v2 (for superblock v2).
func writeRootGroupHeader(fw *writer.FileWriter, btreeAddr, heapAddr uint64, offsetSize, lengthSize int) (uint64, uint64, error) {
	stMsg := core.EncodeSymbolTableMessage(btreeAddr, heapAddr, offsetSize, lengthSize)

	rootGroupHeader := &core.ObjectHeaderWriter{
		Version: 2, // V2 superblock uses Object Header v2
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgSymbolTable, Data: stMsg},
		},
		RefCount: 1, // Always 1 for new files
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
