package core

import (
	"bytes"
	"testing"
)

// FuzzResolveSharedMessage feeds a shared datatype message reference and the
// file it points into to ResolveDatatypeHeaderMessage. Nothing may panic or
// recurse without bound; a resolved datatype must be a plain one. The seeds are
// the crafted references C refuses (H5O__shared_decode, H5Oshared.c:289-367):
// a committed datatype whose message is itself shared, one pointing at itself,
// a SOHM-heap reference, bad versions and addresses past the file.
func FuzzResolveSharedMessage(f *testing.F) {
	const (
		plainAt = 0x100
		chainAt = 0x200
		selfAt  = 0x300
	)
	file := make([]byte, 0x400)
	copy(file[plainAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: int32LEBytes}))
	copy(file[chainAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{3, 2}, le64(plainAt))}))
	copy(file[selfAt:], v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: cat([]byte{3, 2}, le64(selfAt))}))

	for _, ref := range [][]byte{
		cat([]byte{3, 2}, le64(plainAt)),
		cat([]byte{3, 2}, le64(chainAt)),
		cat([]byte{3, 2}, le64(selfAt)),
		cat([]byte{3, shareTypeSOHM}, le64(1)),
		cat([]byte{2, 0}, le64(plainAt)),
		cat([]byte{1, 0}, make([]byte, 6), make([]byte, 8), le64(plainAt)),
		cat([]byte{0, 2}, le64(plainAt)),
		cat([]byte{4, 2}, le64(plainAt)),
		cat([]byte{3, 2}, le64(0xFFFFFFFFFFFFFFFF)),
		cat([]byte{3, 2}, le64(0x10000)),
		{3, 2, 1},
	} {
		f.Add(ref, file)
	}

	f.Fuzz(func(t *testing.T, ref, file []byte) {
		msg := &HeaderMessage{Type: MsgDatatype, Flags: MsgFlagShared, Data: ref}
		dt, err := ResolveDatatypeHeaderMessage(bytes.NewReader(file), sharedTestSB(), msg)
		if err != nil {
			return
		}
		if dt == nil {
			t.Fatal("resolved a nil datatype without an error")
		}
	})
}
