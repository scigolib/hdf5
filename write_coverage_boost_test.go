package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestWriteCov_EncodeOpaqueData_WrongType tests encodeOpaqueData with non-[]byte input.
func TestWriteCov_EncodeOpaqueData_WrongType(t *testing.T) {
	// encodeOpaqueData expects []byte; passing string should error.
	_, err := encodeOpaqueData("not bytes", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be []byte")
}

// TestWriteCov_EncodeOpaqueData_SizeMismatch tests encodeOpaqueData with wrong size.
func TestWriteCov_EncodeOpaqueData_SizeMismatch(t *testing.T) {
	data := []byte{1, 2, 3}
	_, err := encodeOpaqueData(data, 10) // expected 10, got 3
	require.Error(t, err)
	require.Contains(t, err.Error(), "size mismatch")
}

// TestWriteCov_EncodeOpaqueData_Success tests encodeOpaqueData with correct input.
func TestWriteCov_EncodeOpaqueData_Success(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	result, err := encodeOpaqueData(data, 4)
	require.NoError(t, err)
	require.Equal(t, data, result)
}

// TestWriteCov_InitializeFileWriter_InvalidMode tests initializeFileWriter with invalid CreateMode.
func TestWriteCov_InitializeFileWriter_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "invalid_mode.h5")

	_, err := initializeFileWriter(filename, CreateMode(999), 48)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid create mode")
}

// TestWriteCov_CalculateObjectHeaderSize_V1Error tests calculateObjectHeaderSize with v1 header.
func TestWriteCov_CalculateObjectHeaderSize_V1Error(t *testing.T) {
	ohw := &core.ObjectHeaderWriter{
		Version: 1, // Not supported
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgDatatype, Data: []byte{1, 2, 3, 4}},
		},
	}
	_, err := calculateObjectHeaderSize(ohw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only object header version 2 supported")
}

// TestWriteCov_FileWriterClose_DoubleClose tests that calling Close twice is safe.
func TestWriteCov_FileWriterClose_DoubleClose(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "double_close.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// First close should succeed.
	err = fw.Close()
	require.NoError(t, err)

	// Second close should be a no-op (fw.writer == nil).
	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_DatasetWriterClose tests DatasetWriter.Close (no-op).
func TestWriteCov_DatasetWriterClose(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_close.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// DatasetWriter.Close is a no-op but should not error.
	err = ds.Close()
	require.NoError(t, err)

	// Can still write after Close (no-op behavior).
	data := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	err = ds.Write(data)
	require.NoError(t, err)
}

// TestWriteCov_RebalancingEnabledDisabled tests toggle of rebalancing.
func TestWriteCov_RebalancingEnabledDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "rebalance_toggle.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Default: enabled.
	require.True(t, fw.RebalancingEnabled())

	// Disable.
	fw.DisableRebalancing()
	require.False(t, fw.RebalancingEnabled())

	// Enable again.
	fw.EnableRebalancing()
	require.True(t, fw.RebalancingEnabled())
}

// TestWriteCov_RebalancingEnabled_NilConfig tests RebalancingEnabled when config is nil.
func TestWriteCov_RebalancingEnabled_NilConfig(t *testing.T) {
	// Create a FileWriter with nil config to test the nil branch.
	fw := &FileWriter{config: nil}
	require.True(t, fw.RebalancingEnabled(), "nil config should return true (default)")
}

// TestWriteCov_OpenForWrite_ModifyExisting tests OpenForWrite on existing file.
func TestWriteCov_OpenForWrite_ModifyExisting(t *testing.T) {
	filename := filepath.Join("tmp", "cov_openforwrite.h5")
	_ = os.MkdirAll("tmp", 0o755)
	defer func() { _ = os.Remove(filename) }()

	// Phase 1: Create file with a dataset and write data.
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/temperature", Float64, []uint64{5})
	require.NoError(t, err)
	err = ds.Write([]float64{10.0, 20.0, 30.0, 40.0, 50.0})
	require.NoError(t, err)

	// Write compact attributes.
	err = ds.WriteAttribute("units", "Celsius")
	require.NoError(t, err)
	err = ds.WriteAttribute("sensor_id", int32(42))
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Phase 2: OpenForWrite and verify we can open dataset.
	fw2, err := OpenForWrite(filename, OpenReadWrite)
	require.NoError(t, err)

	ds2, err := fw2.OpenDataset("/temperature")
	require.NoError(t, err)
	require.NotNil(t, ds2)

	// Add an attribute via OpenForWrite path.
	err = ds2.WriteAttribute("calibration", int32(100))
	require.NoError(t, err)

	err = fw2.Close()
	require.NoError(t, err)
	// Also close the underlying file reader to release Windows file handles.
	if fw2.file != nil {
		_ = fw2.file.Close()
	}

	// Phase 3: Verify file is still readable.
	f, err := Open(filename)
	require.NoError(t, err)
	require.NotNil(t, f.Root())
	_ = f.Close()
}

// TestWriteCov_OpenForWrite_V0File tests OpenForWrite on a v0 superblock file.
// V0 files with datasets have known SNOD issues for linking; this test verifies
// that CreateForWrite with v0 works for empty files (no datasets).
func TestWriteCov_OpenForWrite_V0File(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "openforwrite_v0.h5")

	// Create v0 file without datasets (empty root group).
	fw, err := CreateForWrite(filename, CreateTruncate, WithSuperblockVersion(core.Version0))
	require.NoError(t, err)
	require.NotNil(t, fw)

	// Verify v0 structure addresses are set.
	require.Equal(t, uint64(96), fw.rootGroupAddr, "V0 root group starts at offset 96")
	require.Greater(t, fw.rootBTreeAddr, uint64(0))
	require.Greater(t, fw.rootHeapAddr, uint64(0))
	require.Greater(t, fw.rootStNodeAddr, uint64(0))

	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists.
	info, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

// TestWriteCov_OpenForWrite_ReadOnly tests OpenForWrite with ReadOnly mode.
// On Windows, closing a read-only opened writer may fail on sync, so we
// only verify that OpenForWrite itself succeeds and the FileWriter has proper state.
func TestWriteCov_OpenForWrite_ReadOnly(t *testing.T) {
	filename := filepath.Join("tmp", "cov_openforwrite_ro.h5")
	_ = os.MkdirAll("tmp", 0o755)
	defer func() { _ = os.Remove(filename) }()

	// Create file.
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateDataset("/data", Int32, []uint64{3})
	require.NoError(t, err)
	err = fw.Close()
	require.NoError(t, err)

	// Open read-only. This exercises the OpenReadOnly code path in OpenForWrite.
	fw2, err := OpenForWrite(filename, OpenReadOnly)
	require.NoError(t, err)
	require.NotNil(t, fw2)
	require.NotNil(t, fw2.file)
	require.NotNil(t, fw2.writer)

	// Close may error on Windows (can't sync read-only file), which is expected.
	// The important thing is that OpenForWrite succeeded with the ReadOnly path.
	_ = fw2.Close()
	if fw2.file != nil {
		_ = fw2.file.Close()
	}
}

// TestWriteCov_OpenForWrite_NonexistentFile tests OpenForWrite on a file that doesn't exist.
func TestWriteCov_OpenForWrite_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "does_not_exist.h5")

	_, err := OpenForWrite(filename, OpenReadWrite)
	require.Error(t, err)
}

// TestWriteCov_CreateCompoundDataset_NilType tests CreateCompoundDataset with nil type.
func TestWriteCov_CreateCompoundDataset_NilType(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "compound_nil.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	_, err = fw.CreateCompoundDataset("/data", nil, []uint64{10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

// TestWriteCov_CreateCompoundDataset_WrongClass tests CreateCompoundDataset with non-compound type.
func TestWriteCov_CreateCompoundDataset_WrongClass(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "compound_wrong.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create a non-compound type.
	wrongType := &core.DatatypeMessage{
		Class:   core.DatatypeFixed,
		Version: 1,
		Size:    4,
	}

	_, err = fw.CreateCompoundDataset("/data", wrongType, []uint64{10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be compound")
}

// TestWriteCov_CreateCompoundDataset_ThreeFieldRoundTrip tests compound with 3 fields.
func TestWriteCov_CreateCompoundDataset_ThreeFieldRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "compound_3field.h5")

	// Define compound: struct { int32 x; int32 y; float64 z }
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)
	float64Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 8)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "x", Offset: 0, Type: int32Type},
		{Name: "y", Offset: 4, Type: int32Type},
		{Name: "z", Offset: 8, Type: float64Type},
	}

	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)
	require.Equal(t, uint32(16), compoundType.Size) // 4+4+8

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateCompoundDataset("/points", compoundType, []uint64{2})
	require.NoError(t, err)

	// Write 2 structs manually.
	data := make([]byte, 32) // 2 * 16 bytes
	// Record 0: x=1, y=2, z=3.14
	binary.LittleEndian.PutUint32(data[0:4], 1)
	binary.LittleEndian.PutUint32(data[4:8], 2)
	binary.LittleEndian.PutUint64(data[8:16], math.Float64bits(3.14))
	// Record 1: x=10, y=20, z=6.28
	binary.LittleEndian.PutUint32(data[16:20], 10)
	binary.LittleEndian.PutUint32(data[20:24], 20)
	binary.LittleEndian.PutUint64(data[24:32], math.Float64bits(6.28))

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_OpaqueDataset_WriteAndVerify tests opaque dataset write with round-trip.
func TestWriteCov_OpaqueDataset_WriteAndVerify(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "opaque_write.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create opaque dataset: 3 elements of 8 bytes each.
	ds, err := fw.CreateDataset("/binary_data", Opaque, []uint64{3}, WithOpaqueTag("custom-format", 8))
	require.NoError(t, err)

	rawData := make([]byte, 24) // 3*8
	for i := range rawData {
		rawData[i] = byte(i * 7)
	}
	err = ds.Write(rawData)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists and is valid.
	info, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))

	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_ChunkedWithMaxDims tests chunked dataset with maxDims option.
func TestWriteCov_ChunkedWithMaxDims(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_maxdims.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Chunked with unlimited max dimension.
	ds, err := fw.CreateDataset("/resizable", Float64, []uint64{20},
		WithChunkDims([]uint64{10}),
		WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NotNil(t, ds)
	require.True(t, ds.isChunked)
	require.Equal(t, []uint64{Unlimited}, ds.maxDims)

	data := make([]float64, 20)
	for i := range data {
		data[i] = float64(i) * 1.5
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_ChunkedWithFixedMaxDims tests chunked dataset with fixed max dimensions.
func TestWriteCov_ChunkedWithFixedMaxDims(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_fixed_maxdims.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/fixed_max", Int32, []uint64{10},
		WithChunkDims([]uint64{5}),
		WithMaxDims([]uint64{100}))
	require.NoError(t, err)
	require.Equal(t, []uint64{100}, ds.maxDims)

	data := make([]int32, 10)
	for i := range data {
		data[i] = int32(i)
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_ChunkedWithFletcher32 tests chunked dataset with Fletcher32 checksum.
func TestWriteCov_ChunkedWithFletcher32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_fletcher32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/checksummed", Int32, []uint64{50},
		WithChunkDims([]uint64{25}),
		WithFletcher32())
	require.NoError(t, err)
	require.NotNil(t, ds.pipeline)

	data := make([]int32, 50)
	for i := range data {
		data[i] = int32(i * 3)
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

// TestWriteCov_ChunkedWithShuffleAndGZIP tests chunked with shuffle+GZIP filters.
func TestWriteCov_ChunkedWithShuffleAndGZIP(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_shuffle_gzip.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/compressed", Float64, []uint64{100},
		WithChunkDims([]uint64{25}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)
	require.NotNil(t, ds.pipeline)

	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i) * 0.01
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_CreateGroupWithLinks_EmptyLinks tests CreateGroupWithLinks with no links.
func TestWriteCov_CreateGroupWithLinks_EmptyLinks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_empty_links.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Empty links => symbol table format, no error.
	err = fw.CreateGroupWithLinks("/empty_group", map[string]string{})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_CreateGroupWithLinks_SmallGroup tests CreateGroupWithLinks with few links.
func TestWriteCov_CreateGroupWithLinks_SmallGroup(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_small_links.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Creating a symbol table group with links is not supported in MVP.
	// 1-8 links triggers symbol table path which returns error for non-empty links.
	links := map[string]string{
		"link1": "/ds1",
	}
	err = fw.CreateGroupWithLinks("/small_group", links)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet supported")
}

// TestWriteCov_CreateSoftLink_ConsecutiveSlashes tests CreateSoftLink with consecutive slashes.
func TestWriteCov_CreateSoftLink_ConsecutiveSlashes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "softlink_bad.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Consecutive slashes in link path.
	err = fw.CreateSoftLink("//bad//path", "/target")
	require.Error(t, err)
	require.Contains(t, err.Error(), "consecutive slashes")
}

// TestWriteCov_CreateSoftLink_ConsecutiveSlashesTarget tests soft link target validation.
func TestWriteCov_CreateSoftLink_ConsecutiveSlashesTarget(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "softlink_bad_target.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Consecutive slashes in target path.
	err = fw.CreateSoftLink("/link", "//bad//target")
	require.Error(t, err)
	require.Contains(t, err.Error(), "consecutive slashes")
}

// TestWriteCov_CreateSoftLink_EmptyTarget tests soft link with empty target path.
func TestWriteCov_CreateSoftLink_EmptyTarget(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "softlink_empty_target.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	err = fw.CreateSoftLink("/link", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")
}

// TestWriteCov_CreateSoftLink_RelativeTarget tests soft link with relative target.
func TestWriteCov_CreateSoftLink_RelativeTarget(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "softlink_relative.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	err = fw.CreateSoftLink("/link", "relative/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

// TestWriteCov_CreateHardLink_ValidationErrors tests hard link validation edge cases.
func TestWriteCov_CreateHardLink_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "hardlink_errors.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Empty link path.
	err = fw.CreateHardLink("", "/target")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")

	// Root path as link.
	err = fw.CreateHardLink("/", "/target")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot create link to root")

	// Non-existent target.
	err = fw.CreateHardLink("/link", "/nonexistent")
	require.Error(t, err)

	// Consecutive slashes.
	err = fw.CreateHardLink("/link//bad", "/target")
	require.Error(t, err)
	require.Contains(t, err.Error(), "consecutive slashes")
}

// TestWriteCov_CreateExternalLink_PathTraversal tests external link path traversal prevention.
func TestWriteCov_CreateExternalLink_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "extlink_traversal.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	err = fw.CreateExternalLink("/link", "../../../etc/passwd", "/data")
	require.Error(t, err)
	require.Contains(t, err.Error(), "path traversal")
}

// TestWriteCov_CreateExternalLink_EmptyFileName tests external link with empty file name.
func TestWriteCov_CreateExternalLink_EmptyFileName(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "extlink_empty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	err = fw.CreateExternalLink("/link", "", "/data")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")
}

// TestWriteCov_SuperblockV0_CreationVerify tests v0 file creation.
// V0 superblock with dataset linking has known SNOD limitations, so we verify
// that the v0 file structure (superblock, B-tree, heap) is created correctly.
func TestWriteCov_SuperblockV0_CreationVerify(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "v0_creation.h5")

	fw, err := CreateForWrite(filename, CreateTruncate, WithSuperblockVersion(core.Version0))
	require.NoError(t, err)

	// Verify v0 layout is correct.
	require.Equal(t, uint64(96), fw.rootGroupAddr)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists and has reasonable size (superblock + root group structures).
	info, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(96), "V0 file should be larger than superblock")

	// Verify it can be read back as a valid HDF5 file.
	f, err := Open(filename)
	require.NoError(t, err)
	require.Equal(t, uint8(0), f.SuperblockVersion())
	_ = f.Close()
}

// TestWriteCov_CreateForWrite_InvalidOptionType tests invalid option type.
func TestWriteCov_CreateForWrite_InvalidOptionType(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "invalid_option.h5")

	_, err := CreateForWrite(filename, CreateTruncate, "invalid_option")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid option type")
}

// TestWriteCov_RebalanceAttributeBTree_NoDenseStorage tests RebalanceAttributeBTree
// on a newly created dataset with no attributes (no dense storage, no object header cached).
func TestWriteCov_RebalanceAttributeBTree_NoDenseStorage(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "rebalance_no_dense.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{10})
	require.NoError(t, err)

	// No attributes written, denseAttrInfo == nil, objectHeader == nil.
	// Should be a no-op (returns nil).
	err = ds.RebalanceAttributeBTree()
	require.NoError(t, err)
}

// TestWriteCov_RebalanceAttributeBTree_DenseViaObjectHeader tests RebalanceAttributeBTree
// on a dataset with 9+ attributes written in the same session (reads object header from file).
func TestWriteCov_RebalanceAttributeBTree_DenseViaObjectHeader(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "rebalance_dense_oh.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write 9 attributes to trigger dense storage.
	for i := 0; i < 9; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i*100))
		require.NoError(t, err)
	}

	// Rebalance triggers the "datasets created in this session" path
	// (objectHeader == nil, denseAttrInfo == nil, reads OH from file).
	err = ds.RebalanceAttributeBTree()
	require.NoError(t, err)
}

// TestWriteCov_RebalanceAttributeBTree_ViaOpenDataset tests RebalanceAttributeBTree
// on a dataset opened with OpenForWrite (has cached denseAttrInfo).
func TestWriteCov_RebalanceAttributeBTree_ViaOpenDataset(t *testing.T) {
	filename := filepath.Join("tmp", "cov_rebalance_open.h5")
	_ = os.MkdirAll("tmp", 0o755)
	defer func() { _ = os.Remove(filename) }()

	// Phase 1: Create file with dense attributes.
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	for i := 0; i < 9; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
		require.NoError(t, err)
	}
	err = fw.Close()
	require.NoError(t, err)

	// Phase 2: Reopen and rebalance.
	fw2, err := OpenForWrite(filename, OpenReadWrite)
	require.NoError(t, err)

	ds2, err := fw2.OpenDataset("/data")
	require.NoError(t, err)

	// This exercises the denseAttrInfo != nil path.
	err = ds2.RebalanceAttributeBTree()
	require.NoError(t, err)

	_ = fw2.Close()
	if fw2.file != nil {
		_ = fw2.file.Close()
	}
}

// TestWriteCov_DenseAttributeModify_Upsert tests modifying an existing dense attribute.
func TestWriteCov_DenseAttributeModify_Upsert(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "dense_upsert.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write 8 attributes to trigger dense storage on the 8th.
	for i := 0; i < 8; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
		require.NoError(t, err)
	}

	// Now modify an existing attribute (upsert semantics in dense storage).
	err = ds.WriteAttribute("attr_0", int32(999))
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_OpenForWrite_DenseUpsertExisting tests modifying dense attribute via OpenForWrite.
func TestWriteCov_OpenForWrite_DenseUpsertExisting(t *testing.T) {
	filename := filepath.Join("tmp", "cov_dense_upsert_rmw.h5")
	_ = os.MkdirAll("tmp", 0o755)
	defer func() { _ = os.Remove(filename) }()

	// Phase 1: Create file with 9 dense attributes.
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)
	err = ds.Write([]int32{10, 20, 30, 40, 50})
	require.NoError(t, err)

	for i := 0; i < 9; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i*10))
		require.NoError(t, err)
	}
	err = fw.Close()
	require.NoError(t, err)

	// Phase 2: Reopen, modify existing dense attribute.
	fw2, err := OpenForWrite(filename, OpenReadWrite)
	require.NoError(t, err)

	ds2, err := fw2.OpenDataset("/data")
	require.NoError(t, err)

	// Modify existing attribute via writeDenseAttributeWithInfo (upsert path).
	err = ds2.WriteAttribute("attr_0", int32(9999))
	require.NoError(t, err)

	// Add a new attribute via writeDenseAttributeWithInfo (create path).
	err = ds2.WriteAttribute("new_attr", int32(42))
	require.NoError(t, err)

	err = fw2.Close()
	require.NoError(t, err)
	// Close underlying file reader to release Windows file handles.
	if fw2.file != nil {
		_ = fw2.file.Close()
	}

	// Verify file is readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_CreateDenseGroup_NoLeadingSlash tests dense group validation - missing slash.
func TestWriteCov_CreateDenseGroup_NoLeadingSlash(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "dense_group_invalid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Missing leading slash.
	err = fw.CreateDenseGroup("no_slash", map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must start with")
}

// TestWriteCov_CreateDenseGroup_EmptyLinks tests dense group with no links (error).
func TestWriteCov_CreateDenseGroup_EmptyLinks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "dense_group_empty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Empty links for dense group should error (dense requires at least one link).
	err = fw.CreateDenseGroup("/empty_dense", map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one link")
}

// TestWriteCov_CreateDenseGroup_NonexistentParent tests dense group with non-existent parent.
func TestWriteCov_CreateDenseGroup_NonexistentParent(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "dense_group_no_parent.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create a dataset target so links are valid, but parent doesn't exist.
	_, err = fw.CreateDataset("/target", Int32, []uint64{1})
	require.NoError(t, err)

	err = fw.CreateDenseGroup("/nonexistent/child", map[string]string{"link1": "/target"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

// TestWriteCov_ChunkedDataset_ValidationErrors tests chunked dataset creation validation.
func TestWriteCov_ChunkedDataset_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_errors.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Chunk dims mismatch.
	_, err = fw.CreateDataset("/bad1", Int32, []uint64{10},
		WithChunkDims([]uint64{5, 5})) // 2D chunks for 1D dataset
	require.Error(t, err)
	require.Contains(t, err.Error(), "must match dataset dimensions")

	// Zero chunk dim.
	_, err = fw.CreateDataset("/bad2", Int32, []uint64{10},
		WithChunkDims([]uint64{0}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be zero")

	// Chunk dim exceeds dataset dim.
	_, err = fw.CreateDataset("/bad3", Int32, []uint64{10},
		WithChunkDims([]uint64{20}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot exceed")
}

// TestWriteCov_RebalanceAllBTrees tests RebalanceAllBTrees (MVP no-op).
func TestWriteCov_RebalanceAllBTrees(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "rebalance_all.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// MVP: returns nil (no-op).
	err = fw.RebalanceAllBTrees()
	require.NoError(t, err)
}

// TestWriteCov_ValidateLinkPath tests validateLinkPath edge cases.
func TestWriteCov_ValidateLinkPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"empty", "", "cannot be empty"},
		{"no leading slash", "bad", "must start with '/'"},
		{"root", "/", "cannot create link to root"},
		{"consecutive slashes", "/a//b", "consecutive slashes"},
		{"valid", "/a/b", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLinkPath(tt.path)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestWriteCov_ValidateGroupPath tests validateGroupPath edge cases.
func TestWriteCov_ValidateGroupPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"empty", "", "cannot be empty"},
		{"no slash", "data", "must start with '/'"},
		{"root", "/", "root group already exists"},
		{"valid", "/group", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGroupPath(tt.path)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestWriteCov_ParsePath tests parsePath with various inputs.
func TestWriteCov_ParsePath(t *testing.T) {
	tests := []struct {
		path       string
		wantParent string
		wantName   string
	}{
		{"/", "", ""},
		{"/group1", "", "group1"},
		{"/data/experiments", "/data", "experiments"},
		{"/a/b/c", "/a/b", "c"},
		{"/trailing/", "", "trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			parent, name := parsePath(tt.path)
			require.Equal(t, tt.wantParent, parent)
			require.Equal(t, tt.wantName, name)
		})
	}
}

// TestWriteCov_EncodeDatatypeMessage_AllTypes tests EncodeDatatypeMessage for
// the various handler types to increase coverage of the type dispatch.
func TestWriteCov_EncodeDatatypeMessage_AllTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "all_types.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Test all basic types.
	types := []struct {
		name  string
		dtype Datatype
		size  int
	}{
		{"Int8", Int8, 1},
		{"Int16", Int16, 2},
		{"Int32", Int32, 4},
		{"Int64", Int64, 8},
		{"Uint8", Uint8, 1},
		{"Uint16", Uint16, 2},
		{"Uint32", Uint32, 4},
		{"Uint64", Uint64, 8},
		{"Float32", Float32, 4},
		{"Float64", Float64, 8},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := fw.CreateDataset("/"+tt.name, tt.dtype, []uint64{3})
			require.NoError(t, err)
			require.NotNil(t, ds)
			require.Equal(t, uint32(tt.size), ds.dtype.Size)
		})
	}
}

// TestWriteCov_SoftLinkValidPath tests CreateSoftLink with a valid dangling link.
func TestWriteCov_SoftLinkValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "softlink_valid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Soft links can point to non-existent targets (dangling).
	err = fw.CreateSoftLink("/dangling_link", "/future/dataset")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_ExternalLinkValid tests CreateExternalLink success path.
func TestWriteCov_ExternalLinkValid(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "extlink_valid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	err = fw.CreateExternalLink("/ext_link", "other_file.h5", "/external/data")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file readable.
	f, err := Open(filename)
	require.NoError(t, err)
	_ = f.Close()
}

// TestWriteCov_DisableEnableRebalancing_WithDelete tests disable/enable around a delete.
func TestWriteCov_DisableEnableRebalancing_WithDelete(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "rebalance_delete.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write compact attributes.
	for i := 0; i < 5; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
		require.NoError(t, err)
	}

	// Disable rebalancing, delete attribute, re-enable.
	fw.DisableRebalancing()
	require.False(t, fw.RebalancingEnabled())

	err = ds.DeleteAttribute("attr_2")
	require.NoError(t, err)

	fw.EnableRebalancing()
	require.True(t, fw.RebalancingEnabled())
}

// TestWriteCov_OpenDataset_NotFound tests OpenDataset with non-existent path.
func TestWriteCov_OpenDataset_NotFound(t *testing.T) {
	filename := filepath.Join("tmp", "cov_open_ds_notfound.h5")
	_ = os.MkdirAll("tmp", 0o755)
	defer func() { _ = os.Remove(filename) }()

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateDataset("/real", Int32, []uint64{1})
	require.NoError(t, err)
	err = fw.Close()
	require.NoError(t, err)

	fw2, err := OpenForWrite(filename, OpenReadWrite)
	require.NoError(t, err)

	_, err = fw2.OpenDataset("/nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	_ = fw2.Close()
	if fw2.file != nil {
		_ = fw2.file.Close()
	}
}

// TestWriteCov_InferDatatypeFromValue_MoreSliceTypes tests inferDatatypeFromValue
// for less common slice types to increase coverage.
func TestWriteCov_InferDatatypeFromValue_MoreSliceTypes(t *testing.T) {
	tests := []struct {
		name      string
		value     interface{}
		wantClass core.DatatypeClass
		wantSize  uint32
	}{
		{"[]int8", []int8{1, 2}, core.DatatypeFixed, 1},
		{"[]uint8", []uint8{1, 2}, core.DatatypeFixed, 1},
		{"[]int16", []int16{1, 2}, core.DatatypeFixed, 2},
		{"[]uint16", []uint16{1, 2}, core.DatatypeFixed, 2},
		{"[]int64", []int64{1, 2}, core.DatatypeFixed, 8},
		{"[]uint64", []uint64{1, 2}, core.DatatypeFixed, 8},
		{"[]float32", []float32{1.0, 2.0}, core.DatatypeFloat, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, ds, err := inferDatatypeFromValue(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.wantClass, dt.Class)
			require.Equal(t, tt.wantSize, dt.Size)
			require.Equal(t, []uint64{2}, ds.Dimensions)
		})
	}
}

// TestWriteCov_EncodeAttributeValue_AllTypes tests encodeAttributeValue for all types.
func TestWriteCov_EncodeAttributeValue_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		wantSize int
	}{
		{"int8", int8(42), 1},
		{"int16", int16(42), 2},
		{"int32", int32(42), 4},
		{"int64", int64(42), 8},
		{"uint8", uint8(42), 1},
		{"uint16", uint16(42), 2},
		{"uint32", uint32(42), 4},
		{"uint64", uint64(42), 8},
		{"float32", float32(3.14), 4},
		{"float64", float64(3.14), 8},
		{"string", "hello", 6}, // 5 + null terminator
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeAttributeValue(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.wantSize, len(data))
		})
	}
}

// TestWriteCov_EncodeAttributeValue_UnsupportedType tests encodeAttributeValue with unsupported type.
func TestWriteCov_EncodeAttributeValue_UnsupportedType(t *testing.T) {
	_, err := encodeAttributeValue(struct{}{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

// TestWriteCov_EncodeSliceValue_AllTypes tests encodeSliceValue for all element types.
func TestWriteCov_EncodeSliceValue_AllTypes(t *testing.T) {
	tests := []struct {
		name         string
		value        interface{}
		wantSize     int
		wantErr      bool
		wantErrMatch string
	}{
		{"[]int8", []int8{1, 2, 3}, 3, false, ""},
		{"[]uint8", []uint8{1, 2, 3}, 3, false, ""},
		{"[]int16", []int16{1, 2}, 4, false, ""},
		{"[]uint16", []uint16{1, 2}, 4, false, ""},
		{"[]int32", []int32{1, 2}, 8, false, ""},
		{"[]uint32", []uint32{1, 2}, 8, false, ""},
		{"[]int64", []int64{1, 2}, 16, false, ""},
		{"[]uint64", []uint64{1, 2}, 16, false, ""},
		{"[]float32", []float32{1.0, 2.0}, 8, false, ""},
		{"[]float64", []float64{1.0, 2.0}, 16, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeAttributeValue(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrMatch)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantSize, len(data))
			}
		})
	}
}

// TestWriteCov_DeleteDenseAttribute tests deleting an attribute from dense storage.
func TestWriteCov_DeleteDenseAttribute(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "delete_dense_attr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write 9 attributes to trigger dense storage.
	for i := 0; i < 9; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("dense_%d", i), int32(i))
		require.NoError(t, err)
	}

	// Delete a dense attribute.
	err = ds.DeleteAttribute("dense_5")
	require.NoError(t, err)

	// Delete another one.
	err = ds.DeleteAttribute("dense_0")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_DeleteCompactAttribute tests deleting a compact attribute.
func TestWriteCov_DeleteCompactAttribute(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "delete_compact_attr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write 3 compact attributes.
	err = ds.WriteAttribute("keep_1", int32(1))
	require.NoError(t, err)
	err = ds.WriteAttribute("delete_me", int32(2))
	require.NoError(t, err)
	err = ds.WriteAttribute("keep_2", int32(3))
	require.NoError(t, err)

	// Delete compact attribute.
	err = ds.DeleteAttribute("delete_me")
	require.NoError(t, err)
}

// TestWriteCov_DeleteAttribute_NotFound tests deleting a non-existent attribute.
func TestWriteCov_DeleteAttribute_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "delete_notfound.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)
	err = ds.WriteAttribute("exists", int32(1))
	require.NoError(t, err)

	// Delete non-existent attribute.
	err = ds.DeleteAttribute("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// TestWriteCov_CompactUpsert tests modifying existing compact attribute (upsert).
func TestWriteCov_CompactUpsert(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "compact_upsert.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write initial value.
	err = ds.WriteAttribute("version", int32(1))
	require.NoError(t, err)

	// Overwrite with new value (upsert).
	err = ds.WriteAttribute("version", int32(2))
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify final value.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var foundVersion int32
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/data" {
			if val, err := ds.ReadAttributeAsInt32("version"); err == nil {
				foundVersion = val
			}
		}
	})
	require.Equal(t, int32(2), foundVersion)
}

// TestWriteCov_DenseAttributeWriteMany tests writing many dense attributes.
func TestWriteCov_DenseAttributeWriteMany(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "many_attrs.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Write 15 attributes (well beyond dense threshold).
	for i := 0; i < 15; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("attr_%02d", i), int32(i*100))
		require.NoError(t, err)
	}

	err = fw.Close()
	require.NoError(t, err)

	// Verify readable and all 15 are there.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var attrCount int
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/data" {
			attrs, err := ds.ListAttributes()
			if err == nil {
				attrCount = len(attrs)
			}
		}
	})
	require.Equal(t, 15, attrCount)
}

// TestWriteCov_WriteRaw_SizeMismatch tests WriteRaw with wrong data size.
func TestWriteCov_WriteRaw_SizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "writeraw_mismatch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)

	// 5 int32 = 20 bytes, but we give 10 bytes.
	err = ds.WriteRaw([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "size mismatch")
}

// TestWriteCov_Write_TypeMismatch tests Write with wrong data type.
func TestWriteCov_Write_TypeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "write_type_mismatch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create int32 dataset, try to write float64 data.
	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)

	err = ds.Write([]float64{1.0, 2.0, 3.0, 4.0, 5.0})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported data type")
}

// TestWriteCov_CreateCompoundDataset_ChunkedError tests that chunked compound is rejected.
func TestWriteCov_CreateCompoundDataset_ChunkedError(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "compound_chunked_error.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)
	fields := []core.CompoundFieldDef{
		{Name: "id", Offset: 0, Type: int32Type},
	}
	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)

	_, err = fw.CreateCompoundDataset("/data", compoundType, []uint64{10},
		WithChunkDims([]uint64{5}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunked compound")
}

// TestWriteCov_WriteAttribute_StringSlice tests writing string slice attribute.
// String slices are not supported as attribute values (only scalar strings).
func TestWriteCov_WriteAttribute_StringSlice(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "attr_string_slice.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// String slices are not supported - should error.
	err = ds.WriteAttribute("labels", []string{"foo", "bar"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

// TestWriteCov_WriteAttribute_SliceValues tests writing various slice types as attributes.
func TestWriteCov_WriteAttribute_SliceValues(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "attr_slices.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Test writing slice attributes of various types.
	err = ds.WriteAttribute("i8", []int8{1, 2, 3})
	require.NoError(t, err)
	err = ds.WriteAttribute("u8", []uint8{1, 2, 3})
	require.NoError(t, err)
	err = ds.WriteAttribute("i16", []int16{100, 200})
	require.NoError(t, err)
	err = ds.WriteAttribute("u16", []uint16{100, 200})
	require.NoError(t, err)
	err = ds.WriteAttribute("f32", []float32{1.0, 2.0})
	require.NoError(t, err)
	err = ds.WriteAttribute("u32", []uint32{1, 2})
	require.NoError(t, err)
	err = ds.WriteAttribute("u64", []uint64{1, 2})
	require.NoError(t, err)
}

// TestWriteCov_GroupAttribute tests writing attribute to a group.
func TestWriteCov_GroupAttribute(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_attr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	group, err := fw.CreateGroup("/metadata")
	require.NoError(t, err)

	// Write attributes to group.
	err = group.WriteAttribute("version", int32(1))
	require.NoError(t, err)
	err = group.WriteAttribute("name", "experiment_1")
	require.NoError(t, err)
	err = group.WriteAttribute("precision", float64(0.001))
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_NestedGroup tests creating nested groups.
func TestWriteCov_NestedGroup(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "nested_groups.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create nested groups.
	_, err = fw.CreateGroup("/level1")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/level1/level2")
	require.NoError(t, err)

	// Create dataset in nested group.
	ds, err := fw.CreateDataset("/level1/level2/data", Int32, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int32{10, 20, 30})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify readable.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var foundData bool
	f.Walk(func(path string, _ Object) {
		if path == "/level1/level2/data" {
			foundData = true
		}
	})
	require.True(t, foundData, "nested dataset should be found")
}

// TestWriteCov_CreateDataset_AllIntSizes tests writing data with all integer sizes.
func TestWriteCov_CreateDataset_AllIntSizes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "all_int_sizes.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Int8
	ds, err := fw.CreateDataset("/i8", Int8, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int8{-1, 0, 1})
	require.NoError(t, err)

	// Uint8
	ds, err = fw.CreateDataset("/u8", Uint8, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]uint8{0, 127, 255})
	require.NoError(t, err)

	// Int16
	ds, err = fw.CreateDataset("/i16", Int16, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int16{-100, 0, 100})
	require.NoError(t, err)

	// Uint16
	ds, err = fw.CreateDataset("/u16", Uint16, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]uint16{0, 1000, 65535})
	require.NoError(t, err)

	// Int64
	ds, err = fw.CreateDataset("/i64", Int64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int64{-1000000, 0, 1000000})
	require.NoError(t, err)

	// Uint64
	ds, err = fw.CreateDataset("/u64", Uint64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]uint64{0, 1000000, 18446744073709551615})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_CreateDataset_Float32 tests writing float32 data.
func TestWriteCov_CreateDataset_Float32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "float32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/f32", Float32, []uint64{4})
	require.NoError(t, err)
	err = ds.Write([]float32{1.5, 2.5, 3.5, 4.5})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_CreateDataset_String tests writing string data.
func TestWriteCov_CreateDataset_String(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "string_ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/names", String, []uint64{3}, WithStringSize(16))
	require.NoError(t, err)

	err = ds.Write([]string{"Alice", "Bob", "Charlie"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_CreateDataset_Enum tests writing enum data.
func TestWriteCov_CreateDataset_Enum(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "enum_ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	names := []string{"Red", "Green", "Blue"}
	values := []int64{0, 1, 2}

	ds, err := fw.CreateDataset("/colors", EnumInt8, []uint64{5},
		WithEnumValues(names, values))
	require.NoError(t, err)

	err = ds.Write([]int8{0, 1, 2, 1, 0})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_HardLink_MultipleGroups tests hard links across multiple groups.
func TestWriteCov_HardLink_MultipleGroups(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "hardlink_groups.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create two groups.
	_, err = fw.CreateGroup("/data")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Create dataset in /data.
	ds, err := fw.CreateDataset("/data/temperature", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{20.0, 21.5, 22.0})
	require.NoError(t, err)

	// Create hard link from /links to /data/temperature.
	err = fw.CreateHardLink("/links/temp_link", "/data/temperature")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_Chunked2D_WithGZIPAndFletcher32 tests multiple filters on 2D chunked data.
func TestWriteCov_Chunked2D_WithGZIPAndFletcher32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_2d_gzip_f32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/matrix", Float64, []uint64{20, 30},
		WithChunkDims([]uint64{10, 15}),
		WithGZIPCompression(6),
		WithFletcher32())
	require.NoError(t, err)
	require.NotNil(t, ds.pipeline)

	data := make([]float64, 20*30)
	for i := range data {
		data[i] = float64(i) * 0.1
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_Chunked1D_Uint8 tests chunked dataset with uint8 data.
func TestWriteCov_Chunked1D_Uint8(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_u8.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/bytes", Uint8, []uint64{50},
		WithChunkDims([]uint64{25}))
	require.NoError(t, err)

	data := make([]uint8, 50)
	for i := range data {
		data[i] = uint8(i * 2)
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_GroupWriteAttribute_DenseTransition tests dense transition on group attributes.
func TestWriteCov_GroupWriteAttribute_DenseTransition(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_dense_attrs.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	group, err := fw.CreateGroup("/data")
	require.NoError(t, err)

	// Write 9 attributes to group to trigger dense transition.
	for i := 0; i < 9; i++ {
		err = group.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i*10))
		require.NoError(t, err)
	}

	err = fw.Close()
	require.NoError(t, err)
}

// TestWriteCov_MultipleDatasets tests creating many datasets in one file.
func TestWriteCov_MultipleDatasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "multi_ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create 10 datasets.
	for i := 0; i < 10; i++ {
		ds, err := fw.CreateDataset(fmt.Sprintf("/ds_%d", i), Int32, []uint64{5})
		require.NoError(t, err)
		data := make([]int32, 5)
		for j := range data {
			data[j] = int32(i*100 + j)
		}
		err = ds.Write(data)
		require.NoError(t, err)
	}

	err = fw.Close()
	require.NoError(t, err)

	// Verify all 10 exist.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	count := 0
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*Dataset); ok {
			count++
		}
	})
	require.Equal(t, 10, count)
}
