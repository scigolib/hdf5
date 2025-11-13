// Package core provides low-level HDF5 file format parsing and generation.
// It handles superblocks, object headers, messages, and other HDF5 structures
// without CGo dependencies.
package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// HDF5 file signature and supported superblock versions.
const (
	Signature = "\x89HDF\r\n\x1a\n"
	Version0  = 0
	Version2  = 2
	Version3  = 3
	Version4  = 4
)

// Superblock represents the HDF5 file superblock containing file-level metadata.
type Superblock struct {
	Version        uint8
	OffsetSize     uint8
	LengthSize     uint8
	BaseAddress    uint64
	RootGroup      uint64
	Endianness     binary.ByteOrder
	SuperExtension uint64
	DriverInfo     uint64

	// V0-specific: Cached symbol table info for root group
	// These are only used when Version == 0
	RootBTreeAddr uint64 // B-tree address for root group (v0 only)
	RootHeapAddr  uint64 // Local heap address for root group (v0 only)

	// V4-specific: Checksum fields (HDF5 2.0+)
	ChecksumAlgorithm uint8  // 0=none, 1=CRC32, 2=Fletcher32 (v4 only)
	Checksum          uint32 // Superblock checksum (v4 only)
}

// ReadSuperblock reads and parses the HDF5 superblock from the file.
// It supports versions 0, 2, 3, and 4 of the superblock format.
//
//nolint:maintidx // Complex HDF5 format parsing requires handling multiple versions and field layouts
func ReadSuperblock(r io.ReaderAt) (*Superblock, error) {
	buf := utils.GetBuffer(128)
	defer utils.ReleaseBuffer(buf)

	n, err := r.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, utils.WrapError("superblock read failed", err)
	}
	if n < 48 {
		return nil, errors.New("file too small to contain a superblock")
	}

	if string(buf[:8]) != Signature {
		return nil, errors.New("invalid HDF5 signature")
	}

	version := buf[8]
	if version != Version0 && version != Version2 && version != Version3 && version != Version4 {
		return nil, fmt.Errorf("unsupported superblock version: %d", version)
	}

	// Endianness and size handling depends on version
	var endianness binary.ByteOrder
	var offsetSize, lengthSize uint8

	if version == Version0 {
		// For v0: sizes in bytes 13-14, endianness presumably little-endian (check spec)
		offsetSize = buf[13]
		lengthSize = buf[14]
		endianness = binary.LittleEndian // v0 files are typically little-endian
	} else {
		// For v2, v3, and v4: endianness in byte 9, packed sizes in byte 10
		// Byte 9: flags byte - bit 0 is endianness (0=LE, 1=BE)
		switch buf[9] & 0x01 { // Check only bit 0
		case 0:
			endianness = binary.LittleEndian
		case 1:
			endianness = binary.BigEndian
		}

		// Byte 10: Size of Offsets
		// In v2/v3, this can be either:
		// 1. Direct size value (1, 2, 4, or 8 bytes)
		// 2. Packed codes (lower 4 bits=offset, upper 4 bits=length)
		// If the value is a valid size (1/2/4/8), treat it as direct size
		// Otherwise, treat as packed codes
		sizesByte := buf[10]

		// Check if it's a direct size value
		validDirectSizes := map[uint8]bool{1: true, 2: true, 4: true, 8: true}
		if validDirectSizes[sizesByte] {
			// Direct size format: byte 10 = offset size, byte 11 = length size (implied 8)
			offsetSize = sizesByte
			lengthSize = 8 // HDF5 v2/v3 typically uses 8-byte lengths
		} else {
			// Packed format: lower 4 bits = offset code, upper 4 bits = length code
			// Size codes: 0=1 byte, 1=2 bytes, 2=4 bytes, 3=8 bytes
			offsetSizeCode := sizesByte & 0x0F        // Lower 4 bits
			lengthSizeCode := (sizesByte >> 4) & 0x0F // Upper 4 bits

			// Convert codes to actual sizes
			sizeCodeMap := map[uint8]uint8{0: 1, 1: 2, 2: 4, 3: 8}
			var ok bool
			offsetSize, ok = sizeCodeMap[offsetSizeCode]
			if !ok {
				return nil, fmt.Errorf("invalid offset size code: %d", offsetSizeCode)
			}
			lengthSize, ok = sizeCodeMap[lengthSizeCode]
			if !ok {
				return nil, fmt.Errorf("invalid length size code: %d", lengthSizeCode)
			}
		}
	}

	// Handle zero sizes (in test files)
	if offsetSize == 0 {
		offsetSize = 8
	}
	if lengthSize == 0 {
		lengthSize = 8
	}

	validSizes := map[uint8]bool{1: true, 2: true, 4: true, 8: true}
	if !validSizes[offsetSize] || !validSizes[lengthSize] {
		return nil, fmt.Errorf("invalid sizes for version %d: offset=%d, length=%d",
			version, offsetSize, lengthSize)
	}

	// Helper function to read variable-sized values
	readValue := func(offset int, size uint8) (uint64, error) {
		if offset < 0 || offset+int(size) > len(buf) {
			return 0, fmt.Errorf("buffer overflow: offset=%d, size=%d", offset, size)
		}

		data := buf[offset : offset+int(size)]
		switch size {
		case 1:
			return uint64(data[0]), nil
		case 2:
			return uint64(endianness.Uint16(data)), nil
		case 4:
			return uint64(endianness.Uint32(data)), nil
		case 8:
			return endianness.Uint64(data), nil
		default:
			return 0, fmt.Errorf("unsupported size: %d", size)
		}
	}

	sb := &Superblock{
		Version:    version,
		OffsetSize: offsetSize,
		LengthSize: lengthSize,
		Endianness: endianness,
	}

	if version == Version0 {
		sb.BaseAddress = 0
		// Version 0 superblock structure:
		// Offset 24-31: Base address
		// Offset 32-39: Free space index
		// Offset 40-47: End-of-File address (NOT root group!)
		// Offset 48-55: Driver info block
		// Offset 56-95: Root group symbol table entry (32 bytes total):
		//   56-63: Link name offset (8 bytes)
		//   64-71: Object header address (8 bytes) <-- Modern format uses this
		//   72-75: Cache type (4 bytes)
		//   76-79: Reserved (4 bytes)
		//   80-87: B-tree address (8 bytes) <-- Symbol table format uses this
		//   88-95: Local heap address (8 bytes)

		// First, try reading the object header address at offset 64
		rootGroupOffset := 64
		sb.RootGroup, err = readValue(rootGroupOffset, offsetSize)
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}

		// If object header address is 0, this file uses symbol table format
		// In this case, read the B-tree address from the scratch-pad at offset 80
		if sb.RootGroup == 0 {
			btreeOffset := 80
			sb.RootGroup, err = readValue(btreeOffset, offsetSize)
			if err != nil {
				return nil, utils.WrapError("b-tree address read failed", err)
			}
		}
	} else {
		// For v2, v3, and v4, fields start at byte 12
		current := 12

		sb.BaseAddress, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("base address read failed", err)
		}
		current += int(offsetSize)

		sb.SuperExtension, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("super extension read failed", err)
		}
		current += int(offsetSize)

		// Skip end-of-file address
		current += int(offsetSize)

		sb.RootGroup, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}
		current += int(offsetSize)

		// V4-specific: Read checksum algorithm and checksum
		if version == Version4 {
			// Checksum algorithm (byte 44)
			if current >= len(buf) {
				return nil, errors.New("insufficient data for v4 checksum fields")
			}
			sb.ChecksumAlgorithm = buf[current]
			current++

			// Reserved bytes (3 bytes, skip)
			current += 3

			// Checksum (4 bytes, bytes 48-51)
			checksumValue, err := readValue(current, 4)
			if err != nil {
				return nil, utils.WrapError("checksum read failed", err)
			}
			// Safe conversion: readValue returns uint64, but checksum is always 4 bytes
			if checksumValue > 0xFFFFFFFF {
				return nil, fmt.Errorf("invalid checksum value: %d", checksumValue)
			}
			sb.Checksum = uint32(checksumValue)

			// Validate checksum (bytes 8-47 are checksummed)
			if err := validateSuperblockChecksum(buf[8:current], sb.Checksum, sb.ChecksumAlgorithm); err != nil {
				return nil, fmt.Errorf("superblock v4 checksum validation failed: %w", err)
			}

			// V4 requires superblock extension (cannot be UNDEFINED)
			if sb.SuperExtension == 0xFFFFFFFFFFFFFFFF {
				return nil, errors.New("superblock v4 requires extension address")
			}
		}
	}

	return sb, nil
}

// validateSuperblockChecksum validates the superblock checksum using the specified algorithm.
// Algorithm codes: 0=none, 1=CRC32, 2=Fletcher32.
func validateSuperblockChecksum(data []byte, checksum uint32, algorithm uint8) error {
	switch algorithm {
	case 0: // No checksum
		return nil

	case 1: // CRC32
		computed := crc32.ChecksumIEEE(data)
		if computed != checksum {
			return fmt.Errorf("CRC32 mismatch: expected 0x%08x, got 0x%08x",
				checksum, computed)
		}
		return nil

	case 2: // Fletcher32
		computed := computeFletcher32(data)
		if computed != checksum {
			return fmt.Errorf("Fletcher32 mismatch: expected 0x%08x, got 0x%08x",
				checksum, computed)
		}
		return nil

	default:
		return fmt.Errorf("unknown checksum algorithm: %d", algorithm)
	}
}

// computeFletcher32 computes the Fletcher-32 checksum as specified in HDF5 format spec.
// Fletcher-32 is a checksum algorithm that provides error detection with low computational cost.
func computeFletcher32(data []byte) uint32 {
	var sum1, sum2 uint16

	// Process 16-bit words
	for i := 0; i < len(data); i += 2 {
		var word uint16
		if i+1 < len(data) {
			word = binary.LittleEndian.Uint16(data[i : i+2])
		} else {
			// Last byte (odd length)
			word = uint16(data[i])
		}

		sum1 = (sum1 + word) % 65535
		sum2 = (sum2 + sum1) % 65535
	}

	return (uint32(sum2) << 16) | uint32(sum1)
}

// WriteTo writes the superblock to the writer at offset 0.
// For MVP (v0.11.0-beta), only superblock v2 is supported for writing.
//
// Superblock v2 format (48 bytes):
//
//	Bytes 0-7:   Signature (\x89HDF\r\n\x1a\n)
//	Byte 8:      Version (2)
//	Byte 9:      Size of Offsets (8 bytes)
//	Byte 10:     Size of Lengths (8 bytes)
//	Byte 11:     File Consistency Flags (0)
//	Bytes 12-19: Base Address (typically 0)
//	Bytes 20-27: Superblock Extension Address (UNDEF if none)
//	Bytes 28-35: End-of-File Address (file size)
//	Bytes 36-43: Root Group Object Header Address
//	Bytes 44-47: Superblock Checksum (CRC32)
//
// Parameters:
//   - w: Writer (typically a FileWriter)
//   - eofAddress: Current end-of-file address
//
// Returns error if write fails or if superblock version is not supported.
func (sb *Superblock) WriteTo(w io.WriterAt, eofAddress uint64) error {
	// Support v0 (legacy) and v2 (modern)
	if sb.Version != Version0 && sb.Version != Version2 {
		return fmt.Errorf("only superblock version 0 and 2 are supported for writing, got version %d", sb.Version)
	}

	// Dispatch to version-specific writer
	switch sb.Version {
	case Version0:
		return sb.writeV0(w, eofAddress)
	case Version2:
		return sb.writeV2(w, eofAddress)
	default:
		return fmt.Errorf("unsupported superblock version: %d", sb.Version)
	}
}

// writeV2 writes superblock version 2 (modern format with checksums).
func (sb *Superblock) writeV2(w io.WriterAt, eofAddress uint64) error {
	// Validate required fields
	if sb.OffsetSize != 8 || sb.LengthSize != 8 {
		return fmt.Errorf("only 8-byte offsets and lengths are supported for writing, got offset=%d, length=%d",
			sb.OffsetSize, sb.LengthSize)
	}

	// Allocate buffer for superblock v2 (48 bytes)
	buf := make([]byte, 48)

	// Bytes 0-7: Signature
	copy(buf[0:8], Signature)

	// Byte 8: Version 2
	buf[8] = 2

	// Byte 9: Size of offsets (8 bytes)
	buf[9] = 8

	// Byte 10: Size of lengths (8 bytes)
	buf[10] = 8

	// Byte 11: File consistency flags (0 for now)
	buf[11] = 0

	// Bytes 12-19: Base address (typically 0)
	binary.LittleEndian.PutUint64(buf[12:20], sb.BaseAddress)

	// Bytes 20-27: Superblock extension address (UNDEF if none)
	// UNDEF is represented as 0xFFFFFFFFFFFFFFFF
	superExt := sb.SuperExtension
	if superExt == 0 {
		superExt = 0xFFFFFFFFFFFFFFFF // UNDEF
	}
	binary.LittleEndian.PutUint64(buf[20:28], superExt)

	// Bytes 28-35: End-of-file address
	binary.LittleEndian.PutUint64(buf[28:36], eofAddress)

	// Bytes 36-43: Root group object header address
	binary.LittleEndian.PutUint64(buf[36:44], sb.RootGroup)

	// Bytes 44-47: Superblock checksum (CRC32 of bytes 0-43)
	checksum := crc32.ChecksumIEEE(buf[0:44])
	binary.LittleEndian.PutUint32(buf[44:48], checksum)

	// Write superblock at offset 0
	n, err := w.WriteAt(buf, 0)
	if err != nil {
		return fmt.Errorf("failed to write superblock: %w", err)
	}

	if n != 48 {
		return fmt.Errorf("incomplete superblock write: wrote %d bytes, expected 48", n)
	}

	return nil
}

// writeV0 writes superblock version 0 (legacy format for maximum compatibility).
// This format is used by older HDF5 tools and is the most widely supported.
//
// Superblock v0 structure (96 bytes minimum):
//
//	Bytes 0-7: Format Signature (\211HDF\r\n\032\n)
//	Byte 8: Superblock Version (0)
//	Byte 9: Free-space Storage Version (0)
//	Byte 10: Root Group Symbol Table Entry Version (0)
//	Byte 11: Reserved (0)
//	Byte 12: Shared Header Message Format Version (0)
//	Byte 13: Size of Offsets (8)
//	Byte 14: Size of Lengths (8)
//	Byte 15: Reserved (0)
//	Bytes 16-17: Group Leaf Node K (4)
//	Bytes 18-19: Group Internal Node K (16)
//	Bytes 20-23: File Consistency Flags (0)
//	Bytes 24-31: Base Address (0)
//	Bytes 32-39: Free Space Info Address (UNDEF)
//	Bytes 40-47: End of File Address
//	Bytes 48-55: Driver Info Block Address (UNDEF)
//	Bytes 56-95: Root Group Symbol Table Entry (40 bytes)
func (sb *Superblock) writeV0(w io.WriterAt, eofAddress uint64) error {
	// Validate required fields
	if sb.OffsetSize != 8 || sb.LengthSize != 8 {
		return fmt.Errorf("only 8-byte offsets and lengths are supported for writing, got offset=%d, length=%d",
			sb.OffsetSize, sb.LengthSize)
	}

	// Allocate buffer for superblock v0 (96 bytes)
	buf := make([]byte, 96)

	// Bytes 0-7: Signature
	copy(buf[0:8], Signature)

	// Byte 8: Version 0
	buf[8] = 0

	// Byte 9: Free-space Storage Version (0)
	buf[9] = 0

	// Byte 10: Root Group Symbol Table Entry Version (0)
	buf[10] = 0

	// Byte 11: Reserved
	buf[11] = 0

	// Byte 12: Shared Header Message Format Version (0)
	buf[12] = 0

	// Byte 13: Size of offsets (8 bytes)
	buf[13] = 8

	// Byte 14: Size of lengths (8 bytes)
	buf[14] = 8

	// Byte 15: Reserved
	buf[15] = 0

	// Bytes 16-17: Group Leaf Node K (default: 4)
	binary.LittleEndian.PutUint16(buf[16:18], 4)

	// Bytes 18-19: Group Internal Node K (default: 16)
	binary.LittleEndian.PutUint16(buf[18:20], 16)

	// Bytes 20-23: File Consistency Flags (0 = file is closed properly)
	binary.LittleEndian.PutUint32(buf[20:24], 0)

	// Bytes 24-31: Base address (typically 0)
	binary.LittleEndian.PutUint64(buf[24:32], sb.BaseAddress)

	// Bytes 32-39: Free Space Info Address (UNDEF for now)
	binary.LittleEndian.PutUint64(buf[32:40], 0xFFFFFFFFFFFFFFFF)

	// Bytes 40-47: End-of-file address
	binary.LittleEndian.PutUint64(buf[40:48], eofAddress)

	// Bytes 48-55: Driver Info Block Address (UNDEF)
	binary.LittleEndian.PutUint64(buf[48:56], 0xFFFFFFFFFFFFFFFF)

	// Bytes 56-95: Root Group Symbol Table Entry (40 bytes)
	// This is a Symbol Table Entry with cached B-tree/Heap addresses
	//
	// Symbol Table Entry structure (from C library H5Gent.c):
	//   Bytes 0-7: Link Name Offset (in local heap) - 0 for root
	//   Bytes 8-15: Object Header Address
	//   Bytes 16-19: Cache Type (1 = H5G_CACHED_STAB for symbol table)
	//   Bytes 20-23: Reserved
	//   Bytes 24-39: Scratch-pad space (16 bytes):
	//     - Bytes 24-31: B-tree address (for H5G_CACHED_STAB)
	//     - Bytes 32-39: Local heap address (for H5G_CACHED_STAB)

	// Link Name Offset (0 for root group)
	binary.LittleEndian.PutUint64(buf[56:64], 0)

	// Object Header Address (root group address)
	binary.LittleEndian.PutUint64(buf[64:72], sb.RootGroup)

	// Cache Type (1 = H5G_CACHED_STAB, meaning symbol table with cached addresses)
	binary.LittleEndian.PutUint32(buf[72:76], 1)

	// Reserved
	binary.LittleEndian.PutUint32(buf[76:80], 0)

	// Scratch-pad space (16 bytes): Cached B-tree and Heap addresses
	// This is CRITICAL for v0 - h5dump needs these to find the root group!
	binary.LittleEndian.PutUint64(buf[80:88], sb.RootBTreeAddr) // B-tree address
	binary.LittleEndian.PutUint64(buf[88:96], sb.RootHeapAddr)  // Heap address

	// Write superblock at offset 0
	n, err := w.WriteAt(buf, 0)
	if err != nil {
		return fmt.Errorf("failed to write superblock v0: %w", err)
	}

	if n != 96 {
		return fmt.Errorf("incomplete superblock v0 write: wrote %d bytes, expected 96", n)
	}

	return nil
}
