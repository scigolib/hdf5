package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// Layout of torderattr.h5's group header at 1700 (h5debug, the file's bytes):
// a version-2 header with flags 0x24 (creation order tracked, times stored,
// 1-byte chunk size), so a message header is 6 bytes. Chunk 0 holds 24
// bytes: one continuation message (6 + 16 bytes), then a 2-byte gap, then
// the checksum. The group's root header at 96 is version 1, with a 24-byte
// first chunk holding one continuation message.
const (
	tordGroupHdr      = 1700
	tordGroupSizeByte = tordGroupHdr + 22 // after OHDR, version, flags, 16 bytes of times
	tordGroupMsg0     = tordGroupSizeByte + 1
	tordRootHdr       = 96
)

// resignOHDR rewrites the checksum of the version-2 chunk 0 at tordGroupHdr
// after a test changed it, so the change reaches the message decoder.
func resignOHDR(img []byte) {
	end := tordGroupMsg0 + int(img[tordGroupSizeByte])
	binary.LittleEndian.PutUint32(img[end:], JenkinsChecksum(img[tordGroupHdr:end]))
}

// TestObjectHeaderV2_Gap: a version-2 chunk may end in a gap shorter than a
// message header, which C skips (H5Ocache.c:1507-1521). Red before TASK-058:
// the reader decoded the gap and the checksum as a message header and
// failed with "message data read failed: EOF", so the attributes and links
// of the group at 1700 could not be read.
func TestObjectHeaderV2_Gap(t *testing.T) {
	img, err := os.ReadFile(torderattrFile)
	require.NoError(t, err)
	r := bytes.NewReader(img)
	sb, err := ReadSuperblock(r)
	require.NoError(t, err)

	hdr, err := ReadObjectHeader(r, tordGroupHdr, sb)
	require.NoError(t, err)
	types := make([]MessageType, 0, len(hdr.Messages))
	for _, m := range hdr.Messages {
		types = append(types, m.Type)
	}
	// h5debug: continuation, then in chunk 1 stab, ainfo and attributes c, b, a.
	require.Equal(t, []MessageType{MsgContinuation, MsgSymbolTable, MsgAttributeInfo,
		MsgAttribute, MsgAttribute, MsgAttribute}, types)
}

// TestObjectHeader_GapCorrupt: the gaps C refuses, and a message that runs
// past its chunk, are utils.ErrCorrupt. Red before TASK-058: the readers had
// no gap rule; a version-1 gap was skipped silently, and the version-2 cases
// read the gap and the checksum as messages.
func TestObjectHeader_GapCorrupt(t *testing.T) {
	base, err := os.ReadFile(torderattrFile)
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		addr  uint64
		patch func(b []byte)
		want  string
	}{
		{"v2 gap with a null message", tordGroupHdr, func(b []byte) {
			b[tordGroupMsg0] = byte(MsgNil) // H5Ocache.c:1513-1515
			resignOHDR(b)
		}, "1 of them null"},
		{"v2 chunk that is a gap only", tordGroupHdr, func(b []byte) {
			b[tordGroupSizeByte] = 3
			resignOHDR(b)
		}, "after 0 messages"},
		{"v2 message past the chunk", tordGroupHdr, func(b []byte) {
			binary.LittleEndian.PutUint16(b[tordGroupMsg0+1:], 19) // 6 + 19 > 24
			resignOHDR(b)
		}, "runs past its chunk's end"},
		{"v1 gap", tordRootHdr, func(b []byte) {
			binary.LittleEndian.PutUint32(b[tordRootHdr+8:], 28) // one 8 + 16-byte message, 4 bytes left
		}, "version 1 object header block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := bytes.Clone(base)
			tc.patch(img)
			r := bytes.NewReader(img)
			sb, err := ReadSuperblock(r)
			require.NoError(t, err)
			_, err = ReadObjectHeader(r, tc.addr, sb)
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
