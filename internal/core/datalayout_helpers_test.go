package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsContiguous tests contiguous layout detection.
func TestIsContiguous(t *testing.T) {
	tests := []struct {
		name string
		dl   *DataLayoutMessage
		want bool
	}{
		{
			name: "contiguous layout",
			dl: &DataLayoutMessage{
				Class: LayoutContiguous,
			},
			want: true,
		},
		{
			name: "compact layout",
			dl: &DataLayoutMessage{
				Class: LayoutCompact,
			},
			want: false,
		},
		{
			name: "chunked layout",
			dl: &DataLayoutMessage{
				Class: LayoutChunked,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dl.IsContiguous()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsCompact tests compact layout detection.
func TestIsCompact(t *testing.T) {
	tests := []struct {
		name string
		dl   *DataLayoutMessage
		want bool
	}{
		{
			name: "compact layout",
			dl: &DataLayoutMessage{
				Class: LayoutCompact,
			},
			want: true,
		},
		{
			name: "contiguous layout",
			dl: &DataLayoutMessage{
				Class: LayoutContiguous,
			},
			want: false,
		},
		{
			name: "chunked layout",
			dl: &DataLayoutMessage{
				Class: LayoutChunked,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dl.IsCompact()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIsChunked tests chunked layout detection.
func TestIsChunked(t *testing.T) {
	tests := []struct {
		name string
		dl   *DataLayoutMessage
		want bool
	}{
		{
			name: "chunked layout",
			dl: &DataLayoutMessage{
				Class: LayoutChunked,
			},
			want: true,
		},
		{
			name: "contiguous layout",
			dl: &DataLayoutMessage{
				Class: LayoutContiguous,
			},
			want: false,
		},
		{
			name: "compact layout",
			dl: &DataLayoutMessage{
				Class: LayoutCompact,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dl.IsChunked()
			require.Equal(t, tt.want, got)
		})
	}
}

// TestDataLayoutString tests String() method.
func TestDataLayoutString(t *testing.T) {
	tests := []struct {
		name string
		dl   *DataLayoutMessage
		want string
	}{
		{
			name: "compact layout",
			dl: &DataLayoutMessage{
				Class:    LayoutCompact,
				DataSize: 100,
			},
			want: "compact (size=100)",
		},
		{
			name: "contiguous layout",
			dl: &DataLayoutMessage{
				Class:       LayoutContiguous,
				DataAddress: 0x1000,
				DataSize:    256,
			},
			want: "contiguous (address=0x1000, size=256)",
		},
		{
			name: "chunked layout",
			dl: &DataLayoutMessage{
				Class:     LayoutChunked,
				ChunkSize: []uint32{10, 20, 30},
			},
			want: "chunked (chunks=[10 20 30])",
		},
		{
			name: "virtual layout",
			dl: &DataLayoutMessage{
				Class: LayoutVirtual,
			},
			want: "virtual",
		},
		{
			name: "unknown layout",
			dl: &DataLayoutMessage{
				Class: 99,
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dl.String()
			require.Equal(t, tt.want, got)
		})
	}
}
