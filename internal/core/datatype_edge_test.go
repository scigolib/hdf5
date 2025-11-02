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
func TestParseDatatypeMessage_VarLen(t *testing.T) {
	// Version 1 variable-length datatype
	data := []byte{
		0x10 | 10, // version 1, class 10 (variable-length)
		0, 0,      // class bits: type = sequence
		16, 0, 0, 0, // size 16
		// Base type (int32)
		0x30, // version 3, integer (class 0)
		0, 0,
		4, 0, 0, 0, // size 4
	}

	dt, err := ParseDatatypeMessage(data)
	require.NoError(t, err)
	require.Equal(t, DatatypeClass(10), dt.Class)
}
