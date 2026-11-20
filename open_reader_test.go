package hdf5_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// bare is an io.ReaderAt with no other method: no Size, no Stat. It is what
// a network or archive reader looks like to the library.
type bare struct{ r io.ReaderAt }

func (b bare) ReadAt(p []byte, off int64) (int, error) { return b.r.ReadAt(p, off) }

// failing is a reader whose every read fails.
type failing struct{ err error }

func (f failing) ReadAt([]byte, int64) (int, error) { return 0, f.err }

// everyDataset reads every dataset of a file with Read, by path: its values,
// or the error Read gave (a string dataset has no float64 form). Two files
// with the same content give equal maps.
func everyDataset(t *testing.T, file *hdf5.File) map[string]string {
	t.Helper()

	read := map[string]string{}
	file.Walk(func(path string, obj hdf5.Object) {
		ds, ok := obj.(*hdf5.Dataset)
		if !ok {
			return
		}
		values, err := ds.Read()
		if err != nil {
			read[path] = "error: " + err.Error()
			return
		}
		read[path] = bitsOf(values)
	})

	return read
}

// bitsOf writes each value's bits, so that equal text means equal values,
// a NaN included.
func bitsOf(values []float64) string {
	var out strings.Builder
	for _, v := range values {
		fmt.Fprintf(&out, "%016x ", math.Float64bits(v))
	}

	return out.String()
}

// readByName opens a named file with Open and reads its datasets.
func readByName(t *testing.T, path string) map[string]string {
	t.Helper()

	named, err := hdf5.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, named.Close()) }()

	return everyDataset(t, named)
}

// readByReader opens a reader with OpenReader and reads its datasets.
func readByReader(t *testing.T, r io.ReaderAt, size int64) map[string]string {
	t.Helper()

	held, err := hdf5.OpenReader(r, size)
	require.NoError(t, err)
	defer func() { require.NoError(t, held.Close()) }()

	return everyDataset(t, held)
}

// withUserBlock writes a file whose HDF5 data starts after a user block of
// the given size, and returns its path.
func withUserBlock(t *testing.T, size uint64) string {
	t.Helper()

	name := filepath.Join(t.TempDir(), "userblock.h5")

	fw, err := hdf5.CreateForWrite(name, hdf5.CreateTruncate, hdf5.WithUserBlock(size))
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/values", hdf5.Float64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, dw.Write([]float64{1, 2, 3}))
	require.NoError(t, fw.Close())

	return name
}

// TestOpenReader_ReadsWhatOpenReads opens the same file by name, from its
// bytes in memory, and through a reader that has no method but ReadAt, and
// expects the same datasets and values from all three.
func TestOpenReader_ReadsWhatOpenReads(t *testing.T) {
	paths := map[string]string{
		"superblock v0":      filepath.Join("testdata", "v0.h5"),
		"superblock v2":      filepath.Join("testdata", "v2.h5"),
		"superblock v3":      filepath.Join("testdata", "v3.h5"),
		"gzip":               filepath.Join("testdata", "gzip_test.h5"),
		"groups":             filepath.Join("testdata", "with_groups.h5"),
		"user block of 512":  withUserBlock(t, 512),
		"user block of 4096": withUserBlock(t, 4096),
	}

	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			want := readByName(t, path)
			require.NotEmpty(t, want, "the fixture has no dataset to compare")

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			size := int64(len(raw))

			require.Equal(t, want, readByReader(t, bytes.NewReader(raw), size), "from a bytes.Reader")
			require.Equal(t, want, readByReader(t, bare{bytes.NewReader(raw)}, size), "from a bare io.ReaderAt")
		})
	}
}

// TestOpenReader_SizeIsTheFilesEnd: the size given is the file's end,
// whatever the reader holds or reports.
func TestOpenReader_SizeIsTheFilesEnd(t *testing.T) {
	path := filepath.Join("testdata", "v0.h5")
	want := readByName(t, path)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	size := int64(len(raw))

	t.Run("a section of a larger file", func(t *testing.T) {
		const before = 1000
		padded := append(make([]byte, before), raw...)
		padded = append(padded, make([]byte, before)...)

		section := io.NewSectionReader(bytes.NewReader(padded), before, size)
		require.Equal(t, want, readByReader(t, section, size))
	})

	t.Run("a reader that holds more than the file", func(t *testing.T) {
		longer := append(bytes.Clone(raw), make([]byte, 4096)...)
		require.Equal(t, want, readByReader(t, bytes.NewReader(longer), size))
	})

	t.Run("a size short of the file", func(t *testing.T) {
		file, err := hdf5.OpenReader(bytes.NewReader(raw), size/2)
		require.Error(t, err)
		require.Nil(t, file)
	})
}

// TestOpenReader_Refusals: what is not a file, and a reader that is nil or
// fails, give an error and no File, and never a panic.
func TestOpenReader_Refusals(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "v0.h5"))
	require.NoError(t, err)
	size := int64(len(raw))

	refused := map[string]struct {
		reader io.ReaderAt
		size   int64
	}{
		"nil reader":       {nil, size},
		"typed nil reader": {(*bytes.Reader)(nil), size},
		"negative size":    {bytes.NewReader(raw), -1},
		"size of zero":     {bytes.NewReader(raw), 0},
		"no bytes":         {bytes.NewReader(nil), 0},
		"plain text":       {bytes.NewReader([]byte("this is not an HDF5 file")), 24},
		"half a file":      {bytes.NewReader(raw[:size/2]), size / 2},
		"failing reader":   {failing{errors.New("the reader is broken")}, size},
	}

	for name, input := range refused {
		t.Run(name, func(t *testing.T) {
			file, err := hdf5.OpenReader(input.reader, input.size)
			require.Error(t, err)
			require.Nil(t, file)
		})
	}
}

// TestOpenReader_CloseLeavesTheReader: the caller owns the reader. File.Close
// must not close it, which for a file opened by name it does. The reader
// here is an *os.File, and a closed *os.File fails every read, so a read
// that succeeds after File.Close shows the caller's file was left open.
func TestOpenReader_CloseLeavesTheReader(t *testing.T) {
	owned, err := os.Open(filepath.Join("testdata", "v0.h5"))
	require.NoError(t, err)
	defer func() { require.NoError(t, owned.Close()) }()

	info, err := owned.Stat()
	require.NoError(t, err)

	file, err := hdf5.OpenReader(owned, info.Size())
	require.NoError(t, err)
	require.NoError(t, file.Close())

	signature := make([]byte, 8)
	_, err = owned.ReadAt(signature, 0)
	require.NoError(t, err, "File.Close closed the caller's file")
	require.Equal(t, "\x89HDF\r\n\x1a\n", string(signature))
}
