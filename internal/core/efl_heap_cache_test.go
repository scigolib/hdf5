package core

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestEFLHeapReadOncePerFile is the regression test for review-r finding 6:
// the local heap of an External File List was read again every time a
// descriptor was built, so many datasets naming one large heap read it once
// each. It is read once per file, and its failure is cached too.
//
// Red on 22cfabf: every parse reads the heap again.
func TestEFLHeapReadOncePerFile(t *testing.T) {
	msg := eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{eflNameA, 0, 100})

	t.Run("good heap", func(t *testing.T) {
		r := &countingReaderAt{ReaderAt: bytes.NewReader(eflTestFile(t))}
		sb := sharedTestSB()
		_, err := ParseExternalFileList(r, sb, msg)
		require.NoError(t, err)
		reads := r.reads.Load()

		for range 20 {
			l, err := ParseExternalFileList(r, sb, msg)
			require.NoError(t, err)
			require.Equal(t, "a.bin", l.Slots[0].Name)
		}
		require.Equal(t, reads, r.reads.Load(), "the heap was read again")
	})

	t.Run("bad heap", func(t *testing.T) {
		file := eflTestFile(t)
		copy(file[eflHeapAddr:], "PAEH")
		r := &countingReaderAt{ReaderAt: bytes.NewReader(file)}
		sb := sharedTestSB()
		_, first := ParseExternalFileList(r, sb, msg)
		require.ErrorIs(t, first, utils.ErrCorrupt)
		reads := r.reads.Load()

		for range 20 {
			_, err := ParseExternalFileList(r, sb, msg)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		}
		require.Equal(t, reads, r.reads.Load(), "the bad heap was read again")
	})
}
