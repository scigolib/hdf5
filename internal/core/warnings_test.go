package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWarnings_DedupAndCap: one warning per code and address; past
// maxWarnings, further findings are counted in a final WarnSuppressed entry;
// a nil superblock records nothing.
func TestWarnings_DedupAndCap(t *testing.T) {
	var nilSB *Superblock
	nilSB.Warnf(WarnHeaderChecksum, 1, "x")
	require.Nil(t, nilSB.Warnings())

	sb := &Superblock{}
	require.Nil(t, sb.Warnings())

	sb.Warnf(WarnHeaderChecksum, 8, "header at 0x%X", 8)
	sb.Warnf(WarnHeaderChecksum, 8, "again")
	sb.Warnf(WarnHeaderChunkSize, 8, "other code, same address")
	ws := sb.Warnings()
	require.Len(t, ws, 2)
	require.Equal(t, Warning{Code: WarnHeaderChecksum, Addr: 8, Message: "header at 0x8"}, ws[0])

	for a := uint64(100); a < 100+maxWarnings+3; a++ {
		sb.Warnf(WarnLegacyMetadataChecksum, a, "block")
	}
	ws = sb.Warnings()
	require.Len(t, ws, maxWarnings+1)
	last := ws[len(ws)-1]
	require.Equal(t, WarnSuppressed, last.Code)
	require.Contains(t, last.Message, "5 more warnings")

	ws[0].Message = "changed"
	require.Equal(t, "header at 0x8", sb.Warnings()[0].Message, "Warnings returns a copy")
}
