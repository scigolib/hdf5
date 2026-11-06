package hdf5

import (
	"errors"
	"fmt"
	"slices"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/writer"
)

// linkHeapIDLen is the length of a heap ID in dense link storage
// (H5G_DENSE_FHEAP_ID_LEN, H5Gpkg.h:40); a link name record (v2 B-tree type
// 5) stores it after the name hash (H5G__dense_btree2_name_encode,
// H5Gbtree2.c:262).
const linkHeapIDLen = 7

// denseLinkStorage is the dense link storage of one group for a
// read-modify-write: the fractal heap of encoded link messages and the
// name index, the same writable structures dense attribute storage uses
// (denseAttributeStorage). This library writes no creation order index for
// links (v2 B-tree type 6): a group that indexes link creation order is
// refused before this is built.
//
// The writable v2 B-tree holds a single leaf and the writable heap a single
// root direct block, so a change that needs a B-tree split or a second heap
// block is refused with ErrNotSupported before anything is written.
type denseLinkStorage struct {
	sb    *core.Superblock
	heap  *structures.WritableFractalHeap
	names *structures.WritableBTreeV2
}

// newDenseLinkStorage creates empty dense link storage as H5G__dense_create
// does (H5Gdense.c:243-300), with this library's deviations: one 64 KiB
// root direct block (structures.NewLinkFractalHeap) instead of a 512-byte
// starting block, and name index nodes of denseIndexNodeSize instead of 512
// bytes, which give the single leaf room before a split would be needed.
// Both are valid parameters the C library reads as written.
func newDenseLinkStorage(sb *core.Superblock) (*denseLinkStorage, error) {
	heap := structures.NewLinkFractalHeap()
	if err := heap.SetAddressSize(sb.OffsetSize); err != nil {
		return nil, fmt.Errorf("dense link heap: %w", err)
	}
	st := &denseLinkStorage{sb: sb, heap: heap, names: structures.NewWritableBTreeV2(denseIndexNodeSize)}
	st.names.SetNameResolver(st.recordName)
	return st, nil
}

// loadDenseLinkStorage loads the dense link storage linfo names. A group
// that indexes creation order, and a heap written by v0.14.2 or earlier, are
// refused with ErrNotSupported: the first needs the type 6 index this
// library does not write, and the heap migration
// (structures.WritableFractalHeap.AdoptObjects) rebuilds a legacy heap with
// the parameters of an attribute heap, whose heap IDs do not fit the 7 bytes
// of a link record.
func (fw *FileWriter) loadDenseLinkStorage(linfo *core.LinkInfoMessage) (*denseLinkStorage, error) {
	sb := fw.file.sb
	if linfo.HasCreationOrderIndex() {
		return nil, fmt.Errorf("%w: changing a group that indexes link creation order (v2 B-tree type 6)", ErrNotSupported)
	}
	if !linfo.HasNameBTree() {
		return nil, fmt.Errorf("%w: dense link storage at heap 0x%X has no name index",
			utils.ErrCorrupt, linfo.FractalHeapAddress)
	}

	r := fw.writer.Reader()
	st := &denseLinkStorage{sb: sb, heap: structures.NewLinkFractalHeap(), names: structures.NewWritableBTreeV2(denseIndexNodeSize)}
	if err := st.heap.LoadFromFile(r, linfo.FractalHeapAddress, sb); err != nil {
		return nil, fmt.Errorf("load dense link heap: %w", err)
	}
	if st.heap.Header.HugeObjectBTreeAddr == 0 && st.heap.Header.FreeSectionAddress == 0 {
		return nil, fmt.Errorf("%w: changing a dense link heap written by v0.14.2 or earlier", ErrNotSupported)
	}
	if st.heap.Header.HeapIDLength != linkHeapIDLen {
		return nil, fmt.Errorf("%w: dense link heap has %d-byte heap IDs, links store %d",
			utils.ErrCorrupt, st.heap.Header.HeapIDLength, linkHeapIDLen)
	}
	if err := st.names.LoadFromFile(r, linfo.NameBTreeAddress, sb); err != nil {
		return nil, fmt.Errorf("load link name index: %w", err)
	}
	if st.names.Type() != structures.BTreeV2TypeLinkNameIndex {
		return nil, fmt.Errorf("%w: link name index at 0x%X has B-tree type %d",
			utils.ErrCorrupt, linfo.NameBTreeAddress, st.names.Type())
	}
	st.names.SetNameResolver(st.recordName)

	// The heap learns which space is live from the records that point into
	// it. A C heap keeps its heap IDs, so no record changes (remap is nil
	// for every heap that passed the legacy check above).
	records := st.names.GetRecords()
	ids := make([][]byte, len(records))
	for i, rec := range records {
		ids[i] = slices.Clone(rec.HeapID[:linkHeapIDLen])
	}
	remap, err := st.heap.AdoptObjects(ids)
	if err != nil {
		return nil, fmt.Errorf("dense link heap: %w", err)
	}
	if remap != nil {
		return nil, fmt.Errorf("dense link heap was rebuilt with new heap IDs: %w", utils.ErrCorrupt)
	}
	return st, nil
}

// recordName is the name index's resolver: the name of the link a record
// points at, read from the heap, as the C compare callback reads it to break
// hash ties (H5G__dense_btree2_name_compare, H5Gbtree2.c:206).
func (st *denseLinkStorage) recordName(rec structures.IndexRecord) (string, error) {
	lm, err := st.link(rec)
	if err != nil {
		return "", err
	}
	return lm.Name, nil
}

// object returns the encoded link message a record points at.
func (st *denseLinkStorage) object(rec structures.IndexRecord) ([]byte, error) {
	data, err := st.heap.GetObject(rec.HeapID[:linkHeapIDLen])
	if err != nil {
		return nil, fmt.Errorf("read link from dense link heap: %w", err)
	}
	return data, nil
}

// link returns the link a record points at.
func (st *denseLinkStorage) link(rec structures.IndexRecord) (*core.LinkMessage, error) {
	data, err := st.object(rec)
	if err != nil {
		return nil, err
	}
	lm, err := core.ParseLinkMessage(data, st.sb)
	if err != nil {
		return nil, fmt.Errorf("%w: link in dense link heap: %w", utils.ErrCorrupt, err)
	}
	return lm, nil
}

// insert adds the encoded link message data of the link name: the message
// into the heap, a record with its heap ID into the name index
// (H5G__dense_insert, H5Gdense.c:344-431).
func (st *denseLinkStorage) insert(name string, data []byte) error {
	id, err := st.heap.InsertObject(data)
	if errors.Is(err, structures.ErrObjectTooLarge) {
		return fmt.Errorf("%w: link %q of %d bytes is a huge heap object: %w", ErrNotSupported, name, len(data), err)
	}
	if err != nil {
		return fmt.Errorf("insert link %q into dense link heap: %w", name, err)
	}
	if st.heap.RootIndirectBlock != nil {
		return fmt.Errorf("%w: the dense link heap needs a second block for link %q", ErrNotSupported, name)
	}
	if len(id) != linkHeapIDLen {
		return fmt.Errorf("dense link heap returned a %d-byte heap ID, want %d", len(id), linkHeapIDLen)
	}
	var rec [8]byte
	copy(rec[:], id)
	err = st.names.InsertNameRecord(name, rec, 0, 0)
	if errors.Is(err, structures.ErrBTreeNodeFull) {
		return fmt.Errorf("%w: the link name index needs a node split for link %q: %w", ErrNotSupported, name, err)
	}
	if err != nil {
		return fmt.Errorf("insert link %q into name index: %w", name, err)
	}
	return nil
}

// remove deletes the link name from the name index and its message from the
// heap, and returns the link (H5G__dense_remove_bt2_cb and
// H5G__dense_remove_fh_cb, H5Gdense.c:1185-1273). A name the storage does not
// hold is errGroupEntryNotFound.
func (st *denseLinkStorage) remove(name string) (*core.LinkMessage, error) {
	rec, found, err := st.names.FindRecord(name)
	if err != nil {
		return nil, fmt.Errorf("look up link %q: %w", name, err)
	}
	if !found {
		return nil, fmt.Errorf("link %q: %w", name, errGroupEntryNotFound)
	}
	lm, err := st.link(rec)
	if err != nil {
		return nil, err
	}
	if err := st.names.DeleteRecordWithRebalancing(name); err != nil {
		return nil, fmt.Errorf("remove link %q from name index: %w", name, err)
	}
	if err := st.heap.DeleteObject(rec.HeapID[:linkHeapIDLen]); err != nil {
		return nil, fmt.Errorf("remove link %q from dense link heap: %w", name, err)
	}
	return lm, nil
}

// count is the number of links: the records of the name index, as
// H5G__obj_get_linfo counts them (H5Gobj.c:326, H5B2_get_nrec).
func (st *denseLinkStorage) count() int {
	return len(st.names.GetRecords())
}

// linkMessages returns the encoded link messages in name index order, the
// order H5G__dense_build_table gives with H5_INDEX_NAME and H5_ITER_NATIVE
// (H5Gobj.c:858).
func (st *denseLinkStorage) linkMessages() ([][]byte, error) {
	records := st.names.GetRecords()
	msgs := make([][]byte, len(records))
	for i, rec := range records {
		data, err := st.object(rec)
		if err != nil {
			return nil, err
		}
		msgs[i] = slices.Clone(data)
	}
	return msgs, nil
}

// flush writes the heap and the name index back where they were loaded.
func (st *denseLinkStorage) flush(fw *FileWriter) error {
	if err := st.heap.WriteAt(fw.writer, st.sb); err != nil {
		return fmt.Errorf("write dense link heap: %w", err)
	}
	if err := st.names.WriteAt(fw.writer, st.sb); err != nil {
		return fmt.Errorf("write link name index: %w", err)
	}
	return nil
}

// create writes new storage at newly allocated space and returns the
// addresses of the heap and of the name index for the link info message.
func (st *denseLinkStorage) create(fw *FileWriter) (heapAddr, nameAddr uint64, err error) {
	heapAddr, err = st.heap.WriteToFile(fw.writer, fw.writer.Allocator(), st.sb)
	if err != nil {
		return 0, 0, fmt.Errorf("write dense link heap: %w", err)
	}
	nameAddr, err = st.names.WriteToFile(fw.writer, fw.writer.Allocator(), st.sb)
	if err != nil {
		return 0, 0, fmt.Errorf("write link name index: %w", err)
	}
	return heapAddr, nameAddr, nil
}

// denseLinkStorageRanges returns every block of file space the dense link
// storage of linfo owns (core.DenseLinkStorageBlocks, as H5G__dense_delete
// frees it, H5Gdense.c:1581-1650), checked against the allocator without
// changing it. Unlike Delete's denseLinkRanges it accepts storage that
// still holds links: the caller has moved them back into the header.
func (fw *FileWriter) denseLinkStorageRanges(linfo *core.LinkInfoMessage) ([]writer.FreeBlock, error) {
	limit := fw.file.ReadLimit()
	var ranges []writer.FreeBlock
	add := func(addr, size uint64) error {
		// Each kept range costs 16 bytes against the read limit, as
		// deleteRanges charges them.
		if uint64(len(ranges)) >= limit/16 {
			return fmt.Errorf("%w: the dense link storage has more than %d blocks, more than the read limit of %d bytes holds",
				utils.ErrLimit, len(ranges), limit)
		}
		ranges = append(ranges, writer.FreeBlock{Offset: addr, Size: size})
		return nil
	}
	if _, err := core.DenseLinkStorageBlocks(fw.writer.Reader(), fw.file.sb, linfo, add); err != nil {
		return nil, fmt.Errorf("dense link storage: %w", err)
	}
	if err := fw.writer.Allocator().CheckFree(ranges); err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}
	return ranges, nil
}
