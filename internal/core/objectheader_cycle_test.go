package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// Object header continuation cycles (review-r finding 1). C loads each
// continuation chunk through the metadata cache; a chunk reached twice comes
// back with the chunk number it was first loaded as, and H5O_protect fails
// with "incorrect chunk number for object header chunk" (H5Oint.c:1063-1064).
// The parser used to follow such a cycle forever, allocating as it went (a
// 536-byte file reached 625 MiB in 3 s).
//
// These tests cannot be run red safely outside a memory-capped sandbox: on the
// unfixed code they never return.

const cycleBlock = 64 // Address of the first continuation block.

// v1ContMessage is a v1 continuation message (type 0x10, 16 bytes of data).
func v1ContMessage(addr, size uint64) []byte {
	m := make([]byte, 8+16)
	binary.LittleEndian.PutUint16(m[0:], uint16(MsgContinuation))
	binary.LittleEndian.PutUint16(m[2:], 16)
	binary.LittleEndian.PutUint64(m[8:], addr)
	binary.LittleEndian.PutUint64(m[16:], size)
	return m
}

// v1CycleFile builds a v1 header at 0 whose continuation leads to a block at
// cycleBlock; that block's continuation points to next.
func v1CycleFile(next uint64) []byte {
	cont := v1ContMessage(cycleBlock, 24)
	file := make([]byte, cycleBlock+24)
	file[0] = 1                                      // Version.
	binary.LittleEndian.PutUint16(file[2:], 1)       // One message.
	binary.LittleEndian.PutUint32(file[4:], 1)       // Reference count.
	binary.LittleEndian.PutUint32(file[8:], 24)      // Header data size.
	copy(file[16:], cont)                            // At 16..40.
	copy(file[cycleBlock:], v1ContMessage(next, 24)) // Block at 64..88.
	return file
}

// v2ContMessage is a v2 continuation message (1-byte type, 2-byte size,
// flags, 16 bytes of data).
func v2ContMessage(addr, size uint64) []byte {
	m := make([]byte, 4+16)
	m[0] = byte(MsgContinuation)
	binary.LittleEndian.PutUint16(m[1:], 16)
	binary.LittleEndian.PutUint64(m[4:], addr)
	binary.LittleEndian.PutUint64(m[12:], size)
	return m
}

// v2CycleFile builds an OHDR at 0 whose continuation leads to an OCHK block
// at cycleBlock; that block's continuation points to next. Checksums are not
// needed by the parser and are left zero.
func v2CycleFile(next uint64) []byte {
	const ochkSize = 4 + 20 + 4
	file := make([]byte, cycleBlock+ochkSize)
	copy(file, "OHDR")
	file[4] = 2  // Version.
	file[5] = 0  // Flags: 1-byte chunk size.
	file[6] = 20 // Chunk 0: one continuation message.
	copy(file[7:], v2ContMessage(cycleBlock, ochkSize))
	copy(file[cycleBlock:], "OCHK")
	copy(file[cycleBlock+4:], v2ContMessage(next, ochkSize)) // Checksum stays zero.
	return file
}

func cycleSB() *Superblock {
	return &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
}

func TestObjectHeaderV1_ContinuationCycle(t *testing.T) {
	cases := map[string]uint64{
		"block to itself":    cycleBlock,
		"block to header":    0,
		"block to main data": 16,
	}
	for name, next := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := parseV1Header(bytes.NewReader(v1CycleFile(next)), 0, cycleSB())
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

func TestObjectHeaderV2_ContinuationCycle(t *testing.T) {
	cases := map[string]uint64{
		"block to itself": cycleBlock,
		"block to header": 0,
	}
	for name, next := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseV2Header(bytes.NewReader(v2CycleFile(next)), 0, 0, cycleSB(), false)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

func TestObjectHeader_ContinuationBytesCapped(t *testing.T) {
	// A continuation block that claims more bytes than the 88-byte file
	// holds: the walk refuses it instead of reading what it can.
	file := v1CycleFile(88)                           // The block's continuation leads past the end.
	binary.LittleEndian.PutUint64(file[16+16:], 1000) // Header's continuation: 1000 bytes at 64.
	msgs, _, _, err := parseV1Header(bytes.NewReader(file), 0, cycleSB())
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Nil(t, msgs)
}
