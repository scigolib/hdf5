package hdf5

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCInterop_DenseLinksCreatedWhileWriting: a group whose links move into
// dense storage while the file is written (the ninth link of a group that
// keeps link messages) leaves a file the C library opens, with every link.
// Red before the fix (v0.14.3 too): the new name index leaf was written at
// its used size although a full node was allocated; when the leaf was the
// last block the file ended before its end-of-file address, h5dump refused
// to open it, and the next link in the same session failed to read the leaf.
func TestCInterop_DenseLinksCreatedWhileWriting(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dense.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/p")
	require.NoError(t, err)
	// An external link turns /p into a group of link messages.
	require.NoError(t, fw.CreateExternalLink("/p/ext", "other.h5", "/x"))
	for i := range 10 {
		ds, err := fw.CreateDataset(fmt.Sprintf("/p/d%d", i), Int32, []uint64{1})
		require.NoError(t, err, "d%d", i)
		require.NoError(t, ds.Write([]int32{int32(i)}))
	}
	require.True(t, groupLinkInfo(t, fw, "/p").HasFractalHeap(), "/p is not dense")
	require.NoError(t, fw.Close())

	values := datasetValues(t, file)
	for i := range 10 {
		require.Equal(t, []float64{float64(i)}, values[fmt.Sprintf("/p/d%d", i)])
	}
	out := requireH5dumpClean(t, file)
	if out != "" {
		require.Contains(t, out, `DATASET "d9"`)
	}
}
