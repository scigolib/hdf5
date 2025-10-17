//nolint:gocritic // commentedOutCode: inline test comments are not actual commented-out code
package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseV1Header_Basic(t *testing.T) {
	// Create a minimal v1 object header
	// Format:
	// Byte 0: Version (1)
	// Byte 1: Reserved (0)
	// Bytes 2-3: Number of messages (2)
	// Bytes 4-7: Reference count (1)
	// Bytes 8-11: Object header size (40 = 16 header + 24 messages)
	// Bytes 12-15: Padding
	// Then messages...

	header := make([]byte, 64)
	offset := 0

	// Header
	header[0] = 1                                   // Version
	header[1] = 0                                   // Reserved
	binary.LittleEndian.PutUint16(header[2:4], 2)   // 2 messages
	binary.LittleEndian.PutUint32(header[4:8], 1)   // Ref count
	binary.LittleEndian.PutUint32(header[8:12], 40) // Header size
	// Bytes 12-15: padding
	offset = 16

	// Message 1: Name message (type 13, size 5, "test" + null)
	binary.LittleEndian.PutUint16(header[offset:offset+2], 13)  // Type: Name
	binary.LittleEndian.PutUint16(header[offset+2:offset+4], 5) // Size: 5 bytes
	header[offset+4] = 0                                        // Flags
	// Bytes 5-7: reserved
	offset += 8
	copy(header[offset:], "test\x00") // Name data
	offset += 5
	// Pad to 8-byte boundary: 8 + 5 = 13, need 3 bytes padding to reach 16
	offset += 3

	// Message 2: Dataspace message (type 1, size 8)
	binary.LittleEndian.PutUint16(header[offset:offset+2], 1)   // Type: Dataspace
	binary.LittleEndian.PutUint16(header[offset+2:offset+4], 8) // Size: 8 bytes
	header[offset+4] = 0                                        // Flags
	offset += 8
	// Dataspace data (8 bytes - simple scalar)
	header[offset] = 1   // Version
	header[offset+1] = 0 // Dimensionality: 0 = scalar
	// Rest is padding

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	messages, name, err := parseV1Header(bytes.NewReader(header), 0, sb)
	require.NoError(t, err)
	require.Equal(t, "test", name)
	require.Len(t, messages, 2)
	require.Equal(t, MsgName, messages[0].Type)
	require.Equal(t, MsgDataspace, messages[1].Type)
}

func TestParseV1Header_SymbolTable(t *testing.T) {
	// Test parsing a group with symbol table message
	header := make([]byte, 128)
	offset := 0

	// Header
	header[0] = 1                                   // Version
	header[1] = 0                                   // Reserved
	binary.LittleEndian.PutUint16(header[2:4], 1)   // 1 message
	binary.LittleEndian.PutUint32(header[4:8], 1)   // Ref count
	binary.LittleEndian.PutUint32(header[8:12], 40) // Header size
	offset = 16

	// Message: Symbol Table (type 17, size 16)
	binary.LittleEndian.PutUint16(header[offset:offset+2], 17)   // Type: SymbolTable
	binary.LittleEndian.PutUint16(header[offset+2:offset+4], 16) // Size: 16 bytes
	header[offset+4] = 0                                         // Flags
	offset += 8

	// Symbol table message data:
	// Bytes 0-7: B-tree address
	// Bytes 8-15: Local heap address
	binary.LittleEndian.PutUint64(header[offset:offset+8], 0x100)
	binary.LittleEndian.PutUint64(header[offset+8:offset+16], 0x200)

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	messages, name, err := parseV1Header(bytes.NewReader(header), 0, sb)
	require.NoError(t, err)
	require.Empty(t, name) // No name message
	require.Len(t, messages, 1)
	require.Equal(t, MsgSymbolTable, messages[0].Type)
	require.Len(t, messages[0].Data, 16)
}

func TestParseV1Header_EmptyMessage(t *testing.T) {
	// Test that zero-size messages are skipped
	header := make([]byte, 64)

	header[0] = 1
	header[1] = 0
	binary.LittleEndian.PutUint16(header[2:4], 2) // 2 messages
	binary.LittleEndian.PutUint32(header[4:8], 1)
	binary.LittleEndian.PutUint32(header[8:12], 32)

	offset := 16
	// Message 1: Zero size
	binary.LittleEndian.PutUint16(header[offset:offset+2], 0)
	binary.LittleEndian.PutUint16(header[offset+2:offset+4], 0)

	offset += 8
	// Message 2: Name
	binary.LittleEndian.PutUint16(header[offset:offset+2], 13)
	binary.LittleEndian.PutUint16(header[offset+2:offset+4], 3)
	offset += 8
	copy(header[offset:], "hi\x00")

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	messages, _, err := parseV1Header(bytes.NewReader(header), 0, sb)
	require.NoError(t, err)
	// Should only have 1 message (the name), zero-size skipped
	require.Len(t, messages, 1)
}
