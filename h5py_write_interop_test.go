package hdf5

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrite_H5PyRead(t *testing.T) {
	testFile := "test_create_h5py.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Create file
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create root-level group
	_, err = fw.CreateGroup("/group")
	require.NoError(t, err)

	// Create datasets in group with data
	ds, err := fw.CreateDataset("/group/uint", Int32, []uint64{5})
	require.NoError(t, err)
	err = ds.Write([]int32{1, 2, 3, 4, 5})
	require.NoError(t, err)

	ds, err = fw.CreateDataset("/group/float", Float32, []uint64{5})
	require.NoError(t, err)
	err = ds.Write([]float32{1.0, 2.0, 3.0, 4.0, 5.0})
	require.NoError(t, err)

	// Close without error
	err = fw.Close()
	require.NoError(t, err)

	// Run UV script to load HDF file and verify no error
	cmd := exec.Command("uv", "run", "h5py_write_interop_test.py", testFile)
	_, err = cmd.Output()
	fmt.Println(err)
	require.NoError(t, err)
}
