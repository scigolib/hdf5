package hdf5

import (
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// datasetHeader reads the object header of the dataset at path in file.
func datasetHeader(t *testing.T, file, path string) *core.ObjectHeader {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var addr uint64
	f.Walk(func(p string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && p == path {
			addr = ds.Address()
		}
	})
	require.NotZero(t, addr, "dataset %s not found", path)
	oh, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	return oh
}

// TestWriteAttribute_KeepsHeaderFlagsAndTimes checks what our reader sees
// after the rewrite: the datatype message keeps H5O_MSG_FLAG_SHARED and the
// header keeps its stored times (flag 0x20, H5Ocache.c:404-410). Writing
// data to the dataset is refused: its committed datatype is not resolved.
func TestWriteAttribute_KeepsHeaderFlagsAndTimes(t *testing.T) {
	file := copySharedTypeFile(t)
	before := datasetHeader(t, file, sharedTypeDataset)
	require.Len(t, before.Times, 16)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset(sharedTypeDataset)
	require.NoError(t, err)
	require.ErrorContains(t, ds.WriteRaw(make([]byte, 32)), "committed (shared) datatype")
	require.NoError(t, ds.WriteAttribute("note", int32(7)))
	require.NoError(t, fw.Close())

	after := datasetHeader(t, file, sharedTypeDataset)
	require.Equal(t, before.Times, after.Times)
	shared := false
	for _, msg := range after.Messages {
		if msg.Type == core.MsgDatatype {
			shared = msg.Flags&core.MsgFlagShared != 0
		}
	}
	require.True(t, shared, "datatype message lost its shared flag")
}
