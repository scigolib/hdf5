// Package main provides a command-line utility to dump HDF5 file contents.
// It displays raw hex data from specific offsets in HDF5 files for debugging.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	// Define command-line flags
	offset := flag.Int64("offset", 0, "Offset in file to start dumping from")
	length := flag.Int("length", 128, "Number of bytes to dump")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: dump_hdf5 [flags] <file.h5>")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		return
	}

	file := args[0]
	f, err := os.Open(file)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Failed to close file: %v", err)
		}
	}()

	// Get file size
	fileInfo, err := f.Stat()
	if err != nil {
		log.Fatalf("Failed to get file info: %v", err)
	}
	fileSize := fileInfo.Size()

	// Validate parameters
	if *offset < 0 || *offset >= fileSize {
		log.Fatalf("Invalid offset: %d (file size: %d)", *offset, fileSize)
	}

	if *length < 1 {
		log.Fatalf("Invalid length: %d", *length)
	}

	// Calculate actual read length
	remaining := fileSize - *offset
	readLength := int64(*length)
	if readLength > remaining {
		readLength = remaining
		fmt.Printf("Warning: requested length %d exceeds available bytes (%d). Dumping %d bytes.\n",
			*length, remaining, readLength)
	}

	// Read specified portion of file
	buf := make([]byte, readLength)
	n, err := f.ReadAt(buf, *offset)
	if err != nil {
		log.Printf("Read error: %v (read %d of %d bytes)", err, n, readLength)
	}

	fmt.Printf("Dumping %d bytes at offset 0x%x (%d) of %s (size: %d bytes):\n",
		n, *offset, *offset, file, fileSize)

	for i := 0; i < n; i += 16 {
		end := i + 16
		if end > n {
			end = n
		}
		chunk := buf[i:end]

		// Hexadecimal dump
		fmt.Printf("%08x: ", *offset+int64(i))
		for j := 0; j < 16; j++ {
			if j < len(chunk) {
				fmt.Printf("%02x ", chunk[j])
			} else {
				fmt.Print("   ")
			}
			if j == 7 {
				fmt.Print(" ")
			}
		}
		fmt.Print(" |")

		// ASCII representation
		for _, b := range chunk {
			if b >= 32 && b <= 126 {
				fmt.Printf("%c", b)
			} else {
				fmt.Print(".")
			}
		}
		fmt.Println("|")
	}
}
