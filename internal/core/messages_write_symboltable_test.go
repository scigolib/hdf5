package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEncodeSymbolTableMessage tests encoding a symbol table message.
func TestEncodeSymbolTableMessage(t *testing.T) {
	btreeAddr := uint64(0x1000)
	heapAddr := uint64(0x2000)
	offsetSize := 8

	data := EncodeSymbolTableMessage(btreeAddr, heapAddr, offsetSize, 0)
	require.NotNil(t, data)
	require.Equal(t, 16, len(data)) // 2 * 8-byte addresses
}

// TestEncodeSymbolTableMessage_OffsetSize4 tests encoding with 4-byte offsets.
func TestEncodeSymbolTableMessage_OffsetSize4(t *testing.T) {
	btreeAddr := uint64(0x1000)
	heapAddr := uint64(0x2000)
	offsetSize := 4

	data := EncodeSymbolTableMessage(btreeAddr, heapAddr, offsetSize, 0)
	require.NotNil(t, data)
	require.Equal(t, 8, len(data)) // 2 * 4-byte addresses
}
