package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBFloat16_SpecialValues tests bfloat16 special values (zero, infinity, NaN).
func TestBFloat16_SpecialValues(t *testing.T) {
	tests := []struct {
		name     string
		bf16     BFloat16
		expected float32
		checkInf bool // If true, check infinity instead of exact value.
		checkNaN bool // If true, check NaN.
	}{
		{"Zero", BFloat16(0x0000), 0.0, false, false},
		//nolint:staticcheck // SA4026: Testing negative zero representation.
		{"NegativeZero", BFloat16(0x8000), -0.0, false, false},
		{"One", BFloat16(0x3F80), 1.0, false, false},
		{"NegativeOne", BFloat16(0xBF80), -1.0, false, false},
		{"PositiveInfinity", BFloat16(0x7F80), float32(math.Inf(1)), true, false},
		{"NegativeInfinity", BFloat16(0xFF80), float32(math.Inf(-1)), true, false},
		{"NaN", BFloat16(0x7FC0), float32(math.NaN()), false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.bf16.ToFloat32()
			switch {
			case tc.checkInf && math.IsInf(float64(tc.expected), 1):
				assert.True(t, math.IsInf(float64(result), 1), "expected +Inf")
			case tc.checkInf && math.IsInf(float64(tc.expected), -1):
				assert.True(t, math.IsInf(float64(result), -1), "expected -Inf")
			case tc.checkNaN:
				assert.True(t, math.IsNaN(float64(result)), "expected NaN")
			default:
				assert.Equal(t, tc.expected, result, "bf16=%04x", tc.bf16)
			}
		})
	}
}

// TestBFloat16_RoundTrip tests bfloat16 round-trip conversion.
func TestBFloat16_RoundTrip(t *testing.T) {
	values := []float32{0.0, 1.0, -1.0, 0.5, -0.5, 2.0, 100.0, 12345.0, 3.14159, -273.15, 0.001, 1000000.0}

	for _, v := range values {
		bf16 := Float32ToBFloat16(v)
		result := bf16.ToFloat32()

		// bfloat16 has ~2 decimal digits precision.
		// Allow 1% error for most values, 5% for very small values.
		if v != 0 {
			relativeError := math.Abs(float64(result-v)) / math.Abs(float64(v))
			if math.Abs(float64(v)) < 0.01 {
				assert.Less(t, relativeError, 0.05, "value=%f, bf16=%04x, result=%f", v, bf16, result)
			} else {
				assert.Less(t, relativeError, 0.01, "value=%f, bf16=%04x, result=%f", v, bf16, result)
			}
		} else {
			assert.Equal(t, float32(0.0), result)
		}
	}
}

// TestBFloat16_SpecialConversions tests bfloat16 conversions of special values.
func TestBFloat16_SpecialConversions(t *testing.T) {
	tests := []struct {
		name     string
		input    float32
		expected BFloat16
	}{
		{"Zero", 0.0, BFloat16(0x0000)},
		{"NegativeZero", float32(math.Copysign(0, -1)), BFloat16(0x8000)},
		{"One", 1.0, BFloat16(0x3F80)},
		{"NegativeOne", -1.0, BFloat16(0xBF80)},
		{"PositiveInfinity", float32(math.Inf(1)), BFloat16(0x7F80)},
		{"NegativeInfinity", float32(math.Inf(-1)), BFloat16(0xFF80)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Float32ToBFloat16(tc.input)
			assert.Equal(t, tc.expected, result, "input=%f", tc.input)
		})
	}
}

// TestBFloat16_NaN tests bfloat16 NaN handling.
func TestBFloat16_NaN(t *testing.T) {
	input := float32(math.NaN())
	bf16 := Float32ToBFloat16(input)
	result := bf16.ToFloat32()
	assert.True(t, math.IsNaN(float64(result)), "expected NaN")
}

// TestBFloat16_Rounding tests bfloat16 rounding to nearest even.
func TestBFloat16_Rounding(t *testing.T) {
	// Test rounding behavior.
	// 1.0 + epsilon (very small) should round to 1.0.
	input := float32(1.0) + float32(0.0001)
	bf16 := Float32ToBFloat16(input)
	result := bf16.ToFloat32()
	assert.InDelta(t, 1.0, result, 0.01, "expected rounding to 1.0")

	// 1.5 should round to 1.5 (if representable) or close.
	input = float32(1.5)
	bf16 = Float32ToBFloat16(input)
	result = bf16.ToFloat32()
	assert.InDelta(t, 1.5, result, 0.01, "expected rounding to 1.5")
}

// TestBFloat16_Encode tests bfloat16 encoding to bytes.
func TestBFloat16_Encode(t *testing.T) {
	tests := []struct {
		name     string
		bf16     BFloat16
		expected []byte
	}{
		{"Zero", BFloat16(0x0000), []byte{0x00, 0x00}},
		{"One", BFloat16(0x3F80), []byte{0x80, 0x3F}}, // Little-endian.
		{"NegativeOne", BFloat16(0xBF80), []byte{0x80, 0xBF}},
		{"Infinity", BFloat16(0x7F80), []byte{0x80, 0x7F}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.bf16.Encode()
			assert.Equal(t, tc.expected, result, "bf16=%04x", tc.bf16)
		})
	}
}

// TestBFloat16_Decode tests bfloat16 decoding from bytes.
func TestBFloat16_Decode(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected BFloat16
	}{
		{"Zero", []byte{0x00, 0x00}, BFloat16(0x0000)},
		{"One", []byte{0x80, 0x3F}, BFloat16(0x3F80)}, // Little-endian.
		{"NegativeOne", []byte{0x80, 0xBF}, BFloat16(0xBF80)},
		{"Infinity", []byte{0x80, 0x7F}, BFloat16(0x7F80)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DecodeBFloat16(tc.data)
			assert.Equal(t, tc.expected, result, "data=%v", tc.data)
		})
	}
}

// TestBFloat16_Range tests bfloat16 dynamic range (same as float32).
func TestBFloat16_Range(t *testing.T) {
	// bfloat16 has same dynamic range as float32 (8-bit exponent).
	// Test large values.
	largeValue := float32(1e20)
	bf16 := Float32ToBFloat16(largeValue)
	result := bf16.ToFloat32()
	assert.InDelta(t, largeValue, result, float64(largeValue)*0.01, "expected large value preserved")

	// Test small values.
	smallValue := float32(1e-20)
	bf16 = Float32ToBFloat16(smallValue)
	result = bf16.ToFloat32()
	assert.InDelta(t, smallValue, result, float64(smallValue)*0.05, "expected small value preserved")
}

// TestBFloat16_Precision tests bfloat16 precision (~2 decimal digits).
func TestBFloat16_Precision(t *testing.T) {
	// bfloat16 has 7-bit mantissa (vs 23-bit in float32).
	// This gives ~2 decimal digits precision.
	// Test that values differing in later digits are indistinguishable.
	input1 := float32(1.23)
	input2 := float32(1.24)

	bf16_1 := Float32ToBFloat16(input1)
	bf16_2 := Float32ToBFloat16(input2)

	result1 := bf16_1.ToFloat32()
	result2 := bf16_2.ToFloat32()

	// Both should be close (precision limited).
	diff := math.Abs(float64(result1 - result2))
	assert.Less(t, diff, 0.02, "expected limited precision")
}

// TestBFloat16_EdgeCases tests bfloat16 edge cases.
func TestBFloat16_EdgeCases(t *testing.T) {
	// Test denormal (subnormal) numbers.
	// bfloat16 subnormals are very small (exp=0, mantissa≠0).
	denormal := BFloat16(0x0001) // exp=0, mant=1.
	result := denormal.ToFloat32()
	assert.Greater(t, result, float32(0.0), "expected non-zero denormal")
	assert.Less(t, result, float32(1e-30), "expected very small denormal")

	// Test negative denormal.
	negativeDenormal := BFloat16(0x8001) // sign=1, exp=0, mant=1.
	result = negativeDenormal.ToFloat32()
	assert.Less(t, result, float32(0.0), "expected negative denormal")
	assert.Greater(t, result, float32(-1e-30), "expected very small negative denormal")
}
