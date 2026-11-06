package core

import (
	"errors"
	"fmt"
)

// CompoundFieldDef defines a field for compound datatype creation.
// This is used when creating new compound datatypes for writing.
type CompoundFieldDef struct {
	Name   string           // Field name (null-terminated in encoding).
	Offset uint32           // Byte offset within compound structure.
	Type   *DatatypeMessage // Field datatype (can be nested compound).
}

// compoundMembers converts field definitions to compound members.
func compoundMembers(fields []CompoundFieldDef) []CompoundMember {
	members := make([]CompoundMember, len(fields))
	for i, f := range fields {
		members[i] = CompoundMember(f)
	}
	return members
}

// EncodeCompoundDatatypeV3 encodes a version 3 compound datatype message, the
// format this library writes: the member count in the class bit field, member
// names without padding and member offsets of H5VM_limit_enc_size(totalSize)
// bytes (H5Odtype.c:1238-1308). The encoding is done by the datatype codec,
// see [EncodeDatatypeMessage].
func EncodeCompoundDatatypeV3(totalSize uint32, fields []CompoundFieldDef) ([]byte, error) {
	return encodeCompoundFields(dtypeVersion3, totalSize, fields)
}

// EncodeCompoundDatatypeV1 encodes a version 1 compound datatype message: the
// member count in the class bit field, names padded to a multiple of eight
// bytes, 4-byte offsets and the 28-byte scalar array information per member
// (H5Odtype.c:1238-1308). The library itself writes version 3; version 1 is
// what the C library writes with its earliest format bound.
func EncodeCompoundDatatypeV1(totalSize uint32, fields []CompoundFieldDef) ([]byte, error) {
	return encodeCompoundFields(dtypeVersion1, totalSize, fields)
}

func encodeCompoundFields(version uint8, totalSize uint32, fields []CompoundFieldDef) ([]byte, error) {
	if len(fields) == 0 {
		return nil, errors.New("compound datatype must have at least one field")
	}
	if totalSize == 0 {
		return nil, errors.New("compound datatype size cannot be 0")
	}
	for i, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("field %d: name cannot be empty", i)
		}
		if field.Type == nil {
			return nil, fmt.Errorf("field %d (%s): type cannot be nil", i, field.Name)
		}
	}
	return EncodeDatatypeMessage(&DatatypeMessage{
		Class:   DatatypeCompound,
		Version: version,
		Size:    totalSize,
		Members: compoundMembers(fields),
	})
}

// CreateCompoundTypeFromFields creates a packed compound datatype: each field
// must start where the previous one ends, and the compound size is the sum of
// the field sizes. The result is a version 3 compound in decoded form (see
// [NewCompoundDatatype]).
func CreateCompoundTypeFromFields(fields []CompoundFieldDef) (*DatatypeMessage, error) {
	if len(fields) == 0 {
		return nil, errors.New("compound type must have at least one field")
	}

	var currentOffset uint64
	for i, field := range fields {
		if field.Type == nil {
			return nil, fmt.Errorf("field %d (%s): type cannot be nil", i, field.Name)
		}
		if uint64(field.Offset) != currentOffset {
			return nil, fmt.Errorf("field %d (%s): offset mismatch (expected %d, got %d)", i, field.Name, currentOffset, field.Offset)
		}
		currentOffset += uint64(field.Type.Size)
		if currentOffset > uint64(^uint32(0)) {
			return nil, fmt.Errorf("field %d (%s): compound size overflows 32 bits", i, field.Name)
		}
	}

	dt, err := NewCompoundDatatype(uint32(currentOffset), compoundMembers(fields))
	if err != nil {
		return nil, fmt.Errorf("failed to encode compound type: %w", err)
	}
	return dt, nil
}

// CreateBasicDatatypeMessage creates a little-endian member type for a
// compound datatype: an unsigned integer (DatatypeFixed, as before: the class
// bit field carries no sign flag), an IEEE 754 float of 2, 4 or 8 bytes, or a
// null-terminated ASCII string. The properties are those the C library writes
// for H5T_STD_U<n>LE, H5T_IEEE_F<n>LE and H5T_C_S1 (offset and precision for
// integers, the full IEEE layout for floats, none for strings: H5Odtype.c
// :1019-1020, :1114-1125, :1158-1159).
func CreateBasicDatatypeMessage(class DatatypeClass, size uint32) (*DatatypeMessage, error) {
	switch class {
	case DatatypeFixed:
		return NewIntegerDatatype(size, false)
	case DatatypeFloat:
		return NewFloatDatatype(size)
	case DatatypeString:
		return NewStringDatatype(size, StringNullTerm, CharsetASCII)
	default:
		return nil, fmt.Errorf("unsupported datatype class: %d", class)
	}
}
