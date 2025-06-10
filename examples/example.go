package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/scigolib/hdf5"
)

func main() {
	testFile := "testdata/simple.h5"

	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		createTestFile()
	}

	file, err := hdf5.Open(testFile)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Используем новый метод для получения версии
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

func createTestFile() {
	if !checkPythonDependencies() {
		log.Fatalf("Required Python dependencies missing. Install with: pip install h5py numpy")
	}

	if err := os.MkdirAll("testdata", 0755); err != nil {
		log.Fatalf("Failed to create testdata directory: %v", err)
	}

	pyScript := `
import h5py
import numpy as np

# Используем совместимый формат HDF5 1.8
with h5py.File('testdata/simple.h5', 'w', libver='v108') as f:
    f.attrs['description'] = 'Simple test file'
    
    # Create datasets
    f.create_dataset('ints', data=np.array([1, 2, 3]), dtype='int32')
    f.create_dataset('floats', data=np.array([1.1, 2.2, 3.3]), dtype='float32')
    
    # Create groups
    grp1 = f.create_group('group1')
    grp1.create_dataset('data', data=np.arange(10), dtype='int32')
    
    grp2 = f.create_group('group2')
    grp2.create_dataset('matrix', data=np.eye(3), dtype='float32')
`

	pyFile := "testdata/create_test_file.py"
	if err := os.WriteFile(pyFile, []byte(pyScript), 0644); err != nil {
		log.Fatalf("Failed to write Python script: %v", err)
	}

	cmd := exec.Command(getPythonCommand(), pyFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to create test file: %v", err)
	}
	fmt.Println("Test file created: testdata/simple.h5")
}

func checkPythonDependencies() bool {
	cmd := exec.Command(getPythonCommand(), "-c", "import h5py, numpy")
	return cmd.Run() == nil
}

func getPythonCommand() string {
	return "python" // Для Windows всегда используем "python"
}
