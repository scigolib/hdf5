package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAttributeReadValueStringBounds proves that the fixed-length string branch
// of ReadValue validates the dataspace-derived byte count against the bytes
// actually stored in the attribute BEFORE allocating the string slice
// (ADR-003, issue #68). Mirrors H5O__attr_decode, where the overflow check and
// H5_IS_BUFFER_OVERFLOW precede H5FL_BLK_MALLOC (H5Oattr.c:274-283).
//
// The claimed count is 1M elements of 8 bytes, so the pre-fix code allocated a
// 16 MB string slice and only then failed per element with
// "data too short for string element 0". That keeps the RED run safe while the
// assertion still distinguishes the two errors.
func TestAttributeReadValueStringBounds(t *testing.T) {
	attr := &Attribute{
		Name: "huge_fixed_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          8,
			ClassBitField: 0x00, // null-terminated
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1_000_000},
		},
		Data: make([]byte, 16),
	}

	val, err := attr.ReadValue()
	require.Error(t, err)
	require.Nil(t, val)
	require.Contains(t, err.Error(), "attribute data size mismatch")
	require.Contains(t, err.Error(), "need 8000000 bytes, have 16")
}

// TestAttributeReadValueStringOverflow covers the multiplication-overflow guard
// on the same branch.
//
// Post-fix only: it is excluded from the RED run because on the pre-fix code
// the element count reaches make([]string, math.MaxUint64/2), which is a fatal
// allocation failure rather than a reportable test failure. Run it only after
// the guard exists.
func TestAttributeReadValueStringOverflow(t *testing.T) {
	attr := &Attribute{
		Name: "overflowing_fixed_string",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          8,
			ClassBitField: 0x00, // null-terminated
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{math.MaxUint64 / 2},
		},
		Data: make([]byte, 16),
	}

	val, err := attr.ReadValue()
	require.Error(t, err)
	require.Nil(t, val)
	require.Contains(t, err.Error(), "attribute size overflow (string)")
}

// TestAttributeReadValueStringBoundsValid pins the behavior the guard must not
// change: a well-formed fixed-length string array still decodes element by
// element.
func TestAttributeReadValueStringBoundsValid(t *testing.T) {
	attr := &Attribute{
		Name: "string_array",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Size:          4,
			ClassBitField: 0x00, // null-terminated
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{3},
		},
		Data: []byte("abc\x00def\x00ghi\x00"),
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def", "ghi"}, val)
}
