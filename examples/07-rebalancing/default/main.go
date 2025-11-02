// Example 1: Default (No Rebalancing)
//
// This example demonstrates the default behavior: NO automatic rebalancing.
// This matches HDF5 C library behavior and is optimal for:
//   - Append-only workloads (no deletions)
//   - Small files (<100MB)
//   - Read-heavy workloads where write performance is critical
//
// Performance: Fastest deletion (0% overhead), but B-tree can become sparse.

package main

import (
	"fmt"
	"log"

	"github.com/scigolib/hdf5"
)

func main() {
	fmt.Println("Example 1: Default (No Rebalancing)")
	fmt.Println("=====================================")
	fmt.Println()

	// Create file with NO rebalancing options (default behavior)
	// This is the same as HDF5 C library: fast deletions, but B-tree may become sparse.
	fw, err := hdf5.CreateForWrite("01-default-output.h5", hdf5.CreateTruncate)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	fmt.Println("✅ Created file with default settings (no rebalancing)")
	fmt.Println()

	// Create dataset
	ds, err := fw.CreateDataset("/temperature", hdf5.Float64, []uint64{1000})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	// Write many attributes (will trigger dense storage at 8+ attributes)
	fmt.Println("Writing 100 attributes...")
	for i := 0; i < 100; i++ {
		attrName := fmt.Sprintf("measurement_%d", i)
		if err := ds.WriteAttribute(attrName, 20.0+float64(i)*0.1); err != nil {
			log.Fatalf("Failed to write attribute: %v", err)
		}
	}
	fmt.Printf("✅ Wrote 100 attributes (dense storage, B-tree used)\n\n")

	// Delete half (simulating data cleanup)
	fmt.Println("Deleting 50 attributes (no rebalancing)...")
	for i := 0; i < 50; i++ {
		attrName := fmt.Sprintf("measurement_%d", i)
		if err := ds.DeleteAttribute(attrName); err != nil {
			log.Fatalf("Failed to delete attribute: %v", err)
		}
	}
	fmt.Printf("✅ Deleted 50 attributes\n\n")

	// Performance notes
	fmt.Println("Performance Characteristics:")
	fmt.Println("  - Deletion speed: FASTEST (baseline, 0% overhead)")
	fmt.Println("  - B-tree state: May become sparse (50%% of nodes underutilized)")
	fmt.Println("  - Disk space: May waste space (deleted records not reclaimed)")
	fmt.Println("  - CPU overhead: ZERO (no rebalancing)")
	fmt.Println()

	fmt.Println("When to use:")
	fmt.Println("  ✅ Append-only workloads (no deletions)")
	fmt.Println("  ✅ Small files (<100MB)")
	fmt.Println("  ✅ Maximum write performance required")
	fmt.Println("  ✅ Identical behavior to HDF5 C library")
	fmt.Println()

	fmt.Println("When NOT to use:")
	fmt.Println("  ❌ Many deletions (B-tree becomes very sparse)")
	fmt.Println("  ❌ Disk space is limited (no space reclamation)")
	fmt.Println("  ❌ Search performance is critical (sparse tree = slower searches)")
	fmt.Println()

	fmt.Println("✅ Example complete! File: 01-default-output.h5")
	fmt.Println()
	fmt.Println("Next: Try 02-lazy.go for batch rebalancing (10-100x faster than immediate rebalancing)")
}
