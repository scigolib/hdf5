package structures

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadDirectBlockFromFile_BlockSizeBounds covers the read-modify-write
// path: StartingBlockSize comes from the fractal heap header, is converted to
// int and then sizes the block buffer. A value that does not fit in int wraps
// negative (32-bit) or asks for an impossible allocation (64-bit). The block
// size must be validated against the header's own MaxDirectBlockSize and the
// platform limit before anything is allocated.
func TestReadDirectBlockFromFile_BlockSizeBounds(t *testing.T) {
	fh := &WritableFractalHeap{}
	reader := bytes.NewReader(make([]byte, 200))

	tests := []struct {
		name            string
		blockSize       uint64
		maxDirectBlocks uint64
		wantErr         bool
	}{
		{name: "size above platform int", blockSize: math.MaxUint64, wantErr: true},
		{name: "size above header maximum", blockSize: 1024, maxDirectBlocks: 512, wantErr: true},
		{name: "valid block size", blockSize: 32, maxDirectBlocks: 64, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, 200)
			address := uint64(100)
			copy(data[address:address+4], "FHDB")
			headerAddr := uint64(0x20)
			binary.LittleEndian.PutUint32(data[address+5:address+9], uint32(headerAddr))

			reader.Reset(data)

			_, err := fh.readDirectBlockFromFile(reader, address, tt.blockSize, 2, 4,
				binary.LittleEndian, headerAddr, tt.maxDirectBlocks)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
