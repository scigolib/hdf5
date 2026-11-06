package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

func sharedTestSB() *Superblock {
	return &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
}

func le64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// TestDecodeSharedMessage covers every layout H5O__shared_decode accepts
// (H5Oshared.c:289-367) and the checks it makes.
func TestDecodeSharedMessage(t *testing.T) {
	sb := sharedTestSB()

	v1 := cat([]byte{1, 0}, make([]byte, 6), make([]byte, 8), le64(0x1234))
	v2 := cat([]byte{2, 0}, le64(0x2345))
	v2Typed := cat([]byte{2, 2}, le64(0x2346))
	v3 := cat([]byte{3, shareTypeCommitted}, le64(0x3456))
	v3Heap := cat([]byte{3, shareTypeSOHM}, le64(0x77))

	tests := []struct {
		name    string
		data    []byte
		kind    uint8
		addr    uint64
		corrupt bool
	}{
		{name: "v1 committed", data: v1, kind: shareTypeCommitted, addr: 0x1234},
		{name: "v2 committed", data: v2, kind: shareTypeCommitted, addr: 0x2345},
		{name: "v2 type byte 2", data: v2Typed, kind: shareTypeCommitted, addr: 0x2346},
		{name: "v3 committed", data: v3, kind: shareTypeCommitted, addr: 0x3456},
		{name: "v3 heap", data: v3Heap, kind: shareTypeSOHM},
		{name: "version 0", data: cat([]byte{0, 2}, le64(1)), corrupt: true},
		{name: "version 4", data: cat([]byte{4, 2}, le64(1)), corrupt: true},
		{name: "one byte", data: []byte{3}, corrupt: true},
		{name: "empty", data: nil, corrupt: true},
		{name: "v1 truncated", data: v1[:len(v1)-1], corrupt: true},
		{name: "v2 truncated", data: v2[:5], corrupt: true},
		{name: "v2 heap", data: cat([]byte{2, shareTypeSOHM}, le64(1)), corrupt: true},
		{name: "v3 type 0", data: cat([]byte{3, 0}, le64(1)), corrupt: true},
		{name: "v3 type 3 (here)", data: cat([]byte{3, 3}, le64(1)), corrupt: true},
		{name: "v3 heap truncated", data: v3Heap[:9], corrupt: true},
		{name: "v3 undefined address", data: cat([]byte{3, 2}, le64(undefinedAddress)), corrupt: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := decodeSharedMessage(tt.data, sb)
			if tt.corrupt {
				require.ErrorIs(t, err, utils.ErrCorrupt)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.kind, sm.kind)
			if tt.kind == shareTypeCommitted {
				require.Equal(t, tt.addr, sm.address)
			}
		})
	}
}

// TestDecodeSharedMessage_SmallOffsets checks 4-byte offsets and lengths: the
// version-1 skip is the length size, and 0xffffffff is the undefined address.
func TestDecodeSharedMessage_SmallOffsets(t *testing.T) {
	sb := &Superblock{OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}

	sm, err := decodeSharedMessage(cat([]byte{1, 0}, make([]byte, 6), make([]byte, 4), []byte{0x10, 0x20, 0, 0}), sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0x2010), sm.address)

	_, err = decodeSharedMessage([]byte{3, 2, 0xff, 0xff, 0xff, 0xff}, sb)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// v1HeaderWith builds a version-1 object header holding msgs (type, flags,
// body), padded as H5O__chunk_deserialize expects (8-byte aligned messages).
func v1HeaderWith(msgs ...HeaderMessage) []byte {
	var body []byte
	for _, m := range msgs {
		data := m.Data
		for len(data)%8 != 0 {
			data = append(data, 0)
		}
		hdr := make([]byte, 8)
		binary.LittleEndian.PutUint16(hdr[0:], uint16(m.Type))
		binary.LittleEndian.PutUint16(hdr[2:], uint16(len(data)))
		hdr[4] = m.Flags
		body = append(body, hdr...)
		body = append(body, data...)
	}
	header := make([]byte, 16, 16+len(body))
	header[0] = 1 // Version.
	binary.LittleEndian.PutUint16(header[2:], uint16(len(msgs)))
	binary.LittleEndian.PutUint32(header[4:], 1) // Reference count.
	binary.LittleEndian.PutUint32(header[8:], uint32(len(body)))
	return append(header[:16], body...)
}

// TestResolveSharedMessage resolves committed datatypes in a hand-built file
// and rejects the cases C cannot produce.
func TestResolveSharedMessage(t *testing.T) {
	sb := sharedTestSB()

	const (
		plainAt  = 0x100 // Committed datatype: a plain int32 datatype message.
		chainAt  = 0x200 // Its datatype message is itself shared (points at plainAt).
		selfAt   = 0x300 // Its datatype message points at itself.
		noTypeAt = 0x400 // A header without a datatype message.
	)

	file := make([]byte, 0x500)
	copy(file[plainAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: int32LEBytes}))
	copy(file[chainAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{3, 2}, le64(plainAt))}))
	copy(file[selfAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{3, 2}, le64(selfAt))}))
	copy(file[noTypeAt:], v1HeaderWith(HeaderMessage{Type: MsgNil, Data: make([]byte, 8)}))
	r := bytes.NewReader(file)

	ref := func(addr uint64) *HeaderMessage {
		return &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Offset: 0x40, Data: cat([]byte{3, 2}, le64(addr))}
	}

	dt, err := ResolveDatatypeHeaderMessage(r, sb, ref(plainAt))
	require.NoError(t, err)
	require.Equal(t, DatatypeFixed, dt.Class)
	require.Equal(t, uint32(4), dt.Size)

	// Version 1 and 2 references resolve the same header.
	v1 := &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{1, 0}, make([]byte, 6), make([]byte, 8), le64(plainAt))}
	dt, err = ResolveDatatypeHeaderMessage(r, sb, v1)
	require.NoError(t, err)
	require.Equal(t, uint32(4), dt.Size)

	for name, addr := range map[string]uint64{
		"chain":           chainAt,
		"self":            selfAt,
		"no datatype":     noTypeAt,
		"not a header":    0x10,
		"beyond the file": 0x10000,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveDatatypeHeaderMessage(r, sb, ref(addr))
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}

	// A datatype in the shared message heap is valid HDF5, not corruption.
	heap := &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{3, shareTypeSOHM}, le64(1))}
	_, err = ResolveDatatypeHeaderMessage(r, sb, heap)
	require.True(t, IsSOHMNotSupported(err), "%v", err)
	require.NotErrorIs(t, err, utils.ErrCorrupt)

	// An unshared message decodes in place.
	dt, err = ResolveDatatypeHeaderMessage(r, sb, &HeaderMessage{Type: MsgDatatype, Data: int32LEBytes})
	require.NoError(t, err)
	require.Equal(t, uint32(4), dt.Size)
}

// TestAttributeFlags_C checks the attribute flag byte as H5O__attr_decode
// reads it (H5Oattr.c:153-161): unused before version 2, unknown bits
// rejected from version 2 on.
func TestAttributeFlags_C(t *testing.T) {
	// Version 1, name "a", int32, scalar dataspace, 4 data bytes; the unused
	// flag byte holds 0x01 and must not be read as "datatype shared".
	scalar := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	dtype := int32LEBytes
	v1 := cat([]byte{1, 0x01, 2, 0, byte(len(dtype)), 0, byte(len(scalar)), 0},
		[]byte{'a', 0, 0, 0, 0, 0, 0, 0}, dtype, make([]byte, (8-len(dtype)%8)%8), scalar, []byte{7, 0, 0, 0})
	attr, err := ParseAttributeMessage(v1, binary.LittleEndian)
	require.NoError(t, err)
	require.Equal(t, "a", attr.Name)

	v3 := cat([]byte{3, 0x04, 2, 0, byte(len(dtype)), 0, byte(len(scalar)), 0, 0}, []byte{'a', 0}, dtype, scalar, []byte{7, 0, 0, 0})
	_, err = ParseAttributeMessage(v3, binary.LittleEndian)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestAttributeV2_ExactSizes: from version 2 on the name, datatype and
// dataspace are not padded (H5Oattr.c:207-208); only version 1 pads them.
func TestAttributeV2_ExactSizes(t *testing.T) {
	scalar := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	dtype := int32LEBytes
	v2 := cat([]byte{2, 0, 2, 0, byte(len(dtype)), 0, byte(len(scalar)), 0}, []byte{'a', 0}, dtype, scalar, []byte{7, 0, 0, 0})
	attr, err := ParseAttributeMessage(v2, binary.LittleEndian)
	require.NoError(t, err)
	require.Equal(t, "a", attr.Name)
	v, err := attr.ReadValue()
	require.NoError(t, err)
	require.EqualValues(t, 7, v)
}
