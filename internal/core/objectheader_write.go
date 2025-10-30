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

	// Signature: "OHDR" (little-endian)
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
		buf[offset] = uint8(msg.Type)
		offset++

		// Message data size (2 bytes, little-endian)
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(len(msg.Data)))
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
	n, err := w.WriteAt(buf, int64(address))
	if err != nil {
		return 0, fmt.Errorf("failed to write object header at address %d: %w", address, err)
	}

	if n != len(buf) {
		return 0, fmt.Errorf("incomplete object header write: wrote %d bytes, expected %d", n, len(buf))
	}

	return headerSize, nil
}
