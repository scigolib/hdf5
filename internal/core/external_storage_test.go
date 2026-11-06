package core

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

const (
	eflHeapAddr = 0x100 // Local heap header in eflTestFile.
	eflHeapData = 0x200 // Its data block.
	eflNameA    = 8     // Heap offset of "a.bin".
	eflNameB    = 16    // Heap offset of "b.bin".
)

// eflTestFile is a byte image holding a local heap whose data block names
// "a.bin" and "b.bin", laid out as H5HL writes it: an empty name at 0, names
// padded to 8 bytes (H5Dlayout.c:330-365 builds the EFL heap so).
func eflTestFile(t *testing.T) []byte {
	t.Helper()
	file := make([]byte, eflHeapData+32)
	copy(file[eflHeapAddr:], cat([]byte("HEAP\x00\x00\x00\x00"), le64(32), le64(localHeapFreeNull), le64(eflHeapData)))
	copy(file[eflHeapData+eflNameA:], "a.bin\x00")
	copy(file[eflHeapData+eflNameB:], "b.bin\x00")
	return file
}

// eflMessage encodes an External File List message (H5O__efl_encode,
// H5Oefl.c:236-270) with 8-byte lengths; each slot is name offset, file
// offset, size.
func eflMessage(version byte, nalloc, nused uint16, heap uint64, slots ...[3]uint64) []byte {
	msg := cat([]byte{version, 0, 0, 0}, le16s(nalloc, nused), le64(heap))
	for _, s := range slots {
		msg = cat(msg, le64(s[0]), le64(s[1]), le64(s[2]))
	}
	return msg
}

func TestParseExternalFileList(t *testing.T) {
	file := eflTestFile(t)
	msg := eflMessage(1, 4, 2, eflHeapAddr, [3]uint64{eflNameA, 0, 100}, [3]uint64{eflNameB, 512, ExternalUnlimited})

	l, err := ParseExternalFileList(bytes.NewReader(file), sharedTestSB(), msg)
	require.NoError(t, err)
	require.Equal(t, uint64(eflHeapAddr), l.HeapAddress)
	require.Equal(t, uint16(4), l.Allocated)
	require.Equal(t, []ExternalFileSlot{
		{Name: "a.bin", Offset: 0, Size: 100},
		{Name: "b.bin", Offset: 512, Size: ExternalUnlimited},
	}, l.Slots)

	total, err := l.TotalSize()
	require.NoError(t, err)
	require.Equal(t, ExternalUnlimited, total)

	// H5MM_strndup copies to the end of the block when no NUL follows.
	noNUL := eflTestFile(t)
	copy(noNUL[eflHeapData+24:], "tailname")
	l, err = ParseExternalFileList(bytes.NewReader(noNUL), sharedTestSB(),
		eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{24, 0, 8}))
	require.NoError(t, err)
	require.Equal(t, "tailname", l.Slots[0].Name)
}

// TestParseExternalFileList_CChecks covers every check of H5O__efl_decode
// and of the local heap header it reads; each is utils.ErrCorrupt.
func TestParseExternalFileList_CChecks(t *testing.T) {
	good := [3]uint64{eflNameA, 0, 16}
	badHeap := func(edit func(f []byte)) []byte {
		f := eflTestFile(t)
		edit(f)
		return f
	}
	cases := []struct {
		name string
		file []byte
		msg  []byte
	}{
		{"empty", nil, nil},
		{"bad version", nil, eflMessage(2, 1, 1, eflHeapAddr, good)},
		{"no slots allocated", nil, eflMessage(1, 0, 0, eflHeapAddr)},
		{"more used than allocated", nil, eflMessage(1, 1, 2, eflHeapAddr, good, good)},
		{"undefined heap", nil, eflMessage(1, 1, 1, undefinedAddress, good)},
		{"heap address truncated", nil, eflMessage(1, 1, 1, eflHeapAddr)[:10]},
		{"slot truncated", nil, eflMessage(1, 1, 1, eflHeapAddr, good)[:30]},
		{"slots beyond the message", nil, eflMessage(1, 0xFFFF, 0xFFFF, eflHeapAddr, good)},
		{"name outside the heap", nil, eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{32, 0, 16})},
		{"empty name", nil, eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{0, 0, 16})},
		{"bad heap signature", badHeap(func(f []byte) { f[eflHeapAddr] = 'X' }), eflMessage(1, 1, 1, eflHeapAddr, good)},
		{"bad heap version", badHeap(func(f []byte) { f[eflHeapAddr+4] = 1 }), eflMessage(1, 1, 1, eflHeapAddr, good)},
		{"bad heap free list", badHeap(func(f []byte) { f[eflHeapAddr+16] = 40 }), eflMessage(1, 1, 1, eflHeapAddr, good)},
		{"undefined heap block", badHeap(func(f []byte) {
			copy(f[eflHeapAddr+24:], le64(undefinedAddress))
		}), eflMessage(1, 1, 1, eflHeapAddr, good)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := tc.file
			if file == nil {
				file = eflTestFile(t)
			}
			_, err := ParseExternalFileList(bytes.NewReader(file), sharedTestSB(), tc.msg)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

func TestExternalFileList_TotalSizeOverflow(t *testing.T) {
	l := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a", Size: math.MaxUint64 - 1}, {Name: "b", Size: 2}}}
	_, err := l.TotalSize()
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// memFile is an in-memory external file that records its Close.
type memFile struct {
	*bytes.Reader
	closed *int
}

func (m memFile) Close() error {
	*m.closed++
	return nil
}

// memOpener opens names from a map; closes counts the files closed.
func memOpener(files map[string][]byte, closes *int) ExternalOpener {
	return func(name string) (ExternalFile, error) {
		data, ok := files[name]
		if !ok {
			return nil, errors.New("no such file")
		}
		return memFile{Reader: bytes.NewReader(data), closed: closes}, nil
	}
}

// TestExternalReader follows H5D__efl_read (H5Defl.c:277-402): the slots are
// concatenated, a slot starts at its file offset, bytes past the end of a
// file read as zeros, and a read past the last slot fails.
func TestExternalReader(t *testing.T) {
	files := map[string][]byte{
		"a.bin": []byte("xxABCDEF"),   // Slot 0: 6 bytes from offset 2.
		"b.bin": []byte("yyyyGHIJKL"), // Slot 1: 4 bytes from offset 4 (the rest of the file is not used).
		"c.bin": []byte("MN"),         // Slot 2: 5 bytes, only 2 on disk.
	}
	l := &ExternalFileList{Slots: []ExternalFileSlot{
		{Name: "a.bin", Offset: 2, Size: 6},
		{Name: "b.bin", Offset: 4, Size: 4},
		{Name: "c.bin", Offset: 0, Size: 5},
	}}
	closes := 0
	r := l.Reader(memOpener(files, &closes))

	got := make([]byte, 15)
	n, err := r.ReadAt(got, 0)
	require.NoError(t, err)
	require.Equal(t, 15, n)
	require.Equal(t, []byte("ABCDEFGHIJMN\x00\x00\x00"), got)
	require.Equal(t, 3, closes, "every opened file is closed")

	// A read that starts inside slot 1 and crosses into slot 2.
	got = make([]byte, 4)
	_, err = r.ReadAt(got, 8)
	require.NoError(t, err)
	require.Equal(t, []byte("IJMN"), got)

	// Past the logical end: no slot holds offset 15.
	_, err = r.ReadAt(make([]byte, 1), 15)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	_, err = r.ReadAt(make([]byte, 2), 14)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// An unlimited last slot takes everything after the earlier slots.
	unl := &ExternalFileList{Slots: []ExternalFileSlot{
		{Name: "a.bin", Offset: 2, Size: 2},
		{Name: "b.bin", Offset: 0, Size: ExternalUnlimited},
	}}
	got = make([]byte, 14)
	_, err = unl.Reader(memOpener(files, &closes)).ReadAt(got, 0)
	require.NoError(t, err)
	require.Equal(t, []byte("AByyyyGHIJKL\x00\x00"), got)
}

func TestExternalReader_Errors(t *testing.T) {
	l := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a.bin", Offset: 0, Size: 8}}}

	// No opener: external storage is refused, not read as zeros.
	_, err := l.Reader(nil).ReadAt(make([]byte, 4), 0)
	require.ErrorIs(t, err, utils.ErrNotSupported)

	// The opener's error is returned.
	closes := 0
	_, err = l.Reader(memOpener(nil, &closes)).ReadAt(make([]byte, 4), 0)
	require.ErrorContains(t, err, "no such file")

	// A file position beyond off_t (H5F_OVERFLOW_HSIZET2OFFT, H5Defl.c:315-316).
	big := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a.bin", Offset: math.MaxInt64, Size: 8}}}
	_, err = big.Reader(memOpener(map[string][]byte{"a.bin": nil}, &closes)).ReadAt(make([]byte, 4), 2)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	_, err = l.Reader(nil).ReadAt(make([]byte, 1), -1)
	require.Error(t, err)
}

// failingFile fails every read with a non-EOF error.
type failingFile struct{}

func (failingFile) ReadAt([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingFile) Close() error                      { return nil }

func TestExternalReader_ReadError(t *testing.T) {
	l := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a.bin", Size: 8}}}
	open := func(string) (ExternalFile, error) { return failingFile{}, nil }
	_, err := l.Reader(open).ReadAt(make([]byte, 4), 0)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// TestDescriptor_ExternalStorage covers H5D__efl_init (H5Defl.c:177-217) and
// the read limit: external storage is not in the file, so a read of it is
// bounded like chunked data.
func TestDescriptor_ExternalStorage(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 4}
	ds := &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{8}}
	contig := &DataLayoutMessage{Class: LayoutContiguous, DataAddress: undefinedAddress, DataSize: 32}
	sb := sharedTestSB()
	r := bytes.NewReader(nil)

	efl := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a", Size: 16}, {Name: "b", Size: 16}}}
	d, err := buildDatasetDescriptor(r, sb, dt, ds, contig, nil, efl)
	require.NoError(t, err)
	require.Same(t, efl, d.ExternalFiles())
	require.True(t, d.ContiguousAllocated(), "C treats external storage as allocated")

	// The data does not fit the external storage.
	short := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a", Size: 31}}}
	_, err = buildDatasetDescriptor(r, sb, dt, ds, contig, nil, short)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// Overflowing total.
	ovf := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a", Size: math.MaxUint64 - 1}, {Name: "b", Size: 2}}}
	_, err = buildDatasetDescriptor(r, sb, dt, ds, contig, nil, ovf)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// Not contiguous.
	chunked := &DataLayoutMessage{Class: LayoutChunked, ChunkIndexType: ChunkIndexBTREE, ChunkSize: []uint64{8, 4}}
	_, err = buildDatasetDescriptor(r, sb, dt, ds, chunked, nil, efl)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	// A huge external dataset in a tiny file: the read limit applies.
	huge := &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{1 << 40}}
	unl := &ExternalFileList{Slots: []ExternalFileSlot{{Name: "a", Size: ExternalUnlimited}}}
	d, err = buildDatasetDescriptor(r, sb, dt, huge, contig, nil, unl)
	require.NoError(t, err)
	_, err = d.ResultSize(d.NumElements(), ResultFloat64, 1<<20)
	require.ErrorIs(t, err, utils.ErrLimit)
}

func FuzzParseExternalFileList(f *testing.F) {
	f.Add(eflMessage(1, 2, 2, eflHeapAddr, [3]uint64{eflNameA, 0, 100}, [3]uint64{eflNameB, 512, ExternalUnlimited}))
	f.Add(eflMessage(1, 1, 1, eflHeapAddr, [3]uint64{24, 0, 8}))
	f.Add(eflMessage(1, 0xFFFF, 0xFFFF, eflHeapAddr))
	f.Fuzz(func(t *testing.T, msg []byte) {
		file := eflTestFile(t)
		l, err := ParseExternalFileList(bytes.NewReader(file), sharedTestSB(), msg)
		if err != nil {
			return
		}
		require.NotEmpty(t, l.Allocated)
		require.LessOrEqual(t, len(l.Slots), int(l.Allocated))
		for _, s := range l.Slots {
			require.NotEmpty(t, s.Name)
		}
	})
}
