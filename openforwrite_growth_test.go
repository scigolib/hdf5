package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestOpenForWrite_ReadSideFollowsWriterEOF is the ADR-003 regression test for
// the read side of a read-modify-write session. Open used to install a reader
// bounded by the size captured at that moment, so once the session grew the
// file every read through the File was measured against a stale limit. The
// read side must follow the writer's live end of file instead — the
// H5FD_get_eoa analog — so that a region check can never reject data the
// session itself has just written.
func TestOpenForWrite_ReadSideFollowsWriterEOF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "growth.h5")

	func() {
		fw, err := CreateForWrite(path, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/seed", Float64, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2}))
		require.NoError(t, fw.Close())
	}()

	info, err := os.Stat(path)
	require.NoError(t, err)
	sizeAtOpen := uint64(info.Size())

	values := make([]float64, 4096)
	for i := range values {
		values[i] = float64(i)
	}

	fw, err := OpenForWrite(path, OpenReadWrite)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/grown", Float64, []uint64{uint64(len(values))})
	require.NoError(t, err)
	require.NoError(t, ds.Write(values))

	// The read side must report the writer's current end of file, which the
	// session has moved past the size captured by Open.
	got, ok := utils.ReaderSize(fw.file.reader)
	require.True(t, ok, "the read side must report its size after the file grew")
	require.Equal(t, fw.writer.EndOfFile(), got)
	require.Greater(t, got, sizeAtOpen, "the file should have grown in this test")

	require.NoError(t, fw.Close())

	// The written data survives a normal reopen.
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(p string, obj Object) {
		if p == "/grown" {
			found = obj.(*Dataset)
		}
	})
	require.NotNil(t, found, "/grown missing after reopen")

	data, err := found.Read()
	require.NoError(t, err)
	require.Equal(t, values, data)
}
