package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildDenseAttributeStorage lays out a B-tree v2 name index (BTHD + BTLF)
// and a fractal heap (FRHP + one direct block FHDB) holding the given
// attribute messages, as in TestReadDenseAttributes_EndToEnd.
func buildDenseAttributeStorage(msgs [][]byte) ([]byte, *AttributeInfoMessage) {
	const bthdAddr, btlfAddr, frhpAddr, fhdbAddr = 0x0100, 0x0200, 0x0300, 0x0500
	buf := make([]byte, 0x0800)

	bthd := buf[bthdAddr:]
	copy(bthd, "BTHD")
	bthd[5] = 8                                        // attribute name index
	binary.LittleEndian.PutUint32(bthd[6:], 4096)      // node size
	binary.LittleEndian.PutUint16(bthd[10:], 11)       // record: 4 hash + 7 heap ID
	bthd[14], bthd[15] = 75, 40                        // split, merge %
	binary.LittleEndian.PutUint64(bthd[16:], btlfAddr) // root
	binary.LittleEndian.PutUint16(bthd[24:], uint16(len(msgs)))
	binary.LittleEndian.PutUint64(bthd[26:], uint64(len(msgs)))

	btlf := buf[btlfAddr:]
	copy(btlf, "BTLF")
	btlf[5] = 8
	off, heapOff := 6, 0
	for i, m := range msgs {
		binary.LittleEndian.PutUint32(btlf[off:], uint32(0x1000+i)) // name hash
		// Managed heap ID: type 0, 2-byte offset, 2-byte length.
		binary.LittleEndian.PutUint16(btlf[off+5:], uint16(heapOff))
		binary.LittleEndian.PutUint16(btlf[off+7:], uint16(len(m)))
		off += 11
		heapOff += len(m)
	}

	frhp := buf[frhpAddr:]
	copy(frhp, "FRHP")
	binary.LittleEndian.PutUint16(frhp[5:], 7)    // heap ID length
	binary.LittleEndian.PutUint32(frhp[10:], 512) // max managed object size
	binary.LittleEndian.PutUint16(frhp[110:], 4)  // table width
	binary.LittleEndian.PutUint64(frhp[112:], 4096)
	binary.LittleEndian.PutUint64(frhp[120:], 4096)
	binary.LittleEndian.PutUint16(frhp[128:], 16) // log2 max heap size
	binary.LittleEndian.PutUint64(frhp[132:], fhdbAddr)

	fhdb := buf[fhdbAddr:]
	copy(fhdb, "FHDB")
	binary.LittleEndian.PutUint64(fhdb[5:], frhpAddr)
	copy(fhdb[15:], bytes.Join(msgs, nil))

	return buf, &AttributeInfoMessage{FractalHeapAddr: frhpAddr, BTreeNameIndexAddr: bthdAddr}
}

// TestReadDenseAttributes_SkipsSharedDatatype is the regression for
// review-064 M2: one attribute with a committed (shared) datatype in dense
// storage failed the whole listing; it is skipped as in compact storage
// (H5Oattr.c:88, valid HDF5) and the other attributes are returned.
func TestReadDenseAttributes_SkipsSharedDatatype(t *testing.T) {
	shared := buildV3AttrMessage(t, "committed", 1)
	shared[1] |= attrFlagTypeShared
	good := buildV3AttrMessage(t, "good", 42)

	buf, info := buildDenseAttributeStorage([][]byte{shared, good})
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	attrs, err := readDenseAttributes(bytes.NewReader(buf), info, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "good", attrs[0].Name)
}
