package structures

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// SymbolTableNode represents a Symbol Table Node (SNOD structure).
// This is different from SymbolTable - it contains actual entries, not addresses.
type SymbolTableNode struct {
	Version    uint8
	NumSymbols uint16
	Entries    []SymbolTableEntry
}

// ParseSymbolTableNode parses a Symbol Table Node (SNOD).
// Format:
// - 4 bytes: Signature ("SNOD").
// - 1 byte: Version (1).
// - 1 byte: Reserved (0).
// - 2 bytes: Number of symbols.
// - Then symbol table entries follow (each entry is offsetSize*2 + 8 + 16 bytes).
func ParseSymbolTableNode(r io.ReaderAt, address uint64, sb *core.Superblock) (*SymbolTableNode, error) {
	// Read header (8 bytes).
	header := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(header)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(header, int64(address)); err != nil {
		return nil, utils.WrapError("SNOD header read failed", err)
	}

	// Check signature.
	sig := string(header[0:4])
	if sig != "SNOD" {
		return nil, fmt.Errorf("invalid SNOD signature: %q", sig)
	}

	version := header[4]
	if version != 1 {
		return nil, fmt.Errorf("unsupported SNOD version: %d", version)
	}

	numSymbols := sb.Endianness.Uint16(header[6:8])

	// Note: Symbol table nodes have a fixed capacity (typically 32 entries for K=16).
	// When parsing, we don't know the original capacity if numSymbols=0.
	// Use standard capacity (32) to allow modifications.
	capacity := uint16(32) // Standard capacity (2*K where K=16)
	if numSymbols > capacity {
		capacity = numSymbols // Increase if needed
	}

	node := &SymbolTableNode{
		Version:    version,
		NumSymbols: numSymbols,
		Entries:    make([]SymbolTableEntry, 0, capacity),
	}

	if numSymbols == 0 {
		return node, nil
	}

	// Each symbol table entry format:
	// - offsetSize bytes: Link name offset in local heap.
	// - offsetSize bytes: Object header address.
	// - 4 bytes: Cache type.
	// - 4 bytes: Reserved.
	// - 16 bytes: Scratch-pad (cache-type specific).
	entrySize := int(sb.OffsetSize)*2 + 4 + 4 + 16

	// Read all entries.
	dataSize := int(numSymbols) * entrySize
	data := utils.GetBuffer(dataSize)
	defer utils.ReleaseBuffer(data)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	entryOffset := int64(address) + 8 // After header.
	if _, err := r.ReadAt(data, entryOffset); err != nil {
		return nil, utils.WrapError("SNOD entries read failed", err)
	}

	// Parse entries.
	offset := 0
	for i := uint16(0); i < numSymbols; i++ {
		if offset+entrySize > len(data) {
			return nil, fmt.Errorf("SNOD data truncated at entry %d", i)
		}

		// Read link name offset.
		linkOffset := readAddressFromBytes(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read object header address.
		objAddr := readAddressFromBytes(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read cache type.
		cacheType := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		// Read reserved.
		reserved := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		// Skip scratch-pad (16 bytes).
		offset += 16

		node.Entries = append(node.Entries, SymbolTableEntry{
			LinkNameOffset: linkOffset,
			ObjectAddress:  objAddr,
			CacheType:      cacheType,
			Reserved:       reserved,
		})
	}

	return node, nil
}

// readAddressFromBytes reads a variable-sized address from byte slice.
func readAddressFromBytes(data []byte, size int, endianness binary.ByteOrder) uint64 {
	if size > len(data) {
		size = len(data)
	}

	switch size {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(endianness.Uint16(data[:2]))
	case 4:
		return uint64(endianness.Uint32(data[:4]))
	case 8:
		return endianness.Uint64(data[:8])
	default:
		// Pad to 8 bytes.
		var buf [8]byte
		copy(buf[:], data[:size])
		return endianness.Uint64(buf[:])
	}
}

// NewSymbolTableNode creates a new empty symbol table node with the given capacity.
// capacity is typically 2*K where K is the B-tree order (default: K=16, so capacity=32).
func NewSymbolTableNode(capacity uint16) *SymbolTableNode {
	return &SymbolTableNode{
		Version:    1,
		NumSymbols: 0,
		Entries:    make([]SymbolTableEntry, 0, capacity),
	}
}

// AddEntry adds a symbol table entry to the node.
// Returns an error if the node would exceed capacity.
func (stn *SymbolTableNode) AddEntry(entry SymbolTableEntry) error {
	capacity := cap(stn.Entries)
	if int(stn.NumSymbols) >= capacity {
		return fmt.Errorf("symbol table node is full (%d/%d)", stn.NumSymbols, capacity)
	}

	stn.Entries = append(stn.Entries, entry)
	stn.NumSymbols++
	return nil
}

// WriteAt writes the symbol table node to w at the specified address.
// offsetSize determines the size of addresses in the file (typically 8 bytes).
// maxEntries is the fixed size of the node (for padding with zeros).
func (stn *SymbolTableNode) WriteAt(w io.WriterAt, address uint64, offsetSize uint8, maxEntries uint16, endianness binary.ByteOrder) error {
	// Calculate entry size: 2*offsetSize + 4 + 4 + 16
	entrySize := int(offsetSize)*2 + 4 + 4 + 16

	// Total size: 8-byte header + (maxEntries * entrySize)
	totalSize := 8 + int(maxEntries)*entrySize
	buf := make([]byte, totalSize)

	// Write header
	copy(buf[0:4], []byte("SNOD"))
	buf[4] = stn.Version
	buf[5] = 0 // Reserved
	endianness.PutUint16(buf[6:8], stn.NumSymbols)

	// Write entries
	pos := 8
	for i := uint16(0); i < maxEntries; i++ {
		if i < stn.NumSymbols {
			entry := stn.Entries[i]

			// Write link name offset
			writeAddressToBytes(buf[pos:], entry.LinkNameOffset, int(offsetSize), endianness)
			pos += int(offsetSize)

			// Write object header address
			writeAddressToBytes(buf[pos:], entry.ObjectAddress, int(offsetSize), endianness)
			pos += int(offsetSize)

			// Write cache type
			endianness.PutUint32(buf[pos:pos+4], entry.CacheType)
			pos += 4

			// Write reserved
			endianness.PutUint32(buf[pos:pos+4], entry.Reserved)
			pos += 4

			// Skip scratch-pad (16 bytes, already zero)
			pos += 16
		} else {
			// Write empty entry (all zeros)
			pos += entrySize
		}
	}

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	_, err := w.WriteAt(buf, int64(address))
	return err
}

// writeAddressToBytes writes a variable-sized address to byte slice.
func writeAddressToBytes(data []byte, addr uint64, size int, endianness binary.ByteOrder) {
	if size > len(data) {
		size = len(data)
	}

	switch size {
	case 1:
		data[0] = byte(addr)
	case 2:
		endianness.PutUint16(data[:2], uint16(addr))
	case 4:
		endianness.PutUint32(data[:4], uint32(addr))
	case 8:
		endianness.PutUint64(data[:8], addr)
	default:
		// Pad to requested size
		var buf [8]byte
		endianness.PutUint64(buf[:], addr)
		copy(data[:size], buf[:size])
	}
}
