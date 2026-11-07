// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// writeUint64 writes a uint64 value to buffer with specified size and endianness.
// This is a helper function for encoding fields with variable sizes.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value) //nolint:gosec // G115: variable-size encoding
	case 2:
		endianness.PutUint16(buf, uint16(value)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(value)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, value)
	}
}

// B-tree v2 constants.
const (
	BTreeV2HeaderSignature = "BTHD" // B-tree v2 header signature
	BTreeV2LeafSignature   = "BTLF" // B-tree v2 leaf node signature

	// BTreeV2TypeLinkNameIndex indexes the names of dense links
	// (H5B2_GRP_DENSE_NAME_ID, H5B2private.h:46).
	BTreeV2TypeLinkNameIndex = uint8(5)
	// BTreeV2TypeAttrNameIndex indexes the names of dense attributes
	// (H5B2_ATTR_DENSE_NAME_ID, H5B2private.h:50).
	BTreeV2TypeAttrNameIndex = uint8(8)
	// BTreeV2TypeAttrCorderIndex indexes the creation order of dense
	// attributes (H5B2_ATTR_DENSE_CORDER_ID, H5B2private.h:52).
	BTreeV2TypeAttrCorderIndex = uint8(9)

	DefaultBTreeV2NodeSize     = uint32(4096) // 4KB default node size
	DefaultBTreeV2SplitPercent = uint8(100)   // Split at 100% full
	DefaultBTreeV2MergePercent = uint8(40)    // Merge at 40% full

	// CreationOrderUntracked is the "bogus" creation index the C library
	// gives an attribute of an object that does not track creation order:
	// H5O_MAX_CRT_ORDER_IDX (H5Opkg.h:33; H5Oattribute.c:293, 307).
	CreationOrderUntracked = uint32(65535)

	// AttrRecordFlagShared marks a record of an attribute index whose heap
	// ID points into the shared message heap, not the object's fractal heap
	// (H5O_MSG_FLAG_SHARED, H5Oprivate.h:74; H5Abtree2.c:225-268).
	AttrRecordFlagShared = uint8(0x02)
)

// Record layouts of the index types.
const (
	// linkHeapIDLen is H5G_DENSE_FHEAP_ID_LEN (H5Gpkg.h:40).
	linkHeapIDLen = 7
	// attrHeapIDLen is H5O_FHEAP_ID_LEN (H5Oprivate.h:64).
	attrHeapIDLen = 8

	// linkNameRecordSize: hash(4), heap id(7) (H5Gbtree2.c
	// H5G__dense_btree2_name_encode).
	linkNameRecordSize = 4 + linkHeapIDLen
	// attrNameRecordSize: heap id(8), flags(1), creation order(4), hash(4)
	// (H5Abtree2.c:312-326; H5Adense.c:217).
	attrNameRecordSize = attrHeapIDLen + 1 + 4 + 4
	// attrCorderRecordSize: heap id(8), flags(1), creation order(4)
	// (H5Abtree2.c:449-462; H5Adense.c:234).
	attrCorderRecordSize = attrHeapIDLen + 1 + 4

	// leafOverhead: signature(4), version(1), type(1), checksum(4)
	// (H5B2cache.c H5B2__cache_leaf_serialize).
	leafOverhead = 4 + 1 + 1 + 4
)

// B-tree v2 error definitions.
var (
	ErrBTreeNodeFull    = errors.New("b-tree node is full")
	ErrInvalidBTreeType = errors.New("invalid b-tree type")
	ErrInvalidNodeSize  = errors.New("invalid node size")
)

// recordSizeOf returns the record size of an index type this package writes.
func recordSizeOf(typ uint8) (int, bool) {
	switch typ {
	case BTreeV2TypeLinkNameIndex:
		return linkNameRecordSize, true
	case BTreeV2TypeAttrNameIndex:
		return attrNameRecordSize, true
	case BTreeV2TypeAttrCorderIndex:
		return attrCorderRecordSize, true
	default:
		return 0, false
	}
}

// BTreeV2Header represents B-tree v2 header structure.
//
// Format (from H5B2hdr.c and HDF5 Format Spec III.A.2):
//   - Signature: "BTHD" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: 5, 8 or 9 (1 byte)
//   - Node Size: 4 bytes
//   - Record Size: 2 bytes
//   - Depth: 0 for single leaf (2 bytes)
//   - Split Percent: 100 (1 byte)
//   - Merge Percent: 40 (1 byte)
//   - Root Node Address: offsetSize bytes
//   - Number of Records in Root: 2 bytes
//   - Total Records: 8 bytes
//   - Checksum: lookup3 (4 bytes)
//
// Reference:
//   - HDF5 Format Spec: Section III.A.2 (B-tree v2)
//   - C Library: H5B2hdr.c - H5B2_hdr_t structure
//   - Serialization: H5B2cache.c - H5B2__hdr_serialize()
type BTreeV2Header struct {
	Signature      [4]byte // "BTHD"
	Version        uint8   // 0
	Type           uint8   // index type (5, 8 or 9)
	NodeSize       uint32  // Node size in bytes
	RecordSize     uint16  // Size of one record of Type
	Depth          uint16  // Tree depth (0 for MVP single leaf)
	SplitPercent   uint8   // 100
	MergePercent   uint8   // 40
	RootNodeAddr   uint64  // Address of root (leaf) node
	NumRecordsRoot uint16  // Number of records in root node
	TotalRecords   uint64  // Total records in tree
}

// BTreeV2LeafNode represents B-tree v2 leaf node structure.
//
// Format (from H5B2leaf.c and HDF5 Format Spec III.A.2):
//   - Signature: "BTLF" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: same as the header (1 byte)
//   - Records: array of records of the header's record size
//   - Checksum: lookup3 (4 bytes)
//
// Reference:
//   - HDF5 Format Spec: Section III.A.2 (B-tree v2)
//   - C Library: H5B2leaf.c - H5B2_leaf_t structure
//   - Serialization: H5B2cache.c - H5B2__leaf_serialize()
type BTreeV2LeafNode struct {
	Signature [4]byte       // "BTLF"
	Version   uint8         // 0
	Type      uint8         // same as the header
	Records   []IndexRecord // records, in index order
}

// IndexRecord is one record of a link name, attribute name or attribute
// creation order index.
//
// Which fields are stored depends on the index type:
//   - type 5 (H5Gbtree2.c H5G__dense_btree2_name_encode): NameHash and the
//     first 7 bytes of HeapID;
//   - type 8 (H5Abtree2.c H5A__dense_btree2_name_encode): HeapID, Flags,
//     CreationOrder, NameHash;
//   - type 9 (H5Abtree2.c H5A__dense_btree2_corder_encode): HeapID, Flags,
//     CreationOrder.
//
// The heap ID points to the link or attribute message in the fractal heap.
type IndexRecord struct {
	NameHash      uint32  // lookup3 of the name (types 5 and 8)
	HeapID        [8]byte // fractal heap ID; type 5 stores the first 7 bytes
	Flags         uint8   // message flags, H5O_MSG_FLAG_SHARED (types 8 and 9)
	CreationOrder uint32  // creation index (types 8 and 9)

	// name is the name the record was inserted or resolved with; empty for
	// a record read from a file and not yet resolved.
	name string
}

// NameResolver returns the name of the object a record of a name index
// points at: the link or attribute name stored in the fractal heap. The
// C library compares records with equal hashes by that name
// (H5A__dense_btree2_name_compare, H5Abtree2.c:225-268;
// H5G__dense_btree2_name_compare).
type NameResolver func(rec IndexRecord) (string, error)

// WritableBTreeV2 manages B-tree v2 construction for link name, attribute
// name and attribute creation order indexes.
//
// MVP Limitations:
//   - Single leaf node only (no internal nodes)
//   - No splits (error if node size exceeded)
//   - Maximum records = (nodeSize - overhead) / recordSize
type WritableBTreeV2 struct {
	header   *BTreeV2Header
	leaf     *BTreeV2LeafNode
	records  []IndexRecord
	nodeSize uint32

	// resolve names records read from a file whose name is not known yet.
	resolve NameResolver

	// Addresses loaded from file (for RMW scenarios)
	loadedHeaderAddress uint64
	loadedLeafAddress   uint64

	// lazyMu guards lazyState (the pointer and everything it points to). The
	// rest of the tree belongs to the caller's goroutine, as everywhere in
	// this package; the lazy state alone is shared with the incremental
	// rebalancer's goroutine, which drains its underflow queue in the
	// background. Lock order: lazyMu before IncrementalRebalancer.mu.
	lazyMu sync.Mutex

	// Lazy rebalancing state (nil if disabled). Guarded by lazyMu.
	lazyState *LazyRebalancingState

	// Incremental rebalancing state (nil if disabled)
	incrementalRebalancer *IncrementalRebalancer
}

// NewWritableBTreeV2 creates a new B-tree v2 for link name indexing (type 5).
//
// Parameters:
//   - nodeSize: size of the B-tree node in bytes (typically 4096)
//
// Returns:
//   - *WritableBTreeV2: B-tree structure ready for record insertion
func NewWritableBTreeV2(nodeSize uint32) *WritableBTreeV2 {
	return newWritableBTreeV2(nodeSize, BTreeV2TypeLinkNameIndex)
}

// NewAttributeNameIndex creates the name index of dense attribute storage
// (type 8), as H5A__dense_create does (H5Adense.c:211-225).
func NewAttributeNameIndex(nodeSize uint32) *WritableBTreeV2 {
	return newWritableBTreeV2(nodeSize, BTreeV2TypeAttrNameIndex)
}

// NewAttributeCreationOrderIndex creates the creation order index of dense
// attribute storage (type 9), which H5A__dense_create adds when the object
// indexes attribute creation order (H5Adense.c:227-241).
func NewAttributeCreationOrderIndex(nodeSize uint32) *WritableBTreeV2 {
	return newWritableBTreeV2(nodeSize, BTreeV2TypeAttrCorderIndex)
}

func newWritableBTreeV2(nodeSize uint32, typ uint8) *WritableBTreeV2 {
	if nodeSize == 0 {
		nodeSize = DefaultBTreeV2NodeSize
	}
	recSize, _ := recordSizeOf(typ)

	return &WritableBTreeV2{
		header: &BTreeV2Header{
			Signature:      [4]byte{'B', 'T', 'H', 'D'},
			Version:        0,
			Type:           typ,
			NodeSize:       nodeSize,
			RecordSize:     uint16(recSize), //nolint:gosec // G115: at most 17
			Depth:          0,               // Single leaf
			SplitPercent:   DefaultBTreeV2SplitPercent,
			MergePercent:   DefaultBTreeV2MergePercent,
			RootNodeAddr:   0, // Set when writing
			NumRecordsRoot: 0,
			TotalRecords:   0,
		},
		leaf: &BTreeV2LeafNode{
			Signature: [4]byte{'B', 'T', 'L', 'F'},
			Version:   0,
			Type:      typ,
			Records:   make([]IndexRecord, 0),
		},
		records:  make([]IndexRecord, 0),
		nodeSize: nodeSize,
	}
}

// Type returns the index type of the tree (5, 8 or 9).
func (bt *WritableBTreeV2) Type() uint8 {
	return bt.header.Type
}

// SetNameResolver sets how the names of records read from a file are found.
// Name lookups and inserts need it to tell apart records whose names have
// equal hashes, and MigrateToAttributeNameIndex needs it to rehash.
func (bt *WritableBTreeV2) SetNameResolver(resolve NameResolver) {
	bt.resolve = resolve
}

// isNameIndex reports whether records are keyed by name (types 5 and 8).
func (bt *WritableBTreeV2) isNameIndex() bool {
	return bt.header.Type == BTreeV2TypeLinkNameIndex || bt.header.Type == BTreeV2TypeAttrNameIndex
}

// InsertRecord adds a name record to a link name (type 5) or attribute name
// (type 8) index. An attribute record gets flags 0 and the creation index
// of an object that does not track creation order.
//
// Parameters:
//   - name: link or attribute name (hashed with lookup3)
//   - heapID: 8-byte fractal heap object ID, little-endian
//
// Returns:
//   - error if node is full, the name exists or insertion fails
func (bt *WritableBTreeV2) InsertRecord(name string, heapID uint64) error {
	var id [8]byte
	binary.LittleEndian.PutUint64(id[:], heapID)
	return bt.InsertNameRecord(name, id, 0, CreationOrderUntracked)
}

// InsertNameRecord adds a record to a name index (type 5 or 8), ordered as
// the C library orders it: by hash, then by name (strcmp) for equal hashes
// (H5A__dense_btree2_name_compare). A type-5 record keeps the first 7 bytes
// of heapID; flags and corder are stored by type 8 only.
func (bt *WritableBTreeV2) InsertNameRecord(name string, heapID [8]byte, flags uint8, corder uint32) error {
	if !bt.isNameIndex() {
		return fmt.Errorf("%w: name record in index type %d", ErrInvalidBTreeType, bt.header.Type)
	}
	if bt.header.Type == BTreeV2TypeLinkNameIndex {
		heapID[linkHeapIDLen] = 0
		flags, corder = 0, 0
	}

	if len(bt.records) >= bt.calculateMaxRecords() {
		return ErrBTreeNodeFull
	}

	record := IndexRecord{
		NameHash:      jenkinsHash(name),
		HeapID:        heapID,
		Flags:         flags,
		CreationOrder: corder,
		name:          name,
	}

	pos, err := bt.namePosition(record.NameHash, name)
	if err != nil {
		return err
	}

	bt.insertAt(pos, record)
	return nil
}

// InsertCreationOrderRecord adds a record to an attribute creation order
// index (type 9), ordered by creation index
// (H5A__dense_btree2_corder_compare, H5Abtree2.c).
func (bt *WritableBTreeV2) InsertCreationOrderRecord(heapID [8]byte, flags uint8, corder uint32) error {
	if bt.header.Type != BTreeV2TypeAttrCorderIndex {
		return fmt.Errorf("%w: creation order record in index type %d", ErrInvalidBTreeType, bt.header.Type)
	}
	if len(bt.records) >= bt.calculateMaxRecords() {
		return ErrBTreeNodeFull
	}

	pos := sort.Search(len(bt.records), func(i int) bool { return bt.records[i].CreationOrder >= corder })
	if pos < len(bt.records) && bt.records[pos].CreationOrder == corder {
		return fmt.Errorf("creation index %d already in the index", corder)
	}

	bt.insertAt(pos, IndexRecord{HeapID: heapID, Flags: flags, CreationOrder: corder})
	return nil
}

// insertAt inserts record at pos and updates the counts.
func (bt *WritableBTreeV2) insertAt(pos int, record IndexRecord) {
	bt.records = append(bt.records, IndexRecord{})
	copy(bt.records[pos+1:], bt.records[pos:])
	bt.records[pos] = record
	bt.header.TotalRecords++
	bt.header.NumRecordsRoot++
	bt.leaf.Records = bt.records
}

// recordName returns the name of record i, resolving and caching it when
// it is not known yet. ok is false when it is unknown and no resolver is set.
func (bt *WritableBTreeV2) recordName(i int) (name string, ok bool, err error) {
	if bt.records[i].name != "" {
		return bt.records[i].name, true, nil
	}
	if bt.resolve == nil {
		return "", false, nil
	}
	name, err = bt.resolve(bt.records[i])
	if err != nil {
		return "", false, fmt.Errorf("resolve name of index record %d: %w", i, err)
	}
	bt.records[i].name = name
	return name, true, nil
}

// namePosition returns where a record of hash and name belongs: after every
// record that compares lower by hash, then by name. A record with the same
// name is an error, as in H5B2_insert.
func (bt *WritableBTreeV2) namePosition(hash uint32, name string) (int, error) {
	pos := sort.Search(len(bt.records), func(i int) bool { return bt.records[i].NameHash >= hash })
	for ; pos < len(bt.records) && bt.records[pos].NameHash == hash; pos++ {
		other, ok, err := bt.recordName(pos)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("cannot order %q: record %d has an equal hash and no known name", name, pos)
		}
		switch c := compareLinkNames(name, other); {
		case c == 0:
			return 0, fmt.Errorf("name %q already in the index", name)
		case c < 0:
			return pos, nil
		}
	}
	return pos, nil
}

// findByName returns the index of the record of name, or -1.
//
// Records with an equal hash are told apart by name. A record read from a
// file without a resolver is matched by hash alone when it is the only one
// with that hash (the behavior before name resolution existed); with
// several such records the name is ambiguous and is not found.
func (bt *WritableBTreeV2) findByName(name string) (int, error) {
	if !bt.isNameIndex() {
		return -1, fmt.Errorf("%w: name lookup in index type %d", ErrInvalidBTreeType, bt.header.Type)
	}
	hash := jenkinsHash(name)
	start := sort.Search(len(bt.records), func(i int) bool { return bt.records[i].NameHash >= hash })

	// The records are sorted by hash, but a tree loaded from a file may not
	// be; fall back to a scan for hash-unsorted legacy data.
	candidates := make([]int, 0, 1)
	for i := start; i < len(bt.records) && bt.records[i].NameHash == hash; i++ {
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		for i := range bt.records {
			if bt.records[i].NameHash == hash {
				candidates = append(candidates, i)
			}
		}
	}

	unknown := -1
	unknownCount := 0
	for _, i := range candidates {
		other, ok, err := bt.recordName(i)
		if err != nil {
			return -1, err
		}
		if !ok {
			unknown = i
			unknownCount++
			continue
		}
		if other == name {
			return i, nil
		}
	}
	if unknownCount == 1 {
		return unknown, nil
	}
	return -1, nil
}

// HasKey checks if a name exists in a name index.
func (bt *WritableBTreeV2) HasKey(name string) bool {
	i, err := bt.findByName(name)
	return err == nil && i >= 0
}

// FindRecord returns the record of name in a name index.
func (bt *WritableBTreeV2) FindRecord(name string) (IndexRecord, bool, error) {
	i, err := bt.findByName(name)
	if err != nil || i < 0 {
		return IndexRecord{}, false, err
	}
	return bt.records[i], true, nil
}

// SearchRecord searches for a record by name and returns its 8-byte heap ID.
//
// Reference: H5Adense.c - H5A__dense_write() searches B-tree by name.
func (bt *WritableBTreeV2) SearchRecord(name string) ([]byte, bool) {
	i, err := bt.findByName(name)
	if err != nil || i < 0 {
		return nil, false
	}
	heapID := make([]byte, 8)
	copy(heapID, bt.records[i].HeapID[:])
	return heapID, true
}

// UpdateRecord updates the heap ID of the record of name.
//
// Reference: H5Adense.c - H5A__dense_write() updates B-tree when size changes.
func (bt *WritableBTreeV2) UpdateRecord(name string, newHeapID uint64) error {
	i, err := bt.findByName(name)
	if err != nil {
		return err
	}
	if i < 0 {
		return fmt.Errorf("record not found for name: %s", name)
	}

	binary.LittleEndian.PutUint64(bt.records[i].HeapID[:], newHeapID)
	if bt.header.Type == BTreeV2TypeLinkNameIndex {
		bt.records[i].HeapID[linkHeapIDLen] = 0
	}
	bt.leaf.Records = bt.records
	return nil
}

// UpdateCreationOrderRecord sets the heap ID of the record of corder in a
// creation order index (type 9).
func (bt *WritableBTreeV2) UpdateCreationOrderRecord(corder uint32, heapID [8]byte) error {
	i, err := bt.findByCreationOrder(corder)
	if err != nil {
		return err
	}
	bt.records[i].HeapID = heapID
	bt.leaf.Records = bt.records
	return nil
}

// DeleteCreationOrderRecord removes the record of corder from a creation
// order index (type 9).
func (bt *WritableBTreeV2) DeleteCreationOrderRecord(corder uint32) error {
	i, err := bt.findByCreationOrder(corder)
	if err != nil {
		return err
	}
	bt.removeAt(i)
	return nil
}

func (bt *WritableBTreeV2) findByCreationOrder(corder uint32) (int, error) {
	if bt.header.Type != BTreeV2TypeAttrCorderIndex {
		return 0, fmt.Errorf("%w: creation order lookup in index type %d", ErrInvalidBTreeType, bt.header.Type)
	}
	for i := range bt.records {
		if bt.records[i].CreationOrder == corder {
			return i, nil
		}
	}
	return 0, fmt.Errorf("creation index %d not in the index", corder)
}

// removeAt removes record i and updates the counts.
func (bt *WritableBTreeV2) removeAt(i int) {
	bt.records = append(bt.records[:i], bt.records[i+1:]...)
	bt.leaf.Records = bt.records
	bt.header.TotalRecords--
	bt.header.NumRecordsRoot--
}

// MigrateToAttributeNameIndex turns an attribute name index that v0.14.2
// and earlier wrote as a link name index (type 5: hash and a 7-byte heap
// ID) into the attribute name index the C library expects (type 8,
// H5Adense.c:211-225). The heap IDs of that writer are at most 6 bytes long
// (fractalheap_write.go: flags, 2-byte offset, 3-byte length), so the
// eighth byte is zero. The records get flags 0 and the untracked creation
// index; their hashes are recomputed from the names (the old hash differed
// from lookup3 for names whose length is a multiple of 12) and they are
// sorted again. The resolver must be set.
func (bt *WritableBTreeV2) MigrateToAttributeNameIndex() error {
	if bt.header.Type != BTreeV2TypeLinkNameIndex {
		return fmt.Errorf("%w: migrate index type %d", ErrInvalidBTreeType, bt.header.Type)
	}
	if bt.resolve == nil {
		return errors.New("migrate attribute name index: no name resolver")
	}
	maxRecords := int((bt.nodeSize - leafOverhead) / attrNameRecordSize)
	if len(bt.records) > maxRecords {
		return fmt.Errorf("%w: %d attribute records do not fit a %d-byte node as type 8",
			ErrBTreeNodeFull, len(bt.records), bt.nodeSize)
	}

	migrated := make([]IndexRecord, len(bt.records))
	for i := range bt.records {
		name, _, err := bt.recordName(i)
		if err != nil {
			return err
		}
		rec := bt.records[i]
		rec.HeapID[linkHeapIDLen] = 0
		rec.Flags = 0
		rec.CreationOrder = CreationOrderUntracked
		rec.NameHash = jenkinsHash(name)
		rec.name = name
		migrated[i] = rec
	}
	sort.SliceStable(migrated, func(a, b int) bool {
		if migrated[a].NameHash != migrated[b].NameHash {
			return migrated[a].NameHash < migrated[b].NameHash
		}
		return migrated[a].name < migrated[b].name
	})
	for i := 1; i < len(migrated); i++ {
		if migrated[i].NameHash == migrated[i-1].NameHash && migrated[i].name == migrated[i-1].name {
			return fmt.Errorf("%w: attribute %q indexed twice", utils.ErrCorrupt, migrated[i].name)
		}
	}

	bt.header.Type = BTreeV2TypeAttrNameIndex
	bt.header.RecordSize = attrNameRecordSize
	bt.leaf.Type = BTreeV2TypeAttrNameIndex
	bt.records = migrated
	bt.leaf.Records = bt.records
	return nil
}

// DeleteRecord removes a record from the B-tree by name.
//
// Deprecated: Use DeleteRecordWithRebalancing for production-quality deletion.
// This method is kept for backward compatibility only.
//
// Reference: H5B2.c - H5B2_remove(), H5Adelete.c - attribute deletion.
func (bt *WritableBTreeV2) DeleteRecord(name string) error {
	// Delegate to rebalancing version (which handles MVP correctly)
	return bt.DeleteRecordWithRebalancing(name)
}

// WriteToFile writes B-tree v2 to file and returns header address.
//
// Writes:
//  1. Leaf node at allocated address
//  2. Header at allocated address (with leaf address)
//
// Returns:
//   - uint64: header address (store this in Link Info Message)
//   - error if write fails
func (bt *WritableBTreeV2) WriteToFile(writer Writer, allocator Allocator, sb *core.Superblock) (uint64, error) {
	if writer == nil || allocator == nil || sb == nil {
		return 0, errors.New("writer, allocator, or superblock is nil")
	}

	// Calculate leaf node size
	// IMPORTANT: For RMW (Read-Modify-Write), allocate FULL node size,
	// not just current content size. This allows leaf to grow without relocation.
	// We'll only write actual data, but reserve full node size for future growth.
	leafSize := uint64(bt.nodeSize) // Allocate FULL node size

	// Allocate space for leaf node
	leafAddr, err := allocator.Allocate(leafSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate leaf node: %w", err)
	}

	// Encode leaf node
	leafData, err := bt.encodeLeafNode(sb)
	if err != nil {
		return 0, fmt.Errorf("failed to encode leaf node: %w", err)
	}

	// Write leaf node
	if err := writer.WriteAtAddress(leafData, leafAddr); err != nil {
		return 0, fmt.Errorf("failed to write leaf node: %w", err)
	}

	// Calculate header size
	headerSize := bt.calculateHeaderSize(sb)

	// Allocate space for header
	headerAddr, err := allocator.Allocate(headerSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate header: %w", err)
	}

	// Set root node address in header
	bt.header.RootNodeAddr = leafAddr

	// Encode header
	headerData, err := bt.encodeHeader(sb)
	if err != nil {
		return 0, fmt.Errorf("failed to encode header: %w", err)
	}

	// Write header
	if err := writer.WriteAtAddress(headerData, headerAddr); err != nil {
		return 0, fmt.Errorf("failed to write header: %w", err)
	}

	return headerAddr, nil
}

// WriteAt writes B-tree v2 in-place at previously loaded addresses.
//
// This method is used for Read-Modify-Write (RMW) scenarios:
// - B-tree was loaded via LoadFromFile()
// - New records were inserted
// - Write back to same addresses
//
// The leaf fits in place: its node was allocated with the full node size
// (WriteToFile here, H5B2__create_leaf in C), and inserts are capped at the
// records that fit a node.
//
// Parameters:
//   - writer: File writer (must implement Writer interface)
//   - sb: Superblock for field sizes
//
// Returns:
//   - error: if write fails or B-tree was not loaded from file
//
// Reference: Same as WriteToFile, but uses stored addresses.
func (bt *WritableBTreeV2) WriteAt(writer Writer, sb *core.Superblock) error {
	if writer == nil || sb == nil {
		return errors.New("writer or superblock is nil")
	}

	// Verify this B-tree was loaded from file
	if bt.loadedHeaderAddress == 0 {
		return errors.New("cannot use WriteAt: B-tree not loaded from file (use WriteToFile for new B-trees)")
	}

	// Encode leaf node
	leafData, err := bt.encodeLeafNode(sb)
	if err != nil {
		return fmt.Errorf("failed to encode leaf node: %w", err)
	}

	// Write leaf node at loaded address
	if err := writer.WriteAtAddress(leafData, bt.loadedLeafAddress); err != nil {
		return fmt.Errorf("failed to write leaf node at 0x%X: %w", bt.loadedLeafAddress, err)
	}

	// Update header with leaf address (in case it was cleared)
	bt.header.RootNodeAddr = bt.loadedLeafAddress

	// Encode header
	headerData, err := bt.encodeHeader(sb)
	if err != nil {
		return fmt.Errorf("failed to encode header: %w", err)
	}

	// Write header at loaded address
	if err := writer.WriteAtAddress(headerData, bt.loadedHeaderAddress); err != nil {
		return fmt.Errorf("failed to write header at 0x%X: %w", bt.loadedHeaderAddress, err)
	}

	return nil
}

// encodeHeader encodes B-tree v2 header for writing.
//
// Format (from H5B2cache.c - H5B2__hdr_serialize): see BTreeV2Header.
//
//nolint:unparam // error reserved for future validation/compression features
func (bt *WritableBTreeV2) encodeHeader(sb *core.Superblock) ([]byte, error) {
	size := bt.calculateHeaderSize(sb)
	buf := make([]byte, size)
	offset := 0

	// Signature (4 bytes)
	copy(buf[offset:], bt.header.Signature[:])
	offset += 4

	// Version (1 byte)
	buf[offset] = bt.header.Version
	offset++

	// Type (1 byte)
	buf[offset] = bt.header.Type
	offset++

	// Node Size (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:], bt.header.NodeSize)
	offset += 4

	// Record Size (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], bt.header.RecordSize)
	offset += 2

	// Depth (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], bt.header.Depth)
	offset += 2

	// Split Percent (1 byte)
	buf[offset] = bt.header.SplitPercent
	offset++

	// Merge Percent (1 byte)
	buf[offset] = bt.header.MergePercent
	offset++

	// Root Node Address (offsetSize bytes)
	writeUint64(buf[offset:], bt.header.RootNodeAddr, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Number of Records in Root (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:], bt.header.NumRecordsRoot)
	offset += 2

	// Total Records (8 bytes)
	binary.LittleEndian.PutUint64(buf[offset:], bt.header.TotalRecords)
	offset += 8

	// Checksum (Jenkins lookup3, 4 bytes)
	checksum := core.JenkinsChecksum(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], checksum)

	return buf, nil
}

// encodeRecord encodes rec in the layout of index type typ into buf.
func encodeRecord(buf []byte, typ uint8, rec *IndexRecord) {
	switch typ {
	case BTreeV2TypeLinkNameIndex:
		// H5Gbtree2.c H5G__dense_btree2_name_encode: hash, 7-byte id.
		binary.LittleEndian.PutUint32(buf, rec.NameHash)
		copy(buf[4:], rec.HeapID[:linkHeapIDLen])
	case BTreeV2TypeAttrNameIndex:
		// H5Abtree2.c:312-326: id, flags, corder, hash.
		copy(buf, rec.HeapID[:])
		buf[attrHeapIDLen] = rec.Flags
		binary.LittleEndian.PutUint32(buf[attrHeapIDLen+1:], rec.CreationOrder)
		binary.LittleEndian.PutUint32(buf[attrHeapIDLen+5:], rec.NameHash)
	case BTreeV2TypeAttrCorderIndex:
		// H5Abtree2.c:449-462: id, flags, corder.
		copy(buf, rec.HeapID[:])
		buf[attrHeapIDLen] = rec.Flags
		binary.LittleEndian.PutUint32(buf[attrHeapIDLen+1:], rec.CreationOrder)
	}
}

// decodeRecord decodes a record of index type typ from buf.
func decodeRecord(buf []byte, typ uint8) IndexRecord {
	var rec IndexRecord
	switch typ {
	case BTreeV2TypeLinkNameIndex:
		rec.NameHash = binary.LittleEndian.Uint32(buf)
		copy(rec.HeapID[:linkHeapIDLen], buf[4:4+linkHeapIDLen])
	case BTreeV2TypeAttrNameIndex:
		copy(rec.HeapID[:], buf[:attrHeapIDLen])
		rec.Flags = buf[attrHeapIDLen]
		rec.CreationOrder = binary.LittleEndian.Uint32(buf[attrHeapIDLen+1:])
		rec.NameHash = binary.LittleEndian.Uint32(buf[attrHeapIDLen+5:])
	case BTreeV2TypeAttrCorderIndex:
		copy(rec.HeapID[:], buf[:attrHeapIDLen])
		rec.Flags = buf[attrHeapIDLen]
		rec.CreationOrder = binary.LittleEndian.Uint32(buf[attrHeapIDLen+1:])
	}
	return rec
}

// encodeLeafNode encodes B-tree v2 leaf node for writing.
//
// Format (from H5B2cache.c - H5B2__leaf_serialize):
//   - Signature: "BTLF" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: same as the header (1 byte)
//   - Records: numRecords records of the type's layout
//   - Checksum: lookup3 (4 bytes)
//
//nolint:unparam // error reserved for future validation/compression features
func (bt *WritableBTreeV2) encodeLeafNode(sb *core.Superblock) ([]byte, error) {
	// The image is the whole node, zero after the checksum, as
	// H5B2__cache_leaf_serialize writes it (H5B2cache.c, "Clear rest of leaf
	// node"). The node is allocated at its full size (WriteToFile); writing
	// only the used bytes left the file short of its end-of-file address
	// when the leaf was the last block, and the C library refuses such a file.
	size := bt.calculateLeafSize(sb)
	if node := uint64(bt.nodeSize); node > size {
		size = node
	}
	buf := make([]byte, size)
	offset := 0

	// Signature (4 bytes)
	copy(buf[offset:], bt.leaf.Signature[:])
	offset += 4

	// Version (1 byte)
	buf[offset] = bt.leaf.Version
	offset++

	// Type (1 byte)
	buf[offset] = bt.leaf.Type
	offset++

	recSize := bt.recordSize()
	for i := range bt.leaf.Records {
		encodeRecord(buf[offset:offset+recSize], bt.header.Type, &bt.leaf.Records[i])
		offset += recSize
	}

	// Checksum (Jenkins lookup3, 4 bytes)
	checksum := core.JenkinsChecksum(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], checksum)

	return buf, nil
}

// recordSize returns the record size of the tree's type.
func (bt *WritableBTreeV2) recordSize() int {
	size, _ := recordSizeOf(bt.header.Type)
	return size
}

// calculateHeaderSize calculates the size of the B-tree v2 header.
func (bt *WritableBTreeV2) calculateHeaderSize(sb *core.Superblock) uint64 {
	return uint64(4 + 1 + 1 + 4 + 2 + 2 + 1 + 1 + int(sb.OffsetSize) + 2 + 8 + 4)
	// Signature + Version + Type + NodeSize + RecordSize + Depth +
	// SplitPercent + MergePercent + RootNodeAddr + NumRecordsRoot + TotalRecords + Checksum
}

// calculateLeafSize calculates the size of the leaf node.
func (bt *WritableBTreeV2) calculateLeafSize(_ *core.Superblock) uint64 {
	return uint64(leafOverhead + len(bt.leaf.Records)*bt.recordSize()) //nolint:gosec // G115: lengths are non-negative
}

// calculateMaxRecords calculates maximum records for a single leaf node.
func (bt *WritableBTreeV2) calculateMaxRecords() int {
	recSize := bt.recordSize()
	if recSize == 0 || bt.nodeSize <= leafOverhead {
		return 0
	}
	return int(bt.nodeSize-leafOverhead) / recSize
}

// insertRecordSorted inserts a record into a sorted slice by hash.
func insertRecordSorted(records []IndexRecord, newRecord IndexRecord) []IndexRecord {
	// Find insertion point
	insertPos := len(records) // Default: append at end

	for i := range records {
		if records[i].NameHash >= newRecord.NameHash {
			insertPos = i
			break
		}
	}

	// Insert at position
	records = append(records, IndexRecord{})
	copy(records[insertPos+1:], records[insertPos:])
	records[insertPos] = newRecord

	return records
}

// compareLinkNames compares two link names lexicographically.
//
// This matches HDF5's comparison in H5G__dense_btree2_name_compare().
// HDF5 uses strcmp(), which performs UTF-8 lexicographic comparison.
//
// Returns:
//   - -1 if a < b
//   - 0 if a == b
//   - 1 if a > b
//
// Reference: H5Gbtree2.c - H5G__dense_btree2_name_compare().
func compareLinkNames(a, b string) int {
	// Go's string comparison is UTF-8 lexicographic (same as strcmp)
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

const (
	// undefinedBTreeV2Addr is HADDR_UNDEF: the root of an empty tree.
	undefinedBTreeV2Addr = ^uint64(0)

	// maxBTreeV2NodeSize caps the node size a loaded header may state. The C
	// library creates 512-byte nodes for dense links and attributes
	// (H5G_NAME_BT2_NODE_SIZE, H5A_NAME_BT2_NODE_SIZE) and 2 KiB nodes for
	// chunk indexes (H5D_BT2_NODE_SIZE); 16 MiB leaves ample room for other
	// writers and bounds the in-memory image of a node rewritten in place.
	maxBTreeV2NodeSize = 16 << 20
)

// checkLeafNodeInFile refuses a header whose leaf node would not lie in the
// file at its full size. The leaf is rewritten in place as a whole node
// (encodeLeafNode), and the C library reads it at node_size and refuses a
// read past the end of the file (H5B2__cache_leaf_get_initial_load_size,
// H5F_block_read). The size is also capped: C's own version 2 B-trees use
// nodes of a few KiB, and the cap bounds the image built in memory even
// inside a large file (a deviation from C, which has no cap).
func checkLeafNodeInFile(r io.ReaderAt, header *BTreeV2Header) error {
	if header.NodeSize > maxBTreeV2NodeSize {
		return fmt.Errorf("%w: B-tree node size %d, more than %d", utils.ErrCorrupt, header.NodeSize, maxBTreeV2NodeSize)
	}
	if header.RootNodeAddr == undefinedBTreeV2Addr {
		return nil
	}
	size, ok := utils.ReaderSize(r)
	if ok && (header.RootNodeAddr > size || uint64(header.NodeSize) > size-header.RootNodeAddr) {
		return fmt.Errorf("%w: B-tree leaf node of %d bytes at 0x%X passes the end of the file at %d",
			utils.ErrCorrupt, header.NodeSize, header.RootNodeAddr, size)
	}
	return nil
}

// LoadFromFile loads an existing B-tree v2 from file.
//
// This implements read-modify-write (RMW) support:
// 1. Read header and leaf from file
// 2. Populate internal structures
// 3. Ready for new record insertion
//
// Parameters:
//   - r: Reader to read from (io.ReaderAt)
//   - headerAddr: Address of B-tree v2 header
//   - sb: Superblock for offset/length sizes
//
// Returns:
//   - error if read fails or validation fails
//
// MVP Limitations:
//   - Single leaf node only (no internal nodes)
//   - Index types 5, 8 and 9 only
func (bt *WritableBTreeV2) LoadFromFile(r io.ReaderAt, headerAddr uint64, sb *core.Superblock) error {
	if r == nil {
		return fmt.Errorf("reader is nil")
	}
	if sb == nil {
		return fmt.Errorf("superblock is nil")
	}

	// 1. Read and decode header
	header, err := readBTreeV2Header(r, headerAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read B-tree header: %w", err)
	}

	// 2. Validate header
	recSize, known := recordSizeOf(header.Type)
	if !known {
		return fmt.Errorf("%w: got %d, want %d, %d or %d", ErrInvalidBTreeType, header.Type,
			BTreeV2TypeLinkNameIndex, BTreeV2TypeAttrNameIndex, BTreeV2TypeAttrCorderIndex)
	}
	if int(header.RecordSize) != recSize {
		return fmt.Errorf("%w: B-tree type %d record size %d, want %d", utils.ErrCorrupt,
			header.Type, header.RecordSize, recSize)
	}

	if header.Depth != 0 {
		return fmt.Errorf("%w: only single-leaf B-trees are supported (depth 0), got depth %d",
			utils.ErrNotSupported, header.Depth)
	}

	// A single leaf holds every record and fits its node (H5B2hdr.c
	// H5B2__hdr_init: node_info[0].max_nrec from the node size).
	if header.NodeSize <= leafOverhead ||
		uint64(header.NumRecordsRoot) > uint64(header.NodeSize-leafOverhead)/uint64(header.RecordSize) ||
		header.TotalRecords != uint64(header.NumRecordsRoot) {
		return fmt.Errorf("%w: B-tree leaf of %d records (total %d) in a %d-byte node", utils.ErrCorrupt,
			header.NumRecordsRoot, header.TotalRecords, header.NodeSize)
	}

	if err := checkLeafNodeInFile(r, header); err != nil {
		return err
	}

	// 3. Store loaded addresses for WriteAt() support (RMW)
	bt.loadedHeaderAddress = headerAddr
	bt.loadedLeafAddress = header.RootNodeAddr

	// 4. Store header
	bt.header = header
	bt.nodeSize = header.NodeSize

	// 5. Read and decode leaf node (if not empty)
	if header.NumRecordsRoot > 0 {
		leaf, records, err := readBTreeV2LeafNode(r, header.RootNodeAddr, int(header.NumRecordsRoot), header.Type, sb)
		if err != nil {
			return fmt.Errorf("failed to read leaf node: %w", err)
		}

		bt.leaf = leaf
		bt.records = records
	} else {
		// Empty tree
		bt.leaf = &BTreeV2LeafNode{
			Signature: [4]byte{'B', 'T', 'L', 'F'},
			Version:   0,
			Type:      header.Type,
			Records:   make([]IndexRecord, 0),
		}
		bt.records = make([]IndexRecord, 0)
	}

	return nil
}

// readBTreeV2Header reads B-tree v2 header from file.
//
// Format (from H5B2cache.c - H5B2__hdr_deserialize): see BTreeV2Header.
func readBTreeV2Header(r io.ReaderAt, address uint64, sb *core.Superblock) (*BTreeV2Header, error) {
	// Calculate header size
	size := 4 + 1 + 1 + 4 + 2 + 2 + 1 + 1 + int(sb.OffsetSize) + 2 + 8 + 4

	// Read header data
	buf := make([]byte, size)
	//nolint:gosec // G115: address conversion, valid for file I/O
	n, err := r.ReadAt(buf, int64(address))
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read header at 0x%X: %w", address, err)
	}
	if n < size {
		return nil, fmt.Errorf("incomplete header read: got %d bytes, want %d", n, size)
	}

	offset := 0

	// Signature (4 bytes)
	var signature [4]byte
	copy(signature[:], buf[offset:offset+4])
	if string(signature[:]) != BTreeV2HeaderSignature {
		return nil, fmt.Errorf("invalid B-tree header signature: got %q, want %q", signature, BTreeV2HeaderSignature)
	}
	offset += 4

	// Version (1 byte)
	version := buf[offset]
	if version != 0 {
		return nil, fmt.Errorf("unsupported B-tree version: %d", version)
	}
	offset++

	// Type (1 byte)
	btreeType := buf[offset]
	offset++

	// Node Size (4 bytes)
	nodeSize := binary.LittleEndian.Uint32(buf[offset : offset+4])
	offset += 4

	// Record Size (2 bytes)
	recordSize := binary.LittleEndian.Uint16(buf[offset : offset+2])
	offset += 2

	// Depth (2 bytes)
	depth := binary.LittleEndian.Uint16(buf[offset : offset+2])
	offset += 2

	// Split Percent (1 byte)
	splitPercent := buf[offset]
	offset++

	// Merge Percent (1 byte)
	mergePercent := buf[offset]
	offset++

	// Root Node Address (offsetSize bytes)
	rootNodeAddr := readUint64(buf[offset:offset+int(sb.OffsetSize)], int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Number of Records in Root (2 bytes)
	numRecordsRoot := binary.LittleEndian.Uint16(buf[offset : offset+2])
	offset += 2

	// Total Records (8 bytes)
	totalRecords := binary.LittleEndian.Uint64(buf[offset : offset+8])
	offset += 8

	// Checksum (Jenkins lookup3, 4 bytes)
	storedChecksum := binary.LittleEndian.Uint32(buf[offset : offset+4])
	expectedChecksum := core.JenkinsChecksum(buf[:offset])
	if storedChecksum != expectedChecksum {
		return nil, fmt.Errorf("b-tree header checksum mismatch: got 0x%X, want 0x%X", storedChecksum, expectedChecksum)
	}

	return &BTreeV2Header{
		Signature:      signature,
		Version:        version,
		Type:           btreeType,
		NodeSize:       nodeSize,
		RecordSize:     recordSize,
		Depth:          depth,
		SplitPercent:   splitPercent,
		MergePercent:   mergePercent,
		RootNodeAddr:   rootNodeAddr,
		NumRecordsRoot: numRecordsRoot,
		TotalRecords:   totalRecords,
	}, nil
}

// readBTreeV2LeafNode reads B-tree v2 leaf node of index type typ from file.
//
// Format (from H5B2cache.c - H5B2__leaf_deserialize): see BTreeV2LeafNode.
func readBTreeV2LeafNode(r io.ReaderAt, address uint64, numRecords int, typ uint8,
	_ *core.Superblock) (*BTreeV2LeafNode, []IndexRecord, error) {
	recSize, known := recordSizeOf(typ)
	if !known {
		return nil, nil, fmt.Errorf("%w: B-tree type %d", ErrInvalidBTreeType, typ)
	}
	size := leafOverhead + numRecords*recSize

	// Read leaf data
	buf := make([]byte, size)
	//nolint:gosec // G115: address conversion, valid for file I/O
	n, err := r.ReadAt(buf, int64(address))
	if err != nil && err != io.EOF {
		return nil, nil, fmt.Errorf("failed to read leaf at 0x%X: %w", address, err)
	}
	if n < size {
		return nil, nil, fmt.Errorf("incomplete leaf read: got %d bytes, want %d", n, size)
	}

	offset := 0

	// Signature (4 bytes)
	var signature [4]byte
	copy(signature[:], buf[offset:offset+4])
	if string(signature[:]) != BTreeV2LeafSignature {
		return nil, nil, fmt.Errorf("invalid B-tree leaf signature: got %q, want %q", signature, BTreeV2LeafSignature)
	}
	offset += 4

	// Version (1 byte)
	version := buf[offset]
	if version != 0 {
		return nil, nil, fmt.Errorf("unsupported B-tree leaf version: %d", version)
	}
	offset++

	// Type (1 byte): H5B2__cache_leaf_deserialize checks it against the header.
	leafType := buf[offset]
	if leafType != typ {
		return nil, nil, fmt.Errorf("%w: B-tree leaf type %d, header type %d", utils.ErrCorrupt, leafType, typ)
	}
	offset++

	records := make([]IndexRecord, numRecords)
	for i := range records {
		records[i] = decodeRecord(buf[offset:offset+recSize], typ)
		offset += recSize
	}

	// Checksum (Jenkins lookup3, 4 bytes)
	storedChecksum := binary.LittleEndian.Uint32(buf[offset : offset+4])
	expectedChecksum := core.JenkinsChecksum(buf[:offset])
	if storedChecksum != expectedChecksum {
		return nil, nil, fmt.Errorf("b-tree leaf checksum mismatch: got 0x%X, want 0x%X", storedChecksum, expectedChecksum)
	}

	leaf := &BTreeV2LeafNode{
		Signature: signature,
		Version:   version,
		Type:      leafType,
		Records:   records,
	}

	return leaf, records, nil
}

// readUint64 reads a uint64 value from buffer with specified size and endianness.
// This is the counterpart to writeUint64 for decoding.
func readUint64(buf []byte, size int, endianness binary.ByteOrder) uint64 {
	switch size {
	case 1:
		return uint64(buf[0])
	case 2:
		return uint64(endianness.Uint16(buf[:2]))
	case 4:
		return uint64(endianness.Uint32(buf[:4]))
	case 8:
		return endianness.Uint64(buf[:8])
	default:
		return 0
	}
}

// GetRecords returns the current list of records in the B-tree.
// This is a read-only accessor for the records slice.
func (bt *WritableBTreeV2) GetRecords() []IndexRecord {
	return bt.records
}

// RemapHeapIDs replaces the heap ID of every record by its entry in remap,
// keyed by the old heap ID's bytes (as WritableFractalHeap.AdoptObjects
// returns it after moving the objects of a legacy heap). The records keep
// their order: name and creation order records are not keyed by heap ID.
// A record whose heap ID is missing from remap is an error, since it would
// point at an object that is no longer where the ID says.
func (bt *WritableBTreeV2) RemapHeapIDs(remap map[string][]byte) error {
	idLen := len(IndexRecord{}.HeapID)
	if bt.Type() == BTreeV2TypeLinkNameIndex {
		idLen = linkHeapIDLen
	}
	for i := range bt.records {
		id, ok := remap[string(bt.records[i].HeapID[:idLen])]
		if !ok {
			return fmt.Errorf("record %d: heap ID %x not among the moved heap objects: %w",
				i, bt.records[i].HeapID[:idLen], utils.ErrCorrupt)
		}
		if len(id) > len(bt.records[i].HeapID) {
			return fmt.Errorf("record %d: new heap ID is %d bytes, record holds %d",
				i, len(id), len(bt.records[i].HeapID))
		}
		var newID [8]byte
		copy(newID[:], id)
		bt.records[i].HeapID = newID
	}
	return nil
}

// jenkinsHash returns the name hash of link and attribute name records:
// H5_checksum_lookup3(name, strlen(name), 0) (H5Gdense.c H5G__dense_insert;
// H5Adense.c:510), ported in core.JenkinsChecksum.
//
// It replaces a separate implementation that processed a final full
// 12-byte block in its main loop and mixed an empty name; H5checksum.c
// leaves the last block (1 to 12 bytes) to the tail switch and returns an
// empty name's seed unmixed, so names whose length is 0 or a multiple of 12
// were hashed differently from the C library.
func jenkinsHash(name string) uint32 {
	return core.JenkinsChecksum([]byte(name))
}
