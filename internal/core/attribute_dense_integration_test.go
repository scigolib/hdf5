package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ReadValue tests – additional datatype varieties
// ---------------------------------------------------------------------------

func TestReadValue_Int8(t *testing.T) {
	// Int8 is not directly supported by ReadValue (only size 4 and 8 for Fixed).
	// Expect an "unsupported" error for size=1.
	attr := &Attribute{
		Name: "int8_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Size:          1,
			ClassBitField: 0x08, // signed
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: []byte{0x01, 0x02, 0x03},
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported datatype class")
}

func TestReadValue_Int16(t *testing.T) {
	// Int16 is not directly supported by ReadValue (only size 4 and 8 for Fixed).
	// Expect an "unsupported" error for size=2.
	attr := &Attribute{
		Name: "int16_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Size:          2,
			ClassBitField: 0x08, // signed
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{2},
		},
		Data: []byte{0x01, 0x00, 0x02, 0x00},
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported datatype class")
}

func TestReadValue_Uint32(t *testing.T) {
	// ReadValue returns int32 for all Fixed/size=4 regardless of sign bit.
	// Unsigned 32-bit values are read as int32 (HDF5 convention in this library).
	attr := &Attribute{
		Name: "uint32_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Size:          4,
			ClassBitField: 0x00, // unsigned, little-endian
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: func() []byte {
			buf := make([]byte, 12)
			binary.LittleEndian.PutUint32(buf[0:4], 100)
			binary.LittleEndian.PutUint32(buf[4:8], 200)
			binary.LittleEndian.PutUint32(buf[8:12], 300)
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []int32{100, 200, 300}, val)
}

func TestReadValue_Float32(t *testing.T) {
	attr := &Attribute{
		Name: "float32_attr",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: func() []byte {
			buf := make([]byte, 4)
			binary.LittleEndian.PutUint32(buf, math.Float32bits(3.14))
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.InDelta(t, float32(3.14), val, 0.001)
}

func TestReadValue_Float64Array(t *testing.T) {
	attr := &Attribute{
		Name: "float64_array_attr",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{4},
		},
		Data: func() []byte {
			values := []float64{1.1, 2.2, 3.3, 4.4}
			buf := make([]byte, 32)
			for i, v := range values {
				binary.LittleEndian.PutUint64(buf[i*8:(i+1)*8], math.Float64bits(v))
			}
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	arr, ok := val.([]float64)
	require.True(t, ok)
	require.Len(t, arr, 4)
	require.InDelta(t, 1.1, arr[0], 1e-10)
	require.InDelta(t, 2.2, arr[1], 1e-10)
	require.InDelta(t, 3.3, arr[2], 1e-10)
	require.InDelta(t, 4.4, arr[3], 1e-10)
}

func TestReadValue_FixedString(t *testing.T) {
	// Fixed-length string, null-terminated (padding type 0).
	attr := &Attribute{
		Name: "fixed_string_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          8,
			ClassBitField: 0x00, // null-terminated
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: []byte("hello\x00\x00\x00"),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, "hello", val)
}

func TestReadValue_FixedStringArray(t *testing.T) {
	// Array of fixed-length strings.
	attr := &Attribute{
		Name: "string_array_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          4,
			ClassBitField: 0x00, // null-terminated
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: []byte("abc\x00def\x00ghi\x00"),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def", "ghi"}, val)
}

func TestReadValue_FixedStringNullPadded(t *testing.T) {
	// Null-padded string (padding type 1).
	attr := &Attribute{
		Name: "nullpad_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          8,
			ClassBitField: 0x01, // null-padded
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: []byte("hi\x00\x00\x00\x00\x00\x00"),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, "hi", val)
}

func TestReadValue_FixedStringSpacePadded(t *testing.T) {
	// Space-padded string (padding type 2).
	attr := &Attribute{
		Name: "spacepad_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          8,
			ClassBitField: 0x02, // space-padded
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: []byte("hi      "),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, "hi", val)
}

func TestReadValue_ScalarInt(t *testing.T) {
	// Scalar int32 (rank 0, single element).
	attr := &Attribute{
		Name: "scalar_int",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: func() []byte {
			buf := make([]byte, 4)
			v := int32(-42)
			buf[0] = byte(v)
			buf[1] = byte(v >> 8)
			buf[2] = byte(v >> 16)
			buf[3] = byte(v >> 24)
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, int32(-42), val)
}

func TestReadValue_ScalarInt64(t *testing.T) {
	attr := &Attribute{
		Name: "scalar_int64",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: func() []byte {
			buf := make([]byte, 8)
			v := int64(-9999999)
			buf[0] = byte(v)
			buf[1] = byte(v >> 8)
			buf[2] = byte(v >> 16)
			buf[3] = byte(v >> 24)
			buf[4] = byte(v >> 32)
			buf[5] = byte(v >> 40)
			buf[6] = byte(v >> 48)
			buf[7] = byte(v >> 56)
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, int64(-9999999), val)
}

func TestReadValue_EmptyArray(t *testing.T) {
	// Dimension with 0 elements.
	attr := &Attribute{
		Name: "empty_array",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{0},
		},
		Data: []byte{},
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []interface{}{}, val)
}

func TestReadValue_SingleElementArray(t *testing.T) {
	// Array with dimensions [1] should return scalar value (isScalar = true).
	attr := &Attribute{
		Name: "single_element",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: func() []byte {
			buf := make([]byte, 4)
			binary.LittleEndian.PutUint32(buf, 77)
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	// Dimensions [1] triggers isScalar=true, returns single value.
	require.Equal(t, int32(77), val)
}

func TestReadValue_Int32DataTooShort(t *testing.T) {
	attr := &Attribute{
		Name: "short_data",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{5},
		},
		Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}, // Only 8 bytes, need 20
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute data size mismatch")
}

func TestReadValue_Int64DataTooShort(t *testing.T) {
	attr := &Attribute{
		Name: "short_data_int64",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}, // Only 8 bytes, need 24
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute data size mismatch")
}

func TestReadValue_Float32DataTooShort(t *testing.T) {
	attr := &Attribute{
		Name: "short_float32",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{10},
		},
		Data: []byte{1, 2, 3, 4}, // Only 4 bytes, need 40
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute data size mismatch")
}

func TestReadValue_Float64DataTooShort(t *testing.T) {
	attr := &Attribute{
		Name: "short_float64",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}, // Only 8 bytes, need 24
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute data size mismatch")
}

func TestReadValue_FixedStringDataTooShort(t *testing.T) {
	attr := &Attribute{
		Name: "short_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          10,
			ClassBitField: 0x00,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{5},
		},
		Data: []byte("hello\x00\x00\x00\x00\x00"), // Only 10 bytes, need 50
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "data too short for string element")
}

func TestReadValue_VarLenStringDataTooShort(t *testing.T) {
	// Variable-length string with data too short for references.
	mockReader := bytes.NewReader(make([]byte, 1024))

	attr := &Attribute{
		Name: "short_vlen",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			ClassBitField: 0x01, // variable-length string
			Size:          16,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{5},
		},
		Data:       make([]byte, 4), // Way too short for 5 vlen references
		reader:     mockReader,
		offsetSize: 8,
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute data size mismatch for vlen strings")
}

func TestReadValue_NullDataspace(t *testing.T) {
	attr := &Attribute{
		Name: "null_ds",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type: DataspaceNull,
		},
		Data: []byte{},
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []interface{}{}, val)
}

func TestReadValue_UnsupportedFloatSize(t *testing.T) {
	// Float with size 2 (half-precision) is not in the ReadValue switch.
	attr := &Attribute{
		Name: "half_float",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  2,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x00, 0x3C},
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported datatype class")
}

func TestReadValue_VariableStringNotFixed(t *testing.T) {
	// DatatypeString with Size=0 (which makes IsFixedString return false).
	attr := &Attribute{
		Name: "not_fixed_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          0,
			ClassBitField: 0x00,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x00},
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "variable-length strings not yet supported in attributes")
}

func TestReadValue_Int32LargeArray(t *testing.T) {
	// Test with a multi-element array, not triggering scalar path.
	n := 10
	data := make([]byte, n*4)
	expected := make([]int32, n)
	for i := 0; i < n; i++ {
		v := int32(i * 100)
		binary.LittleEndian.PutUint32(data[i*4:(i+1)*4], uint32(v))
		expected[i] = v
	}

	attr := &Attribute{
		Name: "large_int32_array",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{uint64(n)},
		},
		Data: data,
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, expected, val)
}

func TestReadValue_Int64Array(t *testing.T) {
	n := 5
	data := make([]byte, n*8)
	expected := make([]int64, n)
	for i := 0; i < n; i++ {
		v := int64(i * 1000)
		binary.LittleEndian.PutUint64(data[i*8:(i+1)*8], uint64(v))
		expected[i] = v
	}

	attr := &Attribute{
		Name: "int64_array",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{uint64(n)},
		},
		Data: data,
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, expected, val)
}

func TestReadValue_ScalarFloat64(t *testing.T) {
	attr := &Attribute{
		Name: "scalar_float64",
		Datatype: &DatatypeMessage{
			Class: DatatypeFloat,
			Size:  8,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data: func() []byte {
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, math.Float64bits(math.Pi))
			return buf
		}(),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.InDelta(t, math.Pi, val, 1e-15)
}

// ---------------------------------------------------------------------------
// Binary parsing tests for readBTreeV2HeaderRaw
// ---------------------------------------------------------------------------

func TestReadBTreeV2HeaderRaw_Valid(t *testing.T) {
	// Construct a valid BTHD header in memory.
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 38)
	offset := 0

	// Signature "BTHD"
	copy(buf[offset:], "BTHD")
	offset += 4

	// Version
	buf[offset] = 0
	offset++

	// Type (8 = attribute name index)
	buf[offset] = 8
	offset++

	// Node Size (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:], 4096)
	offset += 4

	// Record Size (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], 11)
	offset += 2

	// Depth (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], 0)
	offset += 2

	// Split % and Merge %
	buf[offset] = 75
	offset++
	buf[offset] = 40
	offset++

	// Root Node Address (8 bytes)
	binary.LittleEndian.PutUint64(buf[offset:], 0xABCD1234)
	offset += 8

	// Number of Records in Root (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], 5)
	offset += 2

	// Total Records (8 bytes)
	binary.LittleEndian.PutUint64(buf[offset:], 5)

	reader := bytes.NewReader(buf)

	header, err := readBTreeV2HeaderRaw(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, header)
	require.Equal(t, uint8(0), header.Version)
	require.Equal(t, uint8(8), header.Type)
	require.Equal(t, uint32(4096), header.NodeSize)
	require.Equal(t, uint16(11), header.RecordSize)
	require.Equal(t, uint16(0), header.Depth)
	require.Equal(t, uint64(0xABCD1234), header.RootNodeAddr)
	require.Equal(t, uint16(5), header.NumRecordsRoot)
	require.Equal(t, uint64(5), header.TotalRecords)
}

func TestReadBTreeV2HeaderRaw_InvalidSignature(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 38)
	copy(buf[0:4], "XXXX") // Wrong signature

	reader := bytes.NewReader(buf)

	_, err := readBTreeV2HeaderRaw(reader, 0, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid B-tree v2 signature")
}

func TestReadBTreeV2HeaderRaw_TooShort(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 10) // Too short
	copy(buf[0:4], "BTHD")

	reader := bytes.NewReader(buf)

	_, err := readBTreeV2HeaderRaw(reader, 0, sb)
	require.Error(t, err)
}

func TestReadBTreeV2HeaderRaw_4ByteOffsets(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 4,
		LengthSize: 4,
		Endianness: binary.LittleEndian,
	}

	// Build header with 4-byte offsets.
	// Size: 4(sig) + 1(ver) + 1(type) + 4(node size) + 2(record size) + 2(depth)
	//     + 1(split) + 1(merge) + 4(root addr) + 2(num records) + 8(total records) = 30 bytes
	buf := make([]byte, 38) // Extra space is fine
	offset := 0

	copy(buf[offset:], "BTHD")
	offset += 4
	buf[offset] = 0 // version
	offset++
	buf[offset] = 8 // type
	offset++
	binary.LittleEndian.PutUint32(buf[offset:], 2048)
	offset += 4
	binary.LittleEndian.PutUint16(buf[offset:], 11)
	offset += 2
	binary.LittleEndian.PutUint16(buf[offset:], 0)
	offset += 2
	buf[offset] = 75
	offset++
	buf[offset] = 40
	offset++
	binary.LittleEndian.PutUint32(buf[offset:], 0x5678)
	offset += 4
	binary.LittleEndian.PutUint16(buf[offset:], 3)
	offset += 2
	binary.LittleEndian.PutUint64(buf[offset:], 3)

	reader := bytes.NewReader(buf)

	header, err := readBTreeV2HeaderRaw(reader, 0, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0x5678), header.RootNodeAddr)
	require.Equal(t, uint16(3), header.NumRecordsRoot)
}

// ---------------------------------------------------------------------------
// Binary parsing tests for readBTreeV2LeafRecords
// ---------------------------------------------------------------------------

func TestReadBTreeV2LeafRecords_Valid(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	numRecords := uint16(3)
	// Each record: 4 bytes hash + 7 bytes heap ID = 11 bytes.
	// Header: 4 (sig) + 1 (ver) + 1 (type) = 6 bytes.
	// Checksum: 4 bytes.
	bufSize := 6 + int(numRecords)*11 + 4
	buf := make([]byte, bufSize)

	offset := 0
	copy(buf[offset:], "BTLF")
	offset += 4
	buf[offset] = 0 // version
	offset++
	buf[offset] = 8 // type
	offset++

	// Record 0
	binary.LittleEndian.PutUint32(buf[offset:], 0x11111111) // hash
	offset += 4
	heapID0 := [7]byte{0x00, 0x10, 0x00, 0x20, 0x00, 0x00, 0x00}
	copy(buf[offset:offset+7], heapID0[:])
	offset += 7

	// Record 1
	binary.LittleEndian.PutUint32(buf[offset:], 0x22222222)
	offset += 4
	heapID1 := [7]byte{0x00, 0x30, 0x00, 0x40, 0x00, 0x00, 0x00}
	copy(buf[offset:offset+7], heapID1[:])
	offset += 7

	// Record 2
	binary.LittleEndian.PutUint32(buf[offset:], 0x33333333)
	offset += 4
	heapID2 := [7]byte{0x00, 0x50, 0x00, 0x60, 0x00, 0x00, 0x00}
	copy(buf[offset:offset+7], heapID2[:])

	reader := bytes.NewReader(buf)

	heapIDs, err := readBTreeV2LeafRecords(reader, 0, numRecords, sb)
	require.NoError(t, err)
	require.Len(t, heapIDs, 3)
	require.Equal(t, heapID0, heapIDs[0])
	require.Equal(t, heapID1, heapIDs[1])
	require.Equal(t, heapID2, heapIDs[2])
}

func TestReadBTreeV2LeafRecords_InvalidSignature(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 40)
	copy(buf[0:4], "XXXX")

	reader := bytes.NewReader(buf)

	_, err := readBTreeV2LeafRecords(reader, 0, 1, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid B-tree v2 leaf signature")
}

func TestReadBTreeV2LeafRecords_ZeroRecords(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	bufSize := 6 + 4 // header + checksum, no records
	buf := make([]byte, bufSize)
	copy(buf[0:4], "BTLF")
	buf[4] = 0 // version
	buf[5] = 8 // type

	reader := bytes.NewReader(buf)

	heapIDs, err := readBTreeV2LeafRecords(reader, 0, 0, sb)
	require.NoError(t, err)
	require.Len(t, heapIDs, 0)
}

func TestReadBTreeV2LeafRecords_TooShort(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 5) // Too short
	reader := bytes.NewReader(buf)

	_, err := readBTreeV2LeafRecords(reader, 0, 1, sb)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Binary parsing tests for readFractalHeapHeaderRaw
// ---------------------------------------------------------------------------

func TestReadFractalHeapHeaderRaw_Valid(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 144)

	// Signature "FRHP"
	copy(buf[0:4], "FRHP")

	// Version
	buf[4] = 0

	// Heap ID Length (2 bytes)
	binary.LittleEndian.PutUint16(buf[5:7], 7)

	// I/O Filters Encoded Length (2 bytes)
	binary.LittleEndian.PutUint16(buf[7:9], 0)

	// Flags (1 byte)
	buf[9] = 0x00 // no checksum for direct blocks

	// Max Managed Object Size (4 bytes) at offset 10
	binary.LittleEndian.PutUint32(buf[10:14], 512)

	// At offset 110: Table Width (2 bytes)
	binary.LittleEndian.PutUint16(buf[110:112], 4)

	// Starting Block Size (LengthSize=8 bytes) at offset 112
	binary.LittleEndian.PutUint64(buf[112:120], 4096)

	// Max Direct Block Size (LengthSize=8 bytes) at offset 120
	binary.LittleEndian.PutUint64(buf[120:128], 65536)

	// Max Heap Size (2 bytes) at offset 128
	binary.LittleEndian.PutUint16(buf[128:130], 32)

	// Root Block Address at offset 132 (8 bytes)
	binary.LittleEndian.PutUint64(buf[132:140], 0xDEADBEEF)

	reader := bytes.NewReader(buf)

	header, err := readFractalHeapHeaderRaw(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, header)
	require.Equal(t, uint8(0), header.Version)
	require.Equal(t, uint16(7), header.HeapIDLen)
	require.Equal(t, uint32(512), header.MaxManagedObjSize)
	require.Equal(t, uint64(65536), header.MaxDirectBlockSize)
	require.Equal(t, uint16(32), header.MaxHeapSize)
	require.Equal(t, uint64(0xDEADBEEF), header.RootBlockAddress)
	require.False(t, header.ChecksumDirBlocks)

	// HeapOffsetSize = ceil(32/8) = 4
	require.Equal(t, uint8(4), header.HeapOffsetSize)
}

func TestReadFractalHeapHeaderRaw_InvalidSignature(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 144)
	copy(buf[0:4], "XXXX")

	reader := bytes.NewReader(buf)

	_, err := readFractalHeapHeaderRaw(reader, 0, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid fractal heap signature")
}

func TestReadFractalHeapHeaderRaw_TooShort(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 10) // Too short
	copy(buf[0:4], "FRHP")

	reader := bytes.NewReader(buf)

	_, err := readFractalHeapHeaderRaw(reader, 0, sb)
	require.Error(t, err)
}

func TestReadFractalHeapHeaderRaw_WithChecksumFlag(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, 144)
	copy(buf[0:4], "FRHP")
	buf[4] = 0
	binary.LittleEndian.PutUint16(buf[5:7], 7)
	binary.LittleEndian.PutUint16(buf[7:9], 0)
	buf[9] = 0x02 // Checksum flag set
	binary.LittleEndian.PutUint32(buf[10:14], 256)
	binary.LittleEndian.PutUint16(buf[110:112], 4)
	binary.LittleEndian.PutUint64(buf[112:120], 1024)
	binary.LittleEndian.PutUint64(buf[120:128], 32768)
	binary.LittleEndian.PutUint16(buf[128:130], 24)
	binary.LittleEndian.PutUint64(buf[132:140], 0x1234)

	reader := bytes.NewReader(buf)

	header, err := readFractalHeapHeaderRaw(reader, 0, sb)
	require.NoError(t, err)
	require.True(t, header.ChecksumDirBlocks)
}

// ---------------------------------------------------------------------------
// Binary parsing tests for readHeapObject
// ---------------------------------------------------------------------------

func TestReadHeapObject_Valid(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	header := &fractalHeapHeaderRaw{
		HeapOffsetSize:    2,
		HeapLengthSize:    2,
		ChecksumDirBlocks: false,
	}

	// Construct a minimal FHDB direct block.
	// Header: "FHDB" (4) + version (1) + heap header addr (8) + block offset (2) = 15 bytes
	// Then the object data at offset 0 within the block.
	headerSize := 4 + 1 + 8 + 2
	objectData := []byte("hello, heap object data!")
	buf := make([]byte, headerSize+len(objectData)+16)

	offset := 0
	copy(buf[offset:], "FHDB")
	offset += 4
	buf[offset] = 0 // version
	offset++
	// Heap header address (8 bytes)
	binary.LittleEndian.PutUint64(buf[offset:], 0x1000)
	offset += 8
	// Block offset in heap space (2 bytes = HeapOffsetSize)
	binary.LittleEndian.PutUint16(buf[offset:], 0) // block starts at heap offset 0
	offset += 2
	// Object data starts here
	copy(buf[offset:], objectData)

	reader := bytes.NewReader(buf)

	// Read object at heap offset 0, length = len(objectData).
	result, err := readHeapObject(reader, 0, 0, uint64(len(objectData)), sb, header)
	require.NoError(t, err)
	require.Equal(t, objectData, result)
}

func TestReadHeapObject_WithOffset(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	header := &fractalHeapHeaderRaw{
		HeapOffsetSize:    2,
		HeapLengthSize:    2,
		ChecksumDirBlocks: false,
	}

	// Direct block with block offset = 0, object at relative offset 5.
	headerSize := 4 + 1 + 8 + 2
	padding := make([]byte, 5)   // 5 bytes before the object
	objectData := []byte("data") // 4 bytes of data
	buf := make([]byte, headerSize+len(padding)+len(objectData)+16)

	offset := 0
	copy(buf[offset:], "FHDB")
	offset += 4
	buf[offset] = 0
	offset++
	binary.LittleEndian.PutUint64(buf[offset:], 0x1000)
	offset += 8
	binary.LittleEndian.PutUint16(buf[offset:], 0) // block offset = 0
	offset += 2
	copy(buf[offset:], padding)
	offset += len(padding)
	copy(buf[offset:], objectData)

	reader := bytes.NewReader(buf)

	// Object is at heap offset 5, length 4.
	result, err := readHeapObject(reader, 0, 5, 4, sb, header)
	require.NoError(t, err)
	require.Equal(t, objectData, result)
}

func TestReadHeapObject_InvalidSignature(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	header := &fractalHeapHeaderRaw{
		HeapOffsetSize: 2,
		HeapLengthSize: 2,
	}

	buf := make([]byte, 64)
	copy(buf[0:4], "XXXX") // Wrong signature

	reader := bytes.NewReader(buf)

	_, err := readHeapObject(reader, 0, 0, 10, sb, header)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid direct block signature")
}

func TestReadHeapObject_OffsetBeforeBlock(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	header := &fractalHeapHeaderRaw{
		HeapOffsetSize: 2,
		HeapLengthSize: 2,
	}

	headerSize := 4 + 1 + 8 + 2
	buf := make([]byte, headerSize+64)
	copy(buf[0:4], "FHDB")
	buf[4] = 0
	binary.LittleEndian.PutUint64(buf[5:13], 0x1000)
	binary.LittleEndian.PutUint16(buf[13:15], 100) // Block starts at heap offset 100

	reader := bytes.NewReader(buf)

	// Object offset 50 is before block offset 100 -- should fail.
	_, err := readHeapObject(reader, 0, 50, 10, sb, header)
	require.Error(t, err)
	require.Contains(t, err.Error(), "object offset")
}

// ---------------------------------------------------------------------------
// Tests for computeOffsetSize
// ---------------------------------------------------------------------------

func TestComputeOffsetSize_ExtendedCases(t *testing.T) {
	tests := []struct {
		value uint64
		want  uint8
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{1 << 24, 4},
		{1 << 32, 5},
		{1<<64 - 1, 8},
	}

	for _, tt := range tests {
		got := computeOffsetSize(tt.value)
		require.Equal(t, tt.want, got, "computeOffsetSize(%d)", tt.value)
	}
}

// ---------------------------------------------------------------------------
// Tests for readDenseAttributes end-to-end with mock data
// ---------------------------------------------------------------------------

func TestReadDenseAttributes_EmptyBTreeLeaf(t *testing.T) {
	// Build an in-memory "file" with a valid BTHD + BTLF with 0 records.
	// This exercises the "no heap IDs" path returning empty attributes.

	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Layout in memory:
	// offset 0x0100: BTHD header (38 bytes)
	// offset 0x0200: BTLF leaf (10 bytes = 6 header + 4 checksum, 0 records)
	// offset 0x0300: FRHP header (144 bytes) -- won't be read since 0 records

	buf := make([]byte, 0x0500)

	// --- BTHD at 0x0100 ---
	bthdOffset := 0x0100
	copy(buf[bthdOffset:], "BTHD")
	buf[bthdOffset+4] = 0 // version
	buf[bthdOffset+5] = 8 // type
	binary.LittleEndian.PutUint32(buf[bthdOffset+6:], 4096)
	binary.LittleEndian.PutUint16(buf[bthdOffset+10:], 11)
	binary.LittleEndian.PutUint16(buf[bthdOffset+12:], 0) // depth
	buf[bthdOffset+14] = 75
	buf[bthdOffset+15] = 40
	binary.LittleEndian.PutUint64(buf[bthdOffset+16:], 0x0200) // root node at BTLF
	binary.LittleEndian.PutUint16(buf[bthdOffset+24:], 0)      // 0 records in root
	binary.LittleEndian.PutUint64(buf[bthdOffset+26:], 0)      // 0 total records

	// --- BTLF at 0x0200 ---
	btlfOffset := 0x0200
	copy(buf[btlfOffset:], "BTLF")
	buf[btlfOffset+4] = 0 // version
	buf[btlfOffset+5] = 8 // type
	// No records, just checksum space (4 bytes)

	reader := bytes.NewReader(buf)

	attrInfo := &AttributeInfoMessage{
		FractalHeapAddr:    0x0300, // Not actually read since 0 records
		BTreeNameIndexAddr: 0x0100, // Points to BTHD
	}

	attrs, err := readDenseAttributes(reader, attrInfo, sb)
	require.NoError(t, err)
	require.NotNil(t, attrs)
	require.Len(t, attrs, 0)
}

// ---------------------------------------------------------------------------
// Tests for ParseAttributesFromMessages
// ---------------------------------------------------------------------------

func TestParseAttributesFromMessages_CompactOnly(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Build a minimal attribute message (version 3, scalar int32 "myattr" = 42).
	attrData := buildV3AttrMessage(t, "myattr", 42)

	messages := []*HeaderMessage{
		{
			Type: MsgAttribute,
			Data: attrData,
		},
	}

	reader := bytes.NewReader(make([]byte, 1024))

	attrs, err := ParseAttributesFromMessages(reader, messages, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "myattr", attrs[0].Name)
}

func TestParseAttributesFromMessages_SkipBadAttribute(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	goodData := buildV3AttrMessage(t, "good", 99)

	messages := []*HeaderMessage{
		{
			Type: MsgAttribute,
			Data: []byte{0xFF}, // Bad attribute message (too short)
		},
		{
			Type: MsgAttribute,
			Data: goodData,
		},
	}

	reader := bytes.NewReader(make([]byte, 1024))

	attrs, err := ParseAttributesFromMessages(reader, messages, sb)
	require.NoError(t, err)
	// Should skip bad attribute and return the good one.
	require.Len(t, attrs, 1)
	require.Equal(t, "good", attrs[0].Name)
}

func TestParseAttributesFromMessages_IgnoresNonAttributeMessages(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	attrData := buildV3AttrMessage(t, "attr1", 10)

	messages := []*HeaderMessage{
		{
			Type: 0x0001, // Dataspace message, not attribute
			Data: []byte{1, 0, 0, 0},
		},
		{
			Type: MsgAttribute,
			Data: attrData,
		},
		{
			Type: 0x0003, // Datatype message, not attribute
			Data: []byte{1, 0, 0, 0},
		},
	}

	reader := bytes.NewReader(make([]byte, 1024))

	attrs, err := ParseAttributesFromMessages(reader, messages, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
}

// ---------------------------------------------------------------------------
// Helper: build a Version 3 attribute message for int32 scalar
// ---------------------------------------------------------------------------

func buildV3AttrMessage(t *testing.T, name string, value int32) []byte {
	t.Helper()

	nameBytes := append([]byte(name), 0) // null-terminated

	// Datatype: int32 (class 0, size 4)
	dtBytes := make([]byte, 8)
	dtBytes[0] = 0x30 // class=0 (fixed), version=3
	binary.LittleEndian.PutUint32(dtBytes[4:8], 4)

	// Dataspace: scalar (version 1, dimensionality 0)
	dsBytes := make([]byte, 8)
	dsBytes[0] = 1 // version
	dsBytes[1] = 0 // dimensionality = 0 (scalar)

	// Value: int32
	valBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(valBytes, uint32(value))

	// Build the full message
	// Version 3 format:
	//   version(1) + flags(1) + nameSize(2) + dtSize(2) + dsSize(2) + encoding(1)
	//   + name + datatype + dataspace + data
	headerSize := 9 // 1+1+2+2+2+1
	totalSize := headerSize + len(nameBytes) + len(dtBytes) + len(dsBytes) + len(valBytes)

	data := make([]byte, totalSize)
	offset := 0

	data[offset] = 3 // version
	offset++
	data[offset] = 0 // flags
	offset++
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(nameBytes)))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(dtBytes)))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(dsBytes)))
	offset += 2
	data[offset] = 0 // name encoding (ASCII)
	offset++
	copy(data[offset:], nameBytes)
	offset += len(nameBytes)
	copy(data[offset:], dtBytes)
	offset += len(dtBytes)
	copy(data[offset:], dsBytes)
	offset += len(dsBytes)
	copy(data[offset:], valBytes)

	return data
}
