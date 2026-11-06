package hdf5

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expected values in this file come from the HDF5 1.14.6 tools run on the
// same files: `h5dump -H -p` for classes, dimensions, chunk dimensions and
// filters, `h5ls -v` for compound member offsets and sizes.

// metaInt32Info is the description of H5T_STD_I32LE / H5T_STD_I32BE.
func metaInt32Info(order ByteOrder) DatatypeInfo {
	return DatatypeInfo{Class: ClassInteger, Size: 4, Order: order, Signed: true}
}

func TestDatasetShape(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		dataset  string
		shape    []uint64
		maxShape []uint64
	}{
		// DATASPACE SIMPLE { ( 10, 20 ) / ( 10, 20 ) }
		{"contiguous", "testdata/hdf5_official/tdset.h5", "/dset1", []uint64{10, 20}, []uint64{10, 20}},
		// DATASPACE SIMPLE { ( 40, 20 ) / ( 40, 20 ) }
		{"compact", "testdata/hdf5_official/h5repack_layout.h5", "/dset_compact", []uint64{40, 20}, []uint64{40, 20}},
		// DATASPACE SIMPLE { ( 10, 20 ) / ( H5S_UNLIMITED, 20 ) }
		{"unlimited first", "testdata/hdf5_official/tdset2.h5", "/dset1", []uint64{10, 20}, []uint64{Unlimited, 20}},
		// DATASPACE SIMPLE { ( 30, 10 ) / ( 30, H5S_UNLIMITED ) }
		{"unlimited last", "testdata/hdf5_official/tdset2.h5", "/dset2", []uint64{30, 10}, []uint64{30, Unlimited}},
		// DATASPACE SIMPLE { ( 1000 ) / ( 1000 ) }
		{"chunked", "testdata/gzip_test.h5", "/shuffled_compressed", []uint64{1000}, []uint64{1000}},
		// DATASPACE SIMPLE { ( 5, 10, 10 ) / ( H5S_UNLIMITED, 10, 10 ) }
		{"unlimited 3D", "testdata/hdf5_official/a.h5", "/A", []uint64{5, 10, 10}, []uint64{Unlimited, 10, 10}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ds := openDataset(t, tt.file, tt.dataset)

			shape, err := ds.Shape()
			require.NoError(t, err)
			assert.Equal(t, tt.shape, shape)

			maxShape, err := ds.MaxShape()
			require.NoError(t, err)
			assert.Equal(t, tt.maxShape, maxShape)
		})
	}
}

// TestDatasetShape_Scalar: a scalar dataspace has rank 0, as
// H5Sget_simple_extent_dims reports it; the slices are empty, not nil.
// h5dump -H: DATATYPE H5T_STD_I32LE, DATASPACE SCALAR. (The writer cannot
// create a scalar dataset, so the file comes from the C test suite.)
func TestDatasetShape_Scalar(t *testing.T) {
	_, ds := openDataset(t, "testdata/hdf5_official/test_ld.h5", "/DSET_SCALAR")

	shape, err := ds.Shape()
	require.NoError(t, err)
	assert.NotNil(t, shape)
	assert.Empty(t, shape)

	maxShape, err := ds.MaxShape()
	require.NoError(t, err)
	assert.NotNil(t, maxShape)
	assert.Empty(t, maxShape)
}

func TestDatasetDatatype(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		dataset string
		want    DatatypeInfo
	}{
		{
			// H5T_STD_I32BE
			name: "big-endian integer", file: "testdata/hdf5_official/tdset.h5", dataset: "/dset1",
			want: metaInt32Info(OrderBigEndian),
		},
		{
			// H5T_IEEE_F64BE
			name: "big-endian float", file: "testdata/hdf5_official/tdset.h5", dataset: "/dset2",
			want: DatatypeInfo{Class: ClassFloat, Size: 8, Order: OrderBigEndian},
		},
		{
			// H5T_IEEE_F32LE
			name: "float32", file: "testdata/gzip_test.h5", dataset: "/shuffled_compressed",
			want: DatatypeInfo{Class: ClassFloat, Size: 4, Order: OrderLittleEndian},
		},
		{
			// H5T_STRING { STRSIZE 11; STRPAD H5T_STR_SPACEPAD; CSET H5T_CSET_ASCII; }
			name: "fixed string", file: "testdata/hdf5_official/tstr.h5", dataset: "/string2",
			want: DatatypeInfo{Class: ClassString, Size: 11, Order: OrderNone, Padding: PaddingSpacePad, Charset: CharsetASCII},
		},
		{
			// H5T_STRING { STRSIZE H5T_VARIABLE; STRPAD H5T_STR_NULLTERM; CSET H5T_CSET_ASCII; }
			// Stored as a 16-byte vlen: 4-byte length + 12-byte global heap ID
			// (H5Tvlen.c:298-300). C makes the parent of a VL string an unsigned
			// char in the writer's native order (H5T.c:4945-4948) and
			// H5Tget_order defers to it (H5Torder.c:114-116); this file was
			// written on a little-endian host, so the order is little-endian.
			name: "variable-length string", file: "testdata/hdf5_official/tvlstr.h5", dataset: "/Dataset1",
			want: DatatypeInfo{
				Class: ClassString, Size: 16, Order: OrderLittleEndian,
				Padding: PaddingNullTerm, Charset: CharsetASCII, VariableString: true,
			},
		},
		{
			// H5T_ARRAY { [4] H5T_STD_I32LE }
			name: "array", file: "testdata/hdf5_official/tarray1.h5", dataset: "/Dataset1",
			want: DatatypeInfo{
				Class: ClassArray, Size: 16, Order: OrderLittleEndian, Signed: true,
				ArrayDims: []uint64{4}, Base: metaPtr(metaInt32Info(OrderLittleEndian)),
			},
		},
		{
			// H5T_VLEN { H5T_IEEE_F32LE }
			name: "vlen sequence", file: "testdata/hdf5_official/tvldtypes1.h5", dataset: "/Dataset2.0",
			want: DatatypeInfo{
				Class: ClassVLen, Size: 16, Order: OrderLittleEndian,
				Base: &DatatypeInfo{Class: ClassFloat, Size: 4, Order: OrderLittleEndian},
			},
		},
		{
			// h5ls -v: struct { "id" +0 native int; "temperature" +4 native float;
			// "pressure" +8 native double; "name" +16 10-byte null-padded ASCII string } 26 bytes
			name: "compound", file: "testdata/compound_test.h5", dataset: "/matrix",
			want: DatatypeInfo{
				Class: ClassCompound, Size: 26, Order: OrderLittleEndian,
				Members: []CompoundMemberInfo{
					{Name: "id", Offset: 0, Type: metaInt32Info(OrderLittleEndian)},
					{Name: "temperature", Offset: 4, Type: DatatypeInfo{Class: ClassFloat, Size: 4, Order: OrderLittleEndian}},
					{Name: "pressure", Offset: 8, Type: DatatypeInfo{Class: ClassFloat, Size: 8, Order: OrderLittleEndian}},
					{Name: "name", Offset: 16, Type: DatatypeInfo{
						Class: ClassString, Size: 10, Order: OrderNone, Padding: PaddingNullPad, Charset: CharsetASCII,
					}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ds := openDataset(t, tt.file, tt.dataset)

			got, err := ds.Datatype()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDatasetDatatype_CompoundOrder checks the compound byte order rule of
// H5T_get_order (H5Torder.c:118-152): members of order NONE are ignored, the
// first other member order wins, and differing orders give MIXED.
func TestDatasetDatatype_CompoundOrder(t *testing.T) {
	// tstr.h5 comp1: { H5T_ARRAY { [8][10] H5T_STD_I32BE } "int_array";
	// H5T_ARRAY { [3][4] H5T_STRING { STRSIZE 32; ... } } "string" }.
	_, ds := openDataset(t, "testdata/hdf5_official/tstr.h5", "/comp1")
	got, err := ds.Datatype()
	require.NoError(t, err)
	assert.Equal(t, ClassCompound, got.Class)
	assert.Equal(t, OrderBigEndian, got.Order, "the string member (order NONE) is ignored")
	require.Len(t, got.Members, 2)
	assert.Equal(t, []uint64{8, 10}, got.Members[0].Type.ArrayDims)
	assert.Equal(t, []uint64{3, 4}, got.Members[1].Type.ArrayDims)
	// H5Tget_strpad defers to the parent until it finds the string
	// (H5Tstrpad.c:50-51): STRPAD H5T_STR_SPACEPAD.
	assert.Equal(t, PaddingSpacePad, got.Members[1].Type.Padding)
	assert.Equal(t, uint64(32), got.Members[1].Type.Base.Size)

	// vlen_strings.h5 compound_with_vlen: { H5T_STD_I32LE "id"; H5T_STRING
	// { STRSIZE H5T_VARIABLE; CSET H5T_CSET_UTF8 } "name"; H5T_IEEE_F64LE "value" }.
	_, ds = openDataset(t, "testdata/vlen_strings.h5", "/compound_with_vlen")
	got, err = ds.Datatype()
	require.NoError(t, err)
	assert.Equal(t, OrderLittleEndian, got.Order)
	require.Len(t, got.Members, 3)
	name := got.Members[1].Type
	assert.Equal(t, ClassString, name.Class)
	assert.True(t, name.VariableString)
	assert.Equal(t, CharsetUTF8, name.Charset)
	assert.Equal(t, PaddingNullTerm, name.Padding)
}

// TestDatasetDatatype_Enum writes an enumeration with the public API and
// reads its members back. h5dump -H of the same file shows
// H5T_ENUM { H5T_STD_I16LE; "NEG" -2; "LOW" 1; "HIGH" 300; }.
func TestDatasetDatatype_Enum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enum.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/e", EnumInt16, []uint64{3},
		WithEnumValues([]string{"NEG", "LOW", "HIGH"}, []int64{-2, 1, 300}))
	require.NoError(t, err)
	require.NoError(t, dw.Write([]int16{-2, 1, 300}))
	require.NoError(t, fw.Close())

	_, ds := openDataset(t, path, "/e")
	got, err := ds.Datatype()
	require.NoError(t, err)

	base := DatatypeInfo{Class: ClassInteger, Size: 2, Order: OrderLittleEndian, Signed: true}
	assert.Equal(t, DatatypeInfo{
		Class: ClassEnum, Size: 2, Order: OrderLittleEndian, Signed: true,
		EnumMembers: []EnumMemberInfo{{Name: "NEG", Value: -2}, {Name: "LOW", Value: 1}, {Name: "HIGH", Value: 300}},
		Base:        &base,
	}, got)
}

// TestDatasetDatatype_CFixtures covers the classes written by the C library's
// own test generators. h5diff_dset1.h5 comes from h5diffgentest.c; h5ls -v
// shows the types (h5dump of 1.14.6 stops at the HDF5 2.0 complex dataset).
func TestDatasetDatatype_CFixtures(t *testing.T) {
	const dset1 = "testdata/hdf5_official/h5diff_dset1.h5"
	nativeInt := metaInt32Info(OrderLittleEndian)
	nativeFloat := DatatypeInfo{Class: ClassFloat, Size: 4, Order: OrderLittleEndian}

	tests := []struct {
		name    string
		file    string
		dataset string
		want    DatatypeInfo
	}{
		// enum native int { RED = 0, GREEN = 1 }
		{"enum", dset1, "/g1/enum", DatatypeInfo{
			Class: ClassEnum, Size: 4, Order: OrderLittleEndian, Signed: true,
			EnumMembers: []EnumMemberInfo{{Name: "RED", Value: 0}, {Name: "GREEN", Value: 1}},
			Base:        &nativeInt,
		}},
		// 1-byte opaque type (tag = "1-byte opaque type"), h5diffgentest.c:5725-5726.
		{"opaque", dset1, "/g1/opaque", DatatypeInfo{
			Class: ClassOpaque, Size: 1, Order: OrderNone, Tag: "1-byte opaque type",
		}},
		// native 8-bit field (H5T_NATIVE_B8).
		{"bitfield", dset1, "/g1/bitfield", DatatypeInfo{
			Class: ClassBitfield, Size: 1, Order: OrderLittleEndian,
		}},
		// H5Tcomplex_create(H5T_NATIVE_FLOAT), h5diffgentest.c:5897: two floats.
		{"complex", dset1, "/g1/complex", DatatypeInfo{
			Class: ClassComplex, Size: 8, Order: OrderLittleEndian, Base: &nativeFloat,
		}},
		// H5T_IEEE_F32BE
		{"float big-endian", "testdata/hdf5_official/be_data.h5", "/Fletcher_float_data_be", DatatypeInfo{
			Class: ClassFloat, Size: 4, Order: OrderBigEndian,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ds := openDataset(t, tt.file, tt.dataset)

			got, err := ds.Datatype()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDatasetLayout(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		dataset string
		want    LayoutInfo
	}{
		// STORAGE_LAYOUT { CONTIGUOUS } FILTERS { NONE }
		{"contiguous", "testdata/hdf5_official/tdset.h5", "/dset1", LayoutInfo{Class: LayoutContiguous}},
		// STORAGE_LAYOUT { COMPACT }
		{"compact", "testdata/hdf5_official/h5repack_layout.h5", "/dset_compact", LayoutInfo{Class: LayoutCompact}},
		// STORAGE_LAYOUT { CHUNKED ( 20, 10 ) } FILTERS { NONE }
		{
			"chunked", "testdata/hdf5_official/h5repack_layout.h5", "/dset_chunk",
			LayoutInfo{Class: LayoutChunked, ChunkDims: []uint64{20, 10}},
		},
		// CHUNKED ( 100 ) FILTERS { PREPROCESSING SHUFFLE COMPRESSION DEFLATE { LEVEL 9 } }.
		// H5Pset_shuffle and H5Pset_deflate add their filters as
		// H5Z_FLAG_OPTIONAL (H5Pdcpl.c:2963, H5Pocpl.c:1065).
		{
			"shuffle and deflate", "testdata/gzip_test.h5", "/shuffled_compressed",
			LayoutInfo{Class: LayoutChunked, ChunkDims: []uint64{100}, Filters: []FilterInfo{
				{ID: FilterShuffle, Name: "shuffle", Optional: true, ClientData: []uint32{4}},
				{ID: FilterDeflate, Name: "deflate", Optional: true, ClientData: []uint32{9}},
			}},
		},
		// CHUNKED ( 2 ) FILTERS { COMPRESSION DEFLATE { LEVEL 1 } }
		{
			"deflate level 1", "testdata/hdf5_official/h5copytst.h5", "/compressed",
			LayoutInfo{Class: LayoutChunked, ChunkDims: []uint64{2}, Filters: []FilterInfo{
				{ID: FilterDeflate, Name: "deflate", Optional: true, ClientData: []uint32{1}},
			}},
		},
		// CHUNKED ( 4, 10, 10 ) FILTERS { NONE }, MaxShape ( H5S_UNLIMITED, 10, 10 )
		{
			"chunked 3D", "testdata/hdf5_official/a.h5", "/A",
			LayoutInfo{Class: LayoutChunked, ChunkDims: []uint64{4, 10, 10}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ds := openDataset(t, tt.file, tt.dataset)

			got, err := ds.Layout()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDatasetLayout_AllFilters: tfilters.h5 "all" has CHUNKED ( 10, 5 ) and
// FILTERS { PREPROCESSING SHUFFLE; COMPRESSION SZIP; COMPRESSION DEFLATE
// { LEVEL 5 }; CHECKSUM FLETCHER32; COMPRESSION NBIT }, in that order.
func TestDatasetLayout_AllFilters(t *testing.T) {
	_, ds := openDataset(t, "testdata/hdf5_official/tfilters.h5", "/all")

	got, err := ds.Layout()
	require.NoError(t, err)
	assert.Equal(t, LayoutChunked, got.Class)
	assert.Equal(t, []uint64{10, 5}, got.ChunkDims)

	ids := make([]FilterID, len(got.Filters))
	names := make([]string, len(got.Filters))
	for i, f := range got.Filters {
		ids[i] = f.ID
		names[i] = f.Name
	}
	assert.Equal(t, []FilterID{FilterShuffle, FilterSZIP, FilterDeflate, FilterFletcher32, FilterNBit}, ids)
	assert.Equal(t, []string{"shuffle", "szip", "deflate", "fletcher32", "nbit"}, names)
	assert.Equal(t, []uint32{5}, got.Filters[2].ClientData, "DEFLATE { LEVEL 5 }")
}

// TestDatasetMetadata_Copies: the results are copies, so a caller that
// changes them does not change what the next call (or a read) sees.
func TestDatasetMetadata_Copies(t *testing.T) {
	_, ds := openDataset(t, "testdata/hdf5_official/tdset2.h5", "/dset1")

	shape, err := ds.Shape()
	require.NoError(t, err)
	shape[0] = 99
	maxShape, err := ds.MaxShape()
	require.NoError(t, err)
	maxShape[0] = 99
	layout, err := ds.Layout()
	require.NoError(t, err)
	layout.ChunkDims[0] = 99

	shape, err = ds.Shape()
	require.NoError(t, err)
	assert.Equal(t, []uint64{10, 20}, shape)
	maxShape, err = ds.MaxShape()
	require.NoError(t, err)
	assert.Equal(t, []uint64{Unlimited, 20}, maxShape)
	layout, err = ds.Layout()
	require.NoError(t, err)
	assert.Equal(t, []uint64{5, 5}, layout.ChunkDims)

	_, ds = openDataset(t, "testdata/compound_test.h5", "/matrix")
	dt, err := ds.Datatype()
	require.NoError(t, err)
	dt.Members[0].Name = "changed"
	dt, err = ds.Datatype()
	require.NoError(t, err)
	assert.Equal(t, "id", dt.Members[0].Name)
}

// TestDatasetInfo_FromDescriptor: Info is a view of the validated descriptor
// (review-062 L2). The text keeps its former shape.
func TestDatasetInfo_FromDescriptor(t *testing.T) {
	tests := []struct {
		file, dataset, want string
	}{
		{"testdata/hdf5_official/tdset.h5", "/dset1", "Dataset: integer (size=4 bytes), 2D array [10 x 20], contiguous"},
		{"testdata/hdf5_official/tarray1.h5", "/Dataset1", "Dataset: array (size=16 bytes), 1D array [4], contiguous"},
		{"testdata/hdf5_official/tvlstr.h5", "/Dataset1", "Dataset: string (size=16 bytes), 1D array [4], contiguous"},
		{"testdata/gzip_test.h5", "/shuffled_compressed", "Dataset: float (size=4 bytes), 1D array [1000], chunked (chunks=[100 4])"},
	}

	for _, tt := range tests {
		_, ds := openDataset(t, tt.file, tt.dataset)
		info, err := ds.Info()
		require.NoError(t, err)
		assert.Contains(t, info, tt.want)
	}
}

// TestDatasetMetadata_Error: an invalid descriptor fails every accessor with
// the descriptor's error, as H5Dopen fails before any H5Dget_* can run.
func TestDatasetMetadata_Error(t *testing.T) {
	// A dataset whose element count overflows 64 bits, which the descriptor
	// rejects as C does (H5Dcontig.c:392-396).
	path := writeFloat64Dataset(t, []uint64{2, 2})
	path = patchDataspaceDims(t, path, "/x", []uint64{1 << 32, 1 << 32})
	_, ds := openDataset(t, path, "/x")

	_, err := ds.Shape()
	require.ErrorIs(t, err, ErrCorrupt)
	_, err = ds.MaxShape()
	require.ErrorIs(t, err, ErrCorrupt)
	_, err = ds.Datatype()
	require.ErrorIs(t, err, ErrCorrupt)
	_, err = ds.Layout()
	require.ErrorIs(t, err, ErrCorrupt)
	_, err = ds.Info()
	assert.True(t, errors.Is(err, ErrCorrupt), "Info: %v", err)
}

func metaPtr[T any](v T) *T { return &v }

// Fixed-point class bit field flags as the file stores them (H5Odtype.c:171,
// :174). The tests spell the bytes out instead of sharing the decoder's
// constants, so a wrong constant there cannot hide in the fixtures.
const (
	testFlagBigEndian = 0x01
	testFlagSigned    = 0x08
)

// metaFixed is a fixed-point datatype message as the decoder returns it.
func metaFixed(size, flags uint32, bitOffset, precision uint16) *core.DatatypeMessage {
	return &core.DatatypeMessage{
		Class: core.DatatypeFixed, Version: 1, Size: size, ClassBitField: flags,
		BitOffset: bitOffset, Precision: precision,
	}
}

// TestMetaEnumMembers_BigEndianNegative: a big-endian signed int16 base, as
// H5Tenum_create(H5T_STD_I16BE) stores it; values are decoded in the base's
// byte order and sign-extended (H5Tget_member_value copies the base bytes).
func TestMetaEnumMembers_BigEndianNegative(t *testing.T) {
	dt := &core.DatatypeMessage{
		Class: core.DatatypeEnum, Version: 1, Size: 2,
		Base:       metaFixed(2, testFlagBigEndian|testFlagSigned, 0, 16),
		EnumNames:  []string{"A", "B", "C"},
		EnumValues: []byte{0xFF, 0xFF, 0xFE, 0xD4, 0x00, 0x07}, // -1, -300, 7 big-endian.
	}

	got, err := metaDatatypeInfo(dt)
	require.NoError(t, err)
	assert.Equal(t, OrderBigEndian, got.Order)
	assert.True(t, got.Signed)
	assert.Equal(t, []EnumMemberInfo{{Name: "A", Value: -1}, {Name: "B", Value: -300}, {Name: "C", Value: 7}}, got.EnumMembers)

	// An unsigned 16-bit base whose 12 significant bits start at bit 4; the
	// low 4 bits are set padding.
	dt.Base = metaFixed(2, 0, 4, 12)
	dt.EnumNames = []string{"X"}
	dt.EnumValues = []byte{0xBF, 0xFF}
	got, err = metaDatatypeInfo(dt)
	require.NoError(t, err)
	assert.Equal(t, []EnumMemberInfo{{Name: "X", Value: 0xFFB}}, got.EnumMembers)
}

// TestMetaEnumMembers_WideBase: values of a base wider than 64 bits do not
// fit EnumMemberInfo.Value; Datatype says so instead of truncating them.
func TestMetaEnumMembers_WideBase(t *testing.T) {
	dt := &core.DatatypeMessage{
		Class: core.DatatypeEnum, Version: 1, Size: 16,
		Base:       metaFixed(16, 0, 0, 128),
		EnumNames:  []string{"A"},
		EnumValues: make([]byte, 16),
	}

	_, err := metaDatatypeInfo(dt)
	require.ErrorIs(t, err, ErrNotSupported, "valid HDF5 this library cannot describe")
	assert.Contains(t, err.Error(), "not supported")
}

// TestMetaIntegerValue_OffsetPrecision: only the precision bits at the bit
// offset are significant (H5T_atomic_t offset and prec); the bits around
// them are padding and must not leak into the value.
func TestMetaIntegerValue_OffsetPrecision(t *testing.T) {
	// Bits 4..15 hold 0xFFB (-5 as 12-bit two's complement); bits 0..3 and
	// 16..31 are set padding.
	raw := []byte{0xBF, 0xFF, 0xCD, 0xAB}

	assert.Equal(t, int64(-5), metaIntegerValue(raw, false, true, 4, 12), "signed")
	assert.Equal(t, int64(0xFFB), metaIntegerValue(raw, false, false, 4, 12), "unsigned")

	// The same value big-endian.
	be := []byte{0xAB, 0xCD, 0xFF, 0xBF}
	assert.Equal(t, int64(-5), metaIntegerValue(be, true, true, 4, 12), "signed big-endian")

	// A positive 12-bit value is not sign-extended.
	assert.Equal(t, int64(0x7FB), metaIntegerValue([]byte{0xB0, 0x7F}, false, true, 4, 12), "positive")
}

// TestMetaOrder_Compound mirrors the compound branch of H5T_get_order
// (H5Torder.c:118-152): members of order NONE are skipped, the first other
// order is the answer, and a member with a different order gives MIXED.
func TestMetaOrder_Compound(t *testing.T) {
	str := &core.DatatypeMessage{Class: core.DatatypeString, Version: 1, Size: 8}
	le := metaFixed(4, 0, 0, 32)
	be := metaFixed(4, testFlagBigEndian, 0, 32)
	compound := func(types ...*core.DatatypeMessage) *core.DatatypeMessage {
		dt := &core.DatatypeMessage{Class: core.DatatypeCompound, Version: 1}
		for i, typ := range types {
			dt.Members = append(dt.Members, core.CompoundMember{Name: string(rune('a' + i)), Offset: uint32(4 * i), Type: typ})
		}
		return dt
	}

	order := func(dt *core.DatatypeMessage) ByteOrder {
		info, err := metaDatatypeInfo(dt)
		require.NoError(t, err)
		return info.Order
	}

	assert.Equal(t, OrderMixed, order(compound(le, be)), "LE + BE")
	assert.Equal(t, OrderBigEndian, order(compound(str, be)), "string + BE")
	assert.Equal(t, OrderNone, order(compound(str)), "strings only")
}

// TestMetaOrder_FloatVAX: the decoder reads VAX order (bits 0 and 6) only
// from version 3 messages (H5Odtype.c:193-201); an older message with bit 6
// set is ordinary big-endian.
func TestMetaOrder_FloatVAX(t *testing.T) {
	float := func(version uint8, flags uint32) *core.DatatypeMessage {
		return &core.DatatypeMessage{Class: core.DatatypeFloat, Version: version, Size: 4, ClassBitField: flags}
	}

	order := func(dt *core.DatatypeMessage) ByteOrder {
		info, err := metaDatatypeInfo(dt)
		require.NoError(t, err)
		return info.Order
	}

	assert.Equal(t, OrderVAX, order(float(3, 0x41)), "version 3, bits 0 and 6")
	assert.Equal(t, OrderBigEndian, order(float(1, 0x41)), "version 1, bits 0 and 6")
	assert.Equal(t, OrderLittleEndian, order(float(1, 0x40)), "version 1, bit 6 only")
}

// TestMetaCoreEnumValues: metaDatatypeInfo converts the decoder's order,
// padding and character set by value, which is right only while both sides
// hold the C enum values (H5Tpublic.h H5T_order_t, H5T_str_t, H5T_cset_t).
func TestMetaCoreEnumValues(t *testing.T) {
	orders := map[core.TypeOrder]ByteOrder{
		core.OrderLittleEndian: OrderLittleEndian,
		core.OrderBigEndian:    OrderBigEndian,
		core.OrderVAX:          OrderVAX,
		core.OrderMixed:        OrderMixed,
		core.OrderNone:         OrderNone,
	}
	for c, want := range orders {
		assert.Equal(t, want, ByteOrder(c))
	}

	paddings := map[core.StringPadding]StringPadding{
		core.StringNullTerm: PaddingNullTerm,
		core.StringNullPad:  PaddingNullPad,
		core.StringSpacePad: PaddingSpacePad,
	}
	for c, want := range paddings {
		assert.Equal(t, want, StringPadding(c))
	}

	assert.Equal(t, CharsetASCII, Charset(core.CharsetASCII))
	assert.Equal(t, CharsetUTF8, Charset(core.CharsetUTF8))
}

// TestDatasetMaxShape_NoStoredMax: a dataspace without stored maximum
// dimensions reports its current dimensions as the maximum (H5S.c:968-972).
// The fixture is C-written: our writer stores maximum dimensions as C does.
func TestDatasetMaxShape_NoStoredMax(t *testing.T) {
	_, ds := openDataset(t, "testdata/hdf5_official/taindices.h5", "/2d")

	desc, err := ds.descriptor()
	require.NoError(t, err)
	require.Nil(t, desc.Dataspace().MaxDims, "taindices.h5 /2d stores no maximum dimensions")

	maxShape, err := ds.MaxShape()
	require.NoError(t, err)
	assert.Equal(t, []uint64{10, 10}, maxShape)
}

// TestFilterID_String: predefined filters print their C names; others print
// their number.
func TestFilterID_String(t *testing.T) {
	assert.Equal(t, "deflate", FilterDeflate.String())
	assert.Equal(t, "scaleoffset", FilterScaleOffset.String())
	assert.Equal(t, "filter(32000)", FilterID(32000).String())
}
