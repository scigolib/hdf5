package structures

import (
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// SymbolTableSignature is the 4-byte signature for symbol table nodes.
const SymbolTableSignature = "SNOD"

// SymbolTable represents an HDF5 symbol table node for legacy group storage.
type SymbolTable struct {
	Version      uint8
	EntryCount   uint16
	BTreeAddress uint64
	HeapAddress  uint64
}

// ParseSymbolTable parses a symbol table node from the specified file offset.
func ParseSymbolTable(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTable, error) {
	buf := utils.GetBuffer(24)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(offset)); err != nil {
		return nil, utils.WrapError("symbol table read failed", err)
	}

	if string(buf[0:4]) != SymbolTableSignature {
		return nil, errors.New("invalid symbol table signature")
	}

	version := buf[4]
	if version != 1 {
		return nil, fmt.Errorf("unsupported symbol table version: %d", version)
	}

	return &SymbolTable{
		Version:      version,
		EntryCount:   sb.Endianness.Uint16(buf[6:8]),
		BTreeAddress: sb.Endianness.Uint64(buf[8:16]),
		HeapAddress:  sb.Endianness.Uint64(buf[16:24]),
	}, nil
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

// ParseSymbolTableEntry parses a symbol table entry from the specified file offset.
func ParseSymbolTableEntry(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTableEntry, error) {
	// Full entry is 40 bytes for 8-byte offsets
	buf := utils.GetBuffer(40)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(offset)); err != nil {
		return nil, utils.WrapError("symbol table entry read failed", err)
	}

	entry := &SymbolTableEntry{
		LinkNameOffset: sb.Endianness.Uint64(buf[0:8]),
		ObjectAddress:  sb.Endianness.Uint64(buf[8:16]),
		CacheType:      sb.Endianness.Uint32(buf[16:20]),
		Reserved:       sb.Endianness.Uint32(buf[20:24]),
	}

	// Parse scratch-pad based on cache type
	switch entry.CacheType {
	case CacheTypeSymbolTable:
		// CacheType == 1 (H5G_CACHED_STAB): cached symbol table addresses
		entry.CachedBTreeAddr = sb.Endianness.Uint64(buf[24:32])
		entry.CachedHeapAddr = sb.Endianness.Uint64(buf[32:40])
	case CacheTypeSoftLink:
		// CacheType == 2 (H5G_CACHED_SLINK): soft link value offset
		// The soft link target path is stored in local heap at this offset
		entry.CachedSoftLinkOffset = sb.Endianness.Uint32(buf[24:28])
	}

	return entry, nil
}

// ReadSymbolTableEntries reads all entries from a symbol table node.
func ReadSymbolTableEntries(r io.ReaderAt, tableAddress uint64, table *SymbolTable, sb *core.Superblock) ([]SymbolTableEntry, error) {
	if table.EntryCount == 0 {
		return nil, nil
	}

	// Entries follow immediately after symbol table header (8 bytes for SNOD)
	offset := tableAddress + 8
	entries := make([]SymbolTableEntry, 0, table.EntryCount)

	for i := uint16(0); i < table.EntryCount; i++ {
		entry, err := ParseSymbolTableEntry(r, offset, sb)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
		offset += 40 // Each entry is 40 bytes (with scratch-pad)
	}

	return entries, nil
}
