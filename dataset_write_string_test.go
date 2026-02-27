package hdf5

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteStringDataset_FixedLength tests writing fixed-length string dataset with round-trip verification.
func TestWriteStringDataset_FixedLength(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_string_fixed.h5")

	// Write phase
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/names", String, []uint64{3}, WithStringSize(10))
	require.NoError(t, err)
	require.NotNil(t, ds)

	err = ds.Write([]string{"hello", "world", "test"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read phase - verify round-trip
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Len(t, children, 1)

	ds2, ok := children[0].(*Dataset)
	require.True(t, ok, "child should be a Dataset")

	strings, err := ds2.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 3)

	assert.Equal(t, "hello", strings[0])
	assert.Equal(t, "world", strings[1])
	assert.Equal(t, "test", strings[2])
}

// TestWriteStringDataset_Truncation tests that strings longer than StringSize are truncated.
func TestWriteStringDataset_Truncation(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_string_truncation.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// StringSize=5 means strings longer than 5 bytes are truncated
	ds, err := fw.CreateDataset("/truncated", String, []uint64{2}, WithStringSize(5))
	require.NoError(t, err)

	err = ds.Write([]string{"hello world", "truncated string"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back and verify truncation
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/truncated" {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	require.NotNil(t, found, "truncated dataset should be found")

	strings, err := found.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 2)

	// Strings should be truncated to 5 characters
	assert.Equal(t, "hello", strings[0])
	assert.Equal(t, "trunc", strings[1])
}

// TestWriteStringDataset_EmptyStrings tests writing empty strings.
func TestWriteStringDataset_EmptyStrings(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_string_empty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/empty_strings", String, []uint64{3}, WithStringSize(8))
	require.NoError(t, err)

	err = ds.Write([]string{"", "", ""})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back and verify
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/empty_strings" {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	require.NotNil(t, found)

	strings, err := found.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 3)

	for i, s := range strings {
		assert.Equal(t, "", s, "string %d should be empty", i)
	}
}

// TestWriteStringDataset_SingleChar tests writing strings with StringSize=1.
func TestWriteStringDataset_SingleChar(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_string_single_char.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/single_chars", String, []uint64{4}, WithStringSize(1))
	require.NoError(t, err)

	err = ds.Write([]string{"a", "b", "c", "d"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/single_chars" {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	require.NotNil(t, found)

	strings, err := found.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 4)

	assert.Equal(t, "a", strings[0])
	assert.Equal(t, "b", strings[1])
	assert.Equal(t, "c", strings[2])
	assert.Equal(t, "d", strings[3])
}

// TestWriteRaw_ChunkedFloat64 tests WriteRaw with a chunked float64 dataset.
// Uses a single chunk to exercise the chunked code path without B-tree ordering issues.
func TestWriteRaw_ChunkedFloat64(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_writeraw_chunked.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create chunked dataset: 10 float64 values, single chunk of 10
	// This exercises the isChunked=true path in WriteRaw without multi-chunk ordering issues.
	ds, err := fw.CreateDataset("/chunked_data", Float64, []uint64{10}, WithChunkDims([]uint64{10}))
	require.NoError(t, err)

	// Build raw bytes for 10 float64 values
	expected := []float64{1.1, 2.2, 3.3, 4.4, 5.5, 6.6, 7.7, 8.8, 9.9, 10.0}
	rawBytes := make([]byte, 10*8)
	for i, val := range expected {
		binary.LittleEndian.PutUint64(rawBytes[i*8:], math.Float64bits(val))
	}

	err = ds.WriteRaw(rawBytes)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/chunked_data" {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	require.NotNil(t, found, "chunked_data dataset should be found")

	data, err := found.Read()
	require.NoError(t, err)
	require.Len(t, data, 10)

	for i, val := range expected {
		assert.InDelta(t, val, data[i], 1e-10, "value at index %d", i)
	}
}

// TestWriteRaw_ContiguousLayout tests WriteRaw with contiguous (non-chunked) dataset.
func TestWriteRaw_ContiguousLayout(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_writeraw_contiguous.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/raw_data", Float64, []uint64{5})
	require.NoError(t, err)

	// Build raw bytes for 5 float64 values
	values := []float64{1.1, 2.2, 3.3, 4.4, 5.5}
	rawBytes := make([]byte, 5*8)
	for i, v := range values {
		binary.LittleEndian.PutUint64(rawBytes[i*8:], math.Float64bits(v))
	}

	err = ds.WriteRaw(rawBytes)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/raw_data" {
			if ds, ok := obj.(*Dataset); ok {
				found = ds
			}
		}
	})
	require.NotNil(t, found)

	data, err := found.Read()
	require.NoError(t, err)
	require.Equal(t, values, data)
}

// TestWriteRaw_SizeMismatch tests that WriteRaw rejects data with wrong size.
func TestWriteRaw_SizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_writeraw_mismatch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Dataset expects 5 * 8 = 40 bytes
	ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
	require.NoError(t, err)

	// Try writing wrong number of bytes
	wrongBytes := make([]byte, 24) // Only 24 bytes instead of 40
	err = ds.WriteRaw(wrongBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")
}

// TestCreateCompoundDataset_EmptyMembers tests that nil compound type is rejected.
func TestCreateCompoundDataset_EmptyMembers(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_nil.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Pass nil compound type - should error
	_, err = fw.CreateCompoundDataset("/bad", nil, []uint64{2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestCreateCompoundDataset_WrongClass tests that non-compound datatype is rejected.
func TestCreateCompoundDataset_WrongClass(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_wrongclass.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create a non-compound datatype (fixed-point)
	intType, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)

	_, err = fw.CreateCompoundDataset("/bad", intType, []uint64{2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compound")
}

// TestCreateCompoundDataset_InvalidDimensions tests that invalid dims are rejected.
func TestCreateCompoundDataset_InvalidDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_baddims.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create a valid compound type
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "id", Offset: 0, Type: int32Type},
	}
	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)

	tests := []struct {
		name    string
		dims    []uint64
		wantErr string
	}{
		{
			name:    "empty dimensions",
			dims:    []uint64{},
			wantErr: "cannot be empty",
		},
		{
			name:    "zero dimension",
			dims:    []uint64{5, 0},
			wantErr: "cannot be 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fw.CreateCompoundDataset("/compound_"+tt.name, compoundType, tt.dims)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestCreateCompoundDataset_InvalidName tests that invalid dataset names are rejected.
func TestCreateCompoundDataset_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_compound_badname.h5")

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

	// Empty name
	_, err = fw.CreateCompoundDataset("", compoundType, []uint64{2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")

	// Name without leading slash
	_, err = fw.CreateCompoundDataset("noSlash", compoundType, []uint64{2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must start with '/'")
}

// TestEncodeStringData tests the encodeStringData function directly.
func TestEncodeStringData(t *testing.T) {
	tests := []struct {
		name         string
		data         interface{}
		elemSize     uint32
		expectedSize uint64
		wantErr      bool
		errContains  string
	}{
		{
			name:         "normal strings",
			data:         []string{"abc", "def"},
			elemSize:     4,
			expectedSize: 8,
			wantErr:      false,
		},
		{
			name:         "truncated strings",
			data:         []string{"hello world"},
			elemSize:     5,
			expectedSize: 5,
			wantErr:      false,
		},
		{
			name:         "wrong type",
			data:         []int32{1, 2, 3},
			elemSize:     4,
			expectedSize: 12,
			wantErr:      true,
			errContains:  "expected []string",
		},
		{
			name:         "size mismatch",
			data:         []string{"a", "b", "c"},
			elemSize:     4,
			expectedSize: 4, // Only 4 bytes but 3 strings * 4 = 12
			wantErr:      true,
			errContains:  "size mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := encodeStringData(tt.data, tt.elemSize, tt.expectedSize)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int(tt.expectedSize), len(buf))
		})
	}
}

// TestEncodeStringData_NullPadding verifies that short strings are null-padded.
func TestEncodeStringData_NullPadding(t *testing.T) {
	buf, err := encodeStringData([]string{"hi"}, 5, 5)
	require.NoError(t, err)
	require.Len(t, buf, 5)

	// "hi" = 0x68, 0x69, then 3 null bytes
	assert.Equal(t, byte('h'), buf[0])
	assert.Equal(t, byte('i'), buf[1])
	assert.Equal(t, byte(0), buf[2])
	assert.Equal(t, byte(0), buf[3])
	assert.Equal(t, byte(0), buf[4])
}

// TestWithStringSize tests the WithStringSize option function.
func TestWithStringSize(t *testing.T) {
	cfg := &datasetConfig{}
	opt := WithStringSize(42)
	opt(cfg)
	assert.Equal(t, uint32(42), cfg.stringSize)
}
