package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestWriteRefCount_UpgradesLegacyMessage verifies that writeRefCount
// upgrades the pre-v0.15 RefCount message layout (bare uint32, no version
// byte) to the spec layout (version byte + uint32) when rewriting a header
// from an older file.
func TestWriteRefCount_UpgradesLegacyMessage(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "legacy_refcount.h5")

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2}))
	require.NoError(t, fw.CreateHardLink("/alias", "/target"))

	addr, err := fw.resolveObjectAddress("/target")
	require.NoError(t, err)

	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	require.NoError(t, err)

	// Rewrite the RefCount message in the pre-v0.15 layout: bare uint32.
	found := false
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgRefCount {
			continue
		}
		legacy := make([]byte, 4)
		sb.Endianness.PutUint32(legacy, oh.ReferenceCount)
		msg.Data = legacy
		found = true
		break
	}
	require.True(t, found, "hard-linked dataset must carry a RefCount message")

	// writeRefCount must replace the 4-byte body with the 5-byte layout.
	oh.ReferenceCount = 7
	require.NoError(t, fw.writeRefCount(addr, oh, sb))

	reread, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	require.NoError(t, err)
	require.Equal(t, uint32(7), reread.ReferenceCount)
	for _, msg := range reread.Messages {
		if msg.Type == core.MsgRefCount {
			require.Len(t, msg.Data, 5, "message must be upgraded to versioned layout")
			require.Equal(t, byte(0), msg.Data[0], "version byte")
		}
	}
}
