package hdf5

import (
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
)

// CreateHardLink creates a hard link to an existing object.
//
// Hard links are additional names for the same object. All hard links point to
// the same object header address. When one link is modified, changes are visible
// through all other links because they share the same data.
//
// Parameters:
//   - linkPath: Path where the new link will be created (e.g., "/group1/link_name")
//   - targetPath: Path to the existing object to link to (e.g., "/group2/dataset1")
//
// Returns:
//   - error: Non-nil if link creation fails
//
// Behavior:
//   - Validates both paths exist and are properly formatted
//   - Looks up target object's header address
//   - Increments reference count on target object header
//   - Creates link entry in parent group pointing to target address
//   - Supports linking datasets and groups
//   - Works with both symbol table and dense group formats
//
// Example:
//
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create dataset
//	fw.CreateDataset("/data/temperature", []float64{1.0, 2.0, 3.0})
//
//	// Create hard link to dataset
//	err := fw.CreateHardLink("/data/temp_link", "/data/temperature")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Now /data/temperature and /data/temp_link point to the same dataset
//
// Limitations:
//   - Target must exist before creating link
//   - Parent group must exist before creating link
//   - Reference count stored in object header (v1) or RefCount message (v2)
//   - No circular link detection
//
// Reference: H5L.c - H5Lcreate_hard().
func (fw *FileWriter) CreateHardLink(linkPath, targetPath string) error {
	// Validate paths
	if err := validateLinkPath(linkPath); err != nil {
		return fmt.Errorf("invalid link path: %w", err)
	}
	if err := validateLinkPath(targetPath); err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}

	// Resolve target object address
	targetAddr, err := fw.resolveObjectAddress(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve target %q: %w", targetPath, err)
	}

	// Read target object header to increment reference count
	targetHeader, err := core.ReadObjectHeader(fw.writer, targetAddr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("failed to read target object header at 0x%x: %w", targetAddr, err)
	}

	// Increment reference count
	newRefCount := targetHeader.IncrementReferenceCount()

	// Write updated object header back to file
	if err := writeObjectHeaderWithRefCount(fw, targetAddr, targetHeader); err != nil {
		return fmt.Errorf("failed to update reference count: %w", err)
	}

	// Parse link path to find parent and link name
	parent, linkName := parsePath(linkPath)

	// Validate parent exists
	if parent != "" && parent != "/" {
		if _, exists := fw.groups[parent]; !exists {
			return fmt.Errorf("parent group %q does not exist (create it first)", parent)
		}
	}

	// Create link in parent group
	if err := fw.linkToParent(parent, linkName, targetAddr); err != nil {
		// Rollback: decrement reference count
		targetHeader.DecrementReferenceCount()
		_ = writeObjectHeaderWithRefCount(fw, targetAddr, targetHeader)
		return fmt.Errorf("failed to create link in parent group: %w", err)
	}

	// Success
	_ = newRefCount // Used for debugging if needed
	return nil
}

// validateLinkPath validates that a link path is properly formatted.
func validateLinkPath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with '/' (got %q)", path)
	}
	if path == "/" {
		return fmt.Errorf("cannot create link to root group")
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("path cannot contain consecutive slashes (got %q)", path)
	}
	return nil
}

// writeObjectHeaderWithRefCount writes an object header back to disk with updated reference count.
// This handles the difference between v1 (refcount in header) and v2 (RefCount message).
//
// For v1: Updates refcount field directly in header bytes 4-7
// For v2: Updates or creates RefCount message (type 0x0016)
//
// Parameters:
//   - fw: FileWriter
//   - addr: Address of object header
//   - oh: Object header with updated ReferenceCount field
//
// Returns:
//   - error: Non-nil if write fails
func writeObjectHeaderWithRefCount(fw *FileWriter, addr uint64, oh *core.ObjectHeader) error {
	switch oh.Version {
	case 1:
		return writeV1RefCount(fw, addr, oh)
	case 2:
		return writeV2RefCount(fw, addr, oh)
	default:
		return fmt.Errorf("unsupported object header version: %d", oh.Version)
	}
}

// writeV1RefCount writes reference count for v1 object header.
func writeV1RefCount(fw *FileWriter, addr uint64, oh *core.ObjectHeader) error {
	// V1: Reference count is stored directly in header bytes 4-7
	// Write only the refcount field (4 bytes at offset 4)
	refCountBytes := make([]byte, 4)
	fw.file.sb.Endianness.PutUint32(refCountBytes, oh.ReferenceCount)

	_, err := fw.writer.WriteAt(refCountBytes, int64(addr+4)) //nolint:gosec // Safe: addr is file address
	if err != nil {
		return fmt.Errorf("failed to write v1 refcount: %w", err)
	}
	return nil
}

// writeV2RefCount writes reference count for v2 object header.
func writeV2RefCount(fw *FileWriter, addr uint64, oh *core.ObjectHeader) error {
	// V2: Reference count stored in RefCount message (type 0x0016)
	// If refcount > 1, we need to add/update RefCount message
	if oh.ReferenceCount > 1 {
		if err := ensureRefCountMessage(fw, oh); err != nil {
			return err
		}
	}

	// Write entire object header back to disk. The RefCount message may have
	// grown the header past its allocation at the end of the file, so use the
	// bounds-checked writer that advances the allocator (and thus the
	// superblock EOA on Close) — a bare WriteObjectHeader left the EOA 8
	// bytes short and the C library rejected the file ("exceeds EOA").
	if err := writeOHDRWithBoundsCheck(fw, addr, oh, fw.file.sb); err != nil {
		return fmt.Errorf("failed to write v2 object header: %w", err)
	}
	return nil
}

// ensureRefCountMessage ensures RefCount message exists and is updated.
func ensureRefCountMessage(fw *FileWriter, oh *core.ObjectHeader) error {
	// RefCount message body: version (1 byte, always 0) + 4-byte count.
	// Reference: H5Orefcount.c - H5O__refcount_decode().

	// Check if RefCount message already exists
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgRefCount && len(msg.Data) >= 5 {
			// Update existing RefCount message data (skip version byte)
			fw.file.sb.Endianness.PutUint32(msg.Data[1:5], oh.ReferenceCount)
			return nil
		}
	}

	// Create new RefCount message
	refCountData := make([]byte, 5)
	refCountData[0] = 0 // message version
	fw.file.sb.Endianness.PutUint32(refCountData[1:], oh.ReferenceCount)

	// Add message to header
	err := core.AddMessageToObjectHeader(oh, core.MsgRefCount, refCountData)
	if err != nil {
		return fmt.Errorf("failed to add RefCount message: %w", err)
	}
	return nil
}

// CreateSoftLink creates a symbolic link to a path within the HDF5 file.
//
// Soft links (symbolic links) store a path string that is resolved when accessed.
// Unlike hard links, soft links do not increment reference counts and can point
// to objects that don't exist yet (dangling links are allowed).
//
// Parameters:
//   - linkPath: Path where the soft link will be created (e.g., "/group1/link_to_dataset")
//   - targetPath: Target path within file (e.g., "/group2/dataset1")
//
// Returns:
//   - error: Non-nil if link creation fails
//
// Behavior:
//   - Validates linkPath format (must be absolute path)
//   - Target path does NOT need to exist (dangling links allowed)
//   - Creates link message with soft link type
//   - Adds link entry in parent group's symbol table
//   - Link stores target path as string (not object address)
//   - When accessed, target path is resolved dynamically
//
// Example:
//
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create dataset
//	fw.CreateDataset("/data/temperature", []float64{1.0, 2.0, 3.0})
//
//	// Create soft link (target exists)
//	err := fw.CreateSoftLink("/links/temp_link", "/data/temperature")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create dangling link (target doesn't exist yet)
//	err = fw.CreateSoftLink("/links/future_link", "/data/future_dataset")
//	// This is allowed - target can be created later
//
// Limitations:
//   - Symbol table format only (links cannot be added to dense-format groups)
//   - Soft links are not resolved on read
//   - No circular link detection
//
// HDF5 Spec: Section IV.A.2.f "Link Message" - Type 1 (Soft Link)
// Reference: H5L.c - H5Lcreate_soft().
func (fw *FileWriter) CreateSoftLink(linkPath, targetPath string) error {
	// Validate paths
	if err := validateLinkPath(linkPath); err != nil {
		return fmt.Errorf("invalid link path: %w", err)
	}

	if err := validateSoftLinkTargetPath(targetPath); err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}

	// Parse link path to find parent and link name
	parent, linkName := parsePath(linkPath)

	// Validate parent exists
	if parent != "" && parent != "/" {
		if _, exists := fw.groups[parent]; !exists {
			return fmt.Errorf("parent group %q does not exist (create it first)", parent)
		}
	}

	// In the symbol-table group format a soft link is NOT an object: it is a
	// symbol table entry with cache type 2 (H5G_CACHED_SLINK), an undefined
	// object header address, and the target path stored in the parent
	// group's local heap. (An earlier implementation wrote a standalone
	// object header holding a Link message — the C library could not
	// classify that object and rejected the file.)
	if err := fw.linkEntryToParent(parent, linkName, 0, targetPath); err != nil {
		return fmt.Errorf("failed to add soft link to parent group: %w", err)
	}

	return nil
}

// validateSoftLinkTargetPath validates the target path format for soft links.
// Unlike hard links, target doesn't need to exist, but must be valid format.
func validateSoftLinkTargetPath(path string) error {
	if path == "" {
		return fmt.Errorf("target path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("target path must be absolute (start with '/'), got %q", path)
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("target path cannot contain consecutive slashes (got %q)", path)
	}
	return nil
}

// CreateExternalLink creates a link to an object in another HDF5 file.
// The link stores the external file path and object path within that file.
// Both files must exist when the external link is accessed (lazy resolution).
//
// Parameters:
//   - linkPath: Path where external link will be created (e.g., "/links/external1")
//   - fileName: External HDF5 file name (absolute or relative path)
//   - objectPath: Path to object within external file (e.g., "/dataset1")
//
// Returns:
//   - error: if validation fails or creation fails
//
// Examples:
//
//	fw.CreateExternalLink("/links/ext1", "other.h5", "/data/dataset1")
//	fw.CreateExternalLink("/links/ext2", "/absolute/path/file.h5", "/group1")
//
// Behavior:
//   - Validates all paths
//   - Creates link message with external link type
//   - Stores external file name and object path
//   - Adds link entry in parent group's symbol table
//   - No file existence check (lazy resolution)
//
// Security:
//   - Path traversal prevention (blocks ".." in file names)
//   - File path stored as-is (absolute or relative)
//
// Limitations:
//   - Symbol table format only (links cannot be added to dense-format groups)
//   - External links are not resolved on read
//   - No file caching or performance optimization
//
// HDF5 Spec: Section IV.A.2.f "Link Message" - Type 64 (External Link)
// Reference: H5Lcreate_external() in H5L.c.
func (fw *FileWriter) CreateExternalLink(linkPath, fileName, objectPath string) error {
	// Validate link path
	if err := validateLinkPath(linkPath); err != nil {
		return fmt.Errorf("invalid link path: %w", err)
	}

	// Validate file name
	if err := validateExternalFileName(fileName); err != nil {
		return fmt.Errorf("invalid file name: %w", err)
	}

	// Validate object path (use same rules as soft link target)
	if err := validateSoftLinkTargetPath(objectPath); err != nil {
		return fmt.Errorf("invalid object path: %w", err)
	}

	// Parse link path to find parent and link name
	parent, linkName := parsePath(linkPath)

	// Validate parent exists
	if parent != "" && parent != "/" {
		if _, exists := fw.groups[parent]; !exists {
			return fmt.Errorf("parent group %q does not exist (create it first)", parent)
		}
	}

	// Create external link message
	linkMsg := &core.LinkMessage{
		Version: 1,
		Flags:   core.LinkFlagLinkTypeFieldBit | core.LinkFlagCharSetBit, // Bits 3 + 4 set
		Type:    core.LinkTypeExternal,
		CharSet: 0, // ASCII
		Name:    linkName,
		// LinkValue: file name + object path (will be set below)
	}

	// Encode external link value
	// External link format: fileNameLength(2) + fileName + pathLength(2) + path
	fileNameBytes := []byte(fileName)
	objectPathBytes := []byte(objectPath)

	// Validate lengths fit in uint16
	if len(fileNameBytes) > 65535 {
		return fmt.Errorf("file name too long: %d bytes (max 65535)", len(fileNameBytes))
	}
	if len(objectPathBytes) > 65535 {
		return fmt.Errorf("object path too long: %d bytes (max 65535)", len(objectPathBytes))
	}

	linkValue := make([]byte, 2+len(fileNameBytes)+2+len(objectPathBytes))
	offset := 0

	// Write file name length (2 bytes)
	fw.file.sb.Endianness.PutUint16(linkValue[offset:offset+2], uint16(len(fileNameBytes))) //nolint:gosec // Validated above
	offset += 2

	// Write file name
	copy(linkValue[offset:], fileNameBytes)
	offset += len(fileNameBytes)

	// Write object path length (2 bytes)
	fw.file.sb.Endianness.PutUint16(linkValue[offset:offset+2], uint16(len(objectPathBytes))) //nolint:gosec // Validated above
	offset += 2

	// Write object path
	copy(linkValue[offset:], objectPathBytes)

	linkMsg.LinkValue = linkValue

	// Encode link message
	linkMsgData, err := core.EncodeLinkMessage(linkMsg, fw.file.sb)
	if err != nil {
		return fmt.Errorf("failed to encode external link message: %w", err)
	}

	// Create object header writer for the external link
	linkOHW := &core.ObjectHeaderWriter{
		Version: 2, // Use v2 for modern format
		Flags:   0,
		Messages: []core.MessageWriter{
			{
				Type: core.MsgLinkMessage, // 0x0006 - Link message
				Data: linkMsgData,
			},
		},
	}

	// Pre-allocate with padding for future attributes.
	linkOHW.PadToSize(core.MinOHDRAllocSize)

	// Calculate object header size
	headerSize, err := calculateObjectHeaderSize(linkOHW)
	if err != nil {
		return fmt.Errorf("failed to calculate header size: %w", err)
	}

	// Allocate space for object header
	linkAddr, err := fw.writer.Allocate(headerSize)
	if err != nil {
		return fmt.Errorf("failed to allocate space for external link object header: %w", err)
	}

	// Write object header
	_, err = linkOHW.WriteTo(fw.writer, linkAddr)
	if err != nil {
		return fmt.Errorf("failed to write external link object header: %w", err)
	}

	// Add link to parent group's symbol table
	if err := fw.linkToParent(parent, linkName, linkAddr); err != nil {
		return fmt.Errorf("failed to add external link to parent group: %w", err)
	}

	return nil
}

// validateExternalFileName validates the external file name.
// Must not be empty, should have .h5 or .hdf5 extension.
func validateExternalFileName(fileName string) error {
	if fileName == "" {
		return fmt.Errorf("file name cannot be empty")
	}

	// Prevent path traversal attacks
	if strings.Contains(fileName, "..") {
		return fmt.Errorf("file name cannot contain '..' (path traversal)")
	}

	// Extension is not checked strictly: any extension is accepted
	// to support custom file formats.

	return nil
}

// Note: The following methods are already implemented in group_write.go and are reused here:
// - parsePath(path string) (parent, name string)
// - linkToParent(parentPath, childName string, childAddr uint64) error
// - resolveObjectAddress(path string) (uint64, error)
