// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestFractalHeapLoadFromFile tests loading an existing heap and appending more objects.
//
//nolint:gocognit // Comprehensive integration test covering multiple scenarios
func TestFractalHeapLoadFromFile(t *testing.T) {
	// Create a simple superblock for testing
	sb := &core.Superblock{
		LengthSize: 8,
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Phase 1: Create a new heap, insert some objects, write to buffer
	t.Run("CreateAndWrite", func(t *testing.T) {
		heap := NewWritableFractalHeap(64 * 1024) // 64KB block

		// Insert 3 objects
		obj1 := []byte("first object")
		obj2 := []byte("second object")
		obj3 := []byte("third object")

		id1, err := heap.InsertObject(obj1)
		if err != nil {
			t.Fatalf("Failed to insert object 1: %v", err)
		}

		id2, err := heap.InsertObject(obj2)
		if err != nil {
			t.Fatalf("Failed to insert object 2: %v", err)
		}

		id3, err := heap.InsertObject(obj3)
		if err != nil {
			t.Fatalf("Failed to insert object 3: %v", err)
		}

		// Verify objects can be retrieved
		retrieved1, err := heap.GetObject(id1)
		if err != nil {
			t.Fatalf("Failed to get object 1: %v", err)
		}
		if !bytes.Equal(retrieved1, obj1) {
			t.Errorf("Object 1 mismatch: got %q, want %q", retrieved1, obj1)
		}

		retrieved2, err := heap.GetObject(id2)
		if err != nil {
			t.Fatalf("Failed to get object 2: %v", err)
		}
		if !bytes.Equal(retrieved2, obj2) {
			t.Errorf("Object 2 mismatch: got %q, want %q", retrieved2, obj2)
		}

		retrieved3, err := heap.GetObject(id3)
		if err != nil {
			t.Fatalf("Failed to get object 3: %v", err)
		}
		if !bytes.Equal(retrieved3, obj3) {
			t.Errorf("Object 3 mismatch: got %q, want %q", retrieved3, obj3)
		}

		// Write heap to buffer (using mock writer/allocator)
		buf := newMockFileBuffer()
		allocator := &mockAllocator{nextAddr: 1024}

		heapAddr, err := heap.WriteToFile(buf, allocator, sb)
		if err != nil {
			t.Fatalf("Failed to write heap: %v", err)
		}

		t.Logf("Heap written at address 0x%X", heapAddr)
		t.Logf("Heap has %d objects", heap.Header.NumManagedObjects)

		// Phase 2: Load heap from buffer and append more objects
		t.Run("LoadAndAppend", func(t *testing.T) {
			// Debug: Show what was written
			t.Logf("Mock buffer has %d segments:", len(buf.segments))
			for i, seg := range buf.segments {
				t.Logf("  Segment %d: addr=0x%X, len=%d", i, seg.address, len(seg.data))
			}

			// Create a new heap instance
			heap2 := &WritableFractalHeap{}

			// Load from buffer
			err := heap2.LoadFromFile(buf, heapAddr, sb)
			if err != nil {
				t.Fatalf("Failed to load heap: %v", err)
			}

			// Verify loaded state
			if heap2.Header.NumManagedObjects != 3 {
				t.Errorf("Loaded heap has %d objects, want 3", heap2.Header.NumManagedObjects)
			}

			// Verify existing objects are accessible
			retrieved1, err := heap2.GetObject(id1)
			if err != nil {
				t.Fatalf("Failed to get existing object 1: %v", err)
			}
			if !bytes.Equal(retrieved1, obj1) {
				t.Errorf("Existing object 1 mismatch: got %q, want %q", retrieved1, obj1)
			}

			// Insert a NEW object (this is the read-modify-write!)
			obj4 := []byte("fourth object - appended!")

			id4, err := heap2.InsertObject(obj4)
			if err != nil {
				t.Fatalf("Failed to append object 4: %v", err)
			}

			// Verify the new object
			retrieved4, err := heap2.GetObject(id4)
			if err != nil {
				t.Fatalf("Failed to get appended object 4: %v", err)
			}
			if !bytes.Equal(retrieved4, obj4) {
				t.Errorf("Appended object 4 mismatch: got %q, want %q", retrieved4, obj4)
			}

			// Verify count increased
			if heap2.Header.NumManagedObjects != 4 {
				t.Errorf("Heap has %d objects after append, want 4", heap2.Header.NumManagedObjects)
			}

			t.Logf("Successfully appended object to existing heap!")
			t.Logf("Heap now has %d objects", heap2.Header.NumManagedObjects)
		})
	})
}

// mockFileBuffer implements io.ReaderAt and Writer for testing.
// It simulates a file by maintaining written segments.
type mockFileBuffer struct {
	segments []mockSegment // Sorted list of written segments
}

type mockSegment struct {
	address uint64
	data    []byte
}

func newMockFileBuffer() *mockFileBuffer {
	return &mockFileBuffer{
		segments: make([]mockSegment, 0),
	}
}

func (m *mockFileBuffer) WriteAtAddress(data []byte, address uint64) error {
	// Copy data to avoid aliasing
	buf := make([]byte, len(data))
	copy(buf, data)

	// Add segment
	m.segments = append(m.segments, mockSegment{
		address: address,
		data:    buf,
	})

	return nil
}

func (m *mockFileBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	address := uint64(off)
	totalRead := 0

	// Read from segments that overlap with requested range
	for _, segment := range m.segments {
		segStart := segment.address
		segEnd := segment.address + uint64(len(segment.data))

		// Check if this segment overlaps with requested range
		if address < segEnd && address+uint64(len(p)-totalRead) > segStart {
			// Calculate overlap
			readStart := uint64(0)
			if address > segStart {
				readStart = address - segStart
			}

			writeStart := uint64(0)
			if segStart > address {
				writeStart = segStart - address
			}

			// Copy overlapping data
			nCopied := copy(p[writeStart:], segment.data[readStart:])
			totalRead += nCopied

			if totalRead >= len(p) {
				return totalRead, nil
			}
		}
	}

	if totalRead == 0 {
		return 0, io.EOF
	}

	if totalRead < len(p) {
		return totalRead, io.EOF
	}

	return totalRead, nil
}

// mockAllocator implements Allocator for testing.
type mockAllocator struct {
	nextAddr uint64
}

func (m *mockAllocator) Allocate(size uint64) (uint64, error) {
	addr := m.nextAddr
	m.nextAddr += size
	return addr, nil
}
