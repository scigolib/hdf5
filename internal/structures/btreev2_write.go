// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/scigolib/hdf5/internal/core"
)

// writeUint64 writes a uint64 value to buffer with specified size and endianness.
// This is a helper function for encoding fields with variable sizes.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value)
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

	BTreeV2TypeLinkNameIndex = uint8(5) // Type 5 = Link Name Index for dense groups

	DefaultBTreeV2NodeSize     = uint32(4096) // 4KB default node size
	DefaultBTreeV2SplitPercent = uint8(100)   // Split at 100% full
	DefaultBTreeV2MergePercent = uint8(40)    // Merge at 40% full
)

// B-tree v2 error definitions.
var (
	ErrBTreeNodeFull    = errors.New("b-tree node is full")
	ErrInvalidBTreeType = errors.New("invalid b-tree type")
	ErrInvalidNodeSize  = errors.New("invalid node size")
)

// BTreeV2Header represents B-tree v2 header structure.
//
// Format (from H5B2hdr.c and HDF5 Format Spec III.A.2):
//   - Signature: "BTHD" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: 5 = Link Name Index (1 byte)
//   - Node Size: Typically 4096 bytes (4 bytes)
//   - Record Size: Variable (link name record) (2 bytes)
//   - Depth: 0 for single leaf (2 bytes)
//   - Split Percent: 100 (1 byte)
//   - Merge Percent: 40 (1 byte)
//   - Root Node Address: offsetSize bytes
//   - Number of Records in Root: 2 bytes
//   - Total Records: 8 bytes
//   - Checksum: CRC32 (4 bytes)
//
// Reference:
//   - HDF5 Format Spec: Section III.A.2 (B-tree v2)
//   - C Library: H5B2hdr.c - H5B2_hdr_t structure
//   - Serialization: H5B2cache.c - H5B2__hdr_serialize()
type BTreeV2Header struct {
	Signature      [4]byte // "BTHD"
	Version        uint8   // 0
	Type           uint8   // 5 = Link Name Index
	NodeSize       uint32  // Node size in bytes
	RecordSize     uint16  // Size of record (11 bytes for link name records)
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
//   - Type: 5 = Link Name Index (1 byte)
//   - Records: Array of link name records
//   - Checksum: CRC32 (4 bytes)
//
// Reference:
//   - HDF5 Format Spec: Section III.A.2 (B-tree v2)
//   - C Library: H5B2leaf.c - H5B2_leaf_t structure
//   - Serialization: H5B2cache.c - H5B2__leaf_serialize()
type BTreeV2LeafNode struct {
	Signature [4]byte          // "BTLF"
	Version   uint8            // 0
	Type      uint8            // 5
	Records   []LinkNameRecord // Link records
}

// LinkNameRecord represents a link name index record in B-tree v2.
//
// Format (from H5Gbtree2.c - H5G_dense_btree2_name_rec_t):
//   - Name Hash: uint32 (Jenkins hash of link name)
//   - Heap ID: 7 bytes (fractal heap object ID)
//
// The heap ID points to a link message stored in the fractal heap.
// HDF5 uses a 7-byte heap ID in the B-tree (vs 8 bytes in the heap itself).
//
// Reference:
//   - C Library: H5Gbtree2.c - H5G_dense_btree2_name_rec_t
//   - Encoding: H5Gbtree2.c - H5G__dense_btree2_name_encode()
type LinkNameRecord struct {
	NameHash uint32  // Jenkins hash of link name
	HeapID   [7]byte // Fractal heap ID (7 bytes, not 8)
}

// WritableBTreeV2 manages B-tree v2 construction for link name indexing.
//
// MVP Limitations:
//   - Single leaf node only (no internal nodes)
//   - No splits (error if node size exceeded)
//   - Records sorted by name hash
//   - Maximum records = (nodeSize - overhead) / recordSize
type WritableBTreeV2 struct {
	header   *BTreeV2Header
	leaf     *BTreeV2LeafNode
	records  []LinkNameRecord
	nodeSize uint32
}

// NewWritableBTreeV2 creates a new B-tree v2 for link name indexing.
//
// Parameters:
//   - nodeSize: size of the B-tree node in bytes (typically 4096)
//
// Returns:
//   - *WritableBTreeV2: B-tree structure ready for record insertion
func NewWritableBTreeV2(nodeSize uint32) *WritableBTreeV2 {
	if nodeSize == 0 {
		nodeSize = DefaultBTreeV2NodeSize
	}

	return &WritableBTreeV2{
		header: &BTreeV2Header{
			Signature:      [4]byte{'B', 'T', 'H', 'D'},
			Version:        0,
			Type:           BTreeV2TypeLinkNameIndex,
			NodeSize:       nodeSize,
			RecordSize:     11, // 4 bytes hash + 7 bytes heap ID
			Depth:          0,  // Single leaf
			SplitPercent:   DefaultBTreeV2SplitPercent,
			MergePercent:   DefaultBTreeV2MergePercent,
			RootNodeAddr:   0, // Set when writing
			NumRecordsRoot: 0,
			TotalRecords:   0,
		},
		leaf: &BTreeV2LeafNode{
			Signature: [4]byte{'B', 'T', 'L', 'F'},
			Version:   0,
			Type:      BTreeV2TypeLinkNameIndex,
			Records:   make([]LinkNameRecord, 0),
		},
		records:  make([]LinkNameRecord, 0),
		nodeSize: nodeSize,
	}
}

// InsertRecord adds a link name record to the B-tree.
//
// Parameters:
//   - linkName: name of the link (for hash calculation)
//   - heapID: 8-byte fractal heap object ID (we store 7 bytes)
//
// For MVP: stores in single leaf, sorted by name hash.
//
// Returns:
//   - error if node is full or insertion fails
func (bt *WritableBTreeV2) InsertRecord(linkName string, heapID uint64) error {
	// Calculate Jenkins hash for link name
	hash := jenkinsHash(linkName)

	// Convert 8-byte heap ID to 7-byte format
	// HDF5 stores heap IDs as 7 bytes in B-tree (first 7 bytes of the ID)
	var heapIDBytes [7]byte
	var temp [8]byte
	binary.LittleEndian.PutUint64(temp[:], heapID)
	copy(heapIDBytes[:], temp[:7])

	record := LinkNameRecord{
		NameHash: hash,
		HeapID:   heapIDBytes,
	}

	// Check if node will be full
	maxRecords := bt.calculateMaxRecords()
	if len(bt.records) >= maxRecords {
		return ErrBTreeNodeFull
	}

	// Insert sorted by hash
	bt.records = insertRecordSorted(bt.records, record)
	bt.header.TotalRecords++
	bt.header.NumRecordsRoot++
	bt.leaf.Records = bt.records

	return nil
}

// HasKey checks if a key (link/attribute name) exists in the B-tree.
//
// Parameters:
//   - name: name to check
//
// Returns:
//   - bool: true if name exists (hash found), false otherwise
//
// For MVP: searches single leaf node by name hash.
func (bt *WritableBTreeV2) HasKey(name string) bool {
	hash := jenkinsHash(name)

	// Search in records
	for _, record := range bt.records {
		if record.NameHash == hash {
			return true
		}
	}

	return false
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
	leafSize := bt.calculateLeafSize(sb)

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

// encodeHeader encodes B-tree v2 header for writing.
//
// Format (from H5B2cache.c - H5B2__hdr_serialize):
//   - Signature: "BTHD" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: 5 (1 byte)
//   - Node Size: 4 bytes
//   - Record Size: 2 bytes
//   - Depth: 2 bytes
//   - Split Percent: 1 byte
//   - Merge Percent: 1 byte
//   - Root Node Address: offsetSize bytes
//   - Number of Records in Root: 2 bytes
//   - Total Records: 8 bytes
//   - Checksum: CRC32 (4 bytes)
//
//nolint:unparam // error return reserved for future validation
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

	// Checksum (CRC32, 4 bytes)
	checksum := crc32.ChecksumIEEE(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], checksum)

	return buf, nil
}

// encodeLeafNode encodes B-tree v2 leaf node for writing.
//
// Format (from H5B2cache.c - H5B2__leaf_serialize):
//   - Signature: "BTLF" (4 bytes)
//   - Version: 0 (1 byte)
//   - Type: 5 (1 byte)
//   - Records: Array of link name records (numRecords * 11 bytes)
//   - Checksum: CRC32 (4 bytes)
//
//nolint:unparam // error return reserved for future validation
func (bt *WritableBTreeV2) encodeLeafNode(sb *core.Superblock) ([]byte, error) {
	size := bt.calculateLeafSize(sb)
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

	// Records (each record is 11 bytes: 4 bytes hash + 7 bytes heap ID)
	for _, record := range bt.leaf.Records {
		// Name Hash (4 bytes)
		binary.LittleEndian.PutUint32(buf[offset:], record.NameHash)
		offset += 4

		// Heap ID (7 bytes)
		copy(buf[offset:], record.HeapID[:])
		offset += 7
	}

	// Checksum (CRC32, 4 bytes)
	checksum := crc32.ChecksumIEEE(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], checksum)

	return buf, nil
}

// calculateHeaderSize calculates the size of the B-tree v2 header.
func (bt *WritableBTreeV2) calculateHeaderSize(sb *core.Superblock) uint64 {
	//nolint:gosec // G115: size calculation, overflow not possible with valid HDF5 file
	return uint64(4 + 1 + 1 + 4 + 2 + 2 + 1 + 1 + int(sb.OffsetSize) + 2 + 8 + 4)
	// Signature + Version + Type + NodeSize + RecordSize + Depth +
	// SplitPercent + MergePercent + RootNodeAddr + NumRecordsRoot + TotalRecords + Checksum
}

// calculateLeafSize calculates the size of the leaf node.
func (bt *WritableBTreeV2) calculateLeafSize(_ *core.Superblock) uint64 {
	numRecords := len(bt.leaf.Records)
	//nolint:gosec // G115: size calculation, overflow not possible with valid record counts
	return uint64(4 + 1 + 1 + (numRecords * 11) + 4)
	// Signature + Version + Type + Records + Checksum
}

// calculateMaxRecords calculates maximum records for a single leaf node.
func (bt *WritableBTreeV2) calculateMaxRecords() int {
	// Node size - overhead (signature + version + type + checksum)
	overhead := uint32(4 + 1 + 1 + 4) // 10 bytes
	available := bt.nodeSize - overhead
	recordSize := uint32(11) // 4 bytes hash + 7 bytes heap ID
	return int(available / recordSize)
}

// insertRecordSorted inserts a record into a sorted slice by hash.
func insertRecordSorted(records []LinkNameRecord, newRecord LinkNameRecord) []LinkNameRecord {
	// Find insertion point
	i := 0
	for i < len(records) && records[i].NameHash < newRecord.NameHash {
		i++
	}

	// Insert at position i
	records = append(records, LinkNameRecord{})
	copy(records[i+1:], records[i:])
	records[i] = newRecord

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

// jenkinsHash computes Jenkins hash (lookup3) for a string.
//
// This is the hash function used by HDF5 for link name indexing.
// Based on Bob Jenkins' lookup3 hash algorithm.
//
// Reference:
//   - H5checksum.c - H5_checksum_lookup3()
//   - http://burtleburtle.net/bob/hash/doobs.html
func jenkinsHash(name string) uint32 {
	// Jenkins lookup3 hash implementation
	// This is a simplified version for MVP; full implementation matches C library

	length := len(name)
	a, b, c := uint32(0xdeadbeef)+uint32(length), uint32(0xdeadbeef)+uint32(length), uint32(0xdeadbeef)+uint32(length) //nolint:gosec // G115: Jenkins hash algorithm, length is string length

	// Process 12-byte chunks
	i := 0
	for i+12 <= length {
		a += uint32(name[i]) | uint32(name[i+1])<<8 | uint32(name[i+2])<<16 | uint32(name[i+3])<<24
		b += uint32(name[i+4]) | uint32(name[i+5])<<8 | uint32(name[i+6])<<16 | uint32(name[i+7])<<24
		c += uint32(name[i+8]) | uint32(name[i+9])<<8 | uint32(name[i+10])<<16 | uint32(name[i+11])<<24

		// Mix
		a -= c
		a ^= (c << 4) | (c >> 28)
		c += b
		b -= a
		b ^= (a << 6) | (a >> 26)
		a += c
		c -= b
		c ^= (b << 8) | (b >> 24)
		b += a
		a -= c
		a ^= (c << 16) | (c >> 16)
		c += b
		b -= a
		b ^= (a << 19) | (a >> 13)
		a += c
		c -= b
		c ^= (b << 4) | (b >> 28)
		b += a

		i += 12
	}

	// Handle remaining bytes
	remaining := length - i
	switch remaining {
	case 11:
		c += uint32(name[i+10]) << 16
		fallthrough
	case 10:
		c += uint32(name[i+9]) << 8
		fallthrough
	case 9:
		c += uint32(name[i+8])
		fallthrough
	case 8:
		b += uint32(name[i+7]) << 24
		fallthrough
	case 7:
		b += uint32(name[i+6]) << 16
		fallthrough
	case 6:
		b += uint32(name[i+5]) << 8
		fallthrough
	case 5:
		b += uint32(name[i+4])
		fallthrough
	case 4:
		a += uint32(name[i+3]) << 24
		fallthrough
	case 3:
		a += uint32(name[i+2]) << 16
		fallthrough
	case 2:
		a += uint32(name[i+1]) << 8
		fallthrough
	case 1:
		a += uint32(name[i])
	}

	// Final mix
	c ^= b
	c -= (b << 14) | (b >> 18)
	a ^= c
	a -= (c << 11) | (c >> 21)
	b ^= a
	b -= (a << 25) | (a >> 7)
	c ^= b
	c -= (b << 16) | (b >> 16)
	a ^= c
	a -= (c << 4) | (c >> 28)
	b ^= a
	b -= (a << 14) | (a >> 18)
	c ^= b
	c -= (b << 24) | (b >> 8)

	return c
}
