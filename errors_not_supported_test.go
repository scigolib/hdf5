package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The errors below refuse valid HDF5 that this library does not handle yet,
// so a caller must be able to tell them from damage with errors.Is
// (ErrNotSupported, errors.go). Red before the v0.14.3 polish: each was a
// plain errors.New/fmt.Errorf text that wrapped no sentinel.

// TestNotSupported_OpenForWriteUserBlock: a file with a user block (a valid
// HDF5 layout, the C library writes it with H5Pset_userblock) cannot be
// opened for writing yet.
func TestNotSupported_OpenForWriteUserBlock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "twithub.h5")
	data, err := os.ReadFile("testdata/hdf5_official/twithub.h5")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, data, 0o600))

	_, err = OpenForWrite(target, OpenReadWrite)
	require.ErrorIs(t, err, ErrNotSupported)
	require.NotErrorIs(t, err, ErrCorrupt)
}

// TestNotSupported_CommittedDatatypeData: the data of a dataset whose
// datatype is committed (h5copytst_new.h5 /named_vl uses "/vl") cannot be
// written yet.
func TestNotSupported_CommittedDatatypeData(t *testing.T) {
	file := copySharedTypeFile(t)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	ds, err := fw.OpenDataset(sharedTypeDataset)
	require.NoError(t, err)
	err = ds.WriteRaw(make([]byte, 32))
	require.ErrorIs(t, err, ErrNotSupported)
	require.NotErrorIs(t, err, ErrCorrupt)
}

// TestNotSupported_CompoundMemberType: a compound member of a variable-length
// or opaque type is valid HDF5 (H5Tinsert accepts any type), but this library
// cannot write it yet.
func TestNotSupported_CompoundMemberType(t *testing.T) {
	for _, dt := range []Datatype{VLenString, VLenInt32, Opaque} {
		_, err := NewCompoundType(16, []CompoundMember{{Name: "a", Type: dt}})
		require.ErrorIs(t, err, ErrNotSupported, "datatype %d", dt)
	}
}
