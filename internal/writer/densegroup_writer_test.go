// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestDenseGroupWriter_Creation tests basic creation.
func TestDenseGroupWriter_Creation(t *testing.T) {
	dgw := NewDenseGroupWriter("/group")

	// Verify initialization
	if dgw == nil {
		t.Fatal("NewDenseGroupWriter returned nil")
	}

	if dgw.name != "/group" {
		t.Errorf("Name mismatch: got %s, want /group", dgw.name)
	}

	if len(dgw.links) != 0 {
		t.Errorf("Links should be empty, got %d", len(dgw.links))
	}

	if dgw.fractalHeap == nil {
		t.Error("Fractal heap not initialized")
	}

	if dgw.btree == nil {
		t.Error("B-tree not initialized")
	}

	if dgw.linkInfo == nil {
		t.Error("Link info not initialized")
	}
}

// TestDenseGroupWriter_AddLink_Single tests adding one link.
func TestDenseGroupWriter_AddLink_Single(t *testing.T) {
	dgw := NewDenseGroupWriter("/group")

	err := dgw.AddLink("dataset1", 0x1000)
	if err != nil {
		t.Fatalf("AddLink failed: %v", err)
	}

	if len(dgw.links) != 1 {
		t.Errorf("Link count mismatch: got %d, want 1", len(dgw.links))
	}

	if dgw.links[0].name != "dataset1" {
		t.Errorf("Link name mismatch: got %s, want dataset1", dgw.links[0].name)
	}

	if dgw.links[0].targetAddr != 0x1000 {
		t.Errorf("Link target mismatch: got 0x%x, want 0x1000", dgw.links[0].targetAddr)
	}
}

// TestDenseGroupWriter_AddLink_Multiple tests adding multiple links.
func TestDenseGroupWriter_AddLink_Multiple(t *testing.T) {
	dgw := NewDenseGroupWriter("/group")

	// Add 10 links
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("link%d", i)
		addr := uint64(0x1000 + i*0x100)

		err := dgw.AddLink(name, addr)
		if err != nil {
			t.Fatalf("AddLink %d failed: %v", i, err)
		}
	}

	if len(dgw.links) != 10 {
		t.Errorf("Link count mismatch: got %d, want 10", len(dgw.links))
	}

	// Verify all links
	for i := 0; i < 10; i++ {
		expectedName := fmt.Sprintf("link%d", i)
		expectedAddr := uint64(0x1000 + i*0x100)

		if dgw.links[i].name != expectedName {
			t.Errorf("Link %d name mismatch: got %s, want %s", i, dgw.links[i].name, expectedName)
		}

		if dgw.links[i].targetAddr != expectedAddr {
			t.Errorf("Link %d addr mismatch: got 0x%x, want 0x%x", i, dgw.links[i].targetAddr, expectedAddr)
		}
	}
}

// TestDenseGroupWriter_AddLink_EmptyName tests error on empty name.
func TestDenseGroupWriter_AddLink_EmptyName(t *testing.T) {
	dgw := NewDenseGroupWriter("/group")

	err := dgw.AddLink("", 0x1000)
	if err == nil {
		t.Error("Expected error for empty name, got nil")
	}

	if len(dgw.links) != 0 {
		t.Errorf("Links should be empty after error, got %d", len(dgw.links))
	}
}

// TestDenseGroupWriter_AddLink_Duplicate tests error on duplicate name.
func TestDenseGroupWriter_AddLink_Duplicate(t *testing.T) {
	dgw := NewDenseGroupWriter("/group")

	// Add first link
	err := dgw.AddLink("dataset1", 0x1000)
	if err != nil {
		t.Fatalf("First AddLink failed: %v", err)
	}

	// Try to add duplicate
	err = dgw.AddLink("dataset1", 0x2000)
	if err == nil {
		t.Error("Expected error for duplicate name, got nil")
	}

	if len(dgw.links) != 1 {
		t.Errorf("Link count should be 1 after duplicate, got %d", len(dgw.links))
	}
}

// TestDenseGroupWriter_WriteToFile_NoLinks tests error when no links.
func TestDenseGroupWriter_WriteToFile_NoLinks(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/group")

	// Try to write without links
	_, err = dgw.WriteToFile(fw, allocator, sb)
	if err == nil {
		t.Error("Expected error when writing group with no links")
	}
}

// TestDenseGroupWriter_WriteToFile_SingleLink tests writing with one link.
func TestDenseGroupWriter_WriteToFile_SingleLink(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/group")
	err = dgw.AddLink("dataset1", 0x1000)
	if err != nil {
		t.Fatalf("AddLink failed: %v", err)
	}

	// Write dense group
	addr, err := dgw.WriteToFile(fw, allocator, sb)
	if err != nil {
		t.Fatalf("WriteToFile failed: %v", err)
	}

	if addr == 0 {
		t.Error("Object header address should not be 0")
	}

	// Verify addresses were set in Link Info Message
	if dgw.linkInfo.FractalHeapAddress == 0 {
		t.Error("Fractal heap address not set")
	}

	if dgw.linkInfo.NameBTreeAddress == 0 {
		t.Error("B-tree address not set")
	}

	if dgw.linkInfo.CreationOrderBTreeAddress != 0 {
		t.Error("Creation order B-tree address should be 0 for MVP")
	}

	// Verify file was written (non-zero EOF)
	eof := allocator.EndOfFile()
	if eof <= 48 {
		t.Errorf("File size too small: %d bytes", eof)
	}
}

// TestDenseGroupWriter_WriteToFile_MultipleLinks tests writing with many links.
func TestDenseGroupWriter_WriteToFile_MultipleLinks(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/group")

	// Add 20 links
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("dataset_%03d", i)
		addr := uint64(0x1000 + i*0x100)

		err := dgw.AddLink(name, addr)
		if err != nil {
			t.Fatalf("AddLink %d failed: %v", i, err)
		}
	}

	// Write dense group
	addr, err := dgw.WriteToFile(fw, allocator, sb)
	if err != nil {
		t.Fatalf("WriteToFile failed: %v", err)
	}

	if addr == 0 {
		t.Error("Object header address should not be 0")
	}

	// Verify heap and B-tree were written
	if dgw.linkInfo.FractalHeapAddress == 0 {
		t.Error("Fractal heap address not set")
	}

	if dgw.linkInfo.NameBTreeAddress == 0 {
		t.Error("B-tree address not set")
	}
}

// TestDenseGroupWriter_WriteToFile_LargeScale tests writing with 100+ links.
func TestDenseGroupWriter_WriteToFile_LargeScale(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/large_group")

	// Add 150 links
	for i := 0; i < 150; i++ {
		name := fmt.Sprintf("dataset_%03d", i)
		addr := uint64(i * 0x1000)

		err := dgw.AddLink(name, addr)
		if err != nil {
			t.Fatalf("AddLink %d failed: %v", i, err)
		}
	}

	// Write dense group
	addr, err := dgw.WriteToFile(fw, allocator, sb)
	if err != nil {
		t.Fatalf("WriteToFile failed: %v", err)
	}

	if addr == 0 {
		t.Error("Object header address should not be 0")
	}

	// Verify addresses set
	if dgw.linkInfo.FractalHeapAddress == 0 {
		t.Error("Fractal heap address not set")
	}

	if dgw.linkInfo.NameBTreeAddress == 0 {
		t.Error("B-tree address not set")
	}

	// Verify file size is reasonable (should be several KB)
	eof := allocator.EndOfFile()
	if eof < 1000 {
		t.Errorf("File size too small for 150 links: %d bytes", eof)
	}

	t.Logf("Large scale test: 150 links, file size: %d bytes", eof)
}

// TestDenseGroupWriter_UTF8Names tests Unicode link names.
func TestDenseGroupWriter_UTF8Names(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/unicode")

	// Add links with various Unicode characters
	unicodeLinks := map[string]uint64{
		"файл":      0x1000, // Russian
		"文件":        0x2000, // Chinese
		"ファイル":      0x3000, // Japanese
		"파일":        0x4000, // Korean
		"αρχείο":    0x5000, // Greek
		"fichier":   0x6000, // French (with accents)
		"Übung":     0x7000, // German (with umlaut)
		"español":   0x8000, // Spanish
		"português": 0x9000, // Portuguese
	}

	for name, addr := range unicodeLinks {
		err := dgw.AddLink(name, addr)
		if err != nil {
			t.Fatalf("AddLink failed for %s: %v", name, err)
		}
	}

	// Write dense group
	addr, err := dgw.WriteToFile(fw, allocator, sb)
	if err != nil {
		t.Fatalf("WriteToFile failed: %v", err)
	}

	if addr == 0 {
		t.Error("Object header address should not be 0")
	}

	t.Logf("Unicode test: %d links with various scripts", len(unicodeLinks))
}

// TestDenseGroupWriter_LinkMessage tests link message creation.
func TestDenseGroupWriter_LinkMessage(t *testing.T) {
	dgw := NewDenseGroupWriter("/test")
	sb := createTestSuperblock()

	link := denseLink{
		name:       "testlink",
		targetAddr: 0x123456,
	}

	msg := dgw.createLinkMessage(link, sb)

	// Verify message is not empty
	if len(msg) == 0 {
		t.Error("Link message is empty")
	}

	// Verify version (first byte should be 1)
	if msg[0] != 1 {
		t.Errorf("Link message version mismatch: got %d, want 1", msg[0])
	}

	// Verify type (second byte should be 0 for hard link)
	if msg[1] != 0 {
		t.Errorf("Link type mismatch: got %d, want 0 (hard link)", msg[1])
	}

	// Verify flags (third byte should have character set bit set)
	if msg[2]&0x04 == 0 {
		t.Error("Character set flag not set")
	}

	// Verify encoding (fourth byte should be 0 for UTF-8)
	if msg[3] != 0 {
		t.Errorf("Character encoding mismatch: got %d, want 0 (UTF-8)", msg[3])
	}

	// Verify message contains link name
	linkNameBytes := []byte("testlink")
	found := false
	for i := 0; i < len(msg)-len(linkNameBytes); i++ {
		if string(msg[i:i+len(linkNameBytes)]) == "testlink" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Link name not found in message")
	}

	t.Logf("Link message size: %d bytes", len(msg))
}

// TestDenseGroupWriter_ObjectHeader tests object header creation.
func TestDenseGroupWriter_ObjectHeader(t *testing.T) {
	// Create temporary file
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	fw, err := NewFileWriter(tmpFile, ModeTruncate, 48)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer fw.Close()

	allocator := NewAllocator(48)
	sb := createTestSuperblock()

	dgw := NewDenseGroupWriter("/test")

	// Set Link Info addresses (simulated)
	dgw.linkInfo.FractalHeapAddress = 0x1000
	dgw.linkInfo.NameBTreeAddress = 0x2000
	dgw.linkInfo.CreationOrderBTreeAddress = 0

	// Create object header
	addr, err := dgw.createObjectHeader(fw, allocator, sb)
	if err != nil {
		t.Fatalf("createObjectHeader failed: %v", err)
	}

	if addr == 0 {
		t.Error("Object header address should not be 0")
	}

	t.Logf("Object header written at 0x%x", addr)
}

// TestDenseGroupWriter_CompactUint64 tests compact uint64 encoding.
func TestDenseGroupWriter_CompactUint64(t *testing.T) {
	tests := []struct {
		value    uint64
		wantSize int
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{16777215, 3},
		{16777216, 4},
		{4294967295, 4},
		{4294967296, 5},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("value_%d", tt.value), func(t *testing.T) {
			size := compactUint64Size(tt.value)
			if size != tt.wantSize {
				t.Errorf("compactUint64Size(%d) = %d, want %d", tt.value, size, tt.wantSize)
			}

			// Test encoding
			buf := make([]byte, size)
			encodeCompactUint64(buf, tt.value)

			// Decode and verify
			var decoded uint64
			for i := 0; i < size; i++ {
				decoded |= uint64(buf[i]) << (8 * i)
			}

			if decoded != tt.value {
				t.Errorf("Encode/decode mismatch: encoded %d, decoded %d", tt.value, decoded)
			}
		})
	}
}

// TestDenseGroupWriter_DataspaceMessage tests dataspace message creation.
func TestDenseGroupWriter_DataspaceMessage(t *testing.T) {
	msg := createScalarDataspaceMessage()

	// Verify message size
	if len(msg) != 8 {
		t.Errorf("Dataspace message size mismatch: got %d, want 8", len(msg))
	}

	// Verify version (first byte should be 1)
	if msg[0] != 1 {
		t.Errorf("Dataspace version mismatch: got %d, want 1", msg[0])
	}

	// Verify dimensionality (second byte should be 0 for scalar)
	if msg[1] != 0 {
		t.Errorf("Dimensionality mismatch: got %d, want 0 (scalar)", msg[1])
	}

	// Verify flags (third byte should be 0)
	if msg[2] != 0 {
		t.Errorf("Flags mismatch: got %d, want 0", msg[2])
	}
}

// Helper functions

func createTempFile(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("", "hdf5_densegroup_test_*.h5")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	filename := file.Name()
	file.Close()
	return filename
}

func createTestSuperblock() *core.Superblock {
	return &core.Superblock{
		Version:        core.Version2,
		OffsetSize:     8,
		LengthSize:     8,
		BaseAddress:    0,
		RootGroup:      48,
		Endianness:     binary.LittleEndian,
		SuperExtension: 0,
		DriverInfo:     0,
	}
}

// BenchmarkDenseGroupWriter_AddLink_10 benchmarks adding 10 links.
func BenchmarkDenseGroupWriter_AddLink_10(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dgw := NewDenseGroupWriter("/group")
		for j := 0; j < 10; j++ {
			_ = dgw.AddLink(fmt.Sprintf("link%d", j), uint64(j*0x1000))
		}
	}
}

// BenchmarkDenseGroupWriter_AddLink_100 benchmarks adding 100 links.
func BenchmarkDenseGroupWriter_AddLink_100(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dgw := NewDenseGroupWriter("/group")
		for j := 0; j < 100; j++ {
			_ = dgw.AddLink(fmt.Sprintf("link%d", j), uint64(j*0x1000))
		}
	}
}

// BenchmarkDenseGroupWriter_AddLink_1000 benchmarks adding 1000 links.
func BenchmarkDenseGroupWriter_AddLink_1000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dgw := NewDenseGroupWriter("/group")
		for j := 0; j < 1000; j++ {
			_ = dgw.AddLink(fmt.Sprintf("link%d", j), uint64(j*0x1000))
		}
	}
}
