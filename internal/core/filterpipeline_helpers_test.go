package core

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFilterName tests filter name conversion.
func TestFilterName(t *testing.T) {
	tests := []struct {
		name     string
		filterID FilterID
		want     string
	}{
		{
			name:     "GZIP deflate",
			filterID: FilterDeflate,
			want:     "GZIP",
		},
		{
			name:     "Shuffle filter",
			filterID: FilterShuffle,
			want:     "Shuffle",
		},
		{
			name:     "Fletcher32 checksum",
			filterID: FilterFletcher,
			want:     "Fletcher32",
		},
		{
			name:     "SZIP compression",
			filterID: FilterSZIP,
			want:     "SZIP",
		},
		{
			name:     "N-bit compression",
			filterID: FilterNBit,
			want:     "N-bit",
		},
		{
			name:     "Scale-Offset filter",
			filterID: FilterScaleOffset,
			want:     "Scale-Offset",
		},
		{
			name:     "unknown filter ID",
			filterID: 999,
			want:     "Unknown-999",
		},
		{
			name:     "unknown filter ID 0",
			filterID: 0,
			want:     "Unknown-0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterName(tt.filterID)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestBytesReaderAt tests the bytesReaderAt helper.
func TestBytesReaderAt(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	reader := &bytesReaderAt{data: data}

	tests := []struct {
		name     string
		offset   int64
		bufLen   int
		wantN    int
		wantData []byte
		wantErr  bool
		errIsEOF bool
	}{
		{
			name:     "read from start",
			offset:   0,
			bufLen:   4,
			wantN:    4,
			wantData: []byte{0x00, 0x01, 0x02, 0x03},
			wantErr:  false,
		},
		{
			name:     "read from middle",
			offset:   3,
			bufLen:   3,
			wantN:    3,
			wantData: []byte{0x03, 0x04, 0x05},
			wantErr:  false,
		},
		{
			name:     "read to end",
			offset:   5,
			bufLen:   10,
			wantN:    3,
			wantData: []byte{0x05, 0x06, 0x07},
			wantErr:  true,
			errIsEOF: true,
		},
		{
			name:     "read at end",
			offset:   8,
			bufLen:   4,
			wantN:    0,
			wantData: []byte{},
			wantErr:  true,
			errIsEOF: true,
		},
		{
			name:     "negative offset",
			offset:   -1,
			bufLen:   4,
			wantN:    0,
			wantData: []byte{},
			wantErr:  true,
			errIsEOF: true,
		},
		{
			name:     "offset beyond end",
			offset:   10,
			bufLen:   4,
			wantN:    0,
			wantData: []byte{},
			wantErr:  true,
			errIsEOF: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.bufLen)
			n, err := reader.ReadAt(buf, tt.offset)

			require.Equal(t, tt.wantN, n)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIsEOF {
					require.ErrorIs(t, err, io.EOF)
				}
			} else {
				require.NoError(t, err)
			}

			if tt.wantN > 0 {
				require.Equal(t, tt.wantData, buf[:tt.wantN])
			}
		})
	}
}
