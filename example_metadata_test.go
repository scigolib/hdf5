package hdf5_test

import (
	"fmt"
	"log"

	"github.com/scigolib/hdf5"
)

// exampleMetadataDataset opens path and returns the dataset at dsPath.
func exampleMetadataDataset(path, dsPath string) (*hdf5.File, *hdf5.Dataset) {
	f, err := hdf5.Open(path)
	if err != nil {
		log.Fatal(err)
	}

	var ds *hdf5.Dataset
	f.Walk(func(p string, obj hdf5.Object) {
		if d, ok := obj.(*hdf5.Dataset); ok && p == dsPath {
			ds = d
		}
	})
	if ds == nil {
		log.Fatalf("%s: dataset %s not found", path, dsPath)
	}

	return f, ds
}

// Shape and MaxShape report the dataspace; a dimension that can grow without
// limit is hdf5.Unlimited.
func ExampleDataset_Shape() {
	f, ds := exampleMetadataDataset("testdata/hdf5_official/tdset2.h5", "/dset1")
	defer f.Close()

	shape, err := ds.Shape()
	if err != nil {
		fmt.Println(err)
		return
	}
	maxShape, err := ds.MaxShape()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("shape:", shape)
	for i, m := range maxShape {
		if m == hdf5.Unlimited {
			fmt.Printf("dimension %d is unlimited\n", i)
		}
	}
	// Output:
	// shape: [10 20]
	// dimension 0 is unlimited
}

// Datatype describes the element type, including the members of a compound.
func ExampleDataset_Datatype() {
	f, ds := exampleMetadataDataset("testdata/compound_test.h5", "/matrix")
	defer f.Close()

	dt, err := ds.Datatype()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("%s, %d bytes, %s\n", dt.Class, dt.Size, dt.Order)
	for i := range dt.Members {
		m := &dt.Members[i]
		fmt.Printf("  %-12s +%-3d %s (%d bytes)\n", m.Name, m.Offset, m.Type.Class, m.Type.Size)
	}
	// Output:
	// compound, 26 bytes, little-endian
	//   id           +0   integer (4 bytes)
	//   temperature  +4   float (4 bytes)
	//   pressure     +8   float (8 bytes)
	//   name         +16  string (10 bytes)
}

// Layout reports the storage layout, chunk dimensions and filter pipeline.
func ExampleDataset_Layout() {
	f, ds := exampleMetadataDataset("testdata/gzip_test.h5", "/shuffled_compressed")
	defer f.Close()

	layout, err := ds.Layout()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(layout.Class, layout.ChunkDims)
	for _, filter := range layout.Filters {
		fmt.Println(filter.ID, uint16(filter.ID), filter.ClientData)
	}
	// Output:
	// chunked [100]
	// shuffle 2 [4]
	// deflate 1 [9]
}
