package structures

// SymbolTableSignature is the 4-byte signature for symbol table nodes.
const SymbolTableSignature = "SNOD"

// SymbolTable represents an HDF5 symbol table node for legacy group storage.
type SymbolTable struct {
	Version      uint8
	EntryCount   uint16
	BTreeAddress uint64
	HeapAddress  uint64
}

// Cache type constants for symbol table entries.
const (
	// CacheTypeNone indicates no cached information.
	CacheTypeNone uint32 = 0
	// CacheTypeSymbolTable indicates cached symbol table addresses (H5G_CACHED_STAB).
	CacheTypeSymbolTable uint32 = 1
	// CacheTypeSoftLink indicates a soft link (H5G_CACHED_SLINK).
	// For soft links, ObjectAddress is HADDR_UNDEF and the target path offset
	// is stored in CachedSoftLinkOffset.
	CacheTypeSoftLink uint32 = 2
)

// SymbolTableEntry represents a single entry in a symbol table linking to an object.
// Entry format (40 bytes for 8-byte offsets):
//   - Link Name Offset (8 bytes): Offset into local heap for link name
//   - Object Header Address (8 bytes): Address of object header (HADDR_UNDEF for soft links)
//   - Cache Type (4 bytes): 0=none, 1=H5G_CACHED_STAB (symbol table), 2=H5G_CACHED_SLINK (soft link)
//   - Reserved (4 bytes)
//   - Scratch-pad Space (16 bytes):
//   - For CacheType=1: B-tree address (8 bytes) + Heap address (8 bytes)
//   - For CacheType=2: Soft link value offset (4 bytes) into local heap
type SymbolTableEntry struct {
	LinkNameOffset uint64
	ObjectAddress  uint64
	CacheType      uint32
	Reserved       uint32
	// Cached symbol table addresses (only valid when CacheType == 1)
	CachedBTreeAddr uint64
	CachedHeapAddr  uint64
	// Soft link target offset in local heap (only valid when CacheType == 2)
	CachedSoftLinkOffset uint32
}

// IsSoftLink returns true if this entry represents a soft link.
func (e *SymbolTableEntry) IsSoftLink() bool {
	return e.CacheType == CacheTypeSoftLink
}
