package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEncodeFillValueMessage checks the encoder byte for byte against
// H5O__fill_new_encode (H5Ofill.c:409-490); the version 2 and 3 defaults are
// the bytes found in C-written files of testdata (see
// dataset_write_c_defaults_test.go).
func TestEncodeFillValueMessage(t *testing.T) {
	cases := []struct {
		name string
		in   FillValueMessage
		want []byte
	}{
		{"v3_default_contiguous", FillValueMessage{Version: 3, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet}, []byte{3, 0x0a}},
		{"v3_default_chunked", FillValueMessage{Version: 3, AllocTime: AllocTimeIncr, FillTime: FillTimeIfSet}, []byte{3, 0x0b}},
		{"v3_default_compact", FillValueMessage{Version: 3, AllocTime: AllocTimeEarly, FillTime: FillTimeIfSet}, []byte{3, 0x09}},
		{"v3_vlen", FillValueMessage{Version: 3, AllocTime: AllocTimeLate, FillTime: FillTimeAlloc}, []byte{3, 0x02}},
		{"v3_undefined", FillValueMessage{Version: 3, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet, Size: -1}, []byte{3, 0x1a}},
		{
			"v3_value", FillValueMessage{Version: 3, AllocTime: AllocTimeIncr, FillTime: FillTimeIfSet, Size: 4, Value: []byte{1, 2, 3, 4}},
			[]byte{3, 0x2b, 4, 0, 0, 0, 1, 2, 3, 4},
		},
		{"v2_default_contiguous", FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet}, []byte{2, 2, 2, 1, 0, 0, 0, 0}},
		{"v2_default_chunked", FillValueMessage{Version: 2, AllocTime: AllocTimeIncr, FillTime: FillTimeIfSet}, []byte{2, 3, 2, 1, 0, 0, 0, 0}},
		{"v2_undefined", FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet, Size: -1}, []byte{2, 2, 2, 0}},
		{
			"v2_value", FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet, Size: 2, Value: []byte{0xfe, 0xff}},
			[]byte{2, 2, 2, 1, 2, 0, 0, 0, 0xfe, 0xff},
		},
		{"v1_default", FillValueMessage{Version: 1, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet}, []byte{1, 2, 2, 1, 0, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeFillValueMessage(&tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			// The decoder reads back what was encoded.
			back, err := ParseFillValueMessage(got)
			require.NoError(t, err)
			require.Equal(t, tc.in.Version, back.Version)
			require.Equal(t, tc.in.AllocTime, back.AllocTime)
			require.Equal(t, tc.in.FillTime, back.FillTime)
			require.Equal(t, tc.in.Size, back.Size)
		})
	}
}

func TestEncodeFillValueMessage_Invalid(t *testing.T) {
	cases := map[string]*FillValueMessage{
		"nil":            nil,
		"version_0":      {Version: 0},
		"version_4":      {Version: 4},
		"alloc_time":     {Version: 3, AllocTime: 4},
		"fill_time":      {Version: 3, FillTime: 4},
		"size_below_-1":  {Version: 3, Size: -2},
		"size_mismatch":  {Version: 3, Size: 3, Value: []byte{1}},
		"value_size_0":   {Version: 3, Size: 0, Value: []byte{1}},
		"value_undef":    {Version: 2, Size: -1, Value: []byte{1}},
		"size_too_large": {Version: 3, Size: 0x80000000},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := EncodeFillValueMessage(in)
			require.Error(t, err)
		})
	}
}

// TestNewDatasetFillValue checks the defaults H5Dcreate applies
// (H5Pdcpl.c:161-165 and 1856-1876, H5Dint.c:928-937, H5Ofill.c:150-158).
func TestNewDatasetFillValue(t *testing.T) {
	i32 := &DatatypeMessage{Class: DatatypeFixed, Size: 4}
	vlen := &DatatypeMessage{Class: DatatypeVarLen, Size: 16, Base: &DatatypeMessage{Class: DatatypeFixed, Size: 1}}
	cmpVlen := &DatatypeMessage{Class: DatatypeCompound, Size: 20, Members: []CompoundMember{{Name: "a", Type: i32}, {Name: "s", Offset: 4, Type: vlen}}}

	cases := []struct {
		name   string
		dt     *DatatypeMessage
		layout DataLayoutClass
		sb     uint8
		want   FillValueMessage
	}{
		{"contiguous_v2", i32, LayoutContiguous, Version2, FillValueMessage{Version: 3, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet}},
		{"chunked_v3", i32, LayoutChunked, Version3, FillValueMessage{Version: 3, AllocTime: AllocTimeIncr, FillTime: FillTimeIfSet}},
		{"compact_v0", i32, LayoutCompact, Version0, FillValueMessage{Version: 2, AllocTime: AllocTimeEarly, FillTime: FillTimeIfSet}},
		{"vlen_v0", vlen, LayoutContiguous, Version0, FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeAlloc}},
		{"compound_vlen_v2", cmpVlen, LayoutChunked, Version2, FillValueMessage{Version: 3, AllocTime: AllocTimeIncr, FillTime: FillTimeAlloc}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, *NewDatasetFillValue(tc.dt, tc.layout, tc.sb))
		})
	}
}
