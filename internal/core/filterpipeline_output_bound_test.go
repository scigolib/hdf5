package core

import (
	"bytes"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// testFilterCap is the output bound the filter unit tests pass when the bound
// is not what they test: large enough for every fixture.
const testFilterCap uint64 = 1 << 20

// Filter output is bounded by the chunk's byte size (review finding H4): a
// deflate stream expands about a thousandfold, so a few megabytes of chunk
// data used to decompress into gigabytes through io.ReadAll before anything
// compared the result with the chunk. The red proof on the base commit is the
// public TestRead_DeflateBombChunk; these cases pin the bound per filter.

func TestApplyDeflate_OutputBound(t *testing.T) {
	plain := make([]byte, 64<<10)
	stream := zlibCompress(t, plain)

	got, err := applyDeflate(stream, uint64(len(plain)))
	require.NoError(t, err, "output of exactly the bound is valid")
	require.Len(t, got, len(plain))

	_, err = applyDeflate(stream, uint64(len(plain))-1)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "decompressed data exceeds the chunk size 65535")
}

func TestApplyBZIP2_OutputBound(t *testing.T) {
	// "AAAA" compressed with bzip2 -9 (same fixture as TestApplyFilter_BZIP2Dispatch).
	bzip2AAAA := []byte{
		0x42, 0x5A, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26,
		0x53, 0x59, 0xE1, 0x6E, 0x65, 0x71, 0x00, 0x00,
		0x02, 0x44, 0x00, 0x40, 0x00, 0x20, 0x00, 0x20,
		0x00, 0x21, 0x00, 0x82, 0x0B, 0x17, 0x72, 0x45,
		0x38, 0x50, 0x90, 0xE1, 0x6E, 0x65, 0x71,
	}

	got, err := applyBZIP2(bzip2AAAA, 4)
	require.NoError(t, err)
	require.Equal(t, []byte("AAAA"), got)

	_, err = applyBZIP2(bzip2AAAA, 3)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

func TestLZFDecompress_OutputBound(t *testing.T) {
	// Literal "a", then a long back-reference of 264 bytes at offset 1: a
	// 4-byte stream that expands to 265 bytes.
	stream := []byte{0x00, 'a', 0xE0, 0x00, 0xFF}

	got, err := lzfDecompress(stream, 265)
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'a'}, 265), got)

	_, err = lzfDecompress(stream, 264)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// A literal run alone beyond the bound.
	_, err = lzfDecompress([]byte{0x04, 'h', 'e', 'l', 'l', 'o'}, 4)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestApplyFilters_OutputBoundAllowsFletcher: a filter that runs before a
// Fletcher32 checksum on encoding decodes to the chunk plus that 4-byte
// checksum, which the Fletcher32 step strips afterwards.
func TestApplyFilters_OutputBoundAllowsFletcher(t *testing.T) {
	chunk := bytes.Repeat([]byte{7}, 1024)
	withSum := append(append([]byte(nil), chunk...), 0, 0, 0, 0)

	// Encoding order: Fletcher32, then deflate.
	fp := &FilterPipelineMessage{Filters: []Filter{{ID: FilterFletcher}, {ID: FilterDeflate}}}

	got, err := fp.ApplyFilters(zlibCompress(t, withSum), uint64(len(chunk)))
	require.NoError(t, err)
	require.Equal(t, chunk, got)

	// One byte more than chunk + checksum is corruption.
	_, err = fp.ApplyFilters(zlibCompress(t, append(withSum, 0)), uint64(len(chunk)))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestApplyFilters_LZFPaddingBoundedByChunk: the LZF padding size comes from
// cd_values[2]; a value larger than the chunk would allocate beyond it.
func TestApplyFilters_LZFPaddingBoundedByChunk(t *testing.T) {
	fp := &FilterPipelineMessage{Filters: []Filter{{ID: FilterLZF, ClientData: []uint32{0, 0, 1 << 20}}}}

	_, err := fp.ApplyFilters([]byte{0x04, 'h', 'e', 'l', 'l', 'o'}, 4096)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "exceeds the chunk size 4096")
}
