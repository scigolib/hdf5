package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// committedReader reads one dataset whose datatype is a committed datatype and
// checks its values against h5dump 1.14.6 output (TASK-068).
type committedCheck func(t *testing.T, ds *Dataset)

func readsFloats(want ...float64) committedCheck {
	return func(t *testing.T, ds *Dataset) {
		t.Helper()
		got, err := ds.Read()
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(got), len(want))
		require.Equal(t, want, got[:len(want)])
	}
}

func readsCompound(n, idx int, member string, want float64) committedCheck {
	return func(t *testing.T, ds *Dataset) {
		t.Helper()
		got, err := ds.ReadCompound()
		require.NoError(t, err)
		require.Len(t, got, n)
		v, ok := got[idx][member]
		require.True(t, ok, "member %q missing in %v", member, got[idx])
		require.InDelta(t, want, toFloat(t, v), 1e-6)
	}
}

// readsVLen checks the two sequences every named_vl dataset holds.
func readsVLen() committedCheck {
	return func(t *testing.T, ds *Dataset) {
		t.Helper()
		got, err := ds.ReadVLenBytes()
		require.NoError(t, err)
		require.Len(t, got, 2)
		// h5dump: (0): (1), (2, 3) — int32 little-endian sequences.
		require.Equal(t, []byte{1, 0, 0, 0}, got[0])
		require.Equal(t, []byte{2, 0, 0, 0, 3, 0, 0, 0}, got[1])
	}
}

func toFloat(t *testing.T, v interface{}) float64 {
	t.Helper()
	switch x := v.(type) {
	case int8:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint8:
		return float64(x)
	case uint16:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case float32:
		return float64(x)
	case float64:
		return x
	}
	t.Fatalf("unexpected member value %T %v", v, v)
	return 0
}

// committedTargets are the 24 datasets of the official suite whose datatype
// message is a committed-datatype reference (measured for TASK-064), with the
// values h5dump prints for them.
var committedTargets = map[string]map[string]committedCheck{
	"h5copytst.h5": {
		"/named_vl": readsVLen(), "/grp_dsets/named_vl": readsVLen(), "/grp_nested/grp_dsets/named_vl": readsVLen(),
	},
	"h5copytst_new.h5": {
		"/named_vl": readsVLen(), "/grp_dsets/named_vl": readsVLen(), "/grp_nested/grp_dsets/named_vl": readsVLen(),
	},
	"h5repack_named_dtypes.h5": {
		"/A": readsFloats(0, 0, 0), "/M/M/B": readsFloats(0, 0, 0), "/O": readsFloats(0, 0, 0),
	},
	"h5repack_nested_8bit_enum.h5": {"/tracks/1/trace": readsCompound(642, 0, "evaluation_number", 233)},
	// Stored through the N-bit filter (h5dump -p: NBIT, DEFLATE), which has no
	// decoder here yet: the read must fail and say so, never return the packed
	// bytes as values (H5Z.c:1408-1434).
	"h5repack_nested_8bit_enum_deflated.h5": {"/tracks/1/trace": func(t *testing.T, ds *Dataset) {
		t.Helper()
		_, err := ds.ReadCompound()
		require.ErrorIs(t, err, core.ErrFilterNotSupported)
		require.ErrorContains(t, err, "N-bit")
	}},
	// h5dump refuses this file (it is a refcount error case); h5ls -d prints 0..9.
	"h5stat_err_refcount.h5": {"/dset": readsFloats(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)},
	"tcompound.h5": {
		"/group1/dset2": readsCompound(5, 1, "int_name", 1),
		"/group1/dset3": func(t *testing.T, ds *Dataset) {
			t.Helper()
			got, err := ds.ReadCompound()
			require.NoError(t, err)
			require.Len(t, got, 18)
		},
		"/group1/dset4": readsCompound(5, 0, "float", 3),
		"/group2/dset5": readsCompound(5, 1, "int", 1),
	},
	"tcompound2.h5": {
		"/group1/dset2": readsCompound(6, 1, "int_name", 1),
		"/group1/dset4": readsCompound(6, 1, "int", 1),
		"/group2/dset5": readsCompound(6, 1, "int", 1),
	},
	"tenum.h5":             {"/table": readsFloats(0, 1, 2, 1, 3, 3, 4, 1, 2, 0)},
	"tfcontents1.h5":       {"/dsetmytype2": readsFloats(1, 2, 3, 4)},
	"tnamed_dtype_attr.h5": {"/Dataset": readsFloats(0)},
	"tnestedcmpddt.h5": {
		"/dset4": readsFloats(0, 1, 2, 1, 3, 2),
		"/dset5": readsCompound(6, 1, "int_name", 1),
	},
}

// TestCommittedDatatypes_OfficialSuite reads every dataset of the official
// suite whose datatype is committed (red before TASK-068: "committed (shared)
// datatype not supported yet" or a datatype decoded from the reference bytes).
func TestCommittedDatatypes_OfficialSuite(t *testing.T) {
	total := 0
	for file, targets := range committedTargets {
		t.Run(file, func(t *testing.T) {
			f, err := Open(filepath.Join("testdata", "hdf5_official", file))
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()

			seen := map[string]bool{}
			f.Walk(func(path string, obj Object) {
				ds, ok := obj.(*Dataset)
				if !ok {
					return
				}
				check, ok := targets[path]
				if !ok {
					return
				}
				seen[path] = true
				t.Run(path, func(t *testing.T) { check(t, ds) })
			})
			for path := range targets {
				require.True(t, seen[path], "dataset %s not found", path)
			}
		})
		total += len(targets)
	}
	require.Equal(t, 24, total)
}

// TestCommittedDatatypes_Attributes reads attributes whose datatype is a
// committed datatype: compact attributes on datasets and groups, with h5dump
// values (tnamed_dtype_attr.h5: 8; h5repack_named_dtypes.h5: 0, 0, 0;
// tvlstr.h5: a variable-length string).
func TestCommittedDatatypes_Attributes(t *testing.T) {
	read := func(t *testing.T, file string, fn func(f *File)) {
		t.Helper()
		f, err := Open(filepath.Join("testdata", "hdf5_official", file))
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		fn(f)
	}

	datasetAttr := func(t *testing.T, f *File, dsPath, name string) interface{} {
		t.Helper()
		var v interface{}
		var found bool
		f.Walk(func(path string, obj Object) {
			if ds, ok := obj.(*Dataset); ok && path == dsPath {
				var err error
				v, err = ds.ReadAttribute(name)
				require.NoError(t, err, "%s attribute %s", dsPath, name)
				found = true
			}
		})
		require.True(t, found, "dataset %s not found", dsPath)
		return v
	}

	groupAttr := func(t *testing.T, f *File, gPath, name string) interface{} {
		t.Helper()
		var v interface{}
		var found bool
		f.Walk(func(path string, obj Object) {
			g, ok := obj.(*Group)
			if !ok || path != gPath {
				return
			}
			attrs, err := g.Attributes()
			require.NoError(t, err)
			for _, a := range attrs {
				if a.Name == name {
					v, err = a.ReadValue()
					require.NoError(t, err)
					found = true
				}
			}
		})
		require.True(t, found, "attribute %s on group %s not found", name, gPath)
		return v
	}

	t.Run("tnamed_dtype_attr", func(t *testing.T) {
		read(t, "tnamed_dtype_attr.h5", func(f *File) {
			require.EqualValues(t, 8, datasetAttr(t, f, "/Dataset", "Attribute"))
			require.EqualValues(t, 8, groupAttr(t, f, "/g1/", "Attribute"))
		})
	})
	t.Run("h5repack_named_dtypes", func(t *testing.T) {
		read(t, "h5repack_named_dtypes.h5", func(f *File) {
			for _, a := range []struct{ ds, name string }{{"/A", "A"}, {"/M/M/B", "A"}, {"/M/M/B", "B"}} {
				v := datasetAttr(t, f, a.ds, a.name)
				require.NotNil(t, v, "%s %s", a.ds, a.name)
			}
		})
	})
	t.Run("tvlstr", func(t *testing.T) {
		read(t, "tvlstr.h5", func(f *File) {
			require.Equal(t, "This is the string for the attribute", groupAttr(t, f, "/", "test_scalar"))
		})
	})
}
