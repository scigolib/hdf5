package hdf5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// visitAll opens path, walks every object and reads every object's attributes,
// so every structure the warnings cover is read, and returns the file's
// warnings.
func visitAll(t *testing.T, path string) []Warning {
	t.Helper()
	ws, err := tryVisitAll(path)
	require.NoError(t, err)
	return ws
}

func tryVisitAll(path string) (ws []Warning, err error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	var walkErr error
	f.Walk(func(p string, obj Object) {
		if walkErr != nil {
			return
		}
		switch o := obj.(type) {
		case *Dataset:
			// The descriptor decodes the datatype; a valid type this library
			// cannot describe (ErrNotSupported) is not a finding.
			if _, err := o.descriptor(); err != nil && !errors.Is(err, ErrNotSupported) {
				walkErr = fmt.Errorf("%s: %w", p, err)
				return
			}
			if _, err := o.Attributes(); err != nil {
				walkErr = fmt.Errorf("%s: %w", p, err)
			}
		case *Group:
			if _, err := o.Attributes(); err != nil {
				walkErr = fmt.Errorf("%s: %w", p, err)
			}
		}
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return f.Warnings(), nil
}

// requireNoWarnings is the strict verifier: a file the C library or this
// release wrote must produce no warning at all.
func requireNoWarnings(t *testing.T, path string) {
	t.Helper()
	ws := visitAll(t, path)
	require.Empty(t, ws, "%s: unexpected warnings %v", path, ws)
}

func warningCodes(ws []Warning) map[WarningCode][]uint64 {
	out := map[WarningCode][]uint64{}
	for _, w := range ws {
		out[w.Code] = append(out[w.Code], w.Addr)
	}
	return out
}

// checksumEnd finds the checksummed block that starts at p: the smallest n
// in [min, max) with lookup3(b[p:p+n]) stored at b[p+n:].
func checksumEnd(t *testing.T, b []byte, p, minLen, maxLen int) int {
	t.Helper()
	for n := minLen; n < maxLen && p+n+4 <= len(b); n++ {
		if binary.LittleEndian.Uint32(b[p+n:]) == core.JenkinsChecksum(b[p:p+n]) {
			return n
		}
	}
	t.Fatalf("no lookup3 checksum for the block at %d", p)
	return 0
}

// toCRC32 rewrites the lookup3 checksum of the block with signature sig (its
// nth occurrence in b) as CRC32-IEEE, as hdf5-go v0.13.4 and earlier wrote it,
// and returns the block's address.
func toCRC32(t *testing.T, b []byte, sig string, nth int) int {
	t.Helper()
	p := -1
	for i := 0; i <= nth; i++ {
		q := bytes.Index(b[p+1:], []byte(sig))
		require.GreaterOrEqual(t, q, 0, "signature %s #%d", sig, i)
		p += 1 + q
	}
	n := checksumEnd(t, b, p, 8, 4096)
	binary.LittleEndian.PutUint32(b[p+n:], crc32.ChecksumIEEE(b[p:p+n]))
	return p
}

// v2HeaderEnd returns where the messages of chunk 0 of the version 2 object
// header at p end, and where its chunk size field is and how wide it is.
func v2HeaderEnd(t *testing.T, b []byte, p int) (end, sizeAt, width int) {
	t.Helper()
	require.Equal(t, "OHDR", string(b[p:p+4]))
	require.Equal(t, byte(2), b[p+4])
	flags := b[p+5]
	sizeAt = p + 6
	if flags&0x20 != 0 {
		sizeAt += 16
	}
	if flags&0x10 != 0 {
		sizeAt += 4
	}
	width = 1 << (flags & 3)
	var size int
	for k := 0; k < width; k++ {
		size |= int(b[sizeAt+k]) << (8 * k)
	}
	return sizeAt + width + size, sizeAt, width
}

// writeCurrentFile writes a file with this release: a superblock v2, a
// dataset /d of 4 float64 and nAttrs int32 attributes on it (8 or more go to
// dense storage). It returns the path and the dataset's header address.
func writeCurrentFile(t *testing.T, nAttrs int) (string, uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "current.h5")
	fw, err := CreateForWrite(path, CreateTruncate, WithSuperblockVersion(2))
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/d", Float64, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3, 4}))
	for i := 0; i < nAttrs; i++ {
		require.NoError(t, ds.WriteAttribute(fmt.Sprintf("a%02d", i), int32(i)))
	}
	require.NoError(t, fw.Close())

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var addr uint64
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/d" {
			addr = d.Address()
		}
	})
	require.NotZero(t, addr)
	return path, addr
}

func readD(t *testing.T, path string) ([]float64, []Warning) {
	t.Helper()
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var data []float64
	var readErr error
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/d" {
			data, readErr = d.Read()
		}
	})
	require.NoError(t, readErr)
	return data, f.Warnings()
}

// legacyMatlab are the MATLAB v7.3 files the matlab module wrote with hdf5-go
// v0.11.4-beta (testdata/reference/legacy-hdf5go/README.md): one dataset each,
// its values and its MATLAB_class attribute.
var legacyMatlab = []struct {
	name, path, class string
	data              []float64
}{
	{"matlab_simple_double.mat", "/data", "double", []float64{1, 2, 3, 4, 5}},
	{"matlab_simple_int32.mat", "/values", "int32", []float64{10, 20, 30, 40}},
	{"matlab_simple_single.mat", "/floats", "single", []float64{1.5, 2.5, 3.5}},
}

// TestWarnings_LegacyMatlabFiles: the early files carry a CRC32 superblock
// checksum, object headers without checksums and numeric datatypes in the
// early encoding. They read as the writer meant, and each finding is a
// warning.
func TestWarnings_LegacyMatlabFiles(t *testing.T) {
	for _, tc := range legacyMatlab {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", "reference", "legacy-hdf5go", tc.name)
			ws := visitAll(t, path)
			codes := warningCodes(ws)
			require.Equal(t, []uint64{0}, codes[WarnLegacySuperblockChecksum])
			require.NotEmpty(t, codes[WarnHeaderChecksum])
			require.Len(t, codes[WarnLegacyDatatype], 1, "%v", ws)
			for _, w := range ws {
				require.Contains(t, w.Message, "C library", w.String())
			}

			f, err := Open(path)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			var data []float64
			var class any
			var readErr, attrErr error
			f.Walk(func(p string, obj Object) {
				if d, ok := obj.(*Dataset); ok && p == tc.path {
					data, readErr = d.Read()
					class, attrErr = d.ReadAttribute("MATLAB_class")
				}
			})
			require.NoError(t, readErr)
			require.NoError(t, attrErr)
			require.Equal(t, tc.data, data)
			require.Equal(t, tc.class, class)
		})
	}
}

// TestOpenForWrite_RefusesLegacyMatlab: OpenForWrite refuses the early files
// with ErrNeedsRepack (headers without checksums, early datatypes) before it
// writes anything: the file stays byte-identical.
func TestOpenForWrite_RefusesLegacyMatlab(t *testing.T) {
	for _, tc := range legacyMatlab {
		src, err := os.ReadFile(filepath.Join("testdata", "reference", "legacy-hdf5go", tc.name))
		require.NoError(t, err)
		path := filepath.Join(t.TempDir(), tc.name)
		require.NoError(t, os.WriteFile(path, src, 0o600))

		fw, err := OpenForWrite(path, OpenReadWrite)
		if fw != nil {
			require.NoError(t, fw.Close())
		}
		require.ErrorIs(t, err, ErrNeedsRepack, tc.name)
		require.NotErrorIs(t, err, ErrCorrupt, tc.name)

		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.True(t, bytes.Equal(src, after), "%s changed", tc.name)
	}
}

// TestOpenForWrite_RefusesEarlyDatatypeOnly: a current file whose dataset
// datatype is rewritten into the early encoding, its header checksum fixed,
// is refused for WarnLegacyDatatype alone.
func TestOpenForWrite_RefusesEarlyDatatypeOnly(t *testing.T) {
	path, addr := writeCurrentFile(t, 0)
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	modern, err := core.NewFloatDatatype(8)
	require.NoError(t, err)
	enc, err := core.EncodeDatatypeMessage(modern)
	require.NoError(t, err)
	early := []byte{0x11, 0, 0, 0, 8, 0, 0, 0, 0, 0x40, 0, 0x0b, 0x34, 0x7f, 0, 0, 0, 0, 0, 0}
	require.Len(t, early, len(enc))

	hdr := int(addr)
	end, _, _ := v2HeaderEnd(t, b, hdr)
	at := bytes.Index(b[hdr:end], enc)
	require.GreaterOrEqual(t, at, 0, "the dataset's datatype message")
	copy(b[hdr+at:], early)
	binary.LittleEndian.PutUint32(b[end:], core.JenkinsChecksum(b[hdr:end]))
	require.NoError(t, os.WriteFile(path, b, 0o600))

	data, ws := readD(t, path)
	require.Equal(t, []float64{1, 2, 3, 4}, data)
	require.Len(t, warningCodes(ws)[WarnLegacyDatatype], 1, "%v", ws)
	require.Len(t, ws, 1, "%v", ws)

	fw, err := OpenForWrite(path, OpenReadWrite)
	if fw != nil {
		require.NoError(t, fw.Close())
	}
	require.ErrorIs(t, err, ErrNeedsRepack)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(b, after), "a refused file stays unchanged")
}

// TestWarnings_LegacyDenseLayout: dense attributes v0.14.2 wrote (type 5
// attribute name index) read with a WarnLegacyDenseLayout warning.
func TestWarnings_LegacyDenseLayout(t *testing.T) {
	ws := visitAll(t, filepath.Join("testdata", "reference", "legacy-hdf5go", "dense_attr_type5.h5"))
	require.NotEmpty(t, warningCodes(ws)[WarnLegacyDenseLayout], "%v", ws)
}

// TestWarnings_LegacyMetadataChecksum: a version 2 B-tree header and a
// fractal heap header whose checksums are CRC32-IEEE (hdf5-go up to v0.13.4)
// read with WarnLegacyMetadataChecksum at their addresses; any other wrong
// value is corruption, not a warning.
func TestWarnings_LegacyMetadataChecksum(t *testing.T) {
	path, _ := writeCurrentFile(t, 12)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	bthd := toCRC32(t, b, "BTHD", 0)
	frhp := toCRC32(t, b, "FRHP", 0)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	codes := warningCodes(visitAll(t, path))
	require.Contains(t, codes[WarnLegacyMetadataChecksum], uint64(bthd))
	require.Contains(t, codes[WarnLegacyMetadataChecksum], uint64(frhp))

	// Neither lookup3 nor CRC32: the attributes fail as corrupt. The header
	// is 4+1+1+4+2+2+1+1 + sizeof_addr + 2 + sizeof_size bytes before its
	// checksum (H5B2cache.c:224-320); this writer uses 8/8.
	n := 4 + 1 + 1 + 4 + 2 + 2 + 1 + 1 + 8 + 2 + 8
	binary.LittleEndian.PutUint32(b[bthd+n:], crc32.ChecksumIEEE(b[bthd:bthd+n])^1)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var attrErr error
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/d" {
			_, attrErr = d.Attributes()
		}
	})
	require.ErrorIs(t, attrErr, ErrCorrupt)
}

// TestWarnings_V0139HeaderChunkSize: an object header in the layout of
// hdf5-go v0.13.9 (chunk size counting the checksum) reads with its data
// intact and a WarnHeaderChunkSize warning at its address.
func TestWarnings_V0139HeaderChunkSize(t *testing.T) {
	path, addr := writeCurrentFile(t, 0)
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	p := int(addr)
	end, sizeAt, width := v2HeaderEnd(t, b, p)
	require.Equal(t, core.JenkinsChecksum(b[p:end]), binary.LittleEndian.Uint32(b[end:]), "current header has lookup3")
	size := end - (sizeAt + width) + 4
	require.Less(t, size, 1<<(8*width), "the larger size fits its field")
	for k := 0; k < width; k++ {
		b[sizeAt+k] = byte(size >> (8 * k))
	}
	binary.LittleEndian.PutUint32(b[end:], core.JenkinsChecksum(b[p:end]))
	require.NoError(t, os.WriteFile(path, b, 0o600))

	data, ws := readD(t, path)
	require.Equal(t, []float64{1, 2, 3, 4}, data)
	codes := warningCodes(ws)
	require.Equal(t, []uint64{addr}, codes[WarnHeaderChunkSize], "%v", ws)
	require.Empty(t, codes[WarnHeaderChecksum], "%v", ws)
}

// TestWarnings_HeaderChecksumMismatch: an object header whose checksum does
// not match reads with a WarnHeaderChecksum warning at its address.
func TestWarnings_HeaderChecksumMismatch(t *testing.T) {
	path, addr := writeCurrentFile(t, 0)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	end, _, _ := v2HeaderEnd(t, b, int(addr))
	b[end] ^= 0xFF
	require.NoError(t, os.WriteFile(path, b, 0o600))

	data, ws := readD(t, path)
	require.Equal(t, []float64{1, 2, 3, 4}, data)
	require.Equal(t, []uint64{addr}, warningCodes(ws)[WarnHeaderChecksum], "%v", ws)
}

// TestWarnings_NoneForCAndCurrentFiles: files the C library wrote
// (testdata/reference) and files this release writes, compact and dense
// attributes, give no warning.
func TestWarnings_NoneForCAndCurrentFiles(t *testing.T) {
	refs, err := filepath.Glob(filepath.Join("testdata", "reference", "*.h5"))
	require.NoError(t, err)
	require.NotEmpty(t, refs)
	checked := 0
	for _, ref := range refs {
		t.Run(filepath.Base(ref), func(t *testing.T) {
			ws, err := tryVisitAll(ref)
			if err != nil {
				// The reference set includes deliberately corrupt and
				// multi-file inputs that do not open on their own; the
				// suites that own them assert their errors.
				t.Skipf("does not read on its own: %v", err)
			}
			require.Empty(t, ws, "unexpected warnings %v", ws)
			checked++
		})
	}
	require.Greater(t, checked, len(refs)*3/4, "most reference files must be checked, not skipped")
	for _, n := range []int{0, 3, 12} {
		t.Run(fmt.Sprintf("current_%d_attrs", n), func(t *testing.T) {
			path, _ := writeCurrentFile(t, n)
			requireNoWarnings(t, path)
		})
	}
}

func TestWarningCode_String(t *testing.T) {
	for c := WarnLegacySuperblockChecksum; c <= WarnSuppressed; c++ {
		require.False(t, strings.HasPrefix(c.String(), "WarningCode("), "code %d has no name", c)
	}
	require.Equal(t, "WarningCode(200)", WarningCode(200).String())
}
