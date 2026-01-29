package writer

import (
	"compress/bzip2"
	"io"
	"strings"
	"testing"
)

func TestBZIP2Filter_ID(t *testing.T) {
	filter := NewBZIP2Filter(9)
	if filter.ID() != FilterBZIP2 {
		t.Errorf("Expected FilterBZIP2 (307), got %d", filter.ID())
	}
}

func TestBZIP2Filter_Name(t *testing.T) {
	filter := NewBZIP2Filter(9)
	if filter.Name() != "bzip2" {
		t.Errorf("Expected 'bzip2', got %s", filter.Name())
	}
}

func TestBZIP2Filter_Encode(t *testing.T) {
	tests := []struct {
		name      string
		blockSize int
		wantCD    []uint32
	}{
		{"max compression", 9, []uint32{9}},
		{"fast compression", 1, []uint32{1}},
		{"medium compression", 5, []uint32{5}},
		{"invalid low (defaults to 9)", 0, []uint32{9}},
		{"invalid high (defaults to 9)", 10, []uint32{9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewBZIP2Filter(tt.blockSize)
			flags, cdValues := filter.Encode()

			if flags != 0 {
				t.Errorf("Expected flags=0, got %d", flags)
			}

			if len(cdValues) != 1 {
				t.Fatalf("Expected 1 CD value, got %d", len(cdValues))
			}

			if cdValues[0] != tt.wantCD[0] {
				t.Errorf("Expected CD[0]=%d, got %d", tt.wantCD[0], cdValues[0])
			}
		})
	}
}

func TestBZIP2Filter_Remove(t *testing.T) {
	// Test decompression with a simple BZIP2-compressed string.
	// This compressed data was created using: echo -n "Hello, BZIP2!" | bzip2 | base64
	// The actual bytes represent "Hello, BZIP2!" compressed with BZIP2.

	// For testing, we'll compress some data manually and decompress it.
	original := []byte("The quick brown fox jumps over the lazy dog")

	// Since we can't compress in pure Go stdlib, we'll create a simple test
	// that verifies the Remove function accepts and processes BZIP2 data.
	// For real BZIP2 data, this would work with actual compressed chunks from HDF5 files.

	t.Run("empty data", func(t *testing.T) {
		filter := NewBZIP2Filter(9)
		result, err := filter.Remove([]byte{})
		if err != nil {
			t.Errorf("Remove failed on empty data: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("Expected empty result, got %d bytes", len(result))
		}
	})

	t.Run("invalid bzip2 data", func(t *testing.T) {
		filter := NewBZIP2Filter(9)
		_, err := filter.Remove([]byte("not bzip2 data"))
		if err == nil {
			t.Error("Expected error for invalid BZIP2 data, got nil")
		}
		if !strings.Contains(err.Error(), "bzip2 decompression failed") {
			t.Errorf("Expected 'bzip2 decompression failed' error, got: %v", err)
		}
	})

	// Test with actual BZIP2-compressed data (manually created for testing).
	// This is a BZIP2-compressed version of "test".
	t.Run("valid bzip2 data", func(t *testing.T) {
		// BZIP2 magic header: "BZ" (0x425A)
		// This is a minimal BZIP2 stream containing "test"
		// Generated using: echo -n "test" | bzip2 | xxd -p
		compressedHex := "425a6839314159265359338bcfac000001018002000c00200021981984185dc914e14240ce2f3eb0"
		compressed := hexToBytes(compressedHex)

		filter := NewBZIP2Filter(9)
		result, err := filter.Remove(compressed)
		if err != nil {
			t.Errorf("Remove failed: %v", err)
		}

		expected := "test"
		if string(result) != expected {
			t.Errorf("Expected %q, got %q", expected, string(result))
		}
	})

	// Integration test: verify bzip2.NewReader works as expected.
	t.Run("stdlib bzip2 reader", func(t *testing.T) {
		// Known BZIP2 header for "test".
		compressedHex := "425a6839314159265359338bcfac000001018002000c00200021981984185dc914e14240ce2f3eb0"
		compressed := hexToBytes(compressedHex)

		reader := bzip2.NewReader(io.NopCloser(strings.NewReader(string(compressed))))
		result, err := io.ReadAll(reader)
		if err != nil {
			t.Errorf("stdlib bzip2.NewReader failed: %v", err)
		}

		expected := "test"
		if string(result) != expected {
			t.Errorf("Expected %q, got %q", expected, string(result))
		}
	})

	_ = original // Reserved for future compression tests when Apply is implemented
}

func TestBZIP2Filter_Apply(t *testing.T) {
	filter := NewBZIP2Filter(9)
	_, err := filter.Apply([]byte("test data"))

	if err == nil {
		t.Error("Expected error (compression not implemented), got nil")
	}

	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("Expected 'not implemented' error, got: %v", err)
	}
}

// hexToByte converts a 2-character hex string to a byte.
func hexToByte(s string) byte {
	var b byte
	for i := 0; i < 2; i++ {
		b <<= 4
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			b |= c - '0'
		case c >= 'a' && c <= 'f':
			b |= c - 'a' + 10
		case c >= 'A' && c <= 'F':
			b |= c - 'A' + 10
		}
	}
	return b
}

// hexToBytes converts a hex string to bytes.
func hexToBytes(s string) []byte {
	result := make([]byte, len(s)/2)
	for i := 0; i < len(result); i++ {
		result[i] = hexToByte(s[i*2 : i*2+2])
	}
	return result
}
