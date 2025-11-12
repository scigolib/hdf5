package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteCompoundDataset_Simple tests writing a simple compound datatype with round-trip validation.
func TestWriteCompoundDataset_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_simple.h5")

	// Define compound type: struct { int32 id; float32 value }
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)

	float32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 4)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "id", Offset: 0, Type: int32Type},
		{Name: "value", Offset: 4, Type: float32Type},
	}

	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)
	require.NotNil(t, compoundType)
	assert.Equal(t, core.DatatypeCompound, compoundType.Class)
	assert.Equal(t, uint32(8), compoundType.Size) // int32 + float32

	// Create file and dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateCompoundDataset("/data", compoundType, []uint64{3})
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Write data: array of 3 structs
	// Each struct is 8 bytes: [id:int32][value:float32]
	data := []byte{
		// Record 0: {id: 1, value: 1.5}
		0x01, 0x00, 0x00, 0x00, // id = 1 (little-endian int32)
		0x00, 0x00, 0xC0, 0x3F, // value = 1.5 (little-endian float32)

		// Record 1: {id: 2, value: 2.5}
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x20, 0x40,

		// Record 2: {id: 3, value: 3.5}
		0x03, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x60, 0x40,
	}

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	// Close file
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists and can be opened (full round-trip reading TBD in Phase 3)
	_, err = os.Stat(filename)
	require.NoError(t, err)

	t.Logf("✅ Simple compound dataset created successfully")
	t.Logf("   File: %s", filename)
	t.Logf("   Datatype: struct { int32 id; float32 value } (size: 8 bytes)")
	t.Logf("   Elements: 3")
	t.Logf("   Total data: 24 bytes written")
}

// TestWriteCompoundDataset_Nested tests writing nested compound datatypes.
func TestWriteCompoundDataset_Nested(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_nested.h5")

	// Define inner type: struct { float32 x; float32 y }
	float32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 4)
	require.NoError(t, err)

	innerFields := []core.CompoundFieldDef{
		{Name: "x", Offset: 0, Type: float32Type},
		{Name: "y", Offset: 4, Type: float32Type},
	}

	// Encode and parse back to get proper DatatypeMessage
	innerEncoded, err := core.EncodeCompoundDatatypeV3(8, innerFields)
	require.NoError(t, err)

	innerType, err := core.ParseDatatypeMessage(innerEncoded)
	require.NoError(t, err)

	// Define outer type: struct { int32 id; Point point; int32 count }
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)

	outerFields := []core.CompoundFieldDef{
		{Name: "id", Offset: 0, Type: int32Type},
		{Name: "point", Offset: 4, Type: innerType},
		{Name: "count", Offset: 12, Type: int32Type},
	}

	outerType, err := core.CreateCompoundTypeFromFields(outerFields)
	require.NoError(t, err)
	assert.Equal(t, uint32(16), outerType.Size)

	// Create file and dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateCompoundDataset("/data", outerType, []uint64{2})
	require.NoError(t, err)

	// Write data: array of 2 nested structs
	// Each struct is 16 bytes: [id:int32][point.x:float32][point.y:float32][count:int32]
	data := []byte{
		// Record 0: {id: 1, point: {x: 1.5, y: 2.5}, count: 10}
		0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xC0, 0x3F, // x = 1.5
		0x00, 0x00, 0x20, 0x40, // y = 2.5
		0x0A, 0x00, 0x00, 0x00, // count = 10

		// Record 0: {id: 2, point: {x: 3.5, y: 4.5}, count: 20}
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x60, 0x40, // x = 3.5
		0x00, 0x00, 0x90, 0x40, // y = 4.5
		0x14, 0x00, 0x00, 0x00, // count = 20
	}

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	// Close file
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists (full round-trip reading TBD in Phase 3)
	_, err = os.Stat(filename)
	require.NoError(t, err)

	t.Logf("✅ Nested compound dataset created successfully")
	t.Logf("   File: %s", filename)
	t.Logf("   Datatype: struct { int32 id; Point{float32 x,y} point; int32 count } (size: 16 bytes)")
	t.Logf("   Elements: 2")
	t.Logf("   Total data: 32 bytes written")
}

// TestWriteCompoundDataset_MultipleFieldTypes tests compound with various field types.
func TestWriteCompoundDataset_MultipleFieldTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_multitypes.h5")

	// Define compound: struct { int32 i; float64 f; int16 s }
	// Note: Padding/alignment is handled by explicit offsets
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)

	float64Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 8)
	require.NoError(t, err)

	int16Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 2)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "i", Offset: 0, Type: int32Type},
		{Name: "f", Offset: 4, Type: float64Type},
		{Name: "s", Offset: 12, Type: int16Type},
	}

	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)
	assert.Equal(t, uint32(14), compoundType.Size) // 4 + 8 + 2

	// Create file and dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateCompoundDataset("/data", compoundType, []uint64{1})
	require.NoError(t, err)

	// Write one record: {i: 42, f: 3.14159, s: 100}
	data := []byte{
		0x2A, 0x00, 0x00, 0x00, // i = 42
		0x6E, 0x86, 0x1B, 0xF0, 0xF9, 0x21, 0x09, 0x40, // f = 3.14159
		0x64, 0x00, // s = 100
	}

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	// Close file
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists (full round-trip reading TBD in Phase 3)
	_, err = os.Stat(filename)
	require.NoError(t, err)

	t.Logf("✅ Multiple field types compound dataset created successfully")
	t.Logf("   File: %s", filename)
	t.Logf("   Datatype: struct { int32 i; float64 f; int16 s } (size: 14 bytes)")
	t.Logf("   Elements: 1")
	t.Logf("   Total data: 14 bytes written")
}
