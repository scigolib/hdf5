package hdf5

// Public compound datatype API (issue #65). A compound type is described with
// the root package's own types and validated with the rules of the C library's
// H5Tinsert / H5T__insert (H5Tcompound.c:317-470); the datatype message is
// written by the one datatype codec (internal/core/datatype_codec.go, ADR-004),
// so the files are readable by the HDF5 C library, h5py and H5Web.

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/scigolib/hdf5/internal/core"
)

// maxCompoundMembers is the most members a compound datatype message can
// list: the count is the low 16 bits of the class bit field
// (H5Odtype.c:358, :1239).
const maxCompoundMembers = 0xFFFF

// CompoundMemberType is the datatype of one member of a compound type.
//
// It is implemented by:
//   - [Datatype] for the fixed-size scalar types: Int8 … Int64, Uint8 …
//     Uint64, Float32, Float64, ObjectReference and RegionReference;
//   - [FixedString] for a fixed-length string;
//   - [ArrayType] for a fixed-size array of another member type;
//   - [EnumType] for an enumeration over an integer type;
//   - *[CompoundType] for a nested compound.
//
// Other [Datatype] values (String, the Array*, Enum*, VLen* and Opaque
// constants) are rejected by [NewCompoundType] with an error that names the
// type to use instead. The interface cannot be implemented outside this
// package.
type CompoundMemberType interface {
	compoundMemberDatatype() (*core.DatatypeMessage, error)
}

// compoundMemberDatatype implements [CompoundMemberType] for the scalar types.
func (dt Datatype) compoundMemberDatatype() (*core.DatatypeMessage, error) {
	switch dt {
	case Int8, Int16, Int32, Int64, Uint8, Uint16, Uint32, Uint64, Float32, Float64,
		ObjectReference, RegionReference:
		info, err := getDatatypeInfo(dt, &datasetConfig{})
		if err != nil {
			return nil, err
		}
		return datatypeFromInfo(info)
	case String:
		return nil, errors.New("hdf5.String has no length: use hdf5.FixedString{Size: n}")
	case ArrayInt8, ArrayInt16, ArrayInt32, ArrayInt64, ArrayUint8, ArrayUint16, ArrayUint32,
		ArrayUint64, ArrayFloat32, ArrayFloat64:
		return nil, errors.New("array datatype constants have no dimensions: use hdf5.ArrayType")
	case EnumInt8, EnumInt16, EnumInt32, EnumInt64, EnumUint8, EnumUint16, EnumUint32, EnumUint64:
		return nil, errors.New("enum datatype constants have no members: use hdf5.EnumType")
	case Opaque, VLenString, VLenInt32, VLenInt64, VLenFloat32, VLenFloat64, VLenUint32,
		VLenUint64, VLenUint8:
		return nil, fmt.Errorf("datatype %d is not supported as a compound member: %w", dt, ErrNotSupported)
	default:
		return nil, fmt.Errorf("unknown datatype %d", dt)
	}
}

// FixedString is a fixed-length string member type of Size bytes. The zero
// Padding and Charset are those of the C library's H5T_C_S1: null-terminated
// ([PaddingNullTerm], a value holds at most Size-1 bytes) and ASCII.
// [PaddingNullPad] and [PaddingSpacePad] let a value fill all Size bytes. The
// bytes of a value are not checked against the character set, as in C.
type FixedString struct {
	Size    uint32
	Padding StringPadding
	Charset Charset
}

// compoundMemberDatatype implements [CompoundMemberType].
func (s FixedString) compoundMemberDatatype() (*core.DatatypeMessage, error) {
	if s.Size == 0 {
		// H5Tset_size rejects a zero size (H5T.c:3044, "size must be positive").
		return nil, errors.New("fixed string size must be positive")
	}
	switch s.Padding {
	case PaddingNullTerm, PaddingNullPad, PaddingSpacePad:
	default:
		// H5Tset_strpad: "illegal string pad type" (H5Tstrpad.c:100-101).
		return nil, fmt.Errorf("illegal string padding %d", s.Padding)
	}
	switch s.Charset {
	case CharsetASCII, CharsetUTF8:
	default:
		// H5Tset_cset (H5Tcset.c:89-90).
		return nil, fmt.Errorf("illegal character set %d", s.Charset)
	}
	return core.NewStringDatatype(s.Size, core.StringPadding(s.Padding), core.CharacterSet(s.Charset)) //nolint:gosec // G115: checked to 0..2 and 0..1 above
}

// maxArrayRank is the most dimensions an array datatype may have, H5S_MAX_RANK.
const maxArrayRank = 32

// ArrayType is a fixed-size, multi-dimensional array member type: Dims
// elements of Elem in row-major order (H5Tarray_create2). It has 1 to 32
// dimensions (H5S_MAX_RANK, H5Tarray.c:91-92); every dimension must be
// positive and fit in 32 bits, the width of the stored dimension
// (H5Tarray.c:96-97; UINT32ENCODE in H5Odtype.c:1391).
type ArrayType struct {
	Elem CompoundMemberType
	Dims []uint64
}

// compoundMemberDatatype implements [CompoundMemberType].
func (a ArrayType) compoundMemberDatatype() (*core.DatatypeMessage, error) {
	if a.Elem == nil {
		return nil, errors.New("array element type is nil")
	}
	if len(a.Dims) == 0 || len(a.Dims) > maxArrayRank {
		// H5Tarray_create2: "invalid dimensionality" (H5Tarray.c:91-92).
		return nil, fmt.Errorf("array type has %d dimensions, need 1 to %d", len(a.Dims), maxArrayRank)
	}
	dims := make([]uint32, len(a.Dims))
	for i, d := range a.Dims {
		if d == 0 || d > math.MaxUint32 {
			return nil, fmt.Errorf("array dimension %d is %d, must be 1 to %d", i, d, uint32(math.MaxUint32))
		}
		dims[i] = uint32(d)
	}
	base, err := a.Elem.compoundMemberDatatype()
	if err != nil {
		return nil, fmt.Errorf("array element: %w", err)
	}
	return core.NewArrayDatatype(base, dims)
}

// EnumType is an enumeration member type over an integer Base type (Int8 …
// Uint64): Names[i] has the value Values[i]. Names must be unique and
// non-empty, values unique and representable in Base (H5Tenum_create and
// H5Tenum_insert, H5Tenum.c:54, :133, :172-176).
//
// For a Uint64 base, Values hold the two's-complement bit pattern of the
// unsigned value: a value u of 2^63 or more is given as int64(u), and
// uint64(Values[i]) recovers it. [WithEnumValues] and
// [EnumMemberInfo].Value use the same convention.
type EnumType struct {
	Base   Datatype
	Names  []string
	Values []int64
}

// compoundMemberDatatype implements [CompoundMemberType].
func (e EnumType) compoundMemberDatatype() (*core.DatatypeMessage, error) {
	switch e.Base {
	case Int8, Int16, Int32, Int64, Uint8, Uint16, Uint32, Uint64:
	default:
		return nil, fmt.Errorf("enum base type %d is not an integer type", e.Base)
	}
	if len(e.Names) == 0 || len(e.Names) != len(e.Values) {
		return nil, fmt.Errorf("enum needs one value per name (got %d names, %d values)", len(e.Names), len(e.Values))
	}
	base, err := e.Base.compoundMemberDatatype()
	if err != nil {
		return nil, err
	}
	signed := base.ClassBitField&datatypeSignedFlag != 0
	size := int(base.Size)
	// Grown by append: a capacity hint computed from base.Size would be a
	// make sized from an element size, which TestResultSizingGuard reserves
	// for DatasetDescriptor.ResultSize.
	var packed []byte
	names := make(map[string]bool, len(e.Names))
	values := make(map[int64]bool, len(e.Values))
	for i, name := range e.Names {
		v := e.Values[i]
		if name == "" {
			// H5Tenum_insert: "no name specified" (H5Tenum.c:133).
			return nil, fmt.Errorf("enum member %d: no name specified", i)
		}
		// H5T__enum_insert rejects duplicate names and values (H5Tenum.c:172-176).
		if names[name] {
			return nil, fmt.Errorf("enum name %q is not unique", name)
		}
		if values[v] {
			return nil, fmt.Errorf("enum value %d (%q) is not unique", v, name)
		}
		names[name], values[v] = true, true
		// Every int64 is a valid Uint64 bit pattern (see the type's doc).
		uint64Pattern := !signed && size == 8
		if !uint64Pattern && !compoundIntFits(v, size, signed) {
			return nil, fmt.Errorf("enum value %d (%q) does not fit the %d-byte base type", v, name, size)
		}
		packed = compoundAppendLE(packed, uint64(v), size) //nolint:gosec // G115: range checked by compoundIntFits
	}
	return core.NewEnumDatatype(base, e.Names, packed)
}

// CompoundMember is one member of a compound type: a name, the byte offset of
// the member within the compound, and its type.
type CompoundMember struct {
	Name   string
	Offset uint32
	Type   CompoundMemberType
}

// CompoundType is a validated compound (struct-like) datatype. Create it with
// [NewCompoundType] or [NewPackedCompoundType] and use it with
// [FileWriter.CreateCompound], or as the type of a member of another compound.
type CompoundType struct {
	members []CompoundMember
	dt      *core.DatatypeMessage // decoded form of the encoded message
}

// NewCompoundType returns a compound type of size bytes with the given
// members, validated with the C library's rules (H5Tcreate, H5Tinsert and
// H5T__insert in H5T.c and H5Tcompound.c):
//
//   - size is positive (H5T.c:2480-2481);
//   - there is at least one member and at most 65535 (the decoder rejects a
//     compound without members, H5Odtype.c:358-360);
//   - every name is non-empty (H5Tcompound.c:333-334) and unique
//     (H5Tcompound.c:405-408), and contains no NUL byte;
//   - no member overlaps another (H5Tcompound.c:410-417) or extends past the
//     end of the compound (H5Tcompound.c:420-421).
//
// Gaps between members and padding after the last one are allowed, as in C:
// their bytes are written as given to [DatasetWriter.WriteRaw], and as zeros
// by [DatasetWriter.Write]. Members keep the given order, which is the order
// in the file.
func NewCompoundType(size uint32, members []CompoundMember) (*CompoundType, error) {
	// The type keeps its own copy: later changes to the caller's slices
	// (an ArrayType's Dims, an EnumType's Names or Values) do not reach it.
	members = cloneCompoundMembers(members)
	if size == 0 {
		return nil, errors.New("compound type: size must be positive")
	}
	if len(members) == 0 || len(members) > maxCompoundMembers {
		return nil, fmt.Errorf("compound type: %d members, need 1 to %d", len(members), maxCompoundMembers)
	}
	coreMembers := make([]core.CompoundMember, 0, len(members))
	for i, m := range members {
		cm, err := checkCompoundMember(size, members[:i], coreMembers, i, m)
		if err != nil {
			return nil, err
		}
		coreMembers = append(coreMembers, cm)
	}
	dt, err := core.NewCompoundDatatype(size, coreMembers)
	if err != nil {
		return nil, fmt.Errorf("compound type: %w", err)
	}
	return &CompoundType{members: members, dt: dt}, nil
}

// cloneCompoundMembers returns a deep copy of members: the slices inside an
// ArrayType or EnumType are copied, at every level of nesting. A nested
// *CompoundType is immutable and shared.
func cloneCompoundMembers(members []CompoundMember) []CompoundMember {
	if members == nil {
		return nil
	}
	out := make([]CompoundMember, len(members))
	for i, m := range members {
		m.Type = cloneCompoundMemberType(m.Type)
		out[i] = m
	}
	return out
}

// cloneCompoundMemberType returns a copy of t that shares no slice with it.
func cloneCompoundMemberType(t CompoundMemberType) CompoundMemberType {
	switch v := t.(type) {
	case ArrayType:
		v.Elem = cloneCompoundMemberType(v.Elem)
		v.Dims = append([]uint64(nil), v.Dims...)
		return v
	case EnumType:
		v.Names = append([]string(nil), v.Names...)
		v.Values = append([]int64(nil), v.Values...)
		return v
	default:
		return t
	}
}

// checkCompoundMember applies the H5Tinsert / H5T__insert checks to member i
// against the members before it and returns its datatype.
func checkCompoundMember(size uint32, prev []CompoundMember, prevCore []core.CompoundMember, i int, m CompoundMember) (core.CompoundMember, error) {
	if m.Name == "" {
		return core.CompoundMember{}, fmt.Errorf("compound member %d: no member name", i)
	}
	for j := range m.Name {
		if m.Name[j] == 0 {
			return core.CompoundMember{}, fmt.Errorf("compound member %d: name %q contains a NUL byte", i, m.Name)
		}
	}
	for _, p := range prev {
		if p.Name == m.Name {
			return core.CompoundMember{}, fmt.Errorf("compound member %d: member name %q is not unique", i, m.Name)
		}
	}
	if m.Type == nil {
		return core.CompoundMember{}, fmt.Errorf("compound member %q: type is nil", m.Name)
	}
	dt, err := m.Type.compoundMemberDatatype()
	if err != nil {
		return core.CompoundMember{}, fmt.Errorf("compound member %q: %w", m.Name, err)
	}
	lo, hi := uint64(m.Offset), uint64(m.Offset)+uint64(dt.Size)
	for _, p := range prevCore {
		plo, phi := uint64(p.Offset), uint64(p.Offset)+uint64(p.Type.Size)
		if (lo <= plo && hi > plo) || (plo <= lo && phi > lo) {
			return core.CompoundMember{}, fmt.Errorf("compound member %q at [%d,%d) overlaps member %q at [%d,%d)",
				m.Name, lo, hi, p.Name, plo, phi)
		}
	}
	if hi > uint64(size) {
		return core.CompoundMember{}, fmt.Errorf("compound member %q at [%d,%d) extends past the end of the %d-byte compound type",
			m.Name, lo, hi, size)
	}
	return core.CompoundMember{Name: m.Name, Offset: m.Offset, Type: dt}, nil
}

// NewPackedCompoundType returns a compound type whose members are laid out
// one after another in the given order, without gaps, and whose size is the
// sum of the member sizes. Compounds nested in it, directly or as the
// element of an [ArrayType], are packed the same way, with their members
// sorted by offset: the layout H5Tpack gives, which packs recursively
// (H5T__pack, H5Tcompound.c:482-560, :512-530). The caller's nested types
// are not changed. The Offset of every member must be zero; the computed
// offsets are reported by [CompoundType.Members]. All other checks are those
// of [NewCompoundType].
func NewPackedCompoundType(members []CompoundMember) (*CompoundType, error) {
	if len(members) == 0 || len(members) > maxCompoundMembers {
		return nil, fmt.Errorf("compound type: %d members, need 1 to %d", len(members), maxCompoundMembers)
	}
	placed := make([]CompoundMember, len(members))
	var offset uint64
	for i, m := range members {
		if m.Offset != 0 {
			return nil, fmt.Errorf("packed compound member %d (%q): offset %d given, offsets are computed", i, m.Name, m.Offset)
		}
		if m.Type == nil {
			return nil, fmt.Errorf("compound member %q: type is nil", m.Name)
		}
		packedType, err := packCompoundMemberType(m.Type)
		if err != nil {
			return nil, fmt.Errorf("compound member %q: %w", m.Name, err)
		}
		dt, err := packedType.compoundMemberDatatype()
		if err != nil {
			return nil, fmt.Errorf("compound member %q: %w", m.Name, err)
		}
		m.Type = packedType
		m.Offset = uint32(offset)
		placed[i] = m
		offset += uint64(dt.Size)
		if offset > math.MaxUint32 {
			return nil, fmt.Errorf("packed compound type: size exceeds %d bytes at member %q", uint32(math.MaxUint32), m.Name)
		}
	}
	return NewCompoundType(uint32(offset), placed)
}

// packCompoundMemberType returns t with every compound inside it packed, as
// H5T__pack does: it packs the parent of an array (H5Tcompound.c:497-505)
// and each member of a compound (:512-519), then sorts the members by
// offset (H5T__sort_value, :522) and places them without gaps (:524-527).
func packCompoundMemberType(t CompoundMemberType) (CompoundMemberType, error) {
	switch v := t.(type) {
	case *CompoundType:
		if v == nil || v.dt == nil {
			return nil, errors.New("nested compound type is nil or was not created with NewCompoundType")
		}
		members := v.Members()
		sort.SliceStable(members, func(i, j int) bool { return members[i].Offset < members[j].Offset })
		for i := range members {
			members[i].Offset = 0
		}
		return NewPackedCompoundType(members)
	case ArrayType:
		if v.Elem == nil {
			return v, nil // reported by compoundMemberDatatype
		}
		elem, err := packCompoundMemberType(v.Elem)
		if err != nil {
			return nil, fmt.Errorf("array element: %w", err)
		}
		v.Elem = elem
		return v, nil
	default:
		return t, nil
	}
}

// Size returns the size of one compound value in bytes.
func (c *CompoundType) Size() uint32 { return c.dt.Size }

// Members returns a copy of the members, in file order, with their offsets.
// The copy is deep: changing it does not change the type.
func (c *CompoundType) Members() []CompoundMember {
	return cloneCompoundMembers(c.members)
}

// compoundMemberDatatype implements [CompoundMemberType]: a compound nested
// in another one.
func (c *CompoundType) compoundMemberDatatype() (*core.DatatypeMessage, error) {
	if c == nil || c.dt == nil {
		return nil, errors.New("nested compound type is nil or was not created with NewCompoundType")
	}
	return c.dt, nil
}

// CreateCompound creates a dataset whose elements have the compound type ct.
//
// The options are those of [FileWriter.CreateDataset] that concern storage:
// [WithChunkDims], [WithMaxDims], [WithGZIPCompression], [WithShuffle] and
// [WithFletcher32]. Filters and maximum dimensions need chunked layout, as in
// the C library (H5Dint.c:1309, H5Dcontig.c:457). The datatype options
// ([WithStringSize], [WithArrayDims], [WithEnumValues], [WithOpaqueTag]) do
// not apply to a compound and are an error.
//
// Write the data with [DatasetWriter.Write], passing a []map[string]any with
// one map per element (member name to value), or with
// [DatasetWriter.WriteRaw], passing the packed little-endian bytes of all
// elements. Write takes, per member type:
//
//   - integer types: any Go integer that fits the member's range;
//   - Float32, Float64: float32 or float64 (a float64 in a Float32 member is
//     rounded to the nearest float32; a finite one outside the float32
//     range is an error);
//   - [FixedString]: a string of at most Size-1 bytes;
//   - [EnumType]: a member name, or one of the enumeration's values;
//   - [ArrayType]: a slice or Go array of the element values, row-major;
//   - *[CompoundType]: a map[string]any;
//   - ObjectReference: a uint64 object address; RegionReference: 12 bytes.
//
// Every member needs a value and every key must name a member; a value that
// does not fit is an error, never truncated. Padding bytes are written as
// zeros.
func (fw *FileWriter) CreateCompound(name string, ct *CompoundType, dims []uint64, opts ...DatasetOption) (*DatasetWriter, error) {
	if ct == nil || ct.dt == nil {
		return nil, errors.New("compound type is nil or was not created with NewCompoundType")
	}
	return fw.createCompoundDataset(name, ct.dt, dims, opts)
}

// compoundDatasetConfig applies opts and rejects every option a compound
// dataset cannot honor, so none is silently ignored.
func compoundDatasetConfig(dims []uint64, opts []DatasetOption) (*datasetConfig, error) {
	config := &datasetConfig{}
	for _, opt := range opts {
		opt(config)
	}
	switch {
	case config.stringSize != 0:
		return nil, errors.New("WithStringSize does not apply to a compound dataset: give the member a FixedString type")
	case len(config.arrayDims) != 0:
		return nil, errors.New("WithArrayDims does not apply to a compound dataset: give the member an ArrayType")
	case len(config.enumNames) != 0 || len(config.enumValues) != 0:
		return nil, errors.New("WithEnumValues does not apply to a compound dataset: give the member an EnumType")
	case config.opaqueTag != "" || config.opaqueSize != 0:
		return nil, errors.New("WithOpaqueTag does not apply to a compound dataset")
	}
	if err := validateMaxDims(dims, config); err != nil {
		return nil, err
	}
	if len(config.chunkDims) == 0 && (config.pipeline != nil || config.enableShuffle) {
		// H5D__create: "filters can only be used with chunked layout" (H5Dint.c:1309).
		return nil, errors.New("filters can only be used with chunked layout (use WithChunkDims)")
	}
	return config, nil
}

// createCompoundDataset creates a dataset of compound datatype dt, which is in
// the decoded form of the codec (its Members set).
func (fw *FileWriter) createCompoundDataset(name string, dt *core.DatatypeMessage, dims []uint64, opts []DatasetOption) (*DatasetWriter, error) {
	if err := validateDatasetName(name); err != nil {
		return nil, err
	}
	if err := validateDimensions(dims); err != nil {
		return nil, err
	}
	config, err := compoundDatasetConfig(dims, opts)
	if err != nil {
		return nil, err
	}
	datatypeData, err := core.EncodeDatatypeMessage(dt)
	if err != nil {
		return nil, fmt.Errorf("failed to encode compound datatype: %w", err)
	}
	if len(config.chunkDims) > 0 {
		if err := validateChunkDims(dims, config.chunkDims, config.maxDims); err != nil {
			return nil, err
		}
		return fw.createChunkedDatasetEncoded(name, dims, config, datatypeData, dt.Size, dt)
	}
	return fw.createContiguousCompound(name, dt, datatypeData, dims)
}
