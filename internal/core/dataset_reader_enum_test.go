package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConvertToFloat64_Enum: an enumeration reads as its base integer values,
// as H5T__conv_enum_numeric converts it (H5Tconv_enum.c:507-561).
func TestConvertToFloat64_Enum(t *testing.T) {
	for _, tc := range []struct {
		size   uint32
		signed bool
		raw    []byte
		want   []float64
	}{
		{size: 1, signed: false, raw: []byte{0, 4, 2}, want: []float64{0, 4, 2}},
		{size: 2, signed: true, raw: []byte{0xff, 0xff, 3, 0}, want: []float64{-1, 3}},
		{size: 4, signed: true, raw: []byte{1, 0, 0, 0, 2, 0, 0, 0}, want: []float64{1, 2}},
	} {
		base, err := NewIntegerDatatype(tc.size, tc.signed)
		require.NoError(t, err)
		values := make([]byte, 2*tc.size)
		values[tc.size] = 1
		enum, err := NewEnumDatatype(base, []string{"A", "B"}, values)
		require.NoError(t, err)

		got, err := convertToFloat64(tc.raw, enum, uint64(len(tc.want)))
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}

	// An enumeration over a non-integer base is not convertible.
	base, err := NewFloatDatatype(4)
	require.NoError(t, err)
	enum := &DatatypeMessage{Class: DatatypeEnum, Size: 4, Base: base}
	_, err = convertToFloat64(make([]byte, 4), enum, 1)
	require.Error(t, err)
}
