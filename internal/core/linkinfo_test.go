package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeLinkInfoMessage tests encoding of Link Info messages.
func TestEncodeLinkInfoMessage(t *testing.T) {
	tests := []struct {
		name        string
		msg         *LinkInfoMessage
		sb          *Superblock
		wantSize    int
		wantErr     bool
		errContains string
	}{
		{
			name: "basic message without creation order",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				MaxCreationOrder:   0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantSize: 2 + 8 + 8, // version + flags + 2 addresses
			wantErr:  false,
		},
		{
			name: "message with creation order tracking",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              LinkInfoTrackCreationOrder,
				MaxCreationOrder:   42,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantSize: 2 + 8 + 8 + 8, // version + flags + max_corder + 2 addresses
			wantErr:  false,
		},
		{
			name: "message with creation order indexing",
			msg: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoIndexCreationOrder,
				MaxCreationOrder:          0,
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantSize: 2 + 8 + 8 + 8, // version + flags + 3 addresses
			wantErr:  false,
		},
		{
			name: "message with both tracking and indexing",
			msg: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoTrackCreationOrder | LinkInfoIndexCreationOrder,
				MaxCreationOrder:          100,
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantSize: 2 + 8 + 8 + 8 + 8, // version + flags + max_corder + 3 addresses
			wantErr:  false,
		},
		{
			name: "message with 4-byte offsets",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 4,
				Endianness: binary.LittleEndian,
			},
			wantSize: 2 + 4 + 4, // version + flags + 2 addresses (4 bytes each)
			wantErr:  false,
		},
		{
			name: "nil message",
			msg:  nil,
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "nil",
		},
		{
			name: "invalid version",
			msg: &LinkInfoMessage{
				Version:            1,
				Flags:              0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := EncodeLinkInfoMessage(tt.msg, tt.sb)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSize, len(buf), "encoded size mismatch")

			// Verify version and flags
			if tt.msg != nil {
				assert.Equal(t, tt.msg.Version, buf[0], "version mismatch")
				assert.Equal(t, tt.msg.Flags, buf[1], "flags mismatch")
			}
		})
	}
}

// TestParseLinkInfoMessage tests decoding of Link Info messages.
func TestParseLinkInfoMessage(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		sb          *Superblock
		want        *LinkInfoMessage
		wantErr     bool
		errContains string
	}{
		{
			name: "basic message without creation order",
			data: []byte{
				0x00, // version
				0x00, // flags
				// Fractal heap address (8 bytes, little-endian)
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Name B-tree address (8 bytes, little-endian)
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			want: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				MaxCreationOrder:   0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			wantErr: false,
		},
		{
			name: "message with creation order tracking",
			data: []byte{
				0x00, // version
				0x01, // flags (track creation order)
				// Max creation order (8 bytes, int64, little-endian)
				0x2A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 42
				// Fractal heap address
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Name B-tree address
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			want: &LinkInfoMessage{
				Version:            0,
				Flags:              LinkInfoTrackCreationOrder,
				MaxCreationOrder:   42,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			wantErr: false,
		},
		{
			name: "message with creation order indexing",
			data: []byte{
				0x00, // version
				0x02, // flags (index creation order)
				// Fractal heap address
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Name B-tree address
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Creation order B-tree address
				0x00, 0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			want: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoIndexCreationOrder,
				MaxCreationOrder:          0,
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			wantErr: false,
		},
		{
			name: "message with both tracking and indexing",
			data: []byte{
				0x00, // version
				0x03, // flags (track + index creation order)
				// Max creation order
				0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 100
				// Fractal heap address
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Name B-tree address
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Creation order B-tree address
				0x00, 0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			want: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoTrackCreationOrder | LinkInfoIndexCreationOrder,
				MaxCreationOrder:          100,
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			wantErr: false,
		},
		{
			name: "message with 4-byte offsets",
			data: []byte{
				0x00, // version
				0x00, // flags
				// Fractal heap address (4 bytes)
				0x00, 0x10, 0x00, 0x00,
				// Name B-tree address (4 bytes)
				0x00, 0x20, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 4,
				Endianness: binary.LittleEndian,
			},
			want: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				MaxCreationOrder:   0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			wantErr: false,
		},
		{
			name: "too short (less than 2 bytes)",
			data: []byte{0x00},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "too short",
		},
		{
			name: "invalid version",
			data: []byte{
				0x01, // invalid version
				0x00, // flags
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "version",
		},
		{
			name: "invalid flags (reserved bits set)",
			data: []byte{
				0x00, // version
				0x04, // invalid flags (bit 2 set)
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "flags",
		},
		{
			name: "truncated (missing max creation order)",
			data: []byte{
				0x00, // version
				0x01, // flags (track creation order)
				// Missing max creation order!
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "truncated",
		},
		{
			name: "truncated (missing fractal heap address)",
			data: []byte{
				0x00, // version
				0x00, // flags
				// Missing addresses!
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "truncated",
		},
		{
			name: "truncated (missing creation order B-tree address)",
			data: []byte{
				0x00, // version
				0x02, // flags (index creation order)
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Missing creation order B-tree address!
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "truncated",
		},
		{
			name: "negative max creation order (validation error)",
			data: []byte{
				0x00, // version
				0x01, // flags (track creation order)
				// Max creation order (8 bytes, int64, little-endian) = -1
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// Fractal heap address
				0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Name B-tree address
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr:     true,
			errContains: "invalid max creation order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLinkInfoMessage(tt.data, tt.sb)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Equal(t, tt.want.Version, got.Version)
			assert.Equal(t, tt.want.Flags, got.Flags)
			assert.Equal(t, tt.want.MaxCreationOrder, got.MaxCreationOrder)
			assert.Equal(t, tt.want.FractalHeapAddress, got.FractalHeapAddress)
			assert.Equal(t, tt.want.NameBTreeAddress, got.NameBTreeAddress)
			assert.Equal(t, tt.want.CreationOrderBTreeAddress, got.CreationOrderBTreeAddress)
		})
	}
}

// TestLinkInfoMessageRoundTrip tests encode -> decode -> verify.
func TestLinkInfoMessageRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  *LinkInfoMessage
		sb   *Superblock
	}{
		{
			name: "basic message",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
		},
		{
			name: "with creation order tracking",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              LinkInfoTrackCreationOrder,
				MaxCreationOrder:   999,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
		},
		{
			name: "with creation order indexing",
			msg: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoIndexCreationOrder,
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
		},
		{
			name: "with both tracking and indexing",
			msg: &LinkInfoMessage{
				Version:                   0,
				Flags:                     LinkInfoTrackCreationOrder | LinkInfoIndexCreationOrder,
				MaxCreationOrder:          12345,
				FractalHeapAddress:        0xDEADBEEF,
				NameBTreeAddress:          0xCAFEBABE,
				CreationOrderBTreeAddress: 0xFEEDFACE,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.LittleEndian,
			},
		},
		{
			name: "with 4-byte offsets",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              0,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 4,
				Endianness: binary.LittleEndian,
			},
		},
		{
			name: "with big-endian",
			msg: &LinkInfoMessage{
				Version:            0,
				Flags:              LinkInfoTrackCreationOrder,
				MaxCreationOrder:   42,
				FractalHeapAddress: 0x1000,
				NameBTreeAddress:   0x2000,
			},
			sb: &Superblock{
				OffsetSize: 8,
				Endianness: binary.BigEndian,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := EncodeLinkInfoMessage(tt.msg, tt.sb)
			require.NoError(t, err)
			require.NotEmpty(t, encoded)

			// Decode
			decoded, err := ParseLinkInfoMessage(encoded, tt.sb)
			require.NoError(t, err)
			require.NotNil(t, decoded)

			// Verify all fields match
			assert.Equal(t, tt.msg.Version, decoded.Version, "version mismatch")
			assert.Equal(t, tt.msg.Flags, decoded.Flags, "flags mismatch")
			assert.Equal(t, tt.msg.MaxCreationOrder, decoded.MaxCreationOrder, "max creation order mismatch")
			assert.Equal(t, tt.msg.FractalHeapAddress, decoded.FractalHeapAddress, "fractal heap address mismatch")
			assert.Equal(t, tt.msg.NameBTreeAddress, decoded.NameBTreeAddress, "name B-tree address mismatch")
			assert.Equal(t, tt.msg.CreationOrderBTreeAddress, decoded.CreationOrderBTreeAddress, "creation order B-tree address mismatch")
		})
	}
}

// TestLinkInfoMessageFlags tests flag helper methods.
func TestLinkInfoMessageFlags(t *testing.T) {
	tests := []struct {
		name                   string
		flags                  uint8
		wantTrackCreationOrder bool
		wantIndexCreationOrder bool
	}{
		{
			name:                   "no flags",
			flags:                  0,
			wantTrackCreationOrder: false,
			wantIndexCreationOrder: false,
		},
		{
			name:                   "track creation order only",
			flags:                  LinkInfoTrackCreationOrder,
			wantTrackCreationOrder: true,
			wantIndexCreationOrder: false,
		},
		{
			name:                   "index creation order only",
			flags:                  LinkInfoIndexCreationOrder,
			wantTrackCreationOrder: false,
			wantIndexCreationOrder: true,
		},
		{
			name:                   "both flags set",
			flags:                  LinkInfoTrackCreationOrder | LinkInfoIndexCreationOrder,
			wantTrackCreationOrder: true,
			wantIndexCreationOrder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &LinkInfoMessage{Flags: tt.flags}

			assert.Equal(t, tt.wantTrackCreationOrder, msg.HasCreationOrderTracking())
			assert.Equal(t, tt.wantIndexCreationOrder, msg.HasCreationOrderIndex())
		})
	}
}

// TestLinkInfoMessageAddressHelpers tests address helper methods.
func TestLinkInfoMessageAddressHelpers(t *testing.T) {
	tests := []struct {
		name                      string
		msg                       *LinkInfoMessage
		wantHasFractalHeap        bool
		wantHasNameBTree          bool
		wantHasCreationOrderBTree bool
	}{
		{
			name: "no addresses",
			msg: &LinkInfoMessage{
				FractalHeapAddress:        0,
				NameBTreeAddress:          0,
				CreationOrderBTreeAddress: 0,
			},
			wantHasFractalHeap:        false,
			wantHasNameBTree:          false,
			wantHasCreationOrderBTree: false,
		},
		{
			name: "fractal heap only",
			msg: &LinkInfoMessage{
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0,
				CreationOrderBTreeAddress: 0,
			},
			wantHasFractalHeap:        true,
			wantHasNameBTree:          false,
			wantHasCreationOrderBTree: false,
		},
		{
			name: "all addresses",
			msg: &LinkInfoMessage{
				FractalHeapAddress:        0x1000,
				NameBTreeAddress:          0x2000,
				CreationOrderBTreeAddress: 0x3000,
			},
			wantHasFractalHeap:        true,
			wantHasNameBTree:          true,
			wantHasCreationOrderBTree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantHasFractalHeap, tt.msg.HasFractalHeap())
			assert.Equal(t, tt.wantHasNameBTree, tt.msg.HasNameBTree())
			assert.Equal(t, tt.wantHasCreationOrderBTree, tt.msg.HasCreationOrderBTree())
		})
	}
}

// TestLinkInfoMessageEdgeCases tests edge cases and boundary conditions.
func TestLinkInfoMessageEdgeCases(t *testing.T) {
	t.Run("max creation order max value", func(t *testing.T) {
		msg := &LinkInfoMessage{
			Version:            0,
			Flags:              LinkInfoTrackCreationOrder,
			MaxCreationOrder:   int64(^uint64(0) >> 1), // max int64
			FractalHeapAddress: 0x1000,
			NameBTreeAddress:   0x2000,
		}
		sb := &Superblock{
			OffsetSize: 8,
			Endianness: binary.LittleEndian,
		}

		encoded, err := EncodeLinkInfoMessage(msg, sb)
		require.NoError(t, err)

		decoded, err := ParseLinkInfoMessage(encoded, sb)
		require.NoError(t, err)
		assert.Equal(t, msg.MaxCreationOrder, decoded.MaxCreationOrder)
	})

	t.Run("zero addresses are valid", func(t *testing.T) {
		msg := &LinkInfoMessage{
			Version:            0,
			Flags:              0,
			FractalHeapAddress: 0,
			NameBTreeAddress:   0,
		}
		sb := &Superblock{
			OffsetSize: 8,
			Endianness: binary.LittleEndian,
		}

		encoded, err := EncodeLinkInfoMessage(msg, sb)
		require.NoError(t, err)

		decoded, err := ParseLinkInfoMessage(encoded, sb)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), decoded.FractalHeapAddress)
		assert.Equal(t, uint64(0), decoded.NameBTreeAddress)
	})

	t.Run("max uint64 addresses", func(t *testing.T) {
		msg := &LinkInfoMessage{
			Version:                   0,
			Flags:                     LinkInfoIndexCreationOrder,
			FractalHeapAddress:        ^uint64(0),
			NameBTreeAddress:          ^uint64(0),
			CreationOrderBTreeAddress: ^uint64(0),
		}
		sb := &Superblock{
			OffsetSize: 8,
			Endianness: binary.LittleEndian,
		}

		encoded, err := EncodeLinkInfoMessage(msg, sb)
		require.NoError(t, err)

		decoded, err := ParseLinkInfoMessage(encoded, sb)
		require.NoError(t, err)
		assert.Equal(t, msg.FractalHeapAddress, decoded.FractalHeapAddress)
		assert.Equal(t, msg.NameBTreeAddress, decoded.NameBTreeAddress)
		assert.Equal(t, msg.CreationOrderBTreeAddress, decoded.CreationOrderBTreeAddress)
	})
}

// TestReadUint64Helper tests the readUint64 helper function.
func TestReadUint64Helper(t *testing.T) {
	tests := []struct {
		name       string
		buf        []byte
		size       int
		endianness binary.ByteOrder
		want       uint64
	}{
		{
			name:       "1 byte",
			buf:        []byte{0x42},
			size:       1,
			endianness: binary.LittleEndian,
			want:       0x42,
		},
		{
			name:       "2 bytes little-endian",
			buf:        []byte{0x34, 0x12},
			size:       2,
			endianness: binary.LittleEndian,
			want:       0x1234,
		},
		{
			name:       "2 bytes big-endian",
			buf:        []byte{0x12, 0x34},
			size:       2,
			endianness: binary.BigEndian,
			want:       0x1234,
		},
		{
			name:       "4 bytes little-endian",
			buf:        []byte{0x78, 0x56, 0x34, 0x12},
			size:       4,
			endianness: binary.LittleEndian,
			want:       0x12345678,
		},
		{
			name:       "4 bytes big-endian",
			buf:        []byte{0x12, 0x34, 0x56, 0x78},
			size:       4,
			endianness: binary.BigEndian,
			want:       0x12345678,
		},
		{
			name:       "8 bytes little-endian",
			buf:        []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01},
			size:       8,
			endianness: binary.LittleEndian,
			want:       0x0102030405060708,
		},
		{
			name:       "8 bytes big-endian",
			buf:        []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			size:       8,
			endianness: binary.BigEndian,
			want:       0x0102030405060708,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readUint64(tt.buf, tt.size, tt.endianness)
			assert.Equal(t, tt.want, got)
		})
	}
}
