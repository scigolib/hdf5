package core

import (
	"encoding/binary"
	"testing"
)

// TestLinkMessageInvalidVersion tests that encoding rejects unsupported versions.
func TestLinkMessageInvalidVersion(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create message with invalid version
	original := &LinkMessage{
		Version:   2, // Invalid: only version 1 is supported
		Flags:     0,
		Type:      LinkTypeHard,
		Name:      "test",
		LinkValue: make([]byte, 8),
	}

	// Encode should fail
	_, err := EncodeLinkMessage(original, sb)
	if err == nil {
		t.Error("Expected error for invalid version, got nil")
	}
}
