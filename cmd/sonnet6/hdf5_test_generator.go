package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gonum.org/v1/hdf5"
)

// HDF5Version represents different HDF5 format versions
type HDF5Version int

const (
	HDF5v18 HDF5Version = iota // HDF5 1.8.x
	HDF5v110                   // HDF5 1.10.x
	HDF5v112                   // HDF5 1.12.x
)

func (v HDF5Version) String() string {
	switch v {
	case HDF5v18:
		return "1.8"
	case HDF5v110:
		return "1.10"
	case HDF5v112:
		return "1.12"
	default:
		return "unknown"
	}
}

// TestFileConfig holds configuration for test file generation
type TestFileConfig struct {
	OutputDir   string
	Version     HDF5Version
	FilePrefix  string
	NumFiles    int
	WithGroups  bool
	WithAttrs   bool
	WithDatasets bool
	Verbose     bool
}

// TestDataGenerator generates different types of test data
type TestDataGenerator struct {
	config TestFileConfig
}

func NewTestDataGenerator(config TestFileConfig) *TestDataGenerator {
	return &TestDataGenerator{config: config}
}

func (g *TestDataGenerator) CreateTestFile(filename string) error {
	if g.config.Verbose {
		fmt.Printf("Creating test file: %s (HDF5 v%s)\n", filename, g.config.Version.String())
	}

	// Create HDF5 file with version-specific properties
	file, err := g.createFileWithVersion(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Add test content based on configuration
	if g.config.WithGroups {
		if err := g.createTestGroups(file); err != nil {
			return fmt.Errorf("failed to create groups: %w", err)
		}
	}

	if g.config.WithDatasets {
		if err := g.createTestDatasets(file); err != nil {
			return fmt.Errorf("failed to create datasets: %w", err)
		}
	}

	if g.config.WithAttrs {
		if err := g.createTestAttributes(file); err != nil {
			return fmt.Errorf("failed to create attributes: %w", err)
		}
	}

	return nil
}

func (g *TestDataGenerator) createFileWithVersion(filename string) (*hdf5.File, error) {
	// Create file access property list to set HDF5 version
	fapl, err := hdf5.NewPropList(hdf5.P_FILE_ACCESS)
	if err != nil {
		return nil, err
	}
	defer fapl.Close()

	// Set version-specific properties
	switch g.config.Version {
	case HDF5v18:
		// For HDF5 1.8.x compatibility
		if err := fapl.SetLibverBounds(hdf5.F_LIBVER_EARLIEST, hdf5.F_LIBVER_V18); err != nil {
			return nil, fmt.Errorf("failed to set v1.8 bounds: %w", err)
		}
	case HDF5v110:
		// For HDF5 1.10.x compatibility
		if err := fapl.SetLibverBounds(hdf5.F_LIBVER_V18, hdf5.F_LIBVER_V110); err != nil {
			return nil, fmt.Errorf("failed to set v1.10 bounds: %w", err)
		}
	case HDF5v112:
		// For HDF5 1.12.x compatibility (latest)
		if err := fapl.SetLibverBounds(hdf5.F_LIBVER_V110, hdf5.F_LIBVER_LATEST); err != nil {
			return nil, fmt.Errorf("failed to set v1.12 bounds: %w", err)
		}
	}

	// Create the file
	file, err := hdf5.CreateFile(filename, hdf5.F_ACC_TRUNC, hdf5.P_DEFAULT, fapl.Id)
	if err != nil {
		return nil, err
	}

	return file, nil
}

func (g *TestDataGenerator) createTestGroups(file *hdf5.File) error {
	groups := []string{
		"/data",
		"/metadata",
		"/data/raw",
		"/data/processed",
		"/metadata/experiment",
		"/metadata/analysis",
	}

	for _, groupPath := range groups {
		group, err := file.CreateGroup(groupPath)
		if err != nil {
			return fmt.Errorf("failed to create group %s: %w", groupPath, err)
		}
		group.Close()

		if g.config.Verbose {
			fmt.Printf("  Created group: %s\n", groupPath)
		}
	}

	return nil
}

func (g *TestDataGenerator) createTestDatasets(file *hdf5.File) error {
	// Create different types of datasets for comprehensive testing
	
	// 1D integer array
	if err := g.createIntegerDataset(file, "/integers_1d", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}); err != nil {
		return err
	}

	// 2D float array
	floatData := [][]float64{
		{1.1, 2.2, 3.3},
		{4.4, 5.5, 6.6},
		{7.7, 8.8, 9.9},
	}
	if err := g.createFloat2DDataset(file, "/floats_2d", floatData); err != nil {
		return err
	}

	// String dataset
	strings := []string{"hello", "world", "hdf5", "test", "data"}
	if err := g.createStringDataset(file, "/strings", strings); err != nil {
		return err
	}

	// Large dataset for performance testing
	if err := g.createLargeDataset(file, "/large_dataset"); err != nil {
		return err
	}

	// Chunked and compressed dataset (version dependent)
	if g.config.Version >= HDF5v18 {
		if err := g.createChunkedDataset(file, "/chunked_compressed"); err != nil {
			return err
		}
	}

	return nil
}

func (g *TestDataGenerator) createIntegerDataset(file *hdf5.File, name string, data []int) error {
	dims := []uint{uint(len(data))}
	space, err := hdf5.CreateSimpleDataspace(dims, nil)
	if err != nil {
		return err
	}
	defer space.Close()

	dataset, err := file.CreateDataset(name, hdf5.T_NATIVE_INT, space)
	if err != nil {
		return err
	}
	defer dataset.Close()

	if err := dataset.Write(&data[0], hdf5.T_NATIVE_INT); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created integer dataset: %s\n", name)
	}
	return nil
}

func (g *TestDataGenerator) createFloat2DDataset(file *hdf5.File, name string, data [][]float64) error {
	rows, cols := len(data), len(data[0])
	dims := []uint{uint(rows), uint(cols)}
	
	space, err := hdf5.CreateSimpleDataspace(dims, nil)
	if err != nil {
		return err
	}
	defer space.Close()

	dataset, err := file.CreateDataset(name, hdf5.T_NATIVE_DOUBLE, space)
	if err != nil {
		return err
	}
	defer dataset.Close()

	// Flatten 2D array for writing
	flatData := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			flatData[i*cols+j] = data[i][j]
		}
	}

	if err := dataset.Write(&flatData[0], hdf5.T_NATIVE_DOUBLE); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created 2D float dataset: %s (%dx%d)\n", name, rows, cols)
	}
	return nil
}

func (g *TestDataGenerator) createStringDataset(file *hdf5.File, name string, data []string) error {
	// Create variable-length string type
	strType, err := hdf5.CreateStringType()
	if err != nil {
		return err
	}
	defer strType.Close()

	dims := []uint{uint(len(data))}
	space, err := hdf5.CreateSimpleDataspace(dims, nil)
	if err != nil {
		return err
	}
	defer space.Close()

	dataset, err := file.CreateDataset(name, strType, space)
	if err != nil {
		return err
	}
	defer dataset.Close()

	// Convert strings to C-style strings for HDF5
	cStrings := make([][]byte, len(data))
	for i, s := range data {
		cStrings[i] = []byte(s + "\x00")
	}

	if err := dataset.WriteRawData(&cStrings[0][0], strType); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created string dataset: %s\n", name)
	}
	return nil
}

func (g *TestDataGenerator) createLargeDataset(file *hdf5.File, name string) error {
	// Create a 1000x1000 dataset for performance testing
	rows, cols := 1000, 1000
	dims := []uint{uint(rows), uint(cols)}
	
	space, err := hdf5.CreateSimpleDataspace(dims, nil)
	if err != nil {
		return err
	}
	defer space.Close()

	dataset, err := file.CreateDataset(name, hdf5.T_NATIVE_DOUBLE, space)
	if err != nil {
		return err
	}
	defer dataset.Close()

	// Generate test data
	data := make([]float64, rows*cols)
	for i := 0; i < rows*cols; i++ {
		data[i] = float64(i) * 0.001
	}

	if err := dataset.Write(&data[0], hdf5.T_NATIVE_DOUBLE); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created large dataset: %s (%dx%d)\n", name, rows, cols)
	}
	return nil
}

func (g *TestDataGenerator) createChunkedDataset(file *hdf5.File, name string) error {
	dims := []uint{1000, 1000}
	chunks := []uint{100, 100}
	
	space, err := hdf5.CreateSimpleDataspace(dims, nil)
	if err != nil {
		return err
	}
	defer space.Close()

	// Create dataset creation property list for chunking and compression
	dcpl, err := hdf5.NewPropList(hdf5.P_DATASET_CREATE)
	if err != nil {
		return err
	}
	defer dcpl.Close()

	// Set chunking
	if err := dcpl.SetChunk(chunks); err != nil {
		return err
	}

	// Set compression (gzip level 6)
	if err := dcpl.SetDeflate(6); err != nil {
		return err
	}

	dataset, err := file.CreateDatasetWith(name, hdf5.T_NATIVE_DOUBLE, space, dcpl)
	if err != nil {
		return err
	}
	defer dataset.Close()

	// Write test data
	data := make([]float64, 1000*1000)
	for i := range data {
		data[i] = float64(i%100) / 10.0
	}

	if err := dataset.Write(&data[0], hdf5.T_NATIVE_DOUBLE); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created chunked/compressed dataset: %s\n", name)
	}
	return nil
}

func (g *TestDataGenerator) createTestAttributes(file *hdf5.File) error {
	// Root attributes
	if err := g.createStringAttribute(file, "title", "HDF5 Test File"); err != nil {
		return err
	}
	if err := g.createStringAttribute(file, "version", g.config.Version.String()); err != nil {
		return err
	}
	if err := g.createStringAttribute(file, "created", time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	if err := g.createIntAttribute(file, "test_number", 42); err != nil {
		return err
	}
	if err := g.createFloatAttribute(file, "pi", 3.14159265359); err != nil {
		return err
	}

	if g.config.Verbose {
		fmt.Printf("  Created root attributes\n")
	}
	return nil
}

func (g *TestDataGenerator) createStringAttribute(obj hdf5.Object, name, value string) error {
	strType, err := hdf5.CreateStringType()
	if err != nil {
		return err
	}
	defer strType.Close()

	if err := strType.SetSize(len(value)); err != nil {
		return err
	}

	space, err := hdf5.CreateScalarDataspace()
	if err != nil {
		return err
	}
	defer space.Close()

	attr, err := obj.CreateAttribute(name, strType, space)
	if err != nil {
		return err
	}
	defer attr.Close()

	return attr.Write([]byte(value), strType)
}

func (g *TestDataGenerator) createIntAttribute(obj hdf5.Object, name string, value int) error {
	space, err := hdf5.CreateScalarDataspace()
	if err != nil {
		return err
	}
	defer space.Close()

	attr, err := obj.CreateAttribute(name, hdf5.T_NATIVE_INT, space)
	if err != nil {
		return err
	}
	defer attr.Close()

	return attr.Write(&value, hdf5.T_NATIVE_INT)
}

func (g *TestDataGenerator) createFloatAttribute(obj hdf5.Object, name string, value float64) error {
	space, err := hdf5.CreateScalarDataspace()
	if err != nil {
		return err
	}
	defer space.Close()

	attr, err := obj.CreateAttribute(name, hdf5.T_NATIVE_DOUBLE, space)
	if err != nil {
		return err
	}
	defer attr.Close()

	return attr.Write(&value, hdf5.T_NATIVE_DOUBLE)
}

func parseVersion(versionStr string) (HDF5Version, error) {
	switch strings.ToLower(versionStr) {
	case "1.8", "18":
		return HDF5v18, nil
	case "1.10", "110":
		return HDF5v110, nil
	case "1.12", "112":
		return HDF5v112, nil
	default:
		return HDF5v18, fmt.Errorf("unsupported version: %s (supported: 1.8, 1.10, 1.12)", versionStr)
	}
}

func main() {
	var (
		outputDir   = flag.String("output", ".", "Output directory for test files")
		versionStr  = flag.String("version", "1.12", "HDF5 version (1.8, 1.10, 1.12)")
		prefix      = flag.String("prefix", "test", "File prefix")
		numFiles    = flag.Int("count", 1, "Number of test files to generate")
		withGroups  = flag.Bool("groups", true, "Include test groups")
		withAttrs   = flag.Bool("attrs", true, "Include test attributes")
		withDatasets = flag.Bool("datasets", true, "Include test datasets")
		verbose     = flag.Bool("verbose", false, "Verbose output")
		help        = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("HDF5 Test File Generator")
		fmt.Println("========================")
		fmt.Println("Creates valid HDF5 test files for different HDF5 versions.")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  # Create a single test file with HDF5 v1.12")
		fmt.Println("  ./hdf5-test-gen")
		fmt.Println()
		fmt.Println("  # Create 5 test files for HDF5 v1.8")
		fmt.Println("  ./hdf5-test-gen -version 1.8 -count 5")
		fmt.Println()
		fmt.Println("  # Create minimal test file (no groups/attrs)")
		fmt.Println("  ./hdf5-test-gen -groups=false -attrs=false")
		return
	}

	// Parse version
	version, err := parseVersion(*versionStr)
	if err != nil {
		log.Fatal(err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Configure test file generator
	config := TestFileConfig{
		OutputDir:    *outputDir,
		Version:      version,
		FilePrefix:   *prefix,
		NumFiles:     *numFiles,
		WithGroups:   *withGroups,
		WithAttrs:    *withAttrs,
		WithDatasets: *withDatasets,
		Verbose:      *verbose,
	}

	generator := NewTestDataGenerator(config)

	// Generate test files
	fmt.Printf("Generating %d HDF5 v%s test file(s)...\n", *numFiles, version.String())
	
	for i := 0; i < *numFiles; i++ {
		var filename string
		if *numFiles == 1 {
			filename = filepath.Join(*outputDir, fmt.Sprintf("%s_v%s.h5", *prefix, version.String()))
		} else {
			filename = filepath.Join(*outputDir, fmt.Sprintf("%s_v%s_%03d.h5", *prefix, version.String(), i+1))
		}

		if err := generator.CreateTestFile(filename); err != nil {
			log.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	fmt.Printf("Successfully generated %d test file(s) in %s\n", *numFiles, *outputDir)
}