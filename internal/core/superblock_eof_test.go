package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildSuperblockImage builds a hand-crafted superblock image of the minimum
// size for the given version, byte order and an 8-byte offset width. The EOF
// and root group addresses are placed at the offsets the format defines.
func buildSuperblockImage(t *testing.T, version uint8, order binary.ByteOrder, eof, rootGroup uint64) []byte {
	t.Helper()

	// The reader takes v0 field positions from hardcoded 8-byte offsets and
	// always parses v0 as little-endian, so a big-endian v0 image would not
	// reflect supported behavior.
	if version == Version0 && order != binary.LittleEndian {
		t.Fatalf("v0 superblocks are parsed as little-endian only")
	}

	size := minSuperblockSizeV2
	if version == Version0 {
		size = minSuperblockSizeV0
	}
	buf := make([]byte, size)
	copy(buf[0:8], Signature)
	buf[8] = version

	putAddr := func(offset int, value uint64) {
		order.PutUint64(buf[offset:offset+8], value)
	}

	if version == Version0 {
		buf[13] = 8 // Size of offsets.
		buf[14] = 8 // Size of lengths.
		putAddr(40, eof)
		putAddr(64, rootGroup)
		return buf
	}

	// v2/v3: byte 9 selects the byte order, byte 10 packs the sizes
	// (0x33 = 8-byte offsets, 8-byte lengths).
	if order == binary.BigEndian {
		buf[9] = 0x01
	}
	buf[10] = 0x33
	putAddr(12, 0)          // Base address.
	putAddr(20, ^uint64(0)) // Superblock extension (undefined).
	putAddr(28, eof)        // End-of-File address.
	putAddr(36, rootGroup)  // Root group object header address.
	return buf
}

// TestReadSuperblockEOFAddress verifies that the End-of-File address is parsed
// for superblock versions 0, 2 and 3 in both supported byte orders, including
// the zero and minimum-size boundary values.
func TestReadSuperblockEOFAddress(t *testing.T) {
	tests := []struct {
		name      string
		version   uint8
		order     binary.ByteOrder
		eof       uint64
		rootGroup uint64
	}{
		{
			name:      "v0 little-endian",
			version:   Version0,
			order:     binary.LittleEndian,
			eof:       0x818,
			rootGroup: 0x60,
		},
		{
			name:      "v0 zero EOF is accepted",
			version:   Version0,
			order:     binary.LittleEndian,
			eof:       0,
			rootGroup: 0x60,
		},
		{
			name:      "v0 EOF at header boundary is accepted",
			version:   Version0,
			order:     binary.LittleEndian,
			eof:       minSuperblockSizeV0,
			rootGroup: 0x60,
		},
		{
			name:      "v2 little-endian",
			version:   Version2,
			order:     binary.LittleEndian,
			eof:       0x850,
			rootGroup: 0x48,
		},
		{
			name:      "v2 big-endian",
			version:   Version2,
			order:     binary.BigEndian,
			eof:       0x850,
			rootGroup: 0x48,
		},
		{
			name:      "v2 zero EOF is accepted",
			version:   Version2,
			order:     binary.LittleEndian,
			eof:       0,
			rootGroup: 0x48,
		},
		{
			name:      "v2 EOF at header boundary is accepted",
			version:   Version2,
			order:     binary.LittleEndian,
			eof:       minSuperblockSizeV2,
			rootGroup: 0x48,
		},
		{
			name:      "v3 little-endian",
			version:   Version3,
			order:     binary.LittleEndian,
			eof:       0x1000,
			rootGroup: 0x80,
		},
		{
			name:      "v3 big-endian",
			version:   Version3,
			order:     binary.BigEndian,
			eof:       0x1000,
			rootGroup: 0x80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildSuperblockImage(t, tt.version, tt.order, tt.eof, tt.rootGroup)

			sb, err := ReadSuperblock(bytes.NewReader(data))
			require.NoError(t, err)
			require.Equal(t, tt.version, sb.Version)
			require.Equal(t, tt.eof, sb.EOFAddress, "EOFAddress mismatch")
			require.Equal(t, tt.rootGroup, sb.RootGroup, "RootGroup mismatch")
			require.Equal(t, tt.order, sb.Endianness)
		})
	}
}

// TestReadSuperblockEOFAddressTooSmall verifies that an EOF address smaller
// than the superblock header itself is rejected as corruption.
func TestReadSuperblockEOFAddressTooSmall(t *testing.T) {
	tests := []struct {
		name    string
		version uint8
		order   binary.ByteOrder
		eof     uint64
	}{
		{
			name:    "v0 EOF inside header",
			version: Version0,
			order:   binary.LittleEndian,
			eof:     minSuperblockSizeV0 - 1,
		},
		{
			name:    "v0 EOF far too small",
			version: Version0,
			order:   binary.LittleEndian,
			eof:     16,
		},
		{
			name:    "v2 EOF inside header",
			version: Version2,
			order:   binary.LittleEndian,
			eof:     16,
		},
		{
			name:    "v2 big-endian EOF inside header",
			version: Version2,
			order:   binary.BigEndian,
			eof:     minSuperblockSizeV2 - 1,
		},
		{
			name:    "v3 EOF inside header",
			version: Version3,
			order:   binary.LittleEndian,
			eof:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildSuperblockImage(t, tt.version, tt.order, tt.eof, 0x48)

			_, err := ReadSuperblock(bytes.NewReader(data))
			require.Error(t, err)
			require.Contains(t, err.Error(), "EOF address")
			require.Contains(t, err.Error(), "smaller than the superblock header")
		})
	}
}

// TestReadSuperblockEOFAddressFixtures verifies that real files yield their
// recorded End-of-File address while the root group still parses correctly.
func TestReadSuperblockEOFAddressFixtures(t *testing.T) {
	tests := []struct {
		file      string
		version   uint8
		rootGroup uint64
	}{
		{file: "../../testdata/v0.h5", version: Version0, rootGroup: 0x60},
		{file: "../../testdata/v2.h5", version: Version2, rootGroup: 0x30},
		{file: "../../testdata/v3.h5", version: Version3, rootGroup: 0x30},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			f, err := os.Open(tt.file)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			fi, err := f.Stat()
			require.NoError(t, err)

			sb, err := ReadSuperblock(f)
			require.NoError(t, err)
			require.Equal(t, tt.version, sb.Version)
			require.Equal(t, tt.rootGroup, sb.RootGroup)
			// A properly closed file records its physical size as the EOF address.
			require.Equal(t, uint64(fi.Size()), sb.EOFAddress)
		})
	}
}

// TestMinimumSuperblockSize verifies the per-version header sizes used by the
// EOF address sanity check.
func TestMinimumSuperblockSize(t *testing.T) {
	require.Equal(t, uint64(96), minimumSuperblockSize(Version0))
	require.Equal(t, uint64(48), minimumSuperblockSize(Version2))
	require.Equal(t, uint64(48), minimumSuperblockSize(Version3))
}
