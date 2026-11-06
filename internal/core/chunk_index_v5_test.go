package core

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// buildFilteredFArray returns a file image holding a deflated fixed array
// chunk index for 8 float64 values in chunks of 4, and the descriptor that
// reads it. sizeLen is the width of each record's stored chunk size: the
// file's length size for layout version 5, one byte more than the chunk size
// needs (here 2) for version 4 (H5Dfarray.c:55-63). No C library this
// project's tools ship writes version 5 yet, so the image is built by hand
// from the format: H5FAhdr.c / H5FAcache.c for the header and data block,
// H5D__farray_filt_encode (H5Dfarray.c:551-575) for the records.
func buildFilteredFArray(t *testing.T, layoutVersion uint8, sizeLen int) ([]byte, *DatasetDescriptor) {
	t.Helper()
	le := binary.LittleEndian

	chunks := make([][]byte, 0, 2)
	for c := range 2 {
		raw := make([]byte, 32)
		for i := range 4 {
			le.PutUint64(raw[8*i:], math.Float64bits(float64(4*c+i)+0.25))
		}
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		_, err := w.Write(raw)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		chunks = append(chunks, z.Bytes())
	}

	recSize := 8 + sizeLen + 4
	const hdrAddr, dblkAddr = 0, 28
	dataAt := uint64(dblkAddr + 14 + 2*recSize + 4)

	img := []byte("FAHD")
	img = append(img, 0, 1, byte(recSize), 10)
	img = le.AppendUint64(img, 2)
	img = le.AppendUint64(img, dblkAddr)
	img = le.AppendUint32(img, JenkinsChecksum(img))

	start := len(img)
	img = append(img, "FADB"...)
	img = append(img, 0, 1)
	img = le.AppendUint64(img, hdrAddr)
	at := dataAt
	for _, c := range chunks {
		img = le.AppendUint64(img, at)
		size := make([]byte, 8)
		le.PutUint64(size, uint64(len(c)))
		img = append(img, size[:sizeLen]...)
		img = le.AppendUint32(img, 0) // Filter mask: every filter applied.
		at += uint64(len(c))
	}
	img = le.AppendUint32(img, JenkinsChecksum(img[start:]))
	require.Equal(t, int(dataAt), len(img))
	for _, c := range chunks {
		img = append(img, c...)
	}

	d := &DatasetDescriptor{
		datatype:  &DatatypeMessage{Class: DatatypeFloat, Size: 8},
		dataspace: &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{8}, MaxDims: []uint64{8}},
		layout: &DataLayoutMessage{
			Version: layoutVersion, Class: LayoutChunked, ChunkIndexType: ChunkIndexFArray,
			ChunkSize: []uint64{4, 8}, DataAddress: hdrAddr, Index: ChunkIndexParams{FArrayPageBits: 10},
		},
		filterPipeline: &FilterPipelineMessage{Filters: []Filter{{ID: FilterDeflate, ClientData: []uint32{6}}}},
		fill:           &FillValueMessage{},
		nelmts:         8,
		elemSize:       8,
		dataBytes:      64,
		chunkBytes:     32,
	}

	return img, d
}

// TestChunkIndex_LayoutV5FilteredSizeWidth: HDF5 2.0's layout version 5
// stores a filtered chunk's size in the file's length size (8 bytes here),
// version 4 in one byte more than the chunk needs. Reading a version 5 index
// with the version 4 width (or the reverse) misreads every record; the header
// check on the record size catches it as corruption.
func TestChunkIndex_LayoutV5FilteredSizeWidth(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	want := make([]byte, 64)
	for i := range 8 {
		binary.LittleEndian.PutUint64(want[8*i:], math.Float64bits(float64(i)+0.25))
	}

	for _, tc := range []struct {
		name    string
		version uint8
		sizeLen int
	}{
		{"version 5, length-size field", 5, 8},
		{"version 4, chunk size plus one byte", 4, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, d := buildFilteredFArray(t, tc.version, tc.sizeLen)
			got, err := readChunkedData(bytes.NewReader(img), sb, d, 8, utils.DefaultReadLimit)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}

	t.Run("version 5 read with the version 4 width", func(t *testing.T) {
		img, d := buildFilteredFArray(t, 4, 8)
		_, err := readChunkedData(bytes.NewReader(img), sb, d, 8, utils.DefaultReadLimit)
		require.ErrorIs(t, err, utils.ErrCorrupt)
		require.ErrorContains(t, err, "20-byte records, the dataset needs 14")
	})
}
