package structures

import (
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestNameHash_IsLookup3: the name hash of link (type 5) and attribute
// (type 8) name index records is H5_checksum_lookup3(name, strlen(name), 0)
// (H5Gdense.c H5G__dense_insert, H5Adense.c:510). Names whose length is a
// multiple of 12 end in the "case 12" tail of H5checksum.c, not in the main
// loop; a hash that differs there makes the C library miss the record.
func TestNameHash_IsLookup3(t *testing.T) {
	for n := 0; n <= 40; n++ {
		name := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 2)[:n]
		require.Equal(t, core.JenkinsChecksum([]byte(name)), jenkinsHash(name), "length %d", n)
	}
}
