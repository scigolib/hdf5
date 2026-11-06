package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddMessageToObjectHeader_Success tests adding a message successfully.
func TestAddMessageToObjectHeader_Success(t *testing.T) {
	// Create a minimal object header v2
	oh := &ObjectHeader{
		Version:  2,
		Flags:    0,
		Type:     ObjectTypeDataset,
		Messages: []*HeaderMessage{},
	}

	// Create a small test message
	testMessage := []byte{0x01, 0x02, 0x03, 0x04}

	err := AddMessageToObjectHeader(oh, MsgAttribute, testMessage)
	require.NoError(t, err)

	// Verify message was added
	assert.Len(t, oh.Messages, 1)
	assert.Equal(t, MsgAttribute, oh.Messages[0].Type)
	assert.Equal(t, testMessage, oh.Messages[0].Data)
}

// TestAddMessageToObjectHeader_Multiple tests adding multiple messages.
func TestAddMessageToObjectHeader_Multiple(t *testing.T) {
	oh := &ObjectHeader{
		Version:  2,
		Flags:    0,
		Type:     ObjectTypeDataset,
		Messages: []*HeaderMessage{},
	}

	// Add 5 small messages
	for i := 0; i < 5; i++ {
		msg := []byte{byte(i)}
		err := AddMessageToObjectHeader(oh, MsgAttribute, msg)
		require.NoError(t, err)
	}

	assert.Len(t, oh.Messages, 5)
	for i := 0; i < 5; i++ {
		assert.Equal(t, byte(i), oh.Messages[i].Data[0])
	}
}

// TestAddMessageToObjectHeader_LargeMessage tests that large messages are accepted.
// Overflow handling is now the caller's responsibility (bounds check + continuation).
func TestAddMessageToObjectHeader_LargeMessage(t *testing.T) {
	oh := &ObjectHeader{
		Version:  2,
		Flags:    0,
		Type:     ObjectTypeDataset,
		Messages: []*HeaderMessage{},
	}

	// Large messages are now accepted -- the caller uses bounds checking
	// and continuation chunks to handle overflow.
	largeMessage := make([]byte, 252)
	err := AddMessageToObjectHeader(oh, MsgAttribute, largeMessage)
	require.NoError(t, err)
	assert.Len(t, oh.Messages, 1)
}

// TestAddMessageToObjectHeader_NilHeader tests error with nil header.
func TestAddMessageToObjectHeader_NilHeader(t *testing.T) {
	err := AddMessageToObjectHeader(nil, MsgAttribute, []byte{1, 2, 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object header is nil")
}

// TestAddMessageToObjectHeader_V1NotSupported tests error with v1 header.
func TestAddMessageToObjectHeader_V1NotSupported(t *testing.T) {
	oh := &ObjectHeader{
		Version:  1, // Version 1 not supported for modification
		Flags:    0,
		Type:     ObjectTypeDataset,
		Messages: []*HeaderMessage{},
	}

	err := AddMessageToObjectHeader(oh, MsgAttribute, []byte{1, 2, 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only object header version 2 is supported")
}
