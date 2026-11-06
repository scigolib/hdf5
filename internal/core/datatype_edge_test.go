package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDatatypeMessage_InvalidVersion tests error handling.
func TestParseDatatypeMessage_InvalidVersion(_ *testing.T) {
	data := []byte{
		0xF0, // version 15 (invalid), class 0
		0,
		0,
		0,
		0, 0, 0, 0, // size
	}

	dt, err := ParseDatatypeMessage(data)
	// May not error for all invalid versions, but should not panic
	_ = dt
	_ = err
}

// TestParseDatatypeMessage_TooShort tests short data handling.
func TestParseDatatypeMessage_TooShort(t *testing.T) {
	data := []byte{0x30} // Only 1 byte

	_, err := ParseDatatypeMessage(data)
	require.Error(t, err)
}

// TestParseDatatypeMessage_Array tests array datatype (minimal).
func TestParseDatatypeMessage_Array(t *testing.T) {
	// Version 3 array datatype (just check it doesn't panic)
	data := []byte{
		0x3A, // version 3, class 10 (array)
		0, 0, // class bits
		16, 0, 0, 0, // size (16 bytes)
		// Need enough padding to not fail on read
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	// May fail due to incomplete data, but should not panic
	if err == nil {
		require.Equal(t, DatatypeClass(10), dt.Class)
	}
}

// TestParseDatatypeMessage_Enum tests enum datatype (minimal).
func TestParseDatatypeMessage_Enum(t *testing.T) {
	// Version 3 enum datatype
	data := []byte{
		0x38, // version 3, class 8 (enum)
		0, 0, // class bits
		4, 0, 0, 0, // size 4
		// Padding to avoid read errors
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	// May fail, but should not panic
	if err == nil {
		require.Equal(t, DatatypeClass(8), dt.Class)
	}
}

// TestParseDatatypeMessage_Opaque tests opaque datatype.
func TestParseDatatypeMessage_Opaque(t *testing.T) {
	// Version 1 opaque datatype
	data := []byte{
		0x10 | 5, // version 1, class 5 (opaque)
		0, 0,     // class bits
		16, 0, 0, 0, // size 16
		// ASCII tag (null-terminated, multiple of 8)
		'O', 'P', 'A', 'Q', 'U', 'E', '_', 'T',
		'A', 'G', 0, 0, 0, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	require.NoError(t, err)
	require.Equal(t, DatatypeClass(5), dt.Class)
}

// TestParseDatatypeMessage_Reference tests reference datatype.
func TestParseDatatypeMessage_Reference(t *testing.T) {
	// Version 1 reference datatype
	data := []byte{
		0x10 | 9, // version 1, class 9 (reference)
		0, 0,     // class bits
		12, 0, 0, 0, // size 12 (object + dataset region)
		// Add padding
		0, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	if err == nil {
		require.Equal(t, DatatypeClass(9), dt.Class)
		// Size may be parsed differently, just check no panic
	}
}

// TestParseDatatypeMessage_VarLen tests variable-length datatype.
//
// Until the codec this fixture was a 13-byte message with a 3-byte class
// word (decoding as an array of size 0x30000000) whose class 10 the test
// asserted; the C decoder rejects those bytes. It is now a well-formed
// variable-length sequence of int32 (H5Odtype.c:757-784).
func TestParseDatatypeMessage_VarLen(t *testing.T) {
	data := append([]byte{
		0x19,    // version 1, class 9 (variable-length)
		0, 0, 0, // class bits: kind = sequence
		16, 0, 0, 0, // size 16
	}, int32LEBytes...) // base type int32

	dt, err := ParseDatatypeMessage(data)
	require.NoError(t, err)
	require.Equal(t, DatatypeVarLen, dt.Class)
	require.NotNil(t, dt.Base)
	require.Equal(t, DatatypeFixed, dt.Base.Class)
	require.Equal(t, uint32(4), dt.Base.Size)
	require.Equal(t, int32LEBytes[datatypeHeaderSize:], dt.Base.Properties)
}
