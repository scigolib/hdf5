package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConvertToFloat64Bounds proves that convertToFloat64 rejects a
// dataspace-derived element count that exceeds the bytes actually read BEFORE
// allocating the []float64 result (ADR-003, issue #68).
//
// The claimed size is 1M elements = 8 MB, small enough that the pre-fix code
// allocates, then fails on the truncated element with the old message
// "data truncated (float64)". The assertion on both numbers ("need … have …")
// is what distinguishes the pre-fix run from the guarded one.
func TestConvertToFloat64Bounds(t *testing.T) {
	rawData := make([]byte, 8) // exactly one float64 element
	dt := &DatatypeMessage{
		Class:         DatatypeFloat,
		Size:          8,
		ClassBitField: 0x00, // little-endian
	}

	got, err := convertToFloat64(rawData, dt, 1<<20)
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "data truncated")
	require.Contains(t, err.Error(), "need 8388608 bytes for 1048576 elements, have 8")
}

// TestConvertToFloat64HugeCountBounded covers the corrupt-metadata case from
// issue #68 proper: 1<<40 elements at 8 bytes each is an 8 TB allocation.
//
// Post-fix only, and therefore excluded from the RED run
// (-run 'TestConvertToFloat64Bounds$'): on the pre-fix code the allocation is
// fatal, so the test process would die instead of reporting a failure.
func TestConvertToFloat64HugeCountBounded(t *testing.T) {
	rawData := make([]byte, 8)
	dt := &DatatypeMessage{
		Class:         DatatypeFloat,
		Size:          8,
		ClassBitField: 0x00,
	}

	got, err := convertToFloat64(rawData, dt, 1<<40)
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "data truncated")
}

// TestConvertToFloat64BoundsOverflow covers the SafeMultiply overflow guard.
// Post-fix only: the pre-fix code would reach the fatal allocation.
func TestConvertToFloat64BoundsOverflow(t *testing.T) {
	rawData := make([]byte, 8)
	dt := &DatatypeMessage{
		Class:         DatatypeFixed,
		Size:          8,
		ClassBitField: 0x00,
	}

	got, err := convertToFloat64(rawData, dt, math.MaxUint64/4)
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "data size overflow (convert to float64)")
}

// TestConvertToFloat64BoundsUnsupportedKeepsError pins the error precedence:
// a datatype the converter does not handle must keep reporting "unsupported
// datatype" even when rawData is far shorter than numElements*Size, because
// its Size does not describe a convertible element width.
func TestConvertToFloat64BoundsUnsupportedKeepsError(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          8,
		ClassBitField: 0x00,
	}

	got, err := convertToFloat64([]byte{0x00}, dt, 2)
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "unsupported datatype for conversion to float64")
}
