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
	Version     uint8
	OffsetSize  uint8
	LengthSize  uint8
	BaseAddress uint64
	RootGroup   uint64
	Endianness  binary.ByteOrder
}

func ReadSuperblock(r io.ReaderAt) (*Superblock, error) {
	buf := utils.GetBuffer(16)
	defer utils.ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, 0); err != nil {
		return nil, utils.WrapError("superblock read failed", err)
	}

	if string(buf[:8]) != Signature {
		return nil, errors.New("invalid HDF5 signature")
	}

	version := buf[8]
	if version != Version0 && version != Version2 && version != Version3 {
		return nil, fmt.Errorf("unsupported superblock version: %d", version)
	}

	// Исправление: используем только младший бит для порядка байт
	var endianness binary.ByteOrder
	switch buf[9] & 0x01 { // Берем только младший бит
	case 0:
		endianness = binary.LittleEndian
	case 1:
		endianness = binary.BigEndian
	default:
		endianness = binary.LittleEndian
		fmt.Printf("Warning: invalid endianness, using little-endian\n")
	}

	sizeFlags := buf[12]
	offsetSize := sizeFlags & 0x0F
	lengthSize := (sizeFlags >> 4) & 0x0F

	// Всегда читаем 64-битные значения
	readU64 := func(offset int64) (uint64, error) {
		b := make([]byte, 8)
		if _, err := r.ReadAt(b, offset); err != nil {
			return 0, err
		}
		return endianness.Uint64(b), nil
	}

	baseAddr, err := readU64(16)
	if err != nil {
		return nil, utils.WrapError("base address read failed", err)
	}

	rootGroup, err := readU64(32)
	if err != nil {
		return nil, utils.WrapError("root group address read failed", err)
	}

	fmt.Printf("Base address: %d (0x%x)\n", baseAddr, baseAddr)
	fmt.Printf("Root group address: %d (0x%x)\n", rootGroup, rootGroup)

	return &Superblock{
		Version:     version,
		OffsetSize:  offsetSize,
		LengthSize:  lengthSize,
		BaseAddress: baseAddr,
		RootGroup:   rootGroup,
		Endianness:  endianness,
	}, nil
}
