package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// LinkType defines the type of link (hard, soft, external).
type LinkType uint8

// Link type constants from HDF5 specification.
const (
	LinkTypeHard     LinkType = 0  // Hard link: direct reference to object
	LinkTypeSoft     LinkType = 1  // Soft link: symbolic path to object
	LinkTypeExternal LinkType = 64 // External link: reference to object in another file
)

// String returns the string representation of the link type.
func (lt LinkType) String() string {
	switch lt {
	case LinkTypeHard:
		return "Hard"
	case LinkTypeSoft:
		return "Soft"
	case LinkTypeExternal:
		return "External"
	default:
		return fmt.Sprintf("Unknown(%d)", lt)
	}
}

// LinkMessage represents a link message in an HDF5 file.
// Link messages are used in modern HDF5 groups (dense storage) to store
// information about links between objects.
//
// Format (HDF5 Spec Section IV.A.2.f):
//   - Version (1 byte): Always 1 for current spec
//   - Flags (1 byte): Link type and creation order tracking
//   - Link Type (1 byte, optional): Present if bit 3 of flags is set
//   - Creation Order (8 bytes, optional): Present if bit 2 of flags is set
//   - Link Name Character Set (1 byte): 0=ASCII, 1=UTF-8
//   - Link Name Length (1, 2, 4, or 8 bytes): Size of link name encoding depends on flags
//   - Link Name (variable): UTF-8 or ASCII encoded name
//   - Link Information (variable): Format depends on link type
//
// Reference: HDF5 Format Spec Section IV.A.2.f (Link Message).
// C Reference: H5Oint.c - H5O_link_t structure and encoding/decoding functions.
type LinkMessage struct {
	Version       uint8    // Message version (always 1 for now)
	Flags         uint8    // Link type and flags
	Type          LinkType // Link type (hard, soft, external)
	CreationOrder uint64   // Creation order value (optional)
	CharSet       uint8    // Character set encoding (0=ASCII, 1=UTF-8)
	Name          string   // Link name
	LinkValue     []byte   // Link-specific data (depends on type)
}

// Link message flags.
const (
	LinkFlagSizeOfLengthMask   uint8 = 0x03 // Bits 0-1: size of length field (0=1, 1=2, 2=4, 3=8 bytes)
	LinkFlagCreationOrderBit   uint8 = 0x04 // Bit 2: creation order field present
	LinkFlagLinkTypeFieldBit   uint8 = 0x08 // Bit 3: link type field present
	LinkFlagCharSetBit         uint8 = 0x10 // Bit 4: link name character set field present
	LinkFlagLinkNameEncodedBit uint8 = 0x18 // Bits 3-4: both must be set for encoded name
)

// HasCreationOrder returns true if creation order field is present.
func (lm *LinkMessage) HasCreationOrder() bool {
	return (lm.Flags & LinkFlagCreationOrderBit) != 0
}

// HasLinkTypeField returns true if link type field is present.
func (lm *LinkMessage) HasLinkTypeField() bool {
	return (lm.Flags & LinkFlagLinkTypeFieldBit) != 0
}

// HasCharSetField returns true if character set field is present.
func (lm *LinkMessage) HasCharSetField() bool {
	return (lm.Flags & LinkFlagCharSetBit) != 0
}

// GetLinkNameLengthSize returns the size of the link name length field (1, 2, 4, or 8 bytes).
func (lm *LinkMessage) GetLinkNameLengthSize() int {
	sizeCode := lm.Flags & LinkFlagSizeOfLengthMask
	switch sizeCode {
	case 0:
		return 1
	case 1:
		return 2
	case 2:
		return 4
	case 3:
		return 8
	default:
		return 1 // Should never happen
	}
}

// ParseLinkMessage parses a link message from header message data.
//
// This implements the decoding logic matching the C reference H5Oint.c:H5O__link_decode().
//
// Format:
//   - Version (1 byte): Must be 1
//   - Flags (1 byte): Link type and flags
//   - Link Type (1 byte, optional): If bit 3 of flags is set
//   - Creation Order (8 bytes, optional): If bit 2 of flags is set
//   - Link Name Character Set (1 byte, optional): If bit 4 of flags is set
//   - Link Name Length (1-8 bytes): Depends on flags bits 0-1
//   - Link Name (variable): UTF-8 or ASCII
//   - Link Information (variable): Depends on link type
//
// Reference: H5Oint.c - H5O__link_decode().
func ParseLinkMessage(data []byte, sb *Superblock) (*LinkMessage, error) {
	lm, offset, err := parseLinkMessageHeader(data)
	if err != nil {
		return nil, err
	}

	// Read link name
	nameLength, offset, err := parseLinkNameLength(data, offset, lm)
	if err != nil {
		return nil, err
	}

	lm.Name, offset, err = parseLinkName(data, offset, nameLength)
	if err != nil {
		return nil, err
	}

	// Read link information (depends on link type)
	if err := parseLinkValue(data, offset, lm, sb); err != nil {
		return nil, err
	}

	return lm, nil
}

// parseLinkMessageHeader parses the header portion of a link message.
func parseLinkMessageHeader(data []byte) (*LinkMessage, int, error) {
	if len(data) < 2 {
		return nil, 0, errors.New("link message too short (need at least 2 bytes for version and flags)")
	}

	lm := &LinkMessage{
		Version: data[0],
		Flags:   data[1],
	}
	offset := 2

	if lm.Version != 1 {
		return nil, 0, fmt.Errorf("unsupported link message version: %d (only version 1 is supported)", lm.Version)
	}

	// Read link type if present (optional, bit 3 of flags)
	if lm.HasLinkTypeField() {
		if len(data) < offset+1 {
			return nil, 0, errors.New("link message truncated (missing link type field)")
		}
		lm.Type = LinkType(data[offset])
		offset++
	} else {
		lm.Type = LinkTypeHard
	}

	// Read creation order if present (optional, bit 2 of flags)
	if lm.HasCreationOrder() {
		if len(data) < offset+8 {
			return nil, 0, errors.New("link message truncated (missing creation order field)")
		}
		lm.CreationOrder = binary.LittleEndian.Uint64(data[offset : offset+8])
		offset += 8
	}

	// Read character set if present (optional, bit 4 of flags)
	if lm.HasCharSetField() {
		if len(data) < offset+1 {
			return nil, 0, errors.New("link message truncated (missing character set field)")
		}
		lm.CharSet = data[offset]
		offset++
	} else {
		lm.CharSet = 0
	}

	return lm, offset, nil
}

// parseLinkNameLength reads the link name length field.
func parseLinkNameLength(data []byte, offset int, lm *LinkMessage) (uint64, int, error) {
	lengthSize := lm.GetLinkNameLengthSize()
	if len(data) < offset+lengthSize {
		return 0, 0, errors.New("link message truncated (missing link name length)")
	}

	var nameLength uint64
	switch lengthSize {
	case 1:
		nameLength = uint64(data[offset])
	case 2:
		nameLength = uint64(binary.LittleEndian.Uint16(data[offset : offset+2]))
	case 4:
		nameLength = uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	case 8:
		nameLength = binary.LittleEndian.Uint64(data[offset : offset+8])
	}

	return nameLength, offset + lengthSize, nil
}

// parseLinkName reads the link name string.
func parseLinkName(data []byte, offset int, nameLength uint64) (string, int, error) {
	// Validate nameLength is reasonable (< 1MB)
	if nameLength > 1024*1024 {
		return "", 0, fmt.Errorf("link name length too large: %d bytes", nameLength)
	}

	nameLengthInt := int(nameLength)
	if len(data) < offset+nameLengthInt {
		return "", 0, errors.New("link message truncated (missing link name)")
	}

	name := string(data[offset : offset+nameLengthInt])
	return name, offset + nameLengthInt, nil
}

// parseLinkValue reads the link-type-specific value.
func parseLinkValue(data []byte, offset int, lm *LinkMessage, sb *Superblock) error {
	switch lm.Type {
	case LinkTypeHard:
		return parseHardLinkValue(data, offset, lm, sb)
	case LinkTypeSoft:
		return parseSoftLinkValue(data, offset, lm)
	case LinkTypeExternal:
		return parseExternalLinkValue(data, offset, lm)
	default:
		return fmt.Errorf("unsupported link type: %d", lm.Type)
	}
}

// parseHardLinkValue reads hard link value (object address).
func parseHardLinkValue(data []byte, offset int, lm *LinkMessage, sb *Superblock) error {
	if len(data) < offset+int(sb.OffsetSize) {
		return errors.New("link message truncated (missing hard link address)")
	}
	lm.LinkValue = make([]byte, sb.OffsetSize)
	copy(lm.LinkValue, data[offset:offset+int(sb.OffsetSize)])
	return nil
}

// parseSoftLinkValue reads soft link value (target path).
func parseSoftLinkValue(data []byte, offset int, lm *LinkMessage) error {
	if len(data) < offset+2 {
		return errors.New("link message truncated (missing soft link length)")
	}
	softLinkLength := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2

	if len(data) < offset+int(softLinkLength) {
		return errors.New("link message truncated (missing soft link path)")
	}
	lm.LinkValue = make([]byte, softLinkLength)
	copy(lm.LinkValue, data[offset:offset+int(softLinkLength)])
	return nil
}

// parseExternalLinkValue reads external link value (file name + object path).
func parseExternalLinkValue(data []byte, offset int, lm *LinkMessage) error {
	startOffset := offset // Save start for copying entire value including length fields

	if len(data) < offset+2 {
		return errors.New("link message truncated (missing external link file name length)")
	}
	fileNameLength := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2

	if len(data) < offset+int(fileNameLength)+2 {
		return errors.New("link message truncated (missing external link file name or path length)")
	}

	// Calculate total length including both length fields
	// Format: fileNameLength(2) + fileName + pathLength(2) + path
	totalLength := 2 + int(fileNameLength) // File name length field + file name
	pathLengthOffset := offset + int(fileNameLength)
	pathLength := binary.LittleEndian.Uint16(data[pathLengthOffset : pathLengthOffset+2])
	totalLength += 2 + int(pathLength) // Path length field + path

	if len(data) < startOffset+totalLength {
		return errors.New("link message truncated (missing external link data)")
	}

	// Copy entire external link value from start (including length fields)
	lm.LinkValue = make([]byte, totalLength)
	copy(lm.LinkValue, data[startOffset:startOffset+totalLength])
	return nil
}

// EncodeLinkMessage encodes a link message for writing.
//
// This implements the encoding logic matching the C reference H5Oint.c:H5O__link_encode().
//
// Format:
//   - Version (1 byte): Always 1
//   - Flags (1 byte): Link type and flags
//   - Link Type (1 byte, optional): If bit 3 of flags is set
//   - Creation Order (8 bytes, optional): If bit 2 of flags is set
//   - Link Name Character Set (1 byte, optional): If bit 4 of flags is set
//   - Link Name Length (1-8 bytes): Depends on flags bits 0-1
//   - Link Name (variable): UTF-8 or ASCII
//   - Link Information (variable): Depends on link type
//
// Parameters:
//   - lm: Link message to encode
//   - _ : Superblock (unused, kept for API consistency)
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Reference: H5Oint.c - H5O__link_encode().
func EncodeLinkMessage(lm *LinkMessage, _ *Superblock) ([]byte, error) {
	if lm == nil {
		return nil, errors.New("link message is nil")
	}

	// Validate version
	if lm.Version != 1 {
		return nil, fmt.Errorf("unsupported link message version: %d (only version 1 is supported)", lm.Version)
	}

	// Calculate message size
	size := 2 // Version (1) + Flags (1)

	// Add link type field if present
	if lm.HasLinkTypeField() {
		size++
	}

	// Add creation order field if present
	if lm.HasCreationOrder() {
		size += 8
	}

	// Add character set field if present
	if lm.HasCharSetField() {
		size++
	}

	// Add link name length field
	lengthSize := lm.GetLinkNameLengthSize()
	size += lengthSize

	// Add link name
	size += len(lm.Name)

	// Add link value (depends on type)
	size += len(lm.LinkValue)

	buf := make([]byte, size)
	offset := 0

	// Write version (byte 0)
	buf[offset] = lm.Version
	offset++

	// Write flags (byte 1)
	buf[offset] = lm.Flags
	offset++

	// Write link type if present
	if lm.HasLinkTypeField() {
		buf[offset] = uint8(lm.Type)
		offset++
	}

	// Write creation order if present
	if lm.HasCreationOrder() {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], lm.CreationOrder)
		offset += 8
	}

	// Write character set if present
	if lm.HasCharSetField() {
		buf[offset] = lm.CharSet
		offset++
	}

	// Write link name length
	nameLength := uint64(len(lm.Name))
	if err := writeLinkNameLength(buf, offset, nameLength, lengthSize); err != nil {
		return nil, err
	}
	offset += lengthSize

	// Write link name
	copy(buf[offset:], lm.Name)
	offset += len(lm.Name)

	// Write link value
	copy(buf[offset:], lm.LinkValue)

	return buf, nil
}

// writeLinkNameLength writes the link name length field with proper validation.
func writeLinkNameLength(buf []byte, offset int, nameLength uint64, lengthSize int) error {
	switch lengthSize {
	case 1:
		if nameLength > 255 {
			return fmt.Errorf("name length %d exceeds 1-byte maximum (255)", nameLength)
		}
		buf[offset] = uint8(nameLength)
	case 2:
		if nameLength > 65535 {
			return fmt.Errorf("name length %d exceeds 2-byte maximum (65535)", nameLength)
		}
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(nameLength))
	case 4:
		if nameLength > 4294967295 {
			return fmt.Errorf("name length %d exceeds 4-byte maximum (4294967295)", nameLength)
		}
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(nameLength))
	case 8:
		binary.LittleEndian.PutUint64(buf[offset:offset+8], nameLength)
	default:
		return fmt.Errorf("invalid length size: %d", lengthSize)
	}
	return nil
}

// GetHardLinkAddress extracts the object address from a hard link's LinkValue.
// Returns the address and an error if the link is not a hard link or data is invalid.
func (lm *LinkMessage) GetHardLinkAddress(sb *Superblock) (uint64, error) {
	if lm.Type != LinkTypeHard {
		return 0, fmt.Errorf("not a hard link (type=%s)", lm.Type)
	}

	if len(lm.LinkValue) != int(sb.OffsetSize) {
		return 0, fmt.Errorf("invalid hard link value size: got %d, expected %d", len(lm.LinkValue), sb.OffsetSize)
	}

	return readUint64(lm.LinkValue, int(sb.OffsetSize), sb.Endianness), nil
}

// GetSoftLinkPath extracts the target path from a soft link's LinkValue.
// Returns the path string and an error if the link is not a soft link or data is invalid.
func (lm *LinkMessage) GetSoftLinkPath() (string, error) {
	if lm.Type != LinkTypeSoft {
		return "", fmt.Errorf("not a soft link (type=%s)", lm.Type)
	}

	if len(lm.LinkValue) == 0 {
		return "", errors.New("empty soft link path")
	}

	return string(lm.LinkValue), nil
}

// GetExternalLinkInfo extracts the file name and object path from an external link's LinkValue.
// Returns (fileName, objectPath, error).
func (lm *LinkMessage) GetExternalLinkInfo() (string, string, error) {
	if lm.Type != LinkTypeExternal {
		return "", "", fmt.Errorf("not an external link (type=%s)", lm.Type)
	}

	if len(lm.LinkValue) < 4 { // Minimum: 2 bytes file name length + 2 bytes path length
		return "", "", errors.New("external link value too short")
	}

	offset := 0

	// Read file name length (2 bytes)
	fileNameLength := binary.LittleEndian.Uint16(lm.LinkValue[offset : offset+2])
	offset += 2

	if len(lm.LinkValue) < offset+int(fileNameLength)+2 {
		return "", "", errors.New("external link value truncated (missing file name or path length)")
	}

	// Read file name
	fileName := string(lm.LinkValue[offset : offset+int(fileNameLength)])
	offset += int(fileNameLength)

	// Read path length (2 bytes)
	pathLength := binary.LittleEndian.Uint16(lm.LinkValue[offset : offset+2])
	offset += 2

	if len(lm.LinkValue) < offset+int(pathLength) {
		return "", "", errors.New("external link value truncated (missing object path)")
	}

	// Read object path
	objectPath := string(lm.LinkValue[offset : offset+int(pathLength)])

	return fileName, objectPath, nil
}
