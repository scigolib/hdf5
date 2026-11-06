package structures

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestRootDirectBlockImage_BlockSizeBounds covers the read-modify-write path
// (it replaces the test of the deleted readDirectBlockFromFile):
// StartingBlockSize comes from the fractal heap header and sizes the block
// buffer. A value that does not fit in int, or one above the header's own
// MaxDirectBlockSize, must be refused before anything is allocated; a block
// of another heap, or one not at heap offset 0, is corrupt.
func TestRootDirectBlockImage_BlockSizeBounds(t *testing.T) {
	const (
		blockAddr  = uint64(100)
		headerAddr = uint64(0x20)
		sizeofAddr = uint8(4)
	)

	tests := []struct {
		name            string
		blockSize       uint64
		maxDirectBlocks uint64
		owner           uint64
		wantErr         bool
		wantCorrupt     bool
	}{
		{name: "size above platform int", blockSize: math.MaxUint64, maxDirectBlocks: math.MaxUint64,
			owner: headerAddr, wantErr: true},
		{name: "size above header maximum", blockSize: 1024, maxDirectBlocks: 512, owner: headerAddr, wantErr: true},
		{name: "block of another heap", blockSize: 32, maxDirectBlocks: 64, owner: headerAddr + 8,
			wantErr: true, wantCorrupt: true},
		{name: "valid block size", blockSize: 32, maxDirectBlocks: 64, owner: headerAddr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, 200)
			copy(data[blockAddr:blockAddr+4], "FHDB")
			binary.LittleEndian.PutUint32(data[blockAddr+5:blockAddr+9], uint32(tt.owner))

			hdr := &FractalHeapHeader{HeapOffsetSize: 2, RootBlockAddr: blockAddr,
				StartingBlockSize: tt.blockSize, MaxDirectBlockSize: tt.maxDirectBlocks}
			_, err := readRootDirectBlockImage(bytes.NewReader(data), hdr, headerAddr, sizeofAddr)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tt.wantCorrupt {
				require.True(t, errors.Is(err, utils.ErrCorrupt), "want ErrCorrupt, got %v", err)
			}
		})
	}
}
