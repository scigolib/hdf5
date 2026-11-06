package core

import (
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// DenseStorage is what a walk of a dense storage's blocks found.
type DenseStorage struct {
	// Records is the number of records of the name index: the attributes
	// or links the storage holds.
	Records uint64
}

// attrFlagSharedRecord is H5O_MSG_FLAG_SHARED in a dense attribute record's
// flags (H5A_dense_bt2_name_rec_t, H5Abtree2.c:312-326): the attribute is in
// the shared object header message heap and its heap ID names it there.
const attrFlagSharedRecord = MsgFlagShared

// DenseAttributeStorageBlocks passes to block every piece of file space the
// dense attribute storage of ainfo owns, as H5A__dense_delete frees it
// (H5Adense.c:1779-1830): the name index v2 B-tree (H5Adense.c:1806), the
// creation order index if defined (:1818) and the fractal heap (:1825,
// FractalHeap.Blocks). Each block is reported at the size C allocated it.
//
// C's name index callback decrements the references an attribute holds in
// the shared object header message heap (H5A__dense_delete_bt2_cb,
// H5Adense.c:1479-1520: H5SM_delete for a shared attribute, and
// H5O__attr_delete for its shared datatype and dataspace). This library
// cannot change that heap, so such an attribute is an error wrapping
// utils.ErrNotSupported (IsSOHMNotSupported); every other attribute message is
// checked by decoding its header fields.
//
// Legacy layouts of this library up to v0.14.2 (a name index of type 5,
// a heap with 0 for its huge-object and free-space addresses) are walked as
// they were written.
func DenseAttributeStorageBlocks(r io.ReaderAt, sb *Superblock, ainfo *AttributeInfoMessage,
	block func(addr, size uint64) error) (DenseStorage, error) {
	if !definedAddress(ainfo.BTreeNameIndexAddr) {
		return DenseStorage{}, fmt.Errorf("%w: dense attribute storage at heap 0x%X has no name index",
			utils.ErrCorrupt, ainfo.FractalHeapAddr)
	}
	corder := ainfo.BTreeOrderIndexAddr
	if ainfo.Flags&attrInfoIndexCorder == 0 {
		corder = undefinedAddress
	}

	return denseStorageBlocks(r, sb, denseStorageSpec{
		heap: ainfo.FractalHeapAddr, name: ainfo.BTreeNameIndexAddr, corder: corder,
		nameTypes: []uint8{bt2TypeAttrName, bt2TypeGroupName}, corderTypes: []uint8{bt2TypeAttrCorder},
		record: checkDenseAttributeRecord,
	}, block)
}

// DenseLinkStorageBlocks passes to block every piece of file space the dense
// link storage of linfo owns, as H5G__dense_delete frees it
// (H5Gdense.c:1581-1650): the name index v2 B-tree (:1618/:1627), the creation
// order index if defined (:1636) and the fractal heap (:1644,
// FractalHeap.Blocks). The links themselves are not followed: the caller
// decides what a group that still has links means (Records).
func DenseLinkStorageBlocks(r io.ReaderAt, sb *Superblock, linfo *LinkInfoMessage,
	block func(addr, size uint64) error) (DenseStorage, error) {
	if !linfo.HasNameBTree() {
		return DenseStorage{}, fmt.Errorf("%w: dense link storage at heap 0x%X has no name index",
			utils.ErrCorrupt, linfo.FractalHeapAddress)
	}
	corder := undefinedAddress
	if linfo.HasCreationOrderIndex() && linfo.HasCreationOrderBTree() {
		corder = linfo.CreationOrderBTreeAddress
	}

	return denseStorageBlocks(r, sb, denseStorageSpec{
		heap: linfo.FractalHeapAddress, name: linfo.NameBTreeAddress, corder: corder,
		nameTypes: []uint8{bt2TypeGroupName}, corderTypes: []uint8{bt2TypeGroupCorder},
	}, block)
}

// denseStorageSpec names the parts of one dense storage and the record types
// C opens its indexes with.
type denseStorageSpec struct {
	heap, name, corder     uint64
	nameTypes, corderTypes []uint8
	// record checks each name index record against the opened heap; nil:
	// records are only counted.
	record func(r io.ReaderAt, sb *Superblock, heap *FractalHeap, typ uint8, rec []byte) error
}

// denseStorageBlocks walks a dense storage's indexes and heap through the
// checked readers (readBT2Header and walkBlocks, OpenFractalHeap and Blocks).
func denseStorageBlocks(r io.ReaderAt, sb *Superblock, s denseStorageSpec,
	block func(addr, size uint64) error) (DenseStorage, error) {
	heap, err := OpenFractalHeap(r, sb, s.heap)
	if err != nil {
		return DenseStorage{}, err
	}

	name, err := openDenseIndex(r, sb, s.name, s.nameTypes)
	if err != nil {
		return DenseStorage{}, err
	}
	var out DenseStorage
	err = name.walkBlocks(r, sb, block, func(rec []byte) error {
		out.Records++
		if s.record == nil {
			return nil
		}

		return s.record(r, sb, heap, name.typ, rec)
	})
	if err != nil {
		return DenseStorage{}, fmt.Errorf("name index at 0x%X: %w", s.name, err)
	}

	if s.corder != undefinedAddress {
		corder, err := openDenseIndex(r, sb, s.corder, s.corderTypes)
		if err != nil {
			return DenseStorage{}, err
		}
		if err := corder.walkBlocks(r, sb, block, func([]byte) error { return nil }); err != nil {
			return DenseStorage{}, fmt.Errorf("creation order index at 0x%X: %w", s.corder, err)
		}
	}

	if err := heap.Blocks(block); err != nil {
		return DenseStorage{}, err
	}

	return out, nil
}

// openDenseIndex reads the header of a dense storage index of one of types
// and checks its record size against the type's layout (denseRecordLayout).
func openDenseIndex(r io.ReaderAt, sb *Superblock, addr uint64, types []uint8) (*bt2Header, error) {
	h, err := readBT2Header(r, sb, addr, types...)
	if err != nil {
		return nil, err
	}
	if layout := denseRecordLayout[h.typ]; h.rrecSize != layout.size {
		return nil, fmt.Errorf("%w: v2 B-tree at 0x%X has %d-byte records, type %d records are %d bytes",
			utils.ErrCorrupt, h.addr, h.rrecSize, h.typ, layout.size)
	}

	return h, nil
}

// checkDenseAttributeRecord refuses an attribute that holds a reference in
// the shared object header message heap (DenseAttributeStorageBlocks): a
// record flagged shared, or an attribute message whose datatype or dataspace
// is shared there.
func checkDenseAttributeRecord(_ io.ReaderAt, sb *Superblock, heap *FractalHeap, typ uint8, rec []byte) error {
	layout := denseRecordLayout[typ]
	if typ == bt2TypeAttrName && rec[layout.idLen]&attrFlagSharedRecord != 0 {
		return fmt.Errorf("dense attribute record: %w", errSOHMNotSupported)
	}
	data, err := heap.Object(rec[layout.idAt : layout.idAt+layout.idLen])
	if err != nil {
		return err
	}

	return checkAttributeSOHM(data, sb)
}

// CheckNoSOHMReferences returns an error wrapping utils.ErrNotSupported
// (IsSOHMNotSupported) when deleting an object with these header messages
// would have to decrement a reference in the shared object header message
// heap: a message stored there (H5O_MSG_FLAG_SHARED of type
// H5O_SHARE_TYPE_SOHM; H5O__shared_delete → H5SM_delete, H5Oshared.c), or
// an attribute message whose datatype or dataspace is (H5O__attr_delete,
// H5Oattr.c:568-589). Dense attributes are checked by
// DenseAttributeStorageBlocks.
func CheckNoSOHMReferences(sb *Superblock, messages []*HeaderMessage) error {
	for _, msg := range messages {
		if msg.Flags&MsgFlagShared != 0 {
			sh, err := decodeSharedMessage(msg.Data, sb)
			if err != nil {
				return fmt.Errorf("shared message of type %d at 0x%X: %w", msg.Type, msg.Offset, err)
			}
			if sh.kind == shareTypeSOHM {
				return fmt.Errorf("message of type %d at 0x%X: %w", msg.Type, msg.Offset, errSOHMNotSupported)
			}

			continue
		}
		if msg.Type == MsgAttribute {
			if err := checkAttributeSOHM(msg.Data, sb); err != nil {
				return fmt.Errorf("attribute message at 0x%X: %w", msg.Offset, err)
			}
		}
	}

	return nil
}

// checkAttributeSOHM decodes the header fields of an attribute message
// (H5O__attr_decode, H5Oattr.c:150-260) and refuses one whose datatype or
// dataspace field is a reference into the shared object header message heap.
// Version 1 messages have no sharing flags.
func checkAttributeSOHM(data []byte, sb *Superblock) error {
	const fixed = 8 // version, flags, three 2-byte sizes
	if len(data) < fixed {
		return fmt.Errorf("%w: attribute message too short: %d bytes", utils.ErrCorrupt, len(data))
	}
	version, flags := data[0], data[1]
	if version < 2 || flags&(attrFlagTypeShared|attrFlagSpaceShared) == 0 {
		return nil
	}
	nameSize := int(sb.Endianness.Uint16(data[2:]))
	typeSize := int(sb.Endianness.Uint16(data[4:]))
	spaceSize := int(sb.Endianness.Uint16(data[6:]))
	at := fixed + nameSize
	if version >= 3 {
		at++ // name character set
	}
	if at+typeSize+spaceSize > len(data) {
		return fmt.Errorf("%w: attribute message of %d bytes cannot hold fields of %d, %d and %d bytes",
			utils.ErrCorrupt, len(data), nameSize, typeSize, spaceSize)
	}
	fields := []struct {
		shared bool
		body   []byte
	}{
		{flags&attrFlagTypeShared != 0, data[at : at+typeSize]},
		{flags&attrFlagSpaceShared != 0, data[at+typeSize : at+typeSize+spaceSize]},
	}
	for _, f := range fields {
		if !f.shared {
			continue
		}
		sh, err := decodeSharedMessage(f.body, sb)
		if err != nil {
			return err
		}
		if sh.kind == shareTypeSOHM {
			return fmt.Errorf("attribute datatype or dataspace: %w", errSOHMNotSupported)
		}
	}

	return nil
}

// definedAddress reports whether addr names a structure: neither the
// undefined address nor 0, the superblock.
func definedAddress(addr uint64) bool {
	return addr != 0 && addr != undefinedAddress
}
