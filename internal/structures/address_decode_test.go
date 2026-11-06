package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// narrowSB is the superblock of a file with 4-byte addresses and lengths, as
// the C library writes with H5Pset_sizes(fcpl, 4, 4).
func narrowSB() *core.Superblock {
	return &core.Superblock{OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}
}

// TestLoadLocalHeap_NarrowUndefinedDataSegment: with 4-byte addresses the
// undefined data block address is 0xffffffff (H5F_addr_decode_len,
// H5Fint.c:3046-3050), which H5HL__hdr_deserialize rejects
// (H5HLcache.c:200-201). The check used to compare it with the 64-bit value
// only and let the read run to 0xffffffff.
func TestLoadLocalHeap_NarrowUndefinedDataSegment(t *testing.T) {
	buf := []byte("HEAP\x00\x00\x00\x00")
	buf = binary.LittleEndian.AppendUint32(buf, 16) // data segment size
	buf = binary.LittleEndian.AppendUint32(buf, 1)  // free list head: H5HL_FREE_NULL
	buf = append(buf, 0xff, 0xff, 0xff, 0xff)       // data segment address: undefined
	_, err := LoadLocalHeap(bytes.NewReader(buf), 0, narrowSB())
	require.ErrorContains(t, err, "undefined data segment address")
}

// TestParseSymbolTableNode_NarrowUndefinedAddresses: the object header
// address of a soft link's symbol table entry is HADDR_UNDEF (H5Gent.c:130,
// H5G__ent_encode), 0xffffffff with 4-byte addresses; it decodes to the
// undefined address the group code compares with.
func TestParseSymbolTableNode_NarrowUndefinedAddresses(t *testing.T) {
	buf := []byte("SNOD\x01\x00")
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint32(buf, 8)                 // link name offset
	buf = append(buf, 0xff, 0xff, 0xff, 0xff)                      // object header address: undefined
	buf = binary.LittleEndian.AppendUint32(buf, CacheTypeSoftLink) // cache type
	buf = append(buf, make([]byte, 4+16)...)                       // reserved, scratch pad
	node, err := ParseSymbolTableNode(bytes.NewReader(buf), 0, narrowSB())
	require.NoError(t, err)
	require.Len(t, node.Entries, 1)
	require.Equal(t, uint64(8), node.Entries[0].LinkNameOffset)
	require.Equal(t, ^uint64(0), node.Entries[0].ObjectAddress, "soft link: undefined object address")
}
