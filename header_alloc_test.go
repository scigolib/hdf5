package hdf5

import (
	"bytes"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// headerChunk is the file space of one object header chunk.
type headerChunk struct{ addr, size uint64 }

// headerChunks reads the header at addr and returns chunk 0 (its address
// only) and its continuation chunks in the order the reader follows them.
func headerChunks(t *testing.T, fw *FileWriter, addr uint64) (*core.ObjectHeader, []headerChunk) {
	t.Helper()
	sb := fw.file.sb
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	require.NoError(t, err)
	chunks := []headerChunk{{addr: addr}}
	osz, lsz := int(sb.OffsetSize), int(sb.LengthSize)
	for _, m := range oh.Messages {
		if m.Type == core.MsgContinuation {
			chunks = append(chunks, headerChunk{readAddressLE(m.Data[:osz]), readAddressLE(m.Data[osz : osz+lsz])})
		}
	}
	return oh, chunks
}

// TestHeaderAlloc_ExtendLastChunkAtEOF: chunk 0 of /c is not at the end of
// the file, so the first link message that finds no free space goes to a new
// continuation chunk, allocated at the end of the file. The next ones extend
// that chunk in place (H5O__alloc_extend_chunk, H5Oalloc.c:476-672, through
// H5MF_try_extend) and no further chunk is added.
func TestHeaderAlloc_ExtendLastChunkAtEOF(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	file := linkGroupsFile(t, h5tool(t, "h5copy"), h5tool(t, "h5repack"), t.TempDir())

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr := linkGroupAddr(t, fw, "/c")
	_, chunks := headerChunks(t, fw, addr)
	n0 := len(chunks)
	soft := softNames(6)
	i := 0
	for ; i < len(soft) && len(chunks) == n0; i++ {
		require.NoError(t, fw.insertGroupLink(addr, soft[i], 0, "/c/x"))
		_, chunks = headerChunks(t, fw, addr)
	}
	require.Len(t, chunks, n0+1, "a continuation chunk added")
	require.Less(t, i, len(soft), "links left to insert after the new chunk")
	before := chunks
	last := before[len(before)-1]
	require.Equal(t, fw.writer.EndOfFile(), last.addr+last.size, "the new chunk ends at EOF")

	for _, name := range soft[i:] {
		require.NoError(t, fw.insertGroupLink(addr, name, 0, "/c/x"))
	}
	_, after := headerChunks(t, fw, addr)
	require.Len(t, after, len(before), "no chunk added")
	grown := after[len(after)-1]
	require.Equal(t, last.addr, grown.addr, "chunk extended in place")
	require.Greater(t, grown.size, last.size, "chunk grew")
	require.Equal(t, fw.writer.EndOfFile(), grown.addr+grown.size)
	require.NoError(t, fw.Close())

	requireLinkGroupFile(t, h5ls, file, "/c", sorted([]string{"x", "y"}, soft))
}

// TestHeaderAlloc_NewChunkMovesMessage: with a block allocated after the
// continuation chunk of /c, no chunk can grow, so the new link messages go
// to a new continuation chunk (H5O__alloc_new_chunk, H5Oalloc.c:1119-1160).
// Chunk 0 has no free space that holds the new continuation message, so a
// message moves from its place into the new chunk and the continuation
// message takes that place (H5O__alloc_find_best_nonnull, :699-830).
func TestHeaderAlloc_NewChunkMovesMessage(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	file := linkGroupsFile(t, h5tool(t, "h5copy"), h5tool(t, "h5repack"), t.TempDir())

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr := linkGroupAddr(t, fw, "/c")
	ohBefore, before := headerChunks(t, fw, addr)
	_, err = fw.writer.Allocator().Allocate(64)
	require.NoError(t, err)

	soft := softNames(6)
	for _, name := range soft {
		require.NoError(t, fw.insertGroupLink(addr, name, 0, "/c/x"))
	}
	ohAfter, after := headerChunks(t, fw, addr)
	require.Greater(t, len(after), len(before), "a continuation chunk added")
	for i, c := range before[1:] {
		require.Equal(t, c, after[i+1], "existing chunks stay")
	}

	// A message of the header before the inserts now lives in a new chunk.
	moved := false
	for _, m := range ohAfter.Messages {
		if m.Type == core.MsgContinuation || m.Type == core.MsgNil {
			continue
		}
		inNew := false
		for _, c := range after[len(before):] {
			inNew = inNew || (m.Offset >= c.addr && m.Offset < c.addr+c.size)
		}
		for _, old := range ohBefore.Messages {
			if inNew && old.Type == m.Type && bytes.Equal(old.Data, m.Data) {
				moved = true
			}
		}
	}
	require.True(t, moved, "a message moved into the new chunk")
	require.NoError(t, fw.Close())

	requireLinkGroupFile(t, h5ls, file, "/c", sorted([]string{"x", "y"}, soft))
}
