package hdf5

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBadCompound_MirrorsC covers testdata/reference/bad_compound.h5
// (gen_bad_compound.c): a committed compound datatype "cmpnd" with zero
// members, a dataset using it and a group. The datatype decoder rejects the
// zero-member compound as C does (H5Odtype.c:358-360). C still opens the file,
// fails H5Topen2("cmpnd") and H5Dopen2("dataset") (test/dtypes.c:3847-3870),
// and h5ls lists all three objects with the type and the dataset as *ERROR*.
//
// Here: Open and Walk succeed and list all three objects; "cmpnd" has no
// datatype; reading /dataset fails with the decoder's error.
//
// Red proof: with the strict decoder of TASK-064 and the former loadObject,
// Open failed ("failed to parse named datatype"), because the committed type was
// decoded eagerly and its error returned to the group load.
func TestBadCompound_MirrorsC(t *testing.T) {
	f, err := Open("testdata/reference/bad_compound.h5")
	require.NoError(t, err, "C opens bad_compound.h5; so must we")
	defer func() { _ = f.Close() }()

	paths := map[string]Object{}
	f.Walk(func(p string, obj Object) { paths[p] = obj })

	require.Contains(t, paths, "/group/") // Walk reports group paths with a trailing slash.

	named, ok := paths["/cmpnd"].(*NamedDatatype)
	require.True(t, ok, "/cmpnd must be listed as a named datatype, got %T", paths["/cmpnd"])
	require.Nil(t, named.Datatype(), "an undecodable committed datatype has no datatype")

	ds, ok := paths["/dataset"].(*Dataset)
	require.True(t, ok, "/dataset must be listed, got %T", paths["/dataset"])
	_, err = ds.Read()
	require.Error(t, err, "reading a dataset whose datatype does not decode must fail")
}
