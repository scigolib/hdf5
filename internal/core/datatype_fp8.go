// Package core provides HDF5 low-level format structures and parsers.
package core

import (
	"math"
)

// FP8E4M3 represents an 8-bit floating point value in E4M3 format.
//
// Format (8 bits total):
//   - Bit 7:    Sign (1 bit)
//   - Bits 6-3: Exponent (4 bits, bias=7)
//   - Bits 2-0: Mantissa (3 bits)
//
// Special values:
//   - Exponent=15, Mantissa=7: Infinity
//   - Exponent=0, Mantissa=0: Zero
//   - Exponent=0, Mantissa≠0: Subnormal (denormalized)
//
// Range: ±448 (max normal value).
// Precision: ~1 decimal digit.
type FP8E4M3 uint8

const (
	fp8E4M3SignMask     = 0x80 // 1000 0000
	fp8E4M3ExponentMask = 0x78 // 0111 1000
	fp8E4M3MantissaMask = 0x07 // 0000 0111
	fp8E4M3ExponentBias = 7
	fp8E4M3MaxExponent  = 15
)

// ToFloat32 converts FP8 E4M3 to float32.
//
//nolint:dupl,nestif // Similar structure to E5M2 but different constants; nestif acceptable for IEEE 754 format parsing.
func (f FP8E4M3) ToFloat32() float32 {
	bits := uint8(f)

	// Extract components.
	sign := (bits & fp8E4M3SignMask) >> 7
	exponent := (bits & fp8E4M3ExponentMask) >> 3
	mantissa := bits & fp8E4M3MantissaMask

	// Handle special cases: infinity and NaN.
	if exponent == fp8E4M3MaxExponent {
		if mantissa == 0x07 {
			// Infinity.
			if sign == 1 {
				return float32(math.Inf(-1))
			}
			return float32(math.Inf(1))
		}
		// NaN (other combinations with exp=15).
		return float32(math.NaN())
	}

	// Handle zero.
	if exponent == 0 {
		if mantissa == 0 {
			// Zero (positive or negative).
			if sign == 1 {
				//nolint:staticcheck // SA4026: Negative zero representation for IEEE 754 compliance.
				return -0.0
			}
			return 0.0
		}
		// Subnormal number: 0.mantissa × 2^(1-bias).
		fraction := float32(mantissa) / 8.0
		value := fraction * float32(math.Pow(2, 1-fp8E4M3ExponentBias))
		if sign == 1 {
			return -value
		}
		return value
	}

	// Normal number: 1.mantissa × 2^(exp-bias).
	// mantissa = 1.xxx (implicit leading 1).
	fraction := 1.0 + float32(mantissa)/8.0
	exponentValue := int(exponent) - fp8E4M3ExponentBias
	value := fraction * float32(math.Pow(2, float64(exponentValue)))

	if sign == 1 {
		return -value
	}
	return value
}

// Float32ToFP8E4M3 converts float32 to FP8 E4M3 with rounding.
//
//nolint:nestif // Nested conditionals acceptable for IEEE 754 format conversion.
func Float32ToFP8E4M3(f float32) FP8E4M3 {
	// Handle special cases: NaN.
	if math.IsNaN(float64(f)) {
		return FP8E4M3(0x7F) // NaN
	}

	// Handle positive infinity.
	if math.IsInf(float64(f), 1) {
		return FP8E4M3(0x7F) // +Inf
	}

	// Handle negative infinity.
	if math.IsInf(float64(f), -1) {
		return FP8E4M3(0xFF) // -Inf
	}

	// Handle zero (positive or negative).
	if f == 0 {
		if math.Signbit(float64(f)) {
			return FP8E4M3(0x80) // -0
		}
		return FP8E4M3(0x00) // +0
	}

	// Extract sign.
	sign := uint8(0)
	if f < 0 {
		sign = 1
		f = -f
	}

	// Find exponent using log2.
	exponent := math.Log2(float64(f))
	exponentInt := int(math.Floor(exponent))

	// Check range - max exponent = 15 - 7 = 8.
	if exponentInt > 8 {
		// Overflow to infinity.
		if sign == 1 {
			return FP8E4M3(0xFF)
		}
		return FP8E4M3(0x7F)
	}

	// Check range - min normal exponent = 1 - 7 = -6 (exponent field = 1).
	if exponentInt < -6 {
		// Subnormal or underflow.
		if exponentInt < -9 {
			// Too small even for subnormal, underflow to zero.
			if sign == 1 {
				return FP8E4M3(0x80)
			}
			return FP8E4M3(0x00)
		}

		// Subnormal: 0.mantissa × 2^(1-7).
		mantissaValue := f / float32(math.Pow(2, 1-fp8E4M3ExponentBias))
		mantissaBits := uint8(math.Round(float64(mantissaValue * 8.0)))

		// Clamp mantissa to 3 bits.
		if mantissaBits > 7 {
			mantissaBits = 7
		}

		return FP8E4M3((sign << 7) | mantissaBits)
	}

	// Normal number.
	//nolint:gosec // G115: Validated range check above ensures no overflow.
	biasedExponent := uint8(exponentInt + fp8E4M3ExponentBias)
	mantissaValue := f / float32(math.Pow(2, float64(exponentInt)))
	mantissaFraction := mantissaValue - 1.0 // Remove implicit leading 1.
	mantissaBits := uint8(math.Round(float64(mantissaFraction * 8.0)))

	// Clamp mantissa to 3 bits.
	if mantissaBits > 7 {
		mantissaBits = 7
	}

	return FP8E4M3((sign << 7) | (biasedExponent << 3) | mantissaBits)
}

// FP8E5M2 represents an 8-bit floating point value in E5M2 format.
//
// Format (8 bits total):
//   - Bit 7:    Sign (1 bit)
//   - Bits 6-2: Exponent (5 bits, bias=15)
//   - Bits 1-0: Mantissa (2 bits)
//
// Special values:
//   - Exponent=31, Mantissa=3: Infinity
//   - Exponent=0, Mantissa=0: Zero
//   - Exponent=0, Mantissa≠0: Subnormal (denormalized)
//
// Range: ±57344 (max normal value).
// Precision: ~1 decimal digit.
type FP8E5M2 uint8

const (
	fp8E5M2SignMask     = 0x80 // 1000 0000
	fp8E5M2ExponentMask = 0x7C // 0111 1100
	fp8E5M2MantissaMask = 0x03 // 0000 0011
	fp8E5M2ExponentBias = 15
	fp8E5M2MaxExponent  = 31
)

// ToFloat32 converts FP8 E5M2 to float32.
//
//nolint:dupl,nestif // Similar structure to E4M3 but different constants; nestif acceptable for IEEE 754 format parsing.
func (f FP8E5M2) ToFloat32() float32 {
	bits := uint8(f)

	// Extract components.
	sign := (bits & fp8E5M2SignMask) >> 7
	exponent := (bits & fp8E5M2ExponentMask) >> 2
	mantissa := bits & fp8E5M2MantissaMask

	// Handle special cases: infinity and NaN.
	if exponent == fp8E5M2MaxExponent {
		if mantissa == 0x03 {
			// Infinity.
			if sign == 1 {
				return float32(math.Inf(-1))
			}
			return float32(math.Inf(1))
		}
		// NaN (other combinations with exp=31).
		return float32(math.NaN())
	}

	// Handle zero.
	if exponent == 0 {
		if mantissa == 0 {
			// Zero (positive or negative).
			if sign == 1 {
				//nolint:staticcheck // SA4026: Negative zero representation for IEEE 754 compliance.
				return -0.0
			}
			return 0.0
		}
		// Subnormal number: 0.mantissa × 2^(1-bias).
		fraction := float32(mantissa) / 4.0
		value := fraction * float32(math.Pow(2, 1-fp8E5M2ExponentBias))
		if sign == 1 {
			return -value
		}
		return value
	}

	// Normal number: 1.mantissa × 2^(exp-bias).
	// mantissa = 1.xx (implicit leading 1).
	fraction := 1.0 + float32(mantissa)/4.0
	exponentValue := int(exponent) - fp8E5M2ExponentBias
	value := fraction * float32(math.Pow(2, float64(exponentValue)))

	if sign == 1 {
		return -value
	}
	return value
}

// Float32ToFP8E5M2 converts float32 to FP8 E5M2 with rounding.
//
//nolint:nestif // Nested conditionals acceptable for IEEE 754 format conversion.
func Float32ToFP8E5M2(f float32) FP8E5M2 {
	// Handle special cases: NaN.
	if math.IsNaN(float64(f)) {
		return FP8E5M2(0x7F) // NaN
	}

	// Handle positive infinity.
	if math.IsInf(float64(f), 1) {
		return FP8E5M2(0x7F) // +Inf
	}

	// Handle negative infinity.
	if math.IsInf(float64(f), -1) {
		return FP8E5M2(0xFF) // -Inf
	}

	// Handle zero (positive or negative).
	if f == 0 {
		if math.Signbit(float64(f)) {
			return FP8E5M2(0x80) // -0
		}
		return FP8E5M2(0x00) // +0
	}

	// Extract sign.
	sign := uint8(0)
	if f < 0 {
		sign = 1
		f = -f
	}

	// Find exponent using log2.
	exponent := math.Log2(float64(f))
	exponentInt := int(math.Floor(exponent))

	// Check range - max exponent = 31 - 15 = 16.
	if exponentInt > 16 {
		// Overflow to infinity.
		if sign == 1 {
			return FP8E5M2(0xFF)
		}
		return FP8E5M2(0x7F)
	}

	// Check range - min normal exponent = 1 - 15 = -14 (exponent field = 1).
	if exponentInt < -14 {
		// Subnormal or underflow.
		if exponentInt < -17 {
			// Too small even for subnormal, underflow to zero.
			if sign == 1 {
				return FP8E5M2(0x80)
			}
			return FP8E5M2(0x00)
		}

		// Subnormal: 0.mantissa × 2^(1-15).
		mantissaValue := f / float32(math.Pow(2, 1-fp8E5M2ExponentBias))
		mantissaBits := uint8(math.Round(float64(mantissaValue * 4.0)))

		// Clamp mantissa to 2 bits.
		if mantissaBits > 3 {
			mantissaBits = 3
		}

		return FP8E5M2((sign << 7) | mantissaBits)
	}

	// Normal number.
	//nolint:gosec // G115: Validated range check above ensures no overflow.
	biasedExponent := uint8(exponentInt + fp8E5M2ExponentBias)
	mantissaValue := f / float32(math.Pow(2, float64(exponentInt)))
	mantissaFraction := mantissaValue - 1.0 // Remove implicit leading 1.
	mantissaBits := uint8(math.Round(float64(mantissaFraction * 4.0)))

	// Clamp mantissa to 2 bits.
	if mantissaBits > 3 {
		mantissaBits = 3
	}

	return FP8E5M2((sign << 7) | (biasedExponent << 2) | mantissaBits)
}
