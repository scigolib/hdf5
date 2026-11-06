package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// layoutV1V2 builds a version 1/2 layout message (H5Olayout.c:106-216).
func layoutV1V2(version, class uint8, addr []byte, dims []uint32, compact []byte) []byte {
	b := []byte{version, byte(len(dims)), class, 0, 0, 0, 0, 0}
	b = append(b, addr...)
	for _, d := range dims {
		b = binary.LittleEndian.AppendUint32(b, d)
	}
	if compact != nil {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(compact)))
		b = append(b, compact...)
	}
	return b
}

func TestParseDataLayoutMessage_V1V2(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	for _, version := range []uint8{1, 2} {
		msg, err := ParseDataLayoutMessage(layoutV1V2(version, uint8(LayoutContiguous), le64(0x800), []uint32{10, 4}, nil), sb)
		require.NoError(t, err)
		require.Equal(t, LayoutContiguous, msg.Class)
		require.Equal(t, uint64(0x800), msg.DataAddress)

		msg, err = ParseDataLayoutMessage(layoutV1V2(version, uint8(LayoutChunked), le64(0x900), []uint32{5, 2, 4}, nil), sb)
		require.NoError(t, err)
		require.Equal(t, LayoutChunked, msg.Class)
		require.Equal(t, uint64(0x900), msg.DataAddress)
		require.Equal(t, []uint64{5, 2, 4}, msg.ChunkSize)
		require.Equal(t, ChunkIndexBTREE, msg.ChunkIndexType)

		msg, err = ParseDataLayoutMessage(layoutV1V2(version, uint8(LayoutCompact), nil, []uint32{3, 1}, []byte{1, 2, 3}), sb)
		require.NoError(t, err)
		require.Equal(t, LayoutCompact, msg.Class)
		require.Equal(t, []byte{1, 2, 3}, msg.CompactData)

		// An undefined address stays undefined (an unallocated dataset).
		msg, err = ParseDataLayoutMessage(layoutV1V2(version, uint8(LayoutContiguous), le64(undefinedAddress), []uint32{10, 4}, nil), sb)
		require.NoError(t, err)
		require.Equal(t, undefinedAddress, msg.DataAddress)
	}

	corrupt := map[string][]byte{
		"short":              {1, 1, 1},
		"ndims 0":            layoutV1V2(1, uint8(LayoutContiguous), le64(1), nil, nil),
		"ndims 34":           append([]byte{1, 34, 1, 0, 0, 0, 0, 0}, make([]byte, 8+34*4)...),
		"class 3":            layoutV1V2(1, uint8(LayoutVirtual), le64(1), []uint32{1}, nil),
		"address truncated":  layoutV1V2(1, uint8(LayoutContiguous), le64(1), []uint32{1}, nil)[:11],
		"dims truncated":     layoutV1V2(2, uint8(LayoutChunked), le64(1), []uint32{2, 4}, nil)[:20],
		"chunked ndims 1":    layoutV1V2(1, uint8(LayoutChunked), le64(1), []uint32{4}, nil),
		"chunk dim zero":     layoutV1V2(1, uint8(LayoutChunked), le64(1), []uint32{0, 4}, nil),
		"compact size short": layoutV1V2(1, uint8(LayoutCompact), nil, []uint32{1}, nil),
		"compact overrun": func() []byte {
			b := layoutV1V2(1, uint8(LayoutCompact), nil, []uint32{1}, []byte{1, 2})
			binary.LittleEndian.PutUint32(b[12:], 3_000_000) // Claims megabytes, holds 2 bytes.
			return b
		}(),
	}
	for name, data := range corrupt {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDataLayoutMessage(data, sb)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}
