// Package core provides HDF5 low-level format structures and parsers.
package core

import (
	"encoding/binary"
	"math"
)

// BFloat16 represents a 16-bit brain floating point value.
//
// Format (16 bits total):
//   - Bit 15:     Sign (1 bit)
//   - Bits 14-7:  Exponent (8 bits, bias=127) - SAME as float32
//   - Bits 6-0:   Mantissa (7 bits) - truncated from float32's 23 bits
//
// Key property: bfloat16 is just the upper 16 bits of float32.
// This makes conversion extremely fast (just bit shifting).
//
// Range: ±3.4e38 (same as float32)
// Precision: ~2 decimal digits (vs 7 for float32)
//
// Used by: Google TPU, NVIDIA Tensor Cores, Intel AMX.
type BFloat16 uint16

// ToFloat32 converts bfloat16 to float32 (fast operation).
//
// Since bfloat16 is just the upper 16 bits of float32,
// we simply shift left by 16 bits to restore the full float32.
func (b BFloat16) ToFloat32() float32 {
	// bfloat16 is upper 16 bits of float32.
	// Shift left by 16 bits to restore float32.
	bits := uint32(b) << 16
	return math.Float32frombits(bits)
}

// Float32ToBFloat16 converts float32 to bfloat16 with rounding to nearest even.
//
// Rounding mode: Round to nearest, ties to even (banker's rounding).
// This provides better accuracy than simple truncation.
func Float32ToBFloat16(f float32) BFloat16 {
	// Get float32 bits.
	bits := math.Float32bits(f)

	// Round to nearest even (optional but recommended for accuracy).
	// Check bit 15 (first truncated bit).
	if (bits & 0x8000) != 0 {
		// Check if tie (bits 14-0 all zero).
		if (bits & 0x7FFF) != 0 {
			// Not a tie, round up.
			bits += 0x8000
		} else if (bits & 0x10000) != 0 {
			// Tie - round to even (check bit 16).
			bits += 0x8000
		}
	}

	// Take upper 16 bits.
	//nolint:gosec // G115: Validated range, intentional truncation to uint16.
	return BFloat16(bits >> 16)
}

// Encode encodes bfloat16 to bytes (little-endian).
func (b BFloat16) Encode() []byte {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uint16(b))
	return buf
}

// DecodeBFloat16 decodes bytes to bfloat16 (little-endian).
func DecodeBFloat16(data []byte) BFloat16 {
	return BFloat16(binary.LittleEndian.Uint16(data))
}
