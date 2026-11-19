package hdf5

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// readRootAttributes returns the root group's attributes of the file at path,
// by name.
func readRootAttributes(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	attrs, err := f.Root().Attributes()
	require.NoError(t, err)
	got := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		v, err := a.ReadValue()
		require.NoError(t, err, "attribute %q", a.Name)
		got[a.Name] = v
	}
	return got
}

func TestFileWriter_Root_WriteAttribute(t *testing.T) {
	cases := []struct {
		name string
		opts []interface{}
	}{
		{name: "superblock_v2"},
		{name: "superblock_v0", opts: []interface{}{WithSuperblockVersion(SuperblockV0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "root.h5")
			fw, err := CreateForWrite(path, CreateTruncate, tc.opts...)
			require.NoError(t, err)

			root := fw.Root()
			require.Equal(t, "/", root.Path())
			require.NoError(t, root.WriteAttribute("Number of Steps", int32(3)))
			require.NoError(t, root.WriteAttribute("title", "run"))

			ds, err := fw.CreateDataset("/d", Int32, []uint64{2})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{1, 2}))
			require.NoError(t, fw.Close())

			got := readRootAttributes(t, path)
			require.Equal(t, map[string]interface{}{
				"Number of Steps": int32(3),
				"title":           "run",
			}, got)
		})
	}
}

// The root group of a version 0 superblock has a version 1 object header,
// which cannot hold dense attribute storage: its attributes stay compact
// however many there are (H5O__attr_create, H5Oattribute.c).
func TestFileWriter_Root_V1HeaderStaysCompact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root.h5")
	fw, err := CreateForWrite(path, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	want := map[string]interface{}{}
	for i := range 2 * MaxCompactAttributes {
		name := fmt.Sprintf("attr%02d", i)
		require.NoError(t, fw.Root().WriteAttribute(name, int32(i)))
		want[name] = int32(i)
	}
	require.NoError(t, fw.Root().WriteAttribute("attr00", int32(100)))
	want["attr00"] = int32(100)
	require.NoError(t, fw.Root().DeleteAttribute("attr01"))
	delete(want, "attr01")
	rootAddr := fw.rootGroupAddr
	require.NoError(t, fw.Close())

	f, err := Open(path)
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(f.osFile, rootAddr, f.sb)
	require.NoError(t, err)
	require.Equal(t, uint8(1), oh.Version)
	for _, msg := range oh.Messages {
		require.NotEqual(t, core.MsgAttributeInfo, msg.Type, "version 1 header got an attribute info message")
	}
	require.NoError(t, f.Close())

	require.Equal(t, want, readRootAttributes(t, path))
}

func TestFileWriter_Root_OpenForWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	require.NoError(t, fw.Root().WriteAttribute("keep", int32(1)))
	require.NoError(t, fw.Root().WriteAttribute("drop", int32(2)))
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(path, OpenReadWrite)
	require.NoError(t, err)
	root := fw.Root()
	require.NoError(t, root.WriteAttribute("keep", int32(10)))
	require.NoError(t, root.WriteAttribute("added", 2.5))
	require.NoError(t, root.DeleteAttribute("drop"))
	require.NoError(t, fw.Close())

	require.Equal(t, map[string]interface{}{
		"keep":  int32(10),
		"added": 2.5,
	}, readRootAttributes(t, path))
}
