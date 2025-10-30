package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/scigolib/hdf5/internal/core"
)

// WriteAttribute writes an attribute to a dataset.
// Attributes are metadata stored in the object header (compact storage).
//
// For MVP (v0.11.0-beta):
//   - Only compact storage (stored in object header)
//   - No dense attributes (fractal heap storage)
//   - Attributes must fit in object header space
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
//   - Maximum ~1KB total attributes per object (object header space limit)
//   - No variable-length strings
//   - No compound types
//   - Attributes cannot be modified after creation (write-once)
//   - NOT YET IMPLEMENTED - planned for v0.11.0-RC
func (ds *DatasetWriter) WriteAttribute(name string, value interface{}) error {
	// Use fw field instead of objectHeaderAddr directly
	return writeAttribute(ds.fileWriter, ds.address, name, value)
}

// writeAttribute is the internal implementation for writing attributes.
// It encodes the attribute and adds it to the object header.
//
// For MVP, we have a significant limitation: we cannot easily modify object headers
// after they are written to disk because:
// 1. Object header size might change (need continuation blocks)
// 2. File structure is complex (need to track free space)
//
// Current approach for MVP:
// - Return error (not yet implemented)
// - In v0.11.0-RC, we'll implement proper object header modification with continuation blocks
//
// NOTE: Object header modification with continuation blocks planned for v0.11.0-RC.
func writeAttribute(_ *FileWriter, objectHeaderAddr uint64, name string, value interface{}) error {
	// For MVP, attributes are not yet supported
	// This requires:
	// 1. Reading existing object header from disk
	// 2. Adding attribute message
	// 3. Handling object header size growth (continuation blocks)
	// 4. Writing updated object header back to disk
	//
	// This is deferred to v0.11.0-RC for proper implementation
	return fmt.Errorf("attribute writing not yet implemented in MVP (v0.11.0-beta); will be available in v0.11.0-RC")
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
	size := uint32(len(str) + 1) // Include null terminator

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
	length := uint64(v.Len())

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
		binary.LittleEndian.PutUint16(buf, uint16(v.Int()))
		return buf, nil
	case reflect.Int32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Int()))
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(v.Int()))
		return buf, nil
	case reflect.Uint8:
		return []byte{byte(v.Uint())}, nil
	case reflect.Uint16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v.Uint()))
		return buf, nil
	case reflect.Uint32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Uint()))
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
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(val))
		}
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			val := v.Index(i).Int()
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(val))
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
