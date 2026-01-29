package core

import (
	"bytes"
	"compress/zlib"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyDeflate tests GZIP/deflate decompression.
func TestApplyDeflate(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid compressed data",
			input:   zlibCompress(t, []byte("hello world")),
			want:    []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "empty data",
			input:   zlibCompress(t, []byte{}),
			want:    []byte{},
			wantErr: false,
		},
		{
			name:    "large data",
			input:   zlibCompress(t, bytes.Repeat([]byte("test"), 1000)),
			want:    bytes.Repeat([]byte("test"), 1000),
			wantErr: false,
		},
		{
			name:    "invalid compressed data",
			input:   []byte{0x00, 0x01, 0x02, 0x03},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "truncated compressed data",
			input:   zlibCompress(t, []byte("hello"))[:5],
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyDeflate(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplyShuffle tests shuffle filter reversal.
func TestApplyShuffle(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		clientData []uint32
		want       []byte
		wantErr    bool
	}{
		{
			name: "4-byte elements",
			// Shuffled: [byte0 of all elements][byte1 of all elements]...
			data:       []byte{0x01, 0x02, 0x03, 0xAA, 0xBB, 0xCC, 0x11, 0x22, 0x33, 0xDD, 0xEE, 0xFF},
			clientData: []uint32{4},
			// Unshuffled: element0[byte0,byte1,byte2,byte3], element1[...], element2[...]
			want:    []byte{0x01, 0xAA, 0x11, 0xDD, 0x02, 0xBB, 0x22, 0xEE, 0x03, 0xCC, 0x33, 0xFF},
			wantErr: false,
		},
		{
			name:       "2-byte elements",
			data:       []byte{0x01, 0x02, 0x03, 0xAA, 0xBB, 0xCC},
			clientData: []uint32{2},
			want:       []byte{0x01, 0xAA, 0x02, 0xBB, 0x03, 0xCC},
			wantErr:    false,
		},
		{
			name:       "single element",
			data:       []byte{0x01, 0x02, 0x03, 0x04},
			clientData: []uint32{4},
			want:       []byte{0x01, 0x02, 0x03, 0x04},
			wantErr:    false,
		},
		{
			name:       "missing element size",
			data:       []byte{0x01, 0x02, 0x03, 0x04},
			clientData: []uint32{},
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "invalid element size (zero)",
			data:       []byte{0x01, 0x02, 0x03, 0x04},
			clientData: []uint32{0},
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "invalid element size (too large)",
			data:       []byte{0x01, 0x02, 0x03, 0x04},
			clientData: []uint32{100},
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "data size not multiple of element size",
			data:       []byte{0x01, 0x02, 0x03},
			clientData: []uint32{2},
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyShuffle(tt.data, tt.clientData)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplyFletcher32 tests Fletcher32 checksum handling.
func TestApplyFletcher32(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid data with checksum",
			data:    []byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0xBB, 0xCC, 0xDD},
			want:    []byte{0x01, 0x02, 0x03, 0x04},
			wantErr: false,
		},
		{
			name:    "minimum size (4 bytes)",
			data:    []byte{0xAA, 0xBB, 0xCC, 0xDD},
			want:    []byte{},
			wantErr: false,
		},
		{
			name:    "data too short",
			data:    []byte{0x01, 0x02, 0x03},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty data",
			data:    []byte{},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyFletcher32(tt.data)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplyFilter tests individual filter application.
func TestApplyFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  Filter
		data    []byte
		want    []byte
		wantErr bool
	}{
		{
			name: "deflate filter",
			filter: Filter{
				ID: FilterDeflate,
			},
			data:    zlibCompress(t, []byte("test data")),
			want:    []byte("test data"),
			wantErr: false,
		},
		{
			name: "shuffle filter",
			filter: Filter{
				ID:         FilterShuffle,
				ClientData: []uint32{2},
			},
			data:    []byte{0x01, 0x02, 0xAA, 0xBB},
			want:    []byte{0x01, 0xAA, 0x02, 0xBB},
			wantErr: false,
		},
		{
			name: "fletcher32 filter",
			filter: Filter{
				ID: FilterFletcher,
			},
			data:    []byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0xBB, 0xCC, 0xDD},
			want:    []byte{0x01, 0x02, 0x03, 0x04},
			wantErr: false,
		},
		{
			name: "unsupported SZIP filter",
			filter: Filter{
				ID: FilterSZIP,
			},
			data:    []byte{0x01, 0x02, 0x03, 0x04},
			want:    nil,
			wantErr: true,
		},
		{
			name: "unsupported unknown filter",
			filter: Filter{
				ID: FilterID(999),
			},
			data:    []byte{0x01, 0x02, 0x03, 0x04},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyFilter(tt.filter, tt.data)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestFilterPipelineApplyFilters tests full filter pipeline.
func TestFilterPipelineApplyFilters(t *testing.T) {
	tests := []struct {
		name     string
		pipeline *FilterPipelineMessage
		data     []byte
		want     []byte
		wantErr  bool
	}{
		{
			name:     "nil pipeline",
			pipeline: nil,
			data:     []byte{0x01, 0x02, 0x03},
			want:     []byte{0x01, 0x02, 0x03},
			wantErr:  false,
		},
		{
			name: "empty filters",
			pipeline: &FilterPipelineMessage{
				Filters: []Filter{},
			},
			data:    []byte{0x01, 0x02, 0x03},
			want:    []byte{0x01, 0x02, 0x03},
			wantErr: false,
		},
		{
			name: "single filter (deflate)",
			pipeline: &FilterPipelineMessage{
				Filters: []Filter{
					{ID: FilterDeflate},
				},
			},
			data:    zlibCompress(t, []byte("hello")),
			want:    []byte("hello"),
			wantErr: false,
		},
		{
			name: "multiple filters (shuffle + deflate)",
			pipeline: &FilterPipelineMessage{
				Filters: []Filter{
					{ID: FilterShuffle, ClientData: []uint32{2}},
					{ID: FilterDeflate},
				},
			},
			// Data is shuffled then compressed.
			// Decompression: first deflate, then shuffle.
			data:    zlibCompress(t, []byte{0x01, 0x02, 0xAA, 0xBB}),
			want:    []byte{0x01, 0xAA, 0x02, 0xBB},
			wantErr: false,
		},
		{
			name: "required filter failure (should error)",
			pipeline: &FilterPipelineMessage{
				Filters: []Filter{
					{ID: FilterID(999), Flags: 0x0000}, // Required
				},
			},
			data:    []byte{0x01, 0x02, 0x03},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.pipeline.ApplyFilters(tt.data)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplySZIP tests SZIP decompression error handling.
func TestApplySZIP(t *testing.T) {
	tests := []struct {
		name           string
		data           []byte
		wantErrContain []string
	}{
		{
			name: "SZIP compressed data",
			data: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			wantErrContain: []string{
				"libaec",
				"SZIP",
				"Golomb-Rice",
				"CCSDS",
				"GZIP",
			},
		},
		{
			name: "empty SZIP data",
			data: []byte{},
			wantErrContain: []string{
				"libaec",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applySZIP(tt.data)
			require.Error(t, err)

			errMsg := err.Error()
			for _, substr := range tt.wantErrContain {
				require.Contains(t, errMsg, substr,
					"error message should contain %q", substr)
			}
		})
	}
}

// zlibCompress compresses data using zlib (for tests).
func zlibCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write(data)
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)
	return buf.Bytes()
}
