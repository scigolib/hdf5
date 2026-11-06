package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

var denseTestSB = &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

// denseTestMsgs are the heap objects the dense storage tests index.
var denseTestMsgs = [][]byte{[]byte("alpha"), []byte("bravo-message"), []byte("c")}

// resignDense recomputes the checksums of the header and the root leaf that
// buildDenseStorage lays out, after a test changed a field, so that the change
// reaches the decoder instead of the checksum check.
func resignDense(buf []byte, nrec int) {
	le := binary.LittleEndian
	le.PutUint32(buf[denseTestBTHD+34:], JenkinsChecksum(buf[denseTestBTHD:denseTestBTHD+34]))
	end := denseTestBTLF + 6 + nrec*int(le.Uint16(buf[denseTestBTHD+10:]))
	le.PutUint32(buf[end:], JenkinsChecksum(buf[denseTestBTLF:end]))
}

// TestReadDenseHeapObjects_RecordTypes: each record type is decoded with its
// own layout (H5Abtree2.c:312-326, :449-462; H5Gbtree2.c:293-294, :426-427)
// and the index slot accepts the types C opens it with. Red before TASK-058:
// the old reader took the heap ID from bytes 4..10 of every record whatever
// its type, so types 6, 8 and 9 named the wrong heap objects.
func TestReadDenseHeapObjects_RecordTypes(t *testing.T) {
	for _, tc := range []struct {
		kind denseKind
		typ  uint8
	}{
		{denseAttributes, bt2TypeAttrName},
		{denseAttributes, bt2TypeAttrCorder},
		{denseAttributes, bt2TypeGroupName}, // The legacy layout of this library's writer up to v0.14.2.
		{denseLinks, bt2TypeGroupName},
		{denseLinks, bt2TypeGroupCorder},
	} {
		t.Run(fmt.Sprintf("kind%d_type%d", tc.kind, tc.typ), func(t *testing.T) {
			buf := buildDenseStorage(tc.typ, denseTestMsgs)
			got, err := readDenseHeapObjects(bytes.NewReader(buf), denseTestSB, denseTestBTHD, denseTestFRHP, tc.kind)
			require.NoError(t, err)
			require.Equal(t, denseTestMsgs, got)
		})
	}
}

// TestReadDenseHeapObjects_WrongTypeInSlot: an index whose header names a
// record type its slot does not hold is corrupt, as in C, which opens the
// index with the slot's record class and refuses another type
// (H5B2__cache_hdr_deserialize, H5B2cache.c:258).
func TestReadDenseHeapObjects_WrongTypeInSlot(t *testing.T) {
	for _, tc := range []struct {
		kind denseKind
		typ  uint8
	}{
		{denseAttributes, bt2TypeGroupCorder},
		{denseLinks, bt2TypeAttrName},
		{denseLinks, bt2TypeAttrCorder},
	} {
		buf := buildDenseStorage(tc.typ, denseTestMsgs)
		_, err := readDenseHeapObjects(bytes.NewReader(buf), denseTestSB, denseTestBTHD, denseTestFRHP, tc.kind)
		require.ErrorIs(t, err, utils.ErrCorrupt, "kind %d type %d", tc.kind, tc.typ)
		require.ErrorContains(t, err, fmt.Sprintf("type %d, want", tc.typ))
	}
}

// TestReadDenseHeapObjects_Corrupt: lies in the index are utils.ErrCorrupt.
// Red before TASK-058 (the old readers verified no checksum and trusted the
// counts): the checksum cases listed the objects, the count and node size
// lies read past the leaf's records or returned short lists.
func TestReadDenseHeapObjects_Corrupt(t *testing.T) {
	le := binary.LittleEndian
	for _, tc := range []struct {
		name   string
		patch  func(b []byte)
		resign bool
		want   string
	}{
		{"header checksum", func(b []byte) { b[denseTestBTHD+6]++ }, false, "checksum mismatch"},
		{"leaf checksum", func(b []byte) { b[denseTestBTLF+7]++ }, false, "checksum mismatch"},
		{"record size lie", func(b []byte) { le.PutUint16(b[denseTestBTHD+10:], 11) }, true, "type 8 records are 17 bytes"},
		{"total count lie", func(b []byte) { le.PutUint64(b[denseTestBTHD+26:], 4) }, true, ""},
		{"root count lie", func(b []byte) { le.PutUint16(b[denseTestBTHD+24:], 4) }, true, ""},
		{"count with no root", func(b []byte) { le.PutUint64(b[denseTestBTHD+16:], undefinedAddress) }, true, "no root node"},
		{"node size lie", func(b []byte) { le.PutUint32(b[denseTestBTHD+6:], 1<<20) }, true, ""},
		{"node size too small", func(b []byte) { le.PutUint32(b[denseTestBTHD+6:], 16) }, true, "holds no"},
		{"leaf type", func(b []byte) { b[denseTestBTLF+5] = bt2TypeAttrCorder }, true, ""},
		{"heap id type 3", func(b []byte) { b[denseTestBTLF+6] = 0x30 }, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := buildDenseStorage(bt2TypeAttrName, denseTestMsgs)
			tc.patch(buf)
			if tc.resign {
				resignDense(buf, len(denseTestMsgs))
			}
			_, err := readDenseHeapObjects(bytes.NewReader(buf), denseTestSB, denseTestBTHD, denseTestFRHP, denseAttributes)
			require.ErrorIs(t, err, utils.ErrCorrupt)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

// TestReadDenseHeapObjects_EmptiedRoot: an index whose counts are zero but
// whose root address is still defined is empty. C frees an emptied root leaf
// and stores an undefined address (H5B2leaf.c:811-818); this library's
// attribute deletion up to v0.14.2 kept the leaf, and its files must still
// read as empty rather than as corrupt.
func TestReadDenseHeapObjects_EmptiedRoot(t *testing.T) {
	le := binary.LittleEndian
	buf := buildDenseStorage(bt2TypeAttrName, denseTestMsgs)
	le.PutUint16(buf[denseTestBTHD+24:], 0) // root record count
	le.PutUint64(buf[denseTestBTHD+26:], 0) // total record count
	resignDense(buf, len(denseTestMsgs))

	got, err := readDenseHeapObjects(bytes.NewReader(buf), denseTestSB, denseTestBTHD, denseTestFRHP, denseAttributes)
	require.NoError(t, err)
	require.Empty(t, got)
}

// tattr4_be.h5 (official C test suite, tattr.c) keeps the 9 attributes of
// its root group in dense storage: a type 8 name index of depth 0 whose
// header is at this offset (from the file's bytes; h5dump -A lists attr0 ..
// attr8, each the string "1234").
const (
	tattr4File = "../../testdata/hdf5_official/tattr4_be.h5"
	tattr4BTHD = 900
)

// readRootAttributes reads the attributes of the root group's object header.
func readRootAttributes(t *testing.T, img []byte) ([]*Attribute, error) {
	t.Helper()
	r := bytes.NewReader(img)
	sb, err := ReadSuperblock(r)
	require.NoError(t, err)
	hdr, err := ReadObjectHeader(r, sb.RootGroup, sb)
	require.NoError(t, err)

	return hdr.Attributes, hdr.AttributesErr
}

// TestAttributeInfoMessage_IDs: the attribute info message is found by the C
// ID 0x0015 (H5Oprivate.h:228), and by 0x000F, the ID this library wrote up
// to v0.14.2, only when no 0x0015 message is present. Red before TASK-058:
// MsgAttributeInfo was 15, so the attribute info of every C file was ignored
// (tattr4_be.h5 listed no attributes, h5dump lists 9).
func TestAttributeInfoMessage_IDs(t *testing.T) {
	c := &HeaderMessage{Type: 0x0015}
	legacy := &HeaderMessage{Type: 0x000F}
	other := &HeaderMessage{Type: MsgDataspace}

	require.Same(t, c, attributeInfoMessage([]*HeaderMessage{other, c}))
	require.Same(t, c, attributeInfoMessage([]*HeaderMessage{legacy, c}))
	require.Same(t, legacy, attributeInfoMessage([]*HeaderMessage{other, legacy}))
	require.Nil(t, attributeInfoMessage([]*HeaderMessage{other}))
}

// TestParseAttributesFromMessages_BadAttributeInfo: an attribute info message
// that does not decode fails the attribute read, as C fails the iteration
// (H5A__get_ainfo, H5Aint.c:2009-2010). Red before TASK-058: it was ignored
// and the compact attributes were returned as if there were no dense ones.
func TestParseAttributesFromMessages_BadAttributeInfo(t *testing.T) {
	msgs := []*HeaderMessage{{Type: MsgAttributeInfo, Data: []byte{0, 0, 1}}}
	_, err := ParseAttributesFromMessages(bytes.NewReader(nil), msgs, denseTestSB)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.ErrorContains(t, err, "attribute info message")
}

// torderattr.h5 (official C test suite) has a dataset and a group, at these
// header offsets (h5debug), that track attribute creation order: their
// attribute info messages are 0x03 = track only, max creation index 3, and
// undefined dense addresses; the attributes a, b, c are compact, each the
// unsigned byte 0 (h5dump).
const torderattrFile = "../../testdata/hdf5_official/torderattr.h5"

var torderattrTracked = []uint64{800, 1700}

// TestAttributeInfo_CFileTrackedOrder: an attribute info message that tracks
// creation order holds max_crt_idx (2 bytes) and then the addresses
// (H5Oainfo.c:132-138). Red before TASK-058: the decoder skipped a further
// 2-byte "min dense" field that C does not have, so it wanted 22 bytes of a
// 20-byte message; and an undefined heap address was taken for dense storage.
func TestAttributeInfo_CFileTrackedOrder(t *testing.T) {
	img, err := os.ReadFile(torderattrFile)
	require.NoError(t, err)
	r := bytes.NewReader(img)
	sb, err := ReadSuperblock(r)
	require.NoError(t, err)

	for _, addr := range torderattrTracked {
		t.Run(fmt.Sprint(addr), func(t *testing.T) { checkTrackedAinfo(t, r, sb, addr) })
	}
}

// checkTrackedAinfo checks the attribute info and attributes of the
// torderattr.h5 header at addr.
func checkTrackedAinfo(t *testing.T, r *bytes.Reader, sb *Superblock, addr uint64) {
	t.Helper()
	hdr, err := ReadObjectHeader(r, addr, sb)
	require.NoError(t, err)
	msg := attributeInfoMessage(hdr.Messages)
	require.NotNil(t, msg)
	ainfo, err := ParseAttributeInfoMessage(msg.Data, sb)
	require.NoError(t, err)
	require.EqualValues(t, attrInfoTrackCorder, ainfo.Flags)
	require.EqualValues(t, 3, ainfo.MaxCreationIndex, "h5debug: max creation index 3")
	require.Equal(t, undefinedAddress, ainfo.FractalHeapAddr)
	require.Equal(t, undefinedAddress, ainfo.BTreeNameIndexAddr)

	require.NoError(t, hdr.AttributesErr)
	names := make([]string, 0, len(hdr.Attributes))
	for _, a := range hdr.Attributes {
		names = append(names, a.Name)
		v, err := a.ReadValue()
		require.NoError(t, err, a.Name)
		require.EqualValues(t, 0, v, "h5dump: %s is 0", a.Name)
	}
	require.ElementsMatch(t, []string{"a", "b", "c"}, names)
}

// TestAttributeInfo_Rejected: the version and flag checks of C
// (H5Oainfo.c:112-124) on decode, and of the encoder on the values it would
// otherwise truncate or set as unknown bits.
func TestAttributeInfo_Rejected(t *testing.T) {
	addrs := make([]byte, 16)
	for _, data := range [][]byte{
		append([]byte{1, 0}, addrs...),    // version 1
		append([]byte{0, 0x04}, addrs...), // unknown flag bit
		append([]byte{0, 0x80}, addrs...), // unknown flag bit
		{0, attrInfoTrackCorder, 3},       // max_crt_idx cut short
	} {
		_, err := ParseAttributeInfoMessage(data, denseTestSB)
		require.Error(t, err, "% x", data)
	}

	for _, aim := range []*AttributeInfoMessage{
		{Flags: 0x04},
		{Flags: attrInfoTrackCorder, MaxCreationIndex: 1 << 16},
	} {
		_, err := EncodeAttributeInfoMessage(aim, denseTestSB)
		require.Error(t, err, "%+v", aim)
	}
}

// TestReadDenseAttributes_CFile reads a dense attribute name index written by
// the C library: type 8 records, id(8) flags(1) corder(4) hash(4)
// (H5Abtree2.c:312-326). Red before TASK-058: the old reader decoded every
// record as hash(4) + heap ID(7) with an 11-byte stride, so it took the heap
// IDs of these 17-byte records from the wrong bytes.
func TestReadDenseAttributes_CFile(t *testing.T) {
	img, err := os.ReadFile(tattr4File)
	require.NoError(t, err)
	attrs, err := readRootAttributes(t, img)
	require.NoError(t, err)

	names := make([]string, 0, len(attrs))
	for _, a := range attrs {
		names = append(names, a.Name)
		v, err := a.ReadValue()
		require.NoError(t, err, a.Name)
		require.Equal(t, "1234", v, "h5dump: %s is \"1234\"", a.Name)
	}
	require.ElementsMatch(t, []string{"attr0", "attr1", "attr2", "attr3", "attr4", "attr5", "attr6", "attr7", "attr8"}, names)
}

// TestReadDenseAttributes_CFileChecksums: a checksum mismatch in the index is
// utils.ErrCorrupt, as in C (the verify_chksum callbacks of H5B2cache.c).
// Red before TASK-058: the old reader verified no checksum.
func TestReadDenseAttributes_CFileChecksums(t *testing.T) {
	base, err := os.ReadFile(tattr4File)
	require.NoError(t, err)
	leaf := binary.LittleEndian.Uint64(base[tattr4BTHD+16:])

	for _, at := range []uint64{tattr4BTHD + 6, leaf + 8} {
		img := bytes.Clone(base)
		img[at]++
		_, err := readRootAttributes(t, img)
		require.ErrorIs(t, err, utils.ErrCorrupt, "byte %d", at)
		require.ErrorContains(t, err, "checksum mismatch")
	}
}
