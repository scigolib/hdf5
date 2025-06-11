package structures

import (
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

type SymbolTable struct {
	BTreeAddress uint64
	HeapAddress  uint64
}

const SymbolTableSignature = "SNOD"

func ParseSymbolTable(r io.ReaderAt, offset uint64, sb *core.Superblock) (*SymbolTable, error) {
	buf := utils.GetBuffer(24)
	defer utils.ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, int64(offset)); err != nil {
		return nil, utils.WrapError("symbol table read failed", err)
	}

	if string(buf[0:4]) != SymbolTableSignature {
		return nil, errors.New("invalid symbol table signature")
	}

	return &SymbolTable{
		BTreeAddress: sb.Endianness.Uint64(buf[8:16]),
		HeapAddress:  sb.Endianness.Uint64(buf[16:24]),
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
