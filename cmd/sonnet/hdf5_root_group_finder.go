package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// HDF5 Object Header signature
var objectHeaderSignature = []byte{'O', 'H', 'D', 'R'}

func main() {
	filename := "testdata/v0.h5"
	
	fmt.Printf("=== Analyzing %s ===\n", filename)
	
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Cannot open file: %v\n", err)
		return
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		fmt.Printf("Cannot stat file: %v\n", err)
		return
	}
	fileSize := stat.Size()
	fmt.Printf("File size: %d bytes\n", fileSize)

	// Read the entire file for analysis
	fileData := make([]byte, fileSize)
	if _, err := file.ReadAt(fileData, 0); err != nil {
		fmt.Printf("Cannot read file: %v\n", err)
		return
	}

	// Find all OHDR (Object Header) signatures
	fmt.Printf("\n🔍 Searching for Object Headers (OHDR)...\n")
	
	foundHeaders := findObjectHeaders(fileData)
	if len(foundHeaders) == 0 {
		fmt.Printf("❌ No object headers found\n")
		return
	}

	for i, offset := range foundHeaders {
		fmt.Printf("📋 Object Header #%d found at offset 0x%08X (%d)\n", i+1, offset, offset)
		analyzeObjectHeader(fileData, offset)
	}

	// Try to find the root group specifically
	fmt.Printf("\n🔍 Looking for root group...\n")
	
	// Check if root group might be at offset 0 (relative to some base)
	fmt.Printf("Checking data at superblock end (~0x38)...\n")
	if len(fileData) > 0x38 {
		hexDump(fileData[0x38:min(0x38+64, len(fileData))], 0x38)
	}

	// Check the first object header (likely the root group)
	if len(foundHeaders) > 0 {
		fmt.Printf("\n🎯 First object header is likely the root group\n")
		rootGroupOffset := foundHeaders[0]
		
		fmt.Printf("Root group appears to be at offset 0x%08X\n", rootGroupOffset)
		
		// This is what your parser should use as the root group address
		fmt.Printf("\n✅ SOLUTION: Update your superblock validation to accept root group at offset 0x%08X\n", rootGroupOffset)
		fmt.Printf("   OR: Update your parser to search for the first OHDR after the superblock\n")
	}
}

func findObjectHeaders(data []byte) []int {
	var positions []int
	
	for i := 0; i <= len(data)-4; i++ {
		if data[i] == 'O' && data[i+1] == 'H' && data[i+2] == 'D' && data[i+3] == 'R' {
			positions = append(positions, i)
		}
	}
	
	return positions
}

func analyzeObjectHeader(data []byte, offset int) {
	if offset+16 > len(data) {
		fmt.Printf("  ❌ Not enough data to analyze header\n")
		return
	}

	// Skip "OHDR" signature
	headerData := data[offset+4:]
	
	if len(headerData) < 12 {
		fmt.Printf("  ❌ Header too short\n")
		return
	}

	// Read object header fields
	version := headerData[0]
	flags := headerData[1]
	
	fmt.Printf("  📄 Version: %d\n", version)
	fmt.Printf("  🏷️  Flags: 0x%02X\n", flags)
	
	// Show first 32 bytes of header data
	fmt.Printf("  📋 Header data: ")
	end := min(32, len(headerData))
	for i := 0; i < end; i++ {
		fmt.Printf("%02x ", headerData[i])
		if i == 15 {
			fmt.Printf("\n                  ")
		}
	}
	fmt.Printf("\n")
}

func hexDump(data []byte, baseOffset int) {
	fmt.Printf("Hex dump from offset 0x%08X:\n", baseOffset)
	
	for i := 0; i < len(data); i += 16 {
		end := min(i+16, len(data))
		
		// Print offset
		fmt.Printf("%08x: ", baseOffset+i)
		
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
