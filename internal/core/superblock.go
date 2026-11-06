// Package core provides low-level HDF5 file format parsing and generation.
// It handles superblocks, object headers, messages, and other HDF5 structures
// without CGo dependencies.
package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// HDF5 file signature and supported superblock versions.
const (
	Signature = "\x89HDF\r\n\x1a\n"
	Version0  = 0
	Version2  = 2
	Version3  = 3
)

// Minimum on-disk size of a superblock header, per version. Used to sanity
// check the End-of-File address: a well-formed file cannot be shorter than
// its own superblock.
const (
	minSuperblockSizeV0 = 96
	minSuperblockSizeV2 = 48
)

// Superblock represents the HDF5 file superblock containing file-level metadata.
type Superblock struct {
	Version     uint8
	OffsetSize  uint8
	LengthSize  uint8
	BaseAddress uint64
	// EOFAddress is the superblock's End-of-File address — the end of the file's
	// address space, the analog of H5F_get_eoa() in the C library (ADR-003).
	EOFAddress     uint64
	RootGroup      uint64
	Endianness     binary.ByteOrder
	SuperExtension uint64
	DriverInfo     uint64

	// SymLeafK, SymInternalK and ChunkInternalK are the version 1 B-tree K
	// values of the file (H5F_super_t sym_leaf_k, btree_k[H5B_SNODE_ID],
	// btree_k[H5B_CHUNK_ID], H5Fpkg.h): a group's symbol table node holds up to
	// 2*SymLeafK entries, a group B-tree node up to 2*SymInternalK children and
	// a chunk B-tree node up to 2*ChunkInternalK. A version 0 superblock stores
	// the first two (H5Fsuper_cache.c:476-486); a version 2/3 superblock keeps
	// non-default values in its extension's B-tree 'K' values message
	// (H5O_BTREEK, H5Fsuper.c:699-715). Otherwise each is the C default
	// (DefaultSymLeafK, DefaultSymInternalK, DefaultChunkInternalK). Version 1,
	// which also stores ChunkInternalK, is not read.
	SymLeafK       uint16
	SymInternalK   uint16
	ChunkInternalK uint16

	// V0-specific: Cached symbol table info for root group
	// These are only used when Version == 0
	RootBTreeAddr uint64 // B-tree address for root group (v0 only)
	RootHeapAddr  uint64 // Local heap address for root group (v0 only)

	// shared caches the shared messages resolved in this file and bounds the
	// work spent on them (shared_message.go). The superblock is the one object
	// every read of a file carries, so the cache lives as long as the File.
	shared sharedCache

	// warnings collects the findings that do not stop the file from being
	// read (legacy hdf5-go structures, header checksums), bounded per file
	// (warnings.go).
	warnings warningSink

	// ExternalFiles opens the raw data files of datasets stored through an
	// External File List (external_storage.go). Nil refuses every external
	// file; the root package sets it when it opens a file by name, with the
	// policy that decides which names may be opened.
	ExternalFiles ExternalOpener
}

// Superblock layout constants from the C library.
const (
	// superblockFixedSize is the signature plus the version byte
	// (H5F_SUPERBLOCK_FIXED_SIZE, H5Fpkg.h:75).
	superblockFixedSize = 9

	// superblockV0PrefixSize is the fixed part of a version 0 superblock before
	// the first address field (H5F_SUPERBLOCK_VARLEN_SIZE_COMMON, H5Fpkg.h:93-99).
	superblockV0PrefixSize = superblockFixedSize + 15

	// superblockV2PrefixSize is the fixed part of a version 2/3 superblock
	// before the first address field: sizes and status flags
	// (H5F_SUPERBLOCK_VARLEN_SIZE_V2, H5Fpkg.h:116-123).
	superblockV2PrefixSize = superblockFixedSize + 3

	// metadataChecksumSize is the size of a metadata checksum (H5F_SIZEOF_CHKSUM).
	metadataChecksumSize = 4

	// symbolEntryFixedSize is the cache type, the reserved word and the scratch
	// pad of a symbol table entry (H5G_SIZEOF_ENTRY, H5Gprivate.h:38-43).
	symbolEntryFixedSize = 4 + 4 + 16

	// superblockAllFlags are the status flags a superblock may carry
	// (H5F_SUPER_ALL_FLAGS, H5Fpkg.h:50-53).
	superblockAllFlags = 0x01 | 0x02 | 0x04

	// The cache types of a symbol table entry that the C library decodes
	// (H5G_NOTHING_CACHED, H5G_CACHED_STAB, H5G_CACHED_SLINK; H5Gpkg.h:65-67).
	symbolEntryCacheNothing = 0
	symbolEntryCacheStab    = 1
	symbolEntryCacheSlink   = 2
)

// minimumSuperblockSize returns the on-disk size in bytes of the superblock
// for the given version and field widths (H5F_SUPERBLOCK_FIXED_SIZE plus
// H5F_SUPERBLOCK_VARLEN_SIZE, H5Fpkg.h:75-127).
func minimumSuperblockSize(version, offsetSize, lengthSize uint8) uint64 {
	addr := uint64(offsetSize)
	if version == Version0 {
		// Base, free-space, EOF and driver addresses, then the root group
		// symbol table entry (link name offset is a length).
		return superblockV0PrefixSize + 4*addr + uint64(lengthSize) + addr + symbolEntryFixedSize
	}
	// Base, extension, EOF and root group addresses, then the checksum.
	return superblockV2PrefixSize + 4*addr + metadataChecksumSize
}

// ReadSuperblock reads and parses the HDF5 superblock from the file.
// It supports versions 0, 2, and 3 of the superblock format.
func ReadSuperblock(r io.ReaderAt) (*Superblock, error) {
	addr, err := locateSuperblock(r)
	if err != nil {
		return nil, err
	}

	size, sizeKnown := utils.ReaderSize(r)

	return readSuperblockAt(r, addr, size, sizeKnown)
}

// ErrNoSignature reports a file with no HDF5 superblock signature at any of
// the locations the format allows. Open maps it to "not an HDF5 file".
var ErrNoSignature = errors.New("HDF5 signature not found")

// locateSuperblock finds the superblock signature the way the C library does
// (H5FD_locate_signature, H5FDint.c:148): at address 0, then at every power of
// two starting at 512, until the file size is passed. The address found is the
// file's base address.
func locateSuperblock(r io.ReaderAt) (uint64, error) {
	size, sizeKnown := utils.ReaderSize(r)

	// The C loop runs n from 8 (address 0) to the file's bit length, with a
	// floor of 9 so that address 512 is always attempted.
	maxPow := 9
	if sizeKnown {
		maxPow = 0
		for v := size; v != 0; v >>= 1 {
			maxPow++
		}
		if maxPow < 9 {
			maxPow = 9
		}
	}

	buf := utils.GetBuffer(len(Signature))
	defer utils.ReleaseBuffer(buf)

	for n := 8; n < maxPow; n++ {
		addr := uint64(0)
		if n != 8 {
			addr = uint64(1) << n
		}

		if sizeKnown && addr+uint64(len(Signature)) > size {
			break
		}

		if _, err := r.ReadAt(buf, int64(addr)); err != nil {
			continue
		}
		if string(buf) == Signature {
			return addr, nil
		}
	}

	return 0, ErrNoSignature
}

// readSuperblockAt parses the superblock found at addr, which is also the
// file's effective base address: HDF5 metadata addresses are relative to it.
// It follows H5F__superblock_prefix_decode and H5F__cache_superblock_deserialize
// (H5Fsuper_cache.c:140-214, :400-600); the checksum of a version 2/3
// superblock is verified first, as the metadata cache does through
// H5F__cache_superblock_verify_chksum (:365-391).
func readSuperblockAt(r io.ReaderAt, addr, size uint64, sizeKnown bool) (*Superblock, error) {
	buf := utils.GetBuffer(128)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: addr is a file offset located within the file
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, utils.WrapError("superblock read failed", err)
	}
	img := buf[:n]
	if len(img) < superblockFixedSize {
		return nil, fmt.Errorf("file too small to contain a superblock: %w", utils.ErrCorrupt)
	}

	if string(img[:8]) != Signature {
		return nil, fmt.Errorf("invalid HDF5 signature: %w", utils.ErrCorrupt)
	}

	version := img[8]
	// Version 1 (non-default B-tree K values) is valid HDF5 (H5Fprivate.h:301)
	// that this reader does not decode; above 3 C refuses the superblock
	// (H5Fsuper_cache.c:164-165).
	if version != Version0 && version != Version2 && version != Version3 {
		sentinel := utils.ErrCorrupt
		if version == 1 {
			sentinel = utils.ErrNotSupported
		}
		return nil, fmt.Errorf("unsupported superblock version: %d (only 0, 2, 3 supported): %w", version, sentinel)
	}

	offsetSize, lengthSize, err := parseSuperblockSizes(version, img)
	if err != nil {
		return nil, err
	}

	// The image must hold every field its own sizes call for.
	need := minimumSuperblockSize(version, offsetSize, lengthSize)
	if uint64(len(img)) < need {
		return nil, fmt.Errorf("file too small to contain a superblock: %d of %d bytes: %w",
			len(img), need, utils.ErrCorrupt)
	}
	img = img[:need]

	sb := &Superblock{
		Version:    version,
		OffsetSize: offsetSize,
		LengthSize: lengthSize,
		// HDF5 metadata is little-endian whatever the platform; the format
		// has no byte-order field (H5Fsuper_cache.c decodes with UINT*DECODE).
		Endianness:     binary.LittleEndian,
		BaseAddress:    addr,
		SymLeafK:       DefaultSymLeafK,
		SymInternalK:   DefaultSymInternalK,
		ChunkInternalK: DefaultChunkInternalK,
	}

	if version == Version0 {
		err = sb.decodeV0(img, size, sizeKnown)
	} else {
		err = sb.decodeV2(img, size, sizeKnown)
	}
	if err != nil {
		return nil, err
	}

	if err := validateEOFAddress(version, offsetSize, lengthSize, addr, sb.EOFAddress, size, sizeKnown); err != nil {
		return nil, err
	}

	if version >= Version2 && sb.SuperExtension != undefinedAddress {
		// The extension address, like every address in the file, is relative
		// to the base address (H5FD_read adds file->base_addr, H5FDint.c:267); a
		// file with a user block has its superblock, and base, past offset 0.
		er := r
		if addr > 0 {
			er = utils.NewBaseReader(r, addr)
		}
		if err := sb.readBTreeK(er); err != nil {
			return nil, err
		}
	}

	return sb, nil
}

// The C library's default B-tree K values (H5Fprivate.h:200, :314-317).
const (
	DefaultSymLeafK       = 4  // H5F_CRT_SYM_LEAF_DEF
	DefaultSymInternalK   = 16 // HDF5_BTREE_SNODE_IK_DEF
	DefaultChunkInternalK = 32 // HDF5_BTREE_CHUNK_IK_DEF
)

// btreeKMessageSize is the B-tree 'K' values message: version(1)=0, chunk
// internal K(2), symbol table internal K(2), symbol table leaf K(2)
// (H5O__btreek_decode, H5Obtreek.c:80-103).
const btreeKMessageSize = 7

// readBTreeK reads the B-tree 'K' values message of the superblock extension,
// when it has one, into sb (H5F__super_read, H5Fsuper.c:699-715). Deviation,
// stricter: a K of 0 is corrupt, as for the version 0 superblock's group K
// values (H5Fsuper_cache.c:476-486); C does not check the message's values, and
// a B-tree with K 0 holds no entries.
func (sb *Superblock) readBTreeK(r io.ReaderAt) error {
	ext := sb.SuperExtension
	header, err := readObjectHeader(r, ext, sb, false)
	if err != nil {
		return fmt.Errorf("superblock extension at 0x%X: %w", ext, err)
	}
	for _, msg := range header.Messages {
		if msg.Type != MsgBTreeK {
			continue
		}
		d := msg.Data
		if len(d) < btreeKMessageSize {
			return fmt.Errorf("superblock extension at 0x%X: B-tree 'K' values message of %d bytes: %w",
				ext, len(d), utils.ErrCorrupt)
		}
		if d[0] != 0 {
			return fmt.Errorf("superblock extension at 0x%X: B-tree 'K' values message version %d: %w",
				ext, d[0], utils.ErrCorrupt)
		}
		chunkK := binary.LittleEndian.Uint16(d[1:3])
		snodeK := binary.LittleEndian.Uint16(d[3:5])
		leafK := binary.LittleEndian.Uint16(d[5:7])
		if chunkK == 0 || snodeK == 0 || leafK == 0 {
			return fmt.Errorf("superblock extension at 0x%X: B-tree 'K' values %d/%d/%d include 0: %w",
				ext, chunkK, snodeK, leafK, utils.ErrCorrupt)
		}
		sb.ChunkInternalK, sb.SymInternalK, sb.SymLeafK = chunkK, snodeK, leafK

		return nil
	}

	return nil
}

// decodeV0 decodes the variable part of a version 0 superblock
// (H5Fsuper_cache.c:425-546): hard-wired versions, B-tree K values, status
// flags, four addresses (base, free-space index, EOF, driver block) and the
// root group symbol table entry. img holds exactly the superblock.
func (sb *Superblock) decodeV0(img []byte, size uint64, sizeKnown bool) error {
	// Free-space, object directory and shared-header versions are hard-wired
	// to 0 (HDF5_*_VERSION, H5Fprivate.h:308-310; checked :435-453).
	switch {
	case img[9] != 0:
		return fmt.Errorf("bad free space version number %d: %w", img[9], utils.ErrCorrupt)
	case img[10] != 0:
		return fmt.Errorf("bad object directory version number %d: %w", img[10], utils.ErrCorrupt)
	case img[12] != 0:
		return fmt.Errorf("bad shared-header format version number %d: %w", img[12], utils.ErrCorrupt)
	}

	// Group leaf and internal node K must not be 0 (:476-486).
	sb.SymLeafK = binary.LittleEndian.Uint16(img[16:18])
	sb.SymInternalK = binary.LittleEndian.Uint16(img[18:20])
	if sb.SymLeafK == 0 {
		return fmt.Errorf("bad symbol table leaf node 1/2 rank: %w", utils.ErrCorrupt)
	}
	if sb.SymInternalK == 0 {
		return fmt.Errorf("bad 1/2 rank for btree internal nodes: %w", utils.ErrCorrupt)
	}

	// Status flags: a 32-bit field holding the 8-bit flags (:492-499).
	if flags := binary.LittleEndian.Uint32(img[20:24]); flags > 255 || flags&^superblockAllFlags != 0 {
		return fmt.Errorf("bad superblock status flags 0x%x: %w", flags, utils.ErrCorrupt)
	}

	a := int(sb.OffsetSize)
	p := superblockV0PrefixSize
	storedBase := sb.decodeAddr(img[p:])
	if err := validateStoredBaseAddress(storedBase, size, sizeKnown); err != nil {
		return err
	}
	p += 2 * a // base, then the unused free-space index address
	eof, err := movedSuperblockEOF(sb.decodeAddr(img[p:]), storedBase, sb.BaseAddress)
	if err != nil {
		return err
	}
	sb.EOFAddress = eof
	p += a
	sb.DriverInfo = sb.decodeAddr(img[p:])
	p += a

	return sb.decodeRootEntry(img[p:])
}

// decodeRootEntry decodes the root group symbol table entry of a version 0
// superblock as H5G_ent_decode does (H5Gent.c:108-170): a LENGTH-sized link
// name offset (:125, H5F_DECODE_LENGTH), the object header address, the cache
// type, 4 reserved bytes, and the scratch pad, which holds the B-tree and local
// heap addresses only for H5G_CACHED_STAB. An unknown cache type is an error.
func (sb *Superblock) decodeRootEntry(ent []byte) error {
	p := int(sb.LengthSize) // the root's link name offset is not used
	sb.RootGroup = sb.decodeAddr(ent[p:])
	p += int(sb.OffsetSize)
	cacheType := binary.LittleEndian.Uint32(ent[p:])
	p += 8 // cache type and reserved word

	switch cacheType {
	case symbolEntryCacheNothing, symbolEntryCacheSlink:
		// Nothing for the superblock to keep: the root group's symbol table
		// is then found through its object header.
	case symbolEntryCacheStab:
		sb.RootBTreeAddr = sb.decodeAddr(ent[p:])
		sb.RootHeapAddr = sb.decodeAddr(ent[p+int(sb.OffsetSize):])
	default:
		return fmt.Errorf("unknown root symbol table entry cache type %d: %w", cacheType, utils.ErrCorrupt)
	}

	return nil
}

// decodeV2 decodes the variable part of a version 2/3 superblock
// (H5Fsuper_cache.c:557-600) after verifying its checksum: status flags, then
// the base, superblock extension, EOF and root group object header addresses.
// img holds exactly the superblock, checksum last.
func (sb *Superblock) decodeV2(img []byte, size uint64, sizeKnown bool) error {
	// H5F__cache_superblock_verify_chksum via H5F_get_checksums
	// (H5Fio.c:499-530): the stored checksum is the last 4 bytes, the computed
	// one covers everything before them. hdf5-go up to v0.13.4 stored
	// CRC32-IEEE here (VerifyMetadataChecksumLegacy); such a file stays
	// readable, with a warning, although the C library refuses it.
	body := len(img) - metadataChecksumSize
	stored := binary.LittleEndian.Uint32(img[body:])
	legacy, err := VerifyMetadataChecksumLegacy(img[:body], stored, "superblock")
	if err != nil {
		return err
	}
	if legacy {
		// The span is relative to the superblock's own position, which is
		// address 0 unless the file has a user block.
		sb.warnLegacyChecksumf(WarnLegacySuperblockChecksum, 0, uint64(body), //nolint:gosec // body is a small constant
			"superblock checksum is CRC32-IEEE, as hdf5-go v0.13.4 and earlier wrote it; "+
				"the HDF5 C library cannot open this file until it is rewritten (open it with OpenForWrite and close it)")
	}

	if flags := img[11]; flags&^superblockAllFlags != 0 {
		return fmt.Errorf("bad flag value 0x%x for superblock: %w", flags, utils.ErrCorrupt)
	}

	a := int(sb.OffsetSize)
	p := superblockV2PrefixSize
	storedBase := sb.decodeAddr(img[p:])
	if err := validateStoredBaseAddress(storedBase, size, sizeKnown); err != nil {
		return err
	}
	p += a
	sb.SuperExtension = sb.decodeAddr(img[p:])
	p += a
	if sb.EOFAddress, err = movedSuperblockEOF(sb.decodeAddr(img[p:]), storedBase, sb.BaseAddress); err != nil {
		return err
	}
	p += a
	sb.RootGroup = sb.decodeAddr(img[p:])

	return nil
}

// decodeAddr decodes one address field of the superblock: an all-ones value
// of the file's offset width is the undefined address (H5F_addr_decode,
// H5Fint.c:3046-3050).
func (sb *Superblock) decodeAddr(b []byte) uint64 {
	return DecodeAddress(b, int(sb.OffsetSize), sb.Endianness)
}

// parseSuperblockSizes returns sizeof_addr and sizeof_size from the superblock
// prefix as H5F__superblock_prefix_decode does (H5Fsuper_cache.c:173-191):
// version 0/1 store them at bytes 13 and 14, version 2/3 at bytes 9 and 10
// (byte 11 is the status flags). C accepts 2, 4, 8, 16 and 32 and nothing
// else; this reader decodes addresses and lengths into uint64, so 16 and 32
// are reported as not supported.
func parseSuperblockSizes(version uint8, img []byte) (offsetSize, lengthSize uint8, err error) {
	at := superblockFixedSize
	if version < Version2 {
		at += 4 // free-space, object directory, reserved, shared-header bytes
	}
	if len(img) < at+2 {
		return 0, 0, fmt.Errorf("file too small to contain a superblock: %w", utils.ErrCorrupt)
	}
	offsetSize, lengthSize = img[at], img[at+1]

	for _, f := range []struct {
		what string
		v    uint8
	}{{"an address", offsetSize}, {"object size", lengthSize}} {
		switch f.v {
		case 2, 4, 8:
		case 16, 32:
			return 0, 0, fmt.Errorf("superblock byte number for %s %d: %w", f.what, f.v, utils.ErrNotSupported)
		default:
			return 0, 0, fmt.Errorf("bad byte number for %s in superblock version %d: %d: %w",
				f.what, version, f.v, utils.ErrCorrupt)
		}
	}

	return offsetSize, lengthSize, nil
}

// validateEOFAddress rejects a non-zero EOF address smaller than the superblock
// header itself: the file's address space cannot end inside the superblock that
// defines it, so such a value means truncation or corruption (cf. the stored_eof
// check in H5F__super_read, H5Fsuper.c:573).
func validateEOFAddress(version, offsetSize, lengthSize uint8, superAddr, eofAddress, size uint64, sizeKnown bool) error {
	minSize := minimumSuperblockSize(version, offsetSize, lengthSize)
	headerEnd := superAddr + minSize
	if eofAddress != 0 && eofAddress < headerEnd {
		return fmt.Errorf("invalid superblock: EOF address %d is smaller than the superblock header (%d bytes)",
			eofAddress, headerEnd)
	}

	// The stored EOF address is absolute (C: H5Fsuper.c:573 compares the
	// physical end of file plus the base against it), so it cannot be past the
	// bytes the file actually has.
	if sizeKnown && eofAddress > size {
		return fmt.Errorf("truncated file: end-of-file address %d is beyond the file size %d",
			eofAddress, size)
	}

	return nil
}

// movedSuperblockEOF returns the stored end-of-file address adjusted for a
// superblock found at superAddr while the file stores base address
// storedBase, as H5F__super_read does (H5Fsuper.c:475-482): a tool such as
// h5jam or h5unjam changed the user block in front of it, the data moved with
// the superblock, and the absolute EOF moves by the same amount. The base
// address becomes superAddr (Superblock.BaseAddress). C's address arithmetic
// wraps, which its truncation check then reports; an adjusted EOF outside the
// address range is reported as corrupt here.
func movedSuperblockEOF(eof, storedBase, superAddr uint64) (uint64, error) {
	switch {
	case storedBase == superAddr:
		return eof, nil
	case storedBase > superAddr:
		if d := storedBase - superAddr; eof >= d {
			return eof - d, nil
		}
	default:
		if d := superAddr - storedBase; eof <= undefinedAddress-d {
			return eof + d, nil
		}
	}
	return 0, fmt.Errorf("truncated file: end-of-file address %d does not fit the superblock moved from base %d to %d: %w",
		eof, storedBase, superAddr, utils.ErrCorrupt)
}

// validateStoredBaseAddress rejects a base address stored in the file that
// points past the end of the file: it cannot describe a user block.
func validateStoredBaseAddress(stored, size uint64, sizeKnown bool) error {
	if sizeKnown && stored > size {
		return fmt.Errorf("invalid superblock: base address %d is beyond the file size %d", stored, size)
	}

	return nil
}

// WriteTo writes the superblock to the writer at offset 0.
// For MVP (v0.11.0-beta), only superblock v2 is supported for writing.
//
// Superblock v2 format (48 bytes):
//
//	Bytes 0-7:   Signature (\x89HDF\r\n\x1a\n)
//	Byte 8:      Version (2)
//	Byte 9:      Size of Offsets (8 bytes)
//	Byte 10:     Size of Lengths (8 bytes)
//	Byte 11:     File Consistency Flags (0)
//	Bytes 12-19: Base Address (typically 0)
//	Bytes 20-27: Superblock Extension Address (UNDEF if none)
//	Bytes 28-35: End-of-File Address (file size)
//	Bytes 36-43: Root Group Object Header Address
//	Bytes 44-47: Superblock Checksum (CRC32)
//
// Parameters:
//   - w: Writer (typically a FileWriter)
//   - eofAddress: Current end-of-file address
//
// Returns error if write fails or if superblock version is not supported.
func (sb *Superblock) WriteTo(w io.WriterAt, eofAddress uint64) error {
	// Support v0 (legacy), v2 (modern), and v3 (modern with file locking)
	// Note: v2 and v3 use the same structure (v3 just has different flags in byte 11)
	if sb.Version != Version0 && sb.Version != Version2 && sb.Version != Version3 {
		return fmt.Errorf("only superblock version 0, 2, and 3 are supported for writing, got version %d", sb.Version)
	}

	// Dispatch to version-specific writer
	switch sb.Version {
	case Version0:
		return sb.writeV0(w, eofAddress)
	case Version2, Version3:
		// v2 and v3 use the same structure, writeV2 handles both
		return sb.writeV2(w, eofAddress)
	default:
		return fmt.Errorf("unsupported superblock version: %d", sb.Version)
	}
}

// writeV2 writes superblock version 2 (modern format with checksums).
func (sb *Superblock) writeV2(w io.WriterAt, eofAddress uint64) error {
	// Validate required fields
	if sb.OffsetSize != 8 || sb.LengthSize != 8 {
		return fmt.Errorf("only 8-byte offsets and lengths are supported for writing, got offset=%d, length=%d",
			sb.OffsetSize, sb.LengthSize)
	}

	// Allocate buffer for superblock v2/v3 (48 bytes)
	buf := make([]byte, 48)

	// Bytes 0-7: Signature
	copy(buf[0:8], Signature)

	// Byte 8: Version (2 or 3)
	buf[8] = sb.Version

	// Byte 9: Size of offsets (8 bytes)
	buf[9] = 8

	// Byte 10: Size of lengths (8 bytes)
	buf[10] = 8

	// Byte 11: File consistency flags (0 for now)
	buf[11] = 0

	// Bytes 12-19: Base address (typically 0)
	binary.LittleEndian.PutUint64(buf[12:20], sb.BaseAddress)

	// Bytes 20-27: Superblock extension address (UNDEF if none)
	// UNDEF is represented as 0xFFFFFFFFFFFFFFFF
	superExt := sb.SuperExtension
	if superExt == 0 {
		superExt = 0xFFFFFFFFFFFFFFFF // UNDEF
	}
	binary.LittleEndian.PutUint64(buf[20:28], superExt)

	// Bytes 28-35: End-of-file address
	binary.LittleEndian.PutUint64(buf[28:36], eofAddress)

	// Bytes 36-43: Root group object header address
	binary.LittleEndian.PutUint64(buf[36:44], sb.RootGroup)

	// Bytes 44-47: Superblock checksum (Jenkins lookup3 of bytes 0-43)
	checksum := JenkinsChecksum(buf[0:44])
	binary.LittleEndian.PutUint32(buf[44:48], checksum)

	// Write superblock at offset 0
	n, err := w.WriteAt(buf, 0)
	if err != nil {
		return fmt.Errorf("failed to write superblock: %w", err)
	}

	if n != 48 {
		return fmt.Errorf("incomplete superblock write: wrote %d bytes, expected 48", n)
	}

	return nil
}

// writeV0 writes superblock version 0 (legacy format for maximum compatibility).
// This format is used by older HDF5 tools and is the most widely supported.
//
// Superblock v0 structure (96 bytes minimum):
//
//	Bytes 0-7: Format Signature (\211HDF\r\n\032\n)
//	Byte 8: Superblock Version (0)
//	Byte 9: Free-space Storage Version (0)
//	Byte 10: Root Group Symbol Table Entry Version (0)
//	Byte 11: Reserved (0)
//	Byte 12: Shared Header Message Format Version (0)
//	Byte 13: Size of Offsets (8)
//	Byte 14: Size of Lengths (8)
//	Byte 15: Reserved (0)
//	Bytes 16-17: Group Leaf Node K (4)
//	Bytes 18-19: Group Internal Node K (16)
//	Bytes 20-23: File Consistency Flags (0)
//	Bytes 24-31: Base Address (0)
//	Bytes 32-39: Free Space Info Address (UNDEF)
//	Bytes 40-47: End of File Address
//	Bytes 48-55: Driver Info Block Address (UNDEF)
//	Bytes 56-95: Root Group Symbol Table Entry (40 bytes)
func (sb *Superblock) writeV0(w io.WriterAt, eofAddress uint64) error {
	// Validate required fields
	if sb.OffsetSize != 8 || sb.LengthSize != 8 {
		return fmt.Errorf("only 8-byte offsets and lengths are supported for writing, got offset=%d, length=%d",
			sb.OffsetSize, sb.LengthSize)
	}

	// Allocate buffer for superblock v0 (96 bytes)
	buf := make([]byte, 96)

	// Bytes 0-7: Signature
	copy(buf[0:8], Signature)

	// Byte 8: Version 0
	buf[8] = 0

	// Byte 9: Free-space Storage Version (0)
	buf[9] = 0

	// Byte 10: Root Group Symbol Table Entry Version (0)
	buf[10] = 0

	// Byte 11: Reserved
	buf[11] = 0

	// Byte 12: Shared Header Message Format Version (0)
	buf[12] = 0

	// Byte 13: Size of offsets (8 bytes)
	buf[13] = 8

	// Byte 14: Size of lengths (8 bytes)
	buf[14] = 8

	// Byte 15: Reserved
	buf[15] = 0

	// Bytes 16-17 and 18-19: Group Leaf Node K and Group Internal Node K: the
	// file's own values, so a rewrite keeps the node sizes its symbol tables
	// were written with; the C defaults for a new file (H5Fsuper_cache.c
	// H5F__cache_superblock_serialize writes sym_leaf_k and btree_k[SNODE]).
	leafK, internalK := sb.SymLeafK, sb.SymInternalK
	if leafK == 0 {
		leafK = DefaultSymLeafK
	}
	if internalK == 0 {
		internalK = DefaultSymInternalK
	}
	binary.LittleEndian.PutUint16(buf[16:18], leafK)
	binary.LittleEndian.PutUint16(buf[18:20], internalK)

	// Bytes 20-23: File Consistency Flags (0 = file is closed properly)
	binary.LittleEndian.PutUint32(buf[20:24], 0)

	// Bytes 24-31: Base address (typically 0)
	binary.LittleEndian.PutUint64(buf[24:32], sb.BaseAddress)

	// Bytes 32-39: Free Space Info Address (UNDEF for now)
	binary.LittleEndian.PutUint64(buf[32:40], 0xFFFFFFFFFFFFFFFF)

	// Bytes 40-47: End-of-file address
	binary.LittleEndian.PutUint64(buf[40:48], eofAddress)

	// Bytes 48-55: Driver Info Block Address (UNDEF)
	binary.LittleEndian.PutUint64(buf[48:56], 0xFFFFFFFFFFFFFFFF)

	// Bytes 56-95: Root Group Symbol Table Entry (40 bytes)
	// This is a Symbol Table Entry with cached B-tree/Heap addresses
	//
	// Symbol Table Entry structure (from C library H5Gent.c):
	//   Bytes 0-7: Link Name Offset (in local heap) - 0 for root
	//   Bytes 8-15: Object Header Address
	//   Bytes 16-19: Cache Type (1 = H5G_CACHED_STAB for symbol table)
	//   Bytes 20-23: Reserved
	//   Bytes 24-39: Scratch-pad space (16 bytes):
	//     - Bytes 24-31: B-tree address (for H5G_CACHED_STAB)
	//     - Bytes 32-39: Local heap address (for H5G_CACHED_STAB)

	// Link Name Offset (0 for root group)
	binary.LittleEndian.PutUint64(buf[56:64], 0)

	// Object Header Address (root group address)
	binary.LittleEndian.PutUint64(buf[64:72], sb.RootGroup)

	// Cache Type (1 = H5G_CACHED_STAB, meaning symbol table with cached addresses)
	binary.LittleEndian.PutUint32(buf[72:76], 1)

	// Reserved
	binary.LittleEndian.PutUint32(buf[76:80], 0)

	// Scratch-pad space (16 bytes): Cached B-tree and Heap addresses
	// This is CRITICAL for v0 - h5dump needs these to find the root group!
	binary.LittleEndian.PutUint64(buf[80:88], sb.RootBTreeAddr) // B-tree address
	binary.LittleEndian.PutUint64(buf[88:96], sb.RootHeapAddr)  // Heap address

	// Write superblock at offset 0
	n, err := w.WriteAt(buf, 0)
	if err != nil {
		return fmt.Errorf("failed to write superblock v0: %w", err)
	}

	if n != 96 {
		return fmt.Errorf("incomplete superblock v0 write: wrote %d bytes, expected 96", n)
	}

	return nil
}
