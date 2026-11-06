package core

import (
	"errors"
	"fmt"
)

// CompoundMember represents a single field in a compound datatype.
type CompoundMember struct {
	Name   string           // Field name.
	Offset uint32           // Byte offset within the compound structure.
	Type   *DatatypeMessage // Member datatype (can be any type, including nested compound).
}

// CompoundType represents a parsed compound datatype with all its members.
type CompoundType struct {
	Size    uint32           // Total size of the compound structure in bytes.
	Members []CompoundMember // List of members/fields.
}

// ParseCompoundType returns the members of a compound datatype. A message from
// [DecodeDatatype] already carries them; a message assembled by hand from a
// header and Properties is decoded first, by the same decoder (all versions:
// member count in the class bit field, H5Odtype.c:358).
func ParseCompoundType(dt *DatatypeMessage) (*CompoundType, error) {
	if dt == nil || dt.Class != DatatypeCompound {
		return nil, errors.New("not a compound datatype")
	}
	members := dt.Members
	if members == nil {
		full, err := redecode(dt)
		if err != nil {
			return nil, err
		}
		members = full.Members
	}
	return &CompoundType{
		Size:    dt.Size,
		Members: append([]CompoundMember(nil), members...),
	}, nil
}

// String returns human-readable compound type description.
func (ct *CompoundType) String() string {
	result := fmt.Sprintf("compound{size=%d, members=[", ct.Size)
	for i, member := range ct.Members {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s:%s@%d", member.Name, member.Type.String(), member.Offset)
	}
	result += "]}"
	return result
}
