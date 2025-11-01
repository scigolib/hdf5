// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package core

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ModifyCompactAttribute modifies an existing compact attribute in an object header.
//
// This function implements the attribute modification logic matching the C reference:
// - H5Oattribute.c:H5O__attr_write_cb() - Compact attribute modification
// - H5Aint.c:H5A__write() - Main write function
//
// Algorithm:
// 1. Find existing attribute message by name
// 2. Encode new attribute value
// 3. Compare sizes:
//    a. Same size → Overwrite in-place (modify message data)
//    b. Different size → Mark old as deleted, add new message
// 4. Update object header checksum
// 5. Write back to file
//
// Parameters:
//   - writer: Writer for file I/O
//   - objectAddr: Address of object header
//   - name: Attribute name to modify
//   - newAttr: New attribute structure with updated data
//   - sb: Superblock for endianness
//
// Returns:
//   - error: Non-nil if modification fails
//
// Reference: H5Oattribute.c - H5O__attr_write_cb().
func ModifyCompactAttribute(writer io.WriterAt, objectAddr uint64, name string, newAttr *Attribute, sb *Superblock) error {
	if writer == nil {
		return fmt.Errorf("writer is nil")
	}
	if newAttr == nil {
		return fmt.Errorf("new attribute is nil")
	}
	if name == "" {
		return fmt.Errorf("attribute name cannot be empty")
	}

	// 1. Read object header
	readerAt, ok := writer.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("writer does not implement ReaderAt interface")
	}

	oh, err := ReadObjectHeader(readerAt, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// 2. Find existing attribute message
	msgIndex := -1
	var existingAttr *Attribute
	for i, msg := range oh.Messages {
		if msg.Type == MsgAttribute {
			attr, parseErr := ParseAttributeMessage(msg.Data, sb.Endianness)
			if parseErr != nil {
				continue // Skip malformed attributes
			}
			if attr.Name == name {
				msgIndex = i
				existingAttr = attr
				break
			}
		}
	}

	if msgIndex == -1 {
		return fmt.Errorf("attribute %q not found", name)
	}

	// 3. Encode new attribute message
	newAttrData, err := EncodeAttributeFromStruct(newAttr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode new attribute: %w", err)
	}

	// 4. Compare sizes and decide strategy
	oldSize := len(oh.Messages[msgIndex].Data)
	newSize := len(newAttrData)

	if oldSize == newSize {
		// Case A: Same size → Overwrite in-place
		// This is the fast path - just replace message data
		oh.Messages[msgIndex].Data = newAttrData
	} else {
		// Case B: Different size → Mark old as deleted, add new
		// This follows HDF5 C library approach (H5Oattribute.c)
		// Note: We don't compact the header in MVP (defer to future optimization)

		// Mark old message as deleted (not implemented yet in header structure)
		// For MVP: Just replace with new attribute by removing old and appending new
		// This is simpler than implementing message deletion flags

		// Remove old attribute message
		oh.Messages = append(oh.Messages[:msgIndex], oh.Messages[msgIndex+1:]...)

		// Add new attribute message
		oh.Messages = append(oh.Messages, &HeaderMessage{
			Type: MsgAttribute,
			Data: newAttrData,
		})
	}

	// 5. Write back object header
	// For MVP: We don't have a proper object header write-back function yet
	// This is a limitation - we need to implement object header modification
	// For now, return an error indicating this is not yet supported

	// TODO: Implement object header write-back
	// This requires:
	// 1. Re-encoding the entire object header (v1 or v2 format)
	// 2. Updating checksums
	// 3. Writing to file at objectAddr
	//
	// Reference: H5O.c - H5O__msg_write(), H5O_touch_oh()

	_ = existingAttr // Mark as used for future logic

	return fmt.Errorf("object header write-back not yet implemented (MVP limitation)")
}

// FindCompactAttribute searches for an attribute by name in compact storage.
//
// This is a helper function that finds an attribute message in an object header
// without modifying it. Useful for checking existence before modification.
//
// Parameters:
//   - oh: Object header to search
//   - name: Attribute name
//   - sb: Superblock for endianness
//
// Returns:
//   - *Attribute: Found attribute, or nil if not found
//   - int: Message index, or -1 if not found
//   - error: Non-nil if parsing fails
//
// Reference: H5Oattribute.c - attribute iteration callbacks.
func FindCompactAttribute(oh *ObjectHeader, name string, endianness binary.ByteOrder) (*Attribute, int, error) {
	for i, msg := range oh.Messages {
		if msg.Type == MsgAttribute {
			attr, err := ParseAttributeMessage(msg.Data, endianness)
			if err != nil {
				continue // Skip malformed attributes
			}
			if attr.Name == name {
				return attr, i, nil
			}
		}
	}

	return nil, -1, nil // Not found (not an error)
}

// DeleteCompactAttribute deletes an attribute from compact storage.
//
// This implements attribute deletion for compact storage (object header messages).
// Following HDF5 C library approach:
// - H5Adelete.c:H5A__delete() - Attribute deletion
// - H5O.c:H5O_msg_remove() - Object header message removal
//
// Algorithm:
// 1. Find attribute message by name
// 2. Mark message as deleted (or remove from list for MVP)
// 3. Update object header checksum
// 4. Write back to file
//
// MVP Limitation: We remove the message entirely instead of marking as deleted.
// This avoids implementing message deletion flags and header compaction.
//
// Parameters:
//   - writer: Writer for file I/O
//   - objectAddr: Address of object header
//   - name: Attribute name to delete
//   - sb: Superblock for endianness
//
// Returns:
//   - error: Non-nil if deletion fails
//
// Reference: H5Adelete.c - H5A__delete().
func DeleteCompactAttribute(writer io.WriterAt, objectAddr uint64, name string, sb *Superblock) error {
	if writer == nil {
		return fmt.Errorf("writer is nil")
	}
	if name == "" {
		return fmt.Errorf("attribute name cannot be empty")
	}

	// 1. Read object header
	readerAt, ok := writer.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("writer does not implement ReaderAt interface")
	}

	oh, err := ReadObjectHeader(readerAt, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// 2. Find attribute message
	msgIndex := -1
	for i, msg := range oh.Messages {
		if msg.Type == MsgAttribute {
			attr, parseErr := ParseAttributeMessage(msg.Data, sb.Endianness)
			if parseErr != nil {
				continue
			}
			if attr.Name == name {
				msgIndex = i
				break
			}
		}
	}

	if msgIndex == -1 {
		return fmt.Errorf("attribute %q not found", name)
	}

	// 3. Remove message (MVP: direct removal, not marking as deleted)
	oh.Messages = append(oh.Messages[:msgIndex], oh.Messages[msgIndex+1:]...)

	// 4. Write back object header
	// TODO: Implement object header write-back (same as ModifyCompactAttribute)
	return fmt.Errorf("object header write-back not yet implemented (MVP limitation)")
}
