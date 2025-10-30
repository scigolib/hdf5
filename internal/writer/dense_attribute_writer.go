// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
	"encoding/binary"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
)

// DenseAttributeWriter manages dense attribute storage for a single object.
//
// Dense attributes (8+ attributes) use:
// - Fractal Heap: Storage for attribute data (name + type + space + value)
// - B-tree v2: Index for fast attribute lookup by name
// - Attribute Info Message: Metadata with heap/B-tree addresses
//
// This writer REUSES infrastructure from dense groups:
// - structures.WritableFractalHeap (already exists!)
// - structures.WritableBTreeV2 (already exists!)
//
// Reference: H5Adense.c - H5A__dense_create(), H5A__dense_insert().
type DenseAttributeWriter struct {
	objectAddr uint64 // Address of object header

	// Reuse existing components from dense groups!
	fractalHeap *structures.WritableFractalHeap
	btree       *structures.WritableBTreeV2
	attrInfo    *core.AttributeInfoMessage

	// Track attributes for duplicate detection
	attributes map[string]*core.Attribute // name → attribute
}

// NewDenseAttributeWriter creates new dense attribute writer.
//
// Parameters:
//   - objectAddr: Address of object header (for reference)
//
// Returns:
//   - DenseAttributeWriter ready to use
func NewDenseAttributeWriter(objectAddr uint64) *DenseAttributeWriter {
	return &DenseAttributeWriter{
		objectAddr:  objectAddr,
		fractalHeap: structures.NewWritableFractalHeap(64 * 1024), // 64KB heap for attributes
		btree:       structures.NewWritableBTreeV2(4096),          // 4KB B-tree node
		attrInfo: &core.AttributeInfoMessage{
			Version: 0,
			Flags:   0, // No creation order tracking for MVP
		},
		attributes: make(map[string]*core.Attribute),
	}
}

// AddAttribute adds an attribute to dense storage.
//
// Process:
// 1. Encode attribute (name + type + space + data)
// 2. Insert into fractal heap → get heap ID
// 3. Insert into B-tree v2 (name → heap ID)
//
// Parameters:
//   - attr: Attribute to add
//   - sb: Superblock for encoding
//
// Returns:
//   - error: Non-nil if add fails or duplicate name
//
// Reference: H5Adense.c - H5A__dense_insert().
func (daw *DenseAttributeWriter) AddAttribute(attr *core.Attribute, sb *core.Superblock) error {
	if attr == nil {
		return fmt.Errorf("attribute is nil")
	}

	if attr.Name == "" {
		return fmt.Errorf("attribute name cannot be empty")
	}

	// Check for duplicates
	if _, exists := daw.attributes[attr.Name]; exists {
		return fmt.Errorf("attribute %q already exists", attr.Name)
	}

	// 1. Encode attribute message (reuse existing function!)
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute: %w", err)
	}

	// 2. Insert into fractal heap (REUSE from dense groups!)
	heapIDBytes, err := daw.fractalHeap.InsertObject(attrMsg)
	if err != nil {
		return fmt.Errorf("failed to insert into heap: %w", err)
	}

	// Convert []byte heap ID to uint64 for B-tree
	// Heap IDs are 8 bytes (default)
	if len(heapIDBytes) != 8 {
		return fmt.Errorf("unexpected heap ID length: %d bytes", len(heapIDBytes))
	}
	heapID := binary.LittleEndian.Uint64(heapIDBytes)

	// 3. Insert into B-tree v2 (REUSE from dense groups!)
	// For attributes, we use attribute name directly (not link name)
	err = daw.btree.InsertRecord(attr.Name, heapID)
	if err != nil {
		return fmt.Errorf("failed to insert into B-tree: %w", err)
	}

	// Track for duplicate detection
	daw.attributes[attr.Name] = attr

	return nil
}

// WriteToFile writes dense attribute storage to file.
//
// Process:
// 1. Write fractal heap → get heap address
// 2. Write B-tree v2 → get B-tree address
// 3. Create Attribute Info Message with addresses
// 4. Return Attribute Info Message (caller adds to object header)
//
// Parameters:
//   - fw: FileWriter for write operations
//   - allocator: Space allocator (pointer to match existing infrastructure)
//   - sb: Superblock
//
// Returns:
//   - *core.AttributeInfoMessage: Message to add to object header
//   - error: Non-nil if write fails
//
// Reference: H5Adense.c - H5A__dense_create().
func (daw *DenseAttributeWriter) WriteToFile(fw *FileWriter, allocator *Allocator, sb *core.Superblock) (*core.AttributeInfoMessage, error) {
	if len(daw.attributes) == 0 {
		return nil, fmt.Errorf("no attributes to write")
	}

	// 1. Write fractal heap (REUSE!)
	heapAddr, err := daw.fractalHeap.WriteToFile(fw, allocator, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to write fractal heap: %w", err)
	}

	// 2. Write B-tree v2 (REUSE!)
	btreeAddr, err := daw.btree.WriteToFile(fw, allocator, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to write B-tree: %w", err)
	}

	// 3. Create Attribute Info Message
	daw.attrInfo.FractalHeapAddr = heapAddr
	daw.attrInfo.BTreeNameIndexAddr = btreeAddr
	// MaxCreationIndex and BTreeOrderIndexAddr remain 0 (no creation order)

	return daw.attrInfo, nil
}
