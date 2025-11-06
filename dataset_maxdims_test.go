package hdf5_test

import (
	"os"
	"testing"

	"github.com/scigolib/hdf5"
)

func TestCreateDatasetWithUnlimitedDimension(t *testing.T) {
	// Test single unlimited dimension.
	fw, err := hdf5.CreateForWrite("test_unlimited.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_unlimited.h5")
	defer fw.Close()

	_, err = fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))

	if err != nil {
		t.Errorf("create dataset with unlimited: %v", err)
	}
}

func TestMaxDimsRequiresChunkedLayout(t *testing.T) {
	// Should error if maxdims without chunks.
	fw, err := hdf5.CreateForWrite("test_maxdims_error.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_maxdims_error.h5")
	defer fw.Close()

	_, err = fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited}))

	if err == nil {
		t.Error("expected error for maxdims without chunked layout")
	}
}

func TestMaxDimsLengthMustMatchDims(t *testing.T) {
	// Dimension count mismatch.
	fw, err := hdf5.CreateForWrite("test_maxdims_mismatch.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_maxdims_mismatch.h5")
	defer fw.Close()

	_, err = fw.CreateDataset("/data", hdf5.Float64, []uint64{10, 20},
		hdf5.WithChunkDims([]uint64{5, 10}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited})) // Wrong: 1 maxdim for 2 dims

	if err == nil {
		t.Error("expected error for maxdims length mismatch")
	}
}

func TestMaxDimsMustBeGreaterThanOrEqualDims(t *testing.T) {
	// MaxDim < dim should error.
	fw, err := hdf5.CreateForWrite("test_maxdims_less_than_dims.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_maxdims_less_than_dims.h5")
	defer fw.Close()

	_, err = fw.CreateDataset("/data", hdf5.Float64, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{5})) // Wrong: maxDim < dim

	if err == nil {
		t.Error("expected error for maxDim < dim")
	}
}

func TestMaxDims2D(t *testing.T) {
	// 2D dataset with mixed unlimited/fixed dimensions.
	fw, err := hdf5.CreateForWrite("test_maxdims_2d.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_maxdims_2d.h5")
	defer fw.Close()

	// Rows unlimited, columns fixed.
	_, err = fw.CreateDataset("/matrix", hdf5.Float64, []uint64{10, 20},
		hdf5.WithChunkDims([]uint64{5, 10}),
		hdf5.WithMaxDims([]uint64{hdf5.Unlimited, 20}))

	if err != nil {
		t.Errorf("create 2D dataset with mixed maxdims: %v", err)
	}
}

func TestMaxDimsAllFixed(t *testing.T) {
	// MaxDims all fixed (not unlimited).
	fw, err := hdf5.CreateForWrite("test_maxdims_fixed.h5", hdf5.CreateTruncate)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer os.Remove("test_maxdims_fixed.h5")
	defer fw.Close()

	_, err = fw.CreateDataset("/data", hdf5.Int32, []uint64{10},
		hdf5.WithChunkDims([]uint64{5}),
		hdf5.WithMaxDims([]uint64{100})) // Fixed max

	if err != nil {
		t.Errorf("create dataset with fixed maxdims: %v", err)
	}
}
