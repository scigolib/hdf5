// Package core provides HDF5 file format parsing and manipulation functionality.
package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/scigolib/hdf5/internal/utils"
)

// Attribute represents an HDF5 attribute with metadata and data.
type Attribute struct {
	Name string

	// Datatype is the decoded datatype of the attribute. It must be treated
	// as read-only: when the attribute's datatype is committed (a shared
	// datatype message), this is the one decoded copy the File keeps for that
	// committed type, shared by every attribute and dataset that refers to it
	// (resolveSharedDatatype), so a change would alter how all of them read,
	// and would race with concurrent reads. Not copying it is deliberate: a
	// copy per attribute made a file of many references to one large compound
	// type cost hundreds of KiB per attribute. Dataset.Datatype in the public
	// package returns a value (DatatypeInfo) instead.
	Datatype  *DatatypeMessage
	Dataspace *DataspaceMessage
	Data      []byte

	// For variable-length types, we need access to the file reader
	// to resolve Global Heap references.
	reader     io.ReaderAt
	offsetSize int
}

// AttributeInfoMessage represents the Attribute Info Message (0x0015,
// H5Oprivate.h:228; 0x000F in files of this library up to v0.14.2).
// This message contains information about dense attribute storage.
// Reference: H5Adense.c in C library.
type AttributeInfoMessage struct {
	Version            uint8
	Flags              uint8
	FractalHeapAddr    uint64 // Address of fractal heap for dense attribute storage
	BTreeNameIndexAddr uint64 // Address of B-tree v2 for indexing attributes by name
	// Optional fields for creation order (if tracked):
	MaxCreationIndex    uint64 // Only present if creation order tracked
	BTreeOrderIndexAddr uint64 // Only present if creation order indexed
}

// HasDenseStorage reports whether the attributes are in dense storage: the
// fractal heap address is defined. C gives every version 2 object header with
// attributes an attribute info message and leaves its heap address undefined
// while the attributes are compact (H5Oattribute.c:225-236, 246, 315).
//
// The decoder turns an all-ones address of any width into the 64-bit
// undefined value (DecodeAddress); the narrow all-ones value of offsetSize is
// checked too, for a message built in memory. A heap at address 0 (the
// superblock) is no storage either, as this reader always took it.
func (m *AttributeInfoMessage) HasDenseStorage(offsetSize uint8) bool {
	addr := m.FractalHeapAddr
	if addr == 0 || addr == undefinedAddress {
		return false
	}
	return offsetSize >= 8 || addr != undefinedAddress>>(64-8*uint(offsetSize))
}

// ParseAttributeMessage parses an attribute message (type 0x000C).
// Format according to HDF5 spec:
// - Version (1 byte).
// - Flags (1 byte) - reserved, always 0.
// - Name size (2 bytes).
// - Datatype size (2 bytes).
// - Dataspace size (2 bytes).
// - Name encoding (1 byte) - for version 3+.
// - Name (variable, null-terminated).
// - Datatype message data.
// - Dataspace message data.
// - Data (variable).
//
// The message is decoded without the file, so a shared (committed) datatype
// cannot be resolved and is reported with an error IsSharedDatatype
// recognizes; ParseAttributesFromMessages resolves it.
func ParseAttributeMessage(data []byte, endianness binary.ByteOrder) (*Attribute, error) {
	return parseAttributeMessage(data, endianness, nil)
}

// sharedResolver follows the shared-message references of an attribute in one
// file (resolveSharedMessage and resolveSharedDatatype bound to it). A nil
// *sharedResolver has no file: a shared field is reported as unresolved.
type sharedResolver struct {
	r  io.ReaderAt
	sb *Superblock
}

// fileResolver binds shared-message resolution to the file r.
func fileResolver(r io.ReaderAt, sb *Superblock) *sharedResolver {
	return &sharedResolver{r: r, sb: sb}
}

// parseAttributeMessage decodes an attribute message as H5O__attr_decode
// (H5Oattr.c:113-300). With H5O_ATTR_FLAG_TYPE_SHARED or
// H5O_ATTR_FLAG_SPACE_SHARED set (H5Oattr.c:88-89) the datatype or dataspace
// field holds a shared-message reference, which C decodes through the shared
// wrapper (H5Oattr.c:218-221, :240-243); resolve follows it, and a nil resolve
// reports the shared datatype as unresolved.
//
// A shared datatype is decoded once per file and the decoded type is shared
// by every attribute that refers to it (resolveSharedDatatype); callers must
// not modify Attribute.Datatype.
func parseAttributeMessage(data []byte, endianness binary.ByteOrder, resolve *sharedResolver) (*Attribute, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: attribute message too short: %d bytes", utils.ErrCorrupt, len(data))
	}

	attr := &Attribute{}
	offset := 0

	// Version.
	version := data[offset]
	offset++

	// Flags: bit 0 marks a shared (committed) datatype, H5O_ATTR_FLAG_TYPE_SHARED
	// (H5Oattr.c:88, decoded at H5Oattr.c:219). Its datatype field is then a
	// shared-message reference: valid HDF5, not corruption. Without a file to
	// resolve it in, it is reported as unresolved.
	// The byte is unused before version 2 (H5Oattr.c:153-161), and a version
	// 2+ flag outside H5O_ATTR_FLAG_ALL is rejected ("unknown flag").
	var flags uint8
	if version >= 2 {
		flags = data[offset]
		if flags&^(attrFlagTypeShared|attrFlagSpaceShared) != 0 {
			return nil, fmt.Errorf("%w: unknown flag 0x%02x for attribute message (H5Oattr.c:158)", utils.ErrCorrupt, flags)
		}
	}
	typeShared := flags&attrFlagTypeShared != 0
	spaceShared := flags&attrFlagSpaceShared != 0
	if typeShared && resolve == nil {
		return nil, fmt.Errorf("attribute datatype: %w", errSharedDatatype)
	}
	if spaceShared && resolve == nil {
		return nil, fmt.Errorf("attribute dataspace is a shared message: %w", errSharedDatatype)
	}
	offset++

	// Name size (2 bytes).
	nameSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Datatype size (2 bytes).
	datatypeSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Dataspace size (2 bytes).
	dataspaceSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// For version 3+, there's a name encoding byte.
	if version >= 3 {
		// Name encoding (1 byte) - 0 = ASCII, 1 = UTF-8.
		offset++
	}

	// Read name (null-terminated).
	if offset+int(nameSize) > len(data) {
		return nil, fmt.Errorf("name extends beyond message: offset=%d, nameSize=%d, dataLen=%d",
			offset, nameSize, len(data))
	}

	// Name is null-terminated, so subtract 1 for the null byte.
	if nameSize > 0 {
		attr.Name = string(data[offset : offset+int(nameSize)-1])
	}

	// Version 1 pads the name, datatype and dataspace to 8-byte boundaries;
	// from version 2 on the sizes are exact: H5O__attr_decode applies
	// H5O_ALIGN_OLD only when version < H5O_ATTR_VERSION_2 (H5Oattr.c:207-208,
	// :224-225, :257-258). The sum is done in int so 0xFFFF cannot wrap.
	alignTo8 := func(size uint16) int {
		return (int(size) + 7) &^ 7
	}

	if version < 2 {
		// Version 1: pad to 8-byte boundaries.
		offset += alignTo8(nameSize)
	} else {
		// Version 2+: exact sizes.
		offset += int(nameSize)
	}

	// Parse datatype.
	if offset+int(datatypeSize) > len(data) {
		return nil, fmt.Errorf("datatype extends beyond message")
	}

	datatypeData := data[offset : offset+int(datatypeSize)]
	var err error
	if typeShared {
		if attr.Datatype, err = resolveSharedDatatype(resolve.r, resolve.sb, datatypeData); err != nil {
			return nil, fmt.Errorf("attribute datatype: %w", err)
		}
	} else if attr.Datatype, err = ParseDatatypeMessage(datatypeData); err != nil {
		return nil, utils.WrapError("datatype parse failed", err)
	}

	if version < 2 {
		offset += alignTo8(datatypeSize)
	} else {
		offset += int(datatypeSize)
	}

	// Parse dataspace.
	if offset+int(dataspaceSize) > len(data) {
		return nil, fmt.Errorf("dataspace extends beyond message")
	}

	dataspaceData := data[offset : offset+int(dataspaceSize)]
	if spaceShared {
		if dataspaceData, err = resolveSharedMessage(resolve.r, resolve.sb, dataspaceData, MsgDataspace); err != nil {
			return nil, fmt.Errorf("attribute dataspace: %w", err)
		}
	}
	attr.Dataspace, err = ParseDataspaceMessage(dataspaceData)
	if err != nil {
		return nil, utils.WrapError("dataspace parse failed", err)
	}

	if version < 2 {
		offset += alignTo8(dataspaceSize)
	} else {
		offset += int(dataspaceSize)
	}

	// Remaining data is the attribute value.
	if offset < len(data) {
		//nolint:gosec // G115: Safe conversion - offset is bounded by len(data)
		attrDataSize := uint64(len(data) - offset)

		// CVE-2025-6269 fix: Validate attribute data size before allocation.
		if err := utils.ValidateBufferSize(attrDataSize, utils.MaxAttributeSize, "attribute data"); err != nil {
			return nil, fmt.Errorf("invalid attribute size: %w", err)
		}

		attr.Data = make([]byte, attrDataSize)
		copy(attr.Data, data[offset:])
	}

	return attr, nil
}

// ReadValue reads the attribute value as the appropriate Go type.
func (a *Attribute) ReadValue() (interface{}, error) {
	if a.Datatype == nil || a.Dataspace == nil {
		return nil, fmt.Errorf("attribute missing datatype or dataspace")
	}

	totalElements := a.Dataspace.TotalElements()
	if totalElements == 0 || len(a.Data) == 0 {
		// Empty attribute - return empty slice instead of nil.
		return []interface{}{}, nil
	}

	// For scalar attributes, return single value.
	isScalar := len(a.Dataspace.Dimensions) == 0 ||
		(len(a.Dataspace.Dimensions) == 1 && a.Dataspace.Dimensions[0] == 1)

	switch a.Datatype.Class {
	case DatatypeFixed:
		size := a.Datatype.Size
		if !isGoIntegerWidth(size) {
			break
		}
		// CVE-2025-6269 fix: Check for multiplication overflow before processing.
		totalBytes, err := utils.SafeMultiply(totalElements, uint64(size))
		if err != nil {
			return nil, fmt.Errorf("attribute size overflow (%d-byte integer): %w", size, err)
		}
		if totalBytes > uint64(len(a.Data)) {
			return nil, fmt.Errorf("attribute data size mismatch: need %d bytes, have %d",
				totalBytes, len(a.Data))
		}
		// 1- and 2-byte integers read with their sign. 4- and 8-byte integers
		// keep the int32/int64 values attributes have always returned, also
		// when the type is unsigned; every width reads in its stored byte order.
		signed := a.Datatype.IsSignedFixedPoint() || size >= 4
		return fixedArray(a.Data, totalElements, size, signed, a.Datatype.GetByteOrder(), isScalar), nil

	case DatatypeFloat:
		switch a.Datatype.Size {
		case 4:
			// CVE-2025-6269 fix: Check for multiplication overflow before processing.
			totalBytes, err := utils.SafeMultiply(totalElements, 4)
			if err != nil {
				return nil, fmt.Errorf("attribute size overflow (float32): %w", err)
			}

			if totalBytes > uint64(len(a.Data)) {
				return nil, fmt.Errorf("attribute data size mismatch: need %d bytes, have %d",
					totalBytes, len(a.Data))
			}

			values := make([]float32, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 4
				bits := a.Datatype.GetByteOrder().Uint32(a.Data[offset : offset+4])
				values[i] = float32frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		case 8:
			// CVE-2025-6269 fix: Check for multiplication overflow before processing.
			totalBytes, err := utils.SafeMultiply(totalElements, 8)
			if err != nil {
				return nil, fmt.Errorf("attribute size overflow (float64): %w", err)
			}

			if totalBytes > uint64(len(a.Data)) {
				return nil, fmt.Errorf("attribute data size mismatch: need %d bytes, have %d",
					totalBytes, len(a.Data))
			}

			values := make([]float64, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 8
				bits := a.Datatype.GetByteOrder().Uint64(a.Data[offset : offset+8])
				values[i] = float64frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		}

	case DatatypeString:
		// Fixed-length strings.
		if !a.Datatype.IsFixedString() {
			return nil, fmt.Errorf("variable-length strings not yet supported in attributes")
		}

		stringSize := uint64(a.Datatype.Size)
		paddingType := a.Datatype.GetStringPadding()

		// ADR-003 (issue #68): totalElements comes from the attribute dataspace,
		// i.e. from file metadata, so the byte count is validated before the
		// slice is allocated — a failed Go allocation is a fatal runtime error,
		// not a recoverable NULL like the C library's malloc. Mirrors
		// H5O__attr_decode, where the overflow check and H5_IS_BUFFER_OVERFLOW
		// run before H5FL_BLK_MALLOC (H5Oattr.c:274-283).
		totalBytes, err := utils.SafeMultiply(totalElements, stringSize)
		if err != nil {
			return nil, fmt.Errorf("attribute size overflow (string): %w", err)
		}

		if totalBytes > uint64(len(a.Data)) {
			return nil, fmt.Errorf("attribute data size mismatch: need %d bytes, have %d",
				totalBytes, len(a.Data))
		}

		values := make([]string, totalElements)

		for i := uint64(0); i < totalElements; i++ {
			offset := i * stringSize
			if offset+stringSize > uint64(len(a.Data)) {
				return nil, fmt.Errorf("data too short for string element %d", i)
			}
			stringBytes := a.Data[offset : offset+stringSize]
			values[i] = decodeFixedString(stringBytes, paddingType)
		}

		if isScalar {
			return values[0], nil
		}
		return values, nil

	case DatatypeVarLen:
		// Variable-length types (most commonly variable-length strings).
		// Data is stored as Global Heap references.
		if a.reader == nil {
			return nil, fmt.Errorf("variable-length attribute requires file reader (not available)")
		}

		// Check if this is a variable-length string.
		if !a.Datatype.IsVariableString() {
			return nil, fmt.Errorf("variable-length non-string types not yet supported in attributes")
		}

		// Each vlen element is: length (4 bytes) + heap_address (offsetSize bytes) + object_index (4 bytes).
		//nolint:gosec // G115: offsetSize is bounded to 4 or 8 by HDF5 format specification
		refSize := uint64(a.offsetSize + 8)

		// CVE-2025-6269 fix: Check for multiplication overflow before processing.
		totalBytes, err := utils.SafeMultiply(totalElements, refSize)
		if err != nil {
			return nil, fmt.Errorf("attribute size overflow (vlen string): %w", err)
		}

		if totalBytes > uint64(len(a.Data)) {
			return nil, fmt.Errorf("attribute data size mismatch for vlen strings: need %d bytes, have %d",
				totalBytes, len(a.Data))
		}

		values := make([]string, totalElements)
		for i := uint64(0); i < totalElements; i++ {
			offset := i * refSize
			elementData := a.Data[offset : offset+refSize]

			str, err := a.readVariableLengthString(elementData)
			if err != nil {
				return nil, fmt.Errorf("failed to read variable-length string element %d: %w", i, err)
			}
			values[i] = str
		}

		if isScalar {
			return values[0], nil
		}
		return values, nil
	}

	return nil, fmt.Errorf("unsupported datatype class %d or size %d: %w", a.Datatype.Class, a.Datatype.Size, utils.ErrNotSupported)
}

// readVariableLengthString reads a variable-length string from the Global Heap.
//
// For variable-length strings in attributes, the format is:
//   - Length (4 bytes): Number of bytes in the string
//   - Heap Address (offsetSize bytes): Address of the Global Heap collection
//   - Object Index (4 bytes): Index of the object within the collection
//
// Reference: HDF5 Format Specification III.E (Global Heap), H5Tvlen.c.
func (a *Attribute) readVariableLengthString(data []byte) (string, error) {
	// Expected size: 4 (length) + offsetSize (heap address) + 4 (object index)
	expectedSize := 4 + a.offsetSize + 4
	if len(data) < expectedSize {
		return "", fmt.Errorf("vlen string data too short: got %d bytes, need %d", len(data), expectedSize)
	}

	// Parse the length field (4 bytes, little-endian).
	// This is the length of the string in bytes.
	_ = binary.LittleEndian.Uint32(data[0:4]) // length, not used directly but verified via heap object

	// Parse the global heap reference (after the length field).
	ref, err := ParseGlobalHeapReference(data[4:], a.offsetSize)
	if err != nil {
		return "", fmt.Errorf("failed to parse global heap reference: %w", err)
	}

	// Check for null reference (address 0 typically means empty/null string).
	if ref.HeapAddress == 0 {
		return "", nil
	}

	// Read the global heap collection.
	collection, err := ReadGlobalHeapCollection(a.reader, ref.HeapAddress, a.offsetSize)
	if err != nil {
		return "", fmt.Errorf("failed to read global heap collection at 0x%X: %w", ref.HeapAddress, err)
	}

	// Get the object from the collection.
	obj, err := collection.GetObject(ref.ObjectIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get object %d from heap collection: %w", ref.ObjectIndex, err)
	}

	// Convert object data to string.
	// Variable-length strings are typically null-terminated.
	str := string(obj.Data)

	// Remove trailing null bytes if present.
	for str != "" && str[len(str)-1] == 0 {
		str = str[:len(str)-1]
	}

	return str, nil
}

// ParseAttributesFromMessages extracts all attributes from object header messages.
// Supports both compact attributes (stored in object header) and dense attributes
// (stored in fractal heap).
// Reference: H5Adense.c in C library.
func ParseAttributesFromMessages(r io.ReaderAt, messages []*HeaderMessage, sb *Superblock) ([]*Attribute, error) {
	var attributes []*Attribute
	var attrInfo *AttributeInfoMessage

	// First pass: the attribute info message (dense storage). C fails the
	// attribute iteration when it cannot decode it (H5A__get_ainfo,
	// H5Aint.c:2009-2010), so a bad one is an error, not compact-only
	// attributes.
	if msg := attributeInfoMessage(messages); msg != nil {
		var err error
		attrInfo, err = ParseAttributeInfoMessage(msg.Data, sb)
		if err != nil {
			return nil, fmt.Errorf("%w: attribute info message at 0x%x: %w", utils.ErrCorrupt, msg.Offset, err)
		}
		if msg.Type == MsgSharedMessageTable {
			sb.Warnf(WarnLegacyDenseLayout, msg.Offset, legacyDenseLayoutMessage,
				"attribute info message", msg.Offset, "message ID 0x000F instead of 0x0015")
		}
	}

	// Second pass: collect compact attributes
	for _, msg := range messages {
		if msg.Type != MsgAttribute {
			continue
		}
		attr, err := parseCompactAttribute(r, sb, msg)
		if IsSOHMNotSupported(err) {
			// Valid HDF5 the library cannot read yet: skip this attribute and
			// keep the others, as the dense path does.
			continue
		}
		if err != nil {
			// C fails the iteration when H5O__attr_decode fails
			// (H5Oattr.c:113-300); a corrupt attribute is not skipped silently.
			return nil, fmt.Errorf("attribute message at 0x%x: %w", msg.Offset, err)
		}
		attributes = append(attributes, attr)
	}

	// If dense storage exists, read attributes from fractal heap
	// Dense storage exists when the heap address is defined
	// (H5Oattribute.c:315); an object that tracks creation order keeps an
	// attribute info message with an undefined heap while its attributes are
	// compact.
	if attrInfo != nil && attrInfo.HasDenseStorage(sb.OffsetSize) {
		denseAttrs, err := readDenseAttributes(r, attrInfo, sb)
		if err != nil {
			return nil, fmt.Errorf("failed to read dense attributes: %w", err)
		}
		attributes = append(attributes, denseAttrs...)
	}

	return attributes, nil
}

// attributeInfoMessage returns the attribute info message of an object
// header: the one with the C message ID (H5O_AINFO_ID, H5Oprivate.h:228), or
// else one with ID 0x000F, the legacy layout of this library up to v0.14.2,
// which wrote its attribute info messages under the shared message table ID
// (H5O_SHMESG_ID, H5Oprivate.h:222). C writes that ID only in the superblock
// extension's header, which is read without attributes (shared_message.go),
// so in an object's header it can only be the legacy message.
func attributeInfoMessage(messages []*HeaderMessage) *HeaderMessage {
	var legacy *HeaderMessage
	for _, msg := range messages {
		switch msg.Type {
		case MsgAttributeInfo:
			return msg
		case MsgSharedMessageTable:
			if legacy == nil {
				legacy = msg
			}
		}
	}

	return legacy
}

// IsAttributeInfoMessage reports whether a message of an object header is its
// attribute info message: the C ID, or the legacy ID this library wrote up to
// v0.14.2 (attributeInfoMessage).
func IsAttributeInfoMessage(t MessageType) bool {
	return t == MsgAttributeInfo || t == MsgSharedMessageTable
}

// parseCompactAttribute decodes an attribute message stored in an object
// header. The message is sharable itself (H5Oattr.c includes H5Oshared.h); C
// shares it only in the shared message heap.
func parseCompactAttribute(r io.ReaderAt, sb *Superblock, msg *HeaderMessage) (*Attribute, error) {
	body, err := messageBody(r, sb, msg)
	if err != nil {
		return nil, err
	}

	attr, err := parseAttributeMessage(body, sb.Endianness, fileResolver(r, sb))
	if err != nil {
		return nil, err
	}
	if IsEarlyHDF5GoDatatype(attr.Datatype) {
		sb.Warnf(WarnLegacyDatatype, msg.Offset, legacyDatatypeMessage, "attribute message", msg.Offset)
	}

	// The reader resolves variable-length values in the global heap.
	attr.reader = r
	attr.offsetSize = int(sb.OffsetSize)

	return attr, nil
}

// Helper functions for float conversion (copied from dataset_reader.go to avoid circular import).
func float32frombits(b uint32) float32 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float32 bit representation
	return *(*float32)(unsafe.Pointer(&b))
}

func float64frombits(b uint64) float64 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float64 bit representation
	return *(*float64)(unsafe.Pointer(&b))
}

// readDenseAttributes reads attributes from dense storage: the attribute
// name index (a v2 B-tree, H5Abtree2.c) and the fractal heap that holds the
// attribute messages, walked by readDenseHeapObjects (H5A__dense_iterate,
// H5Adense.c).
func readDenseAttributes(r io.ReaderAt, attrInfo *AttributeInfoMessage, sb *Superblock) ([]*Attribute, error) {
	if attrInfo == nil || sb == nil {
		return nil, fmt.Errorf("attribute info or superblock is nil")
	}

	if attrInfo.FractalHeapAddr == 0 || attrInfo.BTreeNameIndexAddr == 0 {
		return nil, fmt.Errorf("invalid dense attribute addresses (heap=0x%X, btree=0x%X)",
			attrInfo.FractalHeapAddr, attrInfo.BTreeNameIndexAddr)
	}

	objects, err := readDenseHeapObjects(r, sb, attrInfo.BTreeNameIndexAddr, attrInfo.FractalHeapAddr, denseAttributes)
	if err != nil {
		return nil, fmt.Errorf("dense attribute storage: %w", err)
	}

	attributes := make([]*Attribute, 0, len(objects))
	for i, objectData := range objects {
		// Parse as attribute message (version 3 for dense attributes)
		attr, err := parseAttributeMessage(objectData, sb.Endianness, fileResolver(r, sb))
		if IsSOHMNotSupported(err) {
			// An attribute typed by a message in the shared message heap is
			// valid HDF5 the library cannot read yet: skip it as the compact
			// path does, keep the others.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse attribute %d: %w", i, err)
		}
		if IsEarlyHDF5GoDatatype(attr.Datatype) {
			sb.Warnf(WarnLegacyDatatype, attrInfo.FractalHeapAddr, legacyDatatypeMessage,
				"an attribute in the dense attribute heap", attrInfo.FractalHeapAddr)
		}

		// Set reader for variable-length type resolution
		attr.reader = r
		attr.offsetSize = int(sb.OffsetSize)
		attributes = append(attributes, attr)
	}

	return attributes, nil
}

// EncodeAttributeFromStruct encodes an Attribute struct to bytes (attribute message format).
//
// This is a convenience wrapper around EncodeAttributeMessage (from messages_write.go)
// that takes an Attribute struct instead of individual parameters.
//
// Parameters:
//   - attr: Attribute to encode
//   - sb: Superblock for endianness (currently unused as we use little-endian)
//
// Returns:
//   - []byte: Encoded attribute message
//   - error: Non-nil if encoding fails
//
// Reference: H5Oattr.c - H5O__attr_encode().
func EncodeAttributeFromStruct(attr *Attribute, sb *Superblock) ([]byte, error) {
	if attr == nil {
		return nil, fmt.Errorf("attribute is nil")
	}

	if attr.Name == "" {
		return nil, fmt.Errorf("attribute name cannot be empty")
	}

	if attr.Datatype == nil {
		return nil, fmt.Errorf("attribute datatype is nil")
	}

	if attr.Dataspace == nil {
		return nil, fmt.Errorf("attribute dataspace is nil")
	}

	// Use the existing EncodeAttributeMessage from messages_write.go
	// It handles all the encoding logic according to HDF5 spec
	_ = sb // Currently unused, encoding is always little-endian

	return EncodeAttributeMessage(attr.Name, attr.Datatype, attr.Dataspace, attr.Data)
}

// Attribute info message version and flags (H5Oainfo.c:73-78).
const (
	attrInfoVersion     = 0
	attrInfoTrackCorder = 0x01
	attrInfoIndexCorder = 0x02
	attrInfoAllFlags    = attrInfoTrackCorder | attrInfoIndexCorder
)

// ParseAttributeInfoMessage parses an Attribute Info Message (0x0015).
// Reference: H5Oainfo.c in C library.
// Format:
// - Version (1 byte)
// - Flags (1 byte):
//   - Bit 0: Track creation order
//   - Bit 1: Index creation order
//
// - Max Creation Index (2 bytes) - only if track creation order flag set
// - Fractal Heap Address (variable, based on superblock offset size)
// - B-tree Name Index Address (variable)
// - B-tree Order Index Address (variable) - only if index creation order flag set.
func ParseAttributeInfoMessage(data []byte, sb *Superblock) (*AttributeInfoMessage, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("attribute info message too short: %d bytes", len(data))
	}

	msg := &AttributeInfoMessage{}
	offset := 0

	// Version and flags, checked as H5O__ainfo_decode does (H5Oainfo.c:112-124).
	msg.Version = data[offset]
	offset++
	if msg.Version != attrInfoVersion {
		return nil, fmt.Errorf("attribute info message version %d, want %d", msg.Version, attrInfoVersion)
	}

	msg.Flags = data[offset]
	offset++
	if msg.Flags&^attrInfoAllFlags != 0 {
		return nil, fmt.Errorf("attribute info message flags 0x%02x, unknown bits", msg.Flags)
	}

	trackCreationOrder := (msg.Flags & attrInfoTrackCorder) != 0
	indexCreationOrder := (msg.Flags & attrInfoIndexCorder) != 0

	// Maximum creation index (2 bytes), only if creation order is tracked
	// (H5Oainfo.c:131-135). Up to TASK-058 this decoder also skipped a 2-byte
	// "min dense" field that the format does not have, reading every address
	// after it 2 bytes late.
	if trackCreationOrder {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("attribute info message too short for max creation index")
		}
		msg.MaxCreationIndex = uint64(sb.Endianness.Uint16(data[offset : offset+2]))
		offset += 2
	}

	// Fractal Heap Address (offset size bytes)
	offsetSize := int(sb.OffsetSize)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for fractal heap address")
	}
	msg.FractalHeapAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Name Index Address (offset size bytes)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for btree name index address")
	}
	msg.BTreeNameIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Order Index Address (offset size bytes) - only if indexed
	if indexCreationOrder {
		if offset+offsetSize > len(data) {
			return nil, fmt.Errorf("attribute info message too short for btree order index address")
		}
		msg.BTreeOrderIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	}

	return msg, nil
}

// EncodeAttributeInfoMessage encodes Attribute Info Message for writing.
//
// This implements the encoding logic matching the C reference H5Oainfo.c:H5O__ainfo_encode().
//
// Format:
// - Version (1 byte): 0
// - Flags (1 byte): creation order tracking/indexing
// - Max Creation Index (2 bytes): Optional, if bit 0 of flags set
// - Fractal Heap Address (8 bytes)
// - B-tree Name Index Address (8 bytes)
// - B-tree Order Index Address (8 bytes): Optional, if bit 1 of flags set
//
// Parameters:
//   - aim: AttributeInfoMessage to encode
//   - sb: Superblock for endianness and offset size
//
// Returns:
//   - []byte: Encoded message
//   - error: Non-nil if encoding fails
//
// Reference: H5Oainfo.c - H5O__ainfo_encode().
func EncodeAttributeInfoMessage(aim *AttributeInfoMessage, sb *Superblock) ([]byte, error) {
	if aim == nil {
		return nil, fmt.Errorf("attribute info message is nil")
	}

	if aim.Flags&^attrInfoAllFlags != 0 {
		return nil, fmt.Errorf("attribute info message flags 0x%02x, unknown bits", aim.Flags)
	}
	if aim.MaxCreationIndex > math.MaxUint16 {
		return nil, fmt.Errorf("attribute info message: max creation index %d does not fit 2 bytes", aim.MaxCreationIndex)
	}

	// Calculate size
	size := 2 // Version (1) + Flags (1)

	trackCreationOrder := (aim.Flags & attrInfoTrackCorder) != 0
	indexCreationOrder := (aim.Flags & attrInfoIndexCorder) != 0

	// Max creation index (2 bytes) if creation order tracked (H5Oainfo.c:206-208)
	if trackCreationOrder {
		size += 2
	}

	// Heap address (offsetSize bytes)
	offsetSize := int(sb.OffsetSize)
	size += offsetSize

	// B-tree name index address (offsetSize bytes)
	size += offsetSize

	// B-tree order index address (offsetSize bytes) if indexed
	if indexCreationOrder {
		size += offsetSize
	}

	buf := make([]byte, size)
	offset := 0

	// Version
	buf[offset] = aim.Version
	offset++

	// Flags
	buf[offset] = aim.Flags
	offset++

	// Max creation index if creation order tracked
	if trackCreationOrder {
		sb.Endianness.PutUint16(buf[offset:], uint16(aim.MaxCreationIndex)) // In range: checked above.
		offset += 2
	}

	// Fractal Heap Address
	writeAddress(buf[offset:], aim.FractalHeapAddr, offsetSize, sb.Endianness)
	offset += offsetSize

	// B-tree Name Index Address
	writeAddress(buf[offset:], aim.BTreeNameIndexAddr, offsetSize, sb.Endianness)
	offset += offsetSize

	// B-tree Order Index Address if indexed
	if indexCreationOrder {
		writeAddress(buf[offset:], aim.BTreeOrderIndexAddr, offsetSize, sb.Endianness)
	}

	return buf, nil
}

// writeAddress writes an address to buffer with specified size and endianness.
// Helper function for encoding variable-size addresses.
func writeAddress(buf []byte, addr uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(addr) //nolint:gosec // G115: variable-size address encoding
	case 2:
		endianness.PutUint16(buf, uint16(addr)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(addr)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, addr)
	}
}
