package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEncodeScalarDataspaceKeepsItsVersion checks that a scalar dataspace is encoded in the version its
// message carries, as H5O__sdspace_encode does (H5Osdspace.c:249-297), and parses back as scalar.
func TestEncodeScalarDataspaceKeepsItsVersion(t *testing.T) {
	cases := []struct {
		version uint8
		want    []byte
	}{
		{DataspaceVersion1, []byte{1, 0, 0, 0, 0, 0, 0, 0}},
		{DataspaceVersion2, []byte{2, 0, 0, byte(DataspaceScalar)}},
	}
	for _, tc := range cases {
		got, err := encodeDataspaceOf(&DataspaceMessage{Version: tc.version, Type: DataspaceScalar})
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "version %d", tc.version)

		ds, err := ParseDataspaceMessage(got)
		require.NoError(t, err)
		require.Equal(t, tc.version, ds.Version)
		require.Equal(t, DataspaceScalar, ds.Type)
		require.Empty(t, ds.Dimensions)
	}
}
