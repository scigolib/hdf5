package hdf5

// Delete frees what the C library frees for an object it deletes (H5O_delete,
// H5Oint.c:1508-1600): every chunk of the object header, the data, and for a
// chunked dataset every chunk and every block of the chunk index
// (H5D__chunk_delete). The oracle is h5stat 1.14.6 on a file that holds the
// dataset (h5copy): its object header total, its raw data size, and its
// index size, which the C library computes from its own allocation records
// (H5D__farray_idx_size, H5Dfarray.c:1577-1605; H5D__earray_idx_size,
// H5Dearray.c:1716-1748; H5D__bt2_idx_size through H5B2_size,
// H5Dbtree2.c:1457-1480; H5D__btree_idx_size, H5Dbtree.c:1305-1331). For an
// extensible array h5stat's index block is short; earrayStatShortfall adds
// the missing term back.

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// h5statDatasetSpace runs h5stat on a file with one dataset and returns the
// bytes its object headers, raw data and chunk index occupy.
func h5statDatasetSpace(t *testing.T, h5stat, file string) uint64 {
	t.Helper()
	out := runH5(t, h5stat, file)
	field := func(re string) uint64 {
		m := regexp.MustCompile(re).FindStringSubmatch(out)
		require.Len(t, m, 2, "h5stat output misses %q:\n%s", re, out)
		v, err := strconv.ParseUint(m[1], 10, 64)
		require.NoError(t, err)
		return v
	}
	oh := field(`Datasets\(exclude compact data\): (\d+)/`)
	raw := field(`Total raw data size: (\d+)`)
	index := field(`Chunked datasets:\s+Index: (\d+)`)
	return oh + raw + index
}

// allocatorFreeSpace returns the free bytes the allocator tracks and its end
// of file: a Free adds to the first, or lowers the second when it frees the
// tail of the file.
func allocatorFreeSpace(fw *FileWriter) (free, eof uint64) {
	a := fw.writer.Allocator()
	for _, b := range a.FreeBlocks() {
		free += b.Size
	}
	return free, a.EndOfFile()
}

// deleteFreedBytes deletes path through OpenForWrite and returns the bytes
// the delete returned to the allocator.
func deleteFreedBytes(t *testing.T, file, path string) uint64 {
	t.Helper()
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	free0, eof0 := allocatorFreeSpace(fw)
	require.NoError(t, fw.Delete(path))
	free1, eof1 := allocatorFreeSpace(fw)
	require.NoError(t, fw.Close())
	require.GreaterOrEqual(t, free1+eof0, free0+eof1, "the delete allocated space")
	return free1 + eof0 - free0 - eof1
}

// TestCInterop_DeleteFreesChunkIndex: deleting a chunked dataset frees its
// object header, its chunks and every block of its chunk index, as many
// bytes as h5stat reports for it, for every index type, and leaves a file
// h5dump reads. Red before the change: the fixed array, extensible array and
// v2 B-tree datasets freed only the object header (cascadeDelete walked v1
// B-trees only, and its errors were dropped), and the implicit index freed
// nothing of its data.
func TestCInterop_DeleteFreesChunkIndex(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5stat := h5tool(t, "h5stat")
	h5dump := h5tool(t, "h5dump")
	h5debug := h5tool(t, "h5debug")

	v1 := filepath.Join(t.TempDir(), "v1.h5")
	writeV1ChunkedFile(t, v1)

	for _, tc := range []struct {
		src, path string
		index     uint8
	}{
		{"testdata/chunk_index/v4_indexes.h5", "/single", core.ChunkIndexSingle},
		{"testdata/chunk_index/v4_indexes.h5", "/single_gzip", core.ChunkIndexSingle},
		{"testdata/chunk_index/v4_indexes.h5", "/fa_paged", core.ChunkIndexFArray},
		{"testdata/chunk_index/v4_indexes.h5", "/fa_paged_gzip", core.ChunkIndexFArray},
		{"testdata/chunk_index/v4_indexes.h5", "/ea_sblk", core.ChunkIndexEArray},
		{"testdata/chunk_index/v4_indexes.h5", "/ea_sblk_gzip", core.ChunkIndexEArray},
		{"testdata/chunk_index/v4_indexes.h5", "/ea_2d", core.ChunkIndexEArray},
		{"testdata/chunk_index/v4_indexes.h5", "/bt2", core.ChunkIndexBT2},
		{"testdata/chunk_index/v4_indexes.h5", "/bt2_gzip", core.ChunkIndexBT2},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/DSET_EA", core.ChunkIndexEArray},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/DSET_FA", core.ChunkIndexFArray},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/DSET_NONE", core.ChunkIndexNone},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/GROUP/DSET_BT2", core.ChunkIndexBT2},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/DSET_NDATA_BT2", core.ChunkIndexBT2},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/GROUP/DSET_NDATA_EA", core.ChunkIndexEArray},
		{"testdata/hdf5_official/h5fc_ext1_f.h5", "/GROUP/DSET_NDATA_FA", core.ChunkIndexFArray},
		{v1, "/v1", core.ChunkIndexBTREE},
	} {
		t.Run(filepath.Base(tc.src)+tc.path, func(t *testing.T) {
			// /keep stays in the root group, so the group's symbol table
			// node does not empty and is not freed with the dataset
			// (H5G__node_remove, H5Gnode.c:818): only the dataset's own
			// space is counted. /keep is a single chunk index dataset.
			dir := t.TempDir()
			keep := filepath.Join(dir, "keep.h5")
			file := filepath.Join(dir, "two.h5")
			for _, f := range []string{keep, file} {
				runH5(t, h5copy, "-i", "testdata/chunk_index/v4_indexes.h5", "-o", f, "-s", "/single", "-d", "/keep")
			}
			runH5(t, h5copy, "-i", tc.src, "-o", file, "-s", tc.path, "-d", "/d")
			at := requireChunkIndex(t, file, "/d", tc.index)
			want := h5statDatasetSpace(t, h5stat, file) - h5statDatasetSpace(t, h5stat, keep)
			if tc.index == core.ChunkIndexEArray && at.index != math.MaxUint64 {
				want += earrayStatShortfall(t, h5debug, file, at.ohdr, at.index, at.sizeofAddr)
			}

			require.Equal(t, want, deleteFreedBytes(t, file, "/d"))

			out := runH5(t, h5dump, "--enable-error-stack", file)
			require.Contains(t, out, `DATASET "keep"`)
			require.NotContains(t, out, `DATASET "d"`)
		})
	}
}

// writeV1ChunkedFile writes a dataset with a v1 B-tree chunk index of more
// than one level: 400 chunks, 64 entries per node.
func writeV1ChunkedFile(t *testing.T, file string) {
	t.Helper()
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/v1", Float64, []uint64{4000}, WithChunkDims([]uint64{10}))
	require.NoError(t, err)
	require.NoError(t, ds.Write(seqFloat(4000, 0)))
	require.NoError(t, fw.Close())
}

// copiedIndex is where the copied dataset's header and chunk index are.
type copiedIndex struct {
	ohdr, index uint64
	sizeofAddr  uint8
}

// requireChunkIndex checks that h5copy kept the chunk index type and returns
// where the dataset's header and index are.
func requireChunkIndex(t *testing.T, file, path string, index uint8) copiedIndex {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	ds := findDatasetPath(t, f, path)
	desc, err := ds.descriptor()
	require.NoError(t, err)
	require.Equal(t, index, desc.Layout().ChunkIndexType)
	return copiedIndex{ohdr: ds.address, index: desc.Layout().DataAddress, sizeofAddr: f.sb.OffsetSize}
}

// contiguousLayout is where a contiguous dataset's header and data are.
type contiguousLayout struct{ ohdr, data, size uint64 }

// contiguousLayoutOf returns the object header address and the data range of
// a contiguous dataset of a closed file.
func contiguousLayoutOf(t *testing.T, file, path string) contiguousLayout {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	ds := findDatasetPath(t, f, path)
	desc, err := ds.descriptor()
	require.NoError(t, err)
	layout := desc.Layout()
	require.True(t, layout.IsContiguous())
	return contiguousLayout{ohdr: ds.address, data: layout.DataAddress, size: layout.DataSize}
}

// writerBytes reads the whole file through the open writer.
func writerBytes(t *testing.T, fw *FileWriter) []byte {
	t.Helper()
	buf := make([]byte, fw.writer.EndOfFile())
	_, err := fw.writer.ReadAt(buf, 0)
	require.NoError(t, err)
	return buf
}

// pointLayoutAt rewrites, through the open writer, the address field of the
// contiguous layout message of the dataset l (layout message v3: the address
// then the size, H5Olayout.c) so that its data starts at addr.
func pointLayoutAt(t *testing.T, fw *FileWriter, l contiguousLayout, addr uint64) {
	t.Helper()
	pattern := binary.LittleEndian.AppendUint64(nil, l.data)
	pattern = binary.LittleEndian.AppendUint64(pattern, l.size)
	at := bytes.Index(writerBytes(t, fw)[l.ohdr:], pattern)
	require.GreaterOrEqual(t, at, 0, "layout message not found")
	require.NoError(t, fw.writer.WriteAtAddress(binary.LittleEndian.AppendUint64(nil, addr), l.ohdr+uint64(at)))
}

// TestDelete_OverlappingRangesRefused: a delete whose ranges overlap each
// other, or overlap space already free, is refused before anything is
// written or freed: the allocator and every byte of the file stay as they
// were. Red before the change: Delete unlinked the dataset first and then
// freed the bad range, so the overlap went into the free list, where a later
// allocation would overwrite the live object header or data.
func TestDelete_OverlappingRangesRefused(t *testing.T) {
	// Superblock v0: version 1 object headers carry no checksum, so a
	// patched layout message still reads.
	file := filepath.Join(t.TempDir(), "overlap.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	for _, name := range []string{"/a", "/b", "/c"} {
		ds, err := fw.CreateDataset(name, Int32, []uint64{64})
		require.NoError(t, err)
		require.NoError(t, ds.Write(make([]int32, 64)))
	}
	require.NoError(t, fw.Close())
	a := contiguousLayoutOf(t, file, "/a")
	b := contiguousLayoutOf(t, file, "/b")

	refuse := func(t *testing.T, fw *FileWriter) {
		t.Helper()
		before := writerBytes(t, fw)
		free0 := fw.writer.Allocator().FreeBlocks()
		err := fw.Delete("/a")
		require.ErrorIs(t, err, ErrCorrupt)
		require.Equal(t, free0, fw.writer.Allocator().FreeBlocks(), "the allocator changed")
		require.True(t, bytes.Equal(before, writerBytes(t, fw)), "the refused delete changed the file")
	}

	t.Run("data overlaps own header", func(t *testing.T) {
		copied := filepath.Join(t.TempDir(), "own.h5")
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(copied, raw, 0o600))
		fw, err := OpenForWrite(copied, OpenReadWrite)
		require.NoError(t, err)
		defer func() { require.NoError(t, fw.Close()) }()
		pointLayoutAt(t, fw, a, a.ohdr)
		refuse(t, fw)
	})

	t.Run("data overlaps free space", func(t *testing.T) {
		copied := filepath.Join(t.TempDir(), "free.h5")
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(copied, raw, 0o600))
		fw, err := OpenForWrite(copied, OpenReadWrite)
		require.NoError(t, err)
		defer func() { require.NoError(t, fw.Close()) }()
		// /c lies after /b, so b's data becomes a free block, not a shorter
		// end of file.
		require.NoError(t, fw.Delete("/b"))
		covered := false
		for _, fb := range fw.writer.Allocator().FreeBlocks() {
			covered = covered || (fb.Offset <= b.data && b.data+b.size <= fb.Offset+fb.Size)
		}
		require.True(t, covered, "b's data is not free")
		pointLayoutAt(t, fw, a, b.data)
		refuse(t, fw)
	})
}

// earrayStatShortfall returns the bytes h5stat 1.14.6 leaves out of an
// extensible array index. H5EA__cache_hdr_deserialize sizes the index block
// for the statistics from a fake index block whose super block address count
// is hdr->nsblks - nsblks (H5EAcache.c:349-355), but hdr->nsblks is set only
// later, by H5EA__hdr_init (H5EAcache.c:376; H5EAhdr.c:185). The count wraps
// and the reported index block is nsblks * sizeof_addr bytes short of the
// block H5EA__iblock_create allocated (H5EAiblock.c:176-179), which h5debug
// prints as "Index Block size". The correction is that missing term, from the
// header fields h5debug prints: nsblks = 1 + max_nelmts_bits -
// log2(data_blk_min_elmts).
func earrayStatShortfall(t *testing.T, h5debug, file string, ohdr, hdr uint64, sizeofAddr uint8) uint64 {
	t.Helper()
	out := runH5(t, h5debug, file, strconv.FormatUint(hdr, 10), strconv.FormatUint(ohdr, 10))
	field := func(re string) uint64 {
		m := regexp.MustCompile(re).FindStringSubmatch(out)
		require.Len(t, m, 2, "h5debug output misses %q:\n%s", re, out)
		v, err := strconv.ParseUint(m[1], 10, 64)
		require.NoError(t, err)
		return v
	}
	if field(`Index Block Address:\s+(\d+)`) == math.MaxUint64 { // The undefined 8-byte address.
		return 0 // No index block: the statistics count none (H5EAcache.c:357-360).
	}
	maxBits := field(`Log2\(Max\. # of elements in array\):\s+(\d+)`)
	minElmts := field(`Min\. # of elements per data block:\s+(\d+)`)
	nsblks := 1 + maxBits - uint64(bits.TrailingZeros64(minElmts))
	return nsblks * uint64(sizeofAddr)
}
