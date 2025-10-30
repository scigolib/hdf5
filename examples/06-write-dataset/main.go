// Package main demonstrates how to create and write datasets to HDF5 files.
//
// This example shows:
//   - Creating an HDF5 file for writing
//   - Creating datasets with different data types
//   - Writing data to datasets
//   - Writing multi-dimensional arrays
//
// Usage:
//
//	go run main.go
//
// This will create "output.h5" with sample datasets.
package main

import (
	"fmt"
	"log"

	"github.com/scigolib/hdf5"
)

func main() {
	// Create a new HDF5 file for writing
	fmt.Println("Creating HDF5 file...")
	fw, err := hdf5.CreateForWrite("output.h5", hdf5.CreateTruncate)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	// Example 1: Write a 1D int32 array
	fmt.Println("Writing 1D int32 dataset...")
	ds1, err := fw.CreateDataset("/temperature", hdf5.Int32, []uint64{5})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	temperatures := []int32{20, 22, 25, 23, 21}
	if err := ds1.Write(temperatures); err != nil {
		log.Fatalf("Failed to write data: %v", err)
	}

	// Example 2: Write a 1D float64 array
	fmt.Println("Writing 1D float64 dataset...")
	ds2, err := fw.CreateDataset("/measurements", hdf5.Float64, []uint64{10})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	measurements := []float64{1.1, 2.2, 3.3, 4.4, 5.5, 6.6, 7.7, 8.8, 9.9, 10.0}
	if err := ds2.Write(measurements); err != nil {
		log.Fatalf("Failed to write data: %v", err)
	}

	// Example 3: Write a 2D matrix (row-major order)
	fmt.Println("Writing 2D float64 matrix...")
	ds3, err := fw.CreateDataset("/matrix", hdf5.Float64, []uint64{3, 4})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	// 3x4 matrix flattened in row-major order:
	// Row 0: [1.0, 2.0, 3.0, 4.0]
	// Row 1: [5.0, 6.0, 7.0, 8.0]
	// Row 2: [9.0, 10.0, 11.0, 12.0]
	matrix := []float64{
		1.0, 2.0, 3.0, 4.0,
		5.0, 6.0, 7.0, 8.0,
		9.0, 10.0, 11.0, 12.0,
	}
	if err := ds3.Write(matrix); err != nil {
		log.Fatalf("Failed to write data: %v", err)
	}

	// Example 4: Write various integer types
	fmt.Println("Writing various integer types...")

	// int8
	ds4, _ := fw.CreateDataset("/int8_data", hdf5.Int8, []uint64{3})
	ds4.Write([]int8{-128, 0, 127})

	// uint8
	ds5, _ := fw.CreateDataset("/uint8_data", hdf5.Uint8, []uint64{3})
	ds5.Write([]uint8{0, 128, 255})

	// int64
	ds6, _ := fw.CreateDataset("/int64_data", hdf5.Int64, []uint64{3})
	ds6.Write([]int64{-1000000, 0, 1000000})

	// Example 5: Write float32 data
	fmt.Println("Writing float32 dataset...")
	ds7, _ := fw.CreateDataset("/float32_data", hdf5.Float32, []uint64{5})
	ds7.Write([]float32{1.5, 2.5, 3.5, 4.5, 5.5})

	// Close file and flush all writes
	if err := fw.Close(); err != nil {
		log.Fatalf("Failed to close file: %v", err)
	}

	fmt.Println("\nSuccess! File 'output.h5' created with datasets:")
	fmt.Println("  /temperature     - 1D int32 array (5 elements)")
	fmt.Println("  /measurements    - 1D float64 array (10 elements)")
	fmt.Println("  /matrix          - 2D float64 matrix (3x4)")
	fmt.Println("  /int8_data       - 1D int8 array (3 elements)")
	fmt.Println("  /uint8_data      - 1D uint8 array (3 elements)")
	fmt.Println("  /int64_data      - 1D int64 array (3 elements)")
	fmt.Println("  /float32_data    - 1D float32 array (5 elements)")
	fmt.Println("\nLimitations (MVP v0.11.0-beta):")
	fmt.Println("  - Datasets are not yet linked to root group (Component 3)")
	fmt.Println("  - Only contiguous layout supported")
	fmt.Println("  - No compression")
	fmt.Println("  - No chunking")
}
