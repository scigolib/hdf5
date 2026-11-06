// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
)

// DenseGroupWriter manages dense group creation.
//
// Dense groups (HDF5 1.8+) use:
//   - Link Info Message: Metadata about link storage
//   - Fractal Heap: Storage for link names and messages
//   - B-tree v2: Index for fast link lookup by name
//
// This coordinator:
//  1. Creates Fractal Heap for link storage
//  2. Creates B-tree v2 for link indexing
//  3. Stores link names and metadata in heap
//  4. Indexes links in B-tree
//  5. Builds Link Info Message with addresses
//  6. Constructs object header with all messages
//
// Reference: H5Gdense.c - H5G_dense_create(), H5G_dense_insert().
type DenseGroupWriter struct {
	name string

	// Components
	fractalHeap *structures.WritableFractalHeap
	btree       *structures.WritableBTreeV2
	linkInfo    *core.LinkInfoMessage

	// Links to add
	links []denseLink
}

// denseLink represents a link to be added to dense group.
type denseLink struct {
	name       string
	targetAddr uint64 // For hard links
	// Future: soft link path, external link file+path
}

// NewDenseGroupWriter creates new dense group writer.
//
// Parameters:
//   - name: Group name (for error messages)
//
// Returns:
//   - DenseGroupWriter ready to accept links
//
// Reference: H5Gdense.c - H5G_dense_create().
func NewDenseGroupWriter(name string) *DenseGroupWriter {
	return &DenseGroupWriter{
		name: name,
		// The C link heap parameters (H5Gdense.c:43-49); its heap IDs are
		// 7 bytes (H5G_DENSE_FHEAP_ID_LEN, H5Gpkg.h:40).
		fractalHeap: structures.NewLinkFractalHeap(),
		btree:       structures.NewWritableBTreeV2(4096), // 4KB node
		linkInfo: &core.LinkInfoMessage{
			Version: 0,
			Flags:   0, // No creation order tracking for MVP
		},
		links: make([]denseLink, 0),
	}
}

// AddLink adds hard link to dense group.
//
// For MVP: Only hard links supported (targetAddr points to object header)
// Future: Soft links, external links
//
// Parameters:
//   - name: Link name (UTF-8 string)
//   - targetAddr: File address of target object header
//
// Returns:
//   - error if name empty, duplicate, or invalid
//
// Reference: H5Gdense.c - H5G_dense_insert().
func (dgw *DenseGroupWriter) AddLink(name string, targetAddr uint64) error {
	if name == "" {
		return errors.New("link name cannot be empty")
	}

	// Check for duplicates
	for _, link := range dgw.links {
		if link.name == name {
			return fmt.Errorf("duplicate link name: %s", name)
		}
	}

	dgw.links = append(dgw.links, denseLink{
		name:       name,
		targetAddr: targetAddr,
	})

	return nil
}

// WriteToFile writes dense group to file, returns object header address.
//
// This method:
//  1. For each link:
//     a. Create link message (hard link format)
//     b. Insert link message into fractal heap
//     c. Insert (name, heapID) into B-tree v2
//  2. Write fractal heap to file
//  3. Write B-tree v2 to file
//  4. Create Link Info Message with heap/B-tree addresses
//  5. Create object header with Link Info + other messages
//  6. Write object header to file
//
// Parameters:
//   - fw: FileWriter for write operations
//   - allocator: Space allocator
//   - sb: Superblock for encoding parameters
//
// Returns:
//   - uint64: File address of group's object header
//   - error: Non-nil if write fails
//
// Reference: H5Gdense.c - H5G_dense_create() + H5G_dense_insert().
func (dgw *DenseGroupWriter) WriteToFile(fw *FileWriter, allocator *Allocator, sb *core.Superblock) (uint64, error) {
	if len(dgw.links) == 0 {
		return 0, errors.New("dense group must have at least one link")
	}

	// The direct block prefix holds a file address, so the heap layout
	// depends on the superblock's address size.
	if err := dgw.fractalHeap.SetAddressSize(sb.OffsetSize); err != nil {
		return 0, fmt.Errorf("dense group heap: %w", err)
	}

	// Step 1: Process all links
	for _, link := range dgw.links {
		// 1a. Create link message (hard link format)
		linkMsg, err := dgw.createLinkMessage(link, sb)
		if err != nil {
			return 0, err
		}

		// 1b. Insert into fractal heap
		heapID, err := dgw.fractalHeap.InsertObject(linkMsg)
		if err != nil {
			return 0, fmt.Errorf("failed to insert link %s into heap: %w", link.name, err)
		}

		// 1c. Convert heap ID to uint64 for B-tree. A link heap ID is 7
		// bytes, little-endian; the type 5 record stores the same 7 bytes.
		if len(heapID) == 0 || len(heapID) > 8 {
			return 0, fmt.Errorf("invalid heap ID length for link %s: %d bytes", link.name, len(heapID))
		}
		var idBuf [8]byte
		copy(idBuf[:], heapID)
		heapIDUint64 := binary.LittleEndian.Uint64(idBuf[:])

		// 1d. Insert into B-tree v2
		err = dgw.btree.InsertRecord(link.name, heapIDUint64)
		if err != nil {
			return 0, fmt.Errorf("failed to insert link %s into B-tree: %w", link.name, err)
		}
	}

	// Step 2: Write fractal heap
	heapAddr, err := dgw.fractalHeap.WriteToFile(fw, allocator, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to write fractal heap: %w", err)
	}

	// Step 3: Write B-tree v2
	btreeAddr, err := dgw.btree.WriteToFile(fw, allocator, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to write B-tree v2: %w", err)
	}

	// Step 4: Create Link Info Message
	dgw.linkInfo.FractalHeapAddress = heapAddr
	dgw.linkInfo.NameBTreeAddress = btreeAddr
	dgw.linkInfo.CreationOrderBTreeAddress = 0 // No creation order tracking in MVP

	// Step 5: Create object header with Link Info Message
	ohAddr, err := dgw.createObjectHeader(fw, allocator, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to create object header: %w", err)
	}

	return ohAddr, nil
}

// createLinkMessage encodes the link message of a hard link as
// H5O__link_encode writes it into the dense link heap (H5Olink.c:282-380,
// H5Gdense.c H5G__dense_insert): version 1, flags with the narrowest name
// length field (bits 0-1) and no optional fields (the link type, creation
// order and character set take their defaults), the name length, the name,
// then the target object header address (H5F_addr_encode, little-endian).
//
// It replaces an encoding with a link type byte, flag 0x04 and a character
// set byte in fixed positions, which the C library decoded as a creation
// order flag and garbage, so h5ls and h5dump saw none of the links.
func (dgw *DenseGroupWriter) createLinkMessage(link denseLink, sb *core.Superblock) ([]byte, error) {
	lm := &core.LinkMessage{Version: 1, Type: core.LinkTypeHard, Name: link.name}
	switch n := uint64(len(link.name)); {
	case n <= math.MaxUint8:
		// Flags bits 0-1 stay 0: one-byte length.
	case n <= math.MaxUint16:
		lm.Flags |= 1
	case n <= math.MaxUint32:
		lm.Flags |= 2
	default:
		lm.Flags |= 3
	}
	lm.LinkValue = make([]byte, sb.OffsetSize)
	writeUint64(lm.LinkValue, link.targetAddr, int(sb.OffsetSize), binary.LittleEndian)
	msg, err := core.EncodeLinkMessage(lm, sb)
	if err != nil {
		return nil, fmt.Errorf("encode link message %q: %w", link.name, err)
	}
	return msg, nil
}

// createObjectHeader creates object header with Link Info Message.
//
// Messages to include:
//   - Link Info Message (type 0x0002)
//   - Dataspace Message (type 0x0001) - scalar for groups
//   - Datatype Message (type 0x0003) - opaque for groups (optional, skipped in MVP)
//
// Reference: H5Oobj.c - H5O_obj_create().
func (dgw *DenseGroupWriter) createObjectHeader(fw *FileWriter, allocator *Allocator, sb *core.Superblock) (uint64, error) {
	// Encode Link Info Message
	linkInfoData, err := core.EncodeLinkInfoMessage(dgw.linkInfo, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to encode link info message: %w", err)
	}

	// Create dataspace message (scalar for groups)
	dataspaceMsg := createScalarDataspaceMessage()

	// Create object header with messages
	ohw := &core.ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgLinkInfo, Data: linkInfoData},
			{Type: core.MsgDataspace, Data: dataspaceMsg},
		},
	}

	// Calculate object header size using the writer's own method
	headerSize := ohw.Size()

	// Allocate space for object header
	headerAddr, err := allocator.Allocate(headerSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate object header: %w", err)
	}

	// Write object header
	writtenSize, err := ohw.WriteTo(fw, headerAddr)
	if err != nil {
		return 0, fmt.Errorf("failed to write object header: %w", err)
	}

	if writtenSize != headerSize {
		return 0, fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	return headerAddr, nil
}

// createScalarDataspaceMessage creates a dataspace message for scalar dataspace.
//
// Groups have scalar dataspace (0 dimensions).
//
// Format:
//   - Version: 1 (1 byte) or 2
//   - Dimensionality: 0 (1 byte)
//   - Flags: 0 (1 byte)
//   - Type: 0 = scalar (1 byte) - optional in v1
//
// Reference: H5Odtype.c - H5O__dspace_encode().
func createScalarDataspaceMessage() []byte {
	// Use version 1 for simplicity
	// Version 1: 1 (ver) + 1 (dims) + 1 (flags) + 5 (reserved) = 8 bytes
	buf := make([]byte, 8)

	buf[0] = 1 // Version 1
	buf[1] = 0 // Dimensionality = 0 (scalar)
	buf[2] = 0 // Flags = 0
	// Bytes 3-7: reserved (zeros)

	return buf
}

// compactUint64Size returns the number of bytes needed to encode a uint64 in compact form.
//
// Compact encoding uses 1-8 bytes based on the value:
//   - Values 0-255: 1 byte
//   - Values 256-65535: 2 bytes
//   - etc.
func compactUint64Size(value uint64) int {
	if value == 0 {
		return 1
	}

	size := 0
	for value > 0 {
		size++
		value >>= 8
	}

	return size
}

// encodeCompactUint64 encodes a uint64 value in compact form (little-endian).
//
// The size is determined by compactUint64Size().
func encodeCompactUint64(buf []byte, value uint64) {
	size := compactUint64Size(value)
	for i := 0; i < size; i++ {
		buf[i] = byte(value >> (8 * i)) //nolint:gosec // G115: variable-size encoding
	}
}

// writeUint64 writes a uint64 value to buffer with specified size and endianness.
// This is a helper function for encoding fields with variable sizes.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value) //nolint:gosec // G115: variable-size encoding
	case 2:
		endianness.PutUint16(buf, uint16(value)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(value)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, value)
	}
}
