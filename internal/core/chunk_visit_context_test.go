package core

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCollectChunksContext_Canceled (review F2): the chunk iterator's
// context reached only its Next calls, not the walk that lists the chunks
// first, so ChunkIteratorWithContext could not stop a long walk. The walk now
// checks the context every chunkVisitTick steps and returns its error
// wrapped. New API: there is no earlier code to be red on.
func TestCollectChunksContext_Canceled(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	d, r := implicitInt8(4 * chunkVisitTick)

	got, err := d.CollectChunksContext(context.Background(), r, sb, 1<<20)
	require.NoError(t, err)
	require.Len(t, got, 4*chunkVisitTick)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.CollectChunksContext(ctx, r, sb, 1<<20)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, err.Error(), "chunk index walk stopped after")
}
