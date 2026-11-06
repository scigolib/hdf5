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
			{"id": int32(1), "value": 0.5},
			{"id": int32(2), "value": 1.5},
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
			{"label": "row0", "count": int32(10)},
			{"label": "row1", "count": int32(11)},
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
			{"id": int32(1), "pos": core.CompoundValue{"x": 1.25, "y": 2.25}},
			{"id": int32(2), "pos": core.CompoundValue{"x": 3.25, "y": 4.25}},
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

// TestCompoundWrite_CInterop writes each compound layout and checks that the
// HDF5 C library (h5dump) decodes it. Red before the codec for every layout:
// "H5O__dtype_decode_helper(): invalid number of members: 0".
func TestCompoundWrite_CInterop(t *testing.T) {
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
