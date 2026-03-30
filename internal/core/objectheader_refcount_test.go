package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Reference Counting Tests ---

// TestGetReferenceCount_V1Header tests reading reference count from a v1 object header.
func TestGetReferenceCount_V1Header(t *testing.T) {
	oh := &ObjectHeader{
		Version:        1,
		ReferenceCount: 1,
		Messages:       []*HeaderMessage{},
	}

	require.Equal(t, uint32(1), oh.GetReferenceCount())
}

// TestGetReferenceCount_V2Header tests reading reference count from a v2 object header.
func TestGetReferenceCount_V2Header(t *testing.T) {
	oh := &ObjectHeader{
		Version:        2,
		ReferenceCount: 1,
		Messages:       []*HeaderMessage{},
	}

	require.Equal(t, uint32(1), oh.GetReferenceCount())

	// Test with explicit RefCount message data (v2 stores refcount in MsgRefCount)
	refCountData := make([]byte, 4)
	binary.LittleEndian.PutUint32(refCountData, 5)
	oh.ReferenceCount = 5
	oh.Messages = append(oh.Messages, &HeaderMessage{
		Type: MsgRefCount,
		Data: refCountData,
	})

	require.Equal(t, uint32(5), oh.GetReferenceCount())
}

// TestGetReferenceCount_Default tests default reference count value.
func TestGetReferenceCount_Default(t *testing.T) {
	oh := &ObjectHeader{
		Version:        2,
		ReferenceCount: 0,
	}
	require.Equal(t, uint32(0), oh.GetReferenceCount())
}

// TestIncrementReferenceCount tests incrementing reference count.
func TestIncrementReferenceCount(t *testing.T) {
	oh := &ObjectHeader{
		Version:        1,
		ReferenceCount: 1,
	}

	// Increment once
	newCount := oh.IncrementReferenceCount()
	require.Equal(t, uint32(2), newCount)
	require.Equal(t, uint32(2), oh.ReferenceCount)

	// Increment again
	newCount = oh.IncrementReferenceCount()
	require.Equal(t, uint32(3), newCount)
	require.Equal(t, uint32(3), oh.ReferenceCount)
}

// TestIncrementReferenceCount_FromZero tests incrementing from zero.
func TestIncrementReferenceCount_FromZero(t *testing.T) {
	oh := &ObjectHeader{
		Version:        2,
		ReferenceCount: 0,
	}

	newCount := oh.IncrementReferenceCount()
	require.Equal(t, uint32(1), newCount)
}

// TestIncrementReferenceCount_MultipleIncrements tests many increments.
func TestIncrementReferenceCount_MultipleIncrements(t *testing.T) {
	oh := &ObjectHeader{
		Version:        1,
		ReferenceCount: 1,
	}

	for i := uint32(2); i <= 10; i++ {
		newCount := oh.IncrementReferenceCount()
		require.Equal(t, i, newCount)
	}
	require.Equal(t, uint32(10), oh.GetReferenceCount())
}

// TestDecrementReferenceCount tests decrementing reference count.
func TestDecrementReferenceCount(t *testing.T) {
	oh := &ObjectHeader{
		Version:        1,
		ReferenceCount: 3,
	}

	// Decrement from 3 to 2
	newCount := oh.DecrementReferenceCount()
	require.Equal(t, uint32(2), newCount)
	require.Equal(t, uint32(2), oh.ReferenceCount)

	// Decrement from 2 to 1
	newCount = oh.DecrementReferenceCount()
	require.Equal(t, uint32(1), newCount)

	// Decrement from 1 to 0
	newCount = oh.DecrementReferenceCount()
	require.Equal(t, uint32(0), newCount)
}

// TestDecrementReferenceCount_AtZero tests that decrement doesn't go below zero.
func TestDecrementReferenceCount_AtZero(t *testing.T) {
	oh := &ObjectHeader{
		Version:        2,
		ReferenceCount: 0,
	}

	// Decrement at 0 should stay at 0
	newCount := oh.DecrementReferenceCount()
	require.Equal(t, uint32(0), newCount)
	require.Equal(t, uint32(0), oh.ReferenceCount)

	// Try again - still 0
	newCount = oh.DecrementReferenceCount()
	require.Equal(t, uint32(0), newCount)
}

// TestReferenceCount_IncrementThenDecrement tests increment/decrement cycle.
func TestReferenceCount_IncrementThenDecrement(t *testing.T) {
	oh := &ObjectHeader{
		Version:        1,
		ReferenceCount: 1,
	}

	// Increment to 5
	for i := 0; i < 4; i++ {
		oh.IncrementReferenceCount()
	}
	require.Equal(t, uint32(5), oh.GetReferenceCount())

	// Decrement back to 1
	for i := 0; i < 4; i++ {
		oh.DecrementReferenceCount()
	}
	require.Equal(t, uint32(1), oh.GetReferenceCount())
}

// --- VLen Datatype Encoding Tests ---

// TestEncodeDatatypeVLen_String tests encoding a variable-length string datatype.
func TestEncodeDatatypeVLen_String(t *testing.T) {
	// Create a vlen string datatype
	// ClassBitField for vlen string: type=1 (string), padding=0, charset=0 (ASCII)
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Version:       0,
		Size:          16,         // VLen heap ID size
		ClassBitField: 0x00000001, // type=1 (string)
		Properties:    []byte{},   // No base type properties for simple vlen string
	}

	data, err := encodeDatatypeVLen(dt)
	require.NoError(t, err)
	require.NotNil(t, data)

	// C Reference (H5Odtype.c:1438-1442):
	// Bytes 0-3 = class (bits 0-3) | version (bits 4-7) | ClassBitField (bits 8-31)
	// Class 9 (VarLen), version 0, ClassBitField 1 (string) =>
	// uint32 LE = 0x09 | (0 << 4) | (1 << 8) = 0x00000109
	require.Equal(t, byte(0x09), data[0]) // class=9, version=0
	require.Equal(t, byte(0x01), data[1]) // ClassBitField bits 8-15: type=1 (string)
	require.Equal(t, byte(0x00), data[2])
	require.Equal(t, byte(0x00), data[3])

	// Bytes 4-7: Size (16)
	size := binary.LittleEndian.Uint32(data[4:8])
	require.Equal(t, uint32(16), size)

	// Total: 8 bytes header + 0 bytes properties = 8 bytes
	require.Equal(t, 8, len(data))
}

// TestEncodeDatatypeVLen_Int tests encoding a variable-length integer sequence datatype.
func TestEncodeDatatypeVLen_Int(t *testing.T) {
	// Base type: int32 (4 bytes properties for integer)
	baseTypeProps := make([]byte, 4)
	baseTypeProps[0] = 0  // Byte order: little-endian
	baseTypeProps[1] = 32 // Precision: 32 bits
	baseTypeProps[2] = 0  // Offset
	baseTypeProps[3] = 0  // Padding

	// Create a vlen integer sequence datatype
	// ClassBitField for vlen sequence: type=0 (sequence), padding=0, charset=0
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Version:       0,
		Size:          16,         // VLen heap ID size
		ClassBitField: 0x00000000, // type=0 (sequence)
		Properties:    baseTypeProps,
	}

	data, err := encodeDatatypeVLen(dt)
	require.NoError(t, err)
	require.NotNil(t, data)

	// C Reference: bytes 0-3 = class|version|classBitField, bytes 4-7 = size, bytes 8+ = properties.
	// Total size: 8 (header) + 4 (base type props) = 12.
	require.Equal(t, 12, len(data))

	// Verify header: class=9, version=0, classBitField=0 => byte 0 = 0x09.
	require.Equal(t, byte(0x09), data[0])

	// Verify size field.
	size := binary.LittleEndian.Uint32(data[4:8])
	require.Equal(t, uint32(16), size)

	// Verify base type properties start at byte 8 (not 12).
	require.Equal(t, baseTypeProps, data[8:12])
}

// TestEncodeDatatypeVLen_UTF8String tests encoding a vlen UTF-8 string.
func TestEncodeDatatypeVLen_UTF8String(t *testing.T) {
	// ClassBitField: type=1 (string), padding=0, charset=1 (UTF-8)
	// Bits 0-3: type=1, bits 4-7: padding=0, bits 8-11: charset=1
	classBitField := uint32(1) | (uint32(1) << 8) // type=string, charset=UTF-8

	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Version:       0,
		Size:          16,
		ClassBitField: classBitField,
		Properties:    []byte{},
	}

	data, err := encodeDatatypeVLen(dt)
	require.NoError(t, err)

	// C Reference: ClassBitField is packed into bytes 1-3 of the header uint32.
	// class=9 | version=0<<4 | classBitField<<8 = 0x09 | (0x0101 << 8) = 0x00010109
	// Bytes: 0x09, 0x01, 0x01, 0x00
	require.Equal(t, byte(0x09), data[0])
	require.Equal(t, byte(0x01), data[1]) // ClassBitField bits 0-7: type=1
	require.Equal(t, byte(0x01), data[2]) // ClassBitField bits 8-15: charset=1 (UTF-8)
	require.Equal(t, byte(0x00), data[3])

	// Total: 8 bytes (no properties).
	require.Equal(t, 8, len(data))
}

// --- Compound Datatype Encoding Tests ---

// TestEncodeDatatypeCompound_TwoMembers tests encoding compound with two integer members.
func TestEncodeDatatypeCompound_TwoMembers(t *testing.T) {
	// Create int32 member types
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	fields := []CompoundFieldDef{
		{Name: "x", Offset: 0, Type: intType},
		{Name: "y", Offset: 4, Type: intType},
	}

	// Encode using V3 format to get full bytes (header + properties)
	encoded, err := EncodeCompoundDatatypeV3(8, fields)
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Parse to get a DatatypeMessage
	dt, err := ParseDatatypeMessage(encoded)
	require.NoError(t, err)
	require.Equal(t, DatatypeCompound, dt.Class)
	require.Equal(t, uint32(8), dt.Size)

	// Now test encodeDatatypeCompound (internal function)
	result, err := encodeDatatypeCompound(dt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the result contains valid header
	classAndVersion := binary.LittleEndian.Uint32(result[0:4])
	class := DatatypeClass(classAndVersion & 0x0F)
	require.Equal(t, DatatypeCompound, class)

	// Verify size
	resultSize := binary.LittleEndian.Uint32(result[4:8])
	require.Equal(t, uint32(8), resultSize)

	// Verify properties are present
	require.True(t, len(result) > 8)
}

// TestEncodeDatatypeCompound_WithString tests compound with a string member.
func TestEncodeDatatypeCompound_WithString(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	strType, err := CreateBasicDatatypeMessage(DatatypeString, 10)
	require.NoError(t, err)

	fields := []CompoundFieldDef{
		{Name: "id", Offset: 0, Type: intType},
		{Name: "name", Offset: 4, Type: strType},
	}

	encoded, err := EncodeCompoundDatatypeV3(14, fields)
	require.NoError(t, err)

	dt, err := ParseDatatypeMessage(encoded)
	require.NoError(t, err)

	result, err := encodeDatatypeCompound(dt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify class
	classAndVersion := binary.LittleEndian.Uint32(result[0:4])
	class := DatatypeClass(classAndVersion & 0x0F)
	require.Equal(t, DatatypeCompound, class)

	// Verify total size = 14 (4 int + 10 string)
	resultSize := binary.LittleEndian.Uint32(result[4:8])
	require.Equal(t, uint32(14), resultSize)
}

// TestEncodeDatatypeCompound_EmptyProperties tests error when properties are empty.
func TestEncodeDatatypeCompound_EmptyProperties(t *testing.T) {
	dt := &DatatypeMessage{
		Class:      DatatypeCompound,
		Version:    3,
		Size:       8,
		Properties: []byte{}, // Empty - should fail
	}

	_, err := encodeDatatypeCompound(dt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no member definitions")
}

// TestEncodeDatatypeCompound_ThreeMembers tests compound with three mixed-type members.
func TestEncodeDatatypeCompound_ThreeMembers(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	floatType, err := CreateBasicDatatypeMessage(DatatypeFloat, 8)
	require.NoError(t, err)

	fields := []CompoundFieldDef{
		{Name: "a", Offset: 0, Type: intType},
		{Name: "b", Offset: 4, Type: floatType},
		{Name: "c", Offset: 12, Type: intType},
	}

	encoded, err := EncodeCompoundDatatypeV3(16, fields)
	require.NoError(t, err)

	dt, err := ParseDatatypeMessage(encoded)
	require.NoError(t, err)

	result, err := encodeDatatypeCompound(dt)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultSize := binary.LittleEndian.Uint32(result[4:8])
	require.Equal(t, uint32(16), resultSize)
}

// TestEncodeDatatypeCompound_RoundTrip tests encode-decode-encode roundtrip.
func TestEncodeDatatypeCompound_RoundTrip(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	fields := []CompoundFieldDef{
		{Name: "x", Offset: 0, Type: intType},
		{Name: "y", Offset: 4, Type: intType},
	}

	// First encode
	encoded1, err := EncodeCompoundDatatypeV3(8, fields)
	require.NoError(t, err)

	// Parse
	dt, err := ParseDatatypeMessage(encoded1)
	require.NoError(t, err)

	// Second encode via encodeDatatypeCompound
	encoded2, err := encodeDatatypeCompound(dt)
	require.NoError(t, err)

	// Both should produce the same class and size
	class1 := DatatypeClass(binary.LittleEndian.Uint32(encoded1[0:4]) & 0x0F)
	class2 := DatatypeClass(binary.LittleEndian.Uint32(encoded2[0:4]) & 0x0F)
	require.Equal(t, class1, class2)

	size1 := binary.LittleEndian.Uint32(encoded1[4:8])
	size2 := binary.LittleEndian.Uint32(encoded2[4:8])
	require.Equal(t, size1, size2)
}

// TestEncodeDatatypeVLen_EmptyProperties tests vlen with empty properties (no base type).
func TestEncodeDatatypeVLen_EmptyProperties(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Version:       0,
		Size:          16,
		ClassBitField: 1,
		Properties:    []byte{},
	}

	data, err := encodeDatatypeVLen(dt)
	require.NoError(t, err)
	// C Reference: 8 bytes header + 0 properties = 8 bytes.
	// ClassBitField is packed into header bytes 1-3, not a separate field.
	require.Equal(t, 8, len(data))
}

// TestEncodeDatatypeVLen_WithBaseType tests vlen with base type properties.
func TestEncodeDatatypeVLen_WithBaseType(t *testing.T) {
	// Base type: float64 (12 bytes properties for float)
	baseTypeProps := make([]byte, 12)
	baseTypeProps[0] = 0  // Byte order: little-endian
	baseTypeProps[1] = 64 // Precision: 64 bits

	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Version:       0,
		Size:          16,
		ClassBitField: 0, // sequence type
		Properties:    baseTypeProps,
	}

	data, err := encodeDatatypeVLen(dt)
	require.NoError(t, err)
	// C Reference: 8 bytes header + 12 properties = 20 bytes.
	// ClassBitField is packed into header bytes 1-3, not a separate field.
	require.Equal(t, 20, len(data))

	// Verify base type properties start at byte 8 (immediately after header).
	require.Equal(t, baseTypeProps, data[8:20])
}
