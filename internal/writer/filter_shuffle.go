package writer

import "fmt"

// ShuffleFilter implements byte shuffle (FilterID = 2).
//
// The shuffle filter reorders bytes in the data to improve compression ratios
// for numeric data. It works by transposing byte order from element-by-element
// to byte-by-byte.
//
// For example, with 4-byte integers [A1 A2 A3 A4][B1 B2 B3 B4][C1 C2 C3 C4]:
//
//	Original: [A1 A2 A3 A4 B1 B2 B3 B4 C1 C2 C3 C4]
//	Shuffled: [A1 B1 C1 A2 B2 C2 A3 B3 C3 A4 B4 C4]
//
// This transformation groups similar bytes together (all first bytes, then all
// second bytes, etc.), which typically compresses much better with algorithms
// like GZIP.
//
// The shuffle filter is especially effective for:
//   - Integer arrays with slowly changing values
//   - Floating-point arrays with similar magnitudes
//   - Multi-dimensional arrays with spatial locality
//
// Note: Shuffle should always be applied BEFORE compression filters like GZIP.
type ShuffleFilter struct {
	elementSize uint32 // Size of each element in bytes
}

// NewShuffleFilter creates a shuffle filter with the specified element size.
//
// The element size should match the datatype size:
//   - int32, float32: elementSize = 4
//   - int64, float64: elementSize = 8
//   - int16: elementSize = 2
//   - int8: elementSize = 1
//
// For compound or array types, use the size of the base element.
func NewShuffleFilter(elementSize uint32) *ShuffleFilter {
	return &ShuffleFilter{elementSize: elementSize}
}

// ID returns the HDF5 filter identifier for shuffle.
func (f *ShuffleFilter) ID() FilterID {
	return FilterShuffle
}

// Name returns the HDF5 filter name.
func (f *ShuffleFilter) Name() string {
	return "shuffle"
}

// Apply performs byte shuffle on the data.
//
// The shuffle algorithm:
//  1. Divide data into elements of size elementSize
//  2. For each byte position in an element (0 to elementSize-1):
//     a. Extract that byte from each element
//     b. Write all those bytes consecutively
//
// Example with elementSize=4, 3 elements:
//
//	Input:  [a1 a2 a3 a4][b1 b2 b3 b4][c1 c2 c3 c4]
//	Output: [a1 b1 c1][a2 b2 c2][a3 b3 c3][a4 b4 c4]
//
// This groups similar bytes together, improving compression with GZIP.
func (f *ShuffleFilter) Apply(data []byte) ([]byte, error) {
	dataLen := uint32(len(data)) //nolint:gosec // G115: Data length validated by HDF5, fits in uint32
	if dataLen == 0 {
		return data, nil
	}

	if dataLen%f.elementSize != 0 {
		return nil, fmt.Errorf("data length %d not multiple of element size %d", dataLen, f.elementSize)
	}

	numElements := dataLen / f.elementSize
	shuffled := make([]byte, dataLen)

	// Shuffle: reorder bytes so all byte[0] together, then all byte[1], etc.
	for byteIndex := uint32(0); byteIndex < f.elementSize; byteIndex++ {
		for elemIndex := uint32(0); elemIndex < numElements; elemIndex++ {
			srcIndex := elemIndex*f.elementSize + byteIndex
			dstIndex := byteIndex*numElements + elemIndex
			shuffled[dstIndex] = data[srcIndex]
		}
	}

	return shuffled, nil
}

// Remove reverses the byte shuffle (unshuffle).
//
// This operation reverses Apply, restoring the original byte order.
//
// Example with elementSize=4, 3 elements:
//
//	Input:  [a1 b1 c1][a2 b2 c2][a3 b3 c3][a4 b4 c4]
//	Output: [a1 a2 a3 a4][b1 b2 b3 b4][c1 c2 c3 c4]
func (f *ShuffleFilter) Remove(data []byte) ([]byte, error) {
	dataLen := uint32(len(data)) //nolint:gosec // G115: Data length validated by HDF5, fits in uint32
	if dataLen == 0 {
		return data, nil
	}

	if dataLen%f.elementSize != 0 {
		return nil, fmt.Errorf("data length %d not multiple of element size %d", dataLen, f.elementSize)
	}

	numElements := dataLen / f.elementSize
	unshuffled := make([]byte, dataLen)

	// Unshuffle: reverse the shuffle operation
	for byteIndex := uint32(0); byteIndex < f.elementSize; byteIndex++ {
		for elemIndex := uint32(0); elemIndex < numElements; elemIndex++ {
			srcIndex := byteIndex*numElements + elemIndex
			dstIndex := elemIndex*f.elementSize + byteIndex
			unshuffled[dstIndex] = data[srcIndex]
		}
	}

	return unshuffled, nil
}

// Encode returns the filter parameters for the Pipeline message.
//
// For shuffle, the client data contains a single value: the element size.
// Flags are always 0 for shuffle.
func (f *ShuffleFilter) Encode() (flags uint16, cdValues []uint32) {
	return 0, []uint32{f.elementSize}
}
