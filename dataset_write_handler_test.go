package hdf5

import (
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatatypeRegistry_BasicTypes tests registry lookup for basic types.
func TestDatatypeRegistry_BasicTypes(t *testing.T) {
	tests := []struct {
		name      string
		dtype     Datatype
		wantClass core.DatatypeClass
		wantSize  uint32
	}{
		{"Int8", Int8, core.DatatypeFixed, 1},
		{"Int16", Int16, core.DatatypeFixed, 2},
		{"Int32", Int32, core.DatatypeFixed, 4},
		{"Int64", Int64, core.DatatypeFixed, 8},
		{"Uint8", Uint8, core.DatatypeFixed, 1},
		{"Uint16", Uint16, core.DatatypeFixed, 2},
		{"Uint32", Uint32, core.DatatypeFixed, 4},
		{"Uint64", Uint64, core.DatatypeFixed, 8},
		{"Float32", Float32, core.DatatypeFloat, 4},
		{"Float64", Float64, core.DatatypeFloat, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check registry contains handler
			handler, ok := datatypeRegistry[tt.dtype]
			require.True(t, ok, "registry should contain handler for %s", tt.name)
			require.NotNil(t, handler)

			// Check GetInfo returns correct metadata
			config := &datasetConfig{}
			info, err := handler.GetInfo(config)
			require.NoError(t, err)
			assert.Equal(t, tt.wantClass, info.class)
			assert.Equal(t, tt.wantSize, info.size)
		})
	}
}

// TestDatatypeRegistry_AllTypesRegistered verifies all Datatype constants are registered.
func TestDatatypeRegistry_AllTypesRegistered(t *testing.T) {
	// All basic types
	basicTypes := []Datatype{
		Int8, Int16, Int32, Int64,
		Uint8, Uint16, Uint32, Uint64,
		Float32, Float64,
		String,
	}

	// All array types
	arrayTypes := []Datatype{
		ArrayInt8, ArrayInt16, ArrayInt32, ArrayInt64,
		ArrayUint8, ArrayUint16, ArrayUint32, ArrayUint64,
		ArrayFloat32, ArrayFloat64,
	}

	// All enum types
	enumTypes := []Datatype{
		EnumInt8, EnumInt16, EnumInt32, EnumInt64,
		EnumUint8, EnumUint16, EnumUint32, EnumUint64,
	}

	// Reference types
	refTypes := []Datatype{ObjectReference, RegionReference}

	// Opaque type
	opaqueTypes := []Datatype{Opaque}

	// Combine all types (fix gocritic: append to same slice)
	allTypes := make([]Datatype, 0, len(basicTypes)+len(arrayTypes)+len(enumTypes)+len(refTypes)+len(opaqueTypes))
	allTypes = append(allTypes, basicTypes...)
	allTypes = append(allTypes, arrayTypes...)
	allTypes = append(allTypes, enumTypes...)
	allTypes = append(allTypes, refTypes...)
	allTypes = append(allTypes, opaqueTypes...)

	for _, dt := range allTypes {
		handler, ok := datatypeRegistry[dt]
		assert.True(t, ok, "datatype %d should be registered", dt)
		assert.NotNil(t, handler, "handler for datatype %d should not be nil", dt)
	}
}

// TestBasicTypeHandler tests the basicTypeHandler implementation.
func TestBasicTypeHandler(t *testing.T) {
	handler := &basicTypeHandler{
		class:         core.DatatypeFixed,
		size:          4,
		classBitField: 0x08,
	}

	config := &datasetConfig{}
	info, err := handler.GetInfo(config)
	require.NoError(t, err)
	assert.Equal(t, core.DatatypeFixed, info.class)
	assert.Equal(t, uint32(4), info.size)
	assert.Equal(t, uint32(0x08), info.classBitField)

	// Test encoding
	data, err := handler.EncodeDatatypeMessage(info)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

// TestStringTypeHandler tests the stringTypeHandler implementation.
func TestStringTypeHandler(t *testing.T) {
	handler := &stringTypeHandler{}

	t.Run("valid size", func(t *testing.T) {
		config := &datasetConfig{stringSize: 32}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeString, info.class)
		assert.Equal(t, uint32(32), info.size)

		// Test encoding
		data, err := handler.EncodeDatatypeMessage(info)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("zero size - error", func(t *testing.T) {
		config := &datasetConfig{stringSize: 0}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "string datatype requires size")
	})
}

// TestArrayTypeHandler tests the arrayTypeHandler implementation.
func TestArrayTypeHandler(t *testing.T) {
	handler := &arrayTypeHandler{baseType: Int32}

	t.Run("valid dimensions", func(t *testing.T) {
		config := &datasetConfig{arrayDims: []uint64{3, 4}}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeArray, info.class)
		assert.Equal(t, uint32(48), info.size) // 3 * 4 * 4 bytes
		assert.NotNil(t, info.baseType)
		assert.Equal(t, []uint64{3, 4}, info.arrayDims)

		// Test encoding
		data, err := handler.EncodeDatatypeMessage(info)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("no dimensions - error", func(t *testing.T) {
		config := &datasetConfig{arrayDims: nil}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "array datatype requires dimensions")
	})

	t.Run("invalid base type - error", func(t *testing.T) {
		badHandler := &arrayTypeHandler{baseType: Datatype(9999)}
		config := &datasetConfig{arrayDims: []uint64{3}}
		_, err := badHandler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid array base type")
	})
}

// TestEnumTypeHandler tests the enumTypeHandler implementation.
func TestEnumTypeHandler(t *testing.T) {
	handler := &enumTypeHandler{baseType: Int32}

	t.Run("valid enum", func(t *testing.T) {
		config := &datasetConfig{
			enumNames:  []string{"Red", "Green", "Blue"},
			enumValues: []int64{0, 1, 2},
		}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeEnum, info.class)
		assert.Equal(t, uint32(4), info.size) // Int32 = 4 bytes
		assert.NotNil(t, info.baseType)
		assert.Equal(t, []string{"Red", "Green", "Blue"}, info.enumNames)
		assert.Equal(t, []int64{0, 1, 2}, info.enumValues)

		// Test encoding
		data, err := handler.EncodeDatatypeMessage(info)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("no names - error", func(t *testing.T) {
		config := &datasetConfig{
			enumNames:  nil,
			enumValues: []int64{0, 1},
		}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "enum datatype requires names and values")
	})

	t.Run("mismatched lengths - error", func(t *testing.T) {
		config := &datasetConfig{
			enumNames:  []string{"A", "B"},
			enumValues: []int64{0, 1, 2},
		}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must have same length")
	})

	t.Run("invalid base type - error", func(t *testing.T) {
		badHandler := &enumTypeHandler{baseType: Datatype(9999)}
		config := &datasetConfig{
			enumNames:  []string{"A"},
			enumValues: []int64{0},
		}
		_, err := badHandler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid enum base type")
	})
}

// TestReferenceTypeHandler tests the referenceTypeHandler implementation.
func TestReferenceTypeHandler(t *testing.T) {
	t.Run("object reference", func(t *testing.T) {
		handler := &referenceTypeHandler{size: 8, classBitField: 0x00}
		config := &datasetConfig{}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeReference, info.class)
		assert.Equal(t, uint32(8), info.size)
		assert.Equal(t, uint32(0x00), info.classBitField)

		// Test encoding
		data, err := handler.EncodeDatatypeMessage(info)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("region reference", func(t *testing.T) {
		handler := &referenceTypeHandler{size: 12, classBitField: 0x01}
		config := &datasetConfig{}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeReference, info.class)
		assert.Equal(t, uint32(12), info.size)
		assert.Equal(t, uint32(0x01), info.classBitField)
	})
}

// TestOpaqueTypeHandler tests the opaqueTypeHandler implementation.
func TestOpaqueTypeHandler(t *testing.T) {
	handler := &opaqueTypeHandler{}

	t.Run("valid opaque", func(t *testing.T) {
		config := &datasetConfig{
			opaqueTag:  "JPEG image",
			opaqueSize: 1024,
		}
		info, err := handler.GetInfo(config)
		require.NoError(t, err)
		assert.Equal(t, core.DatatypeOpaque, info.class)
		assert.Equal(t, uint32(1024), info.size)
		assert.Equal(t, "JPEG image", info.opaqueTag)

		// Test encoding
		data, err := handler.EncodeDatatypeMessage(info)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("no tag - error", func(t *testing.T) {
		config := &datasetConfig{
			opaqueTag:  "",
			opaqueSize: 1024,
		}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "opaque datatype requires tag and size")
	})

	t.Run("zero size - error", func(t *testing.T) {
		config := &datasetConfig{
			opaqueTag:  "JPEG",
			opaqueSize: 0,
		}
		_, err := handler.GetInfo(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "opaque datatype requires tag and size")
	})
}

// TestGetDatatypeInfo_RegistryPattern tests the simplified getDatatypeInfo using registry.
func TestGetDatatypeInfo_RegistryPattern(t *testing.T) {
	tests := []struct {
		name      string
		dtype     Datatype
		config    *datasetConfig
		wantClass core.DatatypeClass
		wantErr   bool
	}{
		{
			name:      "Int32",
			dtype:     Int32,
			config:    &datasetConfig{},
			wantClass: core.DatatypeFixed,
			wantErr:   false,
		},
		{
			name:      "Float64",
			dtype:     Float64,
			config:    &datasetConfig{},
			wantClass: core.DatatypeFloat,
			wantErr:   false,
		},
		{
			name:      "String with size",
			dtype:     String,
			config:    &datasetConfig{stringSize: 32},
			wantClass: core.DatatypeString,
			wantErr:   false,
		},
		{
			name:      "ArrayInt32",
			dtype:     ArrayInt32,
			config:    &datasetConfig{arrayDims: []uint64{3}},
			wantClass: core.DatatypeArray,
			wantErr:   false,
		},
		{
			name:  "EnumInt8",
			dtype: EnumInt8,
			config: &datasetConfig{
				enumNames:  []string{"A", "B"},
				enumValues: []int64{0, 1},
			},
			wantClass: core.DatatypeEnum,
			wantErr:   false,
		},
		{
			name:      "ObjectReference",
			dtype:     ObjectReference,
			config:    &datasetConfig{},
			wantClass: core.DatatypeReference,
			wantErr:   false,
		},
		{
			name:      "Opaque",
			dtype:     Opaque,
			config:    &datasetConfig{opaqueTag: "JPEG", opaqueSize: 1024},
			wantClass: core.DatatypeOpaque,
			wantErr:   false,
		},
		{
			name:    "unsupported datatype",
			dtype:   Datatype(9999),
			config:  &datasetConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := getDatatypeInfo(tt.dtype, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported datatype")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantClass, info.class)
			}
		})
	}
}
