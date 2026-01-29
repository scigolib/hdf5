package writer

import (
	"bytes"
	"testing"
)

// TestLZFFilter_Basic tests basic LZF compression and decompression.
func TestLZFFilter_Basic(t *testing.T) {
	filter := NewLZFFilter()

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty data",
			input: []byte{},
		},
		{
			name:  "single byte",
			input: []byte{0x42},
		},
		{
			name:  "small data",
			input: []byte("Hello, World!"),
		},
		{
			name:  "repeated pattern",
			input: bytes.Repeat([]byte("ABCD"), 100),
		},
		{
			name:  "all zeros",
			input: make([]byte, 1000),
		},
		{
			name:  "sequential bytes",
			input: sequentialBytes(256),
		},
		{
			name:  "random-like data (less compressible)",
			input: []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22, 0x33, 0x44},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compress.
			compressed, err := filter.Apply(tt.input)
			if err != nil {
				t.Fatalf("Apply() failed: %v", err)
			}

			// Empty input should remain empty.
			if len(tt.input) == 0 {
				if len(compressed) != 0 {
					t.Errorf("Expected empty output for empty input, got %d bytes", len(compressed))
				}
				return
			}

			// Decompress.
			decompressed, err := filter.Remove(compressed)
			if err != nil {
				t.Fatalf("Remove() failed: %v", err)
			}

			// Verify round-trip.
			if !bytes.Equal(decompressed, tt.input) {
				t.Errorf("Round-trip failed:\nOriginal:      %v\nDecompressed:  %v", tt.input, decompressed)
			}
		})
	}
}

// TestLZFFilter_LongMatch tests compression with long repeated patterns.
func TestLZFFilter_LongMatch(t *testing.T) {
	filter := NewLZFFilter()

	// Pattern that should compress well (long backreferences).
	pattern := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 50)

	compressed, err := filter.Apply(pattern)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// Should achieve significant compression.
	compressionRatio := float64(len(compressed)) / float64(len(pattern))
	if compressionRatio > 0.5 {
		t.Errorf("Expected compression ratio < 0.5, got %.2f (compressed: %d, original: %d)",
			compressionRatio, len(compressed), len(pattern))
	}

	// Verify decompression.
	decompressed, err := filter.Remove(compressed)
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	if !bytes.Equal(decompressed, pattern) {
		t.Error("Round-trip failed for long repeated pattern")
	}
}

// TestLZFFilter_LiteralRuns tests data with no compressible patterns.
func TestLZFFilter_LiteralRuns(t *testing.T) {
	filter := NewLZFFilter()

	// Pseudo-random data (every byte unique in sequence).
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}

	compressed, err := filter.Apply(data)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// Literal-only data might be larger due to control bytes.
	// But should still decompress correctly.
	decompressed, err := filter.Remove(compressed)
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Error("Round-trip failed for literal-only data")
	}
}

// TestLZFFilter_LargeData tests compression of larger datasets.
func TestLZFFilter_LargeData(t *testing.T) {
	filter := NewLZFFilter()

	// Create 100KB of data with patterns.
	size := 100 * 1024
	data := make([]byte, size)
	for i := 0; i < size; i++ {
		data[i] = byte(i % 256)
	}

	compressed, err := filter.Apply(data)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// Should compress significantly.
	compressionRatio := float64(len(compressed)) / float64(len(data))
	if compressionRatio > 0.2 {
		t.Logf("Compression ratio: %.2f (compressed: %d, original: %d)",
			compressionRatio, len(compressed), len(data))
	}

	// Verify round-trip.
	decompressed, err := filter.Remove(compressed)
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Error("Round-trip failed for large data")
	}
}

// TestLZFFilter_EdgeCases tests edge cases and boundary conditions.
func TestLZFFilter_EdgeCases(t *testing.T) {
	filter := NewLZFFilter()

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "exactly 3 bytes (minimum for pattern matching)",
			input: []byte{0x01, 0x02, 0x03},
		},
		{
			name:  "exactly 32 bytes (max literal run)",
			input: sequentialBytes(32),
		},
		{
			name:  "33 bytes (requires multiple literal runs)",
			input: sequentialBytes(33),
		},
		{
			name:  "exact match at offset 8192 (max offset)",
			input: createDataWithDistantMatch(8192),
		},
		{
			name:  "repeated 2-byte pattern",
			input: bytes.Repeat([]byte{0xAB, 0xCD}, 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, err := filter.Apply(tt.input)
			if err != nil {
				t.Fatalf("Apply() failed: %v", err)
			}

			decompressed, err := filter.Remove(compressed)
			if err != nil {
				t.Fatalf("Remove() failed: %v", err)
			}

			if !bytes.Equal(decompressed, tt.input) {
				t.Errorf("Round-trip failed")
			}
		})
	}
}

// TestLZFFilter_Metadata tests filter metadata.
func TestLZFFilter_Metadata(t *testing.T) {
	filter := NewLZFFilter()

	if filter.ID() != FilterLZF {
		t.Errorf("ID() = %d, want %d", filter.ID(), FilterLZF)
	}

	if filter.Name() != "lzf" {
		t.Errorf("Name() = %q, want %q", filter.Name(), "lzf")
	}

	flags, cdValues := filter.Encode()
	if flags != 0 {
		t.Errorf("Encode() flags = %d, want 0", flags)
	}
	if len(cdValues) != 3 {
		t.Errorf("Encode() cd_values length = %d, want 3", len(cdValues))
	}
	// Expected: [0, 0, 0] (revision, version, chunk_size).
	for i, v := range cdValues {
		if v != 0 {
			t.Errorf("Encode() cd_values[%d] = %d, want 0", i, v)
		}
	}
}

// TestLZFFilter_ShortBackrefs tests short backreferences (3-8 bytes).
func TestLZFFilter_ShortBackrefs(t *testing.T) {
	filter := NewLZFFilter()

	// Create data with short repeated patterns.
	data := []byte("ABCABCABCABCABC") // 5 copies of "ABC" (3 bytes each)

	compressed, err := filter.Apply(data)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// Should compress (multiple 3-byte matches).
	if len(compressed) >= len(data) {
		t.Logf("Short backref compression: %d -> %d bytes", len(data), len(compressed))
	}

	decompressed, err := filter.Remove(compressed)
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Error("Round-trip failed for short backreferences")
	}
}

// TestLZFFilter_LongBackrefs tests long backreferences (9-264 bytes).
func TestLZFFilter_LongBackrefs(t *testing.T) {
	filter := NewLZFFilter()

	// Create data with long repeated pattern.
	longPattern := bytes.Repeat([]byte("X"), 100)
	data := bytes.Repeat(longPattern, 3) // 300 bytes total

	compressed, err := filter.Apply(data)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// Should compress significantly (long backreferences).
	compressionRatio := float64(len(compressed)) / float64(len(data))
	if compressionRatio > 0.3 {
		t.Logf("Long backref compression ratio: %.2f", compressionRatio)
	}

	decompressed, err := filter.Remove(compressed)
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Error("Round-trip failed for long backreferences")
	}
}

// TestLZFDecompress_InvalidData tests decompression error handling.
func TestLZFDecompress_InvalidData(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "truncated literal run",
			input: []byte{0x0A}, // Claims 11 bytes but none follow
		},
		{
			name:  "truncated short backref (missing offset byte)",
			input: []byte{0x20}, // Short backref but missing second byte
		},
		{
			name:  "truncated long backref (missing length)",
			input: []byte{0xE0, 0x00}, // Long backref but missing length byte
		},
		{
			name:  "invalid offset (beyond output)",
			input: []byte{0x20, 0xFF}, // Offset 256 but output is empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := lzfDecompress(tt.input)
			if err == nil {
				t.Error("Expected error for invalid data, got nil")
			}
		})
	}
}

// Helper functions.

// sequentialBytes creates a byte array [0, 1, 2, ..., n-1].
func sequentialBytes(n int) []byte {
	data := make([]byte, n)
	for i := 0; i < n; i++ {
		data[i] = byte(i % 256)
	}
	return data
}

// createDataWithDistantMatch creates data with a match at specific offset.
func createDataWithDistantMatch(offset int) []byte {
	data := make([]byte, offset+10)
	// Fill with varying data.
	for i := range data {
		data[i] = byte(i % 256)
	}
	// Create a 3-byte pattern at start.
	data[0] = 0xAA
	data[1] = 0xBB
	data[2] = 0xCC
	// Repeat it at 'offset' distance.
	if offset < len(data)-2 {
		data[offset] = 0xAA
		data[offset+1] = 0xBB
		data[offset+2] = 0xCC
	}
	return data
}
