package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// chunkIndexFuzzCase names a dataset of testdata/chunk_index/v4_indexes.h5 by
// its object header address, the byte ranges of its chunk index the fuzzer
// may rewrite, and the checksummed spans [start, start+length) whose Jenkins
// checksums (stored right after each span) the target recomputes on request,
// so that mutated fields get past the checksum and reach the decoder. The
// addresses come from h5debug and od of the fixture (chunk_index_corrupt_test.go
// in the root package describes the structures).
type chunkIndexFuzzCase struct {
	ohdr    uint64
	regions [][2]int // [start, end)
	spans   [][2]int // [start, length)
}

// fuzzChunkIndex reads the dataset with the fuzzed index through the whole-read
// path and the chunk iterator's collector under a 1 MB read limit. Errors are
// expected; panics, hangs and allocations past the limit are the bugs.
func fuzzChunkIndex(f *testing.F, c chunkIndexFuzzCase) {
	base, err := os.ReadFile("../../testdata/chunk_index/v4_indexes.h5")
	require.NoError(f, err)
	sb, err := ReadSuperblock(bytes.NewReader(base))
	require.NoError(f, err)
	hdr, err := ReadObjectHeader(bytes.NewReader(base), c.ohdr, sb)
	require.NoError(f, err)
	desc, err := NewDatasetDescriptor(bytes.NewReader(base), hdr, sb)
	require.NoError(f, err)

	for _, s := range c.spans { // The spans are the fixture's real checksummed blocks.
		require.Equal(f, JenkinsChecksum(base[s[0]:s[0]+s[1]]), binary.LittleEndian.Uint32(base[s[0]+s[1]:]), "span %v", s)
	}

	var seed []byte
	for _, r := range c.regions {
		seed = append(seed, base[r[0]:r[1]]...)
	}
	f.Add(seed, false)
	f.Add(seed, true)

	f.Fuzz(func(t *testing.T, data []byte, resign bool) {
		img := bytes.Clone(base)
		for _, r := range c.regions {
			n := copy(img[r[0]:r[1]], data)
			data = data[n:]
		}
		if resign {
			for _, s := range c.spans {
				binary.LittleEndian.PutUint32(img[s[0]+s[1]:], JenkinsChecksum(img[s[0]:s[0]+s[1]]))
			}
		}

		r := bytes.NewReader(img)
		got, err := readChunkedData(r, sb, desc, desc.ElementSize(), fuzzReadLimit)
		if err == nil && uint64(len(got)) != desc.dataBytes {
			t.Fatalf("read %d bytes, the dataset holds %d", len(got), desc.dataBytes)
		}
		entries, err := desc.CollectChunks(r, sb, fuzzReadLimit)
		if err == nil && uint64(len(entries)) > desc.chunkGridTotal() {
			t.Fatalf("%d chunks listed, the grid holds %d", len(entries), desc.chunkGridTotal())
		}
	})
}

// chunkGridTotal is the number of chunks in the current grid, for the fuzz
// invariant only.
func (d *DatasetDescriptor) chunkGridTotal() uint64 {
	n := uint64(1)
	for i, dim := range d.dataspace.Dimensions {
		n *= (dim + d.layout.ChunkSize[i] - 1) / d.layout.ChunkSize[i]
	}

	return n
}

// FuzzChunkIndexFArray fuzzes /fa_paged: the fixed array header, its data
// block with the page bitmap, and both pages.
func FuzzChunkIndexFArray(f *testing.F) {
	fuzzChunkIndex(f, chunkIndexFuzzCase{
		ohdr:    39370,
		regions: [][2]int{{39638, 48493}},
		spans:   [][2]int{{39638, 24}, {39666, 15}, {39685, 8192}, {47881, 608}},
	})
}

// FuzzChunkIndexEArray fuzzes /ea_sblk: the extensible array header, its
// index block, its first data block and the super block of the last records.
func FuzzChunkIndexEArray(f *testing.F) {
	fuzzChunkIndex(f, chunkIndexFuzzCase{
		ohdr:    26922,
		regions: [][2]int{{1987, 2041}, {27190, 27700}},
		spans:   [][2]int{{1987, 50}, {27190, 68}, {27262, 294}, {27560, 14 + 4 + 16*8}},
	})
}

// FuzzChunkIndexBT2 fuzzes /bt2: the v2 B-tree header, its root internal
// node and its three leaves.
func FuzzChunkIndexBT2(f *testing.F) {
	fuzzChunkIndex(f, chunkIndexFuzzCase{
		ohdr:    107,
		regions: [][2]int{{1153, 1191}, {4096, 12288}},
		spans: [][2]int{
			{1153, 34}, {6144, 6 + 2*24 + 3*9},
			{4096, 6 + 84*24}, {8192, 6 + 74*24}, {10240, 6 + 80*24},
		},
	})
}
