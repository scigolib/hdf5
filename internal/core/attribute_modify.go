// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package core

import (
	"encoding/binary"
	"fmt"
)

// ModifyDenseAttribute modifies an existing dense attribute.
//
// This modifies attributes stored in dense storage (fractal heap + B-tree v2).
//
// Algorithm (matching H5Adense.c:H5A__dense_write):
//  1. Search B-tree v2 for attribute name → get heap ID
//  2. Read old attribute from fractal heap
//  3. Encode new attribute value
//  4. Check sizes:
//     a. Same size → Overwrite in heap (in-place, fast path)
//     b. Different size → Delete old, insert new, update B-tree
//  5. Write updated heap and B-tree back to file
//
// Parameters:
//   - heap: Writable fractal heap (loaded from file)
//   - btree: Writable B-tree v2 (loaded from file)
//   - name: Attribute name to modify
//   - newAttr: New attribute structure with updated data
//
// Returns:
//   - error: Non-nil if modification fails
//
// Reference: H5Adense.c - H5A__dense_write().
func ModifyDenseAttribute(heap HeapWriter, btree BTreeWriter, name string, newAttr *Attribute) error {
	if heap == nil || btree == nil {
		return fmt.Errorf("heap or btree is nil")
	}
	if name == "" {
		return fmt.Errorf("attribute name cannot be empty")
	}
	if newAttr == nil {
		return fmt.Errorf("new attribute is nil")
	}

	// 1. Search B-tree for attribute name
	heapID, found := btree.SearchRecord(name)
	if !found {
		return fmt.Errorf("attribute %q not found in dense storage", name)
	}

	// 2. Read old attribute from heap
	oldAttrData, err := heap.GetObject(heapID)
	if err != nil {
		return fmt.Errorf("failed to read old attribute from heap: %w", err)
	}

	// 3. Encode new attribute
	// Note: We need the superblock for encoding - this is passed via EncodeAttributeFromStruct
	// For now, assume newAttr is already fully encoded in Data field
	// In practice, the caller (attribute_write.go) will encode it
	newAttrData := newAttr.Data
	if len(newAttrData) == 0 {
		return fmt.Errorf("new attribute data is empty (caller must encode)")
	}

	// 4. Check sizes and modify
	if len(newAttrData) == len(oldAttrData) { //nolint:nestif // Clear size-based logic
		// Same size → Overwrite in-place (fast path)
		err = heap.OverwriteObject(heapID, newAttrData)
		if err != nil {
			return fmt.Errorf("failed to overwrite heap object: %w", err)
		}
		// B-tree unchanged (same heap ID)
	} else {
		// Different size → Delete old, insert new, update B-tree

		// 4a. Delete old heap object
		err = heap.DeleteObject(heapID)
		if err != nil {
			return fmt.Errorf("failed to delete old heap object: %w", err)
		}

		// 4b. Insert new attribute → get new heap ID
		newHeapIDBytes, err := heap.InsertObject(newAttrData)
		if err != nil {
			return fmt.Errorf("failed to insert new attribute: %w", err)
		}

		// Convert heap ID bytes to uint64
		if len(newHeapIDBytes) != 8 {
			return fmt.Errorf("unexpected heap ID length: %d bytes", len(newHeapIDBytes))
		}
		newHeapID := binary.LittleEndian.Uint64(newHeapIDBytes)

		// 4c. Update B-tree record with new heap ID
		err = btree.UpdateRecord(name, newHeapID)
		if err != nil {
			return fmt.Errorf("failed to update B-tree record: %w", err)
		}
	}

	// Note: Heap and B-tree are written back to file by caller (attribute_write.go)
	// using WriteAt() methods. This function only modifies in-memory structures.

	return nil
}

// DeleteDenseAttribute deletes an attribute from dense storage.
//
// This removes attributes stored in dense storage (fractal heap + B-tree v2).
//
// Algorithm (matching H5Adense.c:H5A__dense_remove):
// 1. Search B-tree v2 for attribute name → get heap ID
// 2. Delete record from B-tree (with optional rebalancing)
// 3. Delete object from fractal heap
// 4. Update Attribute Info message (decrement count)
//
// Parameters:
//   - heap: Writable fractal heap (loaded from file)
//   - btree: Writable B-tree v2 (loaded from file)
//   - name: Attribute name to delete
//
// Returns:
//   - error: Non-nil if deletion fails
//
// Reference: H5Adense.c - H5A__dense_remove(), H5Adelete.c - H5A__delete().
func DeleteDenseAttribute(heap HeapWriter, btree BTreeWriter, name string) error {
	if heap == nil || btree == nil {
		return fmt.Errorf("heap or btree is nil")
	}
	if name == "" {
		return fmt.Errorf("attribute name cannot be empty")
	}

	// 1. Search B-tree for attribute name
	heapID, found := btree.SearchRecord(name)
	if !found {
		return fmt.Errorf("attribute %q not found in dense storage", name)
	}

	// 2. Delete record from B-tree
	if err := btree.DeleteRecord(name); err != nil {
		return fmt.Errorf("failed to delete B-tree record: %w", err)
	}

	// 3. Delete object from fractal heap
	if err := heap.DeleteObject(heapID); err != nil {
		return fmt.Errorf("failed to delete heap object: %w", err)
	}

	// Note: Attribute Info message count update is handled by caller
	// (attribute_write.go), as it requires object header access.

	return nil
}

// HeapWriter interface for dense attribute modification.
// This abstracts fractal heap operations for testing and modularity.
type HeapWriter interface {
	GetObject(heapID []byte) ([]byte, error)
	OverwriteObject(heapID []byte, newData []byte) error
	DeleteObject(heapID []byte) error
	InsertObject(data []byte) ([]byte, error)
}

// BTreeWriter interface for dense attribute modification.
// This abstracts B-tree v2 operations for testing and modularity.
type BTreeWriter interface {
	SearchRecord(name string) ([]byte, bool)
	UpdateRecord(name string, newHeapID uint64) error
	DeleteRecord(name string) error
}
