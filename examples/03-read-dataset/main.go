package main

import (
	"fmt"
	"log"

	"github.com/scigolib/hdf5"
)

func main() {
	// Test reading the simple float64 dataset
	fmt.Println("=== Reading simple_float64.h5 ===")
	testSimpleFloat64()

	fmt.Println("\n=== Reading matrix_2x3.h5 ===")
	testMatrix()

	fmt.Println("\n=== Reading multiple_datasets.h5 ===")
	testMultipleDatasets()
}

func testSimpleFloat64() {
	file, err := hdf5.Open("../testdata/simple_float64.h5")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// Get the dataset
	var dataset *hdf5.Dataset
	file.Walk(func(path string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "data" {
			dataset = ds
		}
	})

	if dataset == nil {
		log.Fatal("Dataset 'data' not found")
	}

	// Get info
	info, err := dataset.Info()
	if err != nil {
		log.Fatalf("Failed to get dataset info: %v", err)
	}
	fmt.Printf("Dataset info: %s\n", info)

	// Read values
	values, err := dataset.Read()
	if err != nil {
		log.Fatalf("Failed to read dataset: %v", err)
	}

	fmt.Printf("Values: %v\n", values)
	fmt.Printf("Expected: [1.0, 2.0, 3.0, 4.0, 5.0]\n")

	// Verify
	expected := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	if len(values) != len(expected) {
		log.Fatalf("Wrong number of values: got %d, expected %d", len(values), len(expected))
	}

	for i, v := range values {
		if v != expected[i] {
			log.Fatalf("Value mismatch at index %d: got %f, expected %f", i, v, expected[i])
		}
	}

	fmt.Println("✓ All values match!")
}

func testMatrix() {
	file, err := hdf5.Open("../testdata/matrix_2x3.h5")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// Get the dataset
	var dataset *hdf5.Dataset
	file.Walk(func(path string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "matrix" {
			dataset = ds
		}
	})

	if dataset == nil {
		log.Fatal("Dataset 'matrix' not found")
	}

	// Read values
	values, err := dataset.Read()
	if err != nil {
		log.Fatalf("Failed to read dataset: %v", err)
	}

	fmt.Printf("Matrix (2x3): %v\n", values)
	fmt.Printf("Expected: [1.0, 2.0, 3.0, 4.0, 5.0, 6.0]\n")

	// Verify
	expected := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}
	if len(values) != 6 {
		log.Fatalf("Wrong number of values: got %d, expected 6", len(values))
	}

	for i, v := range values {
		if v != expected[i] {
			log.Fatalf("Value mismatch at index %d: got %f, expected %f", i, v, expected[i])
		}
	}

	fmt.Println("✓ Matrix read successfully!")
}

func testMultipleDatasets() {
	file, err := hdf5.Open("../testdata/multiple_datasets.h5")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// Read all datasets
	datasets := make(map[string]*hdf5.Dataset)
	file.Walk(func(path string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok {
			datasets[ds.Name()] = ds
		}
	})

	fmt.Printf("Found %d datasets\n", len(datasets))

	// Read vector_x
	if ds, ok := datasets["vector_x"]; ok {
		values, err := ds.Read()
		if err != nil {
			log.Fatalf("Failed to read vector_x: %v", err)
		}
		fmt.Printf("vector_x: %v\n", values)
	}

	// Read vector_y
	if ds, ok := datasets["vector_y"]; ok {
		values, err := ds.Read()
		if err != nil {
			log.Fatalf("Failed to read vector_y: %v", err)
		}
		fmt.Printf("vector_y: %v\n", values)
	}

	// Read scalar_c
	if ds, ok := datasets["scalar_c"]; ok {
		values, err := ds.Read()
		if err != nil {
			log.Fatalf("Failed to read scalar_c: %v", err)
		}
		fmt.Printf("scalar_c: %v\n", values)
	}

	fmt.Println("✓ All datasets read successfully!")
}
