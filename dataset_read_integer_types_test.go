// Round-trip tests for fixed-point datatypes. Writer + reader pair so a
// regression on either side surfaces immediately. Prior to this file the
// suite only verified that fixed-point datasets could be written; reading
// them back via Dataset.Read() silently returned
// "unsupported datatype for conversion to float64" for every width except 4
// and 8 bytes, with no signed/unsigned distinction.
//
// Real-world breakage that motivated these tests:
//   - ODIM HymecNG hydrometeor-class composites store classification as
//     uint8 — Dataset.Read() refused them, forcing downstream callers to
//     duplicate the entire chunked-deflate reader path.
//   - EUMETSAT H SAF H40B precipitation grids store mm/h × 100 as int16
//     chunked datasets — same workaround applied.
//   - ČHMÚ COTREC tiles ship uint8 reflectivity — same.

package hdf5

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

type fixedTypeCase struct {
	name  string
	dtype Datatype
	write any
	want  []float64
}

// lenOfSlice extracts the element count from any of the typed slices the
// fixed-point round-trip covers. Pulled out so the main test body stays
// under the linter's cognitive-complexity ceiling.
func lenOfSlice(v any) uint64 {
	switch s := v.(type) {
	case []int8:
		return uint64(len(s))
	case []int16:
		return uint64(len(s))
	case []int32:
		return uint64(len(s))
	case []int64:
		return uint64(len(s))
	case []uint8:
		return uint64(len(s))
	case []uint16:
		return uint64(len(s))
	case []uint32:
		return uint64(len(s))
	case []uint64:
		return uint64(len(s))
	}
	return 0
}

func writeFixedTypeFixture(t *testing.T, filename string, cases []fixedTypeCase) {
	t.Helper()
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite: %v", err)
	}
	for _, c := range cases {
		ds, err := fw.CreateDataset("/"+c.name, c.dtype, []uint64{lenOfSlice(c.write)})
		if err != nil {
			t.Fatalf("%s: CreateDataset: %v", c.name, err)
		}
		if err := ds.Write(c.write); err != nil {
			t.Fatalf("%s: Write: %v", c.name, err)
		}
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}
}

func readAndCheckCase(t *testing.T, f *File, c fixedTypeCase) {
	t.Helper()
	var obj Object
	f.Walk(func(path string, o Object) {
		if path == "/"+c.name {
			obj = o
		}
	})
	if obj == nil {
		t.Fatalf("dataset %q not found", c.name)
	}
	ds, isDS := obj.(*Dataset)
	if !isDS {
		t.Fatalf("/%s is not a dataset", c.name)
	}
	got, err := ds.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(c.want) {
		t.Fatalf("len = %d, want %d", len(got), len(c.want))
	}
	for i, w := range c.want {
		if got[i] != w {
			t.Errorf("[%d] = %v, want %v", i, got[i], w)
		}
	}
}

func TestDatasetRead_AllFixedTypes_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "fixed_types_roundtrip.h5")

	cases := []fixedTypeCase{
		{"int8", Int8, []int8{-128, -1, 0, 1, 127}, []float64{-128, -1, 0, 1, 127}},
		{"int16", Int16, []int16{-32768, -1, 0, 1, 32767}, []float64{-32768, -1, 0, 1, 32767}},
		{"int32", Int32, []int32{-1 << 30, -1, 0, 1, (1 << 30) - 1}, []float64{-1 << 30, -1, 0, 1, (1 << 30) - 1}},
		{"int64", Int64, []int64{-1 << 60, -1, 0, 1, (1 << 60) - 1}, []float64{-1 << 60, -1, 0, 1, (1 << 60) - 1}},
		{"uint8", Uint8, []uint8{0, 1, 127, 200, 255}, []float64{0, 1, 127, 200, 255}},
		{"uint16", Uint16, []uint16{0, 1, 32768, 65535}, []float64{0, 1, 32768, 65535}},
		{"uint32", Uint32, []uint32{0, 1, 1 << 31, math.MaxUint32}, []float64{0, 1, 1 << 31, math.MaxUint32}},
		// uint64 above 2^53 cannot be losslessly stored in float64; keep
		// values within the precision window.
		{"uint64", Uint64, []uint64{0, 1, 1 << 40, (1 << 53) - 1}, []float64{0, 1, 1 << 40, (1 << 53) - 1}},
	}

	writeFixedTypeFixture(t, filename, cases)

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			readAndCheckCase(t, f, c)
		})
	}

	// Confirm the file is non-empty (sanity).
	info, err := os.Stat(filename)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output file empty or missing")
	}
}

// TestDatasetRead_ChunkedUint8 covers the chunked-layout path with a 1-byte
// type — mirrors the OPERA HymecNG / ČHMÚ COTREC payloads (uint8 + chunked
// DEFLATE). Prior to the dataset_reader extension this combination forced
// every downstream caller to implement its own chunk-walking decoder.
func TestDatasetRead_ChunkedUint8(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_uint8.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite: %v", err)
	}

	// Small 16×16 grid, chunked 4×4. Realistic encoded values include
	// 0 (dry), 1 (drizzle), 2 (rain), 255 (nodata) — the HymecNG palette.
	const w, h = 16, 16
	data := make([]uint8, w*h)
	for i := range data {
		switch i % 4 {
		case 0:
			data[i] = 0
		case 1:
			data[i] = 1
		case 2:
			data[i] = 2
		case 3:
			data[i] = 255
		}
	}
	ds, err := fw.CreateDataset("/classes", Uint8, []uint64{h, w},
		WithChunkDims([]uint64{4, 4}),
	)
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if err := ds.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	var obj Object
	f.Walk(func(path string, o Object) {
		if path == "/classes" {
			obj = o
		}
	})
	if obj == nil {
		t.Fatal("dataset /classes not found")
	}
	got, err := obj.(*Dataset).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != int(w*h) {
		t.Fatalf("len = %d, want %d", len(got), w*h)
	}
	for i, v := range got {
		if v != float64(data[i]) {
			t.Errorf("[%d] = %v, want %v", i, v, data[i])
		}
	}
}
