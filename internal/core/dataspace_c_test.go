package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestParseDataspace_ClassesLikeC checks the extent H5O__sdspace_decode builds
// (H5Osdspace.c:120-215): a scalar has rank 0 and one element, a version 2
// message takes its class from the type byte, and a null dataspace has no
// elements. Red before TASK-068: a scalar came back as Dimensions [1] and a
// version 2 null dataspace as a scalar.
func TestParseDataspace_ClassesLikeC(t *testing.T) {
	t.Run("v1 scalar", func(t *testing.T) {
		ds, err := ParseDataspaceMessage([]byte{1, 0, 0, 0, 0, 0, 0, 0})
		require.NoError(t, err)
		require.Equal(t, DataspaceScalar, ds.Type)
		require.Empty(t, ds.Dimensions)
		require.Equal(t, uint64(1), ds.TotalElements())
	})
	t.Run("v2 scalar", func(t *testing.T) {
		ds, err := ParseDataspaceMessage([]byte{2, 0, 0, byte(DataspaceScalar)})
		require.NoError(t, err)
		require.Equal(t, DataspaceScalar, ds.Type)
		require.Empty(t, ds.Dimensions)
		require.Equal(t, uint64(1), ds.TotalElements())
	})
	t.Run("v2 null", func(t *testing.T) {
		ds, err := ParseDataspaceMessage([]byte{2, 0, 0, byte(DataspaceNull)})
		require.NoError(t, err)
		require.Equal(t, DataspaceNull, ds.Type)
		require.Empty(t, ds.Dimensions)
		require.Equal(t, uint64(0), ds.TotalElements())
	})
	t.Run("v2 simple", func(t *testing.T) {
		data := []byte{2, 1, 0, byte(DataspaceSimple), 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(data[4:], 7)
		ds, err := ParseDataspaceMessage(data)
		require.NoError(t, err)
		require.Equal(t, DataspaceSimple, ds.Type)
		require.Equal(t, []uint64{7}, ds.Dimensions)
	})
}

// TestParseDataspace_RejectsLikeC: H5O__sdspace_decode refuses a scalar or
// null type with a rank (H5Osdspace.c:149-150) and a dimension larger than
// its maximum (H5Osdspace.c:197-201). An unknown class byte is refused too;
// C has no class beyond H5S_NULL (H5Spublic.h), so nothing could read it.
// Red before TASK-068: all three were accepted.
func TestParseDataspace_RejectsLikeC(t *testing.T) {
	dims := func(head []byte, vals ...uint64) []byte {
		out := append([]byte{}, head...)
		for _, v := range vals {
			out = binary.LittleEndian.AppendUint64(out, v)
		}
		return out
	}

	for name, data := range map[string][]byte{
		"v2 scalar with rank": dims([]byte{2, 1, 0, byte(DataspaceScalar)}, 4),
		"v2 null with rank":   dims([]byte{2, 1, 0, byte(DataspaceNull)}, 4),
		"v2 unknown class":    {2, 0, 0, 7},
		"v1 size > maxdim":    dims([]byte{1, 1, 1, 0, 0, 0, 0, 0}, 10, 5),
		"v2 size > maxdim":    dims([]byte{2, 2, 1, byte(DataspaceSimple)}, 3, 10, 3, 9),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDataspaceMessage(data)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}

	// An unlimited maximum is never exceeded.
	ds, err := ParseDataspaceMessage(dims([]byte{1, 1, 1, 0, 0, 0, 0, 0}, 10, ^uint64(0)))
	require.NoError(t, err)
	require.Equal(t, []uint64{^uint64(0)}, ds.MaxDims)
}

// TestEncodeAttributeMessage_ParsedScalar: a scalar attribute read from a file
// is re-encoded as C encodes it, rank 0 with no dimensions (H5Osdspace.c:
// 249-297), not refused and not turned into a one-element simple dataspace.
func TestEncodeAttributeMessage_ParsedScalar(t *testing.T) {
	space := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	ds, err := ParseDataspaceMessage(space)
	require.NoError(t, err)

	int32Type, err := NewIntegerDatatype(4, true)
	require.NoError(t, err)

	msg, err := EncodeAttributeMessage("a", int32Type, ds, []byte{1, 0, 0, 0})
	require.NoError(t, err)

	attr, err := ParseAttributeMessage(msg, binary.LittleEndian)
	require.NoError(t, err)
	require.Equal(t, DataspaceScalar, attr.Dataspace.Type)
	require.Empty(t, attr.Dataspace.Dimensions)

	// A null dataspace keeps its class (version 2, type byte 2).
	null, err := ParseDataspaceMessage([]byte{2, 0, 0, byte(DataspaceNull)})
	require.NoError(t, err)
	msg, err = EncodeAttributeMessage("n", int32Type, null, nil)
	require.NoError(t, err)
	attr, err = ParseAttributeMessage(msg, binary.LittleEndian)
	require.NoError(t, err)
	require.Equal(t, DataspaceNull, attr.Dataspace.Type)
}
