package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// earrayHeaderBytes is a sealed extensible array header at address 0 for
// unfiltered eight-byte records, with the given creation parameters, no
// statistics and its index block at address 0x100.
func earrayHeaderBytes(maxBits, idxBlkElmts, dataBlkMin, supBlkMin, pageBits uint8) []byte {
	const size = arrayPrefixSize + 6 + 6*8 + 8
	h := make([]byte, size)
	copy(h, earrayHeaderSig)
	h[4], h[5], h[6], h[7] = 0, arrayClassChunk, 8, maxBits
	h[8], h[9], h[10], h[11] = idxBlkElmts, dataBlkMin, supBlkMin, pageBits
	binary.LittleEndian.PutUint64(h[12+6*8:], 0x100)
	binary.LittleEndian.PutUint32(h[size-4:], JenkinsChecksum(h[:size-4]))

	return h
}

// TestEArrayHeader_PageSmallerThanFirstSuperBlock: C requires a data block
// page to hold the data blocks of the first super block outside the index
// block, H5EA_SBLK_FIRST_IDX (H5EAhdr.c:376-381), whether or not the array has
// such a super block. The check keeps every data block the index block points
// at unpaged (a block larger than a page is paged, H5EAdblock.c:110-113),
// which is how the walk reads them. The reader made the check only when the
// array had super blocks past the index block's, so a header with none and a
// small page was read with its paged data blocks taken as unpaged.
//
// The arrays here: 4 data block pointers (the index block covers super blocks
// 0-3), blocks of at least 16 elements, 7 bits: 4 super blocks in all, none
// past the index block's. The first super block outside it, 4, would have data
// blocks of 2^((4+1)/2) × 16 = 64 elements.
//
// Red on edb53b1: the 4-element page is accepted.
func TestEArrayHeader_PageSmallerThanFirstSuperBlock(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	codec := chunkElemCodec{sb: sb}

	hdr, err := readEArrayHeader(bytes.NewReader(earrayHeaderBytes(7, 4, 16, 4, 6)), sb, 0, codec)
	require.NoError(t, err, "a page of 64 elements holds the first super block's data blocks")
	require.Len(t, hdr.sblks, int(hdr.iblkNsblks), "no super block past the index block's")

	_, err = readEArrayHeader(bytes.NewReader(earrayHeaderBytes(7, 4, 16, 4, 5)), sb, 0, codec)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "page of 32 elements is smaller than the first super block's data blocks of 64")
}
