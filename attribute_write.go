package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unsafe"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/writer"
)

// Attribute storage threshold.
const (
	// MaxCompactAttributes is the threshold for transitioning to dense storage.
	// When an object has 8+ attributes, dense storage (Fractal Heap + B-tree)
	// is more efficient than compact storage (object header messages).
	MaxCompactAttributes = 8
)

// WriteAttribute writes an attribute to a dataset.
//
// Storage strategy (automatic):
//   - 0-7 attributes: Compact storage (object header messages)
//   - 8+ attributes: Dense storage (Fractal Heap + B-tree v2)
//
// Supported value types:
//   - Scalars: int8, int16, int32, int64, uint8, uint16, uint32, uint64, float32, float64
//   - Arrays: []int32, []float64, etc. (1D arrays only)
//   - Strings: string (fixed-length, converted to byte array)
//
// Parameters:
//   - name: Attribute name (ASCII, no null bytes)
//   - value: Attribute value (Go scalar, slice, or string)
//
// Returns:
//   - error: If attribute cannot be written
//
// Example:
//
//	ds, _ := fw.CreateDataset("/temperature", Float64, []uint64{10})
//	ds.WriteAttribute("units", "Celsius")
//	ds.WriteAttribute("sensor_id", int32(42))
//	ds.WriteAttribute("calibration", []float64{1.0, 0.0})
//
// Limitations:
//   - No variable-length strings
//   - No compound types
//   - Attributes cannot be modified after creation (write-once)
//   - No attribute deletion
func (ds *DatasetWriter) WriteAttribute(name string, value interface{}) error {
	return writeAttribute(ds.fileWriter, ds.address, name, value)
}

// writeAttribute is the internal implementation for writing attributes.
//
// Storage strategy:
// - 0-7 attributes: Compact storage (object header messages)
// - 8+ attributes: Dense storage (Fractal Heap + B-tree v2)
//
// Automatic transition:
// - When adding the 8th attribute, all attributes are migrated to dense storage
// - Compact attribute messages are removed from object header
// - Attribute Info Message is added to object header
//
// For MVP:
// - Transition is one-way (compact → dense only, no dense → compact)
// - No attribute deletion support
//
// Reference: H5Aint.c - H5A__dense_create().
func writeAttribute(fw *FileWriter, objectAddr uint64, name string, value interface{}) error {
	// Get superblock
	sb := fw.file.Superblock()

	// Read object header
	reader := fw.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// Count existing attributes
	compactCount := 0
	hasDenseStorage := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			compactCount++
		}
		if msg.Type == core.MsgAttributeInfo {
			hasDenseStorage = true
		}
	}

	// Determine storage strategy
	if hasDenseStorage {
		// Already using dense storage → add to dense
		return writeDenseAttribute(fw, objectAddr, oh, name, value, sb)
	}

	if compactCount < MaxCompactAttributes {
		// Still compact → add compact attribute
		return writeCompactAttribute(fw, objectAddr, oh, name, value, sb)
	}

	// Transition needed → migrate to dense
	return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
}

// writeCompactAttribute writes attribute to object header (compact storage).
// This is the Phase 1 code, extracted into separate function.
func writeCompactAttribute(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Infer datatype and encode attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 2. Check for duplicate attribute name
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			existingAttr, err := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if err == nil && existingAttr.Name == name {
				return fmt.Errorf("attribute %q already exists (overwrite not yet supported)", name)
			}
		}
	}

	// 3. Encode attribute message
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute message: %w", err)
	}

	// 4. Add attribute message to header
	err = core.AddMessageToObjectHeader(oh, core.MsgAttribute, attrMsg)
	if err != nil {
		// If object header is full, transition to dense storage
		// This can happen before reaching MaxCompactAttributes if attributes are large
		if strings.Contains(err.Error(), "object header full") {
			// Trigger transition by calling transitionToDenseAttributes
			return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
		}
		return fmt.Errorf("failed to add message to header: %w", err)
	}

	// 5. Write updated header back to disk
	err = core.WriteObjectHeader(fw.writer, objectAddr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	return nil
}

// writeDenseAttribute writes attribute to existing dense storage (heap + B-tree).
//
// MVP Limitation (v0.11.1-beta):
// Adding attributes to EXISTING dense storage is not yet supported.
// This will be implemented in v0.11.2-beta when we add read-modify-write support.
//
// Current workaround:
// Write all attributes in a single session before closing the file.
// After file is closed and reopened, adding more attributes requires reading
// the entire heap and B-tree from disk, which needs additional infrastructure.
//
// What works now (Phase 2):
// - Create dataset
// - Add 8+ attributes → automatic dense storage creation
// - All attributes written in one session
//
// What doesn't work yet (Phase 3 - planned for v0.11.2-beta):
// - Reopen file and add more attributes to existing dense storage.
//
// Reference: H5Adense.c - H5A__dense_insert().
func writeDenseAttribute(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// Avoid unused parameter warnings
	_ = fw
	_ = objectAddr
	_ = oh
	_ = name
	_ = value
	_ = sb

	// For v0.11.2-beta: Implement read-modify-write
	// 1. Read existing Attribute Info Message
	// 2. Read WritableFractalHeap from file
	// 3. Read WritableBTreeV2 from file
	// 4. Add new attribute
	// 5. Write updated structures back

	return fmt.Errorf("adding attributes to existing dense storage not yet implemented (v0.11.2-beta planned feature)")
}

// transitionToDenseAttributes migrates all compact attributes to dense storage.
//
// Process:
// 1. Read all compact attributes from object header
// 2. Create DenseAttributeWriter
// 3. Add all existing attributes to dense storage
// 4. Add new attribute to dense storage
// 5. Write dense storage (heap + B-tree)
// 6. Get Attribute Info Message
// 7. Remove all compact attribute messages from object header
// 8. Add Attribute Info Message to object header
// 9. Write updated object header
//
// Reference: H5Aint.c - H5A__dense_create().
func transitionToDenseAttributes(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Read all existing compact attributes
	var compactAttrs []*core.Attribute
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			attr, err := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if err != nil {
				return fmt.Errorf("failed to parse existing attribute: %w", err)
			}
			compactAttrs = append(compactAttrs, attr)
		}
	}

	// 2. Infer datatype and encode new attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	newAttr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 3. Create DenseAttributeWriter
	daw := writer.NewDenseAttributeWriter(objectAddr)

	// 4. Add all existing attributes
	for _, attr := range compactAttrs {
		err = daw.AddAttribute(attr, sb)
		if err != nil {
			return fmt.Errorf("failed to add existing attribute: %w", err)
		}
	}

	// 5. Add new attribute
	err = daw.AddAttribute(newAttr, sb)
	if err != nil {
		return fmt.Errorf("failed to add new attribute: %w", err)
	}

	// 6. Write dense storage
	allocator := fw.writer.Allocator()
	attrInfo, err := daw.WriteToFile(fw.writer, allocator, sb)
	if err != nil {
		return fmt.Errorf("failed to write dense storage: %w", err)
	}

	// 7. Encode Attribute Info Message
	attrInfoMsg, err := core.EncodeAttributeInfoMessage(attrInfo, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute info: %w", err)
	}

	// 8. Remove all compact attribute messages from object header
	var newMessages []*core.HeaderMessage
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgAttribute {
			newMessages = append(newMessages, msg)
		}
	}
	oh.Messages = newMessages

	// 9. Add Attribute Info Message
	err = core.AddMessageToObjectHeader(oh, core.MsgAttributeInfo, attrInfoMsg)
	if err != nil {
		return fmt.Errorf("failed to add attribute info message: %w", err)
	}

	// 10. Write updated object header
	err = core.WriteObjectHeader(fw.writer, objectAddr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	return nil
}

// inferDatatypeFromValue infers HDF5 datatype and dimensions from a Go value.
// Returns datatype message, dataspace message, and error.
func inferDatatypeFromValue(value interface{}) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	v := reflect.ValueOf(value)

	// Handle scalar types
	if !v.IsValid() {
		return nil, nil, fmt.Errorf("value is nil or invalid")
	}

	switch v.Kind() {
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return inferSignedInt(v)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return inferUnsignedInt(v)
	case reflect.Float32, reflect.Float64:
		return inferFloat(v)
	case reflect.String:
		return inferString(v)
	case reflect.Slice:
		return inferSlice(v)
	default:
		return nil, nil, fmt.Errorf("unsupported value type: %s", v.Kind())
	}
}

// inferSignedInt infers datatype for signed integers.
func inferSignedInt(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Int8:
		size = 1
	case reflect.Int16:
		size = 2
	case reflect.Int32:
		size = 4
	case reflect.Int64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not a signed integer type")
	}

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFixed,
		Size:          size,
		ClassBitField: 0x08, // Bit 3 set for signed integers
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar (HDF5 uses [1] for scalars)
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferUnsignedInt infers datatype for unsigned integers.
func inferUnsignedInt(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Uint8:
		size = 1
	case reflect.Uint16:
		size = 2
	case reflect.Uint32:
		size = 4
	case reflect.Uint64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not an unsigned integer type")
	}

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFixed,
		Size:          size,
		ClassBitField: 0, // Bit 3 clear for unsigned integers
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferFloat infers datatype for floating point numbers.
func inferFloat(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Float32:
		size = 4
	case reflect.Float64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not a float type")
	}

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFloat,
		Size:          size,
		ClassBitField: 0, // Little-endian
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferString infers datatype for strings.
func inferString(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	str := v.String()
	size := uint32(len(str) + 1) //nolint:gosec // Safe: string length fits in uint32

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeString,
		Size:          size,
		ClassBitField: 0, // ASCII, null-terminated
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferSlice infers datatype for slices (1D arrays).
func inferSlice(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	if v.Len() == 0 {
		return nil, nil, fmt.Errorf("cannot infer datatype from empty slice")
	}

	elemKind := v.Type().Elem().Kind()
	length := uint64(v.Len()) //nolint:gosec // Safe: slice length conversion

	var dt *core.DatatypeMessage

	switch elemKind {
	case reflect.Int32:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFixed,
			Size:          4,
			ClassBitField: 0x08, // Signed
		}
	case reflect.Int64:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFixed,
			Size:          8,
			ClassBitField: 0x08, // Signed
		}
	case reflect.Float32:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFloat,
			Size:          4,
			ClassBitField: 0,
		}
	case reflect.Float64:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFloat,
			Size:          8,
			ClassBitField: 0,
		}
	default:
		return nil, nil, fmt.Errorf("unsupported slice element type: %s", elemKind)
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{length},
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// encodeAttributeValue encodes a Go value to bytes for attribute storage.
func encodeAttributeValue(value interface{}) ([]byte, error) {
	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Int8:
		return []byte{byte(v.Int())}, nil
	case reflect.Int16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Int32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint8:
		return []byte{byte(v.Uint())}, nil
	case reflect.Uint16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v.Uint())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Uint())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, v.Uint())
		return buf, nil
	case reflect.Float32:
		bits := math.Float32bits(float32(v.Float()))
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, bits)
		return buf, nil
	case reflect.Float64:
		bits := math.Float64bits(v.Float())
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, bits)
		return buf, nil
	case reflect.String:
		str := v.String()
		buf := make([]byte, len(str)+1)
		copy(buf, str)
		buf[len(str)] = 0 // Null terminator
		return buf, nil
	case reflect.Slice:
		return encodeSliceValue(v)
	default:
		return nil, fmt.Errorf("unsupported value type for encoding: %s", v.Kind())
	}
}

// encodeSliceValue encodes a slice to bytes.
func encodeSliceValue(v reflect.Value) ([]byte, error) {
	elemKind := v.Type().Elem().Kind()
	length := v.Len()

	switch elemKind {
	case reflect.Int32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			val := v.Index(i).Int()
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(val)) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			val := v.Index(i).Int()
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(val)) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Float32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			val := v.Index(i).Float()
			bits := math.Float32bits(float32(val))
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}
		return buf, nil
	case reflect.Float64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			val := v.Index(i).Float()
			bits := math.Float64bits(val)
			binary.LittleEndian.PutUint64(buf[i*8:], bits)
		}
		return buf, nil
	default:
		return nil, fmt.Errorf("unsupported slice element type: %s", elemKind)
	}
}

// Suppress unused warnings for now (these will be used when attribute writing is fully implemented).
var (
	_ = (*core.DatatypeMessage)(nil)
	_ = (*core.DataspaceMessage)(nil)
	_ = inferDatatypeFromValue
	_ = encodeAttributeValue
	_ = unsafe.Sizeof(0)
)
