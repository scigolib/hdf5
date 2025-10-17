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

// SymbolTableEntry represents a single entry in a symbol table linking to an object.
type SymbolTableEntry struct {
	LinkNameOffset uint64
	ObjectAddress  uint64
	CacheType      uint32
	Reserved       uint32
}

// ParseSymbolTableEntry parses a symbol table entry from the specified file offset.
func ParseSymbolTableEntry(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTableEntry, error) {
	buf := utils.GetBuffer(24)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(offset)); err != nil {
		return nil, utils.WrapError("symbol table entry read failed", err)
	}

	return &SymbolTableEntry{
		LinkNameOffset: sb.Endianness.Uint64(buf[0:8]),
		ObjectAddress:  sb.Endianness.Uint64(buf[8:16]),
		CacheType:      sb.Endianness.Uint32(buf[16:20]),
		Reserved:       sb.Endianness.Uint32(buf[20:24]),
	}, nil
}

// ReadSymbolTableEntries reads all entries from a symbol table node.
func ReadSymbolTableEntries(r io.ReaderAt, tableAddress uint64, table *SymbolTable, sb *core.Superblock) ([]SymbolTableEntry, error) {
	if table.EntryCount == 0 {
		return nil, nil
	}

	// Entries follow immediately after symbol table header (24 bytes)
	offset := tableAddress + 24
	entries := make([]SymbolTableEntry, 0, table.EntryCount)

	for i := uint16(0); i < table.EntryCount; i++ {
		entry, err := ParseSymbolTableEntry(r, offset, sb)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
		offset += 24 // Each entry is 24 bytes
	}

	return entries, nil
}
