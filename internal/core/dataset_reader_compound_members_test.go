package core

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// compoundMembersType is a record with every member kind ReadCompound maps:
// integers of each width and sign, an enumeration, numeric arrays and an array
// of fixed strings.
func compoundMembersType(t *testing.T) *CompoundType {
	t.Helper()
	mk := func(dt *DatatypeMessage, err error) *DatatypeMessage {
		t.Helper()
		require.NoError(t, err)
		return dt
	}
	u8 := mk(NewIntegerDatatype(1, false))
	i8 := mk(NewIntegerDatatype(1, true))
	u16 := mk(NewIntegerDatatype(2, false))
	u32 := mk(NewIntegerDatatype(4, false))
	i32 := mk(NewIntegerDatatype(4, true))
	u64 := mk(NewIntegerDatatype(8, false))
	f64 := mk(NewFloatDatatype(8))
	str3 := mk(NewStringDatatype(3, StringNullPad, CharsetASCII))
	enum := mk(NewEnumDatatype(u8, []string{"ok", "failed"}, []byte{0, 1}))

	ct := &CompoundType{Size: 70, Members: []CompoundMember{
		{Name: "u8", Offset: 0, Type: u8},
		{Name: "i8", Offset: 1, Type: i8},
		{Name: "u16", Offset: 2, Type: u16},
		{Name: "u32", Offset: 4, Type: u32},
		{Name: "enum", Offset: 8, Type: enum},
		{Name: "xest", Offset: 9, Type: mk(NewArrayDatatype(f64, []uint32{3}))},
		{Name: "pix", Offset: 33, Type: mk(NewArrayDatatype(i32, []uint32{2}))},
		{Name: "tags", Offset: 41, Type: mk(NewArrayDatatype(str3, []uint32{2}))},
		{Name: "u64", Offset: 47, Type: u64},
		{Name: "grid", Offset: 55, Type: mk(NewArrayDatatype(u8, []uint32{2, 3}))},
	}}
	return ct
}

func compoundMembersRecord() []byte {
	b := make([]byte, 70)
	b[0] = 200
	b[1] = 0xff // -1
	binary.LittleEndian.PutUint16(b[2:], 60000)
	binary.LittleEndian.PutUint32(b[4:], 0xfffffffe) // Above 2^31: wrong as int32.
	b[8] = 1
	for i, v := range []float64{1.5, -2, 3.25} {
		binary.LittleEndian.PutUint64(b[9+8*i:], math.Float64bits(v))
	}
	binary.LittleEndian.PutUint32(b[33:], uint32(0xfffffff9)) // -7
	binary.LittleEndian.PutUint32(b[37:], 8)
	copy(b[41:], "ab\x00xyz")
	binary.LittleEndian.PutUint64(b[47:], 1<<63+5)
	copy(b[55:], []byte{1, 2, 3, 4, 5, 6})
	return b
}

// TestParseCompoundData_MemberTypes: every fixed-point width maps to the Go
// integer of its width and sign, an enumeration to its base integer value
// (H5T__conv_enum_numeric, H5Tconv_enum.c:507-561) and an array to a slice of
// its elements in C order (red before TASK-068 (d): "unsupported member
// datatype", and a 4-byte unsigned member read as a negative int32).
func TestParseCompoundData_MemberTypes(t *testing.T) {
	got, err := parseCompoundDataWith(compoundMembersRecord(), compoundMembersType(t), 1, nil)
	require.NoError(t, err)
	require.Equal(t, CompoundValue{
		"u8":   uint8(200),
		"i8":   int8(-1),
		"u16":  uint16(60000),
		"u32":  uint32(0xfffffffe),
		"enum": uint8(1),
		"xest": []float64{1.5, -2, 3.25},
		"pix":  []int32{-7, 8},
		"tags": []interface{}{"ab", "xyz"},
		"u64":  uint64(1<<63 + 5),
		"grid": []uint8{1, 2, 3, 4, 5, 6},
	}, got[0])
}

// TestParseCompoundData_ArrayBounds: an array member whose elements reach past
// the record is rejected, not read from the next record.
func TestParseCompoundData_ArrayBounds(t *testing.T) {
	f64, err := NewFloatDatatype(8)
	require.NoError(t, err)
	arr, err := NewArrayDatatype(f64, []uint32{4})
	require.NoError(t, err)
	ct := &CompoundType{Size: 16, Members: []CompoundMember{{Name: "a", Offset: 0, Type: arr}}}
	_, err = parseCompoundDataWith(make([]byte, 64), ct, 4, nil)
	require.Error(t, err)

	// A fixed-point width the format allows but Go has no type for.
	odd := &DatatypeMessage{Class: DatatypeFixed, Size: 3}
	ct = &CompoundType{Size: 3, Members: []CompoundMember{{Name: "x", Offset: 0, Type: odd}}}
	_, err = parseCompoundDataWith(make([]byte, 3), ct, 1, nil)
	require.ErrorContains(t, err, "unsupported member datatype")
}

// TestCompoundResultSize_Arrays: an array member costs a slice of its
// elements, so the read limit sees it (ADR-003).
func TestCompoundResultSize_Arrays(t *testing.T) {
	f64, err := NewFloatDatatype(8)
	require.NoError(t, err)
	arr, err := NewArrayDatatype(f64, []uint32{1000})
	require.NoError(t, err)
	ct := &CompoundType{Size: 8000, Members: []CompoundMember{{Name: "a", Offset: 0, Type: arr}}}
	require.GreaterOrEqual(t, CompoundResultSize(ct), uint64(8000))
}
