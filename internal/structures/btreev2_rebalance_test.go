// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"fmt"
	"testing"
)

// TestBTreeV2_DeleteRecordWithRebalancing_SingleRecord tests deletion from single-record tree.
func TestBTreeV2_DeleteRecordWithRebalancing_SingleRecord(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert one record
	err := bt.InsertRecord("attr1", 0x1000)
	if err != nil {
		t.Fatalf("Failed to insert record: %v", err)
	}

	// Verify inserted
	if !bt.HasKey("attr1") {
		t.Fatal("Record not found after insertion")
	}

	// Delete record
	err = bt.DeleteRecordWithRebalancing("attr1")
	if err != nil {
		t.Fatalf("Failed to delete record: %v", err)
	}

	// Verify deleted
	if bt.HasKey("attr1") {
		t.Fatal("Record still exists after deletion")
	}

	// Verify counts
	if bt.header.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", bt.header.TotalRecords)
	}
	if bt.header.NumRecordsRoot != 0 {
		t.Errorf("NumRecordsRoot = %d, want 0", bt.header.NumRecordsRoot)
	}
	if len(bt.records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(bt.records))
	}
}

// TestBTreeV2_DeleteRecordWithRebalancing_MultipleRecords tests deletion from tree with multiple records.
func TestBTreeV2_DeleteRecordWithRebalancing_MultipleRecords(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert 10 records
	names := []string{"attr1", "attr2", "attr3", "attr4", "attr5", "attr6", "attr7", "attr8", "attr9", "attr10"}
	for i, name := range names {
		err := bt.InsertRecord(name, uint64(0x1000+i*0x100))
		if err != nil {
			t.Fatalf("Failed to insert %s: %v", name, err)
		}
	}

	// Verify all inserted
	if bt.header.TotalRecords != 10 {
		t.Errorf("TotalRecords = %d, want 10", bt.header.TotalRecords)
	}

	// Delete every other record
	toDelete := []string{"attr2", "attr4", "attr6", "attr8", "attr10"}
	for _, name := range toDelete {
		err := bt.DeleteRecordWithRebalancing(name)
		if err != nil {
			t.Fatalf("Failed to delete %s: %v", name, err)
		}
	}

	// Verify deleted records gone
	for _, name := range toDelete {
		if bt.HasKey(name) {
			t.Errorf("Record %s still exists after deletion", name)
		}
	}

	// Verify remaining records still exist
	remaining := []string{"attr1", "attr3", "attr5", "attr7", "attr9"}
	for _, name := range remaining {
		if !bt.HasKey(name) {
			t.Errorf("Record %s missing after partial deletion", name)
		}
	}

	// Verify counts
	if bt.header.TotalRecords != 5 {
		t.Errorf("TotalRecords = %d, want 5", bt.header.TotalRecords)
	}
	if len(bt.records) != 5 {
		t.Errorf("len(records) = %d, want 5", len(bt.records))
	}
}

// TestBTreeV2_DeleteRecordWithRebalancing_DeleteAll tests deleting all records one by one.
func TestBTreeV2_DeleteRecordWithRebalancing_DeleteAll(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert 20 records
	names := make([]string, 20)
	for i := 0; i < 20; i++ {
		names[i] = fmt.Sprintf("attr%02d", i+1)
		err := bt.InsertRecord(names[i], uint64(0x1000+i*0x100))
		if err != nil {
			t.Fatalf("Failed to insert %s: %v", names[i], err)
		}
	}

	// Delete all records in reverse order
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]

		// Verify exists before deletion
		if !bt.HasKey(name) {
			t.Errorf("Record %s missing before deletion", name)
		}

		// Delete
		err := bt.DeleteRecordWithRebalancing(name)
		if err != nil {
			t.Fatalf("Failed to delete %s: %v", name, err)
		}

		// Verify deleted
		if bt.HasKey(name) {
			t.Errorf("Record %s still exists after deletion", name)
		}

		// Verify counts
		expectedCount := uint64(i)
		if bt.header.TotalRecords != expectedCount {
			t.Errorf("After deleting %s: TotalRecords = %d, want %d", name, bt.header.TotalRecords, expectedCount)
		}
	}

	// Verify tree empty
	if bt.header.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", bt.header.TotalRecords)
	}
	if len(bt.records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(bt.records))
	}
}

// TestBTreeV2_DeleteRecordWithRebalancing_NotFound tests deletion of non-existent record.
func TestBTreeV2_DeleteRecordWithRebalancing_NotFound(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert records
	_ = bt.InsertRecord("attr1", 0x1000)
	_ = bt.InsertRecord("attr2", 0x2000)

	// Try to delete non-existent record
	err := bt.DeleteRecordWithRebalancing("nonexistent")
	if err == nil {
		t.Fatal("Expected error when deleting non-existent record, got nil")
	}

	// Verify original records still exist
	if !bt.HasKey("attr1") {
		t.Error("attr1 missing after failed deletion")
	}
	if !bt.HasKey("attr2") {
		t.Error("attr2 missing after failed deletion")
	}

	// Verify counts unchanged
	if bt.header.TotalRecords != 2 {
		t.Errorf("TotalRecords = %d, want 2", bt.header.TotalRecords)
	}
}

// TestBTreeV2_DeleteRecordWithRebalancing_Idempotent tests that deleting same record twice fails gracefully.
func TestBTreeV2_DeleteRecordWithRebalancing_Idempotent(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert record
	_ = bt.InsertRecord("attr1", 0x1000)

	// Delete once (should succeed)
	err := bt.DeleteRecordWithRebalancing("attr1")
	if err != nil {
		t.Fatalf("First deletion failed: %v", err)
	}

	// Delete again (should fail)
	err = bt.DeleteRecordWithRebalancing("attr1")
	if err == nil {
		t.Fatal("Expected error on second deletion, got nil")
	}

	// Verify tree empty
	if bt.header.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", bt.header.TotalRecords)
	}
}

// TestBTreeV2_DeleteRecordWithRebalancing_OrderMaintained tests that record order is maintained after deletions.
func TestBTreeV2_DeleteRecordWithRebalancing_OrderMaintained(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Insert records in specific order
	names := []string{"zebra", "apple", "mango", "banana", "orange"}
	for i, name := range names {
		_ = bt.InsertRecord(name, uint64(0x1000+i*0x100))
	}

	// Records should be sorted by hash
	records := bt.GetRecords()
	for i := 0; i < len(records)-1; i++ {
		if records[i].NameHash > records[i+1].NameHash {
			t.Errorf("Records not sorted: hash[%d]=%d > hash[%d]=%d",
				i, records[i].NameHash, i+1, records[i+1].NameHash)
		}
	}

	// Delete middle record
	_ = bt.DeleteRecordWithRebalancing("mango")

	// Verify order still maintained
	records = bt.GetRecords()
	for i := 0; i < len(records)-1; i++ {
		if records[i].NameHash > records[i+1].NameHash {
			t.Errorf("Records not sorted after deletion: hash[%d]=%d > hash[%d]=%d",
				i, records[i].NameHash, i+1, records[i+1].NameHash)
		}
	}
}

// TestBTreeV2_CalculateMinRecords tests minimum records calculation.
func TestBTreeV2_CalculateMinRecords(t *testing.T) {
	tests := []struct {
		name     string
		nodeSize uint32
		wantMin  int
	}{
		{"4KB node", 4096, 185},   // (4096-10)/11 = 371 max → 371/2 = 185 min
		{"8KB node", 8192, 371},   // (8192-10)/11 = 743 max → 743/2 = 371 min
		{"16KB node", 16384, 744}, // (16384-10)/11 = 1488 max → 1488/2 = 744 min
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt := NewWritableBTreeV2(tt.nodeSize)
			minRecords := bt.calculateMinRecords()

			if minRecords != tt.wantMin {
				t.Errorf("calculateMinRecords() = %d, want %d", minRecords, tt.wantMin)
			}
		})
	}
}

// TestBTreeV2_MergeNodes tests 2-way node merging (future multi-level trees).
func TestBTreeV2_MergeNodes(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Create two small leaf nodes
	left := &BTreeV2LeafNode{
		Signature: [4]byte{'B', 'T', 'L', 'F'},
		Version:   0,
		Type:      BTreeV2TypeLinkNameIndex,
		Records: []LinkNameRecord{
			{NameHash: 100, HeapID: [7]byte{1, 0, 0, 0, 0, 0, 0}},
			{NameHash: 200, HeapID: [7]byte{2, 0, 0, 0, 0, 0, 0}},
		},
	}

	right := &BTreeV2LeafNode{
		Signature: [4]byte{'B', 'T', 'L', 'F'},
		Version:   0,
		Type:      BTreeV2TypeLinkNameIndex,
		Records: []LinkNameRecord{
			{NameHash: 300, HeapID: [7]byte{3, 0, 0, 0, 0, 0, 0}},
			{NameHash: 400, HeapID: [7]byte{4, 0, 0, 0, 0, 0, 0}},
		},
	}

	// Merge
	err := bt.mergeNodes(left, right)
	if err != nil {
		t.Fatalf("mergeNodes failed: %v", err)
	}

	// Verify left node has all records
	if len(left.Records) != 4 {
		t.Errorf("len(left.Records) = %d, want 4", len(left.Records))
	}

	// Verify right node marked as deleted
	if len(right.Records) != 0 {
		t.Errorf("len(right.Records) = %d, want 0 (deleted)", len(right.Records))
	}

	// Verify records combined correctly
	expectedHashes := []uint32{100, 200, 300, 400}
	for i, hash := range expectedHashes {
		if left.Records[i].NameHash != hash {
			t.Errorf("left.Records[%d].NameHash = %d, want %d", i, left.Records[i].NameHash, hash)
		}
	}
}

// TestBTreeV2_RedistributeRecords tests 2-way record redistribution.
func TestBTreeV2_RedistributeRecords(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Create unbalanced nodes: left=10 records, right=2 records
	leftRecords := make([]LinkNameRecord, 10)
	for i := range leftRecords {
		leftRecords[i] = LinkNameRecord{
			NameHash: uint32(i * 100),
			HeapID:   [7]byte{byte(i), 0, 0, 0, 0, 0, 0},
		}
	}

	rightRecords := make([]LinkNameRecord, 2)
	for i := range rightRecords {
		rightRecords[i] = LinkNameRecord{
			NameHash: uint32((10 + i) * 100),
			HeapID:   [7]byte{byte(10 + i), 0, 0, 0, 0, 0, 0},
		}
	}

	left := &BTreeV2LeafNode{
		Signature: [4]byte{'B', 'T', 'L', 'F'},
		Version:   0,
		Type:      BTreeV2TypeLinkNameIndex,
		Records:   leftRecords,
	}

	right := &BTreeV2LeafNode{
		Signature: [4]byte{'B', 'T', 'L', 'F'},
		Version:   0,
		Type:      BTreeV2TypeLinkNameIndex,
		Records:   rightRecords,
	}

	// Redistribute
	err := bt.redistributeRecords(left, right)
	if err != nil {
		t.Fatalf("redistributeRecords failed: %v", err)
	}

	// Verify balanced: 12 total → 6 left, 6 right
	if len(left.Records) != 6 {
		t.Errorf("len(left.Records) = %d, want 6", len(left.Records))
	}
	if len(right.Records) != 6 {
		t.Errorf("len(right.Records) = %d, want 6", len(right.Records))
	}

	// Verify all records present
	allRecords := make([]LinkNameRecord, len(left.Records)+len(right.Records))
	copy(allRecords, left.Records)
	copy(allRecords[len(left.Records):], right.Records)
	if len(allRecords) != 12 {
		t.Errorf("Total records = %d, want 12", len(allRecords))
	}
}

// TestBTreeV2_BorrowFromLeft tests borrowing records from left sibling.
func TestBTreeV2_BorrowFromLeft(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Left sibling has spare records
	left := &BTreeV2LeafNode{
		Records: []LinkNameRecord{
			{NameHash: 100, HeapID: [7]byte{1}},
			{NameHash: 200, HeapID: [7]byte{2}},
			{NameHash: 300, HeapID: [7]byte{3}},
		},
	}

	// Current node is sparse
	current := &BTreeV2LeafNode{
		Records: []LinkNameRecord{
			{NameHash: 400, HeapID: [7]byte{4}},
		},
	}

	// Borrow from left
	err := bt.borrowFromLeft(current, left)
	if err != nil {
		t.Fatalf("borrowFromLeft failed: %v", err)
	}

	// Verify left lost last record
	if len(left.Records) != 2 {
		t.Errorf("len(left.Records) = %d, want 2", len(left.Records))
	}

	// Verify current gained record
	if len(current.Records) != 2 {
		t.Errorf("len(current.Records) = %d, want 2", len(current.Records))
	}

	// Verify borrowed record is first in current
	if current.Records[0].NameHash != 300 {
		t.Errorf("current.Records[0].NameHash = %d, want 300", current.Records[0].NameHash)
	}
}

// TestBTreeV2_BorrowFromRight tests borrowing records from right sibling.
func TestBTreeV2_BorrowFromRight(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// Current node is sparse
	current := &BTreeV2LeafNode{
		Records: []LinkNameRecord{
			{NameHash: 100, HeapID: [7]byte{1}},
		},
	}

	// Right sibling has spare records
	right := &BTreeV2LeafNode{
		Records: []LinkNameRecord{
			{NameHash: 200, HeapID: [7]byte{2}},
			{NameHash: 300, HeapID: [7]byte{3}},
			{NameHash: 400, HeapID: [7]byte{4}},
		},
	}

	// Borrow from right
	err := bt.borrowFromRight(current, right)
	if err != nil {
		t.Fatalf("borrowFromRight failed: %v", err)
	}

	// Verify right lost first record
	if len(right.Records) != 2 {
		t.Errorf("len(right.Records) = %d, want 2", len(right.Records))
	}

	// Verify current gained record
	if len(current.Records) != 2 {
		t.Errorf("len(current.Records) = %d, want 2", len(current.Records))
	}

	// Verify borrowed record is last in current
	if current.Records[1].NameHash != 200 {
		t.Errorf("current.Records[1].NameHash = %d, want 200", current.Records[1].NameHash)
	}
}

// TestBTreeV2_GetSiblings tests sibling retrieval (MVP returns nil).
func TestBTreeV2_GetSiblings(t *testing.T) {
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)

	// MVP: Single leaf has no siblings
	left, right := bt.getSiblings(bt.leaf)

	if left != nil {
		t.Error("Expected nil left sibling for single-leaf tree")
	}
	if right != nil {
		t.Error("Expected nil right sibling for single-leaf tree")
	}
}
