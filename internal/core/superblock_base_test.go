package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// v2SuperblockImage returns a file image of the given size with a v2
// superblock at addr. Fields follow the v2 layout: base at 12, extension at
// 20, end-of-file at 28, root group at 36.
func v2SuperblockImage(addr, storedBase, eof, root uint64, size int) []byte {
	img := make([]byte, size)
	sb := img[addr:]

	copy(sb[0:8], Signature)
	sb[8] = Version2
	sb[9] = 0  // little-endian
	sb[10] = 8 // size of offsets (direct value)

	binary.LittleEndian.PutUint64(sb[12:20], storedBase)
	binary.LittleEndian.PutUint64(sb[20:28], ^uint64(0)) // no extension
	binary.LittleEndian.PutUint64(sb[28:36], eof)
	binary.LittleEndian.PutUint64(sb[36:44], root)

	return img
}

// TestReadSuperblockLocatesSignatureAtUserBlockOffsets mirrors the C search
// (H5FD_locate_signature, H5FDint.c:148): the signature is at 0 or at a power
// of two starting at 512, and the superblock position is the file's base
// address.
func TestReadSuperblockLocatesSignatureAtUserBlockOffsets(t *testing.T) {
	for _, addr := range []uint64{0, 512, 1024, 2048} {
		size := int(addr) + 4096
		img := v2SuperblockImage(addr, addr, uint64(size), addr+48, size)

		sb, err := ReadSuperblock(bytes.NewReader(img))
		require.NoError(t, err, "superblock at %d", addr)
		require.Equal(t, uint8(Version2), sb.Version, "superblock at %d", addr)
		require.Equal(t, addr, sb.BaseAddress,
			"the effective base address is the superblock position (H5Fsuper.c:475-485)")
	}
}

// TestReadSuperblockPrefersPositionOverStoredBase pins the C override: when the
// stored base disagrees with where the superblock actually is, the position
// wins (H5Fsuper.c:475-485).
func TestReadSuperblockPrefersPositionOverStoredBase(t *testing.T) {
	img := v2SuperblockImage(0, 512, 4096, 48, 4096)

	sb, err := ReadSuperblock(bytes.NewReader(img))
	require.NoError(t, err)
	require.Equal(t, uint64(0), sb.BaseAddress)
}

func TestReadSuperblockWithoutSignature(t *testing.T) {
	_, err := ReadSuperblock(bytes.NewReader(make([]byte, 4096)))
	require.ErrorIs(t, err, ErrNoSignature)
}

// TestReadSuperblockRejectsTruncatedFile covers the C check
// H5Fsuper.c:573: the stored end-of-file address is absolute and cannot be past
// the bytes the file actually has.
func TestReadSuperblockRejectsTruncatedFile(t *testing.T) {
	img := v2SuperblockImage(0, 0, 8192, 48, 4096)

	_, err := ReadSuperblock(bytes.NewReader(img))
	require.Error(t, err)
	require.Contains(t, err.Error(), "truncated")
}

// TestReadSuperblockRejectsStoredBaseBeyondFile proves the stored base address
// is parsed for both formats and validated: a base past the end cannot describe
// a user block.
func TestReadSuperblockRejectsStoredBaseBeyondFile(t *testing.T) {
	img := v2SuperblockImage(0, 1<<40, 4096, 48, 4096)

	_, err := ReadSuperblock(bytes.NewReader(img))
	require.Error(t, err)
	require.Contains(t, err.Error(), "base address")
}

func TestReadSuperblockV0ParsesStoredBase(t *testing.T) {
	img := make([]byte, 4096)
	copy(img[0:8], Signature)
	img[8] = Version0
	img[13] = 8 // size of offsets
	img[14] = 8 // size of lengths

	binary.LittleEndian.PutUint64(img[24:32], 1<<40)            // base address (stored)
	binary.LittleEndian.PutUint64(img[40:48], uint64(len(img))) // end of file
	binary.LittleEndian.PutUint64(img[64:72], 96)               // root group object header

	_, err := ReadSuperblock(bytes.NewReader(img))
	require.Error(t, err, "v0 must read and validate the stored base address, not force zero")
	require.Contains(t, err.Error(), "base address")
}
