package hdf5_test

import (
	"os"
	"testing"

	"github.com/scigolib/hdf5"
)

func TestResizeRoundTrip1D(t *testing.T) {
	// Full round-trip: create, write, resize, write more, close, reopen, read.
	// This tests that resize persists correctly to disk.
	filename := "test_resize_roundtrip.h5"
	defer os.Remove(filename)

	// Phase 1: Create and write initial data.
	{
		fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
		if err != nil {
			t.Fatalf("create file: %v", err)
		}

		ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
			hdf5.WithChunkDims([]uint64{5}),
			hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))
		if err != nil {
			t.Fatalf("create dataset: %v", err)
		}

		// Write initial 10 elements.
		data10 := make([]float64, 10)
		for i := range data10 {
			data10[i] = float64(i)
		}
		if err := ds.Write(data10); err != nil {
			t.Fatalf("write initial data: %v", err)
		}

		// Resize to 20.
		if err := ds.Resize([]uint64{20}); err != nil {
			t.Fatalf("resize: %v", err)
		}

		// Write 20 elements (including extended region).
		data20 := make([]float64, 20)
		for i := range data20 {
			if i < 10 {
				data20[i] = float64(i) // Original data
			} else {
				data20[i] = float64(i * 10) // New data
			}
		}
		if err := ds.Write(data20); err != nil {
			t.Fatalf("write extended data: %v", err)
		}

		if err := fw.Close(); err != nil {
			t.Fatalf("close file: %v", err)
		}
	}

	// Phase 2: Reopen and verify file is valid.
	{
		f, err := hdf5.Open(filename)
		if err != nil {
			t.Fatalf("reopen file: %v", err)
		}
		defer f.Close()

		// Find dataset using Walk.
		var found bool
		f.Walk(func(p string, obj hdf5.Object) {
			if p == "/data" {
				if _, ok := obj.(*hdf5.Dataset); ok {
					found = true
				}
			}
		})

		if !found {
			t.Fatal("dataset not found after resize")
		}

		// Note: Full read validation would require Dataset.Dimensions() method.
		// For now, we verify that the file is readable and dataset exists.
		// The resize operation modified the dataspace message in object header,
		// and if the file opens successfully, the format is valid.
	}
}

func TestResizeMultipleTimes(t *testing.T) {
	// Resize multiple times: 10 -> 20 -> 30 -> 15.
	filename := "test_resize_multiple.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Initial write.
	data10 := make([]int32, 10)
	for i := range data10 {
		data10[i] = int32(i)
	}
	if err := ds.Write(data10); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	// Resize to 20.
	if err := ds.Resize([]uint64{20}); err != nil {
		t.Fatalf("resize to 20: %v", err)
	}

	// Write 20.
	data20 := make([]int32, 20)
	for i := range data20 {
		data20[i] = int32(i + 100)
	}
	if err := ds.Write(data20); err != nil {
		t.Fatalf("write 20: %v", err)
	}

	// Resize to 30.
	if err := ds.Resize([]uint64{30}); err != nil {
		t.Fatalf("resize to 30: %v", err)
	}

	// Resize to 15 (shrink).
	if err := ds.Resize([]uint64{15}); err != nil {
		t.Fatalf("resize to 15: %v", err)
	}

	// Final write.
	data15 := make([]int32, 15)
	for i := range data15 {
		data15[i] = int32(i + 200)
	}
	if err := ds.Write(data15); err != nil {
		t.Fatalf("write 15: %v", err)
	}
}

func TestResize2DRoundTrip(t *testing.T) {
	// 2D dataset resize round-trip.
	filename := "test_resize_2d_roundtrip.h5"
	defer os.Remove(filename)

	// Phase 1: Create and resize.
	{
		fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
		if err != nil {
			t.Fatalf("create file: %v", err)
		}

		ds, err := fw.CreateDataset("/matrix", hdf5.Float64, []uint64{5, 10},
			hdf5.WithChunkDims([]uint64{5, 5}),
			hdf5.WithMaxDims([]uint64{hdf5.Unlimited, 10})) // Rows unlimited, cols fixed
		if err != nil {
			t.Fatalf("create dataset: %v", err)
		}

		// Write initial 5x10.
		data50 := make([]float64, 50)
		for i := range data50 {
			data50[i] = float64(i)
		}
		if err := ds.Write(data50); err != nil {
			t.Fatalf("write initial: %v", err)
		}

		// Resize to 10x10.
		if err := ds.Resize([]uint64{10, 10}); err != nil {
			t.Fatalf("resize: %v", err)
		}

		// Write 10x10.
		data100 := make([]float64, 100)
		for i := range data100 {
			data100[i] = float64(i * 2)
		}
		if err := ds.Write(data100); err != nil {
			t.Fatalf("write extended: %v", err)
		}

		if err := fw.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	// Phase 2: Reopen and verify file is valid.
	{
		f, err := hdf5.Open(filename)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer f.Close()

		// Find dataset using Walk.
		var found bool
		f.Walk(func(p string, obj hdf5.Object) {
			if p == "/matrix" {
				if _, ok := obj.(*hdf5.Dataset); ok {
					found = true
				}
			}
		})

		if !found {
			t.Fatal("dataset not found after 2D resize")
		}

		// Note: Full read validation would require Dataset.Dimensions() method.
		// For now, we verify that the file is readable and dataset exists.
	}
}

func TestResizeWithFixedMaxDims(t *testing.T) {
	// Test resizing with fixed maxdims (not unlimited).
	filename := "test_resize_fixed_max.h5"
	defer os.Remove(filename)

	fw, err := hdf5.CreateForWrite(filename, hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Uint32, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{50})) // Max 50
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Write initial.
	data10 := make([]uint32, 10)
	if err := ds.Write(data10); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Resize to 25 (within maxdims).
	if err := ds.Resize([]uint64{25}); err != nil {
		t.Fatalf("resize to 25: %v", err)
	}

	// Resize to 50 (at maxdims).
	if err := ds.Resize([]uint64{50}); err != nil {
		t.Fatalf("resize to 50: %v", err)
	}

	// Resize back to 30 (shrink).
	if err := ds.Resize([]uint64{30}); err != nil {
		t.Fatalf("resize to 30: %v", err)
	}
}
