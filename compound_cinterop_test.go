package hdf5

// Regression tests for scigolib/hdf5#65: compound datatypes written through
// the internal compound path must be readable by the HDF5 C library and by
// our own reader. Before the datatype codec (TASK-064) the writer put the v3
// member count into the properties instead of the class bit field
// (H5Odtype.c:358), wrote 4-byte member offsets instead of
// H5VM_limit_enc_size(size) bytes (H5Odtype.c:353, :432-446), and the reader
// swallowed every member after a string member (properties length taken as
// "all remaining bytes").

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// compoundCase is one compound layout written through
// core.CreateCompoundTypeFromFields + FileWriter.CreateCompoundDataset.
type compoundCase struct {
	name   string
	fields func(t *testing.T) []core.CompoundFieldDef
	rows   []byte               // raw records, little-endian
	n      uint64               // number of records
	want   []core.CompoundValue // what ReadCompound must return
	dump   []string             // substrings h5dump must print
}

// basicType returns a little-endian member type; an integer is unsigned
// (CreateBasicDatatypeMessage), so ReadCompound returns it as uint32/uint64.
func basicType(t *testing.T, class core.DatatypeClass, size uint32) *core.DatatypeMessage {
	t.Helper()
	dt, err := core.CreateBasicDatatypeMessage(class, size)
	require.NoError(t, err)
	return dt
}

func putFixedString(b []byte, s string) {
	copy(b, s)
}

func compoundCases() []compoundCase {
	numeric := compoundCase{
		name: "int32_float64",
		fields: func(t *testing.T) []core.CompoundFieldDef {
			return []core.CompoundFieldDef{
				{Name: "id", Offset: 0, Type: basicType(t, core.DatatypeFixed, 4)},
				{Name: "value", Offset: 4, Type: basicType(t, core.DatatypeFloat, 8)},
			}
		},
		n: 2,
		want: []core.CompoundValue{
			{"id": uint32(1), "value": 0.5},
			{"id": uint32(2), "value": 1.5},
		},
		dump: []string{"H5T_COMPOUND", `"id"`, `"value"`, "0.5", "1.5"},
	}
	numeric.rows = make([]byte, 2*12)
	for i := 0; i < 2; i++ {
		b := numeric.rows[i*12:]
		binary.LittleEndian.PutUint32(b[0:], uint32(i+1))
		binary.LittleEndian.PutUint64(b[4:], math.Float64bits(float64(i)+0.5))
	}

	// The reporter's shape: {key string[16], value string[16]}.
	kv := compoundCase{
		name: "string_string",
		fields: func(t *testing.T) []core.CompoundFieldDef {
			return []core.CompoundFieldDef{
				{Name: "key", Offset: 0, Type: basicType(t, core.DatatypeString, 16)},
				{Name: "value", Offset: 16, Type: basicType(t, core.DatatypeString, 16)},
			}
		},
		n: 2,
		want: []core.CompoundValue{
			{"key": "alpha", "value": "first"},
			{"key": "beta", "value": "second"},
		},
		dump: []string{"H5T_COMPOUND", `"key"`, `"value"`, `"alpha"`, `"second"`},
	}
	kv.rows = make([]byte, 2*32)
	putFixedString(kv.rows[0:], "alpha")
	putFixedString(kv.rows[16:], "first")
	putFixedString(kv.rows[32:], "beta")
	putFixedString(kv.rows[48:], "second")

	stringFirst := compoundCase{
		name: "string_first",
		fields: func(t *testing.T) []core.CompoundFieldDef {
			return []core.CompoundFieldDef{
				{Name: "label", Offset: 0, Type: basicType(t, core.DatatypeString, 8)},
				{Name: "count", Offset: 8, Type: basicType(t, core.DatatypeFixed, 4)},
			}
		},
		n: 2,
		want: []core.CompoundValue{
			{"label": "row0", "count": uint32(10)},
			{"label": "row1", "count": uint32(11)},
		},
		dump: []string{"H5T_COMPOUND", `"label"`, `"count"`, `"row1"`, "11"},
	}
	stringFirst.rows = make([]byte, 2*12)
	for i := 0; i < 2; i++ {
		b := stringFirst.rows[i*12:]
		putFixedString(b, "row"+string(rune('0'+i)))
		binary.LittleEndian.PutUint32(b[8:], uint32(10+i))
	}

	nested := compoundCase{
		name: "nested",
		fields: func(t *testing.T) []core.CompoundFieldDef {
			pos, err := core.CreateCompoundTypeFromFields([]core.CompoundFieldDef{
				{Name: "x", Offset: 0, Type: basicType(t, core.DatatypeFloat, 8)},
				{Name: "y", Offset: 8, Type: basicType(t, core.DatatypeFloat, 8)},
			})
			require.NoError(t, err)
			return []core.CompoundFieldDef{
				{Name: "id", Offset: 0, Type: basicType(t, core.DatatypeFixed, 4)},
				{Name: "pos", Offset: 4, Type: pos},
			}
		},
		n: 2,
		want: []core.CompoundValue{
			{"id": uint32(1), "pos": core.CompoundValue{"x": 1.25, "y": 2.25}},
			{"id": uint32(2), "pos": core.CompoundValue{"x": 3.25, "y": 4.25}},
		},
		dump: []string{"H5T_COMPOUND", `"pos"`, `"x"`, `"y"`, "1.25", "4.25"},
	}
	nested.rows = make([]byte, 2*20)
	for i := 0; i < 2; i++ {
		b := nested.rows[i*20:]
		binary.LittleEndian.PutUint32(b[0:], uint32(i+1))
		binary.LittleEndian.PutUint64(b[4:], math.Float64bits(float64(2*i)+1.25))
		binary.LittleEndian.PutUint64(b[12:], math.Float64bits(float64(2*i)+2.25))
	}

	return []compoundCase{numeric, kv, stringFirst, nested}
}

// writeCompoundCase writes tc to a new file and returns its path.
func writeCompoundCase(t *testing.T, tc compoundCase) string {
	t.Helper()
	ct, err := core.CreateCompoundTypeFromFields(tc.fields(t))
	require.NoError(t, err)

	file := filepath.Join(t.TempDir(), tc.name+".h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateCompoundDataset("/data", ct, []uint64{tc.n})
	require.NoError(t, err)
	require.NoError(t, dw.WriteRaw(tc.rows))
	require.NoError(t, fw.Close())
	return file
}

// TestCompoundWrite_ReadBack writes each compound layout and reads it back with
// ReadCompound. Red before the codec for every layout with a string member that
// is not the last one.
func TestCompoundWrite_ReadBack(t *testing.T) {
	for _, tc := range compoundCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeCompoundCase(t, tc)

			f, err := Open(file)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()

			var ds *Dataset
			f.Walk(func(path string, obj Object) {
				if d, ok := obj.(*Dataset); ok && path == "/data" {
					ds = d
				}
			})
			require.NotNil(t, ds, "dataset /data not found")

			got, err := ds.ReadCompound()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestCInterop_CompoundDeprecatedMethod writes each compound layout through
// the deprecated CreateCompoundDataset and checks that the HDF5 C library
// (h5dump) decodes it. Red before the codec for every layout:
// "H5O__dtype_decode_helper(): invalid number of members: 0". Named
// TestCInterop_* so the c-interop CI job (go test -run TestCInterop) runs it.
func TestCInterop_CompoundDeprecatedMethod(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	for _, tc := range compoundCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeCompoundCase(t, tc)
			out := runH5(t, h5dump, "--enable-error-stack", file)
			for _, want := range tc.dump {
				require.Contains(t, out, want, "h5dump output missing %q:\n%s", want, out)
			}
		})
	}
}

// publicCompoundCase is one dataset written through the public compound API
// (NewCompoundType / CreateCompound / Write) and the h5dump output it must give.
type publicCompoundCase struct {
	name   string
	create func(t *testing.T, fw *FileWriter) *DatasetWriter
	rows   []map[string]any
	dump   []string
}

func publicCompoundCases() []publicCompoundCase {
	// The reporter's case (#65): {key string, value string}, fixed length.
	kv := func(t *testing.T, fw *FileWriter, opts ...DatasetOption) *DatasetWriter {
		ct, err := NewPackedCompoundType([]CompoundMember{
			{Name: "key", Type: FixedString{Size: 16}},
			{Name: "value", Type: FixedString{Size: 16}},
		})
		require.NoError(t, err)
		dw, err := fw.CreateCompound("/kv", ct, []uint64{4}, opts...)
		require.NoError(t, err)
		return dw
	}
	kvRows := []map[string]any{
		{"key": "alpha", "value": "first"},
		{"key": "beta", "value": "second"},
		{"key": "gamma", "value": "third"},
		{"key": "delta", "value": "fourth"},
	}
	kvDump := []string{
		"H5T_COMPOUND", `H5T_STRING`, "STRSIZE 16", "H5T_STR_NULLTERM", `"key"`, `"value"`,
		`"alpha"`, `"first"`, `"delta"`, `"fourth"`,
	}

	return []publicCompoundCase{
		{
			name:   "key_value",
			create: func(t *testing.T, fw *FileWriter) *DatasetWriter { return kv(t, fw) },
			rows:   kvRows,
			dump:   kvDump,
		},
		{
			name: "key_value_chunked_gzip_shuffle",
			create: func(t *testing.T, fw *FileWriter) *DatasetWriter {
				return kv(t, fw, WithChunkDims([]uint64{2}), WithShuffle(), WithGZIPCompression(6))
			},
			rows: kvRows,
			dump: append([]string{"CHUNKED ( 2 )", "SHUFFLE", "COMPRESSION DEFLATE { LEVEL 6 }"}, kvDump...),
		},
		{
			name: "key_value_extendible",
			create: func(t *testing.T, fw *FileWriter) *DatasetWriter {
				return kv(t, fw, WithChunkDims([]uint64{3}), WithMaxDims([]uint64{Unlimited}))
			},
			rows: kvRows,
			dump: append([]string{"H5S_UNLIMITED"}, kvDump...),
		},
		{
			// The #65 reporter's growing table: one row in a 64-row chunk,
			// then grown to four (a chunk may exceed the current extent of
			// an unlimited dimension, H5Dchunk.c:1078-1081).
			name: "key_value_growing",
			create: func(t *testing.T, fw *FileWriter) *DatasetWriter {
				ct, err := NewPackedCompoundType([]CompoundMember{
					{Name: "key", Type: FixedString{Size: 16}},
					{Name: "value", Type: FixedString{Size: 16}},
				})
				require.NoError(t, err)
				dw, err := fw.CreateCompound("/kv", ct, []uint64{1},
					WithChunkDims([]uint64{64}), WithMaxDims([]uint64{Unlimited}))
				require.NoError(t, err)
				require.NoError(t, dw.Write(kvRows[:1]))
				require.NoError(t, dw.Resize([]uint64{4}))
				return dw
			},
			rows: kvRows,
			dump: append([]string{"CHUNKED ( 64 )", "( 4 ) / ( H5S_UNLIMITED )"}, kvDump...),
		},
		{
			name: "deprecated_chunked_gzip",
			create: func(t *testing.T, fw *FileWriter) *DatasetWriter {
				str, err := core.NewStringDatatype(16, core.StringNullTerm, core.CharsetASCII)
				require.NoError(t, err)
				old, err := core.CreateCompoundTypeFromFields([]core.CompoundFieldDef{
					{Name: "key", Offset: 0, Type: str},
					{Name: "value", Offset: 16, Type: str},
				})
				require.NoError(t, err)
				dw, err := fw.CreateCompoundDataset("/kv", old, []uint64{4}, WithChunkDims([]uint64{2}), WithGZIPCompression(1))
				require.NoError(t, err)
				return dw
			},
			rows: kvRows,
			dump: append([]string{"CHUNKED ( 2 )", "COMPRESSION DEFLATE { LEVEL 1 }"}, kvDump...),
		},
		{
			name:   "member_types",
			create: createMemberTypesDataset,
			rows: []map[string]any{
				{
					"u16": uint16(65000), "i8": int8(-5), "pad": "Zoe", "space": "ab",
					"vec": []float32{0.5, 1.5}, "color": "green",
					"pos": map[string]any{"x": 1.25, "y": -2.5},
				},
				{
					"u16": 7, "i8": 127, "pad": "four", "space": "abcd",
					"vec": [2]float32{-1, 2}, "color": 1,
					"pos": map[string]any{"x": 0.0, "y": 3.0},
				},
			},
			dump: []string{
				"H5T_STD_U16LE", "H5T_STD_I8LE", "H5T_STR_NULLPAD", "H5T_CSET_UTF8", "H5T_STR_SPACEPAD",
				"H5T_ARRAY { [2] H5T_IEEE_F32LE }", "H5T_ENUM", `"red"`, `"green"`,
				// h5dump prints a null-padded value with its padding bytes.
				"65000", "-5", "127", `"Zoe\000\000"`, `"four\000"`, `"ab  "`, "0.5, 1.5", "-1, 2", "green", "red",
				"1.25", "-2.5",
			},
		},
	}
}

// createMemberTypesDataset creates a compound with gaps and padding and one
// member of every kind the public API builds.
func createMemberTypesDataset(t *testing.T, fw *FileWriter) *DatasetWriter {
	pos, err := NewPackedCompoundType([]CompoundMember{{Name: "x", Type: Float64}, {Name: "y", Type: Float64}})
	require.NoError(t, err)
	ct, err := NewCompoundType(64, []CompoundMember{
		{Name: "u16", Offset: 0, Type: Uint16},
		{Name: "i8", Offset: 3, Type: Int8},
		{Name: "pad", Offset: 4, Type: FixedString{Size: 5, Padding: PaddingNullPad, Charset: CharsetUTF8}},
		{Name: "space", Offset: 9, Type: FixedString{Size: 4, Padding: PaddingSpacePad}},
		{Name: "vec", Offset: 16, Type: ArrayType{Elem: Float32, Dims: []uint64{2}}},
		{Name: "color", Offset: 24, Type: EnumType{Base: Uint8, Names: []string{"red", "green"}, Values: []int64{1, 2}}},
		{Name: "pos", Offset: 32, Type: pos},
	})
	require.NoError(t, err)
	dw, err := fw.CreateCompound("/types", ct, []uint64{2})
	require.NoError(t, err)
	return dw
}

// TestCInterop_CompoundPublicAPI writes compound datasets through the public
// API, values through Write, and checks that h5dump decodes the datatype,
// the storage and every value.
func TestCInterop_CompoundPublicAPI(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	for _, tc := range publicCompoundCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.name+".h5")
			fw, err := CreateForWrite(file, CreateTruncate)
			require.NoError(t, err)
			require.NoError(t, tc.create(t, fw).Write(tc.rows))
			require.NoError(t, fw.Close())

			out := runH5(t, h5dump, "--enable-error-stack", "-p", file)
			for _, want := range tc.dump {
				require.Contains(t, out, want, "h5dump output missing %q:\n%s", want, out)
			}
		})
	}
}

// TestFloatAttribute_CInterop checks that float attributes written through
// WriteAttribute carry the IEEE layout the C library expects. A float
// datatype whose sign bit position is 0 overlaps the mantissa and is rejected
// by H5O__dtype_decode_helper ("mantissa and sign positions overlap",
// H5Odtype.c:264-267).
func TestFloatAttribute_CInterop(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	file := filepath.Join(t.TempDir(), "float_attr.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/d", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{7}))
	require.NoError(t, ds.WriteAttribute("gain", float64(1.5)))
	require.NoError(t, ds.WriteAttribute("weights", []float32{0.25, 0.75}))
	require.NoError(t, fw.Close())

	out := runH5(t, h5dump, "--enable-error-stack", file)
	for _, want := range []string{`ATTRIBUTE "gain"`, "H5T_IEEE_F64LE", "1.5", `ATTRIBUTE "weights"`, "H5T_IEEE_F32LE", "0.25, 0.75"} {
		require.Contains(t, out, want, "h5dump output missing %q:\n%s", want, out)
	}
}
