package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// memWriterAt wraps []byte to implement io.WriterAt.
type memWriterAt struct {
	data []byte
}

func (m *memWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, nil
	}
	if int(off)+len(p) > len(m.data) {
		newData := make([]byte, int(off)+len(p))
		copy(newData, m.data)
		m.data = newData
	}
	copy(m.data[off:], p)
	return len(p), nil
}

func (m *memWriterAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, nil
	}
	if off >= int64(len(m.data)) {
		return 0, nil
	}
	n = copy(p, m.data[off:])
	return n, nil
}

// TestSuperblock_WriteV0 tests v0 superblock writing.
func TestSuperblock_WriteV0(t *testing.T) {
	tests := []struct {
		name        string
		sb          *Superblock
		eofAddr     uint64
		wantErr     bool
		errContains string
	}{
		{
			name: "valid v0 superblock",
			sb: &Superblock{
				Version:       0,
				OffsetSize:    8,
				LengthSize:    8,
				BaseAddress:   0,
				RootGroup:     0x100,
				RootBTreeAddr: 0x200,
				RootHeapAddr:  0x300,
			},
			eofAddr: 0x1000,
			wantErr: false,
		},
		{
			name: "invalid offset size",
			sb: &Superblock{
				Version:    0,
				OffsetSize: 4, // Not supported for writing
				LengthSize: 8,
			},
			eofAddr:     0x1000,
			wantErr:     true,
			errContains: "only 8-byte offsets",
		},
		{
			name: "invalid length size",
			sb: &Superblock{
				Version:    0,
				OffsetSize: 8,
				LengthSize: 4, // Not supported for writing
			},
			eofAddr:     0x1000,
			wantErr:     true,
			errContains: "only 8-byte offsets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &memWriterAt{data: make([]byte, 0)}
			err := tt.sb.writeV0(buf, tt.eofAddr)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify written data exists
			data := buf.data
			require.GreaterOrEqual(t, len(data), 96, "v0 superblock should be at least 96 bytes")

			// Check signature bytes
			for i := 0; i < 8; i++ {
				require.Equal(t, Signature[i], data[i], "signature byte %d mismatch", i)
			}

			// Check version
			require.Equal(t, uint8(0), data[8])

			// Check offset/length sizes
			require.Equal(t, uint8(8), data[13])
			require.Equal(t, uint8(8), data[14])

			// Check EOF address
			gotEOF := binary.LittleEndian.Uint64(data[40:48])
			require.Equal(t, tt.eofAddr, gotEOF)
		})
	}
}

// TestSuperblock_WriteV0_SymbolTableEntry tests root group symbol table entry.
func TestSuperblock_WriteV0_SymbolTableEntry(t *testing.T) {
	sb := &Superblock{
		Version:       0,
		OffsetSize:    8,
		LengthSize:    8,
		BaseAddress:   0,
		RootGroup:     0xABCD,
		RootBTreeAddr: 0x1234,
		RootHeapAddr:  0x5678,
	}

	buf := &memWriterAt{data: make([]byte, 0)}
	err := sb.writeV0(buf, 0x10000)
	require.NoError(t, err)

	data := buf.data
	require.GreaterOrEqual(t, len(data), 96)

	// Symbol table entry starts at byte 56 (40 bytes long)
	symEntry := data[56:96]

	// Check object header address (bytes 8-15)
	objHeaderAddr := binary.LittleEndian.Uint64(symEntry[8:16])
	require.Equal(t, sb.RootGroup, objHeaderAddr)

	// Check cache type (byte 16)
	cacheType := binary.LittleEndian.Uint32(symEntry[16:20])
	require.Equal(t, uint32(1), cacheType, "cache type should be 1 for group")
}
