// Package structures provides parsers for HDF5 internal data structures.
package structures

import (
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// BTreeEntry represents a single entry in a B-tree node for symbol table indexing.
type BTreeEntry struct {
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
func (e *BTreeEntry) IsSoftLink() bool {
	return e.CacheType == CacheTypeSoftLink
}

// ReadBTreeEntries reads B-tree entries from a leaf node at the specified address.
func ReadBTreeEntries(r io.ReaderAt, address uint64, sb *core.Superblock) ([]BTreeEntry, error) {
	buf := utils.GetBuffer(6)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(buf, int64(address)); err != nil {
		return nil, utils.WrapError("B-tree node read failed", err)
	}

	if string(buf[0:4]) != "BTRE" {
		return nil, errors.New("invalid B-tree signature")
	}

	_ = buf[4]
	level := buf[5]

	if level != 0 {
		return nil, errors.New("non-leaf nodes not supported yet")
	}

	entryCountBuf := utils.GetBuffer(2)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(entryCountBuf, int64(address+6)); err != nil {
		return nil, err
	}
	entryCount := sb.Endianness.Uint16(entryCountBuf)
	utils.ReleaseBuffer(entryCountBuf)

	var entries []BTreeEntry
	entrySize := 24

	for i := uint16(0); i < entryCount; i++ {
		entryBuf := utils.GetBuffer(entrySize)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		offset := address + 8 + uint64(i)*uint64(entrySize)

		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(entryBuf, int64(offset)); err != nil {
			utils.ReleaseBuffer(entryBuf)
			return nil, err
		}

		entries = append(entries, BTreeEntry{
			LinkNameOffset: sb.Endianness.Uint64(entryBuf[0:8]),
			ObjectAddress:  sb.Endianness.Uint64(entryBuf[8:16]),
			CacheType:      sb.Endianness.Uint32(entryBuf[16:20]),
			Reserved:       sb.Endianness.Uint32(entryBuf[20:24]),
		})

		utils.ReleaseBuffer(entryBuf)
	}

	return entries, nil
}
