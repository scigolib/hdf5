package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadDatasetCompound_CompactLayout tests reading a compound dataset stored with compact layout.
// This exercises ReadDatasetCompound with a fully in-memory compound datatype and layout,
// covering the compact code path and the full parse pipeline.
func TestReadDatasetCompound_CompactLayout(t *testing.T) {
	// Build a version 3 compound datatype with 2 members:
	//   { int32 id; float64 value; } = 12 bytes per element (4 + 8)
	//
	// Compound v3 format:
	//   4 bytes: member count
	//   For each member:
	//     - null-terminated name (NOT padded in v3)
	//     - 4 bytes: member byte offset
	//     - 8+ bytes: member datatype message (class|version, size, [properties])

	compoundProps := buildCompoundV3Props(t, []testCompoundMember{
		{name: "id", offset: 0, dtClass: DatatypeFixed, dtSize: 4},
		{name: "value", offset: 4, dtClass: DatatypeFloat, dtSize: 8},
	})

	// Compound datatype header: class=6 (compound), version=3, member count in properties.
	compoundDtMsg := buildCompoundDatatypeV3(12, compoundProps)

	// Build dataspace: 1D with 2 elements.
	dataspaceMsg := buildDataspaceV1Message([]uint64{2})

	// Build compact layout with the raw compound data inline.
	// Element 0: id=42, value=3.14
	// Element 1: id=99, value=2.71
	rawCompound := make([]byte, 24) // 2 elements * 12 bytes
	binary.LittleEndian.PutUint32(rawCompound[0:4], 42)
	binary.LittleEndian.PutUint64(rawCompound[4:12], math.Float64bits(3.14))
	binary.LittleEndian.PutUint32(rawCompound[12:16], 99)
	binary.LittleEndian.PutUint64(rawCompound[16:24], math.Float64bits(2.71))

	layoutMsg := buildCompactLayoutMessage(rawCompound)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: compoundDtMsg},
			{Type: MsgDataspace, Data: dataspaceMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetCompound(bytes.NewReader(rawCompound), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 2)

	require.Equal(t, int32(42), data[0]["id"])
	require.InDelta(t, 3.14, data[0]["value"].(float64), 1e-10)
	require.Equal(t, int32(99), data[1]["id"])
	require.InDelta(t, 2.71, data[1]["value"].(float64), 1e-10)
}

// TestReadDatasetCompound_Int64Members tests compound dataset reading with int64 fields.
func TestReadDatasetCompound_Int64Members(t *testing.T) {
	compoundProps := buildCompoundV3Props(t, []testCompoundMember{
		{name: "x", offset: 0, dtClass: DatatypeFixed, dtSize: 8},
		{name: "y", offset: 8, dtClass: DatatypeFixed, dtSize: 8},
	})

	compoundDtMsg := buildCompoundDatatypeV3(16, compoundProps)
	dataspaceMsg := buildDataspaceV1Message([]uint64{3})

	// 3 elements, each 16 bytes (two int64s).
	rawCompound := make([]byte, 48)
	binary.LittleEndian.PutUint64(rawCompound[0:8], uint64(100))
	binary.LittleEndian.PutUint64(rawCompound[8:16], uint64(200))
	binary.LittleEndian.PutUint64(rawCompound[16:24], uint64(300))
	binary.LittleEndian.PutUint64(rawCompound[24:32], uint64(400))
	binary.LittleEndian.PutUint64(rawCompound[32:40], uint64(500))
	binary.LittleEndian.PutUint64(rawCompound[40:48], uint64(600))

	layoutMsg := buildCompactLayoutMessage(rawCompound)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: compoundDtMsg},
			{Type: MsgDataspace, Data: dataspaceMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetCompound(bytes.NewReader(rawCompound), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 3)

	require.Equal(t, int64(100), data[0]["x"])
	require.Equal(t, int64(200), data[0]["y"])
	require.Equal(t, int64(300), data[1]["x"])
	require.Equal(t, int64(400), data[1]["y"])
	require.Equal(t, int64(500), data[2]["x"])
	require.Equal(t, int64(600), data[2]["y"])
}

// TestReadDatasetCompound_EmptyDataset_V3 tests reading a compound dataset with zero elements.
func TestReadDatasetCompound_EmptyDataset_V3(t *testing.T) {
	compoundProps := buildCompoundV3Props(t, []testCompoundMember{
		{name: "a", offset: 0, dtClass: DatatypeFixed, dtSize: 4},
	})

	compoundDtMsg := buildCompoundDatatypeV3(4, compoundProps)
	dataspaceMsg := buildDataspaceV1Message([]uint64{0})
	layoutMsg := buildCompactLayoutMessage([]byte{})

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: compoundDtMsg},
			{Type: MsgDataspace, Data: dataspaceMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetCompound(bytes.NewReader(nil), header, sb)
	require.NoError(t, err)
	require.Empty(t, data)
}

// TestReadDatasetCompound_UnsupportedLayoutClass tests error on unknown layout class.
func TestReadDatasetCompound_UnsupportedLayoutClass(t *testing.T) {
	compoundProps := buildCompoundV3Props(t, []testCompoundMember{
		{name: "a", offset: 0, dtClass: DatatypeFixed, dtSize: 4},
	})

	compoundDtMsg := buildCompoundDatatypeV3(4, compoundProps)
	dataspaceMsg := buildDataspaceV1Message([]uint64{2})

	// Build a layout with unsupported class (99).
	layoutMsg := make([]byte, 4)
	layoutMsg[0] = 3  // version 3
	layoutMsg[1] = 99 // invalid class
	layoutMsg[2] = 0
	layoutMsg[3] = 0

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: compoundDtMsg},
			{Type: MsgDataspace, Data: dataspaceMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := ReadDatasetCompound(bytes.NewReader(nil), header, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported layout class")
}

// TestParseMemberValue_Float64 tests parsing a float64 member value.
func TestParseMemberValue_Float64(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFloat, Size: 8, ClassBitField: 0}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, math.Float64bits(3.14))

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.InDelta(t, 3.14, val.(float64), 1e-15)
}

// TestParseMemberValue_Float64_BigEndian tests parsing a big-endian float64.
func TestParseMemberValue_Float64_BigEndian(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFloat, Size: 8, ClassBitField: 1} // bit 0 = 1 -> big-endian
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, math.Float64bits(2.718))

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.InDelta(t, 2.718, val.(float64), 1e-15)
}

// TestParseMemberValue_Float32 tests parsing a float32 member value.
func TestParseMemberValue_Float32(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFloat, Size: 4, ClassBitField: 0}
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, math.Float32bits(1.5))

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, float32(1.5), val.(float32))
}

// TestParseMemberValue_Float32_InsufficientData tests error on truncated float32 data.
func TestParseMemberValue_Float32_InsufficientData(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFloat, Size: 4, ClassBitField: 0}

	_, err := parseMemberValue([]byte{0x00, 0x00}, dt, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient data for float32")
}

// TestParseMemberValue_Int32 tests parsing an int32 member value.
func TestParseMemberValue_Int32(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 4, ClassBitField: 0}
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(42))

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int32(42), val.(int32))
}

// TestParseMemberValue_Int32_Negative tests parsing a negative int32.
func TestParseMemberValue_Int32_Negative(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 4, ClassBitField: 0}
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(0xFFFFFFFF)) // -1

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int32(-1), val.(int32))
}

// TestParseMemberValue_Int64 tests parsing an int64 member value.
func TestParseMemberValue_Int64(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 8, ClassBitField: 0}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(1234567890123))

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1234567890123), val.(int64))
}

// TestParseMemberValue_Int64_InsufficientData tests error on truncated int64.
func TestParseMemberValue_Int64_InsufficientData(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 8, ClassBitField: 0}

	_, err := parseMemberValue([]byte{0x00, 0x00, 0x00, 0x00}, dt, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient data for int64")
}

// TestParseMemberValue_FixedString tests parsing a fixed-length string member.
func TestParseMemberValue_FixedString(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          10,
		ClassBitField: 0, // null-terminated
	}
	data := []byte("hello\x00\x00\x00\x00\x00")

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "hello", val.(string))
}

// TestParseMemberValue_FixedString_SpacePadded tests space-padded string parsing.
func TestParseMemberValue_FixedString_SpacePadded(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          8,
		ClassBitField: 2, // space-padded
	}
	data := []byte("test    ")

	val, err := parseMemberValue(data, dt, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "test", val.(string))
}

// TestParseMemberValue_FixedString_InsufficientData tests error on truncated string data.
func TestParseMemberValue_FixedString_InsufficientData(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          16,
		ClassBitField: 0,
	}

	_, err := parseMemberValue([]byte("short"), dt, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient data for string")
}

// TestParseMemberValue_UnsupportedType tests error on unsupported datatype.
func TestParseMemberValue_UnsupportedType(t *testing.T) {
	dt := &DatatypeMessage{
		Class: DatatypeBitfield, // Not handled in parseMemberValue
		Size:  4,
	}
	data := make([]byte, 4)

	_, err := parseMemberValue(data, dt, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported member datatype")
}

// TestParseMemberValue_VariableString_NullRef tests variable-string with zero heap address.
func TestParseMemberValue_VariableString_NullRef(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Size:          16,
		ClassBitField: 0x01, // type=1 (string)
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Global heap reference: 8 bytes address (all zeros) + 4 bytes index.
	data := make([]byte, 12)

	val, err := parseMemberValue(data, dt, bytes.NewReader(data), sb)
	require.NoError(t, err)
	require.Equal(t, "", val.(string))
}

// TestReadDatasetFloat64_CompactLayout tests ReadDatasetFloat64 using compact layout.
func TestReadDatasetFloat64_CompactLayout(t *testing.T) {
	// Build float64 datatype.
	dtMsg := buildFloat64DatatypeMessage()

	// Build dataspace: 1D with 3 elements.
	dsMsg := buildDataspaceV1Message([]uint64{3})

	// Build compact layout with inline float64 data: [1.0, 2.0, 3.0].
	rawData := make([]byte, 24) // 3 * 8 bytes
	binary.LittleEndian.PutUint64(rawData[0:8], math.Float64bits(1.0))
	binary.LittleEndian.PutUint64(rawData[8:16], math.Float64bits(2.0))
	binary.LittleEndian.PutUint64(rawData[16:24], math.Float64bits(3.0))
	layoutMsg := buildCompactLayoutMessage(rawData)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtMsg},
			{Type: MsgDataspace, Data: dsMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetFloat64(bytes.NewReader(rawData), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 3)
	require.Equal(t, 1.0, data[0])
	require.Equal(t, 2.0, data[1])
	require.Equal(t, 3.0, data[2])
}

// TestReadDatasetFloat64_Int32CompactLayout tests reading int32 dataset as float64 via compact layout.
func TestReadDatasetFloat64_Int32CompactLayout(t *testing.T) {
	// Build int32 datatype.
	dtData := make([]byte, 8)
	classAndVersion := uint32(DatatypeFixed) | (1 << 4)
	binary.LittleEndian.PutUint32(dtData[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(dtData[4:8], 4)

	dsMsg := buildDataspaceV1Message([]uint64{4})

	rawData := make([]byte, 16) // 4 * 4 bytes
	binary.LittleEndian.PutUint32(rawData[0:4], 10)
	binary.LittleEndian.PutUint32(rawData[4:8], 20)
	binary.LittleEndian.PutUint32(rawData[8:12], 30)
	binary.LittleEndian.PutUint32(rawData[12:16], 40)
	layoutMsg := buildCompactLayoutMessage(rawData)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtData},
			{Type: MsgDataspace, Data: dsMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetFloat64(bytes.NewReader(rawData), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 4)
	require.Equal(t, 10.0, data[0])
	require.Equal(t, 20.0, data[1])
	require.Equal(t, 30.0, data[2])
	require.Equal(t, 40.0, data[3])
}

// TestReadDatasetFloat64_ScalarDataset tests reading a scalar (0-dim) float64 dataset.
func TestReadDatasetFloat64_ScalarDataset(t *testing.T) {
	dtMsg := buildFloat64DatatypeMessage()

	// Build scalar dataspace: version=1, ndims=0, type=scalar.
	// Scalar dataspace with 0 dims produces TotalElements=1 only if Type=Scalar.
	// With ndims=0, ParseDataspaceMessage sets Type based on version.
	dsMsg := buildScalarDataspaceMessage()

	rawData := make([]byte, 8)
	binary.LittleEndian.PutUint64(rawData[0:8], math.Float64bits(42.5))
	layoutMsg := buildCompactLayoutMessage(rawData)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtMsg},
			{Type: MsgDataspace, Data: dsMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetFloat64(bytes.NewReader(rawData), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.Equal(t, 42.5, data[0])
}

// TestReadDatasetFloat64_UnsupportedLayoutClass tests error on unknown layout class.
func TestReadDatasetFloat64_UnsupportedLayoutClass(t *testing.T) {
	dtMsg := buildFloat64DatatypeMessage()
	dsMsg := buildDataspaceV1Message([]uint64{2})

	layoutMsg := make([]byte, 4)
	layoutMsg[0] = 3  // version 3
	layoutMsg[1] = 99 // invalid class

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtMsg},
			{Type: MsgDataspace, Data: dsMsg},
			{Type: MsgDataLayout, Data: layoutMsg},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := ReadDatasetFloat64(bytes.NewReader(nil), header, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported layout class")
}

// TestParseCompoundData_SingleElement tests parsing raw bytes for a single compound element.
func TestParseCompoundData_SingleElement(t *testing.T) {
	ct := &CompoundType{
		Size: 12,
		Members: []CompoundMember{
			{Name: "id", Offset: 0, Type: &DatatypeMessage{Class: DatatypeFixed, Size: 4}},
			{Name: "val", Offset: 4, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
		},
	}

	rawData := make([]byte, 12)
	binary.LittleEndian.PutUint32(rawData[0:4], 7)
	binary.LittleEndian.PutUint64(rawData[4:12], math.Float64bits(9.81))

	result, err := parseCompoundData(rawData, ct, 1, bytes.NewReader(rawData), &Superblock{OffsetSize: 8})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, int32(7), result[0]["id"])
	require.InDelta(t, 9.81, result[0]["val"].(float64), 1e-10)
}

// TestParseCompoundData_MultipleElements tests parsing multiple compound elements.
func TestParseCompoundData_MultipleElements(t *testing.T) {
	ct := &CompoundType{
		Size: 16,
		Members: []CompoundMember{
			{Name: "x", Offset: 0, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
			{Name: "y", Offset: 8, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
		},
	}

	rawData := make([]byte, 32)
	binary.LittleEndian.PutUint64(rawData[0:8], math.Float64bits(1.1))
	binary.LittleEndian.PutUint64(rawData[8:16], math.Float64bits(2.2))
	binary.LittleEndian.PutUint64(rawData[16:24], math.Float64bits(3.3))
	binary.LittleEndian.PutUint64(rawData[24:32], math.Float64bits(4.4))

	result, err := parseCompoundData(rawData, ct, 2, bytes.NewReader(rawData), &Superblock{OffsetSize: 8})
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.InDelta(t, 1.1, result[0]["x"].(float64), 1e-10)
	require.InDelta(t, 2.2, result[0]["y"].(float64), 1e-10)
	require.InDelta(t, 3.3, result[1]["x"].(float64), 1e-10)
	require.InDelta(t, 4.4, result[1]["y"].(float64), 1e-10)
}

// TestReadVariableString_NullHeapAddress tests readVariableString with zero heap address.
func TestReadVariableString_NullHeapAddress(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Global heap reference: 8 bytes address (zeros) + 4 bytes object index.
	data := make([]byte, 12)

	result, err := readVariableString(bytes.NewReader(data), data, sb)
	require.NoError(t, err)
	require.Equal(t, "", result)
}

// TestReadVariableString_InvalidOffsetSize tests error for invalid offset size.
func TestReadVariableString_InvalidOffsetSize(t *testing.T) {
	sb := &Superblock{OffsetSize: 3, LengthSize: 8, Endianness: binary.LittleEndian} // offset size 3 is invalid

	data := make([]byte, 12)

	_, err := readVariableString(bytes.NewReader(data), data, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid offset size")
}

// TestReadVariableString_InsufficientData tests error for truncated heap reference.
func TestReadVariableString_InsufficientData(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Only 4 bytes, need 12 (8 + 4).
	data := make([]byte, 4)

	_, err := readVariableString(bytes.NewReader(data), data, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient data")
}

// --- Helper types and functions ---

type testCompoundMember struct {
	name    string
	offset  uint32
	dtClass DatatypeClass
	dtSize  uint32
}

// buildCompoundV3Props builds compound v3 properties from test member definitions.
func buildCompoundV3Props(t *testing.T, members []testCompoundMember) []byte {
	t.Helper()
	buf := make([]byte, 0, 4+len(members)*32)

	// 4 bytes: member count.
	countBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBuf, uint32(len(members)))
	buf = append(buf, countBuf...)

	for _, m := range members {
		// Null-terminated name (NOT padded in v3).
		buf = append(buf, []byte(m.name)...)
		buf = append(buf, 0)

		// 4 bytes: member byte offset.
		offsetBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(offsetBuf, m.offset)
		buf = append(buf, offsetBuf...)

		// Member datatype message (8 bytes minimum for header).
		dtHeader := make([]byte, 8)
		classAndVersion := uint32(m.dtClass) | (3 << 4) // version 3
		binary.LittleEndian.PutUint32(dtHeader[0:4], classAndVersion)
		binary.LittleEndian.PutUint32(dtHeader[4:8], m.dtSize)

		// Add properties for the member datatype so ParseDatatypeMessage works correctly.
		var dtProps []byte
		switch m.dtClass {
		case DatatypeFixed:
			// Integer properties: 4 bytes (bit offset + precision).
			dtProps = make([]byte, 4)
			binary.LittleEndian.PutUint16(dtProps[2:4], uint16(m.dtSize*8))
		case DatatypeFloat:
			// Float properties: 12 bytes.
			dtProps = make([]byte, 12)
		case DatatypeString:
			// No additional properties needed for simple string.
		}

		buf = append(buf, dtHeader...)
		buf = append(buf, dtProps...)
	}

	return buf
}

// buildCompoundDatatypeV3 builds a complete compound datatype message (v3).
func buildCompoundDatatypeV3(structSize uint32, properties []byte) []byte {
	result := make([]byte, 8+len(properties))
	classAndVersion := uint32(DatatypeCompound) | (3 << 4) // version=3, class=6 (compound)
	binary.LittleEndian.PutUint32(result[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(result[4:8], structSize)
	copy(result[8:], properties)
	return result
}

// buildCompactLayoutMessage builds a compact layout message (data stored inline).
func buildCompactLayoutMessage(data []byte) []byte {
	// Layout version 3, class 0 (compact).
	// Byte 0: version (3)
	// Byte 1: class (0 = compact)
	// Bytes 2-3: size (uint16)
	// Bytes 4+: data
	msg := make([]byte, 4+len(data))
	msg[0] = 3 // version
	msg[1] = uint8(LayoutCompact)
	binary.LittleEndian.PutUint16(msg[2:4], uint16(len(data)))
	copy(msg[4:], data)
	return msg
}

// buildScalarDataspaceMessage builds a scalar (single element) dataspace message.
// Uses version 1 format with ndims=0 which ParseDataspaceMessage treats as scalar.
func buildScalarDataspaceMessage() []byte {
	// Version 1: version(1) + ndims(1) + flags(1) + reserved(5) = 8 bytes.
	// ndims=0 triggers scalar handling, returning immediately with Dimensions=[1].
	data := make([]byte, 8)
	data[0] = 1 // version 1
	data[1] = 0 // ndims = 0 (scalar)
	data[2] = 0 // flags
	return data
}

// buildDataspaceV1Message builds a version 1 simple dataspace message with correct header.
// Version 1 header: version(1) + ndims(1) + flags(1) + reserved(5) = 8 bytes total.
// Then 8-byte dimension values follow.
func buildDataspaceV1Message(dims []uint64) []byte {
	data := make([]byte, 8+len(dims)*8)
	data[0] = 1                // version 1
	data[1] = uint8(len(dims)) // dimensionality
	data[2] = 0                // flags (no max dims)
	// bytes 3-7: reserved (zeros)
	offset := 8
	for _, dim := range dims {
		binary.LittleEndian.PutUint64(data[offset:offset+8], dim)
		offset += 8
	}
	return data
}
