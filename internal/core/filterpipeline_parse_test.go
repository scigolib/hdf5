package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseFilterPipelineMessage tests filter pipeline message parsing.
func TestParseFilterPipelineMessage(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		wantVersion  uint8
		wantNumFilts uint8
		wantErr      bool
		errContains  string
	}{
		{
			name:        "too short",
			data:        []byte{0x01},
			wantErr:     true,
			errContains: "too short",
		},
		{
			name:        "unsupported version 0",
			data:        []byte{0x00, 0x01},
			wantErr:     true,
			errContains: "unsupported filter pipeline version",
		},
		{
			name:        "unsupported version 3",
			data:        []byte{0x03, 0x01},
			wantErr:     true,
			errContains: "unsupported filter pipeline version",
		},
		{
			name:         "version 2 empty pipeline",
			data:         []byte{0x02, 0x00},
			wantVersion:  2,
			wantNumFilts: 0,
			wantErr:      false,
		},
		{
			name: "version 2 single deflate filter",
			data: func() []byte {
				d := make([]byte, 2+8)                                       // header + filter
				d[0] = 2                                                     // version 2
				d[1] = 1                                                     // 1 filter
				binary.LittleEndian.PutUint16(d[2:4], uint16(FilterDeflate)) // filter ID
				// Name length skipped in v2
				binary.LittleEndian.PutUint16(d[4:6], 0)  // flags
				binary.LittleEndian.PutUint16(d[6:8], 0)  // num client data
				binary.LittleEndian.PutUint16(d[8:10], 0) // padding
				return d[:10]
			}(),
			wantVersion:  2,
			wantNumFilts: 1,
			wantErr:      false,
		},
		{
			name: "version 1 empty pipeline",
			data: func() []byte {
				d := make([]byte, 8) // version + num + reserved(6)
				d[0] = 1             // version 1
				d[1] = 0             // no filters
				return d
			}(),
			wantVersion:  1,
			wantNumFilts: 0,
			wantErr:      false,
		},
		{
			name: "version 1 filter with name",
			data: func() []byte {
				// Header: version(1) + numFilters(1) + reserved(6) = 8
				// Filter: ID(2) + nameLen(2) + flags(2) + numClient(2) + name(8 padded) = 16
				d := make([]byte, 24)
				d[0] = 1 // version 1
				d[1] = 1 // 1 filter
				// Reserved 6 bytes [2:8]
				offset := 8
				binary.LittleEndian.PutUint16(d[offset:offset+2], uint16(FilterShuffle)) // filter ID
				offset += 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], 7) // name length = 7 ("Shuffle")
				offset += 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], 0) // flags
				offset += 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], 0) // num client data
				offset += 2
				copy(d[offset:], "Shuffle\x00") // name + null terminator
				return d
			}(),
			wantVersion:  1,
			wantNumFilts: 1,
			wantErr:      false,
		},
		{
			name: "version 2 filter with client data",
			data: func() []byte {
				// Header: 2 bytes
				// Filter: ID(2) + flags(2) + numClient(2) + clientData(2*4) = 14
				d := make([]byte, 16)
				d[0] = 2 // version 2
				d[1] = 1 // 1 filter
				offset := 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], uint16(FilterDeflate)) // filter ID
				offset += 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], 0x01) // flags
				offset += 2
				binary.LittleEndian.PutUint16(d[offset:offset+2], 2) // 2 client data values
				offset += 2
				binary.LittleEndian.PutUint32(d[offset:offset+4], 100) // client data 0
				offset += 4
				binary.LittleEndian.PutUint32(d[offset:offset+4], 200) // client data 1
				return d
			}(),
			wantVersion:  2,
			wantNumFilts: 1,
			wantErr:      false,
		},
		{
			name: "truncated filter header",
			data: func() []byte {
				d := make([]byte, 4) // Not enough for even 1 filter
				d[0] = 2             // version 2
				d[1] = 1             // 1 filter (but no data)
				return d
			}(),
			wantErr:     true,
			errContains: "truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFilterPipelineMessage(tt.data)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tt.wantVersion, got.Version)
			require.Equal(t, tt.wantNumFilts, got.NumFilters)
			require.Equal(t, int(tt.wantNumFilts), len(got.Filters))
		})
	}
}

// TestParseFilterPipelineMessage_FilterDetails tests filter details parsing.
func TestParseFilterPipelineMessage_FilterDetails(t *testing.T) {
	// Version 2 with deflate filter and client data
	data := make([]byte, 16)
	data[0] = 2 // version 2
	data[1] = 1 // 1 filter
	offset := 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(FilterDeflate))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], 0x0F) // flags
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], 2) // 2 client data
	offset += 2
	binary.LittleEndian.PutUint32(data[offset:offset+4], 123)
	offset += 4
	binary.LittleEndian.PutUint32(data[offset:offset+4], 456)

	got, err := ParseFilterPipelineMessage(data)
	require.NoError(t, err)
	require.Equal(t, 1, len(got.Filters))

	filter := got.Filters[0]
	require.Equal(t, FilterDeflate, filter.ID)
	require.Equal(t, uint16(0x0F), filter.Flags)
	require.Equal(t, uint16(2), filter.NumClientData)
	require.Equal(t, []uint32{123, 456}, filter.ClientData)
}

// TestParseFilterPipelineMessage_Version1WithName tests version 1 name parsing.
func TestParseFilterPipelineMessage_Version1WithName(t *testing.T) {
	// Header: 2 + 6 reserved = 8
	// Filter: ID(2) + nameLen(2) + flags(2) + numClient(2) = 8
	// Name: 10 bytes padded to 16
	// Client data: 1*4 = 4, padded to 8
	data := make([]byte, 8+8+16+8)
	data[0] = 1 // version 1
	data[1] = 1 // 1 filter
	// Reserved [2:8]
	offset := 8
	binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(FilterShuffle))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], 10) // name length
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], 0) // flags
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:offset+2], 1) // 1 client data
	offset += 2
	copy(data[offset:], "MyFilter\x00\x00") // name + null
	offset += 16                            // 10 bytes name padded to 16
	binary.LittleEndian.PutUint32(data[offset:offset+4], 999)

	got, err := ParseFilterPipelineMessage(data)
	require.NoError(t, err)
	require.Equal(t, 1, len(got.Filters))

	filter := got.Filters[0]
	require.Equal(t, FilterShuffle, filter.ID)
	require.Equal(t, "MyFilter", filter.Name)
	require.Equal(t, uint16(10), filter.NameLength)
	require.Equal(t, []uint32{999}, filter.ClientData)
}
