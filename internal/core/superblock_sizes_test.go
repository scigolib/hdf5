package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// The tests in this file pin the superblock layout to the C library
// (H5Fsuper_cache.c): the v2/v3 prefix is version, sizeof_addr, sizeof_size,
// status flags (:176-191, :560-573); the v2/v3 checksum is verified
// (H5F__cache_superblock_verify_chksum, :365-391); the root symbol table entry
// of a v0 superblock starts with a LENGTH-sized link name offset (H5Gent.c:125).

// putField stores v little-endian in a field of the given width.
func putField(b []byte, v uint64, width int) {
	for i := 0; i < width; i++ {
		b[i] = byte(v >> (8 * i))
	}
}

// sbV2Fields are the variable fields of a version 2/3 superblock.
type sbV2Fields struct {
	version    uint8
	addr, size uint8
	flags      uint8
	base, ext  uint64
	eof, root  uint64
}

// buildSuperblockV2 encodes a v2/v3 superblock as H5F__cache_superblock_serialize
// does (H5Fsuper_cache.c:731-760), checksum included, into a file of fileSize
// bytes.
func buildSuperblockV2(f sbV2Fields, fileSize int) []byte {
	a := int(f.addr)
	file := make([]byte, fileSize)
	copy(file, Signature)
	file[8] = f.version
	file[9] = f.addr
	file[10] = f.size
	file[11] = f.flags
	p := 12
	for _, v := range []uint64{f.base, f.ext, f.eof, f.root} {
		putField(file[p:], v, a)
		p += a
	}
	binary.LittleEndian.PutUint32(file[p:], JenkinsChecksum(file[:p]))
	return file
}

// sbV0Fields are the variable fields of a version 0 superblock and its root
// group symbol table entry.
type sbV0Fields struct {
	addr, size     uint8
	freeSpaceVers  uint8
	objDirVers     uint8
	sharedHdrVers  uint8
	leafK, intK    uint16
	flags          uint32
	eof, root      uint64
	nameOff        uint64
	cacheType      uint32
	btree, heap    uint64
	scratchGarbage bool
}

func defaultV0(addr, size uint8) sbV0Fields {
	return sbV0Fields{
		addr: addr, size: size, leafK: 4, intK: 16, eof: 512,
		root: 0x200, cacheType: 1, btree: 0x300, heap: 0x400,
	}
}

// buildSuperblockV0 encodes a v0 superblock as H5F__cache_superblock_serialize
// does (H5Fsuper_cache.c:676-722) with the root entry of H5G_ent_encode.
func buildSuperblockV0(f sbV0Fields, fileSize int) []byte {
	a, s := int(f.addr), int(f.size)
	file := make([]byte, fileSize)
	copy(file, Signature)
	file[8] = 0
	file[9] = f.freeSpaceVers
	file[10] = f.objDirVers
	file[12] = f.sharedHdrVers
	file[13] = f.addr
	file[14] = f.size
	binary.LittleEndian.PutUint16(file[16:], f.leafK)
	binary.LittleEndian.PutUint16(file[18:], f.intK)
	binary.LittleEndian.PutUint32(file[20:], f.flags)
	p := 24
	undef := ^uint64(0)
	for _, v := range []uint64{0, undef, f.eof, undef} {
		putField(file[p:], v, a)
		p += a
	}
	// Root entry: name offset (length), header (address), cache type, reserved,
	// 16-byte scratch pad.
	putField(file[p:], f.nameOff, s)
	p += s
	putField(file[p:], f.root, a)
	p += a
	binary.LittleEndian.PutUint32(file[p:], f.cacheType)
	p += 8
	switch {
	case f.scratchGarbage:
		for i := 0; i < 16; i++ {
			file[p+i] = 0xA5
		}
	case f.cacheType == 1:
		putField(file[p:], f.btree, a)
		putField(file[p+a:], f.heap, a)
	}
	return file
}

func TestSuperblockV2_SizesFromPrefix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    uint8
		addr, size uint8
	}{
		{"v2 addr4 size8", Version2, 4, 8},
		{"v2 addr8 size4", Version2, 8, 4},
		{"v3 addr4 size4", Version3, 4, 4},
		{"v3 addr2 size8", Version3, 2, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := buildSuperblockV2(sbV2Fields{
				version: tc.version, addr: tc.addr, size: tc.size,
				ext: ^uint64(0), eof: 512, root: 0x30,
			}, 512)
			sb, err := ReadSuperblock(bytes.NewReader(file))
			require.NoError(t, err)
			require.Equal(t, tc.addr, sb.OffsetSize)
			require.Equal(t, tc.size, sb.LengthSize)
			require.Equal(t, binary.LittleEndian, sb.Endianness)
			require.Equal(t, uint64(512), sb.EOFAddress)
			require.Equal(t, uint64(0x30), sb.RootGroup)
			require.Equal(t, ^uint64(0), sb.SuperExtension, "all ones of any width is undefined")
		})
	}
}

// TestSuperblockV2_OddByte9IsNotByteOrder: byte 9 is sizeof_addr, not a
// byte-order flag; HDF5 metadata is always little-endian. An odd value is an
// invalid address size, which C rejects (H5Fsuper_cache.c:187-189).
func TestSuperblockV2_OddByte9IsNotByteOrder(t *testing.T) {
	file := buildSuperblockV2(sbV2Fields{version: Version2, addr: 8, size: 8, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
	file[9] = 9
	binary.LittleEndian.PutUint32(file[44:], JenkinsChecksum(file[:44]))
	_, err := ReadSuperblock(bytes.NewReader(file))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

func TestSuperblockV2_ChecksumMismatch(t *testing.T) {
	for _, v := range []uint8{Version2, Version3} {
		file := buildSuperblockV2(sbV2Fields{version: v, addr: 8, size: 8, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
		file[44] ^= 0x01
		_, err := ReadSuperblock(bytes.NewReader(file))
		require.ErrorIs(t, err, utils.ErrCorrupt, "version %d", v)

		// A flipped field byte is caught too: the checksum covers the whole
		// superblock up to the checksum (H5F_get_checksums, H5Fio.c:518-526).
		file = buildSuperblockV2(sbV2Fields{version: v, addr: 8, size: 8, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
		file[36] ^= 0x10
		_, err = ReadSuperblock(bytes.NewReader(file))
		require.ErrorIs(t, err, utils.ErrCorrupt, "version %d", v)
	}
}

func TestSuperblockV2_BadStatusFlags(t *testing.T) {
	file := buildSuperblockV2(sbV2Fields{version: Version3, addr: 8, size: 8, flags: 0x08, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
	_, err := ReadSuperblock(bytes.NewReader(file))
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// Every defined flag is accepted (H5Fpkg.h:50-53).
	file = buildSuperblockV2(sbV2Fields{version: Version3, addr: 8, size: 8, flags: 0x07, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
	_, err = ReadSuperblock(bytes.NewReader(file))
	require.NoError(t, err)
}

// TestSuperblock_InvalidFieldSizes: C accepts sizes 2, 4, 8, 16 and 32 only
// (H5Fsuper_cache.c:187-191) and has no fallback for zero.
func TestSuperblock_InvalidFieldSizes(t *testing.T) {
	for _, tc := range []struct {
		addr, size uint8
		want       error
	}{
		{0, 8, utils.ErrCorrupt},
		{8, 0, utils.ErrCorrupt},
		{1, 8, utils.ErrCorrupt},
		{8, 3, utils.ErrCorrupt},
		{16, 8, utils.ErrNotSupported},
		{8, 32, utils.ErrNotSupported},
	} {
		v2 := buildSuperblockV2(sbV2Fields{version: Version2, addr: 8, size: 8, ext: ^uint64(0), eof: 512, root: 0x30}, 512)
		v2[9], v2[10] = tc.addr, tc.size
		binary.LittleEndian.PutUint32(v2[44:], JenkinsChecksum(v2[:44]))
		_, err := ReadSuperblock(bytes.NewReader(v2))
		require.ErrorIs(t, err, tc.want, "v2 addr=%d size=%d", tc.addr, tc.size)

		v0 := buildSuperblockV0(defaultV0(8, 8), 512)
		v0[13], v0[14] = tc.addr, tc.size
		_, err = ReadSuperblock(bytes.NewReader(v0))
		require.ErrorIs(t, err, tc.want, "v0 addr=%d size=%d", tc.addr, tc.size)
	}
}

// TestSuperblockV0_RootEntryLengthSizedNameOffset: the root entry's link
// name offset is sizeof_size wide (H5Gent.c:125, H5Gprivate.h:38-43).
func TestSuperblockV0_RootEntryLengthSizedNameOffset(t *testing.T) {
	for _, tc := range []struct{ addr, size uint8 }{{8, 4}, {4, 8}, {4, 4}, {8, 8}, {2, 8}} {
		f := defaultV0(tc.addr, tc.size)
		file := buildSuperblockV0(f, 512)
		sb, err := ReadSuperblock(bytes.NewReader(file))
		require.NoError(t, err, "addr=%d size=%d", tc.addr, tc.size)
		require.Equal(t, tc.addr, sb.OffsetSize)
		require.Equal(t, tc.size, sb.LengthSize)
		require.Equal(t, uint64(512), sb.EOFAddress)
		require.Equal(t, f.root, sb.RootGroup, "addr=%d size=%d", tc.addr, tc.size)
		require.Equal(t, f.btree, sb.RootBTreeAddr, "addr=%d size=%d", tc.addr, tc.size)
		require.Equal(t, f.heap, sb.RootHeapAddr, "addr=%d size=%d", tc.addr, tc.size)
	}
}

// TestSuperblockV0_RootEntryCacheType: the scratch pad holds the cached
// symbol table only for H5G_CACHED_STAB; an unknown cache type is an error
// (H5Gent.c:139-165).
func TestSuperblockV0_RootEntryCacheType(t *testing.T) {
	f := defaultV0(8, 8)
	f.cacheType = 0
	f.scratchGarbage = true
	sb, err := ReadSuperblock(bytes.NewReader(buildSuperblockV0(f, 512)))
	require.NoError(t, err)
	require.Zero(t, sb.RootBTreeAddr, "nothing cached: the scratch pad is not an address")
	require.Zero(t, sb.RootHeapAddr)

	f.cacheType = 3
	_, err = ReadSuperblock(bytes.NewReader(buildSuperblockV0(f, 512)))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestSuperblockV0_PrefixChecks mirrors the hard-wired version and range
// checks of H5F__cache_superblock_deserialize (H5Fsuper_cache.c:432-499).
func TestSuperblockV0_PrefixChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*sbV0Fields)
	}{
		{"free-space version", func(f *sbV0Fields) { f.freeSpaceVers = 1 }},
		{"object directory version", func(f *sbV0Fields) { f.objDirVers = 1 }},
		{"shared header version", func(f *sbV0Fields) { f.sharedHdrVers = 1 }},
		{"leaf K zero", func(f *sbV0Fields) { f.leafK = 0 }},
		{"internal K zero", func(f *sbV0Fields) { f.intK = 0 }},
		{"status flags over 255", func(f *sbV0Fields) { f.flags = 0x100 }},
		{"undefined status flag", func(f *sbV0Fields) { f.flags = 0x08 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := defaultV0(8, 8)
			tc.modify(&f)
			_, err := ReadSuperblock(bytes.NewReader(buildSuperblockV0(f, 512)))
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

// TestSuperblock_TruncatedForItsSizes: the superblock image must hold every
// field its own sizes call for.
func TestSuperblock_TruncatedForItsSizes(t *testing.T) {
	v2 := buildSuperblockV2(sbV2Fields{version: Version2, addr: 4, size: 4, ext: ^uint64(0), eof: 32, root: 0x30}, 32)
	sb, err := ReadSuperblock(bytes.NewReader(v2))
	require.NoError(t, err, "a 32-byte v2 superblock with 4-byte addresses is complete")
	require.Equal(t, uint8(4), sb.OffsetSize)

	_, err = ReadSuperblock(bytes.NewReader(v2[:31]))
	require.Error(t, err)
}
