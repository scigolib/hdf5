package hdf5_test

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// appendable creates an empty dataset of rows of shape row, extendible in its first dimension.
func appendable(t *testing.T, fw *hdf5.FileWriter, name string, dtype hdf5.Datatype, row, chunk []uint64) *hdf5.DatasetWriter {
	t.Helper()
	dims := append([]uint64{0}, row...)
	maxDims := append([]uint64{hdf5.Unlimited}, row...)
	ds, err := fw.CreateDataset(name, dtype, dims, hdf5.WithChunkDims(chunk), hdf5.WithMaxDims(maxDims))
	require.NoError(t, err)
	return ds
}

// ramp returns the float32 values of rows [from, from+n) of a dataset whose rows hold rowElems elements.
func ramp(from, n, rowElems int) []float32 {
	out := make([]float32, n*rowElems)
	for i := range out {
		out[i] = float32(from*rowElems + i)
	}
	return out
}

func readAll(t *testing.T, filename, path string) ([]uint64, []float64) {
	t.Helper()
	f, err := hdf5.Open(filename)
	require.NoError(t, err)
	defer f.Close()
	var ds *hdf5.Dataset
	f.Walk(func(p string, obj hdf5.Object) {
		if d, ok := obj.(*hdf5.Dataset); ok && p == path {
			ds = d
		}
	})
	require.NotNil(t, ds, "dataset %s not found", path)
	shape, err := ds.Shape()
	require.NoError(t, err)
	values, err := ds.Read()
	require.NoError(t, err)
	return shape, values
}

func requireRamp(t *testing.T, values []float64, n int) {
	t.Helper()
	require.Len(t, values, n)
	for i, v := range values {
		if v != float64(i) {
			require.Equal(t, float64(i), v, "element %d", i)
		}
	}
}

func TestAppendRows(t *testing.T) {
	cases := []struct {
		name    string
		row     []uint64
		chunk   []uint64
		packets []int // rows per AppendRows call
		flushAt int   // Flush after this many packets; -1 for never
	}{
		{name: "chunk spans the row", row: []uint64{13}, chunk: []uint64{10, 13}, packets: []int{7, 7, 7, 7, 2}, flushAt: -1},
		{name: "whole slabs per call", row: []uint64{13}, chunk: []uint64{10, 13}, packets: []int{10, 20, 10}, flushAt: -1},
		{name: "partial chunk in the row", row: []uint64{13}, chunk: []uint64{4, 5}, packets: []int{3, 1, 9, 6}, flushAt: -1},
		{name: "three dimensions", row: []uint64{3, 5}, chunk: []uint64{2, 2, 5}, packets: []int{1, 4, 2}, flushAt: -1},
		{name: "flushes between appends", row: []uint64{13}, chunk: []uint64{10, 13}, packets: []int{3, 4, 5, 15, 1}, flushAt: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "append.h5")
			fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
			require.NoError(t, err)
			ds := appendable(t, fw, "/data", hdf5.Float32, tc.row, tc.chunk)

			rowElems := 1
			for _, d := range tc.row {
				rowElems *= int(d)
			}
			rows := 0
			for i, n := range tc.packets {
				require.NoError(t, ds.AppendRows(ramp(rows, n, rowElems)))
				rows += n
				if tc.flushAt >= 0 && i%(tc.flushAt+1) == tc.flushAt {
					require.NoError(t, ds.Flush())
				}
			}
			require.NoError(t, fw.Close())

			shape, values := readAll(t, filename, "/data")
			require.Equal(t, append([]uint64{uint64(rows)}, tc.row...), shape)
			requireRamp(t, values, rows*rowElems)
		})
	}
}

// TestAppendRowsInt64 appends to a one-dimensional dataset of 64-bit integers, as a time axis is kept.
func TestAppendRowsInt64(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "times.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	ds := appendable(t, fw, "/times", hdf5.Int64, nil, []uint64{1000})

	const start = int64(1_762_000_000_000_000) // microseconds since the epoch
	var all []int64
	for p := 0; p < 25; p++ {
		packet := make([]int64, 97)
		for i := range packet {
			packet[i] = start + int64(len(all)+i)
		}
		all = append(all, packet...)
		require.NoError(t, ds.AppendRows(packet))
	}
	require.NoError(t, fw.Close())

	shape, values := readAll(t, filename, "/times")
	require.Equal(t, []uint64{uint64(len(all))}, shape)
	for i, v := range values {
		require.Equal(t, float64(all[i]), v, "element %d", i)
	}
}

// TestAppendRowsReadableAfterFlush reads a copy of the file taken after Flush, as a reader would find it
// if the writer stopped there without closing.
func TestAppendRowsReadableAfterFlush(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "live.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{8}, []uint64{10, 8})

	require.NoError(t, ds.AppendRows(ramp(0, 25, 8)))
	require.NoError(t, ds.Flush())
	snapshot := filepath.Join(dir, "snapshot.h5")
	b, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(snapshot, b, 0o600))

	// Appended after the snapshot: not in it.
	require.NoError(t, ds.AppendRows(ramp(25, 30, 8)))
	require.NoError(t, fw.Close())

	shape, values := readAll(t, snapshot, "/data")
	require.Equal(t, []uint64{25, 8}, shape)
	requireRamp(t, values, 25*8)

	shape, values = readAll(t, filename, "/data")
	require.Equal(t, []uint64{55, 8}, shape)
	requireRamp(t, values, 55*8)
}

// TestAppendRowsFileGrowsWithData checks that the file holds each row once, however often it is flushed.
func TestAppendRowsFileGrowsWithData(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "size.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	const channels, packet, packets = 1000, 100, 60
	ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{channels}, []uint64{10, channels})
	for p := 0; p < packets; p++ {
		require.NoError(t, ds.AppendRows(ramp(p*packet, packet, channels)))
		require.NoError(t, ds.Flush())
	}
	require.NoError(t, fw.Close())

	st, err := os.Stat(filename)
	require.NoError(t, err)
	data := int64(channels * packet * packets * 4)
	require.Less(t, st.Size(), data+data/100, "file of %d bytes for %d bytes of data", st.Size(), data)
}

func TestAppendRowsRefusals(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "refuse.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	fixed, err := fw.CreateDataset("/fixed", hdf5.Float32, []uint64{10, 4}, hdf5.WithChunkDims([]uint64{5, 4}))
	require.NoError(t, err)
	require.Error(t, fixed.AppendRows(make([]float32, 4)), "first dimension cannot grow")

	ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{4}, []uint64{5, 4})
	require.Error(t, ds.AppendRows(make([]float32, 6)), "not a whole number of rows")
	require.NoError(t, ds.AppendRows(make([]float32, 8)))
	require.Error(t, ds.Write(make([]float32, 8)), "Write after AppendRows")
	require.Error(t, ds.Resize([]uint64{4, 4}), "Resize after AppendRows")
}

// TestCInterop_AppendRows has h5dump read an appended dataset back, byte for byte.
func TestCInterop_AppendRows(t *testing.T) {
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not in PATH")
	}
	dir := t.TempDir()
	filename := filepath.Join(dir, "interop.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{13}, []uint64{4, 5})
	rows := 0
	for _, n := range []int{3, 7, 1, 12, 2} {
		require.NoError(t, ds.AppendRows(ramp(rows, n, 13)))
		rows += n
		require.NoError(t, ds.Flush())
	}
	require.NoError(t, ds.WriteAttribute("Count", int64(rows*13)))
	require.NoError(t, fw.Close())

	out := runTool(t, h5dump, "-H", filename)
	require.Contains(t, out, "DATASPACE  SIMPLE { ( 25, 13 ) / ( H5S_UNLIMITED, 13 ) }")

	bin := filepath.Join(dir, "data.bin")
	runTool(t, h5dump, "-d", "/data", "-b", "LE", "-o", bin, filename)
	raw, err := os.ReadFile(bin)
	require.NoError(t, err)
	require.Len(t, raw, rows*13*4)
	for i := 0; i < rows*13; i++ {
		v := math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		require.Equal(t, float32(i), v, "element %d", i)
	}
}

func runTool(t *testing.T, tool string, args ...string) string {
	t.Helper()
	out, err := exec.Command(tool, args...).CombinedOutput()
	require.NoError(t, err, "%s %v:\n%s", tool, args, out)
	return string(out)
}

// TestCInterop_AppendRowsAfterFlush has h5dump read a copy of the file taken after Flush, before Close.
func TestCInterop_AppendRowsAfterFlush(t *testing.T) {
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not in PATH")
	}
	dir := t.TempDir()
	filename := filepath.Join(dir, "live.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()
	ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{6}, []uint64{10, 6})
	require.NoError(t, ds.AppendRows(ramp(0, 17, 6)))
	require.NoError(t, ds.Flush())
	require.NoError(t, ds.AppendRows(ramp(17, 5, 6))) // not flushed: not in the snapshot

	snapshot := filepath.Join(dir, "snapshot.h5")
	b, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(snapshot, b, 0o600))

	bin := filepath.Join(dir, "data.bin")
	runTool(t, h5dump, "-d", "/data", "-b", "LE", "-o", bin, snapshot)
	raw, err := os.ReadFile(bin)
	require.NoError(t, err)
	require.Len(t, raw, 17*6*4)
	for i := 0; i < 17*6; i++ {
		require.Equal(t, float32(i), math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])), "element %d", i)
	}
}

// TestCreateDatasetZeroExtent checks that a dimension may start at 0 only when its maximum is Unlimited.
func TestCreateDatasetZeroExtent(t *testing.T) {
	cases := []struct {
		name    string
		dims    []uint64
		maxDims []uint64
		ok      bool
	}{
		{name: "unlimited first dimension", dims: []uint64{0, 4}, maxDims: []uint64{hdf5.Unlimited, 4}, ok: true},
		{name: "fixed maximum", dims: []uint64{0, 4}, maxDims: []uint64{100, 4}},
		{name: "no maximum", dims: []uint64{0, 4}},
		{name: "zero in a fixed dimension", dims: []uint64{0, 0}, maxDims: []uint64{hdf5.Unlimited, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fw, err := hdf5.CreateForWrite(filepath.Join(t.TempDir(), "zero.h5"), hdf5.CreateTruncate)
			require.NoError(t, err)
			defer fw.Close()
			opts := []hdf5.DatasetOption{hdf5.WithChunkDims([]uint64{5, 4})}
			if tc.maxDims != nil {
				opts = append(opts, hdf5.WithMaxDims(tc.maxDims))
			}
			_, err = fw.CreateDataset("/data", hdf5.Float32, tc.dims, opts...)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// TestAppendRowsEdges covers an appendable dataset left empty and one holding a single row.
func TestAppendRowsEdges(t *testing.T) {
	for _, rows := range []int{0, 1} {
		filename := filepath.Join(t.TempDir(), "edge.h5")
		fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
		require.NoError(t, err)
		ds := appendable(t, fw, "/data", hdf5.Float32, []uint64{4}, []uint64{10, 4})
		if rows > 0 {
			require.NoError(t, ds.AppendRows(ramp(0, rows, 4)))
		}
		require.NoError(t, ds.Flush())
		require.NoError(t, fw.Close())

		f, err := hdf5.Open(filename)
		require.NoError(t, err)
		var found *hdf5.Dataset
		f.Walk(func(p string, obj hdf5.Object) {
			if d, ok := obj.(*hdf5.Dataset); ok && p == "/data" {
				found = d
			}
		})
		require.NotNil(t, found)
		shape, err := found.Shape()
		require.NoError(t, err)
		require.Equal(t, []uint64{uint64(rows), 4}, shape, "%d rows", rows)
		if rows > 0 {
			values, err := found.Read()
			require.NoError(t, err)
			requireRamp(t, values, rows*4)
		}
		require.NoError(t, f.Close())
	}
}

// TestCInterop_AppendRowsEmpty has h5dump read an appendable dataset nothing was appended to.
func TestCInterop_AppendRowsEmpty(t *testing.T) {
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not in PATH")
	}
	filename := filepath.Join(t.TempDir(), "empty.h5")
	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	require.NoError(t, err)
	appendable(t, fw, "/data", hdf5.Float32, []uint64{4}, []uint64{10, 4})
	require.NoError(t, fw.Close())

	out := runTool(t, h5dump, filename)
	require.Contains(t, out, "DATASPACE  SIMPLE { ( 0, 4 ) / ( H5S_UNLIMITED, 4 ) }")
}
