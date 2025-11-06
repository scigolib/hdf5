package hdf5_test

import (
	"os"
	"testing"

	"github.com/scigolib/hdf5"
)

func TestResizeExtend1D(t *testing.T) {
	// Create, write, extend, verify.
	fw, err := hdf5.CreateForWrite("test_resize_extend.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_extend.h5")
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Write initial data.
	data10 := make([]float64, 10)
	for i := range data10 {
		data10[i] = float64(i)
	}
	if err := ds.Write(data10); err != nil {
		t.Fatalf("write data: %v", err)
	}

	// Resize to 20.
	if err := ds.Resize([]uint64{20}); err != nil {
		t.Errorf("resize: %v", err)
	}

	// Note: To fully verify, we would need to reopen and read.
	// For this unit test, we just verify no error.
}

func TestResizeExceedsMaxDims(t *testing.T) {
	// Should error if newDims > maxDims.
	fw, err := hdf5.CreateForWrite("test_resize_exceed.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_exceed.h5")
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{50})) // Max 50
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Try to resize beyond maxDims.
	err = ds.Resize([]uint64{100})
	if err == nil {
		t.Error("expected error when exceeding maxDims")
	}
}

func TestResizeRequiresChunked(t *testing.T) {
	// Should error for contiguous layout (we can't create with maxdims without chunks anyway).
	// This is a defensive test in case someone bypasses validation.
	fw, err := hdf5.CreateForWrite("test_resize_contiguous.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_contiguous.h5")
	defer fw.Close()

	// Create contiguous dataset (no maxdims).
	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Try to resize.
	err = ds.Resize([]uint64{20})
	if err == nil {
		t.Error("expected error for resize on contiguous dataset")
	}
}

func TestResizeRequiresMaxDims(t *testing.T) {
	// Should error if dataset doesn't have maxDims set.
	fw, err := hdf5.CreateForWrite("test_resize_no_maxdims.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_no_maxdims.h5")
	defer fw.Close()

	// Create chunked dataset without maxDims.
	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Try to resize.
	err = ds.Resize([]uint64{20})
	if err == nil {
		t.Error("expected error for resize without maxDims")
	}
}

func TestResizeDimensionCountMismatch(t *testing.T) {
	// Should error if newDims has wrong number of dimensions.
	fw, err := hdf5.CreateForWrite("test_resize_dim_mismatch.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_dim_mismatch.h5")
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Try to resize with wrong number of dims.
	err = ds.Resize([]uint64{20, 30}) // Wrong: 2 dims for 1D dataset
	if err == nil {
		t.Error("expected error for dimension count mismatch")
	}
}

func TestResize2D(t *testing.T) {
	// 2D dataset resize.
	fw, err := hdf5.CreateForWrite("test_resize_2d.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_2d.h5")
	defer fw.Close()

	// Rows unlimited, columns fixed.
	ds, err := fw.CreateDataset("/matrix", hdf5.Float64, []uint64{10, 20},
		hdf5.WithChunkDims([]uint64{5, 10}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited, 20}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Write initial data.
	data := make([]float64, 10*20)
	if err := ds.Write(data); err != nil {
		t.Fatalf("write data: %v", err)
	}

	// Resize rows (extend).
	if err := ds.Resize([]uint64{15, 20}); err != nil {
		t.Errorf("resize 2D: %v", err)
	}
}

func TestResizeShrink(t *testing.T) {
	// Shrink dataset (reduce dimensions).
	fw, err := hdf5.CreateForWrite("test_resize_shrink.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_resize_shrink.h5")
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", hdf5.Int32, []uint64{20},
		hdf5.WithChunkDims([]uint64{10}),
		hdf5.WithMaxDims([]uint64{100}))
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	// Write initial data.
	data := make([]int32, 20)
	if err := ds.Write(data); err != nil {
		t.Fatalf("write data: %v", err)
	}

	// Shrink to 10.
	if err := ds.Resize([]uint64{10}); err != nil {
		t.Errorf("resize shrink: %v", err)
	}
}
