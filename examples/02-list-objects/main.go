package main

import (
	"fmt"
	"log"

	"github.com/scigolib/hdf5"
)

func main() {
	file, err := hdf5.Open("../testdata/simple_float64.h5")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	fmt.Println("Superblock version:", file.SuperblockVersion())
	fmt.Println("\nObjects in file:")

	file.Walk(func(path string, obj hdf5.Object) {
		switch v := obj.(type) {
		case *hdf5.Group:
			fmt.Printf("  Group: %s (children: %d)\n", path, len(v.Children()))
			for _, child := range v.Children() {
				fmt.Printf("    - %s\n", child.Name())
			}
		case *hdf5.Dataset:
			fmt.Printf("  Dataset: %s\n", path)
		default:
			fmt.Printf("  Unknown: %s (type: %T)\n", path, obj)
		}
	})
}
