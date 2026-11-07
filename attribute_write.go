package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/writer"
)

// Attribute storage threshold.
const (
	// MaxCompactAttributes is the threshold for transitioning to dense storage.
	// When an object has 8+ attributes, dense storage (Fractal Heap + B-tree)
	// is more efficient than compact storage (object header messages).
	MaxCompactAttributes = 8
)

// WriteAttribute writes an attribute to a dataset.
//
// Storage strategy (automatic):
//   - 0-7 attributes: Compact storage (object header messages)
//   - 8+ attributes: Dense storage (Fractal Heap + B-tree v2)
//
// Supported value types:
//   - Scalars: int8, int16, int32, int64, uint8, uint16, uint32, uint64, float32, float64
//   - Arrays: []int32, []float64, etc. (1D arrays only)
//   - Strings: string (fixed-length, converted to byte array)
//   - String arrays: []string (variable-length strings via Global Heap)
//
// Parameters:
//   - name: Attribute name (ASCII, no null bytes)
//   - value: Attribute value (Go scalar, slice, or string)
//
// Returns:
//   - error: If attribute cannot be written
//
// Example:
//
//	ds, _ := fw.CreateDataset("/temperature", Float64, []uint64{10})
//	ds.WriteAttribute("units", "Celsius")
//	ds.WriteAttribute("sensor_id", int32(42))
//	ds.WriteAttribute("calibration", []float64{1.0, 0.0})
//	ds.WriteAttribute("topics", []string{"camera", "lidar", "imu"})
//
// Writing a name the dataset already has replaces that attribute's value;
// DeleteAttribute removes one.
//
// Limitations:
//   - No compound types
func (ds *DatasetWriter) WriteAttribute(name string, value interface{}) error {
	// The header is read from the file on every call: an earlier attribute
	// call of this session may have changed it.
	if err := writeAttribute(ds.fileWriter, ds.address, name, value); err != nil {
		return err
	}
	return ds.refreshHeaderCache()
}

// refreshHeaderCache re-reads the object header of a dataset opened with
// OpenDataset, whose cached header and attribute info an attribute call has
// just changed.
func (ds *DatasetWriter) refreshHeaderCache() error {
	if ds.objectHeader == nil {
		return nil
	}
	sb := ds.fileWriter.file.Superblock()
	oh, err := core.ReadObjectHeader(ds.fileWriter.writer.Reader(), ds.address, sb)
	if err != nil {
		return fmt.Errorf("re-read object header: %w", err)
	}
	ds.objectHeader = oh
	info, dense, err := denseAttributeInfo(oh, sb)
	if err != nil {
		return err
	}
	ds.denseAttrInfo = nil
	if dense {
		ds.denseAttrInfo = info
	}
	return nil
}

// DeleteAttribute removes an attribute by name from the dataset.
//
// This method supports both compact and dense attribute storage:
// - Compact storage (0-7 attributes): Removes message from object header
// - Dense storage (8+ attributes): Removes from B-tree and fractal heap
//
// Parameters:
//   - name: Attribute name to delete
//
// Returns:
//   - error: If attribute not found or deletion fails
//
// Reference: H5Adelete.c - H5A__delete(), H5Adense.c - H5A__dense_remove().
func (ds *DatasetWriter) DeleteAttribute(name string) error {
	if err := deleteAttribute(ds.fileWriter, ds.address, name); err != nil {
		return err
	}
	return ds.refreshHeaderCache()
}

// RebalanceAttributeBTree manually triggers B-tree rebalancing for this dataset's dense attribute storage.
//
// Use this when:
//   - You know this specific dataset needs rebalancing
//   - More efficient than RebalanceAllBTrees() for targeted optimization
//   - After batch deletions with rebalancing disabled
//
// Performance (for current MVP with single-leaf B-trees):
//   - Instant (< 1ms) - no-op for single-leaf trees
//
// Future (when multi-level B-trees implemented):
//   - Small (<1000 attrs): <10ms
//   - Medium (1000-10000 attrs): 10-100ms
//   - Large (10000+ attrs): 100ms-1s
//
// Returns:
//   - error: if dataset doesn't use dense storage or rebalancing fails
//
// Example:
//
//	fw.DisableRebalancing()
//	for i := 0; i < 1000; i++ {
//	    ds.DeleteAttribute(fmt.Sprintf("temp_%d", i))  // Fast deletions
//	}
//	ds.RebalanceAttributeBTree()  // Rebalance this dataset only
//
// Reference: Similar to per-object rebalancing in HDF5 (hypothetical - not exposed in C API).
func (ds *DatasetWriter) RebalanceAttributeBTree() error {
	// Check if dataset uses dense attribute storage
	if ds.denseAttrInfo == nil && ds.objectHeader == nil {
		// Dataset doesn't have dense storage (compact or no attributes)
		// Nothing to rebalance
		return nil
	}

	// A dataset opened with OpenDataset caches the attribute info of dense
	// storage only (denseAttributeInfo).
	sb := ds.fileWriter.file.Superblock()
	if ds.denseAttrInfo != nil {
		// Load B-tree from file
		reader := ds.fileWriter.writer.Reader()

		btree := structures.NewWritableBTreeV2(4096)
		err := btree.LoadFromFile(reader, ds.denseAttrInfo.BTreeNameIndexAddr, sb)
		if err != nil {
			return fmt.Errorf("failed to load B-tree: %w", err)
		}

		// Trigger rebalancing
		err = btree.RebalanceAll()
		if err != nil {
			return fmt.Errorf("failed to rebalance B-tree: %w", err)
		}

		// For MVP: RebalanceAll() is a no-op (single-leaf trees are already optimal)
		// Future: If tree was modified, write it back to disk here

		return nil
	}

	// For datasets created in this session, need to read object header
	reader := ds.fileWriter.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, ds.address, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// Check if has dense attribute storage
	attrInfo, dense, err := denseAttributeInfo(oh, sb)
	if err != nil {
		return err
	}
	if !dense {
		// No dense storage - nothing to rebalance
		return nil
	}

	// Load and rebalance B-tree
	btree := structures.NewWritableBTreeV2(4096)
	err = btree.LoadFromFile(reader, attrInfo.BTreeNameIndexAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load B-tree: %w", err)
	}

	err = btree.RebalanceAll()
	if err != nil {
		return fmt.Errorf("failed to rebalance B-tree: %w", err)
	}

	// For MVP: RebalanceAll() is a no-op
	// Future: Write modified tree back to disk

	return nil
}

// writeAttribute is the internal implementation for writing attributes.
//
// Storage strategy:
// - 0-7 attributes: Compact storage (object header messages)
// - 8+ attributes: Dense storage (Fractal Heap + B-tree v2)
//
// Automatic transition:
// - When adding the 8th attribute, all attributes are migrated to dense storage
// - Compact attribute messages are removed from object header
// - Attribute Info Message is added to object header
//
// For MVP:
// - Transition is one-way (compact → dense only, no dense → compact)
// - No attribute deletion support
//
// Reference: H5Aint.c - H5A__dense_create().
func writeAttribute(fw *FileWriter, objectAddr uint64, name string, value interface{}) error {
	// Get superblock
	sb := fw.file.Superblock()

	// Read object header
	reader := fw.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// Count existing compact attributes (main OHDR only, not from continuations).
	compactCount := 0
	continuationAttrCount := 0
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			if msg.FromContinuation {
				continuationAttrCount++
			} else {
				compactCount++
			}
		}
	}
	_, hasDenseStorage, err := denseAttributeInfo(oh, sb)
	if err != nil {
		return err
	}
	// Total compact attributes includes both main and continuation.
	totalCompactCount := compactCount + continuationAttrCount

	// Determine storage strategy
	if hasDenseStorage {
		// Already using dense storage → add to dense
		return writeDenseAttribute(fw, objectAddr, oh, name, value, sb)
	}

	if totalCompactCount < MaxCompactAttributes {
		// Still compact -> add compact attribute.
		return writeCompactAttribute(fw, objectAddr, oh, name, value, sb)
	}

	// Transition needed -> migrate to dense.
	return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
}

// writeCompactAttribute writes attribute to object header (compact storage).
//
// The header is rewritten by rewriteObjectHeader, which keeps it inside its
// space on disk or moves attributes to a continuation chunk (H5Oalloc.c); a
// header that cannot take the attribute even so moves to dense storage.
func writeCompactAttribute(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Infer datatype and encode attribute (handles []string via Global Heap).
	datatype, dataspace, data, err := inferAndEncodeAttributeValue(fw, value)
	if err != nil {
		return fmt.Errorf("failed to infer/encode attribute: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 2. Check if attribute exists (for upsert semantics).
	existingIndex := -1
	for i, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			existingAttr, parseErr := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if parseErr == nil && existingAttr.Name == name {
				existingIndex = i
				break
			}
		}
	}

	// 3. Encode attribute message.
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute message: %w", err)
	}

	// 4. Upsert: replace the message in place, wherever it lives, keeping its
	// creation index; otherwise append a new one with the next creation index.
	if existingIndex >= 0 {
		oh.Messages[existingIndex].Data = attrMsg
	} else {
		crtIdx, err := nextAttrCreationIndex(oh, sb)
		if err != nil {
			return err
		}
		if err := core.AddMessageToObjectHeader(oh, core.MsgAttribute, attrMsg); err != nil {
			return fmt.Errorf("failed to add message to header: %w", err)
		}
		oh.Messages[len(oh.Messages)-1].CreationIndex = crtIdx
	}

	err = fw.rewriteObjectHeader(objectAddr, oh)
	if errors.Is(err, errHeaderFull) {
		return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
	}
	return err
}

// msgFlagDontShare marks a message that is never shared
// (H5O_MSG_FLAG_DONTSHARE, H5Oprivate.h:75).
const msgFlagDontShare = 0x04

// maxAttrCreationIndex is the largest creation index (H5O_MAX_CRT_ORDER_IDX,
// H5Opkg.h:33).
const maxAttrCreationIndex = 0xFFFF

// nextAttrCreationIndex returns the creation index of a new compact
// attribute of the object header oh. On a version 2 header that tracks
// attribute creation order it is the attribute info message's maximum
// creation index, which is incremented in oh; a header without that message
// gets one with the tracking and indexing of its flags and no dense storage
// (H5Oattribute.c:225-236, 283-300). Other headers keep no creation index: 0.
func nextAttrCreationIndex(oh *core.ObjectHeader, sb *core.Superblock) (uint16, error) {
	if !oh.TracksAttrCreationOrder() {
		return 0, nil
	}

	msg, ainfo, err := trackedAttrInfo(oh, sb)
	if err != nil {
		return 0, err
	}
	if ainfo.MaxCreationIndex >= maxAttrCreationIndex {
		return 0, fmt.Errorf("attribute creation index %d can't be incremented (H5O_MAX_CRT_ORDER_IDX)", ainfo.MaxCreationIndex)
	}
	crtIdx := uint16(ainfo.MaxCreationIndex)
	ainfo.MaxCreationIndex++

	data, err := core.EncodeAttributeInfoMessage(ainfo, sb)
	if err != nil {
		return 0, fmt.Errorf("encode attribute info message: %w", err)
	}
	if msg != nil {
		msg.Data = data
		return crtIdx, nil
	}
	if err := core.AddMessageToObjectHeader(oh, core.MsgAttributeInfo, data); err != nil {
		return 0, fmt.Errorf("add attribute info message: %w", err)
	}
	oh.Messages[len(oh.Messages)-1].Flags = msgFlagDontShare
	return crtIdx, nil
}

// trackedAttrInfo returns the attribute info message of oh, a header that
// tracks attribute creation order, and its decoded content; with no such
// message, nil and the content C gives a new one: tracking and indexing as
// the header's flags, creation index 0, no dense storage (H5Oattribute.c:
// 225-236).
func trackedAttrInfo(oh *core.ObjectHeader, sb *core.Superblock) (*core.HeaderMessage, *core.AttributeInfoMessage, error) {
	msg := attributeInfoMessage(oh)
	ainfo := &core.AttributeInfoMessage{
		Flags:               ainfoTrackCorder,
		FractalHeapAddr:     undefinedAddress,
		BTreeNameIndexAddr:  undefinedAddress,
		BTreeOrderIndexAddr: undefinedAddress,
	}
	if oh.IndexesAttrCreationOrder() {
		ainfo.Flags |= ainfoIndexCorder
	}
	if msg == nil {
		return nil, ainfo, nil
	}
	parsed, err := core.ParseAttributeInfoMessage(msg.Data, sb)
	if err != nil {
		return nil, nil, fmt.Errorf("attribute info message: %w", err)
	}
	// C asserts that the message tracks creation order as its header does
	// (H5Oattribute.c:240-241).
	if parsed.Flags&ainfoTrackCorder == 0 {
		return nil, nil, fmt.Errorf("%w: object header tracks attribute creation order, its attribute info message (flags 0x%02x) does not",
			utils.ErrCorrupt, parsed.Flags)
	}
	return msg, parsed, nil
}

// writeOHDRWithBoundsCheck writes a changed object header back through
// rewriteObjectHeader, which keeps it inside its space on disk.
func writeOHDRWithBoundsCheck(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader, _ *core.Superblock) error {
	return fw.rewriteObjectHeader(objectAddr, oh)
}

// deleteAttribute is the internal implementation for deleting attributes.
//
// Handles both compact and dense storage:
// - Compact: Removes attribute message from object header
// - Dense: Removes from B-tree and fractal heap
//
// Reference: H5Adelete.c - H5A__delete().
func deleteAttribute(fw *FileWriter, objectAddr uint64, name string) error {
	// Get superblock
	sb := fw.file.Superblock()

	// Read object header
	reader := fw.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// Check storage type
	_, dense, err := denseAttributeInfo(oh, sb)
	if err != nil {
		return err
	}
	if dense {
		// Dense storage → delete from B-tree and heap
		return deleteDenseAttributeFromHeader(fw, objectAddr, oh, name, sb)
	}

	// Compact storage → delete from object header
	return deleteCompactAttributeFromHeader(fw, objectAddr, oh, name, sb)
}

// deleteCompactAttributeFromHeader deletes attribute from object header.
//
// Implementation note:
// This uses the existing object header write infrastructure to persist
// the deletion to disk.
//
// Reference: H5Adelete.c - H5A__delete(), H5O.c - H5O_msg_remove().
func deleteCompactAttributeFromHeader(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, sb *core.Superblock) error {
	// Find and remove attribute message
	msgIndex := -1
	for i, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			attr, parseErr := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if parseErr == nil && attr.Name == name {
				msgIndex = i
				break
			}
		}
	}

	if msgIndex == -1 {
		return fmt.Errorf("attribute %q not found", name)
	}

	// Remove message (direct removal - clean approach)
	oh.Messages = append(oh.Messages[:msgIndex], oh.Messages[msgIndex+1:]...)

	// Write back object header to disk
	if err := fw.rewriteObjectHeader(objectAddr, oh); err != nil {
		return fmt.Errorf("failed to write object header after deletion: %w", err)
	}

	return nil
}

// deleteDenseAttributeFromHeader deletes attribute from dense storage by reading Attribute Info from header.
//
// The attribute count is not stored in the attribute info message
// (H5Oainfo.c:206-220), so the header itself does not change, unless the
// storage is migrated from the layout of v0.14.2 (loadDenseAttributeStorage).
func deleteDenseAttributeFromHeader(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, sb *core.Superblock) error {
	st, err := loadDenseAttributeStorage(fw, objectAddr, oh, sb)
	if err != nil {
		return err
	}
	if err := st.remove(name, fw.RebalancingEnabled()); err != nil {
		return err
	}
	return st.flush(fw, sb)
}

// writeDenseAttribute writes attribute to existing dense storage (heap + B-tree).
//
// This function implements Phase 3: Read-Modify-Write for dense attribute storage.
//
// Process:
// 1. Find Attribute Info Message in object header
// 2. Load existing WritableFractalHeap from file
// 3. Load existing WritableBTreeV2 from file
// 4. Add new attribute to loaded structures
// 5. Write updated heap and B-tree back to file (overwrite existing)
//
// This enables adding attributes to datasets that already have dense storage
// (i.e., files that were created, closed, and reopened).
//
// Reference: H5Adense.c - H5A__dense_insert().
func writeDenseAttribute(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	st, err := loadDenseAttributeStorage(fw, objectAddr, oh, sb)
	if err != nil {
		return err
	}

	attr, attrMsg, err := encodeDenseAttribute(fw, name, value, sb)
	if err != nil {
		return err
	}

	if err := st.upsert(name, attr, attrMsg); err != nil {
		return err
	}

	return st.flush(fw, sb)
}

// encodeDenseAttribute prepares the value and encodes the attribute message.
// The encoded message is what ModifyDenseAttribute stores for an existing name.
func encodeDenseAttribute(fw *FileWriter, name string, value interface{},
	sb *core.Superblock) (*core.Attribute, []byte, error) {
	datatype, dataspace, data, err := inferAndEncodeAttributeValue(fw, value)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to infer/encode attribute: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode attribute: %w", err)
	}

	return attr, attrMsg, nil
}

// trackDenseCreationOrder makes daw, the dense storage that oh's compact
// attributes move to, keep creation order when oh tracks it, and returns the
// creation index of a new attribute when addsNew (H5Oattribute.c:258-295);
// an indexed header gets a creation order index (H5Adense.c:227-241). For a
// header that does not track creation order it does nothing.
func trackDenseCreationOrder(daw *writer.DenseAttributeWriter, oh *core.ObjectHeader, sb *core.Superblock, addsNew bool) (uint16, error) {
	if !oh.TracksAttrCreationOrder() {
		return 0, nil
	}
	_, ainfo, err := trackedAttrInfo(oh, sb)
	if err != nil {
		return 0, err
	}
	var crtIdx uint16
	if addsNew {
		// The attribute info message itself is replaced by the caller.
		crtIdx, err = nextAttrCreationIndex(oh, sb)
		if err != nil {
			return 0, err
		}
		ainfo.MaxCreationIndex = uint64(crtIdx) + 1
	}
	daw.TrackCreationOrder(oh.IndexesAttrCreationOrder(), ainfo.MaxCreationIndex)
	return crtIdx, nil
}

// transitionToDenseAttributes migrates all compact attributes to dense storage.
//
// Process:
// 1. Read all compact attributes from object header
// 2. Create DenseAttributeWriter
// 3. Add all existing attributes to dense storage
// 4. Add new attribute to dense storage
// 5. Write dense storage (heap + B-tree)
// 6. Get Attribute Info Message
// 7. Remove all compact attribute messages from object header
// 8. Add Attribute Info Message to object header
// 9. Write updated object header
//
// Reference: H5Aint.c - H5A__dense_create().
//
//nolint:gocognit,gocyclo,cyclop // Complex but necessary business logic for compact-to-dense transition
func transitionToDenseAttributes(fw *FileWriter, objectAddr uint64, _ *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Re-read the OHDR from disk to get ALL messages, including continuation-sourced ones.
	// This is necessary because the caller may have filtered out continuation messages.
	reader := fw.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, objectAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to re-read object header for dense transition: %w", err)
	}

	type compactAttr struct {
		attr   *core.Attribute
		crtIdx uint16 // creation index of its message header
	}
	var compactAttrs []compactAttr
	replaced := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			attr, readErr := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if readErr != nil {
				return fmt.Errorf("failed to parse existing attribute: %w", readErr)
			}
			compactAttrs = append(compactAttrs, compactAttr{attr: attr, crtIdx: msg.CreationIndex})
			replaced = replaced || attr.Name == name
		}
	}

	// 2. Infer datatype and encode new attribute (handles []string via Global Heap).
	datatype, dataspace, data, err := inferAndEncodeAttributeValue(fw, value)
	if err != nil {
		return fmt.Errorf("failed to infer/encode attribute: %w", err)
	}

	newAttr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 3. Create DenseAttributeWriter. On a header that tracks creation order
	// the dense storage keeps it: the attributes move with their creation
	// indexes, a new one takes the next (H5Oattribute.c:258-295), and an
	// indexed header gets a creation order index (H5Adense.c:227-241).
	daw := writer.NewDenseAttributeWriter(objectAddr)
	tracked := oh.TracksAttrCreationOrder()
	newCrtIdx, err := trackDenseCreationOrder(daw, oh, sb, !replaced)
	if err != nil {
		return err
	}
	add := func(attr *core.Attribute, crtIdx uint16) error {
		if tracked {
			return daw.AddAttributeWithCreationIndex(attr, sb, crtIdx)
		}
		return daw.AddAttribute(attr, sb)
	}

	// 4. Add all existing attributes, replacing any that match the new attribute name
	// (upsert semantics: if the new attribute already exists in compact storage, replace
	// it; it keeps its creation index).
	for _, c := range compactAttrs {
		attr := c.attr
		if attr.Name == name {
			attr = newAttr
		}
		if err := add(attr, c.crtIdx); err != nil {
			return fmt.Errorf("failed to add attribute %q: %w", attr.Name, err)
		}
	}

	// 5. Add new attribute (only if it wasn't already replacing an existing one).
	if !replaced {
		if err := add(newAttr, newCrtIdx); err != nil {
			return fmt.Errorf("failed to add new attribute: %w", err)
		}
	}

	// 6. Write the dense storage at the end of the file.
	attrInfo, err := daw.WriteToFile(fw.writer, fw.writer.Allocator(), sb)
	if err != nil {
		return fmt.Errorf("failed to write dense storage: %w", err)
	}
	attrInfoMsg, err := core.EncodeAttributeInfoMessage(attrInfo, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute info: %w", err)
	}

	// 7. The header keeps every message but the attributes, now in dense
	// storage, and gains the attribute info message. A C header already has
	// one with an undefined heap address while its attributes are compact; C
	// updates that message (H5Oattribute.c:296-300), so it is replaced here
	// rather than joined by a second one. Messages of continuation chunks stay
	// with the header; rewriteObjectHeader rebuilds the chain and the padding.
	kept := make([]*core.HeaderMessage, 0, len(oh.Messages)+1)
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgAttribute && !core.IsAttributeInfoMessage(msg.Type) {
			kept = append(kept, msg)
		}
	}
	oh.Messages = kept
	if err := core.AddMessageToObjectHeader(oh, core.MsgAttributeInfo, attrInfoMsg); err != nil {
		return fmt.Errorf("failed to add attribute info message: %w", err)
	}
	if err := fw.rewriteObjectHeader(objectAddr, oh); err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	// 8. CRITICAL: Flush buffered writes to disk!
	// Dense storage was just created at new addresses.
	// Subsequent attributes will try to load from these addresses.
	// If data isn't flushed, they'll read uninitialized memory!
	err = fw.writer.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush after transition: %w", err)
	}

	return nil
}

// inferAndEncodeAttributeValue infers the HDF5 datatype and encodes the value for attribute storage.
// For []string values, this uses the Global Heap via prepareVLenStringAttribute.
// For all other types, it delegates to inferDatatypeFromValue + encodeAttributeValue.
func inferAndEncodeAttributeValue(fw *FileWriter, value interface{}) (*core.DatatypeMessage, *core.DataspaceMessage, []byte, error) {
	// Handle []string specially — requires Global Heap I/O.
	if strs, ok := value.([]string); ok {
		if len(strs) == 0 {
			return nil, nil, nil, fmt.Errorf("cannot write empty []string attribute (no elements)")
		}
		datatype, dataspace, data, err := prepareVLenStringAttribute(fw, strs)
		if err != nil {
			return nil, nil, nil, err
		}
		setAttributeDataspaceAsC(fw, dataspace)
		return datatype, dataspace, data, nil
	}

	// Generic path for scalars and numeric slices.
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return nil, nil, nil, err
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return nil, nil, nil, err
	}

	setAttributeDataspaceAsC(fw, dataspace)
	return datatype, dataspace, data, nil
}

// setAttributeDataspaceAsC gives the dataspace of a new attribute the version
// and maximum dimensions H5Acreate writes:
//   - the version follows the file's format (H5S_set_version at H5Aint.c:361,
//     here through core.DataspaceVersionFor);
//   - H5Screate_simple with no maximum sets the maximum to the dimensions
//     (H5S_set_extent_simple, H5S.c:1291-1298), and H5O__sdspace_encode then
//     stores it (H5Osdspace.c:249-297).
func setAttributeDataspaceAsC(fw *FileWriter, ds *core.DataspaceMessage) {
	ds.Version = core.DataspaceVersionFor(fw.file.sb.Version)
	if ds.MaxDims == nil && len(ds.Dimensions) > 0 {
		ds.MaxDims = append([]uint64(nil), ds.Dimensions...)
	}
}

// ensureGlobalHeapWriter lazily initializes the global heap writer on a FileWriter.
// This is needed because OpenForWrite() does not initialize it (only CreateForWrite does).
func ensureGlobalHeapWriter(fw *FileWriter) {
	if fw.globalHeapWriter == nil {
		fw.globalHeapWriter = newGlobalHeapWriter(fw)
	}
}

// prepareVLenStringAttribute writes []string values to the Global Heap and returns
// the HDF5 datatype, dataspace, and encoded heap ID data suitable for attribute storage.
//
// Each string is null-terminated and written to the Global Heap. The attribute data
// consists of 16-byte heap IDs: seq_len(4) + heap_address(8) + object_index(4).
//
// The VLen string datatype is class=9, version=1, size=16 with a nested base type
// of class=3 (String), version=1, size=1 (character).
//
// C Reference: H5Tvlen.c:876 (seq_len encoding), H5Odtype.c:1352-1365 (VLen datatype).
func prepareVLenStringAttribute(fw *FileWriter, strings []string) (*core.DatatypeMessage, *core.DataspaceMessage, []byte, error) {
	ensureGlobalHeapWriter(fw)

	// 1. Write each string to global heap and collect heap IDs.
	heapIDs := make([]HeapID, len(strings))
	for i, str := range strings {
		// Write null-terminated string to global heap (same as VLen dataset writing).
		heapID, err := fw.globalHeapWriter.WriteToGlobalHeap([]byte(str))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("write string %d to global heap: %w", i, err)
		}
		// SeqLen = string length in bytes (characters, not including null terminator).
		// C ref: H5Tvlen.c:876 — UINT32ENCODE(vl, seq_len) where seq_len = nchars.
		heapID.SeqLen = uint32(len(str)) //nolint:gosec // G115: string length fits in uint32
		heapIDs[i] = heapID
	}

	// 2. Flush the global heap to ensure addresses are finalized before attribute encoding.
	if err := fw.globalHeapWriter.Flush(); err != nil {
		return nil, nil, nil, fmt.Errorf("flush global heap: %w", err)
	}

	// 3. Encode heap IDs as attribute data (16 bytes per element).
	data := make([]byte, len(strings)*16)
	for i, hid := range heapIDs {
		copy(data[i*16:], hid.Encode())
	}

	// 4. Build the VLen string datatype: null-terminated ASCII, 16-byte heap
	// IDs, unsigned char base type as the C library builds it (H5T.c:4940-4970).
	dt, err := core.NewVLenStringDatatype(core.StringNullTerm, core.CharsetASCII)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build vlen string datatype: %w", err)
	}

	// 5. Build the dataspace.
	ds := &core.DataspaceMessage{
		Dimensions: []uint64{uint64(len(strings))},
		MaxDims:    nil,
	}

	return dt, ds, data, nil
}

// inferDatatypeFromValue infers HDF5 datatype and dimensions from a Go value.
// Returns datatype message, dataspace message, and error.
func inferDatatypeFromValue(value interface{}) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	v := reflect.ValueOf(value)

	// Handle scalar types
	if !v.IsValid() {
		return nil, nil, fmt.Errorf("value is nil or invalid")
	}

	switch v.Kind() {
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return inferSignedInt(v)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return inferUnsignedInt(v)
	case reflect.Float32, reflect.Float64:
		return inferFloat(v)
	case reflect.String:
		return inferString(v)
	case reflect.Slice:
		return inferSlice(v)
	default:
		return nil, nil, fmt.Errorf("unsupported value type: %s", v.Kind())
	}
}

// inferSignedInt infers datatype for signed integers.
func inferSignedInt(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Int8:
		size = 1
	case reflect.Int16:
		size = 2
	case reflect.Int32:
		size = 4
	case reflect.Int64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not a signed integer type")
	}

	dt, err := core.NewIntegerDatatype(size, true) // H5T_STD_I<n>LE
	if err != nil {
		return nil, nil, err
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar (HDF5 uses [1] for scalars)
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferUnsignedInt infers datatype for unsigned integers.
func inferUnsignedInt(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Uint8:
		size = 1
	case reflect.Uint16:
		size = 2
	case reflect.Uint32:
		size = 4
	case reflect.Uint64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not an unsigned integer type")
	}

	dt, err := core.NewIntegerDatatype(size, false) // H5T_STD_U<n>LE
	if err != nil {
		return nil, nil, err
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferFloat infers datatype for floating point numbers.
func inferFloat(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	var size uint32
	switch v.Kind() {
	case reflect.Float32:
		size = 4
	case reflect.Float64:
		size = 8
	default:
		return nil, nil, fmt.Errorf("not a float type")
	}

	// H5T_IEEE_F<n>LE. Before v0.14.3 this was a literal with class bit
	// field 0: sign bit position 0, which the C library rejects
	// (H5Odtype.c:264-267); the constructor sets sign position and
	// normalization.
	dt, err := core.NewFloatDatatype(size)
	if err != nil {
		return nil, nil, err
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferString infers datatype for strings.
func inferString(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	str := v.String()
	size := uint32(len(str) + 1) //nolint:gosec // Safe: string length fits in uint32

	dt, err := core.NewStringDatatype(size, core.StringNullTerm, core.CharsetASCII)
	if err != nil {
		return nil, nil, err
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferSlice infers datatype for slices (1D arrays).
//
// Note: []string is NOT handled here because it requires Global Heap I/O.
// Use prepareVLenStringAttribute() instead for []string values.
func inferSlice(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	if v.Len() == 0 {
		return nil, nil, fmt.Errorf("cannot infer datatype from empty slice")
	}

	elemKind := v.Type().Elem().Kind()
	length := uint64(v.Len()) //nolint:gosec // Safe: slice length conversion

	var dt *core.DatatypeMessage
	var err error

	// Element types as the C library writes them: H5T_STD_I/U<n>LE and
	// H5T_IEEE_F<n>LE (float literals with class bit field 0 before v0.14.3
	// put the sign bit at position 0, H5Odtype.c:264-267).
	switch elemKind {
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		dt, err = core.NewIntegerDatatype(uint32(v.Type().Elem().Size()), true) //nolint:gosec // G115: 1-8 bytes
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		dt, err = core.NewIntegerDatatype(uint32(v.Type().Elem().Size()), false) //nolint:gosec // G115: 1-8 bytes
	case reflect.Float32, reflect.Float64:
		dt, err = core.NewFloatDatatype(uint32(v.Type().Elem().Size())) //nolint:gosec // G115: 4 or 8 bytes
	default:
		return nil, nil, fmt.Errorf("unsupported slice element type: %s", elemKind)
	}
	if err != nil {
		return nil, nil, err
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{length},
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// encodeAttributeValue encodes a Go value to bytes for attribute storage.
func encodeAttributeValue(value interface{}) ([]byte, error) {
	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Int8:
		return []byte{byte(v.Int())}, nil //nolint:gosec // Safe: source is int8
	case reflect.Int16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Int32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(v.Int())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint8:
		return []byte{byte(v.Uint())}, nil //nolint:gosec // Safe: source is uint8
	case reflect.Uint16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v.Uint())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v.Uint())) //nolint:gosec // Safe: validated data type
		return buf, nil
	case reflect.Uint64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, v.Uint())
		return buf, nil
	case reflect.Float32:
		bits := math.Float32bits(float32(v.Float()))
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, bits)
		return buf, nil
	case reflect.Float64:
		bits := math.Float64bits(v.Float())
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, bits)
		return buf, nil
	case reflect.String:
		str := v.String()
		buf := make([]byte, len(str)+1)
		copy(buf, str)
		buf[len(str)] = 0 // Null terminator
		return buf, nil
	case reflect.Slice:
		return encodeSliceValue(v)
	default:
		return nil, fmt.Errorf("unsupported value type for encoding: %s", v.Kind())
	}
}

// encodeSliceValue encodes a slice to bytes.
//
//nolint:gocognit,gocyclo,cyclop // Type dispatch for all supported HDF5 integer/float types.
func encodeSliceValue(v reflect.Value) ([]byte, error) {
	elemKind := v.Type().Elem().Kind()
	length := v.Len()

	switch elemKind {
	case reflect.Int8:
		buf := make([]byte, length)
		for i := 0; i < length; i++ {
			buf[i] = byte(v.Index(i).Int()) //nolint:gosec // Safe: source is int8 slice
		}
		return buf, nil
	case reflect.Uint8:
		buf := make([]byte, length)
		for i := 0; i < length; i++ {
			buf[i] = byte(v.Index(i).Uint()) //nolint:gosec // Safe: source is uint8 slice
		}
		return buf, nil
	case reflect.Int16:
		buf := make([]byte, length*2)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(v.Index(i).Int())) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Uint16:
		buf := make([]byte, length*2)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(v.Index(i).Uint())) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Int32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(v.Index(i).Int())) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Uint32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(v.Index(i).Uint())) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(v.Index(i).Int())) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Uint64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			binary.LittleEndian.PutUint64(buf[i*8:], v.Index(i).Uint())
		}
		return buf, nil
	case reflect.Float32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			bits := math.Float32bits(float32(v.Index(i).Float()))
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}
		return buf, nil
	case reflect.Float64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			bits := math.Float64bits(v.Index(i).Float())
			binary.LittleEndian.PutUint64(buf[i*8:], bits)
		}
		return buf, nil
	default:
		return nil, fmt.Errorf("unsupported slice element type: %s", elemKind)
	}
}

// Suppress unused warnings for now (these will be used when attribute writing is fully implemented).
var (
	_ = (*core.DatatypeMessage)(nil)
	_ = (*core.DataspaceMessage)(nil)
	_ = inferDatatypeFromValue
	_ = encodeAttributeValue
	_ = unsafe.Sizeof(0)
)
