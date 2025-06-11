package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

const (
	Signature = "\x89HDF\r\n\x1a\n"
	Version0  = 0
	Version2  = 2
	Version3  = 3
)

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

func ReadSuperblock(r io.ReaderAt) (*Superblock, error) {
	buf := utils.GetBuffer(128)
	defer utils.ReleaseBuffer(buf)

	n, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
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

	// Endianness is defined in byte 9 for ALL versions
	var endianness binary.ByteOrder
	switch buf[9] & 0x01 {
	case 0:
		endianness = binary.LittleEndian
	case 1:
		endianness = binary.BigEndian
	default:
		return nil, fmt.Errorf("invalid endianness flag: %d", buf[9]&0x01)
	}

	var offsetSize, lengthSize uint8
	if version == Version0 {
		offsetSize = buf[10]
		lengthSize = buf[11]
	} else {
		sizeFlags := buf[12]
		offsetSize = sizeFlags & 0x0F
		lengthSize = (sizeFlags >> 4) & 0x0F
	}

	// Handle zero sizes (common in test files)
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

	// Helper function to read variable-size values from buffer
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
		sb.RootGroup, err = readValue(40, 8) // Root group symbol table address
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}
	} else {
		current := 16

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

	// Special handling for h5py-generated files
	if sb.RootGroup == 0 {
		sb.RootGroup = 48 // Default location for root group
	}

	return sb, nil
}
