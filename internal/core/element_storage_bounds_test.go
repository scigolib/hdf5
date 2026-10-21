package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateElementStorage covers the shared guard that rejects an element
// count larger than the bytes actually read.
func TestValidateElementStorage(t *testing.T) {
	data := make([]byte, 64)

	require.NoError(t, validateElementStorage(4, 16, data, "test"))
	require.NoError(t, validateElementStorage(0, 16, data, "test"))
	require.NoError(t, validateElementStorage(0, 16, nil, "test"))

	require.Error(t, validateElementStorage(5, 16, data, "test")) // 80 > 64
	require.Error(t, validateElementStorage(1, 0, data, "test"))  // zero element size
	require.Error(t, validateElementStorage(math.MaxUint64, 2, data, "test"))
}

// TestParseCompoundData_RejectsCountWithoutData is the regression for the input
// FuzzOpenFile captured: a compact or empty layout supplies no bytes while the
// dataspace claims billions of elements. On the pre-fix code this reaches
// make([]CompoundValue, 3.3e12) — a terabyte allocation inside the runtime's
// non-preemptible large-allocation path, so it cannot be shown red safely; it
// would hang the test binary instead of failing.
func TestParseCompoundData_RejectsCountWithoutData(t *testing.T) {
	ct := &CompoundType{
		Size: 16,
		Members: []CompoundMember{
			{Name: "v", Offset: 0, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
		},
	}

	_, err := parseCompoundData(nil, ct, 1<<40, nil, &Superblock{OffsetSize: 8})
	require.Error(t, err)
	require.Contains(t, err.Error(), "data size mismatch")
}
