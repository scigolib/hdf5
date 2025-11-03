package core

import (
	"encoding/binary"
	"testing"
)

// TestLinkMessageHardLinkRoundTrip tests encoding and decoding of a hard link message.
func TestLinkMessageHardLinkRoundTrip(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create hard link message
	linkValue := make([]byte, 8)
	binary.LittleEndian.PutUint64(linkValue, 0x1234567890ABCDEF)

	original := &LinkMessage{
		Version:   1,
		Flags:     LinkFlagLinkTypeFieldBit, // Link type field present
		Type:      LinkTypeHard,
		Name:      "dataset1",
		LinkValue: linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify
	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: got %v, want %v", decoded.Type, original.Type)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}

	// Verify link value (object address)
	addr, err := decoded.GetHardLinkAddress(sb)
	if err != nil {
		t.Fatalf("GetHardLinkAddress failed: %v", err)
	}
	expectedAddr := uint64(0x1234567890ABCDEF)
	if addr != expectedAddr {
		t.Errorf("Address mismatch: got 0x%X, want 0x%X", addr, expectedAddr)
	}
}

// TestLinkMessageSoftLinkRoundTrip tests encoding and decoding of a soft link message.
func TestLinkMessageSoftLinkRoundTrip(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create soft link message
	targetPath := "/path/to/target"
	linkValue := make([]byte, 2+len(targetPath))
	binary.LittleEndian.PutUint16(linkValue[0:2], uint16(len(targetPath)))
	copy(linkValue[2:], targetPath)

	original := &LinkMessage{
		Version:   1,
		Flags:     LinkFlagLinkTypeFieldBit, // Link type field present
		Type:      LinkTypeSoft,
		Name:      "softlink1",
		LinkValue: linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify
	if decoded.Type != LinkTypeSoft {
		t.Errorf("Type mismatch: got %v, want %v", decoded.Type, LinkTypeSoft)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}

	// Verify link value (target path)
	path, err := decoded.GetSoftLinkPath()
	if err != nil {
		t.Fatalf("GetSoftLinkPath failed: %v", err)
	}
	if path != targetPath {
		t.Errorf("Path mismatch: got %q, want %q", path, targetPath)
	}
}

// TestLinkMessageExternalLinkRoundTrip tests encoding and decoding of an external link message.
func TestLinkMessageExternalLinkRoundTrip(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Create external link message
	fileName := "external.h5"
	objectPath := "/dataset"

	linkValue := make([]byte, 2+len(fileName)+2+len(objectPath))
	offset := 0

	// File name length + file name
	binary.LittleEndian.PutUint16(linkValue[offset:offset+2], uint16(len(fileName)))
	offset += 2
	copy(linkValue[offset:], fileName)
	offset += len(fileName)

	// Object path length + object path
	binary.LittleEndian.PutUint16(linkValue[offset:offset+2], uint16(len(objectPath)))
	offset += 2
	copy(linkValue[offset:], objectPath)

	original := &LinkMessage{
		Version:   1,
		Flags:     LinkFlagLinkTypeFieldBit, // Link type field present
		Type:      LinkTypeExternal,
		Name:      "externallink1",
		LinkValue: linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify
	if decoded.Type != LinkTypeExternal {
		t.Errorf("Type mismatch: got %v, want %v", decoded.Type, LinkTypeExternal)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}

	// Verify link value (file name + object path)
	gotFileName, gotObjectPath, err := decoded.GetExternalLinkInfo()
	if err != nil {
		t.Fatalf("GetExternalLinkInfo failed: %v", err)
	}
	if gotFileName != fileName {
		t.Errorf("File name mismatch: got %q, want %q", gotFileName, fileName)
	}
	if gotObjectPath != objectPath {
		t.Errorf("Object path mismatch: got %q, want %q", gotObjectPath, objectPath)
	}
}

// TestLinkMessageWithCreationOrder tests link message with creation order tracking.
func TestLinkMessageWithCreationOrder(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	linkValue := make([]byte, 8)
	binary.LittleEndian.PutUint64(linkValue, 0x1000)

	original := &LinkMessage{
		Version:       1,
		Flags:         LinkFlagLinkTypeFieldBit | LinkFlagCreationOrderBit,
		Type:          LinkTypeHard,
		CreationOrder: 42,
		Name:          "dataset42",
		LinkValue:     linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify creation order
	if !decoded.HasCreationOrder() {
		t.Error("Creation order should be present")
	}
	if decoded.CreationOrder != 42 {
		t.Errorf("Creation order mismatch: got %d, want %d", decoded.CreationOrder, 42)
	}
}

// TestLinkMessageWithCharSet tests link message with character set field.
func TestLinkMessageWithCharSet(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	linkValue := make([]byte, 8)
	binary.LittleEndian.PutUint64(linkValue, 0x2000)

	original := &LinkMessage{
		Version:   1,
		Flags:     LinkFlagLinkTypeFieldBit | LinkFlagCharSetBit,
		Type:      LinkTypeHard,
		CharSet:   1, // UTF-8
		Name:      "dataset_utf8",
		LinkValue: linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify character set
	if !decoded.HasCharSetField() {
		t.Error("Character set field should be present")
	}
	if decoded.CharSet != 1 {
		t.Errorf("Character set mismatch: got %d, want %d", decoded.CharSet, 1)
	}
}

// TestLinkMessageLongName tests link message with different name length sizes.
func TestLinkMessageLongName(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	linkValue := make([]byte, 8)
	binary.LittleEndian.PutUint64(linkValue, 0x3000)

	testCases := []struct {
		name       string
		nameLength int
		flags      uint8
	}{
		{"short (1 byte length)", 100, 0x00 | LinkFlagLinkTypeFieldBit},
		{"medium (2 byte length)", 300, 0x01 | LinkFlagLinkTypeFieldBit},
		{"long (4 byte length)", 70000, 0x02 | LinkFlagLinkTypeFieldBit},
		{"very long (8 byte length)", 100000, 0x03 | LinkFlagLinkTypeFieldBit},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testLinkNameLength(t, sb, linkValue, tc.nameLength, tc.flags)
		})
	}
}

// testLinkNameLength is a helper to reduce cognitive complexity.
func testLinkNameLength(t *testing.T, sb *Superblock, linkValue []byte, nameLength int, flags uint8) {
	// For very long names, we just test the encoding logic with a shorter actual name
	actualName := "test"
	if nameLength < 256 {
		// For short names, use actual length
		for len(actualName) < nameLength {
			actualName += "x"
		}
	}

	original := &LinkMessage{
		Version:   1,
		Flags:     flags,
		Type:      LinkTypeHard,
		Name:      actualName,
		LinkValue: linkValue,
	}

	// Encode
	encoded, err := EncodeLinkMessage(original, sb)
	if err != nil {
		t.Fatalf("EncodeLinkMessage failed: %v", err)
	}

	// Decode
	decoded, err := ParseLinkMessage(encoded, sb)
	if err != nil {
		t.Fatalf("ParseLinkMessage failed: %v", err)
	}

	// Verify name
	if decoded.Name != actualName {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, actualName)
	}

	// Verify length field size
	lengthSize := decoded.GetLinkNameLengthSize()
	expectedLengthSize := (flags & LinkFlagSizeOfLengthMask)
	var expectedSize int
	switch expectedLengthSize {
	case 0:
		expectedSize = 1
	case 1:
		expectedSize = 2
	case 2:
		expectedSize = 4
	case 3:
		expectedSize = 8
	}
	if lengthSize != expectedSize {
		t.Errorf("Length size mismatch: got %d, want %d", lengthSize, expectedSize)
	}
}

// TestLinkMessageInvalidVersion tests error handling for invalid version.
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

// TestLinkMessageTruncated tests error handling for truncated messages.
func TestLinkMessageTruncated(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"only version", []byte{1}},
		{"missing link type", []byte{1, LinkFlagLinkTypeFieldBit}},
		{"missing creation order", []byte{1, LinkFlagCreationOrderBit, 0}},
		{"missing name length", []byte{1, 0}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseLinkMessage(tc.data, sb)
			if err == nil {
				t.Error("Expected error for truncated message, got nil")
			}
		})
	}
}

// TestLinkMessageGetters tests the getter methods for different link types.
func TestLinkMessageGetters(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Test hard link address getter
	t.Run("HardLinkAddress", func(t *testing.T) {
		linkValue := make([]byte, 8)
		binary.LittleEndian.PutUint64(linkValue, 0xABCD1234)

		lm := &LinkMessage{
			Type:      LinkTypeHard,
			LinkValue: linkValue,
		}

		addr, err := lm.GetHardLinkAddress(sb)
		if err != nil {
			t.Fatalf("GetHardLinkAddress failed: %v", err)
		}
		if addr != 0xABCD1234 {
			t.Errorf("Address mismatch: got 0x%X, want 0xABCD1234", addr)
		}

		// Test error on wrong type
		lm.Type = LinkTypeSoft
		_, err = lm.GetHardLinkAddress(sb)
		if err == nil {
			t.Error("Expected error for GetHardLinkAddress on soft link")
		}
	})

	// Test soft link path getter
	t.Run("SoftLinkPath", func(t *testing.T) {
		targetPath := "/my/target/path"
		lm := &LinkMessage{
			Type:      LinkTypeSoft,
			LinkValue: []byte(targetPath),
		}

		path, err := lm.GetSoftLinkPath()
		if err != nil {
			t.Fatalf("GetSoftLinkPath failed: %v", err)
		}
		if path != targetPath {
			t.Errorf("Path mismatch: got %q, want %q", path, targetPath)
		}

		// Test error on wrong type
		lm.Type = LinkTypeHard
		_, err = lm.GetSoftLinkPath()
		if err == nil {
			t.Error("Expected error for GetSoftLinkPath on hard link")
		}
	})

	// Test external link info getter
	t.Run("ExternalLinkInfo", func(t *testing.T) {
		fileName := "external.h5"
		objectPath := "/dataset"

		linkValue := make([]byte, 2+len(fileName)+2+len(objectPath))
		offset := 0
		binary.LittleEndian.PutUint16(linkValue[offset:], uint16(len(fileName)))
		offset += 2
		copy(linkValue[offset:], fileName)
		offset += len(fileName)
		binary.LittleEndian.PutUint16(linkValue[offset:], uint16(len(objectPath)))
		offset += 2
		copy(linkValue[offset:], objectPath)

		lm := &LinkMessage{
			Type:      LinkTypeExternal,
			LinkValue: linkValue,
		}

		gotFileName, gotObjectPath, err := lm.GetExternalLinkInfo()
		if err != nil {
			t.Fatalf("GetExternalLinkInfo failed: %v", err)
		}
		if gotFileName != fileName {
			t.Errorf("File name mismatch: got %q, want %q", gotFileName, fileName)
		}
		if gotObjectPath != objectPath {
			t.Errorf("Object path mismatch: got %q, want %q", gotObjectPath, objectPath)
		}

		// Test error on wrong type
		lm.Type = LinkTypeHard
		_, _, err = lm.GetExternalLinkInfo()
		if err == nil {
			t.Error("Expected error for GetExternalLinkInfo on hard link")
		}
	})
}
