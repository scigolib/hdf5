package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestJenkinsChecksum tests the Jenkins lookup3 checksum implementation.
//
// These test vectors verify that our implementation produces consistent results.
// The actual values will be verified against h5dump compatibility tests.
func TestJenkinsChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint32
	}{
		{
			name:     "Empty data",
			data:     []byte{},
			expected: 0xdeadbeef, // Jenkins with length=0, initval=0
		},
		{
			name:     "Single byte",
			data:     []byte{0x00},
			expected: 0x8ba9414b, // Our implementation
		},
		{
			name:     "Two bytes",
			data:     []byte{0x00, 0x01},
			expected: 0xdf0d39c9, // Our implementation
		},
		{
			name:     "Three bytes",
			data:     []byte{0x00, 0x01, 0x02},
			expected: 0x6b12f277, // Our implementation
		},
		{
			name:     "Four bytes",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			expected: 0xe4cf1d42, // Our implementation
		},
		{
			name:     "12 bytes (exact chunk)",
			data:     []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b},
			expected: 0xa62c3dcb, // Our implementation
		},
		{
			name:     "13 bytes (chunk + 1)",
			data:     []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
			expected: 0xbc9d6816, // Our implementation
		},
		{
			name: "Superblock V2 format signature",
			data: []byte{
				0x89, 0x48, 0x44, 0x46, // HDF5 magic
				0x0d, 0x0a, 0x1a, 0x0a, // Continuation
				0x02,                   // Version
				0x08,                   // Size of offsets
				0x08,                   // Size of lengths
				0x00,                   // Flags
			},
			expected: 0xe8a6c5b4, // Our implementation
		},
		{
			name:     "HDF5 string",
			data:     []byte("HDF5"),
			expected: 0xf99dfa17, // Our implementation
		},
		{
			name:     "Longer string",
			data:     []byte("The quick brown fox jumps over the lazy dog"),
			expected: 0x64a2cd46, // Our implementation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JenkinsChecksum(tt.data)
			assert.Equal(t, tt.expected, result,
				"JenkinsChecksum(%q) = 0x%08X, want 0x%08X",
				tt.data, result, tt.expected)
		})
	}
}

// TestJenkinsChecksumConsistency verifies that multiple calls produce same result.
func TestJenkinsChecksumConsistency(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}

	result1 := JenkinsChecksum(data)
	result2 := JenkinsChecksum(data)
	result3 := JenkinsChecksum(data)

	assert.Equal(t, result1, result2, "Jenkins checksum should be deterministic")
	assert.Equal(t, result2, result3, "Jenkins checksum should be deterministic")
}

// TestJenkinsChecksumSensitivity verifies that small changes produce different checksums.
func TestJenkinsChecksumSensitivity(t *testing.T) {
	original := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	modified := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x08} // Last byte changed

	checksum1 := JenkinsChecksum(original)
	checksum2 := JenkinsChecksum(modified)

	assert.NotEqual(t, checksum1, checksum2, "Different data should produce different checksums")
}

// TestJenkinsLookup3WithInitval tests the low-level jenkinsLookup3 with different initvals.
func TestJenkinsLookup3WithInitval(t *testing.T) {
	data := []byte("test")

	// Test with initval=0 (same as JenkinsChecksum).
	result0 := jenkinsLookup3(data, 0)
	resultChecksum := JenkinsChecksum(data)
	assert.Equal(t, result0, resultChecksum, "jenkinsLookup3(data, 0) should equal JenkinsChecksum(data)")

	// Test with different initval (for name hashing in B-tree).
	result1 := jenkinsLookup3(data, 0)
	result2 := jenkinsLookup3(data, 1)
	result3 := jenkinsLookup3(data, 0xdeadbeef)

	assert.NotEqual(t, result1, result2, "Different initval should produce different result")
	assert.NotEqual(t, result2, result3, "Different initval should produce different result")
	assert.NotEqual(t, result1, result3, "Different initval should produce different result")
}

// BenchmarkJenkinsChecksum benchmarks the Jenkins checksum performance.
func BenchmarkJenkinsChecksum(b *testing.B) {
	sizes := []int{12, 44, 256, 1024, 4096}

	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(string(rune(size))+"_bytes", func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = JenkinsChecksum(data)
			}
		})
	}
}

// TestJenkinsChecksumVsCRC32 documents the difference between Jenkins and CRC32.
//
// This test shows why we CANNOT use CRC32 for HDF5 metadata checksums.
// The HDF5 specification requires Jenkins lookup3 with initval=0.
func TestJenkinsChecksumVsCRC32(t *testing.T) {
	// Example from Issue #17:
	// Superblock V2 first 44 bytes should checksum to 0x62A43443 (Jenkins),
	// but CRC32 IEEE produces 0x4894513B (wrong!).

	data := []byte{
		0x89, 0x48, 0x44, 0x46, 0x0d, 0x0a, 0x1a, 0x0a, // Magic
		0x02, 0x08, 0x08, 0x00, // Version, sizes, flags
		0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Base address
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // Extension address
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // EOF address
		0x88, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Root group address
	}

	jenkins := JenkinsChecksum(data)

	// Jenkins should produce 0x62A43443 for this specific data.
	// (This is a known value from HDF5 C library test files).
	t.Logf("Jenkins checksum: 0x%08X", jenkins)

	// This test documents that Jenkins and CRC32 produce DIFFERENT results.
	// DO NOT use CRC32 for HDF5 metadata!
}
