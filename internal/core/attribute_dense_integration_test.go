package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// ---------------------------------------------------------------------------
// ReadValue tests – additional datatype varieties
// ---------------------------------------------------------------------------

func TestReadValue_Int8(t *testing.T) {
	// 1-byte integers read as int8 (TASK-068; they were "unsupported" before).
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

	v, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []int8{1, 2, 3}, v)
}

func TestReadValue_Int16(t *testing.T) {
	// 2-byte integers read as int16 (TASK-068; they were "unsupported" before).
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

	v, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []int16{1, 2}, v)
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
	// ADR-003: the size guard rejects the claimed 50 bytes before the element
	// slice is allocated; the per-element loop is no longer reached for this input.
	require.Contains(t, err.Error(), "attribute data size mismatch")
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

// The tests of readBTreeV2HeaderRaw and readBTreeV2LeafRecords were removed
// with those readers (TASK-058): dense storage indexes are read with the
// checked v2 B-tree reader, tested in dense_heap_test.go and bt2_reader's
// chunk index tests.

// ---------------------------------------------------------------------------
// Binary parsing tests for the fractal heap header (OpenFractalHeap)
// ---------------------------------------------------------------------------

// TestOpenFractalHeap_Valid decodes a header field by field. TASK-058: was
// TestReadFractalHeapHeaderRaw_Valid, on 144 bytes without the current root
// row count and the checksum, both of which the header has (H5HFcache.c:505,
// :545-560).
func TestOpenFractalHeap_Valid(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf := make([]byte, fheapTestHeaderSize)

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
	resealFheap(buf)

	reader := bytes.NewReader(buf)

	h, err := OpenFractalHeap(reader, sb, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(7), h.idLen)
	require.Equal(t, uint64(512), h.maxManSize)
	require.Equal(t, uint64(65536), h.maxDirect)
	require.Equal(t, uint64(0xDEADBEEF), h.rootAddr)
	require.False(t, h.checksumDBlocks)
	require.Equal(t, 4, h.offSize, "ceil(32/8)")
	require.Equal(t, 2, h.lenSize, "min(ceil(16/8), 9/8+1)")
}

// TestOpenFractalHeap_ChecksumFlag: flag bit 1 makes direct blocks
// checksummed (H5HF_HDR_FLAGS_CHECKSUM_DBLOCKS), 4 more header bytes.
func TestOpenFractalHeap_ChecksumFlag(t *testing.T) {
	s := defaultFheapSpec()
	s.flags = fheapFlagChecksumDBs
	h, err := OpenFractalHeap(bytes.NewReader(s.bytes()), fheapTestSB, 0)
	require.NoError(t, err)
	require.True(t, h.checksumDBlocks)
	require.Equal(t, uint64(fheapTestOverhead+fheapChecksumSize), h.dblockOverhead)
}

// ---------------------------------------------------------------------------
// Tests for readDenseAttributes end-to-end with mock data
// ---------------------------------------------------------------------------

// TestReadDenseAttributes_EmptyBTreeLeaf: an empty name index lists no
// attributes. TASK-058: an empty v2 B-tree has no root node, its address
// undefined (H5B2hdr.c:245); this test used to point the header at an empty,
// unchecksummed leaf, which the checked reader refuses as C does (a node holds
// at least one record).
func TestReadDenseAttributes_EmptyBTreeLeaf(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	buf, attrInfo := buildDenseAttributeStorage(nil)

	attrs, err := readDenseAttributes(bytes.NewReader(buf), attrInfo, sb)
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

// TestParseAttributesFromMessages_ReportsBadAttribute: a corrupt compact
// attribute is an error, as in C, where H5O__attr_decode fails and so does
// the attribute access (H5Oattr.c:113-300). It used to be skipped silently,
// which hid the corruption and returned a short attribute list
// (review-068 finding 5).
func TestParseAttributesFromMessages_ReportsBadAttribute(t *testing.T) {
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
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Nil(t, attrs)
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

	// Datatype: int32 as the codec (and the C library) encodes it; the
	// 8-byte header without bit offset and precision that stood here is
	// rejected by the C decoder (H5Odtype.c:175-176).
	dtBytes := testInt32DatatypeBytes(t)

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
