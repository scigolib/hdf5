package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadSuperblockV0(t *testing.T) {
	// Минимальный валидный суперблок версии 0 (96 байт minimum)
	data := []byte{
		// Сигнатура (8 байт)
		0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n',
		// Версия (1 байт) - offset 8
		0x00,
		// Флаг порядка байт (1 байт)
		0x00,
		// Reserved (3 bytes)
		0x00, 0x00, 0x00,
		// Размер смещения (1 байт) - offset 13
		0x08,
		// Размер длины (1 байт) - offset 14
		0x08,
		// Reserved (1 byte)
		0x00,
		// Group leaf/internal node K (2 bytes)
		0x00, 0x00,
		// Group internal node K (2 bytes)
		0x00, 0x00,
		// File consistency flags (4 bytes)
		0x00, 0x00, 0x00, 0x00,
		// Base address (8 bytes) - offset 24
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Free space index address (8 bytes)
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		// End-of-file address (8 bytes)
		0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Driver info block (8 bytes)
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		// Root group symbol table entry (32 bytes) - offset 56
		// Link name offset (8 bytes)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Object header address (8 bytes) - offset 64 - THIS IS ROOT GROUP
		0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Cache type (4 bytes)
		0x00, 0x00, 0x00, 0x00,
		// Reserved (4 bytes)
		0x00, 0x00, 0x00, 0x00,
		// Scratch-pad (16 bytes)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	sb, err := ReadSuperblock(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, uint8(0), sb.Version)
	require.Equal(t, uint8(8), sb.OffsetSize)
	require.Equal(t, uint8(8), sb.LengthSize)
	require.Equal(t, uint64(0x60), sb.RootGroup)
	require.Equal(t, binary.LittleEndian, sb.Endianness)
}

func TestReadSuperblockV2(t *testing.T) {
	// Minimal valid v2 superblock (48 bytes)
	data := []byte{
		// Signature (8 bytes) - offset 0
		0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n',
		// Version (1 byte) - offset 8
		0x02,
		// Endianness flag (1 byte) - offset 9: 0=little-endian
		0x00,
		// Packed sizes (1 byte) - offset 10: 0x33 = offset:8 bytes(code 3), length:8 bytes(code 3)
		0x33,
		// Reserved (1 byte) - offset 11
		0x00,
		// Base address (8 bytes) - offset 12
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Superblock extension address (8 bytes) - offset 20
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		// End-of-file address (8 bytes) - offset 28
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Root group address (8 bytes) - offset 36
		0x48, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Checksum (4 bytes) - offset 44
		0x00, 0x00, 0x00, 0x00,
	}

	sb, err := ReadSuperblock(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, uint8(2), sb.Version)
	require.Equal(t, uint8(8), sb.OffsetSize)
	require.Equal(t, uint8(8), sb.LengthSize)
	require.Equal(t, uint64(0x48), sb.RootGroup)
	require.Equal(t, binary.LittleEndian, sb.Endianness)
}

func TestReadSuperblockV3(t *testing.T) {
	// Minimal valid v3 superblock (48 bytes)
	data := []byte{
		// Signature (8 bytes) - offset 0
		0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n',
		// Version (1 byte) - offset 8
		0x03,
		// Endianness flag (1 byte) - offset 9: 0=little-endian
		0x00,
		// Packed sizes (1 byte) - offset 10: 0x33 = offset:8 bytes(code 3), length:8 bytes(code 3)
		0x33,
		// Reserved (1 byte) - offset 11
		0x00,
		// Base address (8 bytes) - offset 12
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Superblock extension address (8 bytes) - offset 20
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		// End-of-file address (8 bytes) - offset 28
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Root group address (8 bytes) - offset 36
		0x48, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Checksum (4 bytes) - offset 44
		0x00, 0x00, 0x00, 0x00,
	}

	sb, err := ReadSuperblock(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, uint8(3), sb.Version)
	require.Equal(t, uint8(8), sb.OffsetSize)
	require.Equal(t, uint8(8), sb.LengthSize)
	require.Equal(t, uint64(0x48), sb.RootGroup)
	require.Equal(t, binary.LittleEndian, sb.Endianness)
}

func TestRealFileSuperblock(t *testing.T) {
	files := []string{
		"../../testdata/v2.h5",
		"../../testdata/v3.h5",
		"../../testdata/with_groups.h5",
	}

	for _, fname := range files {
		t.Run(fname, func(t *testing.T) {
			f, err := os.Open(fname)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			sb, err := ReadSuperblock(f)
			require.NoError(t, err, "file: %s", fname)
			require.True(t, sb.Version == 2 || sb.Version == 3, "invalid version: %d", sb.Version)
			require.Equal(t, uint8(8), sb.OffsetSize)
			require.Equal(t, uint8(8), sb.LengthSize)

			// Проверка, что адрес корневой группы валиден
			fi, err := f.Stat()
			require.NoError(t, err)
			require.True(t, sb.RootGroup < uint64(fi.Size()),
				"root group address %d beyond file size %d", sb.RootGroup, fi.Size())
		})
	}
}
