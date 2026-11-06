package structures

// SymbolTableSignature is the 4-byte signature for symbol table nodes.
const SymbolTableSignature = "SNOD"

// SymbolTable holds the two addresses that locate an old-style group's links:
// the root of its group B-tree and its local heap, as the symbol table message
// (H5O_stab_t, H5Oprivate.h) or a cached H5G_CACHED_STAB entry carries them.
// Symbol table nodes themselves are decoded by ParseSymbolTableNode.
type SymbolTable struct {
	Version      uint8
	EntryCount   uint16
	BTreeAddress uint64
	HeapAddress  uint64
}

// Cache type constants for symbol table entries (H5G_cache_type_t,
// H5Gpkg.h:65-67).
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

// SymbolTableEntry represents a single entry in a symbol table linking to an
// object. Its encoding (H5G_ent_decode / H5G_ent_encode, H5Gent.c; size
// H5G_SIZEOF_ENTRY, H5Gprivate.h:38-43) is:
//   - Link Name Offset (sizeof_size bytes, a length): offset into the local heap
//   - Object Header Address (sizeof_addr bytes): HADDR_UNDEF for soft links
//   - Cache Type (4 bytes): 0=none, 1=H5G_CACHED_STAB, 2=H5G_CACHED_SLINK
//   - Reserved (4 bytes)
//   - Scratch-pad Space (16 bytes):
//   - For CacheType=1: B-tree address + local heap address (sizeof_addr each)
//   - For CacheType=2: Soft link value offset (4 bytes) into the local heap
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
