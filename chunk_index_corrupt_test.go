package hdf5

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// Offsets into testdata/chunk_index/v4_indexes.h5 (see TestChunkIndex_CLibraryFixture
// for how it was made), taken from `h5debug v4_indexes.h5 <addr> <ohdr>` and
// `od -t x1` of the structures. Superblock v3, 8-byte addresses and lengths.
const (
	// /bt2: v2 B-tree header (BTHD) at 1153, 34 checksummed bytes: type 10,
	// node size 2048, record 24, depth 1, root 0x1800 with 2 records, 240 in all.
	fixBT2Hdr       = 1153
	fixBT2HdrLen    = 34
	fixBT2HdrType   = fixBT2Hdr + 5
	fixBT2HdrRrec   = fixBT2Hdr + 10
	fixBT2HdrRoot   = fixBT2Hdr + 16
	fixBT2HdrAllRec = fixBT2Hdr + 26
	// Its root internal node (BTIN) at 6144: prefix 6, 2 records × 24, then 3
	// child pointers of address(8) + count(1); checksum after 81 bytes.
	fixBT2Root       = 6144
	fixBT2RootLen    = 6 + 2*24 + 3*9
	fixBT2RootChild1 = fixBT2Root + 6 + 2*24 + 9 // Address of the second child.

	// /fa_paged: fixed array header (FAHD) at 39638, 24 checksummed bytes:
	// class 0, record 8, page bits 10, 1100 elements, data block at 39666.
	fixFAHdr       = 39638
	fixFAHdrLen    = 24
	fixFAHdrClass  = fixFAHdr + 5
	fixFAHdrNelmts = fixFAHdr + 8
	fixFAHdrDblk   = fixFAHdr + 16
	// Its data block (FADB) at 39666: prefix 14, page bitmap of 1 byte (0xC0:
	// both pages written), checksum; page 0 at 39685 (1024 records + checksum).
	fixFADblk    = 39666
	fixFADblkLen = 15
	fixFABitmap  = fixFADblk + 14
	fixFAPage0   = fixFADblk + 15 + 4

	// /ea_sblk: extensible array header (EAHD) at 27190, 68 checksummed bytes:
	// data block min 16 at +9, super block min 4 at +10, index block at +60.
	fixEAHdr        = 27190
	fixEAHdrLen     = 68
	fixEAHdrDblkMin = fixEAHdr + 9
	fixEAHdrIblk    = fixEAHdr + 60
	// Its index block (EAIB) at 27262: prefix 14, 4 elements × 8, 6 data block
	// addresses, 25 super block addresses, checksum after 294 bytes.
	fixEAIblk      = 27262
	fixEAIblkLen   = 14 + 4*8 + 6*8 + 25*8
	fixEAIblkDblk0 = fixEAIblk + 14 + 4*8
)

// fixturePatch changes bytes of the C-built fixture; resign recomputes the
// Jenkins checksum stored after each [start, start+length) span, so that a
// structure lies consistently instead of failing its checksum.
type fixturePatch struct {
	at     int
	bytes  []byte
	resign [][2]int
}

func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// patchedFixture writes a patched copy of the fixture and returns its path.
func patchedFixture(t *testing.T, patches ...fixturePatch) string {
	t.Helper()
	buf, err := os.ReadFile("testdata/chunk_index/v4_indexes.h5")
	require.NoError(t, err)
	for _, p := range patches {
		copy(buf[p.at:], p.bytes)
		for _, s := range p.resign {
			binary.LittleEndian.PutUint32(buf[s[0]+s[1]:], core.JenkinsChecksum(buf[s[0]:s[0]+s[1]]))
		}
	}
	path := filepath.Join(t.TempDir(), "patched.h5")
	require.NoError(t, os.WriteFile(path, buf, 0o600))

	return path
}

// TestChunkIndex_CorruptStructures: every lie in a chunk index of the fixture
// is utils.ErrCorrupt with the reason, never a panic, a wrong value or an
// allocation sized by the lie. Run before TASK-058, every case failed with
// "unsupported chunk index type" (also ErrCorrupt-free: the index was not read).
func TestChunkIndex_CorruptStructures(t *testing.T) {
	bt2Hdr := [][2]int{{fixBT2Hdr, fixBT2HdrLen}}
	bt2Root := [][2]int{{fixBT2Root, fixBT2RootLen}}
	faHdr := [][2]int{{fixFAHdr, fixFAHdrLen}}
	eaHdr := [][2]int{{fixEAHdr, fixEAHdrLen}}
	eaIblk := [][2]int{{fixEAIblk, fixEAIblkLen}}
	const beyondEOF = 1 << 30

	for _, tc := range []struct {
		name, dataset, reason string
		patch                 fixturePatch
	}{
		// v2 B-tree.
		{"bt2 header checksum", "/bt2", "checksum mismatch", fixturePatch{at: fixBT2HdrRrec, bytes: []byte{25}}},
		{"bt2 tree type", "/bt2", "type 11, want one of [10]", fixturePatch{at: fixBT2HdrType, bytes: []byte{11}, resign: bt2Hdr}},
		{"bt2 record size", "/bt2", "25-byte records, the dataset needs 24",
			fixturePatch{at: fixBT2HdrRrec, bytes: []byte{25}, resign: bt2Hdr}},
		{"bt2 record count", "/bt2", "holds 240 records, its header says 241",
			fixturePatch{at: fixBT2HdrAllRec, bytes: le64(241), resign: bt2Hdr}},
		{"bt2 record count past root", "/bt2", "tree claims",
			fixturePatch{at: fixBT2HdrAllRec, bytes: le64(1 << 40), resign: bt2Hdr}},
		{"bt2 root outside file", "/bt2", "beyond end of file", fixturePatch{at: fixBT2HdrRoot, bytes: le64(beyondEOF), resign: bt2Hdr}},
		{"bt2 node checksum", "/bt2", "checksum mismatch", fixturePatch{at: fixBT2Root + 7, bytes: []byte{0xEE}}},
		{"bt2 cycle", "/bt2", "reached twice",
			fixturePatch{at: fixBT2RootChild1, bytes: le64(fixBT2Root), resign: bt2Root}},

		// Fixed array.
		{"fa header checksum", "/fa_paged", "checksum mismatch", fixturePatch{at: fixFAHdrNelmts, bytes: []byte{0xFF}}},
		{"fa class", "/fa_paged", "has class 1", fixturePatch{at: fixFAHdrClass, bytes: []byte{1}, resign: faHdr}},
		{"fa elements below grid", "/fa_paged", "has 1000 elements, the chunk grid needs 1100",
			fixturePatch{at: fixFAHdrNelmts, bytes: le64(1000), resign: faHdr}},
		{"fa data block outside file", "/fa_paged", "beyond end of file",
			fixturePatch{at: fixFAHdrDblk, bytes: le64(beyondEOF), resign: faHdr}},
		{"fa data block is the header", "/fa_paged", "fixed array data block at 0x9AD6: checksum mismatch",
			fixturePatch{at: fixFAHdrDblk, bytes: le64(fixFAHdr), resign: faHdr}},
		{"fa page checksum", "/fa_paged", "checksum mismatch", fixturePatch{at: fixFAPage0 + 1, bytes: []byte{0xEE}}},

		// Extensible array.
		{"ea header checksum", "/ea_sblk", "checksum mismatch", fixturePatch{at: fixEAHdrDblkMin, bytes: []byte{3}}},
		{"ea data block min", "/ea_sblk", "not a power of two",
			fixturePatch{at: fixEAHdrDblkMin, bytes: []byte{3}, resign: eaHdr}},
		{"ea index block outside file", "/ea_sblk", "beyond end of file",
			fixturePatch{at: fixEAHdrIblk, bytes: le64(beyondEOF), resign: eaHdr}},
		{"ea index block checksum", "/ea_sblk", "checksum mismatch", fixturePatch{at: fixEAIblk + 20, bytes: []byte{0xEE}}},
		{"ea data block twice", "/ea_sblk", "reached twice",
			fixturePatch{at: fixEAIblkDblk0 + 8, bytes: buf8At(t, fixEAIblkDblk0), resign: eaIblk}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := openDataset(t, patchedFixture(t, tc.patch), tc.dataset)
			_, err := ds.Read()
			require.ErrorIs(t, err, ErrCorrupt)
			require.ErrorContains(t, err, tc.reason)

			_, err = ds.ReadSlice([]uint64{0}, []uint64{1})
			if tc.dataset == "/bt2" {
				_, err = ds.ReadSlice([]uint64{0, 0}, []uint64{1, 1})
			}
			require.ErrorIs(t, err, ErrCorrupt)
		})
	}
}

// buf8At returns the 8 bytes of the unpatched fixture at off.
func buf8At(t *testing.T, off int) []byte {
	t.Helper()
	buf, err := os.ReadFile("testdata/chunk_index/v4_indexes.h5")
	require.NoError(t, err)

	return append([]byte(nil), buf[off:off+8]...)
}

// TestChunkIndex_FixedArrayUnwrittenPage: a page whose bit is clear in the
// data block's bitmap was never written and reads as the fill value, as
// H5FA_get does (H5FA.c:450-454); the other page is unaffected.
func TestChunkIndex_FixedArrayUnwrittenPage(t *testing.T) {
	path := patchedFixture(t, fixturePatch{
		at: fixFABitmap, bytes: []byte{0x40}, resign: [][2]int{{fixFADblk, fixFADblkLen}},
	})
	_, ds := openDataset(t, path, "/fa_paged")

	got, err := ds.Read()
	require.NoError(t, err)
	want := seqFloat(1100, 0)
	for i := range 1024 {
		want[i] = 0
	}
	require.Equal(t, want, got)
}

// TestReadSlice_HugeSparseGridAllocatesByTouchedChunks: the duplicate and
// missing-chunk bookkeeping of a walked index covers only the chunks the
// selection touches (TASK-058). A v1 B-tree dataset of 2^40 one-element
// chunks, 16 written, read through a few chunks allocates a few kilobytes; a
// bitset over the grid would need 128 GiB and fail the read limit.
func TestReadSlice_HugeSparseGridAllocatesByTouchedChunks(t *testing.T) {
	const n = uint64(1) << 40

	path := filepath.Join(t.TempDir(), "sparse.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/x", Int8, []uint64{16}, WithChunkDims([]uint64{1}))
	require.NoError(t, err)
	vals := make([]int8, 16)
	for i := range vals {
		vals[i] = int8(i + 1)
	}
	require.NoError(t, dw.Write(vals))
	require.NoError(t, fw.Close())

	_, ds := openDataset(t, patchDataspaceDims(t, path, "/x", []uint64{n}), "/x")

	var got any
	var readErr error
	allocated := allocatedBy(func() { got, readErr = ds.ReadSlice([]uint64{14}, []uint64{4}) })
	require.NoError(t, readErr)
	require.Equal(t, []float64{15, 16, 0, 0}, sliceToFloat64(t, got))
	require.Less(t, allocated, uint64(1<<20), "the read allocated by the grid, not the selection")

	allocated = allocatedBy(func() { got, readErr = ds.ReadSlice([]uint64{n / 2}, []uint64{3}) })
	require.NoError(t, readErr)
	require.Equal(t, []float64{0, 0, 0}, sliceToFloat64(t, got))
	require.Less(t, allocated, uint64(1<<20), "the read allocated by the grid, not the selection")
}
