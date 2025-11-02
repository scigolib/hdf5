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
	binary.LittleEndian.PutUint32(header[8:12], 48) // Header size (16 header + 16 msg1 + 16 msg2)
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
		OffsetSize: 8,
		LengthSize: 8,
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
		OffsetSize: 8,
		LengthSize: 8,
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
	binary.LittleEndian.PutUint32(header[8:12], 40) // Header size (16 header + 8 empty msg + 16 name msg)

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
		OffsetSize: 8,
		LengthSize: 8,
	}

	messages, _, err := parseV1Header(bytes.NewReader(header), 0, sb)
	require.NoError(t, err)
	// Should only have 1 message (the name), zero-size skipped
	require.Len(t, messages, 1)
}

func TestParseV1Header_WithContinuation(t *testing.T) {
	// Test parsing a v1 header with a continuation block
	// Main header will contain 2 messages: Name + Continuation
	// Continuation block will contain 1 message: Dataspace

	const contBlockAddr = 0x1000
	const contBlockSize = 32

	// Build the main header
	mainHeader := make([]byte, 128)
	offset := 0

	// Header prefix (16 bytes)
	mainHeader[0] = 1                                   // Version
	mainHeader[1] = 0                                   // Reserved
	binary.LittleEndian.PutUint16(mainHeader[2:4], 2)   // 2 messages
	binary.LittleEndian.PutUint32(mainHeader[4:8], 1)   // Ref count
	binary.LittleEndian.PutUint32(mainHeader[8:12], 64) // Header size (16 + 24 + 24)
	offset = 16

	// Message 1: Name (type 13, size 5)
	binary.LittleEndian.PutUint16(mainHeader[offset:offset+2], 13)  // Type: Name
	binary.LittleEndian.PutUint16(mainHeader[offset+2:offset+4], 5) // Size: 5
	mainHeader[offset+4] = 0                                        // Flags
	offset += 8
	copy(mainHeader[offset:], "test\x00")
	offset += 5
	offset += 3 // Padding to 8-byte boundary (8+5=13, pad to 16)

	// Message 2: Continuation (type 16, size 16 for 8-byte address + 8-byte size)
	binary.LittleEndian.PutUint16(mainHeader[offset:offset+2], 16)   // Type: Continuation
	binary.LittleEndian.PutUint16(mainHeader[offset+2:offset+4], 16) // Size: 16 bytes
	mainHeader[offset+4] = 0                                         // Flags
	offset += 8
	// Continuation message data: address + size
	binary.LittleEndian.PutUint64(mainHeader[offset:offset+8], contBlockAddr)
	binary.LittleEndian.PutUint64(mainHeader[offset+8:offset+16], contBlockSize)

	// Build the continuation block (starts at contBlockAddr)
	contBlock := make([]byte, contBlockSize)
	contOffset := 0

	// Message in continuation: Dataspace (type 1, size 8)
	binary.LittleEndian.PutUint16(contBlock[contOffset:contOffset+2], 1)   // Type: Dataspace
	binary.LittleEndian.PutUint16(contBlock[contOffset+2:contOffset+4], 8) // Size: 8 bytes
	contBlock[contOffset+4] = 0                                            // Flags
	contOffset += 8
	// Dataspace data (8 bytes - simple scalar)
	contBlock[contOffset] = 1   // Version
	contBlock[contOffset+1] = 0 // Dimensionality: 0 = scalar

	// Combine main header and continuation block into a single buffer
	// This simulates them being in the same file
	fullFile := make([]byte, contBlockAddr+contBlockSize)
	copy(fullFile[0:], mainHeader)
	copy(fullFile[contBlockAddr:], contBlock)

	sb := &Superblock{
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	messages, name, err := parseV1Header(bytes.NewReader(fullFile), 0, sb)
	require.NoError(t, err)
	require.Equal(t, "test", name)

	// Should have 3 messages total: Name, Continuation, Dataspace
	require.Len(t, messages, 3)
	require.Equal(t, MsgName, messages[0].Type)
	require.Equal(t, MsgContinuation, messages[1].Type)
	require.Equal(t, MsgDataspace, messages[2].Type)
}

func TestParseV1Header_ChainedContinuations(t *testing.T) {
	// Test parsing a v1 header with chained continuation blocks
	// Main header -> Continuation 1 -> Continuation 2
	// This tests that we properly follow continuation chains

	const cont1Addr = 0x1000
	const cont1Size = 64
	const cont2Addr = 0x2000
	const cont2Size = 32

	// Build main header
	mainHeader := make([]byte, 128)
	mainHeader[0] = 1
	mainHeader[1] = 0
	binary.LittleEndian.PutUint16(mainHeader[2:4], 1)   // 1 message
	binary.LittleEndian.PutUint32(mainHeader[4:8], 1)   // Ref count
	binary.LittleEndian.PutUint32(mainHeader[8:12], 40) // Header size
	offset := 16

	// Message: Continuation to cont1
	binary.LittleEndian.PutUint16(mainHeader[offset:offset+2], 16)
	binary.LittleEndian.PutUint16(mainHeader[offset+2:offset+4], 16)
	mainHeader[offset+4] = 0
	offset += 8
	binary.LittleEndian.PutUint64(mainHeader[offset:offset+8], cont1Addr)
	binary.LittleEndian.PutUint64(mainHeader[offset+8:offset+16], cont1Size)

	// Build continuation block 1 (contains Name + Continuation to cont2)
	cont1Block := make([]byte, cont1Size)
	offset = 0
	// Message 1: Name
	binary.LittleEndian.PutUint16(cont1Block[offset:offset+2], 13)
	binary.LittleEndian.PutUint16(cont1Block[offset+2:offset+4], 4)
	cont1Block[offset+4] = 0
	offset += 8
	copy(cont1Block[offset:], "obj\x00")
	offset += 4
	offset += 4 // Padding to 8-byte (8+4=12, pad to 16)

	// Message 2: Continuation to cont2
	binary.LittleEndian.PutUint16(cont1Block[offset:offset+2], 16)
	binary.LittleEndian.PutUint16(cont1Block[offset+2:offset+4], 16)
	cont1Block[offset+4] = 0
	offset += 8
	binary.LittleEndian.PutUint64(cont1Block[offset:offset+8], cont2Addr)
	binary.LittleEndian.PutUint64(cont1Block[offset+8:offset+16], cont2Size)

	// Build continuation block 2 (contains Dataspace)
	cont2Block := make([]byte, cont2Size)
	binary.LittleEndian.PutUint16(cont2Block[0:2], 1) // Type: Dataspace
	binary.LittleEndian.PutUint16(cont2Block[2:4], 8) // Size
	cont2Block[4] = 0                                 // Flags
	cont2Block[8] = 1                                 // Version
	cont2Block[9] = 0                                 // Dimensionality

	// Combine all blocks into a single file
	fullFile := make([]byte, cont2Addr+cont2Size)
	copy(fullFile[0:], mainHeader)
	copy(fullFile[cont1Addr:], cont1Block)
	copy(fullFile[cont2Addr:], cont2Block)

	sb := &Superblock{
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	messages, name, err := parseV1Header(bytes.NewReader(fullFile), 0, sb)
	require.NoError(t, err)
	require.Equal(t, "obj", name)

	// Should have 4 messages: 2 continuations + Name + Dataspace
	require.Len(t, messages, 4)
	require.Equal(t, MsgContinuation, messages[0].Type)
	require.Equal(t, MsgName, messages[1].Type)
	require.Equal(t, MsgContinuation, messages[2].Type)
	require.Equal(t, MsgDataspace, messages[3].Type)
}

func TestParseContinuationMessage(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		offsetSize uint8
		lengthSize uint8
		wantAddr   uint64
		wantSize   uint64
		wantErr    bool
	}{
		{
			name:       "8-byte address, 8-byte size",
			data:       []byte{0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offsetSize: 8,
			lengthSize: 8,
			wantAddr:   0x1000,
			wantSize:   0x80,
			wantErr:    false,
		},
		{
			name:       "4-byte address, 4-byte size",
			data:       []byte{0x00, 0x20, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00},
			offsetSize: 4,
			lengthSize: 4,
			wantAddr:   0x2000,
			wantSize:   0x40,
			wantErr:    false,
		},
		{
			name:       "2-byte address, 2-byte size",
			data:       []byte{0x00, 0x05, 0x20, 0x00},
			offsetSize: 2,
			lengthSize: 2,
			wantAddr:   0x500,
			wantSize:   0x20,
			wantErr:    false,
		},
		{
			name:       "data too small",
			data:       []byte{0x00, 0x10},
			offsetSize: 8,
			lengthSize: 8,
			wantErr:    true,
		},
		{
			name:       "zero size (invalid)",
			data:       []byte{0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offsetSize: 8,
			lengthSize: 8,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := &Superblock{
				Endianness: binary.LittleEndian,
				OffsetSize: tt.offsetSize,
				LengthSize: tt.lengthSize,
			}

			result, err := parseContinuationMessage(tt.data, sb)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantAddr, result.Address)
				require.Equal(t, tt.wantSize, result.Size)
			}
		})
	}
}

func TestParseV1Header_LargeHeaderWithContinuation(t *testing.T) {
	// Test a realistic scenario: large object header that needs continuation
	// Main header contains many messages, last one is continuation
	const contBlockAddr = 0x5000
	const contBlockSize = 256

	mainHeader := make([]byte, 512)
	mainHeader[0] = 1
	mainHeader[1] = 0
	binary.LittleEndian.PutUint16(mainHeader[2:4], 5)    // 5 messages
	binary.LittleEndian.PutUint32(mainHeader[4:8], 1)    // Ref count
	binary.LittleEndian.PutUint32(mainHeader[8:12], 160) // Header size
	offset := 16

	// Add 4 attribute messages (simplified)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint16(mainHeader[offset:offset+2], 12) // Type: Attribute
		binary.LittleEndian.PutUint16(mainHeader[offset+2:offset+4], 8)
		mainHeader[offset+4] = 0
		offset += 8
		// Attribute data (dummy 8 bytes)
		for j := 0; j < 8; j++ {
			mainHeader[offset+j] = byte(i)
		}
		offset += 8
	}

	// Last message: Continuation
	binary.LittleEndian.PutUint16(mainHeader[offset:offset+2], 16)
	binary.LittleEndian.PutUint16(mainHeader[offset+2:offset+4], 16)
	mainHeader[offset+4] = 0
	offset += 8
	binary.LittleEndian.PutUint64(mainHeader[offset:offset+8], contBlockAddr)
	binary.LittleEndian.PutUint64(mainHeader[offset+8:offset+16], contBlockSize)

	// Continuation block with more messages
	contBlock := make([]byte, contBlockSize)
	contOffset := 0

	// Add Name message
	binary.LittleEndian.PutUint16(contBlock[contOffset:contOffset+2], 13)
	binary.LittleEndian.PutUint16(contBlock[contOffset+2:contOffset+4], 10)
	contBlock[contOffset+4] = 0
	contOffset += 8
	copy(contBlock[contOffset:], "largeobj\x00\x00")
	contOffset += 10
	contOffset += 6 // Padding

	// Add Dataspace
	binary.LittleEndian.PutUint16(contBlock[contOffset:contOffset+2], 1)
	binary.LittleEndian.PutUint16(contBlock[contOffset+2:contOffset+4], 8)
	contBlock[contOffset+4] = 0
	contOffset += 8
	contBlock[contOffset] = 1
	contBlock[contOffset+1] = 1 // 1D

	// Combine
	fullFile := make([]byte, contBlockAddr+contBlockSize)
	copy(fullFile[0:], mainHeader)
	copy(fullFile[contBlockAddr:], contBlock)

	sb := &Superblock{
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	messages, name, err := parseV1Header(bytes.NewReader(fullFile), 0, sb)
	require.NoError(t, err)
	require.Equal(t, "largeobj", name)

	// Should have 7 messages: 4 attributes + 1 continuation + 1 name + 1 dataspace
	require.Len(t, messages, 7)

	// Count message types
	attrCount := 0
	contCount := 0
	nameCount := 0
	dsCount := 0

	for _, msg := range messages {
		switch msg.Type {
		case MsgAttribute:
			attrCount++
		case MsgContinuation:
			contCount++
		case MsgName:
			nameCount++
		case MsgDataspace:
			dsCount++
		}
	}

	require.Equal(t, 4, attrCount)
	require.Equal(t, 1, contCount)
	require.Equal(t, 1, nameCount)
	require.Equal(t, 1, dsCount)
}
