package core

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockBytesWriterAt wraps bytes.Buffer to provide WriterAt interface.
type mockBytesWriterAt struct {
	buf []byte
}

func (m *mockBytesWriterAt) WriteAt(p []byte, off int64) (int, error) {
	needed := int(off) + len(p)
	if len(m.buf) < needed {
		newBuf := make([]byte, needed)
		copy(newBuf, m.buf)
		m.buf = newBuf
	}
	copy(m.buf[off:], p)
	return len(p), nil
}

func (m *mockBytesWriterAt) Bytes() []byte {
	return m.buf
}

// TestWriteToV1 tests writing an object header in v1 format.
func TestWriteToV1(t *testing.T) {
	// Create minimal object header
	header := &ObjectHeaderWriter{
		Version: 1,
		Messages: []MessageWriter{
			{
				Type: MsgDatatype,
				Data: make([]byte, 8),
			},
		},
	}

	writer := &mockBytesWriterAt{buf: make([]byte, 0, 1024)}

	n, err := header.writeToV1(writer, 0)
	require.NoError(t, err)
	require.Greater(t, n, uint64(0))
	require.Greater(t, len(writer.Bytes()), 0)
}

// TestWriteToV1_MultipleMessages tests writing with multiple messages.
func TestWriteToV1_MultipleMessages(t *testing.T) {
	header := &ObjectHeaderWriter{
		Version: 1,
		Messages: []MessageWriter{
			{
				Type: MsgDatatype,
				Data: make([]byte, 8),
			},
			{
				Type: MsgDataspace,
				Data: make([]byte, 16),
			},
			{
				Type: MsgDataLayout,
				Data: make([]byte, 18),
			},
		},
	}

	writer := &mockBytesWriterAt{buf: make([]byte, 0, 1024)}

	n, err := header.writeToV1(writer, 0)
	require.NoError(t, err)
	require.Greater(t, n, uint64(0))
}

var _ io.WriterAt = (*mockBytesWriterAt)(nil)
