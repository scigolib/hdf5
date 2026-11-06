package hdf5

// Tests of the public compound datatype API (TASK-065, issue #65).

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// compoundKVType is the reporter's layout: {key string[16], value string[16]}.
func compoundKVType(t *testing.T) *CompoundType {
	t.Helper()
	ct, err := NewCompoundType(32, []CompoundMember{
		{Name: "key", Offset: 0, Type: FixedString{Size: 16}},
		{Name: "value", Offset: 16, Type: FixedString{Size: 16}},
	})
	require.NoError(t, err)
	return ct
}

// openCompoundDataset opens file and returns the dataset at path.
func openCompoundDataset(t *testing.T, file, path string) (*File, *Dataset) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	var ds *Dataset
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == path {
			ds = d
		}
	})
	require.NotNil(t, ds, "dataset %s not found", path)
	return f, ds
}

// compoundDatatypeBytes returns the raw datatype message of the dataset at path.
func compoundDatatypeBytes(t *testing.T, file, path string) []byte {
	t.Helper()
	f, ds := openCompoundDataset(t, file, path)
	oh, err := core.ReadObjectHeader(f.Reader(), ds.address, f.Superblock())
	require.NoError(t, err)
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgDatatype {
			return msg.Data
		}
	}
	t.Fatalf("no datatype message in %s", path)
	return nil
}

// writeCompoundFile creates a file, runs fill with its writer and closes it.
func writeCompoundFile(t *testing.T, name string, fill func(fw *FileWriter)) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	fill(fw)
	require.NoError(t, fw.Close())
	return file
}

func TestNewCompoundType_Validation(t *testing.T) {
	cases := []struct {
		name    string
		size    uint32
		members []CompoundMember
		want    string
	}{
		{"zero size", 0, []CompoundMember{{Name: "a", Type: Int32}}, "size must be positive"},
		{"no members", 8, nil, "0 members"},
		{"empty name", 8, []CompoundMember{{Name: "", Type: Int32}}, "no member name"},
		{"NUL in name", 8, []CompoundMember{{Name: "a\x00b", Type: Int32}}, "NUL"},
		{"duplicate name", 8, []CompoundMember{
			{Name: "a", Offset: 0, Type: Int32},
			{Name: "a", Offset: 4, Type: Int32},
		}, `member name "a" is not unique`},
		{"overlap after", 8, []CompoundMember{
			{Name: "a", Offset: 0, Type: Int32},
			{Name: "b", Offset: 2, Type: Int32},
		}, `"b" at [2,6) overlaps member "a" at [0,4)`},
		{"overlap before", 8, []CompoundMember{
			{Name: "a", Offset: 2, Type: Int32},
			{Name: "b", Offset: 0, Type: Int32},
		}, `"b" at [0,4) overlaps member "a" at [2,6)`},
		{"contains other", 16, []CompoundMember{
			{Name: "a", Offset: 4, Type: Int16},
			{Name: "b", Offset: 0, Type: Int64},
		}, "overlaps"},
		{"past end", 8, []CompoundMember{
			{Name: "a", Offset: 0, Type: Int32},
			{Name: "b", Offset: 6, Type: Int32},
		}, "extends past the end of the 8-byte compound type"},
		{"offset past size", 8, []CompoundMember{{Name: "a", Offset: 100, Type: Uint8}}, "extends past the end"},
		{"offset overflow", 8, []CompoundMember{{Name: "a", Offset: math.MaxUint32, Type: Int64}}, "extends past the end"},
		{"nil type", 8, []CompoundMember{{Name: "a"}}, "type is nil"},
		{"string without size", 8, []CompoundMember{{Name: "a", Type: String}}, "FixedString"},
		{"array constant", 8, []CompoundMember{{Name: "a", Type: ArrayInt32}}, "ArrayType"},
		{"enum constant", 8, []CompoundMember{{Name: "a", Type: EnumInt8}}, "EnumType"},
		{"vlen", 16, []CompoundMember{{Name: "a", Type: VLenString}}, "not supported as a compound member"},
		{"unknown datatype", 8, []CompoundMember{{Name: "a", Type: Datatype(9999)}}, "unknown datatype"},
		{"zero string", 8, []CompoundMember{{Name: "a", Type: FixedString{}}}, "size must be positive"},
		{"array no dims", 8, []CompoundMember{{Name: "a", Type: ArrayType{Elem: Int8}}}, "has 0 dimensions, need 1 to 32"},
		{"array rank 33", 64, []CompoundMember{{Name: "a", Type: ArrayType{Elem: Int8, Dims: make([]uint64, 33)}}}, "has 33 dimensions, need 1 to 32"},
		{"array zero dim", 8, []CompoundMember{{Name: "a", Type: ArrayType{Elem: Int8, Dims: []uint64{0}}}}, "array dimension 0 is 0"},
		{"array huge dim", 8, []CompoundMember{{Name: "a", Type: ArrayType{Elem: Int8, Dims: []uint64{1 << 32}}}}, "must be 1 to"},
		{"array nil elem", 8, []CompoundMember{{Name: "a", Type: ArrayType{Dims: []uint64{2}}}}, "element type is nil"},
		{"array bad elem", 8, []CompoundMember{{Name: "a", Type: ArrayType{Elem: String, Dims: []uint64{2}}}}, "array element"},
		{"enum float base", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Float32, Names: []string{"x"}, Values: []int64{1}}}}, "not an integer type"},
		{"enum count", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Int8, Names: []string{"x"}}}}, "one value per name"},
		{"enum empty name", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Int8, Names: []string{""}, Values: []int64{1}}}}, "no name specified"},
		{"enum dup name", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Int8, Names: []string{"x", "x"}, Values: []int64{1, 2}}}}, `"x" is not unique`},
		{"enum dup value", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Int8, Names: []string{"x", "y"}, Values: []int64{1, 1}}}}, "value 1"},
		{"enum value range", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Uint8, Names: []string{"x"}, Values: []int64{256}}}}, "does not fit"},
		{"enum negative unsigned", 8, []CompoundMember{{Name: "a", Type: EnumType{Base: Uint16, Names: []string{"x"}, Values: []int64{-1}}}}, "does not fit"},
		{"nil nested", 8, []CompoundMember{{Name: "a", Type: (*CompoundType)(nil)}}, "nested compound type is nil"},
		{"zero-value nested", 8, []CompoundMember{{Name: "a", Type: &CompoundType{}}}, "nested compound type is nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct, err := NewCompoundType(tc.size, tc.members)
			require.Error(t, err)
			require.Nil(t, ct)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestNewCompoundType_TooManyMembers(t *testing.T) {
	members := make([]CompoundMember, maxCompoundMembers+1)
	_, err := NewCompoundType(1<<20, members)
	require.ErrorContains(t, err, "need 1 to 65535")
}

// Gaps between members and padding after the last one are allowed, as in
// H5T__insert, which checks only overlap and the end of the compound.
func TestNewCompoundType_Padding(t *testing.T) {
	members := []CompoundMember{
		{Name: "b", Offset: 8, Type: Float64},
		{Name: "a", Offset: 0, Type: Int16},
	}
	ct, err := NewCompoundType(24, members)
	require.NoError(t, err)
	require.Equal(t, uint32(24), ct.Size())
	require.Equal(t, members, ct.Members())

	// Members returns a copy.
	got := ct.Members()
	got[0].Name = "changed"
	require.Equal(t, "b", ct.Members()[0].Name)
}

func TestNewPackedCompoundType(t *testing.T) {
	inner, err := NewPackedCompoundType([]CompoundMember{
		{Name: "x", Type: Float32},
		{Name: "y", Type: Float32},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(8), inner.Size())

	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "id", Type: Uint8},
		{Name: "name", Type: FixedString{Size: 5, Charset: CharsetUTF8}},
		{Name: "pos", Type: inner},
		{Name: "v", Type: ArrayType{Elem: Int16, Dims: []uint64{2, 3}}},
		{Name: "e", Type: EnumType{Base: Int32, Names: []string{"off", "on"}, Values: []int64{0, 1}}},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(1+5+8+12+4), ct.Size())
	offsets := make([]uint32, 0, len(ct.Members()))
	for _, m := range ct.Members() {
		offsets = append(offsets, m.Offset)
	}
	require.Equal(t, []uint32{0, 1, 6, 14, 26}, offsets)

	_, err = NewPackedCompoundType([]CompoundMember{{Name: "a", Offset: 4, Type: Int8}})
	require.ErrorContains(t, err, "offsets are computed")
	_, err = NewPackedCompoundType([]CompoundMember{{Name: "a"}})
	require.ErrorContains(t, err, "type is nil")
	_, err = NewPackedCompoundType([]CompoundMember{{Name: "a", Type: String}})
	require.ErrorContains(t, err, "FixedString")
	_, err = NewPackedCompoundType([]CompoundMember{
		{Name: "a", Type: Int8}, {Name: "a", Type: Int8},
	})
	require.ErrorContains(t, err, "not unique")
	_, err = NewPackedCompoundType([]CompoundMember{
		{Name: "a", Type: ArrayType{Elem: Uint8, Dims: []uint64{1 << 31, 2}}},
	})
	require.ErrorContains(t, err, "array size overflows")
	_, err = NewPackedCompoundType([]CompoundMember{
		{Name: "a", Type: ArrayType{Elem: Uint8, Dims: []uint64{1 << 31}}},
		{Name: "b", Type: ArrayType{Elem: Uint8, Dims: []uint64{1 << 31}}},
	})
	require.ErrorContains(t, err, "size exceeds")
}

// The reporter's case end to end: written with Write([]map[string]any),
// read back with ReadCompound.
func TestCreateCompound_KeyValueRoundTrip(t *testing.T) {
	rows := []map[string]any{
		{"key": "alpha", "value": "first"},
		{"key": "beta", "value": "second"},
		{"key": "", "value": "fifteen bytes.."},
	}
	file := writeCompoundFile(t, "kv.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompound("/kv", compoundKVType(t), []uint64{3})
		require.NoError(t, err)
		require.NoError(t, dw.Write(rows))
	})
	_, ds := openCompoundDataset(t, file, "/kv")
	got, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Len(t, got, 3)
	for i, r := range rows {
		require.Equal(t, r["key"], got[i]["key"])
		require.Equal(t, r["value"], got[i]["value"])
	}
}

// Write output can be passed back: ReadCompound's result written to a second
// dataset reads back unchanged, nested compounds included.
func TestCreateCompound_WriteReadCompoundResult(t *testing.T) {
	pos, err := NewPackedCompoundType([]CompoundMember{{Name: "x", Type: Float64}, {Name: "y", Type: Float64}})
	require.NoError(t, err)
	ct, err := NewCompoundType(32, []CompoundMember{
		{Name: "id", Offset: 0, Type: Int32},
		{Name: "count", Offset: 4, Type: Int64},
		{Name: "pos", Offset: 12, Type: pos},
		{Name: "gain", Offset: 28, Type: Float32},
	})
	require.NoError(t, err)
	rows := []map[string]any{
		{"id": int32(1), "count": int64(-5), "pos": map[string]any{"x": 1.25, "y": 2.25}, "gain": float32(0.5)},
		{"id": int32(-7), "count": int64(1) << 40, "pos": map[string]any{"x": -3.5, "y": 4.75}, "gain": float32(-1.5)},
	}
	file := writeCompoundFile(t, "nested.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompound("/a", ct, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, dw.Write(rows))
	})
	_, ds := openCompoundDataset(t, file, "/a")
	first, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Equal(t, int32(-7), first[1]["id"])
	require.Equal(t, int64(1)<<40, first[1]["count"])
	require.Equal(t, core.CompoundValue{"x": -3.5, "y": 4.75}, first[1]["pos"])
	require.Equal(t, float32(-1.5), first[1]["gain"])

	file2 := writeCompoundFile(t, "copy.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompound("/b", ct, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, dw.Write(first))
	})
	_, ds2 := openCompoundDataset(t, file2, "/b")
	second, err := ds2.ReadCompound()
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// Same layout through the deprecated and the new method: the same datatype
// message bytes in the file, and both decode as the C library would.
func TestCreateCompound_SameDatatypeAsDeprecated(t *testing.T) {
	int32Type, err := core.NewIntegerDatatype(4, true)
	require.NoError(t, err)
	f64Type, err := core.NewFloatDatatype(8)
	require.NoError(t, err)
	strType, err := core.NewStringDatatype(16, core.StringNullTerm, core.CharsetASCII)
	require.NoError(t, err)
	innerCore, err := core.CreateCompoundTypeFromFields([]core.CompoundFieldDef{
		{Name: "x", Offset: 0, Type: f64Type},
		{Name: "y", Offset: 8, Type: f64Type},
	})
	require.NoError(t, err)
	inner, err := NewPackedCompoundType([]CompoundMember{{Name: "x", Type: Float64}, {Name: "y", Type: Float64}})
	require.NoError(t, err)

	layouts := []struct {
		name    string
		fields  []core.CompoundFieldDef
		members []CompoundMember
	}{
		{
			name:    "key_value",
			fields:  []core.CompoundFieldDef{{Name: "key", Offset: 0, Type: strType}, {Name: "value", Offset: 16, Type: strType}},
			members: []CompoundMember{{Name: "key", Type: FixedString{Size: 16}}, {Name: "value", Type: FixedString{Size: 16}}},
		},
		{
			name:    "numeric",
			fields:  []core.CompoundFieldDef{{Name: "id", Offset: 0, Type: int32Type}, {Name: "v", Offset: 4, Type: f64Type}},
			members: []CompoundMember{{Name: "id", Type: Int32}, {Name: "v", Type: Float64}},
		},
		{
			name:    "nested",
			fields:  []core.CompoundFieldDef{{Name: "id", Offset: 0, Type: int32Type}, {Name: "pos", Offset: 4, Type: innerCore}},
			members: []CompoundMember{{Name: "id", Type: Int32}, {Name: "pos", Type: inner}},
		},
	}
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			old, err := core.CreateCompoundTypeFromFields(l.fields)
			require.NoError(t, err)
			ct, err := NewPackedCompoundType(l.members)
			require.NoError(t, err)
			file := writeCompoundFile(t, l.name+".h5", func(fw *FileWriter) {
				_, err := fw.CreateCompoundDataset("/old", old, []uint64{2})
				require.NoError(t, err)
				_, err = fw.CreateCompound("/new", ct, []uint64{2})
				require.NoError(t, err)
			})
			oldBytes := compoundDatatypeBytes(t, file, "/old")
			newBytes := compoundDatatypeBytes(t, file, "/new")
			require.Equal(t, oldBytes, newBytes)
			// Version 3 compound, member count in the class bit field
			// (H5Odtype.c:358).
			require.Equal(t, byte(0x36), oldBytes[0])
			require.Equal(t, uint16(len(l.members)), binary.LittleEndian.Uint16(oldBytes[1:3]))
		})
	}
}

// A message assembled from a header and Properties (no Members) is still
// accepted by the deprecated method, as before.
func TestCreateCompoundDataset_PropertiesOnlyMessage(t *testing.T) {
	ct := compoundKVType(t)
	enc, err := core.EncodeDatatypeMessage(ct.dt)
	require.NoError(t, err)
	propsOnly := &core.DatatypeMessage{
		Class: core.DatatypeCompound, Version: 3, Size: 32,
		ClassBitField: uint32(binary.LittleEndian.Uint16(enc[1:3])),
		Properties:    enc[8:],
	}
	file := writeCompoundFile(t, "props.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompoundDataset("/kv", propsOnly, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, dw.Write([]map[string]any{{"key": "k", "value": "v"}}))
	})
	_, ds := openCompoundDataset(t, file, "/kv")
	got, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Equal(t, []core.CompoundValue{{"key": "k", "value": "v"}}, got)
}

// Chunked, filtered and extendible compound datasets, through the new and
// the deprecated method. The deprecated method refused WithChunkDims before
// TASK-065 ("chunked compound datasets not yet implemented").
func TestCreateCompound_Chunked(t *testing.T) {
	rows := make([]map[string]any, 10)
	for i := range rows {
		rows[i] = map[string]any{"key": "k" + string(rune('0'+i)), "value": "value"}
	}
	old, err := core.CreateCompoundTypeFromFields([]core.CompoundFieldDef{
		{Name: "key", Offset: 0, Type: compoundKVType(t).dt.Members[0].Type},
		{Name: "value", Offset: 16, Type: compoundKVType(t).dt.Members[1].Type},
	})
	require.NoError(t, err)
	create := map[string]func(fw *FileWriter, opts ...DatasetOption) (*DatasetWriter, error){
		"new": func(fw *FileWriter, opts ...DatasetOption) (*DatasetWriter, error) {
			return fw.CreateCompound("/kv", compoundKVType(t), []uint64{10}, opts...)
		},
		"deprecated": func(fw *FileWriter, opts ...DatasetOption) (*DatasetWriter, error) {
			return fw.CreateCompoundDataset("/kv", old, []uint64{10}, opts...)
		},
	}
	options := map[string][]DatasetOption{
		"chunked":    {WithChunkDims([]uint64{4})},
		"gzip":       {WithChunkDims([]uint64{4}), WithGZIPCompression(6)},
		"shuffle":    {WithChunkDims([]uint64{5}), WithShuffle(), WithGZIPCompression(1), WithFletcher32()},
		"extendible": {WithChunkDims([]uint64{4}), WithMaxDims([]uint64{Unlimited})},
	}
	for cname, mk := range create {
		for oname, opts := range options {
			t.Run(cname+"/"+oname, func(t *testing.T) {
				file := writeCompoundFile(t, "c.h5", func(fw *FileWriter) {
					dw, err := mk(fw, opts...)
					require.NoError(t, err)
					require.NoError(t, dw.Write(rows))
				})
				_, ds := openCompoundDataset(t, file, "/kv")
				got, err := ds.ReadCompound()
				require.NoError(t, err)
				require.Len(t, got, len(rows))
				for i, r := range rows {
					require.Equal(t, r, map[string]any(got[i]), "row %d", i)
				}
			})
		}
	}
}

// No option is silently ignored on the compound path: datatype options do
// not apply, and filters and maximum dimensions need chunked layout
// (H5Dint.c:1309, H5Dcontig.c:457). Before TASK-065 the deprecated method
// ignored every one of these options.
func TestCreateCompound_RejectedOptions(t *testing.T) {
	cases := map[string]struct {
		opt  DatasetOption
		want string
	}{
		"string size": {WithStringSize(8), "WithStringSize"},
		"array dims":  {WithArrayDims([]uint64{2}), "WithArrayDims"},
		"enum values": {WithEnumValues([]string{"a"}, []int64{1}), "WithEnumValues"},
		"opaque tag":  {WithOpaqueTag("t", 4), "WithOpaqueTag"},
		"gzip":        {WithGZIPCompression(6), "filters can only be used with chunked layout"},
		"shuffle":     {WithShuffle(), "filters can only be used with chunked layout"},
		"fletcher32":  {WithFletcher32(), "filters can only be used with chunked layout"},
		"max dims":    {WithMaxDims([]uint64{Unlimited}), "require chunked layout"},
	}
	old := compoundKVType(t).dt
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeCompoundFile(t, "r.h5", func(fw *FileWriter) {
				_, err := fw.CreateCompound("/a", compoundKVType(t), []uint64{4}, tc.opt)
				require.ErrorContains(t, err, tc.want)
				_, err = fw.CreateCompoundDataset("/b", old, []uint64{4}, tc.opt)
				require.ErrorContains(t, err, tc.want)
			})
		})
	}
	writeCompoundFile(t, "r.h5", func(fw *FileWriter) {
		_, err := fw.CreateCompound("/a", compoundKVType(t), []uint64{4}, WithChunkDims([]uint64{8}))
		require.ErrorContains(t, err, "cannot exceed dataset dimension")
		_, err = fw.CreateCompound("/a", compoundKVType(t), []uint64{4}, WithChunkDims([]uint64{2}), WithMaxDims([]uint64{3}))
		require.ErrorContains(t, err, "must be >= dims")
		_, err = fw.CreateCompound("/a", nil, []uint64{4})
		require.ErrorContains(t, err, "compound type is nil")
		_, err = fw.CreateCompound("/a", &CompoundType{}, []uint64{4})
		require.ErrorContains(t, err, "compound type is nil")
		_, err = fw.CreateCompound("", compoundKVType(t), []uint64{4})
		require.Error(t, err)
		_, err = fw.CreateCompound("/a", compoundKVType(t), nil)
		require.Error(t, err)
		_, err = fw.CreateCompoundDataset("/a", &core.DatatypeMessage{Class: core.DatatypeFixed, Size: 4}, []uint64{1})
		require.ErrorContains(t, err, "datatype must be compound")
		_, err = fw.CreateCompoundDataset("/a", &core.DatatypeMessage{Class: core.DatatypeCompound, Size: 4}, []uint64{1})
		require.ErrorContains(t, err, "invalid compound datatype")
	})
}

// Values Write takes for each member type, written and checked byte by byte.
func TestCreateCompound_WriteMemberTypes(t *testing.T) {
	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "i8", Type: Int8},
		{Name: "u16", Type: Uint16},
		{Name: "u64", Type: Uint64},
		{Name: "f32", Type: Float32},
		{Name: "s", Type: FixedString{Size: 4, Charset: CharsetUTF8}},
		{Name: "arr", Type: ArrayType{Elem: Int16, Dims: []uint64{2}}},
		{Name: "e", Type: EnumType{Base: Uint8, Names: []string{"red", "green"}, Values: []int64{3, 7}}},
		{Name: "e2", Type: EnumType{Base: Int16, Names: []string{"neg"}, Values: []int64{-2}}},
		{Name: "ref", Type: ObjectReference},
		{Name: "reg", Type: RegionReference},
	})
	require.NoError(t, err)
	var reg [12]byte
	for i := range reg {
		reg[i] = byte(i + 1)
	}
	row := map[string]any{
		"i8": -3, "u16": uint32(65535), "u64": uint64(math.MaxUint64), "f32": 1.5,
		"s": "hé", "arr": [2]int16{-1, 2}, "e": "green", "e2": int16(-2), "ref": uint64(0x1122), "reg": reg,
	}
	buf, err := encodeCompoundData([]map[string]any{row}, ct.dt, uint64(ct.Size()))
	require.NoError(t, err)

	want := []byte{0xFD, 0xFF, 0xFF}
	want = binary.LittleEndian.AppendUint64(want, math.MaxUint64)
	want = binary.LittleEndian.AppendUint32(want, math.Float32bits(1.5))
	want = append(want, 'h', 0xC3, 0xA9, 0, 0xFF, 0xFF, 2, 0, 7, 0xFE, 0xFF)
	want = binary.LittleEndian.AppendUint64(want, 0x1122)
	want = append(want, reg[:]...)
	require.Equal(t, want, buf)

	// The enumeration also takes its integer values.
	row["e"] = 3
	buf, err = encodeCompoundData([]map[string]any{row}, ct.dt, uint64(ct.Size()))
	require.NoError(t, err)
	require.Equal(t, byte(3), buf[23])
	row["reg"] = reg[:]
	_, err = encodeCompoundData([]map[string]any{row}, ct.dt, uint64(ct.Size()))
	require.NoError(t, err)
}

func TestCreateCompound_WriteErrors(t *testing.T) {
	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "id", Type: Int8},
		{Name: "u", Type: Uint8},
		{Name: "f", Type: Float64},
		{Name: "s", Type: FixedString{Size: 4}},
		{Name: "arr", Type: ArrayType{Elem: Int8, Dims: []uint64{2}}},
		{Name: "e", Type: EnumType{Base: Int8, Names: []string{"a"}, Values: []int64{1}}},
		{Name: "ref", Type: ObjectReference},
		{Name: "pos", Type: compoundKVType(t)},
	})
	require.NoError(t, err)
	good := func() map[string]any {
		return map[string]any{
			"id": 1, "u": 2, "f": 1.0, "s": "abc", "arr": []int8{1, 2}, "e": "a", "ref": uint64(8),
			"pos": map[string]any{"key": "k", "value": "v"},
		}
	}
	_, err = encodeCompoundData([]map[string]any{good()}, ct.dt, uint64(ct.Size()))
	require.NoError(t, err)

	cases := map[string]struct {
		set  func(r map[string]any)
		want string
	}{
		"missing member":   {func(r map[string]any) { delete(r, "f") }, `member "f": no value`},
		"unknown key":      {func(r map[string]any) { r["zz"] = 1; r["zy"] = 2 }, `member "zy": not a member`},
		"int range":        {func(r map[string]any) { r["id"] = 128 }, "value 128 does not fit the 1-byte integer"},
		"negative uint":    {func(r map[string]any) { r["u"] = -1 }, "value -1 does not fit"},
		"uint range":       {func(r map[string]any) { r["u"] = uint64(256) }, "value 256 does not fit"},
		"float into int":   {func(r map[string]any) { r["id"] = 1.0 }, "expects a Go integer, got float64"},
		"int into float":   {func(r map[string]any) { r["f"] = 1 }, "expects float32 or float64, got int"},
		"string too long":  {func(r map[string]any) { r["s"] = "abcd" }, "at most 3 bytes"},
		"string type":      {func(r map[string]any) { r["s"] = 5 }, "expects a string"},
		"array length":     {func(r map[string]any) { r["arr"] = []int8{1} }, "array of 2 elements, got 1"},
		"array not slice":  {func(r map[string]any) { r["arr"] = 1 }, "expects a slice"},
		"array element":    {func(r map[string]any) { r["arr"] = []int{1, 300} }, `member "arr[1]"`},
		"enum name":        {func(r map[string]any) { r["e"] = "b" }, `"b" is not a member of the enumeration`},
		"enum value":       {func(r map[string]any) { r["e"] = 2 }, "value 2 is not a member"},
		"enum type":        {func(r map[string]any) { r["e"] = 1.5 }, "member name or an integer value"},
		"ref type":         {func(r map[string]any) { r["ref"] = "x" }, "uint64 object address"},
		"ref length":       {func(r map[string]any) { r["ref"] = []byte{1} }, "expects 8 bytes, got 1"},
		"nested type":      {func(r map[string]any) { r["pos"] = "x" }, `member "pos": nested compound expects map[string]any`},
		"nested missing":   {func(r map[string]any) { r["pos"] = map[string]any{"key": "k"} }, `member "pos.value": no value`},
		"nested too long":  {func(r map[string]any) { r["pos"] = map[string]any{"key": "0123456789abcdef", "value": ""} }, `member "pos.key"`},
		"nil array":        {func(r map[string]any) { r["arr"] = nil }, "expects a slice"},
		"nil ref":          {func(r map[string]any) { r["ref"] = nil }, "uint64 object address"},
		"wrong slice elem": {func(r map[string]any) { r["ref"] = []int{1, 2, 3, 4, 5, 6, 7, 8} }, "uint64 object address"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := good()
			tc.set(r)
			_, err := encodeCompoundData([]map[string]any{good(), r}, ct.dt, 2*uint64(ct.Size()))
			require.ErrorContains(t, err, "element 1")
			require.ErrorContains(t, err, tc.want)
		})
	}

	_, err = encodeCompoundData([]map[string]any{good()}, ct.dt, 2*uint64(ct.Size()))
	require.ErrorContains(t, err, "got 1 elements")
	_, err = encodeCompoundData([]int32{1}, ct.dt, uint64(ct.Size()))
	require.ErrorContains(t, err, "expects []map[string]any, got []int32")
}

// Member types Write cannot convert are reported, not written wrongly; the
// bytes can still be written with WriteRaw.
func TestCreateCompound_WriteUnsupportedMemberTypes(t *testing.T) {
	half, err := core.NewFloatDatatype(2)
	require.NoError(t, err)
	be := &core.DatatypeMessage{Class: core.DatatypeFixed, Size: 4, ClassBitField: 0x01}
	partial := &core.DatatypeMessage{Class: core.DatatypeFixed, Size: 4, Precision: 12}
	spacePad, err := core.NewStringDatatype(4, core.StringSpacePad, core.CharsetASCII)
	require.NoError(t, err)
	opaque, err := core.NewOpaqueDatatype(4, "tag")
	require.NoError(t, err)

	dst := make([]byte, 4)
	require.ErrorContains(t, encodeCompoundMemberValue(dst[:2], half, float32(1), "h"), "not IEEE")
	require.ErrorContains(t, encodeCompoundMemberValue(dst, partial, 1, "p"), "precision 12")
	require.ErrorContains(t, encodeCompoundMemberValue(dst, opaque, []byte{1}, "o"), "use WriteRaw")

	require.NoError(t, encodeCompoundMemberValue(dst, be, 0x01020304, "be"))
	require.Equal(t, []byte{1, 2, 3, 4}, dst)
	require.NoError(t, encodeCompoundMemberValue(dst, spacePad, "ab", "sp"))
	require.Equal(t, []byte("ab  "), dst)
	require.NoError(t, encodeCompoundMemberValue(dst, spacePad, "abcd", "sp"))
}

// Write on a compound dataset written with WriteRaw-only expectations: the
// map form and WriteRaw give the same bytes.
func TestCreateCompound_WriteMatchesWriteRaw(t *testing.T) {
	ct := compoundKVType(t)
	raw := make([]byte, 64)
	copy(raw[0:], "alpha")
	copy(raw[16:], "first")
	copy(raw[32:], "beta")
	copy(raw[48:], "second")
	buf, err := encodeCompoundData([]map[string]any{
		{"key": "alpha", "value": "first"},
		{"key": "beta", "value": "second"},
	}, ct.dt, 64)
	require.NoError(t, err)
	require.Equal(t, raw, buf)
}

// --- Review fixes (review-065) ---

// A chunk of 4 GiB or more cannot be indexed by the v1 B-tree, whose chunk
// size field is 32 bits: H5D__chunk_init rejects it (H5Dchunk.c:1197-1199).
// The check runs at create time, before anything is allocated.
func TestCreateCompound_ChunkOver4GiB(t *testing.T) {
	big, err := NewCompoundType(1<<31, []CompoundMember{{Name: "a", Type: Int8}})
	require.NoError(t, err)
	writeCompoundFile(t, "big.h5", func(fw *FileWriter) {
		_, err := fw.CreateCompound("/c", big, []uint64{2}, WithChunkDims([]uint64{2}))
		require.ErrorContains(t, err, "chunk size must be < 4GB")
		_, err = fw.CreateCompoundDataset("/d", big.dt, []uint64{2}, WithChunkDims([]uint64{2}))
		require.ErrorContains(t, err, "chunk size must be < 4GB")
		_, err = fw.CreateDataset("/e", ArrayUint8, []uint64{2},
			WithArrayDims([]uint64{1 << 31}), WithChunkDims([]uint64{2}))
		require.ErrorContains(t, err, "chunk size must be < 4GB")
		// Just under the limit is allowed: 2 x (2^31 - 1) bytes.
		under, err := NewCompoundType(1<<31-1, []CompoundMember{{Name: "a", Type: Int8}})
		require.NoError(t, err)
		_, err = fw.CreateCompound("/f", under, []uint64{2}, WithChunkDims([]uint64{2}))
		require.NoError(t, err)
	})
}

// The byte size of a dataset is element count x element size, both checked
// for overflow. 0xAAAAAAAAAAAAAAAB x 3 wraps to 1 byte, and 2^32 x 2^32
// elements wrap to 0: before the fix both were accepted with a layout that
// does not match the dataspace.
func TestCreateCompound_SizeOverflow(t *testing.T) {
	three, err := NewCompoundType(3, []CompoundMember{{Name: "a", Type: Int8}})
	require.NoError(t, err)
	wrapDims := []uint64{0xAAAAAAAAAAAAAAAB}
	countDims := []uint64{1 << 32, 1 << 32}
	writeCompoundFile(t, "ovf.h5", func(fw *FileWriter) {
		_, err := fw.CreateCompound("/a", three, wrapDims)
		require.ErrorContains(t, err, "exceeds")
		_, err = fw.CreateCompound("/b", three, wrapDims, WithChunkDims([]uint64{1}))
		require.ErrorContains(t, err, "exceeds")
		_, err = fw.CreateCompound("/c", three, countDims, WithChunkDims([]uint64{1, 1}))
		require.ErrorContains(t, err, "exceeds")
		_, err = fw.CreateDataset("/d", Int64, []uint64{1 << 62})
		require.ErrorContains(t, err, "exceeds")
		_, err = fw.CreateDataset("/e", Int8, countDims, WithChunkDims([]uint64{1, 1}))
		require.ErrorContains(t, err, "exceeds")
	})
}

// A float64 outside the float32 range is an error, not an infinity;
// infinities and values in range are written.
func TestCreateCompound_Float32Range(t *testing.T) {
	ct, err := NewPackedCompoundType([]CompoundMember{{Name: "f", Type: Float32}})
	require.NoError(t, err)
	for _, v := range []float64{1e39, -1e39, math.MaxFloat64} {
		_, err := encodeCompoundData([]map[string]any{{"f": v}}, ct.dt, 4)
		require.ErrorContains(t, err, "out of the float32 range", "value %g", v)
	}
	for _, v := range []float64{math.MaxFloat32, -math.MaxFloat32, math.Inf(1), math.Inf(-1), 1.5} {
		_, err := encodeCompoundData([]map[string]any{{"f": v}}, ct.dt, 4)
		require.NoError(t, err, "value %g", v)
	}
}

// The constructors and Members() copy the caller's slices: changing them
// afterwards does not change the type.
func TestCompoundType_DeepCopy(t *testing.T) {
	dims := []uint64{2, 3}
	names := []string{"off", "on"}
	values := []int64{0, 1}
	members := []CompoundMember{
		{Name: "v", Type: ArrayType{Elem: Int16, Dims: dims}},
		{Name: "e", Offset: 12, Type: EnumType{Base: Int8, Names: names, Values: values}},
	}
	ct, err := NewCompoundType(13, members)
	require.NoError(t, err)
	dims[0], names[0], values[0] = 9, "x", 7
	members[0].Name = "changed"

	got := ct.Members()
	require.Equal(t, "v", got[0].Name)
	require.Equal(t, []uint64{2, 3}, got[0].Type.(ArrayType).Dims)
	require.Equal(t, []string{"off", "on"}, got[1].Type.(EnumType).Names)
	require.Equal(t, []int64{0, 1}, got[1].Type.(EnumType).Values)

	got[0].Type.(ArrayType).Dims[0] = 5
	got[1].Type.(EnumType).Names[0] = "y"
	again := ct.Members()
	require.Equal(t, []uint64{2, 3}, again[0].Type.(ArrayType).Dims)
	require.Equal(t, []string{"off", "on"}, again[1].Type.(EnumType).Names)

	packed, err := NewPackedCompoundType([]CompoundMember{{Name: "v", Type: ArrayType{Elem: Int8, Dims: dims}}})
	require.NoError(t, err)
	dims[1] = 4
	require.Equal(t, []uint64{9, 3}, packed.Members()[0].Type.(ArrayType).Dims)
}

// NewPackedCompoundType packs nested compounds recursively, as H5T__pack
// does (H5Tcompound.c:512-530): a nested compound's members are sorted by
// offset and placed without gaps, also inside an array.
func TestNewPackedCompoundType_PacksNested(t *testing.T) {
	inner, err := NewCompoundType(16, []CompoundMember{
		{Name: "a", Offset: 8, Type: Int32},
		{Name: "b", Offset: 0, Type: Int8},
	})
	require.NoError(t, err)
	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "in", Type: inner},
		{Name: "tag", Type: Int8},
		{Name: "arr", Type: ArrayType{Elem: inner, Dims: []uint64{2}}},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(5+1+10), ct.Size())
	m := ct.Members()
	require.Equal(t, []uint32{0, 5, 6}, []uint32{m[0].Offset, m[1].Offset, m[2].Offset})
	packedInner := m[0].Type.(*CompoundType)
	require.Equal(t, uint32(5), packedInner.Size())
	im := packedInner.Members()
	require.Equal(t, "b", im[0].Name)
	require.Equal(t, uint32(0), im[0].Offset)
	require.Equal(t, "a", im[1].Name)
	require.Equal(t, uint32(1), im[1].Offset)
	require.Equal(t, uint32(5), m[2].Type.(ArrayType).Elem.(*CompoundType).Size())
	// The caller's inner type is unchanged.
	require.Equal(t, uint32(16), inner.Size())

	_, err = NewPackedCompoundType(nil)
	require.ErrorContains(t, err, "0 members")
}

// An enumeration over Uint64 holds values of 2^63 and above as their
// two's-complement bit pattern in Values (int64(u)); Write accepts such a
// member by name, as a uint64 and as the int64 bit pattern.
func TestEnumType_Uint64HighValues(t *testing.T) {
	top := uint64(math.MaxUint64)
	ct, err := NewPackedCompoundType([]CompoundMember{{Name: "e", Type: EnumType{
		Base: Uint64, Names: []string{"low", "top"}, Values: []int64{1, -1},
	}}})
	require.NoError(t, err)
	want := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	for _, v := range []any{"top", top, int64(-1)} {
		buf, err := encodeCompoundData([]map[string]any{{"e": v}}, ct.dt, 8)
		require.NoError(t, err, "value %v", v)
		require.Equal(t, want, buf)
	}
	// A negative value is still out of range for a narrower unsigned base.
	_, err = NewPackedCompoundType([]CompoundMember{{Name: "e", Type: EnumType{
		Base: Uint32, Names: []string{"x"}, Values: []int64{-1},
	}}})
	require.ErrorContains(t, err, "does not fit")
}

// A chunk may be larger than the current extent of a dimension that can grow:
// C compares chunk extents only with fixed maximum dimensions
// (H5Dchunk.c:1078-1081). The #65 reporter's growing key/value table starts
// with one row in a 64-row chunk.
func TestCreateCompound_GrowingTable(t *testing.T) {
	ct := compoundKVType(t)
	file := writeCompoundFile(t, "grow.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompound("/kv", ct, []uint64{1},
			WithChunkDims([]uint64{64}), WithMaxDims([]uint64{Unlimited}))
		require.NoError(t, err)
		require.NoError(t, dw.Write([]map[string]any{{"key": "a", "value": "1"}}))
		require.NoError(t, dw.Resize([]uint64{3}))
		require.NoError(t, dw.Write([]map[string]any{
			{"key": "a", "value": "1"}, {"key": "b", "value": "2"}, {"key": "c", "value": "3"},
		}))
		// A fixed maximum still bounds the chunk.
		_, err = fw.CreateCompound("/fixed", ct, []uint64{1},
			WithChunkDims([]uint64{64}), WithMaxDims([]uint64{10}))
		require.ErrorContains(t, err, "cannot exceed maximum dimension")
		_, err = fw.CreateCompound("/fits", ct, []uint64{1},
			WithChunkDims([]uint64{10}), WithMaxDims([]uint64{10}))
		require.NoError(t, err)
	})
	_, ds := openCompoundDataset(t, file, "/kv")
	got, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Len(t, got, 3)
	for i, k := range []string{"a", "b", "c"} {
		require.Equal(t, k, got[i]["key"])
		require.Equal(t, string(rune('1'+i)), got[i]["value"])
	}
}
