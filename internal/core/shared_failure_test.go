package core

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// countingReaderAt counts the reads made through it.
type countingReaderAt struct {
	io.ReaderAt
	reads atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.reads.Add(1)
	return c.ReaderAt.ReadAt(p, off)
}

// TestSharedFailureDecodedOnce is the regression test for review-r finding 2:
// a shared reference whose target fails to resolve was resolved again for
// every reference, so a file with thousands of attributes typed by one bad
// committed datatype parsed its header thousands of times. The failure is
// cached per file like a success: one target, one decode, the same error.
//
// Red on 22cfabf: every call reads the target header again.
func TestSharedFailureDecodedOnce(t *testing.T) {
	const target = 0x100
	ref := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(target))

	cases := map[string][]byte{
		// A header whose datatype message has class 15, which does not exist.
		"bad datatype": v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01,
			Data: []byte{0x1F, 0, 0, 0, 1, 0, 0, 0}}),
		// Not an object header at all.
		"bad header": make([]byte, 0x40),
		// A header without the datatype message the reference names.
		"no message": v1HeaderWith(HeaderMessage{Type: MsgNil, Data: make([]byte, 8)}),
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			file := make([]byte, target+len(hdr))
			copy(file[target:], hdr)
			r := &countingReaderAt{ReaderAt: bytes.NewReader(file)}
			sb := sharedTestSB()

			_, first := resolveSharedDatatype(r, sb, ref)
			require.ErrorIs(t, first, utils.ErrCorrupt)
			reads := r.reads.Load()

			for range 50 {
				_, err := resolveSharedDatatype(r, sb, ref)
				require.Equal(t, first, err)
				// The raw body of the bad datatype is well formed; only its
				// decoding fails.
				_, err = resolveSharedMessage(r, sb, ref, MsgDatatype)
				require.Equal(t, name != "bad datatype", err != nil, "raw body: %v", err)
			}
			require.Equal(t, reads, r.reads.Load(), "the failing target was read again")
		})
	}
}

// flakyReaderAt fails the first fails reads at or past from with a raw I/O
// error that says nothing about the file's bytes, then reads normally.
type flakyReaderAt struct {
	io.ReaderAt
	from  int64
	fails atomic.Int64
}

func (f *flakyReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.from && f.fails.Add(-1) >= 0 {
		return 0, errors.New("device: transient read failure")
	}
	return f.ReaderAt.ReadAt(p, off)
}

// TestSharedTransientFailureRetried: only failures decided by the file's
// bytes (utils.ErrCorrupt, utils.ErrNotSupported) are cached. A raw read
// error is not: the next access reads the target again and succeeds.
//
// Red before the follow-up: the read error was cached for the life of the
// File.
func TestSharedTransientFailureRetried(t *testing.T) {
	const target = 0x100
	hdr := v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: int8Message(t)})
	file := make([]byte, target+len(hdr))
	copy(file[target:], hdr)
	ref := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(target))

	r := &flakyReaderAt{ReaderAt: bytes.NewReader(file), from: target}
	r.fails.Store(1)
	sb := sharedTestSB()

	_, err := resolveSharedDatatype(r, sb, ref)
	require.Error(t, err)
	require.ErrorContains(t, err, "transient read failure")

	dt, err := resolveSharedDatatype(r, sb, ref)
	require.NoError(t, err)
	require.Equal(t, uint32(1), dt.Size)

	// The same for the raw body.
	r = &flakyReaderAt{ReaderAt: bytes.NewReader(file), from: target}
	r.fails.Store(1)
	sb = sharedTestSB()
	_, err = resolveSharedMessage(r, sb, ref, MsgDatatype)
	require.Error(t, err)
	_, err = resolveSharedMessage(r, sb, ref, MsgDatatype)
	require.NoError(t, err)
}

// TestEFLHeapTransientFailureRetried: a raw read error on the External File
// List heap is not cached either.
func TestEFLHeapTransientFailureRetried(t *testing.T) {
	msg := eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{eflNameA, 0, 100})
	r := &flakyReaderAt{ReaderAt: bytes.NewReader(eflTestFile(t)), from: eflHeapAddr}
	r.fails.Store(1)
	sb := sharedTestSB()

	_, err := ParseExternalFileList(r, sb, msg)
	require.ErrorContains(t, err, "transient read failure")

	l, err := ParseExternalFileList(r, sb, msg)
	require.NoError(t, err)
	require.Equal(t, "a.bin", l.Slots[0].Name)
}
