package writer

import (
	"encoding/binary"
	"fmt"

	"github.com/scigolib/hdf5/internal/utils"
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
// stored little-endian at the end (UINT32ENCODE, H5Zfletcher32.c:126).
func (f *Fletcher32Filter) Apply(data []byte) ([]byte, error) {
	checksum := calculateFletcher32(data)

	// Append 4-byte checksum (little-endian)
	result := make([]byte, len(data)+4)
	copy(result, data)
	binary.LittleEndian.PutUint32(result[len(data):], checksum)

	return result, nil
}

// Remove verifies and strips the Fletcher32 checksum the way
// H5Z__filter_fletcher32 does on read (H5Zfletcher32.c:64-104): the last 4
// bytes are the little-endian checksum of the bytes before them. Like C it
// also accepts the pre-1.6.3 checksum, whose bytes are swapped within each
// 16-bit half (H5Zfletcher32.c:79-98), so chunks written by HDF5 1.6.2 and
// earlier still read. A short chunk or a mismatch wraps utils.ErrCorrupt.
func (f *Fletcher32Filter) Remove(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: data too short for fletcher32: %d bytes", utils.ErrCorrupt, len(data))
	}

	dataLen := len(data) - 4
	originalData := data[:dataLen]
	stored := binary.LittleEndian.Uint32(data[dataLen:])

	sum := calculateFletcher32(originalData)
	reversed := (sum&0x00FF00FF)<<8 | (sum&0xFF00FF00)>>8
	if stored != sum && stored != reversed {
		return nil, fmt.Errorf("%w: fletcher32 checksum mismatch: stored=%08x, calculated=%08x",
			utils.ErrCorrupt, stored, sum)
	}

	return originalData, nil
}

// Encode returns the filter parameters for the Pipeline message.
//
// Fletcher32 has no parameters. It is mandatory, as H5Pset_fletcher32 adds it
// (H5Pocpl.c:1104): a chunk whose checksum cannot be computed is an error,
// never stored unchecked.
func (f *Fletcher32Filter) Encode() (flags uint16, cdValues []uint32) {
	return FilterFlagMandatory, []uint32{}
}

// calculateFletcher32 is H5_checksum_fletcher32 (H5checksum.c:94-136), the
// same algorithm as the reader's core.fletcher32: 16-bit words read
// big-endian, an odd last byte as the high half of a word, both sums folded
// to 16 bits after every 360 words (the most that cannot overflow) and once
// more at the end. Folding keeps 0xFFFF where a modulo 65535 would give 0, so
// the result matches C bit for bit; C and our reader reject any other value.
func calculateFletcher32(data []byte) uint32 {
	var sum1, sum2 uint32

	p := data
	for words := len(data) / 2; words > 0; {
		block := min(words, 360)
		words -= block
		for ; block > 0; block-- {
			sum1 += uint32(p[0])<<8 | uint32(p[1])
			p = p[2:]
			sum2 += sum1
		}
		sum1 = sum1&0xFFFF + sum1>>16
		sum2 = sum2&0xFFFF + sum2>>16
	}

	if len(data)%2 == 1 {
		sum1 += uint32(p[0]) << 8
		sum2 += sum1
		sum1 = sum1&0xFFFF + sum1>>16
		sum2 = sum2&0xFFFF + sum2>>16
	}

	sum1 = sum1&0xFFFF + sum1>>16
	sum2 = sum2&0xFFFF + sum2>>16

	return sum2<<16 | sum1
}
