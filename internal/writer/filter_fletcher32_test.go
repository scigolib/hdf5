package writer

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewFletcher32Filter(t *testing.T) {
	filter := NewFletcher32Filter()
	require.NotNil(t, filter)
}

func TestFletcher32Filter_ID(t *testing.T) {
	filter := NewFletcher32Filter()
	require.Equal(t, FilterFletcher32, filter.ID())
	require.Equal(t, FilterID(3), filter.ID())
}

func TestFletcher32Filter_Name(t *testing.T) {
	filter := NewFletcher32Filter()
	require.Equal(t, "fletcher32", filter.Name())
}

func TestFletcher32Filter_Encode(t *testing.T) {
	filter := NewFletcher32Filter()
	flags, cdValues := filter.Encode()

	require.Equal(t, uint16(0), flags)
	require.Equal(t, 0, len(cdValues))
}

func TestFletcher32Filter_Apply(t *testing.T) {
	filter := NewFletcher32Filter()
	data := []byte{1, 2, 3, 4, 5}

	result, err := filter.Apply(data)
	require.NoError(t, err)

	// Result should be 4 bytes longer (original + checksum)
	require.Equal(t, len(data)+4, len(result))

	// Original data should be preserved
	require.Equal(t, data, result[:len(data)])

	// Checksum should be present (non-zero for this data)
	checksum := binary.LittleEndian.Uint32(result[len(data):])
	require.NotEqual(t, uint32(0), checksum)
}

func TestFletcher32Filter_Apply_EmptyData(t *testing.T) {
	filter := NewFletcher32Filter()
	data := []byte{}

	result, err := filter.Apply(data)
	require.NoError(t, err)

	// Result should be 4 bytes (just checksum)
	require.Equal(t, 4, len(result))
}

func TestFletcher32Filter_Apply_LargeData(t *testing.T) {
	filter := NewFletcher32Filter()
	data := make([]byte, 100*1024) // 100KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	result, err := filter.Apply(data)
	require.NoError(t, err)
	require.Equal(t, len(data)+4, len(result))
}

func TestFletcher32Filter_Remove_Valid(t *testing.T) {
	filter := NewFletcher32Filter()
	original := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	// Apply checksum
	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	// Remove checksum
	restored, err := filter.Remove(withChecksum)
	require.NoError(t, err)

	require.Equal(t, original, restored)
}

func TestFletcher32Filter_Remove_TooShort(t *testing.T) {
	filter := NewFletcher32Filter()

	// Data too short to contain checksum
	data := []byte{1, 2, 3}
	_, err := filter.Remove(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestFletcher32Filter_Remove_InvalidChecksum(t *testing.T) {
	filter := NewFletcher32Filter()
	original := []byte{1, 2, 3, 4, 5}

	// Apply checksum
	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	// Corrupt the checksum
	corrupted := make([]byte, len(withChecksum))
	copy(corrupted, withChecksum)
	corrupted[len(corrupted)-1] ^= 0xFF // Flip bits in checksum

	// Remove should detect corruption
	_, err = filter.Remove(corrupted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatch")
}

func TestFletcher32Filter_Remove_CorruptedData(t *testing.T) {
	filter := NewFletcher32Filter()
	original := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Apply checksum
	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	// Corrupt the data (not checksum)
	corrupted := make([]byte, len(withChecksum))
	copy(corrupted, withChecksum)
	corrupted[5] ^= 0xFF // Flip bits in data

	// Remove should detect corruption
	_, err = filter.Remove(corrupted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatch")
}

func TestFletcher32Filter_RoundTrip_SmallData(t *testing.T) {
	filter := NewFletcher32Filter()
	original := []byte{42}

	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	restored, err := filter.Remove(withChecksum)
	require.NoError(t, err)

	require.Equal(t, original, restored)
}

func TestFletcher32Filter_RoundTrip_MediumData(t *testing.T) {
	filter := NewFletcher32Filter()
	original := make([]byte, 1024)
	for i := range original {
		original[i] = byte(i % 256)
	}

	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	restored, err := filter.Remove(withChecksum)
	require.NoError(t, err)

	require.Equal(t, original, restored)
}

func TestFletcher32Filter_RoundTrip_LargeData(t *testing.T) {
	filter := NewFletcher32Filter()
	original := make([]byte, 1024*1024) // 1MB
	for i := range original {
		original[i] = byte(i * 7 % 256)
	}

	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	restored, err := filter.Remove(withChecksum)
	require.NoError(t, err)

	require.Equal(t, original, restored)
}

func TestFletcher32Filter_RoundTrip_EmptyData(t *testing.T) {
	filter := NewFletcher32Filter()
	original := []byte{}

	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	restored, err := filter.Remove(withChecksum)
	require.NoError(t, err)

	require.Equal(t, original, restored)
}

func TestFletcher32Filter_DifferentDataPatterns(t *testing.T) {
	filter := NewFletcher32Filter()

	tests := []struct {
		name string
		data []byte
	}{
		{"all zeros", make([]byte, 100)},
		{"all ones", func() []byte {
			data := make([]byte, 100)
			for i := range data {
				data[i] = 0xFF
			}
			return data
		}()},
		{"sequential", func() []byte {
			data := make([]byte, 100)
			for i := range data {
				data[i] = byte(i)
			}
			return data
		}()},
		{"alternating", func() []byte {
			data := make([]byte, 100)
			for i := range data {
				data[i] = byte(i % 2)
			}
			return data
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withChecksum, err := filter.Apply(tt.data)
			require.NoError(t, err)

			restored, err := filter.Remove(withChecksum)
			require.NoError(t, err)

			require.Equal(t, tt.data, restored)
		})
	}
}

func TestFletcher32Filter_ChecksumUniqueness(t *testing.T) {
	filter := NewFletcher32Filter()

	// Different data should produce different checksums
	data1 := []byte{1, 2, 3, 4, 5}
	data2 := []byte{5, 4, 3, 2, 1}
	data3 := []byte{1, 2, 3, 4, 6}

	checksum1, _ := filter.Apply(data1)
	checksum2, _ := filter.Apply(data2)
	checksum3, _ := filter.Apply(data3)

	cs1 := binary.LittleEndian.Uint32(checksum1[len(data1):])
	cs2 := binary.LittleEndian.Uint32(checksum2[len(data2):])
	cs3 := binary.LittleEndian.Uint32(checksum3[len(data3):])

	require.NotEqual(t, cs1, cs2)
	require.NotEqual(t, cs1, cs3)
	require.NotEqual(t, cs2, cs3)
}

func TestFletcher32Filter_OddLengthData(t *testing.T) {
	filter := NewFletcher32Filter()

	// Test with odd-length data
	tests := []struct {
		name   string
		length int
	}{
		{"1 byte", 1},
		{"3 bytes", 3},
		{"5 bytes", 5},
		{"99 bytes", 99},
		{"1001 bytes", 1001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.length)
			for i := range data {
				data[i] = byte(i % 256)
			}

			withChecksum, err := filter.Apply(data)
			require.NoError(t, err)

			restored, err := filter.Remove(withChecksum)
			require.NoError(t, err)

			require.Equal(t, data, restored)
		})
	}
}

func TestFletcher32Filter_EvenLengthData(t *testing.T) {
	filter := NewFletcher32Filter()

	// Test with even-length data
	tests := []struct {
		name   string
		length int
	}{
		{"2 bytes", 2},
		{"4 bytes", 4},
		{"100 bytes", 100},
		{"1000 bytes", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.length)
			for i := range data {
				data[i] = byte(i % 256)
			}

			withChecksum, err := filter.Apply(data)
			require.NoError(t, err)

			restored, err := filter.Remove(withChecksum)
			require.NoError(t, err)

			require.Equal(t, data, restored)
		})
	}
}

func TestFletcher32Filter_IntegrationWithPipeline(t *testing.T) {
	pipeline := NewFilterPipeline()
	filter := NewFletcher32Filter()
	pipeline.AddFilter(filter)

	original := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Apply pipeline
	filtered, err := pipeline.Apply(original)
	require.NoError(t, err)
	require.NotEqual(t, original, filtered)

	// Remove pipeline
	restored, err := pipeline.Remove(filtered)
	require.NoError(t, err)
	require.Equal(t, original, restored)
}

func TestFletcher32Filter_WithShuffleAndGZIP(t *testing.T) {
	// Test Fletcher32 at end of pipeline: Shuffle → GZIP → Fletcher32
	shuffleFilter := NewShuffleFilter(4)
	gzipFilter := NewGZIPFilter(6)
	fletcher32Filter := NewFletcher32Filter()

	pipeline := NewFilterPipeline()
	pipeline.AddFilter(shuffleFilter)
	pipeline.AddFilter(gzipFilter)
	pipeline.AddFilter(fletcher32Filter)

	// Create test data
	original := make([]byte, 1000*4)
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint32(original[i*4:], uint32(i))
	}

	// Apply pipeline (Shuffle → GZIP → Fletcher32)
	filtered, err := pipeline.Apply(original)
	require.NoError(t, err)

	// Data should be compressed and have checksum
	require.NotEqual(t, original, filtered)
	require.Less(t, len(filtered), len(original), "Data should be compressed")

	// Remove pipeline (Fletcher32 → GZIP → Shuffle)
	restored, err := pipeline.Remove(filtered)
	require.NoError(t, err)
	require.Equal(t, original, restored)

	// Test corruption detection
	corrupted := make([]byte, len(filtered))
	copy(corrupted, filtered)
	if len(corrupted) > 10 {
		corrupted[10] ^= 0xFF // Corrupt compressed data
	}

	_, err = pipeline.Remove(corrupted)
	require.Error(t, err, "Should detect corruption")
}

func TestCalculateFletcher32_KnownValues(t *testing.T) {
	// Test with known patterns
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{42}},
		{"two bytes", []byte{1, 2}},
		{"hello", []byte("hello")},
		{"zeros", make([]byte, 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum1 := calculateFletcher32(tt.data)
			checksum2 := calculateFletcher32(tt.data)

			// Same data should produce same checksum
			require.Equal(t, checksum1, checksum2)
		})
	}
}

func TestFletcher32Filter_MultipleCorruptionScenarios(t *testing.T) {
	filter := NewFletcher32Filter()
	original := make([]byte, 100)
	for i := range original {
		original[i] = byte(i)
	}

	withChecksum, err := filter.Apply(original)
	require.NoError(t, err)

	tests := []struct {
		name      string
		corruptFn func([]byte) []byte
	}{
		{"flip first byte", func(data []byte) []byte {
			corrupted := make([]byte, len(data))
			copy(corrupted, data)
			corrupted[0] ^= 0xFF
			return corrupted
		}},
		{"flip middle byte", func(data []byte) []byte {
			corrupted := make([]byte, len(data))
			copy(corrupted, data)
			corrupted[len(data)/2] ^= 0xFF
			return corrupted
		}},
		{"flip last data byte", func(data []byte) []byte {
			corrupted := make([]byte, len(data))
			copy(corrupted, data)
			corrupted[len(data)-5] ^= 0xFF // -5 to avoid checksum bytes
			return corrupted
		}},
		{"flip checksum byte", func(data []byte) []byte {
			corrupted := make([]byte, len(data))
			copy(corrupted, data)
			corrupted[len(data)-1] ^= 0xFF
			return corrupted
		}},
		{"truncate data", func(data []byte) []byte {
			return data[:len(data)-1]
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corrupted := tt.corruptFn(withChecksum)
			_, err := filter.Remove(corrupted)
			require.Error(t, err, "Should detect corruption in: %s", tt.name)
		})
	}
}
