package core

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fuzzReadLimit bounds the result a fuzzed chunked read may allocate.
const fuzzReadLimit = 1 << 20

// FuzzDatasetDescriptor feeds arbitrary datatype, dataspace, layout and filter
// pipeline messages to NewDatasetDescriptor, then reads through every dataset
// reader with a small read limit. Nothing may panic, and a descriptor that is
// accepted must satisfy the invariants its validation promises (ADR-004 §1).
//
// The committed corpus under testdata/fuzz/FuzzDatasetDescriptor holds the
// messages of the datasets in testdata/reference, written by
// TestGenerateDescriptorFuzzSeeds in the root package.
func FuzzDatasetDescriptor(f *testing.F) {
	f.Add(buildFloat64DatatypeMessage(), buildDataspaceV1Message([]uint64{12}),
		buildContiguousLayoutMessage(0, 96), []byte(nil), uint8(8), uint32(128))
	f.Add(buildFloat64DatatypeMessage(), buildDataspaceV1Message([]uint64{4}),
		buildCompactLayoutMessage(make([]byte, 32)), []byte(nil), uint8(8), uint32(0))
	f.Add(buildFloat64DatatypeMessage(), buildDataspaceV1Message([]uint64{1 << 40}),
		buildContiguousLayoutMessage(0, 8), []byte(nil), uint8(8), uint32(64))

	f.Fuzz(func(t *testing.T, dtype, space, layout, pipeline []byte, offsetSize uint8, fileSize uint32) {
		sb := &Superblock{
			Version:    2,
			OffsetSize: 8,
			LengthSize: 8,
			Endianness: binary.LittleEndian,
		}
		if offsetSize == 4 {
			sb.OffsetSize, sb.LengthSize = 4, 4
		}

		// The backing file is at most 1 MB, so a descriptor's storage claims
		// beyond that must be rejected.
		r := bytes.NewReader(make([]byte, fileSize%(1<<20)))

		header := &ObjectHeader{}
		for _, m := range []struct {
			typ  MessageType
			data []byte
		}{
			{MsgDatatype, dtype},
			{MsgDataspace, space},
			{MsgDataLayout, layout},
			{MsgFilterPipeline, pipeline},
		} {
			if len(m.data) > 0 {
				header.Messages = append(header.Messages, &HeaderMessage{Type: m.typ, Data: m.data})
			}
		}

		desc, err := NewDatasetDescriptor(r, header, sb)
		if err != nil {
			return
		}

		checkDescriptorInvariants(t, desc, uint64(r.Size()))

		// Every reader must return (an error or data) without panicking and
		// without a result larger than the storage or the limit allows.
		_, _ = ReadFloat64(r, sb, desc, fuzzReadLimit)
		_, _ = ReadStrings(r, sb, desc, fuzzReadLimit)
		_, _ = ReadCompound(r, sb, desc, fuzzReadLimit)
		_, _ = ReadVLenBytes(r, sb, desc, fuzzReadLimit)
	})
}

// checkDescriptorInvariants verifies what an accepted descriptor guarantees.
func checkDescriptorInvariants(t *testing.T, desc *DatasetDescriptor, fileSize uint64) {
	t.Helper()

	if desc.ElementSize() == 0 {
		t.Fatalf("accepted a zero element size")
	}

	layout := desc.Layout()
	switch layout.Class {
	case LayoutContiguous:
		if layout.DataAddress != undefinedAddress && desc.StorageSize() > 0 {
			if layout.DataAddress > fileSize || desc.StorageSize() > fileSize-layout.DataAddress {
				t.Fatalf("contiguous storage [%d, +%d) outside a %d-byte file",
					layout.DataAddress, desc.StorageSize(), fileSize)
			}
		}
	case LayoutCompact:
		if uint64(len(layout.CompactData)) != desc.StorageSize() {
			t.Fatalf("compact data %d bytes, storage %d", len(layout.CompactData), desc.StorageSize())
		}
	case LayoutChunked:
		rank := len(desc.Dims())
		if len(layout.ChunkSize) != rank+1 || layout.ChunkSize[rank] != desc.ElementSize() {
			t.Fatalf("chunk grid %v does not end in the element size %d for rank %d",
				layout.ChunkSize, desc.ElementSize(), rank)
		}
		if _, err := desc.ResultSize(desc.NumElements(), 1, fuzzReadLimit); err == nil &&
			desc.StorageSize() > fuzzReadLimit {
			t.Fatalf("chunked result of %d bytes passed a %d-byte limit", desc.StorageSize(), fuzzReadLimit)
		}
	default:
		t.Fatalf("accepted layout class %d", layout.Class)
	}
}
