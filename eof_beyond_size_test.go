package hdf5

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpen_EOFPastFileSizeRefused: a file whose superblock end-of-file
// address lies past its physical end is truncated; it is refused, as the C
// library refuses it (H5Fsuper.c:573-576), before anything is written.
func TestOpen_EOFPastFileSizeRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "short.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	binary.LittleEndian.PutUint64(raw[40:48], uint64(len(raw))+4096) // version 0: EOF at bytes 40-47
	require.NoError(t, os.WriteFile(file, raw, 0o600))

	_, err = Open(file)
	require.ErrorContains(t, err, "truncated file")
	_, err = OpenForWrite(file, OpenReadWrite)
	require.ErrorContains(t, err, "truncated file")
}

// TestClose_FileReachesItsEndOfFile: space allocated but never written (a
// dataset created and not written) still belongs to the file: Close extends
// the file to its end-of-file address, as the C library sets the file size
// to the EOA when it closes (H5FD_truncate from H5F__dest, H5Fint.c), so the
// file is never shorter than its superblock says and the C library opens it.
func TestClose_FileReachesItsEndOfFile(t *testing.T) {
	for _, sbv := range []uint8{SuperblockV0, SuperblockV2} {
		file := filepath.Join(t.TempDir(), "unwritten.h5")
		fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sbv))
		require.NoError(t, err)
		_, err = fw.CreateDataset("/never_written", Float64, []uint64{1 << 16})
		require.NoError(t, err)
		eof := fw.writer.EndOfFile()
		require.NoError(t, fw.Close())

		st, err := os.Stat(file)
		require.NoError(t, err)
		require.Equal(t, int64(eof), st.Size(), "superblock %d: the file is shorter than its end-of-file address", sbv)
		f, err := Open(file)
		require.NoError(t, err, "superblock %d", sbv)
		require.NoError(t, f.Close())
		requireH5dumpClean(t, file)
	}
}
