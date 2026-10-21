package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyFilters_LZFPaddingRejectsOversizedChunk covers the ADR-003 bound on
// the LZF padding buffer. cd_values[2] is the uncompressed chunk size from the
// filter pipeline message; the padding buffer used to be allocated at that
// size unchecked, so a corrupt value was a fatal allocation.
//
// The value is 1 GiB + 1: above utils.MaxChunkSize on every platform, yet
// below math.MaxInt32, so a 32-bit build reaches the cap check instead of the
// representability check and the assertion is platform-independent.
//
// Post-fix only: on the pre-fix code this input would attempt a 1 GiB
// allocation, which is a fatal runtime throw rather than a reportable test
// failure, so it cannot be shown red safely.
func TestApplyFilters_LZFPaddingRejectsOversizedChunk(t *testing.T) {
	// LZF literal: 0x04 = 5 bytes "hello", decompressed to fewer bytes than the
	// claimed chunk size, which is what reaches the padding branch.
	fp := &FilterPipelineMessage{
		Filters: []Filter{{
			ID:         FilterLZF,
			ClientData: []uint32{0, 0, (1 << 30) + 1}, // 1 GiB + 1, above utils.MaxChunkSize
		}},
	}

	_, err := fp.ApplyFilters([]byte{0x04, 'h', 'e', 'l', 'l', 'o'})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum")
}

// TestApplyFilters_LZFPaddingWithinCap pins the behavior the cap must not
// change: a chunk size inside the limit still pads to the expected length.
func TestApplyFilters_LZFPaddingWithinCap(t *testing.T) {
	fp := &FilterPipelineMessage{
		Filters: []Filter{{
			ID:         FilterLZF,
			ClientData: []uint32{0, 0, 8},
		}},
	}

	got, err := fp.ApplyFilters([]byte{0x04, 'h', 'e', 'l', 'l', 'o'})
	require.NoError(t, err)
	require.Len(t, got, 8)
	require.Equal(t, []byte("hello\x00\x00\x00"), got)
}
