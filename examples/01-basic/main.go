package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/scigolib/hdf5"
)

func main() {
	// Create test files of different versions
	if err := createTestFiles(); err != nil {
		log.Fatalf("Failed to create test files: %v", err)
	}

	// Test all files
	files := []string{
		"testdata/v0.h5",
		"testdata/v2.h5",
		"testdata/v3.h5",
		"testdata/with_groups.h5",
		"testdata/test_v1.12_simple.h5",
	}

	for _, fname := range files {
		fmt.Printf("\n===== Testing file: %s =====\n", fname)
		testFile(fname)
	}
}

func testFile(filename string) {
	file, err := hdf5.Open(filename)
	if err != nil {
		log.Printf("Failed to open file: %v", err)
		return
	}
	defer file.Close()

	fmt.Printf("File opened successfully. Superblock version: %d\n", file.SuperblockVersion())

	fmt.Println("File structure:")
	file.Walk(func(path string, obj hdf5.Object) {
		switch v := obj.(type) {
		case *hdf5.Group:
			fmt.Printf("[Group] %s (%d children)\n", path, len(v.Children()))
		case *hdf5.Dataset:
			fmt.Printf("[Dataset] %s\n", path)
		default:
			fmt.Printf("[Unknown] %s\n", path)
		}
	})
}

func createTestFiles() error {
	if !checkPythonDependencies() {
		log.Printf("Required Python dependencies missing. Install with: pip install h5py numpy")
		return nil
	}

	if err := os.MkdirAll("testdata", 0755); err != nil {
		return fmt.Errorf("failed to create testdata directory: %v", err)
	}

	// Create script for generating test files
	pyScript := `
import h5py
import numpy as np

print("Creating test HDF5 files...")

# Version 0 file (HDF5 1.0)
filename = 'testdata/v0.h5'
with h5py.File(filename, 'w', libver='earliest') as f:
    f.create_dataset('test', data=[1, 2, 3])
print(f'Created: {filename}')

# Version 2 file (HDF5 1.8)
filename = 'testdata/v2.h5'
with h5py.File(filename, 'w', libver='v108') as f:
    f.create_dataset('data', data=np.arange(10))
print(f'Created: {filename}')

# Version 3 file (HDF5 1.10+)
filename = 'testdata/v3.h5'
with h5py.File(filename, 'w', libver='latest') as f:
    f.create_dataset('data', data=np.arange(10))
print(f'Created: {filename}')

# File with groups
filename = 'testdata/with_groups.h5'
with h5py.File(filename, 'w', libver='v108') as f:
    f.create_dataset('dataset1', data=[1.1, 2.2, 3.3])
    grp = f.create_group('subgroup')
    grp.create_dataset('dataset2', data=[4, 5, 6])
    grp.create_group('nested_group').create_dataset('nested_data', data=[7, 8, 9])
print(f'Created: {filename}')
`

	pyFile := "testdata/create_test_files.py"
	if err := os.WriteFile(pyFile, []byte(pyScript), 0644); err != nil {
		return fmt.Errorf("failed to write Python script: %v", err)
	}

	cmd := exec.Command(getPythonCommand(), pyFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create test files: %v", err)
	}

	return nil
}

func checkPythonDependencies() bool {
	cmd := exec.Command(getPythonCommand(), "-c", "import h5py, numpy")
	return cmd.Run() == nil
}

func getPythonCommand() string {
	// Try to use python3 if available
	if _, err := exec.LookPath("python3"); err == nil {
		return "python3"
	}
	return "python"
}
