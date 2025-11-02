// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHeapID(t *testing.T) {
	tests := []struct {
		name       string
		heapID     [7]byte
		header     *fractalHeapHeaderRaw
		wantOffset uint64
		wantLength uint64
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "managed object with 2-byte offset and 2-byte length",
			heapID: [7]byte{
				0x00,       // type = 0 (managed), flags = 0
				0x10, 0x00, // offset = 0x0010 (little-endian)
				0x20, 0x00, // length = 0x0020 (little-endian)
				0x00, 0x00, // padding
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 2,
				HeapLengthSize: 2,
			},
			wantOffset: 0x0010,
			wantLength: 0x0020,
			wantErr:    false,
		},
		{
			name: "managed object with 4-byte offset and 2-byte length",
			heapID: [7]byte{
				0x00,                   // type = 0 (managed)
				0x34, 0x12, 0x00, 0x00, // offset = 0x00001234 (little-endian)
				0x56, 0x00, // length = 0x0056
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 4,
				HeapLengthSize: 2,
			},
			wantOffset: 0x00001234,
			wantLength: 0x0056,
			wantErr:    false,
		},
		{
			name: "managed object with 3-byte offset and 3-byte length",
			heapID: [7]byte{
				0x00,             // type = 0 (managed)
				0xFF, 0xFF, 0x01, // offset = 0x01FFFF (little-endian)
				0x00, 0x10, 0x00, // length = 0x001000 (little-endian)
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 3,
				HeapLengthSize: 3,
			},
			wantOffset: 0x01FFFF,
			wantLength: 0x001000,
			wantErr:    false,
		},
		{
			name: "all zeros (minimal valid)",
			heapID: [7]byte{
				0x00, // type = 0 (managed)
				0x00, // offset byte 1
				0x00, // offset byte 2 (or length byte 1)
				0x00, 0x00, 0x00, 0x00,
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 1,
				HeapLengthSize: 1,
			},
			wantOffset: 0x00,
			wantLength: 0x00,
			wantErr:    false,
		},
		{
			name: "maximum offset and length with 2-byte sizes",
			heapID: [7]byte{
				0x00,       // type = 0 (managed)
				0xFF, 0xFF, // offset = 0xFFFF
				0xFF, 0xFF, // length = 0xFFFF
				0x00, 0x00, // padding
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 2,
				HeapLengthSize: 2,
			},
			wantOffset: 0xFFFF,
			wantLength: 0xFFFF,
			wantErr:    false,
		},
		{
			name: "unsupported heap type (type = 1)",
			heapID: [7]byte{
				0x10, // type = 1 (bits 4-5), unsupported
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 2,
				HeapLengthSize: 2,
			},
			wantOffset: 0,
			wantLength: 0,
			wantErr:    true,
			wantErrMsg: "unsupported heap ID type: 1",
		},
		{
			name: "unsupported heap type (type = 2)",
			heapID: [7]byte{
				0x20, // type = 2 (bits 4-5), unsupported
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 2,
				HeapLengthSize: 2,
			},
			wantOffset: 0,
			wantLength: 0,
			wantErr:    true,
			wantErrMsg: "unsupported heap ID type: 2",
		},
		{
			name: "unsupported heap type (type = 3)",
			heapID: [7]byte{
				0x30, // type = 3 (bits 4-5), unsupported
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 2,
				HeapLengthSize: 2,
			},
			wantOffset: 0,
			wantLength: 0,
			wantErr:    true,
			wantErrMsg: "unsupported heap ID type: 3",
		},
		{
			name: "1-byte offset and length (minimal size)",
			heapID: [7]byte{
				0x00,                   // type = 0 (managed)
				0x42,                   // offset = 0x42
				0x99,                   // length = 0x99
				0x00, 0x00, 0x00, 0x00, // unused
			},
			header: &fractalHeapHeaderRaw{
				HeapOffsetSize: 1,
				HeapLengthSize: 1,
			},
			wantOffset: 0x42,
			wantLength: 0x99,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offset, length, err := parseHeapID(tt.heapID, tt.header)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					require.Contains(t, err.Error(), tt.wantErrMsg)
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantOffset, offset, "offset mismatch")
			require.Equal(t, tt.wantLength, length, "length mismatch")
		})
	}
}

func TestParseHeapID_EdgeCases(t *testing.T) {
	t.Run("flags in byte 0 (should be ignored)", func(t *testing.T) {
		// Bits 0-3 and 6-7 of byte 0 are flags, should not affect parsing
		heapID := [7]byte{
			0x0F,       // type = 0, flags = 0x0F (all flag bits set)
			0x12, 0x00, // offset = 0x0012
			0x34, 0x00, // length = 0x0034
			0x00, 0x00,
		}
		header := &fractalHeapHeaderRaw{
			HeapOffsetSize: 2,
			HeapLengthSize: 2,
		}

		offset, length, err := parseHeapID(heapID, header)
		require.NoError(t, err)
		require.Equal(t, uint64(0x0012), offset)
		require.Equal(t, uint64(0x0034), length)
	})

	t.Run("large values with 4-byte sizes", func(t *testing.T) {
		heapID := [7]byte{
			0x00,             // type = 0 (managed)
			0xFF, 0xFF, 0xFF, // offset bytes (only 3 bytes used out of possible 4)
			0xFF, 0xFF, 0xFF, // length bytes (only 3 bytes used)
		}
		header := &fractalHeapHeaderRaw{
			HeapOffsetSize: 3,
			HeapLengthSize: 3,
		}

		offset, length, err := parseHeapID(heapID, header)
		require.NoError(t, err)
		require.Equal(t, uint64(0xFFFFFF), offset)
		require.Equal(t, uint64(0xFFFFFF), length)
	})
}
