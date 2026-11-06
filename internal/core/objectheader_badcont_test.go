package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestObjectHeader_BadContinuationMessage is the regression test for the
// review-r open item: a continuation message that cannot be decoded was
// skipped, so the header was returned without the messages of the chunk it
// named. C fails the header: H5O__chunk_deserialize decodes every
// continuation message and reports "bad continuation message found"
// (H5Ocache.c:1417-1422) when H5O__cont_decode fails, which it does when the
// address or length runs off the message (H5Ocont.c:98-103) or the length is
// zero (H5Ocont.c:105-106).
//
// Red on 22cfabf: the headers parse without an error.
func TestObjectHeader_BadContinuationMessage(t *testing.T) {
	t.Run("v1 short", func(t *testing.T) {
		file := v1CycleFile(0)
		binary.LittleEndian.PutUint16(file[16+2:], 8) // Message data size 8: address only.
		msgs, _, _, err := parseV1Header(bytes.NewReader(file), 0, cycleSB())
		require.Nil(t, msgs)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})

	t.Run("v1 zero length", func(t *testing.T) {
		file := v1CycleFile(0)
		binary.LittleEndian.PutUint64(file[16+16:], 0) // Continuation length 0.
		msgs, _, _, err := parseV1Header(bytes.NewReader(file), 0, cycleSB())
		require.Nil(t, msgs)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})

	t.Run("v2 short", func(t *testing.T) {
		file := v2CycleFile(0)
		// Chunk 0 holds one 12-byte message: type, size 8, flags, 8 bytes.
		file[6] = 12
		binary.LittleEndian.PutUint16(file[7+1:], 8)
		_, _, err := parseV2Header(bytes.NewReader(file), 0, 0, cycleSB(), false)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})

	t.Run("v2 zero length", func(t *testing.T) {
		file := v2CycleFile(0)
		binary.LittleEndian.PutUint64(file[7+12:], 0) // Continuation length 0.
		_, _, err := parseV2Header(bytes.NewReader(file), 0, 0, cycleSB(), false)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})
}

// TestObjectHeader_BadPrefixIsCorrupt: a header whose prefix is not an object
// header is corruption decided by the file's bytes, so it wraps
// utils.ErrCorrupt (and the shared message cache keeps it). parseV1Header
// returned no error at all for a wrong version: utils.WrapError with a nil
// cause is nil.
//
// Red on 22cfabf: no sentinel for the signature; nil error for the version.
func TestObjectHeader_BadPrefixIsCorrupt(t *testing.T) {
	_, err := readObjectHeader(bytes.NewReader(make([]byte, 64)), 0, cycleSB(), false)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	file := v1CycleFile(0)
	file[0] = 2
	msgs, _, _, err := parseV1Header(bytes.NewReader(file), 0, cycleSB())
	require.Nil(t, msgs)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}
