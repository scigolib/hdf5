// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
	"encoding/binary"
	"errors"
	"fmt"

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
		name:        name,
		fractalHeap: structures.NewWritableFractalHeap(512 * 1024), // 512KB default
		btree:       structures.NewWritableBTreeV2(4096),           // 4KB node
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

	// Step 1: Process all links
	for _, link := range dgw.links {
		// 1a. Create link message (hard link format)
		linkMsg := dgw.createLinkMessage(link, sb)

		// 1b. Insert into fractal heap
		heapID, err := dgw.fractalHeap.InsertObject(linkMsg)
		if err != nil {
			return 0, fmt.Errorf("failed to insert link %s into heap: %w", link.name, err)
		}

		// 1c. Convert heap ID to uint64 for B-tree
		// Heap ID is 8 bytes, read as little-endian uint64
		var heapIDUint64 uint64
		if len(heapID) >= 8 {
			heapIDUint64 = binary.LittleEndian.Uint64(heapID[:8])
		} else {
			return 0, fmt.Errorf("invalid heap ID length for link %s: %d bytes", link.name, len(heapID))
		}

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

// createLinkMessage creates link message for fractal heap storage.
//
// Format (from H5Olinfo.c - link message):
//   - Version: 1 (1 byte)
//   - Type: 0 = Hard Link (1 byte)
//   - Creation Order Present: 0 (1 byte, bit flags) - MVP: no creation order
//   - Link Name Encoding: 0 = ASCII/UTF-8 (1 byte)
//   - Link Name Length: variable (compact uint64 encoding)
//   - Link Name: UTF-8 bytes
//   - Link Info:
//   - For hard link: target object header address (offsetSize bytes)
//
// Reference: H5Ollink.c - H5O__link_encode().
func (dgw *DenseGroupWriter) createLinkMessage(link denseLink, sb *core.Superblock) []byte {
	nameBytes := []byte(link.name)
	nameLen := uint64(len(nameBytes))

	// Calculate message size
	// Version (1) + Type (1) + Flags (1) + Encoding (1) + Name Length (variable) + Name + Address
	// For MVP: name length encoded as compact uint64 (1-8 bytes based on value)
	nameLenSize := compactUint64Size(nameLen)
	messageSize := 4 + nameLenSize + len(nameBytes) + int(sb.OffsetSize)

	buf := make([]byte, messageSize)
	offset := 0

	// Version (1 byte)
	buf[offset] = 1 // Link message version 1
	offset++

	// Type (1 byte): 0 = Hard Link
	buf[offset] = 0
	offset++

	// Flags (1 byte)
	// Bit 0: creation order present (0 = no)
	// Bit 1: link type field present (0 = no, type is in separate field)
	// Bit 2: link name character set field present (1 = yes)
	// Bit 3: link name is stored as a creation order (0 = no)
	// For MVP: only bit 2 set (character set field present)
	buf[offset] = 0x04 // Character set field present
	offset++

	// Link Name Character Set Encoding (1 byte)
	buf[offset] = 0 // ASCII/UTF-8
	offset++

	// Link Name Length (compact uint64)
	encodeCompactUint64(buf[offset:], nameLen)
	offset += nameLenSize

	// Link Name (UTF-8 bytes)
	copy(buf[offset:], nameBytes)
	offset += len(nameBytes)

	// Link Info: For hard link, this is the target object header address
	writeUint64(buf[offset:], link.targetAddr, int(sb.OffsetSize), sb.Endianness)

	return buf
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

	// Calculate object header size
	// Header: 4 (sig) + 1 (ver) + 1 (flags) + 1 (chunk size) = 7 bytes (v2 header)
	// Each message: 1 (type) + 2 (size) + 1 (flags) + len(data)
	var messageSize uint64
	for _, msg := range ohw.Messages {
		//nolint:gosec // G115: message size calculation, safe for HDF5 messages
		messageSize += uint64(1 + 2 + 1 + len(msg.Data)) // type + size + flags + data
	}

	headerSize := 7 + messageSize // 7-byte header + messages

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
		buf[i] = byte(value >> (8 * i))
	}
}

// writeUint64 writes a uint64 value to buffer with specified size and endianness.
// This is a helper function for encoding fields with variable sizes.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value)
	case 2:
		endianness.PutUint16(buf, uint16(value)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(value)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, value)
	}
}
