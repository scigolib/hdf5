package hdf5

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompoundMembers_WriteReadRoundTrip: the member types the compound reader
// maps (TASK-068 items c and d) written through CreateCompound and read back
// with ReadCompound. An array member comes back flattened in C order as a
// typed slice; unsigned members keep values above the signed range, which
// used to be read as negative int32/int64; an enum reads as its integer base,
// as H5T__conv_enum_numeric converts it (H5Tconv_enum.c:507-561).
func TestCompoundMembers_WriteReadRoundTrip(t *testing.T) {
	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "u32", Type: Uint32},
		{Name: "u64", Type: Uint64},
		{Name: "i16", Type: Int16},
		{Name: "grid", Type: ArrayType{Elem: Int16, Dims: []uint64{2, 3}}},
		{Name: "f", Type: Float32},
		{Name: "e", Type: EnumType{Base: Int8, Names: []string{"off", "on"}, Values: []int64{0, 1}}},
	})
	require.NoError(t, err)

	rows := []map[string]any{
		{
			"u32": uint32(1<<31 + 5), "u64": uint64(1<<63 + 7), "i16": int16(-3),
			"grid": []int16{1, 2, 3, -4, -5, -6}, "f": float32(1.5), "e": int64(1),
		},
		{
			"u32": uint32(0), "u64": uint64(42), "i16": int16(32767),
			"grid": []int16{-32768, 0, 0, 0, 0, 32767}, "f": float32(-0.25), "e": int64(0),
		},
	}
	file := writeCompoundFile(t, "members.h5", func(fw *FileWriter) {
		dw, err := fw.CreateCompound("/m", ct, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, dw.Write(rows))
	})

	_, ds := openCompoundDataset(t, file, "/m")
	got, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Len(t, got, 2)

	for i, r := range rows {
		require.Equal(t, r["u32"], got[i]["u32"], "row %d", i)
		require.Equal(t, r["u64"], got[i]["u64"], "row %d", i)
		require.Equal(t, r["i16"], got[i]["i16"], "row %d", i)
		require.Equal(t, r["grid"], got[i]["grid"], "row %d", i)
		require.Equal(t, r["f"], got[i]["f"], "row %d", i)
		require.EqualValues(t, r["e"], got[i]["e"], "row %d", i)
	}
}
