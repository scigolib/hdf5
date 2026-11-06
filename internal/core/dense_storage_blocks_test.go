package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// attributeMessageV3 builds an attribute message of version 3 (H5Oattr.c
// H5O__attr_encode) with the given sharing flags and datatype and dataspace
// fields.
func attributeMessageV3(flags uint8, dt, ds []byte) []byte {
	name := []byte("a\x00")
	data := []byte{3, flags}
	data = binary.LittleEndian.AppendUint16(data, uint16(len(name)))
	data = binary.LittleEndian.AppendUint16(data, uint16(len(dt)))
	data = binary.LittleEndian.AppendUint16(data, uint16(len(ds)))
	data = append(data, 0) // ASCII names
	return cat(data, name, dt, ds, []byte{1, 2, 3, 4})
}

// TestCheckNoSOHMReferences: a delete must refuse every message that holds a
// reference into the shared object header message heap, since freeing the
// object would have to decrement it there (H5O__shared_delete → H5SM_delete,
// H5O__attr_delete → the datatype and dataspace delete callbacks); a message
// shared with a committed object, or not shared at all, is no such reference.
func TestCheckNoSOHMReferences(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	sohmRef := cat([]byte{sharedMsgVersion3, shareTypeSOHM}, le64(0x77))
	committedRef := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(0x400))
	plainType := []byte{0x10, 0x08, 0, 0, 4, 0, 0, 0, 0, 0, 32, 0} // int32 LE
	plainSpace := []byte{2, 0, 0, 0}                               // scalar

	for _, tc := range []struct {
		name   string
		msg    *HeaderMessage
		refuse bool
	}{
		{"message in the heap", &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: sohmRef}, true},
		{"message of a committed datatype", &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: committedRef}, false},
		{"plain message", &HeaderMessage{Type: MsgDatatype, Data: plainType}, false},
		{"attribute datatype in the heap", &HeaderMessage{
			Type: MsgAttribute,
			Data: attributeMessageV3(attrFlagTypeShared, sohmRef, plainSpace),
		}, true},
		{"attribute dataspace in the heap", &HeaderMessage{
			Type: MsgAttribute,
			Data: attributeMessageV3(attrFlagSpaceShared, plainType, sohmRef),
		}, true},
		{"attribute of a committed datatype", &HeaderMessage{
			Type: MsgAttribute,
			Data: attributeMessageV3(attrFlagTypeShared, committedRef, plainSpace),
		}, false},
		{"plain attribute", &HeaderMessage{Type: MsgAttribute, Data: attributeMessageV3(0, plainType, plainSpace)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckNoSOHMReferences(sb, []*HeaderMessage{tc.msg})
			if !tc.refuse {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.True(t, IsSOHMNotSupported(err), "%v", err)
		})
	}
}

// TestCheckNoSOHMReferences_TruncatedAttribute: an attribute message too
// short for the field sizes it declares is corrupt, not a reason to skip the
// check.
func TestCheckNoSOHMReferences_TruncatedAttribute(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	data := attributeMessageV3(attrFlagTypeShared, make([]byte, 10), []byte{2, 0, 0, 0})
	err := CheckNoSOHMReferences(sb, []*HeaderMessage{{Type: MsgAttribute, Data: data[:12]}})
	require.Error(t, err)
	require.False(t, IsSOHMNotSupported(err))
}

// TestCheckDenseAttributeRecord_SharedFlag: a dense attribute name record
// whose flags byte marks the attribute shared (H5A__dense_btree2_name_decode,
// H5Abtree2.c) is refused before its heap object is read
// (H5A__dense_delete_bt2_cb → H5SM_delete).
func TestCheckDenseAttributeRecord_SharedFlag(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	layout := denseRecordLayout[bt2TypeAttrName]
	rec := make([]byte, layout.size)
	rec[layout.idLen] = attrFlagSharedRecord
	err := checkDenseAttributeRecord(nil, sb, nil, bt2TypeAttrName, rec)
	require.Error(t, err)
	require.True(t, IsSOHMNotSupported(err), "%v", err)
}
