package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	mocktesting "github.com/scigolib/hdf5/internal/testing"
	"github.com/stretchr/testify/require"
)

func TestReadObjectHeader(t *testing.T) {
	// Valid object header v2 with proper structure
	data := []byte{
		// Signature "OHDR" (4 bytes)
		'O', 'H', 'D', 'R',
		// Version (1 byte) + flags (1 byte)
		// Flags: 0x02 (bits 0-1 = 10 = 2) means 4-byte chunk size field
		0x02, 0x02,
		// Chunk size (4 bytes LE) - 21 bytes of messages (12 + 9)
		0x15, 0x00, 0x00, 0x00,
		// Message 1: Dataspace
		// Type (1) + Size (2 LE) + Flags (1) = 4 byte header
		0x01,       // Type: Dataspace
		0x08, 0x00, // Size: 8 bytes (little-endian)
		0x00, // Flags
		// Dataspace data (8 bytes)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Message 2: Name
		// Type (1) + Size (2 LE) + Flags (1) = 4 byte header
		0x0D,       // Type: Name (0x0D = 13)
		0x05, 0x00, // Size: 5 bytes (little-endian)
		0x00, // Flags
		// Name data: version(1) + "test"(4) = 5 bytes
		0x00, 't', 'e', 's', 't',
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	header, err := ReadObjectHeader(bytes.NewReader(data), 0, sb)
	require.NoError(t, err)
	require.Equal(t, uint8(2), header.Version)
	require.Equal(t, "test", header.Name)
	require.Len(t, header.Messages, 2)
	require.Equal(t, MsgDataspace, header.Messages[0].Type)
	require.Equal(t, MsgName, header.Messages[1].Type)
}

func TestRealFileObjectHeaders(t *testing.T) {
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

			// Read superblock to get root group address
			sb, err := ReadSuperblock(f)
			require.NoError(t, err)

			// Verify root group address is valid
			fi, err := f.Stat()
			require.NoError(t, err)
			require.True(t, sb.RootGroup < uint64(fi.Size()),
				"root group address %d beyond file size %d", sb.RootGroup, fi.Size())

			// Read root group object header
			header, err := ReadObjectHeader(f, sb.RootGroup, sb)
			require.NoError(t, err)
			require.Equal(t, ObjectTypeGroup, header.Type)

			// Root group may have empty name (it's "/" which is implicit)
			// So we don't require header.Name to be non-empty
			// Just verify header was parsed successfully
			require.NotNil(t, header.Messages)
		})
	}
}

func TestObjectHeaderBoundaryCheck(t *testing.T) {
	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	// Create a small buffer (100 bytes)
	mockFile := mocktesting.NewMockReaderAt(make([]byte, 100))

	// Try to read object header near end of file (should fail with short read)
	_, err := ReadObjectHeader(mockFile, 95, sb)
	require.Error(t, err)
	// The actual error is "short read" when buffer is too small
	require.Contains(t, err.Error(), "read")
}
