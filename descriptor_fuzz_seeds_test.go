package hdf5

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// descriptorSeedDir is the committed corpus of core.FuzzDatasetDescriptor.
var descriptorSeedDir = filepath.Join("internal", "core", "testdata", "fuzz", "FuzzDatasetDescriptor")

// TestGenerateDescriptorFuzzSeeds writes the seed corpus of
// core.FuzzDatasetDescriptor from the datasets of testdata/reference: each
// seed holds a dataset's datatype, dataspace, layout and filter pipeline
// messages, the file's offset size and its size. It runs only when
// HDF5_WRITE_FUZZ_SEEDS=1, because it rewrites committed files:
//
//	HDF5_WRITE_FUZZ_SEEDS=1 go test -run TestGenerateDescriptorFuzzSeeds .
func TestGenerateDescriptorFuzzSeeds(t *testing.T) {
	if os.Getenv("HDF5_WRITE_FUZZ_SEEDS") != "1" {
		t.Skip("set HDF5_WRITE_FUZZ_SEEDS=1 to regenerate the FuzzDatasetDescriptor corpus")
	}

	files, err := filepath.Glob(filepath.Join("testdata", "reference", "*.h5"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	sort.Strings(files)

	require.NoError(t, os.MkdirAll(descriptorSeedDir, 0o750))

	written := 0
	for _, path := range files {
		for _, seed := range descriptorSeeds(t, path) {
			sum := sha256.Sum256([]byte(seed))
			name := filepath.Join(descriptorSeedDir, hex.EncodeToString(sum[:8]))
			require.NoError(t, os.WriteFile(name, []byte(seed), 0o600))
			written++
		}
	}
	t.Logf("wrote %d seeds to %s", written, descriptorSeedDir)
}

// descriptorSeeds returns one corpus entry per dataset of the file at path.
func descriptorSeeds(t *testing.T, path string) []string {
	t.Helper()

	f, err := Open(path)
	if err != nil {
		return nil // Files this library cannot open are covered by FuzzOpenFile.
	}
	defer func() { _ = f.Close() }()

	info, err := os.Stat(path)
	require.NoError(t, err)
	fileSize := min(uint64(info.Size()), 1<<20-1)

	var seeds []string
	f.Walk(func(_ string, obj Object) {
		ds, ok := obj.(*Dataset)
		if !ok {
			return
		}
		header, err := core.ReadObjectHeader(f.Reader(), ds.Address(), f.Superblock())
		if err != nil {
			return
		}

		msgs := map[core.MessageType][]byte{}
		for _, m := range header.Messages {
			if _, seen := msgs[m.Type]; !seen {
				msgs[m.Type] = m.Data
			}
		}

		var b strings.Builder
		b.WriteString("go test fuzz v1\n")
		for _, typ := range []core.MessageType{core.MsgDatatype, core.MsgDataspace, core.MsgDataLayout, core.MsgFilterPipeline} {
			fmt.Fprintf(&b, "[]byte(%s)\n", strconv.Quote(string(msgs[typ])))
		}
		fmt.Fprintf(&b, "uint8(%d)\n", f.Superblock().OffsetSize)
		fmt.Fprintf(&b, "uint32(%d)\n", fileSize)
		seeds = append(seeds, b.String())
	})

	return seeds
}
