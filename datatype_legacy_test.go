package hdf5

// Files written by hdf5-go before v0.14.3 carry datatypes the HDF5 C library
// rejects (testdata/reference/legacy-hdf5go/README.md). The reader keeps
// reading them through two narrow legacy paths of the datatype codec
// (ADR-004): the old compound encoding (version 3, member count 0 in the class
// bit field, H5Odtype.c:358-360) and the old float attribute encoding (sign
// bit position 0, H5Odtype.c:264-267).

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

func openLegacy(t *testing.T, name string) *Dataset {
	t.Helper()
	f, err := Open(filepath.Join("testdata", "reference", "legacy-hdf5go", name))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	var ds *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && ds == nil {
			ds = d
		}
	})
	require.NotNil(t, ds, "no dataset in %s", name)
	return ds
}

// TestLegacyCompound_ReadBack reads the compound datasets the old writer
// produced, including a string member followed by another member, which the
// old reader itself could not read back.
func TestLegacyCompound_ReadBack(t *testing.T) {
	cases := map[string][]core.CompoundValue{
		"numeric.h5": {
			{"id": int32(1), "value": 0.5},
			{"id": int32(2), "value": 1.5},
		},
		"with_string.h5": {
			{"id": int32(1), "value": 0.5, "tag": "row0"},
			{"id": int32(2), "value": 1.5, "tag": "row1"},
		},
		"kv_strings.h5": {
			{"key": "detector", "value": "H1"},
		},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			ds := openLegacy(t, name)
			got, err := ds.ReadCompound()
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// TestLegacyFloatAttribute_ReadBack reads float attributes the old writer
// produced with a sign bit position of 0.
func TestLegacyFloatAttribute_ReadBack(t *testing.T) {
	ds := openLegacy(t, "float_attrs.h5")

	gain, err := ds.ReadAttribute("gain")
	require.NoError(t, err)
	require.Equal(t, 1.5, gain)

	weights, err := ds.ReadAttribute("weights")
	require.NoError(t, err)
	require.Equal(t, []float32{0.25, 0.75}, weights)
}
