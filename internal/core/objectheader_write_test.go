package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWriterAt implements io.WriterAt for testing.
type mockWriterAt struct {
	buf *bytes.Buffer
}

func newMockWriterAt() *mockWriterAt {
	return &mockWriterAt{
		buf: &bytes.Buffer{},
	}
}

func (m *mockWriterAt) WriteAt(p []byte, off int64) (int, error) {
	// Expand buffer if needed
	needed := int(off) + len(p)
	if m.buf.Len() < needed {
		m.buf.Grow(needed - m.buf.Len())
		for m.buf.Len() < needed {
			m.buf.WriteByte(0)
		}
	}

	// Write at offset
	bufBytes := m.buf.Bytes()
	copy(bufBytes[off:], p)
	m.buf.Reset()
	m.buf.Write(bufBytes)

	return len(p), nil
}

func (m *mockWriterAt) Bytes() []byte {
	return m.buf.Bytes()
}

func TestNewMinimalRootGroupHeader(t *testing.T) {
	header := NewMinimalRootGroupHeader()

	assert.Equal(t, uint8(2), header.Version, "Should be version 2")
	assert.Equal(t, uint8(0), header.Flags, "Should have minimal flags")
	assert.Len(t, header.Messages, 1, "Should have 1 message (Link Info)")

	// Verify Link Info message
	msg := header.Messages[0]
	assert.Equal(t, MsgLinkInfo, msg.Type)
	assert.Equal(t, 18, len(msg.Data), "Link Info should be 18 bytes")

	// Verify Link Info content
	assert.Equal(t, uint8(0), msg.Data[0], "Version should be 0")
	assert.Equal(t, uint8(0), msg.Data[1], "Flags should be 0 (compact)")

	// Verify UNDEF addresses
	heapAddr := binary.LittleEndian.Uint64(msg.Data[2:10])
	assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), heapAddr, "Heap address should be UNDEF")

	btreeAddr := binary.LittleEndian.Uint64(msg.Data[10:18])
	assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), btreeAddr, "B-tree address should be UNDEF")
}

func TestObjectHeaderWriter_WriteTo(t *testing.T) {
	tests := []struct {
		name          string
		header        *ObjectHeaderWriter
		address       uint64
		wantSize      uint64
		wantErr       bool
		validateBytes func(t *testing.T, data []byte)
	}{
		{
			name:    "minimal root group",
			header:  NewMinimalRootGroupHeader(),
			address: 48, // After superblock v2
			// Header: 4 (sig) + 1 (ver) + 1 (flags) + 1 (chunk size) = 7
			// Message: 1 (type) + 2 (size) + 1 (flags) + 18 (data) = 22
			// Total: 7 + 22 = 29
			wantSize: 29,
			wantErr:  false,
			validateBytes: func(t *testing.T, data []byte) {
				// Validate signature
				assert.Equal(t, "OHDR", string(data[0:4]), "Should have OHDR signature")

				// Validate version
				assert.Equal(t, uint8(2), data[4], "Should be version 2")

				// Validate flags
				assert.Equal(t, uint8(0), data[5], "Should have flags=0")

				// Validate chunk size
				assert.Equal(t, uint8(22), data[6], "Chunk size should be 22 (1+2+1+18)")

				// Validate message type (Link Info = 2)
				assert.Equal(t, uint8(2), data[7], "Message type should be 2 (Link Info)")

				// Validate message size
				msgSize := binary.LittleEndian.Uint16(data[8:10])
				assert.Equal(t, uint16(18), msgSize, "Message size should be 18")

				// Validate message flags
				assert.Equal(t, uint8(0), data[10], "Message flags should be 0")

				// Validate Link Info message content
				linkInfo := data[11:29]
				assert.Equal(t, uint8(0), linkInfo[0], "Link Info version should be 0")
				assert.Equal(t, uint8(0), linkInfo[1], "Link Info flags should be 0")

				heapAddr := binary.LittleEndian.Uint64(linkInfo[2:10])
				assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), heapAddr, "Heap address should be UNDEF")

				btreeAddr := binary.LittleEndian.Uint64(linkInfo[10:18])
				assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), btreeAddr, "B-tree address should be UNDEF")
			},
		},
		{
			name: "unsupported version",
			header: &ObjectHeaderWriter{
				Version:  1,
				Flags:    0,
				Messages: []MessageWriter{},
			},
			address: 0,
			wantErr: true,
		},
		{
			name: "chunk size too large for 1-byte encoding",
			header: &ObjectHeaderWriter{
				Version: 2,
				Flags:   0, // Bits 0-1 = 0 means 1-byte chunk size (max 255)
				Messages: []MessageWriter{
					{
						Type: MsgLinkInfo,
						// Test data size: 1 (type) + 2 (size) + 1 (flags) + 300 (data) = 304 bytes (exceeds 255 limit).
						Data: make([]byte, 300),
					},
				},
			},
			address: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := newMockWriterAt()

			size, err := tt.header.WriteTo(writer, tt.address)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSize, size, "Size mismatch")

			if tt.validateBytes != nil {
				data := writer.Bytes()
				require.GreaterOrEqual(t, len(data), int(tt.address+tt.wantSize),
					"Buffer should contain written data")

				headerData := data[tt.address : tt.address+tt.wantSize]
				tt.validateBytes(t, headerData)
			}
		})
	}
}

func TestObjectHeaderWriter_RoundTrip(t *testing.T) {
	// Create minimal root group header
	header := NewMinimalRootGroupHeader()

	// Write it
	writer := newMockWriterAt()
	address := uint64(48)
	size, err := header.WriteTo(writer, address)
	require.NoError(t, err)
	require.Greater(t, size, uint64(0))

	// Read it back
	data := writer.Bytes()
	reader := bytes.NewReader(data)

	// Note: We don't have a full superblock here, but we can create a minimal one
	sb := &Superblock{
		Version:    2,
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	readHeader, err := ReadObjectHeader(reader, address, sb)
	require.NoError(t, err)

	// Validate read header
	assert.Equal(t, uint8(2), readHeader.Version, "Version should match")
	assert.Equal(t, ObjectTypeGroup, readHeader.Type, "Should be detected as group")
	assert.NotEmpty(t, readHeader.Messages, "Should have messages")

	// Find Link Info message
	var foundLinkInfo bool
	for _, msg := range readHeader.Messages {
		if msg.Type == MsgLinkInfo {
			foundLinkInfo = true
			// Verify we can read the message data
			assert.NotEmpty(t, msg.Data, "Link Info message should have data")
			break
		}
	}

	assert.True(t, foundLinkInfo, "Should have Link Info message")
}

func TestObjectHeaderWriter_MultipleMessages(t *testing.T) {
	// Test with multiple messages (for future extension)
	header := &ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []MessageWriter{
			{
				Type: MsgLinkInfo,
				Data: make([]byte, 10),
			},
			{
				Type: MsgName,
				Data: []byte("test"),
			},
		},
	}

	writer := newMockWriterAt()
	size, err := header.WriteTo(writer, 0)
	require.NoError(t, err)

	// Expected size:
	// Header: 4+1+1+1 = 7
	// Message 1: 1+2+1+10 = 14
	// Message 2: 1+2+1+4 = 8
	// Total chunk: 14+8 = 22
	// Total: 7 + 22 = 29
	assert.Equal(t, uint64(29), size)

	data := writer.Bytes()

	// Validate chunk size
	assert.Equal(t, uint8(22), data[6], "Chunk size should be sum of messages")
}
