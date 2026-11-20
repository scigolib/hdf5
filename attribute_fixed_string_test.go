package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// readDatasetAttribute opens path and returns the attribute name of the
// dataset at dsPath.
func readDatasetAttribute(t *testing.T, path, dsPath, name string) any {
	t.Helper()
	f, err := Open(path)
	require.NoError(t, err)
	defer f.Close()
	var ds *Dataset
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == dsPath {
			ds = d
		}
	})
	require.NotNil(t, ds, "dataset %s", dsPath)
	v, err := ds.ReadAttribute(name)
	require.NoError(t, err)
	return v
}

func TestWriteAttribute_FixedStringValue(t *testing.T) {
	cases := []struct {
		name string
		v    FixedStringValue
		want string
	}{
		{"text_fills_nullterm", FixedStringValue{Value: "node-007", Type: FixedString{Size: 8}}, "node-007"},
		{"nullterm_shorter", FixedStringValue{Value: "abc", Type: FixedString{Size: 8}}, "abc"},
		{"nullpad", FixedStringValue{Value: "abc", Type: FixedString{Size: 6, Padding: PaddingNullPad}}, "abc"},
		{"spacepad", FixedStringValue{Value: "abc", Type: FixedString{Size: 6, Padding: PaddingSpacePad}}, "abc"},
		{"utf8", FixedStringValue{Value: "é", Type: FixedString{Size: 2, Charset: CharsetUTF8}}, "é"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.h5")
			fw, err := CreateForWrite(path, CreateTruncate)
			require.NoError(t, err)
			ds, err := fw.CreateDataset("/d", Int32, []uint64{1})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{1}))
			require.NoError(t, ds.WriteAttribute("s", tc.v))
			g, err := fw.CreateGroup("/g")
			require.NoError(t, err)
			require.NoError(t, g.WriteAttribute("s", tc.v))
			require.NoError(t, fw.Close())

			require.Equal(t, tc.want, readDatasetAttribute(t, path, "/d", "s"))
		})
	}
}

func TestWriteAttribute_FixedStringValueDense(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/d", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1}))
	for i := range MaxCompactAttributes + 1 {
		v := FixedStringValue{Value: string(rune('a' + i)), Type: FixedString{Size: 1}}
		require.NoError(t, ds.WriteAttribute(string(rune('a'+i)), v))
	}
	require.NoError(t, fw.Close())

	require.Equal(t, "i", readDatasetAttribute(t, path, "/d", "i"))
}

func TestWriteAttribute_FixedStringValueErrors(t *testing.T) {
	cases := []struct {
		name string
		v    FixedStringValue
		msg  string
	}{
		{"too_long", FixedStringValue{Value: "abcdef", Type: FixedString{Size: 4}}, "does not fit"},
		{"zero_size", FixedStringValue{Value: "", Type: FixedString{}}, "size must be positive"},
		{"bad_padding", FixedStringValue{Value: "a", Type: FixedString{Size: 4, Padding: 7}}, "illegal string padding"},
		{"bad_charset", FixedStringValue{Value: "a", Type: FixedString{Size: 4, Charset: 5}}, "illegal character set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fw, err := CreateForWrite(filepath.Join(t.TempDir(), "a.h5"), CreateTruncate)
			require.NoError(t, err)
			defer fw.Close()
			g, err := fw.CreateGroup("/g")
			require.NoError(t, err)
			require.ErrorContains(t, g.WriteAttribute("s", tc.v), tc.msg)
		})
	}
}

// TestCInterop_FixedStringAttribute checks that h5dump reads a string
// attribute of the given size and padding, with no byte added for a
// terminator.
func TestCInterop_FixedStringAttribute(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	path := filepath.Join(t.TempDir(), "a.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	g, err := fw.CreateGroup("/g")
	require.NoError(t, err)
	require.NoError(t, g.WriteAttribute("name", FixedStringValue{Value: "node-007", Type: FixedString{Size: 8}}))
	require.NoError(t, g.WriteAttribute("pad", FixedStringValue{Value: "ab", Type: FixedString{Size: 4, Padding: PaddingSpacePad}}))
	require.NoError(t, fw.Close())

	name := runH5(t, h5dump, "--enable-error-stack", "-a", "/g/name", path)
	for _, want := range []string{"STRSIZE 8;", "H5T_STR_NULLTERM", `"node-007"`} {
		require.Contains(t, name, want, "h5dump output:\n%s", name)
	}
	pad := runH5(t, h5dump, "--enable-error-stack", "-a", "/g/pad", path)
	for _, want := range []string{"STRSIZE 4;", "H5T_STR_SPACEPAD", `"ab  "`} {
		require.Contains(t, pad, want, "h5dump output:\n%s", pad)
	}
}
