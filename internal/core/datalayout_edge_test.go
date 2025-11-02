package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDataLayoutMessage_Chunked_V3 tests chunked layout v3.
func TestParseDataLayoutMessage_Chunked_V3(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Version 3, chunked layout
	data := []byte{
		3, // version
		2, // class: chunked
		// Dimensionality (1 byte)
		2,
		// Dimension sizes (2 * 4 bytes)
		10, 0, 0, 0, // chunk dim 1
		10, 0, 0, 0, // chunk dim 2
		// Dataset element size (4 bytes)
		4, 0, 0, 0,
		// B-tree address (8 bytes)
		0x10, 0, 0, 0, 0, 0, 0, 0,
	}

	layout, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.True(t, layout.IsChunked())
}

// TestParseDataLayoutMessage_Compact tests compact layout.
func TestParseDataLayoutMessage_Compact(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Version 3, compact layout
	data := []byte{
		3, // version
		0, // class: compact
		// Size (2 bytes)
		16, 0,
		// Data (16 bytes)
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
	}

	layout, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.True(t, layout.IsCompact())
}

// TestParseDataLayoutMessage_InvalidVersion tests error handling.
func TestParseDataLayoutMessage_InvalidVersion(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	data := []byte{
		99, // invalid version
		1,  // class
	}

	_, err := ParseDataLayoutMessage(data, sb)
	require.Error(t, err)
}
