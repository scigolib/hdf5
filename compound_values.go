package hdf5

// Encoding of compound values for DatasetWriter.Write: one map per element,
// from member name to value, the shape Dataset.ReadCompound returns. Values
// are converted only where no information is lost (integers range-checked,
// strings length-checked); anything else is an error, never a silent
// truncation. Bytes not covered by a member (padding) are written as zeros.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"

	"github.com/scigolib/hdf5/internal/core"
)

// encodeCompoundData encodes the elements in data, a []map[string]any (or
// the []CompoundValue Dataset.ReadCompound returns), as expectedSize bytes of
// compound datatype dt.
func encodeCompoundData(data any, dt *core.DatatypeMessage, expectedSize uint64) ([]byte, error) {
	var records []map[string]any
	switch v := data.(type) {
	case []map[string]any:
		records = v
	case []core.CompoundValue:
		records = make([]map[string]any, len(v))
		for i, r := range v {
			records[i] = r
		}
	default:
		return nil, fmt.Errorf("compound dataset expects []map[string]any, got %T (use WriteRaw for packed bytes)", data)
	}
	if dt.Size == 0 || uint64(len(records))*uint64(dt.Size) != expectedSize {
		return nil, fmt.Errorf("compound dataset holds %d bytes (%d-byte elements), got %d elements",
			expectedSize, dt.Size, len(records))
	}
	buf := make([]byte, expectedSize)
	for i, r := range records {
		elem := buf[uint64(i)*uint64(dt.Size):][:dt.Size]
		if err := encodeCompoundRecord(elem, dt, r, ""); err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
	}
	return buf, nil
}

// encodeCompoundRecord encodes one compound value r into elem. path names
// the enclosing members for error messages.
func encodeCompoundRecord(elem []byte, dt *core.DatatypeMessage, r map[string]any, path string) error {
	for _, m := range dt.Members {
		v, ok := r[m.Name]
		if !ok {
			return fmt.Errorf("member %q: no value", path+m.Name)
		}
		if err := encodeCompoundMemberValue(elem[m.Offset:][:m.Type.Size], m.Type, v, path+m.Name); err != nil {
			return err
		}
	}
	if len(r) != len(dt.Members) {
		return compoundUnknownMemberError(dt, r, path)
	}
	return nil
}

// compoundUnknownMemberError names the first key of r (in sorted order, so the
// message is stable) that is not a member of dt.
func compoundUnknownMemberError(dt *core.DatatypeMessage, r map[string]any, path string) error {
	known := make(map[string]bool, len(dt.Members))
	for _, m := range dt.Members {
		known[m.Name] = true
	}
	keys := make([]string, 0, len(r))
	for k := range r {
		if !known[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return fmt.Errorf("member %q: not a member of the compound type", path+keys[0])
}

// encodeCompoundMemberValue encodes v as a value of dt into dst (len(dst) == dt.Size).
func encodeCompoundMemberValue(dst []byte, dt *core.DatatypeMessage, v any, path string) error {
	var err error
	switch dt.Class {
	case core.DatatypeFixed:
		err = encodeCompoundInteger(dst, dt, v)
	case core.DatatypeFloat:
		err = encodeCompoundFloat(dst, dt, v)
	case core.DatatypeString:
		err = encodeCompoundString(dst, dt, v)
	case core.DatatypeEnum:
		err = encodeCompoundEnum(dst, dt, v)
	case core.DatatypeReference:
		err = encodeCompoundReference(dst, dt, v)
	case core.DatatypeArray:
		return encodeCompoundArray(dst, dt, v, path)
	case core.DatatypeCompound:
		r, ok := compoundRecord(v)
		if !ok {
			return fmt.Errorf("member %q: nested compound expects map[string]any, got %T", path, v)
		}
		return encodeCompoundRecord(dst, dt, r, path+".")
	default:
		err = fmt.Errorf("member type %s cannot be written by Write (use WriteRaw)", dt)
	}
	if err != nil {
		return fmt.Errorf("member %q: %w", path, err)
	}
	return nil
}

// compoundRecord returns v as a compound value: a map[string]any, or the
// CompoundValue Dataset.ReadCompound returns for a nested compound.
func compoundRecord(v any) (map[string]any, bool) {
	switch r := v.(type) {
	case map[string]any:
		return r, true
	case core.CompoundValue:
		return r, true
	default:
		return nil, false
	}
}

// compoundGoInteger returns v, a Go integer of any width: s for a signed Go type,
// u for an unsigned one, signed telling which; ok is false for a non-integer.
func compoundGoInteger(v any) (s int64, u uint64, signed, ok bool) {
	switch x := v.(type) {
	case int:
		return int64(x), 0, true, true
	case int8:
		return int64(x), 0, true, true
	case int16:
		return int64(x), 0, true, true
	case int32:
		return int64(x), 0, true, true
	case int64:
		return x, 0, true, true
	case uint:
		return 0, uint64(x), false, true
	case uint8:
		return 0, uint64(x), false, true
	case uint16:
		return 0, uint64(x), false, true
	case uint32:
		return 0, uint64(x), false, true
	case uint64:
		return 0, x, false, true
	default:
		return 0, 0, false, false
	}
}

// compoundIntFits reports whether the signed value v fits an integer of size bytes.
func compoundIntFits(v int64, size int, signed bool) bool {
	bits := uint(size) * 8
	if signed {
		if bits >= 64 {
			return true
		}
		limit := int64(1) << (bits - 1)
		return v >= -limit && v < limit
	}
	if v < 0 {
		return false
	}
	return bits >= 64 || uint64(v) < uint64(1)<<bits
}

// compoundUintFits reports whether the unsigned value v fits an integer of size bytes.
func compoundUintFits(v uint64, size int, signed bool) bool {
	bits := uint(size) * 8
	if signed {
		bits--
	}
	return bits >= 64 || v < uint64(1)<<bits
}

// compoundAppendLE appends the low size bytes of v, least significant first.
func compoundAppendLE(buf []byte, v uint64, size int) []byte {
	for i := 0; i < size; i++ {
		buf = append(buf, byte(v>>(8*i))) //nolint:gosec // G115: byte i of v, truncation intended
	}
	return buf
}

// compoundPutInteger stores the low len(dst) bytes of v in the byte order of dt.
func compoundPutInteger(dst []byte, dt *core.DatatypeMessage, v uint64) {
	le := compoundAppendLE(dst[:0:0], v, len(dst))
	if dt.GetByteOrder() == binary.BigEndian {
		for i := range le {
			dst[len(dst)-1-i] = le[i]
		}
		return
	}
	copy(dst, le)
}

// encodeCompoundInteger encodes the Go integer v as fixed-point type dt.
func encodeCompoundInteger(dst []byte, dt *core.DatatypeMessage, v any) error {
	size := int(dt.Size)
	if size < 1 || size > 8 || dt.BitOffset != 0 || (dt.Precision != 0 && int(dt.Precision) != size*8) {
		return fmt.Errorf("integer type %s with bit offset %d and precision %d cannot be written by Write (use WriteRaw)",
			dt, dt.BitOffset, dt.Precision)
	}
	signed := dt.ClassBitField&datatypeSignedFlag != 0
	s, u, isSigned, ok := compoundGoInteger(v)
	switch {
	case !ok:
		return fmt.Errorf("integer member expects a Go integer, got %T", v)
	case isSigned && !compoundIntFits(s, size, signed):
		return fmt.Errorf("value %d does not fit the %d-byte integer", s, size)
	case !isSigned && !compoundUintFits(u, size, signed):
		return fmt.Errorf("value %d does not fit the %d-byte integer", u, size)
	}
	if isSigned {
		u = uint64(s) //nolint:gosec // G115: two's-complement bits, range checked above
	}
	compoundPutInteger(dst, dt, u)
	return nil
}

// compoundIsIEEE reports whether dt has the IEEE 754 binary32 or binary64 layout.
func compoundIsIEEE(dt *core.DatatypeMessage) bool {
	sign := (dt.ClassBitField >> 8) & 0xFF
	switch dt.Size {
	case 4:
		return sign == 31 && dt.ExponentLocation == 23 && dt.ExponentSize == 8 &&
			dt.MantissaLocation == 0 && dt.MantissaSize == 23 && dt.ExponentBias == 127
	case 8:
		return sign == 63 && dt.ExponentLocation == 52 && dt.ExponentSize == 11 &&
			dt.MantissaLocation == 0 && dt.MantissaSize == 52 && dt.ExponentBias == 1023
	default:
		return false
	}
}

// encodeCompoundFloat encodes v (float32 or float64) as floating-point type dt.
// A float64 stored in a 4-byte member is rounded to the nearest float32, as
// the C library's hard conversion does.
func encodeCompoundFloat(dst []byte, dt *core.DatatypeMessage, v any) error {
	if !compoundIsIEEE(dt) {
		return fmt.Errorf("floating-point type %s is not IEEE binary32/binary64 and cannot be written by Write (use WriteRaw)", dt)
	}
	var f float64
	switch x := v.(type) {
	case float32:
		f = float64(x)
	case float64:
		f = x
	default:
		return fmt.Errorf("floating-point member expects float32 or float64, got %T", v)
	}
	if dt.Size == 4 {
		// float32(f) would turn a finite value beyond the range into an
		// infinity: refused, as the file doc promises no silent change.
		if math.Abs(f) > math.MaxFloat32 && !math.IsInf(f, 0) {
			return fmt.Errorf("value %g is out of the float32 range", f)
		}
		compoundPutInteger(dst, dt, uint64(math.Float32bits(float32(f))))
	} else {
		compoundPutInteger(dst, dt, math.Float64bits(f))
	}
	return nil
}

// encodeCompoundString encodes the string v as fixed-length string type dt with
// its padding (H5T_str_t): a null-terminated string keeps one byte for the
// terminator, a space-padded one is filled with spaces.
func encodeCompoundString(dst []byte, dt *core.DatatypeMessage, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("string member expects a string, got %T", v)
	}
	limit := len(dst)
	pad := core.StringPadding(dt.GetStringPadding())
	switch pad {
	case core.StringNullTerm:
		limit--
	case core.StringNullPad, core.StringSpacePad:
	default:
		return fmt.Errorf("string padding %d cannot be written by Write (use WriteRaw)", pad)
	}
	if len(s) > limit {
		return fmt.Errorf("string of %d bytes does not fit the %d-byte member (at most %d bytes)", len(s), len(dst), limit)
	}
	n := copy(dst, s)
	if pad == core.StringSpacePad {
		for i := n; i < len(dst); i++ {
			dst[i] = ' '
		}
	}
	return nil
}

// encodeCompoundEnum encodes v, the name of an enumeration member or one of its
// integer values, as enumeration type dt.
func encodeCompoundEnum(dst []byte, dt *core.DatatypeMessage, v any) error {
	size := int(dt.Size)
	if dt.Base == nil || len(dt.EnumValues) != len(dt.EnumNames)*size {
		return errors.New("enumeration type without members cannot be written by Write")
	}
	if name, ok := v.(string); ok {
		for i, n := range dt.EnumNames {
			if n == name {
				copy(dst, dt.EnumValues[i*size:][:size])
				return nil
			}
		}
		return fmt.Errorf("%q is not a member of the enumeration", name)
	}
	if x, ok := v.(int64); ok && size == 8 && dt.Base.ClassBitField&datatypeSignedFlag == 0 {
		// An int64 for a 64-bit unsigned base is the bit pattern of the
		// value, the convention of EnumType.Values and WithEnumValues.
		v = uint64(x) //nolint:gosec // G115: two's-complement bit pattern by convention
	}
	if err := encodeCompoundInteger(dst, dt.Base, v); err != nil {
		return fmt.Errorf("enumeration member expects a member name or an integer value: %w", err)
	}
	for i := range dt.EnumNames {
		if bytes.Equal(dt.EnumValues[i*size:][:size], dst) {
			return nil
		}
	}
	return fmt.Errorf("value %v is not a member of the enumeration", v)
}

// encodeCompoundReference encodes a reference: a uint64 object address for an
// object reference, or the raw bytes ([]byte or a byte array) of the size of
// the reference type.
func encodeCompoundReference(dst []byte, dt *core.DatatypeMessage, v any) error {
	if addr, ok := v.(uint64); ok && core.ReferenceType(dt.ClassBitField&0x0F) == core.ReferenceObject && len(dst) == 8 {
		binary.LittleEndian.PutUint64(dst, addr)
		return nil
	}
	rv := reflect.ValueOf(v)
	if v == nil || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) || rv.Type().Elem().Kind() != reflect.Uint8 {
		return fmt.Errorf("reference member expects a uint64 object address or %d raw bytes, got %T", len(dst), v)
	}
	if rv.Len() != len(dst) {
		return fmt.Errorf("reference member expects %d bytes, got %d", len(dst), rv.Len())
	}
	for i := range dst {
		dst[i] = byte(rv.Index(i).Uint()) //nolint:gosec // G115: the element kind is uint8
	}
	return nil
}

// encodeCompoundArray encodes v, a slice or Go array with one value per array
// element in row-major order, as array type dt.
func encodeCompoundArray(dst []byte, dt *core.DatatypeMessage, v any, path string) error {
	rv := reflect.ValueOf(v)
	if v == nil || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return fmt.Errorf("member %q: array member expects a slice, got %T", path, v)
	}
	n := uint64(1)
	for _, d := range dt.ArrayDims {
		n *= uint64(d)
	}
	if dt.Base == nil || uint64(rv.Len()) != n { //nolint:gosec // G115: a length is never negative
		return fmt.Errorf("member %q: array of %d elements, got %d values", path, n, rv.Len())
	}
	stride := uint64(dt.Base.Size)
	for i := 0; i < rv.Len(); i++ {
		elem := dst[uint64(i)*stride:][:stride]
		if err := encodeCompoundMemberValue(elem, dt.Base, rv.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}
