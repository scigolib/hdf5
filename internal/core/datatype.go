package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// DatatypeClass represents HDF5 datatype class.
type DatatypeClass uint8

// Datatype class constants identify different HDF5 data types for datasets.
const (
	DatatypeFixed     DatatypeClass = 0  // Fixed-point (integers).
	DatatypeFloat     DatatypeClass = 1  // Floating-point.
	DatatypeTime      DatatypeClass = 2  // Time.
	DatatypeString    DatatypeClass = 3  // String.
	DatatypeBitfield  DatatypeClass = 4  // Bitfield.
	DatatypeOpaque    DatatypeClass = 5  // Opaque.
	DatatypeCompound  DatatypeClass = 6  // Compound.
	DatatypeReference DatatypeClass = 7  // Reference.
	DatatypeEnum      DatatypeClass = 8  // Enumerated.
	DatatypeVarLen    DatatypeClass = 9  // Variable-length.
	DatatypeArray     DatatypeClass = 10 // Array.
	DatatypeComplex   DatatypeClass = 11 // Complex (HDF5 2.0+).
)

// DatatypeMessage represents an HDF5 datatype message (format spec IV.A.2.d),
// the file form of the C library's H5T_t.
//
// Class, Version, Size and ClassBitField are the message header. Properties
// holds exactly the class-specific property bytes that follow the header in
// the file, nested datatypes included; [DecodeDatatype] sets it to the bytes
// it consumed, never to "the rest of the buffer".
//
// The fields after Properties are the decoded properties of the class (the
// union in H5T_shared_t). [DecodeDatatype] fills the ones of the message's
// class, and [EncodeDatatypeMessage] writes the message from them, never from
// Properties. Build new datatypes with the constructors in datatype_codec.go
// (NewFixedPointDatatype, NewCompoundDatatype, ...), which return messages
// in exactly the form the decoder produces.
type DatatypeMessage struct {
	Class         DatatypeClass
	Version       uint8
	Size          uint32
	ClassBitField uint32
	Properties    []byte

	// BitOffset and Precision locate the significant bits of a fixed-point,
	// floating-point or bitfield value (H5T_atomic_t offset, prec); Precision
	// is also the time precision. Zero Precision means "Size*8 bits at offset
	// 0" to the encoder, as zero is never a valid stored precision.
	BitOffset uint16
	Precision uint16

	// Floating-point layout (H5T_atomic_t.u.f): bit positions and sizes of
	// exponent and mantissa, and the exponent bias. The sign bit position and
	// normalization live in ClassBitField (bits 8-15 and 4-5). When both sizes
	// are zero the encoder writes the IEEE 754 layout for Size 2, 4 or 8.
	ExponentLocation uint8
	ExponentSize     uint8
	MantissaLocation uint8
	MantissaSize     uint8
	ExponentBias     uint32

	// OpaqueTag is the ASCII tag of an opaque datatype.
	OpaqueTag string

	// Members are the fields of a compound datatype, in file order.
	Members []CompoundMember

	// EnumNames and EnumValues are the members of an enumeration: the names
	// in order and their values packed in the base type's representation,
	// len(EnumNames)*Base.Size bytes.
	EnumNames  []string
	EnumValues []byte

	// ArrayDims are the dimensions of an array datatype.
	ArrayDims []uint32

	// Base is the parent datatype of an enumeration, variable-length
	// sequence or string, array or complex number.
	Base *DatatypeMessage
}

// ParseDatatypeMessage decodes the datatype message at the start of data.
// Bytes after the message are ignored, as H5O__dtype_decode does with the
// remainder of an object header message. Errors on invalid bytes wrap
// utils.ErrCorrupt.
func ParseDatatypeMessage(data []byte) (*DatatypeMessage, error) {
	dt, _, err := DecodeDatatype(data)
	if err != nil {
		return nil, err
	}
	return dt, nil
}

// MsgFlagShared is the "shared" object header message flag,
// H5O_MSG_FLAG_SHARED (H5Oprivate.h:74): the message body is a reference to
// a shared message (a committed datatype, H5Oshared.h), not the message.
const MsgFlagShared uint8 = 0x02

// attrFlagTypeShared is H5O_ATTR_FLAG_TYPE_SHARED (H5Oattr.c:88): the
// attribute message flag that marks its datatype as shared.
const attrFlagTypeShared = 0x01

// attrFlagSpaceShared is H5O_ATTR_FLAG_SPACE_SHARED (H5Oattr.c:89): the
// attribute's dataspace field is a shared-message reference.
const attrFlagSpaceShared = 0x02

// errSharedDatatype reports a datatype message that refers to a committed
// datatype. Such files are valid HDF5; the library does not resolve the
// reference on this path (no file to read), so it wraps utils.ErrNotSupported,
// deliberately not utils.ErrCorrupt.
var errSharedDatatype = fmt.Errorf("committed (shared) datatype not supported yet: %w", utils.ErrNotSupported)

// ParseDatatypeHeaderMessage decodes the datatype message msg of an object
// header. A shared message (a committed datatype) is reported with an error
// that does not wrap utils.ErrCorrupt; every other message decodes as with
// ParseDatatypeMessage.
func ParseDatatypeHeaderMessage(msg *HeaderMessage) (*DatatypeMessage, error) {
	if msg.Flags&MsgFlagShared != 0 {
		return nil, fmt.Errorf("datatype message at 0x%x: %w", msg.Offset, errSharedDatatype)
	}
	dt, err := ParseDatatypeMessage(msg.Data)
	if err != nil {
		return nil, fmt.Errorf("datatype message at 0x%x: %w", msg.Offset, err)
	}
	return dt, nil
}

// ResolveDatatypeHeaderMessage decodes the datatype message msg of an object
// header in the file r. A shared message (a committed datatype) is followed to
// the committed datatype's object header and its datatype message is decoded
// there (H5O__shared_decode, H5Oshared.c:289; H5O__shared_read,
// H5Oshared.c:95), so the caller gets a plain datatype. Errors on invalid bytes
// wrap utils.ErrCorrupt; a datatype in the shared message heap is reported
// with an error IsSOHMNotSupported recognizes.
//
// A committed datatype is decoded once per file and shared by every object
// that refers to it (resolveSharedDatatype); callers must not modify it.
func ResolveDatatypeHeaderMessage(r io.ReaderAt, sb *Superblock, msg *HeaderMessage) (*DatatypeMessage, error) {
	if msg.Flags&MsgFlagShared != 0 {
		dt, err := resolveSharedDatatype(r, sb, msg.Data)
		if err != nil {
			return nil, fmt.Errorf("datatype message: shared message at 0x%x: %w", msg.Offset, err)
		}
		return dt, nil
	}

	dt, err := ParseDatatypeMessage(msg.Data)
	if err != nil {
		return nil, fmt.Errorf("datatype message at 0x%x: %w", msg.Offset, err)
	}

	return dt, nil
}

// IsFloat64 checks if datatype is IEEE 754 double precision (64-bit).
func (dt *DatatypeMessage) IsFloat64() bool {
	return dt.Class == DatatypeFloat && dt.Size == 8
}

// IsFloat32 checks if datatype is IEEE 754 single precision (32-bit).
func (dt *DatatypeMessage) IsFloat32() bool {
	return dt.Class == DatatypeFloat && dt.Size == 4
}

// IsInt32 checks if datatype is 32-bit signed integer.
func (dt *DatatypeMessage) IsInt32() bool {
	return dt.Class == DatatypeFixed && dt.Size == 4
}

// IsInt64 checks if datatype is 64-bit signed integer.
func (dt *DatatypeMessage) IsInt64() bool {
	return dt.Class == DatatypeFixed && dt.Size == 8
}

// IsFixedPoint reports whether the datatype is an integer (signed or
// unsigned, any width). Use [DatatypeMessage.IsSignedFixedPoint] to
// distinguish signedness; use [DatatypeMessage.Size] for width.
func (dt *DatatypeMessage) IsFixedPoint() bool {
	return dt.Class == DatatypeFixed
}

// IsSignedFixedPoint reports whether a fixed-point datatype is signed
// (two's-complement). Per HDF5 spec III.A.1.b the sign bit lives at bit 3
// of the class bit field. Undefined for non-fixed-point types.
func (dt *DatatypeMessage) IsSignedFixedPoint() bool {
	return dt.Class == DatatypeFixed && dt.ClassBitField&0x08 != 0
}

// IsString checks if datatype is a string type.
func (dt *DatatypeMessage) IsString() bool {
	return dt.Class == DatatypeString
}

// IsFixedString checks if datatype is a fixed-length string.
func (dt *DatatypeMessage) IsFixedString() bool {
	if dt.Class != DatatypeString {
		return false
	}
	// Fixed-length strings have explicit Size.
	// Variable-length strings would have Size = 0 or use DatatypeVarLen class.
	return dt.Size > 0
}

// IsVariableString checks if datatype is a variable-length string.
// Reference: HDF5 Format Specification III.A.2.4.d (Variable-Length Types).
func (dt *DatatypeMessage) IsVariableString() bool {
	if dt.Class == DatatypeVarLen {
		// For variable-length types, ClassBitField contains:
		// - Bits 0-3: Type (0=Sequence, 1=String)
		// - Bits 4-7: Padding type (for strings)
		// - Bits 8-11: Character set (for strings, 0=ASCII, 1=UTF-8)
		vlType := dt.ClassBitField & 0x0F
		return vlType == 1 // 1 = variable-length string
	}
	return false
}

// IsCompound checks if datatype is a compound type (struct).
func (dt *DatatypeMessage) IsCompound() bool {
	return dt.Class == DatatypeCompound
}

// GetStringPadding returns the string padding type.
// 0 = null-terminated, 1 = null-padded, 2 = space-padded.
func (dt *DatatypeMessage) GetStringPadding() uint8 {
	return uint8(dt.ClassBitField & 0x0F)
}

// String returns human-readable datatype description.
func (dt *DatatypeMessage) String() string {
	var className string
	switch dt.Class {
	case DatatypeFixed:
		className = "integer"
	case DatatypeFloat:
		className = "float"
	case DatatypeString:
		className = "string"
	case DatatypeCompound:
		className = "compound"
	case DatatypeArray:
		className = "array"
	default:
		className = fmt.Sprintf("class_%d", dt.Class)
	}

	return fmt.Sprintf("%s (size=%d bytes)", className, dt.Size)
}

// GetByteOrder returns byte order for numeric types.
func (dt *DatatypeMessage) GetByteOrder() binary.ByteOrder {
	// Bit 0 of class bit field indicates byte order for numeric types.
	// 0 = little-endian, 1 = big-endian.
	if dt.ClassBitField&0x01 == 0 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// GetEncodedSize returns the size of this datatype message in the file: the
// 8-byte header plus the property bytes. The fixed property sizes are those of
// H5O__dtype_size (H5Odtype.c:1636-1655); for every other class the size is
// that of Properties (empty for decoded strings and references,
// H5Odtype.c:1732-1738).
func (dt *DatatypeMessage) GetEncodedSize() int {
	switch dt.Class {
	case DatatypeFixed, DatatypeBitfield:
		return datatypeHeaderSize + 4
	case DatatypeFloat:
		return datatypeHeaderSize + 12
	case DatatypeTime:
		return datatypeHeaderSize + 2
	default:
		return datatypeHeaderSize + len(dt.Properties)
	}
}

// IsSharedDatatype reports whether err says that a datatype is a committed
// (shared) datatype, which the library cannot resolve yet.
func IsSharedDatatype(err error) bool {
	return errors.Is(err, errSharedDatatype)
}
