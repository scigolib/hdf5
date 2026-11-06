package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAttributeReadValue_WidthsAndByteOrder: integer attributes of 1 and 2
// bytes read with their sign, and every numeric attribute in its stored byte
// order (h5repack_named_dtypes.h5 holds H5T_STD_I16BE attributes). Red before
// TASK-068: "unsupported datatype class 0 or size 2", and big-endian values
// were decoded as little-endian.
func TestAttributeReadValue_WidthsAndByteOrder(t *testing.T) {
	vec := func(n uint64) *DataspaceMessage {
		return &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{n}}
	}
	const be, signed = 0x01, 0x08

	for name, tc := range map[string]struct {
		dt   *DatatypeMessage
		data []byte
		want interface{}
	}{
		"i16 BE":  {&DatatypeMessage{Class: DatatypeFixed, Size: 2, ClassBitField: be | signed}, []byte{0xff, 0xfe, 0, 7}, []int16{-2, 7}},
		"u16 LE":  {&DatatypeMessage{Class: DatatypeFixed, Size: 2}, []byte{0xff, 0xff, 1, 0}, []uint16{65535, 1}},
		"i8":      {&DatatypeMessage{Class: DatatypeFixed, Size: 1, ClassBitField: signed}, []byte{0xff, 5}, []int8{-1, 5}},
		"u8":      {&DatatypeMessage{Class: DatatypeFixed, Size: 1}, []byte{200, 5}, []uint8{200, 5}},
		"i32 BE":  {&DatatypeMessage{Class: DatatypeFixed, Size: 4, ClassBitField: be | signed}, []byte{0, 0, 0, 3, 0xff, 0xff, 0xff, 0xff}, []int32{3, -1}},
		"f64 BE":  {&DatatypeMessage{Class: DatatypeFloat, Size: 8, ClassBitField: be}, []byte{0x3f, 0xf8, 0, 0, 0, 0, 0, 0, 0xc0, 0, 0, 0, 0, 0, 0, 0}, []float64{1.5, -2}},
		"f32 BE":  {&DatatypeMessage{Class: DatatypeFloat, Size: 4, ClassBitField: be}, []byte{0x3f, 0xc0, 0, 0, 0xc0, 0, 0, 0}, []float32{1.5, -2}},
		"i64 BE ": {&DatatypeMessage{Class: DatatypeFixed, Size: 8, ClassBitField: be | signed}, []byte{0, 0, 0, 0, 0, 0, 0, 9, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe}, []int64{9, -2}},
	} {
		t.Run(name, func(t *testing.T) {
			a := &Attribute{Name: "a", Datatype: tc.dt, Dataspace: vec(2), Data: tc.data}
			got, err := a.ReadValue()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	// A truncated value is refused, not read past the data.
	a := &Attribute{Name: "a", Datatype: &DatatypeMessage{Class: DatatypeFixed, Size: 2}, Dataspace: vec(3), Data: []byte{1, 0, 2, 0}}
	_, err := a.ReadValue()
	require.Error(t, err)
}
