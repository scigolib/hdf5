package core

import (
	"fmt"
	"io"
)

// ReadDenseHeapObjects walks a v2 B-tree whose leaf records reference managed
// objects in a fractal heap (the layout used by both dense attribute storage,
// btree record type 8, and dense link storage, btree record type 5) and
// returns the raw heap object bytes in btree leaf order.
//
// The two record types share the same on-disk shape: 4 bytes of name hash
// followed by a 7-byte heap ID. Differentiation between attribute and link
// content happens at the heap-object decoding step (the caller passes the
// bytes to ParseAttributeMessage or structures.ParseLinkMessage accordingly).
//
// Current limitations (sufficient for the MET Norway NORDRAD layout and any
// group / attribute set ≲30 records):
//
//   - Only depth=0 btrees (root is a leaf). Deeper trees would require
//     walking internal-node "BTIN" pointers and aren't on the critical path.
//   - Only managed heap IDs (type bits 0 in heap-ID byte 0). Tiny/huge IDs
//     aren't used for link/attribute records at the sizes we encounter.
func ReadDenseHeapObjects(r io.ReaderAt, btreeAddr, heapAddr uint64, sb *Superblock) ([][]byte, error) {
	btreeHeader, err := readBTreeV2HeaderRaw(r, btreeAddr, sb)
	if err != nil {
		return nil, fmt.Errorf("btree v2 header: %w", err)
	}
	if btreeHeader.Depth != 0 {
		return nil, fmt.Errorf("btree v2 depth %d unsupported (only depth=0 leaf-root)", btreeHeader.Depth)
	}

	heapIDs, err := readBTreeV2LeafRecords(r, btreeHeader.RootNodeAddr, btreeHeader.NumRecordsRoot, sb)
	if err != nil {
		return nil, fmt.Errorf("btree v2 leaf: %w", err)
	}
	if len(heapIDs) == 0 {
		return nil, nil
	}

	heapHeader, err := readFractalHeapHeaderRaw(r, heapAddr, sb)
	if err != nil {
		return nil, fmt.Errorf("fractal heap header: %w", err)
	}

	out := make([][]byte, 0, len(heapIDs))
	for i, hid := range heapIDs {
		off, length, err := parseHeapID(hid, heapHeader)
		if err != nil {
			return nil, fmt.Errorf("heap id %d: %w", i, err)
		}
		data, err := readHeapObject(r, heapHeader.RootBlockAddress, off, length, sb, heapHeader)
		if err != nil {
			return nil, fmt.Errorf("heap object %d: %w", i, err)
		}
		out = append(out, data)
	}
	return out, nil
}
