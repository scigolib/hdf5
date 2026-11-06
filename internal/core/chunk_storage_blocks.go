package core

import "io"

// StorageBlocks calls fn once for every block of file space the chunked
// dataset's storage holds, at the size C allocated for it: each stored chunk
// and each block of the chunk index, as H5D__chunk_delete frees them through
// the index's delete (H5Dchunk.c, H5D__chunk_delete; H5Dbtree.c:1207,
// H5Dbtree2.c:1240, H5Dearray.c:1500, H5Dfarray.c:1364, H5Dsingle.c:432,
// H5Dnone.c:418). The implicit index is one allocation holding every chunk of
// the maximum grid, reported once and not chunk by chunk. An index with
// nothing allocated reports nothing.
//
// The walk is VisitChunks over the whole grid, with its checks and bounds;
// budget is its read limit. It only reads.
func (d *DatasetDescriptor) StorageBlocks(r io.ReaderAt, sb *Superblock, budget uint64, fn func(addr, size uint64) error) error {
	stored := func(e *ChunkEntry) error { return fn(e.Address, uint64(e.Key.Nbytes)) }
	if d.layout.ChunkIndexType == ChunkIndexNone {
		stored = func(*ChunkEntry) error { return nil } // Inside the one block.
	}

	return d.VisitChunks(r, sb, nil, budget, ChunkVisitor{Stored: stored, Block: fn})
}
