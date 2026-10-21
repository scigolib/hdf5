package core

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCompoundData_MemberOffsetBeyondStruct is the ADR-003 regression
// test found by TestCorruptionCorpus: member.Offset comes from the compound
// datatype properties in the file, and structData[member.Offset:] therefore
// panicked on a corrupt value (slice bounds out of range). The offset must be
// validated against the struct size before the slice.
func TestParseCompoundData_MemberOffsetBeyondStruct(t *testing.T) {
	ct := &CompoundType{
		Size: 16,
		Members: []CompoundMember{
			{Name: "bad", Offset: 1 << 28, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
		},
	}

	rawData := make([]byte, 16)

	_, err := parseCompoundData(rawData, ct, 1, bytes.NewReader(rawData), &Superblock{OffsetSize: 8})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds compound size")
}

// TestParseCompoundData_MemberOffsetInsideStruct pins the behavior the guard
// must not change: a member that starts inside the struct still parses.
func TestParseCompoundData_MemberOffsetInsideStruct(t *testing.T) {
	ct := &CompoundType{
		Size: 16,
		Members: []CompoundMember{
			{Name: "tail", Offset: 8, Type: &DatatypeMessage{Class: DatatypeFloat, Size: 8}},
		},
	}

	rawData := make([]byte, 16)

	result, err := parseCompoundData(rawData, ct, 1, bytes.NewReader(rawData), &Superblock{OffsetSize: 8})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, float64(0), result[0]["tail"])
}
