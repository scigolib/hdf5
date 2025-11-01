package hdf5

import (
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
)

// HDF5 signature constants.
const (
	SignatureSNOD = "SNOD" // Symbol table node signature.
)

// Object represents any HDF5 object (Group or Dataset) that can be accessed in the file structure.
type Object interface {
	Name() string
}

// Dataset represents an HDF5 dataset containing multidimensional array data.
type Dataset struct {
	file    *File
	name    string
	address uint64 // Address of object header.
}

// Name returns the dataset's name.
func (d *Dataset) Name() string {
	return d.name
}

// Address returns the object header address (for internal/debugging use).
func (d *Dataset) Address() uint64 {
	return d.address
}

// Attributes returns all attributes attached to this dataset.
func (d *Dataset) Attributes() ([]*core.Attribute, error) {
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, err
	}
	return header.Attributes, nil
}

// ListAttributes returns the names of all attributes attached to this dataset.
func (d *Dataset) ListAttributes() ([]string, error) {
	attrs, err := d.Attributes()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(attrs))
	for i, attr := range attrs {
		names[i] = attr.Name
	}
	return names, nil
}

// ReadAttribute reads a single attribute by name.
func (d *Dataset) ReadAttribute(name string) (interface{}, error) {
	attrs, err := d.Attributes()
	if err != nil {
		return nil, err
	}

	for _, attr := range attrs {
		if attr.Name == name {
			// Parse and return typed value
			return attr.ReadValue()
		}
	}

	return nil, fmt.Errorf("attribute %q not found", name)
}

// Read reads the dataset values and returns them as float64 array.
// Currently supports float64, float32, int32, int64 datatypes.
// All values are converted to float64 for convenience.
func (d *Dataset) Read() ([]float64, error) {
	// Read object header for this dataset.
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, err
	}

	// Use the dataset reader to get values.
	return core.ReadDatasetFloat64(d.file.osFile, header, d.file.sb)
}

// ReadStrings reads string dataset values and returns them as string array.
// Supports fixed-length strings (null-terminated, null-padded, space-padded).
// Variable-length strings are not yet supported.
func (d *Dataset) ReadStrings() ([]string, error) {
	// Read object header for this dataset.
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, err
	}

	// Use the string dataset reader.
	return core.ReadDatasetStrings(d.file.osFile, header, d.file.sb)
}

// ReadCompound reads compound dataset values and returns them as array of maps.
// Each map represents one compound structure instance with field names as keys.
// Supports nested compound types, numeric types, and fixed-length strings.
func (d *Dataset) ReadCompound() ([]core.CompoundValue, error) {
	// Read object header for this dataset.
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return nil, err
	}

	// Use the compound dataset reader.
	return core.ReadDatasetCompound(d.file.osFile, header, d.file.sb)
}

// Info returns metadata about the dataset without reading actual values.
func (d *Dataset) Info() (string, error) {
	header, err := core.ReadObjectHeader(d.file.osFile, d.address, d.file.sb)
	if err != nil {
		return "", err
	}

	info, err := core.ReadDatasetInfo(header, d.file.sb)
	if err != nil {
		return "", err
	}

	return info.String(), nil
}

// Group represents an HDF5 group that can contain other groups and datasets.
type Group struct {
	file        *File
	name        string
	address     uint64 // Address of object header (0 if traditional/SNOD format)
	children    []Object
	symbolTable *structures.SymbolTable
	localHeap   *structures.LocalHeap
}

// Name returns the group's name.
func (g *Group) Name() string {
	return g.name
}

// Children returns all child objects (groups and datasets) within this group.
func (g *Group) Children() []Object {
	return g.children
}

// Attributes returns all attributes attached to this group.
// Note: For groups loaded via traditional format (SNOD), the address may be 0,
// and attributes cannot be retrieved (traditional format doesn't have attributes).
func (g *Group) Attributes() ([]*core.Attribute, error) {
	// Traditional format groups (SNOD) don't support attributes.
	if g.address == 0 {
		return []*core.Attribute{}, nil
	}

	// Read object header to get attributes.
	header, err := core.ReadObjectHeader(g.file.osFile, g.address, g.file.sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read object header: %w", err)
	}

	// Ensure we return an empty slice instead of nil if no attributes exist.
	if header.Attributes == nil {
		return []*core.Attribute{}, nil
	}

	return header.Attributes, nil
}

func loadGroup(file *File, address uint64) (*Group, error) {
	if address == 0 {
		return nil, errors.New("invalid group address: 0")
	}

	// Check signature to determine group format.
	sig := readSignature(file.osFile, address)

	// SNOD always means traditional format.
	if sig == SignatureSNOD {
		return loadTraditionalGroup(file, address)
	}

	// For OHDR or v1 headers (no signature), try loading as modern group.
	// ReadObjectHeader will handle both v1 and v2 formats.
	return loadModernGroup(file, address)
}

func loadModernGroup(file *File, address uint64) (*Group, error) {
	r := file.osFile
	sb := file.sb

	header, err := core.ReadObjectHeader(r, address, sb)
	if err != nil {
		return nil, utils.WrapError("object header read failed", err)
	}

	group := &Group{
		file:    file,
		name:    header.Name,
		address: address, // Store address for later Attributes() access
	}

	// Load children only for groups.
	if header.Type == core.ObjectTypeGroup {
		// First, try to parse Link messages (modern format).
		hasLinkMessages := false
		for _, msg := range header.Messages {
			if msg.Type == core.MsgLinkMessage {
				hasLinkMessages = true

				// Parse the link message.
				linkMsg, err := structures.ParseLinkMessage(msg.Data, sb)
				if err != nil {
					return nil, utils.WrapError("link message parse failed", err)
				}

				// Process based on link type.
				if linkMsg.IsHardLink() {
					// Load the object that this link points to.
					child, err := loadObject(file, linkMsg.ObjectAddress, linkMsg.Name)
					if err != nil {
						// Log warning but continue with other links.
						// Some links might point to objects we don't support yet.
						continue
					}
					group.children = append(group.children, child)
				} else if linkMsg.IsSoftLink() {
					// Soft link support deferred to v0.11.0-beta.
					// Soft links are symbolic links within HDF5 file pointing to paths.
					// Current implementation focuses on hard links (direct object references).
					// Target version: v0.11.0-beta (write support phase)
					continue
				}
			}
		}

		// Fallback to symbol table if no link messages found (older format).
		if !hasLinkMessages {
			for _, msg := range header.Messages {
				if msg.Type == core.MsgSymbolTable {
					// Symbol table message data format:
					// Bytes 0-7: B-tree address.
					// Bytes 8-15: Local heap address.
					if len(msg.Data) >= 16 {
						btreeAddr := sb.Endianness.Uint64(msg.Data[0:8])
						heapAddr := sb.Endianness.Uint64(msg.Data[8:16])

						group.symbolTable = &structures.SymbolTable{
							Version:      1,
							BTreeAddress: btreeAddr,
							HeapAddress:  heapAddr,
						}
					}
				}
			}

			if group.symbolTable != nil {
				if err := group.loadChildren(); err != nil {
					return nil, utils.WrapError("load children failed", err)
				}
			}
		}
	}

	return group, nil
}

func loadTraditionalGroup(file *File, address uint64) (*Group, error) {
	// Parse the Symbol Table Node (SNOD).
	node, err := structures.ParseSymbolTableNode(file.osFile, address, file.sb)
	if err != nil {
		return nil, utils.WrapError("symbol table node parse failed", err)
	}

	// For traditional format, we need the local heap address.
	// The heap address should be in the root group's object header Symbol Table Message.
	// For now, we'll get it from the root group's symbol table message.
	// This is a bit of a chicken-and-egg problem for nested groups.

	// For root group, get heap from the symbol table message in object header.
	// For nested groups loaded via B-tree, we need to pass heap from parent.

	// TEMPORARY: Try to find heap address from root group's symbol table message.
	// This is a workaround - proper solution would pass heap address explicitly.
	var heap *structures.LocalHeap

	// Read root object header to get heap address.
	rootHeader, err := core.ReadObjectHeader(file.osFile, file.sb.RootGroup, file.sb)
	if err == nil {
		// Find symbol table message.
		for _, msg := range rootHeader.Messages {
			if msg.Type == core.MsgSymbolTable && len(msg.Data) >= 16 {
				heapAddr := file.sb.Endianness.Uint64(msg.Data[8:16])
				heap, err = structures.LoadLocalHeap(file.osFile, heapAddr, file.sb)
				if err != nil {
					return nil, utils.WrapError("local heap load failed", err)
				}
				break
			}
		}
	}

	if heap == nil {
		return nil, errors.New("could not find local heap for traditional group")
	}

	// Create group.
	group := &Group{
		file:      file,
		name:      "/",
		localHeap: heap,
	}

	// Load children from SNOD entries.
	for _, entry := range node.Entries {
		linkName, err := heap.GetString(entry.LinkNameOffset)
		if err != nil {
			return nil, utils.WrapError("link name read failed", err)
		}

		child, err := loadObject(file, entry.ObjectAddress, linkName)
		if err != nil {
			return nil, utils.WrapError("child load failed", err)
		}

		group.children = append(group.children, child)
	}

	return group, nil
}

func (g *Group) loadChildren() error {
	if g.symbolTable == nil {
		return errors.New("symbol table is nil")
	}

	heap, err := structures.LoadLocalHeap(g.file.osFile, g.symbolTable.HeapAddress, g.file.sb)
	if err != nil {
		return utils.WrapError("local heap load failed", err)
	}

	// Detect B-tree format by reading signature.
	btreeSig := readSignature(g.file.osFile, g.symbolTable.BTreeAddress)

	var entries []structures.BTreeEntry
	switch btreeSig {
	case "TREE":
		// v1 B-tree format (used in v0 files and some v1 files).
		entries, err = structures.ReadGroupBTreeEntries(g.file.osFile, g.symbolTable.BTreeAddress, g.file.sb)
	case "BTRE":
		// Modern B-tree format.
		entries, err = structures.ReadBTreeEntries(g.file.osFile, g.symbolTable.BTreeAddress, g.file.sb)
	default:
		return fmt.Errorf("unknown B-tree signature: %q at address 0x%X", btreeSig, g.symbolTable.BTreeAddress)
	}

	if err != nil {
		return utils.WrapError("B-tree read failed", err)
	}

	for _, entry := range entries {
		// Check if this is an unnamed SNOD (offset 0 AND object is SNOD) - means we should inline its children.
		// Note: offset 0 alone is NOT sufficient - it's a valid offset for the first string in the heap!
		// We must verify the object at the address is actually a SNOD, not a regular object with name at offset 0.
		sig := readSignature(g.file.osFile, entry.ObjectAddress)
		if entry.LinkNameOffset == 0 && sig == SignatureSNOD {
			// This is an unnamed SNOD container - load its children directly.
			node, err := structures.ParseSymbolTableNode(g.file.osFile, entry.ObjectAddress, g.file.sb)
			if err != nil {
				return utils.WrapError("SNOD parse failed", err)
			}

			// Add each entry from the SNOD to this group.
			for _, snodEntry := range node.Entries {
				childName, err := heap.GetString(snodEntry.LinkNameOffset)
				if err != nil {
					return utils.WrapError("SNOD child name read failed", err)
				}

				child, err := loadObject(g.file, snodEntry.ObjectAddress, childName)
				if err != nil {
					return utils.WrapError("SNOD child load failed", err)
				}

				g.children = append(g.children, child)
			}
			continue
		}

		linkName, err := heap.GetString(entry.LinkNameOffset)
		if err != nil {
			return utils.WrapError("link name read failed", err)
		}

		child, err := loadObject(g.file, entry.ObjectAddress, linkName)
		if err != nil {
			return utils.WrapError("child load failed", err)
		}

		g.children = append(g.children, child)
	}

	return nil
}

func loadObject(file *File, address uint64, name string) (Object, error) {
	// Check signature first - SNOD means traditional group format.
	sig := readSignature(file.osFile, address)
	if sig == SignatureSNOD {
		// SNOD is a symbol table node - it might be:
		// 1. A true group with multiple children.
		// 2. A redirect node with single entry (v0 files).

		node, err := structures.ParseSymbolTableNode(file.osFile, address, file.sb)
		if err != nil {
			return nil, err
		}

		// If SNOD has single entry, it's likely a redirect - load the target directly.
		if len(node.Entries) == 1 {
			// Get heap from root to read the name.
			rootHeader, err := core.ReadObjectHeader(file.osFile, file.sb.RootGroup, file.sb)
			if err != nil {
				return nil, err
			}

			var heap *structures.LocalHeap
			for _, msg := range rootHeader.Messages {
				if msg.Type == core.MsgSymbolTable && len(msg.Data) >= 16 {
					heapAddr := file.sb.Endianness.Uint64(msg.Data[8:16])
					heap, err = structures.LoadLocalHeap(file.osFile, heapAddr, file.sb)
					if err != nil {
						return nil, err
					}
					break
				}
			}

			if heap != nil {
				entry := node.Entries[0]
				linkName, err := heap.GetString(entry.LinkNameOffset)
				if err == nil && linkName == name {
					// This is a redirect node - load the target object directly.
					return loadObject(file, entry.ObjectAddress, name)
				}
			}
		}

		// Otherwise, treat as a real group.
		group, err := loadTraditionalGroup(file, address)
		if err != nil {
			return nil, err
		}
		// Override name if provided.
		if name != "" {
			group.name = name
		}
		return group, nil
	}

	// Try reading object header (works for both v1 and v2).
	header, err := core.ReadObjectHeader(file.osFile, address, file.sb)
	if err != nil {
		return nil, err
	}

	switch header.Type {
	case core.ObjectTypeGroup:
		group, err := loadGroup(file, address)
		if err != nil {
			return nil, err
		}
		// Override name if provided (but keep stored address).
		if name != "" {
			group.name = name
		}
		return group, nil
	case core.ObjectTypeDataset:
		return &Dataset{
			file:    file,
			name:    name,
			address: address, // Store address for later reading.
		}, nil
	default:
		return nil, fmt.Errorf("unsupported object type: %d", header.Type)
	}
}
