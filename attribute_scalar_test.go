package hdf5_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// writeScalarAttrs writes a number, a string and a slice attribute on a group and on a dataset.
func writeScalarAttrs(t *testing.T, filename string, opts ...interface{}) {
	t.Helper()
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate, opts...)
	require.NoError(t, err)
	g, err := fw.CreateGroup("/g")
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/g/d", hdf5.Float32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float32{1, 2}))
	for _, w := range []interface {
		WriteAttribute(string, interface{}) error
	}{g, ds} {
		require.NoError(t, w.WriteAttribute("n", int64(42)))
		require.NoError(t, w.WriteAttribute("f", 2.5))
		require.NoError(t, w.WriteAttribute("s", "abc"))
		require.NoError(t, w.WriteAttribute("v", []int32{1, 2, 3}))
	}
	require.NoError(t, fw.Close())
}

func readAttrs(t *testing.T, filename, path string) map[string]interface{} {
	t.Helper()
	f, err := hdf5.Open(filename)
	require.NoError(t, err)
	defer f.Close()
	got := map[string]interface{}{}
	f.Walk(func(p string, obj hdf5.Object) {
		if p != path {
			return
		}
		ds, ok := obj.(*hdf5.Dataset)
		require.True(t, ok)
		for _, name := range []string{"n", "f", "s", "v"} {
			v, err := ds.ReadAttribute(name)
			require.NoError(t, err, name)
			got[name] = v
		}
	})
	return got
}

// TestScalarAttributesReadBack checks that the option changes the dataspace, not the values read back.
func TestScalarAttributesReadBack(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.h5")
	scalar := filepath.Join(dir, "scalar.h5")
	writeScalarAttrs(t, plain)
	writeScalarAttrs(t, scalar, hdf5.WithScalarAttributes())
	require.Equal(t, readAttrs(t, plain, "/g/d"), readAttrs(t, scalar, "/g/d"))
}

// TestCInterop_ScalarAttributes has h5dump show the dataspaces each mode writes, in both superblock
// versions that have different dataspace message versions.
func TestCInterop_ScalarAttributes(t *testing.T) {
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not in PATH")
	}
	for _, sb := range []uint8{hdf5.SuperblockV0, hdf5.SuperblockV2} {
		dir := t.TempDir()
		plain := filepath.Join(dir, "plain.h5")
		scalar := filepath.Join(dir, "scalar.h5")
		writeScalarAttrs(t, plain, hdf5.WithSuperblockVersion(sb))
		writeScalarAttrs(t, scalar, hdf5.WithSuperblockVersion(sb), hdf5.WithScalarAttributes())

		for name, want := range map[string]string{
			"n": "DATASPACE  SCALAR", "f": "DATASPACE  SCALAR", "s": "DATASPACE  SCALAR",
			"v": "DATASPACE  SIMPLE { ( 3 ) / ( 3 ) }",
		} {
			for _, obj := range []string{"/g", "/g/d"} {
				out, err := exec.Command(h5dump, "-a", obj+"/"+name, scalar).CombinedOutput()
				require.NoError(t, err, "superblock %d %s/%s:\n%s", sb, obj, name, out)
				require.Contains(t, string(out), want, "superblock %d %s/%s", sb, obj, name)
			}
		}
		// Without the option, every value keeps the dataspace it had before.
		for name, want := range map[string]string{
			"n": "DATASPACE  SIMPLE { ( 1 ) / ( 1 ) }", "f": "DATASPACE  SIMPLE { ( 1 ) / ( 1 ) }",
			"s": "DATASPACE  SIMPLE { ( 1 ) / ( 1 ) }", "v": "DATASPACE  SIMPLE { ( 3 ) / ( 3 ) }",
		} {
			for _, obj := range []string{"/g", "/g/d"} {
				out, err := exec.Command(h5dump, "-a", obj+"/"+name, plain).CombinedOutput()
				require.NoError(t, err, "superblock %d %s/%s:\n%s", sb, obj, name, out)
				require.True(t, strings.Contains(string(out), want),
					"superblock %d %s/%s: without the option the dataspace is unchanged:\n%s", sb, obj, name, out)
			}
		}
	}
}
