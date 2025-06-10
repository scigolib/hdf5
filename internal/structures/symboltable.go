package structures

import (
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

type SymbolTable struct {
	BTreeAddress uint64
	HeapAddress  uint64
}

func ParseSymbolTable(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTable, error) {
	buf := utils.GetBuffer(16)
	defer utils.ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, int64(offset)); err != nil {
		return nil, utils.WrapError("symbol table read failed", err)
	}

	return &SymbolTable{
		BTreeAddress: sb.Endianness.Uint64(buf[0:8]),
		HeapAddress:  sb.Endianness.Uint64(buf[8:16]),
	}, nil
}

type SymbolTableEntry struct {
	LinkNameOffset uint64
	ObjectAddress  uint64
	CacheType      uint32
	Reserved       uint32
}

func ParseSymbolTableEntry(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTableEntry, error) {
	buf := utils.GetBuffer(24)
	defer utils.ReleaseBuffer(buf)

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
