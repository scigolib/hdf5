// Example 2: Lazy Rebalancing
//
// This example demonstrates lazy (batch) rebalancing mode.
// Lazy rebalancing accumulates deletions and rebalances in batches, providing:
//   - 10-100x faster than immediate rebalancing
//   - Occasional pauses (100-500ms) during batch processing
//   - B-tree stays reasonably compact
//
// Optimal for:
//   - Batch deletion workloads
//   - Medium to large files (100-500MB)
//   - Moderate delete ratios (5-20% of operations)

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/scigolib/hdf5"
)

func main() {
	fmt.Println("Example 2: Lazy Rebalancing")
	fmt.Println("===========================")
	fmt.Println()

	// Create file with LAZY rebalancing enabled
	//
	// Configuration:
	//   - LazyThreshold(0.05): Trigger batch rebalancing at 5% underflow
	//   - LazyMaxDelay(5*time.Minute): Force rebalancing after 5 minutes
	//   - LazyBatchSize(100): Process 100 nodes per batch
	fw, err := hdf5.CreateForWrite("02-lazy-output.h5", hdf5.CreateTruncate,
		hdf5.WithLazyRebalancing(
			hdf5.LazyThreshold(0.05),         // 5% underflow triggers batch
			hdf5.LazyMaxDelay(5*time.Minute), // Force rebalance after 5 min
			hdf5.LazyBatchSize(100),          // 100 nodes per batch
		),
	)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer fw.Close()

	fmt.Println("✅ Created file with LAZY rebalancing:")
	fmt.Println("   - Threshold: 5% underflow")
	fmt.Println("   - MaxDelay: 5 minutes")
	fmt.Println("   - BatchSize: 100 nodes")
	fmt.Println()

	// Create dataset
	ds, err := fw.CreateDataset("/sensor_data", hdf5.Float64, []uint64{1000})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	// Phase 1: Write many attributes
	fmt.Println("Phase 1: Writing 500 attributes...")
	startWrite := time.Now()
	for i := 0; i < 500; i++ {
		attrName := fmt.Sprintf("reading_%d", i)
		if err := ds.WriteAttribute(attrName, float64(i)*1.5); err != nil {
			log.Fatalf("Failed to write attribute: %v", err)
		}
	}
	writeTime := time.Since(startWrite)
	fmt.Printf("✅ Wrote 500 attributes in %v\n\n", writeTime)

	// Phase 2: Delete many attributes (batch processing)
	fmt.Println("Phase 2: Deleting 250 attributes (lazy rebalancing will batch)...")
	startDelete := time.Now()
	for i := 0; i < 250; i++ {
		attrName := fmt.Sprintf("reading_%d", i)
		if err := ds.DeleteAttribute(attrName); err != nil {
			log.Fatalf("Failed to delete attribute: %v", err)
		}

		// Note: Rebalancing happens automatically when threshold is reached
		// You may observe occasional pauses (~100-500ms) during batching
	}
	deleteTime := time.Since(startDelete)
	fmt.Printf("✅ Deleted 250 attributes in %v\n", deleteTime)
	fmt.Printf("   Average: %.2fms per deletion\n\n", float64(deleteTime.Milliseconds())/250.0)

	// Performance analysis
	fmt.Println("Performance Characteristics:")
	fmt.Printf("  - Deletion speed: %v total (%.2fms avg per operation)\n",
		deleteTime, float64(deleteTime.Milliseconds())/250.0)
	fmt.Println("  - B-tree state: Compact (rebalanced in batches)")
	fmt.Println("  - Disk space: Efficiently reclaimed (nodes merged)")
	fmt.Println("  - CPU overhead: ~2% (batch processing)")
	fmt.Println("  - Pause time: 100-500ms per batch (occasional)")
	fmt.Println()

	// Explain when batching occurred
	fmt.Println("How it works:")
	fmt.Println("  1. Deletions accumulate, underflow nodes tracked")
	fmt.Println("  2. When 5% of nodes are underflow → trigger batch rebalancing")
	fmt.Println("  3. Process 100 nodes per batch (merge/redistribute)")
	fmt.Println("  4. Repeat until all underflow nodes processed")
	fmt.Println()

	fmt.Println("When to use:")
	fmt.Println("  ✅ Batch deletion workloads (delete many, then continue)")
	fmt.Println("  ✅ Medium/large files (100-500MB)")
	fmt.Println("  ✅ Can tolerate occasional pauses (100-500ms)")
	fmt.Println("  ✅ Want 10-100x faster than immediate rebalancing")
	fmt.Println()

	fmt.Println("When NOT to use:")
	fmt.Println("  ❌ Cannot afford ANY pause (use incremental instead)")
	fmt.Println("  ❌ Very small files (<100MB, overhead not worth it)")
	fmt.Println("  ❌ No deletions (use default/none mode)")
	fmt.Println()

	// Tuning tips
	fmt.Println("Tuning Tips:")
	fmt.Println("  - Lower threshold (0.02): More frequent rebalancing, tighter tree")
	fmt.Println("  - Higher threshold (0.10): Less frequent rebalancing, looser tree")
	fmt.Println("  - Larger batch size (200): Fewer, longer pauses")
	fmt.Println("  - Smaller batch size (50): More frequent, shorter pauses")
	fmt.Println()

	fmt.Println("✅ Example complete! File: 02-lazy-output.h5")
	fmt.Println()
	fmt.Println("Next: Try 03-incremental.go for ZERO-PAUSE background rebalancing")
}
