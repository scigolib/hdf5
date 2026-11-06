package core

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// legacyMetadataChecksumMessage is the warning text for a B-tree v2 or fractal
// heap block with a CRC32-IEEE checksum; its argument names the structure.
const legacyMetadataChecksumMessage = "%s checksum is CRC32-IEEE, as hdf5-go v0.13.4 and earlier wrote it; " +
	"the HDF5 C library cannot open this file until it is rewritten (open it with OpenForWrite and close it)"

// VerifyMetadataChecksum checks a stored metadata checksum the way the C
// library does (H5F_get_checksums + H5_checksum_metadata, H5Fio.c,
// H5checksum.c): the stored value must equal Jenkins lookup3 over data.
// A mismatch is reported as utils.ErrCorrupt; what names the structure for
// the error message.
//
// Use it for structures that no earlier hdf5-go release wrote with another
// checksum (for example the fixed and extensible array chunk indexes).
func VerifyMetadataChecksum(data []byte, stored uint32, what string) error {
	if computed := JenkinsChecksum(data); stored != computed {
		return checksumMismatch(what, stored, computed)
	}
	return nil
}

// VerifyMetadataChecksumLegacy is VerifyMetadataChecksum for the structures
// that hdf5-go releases up to v0.13.4 wrote with CRC32-IEEE instead of
// lookup3: the version 2/3 superblock, the B-tree v2 nodes, the fractal heap
// header and indirect block. Commit 01a277d (Issue #17, first released in
// v0.13.5) switched these writers to lookup3; files written before it opened
// with every hdf5-go release up to v0.14.2 and must stay readable.
//
// The fractal heap direct block is not among them: the old writer set no
// checksum flag in the heap header and put its CRC32 after the block, where
// neither C nor this reader looks, so a direct block is verified strictly.
//
// It accepts lookup3 (legacy false) or exactly CRC32-IEEE over the same bytes
// (legacy true); any other value is utils.ErrCorrupt. The C library accepts
// only lookup3, so a legacy file is readable here but not by the C tools until
// it is rewritten.
func VerifyMetadataChecksumLegacy(data []byte, stored uint32, what string) (legacy bool, err error) {
	computed := JenkinsChecksum(data)
	switch stored {
	case computed:
		return false, nil
	case crc32.ChecksumIEEE(data):
		return true, nil
	}
	return false, checksumMismatch(what, stored, computed)
}

func checksumMismatch(what string, stored, computed uint32) error {
	return fmt.Errorf("%s: checksum mismatch (stored 0x%08X, computed 0x%08X): %w",
		what, stored, computed, utils.ErrCorrupt)
}

// readChecksummedBlockLegacy is readChecksummedBlock for the version 2 B-tree
// nodes, which hdf5-go up to v0.13.4 wrote with CRC32-IEEE instead of lookup3
// (VerifyMetadataChecksumLegacy). Such a checksum is accepted and recorded as a
// WarnLegacyMetadataChecksum warning on sb; the C library accepts only lookup3
// (H5B2cache.c, through H5F_get_checksums). Every failure wraps
// utils.ErrCorrupt.
func readChecksummedBlockLegacy(r io.ReaderAt, sb *Superblock, addr, size uint64, what string) ([]byte, error) {
	if size < 4 {
		return nil, fmt.Errorf("%w: %s at 0x%X: %d bytes cannot hold a checksum", utils.ErrCorrupt, what, addr, size)
	}

	buf, err := ReadBlock(r, sb, addr, size, maxIndexBlock, what)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	n := len(buf) - 4
	legacy, err := VerifyMetadataChecksumLegacy(buf[:n], binary.LittleEndian.Uint32(buf[n:]),
		fmt.Sprintf("%s at 0x%X", what, addr))
	if err != nil {
		return nil, err
	}
	if legacy {
		sb.warnLegacyChecksumf(WarnLegacyMetadataChecksum, addr, size-4, legacyMetadataChecksumMessage, what)
	}

	return buf, nil
}
