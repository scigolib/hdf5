package hdf5

// Writing files with a user block, as the C library does with
// H5Pset_userblock (H5Pfcpl.c:314-330): the superblock sits at the end of
// the block, every address in the file is relative to it (the base address,
// H5Fsuper.c:1194-1204), and the stored end-of-file address is absolute
// (H5Fsuper_cache.c:715, rel_eof + base_addr).

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// hdf5Signature is the 8-byte format signature that starts a superblock.
var hdf5Signature = []byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}

// matHeader returns a 128-byte header shaped like a MATLAB v7.3 file's: text,
// subsystem offset, version 0x0200 and the endian indicator "IM".
func matHeader() []byte {
	h := bytes.Repeat([]byte{' '}, 128)
	copy(h, "MATLAB 7.3 MAT-file, Platform: GLNXA64, Created on: Sat Nov  7 12:00:00 2026 HDF5 schema 1.00 .")
	h[124], h[125] = 0x00, 0x02
	h[126], h[127] = 'I', 'M'
	return h
}

// writeUserBlockFile creates a file with a user block of size bytes, writes
// header into it, and a dataset, a group with an attribute and a dataset in
// that group.
func writeUserBlockFile(t *testing.T, file string, size uint64, header []byte, opts ...interface{}) {
	t.Helper()
	opts = append(opts, WithUserBlock(size))
	fw, err := CreateForWrite(file, CreateTruncate, opts...)
	require.NoError(t, err)
	if header != nil {
		require.NoError(t, fw.WriteUserBlock(header))
	}
	ds, err := fw.CreateDataset("/data", Int32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3, 4}))
	g, err := fw.CreateGroup("/grp")
	require.NoError(t, err)
	require.NoError(t, g.WriteAttribute("units", "m"))
	ds, err = fw.CreateDataset("/grp/inner", Float64, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{0.5, 1.5}))
	require.NoError(t, fw.Close())
}

// datasetValues reads every dataset of file with this library.
func datasetValues(t *testing.T, file string) map[string][]float64 {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	values := map[string][]float64{}
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok {
			v, err := ds.Read()
			if err == nil {
				values[path] = v
			}
		}
	})
	return values
}

// TestUserBlock_Create: CreateForWrite with WithUserBlock puts the superblock
// at the end of the block, keeps the bytes WriteUserBlock wrote and zeros in
// the rest of the block, and the file reads back with the base address set.
// Red before the change: WithUserBlock did not exist.
func TestUserBlock_Create(t *testing.T) {
	for _, tc := range []struct {
		name string
		size uint64
		opts []interface{}
	}{
		{"v2 512", 512, nil},
		{"v0 512", 512, []interface{}{WithSuperblockVersion(SuperblockV0)}},
		{"v3 2048", 2048, []interface{}{WithSuperblockVersion(SuperblockV3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "ub.h5")
			header := matHeader()
			writeUserBlockFile(t, file, tc.size, header, tc.opts...)

			raw, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, header, raw[:len(header)])
			require.Equal(t, make([]byte, int(tc.size)-len(header)), raw[len(header):tc.size], "the rest of the block is zeroed")
			require.Equal(t, hdf5Signature, raw[tc.size:tc.size+8], "the superblock follows the block")

			f, err := Open(file)
			require.NoError(t, err)
			require.Equal(t, tc.size, f.Superblock().BaseAddress)
			require.NoError(t, f.Close())

			values := datasetValues(t, file)
			require.Equal(t, []float64{1, 2, 3, 4}, values["/data"])
			require.Equal(t, []float64{0.5, 1.5}, values["/grp/inner"])
		})
	}
}

// TestUserBlock_InvalidSize: the size is 0 or a power of two of at least
// 512, as H5Pset_userblock requires; anything else fails at create.
func TestUserBlock_InvalidSize(t *testing.T) {
	for _, size := range []uint64{1, 100, 256, 511, 513, 768, 1000} {
		file := filepath.Join(t.TempDir(), "bad.h5")
		_, err := CreateForWrite(file, CreateTruncate, WithUserBlock(size))
		require.Error(t, err, "size %d", size)
		require.Contains(t, err.Error(), "user block", "size %d", size)
	}

	file := filepath.Join(t.TempDir(), "none.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithUserBlock(0))
	require.NoError(t, err)
	require.Error(t, fw.WriteUserBlock([]byte{1}), "no user block to write into")
	require.NoError(t, fw.Close())
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, hdf5Signature, raw[:8], "size 0 is no user block")
}

// TestUserBlock_LargeBlockNotInMemory: a large user block is reserved in the
// file, not built in memory. Red before the fix (review F2): the block was
// zeroed from a buffer of its size, so WithUserBlock(1<<40) ended the
// process with an out-of-memory fatal error. 16 MiB is measured here, small
// enough for any test machine.
func TestUserBlock_LargeBlockNotInMemory(t *testing.T) {
	const size = 1 << 24
	file := filepath.Join(t.TempDir(), "big.h5")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fw, err := CreateForWrite(file, CreateTruncate, WithUserBlock(size))
	require.NoError(t, err)
	runtime.ReadMemStats(&after)
	require.NoError(t, fw.Close())
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(size/4), "the block was built in memory")

	f, err := os.Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	head := make([]byte, 4096)
	_, err = f.ReadAt(head, 0)
	require.NoError(t, err)
	require.Equal(t, make([]byte, 4096), head, "the block reads back as zeros")
	sig := make([]byte, 8)
	_, err = f.ReadAt(sig, size)
	require.NoError(t, err)
	require.Equal(t, hdf5Signature, sig)
}

// TestUserBlock_WriteTooLong: WriteUserBlock refuses data longer than the
// block, writing nothing.
func TestUserBlock_WriteTooLong(t *testing.T) {
	file := filepath.Join(t.TempDir(), "long.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithUserBlock(512))
	require.NoError(t, err)
	require.Error(t, fw.WriteUserBlock(make([]byte, 513)))
	require.NoError(t, fw.WriteUserBlock(bytes.Repeat([]byte{7}, 512)))
	require.NoError(t, fw.Close())

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{7}, 512), raw[:512])
	require.Equal(t, hdf5Signature, raw[512:520])
}

// TestCInterop_UserBlock: h5dump reads a file written with a user block, and
// h5unjam extracts exactly the block that was written and leaves an HDF5 file
// h5dump reads.
func TestCInterop_UserBlock(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	h5unjam := h5tool(t, "h5unjam")
	dir := t.TempDir()
	file := filepath.Join(dir, "ub.h5")
	header := matHeader()
	writeUserBlockFile(t, file, 512, header)

	out := runH5(t, h5dump, "--enable-error-stack", file)
	require.Contains(t, out, `DATASET "data"`)
	require.Contains(t, out, `ATTRIBUTE "units"`)

	ub := filepath.Join(dir, "ub.bin")
	plain := filepath.Join(dir, "plain.h5")
	runH5(t, h5unjam, "-i", file, "-u", ub, "-o", plain)
	block, err := os.ReadFile(ub)
	require.NoError(t, err)
	want := append(append([]byte{}, header...), make([]byte, 512-len(header))...)
	require.Equal(t, want, block)
	runH5(t, h5dump, "--enable-error-stack", plain)
}

// TestUserBlock_OpenForWrite: a file with a user block, written by the C
// library, opens for writing; a dataset added to it is readable by this
// library and h5dump, the existing data is unchanged and the block keeps
// every byte. Replaces the v0.14.3 refusal (TestOpenForWriteRefusesUserBlock,
// TestNotSupported_OpenForWriteUserBlock): the write path now applies the
// base address in its I/O layer.
func TestUserBlock_OpenForWrite(t *testing.T) {
	for _, tc := range []struct {
		src  string
		base uint64
	}{
		{"testdata/hdf5_official/twithub.h5", 512},
		{"testdata/hdf5_official/twithub513.h5", 1024},
		{"testdata/reference/userblock-512.h5", 512},
	} {
		t.Run(filepath.Base(tc.src), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "ub.h5")
			orig, err := os.ReadFile(tc.src)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(file, orig, 0o600))
			before := datasetValues(t, file)

			fw, err := OpenForWrite(file, OpenReadWrite, WithUserBlock(tc.base))
			require.NoError(t, err)
			ds, err := fw.CreateDataset("/added", Int32, []uint64{3})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{9, 8, 7}))
			g, err := fw.CreateGroup("/newgrp")
			require.NoError(t, err)
			require.NoError(t, g.WriteAttribute("k", int32(5)))
			ds, err = fw.CreateDataset("/newgrp/tmp", Int32, []uint64{2})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{1, 1}))
			require.NoError(t, fw.Delete("/newgrp/tmp"))
			require.NoError(t, fw.Close())

			raw, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, orig[:tc.base], raw[:tc.base], "the user block changed")

			after := datasetValues(t, file)
			require.Equal(t, []float64{9, 8, 7}, after["/added"])
			delete(after, "/added")
			require.Equal(t, before, after, "existing data changed")
			requireH5dumpClean(t, file)
		})
	}
}

// TestUserBlock_OpenForWriteSizeMismatch: WithUserBlock on OpenForWrite must
// name the block the file has; the block of an existing file is not resized.
func TestUserBlock_OpenForWriteSizeMismatch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ub.h5")
	orig, err := os.ReadFile("testdata/hdf5_official/twithub.h5")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, orig, 0o600))

	_, err = OpenForWrite(file, OpenReadWrite, WithUserBlock(1024))
	require.Error(t, err)
	require.Contains(t, err.Error(), "user block")
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, orig, raw)
}
