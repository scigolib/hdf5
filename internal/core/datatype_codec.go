package core

// The datatype codec: one decoder and one encoder for the datatype message,
// used for top-level messages and for every nested datatype (compound member,
// enumeration, variable-length, array and complex base types). They mirror
// H5O__dtype_decode_helper (H5Odtype.c:120-916) and H5O__dtype_encode_helper
// (H5Odtype.c:932-1446) of the HDF5 C library; line numbers refer to
// hdf5c/src at the commit recorded in UPSTREAM.md.
//
// Every decode error reports bytes the C library would reject too and wraps
// utils.ErrCorrupt. The encoder decodes what it produced before returning it,
// so the writer can never emit a datatype the decoder (and therefore the C
// library) refuses.
//
// Deliberate deviations from the C decoder:
//   - H5T_is_numeric_with_unusual_unused_bits (H5Odtype.c:894-905) is not
//     applied. C applies it only to messages in object headers without a
//     checksum and lets H5Pset_relax_file_integrity_checks disable it; this
//     decoder has no header context, and an unusual number of padding bits
//     never makes a read go out of bounds.
//   - A version-1 compound member with array dimensions must fit in the
//     compound with its full array size; C checks only the base type's size
//     against the compound (H5Odtype.c:509-515) before building the array.
//   - Two byte patterns only hdf5-go before v0.14.3 wrote, and which no valid
//     file contains, are read instead of rejected, so files users already
//     wrote stay readable: the old float attribute encoding (sign bit
//     position 0, rejected by C at H5Odtype.c:264-267; isLegacyHDF5GoFloat)
//     and the old compound encoding (version 3 with member count 0 in the
//     class bit field, rejected by C at H5Odtype.c:358-360;
//     decodeLegacyCompound). The writer never produces either.
//   - A shared datatype message (a committed datatype) is valid HDF5 that
//     this library cannot resolve yet; ParseDatatypeHeaderMessage reports it
//     without utils.ErrCorrupt.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strings"

	"github.com/scigolib/hdf5/internal/utils"
)

// Datatype message versions (H5Tpkg.h:78-106).
const (
	dtypeVersion1      uint8 = 1 // original encoding
	dtypeVersion2      uint8 = 2 // array class (H5T_ARRAY needs at least this)
	dtypeVersion3      uint8 = 3 // unpadded names, minimal compound offsets, VAX order
	dtypeVersion5      uint8 = 5 // complex number class
	dtypeVersionLatest       = dtypeVersion5
)

const (
	// datatypeHeaderSize is the class/version/flags word plus the size word.
	datatypeHeaderSize = 8

	// opaqueTagMax is H5T_OPAQUE_TAG_MAX (H5Tpublic.h:215).
	opaqueTagMax = 256

	// maxEncodableOpaqueTag is the longest tag H5O__dtype_encode_helper
	// stores without truncation: it writes (len+7) & (H5T_OPAQUE_TAG_MAX-8)
	// bytes (H5Odtype.c:1229), which drops the tag for lengths 249-255.
	maxEncodableOpaqueTag = opaqueTagMax - 8

	// arrayMaxRank is H5S_MAX_RANK (H5Spublic.h:55).
	arrayMaxRank = 32

	// compoundV1MaxDims is the number of dimension slots of a version-1
	// compound member (H5Odtype.c:459, :485-489).
	compoundV1MaxDims = 4

	// compoundV1ArrayInfoSize is the version-1 member array information:
	// dimensionality, 3 reserved, permutation, 4 reserved, 4 dimensions.
	compoundV1ArrayInfoSize = 1 + 3 + 4 + 4 + 4*compoundV1MaxDims

	// maxListedMembers is the largest member count of a compound or
	// enumeration: the count is the low 16 bits of the class bit field.
	maxListedMembers = 0xFFFF

	// referenceEncodeVersion is H5R_ENCODE_VERSION (H5Rprivate.h:27), stored
	// in bits 4-7 of the class bit field of the opaque reference types.
	referenceEncodeVersion = 1

	// Reference types (H5R_type_t, H5Rpublic.h:52-58).
	referenceObject1        = 0
	referenceDatasetRegion1 = 1
	referenceMaxType        = 5

	// Variable-length kinds (H5T_vlen_type_t), bits 0-3 of the class bits.
	vlenSequence = 0
	vlenString   = 1
)

// StringPadding is the padding of a fixed-length string (H5T_str_t), stored
// in bits 0-3 of the string class bit field.
type StringPadding uint8

// String padding values (H5Tpublic.h H5T_str_t).
const (
	StringNullTerm StringPadding = 0 // H5T_STR_NULLTERM
	StringNullPad  StringPadding = 1 // H5T_STR_NULLPAD
	StringSpacePad StringPadding = 2 // H5T_STR_SPACEPAD
)

// nibbleMask keeps the 4 bits a padding, character set or kind occupies in a
// class bit field.
const nibbleMask = 0x0F

// CharacterSet is the character set of a string (H5T_cset_t).
type CharacterSet uint8

// Character sets (H5Tpublic.h H5T_cset_t).
const (
	CharsetASCII CharacterSet = 0 // H5T_CSET_ASCII
	CharsetUTF8  CharacterSet = 1 // H5T_CSET_UTF8
)

// ReferenceType is the kind of a reference datatype (H5R_type_t).
type ReferenceType uint8

// Reference kinds this library writes (H5Rpublic.h:53-54).
const (
	ReferenceObject        ReferenceType = referenceObject1        // H5R_OBJECT1
	ReferenceDatasetRegion ReferenceType = referenceDatasetRegion1 // H5R_DATASET_REGION1
)

// Class bit field flags shared by the atomic classes (H5Odtype.c:171-174,
// :193-205, :303-305).
const (
	flagBigEndian = 0x01 // byte order: big-endian
	flagLSBPadOne = 0x02 // low padding bits are ones
	flagMSBPadOne = 0x04 // high padding bits are ones
	flagSigned    = 0x08 // fixed-point: two's complement; float: internal padding is ones
	flagFloatVAX  = 0x40 // float, version >= 3: VAX byte order (with flagBigEndian)

	floatNormShift   = 4
	floatNormMask    = 0x03
	floatNormImplied = 2 // H5T_NORM_IMPLIED
	floatSignShift   = 8
	floatSignMask    = 0xFF
)

// Where a variable-length string keeps its padding and character set: bits
// 4-7 and 8-11 of the class bit field (H5Odtype.c:763-765). A fixed-length
// string keeps them in bits 0-3 and 4-7 (H5Odtype.c:295-296).
const (
	vlenStringPadShift  = 4
	vlenStringCsetShift = 8
	stringCsetShift     = 4
)

// TypeOrder is the byte order of a datatype as H5T_get_order reports it, the
// Go counterpart of H5T_order_t (H5Tpublic.h:53-61). The values are those of
// the C enum.
type TypeOrder int

// Byte orders (H5Tpublic.h:55-59).
const (
	OrderLittleEndian TypeOrder = 0 // H5T_ORDER_LE
	OrderBigEndian    TypeOrder = 1 // H5T_ORDER_BE
	OrderVAX          TypeOrder = 2 // H5T_ORDER_VAX
	OrderMixed        TypeOrder = 3 // H5T_ORDER_MIXED: compound members differ
	OrderNone         TypeOrder = 4 // H5T_ORDER_NONE: strings, opaque, references
)

// RootType follows the parent chain to the type that has no parent, as
// H5T_get_order and H5T_get_sign do (H5Torder.c:114-116, H5Tfixed.c:73-75).
func (dt *DatatypeMessage) RootType() *DatatypeMessage {
	for dt.Base != nil {
		dt = dt.Base
	}

	return dt
}

// Order mirrors H5T_get_order (H5Torder.c:108-160) on the order the decoder
// sets for each class (H5O__dtype_decode_helper, H5Odtype.c:171-290, 641):
// integer, float, time and bitfield store it in bit 0 of the class bit field
// (a version 3 float with bit 6 also set is VAX, H5Odtype.c:193-201);
// strings, opaque types and references have none; a compound reports its
// members' order; a derived type reports the order of its root type.
func (dt *DatatypeMessage) Order() TypeOrder {
	dt = dt.RootType()

	switch dt.Class {
	case DatatypeFixed, DatatypeTime, DatatypeBitfield:
		return bitFieldOrder(dt.ClassBitField)
	case DatatypeFloat:
		// The decoder reads VAX order only from version 3 messages
		// (H5Odtype.c:193-201); bit 6 of an older one is not a byte order.
		if dt.Version >= dtypeVersion3 && dt.ClassBitField&flagFloatVAX != 0 {
			return OrderVAX
		}
		return bitFieldOrder(dt.ClassBitField)
	case DatatypeCompound:
		return dt.compoundOrder()
	default:
		return OrderNone
	}
}

// bitFieldOrder reads the byte order bit of an atomic class bit field.
func bitFieldOrder(flags uint32) TypeOrder {
	if flags&flagBigEndian != 0 {
		return OrderBigEndian
	}

	return OrderLittleEndian
}

// compoundOrder is the compound branch of H5T_get_order (H5Torder.c:118-152):
// members of order NONE are ignored; the first other order is the result,
// and a member that differs from it makes the result MIXED.
func (dt *DatatypeMessage) compoundOrder() TypeOrder {
	order := OrderNone
	for _, m := range dt.Members {
		if m.Type == nil {
			continue
		}

		mo := m.Type.Order()
		switch {
		case mo == OrderNone:
		case order == OrderNone:
			order = mo
		case mo != order:
			return OrderMixed
		}
	}

	return order
}

// Signed mirrors H5T_get_sign (H5Tfixed.c:65-85): the root of the parent
// chain must be an integer, signed when bit 3 of its class bit field is set
// (H5Odtype.c:174).
func (dt *DatatypeMessage) Signed() bool {
	root := dt.RootType()

	return root.Class == DatatypeFixed && root.ClassBitField&flagSigned != 0
}

// StringProperties mirrors H5Tget_strpad and H5Tget_cset (H5Tstrpad.c:40-62,
// H5Tcset.c:39-60; the parent walk is at H5Tstrpad.c:50-51, H5Tcset.c:49-50):
// follow the parent chain to the first string, then read padding and
// character set from where the decoder keeps them, bits 0-3 and 4-7 for a
// fixed-length string, bits 4-7 and 8-11 for a variable-length one
// (H5Odtype.c:295-296, :763-765). A type with no string in its chain reports
// the zero values.
func (dt *DatatypeMessage) StringProperties() (StringPadding, CharacterSet) {
	for ; dt != nil; dt = dt.Base {
		switch {
		case dt.Class == DatatypeString:
			return StringPadding(dt.ClassBitField & nibbleMask),
				CharacterSet(dt.ClassBitField >> stringCsetShift & nibbleMask)
		case dt.IsVariableString():
			return StringPadding(dt.ClassBitField >> vlenStringPadShift & nibbleMask),
				CharacterSet(dt.ClassBitField >> vlenStringCsetShift & nibbleMask)
		}
	}

	return StringNullTerm, CharsetASCII
}

// errDatatypef reports invalid datatype bytes. The message names what was
// wrong and the C check it mirrors; the error wraps utils.ErrCorrupt.
func errDatatypef(format string, args ...any) error {
	return fmt.Errorf("datatype message: %s: %w", fmt.Sprintf(format, args...), utils.ErrCorrupt)
}

// dtCursor walks a datatype message. Every read is bounds-checked against
// the buffer, the role of H5_IS_KNOWN_BUFFER_OVERFLOW in the C decoder.
type dtCursor struct {
	data []byte
	off  int

	// depth is the nesting level of the datatype being decoded; path names
	// the members and base types leading to it, for the one error wrap at
	// the top (DecodeDatatype).
	depth int
	path  []string
}

// maxDatatypeNesting bounds how deeply datatypes may nest (compound member,
// enumeration, variable-length, array or complex base types). The C decoder
// has no limit (it recurses at H5Odtype.c:500, :683, :771, :825, :872); its
// errors go to a stack without building strings. In Go every level of an
// unbounded recursion costs a frame and, on error, its own context, and a
// 64 KB message can nest about 8000 levels deep (review-064 H1). No real type
// comes near 64 levels.
const maxDatatypeNesting = 64

// enter descends into a nested datatype named label.
func (c *dtCursor) enter(label string) {
	c.path = append(c.path, label)
}

// leave returns from a nested datatype that decoded without error; on error
// the path is kept for the message.
func (c *dtCursor) leave() {
	c.path = c.path[:len(c.path)-1]
}

func (c *dtCursor) remaining() int { return len(c.data) - c.off }

// take returns the next n bytes.
func (c *dtCursor) take(n int, what string) ([]byte, error) {
	if n < 0 || n > c.remaining() {
		return nil, errDatatypef("%s: need %d bytes at offset %d, %d left (ran off end of input buffer)",
			what, n, c.off, c.remaining())
	}
	b := c.data[c.off : c.off+n]
	c.off += n
	return b, nil
}

func (c *dtCursor) u8(what string) (uint8, error) {
	b, err := c.take(1, what)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (c *dtCursor) u16(what string) (uint16, error) {
	b, err := c.take(2, what)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (c *dtCursor) u32(what string) (uint32, error) {
	b, err := c.take(4, what)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// name reads a null-terminated name of a compound or enumeration member and
// advances past it: name and terminator for version 3 and later, a multiple
// of eight bytes before (H5Odtype.c:380-428, :705-745).
func (c *dtCursor) name(version uint8, what string) (string, error) {
	n := bytes.IndexByte(c.data[c.off:], 0)
	if n < 0 {
		return "", errDatatypef("%s not null terminated (H5Odtype.c:387)", what)
	}
	s := string(c.data[c.off : c.off+n])
	if _, err := c.take(encodedNameSize(n, version), what); err != nil {
		return "", err
	}
	return s, nil
}

// encodedNameSize is the space a member name of n bytes occupies: the name
// and its terminator, padded to a multiple of eight before version 3.
func encodedNameSize(n int, version uint8) int {
	if version >= dtypeVersion3 {
		return n + 1
	}
	return ((n + 8) / 8) * 8
}

// limitEncSize is H5VM_limit_enc_size: the bytes needed to encode values from
// 0 to limit (H5VMprivate.h:448-451), log2(limit)/8 + 1.
func limitEncSize(limit uint64) int {
	if limit == 0 {
		return 1
	}
	return (bits.Len64(limit)-1)/8 + 1
}

// DecodeDatatype decodes the datatype message at the start of data and
// returns it with the number of bytes it occupies. It mirrors
// H5O__dtype_decode_helper (H5Odtype.c:120-916); errors wrap utils.ErrCorrupt.
func DecodeDatatype(data []byte) (*DatatypeMessage, int, error) {
	c := &dtCursor{data: data}
	dt, err := decodeDatatype(c)
	if err != nil {
		if len(c.path) > 0 {
			// The single place nested errors get their context.
			return nil, 0, fmt.Errorf("in %s: %w", strings.Join(c.path, " > "), err)
		}
		return nil, 0, err
	}
	return dt, c.off, nil
}

// redecode decodes a message given as header fields plus Properties, the form
// of a DatatypeMessage assembled by hand rather than by DecodeDatatype.
func redecode(dt *DatatypeMessage) (*DatatypeMessage, error) {
	buf := make([]byte, datatypeHeaderSize, datatypeHeaderSize+len(dt.Properties))
	binary.LittleEndian.PutUint32(buf, uint32(dt.Class)&0x0F|uint32(dt.Version&0x0F)<<4|(dt.ClassBitField&0xFFFFFF)<<8)
	binary.LittleEndian.PutUint32(buf[4:], dt.Size)
	full, _, err := DecodeDatatype(append(buf, dt.Properties...))
	if err != nil {
		return nil, err
	}
	return full, nil
}

// decodeDatatype decodes one datatype at the cursor and leaves the cursor
// after it.
func decodeDatatype(c *dtCursor) (*DatatypeMessage, error) {
	if c.depth >= maxDatatypeNesting {
		return nil, errDatatypef("datatype nesting deeper than %d levels at byte %d", maxDatatypeNesting, c.off)
	}
	c.depth++
	defer func() { c.depth-- }()
	start := c.off
	word, err := c.u32("class, version and flags")
	if err != nil {
		return nil, err
	}
	version := uint8(word>>4) & 0x0F //nolint:gosec // G115: a 4-bit field
	if version < dtypeVersion1 || version > dtypeVersionLatest {
		return nil, errDatatypef("bad version number %d (H5Odtype.c:151)", version)
	}
	dt := &DatatypeMessage{
		Class:         DatatypeClass(word & 0x0F),
		Version:       version,
		ClassBitField: word >> 8,
	}
	if dt.Size, err = c.u32("datatype size"); err != nil {
		return nil, err
	}
	if dt.Size == 0 {
		return nil, errDatatypef("invalid datatype size 0 (H5Odtype.c:163)")
	}
	if err := decodeProperties(c, dt); err != nil {
		return nil, err
	}
	dt.Properties = c.data[start+datatypeHeaderSize : c.off]
	// A member or base type of a higher version raises the version, as the
	// C decoder does outside strict format checks (H5O_DTYPE_CHECK_VERSION,
	// H5Odtype.c:75-81, called at :634, :690, :776, :830, :833, :879, :883;
	// compound upgrade :517-523, :566-568, :622-630): arrays need at least 2,
	// complex numbers 5. Properties stay the bytes stored in the file.
	if need := minimumVersion(dt); need > dt.Version {
		upgradeVersion(dt, need)
		// C leaves a complex number type of a stored version below 5 at its
		// parent's version (H5T__upgrade_version_cb, H5T.c:7160-7163) and
		// then cannot encode it (assert at H5Odtype.c:1407); 5 is the version
		// the class needs (H5Tcomplex.c:142).
		dt.Version = need
	}
	return dt, nil
}

// upgradeVersion raises dt to version v the way H5T__upgrade_version
// (H5T.c:7193-7210) does when the decoder raises a type: it visits the
// members and base types first (H5T_VISIT_COMPOSITE_LAST, H5Tvisit.c:81-149)
// and raises every compound, array and enumeration in the tree to at least v,
// every variable-length and complex type to the version of its base type
// (H5T__upgrade_version_cb, H5T.c:7137-7179). A re-encode then writes the
// bytes the C library writes for the type it decoded.
func upgradeVersion(dt *DatatypeMessage, v uint8) {
	for i := range dt.Members {
		if dt.Members[i].Type != nil {
			upgradeVersion(dt.Members[i].Type, v)
		}
	}
	if dt.Base != nil {
		upgradeVersion(dt.Base, v)
	}
	switch dt.Class {
	case DatatypeCompound, DatatypeArray, DatatypeEnum:
		dt.Version = max(dt.Version, v)
	case DatatypeVarLen, DatatypeComplex:
		if dt.Base != nil {
			dt.Version = max(dt.Version, dt.Base.Version)
		}
	}
}

// decodeProperties decodes the class-specific part of dt (the switch of
// H5O__dtype_decode_helper, H5Odtype.c:166-892).
func decodeProperties(c *dtCursor, dt *DatatypeMessage) error {
	switch dt.Class {
	case DatatypeFixed:
		return decodeBitRange(c, dt, "integer", "H5Odtype.c:175-186")
	case DatatypeFloat:
		return decodeFloat(c, dt)
	case DatatypeTime:
		var err error
		dt.Precision, err = c.u16("time precision")
		return err
	case DatatypeString:
		return nil // padding and character set are class flags (H5Odtype.c:295-296)
	case DatatypeBitfield:
		return decodeBitRange(c, dt, "bitfield", "H5Odtype.c:306-317")
	case DatatypeOpaque:
		return decodeOpaque(c, dt)
	case DatatypeCompound:
		return decodeCompound(c, dt)
	case DatatypeReference:
		return checkReferenceFlags(dt.ClassBitField)
	case DatatypeEnum:
		return decodeEnum(c, dt)
	case DatatypeVarLen:
		return decodeBase(c, dt, "variable-length base type")
	case DatatypeArray:
		return decodeArray(c, dt)
	case DatatypeComplex:
		return decodeComplex(c, dt)
	default:
		return errDatatypef("unknown datatype class %d (H5Odtype.c:888-891)", dt.Class)
	}
}

// decodeBitRange decodes the bit offset and precision of an integer or a
// bitfield and applies the C range checks.
func decodeBitRange(c *dtCursor, dt *DatatypeMessage, kind, ref string) error {
	b, err := c.take(4, kind+" offset and precision")
	if err != nil {
		return err
	}
	if dt.Class == DatatypeFixed && isEarlyHDF5GoNumeric(dt.Class, dt.Version, dt.ClassBitField, dt.Size, b) {
		dt.BitOffset, dt.Precision = 0, uint16(dt.Size*8) //nolint:gosec // G115: Size is 1, 2, 4 or 8
		return nil
	}
	dt.BitOffset = binary.LittleEndian.Uint16(b[0:])
	dt.Precision = binary.LittleEndian.Uint16(b[2:])
	sizeBits := uint64(dt.Size) * 8
	switch {
	case uint64(dt.BitOffset) >= sizeBits:
		return errDatatypef("%s offset %d out of bounds for %d-byte type (%s)", kind, dt.BitOffset, dt.Size, ref)
	case dt.Precision == 0:
		return errDatatypef("%s precision is zero (%s)", kind, ref)
	case uint64(dt.BitOffset)+uint64(dt.Precision) > sizeBits: // C: offset+prec-1 >= size*8
		return errDatatypef("%s offset %d + precision %d out of bounds for %d-byte type (%s)",
			kind, dt.BitOffset, dt.Precision, dt.Size, ref)
	}
	return nil
}

// decodeFloat decodes a floating-point datatype (H5Odtype.c:189-273).
func decodeFloat(c *dtCursor, dt *DatatypeMessage) error {
	flags := dt.ClassBitField
	if dt.Version >= dtypeVersion3 && flags&flagFloatVAX != 0 && flags&flagBigEndian == 0 {
		return errDatatypef("bad byte order for floating-point type (H5Odtype.c:196)")
	}
	if (flags>>floatNormShift)&floatNormMask == floatNormMask {
		return errDatatypef("unknown floating-point normalization (H5Odtype.c:219-220)")
	}
	sizeBits := uint64(dt.Size) * 8
	sign := uint64(flags>>floatSignShift) & floatSignMask
	if sign >= sizeBits {
		return errDatatypef("sign bit position %d out of bounds (H5Odtype.c:223)", sign)
	}
	b, err := c.take(12, "floating-point properties")
	if err != nil {
		return err
	}
	if isEarlyHDF5GoNumeric(dt.Class, dt.Version, dt.ClassBitField, dt.Size, b) {
		normalizeIEEEFloat(dt)
		return nil
	}
	dt.BitOffset = binary.LittleEndian.Uint16(b[0:])
	dt.Precision = binary.LittleEndian.Uint16(b[2:])
	dt.ExponentLocation, dt.ExponentSize = b[4], b[5]
	dt.MantissaLocation, dt.MantissaSize = b[6], b[7]
	dt.ExponentBias = binary.LittleEndian.Uint32(b[8:])

	if isLegacyHDF5GoFloat(dt) {
		normalizeIEEEFloat(dt)
		return nil
	}
	if err := checkFloatField("exponent", dt.ExponentLocation, dt.ExponentSize, sizeBits, "H5Odtype.c:235-241"); err != nil {
		return err
	}
	if err := checkFloatField("mantissa", dt.MantissaLocation, dt.MantissaSize, sizeBits, "H5Odtype.c:247-253"); err != nil {
		return err
	}
	expHi := uint64(dt.ExponentLocation) + uint64(dt.ExponentSize) - 1
	manHi := uint64(dt.MantissaLocation) + uint64(dt.MantissaSize) - 1
	switch {
	case rangesOverlap(sign, sign, uint64(dt.ExponentLocation), expHi):
		return errDatatypef("exponent and sign positions overlap (H5Odtype.c:260-263)")
	case rangesOverlap(sign, sign, uint64(dt.MantissaLocation), manHi):
		return errDatatypef("mantissa and sign positions overlap (H5Odtype.c:264-267)")
	case rangesOverlap(uint64(dt.ExponentLocation), expHi, uint64(dt.MantissaLocation), manHi):
		return errDatatypef("mantissa and exponent positions overlap (H5Odtype.c:268-272)")
	}
	return nil
}

// isLegacyHDF5GoFloat reports the float encoding of the attribute writer of
// hdf5-go before v0.14.3 (WriteAttribute with float32/float64 values): version
// 1, class bit field 0 and otherwise the exact IEEE 754 layout of the size.
// Its sign bit position 0 overlaps the mantissa, so the C library rejects it
// ("mantissa and sign positions overlap", H5Odtype.c:264-267) and no valid
// file contains it. Files our users wrote stay readable: it is read as the
// IEEE type the writer meant (documented deviation, ADR-004).
func isLegacyHDF5GoFloat(dt *DatatypeMessage) bool {
	ieee, ok := ieeeLayoutFor(dt.Size)
	return ok && dt.Size != 2 && dt.Version == dtypeVersion1 && dt.ClassBitField == 0 &&
		dt.BitOffset == 0 && uint32(dt.Precision) == dt.Size*8 &&
		dt.ExponentLocation == ieee.epos && dt.ExponentSize == ieee.esize &&
		dt.MantissaLocation == ieee.mpos && dt.MantissaSize == ieee.msize && dt.ExponentBias == ieee.ebias
}

// normalizeIEEEFloat gives dt the IEEE 754 layout of its size: sign in the
// top bit, implied normalization, byte order kept.
func normalizeIEEEFloat(dt *DatatypeMessage) {
	ieee, _ := ieeeLayoutFor(dt.Size)
	dt.ClassBitField = dt.ClassBitField&flagBigEndian | floatNormImplied<<floatNormShift | (dt.Size*8-1)<<floatSignShift
	dt.BitOffset, dt.Precision = 0, uint16(dt.Size*8) //nolint:gosec // G115: Size is 2, 4 or 8
	dt.ExponentLocation, dt.ExponentSize = ieee.epos, ieee.esize
	dt.MantissaLocation, dt.MantissaSize = ieee.mpos, ieee.msize
	dt.ExponentBias = ieee.ebias
}

// errLegacyCompoundf reports an inconsistency in a compound marked as the
// legacy hdf5-go encoding; it wraps utils.ErrCorrupt.
func errLegacyCompoundf(c *dtCursor, format string, args ...any) error {
	return fmt.Errorf("datatype message: legacy hdf5-go compound encoding at byte %d: %s: %w",
		c.off, fmt.Sprintf(format, args...), utils.ErrCorrupt)
}

// decodeLegacyCompound reads the compound encoding of hdf5-go before v0.14.3
// (EncodeCompoundDatatypeV3): version 3 with the member count 0 in the class
// bit field, then a 4-byte member count, and per member a null-terminated
// name, a 4-byte offset and the member type: an 8-byte header plus the old
// writer's fixed property bytes (integer 4, float 12, string 1) or a nested
// legacy compound. The marker never occurs in a valid file: C rejects a
// compound without members (H5Odtype.c:358-360). The result has the same
// members a valid file of the same type decodes to (documented deviation,
// ADR-004).
func decodeLegacyCompound(c *dtCursor, dt *DatatypeMessage) error {
	count, err := c.u32("legacy compound member count")
	if err != nil {
		return errLegacyCompoundf(c, "%v", err)
	}
	if count == 0 || count > maxListedMembers {
		return errLegacyCompoundf(c, "member count %d", count)
	}
	names := make(map[string]struct{})
	var maxMemberEnd uint64
	for i := 0; i < int(count); i++ {
		m, err := decodeLegacyMember(c, i)
		if err != nil {
			return err
		}
		if _, dup := names[m.Name]; dup {
			return errLegacyCompoundf(c, "duplicated member name %q", m.Name)
		}
		names[m.Name] = struct{}{}
		if uint64(m.Offset)+uint64(m.Type.Size) > uint64(dt.Size) {
			return errLegacyCompoundf(c, "member %q at offset %d with size %d outside the %d-byte compound",
				m.Name, m.Offset, m.Type.Size, dt.Size)
		}
		if uint64(m.Offset) < maxMemberEnd {
			if err := checkMemberOverlap(dt.Members, m, i); err != nil {
				return errLegacyCompoundf(c, "%v", err)
			}
		}
		maxMemberEnd = max(maxMemberEnd, uint64(m.Offset)+uint64(m.Type.Size))
		dt.Members = append(dt.Members, m)
	}
	dt.ClassBitField = count // the count a valid version-3 message carries
	return nil
}

// decodeLegacyMember reads one member of a legacy hdf5-go compound.
func decodeLegacyMember(c *dtCursor, i int) (CompoundMember, error) {
	var m CompoundMember
	var err error
	if m.Name, err = c.name(dtypeVersion3, fmt.Sprintf("legacy compound member %d name", i)); err != nil {
		return m, errLegacyCompoundf(c, "%v", err)
	}
	if m.Offset, err = c.u32("legacy compound member offset"); err != nil {
		return m, errLegacyCompoundf(c, "%v", err)
	}
	m.Type, err = decodeLegacyMemberType(c)
	return m, err
}

// decodeLegacyMemberType reads a member type the old writer produced with
// CreateBasicDatatypeMessage (version 1, no flags: unsigned little-endian
// integer, float, null-terminated ASCII string) or a nested legacy compound,
// and returns it in decoded form.
func decodeLegacyMemberType(c *dtCursor) (*DatatypeMessage, error) {
	start := c.off
	hdr, err := c.take(datatypeHeaderSize, "legacy member type header")
	if err != nil {
		return nil, errLegacyCompoundf(c, "%v", err)
	}
	word := binary.LittleEndian.Uint32(hdr)
	dt := &DatatypeMessage{
		Class:         DatatypeClass(word & 0x0F),
		Version:       uint8(word>>4) & 0x0F, //nolint:gosec // G115: a 4-bit field
		ClassBitField: word >> 8,
		Size:          binary.LittleEndian.Uint32(hdr[4:]),
	}
	if dt.Class == DatatypeCompound && dt.Version == dtypeVersion3 && dt.ClassBitField == 0 && dt.Size != 0 {
		if c.depth >= maxDatatypeNesting {
			return nil, errLegacyCompoundf(c, "nesting deeper than %d levels", maxDatatypeNesting)
		}
		c.depth++
		err := decodeLegacyCompound(c, dt)
		c.depth--
		if err != nil {
			return nil, err
		}
		dt.Properties = c.data[start+datatypeHeaderSize : c.off]
		return dt, nil
	}
	if dt.Version != dtypeVersion1 || dt.ClassBitField != 0 {
		return nil, errLegacyCompoundf(c, "member type class %d version %d flags 0x%x not written by hdf5-go",
			dt.Class, dt.Version, dt.ClassBitField)
	}
	props, err := legacyMemberProperties(c, dt)
	if err != nil {
		return nil, err
	}
	dt.Properties = props
	return dt, nil
}

// legacyMemberProperties reads the fixed property bytes of a legacy member
// type and fills the decoded fields. The old writer stored precision in byte
// 1 and nothing else (CreateBasicDatatypeMessage before v0.14.3).
func legacyMemberProperties(c *dtCursor, dt *DatatypeMessage) ([]byte, error) {
	var n int
	switch dt.Class {
	case DatatypeFixed:
		n = 4
	case DatatypeFloat:
		n = 12
	case DatatypeString:
		n = 1
	default:
		return nil, errLegacyCompoundf(c, "member type class %d not written by hdf5-go", dt.Class)
	}
	props, err := c.take(n, "legacy member type properties")
	if err != nil {
		return nil, errLegacyCompoundf(c, "%v", err)
	}
	want := make([]byte, n)
	if dt.Class != DatatypeString {
		want[1] = byte(dt.Size * 8) //nolint:gosec // G115: the old writer's truncating byte
	}
	if !legacyMemberSizeOK(dt) || !bytes.Equal(props, want) {
		return nil, errLegacyCompoundf(c, "%s member of size %d with properties % x not written by hdf5-go",
			className(dt.Class), dt.Size, props)
	}
	switch dt.Class {
	case DatatypeFixed:
		dt.Precision = uint16(dt.Size * 8) //nolint:gosec // G115: Size <= 8
	case DatatypeFloat:
		normalizeIEEEFloat(dt)
	}
	return props, nil
}

// legacyMemberSizeOK reports a member size the old writer could produce:
// integers of 1-8 bytes, IEEE floats of 4 or 8, strings of any size.
func legacyMemberSizeOK(dt *DatatypeMessage) bool {
	switch dt.Class {
	case DatatypeFixed:
		return dt.Size >= 1 && dt.Size <= 8
	case DatatypeFloat:
		return dt.Size == 4 || dt.Size == 8
	default:
		return dt.Size > 0
	}
}

// checkFloatField checks one exponent or mantissa field against the type.
func checkFloatField(field string, pos, size uint8, sizeBits uint64, ref string) error {
	switch {
	case size == 0:
		return errDatatypef("%s size can't be zero (%s)", field, ref)
	case uint64(pos) >= sizeBits:
		return errDatatypef("%s starting position %d out of bounds (%s)", field, pos, ref)
	case uint64(pos)+uint64(size) > sizeBits: // C: pos+size-1 >= size*8
		return errDatatypef("%s range %d+%d out of bounds (%s)", field, pos, size, ref)
	}
	return nil
}

// rangesOverlap is H5_RANGE_OVERLAP (H5private.h:321) for inclusive ranges.
func rangesOverlap(lo1, hi1, lo2, hi2 uint64) bool {
	return lo1 <= hi2 && lo2 <= hi1
}

// decodeOpaque decodes the tag of an opaque datatype (H5Odtype.c:321-343).
// The tag length is in the class bit field and must be a multiple of eight.
func decodeOpaque(c *dtCursor, dt *DatatypeMessage) error {
	z := int(dt.ClassBitField & (opaqueTagMax - 1))
	if z&7 != 0 {
		return errDatatypef("opaque tag length %d not aligned to 8 (H5Odtype.c:330-331)", z)
	}
	tag, err := c.take(z, "opaque tag")
	if err != nil {
		return err
	}
	// The C library keeps the tag as a C string (H5Odtype.c:338-339).
	if i := bytes.IndexByte(tag, 0); i >= 0 {
		tag = tag[:i]
	}
	dt.OpaqueTag = string(tag)
	return nil
}

// decodeCompound decodes the members of a compound datatype
// (H5Odtype.c:345-635). The member count is the low 16 bits of the class bit
// field for every version.
func decodeCompound(c *dtCursor, dt *DatatypeMessage) error {
	nmembs := int(dt.ClassBitField & maxListedMembers)
	if nmembs == 0 && dt.Version == dtypeVersion3 && dt.ClassBitField == 0 {
		return decodeLegacyCompound(c, dt)
	}
	if nmembs == 0 {
		return errDatatypef("invalid number of members: 0 (H5Odtype.c:358-360)")
	}
	offsetBytes := limitEncSize(uint64(dt.Size))
	names := make(map[string]struct{})
	var maxMemberEnd uint64
	for i := 0; i < nmembs; i++ {
		m, err := decodeCompoundMember(c, dt, offsetBytes, i)
		if err != nil {
			return err
		}
		if _, dup := names[m.Name]; dup {
			return errDatatypef("duplicated compound field name %q, member %d (H5Odtype.c:401-406)", m.Name, i)
		}
		names[m.Name] = struct{}{}
		if uint64(m.Offset) < maxMemberEnd {
			if err := checkMemberOverlap(dt.Members, m, i); err != nil {
				return err
			}
		}
		maxMemberEnd = max(maxMemberEnd, uint64(m.Offset)+uint64(m.Type.Size))
		dt.Members = append(dt.Members, m)
	}
	return nil
}

// decodeCompoundMember decodes member i: name, offset, the version-1 array
// information and the member datatype (H5Odtype.c:380-515).
func decodeCompoundMember(c *dtCursor, dt *DatatypeMessage, offsetBytes, i int) (CompoundMember, error) {
	var m CompoundMember
	var err error
	if m.Name, err = c.name(dt.Version, fmt.Sprintf("compound member %d name", i)); err != nil {
		return m, err
	}
	if m.Offset, err = decodeMemberOffset(c, dt.Version, offsetBytes); err != nil {
		return m, err
	}
	var dims []uint32
	if dt.Version == dtypeVersion1 {
		if dims, err = decodeV1MemberDims(c); err != nil {
			return m, err
		}
	}
	c.enter(fmt.Sprintf("compound member %d (%q)", i, m.Name))
	if m.Type, err = decodeDatatype(c); err != nil {
		return m, err
	}
	c.leave()
	if uint64(m.Offset)+uint64(m.Type.Size) > uint64(dt.Size) {
		return m, errDatatypef("member %q at offset %d with size %d extends outside its %d-byte compound type (H5Odtype.c:509-515)",
			m.Name, m.Offset, m.Type.Size, dt.Size)
	}
	if len(dims) > 0 {
		if m.Type, err = v1MemberArray(m.Type, dims); err != nil {
			return m, err
		}
		if uint64(m.Offset)+uint64(m.Type.Size) > uint64(dt.Size) {
			return m, errDatatypef("array member %q at offset %d with size %d extends outside its %d-byte compound type",
				m.Name, m.Offset, m.Type.Size, dt.Size)
		}
	}
	return m, nil
}

// decodeMemberOffset reads a member offset: offsetBytes wide from version 3
// on, 4 bytes before (H5Odtype.c:430-446).
func decodeMemberOffset(c *dtCursor, version uint8, offsetBytes int) (uint32, error) {
	if version < dtypeVersion3 {
		return c.u32("compound member offset")
	}
	b, err := c.take(offsetBytes, "compound member offset")
	if err != nil {
		return 0, err
	}
	var off uint32
	for j := len(b) - 1; j >= 0; j-- {
		off = off<<8 | uint32(b[j])
	}
	return off, nil
}

// decodeV1MemberDims reads the array information of a version-1 compound
// member and returns its dimensions, none for a scalar member
// (H5Odtype.c:451-490, dimension check :529-539).
func decodeV1MemberDims(c *dtCursor) ([]uint32, error) {
	b, err := c.take(compoundV1ArrayInfoSize, "version 1 compound member array information")
	if err != nil {
		return nil, err
	}
	ndims := int(b[0])
	if ndims > compoundV1MaxDims {
		return nil, errDatatypef("invalid number of dimensions %d for array member (H5Odtype.c:459-464)", ndims)
	}
	dims := make([]uint32, ndims)
	for u := range dims {
		dims[u] = binary.LittleEndian.Uint32(b[12+4*u:])
		if dims[u] == 0 {
			return nil, errDatatypef("zero-sized dimension in array member (H5Odtype.c:530-538)")
		}
	}
	return dims, nil
}

// v1MemberArray turns the base type of a version-1 array member into the
// array datatype H5T__array_create builds (H5Odtype.c:541-560, H5Tarray.c:169).
func v1MemberArray(base *DatatypeMessage, dims []uint32) (*DatatypeMessage, error) {
	size := uint64(base.Size)
	for _, d := range dims {
		size *= uint64(d)
		if size > math.MaxUint32 {
			return nil, errDatatypef("array member size overflows 32 bits")
		}
	}
	arr := &DatatypeMessage{
		Class:     DatatypeArray,
		Version:   max(base.Version, dtypeVersion2),
		Size:      uint32(size), //nolint:gosec // G115: checked against MaxUint32 above
		ArrayDims: dims,
		Base:      base,
	}
	// The array has no bytes of its own in the file; give it the properties
	// a version-2 array message would have, so it is self-consistent.
	enc, err := encodeDatatype(arr)
	if err != nil {
		// The base decoded, so its fields are ones the encoder takes; keep
		// the decoder's contract that its errors wrap utils.ErrCorrupt.
		return nil, errDatatypef("version 1 array member: %s", err.Error())
	}
	arr.Properties = enc[datatypeHeaderSize:]
	return arr, nil
}

// checkMemberOverlap reports whether member m overlaps one of the members
// before it (H5Odtype.c:594-611).
func checkMemberOverlap(prev []CompoundMember, m CompoundMember, i int) error {
	lo, hi := uint64(m.Offset), uint64(m.Offset)+uint64(m.Type.Size)
	for u := range prev {
		plo, phi := uint64(prev[u].Offset), uint64(prev[u].Offset)+uint64(prev[u].Type.Size)
		if (lo >= plo && lo < phi) || (lo < plo && hi > plo) {
			return errDatatypef("member %d (%q) overlaps member %d (%q) (H5Odtype.c:597-610)", i, m.Name, u, prev[u].Name)
		}
	}
	return nil
}

// checkReferenceFlags validates the reference type in the class bit field
// (H5Odtype.c:647-663).
func checkReferenceFlags(flags uint32) error {
	rtype := flags & 0x0F
	if rtype >= referenceMaxType {
		return errDatatypef("invalid reference type %d (H5Odtype.c:648-651)", rtype)
	}
	if rtype > referenceDatasetRegion1 && (flags>>4)&0x0F != referenceEncodeVersion {
		return errDatatypef("reference version %d does not match %d (H5Odtype.c:654-660)",
			(flags>>4)&0x0F, referenceEncodeVersion)
	}
	return nil
}

// decodeBase decodes the parent type of an enumeration, variable-length,
// array or complex datatype.
func decodeBase(c *dtCursor, dt *DatatypeMessage, what string) error {
	c.enter(what)
	base, err := decodeDatatype(c)
	if err != nil {
		return err
	}
	c.leave()
	dt.Base = base
	return nil
}

// decodeEnum decodes an enumeration: base type, all names, then all values
// (H5Odtype.c:674-755).
func decodeEnum(c *dtCursor, dt *DatatypeMessage) error {
	nmembs := int(dt.ClassBitField & maxListedMembers)
	if err := decodeBase(c, dt, "enumeration base type"); err != nil {
		return err
	}
	if dt.Base.Size != dt.Size {
		return errDatatypef("enumeration size %d does not match its %d-byte base type (H5Odtype.c:685-686)",
			dt.Size, dt.Base.Size)
	}
	for i := 0; i < nmembs; i++ {
		name, err := c.name(dt.Version, fmt.Sprintf("enumeration member %d name", i))
		if err != nil {
			return err
		}
		if name == "" {
			return errDatatypef("0 length enumeration name, member %d (H5Odtype.c:722-723)", i)
		}
		dt.EnumNames = append(dt.EnumNames, name)
	}
	total := uint64(nmembs) * uint64(dt.Base.Size)
	if total > uint64(c.remaining()) { //nolint:gosec // G115: remaining() is never negative
		return errDatatypef("enumeration values: need %d bytes at offset %d, %d left (H5Odtype.c:751-752)",
			total, c.off, c.remaining())
	}
	values, err := c.take(int(total), "enumeration values") //nolint:gosec // G115: total <= remaining(), an int
	if err != nil {
		return err
	}
	dt.EnumValues = append([]byte(nil), values...)
	return nil
}

// decodeArray decodes an array datatype (H5Odtype.c:786-840).
func decodeArray(c *dtCursor, dt *DatatypeMessage) error {
	ndims, err := c.u8("array rank")
	if err != nil {
		return err
	}
	if ndims > arrayMaxRank {
		return errDatatypef("too many dimensions %d for array datatype (H5Odtype.c:796-797)", ndims)
	}
	if dt.Version < dtypeVersion3 {
		if _, err := c.take(3, "array reserved bytes"); err != nil {
			return err
		}
	}
	b, err := c.take(4*int(ndims), "array dimensions")
	if err != nil {
		return err
	}
	dt.ArrayDims = make([]uint32, ndims)
	for u := range dt.ArrayDims {
		dt.ArrayDims[u] = binary.LittleEndian.Uint32(b[4*u:])
	}
	if dt.Version < dtypeVersion3 {
		if _, err := c.take(4*int(ndims), "array dimension permutation"); err != nil {
			return err
		}
	}
	return decodeBase(c, dt, "array base type")
}

// decodeComplex decodes a complex number datatype (H5Odtype.c:842-886).
func decodeComplex(c *dtCursor, dt *DatatypeMessage) error {
	flags := dt.ClassBitField
	switch {
	case flags&0x01 == 0:
		return errDatatypef("heterogeneous complex number datatypes are unsupported (H5Odtype.c:852-854)")
	case (flags>>1)&0x03 != 0:
		return errDatatypef("only complex numbers in rectangular form are supported (H5Odtype.c:859-861)")
	case flags>>3 != 0:
		return errDatatypef("invalid flag bits 0x%x for complex number datatype (H5Odtype.c:864-866)", flags)
	}
	return decodeBase(c, dt, "complex number base type")
}

// EncodeDatatypeMessage encodes dt as a datatype message, mirroring
// H5O__dtype_encode_helper (H5Odtype.c:932-1446). The message is written from
// the decoded fields (see [DatatypeMessage]); Properties is ignored.
//
// A Version of 0 selects the lowest version that can encode the type, as the
// C library does with the earliest library-version bound: 1, 2 for arrays, 5
// for complex numbers, and never below the version of a member or base type.
// The result is decoded again before it is returned: a datatype the decoder
// would reject is an error, not a file the C library cannot read.
func EncodeDatatypeMessage(dt *DatatypeMessage) ([]byte, error) {
	if err := checkNesting(dt, 1); err != nil {
		return nil, err
	}
	buf, err := encodeDatatype(dt)
	if err != nil {
		return nil, err
	}
	_, n, err := DecodeDatatype(buf)
	if err != nil {
		return nil, fmt.Errorf("encode datatype: the result is not a valid datatype message: %s", err.Error())
	}
	if n != len(buf) {
		return nil, fmt.Errorf("encode datatype: wrote %d bytes, the message decodes as %d", len(buf), n)
	}
	return buf, nil
}

// dtEncoder carries the state of one encode: path names the members and
// base types leading to the datatype being encoded, for the one error wrap
// in encodeDatatype. The nesting depth is bounded before encoding starts
// (checkNesting), so the encoder never builds an error per level.
type dtEncoder struct {
	path []string
}

// encodeDatatype encodes dt; an error inside a member or base type names
// the path to it once.
func encodeDatatype(dt *DatatypeMessage) ([]byte, error) {
	e := &dtEncoder{}
	buf, err := e.encode(nil, dt)
	if err != nil {
		if len(e.path) > 0 {
			return nil, fmt.Errorf("in %s: %w", strings.Join(e.path, " > "), err)
		}
		return nil, err
	}
	return buf, nil
}

// child appends the encoding of the member or base type dt, named label.
// On error the label stays on the path for the message.
func (e *dtEncoder) child(buf []byte, dt *DatatypeMessage, label string) ([]byte, error) {
	e.path = append(e.path, label)
	buf, err := e.encode(buf, dt)
	if err != nil {
		return nil, err
	}
	e.path = e.path[:len(e.path)-1]
	return buf, nil
}

// encode appends the encoding of dt to buf.
func (e *dtEncoder) encode(buf []byte, dt *DatatypeMessage) ([]byte, error) {
	if dt == nil {
		return nil, errors.New("encode datatype: nil datatype")
	}
	if dt.Size == 0 {
		return nil, fmt.Errorf("encode datatype: %s datatype size cannot be 0", className(dt.Class))
	}
	version, err := encodeVersion(dt)
	if err != nil {
		return nil, err
	}
	hdr := len(buf)
	buf = append(buf, 0, 0, 0, 0)
	buf = binary.LittleEndian.AppendUint32(buf, dt.Size)

	var flags uint32
	switch dt.Class {
	case DatatypeFixed, DatatypeFloat, DatatypeTime, DatatypeBitfield:
		buf, flags, err = encodeAtomic(buf, dt, version)
	case DatatypeString:
		flags = dt.ClassBitField & 0xFF // padding bits 0-3, character set bits 4-7 (H5Odtype.c:1158-1159)
	case DatatypeOpaque:
		buf, flags, err = encodeOpaque(buf, dt)
	case DatatypeCompound:
		buf, flags, err = e.encodeCompound(buf, dt, version)
	case DatatypeReference:
		flags, err = encodeReferenceFlags(dt.ClassBitField)
	case DatatypeEnum:
		buf, flags, err = e.encodeEnum(buf, dt, version)
	case DatatypeVarLen, DatatypeArray, DatatypeComplex:
		buf, flags, err = e.encodeDerived(buf, dt, version)
	default:
		err = fmt.Errorf("encode datatype: unsupported datatype class %d", dt.Class)
	}
	if err != nil {
		return nil, err
	}
	// Class, version and class bit field (H5Odtype.c:1438-1442).
	binary.LittleEndian.PutUint32(buf[hdr:], uint32(dt.Class)&0x0F|uint32(version)<<4|(flags&0xFFFFFF)<<8)
	return buf, nil
}

// encodeVersion resolves the version dt is encoded with and checks it
// against the constraints H5O__dtype_encode_helper asserts.
func encodeVersion(dt *DatatypeMessage) (uint8, error) {
	minimum := minimumVersion(dt)
	v := dt.Version
	if v == 0 {
		v = minimum
	}
	if v > dtypeVersionLatest {
		return 0, fmt.Errorf("encode datatype: version %d above %d", v, dtypeVersionLatest)
	}
	if v < minimum {
		return 0, fmt.Errorf("encode datatype: %s version %d below the required %d (a member or base type needs it)",
			className(dt.Class), v, minimum)
	}
	return v, nil
}

// minimumVersion is the lowest version that can encode dt: the class minimum
// (arrays 2, H5Tarray.c:169; complex numbers 5) and at least the version of
// every member or base type (H5Odtype.c:1254-1255, :1318, :1354, :1375,
// :1410).
func minimumVersion(dt *DatatypeMessage) uint8 {
	v := dtypeVersion1
	switch dt.Class {
	case DatatypeArray:
		v = dtypeVersion2
	case DatatypeComplex:
		v = dtypeVersion5
	case DatatypeCompound:
		for _, m := range dt.Members {
			if m.Type != nil {
				v = max(v, childVersion(m.Type))
			}
		}
	}
	if dt.Base != nil {
		v = max(v, childVersion(dt.Base))
	}
	return v
}

// childVersion is the version a member or base type will be encoded with.
func childVersion(dt *DatatypeMessage) uint8 {
	if dt.Version != 0 {
		return dt.Version
	}
	return minimumVersion(dt)
}

// encodeAtomic encodes the properties of the fixed-point, floating-point,
// time and bitfield classes (H5Odtype.c:951-1146, :1162-1217).
func encodeAtomic(buf []byte, dt *DatatypeMessage, version uint8) ([]byte, uint32, error) {
	offset, prec, err := atomicPrecision(dt)
	if err != nil {
		return nil, 0, err
	}
	switch dt.Class {
	case DatatypeFixed:
		// Order, low and high padding, sign (H5Odtype.c:955-1017).
		flags := dt.ClassBitField & (flagBigEndian | flagLSBPadOne | flagMSBPadOne | flagSigned)
		return appendBitRange(buf, offset, prec), flags, nil
	case DatatypeBitfield:
		// Order, low and high padding (H5Odtype.c:1166-1213).
		flags := dt.ClassBitField & (flagBigEndian | flagLSBPadOne | flagMSBPadOne)
		return appendBitRange(buf, offset, prec), flags, nil
	case DatatypeTime:
		// Byte order only (H5Odtype.c:1129-1145).
		return binary.LittleEndian.AppendUint16(buf, prec), dt.ClassBitField & flagBigEndian, nil
	default:
		return encodeFloat(buf, dt, version, offset, prec)
	}
}

// atomicPrecision returns the bit offset and precision to encode; a zero
// precision stands for the whole type (Size*8 bits at offset 0).
func atomicPrecision(dt *DatatypeMessage) (offset, prec uint16, err error) {
	if dt.Precision != 0 {
		return dt.BitOffset, dt.Precision, nil
	}
	if uint64(dt.Size)*8 > math.MaxUint16 {
		return 0, 0, fmt.Errorf("encode datatype: %d-byte %s type needs an explicit precision",
			dt.Size, className(dt.Class))
	}
	return 0, uint16(dt.Size * 8), nil //nolint:gosec // G115: checked against MaxUint16 above
}

func appendBitRange(buf []byte, offset, prec uint16) []byte {
	buf = binary.LittleEndian.AppendUint16(buf, offset)
	return binary.LittleEndian.AppendUint16(buf, prec)
}

// ieeeLayout is the bit layout of an IEEE 754 binary interchange format.
type ieeeLayout struct {
	epos, esize, mpos, msize uint8
	ebias                    uint32
}

// ieeeLayoutFor returns the layout of H5T_IEEE_F16LE, F32LE or F64LE for a
// size of 2, 4 or 8 bytes (IEEE 754 binary16, binary32, binary64).
func ieeeLayoutFor(size uint32) (ieeeLayout, bool) {
	switch size {
	case 2:
		return ieeeLayout{epos: 10, esize: 5, mpos: 0, msize: 10, ebias: 15}, true
	case 4:
		return ieeeLayout{epos: 23, esize: 8, mpos: 0, msize: 23, ebias: 127}, true
	case 8:
		return ieeeLayout{epos: 52, esize: 11, mpos: 0, msize: 52, ebias: 1023}, true
	default:
		return ieeeLayout{}, false
	}
}

// encodeFloat encodes a floating-point type (H5Odtype.c:1023-1126). Without
// an exponent and mantissa layout the type is IEEE 754 for its size: sign in
// the top bit, implied normalization, the caller's byte order.
func encodeFloat(buf []byte, dt *DatatypeMessage, version uint8, offset, prec uint16) ([]byte, uint32, error) {
	l := ieeeLayout{
		epos: dt.ExponentLocation, esize: dt.ExponentSize,
		mpos: dt.MantissaLocation, msize: dt.MantissaSize,
		ebias: dt.ExponentBias,
	}
	var flags uint32
	if l.esize == 0 && l.msize == 0 {
		ieee, ok := ieeeLayoutFor(dt.Size)
		if !ok {
			return nil, 0, fmt.Errorf("encode datatype: %d-byte floating-point type needs an explicit layout", dt.Size)
		}
		l = ieee
		sign := dt.Size*8 - 1
		flags = dt.ClassBitField&flagBigEndian | floatNormImplied<<floatNormShift | sign<<floatSignShift
	} else {
		// Order, paddings, normalization and sign position as given; the VAX
		// order bit exists from version 3 on (H5Odtype.c:1035-1038).
		flags = dt.ClassBitField & 0xFF3F
		if version >= dtypeVersion3 {
			flags |= dt.ClassBitField & flagFloatVAX
		}
	}
	buf = appendBitRange(buf, offset, prec)
	buf = append(buf, l.epos, l.esize, l.mpos, l.msize)
	return binary.LittleEndian.AppendUint32(buf, l.ebias), flags, nil
}

// encodeOpaque encodes the tag of an opaque type, null-padded to a multiple
// of eight bytes whose length is the class bit field (H5Odtype.c:1219-1236).
func encodeOpaque(buf []byte, dt *DatatypeMessage) ([]byte, uint32, error) {
	tag := dt.OpaqueTag
	if strings.IndexByte(tag, 0) >= 0 {
		return nil, 0, errors.New("encode datatype: opaque tag contains a NUL byte")
	}
	if len(tag) > maxEncodableOpaqueTag {
		return nil, 0, fmt.Errorf("encode datatype: opaque tag of %d bytes, at most %d can be stored",
			len(tag), maxEncodableOpaqueTag)
	}
	aligned := (len(tag) + 7) &^ 7
	buf = append(buf, tag...)
	buf = append(buf, make([]byte, aligned-len(tag))...)
	return buf, uint32(aligned), nil //nolint:gosec // G115: at most maxEncodableOpaqueTag
}

// encodeCompound encodes the members of a compound type (H5Odtype.c:1238-1308):
// the member count in the class bit field, then per member its name, its
// offset (limit_enc_size(size) bytes from version 3 on, 4 before), the
// version-1 array information and its datatype.
func (e *dtEncoder) encodeCompound(buf []byte, dt *DatatypeMessage, version uint8) ([]byte, uint32, error) {
	if len(dt.Members) == 0 || len(dt.Members) > maxListedMembers {
		return nil, 0, fmt.Errorf("encode datatype: compound with %d members, need 1 to %d",
			len(dt.Members), maxListedMembers)
	}
	offsetBytes := limitEncSize(uint64(dt.Size))
	for i, m := range dt.Members {
		var err error
		if buf, err = e.encodeCompoundMember(buf, dt, version, offsetBytes, i, m); err != nil {
			return nil, 0, err
		}
	}
	return buf, uint32(len(dt.Members)), nil //nolint:gosec // G115: at most maxListedMembers
}

// encodeCompoundMember appends member i of compound dt.
func (e *dtEncoder) encodeCompoundMember(buf []byte, dt *DatatypeMessage, version uint8, offsetBytes, i int, m CompoundMember) ([]byte, error) {
	if err := checkMemberName(m.Name, "compound member", i); err != nil {
		return nil, err
	}
	if m.Type == nil {
		return nil, fmt.Errorf("encode datatype: compound member %d (%q) has no type", i, m.Name)
	}
	if version == dtypeVersion1 && m.Type.Class == DatatypeArray {
		return nil, fmt.Errorf("encode datatype: compound member %q is an array, needs version 2 (H5Odtype.c:1251-1252)", m.Name)
	}
	// Also keeps the offset within the offsetBytes the decoder reads.
	if uint64(m.Offset)+uint64(m.Type.Size) > uint64(dt.Size) {
		return nil, fmt.Errorf("encode datatype: compound member %q at offset %d with size %d extends outside the %d-byte compound",
			m.Name, m.Offset, m.Type.Size, dt.Size)
	}
	buf = appendName(buf, m.Name, version)
	if version >= dtypeVersion3 {
		for j := 0; j < offsetBytes; j++ {
			buf = append(buf, byte(m.Offset>>(8*j))) //nolint:gosec // G115: little-endian byte j of the offset
		}
	} else {
		buf = binary.LittleEndian.AppendUint32(buf, m.Offset)
	}
	if version == dtypeVersion1 {
		buf = append(buf, make([]byte, compoundV1ArrayInfoSize)...) // scalar member (H5Odtype.c:1282-1302)
	}
	return e.child(buf, m.Type, fmt.Sprintf("compound member %d (%q)", i, m.Name))
}

// checkMemberName rejects names the decoder cannot read back: empty (a
// compound member needs a name, H5Tinsert) or containing NUL.
func checkMemberName(name, what string, i int) error {
	if name == "" {
		return fmt.Errorf("encode datatype: %s %d has an empty name", what, i)
	}
	if strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("encode datatype: %s %d name %q contains a NUL byte", what, i, name)
	}
	return nil
}

// appendName appends a null-terminated name, padded with NULs to a multiple
// of eight bytes before version 3 (H5Odtype.c:1257-1269, :1331-1343).
func appendName(buf []byte, name string, version uint8) []byte {
	buf = append(buf, name...)
	return append(buf, make([]byte, encodedNameSize(len(name), version)-len(name))...)
}

// encodeReferenceFlags returns the class bit field of a reference type: the
// reference type, plus the encoding version for the opaque reference types
// (H5Odtype.c:1310-1314).
func encodeReferenceFlags(bitField uint32) (uint32, error) {
	rtype := bitField & 0x0F
	if rtype >= referenceMaxType {
		return 0, fmt.Errorf("encode datatype: invalid reference type %d", rtype)
	}
	if rtype > referenceDatasetRegion1 {
		return rtype | referenceEncodeVersion<<4, nil
	}
	return rtype, nil
}

// encodeEnum encodes an enumeration: base type, all names, then all values
// (H5Odtype.c:1316-1350).
func (e *dtEncoder) encodeEnum(buf []byte, dt *DatatypeMessage, version uint8) ([]byte, uint32, error) {
	if dt.Base == nil {
		return nil, 0, errors.New("encode datatype: enumeration has no base type")
	}
	if dt.Base.Size != dt.Size {
		return nil, 0, fmt.Errorf("encode datatype: enumeration size %d differs from its base type size %d",
			dt.Size, dt.Base.Size)
	}
	n := len(dt.EnumNames)
	if n > maxListedMembers {
		return nil, 0, fmt.Errorf("encode datatype: %d enumeration members, at most %d", n, maxListedMembers)
	}
	if uint64(len(dt.EnumValues)) != uint64(n)*uint64(dt.Base.Size) {
		return nil, 0, fmt.Errorf("encode datatype: %d enumeration values bytes, need %d members x %d bytes",
			len(dt.EnumValues), n, dt.Base.Size)
	}
	buf, err := e.child(buf, dt.Base, "enumeration base type")
	if err != nil {
		return nil, 0, err
	}
	for i, name := range dt.EnumNames {
		if err := checkMemberName(name, "enumeration member", i); err != nil {
			return nil, 0, err
		}
		buf = appendName(buf, name, version)
	}
	return append(buf, dt.EnumValues...), uint32(n), nil
}

// encodeDerived encodes the classes that consist of a base type plus a few
// fields: variable-length, array and complex number.
func (e *dtEncoder) encodeDerived(buf []byte, dt *DatatypeMessage, version uint8) ([]byte, uint32, error) {
	if dt.Base == nil {
		return nil, 0, fmt.Errorf("encode datatype: %s datatype has no base type", className(dt.Class))
	}
	var flags uint32
	switch dt.Class {
	case DatatypeVarLen:
		// Kind; padding and character set for strings (H5Odtype.c:1356-1360).
		flags = dt.ClassBitField & 0x0F
		if flags == vlenString {
			flags = dt.ClassBitField & 0xFFF
		}
	case DatatypeArray:
		var err error
		if buf, err = encodeArrayDims(buf, dt, version); err != nil {
			return nil, 0, err
		}
	default: // DatatypeComplex: homogeneous, rectangular form (H5Odtype.c:1412-1422).
		if dt.ClassBitField != 0 && dt.ClassBitField != 0x01 {
			return nil, 0, fmt.Errorf("encode datatype: complex number flags 0x%x, only homogeneous rectangular is supported", dt.ClassBitField)
		}
		if uint64(dt.Size) != 2*uint64(dt.Base.Size) {
			return nil, 0, fmt.Errorf("encode datatype: complex number size %d is not twice its base size %d", dt.Size, dt.Base.Size)
		}
		flags = 0x01
	}
	buf, err := e.child(buf, dt.Base, className(dt.Class)+" base type")
	if err != nil {
		return nil, 0, err
	}
	return buf, flags, nil
}

// encodeArrayDims encodes the rank and dimensions of an array type, with the
// reserved bytes and permutation of versions before 3 (H5Odtype.c:1367-1398).
func encodeArrayDims(buf []byte, dt *DatatypeMessage, version uint8) ([]byte, error) {
	n := len(dt.ArrayDims)
	if n == 0 || n > arrayMaxRank {
		return nil, fmt.Errorf("encode datatype: array rank %d, need 1 to %d", n, arrayMaxRank)
	}
	size := uint64(dt.Base.Size)
	for _, d := range dt.ArrayDims {
		if d == 0 {
			return nil, errors.New("encode datatype: zero array dimension")
		}
		size *= uint64(d)
		if size > math.MaxUint32 {
			return nil, errors.New("encode datatype: array size overflows 32 bits")
		}
	}
	if size != uint64(dt.Size) {
		return nil, fmt.Errorf("encode datatype: array size %d, dimensions x base size give %d", dt.Size, size)
	}
	buf = append(buf, byte(n))
	if version < dtypeVersion3 {
		buf = append(buf, 0, 0, 0)
	}
	for _, d := range dt.ArrayDims {
		buf = binary.LittleEndian.AppendUint32(buf, d)
	}
	if version < dtypeVersion3 {
		for i := range dt.ArrayDims {
			buf = binary.LittleEndian.AppendUint32(buf, uint32(i))
		}
	}
	return buf, nil
}

// className names a datatype class in messages.
func className(c DatatypeClass) string {
	if int(c) < len(datatypeClassNames) {
		return datatypeClassNames[c]
	}
	return fmt.Sprintf("class %d", c)
}

var datatypeClassNames = [...]string{
	DatatypeFixed:     "fixed-point",
	DatatypeFloat:     "floating-point",
	DatatypeTime:      "time",
	DatatypeString:    "string",
	DatatypeBitfield:  "bitfield",
	DatatypeOpaque:    "opaque",
	DatatypeCompound:  "compound",
	DatatypeReference: "reference",
	DatatypeEnum:      "enumeration",
	DatatypeVarLen:    "variable-length",
	DatatypeArray:     "array",
	DatatypeComplex:   "complex",
}

// newDatatype encodes dt and decodes the result, so the returned message has
// exactly the form the decoder produces (Properties included) and the bytes
// the encoder writes for it are known to be valid.
func newDatatype(dt *DatatypeMessage) (*DatatypeMessage, error) {
	enc, err := EncodeDatatypeMessage(dt)
	if err != nil {
		return nil, err
	}
	out, _, err := DecodeDatatype(enc)
	if err != nil {
		return nil, fmt.Errorf("decode new datatype: %w", err)
	}
	return out, nil
}

// NewIntegerDatatype returns a little-endian two's-complement (signed) or
// unsigned integer of size bytes, the layout of H5T_STD_I<n>LE /
// H5T_STD_U<n>LE: full precision, zero padding.
func NewIntegerDatatype(size uint32, signed bool) (*DatatypeMessage, error) {
	var flags uint32
	if signed {
		flags = flagSigned
	}
	return newDatatype(&DatatypeMessage{Class: DatatypeFixed, Version: dtypeVersion1, Size: size, ClassBitField: flags})
}

// NewFloatDatatype returns a little-endian IEEE 754 floating-point type of 2,
// 4 or 8 bytes, the layout of H5T_IEEE_F16LE / F32LE / F64LE.
func NewFloatDatatype(size uint32) (*DatatypeMessage, error) {
	return newDatatype(&DatatypeMessage{Class: DatatypeFloat, Version: dtypeVersion1, Size: size})
}

// NewStringDatatype returns a fixed-length string of size bytes.
func NewStringDatatype(size uint32, pad StringPadding, cset CharacterSet) (*DatatypeMessage, error) {
	flags := uint32(pad)&nibbleMask | (uint32(cset)&nibbleMask)<<4
	return newDatatype(&DatatypeMessage{Class: DatatypeString, Version: dtypeVersion1, Size: size, ClassBitField: flags})
}

// NewReferenceDatatype returns an object (8 bytes) or dataset region
// (12 bytes) reference type of the given size.
func NewReferenceDatatype(rtype ReferenceType, size uint32) (*DatatypeMessage, error) {
	return newDatatype(&DatatypeMessage{Class: DatatypeReference, Version: dtypeVersion1, Size: size, ClassBitField: uint32(rtype)})
}

// NewOpaqueDatatype returns an opaque type of size bytes with an ASCII tag.
func NewOpaqueDatatype(size uint32, tag string) (*DatatypeMessage, error) {
	return newDatatype(&DatatypeMessage{Class: DatatypeOpaque, Version: dtypeVersion1, Size: size, OpaqueTag: tag})
}

// NewCompoundDatatype returns a compound type of size bytes. It is encoded as
// version 3, the format of the C library's 1.8+ bound: unpadded member names
// and member offsets of H5VM_limit_enc_size(size) bytes.
func NewCompoundDatatype(size uint32, members []CompoundMember) (*DatatypeMessage, error) {
	return newDatatype(&DatatypeMessage{Class: DatatypeCompound, Version: dtypeVersion3, Size: size, Members: members})
}

// NewEnumDatatype returns an enumeration over base with the given member
// names and their values, packed in the base type's representation. It is
// encoded as version 1 (names padded to eight bytes), as the C library does.
func NewEnumDatatype(base *DatatypeMessage, names []string, values []byte) (*DatatypeMessage, error) {
	if base == nil {
		return nil, errors.New("enumeration base type is nil")
	}
	return newDatatype(&DatatypeMessage{
		Class: DatatypeEnum, Size: base.Size, EnumNames: names, EnumValues: values, Base: base,
	})
}

// vlenHeapIDSize is the in-file size of a variable-length element: a 4-byte
// length plus a global heap ID (8-byte address and 4-byte index), the value
// H5T_set_loc gives a disk VL type with 8-byte addresses.
const vlenHeapIDSize = 16

// NewVLenSequenceDatatype returns a variable-length sequence of base.
func NewVLenSequenceDatatype(base *DatatypeMessage) (*DatatypeMessage, error) {
	if base == nil {
		return nil, errors.New("variable-length base type is nil")
	}
	return newDatatype(&DatatypeMessage{Class: DatatypeVarLen, Size: vlenHeapIDSize, ClassBitField: vlenSequence, Base: base})
}

// NewVLenStringDatatype returns a variable-length string. Its base type is an
// unsigned 8-bit integer, the H5T_NATIVE_UCHAR copy H5T__set_size gives a
// variable-length string (H5T.c:4945-4948).
func NewVLenStringDatatype(pad StringPadding, cset CharacterSet) (*DatatypeMessage, error) {
	base, err := NewIntegerDatatype(1, false)
	if err != nil {
		return nil, err
	}
	flags := vlenString | (uint32(pad)&nibbleMask)<<4 | (uint32(cset)&nibbleMask)<<8
	return newDatatype(&DatatypeMessage{Class: DatatypeVarLen, Size: vlenHeapIDSize, ClassBitField: flags, Base: base})
}

// NewArrayDatatype returns an array of base with the given dimensions,
// encoded as version 3 (no reserved bytes or permutation).
func NewArrayDatatype(base *DatatypeMessage, dims []uint32) (*DatatypeMessage, error) {
	if base == nil {
		return nil, errors.New("array base type is nil")
	}
	size := uint64(base.Size)
	for _, d := range dims {
		size *= uint64(d)
		if size > math.MaxUint32 {
			return nil, errors.New("array size overflows 32 bits")
		}
	}
	return newDatatype(&DatatypeMessage{
		Class: DatatypeArray, Version: dtypeVersion3, Size: uint32(size), ArrayDims: dims, Base: base, //nolint:gosec // G115: checked against MaxUint32 above
	})
}

// checkNesting rejects a datatype nested deeper than maxDatatypeNesting
// levels, the decoder's limit, before the encoder recurses into it. It also
// stops a member or base type that refers back to an enclosing type.
func checkNesting(dt *DatatypeMessage, depth int) error {
	if dt == nil {
		return nil
	}
	if depth > maxDatatypeNesting {
		return fmt.Errorf("encode datatype: nesting deeper than %d levels", maxDatatypeNesting)
	}
	for _, m := range dt.Members {
		if err := checkNesting(m.Type, depth+1); err != nil {
			return err
		}
	}
	return checkNesting(dt.Base, depth+1)
}
