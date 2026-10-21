package hdf5_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// TestVLenDatatypeSizeBounds covers the public path for a corrupted vlen
// datatype size. A variable-length element on disk is a global heap reference
// of at least offsetSize+8 bytes (4-byte length, heap address, object index;
// C stores sizeof(hvl_t) = 16). Sizes 1-3 made ReadVLenBytes slice backwards
// and panic with "slice bounds out of range [4:1]" on a file that Open accepts.
//
// Before the guard this test panics while walking /g1/VLstring; after it the
// reader returns an error. The mutation is a targeted value, not XOR: the
// corruption corpus flips with ^0xFF (0x10 becomes 0xEF), which never reaches
// this band — that is why the fuzzer needed class 9 plus a size of 1-3.
func TestVLenDatatypeSizeBounds(t *testing.T) {
	const (
		fixture = "testdata/hdf5_official/h5diff_dset1.h5"
		sizeOff = 5148 // Size field of the /g1/VLstring datatype message
	)

	original, err := os.ReadFile(fixture)
	require.NoError(t, err)

	if original[sizeOff] != 0x10 {
		t.Fatalf("fixture drifted: byte %d is 0x%02x, expected 0x10", sizeOff, original[sizeOff])
	}

	for _, size := range []byte{1, 2, 3} {
		mutant := append([]byte(nil), original...)
		mutant[sizeOff] = size

		path := filepath.Join(t.TempDir(), "vlen.h5")
		require.NoError(t, os.WriteFile(path, mutant, 0o600))

		f, err := hdf5.Open(path)
		if err != nil {
			// Rejecting the file at open is an acceptable outcome.
			continue
		}

		f.Walk(func(_ string, obj hdf5.Object) {
			ds, ok := obj.(*hdf5.Dataset)
			if !ok {
				return
			}
			// Must return an error, never panic.
			_, _ = ds.ReadVLenBytes()
		})

		require.NoError(t, f.Close())
	}
}
