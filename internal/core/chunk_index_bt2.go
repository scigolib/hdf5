package core

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// iterateBT2 walks the version 2 B-tree chunk index (H5Dbtree2.c), used when
// more than one dimension is unlimited (H5Dchunk.c:966-990). Its records are
// (H5D__bt2_unfilt_decode, H5Dbtree2.c:415-430; H5D__bt2_filt_decode,
// :515-532):
//
//	unfiltered (type 10): address(sizeof_addr) scaled[rank](8 bytes each)
//	filtered   (type 11): address(sizeof_addr) nbytes(chunk_size_len)
//	                      filter_mask(4) scaled[rank](8 bytes each)
//
// with rrec_size = sizeof_addr + rank × 8 (+ chunk_size_len + 4)
// (H5D__bt2_idx_create, H5Dbtree2.c:683-692).
//
// The tree is checked as readBT2Header and walk describe. Deviations,
// stricter: the tree type matches the dataset's filter pipeline and the record
// size the one the dataset implies (C trusts both from the dataset's own
// creation), and a record with an undefined address is utils.ErrCorrupt (C
// asserts a defined address, H5Dbtree2.c:533).
func (d *DatasetDescriptor) iterateBT2(r io.ReaderAt, sb *Superblock, s *chunkSink) error {
	if _, _, err := d.maxChunkGrid(); err != nil {
		return err
	}
	if d.layout.DataAddress == undefinedAddress {
		return nil
	}

	codec := newChunkElemCodec(d, sb)
	rank := len(s.grid)
	wantType := uint8(bt2TypeChunk)
	if codec.filtered {
		wantType = bt2TypeFilteredChunk
	}

	h, err := readBT2Header(r, sb, d.layout.DataAddress, wantType)
	if err != nil {
		return err
	}

	recSize := codec.rawSize() + 8*uint64(rank)
	if h.rrecSize != recSize {
		return fmt.Errorf("%w: v2 B-tree chunk index at 0x%X has %d-byte records, the dataset needs %d",
			utils.ErrCorrupt, h.addr, h.rrecSize, recSize)
	}

	scaled := make([]uint64, rank)
	base := codec.rawSize()

	return h.walk(r, sb, func(rec []byte) error {
		addr, nbytes, mask := codec.decode(rec)
		if addr == undefinedAddress {
			return fmt.Errorf("%w: v2 B-tree chunk index at 0x%X has a record with no address", utils.ErrCorrupt, h.addr)
		}
		for i, at := 0, base; i < len(scaled); i, at = i+1, at+8 {
			scaled[i] = binary.LittleEndian.Uint64(rec[at:])
		}

		return s.add(scaled, addr, nbytes, mask)
	})
}
