package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseFilterPipelineMessage_V1PaddedNameOverflow covers the padded filter
// name length of a version 1 pipeline. The name length is a uint16 from the
// file and the padding rounds it up to a multiple of 8; 65535 rounds to 65536,
// which does not fit in uint16. Computing the padded value in uint16 wraps it
// to zero, so the bounds check passes and the slice below reads 65535 bytes
// from a 16-byte message. The parser must reject the filter.
func TestParseFilterPipelineMessage_V1PaddedNameOverflow(t *testing.T) {
	data := []byte{
		1,                // version
		1,                // number of filters
		0, 0, 0, 0, 0, 0, // v1 reserved
		1, 0, // filter ID (deflate)
		0xFF, 0xFF, // filter name length: 65535, pads to 65536
		0, 0, // flags
		0, 0, // number of client data values
	}

	pipeline, err := ParseFilterPipelineMessage(data)
	require.Error(t, err, "a filter name longer than the message must be rejected")
	require.Nil(t, pipeline)
}

// TestParseFilterPipelineMessage_V1PaddedNameValid keeps the ordinary padded
// name path working: a two-byte name pads to 8 bytes and the filter parses.
func TestParseFilterPipelineMessage_V1PaddedNameValid(t *testing.T) {
	name := []byte{'a', 0, 0, 0, 0, 0, 0, 0} // "a" padded to 8 bytes
	data := append([]byte{
		1,                // version
		1,                // number of filters
		0, 0, 0, 0, 0, 0, // v1 reserved
		1, 0, // filter ID (deflate)
		2, 0, // filter name length (including the null terminator)
		0, 0, // flags
		0, 0, // number of client data values
	}, name...)

	pipeline, err := ParseFilterPipelineMessage(data)
	require.NoError(t, err)
	require.Len(t, pipeline.Filters, 1)
	require.Equal(t, "a", pipeline.Filters[0].Name)
}
