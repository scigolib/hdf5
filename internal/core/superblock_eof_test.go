package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildSuperblockImage encodes a superblock with 8-byte addresses and lengths
// in the layout the C library writes (H5F__cache_superblock_serialize,
// H5Fsuper_cache.c:676-760). HDF5 metadata is always little-endian, so the
// image has no byte-order choice (the format has no byte-order field).
func buildSuperblockImage(t *testing.T, version uint8, eof, rootGroup uint64) []byte {
	t.Helper()

	size := minSuperblockSizeV2
	if version == Version0 {
		size = minSuperblockSizeV0
	}

	// The stored end-of-file address is an absolute offset, so the image must
	// contain at least that many bytes: a header claiming more than the file has
	// is truncated and is rejected before these assertions run.
	if eof > uint64(size) {
		size = int(eof)
	}

	if version == Version0 {
		f := defaultV0(8, 8)
		f.eof = eof
		f.root = rootGroup
		return buildSuperblockV0(f, size)
	}

	return buildSuperblockV2(sbV2Fields{
		version: version, addr: 8, size: 8,
		ext: ^uint64(0), eof: eof, root: rootGroup,
	}, size)
}

// TestReadSuperblockEOFAddress verifies that the End-of-File address is parsed
// for superblock versions 0, 2 and 3, including the zero and minimum-size boundary values.
func TestReadSuperblockEOFAddress(t *testing.T) {
	tests := []struct {
		name      string
		version   uint8
		eof       uint64
		rootGroup uint64
	}{
		{
			name:      "v0 little-endian",
			version:   Version0,
			eof:       0x818,
			rootGroup: 0x60,
		},
		{
			name:      "v0 zero EOF is accepted",
			version:   Version0,
			eof:       0,
			rootGroup: 0x60,
		},
		{
			name:      "v0 EOF at header boundary is accepted",
			version:   Version0,
			eof:       minSuperblockSizeV0,
			rootGroup: 0x60,
		},
		{
			name:      "v2 little-endian",
			version:   Version2,
			eof:       0x850,
			rootGroup: 0x48,
		},
		{
			name:      "v2 zero EOF is accepted",
			version:   Version2,
			eof:       0,
			rootGroup: 0x48,
		},
		{
			name:      "v2 EOF at header boundary is accepted",
			version:   Version2,
			eof:       minSuperblockSizeV2,
			rootGroup: 0x48,
		},
		{
			name:      "v3 little-endian",
			version:   Version3,
			eof:       0x1000,
			rootGroup: 0x80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildSuperblockImage(t, tt.version, tt.eof, tt.rootGroup)

			sb, err := ReadSuperblock(bytes.NewReader(data))
			require.NoError(t, err)
			require.Equal(t, tt.version, sb.Version)
			require.Equal(t, tt.eof, sb.EOFAddress, "EOFAddress mismatch")
			require.Equal(t, tt.rootGroup, sb.RootGroup, "RootGroup mismatch")
			require.Equal(t, binary.LittleEndian, sb.Endianness)
		})
	}
}

// TestReadSuperblockEOFAddressTooSmall verifies that an EOF address smaller
// than the superblock header itself is rejected as corruption.
func TestReadSuperblockEOFAddressTooSmall(t *testing.T) {
	tests := []struct {
		name    string
		version uint8
		eof     uint64
	}{
		{
			name:    "v0 EOF inside header",
			version: Version0,
			eof:     minSuperblockSizeV0 - 1,
		},
		{
			name:    "v0 EOF far too small",
			version: Version0,
			eof:     16,
		},
		{
			name:    "v2 EOF inside header",
			version: Version2,
			eof:     16,
		},
		{
			name:    "v2 EOF one byte inside header",
			version: Version2,
			eof:     minSuperblockSizeV2 - 1,
		},
		{
			name:    "v3 EOF inside header",
			version: Version3,
			eof:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildSuperblockImage(t, tt.version, tt.eof, 0x48)

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
	require.Equal(t, uint64(96), minimumSuperblockSize(Version0, 8, 8))
	require.Equal(t, uint64(72), minimumSuperblockSize(Version0, 4, 4))
	// The root entry's link name offset is a length (H5G_SIZEOF_ENTRY).
	require.Equal(t, uint64(92), minimumSuperblockSize(Version0, 8, 4))
	require.Equal(t, uint64(76), minimumSuperblockSize(Version0, 4, 8))
	require.Equal(t, uint64(48), minimumSuperblockSize(Version2, 8, 8))
	require.Equal(t, uint64(48), minimumSuperblockSize(Version3, 8, 8))
	// Lengths do not appear in a version 2/3 superblock.
	require.Equal(t, uint64(32), minimumSuperblockSize(Version2, 4, 8))
}
