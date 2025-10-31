package hdf5

import (
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestSuperblockV0FileCreation tests v0 file creation with exact addresses.
// This verifies that v0 superblock with Object Header v1 works correctly.
func TestSuperblockV0FileCreation(t *testing.T) {
	tmpFile := "test_v0_debug.h5"
	defer os.Remove(tmpFile)

	// Create v0 file
	fw, err := CreateForWrite(tmpFile, CreateTruncate, WithSuperblockVersion(core.Version0))
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Check file structure
	t.Logf("Root group addr: 0x%X", fw.rootGroupAddr)
	t.Logf("Root B-tree addr: 0x%X", fw.rootBTreeAddr)
	t.Logf("Root symbol table node addr: 0x%X", fw.rootStNodeAddr)
	t.Logf("Root heap addr: 0x%X", fw.rootHeapAddr)

	// Check allocator state
	eof := fw.writer.EndOfFile()
	t.Logf("Allocator EOF: 0x%X (%d)", eof, eof)

	// Close and check file size
	if err := fw.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Check actual file size
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	t.Logf("Actual file size: %d bytes (0x%X)", info.Size(), info.Size())

	// Read and verify signature at SNOD address
	f, err := os.Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer f.Close()

	sig := make([]byte, 4)
	if _, err := f.ReadAt(sig, int64(fw.rootStNodeAddr)); err != nil {
		t.Fatalf("Failed to read SNOD signature: %v", err)
	}

	t.Logf("SNOD signature at 0x%X: %s (hex: %X)", fw.rootStNodeAddr, string(sig), sig)

	if string(sig) != "SNOD" {
		t.Errorf("Expected SNOD signature, got %s (hex: %X)", string(sig), sig)
	}
}
