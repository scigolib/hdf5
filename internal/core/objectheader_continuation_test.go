package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPadToSize_V2_NoOp verifies that PadToSize is a no-op when the header is already large enough.
func TestPadToSize_V2_NoOp(t *testing.T) {
	ohw := &ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []MessageWriter{
			{Type: MsgSymbolTable, Data: make([]byte, 200)},
		},
	}

	sizeBefore := ohw.Size()
	ohw.PadToSize(100) // Less than current size.
	sizeAfter := ohw.Size()

	assert.Equal(t, sizeBefore, sizeAfter, "PadToSize should be a no-op when already >= minSize")
}

// TestPadToSize_V2_Pads verifies that PadToSize increases header size to at least the minimum.
func TestPadToSize_V2_Pads(t *testing.T) {
	ohw := &ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []MessageWriter{
			{Type: MsgSymbolTable, Data: make([]byte, 16)},
		},
	}

	sizeBefore := ohw.Size()
	require.Less(t, sizeBefore, uint64(256), "initial size should be < 256")

	ohw.PadToSize(256)
	sizeAfter := ohw.Size()

	assert.GreaterOrEqual(t, sizeAfter, uint64(256), "padded size should be >= 256")
	assert.Equal(t, len(ohw.Messages), 2, "should have added a null message")
	assert.Equal(t, MsgNil, ohw.Messages[1].Type, "padding message should be Nil type")
}

// TestPadToSize_V1 verifies padding works with v1 headers.
func TestPadToSize_V1(t *testing.T) {
	ohw := &ObjectHeaderWriter{
		Version:  1,
		Flags:    0,
		RefCount: 1,
		Messages: []MessageWriter{
			{Type: MsgSymbolTable, Data: make([]byte, 16)},
		},
	}

	sizeBefore := ohw.Size()
	ohw.PadToSize(256)
	sizeAfter := ohw.Size()

	assert.Greater(t, sizeAfter, sizeBefore, "v1 header should also be padded")
	assert.GreaterOrEqual(t, sizeAfter, uint64(256), "padded v1 size should be >= 256")
}

// TestEncodeContinuationMessage verifies encoding of continuation message data.
func TestEncodeContinuationMessage(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	ochkAddr := uint64(0x1000)
	ochkSize := uint64(64)

	data := EncodeContinuationMessage(ochkAddr, ochkSize, sb)
	require.Len(t, data, 16, "8-byte offset + 8-byte length = 16 bytes")

	// Verify decoding with the existing parseContinuationMessage.
	cont, err := parseContinuationMessage(data, sb)
	require.NoError(t, err)
	assert.Equal(t, ochkAddr, cont.Address, "address should match")
	assert.Equal(t, ochkSize, cont.Size, "size should match")
}

// TestEncodeContinuationMessage_SmallSizes tests with 4-byte offset/length.
func TestEncodeContinuationMessage_SmallSizes(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 4,
		LengthSize: 4,
		Endianness: binary.LittleEndian,
	}

	data := EncodeContinuationMessage(0x2000, 48, sb)
	require.Len(t, data, 8)

	cont, err := parseContinuationMessage(data, sb)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x2000), cont.Address)
	assert.Equal(t, uint64(48), cont.Size)
}

// TestWriteContinuationChunkV2 verifies that OCHK blocks are written correctly.
func TestWriteContinuationChunkV2(t *testing.T) {
	buf := make([]byte, 1024)
	w := &bytesWriterAt{buf: buf}

	messages := []MessageWriter{
		{Type: MsgAttribute, Data: []byte{1, 2, 3, 4, 5}},
	}

	address := uint64(100)
	size, err := WriteContinuationChunkV2(w, address, messages)
	require.NoError(t, err)

	// Expected size: "OCHK"(4) + type(1)+size(2)+flags(1)+data(5) + checksum(4) = 17
	expectedSize := uint64(4 + 1 + 2 + 1 + 5 + 4)
	assert.Equal(t, expectedSize, size, "OCHK block size")

	// Verify OCHK signature.
	assert.Equal(t, "OCHK", string(buf[100:104]))

	// Verify message type.
	assert.Equal(t, byte(MsgAttribute), buf[104])

	// Verify message size (little-endian uint16 = 5).
	assert.Equal(t, uint16(5), binary.LittleEndian.Uint16(buf[105:107]))

	// Verify message flags.
	assert.Equal(t, byte(0), buf[107])

	// Verify message data.
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, buf[108:113])

	// Verify checksum (Jenkins over "OCHK" + messages).
	expectedChecksum := JenkinsChecksum(buf[100:113])
	actualChecksum := binary.LittleEndian.Uint32(buf[113:117])
	assert.Equal(t, expectedChecksum, actualChecksum, "Jenkins checksum should match")
}

// TestContinuationChunkSizeV2 verifies size calculation.
func TestContinuationChunkSizeV2(t *testing.T) {
	messages := []MessageWriter{
		{Type: MsgAttribute, Data: make([]byte, 20)},
	}

	// "OCHK"(4) + type(1)+size(2)+flags(1)+data(20) + checksum(4) = 32
	assert.Equal(t, uint64(32), ContinuationChunkSizeV2(messages))
}

// TestContinuationChunkSizeV2_MultipleMessages tests size with multiple messages.
func TestContinuationChunkSizeV2_MultipleMessages(t *testing.T) {
	messages := []MessageWriter{
		{Type: MsgAttribute, Data: make([]byte, 10)},
		{Type: MsgAttribute, Data: make([]byte, 20)},
	}

	// "OCHK"(4) + 2*(type(1)+size(2)+flags(1)) + data(10+20) + checksum(4) = 42
	expected := uint64(4 + (1+2+1)*2 + 10 + 20 + 4)
	assert.Equal(t, expected, ContinuationChunkSizeV2(messages))
}

// TestParseV2ContinuationBlock verifies that V2 OCHK blocks can be read back.
func TestParseV2ContinuationBlock(t *testing.T) {
	// Build an OCHK block in memory.
	buf := make([]byte, 1024)
	w := &bytesWriterAt{buf: buf}

	messages := []MessageWriter{
		{Type: MsgAttribute, Data: []byte{0xAA, 0xBB, 0xCC}},
		{Type: MsgDataspace, Data: []byte{0x11, 0x22}},
	}

	addr := uint64(0)
	size, err := WriteContinuationChunkV2(w, addr, messages)
	require.NoError(t, err)

	// Now parse it back using the reader.
	r := bytes.NewReader(buf[:size])
	parsedMsgs, name, err := parseV2ContinuationBlock(r, addr, size, 0, false)
	require.NoError(t, err)
	assert.Empty(t, name)
	require.Len(t, parsedMsgs, 2)

	assert.Equal(t, MsgAttribute, parsedMsgs[0].Type)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, parsedMsgs[0].Data)

	assert.Equal(t, MsgDataspace, parsedMsgs[1].Type)
	assert.Equal(t, []byte{0x11, 0x22}, parsedMsgs[1].Data)
}

// TestParseV2ContinuationBlock_TooSmall tests error on tiny OCHK blocks.
func TestParseV2ContinuationBlock_TooSmall(t *testing.T) {
	buf := []byte("OCHK")
	r := bytes.NewReader(buf)
	_, _, err := parseV2ContinuationBlock(r, 0, 4, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small")
}

// TestParseV2ContinuationBlock_BadSignature tests error on invalid OCHK signature.
func TestParseV2ContinuationBlock_BadSignature(t *testing.T) {
	buf := make([]byte, 16)
	copy(buf, "XXXX")
	r := bytes.NewReader(buf)
	_, _, err := parseV2ContinuationBlock(r, 0, 16, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid OCHK signature")
}

// bytesWriterAt wraps a byte slice for io.WriterAt and io.ReaderAt.
type bytesWriterAt struct {
	buf []byte
}

func (b *bytesWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if int(off)+len(p) > len(b.buf) {
		// Extend buffer.
		newBuf := make([]byte, int(off)+len(p))
		copy(newBuf, b.buf)
		b.buf = newBuf
	}
	return copy(b.buf[off:], p), nil
}

func (b *bytesWriterAt) ReadAt(p []byte, off int64) (int, error) {
	return copy(p, b.buf[off:]), nil
}
