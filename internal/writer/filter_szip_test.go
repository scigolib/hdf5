package writer

import (
	"strings"
	"testing"
)

func TestSZIPFilter_ID(t *testing.T) {
	filter := NewSZIPFilter(36, 16, 32, 0)
	if filter.ID() != FilterSZIP {
		t.Errorf("ID() = %d, want %d", filter.ID(), FilterSZIP)
	}
}

func TestSZIPFilter_Name(t *testing.T) {
	filter := NewSZIPFilter(36, 16, 32, 0)
	if filter.Name() != "szip" {
		t.Errorf("Name() = %q, want %q", filter.Name(), "szip")
	}
}

func TestSZIPFilter_Apply(t *testing.T) {
	tests := []struct {
		name           string
		optionMask     uint32
		pixelsPerBlock uint32
		bitsPerPixel   uint32
		pixelsPerScan  uint32
		data           []byte
		wantErrContain string
	}{
		{
			name:           "NN+EC mode",
			optionMask:     36, // NN=32 + EC=4
			pixelsPerBlock: 16,
			bitsPerPixel:   32,
			pixelsPerScan:  0,
			data:           []byte{1, 2, 3, 4, 5, 6, 7, 8},
			wantErrContain: "not implemented",
		},
		{
			name:           "RAW mode",
			optionMask:     128, // RAW mode
			pixelsPerBlock: 8,
			bitsPerPixel:   16,
			pixelsPerScan:  256,
			data:           []byte{10, 20, 30, 40},
			wantErrContain: "not implemented",
		},
		{
			name:           "empty data",
			optionMask:     36,
			pixelsPerBlock: 16,
			bitsPerPixel:   32,
			pixelsPerScan:  0,
			data:           []byte{},
			wantErrContain: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewSZIPFilter(tt.optionMask, tt.pixelsPerBlock, tt.bitsPerPixel, tt.pixelsPerScan)
			_, err := filter.Apply(tt.data)

			if err == nil {
				t.Fatal("Apply() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Errorf("Apply() error = %q, want substring %q", err.Error(), tt.wantErrContain)
			}

			// Verify error message mentions libaec
			if !strings.Contains(err.Error(), "libaec") {
				t.Errorf("Apply() error should mention 'libaec', got: %q", err.Error())
			}
		})
	}
}

func TestSZIPFilter_Remove(t *testing.T) {
	tests := []struct {
		name           string
		data           []byte
		wantErrContain string
	}{
		{
			name:           "compressed data",
			data:           []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0},
			wantErrContain: "not implemented",
		},
		{
			name:           "empty data",
			data:           []byte{},
			wantErrContain: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewSZIPFilter(36, 16, 32, 0)
			_, err := filter.Remove(tt.data)

			if err == nil {
				t.Fatal("Remove() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Errorf("Remove() error = %q, want substring %q", err.Error(), tt.wantErrContain)
			}

			// Verify error message mentions libaec
			if !strings.Contains(err.Error(), "libaec") {
				t.Errorf("Remove() error should mention 'libaec', got: %q", err.Error())
			}
		})
	}
}

func TestSZIPFilter_Encode(t *testing.T) {
	tests := []struct {
		name           string
		optionMask     uint32
		pixelsPerBlock uint32
		bitsPerPixel   uint32
		pixelsPerScan  uint32
		wantFlags      uint16
		wantCDValues   []uint32
	}{
		{
			name:           "NN+EC mode, 32 bpp, 16 ppb",
			optionMask:     36, // NN=32 + EC=4
			pixelsPerBlock: 16,
			bitsPerPixel:   32,
			pixelsPerScan:  0,
			wantFlags:      0,
			wantCDValues:   []uint32{32, 36, 16, 0},
		},
		{
			name:           "RAW mode, 16 bpp, 8 ppb, 2D data",
			optionMask:     128, // RAW mode
			pixelsPerBlock: 8,
			bitsPerPixel:   16,
			pixelsPerScan:  256,
			wantFlags:      0,
			wantCDValues:   []uint32{16, 128, 8, 256},
		},
		{
			name:           "EC only, 8 bpp, 32 ppb",
			optionMask:     4, // EC only
			pixelsPerBlock: 32,
			bitsPerPixel:   8,
			pixelsPerScan:  0,
			wantFlags:      0,
			wantCDValues:   []uint32{8, 4, 32, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewSZIPFilter(tt.optionMask, tt.pixelsPerBlock, tt.bitsPerPixel, tt.pixelsPerScan)
			flags, cdValues := filter.Encode()

			if flags != tt.wantFlags {
				t.Errorf("Encode() flags = %d, want %d", flags, tt.wantFlags)
			}

			if len(cdValues) != len(tt.wantCDValues) {
				t.Fatalf("Encode() cdValues length = %d, want %d", len(cdValues), len(tt.wantCDValues))
			}

			for i, val := range cdValues {
				if val != tt.wantCDValues[i] {
					t.Errorf("Encode() cdValues[%d] = %d, want %d", i, val, tt.wantCDValues[i])
				}
			}
		})
	}
}

//nolint:gocognit // Test validates multiple parameters, complexity acceptable
func TestSZIPFilter_ParameterValidation(t *testing.T) {
	// Test that NewSZIPFilter accepts various parameter combinations.
	// Since it's a stub, we just verify construction doesn't panic.
	tests := []struct {
		name           string
		optionMask     uint32
		pixelsPerBlock uint32
		bitsPerPixel   uint32
		pixelsPerScan  uint32
	}{
		{"minimal", 4, 8, 8, 0},
		{"typical NN+EC", 36, 16, 32, 0},
		{"high compression", 36, 32, 64, 1024},
		{"RAW mode", 128, 16, 16, 512},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewSZIPFilter(tt.optionMask, tt.pixelsPerBlock, tt.bitsPerPixel, tt.pixelsPerScan)
			if filter == nil {
				t.Fatal("NewSZIPFilter() returned nil")
			}

			// Verify parameters are stored correctly
			flags, cdValues := filter.Encode()
			if flags != 0 {
				t.Errorf("unexpected flags: %d", flags)
			}

			if len(cdValues) != 4 {
				t.Fatalf("cdValues length = %d, want 4", len(cdValues))
			}

			if cdValues[0] != tt.bitsPerPixel {
				t.Errorf("bitsPerPixel = %d, want %d", cdValues[0], tt.bitsPerPixel)
			}
			if cdValues[1] != tt.optionMask {
				t.Errorf("optionMask = %d, want %d", cdValues[1], tt.optionMask)
			}
			if cdValues[2] != tt.pixelsPerBlock {
				t.Errorf("pixelsPerBlock = %d, want %d", cdValues[2], tt.pixelsPerBlock)
			}
			if cdValues[3] != tt.pixelsPerScan {
				t.Errorf("pixelsPerScan = %d, want %d", cdValues[3], tt.pixelsPerScan)
			}
		})
	}
}

func TestSZIPFilter_ErrorMessages(t *testing.T) {
	filter := NewSZIPFilter(36, 16, 32, 0)

	// Test Apply error message quality
	t.Run("Apply error message", func(t *testing.T) {
		_, err := filter.Apply([]byte{1, 2, 3, 4})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := err.Error()

		// Check for key information in error message
		requiredStrings := []string{
			"not implemented",
			"libaec",
			"GZIP", // Should suggest GZIP as alternative
		}

		for _, required := range requiredStrings {
			if !strings.Contains(errMsg, required) {
				t.Errorf("error message missing %q: %q", required, errMsg)
			}
		}
	})

	// Test Remove error message quality
	t.Run("Remove error message", func(t *testing.T) {
		_, err := filter.Remove([]byte{0xAA, 0xBB, 0xCC, 0xDD})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := err.Error()

		// Check for key information in error message
		requiredStrings := []string{
			"not implemented",
			"libaec",
			"HDF5 C library", // Should mention C library as alternative
		}

		for _, required := range requiredStrings {
			if !strings.Contains(errMsg, required) {
				t.Errorf("error message missing %q: %q", required, errMsg)
			}
		}
	})
}
