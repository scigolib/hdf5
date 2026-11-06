package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// withChecksum appends the checksum as H5Z__filter_fletcher32 writes it:
// little-endian (UINT32ENCODE).
func withChecksum(data []byte, sum uint32) []byte {
	out := append([]byte(nil), data...)
	return binary.LittleEndian.AppendUint32(out, sum)
}

// TestFletcher32_KnownValues pins fletcher32 to H5_checksum_fletcher32
// (H5checksum.c:94-136), worked by hand:
//   - "abcde": words 0x6162, 0x6364, the odd byte as 0x6500 → 0x4FF029C7;
//   - 0xFF 0xFF: sum1 = 0xFFFF stays 0xFFFF under C's folding, where a
//     modulo-65535 sum would give 0 → 0xFFFFFFFF.
//
// C-written chunks are covered by the official files with fletcher32
// (TestFletcher32_OfficialFiles in the root package).
func TestFletcher32_KnownValues(t *testing.T) {
	require.Equal(t, uint32(0x4FF029C7), fletcher32([]byte("abcde")))
	require.Equal(t, uint32(0xFFFFFFFF), fletcher32([]byte{0xFF, 0xFF}))
	require.Equal(t, uint32(0), fletcher32([]byte{0, 0, 0}))
}

// TestFletcher32_Verifies is the regression test for TASK-073: the checksum
// was stripped without being checked, so a flipped data byte read as valid
// data. C verifies it on read and accepts the checksum or its pre-1.6.3
// byte-swapped variant (H5Zfletcher32.c:64-104).
//
// Red on 40c6d35: the corrupt chunk and the short input are accepted.
func TestFletcher32_Verifies(t *testing.T) {
	// 721 and 4099 bytes cross the 360-word folding blocks.
	for _, n := range []int{1, 2, 7, 720, 721, 4099} {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i*31 + 7)
		}
		sum := fletcher32(data)

		got, err := applyFletcher32(withChecksum(data, sum))
		require.NoError(t, err, "n=%d", n)
		require.Equal(t, data, got)

		// The pre-1.6.3 variant: bytes swapped within each 16-bit half.
		swapped := (sum&0x00FF00FF)<<8 | (sum&0xFF00FF00)>>8
		got, err = applyFletcher32(withChecksum(data, swapped))
		require.NoError(t, err, "n=%d swapped", n)
		require.Equal(t, data, got)

		corrupt := withChecksum(data, sum)
		corrupt[n/2] ^= 0x10
		_, err = applyFletcher32(corrupt)
		require.ErrorIs(t, err, utils.ErrCorrupt, "n=%d flipped byte", n)
	}

	_, err := applyFletcher32([]byte{1, 2, 3})
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

func FuzzFletcher32(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4, 5})
	f.Add(withChecksum([]byte("abcde"), 0x4FF029C7))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := applyFletcher32(data)
		if err != nil {
			return
		}
		require.Len(t, out, len(data)-fletcherLen)
		require.Equal(t, data[:len(out)], out)
	})
}
