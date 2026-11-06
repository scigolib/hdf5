package core

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// nestedVLen repeats the header of a variable-length sequence (version 1,
// size 16) n times: every level's base type is the next level, and the
// innermost one runs off the buffer.
func nestedVLen(n int) []byte {
	return bytes.Repeat([]byte{0x19, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00}, n)
}

// decodeAllocs decodes data and returns the bytes allocated and the error.
func decodeAllocs(data []byte) (uint64, error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, err := DecodeDatatype(data)
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, err
}

// TestDecodeDatatype_DeepNestingBounded is the regression for review-064 H1:
// the error of a deeply nested datatype was wrapped once per level, so the
// memory kept by it grew with the square of the depth (8 KB of input, 1024
// levels: ~14 MB). The decoder now stops at maxDatatypeNesting levels.
func TestDecodeDatatype_DeepNestingBounded(t *testing.T) {
	allocated, err := decodeAllocs(nestedVLen(1024)) // 8 KB: safe to run before the fix
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Less(t, allocated, uint64(1<<20), "decode of 8 KB allocated %d bytes", allocated)
}

// TestDecodeDatatype_DeepNesting64KB uses the largest input an object header
// message can carry (64 KB, 8192 levels). It cannot be shown red safely:
// before the limit it retained about 960 MB.
func TestDecodeDatatype_DeepNesting64KB(t *testing.T) {
	allocated, err := decodeAllocs(nestedVLen(8191))
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.ErrorContains(t, err, "nesting")
	require.Less(t, allocated, uint64(1<<20), "decode of 64 KB allocated %d bytes", allocated)
}

// TestDecodeDatatype_NestingLimitBoundary pins the limit: 64 levels decode,
// 65 are corrupt, and the error of an object header message names the
// limit, the byte in the message and the message address, once.
func TestDecodeDatatype_NestingLimitBoundary(t *testing.T) {
	int32Leaf := []byte{0x10, 0x08, 0x00, 0x00, 4, 0, 0, 0, 0, 0, 32, 0}
	_, n, err := DecodeDatatype(append(nestedVLen(maxDatatypeNesting-1), int32Leaf...))
	require.NoError(t, err)
	require.Equal(t, 8*(maxDatatypeNesting-1)+len(int32Leaf), n)

	msg := &HeaderMessage{Type: MsgDatatype, Offset: 0x1234, Data: append(nestedVLen(maxDatatypeNesting), int32Leaf...)}
	_, err = ParseDatatypeHeaderMessage(msg)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.ErrorContains(t, err, "datatype message at 0x1234")
	require.ErrorContains(t, err, "nesting deeper than 64 levels at byte 512")
	require.Equal(t, 1, strings.Count(err.Error(), "nesting deeper"), "one error, not one per level: %v", err)
}

// vlenChain nests leaf in variable-length sequences: levels types in all.
func vlenChain(levels int, leaf *DatatypeMessage) *DatatypeMessage {
	dt := leaf
	for i := 1; i < levels; i++ {
		dt = &DatatypeMessage{Class: DatatypeVarLen, Size: vlenHeapIDSize, ClassBitField: vlenSequence, Base: dt}
	}
	return dt
}

// errorChainLength counts the errors errors.Unwrap reaches from err.
func errorChainLength(err error) int {
	n := 0
	for ; err != nil; err = errors.Unwrap(err) {
		n++
	}
	return n
}

// TestEncodeDatatype_NestingLimit is the encoder half of review-064 H1: a
// type deeper than the decoder's limit is refused before the encoder
// recurses, and an error deep inside a type is wrapped once with the path to
// it, not once per level.
func TestEncodeDatatype_NestingLimit(t *testing.T) {
	leaf := &DatatypeMessage{Class: DatatypeFixed, Size: 4}

	enc, err := EncodeDatatypeMessage(vlenChain(maxDatatypeNesting, leaf))
	require.NoError(t, err)
	require.Len(t, enc, 8*(maxDatatypeNesting-1)+12)

	_, err = EncodeDatatypeMessage(vlenChain(maxDatatypeNesting+1, leaf))
	require.ErrorContains(t, err, "nesting deeper than 64 levels")

	_, err = EncodeDatatypeMessage(vlenChain(maxDatatypeNesting, &DatatypeMessage{Class: DatatypeFixed}))
	require.ErrorContains(t, err, "fixed-point datatype size cannot be 0")
	require.ErrorContains(t, err, "in variable-length base type > variable-length base type > ")
	require.LessOrEqual(t, errorChainLength(err), 2, "the error is wrapped per level: %v", err)
}
