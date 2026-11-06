package hdf5

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/stretchr/testify/require"
)

// denseAttrNames are the names written by the dense attribute tests. The
// 12-byte name exercises the lookup3 tail that is processed by the last
// "case 12" of H5_checksum_lookup3 (H5checksum.c), not by the main loop.
var denseAttrNames = []string{
	"a00", "a01", "a02", "a03", "a04", "a05", "a06", "a07", "a08", "a09",
	"abcdefghijkl", "temperature_offset",
}

// nameIndexTree is the decoded single-leaf v2 B-tree of an attribute name
// index, as stored in the file.
type nameIndexTree struct {
	objAddr    uint64             // object header of the dataset
	ohVersion  uint8              // its version
	ainfo      core.HeaderMessage // its attribute info message
	headerAddr uint64
	typ        uint8
	recordSize int
	depth      uint16
	rootAddr   uint64
	records    [][]byte // raw records of the root leaf
}

// readNameIndex decodes the B-tree v2 header and root leaf that the
// Attribute Info message of the dataset /d (writeDenseAttrFile) points at.
func readNameIndex(t *testing.T, file string) nameIndexTree {
	t.Helper()
	const path = "/d"
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	ds := findDataset(f, path)
	require.NotNil(t, ds, "dataset %s not found", path)
	oh, err := core.ReadObjectHeader(f.Reader(), ds.Address(), f.Superblock())
	require.NoError(t, err)

	msg := attributeInfoMessage(oh)
	require.NotNil(t, msg, "%s has no Attribute Info message", path)
	ainfo, err := core.ParseAttributeInfoMessage(msg.Data, f.Superblock())
	require.NoError(t, err)

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, uint8(8), f.Superblock().OffsetSize, "the writer uses 8-byte offsets")

	tree := decodeIndexTree(t, raw, ainfo.BTreeNameIndexAddr)
	tree.objAddr, tree.ohVersion, tree.ainfo = ds.Address(), oh.Version, *msg
	return tree
}

// decodeIndexTree decodes the single-leaf v2 B-tree whose header is at addr
// in a file with 8-byte offsets.
func decodeIndexTree(t *testing.T, raw []byte, addr uint64) nameIndexTree {
	t.Helper()
	const osz = 8

	// H5B2cache.c H5B2__cache_hdr_serialize: signature, version, type, node
	// size(4), record size(2), depth(2), split, merge, root address, root
	// record count(2), total records(8), checksum.
	hdr := raw[addr:]
	require.Equal(t, "BTHD", string(hdr[:4]))
	hdrLen := 4 + 1 + 1 + 4 + 2 + 2 + 1 + 1 + osz + 2 + 8
	require.Equal(t, core.JenkinsChecksum(hdr[:hdrLen]), binary.LittleEndian.Uint32(hdr[hdrLen:]), "header checksum")

	tree := nameIndexTree{
		headerAddr: addr,
		typ:        hdr[5],
		recordSize: int(binary.LittleEndian.Uint16(hdr[10:])),
		depth:      binary.LittleEndian.Uint16(hdr[12:]),
	}
	tree.rootAddr = readAddressLE(hdr[16 : 16+osz])
	nrec := int(binary.LittleEndian.Uint16(hdr[16+osz:]))

	leaf := raw[tree.rootAddr:]
	require.Equal(t, "BTLF", string(leaf[:4]))
	require.Equal(t, tree.typ, leaf[5], "leaf type")
	leafLen := 6 + nrec*tree.recordSize
	require.Equal(t, core.JenkinsChecksum(leaf[:leafLen]), binary.LittleEndian.Uint32(leaf[leafLen:]), "leaf checksum")
	for i := 0; i < nrec; i++ {
		off := 6 + i*tree.recordSize
		tree.records = append(tree.records, leaf[off:off+tree.recordSize])
	}
	return tree
}

// writeDenseAttrFile writes /d with one int32 attribute per name, the value
// of names[i] being i*7+1.
func writeDenseAttrFile(t *testing.T, sb uint8, names []string) string {
	t.Helper()
	return writeOne(t, sb, func(fw *FileWriter) (*DatasetWriter, error) {
		return fw.CreateDataset("/d", Int32, []uint64{4})
	}, func(ds *DatasetWriter) error {
		if err := ds.Write([]int32{1, 2, 3, 4}); err != nil {
			return err
		}
		for i, name := range names {
			if err := ds.WriteAttribute(name, int32(i*7+1)); err != nil {
				return fmt.Errorf("attribute %s: %w", name, err)
			}
		}
		return nil
	})
}

// requireAttrNameRecords checks that the tree is an attribute name index as
// H5Adense.c creates it and that its records are those of names: type 8
// (H5B2_ATTR_DENSE_NAME_ID), 17-byte records of heap id(8), flags(1),
// creation order(4), hash(4) (H5Abtree2.c H5A__dense_btree2_name_encode),
// sorted by hash (H5A__dense_btree2_name_compare). The objects do not track
// creation order, so every record holds H5O_MAX_CRT_ORDER_IDX
// (H5Oattribute.c H5O__attr_create).
func requireAttrNameRecords(t *testing.T, tree nameIndexTree, names []string) {
	t.Helper()
	require.Equal(t, uint8(8), tree.typ, "B-tree type: attribute name index")
	require.Equal(t, 17, tree.recordSize, "record size")
	require.Equal(t, uint16(0), tree.depth)
	require.Len(t, tree.records, len(names))

	want := make([]uint32, 0, len(names))
	for _, n := range names {
		want = append(want, core.JenkinsChecksum([]byte(n)))
	}
	slices.Sort(want)

	got := make([]uint32, 0, len(tree.records))
	for i, rec := range tree.records {
		require.Equal(t, uint8(0), rec[8], "record %d flags", i)
		require.Equal(t, uint32(65535), binary.LittleEndian.Uint32(rec[9:]), "record %d creation order", i)
		got = append(got, binary.LittleEndian.Uint32(rec[13:]))
	}
	require.Equal(t, want, got, "record hashes: lookup3 of each name, ascending")
}

// requireH5dumpAttrs runs h5dump on file and requires that it lists every
// attribute of /d with its value (names[i] = i*7+1, offset by the values
// map when given).
func requireH5dumpAttrs(t *testing.T, file string, values map[string]int32) {
	t.Helper()
	out := requireH5dumpClean(t, file)
	if out == "" {
		return
	}
	for name, v := range values {
		block := h5dumpAttrData(t, out, name)
		require.Contains(t, block, fmt.Sprintf("(0): %d", v), "h5dump value of %s:\n%s", name, block)
	}
	require.Equal(t, len(values), strings.Count(out, "ATTRIBUTE \""), "attributes listed by h5dump")
}

// h5dumpAttrData returns the DATA block of attribute name in full h5dump
// output (h5dumpBlock stops at the first closing brace, which in full output
// is the one of the DATASPACE line).
func h5dumpAttrData(t *testing.T, out, name string) string {
	t.Helper()
	start := strings.Index(out, fmt.Sprintf("ATTRIBUTE %q {", name))
	require.GreaterOrEqual(t, start, 0, "h5dump output has no attribute %s:\n%s", name, out)
	data := strings.Index(out[start:], "DATA {")
	require.GreaterOrEqual(t, data, 0, "attribute %s has no DATA block", name)
	data += start
	end := strings.Index(out[data:], "}")
	require.GreaterOrEqual(t, end, 0)
	return strings.Join(strings.Fields(out[data:data+end+1]), " ")
}

func denseAttrValues(names []string) map[string]int32 {
	m := make(map[string]int32, len(names))
	for i, n := range names {
		m[n] = int32(i*7 + 1)
	}
	return m
}

// TestDenseAttributes_NameIndexAsC: the name index of dense attributes is
// written as the C library writes it, so the C library finds every
// attribute (agent X repro: h5dump showed none of the 12 attributes).
func TestDenseAttributes_NameIndexAsC(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file := writeDenseAttrFile(t, sb, denseAttrNames)
			requireAttrNameRecords(t, readNameIndex(t, file), denseAttrNames)
			requireH5dumpAttrs(t, file, denseAttrValues(denseAttrNames))
		})
	}
}

// TestDenseNameIndex_TwelveByteNamesFoundByC: the C library looks a dense
// link or attribute up by the lookup3 hash of its name; a 12-byte name hashed
// differently is listed but cannot be opened by path.
func TestDenseNameIndex_TwelveByteNamesFoundByC(t *testing.T) {
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not in PATH")
	}
	file := filepath.Join(t.TempDir(), "names12.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{5, 6}))
	for i, name := range denseAttrNames {
		require.NoError(t, ds.WriteAttribute(name, int32(i*7+1)))
	}
	links := map[string]string{"abcdefghijkl": "/target", "twelve_bytes": "/target"}
	for i := 0; i < 9; i++ {
		links[fmt.Sprintf("l%02d", i)] = "/target"
	}
	require.NoError(t, fw.CreateDenseGroup("/g", links))
	require.NoError(t, fw.Close())

	for _, args := range [][]string{
		{"-d", "/g/abcdefghijkl"},
		{"-d", "/g/twelve_bytes"},
		{"-a", "/target/abcdefghijkl"},
	} {
		out, runErr := runH5Err(h5dump, append(args, file)...)
		require.NoError(t, runErr, "h5dump %v:\n%s", args, out)
		require.NotContains(t, out, "error", "h5dump %v", args)
	}
}

// TestDenseLinks_ReadableByC: a dense group is read by the C library on both
// superblock versions. Its fractal heap counts object offsets from the start
// of the direct block (H5HFman.c:386-399) and its link messages are encoded
// as H5O__link_encode writes them (H5Olink.c:296-380); before, h5ls listed
// one link with an empty name and h5dump stopped with an internal error.
func TestDenseLinks_ReadableByC(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "links.h5")
			fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sb))
			require.NoError(t, err)
			ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{5, 6}))
			links := make(map[string]string, 10)
			for i := 0; i < 10; i++ {
				links[fmt.Sprintf("link%02d", i)] = "/target"
			}
			require.NoError(t, fw.CreateDenseGroup("/g", links))
			require.NoError(t, fw.Close())

			out := requireH5dumpClean(t, file)
			if out == "" {
				return
			}
			for name := range links {
				require.Contains(t, out, fmt.Sprintf("DATASET %q", name), "h5dump lists link %s", name)
			}
		})
	}
}

// downgradeToLegacyIndex rewrites the attribute name index of /d into the
// layout v0.14.2 and earlier wrote: B-tree type 5 with 11-byte records of
// hash(4) and the first 7 bytes of the heap id. The hashes are kept.
func downgradeToLegacyIndex(t *testing.T, file string, tree nameIndexTree) {
	t.Helper()
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	f, err := os.OpenFile(file, os.O_RDWR, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	// The superblock offset size is 8 for both versions the writer creates.
	const osz = 8
	hdr := slices.Clone(raw[tree.headerAddr : tree.headerAddr+uint64(4+1+1+4+2+2+1+1+osz+2+8+4)])
	hdr[5] = 5
	binary.LittleEndian.PutUint16(hdr[10:], 11)
	n := len(hdr) - 4
	binary.LittleEndian.PutUint32(hdr[n:], core.JenkinsChecksum(hdr[:n]))
	_, err = f.WriteAt(hdr, int64(tree.headerAddr))
	require.NoError(t, err)

	leaf := []byte{'B', 'T', 'L', 'F', 0, 5}
	for _, rec := range tree.records {
		leaf = append(leaf, rec[13:17]...) // hash
		leaf = append(leaf, rec[:7]...)    // heap id, 7 bytes
	}
	leaf = binary.LittleEndian.AppendUint32(leaf, core.JenkinsChecksum(leaf))
	_, err = f.WriteAt(leaf, int64(tree.rootAddr))
	require.NoError(t, err)

	// v0.14.2 wrote the attribute info message under ID 0x000F.
	msg := tree.ainfo
	require.False(t, msg.FromContinuation, "attribute info message expected in chunk 0")
	if tree.ohVersion == 1 {
		_, err = f.WriteAt([]byte{0x0F, 0x00}, int64(msg.Offset))
		require.NoError(t, err)
		return
	}
	// Version 2: type byte, then the checksum of chunk 0 (H5Ocache.c:
	// prefix of signature, version, flags, optional times and phase change
	// values, chunk-size field of 1 << (flags & 3) bytes).
	addr := tree.objAddr
	flags := raw[addr+5]
	prefix := uint64(6)
	if flags&0x20 != 0 {
		prefix += 16
	}
	if flags&0x10 != 0 {
		prefix += 4
	}
	width := uint64(1) << (flags & 3)
	var chunkSize uint64
	for i := width; i > 0; i-- {
		chunkSize = chunkSize<<8 | uint64(raw[addr+prefix+i-1])
	}
	end := addr + prefix + width + chunkSize // checksum position
	chunk := slices.Clone(raw[addr:end])
	chunk[msg.Offset-addr] = 0x0F
	chunk = binary.LittleEndian.AppendUint32(chunk, core.JenkinsChecksum(chunk))
	_, err = f.WriteAt(chunk, int64(addr))
	require.NoError(t, err)
}

// TestDenseAttributes_LegacyIndexMigrated: a file whose attribute name index
// v0.14.2 wrote as a link name index (type 5) is migrated to the attribute
// name index (type 8) by the first change through OpenForWrite, so the C
// library reads every attribute afterwards; no type-5 attribute record is
// ever written again.
func TestDenseAttributes_LegacyIndexMigrated(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			names := denseAttrNames[:10] // a00..a09: hashes as v0.14.2 computed them
			file := writeDenseAttrFile(t, sb, names)
			downgradeToLegacyIndex(t, file, readNameIndex(t, file))
			legacy := readNameIndex(t, file)
			require.Equal(t, uint8(5), legacy.typ)
			require.Equal(t, 11, legacy.recordSize)
			require.Equal(t, core.MsgSharedMessageTable, legacy.ainfo.Type, "legacy attribute info ID 0x000F")

			fw, err := OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			ds, err := fw.OpenDataset("/d")
			require.NoError(t, err)
			require.NoError(t, ds.WriteAttribute("a03", int32(1000)))       // modify in place
			require.NoError(t, ds.WriteAttribute("abcdefghijkl", int32(5))) // add
			require.NoError(t, ds.DeleteAttribute("a05"))                   // remove
			require.NoError(t, fw.Close())

			values := denseAttrValues(names)
			values["a03"] = 1000
			values["abcdefghijkl"] = 5
			delete(values, "a05")
			all := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == "a05" })
			all = append(all, "abcdefghijkl")

			migrated := readNameIndex(t, file)
			require.Equal(t, core.MsgAttributeInfo, migrated.ainfo.Type, "attribute info ID H5O_AINFO_ID")
			requireAttrNameRecords(t, migrated, all)
			requireH5dumpAttrs(t, file, values)
		})
	}
}

// TestDenseAttributes_LegacyHeapMigrated: the dense attributes of a file
// v0.14.2 wrote (a heap whose object offsets count from the end of the block
// prefix, a type 5 name index and attribute info ID 0x000F) come out of the
// first rewrite in the C layout: the heap objects move, every index record
// gets its new heap ID, and the C library reads every attribute.
func TestDenseAttributes_LegacyHeapMigrated(t *testing.T) {
	file := copyFixture(t, "testdata/reference/legacy-hdf5go/dense_attr_type5.h5")

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset("/d")
	require.NoError(t, err)
	require.NoError(t, ds.WriteAttribute("a03", int32(1000)))       // modify
	require.NoError(t, ds.WriteAttribute("abcdefghijkl", int32(5))) // add
	require.NoError(t, ds.DeleteAttribute("a05"))                   // remove
	require.NoError(t, fw.Close())

	values := make(map[string]int32, 13)
	for i := 0; i < 12; i++ {
		values[fmt.Sprintf("a%02d", i)] = int32(i)
	}
	values["a03"] = 1000
	values["abcdefghijkl"] = 5
	delete(values, "a05")

	requireH5dumpAttrs(t, file, values)

	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	d, ok := findDatasetByName(f, "d")
	require.True(t, ok)
	names, err := d.ListAttributes()
	require.NoError(t, err)
	require.Len(t, names, len(values), "attributes listed by this library")
	for name, want := range values {
		v, err := d.ReadAttribute(name)
		require.NoError(t, err, "read %s", name)
		require.EqualValues(t, want, v, "attribute %s read back by this library", name)
	}
}

// TestDenseAttributes_CHeapAppend: an attribute added to a C-written dense
// storage goes after the last live heap object. The writer used to put it
// at the data-area index of the heap's iterator offset (0 for a root direct
// block, H5HFdblock.c:347-366), over the block prefix the C library checks.
func TestDenseAttributes_CHeapAppend(t *testing.T) {
	file := copyFixture(t, "testdata/hdf5_official/tattr4_be.h5")
	before := requireH5dumpClean(t, file)

	// The 9 dense attributes (attr0..attr8) are on the root group; the API
	// has no root group writer, so the test builds one.
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	root := &GroupWriter{path: "/", headerAddr: fw.file.Superblock().RootGroup, file: fw}
	require.NoError(t, root.WriteAttribute("added_by_go", int32(4242)))
	require.NoError(t, fw.Close())

	out := requireH5dumpClean(t, file)
	if out == "" {
		return
	}
	require.Equal(t, strings.Count(before, "ATTRIBUTE \"")+1, strings.Count(out, "ATTRIBUTE \""),
		"h5dump lists the C attributes and the new one")
	require.Contains(t, h5dumpAttrData(t, out, "added_by_go"), "(0): 4242")
}

// TestAttributes_CompactAttributeInfo: the C library gives every version 2
// object header with attributes an attribute info message, also while the
// attributes are compact: its fractal heap address is then undefined
// (H5Oattribute.c:225-236, 315-323), so the attributes go into the header.
// h5copytst.h5 /dset_attr has such a header with one attribute. Adding
// attributes keeps them compact up to the dense threshold, then moves all of
// them to dense storage behind a single attribute info message.
func TestAttributes_CompactAttributeInfo(t *testing.T) {
	file := copyFixture(t, "testdata/hdf5_official/h5copytst.h5")
	const path = "/dset_attr"
	before := requireH5dumpClean(t, file)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset(path)
	require.NoError(t, err)
	require.NoError(t, ds.WriteAttribute("added0", int32(100)))
	require.NoError(t, fw.Close())

	out := requireH5dumpClean(t, file)
	if out != "" {
		require.Equal(t, strings.Count(before, "ATTRIBUTE \"")+1, strings.Count(out, "ATTRIBUTE \""))
		require.Contains(t, h5dumpAttrData(t, out, "added0"), "(0): 100")
	}

	// Past the dense threshold: one attribute info message, with a heap.
	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err = fw.OpenDataset(path)
	require.NoError(t, err)
	for i := 1; i <= MaxCompactAttributes; i++ {
		require.NoError(t, ds.WriteAttribute(fmt.Sprintf("added%d", i), int32(100+i)))
	}
	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), ds.address, sb)
	require.NoError(t, err)
	infos := 0
	for _, m := range oh.Messages {
		if core.IsAttributeInfoMessage(m.Type) {
			infos++
			ai, err := core.ParseAttributeInfoMessage(m.Data, sb)
			require.NoError(t, err)
			require.NotEqual(t, ^uint64(0), ai.FractalHeapAddr, "dense storage has a heap")
		}
	}
	require.Equal(t, 1, infos, "one attribute info message")
	require.NoError(t, fw.Close())

	out = requireH5dumpClean(t, file)
	if out != "" {
		require.Equal(t, strings.Count(before, "ATTRIBUTE \"")+1+MaxCompactAttributes, strings.Count(out, "ATTRIBUTE \""))
		require.Contains(t, h5dumpAttrData(t, out, "added8"), "(0): 108")
	}
}

// TestOpenDataset_CompactAttributeInfoNotDense: OpenDataset caches the
// attribute info only for dense storage. h5copytst.h5 /dset_attr has an
// attribute info message with an undefined heap address (compact attributes,
// H5Oattribute.c:225-236); OpenDataset used to cache it as dense storage.
func TestOpenDataset_CompactAttributeInfoNotDense(t *testing.T) {
	file := copyFixture(t, "testdata/hdf5_official/h5copytst.h5")
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	ds, err := fw.OpenDataset("/dset_attr")
	require.NoError(t, err)
	require.NotNil(t, attributeInfoMessage(ds.objectHeader), "the header has an attribute info message")
	require.Nil(t, ds.denseAttrInfo, "compact attributes: no dense storage cached")
	require.NoError(t, ds.RebalanceAttributeBTree(), "nothing to rebalance")
}

// indexCreationOrder turns the dense attributes of path, written in the
// order of names, into those of an object created with
// H5Pset_attr_creation_order(TRACKED | INDEXED): records with creation
// indexes 0..n-1 in a name index and a creation order index (H5Adense.c:
// 211-241), an attribute info message with both flags and the next creation
// index (H5Oainfo.c:194-220), and an object header that tracks and indexes
// creation order (H5Oint.c:254; H5Opkg.h:85-107). The C library writes no
// such file from its tools, so the test builds one.
func indexCreationOrder(t *testing.T, file, path string, names []string) {
	t.Helper()
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset(path)
	require.NoError(t, err)
	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), ds.address, sb)
	require.NoError(t, err)
	st, err := loadDenseAttributeStorage(fw, ds.address, oh, sb)
	require.NoError(t, err)

	// 512-byte nodes as C creates them (H5A_NAME_BT2_NODE_SIZE,
	// H5A_CORDER_BT2_NODE_SIZE).
	byName := structures.NewAttributeNameIndex(512)
	byOrder := structures.NewAttributeCreationOrderIndex(512)
	for i, n := range names {
		rec, found, err := st.names.FindRecord(n)
		require.NoError(t, err)
		require.True(t, found, n)
		require.NoError(t, byName.InsertNameRecord(n, rec.HeapID, 0, uint32(i)))
		require.NoError(t, byOrder.InsertCreationOrderRecord(rec.HeapID, 0, uint32(i)))
	}
	nameAddr, err := byName.WriteToFile(fw.writer, fw.writer.Allocator(), sb)
	require.NoError(t, err)
	orderAddr, err := byOrder.WriteToFile(fw.writer, fw.writer.Allocator(), sb)
	require.NoError(t, err)
	data, err := core.EncodeAttributeInfoMessage(&core.AttributeInfoMessage{
		Flags:               ainfoTrackCorder | ainfoIndexCorder,
		MaxCreationIndex:    uint64(len(names)),
		FractalHeapAddr:     st.info.FractalHeapAddr,
		BTreeNameIndexAddr:  nameAddr,
		BTreeOrderIndexAddr: orderAddr,
	}, sb)
	require.NoError(t, err)
	addr := ds.address
	require.NoError(t, fw.Close())

	trackCreationOrderHeader(t, file, addr, data)
}

// trackCreationOrderHeader rewrites chunk 0 of the version 2 object header
// at addr with the flags H5O_HDR_ATTR_CRT_ORDER_TRACKED and _INDEXED, so
// that every message header carries a 2-byte creation index (H5Opkg.h:
// 85-107), and with ainfo as its attribute info message. Compact attribute
// messages take the creation indexes 0, 1, ... in header order, as C gives
// them on creation (H5Oattribute.c:283-295); every other message has 0. The
// chunk keeps its size: the null messages give up the room.
func trackCreationOrderHeader(t *testing.T, file string, addr uint64, ainfo []byte) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(f.Reader(), addr, f.Superblock())
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, uint8(2), oh.Version)

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	flags := raw[addr+5]
	require.Zero(t, flags&0x0C, "header already tracks creation order")
	prefix := uint64(6)
	if flags&0x20 != 0 {
		prefix += 16
	}
	if flags&0x10 != 0 {
		prefix += 4
	}
	width := uint64(1) << (flags & 3)
	var chunkSize uint64
	for i := width; i > 0; i-- {
		chunkSize = chunkSize<<8 | uint64(raw[addr+prefix+i-1])
	}

	var body []byte
	var crtIdx uint16
	appendMsg := func(typ core.MessageType, flags uint8, idx uint16, data []byte) {
		body = append(body, byte(typ))
		body = binary.LittleEndian.AppendUint16(body, uint16(len(data)))
		body = append(body, flags)
		body = binary.LittleEndian.AppendUint16(body, idx)
		body = append(body, data...)
	}
	hasAinfo := false
	for _, m := range oh.Messages {
		require.False(t, m.FromContinuation, "message %d in a continuation chunk", m.Type)
		require.NotEqual(t, core.MsgContinuation, m.Type)
		if m.Type == core.MsgNil {
			continue
		}
		data := m.Data
		if core.IsAttributeInfoMessage(m.Type) {
			data = ainfo
			hasAinfo = true
		}
		var idx uint16
		if m.Type == core.MsgAttribute {
			idx = crtIdx
			crtIdx++
		}
		appendMsg(m.Type, m.Flags, idx, data)
	}
	if !hasAinfo {
		// C appends one, not shareable (H5Oattribute.c:296-298;
		// H5O_MSG_FLAG_DONTSHARE).
		appendMsg(core.MsgAttributeInfo, 0x04, 0, ainfo)
	}
	rem := int(chunkSize) - len(body)
	require.True(t, rem == 0 || rem >= 6, "chunk 0 has %d bytes left for a null message", rem)
	if rem > 0 {
		body = append(body, byte(core.MsgNil))
		body = binary.LittleEndian.AppendUint16(body, uint16(rem-6))
		body = append(body, make([]byte, rem-3)...)
	}

	chunk := slices.Clone(raw[addr : addr+prefix+width])
	chunk[5] |= 0x0C
	chunk = append(chunk, body...)
	chunk = binary.LittleEndian.AppendUint32(chunk, core.JenkinsChecksum(chunk))
	w, err := os.OpenFile(file, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = w.WriteAt(chunk, int64(addr))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// TestDenseAttributes_CreationOrderIndex: on an object that tracks and
// indexes attribute creation order, a new dense attribute takes the next
// creation index (H5Oattribute.c:283-295) and gets a record in the creation
// order index (H5Adense.c:505-530), a rewritten one keeps its creation index
// and the index follows its new heap ID (H5Adense.c:616-636), and a deleted
// one leaves both indexes (H5Adense.c:880-960). The C library then lists the
// attributes in creation order through that index.
func TestDenseAttributes_CreationOrderIndex(t *testing.T) {
	names := []string{"zeta", "alpha", "mid", "beta", "omega", "gamma", "delta", "kappa", "eta"}
	file := writeDenseAttrFile(t, SuperblockV2, names)
	indexCreationOrder(t, file, "/d", names)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset("/d")
	require.NoError(t, err)
	require.NoError(t, ds.WriteAttribute("added", int32(77))) // creation index 9
	require.NoError(t, ds.WriteAttribute("mid", int64(500)))  // larger: a new heap ID
	require.NoError(t, ds.DeleteAttribute("beta"))            // creation index 3
	require.NoError(t, fw.Close())

	order := []string{"zeta", "alpha", "mid", "omega", "gamma", "delta", "kappa", "eta", "added"}
	corders := []uint32{0, 1, 2, 4, 5, 6, 7, 8, 9}

	byName := readNameIndex(t, file)
	ainfo := byName.ainfo.Data
	require.Equal(t, core.MsgAttributeInfo, byName.ainfo.Type)
	require.Equal(t, byte(ainfoTrackCorder|ainfoIndexCorder), ainfo[1], "attribute info flags")
	require.Equal(t, uint16(10), binary.LittleEndian.Uint16(ainfo[2:]), "next creation index")

	require.Equal(t, uint8(8), byName.typ)
	require.Len(t, byName.records, len(order))
	ids := make(map[uint32][]byte, len(order))
	for _, rec := range byName.records {
		ids[binary.LittleEndian.Uint32(rec[9:])] = rec[:8]
	}

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	// version, flags, max creation index(2), heap, name index, order index.
	byOrder := decodeIndexTree(t, raw, readAddressLE(ainfo[4+16:4+24]))
	require.Equal(t, uint8(9), byOrder.typ, "B-tree type: attribute creation order index")
	require.Equal(t, 13, byOrder.recordSize)
	require.Len(t, byOrder.records, len(order))
	for i, rec := range byOrder.records {
		c := binary.LittleEndian.Uint32(rec[9:])
		require.Equal(t, corders[i], c, "record %d creation index", i)
		require.Equal(t, uint8(0), rec[8], "record %d flags", i)
		require.Equal(t, ids[c], rec[:8], "record %d heap ID matches the name index", i)
	}

	values := map[string]int32{"added": 77, "mid": 500}
	for i, n := range names {
		if _, set := values[n]; !set && n != "beta" {
			values[n] = int32(i*7 + 1)
		}
	}
	requireH5dumpAttrs(t, file, values)

	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Log("h5dump not in PATH: creation order check skipped")
		return
	}
	out, runErr := runH5Err(h5dump, "--sort_by=creation_order", "-A", file)
	require.NoError(t, runErr, "h5dump:\n%s", out)
	require.NotContains(t, out, "error")
	var listed []string
	for _, line := range strings.Split(out, "\n") {
		if _, rest, ok := strings.Cut(line, "ATTRIBUTE \""); ok {
			name, _, _ := strings.Cut(rest, "\"")
			listed = append(listed, name)
		}
	}
	require.Equal(t, order, listed, "attributes in creation order:\n%s", out)
}
