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
		name     string
		heap     HeapWriter
		btree    BTreeWriter
		attrName string
		wantErr  string
	}{
		{
			name:     "nil heap",
			heap:     nil,
			btree:    &mockBTreeWriter{},
			attrName: "test",
			wantErr:  "heap or btree is nil",
		},
		{
			name:     "nil btree",
			heap:     &mockHeapWriter{},
			btree:    nil,
			attrName: "test",
			wantErr:  "heap or btree is nil",
		},
		{
			name:     "empty attribute name",
			heap:     &mockHeapWriter{},
			btree:    &mockBTreeWriter{},
			attrName: "",
			wantErr:  "attribute name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DeleteDenseAttribute(tt.heap, tt.btree, tt.attrName)
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

	err := DeleteDenseAttribute(heap, btree, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "attribute \"nonexistent\" not found in dense storage")
}

func TestDeleteDenseAttribute_Success(t *testing.T) {
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

	err := DeleteDenseAttribute(heap, btree, "temperature")
	require.NoError(t, err)

	require.True(t, heap.deleteCalled, "Expected DeleteObject to be called")
	require.True(t, btree.deleteRecordCalled, "Expected DeleteRecord to be called")

	_, exists := heap.objects[string(heapID)]
	require.False(t, exists, "Heap object should be deleted")
}

// ============================================================================
// Mock Implementations
// ============================================================================

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
	records            map[string][]byte
	updateCalled       bool
	deleteRecordCalled bool
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

// errDeleteBTreeWriter finds records but fails on DeleteRecord.
type errDeleteBTreeWriter struct {
	mockBTreeWriter
}

func (m *errDeleteBTreeWriter) DeleteRecord(_ string) error {
	return fmt.Errorf("simulated b-tree delete failure")
}

// errDeleteHeapWriter behaves normally but fails on DeleteObject.
type errDeleteHeapWriter struct {
	mockHeapWriter
}

func (m *errDeleteHeapWriter) DeleteObject(_ []byte) error {
	return fmt.Errorf("simulated heap delete failure")
}

func TestDeleteDenseAttribute_BTreeDeleteError(t *testing.T) {
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	heap := &mockHeapWriter{
		objects: map[string][]byte{string(heapID): {0x01}},
	}
	btree := &errDeleteBTreeWriter{
		mockBTreeWriter: mockBTreeWriter{
			records: map[string][]byte{"temperature": heapID},
		},
	}

	err := DeleteDenseAttribute(heap, btree, "temperature")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to delete B-tree record")
}

func TestDeleteDenseAttribute_HeapDeleteError(t *testing.T) {
	heapID := []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	heap := &errDeleteHeapWriter{
		mockHeapWriter: mockHeapWriter{
			objects: map[string][]byte{string(heapID): {0x01}},
		},
	}
	btree := &mockBTreeWriter{
		records: map[string][]byte{"temperature": heapID},
	}

	err := DeleteDenseAttribute(heap, btree, "temperature")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to delete heap object")
}
