package hdf5

// C-library interop tests: files written by this library must be readable by
// the official HDF5 C tools (h5dump, h5diff, h5repack). Tests skip when the
// tools are not in PATH; the c-interop CI job installs them.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func h5tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not in PATH — install HDF5 C tools to run interop tests", name)
	}
	return path
}

func runH5Err(tool string, args ...string) (string, error) {
	out, err := exec.Command(tool, args...).CombinedOutput()
	return string(out), err
}

func runH5(t *testing.T, tool string, args ...string) string {
	t.Helper()
	out, err := runH5Err(tool, args...)
	require.NoError(t, err, "%s %s failed:\n%s", tool, strings.Join(args, " "), out)
	return out
}

// TestCInterop_WriteMatrix writes files covering the write feature matrix and
// verifies that the official h5dump reads each one and prints the expected
// datatypes, structure, filters, and data values.
func TestCInterop_WriteMatrix(t *testing.T) {
	h5dump := h5tool(t, "h5dump")

	cases := []struct {
		name  string
		opts  []interface{} // CreateForWrite options
		build func(t *testing.T, fw *FileWriter)
		want  []string // substrings required in `h5dump -p` output
	}{
		{
			name: "numeric_types",
			build: func(t *testing.T, fw *FileWriter) {
				write := func(name string, dt Datatype, data interface{}) {
					ds, err := fw.CreateDataset(name, dt, []uint64{4})
					require.NoError(t, err)
					require.NoError(t, ds.Write(data))
				}
				write("/i8", Int8, []int8{1, 2, 3, 4})
				write("/i16", Int16, []int16{1, 2, 3, 4})
				write("/i32", Int32, []int32{1, 2, 3, 4})
				write("/i64", Int64, []int64{1, 2, 3, 4})
				write("/u8", Uint8, []uint8{1, 2, 3, 4})
				write("/u16", Uint16, []uint16{1, 2, 3, 4})
				write("/u32", Uint32, []uint32{1, 2, 3, 4})
				write("/u64", Uint64, []uint64{1, 2, 3, 4})
				write("/f32", Float32, []float32{1.5, 2.5, 3.5, 4.5})
				write("/f64", Float64, []float64{1.5, 2.5, 3.5, 4.5})
			},
			want: []string{
				"H5T_STD_I8LE", "H5T_STD_I16LE", "H5T_STD_I32LE", "H5T_STD_I64LE",
				"H5T_STD_U8LE", "H5T_STD_U16LE", "H5T_STD_U32LE", "H5T_STD_U64LE",
				"H5T_IEEE_F32LE", "H5T_IEEE_F64LE",
				"(0): 1, 2, 3, 4",
				"(0): 1.5, 2.5, 3.5, 4.5",
			},
		},
		{
			name: "multidim",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/m", Float64, []uint64{2, 3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]float64{1.5, 2.5, 3.5, 4.5, 5.5, 6.5}))
			},
			want: []string{"SIMPLE { ( 2, 3 )", "6.5"},
		},
		{
			name: "chunked_gzip",
			build: func(t *testing.T, fw *FileWriter) {
				data := make([]int32, 100)
				for i := range data {
					data[i] = int32(i)
				}
				ds, err := fw.CreateDataset("/z", Int32, []uint64{100},
					WithChunkDims([]uint64{25}), WithGZIPCompression(6))
				require.NoError(t, err)
				require.NoError(t, ds.Write(data))
			},
			want: []string{"CHUNKED", "DEFLATE", "(0): 0, 1, 2", "99"},
		},
		{
			name: "shuffle_fletcher32",
			build: func(t *testing.T, fw *FileWriter) {
				data := make([]float64, 50)
				for i := range data {
					data[i] = float64(i) + 0.5
				}
				ds, err := fw.CreateDataset("/sf", Float64, []uint64{50},
					WithChunkDims([]uint64{10}),
					WithShuffle(), WithGZIPCompression(4), WithFletcher32())
				require.NoError(t, err)
				require.NoError(t, ds.Write(data))
			},
			want: []string{"SHUFFLE", "FLETCHER32", "DEFLATE", "0.5", "49.5"},
		},
		{
			name: "fixed_string",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/words", String, []uint64{3}, WithStringSize(8))
				require.NoError(t, err)
				require.NoError(t, ds.Write([]string{"alpha", "bravo", "charlie"}))
			},
			want: []string{"H5T_STRING", "alpha", "bravo", "charlie"},
		},
		{
			name: "vlen_string",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/vs", VLenString, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]string{"x", "medium", "a much longer string"}))
			},
			want: []string{"H5T_VARIABLE", "medium", "a much longer string"},
		},
		{
			name: "vlen_ragged",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/ragged", VLenInt32, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([][]int32{{1, 2}, {3, 4, 5}, {6}}))
			},
			want: []string{"H5T_VLEN", "H5T_STD_I32LE"},
		},
		{
			name: "groups_attributes",
			build: func(t *testing.T, fw *FileWriter) {
				g, err := fw.CreateGroup("/outer")
				require.NoError(t, err)
				require.NoError(t, g.WriteAttribute("desc", "top level"))
				_, err = fw.CreateGroup("/outer/inner")
				require.NoError(t, err)
				ds, err := fw.CreateDataset("/outer/inner/data", Int32, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{7, 8, 9}))
				require.NoError(t, ds.WriteAttribute("units", "kelvin"))
				require.NoError(t, ds.WriteAttribute("scale", int32(42)))
				require.NoError(t, ds.WriteAttribute("channels", []string{"RGB", "IR"}))
			},
			want: []string{
				`GROUP "outer"`, `GROUP "inner"`, `DATASET "data"`,
				`ATTRIBUTE "desc"`, "top level",
				`ATTRIBUTE "units"`, "kelvin",
				`ATTRIBUTE "scale"`, "42",
				`ATTRIBUTE "channels"`, "RGB", "IR",
				"(0): 7, 8, 9",
			},
		},
		{
			name: "hard_link",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{1, 2}))
				require.NoError(t, fw.CreateHardLink("/hard", "/target"))
			},
			want: []string{`DATASET "hard"`, `DATASET "target"`, "HARDLINK", "(0): 1, 2"},
		},
		{
			name: "soft_link",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{1, 2}))
				require.NoError(t, fw.CreateSoftLink("/soft", "/target"))
			},
			want: []string{"SOFTLINK", `LINKTARGET "/target"`, "(0): 1, 2"},
		},
		{
			name: "enum",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/days", EnumInt8, []uint64{3},
					WithEnumValues([]string{"MON", "TUE", "WED"}, []int64{0, 1, 2}))
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int8{0, 1, 2}))
			},
			want: []string{"H5T_ENUM", "MON", "TUE", "WED", "(0): MON, TUE, WED"},
		},
		{
			name: "array_type",
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/vecs", ArrayInt32, []uint64{2},
					WithArrayDims([]uint64{3}))
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{1, 2, 3, 4, 5, 6}))
			},
			want: []string{"H5T_ARRAY", "H5T_STD_I32LE"},
		},
		{
			name: "superblock_v0",
			opts: []interface{}{WithSuperblockVersion(SuperblockV0)},
			build: func(t *testing.T, fw *FileWriter) {
				ds, err := fw.CreateDataset("/legacy", Float64, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]float64{1.5, 2.5, 3.5}))
			},
			want: []string{`DATASET "legacy"`, "(0): 1.5, 2.5, 3.5"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.name+".h5")
			fw, err := CreateForWrite(file, CreateTruncate, tc.opts...)
			require.NoError(t, err)
			tc.build(t, fw)
			require.NoError(t, fw.Close())

			out := runH5(t, h5dump, "--enable-error-stack", "-p", file)
			for _, want := range tc.want {
				require.Contains(t, out, want, "h5dump output missing %q:\n%s", want, out)
			}
		})
	}
}

// TestCInterop_RepackRoundTrip is the strongest interop check: the C library
// itself reads a Go-written file and rewrites it (h5repack), h5diff confirms
// both files hold identical data, and the Go reader reads the C-written copy
// back and verifies every value.
func TestCInterop_RepackRoundTrip(t *testing.T) {
	h5repack := h5tool(t, "h5repack")
	h5diff := h5tool(t, "h5diff")

	dir := t.TempDir()
	orig := filepath.Join(dir, "go_written.h5")
	repacked := filepath.Join(dir, "c_repacked.h5")

	floats := make([]float64, 200)
	ints := make([]int32, 200)
	for i := range floats {
		floats[i] = float64(i) * 0.25
		ints[i] = int32(i * 3)
	}

	fw, err := CreateForWrite(orig, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/g")
	require.NoError(t, err)
	dsF, err := fw.CreateDataset("/g/floats", Float64, []uint64{200},
		WithChunkDims([]uint64{50}), WithGZIPCompression(6))
	require.NoError(t, err)
	require.NoError(t, dsF.Write(floats))
	dsI, err := fw.CreateDataset("/g/ints", Int32, []uint64{200})
	require.NoError(t, err)
	require.NoError(t, dsI.Write(ints))
	require.NoError(t, dsI.WriteAttribute("units", "counts"))
	require.NoError(t, fw.Close())

	// C library reads the Go file and rewrites it.
	runH5(t, h5repack, orig, repacked)
	// C library confirms data equality between the two.
	runH5(t, h5diff, orig, repacked)

	wantI := make([]float64, len(ints))
	for i, v := range ints {
		wantI[i] = float64(v)
	}

	// Go reads both files back: the C-written copy proves we read C output,
	// the Go-written original pins writer/reader agreement on compressed
	// chunks (the gzip-vs-zlib bug hid here — nothing read Go-written
	// compressed data through Open()).
	for _, file := range []string{orig, repacked} {
		f, err := Open(file)
		require.NoError(t, err, file)

		datasets := map[string]*Dataset{}
		f.Walk(func(path string, obj Object) {
			if ds, ok := obj.(*Dataset); ok {
				datasets[path] = ds
			}
		})
		require.Contains(t, datasets, "/g/floats", file)
		require.Contains(t, datasets, "/g/ints", file)

		gotF, err := datasets["/g/floats"].Read()
		require.NoError(t, err, file)
		require.InDeltaSlice(t, floats, gotF, 1e-12, file)

		gotI, err := datasets["/g/ints"].Read()
		require.NoError(t, err, file)
		require.Equal(t, wantI, gotI, file)

		require.NoError(t, f.Close())
	}
}
