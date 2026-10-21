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

	// V0-specific: Cached symbol table info for root group
	// These are only used when Version == 0
	RootBTreeAddr uint64 // B-tree address for root group (v0 only)
	RootHeapAddr  uint64 // Local heap address for root group (v0 only)
}

// minimumSuperblockSize returns the minimum on-disk size in bytes of the
// superblock header for the given version.
func minimumSuperblockSize(version uint8) uint64 {
	if version == Version0 {
		return minSuperblockSizeV0
	}
	return minSuperblockSizeV2
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
func readSuperblockAt(r io.ReaderAt, addr, size uint64, sizeKnown bool) (*Superblock, error) {
	buf := utils.GetBuffer(128)
	defer utils.ReleaseBuffer(buf)

	//nolint:gosec // G115: addr is a file offset located within the file
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, utils.WrapError("superblock read failed", err)
	}
	if n < 48 {
		return nil, errors.New("file too small to contain a superblock")
	}

	if string(buf[:8]) != Signature {
		return nil, errors.New("invalid HDF5 signature")
	}

	version := buf[8]
	if version != Version0 && version != Version2 && version != Version3 {
		return nil, fmt.Errorf("unsupported superblock version: %d (only 0, 2, 3 supported)", version)
	}

	endianness, offsetSize, lengthSize, err := parseSuperblockSizes(version, buf)
	if err != nil {
		return nil, err
	}

	// Helper function to read variable-sized values
	readValue := func(offset int, size uint8) (uint64, error) {
		if offset < 0 || offset+int(size) > len(buf) {
			return 0, fmt.Errorf("buffer overflow: offset=%d, size=%d", offset, size)
		}

		data := buf[offset : offset+int(size)]
		switch size {
		case 1:
			return uint64(data[0]), nil
		case 2:
			return uint64(endianness.Uint16(data)), nil
		case 4:
			return uint64(endianness.Uint32(data)), nil
		case 8:
			return endianness.Uint64(data), nil
		default:
			return 0, fmt.Errorf("unsupported size: %d", size)
		}
	}

	sb := &Superblock{
		Version:     version,
		OffsetSize:  offsetSize,
		LengthSize:  lengthSize,
		Endianness:  endianness,
		BaseAddress: addr,
	}

	// The base address stored in the file is parsed for both layouts and
	// validated, but the effective base is the superblock position: the C
	// library rewrites a disagreeing value the same way (H5Fsuper.c:475-485).
	if version == Version0 {
		storedBase, baseErr := readValue(24, offsetSize)
		if baseErr != nil {
			return nil, utils.WrapError("base address read failed", baseErr)
		}
		if err := validateStoredBaseAddress(storedBase, size, sizeKnown); err != nil {
			return nil, err
		}
		// Version 0 superblock structure:
		// Offset 24-31: Base address
		// Offset 32-39: Free space index
		// Offset 40-47: End-of-File address (NOT root group!)
		// Offset 48-55: Driver info block
		// Offset 56-95: Root group symbol table entry (40 bytes total):
		//   56-63: Link name offset (8 bytes)
		//   64-71: Object header address (8 bytes)
		//   72-75: Cache type (4 bytes) - 1 = H5G_CACHED_STAB
		//   76-79: Reserved (4 bytes)
		//   80-87: B-tree address (8 bytes) - for cached symbol table
		//   88-95: Local heap address (8 bytes) - for cached symbol table

		// End-of-file address at offset 40 (C reference H5Fsuper_cache.c:528
		// decodes base address, superblock extension, EOF, then driver info).
		sb.EOFAddress, err = readValue(40, offsetSize)
		if err != nil {
			return nil, utils.WrapError("end-of-file address read failed", err)
		}

		// Read object header address at offset 64
		sb.RootGroup, err = readValue(64, offsetSize)
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}

		// ALWAYS read cached B-tree and Heap addresses for v0 files
		// These are stored in the scratch-pad area when cache type = 1 (H5G_CACHED_STAB)
		// Even if object header address is non-zero, the symbol table may use these
		sb.RootBTreeAddr, err = readValue(80, offsetSize)
		if err != nil {
			return nil, utils.WrapError("b-tree address read failed", err)
		}

		sb.RootHeapAddr, err = readValue(88, offsetSize)
		if err != nil {
			return nil, utils.WrapError("heap address read failed", err)
		}
	} else {
		// For v2 and v3, fields start at byte 12
		current := 12

		storedBase, baseErr := readValue(current, offsetSize)
		if baseErr != nil {
			return nil, utils.WrapError("base address read failed", baseErr)
		}
		if err := validateStoredBaseAddress(storedBase, size, sizeKnown); err != nil {
			return nil, err
		}
		current += int(offsetSize)

		sb.SuperExtension, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("super extension read failed", err)
		}
		current += int(offsetSize)

		// End-of-file address (C reference H5Fsuper_cache.c:584 decodes base
		// address, superblock extension, EOF, then root group object header).
		sb.EOFAddress, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("end-of-file address read failed", err)
		}
		current += int(offsetSize)

		sb.RootGroup, err = readValue(current, offsetSize)
		if err != nil {
			return nil, utils.WrapError("root group address read failed", err)
		}
		// Note: v2 and v3 have checksum at bytes 44-47, but we don't validate it during read
		// The HDF5 C library also doesn't enforce checksum validation on read
	}

	if err := validateEOFAddress(version, addr, sb.EOFAddress, size, sizeKnown); err != nil {
		return nil, err
	}

	return sb, nil
}

// parseSuperblockSizes returns the byte order and field widths of the
// superblock: v0 stores them at bytes 13-14, while v2/v3 select the byte order
// in byte 9 and either a direct size or a pair of packed codes in byte 10.
func parseSuperblockSizes(version uint8, buf []byte) (binary.ByteOrder, uint8, uint8, error) {
	var endianness binary.ByteOrder
	var offsetSize, lengthSize uint8

	if version == Version0 {
		offsetSize = buf[13]
		lengthSize = buf[14]
		endianness = binary.LittleEndian // v0 files are little-endian only here
	} else {
		// Byte 9: flags — bit 0 selects the byte order.
		if buf[9]&0x01 == 0 {
			endianness = binary.LittleEndian
		} else {
			endianness = binary.BigEndian
		}

		// Byte 10 is either a direct size (1, 2, 4, 8) or packed codes:
		// lower 4 bits = offset size, upper 4 bits = length size.
		sizesByte := buf[10]
		validDirectSizes := map[uint8]bool{1: true, 2: true, 4: true, 8: true}
		if validDirectSizes[sizesByte] {
			offsetSize = sizesByte
			lengthSize = 8 // HDF5 v2/v3 typically uses 8-byte lengths
		} else {
			sizeCodeMap := map[uint8]uint8{0: 1, 1: 2, 2: 4, 3: 8}

			var ok bool
			offsetSize, ok = sizeCodeMap[sizesByte&0x0F]
			if !ok {
				return nil, 0, 0, fmt.Errorf("invalid offset size code: %d", sizesByte&0x0F)
			}
			lengthSize, ok = sizeCodeMap[(sizesByte>>4)&0x0F]
			if !ok {
				return nil, 0, 0, fmt.Errorf("invalid length size code: %d", (sizesByte>>4)&0x0F)
			}
		}
	}

	// Handle zero sizes (in test files).
	if offsetSize == 0 {
		offsetSize = 8
	}
	if lengthSize == 0 {
		lengthSize = 8
	}

	validSizes := map[uint8]bool{1: true, 2: true, 4: true, 8: true}
	if !validSizes[offsetSize] || !validSizes[lengthSize] {
		return nil, 0, 0, fmt.Errorf("invalid sizes for version %d: offset=%d, length=%d",
			version, offsetSize, lengthSize)
	}

	return endianness, offsetSize, lengthSize, nil
}

// validateEOFAddress rejects a non-zero EOF address smaller than the superblock
// header itself: the file's address space cannot end inside the superblock that
// defines it, so such a value means truncation or corruption (cf. the stored_eof
// check in H5F__super_read, H5Fsuper.c:573).
func validateEOFAddress(version uint8, superAddr, eofAddress, size uint64, sizeKnown bool) error {
	minSize := minimumSuperblockSize(version)
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

	// Bytes 16-17: Group Leaf Node K (default: 4)
	binary.LittleEndian.PutUint16(buf[16:18], 4)

	// Bytes 18-19: Group Internal Node K (default: 16)
	binary.LittleEndian.PutUint16(buf[18:20], 16)

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
