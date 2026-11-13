package hdf5_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// integerAttributes are integer attributes of every width and sign, each
// with a value that reads wrongly if its sign is taken wrongly: the unsigned
// ones are above the signed range, the signed ones are negative.
var integerAttributes = []struct {
	name  string
	value interface{}
}{
	{"u8", uint8(200)},
	{"u16", uint16(60000)},
	{"u32", uint32(4294967295)},
	{"u64", uint64(1<<63 + 3)},
	{"u32_pair", []uint32{4294967295, 7}},
	{"u64_pair", []uint64{1<<63 + 3, 7}},
	{"i8", int8(-5)},
	{"i16", int16(-5)},
	{"i32", int32(-5)},
	{"i64", int64(-5)},
	{"i32_pair", []int32{-5, 7}},
	{"i64_pair", []int64{-5, 7}},
}

// attributeReader is what a dataset and a group both give for one attribute.
type attributeReader func(name string) (interface{}, error)

// requireAsWritten reads every integer attribute back and expects the type
// and the value that were written.
func requireAsWritten(t *testing.T, read attributeReader) {
	t.Helper()

	for _, attr := range integerAttributes {
		got, err := read(attr.name)
		require.NoError(t, err, attr.name)
		require.Equal(t, attr.value, got, attr.name)
	}
}

// TestReadAttribute_IntegerSign writes integer attributes with this library's
// writer and reads them back with its reader. Each must come back as the type
// and value that were written. Before the change, the unsigned 4- and 8-byte
// ones came back as int32 and int64, negative.
//
// The attributes are read from the three places the reader finds them: an
// object header's attribute messages (compact storage, one attribute to a
// dataset), dense storage (all of them on one dataset, more than a header
// keeps in messages), and a group.
func TestReadAttribute_IntegerSign(t *testing.T) {
	name := filepath.Join(t.TempDir(), "sign.h5")

	fw, err := hdf5.CreateForWrite(name, hdf5.CreateTruncate)
	require.NoError(t, err)

	// Compact: one dataset to an attribute.
	for _, attr := range integerAttributes {
		dw, err := fw.CreateDataset("/compact_"+attr.name, hdf5.Float64, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, dw.Write([]float64{0}))
		require.NoError(t, dw.WriteAttribute(attr.name, attr.value))
	}

	// Dense: every attribute on one dataset.
	dense, err := fw.CreateDataset("/dense", hdf5.Float64, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, dense.Write([]float64{0}))
	for _, attr := range integerAttributes {
		require.NoError(t, dense.WriteAttribute(attr.name, attr.value))
	}

	// A group, with few enough to stay in its header.
	gw, err := fw.CreateGroup("/group")
	require.NoError(t, err)
	inGroup := integerAttributes[2:6] // The four unsigned 4- and 8-byte ones.
	for _, attr := range inGroup {
		require.NoError(t, gw.WriteAttribute(attr.name, attr.value))
	}

	require.NoError(t, fw.Close())

	file, err := hdf5.Open(name)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	datasets := map[string]*hdf5.Dataset{}
	var group *hdf5.Group
	file.Walk(func(path string, obj hdf5.Object) {
		switch found := obj.(type) {
		case *hdf5.Dataset:
			datasets[path] = found
		case *hdf5.Group:
			if found.Name() == "group" {
				group = found
			}
		}
	})

	t.Run("compact", func(t *testing.T) {
		requireAsWritten(t, func(name string) (interface{}, error) {
			ds := datasets["/compact_"+name]
			require.NotNil(t, ds, name)

			return ds.ReadAttribute(name)
		})
	})

	t.Run("dense", func(t *testing.T) {
		ds := datasets["/dense"]
		require.NotNil(t, ds)

		names, err := ds.ListAttributes()
		require.NoError(t, err)
		require.Len(t, names, len(integerAttributes))

		requireAsWritten(t, ds.ReadAttribute)
	})

	t.Run("group", func(t *testing.T) {
		require.NotNil(t, group)

		attrs, err := group.Attributes()
		require.NoError(t, err)
		require.Len(t, attrs, len(inGroup))

		read := map[string]interface{}{}
		for _, attr := range attrs {
			value, err := attr.ReadValue()
			require.NoError(t, err, attr.Name)
			read[attr.Name] = value
		}
		for _, attr := range inGroup {
			require.Equal(t, attr.value, read[attr.name], fmt.Sprintf("group attribute %s", attr.name))
		}
	})
}
