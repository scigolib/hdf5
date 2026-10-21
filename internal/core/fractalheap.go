package core

import (
	"errors"
	"fmt"
)

// FractalHeapOffsetSize validates the maximum heap size stored in a fractal
// heap header and returns the width in bytes of a heap offset.
//
// Both fractal heap header parsers need this value: the structures package
// (heap reads) and the attribute reader below (dense attribute storage).
//
// Reference: H5HF__hdr_finish_init_phase1 (H5HFhdr.c:194) computes
// H5HF_SIZEOF_OFFSET_BITS(max_index) = (max_index + 7) / 8 from the header's
// maximum-size value, and the header checks require 0 < max_index <=
// 8 * sizeof_size (H5HFhdr.c:335,344).
//
// The field is a uint16 read straight from the file, so the addition is done
// in a wider type: in uint16 arithmetic 65535+7 wraps to 6 and a corrupt
// header would silently get a zero-width offset instead of an error.
func FractalHeapOffsetSize(maxHeapSizeBits uint16, lengthSize uint8) (uint8, error) {
	if maxHeapSizeBits == 0 {
		return 0, errors.New("fractal heap max heap size is zero")
	}

	limit := uint32(lengthSize) * 8
	if uint32(maxHeapSizeBits) > limit {
		return 0, fmt.Errorf("fractal heap max heap size %d bits exceeds the %d-bit limit of the file's length field",
			maxHeapSizeBits, limit)
	}

	// At most 8 bytes: the check above caps the bit count at 8 * lengthSize.
	//nolint:gosec // G115: conversion is bounded by the validation above
	return uint8((uint32(maxHeapSizeBits) + 7) / 8), nil
}
