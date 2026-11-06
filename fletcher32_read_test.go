package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestFletcher32_OfficialFiles checks TASK-073 against chunks the C library
// wrote: every Fletcher32-protected dataset reads unchanged, so our checksum
// (H5_checksum_fletcher32, H5checksum.c:94-136) agrees with C's; a chunk with
// one data byte changed fails with utils.ErrCorrupt, as H5Z__filter_fletcher32
// fails with "data error detected by Fletcher32 checksum"
// (H5Zfletcher32.c:96-104).
//
// The corrupt half is red on 40c6d35, which dropped the checksum unchecked.
func TestFletcher32_OfficialFiles(t *testing.T) {
	cases := []struct{ file, dataset string }{
		{"testdata/hdf5_official/h5repack_fletcher.h5", "/dset_fletcher32"},
		{"testdata/hdf5_official/tfilters.h5", "/fletcher32"},
		{"testdata/hdf5_official/be_data.h5", "/Fletcher_float_data_be"},
		{"testdata/hdf5_official/le_data.h5", "/Fletcher_float_data_le"},
		{"testdata/hdf5_official/h5repack_filters.h5", "/dset_all"}, // shuffle, fletcher32, deflate
	}
	for _, tc := range cases {
		t.Run(filepath.Base(tc.file)+tc.dataset, func(t *testing.T) {
			f, err := Open(tc.file)
			require.NoError(t, err)
			ds := findDataset(f, tc.dataset)
			require.NotNil(t, ds, tc.dataset)
			want, err := ds.Read()
			require.NoError(t, err)
			require.NotEmpty(t, want)

			desc, err := ds.descriptor()
			require.NoError(t, err)
			chunks, err := desc.CollectChunks(f.reader, f.sb, utils.DefaultReadLimit)
			require.NoError(t, err)
			require.NotEmpty(t, chunks)
			addr := chunks[0].Address
			require.NoError(t, f.Close())

			// The same file with the first byte of the first chunk changed.
			raw, err := os.ReadFile(tc.file)
			require.NoError(t, err)
			require.Less(t, addr, uint64(len(raw)))
			raw[addr] ^= 0x01
			bad := filepath.Join(t.TempDir(), "corrupt.h5")
			require.NoError(t, os.WriteFile(bad, raw, 0o600))

			f2, err := Open(bad)
			require.NoError(t, err)
			defer func() { require.NoError(t, f2.Close()) }()
			ds2 := findDataset(f2, tc.dataset)
			require.NotNil(t, ds2, tc.dataset)
			_, err = ds2.Read()
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}
