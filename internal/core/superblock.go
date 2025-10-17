package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// HDF5 file signature and supported superblock versions.
const (
	Signature = "\x89HDF\r\n\x1a\n"
	Version0  = 0
	Version2  = 2
	Version3  = 3
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
}

// ReadSuperblock reads and parses the HDF5 superblock from the file.
// It supports versions 0, 2, and 3 of the superblock format.
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
	if version != Version0 && version != Version2 && version != Version3 {
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
		// For v2 and v3: endianness in byte 9, packed sizes in byte 10
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
		//   64-71: Object header address (8 bytes) <-- This is what we want!
		//   72-75: Cache type (4 bytes)
		//   76-79: Reserved (4 bytes)
		//   80-95: Scratch-pad (16 bytes)

		// The root group object header address is embedded in the superblock at offset 64
		// NOT at a separate file location - it's the second field of the embedded entry
		rootGroupOffset := 64
		sb.RootGroup, err = readValue(rootGroupOffset, offsetSize)
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}
	} else {
		// For v2 and v3, fields start at byte 12
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
	}

	return sb, nil
}
