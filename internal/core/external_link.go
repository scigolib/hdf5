package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// External link user data (H5Ldevelop.h:36-41): the first byte holds the
// version in its high nibble and the flags in its low nibble.
const (
	externalLinkVersion  = 0 // H5L_EXT_VERSION
	externalLinkFlagsAll = 0 // H5L_EXT_FLAGS_ALL
)

// legacyExternalLinkMessage is the warning text for an external link in the
// layout hdf5-go v0.14.2 and earlier wrote; its argument is the address.
const legacyExternalLinkMessage = "external link message at 0x%X is in the layout hdf5-go v0.14.2 and earlier wrote " +
	"(file and object path each behind a 16-bit length, in an object header of its own); the HDF5 C library " +
	"cannot read the file. Recreate the link with a current release"

// EncodeExternalLinkValue returns the link value of an external link to the
// object objectPath in the file fileName as the link message stores it
// (H5O__link_encode, H5Olink.c:384-391): a 16-bit length, then the user data
// H5Lcreate_external builds (H5L.c:644-656): one byte of version and flags,
// the file name and the object path, each NUL-terminated.
func EncodeExternalLinkValue(fileName, objectPath string) ([]byte, error) {
	if bytes.IndexByte([]byte(fileName), 0) >= 0 || bytes.IndexByte([]byte(objectPath), 0) >= 0 {
		return nil, fmt.Errorf("external link: a NUL byte in the file name or the object path")
	}
	n := 1 + len(fileName) + 1 + len(objectPath) + 1
	if n > math.MaxUint16 {
		return nil, fmt.Errorf("external link value of %d bytes exceeds the 16-bit length field", n)
	}
	v := binary.LittleEndian.AppendUint16(make([]byte, 0, 2+n), uint16(n))
	v = append(v, externalLinkVersion<<4|externalLinkFlagsAll)
	v = append(v, fileName...)
	v = append(v, 0)
	v = append(v, objectPath...)
	return append(v, 0), nil
}

// ExternalLinkValue is the decoded value of an external link message.
type ExternalLinkValue struct {
	FileName   string
	ObjectPath string
	Size       int  // bytes of the message data the value takes, its length field included
	Legacy     bool // the layout hdf5-go v0.14.2 and earlier wrote
}

// DecodeExternalLinkValue decodes the value of an external link message, v
// from its 16-bit length field to the end of the message data.
//
// The C layout (EncodeExternalLinkValue) is taken when the first byte of the
// user data has version 0 and no flags (H5Lexternal.c:125-128) and two
// NUL-terminated strings fill the user data exactly. Otherwise the layout
// hdf5-go v0.14.2 and earlier wrote is taken (file name length, file name,
// path length, path, each length 16 bits) when its two lengths account for
// all of v. Anything else is utils.ErrCorrupt.
func DecodeExternalLinkValue(v []byte) (ExternalLinkValue, error) {
	if len(v) < 2 {
		return ExternalLinkValue{}, fmt.Errorf("%w: external link value of %d bytes", utils.ErrCorrupt, len(v))
	}
	size := int(binary.LittleEndian.Uint16(v))
	if 2+size <= len(v) && size >= 3 && v[2]>>4 == externalLinkVersion && v[2]&0x0F&^externalLinkFlagsAll == 0 {
		udata := v[3 : 2+size]
		if i := bytes.IndexByte(udata, 0); i >= 0 {
			rest := udata[i+1:]
			if j := bytes.IndexByte(rest, 0); j >= 0 && j == len(rest)-1 {
				return ExternalLinkValue{FileName: string(udata[:i]), ObjectPath: string(rest[:j]), Size: 2 + size}, nil
			}
		}
	}
	fileLen := size
	if 2+fileLen+2 <= len(v) {
		pathLen := int(binary.LittleEndian.Uint16(v[2+fileLen:]))
		if 2+fileLen+2+pathLen == len(v) {
			return ExternalLinkValue{
				FileName: string(v[2 : 2+fileLen]), ObjectPath: string(v[4+fileLen:]), Size: len(v), Legacy: true,
			}, nil
		}
	}
	return ExternalLinkValue{}, fmt.Errorf("%w: external link value of %d bytes is in no known layout",
		utils.ErrCorrupt, len(v))
}

// ParseLinkMessageAt is ParseLinkMessage for the link message at addr: an
// external link in the legacy layout is recorded as WarnLegacyExternalLink.
func ParseLinkMessageAt(data []byte, sb *Superblock, addr uint64) (*LinkMessage, error) {
	lm, err := ParseLinkMessage(data, sb)
	if err != nil {
		return nil, err
	}
	if lm.Type == LinkTypeExternal {
		// ParseLinkMessage decoded this value already, so it cannot fail here.
		if ext, err := DecodeExternalLinkValue(lm.LinkValue); err == nil && ext.Legacy {
			sb.Warnf(WarnLegacyExternalLink, addr, legacyExternalLinkMessage, addr)
		}
	}
	return lm, nil
}
