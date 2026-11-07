package hdf5

import (
	"errors"
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/writer"
)

// undefinedAddress is the HDF5 undefined address sentinel value.
// Objects with this address are not stored on disk and should be skipped.
const undefinedAddress = uint64(0xFFFFFFFFFFFFFFFF)

// Delete removes the link at path, as H5Ldelete does, and deletes what is no
// longer linked.
//
// This performs a full deletion:
//  1. Removes the link from its parent group
//  2. Decrements the reference count (hard link count) of the object it names
//  3. If the count reaches 0, frees the object's storage: the object header
//     and its continuation chunks, contiguous data, every chunk and every
//     block of the chunk index, dense attribute storage, and a group's
//     structures (a symbol table's B-tree and local heap, or the fractal
//     heap and indexes of dense link storage)
//  4. For a group freed that way, decrements the count of every object its
//     hard links name, in turn: a whole tree is deleted, while an object
//     that another link still names (a hard link from outside the tree)
//     keeps its storage with its count lowered
//
// A hard link from inside the tree back to the group (a cycle) counts as one
// of the group's links, so such a group keeps its storage, unreachable, as
// in the C library.
//
// Constraints:
//   - Cannot delete the root group "/"
//   - Cannot free an object that holds a reference into the file's shared
//     object header message heap (a message, an attribute or an attribute's
//     datatype or dataspace stored there): the error wraps ErrNotSupported
//   - An object named by more links of the tree than its reference count is
//     refused as corrupt (ErrCorrupt)
//   - Path must start with "/"
//   - Object must exist
//
// Every refusal comes before anything is written, so the file is left
// unchanged.
//
// A DatasetWriter or GroupWriter obtained earlier for an object the delete
// frees must not be used afterwards: its storage is free space that later
// writes reuse.
//
// Parameters:
//   - path: Absolute path to the object (e.g., "/dataset1", "/group1/data")
//
// Returns:
//   - error: If deletion fails
//
// Example:
//
//	fw, _ := hdf5.OpenForWrite("data.h5", hdf5.OpenReadWrite)
//	defer fw.Close()
//	fw.Delete("/old_dataset")       // Remove a dataset
//	fw.Delete("/old_group")         // Remove a group and everything only it links to
//
// Reference: H5Ldelete.c, H5G_obj_remove(), H5O_link(adjust=-1), H5O_delete(),
// H5G__node_remove (H5Gnode.c:774-784), H5G__dense_delete (H5Gdense.c:1581).
func (fw *FileWriter) Delete(path string) error {
	// Validate path.
	if path == "" {
		return fmt.Errorf("delete: path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("delete: path must start with '/' (got %q)", path)
	}
	if path == "/" {
		return fmt.Errorf("delete: cannot delete root group")
	}

	// Parse path into parent + name.
	parent, name := parsePath(path)

	// Step 1: Everything that can refuse the delete runs before the first
	// write, so a refused delete leaves the file as it was: the lookup, and
	// the plan of every count the delete lowers and every block it frees,
	// checked against the allocator. (The C library decrements the counts
	// while it removes the entries, H5Gnode.c:774-784; the result is the same
	// once the delete is complete.)
	objectAddr, err := fw.resolveObjectAddress(path)
	if err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	sb := fw.file.Superblock()
	var plan *deletePlan
	if objectAddr != undefinedAddress && objectAddr != 0 {
		if plan, err = fw.planDelete(objectAddr, sb); err != nil {
			return fmt.Errorf("delete %q: %w", path, err)
		}
	}

	// Step 2: Unlink from parent group.
	if _, err := fw.unlinkFromParent(parent, name); err != nil {
		return fmt.Errorf("delete %q: unlink failed: %w", path, err)
	}
	if plan == nil {
		return nil
	}

	// Steps 3 and 4 change the file after the unlink; whatever happens there,
	// what was tracked for the deleted path and the freed objects is dropped
	// (step 5), so a later create cannot use a registration of the tree.
	if err := fw.applyDelete(plan, sb); err != nil {
		return errors.Join(fmt.Errorf("delete %q: %w", path, err), fw.forgetDeleted(path, plan))
	}
	if err := fw.forgetDeleted(path, plan); err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	return nil
}

// applyDelete writes what plan says after the unlink: the lowered counts
// (step 3), then the frees (step 4).
func (fw *FileWriter) applyDelete(plan *deletePlan, sb *core.Superblock) error {
	// Step 3: Objects that keep other links get their lowered count. Each
	// header is read again here, after the unlink: the unlink may have
	// rewritten it (a parent losing the link to a child that links back to
	// it), and only the count may change now. V1 OHDR: the count field of
	// the prefix; V2 OHDR: the RefCount message (type 22), 1 if absent.
	for _, addr := range plan.loweredOrder {
		oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
		if err != nil {
			return fmt.Errorf("read object header at 0x%X to lower its count: %w", addr, err)
		}
		dec := plan.lowered[addr]
		if oh.ReferenceCount <= dec {
			return fmt.Errorf("%w: object at 0x%X has count %d after the unlink, lowering it by %d would free it",
				utils.ErrCorrupt, addr, oh.ReferenceCount, dec)
		}
		oh.ReferenceCount -= dec
		if err := fw.writeRefCount(addr, oh, sb); err != nil {
			return fmt.Errorf("update reference count at 0x%X: %w", addr, err)
		}
	}

	// Step 4: Free every block of every object whose last link went,
	// checked in step 1 (H5O_delete, H5Oint.c:1508-1600). A Free error here
	// is a bug in that check, or space the unlink of step 2 freed overlapping
	// the tree in a crafted file; Free refuses an overlap with free space.
	allocator := fw.writer.Allocator()
	for _, r := range plan.freed {
		if err := allocator.Free(r.Offset, r.Size); err != nil {
			return fmt.Errorf("free [0x%X, +%d) after the check passed: %w", r.Offset, r.Size, err)
		}
	}
	return nil
}

// forgetDeleted is step 5 of Delete: it drops what was tracked for the freed
// objects, their header allocations, which headerSpace trusts over the disk
// (a stale size would make a later, smaller header at the same address be
// rewritten past its allocation), and the group entries at and below path or
// of a freed group.
func (fw *FileWriter) forgetDeleted(path string, plan *deletePlan) error {
	for _, addr := range plan.freedObjects {
		delete(fw.headerAllocs, addr)
	}
	return fw.forgetGroupsBelow(path, plan)
}

// deletePlan is what a delete changes, collected before its first write.
type deletePlan struct {
	// freed holds every block of every object whose last link goes;
	// freedObjects their header addresses.
	freed        []writer.FreeBlock
	freedObjects []uint64

	// lowered holds, for each object that keeps other links, by how much its
	// count drops; loweredOrder their addresses in the order they were
	// reached, so the writes are deterministic. Only the decrement is kept:
	// the header is read again when the count is written, after the unlink
	// changed the file (applyDelete).
	lowered      map[uint64]uint32
	loweredOrder []uint64
}

// planDelete collects what removing one link to the object at objectAddr
// changes, as H5O_link(-1) and H5O_delete do it in turn (H5Oint.c:1508-1600):
// the object's count drops by one; at zero its storage is freed and, for a
// group, every object one of its hard links names gets the same treatment
// (H5G__node_remove, H5Gnode.c:774-784; H5O_link_delete for link messages;
// H5G__dense_delete with adj_link, H5Gdense.c:1581-1650). It only reads.
//
// The walk is a queue over decrements, not a recursion: a deep tree cannot
// grow the stack, and an object freed earlier in the walk that is reached
// again is a count below its links, refused as ErrCorrupt. The decrements
// and the blocks are bounded by the read limit, as deleteRanges bounds its
// blocks. Only counts are kept between decrements, never parsed headers, so
// what the walk holds does not grow with the size of the headers it reads.
//
// The blocks freed are checked together against the allocator, with the
// first chunk of every surviving header among them: no freed block may
// overlap another, free space, or a header that is written afterwards.
func (fw *FileWriter) planDelete(objectAddr uint64, sb *core.Superblock) (*deletePlan, error) {
	limit := fw.file.ReadLimit()
	p := &deletePlanner{
		fw:         fw,
		sb:         sb,
		limit:      limit,
		maxEntries: limit / 16, // each kept range or queued decrement, as CollectChunks charges entries
		plan:       &deletePlan{lowered: make(map[uint64]uint32)},
		freedSet:   make(map[uint64]struct{}),
		counts:     make(map[uint64]uint32),
		queue:      []uint64{objectAddr},
		queued:     1,
	}
	for len(p.queue) > 0 {
		addr := p.queue[0]
		p.queue = p.queue[1:]
		if err := p.decrement(addr); err != nil {
			return nil, err
		}
	}
	return p.finish()
}

// deletePlanner is the state of one planDelete walk.
type deletePlanner struct {
	fw                *FileWriter
	sb                *core.Superblock
	limit, maxEntries uint64

	plan     *deletePlan
	freedSet map[uint64]struct{}
	counts   map[uint64]uint32 // the count, as lowered so far, of each object that kept links

	queue  []uint64 // decrements still to apply
	queued uint64
}

// countBelowLinks is the refusal of an object named by more links than its
// reference count.
func countBelowLinks(addr uint64) error {
	return fmt.Errorf("%w: object at 0x%X is named by more links than its reference count", utils.ErrCorrupt, addr)
}

// decrement applies one H5O_link(-1) to the object at addr: it lowers the
// count and, at zero, collects the object's blocks and queues the objects
// its hard links name.
func (p *deletePlanner) decrement(addr uint64) error {
	if _, freed := p.freedSet[addr]; freed {
		return countBelowLinks(addr)
	}
	count, known := p.counts[addr]
	var oh *core.ObjectHeader
	if !known {
		var err error
		if oh, err = p.readHeader(addr); err != nil {
			return err
		}
		count = oh.ReferenceCount
	}
	if count == 0 {
		return countBelowLinks(addr)
	}
	count--
	if count > 0 {
		if !known {
			p.plan.loweredOrder = append(p.plan.loweredOrder, addr)
		}
		p.counts[addr] = count
		p.plan.lowered[addr]++
		return nil
	}

	// The last link goes: the object is freed. A header read for an
	// earlier decrement was not kept, so it is read again here.
	if oh == nil {
		var err error
		if oh, err = p.readHeader(addr); err != nil {
			return err
		}
	}
	delete(p.counts, addr)
	delete(p.plan.lowered, addr)
	p.freedSet[addr] = struct{}{}
	p.plan.freedObjects = append(p.plan.freedObjects, addr)
	if err := p.fw.objectRanges(addr, oh, p.sb, p.limit, p.add, p.child); err != nil {
		return fmt.Errorf("object at 0x%X: %w", addr, err)
	}
	return nil
}

// readHeader reads the object header at addr.
func (p *deletePlanner) readHeader(addr uint64) (*core.ObjectHeader, error) {
	oh, err := core.ReadObjectHeader(p.fw.writer.Reader(), addr, p.sb)
	if err != nil {
		return nil, fmt.Errorf("read object header at 0x%X: %w", addr, err)
	}
	return oh, nil
}

// add keeps one block a freed object holds.
func (p *deletePlanner) add(addr, size uint64) error {
	if uint64(len(p.plan.freed)) >= p.maxEntries {
		return fmt.Errorf("%w: the deleted objects' storage has more than %d blocks, more than the read limit of %d bytes holds",
			utils.ErrLimit, len(p.plan.freed), p.limit)
	}
	p.plan.freed = append(p.plan.freed, writer.FreeBlock{Offset: addr, Size: size})
	return nil
}

// child queues the decrement of the object a hard link of a freed group
// names; a soft or external link names none (undefinedAddress).
func (p *deletePlanner) child(addr uint64) error {
	if addr == undefinedAddress {
		return nil
	}
	if addr == 0 {
		return fmt.Errorf("%w: hard link to address 0 (the superblock)", utils.ErrCorrupt)
	}
	if p.queued >= p.maxEntries {
		return fmt.Errorf("%w: the deleted group tree has more than %d links, more than the read limit of %d bytes holds",
			utils.ErrLimit, p.queued, p.limit)
	}
	p.queued++
	p.queue = append(p.queue, addr)
	return nil
}

// finish drops from loweredOrder the objects freed after they were first
// lowered, and checks the freed blocks and the first chunks of the
// surviving headers together against the allocator.
func (p *deletePlanner) finish() (*deletePlan, error) {
	plan := p.plan
	kept := plan.loweredOrder[:0]
	for _, addr := range plan.loweredOrder {
		if _, ok := plan.lowered[addr]; ok {
			kept = append(kept, addr)
		}
	}
	plan.loweredOrder = kept

	check := make([]writer.FreeBlock, len(plan.freed), len(plan.freed)+len(plan.loweredOrder))
	copy(check, plan.freed)
	for _, addr := range plan.loweredOrder {
		size0, err := readChunk0Size(p.fw, addr)
		if err != nil {
			return nil, err
		}
		check = append(check, writer.FreeBlock{Offset: addr, Size: size0})
	}
	if err := p.fw.writer.Allocator().CheckFree(check); err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}
	return plan, nil
}

// forgetGroupsBelow drops the group entries at path and below it after a
// delete, and the entries of every group the delete freed. A group that
// survives the delete (another hard link names it) but
// was registered under a path below path, as loadExistingGroups registers a
// shared group once, is registered again by reloading the groups from the
// file.
func (fw *FileWriter) forgetGroupsBelow(path string, plan *deletePlan) error {
	freed := make(map[uint64]struct{}, len(plan.freedObjects))
	for _, addr := range plan.freedObjects {
		freed[addr] = struct{}{}
	}
	reload := false
	prefix := path + "/"
	for p, meta := range fw.groups {
		// A group freed by the delete is dropped under any path: in a file
		// whose counts are below its links it may be registered outside
		// path too, and its header space is free now.
		if _, gone := freed[meta.headerAddr]; gone {
			delete(fw.groups, p)
			continue
		}
		if p != path && !strings.HasPrefix(p, prefix) {
			continue
		}
		if _, survives := plan.lowered[meta.headerAddr]; survives {
			reload = true
		}
		delete(fw.groups, p)
	}
	if !reload {
		return nil
	}
	if err := fw.loadExistingGroups(); err != nil {
		return fmt.Errorf("delete %q: reload groups: %w", path, err)
	}
	return nil
}

// deleteRanges collects every block of file space the object at objectAddr
// holds, as H5O_delete frees it once the last link goes (H5Oint.c:1508-1600):
// each chunk of the object header, chunk 0 and the continuation chunks
// (H5O__cont_delete, H5Ocont.c:207-220); contiguous data (H5D__contig_delete,
// H5Dcontig.c:352); every chunk and every block of the chunk index
// (core.DatasetDescriptor.StorageBlocks); a group's B-tree nodes, symbol
// table nodes and local heap (H5G__stab_delete). Then it checks them against
// the allocator without changing it (writer.Allocator.CheckFree): every range
// lies in the file and overlaps neither free space nor another range.
//
// Dense attribute storage (H5O__ainfo_delete → H5A__dense_delete,
// H5Oainfo.c:327-342) and dense link storage (H5O__linfo_delete →
// H5G__dense_delete, H5Olinfo.c:331-344) are collected by
// core.DenseAttributeStorageBlocks and core.DenseLinkStorageBlocks: their v2
// B-trees, the fractal heap's blocks, free-space manager and huge objects.
// An object holding a reference into the shared object header message heap
// is refused (core.CheckNoSOHMReferences): C would decrement it there
// (H5SM_delete), which this library cannot do.
//
// It only reads, so a caller runs it before its first write: a structure
// that cannot be read or a range that fails the check refuses the change
// with the file unchanged. It deletes one object with no links of its own
// (discardUnlinkedObject): a group that still has links is refused; Delete
// frees whole trees through planDelete.
func (fw *FileWriter) deleteRanges(objectAddr uint64, oh *core.ObjectHeader, sb *core.Superblock) ([]writer.FreeBlock, error) {
	limit := fw.file.ReadLimit()
	var ranges []writer.FreeBlock
	add := func(addr, size uint64) error {
		// Each kept range costs 16 bytes against the read limit, as
		// CollectChunks charges its entries.
		if uint64(len(ranges)) >= limit/16 {
			return fmt.Errorf("%w: the object's storage has more than %d blocks, more than the read limit of %d bytes holds",
				utils.ErrLimit, len(ranges), limit)
		}
		ranges = append(ranges, writer.FreeBlock{Offset: addr, Size: size})
		return nil
	}
	if err := fw.objectRanges(objectAddr, oh, sb, limit, add, nil); err != nil {
		return nil, err
	}
	if err := fw.writer.Allocator().CheckFree(ranges); err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}
	return ranges, nil
}

// objectRanges passes every block the object at objectAddr holds to add
// (deleteRanges lists them), and the address each hard link of a group names
// to child (undefinedAddress for a soft or external link). A nil child
// refuses a group that still has links. It refuses an object holding a
// reference into the shared object header message heap first.
func (fw *FileWriter) objectRanges(objectAddr uint64, oh *core.ObjectHeader, sb *core.Superblock, limit uint64,
	add func(addr, size uint64) error, child func(addr uint64) error,
) error {
	if err := core.CheckNoSOHMReferences(sb, oh.Messages); err != nil {
		return err
	}
	size0, err := readChunk0Size(fw, objectAddr)
	if err != nil {
		return err
	}
	if err := add(objectAddr, size0); err != nil {
		return err
	}
	ainfo := attributeInfoMessage(oh)
	for _, msg := range oh.Messages {
		if err := fw.messageRanges(oh, msg, msg == ainfo, sb, limit, add, child); err != nil {
			return err
		}
	}
	return nil
}

// messageRanges collects the blocks one message of oh refers to
// (deleteRanges), and passes the target of a link message to child
// (objectRanges); isAinfo marks the object's attribute info message.
func (fw *FileWriter) messageRanges(oh *core.ObjectHeader, msg *core.HeaderMessage, isAinfo bool, sb *core.Superblock,
	limit uint64, add func(addr, size uint64) error, child func(addr uint64) error,
) error {
	switch msg.Type {
	case core.MsgContinuation:
		addr, length, err := continuationMessage(msg.Data, sb)
		if err != nil {
			return err
		}
		return add(addr, length)

	case core.MsgDataLayout:
		return fw.layoutRanges(oh, msg.Data, sb, limit, add)

	case core.MsgSymbolTable:
		return fw.groupStructureRanges(msg.Data, sb, add, child)

	case core.MsgLinkInfo:
		return fw.denseLinkRanges(msg.Data, sb, add, child)

	case core.MsgLinkMessage:
		// A compact link: H5O__link_delete → H5O_link_delete lowers the count
		// of the object a hard link names (H5Olink.c).
		if child == nil {
			return nonEmptyGroupError(1)
		}
		lm, err := core.ParseLinkMessage(msg.Data, sb)
		if err != nil {
			return fmt.Errorf("%w: link message: %w", utils.ErrCorrupt, err)
		}
		target, err := linkTarget(lm, sb)
		if err != nil {
			return err
		}
		return child(target)

	default:
		if isAinfo {
			return fw.denseAttributeRanges(msg.Data, sb, add)
		}
		return nil
	}
}

// denseAttributeRanges collects the dense attribute storage an attribute
// info message names (H5O__ainfo_delete, H5Oainfo.c:327-342), nothing while
// the attributes are compact (an undefined heap address).
func (fw *FileWriter) denseAttributeRanges(data []byte, sb *core.Superblock, add func(addr, size uint64) error) error {
	ainfo, err := core.ParseAttributeInfoMessage(data, sb)
	if err != nil {
		return fmt.Errorf("%w: attribute info message: %w", utils.ErrCorrupt, err)
	}
	if !ainfo.HasDenseStorage(sb.OffsetSize) {
		return nil
	}
	if _, err := core.DenseAttributeStorageBlocks(fw.writer.Reader(), sb, ainfo, add); err != nil {
		return fmt.Errorf("dense attribute storage: %w", err)
	}
	return nil
}

// denseLinkRanges collects the dense link storage a link info message names
// (H5O__linfo_delete, H5Olinfo.c:331-344), nothing for a group whose links
// are compact, and passes the target of every link it holds to child, as
// H5G__dense_delete with adj_link lowers their counts (H5Gdense.c:1581-1650).
// A nil child refuses storage that still holds links.
func (fw *FileWriter) denseLinkRanges(data []byte, sb *core.Superblock, add func(addr, size uint64) error,
	child func(addr uint64) error,
) error {
	linfo, err := core.ParseLinkInfoMessage(data, sb)
	if err != nil {
		return fmt.Errorf("%w: link info message: %w", utils.ErrCorrupt, err)
	}
	if !linfo.HasFractalHeap() {
		return nil
	}
	dense, err := core.DenseLinkStorageBlocks(fw.writer.Reader(), sb, linfo, add)
	if err != nil {
		return fmt.Errorf("dense link storage: %w", err)
	}
	if dense.Records == 0 {
		return nil
	}
	if child == nil {
		return nonEmptyGroupError(dense.Records)
	}
	links, err := fw.groupLinks(&linkGroup{linfo: linfo})
	if err != nil {
		return err
	}
	for _, lm := range links {
		target, err := linkTarget(lm, sb)
		if err != nil {
			return err
		}
		if err := child(target); err != nil {
			return err
		}
	}
	return nil
}

// continuationMessage decodes a continuation message: the chunk's address
// and its length, of the file's offset and length sizes (H5O__cont_decode,
// H5Ocont.c).
func continuationMessage(data []byte, sb *core.Superblock) (addr, length uint64, err error) {
	os, ls := int(sb.OffsetSize), int(sb.LengthSize)
	if len(data) < os+ls {
		return 0, 0, fmt.Errorf("continuation message of %d bytes, need %d: %w", len(data), os+ls, utils.ErrCorrupt)
	}
	return readAddrFromBuf(data, os, sb.Endianness), readAddrFromBuf(data[os:], ls, sb.Endianness), nil
}

// layoutRanges collects the data blocks of a data layout message: the one
// block of contiguous data, every chunk and chunk index block of chunked
// data (as StorageBlocks reports them), nothing for compact data, which lives
// in the message. An undefined address is storage never allocated.
func (fw *FileWriter) layoutRanges(oh *core.ObjectHeader, data []byte, sb *core.Superblock, limit uint64,
	add func(addr, size uint64) error,
) error {
	layout, err := core.ParseDataLayoutMessage(data, sb)
	if err != nil {
		return fmt.Errorf("data layout message: %w", err)
	}

	switch {
	case layout.IsContiguous():
		if layout.DataAddress == undefinedAddress || layout.DataSize == 0 {
			return nil
		}
		return add(layout.DataAddress, layout.DataSize)

	case layout.IsChunked():
		if layout.DataAddress == undefinedAddress {
			return nil
		}
		desc, err := core.NewDatasetDescriptor(fw.writer.Reader(), oh, sb)
		if err != nil {
			return fmt.Errorf("dataset messages: %w", err)
		}
		if err := desc.StorageBlocks(fw.writer.Reader(), sb, limit, add); err != nil {
			return fmt.Errorf("chunk index: %w", err)
		}
	}

	return nil
}

// writeRefCount rewrites the object header with an updated reference count.
// For V2 headers, this adds/updates a RefCount message.
// For V1 headers, the refcount is the 4-byte field of the header prefix
// (H5O__cache_serialize, H5Ocache.c), written in place.
func (fw *FileWriter) writeRefCount(addr uint64, oh *core.ObjectHeader, sb *core.Superblock) error {
	switch oh.Version {
	case 1:
		return writeV1RefCount(fw, addr, oh)
	case 2:
	default:
		return fmt.Errorf("object header version %d at 0x%X: %w", oh.Version, addr, utils.ErrNotSupported)
	}

	// V2: Update or add RefCount message (type 0x0016).
	// Body: version byte (0) + uint32 count (H5Orefcount.c).
	refCountData := make([]byte, 5)
	refCountData[0] = 0 // message version
	sb.Endianness.PutUint32(refCountData[1:], oh.ReferenceCount)

	found := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgRefCount {
			if len(msg.Data) == 4 {
				// Pre-v0.15 layout without the version byte — replace it.
				msg.Data = make([]byte, 5)
			}
			copy(msg.Data, refCountData)
			found = true
			break
		}
	}
	if !found {
		// Add a RefCount message if one doesn't exist yet.
		if err := core.AddMessageToObjectHeader(oh, core.MsgRefCount, refCountData); err != nil {
			return fmt.Errorf("add refcount message: %w", err)
		}
	}

	// Bounds-checked write: the header may have grown past its allocation
	// at the end of the file (keeps the superblock EOA correct).
	return writeOHDRWithBoundsCheck(fw, addr, oh, sb)
}

// groupStructureRanges collects the B-tree nodes, symbol table nodes and
// local heap of a group, as H5G__stab_delete frees them through H5B_delete
// and H5HL_delete (H5Gstab.c:200-245, H5B.c:1580-1638). The local heap is one
// block when its data segment follows the prefix (a single cache object),
// otherwise the prefix and the data segment (H5HL_delete, H5HL.c:847-900).
// The object address of every entry goes to child, as H5G__node_remove
// lowers the count of the object a hard link names (H5Gnode.c:774-784); a
// soft link's entry has an undefined address. A nil child refuses a group
// that still has entries.
func (fw *FileWriter) groupStructureRanges(symTableData []byte, sb *core.Superblock, add func(addr, size uint64) error,
	child func(addr uint64) error,
) error {
	btreeAddr, heapAddr, err := symbolTableMessageAddrs(symTableData, sb)
	if err != nil {
		return err
	}

	g := fw.groupSymbolTable(nil)
	btreeNodes, snods, err := g.groupNodes(btreeAddr)
	if err != nil {
		return fmt.Errorf("read group B-tree: %w", err)
	}
	if err := g.entryTargets(snods, child); err != nil {
		return err
	}

	for _, addr := range snods {
		if err := add(addr, g.l.SNODSize()); err != nil {
			return err
		}
	}
	for _, addr := range btreeNodes {
		if err := add(addr, g.l.NodeSize()); err != nil {
			return err
		}
	}

	heap, err := structures.LoadLocalHeap(fw.writer, heapAddr, sb)
	if err != nil {
		return fmt.Errorf("read local heap at 0x%X: %w", heapAddr, err)
	}
	if heap.Contiguous(heapAddr) {
		return add(heapAddr, heap.Size())
	}
	if err := add(heapAddr, heap.PrefixSize()); err != nil {
		return err
	}
	return add(heap.DataSegmentAddress, heap.DataSegmentSize)
}

// nonEmptyGroupError is the refusal of a group that still has children, by
// a caller that frees one object only (deleteRanges).
func nonEmptyGroupError[T int | uint64](children T) error {
	return fmt.Errorf("cannot delete non-empty group (has %d children); delete children first", children)
}

// entryTargets passes the object address of every entry of the symbol table
// nodes snods to child; with a nil child it refuses nodes that hold entries.
func (g *groupSymbolTable) entryTargets(snods []uint64, child func(addr uint64) error) error {
	children := 0
	for _, addr := range snods {
		sn, err := g.readSNOD(addr)
		if err != nil {
			return err
		}
		children += len(sn.Entries)
		if child == nil {
			continue
		}
		for i := range sn.Entries {
			target := sn.Entries[i].ObjectAddress
			if sn.Entries[i].IsSoftLink() {
				target = undefinedAddress
			}
			if err := child(target); err != nil {
				return err
			}
		}
	}
	if child == nil && children > 0 {
		return nonEmptyGroupError(children)
	}
	return nil
}

// symbolTableMessageAddrs decodes a symbol table message: the B-tree
// address, then the local heap address, each of the file's offset size
// (H5O__stab_decode, H5Ostab.c).
func symbolTableMessageAddrs(data []byte, sb *core.Superblock) (btreeAddr, heapAddr uint64, err error) {
	osSize := int(sb.OffsetSize)
	if len(data) < 2*osSize {
		return 0, 0, fmt.Errorf("symbol table message of %d bytes, need %d: %w", len(data), 2*osSize, utils.ErrCorrupt)
	}
	return readAddrFromBuf(data[0:], osSize, sb.Endianness), readAddrFromBuf(data[osSize:], osSize, sb.Endianness), nil
}
