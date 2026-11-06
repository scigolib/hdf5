package hdf5

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNamedDatatype_DatatypeInfo reads the linked committed datatypes of
// h5repack_named_dtypes.h5 (C-written; h5dump -H shows "/M/M/A"
// H5T_STD_I16BE and "/N" H5T_STD_I32LE; its "#800" is an unlinked committed
// type that only h5dump names) and checks that DatatypeInfo describes each as
// H5Tget_class, H5Tget_size, H5Tget_order and H5Tget_sign do on the type
// H5Topen returns. Dataset "/O" uses "/N", so its Datatype must be the same
// description.
func TestNamedDatatype_DatatypeInfo(t *testing.T) {
	f, err := Open("testdata/hdf5_official/h5repack_named_dtypes.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	objects := map[string]Object{}
	f.Walk(func(p string, obj Object) { objects[p] = obj })

	want := map[string]DatatypeInfo{
		"/M/M/A": {Class: ClassInteger, Size: 2, Order: OrderBigEndian, Signed: true},
		"/N":     {Class: ClassInteger, Size: 4, Order: OrderLittleEndian, Signed: true},
	}
	for path, w := range want {
		named, ok := objects[path].(*NamedDatatype)
		require.True(t, ok, "%s must be a named datatype, got %T (walked: %v)", path, objects[path], walkedPaths(objects))

		got, err := named.DatatypeInfo()
		require.NoError(t, err, path)
		assert.Equal(t, w, got, path)
	}

	ds, ok := objects["/O"].(*Dataset)
	require.True(t, ok, "/O must be a dataset, got %T", objects["/O"])
	dsInfo, err := ds.Datatype()
	require.NoError(t, err)
	namedInfo, err := objects["/N"].(*NamedDatatype).DatatypeInfo()
	require.NoError(t, err)
	assert.Equal(t, namedInfo, dsInfo, "a dataset of a committed type describes that type")
}

// walkedPaths lists the paths Walk reported, for failure messages.
func walkedPaths(objects map[string]Object) []string {
	paths := make([]string, 0, len(objects))
	for p := range objects {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	return paths
}

// TestNamedDatatype_DatatypeInfoUndecodable: the zero-member committed
// compound of bad_compound.h5 does not decode (H5Odtype.c:358-360), and C
// fails H5Topen2 on it (test/dtypes.c:3847-3870); DatatypeInfo returns the
// decoder's error, which marks the bytes as corrupt.
func TestNamedDatatype_DatatypeInfoUndecodable(t *testing.T) {
	f, err := Open("testdata/reference/bad_compound.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var named *NamedDatatype
	f.Walk(func(p string, obj Object) {
		if p == "/cmpnd" {
			named, _ = obj.(*NamedDatatype)
		}
	})
	require.NotNil(t, named, "/cmpnd must be listed as a named datatype")

	_, err = named.DatatypeInfo()
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Contains(t, err.Error(), "cmpnd")
}
