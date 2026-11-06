package hdf5

// TASK-072: a dataset the writer creates carries the header messages
// H5Dcreate writes for the default dataset creation property list, byte for
// byte: the fill value message (H5Dint.c:1018), the filter pipeline with C's
// flags and version (H5Dlayout.c:304-311, H5Pocpl.c:1065/1104,
// H5Pdcpl.c:2963), the message flags and the message order. The expected
// bytes are read from C-written files in testdata, not typed in by hand,
// except where no C-written file has the combination (said in the case).

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// datasetHeaderMessages returns the header messages of the dataset at path in
// file, without the null (padding) messages.
func datasetHeaderMessages(t *testing.T, file, path string) []*core.HeaderMessage {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	var addr uint64
	found := false
	f.Walk(func(p string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && p == path {
			addr, found = ds.Address(), true
		}
	})
	require.True(t, found, "dataset %s not found in %s", path, file)

	oh, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	var msgs []*core.HeaderMessage
	for _, m := range oh.Messages {
		if m.Type != core.MsgNil {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

// headerMessage returns the one message of type typ among msgs.
func headerMessage(t *testing.T, msgs []*core.HeaderMessage, typ core.MessageType) *core.HeaderMessage {
	t.Helper()
	var found *core.HeaderMessage
	for _, m := range msgs {
		if m.Type == typ {
			require.Nil(t, found, "message type %d appears twice", typ)
			found = m
		}
	}
	require.NotNil(t, found, "no message of type %d", typ)
	return found
}

// cRef names a dataset in a C-written file of testdata.
type cRef struct{ file, path string }

func (r cRef) message(t *testing.T, typ core.MessageType) *core.HeaderMessage {
	t.Helper()
	return headerMessage(t, datasetHeaderMessages(t, filepath.FromSlash(r.file), r.path), typ)
}

// writeOne creates a file with the superblock version sb holding one dataset
// /d made by create, writes it, closes the file and returns its path.
func writeOne(t *testing.T, sb uint8, create func(fw *FileWriter) (*DatasetWriter, error), write func(ds *DatasetWriter) error) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "w.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sb))
	require.NoError(t, err)
	ds, err := create(fw)
	require.NoError(t, err)
	require.NoError(t, write(ds))
	require.NoError(t, fw.Close())
	return file
}

// The C references for the default fill value message.
var (
	// Superblock 2 (libver low bound 1.8): fill message version 3.
	cContigV18  = cRef{"testdata/hdf5_official/h5copytst.h5", "/simple"}     // 03 0a: LATE, IFSET
	cChunkedV18 = cRef{"testdata/hdf5_official/h5copytst.h5", "/compressed"} // 03 0b: INCR, IFSET
	cVLenV18    = cRef{"testdata/hdf5_official/h5copytst.h5", "/nested_vl"}  // 03 02: LATE, ALLOC
	cCompound18 = cRef{"testdata/hdf5_official/h5copytst.h5", "/compound"}   // 03 0a
	// Superblock 3: still version 3 (H5Ofill.c:150-158).
	cContigV110 = cRef{"testdata/hdf5_official/h5fc_ext1_f.h5", "/DSET_CONTIGUOUS"}
	// Superblock 0 (libver low bound earliest): fill message version 2.
	cContigV0  = cRef{"testdata/c-library-corpus/basic/filespace_1_8.h5", "/dset"}  // 02 02 02 01 size 0
	cChunkedV0 = cRef{"testdata/hdf5_official/a.h5", "/A"}                          // 02 03 02 01 size 0
	cVLenV0    = cRef{"testdata/c-library-corpus/strings/charsets.h5", "/CharSets"} // 02 02 00 01 size 0
)

// sixInt32s is the data written into the six-element test datasets.
func sixInt32s() []int32 {
	out := make([]int32, 6)
	for i := range out {
		out[i] = int32(i * 7)
	}
	return out
}

func TestWriterFillValueMessage_MatchesC(t *testing.T) {
	contiguous := func(dt Datatype) func(fw *FileWriter) (*DatasetWriter, error) {
		return func(fw *FileWriter) (*DatasetWriter, error) { return fw.CreateDataset("/d", dt, []uint64{6}) }
	}
	chunked := func(fw *FileWriter) (*DatasetWriter, error) {
		return fw.CreateDataset("/d", Int32, []uint64{6}, WithChunkDims([]uint64{2}))
	}
	writeInts := func(ds *DatasetWriter) error { return ds.Write(sixInt32s()) }
	writeStrings := func(ds *DatasetWriter) error {
		return ds.Write([]string{"a", "bb", "ccc", "dddd", "e", "ff"})
	}
	compoundType := func(t *testing.T) *CompoundType {
		ct, err := NewPackedCompoundType([]CompoundMember{{Name: "a", Type: Int32}, {Name: "b", Type: Float64}})
		require.NoError(t, err)
		return ct
	}

	cases := []struct {
		name   string
		sb     uint8
		create func(t *testing.T) func(fw *FileWriter) (*DatasetWriter, error)
		write  func(ds *DatasetWriter) error
		ref    cRef
	}{
		{"v2_contiguous", SuperblockV2, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return contiguous(Int32) }, writeInts, cContigV18},
		{"v2_chunked", SuperblockV2, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return chunked }, writeInts, cChunkedV18},
		{"v2_vlen_string", SuperblockV2, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return contiguous(VLenString) }, writeStrings, cVLenV18},
		{"v3_contiguous", SuperblockV3, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return contiguous(Int32) }, writeInts, cContigV110},
		{"v0_contiguous", SuperblockV0, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return contiguous(Int32) }, writeInts, cContigV0},
		{"v0_chunked", SuperblockV0, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return chunked }, writeInts, cChunkedV0},
		{"v0_vlen_string", SuperblockV0, func(*testing.T) func(*FileWriter) (*DatasetWriter, error) { return contiguous(VLenString) }, writeStrings, cVLenV0},
		{
			"v2_compound_contiguous", SuperblockV2,
			func(t *testing.T) func(*FileWriter) (*DatasetWriter, error) {
				ct := compoundType(t)
				return func(fw *FileWriter) (*DatasetWriter, error) { return fw.CreateCompound("/d", ct, []uint64{6}) }
			},
			func(ds *DatasetWriter) error { return ds.WriteRaw(make([]byte, 6*12)) },
			cCompound18,
		},
		{
			"v2_compound_chunked", SuperblockV2,
			func(t *testing.T) func(*FileWriter) (*DatasetWriter, error) {
				ct := compoundType(t)
				return func(fw *FileWriter) (*DatasetWriter, error) {
					return fw.CreateCompound("/d", ct, []uint64{6}, WithChunkDims([]uint64{2}))
				}
			},
			func(ds *DatasetWriter) error { return ds.WriteRaw(make([]byte, 6*12)) },
			cChunkedV18,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := writeOne(t, tc.sb, tc.create(t), tc.write)
			want := tc.ref.message(t, core.MsgFillValue)
			got := headerMessage(t, datasetHeaderMessages(t, file, "/d"), core.MsgFillValue)
			require.Equal(t, hex.EncodeToString(want.Data), hex.EncodeToString(got.Data), "fill value message body")
			require.Equal(t, want.Flags, got.Flags, "fill value message flags")
		})
	}
}

func TestWriterFilterPipeline_MatchesC(t *testing.T) {
	float32s := func(ds *DatasetWriter) error {
		return ds.Write([]float32{1, 2, 3, 4, 5, 6, 7, 8})
	}
	cases := []struct {
		name string
		sb   uint8
		opts []DatasetOption
		// ref is the C-written dataset with the same pipeline; want is used
		// where no C-written file in testdata has the combination.
		ref  *cRef
		want string
	}{
		{
			name: "v2_deflate", sb: SuperblockV2,
			opts: []DatasetOption{WithGZIPCompression(9)},
			ref:  &cRef{"testdata/hdf5_official/h5stat_idx.h5", "/dset_filter"},
		},
		{
			name: "v0_deflate", sb: SuperblockV0,
			opts: []DatasetOption{WithGZIPCompression(6)},
			ref:  &cRef{"testdata/c-library-corpus/basic/be_data.h5", "/Deflate_float_data_le"},
		},
		{
			name: "v0_shuffle", sb: SuperblockV0,
			opts: []DatasetOption{WithShuffle()},
			ref:  &cRef{"testdata/c-library-corpus/basic/be_data.h5", "/Shuffle_float_data_le"},
		},
		{
			name: "v0_fletcher32", sb: SuperblockV0,
			opts: []DatasetOption{WithFletcher32()},
			ref:  &cRef{"testdata/c-library-corpus/basic/be_data.h5", "/Fletcher_float_data_le"},
		},
		{
			// Version 2 as H5O__pline_encode writes it (H5Opline.c:280-348): no
			// reserved bytes, no names for filters below 256, no padding;
			// shuffle and deflate OPTIONAL, fletcher32 MANDATORY.
			name: "v2_shuffle_deflate_fletcher32", sb: SuperblockV2,
			opts: []DatasetOption{WithShuffle(), WithGZIPCompression(4), WithFletcher32()},
			want: "0203" +
				"0200" + "0100" + "0100" + "04000000" +
				"0100" + "0100" + "0100" + "04000000" +
				"0300" + "0000" + "0000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := writeOne(t, tc.sb, func(fw *FileWriter) (*DatasetWriter, error) {
				opts := append([]DatasetOption{WithChunkDims([]uint64{4})}, tc.opts...)
				return fw.CreateDataset("/d", Float32, []uint64{8}, opts...)
			}, float32s)
			got := headerMessage(t, datasetHeaderMessages(t, file, "/d"), core.MsgFilterPipeline)
			want, flags := tc.want, core.MsgFlagConstant
			if tc.ref != nil {
				m := tc.ref.message(t, core.MsgFilterPipeline)
				want, flags = hex.EncodeToString(m.Data), m.Flags
			}
			require.Equal(t, want, hex.EncodeToString(got.Data), "filter pipeline message body")
			require.Equal(t, flags, got.Flags, "filter pipeline message flags")

			// Our reader still reads the data through the pipeline.
			f, err := Open(file)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			var vals []float64
			f.Walk(func(p string, obj Object) {
				if ds, ok := obj.(*Dataset); ok && p == "/d" {
					vals, err = ds.Read()
				}
			})
			require.NoError(t, err)
			require.Equal(t, []float64{1, 2, 3, 4, 5, 6, 7, 8}, vals)
		})
	}
}

// TestWriterDatasetHeader_MessagesMatchC checks the message sequence and the
// message flags against C: H5D__update_oh_info appends dataspace (flags 0,
// H5S.c:1068), datatype (CONSTANT, H5Dint.c:1014), fill value (CONSTANT,
// H5Dint.c:1018), then H5D__layout_oh_create the pipeline (CONSTANT,
// H5Dlayout.c:310) and the layout (0 unless allocation is early,
// H5Dlayout.c:379-392).
func TestWriterDatasetHeader_MessagesMatchC(t *testing.T) {
	type typeFlags struct {
		Type  core.MessageType
		Flags uint8
	}
	sequence := func(msgs []*core.HeaderMessage) []typeFlags {
		out := make([]typeFlags, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, typeFlags{m.Type, m.Flags})
		}
		return out
	}
	cases := []struct {
		name string
		opts []DatasetOption
		ref  cRef
	}{
		{"contiguous", nil, cContigV18},
		{"chunked_deflate", []DatasetOption{WithChunkDims([]uint64{2}), WithGZIPCompression(1)}, cChunkedV18},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := writeOne(t, SuperblockV2, func(fw *FileWriter) (*DatasetWriter, error) {
				return fw.CreateDataset("/d", Int32, []uint64{6}, tc.opts...)
			}, func(ds *DatasetWriter) error { return ds.Write(sixInt32s()) })
			want := sequence(datasetHeaderMessages(t, filepath.FromSlash(tc.ref.file), tc.ref.path))
			got := sequence(datasetHeaderMessages(t, file, "/d"))
			require.Equal(t, want, got)
		})
	}
}

// TestWriterChunked_LargeHeaderKeepsBTreeAddress: the chunk index address is
// patched into the layout message when the data is written. The patch offset
// must follow the real header layout: with more than 255 bytes of messages
// the chunk-size field of the version 2 header is two bytes wide
// (H5Ocache.c:1197-1210), not one.
func TestWriterChunked_LargeHeaderKeepsBTreeAddress(t *testing.T) {
	members := make([]CompoundMember, 0, 24)
	for i := 0; i < 24; i++ {
		members = append(members, CompoundMember{Name: "a_rather_long_member_name_" + string(rune('a'+i)), Type: Int32})
	}
	ct, err := NewPackedCompoundType(members)
	require.NoError(t, err)

	raw := make([]byte, 4*24*4)
	for i := range raw {
		raw[i] = byte(i)
	}
	file := writeOne(t, SuperblockV2, func(fw *FileWriter) (*DatasetWriter, error) {
		return fw.CreateCompound("/d", ct, []uint64{4}, WithChunkDims([]uint64{2}))
	}, func(ds *DatasetWriter) error { return ds.WriteRaw(raw) })

	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var vals []core.CompoundValue
	f.Walk(func(p string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && p == "/d" {
			vals, err = ds.ReadCompound()
		}
	})
	require.NoError(t, err)
	require.Len(t, vals, 4)
	// Element 3, member 23: bytes 3*96+23*4 .. +4.
	off := 3*96 + 23*4
	want := int32(raw[off]) | int32(raw[off+1])<<8 | int32(raw[off+2])<<16 | int32(raw[off+3])<<24
	require.Equal(t, want, vals[3]["a_rather_long_member_name_x"])
}

// TestWriterDataspaceMessage_MatchesC checks the dataset dataspace message
// against C: version 1 in superblock-0 files, version 2 (4-byte prefix with the
// dataspace type, no reserved bytes) from the 1.8 format on
// (H5O_sdspace_ver_bounds, H5S.c:61-69 and 2002; H5O__sdspace_encode,
// H5Osdspace.c:249-297). Resize keeps the version.
func TestWriterDataspaceMessage_MatchesC(t *testing.T) {
	ints := func(n int) []int32 {
		out := make([]int32, n)
		for i := range out {
			out[i] = int32(i)
		}
		return out
	}
	cases := []struct {
		name  string
		sb    uint8
		dims  []uint64
		opts  []DatasetOption
		ref   *cRef
		want  string // hex, when no C file has this shape
		count int
	}{
		{name: "v2_contiguous", sb: SuperblockV2, dims: []uint64{6}, ref: &cContigV18, count: 6},
		{name: "v2_chunked", sb: SuperblockV2, dims: []uint64{6}, opts: []DatasetOption{WithChunkDims([]uint64{2})}, ref: &cChunkedV18, count: 6},
		{name: "v3_contiguous", sb: SuperblockV3, dims: []uint64{10}, ref: &cContigV110, count: 10},
		{name: "v0_contiguous", sb: SuperblockV0, dims: []uint64{100}, ref: &cContigV0, count: 100},
		{
			name: "v0_chunked_unlimited", sb: SuperblockV0, dims: []uint64{5, 10, 10},
			opts: []DatasetOption{WithChunkDims([]uint64{5, 10, 10}), WithMaxDims([]uint64{Unlimited, 10, 10})},
			ref:  &cChunkedV0, count: 500,
		},
		{
			// H5Osdspace.c:262-297: version 2, rank 2, H5S_VALID_MAX, H5S_SIMPLE,
			// then the sizes and maxima as 8-byte lengths.
			name: "v2_chunked_unlimited", sb: SuperblockV2, dims: []uint64{4, 3},
			opts:  []DatasetOption{WithChunkDims([]uint64{2, 3}), WithMaxDims([]uint64{Unlimited, 3})},
			want:  "02020101" + "0400000000000000" + "0300000000000000" + "ffffffffffffffff" + "0300000000000000",
			count: 12,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := writeOne(t, tc.sb, func(fw *FileWriter) (*DatasetWriter, error) {
				return fw.CreateDataset("/d", Int32, tc.dims, tc.opts...)
			}, func(ds *DatasetWriter) error { return ds.Write(ints(tc.count)) })

			got := headerMessage(t, datasetHeaderMessages(t, file, "/d"), core.MsgDataspace)
			want := tc.want
			if tc.ref != nil {
				m := tc.ref.message(t, core.MsgDataspace)
				want = hex.EncodeToString(m.Data)
				require.Equal(t, m.Flags, got.Flags)
			}
			require.Equal(t, want, hex.EncodeToString(got.Data))

			f, err := Open(file)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			ds := findDataset(f, "/d")
			require.NotNil(t, ds)
			back, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, back, tc.count)
		})
	}
}

// TestWriterDataspaceMessage_ResizeKeepsVersion grows a chunked dataset in a
// 1.8+ file: the rewritten dataspace stays version 2, so the message keeps its
// size in the header.
func TestWriterDataspaceMessage_ResizeKeepsVersion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "r.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV2))
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/d", Int32, []uint64{4},
		WithChunkDims([]uint64{2}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3, 4}))
	require.NoError(t, ds.Resize([]uint64{8}))
	require.NoError(t, fw.Close())

	got := headerMessage(t, datasetHeaderMessages(t, file, "/d"), core.MsgDataspace)
	require.Equal(t, "02010101"+"0800000000000000"+"ffffffffffffffff", hex.EncodeToString(got.Data))
}

// TestWriterAttributeDataspace_MatchesC: an attribute's dataspace takes the
// version of the file's format (H5S_set_version, H5Aint.c:361) and
// stores its maximum dimensions, equal to the dimensions as
// H5Screate_simple sets them without a maximum (H5S.c:1291-1298), like a
// dataset's (review of TASK-072, LOW 3).
func TestWriterAttributeDataspace_MatchesC(t *testing.T) {
	for _, tc := range []struct {
		sb      uint8
		version uint8
	}{{SuperblockV0, core.DataspaceVersion1}, {SuperblockV2, core.DataspaceVersion2}} {
		t.Run(fmt.Sprintf("sb%d", tc.sb), func(t *testing.T) {
			file := writeOne(t, tc.sb, func(fw *FileWriter) (*DatasetWriter, error) {
				return fw.CreateDataset("/d", Int32, []uint64{6})
			}, func(ds *DatasetWriter) error {
				if err := ds.WriteAttribute("v", []float64{1, 2, 3}); err != nil {
					return err
				}
				return ds.Write(sixInt32s())
			})
			msg := headerMessage(t, datasetHeaderMessages(t, file, "/d"), core.MsgAttribute)
			attr, err := core.ParseAttributeMessage(msg.Data, binary.LittleEndian)
			require.NoError(t, err)
			require.Equal(t, tc.version, attr.Dataspace.Version)
			require.Equal(t, []uint64{3}, attr.Dataspace.Dimensions)
			require.Equal(t, []uint64{3}, attr.Dataspace.MaxDims)
		})
	}
}
