// Package core provides core HDF5 structures and algorithms.
package core

// JenkinsChecksum computes the Jenkins lookup3 hash checksum for the given data.
//
// This is the checksum function used by HDF5 for metadata integrity verification
// in Superblock V2/V3, B-tree V2 headers, and Fractal Heap structures.
//
// The function implements Bob Jenkins' lookup3 hash algorithm with initval=0,
// which is the standard for HDF5 metadata checksums per H5checksum_metadata().
//
// Reference:
//   - HDF5 C Library: H5checksum.c - H5_checksum_metadata() and H5_checksum_lookup3()
//   - Algorithm: http://burtleburtle.net/bob/hash/doobs.html
//   - Format Spec: https://docs.hdfgroup.org/hdf5/latest/_f_m_t3.html
//
// Parameters:
//   - data: byte slice to checksum
//
// Returns:
//   - uint32 checksum value
func JenkinsChecksum(data []byte) uint32 {
	return jenkinsLookup3(data, 0)
}

// jenkinsLookup3 implements Bob Jenkins' lookup3 hash algorithm.
//
// This is a direct port of H5_checksum_lookup3() from H5checksum.c.
// The algorithm provides good distribution and is fast for checksumming.
//
// Parameters:
//   - data: byte slice to hash
//   - initval: initial value (use 0 for HDF5 metadata checksums)
//
// Returns:
//   - uint32 hash value
//
//nolint:funlen // Jenkins lookup3 algorithm has long switch statement for remaining bytes.
func jenkinsLookup3(data []byte, initval uint32) uint32 {
	length := len(data)

	// Set up the internal state.
	a := uint32(0xdeadbeef) + uint32(length) + initval //nolint:gosec // G115: length is always >= 0, safe conversion.
	b := a
	c := a

	// Process 12-byte chunks.
	// CRITICAL: The C reference uses "while (length > 12)" (strictly greater),
	// so when exactly 12 bytes remain, they are handled by the switch/final below,
	// NOT by the mix loop. Using ">=" here would produce wrong checksums for
	// inputs whose length is a multiple of 12.
	i := 0
	for i+12 < length {
		a += uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		b += uint32(data[i+4]) | uint32(data[i+5])<<8 | uint32(data[i+6])<<16 | uint32(data[i+7])<<24
		c += uint32(data[i+8]) | uint32(data[i+9])<<8 | uint32(data[i+10])<<16 | uint32(data[i+11])<<24

		// Mix (inlined H5_lookup3_mix macro).
		a -= c
		a ^= (c << 4) | (c >> 28)
		c += b
		b -= a
		b ^= (a << 6) | (a >> 26)
		a += c
		c -= b
		c ^= (b << 8) | (b >> 24)
		b += a
		a -= c
		a ^= (c << 16) | (c >> 16)
		c += b
		b -= a
		b ^= (a << 19) | (a >> 13)
		a += c
		c -= b
		c ^= (b << 4) | (b >> 28)
		b += a

		i += 12
	}

	// Handle remaining bytes (0-12 bytes).
	// When length is a multiple of 12, remaining is 12 (not 0), matching the C code's
	// "while (length > 12)" loop condition.
	remaining := length - i
	switch remaining {
	case 12:
		c += uint32(data[i+11]) << 24
		fallthrough
	case 11:
		c += uint32(data[i+10]) << 16
		fallthrough
	case 10:
		c += uint32(data[i+9]) << 8
		fallthrough
	case 9:
		c += uint32(data[i+8])
		fallthrough
	case 8:
		b += uint32(data[i+7]) << 24
		fallthrough
	case 7:
		b += uint32(data[i+6]) << 16
		fallthrough
	case 6:
		b += uint32(data[i+5]) << 8
		fallthrough
	case 5:
		b += uint32(data[i+4])
		fallthrough
	case 4:
		a += uint32(data[i+3]) << 24
		fallthrough
	case 3:
		a += uint32(data[i+2]) << 16
		fallthrough
	case 2:
		a += uint32(data[i+1]) << 8
		fallthrough
	case 1:
		a += uint32(data[i])
	case 0:
		// No remaining bytes, skip final mix.
		return c
	}

	// Final mix (inlined H5_lookup3_final macro).
	c ^= b
	c -= (b << 14) | (b >> 18)
	a ^= c
	a -= (c << 11) | (c >> 21)
	b ^= a
	b -= (a << 25) | (a >> 7)
	c ^= b
	c -= (b << 16) | (b >> 16)
	a ^= c
	a -= (c << 4) | (c >> 28)
	b ^= a
	b -= (a << 14) | (a >> 18)
	c ^= b
	c -= (b << 24) | (b >> 8)

	return c
}
