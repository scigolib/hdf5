package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// boundsHeapDirectBlock builds a minimal 128-byte, checksum-free FHDB direct
// block at the start of the buffer. The block offset is zero and the
// managed-object data area starts right after the header.
func boundsHeapDirectBlock() []byte {
	data := make([]byte, 128)
	copy(data[0:4], "FHDB")
	data[4] = 0                                       // version
	binary.LittleEndian.PutUint64(data[5:13], 0x1000) // heap header address
	binary.LittleEndian.PutUint32(data[13:17], 0)     // block offset (heapOffsetSize = 4)

	return data
}

// boundsHeapHeader returns a heap header whose offset/length fields are 4 bytes
// wide (so a 7-byte heap ID decodes) and whose object-size bounds are given.
func boundsHeapHeader(maxManagedObjSize uint32, maxDirectBlockSize uint64) *fractalHeapHeaderRaw {
	return &fractalHeapHeaderRaw{
		HeapIDLen:          7,
		MaxManagedObjSize:  maxManagedObjSize,
		MaxDirectBlockSize: maxDirectBlockSize,
		HeapOffsetSize:     4,
		HeapLengthSize:     4,
	}
}

// TestReadHeapObjectRejectsLengthBeyondMaxManagedObject is the ADR-003
// regression test for attribute.go readHeapObject: the object length comes from
// the heap ID, and before the fix it was allocated without any check. A
// managed object cannot be larger than MaxManagedObjSize from the heap header
// (H5HFpkg.h, H5HF_MAN_OBJ_MAX_SIZE), so the header is the authoritative bound.
func TestReadHeapObjectRejectsLengthBeyondMaxManagedObject(t *testing.T) {
	reader := bytes.NewReader(boundsHeapDirectBlock())
	header := boundsHeapHeader(4<<20, 16<<20) // 4 MB managed objects, 16 MB blocks
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := readHeapObject(reader, 0, 0, 8<<20, sb, header)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds MaxManagedObjSize",
		"heap ID length must be bounded by the heap header before allocation, got: %v", err)
}

// TestReadHeapObjectRejectsLengthBeyondBlock covers the second header bound:
// an object cannot be larger than the direct block that holds it.
func TestReadHeapObjectRejectsLengthBeyondBlock(t *testing.T) {
	reader := bytes.NewReader(boundsHeapDirectBlock())
	header := boundsHeapHeader(16<<20, 1024) // blocks are 1 KB
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := readHeapObject(reader, 0, 0, 4096, sb, header)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds MaxDirectBlockSize",
		"heap ID length must fit the direct block before allocation, got: %v", err)
}

// TestReadHeapObjectRejectsLengthBeyondReader covers the case the heap header
// bounds allow but the file does not: the read is validated against the reader
// size before the buffer is allocated.
func TestReadHeapObjectRejectsLengthBeyondReader(t *testing.T) {
	reader := bytes.NewReader(boundsHeapDirectBlock())
	header := boundsHeapHeader(16<<20, 16<<20)
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := readHeapObject(reader, 0, 0, 1<<20, sb, header)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file",
		"heap object read must be validated against the reader, got: %v", err)
}

// TestReadHeapObjectValidObjectUnchanged makes sure the bounds do not reject a
// well-formed object: same layout, a length that fits both the header bounds
// and the file.
func TestReadHeapObjectValidObjectUnchanged(t *testing.T) {
	block := boundsHeapDirectBlock()
	copy(block[17:], "dense-attribute")
	reader := bytes.NewReader(block)
	header := boundsHeapHeader(16<<20, 16<<20)
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := readHeapObject(reader, 0, 0, 15, sb, header)
	require.NoError(t, err)
	require.Equal(t, "dense-attribute", string(data))
}
