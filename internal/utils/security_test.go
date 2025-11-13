package utils

import (
	"math"
	"testing"
)

// TestCVE_2025_6269_AttributeOverflow tests protection against heap overflow in attributes.
// This CVE involved malicious files with huge element counts that would overflow
// when multiplied by element size, causing heap buffer overflows.
func TestCVE_2025_6269_AttributeOverflow(t *testing.T) {
	tests := []struct {
		name         string
		elements     uint64
		elementSize  uint64
		shouldFail   bool
		description  string
	}{
		{
			name:        "normal attribute - 100 int32 elements",
			elements:    100,
			elementSize: 4,
			shouldFail:  false,
			description: "Normal attribute size should succeed",
		},
		{
			name:        "large but valid attribute - 1M float64 elements",
			elements:    1_000_000,
			elementSize: 8,
			shouldFail:  false,
			description: "Large but valid attribute should succeed",
		},
		{
			name:        "overflow attack - MaxUint64/4 elements",
			elements:    math.MaxUint64 / 4,
			elementSize: 8,
			shouldFail:  true,
			description: "Malicious overflow should be caught",
		},
		{
			name:        "overflow attack - MaxUint64/7 int64 elements",
			elements:    math.MaxUint64 / 7,
			elementSize: 8,
			shouldFail:  true,
			description: "Malicious overflow should be caught",
		},
		{
			name:        "exceeds MaxAttributeSize",
			elements:    (MaxAttributeSize / 4) + 1000,
			elementSize: 4,
			shouldFail:  true,
			description: "Attribute exceeding MaxAttributeSize should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalBytes, err := SafeMultiply(tt.elements, tt.elementSize)

			// Check for overflow first
			if err != nil {
				if !tt.shouldFail {
					t.Errorf("Unexpected overflow error for %s: %v", tt.description, err)
				}
				return // Overflow detected, test passed for failure case
			}

			// Check against MaxAttributeSize
			validateErr := ValidateBufferSize(totalBytes, MaxAttributeSize, "attribute")
			if tt.shouldFail {
				if validateErr == nil {
					t.Errorf("Expected size validation error for %s (totalBytes=%d), got nil", tt.description, totalBytes)
				}
			} else {
				if validateErr != nil {
					t.Errorf("Unexpected validation error for %s: %v", tt.description, validateErr)
				}
			}
		})
	}
}

// TestCVE_2025_2926_StringOverflow tests protection against stack overflow in strings.
// This CVE involved malicious files with excessively large string sizes that would
// exhaust stack/heap space.
func TestCVE_2025_2926_StringOverflow(t *testing.T) {
	tests := []struct {
		name        string
		stringSize  uint64
		shouldFail  bool
		description string
	}{
		{
			name:        "normal string - 256 bytes",
			stringSize:  256,
			shouldFail:  false,
			description: "Normal string size should succeed",
		},
		{
			name:        "large string - 1MB",
			stringSize:  1024 * 1024,
			shouldFail:  false,
			description: "Large string (1MB) should succeed",
		},
		{
			name:        "max allowed string - 16MB",
			stringSize:  MaxStringSize,
			shouldFail:  false,
			description: "String at MaxStringSize should succeed",
		},
		{
			name:        "overflow attack - 100MB string",
			stringSize:  100 * 1024 * 1024,
			shouldFail:  true,
			description: "Malicious 100MB string should be rejected",
		},
		{
			name:        "overflow attack - 1GB string",
			stringSize:  1024 * 1024 * 1024,
			shouldFail:  true,
			description: "Malicious 1GB string should be rejected",
		},
		{
			name:        "overflow attack - MaxUint64 string",
			stringSize:  math.MaxUint64,
			shouldFail:  true,
			description: "Malicious MaxUint64 string should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBufferSize(tt.stringSize, MaxStringSize, "string")

			if tt.shouldFail {
				if err == nil {
					t.Errorf("Expected size validation error for %s, got nil", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				}
			}
		})
	}
}

// TestCVE_2025_44905_HyperslabOverflow tests protection against integer overflow in hyperslab.
// This CVE involved malicious files with huge hyperslab dimensions that would overflow
// when calculating total elements.
func TestCVE_2025_44905_HyperslabOverflow(t *testing.T) {
	tests := []struct {
		name        string
		count       []uint64
		shouldFail  bool
		description string
	}{
		{
			name:        "normal hyperslab - 100x100",
			count:       []uint64{100, 100},
			shouldFail:  false,
			description: "Normal hyperslab should succeed",
		},
		{
			name:        "large hyperslab - 10000x10000",
			count:       []uint64{10000, 10000},
			shouldFail:  false,
			description: "Large hyperslab (100M elements) should succeed",
		},
		{
			name:        "max allowed hyperslab - close to 1B elements",
			count:       []uint64{31622, 31622}, // ~1 billion elements
			shouldFail:  false,
			description: "Hyperslab close to MaxHyperslabElements should succeed",
		},
		{
			name:        "overflow attack - MaxUint64/100 x 200",
			count:       []uint64{math.MaxUint64 / 100, 200},
			shouldFail:  true,
			description: "Malicious hyperslab overflow should be caught",
		},
		{
			name:        "overflow attack - huge 3D selection",
			count:       []uint64{100000, 100000, 1000},
			shouldFail:  true,
			description: "Malicious 3D hyperslab (10 trillion elements) should be caught",
		},
		{
			name:        "zero count dimension",
			count:       []uint64{100, 0, 100},
			shouldFail:  true,
			description: "Zero count should be rejected",
		},
		{
			name:        "empty count",
			count:       []uint64{},
			shouldFail:  true,
			description: "Empty count should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateHyperslabElements(tt.count)

			if tt.shouldFail {
				if err == nil {
					t.Errorf("Expected hyperslab overflow error for %s, got nil", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				}
			}
		})
	}
}

// TestHyperslabBoundsValidation tests ValidateHyperslabBounds function.
// This tests the CVE-2025-44905 fix for stride overflow and bounds checking.
func TestHyperslabBoundsValidation(t *testing.T) {
	tests := []struct {
		name        string
		start       []uint64
		count       []uint64
		stride      []uint64
		dims        []uint64
		shouldFail  bool
		description string
	}{
		{
			name:        "valid simple selection",
			start:       []uint64{0, 0},
			count:       []uint64{10, 10},
			stride:      []uint64{1, 1},
			dims:        []uint64{100, 100},
			shouldFail:  false,
			description: "Normal selection should succeed",
		},
		{
			name:        "valid strided selection",
			start:       []uint64{0, 0},
			count:       []uint64{10, 10},
			stride:      []uint64{5, 5},
			dims:        []uint64{100, 100},
			shouldFail:  false,
			description: "Strided selection should succeed",
		},
		{
			name:        "out of bounds - exceeds dimension",
			start:       []uint64{90, 90},
			count:       []uint64{20, 20},
			stride:      []uint64{1, 1},
			dims:        []uint64{100, 100},
			shouldFail:  true,
			description: "Selection exceeding bounds should fail",
		},
		{
			name:        "stride overflow attack",
			start:       []uint64{0},
			count:       []uint64{math.MaxUint64 / 100},
			stride:      []uint64{200},
			dims:        []uint64{math.MaxUint64},
			shouldFail:  true,
			description: "Stride causing overflow should be caught",
		},
		{
			name:        "dimension mismatch",
			start:       []uint64{0, 0},
			count:       []uint64{10, 10},
			stride:      []uint64{1, 1},
			dims:        []uint64{100, 100, 100}, // 3D vs 2D
			shouldFail:  true,
			description: "Dimension mismatch should fail",
		},
		{
			name:        "zero count",
			start:       []uint64{0},
			count:       []uint64{0},
			stride:      []uint64{1},
			dims:        []uint64{100},
			shouldFail:  true,
			description: "Zero count should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHyperslabBounds(tt.start, tt.count, tt.stride, tt.dims)

			if tt.shouldFail {
				if err == nil {
					t.Errorf("Expected bounds validation error for %s, got nil", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				}
			}
		})
	}
}

// TestCVE_2025_7067_ChunkOverflow tests protection against buffer overflow in chunk reading.
// This CVE involved malicious chunk dimensions that would overflow when calculating size.
func TestCVE_2025_7067_ChunkOverflow(t *testing.T) {
	tests := []struct {
		name        string
		dimensions  []uint32
		elementSize uint64
		shouldFail  bool
		description string
	}{
		{
			name:        "normal chunk - 100x100",
			dimensions:  []uint32{100, 100},
			elementSize: 8,
			shouldFail:  false,
			description: "Normal chunk size should succeed",
		},
		{
			name:        "large chunk - 1000x1000x100",
			dimensions:  []uint32{1000, 1000, 100},
			elementSize: 4,
			shouldFail:  false,
			description: "Large chunk (400MB) should succeed",
		},
		{
			name:        "max allowed chunk size",
			dimensions:  []uint32{1024, 1024, 128},  // Close to 1GB
			elementSize: 8,
			shouldFail:  false,
			description: "Chunk close to MaxChunkSize should succeed",
		},
		{
			name:        "overflow attack - huge dimensions",
			dimensions:  []uint32{math.MaxUint32 / 2, 10},
			elementSize: 8,
			shouldFail:  true,
			description: "Malicious chunk overflow should be caught",
		},
		{
			name:        "overflow attack - exceeds MaxChunkSize",
			dimensions:  []uint32{2048, 2048, 2048},
			elementSize: 8,
			shouldFail:  true,
			description: "Chunk exceeding MaxChunkSize should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalBytes, err := CalculateChunkSize(tt.dimensions, tt.elementSize)

			// Check for overflow first
			if err != nil {
				if !tt.shouldFail {
					t.Errorf("Unexpected overflow error for %s: %v", tt.description, err)
				}
				return // Overflow detected, test passed for failure case
			}

			// Check against MaxChunkSize
			validateErr := ValidateBufferSize(totalBytes, MaxChunkSize, "chunk")
			if tt.shouldFail {
				if validateErr == nil {
					t.Errorf("Expected size validation error for %s (totalBytes=%d), got nil", tt.description, totalBytes)
				}
			} else {
				if validateErr != nil {
					t.Errorf("Unexpected validation error for %s: %v", tt.description, validateErr)
				}
			}
		})
	}
}

// TestSafeMultiplyEdgeCases tests edge cases in SafeMultiply.
func TestSafeMultiplyEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		a          uint64
		b          uint64
		wantResult uint64
		wantError  bool
	}{
		{
			name:       "zero multiplication",
			a:          0,
			b:          math.MaxUint64,
			wantResult: 0,
			wantError:  false,
		},
		{
			name:       "one multiplication",
			a:          1,
			b:          12345,
			wantResult: 12345,
			wantError:  false,
		},
		{
			name:       "small numbers",
			a:          123,
			b:          456,
			wantResult: 56088,
			wantError:  false,
		},
		{
			name:       "max uint64 - 1",
			a:          math.MaxUint64,
			b:          1,
			wantResult: math.MaxUint64,
			wantError:  false,
		},
		{
			name:       "overflow - max * 2",
			a:          math.MaxUint64,
			b:          2,
			wantResult: 0,
			wantError:  true,
		},
		{
			name:       "overflow - large numbers",
			a:          math.MaxUint64 / 2,
			b:          3,
			wantResult: 0,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SafeMultiply(tt.a, tt.b)

			//nolint:nestif // Test validation logic, acceptable complexity for tests
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error for %s, got nil", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
				if result != tt.wantResult {
					t.Errorf("Wrong result for %s: got %d, want %d", tt.name, result, tt.wantResult)
				}
			}
		})
	}
}

// TestValidateBufferSizeEdgeCases tests edge cases in ValidateBufferSize.
func TestValidateBufferSizeEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		size      uint64
		maxSize   uint64
		wantError bool
	}{
		{
			name:      "zero size",
			size:      0,
			maxSize:   1000,
			wantError: true, // Zero size is invalid
		},
		{
			name:      "size equals max",
			size:      1000,
			maxSize:   1000,
			wantError: false,
		},
		{
			name:      "size just under max",
			size:      999,
			maxSize:   1000,
			wantError: false,
		},
		{
			name:      "size exceeds max",
			size:      1001,
			maxSize:   1000,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBufferSize(tt.size, tt.maxSize, "test")

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error for %s, got nil", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
			}
		})
	}
}
