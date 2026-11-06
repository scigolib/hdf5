package hdf5

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// h5stat_newgrat.h5 (official C test suite, h5stat) keeps 100 attributes of
// /DATASET_NAME in a dense name index of depth 1 and the 35,001 links of the
// root group in a dense name index of depth 3, both with fractal heaps whose
// root is an indirect block. The root indirect blocks' addresses and sizes
// from h5debug of the heap headers: the attribute heap's root has 1 row of 4
// entries and 5-byte heap offsets (a 40-bit heap), the link heap's 16 rows
// and 4-byte offsets (32 bits); each block ends with its checksum.
const (
	newgratFile       = "testdata/hdf5_official/h5stat_newgrat.h5"
	newgratRootLinks  = 35001
	newgratDatasetAtt = 100

	newgratAttrHeapRoot     = 6311826
	newgratAttrHeapRootSize = 5 + 4 + 8 + 5 + 1*4*8
	newgratLinkHeapRoot     = 6361522
	newgratLinkHeapRootSize = 5 + 4 + 8 + 4 + 16*4*8
)

// TestDenseStorage_DeepCIndexes: dense attribute and link indexes deeper than
// a leaf are read whole, with every record decoded by its type, and their
// heap objects found through the heap's indirect blocks. Red before TASK-058:
// the old reader read only the root node as a leaf (a depth-1 or depth-3 root
// is an internal node), and the heap reader handled only a direct root block.
func TestDenseStorage_DeepCIndexes(t *testing.T) {
	f, err := Open(newgratFile)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	// h5ls lists DATASET_NAME and the groups GROUP1 to GROUP35000.
	children := f.Root().Children()
	require.Len(t, children, newgratRootLinks, "h5ls lists 35,001 links in /")
	wantLinks := make(map[string]bool, newgratRootLinks)
	wantLinks["DATASET_NAME"] = true
	for i := 1; i < newgratRootLinks; i++ {
		wantLinks[fmt.Sprintf("GROUP%d", i)] = true
	}
	for _, c := range children {
		require.True(t, wantLinks[c.Name()], "link %q is not in h5ls's listing", c.Name())
		delete(wantLinks, c.Name())
	}
	require.Empty(t, wantLinks, "links h5ls lists and the reader did not return")

	ds, ok := findDatasetByName(f, "DATASET_NAME")
	require.True(t, ok)
	names, err := ds.ListAttributes()
	require.NoError(t, err)
	want := make([]string, 0, newgratDatasetAtt)
	for i := 1; i <= newgratDatasetAtt; i++ {
		want = append(want, fmt.Sprintf("ATTR%d", i))
	}
	require.ElementsMatch(t, want, names)

	for _, name := range []string{"ATTR1", "ATTR57", "ATTR100"} {
		v, err := ds.ReadAttribute(name)
		require.NoError(t, err)
		require.EqualValues(t, int32(0), v, "h5dump: every attribute is 0")
	}
}

// TestDenseStorage_CorruptHeapIndirectBlock: a damaged root indirect block of
// either heap of h5stat_newgrat.h5 fails the read with ErrCorrupt instead of
// returning objects found through wrong addresses.
func TestDenseStorage_CorruptHeapIndirectBlock(t *testing.T) {
	raw, err := os.ReadFile(newgratFile)
	require.NoError(t, err)
	corrupt := func(t *testing.T, at, size int) string {
		t.Helper()
		img := bytes.Clone(raw)
		require.Equal(t, "FHIB", string(img[at:at+4]))
		img[at+size-1] ^= 0xFF // the checksum's last byte
		path := filepath.Join(t.TempDir(), "newgrat-corrupt.h5")
		require.NoError(t, os.WriteFile(path, img, 0o600))

		return path
	}

	t.Run("attribute heap", func(t *testing.T) {
		f, err := Open(corrupt(t, newgratAttrHeapRoot, newgratAttrHeapRootSize))
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		ds, ok := findDatasetByName(f, "DATASET_NAME")
		require.True(t, ok)
		_, err = ds.ListAttributes()
		require.ErrorIs(t, err, ErrCorrupt)
		require.Contains(t, err.Error(), fmt.Sprintf("indirect block at 0x%X has checksum", newgratAttrHeapRoot))
	})

	t.Run("link heap", func(t *testing.T) {
		f, err := Open(corrupt(t, newgratLinkHeapRoot, newgratLinkHeapRootSize))
		if err == nil {
			require.NoError(t, f.Close())
		}
		require.ErrorIs(t, err, ErrCorrupt)
		require.Contains(t, err.Error(), fmt.Sprintf("indirect block at 0x%X has checksum", newgratLinkHeapRoot))
	})
}

// TestDenseStorage_LegacyType5AttributeIndex reads a dense attribute index
// this library wrote up to v0.14.2 with link name records (type 5) in the
// attribute name slot, which the C library refuses (README in
// testdata/reference/legacy-hdf5go). The reader keeps reading it.
func TestDenseStorage_LegacyType5AttributeIndex(t *testing.T) {
	f, err := Open("testdata/reference/legacy-hdf5go/dense_attr_type5.h5")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	ds, ok := findDatasetByName(f, "d")
	require.True(t, ok)
	names, err := ds.ListAttributes()
	require.NoError(t, err)
	require.Len(t, names, 12)
	for i := range 12 {
		v, err := ds.ReadAttribute(fmt.Sprintf("a%02d", i))
		require.NoError(t, err)
		require.EqualValues(t, int32(i), v)
	}
}
