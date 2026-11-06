package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// sharedTypeAttr builds a version 3 attribute message named "a" whose datatype
// is a shared (committed) reference to the object header at target, with a
// scalar dataspace and one byte of value.
func sharedTypeAttr(target uint64) []byte {
	ref := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(target))
	scalar := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	return cat([]byte{3, attrFlagTypeShared, 2, 0, byte(len(ref)), 0, byte(len(scalar)), 0, 0},
		[]byte{'a', 0}, ref, scalar, []byte{7})
}

// bigCompound returns the encoded datatype message of an n-member compound of
// int8 members, about 20 bytes per member.
func bigCompound(t *testing.T, n int) []byte {
	t.Helper()
	i8, err := NewIntegerDatatype(1, true)
	require.NoError(t, err)
	members := make([]CompoundMember, 0, n)
	for i := 0; i < n; i++ {
		members = append(members, CompoundMember{Name: fmt.Sprintf("m%d", i), Offset: uint32(i), Type: i8})
	}
	dt, err := NewCompoundDatatype(uint32(n), members)
	require.NoError(t, err)
	enc, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	return enc
}

// int8Message returns the encoded datatype message of a signed 8-bit integer.
func int8Message(t *testing.T) []byte {
	t.Helper()
	i8, err := NewIntegerDatatype(1, true)
	require.NoError(t, err)
	enc, err := EncodeDatatypeMessage(i8)
	require.NoError(t, err)
	return enc
}

// TestSharedDatatypeDecodedOncePerFile is the regression test for review-068
// finding 2: 200 attributes of 45 bytes, each typed by the same committed
// 2,500-member compound (49 KB), decoded the compound 200 times (128 MiB
// retained, 642 MiB for 1,000 attributes). The committed type must be decoded
// once per file and shared, across headers too.
//
// Red on 40c6d35: the attributes hold distinct copies of the type (run in WSL
// under prlimit, tmp/agents/agent-r-reader.md).
func TestSharedDatatypeDecodedOncePerFile(t *testing.T) {
	sb := sharedTestSB()
	const target = 0x100
	hdr := v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: bigCompound(t, 2500)})
	file := make([]byte, target+len(hdr))
	copy(file[target:], hdr)
	r := bytes.NewReader(file)

	attr := sharedTypeAttr(target)
	msgs := make([]*HeaderMessage, 200)
	for i := range msgs {
		msgs[i] = &HeaderMessage{Type: MsgAttribute, Data: attr}
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	attrs, err := ParseAttributesFromMessages(r, msgs, sb)
	require.NoError(t, err)
	require.Len(t, attrs, len(msgs))

	// A second object header in the same file shares the same decoded type.
	again, err := ParseAttributesFromMessages(r, msgs[:1], sb)
	require.NoError(t, err)

	runtime.GC()
	runtime.ReadMemStats(&after)

	for i, a := range attrs {
		require.True(t, attrs[0].Datatype == a.Datatype, "attribute %d decoded its own copy", i)
	}
	require.True(t, attrs[0].Datatype == again[0].Datatype, "second header decoded its own copy")
	require.Len(t, attrs[0].Datatype.Members, 2500)

	// One decoded copy is well under 1 MiB; 200 copies were 128 MiB.
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	require.Less(t, retained, int64(16<<20), "retained heap after 200 shared attributes")
	runtime.KeepAlive(attrs)
}

// TestSharedResolveBudget checks the per-file budget of what resolving shared
// messages keeps (review-068 finding 2), and is the regression test for review-r
// findings 4 and 5: the budget charged every message of a target header,
// NIL padding included, and a fixed 16 MiB, but not the decoded datatype the
// cache keeps. Now only the target message (body and 8-byte message header)
// and an estimate of its decoded datatype are charged, against the file's read
// limit.
func TestSharedResolveBudget(t *testing.T) {
	i8 := int8Message(t)
	dt, err := ParseDatatypeMessage(i8)
	require.NoError(t, err)
	perTarget := uint64(len(i8)) + sharedMsgHeaderCost + datatypeFootprint(dt)

	// Tiny datatypes padded with 60,000 bytes of NIL message each: the padding
	// is parsed and dropped, not charged.
	build := func(targets int) (*Superblock, *bytes.Reader, []*HeaderMessage) {
		hdr := v1HeaderWith(
			HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: i8},
			HeaderMessage{Type: MsgNil, Data: make([]byte, 60000)},
		)
		file := make([]byte, 0x100, 0x100+targets*len(hdr))
		msgs := make([]*HeaderMessage, targets)
		for i := range msgs {
			msgs[i] = &HeaderMessage{Type: MsgAttribute, Data: sharedTypeAttr(uint64(len(file)))}
			file = append(file, hdr...)
		}
		sb := sharedTestSB()
		sb.SetSharedLimit(func() uint64 { return 275 * perTarget })
		return sb, bytes.NewReader(file), msgs
	}

	// 250 targets fit although their headers hold 15 MB.
	sb, r, msgs := build(250)
	attrs, err := ParseAttributesFromMessages(r, msgs, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 250)

	// 300 do not.
	sb, r, msgs = build(300)
	_, err = ParseAttributesFromMessages(r, msgs, sb)
	require.ErrorIs(t, err, utils.ErrLimit)
	require.NotErrorIs(t, err, utils.ErrCorrupt)

	// A large compound is charged for what it decodes to, not for its 49 KB
	// body: about 1.3 MB for 2,500 members.
	big := bigCompound(t, 2500)
	hdr := v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: big})
	file := make([]byte, 0x100+len(hdr))
	copy(file[0x100:], hdr)
	sb = sharedTestSB()
	sb.SetSharedLimit(func() uint64 { return 4 * uint64(len(big)) })
	_, err = resolveSharedDatatype(bytes.NewReader(file), sb, cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(0x100)))
	require.ErrorIs(t, err, utils.ErrLimit)
}

// errorFromReaderAt fails every read at or past from with err.
type errorFromReaderAt struct {
	io.ReaderAt
	from int64
	err  error
}

func (f errorFromReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.from {
		return 0, f.err
	}
	return f.ReaderAt.ReadAt(p, off)
}

// TestSharedTargetErrorClassified is the regression test for review-068
// finding 4: a failure to read the target header was wrapped in ErrCorrupt
// whatever it was, so an inner ErrLimit or ErrNotSupported matched both
// sentinels. It is classified as filterError does.
//
// Red on 40c6d35: the ErrLimit and ErrNotSupported cases also match ErrCorrupt.
func TestSharedTargetErrorClassified(t *testing.T) {
	const target = 0x100
	file := make([]byte, 0x200)
	ref := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(target))

	for _, inner := range []error{utils.ErrLimit, utils.ErrNotSupported} {
		r := errorFromReaderAt{ReaderAt: bytes.NewReader(file), from: target, err: fmt.Errorf("device: %w", inner)}
		_, err := resolveSharedMessage(r, sharedTestSB(), ref, MsgDatatype)
		require.ErrorIs(t, err, inner)
		require.NotErrorIs(t, err, utils.ErrCorrupt, "inner %v", inner)
	}

	// A target that is not an object header is corruption of the reference.
	_, err := resolveSharedMessage(bytes.NewReader(file), sharedTestSB(), ref, MsgDatatype)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestCompactAttributeErrorReported is the regression test for review-068
// finding 5: a compact attribute that fails to decode (here a shared reference
// of an unknown share type) was skipped silently while the dense path failed.
// Only an attribute in the shared message heap, valid HDF5 the library cannot
// read yet, is skipped.
//
// Red on 40c6d35: ParseAttributesFromMessages returns no error and drops the
// attribute.
func TestCompactAttributeErrorReported(t *testing.T) {
	sb := sharedTestSB()
	r := bytes.NewReader(make([]byte, 0x200))

	badRef := cat([]byte{sharedMsgVersion3, 7}, le64(0x100))
	scalar := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	bad := cat([]byte{3, attrFlagTypeShared, 2, 0, byte(len(badRef)), 0, byte(len(scalar)), 0, 0},
		[]byte{'b', 0}, badRef, scalar)

	_, err := ParseAttributesFromMessages(r, []*HeaderMessage{{Type: MsgAttribute, Data: bad}}, sb)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// SOHM-typed attribute: skipped, the plain one kept.
	sohmRef := cat([]byte{sharedMsgVersion3, shareTypeSOHM}, le64(0x77))
	sohm := cat([]byte{3, attrFlagTypeShared, 2, 0, byte(len(sohmRef)), 0, byte(len(scalar)), 0, 0},
		[]byte{'s', 0}, sohmRef, scalar)
	i8 := int8Message(t)
	plain := cat([]byte{3, 0, 2, 0, byte(len(i8)), 0, byte(len(scalar)), 0, 0}, []byte{'p', 0}, i8, scalar, []byte{5})

	attrs, err := ParseAttributesFromMessages(r, []*HeaderMessage{
		{Type: MsgAttribute, Data: sohm},
		{Type: MsgAttribute, Data: plain},
	}, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "p", attrs[0].Name)
	require.True(t, errors.Is(errSOHMNotSupported, utils.ErrNotSupported))
}
