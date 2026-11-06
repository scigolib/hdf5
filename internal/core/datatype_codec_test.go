package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// mustNew unwraps a constructor result.
func mustNew(t *testing.T, dt *DatatypeMessage, err error) *DatatypeMessage {
	t.Helper()
	require.NoError(t, err)
	require.NotNil(t, dt)
	return dt
}

// codecSamples returns one datatype per supported class and encoding variant.
func codecSamples(t *testing.T) map[string]*DatatypeMessage {
	t.Helper()
	must := func(dt *DatatypeMessage, err error) *DatatypeMessage { return mustNew(t, dt, err) }
	i32 := must(NewIntegerDatatype(4, true))
	u8 := must(NewIntegerDatatype(1, false))
	f32 := must(NewFloatDatatype(4))
	f64 := must(NewFloatDatatype(8))
	s16 := must(NewStringDatatype(16, StringNullPad, CharsetUTF8))
	inner := must(NewCompoundDatatype(16, []CompoundMember{
		{Name: "x", Offset: 0, Type: f64},
		{Name: "y", Offset: 8, Type: f64},
	}))
	enumValues := []byte{0, 1, 2}

	samples := map[string]*DatatypeMessage{
		"int32":         i32,
		"uint8":         u8,
		"int16 BE":      {Class: DatatypeFixed, Version: 1, Size: 2, ClassBitField: flagBigEndian | flagSigned},
		"int24 offset":  {Class: DatatypeFixed, Version: 1, Size: 4, BitOffset: 8, Precision: 24},
		"float16":       must(NewFloatDatatype(2)),
		"float32":       f32,
		"float64":       f64,
		"float64 BE":    {Class: DatatypeFloat, Version: 1, Size: 8, ClassBitField: flagBigEndian},
		"float VAX v3":  {Class: DatatypeFloat, Version: 3, Size: 4, ClassBitField: flagBigEndian | flagFloatVAX | 0x1F20, Precision: 32, ExponentLocation: 23, ExponentSize: 8, MantissaSize: 23, ExponentBias: 129},
		"time":          {Class: DatatypeTime, Version: 1, Size: 8},
		"string":        s16,
		"bitfield":      {Class: DatatypeBitfield, Version: 1, Size: 2},
		"opaque":        must(NewOpaqueDatatype(7, "raw bytes")),
		"opaque empty":  {Class: DatatypeOpaque, Version: 1, Size: 4},
		"ref object":    must(NewReferenceDatatype(ReferenceObject, 8)),
		"ref region":    must(NewReferenceDatatype(ReferenceDatasetRegion, 12)),
		"ref object2":   {Class: DatatypeReference, Version: 4, Size: 64, ClassBitField: 2},
		"ref attribute": {Class: DatatypeReference, Version: 4, Size: 64, ClassBitField: 4},
		"compound v3":   must(NewCompoundDatatype(36, []CompoundMember{{Name: "id", Offset: 0, Type: i32}, {Name: "name", Offset: 4, Type: s16}, {Name: "pos", Offset: 20, Type: inner}})),
		"compound v2":   {Class: DatatypeCompound, Version: 2, Size: 8, Members: []CompoundMember{{Name: "abcdefgh", Offset: 4, Type: f32}}},
		"compound v1":   {Class: DatatypeCompound, Version: 1, Size: 8, Members: []CompoundMember{{Name: "a", Offset: 0, Type: i32}, {Name: "b", Offset: 4, Type: f32}}},
		"enum v1":       must(NewEnumDatatype(u8, []string{"RED", "GREEN", "BLUE"}, enumValues)),
		"enum v3":       {Class: DatatypeEnum, Version: 3, Size: 1, Base: u8, EnumNames: []string{"OFF", "ON"}, EnumValues: []byte{0, 1}},
		"vlen sequence": must(NewVLenSequenceDatatype(f64)),
		"vlen string":   must(NewVLenStringDatatype(StringNullTerm, CharsetUTF8)),
		"array v3":      must(NewArrayDatatype(i32, []uint32{2, 3})),
		"array v2":      {Class: DatatypeArray, Version: 2, Size: 24, ArrayDims: []uint32{3}, Base: f64},
		"complex":       {Class: DatatypeComplex, Size: 16, Base: f64},
	}
	// Offsets of 1, 2, 3 and 4 bytes (limit_enc_size of the compound size).
	for _, size := range []uint32{255, 256, 1 << 16, 1 << 24} {
		samples["compound offset bytes "+strconv.Itoa(limitEncSize(uint64(size)))] = &DatatypeMessage{
			Class: DatatypeCompound, Version: 3, Size: size,
			Members: []CompoundMember{{Name: "head", Offset: 0, Type: u8}, {Name: "tail", Offset: size - 4, Type: i32}},
		}
	}
	return samples
}

// TestDatatypeCodec_RoundTrip encodes every class, decodes it, and requires
// the decoded message to encode to the same bytes and to consume them all.
func TestDatatypeCodec_RoundTrip(t *testing.T) {
	for name, dt := range codecSamples(t) {
		t.Run(name, func(t *testing.T) {
			enc, err := EncodeDatatypeMessage(dt)
			require.NoError(t, err)

			back, n, err := DecodeDatatype(enc)
			require.NoError(t, err)
			require.Equal(t, len(enc), n)
			require.Equal(t, dt.Class, back.Class)
			require.Equal(t, dt.Size, back.Size)
			require.Equal(t, enc[datatypeHeaderSize:], back.Properties)

			again, err := EncodeDatatypeMessage(back)
			require.NoError(t, err)
			require.Equal(t, enc, again)

			// A message embedded in a longer buffer decodes the same way.
			back2, n2, err := DecodeDatatype(append(append([]byte(nil), enc...), 0xAA, 0xBB))
			require.NoError(t, err)
			require.Equal(t, n, n2)
			require.Equal(t, back.Properties, back2.Properties)
		})
	}
}

// TestDatatypeCodec_Truncated cuts every sample short at every length: the
// decoder must report corruption, never panic or read past the buffer.
func TestDatatypeCodec_Truncated(t *testing.T) {
	for name, dt := range codecSamples(t) {
		enc, err := EncodeDatatypeMessage(dt)
		require.NoError(t, err, name)
		for cut := 0; cut < len(enc); cut++ {
			buf := append([]byte(nil), enc[:cut]...) // exact capacity: no reads past it
			_, _, err := DecodeDatatype(buf)
			require.ErrorIs(t, err, utils.ErrCorrupt, "%s cut at %d", name, cut)
		}
	}
}

// TestDatatypeCodec_CBytes pins the bytes of the types the writer emits to
// what the C library writes for the same types (H5T_STD_*LE, H5T_IEEE_*LE,
// H5T_C_S1, compound v3 with minimal offsets).
func TestDatatypeCodec_CBytes(t *testing.T) {
	must := func(dt *DatatypeMessage, err error) *DatatypeMessage { return mustNew(t, dt, err) }
	enc := func(dt *DatatypeMessage, err error) []byte {
		t.Helper()
		b, err2 := EncodeDatatypeMessage(mustNew(t, dt, err))
		require.NoError(t, err2)
		return b
	}
	require.Equal(t, int32LEBytes, enc(NewIntegerDatatype(4, true)))
	require.Equal(t, uint8LEBytes, enc(NewIntegerDatatype(1, false)))
	require.Equal(t, float64LEBytes, enc(NewFloatDatatype(8)))
	require.Equal(t, []byte{0x11, 0x20, 0x1F, 0x00, 4, 0, 0, 0, 0, 0, 32, 0, 23, 8, 0, 23, 127, 0, 0, 0},
		enc(NewFloatDatatype(4)))
	require.Equal(t, []byte{0x13, 0x00, 0x00, 0x00, 16, 0, 0, 0}, enc(NewStringDatatype(16, StringNullTerm, CharsetASCII)))

	// {int32 id; float64 value}: 2 members in the class bits, names
	// unpadded, 1-byte offsets (size 12).
	ct := enc(NewCompoundDatatype(12, []CompoundMember{
		{Name: "id", Offset: 0, Type: must(NewIntegerDatatype(4, true))},
		{Name: "value", Offset: 4, Type: must(NewFloatDatatype(8))},
	}))
	want := cat(
		[]byte{0x36, 2, 0, 0, 12, 0, 0, 0, 'i', 'd', 0, 0}, int32LEBytes,
		[]byte{'v', 'a', 'l', 'u', 'e', 0, 4}, float64LEBytes,
	)
	require.Equal(t, want, ct)
}

// TestDatatypeCodec_LegacyDescriptions checks that literal messages without
// decoded properties (the writer's old form) get the C layout.
func TestDatatypeCodec_LegacyDescriptions(t *testing.T) {
	f64, err := EncodeDatatypeMessage(&DatatypeMessage{Class: DatatypeFloat, Size: 8})
	require.NoError(t, err)
	require.Equal(t, float64LEBytes, f64, "sign position and normalization come with the IEEE layout")

	i32, err := EncodeDatatypeMessage(&DatatypeMessage{Class: DatatypeFixed, Size: 4, ClassBitField: 0x08})
	require.NoError(t, err)
	require.Equal(t, int32LEBytes, i32)

	// Properties never feed the encoder.
	junk, err := EncodeDatatypeMessage(&DatatypeMessage{Class: DatatypeString, Size: 4, Properties: []byte{1, 2, 3}})
	require.NoError(t, err)
	require.Equal(t, []byte{0x13, 0, 0, 0, 4, 0, 0, 0}, junk)
}

// TestLimitEncSize mirrors H5VM_limit_enc_size (H5VMprivate.h:448-451).
func TestLimitEncSize(t *testing.T) {
	for limit, want := range map[uint64]int{
		0: 1, 1: 1, 255: 1, 256: 2, 65535: 2, 65536: 3, 1<<24 - 1: 3, 1 << 24: 4, 1<<32 - 1: 4, 1 << 32: 5,
	} {
		require.Equal(t, want, limitEncSize(limit), "limit %d", limit)
	}
}

// corruptCase is a datatype message the C decoder rejects.
type corruptCase struct {
	name string
	data []byte
	want string // substring of the error
}

func hdr(class DatatypeClass, version uint8, flags, size uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, uint32(class)|uint32(version)<<4|flags<<8)
	binary.LittleEndian.PutUint32(b[4:], size)
	return b
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func floatProps(epos, esize, mpos, msize uint8) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint16(b[2:], 32) // precision
	b[4], b[5], b[6], b[7] = epos, esize, mpos, msize
	binary.LittleEndian.PutUint32(b[8:], 127)
	return b
}

func intProps(off, prec uint16) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b, off)
	binary.LittleEndian.PutUint16(b[2:], prec)
	return b
}

// TestDecodeDatatype_RejectsWhatCRejects covers the checks of
// H5O__dtype_decode_helper, one case each.
func TestDecodeDatatype_RejectsWhatCRejects(t *testing.T) {
	ieee32 := 0x1F20
	cases := []corruptCase{
		{"version 0", hdr(DatatypeFixed, 0, 0, 4), "bad version number 0"},
		{"version 6", hdr(DatatypeFixed, 6, 0, 4), "bad version number 6"},
		{"size 0", hdr(DatatypeString, 1, 0, 0), "invalid datatype size 0"},
		{"unknown class", hdr(12, 1, 0, 4), "unknown datatype class 12"},
		{"int offset", cat(hdr(DatatypeFixed, 1, 0, 4), intProps(32, 8)), "integer offset 32 out of bounds"},
		{"int precision 0", cat(hdr(DatatypeFixed, 1, 0, 4), intProps(0, 0)), "integer precision is zero"},
		{"int offset+prec", cat(hdr(DatatypeFixed, 1, 0, 4), intProps(8, 32)), "integer offset 8 + precision 32"},
		{"bitfield precision 0", cat(hdr(DatatypeBitfield, 1, 0, 2), intProps(0, 0)), "bitfield precision is zero"},
		{"float VAX without BE", cat(hdr(DatatypeFloat, 3, 0x40|uint32(ieee32), 4), floatProps(23, 8, 0, 23)), "bad byte order"},
		{"float norm 3", cat(hdr(DatatypeFloat, 1, 0x30|0x1F00, 4), floatProps(23, 8, 0, 23)), "unknown floating-point normalization"},
		{"float sign pos", cat(hdr(DatatypeFloat, 1, 0x2000|0x20, 4), floatProps(23, 8, 0, 23)), "sign bit position 32"},
		{"float esize 0", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(23, 0, 0, 23)), "exponent size can't be zero"},
		{"float epos", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(32, 8, 0, 23)), "exponent starting position 32"},
		{"float erange", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(30, 8, 0, 23)), "exponent range"},
		{"float msize 0", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(23, 8, 0, 0)), "mantissa size can't be zero"},
		{"float mpos", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(23, 8, 40, 1)), "mantissa starting position 40"},
		{"float mrange", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(23, 8, 10, 23)), "mantissa range"},
		{"float exp/sign", cat(hdr(DatatypeFloat, 1, 0x1E20, 4), floatProps(23, 8, 0, 23)), "exponent and sign positions overlap"},
		{"float mant/sign", cat(hdr(DatatypeFloat, 3, 0, 4), floatProps(23, 8, 0, 23)), "mantissa and sign positions overlap"},
		{"float mant/exp", cat(hdr(DatatypeFloat, 1, uint32(ieee32), 4), floatProps(20, 8, 0, 23)), "mantissa and exponent positions overlap"},
		{"opaque unaligned", cat(hdr(DatatypeOpaque, 1, 5, 4), []byte("abcde")), "not aligned to 8"},
		{"compound 0 members", cat(hdr(DatatypeCompound, 1, 0, 4)), "invalid number of members: 0"},
		{"compound v3 0 members, flags set", cat(hdr(DatatypeCompound, 3, 0x10000, 4)), "invalid number of members: 0"},
		{"legacy compound truncated", cat(hdr(DatatypeCompound, 3, 0, 4)), "legacy hdf5-go compound encoding"},
		{"compound name unterminated", cat(hdr(DatatypeCompound, 3, 1, 4), []byte("abc")), "not null terminated"},
		{"compound duplicate", cat(hdr(DatatypeCompound, 3, 2, 8), []byte("a\x00\x00"), int32LEBytes, []byte("a\x00\x04"), int32LEBytes), "duplicated compound field name"},
		{"compound member outside", cat(hdr(DatatypeCompound, 3, 1, 4), []byte("a\x00\x01"), int32LEBytes), "extends outside"},
		{"compound overlap", cat(hdr(DatatypeCompound, 3, 2, 8), []byte("a\x00\x04"), int32LEBytes, []byte("b\x00\x02"), int32LEBytes), "overlaps member"},
		{"compound v1 ndims 5", cat(hdr(DatatypeCompound, 1, 1, 4), []byte("a\x00\x00\x00\x00\x00\x00\x00"), make([]byte, 4), []byte{5}, make([]byte, 27), int32LEBytes), "invalid number of dimensions 5"},
		{"compound v1 zero dim", cat(hdr(DatatypeCompound, 1, 1, 4), []byte("a\x00\x00\x00\x00\x00\x00\x00"), make([]byte, 4), []byte{1}, make([]byte, 27), int32LEBytes), "zero-sized dimension"},
		{"reference type 5", hdr(DatatypeReference, 1, 5, 8), "invalid reference type 5"},
		{"reference version", hdr(DatatypeReference, 4, 2|2<<4, 8), "reference version 2 does not match 1"},
		{"enum size", cat(hdr(DatatypeEnum, 1, 0, 2), uint8LEBytes), "does not match its 1-byte base type"},
		{"enum empty name", cat(hdr(DatatypeEnum, 3, 1, 1), uint8LEBytes, []byte{0, 0}), "0 length enumeration name"},
		{"enum values", cat(hdr(DatatypeEnum, 3, 2, 1), uint8LEBytes, []byte("A\x00B\x00\x01")), "enumeration values"},
		{"vlen without base", hdr(DatatypeVarLen, 1, 0, 16), "variable-length base type"},
		{"array rank 33", cat(hdr(DatatypeArray, 3, 0, 4), []byte{33}), "too many dimensions 33"},
		{"complex heterogeneous", cat(hdr(DatatypeComplex, 5, 0, 16), float64LEBytes), "heterogeneous"},
		{"complex polar", cat(hdr(DatatypeComplex, 5, 0x03, 16), float64LEBytes), "rectangular"},
		{"complex flags", cat(hdr(DatatypeComplex, 5, 0x09, 16), float64LEBytes), "invalid flag bits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeDatatype(tc.data)
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// TestEncodeDatatype_RefusesInvalid checks that the encoder never writes a
// type the decoder (and the C library) would refuse.
func TestEncodeDatatype_RefusesInvalid(t *testing.T) {
	must := func(dt *DatatypeMessage, err error) *DatatypeMessage { return mustNew(t, dt, err) }
	i32 := must(NewIntegerDatatype(4, true))
	arr := must(NewArrayDatatype(i32, []uint32{2}))
	cases := map[string]*DatatypeMessage{
		"nil member type":       {Class: DatatypeCompound, Size: 4, Members: []CompoundMember{{Name: "a"}}},
		"empty member name":     {Class: DatatypeCompound, Size: 4, Members: []CompoundMember{{Type: i32}}},
		"duplicate member":      {Class: DatatypeCompound, Size: 8, Members: []CompoundMember{{Name: "a", Type: i32}, {Name: "a", Offset: 4, Type: i32}}},
		"member outside":        {Class: DatatypeCompound, Size: 4, Members: []CompoundMember{{Name: "a", Offset: 2, Type: i32}}},
		"v1 array member":       {Class: DatatypeCompound, Version: 1, Size: 8, Members: []CompoundMember{{Name: "a", Type: arr}}},
		"version below member":  {Class: DatatypeCompound, Version: 1, Size: 8, Members: []CompoundMember{{Name: "a", Type: arr}}},
		"version 6":             {Class: DatatypeFixed, Version: 6, Size: 4},
		"array size mismatch":   {Class: DatatypeArray, Size: 12, ArrayDims: []uint32{2}, Base: i32},
		"array no dims":         {Class: DatatypeArray, Size: 4, Base: i32},
		"enum size mismatch":    {Class: DatatypeEnum, Size: 2, Base: i32, EnumNames: []string{"A"}, EnumValues: make([]byte, 4)},
		"enum values length":    {Class: DatatypeEnum, Size: 4, Base: i32, EnumNames: []string{"A"}, EnumValues: make([]byte, 3)},
		"opaque tag too long":   {Class: DatatypeOpaque, Size: 4, OpaqueTag: strings.Repeat("t", maxEncodableOpaqueTag+1)},
		"float without layout":  {Class: DatatypeFloat, Size: 3},
		"vlen without base":     {Class: DatatypeVarLen, Size: 16},
		"complex size":          {Class: DatatypeComplex, Size: 8, Base: must(NewFloatDatatype(8))},
		"reference type":        {Class: DatatypeReference, Size: 8, ClassBitField: 7},
		"size 0":                {Class: DatatypeString},
		"overlapping members":   {Class: DatatypeCompound, Size: 8, Members: []CompoundMember{{Name: "a", Type: i32}, {Name: "b", Offset: 2, Type: i32}}},
		"precision beyond size": {Class: DatatypeFixed, Size: 2, Precision: 32},
	}
	for name, dt := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := EncodeDatatypeMessage(dt)
			require.Error(t, err)
		})
	}
}

// TestDatatypeEncoding_OnlyInCodec is the guard of ADR-004 §3: datatype
// message bytes are produced by datatype_codec.go only. A class/version word
// built anywhere else, or a datatype message assembled from raw Properties,
// is a second encoder that can drift from the C layout again.
func TestDatatypeEncoding_OnlyInCodec(t *testing.T) {
	forbidden := regexp.MustCompile(
		`uint32\([A-Za-z_.]*Class\)` + // class/version word from a class field
			`|uint32\((core\.)?Datatype(Fixed|Float|Time|String|Bitfield|Opaque|Compound|Reference|Enum|VarLen|Array|Complex)\)` +
			`|Properties:\s`) // messages assembled from raw property bytes
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "tmp", "testdata", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			filepath.Base(path) == "datatype_codec.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if forbidden.MatchString(line) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "datatype bytes encoded outside internal/core/datatype_codec.go")
}

// legacyF64Bytes is float64 as the attribute writer of hdf5-go before v0.14.3
// wrote it: class bit field 0 (sign bit position 0), otherwise the IEEE
// layout. C rejects it (H5Odtype.c:264-267).
var legacyF64Bytes = []byte{
	0x11, 0x00, 0x00, 0x00, 8, 0, 0, 0,
	0, 0, 64, 0, 52, 11, 0, 52, 0xFF, 0x03, 0, 0,
}

// TestDecodeDatatype_LegacyFloat pins the one legacy float pattern: it decodes
// as H5T_IEEE_F64LE, and a byte away from it is C-strict again.
func TestDecodeDatatype_LegacyFloat(t *testing.T) {
	dt, n, err := DecodeDatatype(legacyF64Bytes)
	require.NoError(t, err)
	require.Equal(t, len(legacyF64Bytes), n)
	require.Equal(t, uint32(0x3F20), dt.ClassBitField)
	enc, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	require.Equal(t, float64LEBytes, enc, "re-encoded as the type the old writer meant")

	for name, mutate := range map[string]func(b []byte){
		"version 3":  func(b []byte) { b[0] = 0x31 },
		"bias 1022":  func(b []byte) { b[16] = 0xFE },
		"precision":  func(b []byte) { b[10] = 63 },
		"big-endian": func(b []byte) { b[1] = 0x01 },
	} {
		b := append([]byte(nil), legacyF64Bytes...)
		mutate(b)
		_, _, err := DecodeDatatype(b)
		require.ErrorIs(t, err, utils.ErrCorrupt, name)
	}
}

// legacyCompoundBytes is {id uint32 @0; value float64 @4} as
// EncodeCompoundDatatypeV3 wrote it before v0.14.3: 4-byte member count in
// the properties, 4-byte offsets, member properties [0, size*8, 0...].
func legacyCompoundBytes() []byte {
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	return cat(
		hdr(DatatypeCompound, 3, 0, 12), u32(2),
		[]byte("id\x00"), u32(0), hdr(DatatypeFixed, 1, 0, 4), []byte{0, 32, 0, 0},
		[]byte("value\x00"), u32(4), hdr(DatatypeFloat, 1, 0, 8), []byte{0, 64, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	)
}

// TestDecodeDatatype_LegacyCompound pins the legacy compound pattern: it
// decodes to the members a valid file of the same type has, and re-encodes
// as a valid version-3 compound.
func TestDecodeDatatype_LegacyCompound(t *testing.T) {
	must := func(dt *DatatypeMessage, err error) *DatatypeMessage { return mustNew(t, dt, err) }
	data := legacyCompoundBytes()
	dt, n, err := DecodeDatatype(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Len(t, dt.Members, 2)
	require.Equal(t, "id", dt.Members[0].Name)
	require.Equal(t, uint16(32), dt.Members[0].Type.Precision)
	require.Equal(t, uint32(4), dt.Members[1].Offset)
	require.True(t, dt.Members[1].Type.IsFloat64())

	enc, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	want, err := EncodeDatatypeMessage(&DatatypeMessage{Class: DatatypeCompound, Version: 3, Size: 12, Members: []CompoundMember{
		{Name: "id", Type: must(NewIntegerDatatype(4, false))},
		{Name: "value", Offset: 4, Type: must(NewFloatDatatype(8))},
	}})
	require.NoError(t, err)
	require.Equal(t, want, enc)

	ct, err := ParseCompoundType(dt)
	require.NoError(t, err)
	require.Len(t, ct.Members, 2)
}

// TestDecodeDatatype_LegacyCompoundRejects checks that anything the old
// writer could not have produced is corruption inside a legacy compound.
func TestDecodeDatatype_LegacyCompoundRejects(t *testing.T) {
	for name, mutate := range map[string]func(b []byte) []byte{
		"truncated":        func(b []byte) []byte { return b[:len(b)-1] },
		"count 0":          func(b []byte) []byte { b[8] = 0; return b },
		"int props":        func(b []byte) []byte { b[27] = 1; return b },
		"member version 3": func(b []byte) []byte { b[19] = 0x30; return b },
		"member is enum":   func(b []byte) []byte { b[19] = 0x18; return b },
		"member outside":   func(b []byte) []byte { b[37] = 9; return b },
		"garbled member":   func(b []byte) []byte { copy(b[31:36], "id\x00\x00\x00"); return b },
		"duplicate members": func([]byte) []byte {
			u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
			m := cat(hdr(DatatypeFixed, 1, 0, 4), []byte{0, 32, 0, 0})
			return cat(hdr(DatatypeCompound, 3, 0, 8), u32(2), []byte("id\x00"), u32(0), m, []byte("id\x00"), u32(4), m)
		},
	} {
		data := mutate(legacyCompoundBytes())
		_, _, err := DecodeDatatype(data)
		require.ErrorIs(t, err, utils.ErrCorrupt, name)
		require.ErrorContains(t, err, "legacy hdf5-go compound encoding", name)
	}
}

// TestSharedDatatype_NotCorrupt checks that a shared (committed) datatype, in
// an object header message or in an attribute, is reported as unsupported,
// never as corruption: it is valid HDF5.
func TestSharedDatatype_NotCorrupt(t *testing.T) {
	_, err := ParseDatatypeHeaderMessage(&HeaderMessage{Type: MsgDatatype, Offset: 0x40, Flags: MsgFlagShared, Data: []byte{3, 2, 0, 0, 0, 0, 0, 0}})
	require.ErrorIs(t, err, errSharedDatatype)
	require.NotErrorIs(t, err, utils.ErrCorrupt)
	require.ErrorContains(t, err, "0x40")

	// Version 3 attribute message with H5O_ATTR_FLAG_TYPE_SHARED (H5Oattr.c:88).
	attr := cat([]byte{3, attrFlagTypeShared, 2, 0, 10, 0, 8, 0, 0, 'a', 0}, make([]byte, 18))
	_, err = ParseAttributeMessage(attr, binary.LittleEndian)
	require.ErrorIs(t, err, errSharedDatatype)
	require.NotErrorIs(t, err, utils.ErrCorrupt)

	// Non-shared messages decode as before.
	dt, err := ParseDatatypeHeaderMessage(&HeaderMessage{Type: MsgDatatype, Data: int32LEBytes})
	require.NoError(t, err)
	require.Equal(t, uint16(32), dt.Precision)
}

// v1CompoundWithArrayMember encodes a version-1 compound of size with one
// member "name" that has 1-D array dimension dim over base (H5Odtype.c:451-490).
func v1CompoundWithArrayMember(size uint32, name string, dim uint32, base []byte) []byte {
	info := make([]byte, compoundV1ArrayInfoSize)
	info[0] = 1 // ndims
	binary.LittleEndian.PutUint32(info[12:], dim)
	padded := make([]byte, encodedNameSize(len(name), dtypeVersion1))
	copy(padded, name)
	return cat(hdr(DatatypeCompound, 1, 1, size), padded, make([]byte, 4), info, base)
}

// TestDecodeDatatype_V1ArrayMemberUpgrade is the regression for review-064
// M1: C raises a compound's version to that of its members (array members
// are version 2), so (a) a decoded v1 compound with an array member
// re-encodes, as version 2, and (b) a v1 compound whose array member is
// itself a v1 compound with array members decodes.
func TestDecodeDatatype_V1ArrayMemberUpgrade(t *testing.T) {
	inner := v1CompoundWithArrayMember(8, "x", 2, int32LEBytes) // {int32 x[2]}
	outer := v1CompoundWithArrayMember(16, "m", 2, inner)       // {inner m[2]}

	// The bytes C writes for the upgraded types: version-2 compounds whose
	// array members are version-2 array messages.
	innerV2 := v2CompoundBytes(8, v2MemberBytes("x", 0, v2ArrayBytes(2, 4, int32LEBytes)))
	outerV2 := v2CompoundBytes(16, v2MemberBytes("m", 0, v2ArrayBytes(2, 8, innerV2)))

	for _, tc := range []struct {
		name       string
		data, want []byte
	}{
		{"a: one level", inner, innerV2},
		{"b: nested", outer, outerV2},
	} {
		dt, n, err := DecodeDatatype(tc.data)
		require.NoError(t, err, tc.name)
		require.Equal(t, len(tc.data), n, tc.name)
		require.Equal(t, dtypeVersion2, dt.Version, tc.name)

		enc, err := EncodeDatatypeMessage(dt)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, enc, tc.name)
		back, _, err := DecodeDatatype(enc)
		require.NoError(t, err, tc.name)
		require.Equal(t, dtypeVersion2, back.Version, tc.name)
		require.Equal(t, dt.Members[0].Name, back.Members[0].Name, tc.name)
		require.Equal(t, DatatypeArray, back.Members[0].Type.Class, tc.name)
		require.Equal(t, []uint32{2}, back.Members[0].Type.ArrayDims, tc.name)
	}
}

// v2ArrayBytes is a one-dimensional version-2 array message as C encodes it
// (H5Odtype.c:1366-1398): rank, 3 reserved bytes, the dimension, the
// permutation index 0, then the base type.
func v2ArrayBytes(dim, baseSize uint32, base []byte) []byte {
	props := make([]byte, 12)
	props[0] = 1
	binary.LittleEndian.PutUint32(props[4:], dim)
	return cat(hdr(DatatypeArray, 2, 0, dim*baseSize), props, base)
}

// v2MemberBytes is a member of a version-2 compound (H5Odtype.c:1257-1306):
// the name padded to eight bytes, a 4-byte offset, no array information.
func v2MemberBytes(name string, offset uint32, typ []byte) []byte {
	padded := make([]byte, encodedNameSize(len(name), dtypeVersion2))
	copy(padded, name)
	off := make([]byte, 4)
	binary.LittleEndian.PutUint32(off, offset)
	return cat(padded, off, typ)
}

// v2CompoundBytes is a version-2 compound of size with the given members.
func v2CompoundBytes(size uint32, members ...[]byte) []byte {
	return cat(append([][]byte{hdr(DatatypeCompound, 2, uint32(len(members)), size)}, members...)...)
}

// TestDecodeDatatype_UpgradeReachesNestedTypes covers the rest of C's
// upgrade: H5T__upgrade_version (H5T.c:7193) raises every compound, array
// and enumeration inside the raised type, so a scalar version-1 compound
// member of a compound raised to version 2 is encoded as version 2 too (no
// version-1 array information).
func TestDecodeDatatype_UpgradeReachesNestedTypes(t *testing.T) {
	v1Scalar := make([]byte, compoundV1ArrayInfoSize)
	pad := func(name string) []byte {
		b := make([]byte, encodedNameSize(len(name), dtypeVersion1))
		copy(b, name)
		return b
	}
	nested := cat(hdr(DatatypeCompound, 1, 1, 4), pad("i"), make([]byte, 4), v1Scalar, int32LEBytes) // {int32 i}
	arrayInfo := make([]byte, compoundV1ArrayInfoSize)
	arrayInfo[0] = 1
	binary.LittleEndian.PutUint32(arrayInfo[12:], 2)
	offsetB := []byte{4, 0, 0, 0}
	data := cat(hdr(DatatypeCompound, 1, 2, 12),
		pad("a"), make([]byte, 4), v1Scalar, nested, // {nested a; int32 b[2]}
		pad("b"), offsetB, arrayInfo, int32LEBytes)

	dt, n, err := DecodeDatatype(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, dtypeVersion2, dt.Version)
	require.Equal(t, dtypeVersion2, dt.Members[0].Type.Version, "nested compound raised with its parent")
	require.Equal(t, dtypeVersion1, dt.Members[0].Type.Members[0].Type.Version, "atomic types keep their version")

	enc, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	want := v2CompoundBytes(12,
		v2MemberBytes("a", 0, v2CompoundBytes(4, v2MemberBytes("i", 0, int32LEBytes))),
		v2MemberBytes("b", 4, v2ArrayBytes(2, 4, int32LEBytes)))
	require.Equal(t, want, enc)
}
