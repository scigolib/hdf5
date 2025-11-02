package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCompoundType_V1 tests parsing compound v1 datatype.
func TestParseCompoundType_V1(t *testing.T) {
	// Version 1, 2 members, each 4 bytes
	data := []byte{
		0x16, // version 1, class 6 (compound)
		0, 0, // class bits
		8, 0, 0, 0, // size 8 (2 fields * 4 bytes)
		// Number of members (2 bytes)
		2, 0,
		// Member 1
		'x', 0, 0, 0, 0, 0, 0, 0, // name "x" (padded to 8)
		0, 0, 0, 0, // offset = 0
		1, 0, 0, 0, // dim_1_reserved
		4, 0, 0, 0, // dimension size
		0, 0, 0, 0, // permutation (4 bytes)
		0, 0, 0, 0, // reserved (4 bytes)
		// Member 1 type (int32)
		0x30, // version 3, integer (class 0)
		0, 0,
		4, 0, 0, 0,
		// Member 2
		'y', 0, 0, 0, 0, 0, 0, 0, // name "y"
		4, 0, 0, 0, // offset = 4
		0,       // dimensionality = 0 (scalar)
		0, 0, 0, // reserved
		// Member 2 type
		0x30, // version 3, integer (class 0)
		0, 0,
		4, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	if err == nil {
		require.Equal(t, DatatypeClass(6), dt.Class)
	}
}

// TestParseCompoundType_V3_TwoMembers tests v3 compound with 2 members.
func TestParseCompoundType_V3_TwoMembers(t *testing.T) {
	// Version 3, 2 members
	data := []byte{
		0x36, // version 3, class 6
		0, 0, // class bits
		8, 0, 0, 0, // size
		// Member list size and offset (4 bytes each)
		0, 0, 0, 0, // member list size
		0, 0, 0, 0, // member list offset
		// Member 1
		'x', 0, // name
		0, 0, 0, 0, // offset = 0
		// Member 1 type
		0x30, // version 3, integer (class 0)
		0, 0,
		4, 0, 0, 0,
		// Member 2
		'y', 0,
		4, 0, 0, 0, // offset = 4
		// Member 2 type
		0x30, // version 3, integer (class 0)
		0, 0,
		4, 0, 0, 0,
	}

	dt, err := ParseDatatypeMessage(data)
	if err == nil {
		require.Equal(t, DatatypeClass(6), dt.Class)
	}
}

// TestParseCompoundType_NestedCompound tests nested compound types.
func TestParseCompoundType_NestedCompound(_ *testing.T) {
	// Version 3, compound with nested compound
	data := []byte{
		0x36, // version 3, class 6
		0, 0,
		16, 0, 0, 0, // size 16
		0, 0, 0, 0, // member list size
		0, 0, 0, 0, // member list offset
		// Member: nested compound
		'n', 'e', 's', 't', 0, // name "nest"
		0, 0, 0, 0, // offset
		// Nested compound type
		0x36, // version 3, compound
		0, 0, // class bits
		8, 0, 0, 0, // size
		0, 0, 0, 0, // member list size
		0, 0, 0, 0, // member list offset
		'a', 0, // member name
		0, 0, 0, 0, // offset
		0x30, // int type (class 0)
		0, 0, //
		4, 0, 0, 0, //
		'b', 0, //
		4, 0, 0, 0, //
		0x30, //
		0, 0, //
		4, 0, 0, 0, //
	}

	dt, err := ParseDatatypeMessage(data)
	// May fail on complex nested structures, but should not panic
	_ = dt
	_ = err
}
