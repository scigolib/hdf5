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
//
// These tests used to call the per-class encoders of messages_write.go with
// version 0 and without a base type; the C decoder rejects both (version
// H5Odtype.c:151, base type :768-772). They now encode through the codec and
// pin the bytes the C library writes.

// uint8LEBytes is H5T_NATIVE_UCHAR on a little-endian machine, the base type
// of a C variable-length string (H5T.c:4945-4948).
var uint8LEBytes = []byte{0x10, 0x00, 0x00, 0x00, 1, 0, 0, 0, 0, 0, 8, 0}

// float64LEBytes is H5T_IEEE_F64LE: version 1, implied normalization, sign at
// bit 63, exponent 52+11 bits, mantissa 0+52 bits, bias 1023.
var float64LEBytes = []byte{
	0x11, 0x20, 0x3F, 0x00, 8, 0, 0, 0,
	0, 0, 64, 0, 52, 11, 0, 52, 0xFF, 0x03, 0, 0,
}

// TestEncodeDatatypeVLen_String tests encoding a variable-length string datatype.
func TestEncodeDatatypeVLen_String(t *testing.T) {
	dt, err := NewVLenStringDatatype(StringNullTerm, CharsetASCII)
	require.NoError(t, err)
	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)

	// Class 9, version 1; class bits: kind 1 (string), padding 0, ASCII
	// (H5Odtype.c:1356-1360); size 16; base type unsigned char.
	want := append([]byte{0x19, 0x01, 0x00, 0x00, 16, 0, 0, 0}, uint8LEBytes...)
	require.Equal(t, want, data)
}

// TestEncodeDatatypeVLen_Int tests encoding a variable-length integer sequence datatype.
func TestEncodeDatatypeVLen_Int(t *testing.T) {
	base, err := NewIntegerDatatype(4, true)
	require.NoError(t, err)
	dt, err := NewVLenSequenceDatatype(base)
	require.NoError(t, err)
	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)

	// The base type message follows the 8-byte header directly.
	want := append([]byte{0x19, 0x00, 0x00, 0x00, 16, 0, 0, 0}, int32LEBytes...)
	require.Equal(t, want, data)
}

// TestEncodeDatatypeVLen_UTF8String tests encoding a vlen UTF-8 string.
func TestEncodeDatatypeVLen_UTF8String(t *testing.T) {
	dt, err := NewVLenStringDatatype(StringNullTerm, CharsetUTF8)
	require.NoError(t, err)
	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)

	// Character set in class bits 8-11 lands in header byte 2.
	require.Equal(t, []byte{0x19, 0x01, 0x01, 0x00}, data[0:4])
	require.Equal(t, uint32(16), binary.LittleEndian.Uint32(data[4:8]))
	require.Equal(t, uint8LEBytes, data[8:])
}

// TestEncodeDatatypeVLen_EmptyProperties tests that a variable-length type
// without a base type is refused: the C decoder always reads one.
func TestEncodeDatatypeVLen_EmptyProperties(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Size:          16,
		ClassBitField: 1,
	}
	_, err := EncodeDatatypeMessage(dt)
	require.ErrorContains(t, err, "no base type")
}

// TestEncodeDatatypeVLen_WithBaseType tests vlen with a float64 base type.
func TestEncodeDatatypeVLen_WithBaseType(t *testing.T) {
	base, err := NewFloatDatatype(8)
	require.NoError(t, err)
	dt, err := NewVLenSequenceDatatype(base)
	require.NoError(t, err)
	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)

	require.Len(t, data, 8+len(float64LEBytes))
	// Base type starts immediately after the header.
	require.Equal(t, float64LEBytes, data[8:])
}

// --- Compound Datatype Encoding Tests ---

// encodeDecodeCompound encodes fields as a v3 compound, decodes it, and
// re-encodes the decoded message, requiring identical bytes.
func encodeDecodeCompound(t *testing.T, size uint32, fields []CompoundFieldDef) (*DatatypeMessage, []byte) {
	t.Helper()
	encoded, err := EncodeCompoundDatatypeV3(size, fields)
	require.NoError(t, err)

	dt, err := ParseDatatypeMessage(encoded)
	require.NoError(t, err)
	require.Equal(t, DatatypeCompound, dt.Class)
	require.Equal(t, size, dt.Size)
	require.Len(t, dt.Members, len(fields))

	again, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	require.Equal(t, encoded, again)

	// Member count is the low 16 bits of the class bit field (H5Odtype.c:1247).
	require.Equal(t, uint16(len(fields)), binary.LittleEndian.Uint16(encoded[1:3]))
	return dt, encoded
}

// TestEncodeDatatypeCompound_TwoMembers tests encoding compound with two integer members.
func TestEncodeDatatypeCompound_TwoMembers(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	encodeDecodeCompound(t, 8, []CompoundFieldDef{
		{Name: "x", Offset: 0, Type: intType},
		{Name: "y", Offset: 4, Type: intType},
	})
}

// TestEncodeDatatypeCompound_WithString tests compound with a string member.
func TestEncodeDatatypeCompound_WithString(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)
	strType, err := CreateBasicDatatypeMessage(DatatypeString, 10)
	require.NoError(t, err)

	dt, _ := encodeDecodeCompound(t, 14, []CompoundFieldDef{
		{Name: "id", Offset: 0, Type: intType},
		{Name: "name", Offset: 4, Type: strType},
	})
	require.Equal(t, uint32(10), dt.Members[1].Type.Size)
}

// TestEncodeDatatypeCompound_EmptyProperties tests that a compound without
// members is refused (C: "invalid number of members: 0", H5Odtype.c:358-360).
func TestEncodeDatatypeCompound_EmptyProperties(t *testing.T) {
	dt := &DatatypeMessage{
		Class:   DatatypeCompound,
		Version: 3,
		Size:    8,
	}
	_, err := EncodeDatatypeMessage(dt)
	require.ErrorContains(t, err, "compound with 0 members")
}

// TestEncodeDatatypeCompound_ThreeMembers tests compound with three mixed-type members.
func TestEncodeDatatypeCompound_ThreeMembers(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)
	floatType, err := CreateBasicDatatypeMessage(DatatypeFloat, 8)
	require.NoError(t, err)

	dt, _ := encodeDecodeCompound(t, 16, []CompoundFieldDef{
		{Name: "a", Offset: 0, Type: intType},
		{Name: "b", Offset: 4, Type: floatType},
		{Name: "c", Offset: 12, Type: intType},
	})
	require.Equal(t, uint32(12), dt.Members[2].Offset)
}

// TestEncodeDatatypeCompound_RoundTrip tests encode-decode-encode roundtrip.
func TestEncodeDatatypeCompound_RoundTrip(t *testing.T) {
	intType, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	require.NoError(t, err)

	_, encoded := encodeDecodeCompound(t, 8, []CompoundFieldDef{
		{Name: "x", Offset: 0, Type: intType},
		{Name: "y", Offset: 4, Type: intType},
	})
	// Header (class 6, version 3, 2 members, size 8), then per member: name,
	// 1-byte offset (limit_enc_size(8) = 1), member type.
	require.Equal(t, []byte{0x36, 2, 0, 0, 8, 0, 0, 0, 'x', 0, 0}, encoded[:11])
}
