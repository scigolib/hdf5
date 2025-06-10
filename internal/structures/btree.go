package structures

import (
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

type BTreeEntry struct {
	LinkNameOffset uint64
	ObjectAddress  uint64
	CacheType      uint32
	Reserved       uint32
}

func ReadBTreeEntries(r io.ReaderAt, address uint64, sb *core.Superblock) ([]BTreeEntry, error) {
	buf := utils.GetBuffer(6)
	defer utils.ReleaseBuffer(buf)

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
	if _, err := r.ReadAt(entryCountBuf, int64(address+6)); err != nil {
		return nil, err
	}
	entryCount := sb.Endianness.Uint16(entryCountBuf)
	utils.ReleaseBuffer(entryCountBuf)

	var entries []BTreeEntry
	entrySize := 24

	for i := uint16(0); i < entryCount; i++ {
		entryBuf := utils.GetBuffer(entrySize)
		offset := address + 8 + uint64(i)*uint64(entrySize)

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
