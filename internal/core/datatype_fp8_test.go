package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFP8E4M3_SpecialValues tests FP8 E4M3 special values (zero, infinity, NaN).
func TestFP8E4M3_SpecialValues(t *testing.T) {
	tests := []struct {
		name     string
		fp8      FP8E4M3
		expected float32
		checkInf bool // If true, check infinity instead of exact value.
	}{
		{"Zero", FP8E4M3(0x00), 0.0, false},
		//nolint:staticcheck // SA4026: Testing negative zero representation.
		{"NegativeZero", FP8E4M3(0x80), -0.0, false},
		{"One", FP8E4M3(0x38), 1.0, false},
		{"NegativeOne", FP8E4M3(0xB8), -1.0, false},
		{"PositiveInfinity", FP8E4M3(0x7F), float32(math.Inf(1)), true},
		{"NegativeInfinity", FP8E4M3(0xFF), float32(math.Inf(-1)), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.fp8.ToFloat32()
			if tc.checkInf {
				if math.IsInf(float64(tc.expected), 1) {
					assert.True(t, math.IsInf(float64(result), 1), "expected +Inf")
				} else if math.IsInf(float64(tc.expected), -1) {
					assert.True(t, math.IsInf(float64(result), -1), "expected -Inf")
				}
			} else {
				assert.InDelta(t, tc.expected, result, 0.01, "fp8=%02x", tc.fp8)
			}
		})
	}
}

// TestFP8E4M3_NaN tests FP8 E4M3 NaN handling.
func TestFP8E4M3_NaN(t *testing.T) {
	// Any exponent=15 with mantissa != 7 is NaN.
	fp8 := FP8E4M3(0x7E) // exp=15, mantissa=6.
	result := fp8.ToFloat32()
	assert.True(t, math.IsNaN(float64(result)), "expected NaN for fp8=%02x", fp8)
}

// TestFP8E4M3_RoundTrip tests FP8 E4M3 round-trip conversion.
func TestFP8E4M3_RoundTrip(t *testing.T) {
	values := []float32{0.0, 1.0, -1.0, 0.5, -0.5, 2.0, 4.0, 8.0, 16.0, 32.0, 64.0, 100.0, 200.0}

	for _, v := range values {
		fp8 := Float32ToFP8E4M3(v)
		result := fp8.ToFloat32()

		// Allow 10% error due to low precision (1 decimal digit).
		// Note: values > 448 will overflow to infinity.
		if v != 0 {
			if !math.IsInf(float64(result), 0) {
				relativeError := math.Abs(float64(result-v)) / math.Abs(float64(v))
				assert.Less(t, relativeError, 0.10, "value=%f, fp8=%02x, result=%f", v, fp8, result)
			}
		} else {
			assert.Equal(t, float32(0.0), result)
		}
	}
}

// TestFP8E4M3_SpecialConversions tests FP8 E4M3 conversions of special values.
func TestFP8E4M3_SpecialConversions(t *testing.T) {
	tests := []struct {
		name     string
		input    float32
		expected FP8E4M3
	}{
		{"Zero", 0.0, FP8E4M3(0x00)},
		{"NegativeZero", float32(math.Copysign(0, -1)), FP8E4M3(0x80)},
		{"PositiveInfinity", float32(math.Inf(1)), FP8E4M3(0x7F)},
		{"NegativeInfinity", float32(math.Inf(-1)), FP8E4M3(0xFF)},
		{"NaN", float32(math.NaN()), FP8E4M3(0x7F)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Float32ToFP8E4M3(tc.input)
			assert.Equal(t, tc.expected, result, "input=%f", tc.input)
		})
	}
}

// TestFP8E4M3_Overflow tests FP8 E4M3 overflow to infinity.
func TestFP8E4M3_Overflow(t *testing.T) {
	// Max normal value is ~448 (exp=8, mantissa=7).
	// Values larger should overflow to infinity.
	largeValue := float32(1000.0)
	fp8 := Float32ToFP8E4M3(largeValue)
	result := fp8.ToFloat32()
	assert.True(t, math.IsInf(float64(result), 1), "expected +Inf for large value")

	largeNegative := float32(-1000.0)
	fp8 = Float32ToFP8E4M3(largeNegative)
	result = fp8.ToFloat32()
	assert.True(t, math.IsInf(float64(result), -1), "expected -Inf for large negative value")
}

// TestFP8E4M3_Underflow tests FP8 E4M3 underflow to zero.
func TestFP8E4M3_Underflow(t *testing.T) {
	// Subnormal range: ~0.001 to ~0.016.
	// Smaller values should underflow to zero.
	tinyValue := float32(0.0001)
	fp8 := Float32ToFP8E4M3(tinyValue)
	result := fp8.ToFloat32()
	assert.Equal(t, float32(0.0), result, "expected underflow to zero")
}

// TestFP8E5M2_SpecialValues tests FP8 E5M2 special values (zero, infinity, NaN).
func TestFP8E5M2_SpecialValues(t *testing.T) {
	tests := []struct {
		name     string
		fp8      FP8E5M2
		expected float32
		checkInf bool // If true, check infinity instead of exact value.
	}{
		{"Zero", FP8E5M2(0x00), 0.0, false},
		//nolint:staticcheck // SA4026: Testing negative zero representation.
		{"NegativeZero", FP8E5M2(0x80), -0.0, false},
		{"One", FP8E5M2(0x3C), 1.0, false},          // exp=15 (biased), mant=0.
		{"NegativeOne", FP8E5M2(0xBC), -1.0, false}, // sign=1, exp=15, mant=0.
		{"PositiveInfinity", FP8E5M2(0x7F), float32(math.Inf(1)), true},
		{"NegativeInfinity", FP8E5M2(0xFF), float32(math.Inf(-1)), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.fp8.ToFloat32()
			if tc.checkInf {
				if math.IsInf(float64(tc.expected), 1) {
					assert.True(t, math.IsInf(float64(result), 1), "expected +Inf")
				} else if math.IsInf(float64(tc.expected), -1) {
					assert.True(t, math.IsInf(float64(result), -1), "expected -Inf")
				}
			} else {
				assert.InDelta(t, tc.expected, result, 0.01, "fp8=%02x", tc.fp8)
			}
		})
	}
}

// TestFP8E5M2_NaN tests FP8 E5M2 NaN handling.
func TestFP8E5M2_NaN(t *testing.T) {
	// Any exponent=31 with mantissa != 3 is NaN.
	fp8 := FP8E5M2(0x7E) // exp=31, mantissa=2.
	result := fp8.ToFloat32()
	assert.True(t, math.IsNaN(float64(result)), "expected NaN for fp8=%02x", fp8)
}

// TestFP8E5M2_RoundTrip tests FP8 E5M2 round-trip conversion.
func TestFP8E5M2_RoundTrip(t *testing.T) {
	values := []float32{0.0, 1.0, -1.0, 0.5, -0.5, 2.0, 4.0, 8.0, 16.0, 32.0, 64.0, 128.0, 256.0, 512.0, 1024.0, 10000.0, 50000.0}

	for _, v := range values {
		fp8 := Float32ToFP8E5M2(v)
		result := fp8.ToFloat32()

		// Allow 15% error due to very low precision (2-bit mantissa).
		if v != 0 {
			relativeError := math.Abs(float64(result-v)) / math.Abs(float64(v))
			assert.Less(t, relativeError, 0.15, "value=%f, fp8=%02x, result=%f", v, fp8, result)
		} else {
			assert.Equal(t, float32(0.0), result)
		}
	}
}

// TestFP8E5M2_SpecialConversions tests FP8 E5M2 conversions of special values.
func TestFP8E5M2_SpecialConversions(t *testing.T) {
	tests := []struct {
		name     string
		input    float32
		expected FP8E5M2
	}{
		{"Zero", 0.0, FP8E5M2(0x00)},
		{"NegativeZero", float32(math.Copysign(0, -1)), FP8E5M2(0x80)},
		{"PositiveInfinity", float32(math.Inf(1)), FP8E5M2(0x7F)},
		{"NegativeInfinity", float32(math.Inf(-1)), FP8E5M2(0xFF)},
		{"NaN", float32(math.NaN()), FP8E5M2(0x7F)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Float32ToFP8E5M2(tc.input)
			assert.Equal(t, tc.expected, result, "input=%f", tc.input)
		})
	}
}

// TestFP8E5M2_Overflow tests FP8 E5M2 overflow to infinity.
func TestFP8E5M2_Overflow(t *testing.T) {
	// Max normal value is ~114688 (exp=16, mantissa=1.75 = binary 1.11).
	// Values larger should overflow to infinity.
	largeValue := float32(200000.0)
	fp8 := Float32ToFP8E5M2(largeValue)
	result := fp8.ToFloat32()
	assert.True(t, math.IsInf(float64(result), 1), "expected +Inf for large value")

	largeNegative := float32(-200000.0)
	fp8 = Float32ToFP8E5M2(largeNegative)
	result = fp8.ToFloat32()
	assert.True(t, math.IsInf(float64(result), -1), "expected -Inf for large negative value")
}

// TestFP8E5M2_Underflow tests FP8 E5M2 underflow to zero.
func TestFP8E5M2_Underflow(t *testing.T) {
	// Min subnormal value is ~0.000015 (exp=0, mant=1).
	// Smaller values should underflow to zero.
	tinyValue := float32(0.000001)
	fp8 := Float32ToFP8E5M2(tinyValue)
	result := fp8.ToFloat32()
	assert.Equal(t, float32(0.0), result, "expected underflow to zero")
}

// TestFP8E4M3_Subnormal tests FP8 E4M3 subnormal numbers.
func TestFP8E4M3_Subnormal(t *testing.T) {
	// Subnormal: exp=0, mantissa≠0.
	// Example: mantissa=1 → 0.125 × 2^(-6) = 0.001953125.
	fp8 := FP8E4M3(0x01) // exp=0, mant=1.
	result := fp8.ToFloat32()
	expected := float32(0.001953125)
	assert.InDelta(t, expected, result, 0.0001, "fp8=%02x", fp8)
}

// TestFP8E5M2_Subnormal tests FP8 E5M2 subnormal numbers.
func TestFP8E5M2_Subnormal(t *testing.T) {
	// Subnormal: exp=0, mantissa≠0.
	// Example: mantissa=1 → 0.25 × 2^(-14) = 0.0000152587890625.
	fp8 := FP8E5M2(0x01) // exp=0, mant=1.
	result := fp8.ToFloat32()
	expected := float32(0.0000152587890625)
	assert.InDelta(t, expected, result, 0.000001, "fp8=%02x", fp8)
}
