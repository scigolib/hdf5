package core

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// earlyNumericMessage builds a datatype message exactly as the numeric
// encoder of hdf5-go v0.11.0-beta to v0.13.9 wrote it (encodeDatatypeNumeric
// before commit 2f92dd0).
func earlyNumericMessage(class DatatypeClass, size, flags uint32) []byte {
	buf := binary.LittleEndian.AppendUint32(nil, uint32(class)|uint32(dtypeVersion1)<<4|flags<<8)
	buf = binary.LittleEndian.AppendUint32(buf, size)
	return append(buf, earlyHDF5GoNumericProps(class, size, byte(flags&flagBigEndian))...)
}

// TestDecodeDatatype_EarlyHDF5GoNumeric: the early encoding is read as the
// type the writer meant and recognized after decoding. The bytes come from
// the matlab module's fixtures (hdf5 v0.11.4-beta): a float64 of
// 11 00 00 00 08 00 00 00 00 40 00 0b 34 7f 00 00 00 00 00 00.
func TestDecodeDatatype_EarlyHDF5GoNumeric(t *testing.T) {
	matlab := []byte{0x11, 0, 0, 0, 8, 0, 0, 0, 0, 0x40, 0, 0x0b, 0x34, 0x7f, 0, 0, 0, 0, 0, 0}
	require.Equal(t, matlab, earlyNumericMessage(DatatypeFloat, 8, 0))

	for _, tc := range []struct {
		class DatatypeClass
		size  uint32
		flags uint32
	}{
		{DatatypeFloat, 4, 0}, {DatatypeFloat, 8, 0}, {DatatypeFloat, 8, flagBigEndian},
		{DatatypeFixed, 1, 0}, {DatatypeFixed, 2, 0x08}, {DatatypeFixed, 4, 0x08}, {DatatypeFixed, 8, 0x09},
	} {
		dt, err := ParseDatatypeMessage(earlyNumericMessage(tc.class, tc.size, tc.flags))
		require.NoError(t, err, "%v size %d flags %#x", tc.class, tc.size, tc.flags)
		require.True(t, IsEarlyHDF5GoDatatype(dt))
		require.Equal(t, uint16(0), dt.BitOffset)
		require.Equal(t, uint16(tc.size*8), dt.Precision)
		if tc.class == DatatypeFloat {
			want := &DatatypeMessage{Class: DatatypeFloat, Size: tc.size, ClassBitField: tc.flags}
			normalizeIEEEFloat(want)
			require.Equal(t, want.ClassBitField, dt.ClassBitField)
			require.Equal(t, [5]uint32{uint32(want.ExponentLocation), uint32(want.ExponentSize),
				uint32(want.MantissaLocation), uint32(want.MantissaSize), want.ExponentBias},
				[5]uint32{uint32(dt.ExponentLocation), uint32(dt.ExponentSize),
					uint32(dt.MantissaLocation), uint32(dt.MantissaSize), dt.ExponentBias})
		} else {
			require.Equal(t, tc.flags, dt.ClassBitField, "integer flags are kept")
		}
	}

	// A valid type is never taken for the early encoding.
	for _, mk := range []func() (*DatatypeMessage, error){
		func() (*DatatypeMessage, error) { return NewFloatDatatype(8) },
		func() (*DatatypeMessage, error) { return NewFloatDatatype(4) },
		func() (*DatatypeMessage, error) { return NewIntegerDatatype(4, true) },
		func() (*DatatypeMessage, error) { return NewIntegerDatatype(1, false) },
	} {
		dt, err := mk()
		require.NoError(t, err)
		enc, err := EncodeDatatypeMessage(dt)
		require.NoError(t, err)
		got, err := ParseDatatypeMessage(enc)
		require.NoError(t, err)
		require.False(t, IsEarlyHDF5GoDatatype(got))
	}
}

// TestDecodeDatatype_EarlyHDF5GoNumericNearMiss: bytes that differ from the
// early encoder's output in any way stay corrupt, as the C library reads them.
func TestDecodeDatatype_EarlyHDF5GoNumericNearMiss(t *testing.T) {
	base := earlyNumericMessage(DatatypeFloat, 8, 0)
	for name, mutate := range map[string]func(b []byte){
		"version 2":    func(b []byte) { b[0] = 0x21 },
		"sign flag":    func(b []byte) { b[2] = 63 },
		"bias 1023":    func(b []byte) { b[13] = 0xff; b[14] = 3 },
		"trailing one": func(b []byte) { b[19] = 1 },
	} {
		b := append([]byte(nil), base...)
		mutate(b)
		_, err := ParseDatatypeMessage(b)
		require.ErrorIs(t, err, utils.ErrCorrupt, name)
	}

	integer := earlyNumericMessage(DatatypeFixed, 4, 0)
	integer[1] = 0x10 // a flag the early writer never set (padding)
	_, err := ParseDatatypeMessage(integer)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}
