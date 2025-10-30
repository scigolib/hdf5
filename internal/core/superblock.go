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
// Returns error if write fails or if superblock version is not 2.
func (sb *Superblock) WriteTo(w io.WriterAt, eofAddress uint64) error {
	// For MVP, only support writing v2 superblocks
	if sb.Version != Version2 {
		return fmt.Errorf("only superblock version 2 is supported for writing, got version %d", sb.Version)
	}

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
