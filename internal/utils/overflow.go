package utils

import (
	"fmt"
	"math"
)

// CheckMultiplyOverflow checks if multiplying two uint64 values would overflow.
// Returns an error if overflow would occur.
func CheckMultiplyOverflow(a, b uint64) error {
	if a == 0 || b == 0 {
		return nil // No overflow when either is zero
	}

	if a > math.MaxUint64/b {
		return fmt.Errorf("multiplication overflow: %d * %d exceeds uint64 max", a, b)
	}

	return nil
}

// SafeMultiply multiplies two uint64 values and returns the result if no overflow occurs.
// Returns 0 and an error if overflow would occur.
func SafeMultiply(a, b uint64) (uint64, error) {
	if err := CheckMultiplyOverflow(a, b); err != nil {
		return 0, err
	}
	return a * b, nil
}

// CalculateChunkSize safely calculates the total chunk size by multiplying dimensions and element size.
// Returns an error if overflow would occur.
func CalculateChunkSize(dimensions []uint32, elementSize uint64) (uint64, error) {
	if len(dimensions) == 0 {
		return 0, fmt.Errorf("no dimensions provided")
	}

	if elementSize == 0 {
		return 0, fmt.Errorf("element size cannot be zero")
	}

	// Calculate product of all dimensions
	size := uint64(1)
	for i, dim := range dimensions {
		dimU64 := uint64(dim)

		// Check for overflow before multiplication
		if dimU64 > 0 && size > math.MaxUint64/dimU64 {
			return 0, fmt.Errorf("chunk size overflow at dimension %d: dimensions too large", i)
		}

		size *= dimU64
	}

	// Check element size multiplication
	if size > math.MaxUint64/elementSize {
		return 0, fmt.Errorf("chunk size overflow: total size too large (dims product: %d, elem size: %d)", size, elementSize)
	}

	return size * elementSize, nil
}

// CalculateChunkSize64 safely calculates chunk size for 64-bit chunk dimensions (HDF5 2.0.0+).
// This function will be needed for TASK-025 (64-bit chunk support).
func CalculateChunkSize64(dimensions []uint64, elementSize uint64) (uint64, error) {
	if len(dimensions) == 0 {
		return 0, fmt.Errorf("no dimensions provided")
	}

	if elementSize == 0 {
		return 0, fmt.Errorf("element size cannot be zero")
	}

	// Calculate product of all dimensions
	size := uint64(1)
	for i, dim := range dimensions {
		// Check for overflow before multiplication
		if dim > 0 && size > math.MaxUint64/dim {
			return 0, fmt.Errorf("chunk size overflow at dimension %d: dimensions too large", i)
		}

		size *= dim
	}

	// Check element size multiplication
	if size > math.MaxUint64/elementSize {
		return 0, fmt.Errorf("chunk size overflow: total size too large (dims product: %d, elem size: %d)", size, elementSize)
	}

	return size * elementSize, nil
}

// ValidateBufferSize validates that a buffer size is within reasonable limits.
// maxSize parameter allows different limits for different use cases.
func ValidateBufferSize(size, maxSize uint64, description string) error {
	if size == 0 {
		return fmt.Errorf("%s: size cannot be zero", description)
	}

	if size > maxSize {
		return fmt.Errorf("%s: size %d exceeds maximum %d", description, size, maxSize)
	}

	return nil
}

// Common buffer size limits.
const (
	// MaxChunkSize limits chunk size to 1GB (reasonable for in-memory processing).
	MaxChunkSize = 1024 * 1024 * 1024 // 1GB

	// MaxAttributeSize limits attribute size to 64MB.
	MaxAttributeSize = 64 * 1024 * 1024 // 64MB

	// MaxStringSize limits string size to 16MB.
	MaxStringSize = 16 * 1024 * 1024 // 16MB

	// MaxHyperslabElements limits hyperslab selection to 1 billion elements.
	MaxHyperslabElements = 1_000_000_000
)

// ValidateHyperslabBounds validates hyperslab selection bounds.
// Checks that start + (count-1)*stride + block does not exceed dataset dimensions.
func ValidateHyperslabBounds(start, count, stride, dims []uint64) error {
	if len(start) != len(dims) || len(count) != len(dims) || len(stride) != len(dims) {
		return fmt.Errorf("hyperslab dimension mismatch: start=%d, count=%d, stride=%d, dims=%d",
			len(start), len(count), len(stride), len(dims))
	}

	for i := range start {
		// Check if selection exceeds dataset bounds.
		// CVE-2025-44905 fix: Check for overflow in stride multiplication.
		if count[i] == 0 {
			return fmt.Errorf("hyperslab count must be > 0 at dimension %d", i)
		}

		maxIndex, err := SafeMultiply(count[i]-1, stride[i])
		if err != nil {
			return fmt.Errorf("hyperslab stride overflow at dimension %d: %w", i, err)
		}

		endIndex := start[i] + maxIndex
		if endIndex >= dims[i] {
			return fmt.Errorf("hyperslab selection exceeds dataset bounds at dimension %d: start=%d, count=%d, stride=%d, dim_size=%d",
				i, start[i], count[i], stride[i], dims[i])
		}
	}

	return nil
}

// CalculateHyperslabElements calculates total elements in hyperslab with overflow checking.
// Total elements = product(count[i]) for all dimensions.
func CalculateHyperslabElements(count []uint64) (uint64, error) {
	if len(count) == 0 {
		return 0, fmt.Errorf("empty hyperslab count")
	}

	total := uint64(1)
	for i, c := range count {
		if c == 0 {
			return 0, fmt.Errorf("zero count at dimension %d", i)
		}

		// CVE-2025-44905 fix: Check for multiplication overflow.
		if err := CheckMultiplyOverflow(total, c); err != nil {
			return 0, fmt.Errorf("hyperslab element overflow at dimension %d: %w", i, err)
		}

		total *= c
	}

	// Validate total doesn't exceed limit.
	if err := ValidateBufferSize(total, MaxHyperslabElements, "hyperslab selection"); err != nil {
		return 0, err
	}

	return total, nil
}
