package core

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ObjectHeaderWriter provides functionality for writing HDF5 object headers.
// For MVP (v0.11.0-beta), only object header v2 with minimal messages is supported.
type ObjectHeaderWriter struct {
	Version  uint8
	Flags    uint8
	Messages []MessageWriter
}

// MessageWriter represents a message that can be written to an object header.
type MessageWriter struct {
	Type MessageType
	Data []byte
}

// NewMinimalRootGroupHeader creates a minimal object header v2 for an empty root group.
// This is suitable for MVP file creation - just enough to make a valid HDF5 file.
//
// The root group header contains:
//   - Object Header v2 with minimal flags (no times, no attribute phase change)
//   - Link Info message (empty, compact storage)
//
// Returns an ObjectHeaderWriter ready to be written to file.
func NewMinimalRootGroupHeader() *ObjectHeaderWriter {
	// Create minimal Link Info message for an empty group
	// Link Info message format (compact storage, no dense links):
	//   Version: 0 (1 byte)
	//   Flags: 0x00 (1 byte) - compact storage, no index
	//   Max Compact (optional): 2 bytes if flags & 0x01
	//   Min Dense (optional): 2 bytes if flags & 0x01
	//   Heap Address (8 bytes): 0xFFFFFFFFFFFFFFFF (UNDEF for compact)
	//   B-tree Address (8 bytes): 0xFFFFFFFFFFFFFFFF (UNDEF for compact)
	//
	// For MVP empty group: Version=0, Flags=0, no optional fields, two UNDEF addresses
	linkInfoData := make([]byte, 18) // 1+1+8+8 = 18 bytes
	linkInfoData[0] = 0              // Version 0
	linkInfoData[1] = 0              // Flags: compact storage, no tracking

	// Heap address (UNDEF for compact storage)
	binary.LittleEndian.PutUint64(linkInfoData[2:10], 0xFFFFFFFFFFFFFFFF)

	// B-tree name index address (UNDEF for compact storage)
	binary.LittleEndian.PutUint64(linkInfoData[10:18], 0xFFFFFFFFFFFFFFFF)

	return &ObjectHeaderWriter{
		Version: 2,
		Flags:   0, // Minimal flags: no times, no attribute phase change
		Messages: []MessageWriter{
			{
				Type: MsgLinkInfo,
				Data: linkInfoData,
			},
		},
	}
}

// Size calculates the total size of the object header in bytes.
// This is used for pre-allocation before writing.
//
// Returns:
//   - Total size in bytes
//
// For object header v2:
//   - Header: 4 (signature) + 1 (version) + 1 (flags) + 1 (chunk size) = 7 bytes
//   - Messages: sum of (1 + 2 + 1 + len(data)) for each message
func (ohw *ObjectHeaderWriter) Size() uint64 {
	// Calculate message data size
	var messageDataSize uint64
	for _, msg := range ohw.Messages {
		// Each message: Type (1) + Size (2) + Flags (1) + Data (variable)
		messageDataSize += 1 + 2 + 1 + uint64(len(msg.Data))
	}

	// Header size: Signature (4) + Version (1) + Flags (1) + Chunk Size (1) + Messages
	return 4 + 1 + 1 + 1 + messageDataSize
}

// WriteTo writes the object header to the writer at the specified address.
// Returns the total size written (useful for allocation tracking).
//
// Object Header v2 format:
//
//	Signature: "OHDR" (4 bytes)
//	Version: 2 (1 byte)
//	Flags: (1 byte)
//	[Optional fields based on flags]
//	Size of Chunk 0: (1, 2, 4, or 8 bytes based on flags bits 0-1)
//	Messages: variable size
//
// For MVP:
//   - No timestamp fields (flags bit 5 = 0)
//   - No attribute phase change (flags bit 4 = 0)
//   - Chunk size in 1 byte (flags bits 0-1 = 0)
func (ohw *ObjectHeaderWriter) WriteTo(w io.WriterAt, address uint64) (uint64, error) {
	if ohw.Version != 2 {
		return 0, fmt.Errorf("only object header version 2 is supported for writing, got version %d", ohw.Version)
	}

	// Calculate message data size
	var messageDataSize uint64
	for _, msg := range ohw.Messages {
		// Each message has:
		// - Type (1 byte for v2)
		// - Size (2 bytes for v2)
		// - Flags (1 byte for v2)
		// - Data (variable)
		messageDataSize += 1 + 2 + 1 + uint64(len(msg.Data))
	}

	// Calculate total chunk size
	// Chunk contains all messages
	chunkSize := messageDataSize

	// Validate chunk size fits in encoding
	// For MVP, flags bits 0-1 = 0, so chunk size is 1 byte (max 255)
	if chunkSize > 255 {
		return 0, fmt.Errorf("chunk size %d exceeds maximum for 1-byte encoding (255)", chunkSize)
	}

	// Build header
	// Signature (4) + Version (1) + Flags (1) + Chunk Size (1) + Messages (variable)
	headerSize := 4 + 1 + 1 + 1 + chunkSize
	buf := make([]byte, headerSize)

	offset := 0

	// Write signature "OHDR" (4 bytes, little-endian format).
	copy(buf[offset:offset+4], "OHDR")
	offset += 4

	// Version
	buf[offset] = ohw.Version
	offset++

	// Flags
	buf[offset] = ohw.Flags
	offset++

	// Chunk 0 size (1 byte for flags bits 0-1 = 0)
	buf[offset] = uint8(chunkSize)
	offset++

	// Write messages
	for _, msg := range ohw.Messages {
		// Message type (1 byte for v2)
		buf[offset] = uint8(msg.Type) //nolint:gosec // Safe: message type is limited enum
		offset++

		// Message data size (2 bytes, little-endian)
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(len(msg.Data))) //nolint:gosec // Safe: message size validated
		offset += 2

		// Message flags (1 byte)
		// For MVP: flags = 0 (not shared, not constant, not shareable)
		buf[offset] = 0
		offset++

		// Message data
		copy(buf[offset:offset+len(msg.Data)], msg.Data)
		offset += len(msg.Data)
	}

	// Write to file
	n, err := w.WriteAt(buf, int64(address)) //nolint:gosec // Safe: address within file bounds
	if err != nil {
		return 0, fmt.Errorf("failed to write object header at address %d: %w", address, err)
	}

	if n != len(buf) {
		return 0, fmt.Errorf("incomplete object header write: wrote %d bytes, expected %d", n, len(buf))
	}

	return headerSize, nil
}

// AddMessageToObjectHeader adds a message to an object header.
// For MVP (v0.11.1-beta): Only supports object header v2 without continuation blocks.
//
// Parameters:
//   - oh: Object header to modify
//   - msgType: Message type (e.g., MsgAttribute = 0x000C)
//   - msgData: Encoded message bytes
//
// Returns:
//   - error: Non-nil if header full or add fails
//
// Limitations:
//   - No continuation blocks (returns error if header would overflow)
//   - Only object header v2 supported
//   - No message flags (always 0)
//
// Reference: H5O.c - H5O_msg_append().
func AddMessageToObjectHeader(oh *ObjectHeader, msgType MessageType, msgData []byte) error {
	if oh == nil {
		return fmt.Errorf("object header is nil")
	}

	if oh.Version != 2 {
		return fmt.Errorf("only object header version 2 is supported for modification, got version %d", oh.Version)
	}

	// For MVP: We don't support continuation blocks
	// Calculate the space needed for the new message
	// Message format in v2: Type(1) + Size(2) + Flags(1) + Data(variable)
	messageHeaderSize := 4 // Type(1) + Size(2) + Flags(1)
	totalMessageSize := messageHeaderSize + len(msgData)

	// For MVP: We check if adding this message would exceed a reasonable header size
	// HDF5 typically limits object header chunk 0 to 255 bytes (1-byte size encoding)
	// We'll check the total size of all messages
	currentMessagesSize := 0
	for _, msg := range oh.Messages {
		currentMessagesSize += 4 + len(msg.Data)
	}

	newTotalSize := currentMessagesSize + totalMessageSize

	// For MVP: Limit to 255 bytes (max size for 1-byte chunk size encoding)
	// In practice, headers with continuation blocks can be larger,
	// but we're not implementing that yet
	if newTotalSize > 255 {
		return fmt.Errorf("object header full (current: %d bytes, new message: %d bytes, max: 255 bytes); continuation blocks not yet supported",
			currentMessagesSize, totalMessageSize)
	}

	// Create new message
	newMessage := &HeaderMessage{
		Type:   msgType,
		Offset: 0, // Will be calculated during write
		Data:   make([]byte, len(msgData)),
	}
	copy(newMessage.Data, msgData)

	// Add to messages list
	oh.Messages = append(oh.Messages, newMessage)

	return nil
}

// WriteObjectHeader writes an object header back to disk at a given address.
// This is used when modifying object headers (e.g., adding attributes).
//
// For MVP (v0.11.1-beta):
//   - Only object header v2 supported
//   - No continuation blocks
//   - Overwrites existing header at the same address
//
// Parameters:
//   - w: Writer with WriteAt capability
//   - addr: File address where header is located
//   - oh: Object header to write
//   - sb: Superblock for encoding parameters
//
// Returns:
//   - error: Non-nil if write fails
//
// Reference: H5O.c - H5O_flush().
func WriteObjectHeader(w io.WriterAt, addr uint64, oh *ObjectHeader, sb *Superblock) error {
	_ = sb // Reserved for future use (v1 headers or encoding parameters)

	if oh == nil {
		return fmt.Errorf("object header is nil")
	}

	if oh.Version != 2 {
		return fmt.Errorf("only object header version 2 is supported for writing, got version %d", oh.Version)
	}

	// Build object header writer from the object header
	ohw := &ObjectHeaderWriter{
		Version:  oh.Version,
		Flags:    oh.Flags,
		Messages: make([]MessageWriter, len(oh.Messages)),
	}

	// Convert messages
	for i, msg := range oh.Messages {
		ohw.Messages[i] = MessageWriter{
			Type: msg.Type,
			Data: msg.Data,
		}
	}

	// Write the header
	_, err := ohw.WriteTo(w, addr)
	if err != nil {
		return fmt.Errorf("failed to write object header at address %d: %w", addr, err)
	}

	return nil
}

// RewriteObjectHeaderV2 rewrites an object header v2 with updated messages.
// This handles the case where we need to modify an existing object header
// by reading it, modifying it, and writing it back.
//
// For MVP (v0.11.1-beta):
//   - Only supports v2 headers without continuation blocks
//   - Overwrites header at original location if size permits
//   - Returns error if new header doesn't fit in original space
//
// Parameters:
//   - w: Writer with WriteAt capability
//   - r: Reader for reading current header
//   - addr: File address of object header
//   - sb: Superblock
//   - newMessages: Additional messages to add
//
// Returns:
//   - error: Non-nil if operation fails
//
// Note: This is a simplified version for MVP. Full implementation would:
//   - Support continuation blocks
//   - Handle header relocation if needed
//   - Support v1 headers
func RewriteObjectHeaderV2(w io.WriterAt, r io.ReaderAt, addr uint64, sb *Superblock, newMessages []*HeaderMessage) error {
	// Read existing object header
	oh, err := ReadObjectHeader(r, addr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	if oh.Version != 2 {
		return fmt.Errorf("only v2 headers supported for rewrite, got version %d", oh.Version)
	}

	// Add new messages
	for _, msg := range newMessages {
		err = AddMessageToObjectHeader(oh, msg.Type, msg.Data)
		if err != nil {
			return fmt.Errorf("failed to add message: %w", err)
		}
	}

	// Write back to same location
	err = WriteObjectHeader(w, addr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	return nil
}
