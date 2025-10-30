// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package writer

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

func TestDenseAttributeWriter_Creation(t *testing.T) {
	daw := NewDenseAttributeWriter(0x1000)

	require.NotNil(t, daw)
	require.Equal(t, uint64(0x1000), daw.objectAddr)
	require.NotNil(t, daw.fractalHeap)
	require.NotNil(t, daw.btree)
	require.NotNil(t, daw.attrInfo)
	require.Empty(t, daw.attributes)
	require.Equal(t, uint8(0), daw.attrInfo.Version)
	require.Equal(t, uint8(0), daw.attrInfo.Flags) // No creation order
}

func TestDenseAttributeWriter_AddSingleAttribute(t *testing.T) {
	daw := NewDenseAttributeWriter(0x2000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	attr := &core.Attribute{
		Name: "temperature",
		Datatype: &core.DatatypeMessage{
			Class: core.DatatypeFloat,
			Size:  8,
		},
		Dataspace: &core.DataspaceMessage{
			Dimensions: []uint64{1}, // Scalar
		},
		Data: []byte{0, 0, 0, 0, 0, 0, 0x59, 0x40}, // 100.0 in float64
	}

	err := daw.AddAttribute(attr, sb)
	require.NoError(t, err)
	require.Len(t, daw.attributes, 1)
	require.Contains(t, daw.attributes, "temperature")
}

func TestDenseAttributeWriter_AddMultipleAttributes(t *testing.T) {
	daw := NewDenseAttributeWriter(0x3000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Add 20 attributes
	for i := 0; i < 20; i++ {
		attr := &core.Attribute{
			Name: "attr_" + string(rune('A'+i)),
			Datatype: &core.DatatypeMessage{
				Class: core.DatatypeFixed,
				Size:  4,
			},
			Dataspace: &core.DataspaceMessage{
				Dimensions: []uint64{1},
			},
			Data: []byte{byte(i), 0, 0, 0},
		}

		err := daw.AddAttribute(attr, sb)
		require.NoError(t, err)
	}

	require.Len(t, daw.attributes, 20)
}

func TestDenseAttributeWriter_LargeScale(t *testing.T) {
	daw := NewDenseAttributeWriter(0x4000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Add 100+ attributes to test scalability
	const count = 150
	for i := 0; i < count; i++ {
		attr := &core.Attribute{
			Name: makeAttrName(i),
			Datatype: &core.DatatypeMessage{
				Class: core.DatatypeFixed,
				Size:  8,
			},
			Dataspace: &core.DataspaceMessage{
				Dimensions: []uint64{1},
			},
			Data: makeInt64Bytes(int64(i)),
		}

		err := daw.AddAttribute(attr, sb)
		require.NoError(t, err, "failed to add attribute %d", i)
	}

	require.Len(t, daw.attributes, count)
}

func TestDenseAttributeWriter_UTF8Names(t *testing.T) {
	daw := NewDenseAttributeWriter(0x5000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	testNames := []string{
		"температура", // Russian
		"温度",          // Chinese
		"température", // French
		"Temperatur",  // German
		"🌡️_sensor",   // Emoji
		"مقياس_حرارة", // Arabic
	}

	for _, name := range testNames {
		attr := &core.Attribute{
			Name: name,
			Datatype: &core.DatatypeMessage{
				Class: core.DatatypeFloat,
				Size:  4,
			},
			Dataspace: &core.DataspaceMessage{
				Dimensions: []uint64{1},
			},
			Data: []byte{0, 0, 0x48, 0x42}, // 50.0
		}

		err := daw.AddAttribute(attr, sb)
		require.NoError(t, err, "failed to add attribute %q", name)
	}

	require.Len(t, daw.attributes, len(testNames))
}

func TestDenseAttributeWriter_DuplicateNameError(t *testing.T) {
	daw := NewDenseAttributeWriter(0x6000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	attr1 := &core.Attribute{
		Name: "duplicate",
		Datatype: &core.DatatypeMessage{
			Class: core.DatatypeFixed,
			Size:  4,
		},
		Dataspace: &core.DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{1, 0, 0, 0},
	}

	// First add should succeed
	err := daw.AddAttribute(attr1, sb)
	require.NoError(t, err)

	// Second add with same name should fail
	attr2 := &core.Attribute{
		Name: "duplicate",
		Datatype: &core.DatatypeMessage{
			Class: core.DatatypeFixed,
			Size:  4,
		},
		Dataspace: &core.DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{2, 0, 0, 0},
	}

	err = daw.AddAttribute(attr2, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestDenseAttributeWriter_EmptyNameError(t *testing.T) {
	daw := NewDenseAttributeWriter(0x7000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	attr := &core.Attribute{
		Name: "", // Empty name
		Datatype: &core.DatatypeMessage{
			Class: core.DatatypeFixed,
			Size:  4,
		},
		Dataspace: &core.DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{1, 0, 0, 0},
	}

	err := daw.AddAttribute(attr, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")
}

func TestDenseAttributeWriter_NilAttributeError(t *testing.T) {
	daw := NewDenseAttributeWriter(0x8000)
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	err := daw.AddAttribute(nil, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is nil")
}

// Note: WriteToFile tests are in integration tests (dense_attribute_integration_test.go)
// because they require full FileWriter setup with actual file I/O.

// Benchmark tests.
func BenchmarkDenseAttributeWriter_Add(b *testing.B) {
	sb := &core.Superblock{
		OffsetSize: 8,
		Endianness: binary.LittleEndian,
	}

	attr := &core.Attribute{
		Name: "benchmark_attr",
		Datatype: &core.DatatypeMessage{
			Class: core.DatatypeFloat,
			Size:  8,
		},
		Dataspace: &core.DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{0, 0, 0, 0, 0, 0, 0x59, 0x40},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create new writer for each iteration to avoid duplicates
		daw := NewDenseAttributeWriter(0x1000)
		_ = daw.AddAttribute(attr, sb)
	}
}

// BenchmarkDenseAttributeWriter_Write benchmark removed - requires full FileWriter setup

// Helper functions

func makeAttrName(i int) string {
	return "attribute_" + string(rune('0'+(i%10))) + "_" + string(rune('A'+(i/10)%26))
}

func makeInt64Bytes(val int64) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(val))
	return buf
}
