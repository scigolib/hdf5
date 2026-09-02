package core

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// A B-tree node whose EntriesUsed is 0xFFFF must not panic. Computing the key
// count as EntriesUsed+1 in uint16 arithmetic wraps to 0, producing a
// zero-length Keys slice that the key-decode loop then indexes out of bounds.
func TestParseBTreeV1Node_MaxEntriesUsedNoPanic(t *testing.T) {
	const offsetSize = uint8(8)
	ndims := 1
	chunkDims := []uint64{1}

	headerSize := 4 + 1 + 1 + 2 + int(offsetSize)*2
	keySize := 4 + 4 + ndims*8
	childSize := int(offsetSize)
	entrySize := keySize + childSize
	entries := 0xFFFF
	dataSize := entries*entrySize + keySize

	buf := make([]byte, headerSize+dataSize)
	copy(buf[0:4], "TREE")
	buf[4] = 1 // node type
	buf[5] = 0 // level
	binary.LittleEndian.PutUint16(buf[6:8], uint16(entries))
	// siblings left as zero

	// chunkDims[0] is 1, so byteOffset/1 never divides by zero.
	r := bytes.NewReader(buf)
	node, err := ParseBTreeV1Node(r, 0, offsetSize, ndims, chunkDims)
	if err != nil {
		t.Fatalf("unexpected error parsing max-entries node: %v", err)
	}
	if len(node.Keys) != entries+1 {
		t.Fatalf("expected %d keys, got %d", entries+1, len(node.Keys))
	}
}
