package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// Where buildDenseStorage lays out its structures.
const (
	denseTestBTHD     = 0x0100
	denseTestBTLF     = 0x0200
	denseTestFRHP     = 0x0400
	denseTestFHDB     = 0x0600
	denseTestNodeSize = 512
	// denseTestFHDBHeader is the direct block header: signature, version,
	// heap address (8) and block offset (2: a 16-bit heap), no checksum.
	denseTestFHDBHeader = 15
)

// buildDenseStorage lays out a dense storage index as the C library writes
// one: a v2 B-tree of record type typ (header and a root leaf, checksummed as
// H5B2cache.c checks them) and a fractal heap (FRHP + one direct block FHDB)
// holding msgs. Each record names its message by a managed heap ID (type 0,
// 2-byte offset, 2-byte length) where its record type keeps the ID
// (denseRecordLayout); the other record bytes are a marker, so a decoder that
// looks for the ID elsewhere fails. No messages: an empty tree, root address
// undefined (H5B2hdr.c:245).
func buildDenseStorage(typ uint8, msgs [][]byte) []byte {
	layout := denseRecordLayout[typ]
	buf := make([]byte, 0x0900)
	le := binary.LittleEndian

	bthd := buf[denseTestBTHD:]
	copy(bthd, "BTHD")
	bthd[5] = typ
	le.PutUint32(bthd[6:], denseTestNodeSize)
	le.PutUint16(bthd[10:], uint16(layout.size))
	bthd[14], bthd[15] = 75, 40 // split, merge %
	root := uint64(denseTestBTLF)
	if len(msgs) == 0 {
		root = undefinedAddress
	}
	le.PutUint64(bthd[16:], root)
	le.PutUint16(bthd[24:], uint16(len(msgs)))
	le.PutUint64(bthd[26:], uint64(len(msgs)))
	le.PutUint32(bthd[34:], JenkinsChecksum(bthd[:34]))

	btlf := buf[denseTestBTLF:]
	copy(btlf, "BTLF")
	btlf[5] = typ
	// C's heap offsets count from the start of the direct block, whose
	// header is 15 bytes here (H5HFman.c:388-393).
	off, heapOff := 6, denseTestFHDBHeader
	for i, m := range msgs {
		rec := btlf[off : off+int(layout.size)]
		for j := range rec {
			rec[j] = byte(0xA0 + i)
		}
		id := rec[layout.idAt : layout.idAt+layout.idLen]
		clear(id)
		le.PutUint16(id[1:], uint16(heapOff))
		le.PutUint16(id[3:], uint16(len(m)))
		off += int(layout.size)
		heapOff += len(m)
	}
	le.PutUint32(btlf[off:], JenkinsChecksum(btlf[:off]))

	frhp := buf[denseTestFRHP:]
	copy(frhp, "FRHP")
	le.PutUint16(frhp[5:], uint16(layout.idLen)) // heap ID length
	le.PutUint32(frhp[10:], 512)                 // max managed object size
	le.PutUint64(frhp[22:], undefinedAddress)    // huge object B-tree
	le.PutUint64(frhp[38:], undefinedAddress)    // free space manager
	le.PutUint64(frhp[46:], 512)                 // managed space
	le.PutUint16(frhp[110:], 4)                  // table width
	le.PutUint64(frhp[112:], 512)                // starting block size, inside buf
	le.PutUint64(frhp[120:], 512)                // maximum direct block size
	le.PutUint16(frhp[128:], 16)                 // log2 max heap size
	le.PutUint64(frhp[132:], denseTestFHDB)
	le.PutUint32(frhp[142:], JenkinsChecksum(frhp[:142]))

	fhdb := buf[denseTestFHDB:]
	copy(fhdb, "FHDB")
	le.PutUint64(fhdb[5:], denseTestFRHP)
	copy(fhdb[denseTestFHDBHeader:], bytes.Join(msgs, nil))

	return buf
}

// buildDenseAttributeStorage is buildDenseStorage for an attribute name index
// (type 8, H5Abtree2.c) and the attribute info message that points at it.
func buildDenseAttributeStorage(msgs [][]byte) ([]byte, *AttributeInfoMessage) {
	return buildDenseStorage(bt2TypeAttrName, msgs),
		&AttributeInfoMessage{FractalHeapAddr: denseTestFRHP, BTreeNameIndexAddr: denseTestBTHD}
}

// TestReadDenseAttributes_SkipsSharedDatatype is the regression for
// review-064 M2: one attribute with a committed (shared) datatype in dense
// storage failed the whole listing; it is skipped as in compact storage
// (H5Oattr.c:88, valid HDF5) and the other attributes are returned.
//
// Since TASK-068 committed datatypes resolve; the case still unsupported is a
// datatype in the shared object header message heap (SOHM), which keeps the
// skip. A shared reference that is not one (the bytes this test used before)
// is corruption and fails the read.
func TestReadDenseAttributes_SkipsSharedDatatype(t *testing.T) {
	// A version 3 attribute "sohm" whose datatype is a SOHM reference
	// (H5Oshared.c:331-337: version 3, type 1, 8-byte heap ID).
	ref := cat([]byte{3, shareTypeSOHM}, make([]byte, sharedMsgHeapIDSize))
	scalar := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	sohm := cat([]byte{3, attrFlagTypeShared, 5, 0, byte(len(ref)), 0, byte(len(scalar)), 0, 0},
		[]byte("sohm\x00"), ref, scalar, []byte{1, 0, 0, 0})
	good := buildV3AttrMessage(t, "good", 42)

	buf, info := buildDenseAttributeStorage([][]byte{sohm, good})
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	attrs, err := readDenseAttributes(bytes.NewReader(buf), info, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "good", attrs[0].Name)

	bogus := buildV3AttrMessage(t, "bogus", 1)
	bogus[1] |= attrFlagTypeShared // The int32 datatype bytes are no shared reference.
	buf, info = buildDenseAttributeStorage([][]byte{bogus, good})
	_, err = readDenseAttributes(bytes.NewReader(buf), info, sb)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}
