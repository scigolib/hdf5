package hdf5

import (
	"bytes"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// TestVLenUint8_RoundTrip writes VLenUint8 data, closes the file, reopens it,
// reads back via ReadVLenBytes, and verifies the data matches exactly.
func TestVLenUint8_RoundTrip(t *testing.T) {
	filename := "test_vlen_uint8_roundtrip.h5"
	defer os.Remove(filename)

	expected := [][]byte{
		{0x01, 0x02, 0x03},
		{0xFF},
		{0x00, 0xAB, 0xCD, 0xEF},
	}

	writeVLenUint8File(t, filename, "/bytes", expected)

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	dataset := findDataset(f, "/bytes")
	if dataset == nil {
		t.Fatal("Dataset '/bytes' not found after reopen")
	}

	got, err := dataset.ReadVLenBytes()
	if err != nil {
		t.Fatalf("ReadVLenBytes failed: %v", err)
	}

	assertVLenBytesEqual(t, expected, got)
}

// TestVLenUint8_Encode_ByteCheck writes a VLen dataset and verifies that the
// datatype message header byte encodes class=9 correctly (0x09, not the
// previous incorrect 0x90 from swapped nibbles).
func TestVLenUint8_Encode_ByteCheck(t *testing.T) {
	filename := "test_vlen_encode_check.h5"
	defer os.Remove(filename)

	writeVLenUint8File(t, filename, "/bytes", [][]byte{{0x01}, {0x02}})

	firstByte := readVLenDatatypeFirstByte(t, filename)

	// C Reference (H5Odtype.c:1439): byte 0 = (class & 0x0F) | (version << 4).
	// For VLen class=9, version=0: byte 0 = 0x09.
	class := firstByte & 0x0F
	version := (firstByte >> 4) & 0x0F

	if class != 9 {
		t.Errorf("VLen datatype class: got %d (byte=0x%02X), want 9", class, firstByte)
	}
	if version != 0 {
		t.Errorf("VLen datatype version: got %d, want 0", version)
	}
	if firstByte == 0x90 {
		t.Error("BUG: first byte is 0x90 (class/version nibbles are SWAPPED)")
	}
}

// TestVLenUint8_EmptySequences writes a mix of empty and non-empty byte sequences,
// then reads them back and verifies correctness.
func TestVLenUint8_EmptySequences(t *testing.T) {
	filename := "test_vlen_uint8_empty_seq.h5"
	defer os.Remove(filename)

	expected := [][]byte{
		{},           // empty
		{0x42, 0x43}, // non-empty
		{},           // empty
		{0x01},       // single byte
		{},           // empty
	}

	writeVLenUint8File(t, filename, "/mixed", expected)

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	dataset := findDataset(f, "/mixed")
	if dataset == nil {
		t.Fatal("Dataset '/mixed' not found")
	}

	got, err := dataset.ReadVLenBytes()
	if err != nil {
		t.Fatalf("ReadVLenBytes failed: %v", err)
	}

	assertVLenBytesEqual(t, expected, got)
}

// TestVLenUint8_LargeData writes 150 variable-length sequences and reads them back.
func TestVLenUint8_LargeData(t *testing.T) {
	filename := "test_vlen_uint8_large.h5"
	defer os.Remove(filename)

	const numElements = 150
	expected := make([][]byte, numElements)
	for i := range expected {
		seq := make([]byte, i)
		for j := range seq {
			seq[j] = byte((i + j) & 0xFF)
		}
		expected[i] = seq
	}

	writeVLenUint8File(t, filename, "/large", expected)

	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	dataset := findDataset(f, "/large")
	if dataset == nil {
		t.Fatal("Dataset '/large' not found")
	}

	got, err := dataset.ReadVLenBytes()
	if err != nil {
		t.Fatalf("ReadVLenBytes failed: %v", err)
	}

	assertVLenBytesEqual(t, expected, got)
}

// --- Test helpers ---

// writeVLenUint8File creates an HDF5 file with a single VLenUint8 dataset.
func writeVLenUint8File(t *testing.T, filename, path string, data [][]byte) {
	t.Helper()
	fw, err := CreateForWrite(filename, CreateTruncate)
	if err != nil {
		t.Fatalf("CreateForWrite failed: %v", err)
	}

	ds, err := fw.CreateDataset(path, VLenUint8, []uint64{uint64(len(data))})
	if err != nil {
		t.Fatalf("CreateDataset failed: %v", err)
	}

	if err := ds.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// readVLenDatatypeFirstByte opens a file and returns the first byte of the
// VLen dataset's datatype message.
func readVLenDatatypeFirstByte(t *testing.T, filename string) byte {
	t.Helper()
	f, err := Open(filename)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	dataset := findDataset(f, "/bytes")
	if dataset == nil {
		t.Fatal("Dataset '/bytes' not found")
	}

	header, err := core.ReadObjectHeader(f.Reader(), dataset.Address(), f.Superblock())
	if err != nil {
		t.Fatalf("ReadObjectHeader failed: %v", err)
	}

	for _, msg := range header.Messages {
		if msg.Type != core.MsgDatatype {
			continue
		}
		if len(msg.Data) < 1 {
			t.Fatal("Datatype message too short")
		}
		return msg.Data[0]
	}

	t.Fatal("Datatype message not found in object header")
	return 0
}

// assertVLenBytesEqual checks that two [][]byte slices are equal element-by-element.
func assertVLenBytesEqual(t *testing.T, expected, got [][]byte) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(expected))
	}
	for i := range expected {
		if !bytes.Equal(got[i], expected[i]) {
			t.Errorf("element %d mismatch: got %v (len=%d), want %v (len=%d)",
				i, got[i], len(got[i]), expected[i], len(expected[i]))
		}
	}
}
