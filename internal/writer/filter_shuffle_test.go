package writer

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewShuffleFilter(t *testing.T) {
	tests := []struct {
		name        string
		elementSize uint32
	}{
		{"element size 1", 1},
		{"element size 2", 2},
		{"element size 4", 4},
		{"element size 8", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewShuffleFilter(tt.elementSize)
			require.NotNil(t, filter)
			require.Equal(t, tt.elementSize, filter.elementSize)
		})
	}
}

func TestShuffleFilter_ID(t *testing.T) {
	filter := NewShuffleFilter(4)
	require.Equal(t, FilterShuffle, filter.ID())
	require.Equal(t, FilterID(2), filter.ID())
}

func TestShuffleFilter_Name(t *testing.T) {
	filter := NewShuffleFilter(4)
	require.Equal(t, "shuffle", filter.Name())
}

func TestShuffleFilter_Encode(t *testing.T) {
	tests := []struct {
		name        string
		elementSize uint32
	}{
		{"element size 1", 1},
		{"element size 2", 2},
		{"element size 4", 4},
		{"element size 8", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewShuffleFilter(tt.elementSize)
			flags, cdValues := filter.Encode()

			require.Equal(t, uint16(0), flags)
			require.Equal(t, 1, len(cdValues))
			require.Equal(t, tt.elementSize, cdValues[0])
		})
	}
}

func TestShuffleFilter_Apply_ElementSize4(t *testing.T) {
	filter := NewShuffleFilter(4)
	// Three 4-byte elements: [1,2,3,4][5,6,7,8][9,10,11,12]
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	shuffled, err := filter.Apply(data)
	require.NoError(t, err)

	// Expected: [1,5,9][2,6,10][3,7,11][4,8,12]
	expected := []byte{1, 5, 9, 2, 6, 10, 3, 7, 11, 4, 8, 12}
	require.Equal(t, expected, shuffled)
}

func TestShuffleFilter_Apply_ElementSize8(t *testing.T) {
	filter := NewShuffleFilter(8)
	// Two 8-byte elements
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	shuffled, err := filter.Apply(data)
	require.NoError(t, err)

	// Expected: [1,9][2,10][3,11][4,12][5,13][6,14][7,15][8,16]
	expected := []byte{1, 9, 2, 10, 3, 11, 4, 12, 5, 13, 6, 14, 7, 15, 8, 16}
	require.Equal(t, expected, shuffled)
}

func TestShuffleFilter_Apply_ElementSize2(t *testing.T) {
	filter := NewShuffleFilter(2)
	// Four 2-byte elements: [1,2][3,4][5,6][7,8]
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	shuffled, err := filter.Apply(data)
	require.NoError(t, err)

	// Expected: [1,3,5,7][2,4,6,8]
	expected := []byte{1, 3, 5, 7, 2, 4, 6, 8}
	require.Equal(t, expected, shuffled)
}

func TestShuffleFilter_Apply_ElementSize1(t *testing.T) {
	filter := NewShuffleFilter(1)
	data := []byte{1, 2, 3, 4, 5}

	shuffled, err := filter.Apply(data)
	require.NoError(t, err)

	// With element size 1, shuffle does nothing
	require.Equal(t, data, shuffled)
}

func TestShuffleFilter_Apply_EmptyData(t *testing.T) {
	filter := NewShuffleFilter(4)
	data := []byte{}

	shuffled, err := filter.Apply(data)
	require.NoError(t, err)
	require.Equal(t, []byte{}, shuffled)
}

func TestShuffleFilter_Apply_InvalidLength(t *testing.T) {
	filter := NewShuffleFilter(4)
	// Data length not multiple of element size
	data := []byte{1, 2, 3, 4, 5}

	_, err := filter.Apply(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not multiple of element size")
}

func TestShuffleFilter_Remove_ElementSize4(t *testing.T) {
	filter := NewShuffleFilter(4)
	// Shuffled data: [1,5,9][2,6,10][3,7,11][4,8,12]
	shuffled := []byte{1, 5, 9, 2, 6, 10, 3, 7, 11, 4, 8, 12}

	unshuffled, err := filter.Remove(shuffled)
	require.NoError(t, err)

	// Expected: [1,2,3,4][5,6,7,8][9,10,11,12]
	expected := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	require.Equal(t, expected, unshuffled)
}

func TestShuffleFilter_RoundTrip_Int32Array(t *testing.T) {
	filter := NewShuffleFilter(4)

	// Create array of int32 values
	original := make([]byte, 100*4)
	for i := 0; i < 100; i++ {
		binary.LittleEndian.PutUint32(original[i*4:], uint32(i*100))
	}

	shuffled, err := filter.Apply(original)
	require.NoError(t, err)
	require.NotEqual(t, original, shuffled)

	unshuffled, err := filter.Remove(shuffled)
	require.NoError(t, err)
	require.Equal(t, original, unshuffled)
}

func TestShuffleFilter_RoundTrip_Float64Array(t *testing.T) {
	filter := NewShuffleFilter(8)

	// Create array of float64 values
	original := make([]byte, 50*8)
	for i := 0; i < 50; i++ {
		bits := binary.LittleEndian.Uint64(original[i*8:])
		binary.LittleEndian.PutUint64(original[i*8:], bits+uint64(i))
	}

	shuffled, err := filter.Apply(original)
	require.NoError(t, err)
	require.NotEqual(t, original, shuffled)

	unshuffled, err := filter.Remove(shuffled)
	require.NoError(t, err)
	require.Equal(t, original, unshuffled)
}

func TestShuffleFilter_RoundTrip_LargeData(t *testing.T) {
	filter := NewShuffleFilter(4)

	// Create 1MB of data
	original := make([]byte, 1024*1024)
	for i := 0; i < len(original)/4; i++ {
		binary.LittleEndian.PutUint32(original[i*4:], uint32(i))
	}

	shuffled, err := filter.Apply(original)
	require.NoError(t, err)

	unshuffled, err := filter.Remove(shuffled)
	require.NoError(t, err)

	require.Equal(t, original, unshuffled)
}

func TestShuffleFilter_Remove_EmptyData(t *testing.T) {
	filter := NewShuffleFilter(4)
	data := []byte{}

	unshuffled, err := filter.Remove(data)
	require.NoError(t, err)
	require.Equal(t, []byte{}, unshuffled)
}

func TestShuffleFilter_Remove_InvalidLength(t *testing.T) {
	filter := NewShuffleFilter(4)
	// Data length not multiple of element size
	data := []byte{1, 2, 3, 4, 5}

	_, err := filter.Remove(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not multiple of element size")
}

func TestShuffleFilter_WithGZIP(t *testing.T) {
	// Test shuffle improves GZIP compression
	shuffleFilter := NewShuffleFilter(4)
	gzipFilter := NewGZIPFilter(6)

	// Create repetitive int32 data (good for compression)
	original := make([]byte, 1000*4)
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint32(original[i*4:], uint32(i%100))
	}

	// Test 1: GZIP only
	gzipOnly, err := gzipFilter.Apply(original)
	require.NoError(t, err)

	// Test 2: Shuffle + GZIP
	shuffled, err := shuffleFilter.Apply(original)
	require.NoError(t, err)

	shuffledGzipped, err := gzipFilter.Apply(shuffled)
	require.NoError(t, err)

	// Shuffle + GZIP should compress better than GZIP alone
	require.Less(t, len(shuffledGzipped), len(gzipOnly),
		"Shuffle+GZIP should compress better than GZIP alone")

	// Verify round-trip
	decompressed, err := gzipFilter.Remove(shuffledGzipped)
	require.NoError(t, err)

	unshuffled, err := shuffleFilter.Remove(decompressed)
	require.NoError(t, err)

	require.Equal(t, original, unshuffled)
}

func TestShuffleFilter_IntegrationWithPipeline(t *testing.T) {
	pipeline := NewFilterPipeline()
	shuffleFilter := NewShuffleFilter(4)
	gzipFilter := NewGZIPFilter(6)

	pipeline.AddFilter(shuffleFilter)
	pipeline.AddFilter(gzipFilter)

	// Create test data
	original := make([]byte, 1000*4)
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint32(original[i*4:], uint32(i*10))
	}

	// Apply pipeline
	filtered, err := pipeline.Apply(original)
	require.NoError(t, err)
	require.NotEqual(t, original, filtered)

	// Remove pipeline (reverse order)
	restored, err := pipeline.Remove(filtered)
	require.NoError(t, err)
	require.Equal(t, original, restored)
}

func TestShuffleFilter_DifferentElementSizes(t *testing.T) {
	tests := []struct {
		name        string
		elementSize uint32
		numElements int
	}{
		{"1-byte elements", 1, 100},
		{"2-byte elements", 2, 100},
		{"4-byte elements", 4, 100},
		{"8-byte elements", 8, 100},
		{"16-byte elements", 16, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewShuffleFilter(tt.elementSize)

			// Create test data
			dataLen := int(tt.elementSize) * tt.numElements
			original := make([]byte, dataLen)
			for i := range original {
				original[i] = byte(i % 256)
			}

			// Round-trip test
			shuffled, err := filter.Apply(original)
			require.NoError(t, err)

			unshuffled, err := filter.Remove(shuffled)
			require.NoError(t, err)

			require.Equal(t, original, unshuffled)
		})
	}
}

func TestShuffleFilter_BinaryPatterns(t *testing.T) {
	filter := NewShuffleFilter(4)

	tests := []struct {
		name string
		data []byte
	}{
		{"all zeros", bytes.Repeat([]byte{0, 0, 0, 0}, 100)},
		{"all ones", bytes.Repeat([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 100)},
		{"alternating pattern", func() []byte {
			data := make([]byte, 400)
			for i := 0; i < 100; i++ {
				pattern := []byte{0xAA, 0xBB, 0xCC, 0xDD}
				copy(data[i*4:], pattern)
			}
			return data
		}()},
		{"sequential values", func() []byte {
			data := make([]byte, 400)
			for i := 0; i < 100; i++ {
				binary.LittleEndian.PutUint32(data[i*4:], uint32(i))
			}
			return data
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shuffled, err := filter.Apply(tt.data)
			require.NoError(t, err)

			unshuffled, err := filter.Remove(shuffled)
			require.NoError(t, err)

			require.Equal(t, tt.data, unshuffled)
		})
	}
}

func TestShuffleFilter_CompressionImprovement(t *testing.T) {
	// Quantify compression improvement with shuffle
	shuffleFilter := NewShuffleFilter(4)
	gzipFilter := NewGZIPFilter(6)

	// Create data with pattern that benefits from shuffle
	original := make([]byte, 10000*4)
	for i := 0; i < 10000; i++ {
		// Values with similar high bytes (good for shuffle)
		val := uint32(0x12340000 + i%256)
		binary.LittleEndian.PutUint32(original[i*4:], val)
	}

	// GZIP only
	gzipOnly, err := gzipFilter.Apply(original)
	require.NoError(t, err)
	ratioGZIPOnly := float64(len(original)) / float64(len(gzipOnly))

	// Shuffle + GZIP
	shuffled, err := shuffleFilter.Apply(original)
	require.NoError(t, err)
	shuffledGzipped, err := gzipFilter.Apply(shuffled)
	require.NoError(t, err)
	ratioShuffleGZIP := float64(len(original)) / float64(len(shuffledGzipped))

	// Shuffle should improve compression significantly (at least 20%)
	improvement := (ratioShuffleGZIP - ratioGZIPOnly) / ratioGZIPOnly
	require.Greater(t, improvement, 0.2,
		"Shuffle should improve compression by at least 20%% for this data pattern")

	t.Logf("GZIP only: %.2f:1, Shuffle+GZIP: %.2f:1, Improvement: %.1f%%",
		ratioGZIPOnly, ratioShuffleGZIP, improvement*100)
}
