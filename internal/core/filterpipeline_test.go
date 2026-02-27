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

// TestApplyBZIP2 tests BZIP2 decompression.
func TestApplyBZIP2(t *testing.T) {
	// Pre-compressed bzip2 data generated with Python's bz2.compress().
	// Go stdlib has no bzip2 writer, so we use known-good compressed bytes.
	bzip2HelloWorld := []byte{
		0x42, 0x5A, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26,
		0x53, 0x59, 0x44, 0xF7, 0x13, 0x78, 0x00, 0x00,
		0x01, 0x91, 0x80, 0x40, 0x00, 0x06, 0x44, 0x90,
		0x80, 0x20, 0x00, 0x22, 0x03, 0x34, 0x84, 0x30,
		0x21, 0xB6, 0x81, 0x54, 0x27, 0x8B, 0xB9, 0x22,
		0x9C, 0x28, 0x48, 0x22, 0x7B, 0x89, 0xBC, 0x00,
	}

	bzip2Empty := []byte{
		0x42, 0x5A, 0x68, 0x39, 0x17, 0x72, 0x45, 0x38,
		0x50, 0x90, 0x00, 0x00, 0x00, 0x00,
	}

	bzip2AAAA := []byte{
		0x42, 0x5A, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26,
		0x53, 0x59, 0xE1, 0x6E, 0x65, 0x71, 0x00, 0x00,
		0x02, 0x44, 0x00, 0x40, 0x00, 0x20, 0x00, 0x20,
		0x00, 0x21, 0x00, 0x82, 0x0B, 0x17, 0x72, 0x45,
		0x38, 0x50, 0x90, 0xE1, 0x6E, 0x65, 0x71,
	}

	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid bzip2 hello world",
			input:   bzip2HelloWorld,
			want:    []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "valid bzip2 empty data",
			input:   bzip2Empty,
			want:    []byte{},
			wantErr: false,
		},
		{
			name:    "valid bzip2 repeated data",
			input:   bzip2AAAA,
			want:    []byte("AAAA"),
			wantErr: false,
		},
		{
			name:    "invalid bzip2 data",
			input:   []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			wantErr: true,
		},
		{
			name:    "truncated bzip2 header",
			input:   bzip2HelloWorld[:6],
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyBZIP2(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplyLZF tests LZF decompression via the applyLZF wrapper.
func TestApplyLZF(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name: "literal only - single byte",
			// Control byte 0x00 = literal run of 1 byte, followed by 0x42.
			input:   []byte{0x00, 0x42},
			want:    []byte{0x42},
			wantErr: false,
		},
		{
			name: "literal only - multiple bytes",
			// Control byte 0x04 = literal run of 5 bytes.
			input:   []byte{0x04, 'h', 'e', 'l', 'l', 'o'},
			want:    []byte("hello"),
			wantErr: false,
		},
		{
			name: "two literal runs",
			// Run 1: 0x02 = 3 literal bytes "abc".
			// Run 2: 0x01 = 2 literal bytes "de".
			input:   []byte{0x02, 'a', 'b', 'c', 0x01, 'd', 'e'},
			want:    []byte("abcde"),
			wantErr: false,
		},
		{
			name: "literal then short backref",
			// Literal: 0x03 = 4 bytes "abcd"
			// Short backref: top 3 bits = 001 (run=3), offset bits = 4, low byte = 0x00
			//   ctrl = 0x20 | (offset_high & 0x1F), offset_high = 0, offset_low = 3
			//   offset = (0 << 8) | 3 = 3, then +1 = 4. Copy 3 bytes from output[-4].
			//   That copies "abc".
			input:   []byte{0x03, 'a', 'b', 'c', 'd', 0x20, 0x03},
			want:    []byte("abcdabc"),
			wantErr: false,
		},
		{
			name: "literal then long backref",
			// Literal: 0x07 = 8 bytes "abcdefgh"
			// Long backref: ctrl = 0xE0 | (offset_high=0) = 0xE0, offset_low = 7 (offset=8),
			//   extra byte = 0 (run = 0 + 9 = 9). Copies 9 bytes from output[-8].
			//   But output only has 8 bytes, so copies "abcdefgh" + wraps to "a".
			input:   []byte{0x07, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 0xE0, 0x07, 0x00},
			want:    []byte("abcdefghabcdefgha"),
			wantErr: false,
		},
		{
			name:    "empty data",
			input:   []byte{},
			want:    []byte{},
			wantErr: false,
		},
		{
			name:    "truncated literal run",
			input:   []byte{0x05, 'a', 'b'},
			wantErr: true,
		},
		{
			name:    "truncated backref - missing offset low byte",
			input:   []byte{0x03, 'a', 'b', 'c', 'd', 0x20},
			wantErr: true,
		},
		{
			name: "backref with invalid offset",
			// Literal: 0x00 = 1 byte 'a'. Output = "a" (length 1).
			// Backref: ctrl = 0x20, offset_low = 0x09. offset = (0<<8)|9 +1 = 10.
			// Output length is 1, offset 10 > 1 => error.
			input:   []byte{0x00, 'a', 0x20, 0x09},
			wantErr: true,
		},
		{
			name:    "truncated long backref - missing run byte",
			input:   []byte{0x03, 'a', 'b', 'c', 'd', 0xE0, 0x03},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyLZF(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestLZFDecompress tests the low-level LZF decompressor directly.
func TestLZFDecompress(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "empty input",
			input:   []byte{},
			want:    []byte{},
			wantErr: false,
		},
		{
			name: "single literal byte",
			// Control 0x00 = 1 literal byte.
			input:   []byte{0x00, 0xFF},
			want:    []byte{0xFF},
			wantErr: false,
		},
		{
			name: "max short literal run (32 bytes)",
			// Control 0x1F = 32 literal bytes.
			input: append([]byte{0x1F}, bytes.Repeat([]byte{0xAB}, 32)...),
			want:  bytes.Repeat([]byte{0xAB}, 32),
		},
		{
			name: "short backref copies correctly",
			// Literal: 0x02 = 3 bytes "XYZ"
			// Short backref: ctrl = 0x20 (run bits = 001 => len 3), offset_low = 0x02
			//   offset = (0 << 8) | 2 + 1 = 3. Copies 3 bytes from output[-3] = "XYZ".
			input:   []byte{0x02, 'X', 'Y', 'Z', 0x20, 0x02},
			want:    []byte("XYZXYZ"),
			wantErr: false,
		},
		{
			name: "overlapping backref (run-length expansion)",
			// Literal: 0x00 = 1 byte 'A'
			// Short backref: ctrl = 0x60 (run bits = 011 => len 5), offset_low = 0x00
			//   offset = (0<<8)|0 +1 = 1. Copies 5 bytes from output[-1], each time
			//   reading from expanding output. Result: "AAAAAA" (1 + 5).
			input:   []byte{0x00, 'A', 0x60, 0x00},
			want:    []byte("AAAAAA"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lzfDecompress(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestApplyFilter_BZIP2Dispatch tests that applyFilter correctly dispatches BZIP2.
func TestApplyFilter_BZIP2Dispatch(t *testing.T) {
	bzip2AAAA := []byte{
		0x42, 0x5A, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26,
		0x53, 0x59, 0xE1, 0x6E, 0x65, 0x71, 0x00, 0x00,
		0x02, 0x44, 0x00, 0x40, 0x00, 0x20, 0x00, 0x20,
		0x00, 0x21, 0x00, 0x82, 0x0B, 0x17, 0x72, 0x45,
		0x38, 0x50, 0x90, 0xE1, 0x6E, 0x65, 0x71,
	}

	got, err := applyFilter(Filter{ID: FilterBZIP2}, bzip2AAAA)
	require.NoError(t, err)
	require.Equal(t, []byte("AAAA"), got)
}

// TestApplyFilter_LZFDispatch tests that applyFilter correctly dispatches LZF.
func TestApplyFilter_LZFDispatch(t *testing.T) {
	// LZF literal: 0x04 = 5 bytes "hello".
	lzfData := []byte{0x04, 'h', 'e', 'l', 'l', 'o'}

	got, err := applyFilter(Filter{ID: FilterLZF}, lzfData)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), got)
}

// TestApplyFilter_LZFUncompressedPassthrough tests the LZF bypass when data
// size matches cd_values[2] (data stored uncompressed).
func TestApplyFilter_LZFUncompressedPassthrough(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	filter := Filter{
		ID:         FilterLZF,
		ClientData: []uint32{0, 0, 5}, // cd_values[2] = 5 = len(data)
	}

	got, err := applyFilter(filter, raw)
	require.NoError(t, err)
	require.Equal(t, raw, got)
}

// TestApplyFilter_UnknownFilter tests that unknown filter IDs produce an error.
func TestApplyFilter_UnknownFilter(t *testing.T) {
	_, err := applyFilter(Filter{ID: FilterID(12345)}, []byte{0x01})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported filter ID")
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
