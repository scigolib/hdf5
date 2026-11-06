package hdf5

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// eflSlot is one External File List entry of a fixture: the external file's
// name as stored, where its bytes start and how many it holds.
type eflSlot struct {
	name   string
	offset uint64
	size   uint64
}

// eflImage is a little-endian byte builder for hand-made HDF5 files.
type eflImage struct{ bytes.Buffer }

func (b *eflImage) u8(v byte)    { b.WriteByte(v) }
func (b *eflImage) u16(v uint16) { b.Write(binary.LittleEndian.AppendUint16(nil, v)) }
func (b *eflImage) u32(v uint32) { b.Write(binary.LittleEndian.AppendUint32(nil, v)) }
func (b *eflImage) u64(v uint64) { b.Write(binary.LittleEndian.AppendUint64(nil, v)) }

// msg appends a version 1 object header message: type, size, flags, three
// reserved bytes, then the body padded to 8 bytes (H5O_ALIGN_OLD).
func (b *eflImage) msg(typ uint16, body []byte) {
	padded := (len(body) + 7) &^ 7
	b.u16(typ)
	b.u16(uint16(padded))
	b.u8(0)
	b.Write([]byte{0, 0, 0})
	b.Write(body)
	b.Write(make([]byte, padded-len(body)))
}

// ohdrV1 is a version 1 object header holding msgs (H5O__cache_deserialize):
// version, reserved, message count, reference count, header size, padding.
func ohdrV1(nmsgs int, msgs []byte) []byte {
	var b eflImage
	b.u8(1)
	b.u8(0)
	b.u16(uint16(nmsgs))
	b.u32(1)
	b.u32(uint32(len(msgs)))
	b.u32(0)
	b.Write(msgs)
	return b.Bytes()
}

// buildEFLFile writes dir/ext.h5: a 7×4 int32 dataset /DS1 whose storage is
// the given External File List, the layout of the C example h5ex_d_extern.
// The file has a version 2 superblock, a root group with a compact link, and
// a dataset header with dataspace, datatype, fill value, contiguous layout
// with no address, and the EFL whose names live in a local heap, laid out as
// H5D__layout_oh_create writes it (H5Dlayout.c:330-372).
func buildEFLFile(t *testing.T, dir string, slots ...eflSlot) string {
	t.Helper()
	const (
		rootAddr = 48
		rows     = 7
		cols     = 4
	)

	// Root group: link info, group info, one hard link to /DS1.
	var root eflImage
	var linfo eflImage
	linfo.u8(0)
	linfo.u8(0)
	linfo.u64(^uint64(0))
	linfo.u64(^uint64(0))
	root.msg(0x0002, linfo.Bytes())
	root.msg(0x000A, []byte{0, 0})
	var link eflImage
	link.u8(1)
	link.u8(0)
	link.u8(3)
	link.WriteString("DS1")
	dsetAddrPos := root.Len() + 8 + link.Len() // Patched below.
	link.u64(0)
	root.msg(0x0006, link.Bytes())
	rootHdr := ohdrV1(3, root.Bytes())
	dsetAddr := uint64(rootAddr + len(rootHdr))

	// Local heap data block: "" at 0, then each name padded to 8.
	var heapBuf bytes.Buffer
	heapBuf.Write(make([]byte, 8))
	nameOffsets := make([]uint64, len(slots))
	for i, s := range slots {
		nameOffsets[i] = uint64(heapBuf.Len())
		n := (len(s.name) + 1 + 7) &^ 7
		heapBuf.WriteString(s.name)
		heapBuf.Write(make([]byte, n-len(s.name)))
	}
	heapData := heapBuf.Bytes()

	// Dataset messages, with the EFL's heap address patched below.
	var dset eflImage
	var space eflImage
	space.u8(1)
	space.u8(2)
	space.u8(0)
	space.u8(0)
	space.u32(0)
	space.u64(rows)
	space.u64(cols)
	dset.msg(0x0001, space.Bytes())
	var dtype eflImage
	dtype.Write([]byte{0x10, 0x08, 0, 0})
	dtype.u32(4)
	dtype.u16(0)
	dtype.u16(32)
	dset.msg(0x0003, dtype.Bytes())
	dset.msg(0x0005, []byte{2, 2, 2, 0})
	var layout eflImage
	layout.u8(3)
	layout.u8(1)
	layout.u64(^uint64(0))
	layout.u64(rows * cols * 4)
	dset.msg(0x0008, layout.Bytes())
	var efl eflImage
	efl.u8(1)
	efl.Write([]byte{0, 0, 0})
	efl.u16(uint16(len(slots)))
	efl.u16(uint16(len(slots)))
	heapAddrPos := dset.Len() + 8 + efl.Len()
	efl.u64(0)
	for i, s := range slots {
		efl.u64(nameOffsets[i])
		efl.u64(s.offset)
		efl.u64(s.size)
	}
	dset.msg(0x0007, efl.Bytes())
	dsetHdr := ohdrV1(5, dset.Bytes())

	heapAddr := dsetAddr + uint64(len(dsetHdr))
	var heap eflImage
	heap.WriteString("HEAP")
	heap.Write([]byte{0, 0, 0, 0})
	heap.u64(uint64(len(heapData)))
	heap.u64(1) // H5HL_FREE_NULL.
	heap.u64(heapAddr + 32)
	heap.Write(heapData)

	binary.LittleEndian.PutUint64(rootHdr[16+dsetAddrPos:], dsetAddr)
	binary.LittleEndian.PutUint64(dsetHdr[16+heapAddrPos:], heapAddr)
	eof := heapAddr + uint64(heap.Len())

	var sb eflImage
	sb.WriteString("\x89HDF\r\n\x1a\n")
	sb.u8(2)
	sb.u8(8)
	sb.u8(8)
	sb.u8(0)
	sb.u64(0)
	sb.u64(^uint64(0))
	sb.u64(eof)
	sb.u64(rootAddr)
	sb.u32(core.JenkinsChecksum(sb.Bytes()))

	file := append(append(append(sb.Bytes(), rootHdr...), dsetHdr...), heap.Bytes()...)
	require.Equal(t, eof, uint64(len(file)))
	path := filepath.Join(dir, "ext.h5")
	require.NoError(t, os.WriteFile(path, file, 0o600))
	return path
}

// externValues are the values of h5ex_d_extern's DS1: element (i, j) is
// i*j - i (testdata/hdf5_official/ddl/h5ex_d_extern.ddl).
func externValues() []float64 {
	out := make([]float64, 0, 28)
	for i := range 7 {
		for j := range 4 {
			out = append(out, float64(i*j-i))
		}
	}
	return out
}

// externRaw encodes externValues as the raw little-endian int32 bytes the
// external file holds.
func externRaw() []byte {
	var b []byte
	for _, v := range externValues() {
		b = binary.LittleEndian.AppendUint32(b, uint32(int32(v)))
	}
	return b
}

func readDS1(t *testing.T, path string) ([]float64, error) {
	t.Helper()
	f, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	ds := findDataset(f, "/DS1")
	require.NotNil(t, ds)
	return ds.Read()
}

// TestExternalStorage_Read is the regression test for review-068 finding 1 and
// TASK-074: a dataset stored in an external file read as zeros, without an
// error, because the External File List was ignored and the undefined
// contiguous address read as unallocated storage (C never takes that path for
// EFL datasets: H5Dio.c:250, H5Defl.c:227-236).
//
// Red on 40c6d35: the storage is taken for unallocated contiguous storage;
// this fixture has no fill value, so the read fails with "fill value is
// undefined" (with a default fill value it returned zeros, finding 1).
func TestExternalStorage_Read(t *testing.T) {
	t.Run("one unlimited file, as h5ex_d_extern", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "h5ex_d_extern.data"), externRaw(), 0o600))
		path := buildEFLFile(t, dir, eflSlot{"h5ex_d_extern.data", 0, core.ExternalUnlimited})

		got, err := readDS1(t, path)
		require.NoError(t, err)
		require.Equal(t, externValues(), got)

		h5dumpMatches(t, dir, path)
	})

	t.Run("three files with offsets, one in a subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		raw := externRaw()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "p0.bin"), append([]byte("hdr!"), raw[:40]...), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "p1.bin"), raw[40:80], 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "p2.bin"), append(make([]byte, 16), raw[80:]...), 0o600))
		path := buildEFLFile(t, dir,
			eflSlot{"p0.bin", 4, 40},
			eflSlot{"sub/p1.bin", 0, 40},
			eflSlot{"p2.bin", 16, 32})

		got, err := readDS1(t, path)
		require.NoError(t, err)
		require.Equal(t, externValues(), got)

		// A hyperslab crossing the file boundaries: rows 2..5, columns 1..2.
		f, err := Open(path)
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		sel, err := findDataset(f, "/DS1").ReadSlice([]uint64{2, 1}, []uint64{4, 2})
		require.NoError(t, err)
		want := []float64{}
		for i := 2; i < 6; i++ {
			for j := 1; j < 3; j++ {
				want = append(want, float64(i*j-i))
			}
		}
		require.Equal(t, want, sel)

		h5dumpMatches(t, dir, path)
	})

	t.Run("short external file reads zeros past its end", func(t *testing.T) {
		dir := t.TempDir()
		raw := externRaw()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "short.bin"), raw[:100], 0o600))
		path := buildEFLFile(t, dir, eflSlot{"short.bin", 0, 112})

		got, err := readDS1(t, path)
		require.NoError(t, err)
		want := externValues()
		want[25], want[26], want[27] = 0, 0, 0 // H5Defl.c:376-381.
		require.Equal(t, want, got)
	})

	t.Run("missing external file is an error", func(t *testing.T) {
		dir := t.TempDir()
		path := buildEFLFile(t, dir, eflSlot{"absent.bin", 0, 112})
		_, err := readDS1(t, path)
		require.Error(t, err)
		require.ErrorContains(t, err, "absent.bin")
	})

	t.Run("storage smaller than the data is corrupt", func(t *testing.T) {
		dir := t.TempDir()
		path := buildEFLFile(t, dir, eflSlot{"x.bin", 0, 111})
		_, err := readDS1(t, path)
		require.ErrorIs(t, err, ErrCorrupt) // H5Defl.c:211-212.
	})
}

// h5dumpMatches checks that the C library reads the fixture as
// h5ex_d_extern.ddl shows it. h5dump resolves external names against its
// working directory (no prefix set), so it runs in the fixture's directory.
func h5dumpMatches(t *testing.T, dir, path string) {
	t.Helper()
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Log("h5dump not in PATH: C cross-check skipped")
		return
	}
	cmd := exec.Command(h5dump, "-d", "/DS1", "-w", "80", filepath.Base(path))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "h5dump failed:\n%s", out)
	for _, row := range []string{"(0,0): 0, 0, 0, 0,", "(3,0): -3, 0, 3, 6,", "(6,0): -6, 0, 6, 12"} {
		require.Contains(t, string(out), row, "h5dump output:\n%s", out)
	}
}

// TestExternalStorage_PathPolicy: names come from the file, so only names
// that stay inside the external file directory are opened (TASK-074 security
// design). Each refused name fails with ErrNotSupported and is never read.
func TestExternalStorage_PathPolicy(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.bin")
	require.NoError(t, os.WriteFile(secret, externRaw(), 0o600))

	refused := map[string]string{
		"absolute":         secret,
		"parent":           "../" + filepath.Base(outside) + "/secret.bin",
		"climbs after sub": "sub/../../secret.bin",
		"dot-dot only":     "..",
	}
	if runtime.GOOS == "windows" {
		refused["volume"] = "C:secret.bin"
		refused["unc"] = `\\server\share\secret.bin`
		refused["rooted"] = `\secret.bin`
		refused["backslash parent"] = `..\secret.bin`
		refused["reserved name"] = "NUL"
	} else {
		refused["rooted"] = "/etc/passwd"
	}

	for name, extName := range refused {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := buildEFLFile(t, dir, eflSlot{extName, 0, core.ExternalUnlimited})
			_, err := readDS1(t, path)
			require.ErrorIs(t, err, ErrNotSupported)
			require.ErrorContains(t, err, strconv.Quote(extName))
		})
	}

	t.Run("symlink leading outside", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink(secret, filepath.Join(dir, "link.bin")); err != nil {
			t.Skipf("cannot create a symbolic link here: %v", err)
		}
		path := buildEFLFile(t, dir, eflSlot{"link.bin", 0, core.ExternalUnlimited})
		got, err := readDS1(t, path)
		require.Error(t, err, "read %v through a link leaving the directory", got)
	})
}

// TestExternalStorage_Prefix: SetExternalFilePrefix moves the directory;
// names are still confined to it.
func TestExternalStorage_Prefix(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "data.bin"), externRaw(), 0o600))
	path := buildEFLFile(t, dir, eflSlot{"data.bin", 0, core.ExternalUnlimited})

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	require.Equal(t, dir, f.ExternalFilePrefix())

	ds := findDataset(f, "/DS1")
	_, err = ds.Read()
	require.Error(t, err, "data.bin is not next to the HDF5 file")

	require.NoError(t, f.SetExternalFilePrefix(dataDir))
	require.Equal(t, dataDir, f.ExternalFilePrefix())
	got, err := ds.Read()
	require.NoError(t, err)
	require.Equal(t, externValues(), got)

	require.NoError(t, f.SetExternalFilePrefix(""))
	require.Equal(t, dir, f.ExternalFilePrefix())
}

// TestExternalStorage_ReadLimit: external storage is not in the HDF5 file and
// bytes past the end of an external file read as zeros, so a few bytes of
// metadata can claim any size. The read limit bounds it, as for chunked data.
func TestExternalStorage_ReadLimit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "d.bin"), externRaw(), 0o600))
	path := buildEFLFile(t, dir, eflSlot{"d.bin", 0, core.ExternalUnlimited})

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	f.SetReadLimit(64) // 28 elements need 224 bytes as float64.

	_, err = findDataset(f, "/DS1").Read()
	require.ErrorIs(t, err, utils.ErrLimit)
	require.True(t, strings.Contains(err.Error(), "read limit"), err.Error())
}
