package core

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The targets in this file fuzz the metadata parsers that handle bytes coming
// straight from a file. Issue #68 (ADR-003) is the reason they exist: every one
// of these parsers feeds sizes into allocations, and in Go a failed allocation
// is a fatal runtime throw, not a recoverable error. A target that panics here
// fails the fuzz run — there is deliberately no recover() anywhere below, so a
// panic is reported with its reproducer instead of being hidden.
//
// Whole-file fuzzing (Open + Walk + Read) lives in the root package, because
// internal/core cannot import its own parent without an import cycle.

// FuzzParseDatatypeMessage feeds arbitrary bytes to the datatype decoder and
// exercises the accessors callers reach immediately afterwards.
func FuzzParseDatatypeMessage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	// Fixed-point 32-bit: class 0, version 1, size 4, bit offset 0, precision 32.
	f.Add([]byte{0x10, 0, 0, 0, 4, 0, 0, 0, 0, 0, 32, 0})
	// IEEE float64: class 1, version 1, size 8, 12 property bytes.
	f.Add([]byte{
		0x11, 0, 0, 0, 8, 0, 0, 0,
		0, 0, 64, 0, 52, 0, 11, 0, 52, 0, 0x3f, 0x01, 0, 0, 0, 0,
	})
	// Fixed-length string: class 3, version 1, size 8, null-padded ASCII.
	f.Add([]byte{0x13, 0, 0, 0, 8, 0, 0, 0, 0, 0})

	f.Fuzz(func(_ *testing.T, data []byte) {
		dt, err := ParseDatatypeMessage(data)
		if err != nil {
			return
		}

		// Accessors used by every decoder; none may panic on a parsed value.
		_ = dt.String()
		_ = dt.GetByteOrder()
		if dt.IsString() {
			_ = dt.GetStringPadding()
		}
	})
}

// FuzzParseDataspaceMessage feeds arbitrary bytes to the dataspace decoder.
// TotalElements multiplies the dimensions, so a parsed dataspace is exactly
// where an inflated element count enters the library.
func FuzzParseDataspaceMessage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 0, 0, 0, 0, 0, 0, 0})
	// Version 1, rank 1, flags 0, reserved 5, dimension 4.
	f.Add([]byte{1, 1, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0})
	// Version 2, rank 2, flags 0, type 0, dimensions 3 and 5.
	f.Add([]byte{2, 2, 0, 0, 3, 0, 0, 0, 5, 0, 0, 0})
	// Version 2 with max dimensions present.
	f.Add([]byte{2, 1, 1, 0, 4, 0, 0, 0, 8, 0, 0, 0})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ds, err := ParseDataspaceMessage(data)
		if err != nil {
			return
		}

		_ = ds.TotalElements()
		_ = ds.String()
	})
}

// FuzzParseAttributeMessage feeds arbitrary bytes to the attribute decoder and,
// on success, to ReadValue — the path ADR-003 bounds against the attribute's
// own data length (cf. H5O__attr_decode, H5Oattr.c:274-283). ReadValue needs no
// file reader for the numeric and fixed-string branches; the variable-length
// branch reports a missing reader instead of dereferencing one.
func FuzzParseAttributeMessage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 0, 5, 0, 12, 0, 12, 0})
	// Version 1, one int32[2] attribute named "abcd": header, name padded to
	// 8 bytes, datatype padded to 16, dataspace padded to 16, 8 data bytes.
	f.Add([]byte{
		1, 0, 5, 0, 12, 0, 12, 0,
		'a', 'b', 'c', 'd', 0, 0, 0, 0,
		0x10, 0, 0, 0, 4, 0, 0, 0, 0, 0, 32, 0, 0, 0, 0, 0,
		1, 1, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 2, 0, 0, 0,
	})
	// Version 3 "abc" float64 scalar: no padding between the sections.
	f.Add([]byte{
		3, 0, 4, 0, 20, 0, 8, 0, 0,
		'a', 'b', 'c', 0,
		0x11, 0, 0, 0, 8, 0, 0, 0,
		0, 0, 64, 0, 52, 0, 11, 0, 52, 0, 0x3f, 0x01,
		2, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0xf0, 0x3f,
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		attr, err := ParseAttributeMessage(data, binary.LittleEndian)
		if err != nil {
			return
		}

		if attr.Datatype == nil || attr.Dataspace == nil {
			t.Fatalf("parsed attribute without datatype or dataspace")
		}

		// Errors are expected for most inputs; a panic or a fatal allocation
		// is not. The returned value is intentionally not inspected: the
		// bounds it must respect are checked in the whole-file target, where a
		// real file size is available to compare against.
		_, _ = attr.ReadValue()
	})
}

// FuzzParseBTreeV1Node feeds arbitrary bytes to the chunk B-tree decoder with
// fixed offset size and rank, so the entry count is the only unbounded field.
// The ADR-003 scope note keeps this parser out of the cap work because
// EntriesUsed is 16 bits (H5Bpkg.h), which bounds the node at a few megabytes.
func FuzzParseBTreeV1Node(f *testing.F) {
	f.Add([]byte{})
	f.Add(btreeV1NodeSeed())

	f.Fuzz(func(_ *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = ParseBTreeV1Node(r, 0, 8, 2, []uint64{1, 1})
	})
}

// FuzzReadGlobalHeapCollection feeds arbitrary bytes to the GCOL reader.
// ADR-003 (TASK-050) bounds the collection size from the header against the
// reader size and the hard cap; this target is the regression net for that.
func FuzzReadGlobalHeapCollection(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("GCOL"))
	f.Add(globalHeapCollectionSeed())

	f.Fuzz(func(_ *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = ReadGlobalHeapCollection(r, 0, 8)
	})
}

// btreeV1NodeSeed builds a structurally valid chunk B-tree leaf: signature,
// type 1, level 0, one entry, two undefined siblings, two keys and one child
// for rank 2 with 8-byte offsets.
func btreeV1NodeSeed() []byte {
	const (
		ndims   = 2
		keySize = 4 + 4 + ndims*8
	)
	buf := make([]byte, 0, 4+1+1+2+8+8+2*keySize+8)
	buf = append(buf, "TREE"...)
	buf = append(buf, 1, 0) // Node type: chunked dataset; node level: leaf.
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint64(buf, ^uint64(0))
	buf = binary.LittleEndian.AppendUint64(buf, ^uint64(0))

	for i := 0; i < 2; i++ {
		buf = binary.LittleEndian.AppendUint32(buf, 16) // Chunk byte count.
		buf = binary.LittleEndian.AppendUint32(buf, 0)  // Filter mask.
		buf = binary.LittleEndian.AppendUint64(buf, uint64(i))
		buf = binary.LittleEndian.AppendUint64(buf, 0)
		if i == 0 {
			buf = binary.LittleEndian.AppendUint64(buf, 64) // Child address.
		}
	}

	return buf
}

// globalHeapCollectionSeed builds the 32-byte header of a GCOL collection with
// no objects, which is enough for the reader to reach its parse loop.
func globalHeapCollectionSeed() []byte {
	buf := make([]byte, 0, 32)
	buf = append(buf, "GCOL"...)
	buf = append(buf, 1, 0, 0, 0)                   // Version 1, reserved.
	buf = binary.LittleEndian.AppendUint64(buf, 32) // Collection size.
	buf = append(buf, make([]byte, 32-len(buf))...) // Pad to the declared size.

	return buf
}
