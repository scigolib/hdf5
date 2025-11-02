// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package core

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// ============================================================================
// FindCompactAttribute Tests
// ============================================================================

func TestFindCompactAttribute(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
	}

	// Create test attribute with proper DatatypeMessage structure
	testAttr := &Attribute{
		Name: "test_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08, // Signed integer
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x01, 0x00, 0x00, 0x00}, // int32(1)
	}

	attrData, err := EncodeAttributeFromStruct(testAttr, sb)
	require.NoError(t, err)

	tests := []struct {
		name         string
		oh           *ObjectHeader
		searchName   string
		wantFound    bool
		wantIndex    int
		wantAttrName string
	}{
		{
			name: "attribute found",
			oh: &ObjectHeader{
				Version: 2,
				Messages: []*HeaderMessage{
					{Type: MsgAttribute, Data: attrData},
				},
			},
			searchName:   "test_attr",
			wantFound:    true,
			wantIndex:    0,
			wantAttrName: "test_attr",
		},
		{
			name: "attribute not found",
			oh: &ObjectHeader{
				Version: 2,
				Messages: []*HeaderMessage{
					{Type: MsgAttribute, Data: attrData},
				},
			},
			searchName: "nonexistent",
			wantFound:  false,
			wantIndex:  -1,
		},
		{
			name: "multiple attributes, find second",
			oh: &ObjectHeader{
				Version: 2,
				Messages: []*HeaderMessage{
					{Type: MsgDataspace, Data: []byte{0x01}}, // Non-attribute
					{Type: MsgAttribute, Data: attrData},
					{Type: MsgDatatype, Data: []byte{0x02}}, // Non-attribute
				},
			},
			searchName:   "test_attr",
			wantFound:    true,
			wantIndex:    1,
			wantAttrName: "test_attr",
		},
		{
			name: "empty object header",
			oh: &ObjectHeader{
				Version:  2,
				Messages: []*HeaderMessage{},
			},
			searchName: "test_attr",
			wantFound:  false,
			wantIndex:  -1,
		},
		{
			name: "malformed attribute skipped",
			oh: &ObjectHeader{
				Version: 2,
				Messages: []*HeaderMessage{
					{Type: MsgAttribute, Data: []byte{0x00}}, // Too short, malformed
					{Type: MsgAttribute, Data: attrData},     // Valid
				},
			},
			searchName:   "test_attr",
			wantFound:    true,
			wantIndex:    1, // Found at second message (first skipped)
			wantAttrName: "test_attr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr, index, err := FindCompactAttribute(tt.oh, tt.searchName, sb.Endianness)
			require.NoError(t, err)

			if tt.wantFound {
				require.NotNil(t, attr, "Expected to find attribute")
				require.Equal(t, tt.wantAttrName, attr.Name)
				require.Equal(t, tt.wantIndex, index)
			} else {
				require.Nil(t, attr, "Expected not to find attribute")
				require.Equal(t, -1, index)
			}
		})
	}
}

// ============================================================================
// ModifyCompactAttribute Tests
// ============================================================================

func TestModifyCompactAttribute_Validation(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
	}

	newAttr := &Attribute{
		Name: "test",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x02, 0x00, 0x00, 0x00},
	}

	t.Run("nil writer", func(t *testing.T) {
		err := ModifyCompactAttribute(nil, 0x1000, "test", newAttr, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "writer is nil")
	})

	t.Run("nil new attribute", func(t *testing.T) {
		writer := &mockReaderWriterAt{data: make([]byte, 1024)}
		err := ModifyCompactAttribute(writer, 0x1000, "test", nil, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "new attribute is nil")
	})

	t.Run("empty attribute name", func(t *testing.T) {
		writer := &mockReaderWriterAt{data: make([]byte, 1024)}
		err := ModifyCompactAttribute(writer, 0x1000, "", newAttr, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "attribute name cannot be empty")
	})
}

func TestModifyCompactAttribute_AttributeNotFound(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	// Create object header with one attribute
	attr1 := &Attribute{
		Name: "existing",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x01, 0x00, 0x00, 0x00},
	}

	attr1Data, err := EncodeAttributeFromStruct(attr1, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: attr1Data},
		},
	}

	// Write object header to mock storage (use larger buffer for safety)
	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	newAttr := &Attribute{
		Name: "nonexistent",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x02, 0x00, 0x00, 0x00},
	}

	err = ModifyCompactAttribute(writer, 0x1000, "nonexistent", newAttr, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute \"nonexistent\" not found")
}

func TestModifyCompactAttribute_SameSize_InPlaceUpdate(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	// Create original attribute
	originalAttr := &Attribute{
		Name: "temperature",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x14, 0x00, 0x00, 0x00}, // int32(20)
	}

	originalData, err := EncodeAttributeFromStruct(originalAttr, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: originalData},
		},
	}

	// Write to mock storage (use larger buffer for safety)
	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// New attribute with SAME structure (same size)
	newAttr := &Attribute{
		Name: "temperature",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x19, 0x00, 0x00, 0x00}, // int32(25)
	}

	// Modify (should use in-place update)
	err = ModifyCompactAttribute(writer, 0x1000, "temperature", newAttr, sb)
	require.NoError(t, err)

	// Verify: Read back and check
	ohRead, err := ReadObjectHeader(writer, 0x1000, sb)
	require.NoError(t, err)
	require.Len(t, ohRead.Messages, 1, "Should have exactly 1 message (in-place update)")

	// Parse attribute
	attr, err := ParseAttributeMessage(ohRead.Messages[0].Data, sb.Endianness)
	require.NoError(t, err)
	require.Equal(t, "temperature", attr.Name)
	require.Equal(t, []byte{0x19, 0x00, 0x00, 0x00}, attr.Data)
}

func TestModifyCompactAttribute_DifferentSize_ReplaceMessage(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	// Original attribute: int32 scalar
	originalAttr := &Attribute{
		Name: "count",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x0A, 0x00, 0x00, 0x00}, // int32(10)
	}

	originalData, err := EncodeAttributeFromStruct(originalAttr, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgDataspace, Data: []byte{0x01, 0x02, 0x03}}, // Dummy message before
			{Type: MsgAttribute, Data: originalData},
			{Type: MsgDatatype, Data: []byte{0x04, 0x05}}, // Dummy message after
		},
	}

	// Write to mock storage (use larger buffer for safety)
	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// New attribute: int32 array [5] (DIFFERENT SIZE!)
	newAttr := &Attribute{
		Name: "count",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{5},
		},
		Data: []byte{
			0x01, 0x00, 0x00, 0x00,
			0x02, 0x00, 0x00, 0x00,
			0x03, 0x00, 0x00, 0x00,
			0x04, 0x00, 0x00, 0x00,
			0x05, 0x00, 0x00, 0x00,
		},
	}

	// Modify (should remove old, append new)
	err = ModifyCompactAttribute(writer, 0x1000, "count", newAttr, sb)
	require.NoError(t, err)

	// Verify: Read back
	ohRead, err := ReadObjectHeader(writer, 0x1000, sb)
	require.NoError(t, err)
	require.Len(t, ohRead.Messages, 3, "Should have 3 messages (2 dummies + 1 new attribute)")

	// Find attribute message (should be last now, as it was appended)
	var attrMsg *HeaderMessage
	for _, msg := range ohRead.Messages {
		if msg.Type == MsgAttribute {
			attrMsg = msg
			break
		}
	}
	require.NotNil(t, attrMsg, "Should have attribute message")

	attr, err := ParseAttributeMessage(attrMsg.Data, sb.Endianness)
	require.NoError(t, err)
	require.Equal(t, "count", attr.Name)
	require.Equal(t, uint64(5), attr.Dataspace.Dimensions[0])
	require.Equal(t, 20, len(attr.Data)) // 5 int32s = 20 bytes
}

func TestModifyCompactAttribute_MultipleAttributes(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	// Create 3 attributes
	createAttr := func(name string, value int32) *Attribute {
		return &Attribute{
			Name: name,
			Datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Version:       1,
				Size:          4,
				ClassBitField: 0x08,
			},
			Dataspace: &DataspaceMessage{
				Version:    1,
				Type:       DataspaceSimple,
				Dimensions: []uint64{1},
			},
			Data: []byte{
				byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24),
			},
		}
	}

	attr1 := createAttr("attr1", 10)
	attr2 := createAttr("attr2", 20)
	attr3 := createAttr("attr3", 30)

	attr1Data, err := EncodeAttributeFromStruct(attr1, sb)
	require.NoError(t, err)
	attr2Data, err := EncodeAttributeFromStruct(attr2, sb)
	require.NoError(t, err)
	attr3Data, err := EncodeAttributeFromStruct(attr3, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: attr1Data},
			{Type: MsgAttribute, Data: attr2Data},
			{Type: MsgAttribute, Data: attr3Data},
		},
	}

	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// Modify middle attribute (attr2)
	newAttr2 := createAttr("attr2", 999)
	err = ModifyCompactAttribute(writer, 0x1000, "attr2", newAttr2, sb)
	require.NoError(t, err)

	// Verify: all 3 attributes still present, attr2 modified
	ohRead, err := ReadObjectHeader(writer, 0x1000, sb)
	require.NoError(t, err)
	require.Len(t, ohRead.Messages, 3)

	// Parse all attributes and verify
	attrs := make(map[string][]byte)
	for _, msg := range ohRead.Messages {
		if msg.Type == MsgAttribute {
			attr, parseErr := ParseAttributeMessage(msg.Data, sb.Endianness)
			require.NoError(t, parseErr)
			attrs[attr.Name] = attr.Data
		}
	}

	require.Equal(t, []byte{10, 0, 0, 0}, attrs["attr1"], "attr1 unchanged")
	require.Equal(t, []byte{231, 3, 0, 0}, attrs["attr2"], "attr2 modified (999)")
	require.Equal(t, []byte{30, 0, 0, 0}, attrs["attr3"], "attr3 unchanged")
}

// ============================================================================
// DeleteCompactAttribute Tests
// ============================================================================

func TestDeleteCompactAttribute_Validation(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
	}

	t.Run("nil writer", func(t *testing.T) {
		err := DeleteCompactAttribute(nil, 0x1000, "test", sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "writer is nil")
	})

	t.Run("empty attribute name", func(t *testing.T) {
		writer := &mockReaderWriterAt{data: make([]byte, 1024)}
		err := DeleteCompactAttribute(writer, 0x1000, "", sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "attribute name cannot be empty")
	})
}

func TestDeleteCompactAttribute_Success(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	// Create object header with 3 attributes
	createAttr := func(name string, value int32) *Attribute {
		return &Attribute{
			Name: name,
			Datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Version:       1,
				Size:          4,
				ClassBitField: 0x08,
			},
			Dataspace: &DataspaceMessage{
				Version:    1,
				Type:       DataspaceSimple,
				Dimensions: []uint64{1},
			},
			Data: []byte{
				byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24),
			},
		}
	}

	attr1 := createAttr("attr1", 1)
	attr2 := createAttr("attr2", 2)
	attr3 := createAttr("attr3", 3)

	attr1Data, err := EncodeAttributeFromStruct(attr1, sb)
	require.NoError(t, err)
	attr2Data, err := EncodeAttributeFromStruct(attr2, sb)
	require.NoError(t, err)
	attr3Data, err := EncodeAttributeFromStruct(attr3, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: attr1Data},
			{Type: MsgAttribute, Data: attr2Data},
			{Type: MsgAttribute, Data: attr3Data},
		},
	}

	// Write to mock storage (use larger buffer for safety)
	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// Delete middle attribute (attr2)
	err = DeleteCompactAttribute(writer, 0x1000, "attr2", sb)
	require.NoError(t, err)

	// Verify: Read back and check
	ohRead, err := ReadObjectHeader(writer, 0x1000, sb)
	require.NoError(t, err)
	require.Len(t, ohRead.Messages, 2, "Should have 2 messages after deletion")

	// Verify remaining attributes are attr1 and attr3
	attrs := make([]string, 0, 2)
	for _, msg := range ohRead.Messages {
		if msg.Type == MsgAttribute {
			attr, parseErr := ParseAttributeMessage(msg.Data, sb.Endianness)
			require.NoError(t, parseErr)
			attrs = append(attrs, attr.Name)
		}
	}

	require.ElementsMatch(t, []string{"attr1", "attr3"}, attrs)
}

func TestDeleteCompactAttribute_NotFound(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	attr1 := &Attribute{
		Name: "existing",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x01, 0x00, 0x00, 0x00},
	}

	attr1Data, err := EncodeAttributeFromStruct(attr1, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: attr1Data},
		},
	}

	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// Try to delete non-existent attribute
	err = DeleteCompactAttribute(writer, 0x1000, "nonexistent", sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute \"nonexistent\" not found")
}

func TestDeleteCompactAttribute_LastAttribute(t *testing.T) {
	sb := &Superblock{
		Version:    2,
		Endianness: binary.LittleEndian,
		OffsetSize: 8,
		LengthSize: 8,
	}

	attr := &Attribute{
		Name: "only_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data: []byte{0x42, 0x00, 0x00, 0x00},
	}

	attrData, err := EncodeAttributeFromStruct(attr, sb)
	require.NoError(t, err)

	oh := &ObjectHeader{
		Version: 2,
		Messages: []*HeaderMessage{
			{Type: MsgAttribute, Data: attrData},
		},
	}

	writer := &mockReaderWriterAt{data: make([]byte, 16384)}
	err = WriteObjectHeader(writer, 0x1000, oh, sb)
	require.NoError(t, err)

	// Delete the only attribute
	err = DeleteCompactAttribute(writer, 0x1000, "only_attr", sb)
	require.NoError(t, err)

	// Verify: no attribute messages left
	ohRead, err := ReadObjectHeader(writer, 0x1000, sb)
	require.NoError(t, err)

	attrCount := 0
	for _, msg := range ohRead.Messages {
		if msg.Type == MsgAttribute {
			attrCount++
		}
	}
	require.Equal(t, 0, attrCount, "Should have no attribute messages")
}

// ============================================================================
// ModifyDenseAttribute Tests
// ============================================================================

func TestModifyDenseAttribute_Validation(t *testing.T) {
	tests := []struct {
		name     string
		heap     HeapWriter
		btree    BTreeWriter
		attrName string
		newAttr  *Attribute
		wantErr  string
	}{
		{
			name:     "nil heap",
			heap:     nil,
			btree:    &mockBTreeWriter{},
			attrName: "test",
			newAttr:  &Attribute{Data: []byte{0x01}},
			wantErr:  "heap or btree is nil",
		},
		{
			name:     "nil btree",
			heap:     &mockHeapWriter{},
			btree:    nil,
			attrName: "test",
			newAttr:  &Attribute{Data: []byte{0x01}},
			wantErr:  "heap or btree is nil",
		},
		{
			name:     "empty attribute name",
			heap:     &mockHeapWriter{},
			btree:    &mockBTreeWriter{},
			attrName: "",
			newAttr:  &Attribute{Data: []byte{0x01}},
			wantErr:  "attribute name cannot be empty",
		},
		{
			name:     "nil new attribute",
			heap:     &mockHeapWriter{},
			btree:    &mockBTreeWriter{},
			attrName: "test",
			newAttr:  nil,
			wantErr:  "new attribute is nil",
		},
		{
			name: "empty new attribute data",
			heap: &mockHeapWriter{
				objects: map[string][]byte{
					string([]byte{0x10, 0, 0, 0, 0, 0, 0, 0}): {0x01, 0x02},
				},
			},
			btree: &mockBTreeWriter{
				records: map[string][]byte{
					"test": {0x10, 0, 0, 0, 0, 0, 0, 0},
				},
			},
			attrName: "test",
			newAttr:  &Attribute{Name: "test", Data: []byte{}},
			wantErr:  "new attribute data is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ModifyDenseAttribute(tt.heap, tt.btree, tt.attrName, tt.newAttr)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestModifyDenseAttribute_NotFound(t *testing.T) {
	heap := &mockHeapWriter{}
	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"existing": {0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
	}

	newAttr := &Attribute{
		Name: "nonexistent",
		Data: []byte{0x02, 0x03},
	}

	err := ModifyDenseAttribute(heap, btree, "nonexistent", newAttr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute \"nonexistent\" not found in dense storage")
}

func TestModifyDenseAttribute_SameSize_InPlace(t *testing.T) {
	oldData := []byte{0x01, 0x02, 0x03, 0x04} // 4 bytes
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(heapID): oldData,
		},
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"temperature": heapID,
		},
	}

	newData := []byte{0x05, 0x06, 0x07, 0x08} // Same size (4 bytes)
	newAttr := &Attribute{
		Name: "temperature",
		Data: newData,
	}

	err := ModifyDenseAttribute(heap, btree, "temperature", newAttr)
	require.NoError(t, err)

	// Verify: Overwrite called, not Delete+Insert
	require.True(t, heap.overwriteCalled, "Expected OverwriteObject to be called")
	require.False(t, heap.deleteCalled, "Expected DeleteObject NOT to be called")
	require.False(t, heap.insertCalled, "Expected InsertObject NOT to be called")
	require.False(t, btree.updateCalled, "Expected UpdateRecord NOT to be called (same heap ID)")

	// Verify data was overwritten
	require.Equal(t, newData, heap.objects[string(heapID)])
}

func TestModifyDenseAttribute_DifferentSize_Replace(t *testing.T) {
	oldData := []byte{0x01, 0x02} // 2 bytes
	oldHeapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	newHeapID := []byte{0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(oldHeapID): oldData,
		},
		nextHeapID: newHeapID,
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"count": oldHeapID,
		},
	}

	newData := []byte{0x03, 0x04, 0x05, 0x06, 0x07} // Different size (5 bytes)
	newAttr := &Attribute{
		Name: "count",
		Data: newData,
	}

	err := ModifyDenseAttribute(heap, btree, "count", newAttr)
	require.NoError(t, err)

	// Verify: Delete old + Insert new + Update B-tree
	require.True(t, heap.deleteCalled, "Expected DeleteObject to be called")
	require.True(t, heap.insertCalled, "Expected InsertObject to be called")
	require.True(t, btree.updateCalled, "Expected UpdateRecord to be called")
	require.False(t, heap.overwriteCalled, "Expected OverwriteObject NOT to be called")

	// Verify B-tree was updated with new heap ID
	require.Equal(t, newHeapID, btree.records["count"])
}

func TestModifyDenseAttribute_HeapIDLengthValidation(t *testing.T) {
	// Test edge case: InsertObject returns invalid heap ID length
	invalidHeapID := []byte{0x01, 0x02, 0x03} // Wrong length (not 8 bytes)
	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string([]byte{0x10, 0, 0, 0, 0, 0, 0, 0}): {0x01},
		},
		nextHeapID: invalidHeapID,
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"test": {0x10, 0, 0, 0, 0, 0, 0, 0},
		},
	}

	newAttr := &Attribute{
		Name: "test",
		Data: []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // Different size
	}

	err := ModifyDenseAttribute(heap, btree, "test", newAttr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected heap ID length")
}

// ============================================================================
// DeleteDenseAttribute Tests
// ============================================================================

func TestDeleteDenseAttribute_Validation(t *testing.T) {
	tests := []struct {
		name      string
		heap      HeapWriter
		btree     BTreeWriter
		attrName  string
		rebalance bool
		wantErr   string
	}{
		{
			name:      "nil heap",
			heap:      nil,
			btree:     &mockBTreeWriter{},
			attrName:  "test",
			rebalance: false,
			wantErr:   "heap or btree is nil",
		},
		{
			name:      "nil btree",
			heap:      &mockHeapWriter{},
			btree:     nil,
			attrName:  "test",
			rebalance: false,
			wantErr:   "heap or btree is nil",
		},
		{
			name:      "empty attribute name",
			heap:      &mockHeapWriter{},
			btree:     &mockBTreeWriter{},
			attrName:  "",
			rebalance: false,
			wantErr:   "attribute name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DeleteDenseAttribute(tt.heap, tt.btree, tt.attrName, tt.rebalance)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDeleteDenseAttribute_NotFound(t *testing.T) {
	heap := &mockHeapWriter{}
	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"existing": {0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
	}

	err := DeleteDenseAttribute(heap, btree, "nonexistent", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute \"nonexistent\" not found in dense storage")
}

func TestDeleteDenseAttribute_WithoutRebalancing(t *testing.T) {
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	attrData := []byte{0x01, 0x02, 0x03}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(heapID): attrData,
		},
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"temperature": heapID,
		},
	}

	err := DeleteDenseAttribute(heap, btree, "temperature", false)
	require.NoError(t, err)

	// Verify: Delete from both heap and B-tree
	require.True(t, heap.deleteCalled, "Expected DeleteObject to be called")
	require.True(t, btree.deleteRecordCalled, "Expected DeleteRecord to be called (no rebalancing)")
	require.False(t, btree.deleteRecordWithRebalancingCalled, "Expected DeleteRecordWithRebalancing NOT to be called")
	require.False(t, btree.deleteRecordLazyCalled, "Expected DeleteRecordLazy NOT to be called")

	// Verify heap object deleted
	_, exists := heap.objects[string(heapID)]
	require.False(t, exists, "Heap object should be deleted")
}

func TestDeleteDenseAttribute_WithRebalancing(t *testing.T) {
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	attrData := []byte{0x01, 0x02, 0x03}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(heapID): attrData,
		},
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"temperature": heapID,
		},
	}

	err := DeleteDenseAttribute(heap, btree, "temperature", true)
	require.NoError(t, err)

	// Verify: Delete with rebalancing
	require.True(t, heap.deleteCalled, "Expected DeleteObject to be called")
	require.True(t, btree.deleteRecordWithRebalancingCalled, "Expected DeleteRecordWithRebalancing to be called")
	require.False(t, btree.deleteRecordCalled, "Expected DeleteRecord NOT to be called (using rebalancing)")
	require.False(t, btree.deleteRecordLazyCalled, "Expected DeleteRecordLazy NOT to be called")
}

func TestDeleteDenseAttribute_LazyRebalancing(t *testing.T) {
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	attrData := []byte{0x01, 0x02, 0x03}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(heapID): attrData,
		},
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"temperature": heapID,
		},
		lazyRebalancingEnabled: true, // Enable lazy mode
	}

	err := DeleteDenseAttribute(heap, btree, "temperature", false)
	require.NoError(t, err)

	// Verify: Lazy deletion takes precedence
	require.True(t, heap.deleteCalled, "Expected DeleteObject to be called")
	require.True(t, btree.deleteRecordLazyCalled, "Expected DeleteRecordLazy to be called (lazy mode enabled)")
	require.False(t, btree.deleteRecordCalled, "Expected DeleteRecord NOT to be called")
	require.False(t, btree.deleteRecordWithRebalancingCalled, "Expected DeleteRecordWithRebalancing NOT to be called")
}

func TestDeleteDenseAttribute_LazyOverridesRebalance(t *testing.T) {
	// Test that lazy rebalancing takes precedence even if rebalance=true
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	attrData := []byte{0x01, 0x02, 0x03}

	heap := &mockHeapWriter{
		objects: map[string][]byte{
			string(heapID): attrData,
		},
	}

	btree := &mockBTreeWriter{
		records: map[string][]byte{
			"temperature": heapID,
		},
		lazyRebalancingEnabled: true,
	}

	err := DeleteDenseAttribute(heap, btree, "temperature", true) // rebalance=true, but lazy should win
	require.NoError(t, err)

	// Verify: Lazy still takes precedence
	require.True(t, btree.deleteRecordLazyCalled, "Expected DeleteRecordLazy (lazy overrides rebalance)")
	require.False(t, btree.deleteRecordWithRebalancingCalled)
	require.False(t, btree.deleteRecordCalled)
}

// ============================================================================
// Mock Implementations
// ============================================================================

// mockReaderWriterAt implements io.ReaderAt and io.WriterAt for testing.
type mockReaderWriterAt struct {
	data []byte
}

func (m *mockReaderWriterAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(m.data)) {
		return 0, fmt.Errorf("offset out of range")
	}
	n := copy(p, m.data[off:])
	return n, nil
}

func (m *mockReaderWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	if off+int64(len(p)) > int64(len(m.data)) {
		return 0, fmt.Errorf("write exceeds buffer size")
	}
	copy(m.data[off:], p)
	return len(p), nil
}

// mockHeapWriter implements HeapWriter for testing.
type mockHeapWriter struct {
	objects         map[string][]byte
	nextHeapID      []byte
	overwriteCalled bool
	deleteCalled    bool
	insertCalled    bool
}

func (m *mockHeapWriter) GetObject(heapID []byte) ([]byte, error) {
	data, exists := m.objects[string(heapID)]
	if !exists {
		return nil, fmt.Errorf("heap object not found")
	}
	return data, nil
}

func (m *mockHeapWriter) OverwriteObject(heapID, newData []byte) error {
	m.overwriteCalled = true
	m.objects[string(heapID)] = newData
	return nil
}

func (m *mockHeapWriter) DeleteObject(heapID []byte) error {
	m.deleteCalled = true
	delete(m.objects, string(heapID))
	return nil
}

func (m *mockHeapWriter) InsertObject(data []byte) ([]byte, error) {
	m.insertCalled = true
	if m.nextHeapID == nil {
		m.nextHeapID = []byte{0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	}
	m.objects[string(m.nextHeapID)] = data
	return m.nextHeapID, nil
}

// mockBTreeWriter implements BTreeWriter for testing.
type mockBTreeWriter struct {
	records                           map[string][]byte
	lazyRebalancingEnabled            bool
	updateCalled                      bool
	deleteRecordCalled                bool
	deleteRecordWithRebalancingCalled bool
	deleteRecordLazyCalled            bool
}

func (m *mockBTreeWriter) SearchRecord(name string) ([]byte, bool) {
	heapID, found := m.records[name]
	return heapID, found
}

func (m *mockBTreeWriter) UpdateRecord(name string, newHeapID uint64) error {
	m.updateCalled = true
	heapIDBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heapIDBytes, newHeapID)
	m.records[name] = heapIDBytes
	return nil
}

func (m *mockBTreeWriter) DeleteRecord(name string) error {
	m.deleteRecordCalled = true
	delete(m.records, name)
	return nil
}

func (m *mockBTreeWriter) DeleteRecordWithRebalancing(name string) error {
	m.deleteRecordWithRebalancingCalled = true
	delete(m.records, name)
	return nil
}

func (m *mockBTreeWriter) DeleteRecordLazy(name string) error {
	m.deleteRecordLazyCalled = true
	delete(m.records, name)
	return nil
}

func (m *mockBTreeWriter) IsLazyRebalancingEnabled() bool {
	return m.lazyRebalancingEnabled
}
