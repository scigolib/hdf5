package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestObjectHeaderWriterSize tests Size() calculation for both v1 and v2.
func TestObjectHeaderWriterSize(t *testing.T) {
	tests := []struct {
		name     string
		version  uint8
		messages []MessageWriter
		want     uint64
		desc     string
	}{
		{
			name:     "v1 empty header",
			version:  1,
			messages: []MessageWriter{},
			want:     16, // Just header
			desc:     "16-byte header only",
		},
		{
			name:    "v1 single message",
			version: 1,
			messages: []MessageWriter{
				{
					Type: MsgDataspace,
					Data: make([]byte, 16), // 16 bytes data
				},
			},
			want: 40, // 16 (header) + 24 (8-byte msg header + 16 data, aligned to 24)
			desc: "16-byte header + 8-byte msg header + 16-byte data = 24, aligned to 24",
		},
		{
			name:    "v1 message requiring padding",
			version: 1,
			messages: []MessageWriter{
				{
					Type: MsgDataspace,
					Data: make([]byte, 10), // 10 bytes data
				},
			},
			want: 40, // 16 (header) + 24 (8 header + 10 data = 18, padded to 24)
			desc: "18 bytes (8+10) padded to 24",
		},
		{
			name:    "v1 two messages",
			version: 1,
			messages: []MessageWriter{
				{
					Type: MsgDataspace,
					Data: make([]byte, 8),
				},
				{
					Type: MsgDatatype,
					Data: make([]byte, 8),
				},
			},
			want: 48, // 16 (header) + 16 (msg1) + 16 (msg2)
			desc: "Each message = 8 + 8 = 16, total 16 + 16 + 16",
		},
		{
			name:     "v2 empty header",
			version:  2,
			messages: []MessageWriter{},
			want:     7, // Signature (4) + Version (1) + Flags (1) + Chunk Size (1)
			desc:     "7-byte header only",
		},
		{
			name:    "v2 single message",
			version: 2,
			messages: []MessageWriter{
				{
					Type: MsgDataspace,
					Data: make([]byte, 16), // 16 bytes data
				},
			},
			want: 27, // 7 (header) + 20 (Type 1 + Size 2 + Flags 1 + Data 16)
			desc: "7-byte header + 1+2+1+16 message",
		},
		{
			name:    "v2 two messages",
			version: 2,
			messages: []MessageWriter{
				{
					Type: MsgDataspace,
					Data: make([]byte, 8),
				},
				{
					Type: MsgDatatype,
					Data: make([]byte, 12),
				},
			},
			want: 35, // 7 + 12 (1+2+1+8) + 16 (1+2+1+12)
			desc: "7 + (1+2+1+8) + (1+2+1+12)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ohw := &ObjectHeaderWriter{
				Version:  tt.version,
				Messages: tt.messages,
			}
			got := ohw.Size()
			require.Equal(t, tt.want, got, tt.desc)
		})
	}
}

// TestObjectHeaderWriterSizePanic tests that invalid versions panic.
func TestObjectHeaderWriterSizePanic(t *testing.T) {
	ohw := &ObjectHeaderWriter{
		Version:  99, // Invalid version
		Messages: []MessageWriter{},
	}

	require.Panics(t, func() {
		_ = ohw.Size()
	}, "should panic on invalid version")
}

// TestSizeV1 tests v1 size calculation details.
func TestSizeV1(t *testing.T) {
	tests := []struct {
		name     string
		messages []MessageWriter
		want     uint64
	}{
		{
			name:     "no messages",
			messages: []MessageWriter{},
			want:     16,
		},
		{
			name: "message size 1 (padded to 16)",
			messages: []MessageWriter{
				{Type: MsgDataspace, Data: make([]byte, 1)},
			},
			want: 32, // 16 + 16 (8 header + 1 data, padded to 16)
		},
		{
			name: "message size 8 (exactly aligned)",
			messages: []MessageWriter{
				{Type: MsgDataspace, Data: make([]byte, 8)},
			},
			want: 32, // 16 + 16 (8 + 8 = 16, already aligned)
		},
		{
			name: "message size 9 (padded to 24)",
			messages: []MessageWriter{
				{Type: MsgDataspace, Data: make([]byte, 9)},
			},
			want: 40, // 16 + 24 (8 + 9 = 17, padded to 24)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ohw := &ObjectHeaderWriter{
				Version:  1,
				Messages: tt.messages,
			}
			got := ohw.sizeV1()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestSizeV2 tests v2 size calculation details.
func TestSizeV2(t *testing.T) {
	tests := []struct {
		name     string
		messages []MessageWriter
		want     uint64
	}{
		{
			name:     "no messages",
			messages: []MessageWriter{},
			want:     7, // Just header
		},
		{
			name: "single byte message",
			messages: []MessageWriter{
				{Type: MsgDataspace, Data: make([]byte, 1)},
			},
			want: 12, // 7 + 5 (1+2+1+1) but size field is 2 bytes, so 7 + 1+2+1+1 = 12
		},
		{
			name: "large message",
			messages: []MessageWriter{
				{Type: MsgDataspace, Data: make([]byte, 100)},
			},
			want: 111, // 7 + 104 (1+2+1+100)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ohw := &ObjectHeaderWriter{
				Version:  2,
				Messages: tt.messages,
			}
			got := ohw.sizeV2()
			require.Equal(t, tt.want, got)
		})
	}
}
