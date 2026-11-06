package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// movedV2Image places a version 2 superblock without extension at superAddr
// with the given stored base and EOF addresses, padded to size bytes.
func movedV2Image(superAddr, storedBase, eof, size int) []byte {
	img := make([]byte, size)
	sb := img[superAddr:]
	copy(sb, Signature)
	sb[8], sb[9], sb[10], sb[11] = Version2, 8, 8, 0
	binary.LittleEndian.PutUint64(sb[12:], uint64(storedBase))
	binary.LittleEndian.PutUint64(sb[20:], undefinedAddress)
	binary.LittleEndian.PutUint64(sb[28:], uint64(eof))
	binary.LittleEndian.PutUint64(sb[36:], 48) // root: any address in the file
	binary.LittleEndian.PutUint32(sb[44:], JenkinsChecksum(sb[:44]))
	return img
}

// TestSuperblock_MovedSuperblockEOF: when the superblock is found at an
// address other than the stored base (a user block added or removed by
// h5jam/h5unjam), the stored EOF moves with it and the base becomes the
// superblock's address (H5Fsuper.c:475-482).
func TestSuperblock_MovedSuperblockEOF(t *testing.T) {
	for name, tc := range map[string]struct {
		superAddr, storedBase, eof, size int
		wantEOF                          uint64
	}{
		"in place":            {512, 512, 1024, 1024, 1024},
		"user block added":    {512, 0, 512, 1024, 1024},
		"user block shrunk":   {512, 1024, 1536, 1024, 1024},
		"no user block, base": {0, 0, 1024, 1024, 1024},
	} {
		t.Run(name, func(t *testing.T) {
			sb, err := ReadSuperblock(bytes.NewReader(movedV2Image(tc.superAddr, tc.storedBase, tc.eof, tc.size)))
			require.NoError(t, err)
			require.Equal(t, uint64(tc.superAddr), sb.BaseAddress)
			require.Equal(t, tc.wantEOF, sb.EOFAddress)
		})
	}

	// Added user block whose moved EOF is past the file: truncated, as in C
	// (H5Fsuper.c:572-577 after the adjustment).
	_, err := ReadSuperblock(bytes.NewReader(movedV2Image(512, 0, 1024, 1024)))
	require.Error(t, err)

	// A stored EOF smaller than the shift cannot be moved.
	_, err = ReadSuperblock(bytes.NewReader(movedV2Image(512, 1024, 256, 1024)))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}
