package hdf5_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// Where a version 0 file with 8-byte offsets, as this library writes it,
// keeps its root group (HDF5 File Format Specification, II.A and IV.A.1.a).
const (
	rootHeaderAddressAt = 64  // Superblock: the root entry's object header address.
	rootCacheTypeAt     = 72  // Superblock: the root entry's cache type.
	rootHeaderAt        = 96  // The root group's version 1 object header.
	rootMessageCountAt  = 98  // Its number of header messages.
	rootMessageTypeAt   = 112 // Its first message's type.
	rootMessageSizeAt   = 114 // Its first message's size.

	symbolTableMessage = 0x0011
	symbolTableSize    = 16 // Two 8-byte addresses: the B-tree's and the heap's.
	halfASymbolTable   = 8

	// The same root entry field in a version 0 file with 4-byte offsets:
	// four 4-byte addresses after the 24-byte prefix, then the entry's link
	// name offset and object header address.
	narrowRootCacheTypeAt = 48

	nothingCached = 0 // Cache type 0: the entry's scratch pad holds nothing.
)

// version0File writes a version 0 file holding one dataset and returns its
// bytes, having checked that its root header is where the constants say.
func version0File(t *testing.T) []byte {
	t.Helper()

	name := filepath.Join(t.TempDir(), "v0.h5")

	fw, err := hdf5.CreateForWrite(name, hdf5.CreateTruncate,
		hdf5.WithSuperblockVersion(hdf5.SuperblockV0))
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/values", hdf5.Float64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, dw.Write([]float64{1, 2, 3}))
	require.NoError(t, fw.Close())

	raw, err := os.ReadFile(name)
	require.NoError(t, err)

	order := binary.LittleEndian
	require.Equal(t, uint64(rootHeaderAt), order.Uint64(raw[rootHeaderAddressAt:]), "root header address")
	require.Equal(t, uint16(symbolTableMessage), order.Uint16(raw[rootMessageTypeAt:]), "first message type")
	require.Equal(t, uint16(symbolTableSize), order.Uint16(raw[rootMessageSizeAt:]), "first message size")

	return raw
}

// openBytes writes bytes to a file and opens it.
func openBytes(t *testing.T, raw []byte) (*hdf5.File, error) {
	t.Helper()

	name := filepath.Join(t.TempDir(), "changed.h5")
	require.NoError(t, os.WriteFile(name, raw, 0o600))

	return hdf5.Open(name)
}

// TestOpen_RootSymbolTableMessageCutShort halves the root group's Symbol
// Table message. The message is two addresses; eight bytes cannot hold two
// 8-byte addresses, so the root group's header no longer says where its
// members are. Before the change the message was passed over as if absent.
func TestOpen_RootSymbolTableMessageCutShort(t *testing.T) {
	raw := version0File(t)

	intact, err := openBytes(t, raw)
	require.NoError(t, err)
	require.Len(t, intact.Root().Children(), 1)
	require.NoError(t, intact.Close())

	// The header now declares two messages: the cut one, and what is left
	// of it.
	binary.LittleEndian.PutUint16(raw[rootMessageCountAt:], 2)
	binary.LittleEndian.PutUint16(raw[rootMessageSizeAt:], halfASymbolTable)

	uncached := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint32(uncached[rootCacheTypeAt:], nothingCached)

	tests := []struct {
		name string
		raw  []byte
	}{
		// Before: opened from the addresses cached in the superblock's root
		// entry, with its 1 member. The cache stands in for a message that
		// is absent, not for a damaged one.
		{"cache intact", raw},
		// Before: nothing to fall back on, so the root opened with 0
		// members and no error.
		{"nothing cached", uncached},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := openBytes(t, tt.raw)
			require.ErrorIs(t, err, hdf5.ErrCorrupt)
			require.Nil(t, file)
		})
	}
}

// TestOpen_FourByteOffsetsRootFromItsMessage opens a file the C library wrote
// with 4-byte offsets, with its root entry's cache cleared (cache type 0,
// which the format allows). Its Symbol Table message is 8 bytes, two 4-byte
// addresses, and is then the only place that says where the root's members
// are. Before the change a message under 16 bytes was passed over, and this
// valid file opened with an empty root group.
func TestOpen_FourByteOffsetsRootFromItsMessage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "reference", "tsizeslheap.h5"))
	require.NoError(t, err)

	unchanged, err := openBytes(t, raw)
	require.NoError(t, err)
	require.Equal(t, uint8(4), unchanged.Superblock().OffsetSize, "the fixture has 4-byte offsets")
	members := len(unchanged.Root().Children())
	require.NotZero(t, members)
	require.NoError(t, unchanged.Close())

	require.Equal(t, uint32(1), binary.LittleEndian.Uint32(raw[narrowRootCacheTypeAt:]),
		"the fixture's root entry caches its symbol table")
	binary.LittleEndian.PutUint32(raw[narrowRootCacheTypeAt:], nothingCached)

	file, err := openBytes(t, raw)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	require.Len(t, file.Root().Children(), members)
}
