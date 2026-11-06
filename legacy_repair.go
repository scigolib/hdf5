package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/scigolib/hdf5/internal/core"
)

// scanForWrite reads every structure of f that OpenForWrite may later change
// or that the C library would verify, so their findings are on f.Warnings():
// the object headers of every object (loaded by Open), every dataset's
// datatype, dataspace and layout, and every object's attributes, compact and
// dense (the dense name index B-tree and fractal heap).
// It goes through the reader's own validated walkers and budgets.
//
// An attribute that is valid HDF5 this library cannot decode (ErrNotSupported)
// does not stop the scan: the file was writable before and stays so; the
// blocks behind it are not verified.
func scanForWrite(f *File) error {
	var scanErr error
	f.Walk(func(path string, obj Object) {
		if scanErr != nil {
			return
		}
		var err error
		switch o := obj.(type) {
		case *Dataset:
			// The descriptor decodes the datatype (WarnLegacyDatatype).
			if _, err = o.descriptor(); err == nil {
				_, err = o.Attributes()
			}
		case *Group:
			_, err = o.Attributes()
		}
		if err != nil && !errors.Is(err, ErrNotSupported) {
			scanErr = fmt.Errorf("%s: %w", path, err)
		}
	})

	return scanErr
}

// refuseUnrepairableHeaders returns an ErrNeedsRepack error when a scanned
// version 2 object header or continuation chunk has no valid checksum
// (WarnHeaderChecksum, WarnHeaderChunkSize) or a datatype is in the encoding
// of hdf5-go v0.13.9 and earlier (WarnLegacyDatatype). Writing into such a
// file would leave it no more readable by the C library, and repairing it in
// place needs every header rewritten, so OpenForWrite refuses before it writes
// anything. Every dataset header the early datatype encoder came with is a
// version 2 header without a valid checksum, so repairing the datatype bytes
// alone, though they are the same size, would not make such a file readable.
func refuseUnrepairableHeaders(f *File) error {
	for _, w := range f.Warnings() {
		if w.Code == WarnHeaderChecksum || w.Code == WarnHeaderChunkSize || w.Code == WarnLegacyDatatype {
			return fmt.Errorf("%w: %s", ErrNeedsRepack, w.Message)
		}
	}

	return nil
}

// repairLegacyChecksums rewrites, in place, every CRC32-IEEE checksum the scan
// found (superblock, version 2 B-tree nodes, fractal heap headers and indirect
// blocks; hdf5-go v0.13.4 and earlier) as the lookup3 checksum the C library
// verifies (H5F_get_checksums, H5Fio.c). Only the four checksum bytes of each
// block are written, after the block is read again and its stored value is
// confirmed to be CRC32 over the same bytes; nothing else changes.
//
// It runs before any other write. A failure part-way leaves each block with
// either its old CRC32 or its new lookup3 checksum, both of which this
// library reads, so the file is no worse than before. When the scan found
// more findings than the warning list keeps, the list of blocks is incomplete
// and nothing is written.
func repairLegacyChecksums(f *File, r io.ReaderAt, w io.WriterAt) error {
	spans, complete := f.sb.LegacyChecksums()
	if len(spans) == 0 {
		return nil
	}
	if !complete {
		return fmt.Errorf("%w: the file has more legacy checksums than one pass repairs; rewrite it into a new file",
			ErrNeedsRepack)
	}

	sum := make([]byte, 4)
	for _, s := range spans {
		buf, err := core.ReadBlock(r, f.sb, s.Addr, s.Len+4, s.Len+4, "legacy checksum block")
		if err != nil {
			return fmt.Errorf("repairing the checksum at 0x%X: %w", s.Addr, err)
		}
		if stored := binary.LittleEndian.Uint32(buf[s.Len:]); stored != crc32.ChecksumIEEE(buf[:s.Len]) {
			return fmt.Errorf("%w: repairing the checksum at 0x%X: stored 0x%08X is no longer CRC32",
				ErrCorrupt, s.Addr, stored)
		}
		binary.LittleEndian.PutUint32(sum, core.JenkinsChecksum(buf[:s.Len]))
		//nolint:gosec // G115: ReadBlock checked the address against the platform limit
		if _, err := w.WriteAt(sum, int64(s.Addr+s.Len)); err != nil {
			return fmt.Errorf("repairing the checksum at 0x%X: %w", s.Addr, err)
		}
	}

	return nil
}
