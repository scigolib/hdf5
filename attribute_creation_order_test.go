package hdf5

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// torderattrFile has objects whose version 2 object headers track attribute
// creation order (H5O_HDR_ATTR_CRT_ORDER_TRACKED, not indexed): /dt (a
// dataset) and /gt (a group), each with the compact attributes c, b, a
// created in that order.
const torderattrFile = "testdata/hdf5_official/torderattr.h5"

// creationOrderAttrs runs h5dump --sort_by=creation_order on the object at
// path (kind "-d" or "-g") and returns its output and the attribute names in
// the order listed; "" and nil when h5dump is not installed.
func creationOrderAttrs(t *testing.T, file, kind, path string) (string, []string) {
	t.Helper()
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Log("h5dump not in PATH: creation order check skipped")
		return "", nil
	}
	out, runErr := runH5Err(h5dump, "--sort_by=creation_order", kind, path, file)
	require.NoError(t, runErr, "h5dump:\n%s", out)
	require.NotContains(t, out, "error")
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if _, rest, ok := strings.Cut(line, "ATTRIBUTE \""); ok {
			name, _, _ := strings.Cut(rest, "\"")
			names = append(names, name)
		}
	}
	return out, names
}

// requireCreationOrder checks the creation order and the int32 values that
// h5dump lists for the object at path.
func requireCreationOrder(t *testing.T, file, kind, path string, order []string, values map[string]int32) {
	t.Helper()
	requireH5dumpClean(t, file)
	out, names := creationOrderAttrs(t, file, kind, path)
	if out == "" {
		return
	}
	require.Equal(t, order, names, "attributes in creation order:\n%s", out)
	for name, v := range values {
		require.Contains(t, h5dumpAttrData(t, out, name), fmt.Sprintf("(0): %d", v), "value of %s", name)
	}
}

// objectAttrInfo reads the attribute info message of the object header at
// addr.
func objectAttrInfo(t *testing.T, file string, addr uint64) (*core.ObjectHeader, *core.AttributeInfoMessage) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	oh, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	msg := attributeInfoMessage(oh)
	require.NotNil(t, msg, "no attribute info message")
	ainfo, err := core.ParseAttributeInfoMessage(msg.Data, f.Superblock())
	require.NoError(t, err)
	return oh, ainfo
}

// torderattrObject returns the object header address of /dt or /gt.
func torderattrObject(t *testing.T, file, name string) uint64 {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var addr uint64
	f.Walk(func(path string, obj Object) {
		if strings.Trim(path, "/") != name {
			return
		}
		switch o := obj.(type) {
		case *Dataset:
			addr = o.Address()
		case *Group:
			addr = o.address
		}
	})
	require.NotZero(t, addr, "%s not found", name)
	return addr
}

// TestAttributes_TrackedCreationOrder: on an object header that tracks
// attribute creation order, every message header carries a 2-byte creation
// index (H5Opkg.h:112-122, H5Omessage.c:1969-1974). A new attribute takes the
// next creation index of the attribute info message, which is incremented
// (H5Oattribute.c:283-295), so the C library lists the attributes in the order
// they were created, compact and, past the threshold, dense (H5Oattribute.c:
// 258-275 moves the compact ones with their creation indexes).
func TestAttributes_TrackedCreationOrder(t *testing.T) {
	cases := []struct {
		name, kind string
		writer     func(fw *FileWriter, addr uint64) (func(string, interface{}) error, error)
	}{
		{"dt", "-d", func(fw *FileWriter, _ uint64) (func(string, interface{}) error, error) {
			ds, err := fw.OpenDataset("/dt")
			if err != nil {
				return nil, err
			}
			return ds.WriteAttribute, nil
		}},
		{"gt", "-g", func(fw *FileWriter, addr uint64) (func(string, interface{}) error, error) {
			g := &GroupWriter{path: "/gt", headerAddr: addr, file: fw}
			return g.WriteAttribute, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := copyFixture(t, torderattrFile)
			path := "/" + tc.name
			addr := torderattrObject(t, file, tc.name)
			requireCreationOrder(t, file, tc.kind, path, []string{"c", "b", "a"}, nil)

			// A compact attribute: creation index 3.
			fw, err := OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			write, err := tc.writer(fw, addr)
			require.NoError(t, err)
			require.NoError(t, write("d", int32(40)))
			require.NoError(t, fw.Close())

			oh, ainfo := objectAttrInfo(t, file, addr)
			require.Equal(t, uint8(ainfoTrackCorder), ainfo.Flags)
			require.Equal(t, uint64(4), ainfo.MaxCreationIndex, "next creation index")
			var crt []uint16
			for _, m := range oh.Messages {
				if m.Type == core.MsgAttribute {
					crt = append(crt, m.CreationIndex)
				}
			}
			require.Equal(t, []uint16{0, 1, 2, 3}, crt, "creation indexes of the compact attributes")
			order := make([]string, 0, 10)
			order = append(order, "c", "b", "a", "d")
			values := map[string]int32{"d": 40}
			requireCreationOrder(t, file, tc.kind, path, order, values)

			// Past the dense threshold: the attributes move to dense storage
			// with their creation indexes.
			fw, err = OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			write, err = tc.writer(fw, addr)
			require.NoError(t, err)
			for i, n := range []string{"e", "f", "g", "h", "i", "j"} {
				v := int32(50 + 10*i)
				require.NoError(t, write(n, v))
				order = append(order, n)
				values[n] = v
			}
			require.NoError(t, write("b", int32(-2))) // rewritten: keeps creation index 1
			values["b"] = -2
			require.NoError(t, fw.Close())

			_, ainfo = objectAttrInfo(t, file, addr)
			require.True(t, ainfo.HasDenseStorage(8), "dense storage")
			require.Equal(t, uint8(ainfoTrackCorder), ainfo.Flags)
			require.Equal(t, uint64(len(order)), ainfo.MaxCreationIndex, "next creation index")
			requireCreationOrder(t, file, tc.kind, path, order, values)
		})
	}
}

// TestAttributes_IndexedCreationOrderToDense: an object that tracks and
// indexes creation order gets, when its compact attributes move to dense
// storage, a creation order index (B-tree type 9) next to the name index
// (H5Adense.c:227-241), whose records carry the creation indexes the compact
// attributes had (H5O__attr_to_dense_cb, H5Adense.c:505-530).
func TestAttributes_IndexedCreationOrderToDense(t *testing.T) {
	first := []string{"x", "y", "z"}
	// Three more attributes, deleted again, leave chunk 0 the room that the
	// creation indexes and the attribute info message take.
	file := writeDenseAttrFile(t, SuperblockV2, append(slices.Clone(first), "p0", "p1", "p2"))
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	dw, err := fw.OpenDataset("/d")
	require.NoError(t, err)
	for _, n := range []string{"p0", "p1", "p2"} {
		require.NoError(t, dw.DeleteAttribute(n))
	}
	require.NoError(t, fw.Close())

	f, err := Open(file)
	require.NoError(t, err)
	ds := findDataset(f, "/d")
	require.NotNil(t, ds)
	addr := ds.Address()
	sb := f.Superblock()
	require.NoError(t, f.Close())

	ainfo, err := core.EncodeAttributeInfoMessage(&core.AttributeInfoMessage{
		Flags:               ainfoTrackCorder | ainfoIndexCorder,
		MaxCreationIndex:    uint64(len(first)),
		FractalHeapAddr:     undefinedAddress,
		BTreeNameIndexAddr:  undefinedAddress,
		BTreeOrderIndexAddr: undefinedAddress,
	}, sb)
	require.NoError(t, err)
	trackCreationOrderHeader(t, file, addr, ainfo)
	values := denseAttrValues(first)
	requireCreationOrder(t, file, "-d", "/d", first, values)

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	dw, err = fw.OpenDataset("/d")
	require.NoError(t, err)
	order := slices.Clone(first)
	for i, n := range []string{"w", "v", "u", "t", "s", "r"} {
		v := int32(100 + i)
		require.NoError(t, dw.WriteAttribute(n, v))
		order = append(order, n)
		values[n] = v
	}
	require.NoError(t, fw.Close())

	tree := readNameIndex(t, file)
	ai := tree.ainfo.Data
	require.Equal(t, byte(ainfoTrackCorder|ainfoIndexCorder), ai[1], "attribute info flags")
	require.Equal(t, uint16(len(order)), binary.LittleEndian.Uint16(ai[2:]), "next creation index")
	require.Len(t, tree.records, len(order))
	ids := make(map[uint32][]byte, len(order))
	for _, rec := range tree.records {
		ids[binary.LittleEndian.Uint32(rec[9:])] = rec[:8]
	}

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	byOrder := decodeIndexTree(t, raw, readAddressLE(ai[4+16:4+24]))
	require.Equal(t, uint8(9), byOrder.typ, "B-tree type: attribute creation order index")
	require.Len(t, byOrder.records, len(order))
	for i, rec := range byOrder.records {
		c := binary.LittleEndian.Uint32(rec[9:])
		require.Equal(t, uint32(i), c, "record %d creation index", i)
		require.Equal(t, ids[c], rec[:8], "record %d heap ID matches the name index", i)
	}
	requireCreationOrder(t, file, "-d", "/d", order, values)
}
