package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// Tests of reading datasets whose element type is an array (TASK-071). C
// converts an array element by converting its nelem base values in order
// (H5T__conv_array, H5Tconv_array.c:195-199), so a read returns the base
// values flattened in C order with the array dimensions innermost.

func arrayOf(base *DatatypeMessage, dims ...uint32) *DatatypeMessage {
	n := uint32(1)
	for _, d := range dims {
		n *= d
	}
	return &DatatypeMessage{Class: DatatypeArray, Version: 3, Size: n * base.Size, ArrayDims: dims, Base: base}
}

func int16Type() *DatatypeMessage {
	return &DatatypeMessage{Class: DatatypeFixed, Size: 2, ClassBitField: 0x08, Precision: 16}
}

func compactArrayDesc(t *testing.T, dt *DatatypeMessage, data []byte, dims ...uint64) *DatasetDescriptor {
	t.Helper()
	layout := &DataLayoutMessage{Version: 3, Class: LayoutCompact, CompactData: data}
	desc, err := newDatasetDescriptor(bytes.NewReader(nil), descTestSB(), dt, simpleSpace(dims...), layout)
	require.NoError(t, err)
	return desc
}

func TestReadFloat64_Array(t *testing.T) {
	// Two elements of int16[2][3]: values 0..11 in C order.
	dt := arrayOf(int16Type(), 2, 3)
	data := make([]byte, 24)
	for i := range 12 {
		binary.LittleEndian.PutUint16(data[2*i:], uint16(i))
	}
	desc := compactArrayDesc(t, dt, data, 2)

	got, err := ReadFloat64(bytes.NewReader(nil), descTestSB(), desc, utils.DefaultReadLimit)
	require.NoError(t, err)
	require.Equal(t, []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, got)

	// Nested arrays flatten the same way: int16[2] of int16[3].
	nested := arrayOf(arrayOf(int16Type(), 3), 2)
	desc = compactArrayDesc(t, nested, data, 2)
	got, err = ReadFloat64(bytes.NewReader(nil), descTestSB(), desc, utils.DefaultReadLimit)
	require.NoError(t, err)
	require.Len(t, got, 12)
	require.InDelta(t, 11.0, got[11], 0)
}

func TestReadFloat64_ArrayResultSize(t *testing.T) {
	// The float64 result of an array element is its value count times eight
	// bytes: int16[2][3] holds 12 bytes and converts to 48. The read limit
	// must see the converted size, not the size of one float64 per element.
	dt := arrayOf(int16Type(), 2, 3)
	size, err := Float64ResultSize(dt)
	require.NoError(t, err)
	require.Equal(t, uint64(48), size)

	size, err = Float64ResultSize(float64Type())
	require.NoError(t, err)
	require.Equal(t, ResultFloat64, size)
}

func TestReadFloat64_ArrayCorruptSize(t *testing.T) {
	// The size of an array is always nelem × the base size
	// (H5T__array_create, H5Tarray.c:162; H5T_set_size refuses arrays,
	// H5T.c:3049). A file stating otherwise is damaged.
	for _, size := range []uint32{10, 14} {
		dt := arrayOf(int16Type(), 2, 3)
		dt.Size = size
		err := CheckFloat64Convertible(dt)
		require.ErrorIs(t, err, utils.ErrCorrupt, "size %d", size)
	}

	dt := arrayOf(int16Type(), 6)
	dt.Base = nil
	require.ErrorIs(t, CheckFloat64Convertible(dt), utils.ErrCorrupt)
}

func TestReadFloat64_ArrayUnsupportedBase(t *testing.T) {
	str := &DatatypeMessage{Class: DatatypeString, Size: 4}
	err := CheckFloat64Convertible(arrayOf(str, 2))
	require.ErrorIs(t, err, utils.ErrNotSupported)
}

func TestReadStrings_Array(t *testing.T) {
	// Two elements of a null-padded 3-byte string [2].
	str := &DatatypeMessage{Class: DatatypeString, Size: 3, ClassBitField: 0x01}
	dt := arrayOf(str, 2)
	data := []byte("ab\x00cdeghi\x00\x00\x00")
	desc := compactArrayDesc(t, dt, data, 2)

	got, err := ReadStrings(bytes.NewReader(nil), descTestSB(), desc, utils.DefaultReadLimit)
	require.NoError(t, err)
	require.Equal(t, []string{"ab", "cde", "ghi", ""}, got)
}
