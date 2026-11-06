// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
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

	// corderIndex is the creation order index (B-tree type 9) of an object
	// that indexes attribute creation order; nil otherwise.
	corderIndex *structures.WritableBTreeV2

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
		objectAddr: objectAddr,
		// The C attribute heap parameters (H5Oprivate.h:57-64).
		fractalHeap: structures.NewAttributeFractalHeap(),
		// The attribute name index (B-tree type 8, H5Adense.c:211-225). C
		// uses 512-byte nodes (H5A_NAME_BT2_NODE_SIZE); this writer keeps a
		// single leaf, so a 4 KiB node holds 240 records instead of 29. The
		// node size is in the header, which C reads.
		btree: structures.NewAttributeNameIndex(4096),
		attrInfo: &core.AttributeInfoMessage{
			Version: 0,
			Flags:   0, // no creation order tracking unless TrackCreationOrder
		},
		attributes: make(map[string]*core.Attribute),
	}
}

// Attribute info message flags (H5Oainfo.c:96-103) and HADDR_UNDEF.
const (
	ainfoTrackCorder = 0x01 // H5O_AINFO_TRACK_CORDER
	ainfoIndexCorder = 0x02 // H5O_AINFO_INDEX_CORDER
	undefinedAddr    = ^uint64(0)
)

// TrackCreationOrder makes the storage that of an object that tracks
// attribute creation order, and indexes it when indexed: the attribute info
// message gets the flags and maxCreationIndex, the next creation index, and
// an indexed object gets a creation order index next to the name index, as
// H5A__dense_create builds it (H5Adense.c:227-241). Call it before adding
// attributes with AddAttributeWithCreationIndex.
func (daw *DenseAttributeWriter) TrackCreationOrder(indexed bool, maxCreationIndex uint64) {
	daw.attrInfo.Flags = ainfoTrackCorder
	daw.attrInfo.MaxCreationIndex = maxCreationIndex
	if indexed {
		daw.attrInfo.Flags |= ainfoIndexCorder
		// The node size of the name index, for the same reason.
		daw.corderIndex = structures.NewAttributeCreationOrderIndex(4096)
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
	return daw.addAttribute(attr, sb, structures.CreationOrderUntracked)
}

// AddAttributeWithCreationIndex adds an attribute of an object that tracks
// creation order (TrackCreationOrder): its name record carries crtIdx, and
// an indexed object also gets a creation order record (H5Adense.c:505-530).
func (daw *DenseAttributeWriter) AddAttributeWithCreationIndex(attr *core.Attribute, sb *core.Superblock, crtIdx uint16) error {
	if daw.attrInfo.Flags&ainfoTrackCorder == 0 {
		return fmt.Errorf("attribute %q: creation index on storage that does not track creation order", attrName(attr))
	}
	return daw.addAttribute(attr, sb, uint32(crtIdx))
}

// attrName is the name of attr, "" for nil.
func attrName(attr *core.Attribute) string {
	if attr == nil {
		return ""
	}
	return attr.Name
}

// addAttribute inserts attr with the creation index corder of its records.
func (daw *DenseAttributeWriter) addAttribute(attr *core.Attribute, sb *core.Superblock, corder uint32) error {
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

	// 2. Insert into fractal heap. The direct block prefix holds a file
	// address, so the layout follows the superblock's address size (a no-op
	// when it already matches; refused once objects are laid out).
	if err := daw.fractalHeap.SetAddressSize(sb.OffsetSize); err != nil {
		return fmt.Errorf("dense attribute heap: %w", err)
	}
	heapIDBytes, err := daw.fractalHeap.InsertObject(attrMsg)
	if err != nil {
		return fmt.Errorf("failed to insert into heap: %w", err)
	}

	// Heap IDs are 8 bytes (default)
	if len(heapIDBytes) != 8 {
		return fmt.Errorf("unexpected heap ID length: %d bytes", len(heapIDBytes))
	}
	heapID := [8]byte(heapIDBytes)

	// 3. Insert into the name index: flags 0 (not shared) and the creation
	// index, 65535 for an object that does not track creation order
	// (H5Adense.c:508-513; H5Oattribute.c:307), then into the creation
	// order index when there is one (H5Adense.c:516-530).
	if err := daw.btree.InsertNameRecord(attr.Name, heapID, 0, corder); err != nil {
		return fmt.Errorf("failed to insert into B-tree: %w", err)
	}
	if daw.corderIndex != nil {
		if err := daw.corderIndex.InsertCreationOrderRecord(heapID, 0, corder); err != nil {
			return fmt.Errorf("failed to insert into creation order index: %w", err)
		}
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
	daw.attrInfo.BTreeOrderIndexAddr = undefinedAddr
	if daw.corderIndex != nil {
		orderAddr, err := daw.corderIndex.WriteToFile(fw, allocator, sb)
		if err != nil {
			return nil, fmt.Errorf("failed to write creation order index: %w", err)
		}
		daw.attrInfo.BTreeOrderIndexAddr = orderAddr
	}

	return daw.attrInfo, nil
}
