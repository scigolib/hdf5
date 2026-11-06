package hdf5

// Regression test for a header rewrite that dropped object header message
// flags: writing an attribute to a C-written dataset whose datatype is a
// committed (shared) datatype rewrote the header with message flags 0, so
// the shared-message reference (H5O_MSG_FLAG_SHARED, H5Ocache.c:1277-1279)
// was read back as an inline datatype and the dataset became unreadable.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// sharedTypeDataset is an official-suite dataset whose datatype message is a
// shared reference to the committed datatype /vl (h5dump -d /named_vl); the
// file uses version-2 object headers, which the writer can modify.
const (
	sharedTypeFile    = "h5copytst.h5"
	sharedTypeDataset = "/named_vl"
)

// copySharedTypeFile copies the fixture to a temporary file.
func copySharedTypeFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hdf5_official", sharedTypeFile))
	require.NoError(t, err)
	dst := filepath.Join(t.TempDir(), sharedTypeFile)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
	return dst
}

// TestWriteAttribute_SharedDatatypeStaysReadable writes an attribute to a
// dataset with a committed datatype and requires the C library to read the
// dataset afterwards.
func TestWriteAttribute_SharedDatatypeStaysReadable(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	file := copySharedTypeFile(t)

	before := runH5(t, h5dump, "-d", sharedTypeDataset, file)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset(sharedTypeDataset)
	require.NoError(t, err)
	require.NoError(t, ds.WriteAttribute("note", int32(7)))
	require.NoError(t, fw.Close())

	after := runH5(t, h5dump, "--enable-error-stack", "-d", sharedTypeDataset, file)
	require.Contains(t, after, `DATATYPE  "/vl"`, "datatype must still be the committed one:\n%s", after)
	require.Contains(t, before, `DATATYPE  "/vl"`)
	require.Contains(t, after, `ATTRIBUTE "note"`)
}
