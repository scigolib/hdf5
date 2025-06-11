package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// HDF5 signature bytes
var hdf5Signature = []byte{0x89, 0x48, 0x44, 0x46, 0x0d, 0x0a, 0x1a, 0x0a}

func main() {
	files := []string{
		"testdata/v0.h5",
		"testdata/v2.h5",
		"testdata/v3.h5",
		"testdata/with_groups.h5",
	}

	for _, filename := range files {
		fmt.Printf("\n=== Debugging %s ===\n", filename)
		debugHDF5File(filename)
	}
}

func debugHDF5File(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("❌ Cannot open file: %v\n", err)
		return
	}
	defer file.Close()

	// Check file size
	stat, err := file.Stat()
	if err != nil {
		fmt.Printf("❌ Cannot stat file: %v\n", err)
		return
	}
	fmt.Printf("📏 File size: %d bytes\n", stat.Size())

	// Read first 512 bytes for analysis
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		fmt.Printf("❌ Cannot read file: %v\n", err)
		return
	}
	buffer = buffer[:n]

	// Check HDF5 signature
	if len(buffer) < 8 {
		fmt.Printf("❌ File too small (less than 8 bytes)\n")
		return
	}

	signature := buffer[:8]
	if !compareBytes(signature, hdf5Signature) {
		fmt.Printf("❌ Invalid HDF5 signature: %x\n", signature)
		return
	}
	fmt.Printf("✅ Valid HDF5 signature found\n")

	// Try to read superblock
	fmt.Printf("🔍 Analyzing superblock...\n")
	analyzeSuperblock(buffer[8:])

	// Show hex dump of first 64 bytes
	fmt.Printf("🔢 Hex dump (first 64 bytes):\n")
	printHexDump(buffer[:min(64, len(buffer))])
}

func analyzeSuperblock(data []byte) {
	if len(data) < 16 {
		fmt.Printf("❌ Not enough data for superblock analysis\n")
		return
	}

	// Try to read superblock version
	if len(data) >= 1 {
		version := data[0]
		fmt.Printf("📋 Superblock version: %d\n", version)

		switch version {
		case 0:
			analyzeSuperblockV0(data)
		case 1:
			analyzeSuperblockV1(data)
		case 2, 3:
			analyzeSuperblockV2V3(data)
		default:
			fmt.Printf("❓ Unknown superblock version: %d\n", version)
		}
	}
}

func analyzeSuperblockV0(data []byte) {
	fmt.Printf("🔍 Analyzing Version 0 superblock...\n")
	
	if len(data) < 21 {
		fmt.Printf("❌ Not enough data for V0 superblock (need 21+ bytes, have %d)\n", len(data))
		return
	}

	// Version 0 superblock structure:
	// Byte 0: Version
	// Byte 1: Version of file's free space storage
	// Byte 2: Version of root group symbol table entry
	// Byte 3: Reserved
	// Byte 4: Version of shared header message format
	// Byte 5: Sizeof offsets
	// Byte 6: Sizeof lengths
	// Byte 7: Reserved
	// Bytes 8-9: Group leaf node K
	// Bytes 10-11: Group internal node K
	// Bytes 12-15: File consistency flags
	// Bytes 16-19 or 16-23: Base address (depends on sizeof offsets)

	fmt.Printf("  Free space version: %d\n", data[1])
	fmt.Printf("  Root group version: %d\n", data[2])
	fmt.Printf("  Shared header version: %d\n", data[4])
	fmt.Printf("  Sizeof offsets: %d\n", data[5])
	fmt.Printf("  Sizeof lengths: %d\n", data[6])

	if data[5] == 0 || data[6] == 0 {
		fmt.Printf("❌ Invalid sizes: offset size=%d, length size=%d\n", data[5], data[6])
		return
	}

	leafK := binary.LittleEndian.Uint16(data[8:10])
	internalK := binary.LittleEndian.Uint16(data[10:12])
	fmt.Printf("  Group leaf K: %d\n", leafK)
	fmt.Printf("  Group internal K: %d\n", internalK)
}

func analyzeSuperblockV1(data []byte) {
	fmt.Printf("🔍 Analyzing Version 1 superblock...\n")
	// Similar to V0 but with some differences
	analyzeSuperblockV0(data) // Basic analysis
}

func analyzeSuperblockV2V3(data []byte) {
	fmt.Printf("🔍 Analyzing Version 2/3 superblock...\n")
	
	if len(data) < 12 {
		fmt.Printf("❌ Not enough data for V2/V3 superblock\n")
		return
	}

	fmt.Printf("  Sizeof offsets: %d\n", data[1])
	fmt.Printf("  Sizeof lengths: %d\n", data[2])
	fmt.Printf("  File consistency flags: %d\n", data[3])
}

func printHexDump(data []byte) {
	for i := 0; i < len(data); i += 16 {
		end := min(i+16, len(data))
		
		// Print offset
		fmt.Printf("%08x: ", i)
		
		// Print hex bytes
		for j := i; j < end; j++ {
			fmt.Printf("%02x ", data[j])
		}
		
		// Pad if needed
		for j := end; j < i+16; j++ {
			fmt.Printf("   ")
		}
		
		// Print ASCII
		fmt.Printf(" |")
		for j := i; j < end; j++ {
			if data[j] >= 32 && data[j] <= 126 {
				fmt.Printf("%c", data[j])
			} else {
				fmt.Printf(".")
			}
		}
		fmt.Printf("|\n")
	}
}

func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
