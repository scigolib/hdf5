package writer

import (
	"encoding/binary"
	"fmt"
)

// Fletcher32Filter implements Fletcher32 checksum (FilterID = 3).
//
// The Fletcher32 filter adds a 4-byte checksum to the end of data to detect
// corruption during storage or transmission. It uses the Fletcher32 algorithm,
// which is faster than CRC32 but less robust against intentional tampering.
//
// The filter is commonly used in HDF5 to ensure data integrity, especially
// for compressed data where corruption could affect decompression.
//
// On write: checksum is calculated and appended (original_data + 4 bytes).
// On read: checksum is verified and stripped (returns original_data).
type Fletcher32Filter struct{}

// NewFletcher32Filter creates a Fletcher32 checksum filter.
func NewFletcher32Filter() *Fletcher32Filter {
	return &Fletcher32Filter{}
}

// ID returns the HDF5 filter identifier for Fletcher32.
func (f *Fletcher32Filter) ID() FilterID {
	return FilterFletcher32
}

// Name returns the HDF5 filter name.
func (f *Fletcher32Filter) Name() string {
	return "fletcher32"
}

// Apply calculates Fletcher32 checksum and appends it to the data.
//
// The returned data is 4 bytes longer than the input, with the checksum
// stored in little-endian format at the end.
func (f *Fletcher32Filter) Apply(data []byte) ([]byte, error) {
	checksum := calculateFletcher32(data)

	// Append 4-byte checksum (little-endian)
	result := make([]byte, len(data)+4)
	copy(result, data)
	binary.LittleEndian.PutUint32(result[len(data):], checksum)

	return result, nil
}

// Remove verifies and strips the Fletcher32 checksum.
//
// This method:
//  1. Extracts the 4-byte checksum from the end of data
//  2. Calculates the checksum of the original data
//  3. Verifies they match
//  4. Returns the original data without the checksum
//
// Returns an error if the checksum doesn't match (data corruption detected).
func (f *Fletcher32Filter) Remove(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("data too short for fletcher32: %d bytes", len(data))
	}

	// Extract checksum (last 4 bytes)
	dataLen := len(data) - 4
	originalData := data[:dataLen]
	storedChecksum := binary.LittleEndian.Uint32(data[dataLen:])

	// Verify checksum
	calculatedChecksum := calculateFletcher32(originalData)
	if calculatedChecksum != storedChecksum {
		return nil, fmt.Errorf("fletcher32 checksum mismatch: stored=%08x, calculated=%08x",
			storedChecksum, calculatedChecksum)
	}

	return originalData, nil
}

// Encode returns the filter parameters for the Pipeline message.
//
// Fletcher32 has no parameters, so this returns empty values.
func (f *Fletcher32Filter) Encode() (flags uint16, cdValues []uint32) {
	return 0, []uint32{}
}

// calculateFletcher32 calculates the Fletcher32 checksum.
//
// Fletcher32 algorithm:
//  1. Process data in 16-bit words (little-endian)
//  2. Maintain two 16-bit sums (sum1 and sum2)
//  3. sum1 = (sum1 + word) mod 65535
//  4. sum2 = (sum2 + sum1) mod 65535
//  5. checksum = (sum2 << 16) | sum1
//
// For odd-length data, the last byte is treated as a 16-bit word with
// high byte = 0.
func calculateFletcher32(data []byte) uint32 {
	var sum1, sum2 uint32

	// Process data in 16-bit words
	i := 0
	for i+1 < len(data) {
		// Read 16-bit word (little-endian)
		word := uint32(data[i]) | uint32(data[i+1])<<8
		sum1 = (sum1 + word) % 65535
		sum2 = (sum2 + sum1) % 65535
		i += 2
	}

	// Handle last byte if data length is odd
	if i < len(data) {
		word := uint32(data[i])
		sum1 = (sum1 + word) % 65535
		sum2 = (sum2 + sum1) % 65535
	}

	return (sum2 << 16) | sum1
}
