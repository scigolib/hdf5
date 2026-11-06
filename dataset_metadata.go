package hdf5

import (
	"fmt"
	"strconv"

	"github.com/scigolib/hdf5/internal/core"
)

// This file is the public, structured view of a dataset's metadata (ADR-004
// §2): its shape, datatype and storage layout, the information the C library
// returns from H5Dget_space, H5Dget_type and H5Dget_create_plist. Everything
// is built from the dataset's validated descriptor, the same state every
// reader works from, and every call returns fresh copies: a caller can change
// a result without changing the dataset.

// DatatypeClass is the class of a datatype, the Go counterpart of
// H5T_class_t (H5Tpublic.h:30-46). The values are those of the C enum.
type DatatypeClass int

// Datatype classes (H5Tpublic.h:32-43).
const (
	ClassInteger   DatatypeClass = 0  // H5T_INTEGER: fixed-point integers.
	ClassFloat     DatatypeClass = 1  // H5T_FLOAT: floating-point numbers.
	ClassTime      DatatypeClass = 2  // H5T_TIME: date and time.
	ClassString    DatatypeClass = 3  // H5T_STRING: fixed- or variable-length strings.
	ClassBitfield  DatatypeClass = 4  // H5T_BITFIELD: bit fields.
	ClassOpaque    DatatypeClass = 5  // H5T_OPAQUE: uninterpreted bytes with a tag.
	ClassCompound  DatatypeClass = 6  // H5T_COMPOUND: records of named members.
	ClassReference DatatypeClass = 7  // H5T_REFERENCE: object or region references.
	ClassEnum      DatatypeClass = 8  // H5T_ENUM: named integer values.
	ClassVLen      DatatypeClass = 9  // H5T_VLEN: variable-length sequences.
	ClassArray     DatatypeClass = 10 // H5T_ARRAY: fixed-size arrays of a base type.
	ClassComplex   DatatypeClass = 11 // H5T_COMPLEX: complex numbers (HDF5 2.0).
)

var metaClassNames = map[DatatypeClass]string{
	ClassInteger:   "integer",
	ClassFloat:     "float",
	ClassTime:      "time",
	ClassString:    "string",
	ClassBitfield:  "bitfield",
	ClassOpaque:    "opaque",
	ClassCompound:  "compound",
	ClassReference: "reference",
	ClassEnum:      "enum",
	ClassVLen:      "vlen",
	ClassArray:     "array",
	ClassComplex:   "complex",
}

// String returns the class name in lower case ("integer", "compound", ...).
func (c DatatypeClass) String() string {
	return metaEnumName(metaClassNames, c, "class")
}

// ByteOrder is the byte order of a datatype, the Go counterpart of
// H5T_order_t (H5Tpublic.h:53-61). The values are those of the C enum.
type ByteOrder int

// Byte orders (H5Tpublic.h:55-59).
const (
	OrderLittleEndian ByteOrder = 0 // H5T_ORDER_LE.
	OrderBigEndian    ByteOrder = 1 // H5T_ORDER_BE.
	OrderVAX          ByteOrder = 2 // H5T_ORDER_VAX: VAX mixed-endian floating point.
	OrderMixed        ByteOrder = 3 // H5T_ORDER_MIXED: a compound whose members differ.
	OrderNone         ByteOrder = 4 // H5T_ORDER_NONE: no byte order (strings, opaque, references).
)

var metaOrderNames = map[ByteOrder]string{
	OrderLittleEndian: "little-endian",
	OrderBigEndian:    "big-endian",
	OrderVAX:          "VAX",
	OrderMixed:        "mixed",
	OrderNone:         "none",
}

// String returns the byte order's name ("little-endian", "big-endian", ...).
func (o ByteOrder) String() string {
	return metaEnumName(metaOrderNames, o, "order")
}

// StringPadding is how a string is terminated or padded, the Go counterpart
// of H5T_str_t (H5Tpublic.h). The values are those of the C enum.
type StringPadding int

// String paddings (H5Tpublic.h H5T_str_t).
const (
	PaddingNullTerm StringPadding = 0 // H5T_STR_NULLTERM: null-terminated.
	PaddingNullPad  StringPadding = 1 // H5T_STR_NULLPAD: padded with zero bytes.
	PaddingSpacePad StringPadding = 2 // H5T_STR_SPACEPAD: padded with spaces (Fortran).
)

var metaPaddingNames = map[StringPadding]string{
	PaddingNullTerm: "null-terminated",
	PaddingNullPad:  "null-padded",
	PaddingSpacePad: "space-padded",
}

// String returns the padding's name ("null-terminated", ...).
func (p StringPadding) String() string {
	return metaEnumName(metaPaddingNames, p, "padding")
}

// Charset is the character set of a string, the Go counterpart of
// H5T_cset_t (H5Tpublic.h). The values are those of the C enum.
type Charset int

// Character sets (H5Tpublic.h H5T_cset_t).
const (
	CharsetASCII Charset = 0 // H5T_CSET_ASCII.
	CharsetUTF8  Charset = 1 // H5T_CSET_UTF8.
)

var metaCharsetNames = map[Charset]string{
	CharsetASCII: "ASCII",
	CharsetUTF8:  "UTF-8",
}

// String returns the character set's name ("ASCII" or "UTF-8").
func (c Charset) String() string {
	return metaEnumName(metaCharsetNames, c, "charset")
}

// DatatypeInfo describes a dataset's datatype: what H5Tget_class,
// H5Tget_size, H5Tget_order, H5Tget_sign and the class-specific H5Tget_*
// functions report for the type H5Dget_type returns.
//
// Fields that do not apply to the class keep their zero value. Order, Signed,
// Padding and Charset follow the C rules for derived types: an enumeration,
// array, variable-length sequence or string, or complex number reports the
// byte order and sign of its base type (H5Torder.c:114-116, H5Tfixed.c:73-75;
// a variable-length string reports the order of its stored character type,
// which C creates from H5T_NATIVE_UCHAR in the writer's byte order,
// H5T.c:4945-4948), and padding and
// character set are those of the first string in the base chain
// (H5Tstrpad.c:50-51, H5Tcset.c:49-50).
//
// Size and member offsets describe an element as stored in the file. This
// deviates from C for variable-length types only: H5Dget_type converts the
// type to its in-memory form (H5Dint.c:3980), whose size is that of a C
// pointer or hvl_t, which has no meaning in Go.
type DatatypeInfo struct {
	// Class is the datatype class. A variable-length string is ClassString
	// with VariableString set, as C reports it (H5T.c:2808-2814); a
	// variable-length sequence of anything else is ClassVLen.
	Class DatatypeClass

	// Size is the size of one element in bytes (H5Tget_size).
	Size uint64

	// Order is the byte order (H5Tget_order). A compound reports the order of
	// its members: the first one other than OrderNone, or OrderMixed when they
	// differ (H5Torder.c:118-152).
	Order ByteOrder

	// Signed reports a two's complement integer (H5Tget_sign returns
	// H5T_SGN_2); false for unsigned integers and for every class without a
	// sign.
	Signed bool

	// Padding and Charset are the string padding and character set
	// (H5Tget_strpad, H5Tget_cset).
	Padding StringPadding
	Charset Charset

	// VariableString reports a variable-length string (H5Tis_variable_str).
	VariableString bool

	// Members are the members of a compound, in file order (H5Tget_nmembers,
	// H5Tget_member_name, H5Tget_member_offset, H5Tget_member_type).
	Members []CompoundMemberInfo

	// EnumMembers are the names and values of an enumeration
	// (H5Tget_member_name, H5Tget_member_value).
	EnumMembers []EnumMemberInfo

	// ArrayDims are the dimensions of an array type (H5Tget_array_dims2).
	ArrayDims []uint64

	// Tag is the tag of an opaque type (H5Tget_tag).
	Tag string

	// Base is the parent type of an enumeration, array, variable-length
	// sequence or complex number (H5Tget_super), nil for every other class.
	Base *DatatypeInfo
}

// CompoundMemberInfo is one member of a compound datatype.
type CompoundMemberInfo struct {
	Name   string       // Member name.
	Offset uint64       // Byte offset of the member inside the record.
	Type   DatatypeInfo // Member datatype.
}

// EnumMemberInfo is one member of an enumeration.
//
// Value is the member's value read from the base integer type with its size,
// byte order, bit offset, precision and sign. For a 64-bit unsigned base,
// Value holds the two's-complement bit pattern of the unsigned value: a value
// u of 2^63 or more is int64(u), and uint64(Value) recovers it. This is the
// convention of [WithEnumValues] and [EnumType].
type EnumMemberInfo struct {
	Name  string
	Value int64
}

// LayoutClass is how a dataset's raw data is stored, the Go counterpart of
// H5D_layout_t (H5Dpublic.h:42-49). The values are those of the C enum.
type LayoutClass int

// Storage layouts (H5Dpublic.h:44-47).
const (
	LayoutCompact    LayoutClass = 0 // H5D_COMPACT: data inside the object header.
	LayoutContiguous LayoutClass = 1 // H5D_CONTIGUOUS: one block in the file.
	LayoutChunked    LayoutClass = 2 // H5D_CHUNKED: fixed-size chunks found through an index.

	// LayoutVirtual is H5D_VIRTUAL: data mapped from other datasets. Such
	// datasets cannot be opened by this library yet, so no accessor returns it.
	LayoutVirtual LayoutClass = 3
)

var metaLayoutNames = map[LayoutClass]string{
	LayoutCompact:    "compact",
	LayoutContiguous: "contiguous",
	LayoutChunked:    "chunked",
	LayoutVirtual:    "virtual",
}

// String returns the layout's name ("compact", "contiguous", ...).
func (l LayoutClass) String() string {
	return metaEnumName(metaLayoutNames, l, "layout")
}

// FilterID identifies a filter of a dataset's filter pipeline, the Go
// counterpart of H5Z_filter_t. Values below 256 are reserved for the HDF5
// library; others are registered with The HDF Group (H5Zpublic.h).
type FilterID uint16

// Filters predefined by the HDF5 library (H5Zpublic.h:69-89).
const (
	FilterDeflate     FilterID = 1 // H5Z_FILTER_DEFLATE: zlib compression.
	FilterShuffle     FilterID = 2 // H5Z_FILTER_SHUFFLE: byte shuffle.
	FilterFletcher32  FilterID = 3 // H5Z_FILTER_FLETCHER32: Fletcher-32 checksum.
	FilterSZIP        FilterID = 4 // H5Z_FILTER_SZIP: SZIP compression.
	FilterNBit        FilterID = 5 // H5Z_FILTER_NBIT: N-bit packing.
	FilterScaleOffset FilterID = 6 // H5Z_FILTER_SCALEOFFSET: scale-offset packing.
)

// metaFilterNames are the names the C library registers for its own
// filters (H5Zdeflate.c:39, H5Zshuffle.c:34, H5Zfletcher32.c:30, H5Zszip.c:43,
// H5Znbit.c:91, H5Zscaleoffset.c:97). H5Pget_filter2 falls back to them when
// the pipeline message stores no name (H5Pocpl.c:1154-1160), which version 2
// messages never do for these filters.
var metaFilterNames = map[FilterID]string{
	FilterDeflate:     "deflate",
	FilterShuffle:     "shuffle",
	FilterFletcher32:  "fletcher32",
	FilterSZIP:        "szip",
	FilterNBit:        "nbit",
	FilterScaleOffset: "scaleoffset",
}

// String returns the C library's name of a predefined filter ("deflate",
// "shuffle", ...) and "filter(N)" for any other.
func (id FilterID) String() string {
	return metaEnumName(metaFilterNames, id, "filter")
}

// metaFilterOptional is H5Z_FLAG_OPTIONAL (H5Zpublic.h:125).
const metaFilterOptional = 0x0001

// FilterInfo is one filter of a dataset's filter pipeline, as H5Pget_filter2
// reports it.
type FilterInfo struct {
	// ID identifies the filter.
	ID FilterID

	// Name is the name stored in the file or, when none is stored, the C
	// library's name of a predefined filter; empty for an unnamed filter
	// this library does not know.
	Name string

	// Optional reports a filter whose failure does not fail the write
	// (H5Z_FLAG_OPTIONAL).
	Optional bool

	// ClientData are the filter's parameters (cd_values), for example the
	// compression level of FilterDeflate.
	ClientData []uint32
}

// LayoutInfo describes how a dataset's raw data is stored: what
// H5Pget_layout, H5Pget_chunk and H5Pget_filter2 report for the dataset's
// creation property list.
type LayoutInfo struct {
	// Class is the storage layout.
	Class LayoutClass

	// ChunkDims are the chunk dimensions of a chunked dataset, one per
	// dataspace dimension (H5Pget_chunk; the file stores one more, the
	// element size, which is not reported); nil for other layouts.
	ChunkDims []uint64

	// Filters is the filter pipeline in the order it is applied on write;
	// nil when the dataset has none.
	Filters []FilterInfo
}

// Shape returns the dataset's current dimensions, the dims of
// H5Sget_simple_extent_dims. A scalar or null dataspace has rank 0 and
// returns an empty slice.
func (d *Dataset) Shape() ([]uint64, error) {
	desc, err := d.descriptor()
	if err != nil {
		return nil, err
	}

	ds := desc.Dataspace()
	if !metaHasExtent(ds) {
		return []uint64{}, nil
	}

	return append([]uint64{}, ds.Dimensions...), nil
}

// metaHasExtent reports whether the dataspace has dimensions to report.
// H5Sget_simple_extent_dims reports rank 0 for scalar and null dataspaces
// (H5S_extent_get_dims, H5S.c:957-961); deciding by the class keeps that true
// even for a writer-built scalar message, which still carries Dimensions [1].
func metaHasExtent(ds *core.DataspaceMessage) bool {
	return ds.Type == core.DataspaceSimple
}

// MaxShape returns the dataset's maximum dimensions, the maxdims of
// H5Sget_simple_extent_dims. A dimension that can grow without limit is
// Unlimited (H5S_UNLIMITED). A dataspace that stores no maximum reports its
// current dimensions, as C does (H5S.c:968-972).
func (d *Dataset) MaxShape() ([]uint64, error) {
	desc, err := d.descriptor()
	if err != nil {
		return nil, err
	}

	ds := desc.Dataspace()
	if !metaHasExtent(ds) {
		return []uint64{}, nil
	}
	if ds.MaxDims == nil {
		return append([]uint64{}, ds.Dimensions...), nil
	}

	return append([]uint64{}, ds.MaxDims...), nil
}

// Datatype returns a description of the dataset's datatype. See DatatypeInfo
// for how it relates to the C library's H5Tget_* functions.
//
// An enumeration whose base integer is wider than 8 bytes is not supported:
// its values do not fit EnumMemberInfo.Value, so Datatype returns an error
// for it and for any type that contains it, although C can describe it.
func (d *Dataset) Datatype() (DatatypeInfo, error) {
	desc, err := d.descriptor()
	if err != nil {
		return DatatypeInfo{}, err
	}

	info, err := metaDatatypeInfo(desc.Datatype())
	if err != nil {
		return DatatypeInfo{}, fmt.Errorf("dataset %q: %w", d.name, err)
	}

	return info, nil
}

// Layout returns the dataset's storage layout, chunk dimensions and filter
// pipeline.
func (d *Dataset) Layout() (LayoutInfo, error) {
	desc, err := d.descriptor()
	if err != nil {
		return LayoutInfo{}, err
	}

	return metaLayoutInfo(desc), nil
}

// metaLayoutInfo copies the layout and filter pipeline of a descriptor.
func metaLayoutInfo(desc *core.DatasetDescriptor) LayoutInfo {
	info := LayoutInfo{Class: LayoutClass(desc.Layout().Class)}

	if chunk := desc.ChunkDims(); chunk != nil {
		info.ChunkDims = append([]uint64{}, chunk...)
	}

	if pipeline := desc.FilterPipeline(); pipeline != nil && len(pipeline.Filters) > 0 {
		info.Filters = make([]FilterInfo, len(pipeline.Filters))
		for i, f := range pipeline.Filters {
			id := FilterID(f.ID)
			name := f.Name
			if name == "" {
				name = metaFilterNames[id]
			}

			info.Filters[i] = FilterInfo{
				ID:         id,
				Name:       name,
				Optional:   f.Flags&metaFilterOptional != 0,
				ClientData: append([]uint32(nil), f.ClientData...),
			}
		}
	}

	return info
}

// metaDatatypeInfo builds the public description of a decoded datatype.
func metaDatatypeInfo(dt *core.DatatypeMessage) (DatatypeInfo, error) {
	if dt == nil {
		return DatatypeInfo{}, fmt.Errorf("%w: missing datatype", ErrCorrupt)
	}

	// The core enums carry the C values (H5T_order_t, H5T_str_t, H5T_cset_t),
	// as the public ones do, so the conversions keep the meaning.
	padding, charset := dt.StringProperties()
	info := DatatypeInfo{
		Class:          DatatypeClass(dt.Class),
		Size:           uint64(dt.Size),
		Order:          ByteOrder(dt.Order()),
		Signed:         dt.Signed(),
		VariableString: dt.IsVariableString(),
		Padding:        StringPadding(padding),
		Charset:        Charset(charset),
	}
	if info.VariableString {
		info.Class = ClassString // Externally a VL string is a string (H5T.c:2808-2814).
	}

	if err := metaFillClass(&info, dt); err != nil {
		return DatatypeInfo{}, err
	}

	return info, nil
}

// metaFillClass fills the class-specific fields of info.
func metaFillClass(info *DatatypeInfo, dt *core.DatatypeMessage) error {
	switch dt.Class {
	case core.DatatypeCompound:
		members, err := metaCompoundMembers(dt)
		if err != nil {
			return err
		}
		info.Members = members
	case core.DatatypeEnum:
		members, err := metaEnumMembers(dt)
		if err != nil {
			return err
		}
		info.EnumMembers = members
	case core.DatatypeArray:
		info.ArrayDims = make([]uint64, len(dt.ArrayDims))
		for i, dim := range dt.ArrayDims {
			info.ArrayDims[i] = uint64(dim)
		}
	case core.DatatypeOpaque:
		info.Tag = dt.OpaqueTag
	}

	// H5Tget_super: the parent of an enumeration, array, sequence or complex
	// number. A variable-length string also has a parent internally (its
	// character type), but externally it is a string and has none.
	if dt.Base != nil && !info.VariableString {
		base, err := metaDatatypeInfo(dt.Base)
		if err != nil {
			return err
		}
		info.Base = &base
	}

	return nil
}

// metaCompoundMembers describes the members of a compound datatype.
func metaCompoundMembers(dt *core.DatatypeMessage) ([]CompoundMemberInfo, error) {
	ct, err := core.ParseCompoundType(dt)
	if err != nil {
		return nil, err
	}

	members := make([]CompoundMemberInfo, len(ct.Members))
	for i, m := range ct.Members {
		typ, err := metaDatatypeInfo(m.Type)
		if err != nil {
			return nil, fmt.Errorf("compound member %q: %w", m.Name, err)
		}
		members[i] = CompoundMemberInfo{Name: m.Name, Offset: uint64(m.Offset), Type: typ}
	}

	return members, nil
}

// metaEnumMembers describes the members of an enumeration, decoding each value
// from the base integer type as H5Tget_member_value returns it.
func metaEnumMembers(dt *core.DatatypeMessage) ([]EnumMemberInfo, error) {
	base := dt.Base
	if base == nil {
		return nil, fmt.Errorf("%w: enumeration without a base type", ErrCorrupt)
	}

	size := int(base.Size)
	if size > 8 {
		return nil, fmt.Errorf("%w: enumeration base type of %d bytes "+
			"(EnumMemberInfo.Value holds at most 64 bits)", ErrNotSupported, size)
	}
	if len(dt.EnumValues) != len(dt.EnumNames)*size {
		return nil, fmt.Errorf("%w: enumeration has %d names and %d value bytes of %d-byte values",
			ErrCorrupt, len(dt.EnumNames), len(dt.EnumValues), size)
	}

	bigEndian := base.Order() == core.OrderBigEndian
	signed := base.Signed()

	members := make([]EnumMemberInfo, len(dt.EnumNames))
	for i, name := range dt.EnumNames {
		raw := dt.EnumValues[i*size : (i+1)*size]
		members[i] = EnumMemberInfo{
			Name:  name,
			Value: metaIntegerValue(raw, bigEndian, signed, base.BitOffset, base.Precision),
		}
	}

	return members, nil
}

// metaIntegerValue decodes an integer of len(raw) ≤ 8 bytes whose significant
// bits are precision bits at bitOffset (H5T_atomic_t offset and prec; zero
// precision means all bits), sign-extending a signed value.
func metaIntegerValue(raw []byte, bigEndian, signed bool, bitOffset, precision uint16) int64 {
	var v uint64
	for i := range raw {
		b := raw[i]
		if bigEndian {
			b = raw[len(raw)-1-i]
		}
		v |= uint64(b) << (8 * uint(i))
	}

	bits := uint(len(raw)) * 8
	prec := uint(precision)
	if prec == 0 || prec+uint(bitOffset) > bits {
		prec = bits - min(uint(bitOffset), bits)
	}

	v >>= uint(bitOffset)
	if prec < 64 {
		v &= 1<<prec - 1
		if signed && prec > 0 && v&(1<<(prec-1)) != 0 {
			v |= ^uint64(0) << prec
		}
	}

	return int64(v) //nolint:gosec // G115: the bits are kept on purpose; see EnumMemberInfo.
}

// metaEnumName returns the name of v from names, or prefix(N) for a value
// outside the table.
func metaEnumName[T ~int | ~uint16](names map[T]string, v T, prefix string) string {
	if name, ok := names[v]; ok {
		return name
	}

	return prefix + "(" + strconv.Itoa(int(v)) + ")"
}

// metaInfoString formats a descriptor the way Info always has: datatype class and
// size, dataspace and layout.
func metaInfoString(desc *core.DatasetDescriptor) string {
	dt := desc.Datatype()
	class := DatatypeClass(dt.Class)
	if dt.IsVariableString() {
		class = ClassString
	}

	return fmt.Sprintf("Dataset: %s (size=%d bytes), %s, %s",
		class, dt.Size, desc.Dataspace().String(), desc.Layout().String())
}
