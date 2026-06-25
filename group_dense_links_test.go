package hdf5

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Dense link storage tests (via a NetCDF-4 fixture whose root group exceeds
// the 8-link compact threshold, so HDF5 stores links in a fractal heap +
// v2 B-tree). This exercises the ReadDenseHeapObjects path in group.go and,
// because the fixture's heap header has the ChecksumDirBlocks flag set, the
// checksummed direct-block branch in attribute.go.
// ---------------------------------------------------------------------------

// TestDenseLinks_RootGroup opens testdata/dense_links.h5 and verifies all 16
// dense links are read back through the fractal-heap path.
func TestDenseLinks_RootGroup(t *testing.T) {
	f, err := Open("testdata/dense_links.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	children := f.Root().Children()
	require.Len(t, children, 16, "root group should expose all 16 dense links")

	names := make([]string, len(children))
	for i, c := range children {
		names[i] = c.Name()
	}
	sort.Strings(names)

	want := []string{
		"v00", "v01", "v02", "v03", "v04", "v05", "v06", "v07",
		"v08", "v09", "v10", "v11", "v12", "v13", "v14", "v15",
	}
	require.Equal(t, want, names)
}
