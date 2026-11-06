package hdf5

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// Corrupt chunk B-trees (review findings H1 and L1). The chunk index is a node
// graph read from the file: before the fix the walk recursed into every child
// with no level check, no visited set and no entry cap, so a child pointing to
// its parent recursed until the Go stack overflowed (a fatal error), and many
// entries pointing to one subtree multiplied the work beyond the file
// (3 levels of 1000 entries: 10^9 entries from ~75 KB). Keys were converted
// to chunk positions without C's checks. The C library rejects all of these:
// H5B__iterate_helper passes the expected child level (H5B.c:1146) and the
// cache refuses a node of another level (H5Bcache.c:173-176);
// H5D__btree_decode_key refuses a key not on a chunk boundary (H5Dbtree.c:646).
//
// The fixture is /x = 0..7 (float64) chunked by 4: a root leaf with two
// entries. A rank-1 dataset has two key coordinates (the element and the
// element-size dimension), so with 8-byte addresses a key is 24 bytes and an
// entry (key + child address) 32 bytes after the 24-byte node header.
const (
	btreeHeaderBytes = 24
	btreeKeyBytes    = 24
	btreeEntryBytes  = btreeKeyBytes + 8
	btreeLeafBytes   = btreeHeaderBytes + 2*btreeEntryBytes + btreeKeyBytes // Two entries and the final key.
)

// chunkIndexFixture is a two-chunk dataset's file image and the address of its
// chunk B-tree root.
type chunkIndexFixture struct {
	raw  []byte
	root int
}

func newChunkIndexFixture(t *testing.T) *chunkIndexFixture {
	t.Helper()

	path := writeFloat64Dataset(t, []uint64{8}, WithChunkDims([]uint64{4}))
	raw, root := chunkIndexRoot(t, path)

	require.EqualValues(t, 0, raw[root+5], "root must be a leaf")
	require.EqualValues(t, 2, binary.LittleEndian.Uint16(raw[root+6:]), "root must have two entries")

	return &chunkIndexFixture{raw: raw, root: root}
}

// chunkIndexRoot returns the file bytes and the chunk B-tree root of /x.
func chunkIndexRoot(t *testing.T, path string) (raw []byte, root int) {
	t.Helper()

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.EqualValues(t, 8, f.Superblock().OffsetSize)
	ds := findDatasetPath(t, f, "/x")
	layout, err := core.ParseDataLayoutMessage(headerMessageData(t, f, ds.Address(), core.MsgDataLayout), f.Superblock())
	require.NoError(t, err)
	require.Equal(t, core.LayoutChunked, layout.Class)

	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	root = int(layout.DataAddress)
	require.Equal(t, "TREE", string(raw[root:root+4]))

	return raw, root
}

// headerMessageData returns a copy of the first message of type typ in the
// object header at addr.
func headerMessageData(t *testing.T, f *File, addr uint64, typ core.MessageType) []byte {
	t.Helper()

	header, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	for _, msg := range header.Messages {
		if msg.Type == typ {
			return append([]byte(nil), msg.Data...)
		}
	}
	t.Fatalf("no message of type %d at 0x%X", typ, addr)

	return nil
}

// appendLeafCopy appends a copy of the original root leaf and returns its address.
func (c *chunkIndexFixture) appendLeafCopy() uint64 {
	addr := uint64(len(c.raw))
	c.raw = append(c.raw, c.raw[c.root:c.root+btreeLeafBytes]...)

	return addr
}

// makeRootInternal turns the root into a node of the given level whose two
// children are at left and right; the keys stay those of the original leaf.
func (c *chunkIndexFixture) makeRootInternal(level uint8, left, right uint64) {
	c.raw[c.root+5] = level
	binary.LittleEndian.PutUint64(c.raw[c.root+btreeHeaderBytes+btreeKeyBytes:], left)
	binary.LittleEndian.PutUint64(c.raw[c.root+btreeHeaderBytes+btreeEntryBytes+btreeKeyBytes:], right)
}

// setKeyCoord sets coordinate dim (a byte offset in the file format) of key i
// of the root.
func (c *chunkIndexFixture) setKeyCoord(i, dim int, offset uint64) {
	at := c.root + btreeHeaderBytes + i*btreeEntryBytes + 8 + dim*8
	binary.LittleEndian.PutUint64(c.raw[at:], offset)
}

func (c *chunkIndexFixture) write(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "corrupt-index.h5")
	require.NoError(t, os.WriteFile(path, c.raw, 0o600))

	return path
}

// requireAllReadsCorrupt checks that every read path that walks the chunk
// index refuses the file with ErrCorrupt and the given reason.
func requireAllReadsCorrupt(t *testing.T, path, want string) {
	t.Helper()

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	_, err = ds.Read()
	require.ErrorIs(t, err, ErrCorrupt, "Read")
	require.Contains(t, err.Error(), want)

	_, err = ds.ReadSlice([]uint64{0}, []uint64{8})
	require.ErrorIs(t, err, ErrCorrupt, "ReadSlice")
	require.Contains(t, err.Error(), want)

	_, err = ds.ChunkIterator()
	require.ErrorIs(t, err, ErrCorrupt, "ChunkIterator")
	require.Contains(t, err.Error(), want)
}

func TestChunkBTree_CorruptStructure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(c *chunkIndexFixture)
		want   string
	}{
		{
			// A DAG: both entries name one subtree, so its chunks are
			// collected twice (and, nested, 1000^levels times).
			name: "two entries share a subtree",
			mutate: func(c *chunkIndexFixture) {
				leaf := c.appendLeafCopy()
				c.makeRootInternal(1, leaf, leaf)
			},
			want: "is reached twice",
		},
		{
			name: "child two levels below its parent",
			mutate: func(c *chunkIndexFixture) {
				c.makeRootInternal(2, c.appendLeafCopy(), c.appendLeafCopy())
			},
			want: "has level 0, its parent",
		},
		{
			// Two distinct leaves of two entries each: four entries for a
			// grid of two chunks.
			name: "more entries than the chunk grid",
			mutate: func(c *chunkIndexFixture) {
				c.makeRootInternal(1, c.appendLeafCopy(), c.appendLeafCopy())
			},
			want: "more than 2 entries",
		},
		{
			name:   "key not on a chunk boundary",
			mutate: func(c *chunkIndexFixture) { c.setKeyCoord(1, 0, 5) },
			want:   "bad coordinate offset 5",
		},
		{
			name:   "key outside the chunk grid",
			mutate: func(c *chunkIndexFixture) { c.setKeyCoord(1, 0, 8) },
			want:   "has coordinate 2 in dimension 0, the grid has 2 chunks",
		},
		{
			// The element-size coordinate is always 0; 8 is on the boundary
			// of the 8-byte element dimension, so only the grid check sees it.
			name:   "nonzero element-size coordinate",
			mutate: func(c *chunkIndexFixture) { c.setKeyCoord(1, 1, 8) },
			want:   "nonzero element-size coordinate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChunkIndexFixture(t)
			tc.mutate(c)
			requireAllReadsCorrupt(t, c.write(t), tc.want)
		})
	}
}

// TestChunkBTree_ChildIsItsParent: a root that names itself as its child.
//
// This test cannot be shown red safely: before the fix the walk recursed until
// the Go stack limit, a fatal error that kills the test process. It was shown
// red on the base commit in WSL under prlimit (task062-fixes report).
func TestChunkBTree_ChildIsItsParent(t *testing.T) {
	c := newChunkIndexFixture(t)
	c.makeRootInternal(1, uint64(c.root), uint64(c.root))
	requireAllReadsCorrupt(t, c.write(t), "is reached twice")
}

// TestChunkBTree_ValidIndexStillReads guards the fixture: the unmodified file
// reads, so every rejection above comes from its corruption.
func TestChunkBTree_ValidIndexStillReads(t *testing.T) {
	c := newChunkIndexFixture(t)

	f, err := Open(c.write(t))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	got, err := findDatasetPath(t, f, "/x").Read()
	require.NoError(t, err)
	require.Equal(t, []float64{0, 1, 2, 3, 4, 5, 6, 7}, got)
}
