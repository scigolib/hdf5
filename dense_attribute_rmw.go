package hdf5

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
)

// Flags of the attribute info message (H5Oainfo.c:76-77).
const (
	ainfoTrackCorder = 0x01 // H5O_AINFO_TRACK_CORDER
	ainfoIndexCorder = 0x02 // H5O_AINFO_INDEX_CORDER

	// ainfoMaxCrtIdxOffset is the offset of the maximum creation index in
	// an attribute info message that tracks creation order: after the
	// version and flags bytes (H5Oainfo.c:132-138, 206-208).
	ainfoMaxCrtIdxOffset = 2
)

// denseIndexNodeSize is the node size of the B-trees this library writes for
// dense attributes; a loaded tree keeps the node size of its header.
const denseIndexNodeSize = 4096

// attributeInfoMessage returns the attribute info message of an object
// header: the one with the C message ID (H5O_AINFO_ID, H5Oprivate.h:228), or
// else one with the ID this library used up to v0.14.2 (0x000F,
// core.IsAttributeInfoMessage).
func attributeInfoMessage(oh *core.ObjectHeader) *core.HeaderMessage {
	var legacy *core.HeaderMessage
	for _, msg := range oh.Messages {
		switch {
		case msg.Type == core.MsgAttributeInfo:
			return msg
		case legacy == nil && core.IsAttributeInfoMessage(msg.Type):
			legacy = msg
		}
	}
	return legacy
}

// denseAttributeInfo returns the attribute info message of oh and true when
// its attributes are in dense storage, and false when they are compact. The
// C library gives every version 2 object header with attributes an attribute
// info message and leaves its fractal heap address undefined until the
// attributes move to dense storage; only a defined address means dense
// storage (H5Oattribute.c:225-236, 246, 315). The test itself is
// core.AttributeInfoMessage.HasDenseStorage, which the reader uses too.
//
// Every write path asks this function, including OpenDataset for the
// attribute info it caches.
func denseAttributeInfo(oh *core.ObjectHeader, sb *core.Superblock) (*core.AttributeInfoMessage, bool, error) {
	msg := attributeInfoMessage(oh)
	if msg == nil {
		return nil, false, nil
	}
	info, err := core.ParseAttributeInfoMessage(msg.Data, sb)
	if err != nil {
		return nil, false, fmt.Errorf("parse attribute info message: %w", err)
	}
	if !info.HasDenseStorage(sb.OffsetSize) {
		return nil, false, nil
	}
	return info, true, nil
}

// denseAttributeStorage is the dense attribute storage of one object, loaded
// for a read-modify-write: the fractal heap, the name index, the creation
// order index when the object has one, and the attribute info message that
// points at them.
type denseAttributeStorage struct {
	objAddr uint64
	oh      *core.ObjectHeader
	msg     *core.HeaderMessage
	info    *core.AttributeInfoMessage

	heap   *structures.WritableFractalHeap
	names  *structures.WritableBTreeV2
	corder *structures.WritableBTreeV2 // nil unless creation order is indexed

	// infoDirty means the attribute info message must be written back: its
	// maximum creation index grew, or it still has the legacy message ID.
	infoDirty bool
}

// loadDenseAttributeStorage loads the dense attribute storage of the object
// at objAddr, whose header is oh.
//
// A file written by v0.14.2 or earlier holds its attribute name index as a
// link name index (type 5) and its attribute info message under the legacy ID
// 0x000F. The index is turned into the attribute name index C reads (type 8,
// H5Adense.c:211-225) and the message gets the C ID when the storage is
// flushed, so the first change leaves a file the C library reads.
func loadDenseAttributeStorage(fw *FileWriter, objAddr uint64, oh *core.ObjectHeader,
	sb *core.Superblock) (*denseAttributeStorage, error) {
	msg := attributeInfoMessage(oh)
	if msg == nil {
		return nil, fmt.Errorf("attribute info message not found (dense storage not initialized)")
	}
	info, err := core.ParseAttributeInfoMessage(msg.Data, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse attribute info message: %w", err)
	}

	st := &denseAttributeStorage{
		objAddr:   objAddr,
		oh:        oh,
		msg:       msg,
		info:      info,
		infoDirty: msg.Type != core.MsgAttributeInfo,
	}

	r := fw.writer.Reader()
	st.heap = structures.NewAttributeFractalHeap()
	if err := st.heap.LoadFromFile(r, info.FractalHeapAddr, sb); err != nil {
		return nil, fmt.Errorf("failed to load fractal heap: %w", err)
	}

	st.names = structures.NewWritableBTreeV2(denseIndexNodeSize)
	if err := st.names.LoadFromFile(r, info.BTreeNameIndexAddr, sb); err != nil {
		return nil, fmt.Errorf("failed to load attribute name index: %w", err)
	}
	st.names.SetNameResolver(st.recordName(sb))

	switch st.names.Type() {
	case structures.BTreeV2TypeAttrNameIndex:
	case structures.BTreeV2TypeLinkNameIndex:
		if err := st.names.MigrateToAttributeNameIndex(); err != nil {
			return nil, fmt.Errorf("migrate attribute name index: %w", err)
		}
	default:
		return nil, fmt.Errorf("attribute name index at %d has B-tree type %d: %w",
			info.BTreeNameIndexAddr, st.names.Type(), utils.ErrCorrupt)
	}

	if info.Flags&ainfoIndexCorder != 0 {
		st.corder = structures.NewAttributeCreationOrderIndex(denseIndexNodeSize)
		if err := st.corder.LoadFromFile(r, info.BTreeOrderIndexAddr, sb); err != nil {
			return nil, fmt.Errorf("failed to load attribute creation order index: %w", err)
		}
		if st.corder.Type() != structures.BTreeV2TypeAttrCorderIndex {
			return nil, fmt.Errorf("attribute creation order index at %d has B-tree type %d: %w",
				info.BTreeOrderIndexAddr, st.corder.Type(), utils.ErrCorrupt)
		}
	}

	if err := st.adoptHeapObjects(); err != nil {
		return nil, err
	}

	return st, nil
}

// adoptHeapObjects hands the fractal heap the heap IDs of every attribute the
// indexes point at, so that it knows which space is live. A heap written by
// v0.14.2 or earlier counted object offsets from the end of the direct block
// prefix; the heap moves its objects to the C layout (offsets that count the
// prefix, H5HFman.c:386-399), and every index record gets its new heap ID in
// the same flush.
func (st *denseAttributeStorage) adoptHeapObjects() error {
	indexes := []*structures.WritableBTreeV2{st.names}
	if st.corder != nil {
		indexes = append(indexes, st.corder)
	}
	var ids [][]byte
	for _, idx := range indexes {
		for _, rec := range idx.GetRecords() {
			// A shared attribute's ID points into the shared message heap,
			// not this one. Only a C heap can hold such records (this
			// writer never wrote them), and a C heap keeps its heap IDs.
			if rec.Flags&structures.AttrRecordFlagShared != 0 {
				continue
			}
			ids = append(ids, slices.Clone(rec.HeapID[:]))
		}
	}
	remap, err := st.heap.AdoptObjects(ids)
	if err != nil {
		return fmt.Errorf("dense attribute fractal heap: %w", err)
	}
	if remap == nil {
		return nil
	}
	for _, idx := range indexes {
		if err := idx.RemapHeapIDs(remap); err != nil {
			return fmt.Errorf("rewrite dense attribute heap IDs: %w", err)
		}
	}
	return nil
}

// recordName returns the resolver of the name index: the name of the
// attribute a record points at, read from the fractal heap, as the C compare
// callback reads it to break hash ties (H5Abtree2.c:225-268). Shared
// attributes live in the shared message heap, which this writer does not
// open.
func (st *denseAttributeStorage) recordName(sb *core.Superblock) structures.NameResolver {
	return func(rec structures.IndexRecord) (string, error) {
		if rec.Flags&structures.AttrRecordFlagShared != 0 {
			return "", fmt.Errorf("dense attribute stored in the shared message heap: %w", utils.ErrNotSupported)
		}
		data, err := st.heap.GetObject(rec.HeapID[:])
		if err != nil {
			return "", fmt.Errorf("read attribute from fractal heap: %w", err)
		}
		attr, err := core.ParseAttributeMessage(data, sb.Endianness)
		if err != nil {
			return "", fmt.Errorf("parse attribute from fractal heap: %w", err)
		}
		return attr.Name, nil
	}
}

// lookup returns the name index record of name and refuses a shared one.
func (st *denseAttributeStorage) lookup(name string) (structures.IndexRecord, bool, error) {
	rec, found, err := st.names.FindRecord(name)
	if err != nil || !found {
		return rec, found, err
	}
	if rec.Flags&structures.AttrRecordFlagShared != 0 {
		return rec, true, fmt.Errorf("attribute %q is stored in the shared message heap: %w",
			name, utils.ErrNotSupported)
	}
	return rec, true, nil
}

// upsert writes the encoded attribute message attrMsg of attr: it replaces
// the attribute of that name or inserts a new one (H5Adense.c:470-540,
// 560-700).
func (st *denseAttributeStorage) upsert(name string, attr *core.Attribute, attrMsg []byte) error {
	rec, found, err := st.lookup(name)
	if err != nil {
		return err
	}
	if found {
		return st.modify(name, rec, attr, attrMsg)
	}
	return st.insert(name, attrMsg)
}

// modify replaces an attribute. A new heap ID goes into the creation order
// index as well (H5Adense.c:616-636).
func (st *denseAttributeStorage) modify(name string, rec structures.IndexRecord,
	attr *core.Attribute, attrMsg []byte) error {
	attr.Data = attrMsg
	if err := core.ModifyDenseAttribute(st.heap, st.names, name, attr); err != nil {
		return fmt.Errorf("failed to modify existing dense attribute: %w", err)
	}
	if st.corder == nil {
		return nil
	}
	updated, _, err := st.names.FindRecord(name)
	if err != nil {
		return err
	}
	if updated.HeapID == rec.HeapID {
		return nil
	}
	if err := st.corder.UpdateCreationOrderRecord(rec.CreationOrder, updated.HeapID); err != nil {
		return fmt.Errorf("update attribute creation order index: %w", err)
	}
	return nil
}

// insert adds a new attribute. Its creation index is the next one of the
// object when the object tracks creation order, the bogus 65535 otherwise
// (H5Oattribute.c:283-295); the creation order index gets a record when the
// object has one (H5Adense.c:505-530).
func (st *denseAttributeStorage) insert(name string, attrMsg []byte) error {
	corder := structures.CreationOrderUntracked
	if st.info.Flags&ainfoTrackCorder != 0 {
		if st.info.MaxCreationIndex >= uint64(structures.CreationOrderUntracked) {
			return fmt.Errorf("attribute creation index can't be incremented (H5Oattribute.c:287-288)")
		}
		corder = uint32(st.info.MaxCreationIndex) // below 65535, checked above
		st.info.MaxCreationIndex++
		st.infoDirty = true
	}

	idBytes, err := st.heap.InsertObject(attrMsg)
	if err != nil {
		return fmt.Errorf("failed to insert into heap: %w", err)
	}
	var id [8]byte
	if len(idBytes) != len(id) {
		return fmt.Errorf("unexpected heap ID length: %d bytes", len(idBytes))
	}
	copy(id[:], idBytes)

	if err := st.names.InsertNameRecord(name, id, 0, corder); err != nil {
		return fmt.Errorf("failed to insert into attribute name index: %w", err)
	}
	if st.corder != nil {
		if err := st.corder.InsertCreationOrderRecord(id, 0, corder); err != nil {
			return fmt.Errorf("failed to insert into attribute creation order index: %w", err)
		}
	}
	return nil
}

// remove deletes an attribute from both indexes and the heap
// (H5Adense.c:880-960).
func (st *denseAttributeStorage) remove(name string, rebalance bool) error {
	rec, found, err := st.lookup(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("attribute %q not found in dense storage", name)
	}
	if err := core.DeleteDenseAttribute(st.heap, st.names, name, rebalance); err != nil {
		return fmt.Errorf("failed to delete dense attribute: %w", err)
	}
	if st.corder != nil {
		if err := st.corder.DeleteCreationOrderRecord(rec.CreationOrder); err != nil {
			return fmt.Errorf("delete from attribute creation order index: %w", err)
		}
	}
	return nil
}

// flush writes the heap and the indexes back in place, and the attribute info
// message when it changed. Every check runs before the first write, so a
// corrupt attribute info message fails the operation without touching the file.
func (st *denseAttributeStorage) flush(fw *FileWriter, sb *core.Superblock) error {
	var info []byte
	if st.infoDirty {
		// The message keeps its size: only the maximum creation index and the
		// message ID change, so it is patched where it is.
		info = append([]byte(nil), st.msg.Data...)
		if st.info.Flags&ainfoTrackCorder != 0 {
			if len(info) < ainfoMaxCrtIdxOffset+2 {
				return fmt.Errorf("attribute info message of %d bytes: %w", len(info), utils.ErrCorrupt)
			}
			binary.LittleEndian.PutUint16(info[ainfoMaxCrtIdxOffset:], uint16(st.info.MaxCreationIndex)) //nolint:gosec // at most 65535
		}
	}

	if err := st.heap.WriteAt(fw.writer, sb); err != nil {
		return fmt.Errorf("failed to write updated heap: %w", err)
	}
	if err := st.names.WriteAt(fw.writer, sb); err != nil {
		return fmt.Errorf("failed to write attribute name index: %w", err)
	}
	if st.corder != nil {
		if err := st.corder.WriteAt(fw.writer, sb); err != nil {
			return fmt.Errorf("failed to write attribute creation order index: %w", err)
		}
	}
	if info == nil {
		return nil
	}
	if err := fw.patchHeaderMessage(st.objAddr, st.oh, st.msg, core.MsgAttributeInfo, info); err != nil {
		return fmt.Errorf("write attribute info message: %w", err)
	}
	return nil
}

// patchHeaderMessage overwrites the type and data of msg, a message of the
// object header at objAddr as core.ReadObjectHeader returned it in oh, where
// it is in the file, and recomputes the checksum of the version 2 chunk that
// holds it (H5Ocache.c:1150-1175). The data keeps its size.
//
// This changes a message without rewriting the header, which also works for
// version 1 headers and for headers that track attribute creation order,
// whose message headers carry a creation index (H5Opkg.h:85-107).
func (fw *FileWriter) patchHeaderMessage(objAddr uint64, oh *core.ObjectHeader, msg *core.HeaderMessage,
	typ core.MessageType, data []byte) error {
	if len(data) != len(msg.Data) {
		return fmt.Errorf("patch of a %d-byte message with %d bytes", len(msg.Data), len(data))
	}

	if oh.Version == 1 {
		// Type(2), size(2), flags(1), reserved(3) (H5Ocache.c:1270-1290).
		var head [2]byte
		binary.LittleEndian.PutUint16(head[:], uint16(typ))
		if err := fw.writer.WriteAtAddress(head[:], msg.Offset); err != nil {
			return fmt.Errorf("write message type: %w", err)
		}
		if err := fw.writer.WriteAtAddress(data, msg.Offset+8); err != nil {
			return fmt.Errorf("write message data: %w", err)
		}
		msg.Type, msg.Data = typ, data
		return nil
	}

	if typ > 0xFF {
		return fmt.Errorf("message type %d in a version 2 header: %w", typ, utils.ErrNotSupported)
	}
	headerSize := core.V2MessageHeaderSize(oh.Flags)
	start, size, err := fw.headerChunkOf(objAddr, oh, msg.Offset)
	if err != nil {
		return err
	}
	if msg.Offset+headerSize+uint64(len(data)) > start+size-ohdrChecksumSize {
		return fmt.Errorf("message at %d runs past its chunk: %w", msg.Offset, utils.ErrCorrupt)
	}

	chunk, err := core.ReadBlock(fw.writer.Reader(), fw.file.sb, start, size, size, "object header chunk")
	if err != nil {
		return fmt.Errorf("read object header chunk at %d: %w", start, err)
	}
	at := msg.Offset - start
	chunk[at] = byte(typ)
	copy(chunk[at+headerSize:], data)
	binary.LittleEndian.PutUint32(chunk[size-ohdrChecksumSize:], core.JenkinsChecksum(chunk[:size-ohdrChecksumSize]))
	if err := fw.writer.WriteAtAddress(chunk, start); err != nil {
		return fmt.Errorf("write object header chunk at %d: %w", start, err)
	}
	msg.Type, msg.Data = typ, data
	return nil
}

// headerChunkOf returns the address and size of the version 2 chunk of the
// object header at objAddr that holds the byte at addr: chunk 0 or one of the
// continuation chunks (OCHK) its continuation messages name.
func (fw *FileWriter) headerChunkOf(objAddr uint64, oh *core.ObjectHeader, addr uint64) (uint64, uint64, error) {
	size0, err := readChunk0Size(fw, objAddr)
	if err != nil {
		return 0, 0, err
	}
	if addr >= objAddr && addr < objAddr+size0 {
		return objAddr, size0, nil
	}

	sb := fw.file.sb
	osz, lsz := int(sb.OffsetSize), int(sb.LengthSize)
	for _, m := range oh.Messages {
		if m.Type != core.MsgContinuation {
			continue
		}
		if len(m.Data) < osz+lsz {
			return 0, 0, fmt.Errorf("continuation message of %d bytes: %w", len(m.Data), utils.ErrCorrupt)
		}
		start := readAddressLE(m.Data[:osz])
		size := readAddressLE(m.Data[osz : osz+lsz])
		if addr >= start && addr-start < size {
			return start, size, nil
		}
	}
	return 0, 0, fmt.Errorf("message at %d is in no chunk of the object header at %d: %w",
		addr, objAddr, utils.ErrCorrupt)
}
