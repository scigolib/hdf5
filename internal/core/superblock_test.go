package core

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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

// Test superblock writing
func TestSuperblockWrite(t *testing.T) {
	t.Run("write superblock v2", func(t *testing.T) {
		// Create a temporary file
		tmpFile, err := os.CreateTemp("", "test_superblock_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		// Create superblock
		sb := &Superblock{
			Version:        Version2,
			OffsetSize:     8,
			LengthSize:     8,
			BaseAddress:    0,
			RootGroup:      48, // Assume root group starts after superblock
			SuperExtension: 0,  // No extension (will be set to UNDEF)
			Endianness:     binary.LittleEndian,
		}

		// Write superblock
		eofAddress := uint64(1024)
		err = sb.WriteTo(tmpFile, eofAddress)
		require.NoError(t, err)

		// Read back and verify
		readSb, err := ReadSuperblock(tmpFile)
		require.NoError(t, err)

		// Verify fields
		assert.Equal(t, uint8(2), readSb.Version)
		assert.Equal(t, uint8(8), readSb.OffsetSize)
		assert.Equal(t, uint8(8), readSb.LengthSize)
		assert.Equal(t, uint64(0), readSb.BaseAddress)
		assert.Equal(t, uint64(48), readSb.RootGroup)
		// SuperExtension should be UNDEF (0xFFFFFFFFFFFFFFFF)
		assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), readSb.SuperExtension)
	})

	t.Run("write then read round-trip", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test_roundtrip_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		// Original superblock
		original := &Superblock{
			Version:        Version2,
			OffsetSize:     8,
			LengthSize:     8,
			BaseAddress:    0,
			RootGroup:      128,
			SuperExtension: 0,
			Endianness:     binary.LittleEndian,
		}

		// Write
		err = original.WriteTo(tmpFile, 2048)
		require.NoError(t, err)

		// Read
		read, err := ReadSuperblock(tmpFile)
		require.NoError(t, err)

		// Compare
		assert.Equal(t, original.Version, read.Version)
		assert.Equal(t, original.OffsetSize, read.OffsetSize)
		assert.Equal(t, original.LengthSize, read.LengthSize)
		assert.Equal(t, original.BaseAddress, read.BaseAddress)
		assert.Equal(t, original.RootGroup, read.RootGroup)
	})

	t.Run("rejects unsupported version", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test_version_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		sb := &Superblock{
			Version:    Version0, // v0 not supported for writing
			OffsetSize: 8,
			LengthSize: 8,
		}

		err = sb.WriteTo(tmpFile, 1024)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "only superblock version 2 is supported")
	})

	t.Run("rejects invalid sizes", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test_sizes_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		sb := &Superblock{
			Version:    Version2,
			OffsetSize: 4, // Only 8 is supported
			LengthSize: 8,
		}

		err = sb.WriteTo(tmpFile, 1024)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "only 8-byte offsets")
	})

	t.Run("verify checksum", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test_checksum_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		sb := &Superblock{
			Version:        Version2,
			OffsetSize:     8,
			LengthSize:     8,
			BaseAddress:    0,
			RootGroup:      48,
			SuperExtension: 0,
		}

		// Write superblock
		err = sb.WriteTo(tmpFile, 1024)
		require.NoError(t, err)

		// Read raw bytes
		buf := make([]byte, 48)
		n, err := tmpFile.ReadAt(buf, 0)
		require.NoError(t, err)
		require.Equal(t, 48, n)

		// Verify checksum
		expectedChecksum := crc32.ChecksumIEEE(buf[0:44])
		actualChecksum := binary.LittleEndian.Uint32(buf[44:48])
		assert.Equal(t, expectedChecksum, actualChecksum,
			"superblock checksum mismatch")
	})

	t.Run("verify signature", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test_signature_*.h5")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		sb := &Superblock{
			Version:    Version2,
			OffsetSize: 8,
			LengthSize: 8,
			RootGroup:  48,
		}

		err = sb.WriteTo(tmpFile, 1024)
		require.NoError(t, err)

		// Read signature
		buf := make([]byte, 8)
		n, err := tmpFile.ReadAt(buf, 0)
		require.NoError(t, err)
		require.Equal(t, 8, n)

		// Verify signature
		assert.Equal(t, Signature, string(buf))
	})
}
