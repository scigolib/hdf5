package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// v0Image writes a version 0 superblock with the given group K values (0 for
// the writer's default) and returns the file image, 2048 bytes long.
func v0Image(t *testing.T, leafK, internalK uint16) []byte {
	t.Helper()
	w := &bytesWriterAt{}
	sb := &Superblock{
		Version: Version0, OffsetSize: 8, LengthSize: 8, RootGroup: 512,
		Endianness: binary.LittleEndian, SymLeafK: leafK, SymInternalK: internalK,
	}
	require.NoError(t, sb.WriteTo(w, 2048))
	img := make([]byte, 2048)
	copy(img, w.buf)
	return img
}

// TestSuperblock_V0BTreeK: a version 0 superblock's group K values are kept
// (H5Fsuper_cache.c:476-486), the chunk K is the C default, and the writer
// writes the file's own values back, the C defaults for a new file.
func TestSuperblock_V0BTreeK(t *testing.T) {
	sb, err := ReadSuperblock(bytes.NewReader(v0Image(t, 0, 0)))
	require.NoError(t, err)
	require.Equal(t, [3]uint16{4, 16, 32}, [3]uint16{sb.SymLeafK, sb.SymInternalK, sb.ChunkInternalK})

	img := v0Image(t, 8, 32)
	require.Equal(t, []byte{8, 0, 32, 0}, img[16:20], "the writer keeps the file's K values")
	sb, err = ReadSuperblock(bytes.NewReader(img))
	require.NoError(t, err)
	require.Equal(t, [3]uint16{8, 32, 32}, [3]uint16{sb.SymLeafK, sb.SymInternalK, sb.ChunkInternalK})
}

// btreekFile builds a version 2 superblock whose extension object header (a
// version 2 header at 48) holds msg as a B-tree 'K' values message (0x0013).
func btreekFile(msg []byte) []byte {
	const extAddr = 48
	ohdr := []byte{'O', 'H', 'D', 'R', 2, 0, byte(4 + len(msg)), byte(MsgBTreeK), byte(len(msg)), 0, 0}
	ohdr = append(ohdr, msg...)
	ohdr = binary.LittleEndian.AppendUint32(ohdr, JenkinsChecksum(ohdr))

	img := make([]byte, extAddr, extAddr+len(ohdr))
	copy(img, Signature)
	img[8], img[9], img[10], img[11] = Version2, 8, 8, 0
	eof := uint64(extAddr + len(ohdr))
	binary.LittleEndian.PutUint64(img[12:], 0)
	binary.LittleEndian.PutUint64(img[20:], extAddr)
	binary.LittleEndian.PutUint64(img[28:], eof)
	binary.LittleEndian.PutUint64(img[36:], extAddr) // root: any address in the file
	binary.LittleEndian.PutUint32(img[44:], JenkinsChecksum(img[:44]))
	return append(img, ohdr...)
}

// TestSuperblock_BTreeKMessage: a version 2 superblock takes its K values
// from the extension's B-tree 'K' values message (H5Fsuper.c:699-715,
// H5Obtreek.c:80-103); a malformed message or a K of 0 is corrupt.
func TestSuperblock_BTreeKMessage(t *testing.T) {
	sb, err := ReadSuperblock(bytes.NewReader(btreekFile([]byte{0, 64, 0, 20, 0, 6, 0})))
	require.NoError(t, err)
	require.Equal(t, [3]uint16{6, 20, 64}, [3]uint16{sb.SymLeafK, sb.SymInternalK, sb.ChunkInternalK})

	for name, msg := range map[string][]byte{
		"version 1": {1, 64, 0, 20, 0, 6, 0},
		"short":     {0, 64, 0, 20, 0, 6},
		"zero leaf": {0, 64, 0, 20, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadSuperblock(bytes.NewReader(btreekFile(msg)))
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}

	// With a user block the extension address is relative to the base
	// address (H5FDint.c:267): the same file behind 512 bytes reads the same.
	img := append(make([]byte, 512), btreekFile([]byte{0, 64, 0, 20, 0, 6, 0})...)
	// The stored base and EOF are absolute (H5Fsuper.c:572-577).
	binary.LittleEndian.PutUint64(img[512+12:], 512)
	binary.LittleEndian.PutUint64(img[512+28:], uint64(len(img)))
	binary.LittleEndian.PutUint32(img[512+44:], JenkinsChecksum(img[512:512+44]))
	sb, err = ReadSuperblock(bytes.NewReader(img))
	require.NoError(t, err)
	require.Equal(t, uint64(512), sb.BaseAddress)
	require.Equal(t, [3]uint16{6, 20, 64}, [3]uint16{sb.SymLeafK, sb.SymInternalK, sb.ChunkInternalK})

	// The C library's own files with a 512-byte user block and an extension
	// (h5clear test files, H5Fsuper.c writes the extension relative to base).
	for _, name := range []string{"h5clear_fsm_persist_user_equal.h5", "h5clear_fsm_persist_user_less.h5"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hdf5_official", name))
		require.NoError(t, err)
		sb, err := ReadSuperblock(bytes.NewReader(data))
		require.NoError(t, err, name)
		require.Equal(t, uint64(512), sb.BaseAddress, name)
	}

	// No extension: the C defaults.
	w := &bytesWriterAt{}
	require.NoError(t, (&Superblock{Version: Version2, OffsetSize: 8, LengthSize: 8, RootGroup: 48,
		Endianness: binary.LittleEndian}).WriteTo(w, 48))
	sb, err = ReadSuperblock(bytes.NewReader(w.buf))
	require.NoError(t, err)
	require.Equal(t, [3]uint16{4, 16, 32}, [3]uint16{sb.SymLeafK, sb.SymInternalK, sb.ChunkInternalK})
}
