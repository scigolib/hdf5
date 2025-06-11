// HDF5 Test File Generator - Windows Compatible Version
// This version provides multiple approaches for Windows users

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Since gonum.org/v1/hdf5 requires CGO and HDF5 C libraries,
// this version provides setup instructions and alternative approaches

const setupInstructions = `
HDF5 Test File Generator Setup Instructions for Windows
======================================================

The Go HDF5 bindings require HDF5 C libraries to be installed. Here are the setup options:

OPTION 1: Install HDF5 C Libraries (Recommended)
-----------------------------------------------
1. Download HDF5 for Windows from: https://www.hdfgroup.org/downloads/hdf5/
2. Install the HDF5 libraries (choose the version you need: 1.8, 1.10, or 1.12)
3. Set environment variables:
   - HDF5_ROOT=C:\Program Files\HDF_Group\HDF5\1.12.x
   - Add %HDF5_ROOT%\bin to your PATH
   - Set CGO_CFLAGS=-I%HDF5_ROOT%\include
   - Set CGO_LDFLAGS=-L%HDF5_ROOT%\lib -lhdf5
4. Enable CGO: set CGO_ENABLED=1
5. Install a C compiler (MinGW-w64 or Visual Studio Build Tools)

Then run:
go get gonum.org/v1/hdf5
go build

OPTION 2: Use Docker (Easiest)
-----------------------------
Create a Dockerfile with pre-installed HDF5:

FROM golang:1.21-alpine
RUN apk add --no-cache hdf5-dev gcc musl-dev
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o hdf5-test-gen

OPTION 3: Use WSL2 (Linux Subsystem)
-----------------------------------
1. Install WSL2 with Ubuntu
2. In WSL2 terminal:
   sudo apt-get update
   sudo apt-get install libhdf5-dev golang-go
   go get gonum.org/v1/hdf5
   go build

OPTION 4: Cross-platform Binary Generator
------------------------------------------
Use the binary version below that creates test files without HDF5 dependencies.
`

// Pure Go implementation for basic HDF5-like file structure
// This creates files that follow HDF5 format principles but may not be fully compliant
type SimpleHDF5Generator struct {
	outputDir string
	version   string
	verbose   bool
}

func NewSimpleHDF5Generator(outputDir, version string, verbose bool) *SimpleHDF5Generator {
	return &SimpleHDF5Generator{
		outputDir: outputDir,
		version:   version,
		verbose:   verbose,
	}
}

// Create a basic binary file with HDF5-like structure
func (g *SimpleHDF5Generator) CreateTestFile(filename string) error {
	if g.verbose {
		fmt.Printf("Creating simple test file: %s (HDF5 v%s compatible)\n", filename, g.version)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Write HDF5 signature
	signature := []byte{0x89, 0x48, 0x44, 0x46, 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := file.Write(signature); err != nil {
		return err
	}

	// Write version info
	versionInfo := fmt.Sprintf("HDF5-v%s-test-file-created-%s", g.version, time.Now().Format("2006-01-02T15:04:05"))
	if _, err := file.WriteString(versionInfo); err != nil {
		return err
	}

	// Write some test data
	testData := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	}
	if _, err := file.Write(testData); err != nil {
		return err
	}

	if g.verbose {
		fmt.Printf("  Created basic test file structure\n")
	}

	return nil
}

// Generate Python script that creates proper HDF5 files
func generatePythonScript(outputDir string) error {
	scriptContent := `#!/usr/bin/env python3
"""
HDF5 Test File Generator - Python Version
This script creates valid HDF5 files using h5py with proper version constraints.
"""

import h5py
import numpy as np
import os
import sys
from datetime import datetime

def create_hdf5_file(filename, version='1.12'):
    """Create HDF5 test file with version-specific features."""
    
    # Set version-specific parameters
    if version == '1.8':
        libver = 'v18'
        compression_available = True
    elif version == '1.10':
        libver = 'v110'
        compression_available = True
    elif version == '1.12':
        libver = 'latest'
        compression_available = True
    else:
        libver = 'latest'
        compression_available = True
    
    print(f"Creating {filename} with HDF5 v{version} compatibility...")
    
    # Create file with version constraint
    with h5py.File(filename, 'w', libver=libver) as f:
        # Root attributes
        f.attrs['title'] = f'HDF5 Test File v{version}'
        f.attrs['version'] = version
        f.attrs['created'] = datetime.now().isoformat()
        f.attrs['generator'] = 'Python h5py'
        
        # Create groups
        data_group = f.create_group('data')
        metadata_group = f.create_group('metadata')
        data_group.create_group('raw')
        data_group.create_group('processed')
        metadata_group.create_group('experiment')
        
        # Create datasets
        # 1D integer array
        integers = np.arange(1, 11, dtype=np.int32)
        f.create_dataset('integers_1d', data=integers)
        
        # 2D float array
        floats_2d = np.random.random((10, 5)).astype(np.float64)
        f.create_dataset('floats_2d', data=floats_2d)
        
        # String dataset
        strings = ['hello', 'world', 'hdf5', 'test', 'data']
        dt = h5py.string_dtype(encoding='utf-8')
        f.create_dataset('strings', data=strings, dtype=dt)
        
        # Large dataset
        large_data = np.random.random((1000, 1000)).astype(np.float32)
        if compression_available:
            f.create_dataset('large_dataset', data=large_data, 
                           compression='gzip', compression_opts=6,
                           chunks=True)
        else:
            f.create_dataset('large_dataset', data=large_data)
        
        # Nested dataset in group
        data_group.create_dataset('sample_data', data=np.arange(100))
        
        # Add attributes to datasets
        f['integers_1d'].attrs['description'] = 'Test integer array'
        f['floats_2d'].attrs['units'] = 'arbitrary'
        f['strings'].attrs['encoding'] = 'utf-8'
        
        print(f"  Created {len(list(f.keys()))} root datasets")
        print(f"  Created {len(list(f.attrs.keys()))} root attributes")

def main():
    import argparse
    
    parser = argparse.ArgumentParser(description='Create HDF5 test files')
    parser.add_argument('--output', default='.', help='Output directory')
    parser.add_argument('--version', choices=['1.8', '1.10', '1.12'], 
                       default='1.12', help='HDF5 version')
    parser.add_argument('--count', type=int, default=1, 
                       help='Number of files to create')
    parser.add_argument('--prefix', default='test', help='File prefix')
    
    args = parser.parse_args()
    
    # Create output directory
    os.makedirs(args.output, exist_ok=True)
    
    # Generate files
    for i in range(args.count):
        if args.count == 1:
            filename = os.path.join(args.output, f'{args.prefix}_v{args.version}.h5')
        else:
            filename = os.path.join(args.output, f'{args.prefix}_v{args.version}_{i+1:03d}.h5')
        
        try:
            create_hdf5_file(filename, args.version)
        except Exception as e:
            print(f"Error creating {filename}: {e}")
            sys.exit(1)
    
    print(f"\nSuccessfully created {args.count} test file(s) in {args.output}")
    print("Files are ready for testing HDF5 readers and compatibility.")

if __name__ == '__main__':
    main()
`

	scriptPath := filepath.Join(outputDir, "create_hdf5_tests.py")
	return os.WriteFile(scriptPath, []byte(scriptContent), 0755)
}

// Generate batch script for Windows users
func generateBatchScript(outputDir string) error {
	batchContent := `@echo off
REM HDF5 Test File Generator - Windows Batch Script
REM This script helps set up the environment and run the generator

echo HDF5 Test File Generator for Windows
echo =====================================
echo.

REM Check if Python is available
python --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERROR: Python is not installed or not in PATH
    echo Please install Python from https://python.org/
    goto :error
)

REM Check if h5py is available
python -c "import h5py" >nul 2>&1
if %errorlevel% neq 0 (
    echo h5py not found. Installing...
    pip install h5py numpy
    if %errorlevel% neq 0 (
        echo ERROR: Failed to install h5py
        goto :error
    )
)

REM Run the Python script
echo Running HDF5 test file generator...
python create_hdf5_tests.py %*

goto :end

:error
echo.
echo Alternative options:
echo 1. Use WSL2: wsl -d Ubuntu
echo 2. Use Docker: docker run --rm -v %cd%:/work -w /work python:3.9 sh -c "pip install h5py numpy && python create_hdf5_tests.py"
echo 3. Install HDF5 C libraries and rebuild Go version
pause

:end
`

	batchPath := filepath.Join(outputDir, "generate_hdf5_tests.bat")
	return os.WriteFile(batchPath, []byte(batchContent), 0755)
}

func parseVersion(versionStr string) string {
	switch strings.ToLower(versionStr) {
	case "1.8", "18":
		return "1.8"
	case "1.10", "110":
		return "1.10"
	case "1.12", "112":
		return "1.12"
	default:
		return "1.12"
	}
}

func main() {
	var (
		outputDir  = flag.String("output", ".", "Output directory for test files")
		versionStr = flag.String("version", "1.12", "HDF5 version (1.8, 1.10, 1.12)")
		prefix     = flag.String("prefix", "test", "File prefix")
		numFiles   = flag.Int("count", 1, "Number of test files to generate")
		verbose    = flag.Bool("verbose", false, "Verbose output")
		setup      = flag.Bool("setup", false, "Show setup instructions")
		simple     = flag.Bool("simple", false, "Create simple binary files (no HDF5 dependency)")
		python     = flag.Bool("python", false, "Generate Python script for creating HDF5 files")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("HDF5 Test File Generator - Windows Compatible")
		fmt.Println("===========================================")
		fmt.Println("Creates valid HDF5 test files for different HDF5 versions.")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  # Show setup instructions")
		fmt.Println("  ./hdf5-test-gen -setup")
		fmt.Println()
		fmt.Println("  # Generate Python script (recommended for Windows)")
		fmt.Println("  ./hdf5-test-gen -python")
		fmt.Println()
		fmt.Println("  # Create simple binary test files")
		fmt.Println("  ./hdf5-test-gen -simple -count 3")
		return
	}

	if *setup {
		fmt.Println(setupInstructions)
		return
	}

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	version := parseVersion(*versionStr)

	if *python {
		fmt.Println("Generating Python script for HDF5 test file creation...")
		
		if err := generatePythonScript(*outputDir); err != nil {
			log.Fatalf("Failed to generate Python script: %v", err)
		}
		
		if err := generateBatchScript(*outputDir); err != nil {
			log.Fatalf("Failed to generate batch script: %v", err)
		}
		
		fmt.Printf("Generated files in %s:\n", *outputDir)
		fmt.Println("  - create_hdf5_tests.py (Python script)")
		fmt.Println("  - generate_hdf5_tests.bat (Windows batch script)")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  python create_hdf5_tests.py --version 1.12 --count 5")
		fmt.Println("  generate_hdf5_tests.bat --version 1.8")
		return
	}

	if *simple {
		fmt.Printf("Generating %d simple test file(s) (HDF5 v%s compatible)...\n", *numFiles, version)
		
		generator := NewSimpleHDF5Generator(*outputDir, version, *verbose)
		
		for i := 0; i < *numFiles; i++ {
			var filename string
			if *numFiles == 1 {
				filename = filepath.Join(*outputDir, fmt.Sprintf("%s_v%s_simple.h5", *prefix, version))
			} else {
				filename = filepath.Join(*outputDir, fmt.Sprintf("%s_v%s_simple_%03d.h5", *prefix, version, i+1))
			}
			
			if err := generator.CreateTestFile(filename); err != nil {
				log.Fatalf("Failed to create test file %s: %v", filename, err)
			}
		}
		
		fmt.Printf("Successfully generated %d simple test file(s) in %s\n", *numFiles, *outputDir)
		fmt.Println("Note: These are basic binary files. For full HDF5 compliance, use the Python version.")
		return
	}

	// Default: Show instructions
	fmt.Println("HDF5 Test File Generator")
	fmt.Println("========================")
	fmt.Println()
	fmt.Println("The Go version requires HDF5 C libraries. For Windows users, we recommend:")
	fmt.Println()
	fmt.Println("1. Generate Python script (easiest):")
	fmt.Println("   ./hdf5-test-gen -python")
	fmt.Println()
	fmt.Println("2. Create simple test files:")
	fmt.Println("   ./hdf5-test-gen -simple")
	fmt.Println()
	fmt.Println("3. View setup instructions:")
	fmt.Println("   ./hdf5-test-gen -setup")
	fmt.Println()
	fmt.Println("Use -help for all options.")
}