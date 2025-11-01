package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unsafe"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
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
//
// Limitations:
//   - No variable-length strings
//   - No compound types
//   - Attributes cannot be modified after creation (write-once)
//   - No attribute deletion
func (ds *DatasetWriter) WriteAttribute(name string, value interface{}) error {
	// For datasets opened with OpenForWrite, use cached object header and dense attr info
	if ds.objectHeader != nil {
		return writeAttributeWithCachedHeader(ds.fileWriter, ds.address, ds.objectHeader, ds.denseAttrInfo, name, value)
	}

	// For datasets created in this session, read object header fresh
	return writeAttribute(ds.fileWriter, ds.address, name, value)
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
	// For datasets opened with OpenForWrite, use cached object header and dense attr info
	if ds.objectHeader != nil {
		return deleteAttributeWithCachedHeader(ds.fileWriter, ds.address, ds.objectHeader, ds.denseAttrInfo, name)
	}

	// For datasets created in this session, read object header fresh
	return deleteAttribute(ds.fileWriter, ds.address, name)
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

	// For datasets opened with OpenForWrite, we have cached dense attr info
	if ds.denseAttrInfo != nil {
		// Load B-tree from file
		sb := ds.fileWriter.file.Superblock()
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
	sb := ds.fileWriter.file.Superblock()
	reader := ds.fileWriter.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, ds.address, sb)
	if err != nil {
		return fmt.Errorf("failed to read object header: %w", err)
	}

	// Check if has dense attribute storage
	var attrInfo *core.AttributeInfoMessage
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttributeInfo {
			attrInfo, err = core.ParseAttributeInfoMessage(msg.Data, sb)
			if err != nil {
				return fmt.Errorf("failed to parse attribute info: %w", err)
			}
			break
		}
	}

	if attrInfo == nil {
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

	// Count existing attributes
	compactCount := 0
	hasDenseStorage := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			compactCount++
		}
		if msg.Type == core.MsgAttributeInfo {
			hasDenseStorage = true
		}
	}

	// Determine storage strategy
	if hasDenseStorage {
		// Already using dense storage → add to dense
		return writeDenseAttribute(fw, objectAddr, oh, name, value, sb)
	}

	if compactCount < MaxCompactAttributes {
		// Still compact → add compact attribute
		return writeCompactAttribute(fw, objectAddr, oh, name, value, sb)
	}

	// Transition needed → migrate to dense
	return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
}

// writeCompactAttribute writes attribute to object header (compact storage).
// This is the Phase 1 code, extracted into separate function.
func writeCompactAttribute(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Infer datatype and encode attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 2. Check if attribute exists (for upsert semantics)
	// If exists → modify (replace data)
	// If not exists → create (add new message)
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

	// 3. Encode attribute message
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute message: %w", err)
	}

	// 4. Upsert logic: modify if exists, add if not exists
	err = upsertAttributeMessage(fw, objectAddr, oh, existingIndex, attrMsg, name, value, sb)
	if err != nil {
		return err
	}

	// 5. Write updated header back to disk
	err = core.WriteObjectHeader(fw.writer, objectAddr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	return nil
}

// upsertAttributeMessage handles the upsert logic for attribute messages in compact storage.
// If attribute exists (existingIndex >= 0), it replaces the message data.
// If attribute doesn't exist (existingIndex < 0), it adds a new message.
// If object header is full, it triggers transition to dense storage.
func upsertAttributeMessage(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	existingIndex int, attrMsg []byte, name string, value interface{}, sb *core.Superblock) error {
	if existingIndex >= 0 {
		// Attribute exists → Replace (upsert semantics)
		oh.Messages[existingIndex].Data = attrMsg
		return nil
	}

	// Attribute doesn't exist → Add new message
	err := core.AddMessageToObjectHeader(oh, core.MsgAttribute, attrMsg)
	if err != nil {
		// If object header is full, transition to dense storage
		if strings.Contains(err.Error(), "object header full") {
			return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
		}
		return fmt.Errorf("failed to add message to header: %w", err)
	}

	return nil
}

// writeAttributeWithCachedHeader writes attribute using cached object header (for OpenDataset scenarios).
//
// This function is used when a dataset is opened with OpenForWrite() and already has
// a parsed object header and attribute info available.
//
// Parameters:
//   - fw: File writer
//   - objectAddr: Object header address
//   - oh: Cached object header (from OpenDataset)
//   - denseAttrInfo: Cached attribute info (may be nil)
//   - name: Attribute name
//   - value: Attribute value
//
// Reference: Same as writeAttribute, but skips object header re-parsing.
func writeAttributeWithCachedHeader(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	denseAttrInfo *core.AttributeInfoMessage, name string, value interface{}) error {
	sb := fw.file.Superblock()

	// If dense storage info is available, use it directly
	if denseAttrInfo != nil {
		return writeDenseAttributeWithInfo(fw, objectAddr, oh, denseAttrInfo, name, value, sb)
	}

	// No dense storage yet - count compact attributes to determine strategy
	compactCount := 0
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			compactCount++
		}
	}

	if compactCount < MaxCompactAttributes {
		// Still compact → add compact attribute
		return writeCompactAttribute(fw, objectAddr, oh, name, value, sb)
	}

	// Need to transition to dense storage (8th attribute)
	return transitionToDenseAttributes(fw, objectAddr, oh, name, value, sb)
}

// writeDenseAttributeWithInfo writes or modifies attribute in existing dense storage.
//
// This implements upsert semantics for dense attributes:
// - If attribute exists → modify it (Phase 2: Dense modification)
// - If attribute doesn't exist → create it (Phase 3: Dense RMW)
//
// This is similar to writeDenseAttribute but uses the cached AttributeInfoMessage
// instead of searching for it in the object header.
func writeDenseAttributeWithInfo(fw *FileWriter, _ uint64, _ *core.ObjectHeader,
	attrInfo *core.AttributeInfoMessage, name string, value interface{}, sb *core.Superblock) error {
	// Load existing fractal heap from file
	heap := structures.NewWritableFractalHeap(64 * 1024)
	err := heap.LoadFromFile(fw.writer.Reader(), attrInfo.FractalHeapAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load fractal heap: %w", err)
	}

	// Load existing B-tree v2 from file
	btree := structures.NewWritableBTreeV2(4096)
	err = btree.LoadFromFile(fw.writer.Reader(), attrInfo.BTreeNameIndexAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load B-tree: %w", err)
	}

	// Prepare new attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// Encode attribute message
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute: %w", err)
	}

	// Check if attribute already exists (upsert semantics)
	_, exists := btree.SearchRecord(name)

	if exists { //nolint:nestif // Clear upsert logic
		// Modify existing attribute (Phase 2)
		// Set the encoded data in attr for ModifyDenseAttribute
		attr.Data = attrMsg
		err = core.ModifyDenseAttribute(heap, btree, name, attr)
		if err != nil {
			return fmt.Errorf("failed to modify existing dense attribute: %w", err)
		}
	} else {
		// Create new attribute (Phase 3 - original RMW code)

		// Insert into fractal heap
		heapIDBytes, insertErr := heap.InsertObject(attrMsg)
		if insertErr != nil {
			return fmt.Errorf("failed to insert into heap: %w", insertErr)
		}

		// Convert heap ID to uint64 for B-tree
		if len(heapIDBytes) != 8 {
			return fmt.Errorf("unexpected heap ID length: %d bytes", len(heapIDBytes))
		}
		heapID := binary.LittleEndian.Uint64(heapIDBytes)

		// Insert into B-tree
		err = btree.InsertRecord(name, heapID)
		if err != nil {
			return fmt.Errorf("failed to insert into B-tree: %w", err)
		}
	}

	// Write updated structures back to file (IN-PLACE using WriteAt)
	err = heap.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated heap: %w", err)
	}

	err = btree.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated B-tree: %w", err)
	}

	return nil
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
	hasDenseStorage := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttributeInfo {
			hasDenseStorage = true
			break
		}
	}

	if hasDenseStorage {
		// Dense storage → delete from B-tree and heap
		return deleteDenseAttributeFromHeader(fw, objectAddr, oh, name, sb)
	}

	// Compact storage → delete from object header
	return deleteCompactAttributeFromHeader(fw, objectAddr, oh, name, sb)
}

// deleteAttributeWithCachedHeader deletes attribute using cached object header.
//
// This is used when DatasetWriter has cached object header and dense attr info.
func deleteAttributeWithCachedHeader(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	denseAttrInfo *core.AttributeInfoMessage, name string) error {
	sb := fw.file.Superblock()

	// If dense storage info is available, use it directly
	if denseAttrInfo != nil {
		// Find Attribute Info message index in object header (we have the parsed version in denseAttrInfo)
		attrInfoIndex := -1
		for i, msg := range oh.Messages {
			if msg.Type == core.MsgAttributeInfo {
				attrInfoIndex = i
				break
			}
		}

		if attrInfoIndex == -1 {
			return fmt.Errorf("attribute info message not found in cached header")
		}

		// Delete from heap and B-tree
		// Note: Attribute count is implicit in B-tree record count, no explicit field to update
		return deleteDenseAttributeImpl(fw, denseAttrInfo, name, sb)
	}

	// No dense storage - delete from compact
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
	err := core.WriteObjectHeader(fw.writer, objectAddr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header after deletion: %w", err)
	}

	return nil
}

// deleteDenseAttributeFromHeader deletes attribute from dense storage by reading Attribute Info from header.
func deleteDenseAttributeFromHeader(fw *FileWriter, _ uint64, oh *core.ObjectHeader, name string, sb *core.Superblock) error {
	// Find Attribute Info Message
	var attrInfo *core.AttributeInfoMessage
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttributeInfo {
			parsed, err := core.ParseAttributeInfoMessage(msg.Data, sb)
			if err != nil {
				return fmt.Errorf("failed to parse attribute info message: %w", err)
			}
			attrInfo = parsed
			break
		}
	}

	if attrInfo == nil {
		return fmt.Errorf("attribute info message not found")
	}

	// Delete attribute from dense storage
	// Note: Attribute count is implicit in B-tree record count, no explicit field to update
	return deleteDenseAttributeImpl(fw, attrInfo, name, sb)
}

// deleteDenseAttributeImpl is the low-level implementation for deleting dense attributes.
// It deletes from heap and B-tree but does NOT update the Attribute Info count.
// Callers are responsible for updating the count and writing back the object header.
func deleteDenseAttributeImpl(fw *FileWriter, attrInfo *core.AttributeInfoMessage,
	name string, sb *core.Superblock) error {
	// Load existing fractal heap from file
	heap := structures.NewWritableFractalHeap(64 * 1024)
	err := heap.LoadFromFile(fw.writer.Reader(), attrInfo.FractalHeapAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load fractal heap: %w", err)
	}

	// Load existing B-tree v2 from file
	btree := structures.NewWritableBTreeV2(4096)
	err = btree.LoadFromFile(fw.writer.Reader(), attrInfo.BTreeNameIndexAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load B-tree: %w", err)
	}

	// Delete attribute using core deletion function
	// Use FileWriter's rebalancing configuration
	rebalance := fw.RebalancingEnabled()
	err = core.DeleteDenseAttribute(heap, btree, name, rebalance)
	if err != nil {
		return fmt.Errorf("failed to delete dense attribute: %w", err)
	}

	// Write updated heap back to file
	err = heap.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated heap: %w", err)
	}

	// Write updated B-tree back to file
	err = btree.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated B-tree: %w", err)
	}

	// Note: Attribute count update is handled by caller
	return nil
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
//
//nolint:gocognit,gocyclo,cyclop // Complex RMW logic with multiple verification steps
func writeDenseAttribute(fw *FileWriter, _ uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// Step 1: Find Attribute Info Message
	var attrInfo *core.AttributeInfoMessage
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttributeInfo {
			// Parse the message data
			parsed, err := core.ParseAttributeInfoMessage(msg.Data, sb)
			if err != nil {
				return fmt.Errorf("failed to parse attribute info message: %w", err)
			}
			attrInfo = parsed
			break
		}
	}

	if attrInfo == nil {
		return fmt.Errorf("attribute info message not found (dense storage not initialized)")
	}

	// Step 2: Load existing fractal heap from file
	heap := structures.NewWritableFractalHeap(64 * 1024) // Match size from dense attribute writer
	err := heap.LoadFromFile(fw.writer.Reader(), attrInfo.FractalHeapAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load fractal heap: %w", err)
	}

	// Step 3: Load existing B-tree v2 from file
	btree := structures.NewWritableBTreeV2(4096) // Match size from dense attribute writer
	err = btree.LoadFromFile(fw.writer.Reader(), attrInfo.BTreeNameIndexAddr, sb)
	if err != nil {
		return fmt.Errorf("failed to load B-tree: %w", err)
	}

	// Step 4: Prepare new attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	attr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// Encode attribute message
	attrMsg, err := core.EncodeAttributeFromStruct(attr, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute: %w", err)
	}

	// Check if attribute already exists (upsert semantics)
	_, exists := btree.SearchRecord(name)

	if exists { //nolint:nestif // Clear upsert logic
		// Modify existing attribute (Phase 2)
		attr.Data = attrMsg
		err = core.ModifyDenseAttribute(heap, btree, name, attr)
		if err != nil {
			return fmt.Errorf("failed to modify existing dense attribute: %w", err)
		}
	} else {
		// Create new attribute (Phase 3 - original code)

		// Insert into fractal heap
		heapIDBytes, insertErr := heap.InsertObject(attrMsg)
		if insertErr != nil {
			return fmt.Errorf("failed to insert into heap: %w", insertErr)
		}

		// Convert heap ID to uint64 for B-tree
		if len(heapIDBytes) != 8 {
			return fmt.Errorf("unexpected heap ID length: %d bytes", len(heapIDBytes))
		}
		heapID := binary.LittleEndian.Uint64(heapIDBytes)

		// Insert into B-tree
		err = btree.InsertRecord(name, heapID)
		if err != nil {
			return fmt.Errorf("failed to insert into B-tree: %w", err)
		}
	}

	// Step 5: Write updated structures back to file (IN-PLACE using WriteAt)
	// NOTE: WriteAt() writes to the addresses where structures were loaded from
	// This is true Read-Modify-Write - no new allocations!

	// Write heap in-place at loaded address
	err = heap.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated heap: %w", err)
	}

	// Write B-tree in-place at loaded address
	err = btree.WriteAt(fw.writer, sb)
	if err != nil {
		return fmt.Errorf("failed to write updated B-tree: %w", err)
	}

	return nil
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
//nolint:gocognit,gocyclo,cyclop // Complex but necessary business logic for compact→dense transition
func transitionToDenseAttributes(fw *FileWriter, objectAddr uint64, oh *core.ObjectHeader,
	name string, value interface{}, sb *core.Superblock) error {
	// 1. Read all existing compact attributes
	var compactAttrs []*core.Attribute
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgAttribute {
			attr, err := core.ParseAttributeMessage(msg.Data, sb.Endianness)
			if err != nil {
				return fmt.Errorf("failed to parse existing attribute: %w", err)
			}
			compactAttrs = append(compactAttrs, attr)
		}
	}

	// 2. Infer datatype and encode new attribute
	datatype, dataspace, err := inferDatatypeFromValue(value)
	if err != nil {
		return fmt.Errorf("failed to infer datatype: %w", err)
	}

	data, err := encodeAttributeValue(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	newAttr := &core.Attribute{
		Name:      name,
		Datatype:  datatype,
		Dataspace: dataspace,
		Data:      data,
	}

	// 3. Create DenseAttributeWriter
	daw := writer.NewDenseAttributeWriter(objectAddr)

	// 4. Add all existing attributes
	for _, attr := range compactAttrs {
		err = daw.AddAttribute(attr, sb)
		if err != nil {
			return fmt.Errorf("failed to add existing attribute: %w", err)
		}
	}

	// 5. Add new attribute
	err = daw.AddAttribute(newAttr, sb)
	if err != nil {
		return fmt.Errorf("failed to add new attribute: %w", err)
	}

	// 6. Remove compact attributes from object header
	var newMessages []*core.HeaderMessage
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgAttribute {
			newMessages = append(newMessages, msg)
		}
	}
	oh.Messages = newMessages

	// 7. Calculate object header size (without AttrInfo message yet)
	// to determine where dense storage should be allocated
	ohWriter := &core.ObjectHeaderWriter{
		Version:  oh.Version,
		Flags:    oh.Flags,
		Messages: make([]core.MessageWriter, len(oh.Messages)),
	}
	for i, msg := range oh.Messages {
		ohWriter.Messages[i] = core.MessageWriter{
			Type: msg.Type,
			Data: msg.Data,
		}
	}

	// Add temporary AttrInfo message to calculate size
	// Use REAL size (2 + offsetSize*2) even though addresses are unknown
	tempAttrInfo := &core.AttributeInfoMessage{
		Version:            0,
		Flags:              0,
		FractalHeapAddr:    0,
		BTreeNameIndexAddr: 0,
	}
	tempAttrInfoMsg, err := core.EncodeAttributeInfoMessage(tempAttrInfo, sb)
	if err != nil {
		return fmt.Errorf("failed to encode temp attribute info: %w", err)
	}

	ohWriter.Messages = append(ohWriter.Messages, core.MessageWriter{
		Type: core.MsgAttributeInfo,
		Data: tempAttrInfoMsg,
	})

	objectHeaderSize := ohWriter.Size()
	objectHeaderEnd := objectAddr + objectHeaderSize

	// 8. Update allocator to ensure dense storage allocated AFTER object header
	allocator := fw.writer.Allocator()
	if allocator.EndOfFile() < objectHeaderEnd {
		bytesToAdvance := objectHeaderEnd - allocator.EndOfFile()
		_, err = allocator.Allocate(bytesToAdvance)
		if err != nil {
			return fmt.Errorf("failed to advance allocator past object header: %w", err)
		}
	}

	// 9. Write dense storage - allocator will place it AFTER object header
	attrInfo, err := daw.WriteToFile(fw.writer, allocator, sb)
	if err != nil {
		return fmt.Errorf("failed to write dense storage: %w", err)
	}

	// 10. NOW add AttributeInfo message with REAL addresses to object header
	attrInfoMsg, err := core.EncodeAttributeInfoMessage(attrInfo, sb)
	if err != nil {
		return fmt.Errorf("failed to encode attribute info: %w", err)
	}

	err = core.AddMessageToObjectHeader(oh, core.MsgAttributeInfo, attrInfoMsg)
	if err != nil {
		return fmt.Errorf("failed to add attribute info message: %w", err)
	}

	// 11. Write object header with REAL addresses (ONE TIME!)
	err = core.WriteObjectHeader(fw.writer, objectAddr, oh, sb)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	// 13. CRITICAL: Flush buffered writes to disk!
	// Dense storage was just created at new addresses.
	// Subsequent attributes will try to load from these addresses.
	// If data isn't flushed, they'll read uninitialized memory!
	err = fw.writer.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush after transition: %w", err)
	}

	return nil
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

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFixed,
		Size:          size,
		ClassBitField: 0x08, // Bit 3 set for signed integers
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

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFixed,
		Size:          size,
		ClassBitField: 0, // Bit 3 clear for unsigned integers
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

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeFloat,
		Size:          size,
		ClassBitField: 0, // Little-endian
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

	dt := &core.DatatypeMessage{
		Class:         core.DatatypeString,
		Size:          size,
		ClassBitField: 0, // ASCII, null-terminated
	}

	ds := &core.DataspaceMessage{
		Dimensions: []uint64{1}, // Scalar
		MaxDims:    nil,
	}

	return dt, ds, nil
}

// inferSlice infers datatype for slices (1D arrays).
func inferSlice(v reflect.Value) (*core.DatatypeMessage, *core.DataspaceMessage, error) {
	if v.Len() == 0 {
		return nil, nil, fmt.Errorf("cannot infer datatype from empty slice")
	}

	elemKind := v.Type().Elem().Kind()
	length := uint64(v.Len()) //nolint:gosec // Safe: slice length conversion

	var dt *core.DatatypeMessage

	switch elemKind {
	case reflect.Int32:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFixed,
			Size:          4,
			ClassBitField: 0x08, // Signed
		}
	case reflect.Int64:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFixed,
			Size:          8,
			ClassBitField: 0x08, // Signed
		}
	case reflect.Float32:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFloat,
			Size:          4,
			ClassBitField: 0,
		}
	case reflect.Float64:
		dt = &core.DatatypeMessage{
			Class:         core.DatatypeFloat,
			Size:          8,
			ClassBitField: 0,
		}
	default:
		return nil, nil, fmt.Errorf("unsupported slice element type: %s", elemKind)
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
		return []byte{byte(v.Int())}, nil
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
		return []byte{byte(v.Uint())}, nil
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
func encodeSliceValue(v reflect.Value) ([]byte, error) {
	elemKind := v.Type().Elem().Kind()
	length := v.Len()

	switch elemKind {
	case reflect.Int32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			val := v.Index(i).Int()
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(val)) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Int64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			val := v.Index(i).Int()
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(val)) //nolint:gosec // Safe: validated data type
		}
		return buf, nil
	case reflect.Float32:
		buf := make([]byte, length*4)
		for i := 0; i < length; i++ {
			val := v.Index(i).Float()
			bits := math.Float32bits(float32(val))
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}
		return buf, nil
	case reflect.Float64:
		buf := make([]byte, length*8)
		for i := 0; i < length; i++ {
			val := v.Index(i).Float()
			bits := math.Float64bits(val)
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
